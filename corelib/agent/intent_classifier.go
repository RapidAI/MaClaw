package agent

// intent_classifier.go — standalone intent classification logic extracted
// from gui/im_intent_classifier.go as part of the agent-unification plan.
//
// Types, constants, keyword lists, and pure functions live here.
// Methods on *IMMessageHandler (which depend on LLM config, HTTP client,
// etc.) remain in gui/.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// TaskIntent represents the classified intent of a user message.
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

// ---------------------------------------------------------------------------
// Package-level UIC instance
// ---------------------------------------------------------------------------

// UnifiedClassifier is the package-level UIC instance. When non-nil,
// ClassifyTaskIntent delegates to it instead of using keyword rules.
// Set via SetUnifiedClassifier during app initialization.
var UnifiedClassifier *intent.UnifiedIntentClassifier

// SetUnifiedClassifier sets the package-level UIC used by ClassifyTaskIntent.
func SetUnifiedClassifier(uic *intent.UnifiedIntentClassifier) {
	UnifiedClassifier = uic
}

// ---------------------------------------------------------------------------
// Keyword lists (exported)
// ---------------------------------------------------------------------------

var SSHKeywords = []string{
	"ssh", "服务器", "服务端", "主机", "远程机器", "远程主机", "云服务器", "线上机器",
	"登录服务器", "连上服务器", "连接服务器", "远程登录", "看日志", "查看日志", "日志", "tail -f",
	"journalctl", "systemctl", "service ", "nginx", "docker", "docker compose", "k8s", "kubectl",
	"pm2", "supervisor", "重启服务", "重启 nginx", "重启进程", "上传到服务器", "下载服务器文件",
	"sftp", "scp", "rsync", "端口", "进程", "服务器文件", "服务器上", "远程执行",
	"host", "user", "label", "initial_command",
}

var AmbiguousKeywords = []string{
	"部署", "deploy", "上线", "线上问题", "线上故障", "服务挂了", "服务异常", "环境问题",
	"处理一下线上问题", "看看服务", "看下服务", "排查一下", "处理一下这个项目",
}

