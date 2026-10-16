package botmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// turnKeySrv is a MaClawSrv stand-in that records the /messages body and
// serves a run whose status the test flips.
type turnKeySrv struct {
	mu       sync.Mutex
	messages []string
	status   string
	content  string
	blinks   int
	// runReads counts every read of this bot's run. Two watchers polling
	// the same run double this number, which is how a doubled settle is
	// observed without reaching into the service.
	runReads int
}

func (s *turnKeySrv) handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
		_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
		s.mu.Lock()
		status, content := s.status, s.content
		s.runReads++
		s.mu.Unlock()
		if status == "" {
			status = "running"
		}
		if status == "running" {
			_, _ = w.Write([]byte(`{"id":"run_turn","status":"running"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"run_turn","status":"` + status + `","desktop_ready":true,"message":{"content":"` + content + `"}}`))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
		s.mu.Lock()
		blink := s.blinks > 0
		if blink {
			s.blinks--
		}
		s.mu.Unlock()
		if blink {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message":"gateway blink"}`))
			return
		}
		writeInstanceSettings(w, r)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.messages = append(s.messages, string(raw))
		s.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_turn","status":"running"}}`))
	default:
		http.NotFound(w, r)
	}
}

func newTurnKeyService(t *testing.T, srv *httptest.Server) *Service {
	t.Helper()
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
	svc.admitReadyGrace = 30 * time.Millisecond
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	return svc
}

// The caller names the turn, and MaClawSrv receives that name as
// client_message_id. A repeated key replays the run it already started,
// which is the only thing that makes resending a lost admission answer
// safe.
func TestAdmitDesktopMessageCarriesTheCallersTurnKey(t *testing.T) {
	srvSvc := &turnKeySrv{}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "按这个安排执行", "execute", "turn-key-1")
	if err != nil {
		t.Fatal(err)
	}
	if admission.ClientMessageID != "turn-key-1" {
		t.Fatalf("admission key = %q", admission.ClientMessageID)
	}
	srvSvc.mu.Lock()
	messages := append([]string(nil), srvSvc.messages...)
	srvSvc.mu.Unlock()
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	if !strings.Contains(messages[0], `"client_message_id":"turn-key-1"`) {
		t.Fatalf("message body = %s", messages[0])
	}
}

// A caller that names no turn still gets one: the pipeline mints a key, so
// even an old client's command is replayable rather than a blind resend.
func TestAdmitDesktopMessageMintsATurnKeyWhenTheCallerHasNone(t *testing.T) {
	srvSvc := &turnKeySrv{}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(admission.ClientMessageID, "bot-turn-") {
		t.Fatalf("minted key = %q", admission.ClientMessageID)
	}
	srvSvc.mu.Lock()
	messages := append([]string(nil), srvSvc.messages...)
	srvSvc.mu.Unlock()
	if len(messages) != 1 || !strings.Contains(messages[0], `"client_message_id":"`+admission.ClientMessageID+`"`) {
		t.Fatalf("message body = %v", messages)
	}
}

// A gateway blink on the instance read used to fail a command before it was
// sent. The read is tried again, and the same turn reaches MaClawSrv once.
func TestAdmitDesktopMessageSurvivesAGatewayBlinkOnTheInstanceRead(t *testing.T) {
	srvSvc := &turnKeySrv{blinks: 1}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-blink")
	if err != nil {
		t.Fatal(err)
	}
	if admission.RunID != "run_turn" {
		t.Fatalf("admission = %#v", admission)
	}
	srvSvc.mu.Lock()
	messages := append([]string(nil), srvSvc.messages...)
	srvSvc.mu.Unlock()
	if len(messages) != 1 {
		t.Fatalf("message was sent %d times", len(messages))
	}
}

// A restarted Hub has no ledger entry for a run MaClawSrv is still driving.
// The miss asks MaClawSrv instead of answering "bot not found": a running
// run is adopted back, and its reply still reaches the person.
func TestDesktopRunResultAsksSrvWhenTheLedgerLostTheRun(t *testing.T) {
	srvSvc := &turnKeySrv{status: "succeeded", content: "重启后找回的回复"}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	first := newTurnKeyService(t, srv)

	admission, err := first.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-restart")
	if err != nil {
		t.Fatal(err)
	}
	// A second service over the same store is a Hub that restarted: same
	// settings, empty admit ledger.
	restarted := newTurnKeyService(t, srv)
	reply, done, err := restarted.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !done || reply.Text != "重启后找回的回复" {
		t.Fatalf("reply=%q done=%v", reply.Text, done)
	}
}

