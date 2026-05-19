package infrastructure

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/404NFIDv2/bot-game-management/internal/game/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/google/uuid"
)

// PostgresGameRepository implements domain.GameRepository.
type PostgresGameRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresGameRepository(pool *pgxpool.Pool) *PostgresGameRepository {
	return &PostgresGameRepository{pool: pool}
}

func (r *PostgresGameRepository) FindAll(ctx context.Context) ([]*domain.Game, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, slug, name, description, min_players, max_players
		FROM games ORDER BY name
	`)
	if err != nil {
		return nil, apperrors.Internal("failed to list games").WithCause(err)
	}
	defer rows.Close()

	var games []*domain.Game
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, apperrors.Internal("failed to scan game").WithCause(err)
		}
		games = append(games, g)
	}
	return games, nil
}

func (r *PostgresGameRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Game, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, slug, name, description, min_players, max_players
		FROM games WHERE id = $1
	`, id)
	g, err := scanGameRow(row)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (r *PostgresGameRepository) FindBySlug(ctx context.Context, slug domain.GameSlug) (*domain.Game, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, slug, name, description, min_players, max_players
		FROM games WHERE slug = $1
	`, string(slug))
	g, err := scanGameRow(row)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func scanGame(rows pgx.Rows) (*domain.Game, error) {
	var g domain.Game
	var slug string
	if err := rows.Scan(&g.ID, &slug, &g.Name, &g.Description, &g.MinPlayers, &g.MaxPlayers); err != nil {
		return nil, err
	}
	g.Slug = domain.GameSlug(slug)
	return &g, nil
}

func scanGameRow(row pgx.Row) (*domain.Game, error) {
	var g domain.Game
	var slug string
	err := row.Scan(&g.ID, &slug, &g.Name, &g.Description, &g.MinPlayers, &g.MaxPlayers)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.NotFound("game not found")
		}
		return nil, apperrors.Internal("failed to scan game").WithCause(err)
	}
	g.Slug = domain.GameSlug(slug)
	return &g, nil
}
