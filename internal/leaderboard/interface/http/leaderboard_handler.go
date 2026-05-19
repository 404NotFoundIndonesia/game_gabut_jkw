package http

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

// LeaderboardService is the minimal interface consumed by leaderboard handlers.
type LeaderboardService interface {
	GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error)
	GetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error)
	GetGlobal(ctx context.Context, params pagination.Params) (*domain.Leaderboard, error)
	GetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error)
}

// LeaderboardHandler wires leaderboard read endpoints.
type LeaderboardHandler struct {
	svc LeaderboardService
}

func NewLeaderboardHandler(svc LeaderboardService) *LeaderboardHandler {
	return &LeaderboardHandler{svc: svc}
}

// RegisterRoutes mounts leaderboard endpoints (AdminApiKey OR BotApiKey).
func (h *LeaderboardHandler) RegisterRoutes(r fiber.Router) {
	r.Get("/leaderboard", h.GetGlobal)
	r.Get("/leaderboard/:game_id", h.GetGlobalByGame)
	r.Get("/bots/:bot_id/leaderboard", h.GetByBot)
	r.Get("/bots/:bot_id/leaderboard/:game_id", h.GetByBotAndGame)
}

// GetGlobal godoc — GET /api/v1/leaderboard
func (h *LeaderboardHandler) GetGlobal(c *fiber.Ctx) error {
	params, err := pagination.ParseFromQuery(c.Query("limit"), c.Query("offset"))
	if err != nil {
		return err
	}
	lb, svcErr := h.svc.GetGlobal(c.Context(), params)
	if svcErr != nil {
		return svcErr
	}
	meta := pagination.NewMeta(lb.Total, params)
	return c.JSON(response.Success(fiber.Map{"leaderboard": lb.Entries}, &meta))
}

// GetGlobalByGame godoc — GET /api/v1/leaderboard/games/:game_id
func (h *LeaderboardHandler) GetGlobalByGame(c *fiber.Ctx) error {
	gameID, parseErr := uuid.Parse(c.Params("game_id"))
	if parseErr != nil {
		return apperrors.Validation("invalid UUID: game_id")
	}
	params, err := pagination.ParseFromQuery(c.Query("limit"), c.Query("offset"))
	if err != nil {
		return err
	}
	lb, svcErr := h.svc.GetGlobalByGame(c.Context(), gameID, params)
	if svcErr != nil {
		return svcErr
	}
	meta := pagination.NewMeta(lb.Total, params)
	return c.JSON(response.Success(fiber.Map{"leaderboard": lb.Entries}, &meta))
}

// GetByBot godoc — GET /api/v1/bots/:bot_id/leaderboard
func (h *LeaderboardHandler) GetByBot(c *fiber.Ctx) error {
	botID, parseErr := uuid.Parse(c.Params("bot_id"))
	if parseErr != nil {
		return apperrors.Validation("invalid UUID: bot_id")
	}
	params, err := pagination.ParseFromQuery(c.Query("limit"), c.Query("offset"))
	if err != nil {
		return err
	}
	lb, svcErr := h.svc.GetByBot(c.Context(), botID, params)
	if svcErr != nil {
		return svcErr
	}
	meta := pagination.NewMeta(lb.Total, params)
	return c.JSON(response.Success(fiber.Map{"leaderboard": lb.Entries}, &meta))
}

// GetByBotAndGame godoc — GET /api/v1/bots/:bot_id/leaderboard/games/:game_id
func (h *LeaderboardHandler) GetByBotAndGame(c *fiber.Ctx) error {
	botID, parseErr := uuid.Parse(c.Params("bot_id"))
	if parseErr != nil {
		return apperrors.Validation("invalid UUID: bot_id")
	}
	gameID, parseErr := uuid.Parse(c.Params("game_id"))
	if parseErr != nil {
		return apperrors.Validation("invalid UUID: game_id")
	}
	params, err := pagination.ParseFromQuery(c.Query("limit"), c.Query("offset"))
	if err != nil {
		return err
	}
	lb, svcErr := h.svc.GetByBotAndGame(c.Context(), botID, gameID, params)
	if svcErr != nil {
		return svcErr
	}
	meta := pagination.NewMeta(lb.Total, params)
	return c.JSON(response.Success(fiber.Map{"leaderboard": lb.Entries}, &meta))
}
