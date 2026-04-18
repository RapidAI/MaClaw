package session

import (
	"fmt"
	"strings"
)

// TranscriptEntry represents a single message in a conversation.
type TranscriptEntry struct {
	Role       string         `json:"role"`                  // "user", "assistant", "system", "tool"
	Content    string         `json:"content"`
	ToolCalls  []ToolCallMeta `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// ToolCallMeta holds metadata about a tool invocation.
type ToolCallMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// entrySeparator is the delimiter between transcript entries.
const entrySeparator = "---"

// Serialize converts a conversation history into a searchable plain-text format.
// The format preserves role markers, content, and tool call metadata in a
// deterministic, parseable structure.
func Serialize(entries []TranscriptEntry) string {
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, entry := range entries {
		if i > 0 {
			sb.WriteString(entrySeparator)
			sb.WriteByte('\n')
		}

		switch {
		case len(entry.ToolCalls) > 0:
			// Tool call entries: one block per tool call
			for j, tc := range entry.ToolCalls {
				if j > 0 {
					sb.WriteString(entrySeparator)
					sb.WriteByte('\n')
				}
				sb.WriteString(fmt.Sprintf("[tool_call:%s name:%s]\n", tc.ID, tc.Name))
				sb.WriteString(escapeContent(tc.Args))
				sb.WriteByte('\n')
			}
			// If there's also content on the entry (e.g., assistant text + tool calls),
			// append it as a separate assistant block.
			if entry.Content != "" {
				sb.WriteString(entrySeparator)
				sb.WriteByte('\n')
				sb.WriteString(fmt.Sprintf("[%s]\n", entry.Role))
				sb.WriteString(escapeContent(entry.Content))
				sb.WriteByte('\n')
			}

		case entry.ToolCallID != "":
			// Tool result entry
			sb.WriteString(fmt.Sprintf("[tool_result:%s]\n", entry.ToolCallID))
			sb.WriteString(escapeContent(entry.Content))
			sb.WriteByte('\n')

		default:
			// Regular message (user, assistant, system)
			sb.WriteString(fmt.Sprintf("[%s]\n", entry.Role))
			sb.WriteString(escapeContent(entry.Content))
			sb.WriteByte('\n')
		}
	}
	sb.WriteString(entrySeparator)
	sb.WriteByte('\n')

	return sb.String()
}

// Deserialize reconstructs the conversation structure from serialized text.
func Deserialize(text string) ([]TranscriptEntry, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	// Split by separator lines. Each block is between two "---" lines.
	blocks := splitBlocks(text)

	var entries []TranscriptEntry
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		entry, err := parseBlock(block)
		if err != nil {
			return nil, fmt.Errorf("deserialize block: %w", err)
		}
		entries = mergeEntry(entries, entry)
	}

	return entries, nil
}

// splitBlocks splits the serialized text into blocks separated by "---" lines.
// Only bare "---" lines (no leading/trailing whitespace) are treated as separators.
// Content lines that happen to equal "---" inside a block are escaped during
// serialization as "---\x00" and unescaped here.
func splitBlocks(text string) []string {
	var blocks []string
	var current strings.Builder

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if line == entrySeparator {
			if current.Len() > 0 {
				blocks = append(blocks, current.String())
				current.Reset()
			}
		} else {
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			// Unescape content lines that were escaped during serialization.
			if line == entrySeparator+"\x00" {
				current.WriteString(entrySeparator)
			} else {
				current.WriteString(line)
			}
		}
	}
	// Handle trailing content without a final separator
	if current.Len() > 0 {
		blocks = append(blocks, current.String())
	}

	return blocks
}

// escapeContent escapes any bare "---" lines in content so they don't
// collide with the entry separator during deserialization.
func escapeContent(s string) string {
	if !strings.Contains(s, entrySeparator) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == entrySeparator {
			lines[i] = entrySeparator + "\x00"
		}
	}
	return strings.Join(lines, "\n")
}

// parseBlock parses a single block into a TranscriptEntry.
func parseBlock(block string) (TranscriptEntry, error) {
	// Find the first newline to separate the header from content
	headerEnd := strings.IndexByte(block, '\n')
	var header, content string
	if headerEnd == -1 {
		header = block
		content = ""
	} else {
		header = block[:headerEnd]
		content = block[headerEnd+1:]
	}

	header = strings.TrimSpace(header)

	// Try tool_call format: [tool_call:ID name:NAME]
	if strings.HasPrefix(header, "[tool_call:") && strings.HasSuffix(header, "]") {
		inner := header[1 : len(header)-1] // strip [ and ]
		tc, err := parseToolCallHeader(inner)
		if err != nil {
			return TranscriptEntry{}, err
		}
		tc.Args = content
		return TranscriptEntry{
			Role:      "assistant",
			ToolCalls: []ToolCallMeta{tc},
		}, nil
	}

	// Try tool_result format: [tool_result:ID]
	if strings.HasPrefix(header, "[tool_result:") && strings.HasSuffix(header, "]") {
		id := header[len("[tool_result:") : len(header)-1]
		return TranscriptEntry{
			Role:       "tool",
			Content:    content,
			ToolCallID: id,
		}, nil
	}

	// Regular role marker: [user], [assistant], [system], [tool]
	if strings.HasPrefix(header, "[") && strings.HasSuffix(header, "]") {
		role := header[1 : len(header)-1]
		return TranscriptEntry{
			Role:    role,
			Content: content,
		}, nil
	}

	return TranscriptEntry{}, fmt.Errorf("unrecognized block header: %q", header)
}

// parseToolCallHeader parses "tool_call:ID name:NAME" from the inner bracket content.
func parseToolCallHeader(inner string) (ToolCallMeta, error) {
	// Format: "tool_call:CALL_ID name:TOOL_NAME"
	if !strings.HasPrefix(inner, "tool_call:") {
		return ToolCallMeta{}, fmt.Errorf("invalid tool_call header: %q", inner)
	}

	rest := inner[len("tool_call:"):]
	// Find " name:" separator
	nameIdx := strings.Index(rest, " name:")
	if nameIdx == -1 {
		return ToolCallMeta{}, fmt.Errorf("missing name field in tool_call header: %q", inner)
	}

	id := rest[:nameIdx]
	name := rest[nameIdx+len(" name:"):]

	return ToolCallMeta{
		ID:   id,
		Name: name,
	}, nil
}

// mergeEntry merges a parsed entry into the entries slice.
// Consecutive tool_call blocks for the same role are merged into a single entry.
func mergeEntry(entries []TranscriptEntry, entry TranscriptEntry) []TranscriptEntry {
	if len(entry.ToolCalls) > 0 && len(entries) > 0 {
		last := &entries[len(entries)-1]
		// Merge consecutive tool_call entries into one assistant entry
		if last.Role == "assistant" && len(last.ToolCalls) > 0 && last.Content == "" && entry.Content == "" {
			last.ToolCalls = append(last.ToolCalls, entry.ToolCalls...)
			return entries
		}
	}
	return append(entries, entry)
}
