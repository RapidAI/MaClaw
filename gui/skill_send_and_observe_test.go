package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestSkillRunnerExecuteStepWithContext_SendAndObserveUsesSharedHelper(t *testing.T) {
	t.Skip("legacy external send_and_observe is disabled; coding tasks use CodingSubAgent")
	session := &RemoteSession{
		ID:        "sess-observe-1",
		Tool:      "claude-code",
		Title:     "observe-session",
		Status:    SessionBusy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(500),
		RawOutputLines: []string{
			"❯ do work",
		},
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		session.mu.Lock()
		session.Status = SessionWaitingInput
		session.Summary.WaitingForUser = true
		session.RawOutputLines = append(session.RawOutputLines, "done")
		session.mu.Unlock()
	}()

	app := &App{}
	mgr := &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{"sess-observe-1": session}}
	runner := NewSkillRunner(&SkillExecutor{app: app, manager: mgr})

	result, err := runner.executeStepWithContext(context.Background(), "run-observe", corelib.NLSkillStep{
		Action: "send_and_observe",
		Params: map[string]interface{}{
			"session_id": "sess-observe-1",
			"text":       "do work",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext(send_and_observe) error = %v", err)
	}
	if !strings.Contains(result, "会话 sess-observe-1 状态") {
		t.Fatalf("expected formatted session output, got %s", result)
	}
	if !strings.Contains(result, "等待用户输入") && !strings.Contains(result, "done") {
		t.Fatalf("expected observed progress in result, got %s", result)
	}
}

func TestSkillRunnerExecuteStepWithContext_SendAndObserveUsesImplicitSessionID(t *testing.T) {
	t.Skip("legacy external send_and_observe is disabled; coding tasks use CodingSubAgent")
	session := &RemoteSession{
		ID:        "sess-observe-implicit-1",
		Tool:      "claude-code",
		Title:     "observe-session-implicit",
		Status:    SessionWaitingInput,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(502),
		RawOutputLines: []string{
			"done",
		},
		Summary: SessionSummary{WaitingForUser: true},
	}
	app := &App{}
	mgr := &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{"sess-observe-implicit-1": session}}
	runner := NewSkillRunner(&SkillExecutor{app: app, manager: mgr})
	runner.runs["run-implicit"] = &skillRun{status: SkillRunStatus{
		RunID:  "run-implicit",
		Skill:  "demo",
		Status: "running",
		Session: &SkillRunSessionMeta{
			SessionID: "sess-observe-implicit-1",
		},
	}}

	result, err := runner.executeStepWithContext(context.Background(), "run-implicit", corelib.NLSkillStep{
		Action: "send_and_observe",
		Params: map[string]interface{}{
			"text": "continue",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext(send_and_observe implicit) error = %v", err)
	}
	if !strings.Contains(result, "会话 sess-observe-implicit-1 状态") {
		t.Fatalf("expected implicit session reuse, got %s", result)
	}
}

func TestSkillRunnerExecuteStepWithContext_SendAndObserveFallsBackFromUnknownExplicitSessionID(t *testing.T) {
	t.Skip("legacy external send_and_observe is disabled; coding tasks use CodingSubAgent")
	session := &RemoteSession{
		ID:        "sess-observe-fallback-1",
		Tool:      "claude-code",
		Title:     "observe-session-fallback",
		Status:    SessionWaitingInput,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(503),
		RawOutputLines: []string{
			"done",
		},
		Summary: SessionSummary{WaitingForUser: true},
	}
	app := &App{}
	mgr := &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{"sess-observe-fallback-1": session}}
	runner := NewSkillRunner(&SkillExecutor{app: app, manager: mgr})
	runner.runs["run-fallback"] = &skillRun{status: SkillRunStatus{
		RunID:  "run-fallback",
		Skill:  "demo",
		Status: "running",
		Session: &SkillRunSessionMeta{
			SessionID: "sess-observe-fallback-1",
		},
	}}

	result, err := runner.executeStepWithContext(context.Background(), "run-fallback", corelib.NLSkillStep{
		Action: "send_and_observe",
		Params: map[string]interface{}{
			"session_id": "skill-runner",
			"text":       "continue",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext(send_and_observe fallback) error = %v", err)
	}
	if !strings.Contains(result, "会话 sess-observe-fallback-1 状态") {
		t.Fatalf("expected fallback session reuse, got %s", result)
	}
}

func TestSkillExecutorExecuteSkillSteps_FailsWhenOpenAIProxyConfigMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")

	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillSteps(&corelib.NLSkillEntry{
		Name:        "needs-openai",
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo should-not-run",
				},
			},
		},
	})
	if err == nil {
		t.Fatalf("executeSkillSteps() error = nil, output = %s", output)
	}
	if !strings.Contains(err.Error(), "[action: configure_llm]") {
		t.Fatalf("expected configure_llm action, got %v", err)
	}
	if strings.Contains(output, "should-not-run") {
		t.Fatalf("step ran despite missing proxy config, output = %s", output)
	}
}

