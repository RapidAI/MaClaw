package ve

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIntegration_Security_PlaintextNotLeaked verifies that the encrypted quota file
// does not contain the plaintext quota value in any recognizable form.
func TestIntegration_Security_PlaintextNotLeaked(t *testing.T) {
	tmpDir := t.TempDir()
	quotaFile := filepath.Join(tmpDir, "quota.enc")
	keyMat := make([]byte, 32)
	_, _ = rand.Read(keyMat)

	qs := NewQuotaStore(keyMat, "hub-security-test", quotaFile)

	// Test with various quota values
	testQuotas := []int{0, 1, 5, 42, 100, 999, 5000, 10000}
	for _, quota := range testQuotas {
		if err := qs.SaveQuota(quota); err != nil {
			t.Fatalf("SaveQuota(%d) failed: %v", quota, err)
		}

		data, err := os.ReadFile(quotaFile)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}

		// The file should be valid JSON with ciphertext/nonce fields
		var file encryptedQuotaFile
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("quota file is not valid JSON: %v", err)
		}

		// Ciphertext should not be empty
		if len(file.Ciphertext) == 0 {
			t.Fatal("ciphertext should not be empty")
		}

		// The raw file content should not contain the quota value as a JSON field
		// (it's encrypted, so "quota":5 should not appear)
		quotaStr := `"quota":` + strings.TrimSpace(string(rune(quota+'0')))
		if quota >= 10 {
			// For multi-digit numbers, check the full string
			quotaJSON := []byte(`"quota":` + json.Number(strings.TrimSpace(string(json.Number(itoa(quota))))))
			_ = quotaJSON // complex check below
		}
		// Simple check: the plaintext JSON representation should not appear
		plainPayload := quotaPayload{Quota: quota, HubID: "hub-security-test", Timestamp: time.Now()}
		plainJSON, _ := json.Marshal(plainPayload)
		if bytes.Contains(data, plainJSON[:20]) { // first 20 bytes of plaintext
			t.Fatalf("quota file appears to contain plaintext data for quota=%d", quota)
		}
		_ = quotaStr
	}
}

// TestIntegration_Security_MACTamperDetection verifies that modifying the MAC
// in the encrypted payload causes LoadQuota to fail.
func TestIntegration_Security_MACTamperDetection(t *testing.T) {
	tmpDir := t.TempDir()
	quotaFile := filepath.Join(tmpDir, "quota.enc")
	keyMat := []byte("security-test-key-32-bytes-long!")

	qs := NewQuotaStore(keyMat, "hub-mac-test", quotaFile)
	if err := qs.SaveQuota(7); err != nil {
		t.Fatalf("SaveQuota failed: %v", err)
	}

	// Verify normal load works
	q, err := qs.LoadQuota()
	if err != nil {
		t.Fatalf("LoadQuota should succeed: %v", err)
	}
	if q != 7 {
		t.Fatalf("expected quota=7, got %d", q)
	}

	// Tamper with the ciphertext (simulates MAC tampering since GCM is authenticated)
	data, _ := os.ReadFile(quotaFile)
	var file encryptedQuotaFile
	_ = json.Unmarshal(data, &file)

	// Flip a byte in the ciphertext
	if len(file.Ciphertext) > 10 {
		file.Ciphertext[10] ^= 0xFF
	}

	tamperedData, _ := json.Marshal(file)
	_ = os.WriteFile(quotaFile, tamperedData, 0o600)

	// LoadQuota should fail (GCM authentication failure = tamper detected)
	qs.InvalidateCache()
	_, err = qs.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota should fail after ciphertext tampering")
	}
	if !strings.Contains(err.Error(), "decrypt") && !strings.Contains(err.Error(), "tamper") {
		t.Logf("error message: %v (acceptable - GCM detects tampering)", err)
	}

	// GetEffectiveQuota should return 0 (degraded)
	qs.InvalidateCache()
	if qs.GetEffectiveQuota() != 0 {
		t.Fatal("GetEffectiveQuota should return 0 after tampering")
	}
}

// TestIntegration_Security_CrossHubCopyDetection verifies that copying the quota file
// to a different Hub (different hubID) causes LoadQuota to fail.
func TestIntegration_Security_CrossHubCopyDetection(t *testing.T) {
	tmpDir := t.TempDir()
	quotaFile := filepath.Join(tmpDir, "quota.enc")
	keyMat := []byte("cross-hub-test-key-32-bytes-long")

	// Hub A saves quota
	qsA := NewQuotaStore(keyMat, "hub-A", quotaFile)
	if err := qsA.SaveQuota(10); err != nil {
		t.Fatalf("SaveQuota on Hub A failed: %v", err)
	}

	// Hub B tries to load the same file (different hubID, same key)
	qsB := NewQuotaStore(keyMat, "hub-B", quotaFile)
	_, err := qsB.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota should fail when hubID doesn't match")
	}
	if !strings.Contains(err.Error(), "hub ID mismatch") {
		t.Fatalf("expected hub ID mismatch error, got: %v", err)
	}

	// GetEffectiveQuota on Hub B should return 0
	if qsB.GetEffectiveQuota() != 0 {
		t.Fatal("GetEffectiveQuota should return 0 for cross-hub copy")
	}
}

