package telegram_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/telegram"
)

// newMockServer returns a test server that responds with the given JSON body.
func newMockServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, telegram.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// We can't easily inject the base URL into httpClient without modifying production code.
	// We test the real client methods against a mock Telegram endpoint by patching
	// at the transport level using a custom RoundTripper.
	transport := &redirectTransport{base: srv.URL}
	client := telegram.NewHTTPClientWithTransport(&http.Client{Transport: transport})
	return srv, client
}

// redirectTransport rewrites all requests to the given base URL.
type redirectTransport struct {
	base string
}

func (r *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite URL: replace "https://api.telegram.org" with mock server base.
	newURL := strings.Replace(req.URL.String(), "https://api.telegram.org", r.base, 1)
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

func okJSON(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	env := map[string]any{"ok": true, "result": result}
	_ = json.NewEncoder(w).Encode(env)
}

func TestGetBotID(t *testing.T) {
	_, client := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		okJSON(t, w, map[string]any{"id": int64(12345), "is_bot": true, "first_name": "TestBot"})
	})

	id, err := client.GetBotID(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 12345 {
		t.Errorf("expected ID 12345, got %d", id)
	}
}

func TestSetWebhook(t *testing.T) {
	var capturedBody map[string]string
	_, client := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/setWebhook") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		okJSON(t, w, true)
	})

	err := client.SetWebhook(context.Background(), "token", "https://example.com/hook", "mysecret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody["url"] != "https://example.com/hook" {
		t.Errorf("url: got %q", capturedBody["url"])
	}
	if capturedBody["secret_token"] != "mysecret" {
		t.Errorf("secret_token: got %q", capturedBody["secret_token"])
	}
}

func TestDeleteWebhook(t *testing.T) {
	called := false
	_, client := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deleteWebhook") {
			called = true
		}
		okJSON(t, w, true)
	})

	if err := client.DeleteWebhook(context.Background(), "token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("deleteWebhook endpoint not called")
	}
}

func TestGetWebhookInfo(t *testing.T) {
	_, client := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		okJSON(t, w, map[string]any{
			"url":                    "https://example.com/hook",
			"has_custom_certificate": false,
			"pending_update_count":   5,
		})
	})

	wi, err := client.GetWebhookInfo(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wi.URL != "https://example.com/hook" {
		t.Errorf("URL: got %q", wi.URL)
	}
	if wi.PendingUpdateCount != 5 {
		t.Errorf("PendingUpdateCount: got %d", wi.PendingUpdateCount)
	}
}

func TestSendMessage(t *testing.T) {
	var capturedBody map[string]any
	_, client := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		okJSON(t, w, map[string]any{"message_id": 1})
	})

	if err := client.SendMessage(context.Background(), "token", 123456, "Hello!"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chatID, ok := capturedBody["chat_id"].(float64); !ok || int64(chatID) != 123456 {
		t.Errorf("chat_id: got %v", capturedBody["chat_id"])
	}
	if capturedBody["text"] != "Hello!" {
		t.Errorf("text: got %v", capturedBody["text"])
	}
}

func TestClient_ErrorResponse(t *testing.T) {
	_, client := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Unauthorized"})
	})

	_, err := client.GetBotID(context.Background(), "bad-token")
	if err == nil {
		t.Error("expected error for non-OK response")
	}
}
