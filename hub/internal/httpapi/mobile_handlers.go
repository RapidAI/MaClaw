package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/gorilla/websocket"
)

var mobileWebSearch func(context.Context, string, int) ([]websearch.SearchResult, error) = websearch.SearchCtx

var mobileDocuments = struct {
	sync.Mutex
	drafts  map[string]mobileDocumentDraftRecord
	exports map[string]mobileDocumentExportRecord
	uploads map[string]mobileDocumentUploadRecord
}{
	drafts:  make(map[string]mobileDocumentDraftRecord),
	exports: make(map[string]mobileDocumentExportRecord),
	uploads: make(map[string]mobileDocumentUploadRecord),
}

var mobileDigitalEmployeeTasks = struct {
	sync.Mutex
	tasks map[string]mobileDigitalEmployeeTaskRecord
}{
	tasks: make(map[string]mobileDigitalEmployeeTaskRecord),
}

var mobileLlmAuthorizations = struct {
	sync.Mutex
	authorizations map[string]mobileLlmAuthorizationRecord
	qrSessions     map[string]mobileLlmQRSessionRecord
}{
	authorizations: make(map[string]mobileLlmAuthorizationRecord),
	qrSessions:     make(map[string]mobileLlmQRSessionRecord),
}

var mobileStatePersistence = struct {
	sync.Mutex
	loaded bool
}{}

var mobileOfficialHubCenterCandidates = []string{
	"https://hubs.mypapers.top",
	"https://hubs.maclaw.top",
	"https://hubs2.maclaw.top",
}

const mobileStatePathEnv = "MACLAW_MOBILE_STATE_PATH"

type mobilePersistentState struct {
	Drafts               map[string]mobileDocumentDraftRecord       `json:"drafts"`
	Exports              map[string]mobileDocumentExportRecord      `json:"exports"`
	Uploads              map[string]mobileDocumentUploadRecord      `json:"uploads"`
	DigitalEmployeeTasks map[string]mobileDigitalEmployeeTaskRecord `json:"digital_employee_tasks"`
	SavedAt              time.Time                                  `json:"saved_at"`
}

type mobileDocumentDraftRecord struct {
	ID        string
	OwnerID   string
	Title     string
	Template  string
	Markdown  string
	UpdatedAt time.Time
}

type mobileDocumentExportRecord struct {
	JobID     string
	DraftID   string
	OwnerID   string
	Format    string
	Status    string
	Message   string
	CreatedAt time.Time
}

type mobileDocumentUploadRecord struct {
	TaskID      string
	OwnerID     string
	Filename    string
	ContentType string
	Status      string
	DraftID     string
	Message     string
	ClaimedBy   string
	SourceBytes []byte
	OCRMarkdown string
	OCRMessage  string
	OCRError    string
	UploadedAt  time.Time
	UpdatedAt   time.Time
}

type mobileDigitalEmployeeTaskRecord struct {
	TaskID     string
	EmployeeID string
	OwnerID    string
	Prompt     string
	TaskType   string
	Context    map[string]string
	Status     string
	Result     string
	Message    string
	ClaimedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type mobileLlmAuthorizationRecord struct {
	AuthorizationID string
	OwnerID         string
	TenantID        string
	ProviderName    string
	ProviderURL     string
	APIKey          string
	Model           string
	Protocol        string
	AuthorizedAt    time.Time
}

type mobileLlmQRSessionRecord struct {
	SessionID    string
	OwnerID      string
	TenantID     string
	ProviderName string
	ProviderURL  string
	APIKey       string
	Model        string
	Protocol     string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ConsumedAt   time.Time
}

type mobileDesktopLlmQRPayload struct {
	Version       int      `json:"v"`
	Type          string   `json:"type"`
	SessionID     string   `json:"session_id"`
	HubURL        string   `json:"hub_url"`
	ExpiresAt     string   `json:"expires_at"`
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	Key           string   `json:"key"`
	Model         string   `json:"model"`
	Models        []string `json:"models"`
	Protocol      string   `json:"protocol"`
	ContextLength int      `json:"context_length"`
}

func mobileStatePath() string {
	return strings.TrimSpace(os.Getenv(mobileStatePathEnv))
}

func mobileEnsureStateLoaded() {
	mobileStatePersistence.Lock()
	if mobileStatePersistence.loaded {
		mobileStatePersistence.Unlock()
		return
	}
	mobileStatePersistence.loaded = true
	path := mobileStatePath()
	mobileStatePersistence.Unlock()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var state mobilePersistentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return
	}
	mobileDocuments.Lock()
	if state.Drafts != nil {
		mobileDocuments.drafts = state.Drafts
	}
	if state.Exports != nil {
		mobileDocuments.exports = state.Exports
	}
	if state.Uploads != nil {
		mobileDocuments.uploads = state.Uploads
	}
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	if state.DigitalEmployeeTasks != nil {
		mobileDigitalEmployeeTasks.tasks = state.DigitalEmployeeTasks
	}
	mobileDigitalEmployeeTasks.Unlock()
}

func mobilePersistState() {
	path := mobileStatePath()
	if path == "" {
		return
	}
	state := mobilePersistentState{
		Drafts:               make(map[string]mobileDocumentDraftRecord),
		Exports:              make(map[string]mobileDocumentExportRecord),
		Uploads:              make(map[string]mobileDocumentUploadRecord),
		DigitalEmployeeTasks: make(map[string]mobileDigitalEmployeeTaskRecord),
		SavedAt:              time.Now().UTC(),
	}
	mobileDocuments.Lock()
	for id, record := range mobileDocuments.drafts {
		state.Drafts[id] = record
	}
	for id, record := range mobileDocuments.exports {
		state.Exports[id] = record
	}
	for id, record := range mobileDocuments.uploads {
		state.Uploads[id] = record
	}
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	for id, record := range mobileDigitalEmployeeTasks.tasks {
		state.DigitalEmployeeTasks[id] = record
	}
	mobileDigitalEmployeeTasks.Unlock()
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func mobileResetStatePersistenceForTest() {
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = false
	mobileStatePersistence.Unlock()
}

var mobileRealtimeUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type mobileRealtimeJSONWriter interface {
	WriteJSON(v any) error
}

type mobileRealtimeClient struct {
	key  string
	conn mobileRealtimeJSONWriter
	mu   sync.Mutex
}

var mobileRealtimeClients = struct {
	sync.Mutex
	clients map[string]map[*mobileRealtimeClient]struct{}
}{
	clients: make(map[string]map[*mobileRealtimeClient]struct{}),
}

func mobileRealtimeKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
}

func mobileRealtimeRegister(tenantID, userID string, conn mobileRealtimeJSONWriter) (*mobileRealtimeClient, func()) {
	client := &mobileRealtimeClient{
		key:  mobileRealtimeKey(tenantID, userID),
		conn: conn,
	}
	mobileRealtimeClients.Lock()
	if mobileRealtimeClients.clients[client.key] == nil {
		mobileRealtimeClients.clients[client.key] = make(map[*mobileRealtimeClient]struct{})
	}
	mobileRealtimeClients.clients[client.key][client] = struct{}{}
	mobileRealtimeClients.Unlock()
	return client, func() {
		mobileRealtimeUnregister(client)
	}
}

func mobileRealtimeUnregister(client *mobileRealtimeClient) {
	if client == nil {
		return
	}
	mobileRealtimeClients.Lock()
	if bucket := mobileRealtimeClients.clients[client.key]; bucket != nil {
		delete(bucket, client)
		if len(bucket) == 0 {
			delete(mobileRealtimeClients.clients, client.key)
		}
	}
	mobileRealtimeClients.Unlock()
}

func mobileRealtimeBroadcast(tenantID, userID string, event map[string]any) {
	key := mobileRealtimeKey(tenantID, userID)
	mobileRealtimeClients.Lock()
	clients := make([]*mobileRealtimeClient, 0, len(mobileRealtimeClients.clients[key]))
	for client := range mobileRealtimeClients.clients[key] {
		clients = append(clients, client)
	}
	mobileRealtimeClients.Unlock()
	if len(clients) == 0 {
		return
	}
	payload := make(map[string]any, len(event)+1)
	for k, v := range event {
		payload[k] = v
	}
	if _, ok := payload["server_time"]; !ok {
		payload["server_time"] = time.Now().UTC().Format(time.RFC3339)
	}
	for _, client := range clients {
		client.mu.Lock()
		err := client.conn.WriteJSON(payload)
		client.mu.Unlock()
		if err != nil {
			mobileRealtimeUnregister(client)
		}
	}
}

func mobileRealtimeDocumentTaskEvent(kind string, payload map[string]any) map[string]any {
	event := map[string]any{
		"type": kind,
		"task": payload,
	}
	if taskID, _ := payload["task_id"].(string); taskID != "" {
		event["task_id"] = taskID
	}
	if jobID, _ := payload["job_id"].(string); jobID != "" {
		event["job_id"] = jobID
	}
	if status, _ := payload["status"].(string); status != "" {
		event["status"] = status
	}
	return event
}

func mobileRealtimeDigitalEmployeeTaskEvent(payload map[string]any) map[string]any {
	event := map[string]any{
		"type": "digital_employee_task",
		"task": payload,
	}
	if taskID, _ := payload["task_id"].(string); taskID != "" {
		event["task_id"] = taskID
	}
	if status, _ := payload["status"].(string); status != "" {
		event["status"] = status
	}
	return event
}

