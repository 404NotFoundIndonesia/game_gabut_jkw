package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// SessionStatus is the lifecycle state of a game session.
type SessionStatus string

const (
	StatusCreated    SessionStatus = "CREATED"
	StatusWaiting    SessionStatus = "WAITING"
	StatusInProgress SessionStatus = "IN_PROGRESS"
	StatusFinished   SessionStatus = "FINISHED"
	StatusArchived   SessionStatus = "ARCHIVED"
)

// PlayerSession represents a single player's participation in a session.
type PlayerSession struct {
	SessionID      uuid.UUID `json:"session_id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	DisplayName    string    `json:"display_name"`
	Score          int       `json:"score"`
	IsWinner       bool      `json:"is_winner"`
	JoinedAt       time.Time `json:"joined_at"`
}

// GameSession is the aggregate root for a single game session.
type GameSession struct {
	ID        uuid.UUID       `json:"id"`
	BotID     uuid.UUID       `json:"bot_id"`
	GameID    uuid.UUID       `json:"game_id"`
	ChatID    int64           `json:"chat_id"`
	Status    SessionStatus   `json:"status"`
	State     json.RawMessage `json:"state,omitempty"`
	Players   []PlayerSession `json:"players"`
	StartedAt *time.Time      `json:"started_at,omitempty"`
	EndedAt   *time.Time      `json:"ended_at,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// NewGameSession constructs a new session in CREATED status with empty game state.
func NewGameSession(botID, gameID uuid.UUID, chatID int64) *GameSession {
	return &GameSession{
		ID:        uuid.New(),
		BotID:     botID,
		GameID:    gameID,
		ChatID:    chatID,
		Status:    StatusCreated,
		State:     json.RawMessage("{}"),
		Players:   []PlayerSession{},
		CreatedAt: time.Now().UTC(),
	}
}

// AddPlayer adds a player to the session. Idempotent for the same TelegramUserID.
// Returns Conflict if the session is no longer accepting players.
func (s *GameSession) AddPlayer(player PlayerSession) error {
	if s.Status != StatusCreated && s.Status != StatusWaiting {
		return apperrors.Conflict("session is not accepting players")
	}
	for _, p := range s.Players {
		if p.TelegramUserID == player.TelegramUserID {
			return nil // idempotent re-join
		}
	}
	s.Players = append(s.Players, player)
	return nil
}

// Start transitions the session WAITING → IN_PROGRESS.
func (s *GameSession) Start() error {
	if s.Status != StatusWaiting {
		return apperrors.Conflict("session must be in WAITING status to start")
	}
	now := time.Now().UTC()
	s.Status = StatusInProgress
	s.StartedAt = &now
	return nil
}

// Finish transitions IN_PROGRESS → FINISHED and records final scores.
// scores maps TelegramUserID → score.
func (s *GameSession) Finish(scores map[int64]int) error {
	if s.Status != StatusInProgress {
		return apperrors.Conflict("session must be IN_PROGRESS to finish")
	}
	now := time.Now().UTC()
	s.Status = StatusFinished
	s.EndedAt = &now
	for i, p := range s.Players {
		if score, ok := scores[p.TelegramUserID]; ok {
			s.Players[i].Score = score
		}
	}
	return nil
}

// MarkWinner marks a player as a winner by TelegramUserID.
func (s *GameSession) MarkWinner(telegramUserID int64) {
	for i, p := range s.Players {
		if p.TelegramUserID == telegramUserID {
			s.Players[i].IsWinner = true
			return
		}
	}
}

// Archive transitions FINISHED → ARCHIVED.
func (s *GameSession) Archive() error {
	if s.Status != StatusFinished {
		return apperrors.Conflict("session must be FINISHED to archive")
	}
	s.Status = StatusArchived
	return nil
}

// HostPlayerID returns the TelegramUserID of the first player (the host).
// Returns 0 if no players have joined.
func (s *GameSession) HostPlayerID() int64 {
	if len(s.Players) == 0 {
		return 0
	}
	return s.Players[0].TelegramUserID
}

// IsActive returns true when the session can still accept players or moves.
func (s *GameSession) IsActive() bool {
	switch s.Status {
	case StatusCreated, StatusWaiting, StatusInProgress:
		return true
	}
	return false
}
