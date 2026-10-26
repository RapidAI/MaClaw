package guiapp

import (
	"errors"
	"testing"
	"time"
)

// An unreachable Hub used to be re-probed every few seconds by the sidebar
// refresh, token-usage events and provider reads, each holding a 30s timeout.
// The backoff must grow and then cap so an offline Hub stops competing with
// startup and normal work for CPU and sockets.
func TestHubServiceStatusBackoffGrowsAndCaps(t *testing.T) {
	if got := hubServiceStatusBackoff(0); got != 0 {
		t.Fatalf("backoff(0) = %v, want 0 (no failure yet)", got)
	}
	if got := hubServiceStatusBackoff(1); got != hubServiceStatusBaseBackoff {
		t.Fatalf("backoff(1) = %v, want %v", got, hubServiceStatusBaseBackoff)
	}
	if got := hubServiceStatusBackoff(2); got != 2*hubServiceStatusBaseBackoff {
		t.Fatalf("backoff(2) = %v, want %v", got, 2*hubServiceStatusBaseBackoff)
	}
	// Growth must stop at the cap rather than overflowing into absurd waits.
	for _, failures := range []int{10, 64, 1000} {
		if got := hubServiceStatusBackoff(failures); got != hubServiceStatusMaxBackoff {
			t.Fatalf("backoff(%d) = %v, want cap %v", failures, got, hubServiceStatusMaxBackoff)
		}
	}
}

// A failure on one Hub identity must not suppress probes for a different
// account or endpoint: their health is unrelated, and a user who signs in to a
// working Hub after a failed one must not be locked out of status updates.
func TestHubServiceStatusBackoffIsPerIdentity(t *testing.T) {
	clearHubServiceStatusCache()
	t.Cleanup(clearHubServiceStatusCache)

	noteHubServiceStatusFailure("https://hub.example", "token-a", errors.New("dead"))

	if remaining := hubServiceStatusBackoffRemaining("https://hub.example", "token-a"); remaining <= 0 {
		t.Fatal("same identity should be suppressed after a failure")
	}
	if remaining := hubServiceStatusBackoffRemaining("https://hub.example", "token-b"); remaining != 0 {
		t.Fatalf("different token must not be suppressed, got %v", remaining)
	}
	if remaining := hubServiceStatusBackoffRemaining("https://other.example", "token-a"); remaining != 0 {
		t.Fatalf("different hub must not be suppressed, got %v", remaining)
	}
}

// A successful probe clears the streak so the next failure starts from the
// base backoff instead of inheriting an exhausted one. Every success path ends
// at storeHubServiceStatusCache, which is where the reset lives — an explicit
// refresh or redeem must not leave the breaker open.
func TestHubServiceStatusSuccessResetsBackoff(t *testing.T) {
	clearHubServiceStatusCache()
	t.Cleanup(clearHubServiceStatusCache)

	for i := 0; i < 5; i++ {
		noteHubServiceStatusFailure("https://hub.example", "token-a", errors.New("dead"))
	}
	if remaining := hubServiceStatusBackoffRemaining("https://hub.example", "token-a"); remaining <= 2*hubServiceStatusBaseBackoff {
		t.Fatalf("after 5 failures remaining = %v, expected a much longer wait", remaining)
	}

	storeHubServiceStatusCache("https://hub.example", "token-a", HubLLMServiceStatus{})
	if remaining := hubServiceStatusBackoffRemaining("https://hub.example", "token-a"); remaining != 0 {
		t.Fatalf("storing a successful status should clear the backoff, got %v", remaining)
	}
}

// The backoff window must expire on its own so a Hub that comes back online is
// picked up without a user action.
func TestHubServiceStatusBackoffExpires(t *testing.T) {
	clearHubServiceStatusCache()
	t.Cleanup(clearHubServiceStatusCache)

	noteHubServiceStatusFailure("https://hub.example", "token-a", errors.New("dead"))
	hubServiceStatusCache.mu.Lock()
	// Pretend the failure happened long ago.
	hubServiceStatusCache.failedAt = time.Now().Add(-hubServiceStatusMaxBackoff - time.Second)
	hubServiceStatusCache.mu.Unlock()

	if remaining := hubServiceStatusBackoffRemaining("https://hub.example", "token-a"); remaining != 0 {
		t.Fatalf("expired backoff should allow a probe, got %v", remaining)
	}
}
