package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveSelectedStepLabelsOperationOverridesSteps(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "workflow",
		Mode: "api_workflow",
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"build", "verify"},
		}},
		Steps: []corelib.NLSkillStep{
			{Label: "build", Params: map[string]interface{}{"command": "echo build"}},
			{Label: "verify", Params: map[string]interface{}{"command": "echo verify"}},
		},
	}

	got, err := ResolveSelectedStepLabels(entry, map[string]interface{}{
		"steps":     "danger",
		"operation": "safe",
	})

	if err != nil {
		t.Fatalf("ResolveSelectedStepLabels() error = %v", err)
	}
	if len(got) != 2 || got[0] != "build" || got[1] != "verify" {
		t.Fatalf("selected labels = %#v, want operation labels", got)
	}
}

func TestResolveSelectedStepLabelsReadsOperationFromNestedArgs(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "workflow",
		Mode: "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "safe", Labels: []string{"safe-step"}},
			{Name: "danger", Labels: []string{"danger-step"}},
		},
		Steps: []corelib.NLSkillStep{
			{Label: "safe-step", Params: map[string]interface{}{"command": "echo safe"}},
			{Label: "danger-step", Params: map[string]interface{}{"command": "echo danger"}},
		},
	}

	got, err := ResolveSelectedStepLabels(entry, map[string]interface{}{
		"args": map[string]interface{}{"operation": "safe"},
	})

	if err != nil {
		t.Fatalf("ResolveSelectedStepLabels() error = %v", err)
	}
	if len(got) != 1 || got[0] != "safe-step" {
		t.Fatalf("selected labels = %#v, want nested operation label", got)
	}
}

func TestResolveSelectedStepLabelsReadsStepsFromJSONArgs(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "workflow",
		Mode: "api_workflow",
		Steps: []corelib.NLSkillStep{
			{Label: "safe-step", Params: map[string]interface{}{"command": "echo safe"}},
			{Label: "danger-step", Params: map[string]interface{}{"command": "echo danger"}},
		},
	}

	got, err := ResolveSelectedStepLabels(entry, map[string]interface{}{
		"args": `{"steps":"safe-step"}`,
	})

	if err != nil {
		t.Fatalf("ResolveSelectedStepLabels() error = %v", err)
	}
	if len(got) != 1 || got[0] != "safe-step" {
		t.Fatalf("selected labels = %#v, want JSON args step selector", got)
	}
}

func TestResolveSelectedStepLabelsRejectsUnknownOperation(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:       "workflow",
		Mode:       "api_workflow",
		Operations: []corelib.NLSkillOperation{{Name: "safe"}},
	}

	_, err := ResolveSelectedStepLabels(entry, map[string]interface{}{"operation": "missing"})

	if err == nil || !strings.Contains(err.Error(), `operation "missing" not found`) || !strings.Contains(err.Error(), "safe") || !strings.Contains(err.Error(), "choose_operation") {
		t.Fatalf("expected unknown operation error with available names, got %v", err)
	}
}

func TestResolveSelectedStepLabelsDefaultsSingleOperation(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "workflow",
		Mode: "api_workflow",
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"build", "verify"},
		}},
		Steps: []corelib.NLSkillStep{
			{Label: "build", Params: map[string]interface{}{"command": "echo build"}},
			{Label: "verify", Params: map[string]interface{}{"command": "echo verify"}},
		},
	}

	got, err := ResolveSelectedStepLabels(entry, nil)

	if err != nil {
		t.Fatalf("ResolveSelectedStepLabels() error = %v", err)
	}
	if len(got) != 2 || got[0] != "build" || got[1] != "verify" {
		t.Fatalf("selected labels = %#v, want single operation labels", got)
	}
}

func TestResolveSelectedStepLabelsRequiresOperationForMultipleOperations(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "workflow",
		Mode: "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "safe", Labels: []string{"safe-step"}},
			{Name: "danger", Labels: []string{"danger-step"}},
		},
	}

	_, err := ResolveSelectedStepLabels(entry, nil)

	if err == nil || !strings.Contains(err.Error(), "requires an operation") || !strings.Contains(err.Error(), "safe") || !strings.Contains(err.Error(), "[action: choose_operation]") {
		t.Fatalf("expected choose operation error, got %v", err)
	}
}

