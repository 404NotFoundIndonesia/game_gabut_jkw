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

## Telegram Setup

This API is the game backend only — it has no webhook endpoint and never receives Telegram messages directly. To let Telegram users play games, you need a **child bot**: a separate program that receives Telegram messages and translates them into HTTP calls to this API.

### How the two pieces connect

```
Telegram User
     │ sends /newgame, /join, /play …
     ▼
Child Bot  (your code — any language)
     │ HTTP  X-Bot-Token: <BOT_API_KEY>
     ▼
This API  (game_gabut_jkw)
     │
     ├── PostgreSQL  (game state, scores)
     └── Redis       (session cache, rate limit)
```

The child bot is responsible for:
- Receiving Telegram `Update` objects (via webhook or polling)
- Translating commands into HTTP calls to this API
- Sending API responses back to the Telegram chat

---

### Step 0 — Create a Telegram bot via BotFather

1. Open Telegram and search for **@BotFather**
2. Send `/newbot` and follow the prompts
3. BotFather gives you a token like `1234567890:ABCdefGHIjklMNOpqrsTUVwxyz`
4. Optionally set `/setcommands` in BotFather so users see a command menu

Keep the token — you will register it in Step 2.

---

### Step 1 — Start this API

```bash
cp .env.example .env.local

# Edit at minimum:
#   ADMIN_API_KEY=your-secret-admin-key
#   BOT_TOKEN_ENCRYPTION_KEY=exactly-32-characters-here!!

docker compose up -d
```

Verify the API is healthy:

```bash
curl http://localhost:8080/health
# {"success":true,"data":{"status":"ok"}}
```

---

### Step 2 — Register the Telegram bot

```bash
curl -X POST http://localhost:8080/api/v1/bots \
  -H "Authorization: Bearer <ADMIN_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "MyGameBot",
    "token": "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz"
  }'
```

Response:

```json
{
  "success": true,
  "data": {
    "id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "name": "MyGameBot",
    "api_key": "the-bot-api-key-returned-once",
    "active": true
  }
}
```

Save `data.id` as **`BOT_ID`** and `data.api_key` as **`BOT_API_KEY`**. The raw Telegram token is AES-256-GCM encrypted at rest and never returned again.

---

### Step 3 — Find the game IDs

```bash
curl http://localhost:8080/api/v1/games \
  -H "Authorization: Bearer <ADMIN_API_KEY>"
```

Response:

```json
{
  "success": true,
  "data": [
    {"id": "aaaa-0001-...", "slug": "uno",           "name": "Uno",           "min_players": 2, "max_players": 10},
    {"id": "bbbb-0002-...", "slug": "sambung_kata",  "name": "Sambung Kata",  "min_players": 2, "max_players": 20},
    {"id": "cccc-0003-...", "slug": "truth_or_date", "name": "Truth or Date", "min_players": 2, "max_players": 20}
  ]
}
```

Save the `id` of each game you want to use as **`GAME_ID`**.

---

### Step 4 — Assign a game to the bot

```bash
curl -X POST http://localhost:8080/api/v1/bots/<BOT_ID>/games \
  -H "Authorization: Bearer <ADMIN_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"game_id": "<GAME_ID>"}'
```

Repeat for each game the bot should support. Assignment is idempotent — safe to call multiple times.

---

### Step 5 — Write the child bot

