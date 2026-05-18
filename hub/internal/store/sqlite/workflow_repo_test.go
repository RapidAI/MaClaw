package sqlite

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func newTestWorkflowStore(t *testing.T) *store.Store {
	t.Helper()
	st := newTestStore(t)
	return st
}

func TestUnderstandingSessionCRUD(t *testing.T) {
	st := newTestWorkflowStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	session := &store.UnderstandingSessionRow{
		ID:         "us_1",
		UserID:     "user_1",
		IntentJSON: `{"category":"coding","summary":"build a CRM"}`,
		RoundsJSON: `[{"user_text":"build CRM","assistant_text":"got it","timestamp":"2025-01-01T00:00:00Z"}]`,
		State:      "active",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Save
	if err := st.WorkflowRepo.SaveUnderstandingSession(ctx, session); err != nil {
		t.Fatalf("save understanding session: %v", err)
	}

	// Get active
	got, err := st.WorkflowRepo.GetActiveUnderstandingSession(ctx, "user_1")
	if err != nil {
		t.Fatalf("get active session: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.ID != "us_1" || got.UserID != "user_1" || got.State != "active" {
		t.Fatalf("unexpected session: %+v", got)
	}
	if got.IntentJSON != session.IntentJSON {
		t.Fatalf("intent mismatch: got %q", got.IntentJSON)
	}

	// Update (upsert)
	session.IntentJSON = `{"category":"coding","summary":"build a CRM system"}`
	session.UpdatedAt = now.Add(time.Minute)
	if err := st.WorkflowRepo.SaveUnderstandingSession(ctx, session); err != nil {
		t.Fatalf("update session: %v", err)
	}
	got, _ = st.WorkflowRepo.GetActiveUnderstandingSession(ctx, "user_1")
	if got.IntentJSON != session.IntentJSON {
		t.Fatalf("intent not updated: got %q", got.IntentJSON)
	}

	// Non-active session should not be returned
	session.State = "confirmed"
	session.UpdatedAt = now.Add(2 * time.Minute)
	if err := st.WorkflowRepo.SaveUnderstandingSession(ctx, session); err != nil {
		t.Fatalf("update state: %v", err)
	}
	got, _ = st.WorkflowRepo.GetActiveUnderstandingSession(ctx, "user_1")
	if got != nil {
		t.Fatalf("expected nil for non-active session, got %+v", got)
	}

	// Delete
	if err := st.WorkflowRepo.DeleteUnderstandingSession(ctx, "us_1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
}

func TestWorkflowStateCRUD(t *testing.T) {
	st := newTestWorkflowStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	ws := &store.WorkflowStateRow{
		ID:               "wf_1",
		UserID:           "user_1",
		Type:             "coding",
		TemplateType:     "coding",
		IntentJSON:       `{"category":"coding","summary":"build CRM"}`,
		CurrentPhase:     "requirements",
		PhaseOutputsJSON: `{}`,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// Save
	if err := st.WorkflowRepo.SaveWorkflowState(ctx, ws); err != nil {
		t.Fatalf("save workflow state: %v", err)
	}

	// Get active
	got, err := st.WorkflowRepo.GetActiveWorkflowState(ctx, "user_1")
	if err != nil {
		t.Fatalf("get active workflow: %v", err)
	}
	if got == nil {
		t.Fatal("expected workflow state, got nil")
	}
	if got.ID != "wf_1" || got.CurrentPhase != "requirements" {
		t.Fatalf("unexpected workflow: %+v", got)
	}

	// Update (upsert)
	ws.CurrentPhase = "tech_design"
	ws.PhaseOutputsJSON = `{"requirements":"done"}`
	ws.UpdatedAt = now.Add(time.Minute)
	if err := st.WorkflowRepo.SaveWorkflowState(ctx, ws); err != nil {
		t.Fatalf("update workflow: %v", err)
	}
	got, _ = st.WorkflowRepo.GetActiveWorkflowState(ctx, "user_1")
	if got.CurrentPhase != "tech_design" {
		t.Fatalf("phase not updated: got %q", got.CurrentPhase)
	}
	if got.PhaseOutputsJSON != `{"requirements":"done"}` {
		t.Fatalf("outputs not updated: got %q", got.PhaseOutputsJSON)
	}

	// No workflow for different user
	got, _ = st.WorkflowRepo.GetActiveWorkflowState(ctx, "user_999")
	if got != nil {
		t.Fatalf("expected nil for unknown user, got %+v", got)
	}

	// Delete
	if err := st.WorkflowRepo.DeleteWorkflowState(ctx, "wf_1"); err != nil {
		t.Fatalf("delete workflow: %v", err)
	}
	got, _ = st.WorkflowRepo.GetActiveWorkflowState(ctx, "user_1")
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestWorkflowRepoTenantIsolation(t *testing.T) {
	st := newTestWorkflowStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	ctxA := store.WithTenant(context.Background(), "tenant_a")
	ctxB := store.WithTenant(context.Background(), "tenant_b")

	if err := st.WorkflowRepo.SaveUnderstandingSession(ctxA, &store.UnderstandingSessionRow{ID: "us_a", UserID: "user_1", IntentJSON: `{"tenant":"a"}`, RoundsJSON: `[]`, State: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("save tenant A session: %v", err)
	}
	if err := st.WorkflowRepo.SaveUnderstandingSession(ctxB, &store.UnderstandingSessionRow{ID: "us_b", UserID: "user_1", IntentJSON: `{"tenant":"b"}`, RoundsJSON: `[]`, State: "active", CreatedAt: now, UpdatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("save tenant B session: %v", err)
	}

	gotA, err := st.WorkflowRepo.GetActiveUnderstandingSession(ctxA, "user_1")
	if err != nil {
		t.Fatalf("get tenant A session: %v", err)
	}
	if gotA == nil || gotA.ID != "us_a" || gotA.TenantID != "tenant_a" {
		t.Fatalf("unexpected tenant A session: %+v", gotA)
	}
	gotB, err := st.WorkflowRepo.GetActiveUnderstandingSession(ctxB, "user_1")
	if err != nil {
		t.Fatalf("get tenant B session: %v", err)
	}
	if gotB == nil || gotB.ID != "us_b" || gotB.TenantID != "tenant_b" {
		t.Fatalf("unexpected tenant B session: %+v", gotB)
	}

	if err := st.WorkflowRepo.SaveWorkflowState(ctxA, &store.WorkflowStateRow{ID: "wf_a", UserID: "user_1", Type: "coding", TemplateType: "coding", IntentJSON: `{}`, CurrentPhase: "a", PhaseOutputsJSON: `{}`, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("save tenant A workflow: %v", err)
	}
	if err := st.WorkflowRepo.SaveWorkflowState(ctxB, &store.WorkflowStateRow{ID: "wf_b", UserID: "user_1", Type: "coding", TemplateType: "coding", IntentJSON: `{}`, CurrentPhase: "b", PhaseOutputsJSON: `{}`, CreatedAt: now, UpdatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("save tenant B workflow: %v", err)
	}

	wfA, err := st.WorkflowRepo.GetActiveWorkflowState(ctxA, "user_1")
	if err != nil {
		t.Fatalf("get tenant A workflow: %v", err)
	}
	if wfA == nil || wfA.ID != "wf_a" || wfA.TenantID != "tenant_a" || wfA.CurrentPhase != "a" {
		t.Fatalf("unexpected tenant A workflow: %+v", wfA)
	}
	wfB, err := st.WorkflowRepo.GetActiveWorkflowState(ctxB, "user_1")
	if err != nil {
		t.Fatalf("get tenant B workflow: %v", err)
	}
	if wfB == nil || wfB.ID != "wf_b" || wfB.TenantID != "tenant_b" || wfB.CurrentPhase != "b" {
		t.Fatalf("unexpected tenant B workflow: %+v", wfB)
	}
}

func TestCleanupExpired(t *testing.T) {
	st := newTestWorkflowStore(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Second) // 8 days ago
	recent := time.Now().UTC().Truncate(time.Second)

	// Old confirmed session — should be cleaned
	oldSession := &store.UnderstandingSessionRow{
		ID: "us_old", UserID: "u1", IntentJSON: `{}`, RoundsJSON: `[]`,
		State: "confirmed", CreatedAt: old, UpdatedAt: old,
	}
	// Recent active session — should NOT be cleaned
	activeSession := &store.UnderstandingSessionRow{
		ID: "us_active", UserID: "u2", IntentJSON: `{}`, RoundsJSON: `[]`,
		State: "active", CreatedAt: recent, UpdatedAt: recent,
	}
	// Old cancelled session — should be cleaned
	cancelledSession := &store.UnderstandingSessionRow{
		ID: "us_cancelled", UserID: "u3", IntentJSON: `{}`, RoundsJSON: `[]`,
		State: "cancelled", CreatedAt: old, UpdatedAt: old,
	}

	for _, s := range []*store.UnderstandingSessionRow{oldSession, activeSession, cancelledSession} {
		if err := st.WorkflowRepo.SaveUnderstandingSession(ctx, s); err != nil {
			t.Fatalf("save session %s: %v", s.ID, err)
		}
	}

	// Old workflow state — should be cleaned
	oldWF := &store.WorkflowStateRow{
		ID: "wf_old", UserID: "u1", Type: "coding", TemplateType: "coding",
		IntentJSON: `{}`, CurrentPhase: "done", PhaseOutputsJSON: `{}`,
		CreatedAt: old, UpdatedAt: old,
	}
	// Recent workflow state — should NOT be cleaned
	recentWF := &store.WorkflowStateRow{
		ID: "wf_recent", UserID: "u2", Type: "coding", TemplateType: "coding",
		IntentJSON: `{}`, CurrentPhase: "requirements", PhaseOutputsJSON: `{}`,
		CreatedAt: recent, UpdatedAt: recent,
	}
	for _, ws := range []*store.WorkflowStateRow{oldWF, recentWF} {
		if err := st.WorkflowRepo.SaveWorkflowState(ctx, ws); err != nil {
			t.Fatalf("save workflow %s: %v", ws.ID, err)
		}
	}

	// Cleanup with 7-day threshold
	if err := st.WorkflowRepo.CleanupExpired(ctx, 7*24*time.Hour); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// Old confirmed/cancelled sessions should be gone
	got, _ := st.WorkflowRepo.GetActiveUnderstandingSession(ctx, "u1")
	if got != nil {
		t.Fatal("old confirmed session should have been cleaned")
	}

	// Active session should remain (even though it's recent, it's active so not targeted)
	got, _ = st.WorkflowRepo.GetActiveUnderstandingSession(ctx, "u2")
	if got == nil {
		t.Fatal("active session should remain")
	}

	// Old workflow should be gone
	gotWF, _ := st.WorkflowRepo.GetActiveWorkflowState(ctx, "u1")
	if gotWF != nil {
		t.Fatal("old workflow should have been cleaned")
	}

	// Recent workflow should remain
	gotWF, _ = st.WorkflowRepo.GetActiveWorkflowState(ctx, "u2")
	if gotWF == nil {
		t.Fatal("recent workflow should remain")
	}
}

func TestConcurrentSafety(t *testing.T) {
	st := newTestWorkflowStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// Concurrent writes to different users
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			userID := "user_" + string(rune('A'+idx))
			s := &store.UnderstandingSessionRow{
				ID: "us_" + userID, UserID: userID,
				IntentJSON: `{}`, RoundsJSON: `[]`, State: "active",
				CreatedAt: now, UpdatedAt: now,
			}
			if err := st.WorkflowRepo.SaveUnderstandingSession(ctx, s); err != nil {
				errs <- err
			}
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			userID := "user_" + string(rune('A'+idx))
			ws := &store.WorkflowStateRow{
				ID: "wf_" + userID, UserID: userID,
				Type: "coding", TemplateType: "coding",
				IntentJSON: `{}`, CurrentPhase: "requirements", PhaseOutputsJSON: `{}`,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := st.WorkflowRepo.SaveWorkflowState(ctx, ws); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent error: %v", err)
	}
}
