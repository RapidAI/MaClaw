package v2

import (
	"testing"
)

func setupTestRouter() *WorkflowRouter {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	machine := NewStateMachine(store, templates)
	return NewWorkflowRouter(machine, templates, nil)
}

func TestRoute_CodingTask(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "在d:\\game2 下开发贪吃蛇 C++", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow", result.Target)
	}
	if result.WorkflowType != "coding" {
		t.Fatalf("type = %q, want coding", result.WorkflowType)
	}
	if result.ProjectPath != "d:\\game2" {
		t.Fatalf("projectPath = %q, want d:\\game2", result.ProjectPath)
	}
}

func TestRoute_NonCodingTask(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "帮我查一下杭州天气", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop", result.Target)
	}
}

func TestRoute_SkipSignal(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "直接做一个贪吃蛇游戏", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (skip signal)", result.Target)
	}
}

func TestRoute_BugFix(t *testing.T) {
	r := setupTestRouter()
	// "修复加载卡住的bug" doesn't match any template keywords (coding template
	// requires "开发"/"写代码" etc). Without keyword match → RouteToAgentLoop.
	// Bug fixes are handled by the normal agent loop with full tools.
	result := r.Route("user1", "修复加载卡住的bug", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (no keyword match)", result.Target)
	}
}

func TestRoute_BugFixWithCreation(t *testing.T) {
	r := setupTestRouter()
	// "开发一个bug追踪系统" has both bug-fix and creation keywords
	result := r.Route("user1", "开发一个bug追踪系统", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow (creation overrides bug-fix)", result.Target)
	}
}

func TestRoute_PPTTask(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "帮我设计一个产品介绍PPT", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow", result.Target)
	}
	if result.WorkflowType != "presentation_design" {
		t.Fatalf("type = %q, want presentation_design", result.WorkflowType)
	}
}

func TestRoute_ActiveWorkflow_Confirm(t *testing.T) {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	machine := NewStateMachine(store, templates)
	// Set classifier so "确认" is recognized as confirm
	machine.SetConfirmClassifier(func(phaseContext, userText string) string {
		return ClassifyConfirmIntentKeyword(userText)
	})
	router := NewWorkflowRouter(machine, templates, nil)

	// Start a workflow and record output
	machine.Create("user1", "coding", "d:\\project", "build app")
	machine.RecordOutput("user1", "# Requirements")

	// User confirms
	result := router.Route("user1", "确认", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow", result.Target)
	}
	if result.HandleResult == nil || result.HandleResult.Action != ActionRunPhase {
		t.Fatalf("action = %v", result.HandleResult)
	}
}

func TestRoute_ActiveWorkflow_UnrelatedMessage(t *testing.T) {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	machine := NewStateMachine(store, templates)
	router := NewWorkflowRouter(machine, templates, nil)

	machine.Create("user1", "coding", "d:\\project", "build app")
	machine.RecordOutput("user1", "# Requirements")

	// Unrelated short message
	result := router.Route("user1", "嗯", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (unrelated)", result.Target)
	}
}

func TestRoute_AttachmentWithShortText(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "看看这个", []Attachment{{Type: "image", Name: "screenshot.png"}})
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (attachment)", result.Target)
	}
}

func TestExtractProjectPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"在d:\\game2 下开发贪吃蛇", "d:\\game2"},
		{"在d:\\workprj\\snake 目录开发", "d:\\workprj\\snake"},
		{"在/home/user/project 下写代码", "/home/user/project"},
		{"开发一个游戏", ""},
		{"d:\\snake55 开发贪吃蛇", "d:\\snake55"},
	}
	for _, tc := range tests {
		got := ExtractProjectPathFromText(tc.input)
		if got != tc.want {
			t.Errorf("ExtractProjectPathFromText(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
