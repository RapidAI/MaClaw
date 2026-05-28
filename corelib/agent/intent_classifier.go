package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// TaskIntent represents the classified execution route of a user message.
type TaskIntent string

const (
	IntentCoding    TaskIntent = "coding"
	IntentSSH       TaskIntent = "ssh"
	IntentNonCoding TaskIntent = "non_coding"
	IntentAmbiguous TaskIntent = "ambiguous"
	IntentUnknown   TaskIntent = "unknown"
)

// TaskIntentResult holds the full classification result.
type TaskIntentResult struct {
	Intent     TaskIntent
	Matched    string
	Evidence   []string
	Reason     string
	Confidence float64
	Source     string
}

// LLMIntentClassification is the JSON structure returned by the LLM
// intent classifier.
type LLMIntentClassification struct {
	Intent     string   `json:"intent"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
}

var unifiedClassifierPtr atomic.Pointer[intent.UnifiedIntentClassifier]

// SetUnifiedClassifier sets the package-level UIC used by ClassifyTaskIntent.
func SetUnifiedClassifier(uic *intent.UnifiedIntentClassifier) {
	unifiedClassifierPtr.Store(uic)
}

// GetUnifiedClassifier returns the package-level UIC used by ClassifyTaskIntent.
func GetUnifiedClassifier() *intent.UnifiedIntentClassifier {
	return unifiedClassifierPtr.Load()
}

// IntentClassifierJSONSchema is the JSON schema for the LLM intent classifier.
var IntentClassifierJSONSchema = map[string]interface{}{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]interface{}{
		"intent":     map[string]interface{}{"type": "string", "enum": []string{"coding", "ssh", "non_coding", "ambiguous"}},
		"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
		"reason":     map[string]interface{}{"type": "string"},
		"evidence":   map[string]interface{}{"type": "array", "maxItems": 4, "items": map[string]interface{}{"type": "string"}},
	},
	"required": []string{"intent", "confidence", "reason", "evidence"},
}

// IntentClassifierSystemPrompt is the system prompt for the LLM intent classifier.
const IntentClassifierSystemPrompt = `You classify the execution route for the current request.

Routes:
- coding: source-code editing, implementation, debugging, tests, build fixes, or project code work.
- ssh: remote host access, server logs, deployment environment commands, service restart, or file transfer.
- non_coding: document work, translation, summary, knowledge-base capture, reports, PPT/PDF, or attachment understanding.
- ambiguous: not enough information, or multiple execution routes are plausible.

Classify by the action required, not by isolated words. Return only JSON matching the schema.`

// ClassifyTaskIntent classifies a user message into a task intent using the
// UIC. When semantic classification is unavailable, it returns unknown so the
// normal agent path can handle the request. Safety confirmation belongs to
// decisive high-risk routes and tool/action execution, not to the absence of a
// workflow classification.
func ClassifyTaskIntent(text string) TaskIntentResult {
	if uic := GetUnifiedClassifier(); uic != nil {
		result := uic.Classify(intent.MessageContext{Text: text})
		intentStr, matched, evidence, reason, confidence := result.ToTaskIntent()
		return TaskIntentResult{
			Intent:     TaskIntent(intentStr),
			Matched:    matched,
			Evidence:   evidence,
			Reason:     reason,
			Confidence: confidence,
			Source:     "uic",
		}
	}
	if strings.TrimSpace(text) == "" {
		return TaskIntentResult{Intent: IntentUnknown, Source: "classifier-unavailable", Reason: "empty task text; no execution route classified"}
	}
	return TaskIntentResult{Intent: IntentUnknown, Source: "classifier-unavailable", Reason: "UIC unavailable; no execution route classified"}
}

// ShouldRequireExecutionConfirmationForIntent determines whether a message
// requires execution confirmation based on its classified intent.
func ShouldRequireExecutionConfirmationForIntent(msg UserMessage, pending *PendingConfirmation, intentResult TaskIntentResult, looksLikeFreshTaskRequest func(string) bool) bool {
	if msg.IsBackground || pending != nil {
		return false
	}
	if strings.TrimSpace(msg.Text) == "" {
		return false
	}
	return intentResult.Intent == IntentCoding || intentResult.Intent == IntentSSH
}

// BuildIntentClassifierMessages constructs the LLM messages for intent
// classification.
func BuildIntentClassifierMessages(text string, attachments []MessageAttachment) []interface{} {
	payload := map[string]interface{}{
		"text":             strings.TrimSpace(text),
		"has_attachments":  len(attachments) > 0,
		"attachment_types": SummarizeAttachmentTypes(attachments),
		"attachment_names": SummarizeAttachmentNames(attachments),
	}
	payloadJSON, _ := json.Marshal(payload)
	return []interface{}{
		map[string]interface{}{"role": "system", "content": IntentClassifierSystemPrompt},
		map[string]interface{}{"role": "user", "content": string(payloadJSON)},
	}
}

// SummarizeAttachmentTypes returns deduplicated attachment type strings.
func SummarizeAttachmentTypes(attachments []MessageAttachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	types := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		kind := strings.TrimSpace(strings.ToLower(attachment.Type))
		if kind == "" && strings.TrimSpace(attachment.MimeType) != "" {
			kind = strings.TrimSpace(strings.ToLower(strings.SplitN(attachment.MimeType, "/", 2)[0]))
		}
		if kind == "" {
			kind = "file"
		}
		types = AppendIfMissing(types, kind)
	}
	return types
}

// SummarizeAttachmentNames returns up to 4 attachment file names.
func SummarizeAttachmentNames(attachments []MessageAttachment) []string {
	if len(attachments) == 0 {
		return nil
	}
	names := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		if name := strings.TrimSpace(attachment.FileName); name != "" {
			names = append(names, name)
		}
		if len(names) >= 4 {
			break
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// FirstLLMResponseText extracts the text content from the first choice
// of an LLM response.
func FirstLLMResponseText(resp *llm.Response) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

// DecodeIntentClassificationContent parses the JSON content from an LLM
// intent classification response.
func DecodeIntentClassificationContent(content string) (LLMIntentClassification, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if content == "" {
		return LLMIntentClassification{}, fmt.Errorf("empty intent classification response")
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}
	var parsed LLMIntentClassification
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return LLMIntentClassification{}, err
	}
	return parsed, nil
}

// NormalizeIntentClassification converts a raw LLM classification into a
// TaskIntentResult.
func NormalizeIntentClassification(parsed LLMIntentClassification) (TaskIntentResult, error) {
	ti := NormalizeTaskIntent(parsed.Intent)
	if ti == IntentUnknown {
		return TaskIntentResult{}, fmt.Errorf("unknown intent %q", parsed.Intent)
	}
	evidence := NormalizeIntentEvidence(parsed.Evidence)
	matched := firstEvidence(evidence, strings.TrimSpace(parsed.Reason))
	confidence := parsed.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return TaskIntentResult{
		Intent:     ti,
		Matched:    matched,
		Evidence:   evidence,
		Reason:     strings.TrimSpace(parsed.Reason),
		Confidence: confidence,
		Source:     "llm",
	}, nil
}

// NormalizeTaskIntent normalizes a raw intent string to a TaskIntent constant.
func NormalizeTaskIntent(raw string) TaskIntent {
	switch TaskIntent(strings.TrimSpace(strings.ToLower(raw))) {
	case IntentCoding:
		return IntentCoding
	case IntentSSH:
		return IntentSSH
	case IntentNonCoding:
		return IntentNonCoding
	case IntentAmbiguous:
		return IntentAmbiguous
	default:
		return IntentUnknown
	}
}

// NormalizeIntentEvidence deduplicates and trims evidence strings.
func NormalizeIntentEvidence(items []string) []string {
	var normalized []string
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			normalized = AppendIfMissing(normalized, trimmed)
		}
		if len(normalized) >= 4 {
			break
		}
	}
	return normalized
}

// AppendIfMissing appends value to items if not already present.
func AppendIfMissing(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func firstEvidence(items []string, fallback string) string {
	if len(items) > 0 {
		return items[0]
	}
	return fallback
}

// FormatIntentEvidence formats intent evidence for display.
func FormatIntentEvidence(result TaskIntentResult) string {
	if len(result.Evidence) == 0 {
		if result.Matched != "" {
			return fmt.Sprintf("%q", result.Matched)
		}
		return "no local evidence"
	}
	if len(result.Evidence) == 1 {
		return fmt.Sprintf("%q", result.Evidence[0])
	}
	return fmt.Sprintf("%q", strings.Join(result.Evidence, `", "`))
}
