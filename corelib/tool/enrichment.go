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
- content (string, required): Content to write
- mode (string, optional): Write mode (overwrite/append), default overwrite
Typical usage: Create or update source files, configs, scripts`,

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
- server (string, required): MCP server name
- tool (string, required): Tool name on the server
- arguments (object, optional): Tool arguments as key-value pairs
Typical usage: Invoke external tools via MCP protocol`,

	"browser_connect": `Parameters:
- url (string, optional): CDP endpoint URL to connect to
- launch (bool, optional): Launch a new browser instance, default false
Typical usage: Connect to Chrome via CDP for browser automation`,

	"browser_navigate": `Parameters:
- url (string, required): URL to navigate to
- wait_until (string, optional): Wait condition (load/domcontentloaded/networkidle)
Typical usage: Open a webpage in the connected browser`,

	"browser_click": `Parameters:
- selector (string, required): CSS selector of the element to click
- button (string, optional): Mouse button (left/right/middle), default left
Typical usage: Click buttons, links, or interactive elements on a webpage`,

	"list_skills": `Parameters:
- filter (string, optional): Filter skills by name or tag
Typical usage: List installed NL skills, browse skill library`,

	"run_skill": `Parameters:
- name (string, required): Skill name to execute
- input (string, optional): Input text or parameters for the skill
Typical usage: Execute an NL skill by name with optional input`,

	"craft_tool": `Parameters:
- description (string, required): What the tool should do
- language (string, optional): Script language (bash/python), default bash
Typical usage: Generate a one-off automation script for a specific task`,

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
		"create a custom tool script",
		"write a one-off automation",
		"制作自定义工具",
		"generate a script for this task",
		"build a quick tool",
	},
	"browser_connect": {
		"connect to a browser",
		"attach to Chrome for automation",
		"连接浏览器",
		"start browser automation",
		"open browser CDP connection",
	},
	"browser_navigate": {
		"navigate to a URL in the browser",
		"open a webpage in browser",
		"浏览器打开网页",
		"go to this website",
		"visit a URL",
	},
	"browser_click": {
		"click an element on the page",
		"click a button in the browser",
		"点击网页元素",
		"press this button on the page",
		"interact with a web element",
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
