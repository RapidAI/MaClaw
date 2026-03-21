package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/go-lark/lark/v2"
)

// ---------------------------------------------------------------------------
// AutoEnroller — adds Hub users to the Feishu organization on enrollment
// ---------------------------------------------------------------------------
//
// When a user registers (desktop or mobile), the AutoEnroller calls the
// Feishu Contact v3 API to add them to the Feishu organization. Once they
// are a member of the org, they can search for and use the Feishu bot.
//
// Flow:
//   Enrollment (email, mobile) → AutoEnroller.AddToFeishuOrg(email, name, mobile)
//     1. Check if user already exists in Feishu org (GET contact/v3/users)
//     2. If not, create user via POST /open-apis/contact/v3/users
//     3. Bind the new open_id for IM notifications
//     4. Send a welcome message so the bot appears in the user's chat list
//
// Required Feishu app permissions:
//   - contact:user (read & write users)
//   - contact:user.email:readonly (read email)
//   - im:message:send_as_bot (send messages)

const (
	settingsKeyAutoEnroll    = "feishu_auto_enroll"
	feishuAPIBase            = "https://open.feishu.cn"
	larkAPIBase              = "https://open.larksuite.com"
	addMemberCooldown        = 2 * time.Minute
	createUserMaxRetries     = 2
	retryBaseDelay           = 2 * time.Second
)

// AutoEnrollResult holds the structured result of an auto-enroll attempt.
type AutoEnrollResult struct {
	Status  string // "ok", "failed", "skipped", "disabled"
	Message string
	OpenID  string
}

// AutoEnrollConfig holds the auto-enroll settings persisted in the DB.
type AutoEnrollConfig struct {
	Enabled      bool   `json:"enabled"`
	DepartmentID string `json:"department_id"`       // target department (default "0" = root)
	UseLark      bool   `json:"use_lark"`            // true = Lark (overseas), false = Feishu (China)
	EmployeeType int    `json:"employee_type"`       // 1=Regular, 2=Intern, etc. (default 1)
}

// AutoEnroller manages automatic addition of Hub users to the Feishu org.
type AutoEnroller struct {
	mu  sync.Mutex
	cfg AutoEnrollConfig

	// bot returns the current Feishu lark.Bot (may be nil if not configured).
	bot func() *lark.Bot

	// binder persists the email↔open_id mapping in the Notifier.
	binder func(email, openID string)

	// welcomeSender sends a welcome message to the user after enrollment.
	// If nil, no welcome message is sent.
	welcomeSender func(openID string)

	// cooldown: email → last attempt time.
	attempts map[string]time.Time

	// welcomed tracks open_ids that already received a welcome message.
	welcomed map[string]bool
}

// NewAutoEnroller creates an AutoEnroller. Starts disabled.
func NewAutoEnroller(botFunc func() *lark.Bot, binder func(email, openID string)) *AutoEnroller {
	return &AutoEnroller{
		bot:      botFunc,
		binder:   binder,
		attempts: make(map[string]time.Time),
		welcomed: make(map[string]bool),
	}
}

// SetConfig updates the auto-enroll configuration.
func (ae *AutoEnroller) SetConfig(cfg AutoEnrollConfig) {
	ae.mu.Lock()
	ae.cfg = cfg
	ae.mu.Unlock()
}

// Config returns the current configuration.
func (ae *AutoEnroller) Config() AutoEnrollConfig {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	return ae.cfg
}

// IsEnabled returns whether auto-enrollment is active.
func (ae *AutoEnroller) IsEnabled() bool {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	return ae.cfg.Enabled
}

// SetEnabled is a convenience method to toggle the enabled flag.
func (ae *AutoEnroller) SetEnabled(v bool) {
	ae.mu.Lock()
	ae.cfg.Enabled = v
	ae.mu.Unlock()
}

// SetWelcomeSender sets the callback that sends a welcome message to a newly
// enrolled user. The callback receives the user's open_id.
func (ae *AutoEnroller) SetWelcomeSender(fn func(openID string)) {
	ae.mu.Lock()
	ae.welcomeSender = fn
	ae.mu.Unlock()
}

