package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is the minimal Telegram Bot API interface used by this application.
type Client interface {
	// GetBotID calls the Telegram getMe API and returns the bot's numeric user ID.
	GetBotID(ctx context.Context, botToken string) (int64, error)
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

type getMeResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		ID int64 `json:"id"`
	} `json:"result"`
	Description string `json:"description"`
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

	var result getMeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("telegram: decode response: %w", err)
	}
	if !result.OK {
		return 0, fmt.Errorf("telegram: getMe failed: %s", result.Description)
	}
	return result.Result.ID, nil
}
