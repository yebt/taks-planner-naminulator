package domain

import (
	"strings"
	"testing"
)

func TestDisplayTitle(t *testing.T) {
	task := Task{Type: TypeFeat, Title: "Implementación de Lazy Loading en rutas del proyecto"}

	// Before sync (no sequence) the code segment is omitted.
	if got := task.DisplayTitle(); got != "[FEAT] - Implementación de Lazy Loading en rutas del proyecto" {
		t.Fatalf("pre-sync title: %q", got)
	}

	// After sync the Plane code (#343) is embedded, matching LIQMSTR-343.
	task.WorkItemSeq = 343
	if got := task.DisplayTitle(); got != "[FEAT] - #343 - Implementación de Lazy Loading en rutas del proyecto" {
		t.Fatalf("post-sync title: %q", got)
	}
}

func TestPlaneGroup(t *testing.T) {
	cases := map[Status]string{
		StatusBacklog:    "backlog",
		StatusPostponed:  "backlog",
		StatusTodo:       "unstarted",
		StatusInProgress: "started",
		StatusBlocked:    "started",
		StatusDone:       "completed",
		StatusRejected:   "completed",
		StatusCancelled:  "cancelled",
	}
	for s, want := range cases {
		if got := s.PlaneGroup(); got != want {
			t.Errorf("%s.PlaneGroup() = %q, want %q", s, got, want)
		}
	}
}

func TestRenderHTML(t *testing.T) {
	task := Task{
		Type:        TypeFeat,
		Title:       "X",
		Description: "Implementar la carga diferida en todas las rutas.",
		Details: TaskDetails{
			Preconditions:      []string{"Acceso al repositorio principal"},
			AcceptanceCriteria: []string{"Dado X Cuando Y Entonces Z"},
		},
	}
	html := task.RenderHTML()
	for _, want := range []string{
		"<h2>Descripción funcional</h2>",
		"<p>Implementar la carga diferida en todas las rutas.</p>",
		"<h2>Pre-condiciones</h2>",
		"<li>Acceso al repositorio principal</li>",
		"<h2>Criterios de aceptación</h2>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q\n got: %s", want, html)
		}
	}

	// Empty task yields no sections.
	if got := (Task{Type: TypeFix, Title: "Y"}).RenderHTML(); got != "" {
		t.Fatalf("empty task should render no body, got %q", got)
	}
}
