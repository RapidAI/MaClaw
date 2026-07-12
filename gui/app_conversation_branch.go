package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ─────────────────────────────────────────────────────────────────────────────
// Conversation Branching — Wails Bindings
//
// Lets users "go back to a point and try a different approach" in the AI
// assistant panel. The tree structure preserves all branches — no history lost.
// ─────────────────────────────────────────────────────────────────────────────

// ConversationBranchPoint is a branch point returned to the frontend.
type ConversationBranchPoint struct {
	Index    int      `json:"index"`    // index in the active branch (for UI display)
	EntryID  string   `json:"entry_id"` // tree entry ID (for BranchAt)
	Role     string   `json:"role"`
	Preview  string   `json:"preview"`  // first 80 chars of content
	Branches int      `json:"branches"` // number of branches from this point
	Labels   []string `json:"labels"`   // preview of each branch's first message
}

// ConversationBranchResult is returned after a branch operation.
type ConversationBranchResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	NewLength  int    `json:"new_length"`  // number of entries in the new active branch
	TotalNodes int    `json:"total_nodes"` // total entries across all branches
}

// BranchConversationAt creates a new branch from the given entry index.
// The conversation history is rewound to that point — the user can then
// send a new message to start a different path.
//
// Wails binding: called from AI assistant panel "branch from here" button.
func (a *App) BranchConversationAt(entryIndex int) (ConversationBranchResult, error) {
	userID := a.activeAIAssistantUserID()
	if userID == "" {
		return ConversationBranchResult{}, fmt.Errorf("no active session")
	}

	// Refuse to branch while agent loop is running — it would overwrite branched history.
	if a.imHandler != nil && a.imHandler.hasActiveLoopForUser(userID) {
		return ConversationBranchResult{}, fmt.Errorf("无法在任务执行期间创建分支，请先等待任务完成")
	}

	mem := a.aiConversationMemory
	if mem == nil {
		return ConversationBranchResult{}, fmt.Errorf("conversation memory not initialized")
	}

	entries := mem.Load(userID)
	if len(entries) == 0 {
		return ConversationBranchResult{}, fmt.Errorf("no conversation history")
	}
	if entryIndex < 0 || entryIndex >= len(entries) {
		return ConversationBranchResult{}, fmt.Errorf("invalid entry index: %d (history has %d entries)", entryIndex, len(entries))
	}

	// Build tree from all persisted nodes while keeping the current active tip.
	tree := agent.NewConversationTreeWithTip(mem.LoadAll(userID), mem.ActiveBranchTipID(userID))

	// Get the active branch (what the user sees in the chat panel).
	activeBranch := tree.ActiveBranch()
	if entryIndex >= len(activeBranch) {
		return ConversationBranchResult{}, fmt.Errorf("entry index %d exceeds active branch length (%d)", entryIndex, len(activeBranch))
	}

	// Map the display index back to the tree entry ID.
	targetID := activeBranch[entryIndex].ID

	if !tree.BranchAt(targetID) {
		return ConversationBranchResult{}, fmt.Errorf("cannot branch at entry %d", entryIndex)
	}

	if !mem.SetActiveBranchTip(userID, targetID) {
		return ConversationBranchResult{}, fmt.Errorf("cannot set active branch tip")
	}

	newBranch := tree.ActiveBranch()

	log.Printf("[branch] user=%s branched at index=%d, new_branch_len=%d total_nodes=%d",
		userID, entryIndex, len(newBranch), tree.Size())

	return ConversationBranchResult{
		Success:    true,
		Message:    fmt.Sprintf("已回退到第 %d 条消息，请发送新消息开始新分支", entryIndex+1),
		NewLength:  len(newBranch),
		TotalNodes: tree.Size(),
	}, nil
}

// GetConversationBranchPoints returns the points in the current conversation
// where branching is meaningful (user messages — you branch from decisions).
//
// Wails binding: called by frontend to render "branch from here" buttons.
func (a *App) GetConversationBranchPoints() ([]ConversationBranchPoint, error) {
	userID := a.activeAIAssistantUserID()
	if userID == "" {
		return nil, nil
	}

	mem := a.aiConversationMemory
	if mem == nil {
		return nil, nil
	}

	activeBranch := mem.Load(userID)
	if len(activeBranch) < 2 {
		return nil, nil // Need at least 2 messages to branch
	}
	tree := agent.NewConversationTreeWithTip(mem.LoadAll(userID), mem.ActiveBranchTipID(userID))
	branchInfo := make(map[string]agent.BranchInfo)
	for _, info := range tree.BranchPoints() {
		branchInfo[info.EntryID] = info
	}

	// Return user messages as potential branch points (you branch from decisions).
	var points []ConversationBranchPoint
	for i, entry := range activeBranch {
		if entry.Role != "user" && entry.Role != "assistant" {
			continue
		}
		preview := entryContentPreview(entry, 80)
		info := branchInfo[entry.ID]
		points = append(points, ConversationBranchPoint{
			Index:    i,
			EntryID:  entry.ID,
			Role:     entry.Role,
			Preview:  preview,
			Branches: len(info.BranchIDs),
			Labels:   info.Labels,
		})
	}
	return points, nil
}

// activeAIAssistantUserID returns the current user ID for the AI assistant.
func (a *App) activeAIAssistantUserID() string {
	if a.imHandler == nil {
		return ""
	}
	return activeAIAssistantLoopUserID(a.imHandler)
}

