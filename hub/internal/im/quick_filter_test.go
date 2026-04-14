package im

import "testing"

// mockSessionChecker implements ActiveSessionChecker for testing.
type mockSessionChecker struct {
	workflowUsers      map[string]bool
	understandingUsers map[string]bool
}

func (m *mockSessionChecker) HasActiveWorkflow(userID string) bool {
	return m.workflowUsers[userID]
}

func (m *mockSessionChecker) HasActiveUnderstanding(userID string) bool {
	return m.understandingUsers[userID]
}

func TestQuickFilter_Command(t *testing.T) {
	qf := NewQuickFilter(&mockSessionChecker{})
	tests := []string{"/help", "/call all", "/workflow cancel", "/stop"}
	for _, text := range tests {
		if got := qf.Filter("u1", text); got != FilterCommand {
			t.Errorf("Filter(%q) = %d, want FilterCommand", text, got)
		}
	}
}

func TestQuickFilter_ActiveWorkflow(t *testing.T) {
	checker := &mockSessionChecker{
		workflowUsers: map[string]bool{"u1": true},
	}
	qf := NewQuickFilter(checker)
	if got := qf.Filter("u1", "继续"); got != FilterActiveWorkflow {
		t.Errorf("Filter with active workflow = %d, want FilterActiveWorkflow", got)
	}
}

func TestQuickFilter_ActiveUnderstanding(t *testing.T) {
	checker := &mockSessionChecker{
		understandingUsers: map[string]bool{"u1": true},
	}
	qf := NewQuickFilter(checker)
	if got := qf.Filter("u1", "加个登录功能"); got != FilterActiveUnderstanding {
		t.Errorf("Filter with active understanding = %d, want FilterActiveUnderstanding", got)
	}
}

func TestQuickFilter_SmallTalk(t *testing.T) {
	qf := NewQuickFilter(&mockSessionChecker{})
	tests := []string{"你好", "谢谢", "嗯", "ok", "hi", "hello", "thanks", "好的"}
	for _, text := range tests {
		if got := qf.Filter("u1", text); got != FilterSmallTalk {
			t.Errorf("Filter(%q) = %d, want FilterSmallTalk", text, got)
		}
	}
}

func TestQuickFilter_SimpleDirective(t *testing.T) {
	qf := NewQuickFilter(&mockSessionChecker{})
	tests := []string{
		"翻译这段话成英文",
		"格式化这段代码",
		"总结一下这篇文章",
		"整理这些数据",
		"转换成JSON格式",
	}
	for _, text := range tests {
		if got := qf.Filter("u1", text); got != FilterSimpleDirective {
			t.Errorf("Filter(%q) = %d, want FilterSimpleDirective", text, got)
		}
	}
}

func TestQuickFilter_NeedsUnderstanding(t *testing.T) {
	qf := NewQuickFilter(&mockSessionChecker{})
	tests := []string{
		"帮我开发一个CRM系统",
		"我想做一个能让团队协作的项目管理工具",
		"设计一个电商平台的后端架构",
	}
	for _, text := range tests {
		if got := qf.Filter("u1", text); got != FilterNeedsUnderstanding {
			t.Errorf("Filter(%q) = %d, want FilterNeedsUnderstanding", text, got)
		}
	}
}

func TestQuickFilter_WorkflowPriority(t *testing.T) {
	// Even small talk should go to workflow handler if workflow is active
	checker := &mockSessionChecker{
		workflowUsers: map[string]bool{"u1": true},
	}
	qf := NewQuickFilter(checker)
	if got := qf.Filter("u1", "你好"); got != FilterActiveWorkflow {
		t.Errorf("Filter('你好') with active workflow = %d, want FilterActiveWorkflow", got)
	}
}

func TestIsSmallTalk(t *testing.T) {
	positives := []string{"你好", "ok", "hi", "谢谢", "嗯", "好的", "hello"}
	for _, s := range positives {
		if !isSmallTalk(s) {
			t.Errorf("isSmallTalk(%q) = false, want true", s)
		}
	}
	negatives := []string{"帮我写一个程序", "翻译这段话", "这个项目需要重构"}
	for _, s := range negatives {
		if isSmallTalk(s) {
			t.Errorf("isSmallTalk(%q) = true, want false", s)
		}
	}
}

func TestIsSimpleDirective(t *testing.T) {
	positives := []string{"翻译成英文", "格式化代码", "总结文章", "整理数据", "转换格式"}
	for _, s := range positives {
		if !isSimpleDirective(s) {
			t.Errorf("isSimpleDirective(%q) = false, want true", s)
		}
	}
	negatives := []string{"帮我开发一个系统", "设计架构", "你好"}
	for _, s := range negatives {
		if isSimpleDirective(s) {
			t.Errorf("isSimpleDirective(%q) = true, want false", s)
		}
	}
}
