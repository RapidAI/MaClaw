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

func TestTokenBankAutoSettingsRoundTripKeepsThePullEnabled(t *testing.T) {
	identity, _, st, _ := newCenterSkillMarketAuthTestServices(t)
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
	if err := st.System.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid","threshold_micro":5000000}`); err != nil {
		t.Fatal(err)
	}
	handler := TokenBankAutoSettingsHandler(identity, st.System)

	put := httptest.NewRequest(http.MethodPut, "/api/token-bank/auto", strings.NewReader(`{"max_per_withdraw_micro":2500000}`))
	put.Header.Set("Authorization", "Bearer "+viewerToken)
	putRec := httptest.NewRecorder()
	handler(putRec, put)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put status = %d, body %s", putRec.Code, putRec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/token-bank/auto", nil)
	get.Header.Set("Authorization", "Bearer "+viewerToken)
	getRec := httptest.NewRecorder()
	handler(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body %s", getRec.Code, getRec.Body.String())
	}
	var body struct {
		Enabled             bool   `json:"enabled"`
		MaxPerWithdrawMicro int64  `json:"max_per_withdraw_micro"`
		ServiceGroupID      string `json:"service_group_id"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Enabled || body.MaxPerWithdrawMicro != 2_500_000 || body.ServiceGroupID != "paid" {
		t.Fatalf("settings = %+v", body)
	}

	clear := httptest.NewRequest(http.MethodPut, "/api/token-bank/auto", strings.NewReader(`{"max_per_withdraw_micro":0}`))
	clear.Header.Set("Authorization", "Bearer "+viewerToken)
	clearRec := httptest.NewRecorder()
	handler(clearRec, clear)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body %s", clearRec.Code, clearRec.Body.String())
	}
	stored, err := st.System.Get(context.Background(), "token_bank_auto_withdraw")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "max_per_withdraw_micro") || !strings.Contains(stored, `"enabled":true`) || !strings.Contains(stored, `"service_group_id":"paid"`) || !strings.Contains(stored, `"threshold_micro":5000000`) {
		t.Fatalf("stored = %s", stored)
	}

	if err := st.System.Set(context.Background(), "token_bank_auto_withdraw", "null"); err != nil {
		t.Fatal(err)
	}
	putNull := httptest.NewRequest(http.MethodPut, "/api/token-bank/auto", strings.NewReader(`{"max_per_withdraw_micro":1000000}`))
	putNull.Header.Set("Authorization", "Bearer "+viewerToken)
	putNullRec := httptest.NewRecorder()
	handler(putNullRec, putNull)
	if putNullRec.Code != http.StatusOK {
		t.Fatalf("null document status = %d, body %s", putNullRec.Code, putNullRec.Body.String())
	}
	stored, err = st.System.Get(context.Background(), "token_bank_auto_withdraw")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, `"enabled":true`) || !strings.Contains(stored, `"max_per_withdraw_micro":1000000`) {
		t.Fatalf("null document stored = %s", stored)
	}

	anon := httptest.NewRequest(http.MethodPut, "/api/token-bank/auto", strings.NewReader(`{"max_per_withdraw_micro":1}`))
	anonRec := httptest.NewRecorder()
	handler(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anonRec.Code)
	}
}
