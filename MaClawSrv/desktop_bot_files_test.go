package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// A large file on the cloud desktop goes to the store through the real
// helper (several resumable chunks) and the chat message carries only the
// reference.
func TestDeliverLargeFileGoesThroughStore(t *testing.T) {
	r := newRelayRig(t)
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u1", InstanceID: "inst4", RunID: "run_big"}
	data := bytes.Repeat([]byte("0123456789abcdef"), (18<<20)/16) // 18 MB: two store chunks
	src := filepath.Join(r.cloud, "big.pdf")
	os.WriteFile(src, data, 0o644)
	var said map[string]any
	old := desktopDeliverLargeSay
	desktopDeliverLargeSay = func(_ context.Context, body map[string]any) error { said = body; return nil }
	defer func() { desktopDeliverLargeSay = old }()
	out, err := deliverDesktopViaStore(context.Background(), scope, src, "big.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "separate message") {
		t.Fatalf("reply %q", out)
	}
	files, _ := said["files"].([]map[string]string)
	if len(files) != 1 || files[0]["ref"] == "" || files[0]["data"] != "" {
		t.Fatalf("message files %v", said["files"])
	}
	r.files.mu.Lock()
	got := r.files.data[files[0]["ref"]]
	r.files.mu.Unlock()
	if !bytes.Equal(got, data) {
		t.Fatalf("store has %d bytes, want %d", len(got), len(data))
	}
}

func TestDeliverLargeRoutesBySize(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u1", InstanceID: "inst4"}
	oldBash, oldStore := desktopDeliverLargeBash, desktopDeliverViaStore
	defer func() { desktopDeliverLargeBash, desktopDeliverViaStore = oldBash, oldStore }()
	var script string
	desktopDeliverLargeBash = func(_ context.Context, _ agentruntime.Scope, cmd string) (string, error) {
		script = cmd
		return "MCLDL_STORE 50000000\n", nil
	}
	stored := false
	desktopDeliverViaStore = func(context.Context, agentruntime.Scope, string, string) (string, error) {
		stored = true
		return "ok", nil
	}
	if out, err := deliverDesktopLarge(context.Background(), scope, "/root/x.zip", "x.zip"); err != nil || out != "ok" || !stored {
		t.Fatalf("store route: %q %v", out, err)
	}
	if !strings.Contains(script, `-gt 1048576 ]`) {
		t.Fatalf("inline threshold missing: %s", script)
	}
	// An older Hub without the store: up to 8 MB still goes inline.
	calls := 0
	desktopDeliverLargeBash = func(_ context.Context, _ agentruntime.Scope, cmd string) (string, error) {
		calls++
		if calls == 1 {
			return "MCLDL_STORE 3000000\n", nil
		}
		return "MCLDL_BIG 3000000\n", nil // stop before reading pieces
	}
	desktopDeliverViaStore = func(context.Context, agentruntime.Scope, string, string) (string, error) {
		return "", errors.New("desktop document could not be sent: this server's Hub has no bot file store yet")
	}
	_, err := deliverDesktopLarge(context.Background(), scope, "/root/x.pdf", "x.pdf")
	if calls != 2 || err == nil {
		t.Fatalf("fallback calls=%d err=%v", calls, err)
	}
}

func TestSayFileOverInlineUsesStore(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u1", InstanceID: "inst4", RunID: "r"}
	oldUp, oldSay := desktopSayUploadBytes, desktopDeliverLargeSay
	defer func() { desktopSayUploadBytes, desktopDeliverLargeSay = oldUp, oldSay }()
	var uploaded int
	desktopSayUploadBytes = func(_ context.Context, _ agentruntime.Scope, _, _ string, raw []byte) (string, error) {
		uploaded = len(raw)
		return "bf_00000000000000000000001", nil
	}
	var said map[string]any
	desktopDeliverLargeSay = func(_ context.Context, body map[string]any) error { said = body; return nil }
	if _, err := desktopSayFile(context.Background(), scope, "a.docx", make([]byte, 2<<20)); err != nil {
		t.Fatal(err)
	}
	files := said["files"].([]map[string]string)
	if uploaded != 2<<20 || files[0]["ref"] == "" || files[0]["data"] != "" {
		t.Fatalf("uploaded=%d files=%v", uploaded, files)
	}
	// Small files stay inline.
	uploaded = 0
	if _, err := desktopSayFile(context.Background(), scope, "b.pdf", make([]byte, 300_000)); err != nil {
		t.Fatal(err)
	}
	files = said["files"].([]map[string]string)
	if uploaded != 0 || files[0]["data"] == "" {
		t.Fatalf("small file not inline: %v", files[0]["ref"])
	}
}
