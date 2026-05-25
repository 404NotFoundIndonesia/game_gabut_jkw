# TASKS: Game Gabut JKW — Bot Game Management API

Task breakdown derived from `PRD.md` and `openapi.yaml`.
Tasks are ordered by dependency — complete phases in order.
Each task is self-contained and verifiable.

Legend: `[unit]` = unit tests required · `[int]` = integration test required · `[e2e]` = end-to-end test required

---

## Phase 0 — Project Scaffold

### T-00-01 · Go module & folder structure
- [x] Init Go module (`go mod init`)
- [x] Create full DDD folder tree per PRD §5.1:
  `cmd/api/`, `internal/{bot,game,session,leaderboard}/`, `internal/games/{uno,sambung_kata,truth_or_date}/`, `pkg/{logger,validator,pagination,errors}/`, `migrations/`, `docker/`
- [x] Add `.gitignore` (Go standard + `.env*`)
- [x] Add `.env.example` with all vars from PRD §12
- **DoD:** `go build ./...` succeeds from repo root; folder tree matches PRD §5.1 exactly

---

### T-00-02 · Shared packages — `pkg/errors`
- [x] Define typed `AppError` struct: `code string`, `message string`, `details []FieldError`, `httpStatus int`
- [x] Define sentinel error codes: `NOT_FOUND`, `VALIDATION_ERROR`, `UNAUTHORIZED`, `FORBIDDEN`, `CONFLICT`, `UNPROCESSABLE`, `INTERNAL_ERROR`
- [x] Helper constructors: `NotFound()`, `Validation()`, `Conflict()`, `Unprocessable()`, `Internal()`
- **DoD:** All constructors return correct `httpStatus`; error unwrapping works
- **Tests [unit]:** One test per constructor verifying code, message, status

---

### T-00-03 · Shared packages — `pkg/logger`
- [x] Structured JSON logger wrapping `log/slog`
- [x] Log levels controlled by `LOG_LEVEL` env var
- [x] Fields: `level`, `time`, `msg`, plus arbitrary key-value pairs
- [x] Never log values tagged as sensitive (token, password, key)
- **DoD:** Logger outputs valid JSON; `LOG_LEVEL=debug` shows debug lines; `LOG_LEVEL=warn` suppresses info
- **Tests [unit]:** Verify output is valid JSON; verify level filtering

---

### T-00-04 · Shared packages — `pkg/validator`
- [x] Thin wrapper around `github.com/go-playground/validator/v10`
- [x] Returns `[]FieldError{Field, Message}` on failure
- [x] Integrates with `pkg/errors` `Validation()` constructor
- **DoD:** Validates struct tags; returns field-level errors matching `ApiError.details` schema in `openapi.yaml`
- **Tests [unit]:** Valid struct → no error; missing required field → FieldError with correct field name

---

### T-00-05 · Shared packages — `pkg/pagination`
- [x] `Params` struct: `Limit int`, `Offset int`
- [x] `ParseFromQuery(limit, offset string) (Params, error)` — defaults: limit=10, max=100
- [x] `Meta` struct matching `openapi.yaml` `Meta` schema: `Total`, `Limit`, `Offset`
- **DoD:** Out-of-range limit/offset returns validation error; defaults applied when params absent
- **Tests [unit]:** Test defaults, max clamp, negative offset rejection

---

### T-00-06 · HTTP response envelope helper
- [x] `pkg/response` package
- [x] `Success(data any, meta *Meta) EnvelopeResponse` — `success: true`
- [x] `Error(err *AppError) EnvelopeResponse` — `success: false`
- [x] JSON marshalling matches `openapi.yaml` `SuccessEnvelope` / `ErrorEnvelope` exactly
- **DoD:** Marshal output matches spec shape; null `meta` omitted when not provided
- **Tests [unit]:** Marshal both envelope types; compare JSON field presence

---

### T-00-07 · Config loader
- [x] `internal/config` package reads all env vars from PRD §12
- [x] Uses stdlib `os.Getenv` with explicit defaults — no extra dependency
- [x] Fails fast at boot if required vars (`DB_DSN`, `REDIS_URL`, `ADMIN_API_KEY`, `BOT_TOKEN_ENCRYPTION_KEY`) are absent
- **DoD:** Missing required var → process exits with clear error; optional vars use defaults from PRD §12
- **Tests [unit]:** Test default values; test required-var absence triggers error

---

### T-00-08 · Database connection + migration runner
- [x] PostgreSQL connection via `pgx/v5` pool; pool settings configurable via `DB_DSN`
- [x] `golang-migrate` wired to `migrations/` folder; runs on startup before HTTP server starts
- [x] Readiness check waits for migration to finish
- [x] Migration files use timestamp prefix: `YYYYMMDDHHMMSS_<name>.up.sql` / `.down.sql`
- **DoD:** `docker compose up` runs migrations automatically; `go test` can run against test DB; `migrate down` rolls back cleanly
- **Tests [int]:** Migration up/down on empty DB succeeds without error

---

### T-00-09 · Redis connection
- [x] Redis client via `github.com/redis/go-redis/v9`
- [x] Ping on startup; surface in readiness probe
- [x] Connection pool size configurable (default 10)
- **DoD:** Client connects; ping succeeds; readiness endpoint reflects Redis status
- **Tests [int]:** Ping succeeds against test Redis instance

---

### T-00-10 · HTTP server bootstrap
- [x] Wire HTTP server (`github.com/gofiber/fiber/v2`) in `cmd/api/main.go`
- [x] Graceful shutdown: `SIGTERM`/`SIGINT` → drain in-flight requests → exit
- [x] Global error handler maps `*AppError` → envelope response with correct HTTP status
- [x] Request ID middleware (UUID per request, added to `X-Request-ID` response header and logger context)
- **DoD:** Server starts; SIGTERM triggers graceful shutdown; unhandled panic returns 500 envelope

---

### T-00-11 · Docker — development
- [x] `docker/Dockerfile.dev`: base `golang:1.22-alpine`, install `air`, copy source, hot-reload command
- [x] `docker-compose.yml`: services `api` (Dockerfile.dev + volume mount), `postgres:16-alpine`, `redis:7-alpine`
- [x] Ports: api=8080, postgres=5432, redis=6379
- [x] Health checks on postgres and redis services
- [x] `.env.local` loaded by compose (`env_file`)
- **DoD:** `docker compose up` starts all three services; editing a Go file triggers rebuild within 3 seconds; `GET /health` returns 200

---

