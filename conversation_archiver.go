package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ConversationArchiver extracts key information from expiring conversations
// and stores them as long-term memories via MemoryStore.
type ConversationArchiver struct {
	memoryStore *MemoryStore
	app         *App
}

// NewConversationArchiver creates a ConversationArchiver that uses the given
// MemoryStore for persistence and the App to access LLM configuration.
func NewConversationArchiver(memoryStore *MemoryStore, app *App) *ConversationArchiver {
	return &ConversationArchiver{
		memoryStore: memoryStore,
		app:         app,
	}
}

// Archive analyses the conversation entries for a user and stores a summary
// as a long-term memory. It skips archiving when:
//   - The conversation is too short (< 4 entries) — simple Q&A not worth archiving.
//   - The Maclaw LLM is not configured.
//   - The LLM call fails (error is returned to the caller).
func (a *ConversationArchiver) Archive(userID string, entries []conversationEntry) error {
	// Skip trivial conversations.
	if len(entries) < 4 {
		return nil
	}

	// Check LLM configuration.
	if !a.app.isMaclawLLMConfigured() {
		return nil
	}

	llmCfg := a.app.GetMaclawLLMConfig()
	if strings.TrimSpace(llmCfg.URL) == "" || strings.TrimSpace(llmCfg.Model) == "" {
		return nil
	}

	// Build the conversation text for the LLM prompt.
	var convoBuilder strings.Builder
	for _, e := range entries {
		contentStr := formatEntryContent(e.Content)
		if contentStr == "" {
			continue
		}
		convoBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", e.Role, contentStr))
	}
	conversationText := convoBuilder.String()
	if strings.TrimSpace(conversationText) == "" {
		return nil
	}

	// Call the LLM to generate a summary.
	summary, err := a.callLLMForSummary(llmCfg, conversationText)
	if err != nil {
		return fmt.Errorf("conversation_archiver: llm call: %w", err)
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}

	// Store the summary as a MemoryEntry.
	now := time.Now()
	entry := MemoryEntry{
		Content:  summary,
		Category: MemCategoryConversationSummary,
		Tags: []string{
			"conversation_summary",
			userID,
			now.Format("2006-01-02"),
		},
	}
	return a.memoryStore.Save(entry)
}

// callLLMForSummary sends the conversation text to the configured LLM and
// asks it to extract user preferences, decisions, and important facts.
func (a *ConversationArchiver) callLLMForSummary(cfg MaclawLLMConfig, conversationText string) (string, error) {
	prompt := "请从以下对话中提取关键信息，包括：用户偏好、决策结论、重要事实。" +
		"请用简洁的中文列出要点，不要包含无关信息。如果对话中没有值得记录的信息，请回复「无」。\n\n" +
		"对话内容：\n" + conversationText

	messages := []interface{}{
		map[string]string{"role": "user", "content": prompt},
	}

	client := &http.Client{Timeout: 30 * time.Second}
	result, err := doSimpleLLMRequest(cfg, messages, client, 30*time.Second)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// formatEntryContent converts a conversationEntry's Content (which may be a
// string or a complex structure) into a plain string for the LLM prompt.
func formatEntryContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}
