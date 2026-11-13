package guiapp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestExecuteRemoteControlRecoveredWithoutApp(t *testing.T) {
	_, err := (&RemoteHubClient{}).executeRemoteControlRecovered("list_tasks", nil, "grant-1")
	if err == nil || !strings.Contains(err.Error(), "尚未就绪") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteRemoteControlRejectsUnknownOp(t *testing.T) {
	_, err := (&App{}).executeRemoteControl("drop_database", nil)
	if err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteRemoteControlCreateTaskRequiresName(t *testing.T) {
	_, err := (&App{}).executeRemoteControl("create_task", json.RawMessage(`{"name":"  "}`))
	if err == nil || !strings.Contains(err.Error(), "任务名称") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteRemoteControlCreateTaskRejectsSSHMode(t *testing.T) {
	// SSH targets cannot be configured through remote control; the whitelist
	// rejects before CreateTaskWithMode runs.
	_, err := (&App{}).executeRemoteControl("create_task", json.RawMessage(`{"name":"x","mode":"ssh_host"}`))
	if err == nil || !strings.Contains(err.Error(), "任务模式") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteControlStopTreatsIdleSessionAsSuccess(t *testing.T) {
	if !remoteControlStopIsIdle(errors.New("no active session to cancel")) {
		t.Fatal("idle stop should be success")
	}
	if remoteControlStopIsIdle(errors.New("AI assistant not initialized")) {
		t.Fatal("a real stop failure must stay a failure")
	}
}

func TestRemoteControlKnownTaskRejectsEmptyPath(t *testing.T) {
	if _, err := (&App{}).remoteControlKnownTask("  "); err == nil {
		t.Fatal("expected missing task")
	}
}

func TestRemoteControlMessageRejectsDesktopCommands(t *testing.T) {
	if err := remoteControlMessageError("继续看一下"); err != nil {
		t.Fatal(err)
	}
	if err := remoteControlMessageError("/exec whoami"); err == nil {
		t.Fatal("slash command should be rejected")
	}
	if err := remoteControlMessageError("__agent_view_submit__ {}"); err == nil {
		t.Fatal("task panel action should be rejected")
	}
}

func TestTrimRemoteControlTextKeepsShortMessages(t *testing.T) {
	if trimRemoteControlText("你好", 4) != "你好" {
		t.Fatal("short text changed")
	}
	if got := trimRemoteControlText("你好世界", 2); got != "你好…" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteControlTurnDisplayTextMarksOrigin(t *testing.T) {
	// The controlled desktop shows the controller's message as a user row;
	// the bracket marks it did not come from this keyboard (like the ACP
	// Mode B prefix).
	got := remoteControlTurnDisplayText("生成pdf")
	if !strings.HasPrefix(got, "〔") || !strings.Contains(got, "生成pdf") {
		t.Fatalf("got %q", got)
	}
}

func TestEmitAIAssistantForegroundRoundStartedNoWailsContext(t *testing.T) {
	// Without a Wails lifecycle context the emit is a no-op, not a panic.
	(&App{}).emitAIAssistantForegroundRoundStarted("req-1", "desktop-user", "hi", "〔远控〕hi")
}

func TestStampProducedFilesOnLastAssistantEntry(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "生成一个pdf"},
		{Role: "assistant", Content: "已经生成好了"},
	}
	out, changed := stampProducedFilesOnLastAssistantEntry(history, &IMAgentResponse{
		LocalFilePath:  `C:\out\report.pdf`,
		LocalFilePaths: []string{`C:\out\report.pdf`, `C:\out\chart.png`},
	})
	if !changed || len(out[1].LocalFilePaths) != 2 {
		t.Fatalf("stamped paths = %v changed=%v", out[1].LocalFilePaths, changed)
	}
	// The caller's slice is never mutated.
	if len(history[1].LocalFilePaths) != 0 {
		t.Fatal("input history was mutated")
	}
}

func TestStampProducedFilesPrefersNonEchoAssistantEntry(t *testing.T) {
	// A trailing planning echo would be dropped by visible history once the
	// turn has a real reply, so the files must land on the reply instead.
	echo := agent.ConversationEntry{
		Role:             "assistant",
		Content:          "正在生成文件",
		ReasoningContent: "正在生成文件",
		FinishReason:     "tool_calls",
		ToolCalls:        []interface{}{map[string]interface{}{"id": "call-1"}},
	}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "生成一个pdf"},
		{Role: "assistant", Content: "已经生成好了"},
		echo,
	}
	out, changed := stampProducedFilesOnLastAssistantEntry(history, &IMAgentResponse{
		LocalFilePaths: []string{`C:\out\report.pdf`},
	})
	if !changed || len(out[1].LocalFilePaths) != 1 {
		t.Fatalf("files must land on the real reply: %+v", out)
	}
	if len(out[2].LocalFilePaths) != 0 {
		t.Fatal("the planning echo must not take the stamp")
	}
}

func TestStampProducedFilesSkipsSteerAfterReply(t *testing.T) {
	// A user row already queued behind the answer means the turn the file
	// belongs to is not the last assistant entry the history shows next.
	history := []agent.ConversationEntry{
		{Role: "user", Content: "生成一个pdf"},
		{Role: "assistant", Content: "已经生成好了"},
		{Role: "user", Content: "再改一下"},
	}
	out, changed := stampProducedFilesOnLastAssistantEntry(history, &IMAgentResponse{
		LocalFilePaths: []string{`C:\out\report.pdf`},
	})
	if changed || len(out[1].LocalFilePaths) != 0 {
		t.Fatal("paths must not land on an older reply")
	}
}

func TestStampProducedFilesNoopWithoutFiles(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "assistant", Content: "回答"},
	}
	out, changed := stampProducedFilesOnLastAssistantEntry(history, &IMAgentResponse{Text: "回答"})
	if changed || len(out[0].LocalFilePaths) != 0 {
		t.Fatal("no files should stamp nothing")
	}
}

