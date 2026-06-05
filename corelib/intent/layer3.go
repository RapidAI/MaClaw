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

// classifyByLLM performs Layer 3 LLM-based classification using the
// pre-built system prompt. Used in the single-channel LLM-only fallback path.
func classifyByLLM(llmFunc LLMClassifyFunc, prompt string, msg MessageContext) (ClassificationResult, error) {
	if llmFunc == nil {
		return ClassificationResult{}, fmt.Errorf("LLM classify function is nil")
	}

	rawResponse, err := llmFunc(prompt, msg.Text)
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

// buildLLMSystemPrompt constructs the system prompt for Layer 3 LLM
// classification from the given definitions. The intent labels section is
// auto-generated from TreeText, ensuring it stays in sync with the
// Intent Tree prompt used by the fusion pipeline.
//
// Called once during New() — the result is stored on the classifier instance.
func buildLLMSystemPrompt(defs []IntentDefinition) string {

	var b strings.Builder
	b.WriteString(`You are an intent classifier. Given a user message, classify it into exactly one primary intent label and optionally one or more secondary labels.

## Intent Labels

`)

	// Auto-generate from definitions — same data as Intent Tree but in numbered list format.
	for i, def := range defs {
		fmt.Fprintf(&b, "%d. \"%s\" — %s\n", i+1, def.Label, def.TreeText)
	}

	b.WriteString(`
## Disambiguation Principles

When a message is ambiguous (could belong to multiple categories depending on context), follow these principles:

1. **Action + Object analysis**: Focus on what the user wants to DO (action verb) and what they want to do it TO (object). The object determines the domain more than the action.
2. **Context-dependent messages**: If the same message could mean different things in different contexts (e.g., "关掉chrome" could be a desktop operation or a server operation), classify based on the strongest signal in the message itself. If no strong signal exists, use "ambiguous" with appropriate secondary labels — do NOT hardcode a single answer.
3. **Creation vs operation**: "制作/设计/生成 X" (creating new X) is different from "打开/查看/转换 X" (operating on existing X). Creation maps to the domain of X; operation maps to the action type (file operation → non_coding or document_delivery).
4. **Keyword context**: A keyword like "页面" means different things in different contexts — game UI description → coding; browser automation → browser; file viewing → non_coding. Use surrounding words to disambiguate.
5. **Short messages**: Messages ≤5 characters like "继续", "开工", "好的" → "continuation" unless there is strong evidence of another intent.
6. **Mixed signals**: When creation keywords (开发/游戏) co-occur with fix keywords (修复/bug), creation dominates → "coding" not "bug_fix".
7. **Multi-phase delivery**: If the primary work is research/writing but the user asks to publish/login/submit on a named web platform (for example Zhihu), keep the primary work label and add "browser" as a secondary label because delivery requires browser automation.
8. **When truly uncertain**: Set confidence lower (0.50-0.65) and populate "secondary" with alternative labels. Do NOT force a high-confidence answer when the message is genuinely ambiguous without conversation context.

## Output Format

Respond with ONLY a JSON object (no markdown, no explanation outside JSON):
{"intent": "<primary_label>", "confidence": <0.0-1.0>, "reason": "<brief explanation>", "secondary": ["<label>", ...]}

Rules:
- "confidence" must be between 0.0 and 1.0
- "secondary" is an array of zero or more labels that also partially apply
- "reason" should be a brief (1-2 sentence) explanation of why this intent was chosen
- Do NOT include the primary intent in the secondary array`)

	return b.String()
}
