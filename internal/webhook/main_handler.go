package webhook

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	lbdomain "github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// ── Service interfaces (defined here; satisfied by concrete application services) ──

// MainBotSvc is the BotService subset needed by the main handler.
type MainBotSvc interface {
	RegisterBotWithWebhook(ctx context.Context, name, rawToken string) (*botdomain.Bot, error)
	DeleteBotWithWebhook(ctx context.Context, id uuid.UUID) error
	ReactivateBotWithWebhook(ctx context.Context, id uuid.UUID) (*botdomain.Bot, error)
	ListBots(ctx context.Context, filter botdomain.BotFilter, limit, offset int) ([]*botdomain.Bot, int, error)
}

// MainGameSvc is the BotGameService subset needed by the main handler.
type MainGameSvc interface {
	ListGames(ctx context.Context) ([]*gamedomain.Game, error)
	ListBotGames(ctx context.Context, botID uuid.UUID) ([]*gamedomain.BotGame, error)
	AssignGame(ctx context.Context, botID, gameID uuid.UUID) (*gamedomain.BotGame, error)
	RemoveGame(ctx context.Context, botID, gameID uuid.UUID) error
	GetGameBySlug(ctx context.Context, slug gamedomain.GameSlug) (*gamedomain.Game, error)
}

// MainLeaderboardSvc is the LeaderboardService subset needed by the main handler.
type MainLeaderboardSvc interface {
	GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*lbdomain.Leaderboard, error)
	GetGlobal(ctx context.Context, params pagination.Params) (*lbdomain.Leaderboard, error)
}

// ── Handler ───────────────────────────────────────────────────────────────────

// MainBotHandler handles Telegram updates sent to the main admin bot webhook.
type MainBotHandler struct {
	botSvc    MainBotSvc
	gameSvc   MainGameSvc
	lbSvc     MainLeaderboardSvc
	convStore ConversationStore
	tgClient  telegram.Client
	mainToken string
	adminIDs  map[int64]struct{}
	convTTL   time.Duration
}

// NewMainBotHandler constructs the handler.
func NewMainBotHandler(
	botSvc MainBotSvc,
	gameSvc MainGameSvc,
	lbSvc MainLeaderboardSvc,
	convStore ConversationStore,
	tgClient telegram.Client,
	mainToken string,
	adminIDs []int64,
	convTTL time.Duration,
) *MainBotHandler {
	ids := make(map[int64]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		ids[id] = struct{}{}
	}
	return &MainBotHandler{
		botSvc:    botSvc,
		gameSvc:   gameSvc,
		lbSvc:     lbSvc,
		convStore: convStore,
		tgClient:  tgClient,
		mainToken: mainToken,
		adminIDs:  ids,
		convTTL:   convTTL,
	}
}

// RegisterRoutes mounts POST /telegram/main/webhook.
func (h *MainBotHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/telegram/main/webhook", h.handleUpdate)
}

func (h *MainBotHandler) handleUpdate(c *fiber.Ctx) error {
	var update telegram.Update
	if err := c.BodyParser(&update); err != nil {
		return c.SendStatus(fiber.StatusOK)
	}

	ctx := c.Context()

	if update.CallbackQuery != nil {
		h.handleCallbackQuery(ctx, update.CallbackQuery)
		return c.SendStatus(fiber.StatusOK)
	}

	if update.Message == nil || update.Message.From == nil {
		return c.SendStatus(fiber.StatusOK)
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)

	if _, ok := h.adminIDs[userID]; !ok {
		h.reply(ctx, chatID, "⛔ Unauthorized.")
		return c.SendStatus(fiber.StatusOK)
	}

	// Non-command text advances the /addbot FSM.
	if !strings.HasPrefix(text, "/") {
		h.handleFSMText(ctx, userID, chatID, text)
		return c.SendStatus(fiber.StatusOK)
	}

	cmd, args := parseCommand(text)
	switch cmd {
	case "/addbot":
		h.cmdAddBot(ctx, userID, chatID)
	case "/removebot":
		h.cmdRemoveBotMenu(ctx, chatID)
	case "/reactivatebot":
		h.cmdReactivateBotMenu(ctx, chatID)
	case "/listbots":
		h.cmdListBots(ctx, chatID)
	case "/listgames":
		h.cmdListGames(ctx, chatID)
	case "/listbotgames":
		h.cmdListBotGamesMenu(ctx, chatID)
	case "/assigngame":
		h.cmdAssignGameMenu(ctx, chatID)
	case "/removegame":
		h.cmdRemoveGameMenu(ctx, chatID)
	case "/leaderboard":
		h.cmdLeaderboardMenu(ctx, chatID, args)
	default:
		h.reply(ctx, chatID, mainHelpText())
	}
	return c.SendStatus(fiber.StatusOK)
}

