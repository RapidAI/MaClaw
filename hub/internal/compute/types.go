package compute

import (
	"fmt"
	"strings"
)

// Supported protocol values for LLM providers.
const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
	ProtocolGemini    = "gemini"
)

// validProtocols is the set of accepted protocol values.
var validProtocols = map[string]bool{
	ProtocolOpenAI:    true,
	ProtocolAnthropic: true,
	ProtocolGemini:    true,
}

// ComputeProvider is the full configuration record for an LLM service provider.
type ComputeProvider struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	BaseURL              string  `json:"base_url"`
	APIKey               string  `json:"api_key,omitempty"`
	HasAPIKey            bool    `json:"has_api_key,omitempty"`
	Protocol             string  `json:"protocol"`
	UserAgent            string  `json:"user_agent"`
	ComputeType          string  `json:"compute_type"`
	Model                string  `json:"model"`
	Enabled              bool    `json:"enabled"`
	Priority             int     `json:"priority"`
	Description          string  `json:"description"`
	InputPricePerMToken  float64 `json:"input_price_per_mtoken"`
	OutputPricePerMToken float64 `json:"output_price_per_mtoken"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

// TokenUsageRecord represents a single LLM request's token usage.
type TokenUsageRecord struct {
	ID           string `json:"id"`
	CenterID     string `json:"center_id,omitempty"`
	DiWorkerID   string `json:"diworker_id"`
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	Estimated    bool   `json:"estimated"`
	Timestamp    string `json:"timestamp"`
}

// CostSummary is an aggregated cost record for a time period.
type CostSummary struct {
	ID                string  `json:"id"`
	CenterID          string  `json:"center_id,omitempty"`
	DiWorkerID        string  `json:"diworker_id,omitempty"`
	DiWorkerName      string  `json:"diworker_name,omitempty"`
	PeriodType        string  `json:"period_type"`
	PeriodStart       string  `json:"period_start"`
	ProviderName      string  `json:"provider_name"`
	Model             string  `json:"model"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	InputCost         float64 `json:"input_cost"`
	OutputCost        float64 `json:"output_cost"`
	TotalCost         float64 `json:"total_cost"`
	RequestCount      int64   `json:"request_count"`
	InputPriceUsed    float64 `json:"input_price_used"`
	OutputPriceUsed   float64 `json:"output_price_used"`
}

// ComputeSyncStatus tracks the state of provider configuration sync.
type ComputeSyncStatus struct {
	LastSyncAt    string `json:"last_sync_at"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	ProviderCount int    `json:"provider_count"`
}

// ValidateProvider checks that a ComputeProvider has valid field values.
// It returns a non-nil error describing the first validation failure.
func ValidateProvider(p *ComputeProvider) error {
	// base_url must start with "https://".
	if !strings.HasPrefix(p.BaseURL, "https://") {
		return fmt.Errorf("invalid base_url: must start with \"https://\"")
	}

	// protocol must be one of the supported values.
	if !validProtocols[p.Protocol] {
		return fmt.Errorf("invalid protocol %q: must be one of openai, anthropic, gemini", p.Protocol)
	}

	// Prices must be non-negative.
	if p.InputPricePerMToken < 0 {
		return fmt.Errorf("invalid input_price_per_mtoken: must be non-negative, got %f", p.InputPricePerMToken)
	}
	if p.OutputPricePerMToken < 0 {
		return fmt.Errorf("invalid output_price_per_mtoken: must be non-negative, got %f", p.OutputPricePerMToken)
	}

	return nil
}
