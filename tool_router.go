package main

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	// maxToolBudget is the maximum number of tools to send to the LLM.
	// Core tools are always included; remaining budget goes to the highest-
	// scoring candidates ranked by TF-IDF similarity to the user message.
	maxToolBudget = 28

	// maxDynamicRouted caps how many MCP/non-code dynamic tools can be included.
	maxDynamicRouted = 18
)

// ---------------------------------------------------------------------------
// Core tool whitelist — these are always included regardless of the user
// message because they cover the fundamental interaction loop.
// ---------------------------------------------------------------------------

var coreToolNames = map[string]bool{
	// Session lifecycle — essential for the primary interaction loop
	"list_sessions": true, "create_session": true,
	"send_and_observe": true, "get_session_output": true, "get_session_events": true,
	"control_session": true,
	// Local operations — high frequency
	"bash": true, "read_file": true, "write_file": true, "list_directory": true,
	// MCP & Skill essentials
	"call_mcp_tool": true, "run_skill": true,
	// Screenshot (high frequency)
	"screenshot": true,
}

// builtinToolNames is the complete set of all builtin tool names (core + non-core).
// When a ToolRouter has a registry, it uses ToolRouter.isBuiltin() instead.
// This static set is kept as a fallback for tests that create a ToolRouter
// without a registry.
var builtinToolNames = map[string]bool{
	// Core (duplicated here for the isBuiltinToolName check)
	"list_sessions": true, "create_session": true, "list_providers": true,
	"send_input": true, "get_session_output": true, "get_session_events": true,
	"interrupt_session": true, "kill_session": true, "screenshot": true,
	"list_mcp_tools": true, "call_mcp_tool": true,
	"list_skills": true, "search_skill_hub": true, "install_skill_hub": true, "run_skill": true,
	"parallel_execute": true, "recommend_tool": true, "craft_tool": true,
	"bash": true, "read_file": true, "write_file": true, "list_directory": true,
	"send_file": true, "open": true,
	"save_memory": true, "list_memories": true, "delete_memory": true,
	"create_template": true, "list_templates": true, "launch_template": true,
	"get_config": true, "update_config": true, "batch_update_config": true,
	"list_config_schema": true, "export_config": true, "import_config": true,
	"set_max_iterations": true,
	"create_scheduled_task": true, "list_scheduled_tasks": true,
	"delete_scheduled_task": true, "update_scheduled_task": true,
	"search_and_install_skill": true,
	"switch_llm_provider": true,
	// Merged tools (optimized)
	"send_and_observe": true, "control_session": true, "manage_config": true,
	"query_audit_log": true,
}

// isBuiltinToolName returns true if the tool name is a known builtin tool.
// This is the static fallback used when no registry is available.
func isBuiltinToolName(name string) bool {
	return builtinToolNames[name]
}

// ToolRouter selects the most relevant tools for a given user message.
//
// Strategy:
//  1. Core tools (whitelist) are always included — they cover the basic
//     interaction loop and cost ~13 tool slots.
//  2. All remaining tools (non-core builtins + MCP dynamic tools) compete
//     for the remaining budget via TF-IDF cosine similarity against the
//     user message.
//  3. MCP dynamic tools are additionally capped at maxDynamicRouted.
type ToolRouter struct {
	generator *ToolDefinitionGenerator
	hubClient *SkillHubClient
	registry  *ToolRegistry
}

// NewToolRouter creates a new ToolRouter.
func NewToolRouter(generator *ToolDefinitionGenerator) *ToolRouter {
	return &ToolRouter{generator: generator}
}

// SetRegistry sets the ToolRegistry used for dynamic builtin detection and
// tag-based scoring. When set, isBuiltinToolName lookups use the registry
// instead of the static builtinToolNames map.
func (r *ToolRouter) SetRegistry(reg *ToolRegistry) {
	r.registry = reg
}

// SetHubClient sets the SkillHubClient used for recommendation matching.
func (r *ToolRouter) SetHubClient(client *SkillHubClient) {
	r.hubClient = client
}

// isBuiltin checks whether a tool name is a builtin tool. If the router has
// a registry, it queries the registry (category == builtin or non_code);
// otherwise it falls back to the static builtinToolNames map.
func (r *ToolRouter) isBuiltin(name string) bool {
	if r.registry != nil {
		if t, ok := r.registry.Get(name); ok {
			return t.Category == ToolCategoryBuiltin || t.Category == ToolCategoryNonCode
		}
		return false
	}
	return isBuiltinToolName(name)
}