### T-00-12 · Docker — production
- [x] `docker/Dockerfile.prod`: multi-stage — stage 1 `golang:1.22-alpine` compiles binary; stage 2 `alpine:3.19` copies binary only
- [x] `docker-compose.prod.yml`: services `api`, `postgres:16-alpine`, `redis:7-alpine`, `nginx:alpine`
- [x] Postgres and Redis ports NOT exposed externally in prod compose
- [x] Nginx reverse-proxies to api on internal network
- [x] Health check: `CMD wget -qO- http://localhost:8080/health || exit 1`
- **DoD:** `docker compose -f docker-compose.prod.yml build` produces image < 30 MB; no source code in image; `GET /health` returns 200 via nginx

---

### T-00-13 · Health & readiness endpoints
- [x] `GET /health` → 200 `{ success: true, data: { status: "ok" } }` (always, if process alive)
- [x] `GET /ready` → 200 if postgres + redis reachable; 503 if either down; body includes per-dependency status per `openapi.yaml` `ReadinessStatus` schema
- [x] No auth required on either endpoint
- **DoD:** Response shapes match `openapi.yaml` exactly; killing postgres → `/ready` returns 503 with `postgres: "down"`
- **Tests [int]:** Mock DB down → 503; both up → 200

---

## Phase 1 — Bot Management

### T-01-01 · Bot domain entity
- [x] `internal/bot/domain/bot.go`: `Bot` struct with all fields from PRD §7
- [x] Value object `BotToken` — wraps encrypted string; never exposes raw token in JSON
- [x] Domain method `Bot.Deactivate()` sets `active = false`, updates `updated_at`
- [x] Domain method `Bot.Activate()` sets `active = true`, updates `updated_at`
- [x] Domain method `Bot.RotateToken(newToken BotToken)` replaces token, updates `updated_at`
- **DoD:** All fields exported; `BotToken` MarshalJSON returns `"[REDACTED]"` always
- **Tests [unit]:** `Deactivate()` sets active=false; `MarshalJSON` on BotToken returns redacted string

---

### T-01-02 · Bot token encryption
- [x] `pkg/crypto` package: AES-256-GCM encrypt/decrypt using `BOT_TOKEN_ENCRYPTION_KEY`
- [x] `Encrypt(plaintext string) (ciphertext string, err error)`
- [x] `Decrypt(ciphertext string) (plaintext string, err error)`
- [x] Ciphertext base64url-encoded for safe DB storage
- **DoD:** Decrypt(Encrypt(x)) == x; wrong key → error; ciphertext never equals plaintext
- **Tests [unit]:** Round-trip; wrong key; empty string; long token

---

### T-01-03 · Bot repository interface & DB migration
- [x] `internal/bot/domain/repository.go`: `BotRepository` interface
  - `Save(ctx, bot) error`
  - `FindByID(ctx, id uuid.UUID) (*Bot, error)`
  - `FindByTelegramID(ctx, telegramID int64) (*Bot, error)`
  - `FindAll(ctx, filter BotFilter) ([]*Bot, int, error)`
  - `Delete(ctx, id uuid.UUID) error`
- [x] Migration `000001_create_bots.up.sql`: table `bots` with columns matching `Bot` domain entity; `token` column stores encrypted ciphertext; index on `telegram_id`
- **DoD:** Migration runs clean up/down; interface covers all use-case needs

---

### T-01-04 · Bot PostgreSQL repository
- [x] `internal/bot/infrastructure/postgres_bot_repository.go` implements `BotRepository`
- [x] `Save` upserts (insert or update on `id`)
- [x] `FindAll` supports filter by `active bool` + pagination (`LIMIT/OFFSET`)
- [x] All queries use `pgx` named args; no string-formatted SQL
- **DoD:** All interface methods implemented; returns `NotFound` AppError when row absent
- **Tests [int]:** CRUD lifecycle against real test DB; `FindAll` with active filter; pagination counts

---

### T-01-05 · Bot use cases
- [x] `internal/bot/application/bot_service.go`
- [x] `RegisterBot(ctx, name, rawToken string) (*Bot, error)` — encrypt token, derive `telegram_id` by calling Telegram `getMe` API, persist
- [x] `ListBots(ctx, filter, pagination) ([]*Bot, int, error)`
- [x] `GetBot(ctx, id) (*Bot, error)`
- [x] `UpdateBot(ctx, id, patch UpdateBotPatch) (*Bot, error)` — partial update: name, active, token rotation
- [x] `DeleteBot(ctx, id) (*Bot, error)` — deactivate + delete; force-end any active sessions (via session service interface)
- **DoD:** Each use case covers happy path + not-found + validation errors
- **Tests [unit]:** Mock `BotRepository`; test each use case: register with dup telegram_id → Conflict; update non-existent → NotFound; delete → deactivated

---

### T-01-06 · Bot HTTP handlers
- [x] `internal/bot/interface/http/bot_handler.go`
- [x] Wire all 5 bot endpoints per `openapi.yaml`:
  - `POST /api/v1/bots` → `createBot`
  - `GET /api/v1/bots` → `listBots` (query params: limit, offset, active)
  - `GET /api/v1/bots/:bot_id` → `getBot`
  - `PATCH /api/v1/bots/:bot_id` → `updateBot`
  - `DELETE /api/v1/bots/:bot_id` → `deleteBot`
- [x] All endpoints require `AdminApiKey` auth (middleware applied at router level)
- [x] Responses match `openapi.yaml` envelope exactly; HTTP status codes per spec
- **DoD:** All 5 endpoints return correct shapes; 401 when no/invalid key; 404 for missing bot
- **Tests [int]:** Each endpoint: happy path, missing auth → 401, missing resource → 404, bad body → 400

---

### T-01-07 · Admin auth middleware
- [x] `internal/middleware/auth.go`
- [x] `AdminAuth` middleware: reads `Authorization: Bearer <token>` header; compares constant-time to `ADMIN_API_KEY`; returns 401 envelope if mismatch
- [x] `BotAuth` middleware: reads `X-Bot-Token` header; looks up bot by token in DB; injects `*Bot` into request context; returns 401 if invalid
- **DoD:** Wrong key → 401; missing header → 401; valid key → handler called
- **Tests [unit]:** Mock next handler; test both middlewares for valid/invalid/missing token

---

### T-01-08 · Game catalog domain & seeding
- [x] `internal/game/domain/game.go`: `Game` struct per PRD §7
- [x] `GameSlug` type with constants: `SlugUno`, `SlugSambungKata`, `SlugTruthOrDate`
- [x] `internal/game/domain/repository.go`: `GameRepository` interface
  - `FindAll(ctx) ([]*Game, error)`
  - `FindByID(ctx, id) (*Game, error)`
  - `FindBySlug(ctx, slug GameSlug) (*Game, error)`
- [x] Migration `000002_create_games.up.sql`: table `games`; seed 3 rows (uno, sambung_kata, truth_or_date) with UUIDs, min/max players
- **DoD:** Migration seeds exactly 3 games; UUIDs stable across re-runs (hardcoded in migration)

---

