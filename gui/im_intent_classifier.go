package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cagent "github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

var unifiedClassifierPtr atomic.Pointer[intent.UnifiedIntentClassifier]

func setUnifiedClassifierForIM(uic *intent.UnifiedIntentClassifier) {
	unifiedClassifierPtr.Store(uic)
	cagent.SetUnifiedClassifier(uic)
}

type taskIntentResult struct {
	Intent     taskIntent
	Matched    string
	Evidence   []string
	Reason     string
	Confidence float64
	// Degraded means this semantic result is a fallback (for example an
	// inconclusive embedding after the LLM classifier failed). It may provide a
	// routing hint, but it must not independently authorize an execution path.
	Degraded bool
	Source   taskIntentSource
}

type llmIntentClassification struct {
	Intent     taskIntent `json:"intent"`
	Confidence float64    `json:"confidence"`
	Reason     string     `json:"reason"`
	Evidence   []string   `json:"evidence"`
}

func classifyTaskIntent(text string) taskIntentResult {
	if uic := unifiedClassifierPtr.Load(); uic != nil {
		result := uic.Classify(intent.MessageContext{Text: text})
		return taskIntentResultFromUnifiedClassification(result)
	}
	return classifyTaskIntentWithoutSemantic(text)
}

func taskIntentResultFromUnifiedClassification(result intent.ClassificationResult) taskIntentResult {
	intentStr, matched, evidence, reason, confidence := result.ToTaskIntent()
	resultIntent := normalizeTaskIntent(taskIntent(intentStr))
	// A failed L3 escalation can leave a strong-but-insufficiently-separated L2
	// result. Never use that degraded signal to choose a coding/SSH execution
	// route. The ordinary agent can clarify instead.
	if result.Degraded && (resultIntent == intentCoding || resultIntent == intentSSH) {
		resultIntent = intentAmbiguous
	}
	return taskIntentResult{
		Intent:     resultIntent,
		Matched:    matched,
		Evidence:   evidence,
		Reason:     reason,
		Confidence: confidence,
		Degraded:   result.Degraded,
		Source:     taskIntentSourceUIC,
	}
}

func classifyTaskIntentWithoutSemantic(text string) taskIntentResult {
	if strings.TrimSpace(text) == "" {
		return taskIntentResult{Intent: intentUnknown, Source: taskIntentSourceSemanticUnavailable, Reason: "empty task text; no execution route classified", Confidence: 0.3}
	}
	return taskIntentResult{Intent: intentUnknown, Source: taskIntentSourceSemanticUnavailable, Reason: "semantic classifier unavailable; no execution route classified", Confidence: 0.45}
}

func (h *IMMessageHandler) classifyTaskIntentForSessionGuard(text string) taskIntentResult {
	if uic := h.getUnifiedClassifier(); uic != nil {
		// Session creation is a later tool-level capability decision, not a
		// reason to make the current turn wait for tree/LLM classification.
		// An inconclusive local result is fail-closed by the caller.
		return taskIntentResultFromUnifiedClassification(uic.ClassifyEmbeddingOnly(intent.MessageContext{Text: text}))
	}
	return classifyTaskIntent(text)
}

func shouldRequireExecutionConfirmationForIntent(msg IMUserMessage, pending *pendingConfirmation, intent taskIntentResult) bool {
	return !msg.IsBackground && pending == nil && strings.TrimSpace(msg.Text) != "" &&
		!intent.Degraded &&
		!intent.IsContinuationMatch() &&
		(intent.Intent == intentCoding || intent.Intent == intentSSH)
}

func shouldConsiderExecutionConfirmation(freshTask bool, msg IMUserMessage, trimmedText string) bool {
	return freshTask && !msg.IsBackground && strings.TrimSpace(trimmedText) != ""
}

func (h *IMMessageHandler) classifyTaskIntentForExecution(userID, text string, attachments []MessageAttachment, httpClient *http.Client) taskIntentResult {
	if uic := h.getUnifiedClassifier(); uic != nil {
		// This is on the path before the main Agent's first request. A full UIC
		// call can escalate an unavailable or ambiguous embedding to L3 and hold
		// the whole turn for several seconds merely to decide whether to display
		// an optional confirmation card. Only use the local L2 signal here.
		//
		// It cannot authorize execution: a degraded/unknown result simply skips
		// the optional card and the normal Agent safety gates remain in force.
		// Attachments are deliberately not sent to a separate intent LLM here for
		// the same reason; their normal Agent path already receives the files.
		result := uic.ClassifyEmbeddingOnly(intent.MessageContext{
			Text:   text,
			UserID: userID,
		})
		return taskIntentResultFromUnifiedClassification(result)
	}
	// Compatibility fallback for minimal/standalone handlers that have no UIC
	// at all. The desktop runtime initializes UIC before accepting turns, so
	// normal task startup remains on the non-blocking path above.
	fallback := classifyTaskIntentWithoutSemantic(text)
	if h == nil || h.app == nil || httpClient == nil {
		return fallback
	}
	cfg := h.getMaclawLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fallback
	}
	llmResult, err := h.classifyTaskIntentWithLLM(cfg, userID, text, attachments, httpClient)
	if err != nil {
		return fallback
	}
	if llmResult.Confidence < 0.6 {
		llmResult.Intent = intentAmbiguous
		if strings.TrimSpace(llmResult.Reason) == "" {
			llmResult.Reason = "model confidence too low; conservatively downgraded to ambiguous"
		}
	}
	return llmResult
}

