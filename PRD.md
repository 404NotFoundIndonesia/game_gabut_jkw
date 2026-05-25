# PRD: Game Gabut JKW — Bot Game Management API

**Version:** 2.0.0
**Date:** 2026-05-26
**Author:** M. Iqbal Effendi

---

## 1. Overview

Game Gabut JKW is a backend API that **directly handles Telegram webhook updates** from a main admin bot and multiple child game bots. The main bot is operated by the system admin and accepts management commands (`/addbot`, `/listbots`, `/assigngame`, etc.) to register and configure child bots. Child bots receive game commands (`/newgame`, `/join`, `/start`, `/move`) from Telegram users, who accumulate scores tracked on per-bot leaderboards. No external bot process is required — all Telegram interactions are handled inside this API.

### 1.1 Goals

- Single API controls N child Telegram bots.
- Admin can enable/disable games per bot without downtime.
- Three playable games with deterministic, testable game logic.
- Low-latency responses (<200 ms p99 for game state mutations).
- Full observability: structured logs, metrics, health endpoints.
- Dev/prod parity via Docker Compose.
- Direct Telegram webhook handling — no external bot process needed.
- Main admin bot with multi-step conversation FSM for bot registration and management.
- Child bot webhooks registered automatically when a bot is added; deregistered on removal.

### 1.2 Out of Scope (v2)

- Web dashboard UI.
- Payment or subscription logic.
- Push notifications outside Telegram.
- Multi-admin role hierarchy (single admin role).
- Telegram inline mode or callback query keyboards.
- Horizontal scaling of webhook handlers (single-instance assumed).

---

## 2. Stakeholders

| Role | Responsibility |
|------|---------------|
| Admin | Registers bots, assigns/removes games, views leaderboards via main bot commands |
| Telegram User (Player) | Plays games through child-bots |
| System (Main Bot) | Receives admin commands directly; triggers BotService and Telegram API calls |
| System (Child Bot) | Receives player game commands; routes to SessionService via webhook |

---

## 3. Functional Requirements

### 3.1 Bot Management

| ID | Requirement |
|----|-------------|
| BM-01 | Admin can register a new child bot (name, Telegram bot token). |
| BM-02 | Admin can deactivate / reactivate a child bot. |
| BM-03 | Admin can list all registered bots with status. |
| BM-04 | Each bot has a unique internal UUID and a Telegram `bot_id`. |

### 3.2 Game Assignment

| ID | Requirement |
|----|-------------|
| GA-01 | Admin can assign any available game to a bot. |
| GA-02 | Admin can revoke a game from a bot. |
| GA-03 | A bot can have multiple games assigned simultaneously. |
| GA-04 | Assigning an already-assigned game is idempotent (no error). |
| GA-05 | API returns the current game list for any bot. |

### 3.3 Games

Three games are available system-wide:

#### 3.3.1 Uno (ID: `uno`)

- Standard Uno card game, multi-player, turn-based.
- Sessions scoped to a Telegram group chat.
- Rules: draw pile, discard pile, action cards (Skip, Reverse, Draw Two, Wild, Wild Draw Four).
- Win condition: first player to empty hand.
- State persisted in DB; restorable after bot restart.

#### 3.3.2 Sambung Kata (ID: `sambung_kata`)

- Word-chaining game validated against KBBI (Kamus Besar Bahasa Indonesia).
- Each player submits a word starting with the last letter of the previous word.
- Word must exist in KBBI dictionary (offline dataset or API integration).
- Duplicate words in a session are rejected.
- Player eliminated on: invalid word, duplicate, or timeout.
- Score = words submitted before elimination.

#### 3.3.3 Truth or Date (ID: `truth_or_date`)

- Turn-based party game: player picks Truth or Date (Dare).
- System draws a random question/dare from a curated question bank.
- Player responses are free-text; not auto-judged (social game).
- Session tracks who answered what; host can skip or end session.
- Score = turns completed.

### 3.4 Leaderboard

| ID | Requirement |
|----|-------------|
| LB-01 | Leaderboard scoped per bot + per game. |
| LB-02 | Tracks: player Telegram ID, display name, score, games played, wins. |
| LB-03 | Returns top-N (default 10) players, configurable per request. |
| LB-04 | Leaderboard updates atomically at session end. |
| LB-05 | Global leaderboard (across all bots) also available. |

