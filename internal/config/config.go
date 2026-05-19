package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

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
}

// Load reads all env vars and returns a validated Config.
// Returns an error listing all missing required variables.
func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:          getenv("APP_ENV", "development"),
		AppPort:         getenvInt("APP_PORT", 8080),
		MetricsPort:     getenvInt("METRICS_PORT", 9090),
		DBDSN:           os.Getenv("DB_DSN"),
		RedisURL:        os.Getenv("REDIS_URL"),
		AdminAPIKey:     os.Getenv("ADMIN_API_KEY"),
		BotTokenEncryptionKey: os.Getenv("BOT_TOKEN_ENCRYPTION_KEY"),
		KBBIMode:        getenv("KBBI_MODE", "offline"),
		KBBIAPIURL:      os.Getenv("KBBI_API_URL"),
		LogLevel:        getenv("LOG_LEVEL", "info"),
		SessionTTLHours: getenvInt("SESSION_TTL_HOURS", 168),
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
