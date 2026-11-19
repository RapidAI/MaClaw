package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	if !strings.Contains(script, "-gt "+strconv.Itoa(saidInlineRawMax("x.zip", desktopDeliverMIME("x.zip")))+" ]") {
		t.Fatalf("inline threshold missing: %s", script)
	}
	// Without a store, a file the inbox JSON cannot carry is not read
	// again to be inlined. The other GUI could not read that body.
	calls := 0
	desktopDeliverLargeBash = func(_ context.Context, _ agentruntime.Scope, cmd string) (string, error) {
		calls++
		return "MCLDL_STORE 3000000\n", nil
	}
	desktopDeliverViaStore = func(context.Context, agentruntime.Scope, string, string) (string, error) {
		return "", errors.New("desktop document could not be sent: this server's Hub has no bot file store yet")
	}
	_, err := deliverDesktopLarge(context.Background(), scope, "/root/x.pdf", "x.pdf")
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "no bot file store yet") {
		t.Fatalf("fallback calls=%d err=%v", calls, err)
	}
}

func TestUploadKeepsGoingWhileBytesAdvance(t *testing.T) {
	old := botFileHub
	defer func() { botFileHub = old }()
	var got int64
	botFileHub = func(_ context.Context, method, _ string, _ map[string]string, _ io.Reader, _ int64) ([]byte, int, error) {
		if method == http.MethodPost {
			raw, _ := json.Marshal(map[string]any{"ok": true, "id": "bf_00000000000000000000001", "upload_token": "tok"})
			return raw, http.StatusOK, nil
		}
		got++
		raw, _ := json.Marshal(map[string]any{"ok": true, "received": got, "complete": got >= 8})
		return raw, http.StatusOK, nil
	}
	id, err := botFileUploadBytes(context.Background(), agentruntime.Scope{}, "a.png", "image/png", bytes.Repeat([]byte{'a'}, 8))
	if err != nil || id != "bf_00000000000000000000001" || got != 8 {
		t.Fatalf("id %q got %d err %v", id, got, err)
	}
}

func TestUploadDoesNotReturnAnUnfinishedFile(t *testing.T) {
	old := botFileHub
	defer func() { botFileHub = old }()
	botFileHub = func(_ context.Context, method, _ string, _ map[string]string, _ io.Reader, _ int64) ([]byte, int, error) {
		if method == http.MethodPost {
			raw, _ := json.Marshal(map[string]any{"ok": true, "id": "bf_00000000000000000000001", "upload_token": "tok"})
			return raw, http.StatusOK, nil
		}
		raw, _ := json.Marshal(map[string]any{"ok": true, "received": 4, "complete": false})
		return raw, http.StatusOK, nil
	}
	if _, err := botFileUploadBytes(context.Background(), agentruntime.Scope{}, "a.png", "image/png", []byte("abcd")); err == nil {
		t.Fatal("unfinished upload was treated as stored")
	}
	botFileHub = func(_ context.Context, method, _ string, _ map[string]string, _ io.Reader, _ int64) ([]byte, int, error) {
		if method == http.MethodPost {
			raw, _ := json.Marshal(map[string]any{"ok": true, "id": "bf_00000000000000000000001", "upload_token": "tok"})
			return raw, http.StatusOK, nil
		}
		raw, _ := json.Marshal(map[string]any{"ok": true, "received": 4, "complete": true})
		return raw, http.StatusOK, nil
	}
	id, err := botFileUploadBytes(context.Background(), agentruntime.Scope{}, "a.png", "image/png", []byte("abcd"))
	if err != nil || id != "bf_00000000000000000000001" {
		t.Fatalf("id %q err %v", id, err)
	}
}

func TestInboxFileBodyMatchesTheResponse(t *testing.T) {
	name := strings.Repeat("&", 76) + ".png"
	mime := "image/png"
	n := 100
	raw, err := json.Marshal(map[string]any{
		"name": name, "mime": mime, "size": n, "sha256": strings.Repeat("0", 64),
		"data": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{'a'}, n)),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Hub's encoder adds a newline after the object.
	if inboxFileBodyLen(name, mime, n) != len(raw)+1 {
		t.Fatalf("measured %d, response %d", inboxFileBodyLen(name, mime, n), len(raw)+1)
	}
	max := saidInlineRawMax(name, mime)
	if inboxFileBodyLen(name, mime, max) > 1<<20 {
		t.Fatalf("inline max %d does not fit the inbox read", max)
	}
	if max < botFileInlineMax() && inboxFileBodyLen(name, mime, max+1) <= 1<<20 {
		t.Fatalf("inline max %d stopped early", max)
	}
	if saidFileFitsInbox("shot.png", "image/png", 900_000) {
		t.Fatal("900KB fits the inbox body")
	}
	if !saidFileFitsInbox("b.pdf", "application/pdf", 300_000) {
		t.Fatal("300KB does not fit the inbox body")
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
	// 900 KB of PNG is under the old 1 MB inline cap, but its base64 does
	// not fit the 1 MB inbox response the other GUI reads. It is a reference.
	uploaded = 0
	if _, err := desktopSayFile(context.Background(), scope, "shot.png", make([]byte, 900_000)); err != nil {
		t.Fatal(err)
	}
	files = said["files"].([]map[string]string)
	if uploaded != 900_000 || files[0]["ref"] == "" || files[0]["data"] != "" {
		t.Fatalf("picture was inlined: uploaded=%d files=%v", uploaded, files)
	}
	// No store: the picture is not stuffed into the inbox JSON.
	said = nil
	desktopSayUploadBytes = func(context.Context, agentruntime.Scope, string, string, []byte) (string, error) {
		return "", errors.New("this server's Hub has no bot file store yet")
	}
	if _, err := desktopSayFile(context.Background(), scope, "shot.png", make([]byte, 900_000)); err == nil || said != nil {
		t.Fatalf("inlined without a store: %v", said)
	}
}
