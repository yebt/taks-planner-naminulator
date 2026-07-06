package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
)

// --- projects ---

// UpsertProject creates or updates a project by slug (case-insensitive).
func (s *SQLite) UpsertProject(ctx context.Context, p domain.Project) (domain.Project, error) {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	// Empty incoming fields keep the existing value (so an upsert to touch one
	// field doesn't wipe the others).
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (slug,name,description,created_at,updated_at) VALUES (?,?,?,?,?)
		 ON CONFLICT(slug) DO UPDATE SET
		   name        = CASE WHEN excluded.name        != '' THEN excluded.name        ELSE name        END,
		   description = CASE WHEN excluded.description != '' THEN excluded.description ELSE description END,
		   updated_at  = excluded.updated_at`,
		p.Slug, p.Name, p.Description, p.CreatedAt.Unix(), p.UpdatedAt.Unix())
	if err != nil {
		return domain.Project{}, err
	}
	return p, nil
}

// GetProject returns a project with its notes (ErrNotFound if missing).
func (s *SQLite) GetProject(ctx context.Context, slug string) (domain.Project, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT slug,name,description,created_at,updated_at FROM projects WHERE slug = ?`, slug)
	var p domain.Project
	var c, u int64
	err := row.Scan(&p.Slug, &p.Name, &p.Description, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Project{}, fmt.Errorf("%w: project %s", ErrNotFound, slug)
	}
	if err != nil {
		return domain.Project{}, err
	}
	p.CreatedAt, p.UpdatedAt = time.Unix(c, 0).UTC(), time.Unix(u, 0).UTC()
	if p.Notes, err = s.notesFor(ctx, "project", p.Slug); err != nil {
		return domain.Project{}, err
	}
	return p, nil
}

// ListProjects returns all projects (without notes), by slug.
func (s *SQLite) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT slug,name,description,created_at,updated_at FROM projects ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var c, u int64
		if err := rows.Scan(&p.Slug, &p.Name, &p.Description, &c, &u); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = time.Unix(c, 0).UTC(), time.Unix(u, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddProjectNote appends a note to a project and bumps its updated_at.
func (s *SQLite) AddProjectNote(ctx context.Context, slug, kind, text string) error {
	return s.addNote(ctx, "project", slug, kind, text, "projects", "slug")
}

// --- people ---

// UpsertPerson creates or updates a person by nick (case-insensitive).
func (s *SQLite) UpsertPerson(ctx context.Context, p domain.Person) (domain.Person, error) {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO people (nick,name,role,created_at,updated_at) VALUES (?,?,?,?,?)
		 ON CONFLICT(nick) DO UPDATE SET
		   name       = CASE WHEN excluded.name != '' THEN excluded.name ELSE name END,
		   role       = CASE WHEN excluded.role != '' THEN excluded.role ELSE role END,
		   updated_at = excluded.updated_at`,
		p.Nick, p.Name, p.Role, p.CreatedAt.Unix(), p.UpdatedAt.Unix())
	if err != nil {
		return domain.Person{}, err
	}
	return p, nil
}

// GetPerson returns a person with their notes (ErrNotFound if missing).
func (s *SQLite) GetPerson(ctx context.Context, nick string) (domain.Person, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT nick,name,role,created_at,updated_at FROM people WHERE nick = ?`, nick)
	var p domain.Person
	var c, u int64
	err := row.Scan(&p.Nick, &p.Name, &p.Role, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Person{}, fmt.Errorf("%w: person %s", ErrNotFound, nick)
	}
	if err != nil {
		return domain.Person{}, err
	}
	p.CreatedAt, p.UpdatedAt = time.Unix(c, 0).UTC(), time.Unix(u, 0).UTC()
	if p.Notes, err = s.notesFor(ctx, "person", p.Nick); err != nil {
		return domain.Person{}, err
	}
	return p, nil
}

// ListPeople returns all people (without notes), by nick.
func (s *SQLite) ListPeople(ctx context.Context) ([]domain.Person, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT nick,name,role,created_at,updated_at FROM people ORDER BY nick`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Person
	for rows.Next() {
		var p domain.Person
		var c, u int64
		if err := rows.Scan(&p.Nick, &p.Name, &p.Role, &c, &u); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = time.Unix(c, 0).UTC(), time.Unix(u, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddPersonNote appends a note to a person and bumps their updated_at.
func (s *SQLite) AddPersonNote(ctx context.Context, nick, kind, text string) error {
	return s.addNote(ctx, "person", nick, kind, text, "people", "nick")
}

// --- shared notes ---

// addNote inserts a note for a subject and bumps the subject's updated_at.
// table and idCol are internal constants, never user input.
func (s *SQLite) addNote(ctx context.Context, subjectKind, id, kind, text, table, idCol string) error {
	now := time.Now().Unix()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO notes (subject_kind,subject_id,at,kind,text) VALUES (?,?,?,?,?)`,
		subjectKind, id, now, kind, text); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE `+table+` SET updated_at=? WHERE `+idCol+` = ?`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %s %s", ErrNotFound, subjectKind, id)
	}
	return nil
}

func (s *SQLite) notesFor(ctx context.Context, subjectKind, id string) ([]domain.Note, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT at,kind,text FROM notes WHERE subject_kind=? AND subject_id=? COLLATE NOCASE ORDER BY at ASC, id ASC`,
		subjectKind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Note
	for rows.Next() {
		var n domain.Note
		var at int64
		if err := rows.Scan(&at, &n.Kind, &n.Text); err != nil {
			return nil, err
		}
		n.At = time.Unix(at, 0).UTC()
		out = append(out, n)
	}
	return out, rows.Err()
}
