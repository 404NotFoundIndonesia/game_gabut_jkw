package games

import (
	"fmt"

	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// Registry maps game slugs to their engine implementations.
type Registry struct {
	engines map[gamedomain.GameSlug]GameEngine
}

// NewRegistry creates an empty registry. Use Register to add engines.
func NewRegistry() *Registry {
	return &Registry{engines: make(map[gamedomain.GameSlug]GameEngine)}
}

// Register adds an engine for the given slug.
func (r *Registry) Register(slug gamedomain.GameSlug, engine GameEngine) {
	r.engines[slug] = engine
}

// Get returns the engine for the given slug.
// Returns INTERNAL_ERROR if the slug is not registered.
func (r *Registry) Get(slug gamedomain.GameSlug) (GameEngine, error) {
	e, ok := r.engines[slug]
	if !ok {
		return nil, apperrors.Internal(fmt.Sprintf("no engine registered for game slug %q", slug))
	}
	return e, nil
}