func TestRemoteControlNormalizeFilePathMatchesCaseAndSeparators(t *testing.T) {
	want := remoteControlNormalizeFilePath(`C:\Work\Out\Report.PDF`)
	for _, other := range []string{`c:\work\out\report.pdf`, `C:/Work/Out/Report.PDF`, ` c:\work\out\report.pdf `} {
		if remoteControlNormalizeFilePath(other) != want {
			t.Fatalf("%q did not normalize to %q", other, want)
		}
	}
	if remoteControlNormalizeFilePath("  ") != "" {
		t.Fatal("blank path should normalize empty")
	}
}

func TestProjectHistoryFromTabSessionKeepsLocalFilePaths(t *testing.T) {
	items := projectConversationHistoryFromTabSession([]interface{}{
		map[string]interface{}{"role": "user", "content": "生成一个pdf"},
		map[string]interface{}{
			"role":           "assistant",
			"content":        "已经生成好了",
			"localFilePaths": []interface{}{`C:\out\report.pdf`, `C:\out\chart.png`},
		},
	})
	if len(items) != 2 || len(items[1].LocalFilePaths) != 2 {
		t.Fatalf("items = %#v", items)
	}
}

func TestStampProducedFilesViaResponseEmission(t *testing.T) {
	app := newProjectSearchTestApp(t)
	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	app.imHandler = h
	owner := `desktop-user:C:\work\task`
	h.memory.Save(owner, []agent.ConversationEntry{
		{Role: "user", Content: "生成pdf"},
		{Role: "assistant", Content: "已生成"},
	})
	app.stampProducedFilesIntoConversationMemory(&IMAgentResponse{
		SessionKey:     owner,
		LocalFilePaths: []string{`C:\out\report.pdf`},
	})
	entries := h.memory.Load(owner)
	if len(entries) != 2 || len(entries[1].LocalFilePaths) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	// A response without files or without a session key is a no-op.
	app.stampProducedFilesIntoConversationMemory(&IMAgentResponse{SessionKey: owner})
	app.stampProducedFilesIntoConversationMemory(&IMAgentResponse{LocalFilePaths: []string{`C:\out\x.pdf`}})
	if got := h.memory.Load(owner); len(got[1].LocalFilePaths) != 1 {
		t.Fatalf("entries after noop stamps = %#v", got)
	}
}

