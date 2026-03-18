package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LLMSecurityVerdict represents the safety verdict from LLM security review.
type LLMSecurityVerdict string

const (
	VerdictSafe      LLMSecurityVerdict = "safe"
	VerdictRisky     LLMSecurityVerdict = "risky"
	VerdictDangerous LLMSecurityVerdict = "dangerous"
)

// LLMSecurityReview performs LLM-assisted security review on tool invocations.
type LLMSecurityReview struct {
	llmConfig MaclawLLMConfig
	client    *http.Client // 5-second timeout
}

// NewLLMSecurityReview creates a new LLMSecurityReview with a 5-second HTTP timeout.
func NewLLMSecurityReview(cfg MaclawLLMConfig) *LLMSecurityReview {
	return &LLMSecurityReview{
		llmConfig: cfg,
		client:    &http.Client{}, // per-request timeout via context
	}
}

// Review performs an LLM security review on the given risk context and assessment.
// It returns the verdict, a human-readable explanation, and any error.
//
// Behaviour:
//   - If LLM is not configured (empty URL or model), returns VerdictSafe immediately.
//   - On timeout or LLM error, falls back to rule-based assessment derived from
//     the risk level in the provided RiskAssessment.
func (r *LLMSecurityReview) Review(ctx RiskContext, assessment RiskAssessment) (LLMSecurityVerdict, string, error) {
	// Skip review when LLM is not configured.
	if !r.isConfigured() {
		return VerdictSafe, "LLM not configured, skipping security review", nil
	}

	verdict, explanation, err := r.callLLM(ctx, assessment)
	if err != nil {
		// Timeout or any LLM error → fall back to rule-based assessment.
		fbVerdict, fbReason := ruleBasedFallback(assessment.Level)
		return fbVerdict, fmt.Sprintf("LLM review failed (%v), fallback: %s", err, fbReason), nil
	}

	return verdict, explanation, nil
}

// isConfigured returns true when the LLM endpoint and model are set.
func (r *LLMSecurityReview) isConfigured() bool {
	return strings.TrimSpace(r.llmConfig.URL) != "" &&
		strings.TrimSpace(r.llmConfig.Model) != ""
}

// callLLM sends the security review prompt to the configured LLM and parses
// the response into a verdict + explanation.
func (r *LLMSecurityReview) callLLM(riskCtx RiskContext, assessment RiskAssessment) (LLMSecurityVerdict, string, error) {
	prompt := buildSecurityPrompt(riskCtx, assessment)

	messages := []interface{}{
		map[string]string{"role": "system", "content": "You are a security reviewer for an AI coding assistant. Evaluate the safety of tool calls and respond with a JSON object containing \"verdict\" (one of \"safe\", \"risky\", \"dangerous\") and \"explanation\" (a brief reason)."},
		map[string]string{"role": "user", "content": prompt},
	}

	result, err := doSimpleLLMRequest(r.llmConfig, messages, r.client, 5*time.Second)
	if err != nil {
		return "", "", err
	}

	return parseSecurityVerdict(result.Content)
}

// parseSecurityVerdict extracts the verdict and explanation from the LLM response content.
func parseSecurityVerdict(content string) (LLMSecurityVerdict, string, error) {
	content = strings.TrimSpace(content)

	// Try to parse as JSON first.
	var verdictResp struct {
		Verdict     string `json:"verdict"`
		Explanation string `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(content), &verdictResp); err == nil {
		v := normalizeVerdict(verdictResp.Verdict)
		if v != "" {
			return v, verdictResp.Explanation, nil
		}
	}

	// Fallback: scan the raw text for verdict keywords.
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "dangerous"):
		return VerdictDangerous, content, nil
	case strings.Contains(lower, "risky"):
		return VerdictRisky, content, nil
	case strings.Contains(lower, "safe"):
		return VerdictSafe, content, nil
	default:
		return VerdictRisky, content, nil // default to risky when unclear
	}
}

// normalizeVerdict maps a string to a known LLMSecurityVerdict.
func normalizeVerdict(s string) LLMSecurityVerdict {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "safe":
		return VerdictSafe
	case "risky":
		return VerdictRisky
	case "dangerous":
		return VerdictDangerous
	default:
		return ""
	}
}

// buildSecurityPrompt constructs the prompt sent to the LLM for security review.
func buildSecurityPrompt(ctx RiskContext, assessment RiskAssessment) string {
	var sb strings.Builder
	sb.WriteString("Please evaluate the safety of the following tool call:\n\n")
	sb.WriteString(fmt.Sprintf("Tool: %s\n", ctx.ToolName))
	sb.WriteString(fmt.Sprintf("Session: %s\n", ctx.SessionID))
	sb.WriteString(fmt.Sprintf("Project Path: %s\n", ctx.ProjectPath))
	sb.WriteString(fmt.Sprintf("Permission Mode: %s\n", ctx.PermissionMode))
	sb.WriteString(fmt.Sprintf("Consecutive Call Count: %d\n", ctx.CallCount))
	sb.WriteString(fmt.Sprintf("Risk Level: %s\n", assessment.Level))
	sb.WriteString(fmt.Sprintf("Risk Reason: %s\n", assessment.Reason))

	if len(ctx.Arguments) > 0 {
		argsJSON, err := json.Marshal(ctx.Arguments)
		if err == nil {
			sb.WriteString(fmt.Sprintf("Arguments: %s\n", string(argsJSON)))
		}
	}

	sb.WriteString("\nRespond with a JSON object: {\"verdict\": \"safe|risky|dangerous\", \"explanation\": \"...\"}")
	return sb.String()
}

// ruleBasedFallback maps a RiskLevel to an LLMSecurityVerdict when the LLM
// is unavailable or times out.
func ruleBasedFallback(level RiskLevel) (LLMSecurityVerdict, string) {
	switch level {
	case RiskCritical:
		return VerdictDangerous, "critical risk level mapped to dangerous"
	case RiskHigh:
		return VerdictRisky, "high risk level mapped to risky"
	case RiskMedium:
		return VerdictRisky, "medium risk level mapped to risky"
	default:
		return VerdictSafe, "low risk level mapped to safe"
	}
}
