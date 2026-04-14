package tool

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

const (
	// MaxToolBudget is the maximum number of tools to send to the LLM.
	MaxToolBudget = 28

	// MaxDynamicRouted caps how many MCP/non-code dynamic tools can be included.
	MaxDynamicRouted = 18
)

var logDetailEnabled atomic.Bool

func init() {
	logDetailEnabled.Store(false)
	// Ensure every core tool is also recognized as builtin.
	for name := range CoreToolNames {
		BuiltinToolNames[name] = true
	}
}

// SetLogDetailEnabled updates the detailed routing log gate.
func SetLogDetailEnabled(enabled bool) {
	logDetailEnabled.Store(enabled)
}

// CoreToolNames are always included regardless of the user message.
var CoreToolNames = map[string]bool{
	"list_sessions": true, "create_session": true,
	"send_and_observe": true, "get_session_output": true, "get_session_events": true,
	"control_session": true,
	"bash":            true, "read_file": true, "write_file": true, "edit_file": true, "list_directory": true,
	"call_mcp_tool": true, "list_skills": true, "run_skill": true,
	"screenshot": true,
	"memory":     true,
	"web_fetch":  true,
	"set_nickname": true,
	"discover_tool": true,
	"task":          true,
}

type conditionalKeepRule struct {
	keepTools []string
	matches   func(string) bool
}

var sshIntentIPv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

var sshIntentKeywords = []string{
	"ssh", "服务器", "服务端", "主机", "远程机器", "远程主机", "云服务器", "线上机器",
	"登录服务器", "连上服务器", "连接服务器", "远程登录", "看日志", "查看日志", "日志", "tail -f",
	"journalctl", "systemctl", "service ", "nginx", "docker", "docker compose", "k8s", "kubectl",
	"pm2", "supervisor", "重启服务", "重启 nginx", "重启进程", "上传到服务器", "下载服务器文件",
	"sftp", "scp", "rsync", "端口", "进程", "服务器文件", "服务器上", "远程执行",
	"host", "user", "label", "initial_command",
}

