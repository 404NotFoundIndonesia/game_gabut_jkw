package domain

import (
	"context"

	"github.com/google/uuid"
)

// GameRepository provides read-only access to the seeded game catalog.
type GameRepository interface {
	FindAll(ctx context.Context) ([]*Game, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Game, error)
	FindBySlug(ctx context.Context, slug GameSlug) (*Game, error)
}

// BotGameRepository manages game-to-bot assignments.
type BotGameRepository interface {
	// Assign upserts a bot-game assignment (idempotent).
	Assign(ctx context.Context, botID, gameID uuid.UUID) error

	// Remove deletes a bot-game assignment. Returns NotFound if absent.
	Remove(ctx context.Context, botID, gameID uuid.UUID) error

	// FindByBot returns all games assigned to a bot, with Game populated.
	FindByBot(ctx context.Context, botID uuid.UUID) ([]*BotGame, error)

	// ExistsByBotAndGame returns true if the assignment exists.
	ExistsByBotAndGame(ctx context.Context, botID, gameID uuid.UUID) (bool, error)
}
