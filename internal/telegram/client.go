package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is the Telegram Bot API interface used by this application.
type Client interface {
	// GetBotID calls getMe and returns the bot's numeric user ID.
	GetBotID(ctx context.Context, botToken string) (int64, error)

	// SetWebhook registers a webhook URL for the given bot.
	SetWebhook(ctx context.Context, botToken, webhookURL, secretToken string) error

	// DeleteWebhook removes the webhook registration for the given bot.
	DeleteWebhook(ctx context.Context, botToken string) error

	// GetWebhookInfo returns the current webhook configuration for the given bot.
	GetWebhookInfo(ctx context.Context, botToken string) (WebhookInfo, error)

	// SendMessage sends a plain text message to a Telegram chat.
	SendMessage(ctx context.Context, botToken string, chatID int64, text string) error

	// SendMessageWithKeyboard sends a message with an attached inline keyboard.
	SendMessageWithKeyboard(ctx context.Context, botToken string, chatID int64, text string, keyboard InlineKeyboardMarkup) error

	// SendMessageGetID sends a message with a keyboard and returns the sent message's ID.
	SendMessageGetID(ctx context.Context, botToken string, chatID int64, text string, keyboard InlineKeyboardMarkup) (int64, error)

	// SendSticker sends a sticker with an optional inline keyboard attached.
	SendSticker(ctx context.Context, botToken string, chatID int64, fileID string, keyboard *InlineKeyboardMarkup) error

	// AnswerCallbackQuery acknowledges a button tap (clears the loading spinner).
	AnswerCallbackQuery(ctx context.Context, botToken, callbackQueryID string) error

	// AnswerCallbackQueryAlert acknowledges a button tap with a visible alert popup.
	AnswerCallbackQueryAlert(ctx context.Context, botToken, callbackQueryID, text string) error

	// EditMessageText replaces the text (and optionally the keyboard) of an existing message.
	// Pass nil keyboard to remove the inline keyboard.
	EditMessageText(ctx context.Context, botToken string, chatID, messageID int64, text string, keyboard *InlineKeyboardMarkup) error
}

type httpClient struct {
	http *http.Client
}

// NewHTTPClient returns a Client backed by the real Telegram Bot API.
func NewHTTPClient() Client {
	return &httpClient{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewHTTPClientWithTransport returns a Client using a custom HTTP transport.
// Intended for testing — allows redirecting requests to a mock server.
func NewHTTPClientWithTransport(hc *http.Client) Client {
	return &httpClient{http: hc}
}

// ── response envelope ─────────────────────────────────────────────────────────

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

func (c *httpClient) callAPI(ctx context.Context, botToken, method string, payload any) (json.RawMessage, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", botToken, method)

	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			return nil, fmt.Errorf("telegram: encode payload: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("telegram: build request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram: %s request: %w", method, err)
	}
	defer resp.Body.Close()

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram: decode %s response: %w", method, err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram: %s failed: %s", method, result.Description)
	}
	return result.Result, nil
}

// ── GetBotID ──────────────────────────────────────────────────────────────────

type getMeResult struct {
	ID int64 `json:"id"`
}

func (c *httpClient) GetBotID(ctx context.Context, botToken string) (int64, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("telegram: build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("telegram: getMe request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool        `json:"ok"`
		Result getMeResult `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("telegram: decode response: %w", err)
	}
	if !result.OK {
		return 0, fmt.Errorf("telegram: getMe failed: %s", result.Description)
	}
	return result.Result.ID, nil
}

// ── SetWebhook ────────────────────────────────────────────────────────────────

func (c *httpClient) SetWebhook(ctx context.Context, botToken, webhookURL, secretToken string) error {
	payload := map[string]string{
		"url":          webhookURL,
		"secret_token": secretToken,
	}
	_, err := c.callAPI(ctx, botToken, "setWebhook", payload)
	return err
}

// ── DeleteWebhook ─────────────────────────────────────────────────────────────

func (c *httpClient) DeleteWebhook(ctx context.Context, botToken string) error {
	_, err := c.callAPI(ctx, botToken, "deleteWebhook", map[string]bool{"drop_pending_updates": false})
	return err
}

// ── GetWebhookInfo ────────────────────────────────────────────────────────────

func (c *httpClient) GetWebhookInfo(ctx context.Context, botToken string) (WebhookInfo, error) {
	raw, err := c.callAPI(ctx, botToken, "getWebhookInfo", nil)
	if err != nil {
		return WebhookInfo{}, err
	}
	var wi WebhookInfo
	if err := json.Unmarshal(raw, &wi); err != nil {
		return WebhookInfo{}, fmt.Errorf("telegram: decode getWebhookInfo: %w", err)
	}
	return wi, nil
}

// ── SendMessage ───────────────────────────────────────────────────────────────

func (c *httpClient) SendMessage(ctx context.Context, botToken string, chatID int64, text string) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	_, err := c.callAPI(ctx, botToken, "sendMessage", payload)
	return err
}

// ── SendMessageWithKeyboard ───────────────────────────────────────────────────

func (c *httpClient) SendMessageWithKeyboard(ctx context.Context, botToken string, chatID int64, text string, keyboard InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":      chatID,
		"text":         text,
		"reply_markup": keyboard,
	}
	_, err := c.callAPI(ctx, botToken, "sendMessage", payload)
	return err
}

// ── SendMessageGetID ──────────────────────────────────────────────────────────

func (c *httpClient) SendMessageGetID(ctx context.Context, botToken string, chatID int64, text string, keyboard InlineKeyboardMarkup) (int64, error) {
	payload := map[string]any{
		"chat_id":      chatID,
		"text":         text,
		"reply_markup": keyboard,
	}
	raw, err := c.callAPI(ctx, botToken, "sendMessage", payload)
	if err != nil {
		return 0, err
	}
	var msg struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return 0, fmt.Errorf("telegram: decode message_id: %w", err)
	}
	return msg.MessageID, nil
}

// ── SendSticker ───────────────────────────────────────────────────────────────

func (c *httpClient) SendSticker(ctx context.Context, botToken string, chatID int64, fileID string, keyboard *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id": chatID,
		"sticker": fileID,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	_, err := c.callAPI(ctx, botToken, "sendSticker", payload)
	return err
}

// ── AnswerCallbackQuery ───────────────────────────────────────────────────────

func (c *httpClient) AnswerCallbackQuery(ctx context.Context, botToken, callbackQueryID string) error {
	payload := map[string]string{"callback_query_id": callbackQueryID}
	_, err := c.callAPI(ctx, botToken, "answerCallbackQuery", payload)
	return err
}

// ── AnswerCallbackQueryAlert ──────────────────────────────────────────────────

func (c *httpClient) AnswerCallbackQueryAlert(ctx context.Context, botToken, callbackQueryID, text string) error {
	payload := map[string]any{
		"callback_query_id": callbackQueryID,
		"text":              text,
		"show_alert":        true,
	}
	_, err := c.callAPI(ctx, botToken, "answerCallbackQuery", payload)
	return err
}

// ── EditMessageText ───────────────────────────────────────────────────────────

func (c *httpClient) EditMessageText(ctx context.Context, botToken string, chatID, messageID int64, text string, keyboard *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	_, err := c.callAPI(ctx, botToken, "editMessageText", payload)
	return err
}
