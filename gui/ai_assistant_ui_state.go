package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const (
	maxAIAssistantUIStateMessages = 200
	maxAIAssistantUIStatePrompts  = 100
)

var aiAssistantUIStateMu sync.Mutex

// AIAssistantUIState stores frontend-only AI assistant UI history under the
// MaClaw data directory, avoiding WebView localStorage profile splits.
type AIAssistantUIState struct {
	Messages                 []map[string]interface{} `json:"messages"`
	Prompts                  []string                 `json:"prompts"`
	ContextBoundaryMessageID string                   `json:"context_boundary_message_id,omitempty"`
	UpdatedAt                string                   `json:"updated_at,omitempty"`
	StoragePath              string                   `json:"storage_path,omitempty"`
}

func (a *App) aiAssistantUIStatePath() string {
	return filepath.Join(a.GetDataDir(), "ai_assistant_ui_state.json")
}

func (a *App) LoadAIAssistantUIState() (AIAssistantUIState, error) {
	aiAssistantUIStateMu.Lock()
	defer aiAssistantUIStateMu.Unlock()

	path := a.aiAssistantUIStatePath()
	state := AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}, StoragePath: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.injectStartupRecoveryCard(&state)
			log.Printf("[paths] ai_assistant_ui_state load missing path=%q", path)
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}, StoragePath: path}, err
	}
	normalizeAIAssistantUIState(&state)
	if aiAssistantUIStateNeedsSanitizedRewrite(data, state) {
		state.StoragePath = ""
		if err := configfile.AtomicWriteJSON(path, state); err != nil {
			return AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}, StoragePath: path}, err
		}
		log.Printf("[paths] ai_assistant_ui_state sanitized path=%q messages=%d prompts=%d boundary=%q", path, len(state.Messages), len(state.Prompts), state.ContextBoundaryMessageID)
	}
	state.StoragePath = path
	a.injectStartupRecoveryCard(&state)
	log.Printf("[paths] ai_assistant_ui_state load path=%q messages=%d prompts=%d boundary=%q", path, len(state.Messages), len(state.Prompts), state.ContextBoundaryMessageID)
	return state, nil
}

// injectStartupRecoveryCard projects a durable crash-recovery slot into the
// existing unfinished-task card UI. It never sends an LLM request and never
// replays a tool call; the user must explicitly choose a card action.
func (a *App) injectStartupRecoveryCard(state *AIAssistantUIState) {
	if a == nil || state == nil {
		return
	}
	mem := a.ensureConversationMemory()
	if mem == nil {
		return
	}
	slots := make([]*agent.UnfinishedTaskSlot, 0)
	for _, candidate := range mem.UnfinishedSlots() {
		if candidate == nil || !candidate.Source.IsInFlightRecovery() || strings.TrimSpace(candidate.SlotID) == "" {
			continue
		}
		status := candidate.Status
		if status == agent.UnfinishedTaskSlotStatusResumed || status == agent.UnfinishedTaskSlotStatusCompleted {
			continue
		}
		slots = append(slots, candidate)
	}
	if len(slots) == 0 {
		return
	}
	// UnfinishedSlots traverses sharded maps, so impose a stable order before
	// projecting cards into the persisted UI timeline.
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].CreatedAt.Equal(slots[j].CreatedAt) {
			if slots[i].UserID == slots[j].UserID {
				return slots[i].SlotID < slots[j].SlotID
			}
			return slots[i].UserID < slots[j].UserID
		}
		return slots[i].CreatedAt.Before(slots[j].CreatedAt)
	})
	existing := make(map[string]struct{}, len(state.Messages))
	for _, message := range state.Messages {
		if raw, ok := message["unfinishedSlot"].(map[string]interface{}); ok {
			if slotID, ok := raw["slotID"].(string); ok {
				existing[slotID] = struct{}{}
			}
		}
	}
	for _, slot := range slots {
		if _, alreadyProjected := existing[slot.SlotID]; alreadyProjected {
			continue
		}
		payload := startupRecoverySlotPayload(slot)
		state.Messages = append(state.Messages, map[string]interface{}{
			"id":             "startup-recovery-" + slot.SlotID,
			"role":           "assistant",
			"content":        buildUnfinishedSlotHint(slot),
			"sessionKey":     strings.TrimSpace(slot.UserID),
			"unfinishedSlot": payload,
			"timestamp":      time.Now().UnixMilli(),
		})
	}
	normalizeAIAssistantUIState(state)
}

func startupRecoverySlotPayload(slot *agent.UnfinishedTaskSlot) map[string]interface{} {
	payload := buildUnfinishedTaskPayload(slot)
	if payload == nil {
		return nil
	}
	return map[string]interface{}{
		"slotID":          payload.SlotID,
		"title":           payload.Title,
		"summary":         payload.Summary,
		"projectPath":     payload.ProjectPath,
		"status":          payload.Status,
		"lastToolName":    payload.LastToolName,
		"sideEffectState": payload.SideEffectState,
		"recoveryMode":    payload.RecoveryMode,
		"actions":         payload.Actions,
	}
}

