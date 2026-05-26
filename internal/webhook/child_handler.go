package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	sambungkata "github.com/404NFIDv2/bot-game-management/internal/games/sambung_kata"
	truthordate "github.com/404NFIDv2/bot-game-management/internal/games/truth_or_date"
	"github.com/404NFIDv2/bot-game-management/internal/games/uno"
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
	GetSession(ctx context.Context, botID, sessionID uuid.UUID) (*sessiondomain.GameSession, error)
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
	gameSvc        MainGameSvc
	lbSvc          ChildLeaderboardSvc
	chatIndex      ChatSessionIndex
	turnStore      TurnStore
	gameMsgStore   GameMsgStore
	tgClient       telegram.Client
	tokenDecryptor TokenDecryptor
	stickerMap     UnoStickerMap
	sessionTTL     time.Duration
}

// NewChildBotHandler constructs the ChildBotHandler.
func NewChildBotHandler(
	botRepo ChildBotLookup,
	sessionSvc ChildSessionSvc,
	gameSvc MainGameSvc,
	lbSvc ChildLeaderboardSvc,
	chatIndex ChatSessionIndex,
	turnStore TurnStore,
	gameMsgStore GameMsgStore,
	tgClient telegram.Client,
	tokenDecryptor TokenDecryptor,
	stickerMap UnoStickerMap,
	sessionTTL time.Duration,
) *ChildBotHandler {
	return &ChildBotHandler{
		botRepo:        botRepo,
		sessionSvc:     sessionSvc,
		gameSvc:        gameSvc,
		lbSvc:          lbSvc,
		chatIndex:      chatIndex,
		turnStore:      turnStore,
		gameMsgStore:   gameMsgStore,
		tgClient:       tgClient,
		tokenDecryptor: tokenDecryptor,
		stickerMap:     stickerMap,
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

// ── Message dispatcher ────────────────────────────────────────────────────────

func (h *ChildBotHandler) handleMessage(ctx context.Context, botID uuid.UUID, bot *botdomain.Bot, msg *telegram.Message) {
	userID := msg.From.ID
	displayName := msg.From.FirstName
	if msg.From.LastName != "" {
		displayName += " " + msg.From.LastName
	}
	chatID := msg.Chat.ID
	chatType := msg.Chat.Type
	text := strings.TrimSpace(msg.Text)

	slog.Info("child handler: message received", "bot_id", botID, "chat_id", chatID, "user_id", userID, "text", text, "chat_type", chatType)

	if !strings.HasPrefix(text, "/") {
		h.handleTextMove(ctx, botID, chatID, userID, text, bot)
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

// ── Callback dispatcher ───────────────────────────────────────────────────────

func (h *ChildBotHandler) handleCallbackQuery(ctx context.Context, botID uuid.UUID, bot *botdomain.Bot, cq *telegram.CallbackQuery) {
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
	data := cq.Data

	slog.Info("child handler: callback", "bot_id", botID, "data", data, "user_id", userID)

	switch {
	case data == "vhand":
		h.cbViewHand(ctx, botID, chatID, msgID, userID, cq.ID, bot)
	case strings.HasPrefix(data, "uplay:"):
		h.cbPlayCard(ctx, botID, userID, cq.ID, data[6:], bot)
	case data == "udraw":
		h.cbDraw(ctx, botID, userID, cq.ID, bot)
	case strings.HasPrefix(data, "uwild:"):
		h.cbWildSelect(ctx, botID, chatID, msgID, userID, cq.ID, data[6:], bot)
	case strings.HasPrefix(data, "ucolor:"):
		h.cbWildColor(ctx, botID, chatID, msgID, userID, cq.ID, data[7:], bot)
	case strings.HasPrefix(data, "cng:"):
		h.cbNewGame(ctx, botID, chatID, msgID, userID, displayName, data[4:], bot)
	case strings.HasPrefix(data, "tdchoice:"):
		h.cbTDChoice(ctx, botID, chatID, msgID, userID, cq.ID, data[9:], bot)
	case data == "tdskip":
		h.cbTDSkip(ctx, botID, chatID, msgID, userID, cq.ID, bot)
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
	if len(bgs) == 1 {
		h.createSession(ctx, botID, chatID, userID, displayName, bgs[0].Game, bot)
		return
	}
	var rows [][]telegram.InlineKeyboardButton
	for _, bg := range bgs {
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: gameLabel(string(bg.Game.Slug)), CallbackData: "cng:" + string(bg.Game.Slug)},
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
	session, err := h.sessionSvc.StartSession(ctx, botID, sessionID, userID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}

	game, err := h.gameSvc.GetGame(ctx, session.GameID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "🎮 Game started!")
		return
	}
	switch game.Slug {
	case gamedomain.SlugUno:
		h.startUno(ctx, botID, chatID, session, bot)
	case gamedomain.SlugSambungKata:
		h.startSambungKata(ctx, botID, chatID, session, bot)
	case gamedomain.SlugTruthOrDate:
		h.startTruthOrDate(ctx, botID, chatID, session, bot)
	default:
		h.childReply(ctx, bot, chatID, "🎮 Game started! Submit moves with /move <payload>")
	}
}

func (h *ChildBotHandler) startUno(ctx context.Context, botID uuid.UUID, chatID int64, session *sessiondomain.GameSession, bot *botdomain.Bot) {
	state, err := parseUnoState(session.State)
	if err != nil {
		h.childReply(ctx, bot, chatID, "🎮 Uno started!")
		return
	}
	playerName := playerDisplayName(session, state.PlayerOrder[state.CurrentTurnIdx])
	text := unoTurnText(state.DiscardPile[len(state.DiscardPile)-1], playerName, len(state.Hands[state.PlayerOrder[state.CurrentTurnIdx]]))
	msgID, err := h.childSendGetID(ctx, bot, chatID, text, unoViewHandKeyboard)
	if err == nil {
		_ = h.gameMsgStore.Set(ctx, botID, chatID, msgID, h.sessionTTL)
	}
}

func (h *ChildBotHandler) startSambungKata(ctx context.Context, botID uuid.UUID, chatID int64, session *sessiondomain.GameSession, bot *botdomain.Bot) {
	state, err := parseSKState(session.State)
	if err != nil {
		h.childReply(ctx, bot, chatID, "🎮 Sambung Kata started!")
		return
	}
	playerName := playerDisplayName(session, state.PlayerOrder[state.CurrentTurnIdx])
	text := skTurnText(playerName, state.LastWord)
	msgID, err := h.childSendGetID(ctx, bot, chatID, text, telegram.InlineKeyboardMarkup{})
	if err == nil {
		_ = h.gameMsgStore.Set(ctx, botID, chatID, msgID, h.sessionTTL)
	}
}

func (h *ChildBotHandler) startTruthOrDate(ctx context.Context, botID uuid.UUID, chatID int64, session *sessiondomain.GameSession, bot *botdomain.Bot) {
	state, err := parseTDState(session.State)
	if err != nil {
		h.childReply(ctx, bot, chatID, "🎮 Truth or Date started!")
		return
	}
	playerName := playerDisplayName(session, state.PlayerOrder[state.CurrentTurnIdx])
	text := tdTurnText(playerName, state.Round)
	kb := tdChoiceKeyboard()
	msgID, err := h.childSendGetID(ctx, bot, chatID, text, kb)
	if err == nil {
		_ = h.gameMsgStore.Set(ctx, botID, chatID, msgID, h.sessionTTL)
	}
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

	// Edit the turn message to show final result (remove button).
	if msgID, err := h.gameMsgStore.Get(ctx, botID, chatID); err == nil {
		h.childEditMessage(ctx, bot, chatID, msgID, sb.String(), nil)
		_ = h.gameMsgStore.Delete(ctx, botID, chatID)
	} else {
		h.childReply(ctx, bot, chatID, sb.String())
	}
}

func (h *ChildBotHandler) cmdLeaderboardChild(ctx context.Context, botID uuid.UUID, chatID int64, bot *botdomain.Bot) {
	lb, err := h.lbSvc.GetByBot(ctx, botID, pagination.Params{Limit: 10, Offset: 0})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	h.childReply(ctx, bot, chatID, "🏆 Leaderboard:\n\n"+formatLeaderboard(lb))
}

// ── Uno turn callbacks ────────────────────────────────────────────────────────

// cbViewHand is triggered when a player taps "🃏 View my hand" in the group.
func (h *ChildBotHandler) cbViewHand(ctx context.Context, botID uuid.UUID, groupChatID, groupMsgID, userID int64, callbackID string, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, groupChatID)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "No active game in this chat.")
		return
	}

	session, err := h.sessionSvc.GetSession(ctx, botID, sessionID)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "Could not load session.")
		return
	}

	state, err := parseUnoState(session.State)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "Game state error.")
		return
	}

	if state.PlayerOrder[state.CurrentTurnIdx] != userID {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "⏳ Not your turn!")
		return
	}

	// Silently ack the callback.
	_ = h.childAnswerCallback(ctx, bot, callbackID)

	hand := state.Hands[userID]
	top := state.DiscardPile[len(state.DiscardPile)-1]
	dmMsgID := h.sendHandDM(ctx, bot, userID, hand, top)

	// Store turn context so DM callbacks can resolve back to the group.
	_ = h.turnStore.Set(ctx, botID, userID, TurnContext{
		GroupChatID:     groupChatID,
		GroupMsgID:      groupMsgID,
		SessionID:       sessionID,
		DMKeyboardMsgID: dmMsgID,
	}, h.sessionTTL)
}

