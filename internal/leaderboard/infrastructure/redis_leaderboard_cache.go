package infrastructure

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

const leaderboardTTL = 5 * time.Minute

// RedisLeaderboardCache caches leaderboard results as serialised JSON blobs.
// Keys follow the pattern lb:<scope>[:<ids>].
type RedisLeaderboardCache struct {
	client *redis.Client
}

func NewRedisLeaderboardCache(client *redis.Client) *RedisLeaderboardCache {
	return &RedisLeaderboardCache{client: client}
}

// GetByBot returns a cached Leaderboard or (nil, nil) on miss.
func (c *RedisLeaderboardCache) GetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	return c.get(ctx, c.botKey(botID, params))
}

// SetByBot stores a Leaderboard for the bot scope.
func (c *RedisLeaderboardCache) SetByBot(ctx context.Context, botID uuid.UUID, params pagination.Params, lb *domain.Leaderboard) error {
	return c.set(ctx, c.botKey(botID, params), lb)
}

// InvalidateByBot deletes all cached pages for a bot (pattern-based DEL).
func (c *RedisLeaderboardCache) InvalidateByBot(ctx context.Context, botID uuid.UUID) error {
	return c.invalidatePattern(ctx, "lb:bot:"+botID.String()+":*")
}

// GetByBotAndGame returns a cached Leaderboard or (nil, nil) on miss.
func (c *RedisLeaderboardCache) GetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	return c.get(ctx, c.botGameKey(botID, gameID, params))
}

// SetByBotAndGame stores a Leaderboard for the bot+game scope.
func (c *RedisLeaderboardCache) SetByBotAndGame(ctx context.Context, botID, gameID uuid.UUID, params pagination.Params, lb *domain.Leaderboard) error {
	return c.set(ctx, c.botGameKey(botID, gameID, params), lb)
}

// InvalidateByBotAndGame deletes cached pages for a bot+game combination.
func (c *RedisLeaderboardCache) InvalidateByBotAndGame(ctx context.Context, botID, gameID uuid.UUID) error {
	return c.invalidatePattern(ctx, "lb:bot_game:"+botID.String()+":"+gameID.String()+":*")
}

// GetGlobal returns the cached global Leaderboard or (nil, nil) on miss.
func (c *RedisLeaderboardCache) GetGlobal(ctx context.Context, params pagination.Params) (*domain.Leaderboard, error) {
	return c.get(ctx, c.globalKey(params))
}

// SetGlobal stores the global Leaderboard.
func (c *RedisLeaderboardCache) SetGlobal(ctx context.Context, params pagination.Params, lb *domain.Leaderboard) error {
	return c.set(ctx, c.globalKey(params), lb)
}

// InvalidateGlobal deletes all cached global leaderboard pages.
func (c *RedisLeaderboardCache) InvalidateGlobal(ctx context.Context) error {
	return c.invalidatePattern(ctx, "lb:global:*")
}

// GetGlobalByGame returns the cached game-scoped global Leaderboard or (nil, nil) on miss.
func (c *RedisLeaderboardCache) GetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params) (*domain.Leaderboard, error) {
	return c.get(ctx, c.globalGameKey(gameID, params))
}

// SetGlobalByGame stores the game-scoped global Leaderboard.
func (c *RedisLeaderboardCache) SetGlobalByGame(ctx context.Context, gameID uuid.UUID, params pagination.Params, lb *domain.Leaderboard) error {
	return c.set(ctx, c.globalGameKey(gameID, params), lb)
}

// InvalidateGlobalByGame deletes cached pages for a specific game's global leaderboard.
func (c *RedisLeaderboardCache) InvalidateGlobalByGame(ctx context.Context, gameID uuid.UUID) error {
	return c.invalidatePattern(ctx, "lb:global_game:"+gameID.String()+":*")
}

// ─── key builders ─────────────────────────────────────────────────────────────

func (c *RedisLeaderboardCache) botKey(botID uuid.UUID, p pagination.Params) string {
	return "lb:bot:" + botID.String() + ":" + pageKey(p)
}

func (c *RedisLeaderboardCache) botGameKey(botID, gameID uuid.UUID, p pagination.Params) string {
	return "lb:bot_game:" + botID.String() + ":" + gameID.String() + ":" + pageKey(p)
}

func (c *RedisLeaderboardCache) globalKey(p pagination.Params) string {
	return "lb:global:" + pageKey(p)
}

func (c *RedisLeaderboardCache) globalGameKey(gameID uuid.UUID, p pagination.Params) string {
	return "lb:global_game:" + gameID.String() + ":" + pageKey(p)
}

func pageKey(p pagination.Params) string {
	return itoa(p.Limit) + "_" + itoa(p.Offset)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// ─── low-level helpers ────────────────────────────────────────────────────────

func (c *RedisLeaderboardCache) get(ctx context.Context, key string) (*domain.Leaderboard, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, apperrors.Internal("redis leaderboard get failed").WithCause(err)
	}
	var lb domain.Leaderboard
	if err := json.Unmarshal(val, &lb); err != nil {
		return nil, apperrors.Internal("redis leaderboard unmarshal failed").WithCause(err)
	}
	return &lb, nil
}

func (c *RedisLeaderboardCache) set(ctx context.Context, key string, lb *domain.Leaderboard) error {
	data, err := json.Marshal(lb)
	if err != nil {
		return apperrors.Internal("redis leaderboard marshal failed").WithCause(err)
	}
	if err := c.client.Set(ctx, key, data, leaderboardTTL).Err(); err != nil {
		return apperrors.Internal("redis leaderboard set failed").WithCause(err)
	}
	return nil
}

func (c *RedisLeaderboardCache) invalidatePattern(ctx context.Context, pattern string) error {
	keys, err := c.client.Keys(ctx, pattern).Result()
	if err != nil {
		return apperrors.Internal("redis leaderboard keys scan failed").WithCause(err)
	}
	if len(keys) == 0 {
		return nil
	}
	if err := c.client.Del(ctx, keys...).Err(); err != nil && err != redis.Nil {
		return apperrors.Internal("redis leaderboard invalidate failed").WithCause(err)
	}
	return nil
}
