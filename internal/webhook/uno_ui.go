package webhook

import (
	"fmt"

	"github.com/404NFIDv2/bot-game-management/internal/games/uno"
	"github.com/404NFIDv2/bot-game-management/internal/telegram"
)

// UnoStickerMap maps "color_value" (e.g. "red_5", "wild_wild_draw_four") to a
// Telegram sticker file_id used for cached_sticker inline results.
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

// unoPlayButton is the group-message button that opens inline card selection.
// Only the current player taps it; the inline results are private to them.
func unoPlayButton() telegram.InlineKeyboardMarkup {
	empty := ""
	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "🃏 Play a card", SwitchInlineQueryCurrentChat: &empty}},
		},
	}
}

// unoInlineResults builds the list of inline results shown to the current player.
// Non-wild playable cards → one result each.
// Wild cards → four results (one per color) for single-tap color selection.
// A "draw" result is always appended last.
// If stickerMap is non-empty and has a file_id for the card, "sticker" type is used.
func unoInlineResults(hand []uno.Card, top uno.Card, stickerMap UnoStickerMap) []telegram.InlineQueryResult {
	colorEmoji := map[string]string{"red": "🔴", "blue": "🔵", "yellow": "🟡", "green": "🟢"}
	colors := []string{"red", "blue", "yellow", "green"}

	var results []telegram.InlineQueryResult

	for i, card := range hand {
		if !unoIsPlayable(card, top) {
			continue
		}
		if card.Color == uno.ColorWild {
			for _, color := range colors {
				results = append(results, telegram.InlineQueryResult{
					Type:        "article",
					ID:          fmt.Sprintf("wild:%d:%s", i, color),
					Title:       fmt.Sprintf("%s %s", unoCardEmoji(card), colorEmoji[color]),
					Description: fmt.Sprintf("Play Wild → %s", color),
					InputMessageContent: &telegram.InputMessageContent{
						MessageText: fmt.Sprintf("🎴 Wild → %s %s", colorEmoji[color], color),
					},
				})
			}
			continue
		}

		fileID := stickerMap[unoCardKey(card)]
		if len(fileID) > 20 {
			results = append(results, telegram.InlineQueryResult{
				Type:          "sticker",
				ID:            fmt.Sprintf("play:%d", i),
				StickerFileID: fileID,
			})
		} else {
			results = append(results, telegram.InlineQueryResult{
				Type:        "article",
				ID:          fmt.Sprintf("play:%d", i),
				Title:       unoCardEmoji(card),
				Description: "Play this card",
				InputMessageContent: &telegram.InputMessageContent{
					MessageText: fmt.Sprintf("🎴 %s", unoCardEmoji(card)),
				},
			})
		}
	}

	results = append(results, telegram.InlineQueryResult{
		Type:        "article",
		ID:          "draw",
		Title:       "🃏 Draw a card",
		Description: "Draw from the deck",
		InputMessageContent: &telegram.InputMessageContent{
			MessageText: "🃏 Drew a card.",
		},
	})

	return results
}

// unoTurnText builds the group message shown on each turn.
func unoTurnText(topCard uno.Card, playerName string, handSize int) string {
	return fmt.Sprintf(
		"🎮 *Uno* — Top card: %s\n\n👤 It's *%s*'s turn! (%d cards)\nTap below to pick a card.",
		unoCardEmoji(topCard), playerName, handSize,
	)
}

// unoGameOverText builds the group message shown when the game ends.
func unoGameOverText(winnerName string) string {
	return fmt.Sprintf("🏆 *%s* wins! Game over.", winnerName)
}
