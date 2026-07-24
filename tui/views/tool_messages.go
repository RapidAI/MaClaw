package views

// tool_messages.go defines a unified result message for all tool-tab async operations.
// This eliminates the bug where MCP add errors were sent as ToolSkillSearchResultMsg
// (wrong type, handled by the wrong branch in Update).

// ToolOperationResultMsg is the unified result for any async tool-tab operation
// (skill install, MCP add, etc.). The tool view's Update handles this single type.
type ToolOperationResultMsg struct {
	Tab                   int // ToolSubSkill or ToolSubMCP — which sub-tab to update
	Success               bool
	Message               string
	InstalledName         string // populated for successful Skill installs and duplicate detection
	InstalledSearchResult string // source-aware identity of the result that initiated install
}
