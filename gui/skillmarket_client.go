package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type SkillMarketClient struct {
	app    *App
	client *http.Client
}

func NewSkillMarketClient(app *App) *SkillMarketClient {
	return &SkillMarketClient{
		app:    app,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *SkillMarketClient) baseURL() string {
	if c == nil || c.app == nil {
		return ""
	}
	// Display / identity URL: only a confirmed public registered HubCenter.
	// Do not surface loopback or unprobed official defaults here (About parity).
	if base := c.configuredPublicBaseURL(); base != "" {
		return base
	}
	return ""
}

func (c *SkillMarketClient) configuredPublicBaseURL() string {
	if c == nil || c.app == nil {
		return ""
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	return cfg.ConfiguredHubCenterBaseURL()
}

func (c *SkillMarketClient) selectBaseURL(ctx context.Context) (string, []string, error) {
	base, discovered, err := c.app.resolveHubCenterBaseURLCached(ctx, c.client)
	if err != nil {
		return "", nil, err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return base, discovered, nil
}

func (c *SkillMarketClient) getSkillPurchaseMode() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return "auto"
	}
	mode := strings.TrimSpace(cfg.SkillPurchaseMode)
	if mode == "" {
		mode = "auto"
	}
	return mode
}

func (c *SkillMarketClient) SubmitSkill(ctx context.Context, zipPath, email string) (string, error) {
	bodyBytes, contentType, err := buildSkillSubmitMultipart(zipPath, email)
	if err != nil {
		return "", err
	}
	// Upload must use registered HubCenter only (About identity), not HA defaults.
	bases, err := c.app.resolveHubCenterSubmitCandidates(ctx, c.client)
	if err != nil {
		return "", err
	}
	return c.submitSkillToHubCenter(ctx, bases, bodyBytes, contentType)
}

func (c *SkillMarketClient) SubmitSkillToConfiguredTargets(ctx context.Context, zipPath, email string) (string, error) {
	return c.SubmitSkillToConfiguredTargetsWithCompleted(ctx, zipPath, email, nil)
}

func (c *SkillMarketClient) SubmitSkillToConfiguredTargetsWithCompleted(ctx context.Context, zipPath, email string, completed map[string]string) (string, error) {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return "", err
	}
	bodyBytes, contentType, err := buildSkillSubmitMultipart(zipPath, email)
	if err != nil {
		return "", err
	}
	hasEnterpriseHub := strings.TrimSpace(cfg.RemoteHubURL) != "" && capabilityMarketAuthToken(cfg) != ""
	targets := cfg.CapabilityMarketPolicy.UploadTargets(hasEnterpriseHub)
	if len(targets) == 0 {
		return "", fmt.Errorf("enterprise Hub upload is selected but marketplace URL or auth token is not configured")
	}
	targetSet := make(map[string]bool, len(targets))
	for _, target := range targets {
		targetSet[target] = true
	}
	results := map[string]string{}
	for target, id := range normalizedSkillSubmitTargetResults(completed) {
		if targetSet[target] {
			results[target] = id
		}
	}
	var errs []string
	for _, target := range targets {
		if strings.TrimSpace(results[target]) != "" {
			continue
		}
		switch target {
		case corelib.CapabilitySourceHubCenter:
			// Match About registration: do not fan-out to default HA peers
			// (e.g. hubs2) the user never enrolled on.
			bases, err := c.app.resolveHubCenterSubmitCandidates(ctx, c.client)
			if err != nil {
				errs = append(errs, "hubcenter: "+err.Error())
				continue
			}
			id, err := c.submitSkillToHubCenter(ctx, bases, bodyBytes, contentType)
			if err != nil {
				errs = append(errs, "hubcenter: "+err.Error())
				continue
			}
			results[target] = id
		case corelib.CapabilitySourceEnterpriseHub:
			id, err := c.submitSkillToEnterpriseHub(ctx, cfg, bodyBytes, contentType)
			if err != nil {
				errs = append(errs, "enterprise_hub: "+err.Error())
				continue
			}
			results[target] = id
		}
	}
	if len(errs) > 0 {
		return "", &skillSubmitPartialError{Completed: results, Errs: errs}
	}
	if id := formatSkillSubmitTargetResults(results); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no upload target completed")
}

type skillSubmitPartialError struct {
	Completed map[string]string
	Errs      []string
}

func (e *skillSubmitPartialError) Error() string {
	if e == nil {
		return "submit skill failed"
	}
	return "submit skill failed: " + strings.Join(e.Errs, "; ")
}

