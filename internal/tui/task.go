package tui

// The /task detail view.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/webcloster-dev/planner/internal/domain"
)

// showTask renders one task expanded following the activity template.
func (m *chatModel) showTask(ctx context.Context, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		m.add("err", "id must be a number")
		return
	}
	t, err := m.deps.Store.Get(ctx, id)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	w := m.vp.Width
	if w < 10 {
		w = 10
	}
	wrap := lipgloss.NewStyle().Width(w)

	var b strings.Builder
	b.WriteString(title.Render(fmt.Sprintf("%s · %s · #%d", t.Type, t.Label, t.ID)) + "\n")
	b.WriteString(title.Render(t.Title) + "\n\n")
	b.WriteString(head.Render("Estado") + "\n")
	workItem := orDash(t.WorkItemID)
	if t.WorkItemSeq > 0 {
		workItem = fmt.Sprintf("#%d (%s)", t.WorkItemSeq, t.WorkItemID)
	}
	project := "—"
	if t.Project != "" {
		project = "+" + t.Project
	}
	b.WriteString(fmt.Sprintf("- status: %s\n- priority: %s\n- state: %s\n- proyecto: %s\n- work item: %s\n- fechas: %s → %s\n\n",
		t.Status, t.PlanePriority(), orDash(t.State), project, workItem, orDash(t.StartDate), orDash(t.DueDate)))
	b.WriteString(head.Render("Descripción") + "\n")
	b.WriteString(wrap.Render(orDash(t.Description)) + "\n\n")
	writeDetails(&b, head, wrap, t.Details)
	b.WriteString(head.Render("Fechas") + "\n")
	b.WriteString(fmt.Sprintf("- creada: %s\n- actualizada: %s\n- última interacción: %s",
		t.CreatedAt.Local().Format("2006-01-02 15:04"),
		t.UpdatedAt.Local().Format("2006-01-02 15:04"),
		t.TouchedAt.Local().Format("2006-01-02 15:04")))
	if m.deps.Activity != nil {
		if acts, err := m.deps.Activity.ActivityForTask(ctx, t.ID); err == nil && len(acts) > 0 {
			b.WriteString("\n\n" + head.Render("Historial") + "\n")
			for _, a := range acts {
				b.WriteString(fmt.Sprintf("- %s  %s\n", a.At.Local().Format("2006-01-02 15:04"), a.Note))
			}
		}
	}
	m.add("raw", strings.TrimRight(b.String(), "\n"))
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// writeDetails appends the filled activity-template sections (skips empties).
func writeDetails(b *strings.Builder, head, wrap lipgloss.Style, d domain.TaskDetails) {
	line := func(label, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		b.WriteString(head.Render(label) + "\n")
		b.WriteString(wrap.Render(val) + "\n\n")
	}
	list := func(label string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString(head.Render(label) + "\n")
		for _, it := range items {
			b.WriteString(wrap.Render("- "+it) + "\n")
		}
		b.WriteString("\n")
	}
	line("Objetivo", d.Objective)
	line("Justificación", d.Justification)
	if d.AsA != "" || d.IWant != "" || d.SoThat != "" {
		b.WriteString(head.Render("Descripción funcional") + "\n")
		b.WriteString(wrap.Render(fmt.Sprintf("Como %s\nQuiero %s\nPara %s",
			orDash(d.AsA), orDash(d.IWant), orDash(d.SoThat))) + "\n\n")
	}
	list("Pre-condiciones", d.Preconditions)
	list("Criterios de aceptación", d.AcceptanceCriteria)
	line("Consideraciones técnicas", d.TechNotes)
	line("Funcionalidad relacionada", d.RelatedFeature)
	line("Ambiente", d.Environment)
	list("Pasos a reproducir", d.StepsToReproduce)
	line("Resultado actual", d.ActualResult)
	line("Resultado esperado", d.ExpectedResult)
	if len(d.Checklist) > 0 {
		b.WriteString(head.Render("Checklist") + "\n")
		for _, it := range d.Checklist {
			mark := "☐"
			if it.Done {
				mark = "☑"
			}
			b.WriteString(wrap.Render(mark+" "+it.Text) + "\n")
		}
		b.WriteString("\n")
	}
	list("Anexos", d.Links)
}
