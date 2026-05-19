package domain

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// LeaderboardEntry holds one player's cumulative stats within a scope.
type LeaderboardEntry struct {
	Rank           int       `json:"rank"`
	BotID          uuid.UUID `json:"bot_id"`
	GameID         uuid.UUID `json:"game_id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	DisplayName    string    `json:"display_name"`
	TotalScore     int       `json:"total_score"`
	GamesPlayed    int       `json:"games_played"`
	Wins           int       `json:"wins"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Leaderboard groups paginated entries with a total count.
type Leaderboard struct {
	Entries []LeaderboardEntry `json:"entries"`
	Total   int                `json:"total"`
}

// LeaderboardRepository defines the persistence contract for leaderboard data.
type LeaderboardRepository interface {
	// UpsertEntry adds a player's stats cumulatively (score/games_played/wins incremented).
	UpsertEntry(ctx context.Context, entry LeaderboardEntry) error

	// GetByBot returns the per-bot leaderboard across all games.
	GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*Leaderboard, error)

	// GetByBotAndGame returns the per-bot, per-game leaderboard.
	GetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params) (*Leaderboard, error)

	// GetGlobal returns the cross-bot, cross-game leaderboard.
	GetGlobal(ctx context.Context, params pagination.Params) (*Leaderboard, error)

	// GetGlobalByGame returns the cross-bot leaderboard for a specific game.
	GetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params) (*Leaderboard, error)

	// HasCommit reports whether scores for the given session have already been committed.
	HasCommit(ctx context.Context, sessionID uuid.UUID) (bool, error)

	// RecordCommit marks a session as having its scores committed (idempotency guard).
	RecordCommit(ctx context.Context, sessionID uuid.UUID) error
}
