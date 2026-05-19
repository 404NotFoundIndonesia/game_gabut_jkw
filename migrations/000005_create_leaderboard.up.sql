CREATE TABLE IF NOT EXISTS leaderboard_entries (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id           UUID        NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    game_id          UUID        NOT NULL REFERENCES games(id),
    telegram_user_id BIGINT      NOT NULL,
    display_name     VARCHAR(100) NOT NULL,
    total_score      INT         NOT NULL DEFAULT 0,
    games_played     INT         NOT NULL DEFAULT 0,
    wins             INT         NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (bot_id, game_id, telegram_user_id)
);

CREATE INDEX IF NOT EXISTS idx_lb_bot_score      ON leaderboard_entries (bot_id, total_score DESC);
CREATE INDEX IF NOT EXISTS idx_lb_bot_game_score ON leaderboard_entries (bot_id, game_id, total_score DESC);
CREATE INDEX IF NOT EXISTS idx_lb_game_score     ON leaderboard_entries (game_id, total_score DESC);
CREATE INDEX IF NOT EXISTS idx_lb_global_score   ON leaderboard_entries (total_score DESC);

-- Idempotency guard: prevents double-counting scores for the same session.
CREATE TABLE IF NOT EXISTS leaderboard_commits (
    session_id   UUID        PRIMARY KEY,
    committed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