// MobileRealtimeHandler upgrades authenticated mobile clients to a lightweight
// realtime channel. Long-running document and digital employee flows can still
// poll, but the mobile app now has an official Hub WebSocket endpoint for task
// completion and connection-health signals.
func MobileRealtimeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		conn, err := mobileRealtimeUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		client, unregister := mobileRealtimeRegister(principal.TenantID, principal.UserID, conn)
		defer unregister()

		client.mu.Lock()
		err = client.conn.WriteJSON(map[string]any{
			"type":        "ready",
			"user_id":     principal.UserID,
			"tenant_id":   principal.TenantID,
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
		client.mu.Unlock()
		if err != nil {
			return
		}
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msgType, _ := msg["type"].(string); msgType == "ping" {
				client.mu.Lock()
				err := client.conn.WriteJSON(map[string]any{
					"type":        "pong",
					"server_time": time.Now().UTC().Format(time.RFC3339),
				})
				client.mu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}
}

// MobileBootstrapHandler returns the small, cheap payload the mobile app needs
// immediately after restoring a viewer token. Expensive service details stay on
// their existing dedicated endpoints.
func MobileBootstrapHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		writeJSON(w, http.StatusOK, mobileBootstrapPayloadForRequest(principal, r))
	}
}

func mobileBootstrapPayload(principal *auth.ViewerPrincipal) map[string]any {
	return mobileBootstrapPayloadForRequest(principal, nil)
}

func mobileBootstrapPayloadForRequest(principal *auth.ViewerPrincipal, r *http.Request) map[string]any {
	userID := ""
	email := ""
	tenantID := ""
	if principal != nil {
		userID = principal.UserID
		email = principal.Email
		tenantID = principal.TenantID
	}
	hubURL := mobileRequestBaseURL(r)
	llmAccess := mobileLlmAccessPayload(principal)
	return map[string]any{
		"user": map[string]any{
			"user_id":   userID,
			"email":     email,
			"tenant_id": tenantID,
		},
		"connection": map[string]any{
			"hubcenter_candidates":   append([]string(nil), mobileOfficialHubCenterCandidates...),
			"selected_hubcenter_url": mobileOfficialHubCenterCandidates[1],
			"hub_url":                hubURL,
			"hub_id":                 "",
			"tenant_id":              tenantID,
		},
		"llm_access": llmAccess,
		"features": map[string]any{
			"search":             true,
			"documents":          true,
			"local_ssh":          true,
			"digital_employees":  true,
			"push_notifications": false,
		},
		"services": map[string]any{
			"hub_status":               "online",
			"llm_status":               "available",
			"search_status":            "available",
			"documents_status":         "available",
			"digital_employees_status": "available",
			"llm_status_path":          "/api/llm/service/status",
			"models_path":              "/api/llm/v1/models",
			"search_path":              "/api/mobile/search",
			"documents_path":           "/api/mobile/documents",
			"digital_employees_path":   "/api/mobile/digital-employees",
			"realtime_path":            "/api/mobile/realtime",
		},
		"limits": map[string]any{
			"max_upload_bytes": 25 * 1024 * 1024,
			"max_export_jobs":  3,
		},
		"server_time": time.Now().UTC().Format(time.RFC3339),
	}
}

func mobileRequestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "https"
	if r.TLS != nil {
		scheme = "https"
	} else if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(strings.Split(forwarded, ",")[0])
	} else if r.URL != nil && r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	}
	host := strings.TrimSpace(r.Host)
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if scheme != "https" && scheme != "http" {
		scheme = "https"
	}
	return scheme + "://" + host
}

func mobileLlmAccessPayload(principal *auth.ViewerPrincipal) map[string]any {
	if principal != nil {
		key := mobileLlmAuthorizationKey(principal.TenantID, principal.UserID)
		mobileLlmAuthorizations.Lock()
		record, ok := mobileLlmAuthorizations.authorizations[key]
		mobileLlmAuthorizations.Unlock()
		if ok {
			return map[string]any{
				"mode":             "desktop_qr_third_party",
				"status":           "available",
				"authorization_id": record.AuthorizationID,
				"authorized_by":    "maclaw-gui",
				"authorized_at":    record.AuthorizedAt.Format(time.RFC3339),
				"provider_name":    record.ProviderName,
				"provider_url":     record.ProviderURL,
				"model":            record.Model,
				"protocol":         record.Protocol,
			}
		}
	}
	return map[string]any{
		"mode":             "maclaw_official",
		"status":           "available",
		"authorization_id": "",
		"authorized_by":    "maclaw-official",
	}
}

func mobileLlmAuthorizationKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
}

// MobileLLMDesktopQRSessionHandler creates an opaque, one-time QR payload for
// MaClaw GUI. The provider API key stays on the Hub; the QR only carries a
// random session id that a logged-in mobile viewer can redeem.
func MobileLLMDesktopQRSessionHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			Name     string   `json:"name"`
			URL      string   `json:"url"`
			Key      string   `json:"key"`
			Model    string   `json:"model"`
			Models   []string `json:"models"`
			Protocol string   `json:"protocol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		payload, err := normalizeMobileDesktopLlmProviderPayload(mobileDesktopLlmQRPayload{
			Type:     "maclaw_llm",
			Name:     req.Name,
			URL:      req.URL,
			Key:      req.Key,
			Model:    req.Model,
			Models:   req.Models,
			Protocol: req.Protocol,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_LLM_PROVIDER", err.Error())
			return
		}
		sessionID, err := newMobileLlmQRSessionID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SESSION_CREATE_FAILED", "failed to create QR session")
			return
		}
		now := time.Now().UTC()
		expiresAt := now.Add(5 * time.Minute)
		record := mobileLlmQRSessionRecord{
			SessionID:    sessionID,
			OwnerID:      principal.UserID,
			TenantID:     principal.TenantID,
			ProviderName: payload.Name,
			ProviderURL:  payload.URL,
			APIKey:       payload.Key,
			Model:        payload.Model,
			Protocol:     payload.Protocol,
			CreatedAt:    now,
			ExpiresAt:    expiresAt,
		}
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.qrSessions[sessionID] = record
		mobileLlmAuthorizations.Unlock()

		qrPayloadBytes, _ := json.Marshal(mobileDesktopLlmQRPayload{
			Version:   2,
			Type:      "maclaw_mobile_llm_authorization",
			SessionID: sessionID,
			HubURL:    mobileRequestBaseURL(r),
			ExpiresAt: expiresAt.Format(time.RFC3339),
			Name:      payload.Name,
			URL:       payload.URL,
			Model:     payload.Model,
			Protocol:  payload.Protocol,
		})
		writeJSON(w, http.StatusCreated, map[string]any{
			"status":     "created",
			"session_id": sessionID,
			"expires_at": expiresAt.Format(time.RFC3339),
			"qr_payload": string(qrPayloadBytes),
		})
	}
}

// MobileLLMDesktopQRAuthorizationHandler accepts the QR payload generated by
// MaClaw desktop GUI and records that this mobile viewer may use that delegated
// third-party LLM configuration through their discovered Hub.
func MobileLLMDesktopQRAuthorizationHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			QRPayload string `json:"qr_payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		payload, err := parseMobileDesktopLlmQRPayload(req.QRPayload)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_QR", err.Error())
			return
		}
		record, err := mobileLlmAuthorizationFromQR(principal, payload, time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_QR", err.Error())
			return
		}
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.authorizations[mobileLlmAuthorizationKey(principal.TenantID, principal.UserID)] = record
		mobileLlmAuthorizations.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "authorized",
			"bootstrap": mobileBootstrapPayloadForRequest(principal, r),
		})
	}
}

