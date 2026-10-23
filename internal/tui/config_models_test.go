package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/openrouter"
)

func catalogFixture() []openrouter.Model {
	return []openrouter.Model{
		{ID: "anthropic/claude-opus", Name: "Anthropic: Claude Opus", ContextLength: 200000, Tools: true, PromptUSD: 15, CompletionUSD: 75},
		{ID: "openai/gpt-4o-mini", Name: "OpenAI: GPT-4o-mini", ContextLength: 128000, Tools: true, PromptUSD: 0.15, CompletionUSD: 0.6},
		{ID: "z-ai/glm-chat:free", Name: "Z: GLM Chat (free)", ContextLength: 32768, Tools: false},
	}
}

func TestFilterModelsMatchesEveryTermAcrossIDAndName(t *testing.T) {
	all := catalogFixture()
	got := filterModels(all, "gpt mini", false)
	if len(got) != 1 || got[0].ID != "openai/gpt-4o-mini" {
		t.Fatalf("terms should match in any order: %+v", got)
	}
	if got := filterModels(all, "ANTHROPIC", false); len(got) != 1 {
		t.Fatalf("matching must be case-insensitive: %+v", got)
	}
	if got := filterModels(all, "claude opus", false); len(got) != 1 {
		t.Fatal("a term should be allowed to match the display name")
	}
	if got := filterModels(all, "", false); len(got) != 3 {
		t.Fatal("an empty query should keep everything")
	}
}

// The agent runs on tool calls, so a model without them is not a cheaper
// option — it is one that cannot work. The picker hides those by default.
func TestFilterModelsCanHideModelsWithoutToolSupport(t *testing.T) {
	got := filterModels(catalogFixture(), "", true)
	if len(got) != 2 {
		t.Fatalf("tools-only kept %d models, want 2", len(got))
	}
	for _, m := range got {
		if !m.Tools {
			t.Fatalf("%s has no tool support and should have been hidden", m.ID)
		}
	}
}

func TestNewModelPickerHidesToollessModelsUpFront(t *testing.T) {
	p := newModelPicker("openrouter", catalogFixture())
	if !p.toolsOnly {
		t.Fatal("the picker should open filtered to tool-capable models")
	}
	if len(p.matches) != 2 {
		t.Fatalf("opened with %d matches, want 2", len(p.matches))
	}
}

func TestPickerEnterSetsTheProviderModel(t *testing.T) {
	cfg := config.Default()
	m := &configModel{cfg: &cfg, section: 0, provider: "openrouter"}
	m.fields = m.providerDetailFields("openrouter")
	m.picker = newModelPicker("openrouter", catalogFixture())
	m.picker.filter.SetValue("gpt mini")
	m.picker.refresh()

	m.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})

	if got := cfg.Providers["openrouter"].Model; got != "openai/gpt-4o-mini" {
		t.Fatalf("model is %q, want openai/gpt-4o-mini", got)
	}
	if m.picker != nil {
		t.Fatal("choosing a model should close the picker")
	}
	// The detail rows are rebuilt, so the model row shows the new value.
	if m.fields[0].get() != "openai/gpt-4o-mini" {
		t.Fatalf("the detail view still shows %q", m.fields[0].get())
	}
}

func TestPickerEscLeavesTheModelAlone(t *testing.T) {
	cfg := config.Default()
	before := cfg.Providers["openrouter"].Model
	m := &configModel{cfg: &cfg, section: 0, provider: "openrouter"}
	m.picker = newModelPicker("openrouter", catalogFixture())

	m.updatePicker(tea.KeyMsg{Type: tea.KeyEsc})

	if m.picker != nil {
		t.Fatal("esc should close the picker")
	}
	if cfg.Providers["openrouter"].Model != before {
		t.Fatal("esc must not change the model")
	}
}

func TestPickerTypingFiltersAndKeepsTheCursorInRange(t *testing.T) {
	cfg := config.Default()
	m := &configModel{cfg: &cfg, section: 0, provider: "openrouter"}
	m.picker = newModelPicker("openrouter", catalogFixture())
	m.picker.cursor = 1

	m.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("claude")})

	if len(m.picker.matches) != 1 {
		t.Fatalf("typing did not filter: %d matches", len(m.picker.matches))
	}
	if m.picker.cursor != 0 {
		t.Fatalf("cursor %d is past the end of the new match list", m.picker.cursor)
	}
}