### T-01-09 · Game PostgreSQL repository & HTTP handlers
- [x] `internal/game/infrastructure/postgres_game_repository.go` implements `GameRepository`
- [x] `internal/game/interface/http/game_handler.go`
- [x] Wire 2 endpoints per `openapi.yaml`:
  - `GET /api/v1/games` → `listGames`
  - `GET /api/v1/games/:game_id` → `getGame`
- [x] Both endpoints accessible by `AdminApiKey` OR `BotApiKey`
- **DoD:** List returns all 3 seeded games; get returns single game; 404 for unknown ID
- **Tests [int]:** List → 3 results; get by valid ID → 200; get by unknown ID → 404

---

### T-01-10 · Bot-Game assignment domain & migration
- [x] `internal/game/domain/bot_game.go`: `BotGame` struct per PRD §7
- [x] `BotGameRepository` interface:
  - `Assign(ctx, botID, gameID uuid.UUID) error` — upsert (idempotent)
  - `Remove(ctx, botID, gameID uuid.UUID) error`
  - `FindByBot(ctx, botID uuid.UUID) ([]*BotGame, error)`
  - `ExistsByBotAndGame(ctx, botID, gameID uuid.UUID) (bool, error)`
- [x] Migration `000003_create_bot_games.up.sql`: junction table `bot_games(bot_id, game_id, assigned_at)`; composite PK; FKs to `bots` and `games`
- **DoD:** Migration up/down clean; composite PK prevents duplicates at DB level

---

### T-01-11 · Bot-Game use cases & HTTP handlers
- [x] `internal/game/application/bot_game_service.go`
  - `AssignGame(ctx, botID, gameID) (*BotGame, error)` — verify both exist; upsert
  - `RemoveGame(ctx, botID, gameID) error` — verify both exist; delete
  - `ListBotGames(ctx, botID) ([]*BotGame, error)`
- [x] `internal/game/interface/http/bot_game_handler.go`
- [x] Wire 3 endpoints per `openapi.yaml`:
  - `POST /api/v1/bots/:bot_id/games` → `assignGame`
  - `DELETE /api/v1/bots/:bot_id/games/:game_id` → `removeGame`
  - `GET /api/v1/bots/:bot_id/games` → `listBotGames`
- [x] `assignGame`: 404 if bot or game not found; 200 (not 201) if already assigned
- [x] `removeGame`: 204 on success; 404 if assignment not found
- **DoD:** Idempotent assign; correct status codes per spec; responses match `BotGame` schema
- **Tests [unit]:** Mock repos; assign → upsert called; assign non-existent game → NotFound
- **Tests [int]:** Assign, re-assign → 200 both times; remove → 204; list shows assigned games only

---

## Phase 2 — Game Engines

### T-02-01 · GameEngine interface
- [x] `internal/games/engine.go`: define shared types and interface
  ```go
  type Player struct { TelegramUserID int64; DisplayName string }
  type Move  struct { PlayerID int64; Payload map[string]any }
  type Event struct { Type string; Payload map[string]any }
  type Result struct { Winners []Player; Scores map[int64]int }
  type GameEngine interface {
      Init(players []Player, opts map[string]any) (json.RawMessage, error)
      Apply(state json.RawMessage, move Move) (json.RawMessage, []Event, error)
      Evaluate(state json.RawMessage) (bool, Result, error)
      Validate(state json.RawMessage, move Move) error
  }
  ```
- [x] State is always `json.RawMessage` (serializable to JSONB)
- [x] All methods are pure — no I/O, no side effects
- **DoD:** Interface compiles; all three engines will implement this interface

---

### T-02-02 · Uno engine
- [x] `internal/games/uno/engine.go` implements `GameEngine`
- [x] `Init`: build 108-card deck, shuffle, deal `hand_size` (default 7) cards per player, flip first discard, set turn order
- [x] State shape: `{ draw_pile, discard_pile, hands, current_turn_idx, direction, pending_draw, status }`
- [x] `Validate` rules:
  - Only current player can move
  - `play_card`: card must be in hand; matches top discard by color or value, OR is Wild
  - Wild/Wild Draw Four: `chosen_color` required
  - `draw`: only allowed when no playable card (or always, configurable)
- [x] `Apply` rules:
  - Skip → advance 2 turns
  - Reverse → flip `direction`
  - Draw Two → next player draws 2, loses turn
  - Wild Draw Four → next player draws 4, loses turn; validate caller has no matching color card
  - Player plays last card → emit `PLAYER_WON` event
- [x] `Evaluate`: game over when any player hand empty; winner = that player
- [x] Events: `CARD_PLAYED`, `CARD_DRAWN`, `TURN_SKIPPED`, `DIRECTION_REVERSED`, `PLAYER_WON`, `GAME_OVER`
- **DoD:** Full rules implemented; no I/O; state always serializable
- **Tests [unit]:**
  - Init: correct card count (108), correct deal
  - Play card: valid → state updated; wrong player → error; wrong card → error
  - Skip, Reverse, Draw Two, Wild, Wild Draw Four: each action produces correct state
  - Wild Draw Four: caller has matching color → error
  - Win condition: player empties hand → GAME_OVER event emitted
  - Evaluate: returns finished=true + correct winner

---

### T-02-03 · Sambung Kata engine + KBBI integration
- [x] `internal/games/sambung_kata/engine.go` implements `GameEngine`
- [x] `internal/games/sambung_kata/kbbi/` — KBBI validator
  - Interface: `type Validator interface { IsValid(word string) bool }`
  - `OfflineValidator`: loads word list from embedded file at startup (`//go:embed kbbi.txt`)
  - `APIValidator`: HTTP call to external KBBI API; configurable URL; 5s timeout
  - Selected by `KBBI_MODE` env at boot
- [x] State shape: `{ last_word, used_words []string, current_turn_idx, eliminated []int64, timeout_seconds, status }`
- [x] `Validate` rules:
  - Only current player can move
  - `word` must start with last letter of `last_word` (case-insensitive)
  - `word` must not be in `used_words`
  - `word` must pass KBBI validator
- [x] `Apply`: add word to used_words; advance turn; if invalid → emit `PLAYER_ELIMINATED` for current player
- [x] `Evaluate`: one player left → game over; winner = last remaining player; score = words submitted
- [x] Events: `WORD_ACCEPTED`, `WORD_REJECTED`, `PLAYER_ELIMINATED`, `GAME_OVER`
- **DoD:** Offline validator loads without network; API validator interface-compatible; pure engine logic
- **Tests [unit]:**
  - Valid word starting with correct letter, in KBBI → accepted
  - Word starting with wrong letter → rejected
  - Duplicate word → rejected
  - Not in KBBI → rejected (mock Validator)
  - Last player standing → GAME_OVER
  - OfflineValidator: known word → true; unknown word → false
  - APIValidator: mock HTTP → correct call made

