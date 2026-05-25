# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run (local, no Docker)
go run ./cmd/api

# Run with hot-reload (Docker)
docker compose up

# Build binary
go build -o ./tmp/main ./cmd/api

# Unit tests
go test ./... -count=1

# Unit tests with coverage
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out | grep total

# Single package
go test ./internal/games/uno/... -v

# Integration tests (require live Postgres + Redis)
DB_DSN="postgres://app:secret@localhost:5432/bot_game_test?sslmode=disable" \
REDIS_URL="redis://localhost:6379" \
ADMIN_API_KEY=test-admin-key \
BOT_TOKEN_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef \
go test ./... -run Integration -v

# E2E tests (require full stack: docker compose up -d)
BASE_URL=http://localhost:8080 ADMIN_API_KEY=<key> \
go test ./test/e2e/ -v -tags e2e -count=1

# Lint
golangci-lint run

# Production Docker
docker compose -f docker-compose.prod.yml up -d
```

## Architecture

Modular monolith following DDD. Five domains under `internal/`:

| Domain | Path | Responsibility |
|--------|------|----------------|
| `bot` | `internal/bot/` | Bot CRUD, token encryption, BotAuth |
| `game` | `internal/game/` | Game catalog, bot-game assignment |
| `session` | `internal/session/` | Session lifecycle, move dispatch, archival |
| `leaderboard` | `internal/leaderboard/` | Score aggregation, Redis cache-aside |
| `health` | `internal/health/` | Liveness + readiness probes |

Each domain has four sub-layers: `domain/` → `application/` → `infrastructure/` → `interface/http/`.

Cross-domain wiring uses narrow interfaces to avoid circular imports:
- **`SessionEnder`** — `BotService` uses it; `SessionService` implements it. Needed so `DeleteBot` can force-end active sessions.
- **`ScoreCommitter`** — `SessionService` uses it; `LeaderboardService` implements it. Scores committed at session end, idempotency via `leaderboard_commits` table.

All dependency wiring happens in `cmd/api/main.go`.

### Game engines (`internal/games/`)

`GameEngine` interface (`engine.go`) — four pure methods, no I/O:
- `Init` — build initial state
- `Apply` — execute move, return new state + events
- `Evaluate` — check win condition
- `Validate` — check move legality without mutation

State is always `json.RawMessage` (persisted to Postgres JSONB). Engines registered by slug at startup via `games.Registry`.

Three implementations: `uno/`, `sambung_kata/` (KBBI validation — offline word list or HTTP API), `truth_or_date/`.

### State persistence

Redis written first on every move; if Postgres fails, Redis key is immediately invalidated. Keeps both stores consistent without transactions across backends.

### Webhook handlers (`internal/webhook/`)

- `MainBotHandler` — admin commands via main Telegram bot (`/addbot` is a multi-step FSM; state stored in Redis with TTL)
- `ChildBotHandler` — player commands routed by `bot_id` in URL; `RedisChatSessionIndex` maps `(bot_id, chat_id)` → `session_id`

Both always return HTTP 200 to Telegram (prevents retries).

### Auth layers

| Middleware | Header | Scope |
|------------|--------|-------|
| `AdminAuth` | `Authorization: Bearer <key>` | Admin REST routes |
| `BotAuth` | `X-Bot-Token: <raw-token>` | Bot-facing routes (token hashed SHA-256 for lookup) |
| `WebhookSecret` | `X-Telegram-Bot-Api-Secret-Token` | All `/telegram/*` routes |

Bot tokens stored AES-256-GCM encrypted; `BotToken.MarshalJSON()` always returns `"[REDACTED]"`.

### Key env vars

`BOT_TOKEN_ENCRYPTION_KEY` must be exactly 32 characters. `TELEGRAM_ADMIN_IDS` is comma-separated. `WEBHOOK_BASE_URL` must be HTTPS with no trailing slash.

### Observability

- Structured JSON logs via `log/slog`; request ID in context throughout
- Prometheus metrics on separate port (`METRICS_PORT`, default 9090) via plain `net/http` — not through Fiber
- `/health` (liveness) and `/ready` (readiness — pings Postgres + Redis)
