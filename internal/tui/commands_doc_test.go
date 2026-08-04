package tui

import (
	"os"
	"strings"
	"testing"
)

// readmePath is relative to this package. The test lives here, next to the
// command table it guards, rather than in a docs package that would have to
// import tui just to see baseCommands.
const readmePath = "../../README.md"

// The audit found the README and the command table had drifted apart — commands
// that existed but were undocumented, and a usage blurb listing a stale subset.
// Cleaning that up by hand fixes the symptom; nothing stopped it recurring.
//
// This is the real fix: adding a command without documenting it fails the build.
// It asserts presence only, deliberately — the README's prose and grouping are
// free to change, and a test that pinned wording would be a tax rather than a
// guard.
func TestEveryCommandIsDocumented(t *testing.T) {
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("cannot read the README to check it: %v", err)
	}
	readme := string(raw)

	for _, c := range baseCommands {
		t.Run(c.full, func(t *testing.T) {
			// Match the command followed by a boundary, so /task does not get a
			// false pass from /tasks and /daily does not satisfy /dailies.
			if !mentionsCommand(readme, c.full) {
				t.Errorf("%s is offered in the command menu but not documented in README.md — "+
					"add it under Commands, or drop it from baseCommands", c.full)
			}
		})
	}
}

// mentionsCommand reports whether the README refers to cmd as a whole token.
//
// Both ends matter. Looking only at the character after the command lets any
// path that happens to end in it — "internal/config", "~/.config" — pass as
// documentation, which is how /config first shipped undocumented while this
// test was green.
func mentionsCommand(readme, cmd string) bool {
	for i := 0; ; {
		j := strings.Index(readme[i:], cmd)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(cmd)
		standsAlone := (start == 0 || !isCommandRune(readme[start-1])) &&
			(end >= len(readme) || !isCommandRune(readme[end]))
		if standsAlone {
			return true
		}
		i = end
	}
}

// isCommandRune reports whether b could continue a command name, so "/task" is
// not considered found inside "/tasks".
func isCommandRune(b byte) bool {
	return b == '-' || b == '_' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// The guard is only worth having if it cannot be satisfied by a near-miss:
// "/task" must not be considered documented because "/tasks" appears somewhere.
func TestMentionsCommandRequiresAWholeToken(t *testing.T) {
	tests := []struct {
		name   string
		readme string
		cmd    string
		want   bool
	}{
		{"exact mention", "- `/task <id>` — show a task", "/task", true},
		{"longer command does not count", "- `/tasks` — something else", "/task", false},
		{"/daily is not satisfied by /dailies", "- `/dailies` — list stored digests", "/daily", false},
		{"/dailies is found on its own", "- `/dailies` — list", "/dailies", true},
		{"absent", "nothing here", "/task", false},
		{"end of file counts as a boundary", "see /task", "/task", true},
		{"found after a near-miss", "`/tasks` and also `/task <id>`", "/task", true},
		// The guard originally only looked at the character AFTER the command,
		// so the architecture map's "internal/config" silently satisfied
		// "/config" and a genuinely undocumented command shipped green.
		{"a package path does not document a command", "internal/config   JSON config", "/config", false},
		{"a file path does not document a command", "see ~/.config/planner/state for /state", "/config", false},
		{"real documentation after a path still counts", "internal/config\n- `/config` — open settings", "/config", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mentionsCommand(tt.readme, tt.cmd); got != tt.want {
				t.Fatalf("mentionsCommand(%q, %q) = %v, want %v", tt.readme, tt.cmd, got, tt.want)
			}
		})
	}
}
