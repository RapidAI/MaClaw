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
	// The old name stays source-compatible, but only the minimal control plane
	// is now unconditional. Both bootstrap and legacy candidates are builtin
	// capabilities; builtin classification must not depend on whether a tool is
	// permanently exposed to a model request.
	for name := range CoreToolNames {
		BuiltinToolNames[name] = true
	}
	for name := range LegacyCandidateToolNames {
		BuiltinToolNames[name] = true
	}
}

// SetLogDetailEnabled updates the detailed routing log gate.
func SetLogDetailEnabled(enabled bool) {
	logDetailEnabled.Store(enabled)
}

// CoreToolNames is the historical compatibility catalog. New code must not
// treat it as an always-visible surface: only LegacyBootstrapToolNames is
// unconditional in RouteWithOptions. Keeping this broad map for now avoids a
// flag-day break in registries, tests and discovery metadata while individual
// families migrate to reviewed capability provisions.
var CoreToolNames = map[string]bool{
	"list_sessions": true, "get_session_output": true, "get_session_events": true,
	"bash": true, "read_file": true, "read_tool_result": true, "FileRead": true, "ripgrep": true, "Glob": true, "write_file": true, "edit_file": true, "list_directory": true,
	"memory":           true,
	"web_fetch":        true,
	"download_file":    true,
	"set_nickname":     true,
	"discover_tool":    true,
	"task":             true,
	"goal":             true,
	"async_wait":       true,
	"compress_context": true,
	"tts":              true,
	"asr":              true,
}

// LegacyBootstrapToolNames is the only unconditional legacy layer. It carries
// bounded loop-control/recovery affordances, not business execution.
var LegacyBootstrapToolNames = map[string]bool{
	"task":             true,
	"async_wait":       true,
	"compress_context": true,
}

type conditionalKeepRule struct {
	keepTools []string
	// noMemoryPin when true means tools from this rule should NOT be pinned
	// from recalled memory or other indirect context. They are pinned after
	// actual successful use through ActivateSessionTool.
	noMemoryPin bool
	// scoreEligible when true means the rule's tools are benign (no privilege
	// expansion beyond everyday surfaces) and may compete as normal scored
	// candidates instead of being filtered out whenever the semantic
	// classifier does not activate them. Sensitive tools (ssh, browser,
	// screenshot, record_audio, craft_tool, mis_data, IM delivery) stay
	// fail-closed: they are exposed only through classifier activation.
	scoreEligible bool
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
	// Web search and document rendering do not expand privilege beyond what
	// the everyday core surface already allows, so they compete on retrieval
	// score instead of disappearing whenever the classifier stays silent.
	{keepTools: []string{"web_search"}, scoreEligible: true},
	{keepTools: []string{"send_file", "send_to_im", "im_message", "open"}},
	{keepTools: []string{"craft_tool"}},
	{keepTools: allBrowserToolNames, noMemoryPin: true},
	{keepTools: []string{"office"}, scoreEligible: true},
	{keepTools: []string{"generate_pdf", "office"}, scoreEligible: true},
	// Microphone capture is sensitive and must be selected from a semantic
	// audio-record intent. It is never part of the legacy fallback surface.
	{keepTools: []string{"record_audio"}},
	// Knowledge-base writers mutate a shared store. They stay fail-closed and
	// enter only through a knowledge-write classification (UIC affinity pins
	// them, then payload narrowing keeps just the needed ones); a read-only
	// question must never see them as priority fillers.
	{keepTools: knowledgeWriteToolList},
	// Screenshot is a focused, on-demand desktop capture surface. It is
	// activated by the unified semantic intent classifier, never left as a
	// generic fallback candidate.
	{keepTools: []string{"screenshot"}},
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
// Bootstrap and legacy-candidate names are merged in automatically via init(),
// so there is no need to duplicate them here.
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
	// Keep both the bootstrap and candidate catalog classified as builtin. The
	// latter affects dynamic-slot accounting only; it does not make a candidate
	// permanently visible.
	for name := range CoreToolNames {
		BuiltinToolNames[name] = true
	}
	for name := range LegacyCandidateToolNames {
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
	// mu serializes configuration updates and route construction. Route rebuilds
	// cached BM25 indexes, so it must not overlap an embedding activation or a
	// second IM request sharing this router.
	mu                 sync.Mutex
	generator          *DefinitionGenerator
	registry           *Registry
	recommender        SkillRecommender
	skillProvider      SkillProvider
	bm25Index          *bm25.Index
	skillBM25          *bm25.Index // separate index for skill trigger matching
	hybrid             *HybridRetriever
	enrichStore        *EnrichmentStore
	tracker            *UsageTracker
	reranker           Reranker                        // nil when reranking is disabled
	intentClassifier   *IntentClassifier               // hybrid intent classifier (Layer 1+2+3)
	unifiedClassifier  *intent.UnifiedIntentClassifier // UIC replaces conditionalKeepRules when non-nil
	lastRecommendation RoutingRecommendation
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry = reg
}

// SetRecommender sets the SkillRecommender used for recommendation matching.
func (r *Router) SetRecommender(recommender SkillRecommender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recommender = recommender
}

// SetEmbedder configures the embedder for hybrid retrieval.
// If emb is a NoopEmbedder, hybrid is disabled (set to nil).
func (r *Router) SetEmbedder(emb embedding.Embedder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if embedding.IsNoop(emb) {
		r.hybrid = nil
		return
	}
	r.hybrid = NewHybridRetriever(emb)
}

// HybridActive returns true if hybrid retrieval is currently enabled.
func (r *Router) HybridActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hybrid != nil
}

