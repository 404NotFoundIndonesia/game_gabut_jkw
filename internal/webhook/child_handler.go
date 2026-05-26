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
	case update.InlineQuery != nil:
		h.handleInlineQuery(ctx, botID, bot, update.InlineQuery)
	case update.ChosenInlineResult != nil:
		h.handleChosenInlineResult(ctx, botID, bot, update.ChosenInlineResult)
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
	currentPlayerID := state.PlayerOrder[state.CurrentTurnIdx]
	_ = h.turnStore.Set(ctx, botID, currentPlayerID, TurnContext{
		GroupChatID: chatID,
		SessionID:   session.ID,
	}, h.sessionTTL)
	playerName := playerDisplayName(session, currentPlayerID)
	text := unoTurnText(state.DiscardPile[len(state.DiscardPile)-1], playerName, len(state.Hands[currentPlayerID]))
	h.childSendWithKeyboard(ctx, bot, chatID, text, unoPlayButton())
}

func (h *ChildBotHandler) startSambungKata(ctx context.Context, _ uuid.UUID, chatID int64, session *sessiondomain.GameSession, bot *botdomain.Bot) {
	state, err := parseSKState(session.State)
	if err != nil {
		h.childReply(ctx, bot, chatID, "🎮 Sambung Kata started!")
		return
	}
	playerName := playerDisplayName(session, state.PlayerOrder[state.CurrentTurnIdx])
	h.childReply(ctx, bot, chatID, skTurnText(playerName, state.LastWord))
}

