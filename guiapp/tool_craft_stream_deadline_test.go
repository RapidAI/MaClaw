package guiapp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestCraftScriptClientHasNoBodyTimeout(t *testing.T) {
	defer resetCraftScriptHTTPClientForTest()
	headerTimeout := 150 * time.Millisecond
	client := newCraftScriptHTTPClient(headerTimeout)
	if client.Timeout != 0 {
		t.Fatalf("Client.Timeout = %s, want 0", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("craft client transport is not an http.Transport")
	}
	if transport.ResponseHeaderTimeout != headerTimeout {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, headerTimeout)
	}
	if !transport.DisableCompression {
		t.Fatal("craft stream transport still compresses the body")
	}
	shared, _ := llm.SharedHTTPClient.Transport.(*http.Transport)
	if shared == nil {
		t.Fatal("shared LLM transport missing")
	}
	if shared == transport {
		t.Fatal("craft client mutated the shared transport")
	}
	if shared.ResponseHeaderTimeout != 0 {
		t.Fatalf("shared ResponseHeaderTimeout = %s, want 0", shared.ResponseHeaderTimeout)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: hi\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(headerTimeout + 200*time.Millisecond)
		_, _ = w.Write([]byte("data: done\n\n"))
	}))
	defer srv.Close()

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "done") {
		t.Fatalf("body = %q, want the bytes written after the old client timeout", body)
	}
}

func TestCraftScriptClientHeaderTimeoutStopsBeforeHeaders(t *testing.T) {
	defer resetCraftScriptHTTPClientForTest()
	headerTimeout := 80 * time.Millisecond
	client := newCraftScriptHTTPClient(headerTimeout)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	started := time.Now()
	resp, err := client.Get(srv.URL)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("header stall returned a response")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("header stall lasted %s, want the header timeout", elapsed)
	}
}

func TestCraftScriptClientReusesTheConnectionForTheSameHeaderBudget(t *testing.T) {
	resetCraftScriptHTTPClientForTest()
	defer resetCraftScriptHTTPClientForTest()

	var opened atomic.Int32
	closed := make(chan struct{}, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			opened.Add(1)
		case http.StateClosed:
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	srv.Start()
	defer srv.Close()

	first := newCraftScriptHTTPClient(time.Second)
	second := newCraftScriptHTTPClient(time.Second)
	if first != second {
		t.Fatal("same header budget created another craft client")
	}
	for i := 0; i < 2; i++ {
		resp, err := first.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("opened %d connections, want 1", got)
	}

	changed := newCraftScriptHTTPClient(2 * time.Second)
	if changed == first {
		t.Fatal("a new header budget kept the previous craft client")
	}
	if changed.Timeout != 0 {
		t.Fatalf("Client.Timeout = %s, want 0", changed.Timeout)
	}
	transport, ok := changed.Transport.(*http.Transport)
	if !ok {
		t.Fatal("craft client transport is not an http.Transport")
	}
	if transport.ResponseHeaderTimeout != 2*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 2s", transport.ResponseHeaderTimeout)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the previous header budget left its idle connection open")
	}
}

func TestScriptDeadlineExpiredIgnoresASoonerParent(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	select {
	case <-parent.Done():
	case <-time.After(time.Second):
		t.Fatal("parent deadline did not fire")
	}
	script, stop := context.WithTimeout(parent, time.Hour)
	defer stop()
	if scriptDeadlineExpired(parent, script, 3600) {
		t.Fatal("a sooner parent deadline was reported as the script timeout")
	}

	liveParent, cancelParent := context.WithTimeout(context.Background(), time.Hour)
	defer cancelParent()
	scriptTimer, cancelScript := context.WithTimeout(liveParent, 20*time.Millisecond)
	defer cancelScript()
	select {
	case <-scriptTimer.Done():
	case <-time.After(time.Second):
		t.Fatal("script timer did not fire")
	}
	if !scriptDeadlineExpired(liveParent, scriptTimer, 1) {
		t.Fatal("script timer was dropped because the parent was still live")
	}
	if liveParent.Err() != nil {
		t.Fatal("script timer canceled the parent")
	}
}

