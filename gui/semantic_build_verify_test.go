package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func buildVerifyWorkspace(t *testing.T) (string, string) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/tmp\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return workspace, desktopUserID + ":" + workspace
}

// The reason this capability exists next to shell.execute.local is that the
// model never gets to write a command line. If a command, argument list or
// shell knob ever appears in this schema, the narrowed grant has silently
// become the wide one and there is no longer any point in having both.
func TestSemanticBuildVerifySchemaGivesTheModelNoCommandLine(t *testing.T) {
	schema := semanticTrustedBuildVerifyInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok || len(properties) != 2 {
		t.Fatalf("properties=%#v", schema["properties"])
	}
	for _, forbidden := range []string{
		"command", "args", "argv", "shell", "env", "cwd", "working_dir",
		"project_path", "timeout_seconds", "flags", "script",
	} {
		if _, present := properties[forbidden]; present {
			t.Fatalf("schema exposes %q, which hands the command line back to the model", forbidden)
		}
	}
	task, ok := properties["task"].(map[string]interface{})
	if !ok {
		t.Fatalf("task=%#v", properties["task"])
	}
	enum, ok := task["enum"].([]string)
	if !ok || len(enum) != 4 {
		t.Fatalf("task enum=%#v", task["enum"])
	}
	for _, want := range []string{"build", "test", "lint", "format_check"} {
		if !semanticBuildVerifyTaskAllowed(want) {
			t.Fatalf("reviewed task %q is not accepted", want)
		}
	}
	if required, _ := schema["required"].([]string); len(required) != 1 || required[0] != "task" {
		t.Fatalf("required=%#v", schema["required"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties=%#v", schema["additionalProperties"])
	}
}

func TestSemanticBuildVerifyArgsRejectAnythingOutsideTheClosedSet(t *testing.T) {
	if task, target, err := semanticTrustedBuildVerifyArgsAllowed(map[string]interface{}{"task": "test", "target": "corelib"}); err != nil || task != "test" || target != "corelib" {
		t.Fatalf("task=%q target=%q err=%v", task, target, err)
	}
	// A task name is compared against the reviewed set, so shell syntax in it
	// is simply not one of the four words. There is no command string for it
	// to reach even if the comparison were skipped.
	for _, rejected := range []string{"test; rm -rf /", "build && curl example.com", "publish", "deploy", "", "TEST"} {
		if _, _, err := semanticTrustedBuildVerifyArgsAllowed(map[string]interface{}{"task": rejected}); err == nil {
			t.Fatalf("task %q was accepted", rejected)
		}
	}
	for _, args := range []map[string]interface{}{
		{"task": "test", "command": "rm -rf /"},
		{"task": "test", "project_path": "C:\\other"},
		{"command": "go test ./..."},
		{"task": 7},
		{"task": "test", "target": "x", "extra": "y"},
	} {
		if _, _, err := semanticTrustedBuildVerifyArgsAllowed(args); err == nil {
			t.Fatalf("arguments %#v were accepted", args)
		}
	}
	if _, _, err := semanticTrustedBuildVerifyArgsAllowed(map[string]interface{}{"target": "corelib"}); err == nil {
		t.Fatal("a missing task was accepted")
	}
}

func TestSemanticBuildVerifyRefusesTargetsOutsideTheWorkspace(t *testing.T) {
	workspace, principal := buildVerifyWorkspace(t)
	h := &IMMessageHandler{}
	for _, escape := range []string{"..", filepath.Join("..", "elsewhere"), filepath.Dir(workspace)} {
		if _, err := h.runTrustedBuildVerify(principal, "test", escape); err == nil || !strings.Contains(err.Error(), "trusted_build_verify_target") {
			t.Fatalf("target %q err=%v", escape, err)
		}
	}
	if _, err := h.runTrustedBuildVerify(principal, "test", "go.mod"); err == nil || !strings.Contains(err.Error(), "trusted_build_verify_target_not_a_directory") {
		t.Fatalf("a file target err=%v", err)
	}
	if _, err := h.runTrustedBuildVerify("", "test", ""); err == nil || !strings.Contains(err.Error(), "trusted_build_verify_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

// Guessing a command for a project nobody recognised is how a verification
// grant turns into running something unreviewed, so an unmarked workspace is a
// refusal rather than a default.
func TestSemanticBuildVerifyFailsClosedOnUnrecognisedProject(t *testing.T) {
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace
	h := &IMMessageHandler{}
	if _, err := h.runTrustedBuildVerify(principal, "test", ""); err == nil || !strings.Contains(err.Error(), "trusted_build_verify_project_unrecognised") {
		t.Fatalf("unmarked workspace err=%v", err)
	}
	// A recognised project that has no reviewed command for the asked task is
	// also a refusal; python has no build entry.
	if _, ok := tool.BuildVerifyCommand("python", "build"); ok {
		t.Fatal("python gained a build command without review")
	}
	// Every reviewed command must be a fixed argv whose first element is a
	// program name, never a shell. A "sh -c" entry here would reopen exactly
	// the injection surface this capability exists to avoid.
	for _, kind := range tool.BuildVerifyProjectKinds() {
		for _, task := range tool.BuildVerifyTasks() {
			argv, ok := tool.BuildVerifyCommand(kind, task)
			if !ok {
				continue
			}
			switch argv[0] {
			case "sh", "bash", "cmd", "powershell", "pwsh", "zsh", "env":
				t.Fatalf("%s/%s runs through a shell: %v", kind, task, argv)
			}
			for _, arg := range argv {
				if strings.ContainsAny(arg, "|&;<>$`") {
					t.Fatalf("%s/%s carries shell syntax: %v", kind, task, argv)
				}
			}
		}
	}
}

// A package several directories down carries no marker of its own. Detecting
// only in the run directory would refuse most real targets.
func TestSemanticBuildVerifyDetectsProjectKindFromAnAncestor(t *testing.T) {
	workspace, _ := buildVerifyWorkspace(t)
	nested := filepath.Join(workspace, "corelib", "tool")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	kind, ok := tool.BuildVerifyProjectKind(workspace, nested)
	if !ok || kind != "go" {
		t.Fatalf("kind=%q ok=%v", kind, ok)
	}
	// The walk stops at the workspace root. A marker above the bound workspace
	// belongs to a project the plan never bound.
	outside := filepath.Dir(workspace)
	if _, ok := tool.BuildVerifyProjectKind(nested, outside); ok {
		t.Fatalf("detection escaped the workspace at %q", outside)
	}
}

// The end-to-end path: a reviewed task resolves to a fixed argv and runs
// without a shell. format_check is used because it is fast and its output
// names the offending file.
func TestSemanticBuildVerifyRunsTheReviewedCommandWithoutAShell(t *testing.T) {
	workspace, principal := buildVerifyWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "bad.go"), []byte("package main\nfunc  Bad()   {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	out, err := h.runTrustedBuildVerify(principal, "format_check", "")
	if err != nil {
		t.Fatalf("format_check err=%v", err)
	}
	if !strings.Contains(out, "bad.go") {
		t.Fatalf("format_check did not report the unformatted file: %q", out)
	}
	if strings.Contains(out, "toolBash") || strings.Contains(out, "project_path") {
		t.Fatalf("result leaked a legacy shell surface: %q", out)
	}
}

// The host hook is what tests and runtimes substitute. It must receive the
// reviewed task and target, never a rendered command.
func TestSemanticBuildVerifyHookReceivesTaskNotCommand(t *testing.T) {
	var gotTask, gotTarget string
	h := &IMMessageHandler{}
	h.semanticTrustedBuildVerify = func(_, task, target string) (string, error) {
		gotTask, gotTarget = task, target
		return "test passed", nil
	}
	out, err := h.runTrustedBuildVerify("user-1", "test", "corelib")
	if err != nil || out != "test passed" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if gotTask != "test" || gotTarget != "corelib" {
		t.Fatalf("hook got task=%q target=%q", gotTask, gotTarget)
	}
	// The task check runs ahead of the hook, so a substituted host cannot be
	// used to step around the reviewed set.
	if _, err := h.runTrustedBuildVerify("user-1", "deploy", ""); err == nil || !strings.Contains(err.Error(), "trusted_build_verify_task_rejected") {
		t.Fatalf("hook accepted an unreviewed task: %v", err)
	}
}