// MobileLLMDesktopQRSessionConsumeHandler lets a fresh mobile install consume
// the MaClaw GUI one-time QR session and receive a Hub-issued mobile viewer
// token. This is the first-login counterpart to the authenticated
// desktop-qr-authorizations endpoint.
func MobileLLMDesktopQRSessionConsumeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			QRPayload string `json:"qr_payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		payload, err := parseMobileDesktopLlmQRPayload(req.QRPayload)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_QR", err.Error())
			return
		}
		record, err := mobileLlmAuthorizationFromQRSession(nil, payload, time.Now().UTC(), true)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_QR", err.Error())
			return
		}
		token, err := identity.IssueViewerTokenForUser(auth.WithTenant(r.Context(), record.TenantID), record.OwnerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to issue mobile viewer token")
			return
		}
		email := ""
		if repo := identity.UsersRepo(); repo != nil {
			if user, err := repo.GetByID(auth.WithTenant(r.Context(), record.TenantID), record.OwnerID); err == nil && user != nil {
				email = user.Email
			}
		}
		principal := &auth.ViewerPrincipal{
			TenantID: record.TenantID,
			UserID:   record.OwnerID,
			Email:    email,
		}
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.authorizations[mobileLlmAuthorizationKey(record.TenantID, record.OwnerID)] = record
		mobileLlmAuthorizations.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "authorized",
			"access_token": token,
			"hub_url":      mobileRequestBaseURL(r),
			"hub_id":       "",
			"tenant_id":    record.TenantID,
			"bootstrap":    mobileBootstrapPayloadForRequest(principal, r),
		})
	}
}

func parseMobileDesktopLlmQRPayload(raw string) (mobileDesktopLlmQRPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("qr_payload is required")
	}
	var payload mobileDesktopLlmQRPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("qr_payload must be MaClaw desktop GUI JSON")
	}
	payload.Type = strings.TrimSpace(payload.Type)
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.ExpiresAt = strings.TrimSpace(payload.ExpiresAt)
	if payload.Type != "maclaw_mobile_llm_authorization" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("qr_payload must be a MaClaw GUI mobile authorization session")
	}
	if payload.SessionID == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("session_id is required")
	}
	return payload, nil
}

func normalizeMobileDesktopLlmProviderPayload(payload mobileDesktopLlmQRPayload) (mobileDesktopLlmQRPayload, error) {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.URL = strings.TrimSpace(payload.URL)
	payload.Key = strings.TrimSpace(payload.Key)
	payload.Model = strings.TrimSpace(payload.Model)
	payload.Protocol = strings.TrimSpace(payload.Protocol)
	if payload.Name == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("provider name is required")
	}
	if payload.Key == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("provider key is required")
	}
	parsedURL, err := url.Parse(payload.URL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("provider url must be http or https")
	}
	if payload.Model == "" && len(payload.Models) > 0 {
		payload.Model = strings.TrimSpace(payload.Models[0])
	}
	if payload.Protocol == "" {
		payload.Protocol = "openai"
	}
	return payload, nil
}

func mobileLlmAuthorizationFromQR(principal *auth.ViewerPrincipal, payload mobileDesktopLlmQRPayload, now time.Time) (mobileLlmAuthorizationRecord, error) {
	return mobileLlmAuthorizationFromQRSession(principal, payload, now, false)
}

func mobileLlmAuthorizationFromQRSession(principal *auth.ViewerPrincipal, payload mobileDesktopLlmQRPayload, now time.Time, allowSessionAccount bool) (mobileLlmAuthorizationRecord, error) {
	tenantID := ""
	userID := ""
	if principal != nil {
		tenantID = principal.TenantID
		userID = principal.UserID
	}
	mobileLlmAuthorizations.Lock()
	session, ok := mobileLlmAuthorizations.qrSessions[payload.SessionID]
	if !ok {
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session was not found or has already been used")
	}
	if !session.ConsumedAt.IsZero() {
		delete(mobileLlmAuthorizations.qrSessions, payload.SessionID)
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session has already been used")
	}
	if now.After(session.ExpiresAt) {
		delete(mobileLlmAuthorizations.qrSessions, payload.SessionID)
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session has expired")
	}
	if allowSessionAccount {
		tenantID = session.TenantID
		userID = session.OwnerID
	} else if session.TenantID != tenantID || session.OwnerID != userID {
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session belongs to a different MaClaw account")
	}
	session.ConsumedAt = now
	delete(mobileLlmAuthorizations.qrSessions, payload.SessionID)
	mobileLlmAuthorizations.Unlock()
	sum := sha256.Sum256([]byte(strings.Join([]string{
		tenantID,
		userID,
		session.ProviderName,
		session.ProviderURL,
		session.Model,
		now.Format(time.RFC3339Nano),
	}, "\x00")))
	return mobileLlmAuthorizationRecord{
		AuthorizationID: "mllm_" + fmt.Sprintf("%x", sum[:8]),
		OwnerID:         userID,
		TenantID:        tenantID,
		ProviderName:    session.ProviderName,
		ProviderURL:     session.ProviderURL,
		APIKey:          session.APIKey,
		Model:           session.Model,
		Protocol:        session.Protocol,
		AuthorizedAt:    now,
	}, nil
}

func newMobileLlmQRSessionID() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "mlqr_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func mobileDocumentDraftPayload(record mobileDocumentDraftRecord) map[string]any {
	return map[string]any{
		"id":         record.ID,
		"title":      record.Title,
		"template":   record.Template,
		"markdown":   record.Markdown,
		"updated_at": record.UpdatedAt.Format(time.RFC3339),
		"owner_id":   record.OwnerID,
	}
}

func mobileDocumentExportPayload(record mobileDocumentExportRecord) map[string]any {
	downloadURL := ""
	if record.Status == "ready" {
		downloadURL = "/api/mobile/documents/export/" + record.JobID + "/download"
	}
	return map[string]any{
		"job_id":       record.JobID,
		"draft_id":     record.DraftID,
		"format":       record.Format,
		"status":       record.Status,
		"download_url": downloadURL,
		"message":      strings.TrimSpace(record.Message),
		"created_at":   record.CreatedAt.Format(time.RFC3339),
	}
}

func mobileDocumentUploadPayload(record mobileDocumentUploadRecord) map[string]any {
	payload := map[string]any{
		"task_id":      record.TaskID,
		"filename":     record.Filename,
		"content_type": record.ContentType,
		"status":       record.Status,
		"draft_id":     record.DraftID,
		"message":      record.Message,
		"claimed_by":   record.ClaimedBy,
		"uploaded_at":  record.UploadedAt.Format(time.RFC3339),
		"updated_at":   record.UpdatedAt.Format(time.RFC3339),
		"owner_id":     record.OwnerID,
	}
	if record.DraftID != "" {
		if draft, ok := mobileDocuments.drafts[record.DraftID]; ok {
			payload["draft"] = mobileDocumentDraftPayload(draft)
		}
	}
	if record.TaskID != "" && len(record.SourceBytes) > 0 {
		payload["source_download_url"] = "/api/mobile/documents/upload/" + record.TaskID + "/source"
	}
	return payload
}

func mobileApplyUploadPipelineResult(record mobileDocumentUploadRecord, now time.Time) (mobileDocumentUploadRecord, bool) {
	if record.Status != "needs_ocr" {
		return record, false
	}
	if strings.TrimSpace(record.OCRError) != "" {
		record.Status = "failed"
		record.Message = strings.TrimSpace(record.OCRError)
		record.UpdatedAt = now
		return record, true
	}
	ocrMarkdown := strings.TrimSpace(record.OCRMarkdown)
	if ocrMarkdown == "" || record.DraftID == "" {
		return record, false
	}
	draft, ok := mobileDocuments.drafts[record.DraftID]
	if !ok || draft.OwnerID != record.OwnerID {
		return record, false
	}
	draft.Markdown = ocrMarkdown
	draft.UpdatedAt = now
	mobileDocuments.drafts[draft.ID] = draft
	record.Status = "ready"
	record.Message = strings.TrimSpace(record.OCRMessage)
	if record.Message == "" {
		record.Message = "OCR/视觉识别已完成，已更新移动端文档草稿。"
	}
	record.UpdatedAt = now
	return record, true
}

type mobileSearchRequest struct {
	Query   string   `json:"query"`
	Context []string `json:"context,omitempty"`
}

// MobileSearchHandler validates the mobile search request and returns a stable
// response shape. The actual retrieval/LLM implementation can be swapped in
// behind this contract without changing the mobile client.
func MobileSearchHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		var req mobileSearchRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		query := strings.TrimSpace(req.Query)
		if query == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "query is required")
			return
		}

		results, err := mobileWebSearch(r.Context(), query, 5)
		if err != nil {
			writeError(w, http.StatusBadGateway, "SEARCH_FAILED", "mobile search failed")
			return
		}

		linkRefs := mobileExtractQueryLinks(query)
		citations := mobileSearchCitations(results)
		citations = mobileMergeLinkCitations(citations, linkRefs)
		writeJSON(w, http.StatusOK, map[string]any{
			"answer":    mobileSearchAnswer(query, results, linkRefs),
			"citations": citations,
			"query":     query,
			"tenant_id": principal.TenantID,
			"user_id":   principal.UserID,
			"status":    "ready",
		})
	}
}

func mobileSearchAnswer(query string, results []websearch.SearchResult, links []string) string {
	query = strings.TrimSpace(query)
	if len(results) == 0 {
		if len(links) > 0 {
			return "已识别分享链接。当前没有额外搜索结果，已保留链接作为来源，可继续整理为文档草稿或补充问题。"
		}
		return "未找到可引用的搜索结果。请换一个更具体的问题再试。"
	}
	var b strings.Builder
	b.WriteString("已为你检索：")
	b.WriteString(query)
	b.WriteString("\n\n")
	b.WriteString("可先参考这些来源：")
	for i, result := range results {
		if i >= 3 {
			break
		}
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = strings.TrimSpace(result.URL)
		}
		if title == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%d. %s", i+1, title))
		snippet := strings.TrimSpace(result.Snippet)
		if snippet != "" {
			b.WriteString("：")
			b.WriteString(snippet)
		}
	}
	return b.String()
}

func mobileExtractQueryLinks(query string) []string {
	seen := map[string]bool{}
	var links []string
	for _, field := range strings.Fields(query) {
		candidate := strings.Trim(field, " \t\r\n<>（）()[]{}，,。；;\"'")
		u, err := url.Parse(candidate)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		normalized := u.String()
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		links = append(links, normalized)
	}
	return links
}

func mobileMergeLinkCitations(citations []map[string]string, links []string) []map[string]string {
	if len(links) == 0 {
		return citations
	}
	seen := map[string]bool{}
	for _, citation := range citations {
		seen[strings.TrimSpace(citation["url"])] = true
	}
	for _, link := range links {
		if seen[link] {
			continue
		}
		citations = append(citations, map[string]string{
			"title":   link,
			"url":     link,
			"snippet": "用户分享的链接",
		})
	}
	return citations
}
func mobileSearchCitations(results []websearch.SearchResult) []map[string]string {
	citations := make([]map[string]string, 0, len(results))
	for _, result := range results {
		url := strings.TrimSpace(result.URL)
		if url == "" {
			continue
		}
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = url
		}
		citations = append(citations, map[string]string{
			"title":   title,
			"url":     url,
			"snippet": strings.TrimSpace(result.Snippet),
		})
	}
	return citations
}

type mobileDocumentDraftRequest struct {
	Title    string `json:"title"`
	Template string `json:"template"`
	Content  string `json:"content,omitempty"`
}

type mobileDocumentDraftUpdateRequest struct {
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

type mobileDocumentProcessRequest struct {
	Action string `json:"action"`
}

type mobileDocumentExportRequest struct {
	DraftID string `json:"draft_id"`
	Format  string `json:"format"`
}

type mobileDocumentUploadResultRequest struct {
	Status   string `json:"status,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
}

