ALTER TABLE game_sessions
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE game_sessions
    SET updated_at = COALESCE(ended_at, started_at, created_at);

CREATE INDEX IF NOT EXISTS idx_sessions_status_updated
    ON game_sessions (status, updated_at);
