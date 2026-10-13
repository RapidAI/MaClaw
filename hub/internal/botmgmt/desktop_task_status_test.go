package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDesktopTaskStatusQueryAndOfflineDelivery(t *testing.T) {
	var gate sync.Mutex
	ready := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			gate.Lock()
			goNow := ready
			gate.Unlock()
			if !goNow {
				_, _ = w.Write([]byte(`{"id":"run_report","status":"running"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"run_report","status":"succeeded","desktop_ready":true,"message":{"content":"报表已下载"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "下载报表") {
				http.Error(w, "task", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_report","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.admitPollInterval = 10 * time.Millisecond
	svc.admitReadyGrace = 20 * time.Millisecond
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}

	admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "下载报表并保存", "execute", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	text, items, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "那个下载报表怎么样了", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != "running" || items[0].RunID != admission.RunID {
		t.Fatalf("running query = %q %#v", text, items)
	}
	if !strings.Contains(text, "还在进行") || !strings.Contains(text, "下载报表") {
		t.Fatalf("running sentence = %q", text)
	}

	gate.Lock()
	ready = true
	gate.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending, err := svc.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 && pending[0].Reply.Text == "报表已下载" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending = %#v", pending)
		}
		time.Sleep(10 * time.Millisecond)
	}

	restarted := NewService(settings)
	restarted.HTTP = srv.Client()
	restarted.Desktop = svc.Desktop
	pending, err := restarted.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].RunID != admission.RunID || pending[0].Reply.Text != "报表已下载" {
		t.Fatalf("restarted pending = %#v", pending)
	}
	shown, _, err := restarted.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "下载报表", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "已经完成") || !strings.Contains(shown, "报表已下载") {
		t.Fatalf("finished sentence = %q", shown)
	}
	miss, _, err := restarted.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "另一个完全无关的名字", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(miss, "没有找到") {
		t.Fatalf("miss = %q", miss)
	}
	if err := restarted.AckDesktopTask(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID); err != nil {
		t.Fatal(err)
	}
	pending, err = restarted.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("acked pending = %#v", pending)
	}
	again := NewService(settings)
	again.HTTP = srv.Client()
	pending, err = again.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("ack did not stick across restart: %#v", pending)
	}
}

func TestDesktopTaskHintDropsStatusWords(t *testing.T) {
	if got := desktopTaskHint("那个下载报表怎么样了"); got != "下载报表" {
		t.Fatalf("hint = %q", got)
	}
	if got := desktopTaskHint("任务状态"); got != "" {
		t.Fatalf("generic hint = %q", got)
	}
	if !desktopTaskMatches("下载的进度", "下载报表并保存") {
		t.Fatal("progress question did not match the download")
	}
	if !desktopTaskMatches("了解竞品怎么样了", "了解竞品") {
		t.Fatal("a particle inside the task name was stripped")
	}
	if desktopTaskMatches("了解竞品怎么样了", "打开报表") {
		t.Fatal("an unrelated task matched")
	}
	if got := desktopTaskHint("任务分配怎么样了"); got != "任务分配" {
		t.Fatalf("task name was split: %q", got)
	}
	if desktopTaskMatches("任务分配怎么样了", "分配座位") {
		t.Fatal("a split name matched a different task")
	}
	if got := desktopTaskHint("项目进度怎么样了"); got != "项目进度" {
		t.Fatalf("progress in the name was stripped: %q", got)
	}
	if desktopTaskMatches("项目进度怎么样了", "项目计划") {
		t.Fatal("a shortened name matched a different task")
	}
	if desktopTaskMatches("download report", "annual report") {
		t.Fatal("one shared word matched a different task")
	}
	if !desktopTaskMatches("download report", "download the quarterly report") {
		t.Fatal("both words should match the report task")
	}
	if !desktopTaskMatches("下载的报表怎么样了", "下载报表并保存") {
		t.Fatal("a linker 的 hid the report task")
	}
	if !desktopTaskMatches("目的地怎么样了", "目的地") {
		t.Fatal("的 inside the task name was removed")
	}
	narrowed := preferDesktopTaskMatches("项目的进度怎么样了", []DesktopTaskStatus{
		{Task: "项目计划", Status: "succeeded"},
		{Task: "项目进度", Status: "running"},
	})
	if len(narrowed) != 1 || narrowed[0].Task != "项目进度" {
		t.Fatalf("narrowed = %#v", narrowed)
	}
	kept := preferDesktopTaskMatches("下载的进度", []DesktopTaskStatus{{Task: "下载报表并保存", Status: "running"}})
	if len(kept) != 1 || kept[0].Task != "下载报表并保存" {
		t.Fatalf("kept = %#v", kept)
	}
}

func TestDesktopTaskStatusOrdersRunningThenNewest(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	items := []DesktopTaskStatus{
		{RunID: "old", Status: "succeeded", Started: older},
		{RunID: "new", Status: "succeeded", Started: newer},
		{RunID: "run", Status: "running", Started: older},
	}
	sortDesktopTaskStatus(items)
	if items[0].RunID != "run" || items[1].RunID != "new" || items[2].RunID != "old" {
		t.Fatalf("%#v", items)
	}
}
