package im

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// autoRespondDeviceFinder is a mock that auto-responds to im.user_message
// so routeToSingleMachine doesn't block forever.
type autoRespondDeviceFinder struct {
	allMachines []OnlineMachineInfo
	mu          sync.Mutex
	router      *MessageRouter // set after router creation
}

func (f *autoRespondDeviceFinder) FindOnlineMachineForUser(_ context.Context, _ string) (string, bool, bool) {
	if len(f.allMachines) > 0 {
		m := f.allMachines[0]
		return m.MachineID, m.LLMConfigured, true
	}
	return "", false, false
}

func (f *autoRespondDeviceFinder) FindAllOnlineMachinesForUser(_ context.Context, _ string) []OnlineMachineInfo {
	return f.allMachines
}

func (f *autoRespondDeviceFinder) FindOnlineMachineByName(_ context.Context, _ string, name string) (string, bool) {
	for _, m := range f.allMachines {
		if strings.EqualFold(m.Name, name) {
			return m.MachineID, true
		}
	}
	return "", false
}

func (f *autoRespondDeviceFinder) SendToMachine(_ string, msg any) error {
	// Extract request_id directly from the map (no JSON round-trip needed).
	var reqID string
	if m, ok := msg.(map[string]interface{}); ok {
		reqID, _ = m["request_id"].(string)
	}
	if reqID == "" {
		return nil
	}
	// Auto-respond after a short delay.
	go func(id string) {
		time.Sleep(50 * time.Millisecond)
		f.mu.Lock()
		r := f.router
		f.mu.Unlock()
		if r != nil {
			r.HandleAgentResponse(id, &AgentResponse{
				Text: "mock response",
			})
		}
	}(reqID)
	return nil
}

func newTestCoordinator(df *autoRespondDeviceFinder, llmServer *httptest.Server) *Coordinator {
	router := NewMessageRouter(df)
	df.mu.Lock()
	df.router = router
	df.mu.Unlock()

	var configFn func() *HubLLMConfig
	if llmServer != nil {
		configFn = func() *HubLLMConfig {
			return &HubLLMConfig{
				Enabled:  true,
				APIURL:   llmServer.URL,
				APIKey:   "test",
				Model:    "test",
				Protocol: "openai",
			}
		}
	} else {
		configFn = func() *HubLLMConfig { return nil }
	}

	return NewCoordinator(router, df, configFn)
}

func TestCoordinator_NoLLM_Passthrough(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	coord := newTestCoordinator(df, nil)

	// No LLM, no selected machine, multiple devices → passthrough (prompts user to /call)
	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Passthrough goes to RouteToAgent which should prompt device selection.
	if resp.StatusCode != 300 {
		t.Fatalf("expected 300 (select device), got %d: %s", resp.StatusCode, resp.Body)
	}
}

func TestCoordinator_NoLLM_SingleDevice(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
		},
	}
	coord := newTestCoordinator(df, nil)

	// Single device, no LLM → rule engine routes directly.
	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
}

func TestCoordinator_RuleHit_AtName(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	// LLM server — should NOT be called because rule matches.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"type":"broadcast","reason":"test"}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	coord := newTestCoordinator(df, srv)

	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "@MacBook check this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	if callCount != 0 {
		t.Fatalf("expected 0 LLM calls (rule hit), got %d", callCount)
	}
}

func TestCoordinator_LLM_ClassifiesRouteSingle(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	srv := mockLLMServer(`{"type":"route_single","target_id":"m1","reason":"Go project"}`)
	defer srv.Close()

	coord := newTestCoordinator(df, srv)

	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "帮我看Go代码")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
}

func TestCoordinator_LLM_ClassifiesBroadcast(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	srv := mockLLMServer(`{"type":"broadcast","reason":"general question"}`)
	defer srv.Close()

	coord := newTestCoordinator(df, srv)

	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "通用问题")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
}

func TestCoordinator_CircuitBroken_Passthrough(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	srv := mockLLMServer(`{"type":"broadcast","reason":"test"}`)
	defer srv.Close()

	coord := newTestCoordinator(df, srv)

	// Trip the circuit breaker manually.
	coord.breaker.RecordFailure()
	coord.breaker.RecordFailure()
	coord.breaker.RecordFailure()

	// With breaker open, llmEnabled=false → passthrough.
	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Passthrough → RouteToAgent → 300 (select device) since no machine selected.
	if resp.StatusCode != 300 {
		t.Fatalf("expected 300 (passthrough to select device), got %d: %s", resp.StatusCode, resp.Body)
	}
}

func TestCoordinator_NoDevices(t *testing.T) {
	df := &autoRespondDeviceFinder{allMachines: nil}
	coord := newTestCoordinator(df, nil)

	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestCoordinator_NeedClarification(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	srv := mockLLMServer(`{"type":"need_clarification","message":"请说明要发给哪台设备"}`)
	defer srv.Close()

	coord := newTestCoordinator(df, srv)

	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "嗯")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Body, "请说明") {
		t.Fatalf("expected clarification message, got: %s", resp.Body)
	}
}

func TestCoordinator_CommandsStillWork(t *testing.T) {
	// Commands are handled by the Adapter before Coordinator, so Coordinator
	// should never see them. This test verifies that the Coordinator doesn't
	// break when receiving normal text (commands are pre-filtered).
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
		},
	}
	coord := newTestCoordinator(df, nil)

	// Normal text to single device → should route fine.
	resp, err := coord.Coordinate(context.Background(), "user1", "test", "uid1", "normal message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
}
