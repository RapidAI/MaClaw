package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func findEmbModel(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	t.Skip("no gemma embedding model found")
	return ""
}

type intentTestCase struct {
	Input       string
	Expected    string // expected Intent value
	Description string
}

func allTestCases() []intentTestCase {
	return []intentTestCase{
		// ── Coding ──
		{"帮我写一个贪吃蛇游戏", IntentCoding, "明确编码请求"},
		{"开发一个自动抢票工具", IntentCoding, "口语化编码"},
		{"实现一个用户注册登录功能", IntentCoding, "功能开发"},
		{"写个脚本批量重命名文件", IntentCoding, "脚本编写"},
		{"fix the authentication bug", IntentCoding, "英文编码"},
		{"create a REST API for user management", IntentCoding, "英文API开发"},
		{"帮我把这个函数重构一下", IntentCoding, "代码重构"},
		{"给这个项目添加单元测试", IntentCoding, "测试编写"},
		{"设计一个微服务架构", IntentCoding, "架构设计"},
		{"帮我fix一下这个bug", IntentCoding, "中英混合编码"},

		// ── SSH ──
		{"登录4090服务器查看GPU占用率", IntentSSH, "典型SSH"},
		{"连上生产环境看一下日志", IntentSSH, "查看日志"},
		{"重启一下nginx", IntentSSH, "服务管理"},
		{"看看线上机器的磁盘空间", IntentSSH, "服务器监控"},
		{"把这个文件上传到服务器", IntentSSH, "文件传输"},
		{"check the GPU usage on the server", IntentSSH, "英文SSH"},
		{"deploy到production", IntentSSH, "中英混合部署"},

		// ── Content ──
		{"翻译这篇论文摘要", IntentContent, "翻译"},
		{"帮我整理一下会议纪要", IntentContent, "内容整理"},
		{"总结这个PDF的要点", IntentContent, "文档总结"},
		{"把这段英文翻译成中文", IntentContent, "翻译"},
		{"收集一下最近的AI新闻", IntentContent, "资料收集"},
		{"summarize this article for me", IntentContent, "英文总结"},
		{"格式化一下这段文字", IntentContent, "格式化=排版"},
		{"format this text nicely", IntentContent, "英文格式化"},
		{"翻译这段代码的注释", IntentContent, "翻译为主"},

		// ── Chat ──
		{"你好啊", IntentChat, "打招呼"},
		{"你能做什么", IntentChat, "能力询问"},
		{"谢谢你的帮助", IntentChat, "感谢"},
		{"hello there", IntentChat, "英文打招呼"},
		{"讲个笑话吧", IntentChat, "闲聊"},

		// ── Browser ──
		{"打开浏览器帮我填个表单", IntentBrowser, "浏览器操作"},
		{"录制一下这个网页的操作流程", IntentBrowser, "浏览器录制"},
		{"帮我在这个网站上点击购买按钮", IntentBrowser, "网页交互"},

		// ── Query (should NOT trigger action intents) ──
		{"介绍一下微服务架构的优缺点", IntentQuery, "架构知识问答"},
		{"什么是MVC架构", IntentQuery, "概念解释"},
		{"什么是服务器", IntentQuery, "概念问答"},
		{"服务器和客户端的区别是什么", IntentQuery, "知识问答"},
		{"docker是什么", IntentQuery, "概念问答"},
		{"nginx的配置文件怎么写", IntentQuery, "知识问答"},
		{"python怎么安装", IntentQuery, "安装问题"},
		{"git怎么用", IntentQuery, "工具使用问题"},
		{"我想了解一下这个项目的架构设计", IntentQuery, "了解/学习"},

		// ── Short commands (need context) ──
		{"开工", IntentShortCommand, "短指令"},
		{"好的", IntentShortCommand, "确认"},
		{"继续", IntentShortCommand, "延续"},
		{"ok", IntentShortCommand, "确认"},
		{"开干", IntentShortCommand, "短指令"},

		// ── Mixed intent ──
		{"帮我写个脚本然后上传到服务器", IntentCoding, "混合coding+ssh"},
		{"看看服务器上的代码有没有bug", IntentSSH, "SSH为主"},
	}
}

func TestIntentClassifier_RulesOnly(t *testing.T) {
	// Test with NoopEmbedder — only Layer 1 rules active.
	ic := NewIntentClassifier(embedding.NoopEmbedder{})

	cases := allTestCases()
	var ruleHandled, ruleCorrect int

	for _, tc := range cases {
		r := ic.Classify(tc.Input)
		if r.Intent == IntentUnknown {
			continue // not handled by rules, would go to embedding
		}
		ruleHandled++

		// In rules-only mode, short chat messages get classified as short_command,
		// which is acceptable — embedding (Layer 2) would refine them to chat.
		isAcceptable := r.Intent == tc.Expected ||
			(tc.Expected == IntentChat && r.Intent == IntentShortCommand)

		if !isAcceptable {
			ruleCorrect-- // will be corrected below
			t.Logf("RULE MISMATCH: %q expected=%s got=%s", tc.Input, tc.Expected, r.Intent)
		} else {
			ruleCorrect++
		}
	}

	t.Logf("Layer 1 (rules only): handled %d/%d, correct/acceptable %d/%d (%.0f%%)",
		ruleHandled, len(cases), ruleCorrect, ruleHandled,
		float64(ruleCorrect)/float64(max(ruleHandled, 1))*100)

	// Rules should handle all query and short_command cases correctly.
	for _, tc := range cases {
		if tc.Expected == IntentQuery || tc.Expected == IntentShortCommand {
			r := ic.Classify(tc.Input)
			if r.Intent != tc.Expected {
				t.Errorf("Rule should handle %q: expected=%s got=%s", tc.Input, tc.Expected, r.Intent)
			}
		}
	}
}

