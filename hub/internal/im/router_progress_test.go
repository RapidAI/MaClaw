package im

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestRouteToAgent_ProgressResetsTimeout verifies that progress updates
// from the Agent reset the response timeout, preventing 504 on long tasks.
func TestRouteToAgent_ProgressResetsTimeout(t *testing.T) {
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	var mu sync.Mutex
	var progressTexts []string

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		mu.Lock()
		progressTexts = append(progressTexts, text)
		mu.Unlock()
	})

	// Start RouteToAgent in a goroutine.
	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "查找视频文件")
		resultCh <- resp
	}()

	// Wait for the pending request to be created.
	time.Sleep(50 * time.Millisecond)

	router.mu.Lock()
	var reqID string
	var pending *PendingIMRequest
	for id, p := range router.pendingReqs {
		reqID = id
		pending = p
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	// Override timeout to 200ms for testing.
	pending.Timeout = 200 * time.Millisecond

	// Send a progress update after 100ms (before timeout).
	time.Sleep(100 * time.Millisecond)
	router.HandleAgentProgress(reqID, "⚙️ 正在执行工具: bash")

	// Wait another 100ms — without progress, this would have timed out.
	// But progress reset the timer, so we have another 200ms.
	time.Sleep(100 * time.Millisecond)

	// Now send the final response.
	router.HandleAgentResponse(reqID, &AgentResponse{
		Text: "找到 5 个视频文件。",
	})

	// Wait for result.
	select {
	case resp := <-resultCh:
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
		}
		if resp.Body != "找到 5 个视频文件。" {
			t.Fatalf("unexpected body: %s", resp.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}

	// Verify progress was delivered (throttle interval is 10s, first one
	// should always be delivered immediately).
	mu.Lock()
	defer mu.Unlock()
	if len(progressTexts) != 1 {
		t.Fatalf("expected 1 progress text, got %d", len(progressTexts))
	}
	if progressTexts[0] != "⚙️ 正在执行工具: bash" {
		t.Fatalf("unexpected progress text: %s", progressTexts[0])
	}
}

// TestRouteToAgent_TimeoutWithProgressInfo verifies that when a timeout
// occurs after progress was received, the error message includes the last
// progress status.
func TestRouteToAgent_TimeoutWithProgressInfo(t *testing.T) {
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {})

	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "搜索文件")
		resultCh <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	router.mu.Lock()
	var reqID string
	var pending *PendingIMRequest
	for id, p := range router.pendingReqs {
		reqID = id
		pending = p
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	// Set very short timeout.
	pending.Timeout = 150 * time.Millisecond

	// Send progress.
	time.Sleep(50 * time.Millisecond)
	router.HandleAgentProgress(reqID, "⏳ 命令仍在执行中（已 30s）: find / -name *.mp4")

	// Let it timeout after the progress reset.
	select {
	case resp := <-resultCh:
		if resp.StatusCode != 504 {
			t.Fatalf("expected 504, got %d", resp.StatusCode)
		}
		if !containsStr(resp.Body, "命令仍在执行中") {
			t.Fatalf("expected progress info in timeout body, got: %s", resp.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timeout response")
	}
}

// TestRouteToAgent_ProgressThrottling verifies that rapid progress updates
// are throttled so the user doesn't get flooded with messages.
func TestRouteToAgent_ProgressThrottling(t *testing.T) {
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	var mu sync.Mutex
	var deliveredCount int

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		mu.Lock()
		deliveredCount++
		mu.Unlock()
	})

	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "搜索文件")
		resultCh <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	router.mu.Lock()
	var reqID string
	for id := range router.pendingReqs {
		reqID = id
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	// Send 5 rapid progress updates (within the 10s throttle window).
	for i := 0; i < 5; i++ {
		router.HandleAgentProgress(reqID, "progress")
		time.Sleep(10 * time.Millisecond)
	}

	// Send final response.
	router.HandleAgentResponse(reqID, &AgentResponse{Text: "done"})

	select {
	case resp := <-resultCh:
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	// Only the first progress should have been delivered (others throttled).
	mu.Lock()
	defer mu.Unlock()
	if deliveredCount != 1 {
		t.Fatalf("expected 1 delivered progress (throttled), got %d", deliveredCount)
	}
}

// TestRouteToAgent_ProgressDedup verifies that consecutive identical progress
// messages are deduplicated even when the throttle interval has elapsed.
func TestRouteToAgent_ProgressDedup(t *testing.T) {
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	var mu sync.Mutex
	var deliveredTexts []string

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		mu.Lock()
		deliveredTexts = append(deliveredTexts, text)
		mu.Unlock()
	})

	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "搜索文件")
		resultCh <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	router.mu.Lock()
	var reqID string
	var pending *PendingIMRequest
	for id, p := range router.pendingReqs {
		reqID = id
		pending = p
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	pending.Timeout = 5 * time.Second

	// All messages arrive within the 10s throttle window.
	// bash (delivered) → read_file (throttled) → read_file (throttled + dup)
	router.HandleAgentProgress(reqID, "⚙️ 正在执行工具: bash")
	time.Sleep(50 * time.Millisecond)
	router.HandleAgentProgress(reqID, "⚙️ 正在执行工具: read_file")
	time.Sleep(50 * time.Millisecond)
	router.HandleAgentProgress(reqID, "⚙️ 正在执行工具: read_file") // dup

	time.Sleep(50 * time.Millisecond)
	router.HandleAgentResponse(reqID, &AgentResponse{Text: "done"})

	select {
	case resp := <-resultCh:
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}

	mu.Lock()
	defer mu.Unlock()

	// Only the first progress ("bash") is delivered; the rest are throttled.
	if len(deliveredTexts) != 1 {
		t.Fatalf("expected 1 delivered progress, got %d: %v", len(deliveredTexts), deliveredTexts)
	}
	if deliveredTexts[0] != "⚙️ 正在执行工具: bash" {
		t.Fatalf("unexpected delivered text: %s", deliveredTexts[0])
	}
}