func TestSkillExecutorExecuteSkillSteps_RejectsMissingArgsBeforeCommand(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillSteps(&corelib.NLSkillEntry{
		Name:         "weather",
		RequiredArgs: []string{"city"},
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo {{city}}",
				},
			},
		},
	})
	if err == nil {
		t.Fatalf("executeSkillSteps() error = nil, output = %s", output)
	}
	if !strings.Contains(err.Error(), "[action: provide_args]") {
		t.Fatalf("expected provide_args action, got %v", err)
	}
	if strings.Contains(output, "{{city}}") {
		t.Fatalf("command ran with unresolved placeholder, output = %s", output)
	}
}

func TestSkillExecutorExecuteSkillSteps_UsesSharedStepResolverDefaults(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillSteps(&corelib.NLSkillEntry{
		Name: "defaults",
		Params: []corelib.NLSkillParam{
			{Name: "name", Default: "world"},
		},
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo hello {{name}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSkillSteps() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "hello") || !strings.Contains(output, "world") {
		t.Fatalf("expected default param substitution, got %s", output)
	}
	if strings.Contains(output, "{{name}}") {
		t.Fatalf("unresolved default placeholder leaked into output: %s", output)
	}
}

func TestSkillExecutorExecuteSkillStepsWithArgs_FillsRequiredCityFromUserPrompt(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillStepsWithArgs(&corelib.NLSkillEntry{
		Name:         "weather",
		RequiredArgs: []string{"city"},
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo {{city}}",
				},
			},
		},
	}, skillExecutionRunArgs("weather Chengdu"))
	if err != nil {
		t.Fatalf("executeSkillStepsWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "Chengdu") {
		t.Fatalf("expected city inferred from user prompt, got %s", output)
	}
}

func TestSkillExecutorExecuteSkillStepsWithArgs_MergesExtraEnvIntoBash(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	command := "echo $API_TOKEN"
	if runtime.GOOS == "windows" {
		command = "echo %API_TOKEN%"
	}
	output, err := exec.executeSkillStepsWithArgs(&corelib.NLSkillEntry{
		Name: "env",
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": command,
				},
			},
		},
	}, map[string]interface{}{
		"extra_env": map[string]interface{}{
			"API_TOKEN": "secret-from-run",
		},
	})
	if err != nil {
		t.Fatalf("executeSkillStepsWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "secret-from-run") {
		t.Fatalf("expected extra_env to reach bash step, got %s", output)
	}
}

