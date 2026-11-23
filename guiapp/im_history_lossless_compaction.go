package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func historyMayHaveCompacted(original, trimmed []agent.ConversationEntry) bool {
	if len(original) != len(trimmed) {
		return true
	}
	return len(original) > 0 && len(trimmed) > 0 && &original[0] != &trimmed[0]
}

func (h *IMMessageHandler) preserveAutoCompactionHistory(userID string, original, trimmed []agent.ConversationEntry) ([]agent.ConversationEntry, bool) {
	dropped := removedConversationEntries(original, trimmed)
	if len(dropped) == 0 {
		return trimmed, false
	}
	raw, err := json.Marshal(dropped)
	if err != nil {
		log.Printf("[compaction] keep original history: marshal dropped entries failed: %v", err)
		return original, false
	}
	checkpointIndex := -1
	preview := fmt.Sprintf("[上下文压缩] %d 条早期记录已移出当前上下文，完整原文可通过以下句柄检索。", len(dropped))
	for i, entry := range trimmed {
		if entry.CompactionID == "" && !isCompactionHandoffEntry(entry) {
			continue
		}
		checkpointIndex = i
		if text, ok := entry.Content.(string); ok && strings.TrimSpace(text) != "" {
			preview = text
		}
	}
	sessionKey := userID
	if h != nil {
		sessionKey = h.workflowPolicyOwnerID(userID, nil)
	}
	projection, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:   "context_checkpoint",
		SessionKey: sessionKey,
		Content:    string(raw),
		Preview:    preview,
		Limit:      len(preview) + 1024,
		ForceSpill: true,
		Encrypt:    true,
	})
	if err != nil || projection.Handle == nil || !projection.Spilled {
		log.Printf("[compaction] keep original history: persist dropped entries failed: %v", err)
		return original, false
	}
	out := append([]agent.ConversationEntry(nil), trimmed...)
	if checkpointIndex >= 0 {
		out[checkpointIndex].Content = projection.Preview
	} else {
		out = append([]agent.ConversationEntry{{Role: "system", Content: projection.Preview}}, out...)
	}
	log.Printf("[compaction] preserved dropped history user=%s entries=%d bytes=%d handle=%s", userID, len(dropped), len(raw), projection.Handle.ID)
	return out, true
}

func removedConversationEntries(original, trimmed []agent.ConversationEntry) []agent.ConversationEntry {
	kept := make(map[string]int, len(trimmed))
	for _, entry := range trimmed {
		data, err := json.Marshal(entry)
		if err == nil {
			kept[string(data)]++
		}
	}
	var dropped []agent.ConversationEntry
	for _, entry := range original {
		data, err := json.Marshal(entry)
		if err != nil {
			dropped = append(dropped, entry)
			continue
		}
		key := string(data)
		if kept[key] > 0 {
			kept[key]--
			continue
		}
		dropped = append(dropped, entry)
	}
	return dropped
}

func preserveContextCheckpointFooter(content, prior string) string {
	if strings.Contains(content, toolresult.HandleFooterMarker) {
		return content
	}
	index := strings.Index(prior, toolresult.HandleFooterMarker)
	if index < 0 {
		return content
	}
	footer := strings.TrimSpace(prior[index:])
	if footer == "" {
		return content
	}
	return strings.TrimRight(content, "\n") + "\n\n" + footer
}
