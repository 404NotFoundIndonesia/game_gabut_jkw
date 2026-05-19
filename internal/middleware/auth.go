package middleware

import (
	"context"
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

type contextKeyBot struct{}

// BotFromContext returns the authenticated *Bot injected by BotAuth middleware.
func BotFromContext(ctx context.Context) (*domain.Bot, bool) {
	b, ok := ctx.Value(contextKeyBot{}).(*domain.Bot)
	return b, ok
}

// BotTokenLookup is the minimal interface needed by BotAuth to resolve a token.
type BotTokenLookup interface {
	FindByTokenHash(ctx context.Context, tokenHash string) (*domain.Bot, error)
}

// AdminAuth validates Authorization: Bearer <ADMIN_API_KEY> using constant-time compare.
func AdminAuth(adminKey string) fiber.Handler {
	keyBytes := []byte(adminKey)
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" {
			return c.Status(apperrors.Unauthorized("missing Authorization header").HTTPStatus).
				JSON(response.Error(apperrors.Unauthorized("missing Authorization header")))
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return c.Status(apperrors.Unauthorized("invalid Authorization format").HTTPStatus).
				JSON(response.Error(apperrors.Unauthorized("invalid Authorization format")))
		}

		provided := []byte(parts[1])
		if subtle.ConstantTimeCompare(provided, keyBytes) != 1 {
			return c.Status(apperrors.Unauthorized("invalid API key").HTTPStatus).
				JSON(response.Error(apperrors.Unauthorized("invalid API key")))
		}

		return c.Next()
	}
}

// BotAuth validates X-Bot-Token, looks up the bot in the repository,
// and injects it into the request context.
func BotAuth(repo BotTokenLookup, tokenHasher func(raw string) string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := c.Get("X-Bot-Token")
		if raw == "" {
			return c.Status(apperrors.Unauthorized("missing X-Bot-Token header").HTTPStatus).
				JSON(response.Error(apperrors.Unauthorized("missing X-Bot-Token header")))
		}

		hash := tokenHasher(raw)
		bot, err := repo.FindByTokenHash(c.Context(), hash)
		if err != nil {
			return c.Status(apperrors.Unauthorized("invalid bot token").HTTPStatus).
				JSON(response.Error(apperrors.Unauthorized("invalid bot token")))
		}

		if !bot.Active {
			return c.Status(apperrors.Unauthorized("bot is inactive").HTTPStatus).
				JSON(response.Error(apperrors.Unauthorized("bot is inactive")))
		}

		ctx := context.WithValue(c.Context(), contextKeyBot{}, bot)
		c.SetUserContext(ctx)
		return c.Next()
	}
}