func TestSkillExecutorExecuteSkillStepsWithArgs_MergesNestedArgsExtraEnvIntoBash(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	command := "echo $API_TOKEN"
	if runtime.GOOS == "windows" {
		command = "echo %API_TOKEN%"
	}
	output, err := exec.executeSkillStepsWithArgs(&corelib.NLSkillEntry{
		Name: "nested-env",
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": command,
				},
			},
		},
	}, map[string]interface{}{
		"args": map[string]interface{}{
			"extra_env": map[string]interface{}{
				"API_TOKEN": "secret-from-nested-args",
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSkillStepsWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "secret-from-nested-args") {
		t.Fatalf("expected nested args extra_env to reach bash step, got %s", output)
	}
	if strings.Contains(output, "{{extra_env}}") || strings.Contains(output, "extra_env") {
		t.Fatalf("nested control key leaked as skill parameter, output = %s", output)
	}
}

func TestSkillExecutorExecuteSkillStepsWithArgs_MergesExtraEnvIntoCraftTool(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{"task": "print token"},
	}
	vars := cskill.NormalizeRunVars(nil)
	extraEnv := cskill.ExtractRunExtraEnvFromArgs(map[string]interface{}{
		"extra_env": map[string]interface{}{"API_TOKEN": "secret-from-run"},
	})
	resolved, err := resolveSkillStep(step, vars, "", nil)
	if err != nil {
		t.Fatalf("resolveSkillStep() error = %v", err)
	}
	if resolved.Action == "craft_tool" && len(extraEnv) > 0 {
		cskill.MergeExtraEnvParam(resolved.Params, extraEnv)
	}
	restore := installSkillStepProcessEnv(resolved.Action, extraEnv)
	defer restore()

	request := buildCraftToolRequest(resolved.Params, craftRuntimeAvailability{Python: "python"})
	if request.ExtraEnv["API_TOKEN"] != "secret-from-run" {
		t.Fatalf("craft ExtraEnv = %#v, want run env", request.ExtraEnv)
	}
	if got := os.Getenv("API_TOKEN"); got == "secret-from-run" {
		t.Fatal("craft_tool env should flow through params, not process env overlay")
	}
}

