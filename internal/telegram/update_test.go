package telegram_test

import (
	"encoding/json"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/telegram"
)

const sampleUpdateJSON = `{
	"update_id": 123456789,
	"message": {
		"message_id": 42,
		"from": {
			"id": 987654321,
			"is_bot": false,
			"first_name": "John",
			"last_name": "Doe",
			"username": "johndoe"
		},
		"chat": {
			"id": -1001234567890,
			"type": "supergroup"
		},
		"text": "/addbot",
		"date": 1700000000
	}
}`

func TestUpdate_Unmarshal(t *testing.T) {
	var u telegram.Update
	if err := json.Unmarshal([]byte(sampleUpdateJSON), &u); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if u.UpdateID != 123456789 {
		t.Errorf("UpdateID: got %d", u.UpdateID)
	}
	if u.Message == nil {
		t.Fatal("Message must not be nil")
	}
	if u.Message.MessageID != 42 {
		t.Errorf("MessageID: got %d", u.Message.MessageID)
	}
	if u.Message.Text != "/addbot" {
		t.Errorf("Text: got %q", u.Message.Text)
	}
	if u.Message.From == nil {
		t.Fatal("From must not be nil")
	}
	if u.Message.From.ID != 987654321 {
		t.Errorf("From.ID: got %d", u.Message.From.ID)
	}
	if u.Message.From.Username != "johndoe" {
		t.Errorf("From.Username: got %q", u.Message.From.Username)
	}
	if u.Message.Chat == nil {
		t.Fatal("Chat must not be nil")
	}
	if u.Message.Chat.ID != -1001234567890 {
		t.Errorf("Chat.ID: got %d", u.Message.Chat.ID)
	}
	if u.Message.Chat.Type != "supergroup" {
		t.Errorf("Chat.Type: got %q", u.Message.Chat.Type)
	}
}

func TestUpdate_NoMessage(t *testing.T) {
	var u telegram.Update
	if err := json.Unmarshal([]byte(`{"update_id": 1}`), &u); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if u.Message != nil {
		t.Error("Message must be nil when absent")
	}
}

func TestWebhookInfo_Unmarshal(t *testing.T) {
	raw := `{"url":"https://example.com/webhook","has_custom_certificate":false,"pending_update_count":3}`
	var wi telegram.WebhookInfo
	if err := json.Unmarshal([]byte(raw), &wi); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if wi.URL != "https://example.com/webhook" {
		t.Errorf("URL: got %q", wi.URL)
	}
	if wi.PendingUpdateCount != 3 {
		t.Errorf("PendingUpdateCount: got %d", wi.PendingUpdateCount)
	}
}
