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
func GenerateEnrichmentPrompt(toolName, description string) (system, user string) {
	system = `You are a tool usage analyst. Given a tool's name and description, generate 5 typical user queries that would require this tool. Output ONLY a JSON array of strings, no markdown, no commentary.`
	user = fmt.Sprintf("Tool: %s\nDescription: %s", toolName, description)
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