// containsAnyKeyword returns true if msg contains any of the given keywords (case-insensitive).
func containsAnyKeyword(msg string, keywords []string) bool {
	lower := strings.ToLower(msg)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

var searchIntentKeywords = []string{
	"搜索", "search", "查找", "网页", "web", "google", "papers", "paper", "huggingface",
}

var documentDeliveryKeywords = []string{
	"pdf", "报告", "综述", "附件", "发送文件", "文件发我", "发给我", "导出",
}

var codingWorkflowDocKeywords = []string{
	"需求文档", "设计文档", "任务文档", "任务拆分", "任务计划", "技术设计",
	"需求分析", "架构设计", "模块设计", "接口设计",
	"生成需求", "生成设计", "生成任务",
	"开发游戏", "开发应用", "开发工具", "开发系统", "开发程序", "开发项目",
	"写代码", "改代码", "编程", "实现功能", "新增功能", "添加功能",
	"修 bug", "修bug", "修复bug", "重构代码",
}

var browserIntentKeywords = []string{
	"浏览器", "browser", "chrome", "chromium", "playwright",
	"录制", "回放", "replay", "record",
	"browser_", // tool name prefix in follow-up messages
}
var browserPageKeywords = []string{"页面", "网页", "网站", "url", "page", "site"}
var browserActionKeywords = []string{"访问", "导航", "点击", "观察", "打开", "截图", "输入", "填写"}

var conditionalKeepRules = []conditionalKeepRule{
	{
		keepTools: []string{"ssh"},
		matches: func(msg string) bool {
			return containsAnyKeyword(msg, sshIntentKeywords) || sshIntentIPv4Pattern.MatchString(msg)
		},
	},
	{
		keepTools: []string{"web_search"},
		matches: func(msg string) bool {
			return containsAnyKeyword(msg, searchIntentKeywords)
		},
	},
	{
		keepTools: []string{"send_file", "open", "craft_tool"},
		matches: func(msg string) bool {
			return containsAnyKeyword(msg, documentDeliveryKeywords)
		},
	},
	{
		keepTools: []string{
			// Browser agent session tools.
			"browser_session_start", "browser_session_stop", "browser_observe",
			"browser_navigate", "browser_click", "browser_type",
			"browser_wait", "browser_back", "browser_refresh", "browser_extract",
			"browser_connect", "browser_screenshot", "browser_get_text",
			"browser_get_html", "browser_eval", "browser_scroll",
			"browser_select", "browser_list_pages", "browser_switch_page",
			"browser_close", "browser_click_at", "browser_set_files",
			"browser_info", "browser_ocr",
			// Browser task/record/replay tools.
			"browser_task_run", "browser_task_replay", "browser_task_verify", "browser_task_status",
			"browser_record_start", "browser_record_stop", "browser_list_flows",
			// GUI automation recording tools.
			"gui_record_start", "gui_record_stop",
		},
		matches: func(msg string) bool {
			return containsAnyKeyword(msg, browserIntentKeywords) ||
				(containsAnyKeyword(msg, browserPageKeywords) && containsAnyKeyword(msg, browserActionKeywords))
		},
	},
	{
		keepTools: []string{"generate_pdf"},
		matches: func(msg string) bool {
			return containsAnyKeyword(msg, codingWorkflowDocKeywords)
		},
	},
}

// CodingSessionToolNames lists tools that require a coding LLM session provider.
// When the coding LLM is not configured (simple mode), these tools should be
// filtered out since they would be non-functional.
var CodingSessionToolNames = map[string]bool{
	"create_session":     true,
	"list_sessions":      true,
	"send_input":         true,
	"get_session_output": true,
	"get_session_events": true,
	"interrupt_session":  true,
	"kill_session":       true,
	"send_and_observe":   true,
	"control_session":    true,
	"list_providers":     true,
	"parallel_execute":   true,
	"recommend_tool":     true,
	"create_template":    true,
	"list_templates":     true,
	"launch_template":    true,
}

// IsCodingSessionTool returns true if the tool requires a coding LLM session.
func IsCodingSessionTool(name string) bool {
	return CodingSessionToolNames[name]
}

// FilterCodingTools removes coding session tools from the tool list.
func FilterCodingTools(tools []map[string]interface{}) []map[string]interface{} {
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if !CodingSessionToolNames[ExtractToolName(t)] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// BuiltinToolNames is the complete set of all builtin tool names.
// CoreToolNames are merged in automatically via init(), so there is no need
// to duplicate entries that already appear in CoreToolNames.
var BuiltinToolNames = map[string]bool{
	"list_providers":    true,
	"send_input":        true,
	"interrupt_session": true, "kill_session": true,
	"list_mcp_tools":   true,
	"search_skill_hub": true, "install_skill_hub": true,
	"parallel_execute": true, "recommend_tool": true, "craft_tool": true,
	"open":            true,
	"edit_file":       true,
	"create_template": true, "list_templates": true, "launch_template": true,
	"get_config": true, "update_config": true, "batch_update_config": true,
	"list_config_schema": true, "export_config": true, "import_config": true,
	"set_max_iterations":    true,
	"create_scheduled_task": true, "list_scheduled_tasks": true,
	"delete_scheduled_task": true, "update_scheduled_task": true,
	"search_and_install_skill": true,
	"switch_llm_provider":      true,
	"manage_config":            true,
	"manage_template":          true,
	"manage_schedule":          true,
	"query_audit_log":          true,
	// Browser automation tools (browser agent session + legacy CDP helpers).
	"browser_session_start": true, "browser_session_stop": true, "browser_observe": true,
	"browser_navigate": true, "browser_click": true, "browser_type": true,
	"browser_wait": true, "browser_back": true, "browser_refresh": true, "browser_extract": true,
	"browser_connect": true, "browser_screenshot": true, "browser_get_text": true,
	"browser_get_html": true, "browser_eval": true, "browser_scroll": true,
	"browser_select": true, "browser_list_pages": true, "browser_switch_page": true,
	"browser_close": true, "browser_click_at": true, "browser_set_files": true,
	"browser_info": true,
}

func init() {
	// Ensure every core tool is also recognized as builtin.
	for name := range CoreToolNames {
		BuiltinToolNames[name] = true
	}
}

// IsBuiltinToolName returns true if the tool name is a known builtin tool (static fallback).
func IsBuiltinToolName(name string) bool {
	return BuiltinToolNames[name]
}

// SkillRecommendation represents a recommended skill from the hub.
type SkillRecommendation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillRecommender abstracts access to skill hub recommendations (decouples from SkillHubClient).
type SkillRecommender interface {
	GetRecommendations() []SkillRecommendation
}

// SkillSummary is a lightweight view of an active Skill for routing.
type SkillSummary struct {
	Name        string
	Triggers    []string
	Description string
}

// SkillProvider abstracts access to active skills for routing decisions.
type SkillProvider interface {
	ListActiveSkills() []SkillSummary
}

// Router selects the most relevant tools for a given user message.
type Router struct {
	generator     *DefinitionGenerator
	registry      *Registry
	recommender   SkillRecommender
	skillProvider SkillProvider
	bm25Index     *bm25.Index
	skillBM25     *bm25.Index // separate index for skill trigger matching
	hybrid        *HybridRetriever
	enrichStore   *EnrichmentStore
	tracker       *UsageTracker
	reranker      Reranker // nil when reranking is disabled
	sessionTools  map[string]bool
	intentClassifier *IntentClassifier // hybrid intent classifier (Layer 1+2+3)
}

func NewRouter(generator *DefinitionGenerator) *Router {
	return &Router{
		generator: generator,
		bm25Index: bm25.New(),
		skillBM25: bm25.New(),
	}
}

// SetSkillProvider sets the SkillProvider used for skill-aware routing.
func (r *Router) SetSkillProvider(provider SkillProvider) {
	r.skillProvider = provider
	r.refreshSkillIndex()
}

// refreshSkillIndex rebuilds the skill BM25 index from the current SkillProvider.
// Called once on SetSkillProvider; subsequent refreshes are triggered explicitly
// via RefreshSkillIndex() to avoid rebuilding on every Route() call.
func (r *Router) refreshSkillIndex() {
	if r.skillProvider == nil {
		return
	}
	skills := r.skillProvider.ListActiveSkills()
	docs := make([]bm25.Doc, len(skills))
	for i, s := range skills {
		text := s.Name + " " + s.Description + " " + strings.Join(s.Triggers, " ")
		docs[i] = bm25.Doc{ID: s.Name, Text: text}
	}
	r.skillBM25.Rebuild(docs)
}

// RefreshSkillIndex forces a rebuild of the skill BM25 index.
// Call this after new skills are learned or existing skills are updated.
func (r *Router) RefreshSkillIndex() {
	r.refreshSkillIndex()
}

// skillMatchScore computes the best skill match score for the given user message.
// Returns a score in [0,1] and the names of the top matched skills.
func (r *Router) skillMatchScore(userMessage string) (float64, []string) {
	if r.skillProvider == nil {
		return 0, nil
	}
	// Index is built on SetSkillProvider / RefreshSkillIndex — no rebuild here.

	scores := r.skillBM25.Score(userMessage)
	if len(scores) == 0 {
		return 0, nil
	}

	// Find top-3 by score.
	type entry struct {
		name  string
		score float64
	}
	var sorted []entry
	for name, sc := range scores {
		if sc > 0 {
			sorted = append(sorted, entry{name, sc})
		}
	}
	if len(sorted) == 0 {
		return 0, nil
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].score > sorted[j].score })

	// Normalize: clamp raw BM25 score to [0,1] using sigmoid-like mapping.
	// Raw BM25 scores vary widely; a score > 1.0 indicates strong match.
	bestRaw := sorted[0].score
	normBest := clampFloat(bestRaw/3.0, 0, 1) // scale: 3.0 raw → 1.0 normalized

	n := 3
	if len(sorted) < n {
		n = len(sorted)
	}
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = sorted[i].name
	}
	return normBest, names
}

