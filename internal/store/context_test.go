package store

import (
	"context"
	"errors"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
)

func TestProjectUpsertGetListNotes(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.UpsertProject(ctx, domain.Project{Slug: "GarageSale", Name: "Garage Sale", Description: "Go marketplace"}); err != nil {
		t.Fatal(err)
	}
	// upsert again (same slug, different case) must NOT duplicate — updates in place
	if _, err := s.UpsertProject(ctx, domain.Project{Slug: "garagesale", Description: "Go marketplace v2"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("case-insensitive slug should not duplicate: %+v", list)
	}
	if list[0].Description != "Go marketplace v2" {
		t.Fatalf("upsert did not update description: %q", list[0].Description)
	}

	// notes accumulate and are returned by Get (case-insensitive lookup)
	if err := s.AddProjectNote(ctx, "GARAGESALE", "decision", "migrado a Go 1.26"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProjectNote(ctx, "garagesale", "info", "usa SQLite"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(ctx, "garageSALE")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 2 || got.Notes[0].Text != "migrado a Go 1.26" { // oldest first
		t.Fatalf("notes not accumulated/ordered: %+v", got.Notes)
	}

	if _, err := s.GetProject(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.AddProjectNote(ctx, "nope", "info", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("note on missing project should be ErrNotFound, got %v", err)
	}
}

func TestPersonUpsertGetNotes(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.UpsertPerson(ctx, domain.Person{Nick: "kari", Name: "Karime", Role: "área comercial"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPersonNote(ctx, "Kari", "change", "pidió cambios en suspensiones"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPerson(ctx, "KARI")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Karime" || got.Role != "área comercial" {
		t.Fatalf("person fields wrong: %+v", got)
	}
	if len(got.Notes) != 1 || got.Notes[0].Kind != "change" {
		t.Fatalf("person note not stored: %+v", got.Notes)
	}
	people, _ := s.ListPeople(ctx)
	if len(people) != 1 {
		t.Fatalf("list people: %+v", people)
	}
}
