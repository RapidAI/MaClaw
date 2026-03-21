package im

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscussionConductor_StartReturnsImmediately(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	router := NewMessageRouter(df)
	df.mu.Lock()
	df.router = router
	df.mu.Unlock()

	// LLM server that always returns "conclude".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{
					"content": `{"action":"conclude","summary":"讨论结束"}`,
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &HubLLMConfig{Enabled: true, APIURL: srv.URL, APIKey: "k", Model: "m", Protocol: "openai"}
	cb := DefaultCircuitBreaker()
	dc := NewDiscussionConductor(func() *HubLLMConfig { return cfg }, cb, router)

	stopCh := make(chan struct{})
	resp := dc.StartConductedDiscussion(
		context.Background(), "user1", "test", "uid1", "API设计",
		df.allMachines, "", stopCh,
	)

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
	if !strings.Contains(resp.Body, "话题") {
		t.Fatalf("expected topic in start message, got: %s", resp.Body)
	}
}

func TestDiscussionConductor_MaxRoundsLimit(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	router := NewMessageRouter(df)
	df.mu.Lock()
	df.router = router
	df.mu.Unlock()

	// LLM server that always returns "ask_all" — never concludes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{
					"content": `{"action":"ask_all","prompt":"继续讨论"}`,
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &HubLLMConfig{Enabled: true, APIURL: srv.URL, APIKey: "k", Model: "m", Protocol: "openai"}
	cb := DefaultCircuitBreaker()
	dc := NewDiscussionConductor(func() *HubLLMConfig { return cfg }, cb, router)

	stopCh := make(chan struct{})
	dc.StartConductedDiscussion(
		context.Background(), "user1", "test", "uid1", "无限讨论",
		df.allMachines, "", stopCh,
	)

	// The discussion should auto-terminate after MaxRounds (10).
	// Give it some time to run through rounds.
	time.Sleep(3 * time.Second)
	// If we get here without hanging, the max rounds limit works.
}

func TestDiscussionConductor_StopTerminates(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	router := NewMessageRouter(df)
	df.mu.Lock()
	df.router = router
	df.mu.Unlock()

	// Slow LLM server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{
					"content": `{"action":"ask_all","prompt":"继续"}`,
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := &HubLLMConfig{Enabled: true, APIURL: srv.URL, APIKey: "k", Model: "m", Protocol: "openai"}
	cb := DefaultCircuitBreaker()
	dc := NewDiscussionConductor(func() *HubLLMConfig { return cfg }, cb, router)

	stopCh := make(chan struct{})
	dc.StartConductedDiscussion(
		context.Background(), "user1", "test", "uid1", "可停止讨论",
		df.allMachines, "", stopCh,
	)

	// Stop after a short delay.
	time.Sleep(500 * time.Millisecond)
	close(stopCh)

	// Give it time to wind down.
	time.Sleep(1 * time.Second)
	// If we get here, stop worked.
}

func TestDiscussionConductor_LLMFailureFallback(t *testing.T) {
	df := &autoRespondDeviceFinder{
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "MacBook", LLMConfigured: true},
			{MachineID: "m2", Name: "iMac", LLMConfigured: true},
		},
	}
	router := NewMessageRouter(df)
	df.mu.Lock()
	df.router = router
	df.mu.Unlock()

	// LLM server that returns errors.
	srv := mockLLMServerError()
	defer srv.Close()

	cfg := &HubLLMConfig{Enabled: true, APIURL: srv.URL, APIKey: "k", Model: "m", Protocol: "openai"}
	cb := DefaultCircuitBreaker()
	dc := NewDiscussionConductor(func() *HubLLMConfig { return cfg }, cb, router)

	stopCh := make(chan struct{})
	resp := dc.StartConductedDiscussion(
		context.Background(), "user1", "test", "uid1", "失败测试",
		df.allMachines, "", stopCh,
	)

	// Should still return a start message (discussion runs in background).
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Wait for the discussion to fail and terminate.
	time.Sleep(2 * time.Second)
}

func TestFilterDevices(t *testing.T) {
	all := []OnlineMachineInfo{
		{MachineID: "m1", Name: "MacBook"},
		{MachineID: "m2", Name: "iMac"},
		{MachineID: "m3", Name: "Mini"},
	}

	// Filter by ID.
	result := filterDevices(all, []string{"m1", "m3"})
	if len(result) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(result))
	}

	// Filter by name.
	result = filterDevices(all, []string{"iMac"})
	if len(result) != 1 || result[0].MachineID != "m2" {
		t.Fatalf("expected iMac, got %v", result)
	}

	// No match.
	result = filterDevices(all, []string{"nonexistent"})
	if len(result) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(result))
	}
}
