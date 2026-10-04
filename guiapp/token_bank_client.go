package guiapp

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Token Bank client bindings (design doc §6.1).
//
// These call the HubCenter directory server, not the local Hub. The GUI cannot
// use a plain `fetch` for that: the session bearer token lives in the local
// config and is deliberately not exposed to the webview, so every HubCenter
// call is proxied through a Wails method that attaches it server-side.
//
// `cfg.SkillMarketSessionToken` is the same credential the problem-report calls
// use, and it is what `tokenBankSessionUser` on the server resolves.

const tokenBankClientTimeout = 20 * time.Second

// tokenBankPubKeyCache memoizes the HubCenter public key for the process
// lifetime. The key pair is generated once per deployment and does not rotate,
// so a per-share fetch would only add latency. Guarded because data-plane
// bindings can be invoked from more than one goroutine.
var (
	tokenBankPubKeyMu sync.Mutex
	tokenBankPubKey   []byte
)

// tokenBankClientDo issues an authenticated HubCenter request and decodes a
// JSON object response.
//
// Every path that can fail returns a message safe to show the user: the raw
// HubCenter body is included because it carries the server's own error code
// (`token_bank_unavailable`, `identity_not_verified`, ...), which is far more
// actionable than "request failed". It is never used to decide control flow.
func (a *App) tokenBankClientDo(method, path string, body any) (map[string]interface{}, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
	token := strings.TrimSpace(cfg.SkillMarketSessionToken)
	if base == "" || token == "" {
		return nil, fmt.Errorf("please sign in to HubCenter to use the Token Bank")
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: tokenBankClientTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		detail := strings.TrimSpace(string(data))
		if detail == "" {
			detail = resp.Status
		}
		return nil, &tokenBankHTTPError{Status: resp.StatusCode, Detail: detail}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]interface{}{}, nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("HubCenter returned an unreadable response")
	}
	return result, nil
}

// TokenBankSummary returns the caller's credit balance card.
//
// GET /api/v1/token-bank/summary
func (a *App) TokenBankSummary() (map[string]interface{}, error) {
	return a.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/summary", nil)
}

// TokenBankListShares returns the caller's own shares. The server scopes the
// list to the session user, so this cannot read somebody else's shares.
//
// rangeKind is today, month, or all. An empty value omits the query and the
// server treats that as all. Any other value is also omitted: sending it would
// only produce a 400 from a server that has learned the parameter.
//
// GET /api/v1/token-bank/shares?range=today|month|all
func (a *App) TokenBankListShares(rangeKind string) (map[string]interface{}, error) {
	return a.tokenBankClientDo(http.MethodGet, tokenBankRangePath("/api/v1/token-bank/shares", rangeKind), nil)
}

// TokenBankListShareModels returns the models inside one of the caller's
// shares, plus the settled credits for rangeKind. A share id that is not the
// caller's reads as not-found server-side.
//
// GET /api/v1/token-bank/shares/{id}/models?range=today|month|all
func (a *App) TokenBankListShareModels(shareID, rangeKind string) (map[string]interface{}, error) {
	id := strings.TrimSpace(shareID)
	if id == "" {
		return nil, fmt.Errorf("share id is required")
	}
	return a.tokenBankClientDo(http.MethodGet, tokenBankRangePath("/api/v1/token-bank/shares/"+url.PathEscape(id)+"/models", rangeKind), nil)
}

func tokenBankRangePath(path, rangeKind string) string {
	switch strings.ToLower(strings.TrimSpace(rangeKind)) {
	case "today", "month", "all":
		return path + "?range=" + strings.ToLower(strings.TrimSpace(rangeKind))
	default:
		return path
	}
}

// TokenBankSetSharePaused pauses or resumes every model of a share.
//
// PUT /api/v1/token-bank/shares/{id}/paused
func (a *App) TokenBankSetSharePaused(shareID string, paused bool) (map[string]interface{}, error) {
	id := strings.TrimSpace(shareID)
	if id == "" {
		return nil, fmt.Errorf("share id is required")
	}
	return a.tokenBankClientDo(http.MethodPut, "/api/v1/token-bank/shares/"+url.PathEscape(id)+"/paused", map[string]any{"paused": paused})
}

// TokenBankTakeOutShare withdraws a share from the bank permanently.
//
// DELETE /api/v1/token-bank/shares/{id}
//
// The caller must confirm first: the server has no undo for this, and the
// member ids it frees are not recoverable by re-submitting the same key.
func (a *App) TokenBankTakeOutShare(shareID string) (map[string]interface{}, error) {
	id := strings.TrimSpace(shareID)
	if id == "" {
		return nil, fmt.Errorf("share id is required")
	}
	return a.tokenBankClientDo(http.MethodDelete, "/api/v1/token-bank/shares/"+url.PathEscape(id), nil)
}

