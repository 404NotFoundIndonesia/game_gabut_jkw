package crypto_test

import (
	"strings"
	"testing"

	"github.com/404NFIDv2/bot-game-management/pkg/crypto"
)

var testKey = crypto.PadKey("test-key-for-unit-tests-only!!")

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	plaintext := "1234567890:AAFxyz_telegram_bot_token"
	ct, err := crypto.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := crypto.Decrypt(testKey, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Errorf("round-trip: got %q, want %q", got, plaintext)
	}
}

func TestEncrypt_CiphertextNeverEqualsPlaintext(t *testing.T) {
	plaintext := "secret-bot-token"
	ct, err := crypto.Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ct == plaintext {
		t.Error("ciphertext must not equal plaintext")
	}
}

func TestEncrypt_NonceIsRandom(t *testing.T) {
	// Two encryptions of the same plaintext must yield different ciphertext (random nonce).
	ct1, _ := crypto.Encrypt(testKey, "same")
	ct2, _ := crypto.Encrypt(testKey, "same")
	if ct1 == ct2 {
		t.Error("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	ct, err := crypto.Encrypt(testKey, "secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	wrongKey := crypto.PadKey("wrong-key-32-bytes-padding!!!!!")
	_, err = crypto.Decrypt(wrongKey, ct)
	if err == nil {
		t.Error("expected error decrypting with wrong key")
	}
}

func TestEncryptDecrypt_EmptyString(t *testing.T) {
	ct, err := crypto.Encrypt(testKey, "")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	got, err := crypto.Decrypt(testKey, ct)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestEncryptDecrypt_LongToken(t *testing.T) {
	long := strings.Repeat("a", 4096)
	ct, err := crypto.Encrypt(testKey, long)
	if err != nil {
		t.Fatalf("Encrypt long: %v", err)
	}
	got, err := crypto.Decrypt(testKey, ct)
	if err != nil {
		t.Fatalf("Decrypt long: %v", err)
	}
	if got != long {
		t.Error("long token round-trip failed")
	}
}

func TestEncrypt_KeyTooShort(t *testing.T) {
	_, err := crypto.Encrypt([]byte("short"), "x")
	if err == nil {
		t.Error("expected error for key != 32 bytes")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	ct, _ := crypto.Encrypt(testKey, "payload")
	tampered := ct[:len(ct)-4] + "XXXX"
	_, err := crypto.Decrypt(testKey, tampered)
	if err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}