func normalizedSkillSubmitTargetResults(results map[string]string) map[string]string {
	out := map[string]string{}
	for target, id := range results {
		target = corelib.NormalizeCapabilitySource(target)
		id = strings.TrimSpace(id)
		if target != "" && id != "" {
			out[target] = id
		}
	}
	return out
}

func formatSkillSubmitTargetResults(results map[string]string) string {
	if id := strings.TrimSpace(results[corelib.CapabilitySourceHubCenter]); id != "" {
		if enterpriseID := strings.TrimSpace(results[corelib.CapabilitySourceEnterpriseHub]); enterpriseID != "" {
			return id + ";enterprise_hub=" + enterpriseID
		}
		return id
	}
	if id := strings.TrimSpace(results[corelib.CapabilitySourceEnterpriseHub]); id != "" {
		return "enterprise_hub=" + id
	}
	return ""
}

func parseSkillSubmitTargetResults(submissionID string) map[string]string {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return nil
	}
	results := map[string]string{}
	parts := strings.Split(submissionID, ";")
	for i, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			if i == 0 {
				if id := strings.TrimSpace(part); id != "" {
					results[corelib.CapabilitySourceHubCenter] = id
				}
			}
			continue
		}
		key = corelib.NormalizeCapabilitySource(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			results[key] = value
		}
	}
	return results
}

func buildSkillSubmitMultipart(zipPath, email string) ([]byte, string, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, "", fmt.Errorf("open zip: %w", err)
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("zip", filepath.Base(zipPath))
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, "", err
	}
	_ = w.WriteField("email", email)
	contentType := w.FormDataContentType()
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), contentType, nil
}

func (c *SkillMarketClient) submitSkillToHubCenter(ctx context.Context, bases []string, bodyBytes []byte, contentType string) (string, error) {
	id, retryable, authExpired, err := c.submitSkillToHubCenterOnce(ctx, bases, bodyBytes, contentType)
	if err == nil {
		return id, nil
	}

	// Auth expired/invalid: refresh session via Hub machine-login (same credentials
	// as registration) and retry once. Hub login alone is not enough if the cached
	// SkillMarketSessionToken is stale — it takes priority over RemoteViewerToken.
	if authExpired {
		if refreshed, refreshErr := c.refreshSkillMarketSession(ctx, bases); refreshErr != nil {
			log.Printf("[skillmarket] session refresh failed: %v", refreshErr)
			return "", fmt.Errorf("%w (session refresh failed: %v)", err, refreshErr)
		} else if refreshed {
			id, _, _, retryErr := c.submitSkillToHubCenterOnce(ctx, bases, bodyBytes, contentType)
			if retryErr == nil {
				return id, nil
			}
			return "", fmt.Errorf("%w (after session refresh)", retryErr)
		}
		return "", err
	}

	// Only re-discover when the failure is connectivity/server-related (timeout,
	// connection refused, 5xx). Do NOT re-discover for client 4xx.
	if !retryable {
		return "", err
	}

	// All registered candidates failed with connectivity/server errors.
	// Invalidate discovery cache (used by search/download) and re-resolve
	// registered submit targets only — never expand to global HA defaults.
	if c.app != nil && c.app.hubCenterCache != nil {
		c.app.hubCenterCache.Invalidate()
	}
	freshBases, resolveErr := c.app.resolveHubCenterSubmitCandidates(ctx, c.client)
	if resolveErr != nil || len(freshBases) == 0 {
		return "", err // return original error if re-discovery also fails
	}
	// Only retry if we discovered at least one different candidate.
	if remote.StringSliceEqual(freshBases, bases) {
		return "", err
	}
	freshID, _, _, freshErr := c.submitSkillToHubCenterOnce(ctx, freshBases, bodyBytes, contentType)
	if freshErr != nil {
		return "", freshErr
	}
	return freshID, nil
}

