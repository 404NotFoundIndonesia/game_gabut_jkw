package application_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	"github.com/404NFIDv2/bot-game-management/internal/games"
	"github.com/404NFIDv2/bot-game-management/internal/session/application"
	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeBotLookup struct {
	bot *botdomain.Bot
	err error
}

func (f *fakeBotLookup) FindByID(_ context.Context, _ uuid.UUID) (*botdomain.Bot, error) {
	return f.bot, f.err
}

type fakeGameLookup struct {
	game *gamedomain.Game
	err  error
}

func (f *fakeGameLookup) FindByID(_ context.Context, _ uuid.UUID) (*gamedomain.Game, error) {
	return f.game, f.err
}

type fakeBotGameCheck struct {
	exists bool
	err    error
}

func (f *fakeBotGameCheck) ExistsByBotAndGame(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return f.exists, f.err
}

type fakeSessionRepo struct {
	sessions map[uuid.UUID]*domain.GameSession
	saveErr  error
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{sessions: make(map[uuid.UUID]*domain.GameSession)}
}

func (f *fakeSessionRepo) Save(_ context.Context, s *domain.GameSession) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	// Store a copy so mutations don't affect stored state.
	clone := *s
	clone.Players = append([]domain.PlayerSession{}, s.Players...)
	f.sessions[s.ID] = &clone
	return nil
}

func (f *fakeSessionRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.GameSession, error) {
	s, ok := f.sessions[id]
	if !ok {
		return nil, apperrors.NotFound("session not found")
	}
	clone := *s
	clone.Players = append([]domain.PlayerSession{}, s.Players...)
	return &clone, nil
}

func (f *fakeSessionRepo) FindByBotID(_ context.Context, botID uuid.UUID, _ domain.SessionFilter, _, _ int) ([]*domain.GameSession, int, error) {
	var out []*domain.GameSession
	for _, s := range f.sessions {
		if s.BotID == botID {
			out = append(out, s)
		}
	}
	return out, len(out), nil
}

func (f *fakeSessionRepo) FindActiveByChatID(_ context.Context, botID uuid.UUID, chatID int64) (*domain.GameSession, error) {
	for _, s := range f.sessions {
		if s.BotID == botID && s.ChatID == chatID && s.IsActive() {
			return s, nil
		}
	}
	return nil, apperrors.NotFound("no active session")
}

func (f *fakeSessionRepo) UpdateState(_ context.Context, id uuid.UUID, state json.RawMessage) error {
	s, ok := f.sessions[id]
	if !ok {
		return apperrors.NotFound("session not found")
	}
	s.State = state
	return nil
}

func (f *fakeSessionRepo) FindFinishedBefore(_ context.Context, _ time.Time, _ int) ([]*domain.GameSession, error) {
	return nil, nil
}

func (f *fakeSessionRepo) FindInProgressOlderThan(_ context.Context, _ time.Time, _ int) ([]*domain.GameSession, error) {
	return nil, nil
}

type fakeStateCache struct {
	states map[uuid.UUID]json.RawMessage
}

func newFakeStateCache() *fakeStateCache {
	return &fakeStateCache{states: make(map[uuid.UUID]json.RawMessage)}
}

func (f *fakeStateCache) GetState(_ context.Context, id uuid.UUID) (json.RawMessage, error) {
	return f.states[id], nil
}

func (f *fakeStateCache) SetState(_ context.Context, id uuid.UUID, state json.RawMessage, _ time.Duration) error {
	f.states[id] = state
	return nil
}

func (f *fakeStateCache) InvalidateState(_ context.Context, id uuid.UUID) error {
	delete(f.states, id)
	return nil
}

type fakeEngine struct {
	initState   json.RawMessage
	initErr     error
	validateErr error
	applyState  json.RawMessage
	applyEvents []games.Event
	applyErr    error
	finished    bool
	result      games.Result
}

func (f *fakeEngine) Init(_ []games.Player, _ map[string]any) (json.RawMessage, error) {
	if f.initErr != nil {
		return nil, f.initErr
	}
	if f.initState != nil {
		return f.initState, nil
	}
	return json.RawMessage(`{"status":"in_progress"}`), nil
}

func (f *fakeEngine) Validate(_ json.RawMessage, _ games.Move) error {
	return f.validateErr
}

