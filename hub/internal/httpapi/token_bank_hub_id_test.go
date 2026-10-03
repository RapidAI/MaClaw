package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestTokenBankHubIDReportsRegistrationWithoutSecret(t *testing.T) {
	identity, centerSvc, st, _ := newCenterSkillMarketAuthTestServices(t)
	if err := st.System.Set(context.Background(), "server_public_base_url", `{"value":"http://127.0.0.1:9399"}`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.Users.Create(context.Background(), &store.User{
		ID: "user-1", TenantID: store.DefaultTenantID, Email: "user@example.com", Status: "active", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	viewerToken, err := identity.IssueViewerTokenForUser(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/token-bank/hub", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	TokenBankHubIDHandler(identity, centerSvc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hub_secret") || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("response leaked the registration secret: %s", rec.Body.String())
	}
	var body struct {
		HubID string `json:"hub_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.HubID != "hub-1" {
		t.Fatalf("hub_id = %q, want hub-1", body.HubID)
	}

	anon := httptest.NewRequest(http.MethodGet, "/api/token-bank/hub", nil)
	anonRec := httptest.NewRecorder()
	TokenBankHubIDHandler(identity, centerSvc)(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anonRec.Code)
	}
}
