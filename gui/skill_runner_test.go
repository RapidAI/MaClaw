package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestSkillDocHelpersAcceptMixedCaseSkillMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Skill.md"), []byte("# mixed skill docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasSkillDocFile(dir) {
		t.Fatal("hasSkillDocFile should accept Skill.md")
	}
	if got := loadSkillDocContent(dir); !strings.Contains(got, "mixed skill docs") {
		t.Fatalf("loadSkillDocContent() = %q", got)
	}
}

func TestSkillRunnerStartRunDoesNotInheritWorkflowPolicy(t *testing.T) {
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
		Name:   "manual-runner-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo runner"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)
	if _, err := app.workflowEngine.StartWorkflow(desktopUserID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := app.workflowEngine.SkipPhaseForm(desktopUserID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	runID, err := runner.StartRun("manual-runner-skill", nil)
	if err != nil {
		if strings.Contains(err.Error(), "not allowed by the current workflow tool policy") {
			t.Fatalf("manual SkillRunner.StartRun must not inherit workflow policy: %v", err)
		}
		t.Fatalf("StartRun() error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != skillRunStatusSuccess {
		t.Fatalf("skill status = %s, want success; status=%#v", status.Status, status)
	}
}

func TestSkillRunnerStartRunDoesNotWaitForExecutorMutationLock(t *testing.T) {
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
		Name:   "lock-free-start-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo lock-free"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	app.skillExecutor.mu.Lock()
	type startResult struct {
		runID string
		err   error
	}
	done := make(chan startResult, 1)
	go func() {
		runID, err := runner.StartRunForOwner("desktop-user:project-a", "lock-free-start-skill", nil)
		done <- startResult{runID: runID, err: err}
	}()

	var res startResult
	select {
	case res = <-done:
	case <-time.After(300 * time.Millisecond):
		app.skillExecutor.mu.Unlock()
		t.Fatal("StartRunForOwner waited on SkillExecutor.mu; this serializes independent agent starts behind skill stats writes")
	}
	app.skillExecutor.mu.Unlock()
	if res.err != nil {
		t.Fatalf("StartRunForOwner() error = %v", res.err)
	}
	status := waitSkillRunDoneForTest(t, runner, res.runID)
	if status.Status != skillRunStatusSuccess {
		t.Fatalf("skill status = %s, want success; error=%s", status.Status, status.Error)
	}
}

func TestSkillRunnerUsesIsolatedWorkspaceForSkillDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	skillDir := filepath.Join(tempHome, "skill-src")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("WriteFile seed: %v", err)
	}
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	app := h.app
	app.testHomeDir = tempHome
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "workspace-writer",
		Status:   "active",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"working_dir": skillDir,
				"command":     fmt.Sprintf("echo hello > run-output.txt && echo abs > %q", filepath.ToSlash(filepath.Join(skillDir, "cmd-output.txt"))),
			},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRunForOwner("desktop-user:project-a", "workspace-writer", nil)
	if err != nil {
		t.Fatalf("StartRunForOwner() error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != skillRunStatusSuccess {
		t.Fatalf("run status = %s error=%s", status.Status, status.Error)
	}
	if status.OwnerID != "desktop-user:project-a" {
		t.Fatalf("owner id = %q, want project owner", status.OwnerID)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "run-output.txt")); !os.IsNotExist(err) {
		t.Fatalf("run wrote into installed skill dir, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "cmd-output.txt")); !os.IsNotExist(err) {
		t.Fatalf("run command path wrote into installed skill dir, stat err = %v", err)
	}
}

func TestSkillExecutorUsesIsolatedWorkspaceForSkillDir(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "skill-src")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "sync-workspace-writer",
		Status:   "active",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"working_dir": skillDir,
				"command":     fmt.Sprintf("echo sync > sync-output.txt && echo abs > %q", filepath.ToSlash(filepath.Join(skillDir, "sync-cmd-output.txt"))),
			},
		}},
	}
	exec := NewSkillExecutor(&App{}, nil, nil)

	result := exec.executeSkillStepsDetailed(entry, nil)
	if result.Err != nil {
		t.Fatalf("executeSkillStepsDetailed() error = %v output=%s", result.Err, result.Output)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "sync-output.txt")); !os.IsNotExist(err) {
		t.Fatalf("sync run wrote into installed skill dir, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "sync-cmd-output.txt")); !os.IsNotExist(err) {
		t.Fatalf("sync command path wrote into installed skill dir, stat err = %v", err)
	}
}

func TestSkillExecutorLogsOwnerForSyncSkillRun(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "owner-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "sync-owner-log",
		Status:   "active",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"working_dir": skillDir,
				"command":     "echo owner-log",
			},
		}},
	}
	var logs bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalWriter) })

	exec := NewSkillExecutor(&App{}, nil, nil)
	result := exec.executeSkillStepsDetailed(entry, map[string]interface{}{"_skill_owner_id": "desktop-user:D:/tasks/owner"})
	if result.Err != nil {
		t.Fatalf("executeSkillStepsDetailed() error = %v output=%s", result.Err, result.Output)
	}
	if !strings.Contains(logs.String(), `owner="desktop-user:D:/tasks/owner"`) {
		t.Fatalf("sync skill logs do not include owner id; logs=%s", logs.String())
	}
}

func TestPrepareSkillRunWorkspaceIncludesRuntimeArtifacts(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "skill-src")
	nodeModules := filepath.Join(skillDir, "node_modules", "demo")
	if err := os.MkdirAll(nodeModules, 0o755); err != nil {
		t.Fatalf("MkdirAll node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nodeModules, "index.js"), []byte("module.exports = 1"), 0o644); err != nil {
		t.Fatalf("WriteFile runtime artifact: %v", err)
	}

	workspace, cleanup, err := prepareSkillRunWorkspace("run-runtime", "runtime-skill", skillDir)
	if err != nil {
		t.Fatalf("prepareSkillRunWorkspace() error = %v", err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(workspace, "node_modules", "demo", "index.js")); err != nil {
		t.Fatalf("runtime artifact missing from workspace: %v", err)
	}
}

func TestRemapSkillRunStepToWorkspaceRemapsNestedParams(t *testing.T) {
	source := filepath.Clean(filepath.Join(t.TempDir(), "skill-src"))
	workspace := filepath.Clean(filepath.Join(t.TempDir(), "skill-workspace"))
	step := corelib.NLSkillStep{
		Action: "call_mcp_tool",
		Params: map[string]interface{}{
			"command": "python " + filepath.Join(source, "run.py"),
			"arguments": map[string]interface{}{
				"path": filepath.Join(source, "input.txt"),
				"items": []interface{}{
					filepath.ToSlash(filepath.Join(source, "a.txt")),
					map[string]interface{}{"nested": filepath.Join(source, "b.txt")},
				},
			},
			"env": map[string]string{"SKILL_DIR": source},
		},
	}

	remapped := remapSkillRunStepToWorkspace(step, source, workspace)
	command, _ := remapped.Params["command"].(string)
	if strings.Contains(command, source) || !strings.Contains(command, workspace) {
		t.Fatalf("command remap = %q, want workspace path", command)
	}
	args := remapped.Params["arguments"].(map[string]interface{})
	path, _ := args["path"].(string)
	if strings.Contains(path, source) || !strings.Contains(path, workspace) {
		t.Fatalf("nested path remap = %q, want workspace path", path)
	}
	items := args["items"].([]interface{})
	first, _ := items[0].(string)
	if strings.Contains(first, filepath.ToSlash(source)) || !strings.Contains(first, filepath.ToSlash(workspace)) {
		t.Fatalf("list path remap = %q, want slash workspace path", first)
	}
	env := remapped.Params["env"].(map[string]string)
	if env["SKILL_DIR"] != workspace {
		t.Fatalf("env remap = %q, want %q", env["SKILL_DIR"], workspace)
	}
	originalCommand, _ := step.Params["command"].(string)
	if !strings.Contains(originalCommand, source) {
		t.Fatalf("original params mutated: %q", originalCommand)
	}
}

func TestShouldRetainSkillRunWorkspaceStatusForWorkspaceArtifact(t *testing.T) {
	workspace := t.TempDir()
	artifact := filepath.Join(workspace, "out.pdf")
	if err := os.WriteFile(artifact, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	status := SkillRunStatus{
		RunID:  "run-artifact",
		Skill:  "artifact-skill",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{{
			Index:  0,
			Action: "bash",
			Status: skillStepStatusSuccess,
			Output: "artifact: " + artifact,
		}},
	}
	if !shouldRetainSkillRunWorkspaceStatus(status, workspace) {
		t.Fatal("workspace artifact should retain run workspace")
	}
	status.ExpectedOutput = filepath.Join(t.TempDir(), "expected.pdf")
	if !shouldRetainSkillRunWorkspaceStatus(status, workspace) {
		t.Fatal("workspace artifact output should retain workspace even when expected output points elsewhere")
	}
	status.Steps[0].Output = "artifact: " + filepath.Join(t.TempDir(), "out.pdf")
	if shouldRetainSkillRunWorkspaceStatus(status, workspace) {
		t.Fatal("artifact outside workspace should not retain run workspace")
	}
}

func TestSkillRunnerRejectsShellBrowserAutomationSkill(t *testing.T) {
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
		Name:        "zhihu-poster",
		Status:      "active",
		RequiresGUI: true,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": `python post.py article --title "{{title}}" --file "{{file}}" --screenshot`},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("zhihu-poster", map[string]interface{}{"title": "t", "file": "a.md"})
	if err == nil || !strings.Contains(err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("StartRun error = %v, want stable browser rejection", err)
	}
}

func TestSkillExecutorRejectsShellBrowserAutomationSubSkill(t *testing.T) {
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
		Name:             "browser-wrapper",
		Status:           "active",
		RequiresToolsets: []string{"browser"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python automate_browser.py"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	exec := NewSkillExecutor(app, nil, nil)

	result := exec.executeSkillByNameDetailed("browser-wrapper", nil)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("executeSkillByNameDetailed error = %v, want stable browser rejection", result.Err)
	}
}

func TestSkillExecutorRejectsShellBrowserAutomationDirectExecution(t *testing.T) {
	exec := NewSkillExecutor(&App{}, nil, nil)
	result := exec.executeSkillStepsDetailed(&corelib.NLSkillEntry{
		Name:        "direct-browser-script",
		RequiresGUI: true,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python -m playwright connect_over_cdp http://127.0.0.1:3888"},
		}},
	}, nil)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("executeSkillStepsDetailed error = %v, want stable browser rejection", result.Err)
	}
}

