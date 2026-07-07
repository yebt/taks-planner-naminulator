package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
  touched_at   INTEGER NOT NULL,
  details       TEXT    NOT NULL DEFAULT '',
  start_date    TEXT    NOT NULL DEFAULT '',
  due_date      TEXT    NOT NULL DEFAULT '',
  work_item_seq INTEGER NOT NULL DEFAULT 0
);`,
		`CREATE TABLE IF NOT EXISTS conversations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  title      TEXT    NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS activity (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL,
  at      INTEGER NOT NULL,
  kind    TEXT    NOT NULL DEFAULT '',
  note    TEXT    NOT NULL DEFAULT ''
);`,
		`CREATE INDEX IF NOT EXISTS idx_activity_at ON activity(at);`,
		`CREATE TABLE IF NOT EXISTS projects (
  slug        TEXT    PRIMARY KEY COLLATE NOCASE,
  name        TEXT    NOT NULL DEFAULT '',
  description TEXT    NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS people (
  nick       TEXT    PRIMARY KEY COLLATE NOCASE,
  name       TEXT    NOT NULL DEFAULT '',
  role       TEXT    NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS notes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  subject_kind TEXT    NOT NULL,
  subject_id   TEXT    NOT NULL,
  at           INTEGER NOT NULL,
  kind         TEXT    NOT NULL DEFAULT '',
  text         TEXT    NOT NULL DEFAULT ''
);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_subject ON notes(subject_kind, subject_id);`,
		`CREATE TABLE IF NOT EXISTS dailies (
  date       TEXT    PRIMARY KEY,
  content    TEXT    NOT NULL DEFAULT '',
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
	// Add columns introduced after the initial schema (for existing DBs).
	for _, col := range []string{"details", "start_date", "due_date", "project"} {
		if err := s.ensureColumn("tasks", col, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("tasks", "work_item_seq", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// Collapse legacy semantic statuses into Plane's 5 state groups (idempotent).
	for old, group := range map[string]string{
		"todo": "unstarted", "in_progress": "started", "blocked": "started",
		"postponed": "backlog", "done": "completed", "rejected": "completed",
	} {
		if _, err := s.db.Exec(`UPDATE tasks SET status=? WHERE status=?`, group, old); err != nil {
			return err
		}
	}
	return nil
}

// ensureColumn adds a column if the table doesn't already have it.
func (s *SQLite) ensureColumn(table, col, decl string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return rows.Close()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + decl)
	return err
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
	details, _ := json.Marshal(t.Details)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (label,type,title,description,status,state,work_item_id,created_at,updated_at,touched_at,details,start_date,due_date,work_item_seq,project)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.Label, string(t.Type), t.Title, t.Description, string(t.Status), t.State, t.WorkItemID,
		t.CreatedAt.Unix(), t.UpdatedAt.Unix(), t.TouchedAt.Unix(), string(details), t.StartDate, t.DueDate, t.WorkItemSeq, t.Project)
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

const selectCols = `id,label,type,title,description,status,state,work_item_id,created_at,updated_at,touched_at,details,start_date,due_date,work_item_seq,project`

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
	if !f.Day.IsZero() {
		start := time.Date(f.Day.Year(), f.Day.Month(), f.Day.Day(), 0, 0, 0, 0, f.Day.Location())
		conds = append(conds, "touched_at >= ? AND touched_at < ?")
		args = append(args, start.Unix(), start.AddDate(0, 0, 1).Unix())
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
	details, _ := json.Marshal(t.Details)
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET label=?,type=?,title=?,description=?,status=?,state=?,work_item_id=?,updated_at=?,touched_at=?,details=?,start_date=?,due_date=?,work_item_seq=?,project=?
		 WHERE id=?`,
		t.Label, string(t.Type), t.Title, t.Description, string(t.Status), t.State, t.WorkItemID,
		now.Unix(), now.Unix(), string(details), t.StartDate, t.DueDate, t.WorkItemSeq, t.Project, t.ID)
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

// Delete removes a task permanently.
func (s *SQLite) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: id=%d", ErrNotFound, id)
	}
	return nil
}

// LogActivity records one interaction with a task.
func (s *SQLite) LogActivity(ctx context.Context, taskID int64, kind, note string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO activity (task_id, at, kind, note) VALUES (?,?,?,?)`,
		taskID, time.Now().Unix(), kind, note)
	return err
}

// TasksWithActivityOn returns the distinct tasks that had any activity on the
// given calendar day, most-recently-touched first.
func (s *SQLite) TasksWithActivityOn(ctx context.Context, day time.Time) ([]domain.Task, error) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+selectCols+` FROM tasks WHERE id IN (
		   SELECT DISTINCT task_id FROM activity WHERE at >= ? AND at < ?
		 ) ORDER BY touched_at DESC, id DESC`,
		start.Unix(), start.AddDate(0, 0, 1).Unix())
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

// ActivityForTask returns a task's activity, oldest first.
func (s *SQLite) ActivityForTask(ctx context.Context, taskID int64) ([]Activity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT at, kind, note FROM activity WHERE task_id = ? ORDER BY at ASC, id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		var a Activity
		var at int64
		if err := rows.Scan(&at, &a.Kind, &a.Note); err != nil {
			return nil, err
		}
		a.At = time.Unix(at, 0).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveDaily upserts the digest for a date.
func (s *SQLite) SaveDaily(ctx context.Context, date, content string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dailies (date, content, updated_at) VALUES (?,?,?)
		 ON CONFLICT(date) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		date, content, time.Now().Unix())
	return err
}

// GetDaily returns the stored digest for a date (ErrNotFound if none).
func (s *SQLite) GetDaily(ctx context.Context, date string) (Daily, error) {
	row := s.db.QueryRowContext(ctx, `SELECT date, content, updated_at FROM dailies WHERE date = ?`, date)
	var d Daily
	var updated int64
	err := row.Scan(&d.Date, &d.Content, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Daily{}, fmt.Errorf("%w: daily %s", ErrNotFound, date)
	}
	if err != nil {
		return Daily{}, err
	}
	d.UpdatedAt = time.Unix(updated, 0).UTC()
	return d, nil
}

// ListDailies returns stored digests, most recent date first.
func (s *SQLite) ListDailies(ctx context.Context) ([]Daily, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT date, content, updated_at FROM dailies ORDER BY date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Daily
	for rows.Next() {
		var d Daily
		var updated int64
		if err := rows.Scan(&d.Date, &d.Content, &updated); err != nil {
			return nil, err
		}
		d.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanRow(sc scanner) (domain.Task, error) {
	var (
		t                         domain.Task
		typ, status, details      string
		created, updated, touched int64
	)
	err := sc.Scan(&t.ID, &t.Label, &typ, &t.Title, &t.Description, &status, &t.State, &t.WorkItemID,
		&created, &updated, &touched, &details, &t.StartDate, &t.DueDate, &t.WorkItemSeq, &t.Project)
	if err != nil {
		return domain.Task{}, err
	}
	t.Type = domain.TaskType(typ)
	t.Status = domain.Status(status)
	t.CreatedAt = time.Unix(created, 0).UTC()
	t.UpdatedAt = time.Unix(updated, 0).UTC()
	t.TouchedAt = time.Unix(touched, 0).UTC()
	if details != "" && details != "null" {
		_ = json.Unmarshal([]byte(details), &t.Details)
	}
	return t, nil
}

func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}
