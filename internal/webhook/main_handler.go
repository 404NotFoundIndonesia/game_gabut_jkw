package webhook

import (
	"context"
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
	if err := c.BodyParser(&update); err != nil || update.Message == nil || update.Message.From == nil {
		return c.SendStatus(fiber.StatusOK) // always ACK Telegram
	}

	ctx := c.Context()
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)

	if _, ok := h.adminIDs[userID]; !ok {
		h.reply(ctx, chatID, "⛔ Unauthorized.")
		return c.SendStatus(fiber.StatusOK)
	}

	// Non-command text advances the FSM.
	if !strings.HasPrefix(text, "/") {
		h.handleFSMText(ctx, userID, chatID, text)
		return c.SendStatus(fiber.StatusOK)
	}

	cmd, args := parseCommand(text)
	switch cmd {
	case "/addbot":
		h.cmdAddBot(ctx, userID, chatID)
	case "/removebot":
		h.cmdRemoveBot(ctx, chatID, args)
	case "/reactivatebot":
		h.cmdReactivateBot(ctx, chatID, args)
	case "/listbots":
		h.cmdListBots(ctx, chatID)
	case "/listgames":
		h.cmdListGames(ctx, chatID)
	case "/listbotgames":
		h.cmdListBotGames(ctx, chatID, args)
	case "/assigngame":
		h.cmdAssignGame(ctx, chatID, args)
	case "/removegame":
		h.cmdRemoveGame(ctx, chatID, args)
	case "/leaderboard":
		h.cmdLeaderboard(ctx, chatID, args)
	default:
		h.reply(ctx, chatID, mainHelpText())
	}
	return c.SendStatus(fiber.StatusOK)
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

// ── Commands ──────────────────────────────────────────────────────────────────

func (h *MainBotHandler) cmdAddBot(ctx context.Context, userID, chatID int64) {
	_ = h.convStore.Set(ctx, userID, ConversationData{State: ConvStateAwaitToken}, h.convTTL)
	h.reply(ctx, chatID, "Send me the BotFather token for the new child bot:")
}

func (h *MainBotHandler) cmdRemoveBot(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		h.reply(ctx, chatID, "Usage: /removebot <bot_id>")
		return
	}
	botID, err := uuid.Parse(args[0])
	if err != nil {
		h.reply(ctx, chatID, "❌ Invalid bot ID.")
		return
	}
	if err := h.botSvc.DeleteBotWithWebhook(ctx, botID); err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	h.reply(ctx, chatID, "✅ Bot removed and webhook deleted.")
}

func (h *MainBotHandler) cmdReactivateBot(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		h.reply(ctx, chatID, "Usage: /reactivatebot <bot_id>")
		return
	}
	botID, err := uuid.Parse(args[0])
	if err != nil {
		h.reply(ctx, chatID, "❌ Invalid bot ID.")
		return
	}
	bot, err := h.botSvc.ReactivateBotWithWebhook(ctx, botID)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	h.reply(ctx, chatID, fmt.Sprintf("✅ Bot reactivated.\nName: %s\nID: %s", bot.Name, bot.ID))
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

func (h *MainBotHandler) cmdListBotGames(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		h.reply(ctx, chatID, "Usage: /listbotgames <bot_id>")
		return
	}
	botID, err := uuid.Parse(args[0])
	if err != nil {
		h.reply(ctx, chatID, "❌ Invalid bot ID.")
		return
	}
	bgs, err := h.gameSvc.ListBotGames(ctx, botID)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	if len(bgs) == 0 {
		h.reply(ctx, chatID, "No games assigned. Use /assigngame <bot_id> <slug> to add one.")
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
	h.reply(ctx, chatID, sb.String())
}

func (h *MainBotHandler) cmdAssignGame(ctx context.Context, chatID int64, args []string) {
	if len(args) < 2 {
		h.reply(ctx, chatID, "Usage: /assigngame <bot_id> <game_slug>")
		return
	}
	botID, err := uuid.Parse(args[0])
	if err != nil {
		h.reply(ctx, chatID, "❌ Invalid bot ID.")
		return
	}
	game, err := h.gameSvc.GetGameBySlug(ctx, gamedomain.GameSlug(args[1]))
	if err != nil {
		h.reply(ctx, chatID, "❌ Unknown game slug. Use /listgames to see valid options.")
		return
	}
	if _, err := h.gameSvc.AssignGame(ctx, botID, game.ID); err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	h.reply(ctx, chatID, fmt.Sprintf("✅ Game %q assigned to bot %s.", game.Name, botID))
}

func (h *MainBotHandler) cmdRemoveGame(ctx context.Context, chatID int64, args []string) {
	if len(args) < 2 {
		h.reply(ctx, chatID, "Usage: /removegame <bot_id> <game_slug>")
		return
	}
	botID, err := uuid.Parse(args[0])
	if err != nil {
		h.reply(ctx, chatID, "❌ Invalid bot ID.")
		return
	}
	game, err := h.gameSvc.GetGameBySlug(ctx, gamedomain.GameSlug(args[1]))
	if err != nil {
		h.reply(ctx, chatID, "❌ Unknown game slug. Use /listgames to see valid options.")
		return
	}
	if err := h.gameSvc.RemoveGame(ctx, botID, game.ID); err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	h.reply(ctx, chatID, fmt.Sprintf("✅ Game %q removed from bot %s.", game.Name, botID))
}

func (h *MainBotHandler) cmdLeaderboard(ctx context.Context, chatID int64, args []string) {
	params := pagination.Params{Limit: 10, Offset: 0}

	if len(args) == 0 || args[0] == "global" {
		lb, err := h.lbSvc.GetGlobal(ctx, params)
		if err != nil {
			h.reply(ctx, chatID, "❌ "+extractMsg(err))
			return
		}
		h.reply(ctx, chatID, "🏆 Global Leaderboard:\n\n"+formatLeaderboard(lb))
		return
	}

	botID, err := uuid.Parse(args[0])
	if err != nil {
		h.reply(ctx, chatID, "❌ Invalid bot ID. Use /leaderboard global for the global leaderboard.")
		return
	}
	lb, err := h.lbSvc.GetByBot(ctx, botID, params)
	if err != nil {
		h.reply(ctx, chatID, "❌ "+extractMsg(err))
		return
	}
	h.reply(ctx, chatID, fmt.Sprintf("🏆 Leaderboard for bot %s:\n\n%s", botID, formatLeaderboard(lb)))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (h *MainBotHandler) reply(ctx context.Context, chatID int64, text string) {
	if err := h.tgClient.SendMessage(ctx, h.mainToken, chatID, text); err != nil {
		slog.Error("main bot: send message failed", "chat_id", chatID, "err", err)
	}
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
/removebot <bot_id> — remove a bot
/reactivatebot <bot_id> — reactivate an inactive bot
/listbots — list all registered bots
/listgames — list available games
/listbotgames <bot_id> — list games assigned to a bot
/assigngame <bot_id> <game_slug> — assign a game to a bot
/removegame <bot_id> <game_slug> — remove a game from a bot
/leaderboard <bot_id> — per-bot leaderboard
/leaderboard global — global leaderboard`
}
