package response_test

import (
	"encoding/json"
	"testing"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

func TestSuccess_Shape(t *testing.T) {
	env := response.Success(map[string]string{"id": "123"}, nil)
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed["success"] != true {
		t.Errorf("success: got %v", parsed["success"])
	}
	if parsed["error"] != nil {
		t.Errorf("error field must be null on success, got %v", parsed["error"])
	}
	if parsed["data"] == nil {
		t.Error("data field must be present")
	}
}

func TestSuccess_WithMeta(t *testing.T) {
	meta := &pagination.Meta{Total: 100, Limit: 10, Offset: 0}
	env := response.Success([]string{}, meta)
	b, _ := json.Marshal(env)
	var parsed map[string]any
	_ = json.Unmarshal(b, &parsed)
	if parsed["meta"] == nil {
		t.Error("meta field must be present when provided")
	}
}

func TestSuccess_NilMeta_Omitted(t *testing.T) {
	env := response.Success("payload", nil)
	b, _ := json.Marshal(env)
	var parsed map[string]any
	_ = json.Unmarshal(b, &parsed)
	if _, exists := parsed["meta"]; exists {
		t.Error("meta field must be omitted when nil")
	}
}

func TestError_Shape(t *testing.T) {
	appErr := apperrors.NotFound("thing not found")
	env := response.Error(appErr)
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed["success"] != false {
		t.Errorf("success: got %v, want false", parsed["success"])
	}
	if parsed["data"] != nil {
		t.Errorf("data must be null on error, got %v", parsed["data"])
	}
	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		t.Fatalf("error field must be an object, got %T", parsed["error"])
	}
	if errObj["code"] != string(apperrors.CodeNotFound) {
		t.Errorf("code: got %v, want %s", errObj["code"], apperrors.CodeNotFound)
	}
	if errObj["message"] != "thing not found" {
		t.Errorf("message: got %v", errObj["message"])
	}
}

func TestError_WithDetails(t *testing.T) {
	appErr := apperrors.Validation("bad input", apperrors.FieldError{Field: "name", Message: "required"})
	env := response.Error(appErr)
	b, _ := json.Marshal(env)
	var parsed map[string]any
	_ = json.Unmarshal(b, &parsed)
	errObj := parsed["error"].(map[string]any)
	details, ok := errObj["details"].([]any)
	if !ok || len(details) == 0 {
		t.Error("details field must be present and non-empty")
	}
}