type mobileSSHAnalyzeRequest struct {
	Output string `json:"output"`
}

type mobileDigitalEmployeeTaskRequest struct {
	Prompt   string            `json:"prompt"`
	TaskType string            `json:"task_type,omitempty"`
	Context  map[string]string `json:"context,omitempty"`
}

type mobileDigitalEmployeeTaskUpdateRequest struct {
	Status  string `json:"status"`
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func mobileDigitalEmployeeTaskPayload(record mobileDigitalEmployeeTaskRecord) map[string]any {
	return map[string]any{
		"task_id":     record.TaskID,
		"employee_id": record.EmployeeID,
		"prompt":      record.Prompt,
		"task_type":   record.TaskType,
		"context":     record.Context,
		"status":      record.Status,
		"result":      record.Result,
		"message":     record.Message,
		"claimed_by":  record.ClaimedBy,
		"created_at":  record.CreatedAt.Format(time.RFC3339),
		"updated_at":  record.UpdatedAt.Format(time.RFC3339),
	}
}

func normalizeMobileDigitalEmployeeTaskType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "server_maintenance", "remote_server", "ssh":
		return "server_maintenance"
	case "desktop_assist", "remote_desktop", "desktop":
		return "desktop_assist"
	case "document_work", "document", "office":
		return "document_work"
	case "information_check", "info_check", "search":
		return "information_check"
	default:
		return "general"
	}
}

func sanitizeMobileDigitalEmployeeContext(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string)
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if len(key) > 64 {
			key = key[:64]
		}
		if len(value) > 512 {
			value = value[:512]
		}
		out[key] = value
		if len(out) >= 12 {
			break
		}
	}
	return out
}

type mobileDigitalEmployeeWorkerPrincipal struct {
	TenantID  string
	UserID    string
	MachineID string
}

func authenticateMobileDigitalEmployeeWorker(r *http.Request, identity *auth.IdentityService) (mobileDigitalEmployeeWorkerPrincipal, error) {
	if identity == nil {
		return mobileDigitalEmployeeWorkerPrincipal{}, auth.ErrInvalidUserCredentials
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-ID"))
	if machineID == "" {
		machineID = strings.TrimSpace(r.Header.Get("X-MaClaw-Machine-ID"))
	}
	if machineID != "" {
		if token := extractBearerToken(r); token != "" {
			principal, err := identity.AuthenticateMachine(r.Context(), machineID, token)
			if err == nil && principal != nil {
				return mobileDigitalEmployeeWorkerPrincipal{
					TenantID:  principal.TenantID,
					UserID:    principal.UserID,
					MachineID: principal.MachineID,
				}, nil
			}
		}
	}
	viewer, err := authenticateViewerRequest(r, identity)
	if err != nil {
		return mobileDigitalEmployeeWorkerPrincipal{}, err
	}
	return mobileDigitalEmployeeWorkerPrincipal{
		TenantID: viewer.TenantID,
		UserID:   viewer.UserID,
	}, nil
}

// MobileDocumentDraftHandler creates an emergency document draft contract. It
// returns an ID and normalized markdown so the mobile app can continue editing
// offline while the richer document pipeline is implemented.
func MobileDocumentDraftHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()

		var req mobileDocumentDraftRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "title is required")
			return
		}
		template := strings.TrimSpace(req.Template)
		if template == "" {
			template = "report"
		}
		content := strings.TrimSpace(req.Content)
		if content == "" {
			content = "请在这里补充正文。"
		}
		now := time.Now().UTC()
		draftID := fmt.Sprintf("mobdoc_%d", now.UnixNano())
		record := mobileDocumentDraftRecord{
			ID:        draftID,
			OwnerID:   principal.UserID,
			Title:     title,
			Template:  template,
			Markdown:  "# " + title + "\n\n" + content + "\n",
			UpdatedAt: now,
		}
		mobileDocuments.Lock()
		mobileDocuments.drafts[draftID] = record
		mobileDocuments.Unlock()
		mobilePersistState()

		writeJSON(w, http.StatusCreated, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "draft_created",
		})
	}
}

// MobileDocumentDraftUpdateHandler persists lightweight title/body edits made
// on the mobile device before export or sharing.
func MobileDocumentDraftUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
			return
		}
		var req mobileDocumentDraftUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "title is required")
			return
		}
		markdown := strings.TrimSpace(req.Markdown)
		if markdown == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "markdown is required")
			return
		}
		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		if ok && record.OwnerID == principal.UserID {
			record.Title = title
			record.Markdown = markdown
			record.UpdatedAt = now
			mobileDocuments.drafts[draftID] = record
		}
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		mobilePersistState()
		writeJSON(w, http.StatusOK, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "draft_updated",
		})
	}
}

// MobileDocumentProcessHandler applies lightweight emergency document actions.
// It is deterministic today and keeps the same API shape for a richer LLM-backed
// processor later.
func MobileDocumentProcessHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
			return
		}
		var req mobileDocumentProcessRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		if !mobileDocumentProcessActionAllowed(action) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "action must be one of summarize, translate, rewrite, expand, polish, format")
			return
		}

		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		if ok && record.OwnerID == principal.UserID {
			record.Markdown = mobileProcessDocumentMarkdown(action, record.Markdown)
			record.UpdatedAt = now
			mobileDocuments.drafts[draftID] = record
		}
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		mobilePersistState()
		writeJSON(w, http.StatusOK, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "processed",
			"action": action,
		})
	}
}

func mobileDocumentProcessActionAllowed(action string) bool {
	switch action {
	case "summarize", "translate", "rewrite", "expand", "polish", "format":
		return true
	default:
		return false
	}
}

func mobileProcessDocumentMarkdown(action, markdown string) string {
	title, bodyLines := mobileDocumentTitleAndBody(markdown)
	switch action {
	case "summarize":
		return mobileProcessSummarize(title, bodyLines)
	case "translate":
		return mobileProcessTranslate(title, bodyLines)
	case "rewrite":
		return mobileProcessRewrite(title, bodyLines)
	case "expand":
		return mobileProcessExpand(title, bodyLines)
	case "polish":
		return mobileProcessPolish(title, bodyLines)
	case "format":
		return mobileProcessFormat(title, bodyLines)
	default:
		return markdown
	}
}

func mobileDocumentTitleAndBody(markdown string) (string, []string) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	title := "文档"
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && title == "文档" {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		if trimmed != "" {
			body = append(body, trimmed)
		}
	}
	return title, body
}

func mobileProcessSummarize(title string, body []string) string {
	points := mobileFirstNonEmptyLines(body, 5)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 摘要\n\n")
	if len(points) == 0 {
		b.WriteString("- 暂无可摘要内容。\n")
		return b.String()
	}
	for _, point := range points {
		b.WriteString("- ")
		b.WriteString(mobileTrimRunes(point, 120))
		b.WriteString("\n")
	}
	return b.String()
}

func mobileProcessTranslate(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 翻译草稿\n\n")
	b.WriteString("> 当前为移动端应急翻译草稿。联网 LLM 可用后会替换为完整翻译。\n\n")
	for _, line := range body {
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	return b.String()
}

func mobileProcessRewrite(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 改写稿\n\n")
	for _, line := range body {
		b.WriteString("- ")
		b.WriteString(strings.Trim(strings.TrimPrefix(line, "-"), " 。"))
		b.WriteString("。\n")
	}
	return b.String()
}

func mobileProcessExpand(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 扩写稿\n\n")
	for _, line := range body {
		b.WriteString("## ")
		b.WriteString(mobileTrimRunes(strings.Trim(strings.TrimPrefix(line, "-"), " 。"), 36))
		b.WriteString("\n\n")
		b.WriteString(line)
		b.WriteString("\n\n待补充：背景、影响、处理建议和下一步行动。\n\n")
	}
	return b.String()
}

func mobileProcessPolish(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 润色稿\n\n")
	for _, line := range body {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		b.WriteString(line)
		if !strings.HasSuffix(line, "。") && !strings.HasSuffix(line, ".") {
			b.WriteString("。")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func mobileProcessFormat(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, line := range body {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "##") || strings.HasPrefix(line, "```") {
			b.WriteString(line)
		} else {
			b.WriteString("- ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func mobileFirstNonEmptyLines(lines []string, limit int) []string {
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func mobileTrimRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

// MobileDocumentExportHandler validates export requests and returns an export
// job contract. Markdown, lightweight PDF, and lightweight DOCX are generated
// in-process for emergency mobile use.
func MobileDocumentExportHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()

		var req mobileDocumentExportRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		draftID := strings.TrimSpace(req.DraftID)
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft_id is required")
			return
		}
		mobileDocuments.Lock()
		draft, ok := mobileDocuments.drafts[draftID]
		mobileDocuments.Unlock()
		if !ok || draft.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		format := strings.ToLower(strings.TrimSpace(req.Format))
		switch format {
		case "pdf", "word", "markdown":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "format must be one of pdf, word, markdown")
			return
		}

		now := time.Now().UTC()
		job := mobileDocumentExportRecord{
			JobID:     fmt.Sprintf("mobexp_%d", now.UnixNano()),
			DraftID:   draftID,
			OwnerID:   principal.UserID,
			Format:    format,
			Status:    "queued",
			Message:   "导出任务已提交，等待官方服务生成文件。",
			CreatedAt: now,
		}
		if format == "markdown" || format == "pdf" || format == "word" {
			job.Status = "ready"
			job.Message = "导出文件已生成，可下载或分享。"
		}
		mobileDocuments.Lock()
		mobileDocuments.exports[job.JobID] = job
		mobileDocuments.Unlock()
		mobilePersistState()
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", mobileDocumentExportPayload(job)))
		writeJSON(w, http.StatusAccepted, mobileDocumentExportPayload(job))
	}
}

// MobileDocumentExportStatusHandler returns the current export job status.
func MobileDocumentExportStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		jobID := strings.TrimSpace(r.PathValue("jobId"))
		mobileDocuments.Lock()
		job, ok := mobileDocuments.exports[jobID]
		mobileDocuments.Unlock()
		if !ok || job.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "EXPORT_NOT_FOUND", "export job not found")
			return
		}
		writeJSON(w, http.StatusOK, mobileDocumentExportPayload(job))
	}
}

