package domain

import "github.com/google/uuid"

// GameSlug is the stable identifier for a game type.
type GameSlug string

const (
	SlugUno          GameSlug = "uno"
	SlugSambungKata  GameSlug = "sambung_kata"
	SlugTruthOrDate  GameSlug = "truth_or_date"
)

// Game represents an available game in the catalog.
// Game records are seeded via migration and are read-only at runtime.
type Game struct {
	ID          uuid.UUID `json:"id"`
	Slug        GameSlug  `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	MinPlayers  int       `json:"min_players"`
	MaxPlayers  int       `json:"max_players"`
}
