# PRD: Game Gabut JKW — Bot Game Management API

**Version:** 1.0.0
**Date:** 2026-05-19
**Author:** M. Iqbal Effendi

---

## 1. Overview

Game Gabut JKW is a backend API that acts as management center for multiple Telegram bots. One central "management bot" connects to this API; admins use it to assign games to registered child-bots. Telegram users who interact with any child-bot can then discover and play those assigned games, and accumulate scores tracked on per-bot leaderboards.

### 1.1 Goals

- Single API controls N child Telegram bots.
- Admin can enable/disable games per bot without downtime.
- Three playable games with deterministic, testable game logic.
- Low-latency responses (<200 ms p99 for game state mutations).
- Full observability: structured logs, metrics, health endpoints.
- Dev/prod parity via Docker Compose.

### 1.2 Out of Scope (v1)

- Web dashboard UI.
- Payment or subscription logic.
- Push notifications outside Telegram.
- Multi-admin role hierarchy (single admin role).

---

## 2. Stakeholders

| Role | Responsibility |
|------|---------------|
| Admin | Registers bots, assigns/removes games, views leaderboards |
| Telegram User (Player) | Plays games through child-bots |
| System (Management Bot) | Receives admin commands, relays to API |

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

- Management bot authenticates to API via API key (Bearer token in header).
- API key rotatable without downtime.
- All admin endpoints require auth; player endpoints require only valid bot token context.

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
│   └── games/                  # Game engine implementations
│       ├── uno/
│       ├── sambung_kata/
│       └── truth_or_date/
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
Telegram User
    ↓ (command)
Child Bot (Telegram)
    ↓ (webhook / polling)
[External bot process — out of scope]
    ↓ (REST call with bot API key)
Game Management API
    ├── Game Session Service
    │   ├── Game Engine (Uno / Sambung Kata / Truth or Date)
    │   └── Redis (session state cache)
    ├── Leaderboard Service → PostgreSQL
    └── Bot Service → PostgreSQL
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

---

## 14. Risks

| Risk | Mitigation |
|------|-----------|
| KBBI dataset completeness | Ship with verified offline dataset; fallback to API |
| Uno state complexity | State machine diagram before implementation; 100% unit test coverage |
| Session concurrency (Redis race) | Lua scripts or Redis transactions for atomic state updates |
| Bot token leakage | Encrypt tokens at rest; never log tokens |
| Large leaderboard query latency | Redis sorted set for hot leaderboard; DB as source of truth |
