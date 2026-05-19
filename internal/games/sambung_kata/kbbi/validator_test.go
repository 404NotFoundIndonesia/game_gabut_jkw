package kbbi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/404NFIDv2/bot-game-management/internal/games/sambung_kata/kbbi"
)

func TestOfflineValidator_KnownWord(t *testing.T) {
	v := kbbi.NewOfflineValidator()
	if !v.IsValid("apel") {
		t.Error("expected 'apel' to be valid")
	}
}

func TestOfflineValidator_UnknownWord(t *testing.T) {
	v := kbbi.NewOfflineValidator()
	if v.IsValid("xyzabc") {
		t.Error("expected 'xyzabc' to be invalid")
	}
}

func TestOfflineValidator_CaseInsensitive(t *testing.T) {
	v := kbbi.NewOfflineValidator()
	if !v.IsValid("APEL") {
		t.Error("expected 'APEL' (uppercase) to be valid")
	}
}

func TestAPIValidator_ValidWord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"valid": true})
	}))
	defer srv.Close()

	v := kbbi.NewAPIValidator(srv.URL)
	if !v.IsValid("apel") {
		t.Error("expected API to return valid")
	}
}

func TestAPIValidator_InvalidWord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"valid": false})
	}))
	defer srv.Close()

	v := kbbi.NewAPIValidator(srv.URL)
	if v.IsValid("xyzabc") {
		t.Error("expected API to return invalid")
	}
}

func TestAPIValidator_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	v := kbbi.NewAPIValidator(srv.URL)
	if v.IsValid("apel") {
		t.Error("expected invalid on server error")
	}
}
