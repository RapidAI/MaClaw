package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestRemoteDesktopDoesNotUseLocalDocker(t *testing.T) {
	var gotUser, gotMemory string
	dockerHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"memory":"8g"`) {
			t.Errorf("resource body = %s", body)
		}
		_, _ = w.Write([]byte(`{"cdp_url":"http://docker.example:32768","display":":20"}`))
	}))
	defer dockerHost.Close()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hub-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		gotUser = string(body)
		if r.URL.Path == "/api/v1/desktop-services/app" {
			_, _ = w.Write([]byte(`{"output":"ok"}`))
			return
		}
		req, _ := http.NewRequest(http.MethodPost, dockerHost.URL+"/v1/desktops/session", strings.NewReader(`{"memory":"8g"}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		gotMemory = string(raw)
		w.Write(raw)
		_ = body
	}))
	defer hub.Close()
	t.Setenv(desktopHubURLEnv, hub.URL)
	t.Setenv(desktopHubTokenEnv, "hub-token")
	t.Setenv(desktopCDPEnv, "")

	endpoint, err := desktopSession(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.CDP != "http://docker.example:32768" || endpoint.Display != ":20" {
		t.Fatalf("endpoint=%#v", endpoint)
	}
	if !strings.Contains(gotUser, `"user_id":"alice"`) || !strings.Contains(gotMemory, "docker.example") {
		t.Fatalf("user=%s memory=%s", gotUser, gotMemory)
	}
	out, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_list", nil, nil)
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("app out=%q err=%v", out, err)
	}
}

func TestInstanceDesktopFollowsTheBoundUser(t *testing.T) {
	rememberDesktopOwner("inst_duty", map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a"}, "service-account", "maclaw-tenant")
	t.Cleanup(func() { desktopUserByInstance.Delete("inst_duty") })
	owned := desktopOwner(agentruntime.Scope{TenantID: "maclaw-tenant", UserID: "service-account", InstanceID: "inst_duty"})
	if owned.UserID != "alice" || owned.TenantID != "tenant-a" {
		t.Fatalf("desktop account = %s/%s", owned.TenantID, owned.UserID)
	}
	plain := desktopOwner(agentruntime.Scope{TenantID: "maclaw-tenant", UserID: "service-account", InstanceID: "inst_other"})
	if plain.UserID != "service-account" || plain.TenantID != "maclaw-tenant" {
		t.Fatalf("unbound desktop account = %s/%s", plain.TenantID, plain.UserID)
	}
	aliceKey, err := desktop.UserKey(owned.TenantID, owned.UserID)
	if err != nil {
		t.Fatal(err)
	}
	serviceKey, err := desktop.UserKey("maclaw-tenant", "service-account")
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, err := desktop.UserKey("maclaw-tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if aliceKey == serviceKey || aliceKey == wrongKey {
		t.Fatal("desktop key did not follow the Hub tenant")
	}
	missedUser, missedTenant := rememberDesktopOwner("inst_duty", nil, "service-account", "maclaw-tenant")
	if missedUser != "alice" || missedTenant != "tenant-a" {
		t.Fatalf("missed read moved the desktop to %s/%s", missedTenant, missedUser)
	}
	emptyUser, emptyTenant := rememberDesktopOwner("inst_duty", map[string]string{}, "service-account", "maclaw-tenant")
	if emptyUser != "alice" || emptyTenant != "tenant-a" {
		t.Fatalf("empty settings moved the desktop to %s/%s", emptyTenant, emptyUser)
	}
	movedUser, movedTenant := rememberDesktopOwner("inst_duty", map[string]string{"hub_user_id": "bob", "hub_tenant_id": "tenant-b"}, "service-account", "maclaw-tenant")
	if movedUser != "bob" || movedTenant != "tenant-b" {
		t.Fatalf("rebind = %s/%s", movedTenant, movedUser)
	}
	freshUser, freshTenant := rememberDesktopOwner("inst_new", nil, "service-account", "maclaw-tenant")
	if freshUser != "service-account" || freshTenant != "maclaw-tenant" {
		t.Fatalf("unbound missed read = %s/%s", freshTenant, freshUser)
	}
	if _, stored := desktopUserByInstance.Load("inst_new"); stored {
		t.Fatal("a missed read stored the service account as this bot's desktop")
	}
	unboundUser, unboundTenant := rememberDesktopOwner("inst_unbound", map[string]string{"hub_bot": "1"}, "service-account", "maclaw-tenant")
	t.Cleanup(func() { desktopUserByInstance.Delete("inst_unbound") })
	if unboundUser != "" || unboundTenant != "" {
		t.Fatalf("unbound hub bot opened %s/%s", unboundTenant, unboundUser)
	}
	if _, stored := desktopUserByInstance.Load("inst_unbound"); stored {
		t.Fatal("an unbound hub bot stored the service account")
	}
}

func TestDesktopStopsWhenTheUsersLastRunFinishes(t *testing.T) {
	var started, stopped int
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		started++
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
	})
	releaseFirst := occupyUserDesktop(context.Background(), "tenant", "alice", "")
	releaseSecond := occupyUserDesktop(context.Background(), "tenant", "alice", "")
	releaseFirst()
	if stopped != 0 {
		t.Fatalf("stopped while another run is open: %d", stopped)
	}
	releaseSecond()
	if started != 2 || stopped != 1 {
		t.Fatalf("started=%d stopped=%d", started, stopped)
	}
}

