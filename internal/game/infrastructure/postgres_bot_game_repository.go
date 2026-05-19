package infrastructure

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/404NFIDv2/bot-game-management/internal/game/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/google/uuid"
)

// PostgresBotGameRepository implements domain.BotGameRepository.
type PostgresBotGameRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresBotGameRepository(pool *pgxpool.Pool) *PostgresBotGameRepository {
	return &PostgresBotGameRepository{pool: pool}
}

func (r *PostgresBotGameRepository) Assign(ctx context.Context, botID, gameID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO bot_games (bot_id, game_id, assigned_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (bot_id, game_id) DO NOTHING
	`, botID, gameID, time.Now().UTC())
	if err != nil {
		return apperrors.Internal("failed to assign game").WithCause(err)
	}
	return nil
}

func (r *PostgresBotGameRepository) Remove(ctx context.Context, botID, gameID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM bot_games WHERE bot_id = $1 AND game_id = $2
	`, botID, gameID)
	if err != nil {
		return apperrors.Internal("failed to remove game assignment").WithCause(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("game assignment not found")
	}
	return nil
}

func (r *PostgresBotGameRepository) FindByBot(ctx context.Context, botID uuid.UUID) ([]*domain.BotGame, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT bg.bot_id, bg.game_id, bg.assigned_at,
		       g.id, g.slug, g.name, g.description, g.min_players, g.max_players
		FROM bot_games bg
		JOIN games g ON g.id = bg.game_id
		WHERE bg.bot_id = $1
		ORDER BY bg.assigned_at DESC
	`, botID)
	if err != nil {
		return nil, apperrors.Internal("failed to list bot games").WithCause(err)
	}
	defer rows.Close()

	var result []*domain.BotGame
	for rows.Next() {
		var bg domain.BotGame
		var g domain.Game
		var slug string
		if err := rows.Scan(
			&bg.BotID, &bg.GameID, &bg.AssignedAt,
			&g.ID, &slug, &g.Name, &g.Description, &g.MinPlayers, &g.MaxPlayers,
		); err != nil {
			return nil, apperrors.Internal("failed to scan bot game row").WithCause(err)
		}
		g.Slug = domain.GameSlug(slug)
		bg.Game = &g
		result = append(result, &bg)
	}
	return result, nil
}

func (r *PostgresBotGameRepository) ExistsByBotAndGame(ctx context.Context, botID, gameID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM bot_games WHERE bot_id = $1 AND game_id = $2)
	`, botID, gameID).Scan(&exists)
	if err != nil {
		return false, apperrors.Internal("failed to check bot game existence").WithCause(err)
	}
	return exists, nil
}
