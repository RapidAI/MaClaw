package ve

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaPushReceiver_HandleQuotaUpdate_Success(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-001"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.Start()
	defer receiver.Stop()

	msg := QuotaPushMessage{
		Quota:     50,
		HubID:     hubID,
		Timestamp: time.Now().UnixMilli(),
	}

	err := receiver.HandleQuotaUpdate(msg)
	if err != nil {
		t.Fatalf("HandleQuotaUpdate() error = %v", err)
	}

	// Verify quota was persisted
	q, err := store.LoadQuota()
	if err != nil {
		t.Fatalf("LoadQuota() error = %v", err)
	}
	if q != 50 {
		t.Errorf("LoadQuota() = %d, want 50", q)
	}
}

func TestQuotaPushReceiver_HandleQuotaUpdate_Within5Seconds(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-002"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.Start()
	defer receiver.Stop()

	msg := QuotaPushMessage{
		Quota:     100,
		HubID:     hubID,
		Timestamp: time.Now().UnixMilli(),
	}

	start := time.Now()
	err := receiver.HandleQuotaUpdate(msg)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("HandleQuotaUpdate() error = %v", err)
	}

	// Must complete within 5 seconds
	if elapsed > 5*time.Second {
		t.Errorf("HandleQuotaUpdate() took %s, must complete within 5s", elapsed)
	}
}

func TestQuotaPushReceiver_HandleQuotaUpdate_InvalidRange(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-003"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.Start()
	defer receiver.Stop()

	tests := []struct {
		name  string
		quota int
	}{
		{"negative", -1},
		{"too_large", 10001},
		{"way_too_large", 99999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := QuotaPushMessage{
				Quota:     tt.quota,
				HubID:     hubID,
				Timestamp: time.Now().UnixMilli(),
			}
			err := receiver.HandleQuotaUpdate(msg)
			if err == nil {
				t.Errorf("HandleQuotaUpdate(quota=%d) should fail", tt.quota)
			}
		})
	}
}

func TestQuotaPushReceiver_HandleQuotaUpdate_HubIDMismatch(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-004"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.Start()
	defer receiver.Stop()

	msg := QuotaPushMessage{
		Quota:     50,
		HubID:     "different-hub-id",
		Timestamp: time.Now().UnixMilli(),
	}

	err := receiver.HandleQuotaUpdate(msg)
	if err == nil {
		t.Error("HandleQuotaUpdate() with mismatched hub_id should fail")
	}
}

func TestQuotaPushReceiver_HandleQuotaUpdate_EmptyHubID(t *testing.T) {
	// Empty hub_id in message is allowed (backward compat with older HubCenter)
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-005"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.Start()
	defer receiver.Stop()

	msg := QuotaPushMessage{
		Quota:     75,
		HubID:     "", // empty is OK
		Timestamp: time.Now().UnixMilli(),
	}

	err := receiver.HandleQuotaUpdate(msg)
	if err != nil {
		t.Fatalf("HandleQuotaUpdate() with empty hub_id should succeed: %v", err)
	}

	q, err := store.LoadQuota()
	if err != nil {
		t.Fatalf("LoadQuota() error = %v", err)
	}
	if q != 75 {
		t.Errorf("LoadQuota() = %d, want 75", q)
	}
}

func TestQuotaPushReceiver_HandleQuotaUpdate_OverwritesPrevious(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-006"

	store := NewQuotaStore(key, hubID, fp)
	// Pre-save a quota
	if err := store.SaveQuota(30); err != nil {
		t.Fatal(err)
	}

	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.Start()
	defer receiver.Stop()

	msg := QuotaPushMessage{
		Quota:     60,
		HubID:     hubID,
		Timestamp: time.Now().UnixMilli(),
	}

	err := receiver.HandleQuotaUpdate(msg)
	if err != nil {
		t.Fatalf("HandleQuotaUpdate() error = %v", err)
	}

	// Verify new quota replaced old
	store.InvalidateCache()
	q, err := store.LoadQuota()
	if err != nil {
		t.Fatalf("LoadQuota() error = %v", err)
	}
	if q != 60 {
		t.Errorf("LoadQuota() = %d, want 60 (should overwrite previous 30)", q)
	}
}

