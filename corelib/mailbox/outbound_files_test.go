package mailbox

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSendFreezesAWorkspaceAttachmentAndRefusesAnEscape(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "天气.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 weather"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.pdf")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := testHost(twoAccounts(t), nil, &recordingSender{})
	host.WorkingDir = dir
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{"天气.pdf"},
	})
	if !strings.Contains(card, "天气.pdf") || !strings.Contains(card, "请确认发送这封邮件") {
		t.Fatalf("card=%s", card)
	}
	draft, ok := host.Drafts.Peek("draft-1")
	if !ok || len(draft.Message.Files) != 1 || string(draft.Message.Files[0].Bytes) != "%PDF-1.4 weather" {
		t.Fatalf("draft=%+v ok=%v", draft.Message.Files, ok)
	}
	raw := string(rfc822(draft.Message))
	boundary := mimeBoundary(t, raw)
	pdfHeader := "\r\n--" + boundary + "\r\nContent-Type: application/pdf"
	if !strings.Contains(raw, "multipart/mixed") || !strings.Contains(raw, pdfHeader) || !strings.Contains(raw, "filename*=UTF-8''") || !strings.Contains(raw, "filename=\"=?UTF-8?") {
		t.Fatalf("rfc822=%s", raw)
	}
	if strings.Count(raw, "\r\n--"+boundary) < 3 {
		t.Fatalf("attachment is not its own part: %s", raw)
	}
	if got := decodeMIMEPart(t, raw, boundary, "application/pdf"); string(got) != "%PDF-1.4 weather" {
		t.Fatalf("decoded pdf=%q", got)
	}
	reject := testHost(twoAccounts(t), nil, &recordingSender{})
	reject.WorkingDir = dir
	escaped := reject.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{outside},
	})
	if !strings.Contains(escaped, "附件必须位于任务工作区") {
		t.Fatalf("escape=%s", escaped)
	}
	missingHost := testHost(twoAccounts(t), nil, &recordingSender{})
	missingHost.WorkingDir = dir
	missing := missingHost.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{filepath.Join(dir, "missing.pdf")},
	})
	if !strings.Contains(missing, "找不到附件") {
		t.Fatalf("missing=%s", missing)
	}
}

