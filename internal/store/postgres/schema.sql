-- Applied idempotently at boot behind a Postgres advisory lock, so
-- concurrent cold starts on a serverless deployment cannot race the
-- migration against each other.

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    email TEXT,
    name TEXT,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_id)
);

-- token_hash is the SHA-256 of the session cookie, never the token itself -
-- a leaked database row must not be a usable session.
CREATE TABLE IF NOT EXISTS sessions (
    token_hash BYTEA PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS villages (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, tag)
);

CREATE TABLE IF NOT EXISTS snapshots (
    id BIGSERIAL PRIMARY KEY,
    village_id BIGINT NOT NULL REFERENCES villages (id) ON DELETE CASCADE,
    captured_at TIMESTAMPTZ NOT NULL,
    raw JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (village_id, captured_at)
);

CREATE INDEX IF NOT EXISTS snapshots_village_captured_idx ON snapshots (village_id, captured_at DESC);
