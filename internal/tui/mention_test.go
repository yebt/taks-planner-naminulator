package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
	"github.com/webcloster-dev/planner/internal/store"
)

func TestParseMentions(t *testing.T) {
	pr, pe := parseMentions("reunir con @kari y @Kari sobre +liquida y +GarageSale y de nuevo +liquida")
	if len(pr) != 2 { // +liquida deduped (case-insensitive)
		t.Fatalf("projects = %v", pr)
	}
	if len(pe) != 1 { // @kari / @Kari deduped
		t.Fatalf("people = %v", pe)
	}
}

func TestBuildMentionContext(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	_, _ = st.UpsertProject(ctx, domain.Project{Slug: "liquida", Name: "Liquida", Description: "app PHP 8.3"})
	_ = st.AddProjectNote(ctx, "liquida", "decision", "migró a PHP 8.3")

	m := &chatModel{deps: ChatDeps{Context: st}}
	out := m.buildMentionContext(ctx, "revisar +liquida y +ghost")
	for _, want := range []string{"+liquida", "app PHP 8.3", "migró a PHP 8.3", "+ghost: sin registro", "project=<slug>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("context missing %q\n got:\n%s", want, out)
		}
	}
	if got := m.buildMentionContext(ctx, "sin menciones aquí"); got != "" {
		t.Fatalf("no mentions should yield empty context, got %q", got)
	}
}

func TestMentionSuggestions(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	_, _ = st.UpsertProject(ctx, domain.Project{Slug: "liquida"})
	_, _ = st.UpsertProject(ctx, domain.Project{Slug: "GarageSale"})
	_, _ = st.UpsertPerson(ctx, domain.Person{Nick: "kari"})

	m := &chatModel{deps: ChatDeps{Context: st}}

	if got := names(m.mentionSuggestions("reunir con @ka")); !equal(got, []string{"@kari"}) {
		t.Fatalf("@ka = %v", got)
	}
	if got := names(m.mentionSuggestions("sobre +li")); !equal(got, []string{"+liquida"}) {
		t.Fatalf("+li = %v", got)
	}
	if got := names(m.mentionSuggestions("+")); len(got) != 2 { // bare + lists all projects
		t.Fatalf("bare + should list all projects: %v", got)
	}
	if m.mentionSuggestions("hello ") != nil {
		t.Fatal("a trailing space means no active token → no suggestions")
	}
	if m.mentionSuggestions("plain text") != nil {
		t.Fatal("a non-mention token → no suggestions")
	}
}

func TestReplaceLastToken(t *testing.T) {
	if got := replaceLastToken("reunir con @ka", "@kari"); got != "reunir con @kari" {
		t.Fatalf("replaceLastToken = %q", got)
	}
	if got := replaceLastToken("@ka", "@kari"); got != "@kari" {
		t.Fatalf("single-token replace = %q", got)
	}
	if got := lastToken("a b c"); got != "c" {
		t.Fatalf("lastToken = %q", got)
	}
	if got := lastToken("abc "); got != "" {
		t.Fatalf("lastToken with trailing space = %q", got)
	}
}