### 3.5 Game Session Lifecycle

```
CREATED → WAITING → IN_PROGRESS → FINISHED → ARCHIVED
```

- `CREATED`: session initialized, waiting for players to join.
- `WAITING`: min players joined, waiting for host start command.
- `IN_PROGRESS`: game running, accepting moves.
- `FINISHED`: win condition met, scores committed to leaderboard.
- `ARCHIVED`: session older than 7 days, read-only.

### 3.6 Admin Authentication

- REST admin endpoints authenticate via API key (Bearer token in `Authorization` header).
- API key rotatable without downtime.
- All REST admin endpoints require auth; player endpoints require only valid bot token context.
- Telegram admin commands (main bot) are authorized by `TELEGRAM_ADMIN_IDS` — a comma-separated list of Telegram user IDs. Commands from any other user are silently ignored or rejected with an error message.

### 3.7 Telegram Webhook Integration

| ID | Requirement |
|----|-------------|
| TW-01 | API exposes `POST /telegram/main/webhook` to receive updates from the main admin bot. |
| TW-02 | API exposes `POST /telegram/child/:bot_id/webhook` to receive updates from each child bot. |
| TW-03 | Each webhook request is validated via `X-Telegram-Bot-Api-Secret-Token` header (constant-time compare). |
| TW-04 | Main bot supports admin commands: `/addbot`, `/removebot`, `/reactivatebot`, `/listbots`, `/listgames`, `/listbotgames`, `/assigngame`, `/removegame`, `/leaderboard`, `/leaderboard global`. |
| TW-05 | `/addbot` uses a multi-step conversation FSM (`IDLE → AWAIT_TOKEN → AWAIT_NAME → DONE`) stored in Redis with a configurable TTL. |
| TW-06 | Child bot commands: `/newgame <game_slug>`, `/join`, `/start`, `/move <payload>`, `/end`, `/leaderboard`. |
| TW-07 | Chat-to-session index stored in Redis (`chat_session:<bot_id>:<chat_id>`) routes child bot commands to the active session. |
| TW-08 | When a child bot is registered via `/addbot`, the API calls Telegram `setWebhook` with the child's webhook URL automatically. |
| TW-09 | When a child bot is deleted via `/removebot`, the API calls Telegram `deleteWebhook` automatically. |
| TW-10 | On API startup, the main bot webhook is registered automatically via `setWebhook` using `WEBHOOK_BASE_URL`. |
| TW-11 | `/listgames` returns all system-wide available game slugs and descriptions so the admin knows valid values for `/assigngame`. |
| TW-12 | `/listbotgames <bot_id>` returns the games currently assigned to a specific bot. |
| TW-13 | `/reactivatebot <bot_id>` reactivates a previously deactivated bot and re-registers its Telegram webhook. |
| TW-14 | `/leaderboard global` (or `/leaderboard` with no args) returns the global leaderboard across all bots. |

---

## 4. Non-Functional Requirements

| Category | Requirement |
|----------|-------------|
| Latency | p99 < 200 ms for game state mutations; p99 < 50 ms for reads |
| Throughput | 500 concurrent game sessions per bot |
| Availability | 99.9% uptime per month |
| Scalability | Horizontal scale via stateless API + external DB/cache |
| Security | No secrets in logs; rate-limit per bot token; input sanitization |
| Observability | Structured JSON logs, Prometheus metrics endpoint, health check |
| Testability | Unit + integration test coverage ≥ 80%; all game logic unit-tested |

---

## 5. Architecture

### 5.1 Style: Domain-Driven Design (DDD) + Modular Monolith

Modules are separated by domain; internal interfaces, not HTTP calls. Extractable to microservices later without API changes.

