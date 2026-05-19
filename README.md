# Game Gabut JKW — Bot Game Management API

A backend API that acts as management center for multiple Telegram bots. Admins register child bots, assign games to them, and Telegram users play those games through the bots — with scores tracked on per-bot and global leaderboards.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
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
- **Three Game Engines** — Uno, Sambung Kata (KBBI-validated), Truth or Date; all pure functions, 100% unit-tested.
- **Session Lifecycle** — `CREATED → WAITING → IN_PROGRESS → FINISHED → ARCHIVED` with atomic state transitions.
- **Leaderboard** — Per-bot, per-bot-per-game, global, and global-per-game; Redis sorted sets for hot reads.
- **Dual Auth** — `AdminApiKey` (Bearer) for admin endpoints; `BotApiKey` (X-Bot-Token) for bot-facing endpoints.
- **Observability** — Structured JSON logs via `log/slog`; Prometheus metrics endpoint; health + readiness probes.

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
| Cache | Redis 7 |
| Migrations | golang-migrate |
| Container | Docker + Docker Compose |

---

## Project Structure

```
bot-game-management/
├── cmd/api/                    # Entrypoint, dependency wiring
├── internal/
│   ├── bot/                    # Bot domain (CRUD, token encryption, auth)
│   │   ├── domain/
│   │   ├── application/
│   │   ├── infrastructure/
│   │   └── interface/http/
│   ├── game/                   # Game catalog + bot-game assignment
│   ├── session/                # Session lifecycle (Phase 3)
│   ├── leaderboard/            # Score aggregation (Phase 4)
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
│   ├── pagination/             # Query param parsing + Meta
│   ├── response/               # Envelope helpers (Success/Error)
│   └── validator/              # go-playground/validator wrapper
├── migrations/                 # SQL migration files
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

# 2. Edit secrets (at minimum set a strong ADMIN_API_KEY and BOT_TOKEN_ENCRYPTION_KEY)
#    BOT_TOKEN_ENCRYPTION_KEY must be exactly 32 characters
vim .env.local

# 3. Start all services (API + PostgreSQL + Redis) with hot-reload
docker compose up
```

The API is available at `http://localhost:8080`. Editing any Go file triggers automatic rebuild within ~3 seconds.

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
| `DB_DSN` | **Yes** | — | PostgreSQL connection string |
| `REDIS_URL` | **Yes** | — | Redis connection URL |
| `ADMIN_API_KEY` | **Yes** | — | Bearer token for admin endpoints |
| `BOT_TOKEN_ENCRYPTION_KEY` | **Yes** | — | AES key for token encryption (32 chars) |
| `KBBI_MODE` | | `offline` | `offline` (embedded word list) or `api` |
| `KBBI_API_URL` | | — | KBBI API base URL (when `KBBI_MODE=api`) |
| `LOG_LEVEL` | | `info` | `debug`, `info`, `warn`, or `error` |
| `SESSION_TTL_HOURS` | | `168` | Hours before sessions are archived (7 days) |

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

### Endpoints

#### Health

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | None | Liveness — always 200 if process alive |
| GET | `/ready` | None | Readiness — 503 if PostgreSQL or Redis down |

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

#### Sessions *(Phase 3)*

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/bots/:bot_id/sessions` | Bot | Create a game session |
| GET | `/api/v1/bots/:bot_id/sessions` | Admin or Bot | List sessions |
| GET | `/api/v1/bots/:bot_id/sessions/:session_id` | Admin or Bot | Get session state |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/join` | Bot | Player joins session |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/start` | Bot | Host starts session |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/move` | Bot | Submit game move |
| POST | `/api/v1/bots/:bot_id/sessions/:session_id/end` | Admin or Bot | Force end session |

#### Leaderboard *(Phase 4)*

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/bots/:bot_id/leaderboard` | Admin or Bot | Bot leaderboard (all games) |
| GET | `/api/v1/bots/:bot_id/leaderboard/:game_id` | Admin or Bot | Bot leaderboard per game |
| GET | `/api/v1/leaderboard` | Admin or Bot | Global leaderboard |
| GET | `/api/v1/leaderboard/:game_id` | Admin or Bot | Global leaderboard per game |

Full schema definitions are in [`openapi.yaml`](openapi.yaml).

---

## Running Tests

```bash
# All unit tests
go test $(go list ./... | grep -v integration) -count=1

# Specific package
go test ./internal/games/uno/... -v

# Integration tests (requires TEST_DB_DSN and TEST_REDIS_URL)
TEST_DB_DSN="postgres://..." TEST_REDIS_URL="redis://..." \
  go test ./... -tags=integration -count=1
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

The production image is a multi-stage build (`golang:1.22-alpine` → `alpine:3.19`). The final image contains only the compiled binary — no source code. The API runs as a non-root user behind Nginx.

---

## Architecture

The project follows **Domain-Driven Design** with a modular monolith layout. Each domain (`bot`, `game`, `session`, `leaderboard`) owns its entities, repository interfaces, application services, and HTTP handlers. The `games/` package contains pure game engine implementations that are dependency-free and fully unit-testable.

Cross-domain dependencies are expressed as narrow interfaces rather than direct package imports, keeping each module independently testable.

### Security Design

- **Token storage** — Telegram bot tokens are AES-256-GCM encrypted at rest. A SHA-256 hash of the raw token is stored separately for O(1) BotAuth lookup without decrypting.
- **Token redaction** — `BotToken.MarshalJSON()` always returns `"[REDACTED]"`. Tokens never appear in logs, error messages, or response bodies.
- **Constant-time comparison** — `AdminAuth` middleware uses `crypto/subtle` for safe key comparison.

---

## Development Roadmap

| Phase | Status | Scope |
|-------|--------|-------|
| 0 — Scaffold | ✅ Done | Repo structure, shared packages, Docker, DB/Redis setup |
| 1 — Bot Management | ✅ Done | Bot CRUD, game catalog, game assignment |
| 2 — Game Engines | ✅ Done | Uno, Sambung Kata, Truth or Date |
| 3 — Session API | 🔲 Planned | Session lifecycle, move submission, state persistence |
| 4 — Leaderboard | 🔲 Planned | Score aggregation, Redis sorted sets |
| 5 — Hardening | 🔲 Planned | Rate limiting, Prometheus metrics, E2E tests, CI pipeline |

---

## License

[MIT](LICENSE) © 2026 404 Not Found Indonesia
