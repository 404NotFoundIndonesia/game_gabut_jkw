package kbbi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	_ "embed"
)

//go:embed kbbi.txt
var kbbiWords string

// Validator checks if a word exists in KBBI.
type Validator interface {
	IsValid(word string) bool
}

// ── Offline validator ─────────────────────────────────────────────────────────

// OfflineValidator uses an embedded word list loaded at startup.
type OfflineValidator struct {
	words map[string]struct{}
}

// NewOfflineValidator builds the word set from the embedded kbbi.txt file.
func NewOfflineValidator() *OfflineValidator {
	set := make(map[string]struct{})
	sc := bufio.NewScanner(strings.NewReader(kbbiWords))
	for sc.Scan() {
		w := strings.TrimSpace(strings.ToLower(sc.Text()))
		if w != "" {
			set[w] = struct{}{}
		}
	}
	return &OfflineValidator{words: set}
}

func (v *OfflineValidator) IsValid(word string) bool {
	_, ok := v.words[strings.ToLower(strings.TrimSpace(word))]
	return ok
}

// ── API validator ─────────────────────────────────────────────────────────────

// APIValidator calls an external KBBI API to validate words.
type APIValidator struct {
	baseURL string
	client  *http.Client
}

// NewAPIValidator creates a validator backed by an external API.
func NewAPIValidator(baseURL string) *APIValidator {
	return &APIValidator{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

type apiResponse struct {
	Valid bool `json:"valid"`
}

func (v *APIValidator) IsValid(word string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/validate?word=%s", v.baseURL, word)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	return result.Valid
}
