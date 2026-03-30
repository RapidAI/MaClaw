package tool

// Category classifies a tool's origin.
type Category string

const (
	CategoryBuiltin Category = "builtin"
	CategoryMCP     Category = "mcp"
	CategorySkill   Category = "skill"
	CategoryNonCode Category = "non_code"
)

// Status describes the availability of a registered tool.
type Status string

const (
	StatusAvailable   Status = "available"
	StatusDegraded    Status = "degraded"
	StatusUnavailable Status = "unavailable"
)

// ProgressCallback delivers incremental progress updates to the user.
type ProgressCallback func(text string)

// Handler is a simple tool handler.
type Handler func(args map[string]interface{}) string

// HandlerWithProgress is a tool handler that supports progress reporting.
type HandlerWithProgress func(args map[string]interface{}, onProgress ProgressCallback) string

// CapRequirement describes a tool's platform dependency.
type CapRequirement struct {
	RequiresDisplay   bool // needs display server (screenshot, screen brightness, etc.)
	RequiresClipboard bool // needs clipboard
	RequiresNetwork   bool // needs network connection
}

// PlatformChecker abstracts platform capability checks for tool filtering.
type PlatformChecker interface {
	HasDisplay() bool
	HasClipboard() bool
}

// RegisteredTool describes a tool registered in the registry.
type RegisteredTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Category    Category               `json:"category"`
	Tags        []string               `json:"tags"`
	Priority    int                    `json:"priority"`
	Status      Status                 `json:"status"`
	InputSchema map[string]interface{} `json:"input_schema"`
	Required    []string               `json:"required"`
	Source      string                 `json:"source"`
	Body        string                 `json:"body,omitempty"`
	BodySummary string                 `json:"body_summary,omitempty"`
	Caps        CapRequirement         `json:"-"`
	Handler     Handler                `json:"-"`
	HandlerProg HandlerWithProgress    `json:"-"`
}
