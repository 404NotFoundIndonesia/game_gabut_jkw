package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/404NFIDv2/bot-game-management/internal/bot/application"
	botinfra "github.com/404NFIDv2/bot-game-management/internal/bot/infrastructure"
	bothttp "github.com/404NFIDv2/bot-game-management/internal/bot/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/config"
	"github.com/404NFIDv2/bot-game-management/internal/database"
	gameapp "github.com/404NFIDv2/bot-game-management/internal/game/application"
	gameinfra "github.com/404NFIDv2/bot-game-management/internal/game/infrastructure"
	gamehttp "github.com/404NFIDv2/bot-game-management/internal/game/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/games"
	sambungkata "github.com/404NFIDv2/bot-game-management/internal/games/sambung_kata"
	"github.com/404NFIDv2/bot-game-management/internal/games/sambung_kata/kbbi"
	truthordate "github.com/404NFIDv2/bot-game-management/internal/games/truth_or_date"
	"github.com/404NFIDv2/bot-game-management/internal/games/uno"
	healthhttp "github.com/404NFIDv2/bot-game-management/internal/health/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	sessionapp "github.com/404NFIDv2/bot-game-management/internal/session/application"
	sessioninfra "github.com/404NFIDv2/bot-game-management/internal/session/infrastructure"
	sessionhttp "github.com/404NFIDv2/bot-game-management/internal/session/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
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

	// ── Repositories ──────────────────────────────────────────────────────────
	botRepo := botinfra.NewPostgresBotRepository(pool)
	gameRepo := gameinfra.NewPostgresGameRepository(pool)
	botGameRepo := gameinfra.NewPostgresBotGameRepository(pool)
	sessionRepo := sessioninfra.NewPostgresSessionRepository(pool)
	sessionCache := sessioninfra.NewRedisSessionCache(redisClient)

	// ── Game engine registry ──────────────────────────────────────────────────
	var kbbiValidator kbbi.Validator
	if cfg.KBBIMode == "api" {
		kbbiValidator = kbbi.NewAPIValidator(cfg.KBBIAPIURL)
	} else {
		kbbiValidator = kbbi.NewOfflineValidator()
	}

	gameRegistry := games.NewRegistry()
	gameRegistry.Register("uno", uno.New(nil))
	gameRegistry.Register("sambung_kata", sambungkata.New(kbbiValidator))
	gameRegistry.Register("truth_or_date", truthordate.New(nil))

	// ── Services ──────────────────────────────────────────────────────────────
	tgClient := telegram.NewHTTPClient()

	sessionSvc := sessionapp.NewSessionService(
		botRepo,
		gameRepo,
		botGameRepo,
		sessionRepo,
		sessionCache,
		gameRegistry,
		sessionapp.NewNoopScoreCommitter(),
		time.Duration(cfg.SessionTTLHours)*time.Hour,
	)

	// Wire real SessionEnder now that sessionSvc is available.
	botSvc := application.NewBotService(botRepo, tgClient, cfg.BotTokenEncryptionKey, sessionSvc)
	botGameSvc := gameapp.NewBotGameService(botRepo, gameRepo, botGameRepo)

	// ── HTTP server ───────────────────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		ErrorHandler:          middleware.ErrorHandler,
		DisableStartupMessage: true,
		ReadTimeout:           10 * time.Second,
		WriteTimeout:          10 * time.Second,
		IdleTimeout:           30 * time.Second,
	})

	app.Use(recover.New(recover.Config{EnableStackTrace: cfg.IsDevelopment()}))
	app.Use(middleware.RequestID())

	// ── Health (no auth) ──────────────────────────────────────────────────────
	redisPingerFn := healthhttp.RedisPingerFunc(func(c context.Context) healthhttp.RedisCmd {
		return healthhttp.NewRedisResult(redisClient.Ping(c).Err())
	})
	healthhttp.NewHealthHandler(pool, redisPingerFn).RegisterRoutes(app)

	// ── API routes ────────────────────────────────────────────────────────────
	tokenHasher := func(raw string) string {
		h := sha256.Sum256([]byte(raw))
		return fmt.Sprintf("%x", h)
	}

	// Admin-only routes.
	adminAPI := app.Group("/api/v1", middleware.AdminAuth(cfg.AdminAPIKey))
	bothttp.NewBotHandler(botSvc).RegisterRoutes(adminAPI)
	gamehttp.NewBotGameHandler(botGameSvc).RegisterRoutes(adminAPI)

	// Routes accessible by AdminApiKey OR BotApiKey.
	openAPI := app.Group("/api/v1")
	openAPI.Use(func(c *fiber.Ctx) error {
		if c.Get("Authorization") != "" {
			return middleware.AdminAuth(cfg.AdminAPIKey)(c)
		}
		return middleware.BotAuth(botRepo, tokenHasher)(c)
	})
	gamehttp.NewGameHandler(botGameSvc).RegisterRoutes(openAPI)
	sessionhttp.NewSessionHandler(sessionSvc).RegisterRoutes(openAPI)

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
