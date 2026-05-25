package middleware

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

// WebhookSecret validates the X-Telegram-Bot-Api-Secret-Token header using
// constant-time comparison. Applied only to /telegram/* routes.
func WebhookSecret(secret string) fiber.Handler {
	secretBytes := []byte(secret)
	return func(c *fiber.Ctx) error {
		provided := []byte(c.Get("X-Telegram-Bot-Api-Secret-Token"))
		if len(provided) == 0 || subtle.ConstantTimeCompare(provided, secretBytes) != 1 {
			err := apperrors.Unauthorized("invalid webhook secret")
			return c.Status(err.HTTPStatus).JSON(response.Error(err))
		}
		return c.Next()
	}
}
