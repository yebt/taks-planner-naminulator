// Package config loads and saves the planner configuration (providers, db path,
// Plane settings) as JSON under the user config dir.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/webcloster-dev/planner/internal/llm"
)

// PlaneConfig holds the self-hosted Plane connection settings. WorkspaceSlug is
// asked from the user (can't be inferred via API); ProjectID is selected from
// the API or entered manually.
type PlaneConfig struct {
	BaseURL         string            `json:"base_url,omitempty"` // e.g. https://planner.webcloster.cloud
	APIToken        string            `json:"api_token,omitempty"`
	WorkspaceSlug   string            `json:"workspace_slug,omitempty"`
	ProjectID       string            `json:"project_id,omitempty"`
	StateDefaults   map[string]string `json:"state_defaults,omitempty"`   // group -> default state name
	DefaultEstimate string            `json:"default_estimate,omitempty"` // estimate_point sent on new work items; empty = don't set
}

// Favorite is a saved provider+model combo the user can re-select later (e.g.
// swap from a spent Kimi/Moonshot to a specific Groq model that worked before).
type Favorite struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// DefaultPlaneBaseURL is the self-hosted Plane host used unless overridden.
const DefaultPlaneBaseURL = "https://planner.webcloster.cloud"

// MemoryConfig configures the long-term memory backend (Engram).
type MemoryConfig struct {
	Project string `json:"project,omitempty"` // engram project; empty = autodetect from cwd
}

// Config is the top-level app config.
type Config struct {
	ActiveProvider string                        `json:"active_provider"`
	Providers      map[string]llm.ProviderConfig `json:"providers"`
	Favorites      []Favorite                    `json:"favorites,omitempty"` // saved provider+model combos
	DBPath         string                        `json:"db_path"`
	ContextBudget  int                           `json:"context_budget"` // chars kept in the LLM window
	Plane          PlaneConfig                   `json:"plane"`
	Memory         MemoryConfig                  `json:"memory"`
}

// DefaultContextBudget is the default LLM context window budget in characters.
const DefaultContextBudget = 24000

// Default returns a ready-to-run config: Ollama active (free, local) plus
// stub entries for the paid providers so /model can switch once keys are set.
func Default() Config {
	return Config{
		ActiveProvider: "ollama",
		DBPath:         DefaultDBPath(),
		ContextBudget:  DefaultContextBudget,
		Plane:          PlaneConfig{BaseURL: DefaultPlaneBaseURL},
		Providers: map[string]llm.ProviderConfig{
			"ollama":   {Kind: "custom", Label: "ollama", BaseURL: "http://localhost:11434/v1", APIKey: "ollama", Model: "llama3.1"},
			"openai":   {Kind: "openai", Label: "openai", Model: "gpt-4o-mini"},
			"moonshot": {Kind: "moonshot", Label: "moonshot", Model: "moonshot-v1-8k"},
			"kimi":     {Kind: "kimi", Label: "kimi", Model: "kimi-k2-0711-preview"},
			"groq":     {Kind: "groq", Label: "groq", Model: "llama-3.3-70b-versatile"},
			"claude":   {Kind: "claude", Label: "claude", Model: "claude-opus-4-8"},
		},
	}
}

// DefaultDir is the planner config directory (e.g. ~/.config/planner).
func DefaultDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "planner")
}

// DefaultPath is the config file path.
func DefaultPath() string { return filepath.Join(DefaultDir(), "config.json") }

// DefaultDBPath is the SQLite database path.
func DefaultDBPath() string { return filepath.Join(DefaultDir(), "planner.db") }

// Load reads the config, returning defaults if the file does not exist.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	def := Default()
	if len(c.Providers) == 0 {
		c.Providers = def.Providers
	} else {
		// Heal older config files: add providers introduced after they were
		// written (e.g. groq) without touching the user's existing entries.
		for name, pc := range def.Providers {
			if _, ok := c.Providers[name]; !ok {
				c.Providers[name] = pc
			}
		}
	}
	if c.DBPath == "" {
		c.DBPath = DefaultDBPath()
	}
	if c.ActiveProvider == "" {
		c.ActiveProvider = Default().ActiveProvider
	}
	if c.ContextBudget <= 0 {
		c.ContextBudget = DefaultContextBudget
	}
	if c.Plane.BaseURL == "" {
		c.Plane.BaseURL = DefaultPlaneBaseURL
	}
	return c, nil
}

// Save writes the config as indented JSON, creating the directory if needed.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
