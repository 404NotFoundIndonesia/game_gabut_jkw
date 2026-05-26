package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── TurnStore ─────────────────────────────────────────────────────────────────

// TurnContext holds the state needed to process a player's in-progress turn.
// Stored per (botID, userID) so DM callbacks can resolve back to the group.
type TurnContext struct {
	GroupChatID     int64     `json:"group_chat_id"`
	GroupMsgID      int64     `json:"group_msg_id"`
	SessionID       uuid.UUID `json:"session_id"`
	DMKeyboardMsgID int64     `json:"dm_keyboard_msg_id"` // DM message to edit after play
}

// TurnStore maps (botID, userID) → TurnContext in Redis.
type TurnStore interface {
	Set(ctx context.Context, botID uuid.UUID, userID int64, tc TurnContext, ttl time.Duration) error
	Get(ctx context.Context, botID uuid.UUID, userID int64) (TurnContext, error)
	Delete(ctx context.Context, botID uuid.UUID, userID int64) error
}

type RedisTurnStore struct{ client *redis.Client }

func NewRedisTurnStore(client *redis.Client) *RedisTurnStore {
	return &RedisTurnStore{client: client}
}

func turnKey(botID uuid.UUID, userID int64) string {
	return fmt.Sprintf("turn_ctx:%s:%d", botID, userID)
}

func (r *RedisTurnStore) Set(ctx context.Context, botID uuid.UUID, userID int64, tc TurnContext, ttl time.Duration) error {
	b, err := json.Marshal(tc)
	if err != nil {
		return fmt.Errorf("turn store marshal: %w", err)
	}
	return r.client.Set(ctx, turnKey(botID, userID), b, ttl).Err()
}

func (r *RedisTurnStore) Get(ctx context.Context, botID uuid.UUID, userID int64) (TurnContext, error) {
	val, err := r.client.Get(ctx, turnKey(botID, userID)).Result()
	if err == redis.Nil {
		return TurnContext{}, apperrors.NotFound("no active turn context")
	}
	if err != nil {
		return TurnContext{}, fmt.Errorf("turn store get: %w", err)
	}
	var tc TurnContext
	if err := json.Unmarshal([]byte(val), &tc); err != nil {
		return TurnContext{}, fmt.Errorf("turn store unmarshal: %w", err)
	}
	return tc, nil
}

func (r *RedisTurnStore) Delete(ctx context.Context, botID uuid.UUID, userID int64) error {
	return r.client.Del(ctx, turnKey(botID, userID)).Err()
}

// ── GameMsgStore ──────────────────────────────────────────────────────────────

// GameMsgStore maps (botID, chatID) → the turn-announcement message ID in Redis.
// The handler edits this message each turn instead of sending new ones.
type GameMsgStore interface {
	Set(ctx context.Context, botID uuid.UUID, chatID, msgID int64, ttl time.Duration) error
	Get(ctx context.Context, botID uuid.UUID, chatID int64) (int64, error)
	Delete(ctx context.Context, botID uuid.UUID, chatID int64) error
}

type RedisGameMsgStore struct{ client *redis.Client }

func NewRedisGameMsgStore(client *redis.Client) *RedisGameMsgStore {
	return &RedisGameMsgStore{client: client}
}

func gameMsgKey(botID uuid.UUID, chatID int64) string {
	return fmt.Sprintf("game_msg:%s:%d", botID, chatID)
}

func (r *RedisGameMsgStore) Set(ctx context.Context, botID uuid.UUID, chatID, msgID int64, ttl time.Duration) error {
	return r.client.Set(ctx, gameMsgKey(botID, chatID), msgID, ttl).Err()
}

func (r *RedisGameMsgStore) Get(ctx context.Context, botID uuid.UUID, chatID int64) (int64, error) {
	val, err := r.client.Get(ctx, gameMsgKey(botID, chatID)).Int64()
	if err == redis.Nil {
		return 0, apperrors.NotFound("no game message")
	}
	if err != nil {
		return 0, fmt.Errorf("game msg store get: %w", err)
	}
	return val, nil
}

func (r *RedisGameMsgStore) Delete(ctx context.Context, botID uuid.UUID, chatID int64) error {
	return r.client.Del(ctx, gameMsgKey(botID, chatID)).Err()
}
