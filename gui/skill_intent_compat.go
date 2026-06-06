package main

// skill_intent_compat.go implements intent-capability compatibility checking
// for the Skill preference system.
//
// Problem: shouldPreferSkillForTask + matchPreferredLocalSkill match on shared
// topic tokens ("pdf" in both user text and skill description). This is
// fundamentally wrong — it matches on the OBJECT of the action without checking
// the ACTION itself. "统计d盘上的pdf文件" (count PDF files) shares the token
// "pdf" with "xh-md-to-pdf" (Markdown→PDF converter), but the user's intent
// (count/search/list files) is incompatible with the skill's capability
// (convert/generate/export documents).
//
// Mechanism: Extract the user's action verb and the skill's capability verbs.
// A skill is only a valid match when the user's intent is compatible with
// what the skill can do. This is data-driven — adding new verb categories
// requires only adding entries to the verb tables, not changing matching logic.

import (
	"strings"
	"unicode"
)

// intentCategory represents a coarse action category extracted from user text.
type intentCategory string

const (
	intentCatGenerate intentCategory = "generate" // 生成/创建/导出/转换/制作
	intentCatQuery    intentCategory = "query"    // 统计/搜索/查找/列出/查看/打开/读取
	intentCatModify   intentCategory = "modify"   // 修改/编辑/更新/替换
	intentCatSend     intentCategory = "send"     // 发送/分享/转发
	intentCatUnknown  intentCategory = "unknown"
)

// capabilityCategory represents what a skill can do, extracted from its description.
type capabilityCategory string

const (
	capCatGenerate capabilityCategory = "generate" // converts/generates/exports/creates/renders
	capCatQuery    capabilityCategory = "query"    // searches/finds/lists/counts/reads
	capCatModify   capabilityCategory = "modify"   // edits/modifies/updates
	capCatAnalyze  capabilityCategory = "analyze"  // analyzes/extracts/parses/recognizes
)

type skillDomainCategory string

const (
	skillDomainUnknown   skillDomainCategory = "unknown"
	skillDomainInfra     skillDomainCategory = "infra"
	skillDomainAPIDesign skillDomainCategory = "api_design"
	skillDomainWeather   skillDomainCategory = "weather"
	skillDomainDocument  skillDomainCategory = "document"
)

// intentVerbsCJK maps Chinese action verbs (2+ characters) to intent categories.
// Only multi-character verbs are included to avoid false positives from
// single characters that appear as parts of compound words with different
// meanings (e.g. "数" in "数据" means "data", not "count").
var intentVerbsCJK = map[string]intentCategory{
	// Query/inspect — 2-char verbs
	"统计": intentCatQuery, "搜索": intentCatQuery, "查找": intentCatQuery,
	"查询": intentCatQuery, "列出": intentCatQuery, "查看": intentCatQuery,
	"打开": intentCatQuery, "读取": intentCatQuery, "浏览": intentCatQuery,
	"扫描": intentCatQuery, "检索": intentCatQuery, "计数": intentCatQuery,
	"搜集": intentCatQuery, "查阅": intentCatQuery,
	// Generate/create — 2-char verbs
	"生成": intentCatGenerate, "创建": intentCatGenerate, "导出": intentCatGenerate,
	"转换": intentCatGenerate, "制作": intentCatGenerate, "渲染": intentCatGenerate,
	"编写": intentCatGenerate, "撰写": intentCatGenerate,
	// Modify — 2-char verbs
	"修改": intentCatModify, "编辑": intentCatModify, "更新": intentCatModify,
	"替换": intentCatModify,
	// Send — 2-char verbs
	"发送": intentCatSend, "分享": intentCatSend, "转发": intentCatSend,
	"发给": intentCatSend,
}

var supplementalIntentVerbsCJK = map[string]intentCategory{
	"记得": intentCatQuery,
	"知道": intentCatQuery,
	"回忆": intentCatQuery,
	"看看": intentCatQuery,
	"检查": intentCatQuery,
	"确认": intentCatQuery,
}

// intentVerbsEN maps English action verbs to intent categories.
// Matched as whole words (space-delimited) to avoid substring false positives.
var intentVerbsEN = map[string]intentCategory{
	// Query
	"count": intentCatQuery, "search": intentCatQuery, "find": intentCatQuery,
	"list": intentCatQuery, "scan": intentCatQuery, "browse": intentCatQuery,
	"read": intentCatQuery, "open": intentCatQuery, "view": intentCatQuery,
	"stat": intentCatQuery, "check": intentCatQuery, "checks": intentCatQuery,
	"checking": intentCatQuery, "locate": intentCatQuery,
	"remember": intentCatQuery, "recall": intentCatQuery, "know": intentCatQuery,
	// Generate
	"generate": intentCatGenerate, "create": intentCatGenerate, "export": intentCatGenerate,
	"convert": intentCatGenerate, "render": intentCatGenerate, "make": intentCatGenerate,
	"build": intentCatGenerate, "produce": intentCatGenerate, "write": intentCatGenerate,
	// Modify
	"edit": intentCatModify, "modify": intentCatModify, "update": intentCatModify,
	// Send
	"send": intentCatSend, "share": intentCatSend,
}