// trySendWelcome sends a welcome message if one hasn't been sent to this
// open_id yet (in-memory dedup, resets on restart — acceptable).
func (ae *AutoEnroller) trySendWelcome(openID string) {
	ae.mu.Lock()
	if ae.welcomed[openID] || ae.welcomeSender == nil {
		ae.mu.Unlock()
		return
	}
	ae.welcomed[openID] = true
	ws := ae.welcomeSender
	ae.mu.Unlock()
	ws(openID)
}

// ClearCooldown removes the cooldown entry for the given email so that a
// subsequent AddToFeishuOrg call will not be skipped.
func (ae *AutoEnroller) ClearCooldown(email string) {
	ae.mu.Lock()
	delete(ae.attempts, strings.ToLower(strings.TrimSpace(email)))
	ae.mu.Unlock()
}

// apiBase returns the correct API base URL based on the UseLark setting.
func (ae *AutoEnroller) apiBase() string {
	if ae.Config().UseLark {
		return larkAPIBase
	}
	return feishuAPIBase
}

// AddToFeishuOrg is called after a user successfully enrolls (desktop or
// mobile). It adds the user to the Feishu organization so they can discover
// and interact with the bot.
//
// The method is safe to call even if the user already exists in Feishu — it
// will detect duplicates and skip silently.
func (ae *AutoEnroller) AddToFeishuOrg(ctx context.Context, email, displayName, mobile string) (*AutoEnrollResult, error) {
	if !ae.IsEnabled() {
		log.Printf("[feishu/auto-enroll] disabled, skipping %s", email)
		return &AutoEnrollResult{Status: "disabled"}, nil
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return &AutoEnrollResult{Status: "skipped", Message: "empty email"}, nil
	}

	// Normalize mobile: for Feishu (China), ensure +86 prefix.
	mobile = strings.TrimSpace(mobile)
	if mobile != "" && !ae.Config().UseLark {
		mobile = normalizeChinaMobile(mobile)
	}

	log.Printf("[feishu/auto-enroll] starting for email=%s mobile=%q displayName=%q", email, mobile, displayName)

	// Cooldown — avoid repeated API calls for the same email.
	ae.mu.Lock()
	if last, ok := ae.attempts[email]; ok && time.Since(last) < addMemberCooldown {
		ae.mu.Unlock()
		log.Printf("[feishu/auto-enroll] skipping %s (cooldown, last attempt %s ago)", email, time.Since(last).Round(time.Second))
		return &AutoEnrollResult{Status: "skipped", Message: "cooldown"}, nil
	}
	ae.attempts[email] = time.Now()
	// Evict stale entries.
	for k, v := range ae.attempts {
		if time.Since(v) > addMemberCooldown*2 {
			delete(ae.attempts, k)
		}
	}
	ae.mu.Unlock()

	// Obtain bot and token with retry for empty token.
	token, err := ae.acquireToken(ctx)
	if err != nil {
		return &AutoEnrollResult{Status: "failed", Message: err.Error()}, err
	}

	// Step 1: Check if user already exists in Feishu org by email.
	existingOpenID, err := ae.lookupUserByEmail(ctx, token, email)
	if err != nil {
		log.Printf("[feishu/auto-enroll] lookup by email failed for %s: %v", email, err)
		// Non-fatal — proceed to create.
	}
	if existingOpenID != "" {
		log.Printf("[feishu/auto-enroll] user %s already in Feishu org (open_id=%s), binding only", email, existingOpenID)
		ae.binder(email, existingOpenID)
		ae.trySendWelcome(existingOpenID)
		return &AutoEnrollResult{Status: "ok", Message: "already in org", OpenID: existingOpenID}, nil
	}

	// Step 2: Create user in Feishu org with retry.
	deptID := ae.Config().DepartmentID
	if deptID == "" {
		deptID = "0" // root department
	}

	if displayName == "" {
		if idx := strings.Index(email, "@"); idx > 0 {
			displayName = email[:idx]
		} else {
			displayName = email
		}
	}

	var lastErr error
	for attempt := 0; attempt <= createUserMaxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(attempt)
			log.Printf("[feishu/auto-enroll] retry %d/%d for %s after %s", attempt, createUserMaxRetries, email, delay)
			select {
			case <-ctx.Done():
				return &AutoEnrollResult{Status: "failed", Message: "context cancelled"}, ctx.Err()
			case <-time.After(delay):
			}
			// Re-acquire token in case it expired between retries.
			if t, tErr := ae.acquireToken(ctx); tErr == nil {
				token = t
			}
		}

		openID, createErr := ae.createFeishuUser(ctx, token, email, displayName, deptID, mobile)
		if createErr == nil {
			if openID != "" {
				ae.binder(email, openID)
				ae.trySendWelcome(openID)
				log.Printf("[feishu/auto-enroll] ✅ added %s to Feishu org (open_id=%s, dept=%s, attempt=%d)", email, openID, deptID, attempt)
				return &AutoEnrollResult{Status: "ok", OpenID: openID}, nil
			}
			// API succeeded but returned empty open_id — unexpected.
			log.Printf("[feishu/auto-enroll] createFeishuUser returned empty open_id for %s (attempt %d)", email, attempt)
			lastErr = fmt.Errorf("API returned empty open_id")
			continue
		}

		lastErr = createErr
		errMsg := createErr.Error()
		log.Printf("[feishu/auto-enroll] createFeishuUser failed for %s (attempt %d/%d): %v", email, attempt, createUserMaxRetries, createErr)

		// If the error indicates the user already exists, try lookup instead of retrying create.
		if strings.Contains(errMsg, "already exist") || strings.Contains(errMsg, "40003") {
			log.Printf("[feishu/auto-enroll] user %s may already exist, attempting lookup", email)
			if oid, lookupErr := ae.lookupUserByEmail(ctx, token, email); lookupErr == nil && oid != "" {
				ae.binder(email, oid)
				ae.trySendWelcome(oid)
				return &AutoEnrollResult{Status: "ok", Message: "already in org", OpenID: oid}, nil
			}
		}

		// Don't retry on non-transient errors (permission, invalid params, etc.)
		if strings.Contains(errMsg, "41010") || // mobile required
			strings.Contains(errMsg, "40001") || // invalid param
			strings.Contains(errMsg, "99991663") { // no permission
			log.Printf("[feishu/auto-enroll] non-retryable error for %s: %s", email, errMsg)
			break
		}
	}

	errMsg := "unknown error"
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	return &AutoEnrollResult{Status: "failed", Message: errMsg}, fmt.Errorf("create feishu user: %w", lastErr)
}

