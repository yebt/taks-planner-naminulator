package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
)

func newTestStore(t *testing.T) *SQLite {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteCreateGetUpdateList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	created, err := s.Create(ctx, domain.Task{
		Label: "feat-login", Type: domain.TypeFeat, Title: "Login screen", Status: domain.StatusTodo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("expected assigned id")
	}

	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Login screen" || got.Type != domain.TypeFeat || got.Status != domain.StatusTodo {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	got.Status = domain.StatusInProgress
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.Get(ctx, created.ID)
	if got2.Status != domain.StatusInProgress {
		t.Fatalf("update not persisted: %s", got2.Status)
	}

	// second task, different status
	if _, err := s.Create(ctx, domain.Task{Label: "fix-x", Type: domain.TypeFix, Title: "bug", Status: domain.StatusDone}); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}

	done, err := s.List(ctx, Filter{Status: domain.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0].Title != "bug" {
		t.Fatalf("filter failed: %+v", done)
	}
}

func TestSQLiteNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Update(context.Background(), domain.Task{ID: 999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on update, got %v", err)
	}
}
