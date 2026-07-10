package main

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CodeFileEvent is the payload for code:file_update events.
type CodeFileEvent struct {
	SessionID   string `json:"session_id"`
	FilePath    string `json:"file_path"`
	FileName    string `json:"file_name"`
	AbsPath     string `json:"abs_path,omitempty"` // absolute path for tooltip/context menu
	Content     string `json:"content"`
	Original    string `json:"original"`               // empty for new files or empty original content
	OpType      string `json:"op_type"`                // "create", "modify", or "read"
	Language    string `json:"language"`               // detected from extension
	ForceOpen   bool   `json:"force_open,omitempty"`   // true when backend should override a manually closed preview
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
	if e == nil || e.app == nil {
		return
	}
	log.Printf("[code-event] emit file_update session=%q op=%s force_open=%v project=%q file=%q content_len=%d original_len=%d", evt.SessionID, evt.OpType, evt.ForceOpen, evt.ProjectPath, evt.FilePath, len(evt.Content), len(evt.Original))
	if e.app.ctx == nil {
		log.Printf("[code-event] skip file_update session=%q file=%q: app context is nil", evt.SessionID, evt.FilePath)
		return
	}
	runtime.EventsEmit(e.app.ctx, "code:file_update", evt)
}

// EmitSessionStart emits a code:session_start event when a coding session begins.
// If app.ctx is nil, the call is silently skipped.
func (e *CodeEventEmitter) EmitSessionStart(sessionID string, projectPath ...string) {
	if e == nil || e.app == nil {
		return
	}
	if e.app.ctx == nil {
		log.Printf("[code-event] skip session_start session=%q: app context is nil", sessionID)
		return
	}
	payload := map[string]string{
		"session_id": sessionID,
	}
	if len(projectPath) > 0 && projectPath[0] != "" {
		payload["project_path"] = projectPath[0]
	}
	log.Printf("[code-event] emit session_start session=%q project=%q", sessionID, payload["project_path"])
	runtime.EventsEmit(e.app.ctx, "code:session_start", payload)
}

// EmitSessionEnd emits a code:session_end event when a coding session completes.
// If app.ctx is nil, the call is silently skipped.
func (e *CodeEventEmitter) EmitSessionEnd(sessionID string, projectPath ...string) {
	if e == nil || e.app == nil {
		return
	}
	if e.app.ctx == nil {
		log.Printf("[code-event] skip session_end session=%q: app context is nil", sessionID)
		return
	}
	payload := map[string]string{
		"session_id": sessionID,
	}
	if len(projectPath) > 0 && projectPath[0] != "" {
		payload["project_path"] = projectPath[0]
	}
	log.Printf("[code-event] emit session_end session=%q project=%q", sessionID, payload["project_path"])
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
	case ".bat", ".cmd":
		return "batch"
	case ".ps1", ".psm1", ".psd1":
		return "powershell"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".cs":
		return "csharp"
	case ".sql":
		return "sql"
	case ".r", ".R":
		return "r"
	case ".lua":
		return "lua"
	case ".toml":
		return "toml"
	case ".xml", ".xsl", ".xsd", ".svg":
		return "xml"
	case ".dockerfile":
		return "dockerfile"
	case ".tf", ".hcl":
		return "hcl"
	case ".cmake":
		return "cmake"
	default:
		// Handle special filenames without extensions
		baseName := strings.ToLower(filepath.Base(fileName))
		switch baseName {
		case "dockerfile", "containerfile":
			return "dockerfile"
		case "makefile", "gnumakefile":
			return "makefile"
		case "cmakelists.txt":
			return "cmake"
		case ".gitignore", ".dockerignore":
			return "shell"
		}
		return "plaintext"
	}
}
