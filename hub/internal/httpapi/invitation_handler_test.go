package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// stubEmailInviteRepo is a minimal in-memory implementation of store.EmailInviteRepository
// used to test the fixed handlers.
type stubEmailInviteRepo struct {
	items []*store.EmailInvite
}

func (r *stubEmailInviteRepo) Create(_ context.Context, item *store.EmailInvite) error {
	r.items = append(r.items, item)
	return nil
}

func (r *stubEmailInviteRepo) List(_ context.Context) ([]*store.EmailInvite, error) {
	return r.items, nil
}

func (r *stubEmailInviteRepo) ListByTenant(_ context.Context, tenantID string) ([]*store.EmailInvite, error) {
	out := make([]*store.EmailInvite, 0, len(r.items))
	for _, item := range r.items {
		if item != nil && item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *stubEmailInviteRepo) GetByID(_ context.Context, id string) (*store.EmailInvite, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return nil, nil
}

func (r *stubEmailInviteRepo) DeleteByID(_ context.Context, id string) error {
	for i, item := range r.items {
		if item.ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			return nil
		}
	}
	return nil
}

// TestBugCondition_InviteListAPI verifies the bug condition exists on unfixed code.
//
// **Validates: Requirements 1.1, 1.2, 1.3**
//
// Bug Condition: The invite list API returns 410 Gone (DeprecatedEmailInviteHandler),
// and there is no DELETE /api/admin/invites/{id} route at all.
//
// This test encodes the EXPECTED (correct) behavior:
//   - GET /api/admin/invites should return 200 with a JSON array of invites
//   - Each invite object should have lowercase field names (email, role, status)
//   - DELETE /api/admin/invites/{id} should exist and return 200
//
// After the fix, this test PASSES — confirming the bug is resolved.
func TestBugCondition_InviteListAPI(t *testing.T) {
	now := time.Now()
	repo := &stubEmailInviteRepo{
		items: []*store.EmailInvite{
			{
				ID:        "ei_test1",
				Email:     "alice@example.com",
				Role:      "editor",
				Status:    "pending",
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "ei_test2",
				Email:     "bob@example.com",
				Role:      "viewer",
				Status:    "accepted",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	// Build a test mux using the FIXED handlers.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/invites", ListEmailInvitesHandler(repo))
	mux.HandleFunc("POST /api/admin/invites", CreateEmailInviteHandler(repo))
	mux.HandleFunc("DELETE /api/admin/invites/{id}", DeleteEmailInviteHandler(repo))

	t.Run("GET_invites_should_return_200_not_410", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/invites", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("GET /api/admin/invites: expected status 200, got %d; body=%s",
				rr.Code, rr.Body.String())
		}

		// If we got 200, verify the response contains lowercase JSON field names
		var body map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET /api/admin/invites: failed to parse JSON response: %v", err)
		}

		invites, ok := body["invites"].([]any)
		if !ok {
			t.Fatalf("GET /api/admin/invites: response missing 'invites' array, got: %s",
				rr.Body.String())
		}

		if len(invites) == 0 {
			t.Fatal("GET /api/admin/invites: expected non-empty invites array")
		}

		// Verify each invite has lowercase field names
		for i, item := range invites {
			inv, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("invite[%d]: expected object, got %T", i, item)
			}
			for _, field := range []string{"email", "role", "status"} {
				if _, exists := inv[field]; !exists {
					t.Errorf("invite[%d]: missing lowercase field %q (got keys: %v)",
						i, field, keys(inv))
				}
			}
		}
	})

	t.Run("DELETE_invite_route_should_exist_and_return_200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/invites/ei_test1", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code == http.StatusNotFound || rr.Code == http.StatusMethodNotAllowed {
			t.Fatalf("DELETE /api/admin/invites/ei_test1: route does not exist, got status %d; "+
				"expected a registered DELETE handler returning 200", rr.Code)
		}

		if rr.Code != http.StatusOK {
			t.Fatalf("DELETE /api/admin/invites/ei_test1: expected status 200, got %d; body=%s",
				rr.Code, rr.Body.String())
		}
	})
}

// keys returns the keys of a map for diagnostic output.
func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
