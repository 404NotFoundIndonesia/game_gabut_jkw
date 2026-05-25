package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	gamehttp "github.com/404NFIDv2/bot-game-management/internal/game/interface/http"
	"github.com/404NFIDv2/bot-game-management/internal/middleware"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── stubs ──────────────────────────────────────────────────────────────────────

type stubGameSvc struct {
	games []*gamedomain.Game
	game  *gamedomain.Game
	err   error
}

func (s *stubGameSvc) ListGames(_ context.Context) ([]*gamedomain.Game, error) {
	return s.games, s.err
}
func (s *stubGameSvc) GetGame(_ context.Context, _ uuid.UUID) (*gamedomain.Game, error) {
	return s.game, s.err
}

type stubBotGameSvc struct {
	bg  *gamedomain.BotGame
	bgs []*gamedomain.BotGame
	err error
}

func (s *stubBotGameSvc) AssignGame(_ context.Context, _, _ uuid.UUID) (*gamedomain.BotGame, error) {
	return s.bg, s.err
}
func (s *stubBotGameSvc) RemoveGame(_ context.Context, _, _ uuid.UUID) error { return s.err }
func (s *stubBotGameSvc) ListBotGames(_ context.Context, _ uuid.UUID) ([]*gamedomain.BotGame, error) {
	return s.bgs, s.err
}

// ── helpers ───────────────────────────────────────────────────────────────────

const key = "admin-key"

func newApp(gameSvc gamehttp.GameService, bgSvc gamehttp.BotGameService) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          middleware.ErrorHandler,
	})
	api := app.Group("/api/v1", middleware.AdminAuth(key))
	gamehttp.NewGameHandler(gameSvc).RegisterRoutes(api)
	gamehttp.NewBotGameHandler(bgSvc).RegisterRoutes(api)
	return app
}

func req(t *testing.T, app *fiber.App, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	request := httptest.NewRequest(method, path, r)
	request.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	resp, _ := app.Test(request, -1)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

var threeGames = []*gamedomain.Game{
	{ID: uuid.New(), Slug: gamedomain.SlugUno, Name: "Uno", MinPlayers: 2, MaxPlayers: 10},
	{ID: uuid.New(), Slug: gamedomain.SlugSambungKata, Name: "Sambung Kata", MinPlayers: 2, MaxPlayers: 20},
	{ID: uuid.New(), Slug: gamedomain.SlugTruthOrDate, Name: "Truth or Date", MinPlayers: 2, MaxPlayers: 20},
}

// ── game tests ─────────────────────────────────────────────────────────────────

func TestListGames_ReturnsAll(t *testing.T) {
	app := newApp(&stubGameSvc{games: threeGames}, &stubBotGameSvc{})
	code, body := req(t, app, http.MethodGet, "/api/v1/games", nil)
	if code != http.StatusOK {
		t.Errorf("status: got %d", code)
	}
	data := body["data"].(map[string]any)
	games := data["games"].([]any)
	if len(games) != 3 {
		t.Errorf("expected 3 games, got %d", len(games))
	}
}

func TestGetGame_Success(t *testing.T) {
	g := threeGames[0]
	app := newApp(&stubGameSvc{game: g}, &stubBotGameSvc{})
	code, _ := req(t, app, http.MethodGet, "/api/v1/games/"+g.ID.String(), nil)
	if code != http.StatusOK {
		t.Errorf("status: got %d", code)
	}
}

func TestGetGame_NotFound(t *testing.T) {
	app := newApp(&stubGameSvc{err: apperrors.NotFound("game")}, &stubBotGameSvc{})
	code, _ := req(t, app, http.MethodGet, "/api/v1/games/"+uuid.New().String(), nil)
	if code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", code)
	}
}

func TestGetGame_InvalidUUID(t *testing.T) {
	app := newApp(&stubGameSvc{}, &stubBotGameSvc{})
	code, _ := req(t, app, http.MethodGet, "/api/v1/games/not-a-uuid", nil)
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

// ── bot-game tests ─────────────────────────────────────────────────────────────

func TestAssignGame_Success(t *testing.T) {
	g := threeGames[0]
	bg := &gamedomain.BotGame{BotID: uuid.New(), GameID: g.ID, Game: g}
	app := newApp(&stubGameSvc{}, &stubBotGameSvc{bg: bg})
	botID := uuid.New().String()
	code, _ := req(t, app, http.MethodPost, "/api/v1/bots/"+botID+"/games",
		map[string]string{"game_id": g.ID.String()})
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestAssignGame_InvalidGameIDFormat(t *testing.T) {
	app := newApp(&stubGameSvc{}, &stubBotGameSvc{})
	botID := uuid.New().String()
	code, _ := req(t, app, http.MethodPost, "/api/v1/bots/"+botID+"/games",
		map[string]string{"game_id": "bad"})
	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestRemoveGame_Success(t *testing.T) {
	app := newApp(&stubGameSvc{}, &stubBotGameSvc{})
	botID := uuid.New().String()
	gameID := uuid.New().String()
	code, _ := req(t, app, http.MethodDelete, "/api/v1/bots/"+botID+"/games/"+gameID, nil)
	if code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", code)
	}
}

func TestRemoveGame_NotFound(t *testing.T) {
	app := newApp(&stubGameSvc{}, &stubBotGameSvc{err: apperrors.NotFound("assignment")})
	botID := uuid.New().String()
	gameID := uuid.New().String()
	code, _ := req(t, app, http.MethodDelete, "/api/v1/bots/"+botID+"/games/"+gameID, nil)
	if code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", code)
	}
}

func TestListBotGames_ReturnsList(t *testing.T) {
	g := threeGames[0]
	bg := &gamedomain.BotGame{BotID: uuid.New(), GameID: g.ID, Game: g}
	app := newApp(&stubGameSvc{}, &stubBotGameSvc{bgs: []*gamedomain.BotGame{bg}})
	botID := uuid.New().String()
	code, body := req(t, app, http.MethodGet, "/api/v1/bots/"+botID+"/games", nil)
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
	data := body["data"].(map[string]any)
	games := data["games"].([]any)
	if len(games) != 1 {
		t.Errorf("expected 1 game, got %d", len(games))
	}
}
