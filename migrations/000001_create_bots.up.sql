CREATE TABLE IF NOT EXISTS bots (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name         VARCHAR(100) NOT NULL,
    token        TEXT        NOT NULL,          -- AES-256-GCM encrypted Telegram bot token
    token_hash   VARCHAR(64) NOT NULL UNIQUE,   -- SHA-256 hex of raw token for O(1) BotAuth lookup
    telegram_id  BIGINT      NOT NULL UNIQUE,
    active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bots_telegram_id ON bots (telegram_id);
CREATE INDEX IF NOT EXISTS idx_bots_active     ON bots (active);
