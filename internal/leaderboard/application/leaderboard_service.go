package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	sessiondomain "github.com/404NFIDv2/bot-game-management/internal/session/domain"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// LeaderboardCache defines the Redis caching contract.
type LeaderboardCache interface {
	GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error)
	SetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params, lb *domain.Leaderboard) error
	InvalidateByBot(ctx context.Context, botID uuid.UUID) error

	GetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error)
	SetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params, lb *domain.Leaderboard) error
	InvalidateByBotAndGame(ctx context.Context, botID, gameID uuid.UUID) error

	GetGlobal(ctx context.Context, params pagination.Params) (*domain.Leaderboard, error)
	SetGlobal(ctx context.Context, params pagination.Params, lb *domain.Leaderboard) error
	InvalidateGlobal(ctx context.Context) error

	GetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error)
	SetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params, lb *domain.Leaderboard) error
	InvalidateGlobalByGame(ctx context.Context, gameID uuid.UUID) error
}

// LeaderboardService implements session/application.ScoreCommitter and exposes leaderboard reads.
type LeaderboardService struct {
	repo  domain.LeaderboardRepository
	cache LeaderboardCache
}

func NewLeaderboardService(repo domain.LeaderboardRepository, cache LeaderboardCache) *LeaderboardService {
	return &LeaderboardService{repo: repo, cache: cache}
}

// CommitSessionScores persists each player's score from a finished session.
// Idempotent: second call for the same session is a no-op.
func (s *LeaderboardService) CommitSessionScores(ctx context.Context, session *sessiondomain.GameSession) error {
	already, err := s.repo.HasCommit(ctx, session.ID)
	if err != nil {
		return err
	}
	if already {
		return nil
	}

	for _, p := range session.Players {
		wins := 0
		if p.IsWinner {
			wins = 1
		}
		entry := domain.LeaderboardEntry{
			BotID:          session.BotID,
			GameID:         session.GameID,
			TelegramUserID: p.TelegramUserID,
			DisplayName:    p.DisplayName,
			TotalScore:     p.Score,
			GamesPlayed:    1,
			Wins:           wins,
		}
		if err := s.repo.UpsertEntry(ctx, entry); err != nil {
			return err
		}
	}

	if err := s.repo.RecordCommit(ctx, session.ID); err != nil {
		return err
	}

	// Invalidate all affected cache scopes.
	_ = s.cache.InvalidateByBot(ctx, session.BotID)
	_ = s.cache.InvalidateByBotAndGame(ctx, session.BotID, session.GameID)
	_ = s.cache.InvalidateGlobal(ctx)
	_ = s.cache.InvalidateGlobalByGame(ctx, session.GameID)

	return nil
}

// GetByBot returns the bot-scoped leaderboard with cache-aside.
func (s *LeaderboardService) GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	if cached, err := s.cache.GetByBot(ctx, botID, params); err == nil && cached != nil {
		return cached, nil
	}
	lb, err := s.repo.GetByBot(ctx, botID, params)
	if err != nil {
		return nil, err
	}
	_ = s.cache.SetByBot(ctx, botID, params, lb)
	return lb, nil
}

// GetByBotAndGame returns the per-bot, per-game leaderboard with cache-aside.
func (s *LeaderboardService) GetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	if cached, err := s.cache.GetByBotAndGame(ctx, botID, gameID, params); err == nil && cached != nil {
		return cached, nil
	}
	lb, err := s.repo.GetByBotAndGame(ctx, botID, gameID, params)
	if err != nil {
		return nil, err
	}
	_ = s.cache.SetByBotAndGame(ctx, botID, gameID, params, lb)
	return lb, nil
}

// GetGlobal returns the global leaderboard with cache-aside.
func (s *LeaderboardService) GetGlobal(ctx context.Context, params pagination.Params) (*domain.Leaderboard, error) {
	if cached, err := s.cache.GetGlobal(ctx, params); err == nil && cached != nil {
		return cached, nil
	}
	lb, err := s.repo.GetGlobal(ctx, params)
	if err != nil {
		return nil, err
	}
	_ = s.cache.SetGlobal(ctx, params, lb)
	return lb, nil
}

// GetGlobalByGame returns the game-scoped global leaderboard with cache-aside.
func (s *LeaderboardService) GetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	if cached, err := s.cache.GetGlobalByGame(ctx, gameID, params); err == nil && cached != nil {
		return cached, nil
	}
	lb, err := s.repo.GetGlobalByGame(ctx, gameID, params)
	if err != nil {
		return nil, err
	}
	_ = s.cache.SetGlobalByGame(ctx, gameID, params, lb)
	return lb, nil
}