// ── Callback query dispatcher ─────────────────────────────────────────────────

func (h *MainBotHandler) handleCallbackQuery(ctx context.Context, cq *telegram.CallbackQuery) {
	if cq.From == nil || cq.Message == nil {
		return
	}

	// Always ACK to clear the loading spinner, even for non-admins.
	_ = h.tgClient.AnswerCallbackQuery(ctx, h.mainToken, cq.ID)

	if _, ok := h.adminIDs[cq.From.ID]; !ok {
		return
	}

	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID

	// Callback data format: "<action>:<arg1>[:<arg2>]"
	// UUIDs are base64-encoded (22 chars) to stay under Telegram's 64-byte limit.
	parts := strings.SplitN(cq.Data, ":", 3)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "rb": // removebot
		if len(parts) < 2 {
			return
		}
		h.cbRemoveBot(ctx, chatID, msgID, parts[1])
	case "rab": // reactivatebot
		if len(parts) < 2 {
			return
		}
		h.cbReactivateBot(ctx, chatID, msgID, parts[1])
	case "lbg": // listbotgames
		if len(parts) < 2 {
			return
		}
		h.cbListBotGames(ctx, chatID, msgID, parts[1])
	case "ag1": // assigngame step 1: bot selected → show game list
		if len(parts) < 2 {
			return
		}
		h.cbAssignGameStep2(ctx, chatID, msgID, parts[1])
	case "ag2": // assigngame step 2: game selected → confirm
		if len(parts) < 3 {
			return
		}
		h.cbAssignGameConfirm(ctx, chatID, msgID, parts[1], parts[2])
	case "rg1": // removegame step 1: bot selected → show assigned games
		if len(parts) < 2 {
			return
		}
		h.cbRemoveGameStep2(ctx, chatID, msgID, parts[1])
	case "rg2": // removegame step 2: game selected → confirm
		if len(parts) < 3 {
			return
		}
		h.cbRemoveGameConfirm(ctx, chatID, msgID, parts[1], parts[2])
	case "lb": // leaderboard: "global" or <bot_id_b64>
		if len(parts) < 2 {
			return
		}
		h.cbLeaderboard(ctx, chatID, msgID, parts[1])
	}
}

// ── FSM ───────────────────────────────────────────────────────────────────────

func (h *MainBotHandler) handleFSMText(ctx context.Context, userID, chatID int64, text string) {
	data, err := h.convStore.Get(ctx, userID)
	if err != nil || data.State == ConvStateIdle {
		h.reply(ctx, chatID, mainHelpText())
		return
	}

	switch data.State {
	case ConvStateAwaitToken:
		_, err := h.tgClient.GetBotID(ctx, text)
		if err != nil {
			h.reply(ctx, chatID, "❌ Invalid bot token. Please try again or send /addbot to restart.")
			return
		}
		data.Token = text
		data.State = ConvStateAwaitName
		_ = h.convStore.Set(ctx, userID, data, h.convTTL)
		h.reply(ctx, chatID, "✅ Token valid. Now send me a name for this bot:")

	case ConvStateAwaitName:
		if text == "" {
			h.reply(ctx, chatID, "Name cannot be empty. Please send a name:")
			return
		}
		bot, err := h.botSvc.RegisterBotWithWebhook(ctx, text, data.Token)
		_ = h.convStore.Delete(ctx, userID)
		if err != nil {
			h.reply(ctx, chatID, "❌ Failed to register bot: "+extractMsg(err))
			return
		}
		h.reply(ctx, chatID, fmt.Sprintf("✅ Bot registered!\nName: %s\nID: %s", bot.Name, bot.ID))
	}
}

// ── Text commands ─────────────────────────────────────────────────────────────

func (h *MainBotHandler) cmdAddBot(ctx context.Context, userID, chatID int64) {
	_ = h.convStore.Set(ctx, userID, ConversationData{State: ConvStateAwaitToken}, h.convTTL)
	h.reply(ctx, chatID, "Send me the BotFather token for the new child bot:")
}