func TestRemoteControlTaskFileServesOnlyRecordedAttachments(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	app.remoteSessions = NewRemoteSessionManager(app)
	app.interactionInfraDone.Store(true)
	mem := agent.NewConversationMemory()
	t.Cleanup(mem.Stop)
	app.aiConversationMemory = mem

	task := app.createTaskRecordWithWorkingDir("远程任务", "", []string{taskManagementTag, taskUserCreatedTag}, "", true)
	if task.ProjectPath == "" {
		t.Fatal("task record was not created")
	}
	dir := t.TempDir()
	produced := filepath.Join(dir, "report.pdf")
	payload := []byte("%PDF-1.4 fake")
	if err := os.WriteFile(produced, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	stranger := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(stranger, []byte("do not read"), 0o644); err != nil {
		t.Fatal(err)
	}
	mem.Save(projectSessionOwnerID(task.ProjectPath), []agent.ConversationEntry{
		{Role: "user", Content: "生成pdf"},
		{Role: "assistant", Content: "已生成", LocalFilePaths: []string{produced}},
	})

	// A file this task never produced must not be readable through remote control.
	_, err := app.executeRemoteControl("fetch_file", json.RawMessage(fmt.Sprintf(
		`{"project_path":%q,"path":%q}`, task.ProjectPath, stranger)))
	if err == nil || !strings.Contains(err.Error(), "没有找到这个文件") {
		t.Fatalf("unrecorded path err = %v", err)
	}

	raw, err := app.executeRemoteControl("fetch_file", json.RawMessage(fmt.Sprintf(
		`{"project_path":%q,"path":%q}`, task.ProjectPath, produced)))
	if err != nil {
		t.Fatalf("fetch_file err = %v", err)
	}
	result, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", raw)
	}
	if result["name"] != "report.pdf" {
		t.Fatalf("name = %v", result["name"])
	}
	got, err := base64.StdEncoding.DecodeString(fmt.Sprint(result["data_base64"]))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch got=%v err=%v", got, err)
	}

	// A recorded path swapped for a symlink must not follow through to an
	// unrelated target.
	if err := os.Remove(produced); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stranger, produced); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = app.executeRemoteControl("fetch_file", json.RawMessage(fmt.Sprintf(
		`{"project_path":%q,"path":%q}`, task.ProjectPath, produced)))
	if err == nil {
		t.Fatal("symlinked attachment path must be rejected")
	}
}

func TestSaveRemoteControlFileWritesIntoDownloads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	downloads := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: home}
	payload := []byte("remote attachment")
	encoded := base64.StdEncoding.EncodeToString(payload)

	path, err := app.SaveRemoteControlFile("report.pdf", encoded)
	if err != nil {
		t.Fatal(err)
	}
	remoteDir := filepath.Join(downloads, "MaClaw-RemoteFiles")
	if filepath.Dir(path) != remoteDir {
		t.Fatalf("saved into %q, want %q", path, remoteDir)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("saved bytes mismatch: %v", err)
	}

	// The same fetch again reuses the download instead of stacking a copy.
	again, err := app.SaveRemoteControlFile("report.pdf", encoded)
	if err != nil || again != path {
		t.Fatalf("refetch path = %q err = %v, want %q", again, err, path)
	}

	// A different file with the same name lands as a suffixed copy.
	other, err := app.SaveRemoteControlFile("report.pdf", base64.StdEncoding.EncodeToString([]byte("different")))
	if err != nil {
		t.Fatal(err)
	}
	if other == path || filepath.Dir(other) != remoteDir {
		t.Fatalf("duplicate name resolved to %q", other)
	}

	// A same-sized but different file must NOT be mistaken for the download.
	clash, err := app.SaveRemoteControlFile("report.pdf", base64.StdEncoding.EncodeToString([]byte("remote attachemen")))
	if err != nil {
		t.Fatal(err)
	}
	if clash == path {
		t.Fatal("same-size different-content file must not reuse the saved copy")
	}
}

func TestSaveRemoteControlFileRejectsBadInput(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.SaveRemoteControlFile("x.txt", "  "); err == nil {
		t.Fatal("empty payload must fail")
	}
	if _, err := app.SaveRemoteControlFile("x.txt", "not-base64!!!"); err == nil {
		t.Fatal("invalid base64 must fail")
	}
}

func TestRemoteControlSaveFileName(t *testing.T) {
	for name, want := range map[string]string{
		`report.pdf`:           "report.pdf",
		`C:\work\report.pdf`:   "report.pdf",
		`../../etc/passwd`:     "passwd",
		`a<b>c:d"e|f?g*h.docx`: "a_b_c_d_e_f_g_h.docx",
		`nul.txt`:              "_nul.txt",
		`COM1`:                 "_COM1",
		`  ..  `:               "remote-file",
		``:                     "remote-file",
	} {
		if got := remoteControlSaveFileName(name); got != want {
			t.Fatalf("remoteControlSaveFileName(%q) = %q, want %q", name, got, want)
		}
	}
}
