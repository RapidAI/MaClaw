package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// unifiedClassifier is the package-level UIC instance. When non-nil,
// classifyTaskIntent delegates to it instead of using keyword rules.
// Set via setUnifiedClassifierForIM during app initialization.
var unifiedClassifier *intent.UnifiedIntentClassifier

// setUnifiedClassifierForIM sets the package-level UIC used by classifyTaskIntent.
func setUnifiedClassifierForIM(uic *intent.UnifiedIntentClassifier) {
	unifiedClassifier = uic
}

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

var sshKeywords = []string{
	"ssh", "服务器", "服务端", "主机", "远程机器", "远程主机", "云服务器", "线上机器",
	"登录服务器", "连上服务器", "连接服务器", "远程登录", "看日志", "查看日志", "日志", "tail -f",
	"journalctl", "systemctl", "service ", "nginx", "docker", "docker compose", "k8s", "kubectl",
	"pm2", "supervisor", "重启服务", "重启 nginx", "重启进程", "上传到服务器", "下载服务器文件",
	"sftp", "scp", "rsync", "端口", "进程", "服务器文件", "服务器上", "远程执行",
	"host", "user", "label", "initial_command",
}

var ambiguousKeywords = []string{
	"部署", "deploy", "上线", "线上问题", "线上故障", "服务挂了", "服务异常", "环境问题",
	"处理一下线上问题", "看看服务", "看下服务", "排查一下", "处理一下这个项目",
}

var ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

