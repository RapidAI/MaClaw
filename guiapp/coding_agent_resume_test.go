package guiapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestCodingAgentResumeRoundTripKeepsTheLastLoop(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	src := filepath.Join(dir, "snake.cpp")
	body := []byte("int main(){return 0;}\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotCodingFile(src)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicalCodingPath(src)
	delta := []agent.ConversationEntry{
		{Role: "user", Content: "将结果写入result.md"},
		{Role: "assistant", ToolCalls: []interface{}{
			map[string]interface{}{"id": "call-1", "type": "function"},
		}},
		{Role: "tool", ToolCallID: "call-1", ToolName: "code_navigation", Content: "frame at snake.cpp"},
		{Role: "assistant", Content: "结果已写入 result.md"},
	}
	recordCodingAgentResume(userID, dir, delta, map[string]codingFileSnapshot{key: snap})
	recordCodingAgentResume(userID, dir, nil, nil)

	got := loadCodingAgentResume(userID, dir)
	if len(got.History) != 4 {
		t.Fatalf("history = %#v", got.History)
	}
	if got.History[0].Role != "user" || got.History[2].ToolCallID != "call-1" {
		t.Fatalf("history = %#v", got.History)
	}
	if _, ok := got.Snapshots[key]; !ok {
		t.Fatalf("snapshots = %#v", got.Snapshots)
	}

	cb := newCodingSubAgentCallbacks(&CodingSubAgent{projectPath: dir}, &TaskItem{}, "", "", nil)
	if n := cb.installRestoredReadSnapshots(dir, got.Snapshots); n != 1 {
		t.Fatalf("restored snapshots = %d", n)
	}
	if ok, msg := cb.validateReadSnapshot(src); !ok || msg != "" {
		t.Fatalf("unchanged file ok=%v msg=%q", ok, msg)
	}
	cb.trackFile(src)
	if !cb.exploredBeforeFirstEdit() {
		t.Fatal("restored read should count as exploration before this turn's edit")
	}

	if err := os.WriteFile(src, []byte("int main(){return 1;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := newCodingSubAgentCallbacks(&CodingSubAgent{projectPath: dir}, &TaskItem{}, "", "", nil)
	if n := changed.installRestoredReadSnapshots(dir, got.Snapshots); n != 0 {
		t.Fatalf("changed file restored %d snapshots", n)
	}
	if ok, _ := changed.validateReadSnapshot(src); ok {
		t.Fatal("changed file was treated as already read")
	}
}

func TestCodingAgentResumeSecondTurnKeepsEarlierEvidence(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	src := filepath.Join(dir, "snake.cpp")
	if err := os.WriteFile(src, []byte("int main(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotCodingFile(src)
	if err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "开发贪吃蛇"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "call-1"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "frame at snake.cpp"},
		{Role: "assistant", Content: "窗口版已经接上"},
		{Role: "user", Content: "keep going", HostContinuation: true},
	}, map[string]codingFileSnapshot{canonicalCodingPath(src): snap})
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "将结果写入result2.md"},
		{Role: "assistant", Content: "结果已写入 result2.md"},
	}, nil)

	got := loadCodingAgentResume(userID, dir)
	var text string
	for _, entry := range got.History {
		if entry.HostContinuation {
			t.Fatalf("host nudge was stored: %#v", entry)
		}
		if body, ok := entry.Content.(string); ok {
			text += body + "\n"
		}
	}
	if strings.Contains(text, "keep going") {
		t.Fatalf("host nudge leaked into history: %s", text)
	}
	if !strings.Contains(text, "frame at snake.cpp") || !strings.Contains(text, "result2.md") {
		t.Fatalf("history = %s", text)
	}
	if _, ok := got.Snapshots[canonicalCodingPath(src)]; !ok {
		t.Fatalf("read snapshot was dropped by the short turn: %#v", got.Snapshots)
	}
}

