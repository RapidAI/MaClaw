package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postRelayJSON(t *testing.T, handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/third-party/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestThirdPartyRelayCreateSession(t *testing.T) {
	rec := postRelayJSON(t, ThirdPartyRelaySessionHandler(),
		`{"client_id":"rapideditor-ai-assistant","name":"RapidEditor AI 助手"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status     string `json:"status"`
		SessionID  string `json:"session_id"`
		PollToken  string `json:"poll_token"`
		QRPayload  string `json:"qr_payload"`
		ExpiresAt  string `json:"expires_at"`
		ClientName string `json:"client_name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "pending" || resp.SessionID == "" || resp.PollToken == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !strings.HasPrefix(resp.SessionID, "tpr_") {
		t.Fatalf("session id prefix = %q", resp.SessionID)
	}
	if resp.ClientName != "RapidEditor AI 助手" {
		t.Fatalf("client_name = %q", resp.ClientName)
	}
	// The QR must carry center_url and must NOT carry a hub_url: the client
	// does not know the user's Hub, only this center.
	var qr struct {
		Version   int    `json:"version"`
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
		CenterURL string `json:"center_url"`
		HubURL    string `json:"hub_url"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(resp.QRPayload), &qr); err != nil {
		t.Fatalf("decode qr: %v", err)
	}
	if qr.Type != "maclaw_third_party_authorization" || qr.SessionID != resp.SessionID {
		t.Fatalf("unexpected qr: %+v", qr)
	}
	if !strings.HasPrefix(qr.CenterURL, "https://") {
		t.Fatalf("center_url = %q, want https", qr.CenterURL)
	}
	if qr.HubURL != "" {
		t.Fatalf("qr must not leak hub_url, got %q", qr.HubURL)
	}
	if qr.ExpiresAt != resp.ExpiresAt {
		t.Fatalf("qr expires_at %q != %q", qr.ExpiresAt, resp.ExpiresAt)
	}
}

func TestThirdPartyRelayCreateRejectsShortClientID(t *testing.T) {
	rec := postRelayJSON(t, ThirdPartyRelaySessionHandler(), `{"client_id":"ab"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestThirdPartyRelayFlowEndToEnd(t *testing.T) {
	// 1. Desktop creates the session.
	create := postRelayJSON(t, ThirdPartyRelaySessionHandler(),
		`{"client_id":"rapideditor-ai-assistant"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d", create.Code)
	}
	var created struct {
		SessionID string `json:"session_id"`
		PollToken string `json:"poll_token"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// 2. Desktop polls before the scan -> pending.
	pending := postRelayJSON(t, ThirdPartyRelayTokenHandler(),
		`{"session_id":"`+created.SessionID+`","poll_token":"`+created.PollToken+`"}`)
	if pending.Code != http.StatusOK || !strings.Contains(pending.Body.String(), `"pending"`) {
		t.Fatalf("pending: code=%d body=%s", pending.Code, pending.Body.String())
	}

	// 3. Phone delivers the hub credential.
	deliver := postRelayJSON(t, ThirdPartyRelayDeliverHandler(), `{
		"session_id":"`+created.SessionID+`",
		"hub_url":"https://hub.maclaw.top",
		"access_token":"vtok_123",
		"email":"user@example.com"
	}`)
	if deliver.Code != http.StatusOK || !strings.Contains(deliver.Body.String(), `"ok"`) {
		t.Fatalf("deliver: code=%d body=%s", deliver.Code, deliver.Body.String())
	}

	// 4. Desktop polls -> authorized with hub_url, once.
	token := postRelayJSON(t, ThirdPartyRelayTokenHandler(),
		`{"session_id":"`+created.SessionID+`","poll_token":"`+created.PollToken+`"}`)
	if token.Code != http.StatusOK {
		t.Fatalf("token status = %d", token.Code)
	}
	var done struct {
		Status      string `json:"status"`
		HubURL      string `json:"hub_url"`
		AccessToken string `json:"access_token"`
		Email       string `json:"email"`
	}
	if err := json.Unmarshal(token.Body.Bytes(), &done); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if done.Status != "authorized" || done.HubURL != "https://hub.maclaw.top" ||
		done.AccessToken != "vtok_123" || done.Email != "user@example.com" {
		t.Fatalf("unexpected token response: %+v", done)
	}

	// 5. Second poll sees "delivered" and never leaks the token again.
	again := postRelayJSON(t, ThirdPartyRelayTokenHandler(),
		`{"session_id":"`+created.SessionID+`","poll_token":"`+created.PollToken+`"}`)
	if !strings.Contains(again.Body.String(), `"delivered"`) ||
		strings.Contains(again.Body.String(), "vtok_123") {
		t.Fatalf("second poll: %s", again.Body.String())
	}

	// 6. Phone re-delivers (retry) -> calm ack, credential stays cleared.
	redeliver := postRelayJSON(t, ThirdPartyRelayDeliverHandler(), `{
		"session_id":"`+created.SessionID+`",
		"hub_url":"https://hub.maclaw.top",
		"access_token":"vtok_456"
	}`)
	if redeliver.Code != http.StatusOK || !strings.Contains(redeliver.Body.String(), `"delivered"`) {
		t.Fatalf("redeliver: code=%d body=%s", redeliver.Code, redeliver.Body.String())
	}
	if strings.Contains(redeliver.Body.String(), "vtok_456") {
		t.Fatalf("redeliver leaked a new token")
	}
}

func TestThirdPartyRelayTokenRejectsWrongPollToken(t *testing.T) {
	create := postRelayJSON(t, ThirdPartyRelaySessionHandler(), `{"client_id":"rapideditor-ai-assistant"}`)
	var created struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec := postRelayJSON(t, ThirdPartyRelayTokenHandler(),
		`{"session_id":"`+created.SessionID+`","poll_token":"wrong"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestThirdPartyRelayTokenUnknownSessionIsExpired(t *testing.T) {
	rec := postRelayJSON(t, ThirdPartyRelayTokenHandler(),
		`{"session_id":"tpr_unknown0000","poll_token":"whatever"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"expired"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestThirdPartyRelayDeliverRequiresHTTPSAndToken(t *testing.T) {
	create := postRelayJSON(t, ThirdPartyRelaySessionHandler(), `{"client_id":"rapideditor-ai-assistant"}`)
	var created struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(create.Body.Bytes(), &created)

	httpHub := postRelayJSON(t, ThirdPartyRelayDeliverHandler(),
		`{"session_id":"`+created.SessionID+`","hub_url":"http://hub.maclaw.top","access_token":"t"}`)
	if httpHub.Code != http.StatusBadRequest {
		t.Fatalf("http hub_url: status = %d, want 400", httpHub.Code)
	}
	missingToken := postRelayJSON(t, ThirdPartyRelayDeliverHandler(),
		`{"session_id":"`+created.SessionID+`","hub_url":"https://hub.maclaw.top"}`)
	if missingToken.Code != http.StatusBadRequest {
		t.Fatalf("missing token: status = %d, want 400", missingToken.Code)
	}
	unknown := postRelayJSON(t, ThirdPartyRelayDeliverHandler(),
		`{"session_id":"tpr_unknown0000","hub_url":"https://hub.maclaw.top","access_token":"t"}`)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown session: status = %d, want 400", unknown.Code)
	}
}