func (f *fakeEngine) Apply(_ json.RawMessage, _ games.Move) (json.RawMessage, []games.Event, error) {
	if f.applyErr != nil {
		return nil, nil, f.applyErr
	}
	state := f.applyState
	if state == nil {
		state = json.RawMessage(`{"status":"in_progress"}`)
	}
	return state, f.applyEvents, nil
}

func (f *fakeEngine) Evaluate(_ json.RawMessage) (bool, games.Result, error) {
	return f.finished, f.result, nil
}

type fakeRegistry struct {
	engine games.GameEngine
	err    error
}

func (f *fakeRegistry) Get(_ gamedomain.GameSlug) (games.GameEngine, error) {
	return f.engine, f.err
}

type fakeScoreCommitter struct{ called int }

func (f *fakeScoreCommitter) CommitSessionScores(_ context.Context, _ *domain.GameSession) error {
	f.called++
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

var (
	seedBotID  = uuid.New()
	seedGameID = uuid.New()
	seedGame   = &gamedomain.Game{
		ID: seedGameID, Slug: gamedomain.SlugUno,
		Name: "Uno", MinPlayers: 2, MaxPlayers: 10,
	}
	seedBot = func() *botdomain.Bot {
		b := botdomain.NewBot("TestBot", botdomain.NewBotToken("enc"), "hash", 123)
		b.Activate()
		return b
	}()
)

func newSvc(opts ...func(*svcConfig)) (*application.SessionService, *fakeSessionRepo, *fakeStateCache, *fakeScoreCommitter) {
	cfg := &svcConfig{
		bot:       seedBot,
		game:      seedGame,
		assigned:  true,
		engine:    &fakeEngine{},
	}
	for _, o := range opts {
		o(cfg)
	}

	repo := newFakeSessionRepo()
	cache := newFakeStateCache()
	scorer := &fakeScoreCommitter{}

	svc := application.NewSessionService(
		&fakeBotLookup{bot: cfg.bot, err: cfg.botErr},
		&fakeGameLookup{game: cfg.game, err: cfg.gameErr},
		&fakeBotGameCheck{exists: cfg.assigned},
		repo,
		cache,
		&fakeRegistry{engine: cfg.engine},
		scorer,
		time.Hour,
	)
	return svc, repo, cache, scorer
}

type svcConfig struct {
	bot      *botdomain.Bot
	botErr   error
	game     *gamedomain.Game
	gameErr  error
	assigned bool
	engine   games.GameEngine
}

func createReq(chatID int64) application.CreateSessionRequest {
	return application.CreateSessionRequest{
		GameID:         seedGameID,
		ChatID:         chatID,
		TelegramUserID: 1001,
		DisplayName:    "Host",
	}
}

func mustCreate(t *testing.T, svc *application.SessionService, repo *fakeSessionRepo) *domain.GameSession {
	t.Helper()
	session, err := svc.CreateSession(context.Background(), seedBotID, createReq(9001))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session
}

// ── CreateSession ─────────────────────────────────────────────────────────────

func TestCreateSession_Success(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session, err := svc.CreateSession(context.Background(), seedBotID, createReq(1001))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status != domain.StatusCreated {
		t.Errorf("expected CREATED, got %v", session.Status)
	}
	if len(session.Players) != 1 {
		t.Errorf("expected 1 player (host), got %d", len(session.Players))
	}
	if _, ok := repo.sessions[session.ID]; !ok {
		t.Error("session not persisted")
	}
}

func TestCreateSession_BotNotFound(t *testing.T) {
	svc, _, _, _ := newSvc(func(c *svcConfig) {
		c.botErr = apperrors.NotFound("bot")
	})
	_, err := svc.CreateSession(context.Background(), seedBotID, createReq(1))
	assertNotFound(t, err)
}

func TestCreateSession_BotInactive(t *testing.T) {
	inactive := botdomain.NewBot("X", botdomain.NewBotToken("e"), "h", 1)
	inactive.Deactivate()
	svc, _, _, _ := newSvc(func(c *svcConfig) { c.bot = inactive })
	_, err := svc.CreateSession(context.Background(), seedBotID, createReq(1))
	assertNotFound(t, err)
}

func TestCreateSession_GameNotFound(t *testing.T) {
	svc, _, _, _ := newSvc(func(c *svcConfig) {
		c.gameErr = apperrors.NotFound("game")
	})
	_, err := svc.CreateSession(context.Background(), seedBotID, createReq(1))
	assertNotFound(t, err)
}

func TestCreateSession_GameNotAssigned(t *testing.T) {
	svc, _, _, _ := newSvc(func(c *svcConfig) { c.assigned = false })
	_, err := svc.CreateSession(context.Background(), seedBotID, createReq(1))
	assertNotFound(t, err)
}

func TestCreateSession_DuplicateChatID_Conflict(t *testing.T) {
	svc, _, _, _ := newSvc()
	_, _ = svc.CreateSession(context.Background(), seedBotID, createReq(7777))
	_, err := svc.CreateSession(context.Background(), seedBotID, createReq(7777))
	assertConflict(t, err)
}

// ── JoinSession ───────────────────────────────────────────────────────────────

func TestJoinSession_Success(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)

	joined, err := svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{
		TelegramUserID: 2002,
		DisplayName:    "Bob",
	})
	if err != nil {
		t.Fatalf("JoinSession: %v", err)
	}
	if len(joined.Players) != 2 {
		t.Errorf("expected 2 players, got %d", len(joined.Players))
	}
	// MinPlayers=2, so session should transition to WAITING.
	if joined.Status != domain.StatusWaiting {
		t.Errorf("expected WAITING after 2 players, got %v", joined.Status)
	}
}

