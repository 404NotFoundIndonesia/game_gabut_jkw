package webhook_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	lbdomain "github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
	"github.com/404NFIDv2/bot-game-management/internal/webhook"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// ── stubs ─────────────────────────────────────────────────────────────────────

type stubMainBotSvc struct {
	bot  *botdomain.Bot
	bots []*botdomain.Bot
	err  error
}

func (s *stubMainBotSvc) RegisterBotWithWebhook(_ context.Context, name, _ string) (*botdomain.Bot, error) {
	if s.err != nil {
		return nil, s.err
	}
	b := botdomain.NewBot(name, botdomain.NewBotToken("enc"), "hash", 999)
	return b, nil
}
func (s *stubMainBotSvc) DeleteBotWithWebhook(_ context.Context, _ uuid.UUID) error { return s.err }
func (s *stubMainBotSvc) ReactivateBotWithWebhook(_ context.Context, _ uuid.UUID) (*botdomain.Bot, error) {
	return s.bot, s.err
}
func (s *stubMainBotSvc) ListBots(_ context.Context, _ botdomain.BotFilter, _, _ int) ([]*botdomain.Bot, int, error) {
	return s.bots, len(s.bots), s.err
}

type stubMainGameSvc struct {
	games    []*gamedomain.Game
	botGames []*gamedomain.BotGame
	err      error
}

func (s *stubMainGameSvc) ListGames(_ context.Context) ([]*gamedomain.Game, error) {
	return s.games, s.err
}
func (s *stubMainGameSvc) ListBotGames(_ context.Context, _ uuid.UUID) ([]*gamedomain.BotGame, error) {
	return s.botGames, s.err
}
func (s *stubMainGameSvc) AssignGame(_ context.Context, _, _ uuid.UUID) (*gamedomain.BotGame, error) {
	return &gamedomain.BotGame{}, s.err
}
func (s *stubMainGameSvc) RemoveGame(_ context.Context, _, _ uuid.UUID) error { return s.err }
func (s *stubMainGameSvc) GetGameBySlug(_ context.Context, slug gamedomain.GameSlug) (*gamedomain.Game, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &gamedomain.Game{ID: uuid.New(), Slug: slug, Name: string(slug)}, nil
}

type stubMainLbSvc struct {
	lb  *lbdomain.Leaderboard
	err error
}

func (s *stubMainLbSvc) GetByBot(_ context.Context, _ uuid.UUID, _ pagination.Params) (*lbdomain.Leaderboard, error) {
	return s.lb, s.err
}
func (s *stubMainLbSvc) GetGlobal(_ context.Context, _ pagination.Params) (*lbdomain.Leaderboard, error) {
	return s.lb, s.err
}

// stubTGClient satisfies telegram.Client for tests.
// sendCalled counts all outgoing messages (plain, keyboard, and edits).
type stubTGClient struct {
	botIDErr    error
	sendCalled  int
	lastMessage string
}

func (s *stubTGClient) GetBotID(_ context.Context, _ string) (int64, error) {
	if s.botIDErr != nil {
		return 0, s.botIDErr
	}
	return 12345, nil
}
func (s *stubTGClient) SetWebhook(_ context.Context, _, _, _ string) error { return nil }
func (s *stubTGClient) DeleteWebhook(_ context.Context, _ string) error    { return nil }
func (s *stubTGClient) GetWebhookInfo(_ context.Context, _ string) (telegram.WebhookInfo, error) {
	return telegram.WebhookInfo{}, nil
}
func (s *stubTGClient) SendMessage(_ context.Context, _ string, _ int64, text string) error {
	s.sendCalled++
	s.lastMessage = text
	return nil
}
func (s *stubTGClient) SendMessageWithKeyboard(_ context.Context, _ string, _ int64, text string, _ telegram.InlineKeyboardMarkup) error {
	s.sendCalled++
	s.lastMessage = text
	return nil
}
func (s *stubTGClient) AnswerCallbackQuery(_ context.Context, _, _ string) error { return nil }
func (s *stubTGClient) EditMessageText(_ context.Context, _ string, _, _ int64, text string, _ *telegram.InlineKeyboardMarkup) error {
	s.sendCalled++
	s.lastMessage = text
	return nil
}

