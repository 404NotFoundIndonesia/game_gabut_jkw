package webhook

import (
	"fmt"
	"strings"

	"github.com/404NFIDv2/bot-game-management/internal/games/uno"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
)

// UnoStickerMap maps "color_value" (e.g. "red_5", "wild_wild_draw_four") to a
// Telegram sticker file_id. Leave empty to use emoji inline-keyboard buttons.
type UnoStickerMap map[string]string

func unoCardKey(c uno.Card) string {
	return string(c.Color) + "_" + string(c.Value)
}

func unoCardEmoji(c uno.Card) string {
	var col string
	switch c.Color {
	case uno.ColorRed:
		col = "🔴"
	case uno.ColorGreen:
		col = "🟢"
	case uno.ColorBlue:
		col = "🔵"
	case uno.ColorYellow:
		col = "🟡"
	default:
		col = "⬛"
	}
	var val string
	switch c.Value {
	case uno.ValueSkip:
		val = "Skip"
	case uno.ValueReverse:
		val = "Rev"
	case uno.ValueDrawTwo:
		val = "D+2"
	case uno.ValueWild:
		val = "Wild"
	case uno.ValueWildDraw4:
		val = "W+4"
	default:
		val = string(c.Value)
	}
	return col + val
}

func unoIsPlayable(card, top uno.Card) bool {
	if card.Color == uno.ColorWild {
		return true
	}
	return card.Color == top.Color || card.Value == top.Value
}

// unoHandText builds the summary text shown above the hand keyboard.
func unoHandText(hand []uno.Card, top uno.Card) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🎴 Top card: %s\n\n", unoCardEmoji(top))
	playable := 0
	for _, c := range hand {
		if unoIsPlayable(c, top) {
			playable++
		}
	}
	fmt.Fprintf(&sb, "🃏 Your hand (%d cards, %d playable):\n", len(hand), playable)
	for _, c := range hand {
		mark := "  "
		if unoIsPlayable(c, top) {
			mark = "▶ "
		}
		fmt.Fprintf(&sb, "%s%s\n", mark, unoCardEmoji(c))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// unoHandKeyboard builds the inline keyboard for a player's hand.
// Only playable cards are shown; wild cards get a callback that triggers
// color selection before the move is submitted.
func unoHandKeyboard(hand []uno.Card, top uno.Card) telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	var row []telegram.InlineKeyboardButton

	for i, card := range hand {
		if !unoIsPlayable(card, top) {
			continue
		}
		var cb string
		if card.Color == uno.ColorWild {
			cb = fmt.Sprintf("uwild:%d", i)
		} else {
			cb = fmt.Sprintf("uplay:%d", i)
		}
		row = append(row, telegram.InlineKeyboardButton{
			Text:         unoCardEmoji(card),
			CallbackData: cb,
		})
		if len(row) == 4 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: "🃏 Draw a card", CallbackData: "udraw"},
	})
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// unoColorKeyboard returns the color-selection keyboard for a wild card.
func unoColorKeyboard(cardIdx int) telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "🔴 Red", CallbackData: fmt.Sprintf("ucolor:%d:red", cardIdx)},
				{Text: "🔵 Blue", CallbackData: fmt.Sprintf("ucolor:%d:blue", cardIdx)},
			},
			{
				{Text: "🟡 Yellow", CallbackData: fmt.Sprintf("ucolor:%d:yellow", cardIdx)},
				{Text: "🟢 Green", CallbackData: fmt.Sprintf("ucolor:%d:green", cardIdx)},
			},
		},
	}
}

// unoViewHandKeyboard is the single button shown in the group on each turn.
var unoViewHandKeyboard = telegram.InlineKeyboardMarkup{
	InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "🃏 View my hand", CallbackData: "vhand"}},
	},
}

// unoTurnText builds the group message shown on each turn.
func unoTurnText(topCard uno.Card, playerName string, handSize int) string {
	return fmt.Sprintf(
		"🎮 *Uno* — Top card: %s\n\n👤 It's *%s*'s turn! (%d cards in hand)\nTap the button below to view your hand.",
		unoCardEmoji(topCard), playerName, handSize,
	)
}

// unoGameOverText builds the group message shown when the game ends.
func unoGameOverText(winnerName string) string {
	return fmt.Sprintf("🏆 *%s* wins! Game over.", winnerName)
}
