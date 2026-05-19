# Game Gabut JKW — Bot Game Management API

A backend API that acts as management center for multiple Telegram bots. Admins register child bots, assign games to them, and Telegram users play those games through the bots — with scores tracked on per-bot and global leaderboards.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![CI](https://img.shields.io/badge/CI-GitHub_Actions-2088FF?logo=github-actions)](https://github.com/404NFIDv2/bot-game-management/actions)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

---

## Overview

```
Admin
  └─▶ Game Management API  ─── assigns games ──▶ Child Bot A (Uno)
                            ─── assigns games ──▶ Child Bot B (Sambung Kata)
                            ─── assigns games ──▶ Child Bot C (Truth or Date)

Telegram User ─▶ Child Bot ─▶ API (BotApiKey) ─▶ Game Session ─▶ Leaderboard
```

One API controls N child Telegram bots. Each bot can have any combination of the three available games assigned to it. Game state is persisted to PostgreSQL (JSONB) and hot-cached in Redis.

---

## Features

- **Bot Management** — Register, update, deactivate, and delete Telegram bots; tokens encrypted at rest with AES-256-GCM.
- **Game Assignment** — Idempotently assign or revoke games per bot at runtime, no downtime.
- **Three Game Engines** — Uno, Sambung Kata (KBBI-validated), Truth or Date; all pure functions, fully unit-tested.
- **Session Lifecycle** — `CREATED → WAITING → IN_PROGRESS → FINISHED → ARCHIVED` with atomic state transitions.
- **Leaderboard** — Per-bot, per-bot-per-game, global, and global-per-game rankings; Redis cache-aside with 5-min TTL.
- **Rate Limiting** — Sliding-window rate limit per bot token (60 req/min) via Redis sorted sets; 429 with `Retry-After`.
- **Prometheus Metrics** — HTTP request counts/duration, active sessions gauge, game move counters, leaderboard cache hit/miss.
- **Session Archival** — Hourly background job archives `FINISHED` sessions older than the TTL.
- **Input Sanitization** — All user string inputs stripped of control characters and null bytes; length-capped at handler layer.
- **Dual Auth** — `AdminApiKey` (Bearer) for admin endpoints; `BotApiKey` (X-Bot-Token) for bot-facing endpoints.
- **Observability** — Structured JSON logs via `log/slog`; Prometheus metrics on a dedicated port; health + readiness probes.
- **CI Pipeline** — GitHub Actions: lint, unit tests (≥80% coverage gate), integration tests (Postgres + Redis), Docker build.

---

## Games

| Game | Slug | Players | Description |
|------|------|---------|-------------|
| **Uno** | `uno` | 2–10 | Standard card game. Full rules: Skip, Reverse, Draw Two, Wild, Wild Draw Four. First to empty hand wins. |
| **Sambung Kata** | `sambung_kata` | 2–20 | Word chain validated against KBBI. Each word must start with the last letter of the previous word. |
| **Truth or Date** | `truth_or_date` | 2–20 | Truth or Dare party game. Host can skip; free-text answers; 50+ built-in questions per type. |

---

## Tech Stack

| Layer | Choice |
|-------|--------|
| Language | Go 1.26 |
| HTTP Framework | Fiber v2 |
| Database | PostgreSQL 16 (JSONB game state) |
| Cache / Rate Limiting | Redis 7 |
| Migrations | golang-migrate |
| Metrics | Prometheus (`client_golang`) |
| Container | Docker + Docker Compose |
| CI | GitHub Actions |

---

## Project Structure

```
bot-game-management/
├── .github/workflows/
│   └── ci.yml                  # Lint, unit tests, integration tests, Docker build
├── cmd/api/                    # Entrypoint, dependency wiring
├── internal/
│   ├── bot/                    # Bot domain (CRUD, token encryption, auth)
│   │   ├── domain/
│   │   ├── application/
│   │   ├── infrastructure/
│   │   └── interface/http/
│   ├── game/                   # Game catalog + bot-game assignment
│   ├── session/                # Session lifecycle + archival job
│   ├── leaderboard/            # Score aggregation, Redis cache-aside
│   ├── health/                 # Liveness + readiness HTTP handlers
│   ├── middleware/             # Auth, rate limiting, metrics, error handler
│   └── games/                  # Pure game engine implementations
│       ├── engine.go           # GameEngine interface
│       ├── registry.go         # Slug → engine registry
│       ├── uno/
│       ├── sambung_kata/
│       │   └── kbbi/           # Offline + API KBBI validator
│       └── truth_or_date/
│           └── questions/      # Embedded question bank (50+ each)
├── pkg/
│   ├── crypto/                 # AES-256-GCM token encryption
│   ├── errors/                 # Typed AppError with HTTP status mapping
│   ├── logger/                 # Structured JSON slog wrapper
│   ├── metrics/                # Prometheus metric definitions (package-level vars)
│   ├── pagination/             # Query param parsing + Meta
│   ├── response/               # Envelope helpers (Success/Error)
│   ├── sanitize/               # Input sanitization (control chars, null bytes, truncation)
│   └── validator/              # go-playground/validator wrapper
├── migrations/                 # SQL migration files (000001–000005)
├── test/
│   └── e2e/                    # End-to-end tests (run with -tags e2e against live stack)
└── docker/
    ├── Dockerfile.dev          # Hot-reload (air)
    └── Dockerfile.prod         # Multi-stage, alpine, non-root
```

---

## Getting Started

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- Go 1.22+ (for local development without Docker)

### Development (with Docker)

```bash
# 1. Copy environment file
cp .env.example .env.local

# 2. Edit secrets (at minimum set ADMIN_API_KEY and BOT_TOKEN_ENCRYPTION_KEY)
#    BOT_TOKEN_ENCRYPTION_KEY must be exactly 32 characters
vim .env.local

# 3. Start all services (API + PostgreSQL + Redis) with hot-reload
docker compose up
```

The API is available at `http://localhost:8080`. Prometheus metrics are on `http://localhost:9090/metrics`. Editing any Go file triggers automatic rebuild within ~3 seconds.

### Local Development (without Docker)

```bash
# Requires PostgreSQL and Redis running locally.
cp .env.example .env.local
source .env.local   # or use a tool like direnv

go run ./cmd/api
```

---

## Configuration

All configuration is via environment variables. No config files.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `APP_ENV` | | `development` | `development` or `production` |
| `APP_PORT` | | `8080` | HTTP listen port |
| `METRICS_PORT` | | `9090` | Prometheus metrics listen port |
| `DB_DSN` | **Yes** | — | PostgreSQL connection string |
| `REDIS_URL` | **Yes** | — | Redis connection URL |
| `ADMIN_API_KEY` | **Yes** | — | Bearer token for admin endpoints |
| `BOT_TOKEN_ENCRYPTION_KEY` | **Yes** | — | AES key for token encryption (32 chars) |
| `KBBI_MODE` | | `offline` | `offline` (embedded word list) or `api` |
| `KBBI_API_URL` | | — | KBBI API base URL (when `KBBI_MODE=api`) |
| `LOG_LEVEL` | | `info` | `debug`, `info`, `warn`, or `error` |
| `SESSION_TTL_HOURS` | | `168` | Hours before finished sessions are archived (7 days) |

---

## API Reference

Base URL: `/api/v1`

All responses use a consistent envelope:

```json
{
  "success": true,
  "data": {},
  "error": null,
  "meta": { "total": 0, "limit": 10, "offset": 0 }
}
```

### Authentication

| Scheme | Header | Used on |
|--------|--------|---------|
| `AdminApiKey` | `Authorization: Bearer <key>` | All admin endpoints |
| `BotApiKey` | `X-Bot-Token: <raw-token>` | Bot-facing endpoints |

Bot-facing endpoints on the shared group also accept `AdminApiKey` for operations that allow both callers (e.g. `GET /sessions`, `POST .../end`). Endpoints that are bot-only (create, join, start, move) return 403 when called with an admin key.

### Endpoints

#### Health

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | None | Liveness — always 200 if process alive |
| GET | `/ready` | None | Readiness — 503 if PostgreSQL or Redis down |
| GET | `/metrics` | None | Prometheus scrape endpoint (port 9090) |

#### Bots

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/bots` | Admin | Register a new bot |
| GET | `/api/v1/bots` | Admin | List bots (filter by `active`, paginate) |
| GET | `/api/v1/bots/:bot_id` | Admin | Get bot detail |
| PATCH | `/api/v1/bots/:bot_id` | Admin | Update name, active flag, or rotate token |
| DELETE | `/api/v1/bots/:bot_id` | Admin | Deactivate and delete bot |

#### Game Assignment

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/bots/:bot_id/games` | Admin | Assign a game to a bot (idempotent) |
| DELETE | `/api/v1/bots/:bot_id/games/:game_id` | Admin | Remove game from bot |
| GET | `/api/v1/bots/:bot_id/games` | Admin or Bot | List games assigned to bot |

#### Game Catalog

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/games` | Admin or Bot | List all available games |
| GET | `/api/v1/games/:game_id` | Admin or Bot | Get game detail |

#### Sessions

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/bots/:bot_id/sessions` | Bot only | Create a game session |
| GET | `/api/v1/bots/:bot_id/sessions` | Admin or Bot | List sessions (filter by `status`, `game_id`) |
| GET | `/api/v1/bots/:bot_id/sessions/:session_id` | Admin or Bot | Get session state |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/join` | Bot only | Player joins session |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/start` | Bot only | Host starts session |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/move` | Bot only | Submit game move |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/end` | Admin or Bot | Force end session |

#### Leaderboard

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/bots/:bot_id/leaderboard` | Admin or Bot | Bot leaderboard across all games |
| GET | `/api/v1/bots/:bot_id/leaderboard/:game_id` | Admin or Bot | Bot leaderboard for one game |
| GET | `/api/v1/leaderboard` | Admin or Bot | Global leaderboard across all bots and games |
| GET | `/api/v1/leaderboard/:game_id` | Admin or Bot | Global leaderboard for one game |

All list endpoints accept `limit` (default 10, max 100) and `offset` query params.

Full schema definitions are in [`openapi.yaml`](openapi.yaml).

---

## Running Tests

```bash
# All unit tests
go test ./... -count=1

# Single package with verbose output
go test ./internal/games/uno/... -v

# Integration tests (require live DB + Redis)
DB_DSN="postgres://..." REDIS_URL="redis://..." \
  go test ./... -run Integration -count=1

# End-to-end tests (require full stack: docker compose up -d)
BASE_URL=http://localhost:8080 ADMIN_API_KEY=<key> \
  go test ./test/e2e/ -v -tags e2e -count=1
```

### Test Coverage by Layer

| Layer | Target |
|-------|--------|
| Game engine logic | 100% |
| Application / use cases | ≥ 90% |
| HTTP handlers | ≥ 70% |
| Overall | ≥ 80% |

---

## Production Deployment

```bash
# Build and start production stack (API + PostgreSQL + Redis + Nginx)
docker compose -f docker-compose.prod.yml up -d
```

The production image is a multi-stage build (`golang:1.26-alpine` → `alpine`). The final image contains only the compiled binary — no source code, no toolchain. The API runs as a non-root user.

---

## Architecture

The project follows **Domain-Driven Design** with a modular monolith layout. Each domain (`bot`, `game`, `session`, `leaderboard`) owns its entities, repository interfaces, application services, and HTTP handlers. The `games/` package contains pure game engine implementations that are dependency-free and fully unit-testable.

Cross-domain dependencies are expressed as narrow interfaces rather than direct package imports, keeping each module independently testable. Key patterns:

- **`SessionEnder`** — `BotService` depends on this interface; `SessionService` implements it. Lets `DeleteBot` force-end active sessions without a circular import.
- **`ScoreCommitter`** — `SessionService` depends on this interface; `LeaderboardService` implements it. Scores are committed when a session finishes, with idempotency guarded by a `leaderboard_commits` table.
- **Atomic state write** — On every move, Redis is written first; if Postgres fails the Redis key is immediately invalidated, keeping both stores consistent.

### Security Design

- **Token storage** — Telegram bot tokens are AES-256-GCM encrypted at rest. A SHA-256 hash of the raw token is stored separately for O(1) `BotAuth` lookup without decrypting.
- **Token redaction** — `BotToken.MarshalJSON()` always returns `"[REDACTED]"`. Tokens never appear in logs, error messages, or response bodies.
- **Constant-time comparison** — `AdminAuth` middleware uses `crypto/subtle` for safe key comparison.
- **Rate limiting** — Per-bot sliding-window (60 req/min) via Redis sorted sets; `Retry-After` header on 429.
- **Input sanitization** — All user-submitted strings (`display_name`, `reason`, `word`) are stripped of control characters and null bytes before reaching the service layer.
- **Parameterized SQL** — All queries use positional parameters via `pgx/v5`; no string interpolation of user input.

### Observability

| Signal | Implementation |
|--------|---------------|
| Structured logs | `log/slog` JSON handler; request ID propagated through context |
| Prometheus metrics | HTTP duration histogram, request counter, active sessions gauge, game move counter, leaderboard cache counters |
| Health check | `/health` (liveness) + `/ready` (readiness — pings Postgres + Redis) |
| Request tracing | `X-Request-ID` header injected by middleware, logged on every request |

---

## Development Roadmap

| Phase | Status | Scope |
|-------|--------|-------|
| 0 — Scaffold | ✅ Done | Repo structure, shared packages, Docker, DB/Redis setup |
| 1 — Bot Management | ✅ Done | Bot CRUD, game catalog, game assignment |
| 2 — Game Engines | ✅ Done | Uno, Sambung Kata, Truth or Date |
| 3 — Session API | ✅ Done | Session lifecycle, move submission, state persistence |
| 4 — Leaderboard | ✅ Done | Score aggregation, Redis cache-aside, idempotent commit |
| 5 — Hardening | ✅ Done | Rate limiting, Prometheus metrics, archival job, sanitization, E2E tests, CI |

---

## License

[MIT](LICENSE) © 2026 404 Not Found Indonesia
