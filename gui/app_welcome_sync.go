package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WelcomeSyncRequest carries optional Hub overrides (defaults to current login).
type WelcomeSyncRequest struct {
	HubURL   string `json:"hub_url,omitempty"`
	HubToken string `json:"hub_token,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
	Email    string `json:"email,omitempty"`
}

// WelcomeSyncStatus mirrors Hub WelcomeSyncView.
type WelcomeSyncStatus struct {
	OwnerUserID     string `json:"owner_user_id,omitempty"`
	OwnerUserEmail  string `json:"owner_user_email,omitempty"`
	TenantID        string `json:"tenant_id,omitempty"`
	HasDocument     bool   `json:"has_document"`
	Revision        string `json:"revision,omitempty"`
	StoredSizeBytes int64  `json:"stored_size_bytes,omitempty"`
	TemplateCount   int    `json:"template_count,omitempty"`
	Kind            string `json:"kind,omitempty"`
	ExportedAt      string `json:"exported_at,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	LimitBytes      int64  `json:"limit_bytes,omitempty"`
	Message         string `json:"message,omitempty"`
	// Local helper fields (not from Hub)
	LoggedIn bool   `json:"logged_in"`
	HubURL   string `json:"hub_url,omitempty"`
	Error    string `json:"error,omitempty"`
}

// WelcomeSyncPushRequest uploads a welcome export JSON string to Hub.
type WelcomeSyncPushRequest struct {
	WelcomeSyncRequest
	// PayloadJSON is the full welcome export document (stringified JSON).
	PayloadJSON string `json:"payload_json"`
	// IfMatchRevision enables optimistic concurrency against Hub.
	IfMatchRevision string `json:"if_match_revision,omitempty"`
}

// WelcomeSyncPullResult returns the cloud document as a JSON string for frontend import.
type WelcomeSyncPullResult struct {
	WelcomeSyncStatus
	PayloadJSON string `json:"payload_json,omitempty"`
}

// WelcomeSyncStatus queries Hub for the current user's welcome cloud document.
func (a *App) WelcomeSyncStatus(req WelcomeSyncRequest) (WelcomeSyncStatus, error) {
	req = a.fillWelcomeSyncIdentity(req)
	hubURL, token, err := a.resolveWelcomeSyncHub(req)
	if err != nil {
		// Soft failure: not logged in / no hub → report not logged in instead of hard error.
		return WelcomeSyncStatus{
			LoggedIn: false,
			Error:    err.Error(),
		}, nil
	}
	statusCode, body, err := a.doWelcomeSyncRequest(req, hubURL, token, http.MethodGet, "/api/welcome/sync/status", nil, 20*time.Second)
	if err != nil {
		// Soft-fail network/DNS blips so the welcome page still renders.
		return WelcomeSyncStatus{
			LoggedIn: false,
			HubURL:   hubURL,
			Error:    err.Error(),
		}, nil
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return WelcomeSyncStatus{LoggedIn: false, HubURL: hubURL, Error: strings.TrimSpace(string(body))}, nil
	}
	// Older hubs without welcome-sync routes.
	if statusCode == http.StatusNotFound {
		return WelcomeSyncStatus{
			LoggedIn: true,
			HubURL:   hubURL,
			Error:    "hub does not support welcome sync (upgrade hub)",
		}, nil
	}
	if statusCode < 200 || statusCode >= 300 {
		return WelcomeSyncStatus{}, fmt.Errorf("hub returned %d: %s", statusCode, strings.TrimSpace(string(body)))
	}
	var status WelcomeSyncStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return WelcomeSyncStatus{}, fmt.Errorf("decode welcome sync status: %w", err)
	}
	status.LoggedIn = true
	status.HubURL = hubURL
	return status, nil
}

