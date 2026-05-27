package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/bot/application"
	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
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

func (f *fakeTGClient) SetWebhook(_ context.Context, _, _, _ string) error { return f.err }
func (f *fakeTGClient) DeleteWebhook(_ context.Context, _ string) error    { return f.err }
func (f *fakeTGClient) SendMessage(_ context.Context, _ string, _ int64, _ string) error {
	return f.err
}
func (f *fakeTGClient) GetWebhookInfo(_ context.Context, _ string) (telegram.WebhookInfo, error) {
	return telegram.WebhookInfo{}, f.err
}
func (f *fakeTGClient) SendMessageWithKeyboard(_ context.Context, _ string, _ int64, _ string, _ telegram.InlineKeyboardMarkup) error {
	return f.err
}
func (f *fakeTGClient) SendMessageGetID(_ context.Context, _ string, _ int64, _ string, _ telegram.InlineKeyboardMarkup) (int64, error) {
	return 0, f.err
}
func (f *fakeTGClient) SendSticker(_ context.Context, _ string, _ int64, _ string, _ *telegram.InlineKeyboardMarkup) error {
	return f.err
}
func (f *fakeTGClient) AnswerCallbackQuery(_ context.Context, _, _ string) error { return f.err }
func (f *fakeTGClient) AnswerCallbackQueryAlert(_ context.Context, _, _, _ string) error {
	return f.err
}
func (f *fakeTGClient) EditMessageText(_ context.Context, _ string, _, _ int64, _ string, _ *telegram.InlineKeyboardMarkup) error {
	return f.err
}
func (f *fakeTGClient) AnswerInlineQuery(_ context.Context, _, _ string, _ []telegram.InlineQueryResult) error {
	return f.err
}
func (f *fakeTGClient) SendHTMLMessage(_ context.Context, _ string, _ int64, _ string) error {
	return f.err
}
func (f *fakeTGClient) SendHTMLMessageWithKeyboard(_ context.Context, _ string, _ int64, _ string, _ telegram.InlineKeyboardMarkup) error {
	return f.err
}