func TestExecuteScriptParentDeadlineIsNotTheScriptTimeout(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell script")
	}
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip(err.Error())
	}
	parent, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sleep.ps1")
	if err := os.WriteFile(path, []byte("Start-Sleep -Seconds 30\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = executeScriptWithContext(parent, path, "powershell", dir, 30, craftRuntimeAvailability{PowerShell: powershell}, nil, false)
	if err == nil {
		t.Fatal("script finished after the caller deadline")
	}
	if _, ok := err.(*craftScriptTimeoutError); ok {
		t.Fatal("caller deadline was reported as the script timeout")
	}
	if !strings.Contains(err.Error(), "调用方时限已到") {
		t.Fatalf("err = %v, want the caller deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("script kept running for %s after the caller deadline", elapsed)
	}
}

func TestCraftAPIRetryStopLeavesALiveContextAndKeepsASpentRateLimit(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	live, cancel := context.WithTimeout(parent, time.Hour)
	defer cancel()
	if err := craftAPIRetryStop(live, errors.New("HTTP 429")); err != nil {
		t.Fatalf("live context stopped the retry: %v", err)
	}

	expired, cancelExpired := context.WithTimeout(parent, 20*time.Millisecond)
	defer cancelExpired()
	deadline, _ := expired.Deadline()
	expired = llm.BindRouteStreamDeadline(expired, deadline, parent)
	select {
	case <-expired.Done():
	case <-time.After(time.Second):
		t.Fatal("route deadline did not fire")
	}
	rateErr := errors.New("HTTP 429")
	if err := craftAPIRetryStop(expired, rateErr); err != rateErr {
		t.Fatalf("spent route deadline returned %v", err)
	}

	stop()
	if err := craftAPIRetryStop(live, rateErr); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop returned %v, want the caller stop", err)
	}
}

func TestCraftRepairStopsWhenTheCallerContextEnded(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	live, cancelLive := context.WithTimeout(parent, time.Hour)
	defer cancelLive()
	deadline, _ := live.Deadline()
	live = llm.BindRouteStreamDeadline(live, deadline, parent)
	if !craftAnotherGenerationAllowed(live, 2) {
		t.Fatal("a live caller context blocked repair generation")
	}
	if !craftAnotherGenerationAllowed(live, 1) {
		t.Fatal("the first generation was blocked")
	}

	expired, cancelExpired := context.WithTimeout(parent, 20*time.Millisecond)
	defer cancelExpired()
	expiredDeadline, _ := expired.Deadline()
	expired = llm.BindRouteStreamDeadline(expired, expiredDeadline, parent)
	select {
	case <-expired.Done():
	case <-time.After(time.Second):
		t.Fatal("route deadline did not fire")
	}
	if craftAnotherGenerationAllowed(expired, 2) {
		t.Fatal("a spent route deadline still started a repair generation")
	}
	if !craftAnotherGenerationAllowed(expired, 1) {
		t.Fatal("the first generation was blocked after the route deadline")
	}

	host, cancelHost := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelHost()
	select {
	case <-host.Done():
	case <-time.After(time.Second):
		t.Fatal("host deadline did not fire")
	}
	if craftAnotherGenerationAllowed(host, 2) {
		t.Fatal("a spent host deadline still started a repair generation")
	}

	stop()
	if craftAnotherGenerationAllowed(live, 2) {
		t.Fatal("a caller stop still started a repair generation")
	}
}

func TestCraftAPIRetryKeepsTheRateLimitWhenTheRouteDeadlineFires(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	old := craftToolRetryInitialBackoff
	craftToolRetryInitialBackoff = time.Second
	t.Cleanup(func() { craftToolRetryInitialBackoff = old })

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	parent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := context.WithTimeout(parent, 200*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)

	started := time.Now()
	_, err := generateScriptWithContext(route, corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "test-model", TimeoutSec: 30,
	}, craftToolRequest{Task: "draw", RuntimeLanguage: "python"}, craftRuntimeAvailability{Python: "python"}, craftAttemptResult{}, srv.Client())
	if err == nil {
		t.Fatal("rate limit returned a script")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("error = %v, want the rate limit already in hand", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Fatalf("route deadline replaced the rate limit: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
	if elapsed := time.Since(started); elapsed > 800*time.Millisecond {
		t.Fatalf("retry waited %s for a budget that was already spent", elapsed)
	}
}

func TestCraftAPIRetryStopStillAbortsBeforeAnotherRequest(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	old := craftToolRetryInitialBackoff
	craftToolRetryInitialBackoff = time.Second
	t.Cleanup(func() { craftToolRetryInitialBackoff = old })

	started429 := make(chan struct{})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case <-started429:
		default:
			close(started429)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	parent, stop := context.WithCancel(context.Background())
	route, cancel := context.WithTimeout(parent, time.Hour)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)
	go func() {
		select {
		case <-started429:
		case <-time.After(2 * time.Second):
			return
		}
		time.Sleep(30 * time.Millisecond)
		stop()
	}()

	started := time.Now()
	_, err := generateScriptWithContext(route, corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "test-model", TimeoutSec: 30,
	}, craftToolRequest{Task: "draw", RuntimeLanguage: "python"}, craftRuntimeAvailability{Python: "python"}, craftAttemptResult{}, srv.Client())
	if err == nil {
		t.Fatal("caller stop returned a script")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "cancel") {
		t.Fatalf("error = %v, want the caller stop", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
	if elapsed := time.Since(started); elapsed > 800*time.Millisecond {
		t.Fatalf("stop waited %s", elapsed)
	}
}

func TestBindCraftScriptRouteDeadlineLeavesAShortHostDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bound := bindCraftScriptRouteDeadline(ctx)
	if _, owned := llm.OwnedRouteDeadline(bound); owned {
		t.Fatal("a host deadline under MinAgentTimeoutSec was adopted as a route deadline")
	}
	if bound != ctx {
		t.Fatal("short host deadline context was replaced")
	}
}

func TestBindCraftScriptRouteDeadlineDoesNotReplaceTheSkillRunnerParent(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := skillRunContext(parent, time.Hour)
	defer cancel()
	if got := llm.RouteStreamCancelParent(route); got != parent {
		t.Fatalf("skill run stop parent = %v, want the pre-timeout parent", got)
	}
	bound := bindCraftScriptRouteDeadline(route)
	if got := llm.RouteStreamCancelParent(bound); got != parent {
		t.Fatal("script generation replaced the skill runner stop parent")
	}
	stop()
	select {
	case <-route.Done():
	case <-time.After(time.Second):
		t.Fatal("user stop did not cancel the skill run")
	}
}

func TestBindCraftScriptRouteDeadlineLeavesTheCallerUnowned(t *testing.T) {
	caller, cancel := context.WithTimeout(context.Background(), time.Duration(corelib.MinAgentTimeoutSec+1)*time.Second)
	defer cancel()
	bound := bindCraftScriptRouteDeadline(caller)
	if bound == caller {
		t.Fatal("bind replaced the caller context in place")
	}
	if _, owned := llm.OwnedRouteDeadline(caller); owned {
		t.Fatal("bind wrote the route deadline onto the caller context")
	}
	if _, owned := llm.OwnedRouteDeadline(bound); !owned {
		t.Fatal("returned context has no route deadline")
	}
}

func TestChildBindDoesNotLiftTheCallerAfterTheRouteDeadline(t *testing.T) {
	caller, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	deadline, ok := caller.Deadline()
	if !ok {
		t.Fatal("caller has no deadline")
	}
	// generateScript used to keep this child locally. bindCraft itself refuses
	// a 30ms host cap, so the split is shown with the same Bind it would return.
	child := llm.BindRouteStreamDeadline(caller, deadline, llm.WithoutDeadlineCancel(caller))
	select {
	case <-caller.Done():
	case <-time.After(time.Second):
		t.Fatal("caller deadline did not fire")
	}
	if llm.OwnedRouteDeadlineReleased(caller) {
		t.Fatal("unowned caller was treated as an owned route deadline")
	}
	stuck, releaseCaller := llm.ContinueAfterOwnedRouteDeadline(caller)
	defer releaseCaller()
	if stuck != caller {
		t.Fatal("Continue lifted a context that does not own the route deadline")
	}
	if stuck.Err() == nil {
		t.Fatal("unlifted caller is still live after its deadline")
	}
	if !llm.OwnedRouteDeadlineReleased(child) {
		t.Fatal("bound child was not released by the route deadline")
	}
	live, release := llm.ContinueAfterOwnedRouteDeadline(child)
	defer release()
	if live.Err() != nil {
		t.Fatalf("bound context was not lifted: %v", live.Err())
	}
}

func TestBindCraftScriptRouteDeadlineAdoptsALongCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(corelib.MinAgentTimeoutSec+1)*time.Second)
	defer cancel()
	bound := bindCraftScriptRouteDeadline(ctx)
	deadline, owned := llm.OwnedRouteDeadline(bound)
	if !owned {
		t.Fatal("long caller deadline was not recorded")
	}
	got, ok := bound.Deadline()
	if !ok || !got.Equal(deadline) {
		t.Fatalf("recorded deadline = %s, context deadline = %s", deadline, got)
	}
	parent := llm.RouteStreamCancelParent(bound)
	if parent == nil || parent == bound {
		t.Fatal("long caller deadline stored itself as the only stop signal")
	}
	cancel()
	select {
	case <-parent.Done():
	case <-time.After(time.Second):
		t.Fatal("canceling the caller did not reach the stop signal")
	}
}