// SetRegistry sets the Registry used for dynamic builtin detection and tag-based scoring.
func (r *Router) SetRegistry(reg *Registry) {
	r.registry = reg
}

// SetRecommender sets the SkillRecommender used for recommendation matching.
func (r *Router) SetRecommender(recommender SkillRecommender) {
	r.recommender = recommender
}

// SetEmbedder configures the embedder for hybrid retrieval.
// If emb is a NoopEmbedder, hybrid is disabled (set to nil).
func (r *Router) SetEmbedder(emb embedding.Embedder) {
	if embedding.IsNoop(emb) {
		r.hybrid = nil
		return
	}
	r.hybrid = NewHybridRetriever(emb)
}

// HybridActive returns true if hybrid retrieval is currently enabled.
func (r *Router) HybridActive() bool {
	return r.hybrid != nil
}

// SetEnrichmentStore configures the enrichment store for enhanced tool descriptions.
func (r *Router) SetEnrichmentStore(store *EnrichmentStore) {
	r.enrichStore = store
}

// SetUsageTracker configures the usage tracker for experience-aware scoring.
func (r *Router) SetUsageTracker(tracker *UsageTracker) {
	r.tracker = tracker
}

// SetReranker configures the LLM listwise reranker. Pass nil to disable.
func (r *Router) SetReranker(rr Reranker) {
	r.reranker = rr
}

