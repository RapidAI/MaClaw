package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestSkillRunParameterContractSynthesizesAgentViewFields(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name:        "expense-helper",
		Description: "Create an expense report",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "expense-tool --amount {{amount}} --reason {{reason}}",
			},
		}},
		RequiredArgs: []string{"amount", "reason"},
	}
	params, missing := skillRunParameterContract(skill, map[string]string{}, map[string]interface{}{})
	if len(missing) != 2 {
		t.Fatalf("expected two missing params, got %#v", missing)
	}
	view := buildSkillRunAgentView(*skill, map[string]interface{}{"args": map[string]interface{}{}}, params, missing)
	if view == nil || view["type"] != "form" || view["id"] != "skill:run:expense-helper" {
		t.Fatalf("unexpected skill AgentView: %#v", view)
	}
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		t.Fatalf("unexpected fields type: %#v", view["fields"])
	}
	found := map[string]bool{}
	for _, field := range fields {
		name, _ := field["name"].(string)
		found[name] = true
		if name == "amount" && field["type"] != "number" {
			t.Fatalf("amount should infer number field, got %#v", field)
		}
		if (name == "amount" || name == "reason") && field["required"] != true {
			t.Fatalf("%s should be required, got %#v", name, field)
		}
	}
	for _, name := range []string{"amount", "reason", "_run_args", agentViewSchemaVersionField} {
		if !found[name] {
			t.Fatalf("expected field %q, got %#v", name, fields)
		}
	}
	if meta, _ := view["meta"].(map[string]interface{}); meta["schemaVersion"] == "" || meta["schemaSource"] != "skill.adapter" {
		t.Fatalf("expected skill schema metadata, got %#v", meta)
	}
}

func TestSkillRunUnconsumedArgsRejectsIgnoredRuntimeArgs(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "weather",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python weather.py realtime --lat {{lat}} --lng {{lng}}",
			},
		}},
	}
	runArgs := map[string]interface{}{"args": map[string]interface{}{"city": "天津"}}
	runArgs["args"].(map[string]interface{})["lat"] = 30.27
	runArgs["args"].(map[string]interface{})["lng"] = 120.15
	vars := normalizeSkillRunVars(runArgs)
	params, missing := skillRunParameterContract(skill, vars, runArgs)
	if len(missing) != 0 {
		t.Fatalf("missing = %#v, want none", missing)
	}
	unknown := skillRunUnconsumedArgs(skill, params, vars, runArgs)
	if len(unknown) != 1 || unknown[0] != "city" {
		t.Fatalf("unknown = %#v, want [city]", unknown)
	}
}

func TestSkillRunUnconsumedArgsIgnoresHiddenRuntimeArgs(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "weather",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python weather.py {{action}} --lat {{lat}} --lng {{lng}}",
			},
		}},
	}
	runArgs := map[string]interface{}{
		"_runtime_platform":        "desktop",
		"_runtime_policy_owner_id": "desktop-user",
		"args": map[string]interface{}{
			"mode": "realtime",
			"lat":  30.2741,
			"lng":  120.1551,
		},
	}
	vars := normalizeSkillRunVars(runArgs)
	params, missing := skillRunParameterContract(skill, vars, runArgs)
	if len(missing) != 0 {
		t.Fatalf("missing = %#v, want none", missing)
	}
	if unknown := skillRunUnconsumedArgs(skill, params, vars, runArgs); len(unknown) != 0 {
		t.Fatalf("unknown = %#v, want hidden runtime args ignored", unknown)
	}
}

func TestSkillRunUnconsumedArgsAllowsTemplateAndAliasArgs(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "writer",
		Params: []corelib.NLSkillParam{{
			Name:    "input",
			Aliases: []string{"text"},
		}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python write.py --input {{input}} --format {{output_format}}",
			},
		}},
	}
	runArgs := map[string]interface{}{"args": map[string]interface{}{"text": "hello", "output_format": "md"}}
	vars := normalizeSkillRunVars(runArgs)
	params, _ := skillRunParameterContract(skill, vars, runArgs)
	if unknown := skillRunUnconsumedArgs(skill, params, vars, runArgs); len(unknown) != 0 {
		t.Fatalf("unknown = %#v, want none", unknown)
	}
}

