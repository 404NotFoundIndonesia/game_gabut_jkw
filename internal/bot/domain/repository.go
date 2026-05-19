package domain

import (
	"context"

	"github.com/google/uuid"
)

// BotFilter holds optional filters for BotRepository.FindAll.
type BotFilter struct {
	Active *bool // nil = no filter; &true = active only; &false = inactive only
}

// BotRepository defines persistence operations for the Bot aggregate.
type BotRepository interface {
	// Save inserts or updates a bot (upsert on id).
	Save(ctx context.Context, bot *Bot) error

	// FindByID returns a bot or NotFound AppError.
	FindByID(ctx context.Context, id uuid.UUID) (*Bot, error)

	// FindByTelegramID returns a bot by its Telegram user ID or NotFound AppError.
	FindByTelegramID(ctx context.Context, telegramID int64) (*Bot, error)

	// FindByTokenHash returns a bot by its hashed token or NotFound AppError.
	FindByTokenHash(ctx context.Context, tokenHash string) (*Bot, error)

	// FindAll returns a page of bots and the total count matching the filter.
	FindAll(ctx context.Context, filter BotFilter, limit, offset int) ([]*Bot, int, error)

	// Delete removes a bot by ID. Returns NotFound AppError if absent.
	Delete(ctx context.Context, id uuid.UUID) error
}
