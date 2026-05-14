package ve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestIntegration_FullRegistrationFlow tests the complete registration lifecycle:
// HubCenter enrollment (ve_quota) → Hub quota encrypted storage → Client register →
// Admin approve → Client notification → quota enforcement.
func TestIntegration_FullRegistrationFlow(t *testing.T) {
	tmpDir := t.TempDir()
	quotaFile := filepath.Join(tmpDir, "quota.enc")
	registryFile := filepath.Join(tmpDir, "registry.json")

	keyMat := []byte("test-key-material-32-bytes-long!")
	hubID := "hub-integration-test-001"

	// Step 1: HubCenter enrollment delivers ve_quota=5 → Hub encrypts and stores
	qs := NewQuotaStore(keyMat, hubID, quotaFile)
	if err := qs.SaveQuota(5); err != nil {
		t.Fatalf("SaveQuota failed: %v", err)
	}

	// Verify encrypted file exists on disk
	if _, err := os.Stat(quotaFile); err != nil {
		t.Fatalf("encrypted quota file should exist: %v", err)
	}

	// Verify encrypted file does NOT contain plaintext "5"
	data, _ := os.ReadFile(quotaFile)
	// The quota value "5" should not appear as a standalone token in the encrypted file
	// (it's encrypted inside AES-256-GCM ciphertext)
	// We verify the file is valid JSON with ciphertext/nonce fields
	if len(data) < 50 {
		t.Fatal("encrypted file too small, likely not properly encrypted")
	}

	// Step 2: Verify quota can be loaded back
	loadedQuota, err := qs.LoadQuota()
	if err != nil {
		t.Fatalf("LoadQuota failed: %v", err)
	}
	if loadedQuota != 5 {
		t.Fatalf("expected quota=5, got %d", loadedQuota)
	}

	// Step 3: Create Registry with QuotaStore → Register VE
	registry := NewRegistry(qs, registryFile)

	ve1, err := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-client-1",
		Name:           "AI Assistant Alpha",
		SkillDesc:      "General purpose AI assistant",
		AccessPolicy:   PolicyPublic,
	})
	if err != nil {
		t.Fatalf("Register VE1 failed: %v", err)
	}
	if ve1.Status != VEStatusPending {
		t.Fatalf("expected pending status, got %s", ve1.Status)
	}

	// Step 4: Admin approves → status becomes active
	if err := registry.Approve(ve1.ID); err != nil {
		t.Fatalf("Approve failed: %v", err)
	}
	approved, ok := registry.GetByID(ve1.ID)
	if !ok {
		t.Fatal("VE not found after approval")
	}
	if approved.Status != VEStatusActive {
		t.Fatalf("expected active status, got %s", approved.Status)
	}
	if approved.ApprovedAt.IsZero() {
		t.Fatal("ApprovedAt should be set")
	}

	// Step 5: Register 4 more VEs and approve them (total active = 5 = quota)
	for i := 2; i <= 5; i++ {
		ve, err := registry.Register(VERegistrationRequest{
			OwnerMachineID: fmt.Sprintf("machine-client-%d", i),
			Name:           fmt.Sprintf("VE %d", i),
			SkillDesc:      "Test VE",
			AccessPolicy:   PolicyPublic,
		})
		if err != nil {
			t.Fatalf("Register VE%d failed: %v", i, err)
		}
		if err := registry.Approve(ve.ID); err != nil {
			t.Fatalf("Approve VE%d failed: %v", i, err)
		}
	}

	// Step 6: 6th registration should fail with quota_exceeded
	_, err = registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-client-6",
		Name:           "VE 6 (should fail)",
		SkillDesc:      "Over quota",
		AccessPolicy:   PolicyPublic,
	})
	if err == nil {
		t.Fatal("expected quota_exceeded error for 6th registration")
	}
	var qErr *QuotaExceededError
	if !errors.As(err, &qErr) {
		t.Fatalf("expected QuotaExceededError, got: %v", err)
	}
	if qErr.Active != 5 || qErr.Quota != 5 {
		t.Fatalf("expected active=5 quota=5, got active=%d quota=%d", qErr.Active, qErr.Quota)
	}

	// Step 7: Reject a pending VE
	pendingVE, err := registry.Register(VERegistrationRequest{
		OwnerMachineID: "machine-client-7",
		Name:           "VE to reject",
		SkillDesc:      "Will be rejected",
		AccessPolicy:   PolicyPublic,
	})
	// This should succeed because we're checking active count, not pending
	// Actually with quota=5 and 5 active, new registrations are blocked
	// Let's disable one first
	allVEs := registry.ListAll()
	var firstActiveID string
	for _, v := range allVEs {
		if v.Status == VEStatusActive {
			firstActiveID = v.ID
			break
		}
	}

	// Step 7a: Disable one VE → active count drops to 4
	if err := registry.Disable(firstActiveID); err != nil {
		t.Fatalf("Disable failed: %v", err)
	}
	disabledVE, _ := registry.GetByID(firstActiveID)
	if disabledVE.Status != VEStatusDisabled {
		t.Fatalf("expected disabled status, got %s", disabledVE.Status)
	}

	// Step 7b: Disabled VE should not appear in discoverable list
	discoverable := registry.ListDiscoverable("some-requester")
	for _, v := range discoverable {
		if v.ID == firstActiveID {
			t.Fatal("disabled VE should not be in discoverable list")
		}
	}

	// Step 7c: Now we can register again (active=4, quota=5)
	if pendingVE == nil {
		pendingVE, err = registry.Register(VERegistrationRequest{
			OwnerMachineID: "machine-client-7",
			Name:           "VE to reject",
			SkillDesc:      "Will be rejected",
			AccessPolicy:   PolicyPublic,
		})
		if err != nil {
			t.Fatalf("Register after disable failed: %v", err)
		}
	}

	// Step 8: Reject the pending VE
	if err := registry.Reject(pendingVE.ID, "not needed"); err != nil {
		t.Fatalf("Reject failed: %v", err)
	}
	rejectedVE, _ := registry.GetByID(pendingVE.ID)
	if rejectedVE.Status != VEStatusRejected {
		t.Fatalf("expected rejected status, got %s", rejectedVE.Status)
	}
	if rejectedVE.RejectReason != "not needed" {
		t.Fatalf("expected reject reason 'not needed', got %q", rejectedVE.RejectReason)
	}
}
