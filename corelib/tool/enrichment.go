package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ToolEnrichment holds pre-computed synthetic queries for a tool.
type ToolEnrichment struct {
	ToolName         string    `json:"tool_name"`
	SyntheticQueries []string  `json:"synthetic_queries"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// EnrichmentStore persists synthetic queries for tools to improve retrieval.
type EnrichmentStore struct {
	mu          sync.RWMutex
	enrichments map[string]*ToolEnrichment
	path        string
}

// NewEnrichmentStore creates or loads an EnrichmentStore from the given path.
func NewEnrichmentStore(path string) (*EnrichmentStore, error) {
	s := &EnrichmentStore{
		enrichments: make(map[string]*ToolEnrichment),
		path:        path,
	}
	if err := s.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("enrichment_store: load: %w", err)
	}
	return s, nil
}

// DefaultEnrichmentStorePath returns <MaclawBaseDir>/data/tool_enrichments.json.
func DefaultEnrichmentStorePath() string {
	base := maclawBaseDirFallback()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "data", "tool_enrichments.json")
}

// GetSearchText returns enriched text for BM25/vector indexing.
// Format: "name description tag1 tag2 | query1 | query2 | ..."
// Falls back to base text when no enrichment exists.
func (s *EnrichmentStore) GetSearchText(t RegisteredTool) string {
	base := t.Name + " " + t.Description
	for _, tag := range t.Tags {
		base += " " + tag
	}
	if isInternalBrowserDispatchToolName(t.Name) {
		return base
	}

	// Check builtin enrichments first.
	if queries, ok := BuiltinEnrichments[t.Name]; ok {
		for _, q := range queries {
			base += " | " + q
		}
		return base
	}

	// Check stored enrichments.
	s.mu.RLock()
	e, ok := s.enrichments[t.Name]
	s.mu.RUnlock()
	if ok && len(e.SyntheticQueries) > 0 {
		for _, q := range e.SyntheticQueries {
			base += " | " + q
		}
	}
	return base
}

// Set stores synthetic queries for a tool and persists to disk.
func (s *EnrichmentStore) Set(toolName string, queries []string) error {
	if isInternalBrowserDispatchToolName(toolName) {
		return nil
	}
	s.mu.Lock()
	s.enrichments[toolName] = &ToolEnrichment{
		ToolName:         toolName,
		SyntheticQueries: queries,
		UpdatedAt:        time.Now(),
	}
	s.mu.Unlock()
	return s.save()
}

// Has returns true if enrichment exists for the tool (builtin or stored).
func (s *EnrichmentStore) Has(toolName string) bool {
	if isInternalBrowserDispatchToolName(toolName) {
		return false
	}
	if _, ok := BuiltinEnrichments[toolName]; ok {
		return true
	}
	s.mu.RLock()
	_, ok := s.enrichments[toolName]
	s.mu.RUnlock()
	return ok
}

func isInternalBrowserDispatchToolName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "browser" && strings.HasPrefix(name, "browser_")
}

// Load reads enrichments from disk.
func (s *EnrichmentStore) Load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var items []ToolEnrichment
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("enrichment_store: parse: %w", err)
	}
	s.mu.Lock()
	for i := range items {
		s.enrichments[items[i].ToolName] = &items[i]
	}
	s.mu.Unlock()
	return nil
}

func (s *EnrichmentStore) save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	items := make([]ToolEnrichment, 0, len(s.enrichments))
	for _, e := range s.enrichments {
		items = append(items, *e)
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("enrichment_store: marshal: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("enrichment_store: mkdir: %w", err)
	}
	// Atomic write: temp file + rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("enrichment_store: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("enrichment_store: rename: %w", err)
	}
	return nil
}

// GenerateEnrichmentPrompt returns the system+user messages for LLM-based
// synthetic query generation. The caller is responsible for making the LLM call.
// When bodySummary is non-empty, the prompts instruct the LLM to generate
// queries that reflect implementation-level details and distinguish the tool
// from similar tools. When bodySummary is empty, falls back to name + description only.
func GenerateEnrichmentPrompt(toolName, description, bodySummary string) (system, user string) {
	if bodySummary != "" {
		system = `You are a tool usage analyst. Given a tool's name, description, and implementation body summary, generate 5 typical user queries that would require this tool. Focus on queries that reflect implementation-level details visible in the body summary and that distinguish this tool from similar tools in the same category. Output ONLY a JSON array of strings, no markdown, no commentary.`
		user = fmt.Sprintf("Tool: %s\nDescription: %s\nBody Summary:\n%s", toolName, description, bodySummary)
	} else {
		system = `You are a tool usage analyst. Given a tool's name and description, generate 5 typical user queries that would require this tool. Output ONLY a JSON array of strings, no markdown, no commentary.`
		user = fmt.Sprintf("Tool: %s\nDescription: %s", toolName, description)
	}
	return
}

