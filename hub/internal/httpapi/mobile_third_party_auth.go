package httpapi

// Device-authorization ("scan to sign in a third-party client") flow.
//
// A third-party client (for example the RapidEditor AI assistant) has no Hub
// identity. It creates an unauthenticated session, renders the returned QR and
// polls for a token. The user scans the QR with an already-signed-in maclawbot
// phone; the phone authorizes the session with its viewer_token. The Hub then
// mints a fresh viewer_token for the signing-in user and parks it on the
// session, where the polling client can pick it up exactly once.
//
// Everything here deliberately reuses existing Hub primitives:
//   - session IDs / QR transport -> newMobileQRSessionID + mobileDesktopLlmQRPayload
//   - caller identity            -> authenticateViewerRequest
//   - credential minting         -> IdentityService.IssueViewerTokenForUser
//   - a viewer_token is accepted by /api/llm/v1/chat/completions and bills the
//     owning user's own credits, i.e. "the maclaw official service bound to the
//     scanning maclaw account".

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const (
	// thirdPartyAuthQRType is carried in the QR payload "type" field. maclawbot
	// recognizes this value alongside the desktop-auth type.
	thirdPartyAuthQRType = "maclaw_third_party_authorization"

	thirdPartySessionTTL = 5 * time.Minute
)

type thirdPartyAuthSession struct {
	SessionID   string
	ClientID    string
	ClientName  string
	State       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Authorized  bool
	IssuedToken string // viewer_token minted at authorize time, delivered once
	OwnerUserID string
	OwnerEmail  string
	TenantID    string
	DeliveredAt time.Time
}

var thirdPartyAuthSessions = struct {
	sync.Mutex
	sessions map[string]*thirdPartyAuthSession
}{
	sessions: make(map[string]*thirdPartyAuthSession),
}

func pruneThirdPartyAuthSessionsLocked(now time.Time) {
	for id, s := range thirdPartyAuthSessions.sessions {
		if now.After(s.ExpiresAt) || (!s.DeliveredAt.IsZero() && now.After(s.DeliveredAt.Add(10*time.Minute))) {
			delete(thirdPartyAuthSessions.sessions, id)
		}
	}
}

// thirdPartyClientID derives a stable per-client scope from the client_id so a
// session can only be polled by the same client that created it. We store a
// hash, never the raw client_id, in the polling credential check below.
func thirdPartyClientFingerprint(clientID, sessionID string) string {
	sum := sha256.Sum256([]byte(clientID + "\x00" + sessionID))
	return fmt.Sprintf("%x", sum[:8])
}