// cbPlayCard processes a card play from the player's DM.
func (h *ChildBotHandler) cbPlayCard(ctx context.Context, botID uuid.UUID, userID int64, callbackID, idxStr string, bot *botdomain.Bot) {
	tc, err := h.turnStore.Get(ctx, botID, userID)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "Turn expired. Tap 'View my hand' again.")
		return
	}

	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		_ = h.childAnswerCallback(ctx, bot, callbackID)
		return
	}

	_ = h.childAnswerCallback(ctx, bot, callbackID)

	result, err := h.sessionSvc.SubmitMove(ctx, botID, tc.SessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  map[string]any{"action": "play_card", "card_index": idx},
	})
	if err != nil {
		h.childEditMessage(ctx, bot, userID, tc.DMKeyboardMsgID, "❌ "+extractMsg(err), nil)
		return
	}

	h.childEditMessage(ctx, bot, userID, tc.DMKeyboardMsgID, "✅ Card played!", nil)
	_ = h.turnStore.Delete(ctx, botID, userID)
	h.advanceTurn(ctx, botID, tc, result, bot)
}

// cbDraw processes a draw action from the player's DM.
func (h *ChildBotHandler) cbDraw(ctx context.Context, botID uuid.UUID, userID int64, callbackID string, bot *botdomain.Bot) {
	tc, err := h.turnStore.Get(ctx, botID, userID)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "Turn expired. Tap 'View my hand' again.")
		return
	}

	_ = h.childAnswerCallback(ctx, bot, callbackID)

	result, err := h.sessionSvc.SubmitMove(ctx, botID, tc.SessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  map[string]any{"action": "draw"},
	})
	if err != nil {
		h.childEditMessage(ctx, bot, userID, tc.DMKeyboardMsgID, "❌ "+extractMsg(err), nil)
		return
	}

	h.childEditMessage(ctx, bot, userID, tc.DMKeyboardMsgID, "🃏 Drew a card.", nil)
	_ = h.turnStore.Delete(ctx, botID, userID)
	h.advanceTurn(ctx, botID, tc, result, bot)
}

