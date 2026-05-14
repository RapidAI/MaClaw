package ve

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestIntegration_AuthorizationFlow_Allow tests the per_request authorization flow
// where the owner allows access → session is created.
func TestIntegration_AuthorizationFlow_Allow(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-auth-test", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)
	registry := NewRegistry(qs, "")
	authHandler := NewAuthHandler()

	// Register per_request VE
	ve, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-owner",
		Name:           "Private Assistant",
		SkillDesc:      "Requires authorization",
		AccessPolicy:   PolicyPerRequest,
	})
	_ = registry.Approve(ve.ID)

	// Track auth request push
	var pushedReq AuthorizationRequest
	var pushedToMachine string
	authHandler.SetOnPush(func(ownerMachineID string, req AuthorizationRequest) {
		pushedToMachine = ownerMachineID
		pushedReq = req
	})

	// Initiate auth in background
	resultCh := make(chan AuthResult, 1)
	go func() {
		result := authHandler.InitiateAuth(
			context.Background(),
			"Bob",
			"machine-requester",
			ve,
		)
		resultCh <- result
	}()

	// Wait for push
	time.Sleep(50 * time.Millisecond)

	// Verify push was sent to the correct owner
	if pushedToMachine != "machine-owner" {
		t.Fatalf("expected push to machine-owner, got %s", pushedToMachine)
	}
	if pushedReq.RequesterName != "Bob" {
		t.Fatalf("expected requester name Bob, got %s", pushedReq.RequesterName)
	}
	if pushedReq.RequesterMachineID != "machine-requester" {
		t.Fatalf("expected requester machine machine-requester, got %s", pushedReq.RequesterMachineID)
	}
	if pushedReq.TargetVEID != ve.ID {
		t.Fatalf("expected target VE %s, got %s", ve.ID, pushedReq.TargetVEID)
	}
	if pushedReq.TargetVEName != "Private Assistant" {
		t.Fatalf("expected target VE name 'Private Assistant', got %s", pushedReq.TargetVEName)
	}
	// Verify expiry is ~60s from creation
	expectedExpiry := pushedReq.CreatedAt.Add(AuthRequestTimeout)
	if pushedReq.ExpiresAt.Sub(expectedExpiry) > time.Second {
		t.Fatalf("expiry time mismatch: expected ~%s, got %s", expectedExpiry, pushedReq.ExpiresAt)
	}

	// Owner responds: allow
	if err := authHandler.HandleResponse(AuthorizationResponse{
		RequestID: pushedReq.ID,
		Decision:  "allow",
	}); err != nil {
		t.Fatalf("HandleResponse allow failed: %v", err)
	}

	// Verify result
	select {
	case result := <-resultCh:
		if !result.Allowed {
			t.Fatal("expected allowed=true after owner allows")
		}
		if result.Reason != "approved" {
			t.Fatalf("expected reason=approved, got %s", result.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for auth result")
	}

	// Pending count should be 0
	if authHandler.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", authHandler.PendingCount())
	}
}

// TestIntegration_AuthorizationFlow_Deny tests the per_request authorization flow
// where the owner denies access → access_denied.
func TestIntegration_AuthorizationFlow_Deny(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-auth-deny", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)
	registry := NewRegistry(qs, "")
	authHandler := NewAuthHandler()

	ve, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-owner",
		Name:           "Exclusive VE",
		SkillDesc:      "Very selective",
		AccessPolicy:   PolicyPerRequest,
	})
	_ = registry.Approve(ve.ID)

	var pushedReq AuthorizationRequest
	authHandler.SetOnPush(func(_ string, req AuthorizationRequest) {
		pushedReq = req
	})

	resultCh := make(chan AuthResult, 1)
	go func() {
		result := authHandler.InitiateAuth(context.Background(), "Eve", "machine-eve", ve)
		resultCh <- result
	}()

	time.Sleep(50 * time.Millisecond)

	// Owner denies
	if err := authHandler.HandleResponse(AuthorizationResponse{
		RequestID: pushedReq.ID,
		Decision:  "deny",
	}); err != nil {
		t.Fatalf("HandleResponse deny failed: %v", err)
	}

	select {
	case result := <-resultCh:
		if result.Allowed {
			t.Fatal("expected allowed=false after owner denies")
		}
		if result.Reason != "denied" {
			t.Fatalf("expected reason=denied, got %s", result.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for deny result")
	}
}

// TestIntegration_AuthorizationFlow_Timeout tests the 60s timeout behavior.
func TestIntegration_AuthorizationFlow_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-auth-timeout", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)
	registry := NewRegistry(qs, "")
	authHandler := NewAuthHandler()

	ve, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-owner",
		Name:           "Slow Owner VE",
		SkillDesc:      "Owner never responds",
		AccessPolicy:   PolicyPerRequest,
	})
	_ = registry.Approve(ve.ID)

	authHandler.SetOnPush(func(_ string, _ AuthorizationRequest) {
		// Owner receives but never responds
	})

	// Use a short context timeout to simulate the 60s timeout without waiting
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := authHandler.InitiateAuth(ctx, "Charlie", "machine-charlie", ve)

	if result.Allowed {
		t.Fatal("expected allowed=false on timeout")
	}
	if result.Reason != "timeout" && result.Reason != "cancelled" {
		t.Fatalf("expected reason=timeout or cancelled, got %s", result.Reason)
	}

	// Pending should be cleaned up
	time.Sleep(50 * time.Millisecond)
	if authHandler.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after timeout, got %d", authHandler.PendingCount())
	}
}

