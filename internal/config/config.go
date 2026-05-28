package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// parseAdminIDs splits a comma-separated string of Telegram user IDs into []int64.
func parseAdminIDs(s string) ([]int64, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Telegram admin ID %q: %w", p, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Config holds all application configuration sourced from environment variables.
// Required vars are validated at load time; missing ones cause a fatal error.
type Config struct {
	// Application
	AppEnv      string
	AppPort     int
	MetricsPort int

	// Database
	DBDSN string

	// Redis
	RedisURL string

	// Security
	AdminAPIKey              string
	BotTokenEncryptionKey    string

	// KBBI
	KBBIMode   string // "offline" | "api"
	KBBIAPIURL string

	// Logging
	LogLevel string

	// Session
	SessionTTLHours int

	// Telegram webhook integration (Phase 6)
	MainBotToken             string
	TelegramAdminIDs         []int64
	WebhookBaseURL           string
	WebhookSecretToken       string
	ConvStateTTLMinutes      int
	SessionInactivityMinutes int
}

// Load reads all env vars and returns a validated Config.
// Returns an error listing all missing required variables.
func Load() (*Config, error) {
	adminIDs, err := parseAdminIDs(os.Getenv("TELEGRAM_ADMIN_IDS"))
	if err != nil {
		return nil, fmt.Errorf("TELEGRAM_ADMIN_IDS: %w", err)
	}

	webhookBase := strings.TrimRight(os.Getenv("WEBHOOK_BASE_URL"), "/")

	cfg := &Config{
		AppEnv:                getenv("APP_ENV", "development"),
		AppPort:               getenvInt("APP_PORT", 8080),
		MetricsPort:           getenvInt("METRICS_PORT", 9090),
		DBDSN:                 os.Getenv("DB_DSN"),
		RedisURL:              os.Getenv("REDIS_URL"),
		AdminAPIKey:           os.Getenv("ADMIN_API_KEY"),
		BotTokenEncryptionKey: os.Getenv("BOT_TOKEN_ENCRYPTION_KEY"),
		KBBIMode:              getenv("KBBI_MODE", "offline"),
		KBBIAPIURL:            os.Getenv("KBBI_API_URL"),
		LogLevel:              getenv("LOG_LEVEL", "info"),
		SessionTTLHours:       getenvInt("SESSION_TTL_HOURS", 168),
		MainBotToken:          os.Getenv("MAIN_BOT_TOKEN"),
		TelegramAdminIDs:      adminIDs,
		WebhookBaseURL:        webhookBase,
		WebhookSecretToken:    os.Getenv("WEBHOOK_SECRET_TOKEN"),
		ConvStateTTLMinutes:      getenvInt("CONV_STATE_TTL_MINUTES", 10),
		SessionInactivityMinutes: getenvInt("SESSION_INACTIVITY_MINUTES", 120),
	}

	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var missing []string

	if c.DBDSN == "" {
		missing = append(missing, "DB_DSN")
	}
	if c.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if c.AdminAPIKey == "" {
		missing = append(missing, "ADMIN_API_KEY")
	}
	if c.BotTokenEncryptionKey == "" {
		missing = append(missing, "BOT_TOKEN_ENCRYPTION_KEY")
	}

	if c.MainBotToken == "" {
		missing = append(missing, "MAIN_BOT_TOKEN")
	}
	if len(c.TelegramAdminIDs) == 0 {
		missing = append(missing, "TELEGRAM_ADMIN_IDS")
	}
	if c.WebhookBaseURL == "" {
		missing = append(missing, "WEBHOOK_BASE_URL")
	}
	if c.WebhookSecretToken == "" {
		missing = append(missing, "WEBHOOK_SECRET_TOKEN")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

// IsDevelopment returns true when running in development mode.
func (c *Config) IsDevelopment() bool {
	return strings.EqualFold(c.AppEnv, "development")
}

// IsProduction returns true when running in production mode.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