// SetIntentClassifier sets the hybrid intent classifier used for semantic
// intent detection in conditional tool matching and routing decisions.
func (r *Router) SetIntentClassifier(ic *IntentClassifier) {
	r.intentClassifier = ic
}

// IntentClassifier returns the configured IntentClassifier, or nil.
func (r *Router) IntentClassifier() *IntentClassifier {
	return r.intentClassifier
}

// ActivateSessionTool adds a tool to the current session's always-include set.
func (r *Router) ActivateSessionTool(name string) {
	if r.sessionTools == nil {
		r.sessionTools = make(map[string]bool)
	}
	r.sessionTools[name] = true
}

// ResetSession clears session-activated tools.
func (r *Router) ResetSession() {
	r.sessionTools = nil
}

// WarmupDeferredEmbeddings pre-computes and caches embedding vectors for
// deferred tool descriptions in the background. Call this after SearchDeferred
// returns results so that when the tools are activated and enter the Route()
// pipeline, their embeddings are already warm in ToolEmbeddingCache.
// No-op when hybrid retrieval is not active.
func (r *Router) WarmupDeferredEmbeddings(toolDefs []map[string]interface{}) {
	if r.hybrid == nil || len(toolDefs) == 0 {
		return
	}
	texts := make(map[string]string, len(toolDefs))
	for _, def := range toolDefs {
		name := ExtractToolName(def)
		if name == "" {
			continue
		}
		desc := ExtractToolDescription(def)
		texts[name] = r.buildEmbeddingText(name, desc)
	}
	if len(texts) == 0 {
		return
	}
	// Fire-and-forget: GetBatch populates the cache and triggers async disk save.
	go func() {
		_, _ = r.hybrid.toolCache.GetBatch(texts)
		log.Printf("[Router] warmed up embeddings for %d deferred tools", len(texts))
	}()
}

// buildSearchText returns the enriched search text for a tool if an enrichment
// store is configured, otherwise falls back to name + description + tags.
func (r *Router) buildSearchText(name, description string) string {
	if r.enrichStore != nil && r.registry != nil {
		if t, ok := r.registry.Get(name); ok {
			return r.enrichStore.GetSearchText(*t)
		}
	}
	text := name + " " + description
	if tags := r.tagsForTool(name); len(tags) > 0 {
		text += " " + strings.Join(tags, " ")
	}
	return text
}

// buildEmbeddingText returns the text used for embedding vector computation.
// Includes name + description + BodySummary when available.
// Falls back to name + description when BodySummary is empty.
func (r *Router) buildEmbeddingText(name, description string) string {
	text := name + " " + description
	if r.registry != nil {
		if t, ok := r.registry.Get(name); ok && t.BodySummary != "" {
			text += "\n" + t.BodySummary
		}
	}
	return text
}

func (r *Router) isBuiltin(name string) bool {
	if r.registry != nil {
		if t, ok := r.registry.Get(name); ok {
			return t.Category == CategoryBuiltin || t.Category == CategoryNonCode
		}
		return false
	}
	return IsBuiltinToolName(name)
}

func (r *Router) tagsForTool(name string) []string {
	if r.registry == nil {
		return nil
	}
	if t, ok := r.registry.Get(name); ok {
		return t.Tags
	}
	return nil
}

// allConditionalKeepTools returns the set of all tool names that appear in any
// conditional keep rule.
var allConditionalKeepTools map[string]bool

func init() {
	allConditionalKeepTools = make(map[string]bool)
	for _, rule := range conditionalKeepRules {
		for _, name := range rule.keepTools {
			allConditionalKeepTools[name] = true
		}
	}
}

