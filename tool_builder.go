package main

import (
	"sort"
	"strings"
)

// DynamicToolBuilder builds LLM tool definitions dynamically from the ToolRegistry.
// When the total available tools exceed maxDirectTools, it applies context-aware
// filtering: all builtin tools are always included, and the remaining slots are
// filled by the most relevant dynamic tools based on keyword similarity.
type DynamicToolBuilder struct {
	registry      *ToolRegistry
	maxDirectTools int // threshold before filtering kicks in (default 20)
	maxDynamic     int // max non-builtin tools when filtering (default 15)
}

// NewDynamicToolBuilder creates a builder backed by the given registry.
func NewDynamicToolBuilder(registry *ToolRegistry) *DynamicToolBuilder {
	return &DynamicToolBuilder{
		registry:       registry,
		maxDirectTools: 20,
		maxDynamic:     15,
	}
}

// BuildAll returns tool definitions for every available tool (no filtering).
func (b *DynamicToolBuilder) BuildAll() []map[string]interface{} {
	tools := b.registry.ListAvailable()
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		out = append(out, registeredToolToDef(t))
	}
	return out
}

// Build returns tool definitions, applying context-aware filtering when
// the number of available tools exceeds maxDirectTools.
// userMessage is used for relevance scoring when filtering is active.
func (b *DynamicToolBuilder) Build(userMessage string) []map[string]interface{} {
	tools := b.registry.ListAvailable()
	if len(tools) <= b.maxDirectTools {
		out := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			out = append(out, registeredToolToDef(t))
		}
		return out
	}

	// Detect group activation keywords in user message.
	groupTags := detectGroupTags(userMessage)

	// Split into builtin (always included), group-activated, and dynamic (scored).
	var builtins, groupActivated, dynamic []RegisteredTool
	for _, t := range tools {
		if t.Category == ToolCategoryBuiltin {
			builtins = append(builtins, t)
			continue
		}
		// Check if tool matches any group-activated tags.
		if len(groupTags) > 0 {
			matched := false
			for _, tag := range t.Tags {
				if groupTags[strings.ToLower(tag)] {
					matched = true
					break
				}
			}
			if matched {
				groupActivated = append(groupActivated, t)
				continue
			}
		}
		dynamic = append(dynamic, t)
	}

	// Score remaining dynamic tools by keyword overlap with userMessage.
	msgTokens := tokenizeSimple(userMessage)
	type scored struct {
		tool  RegisteredTool
		score float64
	}
	scored_list := make([]scored, 0, len(dynamic))
	for _, t := range dynamic {
		s := scoreTool(t, msgTokens)
		scored_list = append(scored_list, scored{tool: t, score: s})
	}
	sort.Slice(scored_list, func(i, j int) bool {
		return scored_list[i].score > scored_list[j].score
	})

	limit := b.maxDynamic - len(groupActivated)
	if limit < 0 {
		limit = 0
	}
	if limit > len(scored_list) {
		limit = len(scored_list)
	}

	out := make([]map[string]interface{}, 0, len(builtins)+len(groupActivated)+limit)
	for _, t := range builtins {
		out = append(out, registeredToolToDef(t))
	}
	for _, t := range groupActivated {
		out = append(out, registeredToolToDef(t))
	}
	for i := 0; i < limit; i++ {
		out = append(out, registeredToolToDef(scored_list[i].tool))
	}
	return out
}

// registeredToolToDef converts a RegisteredTool to an OpenAI function calling definition.
func registeredToolToDef(t RegisteredTool) map[string]interface{} {
	params := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
	if t.InputSchema != nil && len(t.InputSchema) > 0 {
		params["properties"] = t.InputSchema
	}
	if len(t.Required) > 0 {
		params["required"] = t.Required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  params,
		},
	}
}

// scoreTool computes a relevance score for a tool against user message tokens.
func scoreTool(t RegisteredTool, msgTokens []string) float64 {
	if len(msgTokens) == 0 {
		return 0
	}
	// Collect tool tokens from name, description, and tags.
	var toolToks []string
	toolToks = append(toolToks, tokenizeSimple(t.Name)...)
	toolToks = append(toolToks, tokenizeSimple(t.Description)...)
	for _, tag := range t.Tags {
		toolToks = append(toolToks, tokenizeSimple(tag)...)
	}
	toolSet := make(map[string]bool, len(toolToks))
	for _, tok := range toolToks {
		toolSet[tok] = true
	}
	var hits float64
	for _, mt := range msgTokens {
		if toolSet[mt] {
			hits++
		}
	}
	// Add priority bonus (normalized).
	return hits/float64(len(msgTokens)) + float64(t.Priority)*0.01
}

// groupKeywords maps user-facing group names (Chinese and English) to tag sets.
// When a user message contains a group keyword, all tools matching those tags
// are forcibly included regardless of the scoring threshold.
var groupKeywords = map[string][]string{
	"数据库":    {"database", "sql", "query", "db"},
	"database": {"database", "sql", "query", "db"},
	"git":      {"git", "vcs", "version"},
	"版本控制":   {"git", "vcs", "version"},
	"文件":     {"file", "read", "write", "directory"},
	"file":    {"file", "read", "write", "directory"},
	"mcp":     {"mcp"},
	"skill":   {"skill"},
	"技能":     {"skill"},
	"会话":     {"session"},
	"session": {"session"},
	"配置":     {"config", "settings"},
	"config":  {"config", "settings"},
	"记忆":     {"memory"},
	"memory":  {"memory"},
	"定时":     {"schedule", "task", "cron", "timer"},
	"schedule": {"schedule", "task", "cron", "timer"},
	"网络":     {"network", "clawnet", "p2p"},
	"network": {"network", "clawnet", "p2p"},
}

// detectGroupTags checks if the user message contains any group activation
// keywords and returns the union of matching tag sets.
func detectGroupTags(userMessage string) map[string]bool {
	msg := strings.ToLower(userMessage)
	tags := make(map[string]bool)
	for keyword, tagList := range groupKeywords {
		if strings.Contains(msg, keyword) {
			for _, t := range tagList {
				tags[t] = true
			}
		}
	}
	return tags
}

// tokenizeSimple splits text into lowercase tokens on common delimiters.
func tokenizeSimple(text string) []string {
	text = strings.ToLower(text)
	f := func(r rune) bool {
		return r == ' ' || r == '_' || r == '-' || r == '/' || r == '.' ||
			r == ',' || r == '(' || r == ')' || r == '\n' || r == '\t'
	}
	parts := strings.FieldsFunc(text, f)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) > 0 {
			out = append(out, p)
		}
	}
	return out
}
