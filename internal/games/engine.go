package games

import "encoding/json"

// Player represents a participant in a game session.
type Player struct {
	TelegramUserID int64  `json:"telegram_user_id"`
	DisplayName    string `json:"display_name"`
}

// Move represents a player action submitted to the engine.
type Move struct {
	PlayerID int64          `json:"player_id"`
	Payload  map[string]any `json:"payload"`
}

// Event represents a side-effect or notification produced by an engine action.
type Event struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// Result holds the final outcome of a finished game.
type Result struct {
	Winners []Player       `json:"winners"`
	Scores  map[int64]int  `json:"scores"`
}

// GameEngine is the contract every game must satisfy.
// All methods are pure — no I/O, no side effects.
// State is always json.RawMessage so it can be persisted to JSONB.
type GameEngine interface {
	// Init builds an initial game state for the given players and options.
	Init(players []Player, opts map[string]any) (json.RawMessage, error)

	// Apply executes a move and returns the updated state plus any events.
	Apply(state json.RawMessage, move Move) (json.RawMessage, []Event, error)

	// Evaluate checks whether the game is over and returns the result.
	Evaluate(state json.RawMessage) (bool, Result, error)

	// Validate checks whether a move is legal without mutating state.
	Validate(state json.RawMessage, move Move) error
}