func TestJoinSession_Idempotent(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)

	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})
	joined, err := svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})
	if err != nil {
		t.Fatalf("second join: %v", err)
	}
	if len(joined.Players) != 2 {
		t.Errorf("expected 2 players after re-join, got %d", len(joined.Players))
	}
}

func TestJoinSession_SessionFull(t *testing.T) {
	smallGame := &gamedomain.Game{ID: seedGameID, Slug: gamedomain.SlugUno, MinPlayers: 2, MaxPlayers: 2}
	svc, repo, _, _ := newSvc(func(c *svcConfig) { c.game = smallGame })
	session := mustCreate(t, svc, repo)

	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "B"})
	_, err := svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 3003, DisplayName: "C"})
	assertConflict(t, err)
}

func TestJoinSession_WrongStatus(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)
	// Force status to IN_PROGRESS.
	stored := repo.sessions[session.ID]
	stored.Status = domain.StatusInProgress

	_, err := svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002})
	assertConflict(t, err)
}

// ── StartSession ──────────────────────────────────────────────────────────────

func TestStartSession_Success(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)
	// Join second player so status moves to WAITING.
	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})

	started, err := svc.StartSession(context.Background(), seedBotID, session.ID, 1001)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if started.Status != domain.StatusInProgress {
		t.Errorf("expected IN_PROGRESS, got %v", started.Status)
	}
}

func TestStartSession_NonHost_Forbidden(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)
	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})

	_, err := svc.StartSession(context.Background(), seedBotID, session.ID, 9999)
	assertForbidden(t, err)
}

func TestStartSession_WrongStatus_Conflict(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)
	// CREATED status, not WAITING → Start fails.
	_, err := svc.StartSession(context.Background(), seedBotID, session.ID, 1001)
	assertConflict(t, err)
}

// ── SubmitMove ────────────────────────────────────────────────────────────────

func TestSubmitMove_Success(t *testing.T) {
	eng := &fakeEngine{applyEvents: []games.Event{{Type: "CARD_PLAYED"}}}
	svc, repo, _, _ := newSvc(func(c *svcConfig) { c.engine = eng })
	session := mustCreate(t, svc, repo)
	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})
	_, _ = svc.StartSession(context.Background(), seedBotID, session.ID, 1001)

	result, err := svc.SubmitMove(context.Background(), seedBotID, session.ID, application.MoveRequest{
		PlayerID: 1001,
		Payload:  map[string]any{"action": "draw"},
	})
	if err != nil {
		t.Fatalf("SubmitMove: %v", err)
	}
	if len(result.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(result.Events))
	}
}

func TestSubmitMove_NotInProgress(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)

	_, err := svc.SubmitMove(context.Background(), seedBotID, session.ID, application.MoveRequest{PlayerID: 1001})
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeUnprocessable {
		t.Errorf("expected UNPROCESSABLE, got %v", err)
	}
}

