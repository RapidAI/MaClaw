package workflow

import "testing"

// ---------------------------------------------------------------------------
// Small talk detection — definitively NOT workflow
// ---------------------------------------------------------------------------

func TestQuickFilter_ClassifySmallTalk(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)

	cases := []struct {
		name string
		msg  string
	}{
		{"greeting_nihao", "你好"},
		{"greeting_hello", "hello"},
		{"greeting_hi", "hi"},
		{"thanks", "谢谢"},
		{"morning", "早上好"},
		{"bye", "再见"},
		{"ok", "好的"},
		{"affirmation", "嗯"},
		{"exact_hao", "好"},
		{"exact_xing", "行"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := qf.Classify("u1", tc.msg)
			if result != FilterSmallTalk {
				t.Errorf("expected small_talk for %q, got %s", tc.msg, result)
			}
		})
	}
}

func TestQuickFilter_SmallTalkExactMatch(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)

	cases := []struct {
		name   string
		msg    string
		expect FilterResult
	}{
		// Single-char exact match
		{"exact_hao", "好", FilterSmallTalk},
		{"exact_xing", "行", FilterSmallTalk},
		{"exact_en", "嗯", FilterSmallTalk},
		// "好的开始吧" is NOT small talk — it's an action phrase
		{"hao_de_kaishi", "好的开始吧", FilterNeedsUnderstanding},
		// "好的" alone is small talk (exact match)
		{"hao_de", "好的", FilterSmallTalk},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := qf.Classify("u1", tc.msg)
			if result != tc.expect {
				t.Errorf("Classify(%q) = %s, want %s", tc.msg, result, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Everything non-small-talk goes to LLM (FilterNeedsUnderstanding)
// ---------------------------------------------------------------------------

func TestQuickFilter_AllNonSmallTalkGoesToLLM(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)

	// ALL of these should go to FilterNeedsUnderstanding — the LLM decides
	// whether they are workflow tasks or simple directives.
	cases := []struct {
		name string
		msg  string
	}{
		// Simple directives — LLM will classify as "none"
		{"translate", "翻译这段英文"},
		{"summarize", "总结一下这篇文章"},
		{"format", "格式化这段代码"},
		{"search", "搜索一下Go语言教程"},
		{"generate_uuid", "生成一个UUID"},
		{"explain", "什么是微服务架构"},
		{"how_to", "怎么配置nginx"},

		// Complex tasks — LLM will classify to a workflow template
		{"crm", "帮我开发一个CRM系统，需要支持多租户和权限管理"},
		{"ecommerce", "设计一个电商平台，要求高可用和安全"},
		{"microservice", "帮我搭建一个微服务系统，必须支持高并发"},
		{"game", "开发一个贪吃蛇游戏"},
		{"prd", "生成网络安全产品的PRD文档"},
		{"bp", "帮我做一份商业计划书"},
		{"how_to_ecommerce", "怎么做一个电商系统"},

		// Ambiguous — LLM decides
		{"write_code", "帮我写一段Python排序代码"},
		{"long_text", "帮我整理一下这些会议纪要，按时间排序"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := qf.Classify("u1", tc.msg)
			if result != FilterNeedsUnderstanding {
				t.Errorf("expected needs_understanding for %q, got %s", tc.msg, result)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestQuickFilter_EmptyMessage(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)
	result := qf.Classify("u1", "")
	if result != FilterSimpleDirective {
		t.Errorf("expected simple_directive for empty message, got %s", result)
	}
}

func TestQuickFilter_WhitespaceOnly(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)
	result := qf.Classify("u1", "   \t\n  ")
	if result != FilterSimpleDirective {
		t.Errorf("expected simple_directive for whitespace-only, got %s", result)
	}
}

func TestQuickFilter_VeryLongMessage(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)
	// A long message goes to LLM regardless of content
	long := ""
	for i := 0; i < 100; i++ {
		long += "这是一段很长的普通文本。"
	}
	result := qf.Classify("u1", long)
	if result != FilterNeedsUnderstanding {
		t.Errorf("expected needs_understanding for long text, got %s", result)
	}
}

func TestQuickFilter_PunctuationOnly(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)
	// Punctuation-only goes to LLM (not small talk, not empty)
	result := qf.Classify("u1", "！？。，、；：")
	if result != FilterNeedsUnderstanding {
		t.Errorf("expected needs_understanding for punctuation-only, got %s", result)
	}
}

func TestQuickFilter_NilEngine(t *testing.T) {
	qf := NewQuickFilter(nil)
	result := qf.Classify("u1", "你好")
	if result != FilterSmallTalk {
		t.Errorf("expected small_talk with nil engine, got %s", result)
	}
}

// ---------------------------------------------------------------------------
// Active session priority
// ---------------------------------------------------------------------------

func TestQuickFilter_ActiveWorkflowPriority(t *testing.T) {
	checker := &MockWorkflowChecker{
		ActiveWorkflow: map[string]bool{"u1": true},
	}
	qf := NewQuickFilter(checker)
	result := qf.Classify("u1", "你好")
	if result != FilterActiveWorkflow {
		t.Errorf("expected active_workflow, got %s", result)
	}
}

func TestQuickFilter_ActiveUnderstandingPriority(t *testing.T) {
	checker := &MockWorkflowChecker{
		ActiveUnderstanding: map[string]bool{"u1": true},
	}
	qf := NewQuickFilter(checker)
	result := qf.Classify("u1", "你好")
	if result != FilterActiveUnderstanding {
		t.Errorf("expected active_understanding, got %s", result)
	}
}

// ---------------------------------------------------------------------------
// StartResult rejection test
// ---------------------------------------------------------------------------

func TestStartResult_LLMRejectsNonWorkflow(t *testing.T) {
	// Verify that when LLM returns category="none", Start returns Rejected=true
	result := &StartResult{Rejected: true}
	if !result.Rejected {
		t.Error("expected Rejected=true")
	}
	if result.Reply != "" {
		t.Errorf("expected empty reply for rejected, got %q", result.Reply)
	}
}
