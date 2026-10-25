package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestCodeSearchStaysOnLocalSurfaces(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		cb   *codingSubAgentCallbacks
	}{
		{
			name: "workbench",
			cb: &codingSubAgentCallbacks{
				subagent: &CodingSubAgent{projectPath: root, fullEnvironment: true},
				task:     &TaskItem{Title: "implement a feature", Description: "update the repository", RequestKind: codingRequestImplementation},
			},
		},
		{
			name: "inquiry",
			cb: &codingSubAgentCallbacks{
				subagent: &CodingSubAgent{projectPath: root, fullEnvironment: true},
				task:     &TaskItem{Title: "where is this", Description: "which module writes configuration", RequestKind: codingRequestInquiry},
			},
		},
		{
			name: "operational",
			cb: &codingSubAgentCallbacks{
				subagent: &CodingSubAgent{projectPath: root, fullEnvironment: true},
				task:     &TaskItem{Title: "run the app", Description: "launch the existing binary", RequestKind: codingRequestOperational},
			},
		},
		{
			name: "goal-plan",
			cb: &codingSubAgentCallbacks{
				subagent: &CodingSubAgent{projectPath: root, fullEnvironment: true},
				task:     &TaskItem{Title: "[系统续接][plan] 本轮只做规划", Description: "[系统续接][plan] 本轮只做规划"},
			},
		},
		{
			name: "explorer",
			cb: &codingSubAgentCallbacks{
				subagent: &CodingSubAgent{projectPath: root, nestDepth: 1, role: codingRoleExplorer},
				task:     &TaskItem{Title: "inspect", Description: "find the owner", RequestKind: codingRequestImplementation},
			},
		},
		{
			name: "reviewer",
			cb: &codingSubAgentCallbacks{
				subagent: &CodingSubAgent{projectPath: root, nestDepth: 1, role: codingRoleReviewer},
				task:     &TaskItem{Title: "review", Description: "check the change", RequestKind: codingRequestImplementation},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names := codingSubAgentToolDefinitionNamesForTest(tc.cb.BuildTools("work"))
			if !containsStringForTest(names, codeSearchToolName) {
				t.Fatalf("%s surface missing %s: %v", tc.name, codeSearchToolName, names)
			}
		})
	}
}

func TestCodeSearchIsAbsentFromRemoteBuildTools(t *testing.T) {
	remotes := []*remoteCodingCallbacks{
		{task: "implement the parser"},
		{agent: &RemoteCodingSubAgent{readOnlyInquiry: true}, task: "where is configuration saved"},
		{agent: &RemoteCodingSubAgent{operationalRequest: true}, task: "run the service"},
		{agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleExplorer}, task: "inspect the module"},
		{agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleReviewer}, task: "review the diff"},
	}
	for i, remote := range remotes {
		names := codingSubAgentToolDefinitionNamesForTest(remote.BuildTools("work"))
		if containsStringForTest(names, codeSearchToolName) {
			t.Fatalf("remote surface %d listed %s: %v", i, codeSearchToolName, names)
		}
	}
}

func TestCodeSearchInventoryIsLocalReadOnly(t *testing.T) {
	item, ok := codingStaticCompatibilityInventoryLookup(codingStaticCompatibilityHostLocal, codeSearchToolName)
	if !ok {
		t.Fatal("local inventory missing code_search")
	}
	if item.Capability != tool.CapabilityID("code.search") || item.Effect != tool.EffectReadOnly || item.BindingScope != "local-workspace" || item.ControlPlane {
		t.Fatalf("inventory item = %#v", item)
	}
	if _, ok := codingStaticCompatibilityInventoryLookup(codingStaticCompatibilityHostRemote, codeSearchToolName); ok {
		t.Fatal("remote inventory must not own code_search")
	}
}

func TestLocalPromptsRouteCodeSearch(t *testing.T) {
	zh := []string{
		buildFullCodingEnvironmentPromptPreamble(),
		buildNestedFullCodingEnvironmentPromptPreamble(),
		buildLocalInspectionRoleSystemPrompt(t.TempDir(), codingRoleExplorer, ""),
		buildLocalInspectionRoleSystemPrompt(t.TempDir(), codingRoleReviewer, ""),
		buildCodingSubAgentSystemPrompt(&TaskItem{Title: "implement", Description: "change code", RequestKind: codingRequestImplementation}, t.TempDir(), "", "", nil),
		buildCodingSubAgentSystemPrompt(&TaskItem{Title: "run", Description: "launch", RequestKind: codingRequestOperational}, t.TempDir(), "", "", nil),
		codingSpawnRolePromptHint(codingRoleExplorer),
		codingSpawnRolePromptHint(codingRoleReviewer),
		probeCodingWorkspace(t.TempDir()),
	}
	withIndex := t.TempDir()
	if err := os.Mkdir(filepath.Join(withIndex, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	zh = append(zh, probeCodingWorkspace(withIndex))
	inquiry := &codingSubAgentCallbacks{
		task: &TaskItem{Description: "which module saves configuration", RequestKind: codingRequestInquiry},
	}
	zh = append(zh, inquiry.buildTaskUserMessage())
	var compat strings.Builder
	appendCodingSubAgentCompatibilityChecklist(&compat)
	zh = append(zh, compat.String())
	for i, prompt := range zh {
		if !strings.Contains(prompt, codeSearchRouteZH) {
			t.Fatalf("local prompt %d missing code_search route", i)
		}
	}
	en := []string{
		buildUncorrelatedLocalCodingCompatibilityPrompt(nil),
	}
	var preflight strings.Builder
	appendCodingSubAgentPreflightChecklist(&preflight, true)
	en = append(en, preflight.String())
	for i, prompt := range en {
		if !strings.Contains(prompt, codeSearchRouteEN) {
			t.Fatalf("local English prompt %d missing code_search route", i)
		}
	}
}

func TestRemotePromptsDoNotAdvertiseCodeSearch(t *testing.T) {
	prompts := []string{
		remoteCodingSpawnRolePromptHint(codingRoleExplorer),
		remoteCodingSpawnRolePromptHint(codingRoleReviewer),
		remoteCodingSpawnRolePromptHint(codingRoleWorker),
		buildRemoteFullCodingEnvironmentPromptPreamble(),
		buildRemoteNestedFullCodingEnvironmentPromptPreamble(),
		buildRemoteCodingSystemPrompt("/srv/app", "/srv/app", "fix the parser"),
	}
	for i, prompt := range prompts {
		if strings.Contains(prompt, codeSearchToolName) {
			t.Fatalf("remote prompt %d advertised %s", i, codeSearchToolName)
		}
	}
}

func TestCodeSearchRejectsPathOutsideProject(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: root}}
	got := cb.executeToolWithOutcome(codeSearchToolName, fmt.Sprintf(`{"query":"save widget","path":%q}`, outside))
	if got.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("outside path outcome=%v text=%q", got.Outcome, got.Text)
	}
	status, summary, count := summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: true, Summary: "ok"}}, 0, cb.getGuardrailViolations(), cb.getDynamicToolsRun())
	if status != "failed" || count != 1 || !strings.Contains(summary, "guardrail") || strings.Contains(summary, "dynamic tool failed") {
		t.Fatalf("outside path should fail on the scope guardrail only, got %q, %q, %d", status, summary, count)
	}
}

