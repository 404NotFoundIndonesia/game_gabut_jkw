package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// PostgresSessionRepository implements domain.SessionRepository using pgx.
type PostgresSessionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresSessionRepository(pool *pgxpool.Pool) *PostgresSessionRepository {
	return &PostgresSessionRepository{pool: pool}
}

// Save upserts a session and all its players inside a single transaction.
func (r *PostgresSessionRepository) Save(ctx context.Context, s *domain.GameSession) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperrors.Internal("begin tx failed").WithCause(err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO game_sessions
			(id, bot_id, game_id, chat_id, status, state, started_at, ended_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			status     = EXCLUDED.status,
			state      = EXCLUDED.state,
			started_at = EXCLUDED.started_at,
			ended_at   = EXCLUDED.ended_at
	`, s.ID, s.BotID, s.GameID, s.ChatID, string(s.Status), []byte(s.State),
		s.StartedAt, s.EndedAt, s.CreatedAt)
	if err != nil {
		return apperrors.Internal("failed to upsert session").WithCause(err)
	}

	for _, p := range s.Players {
		_, err = tx.Exec(ctx, `
			INSERT INTO player_sessions
				(session_id, telegram_user_id, display_name, score, is_winner, joined_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (session_id, telegram_user_id) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				score        = EXCLUDED.score,
				is_winner    = EXCLUDED.is_winner
		`, s.ID, p.TelegramUserID, p.DisplayName, p.Score, p.IsWinner, p.JoinedAt)
		if err != nil {
			return apperrors.Internal("failed to upsert player session").WithCause(err)
		}
	}

	return tx.Commit(ctx)
}

func (r *PostgresSessionRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.GameSession, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, bot_id, game_id, chat_id, status, state, started_at, ended_at, created_at
		FROM game_sessions WHERE id = $1
	`, id)

	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.NotFound("session not found")
		}
		return nil, apperrors.Internal("failed to query session").WithCause(err)
	}

	s.Players, err = r.loadPlayers(ctx, id)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *PostgresSessionRepository) FindByBotID(
	ctx context.Context,
	botID uuid.UUID,
	filter domain.SessionFilter,
	limit, offset int,
) ([]*domain.GameSession, int, error) {
	where := "bot_id = $1"
	args := []any{botID}
	n := 2

	if filter.Status != nil {
		where += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, string(*filter.Status))
		n++
	}
	if filter.GameID != nil {
		where += fmt.Sprintf(" AND game_id = $%d", n)
		args = append(args, *filter.GameID)
		n++
	}

	var total int
	if err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM game_sessions WHERE "+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, apperrors.Internal("failed to count sessions").WithCause(err)
	}

	listArgs := append(args, limit, offset)
	rows, err := r.pool.Query(ctx,
		"SELECT id, bot_id, game_id, chat_id, status, state, started_at, ended_at, created_at "+
			"FROM game_sessions WHERE "+where+
			fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", n, n+1),
		listArgs...,
	)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list sessions").WithCause(err)
	}
	defer rows.Close()

	var sessions []*domain.GameSession
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, 0, apperrors.Internal("failed to scan session").WithCause(err)
		}
		sessions = append(sessions, s)
	}
	rows.Close()

	for _, s := range sessions {
		s.Players, err = r.loadPlayers(ctx, s.ID)
		if err != nil {
			return nil, 0, err
		}
	}
	return sessions, total, nil
}

func (r *PostgresSessionRepository) FindActiveByChatID(ctx context.Context, botID uuid.UUID, chatID int64) (*domain.GameSession, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, bot_id, game_id, chat_id, status, state, started_at, ended_at, created_at
		FROM game_sessions
		WHERE bot_id = $1 AND chat_id = $2 AND status IN ('CREATED', 'WAITING', 'IN_PROGRESS')
		ORDER BY created_at DESC LIMIT 1
	`, botID, chatID)

	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.NotFound("no active session for this chat")
		}
		return nil, apperrors.Internal("failed to query active session").WithCause(err)
	}

	s.Players, err = r.loadPlayers(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *PostgresSessionRepository) UpdateState(ctx context.Context, id uuid.UUID, state json.RawMessage) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE game_sessions SET state = $1 WHERE id = $2`, []byte(state), id)
	if err != nil {
		return apperrors.Internal("failed to update session state").WithCause(err)
	}
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (r *PostgresSessionRepository) loadPlayers(ctx context.Context, sessionID uuid.UUID) ([]domain.PlayerSession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT session_id, telegram_user_id, display_name, score, is_winner, joined_at
		FROM player_sessions WHERE session_id = $1 ORDER BY joined_at ASC
	`, sessionID)
	if err != nil {
		return nil, apperrors.Internal("failed to query players").WithCause(err)
	}
	defer rows.Close()

	players := []domain.PlayerSession{}
	for rows.Next() {
		var p domain.PlayerSession
		if err := rows.Scan(&p.SessionID, &p.TelegramUserID, &p.DisplayName,
			&p.Score, &p.IsWinner, &p.JoinedAt); err != nil {
			return nil, apperrors.Internal("failed to scan player").WithCause(err)
		}
		players = append(players, p)
	}
	return players, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row rowScanner) (*domain.GameSession, error) {
	var s domain.GameSession
	var status string
	var state []byte
	err := row.Scan(&s.ID, &s.BotID, &s.GameID, &s.ChatID, &status, &state,
		&s.StartedAt, &s.EndedAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	s.Status = domain.SessionStatus(status)
	if len(state) > 0 {
		s.State = json.RawMessage(state)
	} else {
		s.State = json.RawMessage("{}")
	}
	return &s, nil
}