// ── test helpers ──────────────────────────────────────────────────────────────

const (
	mainTestAdminID int64 = 111111
	mainTestSecret        = "test-secret"
)

func newMainApp(botSvc webhook.MainBotSvc, gameSvc webhook.MainGameSvc, lbSvc webhook.MainLeaderboardSvc, tgClient *stubTGClient) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewMainBotHandler(
		botSvc, gameSvc, lbSvc,
		newStubConvStore(),
		tgClient,
		"main-token",
		[]int64{mainTestAdminID},
		10*time.Minute,
	)
	h.RegisterRoutes(app)
	return app
}

func postUpdate(t *testing.T, app *fiber.App, update telegram.Update, secret string) int {
	t.Helper()
	b, _ := json.Marshal(update)
	req := httptest.NewRequest(http.MethodPost, "/telegram/main/webhook", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	}
	resp, _ := app.Test(req, -1)
	return resp.StatusCode
}

func makeUpdate(userID int64, text string) telegram.Update {
	return telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 1,
			From:      &telegram.User{ID: userID},
			Chat:      &telegram.Chat{ID: userID, Type: "private"},
			Text:      text,
		},
	}
}

func makeCallbackUpdate(userID int64, chatID, msgID int64, data string) telegram.Update {
	return telegram.Update{
		UpdateID: 1,
		CallbackQuery: &telegram.CallbackQuery{
			ID:   "cq-1",
			From: &telegram.User{ID: userID},
			Message: &telegram.Message{
				MessageID: msgID,
				Chat:      &telegram.Chat{ID: chatID},
			},
			Data: data,
		},
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestMainHandler_NonAdminRejected(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(999999, "/listbots"), "")
	if tg.sendCalled != 1 || tg.lastMessage != "⛔ Unauthorized." {
		t.Errorf("expected unauthorized reply, got %q (calls: %d)", tg.lastMessage, tg.sendCalled)
	}
}

func TestMainHandler_ListBots_Empty(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{bots: nil}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/listbots"), "")
	if tg.lastMessage == "" {
		t.Error("expected a reply")
	}
}

func TestMainHandler_ListBots_WithBots(t *testing.T) {
	b := botdomain.NewBot("TestBot", botdomain.NewBotToken("enc"), "hash", 1)
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{bots: []*botdomain.Bot{b}}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/listbots"), "")
	if tg.sendCalled == 0 {
		t.Error("expected reply for listbots")
	}
}

func TestMainHandler_ListGames(t *testing.T) {
	games := []*gamedomain.Game{
		{ID: uuid.New(), Slug: "uno", Name: "Uno", MinPlayers: 2, MaxPlayers: 8},
	}
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{games: games}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/listgames"), "")
	if tg.sendCalled == 0 {
		t.Error("expected reply for listgames")
	}
}

// /removebot with no bots shows a text message (not a keyboard).
func TestMainHandler_RemoveBot_NoBots(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{bots: nil}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/removebot"), "")
	if tg.sendCalled == 0 {
		t.Error("expected reply")
	}
	if !contains(tg.lastMessage, "No active bots") {
		t.Errorf("expected 'No active bots' message, got %q", tg.lastMessage)
	}
}

// /removebot with bots available shows a keyboard (SendMessageWithKeyboard).
func TestMainHandler_RemoveBot_ShowsKeyboard(t *testing.T) {
	b := botdomain.NewBot("MyBot", botdomain.NewBotToken("enc"), "hash", 1)
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{bots: []*botdomain.Bot{b}}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/removebot"), "")
	if tg.sendCalled == 0 {
		t.Error("expected keyboard message")
	}
	if !contains(tg.lastMessage, "Select bot") {
		t.Errorf("expected selection prompt, got %q", tg.lastMessage)
	}
}

// Callback: rb:<encoded_bot_id> removes the bot and edits the message.
func TestMainHandler_CbRemoveBot_Success(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	id := uuid.New()
	enc := encodeIDTest(id)
	postUpdate(t, app, makeCallbackUpdate(mainTestAdminID, 100, 42, "rb:"+enc), "")
	if tg.sendCalled == 0 {
		t.Error("expected edit after remove")
	}
	if !contains(tg.lastMessage, "removed") {
		t.Errorf("expected removal confirmation, got %q", tg.lastMessage)
	}
}

// Callback: rb:<encoded_bot_id> with service error shows error in edited message.
func TestMainHandler_CbRemoveBot_ServiceError(t *testing.T) {
	tg := &stubTGClient{}
	svc := &stubMainBotSvc{err: apperrors.NotFound("bot")}
	app := newMainApp(svc, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	id := uuid.New()
	enc := encodeIDTest(id)
	postUpdate(t, app, makeCallbackUpdate(mainTestAdminID, 100, 42, "rb:"+enc), "")
	if tg.sendCalled == 0 {
		t.Error("expected error edit")
	}
	if !contains(tg.lastMessage, "❌") {
		t.Errorf("expected error message, got %q", tg.lastMessage)
	}
}

// /assigngame with no bots shows a text message.
func TestMainHandler_AssignGame_NoBots(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/assigngame"), "")
	if tg.sendCalled == 0 {
		t.Error("expected a reply")
	}
	if !contains(tg.lastMessage, "No bots") {
		t.Errorf("expected no-bots message, got %q", tg.lastMessage)
	}
}

// /assigngame with bots shows a keyboard.
func TestMainHandler_AssignGame_ShowsKeyboard(t *testing.T) {
	b := botdomain.NewBot("MyBot", botdomain.NewBotToken("enc"), "hash", 1)
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{bots: []*botdomain.Bot{b}}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/assigngame"), "")
	if !contains(tg.lastMessage, "Select bot") {
		t.Errorf("expected selection prompt, got %q", tg.lastMessage)
	}
}

// Callback ag1: shows game list keyboard.
func TestMainHandler_CbAssignGame_Step2_ShowsGames(t *testing.T) {
	games := []*gamedomain.Game{
		{ID: uuid.New(), Slug: "uno", Name: "Uno"},
	}
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{games: games}, &stubMainLbSvc{}, tg)

	enc := encodeIDTest(uuid.New())
	postUpdate(t, app, makeCallbackUpdate(mainTestAdminID, 100, 42, "ag1:"+enc), "")
	if !contains(tg.lastMessage, "Select game") {
		t.Errorf("expected game selection prompt, got %q", tg.lastMessage)
	}
}

// Callback ag2: assigns game and confirms.
func TestMainHandler_CbAssignGame_Confirm_Success(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	botEnc := encodeIDTest(uuid.New())
	gameEnc := encodeIDTest(uuid.New())
	postUpdate(t, app, makeCallbackUpdate(mainTestAdminID, 100, 42, "ag2:"+botEnc+":"+gameEnc), "")
	if !contains(tg.lastMessage, "assigned") {
		t.Errorf("expected assignment confirmation, got %q", tg.lastMessage)
	}
}

// Callback ag2: service error shows error.
func TestMainHandler_CbAssignGame_Confirm_Error(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{err: apperrors.NotFound("game")}, &stubMainLbSvc{}, tg)

	botEnc := encodeIDTest(uuid.New())
	gameEnc := encodeIDTest(uuid.New())
	postUpdate(t, app, makeCallbackUpdate(mainTestAdminID, 100, 42, "ag2:"+botEnc+":"+gameEnc), "")
	if !contains(tg.lastMessage, "❌") {
		t.Errorf("expected error message, got %q", tg.lastMessage)
	}
}

