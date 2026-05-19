package uno

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/404NFIDv2/bot-game-management/internal/games"
)

// ── Card types ────────────────────────────────────────────────────────────────

type Color string
type Value string

const (
	ColorRed    Color = "red"
	ColorGreen  Color = "green"
	ColorBlue   Color = "blue"
	ColorYellow Color = "yellow"
	ColorWild   Color = "wild"

	ValueZero      Value = "0"
	ValueOne       Value = "1"
	ValueTwo       Value = "2"
	ValueThree     Value = "3"
	ValueFour      Value = "4"
	ValueFive      Value = "5"
	ValueSix       Value = "6"
	ValueSeven     Value = "7"
	ValueEight     Value = "8"
	ValueNine      Value = "9"
	ValueSkip      Value = "skip"
	ValueReverse   Value = "reverse"
	ValueDrawTwo   Value = "draw_two"
	ValueWild      Value = "wild"
	ValueWildDraw4 Value = "wild_draw_four"
)

type Card struct {
	Color Color `json:"color"`
	Value Value `json:"value"`
}

// ── State ─────────────────────────────────────────────────────────────────────

const (
	StatusInProgress = "in_progress"
	StatusFinished   = "finished"
)

// Direction: 1 = clockwise, -1 = counter-clockwise.
type State struct {
	DrawPile       []Card            `json:"draw_pile"`
	DiscardPile    []Card            `json:"discard_pile"`
	Hands          map[int64][]Card  `json:"hands"`
	PlayerOrder    []int64           `json:"player_order"`
	CurrentTurnIdx int               `json:"current_turn_idx"`
	Direction      int               `json:"direction"` // 1 or -1
	PendingDraw    int               `json:"pending_draw"`
	Status         string            `json:"status"`
}

func (s *State) currentPlayer() int64 {
	return s.PlayerOrder[s.CurrentTurnIdx]
}

func (s *State) topDiscard() Card {
	return s.DiscardPile[len(s.DiscardPile)-1]
}

func (s *State) advanceTurn(skip int) {
	n := len(s.PlayerOrder)
	s.CurrentTurnIdx = ((s.CurrentTurnIdx + s.Direction*skip) % n + n) % n
}

// ── Event types ───────────────────────────────────────────────────────────────

const (
	EventCardPlayed        = "CARD_PLAYED"
	EventCardDrawn         = "CARD_DRAWN"
	EventTurnSkipped       = "TURN_SKIPPED"
	EventDirectionReversed = "DIRECTION_REVERSED"
	EventPlayerWon         = "PLAYER_WON"
	EventGameOver          = "GAME_OVER"
)

// ── Deck builder ──────────────────────────────────────────────────────────────

func buildDeck() []Card {
	colors := []Color{ColorRed, ColorGreen, ColorBlue, ColorYellow}
	values := []Value{
		ValueOne, ValueTwo, ValueThree, ValueFour, ValueFive,
		ValueSix, ValueSeven, ValueEight, ValueNine,
		ValueSkip, ValueReverse, ValueDrawTwo,
	}

	deck := make([]Card, 0, 108)

	for _, c := range colors {
		deck = append(deck, Card{Color: c, Value: ValueZero}) // one zero each
		for _, v := range values {
			deck = append(deck, Card{Color: c, Value: v}) // two of each
			deck = append(deck, Card{Color: c, Value: v})
		}
	}

	// 4 Wilds + 4 Wild Draw Fours
	for i := 0; i < 4; i++ {
		deck = append(deck, Card{Color: ColorWild, Value: ValueWild})
		deck = append(deck, Card{Color: ColorWild, Value: ValueWildDraw4})
	}

	return deck
}

func shuffle(deck []Card, rng *rand.Rand) []Card {
	out := make([]Card, len(deck))
	copy(out, deck)
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// ── Engine ────────────────────────────────────────────────────────────────────

// Engine implements games.GameEngine for Uno.
type Engine struct {
	rng *rand.Rand
}

// New creates an Engine. Pass nil to use a default random source.
func New(rng *rand.Rand) *Engine {
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}
	return &Engine{rng: rng}
}