func TestSkillExecutorCaptureUsesSharedFullMatchSemantics(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillStepsWithArgs(&corelib.NLSkillEntry{
		Name: "capture-shared",
		Steps: []corelib.NLSkillStep{
			{
				Action:  "bash",
				Params:  map[string]interface{}{"command": "echo status=READY"},
				Capture: map[string]string{"status": `status=READY`},
			},
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo captured={{status}}",
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("executeSkillStepsWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "captured=\"status=READY\"") && !strings.Contains(output, "captured=status=READY") {
		t.Fatalf("output = %s, want full-match captured value", output)
	}
}

func TestSkillExecutorExecuteWithArgs_IncludesRunnerWarnings(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helper.py"), []byte{0xff, 0xfe, '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:     "warning-skill",
		Status:   "active",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "echo helper.py",
			},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("warning-skill", nil)
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "[Warning]") || !strings.Contains(output, "not valid UTF-8") || !strings.Contains(output, "helper.py") {
		t.Fatalf("ExecuteWithArgs() output = %q, want visible runner file warning", output)
	}
}

func TestSkillExecutorExecuteWithArgs_ExecutesPipelineSkillAndPropagatesExtraEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	t.Setenv("API_TOKEN", "")

	command := "echo $API_TOKEN {{input}}"
	if runtime.GOOS == "windows" {
		command = "echo %API_TOKEN% {{input}}"
	}
	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:        "child-env",
		Status:      "active",
		RequiredEnv: []string{"API_TOKEN"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": command},
		}},
	}); err != nil {
		t.Fatalf("Register(child) error = %v", err)
	}
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "pipeline-env",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill:  "child-env",
			Params: map[string]string{"input": "{{input}}"},
		}},
	}); err != nil {
		t.Fatalf("Register(pipeline) error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("pipeline-env", map[string]interface{}{
		"input":     "pipeline-ok",
		"Extra Env": map[string]interface{}{"API_TOKEN": "secret-from-parent"},
	})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "secret-from-parent") || !strings.Contains(output, "pipeline-ok") {
		t.Fatalf("pipeline output = %s, want child output with propagated env and input", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelinePropagatesNestedJSONArgsContext(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	t.Setenv("API_TOKEN", "")

	command := "echo $API_TOKEN {{input}}"
	if runtime.GOOS == "windows" {
		command = "echo %API_TOKEN% {{input}}"
	}
	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:         "child-json-context",
		Status:       "active",
		RequiredArgs: []string{"input"},
		RequiredEnv:  []string{"API_TOKEN"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": command},
		}},
	}); err != nil {
		t.Fatalf("Register(child) error = %v", err)
	}
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "pipeline-json-context",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "child-json-context",
		}},
	}); err != nil {
		t.Fatalf("Register(pipeline) error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("pipeline-json-context", map[string]interface{}{
		"args": `{"input":"json-pipeline-ok","extra_env":{"API_TOKEN":"secret-from-json-parent"},"name":"forged-child"}`,
	})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "secret-from-json-parent") || !strings.Contains(output, "json-pipeline-ok") {
		t.Fatalf("pipeline output = %s, want child output with nested JSON args env and input", output)
	}
	if strings.Contains(output, "forged-child") {
		t.Fatalf("pipeline output = %s, nested manage_skill control key leaked", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineStepEnvMergesWithParentEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	t.Setenv("API_TOKEN", "")
	t.Setenv("SHARED", "")
	t.Setenv("STEP_ONLY", "")

	command := "echo $API_TOKEN $SHARED $STEP_ONLY"
	if runtime.GOOS == "windows" {
		command = "echo %API_TOKEN% %SHARED% %STEP_ONLY%"
	}
	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:        "child-step-env",
		Status:      "active",
		RequiredEnv: []string{"API_TOKEN"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": command},
		}},
	}); err != nil {
		t.Fatalf("Register(child) error = %v", err)
	}
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "pipeline-step-env",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "child-step-env",
			Params: map[string]string{
				"extra_env": `{"SHARED":"step","STEP_ONLY":"1"}`,
			},
		}},
	}); err != nil {
		t.Fatalf("Register(pipeline) error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("pipeline-step-env", map[string]interface{}{
		"args": `{"extra_env":{"API_TOKEN":"parent-secret","SHARED":"parent"}}`,
	})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	for _, want := range []string{"parent-secret", "step", "1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("pipeline output = %s, want merged env value %q", output, want)
		}
	}
	if strings.Contains(output, "parent-secret parent 1") {
		t.Fatalf("pipeline output = %s, want step env to override SHARED only", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelinePropagatesInputForChildInference(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:         "child-city",
		Status:       "active",
		RequiredArgs: []string{"city"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{city}}"},
		}},
	}); err != nil {
		t.Fatalf("Register(child) error = %v", err)
	}
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "pipeline-city",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "child-city",
		}},
	}); err != nil {
		t.Fatalf("Register(pipeline) error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("pipeline-city", map[string]interface{}{"input": "weather in Chengdu"})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "Chengdu") {
		t.Fatalf("pipeline output = %s, want child city inferred from parent input", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelinePropagatesCapturedVars(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "capture-file",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action:  "bash",
				Params:  map[string]interface{}{"command": "echo file=report.md"},
				Capture: map[string]string{"file": `(?m)^file=([^\r\n]+)`},
			}},
		},
		{
			Name:         "use-input",
			Status:       "active",
			RequiredArgs: []string{"input"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo using {{input}}"},
			}},
		},
		{
			Name:   "pipeline-capture",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "capture-file"},
				{Skill: "use-input", Params: map[string]string{"input": "{{capture-file.input}}"}},
			},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-capture", nil)
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "report.md") || strings.Contains(output, "{{capture-file.input}}") {
		t.Fatalf("pipeline output = %s, want downstream step to receive captured alias", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineChecksParentRequiredEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	t.Setenv("API_TOKEN", "")

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "child-never",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo should-not-run"},
			}},
		},
		{
			Name:        "pipeline-env",
			Status:      "active",
			Mode:        "pipeline",
			RequiredEnv: []string{"API_TOKEN"},
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-never",
			}},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-env", nil)
	if err == nil {
		t.Fatalf("ExecuteWithArgs() unexpectedly succeeded, output = %s", output)
	}
	if !strings.Contains(err.Error(), "API_TOKEN") || strings.Contains(output, "should-not-run") {
		t.Fatalf("output = %s err = %v, want parent required_env precheck before child execution", output, err)
	}

	output, err = exec.ExecuteWithArgs("pipeline-env", map[string]interface{}{"env": map[string]interface{}{"API_TOKEN": "secret"}})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() with env error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "completed") || !strings.Contains(output, "should-not-run") {
		t.Fatalf("output = %s, want run-provided env to satisfy parent pipeline", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineContinueOnFailCountsSuccess(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "child-fail",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: pipelineTestFailParams(),
			}},
		},
		{
			Name:   "child-ok",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo recovered",
				},
			}},
		},
		{
			Name:   "pipeline-continue",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "child-fail", ContinueOnFail: true},
				{Skill: "child-ok"},
			},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-continue", nil)
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "diagnostic") || !strings.Contains(output, "recovered") {
		t.Fatalf("pipeline output = %s, want failed and recovered child output", output)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	var parent *corelib.NLSkillEntry
	for i := range cfg.NLSkills {
		if cfg.NLSkills[i].Name == "pipeline-continue" {
			parent = &cfg.NLSkills[i]
			break
		}
	}
	if parent == nil || parent.SuccessCount != 1 || parent.FailureCount != 0 {
		t.Fatalf("parent usage stats = %+v, want success counted", parent)
	}
	for i := range cfg.NLSkills {
		if cfg.NLSkills[i].Name == "child-fail" || cfg.NLSkills[i].Name == "child-ok" {
			if cfg.NLSkills[i].UsageCount != 0 || cfg.NLSkills[i].SuccessCount != 0 || cfg.NLSkills[i].FailureCount != 0 {
				t.Fatalf("child usage stats = %+v, want internal pipeline calls not counted", cfg.NLSkills[i])
			}
		}
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineStepParamsSelectChildAPIWorkflowOperation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	missingCommand := "definitely-missing-sync-pipeline-child-workflow-command"
	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "child-workflow",
			Status: "active",
			Mode:   "api_workflow",
			Operations: []corelib.NLSkillOperation{
				{Name: "safe", Labels: []string{"safe-step"}},
				{Name: "danger", Labels: []string{"danger-step"}},
			},
			Steps: []corelib.NLSkillStep{
				{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo child-safe"}},
				{Action: "bash", Label: "danger-step", Params: map[string]interface{}{"command": missingCommand + " --version"}},
			},
		},
		{
			Name:   "pipeline-child-workflow",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill:  "child-workflow",
				Params: map[string]string{"operation": "safe"},
			}},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-child-workflow", map[string]interface{}{"operation": "danger"})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "child-safe") || strings.Contains(output, missingCommand) {
		t.Fatalf("pipeline output = %s, want child step param to select safe operation without parent operation leak", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineHonorsGlobalTimeout(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "child-slow",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: pipelineTestSlowParams(),
			}},
		},
		{
			Name:   "child-never",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo should-not-run"},
			}},
		},
		{
			Name:          "pipeline-timeout",
			Status:        "active",
			Mode:          "pipeline",
			GlobalTimeout: 1,
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "child-slow"},
				{Skill: "child-never"},
			},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-timeout", nil)
	if err == nil {
		t.Fatalf("ExecuteWithArgs() unexpectedly succeeded, output = %s", output)
	}
	if !strings.Contains(output, "cancelled") || strings.Contains(output, "should-not-run") {
		t.Fatalf("pipeline output = %s, want timeout cancellation before second child", output)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineRejectsRecursion(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "pipeline-self",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "pipeline-self",
		}},
	}); err != nil {
		t.Fatalf("Register(pipeline) error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("pipeline-self", nil)
	if err == nil {
		t.Fatalf("ExecuteWithArgs() unexpectedly succeeded, output = %s", output)
	}
	if !strings.Contains(output, "pipeline recursion detected") || !strings.Contains(output, "pipeline-self -> pipeline-self") {
		t.Fatalf("pipeline output = %s, want recursion chain", output)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	var parent *corelib.NLSkillEntry
	for i := range cfg.NLSkills {
		if cfg.NLSkills[i].Name == "pipeline-self" {
			parent = &cfg.NLSkills[i]
			break
		}
	}
	if parent == nil || parent.UsageCount != 1 || parent.FailureCount != 1 {
		t.Fatalf("parent usage stats = %+v, want one visible failed run", parent)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineRecursionPrecedesRequirementChecks(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	t.Setenv("API_TOKEN", "")

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:        "pipeline-self-env",
		Status:      "active",
		Mode:        "pipeline",
		RequiredEnv: []string{"API_TOKEN"},
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "pipeline-self-env",
		}},
	}); err != nil {
		t.Fatalf("Register(pipeline) error = %v", err)
	}
	runArgs := cskill.WithPipelineRunStack(map[string]interface{}{}, "pipeline-self-env")

	output, err := exec.ExecuteWithArgs("pipeline-self-env", runArgs)
	if err == nil {
		t.Fatalf("ExecuteWithArgs() unexpectedly succeeded, output = %s", output)
	}
	combined := output + "\n" + err.Error()
	if !strings.Contains(combined, "pipeline recursion detected") || strings.Contains(combined, "API_TOKEN") {
		t.Fatalf("pipeline output = %s err = %v, want recursion to short-circuit requirement checks", output, err)
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineRejectsMutualRecursion(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "pipeline-a",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "pipeline-b",
			}},
		},
		{
			Name:   "pipeline-b",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "pipeline-a",
			}},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-a", nil)
	if err == nil {
		t.Fatalf("ExecuteWithArgs() unexpectedly succeeded, output = %s", output)
	}
	if !strings.Contains(output, "pipeline recursion detected") || !strings.Contains(output, "pipeline-a -> pipeline-b -> pipeline-a") {
		t.Fatalf("pipeline output = %s, want mutual recursion chain", output)
	}
}