// IsConditionalTool returns true if the tool is governed by a conditional keep
// rule. Such tools are only included when the user message matches certain
// keywords. Once actually used in a session, callers should pin them via
// ActivateSessionTool so they remain available for follow-up messages.
func IsConditionalTool(name string) bool {
	return allConditionalKeepTools[name]
}

// noPinConditionalTools lists conditional tools that should NOT be session-pinned
// after use. These tools should only appear when the current message matches
// their keywords, and should disappear when the conversation topic changes.
var noPinConditionalTools = map[string]bool{
	"generate_pdf": true,
}

// ShouldPinConditionalTool returns true if the conditional tool should be
// session-pinned after successful use. Some conditional tools (like generate_pdf)
// should NOT be pinned because they should only appear in specific contexts.
func ShouldPinConditionalTool(name string) bool {
	return allConditionalKeepTools[name] && !noPinConditionalTools[name]
}

// MatchConditionalTools returns the set of conditional tool names that match
// the given text. This is used to pin tools based on recalled memory content
// (e.g. when memory mentions "服务器" or "SSH", the ssh tool should be pinned).
func MatchConditionalTools(text string) map[string]bool {
	keep, _ := matchConditionalKeepRules(text)
	return keep
}

// matchConditionalKeepRules returns the set of tool names to conditionally keep
// and the set to penalize for the given user message.
// Tools that have a conditional keep rule but did NOT match the current message
// are filtered out entirely so they don't sneak in via tie-breaking at the score tail.
func matchConditionalKeepRules(userMessage string) (keep map[string]bool, filterOut map[string]bool) {
	keep = make(map[string]bool)
	filterOut = make(map[string]bool)
	msg := strings.ToLower(strings.TrimSpace(userMessage))
	if msg == "" {
		// Empty message: filter out ALL conditionally-kept tools.
		for name := range allConditionalKeepTools {
			filterOut[name] = true
		}
		return keep, filterOut
	}
	for _, rule := range conditionalKeepRules {
		if rule.matches(msg) {
			for _, name := range rule.keepTools {
				keep[name] = true
			}
		}
	}
	// Filter out tools that are conditionally kept but NOT matched this time.
	for name := range allConditionalKeepTools {
		if !keep[name] {
			filterOut[name] = true
		}
	}
	return keep, filterOut
}