// WelcomeSyncPush uploads local welcome export JSON to Hub (overwrite, optional if-match).
func (a *App) WelcomeSyncPush(req WelcomeSyncPushRequest) (WelcomeSyncStatus, error) {
	req.WelcomeSyncRequest = a.fillWelcomeSyncIdentity(req.WelcomeSyncRequest)
	payloadJSON := strings.TrimSpace(req.PayloadJSON)
	if payloadJSON == "" {
		return WelcomeSyncStatus{}, fmt.Errorf("payload_json is required")
	}
	// Validate JSON before talking to Hub.
	var probe any
	if err := json.Unmarshal([]byte(payloadJSON), &probe); err != nil {
		return WelcomeSyncStatus{}, fmt.Errorf("payload_json is not valid JSON: %w", err)
	}
	hubURL, token, err := a.resolveWelcomeSyncHub(req.WelcomeSyncRequest)
	if err != nil {
		return WelcomeSyncStatus{}, err
	}
	bodyMap := map[string]any{
		"payload": probe,
	}
	if rev := strings.TrimSpace(req.IfMatchRevision); rev != "" {
		bodyMap["if_match_revision"] = rev
	}
	bodyMap["client_updated_at"] = time.Now().UTC().Format(time.RFC3339)
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return WelcomeSyncStatus{}, err
	}
	statusCode, respBody, err := a.doWelcomeSyncRequest(req.WelcomeSyncRequest, hubURL, token, http.MethodPut, "/api/welcome/sync", body, 30*time.Second)
	if err != nil {
		return WelcomeSyncStatus{}, fmt.Errorf("upload welcome sync: %w", err)
	}
	if statusCode == http.StatusConflict {
		return WelcomeSyncStatus{}, fmt.Errorf("cloud conflict: document was updated on another device (revision mismatch)")
	}
	if statusCode == http.StatusNotFound {
		return WelcomeSyncStatus{}, fmt.Errorf("hub does not support welcome sync (upgrade hub)")
	}
	if statusCode < 200 || statusCode >= 300 {
		return WelcomeSyncStatus{}, fmt.Errorf("hub returned %d: %s", statusCode, strings.TrimSpace(string(respBody)))
	}
	var status WelcomeSyncStatus
	if err := json.Unmarshal(respBody, &status); err != nil {
		return WelcomeSyncStatus{}, fmt.Errorf("decode welcome sync upload: %w", err)
	}
	status.LoggedIn = true
	status.HubURL = hubURL
	return status, nil
}

// WelcomeSyncPull downloads the cloud welcome document for frontend merge/replace import.
// Single GET — no prior status round-trip.
func (a *App) WelcomeSyncPull(req WelcomeSyncRequest) (WelcomeSyncPullResult, error) {
	req = a.fillWelcomeSyncIdentity(req)
	hubURL, token, err := a.resolveWelcomeSyncHub(req)
	if err != nil {
		return WelcomeSyncPullResult{}, err
	}
	statusCode, body, header, err := a.doWelcomeSyncRequestWithHeader(req, hubURL, token, http.MethodGet, "/api/welcome/sync", nil, 30*time.Second)
	if err != nil {
		return WelcomeSyncPullResult{}, fmt.Errorf("download welcome sync: %w", err)
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return WelcomeSyncPullResult{
			WelcomeSyncStatus: WelcomeSyncStatus{LoggedIn: false, HubURL: hubURL, Error: strings.TrimSpace(string(body))},
		}, fmt.Errorf("hub login required")
	}
	if statusCode == http.StatusNotFound {
		// Distinguish "no document" vs "route missing": body often has WELCOME_SYNC_NOT_FOUND.
		msg := strings.ToLower(string(body))
		if strings.Contains(msg, "welcome_sync_not_found") || strings.Contains(msg, "no cloud welcome") {
			return WelcomeSyncPullResult{
				WelcomeSyncStatus: WelcomeSyncStatus{LoggedIn: true, HubURL: hubURL, HasDocument: false},
			}, fmt.Errorf("no cloud welcome document")
		}
		// Generic 404 — likely old hub without the route.
		if !strings.Contains(msg, "welcome") {
			return WelcomeSyncPullResult{
				WelcomeSyncStatus: WelcomeSyncStatus{LoggedIn: true, HubURL: hubURL, Error: "hub does not support welcome sync"},
			}, fmt.Errorf("hub does not support welcome sync (upgrade hub)")
		}
		return WelcomeSyncPullResult{
			WelcomeSyncStatus: WelcomeSyncStatus{LoggedIn: true, HubURL: hubURL, HasDocument: false},
		}, fmt.Errorf("no cloud welcome document")
	}
	if statusCode < 200 || statusCode >= 300 {
		return WelcomeSyncPullResult{}, fmt.Errorf("hub returned %d: %s", statusCode, strings.TrimSpace(string(body)))
	}
	// Ensure valid JSON.
	var probe any
	if err := json.Unmarshal(body, &probe); err != nil {
		return WelcomeSyncPullResult{}, fmt.Errorf("cloud document is not valid JSON: %w", err)
	}
	// Compact re-encode for stable frontend parsing.
	compact, err := json.Marshal(probe)
	if err != nil {
		return WelcomeSyncPullResult{}, err
	}
	status := WelcomeSyncStatus{
		LoggedIn:    true,
		HubURL:      hubURL,
		HasDocument: true,
	}
	if rev := strings.TrimSpace(header.Get("X-Welcome-Sync-Revision")); rev != "" {
		status.Revision = rev
	}
	if updated := strings.TrimSpace(header.Get("X-Welcome-Sync-Updated-At")); updated != "" {
		status.UpdatedAt = updated
	}
	if countRaw := strings.TrimSpace(header.Get("X-Welcome-Sync-Template-Count")); countRaw != "" {
		if n, err := strconv.Atoi(countRaw); err == nil && n >= 0 {
			status.TemplateCount = n
		}
	}
	// Best-effort template count / revision from payload when headers missing.
	if m, ok := probe.(map[string]any); ok {
		if status.TemplateCount == 0 {
			if templates, ok := m["templates"].([]any); ok {
				status.TemplateCount = len(templates)
			}
		}
	}
	if status.Revision == "" {
		// Content hash fallback so optimistic concurrency still works after pull.
		sum := sha256.Sum256(compact)
		status.Revision = hex.EncodeToString(sum[:])
	}
	return WelcomeSyncPullResult{
		WelcomeSyncStatus: status,
		PayloadJSON:       string(compact),
	}, nil
}