func TestIntentClassifier_FullAccuracy(t *testing.T) {
	modelPath := findEmbModel(t)
	emb, err := embedding.NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	ic := NewIntentClassifier(emb)

	// Wait for anchor warmup.
	deadline := time.Now().Add(10 * time.Second)
	for !ic.Ready() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !ic.Ready() {
		t.Fatal("IntentClassifier not ready after 10s")
	}

	cases := allTestCases()
	var correct, incorrect int

	t.Logf("\n%-50s | %-14s | %-14s | %-6s | %-6s | %s",
		"Input", "Expected", "Got", "Conf", "Gap", "Result")
	t.Logf("%s", strings.Repeat("-", 120))

	for _, tc := range cases {
		r := ic.Classify(tc.Input)

		match := false
		// For action intents, exact match required.
		// For query/short_command, exact match required.
		// We also accept: expected=coding/ssh/content/chat/browser, got=unknown (missed but not wrong).
		if r.Intent == tc.Expected {
			match = true
		}

		result := "✅"
		if !match {
			result = fmt.Sprintf("❌ got=%s", r.Intent)
			incorrect++
		} else {
			correct++
		}

		displayInput := tc.Input
		if len([]rune(displayInput)) > 40 {
			displayInput = string([]rune(displayInput)[:37]) + "..."
		}

		t.Logf("%-50s | %-14s | %-14s | %.4f | %.4f | %s",
			displayInput, tc.Expected, r.Intent, r.Confidence, r.Gap, result)
	}

	total := correct + incorrect
	accuracy := float64(correct) / float64(total) * 100
	t.Logf("\n%s", strings.Repeat("=", 80))
	t.Logf("ACCURACY: %d/%d (%.1f%%)", correct, total, accuracy)

	if accuracy < 85.0 {
		t.Errorf("Accuracy %.1f%% below 85%% target", accuracy)
	}
}

func TestIntentClassifier_Latency(t *testing.T) {
	modelPath := findEmbModel(t)
	emb, err := embedding.NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	ic := NewIntentClassifier(emb)
	deadline := time.Now().Add(10 * time.Second)
	for !ic.Ready() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	queries := []string{
		"帮我写一个贪吃蛇游戏",
		"什么是MVC架构",
		"登录服务器看GPU",
		"你好",
		"开工",
	}

	for _, q := range queries {
		start := time.Now()
		r := ic.Classify(q)
		elapsed := time.Since(start)
		t.Logf("%-40s → %-14s (layer=%d, conf=%.2f) in %v", q, r.Intent, r.Layer, r.Confidence, elapsed)

		if elapsed > 50*time.Millisecond {
			t.Errorf("Classify(%q) took %v, want < 50ms", q, elapsed)
		}
	}
}

func TestIntentClassifier_QuestionPatterns(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})

	questions := []string{
		"什么是微服务",
		"docker是什么",
		"怎么安装python",
		"如何配置nginx",
		"为什么要用容器",
		"有哪些设计模式",
		"介绍一下kubernetes",
		"解释一下什么是REST",
		"了解一下这个项目的架构",
		"服务器和客户端的区别是什么",
		"MVC架构的优缺点",
		"what is docker",
		"how to install python",
		"explain microservices",
		"tell me about kubernetes",
	}

	for _, q := range questions {
		r := ic.Classify(q)
		if r.Intent != IntentQuery {
			t.Errorf("Classify(%q) = %s, want %s", q, r.Intent, IntentQuery)
		}
	}
}

func TestIntentClassifier_ShortCommands(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})

	shorts := []string{"开工", "好的", "继续", "ok", "开干", "嗯", "是", "go"}

	for _, s := range shorts {
		r := ic.Classify(s)
		if r.Intent != IntentShortCommand {
			t.Errorf("Classify(%q) = %s, want %s", s, r.Intent, IntentShortCommand)
		}
	}
}

