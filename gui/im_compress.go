package main

// /compress command handler for the GUI agent loop.
//
// This module bridges the corelib/context.Compressor with the GUI's conversation
// memory system, providing the handleCompressCommandWithLang function for the
// manual /compress slash command.

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
	"github.com/RapidAI/CodeClaw/corelib/llm"

	ctxcompress "github.com/RapidAI/CodeClaw/corelib/context"
)

func (h *IMMessageHandler) handleCompressCommandWithLang(userID, lang string) *IMAgentResponse {
	history := h.memory.Load(userID)
	if len(history) == 0 {
		return &IMAgentResponse{Text: localizedIMCompressNoHistoryMessage(lang)}
	}

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client

	// Convert conversation entries to context.Message format.
	messages := conversationToContextMessages(history)
	if len(messages) == 0 {
		return &IMAgentResponse{Text: localizedIMCompressNoHistoryMessage(lang)}
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
		return &IMAgentResponse{Text: localizedIMCompressFailedMessage(lang, err)}
	}

	// Convert compressed messages back to conversation entries.
	compressed := contextMessagesToConversation(result.Messages)
	h.memory.Save(userID, compressed)

	return &IMAgentResponse{
		Text: localizedIMCompressSuccessMessage(lang, result.MarkerText),
	}
}

func localizedIMCompressNoHistoryMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "There is no conversation history to compress."
	case appLanguageZhHant:
		return "目前沒有可壓縮的對話歷史。"
	default:
		return "当前没有可压缩的对话历史。"
	}
}

func localizedIMCompressFailedMessage(lang string, err error) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Compression failed: %v", err)
	case appLanguageZhHant:
		return fmt.Sprintf("壓縮失敗：%v", err)
	default:
		return fmt.Sprintf("压缩失败：%v", err)
	}
}

func localizedIMCompressSuccessMessage(lang, marker string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Conversation history compressed. %s", marker)
	case appLanguageZhHant:
		return fmt.Sprintf("對話歷史已壓縮。%s", marker)
	default:
		return fmt.Sprintf("对话历史已压缩。%s", marker)
	}
}

// makeSummarizeCallback creates an LLM summarization callback for the compressor.
func makeSummarizeCallback(cfg corelib.MaclawLLMConfig, httpClient *http.Client) func(string) (string, error) {
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

		ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "conversation-compress"})
		resp, err := doSimpleLLMRequest(ctx, attachLightweightHubHint(cfg, llm.TaskSummary), messages, httpClient, 30*time.Second)
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

// conversationToContextMessages converts []agent.ConversationEntry to []context.Message.
// Only text content is preserved; tool calls and multimodal content are
// serialized to their string representation.
func conversationToContextMessages(entries []agent.ConversationEntry) []ctxcompress.Message {
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

// contextMessagesToConversation converts []context.Message back to []agent.ConversationEntry.
func contextMessagesToConversation(messages []ctxcompress.Message) []agent.ConversationEntry {
	entries := make([]agent.ConversationEntry, 0, len(messages))
	for _, m := range messages {
		entries = append(entries, agent.ConversationEntry{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return entries
}

// entryContentToString extracts a string representation from a agent.ConversationEntry's Content field.
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
