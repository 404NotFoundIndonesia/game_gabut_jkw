package infrastructure

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

const sessionStateKeyPrefix = "session:state:"

// RedisSessionCache stores hot session state in Redis.
type RedisSessionCache struct {
	client *redis.Client
}

func NewRedisSessionCache(client *redis.Client) *RedisSessionCache {
	return &RedisSessionCache{client: client}
}

// GetState returns the cached session state. Returns (nil, nil) on cache miss.
func (c *RedisSessionCache) GetState(ctx context.Context, id uuid.UUID) (json.RawMessage, error) {
	val, err := c.client.Get(ctx, sessionStateKeyPrefix+id.String()).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, apperrors.Internal("redis get state failed").WithCause(err)
	}
	return json.RawMessage(val), nil
}

// SetState writes session state to Redis with the given TTL.
func (c *RedisSessionCache) SetState(ctx context.Context, id uuid.UUID, state json.RawMessage, ttl time.Duration) error {
	if err := c.client.Set(ctx, sessionStateKeyPrefix+id.String(), []byte(state), ttl).Err(); err != nil {
		return apperrors.Internal("redis set state failed").WithCause(err)
	}
	return nil
}

// InvalidateState removes the cached state for a session.
func (c *RedisSessionCache) InvalidateState(ctx context.Context, id uuid.UUID) error {
	if err := c.client.Del(ctx, sessionStateKeyPrefix+id.String()).Err(); err != nil && err != redis.Nil {
		return apperrors.Internal("redis delete state failed").WithCause(err)
	}
	return nil
}