func (h *ChildBotHandler) startTruthOrDate(ctx context.Context, _ uuid.UUID, chatID int64, session *sessiondomain.GameSession, bot *botdomain.Bot) {
	state, err := parseTDState(session.State)
	if err != nil {
		h.childReply(ctx, bot, chatID, "🎮 Truth or Date started!")
		return
	}
	playerName := playerDisplayName(session, state.PlayerOrder[state.CurrentTurnIdx])
	h.childSendWithKeyboard(ctx, bot, chatID, tdTurnText(playerName, state.Round), tdChoiceKeyboard())
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

// ── Uno inline query handlers ─────────────────────────────────────────────────

// handleInlineQuery returns the current player's playable cards as inline results.
// Only the user whose turn it is will see valid results; others get an empty list.
func (h *ChildBotHandler) handleInlineQuery(ctx context.Context, botID uuid.UUID, bot *botdomain.Bot, iq *telegram.InlineQuery) {
	slog.Info("child handler: inline query received", "bot_id", botID, "user_id", iq.From.ID, "query", iq.Query)

	rawToken, err := h.childToken(bot)
	if err != nil {
		slog.Error("child handler: inline query token decrypt failed", "bot_id", botID, "err", err)
		return
	}

	tc, err := h.turnStore.Get(ctx, botID, iq.From.ID)
	if err != nil {
		slog.Info("child handler: inline query no turn context", "bot_id", botID, "user_id", iq.From.ID)
		_ = h.tgClient.AnswerInlineQuery(ctx, rawToken, iq.ID, nil)
		return
	}

	session, err := h.sessionSvc.GetSession(ctx, botID, tc.SessionID)
	if err != nil {
		slog.Error("child handler: inline query get session failed", "bot_id", botID, "session_id", tc.SessionID, "err", err)
		_ = h.tgClient.AnswerInlineQuery(ctx, rawToken, iq.ID, nil)
		return
	}

	state, err := parseUnoState(session.State)
	if err != nil {
		slog.Error("child handler: inline query parse state failed", "bot_id", botID, "err", err)
		_ = h.tgClient.AnswerInlineQuery(ctx, rawToken, iq.ID, nil)
		return
	}
	if state.PlayerOrder[state.CurrentTurnIdx] != iq.From.ID {
		slog.Info("child handler: inline query not player's turn", "bot_id", botID, "user_id", iq.From.ID)
		_ = h.tgClient.AnswerInlineQuery(ctx, rawToken, iq.ID, nil)
		return
	}

	hand := state.Hands[iq.From.ID]
	top := state.DiscardPile[len(state.DiscardPile)-1]
	results := unoInlineResults(hand, top, h.stickerMap)
	slog.Info("child handler: inline query answering", "bot_id", botID, "user_id", iq.From.ID, "results", len(results))
	_ = h.tgClient.AnswerInlineQuery(ctx, rawToken, iq.ID, results)
}

// handleChosenInlineResult applies the move when a player selects a card.
// result_id format: "play:<idx>" | "wild:<idx>:<color>" | "draw"
func (h *ChildBotHandler) handleChosenInlineResult(ctx context.Context, botID uuid.UUID, bot *botdomain.Bot, cir *telegram.ChosenInlineResult) {
	userID := cir.From.ID
	tc, err := h.turnStore.Get(ctx, botID, userID)
	if err != nil {
		return
	}

	var payload map[string]any
	rid := cir.ResultID
	switch {
	case rid == "draw":
		payload = map[string]any{"action": "draw"}
	case strings.HasPrefix(rid, "play:"):
		idx, err := strconv.Atoi(rid[5:])
		if err != nil {
			return
		}
		payload = map[string]any{"action": "play_card", "card_index": idx}
	case strings.HasPrefix(rid, "wild:"):
		parts := strings.SplitN(rid[5:], ":", 2)
		if len(parts) != 2 {
			return
		}
		idx, err := strconv.Atoi(parts[0])
		if err != nil {
			return
		}
		payload = map[string]any{"action": "play_card", "card_index": idx, "chosen_color": parts[1]}
	default:
		return
	}

	result, err := h.sessionSvc.SubmitMove(ctx, botID, tc.SessionID, sessionapp.MoveRequest{
		PlayerID: userID,
		Payload:  payload,
	})
	if err != nil {
		h.childReply(ctx, bot, tc.GroupChatID, "❌ "+extractMsg(err))
		return
	}
	_ = h.turnStore.Delete(ctx, botID, userID)
	h.advanceTurn(ctx, botID, tc, result, bot)
}

// advanceTurn sends the next turn announcement and registers the new player's context.
func (h *ChildBotHandler) advanceTurn(ctx context.Context, botID uuid.UUID, tc TurnContext, result *sessionapp.MoveResult, bot *botdomain.Bot) {
	session := result.Session
	if session.Status == sessiondomain.StatusFinished {
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
		h.childReply(ctx, bot, tc.GroupChatID, unoGameOverText(winnerName)+"\n\nFinal scores:\n"+finalScores(session))
		_ = h.chatIndex.Delete(ctx, botID, tc.GroupChatID)
		return
	}

	state, err := parseUnoState(session.State)
	if err != nil {
		return
	}
	nextPlayerID := state.PlayerOrder[state.CurrentTurnIdx]
	_ = h.turnStore.Set(ctx, botID, nextPlayerID, TurnContext{
		GroupChatID: tc.GroupChatID,
		SessionID:   tc.SessionID,
	}, h.sessionTTL)
	playerName := playerDisplayName(session, nextPlayerID)
	handSize := len(state.Hands[nextPlayerID])
	text := unoTurnText(state.DiscardPile[len(state.DiscardPile)-1], playerName, handSize)
	h.childSendWithKeyboard(ctx, bot, tc.GroupChatID, text, unoPlayButton())
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
	h.childSendWithKeyboard(ctx, bot, groupChatID, tdQuestionText(playerName, choice, state.CurrentQuestion), tdSkipKeyboard())
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
		h.childReply(ctx, bot, groupChatID, skGameOverText(winnerName)+"\n\nFinal scores:\n"+finalScores(session))
		_ = h.chatIndex.Delete(ctx, botID, groupChatID)
		return
	}

	state, err := parseSKState(session.State)
	if err != nil {
		return
	}

	var sb strings.Builder
	for _, ev := range result.Events {
		switch ev.Type {
		case "WORD_REJECTED":
			if id, ok := ev.Payload["player_id"].(float64); ok {
				name := playerDisplayName(session, int64(id))
				reason, _ := ev.Payload["reason"].(string)
				fmt.Fprintf(&sb, "❌ %s's word rejected: %s\n", name, reason)
			}
		case "PLAYER_ELIMINATED":
			if id, ok := ev.Payload["player_id"].(float64); ok {
				name := playerDisplayName(session, int64(id))
				fmt.Fprintf(&sb, "🚫 %s eliminated!\n", name)
			}
		}
	}

	currentPlayerID := state.PlayerOrder[state.CurrentTurnIdx]
	playerName := playerDisplayName(session, currentPlayerID)
	h.childReply(ctx, bot, groupChatID, sb.String()+skTurnText(playerName, state.LastWord))
}

// ── Truth or Date advance ─────────────────────────────────────────────────────

func (h *ChildBotHandler) advanceTurnTD(ctx context.Context, _ uuid.UUID, groupChatID int64, result *sessionapp.MoveResult, bot *botdomain.Bot) {
	session := result.Session
	state, err := parseTDState(session.State)
	if err != nil {
		return
	}
	currentPlayerID := state.PlayerOrder[state.CurrentTurnIdx]
	playerName := playerDisplayName(session, currentPlayerID)
	h.childSendWithKeyboard(ctx, bot, groupChatID, tdTurnText(playerName, state.Round), tdChoiceKeyboard())
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