func TestResolveSelectedStepLabelsRejectsOperationWithoutLabels(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:       "workflow",
		Mode:       "api_workflow",
		Operations: []corelib.NLSkillOperation{{Name: "empty"}},
	}

	_, err := ResolveSelectedStepLabels(entry, map[string]interface{}{"operation": "empty"})

	if err == nil || !strings.Contains(err.Error(), "has no step labels") || !strings.Contains(err.Error(), "[action: inspect_skill]") {
		t.Fatalf("expected inspect operation error, got %v", err)
	}
}

func TestResolveSelectedStepLabelsRejectsWorkflowWithoutOperationLabels(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:       "workflow",
		Mode:       "api_workflow",
		Operations: []corelib.NLSkillOperation{{Name: "empty"}},
	}

	_, err := ResolveSelectedStepLabels(entry, nil)

	if err == nil || !strings.Contains(err.Error(), "operations have no step labels") || !strings.Contains(err.Error(), "[action: inspect_skill]") {
		t.Fatalf("expected inspect workflow error, got %v", err)
	}
}

func TestResolveSelectedStepLabelsRejectsUnknownStepLabel(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "workflow",
		Mode: "api_workflow",
		Steps: []corelib.NLSkillStep{
			{Label: "build", Params: map[string]interface{}{"command": "echo build"}},
		},
	}

	_, err := ResolveSelectedStepLabels(entry, map[string]interface{}{"steps": "build,missing"})

	if err == nil || !strings.Contains(err.Error(), "unknown step label") || !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "build") || !strings.Contains(err.Error(), "[action: inspect_skill]") {
		t.Fatalf("expected unknown step label error, got %v", err)
	}
}

func TestResolveSelectedStepLabelsRejectsOperationWithUnknownStepLabel(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "workflow",
		Mode: "api_workflow",
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step", "missing-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
		},
	}

	_, err := ResolveSelectedStepLabels(entry, map[string]interface{}{"operation": "safe"})

	if err == nil || !strings.Contains(err.Error(), "missing-step") || !strings.Contains(err.Error(), "safe-step") || !strings.Contains(err.Error(), "[action: inspect_skill]") {
		t.Fatalf("expected operation unknown step label error, got %v", err)
	}
}

func TestSelectedExecutableStepsSkipsUnselectedAndUnlabeled(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Label: "build", Params: map[string]interface{}{"command": "echo build"}},
		{Label: "danger", Params: map[string]interface{}{"command": "danger"}},
		{Params: map[string]interface{}{"command": "unlabeled"}},
	}

	got := SelectedExecutableSteps(steps, []string{"build"})

	if len(got) != 1 || got[0].Label != "build" {
		t.Fatalf("SelectedExecutableSteps() = %#v, want only build", got)
	}
}

func TestSelectedExecutableStepsTrimsStepLabels(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Label: " build ", Params: map[string]interface{}{"command": "echo build"}},
		{Label: " ", Params: map[string]interface{}{"command": "echo blank"}},
	}

	got := SelectedExecutableSteps(steps, []string{"build"})

	if len(got) != 1 || strings.TrimSpace(got[0].Label) != "build" {
		t.Fatalf("SelectedExecutableSteps() = %#v, want trimmed build label selected", got)
	}
	if !StepLabelSelected(" build ", []string{"build"}) {
		t.Fatal("StepLabelSelected should trim step label before matching")
	}
}

func TestPrecheckExecutableStepsSkipsKnownFalseWhen(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo base"}},
		{Action: "bash", When: "{{mode}} == advanced", Params: map[string]interface{}{"command": "python missing.py {{extra_input}}"}},
	}

	got := PrecheckExecutableSteps(steps, map[string]string{"mode": "basic"})

	if len(got) != 1 || got[0].Params["command"] != "echo base" {
		t.Fatalf("PrecheckExecutableSteps() = %#v, want only base step", got)
	}
}

func TestPrecheckExecutableStepsSkipsMissingControlParam(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", When: "{{include_extra}}", Params: map[string]interface{}{"command": "python optional.py"}},
	}

	got := PrecheckExecutableSteps(steps, nil)

	if len(got) != 0 {
		t.Fatalf("PrecheckExecutableSteps() = %#v, want missing control-param step skipped", got)
	}
}

