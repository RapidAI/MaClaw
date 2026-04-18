package main

// /compress command handler and auto-compression integration for the GUI agent loop.
//
// This module bridges the corelib/context.Compressor with the GUI's conversation
// memory system. It provides:
//   - handleCompressCommand: manual /compress slash command
//   - autoCompressConversation: auto-compression check before each LLM call

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	ctxcompress "github.com/RapidAI/CodeClaw/corelib/context"
)

// handleCompressCommand handles the /compress slash command.
// It loads the current conversation history, compresses it using the
// corelib/context.Compressor, and saves the compressed history back.
func (h *IMMessageHandler) handleCompressCommand(userID string) *IMAgentResponse {
	history := h.memory.load(userID)
	if len(history) == 0 {
		return &IMAgentResponse{Text: "当前没有对话历史可压缩。"}
	}

	cfg := h.app.GetMaclawLLMConfig()
	httpClient := h.client

	// Convert conversation entries to context.Message format.
	messages := conversationToContextMessages(history)
	if len(messages) == 0 {
		return &IMAgentResponse{Text: "当前没有对话历史可压缩。"}
	}

	// Create compressor with LLM summarization callback.
	compressor := ctxcompress.NewCompressor(ctxcompress.CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: cfg.EffectiveContextTokens(),
	}, makeSummarizeCallback(cfg, httpClient))

	// Force compress regardless of threshold (manual trigger).
	result, err := compressor.Compress(messages)
	if err != nil {
		log.Printf("[/compress] compression failed: %v", err)
		return &IMAgentResponse{Text: fmt.Sprintf("压缩失败: %v", err)}
	}

	// Convert compressed messages back to conversation entries.
	compressed := contextMessagesToConversation(result.Messages)
	h.memory.save(userID, compressed)

	return &IMAgentResponse{
		Text: fmt.Sprintf("✅ 对话历史已压缩。%s", result.MarkerText),
	}
}

// autoCompressConversation checks if the conversation should be auto-compressed
// before an LLM call. If compression is needed, it compresses the conversation
// in-place and returns the compressed version. Otherwise returns the original.
//
// This is called alongside trimConversation in the agent loop. The corelib
// compressor provides intelligent summarization-based compression, while
// trimConversation handles the final token budget enforcement.
func autoCompressConversation(
	conversation []interface{},
	cfg MaclawLLMConfig,
	httpClient *http.Client,
) []interface{} {
	if len(conversation) <= 5 {
		return conversation
	}

	// Convert to context.Message for the compressor.
	messages := interfaceSliceToContextMessages(conversation)
	if len(messages) == 0 {
		return conversation
	}

	compressor := ctxcompress.NewCompressor(ctxcompress.CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: cfg.EffectiveContextTokens(),
	}, makeSummarizeCallback(cfg, httpClient))

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
	return contextMessagesToInterfaceSlice(result.Messages)
}

// makeSummarizeCallback creates an LLM summarization callback for the compressor.
func makeSummarizeCallback(cfg MaclawLLMConfig, httpClient *http.Client) func(string) (string, error) {
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

		resp, err := doSimpleLLMRequest(context.Background(), cfg, messages, httpClient, 30*time.Second)
		if err != nil {
			return "", err
		}
		if resp == nil || strings.TrimSpace(resp.Content) == "" {
			return "", fmt.Errorf("empty summarization response")
		}
		return resp.Content, nil
	}
}

// ---------------------------------------------------------------------------
// Conversion helpers between conversation formats
// ---------------------------------------------------------------------------

// conversationToContextMessages converts []conversationEntry to []context.Message.
// Only text content is preserved; tool calls and multimodal content are
// serialized to their string representation.
func conversationToContextMessages(entries []conversationEntry) []ctxcompress.Message {
	messages := make([]ctxcompress.Message, 0, len(entries))
	for _, e := range entries {
		content := entryContentToString(e.Content)
		if content == "" && e.ToolCalls == nil {
			continue
		}
		messages = append(messages, ctxcompress.Message{
			Role:    e.Role,
			Content: content,
		})
	}
	return messages
}

// interfaceSliceToContextMessages converts []interface{} (agent loop format)
// to []context.Message for the compressor.
func interfaceSliceToContextMessages(msgs []interface{}) []ctxcompress.Message {
	messages := make([]ctxcompress.Message, 0, len(msgs))
	for _, m := range msgs {
		role, content := extractRoleContent(m)
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

// contextMessagesToConversation converts []context.Message back to []conversationEntry.
func contextMessagesToConversation(messages []ctxcompress.Message) []conversationEntry {
	entries := make([]conversationEntry, 0, len(messages))
	for _, m := range messages {
		entries = append(entries, conversationEntry{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return entries
}

// contextMessagesToInterfaceSlice converts []context.Message to []interface{}
// for the agent loop conversation format.
func contextMessagesToInterfaceSlice(messages []ctxcompress.Message) []interface{} {
	result := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		result = append(result, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	return result
}

// entryContentToString extracts a string representation from a conversationEntry's Content field.
func entryContentToString(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		// For multimodal content (arrays, maps), serialize to a compact string.
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}

// extractRoleContent extracts role and content from an interface{} message.
func extractRoleContent(m interface{}) (string, string) {
	switch v := m.(type) {
	case map[string]interface{}:
		role, _ := v["role"].(string)
		switch c := v["content"].(type) {
		case string:
			return role, c
		case nil:
			return role, ""
		default:
			data, _ := json.Marshal(c)
			return role, string(data)
		}
	case map[string]string:
		return v["role"], v["content"]
	default:
		return "", ""
	}
}