// MobileDocumentExportDownloadHandler downloads finished lightweight exports.
// Markdown, emergency PDF, and emergency DOCX are generated in-process.
func MobileDocumentExportDownloadHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		jobID := strings.TrimSpace(r.PathValue("jobId"))
		mobileDocuments.Lock()
		job, ok := mobileDocuments.exports[jobID]
		mobileDocuments.Unlock()
		if !ok || job.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "EXPORT_NOT_FOUND", "export job not found")
			return
		}
		if job.Status != "ready" || (job.Format != "markdown" && job.Format != "pdf" && job.Format != "word") {
			writeError(w, http.StatusConflict, "EXPORT_NOT_READY", "export job is not ready")
			return
		}
		mobileDocuments.Lock()
		draft, hasDraft := mobileDocuments.drafts[job.DraftID]
		mobileDocuments.Unlock()
		if !hasDraft || draft.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		if job.Format == "pdf" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".pdf\"")
			_, _ = w.Write(mobileRenderDraftPDF(draft))
			return
		}
		if job.Format == "word" {
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".docx\"")
			_, _ = w.Write(mobileRenderDraftDOCX(draft))
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".md\"")
		_, _ = w.Write([]byte(draft.Markdown))
	}
}

func mobileRenderDraftPDF(draft mobileDocumentDraftRecord) []byte {
	lines := mobilePDFLines(draft.Title, draft.Markdown)
	if len(lines) == 0 {
		lines = []string{draft.Title}
	}
	const linesPerPage = 34
	pageCount := (len(lines) + linesPerPage - 1) / linesPerPage
	if pageCount == 0 {
		pageCount = 1
	}
	fontID := 3 + pageCount*2
	objects := make([]string, fontID+1)
	objects[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	kids := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pageID := 3 + page*2
		contentID := pageID + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageID))
		start := page * linesPerPage
		end := start + linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		contents := mobilePDFPageContent(lines[start:end])
		objects[pageID] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontID, contentID)
		objects[contentID] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(contents), contents)
	}
	objects[2] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount)
	objects[fontID] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	var out strings.Builder
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for id := 1; id < len(objects); id++ {
		offsets[id] = out.Len()
		out.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", id, objects[id]))
	}
	xrefOffset := out.Len()
	out.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objects)))
	out.WriteString("0000000000 65535 f \n")
	for id := 1; id < len(objects); id++ {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[id]))
	}
	out.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects), xrefOffset))
	return []byte(out.String())
}

func mobilePDFLines(title, markdown string) []string {
	var out []string
	title = strings.TrimSpace(title)
	if title != "" {
		out = append(out, title, "")
	}
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, raw := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, mobileWrapPDFLine(line, 48)...)
	}
	return out
}

func mobileWrapPDFLine(line string, width int) []string {
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	var out []string
	for len(runes) > 0 {
		n := width
		if len(runes) < n {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func mobilePDFPageContent(lines []string) string {
	var b strings.Builder
	b.WriteString("BT\n/F1 12 Tf\n50 790 Td\n")
	for idx, line := range lines {
		if idx > 0 {
			b.WriteString("0 -20 Td\n")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString("<")
		b.WriteString(mobilePDFUTF16Hex(line))
		b.WriteString("> Tj\n")
	}
	b.WriteString("ET")
	return b.String()
}

func mobilePDFUTF16Hex(text string) string {
	units := utf16.Encode([]rune(text))
	var b strings.Builder
	b.WriteString("FEFF")
	for _, unit := range units {
		b.WriteString(fmt.Sprintf("%04X", unit))
	}
	return b.String()
}

func mobileRenderDraftDOCX(draft mobileDocumentDraftRecord) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mobileDOCXWriteFile(zw, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	mobileDOCXWriteFile(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)
	mobileDOCXWriteFile(zw, "word/document.xml", mobileDOCXDocumentXML(draft))
	_ = zw.Close()
	return buf.Bytes()
}

func mobileDOCXWriteFile(zw *zip.Writer, name, body string) {
	w, err := zw.Create(name)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, body)
}

func mobileDOCXDocumentXML(draft mobileDocumentDraftRecord) string {
	lines := mobilePDFLines(draft.Title, draft.Markdown)
	if len(lines) == 0 {
		lines = []string{draft.Title}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			b.WriteString(`<w:p/>`)
			continue
		}
		b.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		b.WriteString(mobileXMLEscape(line))
		b.WriteString(`</w:t></w:r></w:p>`)
	}
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func mobileXMLEscape(text string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}

const mobileDocumentUploadMaxBytes = 25 << 20

func mobileUploadedFileIsImmediateDraft(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".txt", ".md", ".markdown", ".log", ".csv", ".json", ".docx", ".xlsx", ".pdf":
		return true
	default:
		return false
	}
}

func mobileUploadedFileIsImage(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

func mobileDraftMarkdownFromUpload(filename string, raw []byte) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".docx":
		return mobileDraftMarkdownFromDOCX(filename, raw)
	case ".xlsx":
		return mobileDraftMarkdownFromXLSX(filename, raw)
	case ".pdf":
		return mobileDraftMarkdownFromPDF(filename, raw)
	}
	if !utf8.Valid(raw) {
		return "", false
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		text = "_导入文件为空。_"
	}
	if ext == ".md" || ext == ".markdown" {
		return text + "\n", true
	}
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		title = "导入文档"
	}
	switch ext {
	case ".log":
		return "# " + title + "\n\n```text\n" + text + "\n```\n", true
	case ".csv":
		return "# " + title + "\n\n```csv\n" + text + "\n```\n", true
	case ".json":
		return "# " + title + "\n\n```json\n" + text + "\n```\n", true
	default:
		return "# " + title + "\n\n" + text + "\n", true
	}
}

func mobileDraftMarkdownFromDOCX(filename string, raw []byte) (string, bool) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", false
	}
	var documentXML []byte
	for _, file := range zr.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", false
		}
		documentXML, err = io.ReadAll(io.LimitReader(rc, mobileDocumentUploadMaxBytes))
		_ = rc.Close()
		if err != nil {
			return "", false
		}
		break
	}
	if len(documentXML) == 0 {
		return "", false
	}
	paragraphs := mobileDOCXParagraphs(documentXML)
	if len(paragraphs) == 0 {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, paragraph := range paragraphs {
		b.WriteString(paragraph)
		b.WriteString("\n\n")
	}
	return b.String(), true
}

func mobileDOCXParagraphs(raw []byte) []string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var paragraphs []string
	var current strings.Builder
	inParagraph := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "p":
				inParagraph = true
				current.Reset()
			case "t":
				if inParagraph {
					var text string
					if err := decoder.DecodeElement(&text, &item); err == nil {
						current.WriteString(text)
					}
				}
			case "tab":
				if inParagraph {
					current.WriteString("\t")
				}
			case "br":
				if inParagraph {
					current.WriteString("\n")
				}
			}
		case xml.EndElement:
			if item.Name.Local == "p" && inParagraph {
				if text := strings.TrimSpace(current.String()); text != "" {
					paragraphs = append(paragraphs, text)
				}
				inParagraph = false
				current.Reset()
			}
		}
	}
	return paragraphs
}

func mobileDraftMarkdownFromXLSX(filename string, raw []byte) (string, bool) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", false
	}
	sharedStrings := mobileXLSXSharedStrings(zr)
	sheets := mobileXLSXSheets(zr, sharedStrings)
	if len(sheets) == 0 {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for index, rows := range sheets {
		if len(rows) == 0 {
			continue
		}
		if index > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Sheet ")
		b.WriteString(fmt.Sprintf("%d", index+1))
		b.WriteString("\n\n")
		for _, row := range rows {
			b.WriteString("- ")
			b.WriteString(strings.Join(row, " | "))
			b.WriteString("\n")
		}
	}
	return b.String(), true
}

