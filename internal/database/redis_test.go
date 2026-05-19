//go:build integration

package database_test

import (
	"context"
	"os"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/database"
)

func testRedisURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set; skipping integration test")
	}
	return url
}

func TestNewRedisClient_Ping(t *testing.T) {
	ctx := context.Background()
	client, err := database.NewRedisClient(ctx, testRedisURL(t))
	if err != nil {
		t.Fatalf("NewRedisClient: %v", err)
	}
	defer client.Close()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Ping after connect: %v", err)
	}
}

func TestNewRedisClient_InvalidURL(t *testing.T) {
	ctx := context.Background()
	_, err := database.NewRedisClient(ctx, "not-a-valid-redis-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}
