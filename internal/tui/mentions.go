package tui

// +project / @person mentions, and the project/person commands.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/webcloster-dev/planner/internal/domain"
)

var mentionRe = regexp.MustCompile(`[+@][A-Za-z0-9_-]+`)

// parseMentions extracts unique +project slugs and @person nicks from text.
func parseMentions(text string) (projects, people []string) {
	seenP, seenU := map[string]bool{}, map[string]bool{}
	for _, tok := range mentionRe.FindAllString(text, -1) {
		name := tok[1:]
		key := strings.ToLower(name)
		if tok[0] == '+' {
			if !seenP[key] {
				seenP[key] = true
				projects = append(projects, name)
			}
		} else if !seenU[key] {
			seenU[key] = true
			people = append(people, name)
		}
	}
	return projects, people
}

// buildMentionContext assembles a context preamble from the projects/people
// referenced in the message, so the agent drafts coherently (e.g. it won't
// propose a PHP login for a Go project). Returns "" when nothing is referenced.
func (m *chatModel) buildMentionContext(ctx context.Context, text string) string {
	if m.deps.Context == nil {
		return ""
	}
	projects, people := parseMentions(text)
	if len(projects) == 0 && len(people) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Contexto de menciones — usalo para redactar coherente; no lo repitas al usuario]\n")
	for _, slug := range projects {
		p, err := m.deps.Context.GetProject(ctx, slug)
		if err != nil {
			fmt.Fprintf(&b, "+%s: sin registro (podés crearlo con upsert_project si corresponde)\n", slug)
			continue
		}
		b.WriteString("+" + p.Slug)
		if p.Name != "" {
			b.WriteString(" (" + p.Name + ")")
		}
		if p.Description != "" {
			b.WriteString(": " + p.Description)
		}
		b.WriteString("\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&b, "  - [%s] %s\n", n.Kind, n.Text)
		}
	}
	for _, nick := range people {
		p, err := m.deps.Context.GetPerson(ctx, nick)
		if err != nil {
			fmt.Fprintf(&b, "@%s: sin registro (podés crearlo con upsert_person si corresponde)\n", nick)
			continue
		}
		b.WriteString("@" + p.Nick)
		if p.Name != "" {
			b.WriteString(" (" + p.Name + ")")
		}
		if p.Role != "" {
			b.WriteString(": " + p.Role)
		}
		b.WriteString("\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&b, "  - [%s] %s\n", n.Kind, n.Text)
		}
	}
	if len(projects) > 0 {
		b.WriteString("Al crear tareas para un proyecto mencionado, pasá project=<slug> en create_task y respetá su stack/decisiones.\n")
	}
	if m.deps.Memory != nil && m.deps.Memory.Available() {
		b.WriteString("Podés usar recall_memory con el slug/nick para más contexto.\n")
	}
	b.WriteString("[fin contexto]")
	return b.String()
}

// --- projects & people ---