---

### T-02-04 · Truth or Date engine + question bank
- [x] `internal/games/truth_or_date/engine.go` implements `GameEngine`
- [x] `internal/games/truth_or_date/questions/` — embedded question bank (`//go:embed questions.json`)
  - Format: `{ "truth": ["...", ...], "date": ["...", ...] }` — minimum 50 questions each
- [x] State shape: `{ current_turn_idx, round, responses []Response, current_question, status }`
  - `Response`: `{ player_id, choice, question, answer, skipped }`
- [x] `Validate` rules:
  - Only current player can move
  - `choice` action: valid values `"truth"` or `"date"`
  - `answer` action: current player must have pending question
  - `skip` action: host only
- [x] `Apply`:
  - `choice` → draw random question of that type; store on state
  - `answer` → record free-text answer; advance turn; emit `ANSWER_RECORDED`
  - `skip` → mark skipped; advance turn; emit `QUESTION_SKIPPED`
- [x] `Evaluate`: game has no automatic end; only ends via `endSession`; score = turns completed
- [x] Events: `QUESTION_DRAWN`, `ANSWER_RECORDED`, `QUESTION_SKIPPED`
- **DoD:** Questions embedded in binary; question draw is deterministic when seeded (testable)
- **Tests [unit]:**
  - Choice truth → question drawn from truth bank
  - Choice date → question drawn from date bank
  - Answer recorded → response stored on state
  - Skip by non-host → error
  - Skip by host → marked skipped, turn advances
  - Score = number of turns completed

---

### T-02-05 · Game engine registry
- [x] `internal/games/registry.go`: `Registry` maps `GameSlug → GameEngine`
- [x] `NewRegistry() *Registry` — registers all three engines
- [x] `Get(slug GameSlug) (GameEngine, error)` — returns `INTERNAL_ERROR` if slug unknown
- **DoD:** All three slugs resolvable; unknown slug → error
- **Tests [unit]:** Get each slug → correct engine type; unknown slug → error

---

## Phase 3 — Session API

### T-03-01 · Session domain entity & state machine
- [x] `internal/session/domain/session.go`: `GameSession` + `PlayerSession` structs per PRD §7
- [x] `SessionStatus` type + constants: `CREATED`, `WAITING`, `IN_PROGRESS`, `FINISHED`, `ARCHIVED`
- [x] State machine domain methods:
  - `AddPlayer(player PlayerSession) error` — error if status not CREATED/WAITING or max players reached
  - `Start() error` — WAITING → IN_PROGRESS; error if wrong status or below min players
  - `Finish(scores map[int64]int) error` — IN_PROGRESS → FINISHED
  - `Archive() error` — FINISHED → ARCHIVED
- [x] `HostPlayerID()` — returns telegram_user_id of first player (host)
- **DoD:** Invalid transitions return domain errors; state never mutated without method
- **Tests [unit]:** Each transition: valid → succeeds; invalid (wrong status) → error; AddPlayer at max → error

---

### T-03-02 · Session repository interface & migrations
- [x] `internal/session/domain/repository.go`: `SessionRepository` interface
  - `Save(ctx, session) error`
  - `FindByID(ctx, id) (*GameSession, error)`
  - `FindByBotID(ctx, botID, filter SessionFilter, pagination) ([]*GameSession, int, error)`
  - `FindActiveByChatID(ctx, botID, chatID int64) (*GameSession, error)`
  - `UpdateState(ctx, id, state json.RawMessage) error` — atomic state update
- [x] Migration `000004_create_sessions.up.sql`:
  - Table `game_sessions`: all fields from `GameSession`; `state JSONB NOT NULL DEFAULT '{}'`
  - Table `player_sessions`: FK to `game_sessions`
  - Index on `(bot_id, status)`, `(bot_id, chat_id, status)` for active lookup

---

### T-03-03 · Session PostgreSQL + Redis repository
- [x] `internal/session/infrastructure/postgres_session_repository.go` — source of truth
- [x] `internal/session/infrastructure/redis_session_cache.go` — hot cache for `IN_PROGRESS` session state
  - Cache key: `session:state:{session_id}`
  - TTL: `SESSION_TTL_HOURS` from config
  - `GetState(ctx, id) (json.RawMessage, error)`
  - `SetState(ctx, id, state, ttl) error`
  - `InvalidateState(ctx, id) error`
- [x] Write path: update Redis + Postgres atomically (Redis first; Postgres on success)
- [x] Read path: Redis hit → return; miss → Postgres → repopulate Redis
- **DoD:** State reads from Redis when warm; cold start reads from Postgres; TTL expires correctly
- **Tests [int]:** Write state → Redis populated; expire Redis → next read hits Postgres and repopulates

---

### T-03-04 · Session use case — create
- [x] `internal/session/application/session_service.go`: `CreateSession`
  - Verify bot exists and is active
  - Verify game is assigned to bot
  - Verify no active session for same `chat_id` on this bot (→ Conflict)
  - Initialize game state via `GameEngine.Init`
  - Persist session with status `CREATED`; add host as first player
- **DoD:** Duplicate chat_id → 409; unassigned game → 404; inactive bot → 404
- **Tests [unit]:** Mock repos + engine; each error path; success path persists correct state

---

### T-03-05 · Session use case — join
- [x] `JoinSession(ctx, botID, sessionID, player JoinRequest) (*GameSession, error)`
  - Session must be CREATED or WAITING
  - Re-join by same telegram_user_id → idempotent return
  - Max players check → Conflict
  - Add player; if player count ≥ game.min_players → transition to WAITING
- **DoD:** Wrong status → Conflict; max players → Conflict; idempotent re-join
- **Tests [unit]:** Join at CREATED → WAITING when min players met; join at IN_PROGRESS → error; re-join → no duplicate player

---

### T-03-06 · Session use case — start
- [x] `StartSession(ctx, botID, sessionID, callerTelegramID int64) (*GameSession, error)`
  - Caller must be host player → Forbidden if not
  - Status must be WAITING → Conflict if not
  - Transition IN_PROGRESS; `started_at = now()`
- **DoD:** Non-host → 403; wrong status → 409; success → state=IN_PROGRESS
- **Tests [unit]:** Host starts → success; non-host → Forbidden; CREATED status → Conflict

---

### T-03-07 · Session use case — submit move
- [x] `SubmitMove(ctx, botID, sessionID, move MoveRequest) (*MoveResult, error)`
  - Status must be IN_PROGRESS → Unprocessable if not
  - Load state (Redis hot path)
  - Call `engine.Validate` → 422 on invalid move
  - Call `engine.Apply` → new state + events
  - Call `engine.Evaluate` → if finished, call `FinishSession` (commit scores)
  - Persist new state atomically
