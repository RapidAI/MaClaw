package main

// llmUsage holds token usage information returned by LLM API responses.
// OpenAI uses prompt_tokens/completion_tokens; Anthropic uses input_tokens/output_tokens.
type llmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
}
