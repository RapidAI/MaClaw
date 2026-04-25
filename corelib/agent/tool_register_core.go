package agent

// tool_register_core.go registers all platform-agnostic tools into a
// CoreToolRegistry. Each tool's definition and handler are bound together
// in a single registration call, so there are no separate lists to keep in sync.
//
// Dependencies (memory store, SSH handler, task store, etc.) are passed
// via CoreToolDeps. Nil deps gracefully disable the corresponding tools.

import (
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/task"
)

// CoreToolDeps holds the dependencies needed by core tool handlers.
// All fields are optional. Nil fields cause the corresponding tools
// to return a friendly "not initialized" style message.
type CoreToolDeps struct {
	MemoryStore *memory.Store
	TaskStore   *task.Store

	// SSHHandler is injected by the host to avoid import cycles.
	// The host can wrap its own SSH implementation and expose it here.
	SSHHandler ToolHandler

	// WebSearchHandler handles web_search. Injected by host.
	WebSearchHandler ToolHandler

	// WebFetchHandler handles web_fetch. Injected by host.
	WebFetchHandler ToolHandler

	// OnBashProgress is called periodically during long-running bash commands.
	OnBashProgress func(msg string)

	// ExtraHandlers maps tool names to host-provided handlers.
	// This is the extension point for tools that need platform-specific
	// implementations (e.g. manage_skill, screenshot) without changing
	// this struct every time a new tool is added.
	//
	// RegisterCoreTools checks this map when registering tools that
	// require host injection. If a handler is found, the tool is
	// registered with that handler. If not, a "not initialized" stub
	// is used, and the tool still appears in BuildDefinitions() so the
	// LLM knows it exists but gets a helpful error if it tries to call it.
	ExtraHandlers map[string]ToolHandler
}

