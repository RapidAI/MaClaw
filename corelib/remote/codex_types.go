package remote

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// CodexEvent represents one JSONL event from `codex exec --json`.
type CodexEvent struct {
	Type     string      `json:"type"`
	ThreadID string      `json:"thread_id,omitempty"`
	Item     *CodexItem  `json:"item,omitempty"`
	Usage    *CodexUsage `json:"usage,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// CodexItem represents an item carried by a Codex event.
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

// CodexUsage represents token usage from a turn.completed event.
type CodexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	CacheWriteTokens  int `json:"cache_write_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens"`
}

func (u *CodexUsage) UnmarshalJSON(data []byte) error {
	type rawUsage struct {
		InputTokens              int            `json:"input_tokens"`
		PromptTokens             int            `json:"prompt_tokens"`
		OutputTokens             int            `json:"output_tokens"`
		CompletionTokens         int            `json:"completion_tokens"`
		CachedInputTokens        int            `json:"cached_input_tokens"`
		CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
		CacheWriteTokens         int            `json:"cache_write_tokens"`
		CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
		PromptTokensDetails      map[string]int `json:"prompt_tokens_details"`
		InputTokensDetails       map[string]int `json:"input_tokens_details"`
	}
	var raw rawUsage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.InputTokens = firstPositiveInt(raw.InputTokens, raw.PromptTokens)
	u.OutputTokens = firstPositiveInt(raw.OutputTokens, raw.CompletionTokens)
	u.CachedInputTokens = firstPositiveInt(
		raw.CachedInputTokens,
		raw.CacheReadInputTokens,
		raw.PromptTokensDetails["cached_tokens"],
		raw.InputTokensDetails["cached_tokens"],
	)
	u.CacheWriteTokens = firstPositiveInt(
		raw.CacheWriteTokens,
		raw.CacheCreationInputTokens,
		raw.PromptTokensDetails["cache_write_tokens"],
		raw.PromptTokensDetails["cache_creation_input_tokens"],
		raw.InputTokensDetails["cache_write_tokens"],
		raw.InputTokensDetails["cache_creation_input_tokens"],
	)
	return nil
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func codexUsageSummary(usage *CodexUsage) string {
	if usage == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("%d in", usage.InputTokens), fmt.Sprintf("%d out", usage.OutputTokens)}
	if usage.CachedInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d cache read", usage.CachedInputTokens))
	}
	if usage.CacheWriteTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d cache write", usage.CacheWriteTokens))
	}
	return strings.Join(parts, ", ")
}

// CodexEventToText converts a Codex JSONL event into readable text.
func CodexEventToText(event CodexEvent) string {
	switch event.Type {
	case "thread.started", "turn.started":
		return ""
	case "turn.completed":
		if event.Usage != nil {
			return fmt.Sprintf("Turn completed (tokens: %s)", codexUsageSummary(event.Usage))
		}
		return "Turn completed"
	case "turn.failed":
		if event.Error != "" {
			return fmt.Sprintf("Turn failed: %s", event.Error)
		}
		return "Turn failed"
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
			return fmt.Sprintf("Reasoning: %s", item.Text)
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
				return fmt.Sprintf("Command: %s", cmd)
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
				return fmt.Sprintf("Editing %s", item.FilePath)
			}
		case "item.completed":
			if item.FilePath != "" {
				return fmt.Sprintf("Modified %s", item.FilePath)
			}
		}
		return ""
	case "mcp_tool_call":
		if item.ToolName != "" {
			return fmt.Sprintf("MCP: %s", item.ToolName)
		}
		return ""
	case "web_search":
		if item.Query != "" {
			return fmt.Sprintf("Searching: %s", item.Query)
		}
		return ""
	default:
		return ""
	}
}

// BuildCodexToolUseEvent creates an ImportantEvent from a Codex item event.
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

func BuildCodexConfigToml(m *corelib.ModelConfig) string {
	if m == nil {
		return configfile.BuildCodexConfigTomlContent("", "", "custom", "responses")
	}
	return configfile.BuildCodexConfigTomlContentWithClientName(
		strings.TrimSpace(m.ModelUrl),
		strings.TrimSpace(m.ModelId),
		strings.TrimSpace(m.ModelName),
		strings.TrimSpace(m.WireApi),
		strings.TrimSpace(m.AgentType),
	)
}
