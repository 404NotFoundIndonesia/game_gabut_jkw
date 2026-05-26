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
	"github.com/404NFIDv2/bot-game-management/internal/webhook"
	"github.com/404NFIDv2/bot-game-management/pkg/crypto"
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
	botSvc := application.NewBotService(botRepo, tgClient, cfg.BotTokenEncryptionKey, sessionSvc, cfg.WebhookBaseURL, cfg.WebhookSecretToken)
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

	// ── Telegram webhook handlers ─────────────────────────────────────────────
	convStore := webhook.NewRedisConversationStore(redisClient)
	chatIndex := webhook.NewRedisChatSessionIndex(redisClient)
	convTTL := time.Duration(cfg.ConvStateTTLMinutes) * time.Minute

	// Empty-prefix group so middleware scopes only to the two webhook routes
	// without adding an extra path segment on top of the full paths inside RegisterRoutes.
	tgGroup := app.Group("", middleware.WebhookSecret(cfg.WebhookSecretToken))
	webhook.NewMainBotHandler(
		botSvc, botGameSvc, lbSvc,
		convStore, tgClient,
		cfg.MainBotToken,
		cfg.TelegramAdminIDs,
		convTTL,
	).RegisterRoutes(tgGroup)
	cryptoKey := crypto.PadKey(cfg.BotTokenEncryptionKey)
	tokenDecryptor := webhook.TokenDecryptor(func(ciphertext string) (string, error) {
		return crypto.Decrypt(cryptoKey, ciphertext)
	})
	turnStore := webhook.NewRedisTurnStore(redisClient)
	webhook.NewChildBotHandler(
		botRepo, sessionSvc, botGameSvc, lbSvc,
		chatIndex, turnStore,
		tgClient, tokenDecryptor,
		webhook.UnoStickerMap{
			// Keys: "{color}_{value}" — values: Telegram sticker file_id strings.
			// Color: red | green | blue | yellow | wild
			// Value: 0-9 | skip | reverse | draw_two | wild | wild_draw_four
			// Cards without an entry fall back to emoji text (article type).
			"blue_0":              "CAACAgQAAxkBAAFKq6tqFT2bmNrDpDaqVRAl0gnXLNpCIgAC2QEAAl9XmQABKp6Tbd0qRv87BA",
			"blue_1":              "CAACAgQAAxkBAAFKq61qFT2cN8LOqLMiUdTuQovxWvnf5AAC2wEAAl9XmQAB5bj5fwVc2g47BA",
			"blue_2":              "CAACAgQAAxkBAAFKq69qFT2cnukqRg62evkuUNUs1HlhDgAC3QEAAl9XmQABBXuSQXaT_nM7BA",
			"blue_3":              "CAACAgQAAxkBAAFKq6lqFT14ofCwKFSxB4ooStd6E20UeQAC3wEAAl9XmQABUCV1h0Ng-7o7BA",
			"blue_4":              "CAACAgQAAxkBAAFKq7RqFT2dhssw7Gl4t0UAAc8T9VPYtYcAAuEBAAJfV5kAAejVI_h169j_OwQ",
			"blue_5":              "CAACAgQAAxkBAAFKq7VqFT2e3jw-h38zjLTgNBpVrTT1dQAC4wEAAl9XmQAC3-oPha7s2jsE",
			"blue_6":              "CAACAgQAAxkBAAFKq7dqFT2eFlvSGQoCorq7HRrL0LKyrwAC5QEAAl9XmQAB8DKFNTW-DoI7BA",
			"blue_7":              "CAACAgQAAxkBAAFKq7lqFT2f-0DJyP23tXuMa_TsnNUAAakAAucBAAJfV5kAAU4ECeiinV9aOwQ",
			"blue_8":              "CAACAgQAAxkBAAFKq7tqFT2foL2ffdiZRTfDGxSNXwujDAAC6QEAAl9XmQAB5iq4s6o8Cd47BA",
			"blue_9":              "CAACAgQAAxkBAAFKq71qFT2fXHBatCvFVBQZN3Gh1LQO_AAC6wEAAl9XmQAB77pfmc7VVoo7BA",
			"blue_skip":           "CAACAgQAAxkBAAFKq8NqFT2le8WWsladfX7q-u1Oh7MDSgAC8QEAAl9XmQABwH9JLP2PNic7BA",
			"blue_reverse":        "CAACAgQAAxkBAAFKrEtqFT_TmlX2eVRbsmM_qMcbpPmQhQAC7wEAAl9XmQABYwGXOd-V8zU7BA",
			"blue_draw_two":       "CAACAgQAAxkBAAFKrE9qFT_gj1MRKtWN8IPv4tGXbUBGQQAC7QEAAl9XmQABnVEYPZ-qrxM7BA",
			"green_0":             "CAACAgQAAxkBAAFKq8FqFT2kU5S1xQckODPTNhO_ufjPrQAC9wEAAl9XmQAB-5vgrDVg8wU7BA",
			"green_1":             "CAACAgQAAxkBAAFKq8JqFT2kVtQX3Mfh5_8RUGjI-oUmGwAC-QEAAl9XmQABVTUhvvmqrPQ7BA",
			"green_2":             "CAACAgQAAxkBAAFKq89qFT20B1APm44V2qGQeDAiTK7ABAAC-wEAAl9XmQABw1-UJ-1W0nQ7BA",
			"green_3":             "CAACAgQAAxkBAAFKq9BqFT21hLp4Xf-KS1YgLBVIzcNj8AAC_QEAAl9XmQABsFMUkikjT2o7BA",
			"green_4":             "CAACAgQAAxkBAAFKq9JqFT21WrzZsWTWAqAcWS_XeBMC3gAC_wEAAl9XmQABARICqk9L7OA7BA",
			"green_5":             "CAACAgQAAxkBAAFKq8pqFT2mQgG1EEGAjP0Sthix8BX4SQACAQIAAl9XmQABjdsDeZ1YX987BA",
			"green_6":             "CAACAgQAAxkBAAFKq8tqFT2mVxIhE-nPNbKHBjqbnZT4FgACAwIAAl9XmQABWiQPNJMNV307BA",
			"green_7":             "CAACAgQAAxkBAAFKq9hqFT23WtMK8P4DdJUuJcyWX9YVFQACBQIAAl9XmQABg2y0wojRiwY7BA",
			"green_8":             "CAACAgQAAxkBAAFKq9pqFT24NZD2utd3faZIpGXLroRKiAACBwIAAl9XmQABp1q0U0WY-4E7BA",
			"green_9":             "CAACAgQAAxkBAAFKrG5qFUC8gVoz1RR2zH4pJKVesQgPVAACCQIAAl9XmQABzgzj3YcM5bc7BA",
			"green_skip":          "CAACAgQAAxkBAAFKrHBqFUDH4almvRBJ8Ps50_ZM_SN4CgACDwIAAl9XmQAB5_oQV8Ub2EI7BA",
			"green_reverse":       "CAACAgQAAxkBAAFKrHRqFUDUOAuuaWqvEt1VEmw_lVwvfgACDQIAAl9XmQABTGKpgkt738k7BA",
			"green_draw_two":      "CAACAgQAAxkBAAFKrE9qFT_gj1MRKtWN8IPv4tGXbUBGQQAC7QEAAl9XmQABnVEYPZ-qrxM7BA",
			"wild_wild":           "CAACAgQAAxkBAAFKq8VqFT2l-OWJnDWuadoorxWHSgZMkQAC8wEAAl9XmQAByOY26RUBPWw7BA",
			"wild_wild_draw_four": "CAACAgQAAxkBAAFKq8dqFT2lJ7foZ4RWy5KpSdBIdbHHRQAC9QEAAl9XmQAB1zoAAWVAoFZJOwQ",
		},
		time.Duration(cfg.SessionTTLHours)*time.Hour,
	).RegisterRoutes(tgGroup)

	// Register the main bot webhook on startup (best-effort; log failure, don't exit).
	mainWebhookURL := cfg.WebhookBaseURL + "/telegram/main/webhook"
	if err := tgClient.SetWebhook(ctx, cfg.MainBotToken, mainWebhookURL, cfg.WebhookSecretToken); err != nil {
		log.Error("startup: failed to register main bot webhook", "url", mainWebhookURL, "err", err)
	} else {
		log.Info("startup: main bot webhook registered", "url", mainWebhookURL)
	}

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
