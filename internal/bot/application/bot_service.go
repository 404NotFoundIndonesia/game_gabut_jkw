package application

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/crypto"
)

// SessionEnder is a thin dependency on the session domain so we can force-end
// active sessions when a bot is deleted. Implemented as no-op until Phase 3.
type SessionEnder interface {
	ForceEndBotSessions(ctx context.Context, botID uuid.UUID) error
}

// noopSessionEnder is used before Phase 3 is wired.
type noopSessionEnder struct{}

func (n *noopSessionEnder) ForceEndBotSessions(_ context.Context, _ uuid.UUID) error { return nil }

// NewNoopSessionEnder returns a no-op SessionEnder for use before Phase 3.
func NewNoopSessionEnder() SessionEnder { return &noopSessionEnder{} }

// UpdateBotPatch holds the optional fields for a partial bot update.
type UpdateBotPatch struct {
	Name     *string
	Active   *bool
	RawToken *string // when set, token is rotated
}

// BotService implements all bot management use cases.
type BotService struct {
	repo         domain.BotRepository
	tgClient     telegram.Client
	cryptoKey    []byte
	sessionEnder SessionEnder
}

// NewBotService constructs a BotService.
// cryptoKeyStr is the BOT_TOKEN_ENCRYPTION_KEY config value.
func NewBotService(
	repo domain.BotRepository,
	tgClient telegram.Client,
	cryptoKeyStr string,
	sessionEnder SessionEnder,
) *BotService {
	return &BotService{
		repo:         repo,
		tgClient:     tgClient,
		cryptoKey:    crypto.PadKey(cryptoKeyStr),
		sessionEnder: sessionEnder,
	}
}

// RegisterBot encrypts the token, verifies it with Telegram, and persists a new bot.
func (s *BotService) RegisterBot(ctx context.Context, name, rawToken string) (*domain.Bot, error) {
	if name == "" {
		return nil, apperrors.Validation("name is required")
	}
	if rawToken == "" {
		return nil, apperrors.Validation("token is required")
	}

	// Resolve Telegram bot identity — fails fast if token is invalid.
	telegramID, err := s.tgClient.GetBotID(ctx, rawToken)
	if err != nil {
		return nil, apperrors.Validation("invalid Telegram bot token: " + err.Error())
	}

	// Duplicate check.
	if _, err := s.repo.FindByTelegramID(ctx, telegramID); err == nil {
		return nil, apperrors.Conflict("a bot with this Telegram ID is already registered")
	}

	// Encrypt token for storage.
	ciphertext, err := crypto.Encrypt(s.cryptoKey, rawToken)
	if err != nil {
		return nil, apperrors.Internal("failed to encrypt bot token").WithCause(err)
	}

	tokenHash := hashToken(rawToken)
	bot := domain.NewBot(name, domain.NewBotToken(ciphertext), tokenHash, telegramID)

	if err := s.repo.Save(ctx, bot); err != nil {
		return nil, err
	}
	return bot, nil
}

// ListBots returns a page of bots.
func (s *BotService) ListBots(
	ctx context.Context, filter domain.BotFilter, limit, offset int,
) ([]*domain.Bot, int, error) {
	return s.repo.FindAll(ctx, filter, limit, offset)
}

// GetBot returns a single bot or NotFound.
func (s *BotService) GetBot(ctx context.Context, id uuid.UUID) (*domain.Bot, error) {
	return s.repo.FindByID(ctx, id)
}

// UpdateBot applies a partial patch to an existing bot.
func (s *BotService) UpdateBot(ctx context.Context, id uuid.UUID, patch UpdateBotPatch) (*domain.Bot, error) {
	bot, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if patch.Name != nil {
		if *patch.Name == "" {
			return nil, apperrors.Validation("name must not be empty")
		}
		bot.UpdateName(*patch.Name)
	}

	if patch.Active != nil {
		if *patch.Active {
			bot.Activate()
		} else {
			bot.Deactivate()
		}
	}

	if patch.RawToken != nil {
		if *patch.RawToken == "" {
			return nil, apperrors.Validation("token must not be empty")
		}
		ciphertext, err := crypto.Encrypt(s.cryptoKey, *patch.RawToken)
		if err != nil {
			return nil, apperrors.Internal("failed to encrypt bot token").WithCause(err)
		}
		tokenHash := hashToken(*patch.RawToken)
		bot.RotateToken(domain.NewBotToken(ciphertext), tokenHash)
	}

	if err := s.repo.Save(ctx, bot); err != nil {
		return nil, err
	}
	return bot, nil
}

// DeleteBot force-ends all active sessions, then deactivates and removes the bot.
func (s *BotService) DeleteBot(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return err
	}

	// Phase 3 will provide a real implementation.
	if err := s.sessionEnder.ForceEndBotSessions(ctx, id); err != nil {
		return apperrors.Internal("failed to end bot sessions").WithCause(err)
	}

	return s.repo.Delete(ctx, id)
}

// hashToken returns the SHA-256 hex digest of a raw bot token.
func hashToken(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return fmt.Sprintf("%x", h)
}
