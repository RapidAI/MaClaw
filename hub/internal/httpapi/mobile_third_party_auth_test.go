package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func clearThirdPartyAuthSessionsForTest(t *testing.T) {
	t.Helper()
	thirdPartyAuthSessions.Lock()
	thirdPartyAuthSessions.sessions = make(map[string]*thirdPartyAuthSession)
	thirdPartyAuthSessions.Unlock()
	t.Cleanup(func() {
		thirdPartyAuthSessions.Lock()
		thirdPartyAuthSessions.sessions = make(map[string]*thirdPartyAuthSession)
		thirdPartyAuthSessions.Unlock()
	})
}

type tpCreateResp struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	QRPayload string `json:"qr_payload"`
	PollToken string `json:"poll_token"`
	ExpiresAt string `json:"expires_at"`
}

func tpCreateSession(t *testing.T, clientID string) tpCreateResp {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"client_id": clientID, "name": "RapidEditor AI"})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions", strings.NewReader(string(body)))
	req.Host = "hub.maclaw.top"
	rec := httptest.NewRecorder()
	ThirdPartyQRSessionHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s, want 201", rec.Code, rec.Body.String())
	}
	var out tpCreateResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	return out
}

func TestThirdPartyQRCreateSession(t *testing.T) {
	clearThirdPartyAuthSessionsForTest(t)
	out := tpCreateSession(t, "rapideditor-ai")

	if out.Status != "pending" {
		t.Fatalf("status=%q, want pending", out.Status)
	}
	if !strings.HasPrefix(out.SessionID, "maqr_") {
		t.Fatalf("session_id=%q, want maqr_ prefix", out.SessionID)
	}
	if out.PollToken == "" {
		t.Fatal("poll_token is empty")
	}
	var payload struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
		HubURL    string `json:"hub_url"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(out.QRPayload), &payload); err != nil {
		t.Fatalf("decode qr_payload: %v", err)
	}
	if payload.Type != thirdPartyAuthQRType {
		t.Fatalf("qr type=%q, want %q", payload.Type, thirdPartyAuthQRType)
	}
	if payload.SessionID != out.SessionID {
		t.Fatalf("qr session_id=%q, want %q", payload.SessionID, out.SessionID)
	}
	if !strings.HasPrefix(payload.HubURL, "https://") {
		t.Fatalf("qr hub_url=%q, want https", payload.HubURL)
	}
	if payload.ExpiresAt == "" {
		t.Fatal("qr expires_at empty")
	}
}

func TestThirdPartyQRCreateRejectsShortClientID(t *testing.T) {
	clearThirdPartyAuthSessionsForTest(t)
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions", strings.NewReader(`{"client_id":"a"}`))
	req.Host = "hub.maclaw.top"
	rec := httptest.NewRecorder()
	ThirdPartyQRSessionHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestThirdPartyQRCreateUpgradesForwardedHTTP(t *testing.T) {
	clearThirdPartyAuthSessionsForTest(t)
	body := `{"client_id":"rapideditor-ai"}`
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions", strings.NewReader(body))
	req.Host = "hub.maclaw.top"
	req.Header.Set("X-Forwarded-Proto", "http")
	rec := httptest.NewRecorder()
	ThirdPartyQRSessionHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out tpCreateResp
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(out.QRPayload, "https://hub.maclaw.top") {
		t.Fatalf("qr_payload=%q, want upgraded https hub URL", out.QRPayload)
	}
}

// Full happy path: create -> poll pending -> phone authorize -> poll token (once)
func TestThirdPartyQRFlowEndToEnd(t *testing.T) {
	clearThirdPartyAuthSessionsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	created := tpCreateSession(t, "rapideditor-ai")

	poll := func() (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{"session_id": created.SessionID, "poll_token": created.PollToken})
		req := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions/token", strings.NewReader(string(body)))
		rec := httptest.NewRecorder()
		ThirdPartyQRTokenHandler().ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, out := poll()
	if code != http.StatusOK || out["status"] != "pending" {
		t.Fatalf("initial poll=%d %#v, want pending", code, out)
	}

	phoneToken, _ := issueViewerToken(t, identity, "phone:19900007777")
	authBody, _ := json.Marshal(map[string]any{"qr_payload": created.QRPayload})
	authReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions/authorize", strings.NewReader(string(authBody)))
	authReq.Header.Set("Authorization", "Bearer "+phoneToken)
	authRec := httptest.NewRecorder()
	ThirdPartyQRAuthorizeHandler(identity).ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", authRec.Code, authRec.Body.String())
	}

	code, out = poll()
	if code != http.StatusOK || out["status"] != "authorized" {
		t.Fatalf("authorized poll=%d %#v", code, out)
	}
	token, _ := out["access_token"].(string)
	if token == "" {
		t.Fatal("no access_token delivered")
	}
	// The minted token must authenticate as the scanning user.
	principal, err := identity.AuthenticateViewer(authReq.Context(), token)
	if err != nil || principal == nil {
		t.Fatalf("delivered token does not authenticate: %v", err)
	}
	if principal.Email != "phone:19900007777" {
		t.Fatalf("delivered token owner=%q, want scanning user", principal.Email)
	}

	// Second poll must NOT leak the token again.
	_, out2 := poll()
	if out2["status"] != "delivered" || out2["access_token"] != nil {
		t.Fatalf("replay poll=%#v, want delivered with no token", out2)
	}
}

func TestThirdPartyQRAuthorizeRequiresViewerAuth(t *testing.T) {
	clearThirdPartyAuthSessionsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	created := tpCreateSession(t, "rapideditor-ai")
	authBody, _ := json.Marshal(map[string]any{"qr_payload": created.QRPayload})
	authReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions/authorize", strings.NewReader(string(authBody)))
	authReq.Header.Set("Authorization", "Bearer invalid")
	rec := httptest.NewRecorder()
	ThirdPartyQRAuthorizeHandler(identity).ServeHTTP(rec, authReq)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
}

func TestThirdPartyQRTokenRejectsWrongPollToken(t *testing.T) {
	clearThirdPartyAuthSessionsForTest(t)
	created := tpCreateSession(t, "rapideditor-ai")
	body, _ := json.Marshal(map[string]any{"session_id": created.SessionID, "poll_token": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions/token", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	ThirdPartyQRTokenHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
}

func TestThirdPartyQRTokenUnknownSessionIsExpired(t *testing.T) {
	clearThirdPartyAuthSessionsForTest(t)
	body, _ := json.Marshal(map[string]any{"session_id": "maqr_unknown", "poll_token": "whatever"})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/third-party-qr-sessions/token", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	ThirdPartyQRTokenHandler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["status"] != "expired" {
		t.Fatalf("out=%#v, want expired", out)
	}
}