func TestNextTaskWaitsUntilTheLoginIsWritten(t *testing.T) {
	var mu sync.Mutex
	var order []string
	stopEntered := make(chan struct{})
	releaseStop := make(chan struct{})
	var once sync.Once
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		mu.Lock()
		order = append(order, "start")
		mu.Unlock()
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		mu.Lock()
		order = append(order, "stop-begin")
		mu.Unlock()
		once.Do(func() { close(stopEntered) })
		<-releaseStop
		mu.Lock()
		order = append(order, "stop-end")
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		select {
		case <-releaseStop:
		default:
			close(releaseStop)
		}
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
	})
	release := occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")
	go release()
	select {
	case <-stopEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the desktop did not stop")
	}
	done := make(chan struct{})
	go func() {
		occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")()
		close(done)
	}()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	during := append([]string(nil), order...)
	mu.Unlock()
	if len(during) != 2 || during[0] != "start" || during[1] != "stop-begin" {
		t.Fatalf("the next task opened the desktop while the login was still being written: %v", during)
	}
	close(releaseStop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the next task did not continue after the login was written")
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	want := []string{"start", "stop-begin", "stop-end", "start", "stop-begin", "stop-end"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("desktop order = %v", got)
	}
}

func TestDesktopStaysUpWhileTheUserLogsIn(t *testing.T) {
	var stopped int
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
		desktopHandoff.Delete(desktopRunKey("tenant", "alice"))
		desktopPersonInstance.Delete(desktopRunKey("tenant", "alice"))
	})
	release := occupyUserDesktop(context.Background(), "tenant", "alice", "")
	noteDesktopHandoff("tenant", "alice", "")
	if !takeDesktopHandoff("tenant", "alice", "") {
		t.Fatal("handoff was not reported")
	}
	release()
	if stopped != 0 {
		t.Fatalf("desktop stopped during login: %d", stopped)
	}
	occupyUserDesktop(context.Background(), "tenant", "alice", "")()
	if stopped != 1 {
		t.Fatalf("desktop stayed up after the follow-up run: %d", stopped)
	}
	stopped = 0
	desktopRunsMu.Lock()
	desktopHolds = map[string]int{}
	desktopRunsMu.Unlock()
	desktopPersonInstance.Delete(desktopRunKey("tenant", "alice"))
	releaseA := occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")
	noteDesktopHandoff("tenant", "alice", "bot-a")
	_, handoffErr := operateDesktop(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-b"}, map[string]any{"action": "probe"})
	if handoffErr == nil || !strings.Contains(handoffErr.Error(), "登录") {
		t.Fatalf("another bot drove the desktop while it was being handed over: %v", handoffErr)
	}
	releaseA()
	releaseB := occupyUserDesktop(context.Background(), "tenant", "alice", "bot-b")
	releaseB()
	if stopped != 0 {
		t.Fatalf("another bot stopped the desktop during login: %d", stopped)
	}
	occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")()
	if stopped != 1 {
		t.Fatalf("the bot that handed off did not stop the desktop after continuing: %d", stopped)
	}
}

func TestOtherBotStopsAfterTheLoginIsDecided(t *testing.T) {
	var calls int
	var mu sync.Mutex
	started := make(chan struct{})
	wait := make(chan struct{})
	var releaseWait sync.Once
	openWait := func() { releaseWait.Do(func() { close(wait) }) }
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > 1 {
			close(started)
			<-wait
		}
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() {
		openWait()
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
		desktopPersonInstance.Delete(desktopRunKey("tenant", "alice"))
		desktopHandoff.Delete(desktopHandoffKey("tenant", "alice", "bot-a"))
	})
	release := occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")
	defer release()
	errCh := make(chan error, 1)
	go func() {
		_, err := operateDesktop(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-b"}, map[string]any{"action": "probe"})
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the other bot did not reach the browser")
	}
	noteDesktopHandoff("tenant", "alice", "bot-a")
	openWait()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "登录") {
			t.Fatalf("another bot drove the desktop after the login was decided: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the other bot kept going after the login was decided")
	}
}

func TestScheduledRunLeavesTheDesktopWhileThePersonLogsIn(t *testing.T) {
	var stopped int
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
		desktopPersonInstance.Delete(desktopRunKey("tenant", "alice"))
		desktopUnattendedInstances.Delete("bot-a")
	})
	release := occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")
	noteDesktopHandoff("tenant", "alice", "bot-a")
	release()
	end := markDesktopUnattended("bot-a")
	_, err := operateDesktop(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-a"}, map[string]any{"action": "probe"})
	if err == nil || !strings.Contains(err.Error(), "登录") {
		t.Fatalf("scheduled run drove the desktop during login: %v", err)
	}
	occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")()
	end()
	if stopped != 0 {
		t.Fatalf("scheduled run stopped the desktop during login: %d", stopped)
	}
	occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")()
	if stopped != 1 {
		t.Fatalf("the follow-up did not stop the desktop: %d", stopped)
	}
}

