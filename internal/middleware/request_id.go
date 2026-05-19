package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/pkg/logger"
)

const HeaderRequestID = "X-Request-ID"

// RequestID injects a UUID into each request context and response header.
// The logger stored in ctx is enriched with the request_id field.
func RequestID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Get(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(HeaderRequestID, id)

		// Enrich the context logger with the request ID.
		log := logger.FromContext(c.Context()).With("request_id", id)
		ctx := logger.WithContext(c.Context(), log)
		c.SetUserContext(ctx)

		return c.Next()
	}
}