func TestSkillExecutorRejectsShellBrowserAutomationHiddenInSkillDirScript(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "post.py"), []byte("from playwright.async_api import async_playwright\nasync def main():\n    browser = await p.chromium.connect_over_cdp('http://127.0.0.1:3888')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile post.py: %v", err)
	}
	exec := NewSkillExecutor(&App{}, nil, nil)
	result := exec.executeSkillStepsDetailed(&corelib.NLSkillEntry{
		Name:        "script-hidden-browser",
		RequiresGUI: true,
		SkillDir:    skillDir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python post.py"},
		}},
	}, nil)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("executeSkillStepsDetailed error = %v, want stable browser rejection", result.Err)
	}
}

func TestSkillExecutorRejectsShellBrowserAutomationWithoutGUIFlag(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "post.py"), []byte("await page.screenshot(path='out.png')\nawait browser.close()\n"), 0o644); err != nil {
		t.Fatalf("WriteFile post.py: %v", err)
	}
	exec := NewSkillExecutor(&App{}, nil, nil)
	result := exec.executeSkillStepsDetailed(&corelib.NLSkillEntry{
		Name:     "missing-gui-browser-script",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python post.py"},
		}},
	}, nil)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("executeSkillStepsDetailed error = %v, want stable browser rejection", result.Err)
	}
}

func TestSkillExecutorRejectsBrowserAutomationCommandWithoutGUIFlag(t *testing.T) {
	exec := NewSkillExecutor(&App{}, nil, nil)
	result := exec.executeSkillStepsDetailed(&corelib.NLSkillEntry{
		Name: "missing-gui-browser-command",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "node run-playwright.js --screenshot"},
		}},
	}, nil)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("executeSkillStepsDetailed error = %v, want stable browser rejection", result.Err)
	}
}

func TestSkillExecutorAllowsNonAutomationBrowserText(t *testing.T) {
	entry := corelib.NLSkillEntry{
		Name:        "browser-doc-note",
		Description: "mentions browser in docs but does not automate it",
		RequiresGUI: true,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo browser docs only"},
		}},
	}
	if isShellBrowserAutomationSkillEntry(entry) {
		t.Fatalf("isShellBrowserAutomationSkillEntry(%+v) = true, want false for non-automation browser text", entry)
	}
}

func TestSkillExecutorRejectsShellBrowserAutomationDirectPipelineExecution(t *testing.T) {
	exec := NewSkillExecutor(&App{}, nil, nil)
	result := exec.executePipelineSkillDetailed(&corelib.NLSkillEntry{
		Name:             "pipeline-browser-wrapper",
		RequiresToolsets: []string{"browser"},
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "child",
		}},
	}, nil, nil)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("executePipelineSkillDetailed error = %v, want stable browser rejection", result.Err)
	}
}

func TestSkillExecutorAsRegisteredToolsFiltersShellBrowserAutomation(t *testing.T) {
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
	cfg.NLSkills = []corelib.NLSkillEntry{
		{
			Name:        "zhihu-poster",
			Status:      "active",
			RequiresGUI: true,
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "python post.py --screenshot"},
			}},
		},
		{Name: "normal", Status: "active", Description: "safe skill"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	exec := NewSkillExecutor(app, nil, nil)

	tools := exec.AsRegisteredTools()
	if len(tools) != 1 || tools[0].Name != "normal" {
		t.Fatalf("AsRegisteredTools() = %+v, want only normal", tools)
	}
}

func TestSkillExecutorRegisterRejectsShellBrowserAutomation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	app := h.app
	app.testHomeDir = tempHome
	exec := NewSkillExecutor(app, nil, nil)

	err := exec.Register(corelib.NLSkillEntry{
		Name:        "new-browser-script",
		Status:      "active",
		RequiresGUI: true,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python -m playwright connect_over_cdp http://127.0.0.1:3888"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("Register() error = %v, want stable browser rejection", err)
	}
}

func TestManagedCapabilityReplacementRejectsShellBrowserAutomation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	app := h.app
	app.testHomeDir = tempHome
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:   "managed-safe",
		Status: "active",
		Capability: &corelib.SkillCapabilityRef{
			CapabilityID: "cap-browser",
		},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo ok"}}},
	}); err != nil {
		t.Fatalf("Register(existing) error = %v", err)
	}

	err := app.registerOrReplaceManagedCapabilitySkill(corelib.NLSkillEntry{
		Name:   "managed-browser",
		Status: "active",
		Capability: &corelib.SkillCapabilityRef{
			CapabilityID: "cap-browser",
		},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "python -m playwright connect_over_cdp http://127.0.0.1:3888"}}},
	}, &corelib.NLSkillEntry{Name: "managed-safe", Capability: &corelib.SkillCapabilityRef{CapabilityID: "cap-browser"}})
	if err == nil || !strings.Contains(err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("registerOrReplaceManagedCapabilitySkill() error = %v, want stable browser rejection", err)
	}
}

func TestSkillInstallAdmissionRejectsShellBrowserAutomationEvenWithGuardrailsOff(t *testing.T) {
	app := &App{policyEngine: NewPolicyEngineWithMode("none")}
	err := app.admitManualSkillInstall(context.Background(), &corelib.NLSkillEntry{
		Name:        "install-browser-script",
		Status:      "active",
		RequiresGUI: true,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python post.py --screenshot"},
		}},
	}, "manual skill create", nil)
	if err == nil || !strings.Contains(err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("admitManualSkillInstall() error = %v, want stable browser rejection", err)
	}
}

func TestSkillRunnerPersistRepairResultRejectsShellBrowserAutomation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	skillDir := filepath.Join(t.TempDir(), "repair-browser")
	exec := NewSkillExecutor(&App{testHomeDir: tempHome}, nil, nil)
	if err := exec.Register(corelib.NLSkillEntry{
		Name:   "repair-browser",
		Status: "active",
		Source: "manual",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo safe"},
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	runner := NewSkillRunner(exec)

	err := runner.persistRepairResult(&corelib.NLSkillEntry{
		Name:     "repair-browser",
		Status:   "active",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python -m playwright connect_over_cdp http://127.0.0.1:3888"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "stable browser tool/session mechanism") {
		t.Fatalf("persistRepairResult() error = %v, want stable browser rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(skillDir, "skill.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("persistRepairResult() wrote rejected skill.yaml, stat err = %v", statErr)
	}
	defs := exec.List()
	if len(defs) != 1 || len(defs[0].Steps) != 1 || browserSkillStringParam(defs[0].Steps[0].Params, "command") != "echo safe" {
		t.Fatalf("persistRepairResult() mutated config despite rejection: %+v", defs)
	}
}

func TestSkillRunnerRecordSkillUsageExperienceSuccess(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	runner := NewSkillRunner(&SkillExecutor{app: &App{usageTracker: tracker}})

	runner.recordSkillUsageExperience(&corelib.NLSkillEntry{
		Name:        "pdf-helper",
		Description: "convert pdf files",
		Triggers:    []string{"pdf", "convert"},
	}, nil, nil)

	if score := tracker.OutcomeScore("skill:pdf-helper"); score != 1 {
		t.Fatalf("OutcomeScore(skill:pdf-helper) = %.2f, want 1", score)
	}
	if score := tracker.ContextOutcomeScore("skill:pdf-helper", []string{"pdf"}); score != 1 {
		t.Fatalf("ContextOutcomeScore(skill:pdf-helper,pdf) = %.2f, want 1", score)
	}
}

func TestSkillRunnerRecordSkillUsageExperienceFailure(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	runner := NewSkillRunner(&SkillExecutor{app: &App{usageTracker: tracker}})
	skill := &corelib.NLSkillEntry{
		Name:        "missing-env-skill",
		Description: "uses api key",
		Triggers:    []string{"api", "key"},
	}

	runner.recordSkillUsageExperience(skill, assertErrorString("Error: API_KEY environment variable not set"), nil)
	runner.recordSkillUsageExperience(skill, assertErrorString("Error: API_KEY environment variable not set"), nil)
	runner.recordSkillUsageExperience(skill, assertErrorString("Error: API_KEY environment variable not set"), nil)

	if score := tracker.OutcomeScore("skill:missing-env-skill"); score != 0 {
		t.Fatalf("OutcomeScore(skill:missing-env-skill) = %.2f, want 0", score)
	}
	stats := tracker.ContextFailureStats([]string{"api"}, 3)
	if len(stats) != 1 || stats[0].ToolName != "skill:missing-env-skill" || stats[0].Failures != 3 || stats[0].FailureRate != 1 {
		t.Fatalf("ContextFailureStats(api) = %#v, want failed skill stats", stats)
	}
}

type assertErrorString string

func (e assertErrorString) Error() string { return string(e) }

func TestSkillDocHelpersAcceptMixedCaseReadme(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Readme.md"), []byte("# mixed docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasSkillDocFile(dir) {
		t.Fatal("hasSkillDocFile should accept Readme.md")
	}
	if got := loadSkillDocContent(dir); !strings.Contains(got, "mixed docs") {
		t.Fatalf("loadSkillDocContent() = %q", got)
	}
}
func TestSkillDocHelpersAcceptLowercaseReadme(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# lower docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasSkillDocFile(dir) {
		t.Fatal("hasSkillDocFile should accept readme.md")
	}
	if got := loadSkillDocContent(dir); !strings.Contains(got, "lower docs") {
		t.Fatalf("loadSkillDocContent() = %q", got)
	}
}
func TestSkillRunnerExecuteStepWithContext_CallMCPToolResolvesName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("enabled-no-autostart", false, false)}
	cfg.LocalMCPServers[0].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()

	runner := NewSkillRunner(&SkillExecutor{app: app, mcpRegistry: app.mcpRegistry})
	result, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "call_mcp_tool",
		Params: map[string]interface{}{
			"server_id": "brave-search",
			"tool_name": "ping",
			"arguments": map[string]interface{}{},
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext() error = %v", err)
	}
	if strings.TrimSpace(result) != "{}" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestExpandPortableHomeVarsUsesSlashHome(t *testing.T) {
	home := `C:\Users\tester`
	cmd := `python "$HOME/scripts/run.py" --config=${HOME}\config\skill.json --root $HOME --name=$HOME_CONFIG`
	got := expandPortableHomeVars(cmd, home)
	wantHome := filepath.ToSlash(home)
	withoutUnrelatedVar := strings.ReplaceAll(got, "$HOME_CONFIG", "")
	if strings.Contains(withoutUnrelatedVar, "$HOME") || strings.Contains(withoutUnrelatedVar, "${HOME}") {
		t.Fatalf("portable HOME placeholders were not expanded: %q", got)
	}
	for _, want := range []string{wantHome + "/scripts/run.py", wantHome + "/config\\skill.json", "--root " + wantHome} {
		if !strings.Contains(got, want) {
			t.Fatalf("expanded command %q missing %q", got, want)
		}
	}
	if !strings.Contains(got, "$HOME_CONFIG") {
		t.Fatalf("unrelated HOME-prefixed variable should be preserved: %q", got)
	}
}

