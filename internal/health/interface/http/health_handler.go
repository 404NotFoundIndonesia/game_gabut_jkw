package http

import (
	"context"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

// DBPinger is satisfied by *pgxpool.Pool.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// RedisCmd is the return type of redis.Client.Ping.
type RedisCmd interface {
	Err() error
}

// CachePinger is satisfied by *redis.Client.
type CachePinger interface {
	Ping(ctx context.Context) *RedisResult
}

// RedisResult adapts redis.Cmd to RedisCmd.
type RedisResult struct {
	err error
}

func NewRedisResult(err error) *RedisResult { return &RedisResult{err: err} }

func (r *RedisResult) Err() error { return r.err }

// RedisPingerFunc allows wrapping a *redis.Client without importing it here.
type RedisPingerFunc func(ctx context.Context) RedisCmd

func (f RedisPingerFunc) Ping(ctx context.Context) RedisCmd { return f(ctx) }

type HealthHandler struct {
	db    DBPinger
	cache RedisPingerFunc
}

func NewHealthHandler(db DBPinger, cache RedisPingerFunc) *HealthHandler {
	return &HealthHandler{db: db, cache: cache}
}

// RegisterRoutes mounts health routes on the given router (no auth required).
func (h *HealthHandler) RegisterRoutes(r fiber.Router) {
	r.Get("/health", h.Liveness)
	r.Get("/ready", h.Readiness)
}

// Liveness godoc
// GET /health — always 200 while the process is alive.
func (h *HealthHandler) Liveness(c *fiber.Ctx) error {
	return c.JSON(response.Success(fiber.Map{"status": "ok"}, nil))
}

// Readiness godoc
// GET /ready — 200 only when DB and Redis are reachable; 503 otherwise.
func (h *HealthHandler) Readiness(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	checks := fiber.Map{
		"postgres": "ok",
		"redis":    "ok",
	}
	overall := "ok"

	if err := h.db.Ping(ctx); err != nil {
		checks["postgres"] = "down"
		overall = "degraded"
	}
	if err := h.cache(ctx).Err(); err != nil {
		checks["redis"] = "down"
		overall = "degraded"
	}

	statusCode := http.StatusOK
	if overall != "ok" {
		statusCode = http.StatusServiceUnavailable
	}

	return c.Status(statusCode).JSON(response.Success(fiber.Map{
		"status": overall,
		"checks": checks,
	}, nil))
}