// SetEnrichmentStore configures the enrichment store for enhanced tool descriptions.
func (r *Router) SetEnrichmentStore(store *EnrichmentStore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enrichStore = store
}

// SetUsageTracker configures the usage tracker for experience-aware scoring.
func (r *Router) SetUsageTracker(tracker *UsageTracker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tracker = tracker
}

// SetReranker configures the LLM listwise reranker. Pass nil to disable.
func (r *Router) SetReranker(rr Reranker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reranker = rr
}

// SetIntentClassifier sets the hybrid intent classifier used for semantic
// intent detection in conditional tool matching and routing decisions.
func (r *Router) SetIntentClassifier(ic *IntentClassifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intentClassifier = ic
}

// IntentClassifier returns the configured IntentClassifier, or nil.
func (r *Router) IntentClassifier() *IntentClassifier {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.intentClassifier
}

// SetUnifiedClassifier sets the Unified Intent Classifier used for
// intent-driven conditional tool selection. When non-nil, Route() uses
// UIC results instead of evaluating local conditional keep rules.
func (r *Router) SetUnifiedClassifier(uic *intent.UnifiedIntentClassifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unifiedClassifier = uic
}

// ActivateSessionTool is retained as a source-compatible no-op while hosts
// migrate from historical name pins to verified task relations. A candidate
// recommender must never retain a tool name as future model authority.
func (r *Router) ActivateSessionTool(name string) {
	_ = r
	_ = name
}

// IsSessionPinned always returns false. Historical tool names are not task
// evidence and cannot affect a later tool surface.
func (r *Router) IsSessionPinned(name string) bool {
	_ = r
	_ = name
	return false
}

// SessionPinnedToolsMissing is retained as a source-compatible no-op. Surface
// replacement is driven by a fresh plan/revision, never by a missing legacy
// tool name.
func (r *Router) SessionPinnedToolsMissing(currentNames map[string]bool) []string {
	_ = r
	_ = currentNames
	return nil
}

// ResetSession is retained for callers that reset other router caches. Router
// instances no longer carry task-continuation state.
func (r *Router) ResetSession() {
	_ = r
}

