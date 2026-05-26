package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	lbdomain "github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	sessionapp "github.com/404NFIDv2/bot-game-management/internal/session/application"
	sessiondomain "github.com/404NFIDv2/bot-game-management/internal/session/domain"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// ── Service interfaces ────────────────────────────────────────────────────────

// ChildBotLookup resolves a bot by ID for child webhook routing.
type ChildBotLookup interface {
	FindByID(ctx context.Context, id uuid.UUID) (*botdomain.Bot, error)
}

// ChildSessionSvc is the SessionService subset needed by the child handler.
type ChildSessionSvc interface {
	CreateSession(ctx context.Context, botID uuid.UUID, req sessionapp.CreateSessionRequest) (*sessiondomain.GameSession, error)
	JoinSession(ctx context.Context, botID, sessionID uuid.UUID, req sessionapp.JoinRequest) (*sessiondomain.GameSession, error)
	StartSession(ctx context.Context, botID, sessionID uuid.UUID, callerTelegramID int64) (*sessiondomain.GameSession, error)
	SubmitMove(ctx context.Context, botID, sessionID uuid.UUID, req sessionapp.MoveRequest) (*sessionapp.MoveResult, error)
	EndSession(ctx context.Context, botID, sessionID uuid.UUID, req sessionapp.EndSessionRequest) (*sessiondomain.GameSession, error)
}

// ChildLeaderboardSvc is the leaderboard interface needed by the child handler.
type ChildLeaderboardSvc interface {
	GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*lbdomain.Leaderboard, error)
}

// ── Handler ───────────────────────────────────────────────────────────────────

// TokenDecryptor decrypts an AES-256-GCM ciphertext back to a raw Telegram bot token.
type TokenDecryptor func(ciphertext string) (string, error)

// ChildBotHandler handles Telegram updates sent to child game bot webhooks.
type ChildBotHandler struct {
	botRepo        ChildBotLookup
	sessionSvc     ChildSessionSvc
	gameSvc        MainGameSvc // reuses the same interface defined in main_handler.go
	lbSvc          ChildLeaderboardSvc
	chatIndex      ChatSessionIndex
	tgClient       telegram.Client
	tokenDecryptor TokenDecryptor
	sessionTTL     time.Duration
}

// NewChildBotHandler constructs the ChildBotHandler.
func NewChildBotHandler(
	botRepo ChildBotLookup,
	sessionSvc ChildSessionSvc,
	gameSvc MainGameSvc,
	lbSvc ChildLeaderboardSvc,
	chatIndex ChatSessionIndex,
	tgClient telegram.Client,
	tokenDecryptor TokenDecryptor,
	sessionTTL time.Duration,
) *ChildBotHandler {
	return &ChildBotHandler{
		botRepo:        botRepo,
		sessionSvc:     sessionSvc,
		gameSvc:        gameSvc,
		lbSvc:          lbSvc,
		chatIndex:      chatIndex,
		tgClient:       tgClient,
		tokenDecryptor: tokenDecryptor,
		sessionTTL:     sessionTTL,
	}
}

// RegisterRoutes mounts POST /telegram/child/:bot_id/webhook.
func (h *ChildBotHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/telegram/child/:bot_id/webhook", h.handleUpdate)
}

