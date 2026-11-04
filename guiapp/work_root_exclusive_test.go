package guiapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestSharedWorkRootStaysWithTheEarlierLiveSession(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	app := newProjectSearchTestApp(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	readme := filepath.Join(repo, "README")
	if err := os.WriteFile(readme, []byte("committed-head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "README")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")
	if err := os.WriteFile(readme, []byte("dirty-uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	font := filepath.Join(repo, "font.c")
	if err := os.WriteFile(font, []byte("buddy-font\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The later task is created first, so its path sorts first. Keeping the
	// tree must follow who entered the live run, not the path string.
	npu := app.createTaskRecordWithWorkingDir("检查NPU", "", []string{taskManagementTag, taskUserCreatedTag}, repo, true)
	buddy := app.createTaskRecordWithWorkingDir("buddy刷机", "", []string{taskManagementTag, taskUserCreatedTag, taskCodingDevTag}, repo, true)
	if npu.ProjectPath == "" || buddy.ProjectPath == "" {
		t.Fatal("task record was not created")
	}

	buddyOwner := projectSessionOwnerID(buddy.ProjectPath)
	npuOwner := projectSessionOwnerID(npu.ProjectPath)
	var releaseBuddy, releaseNPU bool
	t.Cleanup(func() {
		if releaseBuddy {
			endForegroundAgentOwner(buddyOwner)
		}
		if releaseNPU {
			endForegroundAgentOwner(npuOwner)
		}
		checkout := app.exclusiveCheckoutDir(npu.ProjectPath)
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", checkout).Run()
	})
	beginForegroundAgentOwner(buddyOwner)
	releaseBuddy = true
	time.Sleep(20 * time.Millisecond)
	beginForegroundAgentOwner(npuOwner)
	releaseNPU = true
	var buddyEntered, npuEntered int64
	for _, session := range liveWorkRootSessions() {
		switch normalizeProjectSessionPath(session.taskPath) {
		case normalizeProjectSessionPath(buddy.ProjectPath):
			buddyEntered = session.entered
		case normalizeProjectSessionPath(npu.ProjectPath):
			npuEntered = session.entered
		}
	}
	if buddyEntered == 0 || npuEntered == 0 || buddyEntered >= npuEntered {
		t.Fatalf("enter order buddy=%d npu=%d, want the coding session to have entered first", buddyEntered, npuEntered)
	}

	h := &IMMessageHandler{app: app}
	repoKey := normalizeProjectSessionPath(repo)
	if got := normalizeProjectSessionPath(h.projectTabWorkDirForOwner(buddyOwner)); got != repoKey {
		t.Fatalf("earlier session exec = %q, want live tree %q", got, repoKey)
	}
	if got := normalizeProjectSessionPath(app.EffectiveWorkingDirForOwner(buddyOwner)); got != repoKey {
		t.Fatalf("earlier session effective dir = %q, want live tree %q", got, repoKey)
	}

	nested := h.resolveToolWorkDirForOwner(filepath.Join(repo, "missing", "child"), npuOwner)
	if pathInsideWorkRoot(repo, nested) {
		t.Fatalf("nested missing path joined the live tree: %q", nested)
	}
	got := h.projectTabWorkDirForOwner(npuOwner)
	assertPrivateCheckout(t, repo, got)
	if normalizeProjectSessionPath(nested) != normalizeProjectSessionPath(filepath.Join(got, "missing", "child")) {
		t.Fatalf("nested missing path = %q, want it under checkout %q", nested, got)
	}
	if again := h.projectTabWorkDirForOwner(npuOwner); again != got {
		t.Fatalf("second resolve = %q, want cached %q", again, got)
	}
	if effective := app.EffectiveWorkingDirForOwner(npuOwner); effective != got {
		t.Fatalf("effective dir = %q, want the same checkout %q", effective, got)
	}
	if redirected := h.resolveToolWorkDirForOwner(repo, npuOwner); normalizeProjectSessionPath(redirected) == repoKey || pathInsideWorkRoot(repo, redirected) {
		t.Fatalf("explicit working_dir joined the live tree: %q", redirected)
	}
	if tagged := normalizeProjectSessionPath(app.recentTaskWorkingDir(npu.ProjectPath)); tagged != repoKey {
		t.Fatalf("working_dir tag = %q, want the claimed tree %q", tagged, repoKey)
	}
	listed, err := h.resolveFileToolPathForOwner(repo, npuOwner)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeProjectSessionPath(listed) != normalizeProjectSessionPath(got) {
		t.Fatalf("list_directory of the live tree = %q, want checkout %q", listed, got)
	}
	readPath, err := h.resolveFileToolPathForOwner(readme, npuOwner)
	if err != nil {
		t.Fatal(err)
	}
	if pathInsideWorkRoot(repo, readPath) {
		t.Fatalf("read_file joined the live tree: %q", readPath)
	}
	body, err := os.ReadFile(readPath)
	if err != nil || !strings.Contains(string(body), "committed-head") || strings.Contains(string(body), "dirty-uncommitted") {
		t.Fatalf("read of the live README path = %q err=%v, want the checkout copy", body, err)
	}
	primary, err := os.ReadFile(readme)
	if err != nil || !strings.Contains(string(primary), "dirty-uncommitted") {
		t.Fatalf("live tree README = %q err=%v, want the uncommitted edit left in place", primary, err)
	}
	if _, err := os.Stat(font); err != nil {
		t.Fatalf("live tree lost font.c: %v", err)
	}

	endForegroundAgentOwner(buddyOwner)
	releaseBuddy = false
	if got := normalizeProjectSessionPath(h.projectTabWorkDirForOwner(npuOwner)); got != repoKey {
		t.Fatalf("exec after the holder left = %q, want claimed tree %q", got, repoKey)
	}

	endForegroundAgentOwner(npuOwner)
	releaseNPU = false
	second := filepath.Join(repo, "SECOND")
	if err := os.WriteFile(second, []byte("second-head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "SECOND")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "second")

	beginForegroundAgentOwner(buddyOwner)
	releaseBuddy = true
	time.Sleep(20 * time.Millisecond)
	beginForegroundAgentOwner(npuOwner)
	releaseNPU = true
	// The previous checkout path must not answer this run. The holder has
	// entered again, so the snapshot is the HEAD committed above.
	refreshed := h.resolveToolWorkDirForOwner(got, npuOwner)
	secondBody, err := os.ReadFile(filepath.Join(refreshed, "SECOND"))
	if err != nil || !strings.Contains(string(secondBody), "second-head") {
		t.Fatalf("old checkout path = %q body %q err=%v, want the HEAD of the new occupation", refreshed, secondBody, err)
	}
	fresh := h.projectTabWorkDirForOwner(npuOwner)
	assertPrivateCheckout(t, repo, fresh)
	body, err = os.ReadFile(filepath.Join(fresh, "SECOND"))
	if err != nil || !strings.Contains(string(body), "second-head") {
		t.Fatalf("checkout after a new occupation = %q err=%v, want the HEAD committed after the previous run", body, err)
	}
	third := filepath.Join(repo, "THIRD")
	if err := os.WriteFile(third, []byte("moving-head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "THIRD")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "third")
	missing := h.resolveToolWorkDirForOwner(filepath.Join(repo, "not-created-yet"), npuOwner)
	if !pathInsideWorkRoot(fresh, missing) {
		t.Fatalf("missing subdir resolved to %q, want it to stay inside checkout %q", missing, fresh)
	}
	outside := h.resolveToolWorkDirForOwner(t.TempDir(), npuOwner)
	if pathInsideWorkRoot(repo, outside) || pathInsideWorkRoot(fresh, outside) {
		t.Fatalf("unrelated directory joined the occupied tree or replaced the checkout: %q", outside)
	}
	if again := h.projectTabWorkDirForOwner(npuOwner); again != fresh {
		t.Fatalf("same occupation resolved %q, want cached %q", again, fresh)
	}
	if _, err := os.Stat(filepath.Join(fresh, "THIRD")); !os.IsNotExist(err) {
		t.Fatalf("checkout followed a commit made during the same occupation, stat err=%v", err)
	}
}

func TestReopenedTaskContinuationRestoresHistoryAndIsolatesSharedWorkRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	app := newProjectSearchTestApp(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	readme := filepath.Join(repo, "README")
	if err := os.WriteFile(readme, []byte("committed-head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "README")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")
	buddyFile := filepath.Join(repo, "font.c")
	if err := os.WriteFile(buddyFile, []byte("buddy-live-uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	buddy := app.createTaskRecordWithWorkingDir("buddy刷机", "", []string{taskManagementTag, taskUserCreatedTag, taskCodingDevTag}, repo, true)
	npu := app.createTaskRecordWithWorkingDir("检查NPU", "", []string{taskManagementTag, taskUserCreatedTag}, repo, true)
	persist := app.ensureProjectTabSessionPersist()
	if err := persist.SaveSession(&TabSessionData{
		TabID:        "npu-old",
		ProjectPath:  npu.ProjectPath,
		LastActiveAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		Conversation: []interface{}{
			map[string]interface{}{"role": "user", "content": "检查本机 NPU"},
			map[string]interface{}{"role": "assistant", "content": "NPU 空闲"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if msg := app.CreateProjectTabSession("npu-reopened", npu.ProjectPath); strings.TrimSpace(msg) == "" {
		t.Fatal("reopen project tab returned empty")
	}

	buddyOwner := projectSessionOwnerID(buddy.ProjectPath)
	npuOwner := projectSessionOwnerID(npu.ProjectPath)
	t.Cleanup(func() {
		endForegroundAgentOwner(buddyOwner)
		endForegroundAgentOwner(npuOwner)
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", app.exclusiveCheckoutDir(npu.ProjectPath)).Run()
	})
	beginForegroundAgentOwner(buddyOwner)
	time.Sleep(20 * time.Millisecond)
	beginForegroundAgentOwner(npuOwner)

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	memory.Save(buddyOwner, []agent.ConversationEntry{
		{Role: "user", Content: "修复 buddy 字体并刷机"},
		{Role: "assistant", Content: "正在处理 buddy 字库"},
	})
	h := &IMMessageHandler{app: app, memory: memory}
	app.restoreProjectAgentSession(h, npuOwner, npu.ProjectPath)
	memory.Append(npuOwner, agent.ConversationEntry{Role: "user", Content: "继续"})
	continued := memory.Load(npuOwner)
	if len(continued) != 3 || continued[0].Content != "检查本机 NPU" || continued[1].Content != "NPU 空闲" || continued[2].Content != "继续" {
		t.Fatalf("continued NPU history = %#v, want only the restored NPU turns and the continuation", continued)
	}

	if got := normalizeProjectSessionPath(h.projectTabWorkDirForOwner(buddyOwner)); got != normalizeProjectSessionPath(repo) {
		t.Fatalf("buddy exec = %q, want its live tree %q", got, repo)
	}
	npuRoot := h.projectTabWorkDirForOwner(npuOwner)
	assertPrivateCheckout(t, repo, npuRoot)
	readBuddy, err := h.resolveFileToolPathForOwner(buddyFile, npuOwner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(readBuddy); !os.IsNotExist(err) {
		t.Fatalf("NPU read buddy's uncommitted file through %q, err=%v", readBuddy, err)
	}

	npuOutput, err := h.resolveFileToolPathForOwner(filepath.Join(repo, "build", "npu_probe.py"), npuOwner)
	if err != nil {
		t.Fatal(err)
	}
	if !pathInsideWorkRoot(npuRoot, npuOutput) || pathInsideWorkRoot(repo, npuOutput) {
		t.Fatalf("NPU output path = %q, want private checkout %q", npuOutput, npuRoot)
	}
	if err := os.MkdirAll(filepath.Dir(npuOutput), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(npuOutput, []byte("npu-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "build", "npu_probe.py")); !os.IsNotExist(err) {
		t.Fatalf("NPU output leaked into buddy's live tree, stat err=%v", err)
	}
	body, err := os.ReadFile(buddyFile)
	if err != nil || string(body) != "buddy-live-uncommitted\n" {
		t.Fatalf("buddy live file = %q err=%v, want its uncommitted content preserved", body, err)
	}
}

func TestBackgroundTaskGetsPrivateWorkRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	app := newProjectSearchTestApp(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("committed-head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "README")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repo, "font.c"), []byte("buddy-live-uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buddy := app.createTaskRecordWithWorkingDir("buddy刷机", "", []string{taskManagementTag, taskUserCreatedTag, taskCodingDevTag}, repo, true)
	npu := app.createTaskRecordWithWorkingDir("检查NPU", "", []string{taskManagementTag, taskUserCreatedTag}, repo, true)
	buddyOwner := projectSessionOwnerID(buddy.ProjectPath)
	npuOwner := projectSessionOwnerID(npu.ProjectPath)
	t.Cleanup(func() {
		endForegroundAgentOwner(buddyOwner)
		endWorkRootAgentOwner(npuOwner)
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", app.exclusiveCheckoutDir(npu.ProjectPath)).Run()
	})
	beginForegroundAgentOwner(buddyOwner)
	foregroundBefore := activeForegroundAgentWork()

	h := &IMMessageHandler{app: app}
	ctx := NewBackgroundLoopContext("bg-npu", SlotKindChatTask, "NPU continuation", 3, nil, nil)
	ctx.Runtime.RequestID = "req-bg-npu"
	cleanup := h.beginAgentLoopRuntime(ctx, npuOwner, "继续", desktopPlatform)
	npuRoot := h.projectTabWorkDirForOwner(npuOwner)
	assertPrivateCheckout(t, repo, npuRoot)
	if got := activeForegroundAgentWork(); got != foregroundBefore {
		t.Fatalf("background work changed foreground count from %d to %d", foregroundBefore, got)
	}
	cleanup()
	if got := activeForegroundAgentWork(); got != foregroundBefore {
		t.Fatalf("background cleanup changed foreground count from %d to %d", foregroundBefore, got)
	}
	for _, session := range liveWorkRootSessions() {
		if normalizeProjectSessionPath(session.taskPath) == normalizeProjectSessionPath(npu.ProjectPath) {
			t.Fatal("background work-root ownership survived loop cleanup")
		}
	}
}

func TestLaterCodingSessionKeepsDetachedCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	app := newProjectSearchTestApp(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	readme := filepath.Join(repo, "README")
	if err := os.WriteFile(readme, []byte("committed-head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "README")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")
	if err := os.WriteFile(readme, []byte("dirty-uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	coding := []string{taskManagementTag, taskUserCreatedTag, taskCodingDevTag}
	npu := app.createTaskRecordWithWorkingDir("检查NPU", "", coding, repo, true)
	buddy := app.createTaskRecordWithWorkingDir("buddy刷机", "", coding, repo, true)
	buddyOwner := projectSessionOwnerID(buddy.ProjectPath)
	npuOwner := projectSessionOwnerID(npu.ProjectPath)
	t.Cleanup(func() {
		endForegroundAgentOwner(buddyOwner)
		endForegroundAgentOwner(npuOwner)
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", app.exclusiveCheckoutDir(npu.ProjectPath)).Run()
	})
	beginForegroundAgentOwner(buddyOwner)
	time.Sleep(20 * time.Millisecond)
	beginForegroundAgentOwner(npuOwner)

	h := &IMMessageHandler{app: app}
	got := h.projectTabWorkDirForOwner(npuOwner)
	assertPrivateCheckout(t, repo, got)
	if again := h.projectTabWorkDirForOwner(npuOwner); again != got {
		t.Fatalf("second resolve = %q, want cached checkout %q", again, got)
	}
	if effective := app.EffectiveWorkingDirForOwner(npuOwner); effective != got {
		t.Fatalf("effective dir = %q, want the checkout %q", effective, got)
	}
	if pathInsideWorkRoot(npu.ProjectPath, got) {
		t.Fatalf("coding checkout fell into the task folder %q via %q", npu.ProjectPath, got)
	}
}

func TestTaggedWorkRootUsedWhenNoOtherSessionIsLive(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	app := newProjectSearchTestApp(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	font := filepath.Join(repo, "font.c")
	if err := os.WriteFile(font, []byte("buddy-font\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "font.c")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")
	if err := os.WriteFile(font, []byte("dirty-uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := app.createTaskRecordWithWorkingDir("检查NPU", "", []string{taskManagementTag, taskUserCreatedTag}, repo, true)
	if task.ProjectPath == "" {
		t.Fatal("task record was not created")
	}
	owner := projectSessionOwnerID(task.ProjectPath)
	t.Cleanup(func() { endForegroundAgentOwner(owner) })
	beginForegroundAgentOwner(owner)

	h := &IMMessageHandler{app: app}
	if got := normalizeProjectSessionPath(h.projectTabWorkDirForOwner(owner)); got != normalizeProjectSessionPath(repo) {
		t.Fatalf("sole session exec = %q, want tagged tree %q", got, repo)
	}
	body, err := os.ReadFile(filepath.Join(h.projectTabWorkDirForOwner(owner), "font.c"))
	if err != nil || !strings.Contains(string(body), "dirty-uncommitted") {
		t.Fatalf("sole session sees %q err=%v, want the uncommitted tree", body, err)
	}
}

func TestSharedPlainDirectoryDoesNotJoinTheLiveTree(t *testing.T) {
	app := newProjectSearchTestApp(t)
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(shared, "secret.txt")
	if err := os.WriteFile(secret, []byte("live-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	npu := app.createTaskRecordWithWorkingDir("检查NPU", "", []string{taskManagementTag, taskUserCreatedTag}, shared, true)
	buddy := app.createTaskRecordWithWorkingDir("buddy刷机", "", []string{taskManagementTag, taskUserCreatedTag}, shared, true)
	if npu.ProjectPath == "" || buddy.ProjectPath == "" {
		t.Fatal("task record was not created")
	}
	buddyOwner := projectSessionOwnerID(buddy.ProjectPath)
	npuOwner := projectSessionOwnerID(npu.ProjectPath)
	t.Cleanup(func() { endForegroundAgentOwner(buddyOwner) })
	beginForegroundAgentOwner(buddyOwner)

	h := &IMMessageHandler{app: app}
	got := h.projectTabWorkDirForOwner(npuOwner)
	if normalizeProjectSessionPath(got) == normalizeProjectSessionPath(shared) || pathInsideWorkRoot(shared, got) {
		t.Fatalf("later session joined the live directory %q via %q", shared, got)
	}
	if _, err := os.Stat(filepath.Join(got, "secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("private exec contains the live file, stat err=%v", err)
	}
	if again := h.projectTabWorkDirForOwner(npuOwner); again != got {
		t.Fatalf("second resolve = %q, want the remembered sandbox %q", again, got)
	}
	body, err := os.ReadFile(secret)
	if err != nil || !strings.Contains(string(body), "live-only") {
		t.Fatalf("live directory changed: %q err=%v", body, err)
	}
	if normalizeProjectSessionPath(h.projectTabWorkDirForOwner(buddyOwner)) != normalizeProjectSessionPath(shared) {
		t.Fatalf("holder exec = %q, want %q", h.projectTabWorkDirForOwner(buddyOwner), shared)
	}
}

func assertPrivateCheckout(t *testing.T, repo, got string) {
	t.Helper()
	if normalizeProjectSessionPath(got) == normalizeProjectSessionPath(repo) || pathInsideWorkRoot(repo, got) {
		t.Fatalf("later session joined the live tree %q via %q", repo, got)
	}
	body, err := os.ReadFile(filepath.Join(got, "README"))
	if err != nil || !strings.Contains(string(body), "committed-head") || strings.Contains(string(body), "dirty-uncommitted") {
		t.Fatalf("checkout README = %q err=%v, want HEAD", body, err)
	}
	if _, err := os.Stat(filepath.Join(got, "font.c")); !os.IsNotExist(err) {
		t.Fatalf("checkout contains the uncommitted file, stat err=%v", err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
