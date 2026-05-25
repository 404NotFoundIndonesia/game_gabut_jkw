//go:build e2e

// Package e2e contains end-to-end tests that run against a live stack.
// Launch the stack before running:
//
//	docker compose up -d
//	go test ./test/e2e/ -v -tags e2e
//
// The BASE_URL env var selects the server (default: http://localhost:8080).
// ADMIN_API_KEY must match what the server was started with.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func baseURL() string {
	if u := os.Getenv("BASE_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

func adminKey() string {
	return os.Getenv("ADMIN_API_KEY")
}

func do(t *testing.T, method, path string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL()+path, r)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func adminHdr() map[string]string {
	return map[string]string{"Authorization": "Bearer " + adminKey()}
}

func botHdr(token string) map[string]string {
	return map[string]string{"X-Bot-Token": token}
}

// mustStatus fails the test if code != want.
func mustStatus(t *testing.T, want, got int, body map[string]any) {
	t.Helper()
	if got != want {
		t.Fatalf("expected HTTP %d, got %d — body: %v", want, got, body)
	}
}

// registerBot creates a bot via admin API and returns its ID and plain token.
func registerBot(t *testing.T, name string) (botID, token string) {
	t.Helper()
	code, body := do(t, http.MethodPost, "/api/v1/bots",
		map[string]any{"name": name, "description": "e2e bot"},
		adminHdr())
	mustStatus(t, http.StatusCreated, code, body)
	data := body["data"].(map[string]any)
	return data["id"].(string), data["api_token"].(string)
}

// assignGame assigns a game slug to a bot. Returns the game UUID.
func assignGame(t *testing.T, botID, gameSlug string) string {
	t.Helper()
	// Look up game ID by slug.
	code, body := do(t, http.MethodGet, "/api/v1/games", nil, adminHdr())
	mustStatus(t, http.StatusOK, code, body)
	games := body["data"].(map[string]any)["games"].([]any)
	var gameID string
	for _, g := range games {
		gm := g.(map[string]any)
		if gm["slug"].(string) == gameSlug {
			gameID = gm["id"].(string)
			break
		}
	}
	if gameID == "" {
		t.Fatalf("game slug %q not found", gameSlug)
	}
	code, body = do(t, http.MethodPost,
		fmt.Sprintf("/api/v1/bots/%s/games", botID),
		map[string]any{"game_id": gameID},
		adminHdr())
	mustStatus(t, http.StatusCreated, code, body)
	return gameID
}

// ── Scenario 4 — Auth ─────────────────────────────────────────────────────────

func TestE2E_Auth_NoKey_Returns401(t *testing.T) {
	code, body := do(t, http.MethodGet, "/api/v1/bots/"+uuid.New().String()+"/sessions", nil, nil)
	mustStatus(t, http.StatusUnauthorized, code, body)
}

func TestE2E_Auth_WrongKey_Returns401(t *testing.T) {
	code, body := do(t, http.MethodGet, "/api/v1/bots/"+uuid.New().String()+"/sessions",
		nil, map[string]string{"Authorization": "Bearer wrong-key"})
	mustStatus(t, http.StatusUnauthorized, code, body)
}

func TestE2E_Auth_ValidAdminKey_Returns200(t *testing.T) {
	if adminKey() == "" {
		t.Skip("ADMIN_API_KEY not set")
	}
	code, body := do(t, http.MethodGet, "/api/v1/bots/"+uuid.New().String()+"/sessions", nil, adminHdr())
	// Either 200 (empty list) or 404 is acceptable — both mean auth passed.
	if code == http.StatusUnauthorized {
		t.Errorf("expected non-401, got %d — body: %v", code, body)
	}
}

func TestE2E_Auth_BotKeyOnAdminEndpoint_Returns401(t *testing.T) {
	if adminKey() == "" {
		t.Skip("ADMIN_API_KEY not set")
	}
	_, token := registerBot(t, "auth-test-bot-"+uuid.New().String()[:8])
	code, body := do(t, http.MethodGet, "/api/v1/bots", nil, botHdr(token))
	mustStatus(t, http.StatusUnauthorized, code, body)
}

// ── Scenario 1 — Uno full game ────────────────────────────────────────────────

func TestE2E_Uno_FullSession(t *testing.T) {
	if adminKey() == "" {
		t.Skip("ADMIN_API_KEY not set")
	}

	botID, token := registerBot(t, "uno-bot-"+uuid.New().String()[:8])
	gameID := assignGame(t, botID, "uno")
	_ = gameID
	chatID := int64(time.Now().UnixMilli())

	// Create session (player 1 — host).
	code, body := do(t, http.MethodPost,
		fmt.Sprintf("/api/v1/bots/%s/sessions", botID),
		map[string]any{
			"game_id": gameID,
			"chat_id": chatID,
			"player":  map[string]any{"telegram_user_id": 1001, "display_name": "Alice"},
		}, botHdr(token))
	mustStatus(t, http.StatusCreated, code, body)
	sessionID := body["data"].(map[string]any)["id"].(string)

	// Join (player 2).
	code, body = do(t, http.MethodPost,
		fmt.Sprintf("/api/v1/bots/%s/sessions/%s/join", botID, sessionID),
		map[string]any{"telegram_user_id": 1002, "display_name": "Bob"},
		botHdr(token))
	mustStatus(t, http.StatusOK, code, body)

	// Start.
	code, body = do(t, http.MethodPost,
		fmt.Sprintf("/api/v1/bots/%s/sessions/%s/start", botID, sessionID),
		map[string]any{"telegram_user_id": 1001},
		botHdr(token))
	mustStatus(t, http.StatusOK, code, body)

	// Verify session is IN_PROGRESS.
	code, body = do(t, http.MethodGet,
		fmt.Sprintf("/api/v1/bots/%s/sessions/%s", botID, sessionID),
		nil, adminHdr())
	mustStatus(t, http.StatusOK, code, body)
	status := body["data"].(map[string]any)["status"].(string)
	if status != "IN_PROGRESS" {
		t.Errorf("expected IN_PROGRESS, got %s", status)
	}
}

// ── Scenario 5 — Leaderboard aggregation ──────────────────────────────────────

func TestE2E_Leaderboard_GlobalEndpointReachable(t *testing.T) {
	if adminKey() == "" {
		t.Skip("ADMIN_API_KEY not set")
	}
	code, body := do(t, http.MethodGet, "/api/v1/leaderboard", nil, adminHdr())
	mustStatus(t, http.StatusOK, code, body)
	if _, ok := body["data"]; !ok {
		t.Error("response missing data field")
	}
}

func TestE2E_Health_ReturnsOK(t *testing.T) {
	code, _ := do(t, http.MethodGet, "/health", nil, nil)
	if code != http.StatusOK {
		t.Errorf("expected 200 from /health, got %d", code)
	}
}
