package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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
	lbapp "github.com/404NFIDv2/bot-game-management/internal/leaderboard/application"
	lbinfra "github.com/404NFIDv2/bot-game-management/internal/leaderboard/infrastructure"
	lbhttp "github.com/404NFIDv2/bot-game-management/internal/leaderboard/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	sessionapp "github.com/404NFIDv2/bot-game-management/internal/session/application"
	sessioninfra "github.com/404NFIDv2/bot-game-management/internal/session/infrastructure"
	sessionhttp "github.com/404NFIDv2/bot-game-management/internal/session/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
	"github.com/404NFIDv2/bot-game-management/pkg/logger"
	_ "github.com/404NFIDv2/bot-game-management/pkg/metrics" // register Prometheus metrics
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)

	// ctx is used for the server lifetime; cancelled on shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	// ── Leaderboard ───────────────────────────────────────────────────────────
	lbRepo := lbinfra.NewPostgresLeaderboardRepository(pool)
	lbCache := lbinfra.NewRedisLeaderboardCache(redisClient)
	lbSvc := lbapp.NewLeaderboardService(lbRepo, lbCache)

	sessionSvc := sessionapp.NewSessionService(
		botRepo,
		gameRepo,
		botGameRepo,
		sessionRepo,
		sessionCache,
		gameRegistry,
		lbSvc,
		time.Duration(cfg.SessionTTLHours)*time.Hour,
	)

	// Wire real SessionEnder now that sessionSvc is available.
	botSvc := application.NewBotService(botRepo, tgClient, cfg.BotTokenEncryptionKey, sessionSvc)
	botGameSvc := gameapp.NewBotGameService(botRepo, gameRepo, botGameRepo)

	// ── Session archival job ──────────────────────────────────────────────────
	archivalJob := sessionapp.NewArchivalJob(sessionRepo, sessionCache, time.Duration(cfg.SessionTTLHours)*time.Hour)
	archivalJob.Start(ctx, time.Hour)

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
	app.Use(middleware.Metrics())

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
	// Per-bot rate limit: 60 req/min (applied after auth so bot ID is available).
	openAPI.Use(middleware.BotRateLimit(redisClient, 60, time.Minute))

	gamehttp.NewGameHandler(botGameSvc).RegisterRoutes(openAPI)
	sessionhttp.NewSessionHandler(sessionSvc).RegisterRoutes(openAPI)
	lbhttp.NewLeaderboardHandler(lbSvc).RegisterRoutes(openAPI)

	// ── Prometheus metrics endpoint (no auth) ─────────────────────────────────
	// Served on a separate net/http mux so it never goes through Fiber middleware.
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler: promhttp.Handler(),
	}
	go func() {
		log.Info("metrics server starting", "addr", metricsServer.Addr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server error", "err", err)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	addr := fmt.Sprintf(":%d", cfg.AppPort)
	log.Info("server starting", "addr", addr, "env", cfg.AppEnv)

	go func() {
		if err := app.Listen(addr); err != nil {
			log.Error("server error", "err", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Error("metrics server shutdown failed", "err", err)
	}
	log.Info("server stopped gracefully")
}
