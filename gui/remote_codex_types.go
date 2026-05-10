package main

import (
	"fmt"
	"strings"
	"time"
)

// Codex exec --json JSONL event types.
// See: https://docs.onlinetool.cc/codex/docs/exec.html

// CodexEvent represents a single JSONL event from `codex exec --json`.
type CodexEvent struct {
	Type     string      `json:"type"`
	ThreadID string      `json:"thread_id,omitempty"`
	Item     *CodexItem  `json:"item,omitempty"`
	Usage    *CodexUsage `json:"usage,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// CodexItem represents an item within a Codex event.
// Item types: assistant_message, reasoning, command_execution, file_change, mcp_tool_call, web_search
type CodexItem struct {
	ID               string `json:"id"`
	ItemType         string `json:"item_type"`
	Text             string `json:"text,omitempty"`
	Command          string `json:"command,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	Status           string `json:"status,omitempty"`
	// file_change fields
	FilePath string `json:"file_path,omitempty"`
	Diff     string `json:"diff,omitempty"`
	// mcp_tool_call fields
	ToolName string `json:"tool_name,omitempty"`
	// web_search fields
	Query string `json:"query,omitempty"`
}

// CodexUsage represents token usage reported in turn.completed events.
type CodexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// codexEventToText converts a Codex JSONL event to human-readable text
// for the output pipeline and preview display.
func codexEventToText(event CodexEvent) string {
	switch event.Type {
	case "thread.started":
		return "" // handled by session status update

	case "turn.started":
		return "" // no visible output needed

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

// buildCodexToolUseEvent creates an ImportantEvent from a Codex item event.
func buildCodexToolUseEvent(session *RemoteSession, event CodexEvent) ImportantEvent {
	if event.Item == nil {
		return ImportantEvent{}
	}
	item := event.Item

	evt := ImportantEvent{
		EventID:   fmt.Sprintf("codex_%s_%d", item.ID, time.Now().UnixNano()),
		SessionID: session.ID,
		CreatedAt: time.Now().Unix(),
		Count:     1,
	}

	switch item.ItemType {
	case "command_execution":
		evt.Type = summaryEventCommandStarted.String()
		evt.Severity = "info"
		evt.Title = "Running command"
		evt.Command = item.Command
		evt.Summary = item.Command
		if len(evt.Summary) > 100 {
			evt.Summary = evt.Summary[:100] + "..."
		}
	case "file_change":
		evt.Type = summaryEventFileChange.String()
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