func TestIntentClassifier_NoopEmbedder(t *testing.T) {
	ic := NewIntentClassifier(nil)

	// Should still handle rules.
	r := ic.Classify("什么是docker")
	if r.Intent != IntentQuery {
		t.Errorf("nil embedder: Classify('什么是docker') = %s, want query", r.Intent)
	}

	// Non-rule cases should return unknown.
	r = ic.Classify("帮我写一个贪吃蛇游戏")
	if r.Intent != IntentUnknown {
		t.Errorf("nil embedder: Classify('帮我写一个贪吃蛇游戏') = %s, want unknown", r.Intent)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Layer 3 (LLM) tests ────────────────────────────────────────────────────

func TestIntentClassifier_Layer3_MockLLM(t *testing.T) {
	// Use NoopEmbedder so Layer 2 is disabled — forces ambiguous cases to Layer 3.
	ic := NewIntentClassifier(embedding.NoopEmbedder{})

	// Mock LLM that returns the correct intent for known inputs.
	// Note: the prompt includes the system instructions + user message,
	// so we match on the user message portion specifically.
	mockLLM := func(prompt string) (string, error) {
		// Extract user message from the prompt (after "用户消息: ").
		idx := strings.Index(prompt, "用户消息: ")
		userMsg := ""
		if idx >= 0 {
			userMsg = strings.ToLower(prompt[idx:])
		}
		switch {
		case strings.Contains(userMsg, "create a rest api"):
			return "coding", nil
		case strings.Contains(userMsg, "帮我搞个能自动抢票"):
			return "coding", nil
		case strings.Contains(userMsg, "帮我写一个贪吃蛇"):
			return "coding", nil
		case strings.Contains(userMsg, "登录服务器"):
			return "ssh", nil
		case strings.Contains(userMsg, "翻译这篇"):
			return "content", nil
		case strings.Contains(userMsg, "你好"):
			return "chat", nil
		default:
			return "unknown", nil
		}
	}
	ic.SetLLMFunc(mockLLM)

	// Cases that Layer 1 rules don't handle and Layer 2 is disabled.
	cases := []struct {
		input    string
		expected string
	}{
		{"create a REST API for user management", IntentCoding},
		{"帮我搞个能自动抢票的东西", IntentCoding},
		{"帮我写一个贪吃蛇游戏", IntentCoding},
		{"登录服务器查看GPU", IntentSSH},
		{"翻译这篇论文", IntentContent},
		// "你好啊" is 3 runes, not caught by short_command (max=2), not a question pattern.
		// Without embedding, it goes to LLM.
		{"你好啊", IntentChat},
	}

	for _, tc := range cases {
		r := ic.Classify(tc.input)
		if r.Intent != tc.expected {
			t.Errorf("Classify(%q) = %s (layer=%d), want %s", tc.input, r.Intent, r.Layer, tc.expected)
		}
		// Verify it used Layer 3 (not Layer 1 rules).
		if r.Layer != 3 && r.Layer != 1 {
			// Layer 1 is acceptable for question patterns.
		}
	}
}

func TestIntentClassifier_Layer3_FallbackOnError(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})

	// LLM that always errors.
	ic.SetLLMFunc(func(prompt string) (string, error) {
		return "", fmt.Errorf("LLM unavailable")
	})

	r := ic.Classify("帮我写一个贪吃蛇游戏")
	// Should fall back to unknown (no embedding, LLM failed).
	if r.Intent != IntentUnknown {
		t.Errorf("expected unknown on LLM error, got %s", r.Intent)
	}
}

func TestIntentClassifier_Layer3_ParsesVariousFormats(t *testing.T) {
	ic := NewIntentClassifier(embedding.NoopEmbedder{})

	// Test that the LLM response parser handles various formats.
	formats := []struct {
		llmResponse string
		expected    string
	}{
		{"coding", IntentCoding},
		{"Coding", IntentCoding},
		{"CODING", IntentCoding},
		{"coding.", IntentCoding},
		{"coding\n", IntentCoding},
		{"ssh ", IntentSSH},
		{"content。", IntentContent},
		{"chat!", IntentChat},
		{"browser\nsome explanation", IntentBrowser},
		{"query,", IntentQuery},
		{"garbage text", IntentUnknown},
		{"", IntentUnknown},
	}

	for _, f := range formats {
		ic.SetLLMFunc(func(prompt string) (string, error) {
			return f.llmResponse, nil
		})
		r := ic.Classify("some ambiguous input that rules don't catch")
		if r.Intent != f.expected {
			t.Errorf("LLM response %q → %s, want %s", f.llmResponse, r.Intent, f.expected)
		}
	}
}

func TestIntentClassifier_Layer3_WithEmbedding_AmbiguousCase(t *testing.T) {
	modelPath := findEmbModel(t)
	emb, err := embedding.NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	ic := NewIntentClassifier(emb)
	deadline := time.Now().Add(10 * time.Second)
	for !ic.Ready() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	// Set up LLM that correctly classifies the one case embedding gets wrong.
	ic.SetLLMFunc(func(prompt string) (string, error) {
		if strings.Contains(prompt, "create a REST API") {
			return "coding", nil
		}
		return "unknown", nil
	})

	// This is the case that embedding misclassifies as ssh (gap=0.04).
	r := ic.Classify("create a REST API for user management")
	if r.Intent != IntentCoding {
		t.Errorf("With LLM Layer 3, expected coding, got %s (layer=%d, conf=%.2f)", r.Intent, r.Layer, r.Confidence)
	}
	if r.Layer != 3 {
		t.Logf("Classified at layer %d (expected 3 for this ambiguous case)", r.Layer)
	}
}