func TestSkillRunUnconsumedArgsAllowsRunnerCarrierArgs(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "infer-city",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python weather.py --city {{city}}",
			},
		}},
		RequiredArgs: []string{"city"},
	}
	runArgs := map[string]interface{}{"args": map[string]interface{}{"user_prompt": "city: 天津"}}
	vars := normalizeSkillRunVars(runArgs)
	params, _ := skillRunParameterContract(skill, vars, runArgs)
	if unknown := skillRunUnconsumedArgs(skill, params, vars, runArgs); len(unknown) != 0 {
		t.Fatalf("unknown = %#v, want none", unknown)
	}
}

func TestSkillRunUnconsumedArgsUsesRunnerBindingAliases(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "convert",
		Params: []corelib.NLSkillParam{{
			Name:     "input",
			Required: true,
		}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python convert.py --input {{input}}",
			},
		}},
	}
	runArgs := map[string]interface{}{"args": map[string]interface{}{"file": "report.md"}}
	vars := normalizeSkillRunVars(runArgs)
	params, _ := skillRunParameterContract(skill, vars, runArgs)
	if unknown := skillRunUnconsumedArgs(skill, params, vars, runArgs); len(unknown) != 0 {
		t.Fatalf("unknown = %#v, want none", unknown)
	}
}

func TestSkillRunUnconsumedArgsUsesCanonicalKeys(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "convert",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python convert.py --input {{input_file}}",
			},
		}},
	}
	runArgs := map[string]interface{}{"args": map[string]interface{}{"Input-File": "report.md"}}
	vars := normalizeSkillRunVars(runArgs)
	params, _ := skillRunParameterContract(skill, vars, runArgs)
	if unknown := skillRunUnconsumedArgs(skill, params, vars, runArgs); len(unknown) != 0 {
		t.Fatalf("unknown = %#v, want none", unknown)
	}
}

func TestSkillRunUnconsumedArgsUsesPipelinePlaceholders(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "pipe",
		Mode: "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill:  "child",
			Params: map[string]string{"input": "{{input}}"},
		}},
	}
	runArgs := map[string]interface{}{"args": map[string]interface{}{"input": "report.md", "city": "Nanjing"}}
	vars := normalizeSkillRunVars(runArgs)
	params, _ := skillRunParameterContract(skill, vars, runArgs)
	unknown := skillRunUnconsumedArgs(skill, params, vars, runArgs)
	if len(unknown) != 1 || unknown[0] != "city" {
		t.Fatalf("unknown = %#v, want [city]", unknown)
	}
}

func TestSkillAgentViewFieldsPreserveRunArgsAsHiddenContext(t *testing.T) {
	params := []corelib.NLSkillParam{{Name: "input", Required: true}}
	fields := skillAgentViewFields(params, []string{"input"}, map[string]interface{}{"operation": "generate"})
	if len(fields) != 2 {
		t.Fatalf("expected one visible field plus hidden run args, got %#v", fields)
	}
	if fields[0]["name"] != "input" || fields[0]["type"] != "text" {
		t.Fatalf("unexpected visible field: %#v", fields[0])
	}
	if fields[1]["name"] != "_run_args" || fields[1]["type"] != "hidden" {
		t.Fatalf("expected hidden run args field, got %#v", fields[1])
	}
}

