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

	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	"github.com/404NFIDv2/bot-game-management/internal/session/application"
	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
	sessionhttp "github.com/404NFIDv2/bot-game-management/internal/session/interface/http"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── stub service ──────────────────────────────────────────────────────────────

type stubSessionSvc struct {
	session  *domain.GameSession
	sessions []*domain.GameSession
	result   *application.MoveResult
	err      error
}

func (s *stubSessionSvc) CreateSession(_ context.Context, _ uuid.UUID, _ application.CreateSessionRequest) (*domain.GameSession, error) {
	return s.session, s.err
}
func (s *stubSessionSvc) JoinSession(_ context.Context, _, _ uuid.UUID, _ application.JoinRequest) (*domain.GameSession, error) {
	return s.session, s.err
}
func (s *stubSessionSvc) StartSession(_ context.Context, _, _ uuid.UUID, _ int64) (*domain.GameSession, error) {
	return s.session, s.err
}
func (s *stubSessionSvc) SubmitMove(_ context.Context, _, _ uuid.UUID, _ application.MoveRequest) (*application.MoveResult, error) {
	return s.result, s.err
}
func (s *stubSessionSvc) EndSession(_ context.Context, _, _ uuid.UUID, _ application.EndSessionRequest) (*domain.GameSession, error) {
	return s.session, s.err
}
func (s *stubSessionSvc) GetSession(_ context.Context, _, _ uuid.UUID) (*domain.GameSession, error) {
	return s.session, s.err
}
func (s *stubSessionSvc) ListSessions(_ context.Context, _ uuid.UUID, _ domain.SessionFilter, _, _ int) ([]*domain.GameSession, int, error) {
	return s.sessions, len(s.sessions), s.err
}

// ── test helpers ──────────────────────────────────────────────────────────────

const adminKey = "test-admin-key"

func newApp(svc sessionhttp.SessionService) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler,
	})
	api := app.Group("/api/v1")
	// Dual auth: admin OR bot token (bot injected via context stub for bot-only endpoints).
	api.Use(func(c *fiber.Ctx) error {
		if c.Get("Authorization") != "" {
			return middleware.AdminAuth(adminKey)(c)
		}
		// Simulate bot auth by injecting a fake bot into context.
		if c.Get("X-Bot-Token") != "" {
			return c.Next() // BotFromContext will return false (no real bot), handled per test.
		}
		return apperrors.Unauthorized("missing auth")
	})
	sessionhttp.NewSessionHandler(svc).RegisterRoutes(api)
	return app
}


func doReq(t *testing.T, app *fiber.App, method, path string, body any, auth string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, _ := app.Test(req, -1)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func seedSession() *domain.GameSession {
	s := domain.NewGameSession(uuid.New(), uuid.New(), 1001)
	return s
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestGetSession_Success(t *testing.T) {
	s := seedSession()
	app := newApp(&stubSessionSvc{session: s})
	code, _ := doReq(t, app, http.MethodGet,
		"/api/v1/bots/"+s.BotID.String()+"/sessions/"+s.ID.String(),
		nil, adminKey)
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	app := newApp(&stubSessionSvc{err: apperrors.NotFound("session")})
	botID := uuid.New()
	code, _ := doReq(t, app, http.MethodGet,
		"/api/v1/bots/"+botID.String()+"/sessions/"+uuid.New().String(),
		nil, adminKey)
	if code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", code)
	}
}

func TestGetSession_InvalidUUID(t *testing.T) {
	app := newApp(&stubSessionSvc{})
	code, _ := doReq(t, app, http.MethodGet,
		"/api/v1/bots/not-a-uuid/sessions/"+uuid.New().String(),
		nil, adminKey)
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestListSessions_Success(t *testing.T) {
	sessions := []*domain.GameSession{seedSession(), seedSession()}
	app := newApp(&stubSessionSvc{sessions: sessions})
	botID := uuid.New()
	code, body := doReq(t, app, http.MethodGet,
		"/api/v1/bots/"+botID.String()+"/sessions",
		nil, adminKey)
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
	data := body["data"].(map[string]any)
	list := data["sessions"].([]any)
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}
}

func TestEndSession_AdminCanEnd(t *testing.T) {
	s := seedSession()
	s.Status = domain.StatusInProgress
	app := newApp(&stubSessionSvc{session: s})
	code, _ := doReq(t, app, http.MethodPost,
		"/api/v1/bots/"+s.BotID.String()+"/sessions/"+s.ID.String()+"/end",
		map[string]any{"reason": "admin ended"}, adminKey)
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestEndSession_AlreadyFinished(t *testing.T) {
	app := newApp(&stubSessionSvc{err: apperrors.Conflict("session already finished")})
	botID := uuid.New()
	code, _ := doReq(t, app, http.MethodPost,
		"/api/v1/bots/"+botID.String()+"/sessions/"+uuid.New().String()+"/end",
		map[string]any{}, adminKey)
	if code != http.StatusConflict {
		t.Errorf("expected 409, got %d", code)
	}
}

func TestEndSession_NotFound(t *testing.T) {
	app := newApp(&stubSessionSvc{err: apperrors.NotFound("session")})
	botID := uuid.New()
	code, _ := doReq(t, app, http.MethodPost,
		"/api/v1/bots/"+botID.String()+"/sessions/"+uuid.New().String()+"/end",
		map[string]any{}, adminKey)
	if code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", code)
	}
}

func TestCreateSession_RequiresBotAuth(t *testing.T) {
	// Admin key (no bot in context) should get 403 from the handler guard.
	app := newApp(&stubSessionSvc{session: seedSession()})
	botID := uuid.New()
	code, _ := doReq(t, app, http.MethodPost,
		"/api/v1/bots/"+botID.String()+"/sessions",
		map[string]any{"game_id": uuid.New().String(), "chat_id": 123,
			"player": map[string]any{"telegram_user_id": 1, "display_name": "H"}},
		adminKey)
	if code != http.StatusForbidden {
		t.Errorf("expected 403 when admin calls bot-only endpoint, got %d", code)
	}
}

func TestCreateSession_BadBody(t *testing.T) {
	app := newApp(&stubSessionSvc{session: seedSession()})
	botID := uuid.New()
	code, _ := doReq(t, app, http.MethodPost,
		"/api/v1/bots/"+botID.String()+"/sessions",
		map[string]any{"game_id": "not-a-uuid"}, // missing required fields
		adminKey)
	// Will hit bot-auth guard first → 403
	if code != http.StatusForbidden && code != http.StatusBadRequest {
		t.Errorf("expected 400 or 403, got %d", code)
	}
}

func TestNoAuth_Returns401(t *testing.T) {
	app := newApp(&stubSessionSvc{})
	botID := uuid.New()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/bots/"+botID.String()+"/sessions", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}
