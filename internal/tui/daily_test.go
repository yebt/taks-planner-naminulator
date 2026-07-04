package tui

import (
	"strings"
	"testing"

	"github.com/webcloster-dev/planner/internal/domain"
)

func TestBuildDaily(t *testing.T) {
	tasks := []domain.Task{
		{Type: domain.TypeFeat, Title: "Lazy loading", Status: domain.StatusInProgress, WorkItemSeq: 343},
		{Type: domain.TypeFix, Title: "Fecha inválida", Status: domain.StatusDone},
	}
	out := buildDaily(tasks)
	for _, want := range []string{
		"# Daily",
		"## En progreso",
		"- [FEAT] #343 Lazy loading",
		"## Hecho",
		"- [FIX] Fecha inválida",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("daily missing %q\n got:\n%s", want, out)
		}
	}
}

func TestBuildDailyEmpty(t *testing.T) {
	out := buildDaily(nil)
	if !strings.Contains(out, "Sin actividad") {
		t.Fatalf("empty daily should note no activity, got: %s", out)
	}
}
