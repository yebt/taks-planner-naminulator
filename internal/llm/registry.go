package llm

import "fmt"

// ProviderConfig is the serializable description used to build a Provider.
// It lives here (not in config) so both the config layer and the registry
// share one shape.
type ProviderConfig struct {
	Kind    string `json:"kind"`               // openai | moonshot | kimi | groq | claude | custom
	Label   string `json:"label,omitempty"`    // display name / key for /model
	BaseURL string `json:"base_url,omitempty"` // required for custom (e.g. Ollama)
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
}

// Build constructs the concrete adapter for a config entry.
func Build(cfg ProviderConfig) (Provider, error) {
	switch cfg.Kind {
	case "openai":
		return NewOpenAI(cfg.APIKey, cfg.Model), nil
	case "moonshot":
		return NewMoonshot(cfg.APIKey, cfg.Model), nil
	case "kimi":
		return NewKimi(cfg.APIKey, cfg.Model), nil
	case "groq":
		return NewGroq(cfg.APIKey, cfg.Model), nil
	case "claude":
		return NewClaude(cfg.APIKey, cfg.Model), nil
	case "custom":
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("custom provider %q requires base_url", cfg.Label)
		}
		name := cfg.Label
		if name == "" {
			name = "custom"
		}
		return NewOpenAICompatible(name, cfg.BaseURL, cfg.APIKey, cfg.Model), nil
	default:
		return nil, fmt.Errorf("unknown provider kind %q", cfg.Kind)
	}
}