func TestSkillExecutorExecuteWithArgsUpdatesUsageWhenRunByDirName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:         "Weather Display",
		DirName:      "weather-dir",
		Status:       "active",
		RequiredArgs: []string{"city"},
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo {{city}}",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("weather-dir", skillExecutionRunArgs("weather Chengdu"))
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 {
		t.Fatalf("NLSkills len = %d, want 1", len(cfg.NLSkills))
	}
	if cfg.NLSkills[0].UsageCount != 1 || cfg.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("usage stats not updated for dirname execution: %+v", cfg.NLSkills[0])
	}
}

func TestSkillExecutorExecuteWithArgsStoresClassifiedLastError(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "classified-failure",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":         "definitely_missing_skill_runner_binary_for_classified_error",
				"preferred_shell": "cmd",
			},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	exec := NewSkillExecutor(app, nil, nil)

	output, err := exec.ExecuteWithArgs("classified-failure", nil)
	if err == nil {
		t.Fatalf("ExecuteWithArgs() unexpectedly succeeded, output = %s", output)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after run error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].FailureCount != 1 {
		t.Fatalf("stats = %+v, want one failure", cfg.NLSkills)
	}
	if !strings.Contains(cfg.NLSkills[0].LastError, "[class:") || !strings.Contains(cfg.NLSkills[0].LastError, "[action:") {
		t.Fatalf("LastError = %q, want classified error with action hint", cfg.NLSkills[0].LastError)
	}
	if !strings.Contains(cfg.NLSkills[0].LastError, "[class: command_not_found]") {
		t.Fatalf("LastError = %q, want command_not_found classification", cfg.NLSkills[0].LastError)
	}
}

