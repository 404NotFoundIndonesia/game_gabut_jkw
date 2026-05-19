package sambungkata

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/404NFIDv2/bot-game-management/internal/games"
	"github.com/404NFIDv2/bot-game-management/internal/games/sambung_kata/kbbi"
)

// ── State ─────────────────────────────────────────────────────────────────────

const (
	StatusInProgress = "in_progress"
	StatusFinished   = "finished"
)

// State holds the mutable game state for Sambung Kata.
type State struct {
	LastWord       string  `json:"last_word"`
	UsedWords      []string `json:"used_words"`
	PlayerOrder    []int64  `json:"player_order"`
	CurrentTurnIdx int      `json:"current_turn_idx"`
	Eliminated     []int64  `json:"eliminated"`
	Status         string   `json:"status"`
}

func (s *State) currentPlayer() int64 {
	active := s.activePlayers()
	if len(active) == 0 {
		return 0
	}
	return s.PlayerOrder[s.CurrentTurnIdx]
}

// activePlayers returns players not yet eliminated.
func (s *State) activePlayers() []int64 {
	elim := make(map[int64]bool, len(s.Eliminated))
	for _, id := range s.Eliminated {
		elim[id] = true
	}
	var out []int64
	for _, id := range s.PlayerOrder {
		if !elim[id] {
			out = append(out, id)
		}
	}
	return out
}

func (s *State) isEliminated(id int64) bool {
	for _, e := range s.Eliminated {
		if e == id {
			return true
		}
	}
	return false
}

func (s *State) advanceTurn() {
	n := len(s.PlayerOrder)
	for {
		s.CurrentTurnIdx = (s.CurrentTurnIdx + 1) % n
		if !s.isEliminated(s.PlayerOrder[s.CurrentTurnIdx]) {
			break
		}
	}
}

// ── Event types ───────────────────────────────────────────────────────────────

const (
	EventWordAccepted     = "WORD_ACCEPTED"
	EventWordRejected     = "WORD_REJECTED"
	EventPlayerEliminated = "PLAYER_ELIMINATED"
	EventGameOver         = "GAME_OVER"
)

// ── Engine ────────────────────────────────────────────────────────────────────

// Engine implements games.GameEngine for Sambung Kata.
type Engine struct {
	kbbi kbbi.Validator
}

// New creates a Sambung Kata engine with the given KBBI validator.
func New(validator kbbi.Validator) *Engine {
	return &Engine{kbbi: validator}
}

func (e *Engine) Init(players []games.Player, opts map[string]any) (json.RawMessage, error) {
	if len(players) < 2 {
		return nil, fmt.Errorf("sambung kata requires at least 2 players")
	}

	order := make([]int64, len(players))
	for i, p := range players {
		order[i] = p.TelegramUserID
	}

	startWord := ""
	if v, ok := opts["start_word"]; ok {
		startWord, _ = v.(string)
	}

	s := State{
		LastWord:       startWord,
		UsedWords:      []string{},
		PlayerOrder:    order,
		CurrentTurnIdx: 0,
		Eliminated:     []int64{},
		Status:         StatusInProgress,
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

	if s.isEliminated(move.PlayerID) {
		return fmt.Errorf("player is eliminated")
	}

	if s.currentPlayer() != move.PlayerID {
		return fmt.Errorf("not your turn")
	}

	word, ok := move.Payload["word"].(string)
	if !ok || strings.TrimSpace(word) == "" {
		return fmt.Errorf("word is required")
	}

	word = strings.ToLower(strings.TrimSpace(word))

	// Must start with last letter of last word.
	if s.LastWord != "" {
		lastLetter := string([]rune(s.LastWord)[len([]rune(s.LastWord))-1])
		firstLetter := string([]rune(word)[0])
		if !strings.EqualFold(firstLetter, lastLetter) {
			return fmt.Errorf("word must start with %q", lastLetter)
		}
	}

	// Must not be duplicate.
	for _, used := range s.UsedWords {
		if strings.EqualFold(used, word) {
			return fmt.Errorf("word %q already used", word)
		}
	}

	// Must be in KBBI.
	if !e.kbbi.IsValid(word) {
		return fmt.Errorf("word %q not found in KBBI", word)
	}

	return nil
}

func (e *Engine) Apply(raw json.RawMessage, move games.Move) (json.RawMessage, []games.Event, error) {
	s, err := parseState(raw)
	if err != nil {
		return nil, nil, err
	}

	if s.Status == StatusFinished {
		return nil, nil, fmt.Errorf("game is already finished")
	}

	if s.currentPlayer() != move.PlayerID {
		return nil, nil, fmt.Errorf("not your turn")
	}

	word, _ := move.Payload["word"].(string)
	word = strings.ToLower(strings.TrimSpace(word))

	validErr := e.Validate(raw, move)
	var events []games.Event

	if validErr != nil {
		// Eliminate current player.
		s.Eliminated = append(s.Eliminated, move.PlayerID)
		events = append(events, games.Event{
			Type:    EventWordRejected,
			Payload: map[string]any{"player_id": move.PlayerID, "word": word, "reason": validErr.Error()},
		})
		events = append(events, games.Event{
			Type:    EventPlayerEliminated,
			Payload: map[string]any{"player_id": move.PlayerID},
		})

		active := s.activePlayers()
		if len(active) <= 1 {
			s.Status = StatusFinished
			var winnerID int64
			if len(active) == 1 {
				winnerID = active[0]
			}
			events = append(events, games.Event{
				Type:    EventGameOver,
				Payload: map[string]any{"winner_id": winnerID},
			})
			raw, err := marshalState(s)
			return raw, events, err
		}

		s.advanceTurn()
		raw, err := marshalState(s)
		return raw, events, err
	}

	// Valid word.
	s.LastWord = word
	s.UsedWords = append(s.UsedWords, word)
	events = append(events, games.Event{
		Type:    EventWordAccepted,
		Payload: map[string]any{"player_id": move.PlayerID, "word": word},
	})

	s.advanceTurn()

	active := s.activePlayers()
	if len(active) == 1 {
		s.Status = StatusFinished
		events = append(events, games.Event{
			Type:    EventGameOver,
			Payload: map[string]any{"winner_id": active[0]},
		})
	}

	raw2, err := marshalState(s)
	return raw2, events, err
}

func (e *Engine) Evaluate(raw json.RawMessage) (bool, games.Result, error) {
	s, err := parseState(raw)
	if err != nil {
		return false, games.Result{}, err
	}

	if s.Status != StatusFinished {
		return false, games.Result{}, nil
	}

	active := s.activePlayers()
	scores := make(map[int64]int)
	for _, id := range s.PlayerOrder {
		count := 0
		for _, w := range s.UsedWords {
			_ = w
			count++ // each word submitted counts; approximation per player by counting turns
		}
		scores[id] = 0
	}

	// Score = words accepted (owned by player) — count words in used_words per player via turn order.
	playerWords := make(map[int64]int)
	for i, w := range s.UsedWords {
		_ = w
		pid := s.PlayerOrder[i%len(s.PlayerOrder)]
		playerWords[pid]++
	}
	for id, count := range playerWords {
		scores[id] = count
	}

	var winners []games.Player
	for _, id := range active {
		winners = append(winners, games.Player{TelegramUserID: id})
	}

	return true, games.Result{Winners: winners, Scores: scores}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func parseState(raw json.RawMessage) (State, error) {
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("invalid sambung kata state: %w", err)
	}
	return s, nil
}

func marshalState(s State) (json.RawMessage, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal sambung kata state: %w", err)
	}
	return b, nil
}