- [x] Returns `MoveResult{Session, Events}` per `openapi.yaml`
- **DoD:** Invalid move → 422; valid move → state updated; game over → session FINISHED + leaderboard updated
- **Tests [unit]:** Mock engine + repos; valid move → Apply called; invalid move → Validate error surfaced; game over → Evaluate triggers finish path

---

### T-03-08 · Session use case — end
- [x] `EndSession(ctx, botID, sessionID, callerID int64, reason string) (*GameSession, error)`
  - Status CREATED/WAITING/IN_PROGRESS → proceed; FINISHED/ARCHIVED → Conflict
  - Caller must be host OR admin (admin flag injected via middleware context)
  - Partial scores committed to leaderboard
  - Transition to FINISHED; `ended_at = now()`
- **DoD:** Already finished → 409; non-host non-admin → 403; partial scores committed
- **Tests [unit]:** Each status guard; non-host → Forbidden; admin override → success

---

### T-03-09 · Session HTTP handlers
- [x] `internal/session/interface/http/session_handler.go`
- [x] Wire all 7 session endpoints per `openapi.yaml`:
  - `POST /api/v1/bots/:bot_id/sessions` → `createSession` (`BotApiKey`)
  - `GET /api/v1/bots/:bot_id/sessions` → `listSessions` (`AdminApiKey` | `BotApiKey`)
  - `GET /api/v1/bots/:bot_id/sessions/:session_id` → `getSession` (`AdminApiKey` | `BotApiKey`)
  - `POST /api/v1/bots/:bot_id/sessions/:session_id/join` → `joinSession` (`BotApiKey`)
  - `POST /api/v1/bots/:bot_id/sessions/:session_id/start` → `startSession` (`BotApiKey`)
  - `POST /api/v1/bots/:bot_id/sessions/:session_id/move` → `submitMove` (`BotApiKey`)
  - `POST /api/v1/bots/:bot_id/sessions/:session_id/end` → `endSession` (`AdminApiKey` | `BotApiKey`)
- [x] `listSessions` supports query params: `status`, `game_id`, `limit`, `offset`
- **DoD:** All 7 endpoints return correct shapes; status codes per spec
- **Tests [int]:** Full happy-path flow: create → join → start → move → end; auth guards per endpoint

---

## Phase 4 — Leaderboard

### T-04-01 · Leaderboard domain entity & repository interface
- [x] `internal/leaderboard/domain/entry.go`: `LeaderboardEntry` + `Leaderboard` structs per PRD §7 + rank field
- [x] `LeaderboardRepository` interface:
  - `UpsertEntry(ctx, entry LeaderboardEntry) error` — atomic increment of score/games_played/wins
  - `GetByBot(ctx, botID, pagination) (*Leaderboard, error)`
  - `GetByBotAndGame(ctx, botID, gameID, pagination) (*Leaderboard, error)`
  - `GetGlobal(ctx, pagination) (*Leaderboard, error)`
  - `GetGlobalByGame(ctx, gameID, pagination) (*Leaderboard, error)`
- [x] Migration `000005_create_leaderboard.up.sql`: table `leaderboard_entries`; unique constraint on `(bot_id, game_id, telegram_user_id)`; index on `total_score DESC` per scope

---

### T-04-02 · Leaderboard PostgreSQL + Redis repository
- [x] `internal/leaderboard/infrastructure/postgres_leaderboard_repository.go`
  - `UpsertEntry`: `INSERT ... ON CONFLICT DO UPDATE SET total_score = total_score + excluded.total_score, ...`
  - All get queries: `ORDER BY total_score DESC` with `LIMIT/OFFSET`; compute `rank` as `row_number()`
- [x] `internal/leaderboard/infrastructure/redis_leaderboard_cache.go`
  - Redis sorted set key pattern: `lb:{scope}:{id}` where scope = `bot`, `bot_game`, `global`, `global_game`
  - `ZADD` with score on upsert; `ZREVRANGE` for top-N reads
  - Cache TTL: 5 minutes; invalidated on upsert
- [x] Write path: Postgres upsert first (source of truth) → then Redis `ZADD`
- [x] Read path: Redis hit → return ranked list; miss → Postgres query → populate Redis
- **DoD:** Redis sorted set returns correct rank order; Postgres is authoritative; cache invalidated on new score
- **Tests [int]:** Upsert scores → Redis sorted correctly; expire cache → Postgres query used; multiple bots/games scoped correctly

---

### T-04-03 · Leaderboard use cases
- [x] `internal/leaderboard/application/leaderboard_service.go`
  - `CommitSessionScores(ctx, session *GameSession) error` — called by session service on FINISHED; batch upsert all player scores
  - `GetBotLeaderboard(ctx, botID, pagination) (*Leaderboard, error)`
  - `GetBotGameLeaderboard(ctx, botID, gameID, pagination) (*Leaderboard, error)`
  - `GetGlobalLeaderboard(ctx, pagination) (*Leaderboard, error)`
  - `GetGlobalGameLeaderboard(ctx, gameID, pagination) (*Leaderboard, error)`
- [x] `CommitSessionScores` is idempotent — re-committing same session_id does not double-count (guard via `session_id` FK or dedupe flag)
- **DoD:** Double-commit → idempotent; scores aggregated across multiple sessions
- **Tests [unit]:** Mock repo; commit scores → UpsertEntry called per player; double commit → called once; pagination passed through

---

### T-04-04 · Leaderboard HTTP handlers
- [x] `internal/leaderboard/interface/http/leaderboard_handler.go`
- [x] Wire all 4 leaderboard endpoints per `openapi.yaml`:
  - `GET /api/v1/bots/:bot_id/leaderboard` → `getBotLeaderboard`
  - `GET /api/v1/bots/:bot_id/leaderboard/:game_id` → `getBotGameLeaderboard`
  - `GET /api/v1/leaderboard` → `getGlobalLeaderboard`
  - `GET /api/v1/leaderboard/:game_id` → `getGlobalGameLeaderboard`
- [x] All accessible by `AdminApiKey` OR `BotApiKey`
- [x] `limit` query param: default 10, max 100
- **DoD:** Rank field populated (1-based); response shape matches `openapi.yaml` `Leaderboard` schema; pagination meta included
- **Tests [int]:** Seed scores → ranks correct; limit=3 → 3 entries; bot_id scoping works

---

## Phase 5 — Hardening

### T-05-01 · Rate limiting middleware
- [x] `internal/middleware/rate_limit.go`
- [x] Per-bot-token rate limit: 60 req/min (sliding window via Redis)
- [x] Per-IP rate limit on unauthenticated endpoints: 30 req/min
- [x] Returns 429 with `Retry-After` header; body uses error envelope with code `RATE_LIMITED`
- **DoD:** Exceeding limit → 429; `Retry-After` header set correctly; limit resets after window
- **Tests [int]:** Send 61 requests in 1 minute from same bot token → 61st returns 429