func TestCodeSearchRejectsMissingProjectPath(t *testing.T) {
	root := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: root}}
	started := time.Now()
	got := cb.executeToolWithOutcome(codeSearchToolName, `{"query":"save widget","path":"test-prog"}`)
	if got.Outcome != codingToolOutcomeFailed || !strings.Contains(got.Text, "path not found: test-prog") || strings.Contains(got.Text, "freshness") {
		t.Fatalf("missing path outcome=%v text=%q", got.Outcome, got.Text)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("missing path waited %s", time.Since(started))
	}
	tools := cb.getDynamicToolsRun()
	if len(tools) != 1 || tools[0].Tool != codeSearchToolName || tools[0].Succeeded {
		t.Fatalf("missing path dynamic record = %#v", tools)
	}
	status, summary, count := summarizeSubAgentQuality("explored", "passed", true, []string{"main.go"}, nil, []CodingSubAgentCommandResult{{Command: "go test ./...", Succeeded: true, Summary: "ok"}}, 0, cb.getGuardrailViolations(), tools)
	if status != "passed" || count != 0 || strings.Contains(summary, "dynamic tool failed") {
		t.Fatalf("missing path must not fail the task, got %q, %q, %d", status, summary, count)
	}
}

func TestCodeSearchAcceptsInProjectPathWithDifferentCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths")
	}
	root := t.TempDir()
	path := filepath.Join(root, "pkg", "widget.go")
	body := "package widget\n\nfunc saveWidget() {\n\t// persist the widget to disk\n}\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	folded := foldOneProjectLetter(root)
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: root}}
	got := searchCodeUntilIndexed(t, cb, fmt.Sprintf(`{"query":"persist the widget to disk","path":%q}`, folded))
	if got.Outcome != codingToolOutcomeSuccess || strings.Contains(got.Text, "path escapes") || !strings.Contains(got.Text, "saveWidget") {
		t.Fatalf("folded root missed the file: outcome=%v text=%s", got.Outcome, got.Text)
	}
	got = searchCodeUntilIndexed(t, cb, fmt.Sprintf(`{"query":"persist the widget to disk","path":%q}`, filepath.Join(root, "PKG")))
	if got.Outcome != codingToolOutcomeSuccess || !strings.Contains(got.Text, "pkg/widget.go") {
		t.Fatalf("folded subdirectory missed the file: outcome=%v text=%s", got.Outcome, got.Text)
	}
}

func searchCodeUntilIndexed(t *testing.T, cb *codingSubAgentCallbacks, raw string) codingToolExecutionResult {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var got codingToolExecutionResult
	for {
		got = cb.executeToolWithOutcome(codeSearchToolName, raw)
		if got.Outcome != codingToolOutcomeSuccess || !strings.Contains(got.Text, `"freshness":"warming"`) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("index stayed warming: %s", got.Text)
		}
	}
}

func foldOneProjectLetter(path string) string {
	r := []rune(path)
	for i, ch := range r {
		if i < 3 {
			continue
		}
		switch {
		case ch >= 'a' && ch <= 'z':
			r[i] = ch - ('a' - 'A')
			return string(r)
		case ch >= 'A' && ch <= 'Z':
			r[i] = ch + ('a' - 'A')
			return string(r)
		}
	}
	return path
}

func TestCodeSearchDoesNotUnlockEdit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "widget.go")
	body := "package widget\n\nfunc saveWidget() {\n\t// persist the widget to disk\n}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: root}}
	got := cb.executeToolWithOutcome(codeSearchToolName, `{"query":"persist the widget to disk"}`)
	if got.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("search outcome=%v text=%q", got.Outcome, got.Text)
	}
	if !strings.Contains(got.Text, `"freshness"`) {
		t.Fatalf("search payload=%q", got.Text)
	}
	if msg := cb.requireReadBeforeModify(path, "edit_file"); msg == "" {
		t.Fatal("code_search unlocked edit_file")
	}
	if len(cb.filesRead) != 0 {
		t.Fatalf("code_search recorded a source read: %#v", cb.filesRead)
	}
}