func TestMergeRequiredEnvParamPreservesStepEnv(t *testing.T) {
	params := map[string]interface{}{
		"required_env": []interface{}{"STEP_KEY", "SHARED_KEY"},
	}
	mergeRequiredEnvParam(params, []string{"SHARED_KEY", "SKILL_KEY"})

	got, ok := params["required_env"].([]interface{})
	if !ok {
		t.Fatalf("required_env type = %T, want []interface{}", params["required_env"])
	}
	want := []string{"STEP_KEY", "SHARED_KEY", "SKILL_KEY"}
	if len(got) != len(want) {
		t.Fatalf("required_env = %#v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("required_env[%d] = %#v, want %q; all=%#v", i, got[i], name, got)
		}
	}
}

func TestMergeExtraEnvParamLetsRunEnvOverrideStepDefaults(t *testing.T) {
	params := map[string]interface{}{
		"extra_env": map[string]interface{}{
			"STEP_ONLY": "1",
			"SHARED":    "from-step",
		},
	}
	mergeExtraEnvParam(params, map[string]string{"SHARED": "from-run", "RUN_ONLY": "2"})

	got, ok := params["extra_env"].(map[string]interface{})
	if !ok {
		t.Fatalf("extra_env type = %T, want map[string]interface{}", params["extra_env"])
	}
	if got["SHARED"] != "from-run" || got["STEP_ONLY"] != "1" || got["RUN_ONLY"] != "2" {
		t.Fatalf("extra_env merge = %#v", got)
	}
}

func TestSkillRunnerStartRun_RejectsSkillWithoutExecutableSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "doc-only",
		Status: "active",
		Steps:  nil,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("doc-only", nil)
	if err == nil || !strings.Contains(err.Error(), "has no executable steps") {
		t.Fatalf("expected no executable steps error, got %v", err)
	}
}

func TestSkillRunnerStartRun_RejectsKnowledgeSkillWithoutCraftFallback(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	skillDir := filepath.Join(tempHome, "knowledge-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Knowledge\n\n```bash\necho example-only\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "knowledge",
		Type:        "knowledge_skill",
		Description: "Reference docs",
		Status:      "active",
		SkillDir:    skillDir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo stale-cache"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("knowledge", nil)
	if err == nil || !strings.Contains(err.Error(), "knowledge skill") || !strings.Contains(err.Error(), "not directly executable") {
		t.Fatalf("expected knowledge skill error, got %v", err)
	}
}

func TestSkillRunnerStartRun_RejectsMissingRequiredParamSchema(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "schema-required",
		Status: "active",
		Mode:   "interactive",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"task": "write about {{topic}}"},
		}},
		Params: []corelib.NLSkillParam{{Name: "topic", Required: true}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("schema-required", nil)
	if err == nil || !strings.Contains(err.Error(), "topic") {
		t.Fatalf("expected required param schema error, got %v", err)
	}
}

func TestSkillRunnerStartRun_BlocksMissingInferredCommand(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	missingCommand := "definitely-missing-skill-runner-command-gui"
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "missing-command-skill",
		Status: "active",
		Mode:   "interactive",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": missingCommand + " --version",
			},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("missing-command-skill", nil)
	if err == nil || !strings.Contains(err.Error(), missingCommand) {
		t.Fatalf("expected missing inferred command precheck error, got %v", err)
	}
}

func TestSkillRunnerStartRun_AllowsGUIOpenAIProxyEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("OPENAI_API_KEY", "")

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "openai-env-skill",
		Status:      "active",
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}}
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:     "TestOpenAI",
		URL:      "https://api.example.test/v1",
		Key:      "sk-test",
		Model:    "test-model",
		AuthType: "api_key",
		IsCustom: true,
	}}
	cfg.MaclawLLMCurrentProvider = "TestOpenAI"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("openai-env-skill", nil)
	if err != nil {
		t.Fatalf("StartRun() error = %v; GUI runner should satisfy OPENAI_API_KEY via local proxy", err)
	}
	_ = runner.CancelRun(runID)
}

func TestSkillRunnerRunFailsWhenOpenAIProxyConfigMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	skill := &corelib.NLSkillEntry{
		Name:        "openai-env-missing-config",
		Status:      "active",
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo should-not-run"},
		}},
	}
	run := &skillRun{
		status: SkillRunStatus{
			RunID:  "proxy-missing-config",
			Skill:  skill.Name,
			Status: "running",
			Steps: []StepResult{{
				Index:  0,
				Action: "bash",
				Status: "pending",
			}},
		},
	}
	runner := NewSkillRunner(&SkillExecutor{})

	runner.executeAsync(context.Background(), run, skill)

	if run.status.Status != "failed" {
		t.Fatalf("status = %+v, want failed proxy configuration error", run.status)
	}
	if !strings.Contains(run.status.Error, "configure_llm") {
		t.Fatalf("status error = %q, want configure_llm action", run.status.Error)
	}
	if len(run.status.Steps) != 1 || run.status.Steps[0].Status != "skipped" {
		t.Fatalf("steps = %#v, want step skipped before execution", run.status.Steps)
	}
	if run.status.EndedAt == "" || run.status.DurationMs < 0 {
		t.Fatalf("terminal timing not recorded: %+v", run.status)
	}
}

func TestSkillRunnerRunRecordsStatsForOpenAIProxyConfigFailure(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("OPENAI_API_KEY", "")

	skill := corelib.NLSkillEntry{
		Name:        "openai-env-stats-failure",
		Status:      "active",
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo should-not-run"},
		}},
	}
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{skill}
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:     "Empty",
		IsCustom: true,
	}}
	cfg.MaclawLLMCurrentProvider = "Empty"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	runner := NewSkillRunner(NewSkillExecutor(app, nil, nil))
	run := &skillRun{
		status: SkillRunStatus{
			RunID:  "proxy-missing-config-stats",
			Skill:  skill.Name,
			Status: "running",
			Steps: []StepResult{{
				Index:  0,
				Action: "bash",
				Status: "pending",
			}},
		},
	}
	runner.runs[run.status.RunID] = run

	runner.executeAsync(context.Background(), run, &skill)

	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after run error = %v", err)
	}
	if len(cfg.NLSkills) != 1 {
		t.Fatalf("skills = %#v, want one skill", cfg.NLSkills)
	}
	got := cfg.NLSkills[0]
	if got.UsageCount != 1 || got.FailureCount != 1 || got.SuccessCount != 0 {
		t.Fatalf("stats = usage:%d success:%d failure:%d, want usage=1 success=0 failure=1", got.UsageCount, got.SuccessCount, got.FailureCount)
	}
	if !strings.Contains(got.LastError, "configure_llm") {
		t.Fatalf("LastError = %q, want configure_llm action", got.LastError)
	}
}

func TestSkillRunnerRunSkipsProxyForInactiveOpenAIStep(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	skill := &corelib.NLSkillEntry{
		Name:   "inactive-openai-step",
		Status: "active",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo runtime-ok"}},
			{Action: "bash", When: "{{mode}} == openai", Params: map[string]interface{}{"command": "echo $OPENAI_API_KEY"}},
		},
	}
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{*skill}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	runner := NewSkillRunner(NewSkillExecutor(app, nil, nil))
	run := &skillRun{
		status: SkillRunStatus{
			RunID:  "inactive-openai-step-run",
			Skill:  skill.Name,
			Status: "running",
			Steps: []StepResult{
				{Index: 0, Action: "bash", Status: "pending"},
				{Index: 1, Action: "bash", Status: "pending"},
			},
		},
		templateVars: map[string]string{"mode": "basic"},
	}
	runner.runs[run.status.RunID] = run

	runner.executeAsync(context.Background(), run, skill)

	if run.status.Status != "success" {
		t.Fatalf("status = %+v, want success without proxy configuration", run.status)
	}
	if len(run.status.Steps) != 2 || run.status.Steps[0].Status != "success" || run.status.Steps[1].Status != "skipped" {
		t.Fatalf("steps = %#v, want first success and inactive OpenAI step skipped", run.status.Steps)
	}
	if !strings.Contains(run.status.Steps[0].Output, "runtime-ok") {
		t.Fatalf("first step output = %q, want runtime-ok", run.status.Steps[0].Output)
	}
}

func TestSkillRunnerStartRun_AllowsEnvDerivedFromRunParam(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("API_TOKEN", "")

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "env-from-param",
		Status:      "active",
		RequiredEnv: []string{"API_TOKEN"},
		Params:      []corelib.NLSkillParam{{Name: "api_token", Required: true}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":   "echo ok",
				"extra_env": map[string]interface{}{"API_TOKEN": "{{api_token}}"},
			},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("env-from-param", map[string]interface{}{"api_token": "secret"})
	if err != nil {
		t.Fatalf("StartRun() error = %v; env derived from run params should satisfy API_TOKEN", err)
	}
	_ = runner.CancelRun(runID)
}