func (h *MainBotHandler) cmdListBots(ctx context.Context, chatID int64) {
	bots, _, err := h.botSvc.ListBots(ctx, botdomain.BotFilter{}, 100, 0)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bots) == 0 {
		h.reply(ctx, chatID, "No bots registered yet. Use /addbot to add one.")
		return
	}
	var sb strings.Builder
	sb.WriteString("📋 Registered bots:\n\n")
	for _, b := range bots {
		status := "✅ active"
		if !b.Active {
			status = "❌ inactive"
		}
		fmt.Fprintf(&sb, "• %s — %s\n  ID: %s\n\n", b.Name, status, b.ID)
	}
	h.reply(ctx, chatID, sb.String())
}

func (h *MainBotHandler) cmdListGames(ctx context.Context, chatID int64) {
	games, err := h.gameSvc.ListGames(ctx)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	var sb strings.Builder
	sb.WriteString("🎮 Available games:\n\n")
	for _, g := range games {
		fmt.Fprintf(&sb, "• %s (slug: %s)\n  Players: %d–%d\n  %s\n\n",
			g.Name, g.Slug, g.MinPlayers, g.MaxPlayers, g.Description)
	}
	h.reply(ctx, chatID, sb.String())
}

// ── Menu commands (show inline keyboard) ─────────────────────────────────────

func (h *MainBotHandler) cmdRemoveBotMenu(ctx context.Context, chatID int64) {
	active := true
	bots, _, err := h.botSvc.ListBots(ctx, botdomain.BotFilter{Active: &active}, 100, 0)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bots) == 0 {
		h.reply(ctx, chatID, "No active bots to remove.")
		return
	}
	kb := botsKeyboard(bots, "rb")
	_ = h.tgClient.SendMessageWithKeyboard(ctx, h.mainToken, chatID, "Select bot to remove:", kb)
}

func (h *MainBotHandler) cmdReactivateBotMenu(ctx context.Context, chatID int64) {
	inactive := false
	bots, _, err := h.botSvc.ListBots(ctx, botdomain.BotFilter{Active: &inactive}, 100, 0)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bots) == 0 {
		h.reply(ctx, chatID, "No inactive bots to reactivate.")
		return
	}
	kb := botsKeyboard(bots, "rab")
	_ = h.tgClient.SendMessageWithKeyboard(ctx, h.mainToken, chatID, "Select bot to reactivate:", kb)
}

func (h *MainBotHandler) cmdListBotGamesMenu(ctx context.Context, chatID int64) {
	bots, _, err := h.botSvc.ListBots(ctx, botdomain.BotFilter{}, 100, 0)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bots) == 0 {
		h.reply(ctx, chatID, "No bots registered yet. Use /addbot to add one.")
		return
	}
	kb := botsKeyboard(bots, "lbg")
	_ = h.tgClient.SendMessageWithKeyboard(ctx, h.mainToken, chatID, "Select bot to view games:", kb)
}

func (h *MainBotHandler) cmdAssignGameMenu(ctx context.Context, chatID int64) {
	bots, _, err := h.botSvc.ListBots(ctx, botdomain.BotFilter{}, 100, 0)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bots) == 0 {
		h.reply(ctx, chatID, "No bots registered yet. Use /addbot to add one.")
		return
	}
	kb := botsKeyboard(bots, "ag1")
	_ = h.tgClient.SendMessageWithKeyboard(ctx, h.mainToken, chatID, "Select bot to assign a game to:", kb)
}

func (h *MainBotHandler) cmdRemoveGameMenu(ctx context.Context, chatID int64) {
	bots, _, err := h.botSvc.ListBots(ctx, botdomain.BotFilter{}, 100, 0)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bots) == 0 {
		h.reply(ctx, chatID, "No bots registered yet. Use /addbot to add one.")
		return
	}
	kb := botsKeyboard(bots, "rg1")
	_ = h.tgClient.SendMessageWithKeyboard(ctx, h.mainToken, chatID, "Select bot to remove a game from:", kb)
}

