package intent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// llmResponsePayload is the structured JSON response expected from the LLM
// when performing Layer 3 intent classification.
type llmResponsePayload struct {
	Intent     string   `json:"intent"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Secondary  []string `json:"secondary"`
}

// classifyByLLM performs Layer 3 LLM-based classification by sending the user
// message to the configured LLM with a unified system prompt covering all 12
// intent labels and disambiguation rules.
//
// Returns (result, error). If llmFunc returns an error (timeout, network, etc.),
// the error is propagated so the caller can fall back to a lower layer result.
// If the LLM returns confidence < 0.60, the result is treated as ambiguous.
func classifyByLLM(llmFunc LLMClassifyFunc, msg MessageContext) (ClassificationResult, error) {
	if llmFunc == nil {
		return ClassificationResult{}, fmt.Errorf("LLM classify function is nil")
	}

	systemPrompt := buildLLMSystemPrompt()

	// Call the LLM. Timeout enforcement is done by the caller via context;
	// if llmFunc returns an error we propagate it.
	rawResponse, err := llmFunc(systemPrompt, msg.Text)
	if err != nil {
		return ClassificationResult{}, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse the structured JSON response.
	payload, err := parseLLMResponse(rawResponse)
	if err != nil {
		return ClassificationResult{}, fmt.Errorf("LLM response parse failed: %w", err)
	}

	// Validate the primary intent label.
	primary := IntentLabel(payload.Intent)
	if !primary.IsValid() {
		return ClassificationResult{}, fmt.Errorf("LLM returned invalid intent label: %q", payload.Intent)
	}

	// Clamp confidence to [0, 1].
	confidence := payload.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	// If confidence < 0.60, treat as ambiguous.
	if confidence < 0.60 {
		primary = LabelAmbiguous
		confidence = payload.Confidence // preserve original for logging
	}

	// Parse secondary labels.
	var secondary []IntentLabel
	for _, s := range payload.Secondary {
		label := IntentLabel(s)
		if label.IsValid() && label != primary {
			secondary = append(secondary, label)
		}
	}

	reason := payload.Reason
	if reason == "" {
		reason = "LLM classification"
	}

	return ClassificationResult{
		Primary:    primary,
		Confidence: confidence,
		Secondary:  secondary,
		Layer:      3,
		Reason:     fmt.Sprintf("llm: %s", reason),
	}, nil
}

// parseLLMResponse extracts the llmResponsePayload from the raw LLM output.
// It handles cases where the JSON may be wrapped in markdown code fences.
func parseLLMResponse(raw string) (llmResponsePayload, error) {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences if present (```json ... ``` or ``` ... ```).
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) >= 3 {
			// Find the closing ``` line from the end.
			end := len(lines) - 1
			for end > 0 && strings.TrimSpace(lines[end]) != "```" {
				end--
			}
			if end > 0 {
				raw = strings.Join(lines[1:end], "\n")
			}
		}
	}

	raw = strings.TrimSpace(raw)

	var payload llmResponsePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return llmResponsePayload{}, fmt.Errorf("JSON unmarshal: %w (raw: %.100s)", err, raw)
	}
	return payload, nil
}


// buildLLMSystemPrompt returns the unified system prompt for Layer 3 LLM
// classification. It covers all 12 intent labels with descriptions and
// includes disambiguation rules for known confusing cases.
func buildLLMSystemPrompt() string {
	return `You are an intent classifier. Given a user message, classify it into exactly one primary intent label and optionally one or more secondary labels.

## Intent Labels

1. "coding" — User wants to create, develop, or write new code/software/application/game/tool/script
2. "ssh" — User wants to connect to a remote server, execute remote commands, or manage remote systems via SSH
3. "non_coding" — User wants non-coding tasks: translation, summarization, writing, research, information lookup, general Q&A
4. "browser" — User wants browser automation: navigate web pages, click elements, fill forms, take screenshots, record/replay browser actions
5. "search" — User wants to search the web for information, documentation, or solutions
6. "document_delivery" — User wants to open, send, or deliver files/documents
7. "bug_fix" — User wants to fix bugs, debug issues, troubleshoot errors, resolve crashes
8. "continuation" — User is giving a short action phrase to continue or start a previously discussed task (e.g., "继续", "开工", "go ahead")
9. "maintenance" — User wants to refactor, optimize, clean up, upgrade, or improve existing code without adding new features
10. "office" — User wants to create or manipulate office documents: PPT/slides, Excel/spreadsheets, Word documents
11. "ambiguous" — The message is unclear and could belong to multiple categories with no dominant signal
12. "unknown" — The message does not fit any known category

## Disambiguation Rules

- "更新" (update): If the object is software/package/service/server, classify as "maintenance" or "ssh". If the object is a document/file content, classify as "non_coding" or "document_delivery".
- "页面" + "打开": If the context is about browser automation (navigating URLs, clicking web elements), classify as "browser". If the context is about game development or app UI description (e.g., "页面上有飞机和子弹"), classify as "coding".
- Short messages (≤5 characters) like "继续", "开工", "好的": Classify as "continuation" unless there is strong evidence of another intent.
- "修复" + creation keywords (开发/游戏/应用): If creation keywords dominate, classify as "coding" not "bug_fix".
- "搜索" in context of web search vs code search: Web information lookup → "search"; searching within codebase → "coding" or "maintenance".

## Output Format

Respond with ONLY a JSON object (no markdown, no explanation outside JSON):
{"intent": "<primary_label>", "confidence": <0.0-1.0>, "reason": "<brief explanation>", "secondary": ["<label>", ...]}

Rules:
- "confidence" must be between 0.0 and 1.0
- "secondary" is an array of zero or more labels that also partially apply
- "reason" should be a brief (1-2 sentence) explanation of why this intent was chosen
- Do NOT include the primary intent in the secondary array`
}