// cbWildSelect shows the color picker when a wild card is tapped.
func (h *ChildBotHandler) cbWildSelect(ctx context.Context, botID uuid.UUID, chatID, msgID, userID int64, callbackID, idxStr string, bot *botdomain.Bot) {
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		_ = h.childAnswerCallback(ctx, bot, callbackID)
		return
	}
	_ = h.childAnswerCallback(ctx, bot, callbackID)
	kb := unoColorKeyboard(idx)
	h.childEditMessage(ctx, bot, chatID, msgID, "🌈 Choose a color for your wild card:", &kb)
}

// cbWildColor finalizes the wild card play after the player picks a color.
// data format: "<card_idx>:<color>"
func (h *ChildBotHandler) cbWildColor(ctx context.Context, botID uuid.UUID, chatID, msgID, userID int64, callbackID, data string, bot *botdomain.Bot) {
	tc, err := h.turnStore.Get(ctx, botID, userID)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "Turn expired. Tap 'View my hand' again.")
		return
	}

	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		_ = h.childAnswerCallback(ctx, bot, callbackID)
		return
	}
	idx, err := strconv.Atoi(parts[0])
	if err != nil {
		_ = h.childAnswerCallback(ctx, bot, callbackID)
		return
	}
	color := parts[1]

	_ = h.childAnswerCallback(ctx, bot, callbackID)

	result, err := h.sessionSvc.SubmitMove(ctx, botID, tc.SessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload: map[string]any{
			"action":       "play_card",
			"card_index":   idx,
			"chosen_color": color,
		},
	})
	if err != nil {
		h.childEditMessage(ctx, bot, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}

	colorEmoji := map[string]string{"red": "🔴", "blue": "🔵", "yellow": "🟡", "green": "🟢"}
	h.childEditMessage(ctx, bot, chatID, msgID, "✅ Wild played! Color: "+colorEmoji[color], nil)
	_ = h.turnStore.Delete(ctx, botID, userID)
	h.advanceTurn(ctx, botID, tc, result, bot)
}