func TestSubmitMove_InvalidMove(t *testing.T) {
	eng := &fakeEngine{validateErr: apperrors.Unprocessable("not your turn")}
	svc, repo, _, _ := newSvc(func(c *svcConfig) { c.engine = eng })
	session := mustCreate(t, svc, repo)
	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})
	_, _ = svc.StartSession(context.Background(), seedBotID, session.ID, 1001)

	_, err := svc.SubmitMove(context.Background(), seedBotID, session.ID, application.MoveRequest{PlayerID: 9999})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSubmitMove_GameOver_TriggersFinish(t *testing.T) {
	eng := &fakeEngine{
		finished: true,
		result: games.Result{
			Winners: []games.Player{{TelegramUserID: 1001}},
			Scores:  map[int64]int{1001: 0, 2002: 30},
		},
	}
	scorer := &fakeScoreCommitter{}
	_, repo, _, _ := newSvc(func(c *svcConfig) { c.engine = eng })
	// Re-build with custom scorer.
	svc := application.NewSessionService(
		&fakeBotLookup{bot: seedBot},
		&fakeGameLookup{game: seedGame},
		&fakeBotGameCheck{exists: true},
		repo,
		newFakeStateCache(),
		&fakeRegistry{engine: eng},
		scorer,
		time.Hour,
	)

	session := mustCreate(t, svc, repo)
	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})
	_, _ = svc.StartSession(context.Background(), seedBotID, session.ID, 1001)

	_, err := svc.SubmitMove(context.Background(), seedBotID, session.ID, application.MoveRequest{PlayerID: 1001})
	if err != nil {
		t.Fatalf("SubmitMove: %v", err)
	}
	if scorer.called != 1 {
		t.Errorf("expected CommitSessionScores called once, got %d", scorer.called)
	}
	stored := repo.sessions[session.ID]
	if stored.Status != domain.StatusFinished {
		t.Errorf("expected FINISHED, got %v", stored.Status)
	}
}

// ── EndSession ────────────────────────────────────────────────────────────────

func TestEndSession_ByHost(t *testing.T) {
	svc, repo, _, scorer := newSvc()
	session := mustCreate(t, svc, repo)
	_, _ = svc.JoinSession(context.Background(), seedBotID, session.ID, application.JoinRequest{TelegramUserID: 2002, DisplayName: "Bob"})
	_, _ = svc.StartSession(context.Background(), seedBotID, session.ID, 1001)

	ended, err := svc.EndSession(context.Background(), seedBotID, session.ID, application.EndSessionRequest{
		CallerTelegramID: 1001,
		Reason:           "host ended",
	})
	if err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if ended.Status != domain.StatusFinished {
		t.Errorf("expected FINISHED, got %v", ended.Status)
	}
	if scorer.called != 1 {
		t.Errorf("expected score commit called, got %d", scorer.called)
	}
}

func TestEndSession_ByAdmin(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)

	ended, err := svc.EndSession(context.Background(), seedBotID, session.ID, application.EndSessionRequest{
		IsAdmin: true,
		Reason:  "admin override",
	})
	if err != nil {
		t.Fatalf("EndSession by admin: %v", err)
	}
	if ended.Status != domain.StatusFinished {
		t.Errorf("expected FINISHED, got %v", ended.Status)
	}
}

func TestEndSession_AlreadyFinished_Conflict(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)
	_, _ = svc.EndSession(context.Background(), seedBotID, session.ID, application.EndSessionRequest{IsAdmin: true})

	_, err := svc.EndSession(context.Background(), seedBotID, session.ID, application.EndSessionRequest{IsAdmin: true})
	assertConflict(t, err)
}

func TestEndSession_NonHost_NonAdmin_Forbidden(t *testing.T) {
	svc, repo, _, _ := newSvc()
	session := mustCreate(t, svc, repo)

	_, err := svc.EndSession(context.Background(), seedBotID, session.ID, application.EndSessionRequest{
		CallerTelegramID: 9999,
	})
	assertForbidden(t, err)
}

// ── assertion helpers ─────────────────────────────────────────────────────────

func assertNotFound(t *testing.T, err error) {
	t.Helper()
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND, got %v", err)
	}
}

func assertConflict(t *testing.T, err error) {
	t.Helper()
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT, got %v", err)
	}
}

func assertForbidden(t *testing.T, err error) {
	t.Helper()
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeForbidden {
		t.Errorf("expected FORBIDDEN, got %v", err)
	}
}