func TestQuotaPushReceiver_HandleQuotaUpdate_BoundaryValues(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-007"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.Start()
	defer receiver.Stop()

	// Test boundary values: 0 and 10000
	for _, quota := range []int{0, 10000} {
		msg := QuotaPushMessage{
			Quota:     quota,
			HubID:     hubID,
			Timestamp: time.Now().UnixMilli(),
		}
		err := receiver.HandleQuotaUpdate(msg)
		if err != nil {
			t.Errorf("HandleQuotaUpdate(quota=%d) should succeed: %v", quota, err)
		}

		store.InvalidateCache()
		q, err := store.LoadQuota()
		if err != nil {
			t.Fatalf("LoadQuota() after quota=%d: %v", quota, err)
		}
		if q != quota {
			t.Errorf("LoadQuota() = %d, want %d", q, quota)
		}
	}
}

func TestQuotaPushReceiver_RetryScheduled_OnFailure(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "nonexistent_subdir", "deep", "quota.enc") // will fail to write
	key := testKeyMat()
	hubID := "hub-push-008"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	// Use short retry interval for testing
	receiver.retryInterval = 50 * time.Millisecond
	receiver.Start()
	defer receiver.Stop()

	msg := QuotaPushMessage{
		Quota:     42,
		HubID:     hubID,
		Timestamp: time.Now().UnixMilli(),
	}

	err := receiver.HandleQuotaUpdate(msg)
	if err == nil {
		t.Fatal("HandleQuotaUpdate() should fail when file path is invalid")
	}

	// Verify retry is scheduled
	if !receiver.HasPendingRetry() {
		t.Error("expected pending retry after failure")
	}
}

func TestQuotaPushReceiver_RetryExhausted(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "nonexistent_subdir", "deep", "quota.enc") // will fail to write
	key := testKeyMat()
	hubID := "hub-push-009"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	// Use very short intervals for testing
	receiver.retryInterval = 10 * time.Millisecond
	receiver.maxRetries = 2
	receiver.Start()
	defer receiver.Stop()

	msg := QuotaPushMessage{
		Quota:     42,
		HubID:     hubID,
		Timestamp: time.Now().UnixMilli(),
	}

	_ = receiver.HandleQuotaUpdate(msg)

	// Wait for retries to exhaust (2 retries × 10ms + buffer)
	time.Sleep(100 * time.Millisecond)

	// After exhaustion, no more pending retry
	if receiver.HasPendingRetry() {
		t.Error("expected no pending retry after exhaustion")
	}
}

func TestQuotaPushReceiver_NewUpdateCancelsOldRetry(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-push-010"

	store := NewQuotaStore(key, hubID, fp)
	receiver := NewQuotaPushReceiver(store, hubID)
	receiver.retryInterval = 1 * time.Second // long enough to not fire during test
	receiver.Start()
	defer receiver.Stop()

	// First update succeeds
	msg1 := QuotaPushMessage{Quota: 10, HubID: hubID, Timestamp: time.Now().UnixMilli()}
	if err := receiver.HandleQuotaUpdate(msg1); err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	// Second update also succeeds — should clear any pending retry
	msg2 := QuotaPushMessage{Quota: 20, HubID: hubID, Timestamp: time.Now().UnixMilli()}
	if err := receiver.HandleQuotaUpdate(msg2); err != nil {
		t.Fatalf("second update failed: %v", err)
	}

	if receiver.HasPendingRetry() {
		t.Error("successful update should clear pending retry")
	}

	store.InvalidateCache()
	q, err := store.LoadQuota()
	if err != nil {
		t.Fatal(err)
	}
	if q != 20 {
		t.Errorf("LoadQuota() = %d, want 20", q)
	}
}