func TestFormatExecErrorForStorageClassifiesRuntimeMissingDependency(t *testing.T) {
	got := formatExecErrorForStorage(&bashStepError{
		message:  "exit status 1",
		exitCode: 1,
		stderr:   "Traceback...\nModuleNotFoundError: No module named 'weasyprint'",
	})
	if !strings.Contains(got, "[class: missing_dependency]") || !strings.Contains(got, "weasyprint") {
		t.Fatalf("formatExecErrorForStorage() = %q, want missing_dependency with package name", got)
	}
}

func TestFormatExecErrorForStorageTruncatesButKeepsActionHint(t *testing.T) {
	got := formatExecErrorForStorage(&bashStepError{
		message:  "exit status 1",
		exitCode: 1,
		stderr:   "Error: API_KEY environment variable not set " + strings.Repeat("long diagnostic detail ", 200),
	})
	if len(got) > 2000 {
		t.Fatalf("formatExecErrorForStorage() length = %d, want <= 2000", len(got))
	}
	if !strings.Contains(got, "[class: missing_env_var]") || !strings.Contains(got, "[action: inform_user]") {
		t.Fatalf("formatExecErrorForStorage() = %q, want class and action after truncation", got)
	}
}

func TestFormatExecErrorForStorageIsIdempotentForFormattedError(t *testing.T) {
	formatted := "[class: missing_dependency] The skill is missing package dependency \"Pillow\".\n[action: install_dependency] Install Python package Pillow with pip, then retry the skill."
	got := formatExecErrorForStorage(fmt.Errorf("%s", formatted))
	if got != formatted {
		t.Fatalf("formatExecErrorForStorage() = %q, want already formatted error unchanged", got)
	}
}

