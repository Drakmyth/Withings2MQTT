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