func TestSkillAgentViewFieldsInferRuntimeControls(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "include_summary", Description: "true/false", Default: "true"},
		{Name: "format", Description: "one of: pdf, docx, markdown", Required: true},
		{Name: "input_path", Description: "Input file path", Aliases: []string{"input", "file"}},
		{Name: "working_dir", Description: "Working directory"},
		{Name: "api_token", Description: "API token"},
		{Name: "due_date", Description: "Due date"},
	}

	fields := skillAgentViewFields(params, []string{"format"}, map[string]interface{}{})
	byName := map[string]map[string]interface{}{}
	for _, field := range fields {
		name, _ := field["name"].(string)
		byName[name] = field
	}

	if byName["include_summary"]["type"] != "boolean" || byName["include_summary"]["defaultValue"] != true {
		t.Fatalf("boolean field not inferred/coerced: %#v", byName["include_summary"])
	}
	if byName["format"]["type"] != "select" || byName["format"]["required"] != true {
		t.Fatalf("enum field not inferred: %#v", byName["format"])
	}
	options, ok := byName["format"]["options"].([]map[string]interface{})
	if !ok || len(options) != 3 || options[0]["value"] != "pdf" {
		t.Fatalf("enum options not inferred: %#v", byName["format"]["options"])
	}
	if byName["input_path"]["type"] != "file" || !strings.Contains(fmt.Sprint(byName["input_path"]["placeholder"]), "Aliases:") {
		t.Fatalf("file field hints not inferred: %#v", byName["input_path"])
	}
	if byName["working_dir"]["type"] != "directory" || !strings.Contains(fmt.Sprint(byName["working_dir"]["placeholder"]), "folder path") {
		t.Fatalf("directory field hints not inferred: %#v", byName["working_dir"])
	}
	if byName["api_token"]["sensitive"] != true || byName["api_token"]["format"] != "password" {
		t.Fatalf("sensitive field hints not inferred: %#v", byName["api_token"])
	}
	if byName["due_date"]["type"] != "date" {
		t.Fatalf("date field not inferred: %#v", byName["due_date"])
	}
}

func TestSkillAgentViewFieldKindDoesNotTreatRedirectAsDirectory(t *testing.T) {
	if got := skillAgentViewFieldType("redirect_url", "Callback redirect URL"); got != "text" {
		t.Fatalf("redirect_url field type = %q, want text", got)
	}
	if got := skillAgentViewFieldType("pathology_note", "Clinical pathology note"); got != "text" {
		t.Fatalf("pathology_note field type = %q, want text", got)
	}
}

func TestSkillAgentViewFieldKindPrefersPathControlOverBroadTextHints(t *testing.T) {
	if got := skillAgentViewFieldType("content_dir", "Folder containing markdown source content"); got != "directory" {
		t.Fatalf("content_dir field type = %q, want directory", got)
	}
	if got := skillAgentViewFieldType("prompt_file", "Markdown prompt file path"); got != "file" {
		t.Fatalf("prompt_file field type = %q, want file", got)
	}
}

func TestNormalizeSkillAgentViewSubmittedValuesCoercesAndValidates(t *testing.T) {
	fields := []map[string]interface{}{
		{"name": "amount", "label": "Amount", "type": "number"},
		{"name": "include_summary", "label": "Include Summary", "type": "boolean"},
		{"name": "format", "label": "Format", "type": "select", "options": []map[string]interface{}{
			{"value": "pdf", "label": "PDF"},
			{"value": "docx", "label": "DOCX"},
		}},
		{"name": "due_date", "label": "Due Date", "type": "date"},
	}
	submitted := map[string]interface{}{
		"amount":          "42.5",
		"include_summary": "yes",
		"format":          "pdf",
		"due_date":        "2026-05-09",
	}
	runArgs := map[string]interface{}{}
	formArgs := map[string]interface{}{}

	if issues := normalizeSkillAgentViewSubmittedValues(fields, submitted, runArgs, formArgs); len(issues) != 0 {
		t.Fatalf("unexpected issues: %#v", issues)
	}
	if runArgs["amount"] != float64(42.5) || formArgs["amount"] != float64(42.5) {
		t.Fatalf("number was not coerced into run/form args: run=%#v form=%#v", runArgs, formArgs)
	}
	if runArgs["include_summary"] != true || formArgs["include_summary"] != true {
		t.Fatalf("boolean was not coerced into run/form args: run=%#v form=%#v", runArgs, formArgs)
	}
	if runArgs["format"] != "pdf" || runArgs["due_date"] != "2026-05-09" {
		t.Fatalf("select/date values not preserved: %#v", runArgs)
	}
}