func (h *MainBotHandler) cmdLeaderboardMenu(ctx context.Context, chatID int64, args []string) {
	// Keep "/leaderboard global" as a direct shortcut.
	if len(args) > 0 && args[0] == "global" {
		lb, err := h.lbSvc.GetGlobal(ctx, pagination.Params{Limit: 10})
		if err != nil {
			h.reply(ctx, chatID, "❌ "+extractMsg(err))
			return
		}
		h.reply(ctx, chatID, "🏆 Global Leaderboard:\n\n"+formatLeaderboard(lb))
		return
	}
	bots, _, err := h.botSvc.ListBots(ctx, botdomain.BotFilter{}, 100, 0)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	kb := leaderboardKeyboard(bots)
	_ = h.tgClient.SendMessageWithKeyboard(ctx, h.mainToken, chatID, "Select leaderboard:", kb)
}

// ── Callback handlers ─────────────────────────────────────────────────────────

func (h *MainBotHandler) cbRemoveBot(ctx context.Context, chatID, msgID int64, botIDEnc string) {
	botID, err := decodeID(botIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	if err := h.botSvc.DeleteBotWithWebhook(ctx, botID); err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "✅ Bot removed and webhook deleted.", nil)
}

func (h *MainBotHandler) cbReactivateBot(ctx context.Context, chatID, msgID int64, botIDEnc string) {
	botID, err := decodeID(botIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	bot, err := h.botSvc.ReactivateBotWithWebhook(ctx, botID)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID,
		fmt.Sprintf("✅ Bot reactivated.\nName: %s\nID: %s", bot.Name, bot.ID), nil)
}

func (h *MainBotHandler) cbListBotGames(ctx context.Context, chatID, msgID int64, botIDEnc string) {
	botID, err := decodeID(botIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	bgs, err := h.gameSvc.ListBotGames(ctx, botID)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	if len(bgs) == 0 {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID,
			"No games assigned to this bot. Use /assigngame to add one.", nil)
		return
	}
	var sb strings.Builder
	sb.WriteString("🎮 Assigned games:\n\n")
	for _, bg := range bgs {
		if bg.Game != nil {
			fmt.Fprintf(&sb, "• %s (slug: %s)\n", bg.Game.Name, bg.Game.Slug)
		} else {
			fmt.Fprintf(&sb, "• %s\n", bg.GameID)
		}
	}
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, sb.String(), nil)
}

func (h *MainBotHandler) cbAssignGameStep2(ctx context.Context, chatID, msgID int64, botIDEnc string) {
	botID, err := decodeID(botIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	games, err := h.gameSvc.ListGames(ctx)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	if len(games) == 0 {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "No games available.", nil)
		return
	}
	kb := assignGameKeyboard(games, botID)
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "Select game to assign:", &kb)
}

func (h *MainBotHandler) cbAssignGameConfirm(ctx context.Context, chatID, msgID int64, botIDEnc, gameIDEnc string) {
	botID, err := decodeID(botIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	gameID, err := decodeID(gameIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid game ID.", nil)
		return
	}
	if _, err := h.gameSvc.AssignGame(ctx, botID, gameID); err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "✅ Game assigned.", nil)
}

func (h *MainBotHandler) cbRemoveGameStep2(ctx context.Context, chatID, msgID int64, botIDEnc string) {
	botID, err := decodeID(botIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	bgs, err := h.gameSvc.ListBotGames(ctx, botID)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	if len(bgs) == 0 {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "No games assigned to this bot.", nil)
		return
	}
	kb := removeGameKeyboard(bgs, botID)
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "Select game to remove:", &kb)
}

func (h *MainBotHandler) cbRemoveGameConfirm(ctx context.Context, chatID, msgID int64, botIDEnc, gameIDEnc string) {
	botID, err := decodeID(botIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	gameID, err := decodeID(gameIDEnc)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid game ID.", nil)
		return
	}
	if err := h.gameSvc.RemoveGame(ctx, botID, gameID); err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "✅ Game removed.", nil)
}

func (h *MainBotHandler) cbLeaderboard(ctx context.Context, chatID, msgID int64, target string) {
	params := pagination.Params{Limit: 10}
	if target == "global" {
		lb, err := h.lbSvc.GetGlobal(ctx, params)
		if err != nil {
			_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
			return
		}
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID,
			"🏆 Global Leaderboard:\n\n"+formatLeaderboard(lb), nil)
		return
	}
	botID, err := decodeID(target)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ Invalid bot ID.", nil)
		return
	}
	lb, err := h.lbSvc.GetByBot(ctx, botID, params)
	if err != nil {
		_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID, "❌ "+extractMsg(err), nil)
		return
	}
	_ = h.tgClient.EditMessageText(ctx, h.mainToken, chatID, msgID,
		fmt.Sprintf("🏆 Bot Leaderboard:\n\n%s", formatLeaderboard(lb)), nil)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (h *MainBotHandler) reply(ctx context.Context, chatID int64, text string) {
	if err := h.tgClient.SendMessage(ctx, h.mainToken, chatID, text); err != nil {
		slog.Error("main bot: send message failed", "chat_id", chatID, "err", err)
	}
}

