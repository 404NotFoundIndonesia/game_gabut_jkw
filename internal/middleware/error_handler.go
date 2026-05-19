package middleware

import (
	"github.com/gofiber/fiber/v2"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

// ErrorHandler is the global Fiber error handler.
// It maps *AppError to the correct HTTP status and envelope shape.
// Unrecognised errors become 500 Internal Server Error.
func ErrorHandler(c *fiber.Ctx, err error) error {
	ae := apperrors.AsAppError(err)
	return c.Status(ae.HTTPStatus).JSON(response.Error(ae))
}
