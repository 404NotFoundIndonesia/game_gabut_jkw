package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	healthhttp "github.com/404NFIDv2/bot-game-management/internal/health/interface/http"
)

// --- fakes ---

type okDBPinger struct{}

func (o *okDBPinger) Ping(_ context.Context) error { return nil }

type failDBPinger struct{}

func (f *failDBPinger) Ping(_ context.Context) error { return errors.New("db down") }

func okRedisFn(_ context.Context) healthhttp.RedisCmd {
	return healthhttp.NewRedisResult(nil)
}

func failRedisFn(_ context.Context) healthhttp.RedisCmd {
	return healthhttp.NewRedisResult(errors.New("redis down"))
}

// --- helpers ---

func newApp(db healthhttp.DBPinger, cache healthhttp.RedisPingerFunc) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := healthhttp.NewHealthHandler(db, cache)
	h.RegisterRoutes(app)
	return app
}

func doGet(t *testing.T, app *fiber.App, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	return resp.StatusCode, parsed
}

// --- tests ---

func TestLiveness_AlwaysOK(t *testing.T) {
	app := newApp(&okDBPinger{}, okRedisFn)
	code, body := doGet(t, app, "/health")
	if code != http.StatusOK {
		t.Errorf("status: got %d, want 200", code)
	}
	data, _ := body["data"].(map[string]any)
	if data["status"] != "ok" {
		t.Errorf("status field: got %v", data["status"])
	}
}

func TestReadiness_AllHealthy(t *testing.T) {
	app := newApp(&okDBPinger{}, okRedisFn)
	code, body := doGet(t, app, "/ready")
	if code != http.StatusOK {
		t.Errorf("status: got %d, want 200", code)
	}
	data := body["data"].(map[string]any)
	if data["status"] != "ok" {
		t.Errorf("overall status: got %v", data["status"])
	}
}

func TestReadiness_DBDown(t *testing.T) {
	app := newApp(&failDBPinger{}, okRedisFn)
	code, body := doGet(t, app, "/ready")
	if code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", code)
	}
	data := body["data"].(map[string]any)
	checks := data["checks"].(map[string]any)
	if checks["postgres"] != "down" {
		t.Errorf("postgres check: got %v", checks["postgres"])
	}
	if checks["redis"] != "ok" {
		t.Errorf("redis check: got %v", checks["redis"])
	}
}

func TestReadiness_RedisDown(t *testing.T) {
	app := newApp(&okDBPinger{}, failRedisFn)
	code, body := doGet(t, app, "/ready")
	if code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", code)
	}
	data := body["data"].(map[string]any)
	checks := data["checks"].(map[string]any)
	if checks["redis"] != "down" {
		t.Errorf("redis check: got %v", checks["redis"])
	}
	if checks["postgres"] != "ok" {
		t.Errorf("postgres check: got %v", checks["postgres"])
	}
}