// TokenBankListWithdrawals returns the caller's withdrawal history.
//
// GET /api/v1/token-bank/credits/withdrawals
//
// The desktop pages twenty cards at a time, so it asks for the server maximum
// (tokenBankMaxListLimit). A missing limit would stop at the default 100 and
// hide older rows behind a pager that looks complete.
func (a *App) TokenBankListWithdrawals() (map[string]interface{}, error) {
	return a.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/credits/withdrawals?limit=500", nil)
}

// TokenBankGetAutoSettings reads this machine's ceiling for one automatic
// pull. max_per_withdraw_micro 0 means no local ceiling.
func (a *App) TokenBankGetAutoSettings() (map[string]interface{}, error) {
	return a.tokenBankHubSettings(http.MethodGet, nil)
}

// TokenBankSaveAutoSettings stores the ceiling. Zero clears it. A positive
// value is microcredits, the same unit as a withdrawal.
func (a *App) TokenBankSaveAutoSettings(maxPerWithdrawMicro int64) (map[string]interface{}, error) {
	if maxPerWithdrawMicro < 0 {
		return nil, fmt.Errorf("automatic withdrawal cap must not be negative")
	}
	return a.tokenBankHubSettings(http.MethodPut, map[string]any{
		"max_per_withdraw_micro": maxPerWithdrawMicro,
	})
}

func (a *App) tokenBankHubSettings(method string, body any) (map[string]interface{}, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	token := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || token == "" {
		return nil, fmt.Errorf("connect to this Hub before changing automatic Token Bank withdrawal")
	}
	result, status, callErr := a.tokenBankHubCall(method, hubURL+"/api/token-bank/auto", token, body)
	// An older hub has no /api/token-bank/auto route. Its 404 body is often
	// plain text, which fails JSON decoding, so the status has to win over
	// that decode error. The bank page stays usable without this setting.
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		return nil, errTokenBankAutoCapUnsupported
	}
	if callErr != nil {
		return nil, callErr
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return result, nil
	}
	action := "load"
	if method == http.MethodPut {
		action = "save"
	}
	return nil, fmt.Errorf("could not %s the automatic withdrawal cap: %s", action, tokenBankSettingsDetail(result, status))
}

// errTokenBankAutoCapUnsupported is the stable sentence the Token Bank page
// matches when this hub build cannot store the ceiling.
var errTokenBankAutoCapUnsupported = errors.New("this Hub does not support the automatic withdrawal cap yet")

func tokenBankSettingsDetail(result map[string]interface{}, status int) string {
	code := tokenBankJSONCode(result)
	message := ""
	if result != nil {
		message, _ = result["message"].(string)
		message = strings.TrimSpace(message)
	}
	switch {
	case code != "" && message != "":
		return code + ": " + message
	case code != "":
		return code
	case message != "":
		return message
	case status > 0:
		return http.StatusText(status)
	default:
		return "request failed"
	}
}

