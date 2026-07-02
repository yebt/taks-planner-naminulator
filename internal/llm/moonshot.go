package llm

// MoonshotBaseURL is the Moonshot AI (China) OpenAI-compatible endpoint.
const MoonshotBaseURL = "https://api.moonshot.cn/v1"

// NewMoonshot builds the Moonshot adapter (OpenAI-compatible, distinct URL).
func NewMoonshot(apiKey, model string) *OpenAICompatible {
	if model == "" {
		model = "moonshot-v1-8k"
	}
	return NewOpenAICompatible("moonshot", MoonshotBaseURL, apiKey, model)
}
