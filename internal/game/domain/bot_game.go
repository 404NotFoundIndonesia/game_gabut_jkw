package domain

import (
	"time"

	"github.com/google/uuid"
)

// BotGame represents the assignment of a game to a bot.
type BotGame struct {
	BotID      uuid.UUID `json:"bot_id"`
	GameID     uuid.UUID `json:"game_id"`
	Game       *Game     `json:"game,omitempty"`
	AssignedAt time.Time `json:"assigned_at"`
}