func (h *ChildBotHandler) handleUpdate(c *fiber.Ctx) error {
	botIDStr := c.Params("bot_id")
	botID, err := uuid.Parse(botIDStr)
	if err != nil {
		slog.Error("child handler: invalid bot_id param", "bot_id", botIDStr)
		return c.SendStatus(fiber.StatusOK)
	}

	slog.Info("child handler: update received", "bot_id", botID)

	bot, err := h.botRepo.FindByID(c.Context(), botID)
	if err != nil {
		slog.Error("child handler: bot not found", "bot_id", botID, "err", err)
		return c.SendStatus(fiber.StatusOK)
	}
	if !bot.Active {
		slog.Error("child handler: bot inactive", "bot_id", botID)
		return c.SendStatus(fiber.StatusOK)
	}

	var update telegram.Update
	if err := c.BodyParser(&update); err != nil {
		slog.Error("child handler: body parse failed", "bot_id", botID, "err", err)
		return c.SendStatus(fiber.StatusOK)
	}

	ctx := c.Context()

	switch {
	case update.Message != nil && update.Message.From != nil:
		h.handleMessage(ctx, botID, bot, update.Message)
	case update.CallbackQuery != nil:
		h.handleCallbackQuery(ctx, botID, bot, update.CallbackQuery)
	default:
		slog.Info("child handler: non-actionable update, skipping", "bot_id", botID)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *ChildBotHandler) handleMessage(ctx context.Context, botID uuid.UUID, bot *botdomain.Bot, msg *telegram.Message) {
	userID := msg.From.ID
	displayName := msg.From.FirstName
	if msg.From.LastName != "" {
		displayName += " " + msg.From.LastName
	}
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	chatType := msg.Chat.Type
	slog.Info("child handler: message received", "bot_id", botID, "chat_id", chatID, "user_id", userID, "text", text, "chat_type", chatType)

	if !strings.HasPrefix(text, "/") {
		return
	}

	cmd, args := parseCommand(text)
	slog.Info("child handler: dispatching command", "bot_id", botID, "cmd", cmd)
	switch cmd {
	case "/newgame":
		h.cmdNewGame(ctx, botID, chatID, userID, displayName, bot)
	case "/join":
		h.cmdJoin(ctx, botID, chatID, userID, displayName, bot)
	case "/start":
		h.cmdStart(ctx, botID, chatID, userID, bot)
	case "/move":
		h.cmdMove(ctx, botID, chatID, userID, args, bot)
	case "/end":
		h.cmdEnd(ctx, botID, chatID, userID, bot)
	case "/leaderboard":
		h.cmdLeaderboardChild(ctx, botID, chatID, bot)
	default:
		if chatType == "private" {
			h.childReply(ctx, bot, chatID, childHelpText())
		}
	}
}

func (h *ChildBotHandler) handleCallbackQuery(ctx context.Context, botID uuid.UUID, bot *botdomain.Bot, cq *telegram.CallbackQuery) {
	_ = h.childAnswerCallback(ctx, bot, cq.ID)

	if cq.Message == nil || cq.From == nil {
		return
	}

	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID
	userID := cq.From.ID
	displayName := cq.From.FirstName
	if cq.From.LastName != "" {
		displayName += " " + cq.From.LastName
	}

	if strings.HasPrefix(cq.Data, "cng:") {
		h.cbNewGame(ctx, botID, chatID, msgID, userID, displayName, cq.Data[4:], bot)
	}
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (h *ChildBotHandler) cmdNewGame(ctx context.Context, botID uuid.UUID, chatID, userID int64, displayName string, bot *botdomain.Bot) {
	bgs, err := h.gameSvc.ListBotGames(ctx, botID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bgs) == 0 {
		h.childReply(ctx, bot, chatID, "No games assigned to this bot yet.")
		return
	}

	// Single game → create session immediately.
	if len(bgs) == 1 {
		h.createSession(ctx, botID, chatID, userID, displayName, bgs[0].Game, bot)
		return
	}

	// Multiple games → let user pick.
	var rows [][]telegram.InlineKeyboardButton
	for _, bg := range bgs {
		label := gameLabel(string(bg.Game.Slug))
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: label, CallbackData: "cng:" + string(bg.Game.Slug)},
		})
	}
	h.childSendWithKeyboard(ctx, bot, chatID, "Choose a game:", telegram.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (h *ChildBotHandler) cbNewGame(ctx context.Context, botID uuid.UUID, chatID, msgID, userID int64, displayName, slug string, bot *botdomain.Bot) {
	game, err := h.gameSvc.GetGameBySlug(ctx, gamedomain.GameSlug(slug))
	if err != nil {
		h.childEditMessage(ctx, bot, chatID, msgID, "❌ Unknown game.", nil)
		return
	}

	session, err := h.sessionSvc.CreateSession(ctx, botID, sessionapp.CreateSessionRequest{
		GameID:         game.ID,
		ChatID:         chatID,
		TelegramUserID: userID,
		DisplayName:    displayName,
	})
	if err != nil {
		h.childEditMessage(ctx, bot, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}

	_ = h.chatIndex.Set(ctx, botID, chatID, session.ID, h.sessionTTL)
	h.childEditMessage(ctx, bot, chatID, msgID, fmt.Sprintf(
		"🎮 Game %q created!\nHost: %s\nOthers: send /join to play.\nHost: send /start when ready.",
		game.Slug, displayName,
	), nil)
}

// createSession is the shared helper used by cmdNewGame (single-game path) and cbNewGame.
func (h *ChildBotHandler) createSession(ctx context.Context, botID uuid.UUID, chatID, userID int64, displayName string, game *gamedomain.Game, bot *botdomain.Bot) {
	session, err := h.sessionSvc.CreateSession(ctx, botID, sessionapp.CreateSessionRequest{
		GameID:         game.ID,
		ChatID:         chatID,
		TelegramUserID: userID,
		DisplayName:    displayName,
	})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}

	_ = h.chatIndex.Set(ctx, botID, chatID, session.ID, h.sessionTTL)
	h.childReply(ctx, bot, chatID, fmt.Sprintf(
		"🎮 Game %q created!\nHost: %s\nOthers: send /join to play.\nHost: send /start when ready.",
		game.Slug, displayName,
	))
}

func (h *ChildBotHandler) cmdJoin(ctx context.Context, botID uuid.UUID, chatID, userID int64, displayName string, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, chatID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "No active game in this chat. Use /newgame to start one.")
		return
	}
	session, err := h.sessionSvc.JoinSession(ctx, botID, sessionID, sessionapp.JoinRequest{
		TelegramUserID: userID,
		DisplayName:    displayName,
	})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	h.childReply(ctx, bot, chatID, fmt.Sprintf("✅ %s joined! Players: %d", displayName, len(session.Players)))
}