func mobileDraftMarkdownFromPDF(filename string, raw []byte) (string, bool) {
	text := strings.TrimSpace(mobilePDFExtractText(raw))
	if text == "" {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(text)
	b.WriteString("\n")
	return b.String(), true
}

func mobileDraftMarkdownFromImage(filename string, raw []byte) string {
	title := mobileUploadTitle(filename)
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if format == "jpg" {
		format = "jpeg"
	}
	width, height := mobileImageDimensions(format, raw)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("图片已导入，等待 OCR/视觉模型识别。\n\n")
	b.WriteString("- 文件名：")
	b.WriteString(filepath.Base(filename))
	b.WriteString("\n")
	b.WriteString("- 格式：")
	b.WriteString(format)
	b.WriteString("\n")
	b.WriteString("- 大小：")
	b.WriteString(fmt.Sprintf("%d bytes", len(raw)))
	b.WriteString("\n")
	if width > 0 && height > 0 {
		b.WriteString("- 分辨率：")
		b.WriteString(fmt.Sprintf("%d x %d", width, height))
		b.WriteString("\n")
	}
	b.WriteString("\n## 待识别内容\n\n")
	b.WriteString("_OCR 完成后会把识别文本更新到这里。_\n")
	return b.String()
}
func mobileImageDimensions(format string, raw []byte) (int, int) {
	switch format {
	case "png":
		if len(raw) >= 24 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
			width := int(raw[16])<<24 | int(raw[17])<<16 | int(raw[18])<<8 | int(raw[19])
			height := int(raw[20])<<24 | int(raw[21])<<16 | int(raw[22])<<8 | int(raw[23])
			return width, height
		}
	case "jpg", "jpeg":
		for i := 2; i+9 < len(raw); {
			if raw[i] != 0xFF {
				i++
				continue
			}
			marker := raw[i+1]
			if marker == 0xC0 || marker == 0xC2 {
				height := int(raw[i+5])<<8 | int(raw[i+6])
				width := int(raw[i+7])<<8 | int(raw[i+8])
				return width, height
			}
			if i+3 >= len(raw) {
				break
			}
			size := int(raw[i+2])<<8 | int(raw[i+3])
			if size < 2 {
				break
			}
			i += 2 + size
		}
	}
	return 0, 0
}

func mobilePDFExtractText(raw []byte) string {
	text := string(raw)
	var out []string
	out = append(out, mobilePDFExtractHexStrings(text)...)
	out = append(out, mobilePDFExtractLiteralStrings(text)...)
	return strings.Join(mobileCompactTextLines(out), "\n")
}

func mobilePDFExtractHexStrings(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '<' || (i+1 < len(text) && text[i+1] == '<') {
			continue
		}
		end := strings.IndexByte(text[i+1:], '>')
		if end < 0 {
			break
		}
		body := text[i+1 : i+1+end]
		if decoded := mobileDecodePDFHexString(body); decoded != "" {
			out = append(out, decoded)
		}
		i = i + end + 1
	}
	return out
}

func mobileDecodePDFHexString(value string) string {
	var hexDigits []byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			hexDigits = append(hexDigits, c)
		}
	}
	if len(hexDigits) < 2 {
		return ""
	}
	if len(hexDigits)%2 == 1 {
		hexDigits = append(hexDigits, '0')
	}
	data := make([]byte, 0, len(hexDigits)/2)
	for i := 0; i+1 < len(hexDigits); i += 2 {
		hi, okHi := mobileHexNibble(hexDigits[i])
		lo, okLo := mobileHexNibble(hexDigits[i+1])
		if !okHi || !okLo {
			return ""
		}
		data = append(data, byte(hi<<4|lo))
	}
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		var runes []rune
		for i := 2; i+1 < len(data); i += 2 {
			runes = append(runes, rune(data[i])<<8|rune(data[i+1]))
		}
		return strings.TrimSpace(string(runes))
	}
	if utf8.Valid(data) {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func mobileHexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	default:
		return 0, false
	}
}

func mobilePDFExtractLiteralStrings(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '(' {
			continue
		}
		decoded, next, ok := mobileReadPDFLiteralString(text, i)
		if ok && strings.TrimSpace(decoded) != "" {
			out = append(out, decoded)
		}
		i = next
	}
	return out
}

func mobileReadPDFLiteralString(text string, start int) (string, int, bool) {
	var b strings.Builder
	escaped := false
	depth := 0
	for i := start + 1; i < len(text); i++ {
		c := text[i]
		if escaped {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			default:
				b.WriteByte(c)
			}
			escaped = false
			continue
		}
		switch c {
		case '\\':
			escaped = true
		case '(':
			depth++
			b.WriteByte(c)
		case ')':
			if depth == 0 {
				return strings.TrimSpace(b.String()), i, true
			}
			depth--
			b.WriteByte(c)
		default:
			if c >= 0x20 || c == '\n' || c == '\r' || c == '\t' {
				b.WriteByte(c)
			}
		}
	}
	return "", start, false
}

func mobileCompactTextLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	seen := make(map[string]bool)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || len(line) > 1000 || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

func mobileXLSXSharedStrings(zr *zip.Reader) []string {
	raw := mobileZipReadFile(zr, "xl/sharedStrings.xml")
	if len(raw) == 0 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var values []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}
		values = append(values, mobileXLSXInlineText(decoder, start.Name.Local))
	}
	return values
}

func mobileXLSXSheets(zr *zip.Reader, sharedStrings []string) [][][]string {
	var sheets [][][]string
	for _, file := range zr.File {
		if !strings.HasPrefix(file.Name, "xl/worksheets/sheet") || !strings.HasSuffix(file.Name, ".xml") {
			continue
		}
		raw := mobileZipReadFile(zr, file.Name)
		rows := mobileXLSXRows(raw, sharedStrings)
		if len(rows) > 0 {
			sheets = append(sheets, rows)
		}
	}
	return sheets
}

func mobileXLSXRows(raw []byte, sharedStrings []string) [][]string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var rows [][]string
	var current []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "row":
				current = nil
			case "c":
				current = append(current, mobileXLSXCellText(decoder, item, sharedStrings))
			}
		case xml.EndElement:
			if item.Name.Local == "row" {
				row := mobileTrimEmptyTrailingCells(current)
				if len(row) > 0 {
					rows = append(rows, row)
				}
				current = nil
			}
		}
	}
	return rows
}

func mobileXLSXCellText(decoder *xml.Decoder, cell xml.StartElement, sharedStrings []string) string {
	cellType := ""
	for _, attr := range cell.Attr {
		if attr.Name.Local == "t" {
			cellType = attr.Value
			break
		}
	}
	var value string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "v":
				var text string
				if err := decoder.DecodeElement(&text, &item); err == nil {
					value = strings.TrimSpace(text)
				}
			case "is":
				value = strings.TrimSpace(mobileXLSXInlineText(decoder, item.Name.Local))
			}
		case xml.EndElement:
			if item.Name.Local == "c" {
				if cellType == "s" {
					if index, ok := mobileParseInt(value); ok && index >= 0 && index < len(sharedStrings) {
						return sharedStrings[index]
					}
				}
				return value
			}
		}
	}
	return value
}

func mobileXLSXInlineText(decoder *xml.Decoder, endElement string) string {
	var b strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == "t" {
				var text string
				if err := decoder.DecodeElement(&text, &item); err == nil {
					b.WriteString(text)
				}
			}
		case xml.EndElement:
			if item.Name.Local == endElement {
				return strings.TrimSpace(b.String())
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func mobileTrimEmptyTrailingCells(cells []string) []string {
	end := len(cells)
	for end > 0 && strings.TrimSpace(cells[end-1]) == "" {
		end--
	}
	return cells[:end]
}

func mobileZipReadFile(zr *zip.Reader, name string) []byte {
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(rc, mobileDocumentUploadMaxBytes))
		_ = rc.Close()
		if err != nil {
			return nil
		}
		return raw
	}
	return nil
}

func mobileUploadTitle(filename string) string {
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		return "导入文档"
	}
	return title
}

func mobileParseInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// MobileDocumentUploadHandler validates emergency document upload metadata and
// returns a parse task contract. Lightweight text-like files are converted into
// drafts immediately; heavier Office/PDF/image parsing remains queued for the
// document pipeline.
func MobileDocumentUploadHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		if err := r.ParseMultipartForm(mobileDocumentUploadMaxBytes); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", "file upload must be multipart/form-data")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "file is required")
			return
		}
		defer file.Close()
		name := strings.TrimSpace(header.Filename)
		if name == "" {
			name = "upload"
		}
		body, err := io.ReadAll(io.LimitReader(file, mobileDocumentUploadMaxBytes+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "UPLOAD_READ_FAILED", "failed to read uploaded file")
			return
		}
		if len(body) > mobileDocumentUploadMaxBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "file exceeds mobile upload limit")
			return
		}
		now := time.Now().UTC()
		record := mobileDocumentUploadRecord{
			TaskID:      fmt.Sprintf("mobparse_%d", now.UnixNano()),
			OwnerID:     principal.UserID,
			Filename:    name,
			ContentType: strings.TrimSpace(header.Header.Get("Content-Type")),
			Status:      "queued",
			Message:     "已上传，等待文档解析管线处理。",
			SourceBytes: append([]byte(nil), body...),
			UploadedAt:  now,
			UpdatedAt:   now,
		}
		if mobileUploadedFileIsImmediateDraft(name) {
			if markdown, ok := mobileDraftMarkdownFromUpload(name, body); ok {
				draft := mobileDocumentDraftRecord{
					ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
					OwnerID:   principal.UserID,
					Title:     strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))),
					Template:  "report",
					Markdown:  markdown,
					UpdatedAt: now,
				}
				if draft.Title == "" {
					draft.Title = name
				}
				record.Status = "ready"
				record.DraftID = draft.ID
				record.Message = "文件已解析为移动端文档草稿。"
				mobileDocuments.Lock()
				mobileDocuments.drafts[draft.ID] = draft
				mobileDocuments.uploads[record.TaskID] = record
				payload := mobileDocumentUploadPayload(record)
				mobileDocuments.Unlock()
				mobilePersistState()
				mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
				writeJSON(w, http.StatusAccepted, payload)
				return
			}
			record.Message = "文件暂时无法立即解析，等待文档解析管线处理。"
		}
		if mobileUploadedFileIsImage(name) {
			draft := mobileDocumentDraftRecord{
				ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
				OwnerID:   principal.UserID,
				Title:     mobileUploadTitle(name),
				Template:  "report",
				Markdown:  mobileDraftMarkdownFromImage(name, body),
				UpdatedAt: now,
			}
			record.Status = "needs_ocr"
			record.DraftID = draft.ID
			record.Message = "图片已导入为移动端草稿，等待 OCR/视觉模型识别。"
			mobileDocuments.Lock()
			mobileDocuments.drafts[draft.ID] = draft
			mobileDocuments.uploads[record.TaskID] = record
			payload := mobileDocumentUploadPayload(record)
			mobileDocuments.Unlock()
			mobilePersistState()
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
			writeJSON(w, http.StatusAccepted, payload)
			return
		}
		mobileDocuments.Lock()
		mobileDocuments.uploads[record.TaskID] = record
		payload := mobileDocumentUploadPayload(record)
		mobileDocuments.Unlock()
		mobilePersistState()
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		writeJSON(w, http.StatusAccepted, payload)
	}
}

