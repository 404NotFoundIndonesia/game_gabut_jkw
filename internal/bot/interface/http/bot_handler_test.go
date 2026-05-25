package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/bot/application"
	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	bothttp "github.com/404NFIDv2/bot-game-management/internal/bot/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── stub service ──────────────────────────────────────────────────────────────

type stubBotService struct {
	bot    *domain.Bot
	bots   []*domain.Bot
	total  int
	err    error
}

func (s *stubBotService) RegisterBot(_ context.Context, name, _ string) (*domain.Bot, error) {
	if s.err != nil {
		return nil, s.err
	}
	b := domain.NewBot(name, domain.NewBotToken("enc"), "hash", 111)
	return b, nil
}

func (s *stubBotService) ListBots(_ context.Context, _ domain.BotFilter, _, _ int) ([]*domain.Bot, int, error) {
	return s.bots, s.total, s.err
}

func (s *stubBotService) GetBot(_ context.Context, _ uuid.UUID) (*domain.Bot, error) {
	return s.bot, s.err
}

func (s *stubBotService) UpdateBot(_ context.Context, _ uuid.UUID, _ application.UpdateBotPatch) (*domain.Bot, error) {
	return s.bot, s.err
}

func (s *stubBotService) DeleteBot(_ context.Context, _ uuid.UUID) error {
	return s.err
}

// ── test helpers ──────────────────────────────────────────────────────────────

const adminKey = "test-admin-key"

func newTestApp(svc bothttp.BotService) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler,
	})
	api := app.Group("/api/v1", middleware.AdminAuth(adminKey))
	bothttp.NewBotHandler(svc).RegisterRoutes(api)
	return app
}

func do(t *testing.T, app *fiber.App, method, path string, body any, key string) (int, map[string]any) {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestCreateBot_Success(t *testing.T) {
	app := newTestApp(&stubBotService{})
	code, body := do(t, app, http.MethodPost, "/api/v1/bots",
		map[string]string{"name": "TestBot", "token": "1234567890:valid"},
		adminKey)
	if code != http.StatusCreated {
		t.Errorf("status: got %d, want 201 — body: %v", code, body)
	}
	if body["success"] != true {
		t.Error("expected success=true")
	}
}

func TestCreateBot_InvalidBody_MissingName(t *testing.T) {
	app := newTestApp(&stubBotService{})
	code, _ := do(t, app, http.MethodPost, "/api/v1/bots",
		map[string]string{"token": "1234567890:tok"},
		adminKey)
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestCreateBot_ServiceConflict(t *testing.T) {
	app := newTestApp(&stubBotService{err: apperrors.Conflict("dup")})
	code, _ := do(t, app, http.MethodPost, "/api/v1/bots",
		map[string]string{"name": "B", "token": "1234567890:tok"},
		adminKey)
	if code != http.StatusConflict {
		t.Errorf("expected 409, got %d", code)
	}
}

func TestCreateBot_NoAuth(t *testing.T) {
	app := newTestApp(&stubBotService{})
	code, _ := do(t, app, http.MethodPost, "/api/v1/bots",
		map[string]string{"name": "B", "token": "1234567890:tok"},
		"")
	if code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}

func TestListBots_Success(t *testing.T) {
	bot := domain.NewBot("B", domain.NewBotToken("e"), "h", 1)
	app := newTestApp(&stubBotService{bots: []*domain.Bot{bot}, total: 1})
	code, body := do(t, app, http.MethodGet, "/api/v1/bots", nil, adminKey)
	if code != http.StatusOK {
		t.Errorf("status: got %d", code)
	}
	data, _ := body["data"].(map[string]any)
	bots, _ := data["bots"].([]any)
	if len(bots) != 1 {
		t.Errorf("expected 1 bot in response, got %d", len(bots))
	}
}

func TestGetBot_NotFound(t *testing.T) {
	app := newTestApp(&stubBotService{err: apperrors.NotFound("not found")})
	id := uuid.New().String()
	code, _ := do(t, app, http.MethodGet, "/api/v1/bots/"+id, nil, adminKey)
	if code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", code)
	}
}

func TestGetBot_InvalidUUID(t *testing.T) {
	app := newTestApp(&stubBotService{})
	code, _ := do(t, app, http.MethodGet, "/api/v1/bots/not-a-uuid", nil, adminKey)
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestUpdateBot_Success(t *testing.T) {
	bot := domain.NewBot("B", domain.NewBotToken("e"), "h", 1)
	app := newTestApp(&stubBotService{bot: bot})
	id := uuid.New().String()
	code, _ := do(t, app, http.MethodPatch, "/api/v1/bots/"+id,
		map[string]string{"name": "New"}, adminKey)
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestUpdateBot_EmptyBody(t *testing.T) {
	app := newTestApp(&stubBotService{})
	id := uuid.New().String()
	code, _ := do(t, app, http.MethodPatch, "/api/v1/bots/"+id,
		map[string]any{}, adminKey)
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestDeleteBot_Success(t *testing.T) {
	app := newTestApp(&stubBotService{})
	id := uuid.New().String()
	code, _ := do(t, app, http.MethodDelete, "/api/v1/bots/"+id, nil, adminKey)
	if code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", code)
	}
}

func TestDeleteBot_NotFound(t *testing.T) {
	app := newTestApp(&stubBotService{err: apperrors.NotFound("not found")})
	id := uuid.New().String()
	code, _ := do(t, app, http.MethodDelete, "/api/v1/bots/"+id, nil, adminKey)
	if code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", code)
	}
}
