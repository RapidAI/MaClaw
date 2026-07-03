package remote

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"
	"time"
)

// EnrollTimeout is the maximum time allowed for a single enrollment HTTP request.
const EnrollTimeout = 25 * time.Second

// EnrollConfig holds all inputs for the enrollment flow.
type EnrollConfig struct {
	Email          string   // required
	InvitationCode string   // optional, for invitation-only hubs
	Mobile         string   // optional
	ClientID       string   // existing client_id; empty → auto-generate
	HubURL         string   // known hub URL; empty → resolve via HubCenter
	HubCenterURL   string   // explicit HubCenter URL; empty → use defaults
	HubCenterURLs  []string // additional HubCenter URLs from config
	MachineName    string   // e.g. hostname
	Platform       string   // "windows", "mac", "linux"
	Hostname       string
	Arch           string // e.g. "amd64", "arm64"
	AppVersion     string
	HeartbeatSec   int
}

// EnrollResult holds the output of a successful enrollment.
type EnrollResult struct {
	Status              string `json:"status"`
	TenantID            string `json:"tenant_id,omitempty"`
	TenantName          string `json:"tenant_name,omitempty"`
	Message             string `json:"message,omitempty"`
	Code                string `json:"code,omitempty"`
	UserID              string `json:"user_id,omitempty"`
	Email               string `json:"email,omitempty"`
	SN                  string `json:"sn,omitempty"`
	MachineID           string `json:"machine_id,omitempty"`
	MachineToken        string `json:"machine_token,omitempty"`
	ViewerToken         string `json:"viewer_token,omitempty"`
	ExpiresAt           string `json:"expires_at,omitempty"`
	VIPFlag             bool   `json:"vip_flag,omitempty"`
	ReboundExistingUser bool   `json:"rebound_existing_user,omitempty"`
	VEQuota             int    `json:"ve_quota,omitempty"` // Digital employee quota approved by HubCenter (0-10000)

	// Resolved metadata — not from the enroll response, filled by the client.
	HubID          string   // the hub_id selected by HubCenter resolve, when known
	HubURL         string   // the hub URL actually used for enrollment
	HubCenterURL   string   // the hub center URL that resolved the hub
	DiscoveredURLs []string // all discovered hub center URLs (for persistence)
	ClientID       string   // the client_id used (may be newly generated)
}

// HubCenterResolveResult mirrors the JSON returned by POST /api/entry/resolve.
// Exported so both GUI and TUI can use the same type for hub listing.
type HubCenterResolveResult struct {
	Email        string                `json:"email"`
	Mode         string                `json:"mode"`
	DefaultHubID string                `json:"default_hub_id,omitempty"`
	DefaultPWA   string                `json:"default_pwa_url,omitempty"`
	Hubs         []HubCenterResolveHub `json:"hubs,omitempty"`
	Message      string                `json:"message,omitempty"`
}

