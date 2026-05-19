package domain

import (
	"time"

	"github.com/google/uuid"
)

// BotToken wraps the AES-256-GCM encrypted Telegram bot token.
// It never exposes its value in JSON or string representations.
type BotToken struct {
	ciphertext string
}

// NewBotToken constructs a BotToken from an already-encrypted ciphertext.
func NewBotToken(ciphertext string) BotToken {
	return BotToken{ciphertext: ciphertext}
}

// Ciphertext returns the raw encrypted string for DB persistence.
func (t BotToken) Ciphertext() string {
	return t.ciphertext
}

// MarshalJSON always returns the literal "[REDACTED]" — token never leaks via JSON.
func (t BotToken) MarshalJSON() ([]byte, error) {
	return []byte(`"[REDACTED]"`), nil
}

// String returns "[REDACTED]" so the token cannot leak via fmt or logging.
func (t BotToken) String() string {
	return "[REDACTED]"
}

// Bot is the aggregate root for the bot management domain.
type Bot struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Token      BotToken  `json:"token"`
	TokenHash  string    `json:"-"` // SHA-256 of raw token for O(1) BotAuth lookup
	TelegramID int64     `json:"telegram_id"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// NewBot creates a new, active bot with a generated UUID.
func NewBot(name string, token BotToken, tokenHash string, telegramID int64) *Bot {
	now := time.Now().UTC()
	return &Bot{
		ID:         uuid.New(),
		Name:       name,
		Token:      token,
		TokenHash:  tokenHash,
		TelegramID: telegramID,
		Active:     true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Deactivate sets the bot as inactive.
func (b *Bot) Deactivate() {
	b.Active = false
	b.UpdatedAt = time.Now().UTC()
}

// Activate sets the bot as active.
func (b *Bot) Activate() {
	b.Active = true
	b.UpdatedAt = time.Now().UTC()
}

// RotateToken replaces the encrypted token and its lookup hash.
func (b *Bot) RotateToken(newToken BotToken, newTokenHash string) {
	b.Token = newToken
	b.TokenHash = newTokenHash
	b.UpdatedAt = time.Now().UTC()
}

// UpdateName sets a new display name for the bot.
func (b *Bot) UpdateName(name string) {
	b.Name = name
	b.UpdatedAt = time.Now().UTC()
}