```
bot-game-management/
├── cmd/
│   └── api/                    # entrypoint, wire DI
├── internal/
│   ├── bot/                    # Bot domain
│   │   ├── domain/             # entities, value objects, domain events
│   │   ├── application/        # use cases / services
│   │   ├── infrastructure/     # repo implementations, external adapters
│   │   └── interface/          # HTTP handlers
│   ├── game/                   # Game catalog domain
│   │   ├── domain/
│   │   ├── application/
│   │   ├── infrastructure/
│   │   └── interface/
│   ├── session/                # Game session domain
│   │   ├── domain/
│   │   ├── application/
│   │   ├── infrastructure/
│   │   └── interface/
│   ├── leaderboard/            # Leaderboard domain
│   │   ├── domain/
│   │   ├── application/
│   │   ├── infrastructure/
│   │   └── interface/
│   ├── games/                  # Game engine implementations
│   │   ├── uno/
│   │   ├── sambung_kata/
│   │   └── truth_or_date/
│   ├── webhook/                # Telegram webhook handlers (Phase 6)
│   │   ├── main_handler.go     # main bot update handler + admin commands + FSM
│   │   └── child_handler.go    # child bot update handler + game command routing
│   ├── telegram/               # Telegram client & update types
│   │   ├── client.go           # GetMe, SetWebhook, DeleteWebhook, SendMessage
│   │   └── update.go           # Update, Message, User types
│   └── middleware/             # HTTP middleware (auth, rate-limit, metrics)
├── pkg/                        # Shared, dependency-free utilities
│   ├── logger/
│   ├── validator/
│   ├── pagination/
│   └── errors/
├── migrations/                 # SQL migration files
├── docker/
│   ├── Dockerfile.dev
│   └── Dockerfile.prod
├── docker-compose.yml          # dev
├── docker-compose.prod.yml     # prod
└── .env.example
```

### 5.2 Technology Stack

| Layer | Choice | Reason |
|-------|--------|--------|
| Language | Go 1.22+ | Low latency, strong concurrency, easy containerization |
| HTTP Framework | Fiber v2 or Chi | Low-overhead routing |
| Database | PostgreSQL 16 | ACID, JSONB for game state |
| Cache | Redis 7 | Session state, rate limiting, leaderboard hot-path |
| Migration | golang-migrate | Version-controlled schema |
| Test | testify + gomock | Standard Go testing ecosystem |
| Metrics | Prometheus + promhttp | Standard scrape target |
| Container | Docker + Docker Compose | Dev/prod parity |

### 5.3 Data Flow

```
Admin (Telegram message)
    ↓
Main Bot (Telegram) ──→ POST /telegram/main/webhook
    ↓
Main Bot Handler (FSM + admin commands)
    ├── BotService.RegisterBotWithWebhook → PostgreSQL
    │   └── Telegram API: setWebhook(child bot)
    ├── BotService.ListBots / DeleteBotWithWebhook
    └── GameService.AssignGame / RemoveGame

Telegram User (Telegram message)
    ↓
Child Bot (Telegram) ──→ POST /telegram/child/:bot_id/webhook
    ↓
Child Bot Handler (game command routing)
    ├── Redis: chat_session:<bot_id>:<chat_id> → session lookup
    ├── SessionService (newgame / join / start / move / end)
    │   ├── Game Engine (Uno / Sambung Kata / Truth or Date)
    │   └── Redis (session state cache)
    └── LeaderboardService → PostgreSQL

Startup
    └── Telegram API: setWebhook(main bot, WEBHOOK_BASE_URL/telegram/main/webhook)
```

---

## 6. API Design

Base URL: `/api/v1`

All endpoints return:
```json
{ "success": true, "data": {}, "error": null, "meta": {} }
```

### 6.1 Bot Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/bots` | Register bot |
| GET | `/bots` | List all bots |
| GET | `/bots/:id` | Get bot detail |
| PATCH | `/bots/:id` | Update bot |
| DELETE | `/bots/:id` | Deactivate bot |
| POST | `/bots/:id/games` | Assign game to bot |
| DELETE | `/bots/:id/games/:game_id` | Remove game from bot |
| GET | `/bots/:id/games` | List games assigned to bot |

### 6.2 Game Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/games` | List all available games |
| GET | `/games/:id` | Get game detail |

### 6.3 Session Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/bots/:bot_id/sessions` | Create session for game |
| GET | `/bots/:bot_id/sessions/:id` | Get session state |
| POST | `/bots/:bot_id/sessions/:id/join` | Player joins session |
| POST | `/bots/:bot_id/sessions/:id/start` | Host starts session |
| POST | `/bots/:bot_id/sessions/:id/move` | Submit game move |
| POST | `/bots/:bot_id/sessions/:id/end` | Force end session |
| GET | `/bots/:bot_id/sessions` | List sessions (filterable by status) |

