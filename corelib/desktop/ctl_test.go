package desktop

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCtlArgsAndDecode(t *testing.T) {
	args, err := CtlArgs(map[string]any{"op": " clipboard_set ", "text": "你好"})
	if err != nil || len(args) != 5 || !strings.Contains(args[2], "/desktop_supervisor.py ctl") {
		t.Fatalf("args=%v err=%v", args, err)
	}
	raw, _ := base64.StdEncoding.DecodeString(args[4])
	if !strings.Contains(string(raw), `"op":"clipboard_set"`) || !strings.Contains(string(raw), "你好") {
		t.Fatalf("request=%s", raw)
	}
	if _, err := CtlArgs(map[string]any{"op": "exec"}); err == nil {
		t.Fatal("unknown op accepted")
	}
	if _, err := CtlArgs(map[string]any{"op": "clipboard_set", "text": strings.Repeat("x", CtlRequestMax)}); err == nil {
		t.Fatal("oversized request accepted")
	}
	if out, err := DecodeCtl("warning\n{\"ok\":true,\"n\":1}"); err != nil || out["ok"] != true {
		t.Fatalf("decode=%v %v", out, err)
	}
	if _, err := DecodeCtl("usage: desktop_supervisor.py ensure <user-key>"); err == nil {
		t.Fatal("old supervisor output accepted")
	}
	if !CtlReadOnly(CtlWindows) || CtlReadOnly(CtlWindow) {
		t.Fatal("read-only set is wrong")
	}
}
