package validator_test

import (
	"testing"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/validator"
)

type validStruct struct {
	Name  string `json:"name"  validate:"required,min=1,max=100"`
	Email string `json:"email" validate:"required,email"`
}

func TestStruct_Valid(t *testing.T) {
	v := validStruct{Name: "Iqbal", Email: "iqbal@example.com"}
	if err := validator.Struct(v); err != nil {
		t.Errorf("expected nil error for valid struct, got %v", err)
	}
}

func TestStruct_MissingRequired(t *testing.T) {
	v := validStruct{} // both fields empty
	err := validator.Struct(v)
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
	if err.Code != apperrors.CodeValidation {
		t.Errorf("expected VALIDATION_ERROR code, got %s", err.Code)
	}
	if len(err.Details) == 0 {
		t.Error("expected field-level details, got none")
	}
}

func TestStruct_FieldNameFromJSON(t *testing.T) {
	v := validStruct{Name: "ok"}
	err := validator.Struct(v)
	if err == nil {
		t.Fatal("expected error for missing email")
	}
	found := false
	for _, d := range err.Details {
		if d.Field == "email" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected detail with field 'email', got %+v", err.Details)
	}
}

func TestStruct_MinValidation(t *testing.T) {
	type minStruct struct {
		Tag string `json:"tag" validate:"required,min=3"`
	}
	v := minStruct{Tag: "ab"} // too short
	err := validator.Struct(v)
	if err == nil {
		t.Fatal("expected error for min violation")
	}
	if len(err.Details) == 0 || err.Details[0].Field != "tag" {
		t.Errorf("unexpected details: %+v", err.Details)
	}
}
