package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestBuildTrajectoryRecorderFactoryWiresSkillAutoSummaryPipeline(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LLMTrajectoryLogging = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	recorder := app.buildTrajectoryRecorderFactory()()
	if recorder == nil {
		t.Fatal("expected trajectory recorder when logging is enabled")
	}

	recorder.mu.Lock()
	pipeline := recorder.pipeline
	recorder.mu.Unlock()
	if pipeline == nil {
		t.Fatal("expected trajectory recorder to attach skill auto-summary pipeline")
	}
	if pipeline.skillExec == nil {
		t.Fatal("expected skill auto-summary pipeline to have a skill executor")
	}
	if pipeline.checker == nil {
		t.Fatal("expected skill auto-summary pipeline to have a security checker")
	}
	if pipeline.trigger == nil {
		t.Fatal("expected skill auto-summary pipeline to have an upload trigger")
	}
}

func TestRunAutoUploadSkipsWhenDependenciesMissing(t *testing.T) {
	if err := RunAutoUpload(context.Background(), "missing-deps-skill", "", 1, nil, nil, nil); err != nil {
		t.Fatalf("RunAutoUpload() error = %v", err)
	}
}

func TestInferSecurityLabelsRecognizesCommonShellAndNetworkTools(t *testing.T) {
	labels := inferSecurityLabels([]skill.SkillYAMLStep{
		{Action: "bash"},
		{Action: "powershell"},
		{Action: "curl_request"},
		{Action: "wget_download"},
	})
	seen := make(map[string]bool, len(labels))
	for _, label := range labels {
		seen[label] = true
	}
	if !seen["shell_exec"] {
		t.Fatal("expected shell_exec label for common shell tools")
	}
	if !seen["network_access"] {
		t.Fatal("expected network_access label for common network tools")
	}
}
