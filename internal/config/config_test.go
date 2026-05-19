package config_test

import (
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/config"
)

// setenv sets multiple env vars and returns a cleanup func.
func setenv(t *testing.T, pairs map[string]string) {
	t.Helper()
	for k, v := range pairs {
		t.Setenv(k, v)
	}
}

var requiredVars = map[string]string{
	"DB_DSN":                    "postgres://localhost/test",
	"REDIS_URL":                 "redis://localhost:6379",
	"ADMIN_API_KEY":             "secret",
	"BOT_TOKEN_ENCRYPTION_KEY":  "32bytekeyxxxxxxxxxxxxxxxxxxxxxxx",
}

func TestLoad_Defaults(t *testing.T) {
	setenv(t, requiredVars)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Errorf("AppEnv default: got %q", cfg.AppEnv)
	}
	if cfg.AppPort != 8080 {
		t.Errorf("AppPort default: got %d", cfg.AppPort)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default: got %q", cfg.LogLevel)
	}
	if cfg.KBBIMode != "offline" {
		t.Errorf("KBBIMode default: got %q", cfg.KBBIMode)
	}
	if cfg.SessionTTLHours != 168 {
		t.Errorf("SessionTTLHours default: got %d", cfg.SessionTTLHours)
	}
}

func TestLoad_OverrideDefaults(t *testing.T) {
	setenv(t, requiredVars)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("LOG_LEVEL", "warn")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppEnv != "production" {
		t.Errorf("AppEnv: got %q", cfg.AppEnv)
	}
	if cfg.AppPort != 9090 {
		t.Errorf("AppPort: got %d", cfg.AppPort)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel: got %q", cfg.LogLevel)
	}
}

func TestLoad_MissingDBDSN(t *testing.T) {
	// Set all required except DB_DSN.
	for k, v := range requiredVars {
		if k != "DB_DSN" {
			t.Setenv(k, v)
		}
	}
	_, err := config.Load()
	if err == nil {
		t.Error("expected error when DB_DSN is missing")
	}
}

func TestLoad_MissingAdminAPIKey(t *testing.T) {
	for k, v := range requiredVars {
		if k != "ADMIN_API_KEY" {
			t.Setenv(k, v)
		}
	}
	_, err := config.Load()
	if err == nil {
		t.Error("expected error when ADMIN_API_KEY is missing")
	}
}

func TestLoad_MissingMultiple(t *testing.T) {
	// Set none of the required vars.
	_, err := config.Load()
	if err == nil {
		t.Error("expected error when all required vars missing")
	}
}

func TestIsDevelopment(t *testing.T) {
	setenv(t, requiredVars)
	t.Setenv("APP_ENV", "development")
	cfg, _ := config.Load()
	if !cfg.IsDevelopment() {
		t.Error("expected IsDevelopment=true")
	}
	if cfg.IsProduction() {
		t.Error("expected IsProduction=false")
	}
}

func TestIsProduction(t *testing.T) {
	setenv(t, requiredVars)
	t.Setenv("APP_ENV", "production")
	cfg, _ := config.Load()
	if !cfg.IsProduction() {
		t.Error("expected IsProduction=true")
	}
}