func (h *ChildBotHandler) cmdStart(ctx context.Context, botID uuid.UUID, chatID, userID int64, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, chatID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "No active game in this chat.")
		return
	}
	if _, err := h.sessionSvc.StartSession(ctx, botID, sessionID, userID); err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	h.childReply(ctx, bot, chatID, "🎮 Game started! Submit moves with /move <payload>")
}

func (h *ChildBotHandler) cmdMove(ctx context.Context, botID uuid.UUID, chatID, userID int64, args []string, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, chatID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "No active game in this chat.")
		return
	}

	payload := map[string]any{}
	if len(args) > 0 {
		raw := strings.Join(args, " ")
		if jsonErr := json.Unmarshal([]byte(raw), &payload); jsonErr != nil {
			payload["action"] = args[0]
			if len(args) > 1 {
				payload["value"] = strings.Join(args[1:], " ")
			}
		}
	}

	result, err := h.sessionSvc.SubmitMove(ctx, botID, sessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  payload,
	})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}

	var sb strings.Builder
	for _, ev := range result.Events {
		fmt.Fprintf(&sb, "▶ %s\n", ev.Type)
	}
	if sb.Len() == 0 {
		sb.WriteString("✅ Move applied.")
	}
	if result.Session.Status == sessiondomain.StatusFinished {
		sb.WriteString("\n🏆 Game over! Use /leaderboard to see scores.")
	}
	h.childReply(ctx, bot, chatID, sb.String())
}

func (h *ChildBotHandler) cmdEnd(ctx context.Context, botID uuid.UUID, chatID, userID int64, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, chatID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "No active game in this chat.")
		return
	}
	session, err := h.sessionSvc.EndSession(ctx, botID, sessionID, sessionapp.EndSessionRequest{
		CallerTelegramID: userID,
		Reason:           "ended by player",
	})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	_ = h.chatIndex.Delete(ctx, botID, chatID)

	var sb strings.Builder
	sb.WriteString("🏁 Game ended! Final scores:\n\n")
	for _, p := range session.Players {
		fmt.Fprintf(&sb, "• %s: %d\n", p.DisplayName, p.Score)
	}
	h.childReply(ctx, bot, chatID, sb.String())
}

func (h *ChildBotHandler) cmdLeaderboardChild(ctx context.Context, botID uuid.UUID, chatID int64, bot *botdomain.Bot) {
	lb, err := h.lbSvc.GetByBot(ctx, botID, pagination.Params{Limit: 10, Offset: 0})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	h.childReply(ctx, bot, chatID, "🏆 Leaderboard:\n\n"+formatLeaderboard(lb))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (h *ChildBotHandler) childToken(bot *botdomain.Bot) (string, error) {
	return h.tokenDecryptor(bot.Token.Ciphertext())
}

func (h *ChildBotHandler) childReply(ctx context.Context, bot *botdomain.Bot, chatID int64, text string) {
	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: failed to decrypt bot token", "bot_id", bot.ID, "err", err)
		return
	}
	slog.Info("child handler: sending reply", "bot_id", bot.ID, "chat_id", chatID)
	if err := h.tgClient.SendMessage(ctx, rawToken, chatID, text); err != nil {
		slog.Error("child handler: send message failed", "bot_id", bot.ID, "chat_id", chatID, "err", err)
	}
}

func (h *ChildBotHandler) childSendWithKeyboard(ctx context.Context, bot *botdomain.Bot, chatID int64, text string, kb telegram.InlineKeyboardMarkup) {
	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: failed to decrypt bot token", "bot_id", bot.ID, "err", err)
		return
	}
	if err := h.tgClient.SendMessageWithKeyboard(ctx, rawToken, chatID, text, kb); err != nil {
		slog.Error("child handler: send keyboard failed", "bot_id", bot.ID, "chat_id", chatID, "err", err)
	}
}

func (h *ChildBotHandler) childEditMessage(ctx context.Context, bot *botdomain.Bot, chatID, msgID int64, text string, kb *telegram.InlineKeyboardMarkup) {
	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: failed to decrypt bot token", "bot_id", bot.ID, "err", err)
		return
	}
	if err := h.tgClient.EditMessageText(ctx, rawToken, chatID, msgID, text, kb); err != nil {
		slog.Error("child handler: edit message failed", "bot_id", bot.ID, "chat_id", chatID, "err", err)
	}
}

func (h *ChildBotHandler) childAnswerCallback(ctx context.Context, bot *botdomain.Bot, callbackQueryID string) error {
	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: failed to decrypt bot token", "bot_id", bot.ID, "err", err)
		return err
	}
	return h.tgClient.AnswerCallbackQuery(ctx, rawToken, callbackQueryID)
}

func gameLabel(slug string) string {
	switch slug {
	case "uno":
		return "🃏 Uno"
	case "sambung_kata":
		return "📝 Sambung Kata"
	case "truth_or_date":
		return "🎯 Truth or Date"
	default:
		return slug
	}
}

func childHelpText() string {
	return `Available commands:

/newgame — start a new game
/join — join the active game in this chat
/start — start the game (host only)
/move <payload> — submit a move (JSON or "action value")
/end — end the current game
/leaderboard — show bot leaderboard`
}