func (m *chatModel) listProjects(ctx context.Context) {
	if m.deps.Context == nil {
		m.add("err", "context store not available")
		return
	}
	ps, err := m.deps.Context.ListProjects(ctx)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(ps) == 0 {
		m.add("sys", "no projects yet. use: /project new <slug> [description]")
		return
	}
	var b strings.Builder
	b.WriteString(botLabel.Render(fmt.Sprintf("projects · %d", len(ps))) + "\n\n")
	for _, p := range ps {
		line := "  +" + p.Slug
		if p.Name != "" {
			line += "  " + p.Name
		}
		if p.Description != "" {
			line += helpStyle.Render("  · " + trunc(p.Description, 46))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("/project <slug> for detail"))
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) handleProject(ctx context.Context, fields []string) {
	if m.deps.Context == nil {
		m.add("err", "context store not available")
		return
	}
	if len(fields) < 2 {
		m.listProjects(ctx)
		return
	}
	if fields[1] == "new" {
		if len(fields) < 3 {
			m.add("err", "usage: /project new <slug> [description]")
			return
		}
		p, err := m.deps.Context.UpsertProject(ctx, domain.Project{Slug: fields[2], Description: strings.Join(fields[3:], " ")})
		if err != nil {
			m.add("err", err.Error())
			return
		}
		m.mentionLoaded = false
		m.add("sys", "saved project +"+p.Slug)
		return
	}
	slug := fields[1]
	if len(fields) >= 3 && fields[2] == "note" {
		kind, text := parseNoteArgs(fields[3:])
		if text == "" {
			m.add("err", "usage: /project <slug> note [info|decision|change] <text>")
			return
		}
		if err := m.deps.Context.AddProjectNote(ctx, slug, kind, text); err != nil {
			m.add("err", err.Error())
			return
		}
		m.add("sys", "note added to +"+slug)
		return
	}
	p, err := m.deps.Context.GetProject(ctx, slug)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	var b strings.Builder
	b.WriteString(ctxTitle.Render("+"+p.Slug) + "  " + p.Name + "\n")
	if p.Description != "" {
		b.WriteString(p.Description + "\n")
	}
	writeNotes(&b, p.Notes)
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) listPeople(ctx context.Context) {
	if m.deps.Context == nil {
		m.add("err", "context store not available")
		return
	}
	ps, err := m.deps.Context.ListPeople(ctx)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	if len(ps) == 0 {
		m.add("sys", "no people yet. use: /person new <nick> [role]")
		return
	}
	var b strings.Builder
	b.WriteString(botLabel.Render(fmt.Sprintf("people · %d", len(ps))) + "\n\n")
	for _, p := range ps {
		line := "  @" + p.Nick
		if p.Name != "" {
			line += "  " + p.Name
		}
		if p.Role != "" {
			line += helpStyle.Render("  · " + trunc(p.Role, 46))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("/person <nick> for detail"))
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) handlePerson(ctx context.Context, fields []string) {
	if m.deps.Context == nil {
		m.add("err", "context store not available")
		return
	}
	if len(fields) < 2 {
		m.listPeople(ctx)
		return
	}
	if fields[1] == "new" {
		if len(fields) < 3 {
			m.add("err", "usage: /person new <nick> [role]")
			return
		}
		p, err := m.deps.Context.UpsertPerson(ctx, domain.Person{Nick: fields[2], Role: strings.Join(fields[3:], " ")})
		if err != nil {
			m.add("err", err.Error())
			return
		}
		m.mentionLoaded = false
		m.add("sys", "saved person @"+p.Nick)
		return
	}
	nick := fields[1]
	if len(fields) >= 3 && fields[2] == "note" {
		kind, text := parseNoteArgs(fields[3:])
		if text == "" {
			m.add("err", "usage: /person <nick> note [info|decision|change] <text>")
			return
		}
		if err := m.deps.Context.AddPersonNote(ctx, nick, kind, text); err != nil {
			m.add("err", err.Error())
			return
		}
		m.add("sys", "note added to @"+nick)
		return
	}
	p, err := m.deps.Context.GetPerson(ctx, nick)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	var b strings.Builder
	b.WriteString(ctxTitle.Render("@"+p.Nick) + "  " + p.Name + "\n")
	if p.Role != "" {
		b.WriteString(helpStyle.Render(p.Role) + "\n")
	}
	writeNotes(&b, p.Notes)
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

var ctxTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))

// parseNoteArgs splits an optional leading kind (info|decision|change) from the
// note text (default kind: info).
func parseNoteArgs(rest []string) (kind, text string) {
	kind = "info"
	if len(rest) > 1 {
		switch rest[0] {
		case "info", "decision", "change":
			kind, rest = rest[0], rest[1:]
		}
	}
	return kind, strings.Join(rest, " ")
}

func writeNotes(b *strings.Builder, notes []domain.Note) {
	if len(notes) == 0 {
		return
	}
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	b.WriteString("\n" + head.Render("Notas") + "\n")
	for _, n := range notes {
		b.WriteString(fmt.Sprintf("- [%s] %s  %s\n", n.Kind, n.At.Local().Format("2006-01-02"), n.Text))
	}
}
