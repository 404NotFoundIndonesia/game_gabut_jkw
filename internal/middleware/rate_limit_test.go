package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/404NFIDv2/bot-game-management/internal/middleware"
)

// TestIPRateLimit_AllowsUnderLimit verifies requests under the limit pass through.
func TestIPRateLimit_AllowsUnderLimit(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	// Use a tiny limit so we can test cheaply — but stay under it.
	app.Use(middleware.IPRateLimit(nil, 5, time.Minute))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) })

	// nil Redis client: checkLimit will fail-open and allow the request.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on fail-open, got %d", resp.StatusCode)
	}
}

// TestBotRateLimit_NoBot_PassesThrough verifies that BotRateLimit skips when no bot is in ctx.
func TestBotRateLimit_NoBot_PassesThrough(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(middleware.BotRateLimit(nil, 60, time.Minute))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 when no bot in context, got %d", resp.StatusCode)
	}
}
