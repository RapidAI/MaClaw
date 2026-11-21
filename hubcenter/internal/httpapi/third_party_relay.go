// Relay mailbox for third-party device authorization ("scan to authorize").
//
// A third-party client (for example the RapidEditor AI assistant) does not
// know which Hub holds the user's account — only HubCenter does. The flow:
//
//  1. Client POSTs /api/third-party/sessions here and renders the returned
//     qr_payload. The QR carries only center_url + session_id: no hub_url,
//     because the client (and the user) does not know the Hub yet.
//  2. A signed-in maclawbot phone scans the QR, resolves its OWN account's
//     Hub, creates a third-party session on that Hub, authorizes it with the
//     account's viewer_token and fetches the minted viewer_token back.
//  3. The phone delivers {hub_url, access_token, email} to
//     /api/third-party/sessions/deliver — HubCenter is a pure mailbox and
//     never sees the phone's viewer_token.
//  4. The client polls /api/third-party/sessions/token and receives the
//     credential exactly once, together with the Hub base URL it belongs to.
//
// Security notes:
//   - poll_token (handed to the client at creation, never embedded in the QR)
//     prevents a QR observer from stealing the delivered credential.
//   - Delivery authority is "whoever scanned the QR": the same trust model as
//     the direct Hub flow (whoever scans authorizes their own account). A
//     malicious scanner can only bind their own account to the waiting
//     client, which is a visible social attack, not a credential theft.
//   - Sessions live in process memory with a short TTL, matching the Hub's
//     in-memory third-party sessions. HA deployments should route a client's
//     create/poll traffic to the same node (sticky) or front this with a
//     shared store later.
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	thirdPartyRelayQRType = "maclaw_third_party_authorization"
	thirdPartyRelayTTL    = 5 * time.Minute
	// Delivered credentials linger briefly so a client that polls once more
	// sees "delivered" instead of "expired", then the session is pruned.
	thirdPartyRelayDeliveredTTL = 10 * time.Minute
	thirdPartyRelayMaxSessions  = 256
)

type thirdPartyRelaySession struct {
	SessionID   string
	ClientID    string
	ClientName  string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	DeliveredAt time.Time
	HubURL      string
	AccessToken string // delivered once, then cleared
	Email       string
}

var thirdPartyRelaySessions = struct {
	sync.Mutex
	sessions map[string]*thirdPartyRelaySession
}{
	sessions: make(map[string]*thirdPartyRelaySession),
}

func pruneThirdPartyRelaySessionsLocked(now time.Time) {
	for id, s := range thirdPartyRelaySessions.sessions {
		if now.After(s.ExpiresAt) ||
			(!s.DeliveredAt.IsZero() && now.After(s.DeliveredAt.Add(thirdPartyRelayDeliveredTTL))) {
			delete(thirdPartyRelaySessions.sessions, id)
		}
	}
}

func newThirdPartyRelaySessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "tpr_" + hex.EncodeToString(buf), nil
}

// thirdPartyRelayPollToken mirrors the Hub's poll_token: a fingerprint of
// client_id + session_id handed to the client at creation. It authorizes
// nothing on its own; it only stops a QR observer from claiming the
// delivered credential.
func thirdPartyRelayPollToken(clientID, sessionID string) string {
	sum := sha256.Sum256([]byte(clientID + "\x00" + sessionID))
	return fmt.Sprintf("%x", sum[:8])
}

// thirdPartyRelayBaseURL derives the public center URL for the QR payload.
// HTTPS is forced: the phone's QR parser rejects plain http.
func thirdPartyRelayBaseURL(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if scheme == "http" {
		scheme = "https"
	}
	return scheme + "://" + host
}

type thirdPartyRelayQRPayload struct {
	Version   int    `json:"version"`
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	CenterURL string `json:"center_url"`
	ExpiresAt string `json:"expires_at"`
	Name      string `json:"name,omitempty"`
}

