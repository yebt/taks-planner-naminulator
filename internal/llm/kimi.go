package llm

// KimiBaseURL is the Kimi / Moonshot (international) OpenAI-compatible endpoint.
const KimiBaseURL = "https://api.kimi.com/coding/v1"

// NewKimi builds the Kimi adapter (OpenAI-compatible, distinct URL).
func NewKimi(apiKey, model string) *OpenAICompatible {
	if model == "" {
		model = "kimi-k2-0711-preview"
	}
	return NewOpenAICompatible("kimi", KimiBaseURL, apiKey, model)
}
