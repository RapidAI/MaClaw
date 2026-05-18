package memory

// ToolDefinition describes the shared memory tool schema exposed by GUI, TUI,
// and server agents. Keep capabilities here in sync with HandleTool.
type ToolDefinition struct {
	Description string
	Properties  map[string]interface{}
	Required    []string
	Tags        []string
}

// ToolDefinitionSchema returns the canonical memory tool schema shared by all
// agent frontends.
func ToolDefinitionSchema() ToolDefinition {
	return ToolDefinition{
		Description: "Manage long-term memory with actions save, recall, candidates, themes, scenes, trace, list, and delete. Saves are governed; recall supports dynamic, hybrid, lightmem, adaptive, and auto modes.",
		Required:    []string{"action"},
		Tags:        []string{"memory", "save", "remember", "list", "search", "delete", "recall", "themes", "candidates", "scenes", "trace"},
		Properties: map[string]interface{}{
			"action":         map[string]string{"type": "string", "description": "Action: save, recall, candidates, themes, scenes, trace, list, delete"},
			"query":          map[string]string{"type": "string", "description": "Search query for recall"},
			"content":        map[string]string{"type": "string", "description": "Memory content for save"},
			"category":       map[string]string{"type": "string", "description": "Category: user_fact, project_knowledge, preference, instruction"},
			"mode":           map[string]string{"type": "string", "description": "Recall mode: dynamic, hybrid, lightmem, adaptive, auto"},
			"debug":          map[string]string{"type": "boolean", "description": "Include recall trace and adaptive/lightmem plan when available"},
			"stats":          map[string]string{"type": "boolean", "description": "Include theme health diagnostics for action=themes"},
			"tags":           map[string]interface{}{"type": "array", "description": "Specific entity names for save/search anchors", "items": map[string]string{"type": "string"}},
			"keyword":        map[string]string{"type": "string", "description": "Keyword filter for list/candidates"},
			"limit":          map[string]string{"type": "integer", "description": "Optional result limit"},
			"project_path":   map[string]string{"type": "string", "description": "Optional project path for scoped recall"},
			"evidence":       map[string]string{"type": "boolean", "description": "For action=themes, include representative source memories"},
			"evidence_limit": map[string]string{"type": "integer", "description": "Representative evidence entries per theme"},
			"diagnose":       map[string]string{"type": "boolean", "description": "For action=themes, include actionable theme diagnostics"},
			"issue_limit":    map[string]string{"type": "integer", "description": "Maximum diagnostic issues for action=themes"},
			"plan":           map[string]string{"type": "boolean", "description": "For action=themes, include a non-destructive maintenance plan"},
			"action_limit":   map[string]string{"type": "integer", "description": "Maximum maintenance actions for action=themes"},
			"apply":          map[string]string{"type": "boolean", "description": "For action=themes, apply safe maintenance actions"},
			"id":             map[string]string{"type": "string", "description": "Memory ID for delete"},
		},
	}
}
