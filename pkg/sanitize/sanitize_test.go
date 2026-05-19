package sanitize_test

import (
	"strings"
	"testing"

	"github.com/404NFIDv2/bot-game-management/pkg/sanitize"
)

func TestString_StripsNullBytes(t *testing.T) {
	input := "hello\x00world"
	got := sanitize.String(input)
	if strings.ContainsRune(got, 0) {
		t.Errorf("null byte not stripped: %q", got)
	}
	if got != "helloworld" {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestString_StripsControlChars(t *testing.T) {
	// ESC, BEL, BS control characters
	input := "\x1b[31m red \x07 \x08"
	got := sanitize.String(input)
	for _, r := range got {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			t.Errorf("control char %U survived sanitization in %q", r, got)
		}
	}
}

func TestString_TrimsWhitespace(t *testing.T) {
	got := sanitize.String("  hello  ")
	if got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestString_PreservesNormalText(t *testing.T) {
	input := "Hello, World! 123"
	got := sanitize.String(input)
	if got != input {
		t.Errorf("expected %q unchanged, got %q", input, got)
	}
}

func TestTruncate_CapsLength(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := sanitize.Truncate(long, 100)
	if len([]rune(got)) != 100 {
		t.Errorf("expected 100 runes, got %d", len([]rune(got)))
	}
}

func TestTruncate_ShortStringUnchanged(t *testing.T) {
	s := "short"
	got := sanitize.Truncate(s, 100)
	if got != s {
		t.Errorf("expected %q, got %q", s, got)
	}
}

func TestDisplayName_SanitizesAndCaps(t *testing.T) {
	input := strings.Repeat("x", 150) + "\x00"
	got := sanitize.DisplayName(input)
	if len([]rune(got)) > 100 {
		t.Errorf("display_name exceeds 100 chars: len=%d", len([]rune(got)))
	}
	if strings.ContainsRune(got, 0) {
		t.Error("null byte survived DisplayName sanitization")
	}
}

func TestWord_RemovesNewlines(t *testing.T) {
	got := sanitize.Word("kucing\nmeong")
	if strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("newline survived Word sanitization: %q", got)
	}
}

func TestReason_AllowsLongText(t *testing.T) {
	input := strings.Repeat("a", 600)
	got := sanitize.Reason(input)
	if len([]rune(got)) > 500 {
		t.Errorf("reason exceeds 500 chars: len=%d", len([]rune(got)))
	}
}