### 6.4 Leaderboard Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/bots/:bot_id/leaderboard` | Per-bot leaderboard (all games) |
| GET | `/bots/:bot_id/leaderboard/:game_id` | Per-bot per-game leaderboard |
| GET | `/leaderboard` | Global leaderboard |
| GET | `/leaderboard/:game_id` | Global per-game leaderboard |

### 6.5 Health / Metrics

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness check |
| GET | `/ready` | Readiness check (DB + Redis) |
| GET | `/metrics` | Prometheus scrape endpoint |

### 6.6 Telegram Webhook Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/telegram/main/webhook` | Receive updates from main admin bot |
| POST | `/telegram/child/:bot_id/webhook` | Receive updates from a registered child game bot |

No `Authorization` header — secured by `X-Telegram-Bot-Api-Secret-Token` header (constant-time validated against `WEBHOOK_SECRET_TOKEN`). Telegram delivers these automatically; external callers must not use these endpoints.

---

## 7. Domain Models

### Bot

```
Bot {
  id          UUID
  name        string
  token       string (encrypted at rest)
  telegram_id int64
  active      bool
  created_at  timestamp
  updated_at  timestamp
}
```

### Game

```
Game {
  id          UUID
  slug        string  // uno | sambung_kata | truth_or_date
  name        string
  description string
  min_players int
  max_players int
}
```

### BotGame (assignment)

```
BotGame {
  bot_id      UUID
  game_id     UUID
  assigned_at timestamp
}
```

### GameSession

```
GameSession {
  id          UUID
  bot_id      UUID
  game_id     UUID
  chat_id     int64   // Telegram chat/group ID
  status      enum    // CREATED | WAITING | IN_PROGRESS | FINISHED | ARCHIVED
  state       JSONB   // game-specific state
  players     []PlayerSession
  started_at  timestamp?
  ended_at    timestamp?
  created_at  timestamp
}
```

### PlayerSession

```
PlayerSession {
  session_id      UUID
  telegram_user_id int64
  display_name    string
  score           int
  is_winner       bool
  joined_at       timestamp
}
```

### LeaderboardEntry

```
LeaderboardEntry {
  id               UUID
  bot_id           UUID
  game_id          UUID
  telegram_user_id int64
  display_name     string
  total_score      int
  games_played     int
  wins             int
  updated_at       timestamp
}
```

### ConversationState (Redis only)

```
ConversationState {
  telegram_user_id  int64
  state             enum    // IDLE | AWAIT_TOKEN | AWAIT_NAME | DONE
  data              map     // partial data accumulated across FSM steps
  expires_at        timestamp
}
```

Redis key: `conv:main:<telegram_user_id>` · TTL: `CONV_STATE_TTL_MINUTES` (default 10 min).

---

## 8. Game Engine Contracts

Each game engine implements:

```go
type GameEngine interface {
    // Initialize fresh game state for a new session.
    Init(players []Player, opts map[string]any) (State, error)

    // Apply a player move; return updated state and events.
    Apply(state State, move Move) (State, []Event, error)

    // Check if game is over; return winners and final scores.
    Evaluate(state State) (finished bool, result Result)

    // Validate a move without mutating state.
    Validate(state State, move Move) error
}
```

State is serializable to/from JSONB. All game logic is pure functions — no I/O. Enables deterministic unit testing without DB.

---

## 9. Testing Strategy (TDD)

### Layers

| Layer | Type | Tool |
|-------|------|------|
| Game engines | Unit test | `testing` + `testify` |
| Use cases / application | Unit test with mocked repos | `gomock` |
| HTTP handlers | Integration test with test DB | `httptest` + real DB |
| Full flow | E2E (optional, CI-gated) | Docker Compose test env |

### Coverage Targets

- Game engine logic: **100%**
- Application layer (use cases): **≥ 90%**
- Infrastructure / handlers: **≥ 70%**
- Overall: **≥ 80%**

### TDD Cycle

Write failing test → implement minimum code → pass → refactor → repeat.

Game engines built test-first: define all valid moves, invalid moves, edge cases before writing engine code.

---

## 10. KBBI Integration (Sambung Kata)

Two options, configurable at startup:

| Option | Description | Tradeoff |
|--------|-------------|----------|
| Offline dataset | Pre-loaded KBBI word list as in-memory set | Zero latency, needs periodic update |
| External API | HTTP call to third-party KBBI API | Always fresh, adds latency |