func TestSkillRunnerStartRun_AcceptsRunProvidedExtraEnvAlias(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("API_TOKEN", "")

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "extra-env-alias",
		Status:      "active",
		RequiredEnv: []string{"API_TOKEN"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "echo ok",
			},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("extra-env-alias", map[string]interface{}{
		"extra_env": map[string]interface{}{"API_TOKEN": "secret"},
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v; extra_env alias should satisfy API_TOKEN", err)
	}
	_ = runner.CancelRun(runID)
}

func TestSkillRunnerStartRun_ExecutesPipelineSkillWithoutSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{
			Name:        "child-echo",
			Status:      "active",
			RequiredEnv: []string{"API_TOKEN"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": pipelineTestEchoCommand()},
			}},
		},
		{
			Name:   "pipeline-demo",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill:  "child-echo",
				Params: map[string]string{"input": "{{input}}"},
			}},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-demo", map[string]interface{}{
		"input":     "pipeline-ok",
		"extra_env": map[string]interface{}{"API_TOKEN": "secret-from-parent"},
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v; pipeline skills should not need direct steps", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var status *SkillRunStatus
	for time.Now().Before(deadline) {
		status, err = runner.GetRunStatus(runID)
		if err != nil {
			t.Fatalf("GetRunStatus() error = %v", err)
		}
		if status.Status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status == nil || status.Status != "success" {
		t.Fatalf("pipeline status = %+v, want success", status)
	}
	if len(status.Steps) != 1 || status.Steps[0].Status != "success" || !strings.Contains(status.Steps[0].Output, "pipeline-ok") || !strings.Contains(status.Steps[0].Output, "secret-from-parent") {
		t.Fatalf("pipeline steps = %#v, want child output captured", status.Steps)
	}
}

func TestSkillRunnerStartRun_PipelineCarriesNestedArgsContextToSubSkills(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("API_TOKEN", "")

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{
			Name:         "child-nested-context",
			Status:       "active",
			RequiredArgs: []string{"input"},
			RequiredEnv:  []string{"API_TOKEN"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": pipelineTestEchoCommand()},
			}},
		},
		{
			Name:   "pipeline-nested-context",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-nested-context",
			}},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-nested-context", map[string]interface{}{
		"args": map[string]interface{}{
			"input":     "nested-pipeline-ok",
			"extra_env": map[string]interface{}{"API_TOKEN": "secret-from-nested-parent"},
		},
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v; nested args context should satisfy pipeline child", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var status *SkillRunStatus
	for time.Now().Before(deadline) {
		status, err = runner.GetRunStatus(runID)
		if err != nil {
			t.Fatalf("GetRunStatus() error = %v", err)
		}
		if status.Status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status == nil || status.Status != "success" {
		t.Fatalf("pipeline status = %+v, want success", status)
	}
	if len(status.Steps) != 1 || status.Steps[0].Status != "success" || !strings.Contains(status.Steps[0].Output, "nested-pipeline-ok") || !strings.Contains(status.Steps[0].Output, "secret-from-nested-parent") {
		t.Fatalf("pipeline steps = %#v, want nested context in child output", status.Steps)
	}
}

func TestSkillRunnerStartRun_PipelineCarriesTextAliasToSubSkills(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{
			Name:         "child-text",
			Status:       "active",
			RequiredArgs: []string{"text"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo text={{text}}"},
			}},
		},
		{
			Name:   "pipeline-text",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-text",
			}},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-text", map[string]interface{}{"text": "translate me"})
	if err != nil {
		t.Fatalf("StartRun() error = %v; parent text should satisfy pipeline child", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" {
		t.Fatalf("pipeline status = %+v, want success", status)
	}
	if len(status.Steps) != 1 || !strings.Contains(status.Steps[0].Output, "translate me") {
		t.Fatalf("pipeline steps = %#v, want text alias in child output", status.Steps)
	}
}

func TestSkillRunnerStartRun_PipelineCarriesPlainArgsToTextSubSkill(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{
			Name:         "child-text",
			Status:       "active",
			RequiredArgs: []string{"text"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo text={{text}}"},
			}},
		},
		{
			Name:   "pipeline-plain-args",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-text",
			}},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-plain-args", map[string]interface{}{"args": "translate me"})
	if err != nil {
		t.Fatalf("StartRun() error = %v; plain args should satisfy child text", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" {
		t.Fatalf("pipeline status = %+v, want success", status)
	}
	if len(status.Steps) != 1 || !strings.Contains(status.Steps[0].Output, "translate me") {
		t.Fatalf("pipeline steps = %#v, want plain args in child output", status.Steps)
	}
}

func TestSkillRunnerStartRun_PipelinePropagatesCapturedVars(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
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
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-capture", nil)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" {
		t.Fatalf("pipeline status = %+v, want success", status)
	}
	if len(status.Steps) != 2 || !strings.Contains(status.Steps[1].Output, "report.md") || strings.Contains(status.Steps[1].Output, "{{capture-file.input}}") {
		t.Fatalf("pipeline steps = %#v, want async downstream step to receive captured alias", status.Steps)
	}
}

func TestSkillRunnerStartRun_PipelineChecksParentRequiredEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("API_TOKEN", "")

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
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
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	if _, err := runner.StartRun("pipeline-env", nil); err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("StartRun() error = %v, want parent required_env precheck", err)
	}

	runID, err := runner.StartRun("pipeline-env", map[string]interface{}{"env": map[string]interface{}{"API_TOKEN": "secret"}})
	if err != nil {
		t.Fatalf("StartRun() with env error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" || !strings.Contains(status.Steps[0].Output, "should-not-run") {
		t.Fatalf("pipeline status = %+v, want run-provided env to satisfy parent pipeline", status)
	}
}

func TestSkillRunnerStartRun_PipelineContinueOnFailKeepsParentSuccess(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
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
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-continue", nil)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" {
		t.Fatalf("pipeline status = %+v, want success despite continue_on_fail child", status)
	}
	if len(status.Steps) != 2 || status.Steps[0].Status != "failed" || status.Steps[1].Status != "success" {
		t.Fatalf("pipeline steps = %#v, want failed then success", status.Steps)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after run error = %v", err)
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
}

func TestSkillRunnerStartRun_PipelineContinueOnFailPropagatesFailedCapture(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{
			Name:   "child-fail-capture",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action:  "bash",
				Params:  pipelineTestFailCapturedParams(),
				Capture: map[string]string{"file": `(?m)^file=([^\r\n]+)`},
			}},
		},
		{
			Name:         "child-use-input",
			Status:       "active",
			RequiredArgs: []string{"input"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo using {{input}}"},
			}},
		},
		{
			Name:   "pipeline-continue-capture",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "child-fail-capture", ContinueOnFail: true},
				{Skill: "child-use-input", Params: map[string]string{"input": "{{child-fail-capture.input}}"}},
			},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-continue-capture", nil)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" {
		t.Fatalf("pipeline status = %+v, want success despite failed capture step", status)
	}
	if len(status.Steps) != 2 || status.Steps[0].Status != "failed" || !strings.Contains(status.Steps[1].Output, "report.md") {
		t.Fatalf("pipeline steps = %#v, want failed-step capture to feed second step", status.Steps)
	}
}

func TestSkillRunnerStartRun_PipelineStepParamsSelectChildAPIWorkflowOperation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	missingCommand := "definitely-missing-pipeline-child-workflow-command"
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
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
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-child-workflow", map[string]interface{}{"operation": "parent-danger"})
	if err != nil {
		t.Fatalf("StartRun() error = %v; child operation step param should select safe step", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" || len(status.Steps) != 1 || !strings.Contains(status.Steps[0].Output, "child-safe") || strings.Contains(status.Steps[0].Output, missingCommand) {
		t.Fatalf("pipeline status = %+v, want child api_workflow safe operation", status)
	}
}

func TestSkillRunnerStartRun_PipelineRejectsRecursion(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "pipeline-self",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "pipeline-self",
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-self", nil)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "failed" || !strings.Contains(status.Error, "pipeline recursion detected") {
		t.Fatalf("pipeline status = %+v, want recursion failure", status)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after run error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].UsageCount != 1 || cfg.NLSkills[0].FailureCount != 1 {
		t.Fatalf("usage stats = %+v, want one visible failed run", cfg.NLSkills)
	}
}

func TestSkillRunnerStartRun_PipelineRecursionPrecedesRequirementChecks(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("API_TOKEN", "")

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "pipeline-self-env",
		Status:      "active",
		Mode:        "pipeline",
		RequiredEnv: []string{"API_TOKEN"},
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "pipeline-self-env",
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)
	runArgs := cskill.WithPipelineRunStack(map[string]interface{}{}, "pipeline-self-env")

	_, err = runner.StartRun("pipeline-self-env", runArgs)
	if err == nil {
		t.Fatal("StartRun() error = nil, want recursion failure")
	}
	if !strings.Contains(err.Error(), "pipeline recursion detected") || strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("StartRun() error = %v, want recursion to short-circuit requirement checks", err)
	}
}

func TestSkillRunnerStartRun_ExternalPrivatePipelineStackDoesNotTripRecursion(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
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
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("pipeline-stack", map[string]interface{}{
		cskill.PipelineRunStackArg: []string{"pipeline-stack"},
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v; external private stack must not be trusted", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "success" || !strings.Contains(status.Steps[0].Output, "child-ok") {
		t.Fatalf("pipeline status = %+v, want forged external stack ignored", status)
	}
}

func waitSkillRunDoneForTest(t *testing.T, runner *SkillRunner, runID string) *SkillRunStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var status *SkillRunStatus
	var err error
	for time.Now().Before(deadline) {
		status, err = runner.GetRunStatus(runID)
		if err != nil {
			t.Fatalf("GetRunStatus() error = %v", err)
		}
		if status.Status != "running" {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish, last status = %+v", runID, status)
	return nil
}

func pipelineTestEchoCommand() string {
	if runtime.GOOS == "windows" {
		return "echo %API_TOKEN% {{input}}"
	}
	return "echo $API_TOKEN {{input}}"
}

func pipelineTestFailParams() map[string]interface{} {
	if runtime.GOOS == "windows" {
		return map[string]interface{}{
			"command":         "echo diagnostic & exit /b 7",
			"preferred_shell": "cmd",
		}
	}
	return map[string]interface{}{"command": "echo diagnostic; exit 7"}
}

func pipelineTestFailCapturedParams() map[string]interface{} {
	if runtime.GOOS == "windows" {
		return map[string]interface{}{
			"command":         "echo file=report.md & exit /b 7",
			"preferred_shell": "cmd",
		}
	}
	return map[string]interface{}{"command": "echo file=report.md; exit 7"}
}

func pipelineTestSlowParams() map[string]interface{} {
	return map[string]interface{}{"command": `python -c "import time; time.sleep(1.2); print('slow-done')"`}
}

func TestSkillRunnerStartRun_PrechecksOnlySelectedAPIWorkflowSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	missingCommand := "definitely-missing-skill-runner-command-unselected"
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:         "workflow-selected",
		Status:       "active",
		Mode:         "api_workflow",
		RequiredArgs: []string{"danger_input"},
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "bad-step", Params: map[string]interface{}{"command": missingCommand + " --version {{danger_input}}"}},
		},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("workflow-selected", map[string]interface{}{"operation": "safe"})
	if err != nil {
		t.Fatalf("StartRun() error = %v; unselected command %q should not block precheck", err, missingCommand)
	}
	if status := waitSkillRunDoneForTest(t, runner, runID); status.Status != "success" {
		t.Fatalf("status = %+v, want success", status)
	}
}

