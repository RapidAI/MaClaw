package intent

import (
	"testing"
)

func TestClassifyByKeywords_EmptyMessage(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, confident := classifyByKeywords(registry, affinity, MessageContext{Text: ""})
	if !confident {
		t.Error("expected confident=true for empty message")
	}
	if result.Primary != LabelUnknown {
		t.Errorf("expected LabelUnknown, got %s", result.Primary)
	}
	if result.Confidence != 0 {
		t.Errorf("expected confidence 0, got %f", result.Confidence)
	}
	if result.Layer != 1 {
		t.Errorf("expected layer 1, got %d", result.Layer)
	}
	if result.Reason != "empty message" {
		t.Errorf("expected reason 'empty message', got %q", result.Reason)
	}
}

func TestClassifyByKeywords_StrongSSH(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, confident := classifyByKeywords(registry, affinity, MessageContext{Text: "登录服务器查看日志"})
	if !confident {
		t.Error("expected confident=true for strong SSH keywords")
	}
	if result.Primary != LabelSSH {
		t.Errorf("expected LabelSSH, got %s", result.Primary)
	}
	if result.Confidence < 0.90 {
		t.Errorf("expected confidence >= 0.90, got %f", result.Confidence)
	}
	if result.Layer != 1 {
		t.Errorf("expected layer 1, got %d", result.Layer)
	}
}

func TestClassifyByKeywords_StrongBrowser(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, confident := classifyByKeywords(registry, affinity, MessageContext{Text: "打开浏览器帮我截图"})
	if !confident {
		t.Error("expected confident=true for strong browser keyword")
	}
	if result.Primary != LabelBrowser {
		t.Errorf("expected LabelBrowser, got %s", result.Primary)
	}
	if result.Confidence != 0.92 {
		t.Errorf("expected confidence 0.92, got %f", result.Confidence)
	}
}

func TestClassifyByKeywords_WeakBrowserCombo(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	// "页面" + "点击" are both weak browser keywords
	result, confident := classifyByKeywords(registry, affinity, MessageContext{Text: "在页面上点击按钮"})
	if confident {
		t.Error("expected confident=false for weak browser combo")
	}
	if result.Primary != LabelBrowser {
		t.Errorf("expected LabelBrowser, got %s", result.Primary)
	}
	if result.Confidence != 0.55 {
		t.Errorf("expected confidence 0.55, got %f", result.Confidence)
	}
}

func TestClassifyByKeywords_CodingStrong(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, confident := classifyByKeywords(registry, affinity, MessageContext{Text: "帮我写代码实现一个功能"})
	if !confident {
		t.Error("expected confident=true for strong coding keywords")
	}
	if result.Primary != LabelCoding {
		t.Errorf("expected LabelCoding, got %s", result.Primary)
	}
	if result.Confidence < 0.90 {
		t.Errorf("expected confidence >= 0.90, got %f", result.Confidence)
	}
}

func TestClassifyByKeywords_DominanceCreationOverBugFix(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	// "开发" (coding strong) + "修复" (bugfix strong) → coding dominates
	result, _ := classifyByKeywords(registry, affinity, MessageContext{Text: "开发一个bug追踪系统修复流程"})
	if result.Primary != LabelCoding {
		t.Errorf("expected LabelCoding (creation dominates bug-fix), got %s", result.Primary)
	}
}

func TestClassifyByKeywords_NonCodingDominatesCodingWeak(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	// "翻译" (non_coding strong) + "代码" (coding weak) → non_coding dominates
	result, _ := classifyByKeywords(registry, affinity, MessageContext{Text: "翻译这段代码的注释"})
	if result.Primary != LabelNonCoding {
		t.Errorf("expected LabelNonCoding (non-coding dominates coding weak), got %s", result.Primary)
	}
}

func TestClassifyByKeywords_ContinuationWithContext(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	msg := MessageContext{
		Text:          "开工",
		RecentHistory: []string{"帮我开发一个贪吃蛇游戏"},
	}
	result, confident := classifyByKeywords(registry, affinity, msg)
	if result.Primary != LabelContinuation {
		t.Errorf("expected LabelContinuation, got %s", result.Primary)
	}
	if !confident {
		t.Error("expected confident=true for continuation with coding context")
	}
	if result.Confidence < 0.90 {
		t.Errorf("expected confidence >= 0.90, got %f", result.Confidence)
	}
}

func TestClassifyByKeywords_NoMatches(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, confident := classifyByKeywords(registry, affinity, MessageContext{Text: "你好吗朋友们"})
	if confident {
		t.Error("expected confident=false for no keyword matches")
	}
	if result.Primary != LabelUnknown {
		t.Errorf("expected LabelUnknown, got %s", result.Primary)
	}
}

func TestClassifyByKeywords_SSHPriorityOverCoding(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	// "docker" is SSH strong, "build" is coding strong → SSH wins by priority
	result, _ := classifyByKeywords(registry, affinity, MessageContext{Text: "docker build my image"})
	if result.Primary != LabelSSH {
		t.Errorf("expected LabelSSH (ssh > coding priority), got %s", result.Primary)
	}
}

func TestClassifyByKeywords_ToolNamesPopulated(t *testing.T) {
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{Text: "ssh到服务器"})
	if len(result.ToolNames) == 0 {
		t.Error("expected ToolNames to be populated for SSH intent")
	}
	found := false
	for _, name := range result.ToolNames {
		if name == "ssh" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ToolNames to contain 'ssh', got %v", result.ToolNames)
	}
}
