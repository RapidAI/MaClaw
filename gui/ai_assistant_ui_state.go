package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
			log.Printf("[paths] ai_assistant_ui_state load missing path=%q", path)
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}, StoragePath: path}, err
	}
	normalizeAIAssistantUIState(&state)
	if aiAssistantUIStateNeedsSanitizedRewrite(data) {
		state.StoragePath = ""
		if err := configfile.AtomicWriteJSON(path, state); err != nil {
			return AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}, StoragePath: path}, err
		}
		log.Printf("[paths] ai_assistant_ui_state sanitized path=%q messages=%d prompts=%d boundary=%q", path, len(state.Messages), len(state.Prompts), state.ContextBoundaryMessageID)
	}
	state.StoragePath = path
	log.Printf("[paths] ai_assistant_ui_state load path=%q messages=%d prompts=%d boundary=%q", path, len(state.Messages), len(state.Prompts), state.ContextBoundaryMessageID)
	return state, nil
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
	content, ok := message["content"].(string)
	if !ok || content == "" {
		return
	}
	message["content"] = llm.StripAllExtra(content)
}

func aiAssistantUIStateNeedsSanitizedRewrite(data []byte) bool {
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "<details") ||
		strings.Contains(lower, "<think>") ||
		strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<turn: tool_call") ||
		strings.Contains(lower, "<|functioncallbegin|>")
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