func TestSkillRunnerStartRun_ReadsOperationFromNestedArgs(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	missingCommand := "definitely-missing-skill-runner-command-nested-op"
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "workflow-nested-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "safe", Labels: []string{"safe-step"}},
			{Name: "danger", Labels: []string{"danger-step"}},
		},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "danger-step", Params: map[string]interface{}{"command": missingCommand + " --version"}},
		},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("workflow-nested-op", map[string]interface{}{
		"args": map[string]interface{}{"operation": "safe"},
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v; nested operation should select safe step before checking %q", err, missingCommand)
	}
	_ = runner.CancelRun(runID)
}

func TestSkillRunnerStartRun_PrechecksOnlyWhenActiveSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	skillDir := t.TempDir()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:         "conditional-precheck",
		Status:       "active",
		SkillDir:     skillDir,
		RequiredArgs: []string{"advanced_input"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			When:   "{{mode}} == advanced",
			Params: map[string]interface{}{"command": "python missing.py {{advanced_input}}"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("conditional-precheck", map[string]interface{}{"mode": "basic"})
	if err != nil {
		t.Fatalf("StartRun() error = %v; when=false step should not block precheck", err)
	}
	if status := waitSkillRunDoneForTest(t, runner, runID); status.Status != "success" {
		t.Fatalf("status = %+v, want success with skipped step", status)
	}
}

func TestSkillRunnerStartRun_PrechecksResolvedWorkingDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	skillDir := t.TempDir()
	missingDir := filepath.Join(skillDir, "missing-project")

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "dynamic-working-dir",
		Status:   "active",
		SkillDir: skillDir,
		Params:   []corelib.NLSkillParam{{Name: "project_dir", Required: true}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"working_dir": "{{project_dir}}", "command": "echo ok"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("dynamic-working-dir", map[string]interface{}{"project_dir": missingDir})
	if err == nil || !strings.Contains(err.Error(), missingDir) {
		t.Fatalf("StartRun() error = %v, want resolved missing working_dir", err)
	}
}

func TestSkillRunnerStartRun_DefaultsSingleAPIWorkflowOperation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	missingCommand := "definitely-missing-skill-runner-command-default-op"
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "workflow-default-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "bad-step", Params: map[string]interface{}{"command": missingCommand + " --version"}},
		},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("workflow-default-op", nil)
	if err != nil {
		t.Fatalf("StartRun() error = %v; single operation should default to safe step and ignore %q", err, missingCommand)
	}
	_ = runner.CancelRun(runID)
}

func TestSkillRunnerStartRun_RequiresOperationForMultipleAPIWorkflowOperations(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "workflow-choose-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "safe", Labels: []string{"safe-step"}},
			{Name: "danger", Labels: []string{"danger-step"}},
		},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "danger-step", Params: map[string]interface{}{"command": "echo danger"}},
		},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("workflow-choose-op", nil)
	if err == nil || !strings.Contains(err.Error(), "requires an operation") || !strings.Contains(err.Error(), "safe") || !strings.Contains(err.Error(), "[action: choose_operation]") {
		t.Fatalf("expected choose operation error, got %v", err)
	}
}

func TestSkillRunnerCancelDoesNotRecordFailureStats(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "cancel-stats",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: pipelineTestSlowParams(),
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runID, err := runner.StartRun("cancel-stats", nil)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if err := runner.CancelRun(runID); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	status := waitSkillRunDoneForTest(t, runner, runID)
	if status.Status != "cancelled" {
		t.Fatalf("status = %+v, want cancelled", status)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after cancel error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].UsageCount != 0 || cfg.NLSkills[0].FailureCount != 0 || cfg.NLSkills[0].SuccessCount != 0 {
		t.Fatalf("cancelled run stats = %+v, want no usage/success/failure count", cfg.NLSkills)
	}
}

func TestSkillRunnerStartRun_RejectsUnknownAPIWorkflowOperation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "workflow-unknown-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
		},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("workflow-unknown-op", map[string]interface{}{"operation": "missing"})
	if err == nil || !strings.Contains(err.Error(), `operation "missing" not found`) || !strings.Contains(err.Error(), "safe") {
		t.Fatalf("expected unknown operation error with available operations, got %v", err)
	}
}

func TestSkillRunnerExecuteStepWithContext_UnsupportedActionIsClassified(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "python",
		Params: map[string]interface{}{},
	}, "")
	if err == nil {
		t.Fatal("expected unsupported action error")
	}
	classified := cskill.ClassifyStepError(0, "", err.Error(), "")
	if classified.Class != cskill.ErrUnsupportedAction {
		t.Fatalf("Class = %s, err = %v", classified.Class, err)
	}
}
func TestSkillRunnerExecuteStepWithContext_NormalizesSupportedActionSpelling(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "craft-tool",
		Params: map[string]interface{}{},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "missing task parameter") {
		t.Fatalf("expected craft_tool dispatch after action normalization, got: %v", err)
	}
}
func TestSkillRunnerExecuteStepWithContext_CraftToolAcceptsLegacyInstructions(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "legacy task",
		},
	}, "")
	if err != nil && strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("craft_tool should not fall through unknown action: %v", err)
	}
}

func TestSkillRunnerExecuteStepWithContext_CraftToolMissingTask(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "missing task parameter") {
		t.Fatalf("expected missing task error, got: %v", err)
	}
}

func TestIsInstructionOnlySkillEntry(t *testing.T) {
	if !isInstructionOnlySkillEntry(&corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "craft_tool",
		Params: map[string]interface{}{"instructions": "# PPTX Generator\n\nGenerate slides."},
	}}}) {
		t.Fatal("expected craft_tool with instructions to require artifact verification")
	}
	if isInstructionOnlySkillEntry(&corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "craft_tool",
		Params: map[string]interface{}{"task": "generate slides"},
	}}}) {
		t.Fatal("expected structured craft_tool task not to be treated as instruction-only")
	}
}

func TestIsInstructionOnlySkillStatus(t *testing.T) {
	status := &SkillRunStatus{Steps: []StepResult{{
		Action: "craft_tool",
		Status: "success",
		Output: "📝 脚本语言: python\n📁 脚本路径: /tmp/tool.py\n\n✅ 脚本执行成功",
	}}}
	if !isInstructionOnlySkillStatus(status) {
		t.Fatal("expected craft_tool output with script metadata to require artifact verification")
	}
}

func TestSummarizeSkillRunDoesNotMarkStdoutOnlyPathMissing(t *testing.T) {
	missingReport := filepath.Join(t.TempDir(), "gold-report.md")
	status := &SkillRunStatus{
		Status: "success",
		Steps: []StepResult{{
			Action: "bash",
			Status: "success",
			Output: "Gold price report generated on stdout\n" + missingReport + "\n",
		}},
	}

	summarizeSkillRun(status)

	if status.Summary.ArtifactPath != "" || status.Summary.ArtifactStatus != "" || status.Summary.NeedsArtifactVerification {
		t.Fatalf("stdout-only summary = %#v, want no missing artifact", status.Summary)
	}
	if status.Summary.LastOutputSnippet == "" {
		t.Fatalf("stdout-only summary lost output snippet: %#v", status.Summary)
	}
}

func TestSummarizeSkillRunMarksExpectedArtifactMissing(t *testing.T) {
	missingReport := filepath.Join(t.TempDir(), "deck.pptx")
	status := &SkillRunStatus{
		Status:           "success",
		ExpectedArtifact: true,
		ExpectedOutput:   missingReport,
		Steps: []StepResult{{
			Action: "bash",
			Status: "success",
			Output: "render completed",
		}},
	}

	summarizeSkillRun(status)

	if status.Summary.ArtifactPath != missingReport || status.Summary.ArtifactStatus != "missing" {
		t.Fatalf("expected artifact summary = %#v, want missing %s", status.Summary, missingReport)
	}
}

func TestSkillRunExpectsArtifactFromContract(t *testing.T) {
	if !skillRunExpectsArtifact(&corelib.NLSkillEntry{ProducesArtifact: true}, "") {
		t.Fatal("produces_artifact should expect an artifact")
	}
	if !skillRunExpectsArtifact(&corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"verification_mode": "artifact_required"},
	}}}, "") {
		t.Fatal("artifact_required step should expect an artifact")
	}
	if skillRunExpectsArtifact(&corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"verification_mode": "artifact_optional"},
	}}}, "") {
		t.Fatal("artifact_optional stdout-only step should not expect an artifact")
	}
}

func TestSkillRunExpectsArtifactUsesSelectedExecutionSteps(t *testing.T) {
	skill := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{
		{
			Action: "bash",
			Label:  "stdout",
			Params: map[string]interface{}{"command": "echo report"},
		},
		{
			Action: "bash",
			Label:  "artifact",
			Params: map[string]interface{}{"verification_mode": "artifact_required"},
		},
	}}

	if skillRunExpectsArtifactForSteps(skill, []corelib.NLSkillStep{skill.Steps[0]}, "", false) {
		t.Fatal("unselected artifact step should not make this run expect an artifact")
	}
	if !skillRunExpectsArtifactForSteps(skill, []corelib.NLSkillStep{skill.Steps[1]}, "", false) {
		t.Fatal("selected artifact step should make this run expect an artifact")
	}
}

func TestSkillRunExpectsArtifactSelectedStepsDoNotUseGlobalContract(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		ProducesArtifact: true,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Label:  "stdout",
			Params: map[string]interface{}{"command": "echo report"},
		}},
	}

	if skillRunExpectsArtifactForSteps(skill, skill.Steps, "", false) {
		t.Fatal("selected stdout step should not inherit global produces_artifact")
	}
	if !skillRunExpectsArtifactForSteps(skill, skill.Steps, "", true) {
		t.Fatal("unselected/full run should still honor global produces_artifact")
	}
}

func TestNormalizeSkillRunVars_ArgsOverrideLegacy(t *testing.T) {
	got := normalizeSkillRunVars(map[string]interface{}{
		"args":   map[string]interface{}{"input": "new-in", "output": "new-out"},
		"input":  "old-in",
		"output": "old-out",
	})
	if got["input"] != "new-in" || got["output"] != "new-out" {
		t.Fatalf("normalizeSkillRunVars() = %#v, want args values to win", got)
	}
}

