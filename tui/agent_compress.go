package main

// /compress command handler and auto-compression integration for the TUI agent loop.
//
// This module bridges the corelib/context.Compressor with the TUI's conversation
// system. It provides:
//   - handleCompressCommand: manual /compress slash command
//   - autoCompressConversationTUI: auto-compression check before each LLM call
//   - persistSessionTranscriptTUI: persist session transcript to FTS5 store

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	ctxcompress "github.com/RapidAI/CodeClaw/corelib/context"
	"github.com/RapidAI/CodeClaw/corelib/session"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// handleCompressCommand handles the /compress slash command in TUI.
// It compresses the provided conversation history using the corelib/context.Compressor
// and returns the compressed conversation along with a status message.
func (h *TUIAgentHandler) handleCompressCommand(history []map[string]string) ([]map[string]string, string) {
	if len(history) == 0 {
		return history, "当前没有对话历史可压缩。"
	}

	cfg, err := commands.LoadLLMConfig()
	if err != nil {
		return history, fmt.Sprintf("加载 LLM 配置失败: %v", err)
	}

	// Convert history to context.Message format.
	messages := historyToContextMessages(history)
	if len(messages) == 0 {
		return history, "当前没有对话历史可压缩。"
	}

	// Create compressor with LLM summarization callback.
	compressor := ctxcompress.NewCompressor(ctxcompress.CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: cfg.EffectiveContextTokens(),
	}, makeTUISummarizeCallback(cfg, h.httpClient))

	// Force compress regardless of threshold (manual trigger).
	result, err := compressor.Compress(messages)
	if err != nil {
		return history, fmt.Sprintf("压缩失败: %v", err)
	}

	// Convert compressed messages back to history format.
	compressed := contextMessagesToHistory(result.Messages)
	return compressed, fmt.Sprintf("✅ 对话历史已压缩。%s", result.MarkerText)
}

// autoCompressConversationTUI checks if the conversation should be auto-compressed
// before an LLM call. If compression is needed, it compresses the conversation
// in-place and returns the compressed version. Otherwise returns the original.
func (h *TUIAgentHandler) autoCompressConversationTUI(conversation []interface{}) []interface{} {
	if len(conversation) <= 5 {
		return conversation
	}

	cfg, err := commands.LoadLLMConfig()
	if err != nil {
		return conversation
	}

	// Convert to context.Message for the compressor.
	messages := interfaceSliceToCtxMessages(conversation)
	if len(messages) == 0 {
		return conversation
	}

	compressor := ctxcompress.NewCompressor(ctxcompress.CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: cfg.EffectiveContextTokens(),
	}, makeTUISummarizeCallback(cfg, h.httpClient))

	if !compressor.ShouldCompress(messages) {
		return conversation
	}

	result, err := compressor.Compress(messages)
	if err != nil {
		log.Printf("[auto-compress] compression failed, skipping: %v", err)
		return conversation
	}

	log.Printf("[auto-compress] %s", result.MarkerText)

	// Convert back to []interface{} for the agent loop.
	return ctxMessagesToInterfaceSlice(result.Messages)
}

// persistSessionTranscriptTUI converts the conversation history to a
// session.TranscriptEntry slice, serializes it, extracts a topic, and
// persists the document to the FTS5 session search store. Runs in a
// goroutine to avoid blocking the agent loop. Errors are logged but
// do not fail the main flow.
func (h *TUIAgentHandler) persistSessionTranscriptTUI(history []map[string]string) {
	if len(history) == 0 {
		return
	}
	store := h.getSessionStore()
	if store == nil {
		return
	}

	// Copy history to avoid data races with the caller.
	historyCopy := make([]map[string]string, len(history))
	for i, m := range history {
		copied := make(map[string]string, len(m))
		for k, v := range m {
			copied[k] = v
		}
		historyCopy[i] = copied
	}

	go func() {
		entries := historyToTranscriptEntries(historyCopy)
		if len(entries) == 0 {
			return
		}

		fullText := session.Serialize(entries)
		if strings.TrimSpace(fullText) == "" {
			return
		}

		topic := session.ExtractTopic(fullText)

		// Derive session ID from timestamp.
		sessionID := fmt.Sprintf("tui_%d", time.Now().UnixNano())

		doc := session.SessionDocument{
			SessionID: sessionID,
			Timestamp: time.Now(),
			Platform:  "tui",
			Topic:     topic,
			FullText:  fullText,
		}

		if err := store.Persist(doc); err != nil {
			log.Printf("[session_search] persist failed: %v", err)
		}
	}()
}