// MobileDocumentUploadStatusHandler returns the current upload parse task.
func MobileDocumentUploadStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		mobileDocuments.Lock()
		record, ok := mobileDocuments.uploads[taskID]
		changed := false
		if ok && record.OwnerID == principal.UserID {
			record, changed = mobileApplyUploadPipelineResult(record, time.Now().UTC())
			if changed {
				mobileDocuments.uploads[taskID] = record
			}
		}
		payload := mobileDocumentUploadPayload(record)
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "UPLOAD_NOT_FOUND", "upload task not found")
			return
		}
		if changed {
			mobilePersistState()
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

// MobileDocumentUploadSourceHandler streams the original upload to the claimed
// official worker so OCR/Office parsing can run outside the phone.
func MobileDocumentUploadSourceHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		mobileDocuments.Lock()
		record, exists := mobileDocuments.uploads[taskID]
		mobileDocuments.Unlock()
		if !exists || record.OwnerID != principal.UserID || len(record.SourceBytes) == 0 {
			writeError(w, http.StatusNotFound, "UPLOAD_SOURCE_NOT_FOUND", "upload source not found")
			return
		}
		if strings.TrimSpace(record.ClaimedBy) != "" && record.ClaimedBy != principal.MachineID {
			writeError(w, http.StatusForbidden, "UPLOAD_CLAIMED_BY_OTHER_WORKER", "upload task is claimed by another worker")
			return
		}
		contentType := strings.TrimSpace(record.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(record.Filename)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(record.SourceBytes)
	}
}

// MobileDocumentUploadClaimHandler lets an authenticated official worker claim
// one pending mobile document parse task for its user.
func MobileDocumentUploadClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		mobileEnsureStateLoaded()
		claimKind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
		if claimKind == "" {
			claimKind = "all"
		}
		if claimKind != "all" && claimKind != "document" && claimKind != "ocr" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "kind must be one of all, document, ocr")
			return
		}
		now := time.Now().UTC()
		var claimed mobileDocumentUploadRecord
		mobileDocuments.Lock()
		for taskID, record := range mobileDocuments.uploads {
			if record.OwnerID != principal.UserID {
				continue
			}
			if record.Status != "queued" && record.Status != "needs_ocr" {
				continue
			}
			if claimKind == "document" && record.Status != "queued" {
				continue
			}
			if claimKind == "ocr" && record.Status != "needs_ocr" {
				continue
			}
			if strings.TrimSpace(record.ClaimedBy) != "" && record.ClaimedBy != principal.MachineID {
				continue
			}
			record.Status = "in_progress"
			record.ClaimedBy = principal.MachineID
			record.Message = "远程解析 worker 正在处理移动端文档。"
			record.UpdatedAt = now
			mobileDocuments.uploads[taskID] = record
			claimed = record
			break
		}
		payload := mobileDocumentUploadPayload(claimed)
		mobileDocuments.Unlock()
		if claimed.TaskID == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "no_task",
				"task":   nil,
			})
			return
		}
		mobilePersistState()
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "claimed",
			"task":   payload,
		})
	}
}

// MobileDocumentUploadResultHandler lets official Hub workers or remote digital
// employees write OCR/vision parse results back into a mobile upload task.
func MobileDocumentUploadResultHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "task id is required")
			return
		}
		var req mobileDocumentUploadResultRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		markdown := strings.TrimSpace(req.Markdown)
		message := strings.TrimSpace(req.Message)
		errText := strings.TrimSpace(req.Error)
		if status == "" {
			if errText != "" {
				status = "failed"
			} else if markdown != "" {
				status = "ready"
			}
		}
		if status != "ready" && status != "failed" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status must be ready or failed")
			return
		}
		if status == "ready" && markdown == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "markdown is required when status is ready")
			return
		}
		if status == "failed" && errText == "" {
			if message != "" {
				errText = message
			} else {
				errText = "OCR/视觉识别失败。"
			}
		}

		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, exists := mobileDocuments.uploads[taskID]
		if exists && record.OwnerID == principal.UserID {
			if strings.TrimSpace(record.ClaimedBy) != "" && record.ClaimedBy != principal.MachineID {
				mobileDocuments.Unlock()
				writeError(w, http.StatusForbidden, "UPLOAD_CLAIMED_BY_OTHER_WORKER", "upload task is claimed by another worker")
				return
			}
			if record.Status == "ready" || record.Status == "failed" {
				mobileDocuments.Unlock()
				writeError(w, http.StatusConflict, "UPLOAD_ALREADY_FINISHED", "upload task already finished")
				return
			}
			record.OCRMarkdown = markdown
			record.OCRMessage = message
			record.OCRError = errText
			if record.Status == "" || record.Status == "queued" || record.Status == "in_progress" {
				record.Status = "needs_ocr"
			}
			if markdown != "" {
				draft, hasDraft := mobileDocuments.drafts[record.DraftID]
				if record.DraftID == "" || !hasDraft || draft.OwnerID != record.OwnerID {
					draft = mobileDocumentDraftRecord{
						ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
						OwnerID:   record.OwnerID,
						Title:     mobileUploadTitle(record.Filename),
						Template:  "report",
						Markdown:  markdown,
						UpdatedAt: now,
					}
					record.DraftID = draft.ID
					mobileDocuments.drafts[draft.ID] = draft
				}
			}
			record.UpdatedAt = now
			record, _ = mobileApplyUploadPipelineResult(record, now)
			mobileDocuments.uploads[taskID] = record
		}
		payload := mobileDocumentUploadPayload(record)
		mobileDocuments.Unlock()
		if !exists || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "UPLOAD_NOT_FOUND", "upload task not found")
			return
		}
		mobilePersistState()
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		writeJSON(w, http.StatusOK, payload)
	}
}

func mobileSSHAnalysisPayload(output string) map[string]any {
	lower := strings.ToLower(output)
	summary := "已读取终端输出。"
	recommendation := "先确认当前目录、服务名和影响范围，再手动执行排查命令。高风险命令不要直接复制执行。"
	commandDraft := ""
	switch {
	case strings.Contains(lower, "permission denied"):
		summary = "输出显示权限被拒绝。"
		recommendation = "检查当前用户、目标文件权限、sudo 策略和 SSH 密钥是否匹配。"
		commandDraft = "id && ls -la"
	case strings.Contains(lower, "no space left") || strings.Contains(lower, "disk full"):
		summary = "输出显示磁盘空间不足。"
		recommendation = "先查看磁盘和大目录占用，再决定是否清理日志或扩容。"
		commandDraft = "df -h && du -sh ./* 2>/dev/null | sort -h"
	case strings.Contains(lower, "connection refused"):
		summary = "输出显示连接被拒绝。"
		recommendation = "检查服务是否监听、端口是否正确，以及防火墙或安全组规则。"
		commandDraft = "ss -lntp"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
		summary = "输出显示连接超时。"
		recommendation = "优先检查网络连通性、DNS、路由、防火墙和上游服务状态。"
		commandDraft = "ping -c 4 <host> && traceroute <host>"
	case strings.Contains(lower, "nginx"):
		summary = "输出包含 nginx 相关信息。"
		recommendation = "先校验配置，再查看服务状态和最近错误日志。"
		commandDraft = "nginx -t && systemctl status nginx --no-pager"
	case strings.Contains(lower, "failed") || strings.Contains(lower, "error"):
		summary = "输出包含失败或错误信息。"
		recommendation = "先定位具体服务和最近日志，避免直接重启或删除数据。"
		commandDraft = "systemctl --failed && journalctl -xe --no-pager | tail -n 80"
	}
	return map[string]any{
		"summary":        summary,
		"recommendation": recommendation,
		"command_draft":  commandDraft,
		"status":         "ready",
	}
}

// MobileSSHAnalyzeHandler gives the mobile SSH surface a lightweight analysis
// without allowing the server or AI to execute commands.
func MobileSSHAnalyzeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		if _, err := authenticateViewerRequest(r, identity); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req mobileSSHAnalyzeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		output := strings.TrimSpace(req.Output)
		if output == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "output is required")
			return
		}
		writeJSON(w, http.StatusOK, mobileSSHAnalysisPayload(output))
	}
}