// TestBroadcastProgressDedup verifies that when multiple devices send
// identical progress messages during a broadcast, only the first one
// is delivered to the user.
func TestBroadcastProgressDedup(t *testing.T) {
	df := &mockDeviceFinder{
		found: true,
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "Mac-Home", LLMConfigured: true},
			{MachineID: "m2", Name: "Mac-Office", LLMConfigured: true},
		},
	}
	router := NewMessageRouter(df)
	defer router.Stop()

	var mu sync.Mutex
	var deliveredTexts []string

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		mu.Lock()
		deliveredTexts = append(deliveredTexts, text)
		mu.Unlock()
	})
	router.SetResponseDelivery(func(ctx context.Context, userID, platformName, platformUID string, resp *GenericResponse) {})

	// Enter broadcast mode.
	router.mu.Lock()
	router.selectedMachine["user1"] = broadcastMachineID
	router.mu.Unlock()

	// Start broadcast in a goroutine.
	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "hello")
		resultCh <- resp
	}()

	// Wait for pending requests to be created (one per device).
	time.Sleep(100 * time.Millisecond)

	// Collect all pending request IDs.
	router.mu.Lock()
	var reqIDs []string
	for id := range router.pendingReqs {
		reqIDs = append(reqIDs, id)
	}
	router.mu.Unlock()

	if len(reqIDs) < 2 {
		t.Fatalf("expected at least 2 pending requests for broadcast, got %d", len(reqIDs))
	}

	// Both devices send the same progress text.
	for _, id := range reqIDs {
		router.HandleAgentProgress(id, "⏳ 需要一点时间处理，请稍候...")
	}

	// Give progress delivery goroutines time to run.
	time.Sleep(100 * time.Millisecond)

	// Send responses from both devices.
	for _, id := range reqIDs {
		router.HandleAgentResponse(id, &AgentResponse{Text: "done"})
	}

	select {
	case resp := <-resultCh:
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for broadcast response")
	}

	// The key assertion: identical progress from 2 devices should only
	// be delivered once thanks to broadcastProgressDedup.
	mu.Lock()
	defer mu.Unlock()
	if len(deliveredTexts) != 1 {
		t.Fatalf("expected 1 delivered progress (deduped across devices), got %d: %v", len(deliveredTexts), deliveredTexts)
	}
}

// TestBroadcastProgressDedup_DifferentTextsPass verifies that different
// progress messages from different devices are all delivered (not suppressed).
func TestBroadcastProgressDedup_DifferentTextsPass(t *testing.T) {
	df := &mockDeviceFinder{
		found: true,
		allMachines: []OnlineMachineInfo{
			{MachineID: "m1", Name: "Mac-Home", LLMConfigured: true},
			{MachineID: "m2", Name: "Mac-Office", LLMConfigured: true},
		},
	}
	router := NewMessageRouter(df)
	defer router.Stop()

	var mu sync.Mutex
	var deliveredTexts []string

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		mu.Lock()
		deliveredTexts = append(deliveredTexts, text)
		mu.Unlock()
	})
	router.SetResponseDelivery(func(ctx context.Context, userID, platformName, platformUID string, resp *GenericResponse) {})

	router.mu.Lock()
	router.selectedMachine["user1"] = broadcastMachineID
	router.mu.Unlock()

	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "hello")
		resultCh <- resp
	}()

	time.Sleep(100 * time.Millisecond)

	// Collect pending request IDs and sort by machine for deterministic assignment.
	router.mu.Lock()
	reqMap := make(map[string]*PendingIMRequest)
	for id, p := range router.pendingReqs {
		reqMap[id] = p
	}
	router.mu.Unlock()

	if len(reqMap) < 2 {
		t.Fatalf("expected at least 2 pending requests, got %d", len(reqMap))
	}

	// Each device sends a DIFFERENT progress text.
	i := 0
	var reqIDs []string
	for id := range reqMap {
		reqIDs = append(reqIDs, id)
		if i == 0 {
			router.HandleAgentProgress(id, "⚙️ 正在执行工具: bash")
		} else {
			router.HandleAgentProgress(id, "⚙️ 正在执行工具: read_file")
		}
		i++
	}

	time.Sleep(100 * time.Millisecond)

	for _, id := range reqIDs {
		router.HandleAgentResponse(id, &AgentResponse{Text: "done"})
	}

	select {
	case resp := <-resultCh:
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for broadcast response")
	}

	// Both different progress texts should be delivered.
	mu.Lock()
	defer mu.Unlock()
	if len(deliveredTexts) != 2 {
		t.Fatalf("expected 2 delivered progress (different texts), got %d: %v", len(deliveredTexts), deliveredTexts)
	}
}