// RegisterCoreTools registers all platform-agnostic tools into the registry.
// This is the single source of truth for tool definitions and handlers.
func RegisterCoreTools(r *CoreToolRegistry, deps CoreToolDeps) {
	r.Register(ToolEntry{
		Name:        "bash",
		Description: "Run a shell command. Use the built-in ssh tool instead of invoking ssh/scp/rsync through bash.",
		Properties: map[string]interface{}{
			"command":     map[string]string{"type": "string", "description": "Shell command to execute"},
			"working_dir": map[string]string{"type": "string", "description": "Working directory (optional)"},
			"timeout":     map[string]string{"type": "integer", "description": "Timeout seconds, default 30, max 120"},
		},
		Required: []string{"command"},
		Handler:  func(args map[string]interface{}) string { return ToolBash(args, deps.OnBashProgress) },
	})

	r.Register(ToolEntry{
		Name:        "read_file",
		Description: "Read file content. Small files returned in full; large files return a structure outline + preview. Use start_line to read specific sections.",
		Properties: map[string]interface{}{
			"path":       map[string]string{"type": "string", "description": "File path"},
			"lines":      map[string]string{"type": "integer", "description": "Max lines to read (optional, default 200). Specifying this bypasses adaptive mode."},
			"start_line": map[string]string{"type": "integer", "description": "Starting line number, 1-based (optional). Specifying this bypasses adaptive mode."},
		},
		Required: []string{"path"},
		Handler:  func(args map[string]interface{}) string { return ToolReadFile(args) },
	})

	r.Register(ToolEntry{
		Name:        "write_file",
		Description: "Write a UTF-8 text file. mode=overwrite replaces content, mode=append appends content.",
		Properties: map[string]interface{}{
			"path":    map[string]string{"type": "string", "description": "File path"},
			"content": map[string]string{"type": "string", "description": "File content"},
			"mode":    map[string]string{"type": "string", "description": "Write mode: overwrite or append"},
		},
		Required: []string{"path", "content"},
		Handler:  func(args map[string]interface{}) string { return ToolWriteFile(args) },
	})

	r.Register(ToolEntry{
		Name:        "edit_file",
		Description: "Edit a file by replacing old_string with new_string.",
		Properties: map[string]interface{}{
			"path":        map[string]string{"type": "string", "description": "File path"},
			"old_string":  map[string]string{"type": "string", "description": "Text to search for"},
			"new_string":  map[string]string{"type": "string", "description": "Replacement text"},
			"replace_all": map[string]string{"type": "boolean", "description": "Whether to replace all matches, default false"},
		},
		Required: []string{"path", "old_string", "new_string"},
		Handler:  func(args map[string]interface{}) string { return ToolEditFile(args) },
	})

	r.Register(ToolEntry{
		Name:        "list_directory",
		Description: "List directory contents.",
		Properties: map[string]interface{}{
			"path": map[string]string{"type": "string", "description": "Directory path"},
		},
		Required: []string{"path"},
		Handler:  func(args map[string]interface{}) string { return ToolListDirectory(args) },
	})

	r.Register(ToolEntry{
		Name:        "send_file",
		Description: "Read a file and send it to the user.",
		Properties: map[string]interface{}{
			"path":          map[string]string{"type": "string", "description": "File path"},
			"file_name":     map[string]string{"type": "string", "description": "Display file name (optional)"},
			"forward_to_im": map[string]string{"type": "boolean", "description": "Whether to forward through IM"},
		},
		Required: []string{"path"},
		Handler:  func(args map[string]interface{}) string { return ToolSendFile(args) },
	})

	r.Register(ToolEntry{
		Name:        "open",
		Description: "Open a file or URL with the system default application.",
		Properties: map[string]interface{}{
			"target": map[string]string{"type": "string", "description": "File path or URL"},
		},
		Required: []string{"target"},
		Handler:  func(args map[string]interface{}) string { return ToolOpen(args) },
	})

	r.Register(ToolEntry{
		Name:        "memory",
		Description: "Manage long-term memory with actions save, recall, list, and delete.",
		Properties: map[string]interface{}{
			"action":   map[string]string{"type": "string", "description": "Action: save, recall, list, delete"},
			"content":  map[string]string{"type": "string", "description": "Memory content for save"},
			"category": map[string]string{"type": "string", "description": "Optional category for save"},
			"query":    map[string]string{"type": "string", "description": "Search query for recall"},
			"keyword":  map[string]string{"type": "string", "description": "Optional keyword for list"},
			"id":       map[string]string{"type": "string", "description": "Memory ID for delete"},
		},
		Required: []string{"action"},
		Handler:  func(args map[string]interface{}) string { return ToolMemory(deps.MemoryStore, args) },
	})

	r.Register(ToolEntry{
		Name:        "ssh",
		Description: "Manage SSH connections and remote operations such as connect, exec, background exec, upload, download, list, and close.",
		Properties: map[string]interface{}{
			"action":          map[string]string{"type": "string", "description": "Action such as connect, exec, exec_background, check_task, list_tasks, kill_task, sudo_prepare, upload, download, list, close"},
			"host":            map[string]string{"type": "string", "description": "Remote host for connect"},
			"user":            map[string]string{"type": "string", "description": "Login username for connect"},
			"port":            map[string]string{"type": "integer", "description": "SSH port, default 22"},
			"auth_method":     map[string]string{"type": "string", "description": "Authentication method: password, key, or agent"},
			"key_path":        map[string]string{"type": "string", "description": "Private key path"},
			"password":        map[string]string{"type": "string", "description": "SSH password"},
			"label":           map[string]string{"type": "string", "description": "Optional host label"},
			"initial_command": map[string]string{"type": "string", "description": "Command to run immediately after connect"},
			"session_id":      map[string]string{"type": "string", "description": "SSH session ID"},
			"command":         map[string]string{"type": "string", "description": "Remote command"},
			"wait_seconds":    map[string]string{"type": "integer", "description": "Wait time for output, default 5"},
			"task_id":         map[string]string{"type": "string", "description": "Background task ID"},
			"tail_lines":      map[string]string{"type": "integer", "description": "Tail lines for task output"},
			"local_path":      map[string]string{"type": "string", "description": "Local file path"},
			"remote_path":     map[string]string{"type": "string", "description": "Remote file path"},
		},
		Required: []string{"action"},
		Handler: func() ToolHandler {
			if deps.SSHHandler != nil {
				return deps.SSHHandler
			}
			return func(args map[string]interface{}) string {
				return "SSH handler is not initialized (????). Please configure SSH support first."
			}
		}(),
	})

	r.Register(ToolEntry{
		Name:        "ask_user",
		Description: "Ask the user a structured follow-up question and wait for an answer.",
		Properties: map[string]interface{}{
			"question":   map[string]string{"type": "string", "description": "Question to ask"},
			"options":    map[string]interface{}{"type": "array", "description": "Optional choices", "items": map[string]string{"type": "string"}},
			"context":    map[string]string{"type": "string", "description": "Optional background context"},
			"input_type": map[string]string{"type": "string", "description": "Input type: text, choice, or confirm"},
		},
		Required: []string{"question"},
		Handler:  func(args map[string]interface{}) string { return ToolAskUser(args) },
	})

	r.Register(ToolEntry{
		Name:        "task",
		Description: "Manage the internal task checklist with actions such as create, update, complete, fail, list, and delete.",
		Properties: map[string]interface{}{
			"action":      map[string]string{"type": "string", "description": "Action: create, update, complete, fail, list, delete"},
			"task_id":     map[string]string{"type": "string", "description": "Task ID"},
			"title":       map[string]string{"type": "string", "description": "Task title for create"},
			"description": map[string]string{"type": "string", "description": "Task description"},
			"depends_on":  map[string]interface{}{"type": "array", "description": "Dependency task IDs", "items": map[string]string{"type": "string"}},
			"status_note": map[string]string{"type": "string", "description": "Optional status note"},
			"delegate_to": map[string]string{"type": "string", "description": "Optional delegation target"},
		},
		Required: []string{"action"},
		Handler:  func(args map[string]interface{}) string { return ToolTask(deps.TaskStore, args) },
	})

	r.Register(ToolEntry{
		Name:        "read_excel",
		Description: "Read an Excel file.",
		Properties: map[string]interface{}{
			"file_path": map[string]string{"type": "string", "description": "Excel file path"},
			"sheet":     map[string]string{"type": "string", "description": "Optional sheet name"},
		},
		Required: []string{"file_path"},
		Handler:  func(args map[string]interface{}) string { return ToolReadExcel(args) },
	})

	r.Register(ToolEntry{
		Name:        "write_excel",
		Description: "Write an Excel file.",
		Properties: map[string]interface{}{
			"file_path": map[string]string{"type": "string", "description": "Excel file path"},
			"sheet":     map[string]string{"type": "string", "description": "Optional sheet name"},
			"data":      map[string]string{"type": "array", "description": "Two-dimensional array data"},
		},
		Required: []string{"file_path", "data"},
		Handler:  func(args map[string]interface{}) string { return ToolWriteExcel(args) },
	})

	r.Register(ToolEntry{
		Name:        "read_pptx",
		Description: "Read a PowerPoint file.",
		Properties: map[string]interface{}{
			"file_path": map[string]string{"type": "string", "description": "PPTX file path"},
		},
		Required: []string{"file_path"},
		Handler:  func(args map[string]interface{}) string { return ToolReadPPTX(args) },
	})

	r.Register(ToolEntry{
		Name:        "web_search",
		Description: "Search the internet. Returns a list of results with title, URL, and snippet.",
		Properties: map[string]interface{}{
			"query":       map[string]string{"type": "string", "description": "Search query"},
			"max_results": map[string]string{"type": "integer", "description": "Max results, default 8, max 20"},
		},
		Required: []string{"query"},
		Handler: func() ToolHandler {
			if deps.WebSearchHandler != nil {
				return deps.WebSearchHandler
			}
			return func(args map[string]interface{}) string {
				return "Web search is not configured. Please set up a web search provider."
			}
		}(),
	})

	r.Register(ToolEntry{
		Name:        "web_fetch",
		Description: "Fetch and extract text content from a URL. Supports encoding detection and HTML extraction.",
		Properties: map[string]interface{}{
			"url":       map[string]string{"type": "string", "description": "URL to fetch"},
			"render_js": map[string]string{"type": "boolean", "description": "Use Chrome to render JS (optional, default false)"},
			"save_path": map[string]string{"type": "string", "description": "Save file path (optional, downloads file instead of returning text)"},
			"timeout":   map[string]string{"type": "integer", "description": "Timeout seconds, default 30, max 120"},
			"offset":    map[string]string{"type": "integer", "description": "Character offset for pagination (default 0)"},
			"max_chars": map[string]string{"type": "integer", "description": "Max characters to return (optional)"},
		},
		Required: []string{"url"},
		Handler: func() ToolHandler {
			if deps.WebFetchHandler != nil {
				return deps.WebFetchHandler
			}
			return func(args map[string]interface{}) string {
				return "Web fetch is not configured."
			}
		}(),
	})

	// --- Tools requiring host-injected handlers via ExtraHandlers ---

	r.Register(ToolEntry{
		Name:        "manage_skill",
		Description: skill.ManageSkillDescription(),
		Properties: map[string]interface{}{
			"action":       map[string]string{"type": "string", "description": "操作: " + skill.ManageSkillActionSlash()},
			"query":        map[string]string{"type": "string", "description": "搜索关键词（search 时必填）"},
			"skill_id":     map[string]string{"type": "string", "description": "Skill ID（install 时必填，从 search 结果中获取）"},
			"hub_url":      map[string]string{"type": "string", "description": "来源 Hub URL（install 时可选）"},
			"name":         map[string]string{"type": "string", "description": "Skill 名称（run/upload/validate 时必填）"},
			"skill_name":   map[string]string{"type": "string", "description": "Skill 名称（patch/history 时必填）"},
			"find":         map[string]string{"type": "string", "description": "要查找的原始文本（patch 时必填，必须精确匹配唯一一处）"},
			"replace":      map[string]string{"type": "string", "description": "替换后的新文本（patch 时必填）"},
			"reason":       map[string]string{"type": "string", "description": "修补原因说明（patch 时可选）"},
			"args":         map[string]string{"type": "object", "description": "Skill 运行参数（run 时按需传入）"},
			"operation":    map[string]string{"type": "string", "description": "执行指定的 operation（run 时可选）"},
			"input":        map[string]string{"type": "string", "description": "兼容旧调用的输入参数（run 时可选）"},
			"output":       map[string]string{"type": "string", "description": "兼容旧调用的输出参数（run 时可选）"},
			"user_prompt":  map[string]string{"type": "string", "description": "用户的原始请求文本（run 时可选）"},
			"run_id":       map[string]string{"type": "string", "description": "运行 ID（status 时必填）"},
			"auto_fix":     map[string]string{"type": "boolean", "description": "与 validate 配合，为 true 时自动修复可移植性问题（可选，默认 false）"},
		},
		Required: []string{"action"},
		Handler:  extraHandler(deps, "manage_skill", "Skill 管理未初始化。请检查配置。"),
	})

	r.Register(ToolEntry{
		Name:        "screenshot",
		Description: "截取屏幕截图并发送给用户。",
		Properties: map[string]interface{}{
			"display": map[string]string{"type": "integer", "description": "显示器编号（可选，0=主屏，1=第二屏，不传则截取所有屏幕）"},
		},
		Handler: extraHandler(deps, "screenshot", "截图功能在当前环境下不可用。"),
	})
}

// extraHandler returns the host-injected handler for the given tool name,
// or a stub that returns the fallback message.
func extraHandler(deps CoreToolDeps, name, fallback string) ToolHandler {
	if deps.ExtraHandlers != nil {
		if h, ok := deps.ExtraHandlers[name]; ok && h != nil {
			return h
		}
	}
	return func(args map[string]interface{}) string { return fallback }
}
