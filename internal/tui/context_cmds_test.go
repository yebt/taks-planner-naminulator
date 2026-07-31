package tui

// /project and /person: note parsing, subcommand dispatch, and the listings.
// What matters is what ends up in the context store, so the assertions read it
// back rather than trusting the confirmation on screen.

import (
	"context"
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

// contextModel wires a real context store into the model and drops the startup
// banner so the entries left behind belong to the command under test.
func contextModel(t *testing.T) (*chatModel, store.ContextStore) {
	t.Helper()
	m, st := newTestModel(t)
	cs, ok := st.(store.ContextStore)
	if !ok {
		t.Fatal("the sqlite store should implement ContextStore")
	}
	m.deps.Context = cs
	m.entries = nil
	return m, cs
}

// lastEntry is the entry the command just produced.
func lastEntry(t *testing.T, m *chatModel) entry {
	t.Helper()
	if len(m.entries) == 0 {
		t.Fatal("the command produced no entry at all")
	}
	return m.entries[len(m.entries)-1]
}

func TestParseNoteArgs(t *testing.T) {
	cases := []struct {
		name     string
		rest     []string
		wantKind string
		wantText string
	}{
		{
			name:     "an explicit info kind is taken out of the text",
			rest:     strings.Fields("info el stack quedó en Go"),
			wantKind: "info",
			wantText: "el stack quedó en Go",
		},
		{
			name:     "an explicit decision kind is taken out of the text",
			rest:     strings.Fields("decision migramos a SQLite"),
			wantKind: "decision",
			wantText: "migramos a SQLite",
		},
		{
			name:     "an explicit change kind is taken out of the text",
			rest:     strings.Fields("change cambió el owner del repo"),
			wantKind: "change",
			wantText: "cambió el owner del repo",
		},
		{
			name:     "an omitted kind defaults to info and keeps the whole text",
			rest:     strings.Fields("el deploy salió sin incidentes"),
			wantKind: "info",
			wantText: "el deploy salió sin incidentes",
		},
		{
			name:     "a word that only looks like a kind stays in the text",
			rest:     strings.Fields("changes pendientes de review"),
			wantKind: "info",
			wantText: "changes pendientes de review",
		},
		{
			name:     "a note that is only the word decision is text, not a kind",
			rest:     []string{"decision"},
			wantKind: "info",
			wantText: "decision",
		},
		{
			name:     "no arguments yield no text, so the caller can reject it",
			rest:     nil,
			wantKind: "info",
			wantText: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, text := parseNoteArgs(tc.rest)
			if kind != tc.wantKind || text != tc.wantText {
				t.Fatalf("parseNoteArgs(%q) = (%q, %q), want (%q, %q)",
					tc.rest, kind, text, tc.wantKind, tc.wantText)
			}
		})
	}
}

func TestProjectCommandStoresWhatItReports(t *testing.T) {
	ctx := context.Background()

	t.Run("new stores the project and invalidates the mention cache", func(t *testing.T) {
		m, cs := contextModel(t)
		m.mentionLoaded = true

		m.handleProject(ctx, strings.Fields("/project new garagesale app en Go con SQLite"))

		p, err := cs.GetProject(ctx, "garagesale")
		if err != nil {
			t.Fatalf("the project was reported as saved but is not stored: %v", err)
		}
		if p.Description != "app en Go con SQLite" {
			t.Fatalf("description not stored: %q", p.Description)
		}
		if m.mentionLoaded {
			t.Error("a new project must invalidate the +mention cache, or autocomplete cannot see it")
		}
		if hasRole(m.entries, "err") {
			t.Fatalf("a successful create should not report an error; entries=%+v", m.entries)
		}
	})

	t.Run("a note is appended with the kind that was asked for", func(t *testing.T) {
		m, cs := contextModel(t)
		if _, err := cs.UpsertProject(ctx, domain.Project{Slug: "garagesale"}); err != nil {
			t.Fatal(err)
		}

		m.handleProject(ctx, strings.Fields("/project garagesale note decision migramos a SQLite"))

		p, err := cs.GetProject(ctx, "garagesale")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Notes) != 1 {
			t.Fatalf("expected exactly one stored note, got %+v", p.Notes)
		}
		if p.Notes[0].Kind != "decision" || p.Notes[0].Text != "migramos a SQLite" {
			t.Fatalf("note stored wrong: %+v", p.Notes[0])
		}
	})

	t.Run("a note without a kind is stored as info", func(t *testing.T) {
		m, cs := contextModel(t)
		if _, err := cs.UpsertProject(ctx, domain.Project{Slug: "garagesale"}); err != nil {
			t.Fatal(err)
		}

		m.handleProject(ctx, strings.Fields("/project garagesale note el deploy salió bien"))

		p, err := cs.GetProject(ctx, "garagesale")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Notes) != 1 {
			t.Fatalf("expected exactly one stored note, got %+v", p.Notes)
		}
		if p.Notes[0].Kind != "info" || p.Notes[0].Text != "el deploy salió bien" {
			t.Fatalf("note stored wrong: %+v", p.Notes[0])
		}
	})

	t.Run("a bare slug shows the project with its notes", func(t *testing.T) {
		m, cs := contextModel(t)
		if _, err := cs.UpsertProject(ctx, domain.Project{Slug: "garagesale", Description: "app en Go"}); err != nil {
			t.Fatal(err)
		}
		if err := cs.AddProjectNote(ctx, "garagesale", "decision", "migramos a SQLite"); err != nil {
			t.Fatal(err)
		}
		m.entries = nil

		m.handleProject(ctx, strings.Fields("/project garagesale"))

		got := lastEntry(t, m)
		if got.role != "raw" {
			t.Fatalf("the detail should be a raw entry, got role %q", got.role)
		}
		for _, want := range []string{"garagesale", "app en Go", "migramos a SQLite", "decision"} {
			if !strings.Contains(got.text, want) {
				t.Fatalf("the detail is missing %q:\n%s", want, got.text)
			}
		}
	})
}