// advanceTurn updates the group message after any move.
func (h *ChildBotHandler) advanceTurn(ctx context.Context, botID uuid.UUID, tc TurnContext, result *sessionapp.MoveResult, bot *botdomain.Bot) {
	session := result.Session

	if session.Status == sessiondomain.StatusFinished {
		// Find winner name.
		var winnerName string
		for _, ev := range result.Events {
			if ev.Type == "PLAYER_WON" {
				if id, ok := ev.Payload["player_id"].(float64); ok {
					winnerName = playerDisplayName(session, int64(id))
				}
			}
		}
		if winnerName == "" {
			winnerName = "Someone"
		}
		text := unoGameOverText(winnerName) + "\n\nFinal scores:\n" + finalScores(session)
		h.childEditMessage(ctx, bot, tc.GroupChatID, tc.GroupMsgID, text, nil)
		_ = h.gameMsgStore.Delete(ctx, botID, tc.GroupChatID)
		_ = h.chatIndex.Delete(ctx, botID, tc.GroupChatID)
		return
	}

	state, err := parseUnoState(session.State)
	if err != nil {
		return
	}
	currentPlayerID := state.PlayerOrder[state.CurrentTurnIdx]
	playerName := playerDisplayName(session, currentPlayerID)
	handSize := len(state.Hands[currentPlayerID])
	text := unoTurnText(state.DiscardPile[len(state.DiscardPile)-1], playerName, handSize)
	h.childEditMessage(ctx, bot, tc.GroupChatID, tc.GroupMsgID, text, &unoViewHandKeyboard)
}

// sendHandDM sends the player's hand in a private DM and returns the keyboard message ID.
func (h *ChildBotHandler) sendHandDM(ctx context.Context, bot *botdomain.Bot, userID int64, hand []uno.Card, top uno.Card) int64 {
	if len(h.stickerMap) > 0 {
		return h.sendHandDMStickers(ctx, bot, userID, hand, top)
	}
	return h.sendHandDMKeyboard(ctx, bot, userID, hand, top)
}

func (h *ChildBotHandler) sendHandDMKeyboard(ctx context.Context, bot *botdomain.Bot, userID int64, hand []uno.Card, top uno.Card) int64 {
	text := unoHandText(hand, top)
	kb := unoHandKeyboard(hand, top)
	msgID, err := h.childSendGetID(ctx, bot, userID, text, kb)
	if err != nil {
		slog.Error("child handler: failed to send hand DM", "bot_id", bot.ID, "user_id", userID, "err", err)
	}
	return msgID
}