// tagsForTool returns the tags for a tool from the registry, or nil if
// the registry is not set or the tool is not found.
func (r *ToolRouter) tagsForTool(name string) []string {
	if r.registry == nil {
		return nil
	}
	if t, ok := r.registry.Get(name); ok {
		return t.Tags
	}
	return nil
}

// toolTokensWithTags extracts tokens from a tool definition's name,
// description, and registry tags (if available).
func (r *ToolRouter) toolTokensWithTags(def map[string]interface{}) []string {
	name := extractToolName(def)
	desc := extractToolDescription(def)
	combined := name + " " + desc
	if tags := r.tagsForTool(name); len(tags) > 0 {
		combined += " " + strings.Join(tags, " ")
	}
	return tokenize(combined)
}

// Route selects the most relevant tools for userMessage from allTools.
//
// When len(allTools) <= maxToolBudget, all tools are returned unchanged.
// Otherwise, core tools are kept unconditionally and the remaining budget
// is filled by ranking all other tools (builtin + dynamic) via TF-IDF
// cosine similarity to the user message.
func (r *ToolRouter) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	if len(allTools) <= maxToolBudget {
		return allTools
	}

	// Partition into core (always kept) and candidates (compete for budget).
	var core, candidates []map[string]interface{}
	for _, tool := range allTools {
		if coreToolNames[extractToolName(tool)] {
			core = append(core, tool)
		} else {
			candidates = append(candidates, tool)
		}
	}

	remaining := maxToolBudget - len(core)
	if remaining <= 0 || len(candidates) == 0 {
		return core
	}

	// Tokenize user message for TF-IDF scoring.
	msgTokens := tokenize(userMessage)

	if len(msgTokens) == 0 {
		// No meaningful tokens — take the first N candidates in original order.
		if remaining > len(candidates) {
			remaining = len(candidates)
		}
		return append(core, candidates[:remaining]...)
	}

	// Build IDF from all candidate tool documents.
	allDocs := make([][]string, len(candidates))
	for i, tool := range candidates {
		allDocs[i] = r.toolTokensWithTags(tool)
	}
	idf := computeIDF(allDocs)

	// Score each candidate.
	type scored struct {
		index int
		score float64
	}
	scores := make([]scored, len(candidates))
	for i, doc := range allDocs {
		scores[i] = scored{index: i, score: tfidfSimilarity(msgTokens, doc, idf)}
	}

	// Sort by score descending; stable sort preserves original order for ties.
	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Fill remaining budget, respecting the MCP dynamic tool cap.
	dynamicCount := 0
	result := make([]map[string]interface{}, len(core), maxToolBudget+2)
	copy(result, core)
	for _, s := range scores {
		if len(result) >= maxToolBudget {
			break
		}
		name := extractToolName(candidates[s.index])
		if !r.isBuiltin(name) {
			dynamicCount++
			if dynamicCount > maxDynamicRouted {
				continue
			}
		}
		result = append(result, candidates[s.index])
	}

	// Check recommended Skills from Hub for keyword overlap with user message.
	if r.hubClient != nil {
		if hint := r.matchRecommendations(msgTokens); hint != nil {
			result = append(result, hint)
		}
	}

	return result
}

// matchRecommendations checks if any recommended Skill from the Hub matches
// the user message tokens via simple keyword overlap. Returns a tool hint
// map if a match is found, nil otherwise.
func (r *ToolRouter) matchRecommendations(msgTokens []string) map[string]interface{} {
	if len(msgTokens) == 0 {
		return nil
	}

	recommendations := r.hubClient.GetRecommendations()
	if len(recommendations) == 0 {
		return nil
	}

	msgSet := make(map[string]struct{}, len(msgTokens))
	for _, t := range msgTokens {
		msgSet[t] = struct{}{}
	}

	for _, rec := range recommendations {
		recTokens := tokenize(rec.Name + " " + rec.Description)
		matchCount := 0
		for _, rt := range recTokens {
			if _, ok := msgSet[rt]; ok {
				matchCount++
				if len([]rune(rt)) > 1 {
					// A multi-char token match is strong enough on its own.
					return searchAndInstallSkillHint()
				}
			}
		}
		// Require at least 2 single-char matches to avoid false positives
		// from single CJK character overlap.
		if matchCount >= 2 {
			return searchAndInstallSkillHint()
		}
	}

	return nil
}