type llmIntentClassification struct {
	Intent     string   `json:"intent"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
}

func classifyTaskIntent(text string) taskIntentResult {
	// Delegate to UIC when available.
	if uic := unifiedClassifier; uic != nil {
		result := uic.Classify(intent.MessageContext{Text: text})
		intentStr, matched, evidence, reason, confidence := result.ToTaskIntent()
		return taskIntentResult{
			Intent:     taskIntent(intentStr),
			Matched:    matched,
			Evidence:   evidence,
			Reason:     reason,
			Confidence: confidence,
			Source:     "uic",
		}
	}

	// Fallback: keyword-based classification when UIC is nil.
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return taskIntentResult{Intent: intentUnknown}
	}

	codingHits := collectIntentMatches(msg, codingKeywords)
	sshHits := collectIntentMatches(msg, sshKeywords)
	nonCodingHits := collectIntentMatches(msg, nonCodingKeywords)
	ambiguousHits := collectIntentMatches(msg, ambiguousKeywords)
	if ipv4Pattern.MatchString(msg) {
		sshHits = appendIfMissing(sshHits, "ip")
	}

	hasCoding := len(codingHits) > 0
	hasSSH := len(sshHits) > 0
	hasNonCoding := len(nonCodingHits) > 0
	hasAmbiguous := len(ambiguousHits) > 0

	switch {
	case hasNonCoding && hasCoding && !hasSSH:
		if hasOnlyWeakCodingEvidence(codingHits) {
			return taskIntentResult{Intent: intentNonCoding, Matched: nonCodingHits[0], Evidence: combineEvidence(nonCodingHits, codingHits), Source: "rules"}
		}
		return taskIntentResult{Intent: intentAmbiguous, Matched: firstMatch(nonCodingHits, codingHits), Evidence: combineEvidence(nonCodingHits, codingHits), Source: "rules"}
	case hasCoding && !hasSSH && !hasAmbiguous:
		// Weak coding evidence alone (e.g. "测试" appearing in a file name)
		// should not trigger coding classification — treat as non-coding.
		if hasOnlyWeakCodingEvidence(codingHits) {
			return taskIntentResult{Intent: intentNonCoding, Matched: codingHits[0], Evidence: codingHits, Source: "rules"}
		}
		return taskIntentResult{Intent: intentCoding, Matched: codingHits[0], Evidence: codingHits, Source: "rules"}
	case hasSSH && !hasCoding && !hasNonCoding:
		return taskIntentResult{Intent: intentSSH, Matched: sshHits[0], Evidence: sshHits, Source: "rules"}
	case hasNonCoding && !hasCoding && !hasSSH:
		return taskIntentResult{Intent: intentNonCoding, Matched: nonCodingHits[0], Evidence: nonCodingHits, Source: "rules"}
	case hasSSH && hasCoding:
		return taskIntentResult{Intent: intentAmbiguous, Matched: firstMatch(ambiguousHits, sshHits, codingHits), Evidence: combineEvidence(sshHits, codingHits, ambiguousHits), Source: "rules"}
	case hasAmbiguous:
		return taskIntentResult{Intent: intentAmbiguous, Matched: firstMatch(ambiguousHits, sshHits, codingHits, nonCodingHits), Evidence: combineEvidence(ambiguousHits, sshHits, codingHits, nonCodingHits), Source: "rules"}
	case hasSSH:
		return taskIntentResult{Intent: intentSSH, Matched: sshHits[0], Evidence: sshHits, Source: "rules"}
	case hasNonCoding:
		return taskIntentResult{Intent: intentNonCoding, Matched: nonCodingHits[0], Evidence: nonCodingHits, Source: "rules"}
	case hasCoding:
		return taskIntentResult{Intent: intentCoding, Matched: codingHits[0], Evidence: codingHits, Source: "rules"}
	default:
		return taskIntentResult{Intent: intentAmbiguous, Source: "rules"}
	}
}

func shouldRequireExecutionConfirmationForIntent(msg IMUserMessage, pending *pendingConfirmation, intent taskIntentResult) bool {
	if msg.IsBackground || pending != nil {
		return false
	}
	trimmed := strings.TrimSpace(msg.Text)
	if trimmed == "" || !looksLikeFreshTaskRequest(trimmed) {
		return false
	}
	return intent.Intent == intentCoding || intent.Intent == intentSSH || intent.Intent == intentAmbiguous
}

func (h *IMMessageHandler) classifyTaskIntentForExecution(text string, attachments []MessageAttachment, httpClient *http.Client) taskIntentResult {
	fallback := classifyTaskIntent(text)
	if h == nil || h.app == nil || httpClient == nil {
		return fallback
	}
	cfg := h.getMaclawLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fallback
	}
	llmResult, err := h.classifyTaskIntentWithLLM(cfg, text, attachments, httpClient)
	if err != nil {
		return fallback
	}
	if llmResult.Confidence < 0.6 {
		if fallback.Intent != intentAmbiguous && fallback.Intent != intentUnknown {
			return fallback
		}
		llmResult.Intent = intentAmbiguous
		if strings.TrimSpace(llmResult.Reason) == "" {
			llmResult.Reason = "模型置信度不足，保守降级为 ambiguous"
		}
		return llmResult
	}
	return llmResult
}

func (h *IMMessageHandler) classifyTaskIntentWithLLM(cfg MaclawLLMConfig, text string, attachments []MessageAttachment, httpClient *http.Client) (taskIntentResult, error) {
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
		name := strings.TrimSpace(attachment.FileName)
		if name == "" {
			continue
		}
		names = append(names, name)
		if len(names) >= 4 {
			break
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func (h *IMMessageHandler) requestIntentClassification(cfg MaclawLLMConfig, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return h.requestIntentClassificationAnthropic(cfg, messages, httpClient)
	}
	return h.requestIntentClassificationOpenAI(cfg, messages, httpClient)
}

func (h *IMMessageHandler) requestIntentClassificationOpenAI(cfg MaclawLLMConfig, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	responseFormat := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name": "task_intent_classification",
			"schema": intentClassifierJSONSchema,
		},
	}
	req, body, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:         false,
		ResponseFormat: responseFormat,
	})
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

func (h *IMMessageHandler) requestIntentClassificationAnthropic(cfg MaclawLLMConfig, messages []interface{}, httpClient *http.Client) (llmIntentClassification, error) {
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
	intent := normalizeTaskIntent(parsed.Intent)
	if intent == intentUnknown {
		return taskIntentResult{}, fmt.Errorf("unknown intent %q", parsed.Intent)
	}
	evidence := normalizeIntentEvidence(parsed.Evidence)
	matched := ""
	if len(evidence) > 0 {
		matched = evidence[0]
	}
	reason := strings.TrimSpace(parsed.Reason)
	if matched == "" && reason != "" {
		matched = reason
	}
	confidence := parsed.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return taskIntentResult{
		Intent:     intent,
		Matched:    matched,
		Evidence:   evidence,
		Reason:     reason,
		Confidence: confidence,
		Source:     "llm",
	}, nil
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
	if len(items) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		normalized = appendIfMissing(normalized, trimmed)
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
		"intent": map[string]interface{}{
			"type": "string",
			"enum": []string{"coding", "ssh", "non_coding", "ambiguous"},
		},
		"confidence": map[string]interface{}{
			"type":    "number",
			"minimum": 0,
			"maximum": 1,
		},
		"reason": map[string]interface{}{"type": "string"},
		"evidence": map[string]interface{}{
			"type":     "array",
			"maxItems": 4,
			"items": map[string]interface{}{
				"type": "string",
			},
		},
	},
	"required": []string{"intent", "confidence", "reason", "evidence"},
}

const intentClassifierSystemPrompt = `你是一个任务执行方式分类器，只负责判断当前请求应该归类到哪种执行路径。

分类目标：
- coding：明确需要修改代码、写代码、调试、修复 bug、实现功能、处理项目源码
- ssh：明确需要登录服务器、查看线上日志、远程执行命令、重启服务、操作远端主机
- non_coding：不需要编程会话或 ssh，典型如资料整理、翻译、总结、知识库录入、PPT/PDF/报告、截图理解与分析
- ambiguous：信息不足，或同时像 coding 与 ssh，无法安全决定执行路径

规则：
- 这是执行方式分类，不是主题分类。
- 如果请求是在让助手理解、分析、总结截图或附件内容，而不是要求修改代码或登录服务器，优先判为 non_coding。
- 只有在明确提到改代码、修 bug、实现功能、修改项目时，才判为 coding。
- 只有在明确提到服务器、ssh、日志、部署环境、远程命令时，才判为 ssh。
- 信息不足时保守输出 ambiguous。
- 只输出 JSON，不要输出任何额外解释。
`

func hasOnlyWeakCodingEvidence(hits []string) bool {
	if len(hits) == 0 {
		return false
	}
	weak := map[string]struct{}{
		"编程":  {},
		"代码":  {},
		"测试":  {},
		"源码":  {},
		"源代码": {},
	}
	for _, hit := range hits {
		if _, ok := weak[hit]; !ok {
			return false
		}
	}
	return true
}

func collectIntentMatches(msg string, keywords []string) []string {
	var hits []string
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			hits = appendIfMissing(hits, kw)
		}
	}
	return hits
}

func appendIfMissing(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func combineEvidence(groups ...[]string) []string {
	merged := make([]string, 0)
	for _, group := range groups {
		for _, item := range group {
			merged = appendIfMissing(merged, item)
		}
	}
	return merged
}

func firstMatch(groups ...[]string) string {
	for _, group := range groups {
		if len(group) > 0 {
			return group[0]
		}
	}
	return ""
}

func formatIntentEvidence(result taskIntentResult) string {
	if len(result.Evidence) == 0 {
		if result.Matched != "" {
			return fmt.Sprintf("%q", result.Matched)
		}
		return "未命中特征词"
	}
	if len(result.Evidence) == 1 {
		return fmt.Sprintf("%q", result.Evidence[0])
	}
	return fmt.Sprintf("%q", strings.Join(result.Evidence, `", "`))
}