// MobileDigitalEmployeeTaskHandler creates a mobile-origin task request for a
// remote digital employee. The first version is intentionally asynchronous and
// permission-aware: mobile submits intent, the remote worker/owner confirms and
// executes according to its own policy.
func MobileDigitalEmployeeTaskHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		employeeID := strings.TrimSpace(r.PathValue("employeeId"))
		if employeeID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "employee id is required")
			return
		}
		var req mobileDigitalEmployeeTaskRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		prompt := strings.TrimSpace(req.Prompt)
		if prompt == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "prompt is required")
			return
		}
		taskType := normalizeMobileDigitalEmployeeTaskType(req.TaskType)
		taskContext := sanitizeMobileDigitalEmployeeContext(req.Context)
		now := time.Now().UTC()
		record := mobileDigitalEmployeeTaskRecord{
			TaskID:     fmt.Sprintf("mobve_%d", now.UnixNano()),
			EmployeeID: employeeID,
			OwnerID:    principal.UserID,
			Prompt:     prompt,
			TaskType:   taskType,
			Context:    taskContext,
			Status:     "queued",
			Result:     "任务已提交，等待远程数字员工或授权策略处理。",
			Message:    "任务已提交，等待远程数字员工领取。",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		mobileDigitalEmployeeTasks.Lock()
		mobileDigitalEmployeeTasks.tasks[record.TaskID] = record
		mobileDigitalEmployeeTasks.Unlock()
		mobilePersistState()
		payload := mobileDigitalEmployeeTaskPayload(record)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		writeJSON(w, http.StatusAccepted, payload)
	}
}

// MobileDigitalEmployeeTaskClaimHandler lets an authorized remote worker claim
// one queued mobile-origin task for a digital employee. This closes the mobile
// to remote-capability loop without letting the phone execute commands itself.
func MobileDigitalEmployeeTaskClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		employeeID := strings.TrimSpace(r.PathValue("employeeId"))
		if employeeID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "employee id is required")
			return
		}

		now := time.Now().UTC()
		claimedBy := principal.MachineID
		if claimedBy == "" {
			claimedBy = principal.UserID
		}
		var claimed mobileDigitalEmployeeTaskRecord
		mobileDigitalEmployeeTasks.Lock()
		for taskID, record := range mobileDigitalEmployeeTasks.tasks {
			if !groupDiscussionParticipantIdentityMatches(record.EmployeeID, employeeID) || record.OwnerID != principal.UserID || record.Status != "queued" {
				continue
			}
			record.Status = "in_progress"
			record.Result = "远程数字员工已领取任务，正在处理。"
			record.Message = "远程数字员工已领取任务，正在处理。"
			record.ClaimedBy = claimedBy
			record.UpdatedAt = now
			mobileDigitalEmployeeTasks.tasks[taskID] = record
			claimed = record
			break
		}
		mobileDigitalEmployeeTasks.Unlock()
		if claimed.TaskID == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"task":   nil,
				"status": "empty",
			})
			return
		}
		mobilePersistState()
		payload := mobileDigitalEmployeeTaskPayload(claimed)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{
			"task":   payload,
			"status": "claimed",
		})
	}
}

// MobileDigitalEmployeeTaskUpdateHandler lets the remote worker report task
// progress and final results back to the mobile user.
func MobileDigitalEmployeeTaskUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "task id is required")
			return
		}
		var req mobileDigitalEmployeeTaskUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		switch status {
		case "in_progress", "done", "failed":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status must be one of in_progress, done, failed")
			return
		}
		result := strings.TrimSpace(req.Result)
		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = strings.TrimSpace(req.Error)
		}
		if (status == "done" || status == "failed") && result == "" && message == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "result or message is required for final status")
			return
		}

		now := time.Now().UTC()
		workerID := principal.MachineID
		if workerID == "" {
			workerID = principal.UserID
		}
		mobileDigitalEmployeeTasks.Lock()
		record, ok := mobileDigitalEmployeeTasks.tasks[taskID]
		if ok && record.OwnerID == principal.UserID {
			if record.ClaimedBy != "" && record.ClaimedBy != workerID {
				ok = false
			} else {
				record.Status = status
				if result != "" {
					record.Result = result
				}
				if message != "" {
					record.Message = message
				}
				record.ClaimedBy = workerID
				record.UpdatedAt = now
				mobileDigitalEmployeeTasks.tasks[taskID] = record
			}
		}
		mobileDigitalEmployeeTasks.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "digital employee task not found")
			return
		}
		mobilePersistState()
		payload := mobileDigitalEmployeeTaskPayload(record)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		writeJSON(w, http.StatusOK, payload)
	}
}

// MobileDigitalEmployeeTaskStatusHandler returns a mobile-origin digital
// employee task. Full execution is delegated to the remote worker side.
func MobileDigitalEmployeeTaskStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		mobileDigitalEmployeeTasks.Lock()
		record, ok := mobileDigitalEmployeeTasks.tasks[taskID]
		mobileDigitalEmployeeTasks.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "digital employee task not found")
			return
		}
		writeJSON(w, http.StatusOK, mobileDigitalEmployeeTaskPayload(record))
	}
}

// MobileDigitalEmployeesHandler lists digital employees a mobile viewer may use
// as remote capability entry points. It intentionally uses viewer auth instead
// of the desktop machine token required by /api/ve/discoverable.
func MobileDigitalEmployeesHandler(identity *auth.IdentityService, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		tenantSystem := scopedSystemSettingsForTenant(principal.TenantID, system)
		authz := loadVEDigitalEmployeeAuthorization(r.Context(), tenantSystem)
		machineEmployees := mobileMachineDigitalEmployeeEntries(r.Context(), identity, principal)
		if !veAuthorizationActive(authz) {
			writeJSON(w, http.StatusOK, map[string]any{
				"employees":     machineEmployees,
				"authorization": authz,
			})
			return
		}

		baseSystem := globalSystemSettings(system)
		runtimePresence := emptyMacLawSrvRuntimePresence()
		registry := loadVERegistry(r.Context(), tenantSystem)
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, true) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, principal.TenantID)
		}

		employees := make([]digitalEmployeeEntry, 0, len(registry.Employees))
		for _, entry := range registry.Employees {
			if entry.Status != veStatusActive {
				continue
			}
			if !veVisibleToRequester(entry, nil, false) {
				continue
			}
			if !veAccessAllowed(entry, principal.UserID) {
				continue
			}
			entry = applyVEDiscoverablePresence(r.Context(), entry, nil, runtimePresence)
			employees = append(employees, entry)
		}
		employees = appendMobileMachineDigitalEmployees(employees, machineEmployees)
		sort.SliceStable(employees, func(i, j int) bool {
			if employees[i].OnlineStatus != employees[j].OnlineStatus {
				return employees[i].OnlineStatus == veOnlineStatusOnline
			}
			return employees[i].Name < employees[j].Name
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"employees": employees,
		})
	}
}

func mobileMachineDigitalEmployeeEntries(ctx context.Context, identity *auth.IdentityService, principal *auth.ViewerPrincipal) []digitalEmployeeEntry {
	if identity == nil || principal == nil {
		return nil
	}
	repo := identity.MachinesRepo()
	if repo == nil || strings.TrimSpace(principal.UserID) == "" {
		return nil
	}
	machines, err := repo.ListByUserID(ctx, principal.UserID)
	if err != nil {
		return nil
	}
	out := make([]digitalEmployeeEntry, 0, len(machines))
	for _, machine := range machines {
		if machine == nil || strings.TrimSpace(machine.ID) == "" {
			continue
		}
		if store.NormalizeTenantID(machine.TenantID) != store.NormalizeTenantID(principal.TenantID) {
			continue
		}
		name := strings.TrimSpace(machine.Alias)
		if name == "" {
			name = strings.TrimSpace(machine.Name)
		}
		if name == "" {
			name = strings.TrimSpace(machine.Hostname)
		}
		if name == "" {
			name = "MaClaw Remote"
		}
		onlineStatus := veOnlineStatusOffline
		if strings.EqualFold(strings.TrimSpace(machine.Status), veOnlineStatusOnline) {
			onlineStatus = veOnlineStatusOnline
		}
		out = append(out, digitalEmployeeEntry{
			ID:               "ve_" + strings.TrimSpace(machine.ID),
			MachineID:        strings.TrimSpace(machine.ID),
			EmployeeType:     veEmployeeTypePhysical,
			OwnerUserID:      principal.UserID,
			Name:             name,
			SkillDescription: "通过已绑定的 MaClaw GUI 远程电脑/服务器处理手机端应急任务，可用于日志分析、服务器维护、文档处理和信息核查；高风险命令需要用户确认。",
			AccessPolicy:     "public",
			Resident:         true,
			Status:           veStatusActive,
			OnlineStatus:     onlineStatus,
			RegisteredAt:     machine.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:        machine.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func appendMobileMachineDigitalEmployees(employees []digitalEmployeeEntry, machineEmployees []digitalEmployeeEntry) []digitalEmployeeEntry {
	if len(machineEmployees) == 0 {
		return employees
	}
	seen := make(map[string]struct{}, len(employees)*2)
	for _, employee := range employees {
		for _, value := range []string{employee.ID, employee.MachineID} {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" {
				continue
			}
			seen[value] = struct{}{}
			if !strings.HasPrefix(value, "ve_") {
				seen["ve_"+value] = struct{}{}
			}
		}
	}
	for _, employee := range machineEmployees {
		id := strings.ToLower(strings.TrimSpace(employee.ID))
		machineID := strings.ToLower(strings.TrimSpace(employee.MachineID))
		if id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
		}
		if machineID != "" {
			if _, ok := seen[machineID]; ok {
				continue
			}
		}
		employees = append(employees, employee)
	}
	return employees
}
