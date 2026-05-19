CREATE TABLE IF NOT EXISTS games (
    id          UUID         PRIMARY KEY,
    slug        VARCHAR(50)  NOT NULL UNIQUE,
    name        VARCHAR(100) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    min_players INT          NOT NULL DEFAULT 2,
    max_players INT          NOT NULL DEFAULT 10
);

-- Stable UUIDs — hardcoded so they never change across environments.
INSERT INTO games (id, slug, name, description, min_players, max_players) VALUES
(
    '018e7d3b-0001-7000-8000-000000000001',
    'uno',
    'Uno',
    'Standard Uno card game. Multi-player, turn-based. First to empty hand wins.',
    2,
    10
),
(
    '018e7d3b-0002-7000-8000-000000000002',
    'sambung_kata',
    'Sambung Kata',
    'Word-chaining game validated against KBBI. Each word must start with the last letter of the previous word.',
    2,
    20
),
(
    '018e7d3b-0003-7000-8000-000000000003',
    'truth_or_date',
    'Truth or Date',
    'Truth or Dare party game. Players pick Truth or Date and answer random questions.',
    2,
    20
)
ON CONFLICT (id) DO NOTHING;
