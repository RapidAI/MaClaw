package agent

// tool_register_core.go registers all platform-agnostic tools into a
// CoreToolRegistry. Each tool's definition and handler are bound together
// in a single registration call, so there are no separate lists to keep in sync.
//
// Dependencies (memory store, SSH handler, task store, etc.) are passed
// via CoreToolDeps. Nil deps gracefully disable the corresponding tools.

import (
	"context"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/task"
)

const coreInlineToolPayloadMaxLength = 1800

// CoreToolDeps holds the dependencies needed by core tool handlers.
// All fields are optional. Nil fields cause the corresponding tools
// to return a friendly "not initialized" style message.
type CoreToolDeps struct {
	MemoryStore *memory.Store
	TaskStore   *task.Store
	GoalStore   *goal.Store

	// SecurityGuard can reject a tool call before the handler runs.
	// Hosts use this to apply centrally managed security policy.
	SecurityGuard func(name string, args map[string]interface{}) (bool, string)

	// SSHHandler is injected by the host to avoid import cycles.
	// The host can wrap its own SSH implementation and expose it here.
	SSHHandler ToolHandler

	// WebSearchHandler handles web_search. Injected by host.
	WebSearchHandler ToolHandler
	// WebSearchHandlerCtx handles web_search with cancellation. Injected by host.
	WebSearchHandlerCtx ToolHandlerCtx

	// WebFetchHandler handles web_fetch. Injected by host.
	WebFetchHandler ToolHandler
	// WebFetchHandlerCtx handles web_fetch with cancellation. Injected by host.
	WebFetchHandlerCtx ToolHandlerCtx

	// OnBashProgress is called periodically during long-running bash commands.
	OnBashProgress func(msg string)

	// LoopIDFunc returns the current agent loop ID for scroll session scoping.
	// Called each time the memory tool handles a recall with session=true.
	// Nil means no loop ID is available (scroll sessions disabled).
	LoopIDFunc func() string

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
			"timeout":     map[string]string{"type": "integer", "description": "Timeout seconds, default 600, range 240-600"},
		},
		Required: []string{"command"},
		HandlerCtx: guardedHandlerCtx(deps, "bash", func(ctx context.Context, args map[string]interface{}) string {
			return ToolBashWithContext(ctx, args, deps.OnBashProgress)
		}),
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
		Name: "read_tool_result",
		Description: "Re-read a spilled tool_result handle by id or path (from a prior [tool_result_handle] footer). " +
			"Use when a tool preview was truncated and you need a specific byte range of the full output. " +
			"Prefer small limit windows and raise offset to page through large results.",
		Properties: map[string]interface{}{
			"id":          map[string]string{"type": "string", "description": "Handle id from [tool_result_handle] (preferred)"},
			"path":        map[string]string{"type": "string", "description": "Absolute path from the handle footer (must be under tool_results)"},
			"session_key": map[string]string{"type": "string", "description": "Optional session/user key used when the handle was spilled"},
			"offset":      map[string]string{"type": "integer", "description": "0-based byte offset into the full result (default 0)"},
			"limit":       map[string]string{"type": "integer", "description": "Max bytes to return (default 6000, max 32768)"},
		},
		// id OR path required — enforced in handler for clearer errors.
		Handler: func(args map[string]interface{}) string { return ToolReadToolResult(args) },
	})

	r.Register(ToolEntry{
		Name:        "FileRead",
		Description: "按行读取 UTF-8 文本文件。适合在 ripgrep/Glob 找到文件或行号后精确查看代码片段；用 start_line+end_line 读取闭区间，或 start_line+lines 读取指定行数。返回内容默认带行号，便于后续 edit_file 精准修改。",
		Properties: map[string]interface{}{
			"path":              map[string]string{"type": "string", "description": "要读取的文件路径。可用绝对路径，或相对当前项目目录的路径。"},
			"start_line":        map[string]string{"type": "integer", "description": "起始行号，1-based，默认 1。例如从第 120 行开始读就传 120。"},
			"end_line":          map[string]string{"type": "integer", "description": "结束行号，1-based 且包含该行。提供 end_line 时优先使用 start_line..end_line。"},
			"lines":             map[string]string{"type": "integer", "description": "未提供 end_line 时读取的行数，默认 200，最大 1000。例如 start_line=50, lines=80 表示读 50-129 行。"},
			"show_line_numbers": map[string]string{"type": "boolean", "description": "是否在每行前显示行号，默认 true。修改代码前建议保持 true。"},
		},
		Required: []string{"path"},
		Handler:  func(args map[string]interface{}) string { return ToolFileRead(args) },
	})

	r.Register(ToolEntry{
		Name:        "ripgrep",
		Description: "在项目文件中递归搜索文本/正则表达式，返回 file:line:content。适合查找函数、变量、配置项、错误信息或 TODO。先用 ripgrep 定位候选行，再用 FileRead 查看上下文；需要限制文件类型时传 glob，如 **/*.go。",
		Properties: map[string]interface{}{
			"pattern":        map[string]string{"type": "string", "description": "要搜索的正则表达式。普通字符串也可直接填写；默认不区分大小写。"},
			"path":           map[string]string{"type": "string", "description": "搜索范围：目录或单个文件。为空时使用当前项目目录。"},
			"glob":           map[string]string{"type": "string", "description": "可选文件过滤 glob，例如 **/*.go、**/*.tsx、config/*.yaml。"},
			"exclude":        map[string]string{"type": "string", "description": "Optional exclude glob, supports comma/space separated patterns such as vendor/**,dist/**,**/*_generated.go."},
			"exclude_glob":   map[string]string{"type": "string", "description": "Backward-compatible alias for exclude."},
			"no_ignore":      map[string]string{"type": "boolean", "description": "When true, do not apply supported root .gitignore ignore rules."},
			"include_hidden": map[string]string{"type": "boolean", "description": "When true, include hidden dotfiles and dot-directories such as .github or .env. .git internals and large generated dependency dirs are still skipped."},
			"type":           map[string]string{"type": "string", "description": "Optional file type filter such as go, ts, js, py, rust, md, json, yaml, or an extension like .vue."},
			"case_sensitive": map[string]string{"type": "boolean", "description": "是否区分大小写，默认 false。查 Go 导出符号、精确常量名时可设 true。"},
			"fixed_string":   map[string]string{"type": "boolean", "description": "Treat pattern as literal text instead of a regular expression."},
			"whole_word":     map[string]string{"type": "boolean", "description": "Match only whole words/identifiers."},
			"line_regexp":    map[string]string{"type": "boolean", "description": "Require the pattern to match the entire line."},
			"max_results":    map[string]string{"type": "integer", "description": "最多返回匹配数，默认 100，最大 1000。结果太多时缩小 path/glob 或加更具体 pattern。"},
			"output_mode":    map[string]string{"type": "string", "description": "Output mode: content (default), files_with_matches, or count."},
			"context":        map[string]string{"type": "integer", "description": "Lines of context before and after each match when output_mode=content."},
			"before_context": map[string]string{"type": "integer", "description": "Lines before each match when output_mode=content."},
			"after_context":  map[string]string{"type": "integer", "description": "Lines after each match when output_mode=content."},
			"offset":         map[string]string{"type": "integer", "description": "Skip the first N output rows before applying max_results."},
			"stats":          map[string]string{"type": "boolean", "description": "When true, append local search/index statistics to the result."},
		},
		Required: []string{"pattern"},
		HandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			return ToolRipgrepDetailedCtx(ctx, args).Text
		},
	})

	r.Register(ToolEntry{
		Name:        "Glob",
		Description: "按 glob 通配符查找文件路径，支持 ** 递归。适合先发现项目里有哪些相关文件，如 **/*.go、**/package.json、cmd/**/main.go；找到文件后用 FileRead/read_file 查看内容，用 ripgrep 搜索文件内文本。",
		Properties: map[string]interface{}{
			"pattern":        map[string]string{"type": "string", "description": "文件匹配模式。常用：**/*.go 递归找 Go 文件；**/main.go 找所有 main.go；*.md 按文件名匹配各层 Markdown。"},
			"path":           map[string]string{"type": "string", "description": "基准目录，默认当前项目目录。pattern 相对该目录匹配。"},
			"exclude":        map[string]string{"type": "string", "description": "Optional exclude glob, supports comma/space separated patterns such as vendor/**,dist/**,**/*_generated.go."},
			"exclude_glob":   map[string]string{"type": "string", "description": "Backward-compatible alias for exclude."},
			"no_ignore":      map[string]string{"type": "boolean", "description": "When true, do not apply supported root .gitignore ignore rules."},
			"include_hidden": map[string]string{"type": "boolean", "description": "When true, include hidden dotfiles and dot-directories such as .github or .env. .git internals and large generated dependency dirs are still skipped."},
			"type":           map[string]string{"type": "string", "description": "Optional file type filter such as go, ts, js, py, rust, md, json, yaml, or an extension like .vue."},
			"max_results":    map[string]string{"type": "integer", "description": "最多返回路径数，默认 200，最大 2000。结果太多时收窄 path 或 pattern。"},
			"include_dirs":   map[string]string{"type": "boolean", "description": "是否返回目录，默认 false。只有需要找目录结构时设 true。"},
		},
		Required: []string{"pattern"},
		HandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			return ToolGlobDetailedCtx(ctx, args).Text
		},
	})

	r.Register(ToolEntry{
		Name:        "write_file",
		Description: "Write a UTF-8 text file. No content length limit; system handles large content automatically. For very large files (>6000 chars), consider splitting into overwrite + append chunks to avoid model output truncation.",
		Properties: map[string]interface{}{
			"path":     map[string]string{"type": "string", "description": "File path"},
			"content":  map[string]interface{}{"type": "string", "description": "File content. No length limit; you can write complete scripts or documents in a single call."},
			"mode":     map[string]string{"type": "string", "description": "Write mode: overwrite or append"},
			"phase_id": map[string]string{"type": "string", "description": workflowDocSchemaPhaseIDDescription()},
			"doc_type": map[string]string{"type": "string", "description": workflowDocSchemaDocTypeDescription()},
		},
		Required: []string{"path", "content"},
		Handler:  func(args map[string]interface{}) string { return ToolWriteFile(args) },
	})

	r.Register(ToolEntry{
		Name:        "edit_file",
		Description: "Edit a file by replacing old_string with new_string. Keep old_string and new_string under 1800 characters; split large edits into smaller exact replacements to avoid truncated tool-call JSON.",
		Properties: map[string]interface{}{
			"path":        map[string]string{"type": "string", "description": "File path"},
			"old_string":  map[string]interface{}{"type": "string", "description": "Text to search for. Keep under 1800 characters; split large edits into smaller exact replacements.", "maxLength": coreInlineToolPayloadMaxLength},
			"new_string":  map[string]interface{}{"type": "string", "description": "Replacement text. Keep under 1800 characters; split large edits into smaller exact replacements.", "maxLength": coreInlineToolPayloadMaxLength},
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
		Description: "Read a local file and show it in the current chat (desktop). Does not forward to WeChat/IM unless destination/forward_to_im is set. Prefer send_to_im for IM delivery.",
		Properties: map[string]interface{}{
			"path":          map[string]string{"type": "string", "description": "File path"},
			"file_name":     map[string]string{"type": "string", "description": "Display file name (optional)"},
			"destination":   map[string]string{"type": "string", "description": "chat/desktop or im/wechat/feishu/qq/dingtalk"},
			"forward_to_im": map[string]string{"type": "boolean", "description": "Whether to forward through IM"},
			"phase_id":      map[string]string{"type": "string", "description": workflowDocSchemaPhaseIDDescription()},
			"doc_type":      map[string]string{"type": "string", "description": workflowDocSchemaDocTypeDescription()},
		},
		Required: []string{"path"},
		Handler:  guardedHandler(deps, "send_file", func(args map[string]interface{}) string { return ToolSendFile(args) }),
	})

	r.Register(ToolEntry{
		Name:        "send_to_im",
		Description: "Send a local file to the user's bound IM channels (WeChat/Feishu/QQ/etc). Use when the user asks to deliver a file to WeChat. Always forwards; no extra flags required.",
		Properties: map[string]interface{}{
			"path":        map[string]string{"type": "string", "description": "File path"},
			"file_name":   map[string]string{"type": "string", "description": "Display file name (optional)"},
			"destination": map[string]string{"type": "string", "description": "Optional: wechat/feishu/qq/dingtalk/im (default im)"},
			"phase_id":    map[string]string{"type": "string", "description": workflowDocSchemaPhaseIDDescription()},
			"doc_type":    map[string]string{"type": "string", "description": workflowDocSchemaDocTypeDescription()},
		},
		Required: []string{"path"},
		Handler:  guardedHandler(deps, "send_to_im", func(args map[string]interface{}) string { return ToolSendToIM(args) }),
	})

	r.Register(ToolEntry{
		Name:        "open",
		Description: "Open a file or URL with the system default application.",
		Properties: map[string]interface{}{
			"target": map[string]string{"type": "string", "description": "File path or URL"},
		},
		Required: []string{"target"},
		Handler:  guardedHandler(deps, "open", func(args map[string]interface{}) string { return ToolOpen(args) }),
	})

	memoryTool := memory.ToolDefinitionSchema()
	r.Register(ToolEntry{
		Name:        "memory",
		Description: memoryTool.Description,
		Properties:  memoryTool.Properties,
		Required:    memoryTool.Required,
		Handler:     func(args map[string]interface{}) string { return ToolMemory(deps.MemoryStore, args, deps.LoopIDFunc) },
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
		Handler: guardedHandler(deps, "ssh", func() ToolHandler {
			if deps.SSHHandler != nil {
				return deps.SSHHandler
			}
			return func(args map[string]interface{}) string {
				return "SSH handler is not initialized. Please configure SSH support first."
			}
		}()),
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
		Name:        "goal",
		Description: "Manage a persistent long-running goal (action: create/complete/fail/get). Create a goal only when explicitly requested by the user; do not infer goals from ordinary tasks. The system auto-continues until the goal is complete or budget is exhausted.",
		Properties: map[string]interface{}{
			"action":              map[string]string{"type": "string", "description": "Action: create, complete, fail, get"},
			"objective":           map[string]string{"type": "string", "description": "Goal description (required for create)"},
			"token_budget":        map[string]string{"type": "integer", "description": "Token budget limit (optional, 0=unlimited)"},
			"max_turns":           map[string]string{"type": "integer", "description": "Max iteration rounds (optional, default 50)"},
			"acceptance_criteria": map[string]interface{}{"type": "array", "description": "Verifiable completion conditions (optional)", "items": map[string]string{"type": "string"}},
			"summary":             map[string]string{"type": "string", "description": "Completion summary (for complete action)"},
			"reason":              map[string]string{"type": "string", "description": "Failure reason (for fail action)"},
		},
		Required: []string{"action"},
		Handler:  func(args map[string]interface{}) string { return ToolGoal(deps.GoalStore, args) },
	})

	r.Register(ToolEntry{
		Name:        "read_excel",
		Description: "Read an Excel (XLSX/CSV) file. Returns cell data as JSON. Supports optional sheet selection and A1-notation range filtering.",
		Properties: map[string]interface{}{
			"file_path": map[string]string{"type": "string", "description": "Excel file path"},
			"sheet":     map[string]string{"type": "string", "description": "Optional sheet name (defaults to first sheet)"},
			"range":     map[string]string{"type": "string", "description": "Optional A1-notation cell range, e.g. A1:D10"},
		},
		Required: []string{"file_path"},
		Handler:  func(args map[string]interface{}) string { return ToolReadExcel(args) },
	})

	r.Register(ToolEntry{
		Name:        "write_excel",
		Description: "Write an XLSX file. Supports formulas (strings starting with =) and cell styles (bold, font_size, background_color, number_format).",
		Properties: map[string]interface{}{
			"file_path": map[string]string{"type": "string", "description": "Excel file path"},
			"sheet":     map[string]string{"type": "string", "description": "Optional sheet name"},
			"data":      map[string]string{"type": "object", "description": "Write data, format: {\"sheets\": [{\"name\": \"Sheet1\", \"rows\": [[...]]}]}"},
		},
		Required: []string{"file_path", "data"},
		Handler:  func(args map[string]interface{}) string { return ToolWriteExcel(args) },
	})

	r.Register(ToolEntry{
		Name:        "read_pptx",
		Description: "Read a PowerPoint (PPTX) file. Returns structured JSON with slides, shapes, text (with formatting), tables, charts, and speaker notes.",
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
		HandlerCtx: guardedHandlerCtx(deps, "web_search", func(ctx context.Context, args map[string]interface{}) string {
			if deps.WebSearchHandlerCtx != nil {
				return deps.WebSearchHandlerCtx(ctx, args)
			}
			if deps.WebSearchHandler != nil {
				return deps.WebSearchHandler(args)
			}
			return "Web search is not configured. Please set up a web search provider."
		}),
	})

	r.Register(ToolEntry{
		Name:        "web_fetch",
		Description: "Fetch and extract text content from a URL. Supports encoding detection and HTML extraction.",
		Properties: map[string]interface{}{
			"url":       map[string]string{"type": "string", "description": "URL to fetch"},
			"render_js": map[string]string{"type": "boolean", "description": "Use Chrome to render JS (optional, default false)"},
			"save_path": map[string]string{"type": "string", "description": "Save file path (optional, downloads file instead of returning text)"},
			"timeout":   map[string]string{"type": "integer", "description": "Timeout seconds, default 600, range 240-600"},
			"offset":    map[string]string{"type": "integer", "description": "Character offset for pagination (default 0)"},
			"max_chars": map[string]string{"type": "integer", "description": "Max characters to return (optional)"},
		},
		Required: []string{"url"},
		HandlerCtx: guardedHandlerCtx(deps, "web_fetch", func(ctx context.Context, args map[string]interface{}) string {
			if deps.WebFetchHandlerCtx != nil {
				return deps.WebFetchHandlerCtx(ctx, args)
			}
			if deps.WebFetchHandler != nil {
				return deps.WebFetchHandler(args)
			}
			return "Web fetch is not configured."
		}),
	})

	// --- Tools requiring host-injected handlers via ExtraHandlers ---

	r.Register(ToolEntry{
		Name:        "manage_skill",
		Description: skill.ManageSkillDescription() + " maintenance_plan is read-only and returns local skill health recommendations without modifying, archiving, merging, installing, or executing skills.",
		Properties: map[string]interface{}{
			"action":                 map[string]string{"type": "string", "description": "操作: " + skill.ManageSkillActionSlash()},
			"query":                  map[string]string{"type": "string", "description": "搜索关键词（search 时必填）"},
			"skill_id":               map[string]string{"type": "string", "description": "Skill ID（install 时必填，从 search 结果中获取）"},
			"hub_url":                map[string]string{"type": "string", "description": "来源 Hub URL（install 时可选）"},
			"name":                   map[string]string{"type": "string", "description": "Skill 名称（run/upload/validate 时必填）"},
			"skill_name":             map[string]string{"type": "string", "description": "Skill 名称（patch/history 时必填）"},
			"find":                   map[string]string{"type": "string", "description": "要查找的原始文本（patch 时必填，必须精确匹配唯一一处）"},
			"replace":                map[string]string{"type": "string", "description": "替换后的新文本（patch 时必填）"},
			"reason":                 map[string]string{"type": "string", "description": "修补原因说明（patch 时可选）"},
			"args":                   map[string]string{"type": "object", "description": "Skill 运行参数（run 时按需传入）"},
			"operation":              map[string]string{"type": "string", "description": "执行指定的 operation（run 时可选）"},
			"input":                  map[string]string{"type": "string", "description": "兼容旧调用的输入参数（run 时可选）"},
			"output":                 map[string]string{"type": "string", "description": "兼容旧调用的输出参数（run 时可选）"},
			"user_prompt":            map[string]string{"type": "string", "description": "用户的原始请求文本（run 时可选）"},
			"run_id":                 map[string]string{"type": "string", "description": "运行 ID（status 时必填）"},
			"auto_fix":               map[string]string{"type": "boolean", "description": "Validate auto fix flag"},
			"max_actions":            map[string]string{"type": "integer", "description": "maintenance_plan max action count"},
			"stale_after_days":       map[string]string{"type": "integer", "description": "maintenance_plan stale learned/crafted skill threshold in days"},
			"min_failure_runs":       map[string]string{"type": "integer", "description": "maintenance_plan minimum failed runs before review/repair"},
			"duplicate_similarity":   map[string]string{"type": "number", "description": "maintenance_plan duplicate skill similarity threshold"},
			"dry_run":                map[string]string{"type": "boolean", "description": "execute_maintenance_plan preview mode; defaults true"},
			"confirm":                map[string]string{"type": "boolean", "description": "required true when execute_maintenance_plan uses dry_run=false"},
			"approved_actions":       map[string]string{"type": "array", "description": "approved maintenance action names for execute_maintenance_plan"},
			"allow_duplicate_retire": map[string]string{"type": "boolean", "description": "allow execute_maintenance_plan to disable the recommended duplicate skill after merge draft review"},
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

	r.Register(ToolEntry{
		Name:        "tts",
		Description: "将文本转换为语音消息发送给用户。IM 通道以语音气泡形式发送，桌面面板播放语音。适用于状态通知、简短回复摘要、任务完成汇报等场景。",
		Properties: map[string]interface{}{
			"text": map[string]string{"type": "string", "description": "要转换为语音的文本内容（中文，最长 300 字，超出自动截断）"},
		},
		Required: []string{"text"},
		Handler:  extraHandler(deps, "tts", "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS 并等待模型下载完成。"),
	})

	r.Register(ToolEntry{
		Name:        "asr",
		Description: audioconv.ASRToolDescription(),
		Properties: map[string]interface{}{
			"path":   map[string]string{"type": "string", "description": "本地音频文件路径"},
			"format": map[string]string{"type": "string", "description": "可选格式提示: wav/mp3/ogg/opus/silk（默认自动检测）"},
		},
		Required: []string{"path"},
		Handler:  extraHandler(deps, "asr", "语音识别不可用（ASR 模型未加载）。请在设置中启用 ASR 并等待模型下载完成。"),
	})

	r.Register(ToolEntry{
		Name:        "manage_schedule",
		Description: "定时任务管理（action: create/list/delete/update）。create 创建定时任务，list 列出所有任务，delete 删除任务，update 修改任务。day_of_week: -1=每天, 0=周日, 1=周一...6=周六。day_of_month: -1=不限, 1-31。一次性任务请将 start_date 和 end_date 都设为目标日期。",
		Properties: map[string]interface{}{
			"action":           map[string]string{"type": "string", "description": "操作: create/list/delete/update"},
			"id":               map[string]string{"type": "string", "description": "任务 ID（delete/update 时必填）"},
			"name":             map[string]string{"type": "string", "description": "任务名称（create 时必填，delete 时可选）"},
			"task_action":      map[string]string{"type": "string", "description": "到时要执行的操作（自然语言描述，create/update 时使用）"},
			"hour":             map[string]string{"type": "integer", "description": "执行时间-小时（0-23）"},
			"minute":           map[string]string{"type": "integer", "description": "执行时间-分钟（0-59，默认0）"},
			"day_of_week":      map[string]string{"type": "integer", "description": "星期几（-1=每天, 0=周日...6=周六，默认-1）"},
			"day_of_month":     map[string]string{"type": "integer", "description": "每月几号（-1=不限, 1-31，默认-1）"},
			"interval_minutes": map[string]string{"type": "integer", "description": "重复间隔分钟数（>0 启用间隔模式）"},
			"start_date":       map[string]string{"type": "string", "description": "生效开始日期（格式 2006-01-02）"},
			"end_date":         map[string]string{"type": "string", "description": "生效结束日期（格式 2006-01-02）"},
		},
		Required: []string{"action"},
		Handler:  extraHandler(deps, "manage_schedule", "定时任务管理器未初始化。"),
	})

	// --- Knowledge tools (host-injected via ExtraHandlers) ---

	r.Register(ToolEntry{
		Name:        "knowledge_search",
		Description: "Search the local knowledge base (SQLite FTS). Returns ranked results with score, source, and snippet. Use when the user asks about saved documents, imported files, or stored knowledge.",
		Properties: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Search query"},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"limit":        map[string]string{"type": "integer", "description": "Max results, default 8, max 50"},
		},
		Required: []string{"query"},
		Handler:  extraHandler(deps, "knowledge_search", "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_context_pack",
		Description: "Build a compact, citation-backed context bundle from the local knowledge base under a character budget. Use when you need a prompt-ready bundle of ranked cards, facts, and source nodes for answering from stored knowledge.",
		Properties: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Search query for the context pack"},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"max_items":    map[string]string{"type": "integer", "description": "Max context items, default 8, max 30"},
			"max_chars":    map[string]string{"type": "integer", "description": "Max total context characters, default 6000, max 20000"},
		},
		Required: []string{"query"},
		Handler:  extraHandler(deps, "knowledge_context_pack", "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_export",
		Description: "Export all or selected current-user knowledge into an editable MaClaw knowledge JSON package for moving data between machines or sharing through Hub. A description is required.",
		Properties: map[string]interface{}{
			"title":            map[string]string{"type": "string", "description": "Optional export title"},
			"description":      map[string]string{"type": "string", "description": "Required description for this knowledge export"},
			"source_ids":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source IDs for partial export"},
			"include_disabled": map[string]string{"type": "boolean", "description": "Include disabled own sources"},
			"output_path":      map[string]string{"type": "string", "description": "Optional destination path when supported by the host"},
		},
		Required: []string{"description"},
		Handler:  extraHandler(deps, "knowledge_export", "Error: knowledge export is not configured. Use the MaClawSrv knowledge export UI or configure a host handler."),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_import_package",
		Description: "Import a MaClaw editable knowledge JSON package into the current user's knowledge base. URL entries may be rebuilt; text entries require package content.",
		Properties: map[string]interface{}{
			"package_path": map[string]string{"type": "string", "description": "Path to a MaClaw knowledge JSON package"},
			"package_json": map[string]string{"type": "object", "description": "Inline package JSON when supplied by the host"},
		},
		Handler: extraHandler(deps, "knowledge_import_package", "Error: knowledge_import_package is available — call it directly with package_json or package_path parameter."),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_import_share",
		Description: "Import shared knowledge by knowledge ID or by a human-readable, agent-importable share link. The host resolves the Hub and enforces permissions.",
		Properties: map[string]interface{}{
			"knowledge_id": map[string]string{"type": "string", "description": "Unique shared knowledge ID"},
			"share_link":   map[string]string{"type": "string", "description": "Human-readable share link that also carries import data"},
			"hub_url":      map[string]string{"type": "string", "description": "Optional Hub URL hint"},
		},
		Handler: extraHandler(deps, "knowledge_import_share", "Error: knowledge_import_share is available — call it directly with share_link or knowledge_id parameter."),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_import_directory",
		Description: "Scan or import a local directory/folder of documents into the local knowledge base. Only use after the user explicitly provides or approves the directory path.",
		Properties: map[string]interface{}{
			"root_path":    map[string]string{"type": "string", "description": "Directory containing documents"},
			"path":         map[string]string{"type": "string", "description": "Alias for root_path."},
			"dir":          map[string]string{"type": "string", "description": "Alias for root_path."},
			"directory":    map[string]string{"type": "string", "description": "Alias for root_path."},
			"folder":       map[string]string{"type": "string", "description": "Alias for root_path."},
			"root":         map[string]string{"type": "string", "description": "Alias for root_path."},
			"action":       map[string]string{"type": "string", "description": "scan | import. Default import."},
			"save_scope":   map[string]string{"type": "string", "description": "project | personal | local_only. Default project."},
			"topic_hint":   map[string]string{"type": "string", "description": "Optional topic hint"},
			"recursive":    map[string]string{"type": "boolean", "description": "Include subdirectories, default true"},
			"include_exts": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Extensions to include, e.g. .pdf, .docx, .md"},
			"max_file_mb":  map[string]string{"type": "integer", "description": "Max file size in MB, default 100"},
			"start_async":  map[string]string{"type": "boolean", "description": "For import action, start async job. Default true."},
		},
		Handler: extraHandler(deps, "knowledge_import_directory", "Error: knowledge import is not configured. Use the desktop knowledge import UI or configure a host handler."),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_import_files",
		Description: "Scan or import explicitly provided local document file paths into the local knowledge base. Use for importing files/documents/PDFs into the knowledge base / external brain. Only use after the user explicitly provides or approves the file paths.",
		Properties: map[string]interface{}{
			"file_paths":   map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Explicit local document file paths to scan or import"},
			"paths":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Alias for file_paths."},
			"files":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Alias for file_paths."},
			"file_path":    map[string]string{"type": "string", "description": "Alias for a single file_paths item."},
			"path":         map[string]string{"type": "string", "description": "Alias for a single file_paths item."},
			"action":       map[string]string{"type": "string", "description": "scan | import. Default import."},
			"save_scope":   map[string]string{"type": "string", "description": "project | personal | local_only. Default project."},
			"topic_hint":   map[string]string{"type": "string", "description": "Optional topic hint"},
			"include_exts": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Extensions to include, e.g. .pdf, .docx, .md"},
			"max_file_mb":  map[string]string{"type": "integer", "description": "Max file size in MB, default 100"},
			"start_async":  map[string]string{"type": "boolean", "description": "For import action, start async job when the host handler supports it. Default true."},
		},
		Handler: extraHandler(deps, "knowledge_import_files", "Error: knowledge import is not configured. Use the desktop knowledge import UI or configure a host handler."),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_save_text",
		Description: "Save text into the local knowledge base. Only use when the user explicitly asks to save, remember, or persist a specific piece of text to the knowledge base.",
		Properties: map[string]interface{}{
			"text":       map[string]string{"type": "string", "description": "Text content to save"},
			"title":      map[string]string{"type": "string", "description": "Optional source title"},
			"topic_hint": map[string]string{"type": "string", "description": "Optional topic hint to improve write-time structure"},
		},
		Required: []string{"text"},
		Handler:  extraHandler(deps, "knowledge_save_text", "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"),
	})

	r.Register(ToolEntry{
		Name:        "knowledge_save_url",
		Description: "Fetch a public URL and save its content into the local knowledge base. Only use when the user explicitly asks to save, archive, or add a web page to the knowledge base.",
		Properties: map[string]interface{}{
			"url":        map[string]string{"type": "string", "description": "Public HTTP(S) URL to save"},
			"link":       map[string]string{"type": "string", "description": "Alias for url."},
			"href":       map[string]string{"type": "string", "description": "Alias for url."},
			"uri":        map[string]string{"type": "string", "description": "Alias for url."},
			"target":     map[string]string{"type": "string", "description": "Alias for url."},
			"topic_hint": map[string]string{"type": "string", "description": "Optional topic hint to improve write-time structure"},
		},
		Handler: extraHandler(deps, "knowledge_save_url", "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"),
	})
}

func guardedHandler(deps CoreToolDeps, name string, next ToolHandler) ToolHandler {
	return func(args map[string]interface{}) string {
		if deps.SecurityGuard != nil {
			if ok, reason := deps.SecurityGuard(name, args); !ok {
				if reason == "" {
					reason = "blocked by security policy"
				}
				return "[system rejected] " + reason
			}
		}
		return next(args)
	}
}

func guardedHandlerCtx(deps CoreToolDeps, name string, next ToolHandlerCtx) ToolHandlerCtx {
	return func(ctx context.Context, args map[string]interface{}) string {
		if deps.SecurityGuard != nil {
			if ok, reason := deps.SecurityGuard(name, args); !ok {
				if reason == "" {
					reason = "blocked by security policy"
				}
				return "[system rejected] " + reason
			}
		}
		return next(ctx, args)
	}
}

// extraHandler returns the host-injected handler for the given tool name,
// or a stub that returns the fallback message.
func extraHandler(deps CoreToolDeps, name, fallback string) ToolHandler {
	var handler ToolHandler
	if deps.ExtraHandlers != nil {
		if h, ok := deps.ExtraHandlers[name]; ok && h != nil {
			handler = h
		}
	}
	if handler == nil {
		handler = func(args map[string]interface{}) string { return fallback }
	}
	return guardedHandler(deps, name, handler)
}
