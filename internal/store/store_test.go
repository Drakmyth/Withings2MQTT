package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationsAndLinkedUserPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.SaveLinkedUser(ctx, LinkedUser{WithingsUserID: "42", AccessToken: []byte("encrypted-access"), RefreshToken: []byte("encrypted-refresh"), TokenExpiry: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var createdAt, updatedAt string
	if err := s.db.QueryRowContext(ctx, "SELECT created_at, updated_at FROM linked_users WHERE withings_user_id = ?", "42").Scan(&createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if createdAt != updatedAt {
		t.Fatalf("created_at = %s, updated_at = %s; expected identical timestamps", createdAt, updatedAt)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrations must be idempotent: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	count, err := s.LinkedUserCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d", count)
	}
}

func TestMeasurementsPersistDeduplicateAndAdvanceCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "measurements.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.SaveLinkedUser(ctx, LinkedUser{WithingsUserID: "user-1", AccessToken: []byte("a"), RefreshToken: []byte("r"), TokenExpiry: time.Now()}); err != nil {
		t.Fatal(err)
	}
	sample := Measurement{SourceGroupID: 42, Metric: "weight", Value: 80.425, Unit: "kg", ObservedAt: time.Unix(1700000000, 0)}
	inserted, err := s.ImportMeasurements(ctx, "user-1", "weight", []Measurement{sample}, 100)
	if err != nil || inserted != 1 {
		t.Fatalf("first import = %d, %v", inserted, err)
	}
	inserted, err = s.ImportMeasurements(ctx, "user-1", "weight", []Measurement{sample}, 200)
	if err != nil || inserted != 0 {
		t.Fatalf("duplicate import = %d, %v", inserted, err)
	}
	sample.Value = 80.5
	inserted, err = s.ImportMeasurements(ctx, "user-1", "weight", []Measurement{sample}, 300)
	if err != nil || inserted != 1 {
		t.Fatalf("corrected import = %d, %v", inserted, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	measurements, err := s.Measurements(ctx, "user-1")
	if err != nil || len(measurements) != 1 || measurements[0].Metric != "weight" || measurements[0].Value != 80.5 {
		t.Fatalf("persisted measurements = %+v, %v", measurements, err)
	}
	cursor, err := s.SyncCursor(ctx, "user-1", "weight")
	if err != nil || cursor != 300 {
		t.Fatalf("cursor = %d, %v", cursor, err)
	}
}

func TestFailedMeasurementImportDoesNotAdvanceCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.ImportMeasurements(ctx, "user-1", "weight", []Measurement{{Metric: "weight"}}, 100); err == nil {
		t.Fatal("expected invalid measurement error")
	}
	cursor, err := s.SyncCursor(ctx, "user-1", "weight")
	if err != nil || cursor != 0 {
		t.Fatalf("cursor advanced after failure: %d, %v", cursor, err)
	}
}

func TestMeasurementHistoryIsFilteredPaginatedAndLatest(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SaveLinkedUser(ctx, LinkedUser{WithingsUserID: "user-1", AccessToken: []byte("a"), RefreshToken: []byte("r"), TokenExpiry: time.Now()}); err != nil {
		t.Fatal(err)
	}
	measurements := []Measurement{
		{SourceGroupID: 1, Metric: "weight", Value: 80, Unit: "kg", ObservedAt: time.Unix(100, 0)},
		{SourceGroupID: 2, Metric: "weight", Value: 81, Unit: "kg", ObservedAt: time.Unix(200, 0)},
		{SourceGroupID: 2, Metric: "fat_ratio", Value: 20, Unit: "%", ObservedAt: time.Unix(200, 0)},
	}
	if _, err := s.ImportMeasurements(ctx, "user-1", "weight", measurements, 300); err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestMeasurements(ctx, "user-1")
	if err != nil || len(latest) != 2 || latest[0].Metric != "weight" || latest[0].Value != 81 {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	page, err := s.MeasurementHistory(ctx, "user-1", "weight", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 2 || page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].Value != 81 {
		t.Fatalf("page = %+v", page)
	}
	page, err = s.MeasurementHistory(ctx, "user-1", "weight", 2, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Value != 80 {
		t.Fatalf("second page = %+v, %v", page, err)
	}
	status, err := s.SyncStatus(ctx, "user-1", "weight")
	if err != nil || status.LastUpdate != 300 || status.UpdatedAt.IsZero() {
		t.Fatalf("sync status = %+v, %v", status, err)
	}
}

func TestDailyStepsPersistWithTimestampAndUnits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.SaveLinkedUser(ctx, LinkedUser{WithingsUserID: "user-1", AccessToken: []byte("a"), RefreshToken: []byte("r"), TokenExpiry: time.Now()}); err != nil {
		t.Fatal(err)
	}
	observedAt := time.Date(2026, 7, 15, 0, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	distance, calories, elevation := 6123.5, 428.2, 14.5
	activity := DailyActivity{WithingsUserID: "user-1", Date: "2026-07-15", Steps: 8123, StepsUnit: "steps", Distance: &distance, DistanceUnit: "m", Calories: &calories, CaloriesUnit: "kcal", Elevation: &elevation, ElevationUnit: "m", ObservedAt: observedAt}
	if err := s.SaveDailyActivity(ctx, activity); err != nil {
		t.Fatal(err)
	}
	activity.Steps = 8124
	if err := s.SaveDailyActivity(ctx, activity); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	activities, err := s.DailyActivities(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 1 || activities[0].Steps != 8124 || activities[0].StepsUnit != "steps" {
		t.Fatalf("activities = %+v", activities)
	}
	if !activities[0].ObservedAt.Equal(observedAt) {
		t.Fatalf("observed at = %v, want %v", activities[0].ObservedAt, observedAt)
	}
	if activities[0].Distance == nil || *activities[0].Distance != distance || activities[0].DistanceUnit != "m" || activities[0].Calories == nil || activities[0].Elevation == nil {
		t.Fatalf("optional metrics = %+v", activities[0])
	}
}

func TestDailyStepsValidation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "activity-validation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	invalid := []DailyActivity{
		{},
		{WithingsUserID: "user-1", Date: "2026-07-15", Steps: -1, StepsUnit: "steps", ObservedAt: time.Now()},
		{WithingsUserID: "user-1", Date: "2026-07-15", StepsUnit: "steps", Distance: new(float64), ObservedAt: time.Now()},
	}
	for _, activity := range invalid {
		if err := s.SaveDailyActivity(context.Background(), activity); err == nil {
			t.Fatalf("SaveDailyActivity(%+v) succeeded", activity)
		}
	}
}

func TestDailyActivityImportAdvancesCursorAtomically(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "activity-import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SaveLinkedUser(ctx, LinkedUser{WithingsUserID: "user-1", AccessToken: []byte("a"), RefreshToken: []byte("r"), TokenExpiry: time.Now()}); err != nil {
		t.Fatal(err)
	}
	activities := []DailyActivity{{Date: "2026-07-15", Steps: 100, StepsUnit: "steps", ObservedAt: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)}}
	changed, err := s.ImportDailyActivities(ctx, "user-1", activities, 200)
	if err != nil || changed != 1 {
		t.Fatalf("import = %d, %v", changed, err)
	}
	changed, err = s.ImportDailyActivities(ctx, "user-1", activities, 300)
	if err != nil || changed != 0 {
		t.Fatalf("duplicate import = %d, %v", changed, err)
	}
	if cursor, err := s.SyncCursor(ctx, "user-1", "activity"); err != nil || cursor != 300 {
		t.Fatalf("cursor = %d, %v", cursor, err)
	}
	invalid := []DailyActivity{{Date: "2026-07-16", StepsUnit: "steps"}}
	if _, err := s.ImportDailyActivities(ctx, "user-1", invalid, 400); err == nil {
		t.Fatal("invalid import succeeded")
	}
	if cursor, _ := s.SyncCursor(ctx, "user-1", "activity"); cursor != 300 {
		t.Fatalf("cursor advanced after failed import: %d", cursor)
	}
}
