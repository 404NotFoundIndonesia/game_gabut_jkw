package http

import (
	"context"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/bot/application"
	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
	"github.com/404NFIDv2/bot-game-management/pkg/validator"
)

// BotService is the application-layer interface consumed by the handler.
// Defined here to avoid importing the application package from outside.
type BotService interface {
	RegisterBot(ctx context.Context, name, rawToken string) (*domain.Bot, error)
	ListBots(ctx context.Context, filter domain.BotFilter, limit, offset int) ([]*domain.Bot, int, error)
	GetBot(ctx context.Context, id uuid.UUID) (*domain.Bot, error)
	UpdateBot(ctx context.Context, id uuid.UUID, patch application.UpdateBotPatch) (*domain.Bot, error)
	DeleteBot(ctx context.Context, id uuid.UUID) error
}

// BotHandler wires bot endpoints onto a Fiber router.
type BotHandler struct {
	svc BotService
}

func NewBotHandler(svc BotService) *BotHandler {
	return &BotHandler{svc: svc}
}

// RegisterRoutes mounts all bot routes under the given group (should already have AdminAuth applied).
func (h *BotHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/bots", h.CreateBot)
	r.Get("/bots", h.ListBots)
	r.Get("/bots/:bot_id", h.GetBot)
	r.Patch("/bots/:bot_id", h.UpdateBot)
	r.Delete("/bots/:bot_id", h.DeleteBot)
}

// ── request/response DTOs ──────────────────────────────────────────────────

type createBotRequest struct {
	Name  string `json:"name"  validate:"required,min=1,max=100"`
	Token string `json:"token" validate:"required,min=10"`
}

type updateBotRequest struct {
	Name   *string `json:"name"`
	Active *bool   `json:"active"`
	Token  *string `json:"token"`
}

// ── handlers ──────────────────────────────────────────────────────────────────

// CreateBot godoc — POST /api/v1/bots
func (h *BotHandler) CreateBot(c *fiber.Ctx) error {
	var req createBotRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}
	if err := validator.Struct(req); err != nil {
		return err
	}

	bot, err := h.svc.RegisterBot(c.Context(), req.Name, req.Token)
	if err != nil {
		return err
	}
	return c.Status(http.StatusCreated).JSON(response.Success(bot, nil))
}

// ListBots godoc — GET /api/v1/bots
func (h *BotHandler) ListBots(c *fiber.Ctx) error {
	page, err := pagination.ParseFromQuery(c.Query("limit"), c.Query("offset"))
	if err != nil {
		return err
	}

	var filter domain.BotFilter
	if activeStr := c.Query("active"); activeStr != "" {
		v := activeStr == "true"
		filter.Active = &v
	}

	bots, total, svcErr := h.svc.ListBots(c.Context(), filter, page.Limit, page.Offset)
	if svcErr != nil {
		return svcErr
	}

	meta := pagination.NewMeta(total, page)
	return c.JSON(response.Success(fiber.Map{"bots": bots}, &meta))
}

// GetBot godoc — GET /api/v1/bots/:bot_id
func (h *BotHandler) GetBot(c *fiber.Ctx) error {
	id, err := parseUUID(c, "bot_id")
	if err != nil {
		return err
	}

	bot, svcErr := h.svc.GetBot(c.Context(), id)
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(bot, nil))
}

// UpdateBot godoc — PATCH /api/v1/bots/:bot_id
func (h *BotHandler) UpdateBot(c *fiber.Ctx) error {
	id, err := parseUUID(c, "bot_id")
	if err != nil {
		return err
	}

	var req updateBotRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	if req.Name == nil && req.Active == nil && req.Token == nil {
		return apperrors.Validation("at least one field must be provided")
	}

	patch := application.UpdateBotPatch{
		Name:     req.Name,
		Active:   req.Active,
		RawToken: req.Token,
	}

	bot, svcErr := h.svc.UpdateBot(c.Context(), id, patch)
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(bot, nil))
}

// DeleteBot godoc — DELETE /api/v1/bots/:bot_id
func (h *BotHandler) DeleteBot(c *fiber.Ctx) error {
	id, err := parseUUID(c, "bot_id")
	if err != nil {
		return err
	}

	if svcErr := h.svc.DeleteBot(c.Context(), id); svcErr != nil {
		return svcErr
	}
	return c.SendStatus(http.StatusNoContent)
}

// parseUUID extracts and validates a UUID path parameter.
func parseUUID(c *fiber.Ctx, param string) (uuid.UUID, *apperrors.AppError) {
	id, err := uuid.Parse(c.Params(param))
	if err != nil {
		return uuid.UUID{}, apperrors.Validation("invalid UUID: " + param)
	}
	return id, nil
}