// ThirdPartyQRSessionHandler godoc
// POST /api/mobile/auth/third-party-qr-sessions
// No Hub identity required. Body: { client_id, name?, state? }.
func ThirdPartyQRSessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClientID string `json:"client_id"`
			Name     string `json:"name"`
			State    string `json:"state"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		req.ClientID = strings.TrimSpace(req.ClientID)
		if len(req.ClientID) < 4 || len(req.ClientID) > 128 {
			writeError(w, http.StatusBadRequest, "INVALID_CLIENT_ID", "client_id must be 4-128 characters")
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = "第三方客户端"
		}
		sessionID, err := newMobileQRSessionID("maqr_")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SESSION_ID_FAILED", "could not create session id")
			return
		}
		hubURL := mobileRequestBaseURL(r)
		if hubURL == "" {
			writeError(w, http.StatusInternalServerError, "HUB_URL_UNAVAILABLE", "could not determine Hub URL")
			return
		}
		// The phone's QR parser requires an https hub_url. If the request came
		// in over plain http behind a proxy, upgrade it rather than emitting a
		// QR the phone will reject.
		if strings.HasPrefix(hubURL, "http://") {
			hubURL = "https://" + strings.TrimPrefix(hubURL, "http://")
		}
		now := time.Now().UTC()
		s := &thirdPartyAuthSession{
			SessionID:  sessionID,
			ClientID:   req.ClientID,
			ClientName: name,
			State:      strings.TrimSpace(req.State),
			CreatedAt:  now,
			ExpiresAt:  now.Add(thirdPartySessionTTL),
		}
		thirdPartyAuthSessions.Lock()
		pruneThirdPartyAuthSessionsLocked(now)
		thirdPartyAuthSessions.sessions[sessionID] = s
		thirdPartyAuthSessions.Unlock()

		payload := mobileDesktopLlmQRPayload{
			Version:   2,
			Type:      thirdPartyAuthQRType,
			SessionID: sessionID,
			HubURL:    hubURL,
			ExpiresAt: now.Add(thirdPartySessionTTL).Format(time.RFC3339),
			Name:      name,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "QR_ENCODE_FAILED", "could not encode QR payload")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"status":     "pending",
			"session_id": sessionID,
			"expires_at": payload.ExpiresAt,
			"qr_payload": string(raw),
			"poll_token": thirdPartyClientFingerprint(req.ClientID, sessionID),
		})
	}
}

// ThirdPartyQRAuthorizeHandler godoc
// POST /api/mobile/auth/third-party-qr-sessions/authorize
// Requires the scanning phone's viewer_token. Body: { qr_payload, client_id? }.
func ThirdPartyQRAuthorizeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil {
			writeError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "identity service is unavailable")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "MaClaw account sign-in is required")
			return
		}
		var req struct {
			QRPayload string `json:"qr_payload"`
			ClientID  string `json:"client_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		var payload mobileDesktopLlmQRPayload
		if err := json.Unmarshal([]byte(strings.TrimSpace(req.QRPayload)), &payload); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_QR", "QR payload is not valid JSON")
			return
		}
		payload.Type = strings.TrimSpace(payload.Type)
		payload.SessionID = strings.TrimSpace(payload.SessionID)
		if payload.Type != thirdPartyAuthQRType || payload.SessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_QR", "QR is not a MaClaw third-party authorization session")
			return
		}

		now := time.Now().UTC()
		thirdPartyAuthSessions.Lock()
		s, ok := thirdPartyAuthSessions.sessions[payload.SessionID]
		if !ok {
			thirdPartyAuthSessions.Unlock()
			writeError(w, http.StatusBadRequest, "INVALID_QR", "authorization session was not found or has expired")
			return
		}
		if now.After(s.ExpiresAt) {
			delete(thirdPartyAuthSessions.sessions, payload.SessionID)
			thirdPartyAuthSessions.Unlock()
			writeError(w, http.StatusBadRequest, "QR_EXPIRED", "authorization QR has expired; ask the client to retry")
			return
		}
		if s.Authorized {
			thirdPartyAuthSessions.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"status": "authorized", "client_name": s.ClientName})
			return
		}
		// Bind client_id if the phone captured it from the QR (optional field).
		if cid := strings.TrimSpace(req.ClientID); cid != "" && cid != s.ClientID {
			thirdPartyAuthSessions.Unlock()
			writeError(w, http.StatusBadRequest, "CLIENT_MISMATCH", "client_id does not match this session")
			return
		}
		s.Authorized = true
		s.OwnerUserID = principal.UserID
		s.TenantID = principal.TenantID
		thirdPartyAuthSessions.Unlock()

		ctx := auth.WithTenant(r.Context(), principal.TenantID)
		token, err := identity.IssueViewerTokenForUser(ctx, principal.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "could not issue service credential")
			return
		}
		thirdPartyAuthSessions.Lock()
		if live, ok := thirdPartyAuthSessions.sessions[payload.SessionID]; ok {
			live.IssuedToken = token
			live.OwnerEmail = principal.Email
		}
		thirdPartyAuthSessions.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "authorized",
			"client_name": s.ClientName,
			"message":     "已授权，" + s.ClientName + " 现在可以使用你的 MaClaw 官方服务",
		})
	}
}

// ThirdPartyQRTokenHandler godoc
// POST /api/mobile/auth/third-party-qr-sessions/token
// Polled by the unauthenticated client. Body: { session_id, poll_token }.
// On success returns the viewer_token exactly once; subsequent polls see
// "delivered" and never leak the credential again.
func ThirdPartyQRTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"session_id"`
			PollToken string `json:"poll_token"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.PollToken = strings.TrimSpace(req.PollToken)

		thirdPartyAuthSessions.Lock()
		defer thirdPartyAuthSessions.Unlock()
		s, ok := thirdPartyAuthSessions.sessions[req.SessionID]
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"status": "expired"})
			return
		}
		// Constant-shape check: poll_token is derived from client_id + session_id
		// and was handed to the client at creation. It is not a bearer credential
		// on its own (it cannot authorize anything), but it prevents an observer
		// of the QR from stealing the delivered token by guessing the session.
		if thirdPartyClientFingerprint(s.ClientID, s.SessionID) != req.PollToken {
			writeError(w, http.StatusForbidden, "POLL_TOKEN_MISMATCH", "poll token does not match this session")
			return
		}
		if !s.Authorized {
			if time.Now().UTC().After(s.ExpiresAt) {
				delete(thirdPartyAuthSessions.sessions, s.SessionID)
				writeJSON(w, http.StatusOK, map[string]any{"status": "expired"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
			return
		}
		if s.IssuedToken == "" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "delivered"})
			return
		}
		token := s.IssuedToken
		s.IssuedToken = ""
		s.DeliveredAt = time.Now().UTC()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "authorized",
			"access_token": token,
			"user_id":      s.OwnerUserID,
			"email":        s.OwnerEmail,
			"state":        s.State,
		})
	}
}