func TestOutboundPathInsideIgnoresCaseAndKeepsAVolumeRoot(t *testing.T) {
	if !outboundPathInside(`C:\Work\Task`, `c:\work\task\天气.pdf`) {
		t.Fatal("case fold")
	}
	if outboundPathInside(`C:\work`, `C:\workspace\a.pdf`) {
		t.Fatal("prefix without a separator")
	}
	if !outboundPathInside(`C:\`, `C:\天气.pdf`) {
		t.Fatal("volume root")
	}
	if outboundPathInside(`C:\`, `D:\天气.pdf`) {
		t.Fatal("other volume")
	}
	if !outboundPathInside(`\\?\C:\Work`, `C:\work\天气.pdf`) {
		t.Fatal("extended prefix")
	}
}

func TestOutboundAttachmentAcceptsASlashPathWithACommaInTheName(t *testing.T) {
	dir := t.TempDir()
	name := "天气, 报告.pdf"
	abs := filepath.Join(dir, name)
	if err := os.WriteFile(abs, []byte("%PDF-1.4 weather"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := testHost(twoAccounts(t), nil, &recordingSender{})
	host.WorkingDir = dir
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{filepath.ToSlash(abs)},
	})
	if !strings.Contains(card, "请确认发送这封邮件") {
		t.Fatalf("slash path rejected: %s", card)
	}
	draft, ok := host.Drafts.Peek("draft-1")
	if !ok || len(draft.Message.Files) != 1 || draft.Message.Files[0].Name != name {
		t.Fatalf("draft files=%+v ok=%v", draft.Message.Files, ok)
	}
}

func TestOutboundAttachmentAllowsADifferentWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "天气.pdf"), []byte("%PDF-1.4 weather"), 0o644); err != nil {
		t.Fatal(err)
	}
	alt := dir
	if len(alt) >= 2 && alt[1] == ':' {
		switch {
		case alt[0] >= 'A' && alt[0] <= 'Z':
			alt = string(rune(alt[0]+32)) + alt[1:]
		case alt[0] >= 'a' && alt[0] <= 'z':
			alt = string(rune(alt[0]-32)) + alt[1:]
		}
	}
	host := testHost(twoAccounts(t), nil, &recordingSender{})
	host.WorkingDir = dir
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{filepath.Join(alt, "天气.pdf")},
	})
	if !strings.Contains(card, "请确认发送这封邮件") {
		t.Fatalf("case variant rejected: %s", card)
	}
}

func TestOutboundAttachmentRejectsALinkThatLeavesTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.pdf"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "outside")
	linkDir(t, link, outside)
	host := testHost(twoAccounts(t), nil, &recordingSender{})
	host.WorkingDir = dir
	escaped := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{filepath.Join("outside", "secret.pdf")},
	})
	if !strings.Contains(escaped, "附件必须位于任务工作区") {
		t.Fatalf("link escape=%s", escaped)
	}

	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "天气.pdf"), []byte("%PDF-1.4 weather"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(t.TempDir(), "ws")
	linkDir(t, ws, real)
	inside := testHost(twoAccounts(t), nil, &recordingSender{})
	inside.WorkingDir = ws
	card := inside.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{"天气.pdf"},
	})
	if !strings.Contains(card, "请确认发送这封邮件") {
		t.Fatalf("workspace link rejected a file inside it: %s", card)
	}
	// The same file addressed by the junction target, not the link path.
	realHost := testHost(twoAccounts(t), nil, &recordingSender{})
	realHost.WorkingDir = ws
	realCard := realHost.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{filepath.Join(real, "天气.pdf")},
	})
	if !strings.Contains(realCard, "请确认发送这封邮件") {
		t.Fatalf("resolved workspace path rejected: %s", realCard)
	}
}

func TestReplyAndForwardRefuseAttachmentsWithoutSpendingTheSend(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "天气.pdf"), []byte("%PDF-1.4 weather"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := testHost(twoAccounts(t), nil, &recordingSender{})
	host.WorkingDir = dir
	for _, action := range []string{"reply", "forward"} {
		got := host.Send(context.Background(), map[string]interface{}{
			"action": action, "uid": 3, "text": "ack", "to": []any{"a@ex.com"},
			"attachments": []any{"天气.pdf"},
		})
		if !strings.Contains(got, "只有发送新邮件可以带附件") {
			t.Fatalf("%s: %s", action, got)
		}
	}
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "天气", "text": "见附件",
		"attachments": []any{"天气.pdf"},
	})
	if !strings.Contains(card, "请确认发送这封邮件") || strings.Contains(card, "本轮已经处理过") {
		t.Fatalf("send after a refused reply: %s", card)
	}
}

func TestLongAttachmentNameStaysUnderTheSMTPLineLimit(t *testing.T) {
	name := strings.Repeat("天", 120) + ".pdf"
	raw := attachmentPart(OutboundFile{Name: name, Bytes: []byte("%PDF")})
	for _, line := range strings.Split(raw, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line len=%d %q", len(line), line[:80])
		}
	}
	if !strings.Contains(raw, "filename") || !strings.Contains(raw, "UTF-8") {
		t.Fatalf("name missing: %s", raw)
	}
}

func mimeBoundary(t *testing.T, raw string) string {
	t.Helper()
	const marker = `boundary="`
	i := strings.Index(raw, marker)
	if i < 0 {
		t.Fatalf("no boundary: %s", raw)
	}
	rest := raw[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j <= 0 {
		t.Fatalf("open boundary: %s", raw)
	}
	return rest[:j]
}

func decodeMIMEPart(t *testing.T, raw, boundary, media string) []byte {
	t.Helper()
	chunks := strings.Split(raw, "\r\n--"+boundary)
	for _, chunk := range chunks {
		if !strings.Contains(chunk, media) {
			continue
		}
		blank := strings.Index(chunk, "\r\n\r\n")
		if blank < 0 {
			t.Fatalf("part has no body: %s", chunk)
		}
		body := strings.TrimSpace(chunk[blank+4:])
		body = strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' {
				return -1
			}
			return r
		}, body)
		got, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Fatalf("missing %s in %s", media, raw)
	return nil
}

func linkDir(t *testing.T, link, target string) {
	t.Helper()
	symErr := os.Symlink(target, link)
	if symErr == nil {
		return
	}
	if runtime.GOOS != "windows" {
		t.Skip(symErr)
	}
	// A directory junction does not need the symlink privilege. mklink is a
	// cmd builtin, so the arguments stay separate and cmd quotes them.
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("symlink (%v) and junction (%v %s)", symErr, err, out)
	}
}
