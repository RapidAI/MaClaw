package v2

import (
	"fmt"
	"strings"
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
	mustHave := []string{"coding", "coding_subagent", "remote_coding_subagent", "patent_application", "us_patent_application", "gaokao_application"}
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

func TestCodingSubAgentTemplatesUseWorkflowInputSchemas(t *testing.T) {
	local := CodingSubAgentTemplate()
	if local == nil || local.Type != "coding_subagent" || len(local.Phases) != 1 {
		t.Fatalf("CodingSubAgentTemplate shape = %#v", local)
	}
	if local.Phases[0].ID != "coding_subagent_execution" {
		t.Fatalf("local template phase ID = %q, want coding_subagent_execution", local.Phases[0].ID)
	}
	if local.Phases[0].ExecMode != ExecModeSubAgent {
		t.Fatalf("local template phase ExecMode = %q, want %q", local.Phases[0].ExecMode, ExecModeSubAgent)
	}
	localSchema := local.Phases[0].InputSchema
	if localSchema == nil {
		t.Fatal("CodingSubAgentTemplate missing InputSchema")
	}
	localRequired := map[string]bool{}
	for _, field := range localSchema.Fields {
		if field.Required {
			localRequired[field.Name] = true
		}
	}
	for _, want := range []string{"work_dir", "project_description"} {
		if !localRequired[want] {
			t.Fatalf("coding_subagent schema missing required field %q; got %#v", want, localRequired)
		}
	}

	remote := RemoteCodingSubAgentTemplate()
	if remote == nil || remote.Type != "remote_coding_subagent" || len(remote.Phases) != 1 {
		t.Fatalf("RemoteCodingSubAgentTemplate shape = %#v", remote)
	}
	if remote.Phases[0].ID != "remote_coding_subagent_execution" {
		t.Fatalf("remote template phase ID = %q, want remote_coding_subagent_execution", remote.Phases[0].ID)
	}
	if remote.Phases[0].ExecMode != ExecModeRemoteSubAgent {
		t.Fatalf("remote template phase ExecMode = %q, want %q", remote.Phases[0].ExecMode, ExecModeRemoteSubAgent)
	}
	remoteSchema := remote.Phases[0].InputSchema
	if remoteSchema == nil {
		t.Fatal("RemoteCodingSubAgentTemplate missing InputSchema")
	}
	remoteRequired := map[string]PhaseInputField{}
	for _, field := range remoteSchema.Fields {
		if field.Required {
			remoteRequired[field.Name] = field
		}
	}
	for _, want := range []string{"ssh_host", "ssh_port", "ssh_user", "ssh_password", "work_dir", "project_description"} {
		if _, ok := remoteRequired[want]; !ok {
			t.Fatalf("remote_coding_subagent schema missing required field %q; got %#v", want, remoteRequired)
		}
	}
	if !remoteRequired["ssh_password"].Sensitive {
		t.Fatal("ssh_password should be marked sensitive")
	}
	if roundTrip := phaseInputFieldFromSpec(phaseInputFieldToSpec(remoteRequired["ssh_password"])); !roundTrip.Sensitive {
		t.Fatal("ssh_password sensitive flag should survive spec round trip")
	}
	if remoteRequired["ssh_port"].Default != 22 {
		t.Fatalf("ssh_port default = %#v, want 22", remoteRequired["ssh_port"].Default)
	}
}

func TestSSHPasswordFieldsAreSensitive(t *testing.T) {
	registry := NewTemplateRegistry()
	RegisterBuiltinTemplates(registry)
	for _, tmpl := range registry.templates {
		for _, phase := range tmpl.Phases {
			if phase.InputSchema == nil {
				continue
			}
			checkField := func(field PhaseInputField, location string) {
				if field.Name == "ssh_password" && !field.Sensitive {
					t.Fatalf("%s ssh_password field must be marked Sensitive", location)
				}
			}
			for _, field := range phase.InputSchema.Fields {
				checkField(field, tmpl.Type+"/"+phase.ID)
			}
			for _, variant := range phase.InputSchema.Variants {
				for _, field := range variant.Fields {
					checkField(field, tmpl.Type+"/"+phase.ID+"/"+variant.ID)
				}
			}
		}
	}
}

func TestPatentApplicationTemplateSupportsRequiredCNIPATypes(t *testing.T) {
	tmpl := PatentApplicationTemplate()
	if tmpl == nil || len(tmpl.Phases) == 0 || tmpl.Phases[0].InputSchema == nil {
		t.Fatal("PatentApplicationTemplate missing initial input schema")
	}

	var patentType *PhaseInputField
	for i := range tmpl.Phases[0].InputSchema.Fields {
		if tmpl.Phases[0].InputSchema.Fields[i].Name == "patent_type" {
			patentType = &tmpl.Phases[0].InputSchema.Fields[i]
			break
		}
	}
	if patentType == nil {
		t.Fatal("patent_type field not found")
	}

	got := map[string]bool{}
	for _, opt := range patentType.Options {
		got[opt.Value] = true
	}
	for _, want := range []string{"invention", "utility_model", "design"} {
		if !got[want] {
			t.Fatalf("patent_type missing %q option; got %#v", want, got)
		}
	}
}

func TestPatentApplicationTemplateHasDesignInputVariant(t *testing.T) {
	tmpl := PatentApplicationTemplate()
	if tmpl == nil || len(tmpl.Phases) == 0 || tmpl.Phases[0].InputSchema == nil {
		t.Fatal("PatentApplicationTemplate missing initial input schema")
	}

	var designVariant *PhaseInputVariant
	for i := range tmpl.Phases[0].InputSchema.Variants {
		if tmpl.Phases[0].InputSchema.Variants[i].ID == "design_mode" {
			designVariant = &tmpl.Phases[0].InputSchema.Variants[i]
			break
		}
	}
	if designVariant == nil {
		t.Fatal("design_mode variant not found")
	}

	required := map[string]bool{}
	names := map[string]bool{}
	for _, field := range designVariant.Fields {
		if names[field.Name] {
			t.Fatalf("design_mode contains duplicate field %q", field.Name)
		}
		names[field.Name] = true
		if field.Required {
			required[field.Name] = true
		}
	}
	for _, want := range []string{"design_product_name", "design_product_use", "design_images_paths", "design_brief_description"} {
		if !required[want] {
			t.Fatalf("design_mode missing required field %q; got %#v", want, required)
		}
	}
	if names["output_dir"] {
		t.Fatal("design_mode should use the shared output_dir field instead of declaring a duplicate")
	}
}

func TestPatentApplicationTemplateUsesTypeNeutralPhaseNames(t *testing.T) {
	tmpl := PatentApplicationTemplate()
	got := map[string]string{}
	for _, phase := range tmpl.Phases {
		got[phase.ID] = phase.Name
	}
	for phaseID, want := range map[string]string{
		"pa_disclosure_parsing":   "申请材料解析",
		"pa_prior_art_search":     "查新/近似检索分析",
		"pa_claims_drafting":      "权利要求/保护要点",
		"pa_figures_organization": "附图/图片整理",
		"pa_description_writing":  "说明书/简要说明",
	} {
		if got[phaseID] != want {
			t.Fatalf("%s name = %q, want %q", phaseID, got[phaseID], want)
		}
	}
}

func TestPatentApplicationFileModeUsesTypeNeutralLabels(t *testing.T) {
	tmpl := PatentApplicationTemplate()
	schema := tmpl.Phases[0].InputSchema
	if schema == nil {
		t.Fatal("PatentApplicationTemplate missing initial input schema")
	}
	if !strings.Contains(schema.Description, "外观设计图片/照片") {
		t.Fatalf("schema description should mention design image/photo input, got %q", schema.Description)
	}

	var fileVariant *PhaseInputVariant
	for i := range schema.Variants {
		if schema.Variants[i].ID == "file_mode" {
			fileVariant = &schema.Variants[i]
			break
		}
	}
	if fileVariant == nil {
		t.Fatal("file_mode variant not found")
	}
	if fileVariant.Label != "交底书/申请材料文件" {
		t.Fatalf("file_mode label = %q, want type-neutral label", fileVariant.Label)
	}
	if len(fileVariant.Fields) != 1 || fileVariant.Fields[0].Label != "交底书/申请材料文件路径" {
		t.Fatalf("file_mode field label should be type-neutral, got %#v", fileVariant.Fields)
	}
}
