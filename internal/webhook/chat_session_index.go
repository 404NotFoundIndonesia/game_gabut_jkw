package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ChatSessionIndex maps (botID, chatID) → active sessionID in Redis.
type ChatSessionIndex interface {
	Set(ctx context.Context, botID uuid.UUID, chatID int64, sessionID uuid.UUID, ttl time.Duration) error
	Get(ctx context.Context, botID uuid.UUID, chatID int64) (uuid.UUID, error)
	Delete(ctx context.Context, botID uuid.UUID, chatID int64) error
}

// RedisChatSessionIndex implements ChatSessionIndex backed by Redis.
type RedisChatSessionIndex struct {
	client *redis.Client
}

func NewRedisChatSessionIndex(client *redis.Client) *RedisChatSessionIndex {
	return &RedisChatSessionIndex{client: client}
}

func chatSessionKey(botID uuid.UUID, chatID int64) string {
	return fmt.Sprintf("chat_session:%s:%d", botID, chatID)
}

// Set writes botID+chatID → sessionID with the given TTL.
func (r *RedisChatSessionIndex) Set(ctx context.Context, botID uuid.UUID, chatID int64, sessionID uuid.UUID, ttl time.Duration) error {
	return r.client.Set(ctx, chatSessionKey(botID, chatID), sessionID.String(), ttl).Err()
}

// Get returns the sessionID for the given bot+chat pair.
// Returns NotFound when no active session exists.
func (r *RedisChatSessionIndex) Get(ctx context.Context, botID uuid.UUID, chatID int64) (uuid.UUID, error) {
	val, err := r.client.Get(ctx, chatSessionKey(botID, chatID)).Result()
	if err == redis.Nil {
		return uuid.Nil, apperrors.NotFound("no active session for this chat")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("chat session index get: %w", err)
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("chat session index: invalid uuid %q: %w", val, err)
	}
	return id, nil
}

// Delete removes the index entry for the given bot+chat pair.
func (r *RedisChatSessionIndex) Delete(ctx context.Context, botID uuid.UUID, chatID int64) error {
	return r.client.Del(ctx, chatSessionKey(botID, chatID)).Err()
}
