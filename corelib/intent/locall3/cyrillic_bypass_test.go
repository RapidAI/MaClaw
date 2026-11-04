package locall3

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// confidentRuntime: a head that confidently answers file_ops for any input.
func confidentRuntime(t *testing.T, mode string) *Runtime {
	t.Helper()
	t.Setenv(intent.LocalHeadEnv, "")
	dir := t.TempDir()
	writeHead(t, dir, intent.ShippedLocalHeadName, "qwen3", 1024, nil)
	rt := NewWithMode(dir, mode)
	rt.Logf = func(string, ...any) {}
	if _, err := rt.Attach(qwen); err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestCyrillicBypassesLocalHeadToRemote(t *testing.T) {
	rt := confidentRuntime(t, intent.LocalL3On)
	calls := 0
	fn := rt.ContextFunc(func(context.Context, context.Context, string, string) (string, error) {
		calls++
		return `{"top":[{"skill":"ssh","score":0.9}]}`, nil
	})
	out, err := fn(context.Background(), context.Background(), "", "Серверийн төлөвийг шалгана уу")
	if err != nil || calls != 1 || !strings.Contains(out, "ssh") {
		t.Fatalf("cyrillic: out=%q err=%v calls=%d", out, err, calls)
	}
	// zh-Hans / zh-Hant / en unchanged: the confident local head decides, remote untouched.
	for _, q := range []string{"整理一下这个文件夹", "整理一下這個資料夾", "tidy up this folder"} {
		out, err := fn(context.Background(), context.Background(), "", q)
		if err != nil || calls != 1 || !strings.Contains(out, "file_ops") {
			t.Fatalf("%q: out=%q err=%v calls=%d", q, out, err, calls)
		}
	}
}

func TestCyrillicStrictNoRemoteAbstainsWithoutLabel(t *testing.T) {
	rt := confidentRuntime(t, intent.LocalL3Strict)
	calls := 0
	fn := rt.ContextFunc(func(context.Context, context.Context, string, string) (string, error) { calls++; return "", nil })
	_, err := fn(context.Background(), context.Background(), "", "Файлуудыг цэгцэл")
	if err == nil || calls != 0 || !intent.IsLocalHeadUncertain(err) {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if _, _, ok := intent.LocalHeadUncertainTop(err); ok {
		t.Fatal("bypass must not carry a head label (no head veto)")
	}
}

func TestCyrillicProbeSkipped(t *testing.T) {
	rt := confidentRuntime(t, intent.LocalL3On)
	p := rt.ProbeFunc()
	if _, _, _, ok := p(context.Background(), "Файлуудыг цэгцэл"); ok {
		t.Fatal("probe must skip Cyrillic input")
	}
}