// capabilityVerbs maps description keywords to capability categories.
// Checked against the skill's Description field.
var capabilityVerbs = map[string]capabilityCategory{
	// Generate/convert capabilities
	"convert": capCatGenerate, "converts": capCatGenerate,
	"generate": capCatGenerate, "generates": capCatGenerate,
	"export": capCatGenerate, "exports": capCatGenerate,
	"render": capCatGenerate, "renders": capCatGenerate,
	"create": capCatGenerate, "creates": capCatGenerate,
	"produce": capCatGenerate, "produces": capCatGenerate,
	"transform": capCatGenerate, "build": capCatGenerate,
	"转换": capCatGenerate, "生成": capCatGenerate, "导出": capCatGenerate,
	"制作": capCatGenerate, "渲染": capCatGenerate,
	// Query/search capabilities
	"search": capCatQuery, "searches": capCatQuery,
	"find": capCatQuery, "finds": capCatQuery,
	"list": capCatQuery, "lists": capCatQuery,
	"count": capCatQuery, "counts": capCatQuery,
	"query": capCatQuery, "queries": capCatQuery,
	"搜索": capCatQuery, "查找": capCatQuery, "统计": capCatQuery,
	// Analyze capabilities
	"analyze": capCatAnalyze, "analyzes": capCatAnalyze,
	"extract": capCatAnalyze, "extracts": capCatAnalyze,
	"parse": capCatAnalyze, "parses": capCatAnalyze,
	"recognize": capCatAnalyze, "recognizes": capCatAnalyze,
	"ocr": capCatAnalyze,
	"识别":  capCatAnalyze, "解析": capCatAnalyze, "提取": capCatAnalyze,
	// Modify capabilities
	"edit": capCatModify, "edits": capCatModify,
	"modify": capCatModify, "modifies": capCatModify,
	"修改": capCatModify, "编辑": capCatModify,
}

// intentCapabilityCompat defines which intent categories are compatible with
// which capability categories. This is the compatibility matrix.
//
// Key insight: a "query" intent (count/search/list files) is NOT compatible
// with a "generate" capability (convert Markdown to PDF). The user wants to
// inspect existing files, not create new ones.
var intentCapabilityCompat = map[intentCategory]map[capabilityCategory]bool{
	intentCatGenerate: {
		capCatGenerate: true,
		capCatAnalyze:  true, // "生成报告" may need analysis skills
	},
	intentCatQuery: {
		capCatQuery:   true,
		capCatAnalyze: true, // "查找并识别" is compatible
		// capCatGenerate: false — this is the key exclusion
	},
	intentCatModify: {
		capCatModify:   true,
		capCatGenerate: true, // "修改后导出" may need generation
	},
	intentCatSend: {
		capCatGenerate: true, // "发送PDF" may need generation first
		capCatQuery:    true, // "发送搜索结果" is compatible
	},
	intentCatUnknown: {
		// Unknown intent is compatible with everything — don't block
		capCatGenerate: true,
		capCatQuery:    true,
		capCatModify:   true,
		capCatAnalyze:  true,
	},
}

// skillPreferenceCompatibleIntents lists intent categories that are compatible
// with the skill preference path. The skill preference system is designed for
// generation/conversion/send tasks — query and modify intents should not
// trigger skill search.
//
// Used by shouldPreferSkillForTask to gate entry into the skill preference
// path. This is derived from the compatibility matrix: an intent is
// "skill-preference-compatible" if it's compatible with capCatGenerate
// (the dominant skill capability).
var skillPreferenceCompatibleIntents = map[intentCategory]bool{
	intentCatGenerate: true,
	intentCatSend:     true,
	intentCatUnknown:  true, // don't block on ambiguity
}

// extractUserIntentCategory extracts the primary action intent from user text.
// Returns intentCatUnknown if no recognizable verb is found.
//
// Two-pass strategy:
//  1. CJK pass: scan for 2-char Chinese verbs at each rune position.
//     Single-char verbs are excluded to avoid false positives from compound
//     words (e.g. "数" in "数据" means "data", not "count").
//  2. English pass: split on whitespace and match whole words.
//
// Returns on first match — in Chinese, the verb typically appears before the
// object ("统计 PDF 文件"), so first-match is correct.
func extractUserIntentCategory(text string) intentCategory {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return intentCatUnknown
	}

	// Pass 1: CJK 2-char verb scan.
	runes := []rune(lower)
	for i := 0; i+2 <= len(runes); i++ {
		twoChar := string(runes[i : i+2])
		if cat, ok := intentVerbsCJK[twoChar]; ok {
			return cat
		}
		if cat, ok := supplementalIntentVerbsCJK[twoChar]; ok {
			return cat
		}
	}

	// Pass 2: English whole-word scan.
	for _, word := range strings.Fields(lower) {
		word = strings.TrimRightFunc(word, func(r rune) bool {
			return unicode.IsPunct(r)
		})
		if cat, ok := intentVerbsEN[word]; ok {
			return cat
		}
	}

	return intentCatUnknown
}

