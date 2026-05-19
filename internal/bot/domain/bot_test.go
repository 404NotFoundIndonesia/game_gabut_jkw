package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
)

func TestBotToken_MarshalJSON_Redacted(t *testing.T) {
	tok := domain.NewBotToken("super-secret-encrypted-value")
	b, err := json.Marshal(tok)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"[REDACTED]"` {
		t.Errorf("expected [REDACTED], got %s", string(b))
	}
}

func TestBotToken_String_Redacted(t *testing.T) {
	tok := domain.NewBotToken("secret")
	if tok.String() != "[REDACTED]" {
		t.Errorf("String(): got %q", tok.String())
	}
}

func TestBotToken_CiphertextReturnsValue(t *testing.T) {
	tok := domain.NewBotToken("encrypted-blob")
	if tok.Ciphertext() != "encrypted-blob" {
		t.Errorf("Ciphertext(): got %q", tok.Ciphertext())
	}
}

func TestNewBot_Defaults(t *testing.T) {
	tok := domain.NewBotToken("enc")
	bot := domain.NewBot("MyBot", tok, "hash", 123456)
	if bot.ID.String() == "" {
		t.Error("expected non-empty UUID")
	}
	if !bot.Active {
		t.Error("new bot should be active")
	}
	if bot.Name != "MyBot" {
		t.Errorf("name: got %q", bot.Name)
	}
	if bot.TelegramID != 123456 {
		t.Errorf("telegram_id: got %d", bot.TelegramID)
	}
}

func TestBot_Deactivate(t *testing.T) {
	bot := domain.NewBot("b", domain.NewBotToken("x"), "h", 1)
	before := bot.UpdatedAt
	time.Sleep(time.Millisecond)
	bot.Deactivate()
	if bot.Active {
		t.Error("expected active=false after Deactivate")
	}
	if !bot.UpdatedAt.After(before) {
		t.Error("UpdatedAt should advance after Deactivate")
	}
}

func TestBot_Activate(t *testing.T) {
	bot := domain.NewBot("b", domain.NewBotToken("x"), "h", 1)
	bot.Deactivate()
	bot.Activate()
	if !bot.Active {
		t.Error("expected active=true after Activate")
	}
}

func TestBot_RotateToken(t *testing.T) {
	bot := domain.NewBot("b", domain.NewBotToken("old"), "old-hash", 1)
	before := bot.UpdatedAt
	time.Sleep(time.Millisecond)
	newTok := domain.NewBotToken("new")
	bot.RotateToken(newTok, "new-hash")
	if bot.Token.Ciphertext() != "new" {
		t.Errorf("token not rotated: got %q", bot.Token.Ciphertext())
	}
	if bot.TokenHash != "new-hash" {
		t.Errorf("token hash not rotated: got %q", bot.TokenHash)
	}
	if !bot.UpdatedAt.After(before) {
		t.Error("UpdatedAt should advance after RotateToken")
	}
}

func TestBot_JSONDoesNotLeakToken(t *testing.T) {
	bot := domain.NewBot("b", domain.NewBotToken("real-secret"), "h", 1)
	b, err := json.Marshal(bot)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if contains(b, "real-secret") {
		t.Error("raw token must not appear in JSON output")
	}
}

func contains(b []byte, sub string) bool {
	return len(b) > 0 && string(b) != "" &&
		func() bool {
			for i := 0; i <= len(b)-len(sub); i++ {
				if string(b[i:i+len(sub)]) == sub {
					return true
				}
			}
			return false
		}()
}
