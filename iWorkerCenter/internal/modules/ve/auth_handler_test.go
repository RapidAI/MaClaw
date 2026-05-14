package ve

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAuthHandler_AllowFlow(t *testing.T) {
	h := NewAuthHandler()

	var pushMu sync.Mutex
	var pushedReq AuthorizationRequest
	h.SetOnPush(func(ownerMachineID string, req AuthorizationRequest) {
		pushMu.Lock()
		pushedReq = req
		pushMu.Unlock()
	})

	ve := &VirtualEmployee{
		ID:             "ve-auth-1",
		OwnerMachineID: "owner-machine",
		Name:           "Auth Test VE",
	}

	var result AuthResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = h.InitiateAuth(context.Background(), "Requester", "req-machine", ve)
	}()

	// Wait for push callback to fire
	time.Sleep(100 * time.Millisecond)

	pushMu.Lock()
	reqID := pushedReq.ID
	pushMu.Unlock()

	if reqID == "" {
		t.Fatal("auth request was not pushed to owner")
	}

	// Owner allows
	err := h.HandleResponse(AuthorizationResponse{RequestID: reqID, Decision: "allow"})
	if err != nil {
		t.Fatal(err)
	}

	wg.Wait()

	if !result.Allowed {
		t.Error("expected Allowed=true")
	}
	if result.Reason != "approved" {
		t.Errorf("Reason = %q, want approved", result.Reason)
	}
}

func TestAuthHandler_DenyFlow(t *testing.T) {
	h := NewAuthHandler()

	var pushedReq AuthorizationRequest
	h.SetOnPush(func(ownerMachineID string, req AuthorizationRequest) {
		pushedReq = req
	})

	ve := &VirtualEmployee{ID: "ve-deny", OwnerMachineID: "owner", Name: "Deny VE"}

	var result AuthResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		result = h.InitiateAuth(context.Background(), "Req", "req-m", ve)
	}()

	time.Sleep(20 * time.Millisecond)

	h.HandleResponse(AuthorizationResponse{RequestID: pushedReq.ID, Decision: "deny"})
	wg.Wait()

	if result.Allowed {
		t.Error("expected Allowed=false")
	}
	if result.Reason != "denied" {
		t.Errorf("Reason = %q, want denied", result.Reason)
	}
}

func TestAuthHandler_Timeout(t *testing.T) {
	h := NewAuthHandler()
	h.SetOnPush(func(string, AuthorizationRequest) {})

	ve := &VirtualEmployee{ID: "ve-timeout", OwnerMachineID: "owner", Name: "Timeout VE"}

	// Use a short context timeout to simulate the 60s timeout quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := h.InitiateAuth(ctx, "Req", "req-m", ve)

	if result.Allowed {
		t.Error("expected Allowed=false on timeout")
	}
	if result.Reason != "timeout" && result.Reason != "cancelled" {
		t.Errorf("Reason = %q, want timeout or cancelled", result.Reason)
	}

	// Pending should be cleaned up
	if h.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0 after timeout", h.PendingCount())
	}
}

func TestAuthHandler_HandleResponse_NotFound(t *testing.T) {
	h := NewAuthHandler()

	err := h.HandleResponse(AuthorizationResponse{RequestID: "nonexistent", Decision: "allow"})
	if err == nil {
		t.Error("HandleResponse for nonexistent request should fail")
	}
}

func TestAuthHandler_HandleResponse_InvalidDecision(t *testing.T) {
	h := NewAuthHandler()
	h.SetOnPush(func(string, AuthorizationRequest) {})

	ve := &VirtualEmployee{ID: "ve-inv", OwnerMachineID: "owner", Name: "VE"}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.InitiateAuth(ctx, "R", "rm", ve)
	}()

	time.Sleep(50 * time.Millisecond)

	// Get the pending request ID
	h.mu.Lock()
	var reqID string
	for id := range h.pending {
		reqID = id
		break
	}
	h.mu.Unlock()

	err := h.HandleResponse(AuthorizationResponse{RequestID: reqID, Decision: "maybe"})
	if err == nil {
		t.Error("invalid decision should return error")
	}

	// The request should still be pending (invalid decision doesn't consume it)
	// Let context timeout clean it up
	wg.Wait()
}

func TestAuthHandler_CleanupExpired(t *testing.T) {
	h := NewAuthHandler()
	h.SetOnPush(func(string, AuthorizationRequest) {})

	// Manually insert an expired request
	h.mu.Lock()
	h.pending["expired-1"] = &pendingAuth{
		Request: AuthorizationRequest{
			ID:        "expired-1",
			ExpiresAt: time.Now().Add(-1 * time.Second),
		},
		ResultCh: make(chan AuthResult, 1),
	}
	h.mu.Unlock()

	cleaned := h.CleanupExpired()
	if cleaned != 1 {
		t.Errorf("CleanupExpired() = %d, want 1", cleaned)
	}
	if h.PendingCount() != 0 {
		t.Errorf("PendingCount after cleanup = %d, want 0", h.PendingCount())
	}
}

func TestAuthHandler_ConcurrentRequests(t *testing.T) {
	h := NewAuthHandler()

	var pushMu sync.Mutex
	var pushed []AuthorizationRequest
	h.SetOnPush(func(_ string, req AuthorizationRequest) {
		pushMu.Lock()
		pushed = append(pushed, req)
		pushMu.Unlock()
	})

	ve := &VirtualEmployee{ID: "ve-conc", OwnerMachineID: "owner", Name: "Concurrent VE"}

	// Launch 3 concurrent auth requests
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			h.InitiateAuth(ctx, "Req", "req-m", ve)
		}(i)
	}

	time.Sleep(30 * time.Millisecond)

	if h.PendingCount() != 3 {
		t.Errorf("PendingCount = %d, want 3", h.PendingCount())
	}

	// Allow all
	pushMu.Lock()
	for _, req := range pushed {
		h.HandleResponse(AuthorizationResponse{RequestID: req.ID, Decision: "allow"})
	}
	pushMu.Unlock()

	wg.Wait()

	if h.PendingCount() != 0 {
		t.Errorf("PendingCount after all resolved = %d, want 0", h.PendingCount())
	}
}
