CREATE TABLE daily_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    withings_user_id TEXT NOT NULL,
    activity_date TEXT NOT NULL,
    steps INTEGER NOT NULL,
    steps_unit TEXT NOT NULL,
    distance REAL,
    distance_unit TEXT,
    calories REAL,
    calories_unit TEXT,
    elevation REAL,
    elevation_unit TEXT,
    observed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(withings_user_id, activity_date),
    FOREIGN KEY(withings_user_id) REFERENCES linked_users(withings_user_id) ON DELETE CASCADE
);

CREATE INDEX daily_activity_user_observed_at ON daily_activity(withings_user_id, observed_at);