// TokenBankShareWindowInput is when one shared model may be dialed.
// Days use Go weekday numbers: 0=Sunday ... 6=Saturday. Empty Days means
// every day. Start and End are Asia/Shanghai "HH:MM". End is exclusive, and
// "24:00" is the end of the day. A nil window means always available.
// This is not a billing schedule.
type TokenBankShareWindowInput struct {
	Days  []int  `json:"days,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// TokenBankShareModelInput is one model row the GUI submits with a share.
//
// `Available` is the probe verdict: only available models are actually
// published, but an unavailable one is still recorded so the owner can see why
// it is missing instead of finding it silently absent.
type TokenBankShareModelInput struct {
	Model        string                     `json:"model"`
	Available    bool                       `json:"available"`
	ProbeError   string                     `json:"probe_error"`
	InputTokens  int64                      `json:"used_input_tokens"`
	OutputTokens int64                      `json:"used_output_tokens"`
	ShareWindow  *TokenBankShareWindowInput `json:"share_window,omitempty"`
}

// TokenBankShareInput is the Wails argument for a share submission. Wails
// decodes it with encoding/json, so these names must match the dialog payload.
// json:"-" drops every field and the call fails with "provider name is required"
// even though the dialog is showing the name.
//
// The plaintext key stays in APIKey only inside this process. TokenBankCreateShare
// builds the Hub Center body by hand and sends the encrypted envelope, not this struct.
type TokenBankShareInput struct {
	DisplayName      string                     `json:"DisplayName"`
	APIURL           string                     `json:"APIURL"`
	APIKey           string                     `json:"APIKey"`
	Protocol         string                     `json:"Protocol"`
	KeyFingerprint   string                     `json:"KeyFingerprint"`
	Models           []TokenBankShareModelInput `json:"Models"`
	MaxInputTokens   int64                      `json:"MaxInputTokens"`
	MaxOutputTokens  int64                      `json:"MaxOutputTokens"`
	ClientInstanceID string                     `json:"ClientInstanceID"`
	// Visibility is "public" or "private". Empty means public.
	Visibility string `json:"Visibility"`
	// HubIDs and TenantIDs are comma-separated allow-list ids. Equal counts
	// are paired by index, unless SeparateAudiences is set.
	HubIDs    string `json:"HubIDs"`
	TenantIDs string `json:"TenantIDs"`
	// SeparateAudiences keeps every selected hub and every selected tenant as
	// its own allow row. The share dialog sets this so picking two hubs and
	// two tenants does not become two accidental pairs.
	SeparateAudiences bool `json:"SeparateAudiences"`
}

// TokenBankVisibilityAudience is one allow row for a share that already exists.
// A row may name a hub, a tenant, or both. Both means the call must match the
// pair. The access dialog sends these rows; it does not join them with commas.
type TokenBankVisibilityAudience struct {
	HubID    string `json:"hub_id"`
	TenantID string `json:"tenant_id"`
}

// MarshalJSON keeps the Wails field names but never writes the plaintext key.
// Unmarshal still accepts APIKey; only the outbound encoding strips it.
func (in TokenBankShareInput) MarshalJSON() ([]byte, error) {
	type plain TokenBankShareInput
	body, err := json.Marshal(plain(in))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	delete(fields, "APIKey")
	return json.Marshal(fields)
}

// TokenBankCreateShare publishes a provider to the bank.
//
// POST /api/v1/token-bank/shares
//
// The key never leaves this process in plaintext: it is wrapped in a
// client-side envelope (token_bank_envelope.go) against the HubCenter public
// key before the request is built. That is why the key is passed here rather
// than being part of a JSON body the caller assembles.
func (a *App) TokenBankCreateShare(input TokenBankShareInput) (map[string]interface{}, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	apiURL := strings.TrimSpace(input.APIURL)
	apiKey := strings.TrimSpace(input.APIKey)
	fingerprint := strings.TrimSpace(input.KeyFingerprint)
	if displayName == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	if apiURL == "" {
		return nil, fmt.Errorf("provider URL is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("provider key is required")
	}
	if fingerprint == "" {
		// The server treats this as part of the idempotency key, so an empty
		// value would collapse every retry onto the same submission.
		return nil, fmt.Errorf("key fingerprint is required")
	}
	if len(input.Models) == 0 {
		return nil, fmt.Errorf("select at least one model to share")
	}
	visibility, audiences, err := tokenBankAccessBodyMode(input.Visibility, input.HubIDs, input.TenantIDs, input.SeparateAudiences)
	if err != nil {
		return nil, err
	}

	publicKey, err := a.tokenBankPublicKey()
	if err != nil {
		return nil, err
	}
	encrypted, err := tokenBankEncryptKey(publicKey, tokenBankKeyPayload{
		APIKey:   apiKey,
		APIURL:   apiURL,
		Protocol: strings.TrimSpace(input.Protocol),
	})
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"client_instance_id":            strings.TrimSpace(input.ClientInstanceID),
		"display_name":                  displayName,
		"api_url":                       apiURL,
		"protocol":                      strings.TrimSpace(input.Protocol),
		"key_fingerprint":               fingerprint,
		"models":                        input.Models,
		"encrypted_payload":             encrypted,
		"max_input_tokens_per_request":  input.MaxInputTokens,
		"max_output_tokens_per_request": input.MaxOutputTokens,
		"visibility":                    visibility,
	}
	if visibility == "private" {
		body["audiences"] = audiences
	}
	return a.tokenBankClientDo(http.MethodPost, "/api/v1/token-bank/shares", body)
}

// TokenBankSyncShareModels updates which models an existing share publishes.
//
// PUT /api/v1/token-bank/shares/{id}/models
//
// The share already holds the upstream key. This call sends the probed model
// list and each model's dial window. It does not send the key again.
func (a *App) TokenBankSyncShareModels(shareID string, models []TokenBankShareModelInput) (map[string]interface{}, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return nil, fmt.Errorf("share id is required")
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("select at least one model to share")
	}
	for _, model := range models {
		if strings.TrimSpace(model.Model) == "" {
			return nil, fmt.Errorf("select at least one model to share")
		}
	}
	return a.tokenBankClientDo(http.MethodPut, "/api/v1/token-bank/shares/"+url.PathEscape(shareID)+"/models", map[string]any{
		"models": models,
	})
}

// TokenBankAddShareKey appends an upstream key to an existing share. Dispatch
// rotates it with the primary key. The plaintext key is encrypted first.
//
// POST /api/v1/token-bank/shares/{id}/keys
func (a *App) TokenBankAddShareKey(shareID, apiURL, apiKey, protocol, keyFingerprint string) (map[string]interface{}, error) {
	shareID = strings.TrimSpace(shareID)
	apiURL = strings.TrimSpace(apiURL)
	apiKey = strings.TrimSpace(apiKey)
	fingerprint := strings.TrimSpace(keyFingerprint)
	if shareID == "" {
		return nil, fmt.Errorf("share id is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("provider key is required")
	}
	if fingerprint == "" {
		return nil, fmt.Errorf("key fingerprint is required")
	}
	publicKey, err := a.tokenBankPublicKey()
	if err != nil {
		return nil, err
	}
	encrypted, err := tokenBankEncryptKey(publicKey, tokenBankKeyPayload{
		APIKey:   apiKey,
		APIURL:   apiURL,
		Protocol: strings.TrimSpace(protocol),
	})
	if err != nil {
		return nil, err
	}
	return a.tokenBankClientDo(http.MethodPost, "/api/v1/token-bank/shares/"+url.PathEscape(shareID)+"/keys", map[string]any{
		"encrypted_payload": encrypted,
		"key_fingerprint":   fingerprint,
	})
}

// TokenBankSetShareVisibility changes who may call a share.
//
// audiences is the access dialog's selection. A private share needs at least
// one row. A row with both ids stays one pair; it is not split into a hub-only
// grant and a tenant-only grant.
//
// PUT /api/v1/token-bank/shares/{id}/visibility
func (a *App) TokenBankSetShareVisibility(shareID, visibility string, audiences []TokenBankVisibilityAudience) (map[string]interface{}, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return nil, fmt.Errorf("share id is required")
	}
	normalized, rows, err := tokenBankVisibilityRows(visibility, audiences)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"visibility": normalized}
	if normalized == "private" {
		body["audiences"] = rows
	}
	return a.tokenBankClientDo(http.MethodPut, "/api/v1/token-bank/shares/"+url.PathEscape(shareID)+"/visibility", body)
}

// tokenBankVisibilityRows keeps each dialog row intact. Blank rows are dropped.
// The same hub and tenant in a different case are one row.
func tokenBankVisibilityRows(visibility string, in []TokenBankVisibilityAudience) (string, []map[string]string, error) {
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" {
		return "", nil, fmt.Errorf("visibility must be public or private")
	}
	rows := make([]map[string]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		hub := strings.TrimSpace(item.HubID)
		tenant := strings.TrimSpace(item.TenantID)
		if hub == "" && tenant == "" {
			continue
		}
		if len(hub) > 128 || len(tenant) > 128 {
			return "", nil, fmt.Errorf("token bank audience id is too long")
		}
		key := strings.ToLower(hub) + "\x00" + strings.ToLower(tenant)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		row := map[string]string{}
		if hub != "" {
			row["hub_id"] = hub
		}
		if tenant != "" {
			row["tenant_id"] = tenant
		}
		rows = append(rows, row)
	}
	if visibility == "private" && len(rows) == 0 {
		return "", nil, fmt.Errorf("a private share needs at least one hub or tenant")
	}
	if visibility == "private" && len(rows) > tokenBankMaxAudiences {
		return "", nil, fmt.Errorf("a private share can name at most %d hubs and tenants", tokenBankMaxAudiences)
	}
	return visibility, rows, nil
}

// tokenBankAccessBody pairs equal-length hub and tenant lists by index.
// Creating a share can still pass comma lists. Changing access does not:
// TokenBankSetShareVisibility takes audience rows.
func tokenBankAccessBody(visibility, hubs, tenants string) (string, []map[string]string, error) {
	return tokenBankAccessBodyMode(visibility, hubs, tenants, false)
}

// tokenBankAccessBodyMode is tokenBankAccessBody with an explicit pairing
// switch. separate is the checkbox picker: each hub and each tenant is its
// own row, even when the two lists have the same length.
func tokenBankAccessBodyMode(visibility, hubs, tenants string, separate bool) (string, []map[string]string, error) {
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" {
		return "", nil, fmt.Errorf("visibility must be public or private")
	}
	var rows []map[string]string
	if separate {
		rows = tokenBankSeparateAudienceRows(hubs, tenants)
	} else {
		rows = tokenBankAudienceRows(hubs, tenants)
	}
	if visibility == "private" && len(rows) == 0 {
		return "", nil, fmt.Errorf("a private share needs at least one hub or tenant")
	}
	if visibility == "private" && len(rows) > tokenBankMaxAudiences {
		return "", nil, fmt.Errorf("a private share can name at most %d hubs and tenants", tokenBankMaxAudiences)
	}
	return visibility, rows, nil
}

// tokenBankMaxAudiences matches hubcenter TokenBankMaxAudiences. The desktop
// rejects an over-long list before it encrypts the provider key.
const tokenBankMaxAudiences = 32

// tokenBankHTTPError keeps the HubCenter status code without changing the
// message callers already show to the user.
type tokenBankHTTPError struct {
	Status int
	Detail string
}

func (e *tokenBankHTTPError) Error() string {
	if e == nil {
		return "HubCenter rejected the request"
	}
	return fmt.Sprintf("HubCenter rejected the request: %s", e.Detail)
}

func tokenBankSplitIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func tokenBankAudienceRows(hubs, tenants string) []map[string]string {
	hubList := tokenBankSplitIDs(hubs)
	tenantList := tokenBankSplitIDs(tenants)
	rows := make([]map[string]string, 0, len(hubList)+len(tenantList))
	if len(hubList) > 0 && len(hubList) == len(tenantList) {
		for i := range hubList {
			rows = append(rows, map[string]string{"hub_id": hubList[i], "tenant_id": tenantList[i]})
		}
		return rows
	}
	for _, hub := range hubList {
		rows = append(rows, map[string]string{"hub_id": hub})
	}
	for _, tenant := range tenantList {
		rows = append(rows, map[string]string{"tenant_id": tenant})
	}
	return rows
}

// tokenBankSeparateAudienceRows emits hub-only rows, then tenant-only rows.
// A checked hub and a checked tenant are two grants, not one pair.
func tokenBankSeparateAudienceRows(hubs, tenants string) []map[string]string {
	hubList := tokenBankSplitIDs(hubs)
	tenantList := tokenBankSplitIDs(tenants)
	rows := make([]map[string]string, 0, len(hubList)+len(tenantList))
	for _, hub := range hubList {
		rows = append(rows, map[string]string{"hub_id": hub})
	}
	for _, tenant := range tenantList {
		rows = append(rows, map[string]string{"tenant_id": tenant})
	}
	return rows
}

// TokenBankListShareAudiences lists the hubs and tenants the signed-in
// account can grant on a private share.
//
// GET /api/v1/token-bank/audiences
//
// The machine's current hub and tenant are merged in. A HubCenter that does
// not serve this route yet (HTTP 404) still returns that local pair. Other
// failures stay visible, so a sign-in or server error is not replaced by a
// one-item list.
func (a *App) TokenBankListShareAudiences() (map[string]interface{}, error) {
	hubID, tenantID, tenantName := "", "", ""
	if cfg, err := a.LoadConfig(); err == nil {
		hubID = strings.TrimSpace(cfg.RemoteHubID)
		tenantID = strings.TrimSpace(cfg.RemoteTenantID)
		tenantName = strings.TrimSpace(cfg.RemoteTenantName)
	}
	listed, err := a.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/audiences", nil)
	return tokenBankAudienceListOrLocal(listed, err, hubID, tenantID, tenantName)
}

// tokenBankAudienceListOrLocal merges the account list with the machine's
// current hub and tenant. Only a missing route falls back to that local pair.
func tokenBankAudienceListOrLocal(listed map[string]interface{}, err error, hubID, tenantID, tenantName string) (map[string]interface{}, error) {
	if err == nil {
		return mergeLocalTokenBankAudiences(listed, hubID, tenantID, tenantName), nil
	}
	var httpErr *tokenBankHTTPError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound && (strings.TrimSpace(hubID) != "" || strings.TrimSpace(tenantID) != "") {
		return mergeLocalTokenBankAudiences(nil, hubID, tenantID, tenantName), nil
	}
	return nil, err
}

// mergeLocalTokenBankAudiences adds the desktop's current hub and tenant when
// the account list does not already contain them. Comparison is
// case-insensitive. A local hub has no display name in config.
func mergeLocalTokenBankAudiences(remote map[string]interface{}, hubID, tenantID, tenantName string) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range remote {
		out[key] = value
	}
	hubs := audienceChoiceList(out["hubs"])
	tenants := audienceChoiceList(out["tenants"])
	hubID = strings.TrimSpace(hubID)
	tenantID = strings.TrimSpace(tenantID)
	tenantName = strings.TrimSpace(tenantName)
	if hubID != "" && !audienceListHasID(hubs, hubID) {
		hubs = append(hubs, map[string]interface{}{"id": hubID, "name": ""})
	}
	if tenantID != "" && !audienceListHasID(tenants, tenantID) {
		row := map[string]interface{}{"id": tenantID, "name": tenantName}
		if hubID != "" {
			row["hub_id"] = hubID
		}
		tenants = append(tenants, row)
	}
	out["hubs"] = hubs
	out["tenants"] = tenants
	return out
}

func audienceChoiceList(value interface{}) []map[string]interface{} {
	raw, ok := value.([]interface{})
	if !ok || len(raw) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := row["id"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

func audienceListHasID(rows []map[string]interface{}, id string) bool {
	want := strings.ToLower(strings.TrimSpace(id))
	if want == "" {
		return false
	}
	for _, row := range rows {
		got, _ := row["id"].(string)
		if strings.ToLower(strings.TrimSpace(got)) == want {
			return true
		}
	}
	return false
}

// TokenBankRotateShareKey replaces the upstream key on an existing share.
//
// PUT /api/v1/token-bank/shares/{id}/key
//
// The share id and its earned credits stay. The plaintext key is encrypted
// with the HubCenter public key and is not written into the JSON body.
func (a *App) TokenBankRotateShareKey(shareID, apiURL, apiKey, protocol, keyFingerprint string) (map[string]interface{}, error) {
	shareID = strings.TrimSpace(shareID)
	apiURL = strings.TrimSpace(apiURL)
	apiKey = strings.TrimSpace(apiKey)
	fingerprint := strings.TrimSpace(keyFingerprint)
	if shareID == "" {
		return nil, fmt.Errorf("share id is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("provider key is required")
	}
	if fingerprint == "" {
		return nil, fmt.Errorf("key fingerprint is required")
	}
	publicKey, err := a.tokenBankPublicKey()
	if err != nil {
		return nil, err
	}
	encrypted, err := tokenBankEncryptKey(publicKey, tokenBankKeyPayload{
		APIKey:   apiKey,
		APIURL:   apiURL,
		Protocol: strings.TrimSpace(protocol),
	})
	if err != nil {
		return nil, err
	}
	return a.tokenBankClientDo(http.MethodPut, "/api/v1/token-bank/shares/"+url.PathEscape(shareID)+"/key", map[string]any{
		"encrypted_payload": encrypted,
		"key_fingerprint":   fingerprint,
		"api_url":           apiURL,
		"protocol":          strings.TrimSpace(protocol),
	})
}

// tokenBankPublicKey returns the HubCenter RSA public key PEM.
//
// GET /api/v1/crypto/pubkey
//
// Cached in memory for the process lifetime and on disk next to the
// skillmarket key: the key pair is generated once per deployment and does not
// rotate, so re-fetching per share would be pure latency. A failure to read the
// cache falls through to a fresh fetch rather than failing.
func (a *App) tokenBankPublicKey() ([]byte, error) {
	tokenBankPubKeyMu.Lock()
	defer tokenBankPubKeyMu.Unlock()
	if len(tokenBankPubKey) > 0 {
		return tokenBankPubKey, nil
	}

	cachePath := filepath.Join(a.GetDataDir(), "hubcenter_pubkey.pem")
	if data, err := os.ReadFile(cachePath); err == nil && len(bytes.TrimSpace(data)) > 0 {
		tokenBankPubKey = data
		return data, nil
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
	if base == "" {
		return nil, fmt.Errorf("please sign in to HubCenter to use the Token Bank")
	}
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/crypto/pubkey", nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: tokenBankClientTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("could not load the HubCenter public key (%s)", resp.Status)
	}
	if block, _ := pem.Decode(data); block == nil {
		return nil, fmt.Errorf("HubCenter returned an unreadable public key")
	}
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, data, 0o644)
	tokenBankPubKey = data
	return data, nil
}

// TokenBankWithdraw pulls credits into this Hub's grant pool. This is the
// "提取到本机" action, not a cash withdrawal: the design doc is explicit that
// credits never leave the platform (§12).
//
// The grant is written by the Hub. HubCenter's hub-secret pull cannot set
// manual (R7), so an automatic pull is capped at 1/N. manual=true is a person
// taking the whole available balance: this method asks the Hub first, and only
// when that pull is refused for the cap does it authorize the same request id
// on the signed-in user session, then asks the Hub to replay it and write the
// grant. A missing service group fails on the first pull, before any debit.
func (a *App) TokenBankWithdraw(requestID string, amountMicro int64, manual bool) (map[string]interface{}, error) {
	id := strings.TrimSpace(requestID)
	if id == "" {
		return nil, fmt.Errorf("request id is required")
	}
	if amountMicro < 0 {
		return nil, fmt.Errorf("amount must not be negative")
	}
	return a.tokenBankPull(id, amountMicro, manual, "self", "")
}

// TokenBankCreateGiftLink freezes credits into a one-time share link.
//
// POST /api/v1/credits/share-links
//
// Send exactly one unit. credits is whole credits, the unit on the button.
// creditsMicro is the precise form, used when the 50% cap is not a whole
// credit. The server rejects a body that carries both.
func (a *App) TokenBankCreateGiftLink(credits int64, creditsMicro int64) (map[string]interface{}, error) {
	if credits > 0 && creditsMicro > 0 {
		return nil, fmt.Errorf("send either credits or credits_micro, not both")
	}
	if credits <= 0 && creditsMicro <= 0 {
		return nil, fmt.Errorf("credits must be positive")
	}
	body := map[string]any{}
	if credits > 0 {
		body["credits"] = credits
	} else {
		body["credits_micro"] = creditsMicro
	}
	result, err := a.tokenBankClientDo(http.MethodPost, "/api/v1/credits/share-links", body)
	if err != nil {
		return nil, err
	}
	a.annotateGiftClaimLinks(result)
	return result, nil
}

// TokenBankListGiftLinks returns links this user sent. The server omits the
// claim code on purpose: the code is shown once, from the create response.
//
// GET /api/v1/credits/share-links
//
// The desktop pages twenty cards at a time, so it asks for the server maximum
// (tokenBankMaxListLimit). A missing limit would stop at the default 100 and
// hide older links behind a pager that looks complete. This same limit also
// caps claimed_links on the response.
func (a *App) TokenBankListGiftLinks() (map[string]interface{}, error) {
	return a.tokenBankClientDo(http.MethodGet, "/api/v1/credits/share-links?limit=500", nil)
}

// TokenBankRevokeGiftLink unfreezes a link the caller sent while the credits
// are still frozen. That includes a link someone claimed but has not withdrawn.
//
// POST /api/v1/credits/share-links/{id}/revoke
func (a *App) TokenBankRevokeGiftLink(linkID string) (map[string]interface{}, error) {
	id := strings.TrimSpace(linkID)
	if id == "" {
		return nil, fmt.Errorf("link id is required")
	}
	return a.tokenBankClientDo(http.MethodPost, "/api/v1/credits/share-links/"+url.PathEscape(id)+"/revoke", nil)
}

// TokenBankPreviewGiftLink reads a public preview. code may be a bare code,
// an https claim URL, or a maclaw://credit/ deep link.
//
// GET /api/v1/credits/share-links/{code}/preview
func (a *App) TokenBankPreviewGiftLink(code string) (map[string]interface{}, error) {
	parsed := giftCodeFromInput(code)
	if parsed == "" {
		return nil, fmt.Errorf("share code is required")
	}
	result, err := a.tokenBankClientDo(http.MethodGet, "/api/v1/credits/share-links/"+url.PathEscape(parsed)+"/preview", nil)
	if err != nil {
		return nil, err
	}
	a.annotateGiftClaimLinks(result)
	return result, nil
}

// TokenBankClaimGiftLink binds the link to this account. Money stays frozen
// until TokenBankWithdrawGift.
//
// POST /api/v1/credits/share-links/{code}/claim
func (a *App) TokenBankClaimGiftLink(code string) (map[string]interface{}, error) {
	parsed := giftCodeFromInput(code)
	if parsed == "" {
		return nil, fmt.Errorf("share code is required")
	}
	result, err := a.tokenBankClientDo(http.MethodPost, "/api/v1/credits/share-links/"+url.PathEscape(parsed)+"/claim", nil)
	if err != nil {
		return nil, err
	}
	a.annotateGiftClaimLinks(result)
	return result, nil
}

// TokenBankWithdrawGift settles one claimed gift and pulls that exact amount
// into this Hub. It does not replace TokenBankWithdraw: a manual withdrawal of
// the whole balance must stay a 3-argument call with no gift fields.
//
// amountMicro is the gift's own micro amount. Zero is refused here because the
// hub would otherwise treat it as "as much as the mode allows" and drain the
// rest of the receiver's balance. A gift larger than 1/N uses the same
// user-session authorization as "提取到本机".
func (a *App) TokenBankWithdrawGift(requestID string, linkID string, amountMicro int64) (map[string]interface{}, error) {
	id := strings.TrimSpace(requestID)
	if id == "" {
		return nil, fmt.Errorf("request id is required")
	}
	link := strings.TrimSpace(linkID)
	if link == "" {
		return nil, fmt.Errorf("link id is required")
	}
	if amountMicro <= 0 {
		return nil, fmt.Errorf("gift amount must be positive")
	}
	return a.tokenBankPull(id, amountMicro, true, "gift", link)
}

// tokenBankPull asks the connected Hub to debit HubCenter and write the grant.
// manual reports a person pressing the button. The hub secret still cannot
// lift the 1/N cap; see tokenBankAuthorizeManualPull.
func (a *App) tokenBankPull(requestID string, amountMicro int64, manual bool, kind, linkID string) (map[string]interface{}, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	token := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || token == "" {
		return nil, fmt.Errorf("connect to this Hub before withdrawing Token Bank credits")
	}
	body := map[string]any{
		"request_id":   requestID,
		"amount_micro": amountMicro,
		"manual":       manual,
	}
	if kind == "gift" {
		body["kind"] = "gift"
		body["link_id"] = linkID
	}
	result, status, callErr := a.tokenBankHubCall(http.MethodPost, hubURL+"/api/token-bank/withdraw", token, body)
	if callErr != nil {
		return nil, callErr
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return result, nil
	}
	// Only the cap refusal is escalated. A missing service group, an unlinked
	// hub, or an empty balance stops here, before the user-session debit.
	if !manual || status != http.StatusPaymentRequired || tokenBankJSONCode(result) != "INSUFFICIENT_CREDITS" {
		return nil, tokenBankHubRejected(result, status)
	}
	authorized, err := a.tokenBankAuthorizeManualPull(requestID, amountMicro, kind, linkID)
	if err != nil {
		return nil, err
	}
	body["amount_micro"] = authorized
	result, status, callErr = a.tokenBankHubCall(http.MethodPost, hubURL+"/api/token-bank/withdraw", token, body)
	if callErr != nil {
		return nil, fmt.Errorf("credits are reserved for this Hub; retry this withdrawal to finish the grant: %w", callErr)
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return result, nil
	}
	return nil, fmt.Errorf("credits are reserved for this Hub; retry this withdrawal to finish the grant: %s", tokenBankHubRejected(result, status).Error())
}

// tokenBankAuthorizeManualPull records the full amount against this Hub's id
// using the HubCenter user session. The following hub pull replays that row
// and writes the grant. The hub id comes from the Hub itself so the replay
// matches the machine that holds the secret.
func (a *App) tokenBankAuthorizeManualPull(requestID string, amountMicro int64, kind, linkID string) (int64, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return 0, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	token := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || token == "" {
		return 0, fmt.Errorf("connect to this Hub before withdrawing Token Bank credits")
	}
	info, status, callErr := a.tokenBankHubCall(http.MethodGet, hubURL+"/api/token-bank/hub", token, nil)
	if callErr != nil {
		return 0, callErr
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return 0, tokenBankHubRejected(info, status)
	}
	hubID, _ := info["hub_id"].(string)
	hubID = strings.TrimSpace(hubID)
	if hubID == "" {
		return 0, fmt.Errorf("this Hub is not registered with HubCenter")
	}
	sessionBody := map[string]any{
		"request_id":   requestID,
		"amount_micro": amountMicro,
		"hub_id":       hubID,
		"manual":       true,
	}
	if kind == "gift" {
		sessionBody["kind"] = "gift"
		sessionBody["link_id"] = linkID
	}
	debited, err := a.tokenBankClientDo(http.MethodPost, "/api/v1/token-bank/credits/withdraw", sessionBody)
	if err != nil {
		return 0, err
	}
	if amount, ok := tokenBankJSONMicro(debited["amount_micro"]); ok {
		return amount, nil
	}
	if amountMicro > 0 {
		return amountMicro, nil
	}
	return 0, fmt.Errorf("HubCenter did not return the withdrawn amount")
}

// annotateGiftClaimLinks adds the two claim targets from §3.6 onto a payload
// that still carries the code. A list payload has no code, so this is a no-op
// and cannot put the secret back onto a refreshed row.
func (a *App) annotateGiftClaimLinks(result map[string]interface{}) {
	if result == nil {
		return
	}
	code, _ := result["code"].(string)
	code = strings.TrimSpace(code)
	if code == "" {
		return
	}
	escaped := url.PathEscape(code)
	result["deep_link"] = "maclaw://credit/" + escaped
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
	if base == "" {
		return
	}
	result["claim_url"] = base + "/c/" + escaped
}

// giftCodeFromInput accepts the three shapes a person can paste: the bare
// code, https://host/c/<code>, and maclaw://credit/<code>.
func giftCodeFromInput(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		path := strings.Trim(u.Path, "/")
		if path == "" {
			return ""
		}
		if i := strings.LastIndex(path, "/"); i >= 0 {
			path = path[i+1:]
		}
		return strings.TrimSpace(path)
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// tokenBankHubCall returns the decoded JSON and the status code. A non-2xx
// body is still decoded so the caller can tell a 1/N cap refusal from a
// missing service group. Transport and decode failures return a nil map.
func (a *App) tokenBankHubCall(method, rawURL, token string, body any) (map[string]interface{}, int, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, rawURL, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: tokenBankClientTimeout}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]interface{}{}, resp.StatusCode, nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, resp.StatusCode, fmt.Errorf("Hub rejected the withdrawal: %s", strings.TrimSpace(string(data)))
		}
		return nil, resp.StatusCode, fmt.Errorf("Hub returned an unreadable withdrawal response")
	}
	return result, resp.StatusCode, nil
}

func tokenBankHubRejected(result map[string]interface{}, status int) error {
	detail := ""
	if result != nil {
		raw, err := json.Marshal(result)
		if err == nil {
			detail = string(raw)
		}
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = http.StatusText(status)
	}
	return fmt.Errorf("Hub rejected the withdrawal: %s", detail)
}

func tokenBankJSONCode(result map[string]interface{}) string {
	if result == nil {
		return ""
	}
	code, _ := result["code"].(string)
	return strings.TrimSpace(code)
}

func tokenBankJSONMicro(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n <= 0 || n > float64(math.MaxInt64) || n != math.Trunc(n) {
			return 0, false
		}
		return int64(n), true
	case int64:
		if n <= 0 {
			return 0, false
		}
		return n, true
	case json.Number:
		parsed, err := n.Int64()
		if err != nil || parsed <= 0 {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
