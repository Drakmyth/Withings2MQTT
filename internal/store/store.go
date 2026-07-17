package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	s := &Store{db: db}
	if err := s.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &version); err != nil {
			return fmt.Errorf("invalid migration name %q", entry.Name())
		}
		var exists bool
		err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&exists)
		if err != nil && !strings.Contains(err.Error(), "no such table") {
			return err
		}
		if exists {
			continue
		}
		sqlText, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(sqlText)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)", version, time.Now().UTC().Format(time.RFC3339Nano))
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

type LinkedUser struct {
	WithingsUserID string
	AccessToken    []byte
	RefreshToken   []byte
	TokenExpiry    time.Time
}

type Measurement struct {
	WithingsUserID string
	SourceGroupID  int64
	Metric         string
	Value          float64
	Unit           string
	ObservedAt     time.Time
}

type MeasurementPage struct {
	Items      []Measurement
	Page       int
	PageSize   int
	TotalItems int
	TotalPages int
}

type DailyActivity struct {
	WithingsUserID string
	Date           string
	Steps          int
	StepsUnit      string
	Distance       *float64
	DistanceUnit   string
	Calories       *float64
	CaloriesUnit   string
	Elevation      *float64
	ElevationUnit  string
	ObservedAt     time.Time
}

func (s *Store) SaveDailyActivity(ctx context.Context, activity DailyActivity) error {
	if err := validateDailyActivity(activity); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := saveDailyActivity(ctx, s.db, activity, now)
	return err
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func saveDailyActivity(ctx context.Context, executor sqlExecutor, activity DailyActivity, now string) (sql.Result, error) {
	result, err := executor.ExecContext(ctx, `INSERT INTO daily_activity
		(withings_user_id, activity_date, steps, steps_unit, distance, distance_unit, calories, calories_unit, elevation, elevation_unit, observed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(withings_user_id, activity_date) DO UPDATE SET
			steps=excluded.steps, steps_unit=excluded.steps_unit,
			distance=excluded.distance, distance_unit=excluded.distance_unit,
			calories=excluded.calories, calories_unit=excluded.calories_unit,
			elevation=excluded.elevation, elevation_unit=excluded.elevation_unit,
			observed_at=excluded.observed_at, updated_at=excluded.updated_at
		WHERE daily_activity.steps != excluded.steps
			OR daily_activity.steps_unit != excluded.steps_unit
			OR daily_activity.distance IS NOT excluded.distance
			OR daily_activity.distance_unit IS NOT excluded.distance_unit
			OR daily_activity.calories IS NOT excluded.calories
			OR daily_activity.calories_unit IS NOT excluded.calories_unit
			OR daily_activity.elevation IS NOT excluded.elevation
			OR daily_activity.elevation_unit IS NOT excluded.elevation_unit
			OR daily_activity.observed_at != excluded.observed_at`,
		activity.WithingsUserID, activity.Date, activity.Steps, activity.StepsUnit,
		activity.Distance, nullableUnit(activity.Distance, activity.DistanceUnit),
		activity.Calories, nullableUnit(activity.Calories, activity.CaloriesUnit),
		activity.Elevation, nullableUnit(activity.Elevation, activity.ElevationUnit),
		activity.ObservedAt.UTC().Format(time.RFC3339Nano), now, now)
	if err != nil {
		return nil, fmt.Errorf("save daily activity: %w", err)
	}
	return result, nil
}

func validateDailyActivity(activity DailyActivity) error {
	if activity.WithingsUserID == "" || activity.Date == "" || activity.StepsUnit == "" || activity.ObservedAt.IsZero() {
		return fmt.Errorf("daily activity user ID, date, steps unit, and observed time are required")
	}
	if activity.Steps < 0 {
		return fmt.Errorf("daily activity steps cannot be negative")
	}
	for _, metric := range []struct {
		value *float64
		unit  string
	}{{activity.Distance, activity.DistanceUnit}, {activity.Calories, activity.CaloriesUnit}, {activity.Elevation, activity.ElevationUnit}} {
		if metric.value != nil && metric.unit == "" {
			return fmt.Errorf("daily activity optional metric unit is required when value is supplied")
		}
	}
	return nil
}

func nullableUnit(value *float64, unit string) any {
	if value == nil {
		return nil
	}
	return unit
}

func (s *Store) ImportDailyActivities(ctx context.Context, userID string, activities []DailyActivity, lastUpdate int64) (int, error) {
	for i := range activities {
		activities[i].WithingsUserID = userID
		if err := validateDailyActivity(activities[i]); err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin daily activity import: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	changed := 0
	for _, activity := range activities {
		result, err := saveDailyActivity(ctx, tx, activity, now)
		if err != nil {
			return 0, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count imported daily activity: %w", err)
		}
		changed += int(rows)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_cursors(withings_user_id, category, last_update, updated_at)
		VALUES (?, 'activity', ?, ?)
		ON CONFLICT(withings_user_id, category) DO UPDATE SET last_update=MAX(sync_cursors.last_update, excluded.last_update), updated_at=excluded.updated_at`, userID, lastUpdate, now); err != nil {
		return 0, fmt.Errorf("update activity cursor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit daily activity import: %w", err)
	}
	return changed, nil
}

func (s *Store) DailyActivities(ctx context.Context, userID string) ([]DailyActivity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT withings_user_id, activity_date, steps, steps_unit,
		distance, distance_unit, calories, calories_unit, elevation, elevation_unit, observed_at
		FROM daily_activity WHERE withings_user_id = ? ORDER BY observed_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("query daily activity: %w", err)
	}
	defer rows.Close()
	var activities []DailyActivity
	for rows.Next() {
		var activity DailyActivity
		var observedAt string
		var distance, calories, elevation sql.NullFloat64
		var distanceUnit, caloriesUnit, elevationUnit sql.NullString
		if err := rows.Scan(&activity.WithingsUserID, &activity.Date, &activity.Steps, &activity.StepsUnit,
			&distance, &distanceUnit, &calories, &caloriesUnit, &elevation, &elevationUnit, &observedAt); err != nil {
			return nil, fmt.Errorf("scan daily activity: %w", err)
		}
		activity.Distance, activity.DistanceUnit = optionalMetric(distance, distanceUnit)
		activity.Calories, activity.CaloriesUnit = optionalMetric(calories, caloriesUnit)
		activity.Elevation, activity.ElevationUnit = optionalMetric(elevation, elevationUnit)
		activity.ObservedAt, err = time.Parse(time.RFC3339Nano, observedAt)
		if err != nil {
			return nil, fmt.Errorf("parse daily activity time: %w", err)
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query daily activity: %w", err)
	}
	return activities, nil
}

func optionalMetric(value sql.NullFloat64, unit sql.NullString) (*float64, string) {
	if !value.Valid {
		return nil, ""
	}
	return &value.Float64, unit.String
}

type SyncCursorStatus struct {
	LastUpdate int64
	UpdatedAt  time.Time
}

func (s *Store) ImportMeasurements(ctx context.Context, userID, category string, measurements []Measurement, lastUpdate int64) (int, error) {
	if userID == "" || category == "" {
		return 0, fmt.Errorf("user ID and category are required")
	}
	for _, measurement := range measurements {
		if measurement.Metric == "" || measurement.Unit == "" || measurement.ObservedAt.IsZero() {
			return 0, fmt.Errorf("measurement metric, unit, and observed time are required")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin measurement import: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	inserted := 0
	for _, measurement := range measurements {
		result, err := tx.ExecContext(ctx, `INSERT INTO measurements
			(withings_user_id, source_group_id, metric, value, unit, observed_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(withings_user_id, source_group_id, metric) DO UPDATE SET
				value=excluded.value, unit=excluded.unit, observed_at=excluded.observed_at
			WHERE measurements.value != excluded.value OR measurements.unit != excluded.unit OR measurements.observed_at != excluded.observed_at`, userID, measurement.SourceGroupID, measurement.Metric, measurement.Value, measurement.Unit, measurement.ObservedAt.UTC().Format(time.RFC3339Nano), now)
		if err != nil {
			return 0, fmt.Errorf("insert measurement: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count inserted measurement: %w", err)
		}
		inserted += int(rows)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_cursors(withings_user_id, category, last_update, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(withings_user_id, category) DO UPDATE SET last_update=MAX(sync_cursors.last_update, excluded.last_update), updated_at=excluded.updated_at`, userID, category, lastUpdate, now); err != nil {
		return 0, fmt.Errorf("update sync cursor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit measurement import: %w", err)
	}
	return inserted, nil
}

func (s *Store) SyncCursor(ctx context.Context, userID, category string) (int64, error) {
	var cursor int64
	err := s.db.QueryRowContext(ctx, "SELECT last_update FROM sync_cursors WHERE withings_user_id = ? AND category = ?", userID, category).Scan(&cursor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cursor, err
}

func (s *Store) Measurements(ctx context.Context, userID string) ([]Measurement, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT withings_user_id, source_group_id, metric, value, unit, observed_at FROM measurements WHERE withings_user_id = ? ORDER BY observed_at, metric", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var measurements []Measurement
	for rows.Next() {
		var measurement Measurement
		var observedAt string
		if err := rows.Scan(&measurement.WithingsUserID, &measurement.SourceGroupID, &measurement.Metric, &measurement.Value, &measurement.Unit, &observedAt); err != nil {
			return nil, err
		}
		measurement.ObservedAt, err = time.Parse(time.RFC3339Nano, observedAt)
		if err != nil {
			return nil, fmt.Errorf("parse measurement time: %w", err)
		}
		measurements = append(measurements, measurement)
	}
	return measurements, rows.Err()
}

func (s *Store) LatestMeasurements(ctx context.Context, userID string) ([]Measurement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.withings_user_id, m.source_group_id, m.metric, m.value, m.unit, m.observed_at
		FROM measurements m
		WHERE m.withings_user_id = ? AND m.id = (
			SELECT latest.id FROM measurements latest
			WHERE latest.withings_user_id = m.withings_user_id AND latest.metric = m.metric
			ORDER BY latest.observed_at DESC, latest.id DESC LIMIT 1
		)
		ORDER BY CASE m.metric WHEN 'weight' THEN 0 ELSE 1 END, m.metric`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMeasurements(rows)
}

func (s *Store) MeasurementHistory(ctx context.Context, userID, metric string, page, pageSize int) (MeasurementPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	filter := "withings_user_id = ?"
	args := []any{userID}
	if metric != "" {
		filter += " AND metric = ?"
		args = append(args, metric)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM measurements WHERE "+filter, args...).Scan(&total); err != nil {
		return MeasurementPage{}, err
	}
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}
	queryArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT withings_user_id, source_group_id, metric, value, unit, observed_at
		FROM measurements WHERE `+filter+` ORDER BY observed_at DESC, source_group_id DESC, metric LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return MeasurementPage{}, err
	}
	defer rows.Close()
	items, err := scanMeasurements(rows)
	return MeasurementPage{Items: items, Page: page, PageSize: pageSize, TotalItems: total, TotalPages: totalPages}, err
}

func (s *Store) SyncStatus(ctx context.Context, userID, category string) (SyncCursorStatus, error) {
	var status SyncCursorStatus
	var updatedAt string
	err := s.db.QueryRowContext(ctx, "SELECT last_update, updated_at FROM sync_cursors WHERE withings_user_id = ? AND category = ?", userID, category).Scan(&status.LastUpdate, &updatedAt)
	if err == sql.ErrNoRows {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	return status, err
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanMeasurements(rows rowScanner) ([]Measurement, error) {
	var measurements []Measurement
	for rows.Next() {
		var measurement Measurement
		var observedAt string
		if err := rows.Scan(&measurement.WithingsUserID, &measurement.SourceGroupID, &measurement.Metric, &measurement.Value, &measurement.Unit, &observedAt); err != nil {
			return nil, err
		}
		var err error
		measurement.ObservedAt, err = time.Parse(time.RFC3339Nano, observedAt)
		if err != nil {
			return nil, fmt.Errorf("parse measurement time: %w", err)
		}
		measurements = append(measurements, measurement)
	}
	return measurements, rows.Err()
}

func (s *Store) SaveLinkedUser(ctx context.Context, user LinkedUser) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO linked_users(withings_user_id, access_token, refresh_token, token_expiry, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(withings_user_id) DO UPDATE SET access_token=excluded.access_token, refresh_token=excluded.refresh_token, token_expiry=excluded.token_expiry, updated_at=excluded.updated_at`,
		user.WithingsUserID, user.AccessToken, user.RefreshToken, user.TokenExpiry.UTC().Format(time.RFC3339Nano), now, now)
	return err
}

func (s *Store) LinkedUserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM linked_users").Scan(&count)
	return count, err
}

func (s *Store) LinkedUsers(ctx context.Context) ([]LinkedUser, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT withings_user_id, access_token, refresh_token, token_expiry FROM linked_users ORDER BY withings_user_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []LinkedUser
	for rows.Next() {
		var user LinkedUser
		var expiry string
		if err := rows.Scan(&user.WithingsUserID, &user.AccessToken, &user.RefreshToken, &expiry); err != nil {
			return nil, err
		}
		user.TokenExpiry, err = time.Parse(time.RFC3339Nano, expiry)
		if err != nil {
			return nil, fmt.Errorf("parse token expiry: %w", err)
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) LinkedUser(ctx context.Context, id string) (LinkedUser, error) {
	var user LinkedUser
	var expiry string
	err := s.db.QueryRowContext(ctx, "SELECT withings_user_id, access_token, refresh_token, token_expiry FROM linked_users WHERE withings_user_id = ?", id).Scan(&user.WithingsUserID, &user.AccessToken, &user.RefreshToken, &expiry)
	if err != nil {
		return LinkedUser{}, err
	}
	user.TokenExpiry, err = time.Parse(time.RFC3339Nano, expiry)
	return user, err
}

func (s *Store) DeleteLinkedUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM linked_users WHERE withings_user_id = ?", id)
	return err
}

func (s *Store) RawAccessToken(ctx context.Context, id string) ([]byte, error) {
	var value []byte
	err := s.db.QueryRowContext(ctx, "SELECT access_token FROM linked_users WHERE withings_user_id = ?", id).Scan(&value)
	return value, err
}
