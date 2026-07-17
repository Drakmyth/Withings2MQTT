CREATE TABLE measurements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    withings_user_id TEXT NOT NULL,
    source_group_id INTEGER NOT NULL,
    metric TEXT NOT NULL,
    value REAL NOT NULL,
    unit TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(withings_user_id, source_group_id, metric),
    FOREIGN KEY(withings_user_id) REFERENCES linked_users(withings_user_id) ON DELETE CASCADE
);

CREATE INDEX measurements_user_observed_at ON measurements(withings_user_id, observed_at);

CREATE TABLE sync_cursors (
    withings_user_id TEXT NOT NULL,
    category TEXT NOT NULL,
    last_update INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(withings_user_id, category),
    FOREIGN KEY(withings_user_id) REFERENCES linked_users(withings_user_id) ON DELETE CASCADE
);