// entryContentPreview extracts a text preview from a ConversationEntry.
func entryContentPreview(entry agent.ConversationEntry, maxLen int) string {
	var text string
	switch content := entry.Content.(type) {
	case string:
		text = content
	case []interface{}:
		// Multi-part content — extract first text block.
		for _, part := range content {
			if m, ok := part.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					text = t
					break
				}
			}
		}
	default:
		text = fmt.Sprintf("[%s]", entry.Role)
	}

	if len([]rune(text)) > maxLen {
		runes := []rune(text)
		return string(runes[:maxLen]) + "…"
	}
	return text
}

// ─────────────────────────────────────────────────────────────────────────────
// /branch command — IM command handler
// ─────────────────────────────────────────────────────────────────────────────

// handleBranchCommand processes the /branch command from chat.
// Usage:
//
//	/branch       — list branch points (shows where you can branch from)
//	/branch N     — branch from message N (rewind to that point)
func (h *IMMessageHandler) handleBranchCommand(msg IMUserMessage, trimmed string) *IMAgentResponse {
	userID := msg.UserID
	lang := msg.Lang
	if lang == "" {
		lang = "zh"
	}
	isEN := strings.HasPrefix(strings.ToLower(lang), "en")

	// Refuse to branch while an agent loop is actively running — the loop
	// would overwrite the branched history on its next save.
	if h.hasActiveLoopForUser(userID) {
		if isEN {
			return &IMAgentResponse{Text: "Cannot branch while a task is running. Wait for completion or use /cancel."}
		}
		return &IMAgentResponse{Text: "无法在任务执行期间创建分支。请先等待任务完成或使用 /cancel 取消当前任务。"}
	}

	entries := h.memory.Load(userID)

	if len(entries) < 2 {
		if isEN {
			return &IMAgentResponse{Text: "Conversation history too short to branch. Need at least 2 messages."}
		}
		return &IMAgentResponse{Text: "对话历史太短，无法创建分支。至少需要 2 条消息。"}
	}

	// Parse argument: /branch N
	parts := strings.Fields(trimmed)
	if len(parts) == 1 {
		// No argument — show branch points (list messages with indices).
		var sb strings.Builder
		if isEN {
			sb.WriteString("**Conversation Branch Points**\n\n")
			sb.WriteString("Use `/branch N` to branch from message N (rewind to that point).\n\n")
		} else {
			sb.WriteString("**对话历史分支点**\n\n")
			sb.WriteString("使用 `/branch N` 从第 N 条消息处创建分支（回退到该点重新开始）。\n\n")
		}
		count := 0
		for i, entry := range entries {
			if entry.Role != "user" && entry.Role != "assistant" {
				continue
			}
			preview := entryContentPreview(entry, 50)
			icon := ""
			if entry.Role == "assistant" {
				icon = ""
			}
			sb.WriteString(fmt.Sprintf("`%d` %s %s\n", i, icon, preview))
			count++
			if count >= 20 {
				if isEN {
					sb.WriteString(fmt.Sprintf("\n... %d messages total, showing first 20\n", len(entries)))
				} else {
					sb.WriteString(fmt.Sprintf("\n... 共 %d 条消息，只显示前 20 条\n", len(entries)))
				}
				break
			}
		}
		return &IMAgentResponse{Text: sb.String()}
	}

	// Parse index.
	var targetIndex int
	if _, err := fmt.Sscanf(parts[1], "%d", &targetIndex); err != nil {
		if isEN {
			return &IMAgentResponse{Text: fmt.Sprintf("Invalid message number: %s. Use `/branch` to see available numbers.", parts[1])}
		}
		return &IMAgentResponse{Text: fmt.Sprintf("无效的消息编号：%s。使用 `/branch` 查看可用的编号。", parts[1])}
	}

	if targetIndex < 0 || targetIndex >= len(entries) {
		if isEN {
			return &IMAgentResponse{Text: fmt.Sprintf("Message number out of range (0-%d). Use `/branch` to see the list.", len(entries)-1)}
		}
		return &IMAgentResponse{Text: fmt.Sprintf("消息编号超出范围（0-%d）。使用 `/branch` 查看列表。", len(entries)-1)}
	}

	// Build tree from all persisted nodes, then switch the visible branch tip.
	tree := agent.NewConversationTreeWithTip(h.memory.LoadAll(userID), h.memory.ActiveBranchTipID(userID))
	targetID := entries[targetIndex].ID

	if !tree.BranchAt(targetID) {
		if isEN {
			return &IMAgentResponse{Text: "Cannot branch at this position."}
		}
		return &IMAgentResponse{Text: "无法在该位置创建分支。"}
	}

	newBranch := tree.ActiveBranch()
	if !h.memory.SetActiveBranchTip(userID, targetID) {
		return &IMAgentResponse{Text: "internal error: cannot set active branch tip"}
	}

	preview := entryContentPreview(entries[targetIndex], 40)
	log.Printf("[branch] user=%s command branch_at=%d new_len=%d total=%d", userID, targetIndex, len(newBranch), tree.Size())

	if isEN {
		return &IMAgentResponse{
			Text:    fmt.Sprintf("Branched from message #%d\n\n> %s\n\nConversation rewound. Send a new message to start a different path.\n\nPrevious messages after this point are no longer visible (saved in memory).", targetIndex, preview),
			ClearUI: true,
		}
	}
	return &IMAgentResponse{
		Text:    fmt.Sprintf("已从第 %d 条消息处创建分支\n\n> %s\n\n对话已回退到该点。请发送新消息开始新的对话路径。\n\n注意：之前该点之后的对话内容将不在当前视图中显示（已保存在记忆系统中）。", targetIndex, preview),
		ClearUI: true,
	}
}
