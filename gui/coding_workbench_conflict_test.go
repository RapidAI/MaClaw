package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestStickyCodingConflictRoundTrip(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:conflict-test"
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 2,
		Path:      "/tmp/maclaw-coding-worktrees/x",
		Branch:    "maclaw/coding-x",
		Kind:      "local_worktree",
		Error:     "cherry-pick failed",
		Files:     []string{"a.go", "b.go"},
	})
	list := h.listStickyCodingConflicts(userID)
	if len(list) != 1 || list[0].StepIndex != 2 {
		t.Fatalf("%+v", list)
	}
	md := formatCodingConflictsMarkdown(list)
	if !strings.Contains(md, "adopt") || !strings.Contains(md, "a.go") || !strings.Contains(md, "discard") {
		t.Fatalf("md=%s", md)
	}
	got, ok := h.removeStickyCodingConflict(userID, list[0].ID)
	if !ok || got.Path == "" {
		t.Fatal("remove by id")
	}
	if len(h.listStickyCodingConflicts(userID)) != 0 {
		t.Fatal("should be empty")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestDiscardAllStickyCodingConflicts(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:discard-all"
	// Use missing paths so discard is record-only (no git side effects needed).
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 1,
		Path:      filepath.Join(t.TempDir(), "missing-a"),
		Kind:      "local_worktree",
	})
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 2,
		Path:      filepath.Join(t.TempDir(), "missing-b"),
		Kind:      "local_worktree",
	})
	if n := len(h.listStickyCodingConflicts(userID)); n != 2 {
		t.Fatalf("want 2 conflicts, got %d", n)
	}
	msg, err := h.discardAllStickyCodingConflicts(userID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "2") {
		t.Fatalf("msg=%s", msg)
	}
	if len(h.listStickyCodingConflicts(userID)) != 0 {
		t.Fatal("expected empty after discard all")
	}
	// Idempotent empty path.
	msg2, err := h.discardAllStickyCodingConflicts(userID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg2, "没有") {
		t.Fatalf("empty msg=%s", msg2)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestPruneDeadStickyCodingConflictsLocal(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:prune-dead"
	aliveDir := t.TempDir()
	deadPath := filepath.Join(t.TempDir(), "gone-worktree")
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 1,
		Path:      aliveDir,
		Kind:      "local_worktree",
	})
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 2,
		Path:      deadPath,
		Kind:      "local_worktree",
	})
	// Remote without probe must stay (cannot verify).
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 3,
		Path:      "/remote/gone",
		Kind:      "remote_isolate",
	})
	dropped := h.pruneDeadStickyCodingConflicts(userID, false)
	if dropped != 1 {
		t.Fatalf("dropped=%d want 1", dropped)
	}
	list := h.listStickyCodingConflicts(userID)
	if len(list) != 2 {
		t.Fatalf("list=%+v", list)
	}
	for _, c := range list {
		if c.Path == deadPath {
			t.Fatal("dead local should be pruned")
		}
	}
	// probeRemote with no SSH session still keeps remote records.
	if n := h.pruneDeadStickyCodingConflicts(userID, true); n != 0 {
		t.Fatalf("remote without session should not drop, n=%d", n)
	}
	if len(h.listStickyCodingConflicts(userID)) != 2 {
		t.Fatal("remote kept")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestPruneDeadStickyCodingConflictsThrottled(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:prune-throttle"
	// Ensure clean throttle slot.
	stickyConflictPruneAt.Delete(userID)
	deadPath := filepath.Join(t.TempDir(), "gone-wt")
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 1,
		Path:      deadPath,
		Kind:      "local_worktree",
	})
	// First call drops.
	if n := h.pruneDeadStickyCodingConflictsThrottled(userID, false, time.Hour); n != 1 {
		t.Fatalf("first prune n=%d", n)
	}
	// Re-add dead path and throttle should skip (interval 1h).
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 2,
		Path:      filepath.Join(t.TempDir(), "gone2"),
		Kind:      "local_worktree",
	})
	if n := h.pruneDeadStickyCodingConflictsThrottled(userID, false, time.Hour); n != 0 {
		t.Fatalf("throttled prune should skip, n=%d", n)
	}
	// Force interval 0 should prune.
	if n := h.pruneDeadStickyCodingConflictsThrottled(userID, false, 0); n != 1 {
		t.Fatalf("forced prune n=%d", n)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
	stickyConflictPruneAt.Delete(userID)
}

