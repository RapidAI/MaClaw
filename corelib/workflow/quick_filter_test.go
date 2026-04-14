package workflow

import "testing"

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

func TestQuickFilter_ClassifySimpleDirective(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)

	cases := []struct {
		name string
		msg  string
	}{
		{"translate", "翻译这段英文"},
		{"summarize", "总结一下这篇文章"},
		{"format", "格式化这段代码"},
		{"search", "搜索一下Go语言教程"},
		{"generate", "生成一个UUID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := qf.Classify("u1", tc.msg)
			if result != FilterSimpleDirective {
				t.Errorf("expected simple_directive for %q, got %s", tc.msg, result)
			}
		})
	}
}

func TestQuickFilter_ClassifyNeedsUnderstanding(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)

	cases := []struct {
		name string
		msg  string
	}{
		{"crm", "帮我开发一个CRM系统，需要支持多租户和权限管理"},
		{"ecommerce", "设计一个电商平台，要求高可用和安全"},
		{"microservice", "帮我搭建一个微服务系统，必须支持高并发"},
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
	// A long message without complex task features should default to simple_directive
	long := ""
	for i := 0; i < 1000; i++ {
		long += "这是一段很长的普通文本。"
	}
	result := qf.Classify("u1", long)
	if result != FilterSimpleDirective {
		t.Errorf("expected simple_directive for long plain text, got %s", result)
	}
}

func TestQuickFilter_PunctuationOnly(t *testing.T) {
	checker := &MockWorkflowChecker{}
	qf := NewQuickFilter(checker)
	result := qf.Classify("u1", "！？。，、；：")
	if result != FilterSimpleDirective {
		t.Errorf("expected simple_directive for punctuation-only, got %s", result)
	}
}

func TestQuickFilter_NilEngine(t *testing.T) {
	qf := NewQuickFilter(nil)
	// Without engine, active session checks are skipped
	result := qf.Classify("u1", "你好")
	if result != FilterSmallTalk {
		t.Errorf("expected small_talk with nil engine, got %s", result)
	}
}

func TestQuickFilter_ActiveWorkflowPriority(t *testing.T) {
	checker := &MockWorkflowChecker{
		ActiveWorkflow: map[string]bool{"u1": true},
	}
	qf := NewQuickFilter(checker)
	// Even a small talk message should return active_workflow
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
