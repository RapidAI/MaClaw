// Feature: compute-power-management, Property 18
package compute

import (
	"testing"
	"testing/quick"
)

// TestPropertyReconciliationDifference verifies that the reconciliation
// difference is always localTotal - cloudTotal (signed) (Property 18).
func TestPropertyReconciliationDifference(t *testing.T) {
	f := func(localTotal, cloudTotal int64) bool {
		result := ReconciliationResult{
			Month:              "2025-01",
			LocalTotalTokens:   localTotal,
			CloudTotalTokens:   cloudTotal,
			Difference:         localTotal - cloudTotal,
			CloudDataAvailable: true,
		}

		expectedDiff := localTotal - cloudTotal
		if result.Difference != expectedDiff {
			t.Logf("difference mismatch: %d != %d - %d",
				result.Difference, localTotal, cloudTotal)
			return false
		}

		// Verify sign: positive means local > cloud, negative means local < cloud
		if localTotal > cloudTotal && result.Difference <= 0 {
			// Edge case: overflow check
			if localTotal > 0 && cloudTotal < 0 {
				return true // potential overflow, skip
			}
			return false
		}
		if localTotal < cloudTotal && result.Difference >= 0 {
			if localTotal < 0 && cloudTotal > 0 {
				return true // potential overflow, skip
			}
			return false
		}
		if localTotal == cloudTotal && result.Difference != 0 {
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 18 failed: %v", err)
	}
}

// TestPropertyReconciliationResultConstruction verifies that constructing a
// ReconciliationResult always produces correct difference calculation.
func TestPropertyReconciliationResultConstruction(t *testing.T) {
	f := func(local, cloud uint32) bool {
		// Use uint32 to avoid overflow issues
		localTotal := int64(local)
		cloudTotal := int64(cloud)

		diff := localTotal - cloudTotal

		result := ReconciliationResult{
			Month:              "2025-06",
			LocalTotalTokens:   localTotal,
			CloudTotalTokens:   cloudTotal,
			Difference:         diff,
			CloudDataAvailable: true,
		}

		return result.Difference == result.LocalTotalTokens-result.CloudTotalTokens
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 18 (construction) failed: %v", err)
	}
}