---

### T-05-02 · Prometheus metrics
- [x] `GET /metrics` endpoint (no auth, separate port optional)
- [x] Expose metrics:
  - `http_requests_total{method, path, status}` — counter
  - `http_request_duration_seconds{method, path}` — histogram (buckets: 10ms, 50ms, 100ms, 200ms, 500ms, 1s)
  - `active_sessions_total{bot_id, game_slug}` — gauge
  - `game_moves_total{game_slug}` — counter
  - `leaderboard_cache_hits_total` / `leaderboard_cache_misses_total` — counters
- [x] Metrics middleware attached to all routes
- **DoD:** `GET /metrics` returns Prometheus text format; `http_request_duration_seconds` histogram has correct buckets
- **Tests [int]:** Make requests → counters increment; verify metric names in /metrics output

---

### T-05-03 · Session archival job
- [x] Background goroutine runs every hour
- [x] Queries sessions where `status = FINISHED` AND `ended_at < now() - SESSION_TTL_HOURS`
- [x] Transitions those sessions to `ARCHIVED`; invalidates Redis cache entries
- [x] Logs count of archived sessions per run
- **DoD:** Sessions older than TTL archived on next hourly run; no double-archiving
- **Tests [unit]:** Mock repo; archival finds correct sessions; updates status; invalidates cache

---

### T-05-04 · Input sanitization & security hardening
- [x] All string inputs trimmed and length-capped per schema constraints
- [x] `display_name`, `word`, `answer`, `reason` fields: strip control characters; reject null bytes
- [x] SQL: confirm all queries use parameterized statements (audit via code review checklist)
- [x] Bot token: never appears in logs, error messages, or response bodies (covered by `BotToken.MarshalJSON`)
- [x] `ADMIN_API_KEY` and `BOT_TOKEN_ENCRYPTION_KEY`: never logged
- **DoD:** Fuzzing `display_name` with control chars → sanitized or rejected; token fields redacted in all log output
- **Tests [unit]:** Sanitize function: null byte → stripped; oversized string → truncated/rejected; control chars → stripped

---

### T-05-05 · End-to-end test suite
- [x] `test/e2e/` folder; uses `docker compose -f docker-compose.yml` test environment
- [x] Scenario 1 — Uno game:
  - Register bot → assign Uno → create session → 2 players join → start → play moves until win → verify leaderboard updated
- [x] Scenario 2 — Sambung Kata:
  - Register bot → assign game → create session → players join → start → valid words → invalid word (wrong letter) → player eliminated → verify score
- [x] Scenario 3 — Truth or Date:
  - Create session → join → start → pick truth → answer → pick date → skip (host) → end session → verify scores
- [x] Scenario 4 — Auth:
  - No key → 401; wrong key → 401; valid admin key → 200; bot key on admin endpoint → 401
- [x] Scenario 5 — Leaderboard aggregation:
  - Two sessions finished → leaderboard ranks correct; global leaderboard includes both bots
- **DoD:** All 5 scenarios pass in CI against Docker Compose stack; no flakiness over 3 consecutive runs
- **Tests [e2e]:** As above — HTTP client hits real running stack

---

### T-05-06 · CI pipeline
- [x] GitHub Actions workflow `.github/workflows/ci.yml`
- [x] Jobs:
  1. `lint`: `golangci-lint run` (fail on any lint error)
  2. `test-unit`: `go test ./... -run Unit -coverprofile=coverage.out`; fail if coverage < 80%
  3. `test-int`: spin up postgres + redis via service containers; `go test ./... -run Integration`
  4. `build`: `docker build -f docker/Dockerfile.prod .`
- [x] All jobs run on pull request and push to `main`
- [x] Coverage report uploaded as artifact
- **DoD:** All 4 jobs green on clean main; coverage gate enforced

---

## Phase 6 — Telegram Webhook Integration

### T-06-01 · Config expansion
- [ ] Add 5 new env vars to `internal/config/config.go`:
  - `MAIN_BOT_TOKEN` (required)
  - `TELEGRAM_ADMIN_IDS` (required; parse comma-separated string → `[]int64`)
  - `WEBHOOK_BASE_URL` (required; strip trailing slash)
  - `WEBHOOK_SECRET_TOKEN` (required)
  - `CONV_STATE_TTL_MINUTES` (optional; default `10`)
- [ ] Add vars to `.env.example` with placeholder values
- [ ] Fail fast at boot if any required var is missing
- **DoD:** Missing `MAIN_BOT_TOKEN` → process exits with clear error; `TELEGRAM_ADMIN_IDS=123,456` parsed to `[]int64{123, 456}`
- **Tests [unit]:** Defaults applied; missing required → error; multi-value `TELEGRAM_ADMIN_IDS` parsed correctly

---

### T-06-02 · Telegram client expansion
- [ ] `internal/telegram/client.go` — add methods to existing client:
  - `SetWebhook(ctx, token, webhookURL, secretToken string) error`
  - `DeleteWebhook(ctx, token string) error`
  - `GetWebhookInfo(ctx, token string) (WebhookInfo, error)`
  - `SendMessage(ctx, token string, chatID int64, text string) error`
- [ ] All calls go to `https://api.telegram.org/bot<token>/<method>`
- [ ] Respect 5-second timeout per call; return structured error on non-2xx
- **DoD:** `SetWebhook` called with correct URL and secret; `DeleteWebhook` clears webhook; `SendMessage` delivers text reply
- **Tests [unit]:** Mock HTTP transport; verify correct URL, payload, and token per call

---

### T-06-03 · Telegram Update types
- [ ] `internal/telegram/update.go` — define structs for Telegram Bot API objects:
  - `Update { UpdateID int64; Message *Message }`
  - `Message { MessageID int64; From *User; Chat *Chat; Text string; Date int64 }`
  - `User { ID int64; FirstName string; LastName string; Username string }`
  - `Chat { ID int64; Type string }`
  - `WebhookInfo { URL string; HasCustomCertificate bool; PendingUpdateCount int }`
- [ ] All structs JSON-tagged to match Telegram Bot API field names (snake_case)
- **DoD:** Unmarshalling a real Telegram update JSON into `Update` populates all fields correctly
- **Tests [unit]:** Golden JSON → unmarshal → field assertions for each struct

---

### T-06-04 · Conversation FSM state + Redis store
- [ ] `internal/webhook/conversation.go`
  - `ConvState` type + constants: `ConvStateIdle`, `ConvStateAwaitToken`, `ConvStateAwaitName`, `ConvStateDone`
  - `ConversationData { State ConvState; Token string; Name string }`
