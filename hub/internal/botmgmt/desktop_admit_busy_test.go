package botmgmt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAdmitDesktopMessageRefusesASecondExecute keeps one message post blocked
// and checks the next execute. A different client message is refused without
// a second post, and without waiting for the first post to finish. The same
// client message is posted again so MaClawSrv can replay it.
func TestAdmitDesktopMessageRefusesASecondExecute(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_, _ = w.Write([]byte(`{"id":"run_busy","status":"running"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			n := posts
			mu.Unlock()
			if n == 1 {
				select {
				case entered <- struct{}{}:
				default:
				}
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_busy","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-a")
		firstDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first execute did not reach the message post")
	}

	refused := make(chan error, 1)
	go func() {
		_, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "再开一个", "execute", "turn-b")
		refused <- err
	}()
	select {
	case err := <-refused:
		if !errors.Is(err, ErrDesktopBusy) {
			t.Fatalf("second execute err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("busy check waited on the message post")
	}
	mu.Lock()
	if posts != 1 {
		t.Fatalf("posts after refusal = %d", posts)
	}
	mu.Unlock()

	sameDone := make(chan error, 1)
	go func() {
		_, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-a")
		sameDone <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := posts
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("same client message was not posted again")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-sameDone; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if posts != 2 {
		t.Fatalf("posts = %d", posts)
	}
	mu.Unlock()

	svc.mu.Lock()
	empty := svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_alice", "")]
	real := svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_busy")]
	pins := svc.desktopTaskPins("tenant-a")
	svc.mu.Unlock()
	if empty != nil {
		t.Fatal("empty reserve was still in the ledger after the run id arrived")
	}
	if real == nil || real.clientMessageID != "turn-a" || real.runID != "run_busy" {
		t.Fatalf("real admit = %#v", real)
	}
	for _, pin := range pins {
		if pin == nil {
			continue
		}
		if pin.RunID == "" {
			t.Fatal("empty run id was persisted")
		}
		if pin.RunID == "run_busy" && pin.ClientMessageID != "turn-a" {
			t.Fatalf("pin key = %q", pin.ClientMessageID)
		}
	}

	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "第三件", "execute", "turn-c"); !errors.Is(err, ErrDesktopBusy) {
		t.Fatalf("third execute err = %v", err)
	}
	mu.Lock()
	if posts != 2 {
		t.Fatalf("posts after the live run = %d", posts)
	}
	mu.Unlock()
}

func TestAbandonedEmptyReserveDoesNotStayBusy(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_, _ = w.Write([]byte(`{"id":"run_next","status":"running"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_next","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	old := time.Now().Add(-31 * time.Second)
	emptyKey := desktopAdmitKey("tenant-a", "alice", "bot_alice", "")
	svc.desktopAdmits = map[string]*desktopAdmitState{
		emptyKey: {
			userID: "alice", botID: "bot_alice", runID: "", clientMessageID: "turn-old",
			posting: 0, done: false, status: "running", started: old, abandoned: old,
		},
	}

	text, items, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "后台在运行吗", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || text != "现在没有在做的事。" {
		t.Fatalf("abandoned reserve still listed text=%q items=%#v", text, items)
	}
	svc.mu.Lock()
	_, still := svc.desktopAdmits[emptyKey]
	svc.mu.Unlock()
	if still {
		t.Fatal("abandoned reserve was still in the ledger")
	}

	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-new"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if posts != 1 {
		t.Fatalf("posts = %d", posts)
	}
	mu.Unlock()
}

func TestPostingEmptyReserveStillCountsAsBusy(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			posts++
			http.Error(w, "should not post", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	old := time.Now().Add(-31 * time.Second)
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_alice", ""): {
			userID: "alice", botID: "bot_alice", runID: "", clientMessageID: "turn-hold",
			posting: 1, done: false, status: "running", task: "打开报表", started: old, abandoned: old,
		},
	}

	text, items, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != "running" || items[0].RunID != "" {
		t.Fatalf("posting reserve = %q %#v", text, items)
	}
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "另一件", "execute", "turn-other"); !errors.Is(err, ErrDesktopBusy) {
		t.Fatalf("err = %v", err)
	}
	if posts != 0 {
		t.Fatalf("posts = %d", posts)
	}
}

func TestFailedAdmitDropsTheEmptyReserve(t *testing.T) {
	var mu sync.Mutex
	fail := true
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_, _ = w.Write([]byte(`{"id":"run_ok","status":"running"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			reject := fail
			mu.Unlock()
			if reject {
				http.Error(w, "nope", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_ok","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-fail"); err == nil {
		t.Fatal("expected the message post to fail")
	}
	text, items, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "后台在运行吗", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || text != "现在没有在做的事。" {
		t.Fatalf("failed reserve still busy text=%q items=%#v", text, items)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-ok"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if posts != 2 {
		t.Fatalf("posts = %d", posts)
	}
	mu.Unlock()
}

func TestRestoredRunReplaysTheStoredClientMessage(t *testing.T) {
	srvSvc := &turnKeySrv{}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	first := newTurnKeyService(t, srv)
	if _, err := first.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-restore"); err != nil {
		t.Fatal(err)
	}
	raw, err := first.System.Get(context.Background(), storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"client_message_id":"turn-restore"`) || strings.Contains(raw, `"run_id":""`) {
		t.Fatalf("persisted tasks = %s", raw)
	}

	restarted := NewService(first.System)
	restarted.HTTP = srv.Client()
	restarted.admitPollInterval = 10 * time.Millisecond
	restarted.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	if _, err := restarted.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-other"); !errors.Is(err, ErrDesktopBusy) {
		t.Fatalf("other key after restart err = %v", err)
	}
	srvSvc.mu.Lock()
	before := len(srvSvc.messages)
	srvSvc.mu.Unlock()
	if _, err := restarted.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-restore"); err != nil {
		t.Fatal(err)
	}
	srvSvc.mu.Lock()
	after := len(srvSvc.messages)
	body := ""
	if after > 0 {
		body = srvSvc.messages[after-1]
	}
	srvSvc.mu.Unlock()
	if after != before+1 || !strings.Contains(body, `"client_message_id":"turn-restore"`) {
		t.Fatalf("replay posts before=%d after=%d body=%s", before, after, body)
	}
}
