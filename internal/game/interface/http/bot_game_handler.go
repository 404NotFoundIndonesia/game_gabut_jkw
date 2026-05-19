package http

import (
	"context"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
	"github.com/404NFIDv2/bot-game-management/pkg/validator"
)

// BotGameService is the minimal interface consumed by bot-game handlers.
type BotGameService interface {
	AssignGame(ctx context.Context, botID, gameID uuid.UUID) (*gamedomain.BotGame, error)
	RemoveGame(ctx context.Context, botID, gameID uuid.UUID) error
	ListBotGames(ctx context.Context, botID uuid.UUID) ([]*gamedomain.BotGame, error)
}

// BotGameHandler wires bot-game assignment endpoints.
type BotGameHandler struct {
	svc BotGameService
}

func NewBotGameHandler(svc BotGameService) *BotGameHandler {
	return &BotGameHandler{svc: svc}
}

// RegisterRoutes mounts bot-game endpoints on the given router.
// Assign and Remove require AdminApiKey; List accepts AdminApiKey or BotApiKey.
func (h *BotGameHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/bots/:bot_id/games", h.AssignGame)
	r.Delete("/bots/:bot_id/games/:game_id", h.RemoveGame)
	r.Get("/bots/:bot_id/games", h.ListBotGames)
}

type assignGameRequest struct {
	GameID string `json:"game_id" validate:"required,uuid"`
}

// AssignGame godoc — POST /api/v1/bots/:bot_id/games
func (h *BotGameHandler) AssignGame(c *fiber.Ctx) error {
	botID, err := parseParam(c, "bot_id")
	if err != nil {
		return err
	}

	var req assignGameRequest
	if parseErr := c.BodyParser(&req); parseErr != nil {
		return apperrors.Validation("invalid request body")
	}
	if valErr := validator.Struct(req); valErr != nil {
		return valErr
	}

	gameID, _ := uuid.Parse(req.GameID)
	bg, svcErr := h.svc.AssignGame(c.Context(), botID, gameID)
	if svcErr != nil {
		return svcErr
	}
	return c.Status(http.StatusOK).JSON(response.Success(bg, nil))
}

// RemoveGame godoc — DELETE /api/v1/bots/:bot_id/games/:game_id
func (h *BotGameHandler) RemoveGame(c *fiber.Ctx) error {
	botID, err := parseParam(c, "bot_id")
	if err != nil {
		return err
	}
	gameID, err := parseParam(c, "game_id")
	if err != nil {
		return err
	}

	if svcErr := h.svc.RemoveGame(c.Context(), botID, gameID); svcErr != nil {
		return svcErr
	}
	return c.SendStatus(http.StatusNoContent)
}

// ListBotGames godoc — GET /api/v1/bots/:bot_id/games
func (h *BotGameHandler) ListBotGames(c *fiber.Ctx) error {
	botID, err := parseParam(c, "bot_id")
	if err != nil {
		return err
	}

	bgs, svcErr := h.svc.ListBotGames(c.Context(), botID)
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(fiber.Map{"games": bgs}, nil))
}

func parseParam(c *fiber.Ctx, name string) (uuid.UUID, *apperrors.AppError) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.UUID{}, apperrors.Validation("invalid UUID: " + name)
	}
	return id, nil
}
