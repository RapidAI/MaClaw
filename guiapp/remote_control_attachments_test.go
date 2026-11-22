package guiapp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRemoteAttachmentPathsKeepsOnlyTaskAttachments(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "清理-1699999")
	inside := filepath.Join(taskDir, "attachments", "rc-abc-1-报告.pdf")
	raw := "看一下这个\n\n附件：\n- " + inside + "\n"

	text, paths := extractRemoteAttachmentPaths(taskDir, raw)
	if text != "看一下这个" {
		t.Fatalf("text = %q, want the person's words only", text)
	}
	if len(paths) != 1 || paths[0] != inside {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestExtractRemoteAttachmentPathsRejectsOutsidePaths(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	raw := "附件（1 个）：\n- " + outside

	text, paths := extractRemoteAttachmentPaths(taskDir, raw)
	if len(paths) != 0 {
		t.Fatalf("an outside path was accepted: %#v", paths)
	}
	if text != raw {
		t.Fatalf("text changed without a valid attachment: %q", text)
	}
}

func TestExtractRemoteAttachmentPathsRejectsSiblingEscape(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-2")
	escape := filepath.Join(taskDir, "attachments", "..", "..", "evil.sh")
	raw := "附件：\n- " + escape

	if _, paths := extractRemoteAttachmentPaths(taskDir, raw); len(paths) != 0 {
		t.Fatalf("escape accepted: %#v", paths)
	}
}

func TestRemoteAttachmentTurnPromptAndDisplayAgree(t *testing.T) {
	paths := []string{
		`/home/desktop/.maclaw/data/tasks/a-1/attachments/rc-1-x.pdf`,
	}
	turn := remoteAttachmentTurnPrompt(paths, "总结一下")
	if !strings.HasPrefix(turn, remoteAttachmentTurnPrefix) {
		t.Fatalf("turn prompt lost its prefix: %q", turn)
	}
	if !strings.Contains(turn, "总结一下") || !strings.Contains(turn, paths[0]) {
		t.Fatalf("turn prompt = %q", turn)
	}
	display := remoteControlDisplayWithAttachments(turn, paths)
	if strings.Contains(display, remoteAttachmentTurnPrefix) {
		t.Fatalf("display leaked the instruction: %q", display)
	}
	if !strings.Contains(display, "总结一下") || !strings.Contains(display, paths[0]) {
		t.Fatalf("display = %q", display)
	}
}

func TestRemoteAttachmentDisplayWithoutPathsIsUnchanged(t *testing.T) {
	if got := remoteControlDisplayWithAttachments("继续", nil); got != "继续" {
		t.Fatalf("got %q", got)
	}
}

func TestRemoteAttachmentDisplayForAttachmentOnlyMessage(t *testing.T) {
	paths := []string{
		`/home/desktop/.maclaw/data/tasks/a-1/attachments/rc-1-x.pdf`,
		`/home/desktop/.maclaw/data/tasks/a-1/attachments/rc-2-y.png`,
	}
	turn := remoteAttachmentTurnPrompt(paths, "")
	display := remoteControlDisplayWithAttachments(turn, paths)
	// The row is the attachment block only: the "- <path>" lines must appear
	// once (inside the block), not once as leftover body text and once as the
	// list.
	if strings.Count(display, paths[0]) != 1 || strings.Count(display, paths[1]) != 1 {
		t.Fatalf("paths repeated in display: %q", display)
	}
	if !strings.Contains(display, "附件（2 个）：") {
		t.Fatalf("display = %q", display)
	}
}

func TestIsRemoteAttachmentHeader(t *testing.T) {
	for _, line := range []string{"附件：", "附件:", "附件（3 个）：", "附件(3 个):"} {
		if !isRemoteAttachmentHeader(line) {
			t.Errorf("%q should start an attachment block", line)
		}
	}
	for _, line := range []string{"附件", "附件里的图", "- /tmp/a"} {
		if isRemoteAttachmentHeader(line) {
			t.Errorf("%q must not start an attachment block", line)
		}
	}
}