func isDecisiveTaskIntentResult(result taskIntentResult) bool {
	if result.Degraded {
		return false
	}
	if result.IsContinuationMatch() {
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
	return taskIntentResultFromUnifiedClassification(result), true
}

func (h *IMMessageHandler) classifyTaskIntentWithLLM(cfg corelib.MaclawLLMConfig, userID, text string, attachments []MessageAttachment, httpClient *http.Client) (taskIntentResult, error) {
	messages := buildIntentClassifierMessages(text, attachments)
	parsed, err := h.requestIntentClassification(cfg, userID, messages, httpClient)
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

func (h *IMMessageHandler) requestIntentClassification(cfg corelib.MaclawLLMConfig, userID string, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return h.requestIntentClassificationResponses(cfg, userID, messages, httpClient)
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return h.requestIntentClassificationAnthropic(cfg, userID, messages, httpClient)
	}
	return h.requestIntentClassificationOpenAI(cfg, userID, messages, httpClient)
}

func (h *IMMessageHandler) requestIntentClassificationResponses(cfg corelib.MaclawLLMConfig, userID string, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "task-intent-classifier", OwnerID: userID})
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return llmIntentClassification{}, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()

	responseFormat := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "task_intent_classification",
			"schema": intentClassifierJSONSchema,
		},
	}
	req, body, endpoint, err := llm.NewResponsesAPIRequest(scheduledCtx, cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream:    false,
		ExtraBody: map[string]interface{}{"response_format": responseFormat},
	})
	if err != nil {
		return llmIntentClassification{}, err
	}
	resp, err := httpClient.Do(req)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		return llmIntentClassification{}, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := dumpLLMContext(resp.StatusCode, "intent classify responses request failed", body, h.getTempDir())
		globalLLMScheduler.ObserveResult(trace, err)
		return llmIntentClassification{}, err
	}
	parsedResp, err := llm.ParseNonStreamResponsesAPIResponse(resp)
	if err != nil {
		return llmIntentClassification{}, err
	}
	return decodeIntentClassificationContent(firstLLMResponseText(parsedResp))
}

func (h *IMMessageHandler) requestIntentClassificationOpenAI(cfg corelib.MaclawLLMConfig, userID string, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "task-intent-classifier", OwnerID: userID})
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return llmIntentClassification{}, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()

	responseFormat := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "task_intent_classification",
			"schema": intentClassifierJSONSchema,
		},
	}
	req, body, endpoint, err := llm.NewOpenAIChatRequest(scheduledCtx, cfg, messages, llm.OpenAIChatRequestOptions{Stream: false, ResponseFormat: responseFormat})
	if err != nil {
		return llmIntentClassification{}, err
	}
	resp, err := httpClient.Do(req)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		return llmIntentClassification{}, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := dumpLLMContext(resp.StatusCode, "intent classify request failed", body, h.getTempDir())
		globalLLMScheduler.ObserveResult(trace, err)
		return llmIntentClassification{}, err
	}
	parsedResp, err := llm.ParseNonStreamOpenAIResponse(resp)
	if err != nil {
		return llmIntentClassification{}, err
	}
	return decodeIntentClassificationContent(firstLLMResponseText(parsedResp))
}

func (h *IMMessageHandler) requestIntentClassificationAnthropic(cfg corelib.MaclawLLMConfig, userID string, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "task-intent-classifier", OwnerID: userID})
	resp, err := h.doAnthropicLLMRequestWithContext(ctx, cfg, messages, nil, httpClient)
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
		return taskIntentResult{}, fmt.Errorf("unknown intent %q", parsed.Intent.String())
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
	return taskIntentResult{Intent: intentValue, Matched: matched, Evidence: evidence, Reason: strings.TrimSpace(parsed.Reason), Confidence: confidence, Source: taskIntentSourceLLM}, nil
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