// ThirdPartyRelaySessionHandler godoc
// POST /api/third-party/sessions
// No auth. Body: { client_id, name? }. Returns the QR payload for the
// desktop client plus the one-time poll credential.
func ThirdPartyRelaySessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClientID string `json:"client_id"`
			Name     string `json:"name"`
		}
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
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
		centerURL := thirdPartyRelayBaseURL(r)
		if centerURL == "" {
			writeError(w, http.StatusInternalServerError, "CENTER_URL_UNAVAILABLE", "could not determine HubCenter URL")
			return
		}
		sessionID, err := newThirdPartyRelaySessionID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SESSION_ID_FAILED", "could not create session id")
			return
		}
		now := time.Now().UTC()
		expiresAt := now.Add(thirdPartyRelayTTL)

		thirdPartyRelaySessions.Lock()
		pruneThirdPartyRelaySessionsLocked(now)
		if len(thirdPartyRelaySessions.sessions) >= thirdPartyRelayMaxSessions {
			thirdPartyRelaySessions.Unlock()
			writeError(w, http.StatusTooManyRequests, "TOO_MANY_SESSIONS", "too many pending authorization sessions; retry later")
			return
		}
		thirdPartyRelaySessions.sessions[sessionID] = &thirdPartyRelaySession{
			SessionID:  sessionID,
			ClientID:   req.ClientID,
			ClientName: name,
			CreatedAt:  now,
			ExpiresAt:  expiresAt,
		}
		thirdPartyRelaySessions.Unlock()

		payload := thirdPartyRelayQRPayload{
			Version:   2,
			Type:      thirdPartyRelayQRType,
			SessionID: sessionID,
			CenterURL: centerURL,
			ExpiresAt: expiresAt.Format(time.RFC3339),
			Name:      name,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "QR_ENCODE_FAILED", "could not encode QR payload")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"status":      "pending",
			"session_id":  sessionID,
			"expires_at":  payload.ExpiresAt,
			"qr_payload":  string(raw),
			"poll_token":  thirdPartyRelayPollToken(req.ClientID, sessionID),
			"client_name": name,
		})
	}
}

// ThirdPartyRelayDeliverHandler godoc
// POST /api/third-party/sessions/deliver
// Called by the scanning phone after it fetched the minted viewer_token from
// its own Hub. Body: { session_id, hub_url, access_token, email? }.
func ThirdPartyRelayDeliverHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID   string `json:"session_id"`
			HubURL      string `json:"hub_url"`
			AccessToken string `json:"access_token"`
			Email       string `json:"email"`
		}
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.HubURL = strings.TrimRight(strings.TrimSpace(req.HubURL), "/")
		req.AccessToken = strings.TrimSpace(req.AccessToken)
		req.Email = strings.TrimSpace(req.Email)
		if req.SessionID == "" || req.AccessToken == "" {
			writeError(w, http.StatusBadRequest, "INVALID_DELIVERY", "session_id and access_token are required")
			return
		}
		hub := req.HubURL
		if !strings.HasPrefix(hub, "https://") {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_URL", "hub_url must be an https URL")
			return
		}

		now := time.Now().UTC()
		thirdPartyRelaySessions.Lock()
		defer thirdPartyRelaySessions.Unlock()
		s, ok := thirdPartyRelaySessions.sessions[req.SessionID]
		if !ok {
			writeError(w, http.StatusBadRequest, "SESSION_NOT_FOUND", "authorization session was not found or has expired")
			return
		}
		if now.After(s.ExpiresAt) {
			delete(thirdPartyRelaySessions.sessions, req.SessionID)
			writeError(w, http.StatusBadRequest, "SESSION_EXPIRED", "authorization session has expired")
			return
		}
		if !s.DeliveredAt.IsZero() {
			// Already delivered: acknowledge so a phone retry stays calm.
			writeJSON(w, http.StatusOK, map[string]any{"status": "delivered"})
			return
		}
		s.HubURL = hub
		s.AccessToken = req.AccessToken
		s.Email = req.Email
		s.DeliveredAt = now
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

// ThirdPartyRelayTokenHandler godoc
// POST /api/third-party/sessions/token
// Polled by the unauthenticated client. Body: { session_id, poll_token }.
// On success returns {hub_url, access_token, email} exactly once.
func ThirdPartyRelayTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"session_id"`
			PollToken string `json:"poll_token"`
		}
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.PollToken = strings.TrimSpace(req.PollToken)

		now := time.Now().UTC()
		thirdPartyRelaySessions.Lock()
		defer thirdPartyRelaySessions.Unlock()
		s, ok := thirdPartyRelaySessions.sessions[req.SessionID]
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"status": "expired"})
			return
		}
		if thirdPartyRelayPollToken(s.ClientID, s.SessionID) != req.PollToken {
			writeError(w, http.StatusForbidden, "POLL_TOKEN_MISMATCH", "poll token does not match this session")
			return
		}
		if s.DeliveredAt.IsZero() {
			if now.After(s.ExpiresAt) {
				delete(thirdPartyRelaySessions.sessions, s.SessionID)
				writeJSON(w, http.StatusOK, map[string]any{"status": "expired"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
			return
		}
		if s.AccessToken == "" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "delivered"})
			return
		}
		token := s.AccessToken
		s.AccessToken = ""
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "authorized",
			"hub_url":      s.HubURL,
			"access_token": token,
			"email":        s.Email,
			"client_name":  s.ClientName,
		})
	}
}
