package daily

import (
	"strings"
	"testing"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
)

// The builder must emit the exported prefixes verbatim. If someone edits Build
// to write its own indentation or marker, the spec handed to the model and the
// fallback output start describing different formats — which is the drift this
// package exists to prevent.
func TestBuildUsesExportedPrefixes(t *testing.T) {
	tasks := []domain.Task{
		{Type: domain.TypeFeat, Title: "Lazy loading", Status: domain.StatusStarted, WorkItemSeq: 343},
		{Type: domain.TypeFix, Title: "DNS", Status: domain.StatusCompleted,
			Details: domain.TaskDetails{TechNotes: "Usar VPN por restricción de IP"}},
	}
	out := Build(Date(time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)), tasks)

	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "**") {
			continue // blank separator or a bold section title
		}
		if !strings.HasPrefix(line, PrefixWork) &&
			!strings.HasPrefix(line, PrefixBlock) &&
			!strings.HasPrefix(line, PrefixNote) {
			t.Fatalf("item line does not start with an exported prefix: %q\nfull output:\n%s", line, out)
		}
	}

	if !strings.Contains(out, PrefixWork+"[FEAT] #343 Lazy loading") {
		t.Fatalf("work item should use PrefixWork %q:\n%s", PrefixWork, out)
	}
	if !strings.Contains(out, PrefixNote+"Usar VPN por restricción de IP") {
		t.Fatalf("note should use PrefixNote %q:\n%s", PrefixNote, out)
	}
}

// FormatSpec is the text the model is told to obey. If it stops naming a
// section or a prefix, the model can no longer produce what Build produces.
func TestFormatSpecMentionsEveryTitleAndPrefix(t *testing.T) {
	for _, title := range []string{TitleDaily, TitleWork, TitleBlocks, TitleNotes} {
		if !strings.Contains(FormatSpec, "**"+title+":**") {
			t.Errorf("FormatSpec does not mention the bold section title %q", title)
		}
	}
	for name, prefix := range map[string]string{
		"PrefixWork":  PrefixWork,
		"PrefixBlock": PrefixBlock,
		"PrefixNote":  PrefixNote,
	} {
		if !strings.Contains(FormatSpec, prefix) {
			t.Errorf("FormatSpec does not mention %s (%q)", name, prefix)
		}
	}
}

// The /daily prompt must compose the spec, not carry its own copy of it.
func TestPromptEmbedsFormatSpec(t *testing.T) {
	if !strings.Contains(Prompt, FormatSpec) {
		t.Fatalf("Prompt must embed FormatSpec verbatim, got:\n%s", Prompt)
	}
}

func TestDateUsesSpanishMonthAbbreviation(t *testing.T) {
	cases := map[time.Month]string{
		time.January:  "2026-01-15 ENE",
		time.April:    "2026-04-15 ABR",
		time.August:   "2026-08-15 AGO",
		time.December: "2026-12-15 DIC",
	}
	for month, want := range cases {
		got := Date(time.Date(2026, month, 15, 0, 0, 0, 0, time.UTC))
		if got != want {
			t.Errorf("Date(%v) = %q, want %q", month, got, want)
		}
	}
}
