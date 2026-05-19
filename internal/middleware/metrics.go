package middleware

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/404NFIDv2/bot-game-management/pkg/metrics"
)

// Metrics records HTTP request count and latency for every route.
func Metrics() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		method := c.Method()
		// Use the matched route template (e.g. "/api/v1/bots/:bot_id/sessions") to avoid
		// high cardinality from raw URL paths containing IDs.
		path := c.Route().Path
		status := strconv.Itoa(c.Response().StatusCode())

		duration := time.Since(start).Seconds()
		metrics.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)

		return err
	}
}
