package http

import (
	"context"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	"github.com/404NFIDv2/bot-game-management/internal/session/application"
	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
	"github.com/404NFIDv2/bot-game-management/pkg/sanitize"
	"github.com/404NFIDv2/bot-game-management/pkg/validator"
)

// SessionService is the minimal interface consumed by session handlers.
type SessionService interface {
	CreateSession(ctx context.Context, botID uuid.UUID, req application.CreateSessionRequest) (*domain.GameSession, error)
	JoinSession(ctx context.Context, botID, sessionID uuid.UUID, req application.JoinRequest) (*domain.GameSession, error)
	StartSession(ctx context.Context, botID, sessionID uuid.UUID, callerTelegramID int64) (*domain.GameSession, error)
	SubmitMove(ctx context.Context, botID, sessionID uuid.UUID, req application.MoveRequest) (*application.MoveResult, error)
	EndSession(ctx context.Context, botID, sessionID uuid.UUID, req application.EndSessionRequest) (*domain.GameSession, error)
	GetSession(ctx context.Context, botID, sessionID uuid.UUID) (*domain.GameSession, error)
	ListSessions(ctx context.Context, botID uuid.UUID, filter domain.SessionFilter, limit, offset int) ([]*domain.GameSession, int, error)
}

// SessionHandler wires all session lifecycle endpoints.
type SessionHandler struct {
	svc SessionService
}

func NewSessionHandler(svc SessionService) *SessionHandler {
	return &SessionHandler{svc: svc}
}

// RegisterRoutes mounts session endpoints on the given router.
// All routes expect the caller to already be authenticated (AdminAuth OR BotAuth).
func (h *SessionHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/bots/:bot_id/sessions", h.CreateSession)
	r.Get("/bots/:bot_id/sessions", h.ListSessions)
	r.Get("/bots/:bot_id/sessions/:session_id", h.GetSession)
	r.Post("/bots/:bot_id/sessions/:session_id/join", h.JoinSession)
	r.Post("/bots/:bot_id/sessions/:session_id/start", h.StartSession)
	r.Post("/bots/:bot_id/sessions/:session_id/move", h.SubmitMove)
	r.Post("/bots/:bot_id/sessions/:session_id/end", h.EndSession)
}

// ─── request bodies ───────────────────────────────────────────────────────────

type createSessionBody struct {
	GameID  string          `json:"game_id"  validate:"required,uuid"`
	ChatID  int64           `json:"chat_id"  validate:"required"`
	Player  playerBody      `json:"player"   validate:"required"`
}

type playerBody struct {
	TelegramUserID int64  `json:"telegram_user_id" validate:"required"`
	DisplayName    string `json:"display_name"     validate:"required,min=1,max=100"`
}

type joinBody struct {
	TelegramUserID int64  `json:"telegram_user_id" validate:"required"`
	DisplayName    string `json:"display_name"     validate:"required,min=1,max=100"`
}

type startBody struct {
	TelegramUserID int64 `json:"telegram_user_id" validate:"required"`
}

type moveBody struct {
	PlayerID int64          `json:"player_id" validate:"required"`
	Payload  map[string]any `json:"payload"`
}

type endBody struct {
	TelegramUserID int64  `json:"telegram_user_id"`
	Reason         string `json:"reason"`
}

// ─── handlers ─────────────────────────────────────────────────────────────────

// CreateSession godoc — POST /api/v1/bots/:bot_id/sessions (BotApiKey only)
func (h *SessionHandler) CreateSession(c *fiber.Ctx) error {
	// Only bot API key holders can create sessions.
	if _, ok := middleware.BotFromContext(c.Context()); !ok {
		return apperrors.Forbidden("this endpoint requires bot API key authentication")
	}

	botID, err := parseUUIDParam(c, "bot_id")
	if err != nil {
		return err
	}

	var body createSessionBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Validation("invalid request body")
	}
	if err := validator.Struct(body); err != nil {
		return err
	}

	gameID, _ := uuid.Parse(body.GameID)
	session, svcErr := h.svc.CreateSession(c.Context(), botID, application.CreateSessionRequest{
		GameID:         gameID,
		ChatID:         body.ChatID,
		TelegramUserID: body.Player.TelegramUserID,
		DisplayName:    sanitize.DisplayName(body.Player.DisplayName),
	})
	if svcErr != nil {
		return svcErr
	}
	return c.Status(http.StatusCreated).JSON(response.Success(session, nil))
}

