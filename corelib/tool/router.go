package tool

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

const (
	// MaxToolBudget is the maximum number of tools to send to the LLM.
	MaxToolBudget = 28

	// MaxDynamicRouted caps how many MCP/non-code dynamic tools can be included.
	MaxDynamicRouted = 18
)

var logDetailEnabled atomic.Bool
var routeLogPathOverride atomic.Value

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
	"bash":            true, "read_file": true, "FileRead": true, "ripgrep": true, "Glob": true, "write_file": true, "edit_file": true, "list_directory": true,
	"call_mcp_tool":    true,
	"manage_skill":     true,
	"screenshot":       true,
	"memory":           true,
	"web_fetch":        true,
	"set_nickname":     true,
	"discover_tool":    true,
	"task":             true,
	"async_wait":       true,
	"compress_context": true,
	"tts":              true,
}

type conditionalKeepRule struct {
	keepTools []string
	// noMemoryPin when true means tools from this rule should NOT be pinned
	// via memory-driven pinning or eager pinning during Route(). They are pinned
	// after actual successful use through ActivateSessionTool.
	noMemoryPin bool
}

// allBrowserToolNames is the complete list of browser automation tools used
// by browser conditional keep rules. The core browser actions are merged into
// a single "browser" tool (see unified_tool.go).
// Task/recorder/OCR/GUI tools remain as individual tools.
var allBrowserToolNames = []string{
	// Unified browser tool (replaces 22 individual browser_* tools).
	"browser",
	// Browser task/record/replay tools (low-frequency, kept individual).
	"browser_task_run", "browser_task_replay", "browser_task_verify", "browser_task_status",
	"browser_record_start", "browser_record_stop", "browser_list_flows",
	// OCR tool.
	"browser_ocr",
	// GUI automation recording tools.
	"gui_record_start", "gui_record_stop",
}

// NoEagerPinToolNames returns a copy of the noEagerPinTools set as a slice.
// Used by diagnostic code to derive tool sets from the canonical source.
// This set is derived from conditionalKeepRules with noMemoryPin=true.
func NoEagerPinToolNames() []string {
	out := make([]string, 0, len(noEagerPinTools))
	for name := range noEagerPinTools {
		out = append(out, name)
	}
	return out
}

// IsNoEagerPinTool returns true if the named tool is in the noEagerPinTools
// set (derived from conditionalKeepRules with noMemoryPin=true). Such tools
// should not be session-pinned via local matching or memory-driven pinning.
func IsNoEagerPinTool(name string) bool {
	return noEagerPinTools[name]
}

// NOTE: Desktop GUI tools (gui_observe, gui_verify, gui_record_start, gui_record_stop)
// are NOT in conditionalKeepRules. They live in DeferredToolNames, discoverable
// via discover_tool. This avoids false-positive local activation (#87).

var conditionalKeepRules = []conditionalKeepRule{
	{keepTools: []string{"mis_data"}},
	{keepTools: []string{"ssh"}},
	{keepTools: []string{"web_search"}},
	{keepTools: []string{"send_file", "open"}},
	{keepTools: []string{"craft_tool"}},
	{keepTools: allBrowserToolNames, noMemoryPin: true},
	{keepTools: []string{"office"}},
	{keepTools: []string{"generate_pdf", "office"}},
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
	"list_mcp_tools": true,
	"manage_skill":   true,
	"list_skills":    true, "run_skill": true, "get_skill_run": true,
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
	"session_search":           true,
	"office":                   true,
	"mis_data":                 true,
	// Browser automation: unified "browser" tool + individual task/recorder tools.
	"browser":          true,
	"browser_task_run": true, "browser_task_replay": true,
	"browser_task_verify": true, "browser_task_status": true,
	"browser_record_start": true, "browser_record_stop": true, "browser_list_flows": true,
	"browser_ocr": true,
	// Knowledge tools (registered via CoreToolDeps.ExtraHandlers).
	"knowledge_search":       true,
	"knowledge_context_pack": true,
	"knowledge_save_text":    true,
	"knowledge_save_url":     true,
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

	// Tool availability conditions (from NLSkillEntry).
	RequiresTools       []string
	FallbackForTools    []string
	RequiresToolsets    []string
	FallbackForToolsets []string
}