// ParseEnrichmentResponse parses the LLM response into a string slice.
func ParseEnrichmentResponse(resp string) []string {
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)
	var queries []string
	if err := json.Unmarshal([]byte(resp), &queries); err != nil {
		return nil
	}
	return queries
}

// BuiltinBodies provides hardcoded body content (parameter schema descriptions)
// for core builtin tools. Used to populate RegisteredTool.Body during registration.
var BuiltinBodies = map[string]string{
	"bash": `Parameters:
- command (string, required): Shell command to execute
- timeout (int, optional): Timeout seconds, default 600, range 240-600
Typical usage: Run shell commands, check system status, install packages`,

	"read_file": `Parameters:
- path (string, required): File path to read
- encoding (string, optional): File encoding, default utf-8
Typical usage: Read source code, config files, logs`,

	"FileRead": `Parameters:
- path (string, required): 要读取的文件路径，可为绝对路径或相对当前项目目录的路径
- start_line (int, optional): 起始行号，1-based，默认 1
- end_line (int, optional): 结束行号，1-based 且包含该行；有 end_line 时读取 start_line..end_line
- lines (int, optional): 未传 end_line 时读取多少行，默认 200，最大 1000
- show_line_numbers (bool, optional): 是否显示行号，默认 true
How to use: ripgrep 找到 path:line 后，调用 FileRead(path=该文件, start_line=附近行号, lines=80) 查看上下文；修改前保留行号，便于 edit_file 精准替换。`,

	"ripgrep": `Parameters:
- pattern (string, required): 要搜索的文本或正则表达式，默认不区分大小写
- path (string, optional): 搜索目录或单个文件；为空时搜索当前项目目录
- glob (string, optional): 文件过滤模式，如 **/*.go、**/*.tsx、config/*.yaml
- case_sensitive (bool, optional): 是否区分大小写，默认 false
- max_results (int, optional): 最大匹配数，默认 100，最大 1000
How to use: 不知道代码在哪里时先用 ripgrep(pattern="函数名/变量名/错误文案", glob="**/*.go") 定位；结果是 file:line:content，再用 FileRead 读取上下文。`,

	"Glob": `Parameters:
- pattern (string, required): 文件通配符，支持 ** 递归；例：**/*.go、**/main.go、*.md（按文件名匹配各层 Markdown）
- path (string, optional): 基准目录，默认当前项目目录；pattern 相对该目录匹配
- max_results (int, optional): 最大返回路径数，默认 200，最大 2000
- include_dirs (bool, optional): 是否包含目录，默认 false
How to use: 需要先找文件名或文件类型时用 Glob；找到路径后用 FileRead/read_file 读内容，或用 ripgrep 搜索文件内部文本。`,

	"write_file": `Parameters:
- path (string, required): Destination file path
- content (string, required): Content to write (always saved as UTF-8), may be empty to clear/create an empty file
- mode (string, optional): Write mode (overwrite/append), default overwrite. For large content, use overwrite for the first chunk, then append for subsequent chunks.
Typical usage: Create files, overwrite configs, append markdown or logs`,

	"edit_file": `Parameters:
- path (string, required): Existing file path to edit
- old_string (string, required): Existing text to find
- new_string (string, required): Replacement text, may be empty to delete the matched text
- replace_all (bool, optional): Replace all matches, default false
Typical usage: Update a specific snippet in a source file or document`,

	"list_directory": `Parameters:
- path (string, required): Directory path to list
- recursive (bool, optional): List recursively, default false
- depth (int, optional): Max recursion depth
Typical usage: Explore project structure, find files in a folder`,

	"memory": `Parameters:
- action (string, required): Operation type (store/recall/search)
- key (string, optional): Memory key for store/recall
- content (string, optional): Content to store
- query (string, optional): Search query for recall
Typical usage: Persist notes, recall project context, search past interactions`,

	"web_search": `Parameters:
- query (string, required): Search query
- max_results (int, optional): Maximum results to return, default 5
Typical usage: Search for documentation, find solutions, look up APIs`,

	"web_fetch": `Parameters:
- url (string, required): URL to fetch
- format (string, optional): Output format (text/html/markdown), default text
- timeout (int, optional): Timeout seconds, default 600, range 240-600
Typical usage: Fetch webpage content, read documentation, download text resources`,

	"screenshot": `Parameters:
- display (int, optional): Display index for multi-monitor, default 0
- region (string, optional): Screen region to capture (x,y,w,h)
Typical usage: Capture screen state, verify UI changes, document visual output`,

	"call_mcp_tool": `Parameters:
- server_id (string, required): MCP server id or name; if names are duplicated, use the id
- tool_name (string, required): Tool name on the server
- arguments (object, optional): Tool arguments as key-value pairs
Typical usage: Invoke external tools via MCP protocol`,

	"browser": `Parameters:
- action (string, required): session_start/connect/observe/navigate/click/type/wait/extract/scroll/select/list_pages/switch_page/close/set_files/info/task_run/task_status/task_verify/list_flows
- session_id (string, required for page actions): Browser agent session ID returned by session_start/connect
- start_url/url/ref/selector/text/value/task_id/steps (optional): Action-specific arguments
Typical usage: Use one stable BrowserAgentSession workflow. First call action=session_start or connect to get browser-session-*, then use observe refs for page actions. For rich editors, click the editable field then action=type may omit ref/selector to type into the focused element; set content_format=markdown when Markdown should render as rich content. No screenshot fallback, eval, raw HTTP, coordinate clicks, or broad browser process kills`,

	"list_skills": `Parameters:
- filter (string, optional): Filter skills by name or tag
Typical usage: List installed NL skills, browse skill library`,

	"run_skill": `Parameters:
- name (string, required): Skill name to execute
- input (string, optional): Input text or parameters for the skill
Typical usage: Execute an NL skill by name with optional input`,

	"craft_tool": `Parameters:
- task (string, required): One-shot automation task description
- language (string, optional): Preferred script language if a runtime is available
- working_dir (string, optional): Working directory for script execution
- expected_artifacts (array, optional): Expected output file paths used for verification
- verification_mode (string, optional): Verification mode such as artifact_required
- register_policy (string, optional): Skill registration policy (auto/manual)
- max_attempts (int, optional): Max bounded repair attempts, default 2, max 3
- save_as_skill (bool, optional): Whether to try registering as a reusable skill after success
- skill_name (string, optional): Optional skill name when saving
- timeout (int, optional): Execution timeout in seconds
Typical usage: Generate and execute a single local script for data processing, API calls, file conversion, or small automation; supports artifact verification and bounded self-repair; avoid for large codebase refactors or long-lived coding tasks`,

	"parallel_execute": `Disabled for external coding sessions. Split or route coding work through the internal CodingSubAgent instead.`,

	"recommend_tool": `Parameters:
- task (string, required): Description of the task to accomplish
Typical usage: Get a recommendation for which coding tool fits a task best`,

	"send_file": `Parameters:
- path (string, required): Local file path to send
- caption (string, optional): Message to accompany the file
Typical usage: Share a file with the user in the chat`,

	"set_nickname": `Parameters:
- nickname (string, required): New display name to set
Typical usage: Change the assistant's display name in the conversation`,

	"list_sessions": `Parameters:
- status (string, optional): Filter by session status (active/stopped/all)
Typical usage: Show all active or recent coding sessions`,

	"get_session_output": `Parameters:
- session_id (string, required): Session ID to read output from
- lines (int, optional): Number of recent lines to return
Typical usage: Read the latest output from a coding session`,

	"get_session_events": `Parameters:
- session_id (string, required): Session ID to read events from
- since (string, optional): Timestamp to filter events after
Typical usage: Get activity log and events from a coding session`,

	"discover_tool": `Parameters:
- query (string, required): Description of the capability needed
- sources (string, optional): Where to search (mcp/skillhub/all), default all
Typical usage: Find tools from MCP servers or SkillHub matching a need`,
}

