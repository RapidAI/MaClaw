package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

var unifiedClassifier *intent.UnifiedIntentClassifier

func setUnifiedClassifierForIM(uic *intent.UnifiedIntentClassifier) { unifiedClassifier = uic }

type taskIntent string

const (
	intentCoding    taskIntent = "coding"
	intentSSH       taskIntent = "ssh"
	intentNonCoding taskIntent = "non_coding"
	intentAmbiguous taskIntent = "ambiguous"
	intentUnknown   taskIntent = "unknown"
)

type taskIntentResult struct {
	Intent     taskIntent
	Matched    string
	Evidence   []string
	Reason     string
	Confidence float64
	Source     string
}

type llmIntentClassification struct {
	Intent     string   `json:"intent"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
}

func classifyTaskIntent(text string) taskIntentResult {
	if uic := unifiedClassifier; uic != nil {
		result := uic.Classify(intent.MessageContext{Text: text})
		intentStr, matched, evidence, reason, confidence := result.ToTaskIntent()
		return taskIntentResult{Intent: taskIntent(intentStr), Matched: matched, Evidence: evidence, Reason: reason, Confidence: confidence, Source: "uic"}
	}
	return classifyTaskIntentWithoutSemantic(text)
}

func classifyTaskIntentWithoutSemantic(text string) taskIntentResult {
	if strings.TrimSpace(text) == "" {
		return taskIntentResult{Intent: intentUnknown, Source: "semantic-unavailable", Reason: "empty task text; no execution route classified", Confidence: 0.3}
	}
	return taskIntentResult{Intent: intentUnknown, Source: "semantic-unavailable", Reason: "semantic classifier unavailable; no execution route classified", Confidence: 0.45}
}

func (h *IMMessageHandler) classifyTaskIntentForSessionGuard(text string) taskIntentResult {
	if result, ok := h.classifyTaskIntentWithUIC(text); ok {
		return result
	}
	return classifyTaskIntent(text)
}

func shouldRequireExecutionConfirmationForIntent(msg IMUserMessage, pending *pendingConfirmation, intent taskIntentResult) bool {
	return !msg.IsBackground && pending == nil && strings.TrimSpace(msg.Text) != "" &&
		intent.Matched != "continuation" &&
		(intent.Intent == intentCoding || intent.Intent == intentSSH)
}

func shouldConsiderExecutionConfirmation(freshTask bool, msg IMUserMessage, trimmedText string) bool {
	return freshTask && !msg.IsBackground && strings.TrimSpace(trimmedText) != ""
}

func (h *IMMessageHandler) classifyTaskIntentForExecution(text string, attachments []MessageAttachment, httpClient *http.Client) taskIntentResult {
	if len(attachments) == 0 {
		if result, ok := h.classifyTaskIntentWithUIC(text); ok && isDecisiveTaskIntentResult(result) {
			return result
		}
	}
	fallback := classifyTaskIntentWithoutSemantic(text)
	if h == nil || h.app == nil || httpClient == nil {
		return fallback
	}
	cfg := h.getMaclawLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fallback
	}
	llmResult, err := h.classifyTaskIntentWithLLM(cfg, text, attachments, httpClient)
	if err != nil {
		if result, ok := h.classifyTaskIntentWithUIC(text); ok && isDecisiveTaskIntentResult(result) {
			return result
		}
		return fallback
	}
	if llmResult.Confidence < 0.6 {
		if fallback.Intent != intentAmbiguous && fallback.Intent != intentUnknown {
			return fallback
		}
		llmResult.Intent = intentAmbiguous
		if strings.TrimSpace(llmResult.Reason) == "" {
			llmResult.Reason = "model confidence too low; conservatively downgraded to ambiguous"
		}
		return llmResult
	}
	return llmResult
}

func isDecisiveTaskIntentResult(result taskIntentResult) bool {
	if result.Matched == "continuation" {
		return true
	}
	if result.Intent == intentUnknown || result.Intent == intentAmbiguous {
		return false
	}
	return result.Confidence >= 0.6
}

func (h *IMMessageHandler) classifyTaskIntentWithUIC(text string) (taskIntentResult, bool) {
	uic := h.getUnifiedClassifier()
	if uic == nil {
		return taskIntentResult{}, false
	}
	result := uic.Classify(intent.MessageContext{Text: text})
	intentStr, matched, evidence, reason, confidence := result.ToTaskIntent()
	return taskIntentResult{
		Intent:     taskIntent(intentStr),
		Matched:    matched,
		Evidence:   evidence,
		Reason:     reason,
		Confidence: confidence,
		Source:     "uic",
	}, true
}

func (h *IMMessageHandler) classifyTaskIntentWithLLM(cfg corelib.MaclawLLMConfig, text string, attachments []MessageAttachment, httpClient *http.Client) (taskIntentResult, error) {
	messages := buildIntentClassifierMessages(text, attachments)
	parsed, err := h.requestIntentClassification(cfg, messages, httpClient)
	if err != nil {
		return taskIntentResult{}, err
	}
	return normalizeIntentClassification(parsed)
}

func buildIntentClassifierMessages(text string, attachments []MessageAttachment) []interface{} {
	payload := map[string]interface{}{
		"text":             strings.TrimSpace(text),
		"has_attachments":  len(attachments) > 0,
		"attachment_types": summarizeAttachmentTypes(attachments),
		"attachment_names": summarizeAttachmentNames(attachments),
	}
	payloadJSON, _ := json.Marshal(payload)
	return []interface{}{
		map[string]interface{}{"role": "system", "content": intentClassifierSystemPrompt},
		map[string]interface{}{"role": "user", "content": string(payloadJSON)},
	}
}

func summarizeAttachmentTypes(attachments []MessageAttachment) []string {
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
		types = appendIfMissing(types, kind)
	}
	return types
}

func summarizeAttachmentNames(attachments []MessageAttachment) []string {
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

func (h *IMMessageHandler) requestIntentClassification(cfg corelib.MaclawLLMConfig, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return h.requestIntentClassificationAnthropic(cfg, messages, httpClient)
	}
	return h.requestIntentClassificationOpenAI(cfg, messages, httpClient)
}

func (h *IMMessageHandler) requestIntentClassificationOpenAI(cfg corelib.MaclawLLMConfig, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	responseFormat := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "task_intent_classification",
			"schema": intentClassifierJSONSchema,
		},
	}
	req, body, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{Stream: false, ResponseFormat: responseFormat})
	if err != nil {
		return llmIntentClassification{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return llmIntentClassification{}, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return llmIntentClassification{}, dumpLLMContext(resp.StatusCode, "intent classify request failed", body, h.getTempDir())
	}
	parsedResp, err := llm.ParseNonStreamOpenAIResponse(resp)
	if err != nil {
		return llmIntentClassification{}, err
	}
	return decodeIntentClassificationContent(firstLLMResponseText(parsedResp))
}

func (h *IMMessageHandler) requestIntentClassificationAnthropic(cfg corelib.MaclawLLMConfig, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	resp, err := h.doAnthropicLLMRequest(cfg, messages, nil, httpClient)
	if err != nil {
		return llmIntentClassification{}, err
	}
	return decodeIntentClassificationContent(firstLLMResponseText(resp))
}

func firstLLMResponseText(resp *llm.Response) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

func decodeIntentClassificationContent(content string) (llmIntentClassification, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if content == "" {
		return llmIntentClassification{}, fmt.Errorf("empty intent classification response")
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}
	var parsed llmIntentClassification
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return llmIntentClassification{}, err
	}
	return parsed, nil
}

func normalizeIntentClassification(parsed llmIntentClassification) (taskIntentResult, error) {
	intentValue := normalizeTaskIntent(parsed.Intent)
	if intentValue == intentUnknown {
		return taskIntentResult{}, fmt.Errorf("unknown intent %q", parsed.Intent)
	}
	evidence := normalizeIntentEvidence(parsed.Evidence)
	matched := firstEvidence(evidence, strings.TrimSpace(parsed.Reason))
	confidence := parsed.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return taskIntentResult{Intent: intentValue, Matched: matched, Evidence: evidence, Reason: strings.TrimSpace(parsed.Reason), Confidence: confidence, Source: "llm"}, nil
}

func normalizeTaskIntent(raw string) taskIntent {
	switch taskIntent(strings.TrimSpace(strings.ToLower(raw))) {
	case intentCoding:
		return intentCoding
	case intentSSH:
		return intentSSH
	case intentNonCoding:
		return intentNonCoding
	case intentAmbiguous:
		return intentAmbiguous
	default:
		return intentUnknown
	}
}

func normalizeIntentEvidence(items []string) []string {
	var normalized []string
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			normalized = appendIfMissing(normalized, trimmed)
		}
		if len(normalized) >= 4 {
			break
		}
	}
	return normalized
}

var intentClassifierJSONSchema = map[string]interface{}{
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

const intentClassifierSystemPrompt = `You classify the execution route for the current request.

Routes: coding, ssh, non_coding, ambiguous.
Classify by the execution required, not by topic. Output only JSON matching the schema.`

func firstEvidence(items []string, fallback string) string {
	if len(items) > 0 {
		return items[0]
	}
	return fallback
}

func appendIfMissing(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func formatIntentEvidence(result taskIntentResult) string {
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
