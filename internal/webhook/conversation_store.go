package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConversationStore persists per-user FSM state in Redis.
type ConversationStore interface {
	Get(ctx context.Context, userID int64) (ConversationData, error)
	Set(ctx context.Context, userID int64, data ConversationData, ttl time.Duration) error
	Delete(ctx context.Context, userID int64) error
}

// RedisConversationStore implements ConversationStore backed by Redis.
type RedisConversationStore struct {
	client *redis.Client
}

func NewRedisConversationStore(client *redis.Client) *RedisConversationStore {
	return &RedisConversationStore{client: client}
}

func convKey(userID int64) string {
	return fmt.Sprintf("conv:main:%d", userID)
}

// Get returns the conversation state for a user. Returns ConvStateIdle on cache miss.
func (s *RedisConversationStore) Get(ctx context.Context, userID int64) (ConversationData, error) {
	val, err := s.client.Get(ctx, convKey(userID)).Bytes()
	if err == redis.Nil {
		return ConversationData{State: ConvStateIdle}, nil
	}
	if err != nil {
		return ConversationData{State: ConvStateIdle}, fmt.Errorf("conv store get: %w", err)
	}
	var data ConversationData
	if err := json.Unmarshal(val, &data); err != nil {
		return ConversationData{State: ConvStateIdle}, fmt.Errorf("conv store decode: %w", err)
	}
	return data, nil
}

// Set writes the conversation state with the given TTL.
func (s *RedisConversationStore) Set(ctx context.Context, userID int64, data ConversationData, ttl time.Duration) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("conv store encode: %w", err)
	}
	return s.client.Set(ctx, convKey(userID), b, ttl).Err()
}

// Delete removes the conversation state for a user.
func (s *RedisConversationStore) Delete(ctx context.Context, userID int64) error {
	return s.client.Del(ctx, convKey(userID)).Err()
}
