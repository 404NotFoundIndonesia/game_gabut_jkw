package truthordate

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/404NFIDv2/bot-game-management/internal/games"
)

//go:embed questions/questions.json
var questionsJSON []byte

// ── Question bank ─────────────────────────────────────────────────────────────

type questionBank struct {
	Truth []string `json:"truth"`
	Date  []string `json:"date"`
}

var bank questionBank

func init() {
	if err := json.Unmarshal(questionsJSON, &bank); err != nil {
		panic("truth_or_date: invalid questions.json: " + err.Error())
	}
}

// ── State ─────────────────────────────────────────────────────────────────────

const (
	StatusInProgress = "in_progress"
	StatusFinished   = "finished"
)

// Response records a single player's answer to their question.
type Response struct {
	PlayerID int64  `json:"player_id"`
	Choice   string `json:"choice"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Skipped  bool   `json:"skipped"`
}

// State holds the mutable game state for Truth or Date.
type State struct {
	PlayerOrder     []int64    `json:"player_order"`
	HostPlayerID    int64      `json:"host_player_id"`
	CurrentTurnIdx  int        `json:"current_turn_idx"`
	CurrentQuestion string     `json:"current_question"`
	CurrentChoice   string     `json:"current_choice"`
	Round           int        `json:"round"`
	Responses       []Response `json:"responses"`
	Status          string     `json:"status"`
}

func (s *State) currentPlayer() int64 {
	return s.PlayerOrder[s.CurrentTurnIdx]
}

func (s *State) advanceTurn() {
	s.CurrentTurnIdx = (s.CurrentTurnIdx + 1) % len(s.PlayerOrder)
	if s.CurrentTurnIdx == 0 {
		s.Round++
	}
	s.CurrentQuestion = ""
	s.CurrentChoice = ""
}

// ── Event types ───────────────────────────────────────────────────────────────

const (
	EventQuestionDrawn   = "QUESTION_DRAWN"
	EventAnswerRecorded  = "ANSWER_RECORDED"
	EventQuestionSkipped = "QUESTION_SKIPPED"
)

// ── Engine ────────────────────────────────────────────────────────────────────

// Engine implements games.GameEngine for Truth or Date.
type Engine struct {
	rng *rand.Rand
}

// New creates a Truth or Date engine. Pass nil for a default random source.
func New(rng *rand.Rand) *Engine {
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}
	return &Engine{rng: rng}
}

func (e *Engine) Init(players []games.Player, opts map[string]any) (json.RawMessage, error) {
	if len(players) < 2 {
		return nil, fmt.Errorf("truth or date requires at least 2 players")
	}

	order := make([]int64, len(players))
	for i, p := range players {
		order[i] = p.TelegramUserID
	}

	s := State{
		PlayerOrder:  order,
		HostPlayerID: order[0],
		Round:        1,
		Responses:    []Response{},
		Status:       StatusInProgress,
	}

	return marshalState(s)
}

func (e *Engine) Validate(raw json.RawMessage, move games.Move) error {
	s, err := parseState(raw)
	if err != nil {
		return err
	}

	if s.Status == StatusFinished {
		return fmt.Errorf("game is already finished")
	}

	action, _ := move.Payload["action"].(string)

	switch action {
	case "choice":
		if s.currentPlayer() != move.PlayerID {
			return fmt.Errorf("not your turn")
		}
		if s.CurrentQuestion != "" {
			return fmt.Errorf("question already drawn; submit answer first")
		}
		choice, _ := move.Payload["choice"].(string)
		if choice != "truth" && choice != "date" {
			return fmt.Errorf("choice must be 'truth' or 'date'")
		}
	case "answer":
		if s.currentPlayer() != move.PlayerID {
			return fmt.Errorf("not your turn")
		}
		if s.CurrentQuestion == "" {
			return fmt.Errorf("no question pending; pick truth or date first")
		}
		if _, ok := move.Payload["answer"].(string); !ok {
			return fmt.Errorf("answer is required")
		}
	case "skip":
		if move.PlayerID != s.HostPlayerID {
			return fmt.Errorf("only the host can skip a question")
		}
		if s.CurrentQuestion == "" {
			return fmt.Errorf("no question pending to skip")
		}
	default:
		return fmt.Errorf("unknown action: %q", action)
	}

	return nil
}

func (e *Engine) Apply(raw json.RawMessage, move games.Move) (json.RawMessage, []games.Event, error) {
	if err := e.Validate(raw, move); err != nil {
		return nil, nil, err
	}

	s, err := parseState(raw)
	if err != nil {
		return nil, nil, err
	}

	action, _ := move.Payload["action"].(string)
	var events []games.Event

	switch action {
	case "choice":
		choice, _ := move.Payload["choice"].(string)
		question := e.drawQuestion(choice)
		s.CurrentQuestion = question
		s.CurrentChoice = choice
		events = append(events, games.Event{
			Type:    EventQuestionDrawn,
			Payload: map[string]any{"player_id": move.PlayerID, "choice": choice, "question": question},
		})

	case "answer":
		answer, _ := move.Payload["answer"].(string)
		s.Responses = append(s.Responses, Response{
			PlayerID: move.PlayerID,
			Choice:   s.CurrentChoice,
			Question: s.CurrentQuestion,
			Answer:   answer,
		})
		events = append(events, games.Event{
			Type:    EventAnswerRecorded,
			Payload: map[string]any{"player_id": move.PlayerID, "answer": answer},
		})
		s.advanceTurn()

	case "skip":
		s.Responses = append(s.Responses, Response{
			PlayerID: s.currentPlayer(),
			Choice:   s.CurrentChoice,
			Question: s.CurrentQuestion,
			Skipped:  true,
		})
		events = append(events, games.Event{
			Type:    EventQuestionSkipped,
			Payload: map[string]any{"player_id": s.currentPlayer()},
		})
		s.advanceTurn()
	}

	newRaw, err := marshalState(s)
	return newRaw, events, err
}

func (e *Engine) Evaluate(raw json.RawMessage) (bool, games.Result, error) {
	s, err := parseState(raw)
	if err != nil {
		return false, games.Result{}, err
	}

	if s.Status != StatusFinished {
		return false, games.Result{}, nil
	}

	// Score = number of answered (non-skipped) turns per player.
	scores := make(map[int64]int)
	for _, r := range s.Responses {
		if !r.Skipped {
			scores[r.PlayerID]++
		}
	}

	var maxScore int
	for _, sc := range scores {
		if sc > maxScore {
			maxScore = sc
		}
	}
	var winners []games.Player
	for id, sc := range scores {
		if sc == maxScore {
			winners = append(winners, games.Player{TelegramUserID: id})
		}
	}

	return true, games.Result{Winners: winners, Scores: scores}, nil
}

// drawQuestion picks a random question of the given type.
func (e *Engine) drawQuestion(choice string) string {
	var pool []string
	switch choice {
	case "truth":
		pool = bank.Truth
	case "date":
		pool = bank.Date
	}
	if len(pool) == 0 {
		return ""
	}
	return pool[e.rng.Intn(len(pool))]
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func parseState(raw json.RawMessage) (State, error) {
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("invalid truth_or_date state: %w", err)
	}
	return s, nil
}

func marshalState(s State) (json.RawMessage, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal truth_or_date state: %w", err)
	}
	return b, nil
}