// TestIntegration_AuthorizationFlow_ConcurrentRequests tests multiple concurrent
// authorization requests to the same VE.
func TestIntegration_AuthorizationFlow_ConcurrentRequests(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-auth-concurrent", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)
	registry := NewRegistry(qs, "")
	authHandler := NewAuthHandler()

	ve, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-owner",
		Name:           "Popular VE",
		SkillDesc:      "Many want access",
		AccessPolicy:   PolicyPerRequest,
	})
	_ = registry.Approve(ve.ID)

	var pushedReqs []AuthorizationRequest
	var pushMu sync.Mutex
	authHandler.SetOnPush(func(_ string, req AuthorizationRequest) {
		pushMu.Lock()
		pushedReqs = append(pushedReqs, req)
		pushMu.Unlock()
	})

	// Start 3 concurrent auth requests
	results := make([]chan AuthResult, 3)
	for i := 0; i < 3; i++ {
		results[i] = make(chan AuthResult, 1)
		go func(idx int) {
			result := authHandler.InitiateAuth(
				context.Background(),
				"User"+string(rune('A'+idx)),
				"machine-"+string(rune('a'+idx)),
				ve,
			)
			results[idx] <- result
		}(i)
	}

	// Wait for all 3 requests to be pushed
	deadline := time.After(2 * time.Second)
	for {
		pushMu.Lock()
		n := len(pushedReqs)
		pushMu.Unlock()
		if n >= 3 {
			break
		}
		select {
		case <-deadline:
			pushMu.Lock()
			t.Fatalf("timeout waiting for 3 pushed requests, got %d", len(pushedReqs))
			pushMu.Unlock()
			return
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Should have 3 pending requests
	if authHandler.PendingCount() != 3 {
		t.Fatalf("expected 3 pending, got %d", authHandler.PendingCount())
	}

	pushMu.Lock()
	localReqs := make([]AuthorizationRequest, len(pushedReqs))
	copy(localReqs, pushedReqs)
	pushMu.Unlock()

	// Allow the first pushed request, deny the second
	_ = authHandler.HandleResponse(AuthorizationResponse{RequestID: localReqs[0].ID, Decision: "allow"})
	_ = authHandler.HandleResponse(AuthorizationResponse{RequestID: localReqs[1].ID, Decision: "deny"})

	// Collect results from all channels
	time.Sleep(100 * time.Millisecond)

	var allowCount, denyCount int
	for i := 0; i < 3; i++ {
		select {
		case r := <-results[i]:
			if r.Allowed {
				allowCount++
			} else if r.Reason == "denied" {
				denyCount++
			}
		default:
			// Channel not ready yet (third request still pending)
		}
	}

	if allowCount != 1 {
		t.Fatalf("expected 1 allowed result, got %d", allowCount)
	}
	if denyCount != 1 {
		t.Fatalf("expected 1 denied result, got %d", denyCount)
	}

	// Third request should still be pending (not expired yet)
	if authHandler.PendingCount() != 1 {
		t.Fatalf("expected 1 pending (third request), got %d", authHandler.PendingCount())
	}
}
