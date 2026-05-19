package errors_test

import (
	"net/http"
	"testing"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

func TestConstructors_HTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		err        *apperrors.AppError
		wantCode   apperrors.Code
		wantStatus int
	}{
		{"NotFound", apperrors.NotFound("not found"), apperrors.CodeNotFound, http.StatusNotFound},
		{"Validation", apperrors.Validation("bad input"), apperrors.CodeValidation, http.StatusBadRequest},
		{"Unauthorized", apperrors.Unauthorized("no auth"), apperrors.CodeUnauthorized, http.StatusUnauthorized},
		{"Forbidden", apperrors.Forbidden("no access"), apperrors.CodeForbidden, http.StatusForbidden},
		{"Conflict", apperrors.Conflict("dup"), apperrors.CodeConflict, http.StatusConflict},
		{"Unprocessable", apperrors.Unprocessable("bad move"), apperrors.CodeUnprocessable, http.StatusUnprocessableEntity},
		{"Internal", apperrors.Internal("oops"), apperrors.CodeInternal, http.StatusInternalServerError},
		{"RateLimited", apperrors.RateLimited("slow down"), apperrors.CodeRateLimited, http.StatusTooManyRequests},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.wantCode {
				t.Errorf("Code: got %s, want %s", tc.err.Code, tc.wantCode)
			}
			if tc.err.HTTPStatus != tc.wantStatus {
				t.Errorf("HTTPStatus: got %d, want %d", tc.err.HTTPStatus, tc.wantStatus)
			}
		})
	}
}

func TestValidation_FieldDetails(t *testing.T) {
	err := apperrors.Validation("invalid", apperrors.FieldError{Field: "name", Message: "required"})
	if len(err.Details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(err.Details))
	}
	if err.Details[0].Field != "name" {
		t.Errorf("field: got %s, want name", err.Details[0].Field)
	}
}

func TestWithCause_Unwrap(t *testing.T) {
	base := apperrors.Internal("db error")
	wrapped := base.WithCause(errSentinel)
	if wrapped.Unwrap() != errSentinel {
		t.Errorf("Unwrap did not return original cause")
	}
	if wrapped.Error() == base.Error() {
		t.Errorf("Error() should include cause string")
	}
}

func TestWithDetails_DoesNotMutateOriginal(t *testing.T) {
	orig := apperrors.Validation("bad")
	_ = orig.WithDetails([]apperrors.FieldError{{Field: "x", Message: "y"}})
	if len(orig.Details) != 0 {
		t.Errorf("WithDetails mutated original error")
	}
}

func TestAsAppError_WrapsUnknownError(t *testing.T) {
	ae := apperrors.AsAppError(errSentinel)
	if ae.Code != apperrors.CodeInternal {
		t.Errorf("expected INTERNAL_ERROR, got %s", ae.Code)
	}
}

func TestAsAppError_ExtractsAppError(t *testing.T) {
	orig := apperrors.NotFound("thing")
	ae := apperrors.AsAppError(orig)
	if ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND, got %s", ae.Code)
	}
}

// sentinel error for tests
var errSentinel = &sentinelError{}

type sentinelError struct{}

func (e *sentinelError) Error() string { return "sentinel" }
