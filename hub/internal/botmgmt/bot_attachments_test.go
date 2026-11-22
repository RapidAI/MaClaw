package botmgmt

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// attachmentDesktop records what it was asked to write and run.
type attachmentDesktop struct {
	materialDesktop
	wrotePaths    []string
	wroteContents []string
	commands      []string
	writeErr      error
	commandErr    error
}

func (d *attachmentDesktop) WriteFile(_ context.Context, _, _, path, content string) (string, error) {
	if d.writeErr != nil {
		return "", d.writeErr
	}
	d.wrotePaths = append(d.wrotePaths, path)
	d.wroteContents = append(d.wroteContents, content)
	return path, nil
}

func (d *attachmentDesktop) RunCommand(_ context.Context, _, _, command string) (string, error) {
	if d.commandErr != nil {
		return "", d.commandErr
	}
	d.commands = append(d.commands, command)
	return "", nil
}

func attachmentService(t *testing.T) (*Service, *attachmentDesktop) {
	t.Helper()
	d := &attachmentDesktop{}
	return &Service{Desktop: d, MaterialStageDir: t.TempDir()}, d
}

func testRef(taskDir string) AttachmentRef {
	return AttachmentRef{
		GrantID:  "rc_grant-1",
		UploadID: "rc12345abc",
		Name:     "报告 v1.pdf",
		Path:     taskDir,
		BotID:    "bot_1",
	}
}

func TestStageAttachmentChunkPlacesTheFileInTheTaskFolder(t *testing.T) {
	svc, d := attachmentService(t)
	taskDir := "/home/desktop/.maclaw/data/tasks/清理-1699999"
	data := []byte("hello attachment")

	staged, err := svc.StageAttachmentChunk(context.Background(), "t1", AttachmentChunk{
		Ref:    testRef(taskDir),
		Offset: 0,
		Total:  int64(len(data)),
		Data:   data,
	})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	want := taskDir + "/attachments/rc12345abc-报告 v1.pdf"
	if !staged.Done || staged.Path != want {
		t.Fatalf("staged = %#v, want path %q", staged, want)
	}
	// First the bytes land in the bot container's staging folder as base64,
	// then a container command moves them next to the task.
	if len(d.wrotePaths) != 1 || !strings.HasPrefix(d.wrotePaths[0], "/home/desktop/.maclaw-attach/") {
		t.Fatalf("staging write = %#v", d.wrotePaths)
	}
	if d.wroteContents[0] == string(data) {
		t.Fatal("the raw bytes were sent; the file API carries base64")
	}
	if len(d.commands) != 1 || !strings.Contains(d.commands[0], want) || !strings.Contains(d.commands[0], "base64 -d") {
		t.Fatalf("commands = %#v", d.commands)
	}
}

func TestStageAttachmentChunkRefusesUnsafeInput(t *testing.T) {
	dir := "/home/desktop/.maclaw/data/tasks/t"
	noBot := testRef(dir)
	noBot.BotID = ""
	cases := map[string]AttachmentChunk{
		"no grant":   {Ref: AttachmentRef{UploadID: "rc12345abc", Name: "a.txt", Path: dir, BotID: "b"}, Total: 1, Data: []byte("a")},
		"no task":    {Ref: AttachmentRef{GrantID: "rc_g", UploadID: "rc12345abc", Name: "a.txt", BotID: "b"}, Total: 1, Data: []byte("a")},
		"bad upload": {Ref: AttachmentRef{GrantID: "rc_g", UploadID: "-abc", Name: "a.txt", Path: dir, BotID: "b"}, Total: 1, Data: []byte("a")},
		"bad name":   {Ref: AttachmentRef{GrantID: "rc_g", UploadID: "rc12345abc", Name: "..", Path: dir, BotID: "b"}, Total: 1, Data: []byte("a")},
		"no bytes":   {Ref: AttachmentRef{GrantID: "rc_g", UploadID: "rc12345abc", Name: "a.txt", Path: dir, BotID: "b"}, Total: 0},
		"past end":   {Ref: AttachmentRef{GrantID: "rc_g", UploadID: "rc12345abc", Name: "a.txt", Path: dir, BotID: "b"}, Total: 1, Offset: 1, Data: []byte("ab")},
		"no bot":     {Ref: noBot, Total: 1, Data: []byte("a")},
	}
	for label, chunk := range cases {
		svc, d := attachmentService(t)
		if _, err := svc.StageAttachmentChunk(context.Background(), "t1", chunk); err == nil {
			t.Errorf("%s: expected an error", label)
		}
		if len(d.wrotePaths) != 0 || len(d.commands) != 0 {
			t.Errorf("%s: something was written: %#v %#v", label, d.wrotePaths, d.commands)
		}
	}
}

