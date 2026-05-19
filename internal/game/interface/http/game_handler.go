package http

import (
	"context"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

// GameService is the minimal interface consumed by game handlers.
type GameService interface {
	ListGames(ctx context.Context) ([]*gamedomain.Game, error)
	GetGame(ctx context.Context, id uuid.UUID) (*gamedomain.Game, error)
}

// GameHandler wires game-catalog endpoints.
type GameHandler struct {
	svc GameService
}

func NewGameHandler(svc GameService) *GameHandler {
	return &GameHandler{svc: svc}
}

// RegisterRoutes mounts game endpoints (accessible by AdminApiKey OR BotApiKey).
func (h *GameHandler) RegisterRoutes(r fiber.Router) {
	r.Get("/games", h.ListGames)
	r.Get("/games/:game_id", h.GetGame)
}

// ListGames godoc — GET /api/v1/games
func (h *GameHandler) ListGames(c *fiber.Ctx) error {
	games, err := h.svc.ListGames(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(response.Success(fiber.Map{"games": games}, nil))
}

// GetGame godoc — GET /api/v1/games/:game_id
func (h *GameHandler) GetGame(c *fiber.Ctx) error {
	id, parseErr := uuid.Parse(c.Params("game_id"))
	if parseErr != nil {
		return apperrors.Validation("invalid UUID: game_id")
	}

	game, err := h.svc.GetGame(c.Context(), id)
	if err != nil {
		return err
	}
	return c.Status(http.StatusOK).JSON(response.Success(game, nil))
}