func TestMainHandler_Leaderboard_Global_DirectArg(t *testing.T) {
	lb := &lbdomain.Leaderboard{
		Entries: []lbdomain.LeaderboardEntry{{Rank: 1, DisplayName: "Alice", TotalScore: 100, Wins: 3}},
	}
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{lb: lb}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/leaderboard global"), "")
	if tg.sendCalled == 0 || tg.lastMessage == "" {
		t.Error("expected leaderboard reply")
	}
	if !contains(tg.lastMessage, "Global Leaderboard") {
		t.Errorf("expected global leaderboard, got %q", tg.lastMessage)
	}
}

func TestMainHandler_Leaderboard_NoArgs_ShowsMenu(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/leaderboard"), "")
	if tg.sendCalled == 0 {
		t.Error("expected menu reply for /leaderboard with no args")
	}
	if !contains(tg.lastMessage, "Select leaderboard") {
		t.Errorf("expected selection prompt, got %q", tg.lastMessage)
	}
}

// Callback lb:global returns global leaderboard via EditMessageText.
func TestMainHandler_CbLeaderboard_Global(t *testing.T) {
	lb := &lbdomain.Leaderboard{
		Entries: []lbdomain.LeaderboardEntry{{Rank: 1, DisplayName: "Bob", TotalScore: 50, Wins: 1}},
	}
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{lb: lb}, tg)

	postUpdate(t, app, makeCallbackUpdate(mainTestAdminID, 100, 42, "lb:global"), "")
	if !contains(tg.lastMessage, "Global Leaderboard") {
		t.Errorf("expected global leaderboard, got %q", tg.lastMessage)
	}
}

