package webhook

import (
	"fmt"
	"html"

	"github.com/404NFIDv2/bot-game-management/internal/telegram"
)

// ── Sambung Kata UI ───────────────────────────────────────────────────────────

func skTurnText(playerMention, lastWord string) string {
	if lastWord == "" {
		return fmt.Sprintf("📝 Sambung Kata!\n\n%s's turn — type any Indonesian word.", playerMention)
	}
	lastLetter := skLastLetter(lastWord)
	return fmt.Sprintf(
		"📝 Sambung Kata!\n\nLast word: %s\nNext must start with: %s\n\n%s's turn — type your word in the chat!",
		html.EscapeString(lastWord), lastLetter, playerMention,
	)
}

func skGameOverText(winnerName string) string {
	return fmt.Sprintf("🏆 Sambung Kata over!\nWinner: %s 🎉", winnerName)
}

func skLastLetter(s string) string {
	var r rune
	for _, ch := range s {
		r = ch
	}
	return fmt.Sprintf("%c", r)
}

// ── Truth or Date UI ──────────────────────────────────────────────────────────

func tdTurnText(playerName string, round int) string {
	return fmt.Sprintf("🎯 Round %d — %s's turn!\n\nChoose your fate:", round, playerName)
}

func tdQuestionText(playerMention, choice, question string) string {
	emoji := "📅"
	if choice == "truth" {
		emoji = "💬"
	}
	return fmt.Sprintf(
		"%s %s chose <b>%s</b>!\n\nQuestion:\n%s\n\nType your answer in the chat.",
		emoji, playerMention, html.EscapeString(choice), html.EscapeString(question),
	)
}

func tdChoiceKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "💬 Truth", CallbackData: "tdchoice:truth"},
				{Text: "📅 Date", CallbackData: "tdchoice:date"},
			},
		},
	}
}

func tdSkipKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "⏭ Skip (host only)", CallbackData: "tdskip"}},
		},
	}
}
