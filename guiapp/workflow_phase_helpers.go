package guiapp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/intent"
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
	return h.executeSkillSearchInstallAdmitted(args, onProgress, false)
}

// executeSkillSearchInstallAdmitted is the host path. hostAdmitted is not an
// argument the model can set: a complex task with no local skill must still
// be searched when its wording would otherwise look like a lookup.
func (h *IMMessageHandler) executeSkillSearchInstallAdmitted(args map[string]interface{}, onProgress tool.ProgressCallback, hostAdmitted bool) searchAndInstallSkillResult {
	if h != nil && h.skillSearchInstallHandler != nil {
		return h.skillSearchInstallHandler(args, onProgress)
	}
	return h.toolSearchAndInstallSkillResultAdmitted(args, onProgress, hostAdmitted)
}

func (h *IMMessageHandler) toolSearchAndInstallSkillResult(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult {
	return h.toolSearchAndInstallSkillResultAdmitted(args, onProgress, false)
}

func (h *IMMessageHandler) toolSearchAndInstallSkillResultAdmitted(args map[string]interface{}, onProgress tool.ProgressCallback, hostAdmitted bool) searchAndInstallSkillResult {
	if args == nil {
		args = map[string]interface{}{}
	}
	// A model-supplied flag is not host admission.
	delete(args, "host_admitted")
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return searchAndInstallSkillResult{Text: "Missing query parameter.", Success: false}
	}
	if !skillSearchQueryAllowed(query, hostAdmitted) {
		return searchAndInstallSkillResult{Text: skillSearchSkippedText(query), Success: false}
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

// skillSearchQueryAllowed is the market-search gate. hostAdmitted is only
// for the task the host already selected. The caller must put that task in
// query. A model-invented lookup is not host admission and stays skipped.
// Host admission still does not install a skill for a bare continuation or
// an information lookup: those are not missing capabilities.
func skillSearchQueryAllowed(query string, hostAdmitted bool) bool {
	query = strings.TrimSpace(query)
	if query == "" || skillSearchQueryIsBareContinuation(query) {
		return false
	}
	if hostAdmitted {
		return !informationLookupQuery(query)
	}
	return shouldExecuteSearchAndInstallSkillQuery(query)
}

func skillSearchSkippedText(query string) string {
	query = strings.TrimSpace(query)
	switch {
	case skillSearchQueryIsBareContinuation(query):
		return fmt.Sprintf("Skill search skipped: query %q continues the current task and is not a missing capability request.", query)
	case informationLookupQuery(query):
		return fmt.Sprintf("Skill search skipped: query %q is an information lookup, not a missing capability request.", query)
	default:
		return fmt.Sprintf("Skill search skipped: query %q is not a missing capability request.", query)
	}
}

func shouldExecuteSearchAndInstallSkillQuery(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	if isExplicitSkillCapabilitySearchRequest(query) {
		return true
	}
	// "有哪些技能" has no query verb, so the preference gate would allow it.
	// An inventory question is still a lookup.
	if informationLookupQuery(query) {
		return false
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
	// "使用这个技能查询服务器" is a query, but the user named a skill to run.
	// Without this, the host search is skipped after 技能 stops being a bare match.
	if intent.ExplicitSkillInvocation(query) {
		return true
	}
	if containsAny(lower, "missing capability", "search and install", "缺少能力") {
		return true
	}
	if containsASCIIWord(lower, "skill") || containsASCIIWord(lower, "capability") ||
		containsAny(lower, "安装 skill") || mentionsSkillInstallRequest(lower) {
		return true
	}
	// 能力 by itself is ordinary prose ("处理能力"). Only a request to
	// find or install one is a market search.
	if strings.Contains(lower, "能力") && containsAny(lower,
		"找能力", "找个能力", "找一个能力", "需要能力", "安装能力") {
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

// mentionsSkillInstallRequest is a request to find or install a skill.
// The noun 技能 alone is not one: "查询已安装的技能" and "有哪些技能" ask
// what is already here. The verb and the noun need not be adjacent
// ("找一个能改 tex 的技能").
func mentionsSkillInstallRequest(lower string) bool {
	if !strings.Contains(lower, "技能") {
		return false
	}
	probe := stripInstalledSkillInventoryWording(lower)
	if !strings.Contains(probe, "技能") {
		return false
	}
	if strings.Contains(probe, "缺少") {
		return true
	}
	// 查找 is a lookup. 寻找 and 找一个 stay requests.
	if strings.Contains(strings.ReplaceAll(probe, "查找", ""), "找") {
		return true
	}
	if strings.Contains(probe, "需要") || strings.Contains(probe, "搜索") {
		return true
	}
	// 已安装 and 安装过 describe the inventory. 安装了 stays: "安装了一个技能"
	// is a request, and stripping 安装了 would delete that 安装.
	install := strings.NewReplacer("已安装", "", "安装过", "").Replace(probe)
	return strings.Contains(install, "安装")
}

// stripInstalledSkillInventoryWording removes phrases that name skills the
// user already has, so a leftover 技能 can still be a find-or-install request
// ("安装一个和已安装的技能类似的 pdf 技能").
func stripInstalledSkillInventoryWording(lower string) string {
	return strings.NewReplacer(
		"已安装的技能", " ",
		"已安装技能", " ",
		"哪些技能", " ",
		"什么技能", " ",
		"技能列表", " ",
		"本地技能", " ",
		"现有技能", " ",
		"当前技能", " ",
	).Replace(lower)
}

// installedSkillInventoryQuestion asks which skills are already here.
// It is not a request to fetch one from the market.
func installedSkillInventoryQuestion(lower string) bool {
	if !strings.Contains(lower, "技能") {
		return false
	}
	return containsAny(lower, "已安装", "哪些技能", "什么技能", "技能列表", "本地技能", "现有技能", "当前技能", "有没有", "安装过")
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