func TestPrecheckExecutableStepsKeepsWhenDependingOnPriorCapture(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo mode=advanced"}, Capture: map[string]string{"mode": `mode=(\w+)`}},
		{Action: "bash", When: "{{mode}} == advanced", Params: map[string]interface{}{"command": "python followup.py"}},
	}

	got := PrecheckExecutableSteps(steps, nil)

	if len(got) != 2 {
		t.Fatalf("PrecheckExecutableSteps() = %#v, want capture-dependent step kept", got)
	}
}

func TestPrecheckExecutableStepsKeepsWhenDependingOnPriorCaptureAlias(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo file=report.md"}, Capture: map[string]string{"file": `file=(.+)`}},
		{Action: "bash", When: "{{input}} == report.md", Params: map[string]interface{}{"command": "python followup.py"}},
	}

	got := PrecheckExecutableSteps(steps, nil)

	if len(got) != 2 {
		t.Fatalf("PrecheckExecutableSteps() = %#v, want alias capture-dependent step kept", got)
	}
}

func TestDetectImplicitRunRequiredArgsAllowsPriorCapture(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo status=READY"}, Capture: map[string]string{"status": `status=READY`}},
		{Action: "bash", Params: map[string]interface{}{"command": "echo {{status}}"}},
	}

	if missing := DetectImplicitRunRequiredArgs(steps, nil, nil, nil); len(missing) != 0 {
		t.Fatalf("missing = %#v, want prior capture to satisfy status placeholder", missing)
	}
}

func TestDetectImplicitRunRequiredArgsAllowsPriorCaptureAlias(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo file=report.md"}, Capture: map[string]string{"file": `file=(.+)`}},
		{Action: "bash", Params: map[string]interface{}{"command": "echo {{input}}"}},
	}

	if missing := DetectImplicitRunRequiredArgs(steps, nil, nil, nil); len(missing) != 0 {
		t.Fatalf("missing = %#v, want prior capture alias to satisfy input placeholder", missing)
	}
}

func TestRequiredArgsForRunnerPrecheckAllowsPriorCapture(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo status=READY"}, Capture: map[string]string{"status": `status=READY`}},
		{Action: "bash", Params: map[string]interface{}{"command": "echo {{status}} {{output}}"}},
	}

	got := RequiredArgsForRunnerPrecheck([]string{"status", "output"}, steps)

	if len(got) != 1 || got[0] != "output" {
		t.Fatalf("RequiredArgsForRunnerPrecheck() = %#v, want only externally required output", got)
	}
}

func TestRequiredArgsForRunnerPrecheckAllowsPriorCaptureAlias(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo file=report.md"}, Capture: map[string]string{"file": `file=(.+)`}},
		{Action: "bash", Params: map[string]interface{}{"command": "echo {{input}} {{output}}"}},
	}

	got := RequiredArgsForRunnerPrecheck([]string{"input", "output"}, steps)

	if len(got) != 1 || got[0] != "output" {
		t.Fatalf("RequiredArgsForRunnerPrecheck() = %#v, want only externally required output", got)
	}
}

func TestPrepareRunnerExecutionAllowsRequiredArgFromPriorCapture(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:         "capture-required",
		RequiredArgs: []string{"status"},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo status=READY"}, Capture: map[string]string{"status": `status=(\w+)`}},
			{Action: "bash", Params: map[string]interface{}{"command": "echo {{status}}"}},
		},
	}

	if _, err := PrepareRunnerExecution(entry, nil, nil, nil, RunnerBackendTUI); err != nil {
		t.Fatalf("PrepareRunnerExecution() error = %v; prior capture should satisfy required status", err)
	}
}

func TestPrepareRunnerExecutionAllowsRequiredArgFromPriorCaptureAlias(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:         "capture-required-alias",
		RequiredArgs: []string{"input"},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo file=report.md"}, Capture: map[string]string{"file": `file=(.+)`}},
			{Action: "bash", Params: map[string]interface{}{"command": "echo {{input}}"}},
		},
	}

	if _, err := PrepareRunnerExecution(entry, nil, nil, nil, RunnerBackendTUI); err != nil {
		t.Fatalf("PrepareRunnerExecution() error = %v; prior capture alias should satisfy required input", err)
	}
}

