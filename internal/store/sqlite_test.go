package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

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
		Label: "feat-login", Type: domain.TypeFeat, Title: "Login screen", Status: domain.StatusUnstarted,
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
	if got.Title != "Login screen" || got.Type != domain.TypeFeat || got.Status != domain.StatusUnstarted {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	got.Status = domain.StatusStarted
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.Get(ctx, created.ID)
	if got2.Status != domain.StatusStarted {
		t.Fatalf("update not persisted: %s", got2.Status)
	}

	// second task, different status
	if _, err := s.Create(ctx, domain.Task{Label: "fix-x", Type: domain.TypeFix, Title: "bug", Status: domain.StatusCompleted}); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}

	done, err := s.List(ctx, Filter{Status: domain.StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0].Title != "bug" {
		t.Fatalf("filter failed: %+v", done)
	}
}

func TestDetailsRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	created, err := s.Create(ctx, domain.Task{
		Label: "feat-x", Type: domain.TypeFeat, Title: "X", Status: domain.StatusUnstarted,
		Details: domain.TaskDetails{
			Objective:          "obj",
			AsA:                "user",
			AcceptanceCriteria: []string{"a", "b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Details.Objective != "obj" || got.Details.AsA != "user" {
		t.Fatalf("details roundtrip failed: %+v", got.Details)
	}
	if len(got.Details.AcceptanceCriteria) != 2 {
		t.Fatalf("acceptance criteria not restored: %+v", got.Details.AcceptanceCriteria)
	}
}

func TestMigrateAddsDetailsColumnToOldDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// Simulate a pre-existing DB created before the details column existed.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE tasks (
      id INTEGER PRIMARY KEY AUTOINCREMENT, label TEXT NOT NULL, type TEXT NOT NULL,
      title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
      state TEXT NOT NULL DEFAULT '', work_item_id TEXT NOT NULL DEFAULT '',
      created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, touched_at INTEGER NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := OpenSQLite(path) // migrate() must ALTER TABLE ADD COLUMN details
	if err != nil {
		t.Fatalf("open/migrate on old DB failed: %v", err)
	}
	defer s.Close()
	if _, err := s.Create(context.Background(), domain.Task{
		Label: "l", Type: domain.TypeFeat, Title: "t", Status: domain.StatusUnstarted,
		Details: domain.TaskDetails{Objective: "o"},
	}); err != nil {
		t.Fatalf("create after migration failed: %v", err)
	}
}

func TestDatesRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	created, err := s.Create(ctx, domain.Task{
		Label: "feat-x", Type: domain.TypeFeat, Title: "X", Status: domain.StatusUnstarted,
		StartDate: "2026-06-01", DueDate: "2026-06-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StartDate != "2026-06-01" || got.DueDate != "2026-06-02" {
		t.Fatalf("dates not persisted: %q %q", got.StartDate, got.DueDate)
	}
}

func TestDailyRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.SaveDaily(ctx, "2026-07-03", "Daily:  2026-07-03 JUL\n\nTrabajo:\n  + x"); err != nil {
		t.Fatal(err)
	}
	// upsert overwrites content for the same date
	if err := s.SaveDaily(ctx, "2026-07-03", "edited"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDaily(ctx, "2026-07-04", "another"); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDaily(ctx, "2026-07-03")
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "edited" {
		t.Fatalf("upsert did not overwrite: %q", d.Content)
	}
	list, err := s.ListDailies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Date != "2026-07-04" { // most recent first
		t.Fatalf("list order/size wrong: %+v", list)
	}
	if _, err := s.GetDaily(ctx, "1999-01-01"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListFilterByDay(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	old, err := s.Create(ctx, domain.Task{Label: "a", Type: domain.TypeFeat, Title: "old", Status: domain.StatusUnstarted})
	if err != nil {
		t.Fatal(err)
	}
	// Force this task's touched_at into the past by writing it directly.
	past := time.Now().AddDate(0, 0, -3)
	if _, err := s.db.Exec(`UPDATE tasks SET touched_at=? WHERE id=?`, past.Unix(), old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, domain.Task{Label: "b", Type: domain.TypeFeat, Title: "today", Status: domain.StatusUnstarted}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, Filter{Day: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "today" {
		t.Fatalf("day filter should return only today's task: %+v", got)
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
