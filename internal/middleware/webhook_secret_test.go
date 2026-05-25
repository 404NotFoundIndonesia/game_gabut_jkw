package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/404NFIDv2/bot-game-management/internal/middleware"
)

func newWebhookApp(secret string) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Post("/hook", middleware.WebhookSecret(secret), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func TestWebhookSecret_ValidSecret(t *testing.T) {
	app := newWebhookApp("my-secret")
	req := httptest.NewRequest(http.MethodPost, "/hook", nil)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWebhookSecret_InvalidSecret(t *testing.T) {
	app := newWebhookApp("my-secret")
	req := httptest.NewRequest(http.MethodPost, "/hook", nil)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebhookSecret_MissingHeader(t *testing.T) {
	app := newWebhookApp("my-secret")
	req := httptest.NewRequest(http.MethodPost, "/hook", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 when header missing, got %d", resp.StatusCode)
	}
}

func TestWebhookSecret_EmptySecret_AlwaysRejects(t *testing.T) {
	// An empty provided token must not match even an empty configured secret.
	app := newWebhookApp("my-secret")
	req := httptest.NewRequest(http.MethodPost, "/hook", nil)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty token, got %d", resp.StatusCode)
	}
}