func TestNormalizeSkillAgentViewSubmittedValuesRejectsInvalidValues(t *testing.T) {
	fields := []map[string]interface{}{
		{"name": "amount", "label": "Amount", "type": "number"},
		{"name": "format", "label": "Format", "type": "select", "options": []map[string]interface{}{
			{"value": "pdf", "label": "PDF"},
		}},
	}
	submitted := map[string]interface{}{"amount": "abc", "format": "exe"}
	runArgs := map[string]interface{}{}
	formArgs := map[string]interface{}{}

	issues := normalizeSkillAgentViewSubmittedValues(fields, submitted, runArgs, formArgs)
	if len(issues) != 2 {
		t.Fatalf("issues = %#v, want number and option errors", issues)
	}
	if _, ok := runArgs["amount"]; ok {
		t.Fatalf("invalid number should not be written to run args: %#v", runArgs)
	}
	if _, ok := runArgs["format"]; ok {
		t.Fatalf("invalid select should not be written to run args: %#v", runArgs)
	}
}

func TestSkillRunParameterContractRequiresOperationChoice(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "workflow-skill",
		Mode: "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "draft", Description: "Draft report", Labels: []string{"draft"}, Params: []string{"topic"}},
			{Name: "publish", Description: "Publish report", Labels: []string{"publish"}, Params: []string{"target"}},
		},
		Steps: []corelib.NLSkillStep{
			{Label: "draft", Action: "bash", Params: map[string]interface{}{"command": "echo {{topic}}"}},
			{Label: "publish", Action: "bash", Params: map[string]interface{}{"command": "echo {{target}}"}},
		},
	}

	params, missing := skillRunParameterContract(skill, nil, map[string]interface{}{})
	if len(missing) != 1 || missing[0] != "operation" {
		t.Fatalf("missing = %#v, want operation", missing)
	}

	view := buildSkillRunAgentView(*skill, map[string]interface{}{}, params, missing)
	fields := view["fields"].([]map[string]interface{})
	var operationField map[string]interface{}
	for _, field := range fields {
		if field["name"] == "operation" {
			operationField = field
			break
		}
	}
	if operationField == nil {
		t.Fatalf("operation field missing from %#v", fields)
	}
	if operationField["type"] != "select" || operationField["required"] != true {
		t.Fatalf("operation field should be required select, got %#v", operationField)
	}
	options, ok := operationField["options"].([]map[string]interface{})
	if !ok || len(options) != 2 {
		t.Fatalf("operation options = %#v, want two options", operationField["options"])
	}
	if options[0]["value"] != "draft" || options[1]["value"] != "publish" {
		t.Fatalf("unexpected operation options: %#v", options)
	}
}

func TestSkillRunParameterContractUsesSelectedOperationParams(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "workflow-skill",
		Mode: "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "draft", Labels: []string{"draft"}, Params: []string{"topic"}},
			{Name: "publish", Labels: []string{"publish"}, Params: []string{"target"}},
		},
		Steps: []corelib.NLSkillStep{
			{Label: "draft", Action: "bash", Params: map[string]interface{}{"command": "echo {{topic}}"}},
			{Label: "publish", Action: "bash", Params: map[string]interface{}{"command": "echo {{target}}"}},
		},
	}

	params, missing := skillRunParameterContract(skill, map[string]string{"operation": "draft"}, map[string]interface{}{
		"args": map[string]interface{}{"operation": "draft"},
	})
	if len(missing) != 1 || missing[0] != "topic" {
		t.Fatalf("missing = %#v, want only topic for selected draft operation", missing)
	}
	for _, param := range params {
		if param.Name == "target" {
			t.Fatalf("target param from unselected operation should not be required in selected draft form: %#v", params)
		}
	}
}

