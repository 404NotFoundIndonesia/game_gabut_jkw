//go:build integration

package database_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/database"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; skipping integration test")
	}
	return dsn
}

func TestNewPostgresPool_Connect(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPostgresPool(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("NewPostgresPool: %v", err)
	}
	defer pool.Close()
}

func TestRunMigrations_UpDown(t *testing.T) {
	dsn := testDSN(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := database.RunMigrations(dsn, "file://../../migrations", logger); err != nil {
		t.Fatalf("RunMigrations up: %v", err)
	}
	// Running again must be idempotent (ErrNoChange swallowed).
	if err := database.RunMigrations(dsn, "file://../../migrations", logger); err != nil {
		t.Fatalf("RunMigrations up second time: %v", err)
	}
}
