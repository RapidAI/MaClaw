package v2

import (
	"fmt"
	"sync"
	"testing"
)

func TestTemplateRegistryMatchIgnoresKeywords(t *testing.T) {
	registry := NewTemplateRegistry()
	registry.Register(&WorkflowTemplate{
		Type:        "alpha",
		Name:        "Alpha workflow",
		Description: "General planning workflow",
		Keywords:    []string{"backend", "database", "api"},
		Phases:      []PhaseTemplate{{ID: "plan", Name: "Plan"}},
	})

	if got := registry.MatchByText("backend database api"); got != nil {
		t.Fatalf("MatchByText keyword-only query = %#v, want nil", got)
	}
	if got := registry.MatchByKeywords("backend database api"); got != nil {
		t.Fatalf("MatchByKeywords compatibility wrapper used keywords: %#v", got)
	}
	if ranked := registry.RankedByText("   "); len(ranked) != 0 {
		t.Fatalf("blank query should not rank templates, got %#v", ranked)
	}
}

func TestTemplateRegistryConcurrentRegisterAndRank(t *testing.T) {
	registry := NewTemplateRegistry()
	registry.Register(&WorkflowTemplate{
		Type:        "base",
		Name:        "Base workflow",
		Description: "Base workflow for initial ranking",
		Phases:      []PhaseTemplate{{ID: "plan", Name: "Plan"}},
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			registry.Register(&WorkflowTemplate{
				Type:        fmt.Sprintf("dynamic_%d", i),
				Name:        fmt.Sprintf("Dynamic %d dynamicmarker%d", i, i),
				Description: fmt.Sprintf("Unique semantic marker dynamicmarker%d workflow for dynamicmarker%d tasks", i, i),
				Phases:      []PhaseTemplate{{ID: "plan", Name: "Plan"}},
			})
			_ = registry.RankedByText(fmt.Sprintf("dynamicmarker%d", i))
		}(i)
	}
	wg.Wait()

	// Verify that after concurrent registration, the correct template ranks #1.
	// We use RankedByText instead of MatchByText because the test validates
	// concurrency safety, not BM25 absolute score thresholds.
	ranked := registry.RankedByText("dynamicmarker19")
	if len(ranked) == 0 || ranked[0].Type != "dynamic_19" {
		t.Fatalf("RankedByText(dynamicmarker19) top = %v, want dynamic_19", ranked)
	}
}

func TestTemplateRegistryZeroValueRegister(t *testing.T) {
	var registry TemplateRegistry
	registry.Register(nil)
	registry.Register(&WorkflowTemplate{
		Type:        " ",
		Name:        "Blank",
		Description: "blankonly",
		Phases:      []PhaseTemplate{{ID: "plan", Name: "Plan"}},
	})
	registry.Register(&WorkflowTemplate{
		Type:        "zero_value",
		Name:        "Zero value zeromarker",
		Description: "zero marker workflow for zero marker tasks and zero marker projects",
		Phases:      []PhaseTemplate{{ID: "plan", Name: "Plan"}},
	})
	// Use RankedByText to verify zero-value struct works; absolute score
	// threshold is tested separately.
	ranked := registry.RankedByText("zero marker")
	if len(ranked) == 0 || ranked[0].Type != "zero_value" {
		t.Fatalf("zero-value RankedByText top = %v, want zero_value", ranked)
	}
	if got := registry.MatchByText("blankonly"); got != nil {
		t.Fatalf("blank type template should not match: %#v", got)
	}
}

func TestTemplateRegistryNilReceiverAndNilBuiltinRegistrationAreSafe(t *testing.T) {
	var registry *TemplateRegistry
	registry.Register(&WorkflowTemplate{
		Type:        "nil_registry",
		Description: "nil registry",
	})
	if got := registry.Get("anything"); got != nil {
		t.Fatalf("nil Get = %#v, want nil", got)
	}
	RegisterBuiltinTemplates(nil)
}

func TestTemplateRegistryAmbiguousTopScoreDoesNotSelectByTieBreak(t *testing.T) {
	registry := NewTemplateRegistry()
	for _, tmpl := range []*WorkflowTemplate{
		{
			Type:        "alpha",
			Name:        "Shared",
			Description: "same semantic marker",
			Phases:      []PhaseTemplate{{ID: "plan", Name: "Plan"}},
		},
		{
			Type:        "beta",
			Name:        "Shared",
			Description: "same semantic marker",
			Phases:      []PhaseTemplate{{ID: "plan", Name: "Plan"}},
		},
	} {
		registry.Register(tmpl)
	}
	if got := registry.MatchByText("same semantic marker"); got != nil {
		t.Fatalf("ambiguous tied templates should not select by tie-break: %#v", got)
	}
}


// TestAllTypes_ReturnsAllRegisteredTemplates verifies that AllTypes() dynamically
// reflects all templates registered via RegisterBuiltinTemplates. This test will
// FAIL if a new template is added to RegisterBuiltinTemplates but something goes
// wrong with AllTypes(). It also serves as a compile-time signal that adding a
// template to RegisterBuiltinTemplates is sufficient — no other hardcoded list
// needs to be maintained.
func TestAllTypes_ReturnsAllRegisteredTemplates(t *testing.T) {
	registry := NewTemplateRegistry()
	RegisterBuiltinTemplates(registry)

	types := registry.AllTypes()
	if len(types) == 0 {
		t.Fatal("AllTypes() returned empty slice after RegisterBuiltinTemplates")
	}

	// Verify every type returned by AllTypes() is retrievable via Get()
	for _, typ := range types {
		if registry.Get(typ) == nil {
			t.Errorf("AllTypes() returned %q but Get(%q) is nil", typ, typ)
		}
	}

	// Verify count matches the internal map
	registry.mu.RLock()
	mapLen := len(registry.templates)
	registry.mu.RUnlock()
	if len(types) != mapLen {
		t.Errorf("AllTypes() returned %d types, but registry has %d templates", len(types), mapLen)
	}

	// Sanity check: known templates must be present
	mustHave := []string{"coding", "patent_application", "us_patent_application"}
	typeSet := make(map[string]bool, len(types))
	for _, typ := range types {
		typeSet[typ] = true
	}
	for _, required := range mustHave {
		if !typeSet[required] {
			t.Errorf("AllTypes() missing expected template: %q", required)
		}
	}
}
