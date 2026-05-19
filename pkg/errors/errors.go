package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a machine-readable error identifier returned in the API envelope.
type Code string

const (
	CodeNotFound        Code = "NOT_FOUND"
	CodeValidation      Code = "VALIDATION_ERROR"
	CodeUnauthorized    Code = "UNAUTHORIZED"
	CodeForbidden       Code = "FORBIDDEN"
	CodeConflict        Code = "CONFLICT"
	CodeUnprocessable   Code = "UNPROCESSABLE"
	CodeInternal        Code = "INTERNAL_ERROR"
	CodeRateLimited     Code = "RATE_LIMITED"
)

// FieldError describes a single field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// AppError is the canonical error type for this application.
// All handler and service errors should be or wrap an AppError.
type AppError struct {
	Code       Code         `json:"code"`
	Message    string       `json:"message"`
	Details    []FieldError `json:"details,omitempty"`
	HTTPStatus int          `json:"-"`
	cause      error
}

func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.cause)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.cause
}

// WithCause attaches an underlying cause without exposing it in the API response.
func (e *AppError) WithCause(cause error) *AppError {
	cp := *e
	cp.cause = cause
	return &cp
}

// WithDetails attaches field-level validation details.
func (e *AppError) WithDetails(details []FieldError) *AppError {
	cp := *e
	cp.Details = details
	return &cp
}

// Is makes AppError compatible with errors.Is by matching on Code.
func (e *AppError) Is(target error) bool {
	var t *AppError
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// Constructors

func NotFound(msg string) *AppError {
	return &AppError{Code: CodeNotFound, Message: msg, HTTPStatus: http.StatusNotFound}
}

func Validation(msg string, details ...FieldError) *AppError {
	return &AppError{Code: CodeValidation, Message: msg, Details: details, HTTPStatus: http.StatusBadRequest}
}

func Unauthorized(msg string) *AppError {
	return &AppError{Code: CodeUnauthorized, Message: msg, HTTPStatus: http.StatusUnauthorized}
}

func Forbidden(msg string) *AppError {
	return &AppError{Code: CodeForbidden, Message: msg, HTTPStatus: http.StatusForbidden}
}

func Conflict(msg string) *AppError {
	return &AppError{Code: CodeConflict, Message: msg, HTTPStatus: http.StatusConflict}
}

func Unprocessable(msg string) *AppError {
	return &AppError{Code: CodeUnprocessable, Message: msg, HTTPStatus: http.StatusUnprocessableEntity}
}

func Internal(msg string) *AppError {
	return &AppError{Code: CodeInternal, Message: msg, HTTPStatus: http.StatusInternalServerError}
}

func RateLimited(msg string) *AppError {
	return &AppError{Code: CodeRateLimited, Message: msg, HTTPStatus: http.StatusTooManyRequests}
}

// AsAppError extracts *AppError from any error, or wraps it as Internal.
func AsAppError(err error) *AppError {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae
	}
	return Internal("an unexpected error occurred").WithCause(err)
}
