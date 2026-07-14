package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	workflowPhaseRequirements = "requirements"
	workflowPhaseDesign       = "design"
	workflowPhaseTasks        = "tasks"
)

type workflowPhaseKind string

const workflowPhaseUnknown workflowPhaseKind = ""

// normalizeWorkflowPhaseKind classifies a raw phase ID into one of the three
// canonical coding-workflow kinds, or workflowPhaseUnknown for anything else.
//
// It delegates to workflow.CanonicalPhaseID so the phase-ID alias table lives in
// exactly one place (corelib/workflow): adding or changing an alias there flows
// here automatically and the two can never drift. A canonical ID that is not one
// of the three known coding kinds (i.e. CanonicalPhaseID passed it through
// unchanged) maps to workflowPhaseUnknown.
func normalizeWorkflowPhaseKind(value string) workflowPhaseKind {
	switch canonical := workflow.CanonicalPhaseID(value); canonical {
	case workflowPhaseRequirements, workflowPhaseDesign, workflowPhaseTasks:
		return workflowPhaseKind(canonical)
	default:
		return workflowPhaseUnknown
	}
}

func (k workflowPhaseKind) String() string {
	return string(k)
}

func normalizeWorkflowPhaseID(value string) string {
	return normalizeWorkflowPhaseKind(value).String()
}

func workflowPhaseKindFromMetadata(values ...string) workflowPhaseKind {
	for _, value := range values {
		if phase := normalizeWorkflowPhaseKind(value); phase != workflowPhaseUnknown {
			return phase
		}
	}
	return workflowPhaseUnknown
}

// inferFileDeliveryMessage builds a default proactive-IM caption (zh fallback).
// Prefer resolveIMProactiveCaption(lang, …) at send time for GUI language.
func inferFileDeliveryMessage(fileName string) string {
	return localizeIMProactiveCaption("zh", fileName, "")
}

// isLegacyBotFileInstruction detects bot-to-bot placeholders that must never
// be shown to WeChat/Feishu end users as captions.
func isLegacyBotFileInstruction(msg string) bool {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return false
	}
	// Historical English: "Please send <name> to the user."
	if strings.HasPrefix(msg, "Please send ") && strings.HasSuffix(msg, " to the user.") {
		return true
	}
	// Case-insensitive / punctuation variants.
	lower := strings.ToLower(msg)
	if strings.HasPrefix(lower, "please send ") && strings.Contains(lower, " to the user") {
		return true
	}
	return false
}

// isAutoProactiveCaption reports whether msg looks like a short auto-generated
// delivery caption (so send-time can re-localize for the current GUI language).
func isAutoProactiveCaption(msg string) bool {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return false
	}
	// Named captions we generate (fullwidth or ASCII colon).
	if strings.HasPrefix(msg, "请查收图片：") || strings.HasPrefix(msg, "请查收文件：") ||
		strings.HasPrefix(msg, "请查收图片:") || strings.HasPrefix(msg, "请查收文件:") {
		return true
	}
	if strings.HasPrefix(msg, "Please find the image:") || strings.HasPrefix(msg, "Please find the file:") ||
		strings.HasPrefix(msg, "Please find the image：") || strings.HasPrefix(msg, "Please find the file：") {
		return true
	}
	// Bare captions without filename.
	switch msg {
	case "请查收图片", "请查收文件", "Please find the image", "Please find the file":
		return true
	default:
		return false
	}
}

// localizeIMProactiveCaption returns a short user-facing caption for IM file
// delivery, matching the GUI interface language (zh/en).
//
// Images use the bare form ("请查收图片") — the media bubble already carries
// the content, and system screenshot names are noisy. Non-image files keep the
// filename so users can tell PDFs/docs apart.
func localizeIMProactiveCaption(lang, fileName, mimeType string) string {
	base := strings.TrimSpace(filepath.Base(fileName))
	invalidName := base == "" || base == "." || base == string(filepath.Separator)
	image := isProactiveImageMIMEOrName(mimeType, base)
	if image {
		return i18n.T(i18n.MsgIMProactiveImageCaptionBare, lang)
	}
	if invalidName {
		return i18n.T(i18n.MsgIMProactiveFileCaptionBare, lang)
	}
	return i18n.Tf(i18n.MsgIMProactiveFileCaption, lang, base)
}

func isProactiveImageMIMEOrName(mimeType, fileName string) bool {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mt, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic", ".heif":
		return true
	default:
		return false
	}
}

// resolveIMProactiveCaption picks the caption actually sent with a file on IM.
// Explicit workflow messages (e.g. 需求文档已生成) are kept; empty, legacy bot
// instructions, or previously auto-generated captions are (re)localized.
func resolveIMProactiveCaption(lang, message, fileName, mimeType string) string {
	msg := strings.TrimSpace(message)
	if msg != "" && !isLegacyBotFileInstruction(msg) && !isAutoProactiveCaption(msg) {
		return msg
	}
	return localizeIMProactiveCaption(lang, fileName, mimeType)
}

