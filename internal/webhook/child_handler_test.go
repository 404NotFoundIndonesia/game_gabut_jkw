package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	lbdomain "github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	"github.com/404NFIDv2/bot-game-management/internal/games"
	sessionapp "github.com/404NFIDv2/bot-game-management/internal/session/application"
	sessiondomain "github.com/404NFIDv2/bot-game-management/internal/session/domain"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
	"github.com/404NFIDv2/bot-game-management/internal/webhook"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// ── stubs ─────────────────────────────────────────────────────────────────────

type stubChildBotLookup struct {
	bot *botdomain.Bot
	err error
}

func (s *stubChildBotLookup) FindByID(_ context.Context, _ uuid.UUID) (*botdomain.Bot, error) {
	return s.bot, s.err
}

type stubChildSessionSvc struct {
	session *sessiondomain.GameSession
	result  *sessionapp.MoveResult
	err     error
}

func (s *stubChildSessionSvc) CreateSession(_ context.Context, _ uuid.UUID, _ sessionapp.CreateSessionRequest) (*sessiondomain.GameSession, error) {
	return s.session, s.err
}
func (s *stubChildSessionSvc) JoinSession(_ context.Context, _, _ uuid.UUID, _ sessionapp.JoinRequest) (*sessiondomain.GameSession, error) {
	return s.session, s.err
}
func (s *stubChildSessionSvc) StartSession(_ context.Context, _, _ uuid.UUID, _ int64) (*sessiondomain.GameSession, error) {
	return s.session, s.err
}
func (s *stubChildSessionSvc) GetSession(_ context.Context, _, _ uuid.UUID) (*sessiondomain.GameSession, error) {
	return s.session, s.err
}
func (s *stubChildSessionSvc) SubmitMove(_ context.Context, _, _ uuid.UUID, _ sessionapp.MoveRequest) (*sessionapp.MoveResult, error) {
	return s.result, s.err
}
func (s *stubChildSessionSvc) EndSession(_ context.Context, _, _ uuid.UUID, _ sessionapp.EndSessionRequest) (*sessiondomain.GameSession, error) {
	return s.session, s.err
}

// ── in-memory TurnStore / GameMsgStore stubs ──────────────────────────────────

type stubTurnStore struct{ m map[string]webhook.TurnContext }

func newStubTurnStore() *stubTurnStore { return &stubTurnStore{m: map[string]webhook.TurnContext{}} }
func (s *stubTurnStore) Set(_ context.Context, botID uuid.UUID, userID int64, tc webhook.TurnContext, _ time.Duration) error {
	s.m[botID.String()+":"+strconv.FormatInt(userID, 10)] = tc
	return nil
}
func (s *stubTurnStore) Get(_ context.Context, botID uuid.UUID, userID int64) (webhook.TurnContext, error) {
	tc, ok := s.m[botID.String()+":"+strconv.FormatInt(userID, 10)]
	if !ok {
		return webhook.TurnContext{}, fmt.Errorf("not found")
	}
	return tc, nil
}
func (s *stubTurnStore) Delete(_ context.Context, botID uuid.UUID, userID int64) error {
	delete(s.m, botID.String()+":"+strconv.FormatInt(userID, 10))
	return nil
}

type stubGameMsgStore struct{ m map[string]int64 }

func newStubGameMsgStore() *stubGameMsgStore { return &stubGameMsgStore{m: map[string]int64{}} }
func (s *stubGameMsgStore) Set(_ context.Context, botID uuid.UUID, chatID, msgID int64, _ time.Duration) error {
	s.m[botID.String()+":"+strconv.FormatInt(chatID, 10)] = msgID
	return nil
}
func (s *stubGameMsgStore) Get(_ context.Context, botID uuid.UUID, chatID int64) (int64, error) {
	v, ok := s.m[botID.String()+":"+strconv.FormatInt(chatID, 10)]
	if !ok {
		return 0, fmt.Errorf("not found")
	}
	return v, nil
}
func (s *stubGameMsgStore) Delete(_ context.Context, botID uuid.UUID, chatID int64) error {
	delete(s.m, botID.String()+":"+strconv.FormatInt(chatID, 10))
	return nil
}

type stubChildLbSvc struct {
	lb  *lbdomain.Leaderboard
	err error
}

func (s *stubChildLbSvc) GetByBot(_ context.Context, _ uuid.UUID, _ pagination.Params) (*lbdomain.Leaderboard, error) {
	return s.lb, s.err
}

// ── helpers ───────────────────────────────────────────────────────────────────

func activeBot() *botdomain.Bot {
	b := botdomain.NewBot("TestBot", botdomain.NewBotToken("enc"), "hash", 42)
	return b
}

