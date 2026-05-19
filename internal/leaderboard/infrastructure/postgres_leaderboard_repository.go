package infrastructure

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// PostgresLeaderboardRepository implements domain.LeaderboardRepository using pgx.
type PostgresLeaderboardRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresLeaderboardRepository(pool *pgxpool.Pool) *PostgresLeaderboardRepository {
	return &PostgresLeaderboardRepository{pool: pool}
}

// UpsertEntry accumulates a player's score, games_played, and wins.
func (r *PostgresLeaderboardRepository) UpsertEntry(ctx context.Context, e domain.LeaderboardEntry) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO leaderboard_entries
			(bot_id, game_id, telegram_user_id, display_name, total_score, games_played, wins, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (bot_id, game_id, telegram_user_id) DO UPDATE SET
			display_name  = EXCLUDED.display_name,
			total_score   = leaderboard_entries.total_score + EXCLUDED.total_score,
			games_played  = leaderboard_entries.games_played + EXCLUDED.games_played,
			wins          = leaderboard_entries.wins + EXCLUDED.wins,
			updated_at    = NOW()
	`, e.BotID, e.GameID, e.TelegramUserID, e.DisplayName,
		e.TotalScore, e.GamesPlayed, e.Wins)
	if err != nil {
		return apperrors.Internal("failed to upsert leaderboard entry").WithCause(err)
	}
	return nil
}

// GetByBot returns the bot-scoped leaderboard (aggregated across all games for that bot).
func (r *PostgresLeaderboardRepository) GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	const countQ = `
		SELECT COUNT(DISTINCT telegram_user_id)
		FROM leaderboard_entries
		WHERE bot_id = $1
	`
	const listQ = `
		WITH ranked AS (
			SELECT
				telegram_user_id,
				MAX(display_name)     AS display_name,
				SUM(total_score)      AS total_score,
				SUM(games_played)     AS games_played,
				SUM(wins)             AS wins,
				MAX(updated_at)       AS updated_at,
				ROW_NUMBER() OVER (ORDER BY SUM(total_score) DESC) AS rank
			FROM leaderboard_entries
			WHERE bot_id = $1
			GROUP BY telegram_user_id
		)
		SELECT rank, telegram_user_id, display_name, total_score, games_played, wins, updated_at
		FROM ranked
		ORDER BY rank
		LIMIT $2 OFFSET $3
	`
	return r.queryLeaderboard(ctx, countQ, listQ, params,
		func(e *domain.LeaderboardEntry) { e.BotID = botID },
		botID)
}

// GetByBotAndGame returns the per-bot, per-game leaderboard.
func (r *PostgresLeaderboardRepository) GetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	const countQ = `
		SELECT COUNT(*)
		FROM leaderboard_entries
		WHERE bot_id = $1 AND game_id = $2
	`
	const listQ = `
		WITH ranked AS (
			SELECT
				telegram_user_id,
				display_name,
				total_score,
				games_played,
				wins,
				updated_at,
				ROW_NUMBER() OVER (ORDER BY total_score DESC) AS rank
			FROM leaderboard_entries
			WHERE bot_id = $1 AND game_id = $2
		)
		SELECT rank, telegram_user_id, display_name, total_score, games_played, wins, updated_at
		FROM ranked
		ORDER BY rank
		LIMIT $3 OFFSET $4
	`
	return r.queryLeaderboard(ctx, countQ, listQ, params,
		func(e *domain.LeaderboardEntry) { e.BotID = botID; e.GameID = gameID },
		botID, gameID)
}

// GetGlobal returns the cross-bot, cross-game global leaderboard.
func (r *PostgresLeaderboardRepository) GetGlobal(ctx context.Context, params pagination.Params) (*domain.Leaderboard, error) {
	const countQ = `SELECT COUNT(DISTINCT telegram_user_id) FROM leaderboard_entries`
	const listQ = `
		WITH ranked AS (
			SELECT
				telegram_user_id,
				MAX(display_name)     AS display_name,
				SUM(total_score)      AS total_score,
				SUM(games_played)     AS games_played,
				SUM(wins)             AS wins,
				MAX(updated_at)       AS updated_at,
				ROW_NUMBER() OVER (ORDER BY SUM(total_score) DESC) AS rank
			FROM leaderboard_entries
			GROUP BY telegram_user_id
		)
		SELECT rank, telegram_user_id, display_name, total_score, games_played, wins, updated_at
		FROM ranked
		ORDER BY rank
		LIMIT $1 OFFSET $2
	`
	return r.queryLeaderboard(ctx, countQ, listQ, params, nil)
}

// GetGlobalByGame returns the cross-bot leaderboard for a specific game.
func (r *PostgresLeaderboardRepository) GetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	const countQ = `
		SELECT COUNT(DISTINCT telegram_user_id)
		FROM leaderboard_entries
		WHERE game_id = $1
	`
	const listQ = `
		WITH ranked AS (
			SELECT
				telegram_user_id,
				MAX(display_name)     AS display_name,
				SUM(total_score)      AS total_score,
				SUM(games_played)     AS games_played,
				SUM(wins)             AS wins,
				MAX(updated_at)       AS updated_at,
				ROW_NUMBER() OVER (ORDER BY SUM(total_score) DESC) AS rank
			FROM leaderboard_entries
			WHERE game_id = $1
			GROUP BY telegram_user_id
		)
		SELECT rank, telegram_user_id, display_name, total_score, games_played, wins, updated_at
		FROM ranked
		ORDER BY rank
		LIMIT $2 OFFSET $3
	`
	return r.queryLeaderboard(ctx, countQ, listQ, params,
		func(e *domain.LeaderboardEntry) { e.GameID = gameID },
		gameID)
}

// HasCommit reports whether this session's scores were already committed.
func (r *PostgresLeaderboardRepository) HasCommit(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM leaderboard_commits WHERE session_id = $1)`, sessionID,
	).Scan(&exists)
	if err != nil {
		return false, apperrors.Internal("failed to check leaderboard commit").WithCause(err)
	}
	return exists, nil
}