func TestStageAttachmentChunkRejectsAFilePastTheWriteLimit(t *testing.T) {
	svc, d := attachmentService(t)
	big := make([]byte, 200<<10+1)
	if _, err := svc.StageAttachmentChunk(context.Background(), "t1", AttachmentChunk{
		Ref:    testRef("/home/desktop/.maclaw/data/tasks/t"),
		Offset: 0,
		Total:  int64(len(big)),
		Data:   big,
	}); err == nil {
		t.Fatal("a file above the container write limit was accepted")
	}
	if len(d.wrotePaths) != 0 || len(d.commands) != 0 {
		t.Fatalf("oversized file was written: %#v", d.wrotePaths)
	}
}

func TestStageAttachmentChunkReportsAFinishedUploadAgain(t *testing.T) {
	svc, d := attachmentService(t)
	chunk := AttachmentChunk{
		Ref:   testRef("/home/desktop/.maclaw/data/tasks/t"),
		Total: 2,
		Data:  []byte("ab"),
	}
	if _, err := svc.StageAttachmentChunk(context.Background(), "t1", chunk); err != nil {
		t.Fatalf("first: %v", err)
	}
	d.wrotePaths, d.commands = nil, nil
	again, err := svc.StageAttachmentChunk(context.Background(), "t1", chunk)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !again.Done || again.Path == "" {
		t.Fatalf("retry = %#v", again)
	}
	if len(d.wrotePaths) != 0 || len(d.commands) != 0 {
		t.Fatalf("the finished file was placed twice: %#v", d.wrotePaths)
	}
}

func TestStageAttachmentChunkSplitsIntoTwoChunks(t *testing.T) {
	svc, d := attachmentService(t)
	ref := testRef("/home/desktop/.maclaw/data/tasks/t")
	if _, err := svc.StageAttachmentChunk(context.Background(), "t1", AttachmentChunk{
		Ref: ref, Offset: 0, Total: 4, Data: []byte("ab"),
	}); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if len(d.commands) != 0 {
		t.Fatal("a partial upload must not be placed")
	}
	staged, err := svc.StageAttachmentChunk(context.Background(), "t1", AttachmentChunk{
		Ref: ref, Offset: 2, Total: 4, Data: []byte("cd"),
	})
	if err != nil {
		t.Fatalf("second chunk: %v", err)
	}
	if !staged.Done || staged.Received != 4 {
		t.Fatalf("staged = %#v", staged)
	}
	if len(d.commands) != 1 {
		t.Fatalf("commands = %#v", d.commands)
	}
}

func TestStageAttachmentChunkRejectsAnOffsetJump(t *testing.T) {
	svc, d := attachmentService(t)
	ref := testRef("/home/desktop/.maclaw/data/tasks/t")
	if _, err := svc.StageAttachmentChunk(context.Background(), "t1", AttachmentChunk{
		Ref: ref, Offset: 0, Total: 8, Data: []byte("ab"),
	}); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if _, err := svc.StageAttachmentChunk(context.Background(), "t1", AttachmentChunk{
		Ref: ref, Offset: 6, Total: 8, Data: []byte("cd"),
	}); err == nil {
		t.Fatal("a gap in the upload was accepted")
	}
	if len(d.commands) != 0 {
		t.Fatalf("commands = %#v", d.commands)
	}
}

func TestRenderAttachmentPlaceStaysInTheDesktopHome(t *testing.T) {
	if _, err := desktop.RenderAttachmentPlace("/home/desktop/.maclaw-attach/a/r/1-x", "/etc/passwd"); err == nil {
		t.Fatal("a target outside the container home was accepted")
	}
	argv, err := desktop.RenderAttachmentPlace("/home/desktop/.maclaw-attach/a/r/1-x", "/home/desktop/.maclaw/data/tasks/t/attachments/1-x")
	if err != nil {
		t.Fatalf("a task path was rejected: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "base64 -d") || !strings.Contains(joined, "/home/desktop/.maclaw/data/tasks/t/attachments/1-x") {
		t.Fatalf("argv = %#v", argv)
	}
	// A relative path resolves under the home like every container path,
	// but it must not be able to leave it.
	if _, err := desktop.RenderAttachmentPlace("~/.maclaw-attach/a", "/home/desktop/y"); err != nil {
		t.Fatalf("a ~ path inside the home was rejected: %v", err)
	}
	if _, err := desktop.RenderAttachmentPlace("/home/desktop/a", "/home/desktop/../../etc/x"); err == nil {
		t.Fatal("a path escaping the home was accepted")
	}
}

func TestAttachmentsFolderStaysNextToTheTask(t *testing.T) {
	taskDir := "/home/desktop/.maclaw/data/tasks/x"
	target := filepath.ToSlash(filepath.Join(taskDir, attachmentsFolder, "rc12345abc-a.txt"))
	if target != taskDir+"/attachments/rc12345abc-a.txt" {
		t.Fatalf("target = %q", target)
	}
	if fmt.Sprintf("%s", attachmentsFolder) != "attachments" {
		t.Fatal("folder name changed")
	}
}