// WelcomeSyncDelete removes the cloud welcome document.
func (a *App) WelcomeSyncDelete(req WelcomeSyncRequest) (WelcomeSyncStatus, error) {
	req = a.fillWelcomeSyncIdentity(req)
	hubURL, token, err := a.resolveWelcomeSyncHub(req)
	if err != nil {
		return WelcomeSyncStatus{}, err
	}
	statusCode, body, err := a.doWelcomeSyncRequest(req, hubURL, token, http.MethodDelete, "/api/welcome/sync", nil, 20*time.Second)
	if err != nil {
		return WelcomeSyncStatus{}, fmt.Errorf("delete welcome sync: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 {
		return WelcomeSyncStatus{}, fmt.Errorf("hub returned %d: %s", statusCode, strings.TrimSpace(string(body)))
	}
	return a.WelcomeSyncStatus(req)
}

func (a *App) doWelcomeSyncRequest(
	req WelcomeSyncRequest,
	hubURL, token, method, path string,
	body []byte,
	timeout time.Duration,
) (int, []byte, error) {
	code, respBody, _, err := a.doWelcomeSyncRequestWithHeader(req, hubURL, token, method, path, body, timeout)
	return code, respBody, err
}

func (a *App) doWelcomeSyncRequestWithHeader(
	req WelcomeSyncRequest,
	hubURL, token, method, path string,
	body []byte,
	timeout time.Duration,
) (int, []byte, http.Header, error) {
	ctx, cancel := context.WithTimeout(a.welcomeSyncContext(), timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, hubURL+path, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", knowledgeShareBearerToken(token))
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	setWelcomeSyncIdentityHeaders(httpReq, req)
	resp, err := hubHTTPClient.Do(httpReq)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0, nil, nil, err
	}
	// Clone headers before body close (header map remains valid).
	header := resp.Header.Clone()
	return resp.StatusCode, respBody, header, nil
}

func (a *App) fillWelcomeSyncIdentity(req WelcomeSyncRequest) WelcomeSyncRequest {
	cfg, _ := a.LoadConfig()
	if strings.TrimSpace(req.TenantID) == "" {
		req.TenantID = strings.TrimSpace(cfg.RemoteTenantID)
	}
	if strings.TrimSpace(req.Email) == "" {
		req.Email = strings.TrimSpace(cfg.RemoteEmail)
	}
	if strings.TrimSpace(req.HubURL) == "" {
		req.HubURL = strings.TrimSpace(cfg.RemoteHubURL)
	}
	if strings.TrimSpace(req.HubToken) == "" {
		req.HubToken = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	return req
}

func (a *App) resolveWelcomeSyncHub(req WelcomeSyncRequest) (string, string, error) {
	hubURL := strings.TrimRight(strings.TrimSpace(req.HubURL), "/")
	if hubURL == "" {
		return "", "", fmt.Errorf("hub login required (hub_url missing)")
	}
	if parsed, err := url.Parse(hubURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("hub_url must be an absolute URL")
	}
	token := strings.TrimSpace(req.HubToken)
	if token == "" {
		return "", "", fmt.Errorf("hub login required (viewer token missing)")
	}
	return hubURL, token, nil
}

func setWelcomeSyncIdentityHeaders(httpReq *http.Request, req WelcomeSyncRequest) {
	if httpReq == nil {
		return
	}
	if tenantID := strings.TrimSpace(req.TenantID); tenantID != "" {
		httpReq.Header.Set("X-Maclaw-Tenant-ID", tenantID)
	}
	if email := strings.TrimSpace(req.Email); email != "" {
		httpReq.Header.Set("X-Maclaw-User-Email", email)
	}
}

func (a *App) welcomeSyncContext() context.Context {
	if a != nil {
		return a.knowledgeContext()
	}
	return context.Background()
}