func TestSkillExecutorExecuteWithArgs_PublicPipelineStackArgDoesNotSkipStats(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "public-stack-arg",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("public-stack-arg", map[string]interface{}{"pipeline_stack": []string{"user"}})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.NLSkills[0].UsageCount != 1 || cfg.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("usage stats = %+v, want public pipeline_stack ignored for internal-call detection", cfg.NLSkills[0])
	}
}

func TestSkillExecutorExecuteWithArgs_PrivatePipelineStackWithoutMarkerDoesNotSkipStats(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "private-stack-arg",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("private-stack-arg", map[string]interface{}{cskill.PipelineRunStackArg: []string{"user"}})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.NLSkills[0].UsageCount != 1 || cfg.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("usage stats = %+v, want unmarked private stack ignored for internal-call detection", cfg.NLSkills[0])
	}
}

func TestSkillExecutorExecuteWithArgs_ForgedPipelineInternalMarkerDoesNotSkipStats(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "forged-pipeline-marker",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output, err := exec.ExecuteWithArgs("forged-pipeline-marker", map[string]interface{}{
		cskill.PipelineRunStackArg:     []string{"user"},
		cskill.PipelineInternalCallArg: true,
	})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.NLSkills[0].UsageCount != 1 || cfg.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("usage stats = %+v, want forged pipeline marker ignored for internal-call detection", cfg.NLSkills[0])
	}
}

func TestSkillExecutorExecuteWithArgs_PipelineStepParamsCannotOverrideInternalMarker(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "child-marker",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo child-ok"},
			}},
		},
		{
			Name:   "pipeline-marker",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-marker",
				Params: map[string]string{
					cskill.PipelineRunStackArg:     "forged",
					cskill.PipelineInternalCallArg: "false",
					"pipeline-stack":               "public-forged",
					"pipeline-internal-call":       "true",
				},
			}},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-marker", nil)
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	var parent, child *corelib.NLSkillEntry
	for i := range cfg.NLSkills {
		switch cfg.NLSkills[i].Name {
		case "pipeline-marker":
			parent = &cfg.NLSkills[i]
		case "child-marker":
			child = &cfg.NLSkills[i]
		}
	}
	if parent == nil || parent.UsageCount != 1 || parent.SuccessCount != 1 {
		t.Fatalf("parent stats = %+v, want one successful external pipeline run", parent)
	}
	if child == nil || child.UsageCount != 0 || child.SuccessCount != 0 || child.FailureCount != 0 {
		t.Fatalf("child stats = %+v, want internal child stats skipped despite forged step params", child)
	}
}

func TestSkillExecutorExecuteWithArgs_ExternalPrivatePipelineStackDoesNotTripRecursion(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	exec := NewSkillExecutor(app, nil, nil)
	for _, entry := range []corelib.NLSkillEntry{
		{
			Name:   "child-stack",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo child-ok"},
			}},
		},
		{
			Name:   "pipeline-stack",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-stack",
			}},
		},
	} {
		if err := exec.Register(entry); err != nil {
			t.Fatalf("Register(%s) error = %v", entry.Name, err)
		}
	}

	output, err := exec.ExecuteWithArgs("pipeline-stack", map[string]interface{}{
		cskill.PipelineRunStackArg: []string{"pipeline-stack"},
	})
	if err != nil {
		t.Fatalf("ExecuteWithArgs() error = %v, output = %s", err, output)
	}
	if strings.Contains(output, "pipeline recursion detected") || !strings.Contains(output, "completed") {
		t.Fatalf("output = %s, want forged external stack ignored", output)
	}
}