// Callback lb:<bot_id> returns per-bot leaderboard.
func TestMainHandler_CbLeaderboard_Bot(t *testing.T) {
	lb := &lbdomain.Leaderboard{Entries: []lbdomain.LeaderboardEntry{}}
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{lb: lb}, tg)

	enc := encodeIDTest(uuid.New())
	postUpdate(t, app, makeCallbackUpdate(mainTestAdminID, 100, 42, "lb:"+enc), "")
	if !contains(tg.lastMessage, "Bot Leaderboard") {
		t.Errorf("expected bot leaderboard, got %q", tg.lastMessage)
	}
}

// Non-admin callback is silently ignored (AnswerCallbackQuery is called but no edit/send).
func TestMainHandler_Callback_NonAdminIgnored(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeCallbackUpdate(999999, 100, 42, "lb:global"), "")
	if tg.sendCalled != 0 {
		t.Errorf("non-admin callback should trigger no send/edit, got %d calls with %q", tg.sendCalled, tg.lastMessage)
	}
}

func TestMainHandler_AddBot_FSM(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	// Step 1: /addbot
	postUpdate(t, app, makeUpdate(mainTestAdminID, "/addbot"), "")
	if !contains(tg.lastMessage, "token") {
		t.Errorf("step 1: expected token prompt, got %q", tg.lastMessage)
	}

	// Step 2: send token
	postUpdate(t, app, makeUpdate(mainTestAdminID, "123456:ABC-token"), "")
	if !contains(tg.lastMessage, "name") {
		t.Errorf("step 2: expected name prompt, got %q", tg.lastMessage)
	}

	// Step 3: send name → bot registered
	postUpdate(t, app, makeUpdate(mainTestAdminID, "MyNewBot"), "")
	if !contains(tg.lastMessage, "registered") {
		t.Errorf("step 3: expected registration confirmation, got %q", tg.lastMessage)
	}
}

func TestMainHandler_AddBot_InvalidToken(t *testing.T) {
	tg := &stubTGClient{botIDErr: apperrors.NotFound("bot")}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/addbot"), "")
	postUpdate(t, app, makeUpdate(mainTestAdminID, "bad-token"), "")
	if !contains(tg.lastMessage, "Invalid") {
		t.Errorf("expected invalid token reply, got %q", tg.lastMessage)
	}
}

func TestMainHandler_UnknownCommand_ShowsHelp(t *testing.T) {
	tg := &stubTGClient{}
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, tg)

	postUpdate(t, app, makeUpdate(mainTestAdminID, "/unknown"), "")
	if !contains(tg.lastMessage, "Available commands") {
		t.Errorf("expected help text, got %q", tg.lastMessage)
	}
}

func TestMainHandler_AlwaysReturns200(t *testing.T) {
	app := newMainApp(&stubMainBotSvc{}, &stubMainGameSvc{}, &stubMainLbSvc{}, &stubTGClient{})

	// Malformed body — still 200 to avoid Telegram retries.
	req := httptest.NewRequest(http.MethodPost, "/telegram/main/webhook", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for malformed body, got %d", resp.StatusCode)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// encodeIDTest mirrors the handler's encodeID for constructing test callback data.
func encodeIDTest(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(id[:])
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && (len(s) >= len(sub)) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
