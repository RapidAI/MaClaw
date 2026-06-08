package main

import (
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CodeFileEvent is the payload for code:file_update events.
type CodeFileEvent struct {
	SessionID   string `json:"session_id"`
	FilePath    string `json:"file_path"`
	FileName    string `json:"file_name"`
	Content     string `json:"content"`
	Original    string `json:"original,omitempty"`    // empty for new files
	OpType      string `json:"op_type"`               // "create" or "modify"
	Language    string `json:"language"`              // detected from extension
	ForceOpen   bool   `json:"force_open,omitempty"`  // true when backend should override a manually closed preview
	ProjectPath string `json:"project_path,omitempty"` // project/working directory for frontend tab routing
}

// CodeEventEmitter emits code file events to the frontend via Wails runtime.
type CodeEventEmitter struct {
	app *App
}

// NewCodeEventEmitter creates a new CodeEventEmitter.
func NewCodeEventEmitter(app *App) *CodeEventEmitter {
	return &CodeEventEmitter{app: app}
}

// EmitCodeFileEvent emits a code:file_update event to the frontend.
// If app.ctx is nil, the call is silently skipped.
func (e *CodeEventEmitter) EmitCodeFileEvent(evt CodeFileEvent) {
	if e.app.ctx == nil {
		return
	}
	runtime.EventsEmit(e.app.ctx, "code:file_update", evt)
}

// EmitSessionStart emits a code:session_start event when a coding session begins.
// If app.ctx is nil, the call is silently skipped.
func (e *CodeEventEmitter) EmitSessionStart(sessionID string, projectPath ...string) {
	if e.app.ctx == nil {
		return
	}
	payload := map[string]string{
		"session_id": sessionID,
	}
	if len(projectPath) > 0 && projectPath[0] != "" {
		payload["project_path"] = projectPath[0]
	}
	runtime.EventsEmit(e.app.ctx, "code:session_start", payload)
}

// EmitSessionEnd emits a code:session_end event when a coding session completes.
// If app.ctx is nil, the call is silently skipped.
func (e *CodeEventEmitter) EmitSessionEnd(sessionID string, projectPath ...string) {
	if e.app.ctx == nil {
		return
	}
	payload := map[string]string{
		"session_id": sessionID,
	}
	if len(projectPath) > 0 && projectPath[0] != "" {
		payload["project_path"] = projectPath[0]
	}
	runtime.EventsEmit(e.app.ctx, "code:session_end", payload)
}

// detectLanguageFromExt maps a file extension to a language identifier.
// Returns "plaintext" for unknown extensions.
func detectLanguageFromExt(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	case ".sh", ".bash":
		return "shell"
	default:
		return "plaintext"
	}
}