// RecordCommit marks a session as committed.
func (r *PostgresLeaderboardRepository) RecordCommit(ctx context.Context, sessionID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO leaderboard_commits (session_id) VALUES ($1) ON CONFLICT (session_id) DO NOTHING`,
		sessionID)
	if err != nil {
		return apperrors.Internal("failed to record leaderboard commit").WithCause(err)
	}
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// queryLeaderboard runs count + paginated ranked queries and assembles a Leaderboard.
// filterArgs are the positional args that appear in countQ (and at the start of listQ).
// The listQ must end with LIMIT $N OFFSET $N+1 after the filter args.
// decorate, if non-nil, is called on each entry to set scope-specific fields (BotID, GameID).
func (r *PostgresLeaderboardRepository) queryLeaderboard(
	ctx context.Context,
	countQ, listQ string,
	params pagination.Params,
	decorate func(*domain.LeaderboardEntry),
	filterArgs ...any,
) (*domain.Leaderboard, error) {
	var total int
	if err := r.pool.QueryRow(ctx, countQ, filterArgs...).Scan(&total); err != nil {
		return nil, apperrors.Internal("failed to count leaderboard entries").WithCause(err)
	}

	listArgs := append(filterArgs, params.Limit, params.Offset)
	rows, err := r.pool.Query(ctx, listQ, listArgs...)
	if err != nil {
		return nil, apperrors.Internal("failed to query leaderboard").WithCause(err)
	}
	defer rows.Close()

	entries := []domain.LeaderboardEntry{}
	for rows.Next() {
		var e domain.LeaderboardEntry
		var updatedAt time.Time
		if err := rows.Scan(&e.Rank, &e.TelegramUserID, &e.DisplayName,
			&e.TotalScore, &e.GamesPlayed, &e.Wins, &updatedAt); err != nil {
			return nil, apperrors.Internal("failed to scan leaderboard entry").WithCause(err)
		}
		e.UpdatedAt = updatedAt
		if decorate != nil {
			decorate(&e)
		}
		entries = append(entries, e)
	}

	return &domain.Leaderboard{Entries: entries, Total: total}, nil
}
