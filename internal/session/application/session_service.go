package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	"github.com/404NFIDv2/bot-game-management/internal/games"
	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ─── Dependency interfaces ────────────────────────────────────────────────────

// BotLookup is the minimal bot-domain interface needed by SessionService.
type BotLookup interface {
	FindByID(ctx context.Context, id uuid.UUID) (*botdomain.Bot, error)
}

// GameLookup is the minimal game-domain interface needed by SessionService.
type GameLookup interface {
	FindByID(ctx context.Context, id uuid.UUID) (*gamedomain.Game, error)
}

// BotGameCheck checks whether a game is assigned to a bot.
type BotGameCheck interface {
	ExistsByBotAndGame(ctx context.Context, botID, gameID uuid.UUID) (bool, error)
}

// StateCache abstracts the Redis hot-path for session state.
type StateCache interface {
	GetState(ctx context.Context, id uuid.UUID) (json.RawMessage, error)
	SetState(ctx context.Context, id uuid.UUID, state json.RawMessage, ttl time.Duration) error
	InvalidateState(ctx context.Context, id uuid.UUID) error
}

// EngineRegistry resolves a game engine by slug.
type EngineRegistry interface {
	Get(slug gamedomain.GameSlug) (games.GameEngine, error)
}

// ScoreCommitter commits final session scores to the leaderboard.
// A no-op implementation is used until Phase 4 is wired.
type ScoreCommitter interface {
	CommitSessionScores(ctx context.Context, session *domain.GameSession) error
}

type noopScoreCommitter struct{}

func (n *noopScoreCommitter) CommitSessionScores(_ context.Context, _ *domain.GameSession) error {
	return nil
}

// NewNoopScoreCommitter returns a no-op ScoreCommitter for use before Phase 4.
func NewNoopScoreCommitter() ScoreCommitter { return &noopScoreCommitter{} }

// ─── Request / response types ─────────────────────────────────────────────────

// CreateSessionRequest carries the inputs for CreateSession.
type CreateSessionRequest struct {
	GameID         uuid.UUID
	ChatID         int64
	TelegramUserID int64
	DisplayName    string
}

// JoinRequest carries player info for JoinSession.
type JoinRequest struct {
	TelegramUserID int64
	DisplayName    string
}

// MoveRequest carries a player move for SubmitMove.
type MoveRequest struct {
	PlayerID int64
	Payload  map[string]any
}

// MoveResult is the response from SubmitMove.
type MoveResult struct {
	Session *domain.GameSession
	Events  []games.Event
}

// EndSessionRequest carries inputs for EndSession.
type EndSessionRequest struct {
	CallerTelegramID int64
	IsAdmin          bool
	Reason           string
}

// ─── Service ──────────────────────────────────────────────────────────────────

// SessionService implements all session lifecycle use cases.
type SessionService struct {
	botRepo        BotLookup
	gameRepo       GameLookup
	botGameRepo    BotGameCheck
	sessionRepo    domain.SessionRepository
	cache          StateCache
	registry       EngineRegistry
	scoreCommitter ScoreCommitter
	sessionTTL     time.Duration
}

func NewSessionService(
	botRepo BotLookup,
	gameRepo GameLookup,
	botGameRepo BotGameCheck,
	sessionRepo domain.SessionRepository,
	cache StateCache,
	registry EngineRegistry,
	scoreCommitter ScoreCommitter,
	sessionTTL time.Duration,
) *SessionService {
	return &SessionService{
		botRepo:        botRepo,
		gameRepo:       gameRepo,
		botGameRepo:    botGameRepo,
		sessionRepo:    sessionRepo,
		cache:          cache,
		registry:       registry,
		scoreCommitter: scoreCommitter,
		sessionTTL:     sessionTTL,
	}
}