func (h *ChildBotHandler) sendHandDMStickers(ctx context.Context, bot *botdomain.Bot, userID int64, hand []uno.Card, top uno.Card) int64 {
	rawToken, err := h.childToken(bot)
	if err != nil {
		return 0
	}

	// Send one sticker per playable card.
	for i, card := range hand {
		if !unoIsPlayable(card, top) {
			continue
		}
		fileID, ok := h.stickerMap[unoCardKey(card)]
		if !ok || fileID == "" {
			continue
		}
		var cb string
		if card.Color == uno.ColorWild {
			cb = fmt.Sprintf("uwild:%d", i)
		} else {
			cb = fmt.Sprintf("uplay:%d", i)
		}
		kb := &telegram.InlineKeyboardMarkup{
			InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{{Text: "▶ Play " + unoCardEmoji(card), CallbackData: cb}},
			},
		}
		if err := h.tgClient.SendSticker(ctx, rawToken, userID, fileID, kb); err != nil {
			slog.Error("child handler: sticker send failed", "bot_id", bot.ID, "err", err)
		}
	}

	// Summary + draw button as a keyboard message (also used for editing after play).
	text := fmt.Sprintf("🎴 Top card: %s — Tap a card above to play, or:", unoCardEmoji(top))
	kb := telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "🃏 Draw a card", CallbackData: "udraw"}},
		},
	}
	msgID, err := h.childSendGetID(ctx, bot, userID, text, kb)
	if err != nil {
		slog.Error("child handler: failed to send DM draw button", "bot_id", bot.ID, "err", err)
	}
	return msgID
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (h *ChildBotHandler) childToken(bot *botdomain.Bot) (string, error) {
	return h.tokenDecryptor(bot.Token.Ciphertext())
}

func (h *ChildBotHandler) childReply(ctx context.Context, bot *botdomain.Bot, chatID int64, text string) {
	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: decrypt failed", "bot_id", bot.ID, "err", err)
		return
	}
	slog.Info("child handler: sending reply", "bot_id", bot.ID, "chat_id", chatID)
	if err := h.tgClient.SendMessage(ctx, rawToken, chatID, text); err != nil {
		slog.Error("child handler: send failed", "bot_id", bot.ID, "chat_id", chatID, "err", err)
	}
}

func (h *ChildBotHandler) childSendWithKeyboard(ctx context.Context, bot *botdomain.Bot, chatID int64, text string, kb telegram.InlineKeyboardMarkup) {
	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: decrypt failed", "bot_id", bot.ID, "err", err)
		return
	}
	if err := h.tgClient.SendMessageWithKeyboard(ctx, rawToken, chatID, text, kb); err != nil {
		slog.Error("child handler: send keyboard failed", "bot_id", bot.ID, "chat_id", chatID, "err", err)
	}
}

func (h *ChildBotHandler) childSendGetID(ctx context.Context, bot *botdomain.Bot, chatID int64, text string, kb telegram.InlineKeyboardMarkup) (int64, error) {
	rawToken, err := h.childToken(bot)
	if err != nil {
		return 0, err
	}
	return h.tgClient.SendMessageGetID(ctx, rawToken, chatID, text, kb)
}

func (h *ChildBotHandler) childEditMessage(ctx context.Context, bot *botdomain.Bot, chatID, msgID int64, text string, kb *telegram.InlineKeyboardMarkup) {
	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: decrypt failed", "bot_id", bot.ID, "err", err)
		return
	}
	if err := h.tgClient.EditMessageText(ctx, rawToken, chatID, msgID, text, kb); err != nil {
		slog.Error("child handler: edit failed", "bot_id", bot.ID, "chat_id", chatID, "err", err)
	}
}

func (h *ChildBotHandler) childAnswerCallback(ctx context.Context, bot *botdomain.Bot, callbackQueryID string) error {
	rawToken, err := h.childToken(bot)
	if err != nil {
		return err
	}
	return h.tgClient.AnswerCallbackQuery(ctx, rawToken, callbackQueryID)
}

func (h *ChildBotHandler) childAnswerCallbackAlert(ctx context.Context, bot *botdomain.Bot, callbackQueryID, text string) error {
	rawToken, err := h.childToken(bot)
	if err != nil {
		return err
	}
	return h.tgClient.AnswerCallbackQueryAlert(ctx, rawToken, callbackQueryID, text)
}

// ── Text-move handler (sambung_kata, truth_or_date) ───────────────────────────

