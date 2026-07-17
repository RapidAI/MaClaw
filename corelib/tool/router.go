package tool

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

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
var routeLogMu sync.Mutex

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
	"list_sessions": true, "get_session_output": true, "get_session_events": true,
	"bash": true, "read_file": true, "read_tool_result": true, "FileRead": true, "ripgrep": true, "Glob": true, "write_file": true, "edit_file": true, "list_directory": true,
	"call_mcp_tool":    true,
	"manage_skill":     true,
	"memory":           true,
	"web_fetch":        true,
	"set_nickname":     true,
	"discover_tool":    true,
	"task":             true,
	"goal":             true,
	"async_wait":       true,
	"compress_context": true,
	"tts":              true,
	"asr":              true,
	// Desktop long-form / meeting recording. Always expose so hybrid budget
	// contention cannot hide it when the user asks to start recording.
	// On IM channels the tool itself returns a desktop-only message.
	"record_audio": true,
}

type conditionalKeepRule struct {
	keepTools []string
	// noMemoryPin when true means tools from this rule should NOT be pinned
	// from recalled memory or other indirect context. They are pinned after
	// actual successful use through ActivateSessionTool.
	noMemoryPin bool
}

// allBrowserToolNames is the browser automation surface exposed to routing.
// Browser actions, tasks, OCR, and flow helpers are all reached through the
// merged "browser" tool; individual browser_* handlers stay internal.
var allBrowserToolNames = []string{
	"browser",
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
// should not be session-pinned from indirect context.
func IsNoEagerPinTool(name string) bool {
	return noEagerPinTools[name]
}

// NOTE: Desktop GUI observe/verify tools (gui_observe, gui_verify) are NOT in
// conditionalKeepRules. They live in DeferredToolNames, discoverable via
// discover_tool. GUI record start/stop are browser-workflow conditional tools.
// This avoids false-positive local activation (#87).

var conditionalKeepRules = []conditionalKeepRule{
	{keepTools: []string{"mis_data"}},
	{keepTools: []string{"ssh"}},
	{keepTools: []string{"web_search"}},
	{keepTools: []string{"send_file", "send_to_im", "im_message", "open"}},
	{keepTools: []string{"craft_tool"}},
	{keepTools: allBrowserToolNames, noMemoryPin: true},
	{keepTools: []string{"office"}},
	{keepTools: []string{"generate_pdf", "office"}},
}

// CodingSessionToolNames lists tools that require a coding LLM session provider.
// When the coding LLM is not configured (simple mode), these tools should be
// filtered out since they would be non-functional.
var CodingSessionToolNames = map[string]bool{
	"list_sessions":      true,
	"send_input":         true,
	"get_session_output": true,
	"get_session_events": true,
	"interrupt_session":  true,
	"kill_session":       true,
	"list_providers":     true,
	"parallel_execute":   true,
	"recommend_tool":     true,
	"create_template":    true,
	"list_templates":     true,
	"launch_template":    true,
}

// IsCodingSessionTool returns true if the tool requires a coding LLM session.
func IsCodingSessionTool(name string) bool {
	return IsDisabledExternalCodingSessionTool(name) || CodingSessionToolNames[name]
}

// FilterCodingTools removes coding session tools from the tool list.
func FilterCodingTools(tools []map[string]interface{}) []map[string]interface{} {
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		name := ExtractToolName(t)
		if !IsDisabledExternalCodingSessionTool(name) && !CodingSessionToolNames[name] {
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
	"ssh":               true,
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
	"screenshot":            true,
	"create_scheduled_task": true, "list_scheduled_tasks": true,
	"delete_scheduled_task": true, "update_scheduled_task": true,
	"search_and_install_skill": true,
	"switch_llm_provider":      true,
	"manage_config":            true,
	"manage_template":          true,
	"manage_schedule":          true,
	"im_message":               true,
	"query_audit_log":          true,
	"session_search":           true,
	"office":                   true,
	"mis_data":                 true,
	// Browser automation: unified "browser" tool only. Individual browser_*
	// handlers are internal dispatch targets and should not be routed directly.
	"browser": true,
	// Knowledge tools (registered via CoreToolDeps.ExtraHandlers).
	"knowledge_search":           true,
	"knowledge_context_pack":     true,
	"knowledge_save_text":        true,
	"knowledge_save_url":         true,
	"knowledge_import_directory": true,
	"knowledge_import_files":     true,
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
	Name         string
	Triggers     []string
	Description  string
	Capabilities []string

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
	return r.skillMatchScoreWithCapabilityConstraint(userMessage, nil, false)
}

func (r *Router) skillMatchScoreWithCapabilityConstraint(userMessage string, requiredCapabilities []string, constrained bool) (float64, []string) {
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

	if constrained {
		filtered := sorted[:0]
		for _, item := range sorted {
			if skillMatchesCapabilityConstraint(r.skillProvider, item.name, requiredCapabilities) {
				filtered = append(filtered, item)
			}
		}
		sorted = filtered
		if len(sorted) == 0 {
			return 0, nil
		}
	}

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

func (r *Router) matchedSkillCapabilities(matchedSkills []string) []string {
	return matchedSkillCapabilitiesFromProvider(r.skillProvider, matchedSkills)
}

func (r *Router) enrichMatchedSkillTool(defs []map[string]interface{}, matchedSkills, skillCapabilities []string) []map[string]interface{} {
	if len(matchedSkills) == 0 {
		return defs
	}
	for i, def := range defs {
		if ExtractToolName(def) == "manage_skill" {
			out := append([]map[string]interface{}(nil), defs...)
			out[i] = enrichRunSkillDescription(def, matchedSkills, skillCapabilities)
			return out
		}
	}
	return defs
}

func matchedSkillCapabilitiesFromProvider(provider SkillProvider, matchedSkills []string) []string {
	if provider == nil || len(matchedSkills) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(matchedSkills))
	for _, name := range matchedSkills {
		key := strings.ToLower(strings.TrimSpace(name))
		if key != "" {
			wanted[key] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	seen := map[string]bool{"skill": true}
	caps := []string{"skill"}
	for _, skill := range provider.ListActiveSkills() {
		if !wanted[strings.ToLower(strings.TrimSpace(skill.Name))] {
			continue
		}
		for _, cap := range skill.Capabilities {
			cap = normalizeSkillCapability(cap)
			if cap == "" || seen[cap] {
				continue
			}
			seen[cap] = true
			caps = append(caps, cap)
		}
	}
	return caps
}

func skillMatchesCapabilityConstraint(provider SkillProvider, skillName string, requiredCapabilities []string) bool {
	if provider == nil {
		return true
	}
	required := make(map[string]bool, len(requiredCapabilities))
	for _, cap := range requiredCapabilities {
		cap = normalizeSkillCapability(cap)
		if cap != "" {
			required[cap] = true
		}
	}
	if len(required) == 0 {
		return false
	}
	for _, skill := range provider.ListActiveSkills() {
		if skill.Name != skillName {
			continue
		}
		caps := skill.Capabilities
		if len(caps) == 0 {
			caps = []string{"skill"}
		}
		for _, cap := range caps {
			if required[normalizeSkillCapability(cap)] {
				return true
			}
		}
		return false
	}
	return true
}

func skillCapabilityConstraintForUIC(result intent.ClassificationResult) ([]string, bool) {
	if !uicResultUsableForToolActivation(result) || result.Confidence < 0.50 {
		return nil, false
	}
	switch result.Primary {
	case intent.LabelLiveData:
		return []string{"current_data", "weather", "finance", "time", "web"}, true
	case intent.LabelCurrentTime:
		return []string{"time"}, true
	case intent.LabelSearch:
		return []string{"web", "search"}, true
	case intent.LabelSSH:
		return []string{"ssh", "remote", "server"}, true
	case intent.LabelBrowser:
		return []string{"browser", "browser_automation", "web"}, true
	case intent.LabelBusinessData:
		return []string{"business_data", "mis", "data"}, true
	case intent.LabelOffice:
		return []string{"office", "document"}, true
	case intent.LabelDocumentDelivery:
		return []string{"document", "file", "delivery", "send"}, true
	case intent.LabelKnowledgeWrite:
		return []string{"knowledge", "memory"}, true
	case intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance:
		return []string{"coding", "code", "development"}, true
	default:
		return nil, false
	}
}

func normalizeSkillCapability(cap string) string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(cap)), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
	return strings.Join(parts, "_")
}

// IsLocalStoredInfoQuery reports whether the user is asking what the agent
// already knows/remembers from local memory or the knowledge base, especially
// for operational details like servers, environments, credentials, and configs.
func IsLocalStoredInfoQuery(userMessage string) bool {
	text := strings.ToLower(strings.TrimSpace(userMessage))
	if text == "" {
		return false
	}
	hasLookupIntent := strings.Contains(text, "\u77e5\u9053") ||
		strings.Contains(text, "\u8bb0\u5f97") ||
		strings.Contains(text, "\u6709\u6ca1\u6709") ||
		strings.Contains(text, "\u662f\u5426") ||
		strings.Contains(text, "\u67e5\u4e00\u4e0b") ||
		strings.Contains(text, "\u67e5\u8be2") ||
		strings.Contains(text, "what do you remember") ||
		strings.Contains(text, "do you know") ||
		strings.Contains(text, "use the saved")
	if !hasLookupIntent {
		return false
	}
	hasLocalSubject := strings.Contains(text, "\u670d\u52a1\u5668") ||
		containsASCIIToken(text, "server") ||
		strings.Contains(text, "\u4e3b\u673a") ||
		strings.Contains(text, "\u73af\u5883") ||
		containsASCIIToken(text, "env") ||
		strings.Contains(text, "\u914d\u7f6e") ||
		containsASCIIToken(text, "config") ||
		strings.Contains(text, "\u8d26\u53f7") ||
		strings.Contains(text, "\u8d26\u6237") ||
		strings.Contains(text, "\u5bc6\u7801") ||
		strings.Contains(text, "\u51ed\u636e") ||
		containsASCIIToken(text, "credential") ||
		containsASCIIToken(text, "credentials") ||
		containsASCIIToken(text, "endpoint") ||
		isNamedLocalAPIReference(text) ||
		strings.Contains(text, "local corpus") ||
		strings.Contains(text, "saved corpus") ||
		containsASCIIToken(text, "document") ||
		containsASCIIToken(text, "docs") ||
		strings.Contains(text, "\u8d44\u6599") ||
		strings.Contains(text, "\u6587\u6863")
	if hasLocalSubject {
		return true
	}
	return strings.Contains(text, "\u77e5\u8bc6\u5e93") ||
		strings.Contains(text, "\u8bb0\u5fc6") ||
		containsASCIIToken(text, "memory") ||
		containsASCIIToken(text, "knowledge")
}

func containsASCIIToken(text, token string) bool {
	if token == "" {
		return false
	}
	for start := 0; ; {
		idx := strings.Index(text[start:], token)
		if idx < 0 {
			return false
		}
		pos := start + idx
		beforeOK := pos == 0 || !isASCIIAlphaNum(text[pos-1])
		after := pos + len(token)
		afterOK := after >= len(text) || !isASCIIAlphaNum(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = pos + 1
	}
}

func isASCIIAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func isNamedLocalAPIReference(text string) bool {
	for i := 0; i+3 < len(text); i++ {
		if text[i] == 'a' && text[i+1] == 'p' && text[i+2] == 'i' && text[i+3] >= '0' && text[i+3] <= '9' {
			return true
		}
	}
	return false
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
	if !ShouldPinConditionalTool(name) {
		return
	}
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

// SessionPinnedToolsMissing returns session-pinned tool names that are NOT
// in the provided currentNames set. This is used by the agent loop to detect
// tools that were session-pinned mid-loop (e.g. by discover_tool) but are
// not yet in the LLM's tool definition list.
func (r *Router) SessionPinnedToolsMissing(currentNames map[string]bool) []string {
	if len(r.sessionTools) == 0 {
		return nil
	}
	var missing []string
	for name := range r.sessionTools {
		if !currentNames[name] {
			missing = append(missing, name)
		}
	}
	return missing
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

func narrowKnowledgeWriteTools(condKeep map[string]bool, userMessage string) map[string]bool {
	if condKeep == nil {
		condKeep = make(map[string]bool)
	}
	for _, name := range knowledgeWriteToolList {
		delete(condKeep, name)
	}
	for name := range knowledgeWriteToolsForPayload(userMessage) {
		condKeep[name] = true
	}
	return condKeep
}

func knowledgeWriteToolsForPayload(userMessage string) map[string]bool {
	tools := map[string]bool{}
	if knowledgePayloadHasURL(userMessage) {
		tools["knowledge_save_url"] = true
		tools["knowledge_save_urls"] = true
	}
	paths := knowledgePayloadLocalPaths(userMessage)
	if len(paths) == 0 {
		if len(tools) > 0 {
			return tools
		}
		return map[string]bool{"knowledge_save_text": true}
	}
	hasFile := false
	hasDir := false
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			if info.IsDir() {
				hasDir = true
			} else {
				hasFile = true
			}
			continue
		}
		if filepath.Ext(p) == "" {
			hasDir = true
		} else {
			hasFile = true
		}
	}
	if hasFile {
		tools["knowledge_import_files"] = true
	}
	if hasDir {
		tools["knowledge_import_directory"] = true
	}
	if len(tools) == 0 {
		tools["knowledge_save_text"] = true
	}
	return tools
}

func knowledgePayloadHasURL(text string) bool {
	for _, field := range strings.Fields(text) {
		trimmed := trimKnowledgePayloadToken(field)
		u, err := url.Parse(trimmed)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return true
		}
	}
	return false
}

func knowledgePayloadLocalPaths(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = trimKnowledgePayloadToken(p)
		if p == "" || seen[p] || !isLocalPathToken(p) {
			return
		}
		for i := 0; i < len(out); i++ {
			existing := out[i]
			if isPathPrefixToken(p, existing) {
				return
			}
			if isPathPrefixToken(existing, p) {
				delete(seen, existing)
				out = append(out[:i], out[i+1:]...)
				i--
			}
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range windowsKnownFilePathPattern.FindAllString(text, -1) {
		add(p)
	}
	for _, raw := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t'
	}) {
		line := strings.TrimSpace(raw)
		if p := trimKnowledgePayloadToken(line); isLocalPathToken(p) {
			add(p)
			continue
		}
		for _, token := range strings.Fields(line) {
			add(token)
		}
	}
	for _, p := range windowsLocalPathPattern.FindAllString(text, -1) {
		add(p)
	}
	return out
}

func isPathPrefixToken(prefix, full string) bool {
	if len(prefix) >= len(full) || !strings.HasPrefix(full, prefix) {
		return false
	}
	if strings.HasSuffix(prefix, `\`) || strings.HasSuffix(prefix, `/`) {
		return true
	}
	next := full[len(prefix)]
	return next == '\\' || next == '/' || unicode.IsSpace(rune(next))
}

var windowsKnownFilePathPattern = regexp.MustCompile(`[A-Za-z]:[\\/][^\r\n<>|"',\x{ff0c}\x{3002}\x{ff1b};]+?\.(?i:pdf|docx?|xlsx?|pptx?|txt|md|csv|json|html?|xml|rtf|png|jpe?g|webp|gif|zip|7z|rar)`)

var windowsLocalPathPattern = regexp.MustCompile(`[A-Za-z]:[\\/][^\s<>|"',\x{ff0c}\x{3002}\x{ff1b};]+`)

func trimKnowledgePayloadToken(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case '<', '>', '[', ']', '(', ')', '{', '}', '"', '\'', '`', ',', '.', ';', ':', '\uff0c', '\u3002', '\uff1b', '\uff1a':
			return true
		default:
			return false
		}
	})
}

func isLocalPathToken(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "://") {
		return false
	}
	if filepath.IsAbs(s) || filepath.VolumeName(s) != "" {
		return true
	}
	return strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, `.\`) || strings.HasPrefix(s, `..\`)
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

// noEagerPinTools lists conditional tools that should NOT be pinned from
// indirect context, but SHOULD be pinned after actual successful use (via
// ActivateSessionTool in the tool execution path).
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

func uicResultUsableForToolActivation(result intent.ClassificationResult) bool {
	return result.Primary != intent.LabelUnknown && result.Primary != intent.LabelAmbiguous
}

func uicToolNameActivatable(name string) bool {
	return allConditionalKeepTools[name] || knowledgeWriteToolNames[name]
}

func coreRoutePriority(name string, condKeep, sessionTools, mustKeep map[string]bool) int {
	if mustKeep[name] {
		return -1
	}
	if condKeep[name] {
		return 0
	}
	switch name {
	case "bash", "read_file", "FileRead", "ripgrep", "Glob", "write_file", "edit_file", "list_directory":
		return 1
	}
	if sessionTools[name] {
		return 2
	}
	switch name {
	case "task", "async_wait", "compress_context", "memory":
		return 3
	case "list_sessions", "get_session_output", "get_session_events",
		// Prefer keeping recording surface when core set is trimmed for budget.
		"record_audio":
		return 4
	case "screenshot", "web_fetch", "set_nickname", "tts", "asr":
		return 5
	case "manage_skill", "discover_tool", "call_mcp_tool":
		return 6
	default:
		return 7
	}
}

func trimCoreToolsToBudget(core []map[string]interface{}, condKeep, sessionTools, mustKeep map[string]bool) []map[string]interface{} {
	if len(core) <= MaxToolBudget && len(mustKeep) == 0 {
		return core
	}
	trimmed := append([]map[string]interface{}(nil), core...)
	sort.SliceStable(trimmed, func(i, j int) bool {
		ni := ExtractToolName(trimmed[i])
		nj := ExtractToolName(trimmed[j])
		pi := coreRoutePriority(ni, condKeep, sessionTools, mustKeep)
		pj := coreRoutePriority(nj, condKeep, sessionTools, mustKeep)
		if pi != pj {
			return pi < pj
		}
		return ni < nj
	})
	if len(trimmed) <= MaxToolBudget {
		return trimmed
	}
	return trimmed[:MaxToolBudget]
}

// Route selects the most relevant tools for userMessage from allTools.
func (r *Router) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	return r.RouteWithOptions(userMessage, allTools, RouteOptions{})
}

// RouteWithOptions is Route plus optional intent rewrite pins / search query.
func (r *Router) RouteWithOptions(userMessage string, allTools []map[string]interface{}, opts RouteOptions) []map[string]interface{} {
	var condKeep map[string]bool
	var condFilterOut map[string]bool
	var cachedICResult *IntentResult
	var skillRequiredCapabilities []string
	var skillCapabilityConstrained bool
	suppressedTools := map[string]bool{}
	skillInstallEligible := true
	localStoredInfoQuery := IsLocalStoredInfoQuery(userMessage)
	routeIntent := opts.Intent
	if routeIntent != nil && !routeIntent.Usable() {
		routeIntent = nil
	}
	availableNames := availableToolNameSet(allTools)
	searchQuery := userMessage
	if routeIntent != nil {
		searchQuery = routeIntent.SearchQuery(userMessage)
	}

	if r.unifiedClassifier != nil {
		// UIC path: use UnifiedIntentClassifier to determine which conditional
		// tools to keep, replacing local matchConditionalKeepRules.
		condKeep = make(map[string]bool)
		condFilterOut = make(map[string]bool)

		uicResult := r.unifiedClassifier.Classify(intent.MessageContext{Text: userMessage})
		skillInstallEligible = uicSkillInstallEligible(uicResult)
		skillRequiredCapabilities, skillCapabilityConstrained = skillCapabilityConstraintForUIC(uicResult)

		// UIC activation: UIC returns a concrete, non-ambiguous top
		// intent, including degraded embedding-only fusion results. Activate that
		// intent's ToolNames so the LLM can see and
		// call them. This uses a LOW threshold because the cost of a false
		// positive is small (an extra tool definition in context) while the
		// cost of a false negative is high (LLM cannot perform the task and
		// falls back to dangerous workarounds like raw ssh via bash).
		//
		// The old design used a single 0.90 threshold for activation. Fusion-
		// ambiguous results (e.g. ssh 0.695 vs search 0.676)
		// never reach 0.90, causing the correct top-intent's tools to be
		// completely hidden from the LLM.
		const uicActivationThreshold = 0.50

		// A degraded UIC result can still be actionable: in practice the tree
		// channel may time out while the embedding channel returns a confident
		// concrete intent. Do not hide first-class conditional tools in that
		// case; tool availability must follow the current intent, not stale
		// experience or an auxiliary classifier outage.
		uicUsable := uicResultUsableForToolActivation(uicResult)

		uicKnowledgeWrite := uicUsable && uicResult.Primary == intent.LabelKnowledgeWrite && uicResult.Confidence >= uicActivationThreshold

		if uicUsable && uicResult.Confidence >= uicActivationThreshold && len(uicResult.ToolNames) > 0 {
			for _, toolName := range uicResult.ToolNames {
				if !uicToolNameActivatable(toolName) {
					continue
				}
				condKeep[toolName] = true
			}
		}
		if uicKnowledgeWrite {
			condKeep = narrowKnowledgeWriteTools(condKeep, userMessage)
			for name := range knowledgeWriteToolNames {
				if !condKeep[name] {
					suppressedTools[name] = true
				}
			}
			suppressedTools["memory"] = true
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
			skillInstallEligible = intentClassifierSkillInstallEligible(result)
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
				// Fallback semantic promotion uses a single high-confidence threshold
				// for all conditional tool groups and only affects this route call.
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
	browserPublishAffordance := intent.BrowserPublicationAffordance(userMessage)
	skillUploadRequest := isSkillUploadRequest(userMessage)
	if browserPublishAffordance {
		condKeep["browser"] = true
		delete(condFilterOut, "browser")
	}
	if skillUploadRequest {
		skillInstallEligible = false
		suppressedTools["search_and_install_skill"] = true
		delete(suppressedTools, "manage_skill")
	}
	if localStoredInfoQuery {
		condKeep["knowledge_search"] = true
		condKeep["knowledge_context_pack"] = true
		for name := range knowledgeWriteToolNames {
			suppressedTools[name] = true
		}
	}
	if !skillInstallEligible {
		suppressedTools["search_and_install_skill"] = true
	}

	if condKeep["ssh"] {
		// SSH is a first-class builtin execution surface. When it is selected,
		// hide generic fallback/discovery surfaces so the model cannot route the
		// same action through call_mcp_tool(server_id="ssh", tool_name="ssh") or
		// install a community SSH skill instead of using the builtin.
		suppressedTools["call_mcp_tool"] = true
		suppressedTools["manage_skill"] = true
		suppressedTools["discover_tool"] = true
		suppressedTools["search_and_install_skill"] = true
	} else if r.sessionTools["ssh"] {
		// Preserve the older gateway guard for follow-up messages where ssh was
		// session-pinned, without hiding skill/discovery tools for a possible
		// topic change in the same conversation.
		suppressedTools["call_mcp_tool"] = true
	}
	explicitScreenshotRequest := isExplicitScreenshotRequest(userMessage)
	explicitRecordAudioRequest := isExplicitRecordAudioRequest(userMessage)
	explicitGitRequest := isExplicitGitRequest(userMessage)
	// Explicit start-recording intents must never lose record_audio to budget
	// pressure (also in CoreToolNames; pin keeps session continuity if core set changes).
	if explicitRecordAudioRequest {
		condKeep["record_audio"] = true
		delete(condFilterOut, "record_audio")
		delete(suppressedTools, "record_audio")
	}
	// Explicit screenshot/git must be pinned: with score-floor candidate selection
	// they can otherwise lose remaining slots to noise or land at score 0.
	if explicitScreenshotRequest {
		condKeep["screenshot"] = true
		delete(condFilterOut, "screenshot")
		delete(suppressedTools, "screenshot")
	}
	if explicitGitRequest {
		condKeep["git_commit"] = true
		condKeep["git_push"] = true
		delete(condFilterOut, "git_commit")
		delete(condFilterOut, "git_push")
		delete(suppressedTools, "git_commit")
		delete(suppressedTools, "git_push")
	}
	// LLM / structured intent rewrite: pin families and suppress excludes.
	if routeIntent != nil {
		intentPins := routeIntent.ExpandPins(availableNames)
		intentExcludes := routeIntent.ExpandExcludes(availableNames)
		for _, name := range intentPins {
			condKeep[name] = true
			delete(condFilterOut, name)
			delete(suppressedTools, name)
		}
		for _, name := range intentExcludes {
			// Do not suppress something we just pinned for this turn.
			if condKeep[name] {
				continue
			}
			suppressedTools[name] = true
			delete(condKeep, name)
			condFilterOut[name] = true
		}
		if routeIntent.QueryForRoute != "" || len(intentPins) > 0 {
			q := routeIntent.QueryForRoute
			if rs := []rune(q); len(rs) > 80 {
				q = string(rs[:80]) + "..."
			}
			log.Printf("[RouteIntent] intent=%q confidence=%.2f query=%q pins=%v excludes=%v families=%v",
				routeIntent.Intent, routeIntent.Confidence, q, intentPins, intentExcludes, routeIntent.ToolFamilies)
		}
	}
	browserSessionActive := condKeep["browser"] || r.sessionTools["browser"]

	// When the user explicitly asks for a desktop screenshot and browser was
	// only activated by UIC misclassification (not by session pin, not by
	// browserPublishAffordance, not by the message also mentioning browser/web
	// context), demote browser activation to avoid confusing the LLM with an
	// irrelevant browser tool alongside the correct screenshot tool.
	//
	// Exception: if the message also contains browser-context words (浏览器,
	// chrome, 网页, 页面, web page, etc.), the screenshot is likely a browser
	// screenshot request, so browser should remain active.
	if explicitScreenshotRequest && condKeep["browser"] && !r.sessionTools["browser"] && !browserPublishAffordance && !messageHasBrowserContext(userMessage) {
		delete(condKeep, "browser")
		condFilterOut["browser"] = true
		browserSessionActive = false
	}

	if browserSessionActive {
		// Browser tasks must use the stable merged browser surface. Hide generic
		// desktop screenshot, shell, skill/discovery, MCP, and git fallbacks so the
		// model cannot drift into screen scraping, taskkill, raw authenticated HTTP
		// calls, skill reruns, or source-control actions instead of page actions.
		suppressedTools["bash"] = true
		if !explicitScreenshotRequest {
			suppressedTools["screenshot"] = true
		}
		suppressedTools["call_mcp_tool"] = true
		suppressedTools["manage_skill"] = true
		suppressedTools["discover_tool"] = true
		suppressedTools["search_and_install_skill"] = true
		suppressedTools["git_commit"] = true
		suppressedTools["git_push"] = true
		suppressedTools["passthrough_task"] = true
		suppressedTools["list_mcp_tools"] = true
	}
	if !explicitScreenshotRequest {
		suppressedTools["screenshot"] = true
	}
	if !isExplicitGitRequest(userMessage) {
		suppressedTools["git_commit"] = true
		suppressedTools["git_push"] = true
	}

	// Compute skill match before generic fallback suppression so a concrete
	// matched skill is not removed by broad SSH/browser fallback guards.
	var skillScore float64
	var matchedSkills []string
	if r.skillProvider != nil {
		skillScore, matchedSkills = r.skillMatchScoreWithCapabilityConstraint(userMessage, skillRequiredCapabilities, skillCapabilityConstrained)
		if len(matchedSkills) > 0 {
			availableTools := buildAvailableToolsMap(allTools)
			matchedSkills = r.filterSkillsByConditions(matchedSkills, availableTools)
			if len(matchedSkills) == 0 {
				skillScore = 0
			}
		}
	}
	hasMatchedSkill := len(matchedSkills) > 0

	if condKeep["ssh"] && hasMatchedSkill {
		delete(suppressedTools, "manage_skill")
	}
	if browserSessionActive && hasMatchedSkill {
		delete(suppressedTools, "manage_skill")
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
		if suppressedTools[name] {
			continue
		}
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

	// Skill match has already been computed before suppression so manage_skill's
	// execution contract does not depend on dynamic candidate routing being active.
	mustKeepCore := map[string]bool{}
	if len(matchedSkills) > 0 || skillUploadRequest {
		mustKeepCore["manage_skill"] = true
	}
	matchedSkillCapabilities := r.matchedSkillCapabilities(matchedSkills)
	core = trimCoreToolsToBudget(core, condKeep, r.sessionTools, mustKeepCore)
	core = r.enrichMatchedSkillTool(core, matchedSkills, matchedSkillCapabilities)
	remainingSlots := MaxToolBudget - len(core)
	if len(candidates) == 0 || remainingSlots <= 0 {
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

	// Tokenize retrieval query (rewritten intent when present). Experience
	// signals still use the original user tokens so personal patterns stick.
	queryTokens := bm25.Tokenize(searchQuery)
	userTokens := bm25.Tokenize(userMessage)
	scores := r.bm25Index.ScoreWithTokens(queryTokens)

	// Fuse with vector scores when hybrid retrieval is active.
	if r.hybrid != nil {
		scores = r.hybrid.FuseScores(searchQuery, scores, embeddingTexts)

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
	normScores := minMaxNormalize(scores)

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
			expScore = r.tracker.ExperienceScore(name, userTokens)
		}
		var outcomeScore float64
		if r.tracker != nil {
			outcomeScore = r.tracker.ContextOutcomeScore(name, userTokens)
		}
		var priorityBonus float64
		if r.registry != nil {
			if t, ok := r.registry.Get(name); ok {
				priorityBonus = clampFloat(float64(t.Priority)*0.1, 0, 1)
			}
		}
		var routingHintAdjustment float64
		if r.tracker != nil {
			routingHintAdjustment = r.tracker.RoutingHintAdjustment(name, userTokens)
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

	// Rerank top candidates when reranker is configured and candidates exceed
	// the remaining non-core slots. Core/session/conditional tools consume part
	// of MaxToolBudget before candidate routing starts.
	var rerankerResult []string
	if r.reranker != nil && len(scoredList) > remainingSlots {
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

		// Prefer rewritten search query so listwise rerank aligns with hybrid retrieval.
		reranked, err := r.reranker.Rerank(searchQuery, summaries, 5)
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
	resultNames := make(map[string]bool, MaxToolBudget+2)
	for _, t := range result {
		resultNames[ExtractToolName(t)] = true
	}
	rerankedKeep := make(map[string]bool, len(rerankerResult))
	for _, name := range rerankerResult {
		rerankedKeep[name] = true
	}

	for _, s := range scoredList {
		if len(result) >= MaxToolBudget {
			break
		}
		name := candidateNames[s.index]
		// Do not pad the budget with zero-score noise (e.g. computer_* at 0),
		// unless the listwise reranker explicitly promoted the tool.
		if s.score < MinCandidateRouteScore && !rerankedKeep[name] {
			continue
		}
		if suppressedTools[name] {
			continue
		}
		if !r.isBuiltin(name) {
			dynamicCount++
			if dynamicCount > MaxDynamicRouted {
				continue
			}
		}
		result = append(result, candidates[s.index])
		resultNames[name] = true
	}

	if r.recommender != nil && skillInstallEligible {
		if hint := r.matchRecommendations(userTokens); hint != nil {
			if name := ExtractToolName(hint); !resultNames[name] && !suppressedTools[name] {
				result = append(result, hint)
				resultNames[name] = true
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

	suppressedNames := make([]string, 0, len(suppressedTools))
	for name, suppressed := range suppressedTools {
		if suppressed {
			suppressedNames = append(suppressedNames, name)
		}
	}
	sort.Strings(suppressedNames)

	go writeRouteLog(userMessage, len(allTools), len(core), len(candidates), r.hybrid != nil, bodyAware, rankedNames, rankedScores, rankedRoutingHintAdjustments, selectedNames, suppressedNames, browserPublishAffordance, explicitScreenshotRequest, rerankerResult, skillScore, matchedSkills, matchedSkillCapabilities, skillCapabilityConstrained, skillRequiredCapabilities)

	return result
}

func uicSkillInstallEligible(result intent.ClassificationResult) bool {
	switch result.Primary {
	case intent.LabelSSH, intent.LabelBrowser, intent.LabelSearch, intent.LabelLiveData,
		intent.LabelNonCoding, intent.LabelCurrentTime, intent.LabelContinuation,
		intent.LabelMaintenance, intent.LabelBugFix,
		intent.LabelAmbiguous, intent.LabelUnknown:
		return false
	default:
		return true
	}
}

func intentClassifierSkillInstallEligible(result IntentResult) bool {
	switch result.Intent {
	case IntentSSH, IntentBrowser, IntentQuery, IntentShortCommand, IntentChat, IntentUnknown:
		return false
	default:
		return true
	}
}

func isExplicitScreenshotRequest(userMessage string) bool {
	msg := strings.ToLower(strings.TrimSpace(userMessage))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "screenshot") || strings.Contains(msg, "screen shot") {
		return true
	}
	for _, marker := range []string{"\u622a\u56fe", "\u622a\u5c4f", "\u5c4f\u5e55\u622a\u56fe", "\u684c\u9762\u622a\u56fe", "\u622a\u4e2a\u56fe", "\u622a\u4e00\u4e0b\u56fe"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// recordingStopMarkers are unambiguous stop/cancel/refuse phrases. Checked
// before start markers so "不要帮我录音" (contains "帮我录音") is not treated
// as a start request.
var recordingStopMarkers = []string{
	"停止录音", "停止錄音", "停止录制", "停止錄製",
	"结束录音", "結束錄音", "结束录制", "結束錄製",
	"取消录音", "取消錄音", "取消录制", "取消錄製",
	"关闭录音", "關閉錄音", "关掉录音", "關掉錄音",
	"不要录音", "不要錄音", "别录音", "別錄音", "别录了", "別錄了",
	"不要帮我录音", "不要幫我錄音", "别帮我录音", "別幫我錄音",
	"不用录音", "不用錄音", "无需录音", "無需錄音",
	"stop recording", "cancel recording", "end recording", "stop the recording",
	"don't record", "do not record", "dont record",
}

// recordingStartMarkers are strong start-recording phrases (substring match).
var recordingStartMarkers = []string{
	"record_audio",
	"start recording", "start a recording", "begin recording",
	"record the meeting", "record meeting", "meeting recording",
	"record this meeting", "long-form recording", "long form recording",
	"open the recorder", "open recorder", "start the recorder",
	"会议录音", "開始錄音", "开始录音", "打开录音", "打開錄音",
	"开始录制", "開始錄製", "打开录制", "打開錄製",
	"录一下", "錄一下", "帮我录音", "幫我錄音", "给我录音", "給我錄音",
	"现场录音", "現場錄音", "长时录音", "長時錄音", "长时录制", "長時錄製",
	"讨论录制", "討論錄製", "访谈录制", "訪談錄製", "访谈录音", "訪談錄音",
	"录个音", "錄個音", "录制会议", "錄製會議", "录音会议", "錄音會議",
}

// isExplicitRecordAudioRequest detects clear user intent to START interactive
// long-form / meeting recording (not "transcribe an existing audio file",
// and not stop/cancel recording).
func isExplicitRecordAudioRequest(userMessage string) bool {
	msg := strings.ToLower(strings.TrimSpace(userMessage))
	if msg == "" {
		return false
	}
	// Hard stop/cancel always wins (even if a start substring is nested).
	if looksLikeHardStopRecordingRequest(msg) {
		return false
	}
	// Strong start phrases win over co-occurring 转写/路径 talk
	// (e.g. "不要转写，开始录音"), but not over "整理会议录音纪要".
	if hasStrongStartRecordingPhrase(msg) && !looksLikeProcessExistingMeetingRecording(msg) {
		return true
	}
	if looksLikeStopOrNegateRecordingRequest(msg) ||
		looksLikeExistingAudioTranscriptionRequest(msg) ||
		looksLikeProcessExistingMeetingRecording(msg) {
		return false
	}
	// Bare "录音" / "录制" only for short messages (e.g. "录音", "帮我录一下"),
	// not long narrative that merely mentions recording as a topic.
	runeCount := len([]rune(msg))
	if runeCount > 0 && runeCount <= 16 &&
		(strings.Contains(msg, "录音") || strings.Contains(msg, "錄音") ||
			strings.Contains(msg, "录制") || strings.Contains(msg, "錄製") ||
			containsASCIIToken(msg, "record") || containsASCIIToken(msg, "recording")) {
		return true
	}
	return false
}

func hasStrongStartRecordingPhrase(msg string) bool {
	for _, marker := range recordingStartMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// looksLikeHardStopRecordingRequest matches unambiguous stop/cancel/refuse phrases.
func looksLikeHardStopRecordingRequest(msg string) bool {
	for _, marker := range recordingStopMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// looksLikeExistingAudioTranscriptionRequest is true when the user is asking to
// transcribe/process an existing audio file rather than start a new recording.
func looksLikeExistingAudioTranscriptionRequest(msg string) bool {
	for _, marker := range []string{
		"转写", "轉寫", "转录", "轉錄", "asr", "whisper",
		"音频文件", "音頻文件", "录音文件", "錄音文件",
		".wav", ".mp3", ".m4a", ".ogg", ".opus", ".silk", ".aac",
		"path=", "路径", "路徑",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// looksLikeProcessExistingMeetingRecording is true when the user wants to
// process/summarize an existing meeting recording, not open the mic UI.
// Example: "把会议录音整理成纪要" (contains "会议录音" but is not a start intent).
func looksLikeProcessExistingMeetingRecording(msg string) bool {
	hasMeetingAudio := strings.Contains(msg, "会议录音") || strings.Contains(msg, "會議錄音") ||
		strings.Contains(msg, "meeting recording") || strings.Contains(msg, "recording of the meeting")
	if !hasMeetingAudio {
		return false
	}
	// Explicit start verbs still mean "start recording (and maybe summarize later)".
	for _, start := range []string{
		"开始", "開始", "打开", "打開", "帮我录", "幫我錄", "给我录", "給我錄",
		"start ", "begin ", "open ",
	} {
		if strings.Contains(msg, start) {
			return false
		}
	}
	for _, process := range []string{
		"整理", "纪要", "紀要", "总结", "總結", "摘要", "转成", "轉成",
		"写成", "寫成", "生成", "summar", "minutes", "notes",
	} {
		if strings.Contains(msg, process) {
			return true
		}
	}
	return false
}

// looksLikeStopOrNegateRecordingRequest is true when the user is stopping,
// cancelling, or refusing recording — not asking to start it.
func looksLikeStopOrNegateRecordingRequest(msg string) bool {
	if looksLikeHardStopRecordingRequest(msg) {
		return true
	}
	// Soft negate only for short messages ("不要录音啊") so we do not kill
	// compound intents like "不要转写，开始录音".
	runeCount := len([]rune(msg))
	if runeCount > 0 && runeCount <= 20 &&
		(strings.Contains(msg, "不要") || strings.Contains(msg, "别") || strings.Contains(msg, "別") ||
			strings.Contains(msg, "不用") || strings.Contains(msg, "无需") || strings.Contains(msg, "無需")) &&
		(strings.Contains(msg, "录音") || strings.Contains(msg, "錄音") ||
			strings.Contains(msg, "录制") || strings.Contains(msg, "錄製")) {
		return true
	}
	return false
}

func isExplicitGitRequest(userMessage string) bool {
	msg := strings.ToLower(strings.TrimSpace(userMessage))
	if msg == "" {
		return false
	}
	for _, marker := range []string{
		"git", "commit", "push", "pull request", "branch", "repository",
		"\u4ee3\u7801", "\u4ed3\u5e93", "\u63d0\u4ea4\u4ee3\u7801", "\u63a8\u9001", "\u5206\u652f", "\u5408\u5e76\u8bf7\u6c42",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// messageHasBrowserContext returns true if the message mentions browser/web
// context words, indicating the user wants a browser-based action (like a
// webpage screenshot) rather than a desktop screenshot.
func messageHasBrowserContext(userMessage string) bool {
	msg := strings.ToLower(strings.TrimSpace(userMessage))
	for _, marker := range []string{
		"\u6d4f\u89c8\u5668", "\u7f51\u9875", "\u9875\u9762", "chrome", "firefox", "safari",
		"playwright", "web page", "webpage", "browser", "\u7f51\u7ad9", "url",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func isSkillUploadRequest(userMessage string) bool {
	msg := strings.ToLower(strings.TrimSpace(userMessage))
	if msg == "" {
		return false
	}
	hasSkill := false
	for _, marker := range []string{"\u6280\u80fd", "\u80fd\u529b"} {
		if strings.Contains(msg, marker) {
			hasSkill = true
			break
		}
	}
	if !hasSkill {
		hasSkill = containsASCIIToken(msg, "skill") ||
			strings.Contains(msg, "skillmarket") ||
			strings.Contains(msg, "skill market") ||
			strings.Contains(msg, "skillhub")
	}
	if !hasSkill {
		return false
	}
	hasUploadVerb := false
	for _, marker := range []string{"\u4e0a\u4f20", "\u767c\u5e03", "\u53d1\u5e03", "\u4e0a\u67b6", "\u63d0\u4ea4"} {
		if strings.Contains(msg, marker) {
			hasUploadVerb = true
			break
		}
	}
	if !hasUploadVerb {
		for _, marker := range []string{"upload", "publish", "submit"} {
			if containsASCIIToken(msg, marker) {
				hasUploadVerb = true
				break
			}
		}
	}
	if !hasUploadVerb {
		return false
	}
	for _, marker := range []string{"skillmarket", "skill market", "hubcenter", "hub center", "\u80fd\u529b\u5e02\u573a", "\u80fd\u529b\u5e02\u5834", "skillhub"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	if containsASCIIToken(msg, "hub") {
		return true
	}
	return strings.Contains(msg, "\u4e0a\u4f20") || strings.Contains(msg, "\u53d1\u5e03") || strings.Contains(msg, "\u767c\u5e03") || strings.Contains(msg, "\u4e0a\u67b6")
}

var knowledgeWriteToolList = []string{
	"knowledge_save_text",
	"knowledge_save_url",
	"knowledge_save_urls",
	"knowledge_import_files",
	"knowledge_import_directory",
}

var knowledgeWriteToolNames = map[string]bool{
	"knowledge_save_text":        true,
	"knowledge_save_url":         true,
	"knowledge_save_urls":        true,
	"knowledge_import_files":     true,
	"knowledge_import_directory": true,
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
func enrichRunSkillDescription(def map[string]interface{}, skillNames, skillCapabilities []string) map[string]interface{} {
	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		return def
	}
	desc, _ := fn["description"].(string)
	suffix := " \u53ef\u7528 Skill: " + strings.Join(skillNames, ", ") + ". Matched local Skill(s): " + strings.Join(skillNames, ", ") + `. Prefer manage_skill(action="run", name=<matched skill>) before generic fallback tools when the user request matches one.`
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
	if len(skillCapabilities) > 0 {
		newDef["x_execution_contract"] = mergeSkillExecutionContract(def["x_execution_contract"], skillCapabilities)
	}
	return newDef
}

func mergeSkillExecutionContract(existing interface{}, skillCapabilities []string) map[string]interface{} {
	contract := map[string]interface{}{
		"deterministic":           false,
		"supports_direct":         false,
		"requires_agent_planning": false,
	}
	if raw, ok := existing.(map[string]interface{}); ok {
		for k, v := range raw {
			contract[k] = v
		}
	}
	contract["capabilities"] = mergeExecutionCapabilityValues(contract["capabilities"], skillCapabilities)
	return contract
}

func mergeExecutionCapabilityValues(existing interface{}, additional []string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(cap string) {
		cap = normalizeSkillCapability(cap)
		if cap == "" || seen[cap] {
			return
		}
		seen[cap] = true
		out = append(out, cap)
	}
	switch caps := existing.(type) {
	case []string:
		for _, cap := range caps {
			add(cap)
		}
	case []interface{}:
		for _, item := range caps {
			if cap, ok := item.(string); ok {
				add(cap)
			}
		}
	}
	for _, cap := range additional {
		add(cap)
	}
	return out
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
	suppressedNames []string,
	browserPublishAffordance bool,
	explicitScreenshotRequest bool,
	rerankerResult []string,
	skillMatchScore float64,
	matchedSkills []string,
	matchedSkillCapabilities []string,
	skillCapabilityConstrained bool,
	skillRequiredCapabilities []string,
) {
	if !logDetailEnabled.Load() {
		return
	}
	logPath, ok := routeLogPath()
	if !ok {
		return
	}
	routeLogMu.Lock()
	defer routeLogMu.Unlock()
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
	fmt.Fprintf(f, "Execution affordances: browser_publish=%v explicit_screenshot=%v\n", browserPublishAffordance, explicitScreenshotRequest)
	if len(suppressedNames) > 0 {
		fmt.Fprintf(f, "Suppressed tools (%d): %v\n", len(suppressedNames), suppressedNames)
	}

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
	if skillMatchScore > 0 || len(matchedSkills) > 0 || skillCapabilityConstrained {
		fmt.Fprintf(f, "Skill match: score=%.4f matched=%v\n", skillMatchScore, matchedSkills)
		if skillCapabilityConstrained {
			fmt.Fprintf(f, "Skill capability constraint: %v\n", skillRequiredCapabilities)
		}
		if len(matchedSkillCapabilities) > 0 {
			fmt.Fprintf(f, "Skill capabilities: %v\n", matchedSkillCapabilities)
		}
	}

	fmt.Fprintln(f, "---")
}

// WriteToolExposureLog appends the final tool exposure state after downstream
// filters such as execution profile or workflow policy have run.
func WriteToolExposureLog(
	stage string,
	userMessage string,
	requestID string,
	userID string,
	profileLayer string,
	profileTask string,
	beforeCount int,
	toolNames []string,
) {
	if !logDetailEnabled.Load() {
		return
	}
	logPath, ok := routeLogPath()
	if !ok {
		return
	}
	routeLogMu.Lock()
	defer routeLogMu.Unlock()
	logDir := filepath.Dir(logPath)
	os.MkdirAll(logDir, 0755)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	if info, e := f.Stat(); e == nil && info.Size() > 5*1024*1024 {
		f.Truncate(0)
		f.Seek(0, 0)
		fmt.Fprintln(f, "[log truncated: exceeded 5MB]")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	msgPreview := userMessage
	if len([]rune(msgPreview)) > 100 {
		msgPreview = string([]rune(msgPreview)[:100]) + "..."
	}
	fmt.Fprintf(f, "\n=== Tool Exposure [%s] ===\n", now)
	fmt.Fprintf(f, "Stage: %s\n", strings.TrimSpace(stage))
	fmt.Fprintf(f, "Message: %s\n", msgPreview)
	fmt.Fprintf(f, "Request: %s | User: %s\n", requestID, userID)
	fmt.Fprintf(f, "Profile: layer=%s task=%s\n", profileLayer, profileTask)
	fmt.Fprintf(f, "Tools: before=%d after=%d\n", beforeCount, len(toolNames))
	for _, name := range toolNames {
		fmt.Fprintf(f, "  - %s\n", name)
	}
	fmt.Fprintln(f, "---")
}

func routeLogPath() (string, bool) {
	if value := routeLogPathOverride.Load(); value != nil {
		if path, ok := value.(string); ok && strings.TrimSpace(path) != "" {
			return path, true
		}
	}
	base := maclawBaseDirFallback()
	if base == "" {
		return "", false
	}
	return filepath.Join(base, "logs", "tool_route.log"), true
}

// BaseDirFunc is an atomic.Value holding a func() string that returns the
// effective maclaw base directory. Set by the corelib package at init time
// to avoid circular imports. If not set, falls back to the default base dir.
var BaseDirFunc atomic.Value

func maclawBaseDirFallback() string {
	if fn := BaseDirFunc.Load(); fn != nil {
		if dirFn, ok := fn.(func() string); ok {
			if dir := strings.TrimSpace(dirFn()); dir != "" {
				return dir
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".maclaw")
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