// SkillProvider abstracts access to active skills for routing decisions.
type SkillProvider interface {
	ListActiveSkills() []SkillSummary
}

// Router selects the most relevant tools for a given user message.
type Router struct {
	generator         *DefinitionGenerator
	registry          *Registry
	recommender       SkillRecommender
	skillProvider     SkillProvider
	bm25Index         *bm25.Index
	skillBM25         *bm25.Index // separate index for skill trigger matching
	hybrid            *HybridRetriever
	enrichStore       *EnrichmentStore
	tracker           *UsageTracker
	reranker          Reranker // nil when reranking is disabled
	sessionTools      map[string]bool
	intentClassifier  *IntentClassifier               // hybrid intent classifier (Layer 1+2+3)
	unifiedClassifier *intent.UnifiedIntentClassifier // UIC replaces conditionalKeepRules when non-nil
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
	// Index is built on SetSkillProvider / RefreshSkillIndex; no rebuild here.

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
	normBest := clampFloat(bestRaw/3.0, 0, 1) // scale: 3.0 raw => 1.0 normalized

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

// buildAvailableToolsMap constructs a set of available tool names from the
// given tool definitions. Used for evaluating skill tool availability conditions.
func buildAvailableToolsMap(allTools []map[string]interface{}) map[string]bool {
	available := make(map[string]bool, len(allTools))
	for _, t := range allTools {
		name := ExtractToolName(t)
		if name != "" {
			available[name] = true
		}
	}
	return available
}

// filterSkillsByConditions filters matched skill names through tool availability
// conditions. Only skills whose conditions are satisfied (or have no conditions)
// are retained.
func (r *Router) filterSkillsByConditions(matchedSkills []string, availableTools map[string]bool) []string {
	if r.skillProvider == nil {
		return matchedSkills
	}
	// Build a lookup of skill conditions by name.
	skills := r.skillProvider.ListActiveSkills()
	condByName := make(map[string]*SkillSummary, len(skills))
	for i := range skills {
		condByName[skills[i].Name] = &skills[i]
	}

	filtered := make([]string, 0, len(matchedSkills))
	for _, name := range matchedSkills {
		s, ok := condByName[name]
		if !ok {
			// Skill not found in provider; include it defensively.
			filtered = append(filtered, name)
			continue
		}
		if EvaluateToolConditionsForSkill(
			s.RequiresTools, s.FallbackForTools,
			s.RequiresToolsets, s.FallbackForToolsets,
			availableTools,
		) {
			filtered = append(filtered, name)
		}
	}
	return filtered
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

// SetUnifiedClassifier sets the Unified Intent Classifier used for
// intent-driven conditional tool selection. When non-nil, Route() uses
// UIC results instead of evaluating local conditional keep rules.
func (r *Router) SetUnifiedClassifier(uic *intent.UnifiedIntentClassifier) {
	r.unifiedClassifier = uic
}

// ActivateSessionTool adds a tool to the current session's always-include set.
func (r *Router) ActivateSessionTool(name string) {
	if r.sessionTools == nil {
		r.sessionTools = make(map[string]bool)
	}
	r.sessionTools[name] = true
}

// IsSessionPinned returns true if the named tool has been session-pinned
// via ActivateSessionTool. This is used by callers (e.g. routeTools) to
// avoid removing a tool that was previously used in this session.
func (r *Router) IsSessionPinned(name string) bool {
	return r.sessionTools[name]
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
// rule. Such tools are included only when semantic routing selects them.
// Once actually used in a session, callers should pin them via
// ActivateSessionTool so they remain available for follow-up messages.
func IsConditionalTool(name string) bool {
	return allConditionalKeepTools[name]
}

// noPinConditionalTools lists conditional tools that should NOT be session-pinned
// after use. These tools should only appear for the current routed task and
// should disappear when the conversation topic changes.
var noPinConditionalTools = map[string]bool{
	"generate_pdf": true,
	"office":       true,
}

// noEagerPinTools lists conditional tools that should NOT be eagerly pinned
// during Route() local matching or memory-driven pinning, but SHOULD be
// pinned after actual successful use (via ActivateSessionTool in the tool
// execution path).
//
// This set is derived automatically from conditionalKeepRules: any rule with
// noMemoryPin=true contributes all its keepTools to this set. This ensures
// the protection is co-located with the rule definition: adding a new rule
// with noMemoryPin=true automatically protects its tools, with zero changes
// needed in MatchConditionalTools, Route(), or any other consumer.
var noEagerPinTools map[string]bool

func init() {
	noEagerPinTools = make(map[string]bool)
	for _, rule := range conditionalKeepRules {
		if rule.noMemoryPin {
			for _, name := range rule.keepTools {
				noEagerPinTools[name] = true
			}
		}
	}
}

// ShouldPinConditionalTool returns true if the conditional tool should be
// session-pinned after successful use. Some conditional tools (like generate_pdf)
// should NOT be pinned because they should only appear in specific contexts.
func ShouldPinConditionalTool(name string) bool {
	return allConditionalKeepTools[name] && !noPinConditionalTools[name]
}

// isBrowserDiagTool returns true if the tool name is a browser-related or
// desktop GUI tool for diagnostic logging purposes. Uses noEagerPinTools
// as the canonical set; any tool protected from memory-driven pinning is
// worth diagnostic logging.
func isBrowserDiagTool(name string) bool {
	return noEagerPinTools[name]
}

// MatchConditionalTools intentionally does not infer execution tools from
// recalled memory text. Memory recall can surface context, but pinning SSH,
// browser, or similar tools from lexical mentions in past summaries is too
// error-prone. Conditional tools are activated by UIC/semantic routing for the
// current user message, or after actual successful use via ActivateSessionTool.
func MatchConditionalTools(text string) map[string]bool {
	return map[string]bool{}
}

// matchConditionalKeepRules keeps the legacy API shape for callers and tests,
// but local wording is no longer an execution-routing authority. Conditional
// tools start filtered and are promoted only by semantic classifiers in Route.
func matchConditionalKeepRules(userMessage string) (keep map[string]bool, filterOut map[string]bool, needsConfirm map[string]string) {
	_ = userMessage
	keep = make(map[string]bool)
	filterOut = make(map[string]bool)
	needsConfirm = make(map[string]string)
	for name := range allConditionalKeepTools {
		filterOut[name] = true
	}
	return keep, filterOut, needsConfirm
}

// Route selects the most relevant tools for userMessage from allTools.
func (r *Router) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	var condKeep map[string]bool
	var condFilterOut map[string]bool
	var cachedICResult *IntentResult

	if r.unifiedClassifier != nil {
		// UIC path: use UnifiedIntentClassifier to determine which conditional
		// tools to keep, replacing local matchConditionalKeepRules.
		condKeep = make(map[string]bool)
		condFilterOut = make(map[string]bool)

		uicResult := r.unifiedClassifier.Classify(intent.MessageContext{Text: userMessage})

		const uicToolActivationThreshold = 0.90
		uicClear := !uicResult.Degraded &&
			uicResult.Primary != intent.LabelUnknown &&
			uicResult.Primary != intent.LabelAmbiguous &&
			uicResult.Confidence >= uicToolActivationThreshold

		if uicClear {
			// Use ToolNames from the UIC result to populate condKeep. UIC is an
			// execution-routing authority only when it is non-degraded and clear.
			for _, toolName := range uicResult.ToolNames {
				condKeep[toolName] = true
			}
		}

		// Filter out conditional tools NOT matched by UIC.
		for name := range allConditionalKeepTools {
			if !condKeep[name] {
				condFilterOut[name] = true
			}
		}

	} else {
		// Fallback path: UIC not available. Local conditional rules are not an
		// execution-routing authority, so keep every conditional tool filtered
		// unless the semantic IntentClassifier below explicitly promotes it.
		condKeep = make(map[string]bool)
		condFilterOut = make(map[string]bool)
		for name := range allConditionalKeepTools {
			condFilterOut[name] = true
		}

		if r.intentClassifier != nil {
			result := r.intentClassifier.Classify(userMessage)
			cachedICResult = &result
		}

		// Semantic intent enhancement: when local matching yields nothing but the
		// IntentClassifier detects a specific intent, activate the corresponding
		// conditional tools without consulting local lexical heuristics.
		//
		// Semantic promotion requires a clear high-confidence result. Unknown,
		// query, short-command, and low-confidence classifications fail closed.
		if cachedICResult != nil {
			const semanticToolActivationThreshold = 0.78
			if cachedICResult.Intent != IntentQuery && cachedICResult.Intent != IntentShortCommand &&
				cachedICResult.Intent != IntentUnknown &&
				cachedICResult.Confidence >= semanticToolActivationThreshold {
				var intentTools []string
				switch cachedICResult.Intent {
				case IntentSSH:
					intentTools = []string{"ssh"}
				case IntentBrowser:
					for name := range allConditionalKeepTools {
						if name == "browser" || strings.HasPrefix(name, "browser_") || name == "gui_record_start" || name == "gui_record_stop" {
							intentTools = append(intentTools, name)
						}
					}
				}
				// For tools in noEagerPinTools (high-cost sets like browser_*),
				// require higher confidence to avoid false-positive activation.
				for _, name := range intentTools {
					if condKeep[name] {
						continue
					}
					condKeep[name] = true
					delete(condFilterOut, name)
				}
			}
		}
	}

	// Eager pin: when semantic routing selects a conditional tool,
	// pin it to the session immediately so it survives follow-up messages
	// in the same task.
	// Tools from noMemoryPin rules are excluded (noEagerPinTools) because
	// eager pinning is prone to false positives. They get pinned after
	// actual successful use via ActivateSessionTool in the tool execution path.
	for name := range condKeep {
		if ShouldPinConditionalTool(name) && !noEagerPinTools[name] {
			r.ActivateSessionTool(name)
		}
	}

	// --- Browser diagnostic: log how browser tools entered condKeep ---
	{
		var browserInKeep []string
		var browserInSession []string
		for name := range condKeep {
			if isBrowserDiagTool(name) {
				browserInKeep = append(browserInKeep, name)
			}
		}
		for name := range r.sessionTools {
			if isBrowserDiagTool(name) {
				browserInSession = append(browserInSession, name)
			}
		}
		if len(browserInKeep) > 0 || len(browserInSession) > 0 {
			source := "unknown"
			if r.unifiedClassifier != nil {
				source = "UIC"
			} else {
				source = "semantic_intent"
			}
			log.Printf("[browser-diag] Route_condKeep: source=%s browserInKeep=%v browserInSession=%v",
				source, browserInKeep, browserInSession)
		}
	}

	var core, candidates []map[string]interface{}
	var candidateNames []string
	seenNames := make(map[string]bool, len(allTools))
	for _, t := range allTools {
		name := ExtractToolName(t)
		if seenNames[name] {
			continue // drop duplicates from allTools input
		}
		seenNames[name] = true
		if CoreToolNames[name] || r.sessionTools[name] || condKeep[name] {
			core = append(core, t)
		} else if condFilterOut[name] {
			// This tool has a conditional keep rule that did NOT match this
			// message; exclude it from candidates entirely.
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

		// Filter matched skills through tool availability conditions (AND logic).
		// Build availableTools set from the current tool list.
		if len(matchedSkills) > 0 {
			availableTools := buildAvailableToolsMap(allTools)
			matchedSkills = r.filterSkillsByConditions(matchedSkills, availableTools)
			// If all matched skills were filtered out, reset skill score.
			if len(matchedSkills) == 0 {
				skillScore = 0
			}
		}
	}

	type scored struct {
		index                 int
		score                 float64
		routingHintAdjustment float64
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
			outcomeScore = r.tracker.ContextOutcomeScore(name, queryTokens)
		}
		var priorityBonus float64
		if r.registry != nil {
			if t, ok := r.registry.Get(name); ok {
				priorityBonus = clampFloat(float64(t.Priority)*0.1, 0, 1)
			}
		}
		var routingHintAdjustment float64
		if r.tracker != nil {
			routingHintAdjustment = r.tracker.RoutingHintAdjustment(name, queryTokens)
		}

		// Skill match bonus: only applies to manage_skill tool.
		var skillBonus float64
		if r.skillProvider != nil && name == "manage_skill" {
			skillBonus = skillScore
		}

		var finalScore float64
		if r.skillProvider != nil && r.tracker != nil {
			// Five signals: retrieval + experience + skill match + outcome + priority.
			finalScore = 0.45*retrievalScore + 0.20*expScore + 0.15*skillBonus + 0.10*outcomeScore + 0.10*priorityBonus
		} else if r.tracker != nil {
			// Four signals: retrieval + experience + outcome + priority.
			finalScore = 0.50*retrievalScore + 0.25*expScore + 0.15*outcomeScore + 0.10*priorityBonus
		} else {
			// No tracker: retrieval + priority.
			finalScore = 0.9*retrievalScore + 0.1*priorityBonus
		}
		finalScore = clampFloat(finalScore+routingHintAdjustment, 0, 1)

		scoredList[i] = scored{index: i, score: finalScore, routingHintAdjustment: routingHintAdjustment}
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
			// Fall back to fused score ordering; no change to scoredList.
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

	// seenNames (built during core/candidate split above) already tracks all
	// tool names in core + candidates. Reuse it for downstream dedup; any
	// tool appended to result must check seenNames first.

	// Enhance manage_skill description with matched skill names.
	if len(matchedSkills) > 0 && skillScore > 0.3 {
		for i, t := range result {
			if ExtractToolName(t) == "manage_skill" {
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
			if name := ExtractToolName(hint); !seenNames[name] {
				result = append(result, hint)
				seenNames[name] = true
			}
		}
	}

	// Write detailed routing log to ~/.maclaw/logs/tool_route.log
	selectedNames := make([]string, 0, len(result))
	for _, t := range result {
		selectedNames = append(selectedNames, ExtractToolName(t))
	}
	rankedNames := make([]string, len(scoredList))
	rankedScores := make([]float64, len(scoredList))
	rankedRoutingHintAdjustments := make([]float64, len(scoredList))
	for i, s := range scoredList {
		rankedNames[i] = candidateNames[s.index]
		rankedScores[i] = s.score
		rankedRoutingHintAdjustments[i] = s.routingHintAdjustment
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

	go writeRouteLog(userMessage, len(allTools), len(core), len(candidates), r.hybrid != nil, bodyAware, rankedNames, rankedScores, rankedRoutingHintAdjustments, selectedNames, rerankerResult, skillScore, matchedSkills)

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
	rankedRoutingHintAdjustments []float64,
	selectedNames []string,
	rerankerResult []string,
	skillMatchScore float64,
	matchedSkills []string,
) {
	if !logDetailEnabled.Load() {
		return
	}
	logPath, ok := routeLogPath()
	if !ok {
		return
	}
	logDir := filepath.Dir(logPath)
	os.MkdirAll(logDir, 0755)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Truncate if over 5MB to prevent unbounded growth.
	if info, e := f.Stat(); e == nil && info.Size() > 5*1024*1024 {
		f.Truncate(0)
		f.Seek(0, 0)
		fmt.Fprintln(f, "[log truncated: exceeded 5MB]")
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
		adjustment := 0.0
		if i < len(rankedRoutingHintAdjustments) {
			adjustment = rankedRoutingHintAdjustments[i]
		}
		if adjustment != 0 {
			fmt.Fprintf(f, "  #%d %s = %.4f (routing_hint %+0.4f)\n", i+1, rankedNames[i], rankedScores[i], adjustment)
			continue
		}
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

func routeLogPath() (string, bool) {
	if value := routeLogPathOverride.Load(); value != nil {
		if path, ok := value.(string); ok && strings.TrimSpace(path) != "" {
			return path, true
		}
	}
	// Use BaseDirFunc if set (injected by corelib package to avoid circular import).
	if fn := BaseDirFunc.Load(); fn != nil {
		if dirFn, ok := fn.(func() string); ok {
			return filepath.Join(dirFn(), "logs", "tool_route.log"), true
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".maclaw", "logs", "tool_route.log"), true
}

// BaseDirFunc is an atomic.Value holding a func() string that returns the
// effective maclaw base directory. Set by the corelib package at init time
// to avoid circular imports. If not set, falls back to ~/.maclaw.
var BaseDirFunc atomic.Value

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
