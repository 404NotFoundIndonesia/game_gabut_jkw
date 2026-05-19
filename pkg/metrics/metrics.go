package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// HTTPRequestsTotal counts all HTTP requests by method, path, and status code.
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration records latency of HTTP requests by method and path.
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1.0},
		},
		[]string{"method", "path"},
	)

	// ActiveSessionsTotal tracks currently active sessions by bot and game slug.
	ActiveSessionsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "active_sessions_total",
			Help: "Number of currently active game sessions.",
		},
		[]string{"bot_id", "game_slug"},
	)

	// GameMovesTotal counts submitted moves per game engine.
	GameMovesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "game_moves_total",
			Help: "Total number of moves submitted per game.",
		},
		[]string{"game_slug"},
	)

	// LeaderboardCacheHits counts cache hits on leaderboard reads.
	LeaderboardCacheHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "leaderboard_cache_hits_total",
		Help: "Total number of leaderboard cache hits.",
	})

	// LeaderboardCacheMisses counts cache misses on leaderboard reads.
	LeaderboardCacheMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "leaderboard_cache_misses_total",
		Help: "Total number of leaderboard cache misses.",
	})
)

func init() {
	prometheus.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		ActiveSessionsTotal,
		GameMovesTotal,
		LeaderboardCacheHits,
		LeaderboardCacheMisses,
	)
}