// submitSkillToHubCenterOnce attempts to upload to each base in order.
// Returns (submissionID, retryableWithDifferentNodes, authExpired, error).
// retryable=true means connectivity/server failure (timeout, 5xx).
// authExpired=true means at least one candidate returned 401/403 (prefer
// machine-login refresh over HA rediscover, including mixed 401+5xx).
func (c *SkillMarketClient) submitSkillToHubCenterOnce(ctx context.Context, bases []string, bodyBytes []byte, contentType string) (string, bool, bool, error) {
	authHeader := c.skillMarketAuthHeader()

	var lastErr error
	authFailures := 0
	connectivityFailures := 0
	for _, base := range bases {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/skills/submit", bytes.NewReader(bodyBytes))
		if err != nil {
			return "", false, false, err
		}
		req.Header.Set("Content-Type", contentType)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			// Context cancellation is not a connectivity failure — do not retry.
			if ctx.Err() != nil {
				return "", false, false, fmt.Errorf("submit skill via %s: %w", base, err)
			}
			// Connection error (timeout, refused, DNS failure) — retryable with different node
			lastErr = fmt.Errorf("submit skill via %s: %w", base, err)
			connectivityFailures++
			continue
		}
		var result struct {
			SubmissionID string `json:"submission_id"`
		}
		retry, reqErr := func() (bool, error) {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				err := fmt.Errorf("submit failed via %s (%d): body_len=%d", base, resp.StatusCode, len(body))
				return shouldRetrySkillSubmitStatus(resp.StatusCode), err
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return true, fmt.Errorf("decode submit response from %s: %w", base, err)
			}
			return false, nil
		}()
		if reqErr == nil {
			if strings.TrimSpace(result.SubmissionID) == "" {
				lastErr = fmt.Errorf("submit response from %s missing submission_id", base)
				continue
			}
			c.app.rememberHubCenterSelection(base, bases)
			return result.SubmissionID, false, false, nil
		}
		lastErr = reqErr
		if isSkillSubmitAuthStatus(resp.StatusCode) {
			authFailures++
		} else if resp.StatusCode >= 500 {
			connectivityFailures++
		}
		if !retry {
			break
		}
	}
	// Any 401/403 means the session token is unusable on at least one node.
	// Prefer machine-login refresh over HA rediscover (mixed 401+5xx used to
	// skip refresh when authFailures < len(bases)).
	if authFailures > 0 {
		return "", false, true, fmt.Errorf("SkillMarket 认证失败或已过期，请重新登录 SkillMarket 后再上传；如果是多机集群，请确认各 HubCenter 使用相同 cluster_secret 并已同步 session revocation")
	}
	// retryable only when there were connectivity/server failures (the node
	// may be dead), NOT when the error is a client-side 4xx.
	retryable := connectivityFailures > 0
	if lastErr != nil {
		return "", retryable, false, lastErr
	}
	return "", retryable, false, fmt.Errorf("no reachable hubcenter")
}

// refreshSkillMarketSession exchanges Hub enrollment credentials for a fresh
// SkillMarket session token and persists it. Used when submit gets 401/403.
func (c *SkillMarketClient) refreshSkillMarketSession(ctx context.Context, bases []string) (bool, error) {
	if c == nil || c.app == nil {
		return false, fmt.Errorf("skill market client not initialized")
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return false, err
	}
	account := strings.TrimSpace(cfg.RemoteEmail)
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if account == "" || machineID == "" || viewerToken == "" {
		return false, fmt.Errorf("hub enrollment incomplete (need remote_email, remote_machine_id, remote_viewer_token)")
	}
	candidates := make([]string, 0, len(bases)+1)
	seen := make(map[string]struct{}, len(bases)+1)
	add := func(u string) {
		u = remote.NormalizeHubCenterURL(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		candidates = append(candidates, u)
	}
	for _, b := range bases {
		add(b)
	}
	add(cfg.ConfiguredHubCenterBaseURL())
	if len(candidates) == 0 {
		return false, fmt.Errorf("hubcenter URL not configured")
	}
	authClient := remote.NewSkillMarketAuthClient()
	var lastErr error
	for _, baseURL := range candidates {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		result, loginErr := authClient.MachineLogin(ctx, baseURL, account, machineID, viewerToken)
		if loginErr != nil {
			lastErr = loginErr
			continue
		}
		if result == nil || strings.TrimSpace(result.SessionToken) == "" {
			lastErr = fmt.Errorf("machine-login returned empty session token from %s", baseURL)
			continue
		}
		if err := c.app.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.SkillMarketSessionToken = strings.TrimSpace(result.SessionToken)
		}); err != nil {
			return false, err
		}
		log.Printf("[skillmarket] session refreshed via hub machine-login account=%s center=%s", account, baseURL)
		return true, nil
	}
	if lastErr != nil {
		return false, lastErr
	}
	return false, fmt.Errorf("machine-login failed on all hubcenter candidates")
}