func (a *App) SaveAIAssistantUIState(state AIAssistantUIState) error {
	aiAssistantUIStateMu.Lock()
	defer aiAssistantUIStateMu.Unlock()

	path := a.aiAssistantUIStatePath()
	state.StoragePath = ""
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	normalizeAIAssistantUIState(&state)
	if err := configfile.AtomicWriteJSON(path, state); err != nil {
		return err
	}
	log.Printf("[paths] ai_assistant_ui_state save path=%q messages=%d prompts=%d boundary=%q", path, len(state.Messages), len(state.Prompts), state.ContextBoundaryMessageID)
	return nil
}

func (a *App) ClearAIAssistantUIState() error {
	aiAssistantUIStateMu.Lock()
	defer aiAssistantUIStateMu.Unlock()

	path := a.aiAssistantUIStatePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	log.Printf("[paths] ai_assistant_ui_state clear path=%q", path)
	return nil
}

func normalizeAIAssistantUIState(state *AIAssistantUIState) {
	if state == nil {
		return
	}
	state.ContextBoundaryMessageID = strings.TrimSpace(state.ContextBoundaryMessageID)
	if len(state.Messages) > 0 {
		messages := make([]map[string]interface{}, 0, len(state.Messages))
		for _, message := range state.Messages {
			normalizeAIAssistantUIMessage(message)
			if isPersistableAIAssistantUIMessage(message) {
				messages = append(messages, message)
			}
		}
		state.Messages = messages
	}
	if len(state.Messages) > maxAIAssistantUIStateMessages {
		state.Messages = state.Messages[len(state.Messages)-maxAIAssistantUIStateMessages:]
	}
	if state.Messages == nil {
		state.Messages = []map[string]interface{}{}
	}
	if len(state.Prompts) > 0 {
		prompts := make([]string, 0, len(state.Prompts))
		for _, prompt := range state.Prompts {
			trimmed := strings.TrimSpace(prompt)
			if trimmed != "" {
				prompts = append(prompts, trimmed)
			}
		}
		state.Prompts = prompts
	}
	if len(state.Prompts) > maxAIAssistantUIStatePrompts {
		state.Prompts = state.Prompts[len(state.Prompts)-maxAIAssistantUIStatePrompts:]
	}
	if state.Prompts == nil {
		state.Prompts = []string{}
	}
}

func normalizeAIAssistantUIMessage(message map[string]interface{}) {
	if message == nil {
		return
	}
	role, _ := message["role"].(string)
	if strings.TrimSpace(role) != "assistant" {
		return
	}
	if content, ok := message["content"].(string); ok && content != "" {
		message["content"] = sanitizeAIAssistantUIPersistedText(llm.StripAllExtra(content))
	}
	if reasoning, ok := message["reasoning"].(string); ok && reasoning != "" {
		message["reasoning"] = sanitizeAIAssistantUIPersistedText(reasoning)
	}
}

func sanitizeAIAssistantUIPersistedText(value string) string {
	value = strings.TrimPrefix(value, "\x01")
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return r
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, value)
}

func aiAssistantUIStateNeedsSanitizedRewrite(data []byte, state AIAssistantUIState) bool {
	lower := strings.ToLower(string(data))
	if strings.Contains(lower, "<details") ||
		strings.Contains(lower, "<think>") ||
		strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<turn: tool_call") ||
		strings.Contains(lower, "<|functioncallbegin|>") {
		return true
	}
	for r := rune(0); r <= 0x9f; r++ {
		if r == '\n' || r == '\r' || r == '\t' || (r > 0x1f && r < 0x7f) {
			continue
		}
		if strings.Contains(lower, fmt.Sprintf(`\u%04x`, r)) {
			return true
		}
	}
	return aiAssistantUIStateContainsForbiddenControls(state)
}

func aiAssistantUIStateContainsForbiddenControls(state AIAssistantUIState) bool {
	for _, message := range state.Messages {
		for _, field := range []string{"content", "reasoning"} {
			value, _ := message[field].(string)
			if value != sanitizeAIAssistantUIPersistedText(value) {
				return true
			}
		}
	}
	return false
}

func isPersistableAIAssistantUIMessage(message map[string]interface{}) bool {
	if message == nil {
		return false
	}
	id, _ := message["id"].(string)
	role, _ := message["role"].(string)
	role = strings.TrimSpace(role)
	if strings.TrimSpace(id) == "" || role == "" {
		return false
	}
	switch role {
	case "progress", "system":
		return false
	}
	return true
}
