package infrastructure

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/google/uuid"
)

// PostgresBotRepository implements domain.BotRepository using pgx.
type PostgresBotRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresBotRepository(pool *pgxpool.Pool) *PostgresBotRepository {
	return &PostgresBotRepository{pool: pool}
}

// Save inserts a new bot or updates all mutable columns on id conflict.
func (r *PostgresBotRepository) Save(ctx context.Context, bot *domain.Bot) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO bots (id, name, token, token_hash, telegram_id, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		  SET name        = EXCLUDED.name,
		      token       = EXCLUDED.token,
		      token_hash  = EXCLUDED.token_hash,
		      active      = EXCLUDED.active,
		      updated_at  = EXCLUDED.updated_at
	`,
		bot.ID, bot.Name, bot.Token.Ciphertext(), bot.TokenHash,
		bot.TelegramID, bot.Active, bot.CreatedAt, bot.UpdatedAt,
	)
	if err != nil {
		return apperrors.Internal("failed to save bot").WithCause(err)
	}
	return nil
}

// FindByID returns a bot by its UUID or a NotFound error.
func (r *PostgresBotRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Bot, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, token, token_hash, telegram_id, active, created_at, updated_at
		FROM bots WHERE id = $1
	`, id)
	return scanBot(row)
}

// FindByTelegramID returns a bot by its Telegram user ID or a NotFound error.
func (r *PostgresBotRepository) FindByTelegramID(ctx context.Context, telegramID int64) (*domain.Bot, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, token, token_hash, telegram_id, active, created_at, updated_at
		FROM bots WHERE telegram_id = $1
	`, telegramID)
	return scanBot(row)
}

// FindByTokenHash returns a bot by its SHA-256 token hash or a NotFound error.
func (r *PostgresBotRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*domain.Bot, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, token, token_hash, telegram_id, active, created_at, updated_at
		FROM bots WHERE token_hash = $1
	`, tokenHash)
	return scanBot(row)
}

// FindAll returns a page of bots matching the filter, plus the total count.
func (r *PostgresBotRepository) FindAll(
	ctx context.Context, filter domain.BotFilter, limit, offset int,
) ([]*domain.Bot, int, error) {
	args := []any{limit, offset}
	where := ""
	if filter.Active != nil {
		args = append(args, *filter.Active)
		where = " WHERE active = $3"
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, name, token, token_hash, telegram_id, active, created_at, updated_at
		FROM bots`+where+`
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, args...)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list bots").WithCause(err)
	}
	defer rows.Close()

	var bots []*domain.Bot
	for rows.Next() {
		bot, err := scanBotFromRows(rows)
		if err != nil {
			return nil, 0, apperrors.Internal("failed to scan bot row").WithCause(err)
		}
		bots = append(bots, bot)
	}

	var total int
	countArgs := []any{}
	countWhere := ""
	if filter.Active != nil {
		countArgs = append(countArgs, *filter.Active)
		countWhere = " WHERE active = $1"
	}
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bots"+countWhere, countArgs...).Scan(&total); err != nil {
		return nil, 0, apperrors.Internal("failed to count bots").WithCause(err)
	}

	return bots, total, nil
}

// Delete removes a bot by ID. Returns NotFound if the row does not exist.
func (r *PostgresBotRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM bots WHERE id = $1`, id)
	if err != nil {
		return apperrors.Internal("failed to delete bot").WithCause(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("bot not found")
	}
	return nil
}

// scanBot reads a single bot from a pgx.Row.
func scanBot(row pgx.Row) (*domain.Bot, error) {
	var (
		b         domain.Bot
		ciphertext string
	)
	err := row.Scan(
		&b.ID, &b.Name, &ciphertext, &b.TokenHash,
		&b.TelegramID, &b.Active, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.NotFound("bot not found")
		}
		return nil, apperrors.Internal("failed to scan bot").WithCause(err)
	}
	b.Token = domain.NewBotToken(ciphertext)
	return &b, nil
}

// scanBotFromRows reads a single bot from pgx.Rows.
func scanBotFromRows(rows pgx.Rows) (*domain.Bot, error) {
	var (
		b          domain.Bot
		ciphertext string
	)
	err := rows.Scan(
		&b.ID, &b.Name, &ciphertext, &b.TokenHash,
		&b.TelegramID, &b.Active, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	b.Token = domain.NewBotToken(ciphertext)
	return &b, nil
}
