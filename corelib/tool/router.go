package tool

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// CoreToolNames are always included regardless of the user message.
var CoreToolNames = map[string]bool{
	"list_sessions": true, "create_session": true,
	"send_and_observe": true, "get_session_output": true, "get_session_events": true,
	"control_session": true,
	"bash": true, "read_file": true, "write_file": true, "list_directory": true,
	"call_mcp_tool": true, "list_skills": true, "run_skill": true,
	"screenshot": true, "send_file": true,
	"memory": true,
	"web_search": true, "web_fetch": true,
	"set_nickname": true,
	"browser_connect": true, "browser_navigate": true, "browser_click": true,
	"discover_tool": true,
}

// CodingSessionToolNames lists tools that require a coding LLM session provider.
// When the coding LLM is not configured (simple mode), these tools should be
// filtered out since they would be non-functional.
var CodingSessionToolNames = map[string]bool{
	"create_session":    true,
	"list_sessions":     true,
	"send_input":        true,
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
	"list_providers": true,
	"send_input": true,
	"interrupt_session": true, "kill_session": true,
	"list_mcp_tools": true,
	"search_skill_hub": true, "install_skill_hub": true,
	"parallel_execute": true, "recommend_tool": true, "craft_tool": true,
	"open": true,
	"create_template": true, "list_templates": true, "launch_template": true,
	"get_config": true, "update_config": true, "batch_update_config": true,
	"list_config_schema": true, "export_config": true, "import_config": true,
	"set_max_iterations": true,
	"create_scheduled_task": true, "list_scheduled_tasks": true,
	"delete_scheduled_task": true, "update_scheduled_task": true,
	"search_and_install_skill": true,
	"switch_llm_provider": true,
	"manage_config": true,
	"query_audit_log": true,
	// Browser automation tools (CDP).
	"browser_connect": true, "browser_navigate": true, "browser_click": true,
	"browser_type": true, "browser_screenshot": true, "browser_get_text": true,
	"browser_get_html": true, "browser_eval": true, "browser_wait": true,
	"browser_scroll": true, "browser_select": true, "browser_list_pages": true,
	"browser_switch_page": true, "browser_close": true,
	"browser_click_at": true, "browser_set_files": true,
	"browser_back": true, "browser_info": true,
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

// Router selects the most relevant tools for a given user message.
type Router struct {
	generator    *DefinitionGenerator
	registry     *Registry
	recommender  SkillRecommender
	bm25Index    *bm25.Index
	hybrid       *HybridRetriever
	enrichStore  *EnrichmentStore
	tracker      *UsageTracker
	sessionTools map[string]bool
}

func NewRouter(generator *DefinitionGenerator) *Router {
	return &Router{
		generator: generator,
		bm25Index: bm25.New(),
	}
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

// Route selects the most relevant tools for userMessage from allTools.
func (r *Router) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	if len(allTools) <= MaxToolBudget {
		return allTools
	}

	var core, candidates []map[string]interface{}
	var candidateNames []string
	for _, t := range allTools {
		name := ExtractToolName(t)
		if CoreToolNames[name] || r.sessionTools[name] {
			core = append(core, t)
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
	for i, t := range candidates {
		name := candidateNames[i]
		text := r.buildSearchText(name, ExtractToolDescription(t))
		docs[i] = bm25.Doc{ID: name, Text: text}
		candidateTexts[name] = text
	}
	r.bm25Index.RebuildIfChanged(docs)
	scores := r.bm25Index.Score(userMessage)

	// Fuse with vector scores when hybrid retrieval is active.
	if r.hybrid != nil {
		scores = r.hybrid.FuseScores(userMessage, scores, candidateTexts)

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

	// Three-signal scoring: retrieval + experience + priority.
	queryTokens := bm25.Tokenize(userMessage)
	normScores := minMaxNormalize(scores)

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
		var priorityBonus float64
		if r.registry != nil {
			if t, ok := r.registry.Get(name); ok {
				priorityBonus = clampFloat(float64(t.Priority)*0.1, 0, 1)
			}
		}
		if r.tracker != nil {
			// α=0.6 retrieval + β=0.3 experience + γ=0.1 priority
			scoredList[i] = scored{index: i, score: 0.6*retrievalScore + 0.3*expScore + 0.1*priorityBonus}
		} else {
			// No tracker: α=0.9 retrieval + γ=0.1 priority
			scoredList[i] = scored{index: i, score: 0.9*retrievalScore + 0.1*priorityBonus}
		}
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	dynamicCount := 0
	result := make([]map[string]interface{}, len(core), MaxToolBudget+2)
	copy(result, core)
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
	go writeRouteLog(userMessage, len(allTools), len(core), len(candidates), r.hybrid != nil, rankedNames, rankedScores, selectedNames)

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

// writeRouteLog writes a detailed tool routing decision log to ~/.maclaw/logs/tool_route.log.
// Runs in a goroutine to avoid blocking the hot path.
func writeRouteLog(
	userMessage string,
	totalTools, coreCount, candidateCount int,
	hybridActive bool,
	rankedNames []string,
	rankedScores []float64,
	selectedNames []string,
) {
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