func (h *ChildBotHandler) handleTextMove(ctx context.Context, botID uuid.UUID, chatID, userID int64, text string, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, chatID)
	if err != nil {
		return
	}
	session, err := h.sessionSvc.GetSession(ctx, botID, sessionID)
	if err != nil || session.Status == sessiondomain.StatusFinished {
		return
	}
	game, err := h.gameSvc.GetGame(ctx, session.GameID)
	if err != nil {
		return
	}
	switch game.Slug {
	case gamedomain.SlugSambungKata:
		h.handleSKWord(ctx, botID, chatID, userID, sessionID, session, text, bot)
	case gamedomain.SlugTruthOrDate:
		h.handleTDAnswer(ctx, botID, chatID, userID, sessionID, session, text, bot)
	}
}

func (h *ChildBotHandler) handleSKWord(ctx context.Context, botID uuid.UUID, chatID, userID int64, sessionID uuid.UUID, session *sessiondomain.GameSession, word string, bot *botdomain.Bot) {
	state, err := parseSKState(session.State)
	if err != nil || state.Status == sambungkata.StatusFinished {
		return
	}
	if state.PlayerOrder[state.CurrentTurnIdx] != userID {
		return
	}
	result, err := h.sessionSvc.SubmitMove(ctx, botID, sessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  map[string]any{"word": word},
	})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	h.advanceTurnSK(ctx, botID, chatID, result, bot)
}

func (h *ChildBotHandler) handleTDAnswer(ctx context.Context, botID uuid.UUID, chatID, userID int64, sessionID uuid.UUID, session *sessiondomain.GameSession, answer string, bot *botdomain.Bot) {
	state, err := parseTDState(session.State)
	if err != nil || state.Status == truthordate.StatusFinished {
		return
	}
	if state.PlayerOrder[state.CurrentTurnIdx] != userID {
		return
	}
	if state.CurrentQuestion == "" {
		return
	}
	result, err := h.sessionSvc.SubmitMove(ctx, botID, sessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  map[string]any{"action": "answer", "answer": answer},
	})
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ "+extractMsg(err))
		return
	}
	h.advanceTurnTD(ctx, botID, chatID, result, bot)
}

// ── Truth or Date callbacks ───────────────────────────────────────────────────

func (h *ChildBotHandler) cbTDChoice(ctx context.Context, botID uuid.UUID, groupChatID, groupMsgID, userID int64, callbackID, choice string, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, groupChatID)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "No active game in this chat.")
		return
	}
	_ = h.childAnswerCallback(ctx, bot, callbackID)
	result, err := h.sessionSvc.SubmitMove(ctx, botID, sessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  map[string]any{"action": "choice", "choice": choice},
	})
	if err != nil {
		h.childEditMessage(ctx, bot, groupChatID, groupMsgID, "❌ "+extractMsg(err), nil)
		return
	}
	state, err := parseTDState(result.Session.State)
	if err != nil {
		return
	}
	playerName := playerDisplayName(result.Session, userID)
	text := tdQuestionText(playerName, choice, state.CurrentQuestion)
	kb := tdSkipKeyboard()
	h.childEditMessage(ctx, bot, groupChatID, groupMsgID, text, &kb)
}

func (h *ChildBotHandler) cbTDSkip(ctx context.Context, botID uuid.UUID, groupChatID, groupMsgID, userID int64, callbackID string, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, groupChatID)
	if err != nil {
		_ = h.childAnswerCallbackAlert(ctx, bot, callbackID, "No active game.")
		return
	}
	_ = h.childAnswerCallback(ctx, bot, callbackID)
	result, err := h.sessionSvc.SubmitMove(ctx, botID, sessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  map[string]any{"action": "skip"},
	})
	if err != nil {
		h.childEditMessage(ctx, bot, groupChatID, groupMsgID, "❌ "+extractMsg(err), nil)
		return
	}
	h.advanceTurnTD(ctx, botID, groupChatID, result, bot)
}

// ── Sambung Kata advance ──────────────────────────────────────────────────────

