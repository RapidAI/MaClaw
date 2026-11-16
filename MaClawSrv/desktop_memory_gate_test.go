package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryGateReadsDesktopdDirectly(t *testing.T) {
	restore := installMemoryGateTest(t)
	defer restore()

	var hubCalls atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubCalls.Add(1)
		http.NotFound(w, r)
	}))
	defer hub.Close()
	var gotPath string
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"used_bytes":90,"limit_bytes":100}`))
	}))
	defer direct.Close()
	t.Setenv(desktopdURLEnv, direct.URL)
	t.Setenv(desktopdTokenEnv, "desktopd-token")
	t.Setenv(desktopHubURLEnv, hub.URL)
	t.Setenv(desktopHubTokenEnv, "hub-token")

	pressed, line, err := readDesktopMemoryPressure(context.Background(), "tenant-a", "alice")
	if err != nil || !pressed || line != desktopMemoryWaitLine {
		t.Fatalf("pressed=%v line=%q err=%v", pressed, line, err)
	}
	if gotPath != desktopdMemoryPath {
		t.Fatalf("path %q", gotPath)
	}
	if hubCalls.Load() != 0 {
		t.Fatalf("hub calls %d", hubCalls.Load())
	}
	if got := memoryGateFields()["memory_gate"]; got != memoryGateOK {
		t.Fatalf("memory_gate %q", got)
	}
}

func TestMemoryGateFallsBackToHub(t *testing.T) {
	var events []memoryGateEvent
	restore := installMemoryGateTest(t)
	defer restore()
	memoryGateNotify = func(ev memoryGateEvent) { events = append(events, ev) }

	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer direct.Close()
	var hubCalls atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubCalls.Add(1)
		if r.URL.Path != "/api/v1/desktop-services/memory" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"used_bytes":90,"limit_bytes":100}`))
	}))
	defer hub.Close()
	t.Setenv(desktopdURLEnv, direct.URL)
	t.Setenv(desktopdTokenEnv, "desktopd-token")
	t.Setenv(desktopHubURLEnv, hub.URL)
	t.Setenv(desktopHubTokenEnv, "hub-token")

	pressed, _, err := readDesktopMemoryPressure(context.Background(), "tenant-a", "alice")
	if err != nil || !pressed {
		t.Fatalf("pressed=%v err=%v", pressed, err)
	}
	if hubCalls.Load() != 1 {
		t.Fatalf("hub calls %d", hubCalls.Load())
	}
	fields := memoryGateFields()
	if fields["memory_gate"] != memoryGateOK {
		t.Fatalf("fields %#v", fields)
	}
	if len(events) != 1 || events[0].Gate != memoryGateOK || events[0].Source != memorySourceDesktopd || events[0].Reason != memoryReasonUnsupported {
		t.Fatalf("events %#v", events)
	}
}

func TestMemoryGateLogsDegradationOnce(t *testing.T) {
	var events []memoryGateEvent
	restore := installMemoryGateTest(t)
	defer restore()
	memoryGateNotify = func(ev memoryGateEvent) { events = append(events, ev) }
	memoryGateLogGap = time.Hour

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer hub.Close()
	t.Setenv(desktopdURLEnv, "")
	t.Setenv(desktopdURLLegacyEnv, "")
	t.Setenv(desktopdTokenEnv, "")
	t.Setenv(desktopHubURLEnv, hub.URL)
	t.Setenv(desktopHubTokenEnv, "hub-token")

	for i := 0; i < 2; i++ {
		pressed, _, err := readDesktopMemoryPressure(context.Background(), "tenant-a", "alice")
		if err != nil || pressed {
			t.Fatalf("read %d pressed=%v err=%v", i, pressed, err)
		}
	}
	if len(events) != 1 || events[0].Gate != memoryGateDegraded || events[0].Source != memorySourceHub || events[0].Reason != memoryReasonUnsupported || events[0].Policy != memoryPolicyFailOpen {
		t.Fatalf("events %#v", events)
	}
	fields := memoryGateFields()
	if fields["memory_gate"] != memoryGateDegraded || fields["memory_gate_reason"] != memoryReasonUnsupported || fields["memory_gate_policy"] != memoryPolicyFailOpen {
		t.Fatalf("fields %#v", fields)
	}

	memoryGateLogGap = 0
	pressed, _, err := readDesktopMemoryPressure(context.Background(), "tenant-a", "alice")
	if err != nil || pressed {
		t.Fatalf("third pressed=%v err=%v", pressed, err)
	}
	if len(events) != 2 {
		t.Fatalf("events after interval %#v", events)
	}
}

func TestMemoryGateFailClosedReturnsTypedError(t *testing.T) {
	restore := installMemoryGateTest(t)
	defer restore()
	t.Setenv(memoryReadPolicyEnv, memoryPolicyFailClosed)
	t.Setenv(desktopdURLEnv, "")
	t.Setenv(desktopdURLLegacyEnv, "")
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer hub.Close()
	t.Setenv(desktopHubURLEnv, hub.URL)
	t.Setenv(desktopHubTokenEnv, "hub-token")

	_, _, err := readDesktopMemoryPressure(context.Background(), "tenant-a", "alice")
	var gate *memoryGateError
	if !errors.As(err, &gate) || !errors.Is(err, errMemoryGateDegraded) || gate.Kind() != memoryReasonUnsupported || gate.Source != memorySourceHub {
		t.Fatalf("err=%v kind=%v", err, gate)
	}
	if got := memoryGateFields()["memory_gate"]; got != memoryGateDegraded {
		t.Fatalf("memory_gate %q", got)
	}
}

func TestVersionAndHealthExposeMemoryGate(t *testing.T) {
	restore := installMemoryGateTest(t)
	defer restore()
	noteMemoryReadFailure(memorySourceHub, memoryReasonUnsupported)

	version := httptest.NewRecorder()
	(&HTTPServer{}).handleVersion(version, httptest.NewRequest(http.MethodGet, "/version", nil))
	if version.Code != http.StatusOK {
		t.Fatalf("version status %d", version.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(version.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["memory_gate"] != memoryGateDegraded || body["memory_gate_reason"] != memoryReasonUnsupported || body["version"] == "" {
		t.Fatalf("version %#v", body)
	}

	health := httptest.NewRecorder()
	(&HTTPServer{}).handleHealth(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status %d", health.Code)
	}
	body = nil
	if err := json.Unmarshal(health.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["memory_gate"] != memoryGateDegraded || body["memory_gate_reason"] != memoryReasonUnsupported {
		t.Fatalf("health %#v", body)
	}
}

func installMemoryGateTest(t *testing.T) func() {
	t.Helper()
	prevRemote := desktopRemoteMemory
	prevNotify := memoryGateNotify
	prevGap := memoryGateLogGap
	desktopRemoteMemory = nil
	resetMemoryGateForTest()
	memoryGateLogGap = time.Hour
	return func() {
		desktopRemoteMemory = prevRemote
		memoryGateNotify = prevNotify
		memoryGateLogGap = prevGap
		resetMemoryGateForTest()
	}
}
