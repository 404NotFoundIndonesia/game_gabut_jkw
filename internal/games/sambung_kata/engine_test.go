package sambungkata_test

import (
	"encoding/json"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/games"
	sambungkata "github.com/404NFIDv2/bot-game-management/internal/games/sambung_kata"
)

// ── stub KBBI validator ───────────────────────────────────────────────────────

type stubKBBI struct{ valid map[string]bool }

func (s *stubKBBI) IsValid(word string) bool { return s.valid[word] }

func newKBBI(words ...string) *stubKBBI {
	m := make(map[string]bool)
	for _, w := range words {
		m[w] = true
	}
	return &stubKBBI{valid: m}
}

// ── helpers ───────────────────────────────────────────────────────────────────

var twoPlayers = []games.Player{
	{TelegramUserID: 1, DisplayName: "A"},
	{TelegramUserID: 2, DisplayName: "B"},
}

func newEngine(words ...string) *sambungkata.Engine {
	return sambungkata.New(newKBBI(words...))
}

func initGame(t *testing.T, e *sambungkata.Engine, startWord string) json.RawMessage {
	t.Helper()
	opts := map[string]any{"start_word": startWord}
	raw, err := e.Init(twoPlayers, opts)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return raw
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestInit_RequiresTwoPlayers(t *testing.T) {
	e := newEngine()
	_, err := e.Init([]games.Player{{TelegramUserID: 1}}, nil)
	if err == nil {
		t.Error("expected error for single player")
	}
}

func TestValidWord_Accepted(t *testing.T) {
	e := newEngine("apel", "langit")
	raw := initGame(t, e, "apel")

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"word": "langit"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	hasAccepted := false
	for _, ev := range events {
		if ev.Type == "WORD_ACCEPTED" {
			hasAccepted = true
		}
	}
	if !hasAccepted {
		t.Error("expected WORD_ACCEPTED event")
	}

	var s struct {
		LastWord string `json:"last_word"`
	}
	json.Unmarshal(newRaw, &s)
	if s.LastWord != "langit" {
		t.Errorf("expected last_word 'langit', got %q", s.LastWord)
	}
}

func TestWord_WrongFirstLetter(t *testing.T) {
	e := newEngine("apel", "bunga")
	raw := initGame(t, e, "apel")

	// "bunga" starts with 'b', not 'l' (last of "apel").
	err := e.Validate(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"word": "bunga"},
	})
	if err == nil {
		t.Error("expected error for wrong starting letter")
	}
}

func TestWord_Duplicate(t *testing.T) {
	e := newEngine("apel", "langit", "tidur")
	raw := initGame(t, e, "apel")

	// Player 1 submits "langit".
	raw, _, _ = e.Apply(raw, games.Move{PlayerID: 1, Payload: map[string]any{"word": "langit"}})
	// Player 2 submits "tidur".
	raw, _, _ = e.Apply(raw, games.Move{PlayerID: 2, Payload: map[string]any{"word": "tidur"}})
	// Player 1 tries "langit" again — duplicate.
	err := e.Validate(raw, games.Move{PlayerID: 1, Payload: map[string]any{"word": "langit"}})
	if err == nil {
		t.Error("expected error for duplicate word")
	}
}

func TestWord_NotInKBBI(t *testing.T) {
	e := newEngine("apel") // "langit" not in validator
	raw := initGame(t, e, "apel")

	err := e.Validate(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"word": "langit"},
	})
	if err == nil {
		t.Error("expected error for word not in KBBI")
	}
}

func TestPlayerEliminated_OnInvalidWord(t *testing.T) {
	e := newEngine("apel") // "xyz" not valid
	raw := initGame(t, e, "apel")

	_, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"word": "xyz"}, // wrong letter + not in KBBI
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	hasElim := false
	for _, ev := range events {
		if ev.Type == "PLAYER_ELIMINATED" {
			hasElim = true
		}
	}
	if !hasElim {
		t.Error("expected PLAYER_ELIMINATED event")
	}
}

func TestLastPlayerStanding_GameOver(t *testing.T) {
	e := newEngine("apel") // only player 1 submits invalid → eliminated → player 2 wins
	raw := initGame(t, e, "apel")

	// Player 1 submits invalid word → eliminated.
	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"word": "xyz"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	hasGameOver := false
	for _, ev := range events {
		if ev.Type == "GAME_OVER" {
			hasGameOver = true
		}
	}
	if !hasGameOver {
		t.Error("expected GAME_OVER when only one player remains")
	}

	finished, result, err := e.Evaluate(newRaw)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !finished {
		t.Error("expected game finished")
	}
	if len(result.Winners) == 0 || result.Winners[0].TelegramUserID != 2 {
		t.Errorf("expected winner player 2, got %v", result.Winners)
	}
}

func TestNotYourTurn(t *testing.T) {
	e := newEngine("apel", "langit")
	raw := initGame(t, e, "apel")

	// Player 2's turn comes after player 1, but player 2 tries to go first.
	err := e.Validate(raw, games.Move{
		PlayerID: 2,
		Payload:  map[string]any{"word": "langit"},
	})
	if err == nil {
		t.Error("expected error for wrong turn")
	}
}

func TestEvaluate_NotFinished(t *testing.T) {
	e := newEngine("apel")
	raw := initGame(t, e, "apel")
	finished, _, err := e.Evaluate(raw)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if finished {
		t.Error("expected game not finished after init")
	}
}