// HubCenterResolveHub describes a single hub returned by the resolve endpoint.
type HubCenterResolveHub struct {
	HubID          string `json:"hub_id"`
	TenantID       string `json:"tenant_id,omitempty"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	PWAURL         string `json:"pwa_url"`
	Visibility     string `json:"visibility"`
	EnrollmentMode string `json:"enrollment_mode"`
	Status         string `json:"status"`
}

// EnrollmentClient performs the full HubCenter discovery → Hub resolve → Enroll flow.
// Both GUI and TUI share this implementation.
type EnrollmentClient struct {
	HTTPClient    *http.Client  // if nil, a default TLS-skip client is created
	EnrollTimeout time.Duration // if zero, EnrollTimeout is used
}

// NewEnrollmentClient creates an EnrollmentClient with a default HTTP client
// that skips TLS certificate verification (hub servers commonly use self-signed certs).
func NewEnrollmentClient() *EnrollmentClient {
	return &EnrollmentClient{
		HTTPClient: NewHubHTTPClient(),
	}
}

// NewHubHTTPClient creates an HTTP client suitable for hub/hubcenter communication.
// It skips TLS certificate verification because hub servers commonly use self-signed certs.
func NewHubHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// ResolveHubs queries HubCenter for available hubs matching the given email.
// This is the resolve-only path used by ListRemoteHubs. Enroll() calls this internally.
func (c *EnrollmentClient) ResolveHubs(ctx context.Context, email string, invitationCode string, hubCenterURL string, hubCenterURLs []string) (*HubCenterResolveResult, string, []string, error) {
	httpClient := c.httpClient()
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, "", nil, fmt.Errorf("email is required")
	}

	urls := BuildCenterURLList(hubCenterURL, hubCenterURLs)
	if len(urls) == 0 {
		return nil, "", nil, fmt.Errorf("no hub center URLs configured")
	}

	preferredCenter := strings.TrimSpace(hubCenterURL)
	ordered := DiscoverHubCenterURLs(ctx, httpClient, urls, preferredCenter)
	if len(ordered) == 0 {
		ordered = SelectBestCenter(ctx, httpClient, urls, preferredCenter)
	}
	if len(ordered) == 0 {
		ordered = urls
	}

	payload := map[string]string{"email": email}
	if phone := normalizeResolvePhoneNumber(email); phone != "" {
		payload["phone_number"] = phone
	}
	if strings.TrimSpace(invitationCode) != "" {
		payload["invitation_code"] = strings.TrimSpace(invitationCode)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", nil, err
	}

	var lastErr error
	for _, u := range ordered {
		resolveURL := strings.TrimRight(u, "/") + "/api/entry/resolve"
		resp, err := httpClient.Post(resolveURL, "application/json", bytes.NewReader(data))
		if err != nil {
			lastErr = fmt.Errorf("resolve via %s: %w", u, err)
			continue
		}

		var result HubCenterResolveResult
		decodeErr := DecodeHTTPJSONResponse(resp, &result, "hub center resolve")
		resp.Body.Close()
		if decodeErr != nil {
			lastErr = fmt.Errorf("decode response from %s: %w", u, decodeErr)
			continue
		}
		if resp.StatusCode >= 300 {
			msg := result.Message
			if msg == "" {
				msg = resp.Status
			}
			lastErr = fmt.Errorf("hub center %s: %s", u, msg)
			continue
		}

		return &result, u, ordered, nil
	}

	if lastErr != nil {
		return nil, "", nil, fmt.Errorf("all hub centers failed: %w", lastErr)
	}
	return nil, "", nil, fmt.Errorf("no reachable hub center")
}

func normalizeResolvePhoneNumber(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "@") || strings.HasPrefix(strings.ToLower(value), "phone:") {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' || r == '-' || r == '.' || r == '(' || r == ')' || r == ' ' || r == '\t':
			continue
		default:
			return ""
		}
	}
	if b.Len() < 6 {
		return ""
	}
	return b.String()
}

// Enroll performs the full registration flow:
//  1. Discovery: find the best reachable HubCenter node
//  2. Resolve: query HubCenter for the hub matching the user's email
//  3. Enroll: register the machine with the selected hub
//
// The caller is responsible for persisting the result to config.
func (c *EnrollmentClient) Enroll(ctx context.Context, cfg EnrollConfig) (*EnrollResult, error) {
	start := time.Now()
	httpClient := c.httpClient()

	email := strings.TrimSpace(cfg.Email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	// --- Step 1+2: Resolve hub URL ---
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.HubURL), "/")
	var discoveredURLs []string
	var usedCenterURL string
	var resolvedHubID string
	var resolvedTenantID string // tenant_id from invitation code routing

	// When an invitation code is provided, always resolve via HubCenter even if
	// a cached HubURL exists. The invitation code may target a different Hub than
	// the one previously cached (e.g. user was deleted from old Hub and re-invited
	// to a new one). HubCenter's invitation code routing ensures the request
	// reaches the correct Hub that issued the code.
	hasInvitationCode := strings.TrimSpace(cfg.InvitationCode) != ""
	if hubURL == "" || hasInvitationCode {
		if hasInvitationCode && hubURL != "" {
			log.Printf("[enrollment] invitation code provided — ignoring cached hub_url=%s, resolving via HubCenter", hubURL)
		}
		log.Printf("[enrollment] resolving hub for email=%s", email)
		resolved, err := c.resolveHubURL(ctx, httpClient, email, cfg)
		if err != nil {
			// Only fall back to cached HubURL on network-level failures (all
			// HubCenter nodes unreachable). If HubCenter was reachable but
			// explicitly rejected the routing (e.g. INVITATION_CODE_NOT_ROUTED,
			// email blocked), falling back would bypass that rejection.
			isNetworkFailure := hasInvitationCode && hubURL != "" && isResolveNetworkError(err)
			if isNetworkFailure {
				log.Printf("[enrollment] resolve failed (network: %v) — falling back to cached hub_url=%s", err, hubURL)
			} else {
				return nil, fmt.Errorf("hub resolution failed: %w", err)
			}
		} else {
			hubURL = resolved.hubURL
			resolvedHubID = resolved.hubID
			resolvedTenantID = resolved.tenantID
			discoveredURLs = resolved.discoveredURLs
			usedCenterURL = resolved.usedCenterURL
			log.Printf("[enrollment] resolved hub=%s tenant=%s center=%s duration=%s", hubURL, resolvedTenantID, usedCenterURL, time.Since(start))
		}
	}

	// --- Step 3: Ensure client_id ---
	clientID := strings.TrimSpace(cfg.ClientID)
	if clientID == "" {
		clientID = GenerateClientID()
		log.Printf("[enrollment] generated new client_id=%s", clientID[:8]+"...")
	}

	// --- Step 4: Build enroll request ---
	body := map[string]any{
		"email":        email,
		"machine_name": cfg.MachineName,
		"platform":     cfg.Platform,
		"hostname":     cfg.Hostname,
		"arch":         cfg.Arch,
		"app_version":  cfg.AppVersion,
		"client_id":    clientID,
	}
	heartbeat := cfg.HeartbeatSec
	if heartbeat <= 0 {
		heartbeat = 30
	} else if heartbeat < 5 {
		heartbeat = 5
	}
	body["heartbeat_interval_sec"] = heartbeat
	if cfg.InvitationCode != "" {
		body["invitation_code"] = cfg.InvitationCode
	}
	if cfg.Mobile != "" {
		body["mobile"] = strings.TrimSpace(cfg.Mobile)
	}
	// When the hub was resolved via invitation code routing, pass the target
	// tenant_id so Hub's enrollment handler places the user in the correct tenant.
	if resolvedTenantID != "" {
		body["tenant_id"] = resolvedTenantID
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal enroll request: %w", err)
	}

	// --- Step 5: Send enroll request ---
	enrollURL := strings.TrimRight(hubURL, "/") + "/api/enroll/start"
	enrollTimeout := c.enrollTimeout()
	enrollCtx, cancel := context.WithTimeout(ctx, enrollTimeout)
	defer cancel()

	log.Printf("[enrollment] POST %s", enrollURL)
	req, err := http.NewRequestWithContext(enrollCtx, http.MethodPost, enrollURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeoutError(err) {
			return nil, fmt.Errorf("registration timed out after %s", enrollTimeout)
		}
		return nil, fmt.Errorf("enroll request failed: %w", err)
	}
	defer resp.Body.Close()

	// --- Step 6: Parse response ---
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read enroll response: %w", err)
	}

	// Handle non-2xx responses first. Hub/proxy may return HTML or plain text
	// error pages (e.g. Nginx 502 "The server is temporarily unavailable")
	// that cannot be JSON-decoded.
	if resp.StatusCode >= 300 {
		// Try JSON first for structured error messages from the Hub itself.
		var errResp EnrollResult
		if DecodeJSONResponseBody(respBody, &errResp) == nil {
			if errResp.Code != "" {
				msg := errResp.Code + ": " + errResp.Message
				if errResp.ExpiresAt != "" {
					msg += " expires_at:" + errResp.ExpiresAt
				}
				return nil, fmt.Errorf("%s", msg)
			}
			if errResp.Message != "" {
				return nil, fmt.Errorf("%s", errResp.Message)
			}
		}
		// Non-JSON error: return user-friendly message instead of raw parse error.
		log.Printf("[enrollment] enroll HTTP %d non-JSON response: %s", resp.StatusCode, responsePreview(respBody))
		return nil, fmt.Errorf("registration service is temporarily unavailable (HTTP %d); please retry later", resp.StatusCode)
	}

	var enrollResp EnrollResult
	if err := DecodeJSONResponseBody(respBody, &enrollResp); err != nil {
		log.Printf("[enrollment] enroll 2xx but non-JSON response: %s", responsePreview(respBody))
		return nil, fmt.Errorf("registration service returned an unexpected response format; please retry later")
	}

	// Fill resolved metadata.
	enrollResp.HubID = resolvedHubID
	enrollResp.HubURL = hubURL
	// Never report a loopback address as the HubCenterURL — it would be
	// persisted to config and displayed in the "About" panel. Promote the
	// first public URL from discovered list instead.
	if usedCenterURL != "" && IsLoopbackURL(usedCenterURL) {
		promoted := ""
		for _, u := range discoveredURLs {
			if !IsLoopbackURL(u) {
				promoted = u
				break
			}
		}
		if promoted != "" {
			enrollResp.HubCenterURL = promoted
		} else {
			enrollResp.HubCenterURL = usedCenterURL // all loopback — preserve as-is (edge case)
		}
	} else {
		enrollResp.HubCenterURL = usedCenterURL
	}
	enrollResp.DiscoveredURLs = discoveredURLs
	enrollResp.ClientID = clientID

	// Validate VEQuota range — invalid/missing defaults to 0.
	if enrollResp.VEQuota < 0 || enrollResp.VEQuota > 10000 {
		log.Printf("[enrollment] ve_quota %d out of valid range [0,10000], defaulting to 0", enrollResp.VEQuota)
		enrollResp.VEQuota = 0
	}

	log.Printf("[enrollment] success machine_id=%s email=%s ve_quota=%d duration=%s", enrollResp.MachineID, enrollResp.Email, enrollResp.VEQuota, time.Since(start))
	return &enrollResp, nil
}

// resolveResult holds the output of hub center discovery + resolve.
type resolveResult struct {
	hubURL         string
	hubID          string
	tenantID       string // tenant_id from invitation code routing (empty if not code-routed)
	usedCenterURL  string
	discoveredURLs []string
}

// resolveHubURL discovers the best HubCenter, then resolves the hub for the given email.
func (c *EnrollmentClient) resolveHubURL(ctx context.Context, httpClient *http.Client, email string, cfg EnrollConfig) (*resolveResult, error) {
	result, usedCenter, ordered, err := c.ResolveHubs(ctx, email, cfg.InvitationCode, cfg.HubCenterURL, cfg.HubCenterURLs)
	if err != nil {
		return nil, err
	}

	hubURL, hubID, tenantID, err := PickBestHubWithTenantAndID(*result)
	if err != nil {
		return nil, err
	}

	return &resolveResult{
		hubURL:         hubURL,
		hubID:          hubID,
		tenantID:       tenantID,
		usedCenterURL:  usedCenter,
		discoveredURLs: ordered,
	}, nil
}

// PickBestHub selects the best hub URL from a resolve result.
// Priority: default_hub_id > first online hub > any hub (last resort).
// Hubs with non-online status are skipped as a defensive measure against
// stale snapshots on the HubCenter side.
func PickBestHub(result HubCenterResolveResult) (string, error) {
	url, _, err := PickBestHubWithTenant(result)
	return url, err
}

// PickBestHubWithTenant selects the best hub URL and its associated tenant_id.
// The tenant_id is non-empty when the hub was resolved via invitation code routing.
func PickBestHubWithTenant(result HubCenterResolveResult) (string, string, error) {
	url, _, tenantID, err := PickBestHubWithTenantAndID(result)
	return url, tenantID, err
}

// PickBestHubWithTenantAndID selects the best hub URL, hub_id, and tenant_id.
func PickBestHubWithTenantAndID(result HubCenterResolveResult) (string, string, string, error) {
	if len(result.Hubs) == 0 {
		msg := result.Message
		if msg == "" {
			msg = "no available hubs found"
		}
		return "", "", "", fmt.Errorf("%s", msg)
	}

	// Prefer the default hub (if online).
	if result.DefaultHubID != "" {
		for _, hub := range result.Hubs {
			if hub.HubID == result.DefaultHubID && strings.TrimSpace(hub.BaseURL) != "" && isHubOnline(hub.Status) {
				return strings.TrimRight(hub.BaseURL, "/"), hub.HubID, hub.TenantID, nil
			}
		}
	}

	// Fallback: first online hub with a non-empty BaseURL.
	for _, hub := range result.Hubs {
		if strings.TrimSpace(hub.BaseURL) != "" && isHubOnline(hub.Status) {
			return strings.TrimRight(hub.BaseURL, "/"), hub.HubID, hub.TenantID, nil
		}
	}

	// Last resort: first hub with a non-empty BaseURL regardless of status.
	// This handles the case where HubCenter doesn't populate the Status field.
	for _, hub := range result.Hubs {
		if strings.TrimSpace(hub.BaseURL) != "" {
			return strings.TrimRight(hub.BaseURL, "/"), hub.HubID, hub.TenantID, nil
		}
	}

	return "", "", "", fmt.Errorf("hub center did not return a usable hub url")
}

// isHubOnline returns true if the hub status indicates it's operational.
// Empty status is treated as online (backward compat with older HubCenter versions).
func isHubOnline(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "" || s == "online"
}

// BuildCenterURLList assembles a deduplicated list of HubCenter URLs to probe.
// Priority: explicit > saved list > built-in defaults.
func BuildCenterURLList(explicit string, saved []string) []string {
	seen := make(map[string]bool)
	var urls []string
	add := func(u string) {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	add(explicit)
	for _, u := range saved {
		add(u)
	}
	for _, u := range DefaultRemoteHubCenterURLs {
		add(u)
	}
	return urls
}

// BuildMachineProfile creates an EnrollConfig with machine-level defaults filled in.
// The caller should set Email, InvitationCode, and any config-derived fields
// (ClientID, HubURL, HubCenterURL, HubCenterURLs) after calling this.
func BuildMachineProfile(appVersion string) EnrollConfig {
	hostname, _ := os.Hostname()
	name := hostname
	if name == "" {
		name = "MaClaw"
	}
	return EnrollConfig{
		MachineName:  name,
		Platform:     NormalizedPlatform(),
		Hostname:     hostname,
		Arch:         goruntime.GOARCH,
		AppVersion:   appVersion,
		HeartbeatSec: 10,
	}
}

// NormalizedPlatform returns the platform string used in enrollment requests.
func NormalizedPlatform() string {
	switch goruntime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	default:
		return "linux"
	}
}

func (c *EnrollmentClient) httpClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return NewHubHTTPClient()
}

func (c *EnrollmentClient) enrollTimeout() time.Duration {
	if c != nil && c.EnrollTimeout > 0 {
		return c.EnrollTimeout
	}
	return EnrollTimeout
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// isResolveNetworkError returns true when the resolve failure is caused by
// network-level issues (all HubCenter nodes unreachable, DNS failure, timeout).
// Returns false when HubCenter was reachable but explicitly rejected the routing
// (invitation code not found, email blocked, no available hubs, etc.).
//
// Heuristic: ResolveHubs returns errors wrapping network failures with "resolve via"
// prefix or "all hub centers failed" / "no reachable hub center" messages.
// PickBestHubWithTenantAndID returns errors from HubCenter's response message
// (e.g. "INVITATION_CODE_NOT_ROUTED", "No available hubs found").
func isResolveNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Network-level failures from ResolveHubs retry loop.
	if strings.Contains(msg, "all hub centers failed") ||
		strings.Contains(msg, "no reachable hub center") ||
		strings.Contains(msg, "no hub center URLs configured") {
		return true
	}
	// Timeout / connection refused from underlying HTTP calls.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if isTimeoutError(err) {
		return true
	}
	return false
}