// ListSessions godoc — GET /api/v1/bots/:bot_id/sessions (AdminApiKey | BotApiKey)
func (h *SessionHandler) ListSessions(c *fiber.Ctx) error {
	botID, err := parseUUIDParam(c, "bot_id")
	if err != nil {
		return err
	}

	params, pErr := pagination.ParseFromQuery(c.Query("limit"), c.Query("offset"))
	if pErr != nil {
		return pErr
	}

	filter := domain.SessionFilter{}
	if raw := c.Query("status"); raw != "" {
		s := domain.SessionStatus(raw)
		filter.Status = &s
	}
	if raw := c.Query("game_id"); raw != "" {
		gid, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return apperrors.Validation("invalid game_id query param")
		}
		filter.GameID = &gid
	}

	sessions, total, svcErr := h.svc.ListSessions(c.Context(), botID, filter, params.Limit, params.Offset)
	if svcErr != nil {
		return svcErr
	}
	meta := pagination.NewMeta(total, params)
	return c.JSON(response.Success(fiber.Map{"sessions": sessions}, &meta))
}

// GetSession godoc — GET /api/v1/bots/:bot_id/sessions/:session_id (AdminApiKey | BotApiKey)
func (h *SessionHandler) GetSession(c *fiber.Ctx) error {
	botID, err := parseUUIDParam(c, "bot_id")
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "session_id")
	if err != nil {
		return err
	}

	session, svcErr := h.svc.GetSession(c.Context(), botID, sessionID)
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(session, nil))
}

// JoinSession godoc — POST /api/v1/bots/:bot_id/sessions/:session_id/join (BotApiKey only)
func (h *SessionHandler) JoinSession(c *fiber.Ctx) error {
	if _, ok := middleware.BotFromContext(c.Context()); !ok {
		return apperrors.Forbidden("this endpoint requires bot API key authentication")
	}

	botID, err := parseUUIDParam(c, "bot_id")
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "session_id")
	if err != nil {
		return err
	}

	var body joinBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Validation("invalid request body")
	}
	if err := validator.Struct(body); err != nil {
		return err
	}

	session, svcErr := h.svc.JoinSession(c.Context(), botID, sessionID, application.JoinRequest{
		TelegramUserID: body.TelegramUserID,
		DisplayName:    sanitize.DisplayName(body.DisplayName),
	})
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(session, nil))
}

// StartSession godoc — POST /api/v1/bots/:bot_id/sessions/:session_id/start (BotApiKey only)
func (h *SessionHandler) StartSession(c *fiber.Ctx) error {
	if _, ok := middleware.BotFromContext(c.Context()); !ok {
		return apperrors.Forbidden("this endpoint requires bot API key authentication")
	}

	botID, err := parseUUIDParam(c, "bot_id")
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "session_id")
	if err != nil {
		return err
	}

	var body startBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Validation("invalid request body")
	}
	if err := validator.Struct(body); err != nil {
		return err
	}

	session, svcErr := h.svc.StartSession(c.Context(), botID, sessionID, body.TelegramUserID)
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(session, nil))
}

// SubmitMove godoc — POST /api/v1/bots/:bot_id/sessions/:session_id/move (BotApiKey only)
func (h *SessionHandler) SubmitMove(c *fiber.Ctx) error {
	if _, ok := middleware.BotFromContext(c.Context()); !ok {
		return apperrors.Forbidden("this endpoint requires bot API key authentication")
	}

	botID, err := parseUUIDParam(c, "bot_id")
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "session_id")
	if err != nil {
		return err
	}

	var body moveBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Validation("invalid request body")
	}
	if err := validator.Struct(body); err != nil {
		return err
	}

	result, svcErr := h.svc.SubmitMove(c.Context(), botID, sessionID, application.MoveRequest{
		PlayerID: body.PlayerID,
		Payload:  body.Payload,
	})
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(fiber.Map{
		"session": result.Session,
		"events":  result.Events,
	}, nil))
}

// EndSession godoc — POST /api/v1/bots/:bot_id/sessions/:session_id/end (AdminApiKey | BotApiKey)
func (h *SessionHandler) EndSession(c *fiber.Ctx) error {
	botID, err := parseUUIDParam(c, "bot_id")
	if err != nil {
		return err
	}
	sessionID, err := parseUUIDParam(c, "session_id")
	if err != nil {
		return err
	}

	var body endBody
	if err := c.BodyParser(&body); err != nil {
		return apperrors.Validation("invalid request body")
	}

	_, isBotAuth := middleware.BotFromContext(c.Context())
	req := application.EndSessionRequest{
		CallerTelegramID: body.TelegramUserID,
		IsAdmin:          !isBotAuth,
		Reason:           sanitize.Reason(body.Reason),
	}

	session, svcErr := h.svc.EndSession(c.Context(), botID, sessionID, req)
	if svcErr != nil {
		return svcErr
	}
	return c.JSON(response.Success(session, nil))
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func parseUUIDParam(c *fiber.Ctx, name string) (uuid.UUID, *apperrors.AppError) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.UUID{}, apperrors.Validation("invalid UUID: " + name)
	}
	return id, nil
}
