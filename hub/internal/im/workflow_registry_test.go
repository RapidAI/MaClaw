package im

import (
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Task 1.2 — WorkflowRegistry tests
// ---------------------------------------------------------------------------

func TestNewWorkflowRegistry_BuiltinTemplates(t *testing.T) {
	r := NewWorkflowRegistry()

	builtins := []WorkflowType{
		WorkflowCoding,
		WorkflowProductDesign,
		WorkflowInnovation,
		WorkflowBusinessPlan,
	}
	for _, wt := range builtins {
		tmpl := r.Match(wt)
		if tmpl == nil {
			t.Fatalf("Match(%q) returned nil, expected builtin template", wt)
		}
		if tmpl.Type != wt {
			t.Errorf("Match(%q).Type = %q", wt, tmpl.Type)
		}
	}
}

func TestWorkflowRegistry_Register_Override(t *testing.T) {
	r := NewWorkflowRegistry()

	custom := &WorkflowTemplate{
		Type:        WorkflowCoding,
		Name:        "自定义编程",
		Description: "覆盖内置模板",
		Keywords:    []string{"custom"},
		Phases:      []PhaseTemplate{{ID: "only_phase", Name: "唯一阶段"}},
	}
	r.Register(custom)

	got := r.Match(WorkflowCoding)
	if got == nil || got.Name != "自定义编程" {
		t.Fatalf("Register did not override builtin: got %+v", got)
	}
	if len(got.Phases) != 1 {
		t.Errorf("expected 1 phase after override, got %d", len(got.Phases))
	}
}

func TestWorkflowRegistry_Match_NotFound(t *testing.T) {
	r := NewWorkflowRegistry()
	if got := r.Match("nonexistent"); got != nil {
		t.Errorf("Match(nonexistent) = %+v, want nil", got)
	}
}

func TestWorkflowRegistry_AllDescriptions(t *testing.T) {
	r := NewWorkflowRegistry()
	desc := r.AllDescriptions()

	// Must mention all 4 builtin types.
	for _, wt := range []WorkflowType{WorkflowCoding, WorkflowProductDesign, WorkflowInnovation, WorkflowBusinessPlan} {
		if !strings.Contains(desc, string(wt)) {
			t.Errorf("AllDescriptions missing type %q", wt)
		}
	}
	// Each line should contain the expected fields.
	for _, keyword := range []string{"类型:", "名称:", "描述:", "关键词:"} {
		if !strings.Contains(desc, keyword) {
			t.Errorf("AllDescriptions missing field label %q", keyword)
		}
	}
}

func TestWorkflowRegistry_ConcurrentAccess(t *testing.T) {
	r := NewWorkflowRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.Register(&WorkflowTemplate{
				Type: "concurrent_test",
				Name: "并发测试",
			})
		}()
		go func() {
			defer wg.Done()
			_ = r.Match("concurrent_test")
			_ = r.AllDescriptions()
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Task 1.3 — Builtin template validation tests
// ---------------------------------------------------------------------------

func TestBuiltinCodingTemplate(t *testing.T) {
	tmpl := builtinCodingTemplate()
	assertTemplateValid(t, tmpl, WorkflowCoding, 5)
}

func TestBuiltinProductDesignTemplate(t *testing.T) {
	tmpl := builtinProductDesignTemplate()
	assertTemplateValid(t, tmpl, WorkflowProductDesign, 4)
}

func TestBuiltinInnovationTemplate(t *testing.T) {
	tmpl := builtinInnovationTemplate()
	assertTemplateValid(t, tmpl, WorkflowInnovation, 5)
}

func TestBuiltinBusinessPlanTemplate(t *testing.T) {
	tmpl := builtinBusinessPlanTemplate()
	assertTemplateValid(t, tmpl, WorkflowBusinessPlan, 5)
}

// assertTemplateValid checks phase count, ID uniqueness, and required fields.
func assertTemplateValid(t *testing.T, tmpl *WorkflowTemplate, wantType WorkflowType, wantPhases int) {
	t.Helper()

	if tmpl.Type != wantType {
		t.Errorf("Type = %q, want %q", tmpl.Type, wantType)
	}
	if tmpl.Name == "" {
		t.Error("Name is empty")
	}
	if tmpl.Description == "" {
		t.Error("Description is empty")
	}
	if len(tmpl.Keywords) == 0 {
		t.Error("Keywords is empty")
	}
	if len(tmpl.Phases) != wantPhases {
		t.Fatalf("len(Phases) = %d, want %d", len(tmpl.Phases), wantPhases)
	}

	ids := make(map[string]bool)
	for i, p := range tmpl.Phases {
		if p.ID == "" {
			t.Errorf("Phase[%d].ID is empty", i)
		}
		if ids[p.ID] {
			t.Errorf("Phase[%d].ID %q is duplicate", i, p.ID)
		}
		ids[p.ID] = true

		if p.Name == "" {
			t.Errorf("Phase[%d] %q: Name is empty", i, p.ID)
		}
		if p.Description == "" {
			t.Errorf("Phase[%d] %q: Description is empty", i, p.ID)
		}
		if p.Prompt == "" {
			t.Errorf("Phase[%d] %q: Prompt is empty", i, p.ID)
		}
		if len(p.Checklist) == 0 {
			t.Errorf("Phase[%d] %q: Checklist is empty", i, p.ID)
		}
	}
}
