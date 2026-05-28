package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"

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
	repo           domain.BotRepository
	tgClient       telegram.Client
	cryptoKey      []byte
	sessionEnder   SessionEnder
	webhookBaseURL string
	webhookSecret  string
}

// NewBotService constructs a BotService.
// cryptoKeyStr is the BOT_TOKEN_ENCRYPTION_KEY config value.
// webhookBaseURL and webhookSecret are used by the webhook-aware registration methods.
func NewBotService(
	repo domain.BotRepository,
	tgClient telegram.Client,
	cryptoKeyStr string,
	sessionEnder SessionEnder,
	webhookBaseURL string,
	webhookSecret string,
) *BotService {
	return &BotService{
		repo:           repo,
		tgClient:       tgClient,
		cryptoKey:      crypto.PadKey(cryptoKeyStr),
		sessionEnder:   sessionEnder,
		webhookBaseURL: webhookBaseURL,
		webhookSecret:  webhookSecret,
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

// RegisterBotWithWebhook encrypts the token, verifies it with Telegram, persists the bot,
// and then calls Telegram setWebhook for the child bot's webhook URL.
// If setWebhook fails the bot record is removed and the error is returned.
func (s *BotService) RegisterBotWithWebhook(ctx context.Context, name, rawToken string) (*domain.Bot, error) {
	bot, err := s.RegisterBot(ctx, name, rawToken)
	if err != nil {
		return nil, err
	}

	webhookURL := s.childWebhookURL(bot.ID.String())
	if err := s.tgClient.SetWebhook(ctx, rawToken, webhookURL, s.webhookSecret); err != nil {
		// Best-effort rollback: remove the bot record so the DB stays consistent.
		_ = s.repo.Delete(ctx, bot.ID)
		return nil, apperrors.Internal("failed to register Telegram webhook: " + err.Error())
	}

	// Best-effort: register bot commands with BotFather.
	if err := s.tgClient.SetCommands(ctx, rawToken, childBotCommands()); err != nil {
		slog.Warn("RegisterBotWithWebhook: failed to set bot commands", "bot_id", bot.ID, "err", err)
	}

	return bot, nil
}

// DeleteBotWithWebhook calls Telegram deleteWebhook (best-effort), then deactivates the bot.
// Deactivation always proceeds even if the Telegram call fails.
func (s *BotService) DeleteBotWithWebhook(ctx context.Context, id uuid.UUID) error {
	bot, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	rawToken, err := crypto.Decrypt(s.cryptoKey, bot.Token.Ciphertext())
	if err == nil {
		// Best-effort: ignore Telegram errors so deactivation always completes.
		_ = s.tgClient.DeleteWebhook(ctx, rawToken)
	}

	if err := s.sessionEnder.ForceEndBotSessions(ctx, id); err != nil {
		return apperrors.Internal("failed to end bot sessions").WithCause(err)
	}
	return s.repo.Delete(ctx, id)
}

// ReactivateBotWithWebhook sets the bot active and re-registers its Telegram webhook.
// If setWebhook fails the bot is reverted to inactive.
func (s *BotService) ReactivateBotWithWebhook(ctx context.Context, id uuid.UUID) (*domain.Bot, error) {
	bot, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if bot.Active {
		return nil, apperrors.Conflict("bot is already active")
	}

	bot.Activate()
	if err := s.repo.Save(ctx, bot); err != nil {
		return nil, err
	}

	rawToken, err := crypto.Decrypt(s.cryptoKey, bot.Token.Ciphertext())
	if err != nil {
		return nil, apperrors.Internal("failed to decrypt bot token").WithCause(err)
	}

	webhookURL := s.childWebhookURL(bot.ID.String())
	if err := s.tgClient.SetWebhook(ctx, rawToken, webhookURL, s.webhookSecret); err != nil {
		// Revert activation on webhook failure.
		bot.Deactivate()
		_ = s.repo.Save(ctx, bot)
		return nil, apperrors.Internal("failed to re-register Telegram webhook: " + err.Error())
	}

	// Best-effort: restore bot commands with BotFather.
	if err := s.tgClient.SetCommands(ctx, rawToken, childBotCommands()); err != nil {
		slog.Warn("ReactivateBotWithWebhook: failed to set bot commands", "bot_id", bot.ID, "err", err)
	}

	return bot, nil
}

// ReregisterAllChildWebhooks re-registers webhooks for all active child bots.
// Called at startup to recover from missed webhook registrations.
func (s *BotService) ReregisterAllChildWebhooks(ctx context.Context) {
	active := true
	bots, _, err := s.repo.FindAll(ctx, domain.BotFilter{Active: &active}, 1000, 0)
	if err != nil {
		slog.Error("ReregisterAllChildWebhooks: failed to list bots", "err", err)
		return
	}
	for _, bot := range bots {
		rawToken, err := crypto.Decrypt(s.cryptoKey, bot.Token.Ciphertext())
		if err != nil {
			slog.Error("ReregisterAllChildWebhooks: failed to decrypt token", "bot_id", bot.ID, "err", err)
			continue
		}
		webhookURL := s.childWebhookURL(bot.ID.String())
		if err := s.tgClient.SetWebhook(ctx, rawToken, webhookURL, s.webhookSecret); err != nil {
			slog.Error("ReregisterAllChildWebhooks: failed to set webhook", "bot_id", bot.ID, "err", err)
		} else {
			slog.Info("ReregisterAllChildWebhooks: webhook set", "bot_id", bot.ID)
		}
		// Best-effort command list sync.
		if err := s.tgClient.SetCommands(ctx, rawToken, childBotCommands()); err != nil {
			slog.Warn("ReregisterAllChildWebhooks: failed to set commands", "bot_id", bot.ID, "err", err)
		}
	}
}

func (s *BotService) childWebhookURL(botID string) string {
	return s.webhookBaseURL + "/telegram/child/" + botID + "/webhook"
}

// childBotCommands returns the standard command list registered with BotFather for child bots.
func childBotCommands() []telegram.BotCommand {
	return []telegram.BotCommand{
		{Command: "newgame", Description: "Start a new game (e.g. /newgame uno)"},
		{Command: "join", Description: "Join the active game in this chat"},
		{Command: "start", Description: "Start the game (host only)"},
		{Command: "move", Description: "Submit a move (e.g. /move draw or /move {\"action\":\"play\",\"card\":\"R5\"})"},
		{Command: "state", Description: "Show current game state and scores"},
		{Command: "end", Description: "End the current game"},
		{Command: "leaderboard", Description: "Show this bot's leaderboard"},
		{Command: "mystats", Description: "Show your personal stats"},
		{Command: "history", Description: "Show recent finished games in this chat"},
	}
}

// hashToken returns the SHA-256 hex digest of a raw bot token.
func hashToken(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return fmt.Sprintf("%x", h)
}
