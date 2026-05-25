package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/bot/application"
	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeBotRepo struct {
	bots       map[uuid.UUID]*domain.Bot
	byTGID     map[int64]*domain.Bot
	byTokenHash map[string]*domain.Bot
	saveErr    error
}

func newFakeRepo() *fakeBotRepo {
	return &fakeBotRepo{
		bots:        make(map[uuid.UUID]*domain.Bot),
		byTGID:      make(map[int64]*domain.Bot),
		byTokenHash: make(map[string]*domain.Bot),
	}
}

func (r *fakeBotRepo) Save(_ context.Context, bot *domain.Bot) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.bots[bot.ID] = bot
	r.byTGID[bot.TelegramID] = bot
	r.byTokenHash[bot.TokenHash] = bot
	return nil
}

func (r *fakeBotRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Bot, error) {
	if b, ok := r.bots[id]; ok {
		return b, nil
	}
	return nil, apperrors.NotFound("bot not found")
}

func (r *fakeBotRepo) FindByTelegramID(_ context.Context, tid int64) (*domain.Bot, error) {
	if b, ok := r.byTGID[tid]; ok {
		return b, nil
	}
	return nil, apperrors.NotFound("bot not found")
}

func (r *fakeBotRepo) FindByTokenHash(_ context.Context, h string) (*domain.Bot, error) {
	if b, ok := r.byTokenHash[h]; ok {
		return b, nil
	}
	return nil, apperrors.NotFound("bot not found")
}

func (r *fakeBotRepo) FindAll(_ context.Context, _ domain.BotFilter, limit, offset int) ([]*domain.Bot, int, error) {
	all := make([]*domain.Bot, 0, len(r.bots))
	for _, b := range r.bots {
		all = append(all, b)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (r *fakeBotRepo) Delete(_ context.Context, id uuid.UUID) error {
	b, ok := r.bots[id]
	if !ok {
		return apperrors.NotFound("bot not found")
	}
	delete(r.bots, id)
	delete(r.byTGID, b.TelegramID)
	delete(r.byTokenHash, b.TokenHash)
	return nil
}

type fakeTGClient struct {
	botID int64
	err   error
}

func (f *fakeTGClient) GetBotID(_ context.Context, _ string) (int64, error) {
	return f.botID, f.err
}

func newService(repo *fakeBotRepo, tgID int64, tgErr error) *application.BotService {
	return application.NewBotService(
		repo,
		&fakeTGClient{botID: tgID, err: tgErr},
		"32-byte-test-key-for-unit-tests!", // exactly 32 bytes
		application.NewNoopSessionEnder(),
	)
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestRegisterBot_Success(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, 111, nil)

	bot, err := svc.RegisterBot(context.Background(), "MyBot", "valid-token")
	if err != nil {
		t.Fatalf("RegisterBot: %v", err)
	}
	if bot.Name != "MyBot" {
		t.Errorf("name: got %q", bot.Name)
	}
	if bot.TelegramID != 111 {
		t.Errorf("telegram_id: got %d", bot.TelegramID)
	}
	if !bot.Active {
		t.Error("new bot should be active")
	}
}

func TestRegisterBot_DuplicateTelegramID(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, 222, nil)

	_, _ = svc.RegisterBot(context.Background(), "First", "tok1")
	_, err := svc.RegisterBot(context.Background(), "Second", "tok2")
	if err == nil {
		t.Fatal("expected error for duplicate telegram ID")
	}
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT, got %s", ae.Code)
	}
}

func TestRegisterBot_InvalidToken(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, 0, errors.New("telegram error"))

	_, err := svc.RegisterBot(context.Background(), "Bot", "bad-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", ae.Code)
	}
}

func TestGetBot_NotFound(t *testing.T) {
	svc := newService(newFakeRepo(), 0, nil)
	_, err := svc.GetBot(context.Background(), uuid.New())
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND, got %s", ae.Code)
	}
}

func TestUpdateBot_Rename(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, 333, nil)
	bot, _ := svc.RegisterBot(context.Background(), "Old", "tok")

	newName := "New"
	updated, err := svc.UpdateBot(context.Background(), bot.ID, application.UpdateBotPatch{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateBot: %v", err)
	}
	if updated.Name != "New" {
		t.Errorf("name after update: got %q", updated.Name)
	}
}

func TestUpdateBot_Deactivate(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, 444, nil)
	bot, _ := svc.RegisterBot(context.Background(), "B", "tok")

	f := false
	updated, err := svc.UpdateBot(context.Background(), bot.ID, application.UpdateBotPatch{Active: &f})
	if err != nil {
		t.Fatalf("UpdateBot deactivate: %v", err)
	}
	if updated.Active {
		t.Error("expected active=false")
	}
}

func TestUpdateBot_NotFound(t *testing.T) {
	svc := newService(newFakeRepo(), 0, nil)
	name := "x"
	_, err := svc.UpdateBot(context.Background(), uuid.New(), application.UpdateBotPatch{Name: &name})
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND, got %s", ae.Code)
	}
}

func TestDeleteBot_Success(t *testing.T) {
	repo := newFakeRepo()
	svc := newService(repo, 555, nil)
	bot, _ := svc.RegisterBot(context.Background(), "B", "tok")

	if err := svc.DeleteBot(context.Background(), bot.ID); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}
	_, err := svc.GetBot(context.Background(), bot.ID)
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND after delete, got %s", ae.Code)
	}
}

func TestDeleteBot_NotFound(t *testing.T) {
	svc := newService(newFakeRepo(), 0, nil)
	err := svc.DeleteBot(context.Background(), uuid.New())
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND, got %s", ae.Code)
	}
}

func TestListBots_Pagination(t *testing.T) {
	repo := newFakeRepo()

	tgIDs := []int64{601, 602, 603}
	for i, tid := range tgIDs {
		svc := newService(repo, tid, nil)
		if _, err := svc.RegisterBot(context.Background(), fmt.Sprintf("Bot%d", i), "tok"); err != nil {
			t.Fatalf("seed bot %d: %v", i, err)
		}
	}

	svc := newService(repo, 0, nil)
	bots, total, err := svc.ListBots(context.Background(), domain.BotFilter{}, 2, 0)
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
	if len(bots) != 2 {
		t.Errorf("page size: got %d, want 2", len(bots))
	}
}

// helpers

func toAppError(t *testing.T, err error) *apperrors.AppError {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ae, ok := err.(*apperrors.AppError)
	if !ok {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	return ae
}
