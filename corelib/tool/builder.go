package tool

import (
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// DynamicToolBuilder builds LLM tool definitions dynamically from the Registry.
// When the total available tools exceed maxDirectTools, it applies context-aware
// filtering: all builtin tools are always included, and the remaining slots are
// filled by the most relevant dynamic tools based on keyword similarity.
type DynamicToolBuilder struct {
	registry       *Registry
	maxDirectTools int // threshold before filtering kicks in (default 20)
	maxDynamic     int // max non-builtin tools when filtering (default 15)
}

// NewDynamicToolBuilder creates a builder backed by the given registry.
func NewDynamicToolBuilder(registry *Registry) *DynamicToolBuilder {
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
		out = append(out, RegisteredToolToDef(t))
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
			out = append(out, RegisteredToolToDef(t))
		}
		return out
	}

	// Detect group activation keywords in user message.
	groupTags := DetectGroupTags(userMessage)

	// Split into builtin (always included), group-activated, and dynamic (scored).
	var builtins, groupActivated, dynamic []RegisteredTool
	for _, t := range tools {
		if t.Category == CategoryBuiltin {
			builtins = append(builtins, t)
			continue
		}
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

	// Score remaining dynamic tools using BM25.
	docs := make([]bm25.Doc, len(dynamic))
	for i, t := range dynamic {
		text := t.Name + " " + t.Description
		for _, tag := range t.Tags {
			text += " " + tag
		}
		docs[i] = bm25.Doc{ID: t.Name, Text: text}
	}
	idx := bm25.New()
	idx.Rebuild(docs)
	bm25Scores := idx.Score(userMessage)

	type scored struct {
		tool  RegisteredTool
		score float64
	}
	scoredList := make([]scored, 0, len(dynamic))
	for _, t := range dynamic {
		s := bm25Scores[t.Name] + float64(t.Priority)*0.01
		scoredList = append(scoredList, scored{tool: t, score: s})
	}
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	limit := b.maxDynamic - len(groupActivated)
	if limit < 0 {
		limit = 0
	}
	if limit > len(scoredList) {
		limit = len(scoredList)
	}

	out := make([]map[string]interface{}, 0, len(builtins)+len(groupActivated)+limit)
	for _, t := range builtins {
		out = append(out, RegisteredToolToDef(t))
	}
	for _, t := range groupActivated {
		out = append(out, RegisteredToolToDef(t))
	}
	for i := 0; i < limit; i++ {
		out = append(out, RegisteredToolToDef(scoredList[i].tool))
	}
	return out
}

// RegisteredToolToDef converts a RegisteredTool to an OpenAI function calling definition.
func RegisteredToolToDef(t RegisteredTool) map[string]interface{} {
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

// ScoreTool computes a relevance score for a tool against user message tokens.
func ScoreTool(t RegisteredTool, msgTokens []string) float64 {
	if len(msgTokens) == 0 {
		return 0
	}
	var toolToks []string
	toolToks = append(toolToks, TokenizeSimple(t.Name)...)
	toolToks = append(toolToks, TokenizeSimple(t.Description)...)
	for _, tag := range t.Tags {
		toolToks = append(toolToks, TokenizeSimple(tag)...)
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
	return hits/float64(len(msgTokens)) + float64(t.Priority)*0.01
}

// GroupKeywords maps user-facing group names (Chinese and English) to tag sets.
var GroupKeywords = map[string][]string{
	"数据库":      {"database", "sql", "query", "db"},
	"database":  {"database", "sql", "query", "db"},
	"git":       {"git", "vcs", "version"},
	"版本控制":     {"git", "vcs", "version"},
	"文件":       {"file", "read", "write", "directory"},
	"file":      {"file", "read", "write", "directory"},
	"mcp":       {"mcp"},
	"skill":     {"skill"},
	"技能":       {"skill"},
	"会话":       {"session"},
	"session":   {"session"},
	"配置":       {"config", "settings"},
	"config":    {"config", "settings"},
	"记忆":       {"memory"},
	"memory":    {"memory"},
	"定时":       {"schedule", "task", "cron", "timer"},
	"schedule":  {"schedule", "task", "cron", "timer"},
	"网络":       {"network", "clawnet", "p2p"},
	"network":   {"network", "clawnet", "p2p"},
}

// DetectGroupTags checks if the user message contains any group activation
// keywords and returns the union of matching tag sets.
func DetectGroupTags(userMessage string) map[string]bool {
	msg := strings.ToLower(userMessage)
	tags := make(map[string]bool)
	for keyword, tagList := range GroupKeywords {
		if strings.Contains(msg, keyword) {
			for _, t := range tagList {
				tags[t] = true
			}
		}
	}
	return tags
}

// TokenizeSimple splits text into lowercase tokens on common delimiters.
func TokenizeSimple(text string) []string {
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