func TestCaptureOutputVariablesUsesFullMatchWhenNoGroup(t *testing.T) {
	got := CaptureOutputVariables("status=READY token=abc", map[string]string{
		"status": `status=READY`,
		"token":  `token=(\w+)`,
		"bad":    `(`,
	})

	if got["status"] != "status=READY" || got["token"] != "abc" {
		t.Fatalf("CaptureOutputVariables() = %#v, want full match and first submatch", got)
	}
	if _, ok := got["bad"]; ok {
		t.Fatalf("CaptureOutputVariables() = %#v, invalid regex should be ignored", got)
	}
}

func TestCaptureOutputVariablesMirrorsCommonAliases(t *testing.T) {
	got := CaptureOutputVariables("file=report.md", map[string]string{
		"file": `file=(.+)`,
	})

	if got["file"] != "report.md" || got["input"] != "report.md" {
		t.Fatalf("CaptureOutputVariables() = %#v, want capture alias mirrored to input", got)
	}
}

func TestCaptureOutputVariablesTrimsBoundaryWhitespace(t *testing.T) {
	got := CaptureOutputVariables("file=report.md \r\n", map[string]string{
		"file": `file=(.+)`,
	})

	if got["file"] != "report.md" || got["input"] != "report.md" {
		t.Fatalf("CaptureOutputVariables() = %#v, want whitespace-trimmed captured value and aliases", got)
	}
}

func TestResolveStepUsesCapturedAliasForPlaceholder(t *testing.T) {
	vars := CaptureOutputVariables("file=report.md", map[string]string{
		"file": `file=(.+)`,
	})
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "echo {{input}}",
		},
	}

	resolved, err := ResolveStep(step, vars, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := resolved.Step.Params["command"].(string)
	if cmd != "echo report.md" {
		t.Fatalf("resolved command = %q, want captured file alias to satisfy input placeholder", cmd)
	}
}

func TestPrepareRunnerExecutionInfersInputAndCompletesParams(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:         "weather",
		RequiredArgs: []string{"city"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{city}}"},
		}},
	}
	vars := NormalizeRunVars(map[string]interface{}{"input": "weather in Chengdu"})

	prep, err := PrepareRunnerExecution(entry, vars, map[string]interface{}{"input": "weather in Chengdu"}, nil, RunnerBackendTUI)
	if err != nil {
		t.Fatalf("PrepareRunnerExecution() error = %v", err)
	}
	if vars["city"] != "Chengdu" {
		t.Fatalf("vars[city] = %q, want Chengdu", vars["city"])
	}
	if len(prep.Params) == 0 || entry.Params[0].Name != "city" {
		t.Fatalf("params = %#v, entry.Params=%#v; want completed city schema", prep.Params, entry.Params)
	}
	if len(prep.ResolvedPrecheckSteps) != 1 {
		t.Fatalf("ResolvedPrecheckSteps = %#v, want one step", prep.ResolvedPrecheckSteps)
	}
}

func TestPrepareRunnerExecutionRejectsUnsupportedRunnerAction(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "gui-only",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"task": "make a thing"},
		}},
	}

	_, err := PrepareRunnerExecution(entry, nil, nil, nil, RunnerBackendTUI)
	if err == nil {
		t.Fatal("expected unsupported action error")
	}
	if !strings.Contains(err.Error(), "unsupported_step_action") || !strings.Contains(err.Error(), "open_gui") {
		t.Fatalf("err = %v, want unsupported action with open_gui hint", err)
	}
}

func TestPreparePipelineRunnerExecutionChecksRequiredEnv(t *testing.T) {
	orig := envLookup
	defer func() { envLookup = orig }()
	envLookup = func(string) string { return "" }

	entry := &corelib.NLSkillEntry{
		Name:        "pipeline-env",
		RequiredEnv: []string{"API_TOKEN"},
		Pipeline:    []corelib.SkillPipelineStep{{Skill: "child"}},
	}
	_, err := PreparePipelineRunnerExecution(entry, nil, nil, nil, RunnerBackendTUI)
	if err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("PreparePipelineRunnerExecution() error = %v, want missing API_TOKEN", err)
	}

	_, err = PreparePipelineRunnerExecution(entry, nil, nil, map[string]string{"API_TOKEN": "secret"}, RunnerBackendTUI)
	if err != nil {
		t.Fatalf("PreparePipelineRunnerExecution() with run env error = %v", err)
	}
}

