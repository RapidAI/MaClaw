package workflow

import (
	"strings"
	"unicode/utf8"
)

// WorkflowChecker is the minimal interface QuickFilter needs from the engine.
// Defined here to avoid circular dependency since engine.go doesn't exist yet.
type WorkflowChecker interface {
	HasActiveWorkflow(userID string) bool
	HasActiveUnderstanding(userID string) bool
}

// QuickFilter classifies incoming user messages using pure in-memory rules.
// No I/O operations — must complete in <5ms.
type QuickFilter struct {
	engine WorkflowChecker
}

// NewQuickFilter creates a QuickFilter with the given engine reference.
// engine may be nil (classification still works for pattern-based rules).
func NewQuickFilter(engine WorkflowChecker) *QuickFilter {
	return &QuickFilter{engine: engine}
}

// Classify determines the FilterResult for a user message.
// Priority (highest to lowest):
//  1. active_workflow   — user has an active workflow
//  2. active_understanding — user has an active understanding session
//  3. small_talk        — short message with greeting/time/thanks words
//  4. simple_directive  — matches translate/format/summarize patterns
//  5. needs_understanding — complex task features (verb + target + constraints)
//  6. simple_directive  — default (conservative, don't over-intercept)
func (f *QuickFilter) Classify(userID, text string) FilterResult {
	// 1. Active session checks (highest priority)
	if f.engine != nil {
		if f.engine.HasActiveWorkflow(userID) {
			return FilterActiveWorkflow
		}
		if f.engine.HasActiveUnderstanding(userID) {
			return FilterActiveUnderstanding
		}
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return FilterSimpleDirective
	}

	runeCount := utf8.RuneCountInString(trimmed)

	// 2. Small talk detection — short message + greeting/time/thanks words
	if runeCount <= smallTalkMaxRunes && isSmallTalk(trimmed) {
		return FilterSmallTalk
	}

	// 3. Simple directive detection
	if isSimpleDirective(trimmed) {
		return FilterSimpleDirective
	}

	// 4. Complex task detection
	if isComplexTask(trimmed) {
		return FilterNeedsUnderstanding
	}

	// 5. Default — conservative, don't over-intercept
	return FilterSimpleDirective
}

// ---------------------------------------------------------------------------
// Small talk detection
// ---------------------------------------------------------------------------

const smallTalkMaxRunes = 15

// smallTalkWords are common Chinese greetings, time queries, thanks, farewells.
var smallTalkWords = []string{
	// Greetings
	"你好", "您好", "嗨", "嘿", "哈喽", "hello", "hi", "hey",
	"早上好", "上午好", "中午好", "下午好", "晚上好", "晚安",
	"早安", "早", "午安",
	// Thanks
	"谢谢", "感谢", "多谢", "谢了", "thanks", "thank you", "thx",
	// Time / weather
	"几点了", "几点", "什么时间", "现在几点", "今天天气", "天气怎么样", "天气如何",
	// Farewells
	"再见", "拜拜", "bye", "拜",
	// Affirmations / fillers
	"好的", "嗯", "哦", "ok", "okay", "好", "行",
	"在吗", "在不在",
}

func isSmallTalk(text string) bool {
	lower := strings.ToLower(text)
	for _, w := range smallTalkWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Simple directive detection
// ---------------------------------------------------------------------------

// simpleDirectivePrefixes are Chinese phrases that indicate a straightforward
// single-step instruction (translate, format, summarize, etc.).
var simpleDirectivePrefixes = []string{
	"翻译", "帮我翻译", "请翻译",
	"格式化", "帮我格式化",
	"总结", "帮我总结", "请总结", "概括",
	"解释一下", "帮我解释", "请解释",
	"帮我看看", "帮我查", "帮我找",
	"转换", "帮我转换",
	"计算", "帮我算",
	"搜索", "帮我搜",
	"查一下", "查询",
	"纠错", "帮我改",
	"润色", "帮我润色",
	"简化", "精简",
	"列出", "列举",
	"对比", "比较",
	"生成", "帮我生成",
	"写一段", "写个",
}


func isSimpleDirective(text string) bool {
	for _, prefix := range simpleDirectivePrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Complex task detection
// ---------------------------------------------------------------------------

// A message is considered a complex task when it contains:
//   - An action verb (做/开发/设计/搭建/实现/创建/构建/编写/重构/优化/部署/...)
//   - A target object (系统/平台/应用/网站/服务/模块/功能/工具/项目/...)
//   - A constraint or requirement indicator (需要/要求/必须/支持/包含/能够/至少/...)
//
// All three categories must be present for the message to qualify.

var actionVerbs = []string{
	"做", "开发", "设计", "搭建", "实现", "创建", "构建",
	"编写", "重构", "优化", "部署", "迁移", "升级",
	"打造", "建设", "制定", "规划", "策划",
	"写一个", "做一个", "搞一个", "弄一个", "建一个",
	"帮我做", "帮我开发", "帮我设计", "帮我搭建", "帮我实现", "帮我创建",
}

var targetObjects = []string{
	"系统", "平台", "应用", "网站", "服务", "模块", "功能",
	"工具", "项目", "程序", "软件", "接口", "API",
	"页面", "组件", "插件", "脚本", "数据库",
	"小程序", "APP", "app", "后端", "前端", "微服务",
	"方案", "计划", "文档", "报告", "产品",
	"CRM", "ERP", "OA", "crm", "erp",
}

var constraintIndicators = []string{
	"需要", "要求", "必须", "支持", "包含", "能够",
	"至少", "不少于", "不超过", "兼容", "满足",
	"要有", "得有", "还要", "同时", "并且", "而且",
	"以及", "另外", "此外", "还需", "还得",
	"用户可以", "管理员可以", "可以通过",
	"基于", "采用", "使用",
	"高可用", "高并发", "可扩展", "安全",
	"多租户", "权限", "认证", "授权",
}

func isComplexTask(text string) bool {
	hasVerb := containsAny(text, actionVerbs)
	hasTarget := containsAny(text, targetObjects)
	hasConstraint := containsAny(text, constraintIndicators)
	return hasVerb && hasTarget && hasConstraint
}

// containsAny returns true if text contains any of the given substrings.
func containsAny(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}
