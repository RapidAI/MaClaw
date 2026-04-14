package workflow

import (
	"strings"
	"testing"
	"testing/quick"
)

// Feature: maclaw-agent-workflow, Property 5: 模板注册-匹配往返一致性
// For any valid WorkflowTemplate, Register then Match returns the same template.
// Re-registering the same Type overwrites the previous template.
// **Validates: Requirements 3.2, 3.5**
func TestProperty5_RegisterThenMatchRoundTrip(t *testing.T) {
	workflowTypes := []WorkflowType{
		WorkflowCoding, WorkflowProductDesign, WorkflowInnovation,
		WorkflowBusinessPlan, WorkflowTesting,
		"custom_type_a", "custom_type_b",
	}

	f := func(nameIdx, descIdx uint8) bool {
		wt := workflowTypes[int(nameIdx)%len(workflowTypes)]
		names := []string{"模板A", "模板B", "Template C", "工作流D"}
		descs := []string{"描述1", "描述2", "Description 3", "说明4"}
		name := names[int(nameIdx)%len(names)]
		desc := descs[int(descIdx)%len(descs)]

		r := &WorkflowRegistry{templates: make(map[WorkflowType]*WorkflowTemplate)}
		tmpl := &WorkflowTemplate{
			Type:        wt,
			Name:        name,
			Description: desc,
			Phases: []PhaseTemplate{
				{ID: "p1", Name: "Phase1", Prompt: "Do something", Checklist: []string{"check1"}},
			},
		}
		r.Register(tmpl)
		got := r.Match(wt)
		if got == nil {
			return false
		}
		if got.Type != wt || got.Name != name || got.Description != desc {
			return false
		}

		// Re-register with different name — should overwrite
		tmpl2 := &WorkflowTemplate{
			Type:        wt,
			Name:        name + "_v2",
			Description: desc + "_v2",
			Phases:      tmpl.Phases,
		}
		r.Register(tmpl2)
		got2 := r.Match(wt)
		if got2 == nil {
			return false
		}
		return got2.Name == name+"_v2" && got2.Description == desc+"_v2"
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 5 (register-match round trip) failed: %v", err)
	}
}

// Feature: maclaw-agent-workflow, Property 6: AllDescriptions 完整性
// For any set of registered templates, AllDescriptions contains every
// template's Name and Description.
// **Validates: Requirements 3.3**
func TestProperty6_AllDescriptionsCompleteness(t *testing.T) {
	f := func(count uint8) bool {
		n := int(count)%5 + 1 // 1-5 templates
		r := &WorkflowRegistry{templates: make(map[WorkflowType]*WorkflowTemplate)}

		type entry struct {
			name string
			desc string
		}
		entries := make([]entry, n)
		for i := 0; i < n; i++ {
			wt := WorkflowType(string(rune('A' + i)))
			name := "Name_" + string(rune('A'+i))
			desc := "Desc_" + string(rune('A'+i))
			r.Register(&WorkflowTemplate{
				Type:        wt,
				Name:        name,
				Description: desc,
			})
			entries[i] = entry{name, desc}
		}

		allDesc := r.AllDescriptions()
		for _, e := range entries {
			if !strings.Contains(allDesc, e.name) {
				return false
			}
			if !strings.Contains(allDesc, e.desc) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 6 (AllDescriptions completeness) failed: %v", err)
	}
}
