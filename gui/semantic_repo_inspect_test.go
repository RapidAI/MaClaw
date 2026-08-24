package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func gitInspectClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelGitInspect,
		Confidence: .98,
		ToolNames:  []string{"git_status", "git_diff", "git_commit"},
	}
}

func TestIMSemanticRepoInspectUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGitInspect)}
	h.semanticTrustedRepoInspect = func(userID string) (string, error) {
		t.Fatalf("planning must not execute inspect user=%q", userID)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前 git 状态", "lansenger", "root-repo", "turn-repo", gitInspectClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedRepoInspectAdapter || selection.FitProof.MatchedCapability != tool.CapabilityRepoInspectVCS {
		t.Fatalf("selection=%+v", selection)
	}
	if semanticSelectionRequiresReceipt(selection) {
		t.Fatalf("read-only repo inspect must not require a receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "git_status", "git_diff", "git_commit", "git_push")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if len(properties) != 0 {
		t.Fatalf("repo inspect schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"project_path", "path", "staged", "message", "file_path",
		"channel", "destination", "group_name",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing repo inspect schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedRepoInspectAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"project_path":"/tmp","staged":true}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged inspect fields=%q", got)
	}
}

func TestIMSemanticRepoInspectExecutesEmptyObjectWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGitInspect)}
	var seenUser string
	h.semanticTrustedRepoInspect = func(userID string) (string, error) {
		seenUser = userID
		return "git status:\n## main\n\ngit diff --stat:\nno unstaged differences\n\ngit diff --cached --stat:\nno staged differences", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "帮我看下现在的 diff", "lansenger", "root-repo-exec", "turn-repo-exec", gitInspectClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{}`)
	if !strings.Contains(got, "git status:") || strings.Contains(got, "git_status") || strings.Contains(got, "git_diff") {
		t.Fatalf("bound inspect=%q", got)
	}
	if seenUser != "user-1" {
		t.Fatalf("principal=%q", seenUser)
	}
	if replay := cb.ExecuteTool(name, `{}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticRepoInspectRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGitInspect)}
	h.semanticTrustedRepoInspect = func(string) (string, error) {
		return "[file_base64|text/plain]AAAA", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前 git 状态", "lansenger", "root-repo-token", "turn-repo-token", gitInspectClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"path":".","channel":"lansenger"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") && !strings.Contains(got, "trusted_repo_inspect_arguments_rejected") {
		t.Fatalf("extra field=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前 git 状态", "lansenger", "root-repo-token-2", "turn-repo-token-2", gitInspectClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_repo_inspect_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.inspectTrustedRepo(""); err == nil || !strings.Contains(err.Error(), "trusted_repo_inspect_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticRepoInspectReadsBoundWorkspaceWithoutMutating(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for trusted repo inspect")
	}
	h := &IMMessageHandler{}
	if _, err := h.inspectTrustedRepo("user-1"); err == nil || !strings.Contains(err.Error(), "trusted_repo_inspect_unavailable") {
		t.Fatalf("empty workspace err=%v", err)
	}

	workspace := t.TempDir()
	runTrustedRepoTestGit(t, workspace, "init")
	runTrustedRepoTestGit(t, workspace, "config", "user.email", "repo-inspect@example.invalid")
	runTrustedRepoTestGit(t, workspace, "config", "user.name", "Repo Inspect")
	if err := os.WriteFile(filepath.Join(workspace, "tracked.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTrustedRepoTestGit(t, workspace, "add", "tracked.txt")
	runTrustedRepoTestGit(t, workspace, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(workspace, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "new.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := desktopUserID + ":" + workspace
	head := runTrustedRepoTestGit(t, workspace, "rev-parse", "HEAD")
	out, err := h.inspectTrustedRepo(principal)
	if err != nil || !strings.Contains(out, "git status:") || !strings.Contains(out, "tracked.txt") || !strings.Contains(out, "new.txt") {
		t.Fatalf("inspect=%q err=%v", out, err)
	}
	if strings.Contains(out, "git_status") || strings.Contains(out, "git_diff") || strings.Contains(out, "[file_base64") {
		t.Fatalf("inspect leaked legacy/delivery names: %q", out)
	}
	after := runTrustedRepoTestGit(t, workspace, "rev-parse", "HEAD")
	if strings.TrimSpace(after) != strings.TrimSpace(head) {
		t.Fatalf("inspect mutated HEAD: before=%q after=%q", head, after)
	}

	plain := t.TempDir()
	notRepo, err := inspectTrustedRepoWorkspace(nil, plain)
	if err != nil || !strings.Contains(notRepo, "not a git repository") {
		t.Fatalf("non-repo=%q err=%v", notRepo, err)
	}
}

func runTrustedRepoTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
