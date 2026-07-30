package tui

// Provider switching, favorites, and API keys.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/webcloster-dev/planner/internal/config"
)

func (m *chatModel) switchModel(name string) {
	if _, ok := m.deps.Cfg.Providers[name]; !ok {
		m.add("err", "provider not found: "+name)
		return
	}
	p, err := m.deps.Build(*m.deps.Cfg, name)
	if err != nil {
		m.add("err", err.Error())
		return
	}
	m.deps.Agent.SetProvider(p)
	m.deps.Cfg.ActiveProvider = name
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "switched, but save failed: "+err.Error())
		return
	}
	m.add("sys", "provider → "+name)
}

// --- favorites (saved provider+model combos) ---

func (m *chatModel) listFavorites() {
	if len(m.deps.Cfg.Favorites) == 0 {
		m.add("sys", "no favorites yet. use: /fav save <name> to store the current provider+model.")
		return
	}
	var b strings.Builder
	b.WriteString("favorites:\n")
	for _, f := range m.deps.Cfg.Favorites {
		b.WriteString(fmt.Sprintf("  %-16s %s · %s\n", f.Name, f.Provider, f.Model))
	}
	b.WriteString("\nuse: /fav <name> to switch")
	m.add("sys", strings.TrimRight(b.String(), "\n"))
}

func (m *chatModel) saveFavorite(name string) {
	prov := m.deps.Cfg.ActiveProvider
	model := m.deps.Cfg.Providers[prov].Model
	if strings.TrimSpace(name) == "" {
		name = prov + ":" + model
	}
	fav := config.Favorite{Name: name, Provider: prov, Model: model}
	replaced := false
	for i, f := range m.deps.Cfg.Favorites {
		if strings.EqualFold(f.Name, name) {
			m.deps.Cfg.Favorites[i] = fav
			replaced = true
			break
		}
	}
	if !replaced {
		m.deps.Cfg.Favorites = append(m.deps.Cfg.Favorites, fav)
	}
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "save failed: "+err.Error())
		return
	}
	m.add("sys", fmt.Sprintf("saved favorite %q → %s · %s", name, prov, model))
}

func (m *chatModel) delFavorite(name string) {
	out := m.deps.Cfg.Favorites[:0]
	found := false
	for _, f := range m.deps.Cfg.Favorites {
		if strings.EqualFold(f.Name, name) {
			found = true
			continue
		}
		out = append(out, f)
	}
	if !found {
		m.add("err", "favorite not found: "+name)
		return
	}
	m.deps.Cfg.Favorites = out
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "save failed: "+err.Error())
		return
	}
	m.add("sys", "removed favorite: "+name)
}

func (m *chatModel) applyFavorite(name string) {
	for _, f := range m.deps.Cfg.Favorites {
		if !strings.EqualFold(f.Name, name) {
			continue
		}
		pc, ok := m.deps.Cfg.Providers[f.Provider]
		if !ok {
			m.add("err", "favorite provider missing from config: "+f.Provider)
			return
		}
		pc.Model = f.Model
		m.deps.Cfg.Providers[f.Provider] = pc
		p, err := m.deps.Build(*m.deps.Cfg, f.Provider)
		if err != nil {
			m.add("err", err.Error())
			return
		}
		m.deps.Agent.SetProvider(p)
		m.deps.Cfg.ActiveProvider = f.Provider
		if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
			m.add("err", "applied, but save failed: "+err.Error())
			return
		}
		m.add("sys", fmt.Sprintf("favorite → %s (%s · %s)", f.Name, f.Provider, f.Model))
		return
	}
	m.add("err", "favorite not found: "+name)
}

func (m *chatModel) setKey(name, apiKey string) {
	pc, ok := m.deps.Cfg.Providers[name]
	if !ok {
		m.add("err", "provider not found: "+name)
		return
	}
	pc.APIKey = apiKey
	m.deps.Cfg.Providers[name] = pc
	if err := config.Save(m.deps.ConfigPath, *m.deps.Cfg); err != nil {
		m.add("err", "save failed: "+err.Error())
		return
	}
	if m.deps.Cfg.ActiveProvider == name {
		if p, err := m.deps.Build(*m.deps.Cfg, name); err == nil {
			m.deps.Agent.SetProvider(p)
		}
	}
	m.add("sys", "API key saved for "+name+".")
}

func (m *chatModel) providerNames() []string {
	names := make([]string, 0, len(m.deps.Cfg.Providers))
	for n := range m.deps.Cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