func newService(repo *fakeBotRepo, tgID int64, tgErr error) *application.BotService {
	return application.NewBotService(
		repo,
		&fakeTGClient{botID: tgID, err: tgErr},
		"32-byte-test-key-for-unit-tests!", // exactly 32 bytes
		application.NewNoopSessionEnder(),
		"https://api.example.com",
		"test-webhook-secret",
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

// ── T-06-09: webhook-aware service methods ────────────────────────────────────

type fakeTGClientWithWebhook struct {
	botID      int64
	botErr     error
	webhookErr error
	setWebhookCalled    int
	deleteWebhookCalled int
}

func (f *fakeTGClientWithWebhook) GetBotID(_ context.Context, _ string) (int64, error) {
	return f.botID, f.botErr
}
func (f *fakeTGClientWithWebhook) SetWebhook(_ context.Context, _, _, _ string) error {
	f.setWebhookCalled++
	return f.webhookErr
}
func (f *fakeTGClientWithWebhook) DeleteWebhook(_ context.Context, _ string) error {
	f.deleteWebhookCalled++
	return f.webhookErr
}
func (f *fakeTGClientWithWebhook) GetWebhookInfo(_ context.Context, _ string) (telegram.WebhookInfo, error) {
	return telegram.WebhookInfo{}, f.botErr
}
func (f *fakeTGClientWithWebhook) SendMessage(_ context.Context, _ string, _ int64, _ string) error {
	return nil
}
func (f *fakeTGClientWithWebhook) SendMessageWithKeyboard(_ context.Context, _ string, _ int64, _ string, _ telegram.InlineKeyboardMarkup) error {
	return nil
}
func (f *fakeTGClientWithWebhook) SendMessageGetID(_ context.Context, _ string, _ int64, _ string, _ telegram.InlineKeyboardMarkup) (int64, error) {
	return 0, nil
}
func (f *fakeTGClientWithWebhook) SendSticker(_ context.Context, _ string, _ int64, _ string, _ *telegram.InlineKeyboardMarkup) error {
	return nil
}
func (f *fakeTGClientWithWebhook) AnswerCallbackQuery(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeTGClientWithWebhook) AnswerCallbackQueryAlert(_ context.Context, _, _, _ string) error {
	return nil
}
func (f *fakeTGClientWithWebhook) EditMessageText(_ context.Context, _ string, _, _ int64, _ string, _ *telegram.InlineKeyboardMarkup) error {
	return nil
}
func (f *fakeTGClientWithWebhook) AnswerInlineQuery(_ context.Context, _, _ string, _ []telegram.InlineQueryResult) error {
	return nil
}
func (f *fakeTGClientWithWebhook) SendHTMLMessage(_ context.Context, _ string, _ int64, _ string) error {
	return nil
}
func (f *fakeTGClientWithWebhook) SendHTMLMessageWithKeyboard(_ context.Context, _ string, _ int64, _ string, _ telegram.InlineKeyboardMarkup) error {
	return nil
}

func newServiceWithWebhookClient(repo *fakeBotRepo, tgClient *fakeTGClientWithWebhook) *application.BotService {
	return application.NewBotService(
		repo,
		tgClient,
		"32-byte-test-key-for-unit-tests!",
		application.NewNoopSessionEnder(),
		"https://api.example.com",
		"test-secret",
	)
}

func TestRegisterBotWithWebhook_Success(t *testing.T) {
	repo := newFakeRepo()
	tgClient := &fakeTGClientWithWebhook{botID: 777}
	svc := newServiceWithWebhookClient(repo, tgClient)

	bot, err := svc.RegisterBotWithWebhook(context.Background(), "WebhookBot", "valid-token")
	if err != nil {
		t.Fatalf("RegisterBotWithWebhook: %v", err)
	}
	if bot.Name != "WebhookBot" {
		t.Errorf("name: got %q", bot.Name)
	}
	if tgClient.setWebhookCalled != 1 {
		t.Errorf("SetWebhook calls: got %d, want 1", tgClient.setWebhookCalled)
	}
}

func TestRegisterBotWithWebhook_WebhookFailure_RollsBack(t *testing.T) {
	repo := newFakeRepo()
	tgClient := &fakeTGClientWithWebhook{botID: 888, webhookErr: errors.New("telegram error")}
	svc := newServiceWithWebhookClient(repo, tgClient)

	_, err := svc.RegisterBotWithWebhook(context.Background(), "Bot", "token")
	if err == nil {
		t.Fatal("expected error when SetWebhook fails")
	}
	// Bot must not exist in repo after rollback.
	all, total, _ := svc.ListBots(context.Background(), domain.BotFilter{}, 100, 0)
	if total != 0 || len(all) != 0 {
		t.Errorf("bot should have been rolled back, found %d", total)
	}
}

func TestDeleteBotWithWebhook_Success(t *testing.T) {
	repo := newFakeRepo()
	tgClient := &fakeTGClientWithWebhook{botID: 999}
	svc := newServiceWithWebhookClient(repo, tgClient)

	bot, _ := svc.RegisterBotWithWebhook(context.Background(), "B", "tok")

	// Reset counter after registration.
	tgClient.deleteWebhookCalled = 0
	if err := svc.DeleteBotWithWebhook(context.Background(), bot.ID); err != nil {
		t.Fatalf("DeleteBotWithWebhook: %v", err)
	}
	if tgClient.deleteWebhookCalled != 1 {
		t.Errorf("DeleteWebhook calls: got %d, want 1", tgClient.deleteWebhookCalled)
	}
	_, err := svc.GetBot(context.Background(), bot.ID)
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected bot deleted, got %s", ae.Code)
	}
}

func TestReactivateBotWithWebhook_Success(t *testing.T) {
	repo := newFakeRepo()
	tgClient := &fakeTGClientWithWebhook{botID: 1001}
	svc := newServiceWithWebhookClient(repo, tgClient)

	bot, _ := svc.RegisterBotWithWebhook(context.Background(), "B", "tok")
	// Manually deactivate via UpdateBot.
	f := false
	_, _ = svc.UpdateBot(context.Background(), bot.ID, application.UpdateBotPatch{Active: &f})

	tgClient.setWebhookCalled = 0
	reactivated, err := svc.ReactivateBotWithWebhook(context.Background(), bot.ID)
	if err != nil {
		t.Fatalf("ReactivateBotWithWebhook: %v", err)
	}
	if !reactivated.Active {
		t.Error("bot should be active after reactivation")
	}
	if tgClient.setWebhookCalled != 1 {
		t.Errorf("SetWebhook calls: got %d, want 1", tgClient.setWebhookCalled)
	}
}

func TestReactivateBotWithWebhook_AlreadyActive_ReturnsConflict(t *testing.T) {
	repo := newFakeRepo()
	tgClient := &fakeTGClientWithWebhook{botID: 1002}
	svc := newServiceWithWebhookClient(repo, tgClient)

	bot, _ := svc.RegisterBotWithWebhook(context.Background(), "B", "tok")
	_, err := svc.ReactivateBotWithWebhook(context.Background(), bot.ID)
	ae := toAppError(t, err)
	if ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT for already-active bot, got %s", ae.Code)
	}
}

func TestReactivateBotWithWebhook_WebhookFailure_RevertsActivation(t *testing.T) {
	repo := newFakeRepo()
	tgClient := &fakeTGClientWithWebhook{botID: 1003}
	svc := newServiceWithWebhookClient(repo, tgClient)

	bot, _ := svc.RegisterBotWithWebhook(context.Background(), "B", "tok")
	f := false
	_, _ = svc.UpdateBot(context.Background(), bot.ID, application.UpdateBotPatch{Active: &f})

	// Now make SetWebhook fail.
	tgClient.webhookErr = errors.New("telegram down")
	_, err := svc.ReactivateBotWithWebhook(context.Background(), bot.ID)
	if err == nil {
		t.Fatal("expected error when SetWebhook fails during reactivation")
	}
	// Bot must remain inactive.
	fetched, _ := svc.GetBot(context.Background(), bot.ID)
	if fetched.Active {
		t.Error("bot should remain inactive after failed reactivation")
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
