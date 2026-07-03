package tui

import "testing"

func names(sugs []suggestion) []string {
	out := make([]string, len(sugs))
	for i, s := range sugs {
		out[i] = s.full
	}
	return out
}

func TestComputeSuggestions(t *testing.T) {
	providers := []string{"claude", "kimi", "ollama"}

	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"hola", nil},
		{"/", []string{"/help", "/todos", "/task", "/new", "/status", "/model", "/key", "/save", "/chats", "/load"}}, // capped at 10
		{"/t", []string{"/todos", "/task"}},
		{"/re", []string{"/recall", "/remember"}},
		{"/rem", []string{"/remember"}},
		{"/c", []string{"/chats", "/clear"}},
		{"/model ", []string{"/model claude", "/model kimi", "/model ollama"}},
		{"/model k", []string{"/model kimi"}},
		{"/model kimi ", nil},      // provider chosen, nothing more to complete
		{"/model kimi extra", nil}, // extra args, no menu
		{"/key o", []string{"/key ollama "}},
		{"/key kimi ", nil},          // provider locked, now typing the key
		{"/key kimi sk-abc123", nil}, // key being typed — menu must stay closed
		{"/new FEAT thing", nil},     // past the command token, no completion
	}

	for _, c := range cases {
		got := names(computeSuggestions(c.in, providers))
		if !equal(got, c.want) {
			t.Errorf("computeSuggestions(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCompletedValue(t *testing.T) {
	cases := map[string]string{
		"/model":      "/model ",     // base needs-arg command → trailing space
		"/model kimi": "/model kimi", // already carries its arg → as-is
		"/key kimi ":  "/key kimi ",  // provider chosen, ready for the key → as-is
		"/help":       "/help",       // terminal command → no space
	}
	for full, want := range cases {
		if got := completedValue(suggestion{full: full}); got != want {
			t.Errorf("completedValue(%q) = %q, want %q", full, got, want)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
