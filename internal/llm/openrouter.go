package llm

// OpenRouterBaseURL is OpenRouter's OpenAI-compatible endpoint.
const OpenRouterBaseURL = "https://openrouter.ai/api/v1"

// OpenRouterDefaultModel is what a fresh OpenRouter entry points at: cheap,
// tool-capable, and available to every key. Any catalog id replaces it.
const OpenRouterDefaultModel = "openai/gpt-4o-mini"

// NewOpenRouter builds the OpenRouter adapter.
//
// OpenRouter is a gateway, not a lab: one key reaches every model in its
// catalog, and the model id is the only thing that changes between them. That
// is why it needs no adapter of its own beyond a base URL — and why the useful
// work lives in picking the id (see internal/openrouter).
func NewOpenRouter(apiKey, model string) *OpenAICompatible {
	if model == "" {
		model = OpenRouterDefaultModel
	}
	o := NewOpenAICompatible("openrouter", OpenRouterBaseURL, apiKey, model)
	// Optional attribution headers: OpenRouter uses them to name the calling
	// app in its dashboards and rankings. Nothing breaks without them.
	o.SetHeader("HTTP-Referer", "https://github.com/webcloster-dev/planner")
	o.SetHeader("X-Title", "planner")
	return o
}