// extractSkillCapabilities extracts capability categories from a skill's description.
// Returns all matching categories (a skill may have multiple capabilities).
func extractSkillCapabilities(description string) map[capabilityCategory]bool {
	caps := make(map[capabilityCategory]bool)
	lower := strings.ToLower(strings.TrimSpace(description))
	if lower == "" {
		return caps
	}

	// Build word set for English word-boundary matching.
	words := make(map[string]bool)
	for _, w := range strings.FieldsFunc(lower, isDescriptionSeparator) {
		words[w] = true
	}

	for verb, cat := range capabilityVerbs {
		if isASCII(verb) {
			// English: word-boundary match
			if words[verb] {
				caps[cat] = true
			}
		} else {
			// CJK: substring match
			if strings.Contains(lower, verb) {
				caps[cat] = true
			}
		}
	}

	return caps
}

// isDescriptionSeparator returns true for characters that separate words in
// skill descriptions.
func isDescriptionSeparator(r rune) bool {
	return r == ' ' || r == ',' || r == '.' || r == ';' || r == ':' ||
		r == '(' || r == ')' || r == '[' || r == ']' || r == '/' ||
		r == '-' || r == '|'
}

// isIntentCompatibleWithSkill checks whether the user's intent is compatible
// with the skill's declared capabilities.
//
// Returns true (compatible) when:
// - User intent is unknown (don't block on ambiguity)
// - Skill has no extractable capabilities (don't block on missing data)
// - At least one of the skill's capabilities is in the compatibility set
//
// Returns false (incompatible) when:
// - User intent is clear AND skill capabilities are clear AND none match
func isIntentCompatibleWithSkill(userText string, skillDescription string) bool {
	userIntent := extractUserIntentCategory(userText)
	return isIntentCategoryCompatibleWithSkill(userIntent, skillDescription) &&
		isTaskDomainCompatibleWithSkill(userIntent, userText, skillDescription)
}

// isIntentCategoryCompatibleWithSkill is the inner implementation that accepts
// a pre-extracted intent category. Use this when checking multiple skills
// against the same user text to avoid redundant intent extraction.
func isIntentCategoryCompatibleWithSkill(userIntent intentCategory, skillDescription string) bool {
	if userIntent == intentCatUnknown {
		return true
	}

	skillCaps := extractSkillCapabilities(skillDescription)
	if len(skillCaps) == 0 {
		return true
	}

	compatSet, ok := intentCapabilityCompat[userIntent]
	if !ok {
		return true
	}

	for cap := range skillCaps {
		if compatSet[cap] {
			return true
		}
	}

	return false
}

func isTaskCompatibleWithSkillCandidate(userIntent intentCategory, userText string, skillText string) bool {
	return isIntentCategoryCompatibleWithSkill(userIntent, skillText) &&
		isTaskDomainCompatibleWithSkill(userIntent, userText, skillText)
}

func isTaskDomainCompatibleWithSkill(userIntent intentCategory, userText string, skillText string) bool {
	if userIntent != intentCatQuery {
		return true
	}
	taskDomain := extractTaskDomain(userText)
	if taskDomain == skillDomainUnknown {
		return true
	}
	skillDomain := extractSkillDomain(skillText)
	if skillDomain == skillDomainUnknown {
		return true
	}
	return taskDomain == skillDomain
}

func extractTaskDomain(text string) skillDomainCategory {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return skillDomainUnknown
	}
	if containsAny(lower, "server", "status", "health", "ssh", "host", "uptime", "api2", "api1",
		"服务器", "服务", "状态", "健康", "主机", "连接") {
		return skillDomainInfra
	}
	if containsAny(lower, "weather", "forecast", "temperature", "天气", "气温", "预报") {
		return skillDomainWeather
	}
	if containsAny(lower, "pdf", "ppt", "pptx", "document", "docx", "file", "文档", "文件") {
		return skillDomainDocument
	}
	return skillDomainUnknown
}

func extractSkillDomain(text string) skillDomainCategory {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return skillDomainUnknown
	}
	if containsAny(lower, "api design", "design review", "design reviewer", "api reviewer", "openapi review",
		"接口设计", "api设计", "设计评审") {
		return skillDomainAPIDesign
	}
	if containsAny(lower, "server", "status", "health", "ssh", "host", "uptime", "monitor",
		"服务器", "服务状态", "健康检查", "主机", "运维", "监控") {
		return skillDomainInfra
	}
	if containsAny(lower, "weather", "forecast", "temperature", "天气", "气温", "预报") {
		return skillDomainWeather
	}
	if containsAny(lower, "pdf", "ppt", "pptx", "document", "docx", "file", "文档", "文件") {
		return skillDomainDocument
	}
	return skillDomainUnknown
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// isIntentSkillPreferenceCompatible checks whether the user's intent should
// enter the skill preference path at all. Query and modify intents should
// not trigger skill search — the user wants to operate on existing files,
// not use a skill to generate/convert something.
func isIntentSkillPreferenceCompatible(text string) bool {
	intent := extractUserIntentCategory(text)
	return skillPreferenceCompatibleIntents[intent]
}

// isASCII returns true if the string contains only ASCII characters.
func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