func TestSkillRunContextKeepsAnEarlierHostDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, stop := skillRunContext(parent, time.Hour)
	defer stop()
	if _, owned := llm.OwnedRouteDeadline(ctx); owned {
		t.Fatal("earlier host deadline was recorded as a route deadline")
	}
	got, ok := ctx.Deadline()
	parentDeadline, parentOK := parent.Deadline()
	if !ok || !parentOK || !got.Equal(parentDeadline) {
		t.Fatalf("child deadline = %s, parent deadline = %s", got, parentDeadline)
	}
}

func TestExecuteScriptRunsAfterTheOwnedRouteDeadline(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell script")
	}
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip(err.Error())
	}
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := context.WithTimeout(parent, 20*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)
	select {
	case <-route.Done():
	case <-time.After(time.Second):
		t.Fatal("route deadline did not fire")
	}
	scriptCtx, release := llm.ContinueAfterOwnedRouteDeadline(route)
	defer release()

	dir := t.TempDir()
	path := filepath.Join(dir, "ok.ps1")
	if err := os.WriteFile(path, []byte("Write-Output ok\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := executeScriptWithContext(scriptCtx, path, "powershell", dir, 30, craftRuntimeAvailability{PowerShell: powershell}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("output = %q", out)
	}
}