func TestPreparePipelineRunnerExecutionChecksTrustedStackBeforeRequirements(t *testing.T) {
	orig := envLookup
	defer func() { envLookup = orig }()
	envLookup = func(string) string { return "" }

	entry := &corelib.NLSkillEntry{
		Name:        "pipeline-self-env",
		RequiredEnv: []string{"API_TOKEN"},
		Pipeline:    []corelib.SkillPipelineStep{{Skill: "pipeline-self-env"}},
	}
	runArgs := WithPipelineRunStack(map[string]interface{}{}, "pipeline-self-env")
	_, err := PreparePipelineRunnerExecution(entry, nil, runArgs, nil, RunnerBackendTUI)
	if err == nil {
		t.Fatal("PreparePipelineRunnerExecution() error = nil, want recursion")
	}
	if !strings.Contains(err.Error(), "pipeline recursion detected") || strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("PreparePipelineRunnerExecution() error = %v, want recursion before required env", err)
	}
}

func TestPreparePipelineRunnerExecutionIgnoresUntrustedStack(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:     "pipeline-stack",
		Pipeline: []corelib.SkillPipelineStep{{Skill: "child"}},
	}
	runArgs := map[string]interface{}{PipelineRunStackArg: []string{"pipeline-stack"}}
	if _, err := PreparePipelineRunnerExecution(entry, nil, runArgs, nil, RunnerBackendTUI); err != nil {
		t.Fatalf("PreparePipelineRunnerExecution() error = %v; untrusted stack must be ignored", err)
	}
}

func TestPrepareRunnerExecutionSkipsUnsupportedActionWhenInactive(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "conditional-gui-only",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			When:   "{{mode}} == advanced",
			Params: map[string]interface{}{"task": "make a thing"},
		}},
	}

	prep, err := PrepareRunnerExecution(entry, map[string]string{"mode": "basic"}, nil, nil, RunnerBackendTUI)
	if err != nil {
		t.Fatalf("PrepareRunnerExecution() error = %v; inactive unsupported step should not block", err)
	}
	if len(prep.PrecheckSteps) != 0 || len(prep.ResolvedPrecheckSteps) != 0 {
		t.Fatalf("precheck steps = %#v resolved=%#v, want inactive step skipped", prep.PrecheckSteps, prep.ResolvedPrecheckSteps)
	}
}

func TestEvaluateStepWhenStripsMissingPlaceholder(t *testing.T) {
	if EvaluateStepWhen("{{enabled}}", nil) {
		t.Fatal("missing when placeholder should evaluate false")
	}
}

func TestEvaluateStepWhenTreatsMissingComparisonAsFalse(t *testing.T) {
	if EvaluateStepWhen("{{mode}} != skip", nil) {
		t.Fatal("missing comparison placeholder should evaluate false")
	}
}

func TestEvaluateStepWhenCanonicalizesPlaceholders(t *testing.T) {
	if !EvaluateStepWhen("{{Run-Mode}} == build", map[string]string{"run_mode": "build"}) {
		t.Fatal("canonical placeholder should satisfy when expression")
	}
}

func TestEvaluateStepWhenTrimsQuotedComparisonOperands(t *testing.T) {
	vars := map[string]string{"mode": "advanced", "text": "weather report"}
	if !EvaluateStepWhen(`{{mode}} == "advanced"`, vars) {
		t.Fatal("quoted comparison operand should match substituted value")
	}
	if !EvaluateStepWhen(`{{text}} contains "report"`, vars) {
		t.Fatal("quoted contains operand should match substring")
	}
	if !EvaluateStepWhen(`"true"`, nil) {
		t.Fatal("quoted boolean literal should evaluate true")
	}
}

func TestEvaluateStepWhenHonorsCommonAliases(t *testing.T) {
	if !EvaluateStepWhen("{{input}} == report.md", map[string]string{"file": "report.md"}) {
		t.Fatal("common alias should satisfy when expression")
	}
}

func TestEvaluateSimpleConditionContains(t *testing.T) {
	if !EvaluateSimpleCondition("weather report contains report") {
		t.Fatal("contains condition should evaluate true")
	}
	if EvaluateSimpleCondition("weather report contains finance") {
		t.Fatal("contains condition should evaluate false when substring is absent")
	}
}