func TestNormalizeSkillRunVars_CoercesNonStringArgs(t *testing.T) {
	got := normalizeSkillRunVars(map[string]interface{}{
		"args": map[string]interface{}{"count": 3, "enabled": true, "format": "pdf"},
	})
	// Non-string values are coerced via fmt.Sprintf (aligned with TUI behavior).
	if len(got) != 3 || got["format"] != "pdf" || got["count"] != "3" || got["enabled"] != "true" {
		t.Fatalf("normalizeSkillRunVars() = %#v, want all args coerced to strings", got)
	}
}

func TestNormalizeSkillRunVars_CanonicalizesKeyShape(t *testing.T) {
	got := normalizeSkillRunVars(map[string]interface{}{
		"User Prompt": "请查询成都天气",
		"Args":        map[string]interface{}{"Input-File": "report.md"},
	})
	if got["user_prompt"] != "请查询成都天气" || got["input_file"] != "report.md" {
		t.Fatalf("normalizeSkillRunVars() = %#v, want canonical key shapes", got)
	}
}

func TestApplySkillRunInputInference_DoesNotGuessRequiredCityFromInput(t *testing.T) {
	vars := normalizeSkillRunVars(map[string]interface{}{"input": "成都"})
	skill := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}
	cskill.ApplyRunInputInference(skill, vars, map[string]interface{}{"input": "成都"})
	if vars["city"] != "" {
		t.Fatalf("city = %q, want empty without named argument", vars["city"])
	}
}

func TestApplySkillRunInputInference_FillsRequiredArgFromNamedPrompt(t *testing.T) {
	vars := normalizeSkillRunVars(map[string]interface{}{"user_prompt": "请查询 city: 上海 的天气"})
	skill := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}
	cskill.ApplyRunInputInference(skill, vars, map[string]interface{}{"user_prompt": "请查询 city: 上海 的天气"})
	if vars["city"] != "上海" {
		t.Fatalf("city = %q, want named value", vars["city"])
	}
}

func TestApplySkillRunInputInference_PromotesFileAliasToInput(t *testing.T) {
	vars := normalizeSkillRunVars(map[string]interface{}{"file": "report.md"})
	skill := &corelib.NLSkillEntry{
		RequiredArgs: []string{"input"},
		Params:       []corelib.NLSkillParam{{Name: "input", Aliases: []string{"file"}, Required: true}},
	}
	cskill.ApplyRunInputInference(skill, vars, map[string]interface{}{"file": "report.md"})
	if vars["input"] != "report.md" {
		t.Fatalf("input = %q, want file alias promoted", vars["input"])
	}
}

func TestMissingRequiredArgsHonorsCanonicalKeysAndAliases(t *testing.T) {
	missing := cskill.MissingRequiredArgs([]string{"Input-File", "text"}, map[string]string{
		"input_file": "report.md",
		"content":    "hello",
	})
	if len(missing) != 0 {
		t.Fatalf("missing = %#v, want canonical keys and aliases to satisfy required args", missing)
	}
}
func TestDetectImplicitRequiredArgsHonorsCommonAliases(t *testing.T) {
	missing := detectImplicitRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "cat {{input}}"},
	}}, map[string]string{"file": "report.md"})
	if len(missing) != 0 {
		t.Fatalf("missing = %#v, want file alias to satisfy input", missing)
	}
}

func TestDetectImplicitRequiredArgsHonorsTextAliases(t *testing.T) {
	missing := detectImplicitRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "translate {{text}}"},
	}}, map[string]string{"content": "hello"})
	if len(missing) != 0 {
		t.Fatalf("missing = %#v, want content alias to satisfy text", missing)
	}
}

func TestCollectSkillProvidedEnvReadsStepEnvMap(t *testing.T) {
	skill := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"extra_env": map[string]interface{}{"API_TOKEN": "secret"}},
	}}}
	got := cskill.CollectSkillProvidedEnv(skill)
	if got["API_TOKEN"] != "secret" {
		t.Fatalf("provided env = %#v", got)
	}
}

func TestExtractSkillRunExtraEnvAcceptsStringAssignments(t *testing.T) {
	got := cskill.ExtractRunExtraEnv("OPENAI_API_KEY=sk-test,HTTP_PROXY=http://127.0.0.1:7890")
	if got["OPENAI_API_KEY"] != "sk-test" || got["HTTP_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("extra env = %#v", got)
	}
}

func TestDetectArtifactPathFromTextAcceptsNonPDFArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	got := detectArtifactPathFromText("artifact: " + path)
	if got != path {
		t.Fatalf("artifact path = %q, want %q", got, path)
	}
}

func TestResolveSkillStep_ReplacesNestedPlaceholders(t *testing.T) {
	skillDir := filepath.Join("base", "skill")
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command":     "printf '%s %s' {{input}} ${output}",
			"working_dir": "nested",
			"arguments": map[string]interface{}{
				"path": "{{input}}",
			},
			"items": []interface{}{"${output}", 7},
		},
	}, map[string]string{"input": "report.md", "output": "out.pdf"}, skillDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "report.md") || !strings.Contains(command, "out.pdf") {
		t.Fatalf("command = %q, want placeholders replaced", command)
	}
	workDir, _ := resolved.Params["working_dir"].(string)
	wantDir := filepath.Clean(filepath.Join(skillDir, "nested"))
	if workDir != wantDir {
		t.Fatalf("working_dir = %q, want %q", workDir, wantDir)
	}
	args, _ := resolved.Params["arguments"].(map[string]interface{})
	if path, _ := args["path"].(string); !strings.Contains(path, "report.md") {
		t.Fatalf("nested argument path = %q, want replaced input", path)
	}
	items, _ := resolved.Params["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if item0, _ := items[0].(string); !strings.Contains(item0, "out.pdf") {
		t.Fatalf("items[0] = %q, want replaced output", item0)
	}
}

func TestResolveSkillStep_CraftToolInheritsRunArgs(t *testing.T) {
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "Generate slides.",
		},
	}, map[string]string{
		"output": "out/live_test_deck.pptx",
		"topic":  "Quarterly product review",
	}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := resolved.Params["output"].(string); got != "out/live_test_deck.pptx" {
		t.Fatalf("output = %q, want propagated run arg", got)
	}
	if got, _ := resolved.Params["topic"].(string); got != "Quarterly product review" {
		t.Fatalf("topic = %q, want propagated run arg", got)
	}
}

func TestResolveSkillStep_StripsMissingOptionalPlaceholder(t *testing.T) {
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node ./tool.mjs {{input}} {{output}}",
		},
	}, map[string]string{"input": "report.md"}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if strings.Contains(command, "{{output}}") || strings.Contains(command, "${output}") {
		t.Fatalf("command = %q, want unresolved output placeholder stripped", command)
	}
	if !strings.Contains(command, "report.md") {
		t.Fatalf("command = %q, want input retained", command)
	}
}

func TestSubstituteSkillVariables_LeavesUnknownPlaceholderUntouched(t *testing.T) {
	got := substituteSkillVariables("echo {{missing}}", map[string]string{"input": "ignored"})
	if got != "echo " {
		t.Fatalf("substituteSkillVariables() = %q, want unresolved placeholder removed", got)
	}
}

func TestQuoteSkillInputForShell_EscapesQuotes(t *testing.T) {
	input := "a'b"
	got := quoteSkillInputForShell(input)
	if runtime.GOOS == "windows" {
		// On Windows, quoteSkillInputForShell uses double-quotes for cmd.exe.
		if got != `"a'b"` {
			t.Fatalf("quoteSkillInputForShell() = %q, want %q", got, `"a'b"`)
		}
		return
	}
	if got != `'a'"'"'b'` {
		t.Fatalf("quoteSkillInputForShell() = %q, want %q", got, `'a'"'"'b'`)
	}
}

func TestResolveSkillStepUsesPreferredShellQuoting(t *testing.T) {
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command":         "echo {{text}}",
			"preferred_shell": "powershell",
		},
	}, map[string]string{"text": "a'b"}, "", nil)
	if err != nil {
		t.Fatalf("resolveSkillStep() error = %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "'a''b'") {
		t.Fatalf("command = %q, want PowerShell single-quote escaping", command)
	}
}

func TestWithSkillPreferredShellCopiesParams(t *testing.T) {
	params := map[string]interface{}{"command": "echo ok"}
	step := withSkillPreferredShell(corelib.NLSkillStep{Action: "bash", Params: params}, "powershell")
	if step.Params["preferred_shell"] != "powershell" {
		t.Fatalf("preferred_shell = %#v, want powershell", step.Params["preferred_shell"])
	}
	if _, mutated := params["preferred_shell"]; mutated {
		t.Fatalf("withSkillPreferredShell mutated original params: %#v", params)
	}
}

func TestInstallSkillStepProcessEnvRestoresLegacyNonBashEnv(t *testing.T) {
	const key = "MACLAW_SKILL_RUNNER_TEST_EXTRA_ENV"
	t.Setenv(key, "before")

	restore := installSkillStepProcessEnv("create_session", map[string]string{key: "during"})
	if got := os.Getenv(key); got != "during" {
		t.Fatalf("env during step = %q, want during", got)
	}
	restore()
	if got := os.Getenv(key); got != "before" {
		t.Fatalf("env after restore = %q, want before", got)
	}
	restore()
	if got := os.Getenv(key); got != "before" {
		t.Fatalf("env after second restore = %q, want before", got)
	}
}

func TestInstallSkillStepProcessEnvDoesNotInstallForCraftTool(t *testing.T) {
	const key = "MACLAW_SKILL_RUNNER_TEST_CRAFT_ENV"
	t.Setenv(key, "before")

	restore := installSkillStepProcessEnv("craft_tool", map[string]string{key: "during"})
	if got := os.Getenv(key); got != "before" {
		t.Fatalf("craft_tool env = %q, want unchanged before restore", got)
	}
	restore()
	if got := os.Getenv(key); got != "before" {
		t.Fatalf("craft_tool env after restore = %q, want before", got)
	}
}

func TestInstallSkillStepProcessEnvDoesNotInstallForBash(t *testing.T) {
	const key = "MACLAW_SKILL_RUNNER_TEST_BASH_ENV"
	t.Setenv(key, "before")

	restore := installSkillStepProcessEnv("bash", map[string]string{key: "during"})
	if got := os.Getenv(key); got != "before" {
		t.Fatalf("bash env = %q, want unchanged before restore", got)
	}
	restore()
	if got := os.Getenv(key); got != "before" {
		t.Fatalf("bash env after restore = %q, want before", got)
	}
}