// acquireToken obtains a valid tenant access token, retrying once if the
// initial token is empty (e.g. bot just started and hasn't fetched yet).
func (ae *AutoEnroller) acquireToken(ctx context.Context) (string, error) {
	bot := ae.bot()
	if bot == nil {
		log.Printf("[feishu/auto-enroll] bot is nil — feishu app not configured")
		return "", fmt.Errorf("feishu bot not initialized")
	}
	token := bot.TenantAccessToken()
	if token != "" {
		return token, nil
	}

	// Token empty — the bot may not have fetched it yet. Wait briefly and retry.
	log.Printf("[feishu/auto-enroll] tenant access token empty, waiting 3s for token refresh...")
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(3 * time.Second):
	}
	token = bot.TenantAccessToken()
	if token != "" {
		return token, nil
	}
	log.Printf("[feishu/auto-enroll] tenant access token still empty after retry")
	return "", fmt.Errorf("feishu tenant access token is empty (bot may not be configured)")
}


// ---------------------------------------------------------------------------
// Feishu Contact v3 API helpers
// ---------------------------------------------------------------------------

// feishuAPIResponse is the common response envelope for Feishu APIs.
type feishuAPIResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// lookupUserByEmail calls POST /open-apis/contact/v3/users/batch_get_id
// to find a user's open_id by email.
func (ae *AutoEnroller) lookupUserByEmail(ctx context.Context, token, email string) (string, error) {
	apiURL := ae.apiBase() + "/open-apis/contact/v3/users/batch_get_id?user_id_type=open_id"

	body, _ := json.Marshal(map[string]any{
		"emails": []string{email},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result feishuAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("API error: code=%d msg=%s", result.Code, result.Msg)
	}

	// Parse the user_list from response data.
	var data struct {
		UserList []struct {
			UserID string `json:"user_id"`
		} `json:"user_list"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return "", fmt.Errorf("parse user_list: %w", err)
	}
	if len(data.UserList) > 0 && data.UserList[0].UserID != "" {
		return data.UserList[0].UserID, nil
	}
	return "", nil
}

// createFeishuUser calls POST /open-apis/contact/v3/users to add a user
// to the Feishu organization.
//
// Required fields:
//   - name (always required)
//   - department_ids (always required)
//   - employee_type (always required, default 1 = Regular)
//   - mobile (required for Feishu/China, optional for Lark/overseas)
//   - email (optional, but at least one of email/mobile must be present)
//
// Since Hub only has the user's email (no phone number), this works
// directly on Lark (overseas). For Feishu (China), the mobile parameter
// must be provided — the API will return error 41010 ("mobile cannot be
// empty") if it is missing.
//
// Required scope: contact:user (write).
func (ae *AutoEnroller) createFeishuUser(ctx context.Context, token, email, name, departmentID, mobile string) (openID string, err error) {
	apiURL := ae.apiBase() + "/open-apis/contact/v3/users?user_id_type=open_id&department_id_type=department_id"

	empType := ae.Config().EmployeeType
	if empType < 1 || empType > 5 {
		empType = 1 // default: Regular
	}

	payload := map[string]any{
		"name":            name,
		"email":           email,
		"department_ids":  []string{departmentID},
		"employee_type":   empType,
	}
	if mobile != "" {
		payload["mobile"] = mobile
	}
	body, _ := json.Marshal(payload)

	log.Printf("[feishu/auto-enroll] createFeishuUser request: email=%s name=%q dept=%s mobile=%q empType=%d", email, name, departmentID, mobile, empType)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[feishu/auto-enroll] createFeishuUser HTTP error for %s: %v", email, err)
		return "", err
	}
	defer resp.Body.Close()

	var result feishuAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if result.Code != 0 {
		log.Printf("[feishu/auto-enroll] createFeishuUser API error for %s: code=%d msg=%s", email, result.Code, result.Msg)
		return "", fmt.Errorf("API error: code=%d msg=%s", result.Code, result.Msg)
	}

	// Extract the created user's open_id.
	var data struct {
		User struct {
			OpenID string `json:"open_id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return "", fmt.Errorf("parse user: %w", err)
	}
	log.Printf("[feishu/auto-enroll] createFeishuUser success for %s: open_id=%s", email, data.User.OpenID)
	return data.User.OpenID, nil
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// normalizeChinaMobile ensures a Chinese mobile number has the +86 prefix.
// Accepts formats like "15646550398", "86 15646550398", "+86 15646550398",
// "+8615646550398" and normalizes to "+8615646550398".
func normalizeChinaMobile(mobile string) string {
	// Strip spaces and dashes.
	mobile = strings.ReplaceAll(mobile, " ", "")
	mobile = strings.ReplaceAll(mobile, "-", "")

	if strings.HasPrefix(mobile, "+86") {
		return mobile // already correct
	}
	if strings.HasPrefix(mobile, "86") && len(mobile) == 13 {
		return "+" + mobile
	}
	if len(mobile) == 11 && (mobile[0] == '1') {
		return "+86" + mobile
	}
	// Unknown format — return as-is and let the API validate.
	return mobile
}

// ---------------------------------------------------------------------------
// Settings persistence
// ---------------------------------------------------------------------------

// LoadAutoEnrollSetting reads the auto-enroll config from system settings.
func LoadAutoEnrollSetting(ctx context.Context, settings store.SystemSettingsRepository) AutoEnrollConfig {
	raw, err := settings.Get(ctx, settingsKeyAutoEnroll)
	if err != nil || raw == "" {
		return AutoEnrollConfig{}
	}
	var cfg AutoEnrollConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return AutoEnrollConfig{}
	}
	return cfg
}

// SaveAutoEnrollSetting persists the auto-enroll config to system settings.
func SaveAutoEnrollSetting(ctx context.Context, settings store.SystemSettingsRepository, cfg AutoEnrollConfig) error {
	data, _ := json.Marshal(cfg)
	return settings.Set(ctx, settingsKeyAutoEnroll, string(data))
}