// CreateSession creates a new game session. The host is added as the first player.
// Engine state is initialised at StartSession time (when all players are known).
func (s *SessionService) CreateSession(ctx context.Context, botID uuid.UUID, req CreateSessionRequest) (*domain.GameSession, error) {
	bot, err := s.botRepo.FindByID(ctx, botID)
	if err != nil {
		return nil, apperrors.NotFound("bot not found")
	}
	if !bot.Active {
		return nil, apperrors.NotFound("bot is inactive")
	}

	if _, err := s.gameRepo.FindByID(ctx, req.GameID); err != nil {
		return nil, apperrors.NotFound("game not found")
	}

	assigned, err := s.botGameRepo.ExistsByBotAndGame(ctx, botID, req.GameID)
	if err != nil {
		return nil, apperrors.Internal("failed to check game assignment").WithCause(err)
	}
	if !assigned {
		return nil, apperrors.NotFound("game is not assigned to this bot")
	}

	// Guard: no active session for this chat on this bot.
	if existing, err := s.sessionRepo.FindActiveByChatID(ctx, botID, req.ChatID); err == nil && existing != nil {
		return nil, apperrors.Conflict("an active session already exists for this chat")
	}

	session := domain.NewGameSession(botID, req.GameID, req.ChatID)
	host := domain.PlayerSession{
		SessionID:      session.ID,
		TelegramUserID: req.TelegramUserID,
		DisplayName:    req.DisplayName,
		JoinedAt:       time.Now().UTC(),
	}
	// Cannot fail: session is CREATED and player list is empty.
	_ = session.AddPlayer(host)

	if err := s.sessionRepo.Save(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// JoinSession adds a player to a CREATED or WAITING session.
func (s *SessionService) JoinSession(ctx context.Context, botID, sessionID uuid.UUID, req JoinRequest) (*domain.GameSession, error) {
	session, err := s.getSessionForBot(ctx, sessionID, botID)
	if err != nil {
		return nil, err
	}

	// Idempotent re-join.
	for _, p := range session.Players {
		if p.TelegramUserID == req.TelegramUserID {
			return session, nil
		}
	}

	game, err := s.gameRepo.FindByID(ctx, session.GameID)
	if err != nil {
		return nil, apperrors.Internal("failed to fetch game").WithCause(err)
	}
	if len(session.Players) >= game.MaxPlayers {
		return nil, apperrors.Conflict("session is full")
	}

	newPlayer := domain.PlayerSession{
		SessionID:      session.ID,
		TelegramUserID: req.TelegramUserID,
		DisplayName:    req.DisplayName,
		JoinedAt:       time.Now().UTC(),
	}
	if err := session.AddPlayer(newPlayer); err != nil {
		return nil, err
	}

	// Transition to WAITING once min players threshold is reached.
	if session.Status == domain.StatusCreated && len(session.Players) >= game.MinPlayers {
		session.Status = domain.StatusWaiting
	}

	if err := s.sessionRepo.Save(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// StartSession transitions a WAITING session to IN_PROGRESS. Only the host can start.
// The game engine is initialised here — this is when all players are known.
func (s *SessionService) StartSession(ctx context.Context, botID, sessionID uuid.UUID, callerTelegramID int64) (*domain.GameSession, error) {
	session, err := s.getSessionForBot(ctx, sessionID, botID)
	if err != nil {
		return nil, err
	}
	if session.HostPlayerID() != callerTelegramID {
		return nil, apperrors.Forbidden("only the host can start the session")
	}

	game, err := s.gameRepo.FindByID(ctx, session.GameID)
	if err != nil {
		return nil, apperrors.Internal("failed to fetch game").WithCause(err)
	}
	engine, err := s.registry.Get(game.Slug)
	if err != nil {
		return nil, err
	}

	players := make([]games.Player, len(session.Players))
	for i, p := range session.Players {
		players[i] = games.Player{TelegramUserID: p.TelegramUserID, DisplayName: p.DisplayName}
	}
	initialState, err := engine.Init(players, nil)
	if err != nil {
		return nil, apperrors.Unprocessable(err.Error())
	}
	session.State = initialState

	if err := session.Start(); err != nil {
		return nil, err
	}

	if err := s.sessionRepo.Save(ctx, session); err != nil {
		return nil, err
	}
	// Warm the cache with the fresh state.
	_ = s.cache.SetState(ctx, session.ID, session.State, s.sessionTTL)

	return session, nil
}

// SubmitMove validates and applies a player move. Triggers session finish if the game ends.
func (s *SessionService) SubmitMove(ctx context.Context, botID, sessionID uuid.UUID, req MoveRequest) (*MoveResult, error) {
	session, err := s.getSessionForBot(ctx, sessionID, botID)
	if err != nil {
		return nil, err
	}
	if session.Status != domain.StatusInProgress {
		return nil, apperrors.Unprocessable("session is not in progress")
	}

	state, err := s.loadState(ctx, session)
	if err != nil {
		return nil, err
	}

	game, err := s.gameRepo.FindByID(ctx, session.GameID)
	if err != nil {
		return nil, apperrors.Internal("failed to fetch game").WithCause(err)
	}
	engine, err := s.registry.Get(game.Slug)
	if err != nil {
		return nil, err
	}

	move := games.Move{PlayerID: req.PlayerID, Payload: req.Payload}

	if err := engine.Validate(state, move); err != nil {
		return nil, apperrors.Unprocessable(err.Error())
	}

	newState, events, err := engine.Apply(state, move)
	if err != nil {
		return nil, apperrors.Unprocessable(err.Error())
	}

	// Atomic write: Redis first, then Postgres. Roll back Redis on DB failure.
	if err := s.cache.SetState(ctx, session.ID, newState, s.sessionTTL); err != nil {
		return nil, err
	}
	if err := s.sessionRepo.UpdateState(ctx, session.ID, newState); err != nil {
		_ = s.cache.InvalidateState(ctx, session.ID)
		return nil, err
	}
	session.State = newState

	finished, result, err := engine.Evaluate(newState)
	if err != nil {
		return nil, apperrors.Internal("evaluate failed").WithCause(err)
	}
	if finished {
		if err := s.finishSession(ctx, session, result); err != nil {
			return nil, err
		}
	}

	return &MoveResult{Session: session, Events: events}, nil
}

// EndSession force-ends a session. Host or admin may call this.
func (s *SessionService) EndSession(ctx context.Context, botID, sessionID uuid.UUID, req EndSessionRequest) (*domain.GameSession, error) {
	session, err := s.getSessionForBot(ctx, sessionID, botID)
	if err != nil {
		return nil, err
	}
	if !session.IsActive() {
		return nil, apperrors.Conflict("session is already finished or archived")
	}
	if !req.IsAdmin && session.HostPlayerID() != req.CallerTelegramID {
		return nil, apperrors.Forbidden("only the host or admin can end the session")
	}

	if session.Status == domain.StatusInProgress {
		scores := make(map[int64]int, len(session.Players))
		for _, p := range session.Players {
			scores[p.TelegramUserID] = p.Score
		}
		if err := session.Finish(scores); err != nil {
			return nil, err
		}
	} else {
		now := time.Now().UTC()
		session.Status = domain.StatusFinished
		session.EndedAt = &now
	}

	if err := s.sessionRepo.Save(ctx, session); err != nil {
		return nil, err
	}
	_ = s.cache.InvalidateState(ctx, session.ID)
	_ = s.scoreCommitter.CommitSessionScores(ctx, session)

	return session, nil
}

// GetSession returns a session scoped to a bot.
func (s *SessionService) GetSession(ctx context.Context, botID, sessionID uuid.UUID) (*domain.GameSession, error) {
	return s.getSessionForBot(ctx, sessionID, botID)
}

// ListSessions returns sessions for a bot with optional filters and pagination.
func (s *SessionService) ListSessions(ctx context.Context, botID uuid.UUID, filter domain.SessionFilter, limit, offset int) ([]*domain.GameSession, int, error) {
	return s.sessionRepo.FindByBotID(ctx, botID, filter, limit, offset)
}

// ForceEndBotSessions implements bot/application.SessionEnder. Called on bot deletion.
func (s *SessionService) ForceEndBotSessions(ctx context.Context, botID uuid.UUID) error {
	sessions, _, err := s.sessionRepo.FindByBotID(ctx, botID, domain.SessionFilter{}, 1000, 0)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if !session.IsActive() {
			continue
		}
		req := EndSessionRequest{IsAdmin: true, Reason: "bot deleted"}
		if _, err := s.EndSession(ctx, botID, session.ID, req); err != nil {
			return err
		}
	}
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (s *SessionService) getSessionForBot(ctx context.Context, sessionID, botID uuid.UUID) (*domain.GameSession, error) {
	session, err := s.sessionRepo.FindByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.BotID != botID {
		return nil, apperrors.NotFound("session not found")
	}
	return session, nil
}

// loadState returns state from Redis if warm; otherwise fetches from the
// already-loaded session and repopulates the cache.
func (s *SessionService) loadState(ctx context.Context, session *domain.GameSession) (json.RawMessage, error) {
	cached, err := s.cache.GetState(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached, nil
	}
	// Cache miss: repopulate from DB snapshot on the session struct.
	_ = s.cache.SetState(ctx, session.ID, session.State, s.sessionTTL)
	return session.State, nil
}

func (s *SessionService) finishSession(ctx context.Context, session *domain.GameSession, result games.Result) error {
	scores := result.Scores
	if scores == nil {
		scores = make(map[int64]int)
	}
	if err := session.Finish(scores); err != nil {
		return err
	}
	for _, w := range result.Winners {
		session.MarkWinner(w.TelegramUserID)
	}
	if err := s.sessionRepo.Save(ctx, session); err != nil {
		return err
	}
	_ = s.cache.InvalidateState(ctx, session.ID)
	return s.scoreCommitter.CommitSessionScores(ctx, session)
}
