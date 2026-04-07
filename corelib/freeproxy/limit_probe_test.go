package freeproxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProbeQuestionLengthLimit sends increasingly long questions to find
// the maximum accepted length by the 当贝 API.
func TestProbeQuestionLengthLimit(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(configDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	// Test with realistic system prompt content (JSON schemas, special chars, mixed lang)
	lengths := []int{8000, 9000, 9500, 10000, 10500, 11000, 11500, 12000}

	for _, length := range lengths {
		convID, err := client.CreateSession(ctx)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		// Build a realistic prompt with JSON schemas, tool definitions, mixed content
		var sb strings.Builder
		sb.WriteString("[System] 你是智能助手。需要执行操作时，使用以下格式调用工具：\n```tool_call\n{\"name\":\"工具名\",\"arguments\":{\"参数\":\"值\"}}\n```\n\n")
		sb.WriteString("可用工具：\n- read_file: 读取文件\n  参数: {\"properties\":{\"path\":{\"description\":\"文件路径\",\"type\":\"string\"}},\"required\":[\"path\"],\"type\":\"object\"}\n")
		sb.WriteString("- bash: 执行命令\n  参数: {\"properties\":{\"command\":{\"description\":\"shell 命令\",\"type\":\"string\"},\"timeout\":{\"type\":\"integer\"}},\"required\":[\"command\"],\"type\":\"object\"}\n")
		// Pad with realistic mixed content
		unit := "## 工作流规则\n- 编程任务需要确认\n- 使用 create_session 启动工具\n- 参数: {\"type\":\"object\",\"properties\":{\"tool\":{\"type\":\"string\"}}}\n- URL: http://127.0.0.1:18099/v1\n- 特殊字符: &amp; <tag> \"quoted\" 'single'\n"
		for sb.Len() < length {
			sb.WriteString(unit)
		}
		sb.WriteString("\nhello\n")

		question := sb.String()
		if len(question) > length+500 {
			question = string([]rune(question)[:length])
		}

		cr := CompletionRequest{
			ConversationID: convID,
			Prompt:         question,
		}

		t.Logf("Testing realistic content length=%d bytes...", len(question))
		fullText, _, err := client.StreamCompletion(ctx, cr, nil)

		// Clean up
		client.DeleteSession(context.Background(), convID)

		if err != nil {
			t.Logf("  ❌ FAILED at %d chars: %v", length, err)
			t.Logf("  >>> Limit is somewhere below %d chars", length)
			break
		}
		t.Logf("  ✅ OK at %d chars, response: %.50s", length, fullText)

		// Small delay to avoid rate limiting
		time.Sleep(2 * time.Second)
	}
}