// ctrl+t is the escape hatch for a model that OpenRouter has not flagged.
func TestPickerCtrlTRevealsToollessModels(t *testing.T) {
	cfg := config.Default()
	m := &configModel{cfg: &cfg, section: 0, provider: "openrouter"}
	m.picker = newModelPicker("openrouter", catalogFixture())

	m.updatePicker(tea.KeyMsg{Type: tea.KeyCtrlT})

	if len(m.picker.matches) != 3 {
		t.Fatalf("ctrl+t showed %d models, want all 3", len(m.picker.matches))
	}
}

func TestOnlyOpenRouterOffersTheModelBrowser(t *testing.T) {
	cfg := config.Default()
	m := &configModel{cfg: &cfg}
	has := func(name, want string) bool {
		for _, f := range m.providerDetailFields(name) {
			if strings.Contains(f.label, want) {
				return true
			}
		}
		return false
	}
	if !has("openrouter", "browse") {
		t.Fatal("openrouter should offer the catalog browser")
	}
	if has("groq", "browse") {
		t.Fatal("providers without a catalog API must not offer the browser")
	}
}

func TestSaveAsFavoriteStoresAnAliasForTheCurrentModel(t *testing.T) {
	cfg := config.Default()
	pc := cfg.Providers["openrouter"]
	pc.Model = "openai/gpt-4o-mini"
	cfg.Providers["openrouter"] = pc
	m := &configModel{cfg: &cfg}

	var fav *cfgField
	for i, f := range m.providerDetailFields("openrouter") {
		if strings.Contains(f.label, "favorite") {
			fav = &m.providerDetailFields("openrouter")[i]
		}
	}
	if fav == nil {
		t.Fatal("the provider detail should offer saving a favorite")
	}
	// The suggested alias is the model id without its vendor prefix, so the
	// user only has to shorten it — "gptmin" — instead of inventing it.
	if got := fav.get(); got != "gpt-4o-mini" {
		t.Fatalf("suggested alias is %q", got)
	}
	fav.set("gptmin")

	if len(cfg.Favorites) != 1 {
		t.Fatalf("favorites: %+v", cfg.Favorites)
	}
	f := cfg.Favorites[0]
	if f.Name != "gptmin" || f.Provider != "openrouter" || f.Model != "openai/gpt-4o-mini" {
		t.Fatalf("bad favorite: %+v", f)
	}

	// Saving the same alias twice replaces it rather than duplicating it.
	pc.Model = "anthropic/claude-opus"
	cfg.Providers["openrouter"] = pc
	fav.set("gptmin")
	if len(cfg.Favorites) != 1 || cfg.Favorites[0].Model != "anthropic/claude-opus" {
		t.Fatalf("re-saving an alias should replace it: %+v", cfg.Favorites)
	}
}

func TestFormatContextLength(t *testing.T) {
	if got := formatContext(128000); got != "128k" {
		t.Fatalf("128000 → %q", got)
	}
	if got := formatContext(1000000); got != "1.0M" {
		t.Fatalf("1000000 → %q", got)
	}
	if got := formatContext(0); got != "—" {
		t.Fatalf("0 → %q", got)
	}
}

func TestPriceLabel(t *testing.T) {
	if got := priceLabel(openrouter.Model{}); got != "free" {
		t.Fatalf("a zero-priced model → %q", got)
	}
	if got := priceLabel(openrouter.Model{PromptUSD: 0.15, CompletionUSD: 0.6}); got != "$0.15/$0.6" {
		t.Fatalf("priced model → %q", got)
	}
	// Real values off the live catalog: the per-token → per-million conversion
	// is a multiplication, and float64 shows its seams. The screen must not.
	got := priceLabel(openrouter.Model{PromptUSD: 0.7999999999999999, CompletionUSD: 1.5999999999999999})
	if got != "$0.8/$1.6" {
		t.Fatalf("float noise reached the screen: %q", got)
	}
}