// BuiltinEnrichments provides hardcoded synthetic queries for core tools.
// These don't require LLM generation and are always available.
var BuiltinEnrichments = map[string][]string{
	"bash": {
		"run a shell command",
		"execute terminal command",
		"运行命令行",
		"check disk usage",
		"list running processes",
	},
	"read_file": {
		"read file contents",
		"show me the code in this file",
		"查看文件内容",
		"open and display a file",
		"cat a file",
	},
	"FileRead": {
		"read specific lines from a file",
		"show lines 20 to 80 in source code",
		"inspect code around a line number",
		"按行读取文件",
		"查看指定行范围",
	},
	"ripgrep": {
		"search code with regex",
		"find symbol references recursively",
		"ripgrep text search",
		"grep project files",
		"搜索代码中的字符串",
	},
	"Glob": {
		"find files by glob pattern",
		"list all go files recursively",
		"match files with wildcard",
		"查找匹配文件",
		"按通配符查找文件",
	},
	"write_file": {
		"write content to a file",
		"create a new file",
		"修改文件",
		"save text to disk",
		"update file contents",
		"append text to file",
		"追加写入文件",
	},
	"edit_file": {
		"edit an existing file",
		"replace text in file",
		"局部编辑文件内容",
		"update a snippet in a file",
		"find and replace in file",
	},
	"list_directory": {
		"list files in a directory",
		"show folder contents",
		"查看目录结构",
		"what files are in this folder",
		"browse directory tree",
	},
	"memory": {
		"remember this for later",
		"recall what we discussed",
		"记住这个信息",
		"save a note about this project",
		"what do you remember about",
	},
	"web_search": {
		"search the internet for",
		"look up information online",
		"搜索网页",
		"find documentation for",
		"google this topic",
	},
	"web_fetch": {
		"fetch a webpage",
		"download page content",
		"获取网页内容",
		"read this URL",
		"get the content from this link",
	},
	"screenshot": {
		"take a screenshot",
		"capture the screen",
		"截屏",
		"show me what's on screen",
		"grab a screenshot of the desktop",
	},
	"call_mcp_tool": {
		"call an MCP server tool",
		"use an external tool via MCP",
		"调用MCP工具",
		"invoke MCP function",
		"run MCP server command",
	},
	"list_skills": {
		"show available skills",
		"what skills are installed",
		"列出技能",
		"list NL skills",
		"show my skill library",
	},
	"run_skill": {
		"execute a skill",
		"run an NL skill",
		"执行技能",
		"trigger a skill by name",
		"use a saved automation",
	},
	"craft_tool": {
		"create a custom local automation script",
		"write a one-off script for data processing or file conversion",
		"制作一次性自动化工具",
		"generate and run a script for this task",
		"build a quick local automation without opening a coding session",
	},
	"browser": {
		"browser automation with one merged tool",
		"open a browser session and inspect page",
		"use browser action session_start then observe",
		"stable browser-session workflow",
		"网页浏览器自动化",
	},
	"parallel_execute": {
		"disabled external queued coding sessions",
		"do not queue external coding tasks",
		"按 SubAgent 并发数执行多个任务",
		"use internal CodingSubAgent instead",
		"external queued coding execution is disabled",
	},
	"recommend_tool": {
		"which coding tool is best for this",
		"recommend a tool for this task",
		"推荐编程工具",
		"suggest the right tool",
		"help me pick a coding tool",
	},
	"send_file": {
		"send a file to the user",
		"share a file",
		"发送文件",
		"deliver this file",
		"upload file to chat",
	},
	"set_nickname": {
		"change my nickname",
		"set a display name",
		"设置昵称",
		"rename myself",
		"update my name",
	},
	"list_sessions": {
		"show active coding sessions",
		"list running sessions",
		"查看会话列表",
		"what sessions are open",
		"show all coding sessions",
	},
	"get_session_output": {
		"get output from a coding session",
		"show session results",
		"查看会话输出",
		"what did the session produce",
		"read session output",
	},
	"get_session_events": {
		"get events from a coding session",
		"show session activity log",
		"查看会话事件",
		"what happened in the session",
		"session event history",
	},
	"discover_tool": {
		"find a tool I don't have",
		"search for additional tools",
		"查找更多工具",
		"I need a capability not in the current list",
		"discover matching tools from MCP or SkillHub",
	},
}
