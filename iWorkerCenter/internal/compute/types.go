package compute

// TokenUsageRecord represents a single LLM request's token usage at the Center level.
type TokenUsageRecord struct {
	ID           string `json:"id"`
	DiWorkerID   string `json:"diworker_id"`
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	Estimated    bool   `json:"estimated"`
	Timestamp    string `json:"timestamp"`
}

// UsageFilter specifies optional filters for querying token usage records.
type UsageFilter struct {
	DiWorkerID string
	Start      string // RFC3339
	End        string // RFC3339
}