// noopDecryptor returns the ciphertext unchanged — tests don't need real decryption
// because the stub TG client ignores the token value entirely.
func noopDecryptor(ciphertext string) (string, error) { return ciphertext, nil }

func newChildApp(
	botLookup webhook.ChildBotLookup,
	sessionSvc webhook.ChildSessionSvc,
	gameSvc webhook.MainGameSvc,
	lbSvc webhook.ChildLeaderboardSvc,
	chatIndex webhook.ChatSessionIndex,
	tg *stubTGClient,
) (*fiber.App, uuid.UUID) {
	botID := uuid.New()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		botLookup, sessionSvc, gameSvc, lbSvc,
		chatIndex, newStubTurnStore(), newStubGameMsgStore(),
		tg, noopDecryptor, nil, 30*time.Minute,
	)
	h.RegisterRoutes(app)
	return app, botID
}

func postChildUpdate(t *testing.T, app *fiber.App, botID uuid.UUID, update telegram.Update) int {
	t.Helper()
	b, _ := json.Marshal(update)
	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+botID.String()+"/webhook", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	return resp.StatusCode
}

func makeChildUpdate(userID int64, firstName, text string) telegram.Update {
	return telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 1,
			From:      &telegram.User{ID: userID, FirstName: firstName},
			Chat:      &telegram.Chat{ID: userID, Type: "private"},
			Text:      text,
		},
	}
}

func activeSession(botID uuid.UUID) *sessiondomain.GameSession {
	return &sessiondomain.GameSession{
		ID:     uuid.New(),
		BotID:  botID,
		Status: sessiondomain.StatusWaiting,
		Players: []sessiondomain.PlayerSession{
			{TelegramUserID: 111, DisplayName: "Alice", Score: 0},
		},
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestChildHandler_InvalidBotID_Returns200(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		newStubTurnStore(), newStubGameMsgStore(),
		&stubTGClient{},
		noopDecryptor,
		nil,
		time.Minute,
	)
	h.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/telegram/child/not-a-uuid/webhook", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestChildHandler_BotNotFound_Returns200(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{err: apperrors.NotFound("bot")},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	sc := postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/newgame uno"))
	if sc != http.StatusOK {
		t.Errorf("expected 200, got %d", sc)
	}
	if tg.sendCalled != 0 {
		t.Error("no message should be sent for missing bot")
	}
}

func TestChildHandler_InactiveBot_Returns200(t *testing.T) {
	bot := activeBot()
	bot.Active = false
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: bot},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/newgame uno"))
	if tg.sendCalled != 0 {
		t.Error("no message should be sent for inactive bot")
	}
}

func TestChildHandler_NonCommandText_Ignored(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "hello world"))
	if tg.sendCalled != 0 {
		t.Error("non-command text should be ignored")
	}
}

