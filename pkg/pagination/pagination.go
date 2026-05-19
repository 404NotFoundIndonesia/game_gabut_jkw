package pagination

import (
	"strconv"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

const (
	defaultLimit = 10
	maxLimit     = 100
)

// Params holds validated pagination inputs.
type Params struct {
	Limit  int
	Offset int
}

// Meta is the pagination metadata returned in API responses.
type Meta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ParseFromQuery parses and validates limit/offset query string values.
// Empty strings produce the defaults (limit=10, offset=0).
func ParseFromQuery(limitStr, offsetStr string) (Params, *apperrors.AppError) {
	limit := defaultLimit
	offset := 0

	if limitStr != "" {
		v, err := strconv.Atoi(limitStr)
		if err != nil || v < 1 {
			return Params{}, apperrors.Validation("limit must be a positive integer")
		}
		if v > maxLimit {
			v = maxLimit
		}
		limit = v
	}

	if offsetStr != "" {
		v, err := strconv.Atoi(offsetStr)
		if err != nil || v < 0 {
			return Params{}, apperrors.Validation("offset must be a non-negative integer")
		}
		offset = v
	}

	return Params{Limit: limit, Offset: offset}, nil
}

// NewMeta constructs a Meta from total record count and the applied Params.
func NewMeta(total int, p Params) Meta {
	return Meta{Total: total, Limit: p.Limit, Offset: p.Offset}
}