func TestCallerDisconnectLeavesTheLoggedInDesktopUp(t *testing.T) {
	var stopped int
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-a"}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
		desktopResumeFocus.Delete(desktopResumeKey(scope))
		desktopResumeUser.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		desktopLoginDocument.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		desktopPersonInstance.Delete(desktopRunKey(scope.TenantID, scope.UserID))
	})
	noteDesktopResume(scope)
	if !consumeDesktopResume(scope) || !desktopLoginDocumentHeld(scope.TenantID, scope.UserID) {
		t.Fatal("the logged-in page was not waiting for the continuation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	release := occupyUserDesktop(ctx, "tenant", "alice", "bot-a")
	cancel()
	release()
	if stopped != 0 {
		t.Fatalf("a disconnected caller stopped the logged-in desktop: %d", stopped)
	}
	if !desktopLoginDocumentHeld(scope.TenantID, scope.UserID) {
		t.Fatal("a disconnected caller left the logged-in page")
	}
	occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")()
	if stopped != 1 {
		t.Fatalf("the next command left the desktop up: %d", stopped)
	}

	end := markDesktopUnattended("sched")
	ctx, cancel = context.WithCancel(context.Background())
	release = occupyUserDesktop(ctx, "tenant", "alice", "sched")
	cancel()
	release()
	end()
	if stopped != 2 {
		t.Fatalf("a cancelled scheduled run left the desktop up: %d", stopped)
	}
}

func TestHubKeepsTheLoggedInPageWhenTheDesktopStays(t *testing.T) {
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		return errDesktopKept
	}
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-a"}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
		desktopResumeFocus.Delete(desktopResumeKey(scope))
		desktopResumeUser.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		desktopLoginDocument.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		desktopPersonInstance.Delete(desktopRunKey(scope.TenantID, scope.UserID))
	})
	noteDesktopResume(scope)
	if !consumeDesktopResume(scope) || !desktopLoginDocumentHeld(scope.TenantID, scope.UserID) {
		t.Fatal("the logged-in page was not waiting")
	}
	occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")()
	if !desktopLoginDocumentHeld(scope.TenantID, scope.UserID) {
		t.Fatal("the page lock dropped while the desktop stayed up")
	}
	desktopRemoteStop = func(context.Context, string, string) error { return nil }
	occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")()
	if desktopLoginDocumentHeld(scope.TenantID, scope.UserID) {
		t.Fatal("a finished command kept the old page lock")
	}
}

func TestFailedDesktopStartStopsWhenNothingElseIsRunning(t *testing.T) {
	var stopped int
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{}, fmt.Errorf("desktop service rejected the request")
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
	})
	release := occupyUserDesktop(context.Background(), "tenant", "alice", "")
	if stopped != 0 {
		t.Fatalf("stopped while the run was still open: %d", stopped)
	}
	release()
	if stopped != 1 {
		t.Fatalf("stopped=%d, want the unused desktop stopped", stopped)
	}
}

func TestFailedDesktopStartLeavesAnOpenRun(t *testing.T) {
	var calls, stopped int
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		calls++
		if calls == 1 {
			return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
		}
		return desktopEndpoint{}, fmt.Errorf("desktop service rejected the request")
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
	})
	release := occupyUserDesktop(context.Background(), "tenant", "alice", "")
	failed := occupyUserDesktop(context.Background(), "tenant", "alice", "")
	failed()
	if stopped != 0 {
		t.Fatalf("stopped an open run: %d", stopped)
	}
	release()
	if stopped != 1 {
		t.Fatalf("stopped=%d after the open run finished", stopped)
	}
}

func TestHandoffTellsHubBeforeTheReplyReturns(t *testing.T) {
	var got string
	previous := desktopRemoteHold
	desktopRemoteHold = func(_ context.Context, tenantID, userID, instanceID string) error {
		got = tenantID + "/" + userID + "/" + instanceID
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteHold = previous
		desktopHandoff.Delete(desktopHandoffKey("tenant-a", "alice", "inst_alice"))
		desktopHandoff.Delete(desktopHandoffKey("tenant-a", "alice", ""))
		desktopHandoff.Delete(desktopHandoffKey("tenant-a", "alice", "sched"))
		end := markDesktopUnattended("sched")
		end()
		desktopRunsMu.Lock()
		delete(desktopHolds, desktopRunKey("tenant-a", "alice"))
		desktopRunsMu.Unlock()
	})
	noteDesktopHandoff("tenant-a", "alice", "inst_alice")
	if got != "tenant-a/alice/inst_alice" {
		t.Fatalf("hub was not told about the login: %q", got)
	}
	got = ""
	noteDesktopHandoff("tenant-a", "alice", "")
	if got != "" {
		t.Fatal("a handoff without an instance told hub")
	}
	got = ""
	end := markDesktopUnattended("sched")
	defer end()
	noteDesktopHandoff("tenant-a", "alice", "sched")
	if got != "" {
		t.Fatal("a scheduled task told hub to hold the desktop")
	}
}

