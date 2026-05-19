package middleware

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
	"github.com/404NFIDv2/bot-game-management/pkg/response"
)

// BotRateLimit enforces a sliding-window rate limit per authenticated bot token.
// The bot token hash is read from the context (injected by BotAuth).
// If no bot is present in context (e.g. admin-auth path) the limit is not applied.
func BotRateLimit(client *redis.Client, limit int, window time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		bot, ok := BotFromContext(c.Context())
		if !ok {
			return c.Next()
		}
		key := "rl:bot:" + bot.ID.String()
		if err := checkLimit(c, client, key, limit, window); err != nil {
			return err
		}
		return c.Next()
	}
}

// IPRateLimit enforces a sliding-window rate limit per client IP address.
// Intended for unauthenticated (or pre-auth) endpoints.
func IPRateLimit(client *redis.Client, limit int, window time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := "rl:ip:" + c.IP()
		if err := checkLimit(c, client, key, limit, window); err != nil {
			return err
		}
		return c.Next()
	}
}

// checkLimit implements a Redis sliding-window counter using a sorted set.
// Members and scores are both the current Unix timestamp in nanoseconds to ensure
// uniqueness within the same millisecond.
// If client is nil the check is skipped (fail-open).
func checkLimit(c *fiber.Ctx, client *redis.Client, key string, limit int, window time.Duration) error {
	if client == nil {
		return nil
	}
	ctx := context.Background()
	now := time.Now()
	windowStart := now.Add(-window)

	pipe := client.Pipeline()
	// Remove expired entries.
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart.UnixNano(), 10))
	// Add current request.
	member := fmt.Sprintf("%d", now.UnixNano())
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: member})
	// Count entries in window.
	countCmd := pipe.ZCard(ctx, key)
	// Set key expiry.
	pipe.Expire(ctx, key, window)

	if _, err := pipe.Exec(ctx); err != nil {
		// On Redis failure, fail open (allow the request).
		return c.Next()
	}

	count := countCmd.Val()
	if count > int64(limit) {
		// Retry-After: seconds until the oldest entry in the window expires.
		oldest, err := client.ZRangeWithScores(ctx, key, 0, 0).Result()
		retryAfter := int(window.Seconds())
		if err == nil && len(oldest) > 0 {
			oldestNs := int64(oldest[0].Score)
			expiresAt := time.Unix(0, oldestNs).Add(window)
			if secs := int(time.Until(expiresAt).Seconds()) + 1; secs > 0 {
				retryAfter = secs
			}
		}

		c.Set("Retry-After", strconv.Itoa(retryAfter))
		appErr := apperrors.RateLimited("rate limit exceeded")
		return c.Status(appErr.HTTPStatus).JSON(response.Error(appErr))
	}
	return nil
}
