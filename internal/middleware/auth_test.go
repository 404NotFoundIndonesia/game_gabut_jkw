package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── AdminAuth tests ───────────────────────────────────────────────────────────

func adminApp() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/protected", middleware.AdminAuth("secret"), func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})
	return app
}

func TestAdminAuth_ValidKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, _ := adminApp().Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdminAuth_InvalidKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, _ := adminApp().Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAdminAuth_MissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	resp, _ := adminApp().Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAdminAuth_WrongScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Basic secret")
	resp, _ := adminApp().Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ── BotAuth tests ─────────────────────────────────────────────────────────────

type fakeBotLookup struct {
	bot *domain.Bot
	err error
}

func (f *fakeBotLookup) FindByTokenHash(_ context.Context, _ string) (*domain.Bot, error) {
	return f.bot, f.err
}

func hashNoop(raw string) string { return raw }

func botApp(lookup middleware.BotTokenLookup) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/bot", middleware.BotAuth(lookup, hashNoop), func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})
	return app
}

func TestBotAuth_ValidToken(t *testing.T) {
	bot := domain.NewBot("b", domain.NewBotToken("enc"), "h", 1)
	lookup := &fakeBotLookup{bot: bot}

	req := httptest.NewRequest(http.MethodGet, "/bot", nil)
	req.Header.Set("X-Bot-Token", "valid")
	resp, _ := botApp(lookup).Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBotAuth_MissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bot", nil)
	resp, _ := botApp(&fakeBotLookup{}).Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestBotAuth_UnknownToken(t *testing.T) {
	lookup := &fakeBotLookup{err: apperrors.NotFound("not found")}
	req := httptest.NewRequest(http.MethodGet, "/bot", nil)
	req.Header.Set("X-Bot-Token", "unknown")
	resp, _ := botApp(lookup).Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestBotAuth_InactiveBot(t *testing.T) {
	bot := domain.NewBot("b", domain.NewBotToken("enc"), "h", 1)
	bot.Deactivate()
	lookup := &fakeBotLookup{bot: bot}

	req := httptest.NewRequest(http.MethodGet, "/bot", nil)
	req.Header.Set("X-Bot-Token", "tok")
	resp, _ := botApp(lookup).Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for inactive bot, got %d", resp.StatusCode)
	}
}