func TestProjectCommandRejectsBadInvocations(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		line string
	}{
		{"an unknown slug", "/project ghost"},
		{"a note on an unknown slug", "/project ghost note info algo"},
		{"new without a slug", "/project new"},
		{"a note with no text at all", "/project garagesale note"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, cs := contextModel(t)
			if _, err := cs.UpsertProject(ctx, domain.Project{Slug: "garagesale"}); err != nil {
				t.Fatal(err)
			}
			m.entries = nil

			m.handleProject(ctx, strings.Fields(tc.line))

			if !hasRole(m.entries, "err") {
				t.Fatalf("the user must be told why nothing happened; entries=%+v", m.entries)
			}
			// A rejected command must leave the store exactly as it was.
			p, err := cs.GetProject(ctx, "garagesale")
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Notes) != 0 {
				t.Fatalf("a rejected command stored a note anyway: %+v", p.Notes)
			}
			if _, err := cs.GetProject(ctx, "ghost"); err == nil {
				t.Fatal("a rejected command created the project it was complaining about")
			}
		})
	}
}

func TestPersonCommandStoresWhatItReports(t *testing.T) {
	ctx := context.Background()

	t.Run("new stores the person and invalidates the mention cache", func(t *testing.T) {
		m, cs := contextModel(t)
		m.mentionLoaded = true

		m.handlePerson(ctx, strings.Fields("/person new kari área comercial"))

		p, err := cs.GetPerson(ctx, "kari")
		if err != nil {
			t.Fatalf("the person was reported as saved but is not stored: %v", err)
		}
		if p.Role != "área comercial" {
			t.Fatalf("role not stored: %q", p.Role)
		}
		if m.mentionLoaded {
			t.Error("a new person must invalidate the @mention cache, or autocomplete cannot see them")
		}
		if hasRole(m.entries, "err") {
			t.Fatalf("a successful create should not report an error; entries=%+v", m.entries)
		}
	})

	t.Run("a note is appended with the kind that was asked for", func(t *testing.T) {
		m, cs := contextModel(t)
		if _, err := cs.UpsertPerson(ctx, domain.Person{Nick: "kari"}); err != nil {
			t.Fatal(err)
		}

		m.handlePerson(ctx, strings.Fields("/person kari note change pasó a producto"))

		p, err := cs.GetPerson(ctx, "kari")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Notes) != 1 {
			t.Fatalf("expected exactly one stored note, got %+v", p.Notes)
		}
		if p.Notes[0].Kind != "change" || p.Notes[0].Text != "pasó a producto" {
			t.Fatalf("note stored wrong: %+v", p.Notes[0])
		}
	})

	t.Run("a bare nick shows the person with their notes", func(t *testing.T) {
		m, cs := contextModel(t)
		if _, err := cs.UpsertPerson(ctx, domain.Person{Nick: "kari", Role: "área comercial"}); err != nil {
			t.Fatal(err)
		}
		if err := cs.AddPersonNote(ctx, "kari", "info", "prefiere reuniones cortas"); err != nil {
			t.Fatal(err)
		}
		m.entries = nil

		m.handlePerson(ctx, strings.Fields("/person kari"))

		got := lastEntry(t, m)
		if got.role != "raw" {
			t.Fatalf("the detail should be a raw entry, got role %q", got.role)
		}
		for _, want := range []string{"kari", "área comercial", "prefiere reuniones cortas"} {
			if !strings.Contains(got.text, want) {
				t.Fatalf("the detail is missing %q:\n%s", want, got.text)
			}
		}
	})
}

func TestPersonCommandRejectsBadInvocations(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		line string
	}{
		{"an unknown nick", "/person ghost"},
		{"a note on an unknown nick", "/person ghost note info algo"},
		{"new without a nick", "/person new"},
		{"a note with no text at all", "/person kari note"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, cs := contextModel(t)
			if _, err := cs.UpsertPerson(ctx, domain.Person{Nick: "kari"}); err != nil {
				t.Fatal(err)
			}
			m.entries = nil

			m.handlePerson(ctx, strings.Fields(tc.line))

			if !hasRole(m.entries, "err") {
				t.Fatalf("the user must be told why nothing happened; entries=%+v", m.entries)
			}
			p, err := cs.GetPerson(ctx, "kari")
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Notes) != 0 {
				t.Fatalf("a rejected command stored a note anyway: %+v", p.Notes)
			}
			if _, err := cs.GetPerson(ctx, "ghost"); err == nil {
				t.Fatal("a rejected command created the person it was complaining about")
			}
		})
	}
}