func TestChildHandler_NewGame_NoGames(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{}, // botGames nil → "No games assigned"
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/newgame"))
	if tg.sendCalled == 0 {
		t.Error("expected reply when no games assigned")
	}
	if !contains(tg.lastMessage, "No games assigned") {
		t.Errorf("expected no-games reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_NewGame_ListGamesError(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{err: apperrors.Internal("db error")},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/newgame"))
	if !contains(tg.lastMessage, "❌") {
		t.Errorf("expected error reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_NewGame_SingleGame_ImmediateSession(t *testing.T) {
	bid := uuid.New()
	sess := activeSession(bid)
	tg := &stubTGClient{}
	chatIndex := newStubChatIndex()
	gameID := uuid.New()
	botGames := []*gamedomain.BotGame{
		{BotID: bid, GameID: gameID, Game: &gamedomain.Game{ID: gameID, Slug: "uno", Name: "Uno"}},
	}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{session: sess},
		&stubMainGameSvc{botGames: botGames},
		&stubChildLbSvc{},
		chatIndex,
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/newgame"))
	if !contains(tg.lastMessage, "created") {
		t.Errorf("expected creation confirmation, got %q", tg.lastMessage)
	}
	if _, err := chatIndex.Get(context.Background(), botID, 111); err != nil {
		t.Errorf("chatIndex not set after /newgame: %v", err)
	}
}

func TestChildHandler_NewGame_MultipleGames_ShowsKeyboard(t *testing.T) {
	bid := uuid.New()
	tg := &stubTGClient{}
	botGames := []*gamedomain.BotGame{
		{BotID: bid, GameID: uuid.New(), Game: &gamedomain.Game{ID: uuid.New(), Slug: "uno", Name: "Uno"}},
		{BotID: bid, GameID: uuid.New(), Game: &gamedomain.Game{ID: uuid.New(), Slug: "sambung_kata", Name: "Sambung Kata"}},
	}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{botGames: botGames},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/newgame"))
	if !contains(tg.lastMessage, "Choose a game") {
		t.Errorf("expected game-selection keyboard, got %q", tg.lastMessage)
	}
}

func TestChildHandler_NewGame_Callback_Success(t *testing.T) {
	bid := uuid.New()
	sess := activeSession(bid)
	tg := &stubTGClient{}
	chatIndex := newStubChatIndex()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{session: sess},
		&stubMainGameSvc{}, // GetGameBySlug returns a game when err==nil
		&stubChildLbSvc{},
		chatIndex,
		newStubTurnStore(), newStubGameMsgStore(),
		tg,
		noopDecryptor,
		nil,
		30*time.Minute,
	)
	h.RegisterRoutes(app)

	update := telegram.Update{
		CallbackQuery: &telegram.CallbackQuery{
			ID:   "cq1",
			From: &telegram.User{ID: 111, FirstName: "Alice"},
			Message: &telegram.Message{
				MessageID: 10,
				Chat:      &telegram.Chat{ID: 111},
			},
			Data: "cng:uno",
		},
	}
	b, _ := json.Marshal(update)
	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+bid.String()+"/webhook", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	_, _ = app.Test(req, -1)

	if !contains(tg.lastMessage, "created") {
		t.Errorf("expected session created reply via callback, got %q", tg.lastMessage)
	}
	if _, err := chatIndex.Get(context.Background(), bid, 111); err != nil {
		t.Errorf("chatIndex not set after callback: %v", err)
	}
}

func TestChildHandler_Join_NoActiveGame(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(), // empty
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(222, "Bob", "/join"))
	if !contains(tg.lastMessage, "No active game") {
		t.Errorf("expected no-active-game reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_Join_Success(t *testing.T) {
	bid := uuid.New()
	sess := activeSession(bid)
	sess.Players = append(sess.Players, sessiondomain.PlayerSession{TelegramUserID: 222, DisplayName: "Bob"})
	chatIndex := newStubChatIndex()
	_ = chatIndex.Set(context.Background(), bid, 111, sess.ID, time.Hour)

	tg := &stubTGClient{}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	bot := activeBot()
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{bot: bot},
		&stubChildSessionSvc{session: sess},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		chatIndex,
		newStubTurnStore(), newStubGameMsgStore(),
		tg,
		noopDecryptor,
		nil,
		time.Minute,
	)
	h.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+bid.String()+"/webhook",
		func() *bytes.Reader { b, _ := json.Marshal(makeChildUpdate(111, "Bob", "/join")); return bytes.NewReader(b) }())
	req.Header.Set("Content-Type", "application/json")
	_, _ = app.Test(req, -1)

	if !contains(tg.lastMessage, "joined") {
		t.Errorf("expected joined reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_Start_NoActiveGame(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/start"))
	if !contains(tg.lastMessage, "No active game") {
		t.Errorf("expected no-active-game reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_Start_ServiceError(t *testing.T) {
	bid := uuid.New()
	chatIndex := newStubChatIndex()
	_ = chatIndex.Set(context.Background(), bid, 111, uuid.New(), time.Hour)

	tg := &stubTGClient{}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{err: apperrors.Conflict("not enough players")},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		chatIndex,
		newStubTurnStore(), newStubGameMsgStore(),
		tg,
		noopDecryptor,
		nil,
		time.Minute,
	)
	h.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+bid.String()+"/webhook",
		func() *bytes.Reader { b, _ := json.Marshal(makeChildUpdate(111, "Alice", "/start")); return bytes.NewReader(b) }())
	req.Header.Set("Content-Type", "application/json")
	_, _ = app.Test(req, -1)

	if !contains(tg.lastMessage, "❌") {
		t.Errorf("expected error reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_Move_NoActiveGame(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/move draw"))
	if !contains(tg.lastMessage, "No active game") {
		t.Errorf("expected no-active-game reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_Move_Success(t *testing.T) {
	bid := uuid.New()
	sess := activeSession(bid)
	chatIndex := newStubChatIndex()
	_ = chatIndex.Set(context.Background(), bid, 111, sess.ID, time.Hour)

	result := &sessionapp.MoveResult{
		Session: sess,
		Events:  []games.Event{{Type: "card_played"}},
	}
	tg := &stubTGClient{}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{session: sess, result: result},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		chatIndex,
		newStubTurnStore(), newStubGameMsgStore(),
		tg,
		noopDecryptor,
		nil,
		time.Minute,
	)
	h.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+bid.String()+"/webhook",
		func() *bytes.Reader { b, _ := json.Marshal(makeChildUpdate(111, "Alice", "/move draw")); return bytes.NewReader(b) }())
	req.Header.Set("Content-Type", "application/json")
	_, _ = app.Test(req, -1)

	if !contains(tg.lastMessage, "card_played") {
		t.Errorf("expected event in reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_Move_GameOver(t *testing.T) {
	bid := uuid.New()
	sess := activeSession(bid)
	sess.Status = sessiondomain.StatusFinished
	chatIndex := newStubChatIndex()
	_ = chatIndex.Set(context.Background(), bid, 111, sess.ID, time.Hour)

	result := &sessionapp.MoveResult{Session: sess, Events: nil}
	tg := &stubTGClient{}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{session: sess, result: result},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		chatIndex,
		newStubTurnStore(), newStubGameMsgStore(),
		tg,
		noopDecryptor,
		nil,
		time.Minute,
	)
	h.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+bid.String()+"/webhook",
		func() *bytes.Reader { b, _ := json.Marshal(makeChildUpdate(111, "Alice", "/move draw")); return bytes.NewReader(b) }())
	req.Header.Set("Content-Type", "application/json")
	_, _ = app.Test(req, -1)

	if !contains(tg.lastMessage, "Game over") {
		t.Errorf("expected game-over message, got %q", tg.lastMessage)
	}
}