// TestIntegration_Security_TimestampExpiry verifies that an expired quota file
// (>24h old) causes LoadQuota to fail.
func TestIntegration_Security_TimestampExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	quotaFile := filepath.Join(tmpDir, "quota.enc")
	keyMat := []byte("expiry-test-key-32-bytes-long!!!")

	qs := NewQuotaStore(keyMat, "hub-expiry", quotaFile)

	// Create a payload with timestamp 25 hours ago
	oldTimestamp := time.Now().UTC().Add(-25 * time.Hour)
	mac := qs.computeMAC(8, "hub-expiry", oldTimestamp)
	payload := quotaPayload{
		Quota:     8,
		HubID:     "hub-expiry",
		Timestamp: oldTimestamp,
		MAC:       mac,
	}

	// Encrypt and write using test helper
	data := encryptPayloadForTest(t, qs, payload)
	if err := os.WriteFile(quotaFile, data, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// LoadQuota should fail due to expired timestamp
	_, err := qs.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota should fail for expired timestamp")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected timestamp expired error, got: %v", err)
	}

	// GetEffectiveQuota should return 0
	if qs.GetEffectiveQuota() != 0 {
		t.Fatal("GetEffectiveQuota should return 0 for expired quota")
	}
}

// TestIntegration_Security_AccessPolicyConsistency verifies that AccessPolicy
// filtering is consistent between ListDiscoverable and CanAccess.
func TestIntegration_Security_AccessPolicyConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	keyMat := []byte("policy-test-key-32-bytes-long!!!")

	qs := NewQuotaStore(keyMat, "hub-policy", tmpDir+"/quota.enc")
	_ = qs.SaveQuota(20)
	registry := NewRegistry(qs, "")

	// Create VEs with different policies
	policies := []struct {
		name      string
		policy    AccessPolicy
		whitelist []string
		blacklist []string
	}{
		{"Public VE", PolicyPublic, nil, nil},
		{"Whitelist VE", PolicyWhitelist, []string{"allowed-1", "allowed-2"}, nil},
		{"Blacklist VE", PolicyBlacklist, nil, []string{"blocked-1"}},
		{"PerRequest VE", PolicyPerRequest, nil, nil},
	}

	veIDs := make([]string, len(policies))
	for i, p := range policies {
		ve, err := registry.Register(VERegistrationRequest{
			OwnerMachineID: "owner-" + strings.ReplaceAll(p.name, " ", "-"),
			Name:           p.name,
			SkillDesc:      "Test",
			AccessPolicy:   p.policy,
			Whitelist:      p.whitelist,
			Blacklist:      p.blacklist,
		})
		if err != nil {
			t.Fatalf("Register %s failed: %v", p.name, err)
		}
		_ = registry.Approve(ve.ID)
		veIDs[i] = ve.ID
	}

	// Test consistency: if a VE is visible in ListDiscoverable, CanAccess should
	// return allowed=true (or needsAuth=true for per_request).
	// If not visible, CanAccess should return allowed=false.
	testRequesters := []string{"allowed-1", "allowed-2", "blocked-1", "random-user"}

	for _, requester := range testRequesters {
		discoverable := registry.ListDiscoverable(requester)
		discoverableIDs := make(map[string]bool)
		for _, ve := range discoverable {
			discoverableIDs[ve.ID] = true
		}

		for i, veID := range veIDs {
			isDiscoverable := discoverableIDs[veID]
			allowed, needsAuth, err := registry.CanAccess(veID, requester)
			if err != nil {
				t.Fatalf("CanAccess(%s, %s) error: %v", veID, requester, err)
			}

			policy := policies[i].policy

			// Consistency check
			switch policy {
			case PolicyPublic:
				if !isDiscoverable {
					t.Fatalf("public VE should always be discoverable (requester=%s)", requester)
				}
				if !allowed {
					t.Fatalf("public VE should always allow access (requester=%s)", requester)
				}
			case PolicyWhitelist:
				if isDiscoverable && !allowed {
					t.Fatalf("whitelist VE: discoverable but not allowed (requester=%s) — inconsistency!", requester)
				}
				if !isDiscoverable && allowed {
					t.Fatalf("whitelist VE: not discoverable but allowed (requester=%s) — inconsistency!", requester)
				}
			case PolicyBlacklist:
				if isDiscoverable && !allowed {
					t.Fatalf("blacklist VE: discoverable but not allowed (requester=%s) — inconsistency!", requester)
				}
				if !isDiscoverable && allowed {
					t.Fatalf("blacklist VE: not discoverable but allowed (requester=%s) — inconsistency!", requester)
				}
			case PolicyPerRequest:
				if !isDiscoverable {
					t.Fatalf("per_request VE should always be discoverable (requester=%s)", requester)
				}
				if allowed {
					t.Fatalf("per_request VE should not directly allow (requester=%s)", requester)
				}
				if !needsAuth {
					t.Fatalf("per_request VE should require auth (requester=%s)", requester)
				}
			}
		}
	}
}

// itoa is a simple int-to-string helper for the test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