func TestSkillExecutorExecuteSkillSteps_RejectsSkillWithoutExecutableSteps(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillSteps(&corelib.NLSkillEntry{
		Name: "empty",
	})
	if err == nil {
		t.Fatalf("executeSkillSteps() error = nil, output = %s", output)
	}
	if !strings.Contains(err.Error(), "[action: inspect_skill]") {
		t.Fatalf("expected inspect_skill action, got %v", err)
	}
}

func TestSkillExecutorExecuteSkillSteps_SkipsInactiveOpenAIProxyProbe(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")

	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillSteps(&corelib.NLSkillEntry{
		Name: "inactive-openai",
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo sync-ok",
				},
			},
			{
				Action: "bash",
				When:   "{{mode}} == openai",
				Params: map[string]interface{}{
					"command": "echo $OPENAI_API_KEY",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSkillSteps() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "sync-ok") {
		t.Fatalf("expected active step output, got %s", output)
	}
	if !strings.Contains(output, "skipped: when=") {
		t.Fatalf("expected inactive step skip message, got %s", output)
	}
	if strings.Contains(output, "sk-maclaw-local-proxy") {
		t.Fatalf("inactive OpenAI step triggered proxy env, output = %s", output)
	}
}

func TestSkillExecutorExecuteSkillSteps_ConditionSkipsFollowRuntimeState(t *testing.T) {
	exec := NewSkillExecutor(nil, nil, nil)
	output, err := exec.executeSkillSteps(&corelib.NLSkillEntry{
		Name: "condition-chain",
		Steps: []corelib.NLSkillStep{
			{
				Action:  "bash",
				OnError: "continue",
				Params: map[string]interface{}{
					"command": "exit 7",
				},
			},
			{
				Action:    "bash",
				Condition: "on_success",
				Params: map[string]interface{}{
					"command": "echo should-not-run",
				},
			},
			{
				Action:    "bash",
				Condition: "on_failure",
				Params: map[string]interface{}{
					"command": "echo recovery-ok",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSkillSteps() error = %v, output = %s", err, output)
	}
	if !strings.Contains(output, "skipped: prior failure") {
		t.Fatalf("expected on_success skip after failure, got %s", output)
	}
	if strings.Contains(output, "should-not-run") {
		t.Fatalf("on_success step ran after failure, output = %s", output)
	}
	if !strings.Contains(output, "recovery-ok") {
		t.Fatalf("expected on_failure recovery step output, got %s", output)
	}
}

func TestSkillExecutorExecute_ImplicitSessionReuse(t *testing.T) {
	t.Skip("legacy external session reuse is disabled; coding tasks use CodingSubAgent")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Projects = []corelib.ProjectConfig{{Id: "proj-implicit", Name: "Implicit", Path: tempHome}}
	cfg.CurrentProject = "proj-implicit"
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models:       []corelib.ModelConfig{{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	mgr := NewRemoteSessionManager(app)
	mgr.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	mgr.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(504)}, nil
	}
	app.remoteSessions = mgr
	app.sessionStarter = NewCodingSessionStarter(app)
	exec := NewSkillExecutor(app, nil, mgr)

	output, err := exec.executeSkillSteps(&corelib.NLSkillEntry{
		Name:        "seq-skill",
		Description: "修复 Go 项目中的 bug 并继续完成之前的任务",
		Steps: []corelib.NLSkillStep{
			{
				Action: "create_session",
				Params: map[string]interface{}{
					"tool":       "claude",
					"project_id": "proj-implicit",
					"task":       "修复 bug 并修改代码",
				},
			},
			{
				Action: "send_and_observe",
				Params: map[string]interface{}{
					"text": "continue implicit",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSkillSteps() error = %v", err)
	}
	if !strings.Contains(output, "会话已创建: ID=") {
		t.Fatalf("expected create_session output, got %s", output)
	}
	if !strings.Contains(output, "会话 ") {
		t.Fatalf("expected send_and_observe output, got %s", output)
	}
}
