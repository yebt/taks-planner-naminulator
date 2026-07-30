package tui

// Autocomplete for slash commands and mentions.

import (
	"context"
	"strings"
)

func computeSuggestions(val string, providers []string) []suggestion {
	if !strings.HasPrefix(val, "/") || strings.Contains(val, "\n") {
		return nil
	}
	fields := strings.Fields(val)
	endsSpace := strings.HasSuffix(val, " ")
	var out []suggestion

	switch {
	case len(fields) <= 1 && !endsSpace:
		for _, c := range baseCommands {
			if strings.HasPrefix(c.full, val) {
				out = append(out, c)
			}
		}
	case fields[0] == "/todo":
		switch {
		case completingArg(fields, endsSpace): // first arg: all|<status>
			prefix := argPrefix(fields)
			for _, f := range todoFlags {
				if strings.HasPrefix(f, prefix) {
					out = append(out, suggestion{"/todo " + f, "list " + f})
				}
			}
		case (len(fields) == 2 && endsSpace) || (len(fields) == 3 && !endsSpace): // second arg: day
			prefix := ""
			if len(fields) == 3 {
				prefix = fields[2]
			}
			for _, d := range todoDayFlags {
				if strings.HasPrefix(d, prefix) {
					out = append(out, suggestion{"/todo " + fields[1] + " " + d, "day: " + d})
				}
			}
		}
	case fields[0] == "/model":
		if !completingArg(fields, endsSpace) {
			break
		}
		prefix := argPrefix(fields)
		for _, name := range providers {
			if strings.HasPrefix(name, prefix) {
				out = append(out, suggestion{"/model " + name, "switch to " + name})
			}
		}
	case fields[0] == "/key":
		if !completingArg(fields, endsSpace) {
			break
		}
		prefix := argPrefix(fields)
		for _, name := range providers {
			if strings.HasPrefix(name, prefix) {
				out = append(out, suggestion{"/key " + name + " ", "set API key for " + name})
			}
		}
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// completingArg reports whether the cursor is still on the first argument token
// (so we keep suggesting values for it); once the arg is filled we stop.
func completingArg(fields []string, endsSpace bool) bool {
	return (len(fields) == 1 && endsSpace) || (len(fields) == 2 && !endsSpace)
}

func argPrefix(fields []string) string {
	if len(fields) == 2 {
		return fields[1]
	}
	return ""
}

// mentionSuggestions completes a +project or @person mention being typed as the
// last word of the message. Returns nil unless the current word starts with +/@.
func (m *chatModel) mentionSuggestions(val string) []suggestion {
	if m.deps.Context == nil || strings.HasPrefix(val, "/") || strings.HasSuffix(val, " ") {
		return nil // not while typing a slash command or between words
	}
	tok := lastToken(val)
	if len(tok) == 0 {
		return nil
	}
	var pool []string
	var sym byte
	switch tok[0] {
	case '+':
		sym = '+'
		pool = m.mentionList(true)
	case '@':
		sym = '@'
		pool = m.mentionList(false)
	default:
		return nil
	}
	prefix := strings.ToLower(tok[1:])
	desc := "project"
	if sym == '@' {
		desc = "person"
	}
	var out []suggestion
	for _, name := range pool {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			out = append(out, suggestion{full: string(sym) + name, desc: desc})
		}
		if len(out) >= 10 {
			break
		}
	}
	return out
}

// mentionList returns the cached project slugs (projects=true) or person nicks,
// loading them lazily from the context store.
func (m *chatModel) mentionList(projects bool) []string {
	if !m.mentionLoaded {
		ctx := context.Background()
		if ps, err := m.deps.Context.ListProjects(ctx); err == nil {
			m.mentionProjects = m.mentionProjects[:0]
			for _, p := range ps {
				m.mentionProjects = append(m.mentionProjects, p.Slug)
			}
		}
		if pp, err := m.deps.Context.ListPeople(ctx); err == nil {
			m.mentionPeople = m.mentionPeople[:0]
			for _, p := range pp {
				m.mentionPeople = append(m.mentionPeople, p.Nick)
			}
		}
		m.mentionLoaded = true
	}
	if projects {
		return m.mentionProjects
	}
	return m.mentionPeople
}

// lastToken returns the final whitespace-delimited token of s ("" if s ends in
// whitespace or is empty).
func lastToken(s string) string {
	if i := strings.LastIndexAny(s, " \t\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// replaceLastToken swaps the final token of val for repl, keeping the prefix.
func replaceLastToken(val, repl string) string {
	if i := strings.LastIndexAny(val, " \t\n"); i >= 0 {
		return val[:i+1] + repl
	}
	return repl
}

func completedValue(sel suggestion) string {
	val := sel.full
	if len(strings.Fields(val)) > 1 {
		return val // already carries its argument (e.g. "/model kimi", "/key kimi ")
	}
	if needsArg[val] && !strings.HasSuffix(val, " ") {
		val += " "
	}
	return val
}

func (m *chatModel) acceptSuggestion() {
	if m.selected >= len(m.suggestions) {
		return
	}
	sel := m.suggestions[m.selected]
	if isMention(sel) {
		// Replace only the word under the cursor and keep typing after it.
		m.ta.SetValue(replaceLastToken(m.ta.Value(), sel.full) + " ")
		m.ta.CursorEnd()
		m.suggestions = nil
		m.selected = 0
		m.layout()
		return
	}
	val := completedValue(sel)
	m.ta.SetValue(val)
	next := computeSuggestions(val, m.providerNames())
	// If the only remaining suggestion is exactly what we just completed, the
	// command is ready — close the menu so Enter submits instead of re-selecting.
	if len(next) == 1 && next[0].full == val {
		next = nil
	}
	m.suggestions = next
	m.selected = 0
	m.layout()
}