func TestExecuteScriptTimeoutStillBoundsTheLiftedContext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell script")
	}
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip(err.Error())
	}
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := context.WithTimeout(parent, 20*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)
	<-route.Done()
	scriptCtx, release := llm.ContinueAfterOwnedRouteDeadline(route)
	defer release()

	dir := t.TempDir()
	path := filepath.Join(dir, "sleep.ps1")
	if err := os.WriteFile(path, []byte("Start-Sleep -Seconds 30\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = executeScriptWithContext(scriptCtx, path, "powershell", dir, 1, craftRuntimeAvailability{PowerShell: powershell}, nil, false)
	if _, ok := err.(*craftScriptTimeoutError); !ok {
		t.Fatalf("err = %v, want the script timeout", err)
	}
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("script ended in %s, so the route deadline was still the context", elapsed)
	}
}

func TestExecuteScriptStopsWhenTheRouteParentStops(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell script")
	}
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip(err.Error())
	}
	parent, stop := context.WithCancel(context.Background())
	route, cancel := context.WithTimeout(parent, 20*time.Millisecond)
	defer cancel()
	deadline, _ := route.Deadline()
	route = llm.BindRouteStreamDeadline(route, deadline, parent)
	<-route.Done()
	scriptCtx, release := llm.ContinueAfterOwnedRouteDeadline(route)
	defer release()

	dir := t.TempDir()
	path := filepath.Join(dir, "sleep.ps1")
	if err := os.WriteFile(path, []byte("Start-Sleep -Seconds 30\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		stop()
	}()
	started := time.Now()
	_, err = executeScriptWithContext(scriptCtx, path, "powershell", dir, 30, craftRuntimeAvailability{PowerShell: powershell}, nil, false)
	if err == nil {
		t.Fatal("script finished after the user stop")
	}
	if _, ok := err.(*craftScriptTimeoutError); ok {
		t.Fatal("user stop was reported as the script timeout")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("script kept running for %s after the stop", elapsed)
	}
}
