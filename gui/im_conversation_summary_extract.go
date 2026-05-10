package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// extractKeyDataFromEntries scans conversation entries for critical data
// references that must survive compaction: file paths, URLs, data statistics.
//
// These references typically appear in tool results (role:"tool") and in
// assistant messages that follow tool calls. Turn boundaries miss them
// because they only capture the first assistant response per turn.
//
// Returns deduplicated key data strings, capped at 30 items.
func extractKeyDataFromEntries(entries []agent.ConversationEntry) []string {
	seen := make(map[string]bool)
	var result []string
	const maxItems = 30

	for _, e := range entries {
		if len(result) >= maxItems {
			break
		}
		text, ok := e.Content.(string)
		if !ok || text == "" {
			continue
		}
		// Only scan tool results and assistant messages (not user messages 鈥?		// those are preserved verbatim in turn boundaries).
		if e.Role != "tool" && e.Role != "assistant" {
			continue
		}

		refs := extractKeyDataRefsFromText(text)
		for _, ref := range refs {
			if len(result) >= maxItems {
				break
			}
			if !seen[ref] {
				seen[ref] = true
				result = append(result, ref)
			}
		}
	}
	return result
}

// extractKeyDataRefsFromText extracts file paths, URLs, and data statistics
// from a text string. Uses pattern matching (not LLM) for speed.
// Returns at most 10 refs per text to avoid noise from large tool outputs.
func extractKeyDataRefsFromText(text string) []string {
	var refs []string
	const maxRefsPerText = 10

	// Scan each line for key data patterns.
	for _, line := range strings.Split(text, "\n") {
		if len(refs) >= maxRefsPerText {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Pattern 1: Windows absolute paths (C:\..., D:\...)
		// Pattern 2: Unix absolute paths (/home/..., /tmp/...)
		// Pattern 3: URLs (http://, https://)
		for _, token := range strings.Fields(line) {
			if len(refs) >= maxRefsPerText {
				break
			}
			cleaned := strings.Trim(token, "\"'`()[]{}，。；：,.;:")
			if cleaned == "" {
				continue
			}
			// Windows path: drive letter + colon + backslash
			if len(cleaned) >= 3 && cleaned[1] == ':' && (cleaned[2] == '\\' || cleaned[2] == '/') &&
				((cleaned[0] >= 'A' && cleaned[0] <= 'Z') || (cleaned[0] >= 'a' && cleaned[0] <= 'z')) {
				refs = append(refs, "鏂囦欢璺緞: "+cleaned)
				continue
			}
			// Unix absolute path (skip short ones like "/n" or "/")
			if len(cleaned) > 4 && cleaned[0] == '/' && cleaned[1] != '/' &&
				(strings.Contains(cleaned, "/") && strings.Count(cleaned, "/") >= 2) {
				refs = append(refs, "鏂囦欢璺緞: "+cleaned)
				continue
			}
			// URL
			if strings.HasPrefix(cleaned, "http://") || strings.HasPrefix(cleaned, "https://") {
				// Skip very common/noisy URLs
				if !strings.Contains(cleaned, "api.deepseek.com") &&
					!strings.Contains(cleaned, "api.openai.com") {
					runes := []rune(cleaned)
					if len(runes) > 120 {
						cleaned = string(runes[:120]) + "..."
					}
					refs = append(refs, "URL: "+cleaned)
				}
				continue
			}
		}

		// Pattern 4: Data statistics 鈥?lines containing numbers + Chinese
		// quantity words (绡?鏉?涓?浠?椤? near keywords (璁烘枃/璇勮/鏁版嵁/鏂囦欢).
		if len(refs) < maxRefsPerText && containsDataStatistic(line) {
			runes := []rune(line)
			if len(runes) > 150 {
				line = string(runes[:150]) + "..."
			}
			refs = append(refs, "鏁版嵁缁熻: "+line)
		}
	}
	return refs
}

// containsDataStatistic returns true if a line contains a data statistic
// pattern: a number followed by a Chinese quantity word near a data keyword.
func containsDataStatistic(line string) bool {
	// Must contain a digit
	hasDigit := false
	for _, r := range line {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return false
	}
	// Must contain a quantity word
	quantityWords := []string{"paper", "item", "file", "record", "result"}
	hasQuantity := false
	for _, w := range quantityWords {
		if strings.Contains(line, w) {
			hasQuantity = true
			break
		}
	}
	if !hasQuantity {
		return false
	}
	// Must contain a data keyword
	dataKeywords := []string{"璁烘枃", "璇勮", "鏁版嵁", "鏂囦欢", "璁板綍", "缁撴灉", "鎶ュ憡", "鍥剧墖", "瑙嗛", "paper", "comment", "file", "record"}
	for _, kw := range dataKeywords {
		if strings.Contains(line, kw) {
			return true
		}
	}
	return false
}

// extractFinalAssistantTexts returns the last assistant message before each
// new user turn in the conversation. These "conclusion" messages often
// contain the results of a multi-tool sequence (e.g., "99绡囪鏂囧凡淇濆瓨鍒?..").
//
// Turn boundaries capture the FIRST assistant response; this captures the
// LAST one 鈥?they are complementary. If a turn has only one assistant
// message, it's already captured by turn boundaries and is skipped here
// to avoid duplication in the summarizer input.
func extractFinalAssistantTexts(entries []agent.ConversationEntry, maxTexts int) []string {
	var texts []string
	var lastAssistantText string
	var lastAssistantIdx int = -1
	var firstAssistantIdx int = -1 // first assistant after the most recent user

	for i, e := range entries {
		text, ok := e.Content.(string)
		if !ok {
			continue
		}
		switch e.Role {
		case "assistant":
			if text != "" {
				if firstAssistantIdx < 0 {
					firstAssistantIdx = i
				}
				lastAssistantText = text
				lastAssistantIdx = i
			}
		case "user":
			// A new user turn 鈥?the previous assistant message is the "final"
			// one for the preceding turn.
			if lastAssistantIdx >= 0 && lastAssistantText != "" && len(texts) < maxTexts {
				// Skip if this is the same as the first assistant (already in turn boundaries).
				if lastAssistantIdx != firstAssistantIdx {
					runes := []rune(lastAssistantText)
					if len(runes) > 600 {
						lastAssistantText = string(runes[:600]) + "..."
					}
					texts = append(texts, lastAssistantText)
				}
			}
			lastAssistantText = ""
			lastAssistantIdx = -1
			firstAssistantIdx = -1
		}
	}
	// Don't forget the last assistant message at the end of the conversation.
	if lastAssistantIdx >= 0 && lastAssistantText != "" && len(texts) < maxTexts {
		if lastAssistantIdx != firstAssistantIdx {
			runes := []rune(lastAssistantText)
			if len(runes) > 600 {
				lastAssistantText = string(runes[:600]) + "..."
			}
			texts = append(texts, lastAssistantText)
		}
	}
	return texts
}

// extractToolOperationSummary extracts a concise summary of tool calls from
// conversation entries. Returns lines like:
//
//	"web_fetch: https://huggingface.co/papers"
//	"write_file: D:\workprj\hf_papers.json"
//	"generate_pdf: HF_World_鏃ユ姤_2026-04-30.pdf"
//
// This captures WHAT was done (tool names + key args), complementing
// extractKeyDataFromEntries (WHAT was produced) and extractTurnBoundaryTexts
// (WHAT was requested).
func extractToolOperationSummary(entries []agent.ConversationEntry, maxOps int) []string {
	// Two-pass approach: first count per-tool frequency, then emit summaries
	// with high-frequency tools capped at 2 examples + count.
	type toolOp struct {
		name   string
		keyArg string
	}
	var allOps []toolOp
	toolFreq := make(map[string]int)

	for _, e := range entries {
		if e.Role != "assistant" || e.ToolCalls == nil {
			continue
		}
		arr, ok := e.ToolCalls.([]interface{})
		if !ok {
			continue
		}
		for _, item := range arr {
			tc, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			fn, _ := tc["function"].(map[string]interface{})
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			if name == "" {
				continue
			}
			argsStr, _ := fn["arguments"].(string)
			keyArg := extractKeyToolArg(name, argsStr)
			allOps = append(allOps, toolOp{name: name, keyArg: keyArg})
			toolFreq[name]++
		}
	}

	// Emit: for each tool, show up to 2 distinct examples. If the tool was
	// called more than 2 times, append "(鍏盢娆?" to the last example.
	var ops []string
	toolEmitted := make(map[string]int)
	seen := make(map[string]bool)

	for _, op := range allOps {
		if len(ops) >= maxOps {
			break
		}
		emitted := toolEmitted[op.name]
		freq := toolFreq[op.name]

		// High-frequency tool (>2 calls): cap at 2 examples.
		if freq > 2 && emitted >= 2 {
			continue
		}

		summary := op.name
		if op.keyArg != "" {
			summary += ": " + op.keyArg
		}
		if seen[summary] {
			continue
		}
		seen[summary] = true
		toolEmitted[op.name] = emitted + 1

		// On the last emitted example for a high-frequency tool, append count.
		if freq > 2 && toolEmitted[op.name] >= 2 {
			summary += fmt.Sprintf(" (%d total)", freq)
		}
		ops = append(ops, summary)
	}
	return ops
}

// extractKeyToolArg extracts the most meaningful argument from a tool call's
// JSON arguments string, based on the tool name.
func extractKeyToolArg(toolName, argsJSON string) string {
	if argsJSON == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}

	// Tool-specific key argument extraction.
	switch classifyAgentToolKind(toolName) {
	case agentToolKindWebFetch:
		if v, ok := args["url"].(string); ok {
			return truncateStr(v, 100)
		}
	case agentToolKindWebSearch:
		if v, ok := args["query"].(string); ok {
			return truncateStr(v, 80)
		}
	case agentToolKindWriteFile, agentToolKindReadFile, agentToolKindEditFile:
		if v, ok := args["path"].(string); ok {
			return v
		}
	case agentToolKindGeneratePDF:
		if v, ok := args["title"].(string); ok {
			return truncateStr(v, 80)
		}
		if v, ok := args["output"].(string); ok {
			return v
		}
	case agentToolKindBash:
		if v, ok := args["command"].(string); ok {
			return truncateStr(v, 80)
		}
	case agentToolKindSendFile:
		if v, ok := args["file_path"].(string); ok {
			return v
		}
	case agentToolKindManageSkill:
		action, _ := args["action"].(string)
		name, _ := args["name"].(string)
		if action != "" && name != "" {
			return action + " " + name
		}
		if action != "" {
			return action
		}
	case agentToolKindSSH:
		action, _ := args["action"].(string)
		cmd, _ := args["command"].(string)
		if classifySSHToolAction(action) == sshToolActionExec && cmd != "" {
			return "exec: " + truncateStr(cmd, 60)
		}
		if action != "" {
			return action
		}
	case agentToolKindMemory:
		action, _ := args["action"].(string)
		if action != "" {
			return action
		}
	}

	// Generic fallback: first non-empty string argument, truncated.
	for _, key := range []string{"query", "url", "path", "command", "name", "text", "content"} {
		if v, ok := args[key].(string); ok && v != "" {
			return truncateStr(v, 80)
		}
	}
	return ""
}

// truncateStr truncates a string to maxLen runes, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
