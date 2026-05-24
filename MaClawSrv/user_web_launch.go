package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

const webLaunchTokenTTL = 2 * time.Minute

type webLaunchTokenRecord struct {
	AccessToken     string
	LaunchExpiresAt time.Time
	AccessExpiresAt time.Time
	TenantID        string
	UserID          string
	SourceUserID    string
	InstanceID      string
	View            string
}

type launchTokenStore struct {
	mu     sync.Mutex
	tokens map[string]webLaunchTokenRecord
}

func newLaunchTokenStore() *launchTokenStore {
	return &launchTokenStore{tokens: map[string]webLaunchTokenRecord{}}
}

func (s *launchTokenStore) Create(accessToken string, accessExpiresAt time.Time, now time.Time, meta webLaunchTokenRecord) (string, time.Time, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	tokenHash := hashWebLaunchToken(token)
	launchExpiresAt := now.Add(webLaunchTokenTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	meta.AccessToken = accessToken
	meta.LaunchExpiresAt = launchExpiresAt
	meta.AccessExpiresAt = accessExpiresAt
	s.tokens[tokenHash] = meta
	return token, launchExpiresAt, tokenHash, nil
}

func (s *launchTokenStore) Consume(token string, now time.Time) (webLaunchTokenRecord, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return webLaunchTokenRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	key := hashWebLaunchToken(token)
	rec, ok := s.tokens[key]
	if !ok || !rec.LaunchExpiresAt.After(now) {
		delete(s.tokens, key)
		return webLaunchTokenRecord{}, false
	}
	delete(s.tokens, key)
	return rec, true
}

func (s *launchTokenStore) pruneLocked(now time.Time) {
	for key, rec := range s.tokens {
		if !rec.LaunchExpiresAt.After(now) {
			delete(s.tokens, key)
		}
	}
}

func hashWebLaunchToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func (s *HTTPServer) newWebLaunchToken(accessToken string, accessExpiresAt time.Time, now time.Time, meta webLaunchTokenRecord) (string, time.Time, string, error) {
	if s.launchTokens == nil {
		s.launchTokens = newLaunchTokenStore()
	}
	return s.launchTokens.Create(accessToken, accessExpiresAt, now, meta)
}

func (s *HTTPServer) handleWebLaunchExchange(w http.ResponseWriter, r *http.Request) {
	setUserSecurityHeaders(w)
	var in struct {
		LaunchToken string `json:"launch_token"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.launchTokens == nil {
		s.recordWebLaunchRejected(r, in.LaunchToken, "store_unavailable")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid launch token"})
		return
	}
	rec, ok := s.launchTokens.Consume(in.LaunchToken, time.Now().UTC())
	if !ok {
		s.recordWebLaunchRejected(r, in.LaunchToken, "invalid_or_expired")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid launch token"})
		return
	}
	s.recordWebLaunchExchanged(r, rec)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"access_token": rec.AccessToken, "token_type": "bearer", "expires_at": rec.AccessExpiresAt})
}

func (s *HTTPServer) recordWebLaunchExchanged(r *http.Request, rec webLaunchTokenRecord) {
	_ = s.svc.RecordAuditEvent(r.Context(), agentservice.AuditEvent{
		TenantID:     strings.TrimSpace(rec.TenantID),
		UserID:       strings.TrimSpace(rec.UserID),
		ActorType:    "user",
		ActorTenant:  strings.TrimSpace(rec.TenantID),
		ActorUser:    strings.TrimSpace(rec.UserID),
		Action:       "web.launch_token.exchanged",
		ResourceType: "web_launch_token",
		ResourceID:   strings.TrimSpace(rec.SourceUserID),
		Metadata: map[string]string{
			"tenant_id":      strings.TrimSpace(rec.TenantID),
			"user_id":        strings.TrimSpace(rec.UserID),
			"source_user_id": strings.TrimSpace(rec.SourceUserID),
			"instance_id":    strings.TrimSpace(rec.InstanceID),
			"view":           strings.TrimSpace(rec.View),
			"remote_ip":      requestClientIP(r),
		},
	})
}

func (s *HTTPServer) recordWebLaunchRejected(r *http.Request, launchToken, reason string) {
	meta := map[string]string{"reason": strings.TrimSpace(reason), "remote_ip": requestClientIP(r)}
	if strings.TrimSpace(launchToken) != "" {
		tokenHash := hashWebLaunchToken(launchToken)
		meta["launch_token_hash_prefix"] = shortWebLaunchTokenHash(tokenHash)
	}
	_ = s.svc.RecordAuditEvent(r.Context(), agentservice.AuditEvent{ActorType: "user", Action: "web.launch_token.rejected", ResourceType: "web_launch_token", Metadata: meta})
}

func shortWebLaunchTokenHash(tokenHash string) string {
	tokenHash = strings.TrimSpace(tokenHash)
	if len(tokenHash) <= 12 {
		return tokenHash
	}
	return tokenHash[:12]
}
