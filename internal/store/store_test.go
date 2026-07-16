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
