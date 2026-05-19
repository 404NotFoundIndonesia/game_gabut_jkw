//go:build integration

package infrastructure_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	"github.com/404NFIDv2/bot-game-management/internal/bot/infrastructure"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func makeBot(name string, telegramID int64) *domain.Bot {
	return domain.NewBot(name, domain.NewBotToken("enc-"+name), "hash-"+name, telegramID)
}

func TestPostgresBotRepository_CRUD(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresBotRepository(pool)
	ctx := context.Background()

	bot := makeBot("TestBot", 9991)
	t.Cleanup(func() { repo.Delete(ctx, bot.ID) })

	// Save (insert)
	if err := repo.Save(ctx, bot); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// FindByID
	found, err := repo.FindByID(ctx, bot.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Name != bot.Name {
		t.Errorf("Name: got %q, want %q", found.Name, bot.Name)
	}

	// Save (update)
	bot.UpdateName("Renamed")
	if err := repo.Save(ctx, bot); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	found, _ = repo.FindByID(ctx, bot.ID)
	if found.Name != "Renamed" {
		t.Errorf("Name after update: got %q", found.Name)
	}

	// FindByTelegramID
	found, err = repo.FindByTelegramID(ctx, 9991)
	if err != nil {
		t.Fatalf("FindByTelegramID: %v", err)
	}
	if found.ID != bot.ID {
		t.Error("FindByTelegramID returned wrong bot")
	}

	// FindByTokenHash
	found, err = repo.FindByTokenHash(ctx, "hash-TestBot")
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if found.ID != bot.ID {
		t.Error("FindByTokenHash returned wrong bot")
	}

	// Delete
	if err := repo.Delete(ctx, bot.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = repo.FindByID(ctx, bot.ID)
	if ae, ok := err.(*apperrors.AppError); !ok || ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NotFound after delete, got %v", err)
	}
}

func TestPostgresBotRepository_FindAll_Filter(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresBotRepository(pool)
	ctx := context.Background()

	active := makeBot("ActiveBot", 9992)
	inactive := makeBot("InactiveBot", 9993)
	inactive.Deactivate()

	for _, b := range []*domain.Bot{active, inactive} {
		repo.Save(ctx, b)
	}
	t.Cleanup(func() {
		repo.Delete(ctx, active.ID)
		repo.Delete(ctx, inactive.ID)
	})

	trueVal := true
	bots, total, err := repo.FindAll(ctx, domain.BotFilter{Active: &trueVal}, 10, 0)
	if err != nil {
		t.Fatalf("FindAll active: %v", err)
	}
	for _, b := range bots {
		if !b.Active {
			t.Errorf("FindAll(active=true) returned inactive bot %s", b.Name)
		}
	}
	if total == 0 {
		t.Error("expected total > 0")
	}

	// No filter
	allBots, _, err := repo.FindAll(ctx, domain.BotFilter{}, 100, 0)
	if err != nil {
		t.Fatalf("FindAll no filter: %v", err)
	}
	if len(allBots) < 2 {
		t.Errorf("expected at least 2 bots, got %d", len(allBots))
	}
}

func TestPostgresBotRepository_FindByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresBotRepository(pool)
	_, err := repo.FindByID(context.Background(), [16]byte{})
	if ae, ok := err.(*apperrors.AppError); !ok || ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}