func TestDesktopStopNamesTheInstanceThatFinished(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"kept"}`))
	}))
	defer srv.Close()
	client := &desktopHubClient{baseURL: srv.URL, token: "t", http: srv.Client()}
	ctx := context.WithValue(context.Background(), desktopStopInstanceKey{}, "inst_b")
	if err := client.Stop(ctx, "tenant-a", "alice"); err != errDesktopKept {
		t.Fatalf("stop err=%v", err)
	}
	if !strings.Contains(body, `"instance_id":"inst_b"`) || !strings.Contains(body, `"user_id":"alice"`) {
		t.Fatalf("stop body %s", body)
	}
}

// A repeated admission occupies again and can be the release that reaches
// zero. That release has to apply the owning run's choice. Its own hold
// baseline would clear a login the owner just handed off.
func TestReplayOccupyKeepsTheOwningRunsDesktopChoice(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		name := "stops when the owner did not hand off"
		if handoff {
			name = "keeps a handed-off desktop"
		}
		t.Run(name, func(t *testing.T) {
			var sessionCalls int
			replayEntered := make(chan struct{})
			releaseReplayOpen := make(chan struct{})
			var openOnce sync.Once
			var stopped int
			previousSession := desktopRemoteSession
			previousStop := desktopRemoteStop
			previousHold := desktopRemoteHold
			desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
				sessionCalls++
				if sessionCalls == 2 {
					openOnce.Do(func() { close(replayEntered) })
					<-releaseReplayOpen
				}
				return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
			}
			desktopRemoteStop = func(context.Context, string, string) error {
				stopped++
				return nil
			}
			desktopRemoteHold = func(context.Context, string, string, string) error { return nil }
			key := desktopRunKey("tenant", "replay-owner")
			handoffKey := desktopHandoffKey("tenant", "replay-owner", "bot-a")
			t.Cleanup(func() {
				select {
				case <-releaseReplayOpen:
				default:
					close(releaseReplayOpen)
				}
				desktopRemoteSession = previousSession
				desktopRemoteStop = previousStop
				desktopRemoteHold = previousHold
				desktopRunsMu.Lock()
				delete(desktopRuns, key)
				delete(desktopHolds, key)
				delete(desktopReleasePlans, key)
				desktopRunsMu.Unlock()
				desktopHandoff.Delete(handoffKey)
				desktopPersonInstance.Delete(key)
			})

			ownerRelease, _ := occupyUserDesktopRun(context.Background(), "tenant", "replay-owner", "bot-a")
			if handoff {
				noteDesktopHandoff("tenant", "replay-owner", "bot-a")
			}
			replayDone := make(chan struct{})
			var replayRelease func()
			var replayOwned *bool
			go func() {
				replayRelease, replayOwned = occupyUserDesktopRun(context.Background(), "tenant", "replay-owner", "bot-a")
				close(replayDone)
			}()
			select {
			case <-replayEntered:
			case <-time.After(2 * time.Second):
				t.Fatal("replay occupy did not reach the desktop open")
			}
			ownerRelease()
			if stopped != 0 {
				t.Fatalf("owner release stopped the desktop while the replay was still open: %d", stopped)
			}
			close(releaseReplayOpen)
			select {
			case <-replayDone:
			case <-time.After(2 * time.Second):
				t.Fatal("replay occupy did not return")
			}
			if replayOwned != nil {
				*replayOwned = false
			}
			replayRelease()
			desktopRunsMu.Lock()
			left := desktopRuns[key]
			holds := desktopHolds[key]
			desktopRunsMu.Unlock()
			_, person := desktopPersonInstance.Load(key)
			if handoff {
				if stopped != 0 || left != 0 || holds < 1 || !person {
					t.Fatalf("stopped=%d left=%d holds=%d person=%v", stopped, left, holds, person)
				}
				return
			}
			if stopped != 1 || left != 0 || holds != 0 {
				t.Fatalf("stopped=%d left=%d holds=%d", stopped, left, holds)
			}
		})
	}
}

// The owner's release deletes its saved plan once the count hits zero.
// A repeated admission that opens after that still must not clear a login
// the owner kept. After a normal stop, the extra open is closed.
func TestReplayOccupyAfterTheOwnerFinished(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		name := "closes the extra open after a normal stop"
		if handoff {
			name = "keeps a handed-off desktop"
		}
		t.Run(name, func(t *testing.T) {
			var stopped int
			previousSession := desktopRemoteSession
			previousStop := desktopRemoteStop
			previousHold := desktopRemoteHold
			desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
				return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
			}
			desktopRemoteStop = func(context.Context, string, string) error {
				stopped++
				return nil
			}
			desktopRemoteHold = func(context.Context, string, string, string) error { return nil }
			key := desktopRunKey("tenant", "replay-late")
			handoffKey := desktopHandoffKey("tenant", "replay-late", "bot-a")
			scope := agentruntime.Scope{TenantID: "tenant", UserID: "replay-late", InstanceID: "bot-a"}
			t.Cleanup(func() {
				desktopRemoteSession = previousSession
				desktopRemoteStop = previousStop
				desktopRemoteHold = previousHold
				desktopRunsMu.Lock()
				delete(desktopRuns, key)
				delete(desktopHolds, key)
				delete(desktopReleasePlans, key)
				desktopRunsMu.Unlock()
				desktopHandoff.Delete(handoffKey)
				desktopPersonInstance.Delete(key)
				desktopResumeUser.Delete(key)
				desktopLoginDocument.Delete(key)
				desktopResumeFocus.Delete(desktopResumeKey(scope))
			})

			ownerRelease, ownerOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-late", "bot-a")
			if ownerOwned != nil {
				*ownerOwned = true
			}
			if handoff {
				noteDesktopHandoff("tenant", "replay-late", "bot-a")
				noteDesktopResume(scope)
				// The owning finish takes the handoff flag before it releases.
				// The keep decision has to stand without that flag.
				takeDesktopHandoff("tenant", "replay-late", "bot-a")
			}
			ownerRelease()
			if handoff {
				if stopped != 0 {
					t.Fatalf("owner stop on handoff: %d", stopped)
				}
			} else if stopped != 1 {
				t.Fatalf("owner stop: %d", stopped)
			}

			replayRelease, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-late", "bot-a")
			if replayOwned != nil {
				*replayOwned = false
			}
			replayRelease()

			desktopRunsMu.Lock()
			left := desktopRuns[key]
			holds := desktopHolds[key]
			desktopRunsMu.Unlock()
			_, person := desktopPersonInstance.Load(key)
			if handoff {
				if stopped != 0 || left != 0 || holds < 1 || !person || !desktopLoginDocumentHeld("tenant", "replay-late") {
					t.Fatalf("stopped=%d left=%d holds=%d person=%v document=%v", stopped, left, holds, person, desktopLoginDocumentHeld("tenant", "replay-late"))
				}
				return
			}
			if stopped != 2 || left != 0 || holds != 0 || person {
				t.Fatalf("stopped=%d left=%d holds=%d person=%v", stopped, left, holds, person)
			}
		})
	}
}

// A handoff with no recorded person leaves the hold count after the
// follow-up stops the desktop. The next repeated admission must still
// close the session it opened.
func TestReplayAfterFollowUpClosesTheExtraOpen(t *testing.T) {
	var stopped int
	previousSession := desktopRemoteSession
	previousStop := desktopRemoteStop
	previousHold := desktopRemoteHold
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	desktopRemoteHold = func(context.Context, string, string, string) error { return nil }
	key := desktopRunKey("tenant", "replay-follow")
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteStop = previousStop
		desktopRemoteHold = previousHold
		desktopRunsMu.Lock()
		delete(desktopRuns, key)
		delete(desktopHolds, key)
		delete(desktopReleasePlans, key)
		desktopRunsMu.Unlock()
		desktopHandoff.Delete(desktopHandoffKey("tenant", "replay-follow", ""))
		desktopPersonInstance.Delete(key)
	})

	ownerRelease, _ := occupyUserDesktopRun(context.Background(), "tenant", "replay-follow", "")
	noteDesktopHandoff("tenant", "replay-follow", "")
	ownerRelease()
	if stopped != 0 {
		t.Fatalf("handoff stop: %d", stopped)
	}
	followRelease, followOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-follow", "")
	if followOwned != nil {
		*followOwned = true
	}
	followRelease()
	desktopRunsMu.Lock()
	holds := desktopHolds[key]
	desktopRunsMu.Unlock()
	if stopped != 1 || holds != 0 {
		t.Fatalf("follow-up stopped=%d holds=%d", stopped, holds)
	}

	replayRelease, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-follow", "")
	if replayOwned != nil {
		*replayOwned = false
	}
	replayRelease()
	desktopRunsMu.Lock()
	left := desktopRuns[key]
	holds = desktopHolds[key]
	desktopRunsMu.Unlock()
	if stopped != 2 || left != 0 || holds != 0 {
		t.Fatalf("stopped=%d left=%d holds=%d", stopped, left, holds)
	}
}

// Hub can refuse the stop because the person is still on the desktop.
// The handoff has to stay, and a repeated admission must not shut it.
func TestKeptStopStillProtectsTheHandoff(t *testing.T) {
	var stops int
	previousSession := desktopRemoteSession
	previousStop := desktopRemoteStop
	previousHold := desktopRemoteHold
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stops++
		return errDesktopKept
	}
	desktopRemoteHold = func(context.Context, string, string, string) error { return nil }
	key := desktopRunKey("tenant", "replay-kept")
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteStop = previousStop
		desktopRemoteHold = previousHold
		desktopRunsMu.Lock()
		delete(desktopRuns, key)
		delete(desktopHolds, key)
		delete(desktopReleasePlans, key)
		desktopRunsMu.Unlock()
		desktopPersonInstance.Delete(key)
		desktopHandoff.Delete(desktopHandoffKey("tenant", "replay-kept", "bot-a"))
	})

	ownerRelease, _ := occupyUserDesktopRun(context.Background(), "tenant", "replay-kept", "bot-a")
	noteDesktopHandoff("tenant", "replay-kept", "bot-a")
	ownerRelease()
	followRelease, followOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-kept", "bot-a")
	if followOwned != nil {
		*followOwned = true
	}
	followRelease()
	desktopRunsMu.Lock()
	holds := desktopHolds[key]
	desktopRunsMu.Unlock()
	_, person := desktopPersonInstance.Load(key)
	if stops != 1 || holds < 1 || !person {
		t.Fatalf("stops=%d holds=%d person=%v", stops, holds, person)
	}
	replayRelease, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-kept", "bot-a")
	if replayOwned != nil {
		*replayOwned = false
	}
	replayRelease()
	if stops != 1 {
		t.Fatalf("replay stop calls=%d", stops)
	}
}

// The continuation's caller left before the reply. This release does not
// stop, and a later repeated admission must not stop either.
func TestDisconnectDuringContinuationKeepsTheHandoff(t *testing.T) {
	var stops int
	previousSession := desktopRemoteSession
	previousStop := desktopRemoteStop
	previousHold := desktopRemoteHold
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		stops++
		return nil
	}
	desktopRemoteHold = func(context.Context, string, string, string) error { return nil }
	key := desktopRunKey("tenant", "replay-disconnect")
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteStop = previousStop
		desktopRemoteHold = previousHold
		desktopRunsMu.Lock()
		delete(desktopRuns, key)
		delete(desktopHolds, key)
		delete(desktopReleasePlans, key)
		desktopRunsMu.Unlock()
		desktopPersonInstance.Delete(key)
		desktopHandoff.Delete(desktopHandoffKey("tenant", "replay-disconnect", "bot-a"))
	})

	ownerRelease, _ := occupyUserDesktopRun(context.Background(), "tenant", "replay-disconnect", "bot-a")
	noteDesktopHandoff("tenant", "replay-disconnect", "bot-a")
	ownerRelease()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	followRelease, followOwned := occupyUserDesktopRun(ctx, "tenant", "replay-disconnect", "bot-a")
	if followOwned != nil {
		*followOwned = true
	}
	followRelease()
	desktopRunsMu.Lock()
	holds := desktopHolds[key]
	desktopRunsMu.Unlock()
	_, person := desktopPersonInstance.Load(key)
	if stops != 0 || holds < 1 || !person {
		t.Fatalf("stops=%d holds=%d person=%v", stops, holds, person)
	}
	replayRelease, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-disconnect", "bot-a")
	if replayOwned != nil {
		*replayOwned = false
	}
	replayRelease()
	if stops != 0 {
		t.Fatalf("replay stop calls=%d", stops)
	}
}

// The login continuation already decided to stop, then a newer open took
// the user gate. The old handoff must not keep that open's desktop. A
// handoff the newer open records still does. Hub keeping the desktop and
// a disconnected caller are covered by the tests above.
func TestBusyOpenRetiresTheFinishedLogin(t *testing.T) {
	cases := []struct {
		name         string
		user         string
		during       bool
		laterHandoff bool
	}{
		{name: "replay closes the extra open", user: "replay-busy-stop", during: false, laterHandoff: false},
		{name: "handoff during the open stays", user: "replay-busy-during", during: true, laterHandoff: false},
		{name: "later handoff stays", user: "replay-busy-later", during: false, laterHandoff: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stopped int
			previousSession := desktopRemoteSession
			previousStop := desktopRemoteStop
			previousHold := desktopRemoteHold
			desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
				return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
			}
			desktopRemoteStop = func(context.Context, string, string) error {
				stopped++
				return nil
			}
			desktopRemoteHold = func(context.Context, string, string, string) error { return nil }
			key := desktopRunKey("tenant", tc.user)
			gate := desktopUserGate(key)
			gateHeld := false
			t.Cleanup(func() {
				if gateHeld {
					gate.Unlock()
				}
				desktopRemoteSession = previousSession
				desktopRemoteStop = previousStop
				desktopRemoteHold = previousHold
				desktopRunsMu.Lock()
				delete(desktopRuns, key)
				delete(desktopHolds, key)
				delete(desktopSpentHolds, key)
				delete(desktopRetiredLogins, key)
				delete(desktopReleasePlans, key)
				desktopRunsMu.Unlock()
				desktopPersonInstance.Delete(key)
				desktopLoginDocument.Delete(key)
				desktopHandoff.Delete(desktopHandoffKey("tenant", tc.user, "bot-a"))
				desktopHandoff.Delete(desktopHandoffKey("tenant", tc.user, "bot-b"))
			})

			ownerRelease, _ := occupyUserDesktopRun(context.Background(), "tenant", tc.user, "bot-a")
			noteDesktopHandoff("tenant", tc.user, "bot-a")
			ownerRelease()
			if stopped != 0 {
				t.Fatalf("owner stop: %d", stopped)
			}
			desktopLoginDocument.Store(key, true)

			followRelease, followOwned := occupyUserDesktopRun(context.Background(), "tenant", tc.user, "bot-a")
			if followOwned != nil {
				*followOwned = true
			}
			gate.Lock()
			gateHeld = true
			released := make(chan struct{})
			go func() {
				followRelease()
				close(released)
			}()
			deadline := time.Now().Add(2 * time.Second)
			for {
				desktopRunsMu.Lock()
				left := desktopRuns[key]
				desktopRunsMu.Unlock()
				if left == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("release did not reach the gate")
				}
				time.Sleep(5 * time.Millisecond)
			}
			desktopRunsMu.Lock()
			desktopRuns[key] = 1
			desktopRunsMu.Unlock()
			if tc.during {
				noteDesktopHandoff("tenant", tc.user, "bot-b")
			}
			gate.Unlock()
			gateHeld = false
			select {
			case <-released:
			case <-time.After(2 * time.Second):
				t.Fatal("release did not finish")
			}
			if stopped != 0 {
				t.Fatalf("stop while a newer open holds the desktop: %d", stopped)
			}
			if !desktopLoginDocumentHeld("tenant", tc.user) {
				t.Fatal("page lock dropped while the newer open was running")
			}
			desktopRunsMu.Lock()
			holds := desktopHolds[key]
			spent := desktopSpentHolds[key]
			desktopRuns[key] = 0
			desktopRunsMu.Unlock()
			_, person := desktopPersonInstance.Load(key)
			if tc.during {
				if holds != 2 || spent != 0 || !person {
					t.Fatalf("holds=%d spent=%d person=%v", holds, spent, person)
				}
			} else if holds != 1 || spent != 1 || person {
				t.Fatalf("holds=%d spent=%d person=%v", holds, spent, person)
			}

			if tc.laterHandoff {
				laterRelease, laterOwned := occupyUserDesktopRun(context.Background(), "tenant", tc.user, "bot-b")
				if laterOwned != nil {
					*laterOwned = true
				}
				noteDesktopHandoff("tenant", tc.user, "bot-b")
				laterRelease()
				desktopRunsMu.Lock()
				holds = desktopHolds[key]
				spent = desktopSpentHolds[key]
				desktopRunsMu.Unlock()
				personValue, personOK := desktopPersonInstance.Load(key)
				personID, _ := personValue.(string)
				if stopped != 0 || holds < 2 || spent != 1 || !personOK || personID != "bot-b" {
					t.Fatalf("stopped=%d holds=%d spent=%d person=%v id=%q", stopped, holds, spent, personOK, personID)
				}
			}

			replayRelease, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", tc.user, "bot-a")
			if replayOwned != nil {
				*replayOwned = false
			}
			replayRelease()
			desktopRunsMu.Lock()
			left := desktopRuns[key]
			holds = desktopHolds[key]
			spent = desktopSpentHolds[key]
			desktopRunsMu.Unlock()
			_, person = desktopPersonInstance.Load(key)
			if tc.during || tc.laterHandoff {
				if stopped != 0 || left != 0 || holds < 1 || spent > holds || !person {
					t.Fatalf("stopped=%d left=%d holds=%d spent=%d person=%v", stopped, left, holds, spent, person)
				}
				return
			}
			if stopped != 1 || left != 0 || holds != 0 || spent != 0 || person {
				t.Fatalf("stopped=%d left=%d holds=%d spent=%d person=%v", stopped, left, holds, spent, person)
			}
		})
	}
}

// occupyUntilLoginIsSetAside runs the login continuation until it has set
// the finished login aside for an open that holds the user gate. The stop
// hook is what later releases call.
func occupyUntilLoginIsSetAside(t *testing.T, user string) (stops *int, stopErr *error) {
	t.Helper()
	var n int
	var err error
	previousSession := desktopRemoteSession
	previousStop := desktopRemoteStop
	previousHold := desktopRemoteHold
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error {
		n++
		return err
	}
	desktopRemoteHold = func(context.Context, string, string, string) error { return nil }
	key := desktopRunKey("tenant", user)
	gate := desktopUserGate(key)
	gateHeld := false
	t.Cleanup(func() {
		if gateHeld {
			gate.Unlock()
		}
		desktopRemoteSession = previousSession
		desktopRemoteStop = previousStop
		desktopRemoteHold = previousHold
		desktopRunsMu.Lock()
		delete(desktopRuns, key)
		delete(desktopHolds, key)
		delete(desktopSpentHolds, key)
		delete(desktopRetiredLogins, key)
		delete(desktopReleasePlans, key)
		desktopRunsMu.Unlock()
		desktopPersonInstance.Delete(key)
		desktopLoginDocument.Delete(key)
		desktopHandoff.Delete(desktopHandoffKey("tenant", user, "bot-a"))
		desktopHandoff.Delete(desktopHandoffKey("tenant", user, "bot-b"))
	})

	ownerRelease, _ := occupyUserDesktopRun(context.Background(), "tenant", user, "bot-a")
	noteDesktopHandoff("tenant", user, "bot-a")
	ownerRelease()
	if n != 0 {
		t.Fatalf("owner stop: %d", n)
	}
	desktopLoginDocument.Store(key, true)
	followRelease, followOwned := occupyUserDesktopRun(context.Background(), "tenant", user, "bot-a")
	if followOwned != nil {
		*followOwned = true
	}
	gate.Lock()
	gateHeld = true
	released := make(chan struct{})
	go func() {
		followRelease()
		close(released)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		desktopRunsMu.Lock()
		left := desktopRuns[key]
		desktopRunsMu.Unlock()
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("release did not reach the gate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	desktopRunsMu.Lock()
	desktopRuns[key] = 1
	desktopRunsMu.Unlock()
	gate.Unlock()
	gateHeld = false
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("release did not finish")
	}
	desktopRunsMu.Lock()
	holds := desktopHolds[key]
	spent := desktopSpentHolds[key]
	desktopRuns[key] = 0
	desktopRunsMu.Unlock()
	_, person := desktopPersonInstance.Load(key)
	if n != 0 || holds != 1 || spent != 1 || person || !desktopLoginDocumentHeld("tenant", user) {
		t.Fatalf("stops=%d holds=%d spent=%d person=%v document=%v", n, holds, spent, person, desktopLoginDocumentHeld("tenant", user))
	}
	return &n, &err
}

// The open that took the gate tries to stop and Hub keeps the desktop, or
// that open's caller has already disconnected. The finished login comes
// back, and a later repeated admission does not shut the desktop.
func TestBusyStopRestoresTheLoginWhenTheStopIsNotAccepted(t *testing.T) {
	t.Run("hub keeps the desktop", func(t *testing.T) {
		stops, stopErr := occupyUntilLoginIsSetAside(t, "replay-busy-kept")
		*stopErr = errDesktopKept
		release, owned := occupyUserDesktopRun(context.Background(), "tenant", "replay-busy-kept", "bot-b")
		if owned != nil {
			*owned = true
		}
		release()
		desktopRunsMu.Lock()
		key := desktopRunKey("tenant", "replay-busy-kept")
		holds := desktopHolds[key]
		spent := desktopSpentHolds[key]
		desktopRunsMu.Unlock()
		person, _ := desktopPersonInstance.Load(key)
		personID, _ := person.(string)
		if *stops != 1 || holds != 1 || spent != 0 || personID != "bot-a" || !desktopLoginDocumentHeld("tenant", "replay-busy-kept") {
			t.Fatalf("stops=%d holds=%d spent=%d person=%q document=%v", *stops, holds, spent, personID, desktopLoginDocumentHeld("tenant", "replay-busy-kept"))
		}
		replay, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-busy-kept", "bot-a")
		if replayOwned != nil {
			*replayOwned = false
		}
		replay()
		if *stops != 1 {
			t.Fatalf("replay stop calls=%d", *stops)
		}
	})

	t.Run("caller disconnected", func(t *testing.T) {
		stops, _ := occupyUntilLoginIsSetAside(t, "replay-busy-gone")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		release, owned := occupyUserDesktopRun(ctx, "tenant", "replay-busy-gone", "bot-b")
		if owned != nil {
			*owned = true
		}
		release()
		key := desktopRunKey("tenant", "replay-busy-gone")
		desktopRunsMu.Lock()
		holds := desktopHolds[key]
		spent := desktopSpentHolds[key]
		desktopRunsMu.Unlock()
		person, _ := desktopPersonInstance.Load(key)
		personID, _ := person.(string)
		if *stops != 0 || holds != 1 || spent != 0 || personID != "bot-a" || !desktopLoginDocumentHeld("tenant", "replay-busy-gone") {
			t.Fatalf("stops=%d holds=%d spent=%d person=%q document=%v", *stops, holds, spent, personID, desktopLoginDocumentHeld("tenant", "replay-busy-gone"))
		}
		replay, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-busy-gone", "bot-a")
		if replayOwned != nil {
			*replayOwned = false
		}
		replay()
		if *stops != 0 {
			t.Fatalf("replay stop calls=%d", *stops)
		}
	})

	t.Run("later handoff stays after a kept stop", func(t *testing.T) {
		stops, stopErr := occupyUntilLoginIsSetAside(t, "replay-busy-handed")
		release, owned := occupyUserDesktopRun(context.Background(), "tenant", "replay-busy-handed", "bot-b")
		if owned != nil {
			*owned = true
		}
		noteDesktopHandoff("tenant", "replay-busy-handed", "bot-b")
		release()
		if *stops != 0 {
			t.Fatalf("handoff stop: %d", *stops)
		}
		*stopErr = errDesktopKept
		follow, followOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-busy-handed", "bot-b")
		if followOwned != nil {
			*followOwned = true
		}
		follow()
		key := desktopRunKey("tenant", "replay-busy-handed")
		person, _ := desktopPersonInstance.Load(key)
		personID, _ := person.(string)
		if *stops != 1 || personID != "bot-b" {
			t.Fatalf("stops=%d person=%q", *stops, personID)
		}
		replay, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", "replay-busy-handed", "bot-a")
		if replayOwned != nil {
			*replayOwned = false
		}
		replay()
		if *stops != 1 {
			t.Fatalf("replay stop calls=%d", *stops)
		}
	})
}

// A second open can also find the desktop busy after the login was set
// aside. That pass no longer sees the person. The saved login has to
// stay, so a stop Hub does not accept still brings the person back.
func TestNestedBusyKeepsTheSetAsideLogin(t *testing.T) {
	user := "replay-busy-nested"
	stops, stopErr := occupyUntilLoginIsSetAside(t, user)
	key := desktopRunKey("tenant", user)
	gate := desktopUserGate(key)
	gateHeld := false
	t.Cleanup(func() {
		if gateHeld {
			gate.Unlock()
		}
	})
	release, owned := occupyUserDesktopRun(context.Background(), "tenant", user, "bot-a")
	if owned != nil {
		*owned = true
	}
	gate.Lock()
	gateHeld = true
	released := make(chan struct{})
	go func() {
		release()
		close(released)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		desktopRunsMu.Lock()
		left := desktopRuns[key]
		desktopRunsMu.Unlock()
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("release did not reach the gate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	desktopRunsMu.Lock()
	desktopRuns[key] = 1
	desktopRunsMu.Unlock()
	gate.Unlock()
	gateHeld = false
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("release did not finish")
	}
	desktopRunsMu.Lock()
	saved := desktopRetiredLogins[key]
	holds := desktopHolds[key]
	spent := desktopSpentHolds[key]
	desktopRuns[key] = 0
	desktopRunsMu.Unlock()
	if *stops != 0 || holds != 1 || spent != 1 || !saved.personHeld || saved.person != "bot-a" || saved.prevSpent != 0 {
		t.Fatalf("stops=%d holds=%d spent=%d saved=%+v", *stops, holds, spent, saved)
	}
	*stopErr = errDesktopKept
	later, laterOwned := occupyUserDesktopRun(context.Background(), "tenant", user, "bot-b")
	if laterOwned != nil {
		*laterOwned = true
	}
	later()
	desktopRunsMu.Lock()
	holds = desktopHolds[key]
	spent = desktopSpentHolds[key]
	desktopRunsMu.Unlock()
	person, _ := desktopPersonInstance.Load(key)
	personID, _ := person.(string)
	if *stops != 1 || holds != 1 || spent != 0 || personID != "bot-a" || !desktopLoginDocumentHeld("tenant", user) {
		t.Fatalf("stops=%d holds=%d spent=%d person=%q document=%v", *stops, holds, spent, personID, desktopLoginDocumentHeld("tenant", user))
	}
	replay, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", user, "bot-a")
	if replayOwned != nil {
		*replayOwned = false
	}
	replay()
	if *stops != 1 {
		t.Fatalf("replay stop calls=%d", *stops)
	}
}

// Deleting the bot after the login was set aside used to leave the saved
// person in place. A stop Hub did not accept put that bot back, and a
// later repeated admission would not shut the desktop.
func TestDeletedBotDropsTheSetAsideLogin(t *testing.T) {
	user := "replay-busy-deleted"
	stops, stopErr := occupyUntilLoginIsSetAside(t, user)
	key := desktopRunKey("tenant", user)
	releaseDesktopPerson("bot-a")
	desktopRunsMu.Lock()
	_, retired := desktopRetiredLogins[key]
	holds := desktopHolds[key]
	spent := desktopSpentHolds[key]
	left := desktopRuns[key]
	desktopRunsMu.Unlock()
	_, person := desktopPersonInstance.Load(key)
	if retired || person || holds != 1 || spent != 1 || left != 0 || *stops != 1 {
		t.Fatalf("retired=%v person=%v holds=%d spent=%d left=%d stops=%d", retired, person, holds, spent, left, *stops)
	}

	*stopErr = errDesktopKept
	release, owned := occupyUserDesktopRun(context.Background(), "tenant", user, "bot-b")
	if owned != nil {
		*owned = true
	}
	release()
	desktopRunsMu.Lock()
	holds = desktopHolds[key]
	spent = desktopSpentHolds[key]
	_, retired = desktopRetiredLogins[key]
	desktopRunsMu.Unlock()
	personValue, personOK := desktopPersonInstance.Load(key)
	personID, _ := personValue.(string)
	if personOK || retired || holds != 1 || spent != 1 || *stops != 2 {
		t.Fatalf("stops=%d holds=%d spent=%d retired=%v person=%v id=%q", *stops, holds, spent, retired, personOK, personID)
	}

	replay, replayOwned := occupyUserDesktopRun(context.Background(), "tenant", user, "bot-a")
	if replayOwned != nil {
		*replayOwned = false
	}
	replay()
	_, person = desktopPersonInstance.Load(key)
	if *stops != 3 || person {
		t.Fatalf("replay stops=%d person=%v", *stops, person)
	}
}

// A newer handoff already replaced the set-aside login. Deleting the old
// bot leaves that newer person on the desktop.
func TestDeletedBotLeavesTheNewerHandoff(t *testing.T) {
	user := "replay-busy-deleted-next"
	stops, _ := occupyUntilLoginIsSetAside(t, user)
	noteDesktopHandoff("tenant", user, "bot-b")
	releaseDesktopPerson("bot-a")
	key := desktopRunKey("tenant", user)
	personValue, personOK := desktopPersonInstance.Load(key)
	personID, _ := personValue.(string)
	desktopRunsMu.Lock()
	holds := desktopHolds[key]
	spent := desktopSpentHolds[key]
	_, retired := desktopRetiredLogins[key]
	desktopRunsMu.Unlock()
	if *stops != 0 || retired || !personOK || personID != "bot-b" || holds != 2 || spent != 1 {
		t.Fatalf("stops=%d retired=%v person=%v id=%q holds=%d spent=%d", *stops, retired, personOK, personID, holds, spent)
	}
}