// Route selects the most relevant tools for userMessage from allTools.
func (r *Router) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	if len(allTools) <= MaxToolBudget {
		return allTools
	}

	condKeep, condFilterOut := matchConditionalKeepRules(userMessage)

	// Semantic intent enhancement: when keyword matching misses but the
	// IntentClassifier detects a specific intent, activate the corresponding
	// conditional tools. This catches cases like "帮我搞个能自动抢票的东西"
	// where no SSH/browser keywords appear but embedding detects the intent.
	if r.intentClassifier != nil {
		icResult := r.intentClassifier.Classify(userMessage)
		if icResult.Intent != IntentQuery && icResult.Intent != IntentShortCommand &&
			icResult.Intent != IntentUnknown && icResult.Confidence >= 0.50 {
			var intentTools []string
			switch icResult.Intent {
			case IntentSSH:
				intentTools = []string{"ssh"}
			case IntentBrowser:
				// Activate all browser-related conditional tools.
				for name := range allConditionalKeepTools {
					if strings.HasPrefix(name, "browser_") || name == "gui_record_start" || name == "gui_record_stop" {
						intentTools = append(intentTools, name)
					}
				}
			}
			for _, name := range intentTools {
				if !condKeep[name] {
					condKeep[name] = true
					delete(condFilterOut, name)
				}
			}
		}
	}

	// Eager pin: when a conditional tool matches the current message,
	// pin it to the session immediately so it survives follow-up messages
	// that lack the triggering keywords (e.g. user says "回忆下" after
	// asking about a server — ssh should stay available).
	for name := range condKeep {
		if ShouldPinConditionalTool(name) {
			r.ActivateSessionTool(name)
		}
	}

	var core, candidates []map[string]interface{}
	var candidateNames []string
	for _, t := range allTools {
		name := ExtractToolName(t)
		if CoreToolNames[name] || r.sessionTools[name] || condKeep[name] {
			core = append(core, t)
		} else if condFilterOut[name] {
			// This tool has a conditional keep rule that did NOT match this
			// message — exclude it from candidates entirely.
			continue
		} else {
			candidates = append(candidates, t)
			candidateNames = append(candidateNames, name)
		}
	}

	if len(candidates) == 0 || len(core) >= MaxToolBudget {
		return core
	}

	// Build a BM25 index over candidate tool descriptions (reuses cached index).
	docs := make([]bm25.Doc, len(candidates))
	candidateTexts := make(map[string]string, len(candidates))
	embeddingTexts := make(map[string]string, len(candidates))
	for i, t := range candidates {
		name := candidateNames[i]
		desc := ExtractToolDescription(t)
		text := r.buildSearchText(name, desc)
		docs[i] = bm25.Doc{ID: name, Text: text}
		candidateTexts[name] = text
		embeddingTexts[name] = r.buildEmbeddingText(name, desc)
	}
	r.bm25Index.RebuildIfChanged(docs)
	scores := r.bm25Index.Score(userMessage)

	// Fuse with vector scores when hybrid retrieval is active.
	if r.hybrid != nil {
		scores = r.hybrid.FuseScores(userMessage, scores, embeddingTexts)

		// Debug log top-5 tools with fused scores.
		type debugEntry struct {
			name  string
			score float64
		}
		debugList := make([]debugEntry, 0, len(scores))
		for name, s := range scores {
			debugList = append(debugList, debugEntry{name: name, score: s})
		}
		sort.Slice(debugList, func(i, j int) bool {
			return debugList[i].score > debugList[j].score
		})
		n := 5
		if len(debugList) < n {
			n = len(debugList)
		}
		for i := 0; i < n; i++ {
			log.Printf("[HybridRoute] #%d %s fused=%.4f", i+1, debugList[i].name, debugList[i].score)
		}
	}

	// Three-signal scoring: retrieval + experience + priority + skill_match.
	queryTokens := bm25.Tokenize(userMessage)
	normScores := minMaxNormalize(scores)

	// Compute skill match score (fourth signal).
	var skillScore float64
	var matchedSkills []string
	if r.skillProvider != nil {
		skillScore, matchedSkills = r.skillMatchScore(userMessage)
	}

	type scored struct {
		index int
		score float64
	}
	scoredList := make([]scored, len(candidates))
	for i, name := range candidateNames {
		retrievalScore := normScores[name]
		var expScore float64
		if r.tracker != nil {
			expScore = r.tracker.ExperienceScore(name, queryTokens)
		}
		var outcomeScore float64
		if r.tracker != nil {
			outcomeScore = r.tracker.OutcomeScore(name)
		}
		var priorityBonus float64
		if r.registry != nil {
			if t, ok := r.registry.Get(name); ok {
				priorityBonus = clampFloat(float64(t.Priority)*0.1, 0, 1)
			}
		}

		// Skill match bonus: only applies to run_skill tool.
		var skillBonus float64
		if r.skillProvider != nil && name == "run_skill" {
			skillBonus = skillScore
		}

		var finalScore float64
		if r.skillProvider != nil && r.tracker != nil {
			// Five signals: α=0.45 retrieval + β=0.20 experience + γ=0.15 skill_match + δ=0.10 outcome + ε=0.10 priority
			finalScore = 0.45*retrievalScore + 0.20*expScore + 0.15*skillBonus + 0.10*outcomeScore + 0.10*priorityBonus
		} else if r.tracker != nil {
			// α=0.50 retrieval + β=0.25 experience + γ=0.15 outcome + δ=0.10 priority
			finalScore = 0.50*retrievalScore + 0.25*expScore + 0.15*outcomeScore + 0.10*priorityBonus
		} else {
			// No tracker: α=0.9 retrieval + γ=0.1 priority
			finalScore = 0.9*retrievalScore + 0.1*priorityBonus
		}

		scoredList[i] = scored{index: i, score: finalScore}
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	// Rerank top candidates when reranker is configured and candidates exceed budget.
	var rerankerResult []string
	if r.reranker != nil && len(scoredList) > MaxToolBudget {
		// Take top-20 candidates for reranking.
		rerankerCount := 20
		if rerankerCount > len(scoredList) {
			rerankerCount = len(scoredList)
		}
		summaries := make([]CandidateSummary, rerankerCount)
		for i := 0; i < rerankerCount; i++ {
			name := candidateNames[scoredList[i].index]
			desc := ExtractToolDescription(candidates[scoredList[i].index])
			var bodySummary string
			if r.registry != nil {
				if t, ok := r.registry.Get(name); ok {
					bodySummary = t.BodySummary
				}
			}
			summaries[i] = CandidateSummary{
				Name:        name,
				Description: desc,
				BodySummary: bodySummary,
			}
		}

		reranked, err := r.reranker.Rerank(userMessage, summaries, 5)
		if err != nil || len(reranked) == 0 {
			if err != nil {
				log.Printf("[Router] WARN: reranker failed: %v, falling back to fused scores", err)
			}
			// Fall back to fused score ordering — no change to scoredList
		} else {
			rerankerResult = reranked
			// Promote reranked results to front of scored list.
			// Build a set of reranked names for quick lookup.
			rerankedSet := make(map[string]bool, len(reranked))
			for _, name := range reranked {
				rerankedSet[name] = true
			}

			// Build new scored list: reranked first (in reranker order), then remaining by fused score.
			newScored := make([]scored, 0, len(scoredList))
			// Add reranked items first, in reranker order.
			for _, name := range reranked {
				for _, s := range scoredList {
					if candidateNames[s.index] == name {
						newScored = append(newScored, s)
						break
					}
				}
			}
			// Supplement with remaining items from fused score list.
			for _, s := range scoredList {
				if !rerankedSet[candidateNames[s.index]] {
					newScored = append(newScored, s)
				}
			}

			// If reranker returned < 5 results, the remaining are already supplemented from fused scores.
			scoredList = newScored
		}
	}

	dynamicCount := 0
	result := make([]map[string]interface{}, len(core), MaxToolBudget+2)
	copy(result, core)

	// Enhance run_skill description with matched skill names.
	if len(matchedSkills) > 0 && skillScore > 0.3 {
		for i, t := range result {
			if ExtractToolName(t) == "run_skill" {
				result[i] = enrichRunSkillDescription(t, matchedSkills)
				break
			}
		}
	}

	for _, s := range scoredList {
		if len(result) >= MaxToolBudget {
			break
		}
		if !r.isBuiltin(candidateNames[s.index]) {
			dynamicCount++
			if dynamicCount > MaxDynamicRouted {
				continue
			}
		}
		result = append(result, candidates[s.index])
	}

	if r.recommender != nil {
		if hint := r.matchRecommendations(bm25.Tokenize(userMessage)); hint != nil {
			result = append(result, hint)
		}
	}

	// Write detailed routing log to ~/.maclaw/logs/tool_route.log
	selectedNames := make([]string, len(result))
	for i, t := range result {
		selectedNames[i] = ExtractToolName(t)
	}
	rankedNames := make([]string, len(scoredList))
	rankedScores := make([]float64, len(scoredList))
	for i, s := range scoredList {
		rankedNames[i] = candidateNames[s.index]
		rankedScores[i] = s.score
	}

	// Compute bodyAware: true when hybrid is active and any candidate has non-empty BodySummary.
	bodyAware := false
	if r.hybrid != nil && r.registry != nil {
		for _, name := range candidateNames {
			if t, ok := r.registry.Get(name); ok && t.BodySummary != "" {
				bodyAware = true
				break
			}
		}
	}

	go writeRouteLog(userMessage, len(allTools), len(core), len(candidates), r.hybrid != nil, bodyAware, rankedNames, rankedScores, selectedNames, rerankerResult, skillScore, matchedSkills)

	return result
}

