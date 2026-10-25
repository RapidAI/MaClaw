package skill

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type deliverableBashDeps struct {
	outputs  []string
	errs     []error
	calls    int
	statuses []string
}

func (d *deliverableBashDeps) ExecuteBash(ctx context.Context, command, workDir string, env map[string]string) (string, error) {
	i := d.calls
	d.calls++
	var out string
	var err error
	if i < len(d.outputs) {
		out = d.outputs[i]
	}
	if i < len(d.errs) {
		err = d.errs[i]
	}
	return out, err
}

func (d *deliverableBashDeps) OnStepProgress(stepIndex, totalSteps int, stepAction, status string) {
	d.statuses = append(d.statuses, status)
}

func TestExecuteStepsSyncContinueAfterFailureIsIncomplete(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "demo", Steps: []corelib.NLSkillStep{{
		Action:  "bash",
		OnError: "continue",
		Params:  map[string]interface{}{"command": "exit 1"},
	}}}
	deps := &deliverableBashDeps{outputs: []string{"boom"}, errs: []error{errors.New("exit status 1")}}
	result, err := ExecuteStepsSync(context.Background(), entry, nil, ExecConfig{}, deps)
	if err == nil || !strings.Contains(err.Error(), "skill run incomplete") {
		t.Fatalf("err=%v", err)
	}
	if result == nil || strings.Contains(result.Output, "executed successfully") {
		t.Fatalf("output=%q", result.Output)
	}
	if result.StepsFailed != 1 || len(result.FailedStepNumbers) != 1 || result.FailedStepNumbers[0] != 1 {
		t.Fatalf("failed=%d numbers=%v", result.StepsFailed, result.FailedStepNumbers)
	}
}

func TestExecuteStepsSyncEmptyDeclaredCaptureIsIncomplete(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "demo", Steps: []corelib.NLSkillStep{{
		Action:  "bash",
		Params:  map[string]interface{}{"command": "echo hello"},
		Capture: map[string]string{"report_path": `PATH=(.+)`},
	}}}
	deps := &deliverableBashDeps{outputs: []string{"hello"}}
	result, err := ExecuteStepsSync(context.Background(), entry, nil, ExecConfig{}, deps)
	if err == nil || !strings.Contains(err.Error(), "declared capture empty: report_path") {
		t.Fatalf("err=%v", err)
	}
	if result == nil || strings.Contains(result.Output, "executed successfully") {
		t.Fatalf("output=%q", result.Output)
	}
}

func TestExecuteStepsSyncEmptyOutputWithoutCaptureStaysSuccess(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "demo", Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "true"},
	}}}
	deps := &deliverableBashDeps{outputs: []string{""}}
	result, err := ExecuteStepsSync(context.Background(), entry, nil, ExecConfig{}, deps)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result == nil || result.Output != "Skill executed successfully (no output)" {
		t.Fatalf("output=%q", result.Output)
	}
}

func TestExecuteStepsSyncUnsupportedCaptureIsIncomplete(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "demo", Steps: []corelib.NLSkillStep{{
		Action:  "craft_tool",
		Capture: map[string]string{"out": "(.+)"},
	}}}
	deps := &deliverableBashDeps{}
	result, err := ExecuteStepsSync(context.Background(), entry, nil, ExecConfig{}, deps)
	if err == nil || !strings.Contains(err.Error(), "declared capture empty: out") {
		t.Fatalf("err=%v", err)
	}
	if result == nil || strings.Contains(result.Output, "executed successfully") {
		t.Fatalf("output=%q", result.Output)
	}
	if deps.calls != 0 {
		t.Fatalf("unsupported step ran bash %d times", deps.calls)
	}
}

func TestExecuteStepsSyncLaterStepFillsCapture(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "demo", Steps: []corelib.NLSkillStep{
		{
			Action:  "bash",
			Params:  map[string]interface{}{"command": "echo no"},
			Capture: map[string]string{"report_path": `PATH=(.+)`},
		},
		{
			Action:  "bash",
			Params:  map[string]interface{}{"command": "echo PATH=ok"},
			Capture: map[string]string{"report_path": `PATH=(.+)`},
		},
	}}
	deps := &deliverableBashDeps{outputs: []string{"no", "PATH=ok"}}
	result, err := ExecuteStepsSync(context.Background(), entry, nil, ExecConfig{}, deps)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result == nil || len(result.UnassignedCaptures) != 0 {
		t.Fatalf("still missing %#v", result.UnassignedCaptures)
	}
	if result.Vars["report_path"] != "ok" {
		t.Fatalf("vars=%v", result.Vars)
	}
}

func TestExecuteStepsSyncEmptyCommandCaptureIsIncomplete(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "demo", Steps: []corelib.NLSkillStep{{
		Action:  "bash",
		Params:  map[string]interface{}{"command": ""},
		Capture: map[string]string{"report_path": `PATH=(.+)`},
	}}}
	deps := &deliverableBashDeps{}
	_, err := ExecuteStepsSync(context.Background(), entry, nil, ExecConfig{}, deps)
	if err == nil || !strings.Contains(err.Error(), "declared capture empty: report_path") {
		t.Fatalf("err=%v", err)
	}
	if deps.calls != 0 {
		t.Fatalf("empty command ran bash")
	}
}