func (c *SkillMarketClient) submitSkillToEnterpriseHub(ctx context.Context, cfg corelib.AppConfig, bodyBytes []byte, contentType string) (string, error) {
	base := capabilityMarketBaseURL(cfg)
	token := capabilityMarketAuthToken(cfg)
	if base == "" || token == "" {
		return "", fmt.Errorf("enterprise Hub marketplace URL or auth token is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/capabilities/skills/submit", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("submit failed via enterprise Hub (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		SubmissionID string `json:"submission_id"`
		CapabilityID string `json:"capability_id"`
		VersionKey   string `json:"version_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	id := firstNonEmptySkillMarket(result.SubmissionID, result.VersionKey, result.CapabilityID)
	if id == "" {
		return "", fmt.Errorf("enterprise Hub submit response missing submission_id, version_key, or capability_id")
	}
	return id, nil
}

func firstNonEmptySkillMarket(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *SkillMarketClient) skillMarketAuthHeader() string {
	if c == nil || c.app == nil {
		return ""
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	if token := strings.TrimSpace(cfg.SkillMarketSessionToken); token != "" {
		return "Bearer " + token
	}
	if token := strings.TrimSpace(cfg.RemoteViewerToken); token != "" {
		return "Bearer " + token
	}
	return ""
}

func shouldRetrySkillSubmitStatus(status int) bool {
	return isSkillSubmitAuthStatus(status) || status >= 500
}

func isSkillSubmitAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func (c *SkillMarketClient) GetSubmissionStatus(ctx context.Context, submissionID string) (string, string, error) {
	var result struct {
		Status   string `json:"status"`
		ErrorMsg string `json:"error_msg"`
	}
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, "/api/v1/skill-submissions/"+submissionID, 0, &result); err != nil {
		return "", "", err
	}
	return result.Status, result.ErrorMsg, nil
}

func (c *SkillMarketClient) DownloadEncrypted(ctx context.Context, skillID, email string) ([]byte, error) {
	mode := c.getSkillPurchaseMode()
	if mode == "free_only" {
		price, err := c.getSkillPrice(ctx, skillID)
		if err == nil && price > 0 {
			return nil, fmt.Errorf("skill %s requires %d credits, skipped (free_only mode)", skillID, price)
		}
	}
	path := fmt.Sprintf("/api/v1/skillmarket/%s/download?email=%s", skillID, email)
	_, _, data, err := c.app.getHubCenterBytes(ctx, c.client, path, 0)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	return data, nil
}

type AccountInfo struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Status            string `json:"status"`
	Credits           int64  `json:"credits"`
	SettledCredits    int64  `json:"settled_credits"`
	PendingSettlement int64  `json:"pending_settlement"`
	VoucherCount      int    `json:"voucher_count"`
}

func (c *SkillMarketClient) EnsureAccount(ctx context.Context, email string) (*AccountInfo, error) {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/account/ensure", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info AccountInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return &info, nil
}

func (c *SkillMarketClient) GetAccountInfo(ctx context.Context, email string) (*AccountInfo, error) {
	var info AccountInfo
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, "/api/v1/account/"+email, 0, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *SkillMarketClient) SubmitRating(ctx context.Context, skillID, email string, score int) error {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]interface{}{"email": email, "score": score})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/skillmarket/"+skillID+"/rate", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rate failed (%d): body_len=%d", resp.StatusCode, len(body))
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return nil
}

func (c *SkillMarketClient) GetPublicKey(ctx context.Context) ([]byte, error) {
	cachePath := filepath.Join(c.app.GetDataDir(), "skillmarket_pubkey.pem")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return data, nil
	}
	_, _, data, err := c.app.getHubCenterBytes(ctx, c.client, "/api/v1/crypto/pubkey", 0)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, data, 0o644)
	return data, nil
}

func (c *SkillMarketClient) getSkillPrice(ctx context.Context, skillID string) (int, error) {
	var result struct {
		Results []struct {
			Price int `json:"price"`
		} `json:"results"`
	}
	path := "/api/v1/skillmarket/search?q=&top_n=1&skill_id=" + skillID
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, path, 0, &result); err != nil {
		return 0, err
	}
	if len(result.Results) > 0 {
		return result.Results[0].Price, nil
	}
	return 0, nil
}

func (c *SkillMarketClient) getJSON(ctx context.Context, path string, dest interface{}) error {
	_, _, err := c.app.getHubCenterJSON(ctx, c.client, path, 0, dest)
	return err
}
