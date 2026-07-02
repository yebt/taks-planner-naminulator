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
		{"/", []string{"/help", "/todos", "/new", "/status", "/model", "/key", "/clear", "/quit"}},
		{"/t", []string{"/todos"}},
		{"/model ", []string{"/model claude", "/model kimi", "/model ollama"}},
		{"/model k", []string{"/model kimi"}},
		{"/key o", []string{"/key ollama "}},
		{"/new FEAT thing", nil}, // past the command token, no completion
	}

	for _, c := range cases {
		got := names(computeSuggestions(c.in, providers))
		if !equal(got, c.want) {
			t.Errorf("computeSuggestions(%q) = %v, want %v", c.in, got, c.want)
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
