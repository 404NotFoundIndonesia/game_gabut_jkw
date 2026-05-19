package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SessionFilter holds optional filters for listing sessions.
type SessionFilter struct {
	Status *SessionStatus
	GameID *uuid.UUID
}

// SessionRepository is the persistence contract for game sessions.
type SessionRepository interface {
	Save(ctx context.Context, session *GameSession) error
	FindByID(ctx context.Context, id uuid.UUID) (*GameSession, error)
	FindByBotID(ctx context.Context, botID uuid.UUID, filter SessionFilter, limit, offset int) ([]*GameSession, int, error)
	FindActiveByChatID(ctx context.Context, botID uuid.UUID, chatID int64) (*GameSession, error)
	UpdateState(ctx context.Context, id uuid.UUID, state json.RawMessage) error
	// FindFinishedBefore returns FINISHED sessions whose ended_at is before the given threshold.
	FindFinishedBefore(ctx context.Context, threshold time.Time, limit int) ([]*GameSession, error)
}
