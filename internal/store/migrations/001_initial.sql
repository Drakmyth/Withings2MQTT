CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS linked_users (
    withings_user_id TEXT PRIMARY KEY,
    access_token BLOB NOT NULL,
    refresh_token BLOB NOT NULL,
    token_expiry TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