func (h *ChildBotHandler) advanceTurnSK(ctx context.Context, botID uuid.UUID, groupChatID int64, result *sessionapp.MoveResult, bot *botdomain.Bot) {
	session := result.Session

	if session.Status == sessiondomain.StatusFinished {
		var winnerName string
		for _, ev := range result.Events {
			if ev.Type == "GAME_OVER" {
				if id, ok := ev.Payload["winner_id"].(float64); ok {
					winnerName = playerDisplayName(session, int64(id))
				}
			}
		}
		if winnerName == "" {
			winnerName = "Someone"
		}
		text := skGameOverText(winnerName) + "\n\nFinal scores:\n" + finalScores(session)
		if msgID, err := h.gameMsgStore.Get(ctx, botID, groupChatID); err == nil {
			h.childEditMessage(ctx, bot, groupChatID, msgID, text, nil)
			_ = h.gameMsgStore.Delete(ctx, botID, groupChatID)
		}
		_ = h.chatIndex.Delete(ctx, botID, groupChatID)
		return
	}

	state, err := parseSKState(session.State)
	if err != nil {
		return
	}

	var prefix string
	for _, ev := range result.Events {
		switch ev.Type {
		case "WORD_REJECTED":
			if id, ok := ev.Payload["player_id"].(float64); ok {
				name := playerDisplayName(session, int64(id))
				reason, _ := ev.Payload["reason"].(string)
				prefix += fmt.Sprintf("❌ %s's word rejected: %s\n", name, reason)
			}
		case "PLAYER_ELIMINATED":
			if id, ok := ev.Payload["player_id"].(float64); ok {
				name := playerDisplayName(session, int64(id))
				prefix += fmt.Sprintf("🚫 %s eliminated!\n", name)
			}
		}
	}

	currentPlayerID := state.PlayerOrder[state.CurrentTurnIdx]
	playerName := playerDisplayName(session, currentPlayerID)
	text := prefix + skTurnText(playerName, state.LastWord)
	if msgID, err := h.gameMsgStore.Get(ctx, botID, groupChatID); err == nil {
		h.childEditMessage(ctx, bot, groupChatID, msgID, text, nil)
	}
}

// ── Truth or Date advance ─────────────────────────────────────────────────────

func (h *ChildBotHandler) advanceTurnTD(ctx context.Context, botID uuid.UUID, groupChatID int64, result *sessionapp.MoveResult, bot *botdomain.Bot) {
	session := result.Session
	state, err := parseTDState(session.State)
	if err != nil {
		return
	}
	currentPlayerID := state.PlayerOrder[state.CurrentTurnIdx]
	playerName := playerDisplayName(session, currentPlayerID)
	text := tdTurnText(playerName, state.Round)
	kb := tdChoiceKeyboard()
	if msgID, err := h.gameMsgStore.Get(ctx, botID, groupChatID); err == nil {
		h.childEditMessage(ctx, bot, groupChatID, msgID, text, &kb)
	}
}

// ── Sambung Kata / Truth or Date state parsers ────────────────────────────────

func parseSKState(raw json.RawMessage) (sambungkata.State, error) {
	var s sambungkata.State
	if err := json.Unmarshal(raw, &s); err != nil {
		return sambungkata.State{}, fmt.Errorf("parse sk state: %w", err)
	}
	return s, nil
}

func parseTDState(raw json.RawMessage) (truthordate.State, error) {
	var s truthordate.State
	if err := json.Unmarshal(raw, &s); err != nil {
		return truthordate.State{}, fmt.Errorf("parse td state: %w", err)
	}
	return s, nil
}

// ── Uno state helpers ─────────────────────────────────────────────────────────

func parseUnoState(raw json.RawMessage) (uno.State, error) {
	var s uno.State
	if err := json.Unmarshal(raw, &s); err != nil {
		return uno.State{}, fmt.Errorf("parse uno state: %w", err)
	}
	return s, nil
}

func playerDisplayName(session *sessiondomain.GameSession, telegramUserID int64) string {
	for _, p := range session.Players {
		if p.TelegramUserID == telegramUserID {
			return p.DisplayName
		}
	}
	return fmt.Sprintf("Player %d", telegramUserID)
}

func finalScores(session *sessiondomain.GameSession) string {
	var sb strings.Builder
	for _, p := range session.Players {
		fmt.Fprintf(&sb, "• %s: %d pts\n", p.DisplayName, p.Score)
	}
	return strings.TrimRight(sb.String(), "\n")
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
