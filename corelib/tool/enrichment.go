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

// DefaultEnrichmentStorePath returns ~/.maclaw/data/tool_enrichments.json.
func DefaultEnrichmentStorePath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".maclaw", "data", "tool_enrichments.json")
}

// GetSearchText returns enriched text for BM25/vector indexing.
// Format: "name description tag1 tag2 | query1 | query2 | ..."
// Falls back to base text when no enrichment exists.
func (s *EnrichmentStore) GetSearchText(t RegisteredTool) string {
	base := t.Name + " " + t.Description
	for _, tag := range t.Tags {
		base += " " + tag
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
	if _, ok := BuiltinEnrichments[toolName]; ok {
		return true
	}
	s.mu.RLock()
	_, ok := s.enrichments[toolName]
	s.mu.RUnlock()
	return ok
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
- timeout (int, optional): Timeout in seconds, default 30
Typical usage: Run shell commands, check system status, install packages`,

	"read_file": `Parameters:
- path (string, required): File path to read
- encoding (string, optional): File encoding, default utf-8
Typical usage: Read source code, config files, logs`,

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
Typical usage: Fetch webpage content, read documentation, download text resources`,

	"screenshot": `Parameters:
- display (int, optional): Display index for multi-monitor, default 0
- region (string, optional): Screen region to capture (x,y,w,h)
Typical usage: Capture screen state, verify UI changes, document visual output`,

	"send_and_observe": `Parameters:
- session_id (string, required): Target coding session ID
- message (string, required): Message to send to the session
- timeout (int, optional): Wait timeout in seconds, default 120
Typical usage: Send instructions to coding agent and wait for results`,

	"create_session": `Parameters:
- tool (string, required): Coding tool to launch (e.g. claude, cursor)
- project_dir (string, optional): Working directory for the session
Typical usage: Start a new coding session with a specific tool and project`,

	"call_mcp_tool": `Parameters:
- server_id (string, required): MCP server id or name; if names are duplicated, use the id
- tool_name (string, required): Tool name on the server
- arguments (object, optional): Tool arguments as key-value pairs
Typical usage: Invoke external tools via MCP protocol`,

	"browser_session_start": `Parameters:
- addr (string, optional): CDP address for browser session discovery or launch
- start_url (string, optional): Initial URL to open in the session
- reuse_existing (bool, optional): Reuse an existing browser agent session, default true
- allowed_domains (array, optional): Allowlist of navigable domains
- blocked_domains (array, optional): Denylist of blocked domains
Typical usage: Start or reuse a long-lived browser agent session with policy controls`,

	"browser_observe": `Parameters:
- session_id (string, required): Browser agent session ID
- include_screenshot (bool, optional): Include screenshot in observation, default true
Typical usage: Capture a snapshot with refs, screenshot, console, and network summaries`,

	"browser_navigate": `Parameters:
- session_id (string, required): Browser agent session ID
- url (string, required): URL to navigate to inside the session
Typical usage: Open a webpage in the browser session and auto-observe after navigation`,

	"browser_click": `Parameters:
- session_id (string, required): Browser agent session ID
- snapshot_id (string, optional): Snapshot ID for ref resolution
- ref (string, optional): Stable ref from browser_observe such as @e1
- selector (string, optional): CSS selector fallback
Typical usage: Click a page element via stable refs with selector fallback`,

	"browser_type": `Parameters:
- session_id (string, required): Browser agent session ID
- snapshot_id (string, optional): Snapshot ID for ref resolution
- ref (string, optional): Stable ref from browser_observe such as @e1
- selector (string, optional): CSS selector fallback
- text (string, required): Text to input
Typical usage: Fill an input in the browser session via stable refs or selector fallback`,

	"browser_wait": `Parameters:
- session_id (string, required): Browser agent session ID
- snapshot_id (string, optional): Snapshot ID for ref resolution
- ref (string, optional): Stable ref to wait for
- selector (string, optional): CSS selector to wait for
- duration_ms (int, optional): Milliseconds to wait when no target is provided
Typical usage: Wait for a page, ref, or selector to become ready in the browser session`,

	"browser_refresh": `Parameters:
- session_id (string, required): Browser agent session ID
Typical usage: Refresh the current page in the browser session and auto-observe`,

	"browser_back": `Parameters:
- session_id (string, required): Browser agent session ID
Typical usage: Go back in browser history inside the browser session and auto-observe`,

	"browser_extract": `Parameters:
- session_id (string, required): Browser agent session ID
- snapshot_id (string, optional): Snapshot ID for ref resolution
- ref (string, optional): Stable ref from browser_observe
- selector (string, optional): CSS selector fallback
- query (string, optional): What content to extract
- format (string, optional): Output format, default text
Typical usage: Extract page or element text from the browser session using refs, selectors, or whole-page summary`,

	"browser_connect": `Parameters:
- url (string, optional): CDP endpoint URL to connect to
- launch (bool, optional): Launch a new browser instance, default false
Typical usage: Connect to Chrome via CDP for browser automation`,

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

	"parallel_execute": `Parameters:
- tasks (array, required): List of task descriptions to execute in parallel
- max_concurrent (int, optional): Max concurrent sessions, default 3
Typical usage: Run multiple coding tasks simultaneously across sessions`,

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

	"control_session": `Parameters:
- session_id (string, required): Session ID to control
- action (string, required): Control action (interrupt/kill/resume)
Typical usage: Stop, interrupt, or resume a coding session`,

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
	"send_and_observe": {
		"send a message to the coding session and wait",
		"execute in coding tool and observe",
		"发送到编程工具并等待结果",
		"run this in the coding session",
		"ask the coding agent to do something",
	},
	"create_session": {
		"start a new coding session",
		"open a coding tool",
		"创建编程会话",
		"launch claude session",
		"begin a new coding task",
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
	"browser_session_start": {
		"start a browser agent session",
		"reuse an existing browser session",
		"启动浏览器会话",
		"open a browser session with allowed domains",
		"create a long lived browser automation session",
	},
	"browser_observe": {
		"observe the current browser page",
		"capture browser snapshot and refs",
		"观察浏览器页面",
		"take a browser snapshot with screenshot",
		"inspect page refs console and network",
	},
	"browser_navigate": {
		"navigate to a URL in the browser session",
		"open a webpage in browser session",
		"浏览器打开网页",
		"go to this website in current browser session",
		"visit a URL and observe the page",
	},
	"browser_click": {
		"click an element on the page",
		"click a button in the browser session",
		"点击网页元素",
		"press this ref on the page",
		"interact with a web element via ref",
	},
	"browser_type": {
		"type text into a browser field",
		"fill an input in browser session",
		"在浏览器输入内容",
		"enter text into page element by ref",
		"type into a web form",
	},
	"browser_wait": {
		"wait for browser page to load",
		"wait for selector in browser session",
		"等待浏览器页面稳定",
		"pause until a page element appears",
		"wait for a ref to become available",
	},
	"browser_refresh": {
		"refresh the current browser page",
		"reload browser session page",
		"刷新浏览器页面",
		"reload this website",
		"refresh and observe current page",
	},
	"browser_back": {
		"go back in browser history",
		"navigate back in browser session",
		"浏览器后退",
		"return to previous page",
		"back to last webpage",
	},
	"browser_extract": {
		"extract text from the webpage",
		"get content from browser page",
		"提取网页内容",
		"read text from page element by ref",
		"extract page summary or selected content",
	},
	"browser_connect": {
		"connect to a browser",
		"attach to Chrome for automation",
		"连接浏览器",
		"start browser automation",
		"open browser CDP connection",
	},
	"parallel_execute": {
		"run multiple tasks in parallel",
		"execute several coding sessions simultaneously",
		"并行执行多个任务",
		"do these things at the same time",
		"concurrent task execution",
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
	"control_session": {
		"control a coding session",
		"interrupt or kill a session",
		"控制会话",
		"stop the coding session",
		"manage session lifecycle",
	},
	"discover_tool": {
		"find a tool I don't have",
		"search for additional tools",
		"查找更多工具",
		"I need a capability not in the current list",
		"discover matching tools from MCP or SkillHub",
	},
}