// historyToTranscriptEntries converts TUI conversation history ([]map[string]string)
// to the corelib session.TranscriptEntry format for serialization and FTS5 indexing.
func historyToTranscriptEntries(history []map[string]string) []session.TranscriptEntry {
	var entries []session.TranscriptEntry
	for _, msg := range history {
		role := msg["role"]
		content := msg["content"]
		if role == "" || (content == "" && role != "assistant") {
			continue
		}
		entries = append(entries, session.TranscriptEntry{
			Role:    role,
			Content: content,
		})
	}
	return entries
}

// ---------------------------------------------------------------------------
// Conversion helpers between conversation formats
// ---------------------------------------------------------------------------

// historyToContextMessages converts []map[string]string to []context.Message.
func historyToContextMessages(history []map[string]string) []ctxcompress.Message {
	messages := make([]ctxcompress.Message, 0, len(history))
	for _, msg := range history {
		role := msg["role"]
		content := msg["content"]
		if role == "" {
			continue
		}
		messages = append(messages, ctxcompress.Message{
			Role:    role,
			Content: content,
		})
	}
	return messages
}

// contextMessagesToHistory converts []context.Message back to []map[string]string.
func contextMessagesToHistory(messages []ctxcompress.Message) []map[string]string {
	history := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		history = append(history, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	return history
}

// interfaceSliceToCtxMessages converts []interface{} (agent loop format)
// to []context.Message for the compressor.
func interfaceSliceToCtxMessages(msgs []interface{}) []ctxcompress.Message {
	messages := make([]ctxcompress.Message, 0, len(msgs))
	for _, m := range msgs {
		role, content := extractRoleContentTUI(m)
		if role == "" {
			continue
		}
		messages = append(messages, ctxcompress.Message{
			Role:    role,
			Content: content,
		})
	}
	return messages
}

// ctxMessagesToInterfaceSlice converts []context.Message to []interface{}
// for the agent loop conversation format.
func ctxMessagesToInterfaceSlice(messages []ctxcompress.Message) []interface{} {
	result := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		result = append(result, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	return result
}

// extractRoleContentTUI extracts role and content from an interface{} message.
func extractRoleContentTUI(m interface{}) (string, string) {
	switch v := m.(type) {
	case map[string]interface{}:
		role, _ := v["role"].(string)
		switch c := v["content"].(type) {
		case string:
			return role, c
		case nil:
			return role, ""
		default:
			return role, fmt.Sprintf("%v", c)
		}
	case map[string]string:
		return v["role"], v["content"]
	default:
		return "", ""
	}
}

// makeTUISummarizeCallback creates an LLM summarization callback for the compressor.
func makeTUISummarizeCallback(cfg corelib.MaclawLLMConfig, httpClient *http.Client) func(string) (string, error) {
	return func(text string) (string, error) {
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return "", fmt.Errorf("LLM not configured")
		}

		// Truncate input to avoid exceeding context limits for the summarization call.
		runes := []rune(text)
		if len(runes) > 16000 {
			text = string(runes[:16000]) + "\n...(truncated)"
		}

		messages := []interface{}{
			map[string]string{
				"role":    "system",
				"content": "你是一个对话摘要助手。请将以下对话历史压缩为简洁的摘要，保留关键决策、工具调用结果、文件路径和代码片段。去除冗余的思考过程和重复的失败尝试。",
			},
			map[string]string{
				"role":    "user",
				"content": "请压缩以下对话历史:\n\n" + text,
			},
		}

		resp, err := agent.DoSimpleLLMRequest(cfg, messages, httpClient, 30*time.Second)
		if err != nil {
			return "", err
		}
		if resp == nil || strings.TrimSpace(resp.Content) == "" {
			return "", fmt.Errorf("empty summarization response")
		}
		return resp.Content, nil
	}
}