Below is a minimal Python example using [`pyTelegramBotAPI`](https://github.com/eternnoir/pyTelegramBotAPI). Any language or Telegram library works — the only requirement is that every request to this API includes the header `X-Bot-Token: <BOT_API_KEY>`.

```bash
pip install pyTelegramBotAPI requests
```

```python
import json, os
import telebot, requests

TELEGRAM_TOKEN = os.environ["TELEGRAM_BOT_TOKEN"]
API_BASE       = os.environ.get("API_BASE_URL", "http://localhost:8080")
BOT_ID         = os.environ["BOT_ID"]       # UUID from Step 2
BOT_API_KEY    = os.environ["BOT_API_KEY"]  # api_key from Step 2
GAME_ID        = os.environ["GAME_ID"]      # UUID from Step 3

bot = telebot.TeleBot(TELEGRAM_TOKEN)

# Track active session per chat.  Use Redis or a DB in production.
active_sessions: dict[int, str] = {}

HEADERS = {"X-Bot-Token": BOT_API_KEY, "Content-Type": "application/json"}

def api(method, path, **kwargs):
    resp = requests.request(method, f"{API_BASE}/api/v1/{path}",
                            headers=HEADERS, **kwargs)
    return resp.json()

# ── /newgame — host creates a session ────────────────────────────────────────

@bot.message_handler(commands=["newgame"])
def cmd_newgame(message):
    result = api("POST", f"bots/{BOT_ID}/sessions", json={
        "game_id": GAME_ID,
        "chat_id": message.chat.id,
        "player": {
            "telegram_user_id": message.from_user.id,
            "display_name":     message.from_user.first_name,
        },
    })
    if not result.get("success"):
        bot.reply_to(message, f"Error: {result['error']['message']}")
        return
    session = result["data"]
    active_sessions[message.chat.id] = session["id"]
    bot.reply_to(message,
        f"Game created!\nSession ID: {session['id']}\n"
        f"Others can /join now. Host sends /startgame when ready.")

# ── /join — other players join ────────────────────────────────────────────────

@bot.message_handler(commands=["join"])
def cmd_join(message):
    session_id = active_sessions.get(message.chat.id)
    if not session_id:
        bot.reply_to(message, "No active session in this chat. Use /newgame first.")
        return
    result = api("POST", f"bots/{BOT_ID}/sessions/{session_id}/join", json={
        "telegram_user_id": message.from_user.id,
        "display_name":     message.from_user.first_name,
    })
    if not result.get("success"):
        bot.reply_to(message, f"Error: {result['error']['message']}")
        return
    players = [p["display_name"] for p in result["data"]["players"]]
    bot.reply_to(message, f"Joined! Players so far: {', '.join(players)}")

# ── /startgame — host starts the game ────────────────────────────────────────

@bot.message_handler(commands=["startgame"])
def cmd_startgame(message):
    session_id = active_sessions.get(message.chat.id)
    if not session_id:
        bot.reply_to(message, "No active session.")
        return
    result = api("POST", f"bots/{BOT_ID}/sessions/{session_id}/start", json={
        "telegram_user_id": message.from_user.id,
    })
    if not result.get("success"):
        bot.reply_to(message, f"Error: {result['error']['message']}")
        return
    bot.reply_to(message, "Game started! Use /play to submit moves.")

# ── /play — submit a move ─────────────────────────────────────────────────────
# Usage: /play {"action":"play_card","card":"red_7"}   (Uno)
#        /play {"word":"apel"}                          (Sambung Kata)
#        /play {"choice":"truth"}                       (Truth or Date)

@bot.message_handler(commands=["play"])
def cmd_play(message):
    session_id = active_sessions.get(message.chat.id)
    if not session_id:
        bot.reply_to(message, "No active session.")
        return
    try:
        payload = json.loads(message.text.split(" ", 1)[1])
    except Exception:
        bot.reply_to(message, 'Usage: /play {"key": "value", ...}')
        return
    result = api("POST", f"bots/{BOT_ID}/sessions/{session_id}/move", json={
        "player_id": message.from_user.id,
        "payload":   payload,
    })
    if not result.get("success"):
        bot.reply_to(message, f"Error: {result['error']['message']}")
        return
    data   = result["data"]
    events = data.get("events", [])
    session = data.get("session", {})
    for event in events:
        bot.send_message(message.chat.id, f"[{event['type']}] {event.get('payload', '')}")
    if session.get("status") == "FINISHED":
        active_sessions.pop(message.chat.id, None)
        bot.send_message(message.chat.id, "Game over!")

# ── /endgame — force end ──────────────────────────────────────────────────────

@bot.message_handler(commands=["endgame"])
def cmd_endgame(message):
    session_id = active_sessions.get(message.chat.id)
    if not session_id:
        bot.reply_to(message, "No active session.")
        return
    result = api("POST", f"bots/{BOT_ID}/sessions/{session_id}/end", json={
        "telegram_user_id": message.from_user.id,
        "reason":           "host ended the game",
    })
    if not result.get("success"):
        bot.reply_to(message, f"Error: {result['error']['message']}")
        return
    active_sessions.pop(message.chat.id, None)
    bot.reply_to(message, "Game ended.")

# ── /leaderboard — show top scores for this bot ───────────────────────────────

@bot.message_handler(commands=["leaderboard"])
def cmd_leaderboard(message):
    result = api("GET", f"bots/{BOT_ID}/leaderboard")
    if not result.get("success"):
        bot.reply_to(message, "Could not fetch leaderboard.")
        return
    entries = result["data"].get("entries", [])
    if not entries:
        bot.reply_to(message, "No scores yet.")
        return
    lines = [f"{i+1}. {e['display_name']} — {e['total_score']} pts"
             for i, e in enumerate(entries[:10])]
    bot.reply_to(message, "Leaderboard:\n" + "\n".join(lines))

bot.infinity_polling()
```

Run the child bot:

```bash
TELEGRAM_BOT_TOKEN=1234567890:ABCdef... \
BOT_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx \
BOT_API_KEY=the-bot-api-key \
GAME_ID=aaaa-0001-xxxx-xxxx-xxxxxxxxxxxx \
python bot.py
```

---

### Step 6 — Configure how Telegram delivers updates

Telegram can deliver `Update` objects to your child bot in two ways. Choose one.

#### Option A — Polling (simplest, best for local development)

The child bot periodically asks Telegram "any new messages?". No public URL needed, no SSL.

`bot.infinity_polling()` in the example above already uses this. Nothing extra to configure.

To make sure no leftover webhook is set (which blocks polling), run:

```bash
curl "https://api.telegram.org/bot<TELEGRAM_TOKEN>/deleteWebhook"
```

#### Option B — Webhook (recommended for production)

Telegram pushes each `Update` as an HTTP POST to a URL you register. Requires:
- A **publicly reachable HTTPS URL** (Telegram enforces SSL)
- Your child bot running as an HTTP server

##### Local development with ngrok

[ngrok](https://ngrok.com) creates a temporary HTTPS tunnel to your local machine.

```bash
# 1. Install ngrok: https://ngrok.com/download

# 2. Start your child bot in webhook mode (see code below) on port 5000
python bot_webhook.py

# 3. In another terminal, expose port 5000 publicly
ngrok http 5000
# ngrok prints a URL like: https://abc123.ngrok-free.app

# 4. Register that URL with Telegram
curl "https://api.telegram.org/bot<TELEGRAM_TOKEN>/setWebhook" \
  -d "url=https://abc123.ngrok-free.app/webhook"

# 5. Verify Telegram accepted the webhook
curl "https://api.telegram.org/bot<TELEGRAM_TOKEN>/getWebhookInfo"
```

Webhook version of the child bot (`bot_webhook.py`):

```python
import json, os
from flask import Flask, request as freq
import telebot, requests

TELEGRAM_TOKEN = os.environ["TELEGRAM_BOT_TOKEN"]
API_BASE       = os.environ.get("API_BASE_URL", "http://localhost:8080")
BOT_ID         = os.environ["BOT_ID"]
BOT_API_KEY    = os.environ["BOT_API_KEY"]
GAME_ID        = os.environ["GAME_ID"]
WEBHOOK_HOST   = os.environ["WEBHOOK_HOST"]  # e.g. https://abc123.ngrok-free.app

bot = telebot.TeleBot(TELEGRAM_TOKEN)

# ... (same handlers as the polling example above) ...

# Remove polling, add Flask webhook server instead
app = Flask(__name__)

@app.route("/webhook", methods=["POST"])
def webhook():
    update = telebot.types.Update.de_json(freq.data.decode("utf-8"))
    bot.process_new_updates([update])
    return "", 200

if __name__ == "__main__":
    bot.remove_webhook()
    bot.set_webhook(url=f"{WEBHOOK_HOST}/webhook")
    app.run(host="0.0.0.0", port=5000)
```

##### Production deployment

In production your child bot needs a real domain with a valid TLS certificate (Let's Encrypt is free).

```bash
# Register your production webhook (do this once, or after each redeploy)
curl "https://api.telegram.org/bot<TELEGRAM_TOKEN>/setWebhook" \
  -d "url=https://bot.your-domain.com/webhook"

# Verify
curl "https://api.telegram.org/bot<TELEGRAM_TOKEN>/getWebhookInfo"
# "url" should show your domain, "pending_update_count" should be 0

# To switch back to polling (e.g. for debugging), remove the webhook first
curl "https://api.telegram.org/bot<TELEGRAM_TOKEN>/deleteWebhook"
```

> **Note:** Polling and webhook cannot be active at the same time. If `getWebhookInfo` shows a URL set, polling calls will return an error. Always call `deleteWebhook` before switching to polling.

---

### Step 7 — Running multiple bots

Register as many Telegram bots as you need. Each bot:
- Has its own BotFather token
- Gets its own `BOT_ID` + `BOT_API_KEY` from this API (Step 2)
- Runs as a separate child bot process
- Can have a different set of games assigned

```
BotFather token A → register → BOT_ID_A + BOT_API_KEY_A → assign Uno
BotFather token B → register → BOT_ID_B + BOT_API_KEY_B → assign Sambung Kata + Truth or Date
BotFather token C → register → BOT_ID_C + BOT_API_KEY_C → assign all three
        │                   │                   │
        └─────────── all call this same API ────┘
                   (separate X-Bot-Token per bot)
```

Run each bot as a separate process, each with its own env vars:

```bash
# Bot A — Uno only
TELEGRAM_BOT_TOKEN=<token-A> BOT_ID=<id-A> BOT_API_KEY=<key-A> GAME_ID=<uno-id> \
  python bot.py &

# Bot B — Sambung Kata
TELEGRAM_BOT_TOKEN=<token-B> BOT_ID=<id-B> BOT_API_KEY=<key-B> GAME_ID=<sambung-id> \
  python bot.py &
```

Or use Docker Compose to manage them together:

```yaml
# docker-compose.bots.yml
services:
  bot-uno:
    image: python:3.12-slim
    command: python bot.py
    environment:
      TELEGRAM_BOT_TOKEN: "${TOKEN_A}"
      BOT_ID:             "${BOT_ID_A}"
      BOT_API_KEY:        "${BOT_API_KEY_A}"
      GAME_ID:            "${UNO_GAME_ID}"
      API_BASE_URL:       "http://api:8080"

  bot-sambung:
    image: python:3.12-slim
    command: python bot.py
    environment:
      TELEGRAM_BOT_TOKEN: "${TOKEN_B}"
      BOT_ID:             "${BOT_ID_B}"
      BOT_API_KEY:        "${BOT_API_KEY_B}"
      GAME_ID:            "${SAMBUNG_GAME_ID}"
      API_BASE_URL:       "http://api:8080"
```

---

### Command → API mapping reference

| User sends | Child bot calls | Auth |
|------------|-----------------|------|
| `/newgame` | `POST /api/v1/bots/<BOT_ID>/sessions` | `X-Bot-Token` |
| `/join` | `POST /api/v1/bots/<BOT_ID>/sessions/<SESSION_ID>/join` | `X-Bot-Token` |
| `/startgame` | `POST /api/v1/bots/<BOT_ID>/sessions/<SESSION_ID>/start` | `X-Bot-Token` |
| `/play <json>` | `POST /api/v1/bots/<BOT_ID>/sessions/<SESSION_ID>/move` | `X-Bot-Token` |
| `/endgame` | `POST /api/v1/bots/<BOT_ID>/sessions/<SESSION_ID>/end` | `X-Bot-Token` |
| `/leaderboard` | `GET /api/v1/bots/<BOT_ID>/leaderboard` | `X-Bot-Token` |

---

### Move payload reference

The `payload` field in `POST .../move` is game-specific.

#### Uno

| Action | Payload |
|--------|---------|
| Play a card | `{"action": "play_card", "card": "red_7"}` |
| Draw a card | `{"action": "draw"}` |
| Play Wild (choose color) | `{"action": "play_card", "card": "wild", "chosen_color": "blue"}` |
| Play Wild Draw Four | `{"action": "play_card", "card": "wild_draw_four", "chosen_color": "green"}` |

Card format: `<color>_<value>`. Colors: `red`, `green`, `blue`, `yellow`. Values: `0`–`9`, `skip`, `reverse`, `draw_two`.

#### Sambung Kata

| Action | Payload |
|--------|---------|
| Submit a word | `{"word": "apel"}` |

The word must start with the last letter of the previous word and must exist in the KBBI dictionary.

#### Truth or Date

| Action | Payload |
|--------|---------|
| Choose truth | `{"choice": "truth"}` |
| Choose dare | `{"choice": "dare"}` |
| Submit answer | `{"answer": "my answer text"}` |
| Skip question (host only) | `{"skip": true}` |

---

### Important notes

- **Session tracking** — The child bot must store `chat_id → session_id` itself (in memory, Redis, or a DB). The API does not push session IDs to the bot.
- **One session per chat** — Only one active session is allowed per `(bot_id, chat_id)`. A second `/newgame` in the same chat returns `409 Conflict` until the current session ends.
- **Bot-only endpoints** — `create`, `join`, `start`, and `move` require `X-Bot-Token` and return `403` if called with an admin key.
- **Rate limit** — 60 requests/min per bot token. The API returns `429` with a `Retry-After` header when exceeded.
- **Multiple bots** — Register as many bots as you need. Each gets its own `BOT_ID` and `BOT_API_KEY`. Run a separate child bot process per Telegram bot.

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
