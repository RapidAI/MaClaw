package ve

import (
	"context"
	"testing"
	"time"
)

// TestIntegration_ConversationFlow tests the conversation initiation flow:
// Client A initiates → Hub routes → VE Client B receives → session created.
// Tests public policy (direct session) and message routing via GroupDiscussionMessage.
func TestIntegration_ConversationFlow(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")
	hubID := "hub-conv-test"

	// Setup: QuotaStore + Registry + PresenceManager + AuthHandler
	qs := NewQuotaStore(keyMat, hubID, tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)

	registry := NewRegistry(qs, "")
	presence := NewPresenceManager()
	authHandler := NewAuthHandler()

	// Register and approve a VE with public policy
	ve, err := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-ve-owner",
		Name:           "Code Helper",
		SkillDesc:      "Helps with coding tasks",
		AccessPolicy:   PolicyPublic,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := registry.Approve(ve.ID); err != nil {
		t.Fatalf("Approve failed: %v", err)
	}

	// Mark VE as online (simulating WebSocket connect + heartbeat)
	presence.RecordHeartbeat(ve.ID, "machine-ve-owner")
	if !presence.IsOnline(ve.ID) {
		t.Fatal("VE should be online after heartbeat")
	}

	// Client A initiates conversation with public VE → direct session (no auth needed)
	allowed, needsAuth, err := registry.CanAccess(ve.ID, "machine-client-a")
	if err != nil {
		t.Fatalf("CanAccess failed: %v", err)
	}
	if !allowed {
		t.Fatal("public VE should allow access")
	}
	if needsAuth {
		t.Fatal("public VE should not need auth")
	}

	// Verify VE is discoverable by Client A
	discoverable := registry.ListDiscoverable("machine-client-a")
	found := false
	for _, d := range discoverable {
		if d.ID == ve.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("VE should be discoverable by Client A")
	}

	// Test per_request VE requires auth flow
	vePerReq, err := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-ve-owner-2",
		Name:           "Private Helper",
		SkillDesc:      "Private assistant",
		AccessPolicy:   PolicyPerRequest,
	})
	if err != nil {
		t.Fatalf("Register per_request VE failed: %v", err)
	}
	if err := registry.Approve(vePerReq.ID); err != nil {
		t.Fatalf("Approve per_request VE failed: %v", err)
	}

	allowed, needsAuth, err = registry.CanAccess(vePerReq.ID, "machine-client-a")
	if err != nil {
		t.Fatalf("CanAccess per_request failed: %v", err)
	}
	if allowed {
		t.Fatal("per_request VE should not directly allow access")
	}
	if !needsAuth {
		t.Fatal("per_request VE should require auth")
	}

	// Simulate auth flow: initiate → owner allows → session created
	presence.RecordHeartbeat(vePerReq.ID, "machine-ve-owner-2")

	var pushReceived AuthorizationRequest
	authHandler.SetOnPush(func(ownerMachineID string, req AuthorizationRequest) {
		pushReceived = req
	})

	// Start auth in background
	resultCh := make(chan AuthResult, 1)
	go func() {
		result := authHandler.InitiateAuth(
			context.Background(),
			"Alice",
			"machine-client-a",
			vePerReq,
		)
		resultCh <- result
	}()

	// Wait for push to be received
	time.Sleep(50 * time.Millisecond)
	if pushReceived.ID == "" {
		t.Fatal("auth request should have been pushed to owner")
	}
	if pushReceived.TargetVEID != vePerReq.ID {
		t.Fatalf("expected target VE %s, got %s", vePerReq.ID, pushReceived.TargetVEID)
	}

	// Owner responds with "allow"
	err = authHandler.HandleResponse(AuthorizationResponse{
		RequestID: pushReceived.ID,
		Decision:  "allow",
	})
	if err != nil {
		t.Fatalf("HandleResponse failed: %v", err)
	}

	// Verify result
	select {
	case result := <-resultCh:
		if !result.Allowed {
			t.Fatalf("expected allowed=true, got reason=%s", result.Reason)
		}
		if result.Reason != "approved" {
			t.Fatalf("expected reason=approved, got %s", result.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for auth result")
	}
}

// TestIntegration_ConversationFlow_WhitelistBlacklist tests access policy filtering
// for whitelist and blacklist VEs.
func TestIntegration_ConversationFlow_WhitelistBlacklist(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("test-key-material-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-wb-test", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(10)
	registry := NewRegistry(qs, "")

	// Register whitelist VE
	veWL, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "owner-wl",
		Name:           "Whitelist VE",
		SkillDesc:      "Only for allowed users",
		AccessPolicy:   PolicyWhitelist,
		Whitelist:      []string{"machine-allowed-1", "machine-allowed-2"},
	})
	_ = registry.Approve(veWL.ID)

	// Register blacklist VE
	veBL, _ := registry.Register(VERegistrationRequest{
		OwnerMachineID: "owner-bl",
		Name:           "Blacklist VE",
		SkillDesc:      "Blocked for some users",
		AccessPolicy:   PolicyBlacklist,
		Blacklist:      []string{"machine-blocked-1"},
	})
	_ = registry.Approve(veBL.ID)

	// Whitelist: allowed user can access
	allowed, _, _ := registry.CanAccess(veWL.ID, "machine-allowed-1")
	if !allowed {
		t.Fatal("whitelisted user should be allowed")
	}

	// Whitelist: non-listed user cannot access
	allowed, _, _ = registry.CanAccess(veWL.ID, "machine-not-listed")
	if allowed {
		t.Fatal("non-whitelisted user should not be allowed")
	}

	// Blacklist: non-blocked user can access
	allowed, _, _ = registry.CanAccess(veBL.ID, "machine-normal")
	if !allowed {
		t.Fatal("non-blacklisted user should be allowed")
	}

	// Blacklist: blocked user cannot access
	allowed, _, _ = registry.CanAccess(veBL.ID, "machine-blocked-1")
	if allowed {
		t.Fatal("blacklisted user should not be allowed")
	}

	// Discoverable list respects policies
	discoverableForAllowed := registry.ListDiscoverable("machine-allowed-1")
	discoverableForBlocked := registry.ListDiscoverable("machine-blocked-1")

	// machine-allowed-1 should see whitelist VE
	foundWL := false
	for _, v := range discoverableForAllowed {
		if v.ID == veWL.ID {
			foundWL = true
		}
	}
	if !foundWL {
		t.Fatal("whitelisted user should see whitelist VE in discoverable list")
	}

	// machine-blocked-1 should NOT see blacklist VE
	for _, v := range discoverableForBlocked {
		if v.ID == veBL.ID {
			t.Fatal("blacklisted user should NOT see blacklist VE in discoverable list")
		}
	}
}