// WarmupDeferredEmbeddings pre-computes and caches embedding vectors for
// deferred tool descriptions in the background. Call this after SearchDeferred
// returns results so that when the tools are activated and enter the Route()
// pipeline, their embeddings are already warm in ToolEmbeddingCache.
// No-op when hybrid retrieval is not active.
func (r *Router) WarmupDeferredEmbeddings(toolDefs []map[string]interface{}) {
	r.mu.Lock()
	hybrid := r.hybrid
	if hybrid == nil || len(toolDefs) == 0 {
		r.mu.Unlock()
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
	r.mu.Unlock()
	if len(texts) == 0 {
		return
	}
	// Fire-and-forget: GetBatch populates the cache and triggers async disk save.
	go func() {
		_, _ = hybrid.toolCache.GetBatch(texts)
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
// It is name + description only: BodySummary is long, templated parameter
// documentation whose shared boilerplate ("Parameters:/Typical usage:")
// collapses the embedding space (measured median pairwise cosine ~0.8 across
// tool vectors), turning cosine ranking into noise. BodySummary is still fed
// to the LLM reranker via CandidateSummary, where a judge can use it.
func (r *Router) buildEmbeddingText(name, description string) string {
	return name + " " + description
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

// scoreEligibleConditionalToolNames is the set of conditional tools whose
// rules are marked scoreEligible. These benign tools are not filtered out of
// the candidate pool when the classifier stays silent; they compete on
// retrieval score like ordinary candidates.
var scoreEligibleConditionalToolNames map[string]bool

func init() {
	allConditionalKeepTools = make(map[string]bool)
	scoreEligibleConditionalToolNames = make(map[string]bool)
	for _, rule := range conditionalKeepRules {
		for _, name := range rule.keepTools {
			allConditionalKeepTools[name] = true
			if rule.scoreEligible {
				scoreEligibleConditionalToolNames[name] = true
			}
		}
	}
}

// IsConditionalTool returns true if the tool is governed by a conditional keep
// rule. Sensitive conditional tools are included only when the semantic
// classifier activates them; score-eligible (benign) ones may also enter via
// retrieval scoring. Once actually used in a session, callers should pin them
// via ActivateSessionTool so they remain available for follow-up messages.
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
// needed in Route() or any other consumer.
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

func uicResultUsableForToolActivation(result intent.ClassificationResult) bool {
	// The compatibility router has no capability-plan evidence of its own.  A
	// degraded classification means one of the semantic channels did not
	// complete, so its label is only a routing hint and must not become a
	// model-visible capability here.  Managed GUI turns use the governed
	// planner, which has its own explicit read-only degraded-hint policy.
	//
	// This is deliberately a provenance check, rather than another confidence
	// threshold: a high score from a partial classifier is still partial
	// evidence, not an authorization decision.
	return !result.Degraded && result.Primary != intent.LabelUnknown && result.Primary != intent.LabelAmbiguous
}

func uicToolNameActivatable(name string) bool {
	return allConditionalKeepTools[name] || knowledgeWriteToolNames[name]
}

func coreRoutePriority(name string, condKeep, mustKeep map[string]bool) int {
	if mustKeep[name] {
		return -1
	}
	if condKeep[name] {
		// A current-request policy requirement (for example office for a staged
		// document) is never silently displaced by optional candidates.
		return 0
	}
	switch name {
	case "task", "async_wait", "compress_context":
		return 1
	}
	return 2
}

// legacyRouteFallbackTool is a deliberately tiny compatibility bridge for
// operations that must remain discoverable while their callers move to
// explicit adapter provisions. It is not a bootstrap grant and must never be
// expanded from history, text matches, or tool metadata.
func legacyRouteFallbackTool(name string) bool {
	return legacyAdapterFallbackAllowed(name, time.Now().UTC()) && legacyFallbackSurfaceTool(name)
}

// legacyFallbackSurfaceTool is the temporary minimal adapter surface needed by
// existing unmanaged hosts. Each listed name has a reviewed provision in
// legacy_adapter_catalog.go; this function only defines which provisions are
// required without a current retrieval match while that host migration is
// underway.
func legacyFallbackSurfaceTool(name string) bool {
	switch name {
	case "bash", "read_file", "ripgrep", "edit_file", "discover_tool":
		return true
	default:
		return false
	}
}

func trimCoreToolsToBudget(core []map[string]interface{}, condKeep, mustKeep map[string]bool) []map[string]interface{} {
	if len(core) <= MaxToolBudget && len(mustKeep) == 0 {
		return core
	}
	trimmed := append([]map[string]interface{}(nil), core...)
	sort.SliceStable(trimmed, func(i, j int) bool {
		ni := ExtractToolName(trimmed[i])
		nj := ExtractToolName(trimmed[j])
		pi := coreRoutePriority(ni, condKeep, mustKeep)
		pj := coreRoutePriority(nj, condKeep, mustKeep)
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

// RouteWithOptions preserves the legacy definitions return for callers that
// have not yet migrated. New code should use RecommendWithOptions followed by
// BuildLegacyAdapterPlan/RenderLegacyAdapterPlan, not treat this return value
// as an authorization decision.
func (r *Router) RouteWithOptions(userMessage string, allTools []map[string]interface{}, opts RouteOptions) []map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.routeWithOptionsLocked(userMessage, allTools, opts)
}

// RecommendWithOptions returns the Router's compatibility selection together
// with the reviewed host-only capability evidence from the same serialized
// routing decision. It closes the old Route(); LastRoutingRecommendation()
// race, where another turn could overwrite evidence before a host built its
// adapter plan. The selected definitions remain compatibility data only; an
// LLM surface must be built by BuildLegacyAdapterPlan and rendered from that
// immutable plan.
func (r *Router) RecommendWithOptions(userMessage string, allTools []map[string]interface{}, opts RouteOptions) ([]map[string]interface{}, RoutingRecommendation) {
	if r == nil {
		return nil, RoutingRecommendation{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	selected := r.routeWithOptionsLocked(userMessage, allTools, opts)
	return selected, r.lastRecommendation.clone()
}

// LastRoutingRecommendation returns the host-only capability evidence from
// the most recent Route call. It is a migration bridge for callers moving to
// a planner: the existing Route return remains a legacy compatibility surface
// and must not be confused with authorization.
func (r *Router) LastRoutingRecommendation() RoutingRecommendation {
	if r == nil {
		return RoutingRecommendation{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRecommendation.clone()
}

// routeWithOptionsLocked is RouteWithOptions' serialized implementation.
// Keeping this private ensures the candidate recommendation and compatibility
// selection can be observed atomically by RecommendWithOptions.
func (r *Router) routeWithOptionsLocked(userMessage string, allTools []map[string]interface{}, opts RouteOptions) []map[string]interface{} {
	// A dynamic MCP/Skill gateway is a host-controlled transport, not a legacy
	// model capability. Filter before any match scoring or budget partition so
	// it cannot consume a slot, gain a fallback pin, or leave stale advice in a
	// routing recommendation even if a later surface compositor strips it.
	allTools = withoutLegacyModelDynamicGateways(allTools)
	r.lastRecommendation = RoutingRecommendation{}
	routeNow := time.Now().UTC()
	var condKeep map[string]bool
	var condFilterOut map[string]bool
	var cachedICResult *IntentResult
	var skillRequiredCapabilities []string
	var skillCapabilityConstrained bool
	suppressedTools := map[string]bool{}
	skillInstallEligible := true
	routeIntent := opts.Intent
	if routeIntent != nil && !routeIntent.Usable() {
		routeIntent = nil
	}
	searchQuery := userMessage
	if routeIntent != nil {
		searchQuery = routeIntent.SearchQuery(userMessage)
	}

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

	if (r.unifiedClassifier != nil || opts.PreResolved != nil) && !opts.SkipUnifiedClassifier {
		// UIC path: the UnifiedIntentClassifier determines which conditional
		// tools to keep. Local wording never promotes one. A pre-resolved
		// classification is data from the turn's governed upstream pass and is
		// honored even when no live classifier is attached to this router.
		condKeep = make(map[string]bool)
		condFilterOut = make(map[string]bool)

		var uicResult intent.ClassificationResult
		switch {
		case opts.PreResolved != nil:
			// The turn's governed classification was already computed upstream
			// (e.g. RuntimeContext.SemanticIntent): use it verbatim instead of
			// classifying again on possibly different context.
			uicResult = *opts.PreResolved
		case opts.PreferEmbeddingOnly:
			uicResult = r.unifiedClassifier.ClassifyEmbeddingOnly(intent.MessageContext{Text: userMessage})
			if !uicResultUsableForToolActivation(uicResult) || uicResult.Confidence < uicActivationThreshold {
				// The fast channel cannot activate anything for this message.
				// Reuse the main loop's full-fusion classification when it is
				// already cached for this same message: a pure cache read that
				// never triggers a new embedding or LLM call, so the
				// "routing never calls tree/LLM" contract still holds while a
				// degraded/unknown fast result no longer hides every
				// conditional tool.
				if cached, ok := r.unifiedClassifier.ClassifyCached(intent.MessageContext{Text: userMessage}); ok {
					uicResult = cached
				}
			}
		default:
			uicResult = r.unifiedClassifier.Classify(intent.MessageContext{Text: userMessage})
		}
		skillInstallEligible = uicSkillInstallEligible(uicResult)
		skillRequiredCapabilities, skillCapabilityConstrained = skillCapabilityConstraintForUIC(uicResult)

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

		// Filter out conditional tools NOT matched by UIC. Score-eligible
		// (benign) conditional tools stay in the candidate pool and compete on
		// retrieval score; only sensitive tools fail closed.
		for name := range allConditionalKeepTools {
			if !condKeep[name] && !scoreEligibleConditionalToolNames[name] {
				condFilterOut[name] = true
			}
		}

	} else {
		// Fallback path: UIC not available. Local conditional rules are not an
		// execution-routing authority, so keep every sensitive conditional tool
		// filtered unless the semantic IntentClassifier below explicitly
		// promotes it. Score-eligible (benign) tools compete on retrieval score.
		condKeep = make(map[string]bool)
		condFilterOut = make(map[string]bool)
		for name := range allConditionalKeepTools {
			if !scoreEligibleConditionalToolNames[name] {
				condFilterOut[name] = true
			}
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
	skillUploadRequest := isSkillUploadRequest(userMessage)
	if skillUploadRequest {
		skillInstallEligible = false
		suppressedTools["search_and_install_skill"] = true
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
	}
	// LLM / structured intent rewrite only contributes an expanded retrieval
	// query (applied at the search stage). It must never pin or exclude tools
	// by name — that is the planner's job under the capability-first design.
	if routeIntent != nil && routeIntent.QueryForRoute != "" {
		q := routeIntent.QueryForRoute
		if rs := []rune(q); len(rs) > 80 {
			q = string(rs[:80]) + "..."
		}
		log.Printf("[RouteIntent] intent=%q confidence=%.2f query=%q",
			routeIntent.Intent, routeIntent.Confidence, q)
	}
	// Screenshot can only be promoted by the semantic classifier. A legacy
	// router must not derive capture, recording, VCS, or document authority
	// from wording, filenames, or host presentation markers.
	semanticScreenshotRequest := condKeep["screenshot"]
	screenshotRequest := semanticScreenshotRequest
	browserSessionActive := condKeep["browser"]

	if browserSessionActive {
		// Browser tasks must use the stable merged browser surface. Hide generic
		// desktop screenshot, shell, skill/discovery, MCP, and git fallbacks so the
		// model cannot drift into screen scraping, taskkill, raw authenticated HTTP
		// calls, skill reruns, or source-control actions instead of page actions.
		suppressedTools["bash"] = true
		if !screenshotRequest {
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
	if !screenshotRequest {
		suppressedTools["screenshot"] = true
	}
	suppressedTools["git_commit"] = true
	suppressedTools["git_push"] = true

	// Skill matching is recommendation evidence only on this compatibility
	// route. Dynamic Skills must enter the model surface through a managed,
	// identity-bound semantic binding; a legacy name router must never turn a
	// matched name into authority for manage_skill.
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
	// --- Browser diagnostic: log how browser tools entered condKeep ---
	{
		var browserInKeep []string
		for name := range condKeep {
			if isBrowserDiagTool(name) {
				browserInKeep = append(browserInKeep, name)
			}
		}
		if len(browserInKeep) > 0 {
			source := "unknown"
			if r.unifiedClassifier != nil {
				source = "UIC"
			} else {
				source = "semantic_intent"
			}
			log.Printf("[browser-diag] Route_condKeep: source=%s browserInKeep=%v", source, browserInKeep)
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
		if LegacyCandidateToolNames[name] && !legacyAdapterCandidateAllowed(name, routeNow) {
			// A known compatibility name without a live reviewed provision is
			// catalog_incomplete, not a candidate. Never let its description or
			// a BM25 hit reconstruct authority.
			continue
		}
		if LegacyBootstrapToolNames[name] || condKeep[name] || legacyRouteFallbackTool(name) {
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

	mustKeepCore := map[string]bool{}
	matchedSkillCapabilities := r.matchedSkillCapabilities(matchedSkills)
	core = trimCoreToolsToBudget(core, condKeep, mustKeepCore)
	remainingSlots := MaxToolBudget - len(core)
	if len(candidates) == 0 || remainingSlots <= 0 {
		r.recordLegacyRoutingRecommendation(searchQuery, core, nil, nil, mustKeepCore, routeNow)
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
	normScores := normalizeRetrievalScores(scores)

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

		var finalScore float64
		if r.skillProvider != nil && r.tracker != nil {
			// Four signals: retrieval + experience + outcome + priority. Dynamic
			// Skill match data remains recommendation evidence, never a bonus for
			// a legacy gateway candidate.
			finalScore = 0.50*retrievalScore + 0.25*expScore + 0.15*outcomeScore + 0.10*priorityBonus
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
				// Recommendation hints are metadata-only client-side prompts in
				// legacy hosts. Do not let them violate the model tool budget: if
				// the routed request surface is full, replace the lowest-priority
				// optional candidate instead of appending a 29th definition.
				if len(result) >= MaxToolBudget {
					if evicted := evictLegacyOptionalTool(result, LegacyBootstrapToolNames, condKeep, mustKeepCore); evicted >= 0 {
						delete(resultNames, ExtractToolName(result[evicted]))
						result[evicted] = hint
						resultNames[name] = true
					}
				} else {
					result = append(result, hint)
					resultNames[name] = true
				}
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

	// Compute bodyAware: true when hybrid is active and any candidate has a
	// non-empty BodySummary. BodySummary no longer feeds the embedding text
	// (it collapsed the vector space); it is still passed to the LLM reranker
	// via CandidateSummary, so the flag now only means "reranker has body
	// context available", not "body affects retrieval scoring".
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

	go writeRouteLog(userMessage, len(allTools), len(core), len(candidates), r.hybrid != nil, bodyAware, rankedNames, rankedScores, rankedRoutingHintAdjustments, selectedNames, suppressedNames, false, false, semanticScreenshotRequest, screenshotRequest, rerankerResult, skillScore, matchedSkills, matchedSkillCapabilities, skillCapabilityConstrained, skillRequiredCapabilities)

	r.recordLegacyRoutingRecommendation(searchQuery, result, rankedNames, rankedScores, mustKeepCore, routeNow)
	return result
}

func withoutLegacyModelDynamicGateways(definitions []map[string]interface{}) []map[string]interface{} {
	if len(definitions) == 0 {
		return definitions
	}
	filtered := make([]map[string]interface{}, 0, len(definitions))
	for _, definition := range definitions {
		if IsLegacyModelDynamicGateway(ExtractToolName(definition)) {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}

func (r *Router) recordLegacyRoutingRecommendation(searchQuery string, selected []map[string]interface{}, rankedNames []string, rankedScores []float64, required map[string]bool, now time.Time) {
	recommendation := RoutingRecommendation{SearchQuery: strings.TrimSpace(searchQuery)}
	scoreByName := make(map[string]float64, len(rankedNames))
	for i, name := range rankedNames {
		if i < len(rankedScores) {
			scoreByName[name] = rankedScores[i]
		}
	}
	seenCapabilities := make(map[CapabilityID]bool)
	for _, definition := range selected {
		name := ExtractToolName(definition)
		provision, ok := LegacyAdapterProvisionForTool(name, now)
		if !ok {
			continue
		}
		reason := "retrieval_candidate"
		score := scoreByName[name]
		if LegacyBootstrapToolNames[name] {
			reason = "bootstrap"
			score = 1
		} else if required[name] {
			reason = "current_turn_required"
			score = 1
		} else if legacyFallbackSurfaceTool(name) {
			reason = "compatibility_fallback"
			score = 1
		}
		recommendation.Evidence = append(recommendation.Evidence, RoutingEvidence{
			ToolName: name, Capability: provision.Capability, Reason: reason, Score: score, AdapterContract: provision.AdapterContract,
		})
		if !seenCapabilities[provision.Capability] {
			seenCapabilities[provision.Capability] = true
			recommendation.CandidateCapabilities = append(recommendation.CandidateCapabilities, provision.Capability)
		}
		if score > recommendation.Confidence {
			recommendation.Confidence = score
		}
	}
	sort.SliceStable(recommendation.Evidence, func(i, j int) bool {
		if recommendation.Evidence[i].Reason != recommendation.Evidence[j].Reason {
			return recommendation.Evidence[i].Reason < recommendation.Evidence[j].Reason
		}
		return recommendation.Evidence[i].ToolName < recommendation.Evidence[j].ToolName
	})
	r.lastRecommendation = recommendation
}

// evictLegacyOptionalTool finds a candidate that was neither bootstrap nor a
// current-turn required tool. Returning -1 is intentional: required tools are
// never silently replaced merely to fit a recommendation hint.
func evictLegacyOptionalTool(result []map[string]interface{}, bootstrap, condKeep, mustKeep map[string]bool) int {
	for i := len(result) - 1; i >= 0; i-- {
		name := ExtractToolName(result[i])
		if !bootstrap[name] && !condKeep[name] && !mustKeep[name] && !legacyRouteFallbackTool(name) {
			return i
		}
	}
	return -1
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
	semanticScreenshotRequest bool,
	screenshotRequest bool,
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
	fmt.Fprintf(f, "Execution affordances: browser_publish=%v explicit_screenshot=%v semantic_screenshot=%v screenshot_requested=%v\n", browserPublishAffordance, explicitScreenshotRequest, semanticScreenshotRequest, screenshotRequest)
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