Default: offline dataset. Loaded at boot, configurable via `KBBI_MODE=offline|api` env var.

---

## 11. Docker Setup

### Development (`docker-compose.yml`)

```yaml
services:
  api:      # hot-reload via Air
  postgres: # persistent volume
  redis:    # persistent volume
```

- Mounts source code into container.
- Uses `Dockerfile.dev` with `air` for hot reload.
- Exposes ports locally: API `8080`, Postgres `5432`, Redis `6379`.
- `.env` loaded from `.env.local`.

### Production (`docker-compose.prod.yml`)

```yaml
services:
  api:      # multi-stage build, minimal alpine image
  postgres: # prod volume, no port exposed externally
  redis:    # prod volume, no port exposed externally
  nginx:    # reverse proxy, TLS termination
```

- Multi-stage `Dockerfile.prod`: builder stage (Go compile) + runtime stage (alpine, binary only).
- No source code in prod image.
- Secrets via env file or Docker secrets.
- Health check configured for all services.

---

## 12. Configuration

All config via environment variables. No config files in code.

| Variable | Description | Default |
|----------|-------------|---------|
| `APP_ENV` | `development` / `production` | `development` |
| `APP_PORT` | HTTP listen port | `8080` |
| `DB_DSN` | PostgreSQL connection string | — |
| `REDIS_URL` | Redis connection URL | — |
| `ADMIN_API_KEY` | Management bot API key | — |
| `BOT_TOKEN_ENCRYPTION_KEY` | AES key for token encryption | — |
| `KBBI_MODE` | `offline` / `api` | `offline` |
| `KBBI_API_URL` | KBBI API base URL (if mode=api) | — |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | `info` |
| `SESSION_TTL_HOURS` | Hours before session archived | `168` (7d) |
| `MAIN_BOT_TOKEN` | Telegram token for the main admin bot | — |
| `TELEGRAM_ADMIN_IDS` | Comma-separated Telegram user IDs authorized for admin commands | — |
| `WEBHOOK_BASE_URL` | Public base URL where Telegram delivers webhooks (e.g. `https://api.example.com`) | — |
| `WEBHOOK_SECRET_TOKEN` | Secret for `X-Telegram-Bot-Api-Secret-Token` header validation | — |
| `CONV_STATE_TTL_MINUTES` | Redis TTL for conversation FSM state | `10` |

---

## 13. Milestones

| Phase | Deliverable | Scope |
|-------|-------------|-------|
| 0 | Project scaffold | Repo structure, Docker dev setup, CI skeleton, DB migration runner |
| 1 | Bot management | Bot CRUD, game catalog seeding, game assignment |
| 2 | Game engines | Uno, Sambung Kata, Truth or Date — fully unit-tested |
| 3 | Session API | Session lifecycle, move submission, state persistence |
| 4 | Leaderboard | Score aggregation, per-bot and global leaderboards |
| 5 | Hardening | Rate limiting, metrics, health checks, prod Docker, E2E tests |
| 6 | Telegram Webhook Integration | Main bot handler + admin commands + FSM, child bot handler + game commands, auto webhook registration, chat-session routing |

---

## 14. Risks

| Risk | Mitigation |
|------|-----------|
| KBBI dataset completeness | Ship with verified offline dataset; fallback to API |
| Uno state complexity | State machine diagram before implementation; 100% unit test coverage |
| Session concurrency (Redis race) | Lua scripts or Redis transactions for atomic state updates |
| Bot token leakage | Encrypt tokens at rest; never log tokens |
| Large leaderboard query latency | Redis sorted set for hot leaderboard; DB as source of truth |
| Webhook secret mismatch | Generate `WEBHOOK_SECRET_TOKEN` once; stored in `.env`; same value used for all child bot `setWebhook` calls |
| Telegram webhook replay / duplicate delivery | Track `message_id` per bot in Redis; idempotent session operations absorb duplicates |
| FSM state corruption (partial `/addbot` flow) | Short `CONV_STATE_TTL_MINUTES` TTL cleans up stale state; explicit DONE/CANCEL transitions clear immediately |
| Main bot token compromise | Token stored in `.env` only; never persisted to DB; rotate by updating env + restarting API |