// searchAndInstallSkillHint returns a tool definition map for the
// search_and_install_skill hint that the LLM can invoke.
func searchAndInstallSkillHint() map[string]interface{} {
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

// ---------------------------------------------------------------------------
// Tokenization
// ---------------------------------------------------------------------------

// tokenize splits text into lowercase tokens by whitespace and punctuation.
// CJK characters are emitted as individual tokens so that Chinese tool
// descriptions can participate in TF-IDF scoring.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	// First pass: split on whitespace/punctuation/separators.
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || r == '_' || r == '-'
	})
	// Second pass: break CJK characters out of mixed tokens.
	result := make([]string, 0, len(tokens))
	for _, t := range tokens {
		hasCJK := false
		for _, r := range t {
			if unicode.Is(unicode.Han, r) {
				hasCJK = true
				break
			}
		}
		if !hasCJK {
			if len(t) > 1 {
				result = append(result, t)
			}
			continue
		}
		// Split: consecutive non-CJK chars form one token, each CJK char
		// is its own token.
		var buf strings.Builder
		for _, r := range t {
			if unicode.Is(unicode.Han, r) {
				if buf.Len() > 1 {
					result = append(result, buf.String())
				}
				buf.Reset()
				result = append(result, string(r))
			} else {
				buf.WriteRune(r)
			}
		}
		if buf.Len() > 1 {
			result = append(result, buf.String())
		}
	}
	return result
}

// toolTokens extracts tokens from a tool definition's name and description.
func toolTokens(def map[string]interface{}) []string {
	name := extractToolName(def)
	desc := extractToolDescription(def)
	combined := name + " " + desc
	return tokenize(combined)
}

// extractToolDescription extracts the description from an OpenAI function
// calling tool definition.
func extractToolDescription(def map[string]interface{}) string {
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

// ---------------------------------------------------------------------------
// TF-IDF Computation
// ---------------------------------------------------------------------------

// termFrequency computes the term frequency of each token in a document.
// TF(t, d) = count(t in d) / len(d)
func termFrequency(tokens []string) map[string]float64 {
	counts := make(map[string]int, len(tokens))
	for _, t := range tokens {
		counts[t]++
	}
	tf := make(map[string]float64, len(counts))
	n := float64(len(tokens))
	if n == 0 {
		return tf
	}
	for t, c := range counts {
		tf[t] = float64(c) / n
	}
	return tf
}

// computeIDF computes inverse document frequency across all documents.
// IDF(t) = log(N / (1 + df(t)))  where df(t) = number of docs containing t.
func computeIDF(docs [][]string) map[string]float64 {
	df := make(map[string]int)
	for _, doc := range docs {
		seen := make(map[string]bool, len(doc))
		for _, t := range doc {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
	}
	n := float64(len(docs))
	idf := make(map[string]float64, len(df))
	for t, count := range df {
		idf[t] = math.Log(n / (1.0 + float64(count)))
	}
	return idf
}

// tfidfVector computes the TF-IDF vector for a document given precomputed IDF.
func tfidfVector(tokens []string, idf map[string]float64) map[string]float64 {
	tf := termFrequency(tokens)
	vec := make(map[string]float64, len(tf))
	for t, tfVal := range tf {
		idfVal, ok := idf[t]
		if !ok {
			// Term not in any document corpus — use a default IDF.
			idfVal = math.Log(float64(len(idf)) + 1.0)
		}
		vec[t] = tfVal * idfVal
	}
	return vec
}

// tfidfSimilarity computes the cosine similarity between the user message
// tokens and a tool document's tokens using TF-IDF weighting.
func tfidfSimilarity(queryTokens, docTokens []string, idf map[string]float64) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}

	qVec := tfidfVector(queryTokens, idf)
	dVec := tfidfVector(docTokens, idf)

	// Cosine similarity = dot(q, d) / (|q| * |d|)
	var dot, qNorm, dNorm float64
	for t, qVal := range qVec {
		if dVal, ok := dVec[t]; ok {
			dot += qVal * dVal
		}
		qNorm += qVal * qVal
	}
	for _, dVal := range dVec {
		dNorm += dVal * dVal
	}

	denom := math.Sqrt(qNorm) * math.Sqrt(dNorm)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
