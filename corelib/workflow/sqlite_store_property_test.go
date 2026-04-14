package workflow

import (
	"testing"
	"testing/quick"
	"time"

	_ "modernc.org/sqlite"
)

func newMemoryStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore(:memory:) failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// Feature: maclaw-agent-workflow, Property 13: 持久化往返一致性
// For any valid WorkflowState and UnderstandingSession, Save then Load
// returns equivalent data.
// **Validates: Requirements 7.1, 7.2, 7.3, 7.4**
func TestProperty13_PersistenceRoundTrip(t *testing.T) {
	store := newMemoryStore(t)

	// 13a: WorkflowState round-trip
	workflowTypes := []WorkflowType{
		WorkflowCoding, WorkflowProductDesign, WorkflowInnovation,
		WorkflowBusinessPlan, WorkflowTesting,
	}
	statuses := []WorkflowStatus{WorkflowActive, WorkflowCompleted, WorkflowCancelled}

	f1 := func(typeIdx, statusIdx uint8, summary string) bool {
		if summary == "" {
			summary = "default"
		}
		wt := workflowTypes[int(typeIdx)%len(workflowTypes)]
		status := statuses[int(statusIdx)%len(statuses)]
		userID := "user_wf_" + summary[:min(len(summary), 8)]

		now := time.Now().Truncate(time.Second)
		state := &WorkflowState{
			ID:           "wf-" + userID,
			UserID:       userID,
			Type:         wt,
			Intent:       StructuredIntent{Category: wt, Summary: summary, Goals: []string{"g1"}, Constraints: []string{"c1"}},
			CurrentPhase: "phase1",
			PhaseIndex:   2,
			PhaseOutputs: map[string]string{"p0": "output0", "p1": "output1"},
			GateResults:  make(map[string]*QualityGateResult),
			Status:       status,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if err := store.SaveWorkflowState(state); err != nil {
			t.Logf("SaveWorkflowState error: %v", err)
			return false
		}
		loaded, err := store.LoadWorkflowState(userID)
		if err != nil {
			t.Logf("LoadWorkflowState error: %v", err)
			return false
		}
		if loaded == nil {
			t.Log("LoadWorkflowState returned nil")
			return false
		}

		if loaded.ID != state.ID || loaded.UserID != state.UserID {
			return false
		}
		if loaded.Type != state.Type || loaded.Status != state.Status {
			return false
		}
		if loaded.CurrentPhase != state.CurrentPhase || loaded.PhaseIndex != state.PhaseIndex {
			return false
		}
		if loaded.Intent.Summary != state.Intent.Summary {
			return false
		}
		if len(loaded.PhaseOutputs) != len(state.PhaseOutputs) {
			return false
		}
		for k, v := range state.PhaseOutputs {
			if loaded.PhaseOutputs[k] != v {
				return false
			}
		}
		// Clean up for next iteration
		_ = store.DeleteWorkflowState(state.ID)
		return true
	}
	if err := quick.Check(f1, quickConfig()); err != nil {
		t.Errorf("Property 13a (WorkflowState round-trip) failed: %v", err)
	}

	// 13b: UnderstandingSession round-trip
	understandingStates := []UnderstandingState{
		UnderstandingActive, UnderstandingConfirmed, UnderstandingCancelled, UnderstandingExpired,
	}

	f2 := func(stateIdx uint8, summary string) bool {
		if summary == "" {
			summary = "default"
		}
		us := understandingStates[int(stateIdx)%len(understandingStates)]
		userID := "user_iu_" + summary[:min(len(summary), 8)]

		now := time.Now().Truncate(time.Second)
		sess := &UnderstandingSession{
			ID:     "iu-" + userID,
			UserID: userID,
			Intent: StructuredIntent{Category: WorkflowCoding, Summary: summary},
			Rounds: []UnderstandingRound{
				{UserText: "hello", AssistantText: "hi", Timestamp: now},
			},
			State:     us,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := store.SaveUnderstandingSession(sess); err != nil {
			t.Logf("SaveUnderstandingSession error: %v", err)
			return false
		}
		loaded, err := store.LoadUnderstandingSession(userID)
		if err != nil {
			t.Logf("LoadUnderstandingSession error: %v", err)
			return false
		}
		if loaded == nil {
			t.Log("LoadUnderstandingSession returned nil")
			return false
		}

		if loaded.ID != sess.ID || loaded.UserID != sess.UserID {
			return false
		}
		if loaded.State != sess.State {
			return false
		}
		if loaded.Intent.Summary != sess.Intent.Summary {
			return false
		}
		if len(loaded.Rounds) != len(sess.Rounds) {
			return false
		}
		// Clean up
		_ = store.DeleteUnderstandingSession(userID)
		return true
	}
	if err := quick.Check(f2, quickConfig()); err != nil {
		t.Errorf("Property 13b (UnderstandingSession round-trip) failed: %v", err)
	}
}

// Feature: maclaw-agent-workflow, Property 14: 过期记录清理正确性
// For any set of WorkflowStates, CleanupExpired correctly removes old
// completed/cancelled records and preserves active records.
// **Validates: Requirements 7.5**
func TestProperty14_CleanupExpiredCorrectness(t *testing.T) {
	store := newMemoryStore(t)

	now := time.Now().Truncate(time.Second)
	oldTime := now.Add(-8 * 24 * time.Hour) // 8 days ago
	recentTime := now.Add(-1 * 24 * time.Hour) // 1 day ago

	// Insert records with various statuses and ages
	records := []struct {
		id        string
		userID    string
		status    WorkflowStatus
		updatedAt time.Time
		shouldSurvive bool
	}{
		{"wf-old-completed", "u1", WorkflowCompleted, oldTime, false},
		{"wf-old-cancelled", "u2", WorkflowCancelled, oldTime, false},
		{"wf-old-active", "u3", WorkflowActive, oldTime, true},          // active always preserved
		{"wf-recent-completed", "u4", WorkflowCompleted, recentTime, true}, // recent preserved
		{"wf-recent-active", "u5", WorkflowActive, recentTime, true},
	}

	for _, r := range records {
		state := &WorkflowState{
			ID:           r.id,
			UserID:       r.userID,
			Type:         WorkflowCoding,
			Intent:       StructuredIntent{Category: WorkflowCoding},
			CurrentPhase: "p1",
			PhaseOutputs: make(map[string]string),
			GateResults:  make(map[string]*QualityGateResult),
			Status:       r.status,
			CreatedAt:    r.updatedAt,
			UpdatedAt:    r.updatedAt,
		}
		if err := store.SaveWorkflowState(state); err != nil {
			t.Fatalf("SaveWorkflowState(%s) failed: %v", r.id, err)
		}
	}

	// Cleanup records older than 7 days
	if err := store.CleanupExpired(7 * 24 * time.Hour); err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}

	// Verify survivors
	for _, r := range records {
		loaded, err := store.LoadWorkflowState(r.userID)
		if err != nil {
			t.Fatalf("LoadWorkflowState(%s) failed: %v", r.userID, err)
		}
		if r.shouldSurvive && loaded == nil {
			t.Errorf("record %s (status=%s, age=%v) should have survived cleanup but was deleted",
				r.id, r.status, now.Sub(r.updatedAt))
		}
		if !r.shouldSurvive && loaded != nil {
			t.Errorf("record %s (status=%s, age=%v) should have been cleaned up but still exists",
				r.id, r.status, now.Sub(r.updatedAt))
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
