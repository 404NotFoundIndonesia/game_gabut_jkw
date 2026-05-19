package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	lbhttp "github.com/404NFIDv2/bot-game-management/internal/leaderboard/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// ── stub service ──────────────────────────────────────────────────────────────

type stubLbSvc struct {
	lb  *domain.Leaderboard
	err error
}

func (s *stubLbSvc) GetByBot(_ context.Context, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return s.lb, s.err
}
func (s *stubLbSvc) GetByBotAndGame(_ context.Context, _, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return s.lb, s.err
}
func (s *stubLbSvc) GetGlobal(_ context.Context, _ pagination.Params) (*domain.Leaderboard, error) {
	return s.lb, s.err
}
func (s *stubLbSvc) GetGlobalByGame(_ context.Context, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return s.lb, s.err
}

// ── helpers ───────────────────────────────────────────────────────────────────

const testAdminKey = "test-admin-key"

func newLbApp(svc lbhttp.LeaderboardService) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler,
	})
	api := app.Group("/api/v1", middleware.AdminAuth(testAdminKey))
	lbhttp.NewLeaderboardHandler(svc).RegisterRoutes(api)
	return app
}

func doGet(t *testing.T, app *fiber.App, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+testAdminKey)
	resp, _ := app.Test(req, -1)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func seedLeaderboard(n int) *domain.Leaderboard {
	entries := make([]domain.LeaderboardEntry, n)
	for i := range entries {
		entries[i] = domain.LeaderboardEntry{
			Rank:           i + 1,
			TelegramUserID: int64(i + 1),
			DisplayName:    "player",
			TotalScore:     100 - i*10,
		}
	}
	return &domain.Leaderboard{Entries: entries, Total: n}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestGetGlobal_Success(t *testing.T) {
	lb := seedLeaderboard(3)
	app := newLbApp(&stubLbSvc{lb: lb})
	code, body := doGet(t, app, "/api/v1/leaderboard")
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
	data := body["data"].(map[string]any)
	entries := data["leaderboard"].([]any)
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestGetGlobal_RespectsPagination(t *testing.T) {
	lb := seedLeaderboard(3)
	app := newLbApp(&stubLbSvc{lb: lb})
	code, body := doGet(t, app, "/api/v1/leaderboard?limit=3&offset=0")
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
	meta := body["meta"].(map[string]any)
	if int(meta["limit"].(float64)) != 3 {
		t.Errorf("expected limit=3 in meta, got %v", meta["limit"])
	}
}

func TestGetGlobalByGame_Success(t *testing.T) {
	lb := seedLeaderboard(2)
	app := newLbApp(&stubLbSvc{lb: lb})
	code, _ := doGet(t, app, "/api/v1/leaderboard/"+uuid.New().String())
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestGetGlobalByGame_InvalidUUID(t *testing.T) {
	app := newLbApp(&stubLbSvc{lb: seedLeaderboard(0)})
	code, _ := doGet(t, app, "/api/v1/leaderboard/not-a-uuid")
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestGetByBot_Success(t *testing.T) {
	lb := seedLeaderboard(5)
	app := newLbApp(&stubLbSvc{lb: lb})
	code, body := doGet(t, app, "/api/v1/bots/"+uuid.New().String()+"/leaderboard")
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
	data := body["data"].(map[string]any)
	entries := data["leaderboard"].([]any)
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestGetByBot_InvalidUUID(t *testing.T) {
	app := newLbApp(&stubLbSvc{lb: seedLeaderboard(0)})
	code, _ := doGet(t, app, "/api/v1/bots/bad-uuid/leaderboard")
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestGetByBotAndGame_Success(t *testing.T) {
	lb := seedLeaderboard(2)
	app := newLbApp(&stubLbSvc{lb: lb})
	path := "/api/v1/bots/" + uuid.New().String() + "/leaderboard/" + uuid.New().String()
	code, _ := doGet(t, app, path)
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestGetByBotAndGame_InvalidGameUUID(t *testing.T) {
	app := newLbApp(&stubLbSvc{lb: seedLeaderboard(0)})
	path := "/api/v1/bots/" + uuid.New().String() + "/leaderboard/not-a-uuid"
	code, _ := doGet(t, app, path)
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestLeaderboard_NoAuth_Returns401(t *testing.T) {
	app := newLbApp(&stubLbSvc{lb: seedLeaderboard(0)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/leaderboard", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLeaderboard_RankFieldPresent(t *testing.T) {
	lb := seedLeaderboard(1)
	app := newLbApp(&stubLbSvc{lb: lb})
	code, body := doGet(t, app, "/api/v1/leaderboard")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	data := body["data"].(map[string]any)
	entries := data["leaderboard"].([]any)
	entry := entries[0].(map[string]any)
	if _, ok := entry["rank"]; !ok {
		t.Error("rank field missing from leaderboard entry")
	}
	if int(entry["rank"].(float64)) != 1 {
		t.Errorf("expected rank=1, got %v", entry["rank"])
	}
}