func TestContextListings(t *testing.T) {
	ctx := context.Background()

	t.Run("no projects yet says so and points at how to create one", func(t *testing.T) {
		m, _ := contextModel(t)

		m.listProjects(ctx)

		got := lastEntry(t, m)
		if got.role == "err" {
			t.Fatalf("an empty list is not an error: %q", got.text)
		}
		if !strings.Contains(got.text, "/project new") {
			t.Fatalf("an empty list should tell the user how to fill it: %q", got.text)
		}
	})

	t.Run("every stored project is listed with what identifies it", func(t *testing.T) {
		m, cs := contextModel(t)
		seeded := []domain.Project{
			{Slug: "garagesale", Name: "Garage Sale", Description: "app en Go"},
			{Slug: "liquida", Name: "Liquida", Description: "app PHP 8.3"},
			{Slug: "sensei"},
		}
		for _, p := range seeded {
			if _, err := cs.UpsertProject(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		m.entries = nil

		m.listProjects(ctx)

		got := lastEntry(t, m)
		for _, want := range []string{
			"garagesale", "Garage Sale", "app en Go",
			"liquida", "Liquida", "app PHP 8.3",
			"sensei",
		} {
			if !strings.Contains(got.text, want) {
				t.Fatalf("the listing dropped %q:\n%s", want, got.text)
			}
		}
	})

	t.Run("a bare /project falls back to the listing", func(t *testing.T) {
		m, cs := contextModel(t)
		if _, err := cs.UpsertProject(ctx, domain.Project{Slug: "garagesale"}); err != nil {
			t.Fatal(err)
		}
		m.entries = nil

		m.handleProject(ctx, []string{"/project"})

		if got := lastEntry(t, m); !strings.Contains(got.text, "garagesale") {
			t.Fatalf("a bare /project should list the projects:\n%s", got.text)
		}
	})

	t.Run("no people yet says so and points at how to create one", func(t *testing.T) {
		m, _ := contextModel(t)

		m.listPeople(ctx)

		got := lastEntry(t, m)
		if got.role == "err" {
			t.Fatalf("an empty list is not an error: %q", got.text)
		}
		if !strings.Contains(got.text, "/person new") {
			t.Fatalf("an empty list should tell the user how to fill it: %q", got.text)
		}
	})

	t.Run("every stored person is listed with what identifies them", func(t *testing.T) {
		m, cs := contextModel(t)
		seeded := []domain.Person{
			{Nick: "kari", Name: "Karina", Role: "área comercial"},
			{Nick: "nacho"},
		}
		for _, p := range seeded {
			if _, err := cs.UpsertPerson(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		m.entries = nil

		m.listPeople(ctx)

		got := lastEntry(t, m)
		for _, want := range []string{"kari", "Karina", "área comercial", "nacho"} {
			if !strings.Contains(got.text, want) {
				t.Fatalf("the listing dropped %q:\n%s", want, got.text)
			}
		}
	})

	t.Run("a bare /person falls back to the listing", func(t *testing.T) {
		m, cs := contextModel(t)
		if _, err := cs.UpsertPerson(ctx, domain.Person{Nick: "kari"}); err != nil {
			t.Fatal(err)
		}
		m.entries = nil

		m.handlePerson(ctx, []string{"/person"})

		if got := lastEntry(t, m); !strings.Contains(got.text, "kari") {
			t.Fatalf("a bare /person should list the people:\n%s", got.text)
		}
	})

	t.Run("without a context store the commands say so instead of panicking", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.deps.Context = nil

		for _, run := range []func(){
			func() { m.listProjects(ctx) },
			func() { m.listPeople(ctx) },
			func() { m.handleProject(ctx, strings.Fields("/project garagesale")) },
			func() { m.handlePerson(ctx, strings.Fields("/person kari")) },
		} {
			m.entries = nil
			run()
			if !hasRole(m.entries, "err") {
				t.Fatalf("an unavailable context store must be reported; entries=%+v", m.entries)
			}
		}
	})
}

func TestWriteNotes(t *testing.T) {
	t.Run("with no notes nothing is written at all", func(t *testing.T) {
		var b strings.Builder
		writeNotes(&b, nil)
		if b.Len() != 0 {
			t.Fatalf("an empty note list should add no section, got %q", b.String())
		}
	})

	t.Run("every note is written with its kind and text", func(t *testing.T) {
		var b strings.Builder
		writeNotes(&b, []domain.Note{
			{Kind: "decision", Text: "migramos a SQLite"},
			{Kind: "change", Text: "cambió el owner"},
		})
		for _, want := range []string{"decision", "migramos a SQLite", "change", "cambió el owner"} {
			if !strings.Contains(b.String(), want) {
				t.Fatalf("notes section is missing %q:\n%s", want, b.String())
			}
		}
	})
}
