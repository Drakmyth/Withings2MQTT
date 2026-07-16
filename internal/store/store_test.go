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
