package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ConversationArchiver extracts key information from expiring conversations
// and stores them as long-term memories via MemoryStore.
type ConversationArchiver struct {
	memoryStore        *memory.Store
	app                *App
	knowledgeExtractor *memory.KnowledgeExtractor
	slotScopeResolver  func(userID string) *agent.UnfinishedTaskSlot
}

// NewConversationArchiver creates a ConversationArchiver that uses the given
// MemoryStore for persistence and the App to access LLM configuration.
func NewConversationArchiver(memoryStore *memory.Store, app *App) *ConversationArchiver {
	ca := &ConversationArchiver{
		memoryStore: memoryStore,
		app:         app,
	}
	// Reuse the corelib/memory maintenance topology for fallback extraction so
	// TiMem consolidation stays shared with online memory maintenance.
	if app == nil {
		return ca
	}
	llmAdapter := &archiverLLMCaller{app: app}
	if app.memoryMaintenance != nil {
		app.memoryMaintenance.SetLLM(llmAdapter)
		ca.knowledgeExtractor = app.memoryMaintenance.KnowledgeExtractor()
	} else {
		maintenance := memory.NewMaintenance(memoryStore, llmAdapter, nil)
		ca.knowledgeExtractor = maintenance.KnowledgeExtractor()
	}
	return ca
}

func (a *ConversationArchiver) SetSlotScopeResolver(fn func(userID string) *agent.UnfinishedTaskSlot) {
	a.slotScopeResolver = fn
}

// Archive analyses the conversation entries for a user and stores a summary
// as a long-term memory. It skips archiving when:
//   - The conversation is too short (< 4 entries), so simple Q&A is not worth archiving.
//   - The Maclaw LLM is not configured.
//   - The OnlineExtractor has been active (summary would be filtered out anyway).
//
// LLM failures during summary generation are logged but do not fail the call,
// allowing session eviction to proceed regardless of transient LLM issues.
func (a *ConversationArchiver) Archive(userID string, entries []agent.ConversationEntry) error {
	if a == nil || a.app == nil || a.memoryStore == nil {
		return nil
	}
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

	// Mutual exclusion with OnlineExtractor: if the online pipeline has been
	// actively extracting during this session, skip summary generation.
	// The conversation_summary category is filtered out by RecallDynamic anyway,
	// so generating it when OnlineExtractor is active wastes LLM calls and capacity.
	skipSummary := false
	if a.memoryStore != nil {
		if oe := a.memoryStore.OnlineExtractor(); oe != nil && oe.HasRecentActivity(60*time.Minute) {
			skipSummary = true
		}
	}

	if !skipSummary {
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

		if strings.TrimSpace(conversationText) != "" {
			// Call the LLM to generate a summary.
			summary, err := a.callLLMForSummary(llmCfg, conversationText)
			if err != nil {
				log.Printf("[conversation_archiver] summary generation error (non-fatal): %v", err)
			} else {
				summary = strings.TrimSpace(summary)
				if !memory.IsEmptyConversationSummary(summary) {
					// Store the summary as a MemoryEntry.
					now := time.Now()
					tags := []string{
						"conversation_summary",
						userID,
						now.Format("2006-01-02"),
					}
					if a.slotScopeResolver != nil {
						if slot := a.slotScopeResolver(userID); slot != nil {
							tags = append(tags,
								"scope:unfinished_slot",
								"slot:"+slot.SlotID,
								"project:"+slot.ProjectPath,
							)
						} else {
							tags = append(tags, "scope:main_conversation")
						}
					}
					identityTagCount := 3
					if a.slotScopeResolver != nil {
						identityTagCount = len(tags)
					}
					_, err := a.memoryStore.UpsertConversationSummary(memory.ConversationSummaryUpsertOptions{
						Title:            "Conversation summary",
						Content:          summary,
						Tags:             tags,
						IdentityTagCount: identityTagCount,
						OwnerID:          userID,
					})
					if err != nil {
						return err
					}
				}
			}
		}
	}

	// Post-session knowledge extraction: convert entries to ConversationMessages
	// and run the KnowledgeExtractor. Errors are logged but do not fail Archive.
	// Note: KnowledgeExtractor has its own mutual exclusion with OnlineExtractor.
	if a.knowledgeExtractor != nil {
		var msgs []memory.ConversationMessage
		for _, e := range entries {
			contentStr := formatEntryContent(e.Content)
			if contentStr == "" {
				continue
			}
			msgs = append(msgs, memory.ConversationMessage{
				Role:    e.Role,
				Content: contentStr,
			})
		}
		if err := a.knowledgeExtractor.Extract(userID, msgs); err != nil {
			log.Printf("[conversation_archiver] knowledge extraction error: %v", err)
		}
	}

	return nil
}

// callLLMForSummary sends the conversation text to the configured LLM and
// asks it to extract user preferences, decisions, and important facts.
func (a *ConversationArchiver) callLLMForSummary(cfg corelib.MaclawLLMConfig, conversationText string) (string, error) {
	prompt := "Extract key information from the following conversation, including user preferences, decisions, important facts, and task progress. Use concise Chinese bullet points. If there is nothing worth remembering, reply with only: NONE.\n\nConversation:\n" + conversationText

	messages := []interface{}{
		map[string]string{"role": "user", "content": prompt},
	}

	client := &http.Client{Timeout: 30 * time.Second}
	result, err := doSimpleLLMRequest(context.Background(), cfg, messages, client, 30*time.Second)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// formatEntryContent converts a agent.ConversationEntry's Content (which may be a
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

// archiverLLMCaller adapts the GUI's LLM calling to memory.LLMChatCaller.
type archiverLLMCaller struct {
	app *App
}

func (c *archiverLLMCaller) ChatCall(messages []map[string]string) (string, error) {
	if c == nil || c.app == nil {
		return "", fmt.Errorf("archiver LLM caller is not configured")
	}
	cfg := c.app.GetMaclawLLMConfig()
	// Convert []map[string]string to []interface{} for doSimpleLLMRequest.
	ifaces := make([]interface{}, len(messages))
	for i, m := range messages {
		ifaces[i] = m
	}
	client := &http.Client{Timeout: 60 * time.Second}
	result, err := doSimpleLLMRequest(context.Background(), cfg, ifaces, client, 60*time.Second)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (c *archiverLLMCaller) IsConfigured() bool {
	return c != nil && c.app != nil && c.app.isMaclawLLMConfigured()
}
