package pagination_test

import (
	"testing"

	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

func TestParseFromQuery_Defaults(t *testing.T) {
	p, err := pagination.ParseFromQuery("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Limit != 10 {
		t.Errorf("default limit: got %d, want 10", p.Limit)
	}
	if p.Offset != 0 {
		t.Errorf("default offset: got %d, want 0", p.Offset)
	}
}

func TestParseFromQuery_ValidValues(t *testing.T) {
	p, err := pagination.ParseFromQuery("25", "50")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Limit != 25 {
		t.Errorf("limit: got %d, want 25", p.Limit)
	}
	if p.Offset != 50 {
		t.Errorf("offset: got %d, want 50", p.Offset)
	}
}

func TestParseFromQuery_MaxClamp(t *testing.T) {
	p, err := pagination.ParseFromQuery("9999", "0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Limit != 100 {
		t.Errorf("expected limit clamped to 100, got %d", p.Limit)
	}
}

func TestParseFromQuery_NegativeOffset(t *testing.T) {
	_, err := pagination.ParseFromQuery("10", "-1")
	if err == nil {
		t.Error("expected error for negative offset")
	}
}

func TestParseFromQuery_ZeroLimit(t *testing.T) {
	_, err := pagination.ParseFromQuery("0", "0")
	if err == nil {
		t.Error("expected error for zero limit")
	}
}

func TestParseFromQuery_InvalidLimit(t *testing.T) {
	_, err := pagination.ParseFromQuery("abc", "0")
	if err == nil {
		t.Error("expected error for non-numeric limit")
	}
}

func TestParseFromQuery_InvalidOffset(t *testing.T) {
	_, err := pagination.ParseFromQuery("10", "xyz")
	if err == nil {
		t.Error("expected error for non-numeric offset")
	}
}

func TestNewMeta(t *testing.T) {
	p := pagination.Params{Limit: 10, Offset: 20}
	m := pagination.NewMeta(100, p)
	if m.Total != 100 || m.Limit != 10 || m.Offset != 20 {
		t.Errorf("unexpected meta: %+v", m)
	}
}
