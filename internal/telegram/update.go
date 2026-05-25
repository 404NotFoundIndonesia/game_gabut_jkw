package telegram

// Update represents a single incoming update from the Telegram Bot API.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message is a Telegram message object.
type Message struct {
	MessageID int64   `json:"message_id"`
	From      *User   `json:"from"`
	Chat      *Chat   `json:"chat"`
	Text      string  `json:"text"`
	Date      int64   `json:"date"`
}

// User is a Telegram user or bot.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// Chat is a Telegram chat (private, group, supergroup, or channel).
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// WebhookInfo describes the current webhook configuration for a bot.
type WebhookInfo struct {
	URL                  string `json:"url"`
	HasCustomCertificate bool   `json:"has_custom_certificate"`
	PendingUpdateCount   int    `json:"pending_update_count"`
}
