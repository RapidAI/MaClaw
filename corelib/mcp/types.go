package mcp

// ToolEntry represents a tool with its server context for filtering and display.
type ToolEntry struct {
	ServerName   string
	ServerID     string
	SourceType   string // "local/stdio" or "remote/HTTP"
	HealthStatus string
	ToolName     string
	Description  string
	InputSchema  map[string]interface{}
}