func TestCodingAgentResumeDropsExpiredFile(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	path := codingAgentResumeStatePath(userID)
	raw := []byte(`{"project_path":"` + filepath.ToSlash(dir) + `","updated_at_unix":1,"history":[{"role":"user","content":"old"},{"role":"assistant","content":"old answer"}]}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadCodingAgentResume(userID, dir); len(got.History) != 0 {
		t.Fatalf("expired history loaded: %#v", got.History)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired resume file still present: %v", err)
	}
}

func TestCodingAgentResumeRejectsAnotherProject(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	userID := projectSessionOwnerID(dir)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "build the game"},
		{Role: "assistant", Content: "done"},
	}, nil)
	if got := loadCodingAgentResume(userID, other); len(got.History) != 0 {
		t.Fatalf("other project loaded history: %#v", got.History)
	}
	outside := filepath.Join(other, "secret.cpp")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotCodingFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "build the game"},
		{Role: "assistant", Content: "done"},
	}, map[string]codingFileSnapshot{canonicalCodingPath(outside): snap})
	if got := loadCodingAgentResume(userID, dir); len(got.Snapshots) != 0 {
		t.Fatalf("snapshot outside the project was stored: %#v", got.Snapshots)
	}
}

func TestCodingAgentResumeDropsUnmatchedToolCalls(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "开发贪吃蛇"},
		{Role: "assistant", Content: "先确认范围", ToolCalls: []llm.ToolCall{
			{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "code_navigation", Arguments: "{}"}},
			{ID: "call-ask", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user", Arguments: "{}"}},
		}},
		{Role: "user", Content: "keep going", HostContinuation: true},
		{Role: "tool", ToolCallID: "call-1", ToolName: "code_navigation", Content: "frame at snake.cpp"},
		{Role: "tool", ToolCallID: "call-stray", Content: "unpaired result"},
	}, nil)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "将结果写入result2.md"},
		{Role: "assistant", ToolCalls: []interface{}{
			map[string]interface{}{
				"id":   "call-open",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "ask_user",
					"arguments": "{}",
				},
			},
		}},
	}, nil)

	got := loadCodingAgentResume(userID, dir)
	if len(got.History) == 0 || got.History[0].Role != "user" {
		t.Fatalf("history = %#v", got.History)
	}
	var text string
	ids := map[string]bool{}
	for i, entry := range got.History {
		if entry.HostContinuation {
			t.Fatalf("host nudge was stored: %#v", entry)
		}
		if body, ok := entry.Content.(string); ok {
			text += body + "\n"
		}
		if id := strings.TrimSpace(entry.ToolCallID); id != "" {
			ids[id] = true
		}
		callIDs := codingAgentResumeToolCallIDs(entry.ToolCalls)
		if len(callIDs) == 0 {
			continue
		}
		answered := map[string]bool{}
		for _, id := range callIDs {
			ids[id] = true
			answered[id] = false
		}
		for j := i + 1; j < len(got.History) && got.History[j].Role == "tool"; j++ {
			if _, ok := answered[got.History[j].ToolCallID]; ok {
				answered[got.History[j].ToolCallID] = true
			}
		}
		for id, ok := range answered {
			if !ok {
				t.Fatalf("open tool call %s in %#v", id, got.History)
			}
		}
	}
	if !strings.Contains(text, "frame at snake.cpp") || !strings.Contains(text, "先确认范围") {
		t.Fatalf("history = %s", text)
	}
	if strings.Contains(text, "keep going") || strings.Contains(text, "unpaired result") || strings.Contains(text, "result2.md") {
		t.Fatalf("dropped rows leaked: %s", text)
	}
	if got.History[len(got.History)-1].Role == "user" {
		t.Fatalf("resume ends on a user row: %#v", got.History)
	}
	if !ids["call-1"] || ids["call-ask"] || ids["call-open"] || ids["call-stray"] {
		t.Fatalf("tool call ids = %#v", ids)
	}
}

func TestCodingAgentResumeKeepsVerifyFixLoop(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	main := []agent.ConversationEntry{
		{Role: "user", Content: "开发贪吃蛇"},
		{Role: "assistant", Content: "已实现 snake.cpp"},
	}
	// A fix round that stored no assistant or tool output. The next model
	// loop starts with its own user message, so this row must not remain.
	bare := []agent.ConversationEntry{
		{Role: "user", Content: "验证失败但没有回复"},
	}
	fix := []agent.ConversationEntry{
		{Role: "user", Content: "验证命令失败，请修复 tick"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "call-fix", Type: "function", Function: llm.ToolCallFunction{Name: "edit_file", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "call-fix", ToolName: "edit_file", Content: "patched kTick in snake.cpp"},
		{Role: "assistant", Content: "已修复 tick"},
	}
	if got := codingAgentResumeAppendLoop(main, nil); len(got) != len(main) {
		t.Fatalf("empty extra changed the base: %#v", got)
	}
	delta := codingAgentResumeAppendLoop(main, bare)
	delta = codingAgentResumeAppendLoop(delta, fix)
	recordCodingAgentResume(userID, dir, delta, nil)

	got := loadCodingAgentResume(userID, dir)
	if len(got.History) == 0 || got.History[0].Role != "user" {
		t.Fatalf("history = %#v", got.History)
	}
	var text string
	for i, entry := range got.History {
		if i > 0 && entry.Role == "user" && got.History[i-1].Role == "user" {
			t.Fatalf("adjacent user rows: %#v", got.History)
		}
		if body, ok := entry.Content.(string); ok {
			text += body + "\n"
		}
	}
	if !strings.Contains(text, "已实现 snake.cpp") || !strings.Contains(text, "patched kTick in snake.cpp") || !strings.Contains(text, "已修复 tick") {
		t.Fatalf("fix loop was dropped: %s", text)
	}
	if strings.Contains(text, "验证失败但没有回复") {
		t.Fatalf("bare fix round was stored: %s", text)
	}
	if got.History[len(got.History)-1].Role == "user" {
		t.Fatalf("resume ends on a user row: %#v", got.History)
	}
	sawFix := false
	for i, entry := range got.History {
		if entry.ToolCallID != "call-fix" {
			continue
		}
		sawFix = true
		if i == 0 || got.History[i-1].Role != "assistant" {
			t.Fatalf("fix tool result was split: %#v", got.History)
		}
	}
	if !sawFix {
		t.Fatalf("fix tool result missing: %#v", got.History)
	}
}

func TestCodingAgentResumeReadyStartsWithTheUserTurn(t *testing.T) {
	entries := []agent.ConversationEntry{{Role: "user", Content: "将结果写入result.md"}}
	for i := 0; i < agent.MaxConversationTurns+5; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "assistant", Content: fmt.Sprintf("step %d", i)})
	}
	ready := codingAgentResumeReady(entries)
	if len(ready) == 0 || ready[0].Role != "user" {
		t.Fatalf("ready = %#v", ready)
	}
	text, _ := ready[0].Content.(string)
	if !strings.Contains(text, "result.md") {
		t.Fatalf("anchor = %q", text)
	}
	last, _ := ready[len(ready)-1].Content.(string)
	if !strings.Contains(last, fmt.Sprintf("step %d", agent.MaxConversationTurns+4)) {
		t.Fatalf("last = %q", last)
	}
}

func TestClearStickyCodingDropsAgentResume(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "build the game"},
		{Role: "assistant", Content: "done"},
	}, nil)
	if _, err := os.Stat(codingAgentResumeStatePath(userID)); err != nil {
		t.Fatal(err)
	}
	(&IMMessageHandler{}).clearStickyCodingWorkbenchMemory(userID)
	if _, err := os.Stat(codingAgentResumeStatePath(userID)); !os.IsNotExist(err) {
		t.Fatalf("resume file still present: %v", err)
	}
}

func TestCodingAgentResumeSurvivesSharedDirectoryCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "README")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")

	taskDir := filepath.Join(t.TempDir(), "tasks", "check-npu")
	siblingDir := filepath.Join(t.TempDir(), "tasks", "buddy-font")
	owner := projectSessionOwnerID(taskDir)
	sibling := projectSessionOwnerID(siblingDir)
	recordCodingAgentResume(owner, repo, []agent.ConversationEntry{
		{Role: "user", Content: "检查本机 NPU"},
		{Role: "assistant", Content: "NPU 空闲"},
	}, nil)
	recordCodingAgentResume(sibling, repo, []agent.ConversationEntry{
		{Role: "user", Content: "修复 buddy 字体"},
		{Role: "assistant", Content: "字库已接上"},
	}, nil)

	checkout := filepath.Join(t.TempDir(), "exclusive", exclusiveTaskLeaf(taskDir))
	mustGit(t, repo, "worktree", "add", "--detach", checkout)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", checkout).Run()
	})

	got := loadCodingAgentResume(owner, checkout)
	if len(got.History) != 2 || got.History[0].Content != "检查本机 NPU" || got.History[1].Content != "NPU 空闲" {
		t.Fatalf("checkout load = %#v, want the task transcript", got.History)
	}
	recordCodingAgentResume(owner, checkout, []agent.ConversationEntry{
		{Role: "user", Content: "继续"},
		{Role: "assistant", Content: "继续检查"},
	}, nil)
	back := loadCodingAgentResume(owner, repo)
	if len(back.History) != 4 || back.History[0].Content != "检查本机 NPU" || back.History[2].Content != "继续" {
		t.Fatalf("return to the shared tree dropped history: %#v", back.History)
	}
	siblingHistory := loadCodingAgentResume(sibling, repo)
	if len(siblingHistory.History) != 2 || siblingHistory.History[0].Content != "修复 buddy 字体" {
		t.Fatalf("sibling history = %#v", siblingHistory.History)
	}
}

func TestCodingAgentResumeSurvivesPrivateSandbox(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	recordCodingAgentResume(owner, repo, []agent.ConversationEntry{
		{Role: "user", Content: "整理笔记"},
		{Role: "assistant", Content: "已列出大纲"},
	}, nil)
	sandbox := filepath.Join(taskDir, "workspace")
	got := loadCodingAgentResume(owner, sandbox)
	if len(got.History) != 2 || got.History[0].Content != "整理笔记" {
		t.Fatalf("sandbox load = %#v, want the task transcript", got.History)
	}
	if other := loadCodingAgentResume(owner, t.TempDir()); len(other.History) != 0 {
		t.Fatalf("unrelated directory loaded history: %#v", other.History)
	}
}

func TestRecoveredSlotMatchesPrivateCheckoutOfSameRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "README")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")
	checkout := filepath.Join(t.TempDir(), "checkout")
	mustGit(t, repo, "worktree", "add", "--detach", checkout)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", checkout).Run()
	})
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	if !recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: repo}, checkout) {
		t.Fatal("a private checkout of the same repository was treated as a different task")
	}
	other := filepath.Join(t.TempDir(), "other-repo")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, other, "init")
	if recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: other}, checkout) {
		t.Fatal("a different repository matched this checkout")
	}
}

func TestCodingAgentResumeStoredAtSandboxDoesNotFollowAnotherDirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	sandbox := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(owner, sandbox, []agent.ConversationEntry{
		{Role: "user", Content: "只在沙箱里"},
		{Role: "assistant", Content: "记下了"},
	}, nil)
	if other := loadCodingAgentResume(owner, t.TempDir()); len(other.History) != 0 {
		t.Fatalf("unrelated directory loaded sandbox history: %#v", other.History)
	}
	got := loadCodingAgentResume(owner, sandbox)
	if len(got.History) != 2 || got.History[0].Content != "只在沙箱里" {
		t.Fatalf("sandbox reload = %#v", got.History)
	}
}

func TestRecoveredSlotDoesNotAdoptForeignDirectoryViaSandbox(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	sandbox := filepath.Join(taskDir, "workspace")
	if recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: t.TempDir()}, sandbox) {
		t.Fatal("a foreign folder matched this task's sandbox")
	}
}

func TestRecoveredSlotMatchesCachedSandboxOfClaimedDirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	claimed := filepath.Join(t.TempDir(), "shared-notes")
	if err := os.MkdirAll(claimed, 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox := filepath.Join(taskDir, "workspace")
	key := normalizeRecentTaskPathKey(taskDir)
	exclusiveCheckouts.Store(key, exclusiveCheckout{
		requested: normalizeCodingRuntimeRef(claimed),
		result:    sandbox,
		fallback:  true,
	})
	t.Cleanup(func() { exclusiveCheckouts.Delete(key) })
	if !recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: claimed}, sandbox) {
		t.Fatal("the sandbox created for this occupation was treated as a different task")
	}
	if recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: t.TempDir()}, sandbox) {
		t.Fatal("a directory outside the occupation matched the sandbox")
	}
}

func TestTaskPrivateExecutionRequiresLeafBoundary(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	leaf := exclusiveTaskLeaf(taskDir)
	checkout := filepath.Join(t.TempDir(), "exclusive", leaf)
	longer := filepath.Join(t.TempDir(), "exclusive", leaf+"-more")
	if !taskPrivateExecution(taskDir, checkout) {
		t.Fatal("checkout was not this task's private copy")
	}
	if taskPrivateExecution(taskDir, longer) {
		t.Fatal("a longer directory name was treated as this task's checkout")
	}
}

func TestCodingAgentResumeSandboxDoesNotAdoptAnotherDirectoryDuringOccupation(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	claimed := filepath.Join(t.TempDir(), "shared-notes")
	other := filepath.Join(t.TempDir(), "other-notes")
	if err := os.MkdirAll(claimed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(owner, other, []agent.ConversationEntry{
		{Role: "user", Content: "别的目录"},
		{Role: "assistant", Content: "不应被接上"},
	}, nil)
	key := normalizeRecentTaskPathKey(taskDir)
	exclusiveCheckouts.Store(key, exclusiveCheckout{
		requested: normalizeCodingRuntimeRef(claimed),
		result:    sandbox,
		fallback:  true,
	})
	t.Cleanup(func() { exclusiveCheckouts.Delete(key) })
	if got := loadCodingAgentResume(owner, sandbox); len(got.History) != 0 {
		t.Fatalf("sandbox of the claimed directory loaded another directory: %#v", got.History)
	}
	recordCodingAgentResume(owner, claimed, []agent.ConversationEntry{
		{Role: "user", Content: "整理笔记"},
		{Role: "assistant", Content: "已列出大纲"},
	}, nil)
	got := loadCodingAgentResume(owner, sandbox)
	if len(got.History) != 2 || got.History[0].Content != "整理笔记" {
		t.Fatalf("claimed directory via sandbox = %#v", got.History)
	}
}

func TestCodingAgentResumeSandboxIgnoresCheckoutOccupation(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	notes := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(owner, notes, []agent.ConversationEntry{
		{Role: "user", Content: "整理笔记"},
		{Role: "assistant", Content: "已列出大纲"},
	}, nil)
	checkout := filepath.Join(t.TempDir(), "exclusive", exclusiveTaskLeaf(taskDir))
	key := normalizeRecentTaskPathKey(taskDir)
	exclusiveCheckouts.Store(key, exclusiveCheckout{
		gitRoot:   filepath.Join(t.TempDir(), "repo"),
		requested: normalizeCodingRuntimeRef(filepath.Join(t.TempDir(), "repo")),
		result:    checkout,
		path:      checkout,
	})
	t.Cleanup(func() { exclusiveCheckouts.Delete(key) })
	sandbox := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	got := loadCodingAgentResume(owner, sandbox)
	if len(got.History) != 2 || got.History[0].Content != "整理笔记" {
		t.Fatalf("checkout occupation blocked the sandbox = %#v", got.History)
	}
}

func TestGitPathIdentitySeesRepositoryCreatedAfterAMiss(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := filepath.Join(t.TempDir(), "later")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if id := gitPathIdentity(dir); sameNormalizedPath(id.top, dir) {
		t.Fatal("directory was its own repository before init")
	}
	mustGit(t, dir, "init")
	id := gitPathIdentity(dir)
	if !sameNormalizedPath(id.top, dir) {
		t.Fatalf("git init was not visible, top=%q", id.top)
	}
}

func TestGitPathIdentityIsSharedAcrossOneRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	pkgDir := filepath.Join(repo, "pkg")
	otherDir := filepath.Join(repo, "other")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	pkg := gitPathIdentity(pkgDir)
	other := gitPathIdentity(otherDir)
	if !sameNormalizedPath(pkg.top, repo) || !sameNormalizedPath(other.top, repo) {
		t.Fatalf("pkg top=%q other top=%q repo=%q", pkg.top, other.top, repo)
	}
	if !sameNormalizedPath(pkg.common, other.common) {
		t.Fatalf("common dirs differ: %q %q", pkg.common, other.common)
	}
}

func TestCodingAgentResumeSiblingDirectoriesInsideOneRepositoryStayApart(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	pkgDir := filepath.Join(repo, "pkg")
	otherDir := filepath.Join(repo, "other")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	taskDir := filepath.Join(t.TempDir(), "tasks", "pkg-task")
	owner := projectSessionOwnerID(taskDir)
	recordCodingAgentResume(owner, pkgDir, []agent.ConversationEntry{
		{Role: "user", Content: "改 pkg"},
		{Role: "assistant", Content: "pkg 已改"},
	}, nil)
	if got := loadCodingAgentResume(owner, otherDir); len(got.History) != 0 {
		t.Fatalf("sibling directory loaded history: %#v", got.History)
	}
	if got := loadCodingAgentResume(owner, repo); len(got.History) != 2 || got.History[0].Content != "改 pkg" {
		t.Fatalf("repository root = %#v", got.History)
	}
}

func TestCodingAgentResumeCheckoutDoesNotAdoptSiblingDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	pkgDir := filepath.Join(repo, "pkg")
	otherDir := filepath.Join(repo, "other")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "-c", "core.autocrlf=false", "add", "README")
	mustGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "core.autocrlf=false", "commit", "-m", "init")
	taskDir := filepath.Join(t.TempDir(), "tasks", "pkg-task")
	owner := projectSessionOwnerID(taskDir)
	recordCodingAgentResume(owner, pkgDir, []agent.ConversationEntry{
		{Role: "user", Content: "改 pkg"},
		{Role: "assistant", Content: "pkg 已改"},
	}, nil)
	checkout := filepath.Join(t.TempDir(), "exclusive", exclusiveTaskLeaf(taskDir))
	mustGit(t, repo, "worktree", "add", "--detach", checkout)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", checkout).Run()
	})
	if err := os.MkdirAll(filepath.Join(checkout, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(checkout, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := loadCodingAgentResume(owner, filepath.Join(checkout, "pkg"))
	if len(got.History) != 2 || got.History[0].Content != "改 pkg" {
		t.Fatalf("same directory in the checkout = %#v", got.History)
	}
	if other := loadCodingAgentResume(owner, filepath.Join(checkout, "other")); len(other.History) != 0 {
		t.Fatalf("sibling directory loaded history: %#v", other.History)
	}
}

func TestCodingAgentResumeSandboxDoesNotAdoptGitSubdirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "init")
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	recordCodingAgentResume(owner, sub, []agent.ConversationEntry{
		{Role: "user", Content: "仓库里的目录"},
		{Role: "assistant", Content: "不应进沙箱"},
	}, nil)
	sandbox := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := loadCodingAgentResume(owner, sandbox); len(got.History) != 0 {
		t.Fatalf("sandbox loaded a git subdirectory: %#v", got.History)
	}
}

func TestRecoveredSlotCachedCheckoutDoesNotMatchSiblingDirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "pkg-task")
	owner := projectSessionOwnerID(taskDir)
	repo := filepath.Join(t.TempDir(), "repo")
	claimed := filepath.Join(repo, "pkg")
	other := filepath.Join(repo, "other")
	checkout := filepath.Join(t.TempDir(), "exclusive", exclusiveTaskLeaf(taskDir), "pkg")
	key := normalizeRecentTaskPathKey(taskDir)
	exclusiveCheckouts.Store(key, exclusiveCheckout{
		gitRoot:   repo,
		requested: normalizeCodingRuntimeRef(claimed),
		result:    checkout,
		path:      filepath.Dir(checkout),
	})
	t.Cleanup(func() { exclusiveCheckouts.Delete(key) })
	if recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: other}, checkout) {
		t.Fatal("a sibling directory matched this checkout")
	}
	if !recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: claimed}, checkout) {
		t.Fatal("the occupied directory did not match its checkout")
	}
}

func TestCodingAgentResumePrivateCopiesStayInTheirOwnTree(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "notes")
	owner := projectSessionOwnerID(taskDir)
	sandbox := filepath.Join(taskDir, "workspace")
	if err := os.MkdirAll(filepath.Join(sandbox, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(owner, filepath.Join(sandbox, "a"), []agent.ConversationEntry{
		{Role: "user", Content: "沙箱甲"},
		{Role: "assistant", Content: "只在甲"},
	}, nil)
	if got := loadCodingAgentResume(owner, sandbox); len(got.History) != 2 || got.History[0].Content != "沙箱甲" {
		t.Fatalf("parent of the sandbox directory = %#v", got.History)
	}
	if other := loadCodingAgentResume(owner, filepath.Join(sandbox, "b")); len(other.History) != 0 {
		t.Fatalf("sibling sandbox directory loaded history: %#v", other.History)
	}
	checkout := filepath.Join(t.TempDir(), "exclusive", exclusiveTaskLeaf(taskDir))
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := loadCodingAgentResume(owner, checkout); len(got.History) != 0 {
		t.Fatalf("exclusive checkout loaded sandbox history: %#v", got.History)
	}
}

func TestRecoveredSlotCachedCheckoutDoesNotMatchSiblingSubdirectory(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "tasks", "pkg-task")
	owner := projectSessionOwnerID(taskDir)
	claimed := filepath.Join(t.TempDir(), "repo", "pkg")
	checkout := filepath.Join(t.TempDir(), "exclusive", exclusiveTaskLeaf(taskDir), "pkg")
	key := normalizeRecentTaskPathKey(taskDir)
	exclusiveCheckouts.Store(key, exclusiveCheckout{
		requested: normalizeCodingRuntimeRef(claimed),
		result:    checkout,
		fallback:  true,
	})
	t.Cleanup(func() { exclusiveCheckouts.Delete(key) })
	if recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: filepath.Join(claimed, "a")}, filepath.Join(checkout, "b")) {
		t.Fatal("a sibling under the occupied directory matched the other sibling of the copy")
	}
	if !recoveredSlotMatchesSession(owner, &agent.UnfinishedTaskSlot{ProjectPath: filepath.Join(claimed, "a")}, filepath.Join(checkout, "a")) {
		t.Fatal("the same subdirectory did not match across the occupied tree and its copy")
	}
}

func TestGitIdentityLinesSkipHints(t *testing.T) {
	top, common := gitIdentityLines([]string{
		"hint: using a different repository",
		"D:/repo",
		".git",
	})
	if top != "D:/repo" || common != ".git" {
		t.Fatalf("top=%q common=%q", top, common)
	}
}
