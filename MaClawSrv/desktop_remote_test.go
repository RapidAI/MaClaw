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
	out, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_list", nil)
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