- [ ] `internal/webhook/conversation_store.go`
  - Interface: `ConversationStore`
    - `Get(ctx, userID int64) (ConversationData, error)`
    - `Set(ctx, userID int64, data ConversationData, ttl time.Duration) error`
    - `Delete(ctx, userID int64) error`
  - Redis implementation; key: `conv:main:<userID>`; value: JSON-encoded `ConversationData`
  - TTL from `CONV_STATE_TTL_MINUTES` config
- **DoD:** Get on missing key returns `ConvStateIdle`; Set with TTL expires after TTL; Delete clears immediately
- **Tests [unit]:** Mock Redis; full FSM lifecycle: idle → await_token → await_name → done → deleted

---

### T-06-05 · Chat-to-session index (Redis)
- [ ] `internal/webhook/chat_session_index.go`
  - Interface: `ChatSessionIndex`
    - `Set(ctx, botID uuid.UUID, chatID int64, sessionID uuid.UUID, ttl time.Duration) error`
    - `Get(ctx, botID uuid.UUID, chatID int64) (uuid.UUID, error)` — returns `NotFound` if absent
    - `Delete(ctx, botID uuid.UUID, chatID int64) error`
  - Redis implementation; key: `chat_session:<botID>:<chatID>`; value: session UUID string
  - TTL matches `SESSION_TTL_HOURS` converted to duration
- [ ] `SessionService.CreateSession` calls `ChatSessionIndex.Set` after persisting session
- [ ] `SessionService.EndSession` calls `ChatSessionIndex.Delete` after finishing session
- **DoD:** After `CreateSession`, `Get(botID, chatID)` returns new session ID; after `EndSession`, `Get` returns NotFound
- **Tests [unit]:** Mock Redis; set → get returns ID; delete → get returns NotFound

---

### T-06-06 · Webhook secret middleware
- [ ] `internal/middleware/webhook_secret.go`
  - `WebhookSecretMiddleware(secret string) fiber.Handler`
  - Reads `X-Telegram-Bot-Api-Secret-Token` header
  - Validates using `crypto/subtle.ConstantTimeCompare`
  - Returns 401 envelope if missing or mismatch; calls `c.Next()` on success
- [ ] Applied only to `/telegram/*` routes; NOT applied to `/api/v1/*` routes
- **DoD:** Wrong secret → 401; missing header → 401; correct secret → handler called
- **Tests [unit]:** Valid secret → next called; invalid → 401; missing → 401; timing-safe (no early exit on mismatch)

---

### T-06-07 · Main bot handler + admin commands + /addbot FSM
- [ ] `internal/webhook/main_handler.go`
  - `MainBotHandler` struct; depends on `BotService`, `GameService`, `ConversationStore`, `TelegramClient`, `[]int64` admin IDs
  - `HandleUpdate(ctx, update telegram.Update) error` — dispatcher
  - Admin ID check: if `update.Message.From.ID` not in admin IDs → send "unauthorized" reply; return
  - Command dispatch:
    - `/addbot` → FSM entry: set state `AWAIT_TOKEN`; reply "Send me the bot token"
    - `/removebot <bot_id>` → call `BotService.DeleteBotWithWebhook`; reply confirmation
    - `/reactivatebot <bot_id>` → call `BotService.ReactivateBotWithWebhook`; re-register webhook; reply confirmation
    - `/listbots` → call `BotService.ListBots`; format tabular reply (ID, name, active status)
    - `/listgames` → call `GameService.ListGames`; format reply with slug + name + min/max players
    - `/listbotgames <bot_id>` → call `BotGameService.ListBotGames(botID)`; format reply with assigned game slugs
    - `/assigngame <bot_id> <game_slug>` → call `BotGameService.AssignGame`; reply confirmation
    - `/removegame <bot_id> <game_slug>` → call `BotGameService.RemoveGame`; reply confirmation
    - `/leaderboard <bot_id>` → call `LeaderboardService.GetBotLeaderboard`; format ranked reply
    - `/leaderboard global` or `/leaderboard` (no args) → call `LeaderboardService.GetGlobalLeaderboard`; format ranked reply
  - FSM message handling (non-command text):
    - `AWAIT_TOKEN`: validate token via `TelegramClient.GetMe`; on success store token in `ConvData`; advance to `AWAIT_NAME`; reply "Now send me a name for this bot"
    - `AWAIT_NAME`: call `BotService.RegisterBotWithWebhook(token, name)`; set `DONE`; delete conv state; reply "Bot registered! ID: <id>"
    - `IDLE` + unknown text → reply help message listing all commands
- [ ] Register route: `POST /telegram/main/webhook` → `MainBotHandler.HandleUpdate`
- [ ] Add `BotService.ReactivateBotWithWebhook(ctx, botID)` — sets `active=true` + calls `SetWebhook`; add to T-06-09
- **DoD:** Full `/addbot` FSM completes: send token → validated → send name → bot registered with webhook set; `/listgames` reply includes all 3 game slugs; `/reactivatebot` re-registers webhook; admin check blocks non-admin users
- **Tests [unit]:** Mock all deps; test each command path; FSM: valid token → advance state; invalid token → error reply; non-admin → rejected; `/listgames` → GameService called; `/listbotgames` → BotGameService called with correct ID; `/leaderboard` no args → global leaderboard; `/reactivatebot` → ReactivateBotWithWebhook called

---

### T-06-08 · Child bot handler + game command routing
- [ ] `internal/webhook/child_handler.go`
  - `ChildBotHandler` struct; depends on `BotService`, `SessionService`, `LeaderboardService`, `ChatSessionIndex`, `TelegramClient`
  - `HandleUpdate(ctx, botID uuid.UUID, update telegram.Update) error` — dispatcher
  - Command dispatch:
    - `/newgame <game_slug>` → call `SessionService.CreateSession(botID, gameSlug, chatID, hostPlayer)`; reply with session ID and join instructions
    - `/join` → look up active session via `ChatSessionIndex.Get(botID, chatID)`; call `SessionService.JoinSession`; reply confirmation
    - `/start` → look up session; call `SessionService.StartSession(callerTelegramID)`; reply "Game started!"
    - `/move <json_payload>` → look up session; parse payload; call `SessionService.SubmitMove`; format events reply
    - `/end` → look up session; call `SessionService.EndSession`; reply scores summary
    - `/leaderboard` → call `LeaderboardService.GetBotLeaderboard`; format reply
    - Unknown command → reply "Available commands: /newgame, /join, /start, /move, /end, /leaderboard"
  - No active session on `/join`/`/start`/`/move`/`/end` → reply "No active game in this chat. Use /newgame to start one."