func (e *Engine) Init(players []games.Player, opts map[string]any) (json.RawMessage, error) {
	if len(players) < 2 {
		return nil, fmt.Errorf("uno requires at least 2 players")
	}

	handSize := 7
	if v, ok := opts["hand_size"]; ok {
		if f, ok := v.(float64); ok {
			handSize = int(f)
		}
	}

	deck := shuffle(buildDeck(), e.rng)

	order := make([]int64, len(players))
	for i, p := range players {
		order[i] = p.TelegramUserID
	}

	hands := make(map[int64][]Card, len(players))
	for _, id := range order {
		hands[id] = make([]Card, handSize)
		for j := 0; j < handSize; j++ {
			hands[id][j], deck = deck[0], deck[1:]
		}
	}

	// Flip first non-wild card to start discard pile.
	var first Card
	for {
		first, deck = deck[0], deck[1:]
		if first.Color != ColorWild {
			break
		}
		deck = append(deck, first)
	}

	s := State{
		DrawPile:       deck,
		DiscardPile:    []Card{first},
		Hands:          hands,
		PlayerOrder:    order,
		CurrentTurnIdx: 0,
		Direction:      1,
		PendingDraw:    0,
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

	if s.currentPlayer() != move.PlayerID {
		return fmt.Errorf("not your turn")
	}

	action, _ := move.Payload["action"].(string)

	switch action {
	case "play_card":
		return validatePlayCard(s, move)
	case "draw":
		return nil
	default:
		return fmt.Errorf("unknown action: %q", action)
	}
}

func validatePlayCard(s State, move games.Move) error {
	idx, err := cardIndex(move)
	if err != nil {
		return err
	}

	hand := s.Hands[move.PlayerID]
	if idx < 0 || idx >= len(hand) {
		return fmt.Errorf("card index out of range")
	}

	card := hand[idx]
	top := s.topDiscard()

	// Wild cards are always playable.
	if card.Color == ColorWild {
		if card.Value == ValueWildDraw4 {
			// Validate: player must not have a card matching top discard color.
			if top.Color != ColorWild {
				for _, c := range hand {
					if c.Color == top.Color {
						return fmt.Errorf("wild draw four invalid: you have a card matching the current color")
					}
				}
			}
		}
		if _, ok := move.Payload["chosen_color"]; !ok {
			return fmt.Errorf("chosen_color required for wild cards")
		}
		return nil
	}

	if card.Color == top.Color || card.Value == top.Value {
		return nil
	}

	return fmt.Errorf("card does not match top discard (color or value)")
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
	case "play_card":
		raw, events, err = applyPlayCard(s, move)
	case "draw":
		raw, events, err = applyDraw(s, move, e.rng)
	}

	return raw, events, err
}

func applyPlayCard(s State, move games.Move) (json.RawMessage, []games.Event, error) {
	idx, _ := cardIndex(move)
	hand := s.Hands[move.PlayerID]
	card := hand[idx]

	// Remove card from hand.
	newHand := make([]Card, 0, len(hand)-1)
	newHand = append(newHand, hand[:idx]...)
	newHand = append(newHand, hand[idx+1:]...)
	s.Hands[move.PlayerID] = newHand

	// Apply chosen color for wilds.
	played := card
	if card.Color == ColorWild {
		cc, _ := move.Payload["chosen_color"].(string)
		played.Color = Color(cc)
	}
	s.DiscardPile = append(s.DiscardPile, played)

	events := []games.Event{{
		Type:    EventCardPlayed,
		Payload: map[string]any{"player_id": move.PlayerID, "card": card},
	}}

	if len(newHand) == 0 {
		s.Status = StatusFinished
		events = append(events,
			games.Event{Type: EventPlayerWon, Payload: map[string]any{"player_id": move.PlayerID}},
			games.Event{Type: EventGameOver, Payload: map[string]any{"winner_id": move.PlayerID}},
		)
		raw, err := marshalState(s)
		return raw, events, err
	}

	switch card.Value {
	case ValueSkip:
		s.advanceTurn(1) // skip next player
		events = append(events, games.Event{Type: EventTurnSkipped, Payload: map[string]any{"skipped_player": s.currentPlayer()}})
		s.advanceTurn(1)
	case ValueReverse:
		s.Direction *= -1
		events = append(events, games.Event{Type: EventDirectionReversed, Payload: map[string]any{}})
		s.advanceTurn(1)
	case ValueDrawTwo:
		s.advanceTurn(1)
		next := s.currentPlayer()
		drawn := drawCards(&s, 2)
		events = append(events, games.Event{Type: EventCardDrawn, Payload: map[string]any{"player_id": next, "count": 2, "cards": drawn}})
		events = append(events, games.Event{Type: EventTurnSkipped, Payload: map[string]any{"skipped_player": next}})
		s.advanceTurn(1)
	case ValueWildDraw4:
		s.advanceTurn(1)
		next := s.currentPlayer()
		drawn := drawCards(&s, 4)
		events = append(events, games.Event{Type: EventCardDrawn, Payload: map[string]any{"player_id": next, "count": 4, "cards": drawn}})
		events = append(events, games.Event{Type: EventTurnSkipped, Payload: map[string]any{"skipped_player": next}})
		s.advanceTurn(1)
	default:
		s.advanceTurn(1)
	}

	raw, err := marshalState(s)
	return raw, events, err
}

