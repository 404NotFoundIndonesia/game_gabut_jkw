package response

import (
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// Envelope is the canonical JSON wrapper for all API responses.
//
//	{ "success": true/false, "data": ..., "error": ..., "meta": ... }
type Envelope struct {
	Success bool              `json:"success"`
	Data    any               `json:"data"`
	Error   *errorBody        `json:"error"`
	Meta    *pagination.Meta  `json:"meta,omitempty"`
}

type errorBody struct {
	Code    apperrors.Code       `json:"code"`
	Message string               `json:"message"`
	Details []apperrors.FieldError `json:"details,omitempty"`
}

// Success builds a 2xx envelope. Pass nil meta when not applicable.
func Success(data any, meta *pagination.Meta) Envelope {
	return Envelope{
		Success: true,
		Data:    data,
		Error:   nil,
		Meta:    meta,
	}
}

// Error builds an error envelope from an AppError.
func Error(err *apperrors.AppError) Envelope {
	return Envelope{
		Success: false,
		Data:    nil,
		Error: &errorBody{
			Code:    err.Code,
			Message: err.Message,
			Details: err.Details,
		},
	}
}