var IPv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// IntentClassifierJSONSchema is the JSON schema for the LLM intent classifier.
var IntentClassifierJSONSchema = map[string]interface{}{
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

// IntentClassifierSystemPrompt is the system prompt for the LLM intent classifier.
const IntentClassifierSystemPrompt = `你是一个任务执行方式分类器，只负责判断当前请求应该归类到哪种执行路径。

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

// ---------------------------------------------------------------------------
// Standalone functions (exported)
// ---------------------------------------------------------------------------

// ClassifyTaskIntent classifies a user message into a task intent using
// the UIC (if available) or keyword-based rules as fallback.
//
// NOTE: This function references CodingKeywords and NonCodingKeywords which
// are defined in gui/im_tools_session.go. Callers must ensure those package-
// level variables are accessible. For the corelib version, the function
// accepts them via the package-level variables imported by gui/ aliases.
//
// The gui/ package sets CodingKeywords and NonCodingKeywords at init time.
func ClassifyTaskIntent(text string, codingKeywords, nonCodingKeywords []string) TaskIntentResult {
	// Delegate to UIC when available.
	if uic := UnifiedClassifier; uic != nil {
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

	// Fallback: keyword-based classification when UIC is nil.
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return TaskIntentResult{Intent: IntentUnknown}
	}

	codingHits := CollectIntentMatches(msg, codingKeywords)
	sshHits := CollectIntentMatches(msg, SSHKeywords)
	nonCodingHits := CollectIntentMatches(msg, nonCodingKeywords)
	ambiguousHits := CollectIntentMatches(msg, AmbiguousKeywords)
	if IPv4Pattern.MatchString(msg) {
		sshHits = AppendIfMissing(sshHits, "ip")
	}

	hasCoding := len(codingHits) > 0
	hasSSH := len(sshHits) > 0
	hasNonCoding := len(nonCodingHits) > 0
	hasAmbiguous := len(ambiguousHits) > 0

	switch {
	case hasNonCoding && hasCoding && !hasSSH:
		if HasOnlyWeakCodingEvidence(codingHits) {
			return TaskIntentResult{Intent: IntentNonCoding, Matched: nonCodingHits[0], Evidence: CombineEvidence(nonCodingHits, codingHits), Source: "rules"}
		}
		return TaskIntentResult{Intent: IntentAmbiguous, Matched: FirstMatch(nonCodingHits, codingHits), Evidence: CombineEvidence(nonCodingHits, codingHits), Source: "rules"}
	case hasCoding && !hasSSH && !hasAmbiguous:
		if HasOnlyWeakCodingEvidence(codingHits) {
			return TaskIntentResult{Intent: IntentNonCoding, Matched: codingHits[0], Evidence: codingHits, Source: "rules"}
		}
		return TaskIntentResult{Intent: IntentCoding, Matched: codingHits[0], Evidence: codingHits, Source: "rules"}
	case hasSSH && !hasCoding && !hasNonCoding:
		return TaskIntentResult{Intent: IntentSSH, Matched: sshHits[0], Evidence: sshHits, Source: "rules"}
	case hasNonCoding && !hasCoding && !hasSSH:
		return TaskIntentResult{Intent: IntentNonCoding, Matched: nonCodingHits[0], Evidence: nonCodingHits, Source: "rules"}
	case hasSSH && hasCoding:
		return TaskIntentResult{Intent: IntentAmbiguous, Matched: FirstMatch(ambiguousHits, sshHits, codingHits), Evidence: CombineEvidence(sshHits, codingHits, ambiguousHits), Source: "rules"}
	case hasAmbiguous:
		return TaskIntentResult{Intent: IntentAmbiguous, Matched: FirstMatch(ambiguousHits, sshHits, codingHits, nonCodingHits), Evidence: CombineEvidence(ambiguousHits, sshHits, codingHits, nonCodingHits), Source: "rules"}
	case hasSSH:
		return TaskIntentResult{Intent: IntentSSH, Matched: sshHits[0], Evidence: sshHits, Source: "rules"}
	case hasNonCoding:
		return TaskIntentResult{Intent: IntentNonCoding, Matched: nonCodingHits[0], Evidence: nonCodingHits, Source: "rules"}
	case hasCoding:
		return TaskIntentResult{Intent: IntentCoding, Matched: codingHits[0], Evidence: codingHits, Source: "rules"}
	default:
		return TaskIntentResult{Intent: IntentAmbiguous, Source: "rules"}
	}
}

// ShouldRequireExecutionConfirmationForIntent determines whether a message
// requires execution confirmation based on its classified intent.
func ShouldRequireExecutionConfirmationForIntent(msg UserMessage, pending *PendingConfirmation, intentResult TaskIntentResult, looksLikeFreshTaskRequest func(string) bool) bool {
	if msg.IsBackground || pending != nil {
		return false
	}
	trimmed := strings.TrimSpace(msg.Text)
	if trimmed == "" || !looksLikeFreshTaskRequest(trimmed) {
		return false
	}
	return intentResult.Intent == IntentCoding || intentResult.Intent == IntentSSH || intentResult.Intent == IntentAmbiguous
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
	return TaskIntentResult{
		Intent:     ti,
		Matched:    matched,
		Evidence:   evidence,
		Reason:     reason,
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
	if len(items) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		normalized = AppendIfMissing(normalized, trimmed)
		if len(normalized) >= 4 {
			break
		}
	}
	return normalized
}

// HasOnlyWeakCodingEvidence returns true if all hits are weak coding signals.
func HasOnlyWeakCodingEvidence(hits []string) bool {
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

// CollectIntentMatches returns all keywords found in msg.
func CollectIntentMatches(msg string, keywords []string) []string {
	var hits []string
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			hits = AppendIfMissing(hits, kw)
		}
	}
	return hits
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

// CombineEvidence merges multiple evidence groups, deduplicating.
func CombineEvidence(groups ...[]string) []string {
	merged := make([]string, 0)
	for _, group := range groups {
		for _, item := range group {
			merged = AppendIfMissing(merged, item)
		}
	}
	return merged
}

// FirstMatch returns the first non-empty element from the given groups.
func FirstMatch(groups ...[]string) string {
	for _, group := range groups {
		if len(group) > 0 {
			return group[0]
		}
	}
	return ""
}

// FormatIntentEvidence formats intent evidence for display.
func FormatIntentEvidence(result TaskIntentResult) string {
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