// The same miss on a run that is still going adopts it back, so the
// watcher this Hub does have settles it when it finishes.
func TestDesktopRunResultReadoptsARunningRunAfterRestart(t *testing.T) {
	srvSvc := &turnKeySrv{status: "running"}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	first := newTurnKeyService(t, srv)

	admission, err := first.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", "turn-readopt")
	if err != nil {
		t.Fatal(err)
	}
	restarted := newTurnKeyService(t, srv)
	if _, done, err := restarted.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID); err != nil || done {
		t.Fatalf("running run done=%v err=%v", done, err)
	}
	srvSvc.mu.Lock()
	srvSvc.status = "succeeded"
	srvSvc.content = "接管后完成"
	srvSvc.mu.Unlock()

	deadline := time.Now().Add(3 * time.Second)
	for {
		reply, done, err := restarted.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			if reply.Text != "接管后完成" {
				t.Fatalf("reply = %q", reply.Text)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("readopted run was never settled")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A run the ledger lost is adopted once. Several polls can discover the same
// miss at once — the miss reaches MaClawSrv outside any lock, so they
// overlap — and the watcher each one starts settles the run: it hands back
// the keyboard, clears the hold, and closes the desktop open. Settling twice
// is not idempotent. Only the first caller may claim the entry; the others
// read what it claimed.
func TestDesktopRunResultAdoptsALostRunOnlyOnce(t *testing.T) {
	// The run stays running so every watcher keeps polling. Counting those
	// polls is the observation: one watcher reads once per interval, and N
	// watchers read N times as often in the same window.
	srvSvc := &turnKeySrv{status: "running"}
	srv := httptest.NewServer(http.HandlerFunc(srvSvc.handler))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.admitPollInterval = 100 * time.Millisecond

	// Every caller sees the same miss at once: the read that resolves it
	// happens outside the ledger lock.
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_race")
		}()
	}
	wg.Wait()

	// Let every watcher that was started reach its polling loop.
	time.Sleep(700 * time.Millisecond)
	srvSvc.mu.Lock()
	before := srvSvc.runReads
	srvSvc.mu.Unlock()
	time.Sleep(1000 * time.Millisecond)
	srvSvc.mu.Lock()
	after := srvSvc.runReads
	srvSvc.mu.Unlock()

	// One watcher at a 100ms interval reads about 10 times a second, plus
	// jitter. Anything approaching twice that means the run is being
	// watched — and therefore settled — more than once.
	polls := after - before
	if polls > 16 {
		t.Fatalf("run is watched by more than one watcher: %d polls in one second", polls)
	}
}

// A run that had already finished when this Hub was asked about it is
// settled once. Settling hands back the keyboard, clears the hold, and
// closes the desktop open — none of which survives being done twice — and
// the caller asks again every time an answer is lost. The first caller
// settles and keeps the reply; later ones answer that same reply.
func TestSettleAdmittedDesktopRunSettlesOnceAndRepeatsTheSameReply(t *testing.T) {
	svc := NewService(&memSettings{})
	var calls int32
	var first Reply
	call := func() (Reply, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			first = Reply{Text: "结算一次"}
		}
		return first, nil
	}
	key := "run_done"
	reply, done, err := svc.settleAdmittedDesktopRun("t1", "u1", "bot_1", key, call)
	if err != nil || !done || reply.Text != "结算一次" {
		t.Fatalf("reply=%q done=%v err=%v", reply.Text, done, err)
	}
	for i := 0; i < 4; i++ {
		again, done, err := svc.settleAdmittedDesktopRun("t1", "u1", "bot_1", key, call)
		if err != nil || !done || again.Text != "结算一次" {
			t.Fatalf("repeat %d: reply=%q done=%v err=%v", i, again.Text, done, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("settle ran %d times, want 1", got)
	}
}

// Every caller sees the same miss at once, and settling is claimed rather
// than repeated, so concurrent callers together settle one time.
func TestSettleAdmittedDesktopRunSettlesOnceUnderConcurrentCallers(t *testing.T) {
	svc := NewService(&memSettings{})
	var calls int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = svc.settleAdmittedDesktopRun("t1", "u1", "bot_1", "run_race", func() (Reply, error) {
				atomic.AddInt32(&calls, 1)
				return Reply{Text: "结算一次"}, nil
			})
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("settle ran %d times across 8 concurrent callers, want 1", got)
	}
}

// A settle that failed must not keep its claim. Holding it would answer
// every later read with "still going" forever and strand the run for good,
// which is the dead end this whole fallback exists to remove.
func TestSettleAdmittedDesktopRunReleasesTheClaimWhenSettleFails(t *testing.T) {
	svc := NewService(&memSettings{})
	failing := func() (Reply, error) { return Reply{}, fmt.Errorf("settle failed") }
	if _, _, err := svc.settleAdmittedDesktopRun("t1", "u1", "bot_1", "run_err", failing); err == nil {
		t.Fatal("expected the settle error to surface")
	}
	var calls int32
	ok := func() (Reply, error) {
		atomic.AddInt32(&calls, 1)
		return Reply{Text: "第二次成功"}, nil
	}
	reply, done, err := svc.settleAdmittedDesktopRun("t1", "u1", "bot_1", "run_err", ok)
	if err != nil || !done || reply.Text != "第二次成功" {
		t.Fatalf("reply=%q done=%v err=%v", reply.Text, done, err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("retry after failure ran %d times, want 1", got)
	}
}

// A run MaClawSrv does not know either is still not found: the fallback
// reads the server, it does not invent an answer.
func TestDesktopRunResultKeepsNotFoundWhenSrvHasNoRunEither(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	_, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_unknown")
	if err == nil {
		t.Fatal("an unknown run must not answer a reply")
	}
	if done {
		t.Fatal("an unknown run must not be done")
	}
}

// A Hub that restarted cannot reach MaClawSrv to ask about a run it lost.
// That outage is not evidence the run is gone — the desktop may still be
// moving — so this answers "still running" and lets the next poll try again.
// Reporting the transport failure as a terminal error was the original bug in
// miniature: either way the person lost a run that was never actually dead.
func TestDesktopRunResultStaysPendingWhenSrvCannotBeAsked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message":"gateway down"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	_, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_lost")
	if err != nil {
		t.Fatalf("an unreachable MaClawSrv must not end the poll: %v", err)
	}
	if done {
		t.Fatal("an unverified run must not be reported as finished")
	}
}
