package main

import (
	"context"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// desktopWatchFiles is the file slot of one server-started turn (a
// background-job check or resume). Such a turn has no chat reply, so files
// it delivers ride on the say that carries its final reply; Hub stores them
// on the said note and the GUI shows them as chips (papers 11-12 01:09:
// Bot 1's REPORT.md could not reach the chat from a resume turn).
type desktopWatchFiles struct {
	mu   sync.Mutex
	slot *desktopShotSlot
}

func (w *desktopWatchFiles) prepare(ctx context.Context) context.Context {
	next, slot := withDesktopShotSlot(ctx)
	w.mu.Lock()
	w.slot = slot
	w.mu.Unlock()
	return next
}

// take returns the delivered files once; nil when nothing was delivered.
func (w *desktopWatchFiles) take() []agent.MessageAttachment {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	slot := w.slot
	w.slot = nil
	w.mu.Unlock()
	if slot == nil {
		return nil
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	out := append([]agent.MessageAttachment(nil), slot.files...)
	if len(out) > desktopWatchFilesMax {
		out = out[:desktopWatchFilesMax]
	}
	return out
}

// desktopWatchFilesMax matches Hub's say: at most three files per note.
const desktopWatchFilesMax = 3

func desktopWatchFileNames(files []agent.MessageAttachment) string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, firstNonEmptyString(strings.TrimSpace(f.FileName), "文件"))
	}
	return strings.Join(names, "、")
}

// desktopWatchFilesText is the said text: the reply, or a short line when
// the turn already spoke and only its files are left to say.
func desktopWatchFilesText(text string, files []agent.MessageAttachment) string {
	if len(files) == 0 {
		return text
	}
	if strings.TrimSpace(text) == "" {
		return "附上文件：" + desktopWatchFileNames(files)
	}
	return text
}

func desktopWatchFilesBody(files []agent.MessageAttachment) []map[string]string {
	out := make([]map[string]string, 0, len(files))
	for _, f := range files {
		out = append(out, map[string]string{"name": f.FileName, "mime": f.MimeType, "data": f.Data})
	}
	return out
}

func desktopWatchFilesMissed(files []agent.MessageAttachment) string {
	return "（附件没能随这条消息发出，文件还在电脑上：" + desktopWatchFileNames(files) + "）"
}

// desktopSayRefusedFiles: the say with files did not land. An older Hub
// refuses a body over 64 KB (invalid say request, or it drops the
// connection mid-upload), so any failure other than "already said" is
// retried as text that names the files.
func desktopSayRefusedFiles(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return !strings.Contains(msg, "ALREADY_SAID") && !strings.Contains(msg, "409")
}