func TestInstallSkillStepProcessEnvSerializesNonBashEnv(t *testing.T) {
	const key = "MACLAW_SKILL_RUNNER_TEST_SERIAL_ENV"
	t.Setenv(key, "before")

	restoreFirst := installSkillStepProcessEnv("create_session", map[string]string{key: "first"})
	if got := os.Getenv(key); got != "first" {
		t.Fatalf("first env = %q, want first", got)
	}

	acquired := make(chan func(), 1)
	go func() {
		acquired <- installSkillStepProcessEnv("create_session", map[string]string{key: "second"})
	}()

	select {
	case restoreSecond := <-acquired:
		restoreSecond()
		t.Fatal("second env overlay acquired before first restore")
	case <-time.After(50 * time.Millisecond):
	}

	restoreFirst()

	var restoreSecond func()
	select {
	case restoreSecond = <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second env overlay did not acquire after first restore")
	}
	if got := os.Getenv(key); got != "second" {
		restoreSecond()
		t.Fatalf("second env = %q, want second", got)
	}
	restoreSecond()
	if got := os.Getenv(key); got != "before" {
		t.Fatalf("env after serialized restores = %q, want before", got)
	}
}

// TestSubstituteSkillVariables_DedupsQuotedPlaceholder verifies that when a
// placeholder is already wrapped in quotes in the template (e.g. "{{text}}"),
// substituteSkillVariables does not produce double-quoting.
func TestSubstituteSkillVariables_DedupsQuotedPlaceholder(t *testing.T) {
	vars := map[string]string{"text": "Hello, how are you today?"}
	quoted := quoteSkillInputForShell("Hello, how are you today?")

	// Template with placeholder already in double quotes — common in SKILL.md
	got := substituteSkillVariables(`python translate.py --text "{{text}}"`, vars)
	want := `python translate.py --text ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() double-quoted dedup:\n  got  = %q\n  want = %q", got, want)
	}

	// Template with placeholder NOT in quotes — should still get quoted
	got2 := substituteSkillVariables(`python translate.py --text {{text}}`, vars)
	want2 := `python translate.py --text ` + quoted
	if got2 != want2 {
		t.Fatalf("substituteSkillVariables() bare placeholder:\n  got  = %q\n  want = %q", got2, want2)
	}

	// Template with placeholder in single quotes
	got3 := substituteSkillVariables(`python translate.py --text '{{text}}'`, vars)
	want3 := `python translate.py --text ` + quoted
	if got3 != want3 {
		t.Fatalf("substituteSkillVariables() single-quoted dedup:\n  got  = %q\n  want = %q", got3, want3)
	}
}

// TestSubstituteSkillVariables_DollarBraceDedup verifies dedup also works
// for ${key} style placeholders.
func TestSubstituteSkillVariables_DollarBraceDedup(t *testing.T) {
	vars := map[string]string{"city": "New York"}
	quoted := quoteSkillInputForShell("New York")

	got := substituteSkillVariables(`curl --data "${city}"`, vars)
	want := `curl --data ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() ${} dedup:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_MixedQuotedAndBare verifies that a command
// containing both quoted and bare occurrences of the same placeholder handles
// each occurrence correctly.
func TestSubstituteSkillVariables_MixedQuotedAndBare(t *testing.T) {
	vars := map[string]string{"text": "hello world"}
	quoted := quoteSkillInputForShell("hello world")

	got := substituteSkillVariables(`echo "{{text}}" && log {{text}}`, vars)
	want := `echo ` + quoted + ` && log ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() mixed quoted+bare:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_SingleBracePlaceholder verifies that {key}
// single-brace placeholders are also replaced (bug fix for manage_skill path).
func TestSubstituteSkillVariables_SingleBracePlaceholder(t *testing.T) {
	vars := map[string]string{"input": "/path/to/image.png", "output": "/path/to/out.pdf"}
	got := substituteSkillVariables("python convert.py {input} {output}", vars)
	quoted1 := quoteSkillInputForShell(vars["input"])
	quoted2 := quoteSkillInputForShell(vars["output"])
	want := "python convert.py " + quoted1 + " " + quoted2
	if got != want {
		t.Fatalf("substituteSkillVariables() single-brace:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_SingleBraceDoesNotBreakDoubleBrace verifies
// that {{key}} is still correctly replaced when {key} support is present.
func TestSubstituteSkillVariables_SingleBraceDoesNotBreakDoubleBrace(t *testing.T) {
	vars := map[string]string{"input": "test.png"}
	got := substituteSkillVariables("echo {{input}} && echo {input}", vars)
	quoted := quoteSkillInputForShell("test.png")
	want := "echo " + quoted + " && echo " + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() mixed double+single brace:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_SingleBraceQuotedDedup verifies that quoted
// single-brace placeholders like "{input}" are handled without double-quoting.
func TestSubstituteSkillVariables_SingleBraceQuotedDedup(t *testing.T) {
	vars := map[string]string{"input": "/tmp/my file.png"}
	quoted := quoteSkillInputForShell(vars["input"])

	got := substituteSkillVariables(`python convert.py "{input}"`, vars)
	want := `python convert.py ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() single-brace quoted dedup:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestUnresolvedSkillPlaceholderPattern_MatchesSingleBrace verifies that the
// unresolved placeholder regex also catches single-brace patterns.
func TestUnresolvedSkillPlaceholderPattern_MatchesSingleBrace(t *testing.T) {
	// Known vars don't include "missing", so it should be detected as unresolved.
	got := substituteSkillVariables("echo {missing}", map[string]string{"input": "ignored"})
	// GUI version strips unresolved placeholders.
	if got != "echo " {
		t.Fatalf("substituteSkillVariables() unresolved single-brace:\n  got  = %q\n  want = %q", got, "echo ")
	}
}

// TestDetectImplicitRequiredArgs_SingleBrace verifies that {input} single-brace
// placeholders are detected as missing required args.
func TestDetectImplicitRequiredArgs_SingleBrace(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "python ocr.py {input}"}},
	}
	// No vars provided — {input} should be detected as missing.
	missing := detectImplicitRequiredArgs(steps, nil)
	if len(missing) != 1 || missing[0] != "input" {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want [input]", missing)
	}
}

// TestDetectImplicitRequiredArgs_SingleBraceProvided verifies that {input}
// is NOT reported as missing when the var is provided.
func TestDetectImplicitRequiredArgs_SingleBraceProvided(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "python ocr.py {input}"}},
	}
	vars := map[string]string{"input": "/path/to/image.png"}
	missing := detectImplicitRequiredArgs(steps, vars)
	if len(missing) != 0 {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want []", missing)
	}
}

// TestDetectImplicitRequiredArgs_MixedBraceStyles verifies detection works
// with mixed {{key}}, ${key}, and {key} in the same command.
func TestDetectImplicitRequiredArgs_MixedBraceStyles(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "python convert.py {{input}} --format {format} --out ${output}",
		}},
	}
	vars := map[string]string{"input": "in.png"} // format and output missing
	missing := detectImplicitRequiredArgs(steps, vars)
	if len(missing) != 2 {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want [format, output]", missing)
	}
	// Should be sorted
	if missing[0] != "format" || missing[1] != "output" {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want [format, output]", missing)
	}
}

// ── Parameter Contract Integration Tests ──

func TestResolveSkillStep_WithParamBinding_AliasResolution(t *testing.T) {
	// Skill declares "description" with alias "content".
	// LLM passes "content" → BindParams resolves to "description".
	params := []corelib.NLSkillParam{
		{Name: "description", Aliases: []string{"content", "input"}},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node gen.js --desc {{description}}",
		},
	}, map[string]string{"content": "北京5环图"}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "北京5环图") {
		t.Fatalf("command = %q, want alias-resolved value", command)
	}
}

func TestResolveSkillStep_WithParamBinding_RequiredMissing(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Required: true},
	}
	_, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "tool {{input}}",
		},
	}, map[string]string{}, "", params)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "input") {
		t.Fatalf("error should mention 'input', got %q", err.Error())
	}
}

func TestResolveSkillStep_WithParamBinding_DefaultValue(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "format", Default: "png"},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "convert --format {{format}}",
		},
	}, map[string]string{}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "png") {
		t.Fatalf("command = %q, want default value applied", command)
	}
}

func TestResolveSkillStep_WithParamBinding_CLIFlagAppend(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node gen.js",
		},
	}, map[string]string{"format": "svg"}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "--format") || !strings.Contains(command, "svg") {
		t.Fatalf("command = %q, want CLI flag appended", command)
	}
}

func TestResolveSkillStep_WithParamBinding_CLIFlagNotDuplicated(t *testing.T) {
	// When a param has both CLIFlag and a template placeholder, the CLI flag
	// should NOT be appended (template substitution handles it).
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node gen.js --format {{format}}",
		},
	}, map[string]string{"format": "svg"}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	// Should contain --format exactly once (from template), not twice.
	count := strings.Count(command, "--format")
	if count != 1 {
		t.Fatalf("command = %q, want --format exactly once (got %d)", command, count)
	}
}

func TestResolveSkillStep_NilParams_BackwardCompatible(t *testing.T) {
	// When params is nil, resolveSkillStep should work exactly as before.
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "echo {{name}}",
		},
	}, map[string]string{"name": "world"}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "world") {
		t.Fatalf("command = %q, want substituted value", command)
	}
}

