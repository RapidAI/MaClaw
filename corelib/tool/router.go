package tool

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
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
	"call_mcp_tool": true, "run_skill": true,
	"screenshot": true, "send_file": true,
	"memory": true,
}

// BuiltinToolNames is the complete set of all builtin tool names.
var BuiltinToolNames = map[string]bool{
	"list_sessions": true, "create_session": true, "list_providers": true,
	"send_input": true, "get_session_output": true, "get_session_events": true,
	"interrupt_session": true, "kill_session": true, "screenshot": true,
	"list_mcp_tools": true, "call_mcp_tool": true,
	"list_skills": true, "search_skill_hub": true, "install_skill_hub": true, "run_skill": true,
	"parallel_execute": true, "recommend_tool": true, "craft_tool": true,
	"bash": true, "read_file": true, "write_file": true, "list_directory": true,
	"send_file": true, "open": true,
	"memory": true,
	"create_template": true, "list_templates": true, "launch_template": true,
	"get_config": true, "update_config": true, "batch_update_config": true,
	"list_config_schema": true, "export_config": true, "import_config": true,
	"set_max_iterations": true,
	"create_scheduled_task": true, "list_scheduled_tasks": true,
	"delete_scheduled_task": true, "update_scheduled_task": true,
	"search_and_install_skill": true,
	"switch_llm_provider": true,
	"send_and_observe": true, "control_session": true, "manage_config": true,
	"query_audit_log": true,
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
	generator   *DefinitionGenerator
	recommender SkillRecommender
	registry    *Registry
}

// NewRouter creates a new Router.
func NewRouter(generator *DefinitionGenerator) *Router {
	return &Router{generator: generator}
}

// SetRegistry sets the Registry used for dynamic builtin detection and tag-based scoring.
func (r *Router) SetRegistry(reg *Registry) {
	r.registry = reg
}

// SetRecommender sets the SkillRecommender used for recommendation matching.
func (r *Router) SetRecommender(recommender SkillRecommender) {
	r.recommender = recommender
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

func (r *Router) toolTokensWithTags(def map[string]interface{}) []string {
	name := ExtractToolName(def)
	desc := ExtractToolDescription(def)
	combined := name + " " + desc
	if tags := r.tagsForTool(name); len(tags) > 0 {
		combined += " " + strings.Join(tags, " ")
	}
	return Tokenize(combined)
}

// Route selects the most relevant tools for userMessage from allTools.
func (r *Router) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	if len(allTools) <= MaxToolBudget {
		return allTools
	}

	var core, candidates []map[string]interface{}
	for _, t := range allTools {
		if CoreToolNames[ExtractToolName(t)] {
			core = append(core, t)
		} else {
			candidates = append(candidates, t)
		}
	}

	remaining := MaxToolBudget - len(core)
	if remaining <= 0 || len(candidates) == 0 {
		return core
	}

	// Build a BM25 index over candidate tool descriptions.
	docs := make([]bm25.Doc, len(candidates))
	for i, t := range candidates {
		name := ExtractToolName(t)
		desc := ExtractToolDescription(t)
		text := name + " " + desc
		if tags := r.tagsForTool(name); len(tags) > 0 {
			text += " " + strings.Join(tags, " ")
		}
		docs[i] = bm25.Doc{ID: name, Text: text}
	}
	idx := bm25.New()
	idx.Rebuild(docs)
	scores := idx.Score(userMessage)

	type scored struct {
		index int
		score float64
	}
	scoredList := make([]scored, len(candidates))
	for i, t := range candidates {
		name := ExtractToolName(t)
		scoredList[i] = scored{index: i, score: scores[name]}
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
		name := ExtractToolName(candidates[s.index])
		if !r.isBuiltin(name) {
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

// Tokenize splits text into lowercase tokens by whitespace and punctuation.
// CJK characters are emitted as individual tokens.
func Tokenize(text string) []string {
	lower := strings.ToLower(text)
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || r == '_' || r == '-'
	})
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

// TermFrequency computes the term frequency of each token in a document.
func TermFrequency(tokens []string) map[string]float64 {
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

// ComputeIDF computes inverse document frequency across all documents.
func ComputeIDF(docs [][]string) map[string]float64 {
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

// TFIDFVector computes the TF-IDF vector for a document given precomputed IDF.
func TFIDFVector(tokens []string, idf map[string]float64) map[string]float64 {
	tf := TermFrequency(tokens)
	vec := make(map[string]float64, len(tf))
	for t, tfVal := range tf {
		idfVal, ok := idf[t]
		if !ok {
			idfVal = math.Log(float64(len(idf)) + 1.0)
		}
		vec[t] = tfVal * idfVal
	}
	return vec
}

// TFIDFSimilarity computes the cosine similarity between query and doc tokens using TF-IDF.
func TFIDFSimilarity(queryTokens, docTokens []string, idf map[string]float64) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}
	qVec := TFIDFVector(queryTokens, idf)
	dVec := TFIDFVector(docTokens, idf)

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
