package tool

import (
	"sort"
	"strings"

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
	bm25Index   *bm25.Index // cached BM25 index, reused across Route calls
}

// NewRouter creates a new Router.
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

	// Build a BM25 index over candidate tool descriptions (reuses cached index).
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
	r.bm25Index.RebuildIfChanged(docs)
	scores := r.bm25Index.Score(userMessage)

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
