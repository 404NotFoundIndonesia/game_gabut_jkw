CREATE TYPE session_status AS ENUM ('CREATED', 'WAITING', 'IN_PROGRESS', 'FINISHED', 'ARCHIVED');

CREATE TABLE IF NOT EXISTS game_sessions (
    id         UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id     UUID           NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    game_id    UUID           NOT NULL REFERENCES games(id),
    chat_id    BIGINT         NOT NULL,
    status     session_status NOT NULL DEFAULT 'CREATED',
    state      JSONB          NOT NULL DEFAULT '{}',
    started_at TIMESTAMPTZ,
    ended_at   TIMESTAMPTZ,
    created_at TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_bot_status      ON game_sessions (bot_id, status);
CREATE INDEX IF NOT EXISTS idx_sessions_bot_chat_status ON game_sessions (bot_id, chat_id, status);

CREATE TABLE IF NOT EXISTS player_sessions (
    session_id       UUID         NOT NULL REFERENCES game_sessions(id) ON DELETE CASCADE,
    telegram_user_id BIGINT       NOT NULL,
    display_name     VARCHAR(100) NOT NULL,
    score            INT          NOT NULL DEFAULT 0,
    is_winner        BOOLEAN      NOT NULL DEFAULT FALSE,
    joined_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, telegram_user_id)
);
