package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/404NFIDv2/bot-game-management/internal/config"
	"github.com/404NFIDv2/bot-game-management/internal/database"
	healthhttp "github.com/404NFIDv2/bot-game-management/internal/health/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	"github.com/404NFIDv2/bot-game-management/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)

	ctx := context.Background()

	// ── Database ──────────────────────────────────────────────────────────────
	pool, err := database.NewPostgresPool(ctx, cfg.DBDSN)
	if err != nil {
		log.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := database.RunMigrations(cfg.DBDSN, "file://migrations", log); err != nil {
		log.Error("migrations failed", "err", err)
		os.Exit(1)
	}

	// ── Redis ─────────────────────────────────────────────────────────────────
	redisClient, err := database.NewRedisClient(ctx, cfg.RedisURL)
	if err != nil {
		log.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// ── HTTP server ───────────────────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		ErrorHandler:          middleware.ErrorHandler,
		DisableStartupMessage: true,
		ReadTimeout:           10 * time.Second,
		WriteTimeout:          10 * time.Second,
		IdleTimeout:           30 * time.Second,
	})

	// Global middleware
	app.Use(recover.New(recover.Config{EnableStackTrace: cfg.IsDevelopment()}))
	app.Use(middleware.RequestID())

	// ── Routes ────────────────────────────────────────────────────────────────
	api := app.Group("/api/v1")

	// Health (no auth)
	redisPingerFn := healthhttp.RedisPingerFunc(func(c context.Context) healthhttp.RedisCmd {
		return healthhttp.NewRedisResult(redisClient.Ping(c).Err())
	})
	healthhttp.NewHealthHandler(pool, redisPingerFn).RegisterRoutes(app)

	// Future domain routes registered here (Phase 1–4).
	_ = api

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	addr := fmt.Sprintf(":%d", cfg.AppPort)
	log.Info("server starting", "addr", addr, "env", cfg.AppEnv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := app.Listen(addr); err != nil {
			log.Error("server error", "err", err)
		}
	}()

	<-quit
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	log.Info("server stopped gracefully")
}