type searchAndInstallSkillResult struct {
	Text    string
	Success bool
}

func (h *IMMessageHandler) executeSkillSearchInstall(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult {
	if h != nil && h.skillSearchInstallHandler != nil {
		return h.skillSearchInstallHandler(args, onProgress)
	}
	return h.toolSearchAndInstallSkillResult(args, onProgress)
}

func (h *IMMessageHandler) toolSearchAndInstallSkillResult(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return searchAndInstallSkillResult{Text: "Missing query parameter.", Success: false}
	}
	if !shouldExecuteSearchAndInstallSkillQuery(query) {
		return searchAndInstallSkillResult{Text: fmt.Sprintf("Skill search skipped: query %q is an information lookup, not a missing capability request.", strings.TrimSpace(query)), Success: false}
	}
	if h == nil || h.app == nil {
		return searchAndInstallSkillResult{Text: "Skill search failed: app is not initialized.", Success: false}
	}
	sendStatus := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}
	policyOwnerID, explicitRuntime := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrentState(args)
	if policyOwnerID == "" && explicitRuntime {
		return searchAndInstallSkillResult{Text: "Skill search failed: runtime owner is missing; isolated runtime will not fall back to desktop owner", Success: false}
	}
	platform := consumeRuntimePlatformFromToolArgs(args)
	if platform == "" {
		platform = h.runtimePlatformForOwnerOrCurrent(policyOwnerID, explicitRuntime)
	}
	ctx := context.Background()
	searcher := NewSkillSearcher(NewSkillMarketClient(h.app))
	compatibilityTask := skillSearchCompatibilityTaskText(query)
	best, err := searcher.SearchAndInstallForTask(ctx, query, compatibilityTask)
	if err != nil {
		return searchAndInstallSkillResult{Text: fmt.Sprintf("Skill search failed: %v", err), Success: false}
	}
	if best == nil {
		return searchAndInstallSkillResult{Text: fmt.Sprintf("No matching skill found for %q.", query), Success: false}
	}
	installResult := h.installAndExecuteSkill(ctx, best, query, platform, policyOwnerID, policyOwnerID, sendStatus)
	return searchAndInstallSkillResult{Text: installResult.Text, Success: installResult.Success}
}

func skillSearchCompatibilityTaskText(query string) string {
	query = strings.TrimSpace(query)
	if !isExplicitSkillCapabilitySearchRequest(query) {
		return query
	}
	stripped := strings.TrimSpace(stripSkillSearchWrapper(query))
	if stripped == "" {
		return query
	}
	return stripped
}

func stripSkillSearchWrapper(query string) string {
	text := strings.ToLower(strings.TrimSpace(query))
	replacer := strings.NewReplacer(
		"search and install", " ",
		"search for", " ",
		"look for", " ",
		"find", " ",
		"install", " ",
		"i need", " ",
		"need", " ",
		"missing capability", " ",
		"capability", " ",
		"a skill for", " ",
		"skill for", " ",
		"a tool for", " ",
		"tool for", " ",
		"skill", " ",
		"tool", " ",
		"技能", " ",
		"能力", " ",
		"工具", " ",
		"找一个", " ",
		"找个", " ",
		"搜索", " ",
		"安装", " ",
		"需要", " ",
	)
	text = replacer.Replace(text)
	text = strings.Trim(text, " \t\r\n.。?？!！,，:：;；")
	text = strings.Join(dropSkillSearchWrapperStopWords(strings.Fields(text)), " ")
	return text
}

func dropSkillSearchWrapperStopWords(fields []string) []string {
	out := fields[:0]
	for _, field := range fields {
		switch field {
		case "a", "an", "the", "for":
			continue
		default:
			out = append(out, field)
		}
	}
	return out
}

func shouldExecuteSearchAndInstallSkillQuery(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	if isExplicitSkillCapabilitySearchRequest(query) {
		return true
	}
	if !isIntentSkillPreferenceCompatible(query) {
		return false
	}
	if extractUserIntentCategory(query) == intentCatUnknown && extractTaskDomain(query) == skillDomainInfra {
		return false
	}
	return true
}

func isExplicitSkillCapabilitySearchRequest(query string) bool {
	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return false
	}
	if containsAny(lower, "missing capability", "search and install", "缺少能力") {
		return true
	}
	if containsASCIIWord(lower, "skill") || containsASCIIWord(lower, "capability") ||
		containsAny(lower, "技能", "能力", "安装 skill") {
		return true
	}
	if containsASCIIWord(lower, "tool") {
		return containsAny(lower, "find", "search", "install", "need", "missing", "look for")
	}
	if strings.Contains(lower, "工具") {
		return containsAny(lower, "找工具", "找个工具", "找一个工具", "搜索工具", "安装工具", "需要工具", "缺少工具")
	}
	return false
}

func containsASCIIWord(text, word string) bool {
	if word == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return !isASCIIAlphaNum(r)
	}) {
		if part == word {
			return true
		}
	}
	return false
}

func isASCIIAlphaNum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
