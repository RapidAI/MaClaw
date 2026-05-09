package workflow

import "testing"

func TestQuickFilter_NonEmptyInputGoesToUnderstanding(t *testing.T) {
	qf := NewQuickFilter(&MockWorkflowChecker{})

	cases := []string{
		"你好",
		"hello",
		"好的",
		"帮我开发一个 CRM 系统",
		"总结这篇文章",
		"？！。，、；：",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			result := qf.Classify("u1", msg)
			if result != FilterNeedsUnderstanding {
				t.Errorf("Classify(%q) = %s, want %s", msg, result, FilterNeedsUnderstanding)
			}
		})
	}
}

func TestQuickFilter_EmptyMessage(t *testing.T) {
	qf := NewQuickFilter(&MockWorkflowChecker{})
	result := qf.Classify("u1", "")
	if result != FilterSimpleDirective {
		t.Errorf("expected simple_directive for empty message, got %s", result)
	}
}

func TestQuickFilter_WhitespaceOnly(t *testing.T) {
	qf := NewQuickFilter(&MockWorkflowChecker{})
	result := qf.Classify("u1", "   \t\n  ")
	if result != FilterSimpleDirective {
		t.Errorf("expected simple_directive for whitespace-only, got %s", result)
	}
}

func TestQuickFilter_NilEngine(t *testing.T) {
	qf := NewQuickFilter(nil)
	result := qf.Classify("u1", "你好")
	if result != FilterNeedsUnderstanding {
		t.Errorf("expected needs_understanding with nil engine, got %s", result)
	}
}

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

func TestStartResult_LLMRejectsNonWorkflow(t *testing.T) {
	result := &StartResult{Rejected: true}
	if !result.Rejected {
		t.Error("expected Rejected=true")
	}
	if result.Reply != "" {
		t.Errorf("expected empty reply for rejected, got %q", result.Reply)
	}
}
