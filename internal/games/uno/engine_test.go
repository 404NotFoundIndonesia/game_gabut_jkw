package uno_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/games"
	"github.com/404NFIDv2/bot-game-management/internal/games/uno"
)

var seededRng = rand.New(rand.NewSource(42))

func newEngine() *uno.Engine { return uno.New(rand.New(rand.NewSource(42))) }

var twoPlayers = []games.Player{
	{TelegramUserID: 1, DisplayName: "Alice"},
	{TelegramUserID: 2, DisplayName: "Bob"},
}

func initState(t *testing.T, players []games.Player) json.RawMessage {
	t.Helper()
	raw, err := newEngine().Init(players, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return raw
}

// ── Init tests ────────────────────────────────────────────────────────────────

func TestInit_CardCount(t *testing.T) {
	raw := initState(t, twoPlayers)

	var s struct {
		DrawPile    []json.RawMessage            `json:"draw_pile"`
		DiscardPile []json.RawMessage            `json:"discard_pile"`
		Hands       map[string][]json.RawMessage `json:"hands"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 108 total cards - 2*7 (hands) - 1 (first discard) = 87 in draw pile
	total := len(s.DrawPile) + len(s.DiscardPile)
	for _, h := range s.Hands {
		total += len(h)
	}
	if total != 108 {
		t.Errorf("expected 108 total cards, got %d", total)
	}
	for pid, h := range s.Hands {
		if len(h) != 7 {
			t.Errorf("player %s should have 7 cards, got %d", pid, len(h))
		}
	}
}

func TestInit_RequiresTwoPlayers(t *testing.T) {
	e := newEngine()
	_, err := e.Init([]games.Player{{TelegramUserID: 1}}, nil)
	if err == nil {
		t.Error("expected error for single player")
	}
}

func TestInit_HandSizeOption(t *testing.T) {
	e := newEngine()
	raw, err := e.Init(twoPlayers, map[string]any{"hand_size": float64(5)})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	var s struct {
		Hands map[string][]json.RawMessage `json:"hands"`
	}
	json.Unmarshal(raw, &s)
	for _, h := range s.Hands {
		if len(h) != 5 {
			t.Errorf("expected hand size 5, got %d", len(h))
		}
	}
}

// ── Validate tests ────────────────────────────────────────────────────────────

func TestValidate_WrongPlayer(t *testing.T) {
	e := newEngine()
	raw := initState(t, twoPlayers)

	// Figure out whose turn it is, then use the other player.
	var s struct {
		PlayerOrder    []int64 `json:"player_order"`
		CurrentTurnIdx int     `json:"current_turn_idx"`
	}
	json.Unmarshal(raw, &s)
	wrongPlayer := s.PlayerOrder[1-s.CurrentTurnIdx]

	err := e.Validate(raw, games.Move{
		PlayerID: wrongPlayer,
		Payload:  map[string]any{"action": "draw"},
	})
	if err == nil {
		t.Error("expected error for wrong player")
	}
}

func TestValidate_UnknownAction(t *testing.T) {
	e := newEngine()
	raw := initState(t, twoPlayers)

	var s struct {
		PlayerOrder    []int64 `json:"player_order"`
		CurrentTurnIdx int     `json:"current_turn_idx"`
	}
	json.Unmarshal(raw, &s)
	current := s.PlayerOrder[s.CurrentTurnIdx]

	err := e.Validate(raw, games.Move{
		PlayerID: current,
		Payload:  map[string]any{"action": "invalid"},
	})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

// ── Play card helpers ─────────────────────────────────────────────────────────

type stateView struct {
	DrawPile       []uno.Card            `json:"draw_pile"`
	DiscardPile    []uno.Card            `json:"discard_pile"`
	Hands          map[int64][]uno.Card  `json:"hands"`
	PlayerOrder    []int64               `json:"player_order"`
	CurrentTurnIdx int                   `json:"current_turn_idx"`
	Direction      int                   `json:"direction"`
	Status         string                `json:"status"`
}

func parseView(t *testing.T, raw json.RawMessage) stateView {
	t.Helper()
	var s stateView
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parseView: %v", err)
	}
	return s
}

// buildStateWithHands constructs a minimal state JSON for testing specific hands.
func buildStateWithHands(t *testing.T, p1Hand, p2Hand []uno.Card, topDiscard uno.Card) json.RawMessage {
	t.Helper()
	s := stateView{
		DrawPile:    []uno.Card{{Color: "red", Value: "5"}},
		DiscardPile: []uno.Card{topDiscard},
		Hands:       map[int64][]uno.Card{1: p1Hand, 2: p2Hand},
		PlayerOrder: []int64{1, 2},
		Direction:   1,
		Status:      "in_progress",
	}
	b, _ := json.Marshal(s)
	return b
}

func TestPlayCard_Valid(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "red", Value: "3"}, {Color: "blue", Value: "5"}}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0)},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected events")
	}
	sv := parseView(t, newRaw)
	if len(sv.Hands[1]) != 1 {
		t.Errorf("expected 1 card in hand after play, got %d", len(sv.Hands[1]))
	}
}

func TestPlayCard_WrongCard(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "blue", Value: "3"}}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	err := e.Validate(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0)},
	})
	if err == nil {
		t.Error("expected error for non-matching card")
	}
}

func TestPlayCard_Skip(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "red", Value: "skip"}, {Color: "blue", Value: "3"}}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0)},
	})
	if err != nil {
		t.Fatalf("Apply skip: %v", err)
	}
	sv := parseView(t, newRaw)
	// After skip, turn should return to player 1 (2 players: skip p2, back to p1).
	if sv.PlayerOrder[sv.CurrentTurnIdx] != 1 {
		t.Errorf("expected turn to be player 1 after skip, got %d", sv.PlayerOrder[sv.CurrentTurnIdx])
	}
	hasSkip := false
	for _, ev := range events {
		if ev.Type == "TURN_SKIPPED" {
			hasSkip = true
		}
	}
	if !hasSkip {
		t.Error("expected TURN_SKIPPED event")
	}
}

func TestPlayCard_Reverse(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "red", Value: "reverse"}, {Color: "blue", Value: "3"}}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0)},
	})
	if err != nil {
		t.Fatalf("Apply reverse: %v", err)
	}
	sv := parseView(t, newRaw)
	if sv.Direction != -1 {
		t.Errorf("expected direction -1 after reverse, got %d", sv.Direction)
	}
	hasReverse := false
	for _, ev := range events {
		if ev.Type == "DIRECTION_REVERSED" {
			hasReverse = true
		}
	}
	if !hasReverse {
		t.Error("expected DIRECTION_REVERSED event")
	}
}

func TestPlayCard_DrawTwo(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "red", Value: "draw_two"}, {Color: "blue", Value: "3"}}
	p2Hand := []uno.Card{{Color: "green", Value: "9"}}
	raw := buildStateWithHands(t, p1Hand, p2Hand, top)

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0)},
	})
	if err != nil {
		t.Fatalf("Apply draw_two: %v", err)
	}
	sv := parseView(t, newRaw)
	// Player 2 should have drawn 2, their turn is skipped → back to player 1.
	if len(sv.Hands[2]) != 3 {
		t.Errorf("expected p2 to have 3 cards after draw_two, got %d", len(sv.Hands[2]))
	}
	_ = events
}

func TestPlayCard_Wild(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "wild", Value: "wild"}}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	newRaw, _, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0), "chosen_color": "blue"},
	})
	if err != nil {
		t.Fatalf("Apply wild: %v", err)
	}
	sv := parseView(t, newRaw)
	topCard := sv.DiscardPile[len(sv.DiscardPile)-1]
	if topCard.Color != "blue" {
		t.Errorf("expected discard top color blue, got %s", topCard.Color)
	}
}

func TestPlayCard_WildRequiresChosenColor(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "wild", Value: "wild"}}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	err := e.Validate(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0)},
	})
	if err == nil {
		t.Error("expected error when chosen_color missing for wild")
	}
}

func TestPlayCard_WildDraw4_HasMatchingColor(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	// Player has a red card — WD4 should be invalid.
	p1Hand := []uno.Card{
		{Color: "wild", Value: "wild_draw_four"},
		{Color: "red", Value: "3"},
	}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	err := e.Validate(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0), "chosen_color": "blue"},
	})
	if err == nil {
		t.Error("expected error: player has matching color card, WD4 invalid")
	}
}

func TestPlayCard_WildDraw4_NoMatchingColor(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	// Player has no red cards — WD4 valid.
	p1Hand := []uno.Card{
		{Color: "wild", Value: "wild_draw_four"},
		{Color: "blue", Value: "3"},
	}
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	err := e.Validate(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0), "chosen_color": "blue"},
	})
	if err != nil {
		t.Errorf("expected WD4 valid, got: %v", err)
	}
}

// ── Win condition ─────────────────────────────────────────────────────────────

func TestWinCondition(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "red", Value: "3"}} // one card left
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "play_card", "card_index": float64(0)},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	hasWon := false
	hasGameOver := false
	for _, ev := range events {
		if ev.Type == "PLAYER_WON" {
			hasWon = true
		}
		if ev.Type == "GAME_OVER" {
			hasGameOver = true
		}
	}
	if !hasWon || !hasGameOver {
		t.Errorf("expected PLAYER_WON and GAME_OVER events, got %v", events)
	}

	finished, result, err := e.Evaluate(newRaw)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !finished {
		t.Error("expected game finished")
	}
	if len(result.Winners) == 0 || result.Winners[0].TelegramUserID != 1 {
		t.Errorf("expected winner player 1, got %v", result.Winners)
	}
}

func TestEvaluate_NotFinished(t *testing.T) {
	e := newEngine()
	raw := initState(t, twoPlayers)
	finished, _, err := e.Evaluate(raw)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if finished {
		t.Error("expected game not finished after init")
	}
}

// ── Draw action ───────────────────────────────────────────────────────────────

func TestDraw_AddsCardToHand(t *testing.T) {
	e := newEngine()
	top := uno.Card{Color: "red", Value: "5"}
	p1Hand := []uno.Card{{Color: "blue", Value: "3"}} // can't play
	raw := buildStateWithHands(t, p1Hand, []uno.Card{{Color: "green", Value: "9"}}, top)

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "draw"},
	})
	if err != nil {
		t.Fatalf("Apply draw: %v", err)
	}
	sv := parseView(t, newRaw)
	if len(sv.Hands[1]) != 2 {
		t.Errorf("expected 2 cards after draw, got %d", len(sv.Hands[1]))
	}
	hasDrawn := false
	for _, ev := range events {
		if ev.Type == "CARD_DRAWN" {
			hasDrawn = true
		}
	}
	if !hasDrawn {
		t.Error("expected CARD_DRAWN event")
	}
}

// Keep seededRng used to prevent unused import.
var _ = seededRng
