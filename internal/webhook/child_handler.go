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

// ChildBotHandler handles Telegram updates sent to child game bot webhooks.
type ChildBotHandler struct {
	botRepo    ChildBotLookup
	sessionSvc ChildSessionSvc
	gameSvc    MainGameSvc // reuses the same interface defined in main_handler.go
	lbSvc      ChildLeaderboardSvc
	chatIndex  ChatSessionIndex
	tgClient   telegram.Client
	sessionTTL time.Duration
}

// NewChildBotHandler constructs the ChildBotHandler.
func NewChildBotHandler(
	botRepo ChildBotLookup,
	sessionSvc ChildSessionSvc,
	gameSvc MainGameSvc,
	lbSvc ChildLeaderboardSvc,
	chatIndex ChatSessionIndex,
	tgClient telegram.Client,
	sessionTTL time.Duration,
) *ChildBotHandler {
	return &ChildBotHandler{
		botRepo:    botRepo,
		sessionSvc: sessionSvc,
		gameSvc:    gameSvc,
		lbSvc:      lbSvc,
		chatIndex:  chatIndex,
		tgClient:   tgClient,
		sessionTTL: sessionTTL,
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

	bot, err := h.botRepo.FindByID(c.Context(), botID)
	if err != nil || !bot.Active {
		slog.Error("child handler: bot not found or inactive", "bot_id", botID)
		return c.SendStatus(fiber.StatusOK)
	}

	var update telegram.Update
	if err := c.BodyParser(&update); err != nil || update.Message == nil || update.Message.From == nil {
		return c.SendStatus(fiber.StatusOK)
	}

	ctx := c.Context()
	userID := update.Message.From.ID
	displayName := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		displayName += " " + update.Message.From.LastName
	}
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)

	if !strings.HasPrefix(text, "/") {
		return c.SendStatus(fiber.StatusOK)
	}

	cmd, args := parseCommand(text)
	switch cmd {
	case "/newgame":
		h.cmdNewGame(ctx, botID, chatID, userID, displayName, args, bot)
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
		h.childReply(ctx, bot, chatID, childHelpText())
	}
	return c.SendStatus(fiber.StatusOK)
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (h *ChildBotHandler) cmdNewGame(ctx context.Context, botID uuid.UUID, chatID, userID int64, displayName string, args []string, bot *botdomain.Bot) {
	if len(args) == 0 {
		h.childReply(ctx, bot, chatID, "Usage: /newgame <game_slug>\nSlugs: uno, sambung_kata, truth_or_date")
		return
	}
	game, err := h.gameSvc.GetGameBySlug(ctx, gamedomain.GameSlug(args[0]))
	if err != nil {
		h.childReply(ctx, bot, chatID, "❌ Unknown game slug. Try: uno, sambung_kata, truth_or_date")
		return
	}

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
		"🎮 Game %q created!\nSession ID: %s\nHost: %s\nOthers: send /join to play.\nHost: send /start when ready.",
		game.Slug, session.ID, displayName,
	))
}

func (h *ChildBotHandler) cmdJoin(ctx context.Context, botID uuid.UUID, chatID, userID int64, displayName string, bot *botdomain.Bot) {
	sessionID, err := h.chatIndex.Get(ctx, botID, chatID)
	if err != nil {
		h.childReply(ctx, bot, chatID, "No active game in this chat. Use /newgame <slug> to start one.")
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

// childReply sends a text message via the child bot's Telegram token.
// The token ciphertext is stored on the bot; in production the infrastructure
// layer (TokenDecryptingClient) decrypts it before the actual API call.
func (h *ChildBotHandler) childReply(ctx context.Context, bot *botdomain.Bot, chatID int64, text string) {
	if err := h.tgClient.SendMessage(ctx, bot.Token.Ciphertext(), chatID, text); err != nil {
		slog.Error("child handler: send message failed", "bot_id", bot.ID, "chat_id", chatID, "err", err)
	}
}

func childHelpText() string {
	return `Available commands:

/newgame <slug> — start a new game (slug: uno, sambung_kata, truth_or_date)
/join — join the active game in this chat
/start — start the game (host only)
/move <payload> — submit a move (JSON or "action value")
/end — end the current game
/leaderboard — show bot leaderboard`
}