func TestChildHandler_End_NoActiveGame(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/end"))
	if !contains(tg.lastMessage, "No active game") {
		t.Errorf("expected no-active-game reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_End_Success(t *testing.T) {
	bid := uuid.New()
	sess := activeSession(bid)
	chatIndex := newStubChatIndex()
	_ = chatIndex.Set(context.Background(), bid, 111, sess.ID, time.Hour)

	tg := &stubTGClient{}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{session: sess},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		chatIndex,
		newStubTurnStore(), newStubGameMsgStore(),
		tg,
		noopDecryptor,
		nil,
		time.Minute,
	)
	h.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+bid.String()+"/webhook",
		func() *bytes.Reader { b, _ := json.Marshal(makeChildUpdate(111, "Alice", "/end")); return bytes.NewReader(b) }())
	req.Header.Set("Content-Type", "application/json")
	_, _ = app.Test(req, -1)

	if !contains(tg.lastMessage, "ended") {
		t.Errorf("expected game-ended reply, got %q", tg.lastMessage)
	}
	// chatIndex must be cleared
	if _, err := chatIndex.Get(context.Background(), bid, 111); err == nil {
		t.Error("chatIndex should be cleared after /end")
	}
}

func TestChildHandler_Leaderboard(t *testing.T) {
	lb := &lbdomain.Leaderboard{
		Entries: []lbdomain.LeaderboardEntry{{Rank: 1, DisplayName: "Alice", TotalScore: 50, Wins: 2}},
	}
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{lb: lb},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/leaderboard"))
	if !contains(tg.lastMessage, "Alice") {
		t.Errorf("expected leaderboard reply with entry, got %q", tg.lastMessage)
	}
}

func TestChildHandler_Leaderboard_Error(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{err: apperrors.Internal("db error")},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/leaderboard"))
	if !contains(tg.lastMessage, "❌") {
		t.Errorf("expected error reply, got %q", tg.lastMessage)
	}
}

func TestChildHandler_UnknownCommand_ShowsHelp(t *testing.T) {
	tg := &stubTGClient{}
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		tg,
	)
	postChildUpdate(t, app, botID, makeChildUpdate(111, "Alice", "/unknown"))
	if !contains(tg.lastMessage, "Available commands") {
		t.Errorf("expected help text, got %q", tg.lastMessage)
	}
}

func TestChildHandler_AlwaysReturns200(t *testing.T) {
	app, botID := newChildApp(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		newStubChatIndex(),
		&stubTGClient{},
	)
	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+botID.String()+"/webhook",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for malformed body, got %d", resp.StatusCode)
	}
}

func TestChildHandler_Move_JSONPayload(t *testing.T) {
	bid := uuid.New()
	sess := activeSession(bid)
	chatIndex := newStubChatIndex()
	_ = chatIndex.Set(context.Background(), bid, 111, sess.ID, time.Hour)

	result := &sessionapp.MoveResult{Session: sess, Events: nil}
	tg := &stubTGClient{}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := webhook.NewChildBotHandler(
		&stubChildBotLookup{bot: activeBot()},
		&stubChildSessionSvc{session: sess, result: result},
		&stubMainGameSvc{},
		&stubChildLbSvc{},
		chatIndex,
		newStubTurnStore(), newStubGameMsgStore(),
		tg,
		noopDecryptor,
		nil,
		time.Minute,
	)
	h.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/telegram/child/"+bid.String()+"/webhook",
		func() *bytes.Reader {
			b, _ := json.Marshal(makeChildUpdate(111, "Alice", `/move {"action":"draw","count":2}`))
			return bytes.NewReader(b)
		}())
	req.Header.Set("Content-Type", "application/json")
	_, _ = app.Test(req, -1)

	if !contains(tg.lastMessage, "Move applied") {
		t.Errorf("expected move-applied reply, got %q", tg.lastMessage)
	}
}