func applyDraw(s State, move games.Move, rng *rand.Rand) (json.RawMessage, []games.Event, error) {
	drawn := drawCards(&s, 1)
	events := []games.Event{{
		Type:    EventCardDrawn,
		Payload: map[string]any{"player_id": move.PlayerID, "count": 1, "cards": drawn},
	}}
	s.advanceTurn(1)
	raw, err := marshalState(s)
	return raw, events, err
}

func drawCards(s *State, n int) []Card {
	drawn := make([]Card, 0, n)
	for i := 0; i < n; i++ {
		if len(s.DrawPile) == 0 {
			reshuffleDiscard(s)
		}
		if len(s.DrawPile) == 0 {
			break
		}
		card := s.DrawPile[0]
		s.DrawPile = s.DrawPile[1:]
		s.Hands[s.currentPlayer()] = append(s.Hands[s.currentPlayer()], card)
		drawn = append(drawn, card)
	}
	return drawn
}

func reshuffleDiscard(s *State) {
	if len(s.DiscardPile) <= 1 {
		return
	}
	top := s.DiscardPile[len(s.DiscardPile)-1]
	discard := s.DiscardPile[:len(s.DiscardPile)-1]
	rng := rand.New(rand.NewSource(rand.Int63()))
	rng.Shuffle(len(discard), func(i, j int) { discard[i], discard[j] = discard[j], discard[i] })
	s.DrawPile = discard
	s.DiscardPile = []Card{top}
}

func (e *Engine) Evaluate(raw json.RawMessage) (bool, games.Result, error) {
	s, err := parseState(raw)
	if err != nil {
		return false, games.Result{}, err
	}

	if s.Status != StatusFinished {
		return false, games.Result{}, nil
	}

	// Find winner (empty hand).
	var winners []games.Player
	scores := make(map[int64]int)
	for _, id := range s.PlayerOrder {
		hand := s.Hands[id]
		score := 0
		for _, c := range hand {
			score += cardPoints(c)
		}
		scores[id] = score
		if len(hand) == 0 {
			winners = append(winners, games.Player{TelegramUserID: id})
		}
	}

	return true, games.Result{Winners: winners, Scores: scores}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func cardIndex(move games.Move) (int, error) {
	v, ok := move.Payload["card_index"]
	if !ok {
		return 0, fmt.Errorf("card_index required")
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("card_index must be a number")
	}
}

func cardPoints(c Card) int {
	switch c.Value {
	case ValueZero:
		return 0
	case ValueOne:
		return 1
	case ValueTwo:
		return 2
	case ValueThree:
		return 3
	case ValueFour:
		return 4
	case ValueFive:
		return 5
	case ValueSix:
		return 6
	case ValueSeven:
		return 7
	case ValueEight:
		return 8
	case ValueNine:
		return 9
	case ValueSkip, ValueReverse, ValueDrawTwo:
		return 20
	case ValueWild, ValueWildDraw4:
		return 50
	}
	return 0
}

func parseState(raw json.RawMessage) (State, error) {
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("invalid uno state: %w", err)
	}
	return s, nil
}

func marshalState(s State) (json.RawMessage, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal uno state: %w", err)
	}
	return b, nil
}
