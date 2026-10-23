package tui

// The OpenRouter model browser: a filterable list over the live catalog.
//
// OpenRouter is one key in front of hundreds of models, which is exactly what
// makes it awkward — the id is the only thing that selects a model, and there
// is no way to guess it. This screen turns that into a search box, and marks
// the one property that decides whether a model can run this agent at all:
// tool-calling support.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/webcloster-dev/planner/internal/openrouter"
	"github.com/webcloster-dev/planner/internal/wiring"
)

// pickerVisibleRows is how many catalog rows fit when the terminal height is
// unknown (tests, or before the first WindowSizeMsg).
const pickerVisibleRows = 12

type modelPicker struct {
	provider string // whose model this picker is choosing
	all      []openrouter.Model
	matches  []openrouter.Model
	filter   textinput.Model
	cursor   int // index into matches
	offset   int // first visible row, so long lists scroll
	// toolsOnly hides models OpenRouter does not advertise tool support for.
	// On by default: picking one of those looks fine until the first message.
	toolsOnly bool
}

func newModelPicker(provider string, all []openrouter.Model) *modelPicker {
	ti := textinput.New()
	ti.Prompt = "search › "
	ti.Focus()
	p := &modelPicker{provider: provider, all: all, filter: ti, toolsOnly: true}
	p.refresh()
	return p
}

// filterModels keeps the models matching every whitespace-separated term, in
// id or display name. Every term must match, so "gpt mini" narrows rather than
// widens — that is what makes a 400-entry catalog navigable.
func filterModels(all []openrouter.Model, query string, toolsOnly bool) []openrouter.Model {
	terms := strings.Fields(strings.ToLower(query))
	out := make([]openrouter.Model, 0, len(all))
	for _, m := range all {
		if toolsOnly && !m.Tools {
			continue
		}
		hay := strings.ToLower(m.ID + " " + m.Name)
		ok := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, m)
		}
	}
	return out
}

// refresh recomputes the matches and pulls the cursor and the scroll window
// back into range — a narrower filter must never leave the cursor past the end.
func (p *modelPicker) refresh() {
	p.matches = filterModels(p.all, p.filter.Value(), p.toolsOnly)
	if p.cursor >= len(p.matches) {
		p.cursor = len(p.matches) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.clampOffset(pickerVisibleRows)
}

func (p *modelPicker) clampOffset(rows int) {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *modelPicker) selected() (openrouter.Model, bool) {
	if p.cursor < 0 || p.cursor >= len(p.matches) {
		return openrouter.Model{}, false
	}
	return p.matches[p.cursor], true
}

// --- update ---

// openModelPicker fetches the catalog and opens the browser over it.
//
// The fetch is synchronous, like the Plane state fetch next door: this screen
// has no message plumbing of its own, and a second or two of stillness after
// pressing enter is a fair price for keeping it that way.
func (m *configModel) openModelPicker(provider string) string {
	models, err := wiring.OpenRouterClient(*m.cfg).ListModels(context.Background())
	if err != nil {
		return "fetch failed: " + err.Error()
	}
	if len(models) == 0 {
		return "OpenRouter returned an empty catalog"
	}
	m.picker = newModelPicker(provider, models)
	m.status = ""
	return ""
}

func (m *configModel) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.picker = nil
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if p.cursor > 0 {
			p.cursor--
		}
		p.clampOffset(m.pickerRows())
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		if p.cursor < len(p.matches)-1 {
			p.cursor++
		}
		p.clampOffset(m.pickerRows())
		return m, nil
	case tea.KeyCtrlT:
		p.toolsOnly = !p.toolsOnly
		p.refresh()
		return m, nil
	case tea.KeyEnter:
		sel, ok := p.selected()
		if !ok {
			return m, nil
		}
		pc := m.cfg.Providers[p.provider]
		pc.Model = sel.ID
		m.cfg.Providers[p.provider] = pc
		m.picker = nil
		// Rebuild the detail rows so the model row shows what was just chosen.
		m.fields = m.providerDetailFields(p.provider)
		m.status = "model → " + sel.ID + " — press s to save"
		return m, nil
	}

	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(msg)
	p.refresh()
	return m, cmd
}

// pickerRows is how many catalog rows the current terminal can show.
func (m *configModel) pickerRows() int {
	rows := m.height - 10 // header, search box, help line, breathing room
	if rows < 3 {
		return pickerVisibleRows
	}
	if rows > 24 {
		return 24
	}
	return rows
}

// --- view ---

func (m *configModel) viewPicker() string {
	p := m.picker
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	b.WriteString(title.Render("Providers · "+p.provider+" · models") + "\n\n")
	b.WriteString(p.filter.View() + "\n\n")

	if len(p.matches) == 0 {
		b.WriteString(sysStyle.Render("nothing matches that search") + "\n")
	}
	rows := m.pickerRows()
	for i := p.offset; i < len(p.matches) && i < p.offset+rows; i++ {
		mm := p.matches[i]
		cursor := "  "
		id := fmt.Sprintf("%-40s", truncate(mm.ID, 40))
		if i == p.cursor {
			cursor = selStyle.Render("›") + " "
			id = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111")).Render(id)
		}
		b.WriteString(fmt.Sprintf("%s%s %6s  %s\n", cursor, id, formatContext(mm.ContextLength), priceLabel(mm)))
	}
	if n := len(p.matches); n > rows {
		b.WriteString(helpStyle.Render(fmt.Sprintf("  …%d more — keep typing to narrow", n-rows)) + "\n")
	}

	scope := "tool-capable only"
	if !p.toolsOnly {
		scope = "all models"
	}
	b.WriteString("\n" + helpStyle.Render("type to search · ↑/↓ move · enter choose · ctrl+t "+scope+" · esc back"))
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// formatContext renders a context window the way model cards do.
func formatContext(n int) string {
	switch {
	case n <= 0:
		return "—"
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// priceLabel renders prompt/completion cost per million tokens — the unit the
// catalog is usually compared in.
func priceLabel(m openrouter.Model) string {
	if m.Free() {
		return "free"
	}
	return fmt.Sprintf("$%s/$%s", trimPrice(m.PromptUSD), trimPrice(m.CompletionUSD))
}

// trimPrice keeps four significant digits: the per-token figures are converted
// by multiplication, and printing the full float64 would show the rounding
// error rather than the price.
func trimPrice(v float64) string {
	return strconv.FormatFloat(v, 'g', 4, 64)
}

// favoriteAlias suggests a short name for a model id: the vendor prefix is
// noise once the provider is already known.
func favoriteAlias(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 && i+1 < len(model) {
		return model[i+1:]
	}
	return model
}
