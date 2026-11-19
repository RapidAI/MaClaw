package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMCPMaskKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"mck_short", "mck_short"},
		{"mck_testkey", "mck_tes…key"},
		{"mck_0123456789abcdef0123", "mck_012345…0123"},
	}
	for _, tc := range cases {
		if got := mcpMaskKey(tc.in); got != tc.want {
			t.Fatalf("mcpMaskKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

type mcpAccessCaptureTasks struct {
	fakeMCPTasks
	mu  sync.Mutex
	ctx context.Context
}

func (f *mcpAccessCaptureTasks) mcpCreateTask(ctx context.Context, args mcpCreateArgs) (mcpTaskView, error) {
	f.mu.Lock()
	f.ctx = ctx
	f.mu.Unlock()
	return f.fakeMCPTasks.mcpCreateTask(ctx, args)
}

func postMCPAccessCall(handler http.Handler, remoteAddr, token string) *httptest.ResponseRecorder {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_task","arguments":{"name":"访问日志任务","prompt":"hi"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestMCPAccessClientInfoRidesRequestContext(t *testing.T) {
	api := &mcpAccessCaptureTasks{}
	token := "mck_accesskey0123456789"
	handler := newMCPHTTPLAN(func() string { return token }, api, func() bool { return true })

	rec := postMCPAccessCall(handler, "192.168.1.30:51000", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("remote create_task status %d", rec.Code)
	}
	api.mu.Lock()
	ctx := api.ctx
	api.mu.Unlock()
	info, ok := mcpClientInfoFromContext(ctx)
	if !ok {
		t.Fatalf("no client info in request context")
	}
	if info.IP != "192.168.1.30" {
		t.Fatalf("ip = %q, want 192.168.1.30", info.IP)
	}
	if info.APIKey != "mck_access…6789" {
		t.Fatalf("masked key = %q", info.APIKey)
	}

	rec = postMCPAccessCall(handler, "127.0.0.1:51001", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback create_task status %d", rec.Code)
	}
	api.mu.Lock()
	ctx = api.ctx
	api.mu.Unlock()
	if info, _ := mcpClientInfoFromContext(ctx); info.IP != "127.0.0.1" {
		t.Fatalf("loopback ip = %q", info.IP)
	}
}

func TestMCPAccessStoreRecordSnapshotClear(t *testing.T) {
	a := &App{}
	a.mcpAccess.dir = t.TempDir()
	stash := func(ip, key string) context.Context {
		return context.WithValue(context.Background(), mcpClientInfoContextKey{}, mcpClientInfo{IP: ip, APIKey: key})
	}
	a.recordMCPAccess(stash("10.0.0.9", "mck_0123456789abcdef0123"), "create_task", "任务A", `D:\work\a`)
	a.recordMCPAccess(stash("192.168.1.30", "mck_0123456789abcdef0123"), "send_command", "任务B", `D:\work\b`)
	a.recordMCPAccess(context.Background(), "create_task", "无来源不记录", `D:\work\ghost`)

	log := a.GetMCPAccessLog()
	if len(log) != 2 {
		t.Fatalf("log length %d, want 2 (direct calls without ctx are skipped)", len(log))
	}
	newest := log[0]
	if newest["task"] != "任务B" || newest["ip"] != "192.168.1.30" || newest["action"] != "send_command" {
		t.Fatalf("newest entry = %v, want 任务B/192.168.1.30/send_command", newest)
	}
	if newest["apiKey"] != "mck_012345…0123" {
		t.Fatalf("masked key = %v", newest["apiKey"])
	}
	if got, _ := newest["time"].(string); len(got) != len(mcpAccessTimeLayout) {
		t.Fatalf("time = %q, want layout %s", got, mcpAccessTimeLayout)
	}
	if log[1]["task"] != "任务A" {
		t.Fatalf("older entry = %v, want 任务A first", log[1])
	}

	// A fresh store over the same logs dir loads the file: history survives restarts.
	fresh := &App{}
	fresh.mcpAccess.dir = a.mcpAccess.dir
	if log := fresh.GetMCPAccessLog(); len(log) != 2 || log[0]["task"] != "任务B" {
		t.Fatalf("reloaded log = %v", log)
	}

	if err := fresh.ClearMCPAccessLog(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if log := fresh.GetMCPAccessLog(); len(log) != 0 {
		t.Fatalf("log after clear = %v", log)
	}
	if _, err := os.Stat(filepath.Join(a.mcpAccess.dir, mcpAccessFileBase)); !os.IsNotExist(err) {
		t.Fatalf("log file still present after clear: %v", err)
	}
}

func TestMCPAccessStoreRewriteKeepsTail(t *testing.T) {
	a := &App{}
	a.mcpAccess.dir = t.TempDir()
	// Pre-seed an oversized file directly; ensureLoadedLocked keeps the last 200.
	path := filepath.Join(a.mcpAccess.dir, mcpAccessFileBase)
	var lines []string
	for i := 0; i < 1200; i++ {
		entry := mcpAccessEntry{Time: time.Now(), IP: "10.1.1.1", APIKey: "mck_0123456789abcdef0123", Action: "create_task", Task: strings.Repeat("行", 350) + string(rune('A'+i%26))}
		data, _ := json.Marshal(entry)
		lines = append(lines, string(data))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stash := context.WithValue(context.Background(), mcpClientInfoContextKey{}, mcpClientInfo{IP: "10.0.0.2", APIKey: "mck_012345…0123"})
	a.recordMCPAccess(stash, "create_task", "最新任务", `D:\work\new`)

	if info, err := os.Stat(path); err != nil || info.Size() > mcpAccessFileMaxBytes {
		t.Fatalf("file size after rewrite = %v %v", info, err)
	}
	log := a.GetMCPAccessLog()
	if len(log) != mcpAccessEntryLimit {
		t.Fatalf("log length %d, want %d", len(log), mcpAccessEntryLimit)
	}
	if log[0]["task"] != "最新任务" {
		t.Fatalf("newest = %v, want 最新任务", log[0])
	}
}
