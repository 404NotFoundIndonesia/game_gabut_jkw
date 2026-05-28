DROP INDEX IF EXISTS idx_sessions_status_updated;
ALTER TABLE game_sessions DROP COLUMN IF EXISTS updated_at;