func (r *Router) matchRecommendations(msgTokens []string) map[string]interface{} {
	if len(msgTokens) == 0 {
		return nil
	}
	recommendations := r.recommender.GetRecommendations()
	if len(recommendations) == 0 {
		return nil
	}
	msgSet := make(map[string]struct{}, len(msgTokens))
	for _, t := range msgTokens {
		msgSet[t] = struct{}{}
	}
	for _, rec := range recommendations {
		recTokens := bm25.Tokenize(rec.Name + " " + rec.Description)
		matchCount := 0
		for _, rt := range recTokens {
			if _, ok := msgSet[rt]; ok {
				matchCount++
				if len([]rune(rt)) > 1 {
					return SearchAndInstallSkillHint()
				}
			}
		}
		if matchCount >= 2 {
			return SearchAndInstallSkillHint()
		}
	}
	return nil
}

// SearchAndInstallSkillHint returns a tool definition for the search_and_install_skill hint.
func SearchAndInstallSkillHint() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "search_and_install_skill",
			"description": "Search SkillHub for a matching Skill and install it. Use this when the user's request might be handled by a Skill available on the Hub.",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// ExtractToolDescription extracts the description from an OpenAI function calling tool definition.
func ExtractToolDescription(def map[string]interface{}) string {
	fn, ok := def["function"]
	if !ok {
		return ""
	}
	fnMap, ok := fn.(map[string]interface{})
	if !ok {
		return ""
	}
	desc, _ := fnMap["description"].(string)
	return desc
}