// encodeID base64-encodes a UUID into 22 chars (fits Telegram's 64-byte callback_data limit).
func encodeID(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(id[:])
}

func decodeID(s string) (uuid.UUID, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return uuid.UUID{}, err
	}
	return uuid.FromBytes(b)
}

// botsKeyboard builds a one-button-per-row keyboard from a bot list.
// prefix is the callback data action prefix (e.g. "rb").
func botsKeyboard(bots []*botdomain.Bot, prefix string) telegram.InlineKeyboardMarkup {
	rows := make([][]telegram.InlineKeyboardButton, len(bots))
	for i, b := range bots {
		rows[i] = []telegram.InlineKeyboardButton{
			{Text: b.Name, CallbackData: prefix + ":" + encodeID(b.ID)},
		}
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// leaderboardKeyboard adds a "Global" button at the top plus one button per bot.
func leaderboardKeyboard(bots []*botdomain.Bot) telegram.InlineKeyboardMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, len(bots)+1)
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: "🌍 Global", CallbackData: "lb:global"},
	})
	for _, b := range bots {
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: b.Name, CallbackData: "lb:" + encodeID(b.ID)},
		})
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// assignGameKeyboard builds the game selection keyboard for /assigngame step 2.
// The bot ID is embedded in each button's callback data.
func assignGameKeyboard(games []*gamedomain.Game, botID uuid.UUID) telegram.InlineKeyboardMarkup {
	encodedBot := encodeID(botID)
	rows := make([][]telegram.InlineKeyboardButton, len(games))
	for i, g := range games {
		rows[i] = []telegram.InlineKeyboardButton{
			{
				Text:         fmt.Sprintf("%s (%s)", g.Name, g.Slug),
				CallbackData: "ag2:" + encodedBot + ":" + encodeID(g.ID),
			},
		}
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// removeGameKeyboard builds the game selection keyboard for /removegame step 2.
func removeGameKeyboard(botGames []*gamedomain.BotGame, botID uuid.UUID) telegram.InlineKeyboardMarkup {
	encodedBot := encodeID(botID)
	rows := make([][]telegram.InlineKeyboardButton, 0, len(botGames))
	for _, bg := range botGames {
		if bg.Game == nil {
			continue
		}
		rows = append(rows, []telegram.InlineKeyboardButton{
			{
				Text:         fmt.Sprintf("%s (%s)", bg.Game.Name, bg.Game.Slug),
				CallbackData: "rg2:" + encodedBot + ":" + encodeID(bg.GameID),
			},
		})
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// parseCommand splits "/command@botname arg1 arg2" into ("/command", ["arg1","arg2"]).
func parseCommand(text string) (string, []string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", nil
	}
	cmd := parts[0]
	if at := strings.IndexByte(cmd, '@'); at != -1 {
		cmd = cmd[:at]
	}
	return cmd, parts[1:]
}

func extractMsg(err error) string {
	if ae, ok := err.(*apperrors.AppError); ok {
		return ae.Message
	}
	return err.Error()
}

func formatLeaderboard(lb *lbdomain.Leaderboard) string {
	if lb == nil || len(lb.Entries) == 0 {
		return "No entries yet."
	}
	var sb strings.Builder
	for _, e := range lb.Entries {
		fmt.Fprintf(&sb, "%d. %s — score: %d, wins: %d\n", e.Rank, e.DisplayName, e.TotalScore, e.Wins)
	}
	return sb.String()
}

func mainHelpText() string {
	return `Available commands:

/addbot — register a new child bot
/removebot — remove a bot (pick from list)
/reactivatebot — reactivate an inactive bot (pick from list)
/listbots — list all registered bots
/listgames — list available games
/listbotgames — list games assigned to a bot (pick from list)
/assigngame — assign a game to a bot (pick from list)
/removegame — remove a game from a bot (pick from list)
/leaderboard — show leaderboard (pick global or per-bot)
/leaderboard global — global leaderboard directly`
}