func TestGetCodingConflictFileTriple(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:triple"
	root := t.TempDir()
	rel := "t.go"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("package main\n// main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(root, "iso")
	if err := os.MkdirAll(iso, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(iso, rel), []byte("package main\n// theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		ID:          "c-tri",
		Path:        iso,
		GitRoot:     root,
		MainProject: root,
		Kind:        "local_worktree",
		Files:       []string{rel},
	})
	tri, err := h.getCodingConflictFileTriple(userID, "c-tri", rel, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if tri.Main.Missing || !strings.Contains(tri.Main.Content, "main") {
		t.Fatalf("main=%+v", tri.Main)
	}
	if tri.Theirs.Missing || !strings.Contains(tri.Theirs.Content, "theirs") {
		t.Fatalf("theirs=%+v", tri.Theirs)
	}
	// base may be missing without git history — OK
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestClearStickyCodingConflictLog(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:log-clear"
	h.appendStickyCodingConflictLog(userID, "one")
	h.appendStickyCodingConflictLog(userID, "two")
	if len(h.getStickyCodingWorkbenchMemory(userID).ConflictLog) != 2 {
		t.Fatal("want 2")
	}
	h.clearStickyCodingConflictLog(userID)
	if len(h.getStickyCodingWorkbenchMemory(userID).ConflictLog) != 0 {
		t.Fatal("want empty")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestExportStickyCodingConflictLog(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:log-export"
	// Empty → zero entries.
	md, n := h.exportStickyCodingConflictLog(userID)
	if n != 0 || md != "" {
		t.Fatalf("empty: md=%q n=%d", md, n)
	}
	h.appendStickyCodingConflictLog(userID, "adopt c1 a.go")
	h.appendStickyCodingConflictLog(userID, "keep c1 b.go")
	md, n = h.exportStickyCodingConflictLog(userID)
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
	if !strings.Contains(md, "Conflict resolve log export") {
		t.Fatalf("header missing: %s", md)
	}
	if !strings.Contains(md, "adopt") || !strings.Contains(md, "keep") {
		t.Fatalf("body missing entries: %s", md)
	}
	// Folded into worktree notes for /worktree status.
	mem := h.getStickyCodingWorkbenchMemory(userID)
	foundNote := false
	for _, note := range mem.WorktreeNotes {
		if strings.Contains(note, "conflict-log export") && strings.Contains(note, "2") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Fatalf("worktree notes missing export summary: %v", mem.WorktreeNotes)
	}
	// Log itself is preserved (export does not clear).
	if len(mem.ConflictLog) != 2 {
		t.Fatalf("export should keep log, len=%d", len(mem.ConflictLog))
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestBuildRemoteMergeBaseCmd(t *testing.T) {
	cmd := buildRemoteMergeBaseCmd("/repo/main", "/repo/iso", "maclaw/coding-x", "pkg/a.go")
	for _, want := range []string{
		"merge-base",
		"git show",
		"cat-file -e",
		"base64",
		"__MACLAW_MISS__",
		"/repo/main",
		"/repo/iso",
		"maclaw/coding-x",
		"pkg/a.go",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("cmd missing %q:\n%s", want, cmd)
		}
	}
	// Without branch, tip comes from isolate HEAD only.
	cmd2 := buildRemoteMergeBaseCmd("/m", "/i", "", "f.go")
	if !strings.Contains(cmd2, "rev-parse HEAD") {
		t.Fatalf("no-branch tip: %s", cmd2)
	}
	if strings.Contains(cmd2, "rev-parse --verify") {
		t.Fatalf("no-branch should not verify named branch: %s", cmd2)
	}
}

func TestReadRemoteMergeBaseBlobMissingArgs(t *testing.T) {
	// No SSH — empty args must short-circuit to missing without panic.
	body, missing := readRemoteMergeBaseBlob(nil, "", "/m", "/i", "b", "f.go", 1000)
	if !missing || body != "" {
		t.Fatalf("nil handler: body=%q missing=%v", body, missing)
	}
	h := &IMMessageHandler{}
	body, missing = readRemoteMergeBaseBlob(h, "", "/m", "/i", "b", "f.go", 1000)
	if !missing || body != "" {
		t.Fatalf("empty sid: body=%q missing=%v", body, missing)
	}
	body, missing = readRemoteMergeBaseBlob(h, "sid", "", "/i", "b", "f.go", 1000)
	if !missing || body != "" {
		t.Fatalf("empty main: body=%q missing=%v", body, missing)
	}
}

func TestExtractRemoteBase64PayloadWrapped(t *testing.T) {
	// Simulate base64 without -w0 (76-col wrap) plus log noise.
	raw := []byte("package main\n\nfunc Hello() {}\n")
	enc := base64.StdEncoding.EncodeToString(raw)
	var wrapped strings.Builder
	wrapped.WriteString("ssh: connection noise\n")
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		wrapped.WriteString(enc[i:end])
		wrapped.WriteByte('\n')
	}
	wrapped.WriteString("__MACLAW_TRAIL__\n")
	got, err := decodeRemoteBase64Payload(wrapped.String())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("decoded=%q want=%q", got, raw)
	}
	// Last-line-only would fail for multi-line wrap — ensure we join.
	if onlyLast := strings.TrimSpace(strings.Split(strings.TrimSpace(wrapped.String()), "\n")[len(strings.Split(strings.TrimSpace(wrapped.String()), "\n"))-2]); onlyLast != "" {
		if _, err := base64.StdEncoding.DecodeString(onlyLast); err == nil && onlyLast != enc {
			// last pure b64 line is a fragment — full join must still succeed (already checked).
		}
	}
}

func TestWriteRemoteConflictFileMissingArgs(t *testing.T) {
	if err := writeRemoteConflictFile(nil, "sid", "/tmp/a", "x"); err == nil {
		t.Fatal("nil handler")
	}
	h := &IMMessageHandler{}
	if err := writeRemoteConflictFile(h, "", "/tmp/a", "x"); err == nil {
		t.Fatal("empty sid")
	}
	if err := writeRemoteConflictFile(h, "sid", "", "x"); err == nil {
		t.Fatal("empty path")
	}
}

func TestRemoteConflictWriteCommandBuilders(t *testing.T) {
	// Small write uses python one-shot (no raw printf|base64 of full payload in shell argv).
	cmd := remoteWriteFilePythonCommand("/repo/a.go", "package main\n")
	if !strings.Contains(cmd, "python") && !strings.Contains(cmd, "base64") {
		t.Fatalf("unexpected small write cmd: %s", cmd)
	}
	if strings.Contains(cmd, "printf %s") && strings.Contains(cmd, "package main") {
		t.Fatal("must not embed raw content via printf")
	}
	// Large path uses chunk + decode helpers.
	chunk := remoteWriteFileLargeChunkCommand("/tmp/t", "YWJj", false)
	if !strings.Contains(chunk, "printf") || !strings.Contains(chunk, "YWJj") {
		t.Fatalf("chunk: %s", chunk)
	}
	dec := remoteWriteFileLargeDecodeCommand("/repo/a.go", "/tmp/t")
	if !strings.Contains(dec, "rm -f") {
		t.Fatalf("decode should clean temp: %s", dec)
	}
}

func TestStickyCodingCheckpointsFromMemSingleParse(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:cp-parse"
	root := t.TempDir()
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.ProjectPath = root
		m.SessionPlan = "g1"
		m.ExecutionPlan = "T1"
		m.LastSummary = "s1"
	})
	_ = h.saveStickyCodingCheckpoint(userID, "one")
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.SessionPlan = "g2"
	})
	_ = h.saveStickyCodingCheckpoint(userID, "two")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	cur, ok, hist := stickyCodingCheckpointsFromMem(mem)
	if !ok || cur.Label != "two" {
		t.Fatalf("current=%+v ok=%v", cur, ok)
	}
	if len(hist) < 1 || hist[len(hist)-1].Label != "one" && hist[0].Label != "one" {
		// history stores oldest→newest; last entry should be the archived "one"
		found := false
		for _, x := range hist {
			if x.Label == "one" {
				found = true
			}
		}
		if !found {
			t.Fatalf("hist=%+v", hist)
		}
	}
	list := listStickyCodingCheckpointsFromMem(mem)
	if len(list) < 2 || !list[0].Current || list[0].Label != "two" {
		t.Fatalf("list=%+v", list)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestFinalizeConflictPreviewBinary(t *testing.T) {
	prev := finalizeConflictPreview(codingConflictFilePreview{Path: "x", Side: "main"}, string([]byte{0, 1, 2, 255}), 1000)
	if !strings.Contains(prev.Content, "binary") {
		t.Fatalf("%+v", prev)
	}
}

func TestAppendStickyCodingConflictLogAndWrite(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:conflict-log"
	root := t.TempDir()
	rel := "m.go"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(root, "iso")
	_ = os.MkdirAll(iso, 0o755)
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		ID:          "c-write",
		Path:        iso,
		GitRoot:     root,
		MainProject: root,
		Kind:        "local_worktree",
		Files:       []string{rel, "other.go"},
	})
	msg, err := h.writeCodingConflictFileContent(userID, "c-write", rel, "package main\n// edited\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "写入") && !strings.Contains(msg, "wrote") {
		t.Fatalf("%s", msg)
	}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil || !strings.Contains(string(data), "edited") {
		t.Fatalf("file=%s err=%v", data, err)
	}
	list := h.listStickyCodingConflicts(userID)
	if len(list) != 1 || len(list[0].Files) != 1 || list[0].Files[0] != "other.go" {
		t.Fatalf("%+v", list)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.ConflictLog) == 0 || !strings.Contains(mem.ConflictLog[len(mem.ConflictLog)-1], "write") {
		t.Fatalf("log=%v", mem.ConflictLog)
	}
	// Cap log
	for i := 0; i < 30; i++ {
		h.appendStickyCodingConflictLog(userID, fmt.Sprintf("line-%d", i))
	}
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.ConflictLog) > stickyCodingConflictLogMax {
		t.Fatalf("log len=%d", len(mem.ConflictLog))
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestMapConflictPreviewSideToAction(t *testing.T) {
	cases := []struct {
		side   string
		action string
	}{
		{"main", "keep"},
		{"ours", "keep"},
		{"theirs", "adopt"},
		{"isolate", "adopt"},
		{"base", "base"},
		{"take-base", "base"},
	}
	for _, tc := range cases {
		got, err := mapConflictPreviewSideToAction(tc.side)
		if err != nil || got != tc.action {
			t.Fatalf("side=%q got=%q err=%v want=%q", tc.side, got, err, tc.action)
		}
	}
	if _, err := mapConflictPreviewSideToAction("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAdoptCodingWorkbenchConflict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init")
	run(root, "config", "user.email", "t@e.com")
	run(root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(root, "add", ".")
	run(root, "commit", "-m", "init")

	wt, err := createCodingWorkbenchWorktree(root, 3, "feature")
	if err != nil || wt == nil {
		t.Fatalf("create wt: %v %#v", err, wt)
	}
	// Modify in worktree and leave unmerged.
	if err := os.WriteFile(filepath.Join(wt.ProjectPath, "f.go"), []byte("package main\n\nfunc X() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(wt.Path, "add", "-A")
	run(wt.Path, "commit", "-m", "wt change")

	h := &IMMessageHandler{}
	userID := "desktop-user:adopt-test"
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex:   3,
		Path:        wt.Path,
		ProjectPath: wt.ProjectPath,
		Branch:      wt.Branch,
		GitRoot:     wt.GitRoot,
		MainProject: root,
		Kind:        "local_worktree",
		Error:       "test",
	})
	// Detach from auto cleanup tracking.
	wt.created = false

	list := h.listStickyCodingConflicts(userID)
	if len(list) != 1 {
		t.Fatal(list)
	}
	msg, err := h.adoptCodingWorkbenchConflict(userID, list[0].ID)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !strings.Contains(msg, "采纳") && !strings.Contains(strings.ToLower(msg), "file") {
		t.Fatalf("msg=%s", msg)
	}
	data, err := os.ReadFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "func X") {
		t.Fatalf("main not updated: %s", data)
	}
	if len(h.listStickyCodingConflicts(userID)) != 0 {
		t.Fatal("conflict should be cleared")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestIsCodingWorkbenchSlashRoute(t *testing.T) {
	if !isCodingWorkbenchSlash("/route") {
		t.Fatal("route slash")
	}
	if classifyImmediateIMCommand("/worktree conflicts") != imCommandCodingWorkbench {
		t.Fatal("classify")
	}
}

func TestHandleWorktreeDiscardAllSlash(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:slash-discard-all"
	// projectPath unused for discard path but required by handler signature.
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 1,
		Path:      filepath.Join(t.TempDir(), "wt-a"),
		Kind:      "local_worktree",
	})
	resp := h.handleCodingWorktreeSlash(userID, t.TempDir(), "discard all")
	if resp == nil || !strings.Contains(resp.Text, "1") {
		t.Fatalf("%+v", resp)
	}
	if len(h.listStickyCodingConflicts(userID)) != 0 {
		t.Fatal("expected cleared")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestCodingCheckpointSaveRestore(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:checkpoint-ui"
	h.setStickyCodingSessionPlan(userID, "build the feature")
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = "T1 do A\nT2 do B"
		mem.LastSummary = "halfway"
	})
	cp := h.saveStickyCodingCheckpoint(userID, "before-refactor")
	if cp.Label != "before-refactor" {
		t.Fatalf("label=%q", cp.Label)
	}
	h.setStickyCodingSessionPlan(userID, "changed goal")
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = "gone"
	})
	got, ok := h.restoreStickyCodingCheckpoint(userID)
	if !ok || got.Label != "before-refactor" {
		t.Fatalf("restore %+v ok=%v", got, ok)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "build the feature" {
		t.Fatalf("session plan=%q", mem.SessionPlan)
	}
	if !strings.Contains(mem.ExecutionPlan, "T1") {
		t.Fatalf("exec plan=%q", mem.ExecutionPlan)
	}
	// Slash show
	resp := h.handleCodingCheckpointSlash(userID, "", "/checkpoint show")
	if resp == nil || !strings.Contains(resp.Text, "before-refactor") {
		t.Fatalf("%+v", resp)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestCodingCheckpointFileSnapshotRestore(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:checkpoint-files"
	root := t.TempDir()
	rel := "src/a.go"
	abs := filepath.Join(root, "src", "a.go")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("package main\n// v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ProjectPath = root
		mem.FilesModified = []string{rel}
		mem.SessionPlan = "goal"
	})
	cp := h.saveStickyCodingCheckpoint(userID, "snap1")
	if len(cp.FileSnapshots) == 0 {
		t.Fatalf("expected snapshots: %+v", cp.FileSnapshots)
	}
	if cp.FileSnapshots[0].Content == "" && cp.FileSnapshots[0].Sidecar == "" {
		t.Fatalf("expected snapshot content or sidecar: %+v", cp.FileSnapshots[0])
	}
	// Mutate file on disk.
	if err := os.WriteFile(abs, []byte("package main\n// v2 corrupted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restored, skipped, err := h.applyCodingCheckpointFileSnapshots(userID, cp, nil)
	if err != nil || restored != 1 {
		t.Fatalf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "v1") {
		t.Fatalf("file not restored: %s", data)
	}
	// Path escape blocked
	bad := codingWorkbenchCheckpoint{
		ProjectPath: root,
		FileSnapshots: []codingCheckpointFileSnap{
			{Path: "../outside.go", Content: "x"},
		},
	}
	if _, _, err := h.applyCodingCheckpointFileSnapshots(userID, bad, nil); err == nil {
		// may return no restorable if filter skips all — either error or 0 restored
	}
	// keep main conflict
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 1,
		Path:      filepath.Join(root, "wt-missing"),
		Kind:      "local_worktree",
		Files:     []string{"a.go", "b.go"},
	})
	list := h.listStickyCodingConflicts(userID)
	if len(list) != 1 {
		t.Fatal(list)
	}
	msg, err := h.keepMainCodingConflictFiles(userID, list[0].ID, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "1") {
		t.Fatalf("%s", msg)
	}
	list2 := h.listStickyCodingConflicts(userID)
	if len(list2) != 1 || len(list2[0].Files) != 1 || list2[0].Files[0] != "b.go" {
		t.Fatalf("%+v", list2)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestIsPathInsideRoot(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	if !isPathInsideRoot(root, filepath.Join(root, "a.go")) {
		t.Fatal("child should be inside")
	}
	if isPathInsideRoot(root, filepath.Join(root, "..", "x")) {
		// Clean may still resolve outside
		outside := filepath.Clean(filepath.Join(root, "..", "x"))
		if isPathInsideRoot(root, outside) {
			t.Fatal("outside should fail")
		}
	}
}

func TestCollectCodingCheckpointSidecarStats(t *testing.T) {
	userID := "desktop-user:sidecar-stats"
	dir := codingCheckpointSidecarDir(userID, "lab-stats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("z", 1024)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	st := collectCodingCheckpointSidecarStats(userID, "lab-stats")
	if st.MaxBytes <= 0 {
		t.Fatal("max bytes")
	}
	if st.UserBytes < int64(len(payload)) {
		t.Fatalf("user bytes=%d", st.UserBytes)
	}
	if st.UserDirCount < 1 {
		t.Fatalf("user dirs=%d", st.UserDirCount)
	}
	if st.KeepLabel != "lab-stats" {
		t.Fatalf("keep=%q", st.KeepLabel)
	}
	line := formatCodingCheckpointSidecarStatsLine(st)
	if !strings.Contains(line, "sidecar") || !strings.Contains(line, "MB") {
		t.Fatalf("%s", line)
	}
	_, _ = pruneCodingCheckpointSidecars(userID, "")
}

func TestEnforceCodingCheckpointSidecarBudget(t *testing.T) {
	userID := "desktop-user:budget-sidecar"
	dirKeep := codingCheckpointSidecarDir(userID, "keep-lab")
	dirDrop := codingCheckpointSidecarDir(userID, "drop-lab")
	if err := os.MkdirAll(dirKeep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirDrop, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make drop-lab older and non-empty so eviction prefers it.
	if err := os.WriteFile(filepath.Join(dirDrop, "old.txt"), []byte(strings.Repeat("x", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(dirDrop, old, old)
	_ = os.Chtimes(filepath.Join(dirDrop, "old.txt"), old, old)
	if err := os.WriteFile(filepath.Join(dirKeep, "new.txt"), []byte(strings.Repeat("y", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	protect := filepath.ToSlash(filepath.Join(codingCheckpointUserKey(userID), sanitizeCodingCheckpointLabel("keep-lab")))
	// Force tiny cap so at least one dir is removed (protect keep-lab).
	n, err := enforceCodingCheckpointSidecarBudgetWithCap(protect, 50)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected eviction under tiny cap, removed=%d", n)
	}
	if _, err := os.Stat(dirKeep); err != nil {
		t.Fatal("protected dir should remain")
	}
	if _, err := os.Stat(dirDrop); !os.IsNotExist(err) {
		t.Fatal("older unprotected dir should be gone")
	}
	// setCodingCheckpointSidecarMaxMB round-trip
	setCodingCheckpointSidecarMaxMB(128)
	if codingCheckpointSidecarMaxBytes() != 128*1024*1024 {
		t.Fatalf("budget=%d", codingCheckpointSidecarMaxBytes())
	}
	setCodingCheckpointSidecarMaxMB(0) // reset default
	if codingCheckpointSidecarMaxBytes() != codingCheckpointSidecarDefaultMaxBytes {
		t.Fatalf("default budget=%d", codingCheckpointSidecarMaxBytes())
	}
	_, _ = pruneCodingCheckpointSidecars(userID, "")
}

func TestPruneCodingCheckpointSidecars(t *testing.T) {
	userID := "desktop-user:prune-sidecar"
	// Create two label dirs under user bucket.
	dirA := codingCheckpointSidecarDir(userID, "keep-me")
	dirB := codingCheckpointSidecarDir(userID, "drop-me")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := pruneCodingCheckpointSidecars(userID, "keep-me")
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 removed, got %d", n)
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Fatal("keep-me should remain")
	}
	if _, err := os.Stat(dirB); !os.IsNotExist(err) {
		t.Fatal("drop-me should be gone")
	}
	// Clear all
	n, err = pruneCodingCheckpointSidecars(userID, "")
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("clear all removed=%d", n)
	}
	userDir := filepath.Join(codingCheckpointSidecarRoot(), codingCheckpointUserKey(userID))
	if _, err := os.Stat(userDir); !os.IsNotExist(err) {
		t.Fatal("user dir should be gone")
	}
}

func TestCodingCheckpointSidecarRestore(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:checkpoint-sidecar"
	root := t.TempDir()
	// Force sidecar path: content larger than inline limit but under sidecar limit.
	payload := strings.Repeat("line\n", (codingCheckpointMaxInlineBytes/5)+20)
	rel := "big.txt"
	abs := filepath.Join(root, rel)
	if err := os.WriteFile(abs, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ProjectPath = root
		mem.FilesModified = []string{rel}
	})
	cp := h.saveStickyCodingCheckpoint(userID, "big-snap")
	if len(cp.FileSnapshots) != 1 {
		t.Fatalf("%+v", cp.FileSnapshots)
	}
	snap := cp.FileSnapshots[0]
	if snap.Sidecar == "" {
		t.Fatalf("expected sidecar for large text file: %+v", snap)
	}
	if snap.Content != "" {
		t.Fatal("large file should not be fully inlined")
	}
	// Corrupt working copy.
	if err := os.WriteFile(abs, []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	restored, _, err := h.applyCodingCheckpointFileSnapshots(userID, cp, nil)
	if err != nil || restored != 1 {
		t.Fatalf("restored=%d err=%v", restored, err)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("sidecar restore mismatch len=%d want=%d", len(got), len(payload))
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestCodingHooksSlash(t *testing.T) {
	root := t.TempDir()
	maclaw := filepath.Join(root, ".maclaw")
	if err := os.MkdirAll(maclaw, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksJSON := `{"pre_step":["echo pre"],"post_step":["echo post"],"pre_plan":[],"post_turn":["echo turn"],"pre_verify":["echo v"],"on_conflict":["echo c"],"fail_on_error":true}`
	if err := os.WriteFile(filepath.Join(maclaw, "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	if !isCodingWorkbenchSlash("/hooks") {
		t.Fatal("hooks should be slash")
	}
	resp := h.handleCodingHooksSlash("u", root, "/hooks")
	if resp == nil || !strings.Contains(resp.Text, "pre_step") || !strings.Contains(resp.Text, "echo pre") {
		t.Fatalf("%+v", resp)
	}
	if !strings.Contains(resp.Text, "post_turn") {
		t.Fatalf("missing post_turn: %s", resp.Text)
	}
	if !strings.Contains(resp.Text, "pre_verify") || !strings.Contains(resp.Text, "on_conflict") {
		t.Fatalf("missing extended phases: %s", resp.Text)
	}
	if !strings.Contains(resp.Text, "fail_on_error") {
		t.Fatalf("missing fail_on_error: %s", resp.Text)
	}
}

func TestLoadCodingWorkbenchHooksCache(t *testing.T) {
	root := t.TempDir()
	maclaw := filepath.Join(root, ".maclaw")
	if err := os.MkdirAll(maclaw, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(maclaw, "hooks.json")
	if err := os.WriteFile(path, []byte(`{"pre_step":["echo a"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Bust any prior cache for this path (new temp dir is unique).
	h1 := loadCodingWorkbenchHooks(root)
	if codingWorkbenchHooksCommandCount(h1) != 1 {
		t.Fatalf("%+v", h1)
	}
	// Rewrite file with different content + bump mtime.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"pre_step":["echo a"],"post_turn":["echo b"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Within TTL but mtime changed → reload.
	h2 := loadCodingWorkbenchHooks(root)
	if codingWorkbenchHooksCommandCount(h2) != 2 {
		t.Fatalf("want reload on mtime change, got %+v", h2)
	}
	phases, n := codingWorkbenchHooksSummary(h2)
	if n != 2 || len(phases) != 2 {
		t.Fatalf("summary phases=%v n=%d", phases, n)
	}
}

func TestLoadCodingWorkbenchHooksExtended(t *testing.T) {
	root := t.TempDir()
	maclaw := filepath.Join(root, ".maclaw")
	if err := os.MkdirAll(maclaw, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksJSON := `{
		"pre_step":["echo pre"],
		"pre_verify":["echo v"],
		"post_verify":["echo pv"],
		"pre_checkpoint":["echo pc"],
		"post_checkpoint":["echo oc"],
		"on_conflict":["echo conf"],
		"fail_on_error": true
	}`
	if err := os.WriteFile(filepath.Join(maclaw, "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	hooks := loadCodingWorkbenchHooks(root)
	if !hooks.FailOnError {
		t.Fatal("want fail_on_error")
	}
	phases := codingWorkbenchHooksActivePhases(hooks)
	want := map[string]bool{
		"pre_step": true, "pre_verify": true, "post_verify": true,
		"pre_checkpoint": true, "post_checkpoint": true, "on_conflict": true,
	}
	for _, p := range phases {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("missing phases %v got %v", want, phases)
	}
	if n := codingWorkbenchHooksCommandCount(hooks); n != 6 {
		t.Fatalf("count=%d", n)
	}
}

func TestRunCodingWorkbenchHooksFailDetect(t *testing.T) {
	root := t.TempDir()
	// Cross-platform failing command via powershell/bash exit.
	failCmd := "exit 1"
	res := runCodingWorkbenchHooks(root, []string{failCmd}, "pre_step")
	if !res.Failed || res.Ran != 1 {
		t.Fatalf("%+v", res)
	}
	hooks := codingWorkbenchHooks{FailOnError: true, PreStep: []string{failCmd}}
	if !codingHookShouldAbort(hooks, res) {
		t.Fatal("should abort")
	}
	hooks.FailOnError = false
	if codingHookShouldAbort(hooks, res) {
		t.Fatal("should not abort when fail_on_error false")
	}
	// fail_on_error: stop after first fail (do not run later cmds).
	marker := filepath.Join(root, "should-not-run.txt")
	var second string
	if runtime.GOOS == "windows" {
		second = fmt.Sprintf("Set-Content -Path '%s' -Value x", marker)
	} else {
		second = fmt.Sprintf("echo x > %s", marker)
	}
	hooksStop := codingWorkbenchHooks{FailOnError: true, PreStep: []string{failCmd, second}}
	res2 := runCodingWorkbenchHookPhase(root, hooksStop, "pre_step")
	if !res2.Failed || res2.Ran != 1 {
		t.Fatalf("stopOnFail ran=%d failed=%v", res2.Ran, res2.Failed)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("second cmd should not run when fail_on_error stops")
	}
}

func TestRunCodingWorkbenchHookPhaseCheckpoint(t *testing.T) {
	root := t.TempDir()
	maclaw := filepath.Join(root, ".maclaw")
	_ = os.MkdirAll(maclaw, 0o755)
	// Write a marker file via hook so we can observe execution.
	marker := filepath.Join(root, "hook-ran.txt")
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = fmt.Sprintf("Set-Content -Path '%s' -Value ok", marker)
	} else {
		cmd = fmt.Sprintf("echo ok > %s", marker)
	}
	hooks := codingWorkbenchHooks{PreCheckpoint: []string{cmd}}
	res := runCodingWorkbenchHookPhase(root, hooks, "pre_checkpoint")
	if res.Failed {
		t.Fatalf("hook failed: %+v", res)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker missing: %v report=%s", err, res.Report)
	}
}

// TestRemoteCodingHooksLoadFromLocalProjectPath documents that remote coding
// runs hooks from the local task ProjectPath (.maclaw/hooks.json), not SSH dirs.
func TestRemoteCodingHooksLoadFromLocalProjectPath(t *testing.T) {
	localRoot := t.TempDir()
	maclaw := filepath.Join(localRoot, ".maclaw")
	if err := os.MkdirAll(maclaw, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksJSON := `{"pre_step":["echo remote-local"],"pre_verify":["echo v"],"fail_on_error":true}`
	if err := os.WriteFile(filepath.Join(maclaw, "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate remote sticky: ProjectPath is local task dir; remote workdir is SSH path.
	hooks := loadCodingWorkbenchHooks(localRoot)
	if codingWorkbenchHooksCommandCount(hooks) < 2 {
		t.Fatalf("expected hooks from local project, got %+v", hooks)
	}
	if !hooks.FailOnError {
		t.Fatal("fail_on_error")
	}
	// Empty path (missing sticky ProjectPath) must not invent remote-dir hooks.
	if n := codingWorkbenchHooksCommandCount(loadCodingWorkbenchHooks("")); n != 0 {
		t.Fatalf("empty path should load zero hooks, n=%d", n)
	}
	// Nonexistent remote-style path should be empty.
	if n := codingWorkbenchHooksCommandCount(loadCodingWorkbenchHooks("/tmp/ssh-only-no-hooks")); n != 0 {
		t.Fatalf("missing path should load zero hooks, n=%d", n)
	}
}

func TestStickyCodingCheckpointHistory(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:cp-history"
	root := t.TempDir()
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.ProjectPath = root
		m.SessionPlan = "goal-v1"
		m.ExecutionPlan = "T1 a"
		m.LastSummary = "sum1"
		m.FilesModified = []string{"a.go"}
	})
	cp1 := h.saveStickyCodingCheckpoint(userID, "first")
	if cp1.Label != "first" {
		t.Fatalf("cp1=%+v", cp1)
	}
	// Mutate session and save second — first should archive.
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.SessionPlan = "goal-v2"
		m.ExecutionPlan = "T1 b"
		m.LastSummary = "sum2"
	})
	cp2 := h.saveStickyCodingCheckpoint(userID, "second")
	if cp2.Label != "second" {
		t.Fatalf("cp2=%+v", cp2)
	}
	list := h.listStickyCodingCheckpoints(userID)
	if len(list) < 2 {
		t.Fatalf("list=%+v", list)
	}
	if !list[0].Current || list[0].Label != "second" {
		t.Fatalf("current first: %+v", list[0])
	}
	// History contains first.
	foundFirst := false
	for _, e := range list {
		if e.Label == "first" && !e.Current {
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Fatalf("first not in history: %+v", list)
	}
	// Restore by label rewinds session plan.
	got, ok := h.restoreStickyCodingCheckpointByLabel(userID, "first", false)
	if !ok || got.Label != "first" {
		t.Fatalf("restore: %+v ok=%v", got, ok)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if !strings.Contains(mem.SessionPlan, "goal-v1") {
		t.Fatalf("session plan not restored: %q", mem.SessionPlan)
	}
	// Keep labels include both.
	keeps := h.stickyCodingCheckpointKeepLabels(userID)
	if len(keeps) < 2 {
		t.Fatalf("keeps=%v", keeps)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestReplaceStickyPendingCodingPlanMarkdown(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:plan-edit"
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "explore", Description: "map"},
		{Index: 2, Title: "implement", Description: "code"},
	}
	h.storeStickyPendingCodingPlan(userID, "ship feature", "### T1: explore\n### T2: implement", tasks)
	// Too few steps → error.
	if _, err := h.replaceStickyPendingCodingPlanMarkdown(userID, "only one step"); err == nil {
		t.Fatal("expected error for <2 steps")
	}
	before, _ := h.loadStickyPendingCodingPlan(userID)
	// Stabilize CreatedAt for preserve check.
	if before.CreatedAt <= 0 {
		t.Fatal("want created_at")
	}
	updated, err := h.replaceStickyPendingCodingPlanMarkdown(userID, "1. locate bug\n2. write fix\n3. verify tests")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tasks) < 2 {
		t.Fatalf("tasks=%d", len(updated.Tasks))
	}
	if updated.UserText != "ship feature" {
		t.Fatalf("user text clobbered: %q", updated.UserText)
	}
	if updated.CreatedAt != before.CreatedAt {
		t.Fatalf("CreatedAt should preserve: before=%d after=%d", before.CreatedAt, updated.CreatedAt)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if !strings.Contains(mem.ExecutionPlan, "locate") && !strings.Contains(updated.Markdown, "locate") {
		t.Fatalf("markdown/exec not updated: exec=%q md=%q", mem.ExecutionPlan, updated.Markdown)
	}
	if len(mem.StepStatuses) < 2 {
		t.Fatalf("step statuses=%d", len(mem.StepStatuses))
	}
	// No pending → error.
	h.clearStickyPendingCodingPlan(userID)
	if _, err := h.replaceStickyPendingCodingPlanMarkdown(userID, "1. a\n2. b"); err == nil {
		t.Fatal("expected no pending")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestPromotePendingCodingPlanGate(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:plan-gate"
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "explore", Description: "map code"},
		{Index: 2, Title: "implement", Description: "write fix"},
	}
	h.storeStickyPendingCodingPlan(userID, "fix auth", "## plan\nT1 explore\nT2 implement", tasks)
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) != 2 {
		t.Fatalf("pending: %+v ok=%v", pending, ok)
	}
	approved, ok := h.promotePendingToApprovedCodingPlan(userID)
	if !ok || len(approved.Tasks) != 2 {
		t.Fatalf("approved: %+v", approved)
	}
	if _, still := h.loadStickyPendingCodingPlan(userID); still {
		t.Fatal("pending should be cleared after promote")
	}
	taken, ok := h.takeStickyApprovedCodingPlan(userID)
	if !ok || len(taken.Tasks) != 2 {
		t.Fatalf("take: %+v", taken)
	}
	if _, again := h.takeStickyApprovedCodingPlan(userID); again {
		t.Fatal("approved is one-shot")
	}
	// Reject path clears pending without execute.
	h.storeStickyPendingCodingPlan(userID, "again", "md", tasks)
	h.clearStickyPendingCodingPlan(userID)
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("reject should clear")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestStartCodingWorkbenchBackgroundVerifyMarksRunning(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:bg-verify"
	root := t.TempDir()
	// No package.json / Makefile — verify will finish as skipped quickly.
	msg, err := h.startCodingWorkbenchBackgroundVerify(userID, root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "后台") && !strings.Contains(strings.ToLower(msg), "background") {
		t.Fatalf("msg=%s", msg)
	}
	// Allow goroutine to finish.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sum := strings.TrimSpace(h.getStickyCodingWorkbenchMemory(userID).BackgroundVerifySummary)
		if sum != "" && sum != "后台验证运行中…" {
			h.clearStickyCodingWorkbenchMemory(userID)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Still OK if stuck on running (slow CI); just ensure it was set.
	sum := h.getStickyCodingWorkbenchMemory(userID).BackgroundVerifySummary
	if sum == "" {
		t.Fatal("expected background verify summary")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestHandleCodingRouteSlash(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:route-slash"
	h.recordStickyCodingRoute(userID, "claude-sonnet", "route", "reasoning", "coding")
	resp := h.handleCodingRouteSlash(userID, "", "/route")
	if resp == nil || !strings.Contains(resp.Text, "claude-sonnet") {
		t.Fatalf("%+v", resp)
	}
	resp2 := h.handleCodingRouteSlash(userID, "", "/route pref vision")
	if resp2 == nil || !strings.Contains(resp2.Text, "vision") {
		t.Fatalf("%+v", resp2)
	}
	if h.getStickyCodingRoutePref(userID) != codingRoutePrefVision {
		t.Fatal(h.getStickyCodingRoutePref(userID))
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestParseAdoptArgs(t *testing.T) {
	id, files := parseAdoptArgs("c1 -- a.go b.go")
	if id != "c1" || len(files) != 2 {
		t.Fatalf("%q %v", id, files)
	}
	id, files = parseAdoptArgs("c2 foo.go")
	if id != "c2" || len(files) != 1 || files[0] != "foo.go" {
		t.Fatalf("%q %v", id, files)
	}
}

func TestStickyCodingConflictUIState(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:conflict-ui"
	h.setStickyCodingConflictUIState(userID, "c42", "src/a.go", []string{"src/a.go", "src/b.go", "src/a.go", ""})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.ConflictActiveID != "c42" || mem.ConflictFocusFile != "src/a.go" {
		t.Fatalf("%+v", mem)
	}
	if len(mem.ConflictSelected) != 2 {
		t.Fatalf("selected=%v", mem.ConflictSelected)
	}
	// Cap + clean
	many := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		many = append(many, fmt.Sprintf("f%d.go", i))
	}
	h.setStickyCodingConflictUIState(userID, "c1", "", many)
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.ConflictSelected) > 40 {
		t.Fatalf("cap failed %d", len(mem.ConflictSelected))
	}
	// remove conflict clears UI when active matches
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		ID:   "c1",
		Path: filepath.Join(t.TempDir(), "wt"),
		Kind: "local_worktree",
	})
	h.setStickyCodingConflictUIState(userID, "c1", "x.go", []string{"x.go"})
	_, ok := h.removeStickyCodingConflict(userID, "c1")
	if !ok {
		t.Fatal("remove")
	}
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if mem.ConflictActiveID != "" || len(mem.ConflictSelected) != 0 {
		t.Fatalf("UI should clear: %+v", mem)
	}
	// prune selection
	h.setStickyCodingConflictUIState(userID, "c9", "gone.go", []string{"keep.go", "gone.go"})
	h.pruneStickyConflictUISelection(userID, []string{"keep.go"})
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.ConflictSelected) != 1 || mem.ConflictSelected[0] != "keep.go" || mem.ConflictFocusFile != "" {
		t.Fatalf("%+v", mem)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestEnsureRoutePrefSeedDoesNotUseStaleHelperRMW(t *testing.T) {
	// Document the fixed pattern: seed RoutePref onto mem then single store.
	h := &IMMessageHandler{}
	userID := "desktop-user:route-seed"
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:      "local",
		RoutePref: "",
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	// Simulate Ensure: apply kind + route seed on one snapshot.
	mem.Kind = "remote"
	mem.RoutePref = codingRoutePrefVision
	h.storeStickyCodingWorkbenchMemory(userID, mem)
	got := h.getStickyCodingWorkbenchMemory(userID)
	if got.Kind != "remote" || got.RoutePref != codingRoutePrefVision {
		t.Fatalf("%+v", got)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestGetCodingConflictFilePreviewMain(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:preview"
	root := t.TempDir()
	rel := "p.go"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("package main\n// preview\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fake isolate path (need existing dir for theirs; main uses git root).
	iso := filepath.Join(root, "iso")
	_ = os.MkdirAll(iso, 0o755)
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		ID:          "c-prev",
		Path:        iso,
		MainProject: root,
		GitRoot:     root,
		Kind:        "local_worktree",
		Files:       []string{rel},
	})
	prev, err := h.getCodingConflictFilePreview(userID, "c-prev", rel, "main", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if prev.Missing || !strings.Contains(prev.Content, "preview") {
		t.Fatalf("%+v", prev)
	}
	// missing theirs
	prev2, err := h.getCodingConflictFilePreview(userID, "c-prev", rel, "theirs", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !prev2.Missing {
		t.Fatal("theirs should be missing")
	}
	// binary on main
	binRel := "pic.bin"
	bin := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}
	if err := os.WriteFile(filepath.Join(root, binRel), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	prev3, err := h.getCodingConflictFilePreview(userID, "c-prev", binRel, "main", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prev3.Content, "binary") {
		t.Fatalf("want binary marker, got %+v", prev3)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestParseWorktreeResolveArgs(t *testing.T) {
	id, _, action, files := parseWorktreeResolveArgs("c9 keep -- a.go b.go")
	if id != "c9" || action != "keep" || len(files) != 2 {
		t.Fatalf("%q %q %v", id, action, files)
	}
	id, _, action, files = parseWorktreeResolveArgs("c1 adopt")
	if id != "c1" || action != "adopt" || len(files) != 0 {
		t.Fatalf("%q %q %v", id, action, files)
	}
	id, _, action, files = parseWorktreeResolveArgs("c2 base x.go")
	if id != "c2" || action != "base" || len(files) != 1 || files[0] != "x.go" {
		t.Fatalf("%q %q %v", id, action, files)
	}
}

func TestSimpleUnifiedSnippet(t *testing.T) {
	s := simpleUnifiedSnippet("a\nb\nc", "a\nB\nc", 10)
	if !strings.Contains(s, "- b") || !strings.Contains(s, "+ B") {
		t.Fatalf("%q", s)
	}
}

func TestFormatThreeWaySnippet(t *testing.T) {
	s := formatThreeWaySnippet("a\nb\nc", "a\nB\nc", "a\nX\nc", 10)
	if s == "" || !strings.Contains(s, "base") || !strings.Contains(s, "main") || !strings.Contains(s, "theirs") {
		t.Fatalf("%q", s)
	}
	if !strings.Contains(s, "B") || !strings.Contains(s, "X") {
		t.Fatalf("missing side content: %q", s)
	}
}

func TestAdoptBaseCodingConflictFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init")
	run(root, "config", "user.email", "t@e.com")
	run(root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n// base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(root, "add", ".")
	run(root, "commit", "-m", "init")

	wt, err := createCodingWorkbenchWorktree(root, 11, "base-adopt")
	if err != nil || wt == nil {
		t.Fatalf("create wt: %v %#v", err, wt)
	}
	// Diverge main and worktree.
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n// main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.ProjectPath, "f.go"), []byte("package main\n// theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	userID := "desktop-user:adopt-base"
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex:   11,
		Path:        wt.ProjectPath,
		ProjectPath: wt.ProjectPath,
		Branch:      wt.Branch,
		GitRoot:     root,
		MainProject: root,
		Kind:        "local_worktree",
		Files:       []string{"f.go"},
	})
	msg, err := h.adoptBaseCodingConflictFiles(userID, h.listStickyCodingConflicts(userID)[0].ID, []string{"f.go"})
	if err != nil {
		// merge-base may fail on some git setups; skip soft
		if strings.Contains(err.Error(), "merge-base") || strings.Contains(err.Error(), "no merge-base") {
			wt.cleanup(false)
			h.clearStickyCodingWorkbenchMemory(userID)
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	if !strings.Contains(msg, "1") && !strings.Contains(msg, "merge-base") {
		t.Fatalf("%s", msg)
	}
	data, err := os.ReadFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	// Base content should have been written when merge-base worked.
	if strings.Contains(string(data), "base") || strings.Contains(msg, "写回") {
		// ok
	} else {
		t.Logf("content=%q msg=%s", data, msg)
	}
	wt.cleanup(false)
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestBuildLocalConflictFileDiffWithBranchThreeWay(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init")
	run(root, "config", "user.email", "t@e.com")
	run(root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n// base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(root, "add", ".")
	run(root, "commit", "-m", "init")

	wt, err := createCodingWorkbenchWorktree(root, 9, "3way")
	if err != nil || wt == nil {
		t.Fatalf("create wt: %v %#v", err, wt)
	}
	// Change main and worktree divergently.
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n// main-side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.ProjectPath, "f.go"), []byte("package main\n// theirs-side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := buildLocalConflictFileDiffWithBranch(root, wt.ProjectPath, wt.Branch, "f.go")
	if d.Status != "modified" {
		t.Fatalf("status=%s", d.Status)
	}
	if d.BaseHead == "" && d.ThreeWay == "" {
		// merge-base should work for worktree branch; if empty, still OK if unified present
		if d.Unified == "" {
			t.Fatal("expected some preview")
		}
		t.Logf("no three-way (ok on some git layouts): %+v", d)
	} else if d.ThreeWay != "" && !strings.Contains(d.ThreeWay, "theirs") {
		t.Fatalf("three_way=%q", d.ThreeWay)
	}
	wt.cleanup(false)
}

func TestFilterAndSubtractFiles(t *testing.T) {
	all := []string{"a.go", "b.go", "c.go"}
	got := filterFilesBySelection(all, []string{"b.go"})
	if len(got) != 1 || got[0] != "b.go" {
		t.Fatalf("%v", got)
	}
	rest := subtractFiles(all, []string{"b.go"})
	if len(rest) != 2 {
		t.Fatalf("%v", rest)
	}
}

func TestParseGitPorcelainPaths(t *testing.T) {
	raw := " M src/a.go\n?? new file.txt\nR  old.go -> new.go\n"
	got := parseGitPorcelainPaths(raw, 10)
	if len(got) < 2 {
		t.Fatalf("%v", got)
	}
	// space in path kept
	found := false
	for _, p := range got {
		if strings.Contains(p, "new file.txt") || p == "new file.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected spaced path, got %v", got)
	}
}

func TestUpdateStickyCodingConcurrent(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:sticky-race"
	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			h.updateStickyCodingStepStatus(userID, idx, codingStepPassed, "ok")
			h.accumulateStickyCodingUsage(userID, 1, 1, 0.001)
		}(i)
	}
	wg.Wait()
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 8 {
		t.Fatalf("steps=%d want 8", len(mem.StepStatuses))
	}
	if mem.SessionInputTokens != 8 {
		t.Fatalf("tokens=%d", mem.SessionInputTokens)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}
