package llm

// GroqBaseURL is Groq's OpenAI-compatible endpoint.
const GroqBaseURL = "https://api.groq.com/openai/v1"

// NewGroq builds the Groq adapter (OpenAI-compatible, distinct URL).
func NewGroq(apiKey, model string) *OpenAICompatible {
	if model == "" {
		model = "llama-3.3-70b-versatile"
	}
	return NewOpenAICompatible("groq", GroqBaseURL, apiKey, model)
}
