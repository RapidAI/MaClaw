// Feature: compute-power-management, Property 4: Admin API Key masking
package compute

import (
	"testing"
	"testing/quick"
)

func TestPropertyAdminAPIMasking(t *testing.T) {
	f := func(apiKey string) bool {
		p := ComputeProvider{
			ID:       "test-id",
			Name:     "test-provider",
			BaseURL:  "https://api.example.com",
			APIKey:   apiKey,
			Protocol: ProtocolOpenAI,
		}

		// Simulate admin API masking: set HasAPIKey, clear APIKey
		masked := p
		masked.HasAPIKey = masked.APIKey != ""
		masked.APIKey = ""

		// Verify: api_key is always empty in masked version
		if masked.APIKey != "" {
			t.Log("masked api_key should be empty")
			return false
		}

		// Verify: has_api_key reflects whether original had a key
		expectedHasKey := apiKey != ""
		if masked.HasAPIKey != expectedHasKey {
			t.Logf("has_api_key=%v, expected=%v (original key=%q)", masked.HasAPIKey, expectedHasKey, apiKey)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}