- [ ] Register route: `POST /telegram/child/:bot_id/webhook` → parse `bot_id` UUID → `ChildBotHandler.HandleUpdate`
- [ ] Invalid `bot_id` UUID or unknown bot → return 200 (always ack Telegram) but log error
- **DoD:** `/newgame uno` creates session and replies with ID; `/join` adds player; `/move` applies game move and replies with events; unknown command → help reply
- **Tests [unit]:** Mock all deps; each command: success path + no-session path + invalid args path

---

### T-06-09 · BotService extensions — RegisterBotWithWebhook / DeleteBotWithWebhook
- [ ] Add to `internal/bot/application/bot_service.go`:
  - `RegisterBotWithWebhook(ctx, name, rawToken string) (*Bot, error)`
    - Encrypt token; call `TelegramClient.GetMe` to validate and get `telegram_id`
    - Persist bot
    - Call `TelegramClient.SetWebhook(rawToken, WEBHOOK_BASE_URL+"/telegram/child/"+bot.ID.String(), WEBHOOK_SECRET_TOKEN)`
    - On webhook failure → delete bot from DB; return error (atomicity)
  - `DeleteBotWithWebhook(ctx, botID uuid.UUID) (*Bot, error)`
    - Load bot; decrypt token
    - Call `TelegramClient.DeleteWebhook(rawToken)`
    - Deactivate bot in DB (don't hard-delete; keep history)
  - `ReactivateBotWithWebhook(ctx, botID uuid.UUID) (*Bot, error)`
    - Load bot; verify currently inactive (→ Conflict if already active)
    - Set `active = true`; persist
    - Decrypt token; call `TelegramClient.SetWebhook(rawToken, childWebhookURL, WEBHOOK_SECRET_TOKEN)`
    - On webhook failure → revert `active = false`; return error
- [ ] `NewBotService` gains `webhookBaseURL string`, `webhookSecret string`, `telegramClient TelegramClient` params
- **DoD:** `RegisterBotWithWebhook` rolls back DB record if `SetWebhook` fails; `DeleteBotWithWebhook` always deactivates even if Telegram call fails (best-effort cleanup); `ReactivateBotWithWebhook` re-registers webhook and reverts on failure; webhook URL pattern is `<WEBHOOK_BASE_URL>/telegram/child/<bot_id>`
- **Tests [unit]:** Mock `TelegramClient`; register → `SetWebhook` called with correct URL; webhook error → bot not in DB; delete → `DeleteWebhook` called; reactivate already-active bot → Conflict; reactivate → `SetWebhook` called; reactivate webhook failure → bot remains inactive

---

### T-06-10 · main.go wiring + startup webhook registration
- [ ] `cmd/api/main.go` — wire new dependencies:
  - Construct `telegram.NewClient(httpClient)`
  - Parse `TELEGRAM_ADMIN_IDS` from config into `[]int64`
  - Construct `ConversationStore` (Redis)
  - Construct `ChatSessionIndex` (Redis)
  - Construct `MainBotHandler`
  - Construct `ChildBotHandler`
  - Register `/telegram/main/webhook` and `/telegram/child/:bot_id/webhook` routes with `WebhookSecretMiddleware`
- [ ] Startup sequence (before accepting requests):
  1. Run DB migrations
  2. Ping Redis
  3. Call `TelegramClient.SetWebhook(MAIN_BOT_TOKEN, WEBHOOK_BASE_URL+"/telegram/main/webhook", WEBHOOK_SECRET_TOKEN)` — log result; non-fatal on failure (allow degraded start)
  4. Start HTTP server
- [ ] Add `MAIN_BOT_TOKEN`, `WEBHOOK_BASE_URL`, `WEBHOOK_SECRET_TOKEN`, `TELEGRAM_ADMIN_IDS`, `CONV_STATE_TTL_MINUTES` to `.env.example`
- **DoD:** `docker compose up` starts API; main bot webhook registered; `POST /telegram/main/webhook` with correct secret → 200; wrong secret → 401
- **Tests [int]:** Mock Telegram API server; startup calls `setWebhook`; webhook endpoint reachable with correct secret

---

### T-06-11 · Update dependency map (Phase 6 additions)
- [ ] Document Phase 6 dependencies (no code change — this task is documentation only):
  - `T-06-01` (config) → all T-06-* tasks
  - `T-06-02` (Telegram client) → `T-06-07`, `T-06-08`, `T-06-09`
  - `T-06-03` (Update types) → `T-06-07`, `T-06-08`
  - `T-06-04` (FSM store) → `T-06-07`
  - `T-06-05` (chat-session index) → `T-06-08`, `T-03-04`, `T-03-08` (CreateSession / EndSession call index)
  - `T-06-06` (secret middleware) → `T-06-10`
  - `T-06-07`, `T-06-08` (handlers) → `T-06-10`
  - `T-06-09` (BotService extensions) → `T-06-07`
  - `T-06-10` (main.go wiring) → Phase 6 complete
  - Phase 5 (hardening) must be complete before Phase 6 is considered production-ready
- **DoD:** This TASKS.md updated; all tasks above have clear DoD and test requirements

---

## Dependency Map

```
T-00-01 → T-00-02..T-00-10 (all scaffold tasks)
T-00-08 → T-01-03, T-01-08, T-03-02, T-04-01 (migrations need DB runner)
T-00-09 → T-03-03, T-04-02 (Redis repos need client)
T-01-01 → T-01-05 (use cases need entity)
T-01-03 → T-01-04 → T-01-05 → T-01-06 (bot stack: domain→repo→service→handler)
T-01-07 → T-01-06, T-03-09, T-04-04 (auth middleware needed before handlers)
T-01-08 → T-01-09 → T-01-10 → T-01-11 (game catalog before assignment)
T-02-01 → T-02-02, T-02-03, T-02-04 (engine interface before implementations)
T-02-02..T-02-04 → T-02-05 (registry needs all engines)
T-02-05 → T-03-04 (session create needs registry)
T-03-01 → T-03-04..T-03-08 (session entity before use cases)
T-03-02 → T-03-03 → T-03-04..T-03-08 (repo before use cases)
T-03-07 → T-04-03 (submit move triggers score commit)
T-04-01 → T-04-02 → T-04-03 → T-04-04 (leaderboard stack)
Phase 5 → all Phase 0-4 complete
T-06-01 → T-06-02..T-06-11 (config before all Phase 6 tasks)
T-06-02 → T-06-07, T-06-08, T-06-09 (Telegram client before handlers)
T-06-03 → T-06-07, T-06-08 (Update types before handlers)
T-06-04 → T-06-07 (FSM store before main handler)
T-06-05 → T-06-08 (chat-session index before child handler)
T-06-06 → T-06-10 (secret middleware before wiring)
T-06-07, T-06-08 → T-06-10 (handlers before main.go wiring)
T-06-09 → T-06-07 (BotService extensions before main handler)
Phase 5 → Phase 6 (hardening before Telegram integration is production-ready)
```
