CREATE TABLE IF NOT EXISTS bot_games (
    bot_id      UUID        NOT NULL REFERENCES bots(id)  ON DELETE CASCADE,
    game_id     UUID        NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bot_id, game_id)
);

CREATE INDEX IF NOT EXISTS idx_bot_games_bot_id ON bot_games (bot_id);