func TestHandleSkillRunAgentViewSubmitRevalidatesMissingOperation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "workflow-skill",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "draft", Labels: []string{"draft"}},
			{Name: "publish", Labels: []string{"publish"}},
		},
		Steps: []corelib.NLSkillStep{
			{Label: "draft", Action: "bash", Params: map[string]interface{}{"command": "echo draft"}},
			{Label: "publish", Action: "bash", Params: map[string]interface{}{"command": "echo publish"}},
		},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)

	resp := app.handleSkillRunAgentViewSubmit("workflow-skill", map[string]interface{}{
		"_run_args": map[string]interface{}{},
	})
	if resp == nil || resp.ResponseSource != "agent_view_submit" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if !strings.Contains(resp.Error, "operation") {
		t.Fatalf("response should mention missing operation, got %#v", resp)
	}
}

func TestHandleSkillRunAgentViewSubmitDoesNotInheritWorkflowPolicy(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	app := h.app
	app.testHomeDir = tempHome
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "writer-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo write"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	userID := desktopUserID
	if _, err := app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	resp := app.handleSkillRunAgentViewSubmit("writer-skill", map[string]interface{}{
		"_run_args": map[string]interface{}{},
	})
	if resp == nil {
		t.Fatal("expected skill response")
	}
	if strings.Contains(resp.Text, "not allowed by the current workflow tool policy") || strings.Contains(resp.Error, "not allowed by the current workflow tool policy") {
		t.Fatalf("manual agent-view skill submit must not inherit workflow policy, got %#v", resp)
	}
}

func TestBuildSkillRunStatusAgentViewShowsProgressWhenRunning(t *testing.T) {
	previousLang, _ := agentViewCurrentLang.Load().(string)
	setAgentViewLang("en")
	t.Cleanup(func() { setAgentViewLang(previousLang) })

	status := &SkillRunStatus{
		RunID:  "run-1",
		Skill:  "demo",
		Status: skillRunStatusRunning,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusRunning,
			Output: "working",
		}},
	}
	view := buildSkillRunStatusAgentView(status, "run-1")
	if view["type"] != "progress" {
		t.Fatalf("running status should render progress view, got %#v", view)
	}
	if view["title"] != "Running demo" {
		t.Fatalf("unexpected title: %#v", view["title"])
	}
	steps := view["steps"].([]map[string]interface{})
	if len(steps) != 1 || steps[0]["description"] != "working" {
		t.Fatalf("step output should be visible in progress view: %#v", steps)
	}
	if steps[0]["status"] != string(agentViewStepStatusRunning) {
		t.Fatalf("step status should stay typed as running in progress view: %#v", steps)
	}
	actions := view["actions"].([]map[string]interface{})
	if len(actions) != 1 || actions[0]["viewId"] != "skill:status" {
		t.Fatalf("progress view should include refresh action: %#v", actions)
	}
}

func TestBuildSkillRunStatusAgentViewShowsResultWhenFinished(t *testing.T) {
	status := &SkillRunStatus{
		RunID:      "run-1",
		Skill:      "demo",
		Status:     skillRunStatusSuccess,
		DurationMs: 123,
		Steps: []StepResult{{
			Index:      0,
			Action:     "bash",
			Status:     skillStepStatusSuccess,
			Output:     "final output",
			DurationMs: 123,
		}},
	}
	status.Summary.ArtifactPath = "out.pdf"
	status.Summary.ArtifactStatus = skillArtifactStatusVerified
	view := buildSkillRunStatusAgentView(status, "run-1")
	if view["type"] != "result_browser" {
		t.Fatalf("finished status should render result browser, got %#v", view)
	}
	results := view["results"].([]map[string]interface{})
	if len(results) != 2 {
		t.Fatalf("expected summary plus step result, got %#v", results)
	}
	summary := results[0]["data"].(map[string]interface{})
	if summary["artifact_path"] != "out.pdf" || summary["artifact_status"] != "verified" {
		t.Fatalf("artifact summary missing: %#v", summary)
	}
	stepData := results[1]["data"].(map[string]interface{})
	if stepData["output"] != "final output" {
		t.Fatalf("step output missing from result view: %#v", stepData)
	}
	actions := results[0]["actions"].([]map[string]interface{})
	if len(actions) != 1 || actions[0]["viewId"] != "skill:status" {
		t.Fatalf("result view should include refresh action: %#v", actions)
	}
}
