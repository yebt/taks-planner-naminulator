package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO
)

// ErrNotFound is returned when a task id does not exist.
var ErrNotFound = errors.New("task not found")

// SQLite is the SQLite-backed TaskStore. Timestamps are stored as Unix seconds
// to avoid driver-specific time parsing.
type SQLite struct{ db *sql.DB }

// OpenSQLite opens (creating if needed) the database at path.
func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &SQLite{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  label        TEXT    NOT NULL,
  type         TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  description  TEXT    NOT NULL DEFAULT '',
  status       TEXT    NOT NULL,
  state        TEXT    NOT NULL DEFAULT '',
  work_item_id TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  touched_at   INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS conversations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  title      TEXT    NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS conv_messages (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  conversation_id INTEGER NOT NULL,
  idx             INTEGER NOT NULL,
  role            TEXT    NOT NULL,
  content         TEXT    NOT NULL DEFAULT '',
  tool_calls      TEXT    NOT NULL DEFAULT '',
  tool_call_id    TEXT    NOT NULL DEFAULT '',
  name            TEXT    NOT NULL DEFAULT ''
);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the underlying database.
func (s *SQLite) Close() error { return s.db.Close() }

// Create inserts a task and returns it with its assigned id.
func (s *SQLite) Create(ctx context.Context, t domain.Task) (domain.Task, error) {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.TouchedAt.IsZero() {
		t.TouchedAt = now
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (label,type,title,description,status,state,work_item_id,created_at,updated_at,touched_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		t.Label, string(t.Type), t.Title, t.Description, string(t.Status), t.State, t.WorkItemID,
		t.CreatedAt.Unix(), t.UpdatedAt.Unix(), t.TouchedAt.Unix())
	if err != nil {
		return domain.Task{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Task{}, err
	}
	t.ID = id
	return t, nil
}

const selectCols = `id,label,type,title,description,status,state,work_item_id,created_at,updated_at,touched_at`

// Get fetches a single task by id.
func (s *SQLite) Get(ctx context.Context, id int64) (domain.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+selectCols+` FROM tasks WHERE id = ?`, id)
	t, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, fmt.Errorf("%w: id=%d", ErrNotFound, id)
	}
	return t, err
}

// List returns tasks matching the filter, most-recently-touched first.
func (s *SQLite) List(ctx context.Context, f Filter) ([]domain.Task, error) {
	q := `SELECT ` + selectCols + ` FROM tasks`
	var conds []string
	var args []any
	if f.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, string(f.Status))
	}
	if f.TouchedToday {
		conds = append(conds, "touched_at >= ?")
		args = append(args, startOfToday().Unix())
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY touched_at DESC, id DESC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Update writes a task and bumps updated_at/touched_at to now.
func (s *SQLite) Update(ctx context.Context, t domain.Task) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET label=?,type=?,title=?,description=?,status=?,state=?,work_item_id=?,updated_at=?,touched_at=?
		 WHERE id=?`,
		t.Label, string(t.Type), t.Title, t.Description, string(t.Status), t.State, t.WorkItemID,
		now.Unix(), now.Unix(), t.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: id=%d", ErrNotFound, t.ID)
	}
	return nil
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanRow(sc scanner) (domain.Task, error) {
	var (
		t                         domain.Task
		typ, status               string
		created, updated, touched int64
	)
	err := sc.Scan(&t.ID, &t.Label, &typ, &t.Title, &t.Description, &status, &t.State, &t.WorkItemID,
		&created, &updated, &touched)
	if err != nil {
		return domain.Task{}, err
	}
	t.Type = domain.TaskType(typ)
	t.Status = domain.Status(status)
	t.CreatedAt = time.Unix(created, 0).UTC()
	t.UpdatedAt = time.Unix(updated, 0).UTC()
	t.TouchedAt = time.Unix(touched, 0).UTC()
	return t, nil
}

func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}