func TestSkillRunnerPersistRepairResultWritesFileBackedYAML(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "repair-file-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data := []byte("name: repair-file-skill\ndescription: A file backed skill that should persist repairs.\ntriggers:\n  - repair-file-skill\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: echo old\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "repair-file-skill", Source: "file", SkillDir: dir, FailureCount: 1}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	if err := runner.persistRepairResult(&corelib.NLSkillEntry{
		Name:               "repair-file-skill",
		Description:        "A file backed skill that should persist repairs.",
		Triggers:           []string{"repair-file-skill"},
		Source:             "file",
		SkillDir:           dir,
		Status:             "needs_review",
		Platforms:          []string{"universal"},
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo repaired"}}},
		FailureCount:       1,
		LastError:          "security blocked",
		RepairAttemptCount: 1,
		LastRepairAt:       "2026-05-14T10:00:00Z",
		RepairHistory:      []corelib.SkillRepairRecord{{Timestamp: "2026-05-14T10:00:00Z", ErrorClass: "security_scan_blocked", Explanation: "blocked", Success: false}},
	}); err != nil {
		t.Fatalf("persistRepairResult() error = %v", err)
	}

	reloaded, err := loadImportedSkillEntry(dir)
	if err != nil {
		t.Fatalf("loadImportedSkillEntry() error = %v", err)
	}
	if len(reloaded.Steps) != 1 {
		t.Fatalf("reloaded steps = %+v", reloaded.Steps)
	}
	cmd, _ := reloaded.Steps[0].Params["command"].(string)
	if cmd != "echo repaired" {
		t.Fatalf("command = %q, want repaired yaml", cmd)
	}
	if _, err := os.Stat(filepath.Join(dir, "skill.yaml.bak")); err != nil {
		t.Fatalf("expected skill.yaml.bak, stat err = %v", err)
	}
	reloadedCfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after repair error = %v", err)
	}
	if len(reloadedCfg.NLSkills) != 1 {
		t.Fatalf("config skills = %+v, want one file overlay", reloadedCfg.NLSkills)
	}
	overlay := reloadedCfg.NLSkills[0]
	if overlay.Status != "needs_review" || overlay.RepairAttemptCount != 1 || overlay.LastRepairAt == "" || len(overlay.RepairHistory) != 1 {
		t.Fatalf("file overlay repair metadata = %+v", overlay)
	}
}

func TestSkillRunnerPersistRepairResultReturnsYAMLWriteError(t *testing.T) {
	if os.Getenv("OS") == "Windows_NT" {
		t.Skip("symlink creation often requires elevated permissions on Windows")
	}
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "repair-symlink")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("name: outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "skill.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "repair-symlink", Source: "file", SkillDir: dir}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	err = runner.persistRepairResult(&corelib.NLSkillEntry{
		Name:     "repair-symlink",
		Source:   "file",
		SkillDir: dir,
		Status:   "needs_review",
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo repaired"}}},
	})
	if err == nil {
		t.Fatal("persistRepairResult() error = nil, want YAML write error")
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "name: outside\n" {
		t.Fatalf("outside file mutated to %q", string(data))
	}
}

func TestFileSkillRuntimeOverlayDoesNotPersistDefaultActiveStatus(t *testing.T) {
	active := corelib.NLSkillEntry{Name: "file-skill", Source: "file", Status: "active"}
	if fileSkillHasRuntimeOverlay(active) {
		t.Fatal("active-only file skill should not create a config overlay")
	}
	withStats := active
	withStats.UsageCount = 1
	if !fileSkillHasRuntimeOverlay(withStats) {
		t.Fatal("file skill usage stats should create an overlay")
	}
	if got := fileSkillOverlayStatus(withStats.Status); got != "" {
		t.Fatalf("active status overlay = %q, want empty so YAML remains source of truth", got)
	}
	blocked := active
	blocked.Status = "needs_review"
	if !fileSkillHasRuntimeOverlay(blocked) {
		t.Fatal("blocked file skill status should create an overlay")
	}
	if got := fileSkillOverlayStatus(blocked.Status); got != "needs_review" {
		t.Fatalf("blocked status overlay = %q", got)
	}
}

func TestWriteSkillYAMLForEntryRejectsSymlinkDefinition(t *testing.T) {
	if os.Getenv("OS") == "Windows_NT" {
		t.Skip("symlink creation often requires elevated permissions on Windows")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("name: outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "skill.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := writeSkillYAMLForEntry(dir, &corelib.NLSkillEntry{
		Name:  "repair-file-skill",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo repaired"}}},
	})
	if err == nil {
		t.Fatal("writeSkillYAMLForEntry() wrote through symlink, want error")
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "name: outside\n" {
		t.Fatalf("outside file mutated to %q", string(data))
	}
}

func TestSkillRunnerBlockedRepairRestoresOriginalSteps(t *testing.T) {
	repaired := &corelib.NLSkillEntry{
		Name:               "blocked-repair",
		Status:             "active",
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "rm -rf /"}}},
		RepairAttemptCount: 1,
		LastRepairAt:       "2026-05-14T10:00:00Z",
		RepairHistory: []corelib.SkillRepairRecord{{
			Timestamp:   "2026-05-14T10:00:00Z",
			Explanation: "replace cleanup command",
			Success:     false,
		}},
	}
	original := []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo old"}}}
	report := &cskill.ScanReport{FinalLevel: security.RiskHigh, Summary: "dangerous command"}

	markRepairBlockedBySecurity(repaired, original, report)

	if repaired.Status != "needs_review" {
		t.Fatalf("status = %q, want needs_review", repaired.Status)
	}
	cmd, _ := repaired.Steps[0].Params["command"].(string)
	if cmd != "echo old" {
		t.Fatalf("command = %q, want original safe command", cmd)
	}
	if repaired.RepairHistory[0].ErrorClass != "security_scan_blocked" {
		t.Fatalf("repair history error_class = %q", repaired.RepairHistory[0].ErrorClass)
	}
	if !strings.Contains(repaired.LastError, "level=high") {
		t.Fatalf("last_error = %q, want scan level", repaired.LastError)
	}
}

func TestSkillRunnerScanRepairedSkillBlocksDangerousRepair(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)
	entry := &corelib.NLSkillEntry{
		Name:       "dangerous-repair",
		TrustLevel: security.TrustLevelTrusted,
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{
			"command": "rm -rf /",
		}}},
	}

	report := runner.scanRepairedSkill(entry)
	if report == nil || report.FinalLevel != security.RiskCritical {
		t.Fatalf("scanRepairedSkill() report = %+v, want critical", report)
	}
	if entry.TrustLevel != security.TrustLevelTrusted {
		t.Fatalf("scanRepairedSkill mutated trust level to %q", entry.TrustLevel)
	}
}

func TestSkillRunnerTryAutoUploadUsesQualityGateAfterSingleSuccess(t *testing.T) {
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	var submitCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{server.URL}})
		case "/api/v1/skills/submit":
			submitCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"submission_id": "sub-quality"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "quality-upload-skill")
	writeLifecycleTestSkill(t, dir, "quality-upload-skill")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Quality upload skill\n\nA verified portable skill.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.RemoteViewerToken = "test-token"
	cfg.RemoteHubCenterURL = server.URL
	cfg.RemoteHubCenterURLs = []string{server.URL}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:         "quality-upload-skill",
		SkillDir:     dir,
		Source:       "file",
		Status:       "active",
		SuccessCount: 1,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillMarketClient = NewSkillMarketClient(app)
	app.autoUploadTrigger = NewAutoUploadTrigger(app.skillMarketClient, func() string { return "user@example.com" })
	app.skillLifecycle = NewSkillLifecycleManager(app)
	runner := NewSkillRunner(app.skillExecutor)
	runner.uploadTrigger = app.autoUploadTrigger

	runner.tryAutoUpload(&corelib.NLSkillEntry{Name: "quality-upload-skill", SkillDir: dir}, &skillRun{status: SkillRunStatus{
		Skill:  "quality-upload-skill",
		Status: "success",
		Steps:  []StepResult{{Status: "success"}},
	}})
	runner.tryAutoUpload(&corelib.NLSkillEntry{Name: "quality-upload-skill", SkillDir: dir}, &skillRun{status: SkillRunStatus{
		Skill:  "quality-upload-skill",
		Status: "success",
		Steps:  []StepResult{{Status: "success"}},
	}})

	items, err := app.skillLifecycle.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusUploaded || items[0].SubmissionID != "sub-quality" {
		t.Fatalf("upload queue = %+v", items)
	}
	if submitCount != 1 {
		t.Fatalf("submitCount = %d, want exactly one upload for unchanged hash", submitCount)
	}
}

func TestSkillRunnerTryAutoUploadBlocksWhenQualityGateFails(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "quality-blocked-skill")
	writeLifecycleTestSkill(t, dir, "quality-blocked-skill")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Quality blocked skill\n\nA portable skill that still needs runtime proof.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "quality-blocked-skill",
		SkillDir: dir,
		Source:   "file",
		Status:   "active",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillMarketClient = NewSkillMarketClient(app)
	app.autoUploadTrigger = NewAutoUploadTrigger(app.skillMarketClient, func() string { return "user@example.com" })
	app.skillLifecycle = NewSkillLifecycleManager(app)
	runner := NewSkillRunner(app.skillExecutor)
	runner.uploadTrigger = app.autoUploadTrigger

	runner.tryAutoUpload(&corelib.NLSkillEntry{Name: "quality-blocked-skill", SkillDir: dir}, &skillRun{status: SkillRunStatus{
		Skill:  "quality-blocked-skill",
		Status: "success",
		Steps:  []StepResult{{Status: "success"}},
	}})

	items, err := app.skillLifecycle.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusBlocked || items[0].QualityScore >= 70 {
		t.Fatalf("upload queue = %+v", items)
	}
	if !strings.Contains(items[0].LastError, "successful verification run") {
		t.Fatalf("LastError = %q, want verification quality reason", items[0].LastError)
	}
}

func TestResolveSkillWorkingDirResolvesRelativeToSkillDir(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "skill")
	got := resolveSkillWorkingDir("scripts", skillDir)
	want := filepath.Join(skillDir, "scripts")
	if got != want {
		t.Fatalf("resolveSkillWorkingDir(relative) = %q, want %q", got, want)
	}
	got = resolveSkillWorkingDir("{baseDir}/scripts", skillDir)
	if got != want {
		t.Fatalf("resolveSkillWorkingDir(baseDir) = %q, want %q", got, want)
	}
	abs := filepath.Join(t.TempDir(), "abs")
	if got := resolveSkillWorkingDir(abs, skillDir); got != filepath.Clean(abs) {
		t.Fatalf("resolveSkillWorkingDir(abs) = %q, want %q", got, filepath.Clean(abs))
	}
}

func TestFindSkillMarkdownDocPathPrefersCanonicalSkillMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Readme.md"), []byte("# readme docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("# skill docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := findSkillMarkdownDocPath(dir)
	if filepath.Base(got) != "skill.md" {
		t.Fatalf("findSkillMarkdownDocPath() = %q, want skill.md", got)
	}
	if content := loadSkillDocContent(dir); !strings.Contains(content, "skill docs") {
		t.Fatalf("loadSkillDocContent() = %q, want skill docs", content)
	}
}
