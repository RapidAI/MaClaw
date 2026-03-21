package remote

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// CodexEvent 表示 `codex exec --json` 的单个 JSONL 事件。
type CodexEvent struct {
	Type     string      `json:"type"`
	ThreadID string      `json:"thread_id,omitempty"`
	Item     *CodexItem  `json:"item,omitempty"`
	Usage    *CodexUsage `json:"usage,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// CodexItem 表示 Codex 事件中的一个条目。
type CodexItem struct {
	ID               string `json:"id"`
	ItemType         string `json:"item_type"`
	Text             string `json:"text,omitempty"`
	Command          string `json:"command,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	Status           string `json:"status,omitempty"`
	FilePath         string `json:"file_path,omitempty"`
	Diff             string `json:"diff,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
	Query            string `json:"query,omitempty"`
}

// CodexUsage 表示 turn.completed 事件中的 token 使用量。
type CodexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// CodexEventToText 将 Codex JSONL 事件转换为可读文本。
func CodexEventToText(event CodexEvent) string {
	switch event.Type {
	case "thread.started", "turn.started":
		return ""
	case "turn.completed":
		if event.Usage != nil {
			return fmt.Sprintf("✓ Turn completed (tokens: %d in, %d out)",
				event.Usage.InputTokens, event.Usage.OutputTokens)
		}
		return "✓ Turn completed"
	case "turn.failed":
		if event.Error != "" {
			return fmt.Sprintf("✗ Turn failed: %s", event.Error)
		}
		return "✗ Turn failed"
	case "item.started", "item.updated", "item.completed":
		return codexItemToText(event)
	default:
		return ""
	}
}

func codexItemToText(event CodexEvent) string {
	if event.Item == nil {
		return ""
	}
	item := event.Item

	switch item.ItemType {
	case "assistant_message":
		if event.Type == "item.completed" && item.Text != "" {
			return item.Text
		}
		return ""
	case "reasoning":
		if item.Text != "" {
			return fmt.Sprintf("💭 %s", item.Text)
		}
		return ""
	case "command_execution":
		switch event.Type {
		case "item.started":
			if item.Command != "" {
				cmd := item.Command
				if len(cmd) > 100 {
					cmd = cmd[:100] + "..."
				}
				return fmt.Sprintf("⚡ %s", cmd)
			}
		case "item.completed":
			if item.AggregatedOutput != "" {
				output := strings.TrimSpace(item.AggregatedOutput)
				if len(output) > 500 {
					output = output[:500] + "..."
				}
				exitStr := ""
				if item.ExitCode != nil {
					exitStr = fmt.Sprintf(" (exit %d)", *item.ExitCode)
				}
				return fmt.Sprintf("%s%s", output, exitStr)
			}
		}
		return ""
	case "file_change":
		switch event.Type {
		case "item.started":
			if item.FilePath != "" {
				return fmt.Sprintf("📝 Editing %s", item.FilePath)
			}
		case "item.completed":
			if item.FilePath != "" {
				return fmt.Sprintf("✓ Modified %s", item.FilePath)
			}
		}
		return ""
	case "mcp_tool_call":
		if item.ToolName != "" {
			return fmt.Sprintf("🔧 MCP: %s", item.ToolName)
		}
		return ""
	case "web_search":
		if item.Query != "" {
			return fmt.Sprintf("🔍 Searching: %s", item.Query)
		}
		return ""
	default:
		return ""
	}
}

// BuildCodexToolUseEvent 从 Codex item 事件创建 ImportantEvent。
func BuildCodexToolUseEvent(sessionID string, event CodexEvent) ImportantEvent {
	if event.Item == nil {
		return ImportantEvent{}
	}
	item := event.Item

	evt := ImportantEvent{
		EventID:   fmt.Sprintf("codex_%s_%d", item.ID, time.Now().UnixNano()),
		SessionID: sessionID,
		CreatedAt: time.Now().Unix(),
		Count:     1,
	}

	switch item.ItemType {
	case "command_execution":
		evt.Type = "command.started"
		evt.Severity = "info"
		evt.Title = "Running command"
		evt.Command = item.Command
		evt.Summary = item.Command
		if len(evt.Summary) > 100 {
			evt.Summary = evt.Summary[:100] + "..."
		}
	case "file_change":
		evt.Type = "file.change"
		evt.Severity = "info"
		evt.Title = "File modified"
		evt.RelatedFile = item.FilePath
		evt.Summary = fmt.Sprintf("Modified %s", item.FilePath)
	case "mcp_tool_call":
		evt.Type = "tool.call"
		evt.Severity = "info"
		evt.Title = "MCP tool call"
		evt.Summary = fmt.Sprintf("Called %s", item.ToolName)
	case "web_search":
		evt.Type = "web.search"
		evt.Severity = "info"
		evt.Title = "Web search"
		evt.Summary = fmt.Sprintf("Searched: %s", item.Query)
	default:
		return ImportantEvent{}
	}

	return evt
}

// tomlKeySanitize strips characters that are invalid in a bare TOML key,
// keeping only ASCII letters, digits, hyphens, and underscores.
var tomlKeyRe = regexp.MustCompile(`[^a-z0-9_-]`)

func sanitizeTomlKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = tomlKeyRe.ReplaceAllString(s, "")
	if s == "" {
		return "custom"
	}
	return s
}

// BuildCodexConfigToml generates a unified config.toml content for Codex CLI.
// It replaces the deprecated OPENAI_BASE_URL env var approach with proper
// model_providers configuration, including WebSocket support.
// This is shared between GUI and TUI to avoid duplication.
func BuildCodexConfigToml(m *corelib.ModelConfig) string {
	providerName := strings.ToLower(strings.TrimSpace(m.ModelName))
	if providerName == "" || providerName == "custom" {
		providerName = "custom"
	}
	// Normalize Chinese names to ASCII provider keys
	switch providerName {
	case "讯飞星辰":
		providerName = "xfyun"
	case "阿里云":
		providerName = "aliyun"
	}
	// Sanitize for use as TOML bare key
	providerName = sanitizeTomlKey(providerName)

	modelId := strings.TrimSpace(m.ModelId)
	if modelId == "" {
		modelId = "gpt-5.4"
	}

	baseUrl := strings.TrimSpace(m.ModelUrl)

	wireApi := strings.TrimSpace(m.WireApi)
	if wireApi == "" {
		wireApi = "responses"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "model_provider = %q\n", providerName)
	fmt.Fprintf(&sb, "model = %q\n", modelId)
	sb.WriteString("model_reasoning_effort = \"xhigh\"\n")
	sb.WriteString("disable_response_storage = true\n")

	fmt.Fprintf(&sb, "\n[model_providers.%s]\n", providerName)
	fmt.Fprintf(&sb, "name = %q\n", providerName)
	if baseUrl != "" {
		fmt.Fprintf(&sb, "base_url = %q\n", baseUrl)
	}
	fmt.Fprintf(&sb, "wire_api = %q\n", wireApi)
	sb.WriteString("supports_websockets = true\n")
	sb.WriteString("requires_openai_auth = true\n")

	sb.WriteString("\n[features]\n")
	sb.WriteString("responses_websockets_v2 = true\n")

	return sb.String()
}