// enrichRunSkillDescription returns a shallow copy of the run_skill tool
// definition with matched skill names appended to the description.
func enrichRunSkillDescription(def map[string]interface{}, skillNames []string) map[string]interface{} {
	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		return def
	}
	desc, _ := fn["description"].(string)
	suffix := " 可用 Skill: " + strings.Join(skillNames, ", ")
	newFn := make(map[string]interface{}, len(fn))
	for k, v := range fn {
		newFn[k] = v
	}
	newFn["description"] = desc + suffix
	newDef := make(map[string]interface{}, len(def))
	for k, v := range def {
		newDef[k] = v
	}
	newDef["function"] = newFn
	return newDef
}

// writeRouteLog writes a detailed tool routing decision log to ~/.maclaw/logs/tool_route.log.
// Runs in a goroutine to avoid blocking the hot path.
func writeRouteLog(
	userMessage string,
	totalTools, coreCount, candidateCount int,
	hybridActive bool,
	bodyAware bool,
	rankedNames []string,
	rankedScores []float64,
	selectedNames []string,
	rerankerResult []string,
	skillMatchScore float64,
	matchedSkills []string,
) {
	if !logDetailEnabled.Load() {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".maclaw", "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "tool_route.log")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Truncate if over 5MB to prevent unbounded growth.
	if info, e := f.Stat(); e == nil && info.Size() > 5*1024*1024 {
		f.Truncate(0)
		f.Seek(0, 0)
		fmt.Fprintln(f, "[log truncated — exceeded 5MB]")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "\n=== Tool Route [%s] ===\n", now)
	msgPreview := userMessage
	if len([]rune(msgPreview)) > 100 {
		msgPreview = string([]rune(msgPreview)[:100]) + "..."
	}
	fmt.Fprintf(f, "Message: %s\n", msgPreview)
	fmt.Fprintf(f, "Total tools: %d | Core: %d | Candidates: %d | Hybrid: %v\n",
		totalTools, coreCount, candidateCount, hybridActive)
	fmt.Fprintf(f, "Body-aware: %v\n", bodyAware)

	// Top-20 candidates by score
	n := 20
	if len(rankedNames) < n {
		n = len(rankedNames)
	}
	fmt.Fprintf(f, "Top-%d candidates by fused score:\n", n)
	for i := 0; i < n; i++ {
		fmt.Fprintf(f, "  #%d %s = %.4f\n", i+1, rankedNames[i], rankedScores[i])
	}

	// Final selected tools
	fmt.Fprintf(f, "Selected tools (%d):\n", len(selectedNames))
	for _, name := range selectedNames {
		fmt.Fprintf(f, "  - %s\n", name)
	}

	// Reranker output (if invoked)
	if len(rerankerResult) > 0 {
		fmt.Fprintf(f, "Reranker output (%d):", len(rerankerResult))
		for i, name := range rerankerResult {
			fmt.Fprintf(f, " #%d %s", i+1, name)
		}
		fmt.Fprintln(f)
	}

	// Skill match info
	if skillMatchScore > 0 || len(matchedSkills) > 0 {
		fmt.Fprintf(f, "Skill match: score=%.4f matched=%v\n", skillMatchScore, matchedSkills)
	}

	fmt.Fprintln(f, "---")
}

// clampFloat clamps v to [lo, hi].
func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
