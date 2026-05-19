package games_test

import (
	"encoding/json"
	"testing"

	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	"github.com/404NFIDv2/bot-game-management/internal/games"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// stubEngine satisfies games.GameEngine for registry tests.
type stubEngine struct{ slug string }

func (s *stubEngine) Init(_ []games.Player, _ map[string]any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (s *stubEngine) Apply(_ json.RawMessage, _ games.Move) (json.RawMessage, []games.Event, error) {
	return json.RawMessage(`{}`), nil, nil
}
func (s *stubEngine) Evaluate(_ json.RawMessage) (bool, games.Result, error) {
	return false, games.Result{}, nil
}
func (s *stubEngine) Validate(_ json.RawMessage, _ games.Move) error { return nil }

func TestRegistry_GetRegisteredEngine(t *testing.T) {
	r := games.NewRegistry()
	e := &stubEngine{slug: "uno"}
	r.Register(gamedomain.SlugUno, e)

	got, err := r.Get(gamedomain.SlugUno)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != e {
		t.Error("expected same engine instance")
	}
}

func TestRegistry_UnknownSlug_ReturnsInternalError(t *testing.T) {
	r := games.NewRegistry()

	_, err := r.Get(gamedomain.SlugUno)
	if err == nil {
		t.Fatal("expected error for unknown slug")
	}
	ae, ok := err.(*apperrors.AppError)
	if !ok {
		t.Fatalf("expected *AppError, got %T", err)
	}
	if ae.Code != apperrors.CodeInternal {
		t.Errorf("expected INTERNAL_ERROR code, got %s", ae.Code)
	}
}

func TestRegistry_AllThreeSlugs(t *testing.T) {
	r := games.NewRegistry()
	slugs := []gamedomain.GameSlug{
		gamedomain.SlugUno,
		gamedomain.SlugSambungKata,
		gamedomain.SlugTruthOrDate,
	}
	for _, slug := range slugs {
		r.Register(slug, &stubEngine{slug: string(slug)})
	}
	for _, slug := range slugs {
		e, err := r.Get(slug)
		if err != nil {
			t.Errorf("Get(%s): %v", slug, err)
		}
		if e == nil {
			t.Errorf("expected non-nil engine for %s", slug)
		}
	}
}
