package truthordate_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/games"
	truthordate "github.com/404NFIDv2/bot-game-management/internal/games/truth_or_date"
)

var twoPlayers = []games.Player{
	{TelegramUserID: 1, DisplayName: "Alice"},
	{TelegramUserID: 2, DisplayName: "Bob"},
}

func newEngine() *truthordate.Engine {
	return truthordate.New(rand.New(rand.NewSource(42)))
}

func initGame(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := newEngine().Init(twoPlayers, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return raw
}

// ── Init ──────────────────────────────────────────────────────────────────────

func TestInit_RequiresTwoPlayers(t *testing.T) {
	e := newEngine()
	_, err := e.Init([]games.Player{{TelegramUserID: 1}}, nil)
	if err == nil {
		t.Error("expected error for single player")
	}
}

// ── Choice truth ──────────────────────────────────────────────────────────────

func TestChoice_Truth_DrawsQuestion(t *testing.T) {
	e := newEngine()
	raw := initGame(t)

	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "choice", "choice": "truth"},
	})
	if err != nil {
		t.Fatalf("Apply choice truth: %v", err)
	}

	hasDrawn := false
	for _, ev := range events {
		if ev.Type == "QUESTION_DRAWN" {
			hasDrawn = true
			if ev.Payload["choice"] != "truth" {
				t.Errorf("expected choice 'truth', got %v", ev.Payload["choice"])
			}
		}
	}
	if !hasDrawn {
		t.Error("expected QUESTION_DRAWN event")
	}

	var s struct {
		CurrentQuestion string `json:"current_question"`
	}
	_ = json.Unmarshal(newRaw, &s)
	if s.CurrentQuestion == "" {
		t.Error("expected current_question to be set")
	}
}

// ── Choice date ───────────────────────────────────────────────────────────────

func TestChoice_Date_DrawsQuestion(t *testing.T) {
	e := newEngine()
	raw := initGame(t)

	_, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "choice", "choice": "date"},
	})
	if err != nil {
		t.Fatalf("Apply choice date: %v", err)
	}

	hasDrawn := false
	for _, ev := range events {
		if ev.Type == "QUESTION_DRAWN" && ev.Payload["choice"] == "date" {
			hasDrawn = true
		}
	}
	if !hasDrawn {
		t.Error("expected QUESTION_DRAWN event with choice=date")
	}
}

// ── Answer ────────────────────────────────────────────────────────────────────

func TestAnswer_RecordsResponse(t *testing.T) {
	e := newEngine()
	raw := initGame(t)

	// First pick truth.
	raw, _, _ = e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "choice", "choice": "truth"},
	})

	// Then answer.
	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "answer", "answer": "Tidak tahu"},
	})
	if err != nil {
		t.Fatalf("Apply answer: %v", err)
	}

	hasRecorded := false
	for _, ev := range events {
		if ev.Type == "ANSWER_RECORDED" {
			hasRecorded = true
		}
	}
	if !hasRecorded {
		t.Error("expected ANSWER_RECORDED event")
	}

	var s struct {
		Responses []struct {
			Answer string `json:"answer"`
		} `json:"responses"`
		CurrentTurnIdx int `json:"current_turn_idx"`
	}
	_ = json.Unmarshal(newRaw, &s)
	if len(s.Responses) == 0 || s.Responses[0].Answer != "Tidak tahu" {
		t.Errorf("expected response recorded, got %v", s.Responses)
	}
	// Turn should have advanced to player 2.
	if s.CurrentTurnIdx != 1 {
		t.Errorf("expected turn index 1 (player 2), got %d", s.CurrentTurnIdx)
	}
}

// ── Skip ──────────────────────────────────────────────────────────────────────

func TestSkip_NonHost_Error(t *testing.T) {
	e := newEngine()
	raw := initGame(t)

	// Draw question for player 1.
	raw, _, _ = e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "choice", "choice": "truth"},
	})

	// Player 2 tries to skip — not host.
	err := e.Validate(raw, games.Move{
		PlayerID: 2,
		Payload:  map[string]any{"action": "skip"},
	})
	if err == nil {
		t.Error("expected error: non-host cannot skip")
	}
}

func TestSkip_Host_Advances(t *testing.T) {
	e := newEngine()
	raw := initGame(t)

	// Player 1 (host) picks truth.
	raw, _, _ = e.Apply(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "choice", "choice": "truth"},
	})

	// Host skips.
	newRaw, events, err := e.Apply(raw, games.Move{
		PlayerID: 1, // host
		Payload:  map[string]any{"action": "skip"},
	})
	if err != nil {
		t.Fatalf("Apply skip: %v", err)
	}

	hasSkipped := false
	for _, ev := range events {
		if ev.Type == "QUESTION_SKIPPED" {
			hasSkipped = true
		}
	}
	if !hasSkipped {
		t.Error("expected QUESTION_SKIPPED event")
	}

	var s struct {
		CurrentTurnIdx int `json:"current_turn_idx"`
	}
	_ = json.Unmarshal(newRaw, &s)
	if s.CurrentTurnIdx != 1 {
		t.Errorf("expected turn to advance to player 2, got idx %d", s.CurrentTurnIdx)
	}
}

// ── Score ─────────────────────────────────────────────────────────────────────

func TestEvaluate_ScoreEqualsTurnsCompleted(t *testing.T) {
	e := newEngine()
	raw, _ := e.Init(twoPlayers, nil)

	// Manually mark as finished and add 2 responses for player 1.
	var s truthordate.State
	_ = json.Unmarshal(raw, &s)
	s.Status = "finished"
	s.Responses = []truthordate.Response{
		{PlayerID: 1, Choice: "truth", Answer: "yes"},
		{PlayerID: 2, Choice: "date", Answer: "done"},
		{PlayerID: 1, Choice: "truth", Answer: "no"},
	}
	finishedRaw, _ := json.Marshal(s)

	finished, result, err := e.Evaluate(finishedRaw)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !finished {
		t.Error("expected finished")
	}
	if result.Scores[1] != 2 {
		t.Errorf("expected player 1 score 2, got %d", result.Scores[1])
	}
	if result.Scores[2] != 1 {
		t.Errorf("expected player 2 score 1, got %d", result.Scores[2])
	}
}

// ── Evaluate not finished ─────────────────────────────────────────────────────

func TestEvaluate_NotFinished(t *testing.T) {
	e := newEngine()
	raw := initGame(t)
	finished, _, err := e.Evaluate(raw)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if finished {
		t.Error("expected not finished after init")
	}
}

// ── Invalid choice ────────────────────────────────────────────────────────────

func TestChoice_InvalidValue(t *testing.T) {
	e := newEngine()
	raw := initGame(t)
	err := e.Validate(raw, games.Move{
		PlayerID: 1,
		Payload:  map[string]any{"action": "choice", "choice": "dare"},
	})
	if err == nil {
		t.Error("expected error for invalid choice value")
	}
}
