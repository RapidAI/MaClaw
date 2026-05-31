package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// =============================================================================
// Focused HTTP-level handler tests for the confirm endpoints (Task 3.10).
//
// These exercise handleConfirm and handleListPendingConfirmations through the
// registered RuntimeAPI routes (so r.PathValue works), verifying that the
// endpoints return real ConfirmationTracker / ConfirmationStore results instead
// of NOT_IMPLEMENTED, and that ConfirmationTracker.Confirm sentinel errors map
// to the correct HTTP status codes.
//
// Validates: Requirements 2.10
// =============================================================================

// testAuthInject is a minimal auth middleware for tests: it injects a fixed
// authenticated user ID into the request context, mirroring what AuthMiddleware
// does after validating a Bearer token.
func testAuthInject(userID string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r = setUserIDInContext(r, userID)
			next(w, r)
		}
	}
}

// newConfirmTestServer builds a RuntimeAPI wired with the runtime mock stores,
// registers its routes on a mux with an auth middleware that authenticates as
// userID, and seeds the confirmation store with the given records.
func newConfirmTestServer(userID string, seed ...*Confirmation) (*httptest.Server, *rtConfirmationStore) {
	instStore := newRTInstanceStore()
	auditStore := newRTAuditStore()
	confirmStore := newRTConfirmationStore()

	for _, c := range seed {
		_ = confirmStore.Create(context.Background(), c)
	}

	api := NewRuntimeAPI(nil, instStore, auditStore, &FormValidator{}, &rtWorkflowStore{})
	// handleConfirm / handleListPendingConfirmations resolve the confirmation
	// store through the directory service.
	api.SetDirectoryService(NewDirectoryService(instStore, confirmStore, nil))

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, testAuthInject(userID))

	return httptest.NewServer(mux), confirmStore
}

func TestHandleConfirm_Success(t *testing.T) {
	const userID = "executor-1"
	conf := &Confirmation{
		ID:          "conf-success",
		InstanceID:  "inst-1",
		RecipientID: userID,
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmPending,
	}
	srv, store := newConfirmTestServer(userID, conf)
	defer srv.Close()

	body := strings.NewReader(`{"notes":"done processing"}`)
	resp, err := http.Post(srv.URL+"/api/v1/confirmations/conf-success/confirm", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Errorf("expected ok=true, got %v", out["ok"])
	}
	if status, _ := out["status"].(string); status != string(ConfirmConfirmed) {
		t.Errorf("expected status %q, got %v", ConfirmConfirmed, out["status"])
	}

	// The real ConfirmationTracker.Confirm path must have updated the store.
	updated, _ := store.Get(context.Background(), "conf-success")
	if updated == nil || updated.Status != ConfirmConfirmed {
		t.Errorf("expected store status confirmed, got %+v", updated)
	}
	if updated != nil && updated.Notes != "done processing" {
		t.Errorf("expected notes persisted, got %q", updated.Notes)
	}
}

func TestHandleConfirm_NotFound(t *testing.T) {
	srv, _ := newConfirmTestServer("user-1") // no confirmations seeded
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/confirmations/does-not-exist/confirm", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for ErrConfirmationNotFound, got %d", resp.StatusCode)
	}
}

func TestHandleConfirm_RecipientMismatch(t *testing.T) {
	// Confirmation belongs to someone else; the authenticated caller is not the recipient.
	conf := &Confirmation{
		ID:          "conf-mismatch",
		InstanceID:  "inst-1",
		RecipientID: "other-user",
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmPending,
	}
	srv, _ := newConfirmTestServer("intruder", conf)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/confirmations/conf-mismatch/confirm", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for ErrRecipientMismatch, got %d", resp.StatusCode)
	}
}

func TestHandleConfirm_AlreadyConfirmed(t *testing.T) {
	const userID = "executor-1"
	conf := &Confirmation{
		ID:          "conf-done",
		InstanceID:  "inst-1",
		RecipientID: userID,
		Type:        ConfirmTypeExecutor,
		Status:      ConfirmConfirmed, // not pending
	}
	srv, _ := newConfirmTestServer(userID, conf)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/confirmations/conf-done/confirm", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for ErrAlreadyConfirmed, got %d", resp.StatusCode)
	}
}

func TestHandleListPendingConfirmations_ReturnsStoreResults(t *testing.T) {
	const userID = "executor-1"
	// Two pending for the user, one pending for another user, one already confirmed.
	srv, _ := newConfirmTestServer(userID,
		&Confirmation{ID: "c1", InstanceID: "inst-1", RecipientID: userID, Type: ConfirmTypeExecutor, Status: ConfirmPending},
		&Confirmation{ID: "c2", InstanceID: "inst-2", RecipientID: userID, Type: ConfirmTypeNotifier, Status: ConfirmPending},
		&Confirmation{ID: "c3", InstanceID: "inst-3", RecipientID: "other-user", Type: ConfirmTypeExecutor, Status: ConfirmPending},
		&Confirmation{ID: "c4", InstanceID: "inst-4", RecipientID: userID, Type: ConfirmTypeExecutor, Status: ConfirmConfirmed},
	)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/confirmations/pending")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out struct {
		Confirmations []Confirmation `json:"confirmations"`
		Total         int            `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Only the two pending confirmations for this user should be returned.
	if out.Total != 2 {
		t.Fatalf("expected total=2, got %d", out.Total)
	}
	if len(out.Confirmations) != 2 {
		t.Fatalf("expected 2 confirmations, got %d", len(out.Confirmations))
	}
	for _, c := range out.Confirmations {
		if c.RecipientID != userID {
			t.Errorf("expected recipient %q, got %q", userID, c.RecipientID)
		}
		if c.Status != ConfirmPending {
			t.Errorf("expected pending status, got %q", c.Status)
		}
	}
}

func TestHandleListPendingConfirmations_EmptyReturnsEmptyArray(t *testing.T) {
	srv, _ := newConfirmTestServer("lonely-user") // no confirmations
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/confirmations/pending")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out struct {
		Confirmations []Confirmation `json:"confirmations"`
		Total         int            `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Total != 0 {
		t.Errorf("expected total=0, got %d", out.Total)
	}
	// confirmations should be a non-nil empty array, not null.
	if out.Confirmations == nil {
		t.Errorf("expected non-nil empty confirmations array")
	}
}
