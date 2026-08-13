package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"golang.org/x/text/unicode/norm"
)

const userReferralSettingsKey = "user_referral_config"

var userReferralMu sync.Mutex

// userReferralConfigMu serializes compare-and-save in a Hub process. The
// version is returned to admin clients and accepted through If-Match so the
// settings and rewards screens cannot silently overwrite one another.
var userReferralConfigMu sync.Mutex

const userReferralIdempotencyTTL = 10 * time.Minute
const userReferralRegistrationSessionTTL = 15 * time.Minute
const userReferralHandoffTTL = 30 * time.Minute
const userReferralReservedReviewTTL = 7 * 24 * time.Hour
const userReferralRegistrationCookie = "maclaw_referral_registration"
const userReferralRegistrationHeader = "X-MaClaw-Referral-Session"
const userReferralRegistrationTenantHeader = "X-MaClaw-Referral-Tenant"

// userReferralMaxRewardCredits is a per-recipient, per-registration guardrail.
// Larger campaigns should be split into explicit grants so a mistyped admin
// setting cannot create an unbounded liability on every registration.
const userReferralMaxRewardCredits = 1_000_000.00

const (
	userReferralMetricLanding               = "landing"
	userReferralMetricRegistrationSession   = "registration_session_created"
	userReferralMetricExistingAccount       = "existing_account"
	userReferralMetricVerificationSucceeded = "verification_succeeded"
	userReferralMetricAttributionSucceeded  = "attribution_succeeded"
	userReferralMetricRewardFailed          = "reward_failed"
	userReferralMetricRiskRejected          = "risk_rejected"
	userReferralMetricRewardUsed            = "reward_used"
	userReferralMetricRewardExpired         = "reward_expired"
)

// UserReferralRewardFailureAlertThreshold is deliberately conservative: it
// turns a durable retry backlog into an operator-visible warning before a
// tenant accumulates a material amount of unissued Credits. Administrators
// can still retry individual records below this threshold from the Hub UI.
const UserReferralRewardFailureAlertThreshold = 10

const (
	userReferralPublicWindow      = time.Hour
	userReferralLandingLimit      = 60
	userReferralVerificationLimit = 10
	userReferralRegistrationLimit = 10
)

type userReferralIdempotencyRecord struct {
	fingerprint string
	status      int
	payload     []byte
	expiresAt   time.Time
}

var userReferralIdempotency = struct {
	sync.Mutex
	records map[string]userReferralIdempotencyRecord
}{records: map[string]userReferralIdempotencyRecord{}}

type userReferralRateCounter struct {
	windowStart time.Time
	count       int
}

var userReferralRateLimits = struct {
	sync.Mutex
	entries map[string]userReferralRateCounter
}{entries: map[string]userReferralRateCounter{}}

type userReferralDailyRiskCounter struct {
	dayStart time.Time
	count    int
}

// userReferralNetworkClientRisk keeps only a keyed digest of the remote
// network address and browser/client fingerprint. It is deliberately
// in-memory and short-lived: the value is a registration-risk signal, not a
// durable device profile or analytics identifier.
var userReferralNetworkClientRisk = struct {
	sync.Mutex
	entries map[string]userReferralDailyRiskCounter
}{entries: map[string]userReferralDailyRiskCounter{}}

func referralUserAgentHash(r *http.Request) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(r.UserAgent())))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func userReferralRemoteHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || host == "" {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func userReferralNetworkClientRiskHash(tenantID string, r *http.Request) string {
	// HMAC prevents the short-lived map key from becoming a reusable hash of a
	// raw address. Include the tenant to prevent one tenant's activity from
	// influencing another tenant's referral review queue.
	mac := hmac.New(sha256.New, []byte("maclaw-user-referral-risk-v1:"+store.NormalizeTenantID(tenantID)))
	_, _ = mac.Write([]byte(userReferralRemoteHost(r)))
	_, _ = mac.Write([]byte("\x00"))
	_, _ = mac.Write([]byte(strings.TrimSpace(r.UserAgent())))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// userReferralNeedsNetworkClientReview records one completed registration for
// the anonymous network/client signature and reports whether it exceeds the
// daily review threshold. It is invoked only after the invitee user has been
// created successfully. The signal does not reject the user or remove a
// registration; it merely withholds automatic rewards pending approval.
func userReferralNeedsNetworkClientReview(tenantID string, r *http.Request, dailyCap int, now time.Time) bool {
	if dailyCap <= 0 || r == nil {
		return false
	}
	key := userReferralNetworkClientRiskHash(tenantID, r)
	if key == "" {
		return false
	}
	dayStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	userReferralNetworkClientRisk.Lock()
	defer userReferralNetworkClientRisk.Unlock()
	for entryKey, entry := range userReferralNetworkClientRisk.entries {
		if entry.dayStart.Before(dayStart) {
			delete(userReferralNetworkClientRisk.entries, entryKey)
		}
	}
	entry := userReferralNetworkClientRisk.entries[key]
	if !entry.dayStart.Equal(dayStart) {
		entry = userReferralDailyRiskCounter{dayStart: dayStart}
	}
	entry.count++
	userReferralNetworkClientRisk.entries[key] = entry
	return entry.count > dailyCap
}

func userReferralRegistrationSessionTokenHash(tenantID, token string) string {
	digest := sha256.Sum256([]byte(store.NormalizeTenantID(tenantID) + "\x00" + strings.TrimSpace(token)))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func userReferralHandoffTokenHash(token string) string {
	digest := sha256.Sum256([]byte("maclaw-user-referral-handoff-v1\x00" + strings.TrimSpace(token)))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func userReferralRegistrationSessionToken(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	if cookie, err := r.Cookie(userReferralRegistrationCookie); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value), true
	}
	// Desktop handoff requests use a bearer-like session header because they
	// cannot inherit the browser's HttpOnly cookie. It is accepted only after a
	// one-time handoff claim has issued it; it never contains a referral code.
	if token := strings.TrimSpace(r.Header.Get(userReferralRegistrationHeader)); token != "" {
		return token, true
	}
	return "", false
}

func userReferralIdentityHash(tenantID, identityType, value string) string {
	identityType = strings.ToLower(strings.TrimSpace(identityType))
	value = userReferralNormalizedIdentityValue(identityType, value)
	digest := sha256.Sum256([]byte("maclaw-user-referral-identity-v1\x00" + store.NormalizeTenantID(tenantID) + "\x00" + identityType + "\x00" + value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// userReferralLegacyPhoneIdentityHash preserves lookup compatibility with
// referral reservations written before phone values became canonical E.164.
// It is only used as a fallback for pre-existing reservations; new writes use
// the E.164 hash so country-code aliases cannot reserve separate accounts.
func userReferralLegacyPhoneIdentityHash(tenantID, value string) string {
	digits := normalizePhoneNumber(norm.NFC.String(strings.TrimSpace(value)))
	digest := sha256.Sum256([]byte("maclaw-user-referral-identity-v1\x00" + store.NormalizeTenantID(tenantID) + "\x00phone\x00" + digits))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func userReferralLegacyPhoneIdentityHashes(tenantID, value string) []string {
	values := []string{normalizePhoneNumber(norm.NFC.String(strings.TrimSpace(value)))}
	canonicalDigits := strings.TrimPrefix(userReferralCanonicalE164Phone(value), "+")
	if canonicalDigits != "" {
		values = append(values, canonicalDigits)
		if strings.HasPrefix(canonicalDigits, "86") && len(canonicalDigits) == 13 && canonicalDigits[2] == '1' {
			values = append(values, canonicalDigits[2:])
		}
	}
	seen := make(map[string]struct{}, len(values))
	hashes := make([]string, 0, len(values))
	for _, item := range values {
		if item == "" {
			continue
		}
		hash := userReferralLegacyPhoneIdentityHash(tenantID, item)
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}
	return hashes
}

// recordUserReferralMetric is intentionally best-effort. It records a
// tenant/day/event aggregate only, never a referral code, contact, user ID,
// IP address, device fingerprint, or user agent.
func recordUserReferralMetric(ctx context.Context, repo store.UserReferralRepository, tenantID, event string) {
	if repo == nil || strings.TrimSpace(event) == "" {
		return
	}
	_ = repo.IncrementDailyMetric(ctx, tenantID, event, time.Now().UTC())
}

// userReferralNormalizedIdentityValue normalizes e-mail with NFC/lowercase
// and phone identity to E.164. This canonical form is intentionally scoped to
// referral attribution: legacy account and provider routes continue to use
// their existing digits-only protocol during a separately managed migration.
func userReferralNormalizedIdentityValue(identityType, value string) string {
	value = norm.NFC.String(strings.TrimSpace(value))
	if strings.EqualFold(strings.TrimSpace(identityType), "phone") {
		return userReferralCanonicalE164Phone(value)
	}
	return strings.ToLower(value)
}

func userReferralCanonicalE164Phone(value string) string {
	raw := norm.NFC.String(strings.TrimSpace(value))
	digits := normalizePhoneNumber(value)
	if digits == "" {
		return ""
	}
	// China is the existing default SMS market. Treat national numbers and the
	// common +86/0086 variants as one canonical identity. Other explicitly
	// international numbers retain their country code; a bare non-CN number is
	// left untouched because this Hub has no tenant country configuration yet.
	if strings.HasPrefix(digits, "0086") && len(digits) == 15 && digits[4] == '1' {
		digits = digits[4:]
	}
	if !strings.HasPrefix(raw, "+") && strings.HasPrefix(digits, "86") && len(digits) == 13 && digits[2] == '1' {
		digits = digits[2:]
	}
	if strings.HasPrefix(raw, "+") && strings.HasPrefix(digits, "86") && len(digits) == 13 && digits[2] == '1' {
		return "+" + digits
	}
	if strings.HasPrefix(raw, "+") {
		return "+" + digits
	}
	if len(digits) == 11 && digits[0] == '1' {
		return "+86" + digits
	}
	return "+" + digits
}

func userReferralRegistrationSessionHash(r *http.Request, tenantID string) (string, bool) {
	if r == nil {
		return "", false
	}
	token, ok := userReferralRegistrationSessionToken(r)
	if !ok {
		return "", false
	}
	return userReferralRegistrationSessionTokenHash(tenantID, token), true
}

// reserveUserReferralIdentity creates the durable pre-verification ownership
// record for a normalized tenant identity. It is intentionally keyed by a
// salted hash rather than contact data so it is safe to persist and inspect.
func reserveUserReferralIdentity(ctx context.Context, repo store.UserReferralRepository, r *http.Request, tenantID, codeHash, identityType, value string) (string, bool, error) {
	if repo == nil {
		return "", false, errors.New("referral reservation store is unavailable")
	}
	sessionHash, ok := userReferralRegistrationSessionHash(r, tenantID)
	if !ok {
		return "", false, nil
	}
	identityHash := userReferralIdentityHash(tenantID, identityType, value)
	if strings.EqualFold(strings.TrimSpace(identityType), "phone") {
		for _, legacyHash := range userReferralLegacyPhoneIdentityHashes(tenantID, value) {
			if legacyHash == identityHash {
				continue
			}
			legacy, getErr := repo.GetIdentityReservation(ctx, tenantID, legacyHash, time.Now().UTC())
			if getErr != nil {
				return "", false, getErr
			}
			if legacy != nil && (legacy.CodeHash != strings.TrimSpace(codeHash) || legacy.SessionHash != sessionHash) {
				return identityHash, false, nil
			}
		}
	}
	now := time.Now().UTC()
	reserved, err := repo.ReserveIdentity(ctx, &store.UserReferralIdentityReservation{TenantID: tenantID, IdentityHash: identityHash, CodeHash: strings.TrimSpace(codeHash), SessionHash: sessionHash, ReservedAt: now, ExpiresAt: now.Add(userReferralRegistrationSessionTTL)}, now)
	return identityHash, reserved, err
}

func verifyUserReferralIdentityReservation(ctx context.Context, repo store.UserReferralRepository, r *http.Request, tenantID, codeHash, identityHash string) bool {
	if repo == nil || strings.TrimSpace(identityHash) == "" {
		return false
	}
	sessionHash, ok := userReferralRegistrationSessionHash(r, tenantID)
	if !ok {
		return false
	}
	item, err := repo.GetIdentityReservation(ctx, tenantID, identityHash, time.Now().UTC())
	return err == nil && item != nil && item.CodeHash == strings.TrimSpace(codeHash) && item.SessionHash == sessionHash
}

// verifyUserReferralIdentityReservationForValue accepts both the canonical
// E.164 reservation and the historic digits-only hash. The latter lets an
// in-flight session created before the E.164 rollout finish safely, while all
// newly created reservations use the canonical hash.
func verifyUserReferralIdentityReservationForValue(ctx context.Context, repo store.UserReferralRepository, r *http.Request, tenantID, codeHash, identityType, value string) bool {
	if verifyUserReferralIdentityReservation(ctx, repo, r, tenantID, codeHash, userReferralIdentityHash(tenantID, identityType, value)) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(identityType), "phone") {
		for _, legacyHash := range userReferralLegacyPhoneIdentityHashes(tenantID, value) {
			if legacyHash != "" && verifyUserReferralIdentityReservation(ctx, repo, r, tenantID, codeHash, legacyHash) {
				return true
			}
		}
	}
	return false
}

func releaseUserReferralIdentityReservation(ctx context.Context, repo store.UserReferralRepository, r *http.Request, tenantID, identityHash string) {
	if repo == nil || strings.TrimSpace(identityHash) == "" {
		return
	}
	if sessionHash, ok := userReferralRegistrationSessionHash(r, tenantID); ok {
		_ = repo.ReleaseIdentityReservation(ctx, tenantID, identityHash, sessionHash)
	}
}

func writeUserReferralIdentityReserved(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "REFERRAL_IDENTITY_RESERVED", "This registration is already in progress. Open the invitation link again later.")
}

func clearUserReferralRegistrationSessionCookie(w http.ResponseWriter, r *http.Request) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: userReferralRegistrationCookie, Value: "", Path: "/invite/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: r != nil && r.TLS != nil, SameSite: http.SameSiteLaxMode})
}

func newUserReferralRegistrationSession(ctx context.Context, repo store.UserReferralRepository, w http.ResponseWriter, r *http.Request, tenantID, codeHash, configEpoch string) error {
	token, err := newUserReferralRegistrationSessionToken(ctx, repo, r, tenantID, codeHash, configEpoch)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: userReferralRegistrationCookie, Value: token, Path: "/invite/", MaxAge: int(userReferralRegistrationSessionTTL.Seconds()), HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
	return nil
}

func newUserReferralRegistrationSessionToken(ctx context.Context, repo store.UserReferralRepository, r *http.Request, tenantID, codeHash, configEpoch string) (string, error) {
	raw, err := newUserReferralCode()
	if err != nil {
		return "", err
	}
	// The session token is unrelated to the referral code. Only the code HMAC
	// is retained server-side, so a memory dump or request header cannot recover
	// the invitation link.
	token := strings.TrimPrefix(raw, "rf_")
	now := time.Now().UTC()
	if repo == nil {
		return "", errors.New("referral session store is unavailable")
	}
	if err := repo.SaveRegistrationSession(ctx, &store.UserReferralRegistrationSession{TenantID: tenantID, TokenHash: userReferralRegistrationSessionTokenHash(tenantID, token), CodeHash: strings.TrimSpace(codeHash), ConfigEpoch: strings.TrimSpace(configEpoch), UserAgentHash: referralUserAgentHash(r), ExpiresAt: now.Add(userReferralRegistrationSessionTTL), CreatedAt: now}); err != nil {
		return "", err
	}
	recordUserReferralMetric(ctx, repo, tenantID, userReferralMetricRegistrationSession)
	return token, nil
}

func verifyUserReferralRegistrationSession(ctx context.Context, repo store.UserReferralRepository, w http.ResponseWriter, r *http.Request, tenantID, codeHash, configEpoch string) bool {
	token, ok := userReferralRegistrationSessionToken(r)
	if !ok {
		writeError(w, http.StatusForbidden, "REFERRAL_SESSION_REQUIRED", "Open the invitation link again before registering")
		return false
	}
	if repo == nil {
		writeError(w, http.StatusServiceUnavailable, "REFERRAL_SESSION_FAILED", "Registration is unavailable")
		return false
	}
	session, err := repo.GetRegistrationSession(ctx, tenantID, userReferralRegistrationSessionTokenHash(tenantID, token), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "REFERRAL_SESSION_FAILED", "Registration is unavailable")
		return false
	}
	if session == nil {
		clearUserReferralRegistrationSessionCookie(w, r)
		writeError(w, http.StatusForbidden, "REFERRAL_SESSION_EXPIRED", "Open the invitation link again before registering")
		return false
	}
	valid := session.TenantID == store.NormalizeTenantID(tenantID) && session.CodeHash == strings.TrimSpace(codeHash) && session.ConfigEpoch == strings.TrimSpace(configEpoch) && session.UserAgentHash == referralUserAgentHash(r)
	if !valid {
		clearUserReferralRegistrationSessionCookie(w, r)
		writeError(w, http.StatusForbidden, "REFERRAL_SESSION_INVALID", "Open the invitation link again before registering")
	}
	return valid
}

func allowUserReferralPublicRequest(r *http.Request, category string, limit int) bool {
	if r == nil || limit <= 0 {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || host == "" {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	if host == "" {
		host = "unknown"
	}
	now := time.Now().UTC()
	key := category + "\x00" + host
	userReferralRateLimits.Lock()
	defer userReferralRateLimits.Unlock()
	for entryKey, entry := range userReferralRateLimits.entries {
		if now.Sub(entry.windowStart) >= userReferralPublicWindow {
			delete(userReferralRateLimits.entries, entryKey)
		}
	}
	entry := userReferralRateLimits.entries[key]
	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= userReferralPublicWindow {
		entry = userReferralRateCounter{windowStart: now}
	}
	if entry.count >= limit {
		return false
	}
	entry.count++
	userReferralRateLimits.entries[key] = entry
	return true
}

func rejectUserReferralRateLimit(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "3600")
	writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please try again later")
}

func referralRequestFingerprint(code, email, phone, verifyCode string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, strings.TrimSpace(code))
	_, _ = io.WriteString(h, "\x00")
	_, _ = io.WriteString(h, userReferralNormalizedIdentityValue("email", email))
	_, _ = io.WriteString(h, "\x00")
	_, _ = io.WriteString(h, userReferralNormalizedIdentityValue("phone", phone))
	_, _ = io.WriteString(h, "\x00")
	_, _ = io.WriteString(h, strings.TrimSpace(verifyCode))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func userReferralIdempotencyKey(r *http.Request) (string, error) {
	if r == nil {
		return "", nil
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return "", nil
	}
	if len(key) > 200 || strings.ContainsAny(key, "\r\n\t") {
		return "", errors.New("invalid idempotency key")
	}
	return key, nil
}

func userReferralIdempotencyKeyHash(tenantID, key string) string {
	digest := sha256.Sum256([]byte(store.NormalizeTenantID(tenantID) + "\x00" + strings.TrimSpace(key)))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func beginUserReferralIdempotency(r *http.Request, fingerprint string) (string, *userReferralIdempotencyRecord, error) {
	key, err := userReferralIdempotencyKey(r)
	if err != nil || key == "" {
		return "", nil, err
	}
	// The raw header value is retained only as a lookup key for the short-lived
	// process-local cache. The stored fingerprint detects both a different
	// payload and a different referral/tenant, preventing cross-request replay.
	now := time.Now().UTC()
	userReferralIdempotency.Lock()
	defer userReferralIdempotency.Unlock()
	for existingKey, record := range userReferralIdempotency.records {
		if !record.expiresAt.After(now) {
			delete(userReferralIdempotency.records, existingKey)
		}
	}
	if record, ok := userReferralIdempotency.records[key]; ok {
		if record.fingerprint != fingerprint {
			return "", nil, errors.New("idempotency key is already used for another request")
		}
		return key, &record, nil
	}
	return key, nil, nil
}

// beginPersistedUserReferralIdempotency makes completed registration retries
// durable. The tenant-scoped key hash prevents one tenant from replaying a
// response produced by another tenant, while keeping the raw header out of
// durable storage.
func beginPersistedUserReferralIdempotency(ctx context.Context, repo store.UserReferralRepository, tenantID string, r *http.Request, fingerprint string) (string, *userReferralIdempotencyRecord, error) {
	key, err := userReferralIdempotencyKey(r)
	if err != nil || key == "" {
		return "", nil, err
	}
	keyHash := userReferralIdempotencyKeyHash(tenantID, key)
	item, err := repo.GetRegistrationIdempotency(ctx, tenantID, keyHash, time.Now().UTC())
	if err != nil || item == nil {
		return keyHash, nil, err
	}
	if item.Fingerprint != fingerprint {
		return "", nil, errors.New("idempotency key is already used for another request")
	}
	return keyHash, &userReferralIdempotencyRecord{fingerprint: item.Fingerprint, status: item.Status, payload: append([]byte(nil), item.Payload...), expiresAt: item.ExpiresAt}, nil
}

func finishPersistedUserReferralIdempotency(ctx context.Context, repo store.UserReferralRepository, tenantID, keyHash, fingerprint string, status int, payload []byte) error {
	if keyHash == "" || len(payload) == 0 {
		return nil
	}
	now := time.Now().UTC()
	return repo.SaveRegistrationIdempotency(ctx, &store.UserReferralRegistrationIdempotency{TenantID: tenantID, KeyHash: keyHash, Fingerprint: fingerprint, Status: status, Payload: append([]byte(nil), payload...), ExpiresAt: now.Add(userReferralIdempotencyTTL), CreatedAt: now})
}

func finishUserReferralIdempotency(key, fingerprint string, status int, payload []byte) {
	if key == "" || len(payload) == 0 {
		return
	}
	userReferralIdempotency.Lock()
	defer userReferralIdempotency.Unlock()
	userReferralIdempotency.records[key] = userReferralIdempotencyRecord{fingerprint: fingerprint, status: status, payload: append([]byte(nil), payload...), expiresAt: time.Now().UTC().Add(userReferralIdempotencyTTL)}
}

func replayUserReferralIdempotency(w http.ResponseWriter, record *userReferralIdempotencyRecord) bool {
	if record == nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Idempotency-Replayed", "true")
	w.WriteHeader(record.status)
	_, _ = w.Write(record.payload)
	return true
}

type UserReferralDownloads struct {
	WindowsAMD64 string `json:"windows_amd64"`
	WindowsARM64 string `json:"windows_arm64"`
	MacOSAMD64   string `json:"macos_amd64"`
	MacOSARM64   string `json:"macos_arm64"`
	LinuxAMD64   string `json:"linux_amd64"`
	LinuxARM64   string `json:"linux_arm64"`
}

type UserReferralConfig struct {
	Enabled bool `json:"enabled"`
	// SessionEpoch is intentionally omitted from API responses by
	// writeUserReferralConfig, but persisted with the tenant setting so a
	// disable/enable cycle cannot revive browser sessions minted before it.
	SessionEpoch                string                `json:"session_epoch,omitempty"`
	InviterCredits              float64               `json:"inviter_credits"`
	InviteeCredits              float64               `json:"invitee_credits"`
	DurationDays                int                   `json:"duration_days"`
	DailyRewardCap              int                   `json:"daily_reward_cap"`
	DailyNetworkClientReviewCap int                   `json:"daily_network_client_review_cap"`
	ServiceGroupID              string                `json:"service_group_id,omitempty"`
	Downloads                   UserReferralDownloads `json:"downloads"`
}

func defaultUserReferralDownloads() UserReferralDownloads {
	return UserReferralDownloads{
		WindowsAMD64: "https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-windows-amd64.exe",
		WindowsARM64: "https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-windows-arm64.exe",
		MacOSAMD64:   "https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-darwin-amd64",
		MacOSARM64:   "https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-darwin-arm64",
		LinuxAMD64:   "https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-linux-amd64",
		LinuxARM64:   "https://github.com/RapidAI/MaClaw/releases/latest/download/Ins-maclaw-linux-arm64",
	}
}

func defaultUserReferralConfig() UserReferralConfig {
	return UserReferralConfig{SessionEpoch: "legacy", DurationDays: 30, DailyRewardCap: 20, DailyNetworkClientReviewCap: 3, Downloads: defaultUserReferralDownloads()}
}

func loadUserReferralConfig(ctx context.Context, system store.SystemSettingsRepository, tenantID string) (UserReferralConfig, error) {
	cfg := defaultUserReferralConfig()
	if system == nil {
		return cfg, nil
	}
	raw, err := ScopedSystemSettingsForTenant(tenantID, system).Get(ctx, userReferralSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cfg, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultUserReferralConfig(), nil
	}
	if strings.TrimSpace(cfg.SessionEpoch) == "" {
		cfg.SessionEpoch = "legacy"
	}
	return normalizeUserReferralConfig(cfg), nil
}

func userReferralConfigVersion(raw string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return `"` + base64.RawURLEncoding.EncodeToString(digest[:]) + `"`
}

func loadUserReferralConfigWithVersion(ctx context.Context, system store.SystemSettingsRepository, tenantID string) (UserReferralConfig, string, error) {
	cfg := defaultUserReferralConfig()
	if system == nil {
		return cfg, userReferralConfigVersion(""), nil
	}
	raw, err := ScopedSystemSettingsForTenant(tenantID, system).Get(ctx, userReferralSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cfg, userReferralConfigVersion(raw), err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultUserReferralConfig(), userReferralConfigVersion(raw), nil
	}
	if strings.TrimSpace(cfg.SessionEpoch) == "" {
		cfg.SessionEpoch = "legacy"
	}
	return normalizeUserReferralConfig(cfg), userReferralConfigVersion(raw), nil
}

func writeUserReferralConfig(w http.ResponseWriter, status int, cfg UserReferralConfig, version string) {
	w.Header().Set("ETag", version)
	writeJSON(w, status, map[string]any{
		"enabled":                         cfg.Enabled,
		"inviter_credits":                 cfg.InviterCredits,
		"invitee_credits":                 cfg.InviteeCredits,
		"duration_days":                   cfg.DurationDays,
		"daily_reward_cap":                cfg.DailyRewardCap,
		"daily_network_client_review_cap": cfg.DailyNetworkClientReviewCap,
		"service_group_id":                cfg.ServiceGroupID,
		"downloads":                       cfg.Downloads,
		"version":                         version,
	})
}

func normalizeUserReferralConfig(cfg UserReferralConfig) UserReferralConfig {
	if cfg.DurationDays <= 0 {
		cfg.DurationDays = 30
	}
	if cfg.DurationDays > 3650 {
		cfg.DurationDays = 3650
	}
	if cfg.DailyRewardCap <= 0 {
		cfg.DailyRewardCap = 20
	}
	if cfg.DailyRewardCap > 100000 {
		cfg.DailyRewardCap = 100000
	}
	if cfg.DailyNetworkClientReviewCap <= 0 {
		cfg.DailyNetworkClientReviewCap = 3
	}
	if cfg.DailyNetworkClientReviewCap > 100000 {
		cfg.DailyNetworkClientReviewCap = 100000
	}
	if cfg.InviterCredits < 0 {
		cfg.InviterCredits = 0
	}
	if cfg.InviteeCredits < 0 {
		cfg.InviteeCredits = 0
	}
	defaults := defaultUserReferralDownloads()
	for _, pair := range []struct {
		target   *string
		fallback string
	}{{&cfg.Downloads.WindowsAMD64, defaults.WindowsAMD64}, {&cfg.Downloads.WindowsARM64, defaults.WindowsARM64}, {&cfg.Downloads.MacOSAMD64, defaults.MacOSAMD64}, {&cfg.Downloads.MacOSARM64, defaults.MacOSARM64}, {&cfg.Downloads.LinuxAMD64, defaults.LinuxAMD64}, {&cfg.Downloads.LinuxARM64, defaults.LinuxARM64}} {
		*pair.target = normalizeReferralDownloadURL(*pair.target, pair.fallback)
	}
	cfg.ServiceGroupID = strings.TrimSpace(cfg.ServiceGroupID)
	return cfg
}

func validateUserReferralCredits(inviterCredits, inviteeCredits float64) error {
	for _, item := range []struct {
		name  string
		value float64
	}{
		{name: "inviter credits", value: inviterCredits},
		{name: "invitee credits", value: inviteeCredits},
	} {
		if math.IsNaN(item.value) || math.IsInf(item.value, 0) {
			return fmt.Errorf("%s must be a finite number", item.name)
		}
		if item.value < 0 {
			return fmt.Errorf("%s cannot be negative", item.name)
		}
		if item.value > userReferralMaxRewardCredits {
			return fmt.Errorf("%s cannot exceed %.2f", item.name, userReferralMaxRewardCredits)
		}
		// Credits are settled to cents. Reject, rather than silently round, an
		// administrator's intended reward amount.
		if math.Abs(item.value*100-math.Round(item.value*100)) > 1e-8 {
			return fmt.Errorf("%s can have at most two decimal places", item.name)
		}
	}
	return nil
}

func normalizeReferralDownloadURL(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(raw) > 2048 {
		return fallback
	}
	return raw
}

func validateUserReferralDownloads(downloads UserReferralDownloads) error {
	for _, item := range []struct {
		name string
		url  string
	}{
		{"windows_amd64", downloads.WindowsAMD64}, {"windows_arm64", downloads.WindowsARM64},
		{"macos_amd64", downloads.MacOSAMD64}, {"macos_arm64", downloads.MacOSARM64},
		{"linux_amd64", downloads.LinuxAMD64}, {"linux_arm64", downloads.LinuxARM64},
	} {
		raw := strings.TrimSpace(item.url)
		if raw == "" {
			continue // Empty means use the official fallback URL.
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t") {
			return fmt.Errorf("invalid %s download URL", item.name)
		}
	}
	return nil
}

func UpdateUserReferralConfigHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg UserReferralConfig
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&cfg); err != nil {
			writeError(w, 400, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := validateUserReferralCredits(cfg.InviterCredits, cfg.InviteeCredits); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REFERRAL_CREDITS", err.Error())
			return
		}
		if err := validateUserReferralDownloads(cfg.Downloads); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DOWNLOAD_URL", err.Error())
			return
		}
		cfg = normalizeUserReferralConfig(cfg)
		if (cfg.InviterCredits > 0 || cfg.InviteeCredits > 0) && strings.TrimSpace(cfg.ServiceGroupID) == "" {
			writeError(w, 400, "SERVICE_GROUP_REQUIRED", "Service group ID is required when referral credits are enabled")
			return
		}
		tenantID := RequestTenantID(r)
		if cfg.InviterCredits > 0 || cfg.InviteeCredits > 0 {
			registry, registryErr := llmservice.LoadRegistry(r.Context(), ScopedSystemSettingsForTenant(tenantID, system))
			if registryErr != nil {
				writeError(w, 500, "SERVICE_GROUP_LOAD_FAILED", "Unable to validate service group")
				return
			}
			if registry == nil || registry.FindModelServiceGroup(cfg.ServiceGroupID) == nil {
				writeError(w, 400, "SERVICE_GROUP_NOT_FOUND", "Referral service group does not exist")
				return
			}
		}
		userReferralConfigMu.Lock()
		defer userReferralConfigMu.Unlock()
		currentCfg, currentVersion, currentErr := loadUserReferralConfigWithVersion(r.Context(), system, tenantID)
		if currentErr != nil {
			writeError(w, http.StatusInternalServerError, "USER_REFERRAL_LOAD_FAILED", currentErr.Error())
			return
		}
		if expected := strings.TrimSpace(r.Header.Get("If-Match")); expected != "" && expected != currentVersion {
			writeErrorWithFields(w, http.StatusConflict, "USER_REFERRAL_CONFIG_CONFLICT", "Invitation settings changed by another administrator. Reload before saving.", map[string]any{"version": currentVersion})
			return
		}
		if cfg.Enabled != currentCfg.Enabled {
			epoch, epochErr := newUserReferralCode()
			if epochErr != nil {
				writeError(w, http.StatusInternalServerError, "USER_REFERRAL_SAVE_FAILED", "Unable to update invitation session state")
				return
			}
			cfg.SessionEpoch = epoch
		} else {
			cfg.SessionEpoch = currentCfg.SessionEpoch
		}
		data, _ := json.Marshal(cfg)
		if err := ScopedSystemSettingsForTenant(tenantID, system).Set(r.Context(), userReferralSettingsKey, string(data)); err != nil {
			writeError(w, 500, "USER_REFERRAL_SAVE_FAILED", err.Error())
			return
		}
		if audit != nil && AdminFromContext(r.Context()) != nil {
			_ = audit.Create(r.Context(), &store.AdminAuditLog{ID: llmservice.NewID("audit"), TenantID: tenantID, AdminUserID: AdminFromContext(r.Context()).ID, Action: "user_referral_config_updated", PayloadJSON: `{"scope":"future_registrations"}`, CreatedAt: time.Now().UTC()})
		}
		writeUserReferralConfig(w, http.StatusOK, cfg, userReferralConfigVersion(string(data)))
	}
}

func GetUserReferralConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, version, err := loadUserReferralConfigWithVersion(r.Context(), system, RequestTenantID(r))
		if err != nil {
			writeError(w, 500, "USER_REFERRAL_LOAD_FAILED", err.Error())
			return
		}
		writeUserReferralConfig(w, http.StatusOK, cfg, version)
	}
}

func userReferralCodeHash(tenantID, code string) string {
	mac := hmac.New(sha256.New, []byte("maclaw-user-referral-v1:"+strings.TrimSpace(tenantID)))
	_, _ = mac.Write([]byte(strings.TrimSpace(code)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func newUserReferralCode() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "rf_" + base64.RawURLEncoding.EncodeToString(b), nil
}
func userReferralURL(r *http.Request, code string) string {
	if r == nil || strings.TrimSpace(code) == "" {
		return ""
	}
	// Hubs are commonly deployed behind a TLS-terminating reverse proxy. Use
	// only the normalized first forwarding value and a syntactically safe host
	// so a malformed header cannot become a browser-visible invitation URL.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if forwarded := normalizeForwardedProto(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := cleanModelDownloadHost(firstForwardedValue(r.Header.Get("X-Forwarded-Host")))
	if host == "" {
		host = cleanModelDownloadHost(r.Host)
	}
	if host == "" {
		return "/invite/" + url.PathEscape(code)
	}
	return scheme + "://" + host + "/invite/" + url.PathEscape(code)
}
func maskReferralID(v string) string {
	r := []rune(strings.TrimSpace(v))
	if len(r) < 6 {
		return "*****"
	}
	return string(r[:3]) + "*****" + string(r[len(r)-2:])
}
func maskReferralContact(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "@") {
		p := strings.SplitN(v, "@", 2)
		if len([]rune(p[0])) > 1 {
			return string([]rune(p[0])[:1]) + "***@" + p[1]
		}
		return "***@" + p[1]
	}
	if len([]rune(v)) > 4 {
		rr := []rune(v)
		return string(rr[:2]) + "****" + string(rr[len(rr)-2:])
	}
	return "***"
}

func GetMyUserInvitationsHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil || identity.UsersRepo() == nil || repo == nil || system == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REFERRAL_UNAVAILABLE", "Invitations are temporarily unavailable")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil {
			writeError(w, 401, "VIEWER_UNAUTHORIZED", "viewer authorization required")
			return
		}
		cfg, err := loadUserReferralConfig(r.Context(), system, principal.TenantID)
		if err != nil {
			writeError(w, 500, "USER_REFERRAL_LOAD_FAILED", err.Error())
			return
		}
		if !cfg.Enabled {
			writeJSON(w, 200, map[string]any{"enabled": false})
			return
		}
		if viewer, viewerErr := identity.UsersRepo().GetByID(r.Context(), principal.UserID); viewerErr != nil || viewer == nil || store.NormalizeTenantID(viewer.TenantID) != store.NormalizeTenantID(principal.TenantID) || !strings.EqualFold(strings.TrimSpace(viewer.Status), "active") {
			writeJSON(w, 200, map[string]any{"enabled": false})
			return
		}
		userReferralMu.Lock()
		code, err := repo.GetActiveCodeForInviter(r.Context(), principal.TenantID, principal.UserID)
		if err == nil && code == nil {
			plain, genErr := newUserReferralCode()
			if genErr != nil {
				err = genErr
			} else {
				enc, encErr := llmservice.EncryptCardCode(plain)
				if encErr != nil {
					err = encErr
				} else {
					code = &store.UserReferralCode{ID: llmservice.NewID("refcode"), TenantID: principal.TenantID, InviterUserID: principal.UserID, CodeHash: userReferralCodeHash(principal.TenantID, plain), EncryptedCode: enc, Status: "active", CreatedAt: time.Now().UTC()}
					err = repo.CreateCode(r.Context(), code)
				}
			}
		}
		userReferralMu.Unlock()
		if err != nil || code == nil {
			writeError(w, 500, "USER_REFERRAL_CODE_FAILED", "Unable to prepare invitation link")
			return
		}
		plain := llmservice.DecryptCardCode(code.EncryptedCode)
		if plain == "" {
			writeError(w, 500, "USER_REFERRAL_CODE_FAILED", "Unable to retrieve invitation link")
			return
		}
		page, pageErr := referralPage(r)
		if pageErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAGINATION", pageErr.Error())
			return
		}
		invitees, total, err := repo.ListInvitees(r.Context(), store.UserReferralFilter{TenantID: principal.TenantID, InviterUserID: principal.UserID, Offset: (page - 1) * 20, Limit: 20})
		if err != nil {
			writeError(w, 500, "USER_REFERRAL_LIST_FAILED", err.Error())
			return
		}
		list := make([]map[string]any, 0, len(invitees))
		for _, item := range invitees {
			list = append(list, map[string]any{"user_id": item.InviteeUserID, "contact": maskReferralContact(item.InviteeEmail), "registered_at": item.RegisteredAt, "status": item.Status})
		}
		writeJSON(w, 200, map[string]any{"enabled": true, "invite_url": userReferralURL(r, plain), "inviter_credits": cfg.InviterCredits, "invitee_credits": cfg.InviteeCredits, "duration_days": cfg.DurationDays, "invitees": list, "total": total, "page": page, "page_size": 20})
	}
}

// RotateMyUserInvitationHandler invalidates the caller's prior link before it
// issues a replacement. The raw code only ever exists in this authenticated
// response and is never written to an audit payload.
func RotateMyUserInvitationHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil || identity.UsersRepo() == nil || repo == nil || system == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REFERRAL_UNAVAILABLE", "Invitations are temporarily unavailable")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil {
			writeError(w, http.StatusUnauthorized, "VIEWER_UNAUTHORIZED", "viewer authorization required")
			return
		}
		cfg, err := loadUserReferralConfig(r.Context(), system, principal.TenantID)
		if err != nil || !cfg.Enabled {
			writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "Invitations are unavailable")
			return
		}
		if viewer, viewerErr := identity.UsersRepo().GetByID(r.Context(), principal.UserID); viewerErr != nil || viewer == nil || store.NormalizeTenantID(viewer.TenantID) != store.NormalizeTenantID(principal.TenantID) || !strings.EqualFold(strings.TrimSpace(viewer.Status), "active") {
			writeError(w, http.StatusForbidden, "INVITER_INACTIVE", "Invitation link is unavailable")
			return
		}
		userReferralMu.Lock()
		defer userReferralMu.Unlock()
		plain, err := newUserReferralCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_REFERRAL_CODE_FAILED", "Unable to rotate invitation link")
			return
		}
		encrypted, err := llmservice.EncryptCardCode(plain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_REFERRAL_CODE_FAILED", "Unable to rotate invitation link")
			return
		}
		now := time.Now().UTC()
		if err := repo.ReplaceActiveCode(r.Context(), principal.TenantID, principal.UserID, &store.UserReferralCode{ID: llmservice.NewID("refcode"), TenantID: principal.TenantID, InviterUserID: principal.UserID, CodeHash: userReferralCodeHash(principal.TenantID, plain), EncryptedCode: encrypted, Status: "active", CreatedAt: now}, now); err != nil {
			writeError(w, http.StatusInternalServerError, "USER_REFERRAL_CODE_FAILED", "Unable to rotate invitation link")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "invite_url": userReferralURL(r, plain)})
	}
}

// referralPage accepts an omitted page as the first page, but never silently
// coerces an explicit invalid value. Stable pagination is part of the referral
// ledger contract; treating page=0 or page=abc as page one can make operators
// act on a different page than the one they intended to review.
func referralPage(r *http.Request) (int, error) {
	raw := ""
	if r != nil {
		raw = strings.TrimSpace(r.URL.Query().Get("page"))
	}
	if raw == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 0, errors.New("page must be a positive integer")
	}
	return page, nil
}

func userReferralSearchQuery(r *http.Request) (string, error) {
	if r == nil {
		return "", nil
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		// Keep compatibility with the first version of the admin client while
		// standardizing the documented public query parameter.
		query = strings.TrimSpace(r.URL.Query().Get("search"))
	}
	if len([]rune(query)) > 128 {
		return "", errors.New("query must be at most 128 characters")
	}
	return query, nil
}

func ListUserReferralInvitersHandler(repo store.UserReferralRepository, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, pageErr := referralPage(r)
		if pageErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAGINATION", pageErr.Error())
			return
		}
		query, queryErr := userReferralSearchQuery(r)
		if queryErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_QUERY", queryErr.Error())
			return
		}
		items, total, err := repo.ListInviterSummaries(r.Context(), store.UserReferralFilter{TenantID: RequestTenantID(r), Search: query, Offset: (page - 1) * 20, Limit: 20})
		if err != nil {
			writeError(w, 500, "USER_REFERRAL_LIST_FAILED", err.Error())
			return
		}
		out := make([]map[string]any, 0, len(items))
		registry, _ := llmservice.LoadRegistry(r.Context(), ScopedSystemSettingsForTenant(RequestTenantID(r), system))
		for _, x := range items {
			ledger := userReferralGrantLedgerSnapshot(registry, x.InviterGrantIDs, time.Now().UTC())
			out = append(out, map[string]any{"inviter_user_id": x.InviterUserID, "inviter_contact": x.InviterEmail, "invitee_count": x.InviteeCount, "credits_granted": ledger.Granted, "credits_consumed": ledger.Consumed, "credits_available": ledger.Available, "credits_expired": ledger.Expired, "last_registered_at": x.LastRegisteredAt})
		}
		writeJSON(w, 200, map[string]any{"inviters": out, "total": total, "page": page, "page_size": 20})
	}
}

// GetUserReferralMetricsHandler exposes only tenant/day/event aggregates for
// operations dashboards. The retry backlog is derived from durable referral
// state rather than counters, so it remains correct after a process restart.
func GetUserReferralMetricsHandler(repo store.UserReferralRepository, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		from := now.AddDate(0, 0, -29)
		if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
			days, err := strconv.Atoi(raw)
			if err != nil || days < 1 || days > 90 {
				writeError(w, http.StatusBadRequest, "INVALID_METRICS_RANGE", "days must be between 1 and 90")
				return
			}
			from = now.AddDate(0, 0, -(days - 1))
		}
		tenantID := RequestTenantID(r)
		// Expiry is reconciled when the operations view is read rather than on
		// the token hot path. The repository's unique event key makes this safe
		// across refreshes and process restarts while preserving the grant's real
		// expiry date as the metric date.
		if system != nil {
			if registry, loadErr := llmservice.LoadRegistry(r.Context(), ScopedSystemSettingsForTenant(tenantID, system)); loadErr == nil && registry != nil {
				for _, grant := range registry.Grants {
					if grant.Source != "user_referral" || grant.Frozen || grant.ID == "" || now.Before(grant.ExpiresAt) {
						continue
					}
					if _, eventErr := repo.RecordRewardMetricEvent(r.Context(), tenantID, grant.ID, userReferralMetricRewardExpired, grant.ExpiresAt); eventErr != nil {
						log.Printf("[user-referral] record reward expiry metric: %v", eventErr)
					}
				}
			}
		}
		metrics, err := repo.ListDailyMetrics(r.Context(), tenantID, from, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_REFERRAL_METRICS_FAILED", "Unable to load invitation metrics")
			return
		}
		candidates, err := repo.ListRewardRecoveryCandidates(r.Context(), tenantID, 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_REFERRAL_METRICS_FAILED", "Unable to load invitation reward backlog")
			return
		}
		backlog := 0
		for _, referral := range candidates {
			if referral != nil && strings.EqualFold(strings.TrimSpace(referral.Status), "reward_failed") {
				backlog++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"from": from.Format("2006-01-02"), "to": now.Format("2006-01-02"), "metrics": metrics, "reward_failed_backlog": backlog})
	}
}

// userReferralGrantLedger is derived exclusively from the persisted grant
// ledger. Referral rule snapshots tell us what was promised, while this view
// answers what was actually issued, consumed, still usable, or left unused at
// expiry. Frozen (revoked) grants are intentionally kept out of available and
// expired buckets because their audit status is neither.
type userReferralGrantLedger struct {
	Granted   float64
	Consumed  float64
	Available float64
	Expired   float64
}

func userReferralGrantLedgerSnapshot(registry *llmservice.Registry, grantIDs []string, now time.Time) userReferralGrantLedger {
	var ledger userReferralGrantLedger
	if registry == nil || len(grantIDs) == 0 {
		return ledger
	}
	ids := make(map[string]struct{}, len(grantIDs))
	for _, grantID := range grantIDs {
		if grantID = strings.TrimSpace(grantID); grantID != "" {
			ids[grantID] = struct{}{}
		}
	}
	for _, grant := range registry.Grants {
		if _, ok := ids[grant.ID]; !ok {
			continue
		}
		total := math.Max(0, grant.CreditsTotal)
		used := math.Min(total, math.Max(0, grant.CreditsUsed))
		remaining := math.Max(0, total-used)
		ledger.Granted += total
		ledger.Consumed += used
		if grant.Frozen {
			continue
		}
		if !now.Before(grant.ExpiresAt) {
			ledger.Expired += remaining
			continue
		}
		if !now.Before(grant.StartsAt) {
			ledger.Available += remaining
		}
	}
	ledger.Granted = math.Round(ledger.Granted*100) / 100
	ledger.Consumed = math.Round(ledger.Consumed*100) / 100
	ledger.Available = math.Round(ledger.Available*100) / 100
	ledger.Expired = math.Round(ledger.Expired*100) / 100
	return ledger
}
func ListUserReferralInviteesHandler(repo store.UserReferralRepository, systems ...store.SystemSettingsRepository) http.HandlerFunc {
	var system store.SystemSettingsRepository
	if len(systems) > 0 {
		system = systems[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		inviterID := strings.TrimSpace(r.PathValue("inviter_id"))
		if inviterID == "" {
			writeError(w, 400, "INVITER_REQUIRED", "inviter is required")
			return
		}
		page, pageErr := referralPage(r)
		if pageErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAGINATION", pageErr.Error())
			return
		}
		// Administrators need the complete referral lifecycle for an inviter:
		// pending risk reviews and revoked records remain actionable/auditable,
		// while the user-facing dialog remains limited to successful referrals.
		items, total, err := repo.ListReferralInviteesForReview(r.Context(), store.UserReferralFilter{TenantID: RequestTenantID(r), InviterUserID: inviterID, Offset: (page - 1) * 20, Limit: 20})
		if err != nil {
			writeError(w, 500, "USER_REFERRAL_LIST_FAILED", err.Error())
			return
		}
		out := make([]map[string]any, 0, len(items))
		registry, _ := llmservice.LoadRegistry(r.Context(), ScopedSystemSettingsForTenant(RequestTenantID(r), system))
		for _, item := range items {
			history, historyErr := repo.ListStatusHistory(r.Context(), RequestTenantID(r), item.ReferralID)
			if historyErr != nil {
				writeError(w, 500, "USER_REFERRAL_HISTORY_FAILED", historyErr.Error())
				return
			}
			out = append(out, map[string]any{"referral_id": item.ReferralID, "invitee_user_id": item.InviteeUserID, "invitee_email": item.InviteeEmail, "registered_at": item.RegisteredAt, "status": item.Status, "inviter_credits": item.InviterCredits, "invitee_credits": item.InviteeCredits, "inviter_grant_state": userReferralGrantState(registry, item.InviterGrantID, time.Now().UTC()), "invitee_grant_state": userReferralGrantState(registry, item.InviteeGrantID, time.Now().UTC()), "history": history})
		}
		writeJSON(w, 200, map[string]any{"invitees": out, "total": total, "page": page, "page_size": 20})
	}
}

// userReferralGrantState derives the per-beneficiary state from the actual
// grant ledger. Attribution status describes reward delivery, while this value
// tells the administrator whether the issued Credits are active, exhausted,
// expired, frozen, or intentionally absent for a zero-value reward.
func userReferralGrantState(registry *llmservice.Registry, grantID string, now time.Time) string {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return "not_issued"
	}
	if registry == nil {
		return "unavailable"
	}
	for _, grant := range registry.Grants {
		if strings.TrimSpace(grant.ID) != grantID {
			continue
		}
		if grant.Frozen {
			return "frozen"
		}
		if !now.Before(grant.ExpiresAt) {
			return "expired"
		}
		if now.Before(grant.StartsAt) {
			return "pending"
		}
		if grant.CreditsTotal > 0 && grant.CreditsUsed >= grant.CreditsTotal {
			return "exhausted"
		}
		return "active"
	}
	return "unavailable"
}

// ListReservedUserReferralsHandler presents the tenant-scoped risk queue to
// tenant administrators. Reasons stay in the existing status history; the
// operation itself is handled by the same audited approve/reject endpoints.
func ListReservedUserReferralsHandler(repo store.UserReferralRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, pageErr := referralPage(r)
		if pageErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAGINATION", pageErr.Error())
			return
		}
		items, total, err := repo.ListReservedReferrals(r.Context(), RequestTenantID(r), (page-1)*20, 20)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_REFERRAL_REVIEW_LIST_FAILED", "Unable to load referrals awaiting review")
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			history, historyErr := repo.ListStatusHistory(r.Context(), RequestTenantID(r), item.ReferralID)
			if historyErr != nil {
				writeError(w, http.StatusInternalServerError, "USER_REFERRAL_HISTORY_FAILED", "Unable to load referral history")
				return
			}
			reason := ""
			if len(history) > 0 {
				for _, event := range history {
					if event != nil && event.ToStatus == "reserved" {
						reason = event.Reason
						break
					}
				}
			}
			out = append(out, map[string]any{"referral_id": item.ReferralID, "invitee_user_id": item.InviteeUserID, "invitee_email": item.InviteeEmail, "registered_at": item.RegisteredAt, "status": item.Status, "inviter_credits": item.InviterCredits, "invitee_credits": item.InviteeCredits, "review_reason": reason})
		}
		writeJSON(w, http.StatusOK, map[string]any{"referrals": out, "total": total, "page": page, "page_size": 20})
	}
}

func recordUserReferralStatusHistory(ctx context.Context, repo store.UserReferralRepository, tenantID, referralID, fromStatus, toStatus, reason, actorUserID string) error {
	if repo == nil || strings.TrimSpace(referralID) == "" || strings.TrimSpace(toStatus) == "" {
		return nil
	}
	return repo.CreateStatusHistory(ctx, &store.UserReferralStatusHistory{ID: llmservice.NewID("refhist"), TenantID: tenantID, ReferralID: referralID, FromStatus: fromStatus, ToStatus: toStatus, Reason: reason, ActorUserID: actorUserID, CreatedAt: time.Now().UTC()})
}

// recordUserReferralRewardFailure creates a durable, tenant-scoped operational
// signal for the compensation queue. Keep this deliberately free of contact,
// IP, user-agent and referral-code data: the referral ID is enough for an
// authorized administrator to locate and retry the failed work.
func recordUserReferralRewardFailure(ctx context.Context, failureLogs []store.FailureEventLogRepository, tenantID, referralID string) {
	var failureLog store.FailureEventLogRepository
	if len(failureLogs) > 0 {
		failureLog = failureLogs[0]
	}
	diagnostics.NewFailureEventRecorder(failureLog).Record(ctx, diagnostics.FailureEventInput{
		TenantID:  tenantID,
		Category:  "user_referral",
		EventCode: "reward_failed",
		Message:   "Referral reward processing failed",
		EntityID:  referralID,
		Details:   map[string]any{"status": "reward_failed"},
	})
}

func RetryUserReferralRewardHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, audit store.AdminAuditRepository, failureLogs ...store.FailureEventLogRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil || identity.UsersRepo() == nil || repo == nil || system == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REFERRAL_UNAVAILABLE", "Referral rewards are temporarily unavailable")
			return
		}
		tenantID, referralID := RequestTenantID(r), strings.TrimSpace(r.PathValue("referral_id"))
		if referralID == "" {
			writeError(w, http.StatusBadRequest, "REFERRAL_REQUIRED", "Referral is required")
			return
		}
		referral, err := repo.GetReferralByID(r.Context(), tenantID, referralID)
		if err != nil || referral == nil {
			writeError(w, http.StatusNotFound, "REFERRAL_NOT_FOUND", "Referral was not found")
			return
		}
		if referral.Status != "reward_failed" && referral.Status != "attributed" {
			writeError(w, http.StatusConflict, "REFERRAL_NOT_RETRYABLE", "Referral reward is not retryable")
			return
		}
		userReferralMu.Lock()
		defer userReferralMu.Unlock()
		// Re-read under the same lock used by registration and recovery. Without
		// this, two administrators can both observe reward_failed and race their
		// registry writes, which risks losing one beneficiary grant in a
		// last-write-wins settings store.
		referral, err = repo.GetReferralByID(r.Context(), tenantID, referralID)
		if err != nil || referral == nil {
			writeError(w, http.StatusNotFound, "REFERRAL_NOT_FOUND", "Referral was not found")
			return
		}
		if referral.Status != "reward_failed" && referral.Status != "attributed" {
			writeError(w, http.StatusConflict, "REFERRAL_NOT_RETRYABLE", "Referral reward is not retryable")
			return
		}
		if err := grantUserReferralRewards(r.Context(), repo, system, identity, referral); err != nil {
			_ = recordUserReferralStatusHistory(r.Context(), repo, tenantID, referral.ID, referral.Status, "reward_failed", "reward retry failed", adminAuditUserID(r))
			recordUserReferralRewardFailure(r.Context(), failureLogs, tenantID, referral.ID)
			recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricRewardFailed)
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_REWARD_FAILED", "Referral reward retry failed")
			return
		}
		_ = recordUserReferralStatusHistory(r.Context(), repo, tenantID, referral.ID, referral.Status, "rewarded", "", adminAuditUserID(r))
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "user_referral.reward_retried", map[string]any{"referral_id": referral.ID})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "rewarded"})
	}
}

type userReferralModerationRequest struct {
	Reason string `json:"reason"`
}

// ModerateUserReferralHandler exposes the audited manual transitions required
// for reserved/refused/revoked referral cases. The repo transition is a CAS so
// two administrators cannot both approve or revoke the same attribution.
func ModerateUserReferralHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, audit store.AdminAuditRepository, failureLogs ...store.FailureEventLogRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil || identity.UsersRepo() == nil || repo == nil || system == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_REFERRAL_UNAVAILABLE", "Referral moderation is temporarily unavailable")
			return
		}
		tenantID, referralID, action := RequestTenantID(r), strings.TrimSpace(r.PathValue("referral_id")), strings.TrimSpace(r.PathValue("action"))
		if referralID == "" {
			writeError(w, http.StatusBadRequest, "REFERRAL_REQUIRED", "Referral is required")
			return
		}
		var req userReferralModerationRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		reason := strings.TrimSpace(req.Reason)
		if len([]rune(reason)) > 500 {
			writeError(w, http.StatusBadRequest, "INVALID_REASON", "Reason is too long")
			return
		}
		if reason == "" {
			writeError(w, http.StatusBadRequest, "REASON_REQUIRED", "A reason is required")
			return
		}
		referral, err := repo.GetReferralByID(r.Context(), tenantID, referralID)
		if err != nil || referral == nil {
			writeError(w, http.StatusNotFound, "REFERRAL_NOT_FOUND", "Referral was not found")
			return
		}
		var from []string
		var to, auditAction string
		switch action {
		case "approve":
			from, to, auditAction = []string{"reserved"}, "attributed", "user_referral.approved"
		case "reject":
			from, to, auditAction = []string{"reserved"}, "rejected", "user_referral.rejected"
		case "revoke":
			from, to, auditAction = []string{"attributed", "rewarded", "reward_failed"}, "revoked", "user_referral.revoked"
		default:
			writeError(w, http.StatusBadRequest, "INVALID_REFERRAL_ACTION", "Unsupported referral action")
			return
		}
		// Freezing is the safety-critical side effect of revocation: once the
		// referral says revoked, no remaining referral Credits may remain
		// billable. Perform it before the durable state transition so a settings
		// write failure cannot leave a terminally revoked row with spendable
		// benefits. The grants are referral-ID scoped and freezing is idempotent.
		userReferralMu.Lock()
		defer userReferralMu.Unlock()
		// The initial read validates the request. Re-read while holding the
		// referral mutation lock so moderation cannot race reward recovery or a
		// manual retry between validation and the status transition.
		referral, err = repo.GetReferralByID(r.Context(), tenantID, referralID)
		if err != nil || referral == nil {
			writeError(w, http.StatusNotFound, "REFERRAL_NOT_FOUND", "Referral was not found")
			return
		}
		if action == "revoke" {
			if err := llmservice.FreezeUserReferralBenefits(r.Context(), ScopedSystemSettingsForTenant(tenantID, system), referral.ID); err != nil {
				writeError(w, http.StatusServiceUnavailable, "REFERRAL_FREEZE_FAILED", "Unable to freeze the referral's remaining Credits")
				return
			}
		}
		changed, err := repo.TransitionReferralStatus(r.Context(), tenantID, referral.ID, from, to, time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "REFERRAL_TRANSITION_FAILED", "Unable to update referral")
			return
		}
		if !changed {
			writeError(w, http.StatusConflict, "REFERRAL_STATE_CONFLICT", "Referral state has already changed")
			return
		}
		if err := recordUserReferralStatusHistory(r.Context(), repo, tenantID, referral.ID, referral.Status, to, reason, adminAuditUserID(r)); err != nil {
			writeError(w, http.StatusInternalServerError, "REFERRAL_HISTORY_FAILED", "Referral was updated but its history could not be recorded")
			return
		}
		if action == "reject" {
			recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricRiskRejected)
		}
		finalStatus := to
		if action == "approve" {
			referral.Status = "attributed"
			if err := grantUserReferralRewards(r.Context(), repo, system, identity, referral); err != nil {
				_ = recordUserReferralStatusHistory(r.Context(), repo, tenantID, referral.ID, "attributed", "reward_failed", "reward processing failed", adminAuditUserID(r))
				recordUserReferralRewardFailure(r.Context(), failureLogs, tenantID, referral.ID)
				recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricRewardFailed)
				writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), auditAction, map[string]any{"referral_id": referral.ID, "reason": reason, "reward_status": "reward_failed"})
				writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "status": "reward_failed"})
				return
			}
			_ = recordUserReferralStatusHistory(r.Context(), repo, tenantID, referral.ID, "attributed", "rewarded", "", adminAuditUserID(r))
			finalStatus = "rewarded"
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), auditAction, map[string]any{"referral_id": referral.ID, "reason": reason})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": finalStatus})
	}
}

type publicReferralRegistrationRequest struct {
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	VerifyCode string `json:"verify_code,omitempty"`
}

type publicReferralAccountCheckRequest struct {
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

type publicReferralEmailSendCodeRequest struct {
	Email string `json:"email"`
}

// PublicUserReferralRegistrationStatusHandler is a session-bound recovery
// endpoint for the browser and desktop registration flows. It intentionally
// returns only workflow state and configured downloads; it never exposes a
// referral code, inviter identity, or raw contact information.
func PublicUserReferralRegistrationStatusHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "registration_status", userReferralVerificationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		if identity == nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "Registration is unavailable")
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		registrationStatus := "continue"
		if token, ok := userReferralRegistrationSessionToken(r); ok {
			session, sessionErr := repo.GetRegistrationSession(r.Context(), tenantID, userReferralRegistrationSessionTokenHash(tenantID, token), time.Now().UTC())
			if sessionErr != nil {
				writeError(w, http.StatusServiceUnavailable, "REFERRAL_SESSION_FAILED", "Registration is unavailable")
				return
			}
			if session != nil && strings.TrimSpace(session.ReferralID) != "" {
				referral, referralErr := repo.GetReferralByID(r.Context(), tenantID, session.ReferralID)
				if referralErr != nil {
					writeError(w, http.StatusServiceUnavailable, "REFERRAL_STATUS_UNAVAILABLE", "Registration status is unavailable")
					return
				}
				if referral != nil && referral.InviteeUserID == session.InviteeUserID {
					registrationStatus = userReferralRegistrationRecoveryStatus(referral.Status)
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"registration_status": registrationStatus, "registration_method": registrationAuthMethodForConfig(r, system, tenantID), "downloads": cfg.Downloads})
	}
}

// userReferralRegistrationRecoveryStatus is deliberately a small public
// vocabulary. It tells a completed invitee whether another registration
// attempt is needed, but does not reveal the inviter, referral code, grant ID
// or any other account information.
func userReferralRegistrationRecoveryStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "reserved":
		return "approval_pending"
	case "attributed":
		return "registered_reward_pending"
	case "rewarded":
		return "registered_rewarded"
	case "reward_failed":
		return "registered_reward_failed"
	case "rejected":
		return "registered_rejected"
	case "revoked":
		return "registered_revoked"
	case "expired":
		return "registered_expired"
	default:
		return "continue"
	}
}

func registrationAuthMethodForConfig(r *http.Request, system store.SystemSettingsRepository, tenantID string) string {
	cfg, err := loadRegistrationAuthConfigForTenant(r, system, tenantID)
	if err != nil {
		return ""
	}
	return cfg.Method
}

// PublicUserReferralAccountCheckHandler verifies a registration session before
// checking the locked tenant identity. It is designed for the invitation UI:
// an existing account receives a clear new-users-only result and the tenant's
// client downloads, but no invitation, inviter or reward data.
func PublicUserReferralAccountCheckHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "account_check", userReferralVerificationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		if identity == nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "Registration is unavailable")
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		var req publicReferralAccountCheckRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email, phone := userReferralNormalizedIdentityValue("email", req.Email), normalizePhoneNumber(req.Phone)
		if (email == "" && phone == "") || (email != "" && phone != "") {
			writeError(w, http.StatusBadRequest, "CONTACT_REQUIRED", "Provide exactly one email or phone number")
			return
		}
		var existing *store.User
		if email != "" {
			if !looksLikeRegistrationContactEmail(email) {
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
				return
			}
			existing, err = identity.LookupUserByEmail(auth.WithTenant(r.Context(), tenantID), email)
		} else {
			if _, phoneErr := phoneRegistrationIdentity(phone); phoneErr != nil {
				writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "Valid phone number is required")
				return
			}
			existing, err = identity.LookupUserByPhone(auth.WithTenant(r.Context(), tenantID), phone)
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "REGISTRATION_CHECK_FAILED", "Registration is unavailable")
			return
		}
		if existing != nil {
			recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricExistingAccount)
			writeJSON(w, http.StatusConflict, map[string]any{"eligible": false, "reason": "existing_user", "message": "This invitation is only for new users", "downloads": cfg.Downloads, "login_hint": "Sign in with your existing account in MaClaw."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"eligible": true})
	}
}

type publicReferralPhoneRegistrationRequest struct {
	Phone      string `json:"phone"`
	VerifyCode string `json:"verify_code"`
}

type publicReferralEmailEnrollRequest struct {
	Email                string `json:"email"`
	MachineName          string `json:"machine_name"`
	Platform             string `json:"platform"`
	ClientID             string `json:"client_id"`
	Hostname             string `json:"hostname"`
	Arch                 string `json:"arch"`
	AppVersion           string `json:"app_version"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
}

// referralResponseRecorder observes the delegated SMS handler without
// buffering or changing its response. A failed SMS-send must release the
// identity reservation so a subsequent valid attempt is not stranded.
type referralResponseRecorder struct {
	http.ResponseWriter
	status int
}

func (w *referralResponseRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *referralResponseRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func referralVerificationKey(codeHash, email string) string {
	return "referral:" + codeHash + ":email:" + userReferralNormalizedIdentityValue("email", email)
}

func referralRecommendedDownload(r *http.Request, downloads UserReferralDownloads) (string, string) {
	ua := ""
	if r != nil {
		ua = strings.ToLower(r.UserAgent())
	}
	isARM := strings.Contains(ua, "arm64") || strings.Contains(ua, "aarch64") || strings.Contains(ua, "apple silicon")
	switch {
	case strings.Contains(ua, "windows"):
		if isARM {
			return "Windows ARM64", downloads.WindowsARM64
		}
		return "Windows x64", downloads.WindowsAMD64
	case strings.Contains(ua, "mac os") || strings.Contains(ua, "macintosh"):
		if isARM {
			return "macOS Apple Silicon", downloads.MacOSARM64
		}
		return "macOS Intel", downloads.MacOSAMD64
	case strings.Contains(ua, "linux"):
		if isARM {
			return "Linux ARM64", downloads.LinuxARM64
		}
		return "Linux x64", downloads.LinuxAMD64
	default:
		return "", ""
	}
}

func writePublicReferralLandingHTML(w http.ResponseWriter, r *http.Request, code string, tenant *store.Tenant, inviter *store.UserReferralCode, cfg UserReferralConfig, method string, available bool) {
	name, slug := "MaClaw Hub", ""
	if tenant != nil {
		if strings.TrimSpace(tenant.Name) != "" {
			name = tenant.Name
		}
		slug = tenant.Slug
	}
	logoURL := tenantReferralLogoURL(tenant)
	imageSource := "'self' data:"
	if logoURL != "" {
		if parsed, err := url.Parse(logoURL); err == nil && parsed.Scheme == "https" && parsed.Host != "" {
			imageSource += " " + parsed.Scheme + "://" + parsed.Host
		}
	}
	brand := `<p class="tenant">` + html.EscapeString(name)
	if slug != "" {
		brand += ` / ` + html.EscapeString(slug)
	}
	brand += `</p>`
	if logoURL != "" {
		brand = `<div class="brand"><img class="brand-logo" src="` + html.EscapeString(logoURL) + `" alt="` + html.EscapeString(name) + ` logo" onerror="this.remove()">` + brand + `</div>`
	}
	download := func(label, value string) string {
		return `<a href="` + html.EscapeString(value) + `" target="_blank" rel="noopener noreferrer">` + html.EscapeString(label) + `</a>`
	}
	recommendedLabel, recommendedURL := referralRecommendedDownload(r, cfg.Downloads)
	recommendedDownload := ""
	if recommendedLabel != "" && recommendedURL != "" {
		recommendedDownload = `<p class="recommend">Recommended for this device: ` + download(recommendedLabel, recommendedURL) + `</p>`
	}
	form := `<p class="notice">Registration is currently unavailable. You can still download the client below.</p>`
	if available && method == registrationAuthMethodMixed {
		form = `<form id="register" data-mixed="1"><label>Registration method<select id="mode"><option value="email">Email</option><option value="phone">Phone</option></select></label><label id="email-wrap">Email<input id="email" type="email" required autocomplete="email"></label><label id="phone-wrap" hidden>Phone<input id="phone" type="tel" autocomplete="tel"></label><div class="verify"><input id="verify" inputmode="numeric" placeholder="Verification code" required><button type="button" id="send">Send code</button></div><button type="submit">Create account</button><p id="message" role="status"></p></form>`
	} else if available && method != registrationAuthMethodPhone {
		form = `<form id="register"><label>Email<input id="email" type="email" required autocomplete="email"></label><div class="verify"><input id="verify" inputmode="numeric" placeholder="Verification code" required><button type="button" id="send">Send code</button></div><button type="submit">Create account</button><p id="message" role="status"></p></form>`
	} else if available {
		form = `<form id="register" data-phone="1"><label>Phone<input id="phone" type="tel" required autocomplete="tel"></label><div class="verify"><input id="verify" inputmode="numeric" placeholder="SMS verification code" required><button type="button" id="send">Send code</button></div><button type="submit">Create account</button><p id="message" role="status"></p></form>`
	}
	handoff := ""
	if available {
		handoff = `<button type="button" class="open-app" id="open-app">Open MaClaw</button><p class="open-app-note" id="open-app-note">Already installed? Continue registration in MaClaw.</p>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	// The landing page deliberately has no third-party analytics or assets: the
	// referral code is part of its URL and must never become a Referer. Inline
	// CSS/JS is limited to this server-rendered document; connections remain
	// same-origin only.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; connect-src 'self'; img-src "+imageSource+"; script-src 'unsafe-inline'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Join %s</title><style>body{margin:0;background:#f6f7f9;color:#262b36;font-family:system-ui,-apple-system,"Segoe UI",sans-serif}.page{max-width:720px;margin:8vh auto;padding:24px}.card{background:#fff;border:1px solid #e5e8ef;border-radius:14px;padding:28px}h1{margin:0 0 8px;font-size:26px}p{line-height:1.55;color:#5f6878}.brand{display:flex;align-items:center;gap:10px;margin-bottom:12px}.brand-logo{width:36px;height:36px;object-fit:contain;border-radius:8px}.tenant{margin:0;color:#a83f59;font-weight:700}.inviter{font-family:ui-monospace,monospace;color:#707684}form{display:grid;gap:12px;margin-top:22px}label{display:grid;gap:6px;font-size:13px;font-weight:650}input,select{box-sizing:border-box;width:100%%;height:42px;padding:0 12px;border:1px solid #dfe3eb;border-radius:10px;font:inherit;background:#fff}.verify{display:grid;grid-template-columns:1fr auto;gap:8px}button{height:42px;padding:0 15px;border:0;border-radius:10px;background:#e55468;color:#fff;font:inherit;font-weight:700;cursor:pointer}button[disabled]{opacity:.55;cursor:not-allowed}.open-app{width:100%%;margin-top:14px;background:#253857}.open-app-note{font-size:13px;margin:7px 0 0}.downloads{display:flex;gap:10px;flex-wrap:wrap;margin-top:22px;padding-top:18px;border-top:1px solid #edf0f4}.downloads a{font-size:13px;color:#9f3651;text-decoration:none}.recommend{margin:18px 0 -8px;padding:10px 12px;background:#fff5f6;border-radius:8px}.notice{padding:12px;background:#fbfcfd;border:1px solid #dfe3eb;border-radius:10px}@media(max-width:560px){.page{margin:0;padding:14px}.card{padding:20px}.verify{grid-template-columns:1fr}}</style></head><body><main class="page"><section class="card">%s<h1>Join this tenant on MaClaw</h1><p>You will register under <strong>%s</strong>. Invited by <span class="inviter">%s</span>.</p><p>Complete registration to receive %s Credits, valid for %d days.</p>%s%s%s<div class="downloads">%s%s%s%s%s%s</div></section></main><script>(function(){var path=location.pathname,form=document.getElementById('register'),email=document.getElementById('email'),phone=document.getElementById('phone'),mode=document.getElementById('mode'),verify=document.getElementById('verify'),message=document.getElementById('message'),send=document.getElementById('send'),openApp=document.getElementById('open-app'),openAppNote=document.getElementById('open-app-note');function isPhone(){return!!(form&&(form.dataset.phone||(form.dataset.mixed&&mode.value==='phone')))}function sync(){if(!form||!form.dataset.mixed)return;var p=isPhone(),ew=document.getElementById('email-wrap'),pw=document.getElementById('phone-wrap');ew.hidden=p;pw.hidden=!p;email.required=!p;phone.required=p;verify.value='';tell('')}if(mode)mode.onchange=sync;sync();function tell(t){if(message)message.textContent=t}function contact(){return isPhone()?{phone:(phone.value||'')}:{email:(email.value||'')}}function downloadNotice(d){var links=d&&d.downloads?d.downloads:null;if(!links)return '';var values=[['Windows x64',links.windows_amd64],['Windows ARM64',links.windows_arm64],['macOS Intel',links.macos_amd64],['macOS Apple Silicon',links.macos_arm64],['Linux x64',links.linux_amd64],['Linux ARM64',links.linux_arm64]],html='';for(var i=0;i<values.length;i++)if(values[i][1])html+='<a href="'+values[i][1]+'" target="_blank" rel="noopener noreferrer">'+values[i][0]+'</a> ';return html?'<div class="downloads">'+html+'</div>':''}function recoveryMessage(s){return s==='registered_rewarded'?'Registration completed. Your invitation reward has been issued.':s==='approval_pending'?'Registration completed. Your invitation reward is awaiting review.':s==='registered_expired'?'Registration completed, but the invitation review window expired before the reward could be issued.':s==='registered_rejected'?'Registration completed, but this invitation is not eligible for a reward.':s==='registered_revoked'?'Registration completed, but this invitation reward has been revoked.':s==='registered_reward_failed'?'Registration completed. Your invitation reward is still being processed.':'Registration completed. Your invitation reward is being processed.'}async function restore(){if(!form)return;try{var r=await fetch(path+'/registration/status'),d=await r.json();if(!r.ok||d.registration_status==='continue')return;form.innerHTML='<p class="notice">'+recoveryMessage(d.registration_status)+' Download MaClaw using a link below.</p>'}catch(e){}}void restore();async function checkAccount(){var r=await fetch(path+'/registration/account-check',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(contact())}),d=await r.json();if(r.status===409&&d.reason==='existing_user'){form.innerHTML='<p class="notice">This invitation is only for new users. Sign in with your existing MaClaw account, or download the client below.</p>'+downloadNotice(d);return false}if(!r.ok)throw new Error(d.message||'Could not verify this account');return true}if(openApp)openApp.onclick=async function(){openApp.disabled=true;try{var r=await fetch(path+'/handoff',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'}),d=await r.json();if(!r.ok)throw new Error(d.message||'Could not open MaClaw');location.href='maclaw://onboarding?referral_handoff='+encodeURIComponent(d.handoff);if(openAppNote)openAppNote.textContent='MaClaw should open shortly. You can continue on this page if it does not.'}catch(e){if(openAppNote)openAppNote.textContent=e.message||'Could not open MaClaw. Continue registration in this browser.'}finally{openApp.disabled=false}};if(send)send.onclick=async function(){send.disabled=true;try{if(!await checkAccount())return;var p=isPhone()?'/phone/send-code':'/email/send-code',body=contact(),r=await fetch(path+p,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}),d=await r.json();if(!r.ok)throw new Error(d.message||'Could not send code');tell(isPhone()?'Code sent by SMS.':'Code sent. Check your email.')}catch(e){tell(e.message)}finally{send.disabled=false}};if(form)form.onsubmit=async function(e){e.preventDefault();var b=form.querySelector('button[type=submit]');b.disabled=true;try{if(!await checkAccount())return;var p=isPhone()?'/phone/register':'/register',body=contact();body.verify_code=verify.value;var r=await fetch(path+p,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}),d=await r.json();if(!r.ok)throw new Error(d.message||'Registration failed');var status=d.reward_status==='reserved'?'Your invitation reward is awaiting review.':d.reward_status==='reward_failed'?'Your invitation reward is pending and will be processed shortly.':'Your invitation Credits have been applied.';form.innerHTML='<p class="notice">Registration completed. '+status+' Download MaClaw using a link below.</p>'}catch(e){tell(e.message)}finally{b.disabled=false}}})();</script></body></html>`, html.EscapeString(name), brand, html.EscapeString(name), html.EscapeString(maskReferralID(inviter.InviterUserID)), formatReferralCredits(cfg.InviteeCredits), cfg.DurationDays, form, handoff, recommendedDownload, download("Windows x64", cfg.Downloads.WindowsAMD64), download("Windows ARM64", cfg.Downloads.WindowsARM64), download("macOS Intel", cfg.Downloads.MacOSAMD64), download("macOS Apple Silicon", cfg.Downloads.MacOSARM64), download("Linux x64", cfg.Downloads.LinuxAMD64), download("Linux ARM64", cfg.Downloads.LinuxARM64))
}

func tenantReferralLogoURL(tenant *store.Tenant) string {
	if tenant == nil {
		return ""
	}
	settings := map[string]any{}
	if json.Unmarshal([]byte(tenant.SettingsJSON), &settings) != nil {
		return ""
	}
	value, _ := settings["logo_url"].(string)
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\r\n\t") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return value
}

func formatReferralCredits(credits float64) string { return fmt.Sprintf("%.0f", credits) }

// referralTenantAllowsRegistration mirrors the public tenant policy in the
// landing handler. IdentityService enforces it again when it owns a tenant
// repository; keeping this check local also prevents a form from being shown
// on minimal/legacy deployments where IdentityService was constructed before
// tenant repositories were introduced.
func referralTenantAllowsRegistration(tenant *store.Tenant) bool {
	if tenant == nil || strings.TrimSpace(tenant.SettingsJSON) == "" {
		return true
	}
	settings := map[string]any{}
	if json.Unmarshal([]byte(tenant.SettingsJSON), &settings) != nil {
		return true
	}
	for _, key := range []string{"allow_user_registration", "registration_enabled"} {
		if value, ok := settings[key].(bool); ok {
			return value
		}
	}
	return true
}

func PublicUserReferralLandingHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "landing", userReferralLandingLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		if identity == nil {
			writeError(w, 503, "IDENTITY_UNAVAILABLE", "Registration is unavailable")
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, referralCode, cfg, inviter, err := resolvePublicReferral(r.Context(), code, repo, system, tenants, identity.UsersRepo())
		if err != nil {
			writeError(w, 404, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		_ = referralCode
		recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricLanding)
		tenant, _ := tenants.GetByID(r.Context(), tenantID)
		authCfg, _ := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		registrationAllowed := referralTenantAllowsRegistration(tenant)
		if identityAllowed, allowedErr := identity.TenantAllowsNewUserRegistration(auth.WithTenant(r.Context(), tenantID)); allowedErr == nil {
			registrationAllowed = registrationAllowed && identityAllowed
		}
		available := cfg.Enabled && registrationAllowed
		if available {
			// A refresh after successful registration must retain the original
			// session so the landing page can restore its completed state. Mint a
			// new session only when there is no valid session for this exact
			// tenant, invitation and configuration epoch.
			reuseSession := false
			if token, ok := userReferralRegistrationSessionToken(r); ok {
				session, sessionErr := repo.GetRegistrationSession(r.Context(), tenantID, userReferralRegistrationSessionTokenHash(tenantID, token), time.Now().UTC())
				reuseSession = sessionErr == nil && session != nil && session.TenantID == store.NormalizeTenantID(tenantID) && session.CodeHash == inviter.CodeHash && session.ConfigEpoch == cfg.SessionEpoch && session.UserAgentHash == referralUserAgentHash(r)
			}
			if !reuseSession {
				if err := newUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviter.CodeHash, cfg.SessionEpoch); err != nil {
					writeError(w, http.StatusServiceUnavailable, "REFERRAL_SESSION_FAILED", "Registration is unavailable")
					return
				}
			}
		}
		tenantName, tenantSlug := "MaClaw Hub", ""
		if tenant != nil {
			tenantName, tenantSlug = tenant.Name, tenant.Slug
		}
		payload := map[string]any{"available": available, "tenant": map[string]any{"name": tenantName, "slug": tenantSlug, "logo_url": tenantReferralLogoURL(tenant)}, "inviter": map[string]any{"masked_id": maskReferralID(inviter.InviterUserID)}, "registration_method": authCfg.Method, "downloads": cfg.Downloads, "duration_days": cfg.DurationDays, "invitee_credits": cfg.InviteeCredits}
		if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") {
			writeJSON(w, 200, payload)
			return
		}
		writePublicReferralLandingHTML(w, r, code, tenant, inviter, cfg, authCfg.Method, available)
	}
}

// PublicUserReferralHandoffHandler mints a single-use opaque desktop handoff
// only for a valid browser registration session. It deliberately does not
// expose the referral code, inviter contact, or internal tenant identifiers
// beyond the existing tenant-scoped browser session.
func PublicUserReferralHandoffHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "handoff", userReferralVerificationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		if identity == nil {
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "Registration is unavailable")
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferral(r.Context(), code, repo, system, tenants, identity.UsersRepo())
		if err != nil || !cfg.Enabled || !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			if err != nil {
				clearUserReferralRegistrationSessionCookie(w, r)
				writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			}
			return
		}
		if tenant, tenantErr := tenants.GetByID(r.Context(), tenantID); tenantErr != nil || !referralTenantAllowsRegistration(tenant) {
			writeError(w, http.StatusForbidden, "REGISTRATION_UNAVAILABLE", "Registration is unavailable")
			return
		}
		if allowed, allowedErr := identity.TenantAllowsNewUserRegistration(auth.WithTenant(r.Context(), tenantID)); allowedErr != nil || !allowed {
			writeError(w, http.StatusForbidden, "REGISTRATION_UNAVAILABLE", "Registration is unavailable")
			return
		}
		raw, err := newUserReferralCode()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_HANDOFF_UNAVAILABLE", "Unable to open MaClaw right now")
			return
		}
		now := time.Now().UTC()
		handoff := &store.UserReferralHandoff{TokenHash: userReferralHandoffTokenHash(strings.TrimPrefix(raw, "rf_")), TenantID: tenantID, CodeHash: inviterCode.CodeHash, ReferralCodeID: inviterCode.ID, InviterUserID: inviterCode.InviterUserID, ConfigEpoch: cfg.SessionEpoch, ServiceGroupID: cfg.ServiceGroupID, InviterCredits: cfg.InviterCredits, InviteeCredits: cfg.InviteeCredits, DurationDays: cfg.DurationDays, ExpiresAt: now.Add(userReferralHandoffTTL), CreatedAt: now}
		if err := repo.CreateHandoff(r.Context(), handoff); err != nil {
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_HANDOFF_UNAVAILABLE", "Unable to open MaClaw right now")
			return
		}
		base := userReferralPublicBaseURL(r)
		// `handoff` remains a single deep-link value. Its random first component
		// is the only value accepted by the claim endpoint; the URL component is
		// merely the issuer location needed to reach that endpoint.
		writeJSON(w, http.StatusCreated, map[string]any{"handoff": strings.TrimPrefix(raw, "rf_") + "?hub_url=" + url.QueryEscape(base), "expires_in_seconds": int(userReferralHandoffTTL.Seconds())})
	}
}

type publicUserReferralHandoffClaimRequest struct {
	Handoff string `json:"handoff"`
}

// PublicUserReferralHandoffClaimHandler turns a browser-issued one-time
// handoff into a UA-bound short-lived registration session. The returned
// session is opaque and can only be presented by the desktop client to the
// follow-up referral registration endpoints; the invitation code is never
// returned to the desktop or embedded in an installer URL.
func PublicUserReferralHandoffClaimHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "handoff_claim", userReferralVerificationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		var req publicUserReferralHandoffClaimRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		token := strings.TrimSpace(req.Handoff)
		if token == "" || len(token) > 256 || strings.ContainsAny(token, "\r\n\t ") {
			writeError(w, http.StatusBadRequest, "INVALID_REFERRAL_HANDOFF", "This MaClaw handoff is invalid or expired")
			return
		}
		now := time.Now().UTC()
		handoff, err := repo.GetHandoff(r.Context(), userReferralHandoffTokenHash(token), now)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_HANDOFF_UNAVAILABLE", "Unable to open registration right now")
			return
		}
		if handoff == nil || identity == nil || tenants == nil {
			writeError(w, http.StatusGone, "REFERRAL_HANDOFF_EXPIRED", "This MaClaw handoff is invalid or expired")
			return
		}
		cfg, cfgErr := loadUserReferralConfig(r.Context(), system, handoff.TenantID)
		tenant, tenantErr := tenants.GetByID(r.Context(), handoff.TenantID)
		allowed, allowedErr := identity.TenantAllowsNewUserRegistration(auth.WithTenant(r.Context(), handoff.TenantID))
		if cfgErr != nil || tenantErr != nil || !cfg.Enabled || cfg.SessionEpoch != handoff.ConfigEpoch || !referralTenantAllowsRegistration(tenant) || allowedErr != nil || !allowed {
			writeError(w, http.StatusGone, "REFERRAL_HANDOFF_EXPIRED", "This MaClaw handoff is no longer available")
			return
		}
		consumed, err := repo.ConsumeHandoff(r.Context(), handoff.TokenHash, now)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_HANDOFF_UNAVAILABLE", "Unable to open registration right now")
			return
		}
		if !consumed {
			writeError(w, http.StatusGone, "REFERRAL_HANDOFF_EXPIRED", "This MaClaw handoff is invalid or expired")
			return
		}
		// The token is now irreversibly consumed. A failed session write cannot
		// be retried with the same browser handoff, but is the safer outcome than
		// handing one token to two desktops. Persist the desktop session only for
		// the winning claimant.
		session, err := newUserReferralRegistrationSessionToken(r.Context(), repo, r, handoff.TenantID, handoff.CodeHash, handoff.ConfigEpoch)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_HANDOFF_UNAVAILABLE", "Unable to open registration right now")
			return
		}
		authCfg, _ := loadRegistrationAuthConfigForTenant(r, system, handoff.TenantID)
		writeJSON(w, http.StatusOK, map[string]any{"registration_session": session, "expires_in_seconds": int(userReferralRegistrationSessionTTL.Seconds()), "tenant": map[string]any{"id": handoff.TenantID, "name": tenant.Name, "slug": tenant.Slug}, "registration_method": authCfg.Method, "invitee_credits": handoff.InviteeCredits, "duration_days": handoff.DurationDays})
	}
}

// PublicUserReferralDesktopRegistrationStatusHandler is the desktop variant of
// the registration recovery endpoint. Unlike the browser endpoint it has no
// invitation URL; resolvePublicReferralRequest binds the opaque session to its
// tenant and referral before any response can be returned.
func PublicUserReferralDesktopRegistrationStatusHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "desktop_registration_status", userReferralVerificationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		if identity == nil {
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "Registration is unavailable")
			return
		}
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, "", repo, system, tenants, identity.UsersRepo())
		if err != nil || !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			if err != nil {
				writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			}
			return
		}
		registrationStatus := "continue"
		if token, ok := userReferralRegistrationSessionToken(r); ok {
			session, sessionErr := repo.GetRegistrationSession(r.Context(), tenantID, userReferralRegistrationSessionTokenHash(tenantID, token), time.Now().UTC())
			if sessionErr != nil {
				writeError(w, http.StatusServiceUnavailable, "REFERRAL_SESSION_FAILED", "Registration is unavailable")
				return
			}
			if session != nil && strings.TrimSpace(session.ReferralID) != "" {
				referral, referralErr := repo.GetReferralByID(r.Context(), tenantID, session.ReferralID)
				if referralErr != nil {
					writeError(w, http.StatusServiceUnavailable, "REFERRAL_STATUS_UNAVAILABLE", "Registration status is unavailable")
					return
				}
				if referral != nil && referral.InviteeUserID == session.InviteeUserID {
					registrationStatus = userReferralRegistrationRecoveryStatus(referral.Status)
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"registration_status": registrationStatus, "registration_method": registrationAuthMethodForConfig(r, system, tenantID), "downloads": cfg.Downloads})
	}
}

func userReferralPublicBaseURL(r *http.Request) string {
	if r == nil || strings.TrimSpace(r.Host) == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
func PublicUserReferralEmailSendCodeHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository, mailer *mail.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Do not dereference identity until after its availability has been
		// checked. This public endpoint must return a normal service error while
		// Hub is starting or a dependency is intentionally disabled.
		if identity == nil || mailer == nil {
			writeError(w, http.StatusServiceUnavailable, "REGISTRATION_UNAVAILABLE", "Registration is unavailable")
			return
		}
		if !allowUserReferralPublicRequest(r, "verification", userReferralVerificationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		var req publicReferralEmailSendCodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, 400, "INVALID_JSON", "Invalid request body")
			return
		}
		email := userReferralNormalizedIdentityValue("email", req.Email)
		if !looksLikeRegistrationContactEmail(email) {
			writeError(w, 400, "INVALID_EMAIL", "Valid email is required")
			return
		}
		if existing, lookupErr := identity.UsersRepo().GetByTenantIdentity(auth.WithTenant(r.Context(), tenantID), tenantID, "email", email); lookupErr != nil {
			writeError(w, http.StatusInternalServerError, "REGISTRATION_CHECK_FAILED", "Registration is unavailable")
			return
		} else if existing != nil {
			writeError(w, http.StatusConflict, "ALREADY_REGISTERED", "This invitation is only for new users")
			return
		}
		authCfg, configErr := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		registrationAllowed, allowedErr := identity.TenantAllowsNewUserRegistration(auth.WithTenant(r.Context(), tenantID))
		if configErr != nil || allowedErr != nil || !registrationAllowed || authCfg.Method == registrationAuthMethodPhone || !cfg.Enabled {
			writeError(w, 403, "REGISTRATION_UNAVAILABLE", "Registration is unavailable")
			return
		}
		if err := identity.ValidateEmailEnrollment(auth.WithTenant(r.Context(), tenantID), email); err != nil && !errors.Is(err, auth.ErrUserAlreadyRegistered) {
			writeReferralRegistrationError(w, err)
			return
		}
		identityHash, reserved, reserveErr := reserveUserReferralIdentity(r.Context(), repo, r, tenantID, inviterCode.CodeHash, "email", email)
		if reserveErr != nil {
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_RESERVATION_FAILED", "Registration is unavailable")
			return
		}
		if !reserved {
			writeUserReferralIdentityReserved(w)
			return
		}
		verifyCode, err := generateVerifyCode()
		if err != nil {
			releaseUserReferralIdentityReservation(r.Context(), repo, r, tenantID, identityHash)
			writeError(w, 500, "CODE_GEN_FAILED", "Failed to generate verification code")
			return
		}
		key := referralVerificationKey(inviterCode.CodeHash, email)
		previous := snapshotVerifyCode(tenantID, key)
		if !storeVerifyCode(tenantID, key, verifyCode) {
			releaseUserReferralIdentityReservation(r.Context(), repo, r, tenantID, identityHash)
			writeError(w, 429, "RATE_LIMITED", "Please wait 60 seconds before requesting a new code")
			return
		}
		body := fmt.Sprintf("Your MaClaw invitation verification code is: %s\n\nThis code expires in %d minutes.", verifyCode, int(verifyCodeTTL.Minutes()))
		if err := mailer.Send(auth.WithTenant(r.Context(), tenantID), []string{email}, "MaClaw invitation verification code", body); err != nil {
			_ = rollbackVerifyCode(tenantID, key, verifyCode, previous)
			releaseUserReferralIdentityReservation(r.Context(), repo, r, tenantID, identityHash)
			status, deliveryCode := registrationEmailDeliveryError(err)
			writeError(w, status, deliveryCode, "Mail delivery is unavailable")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "expires_min": int(verifyCodeTTL.Minutes()), "resend_cooldown_seconds": int(verifyCooldown.Seconds())})
	}
}

// PublicUserReferralPhoneSendCodeHandler deliberately resolves the tenant from
// the referral path and then delegates SMS delivery to the existing onboarding
// implementation. The client-provided tenant ID is discarded.
func PublicUserReferralPhoneSendCodeHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "verification", userReferralVerificationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil || identity == nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, 404, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		var req publicReferralPhoneRegistrationRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, 400, "INVALID_JSON", "Invalid request body")
			return
		}
		phone := normalizePhoneNumber(req.Phone)
		canonicalPhone := userReferralNormalizedIdentityValue("phone", req.Phone)
		if _, err := phoneRegistrationIdentity(phone); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "Valid phone number is required")
			return
		}
		if existing, lookupErr := identity.UsersRepo().GetByTenantIdentity(auth.WithTenant(r.Context(), tenantID), tenantID, "phone", phone); lookupErr != nil {
			writeError(w, http.StatusInternalServerError, "REGISTRATION_CHECK_FAILED", "Registration is unavailable")
			return
		} else if existing != nil {
			writeError(w, http.StatusConflict, "ALREADY_REGISTERED", "This invitation is only for new users")
			return
		}
		if !cfg.Enabled {
			writeError(w, http.StatusForbidden, "REGISTRATION_UNAVAILABLE", "Registration is unavailable")
			return
		}
		if _, reserved, reserveErr := reserveUserReferralIdentity(r.Context(), repo, r, tenantID, inviterCode.CodeHash, "phone", canonicalPhone); reserveErr != nil {
			writeError(w, http.StatusServiceUnavailable, "REFERRAL_RESERVATION_FAILED", "Registration is unavailable")
			return
		} else if !reserved {
			writeUserReferralIdentityReserved(w)
			return
		}
		identityHash := userReferralIdentityHash(tenantID, "phone", canonicalPhone)
		payload, _ := json.Marshal(RegistrationSMSSendCodeRequest{PhoneNumber: req.Phone, TenantID: tenantID})
		next := r.Clone(r.Context())
		next.Body = io.NopCloser(strings.NewReader(string(payload)))
		next.ContentLength = int64(len(payload))
		next.Header = r.Header.Clone()
		next.Header.Set("Content-Type", "application/json")
		smsRecorder := &referralResponseRecorder{ResponseWriter: w}
		RegistrationSMSSendCodeHandler(identity, system, nil)(smsRecorder, next)
		if smsRecorder.status >= http.StatusBadRequest {
			releaseUserReferralIdentityReservation(r.Context(), repo, r, tenantID, identityHash)
		}
	}
}

func PublicUserReferralPhoneRegisterHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository, failureLogs ...store.FailureEventLogRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "registration", userReferralRegistrationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil || identity == nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, 404, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		var req publicReferralPhoneRegistrationRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, 400, "INVALID_JSON", "Invalid request body")
			return
		}
		phone := normalizePhoneNumber(req.Phone)
		canonicalPhone := userReferralNormalizedIdentityValue("phone", req.Phone)
		idempotencyFingerprint := referralRequestFingerprint(referralRequestReference(r, code, tenantID), "", canonicalPhone, req.VerifyCode)
		idempotencyKey, replay, idempotencyErr := beginPersistedUserReferralIdempotency(r.Context(), repo, tenantID, r, idempotencyFingerprint)
		if idempotencyErr != nil {
			writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT", idempotencyErr.Error())
			return
		}
		if replayUserReferralIdempotency(w, replay) {
			return
		}
		authCfg, configErr := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		if configErr != nil || (authCfg.Method != registrationAuthMethodPhone && authCfg.Method != registrationAuthMethodMixed) {
			writeError(w, 400, "PHONE_REGISTRATION_DISABLED", "Phone registration is not enabled")
			return
		}
		phone = normalizePhoneNumber(req.Phone)
		if _, err := phoneRegistrationIdentity(phone); err != nil {
			writeError(w, 400, "INVALID_PHONE_NUMBER", "Valid phone number is required")
			return
		}
		if !verifyUserReferralIdentityReservationForValue(r.Context(), repo, r, tenantID, inviterCode.CodeHash, "phone", canonicalPhone) {
			writeUserReferralIdentityReserved(w)
			return
		}
		checkReq, err := buildAliyunSMSVerifyCodeCheckRequest(phone, req.VerifyCode, authCfg.CodeLength)
		if err != nil {
			writeError(w, 400, "INVALID_SMS_VERIFY_REQUEST", err.Error())
			return
		}
		verified, err := aliyunDypnsProviderForRegistration(authCfg).CheckVerifyCode(r.Context(), checkReq)
		if err != nil {
			writeError(w, 502, "SMS_VERIFY_CHECK_FAILED", "Phone verification is unavailable")
			return
		}
		if !verified {
			writeError(w, 400, "INVALID_SMS_VERIFY_CODE", "Invalid SMS verification code")
			return
		}
		recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricVerificationSucceeded)
		user, referral, err := registerUserReferralWithAttribution(r.Context(), r, identity, repo, tenantID, cfg, inviterCode, "", phone, true)
		if err != nil {
			writeReferralRegistrationError(w, err)
			return
		}
		writeReferralRegistrationResult(w, r, repo, system, identity, tenantID, cfg, inviterCode, user, referral, idempotencyKey, idempotencyFingerprint, failureLogs...)
	}
}

// PublicUserReferralPhoneEnrollHandler binds a desktop after the phone user
// has been created and credited by the preceding referral registration. It is
// deliberately separate from verification: the SMS code is one-time and must
// never be reused merely to create a machine.
func PublicUserReferralPhoneEnrollHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil || identity == nil {
			writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		var req RegistrationSMSVerifyAndStartRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		phone := normalizePhoneNumber(req.PhoneNumber)
		canonicalPhone := userReferralNormalizedIdentityValue("phone", req.PhoneNumber)
		if _, err := phoneRegistrationIdentity(phone); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "Valid phone number is required")
			return
		}
		user, err := identity.LookupUserByPhone(auth.WithTenant(r.Context(), tenantID), phone)
		if err != nil || user == nil {
			writeError(w, http.StatusConflict, "REFERRAL_REGISTRATION_REQUIRED", "Complete invitation registration first")
			return
		}
		if !verifyUserReferralIdentityReservationForValue(r.Context(), repo, r, tenantID, inviterCode.CodeHash, "phone", canonicalPhone) {
			writeError(w, http.StatusForbidden, "REFERRAL_ENROLLMENT_UNAVAILABLE", "Invitation registration is unavailable")
			return
		}
		referral, err := repo.GetReferralForInvitee(r.Context(), tenantID, user.ID)
		if err != nil || referral == nil || referral.ReferralCodeID != inviterCode.ID || strings.EqualFold(referral.Status, "revoked") || strings.EqualFold(referral.Status, "rejected") {
			writeError(w, http.StatusForbidden, "REFERRAL_ENROLLMENT_UNAVAILABLE", "Invitation registration is unavailable")
			return
		}
		result, err := identity.StartReferralPhoneEnrollment(auth.WithTenant(r.Context(), tenantID), phone, req.MachineName, req.Platform, req.ClientID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ENROLL_FAILED", "Desktop enrollment is unavailable")
			return
		}
		payload := enrollmentStartResponseMap(result)
		payload["phone_number"] = phone
		writeJSON(w, http.StatusOK, payload)
	}
}

// PublicUserReferralEmailEnrollHandler binds a desktop only after the email
// invitation registration has completed. The opaque referral session locks the
// caller to the tenant and referral code, so this endpoint cannot be used as a
// general existing-account enrollment route.
func PublicUserReferralEmailEnrollHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil {
			writeError(w, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "Desktop enrollment is unavailable")
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, _, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, http.StatusNotFound, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		var req publicReferralEmailEnrollRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := userReferralNormalizedIdentityValue("email", req.Email)
		if !looksLikeRegistrationContactEmail(email) {
			writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
			return
		}
		user, err := identity.LookupUserByEmail(auth.WithTenant(r.Context(), tenantID), email)
		if err != nil || user == nil {
			writeError(w, http.StatusConflict, "REFERRAL_REGISTRATION_REQUIRED", "Complete invitation registration first")
			return
		}
		if !verifyUserReferralIdentityReservation(r.Context(), repo, r, tenantID, inviterCode.CodeHash, userReferralIdentityHash(tenantID, "email", email)) {
			writeError(w, http.StatusForbidden, "REFERRAL_ENROLLMENT_UNAVAILABLE", "Invitation registration is unavailable")
			return
		}
		referral, err := repo.GetReferralForInvitee(r.Context(), tenantID, user.ID)
		if err != nil || referral == nil || referral.ReferralCodeID != inviterCode.ID || strings.EqualFold(referral.Status, "revoked") || strings.EqualFold(referral.Status, "rejected") {
			writeError(w, http.StatusForbidden, "REFERRAL_ENROLLMENT_UNAVAILABLE", "Invitation registration is unavailable")
			return
		}
		result, err := identity.StartEnrollment(auth.WithTenant(r.Context(), tenantID), email, req.MachineName, req.Platform, req.ClientID, "", auth.WithEmailVerifiedRegistration())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ENROLL_FAILED", "Desktop enrollment is unavailable")
			return
		}
		payload := enrollmentStartResponseMap(result)
		payload["email"] = email
		writeJSON(w, http.StatusOK, payload)
	}
}

func PublicUserReferralRegisterHandler(identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository, failureLogs ...store.FailureEventLogRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowUserReferralPublicRequest(r, "registration", userReferralRegistrationLimit) {
			rejectUserReferralRateLimit(w)
			return
		}
		code := strings.TrimSpace(r.PathValue("code"))
		tenantID, referralCode, cfg, inviterCode, err := resolvePublicReferralRequest(r.Context(), r, code, repo, system, tenants, identity.UsersRepo())
		if err != nil {
			clearUserReferralRegistrationSessionCookie(w, r)
			writeError(w, 404, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
			return
		}
		_ = referralCode
		if !verifyUserReferralRegistrationSession(r.Context(), repo, w, r, tenantID, inviterCode.CodeHash, cfg.SessionEpoch) {
			return
		}
		var req publicReferralRegistrationRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			writeError(w, 400, "INVALID_JSON", "Invalid request body")
			return
		}
		idempotencyFingerprint := referralRequestFingerprint(referralRequestReference(r, code, tenantID), req.Email, req.Phone, req.VerifyCode)
		idempotencyKey, replay, idempotencyErr := beginPersistedUserReferralIdempotency(r.Context(), repo, tenantID, r, idempotencyFingerprint)
		if idempotencyErr != nil {
			writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT", idempotencyErr.Error())
			return
		}
		if replayUserReferralIdempotency(w, replay) {
			return
		}
		email := userReferralNormalizedIdentityValue("email", req.Email)
		phone := normalizePhoneNumber(req.Phone)
		authCfg, configErr := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		if configErr != nil {
			writeError(w, 500, "REGISTRATION_AUTH_LOAD_FAILED", "Registration is unavailable")
			return
		}
		if authCfg.Method == registrationAuthMethodPhone || (authCfg.Method == registrationAuthMethodMixed && email == "") || phone != "" {
			writeError(w, 400, "PHONE_REGISTRATION_REQUIRES_SMS", "Phone registration requires SMS verification")
			return
		}
		if email == "" || strings.TrimSpace(req.VerifyCode) == "" {
			writeError(w, 400, "VERIFY_CODE_REQUIRED", "Email and verification code are required")
			return
		}
		if !verifyUserReferralIdentityReservation(r.Context(), repo, r, tenantID, inviterCode.CodeHash, userReferralIdentityHash(tenantID, "email", email)) {
			writeUserReferralIdentityReserved(w)
			return
		}
		valid, locked := consumeVerifyCode(tenantID, referralVerificationKey(inviterCode.CodeHash, email), strings.TrimSpace(req.VerifyCode))
		if !valid {
			if locked {
				writeError(w, 429, "VERIFY_LOCKED", "Too many attempts. Please request a new code")
			} else {
				writeError(w, 400, "INVALID_VERIFY_CODE", "Invalid or expired verification code")
			}
			return
		}
		recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricVerificationSucceeded)
		user, referral, err := registerUserReferralWithAttribution(r.Context(), r, identity, repo, tenantID, cfg, inviterCode, email, phone, phone != "")
		if err != nil {
			writeReferralRegistrationError(w, err)
			return
		}
		writeReferralRegistrationResult(w, r, repo, system, identity, tenantID, cfg, inviterCode, user, referral, idempotencyKey, idempotencyFingerprint, failureLogs...)
	}
}

// registerUserReferralWithAttribution computes the reward-state snapshot and
// then uses the SQLite atomic registration capability to commit the user and
// referral row together. It deliberately does not write Credits: the registry
// belongs to a separate durable state machine and is replayed from attributed
// if it is temporarily unavailable.
func registerUserReferralWithAttribution(ctx context.Context, r *http.Request, identity *auth.IdentityService, repo store.UserReferralRepository, tenantID string, cfg UserReferralConfig, inviterCode *store.UserReferralCode, email, phone string, phoneVerified bool) (*store.User, *store.UserReferral, error) {
	if identity == nil || repo == nil || inviterCode == nil {
		return nil, nil, errors.New("referral registration is unavailable")
	}
	userReferralMu.Lock()
	defer userReferralMu.Unlock()
	now := time.Now().UTC()
	status := "attributed"
	needsNetworkClientReview := userReferralNeedsNetworkClientReview(tenantID, r, cfg.DailyNetworkClientReviewCap, now)
	if cfg.DailyRewardCap > 0 {
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		rewarded, err := repo.CountInviterRewardedOnOrAfter(ctx, tenantID, inviterCode.InviterUserID, dayStart)
		if err != nil {
			return nil, nil, err
		}
		if rewarded >= cfg.DailyRewardCap {
			status = "reserved"
		}
	}
	if needsNetworkClientReview {
		status = "reserved"
	}
	referral := &store.UserReferral{ID: llmservice.NewID("referral"), TenantID: tenantID, ReferralCodeID: inviterCode.ID, InviterUserID: inviterCode.InviterUserID, Status: status, RegisteredAt: now, ServiceGroupID: cfg.ServiceGroupID, InviterCredits: cfg.InviterCredits, InviteeCredits: cfg.InviteeCredits, DurationDays: cfg.DurationDays, CreatedAt: now, UpdatedAt: now}
	user, err := identity.RegisterReferralUserWithAttribution(auth.WithTenant(ctx, tenantID), email, phone, phoneVerified, referral)
	if err != nil {
		return user, nil, err
	}
	reason := "registration completed"
	if status == "reserved" && needsNetworkClientReview {
		reason = "network/client daily registration review threshold reached; awaiting review"
	} else if status == "reserved" {
		reason = "inviter daily reward cap reached; awaiting review"
	}
	recordUserReferralMetric(ctx, repo, tenantID, userReferralMetricAttributionSucceeded)
	_ = recordUserReferralStatusHistory(ctx, repo, tenantID, referral.ID, "", status, reason, "system")
	return user, referral, nil
}

func writeReferralRegistrationResult(w http.ResponseWriter, r *http.Request, repo store.UserReferralRepository, system store.SystemSettingsRepository, identity *auth.IdentityService, tenantID string, cfg UserReferralConfig, inviterCode *store.UserReferralCode, user *store.User, initialReferral *store.UserReferral, idempotencyKey, idempotencyFingerprint string, failureLogs ...store.FailureEventLogRepository) {
	if user == nil {
		writeError(w, 500, "REGISTRATION_FAILED", "Registration failed")
		return
	}
	if user.ID == inviterCode.InviterUserID {
		writeError(w, 400, "SELF_REFERRAL", "You cannot use your own invitation")
		return
	}
	// Keep the short-lived reservation through the optional first-device
	// enrollment. It binds that enrollment to the verified identity which just
	// registered; it expires with the registration session and never becomes a
	// durable profile or login credential.
	userReferralMu.Lock()
	existing, getErr := repo.GetReferralForInvitee(r.Context(), tenantID, user.ID)
	rewardStatus := "rewarded"
	createdReferralID := ""
	if initialReferral != nil {
		// The account and attribution were already committed atomically by the
		// registration helper. Re-read it to guard against stale/cross-tenant
		// caller data before advancing the reward state.
		existing = initialReferral
		createdReferralID = initialReferral.ID
		if initialReferral.Status == "reserved" {
			rewardStatus = "reserved"
		}
	}
	if getErr == nil && existing == nil {
		now := time.Now().UTC()
		status := "attributed"
		needsNetworkClientReview := userReferralNeedsNetworkClientReview(tenantID, r, cfg.DailyNetworkClientReviewCap, now)
		if cfg.DailyRewardCap > 0 {
			dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			if rewarded, countErr := repo.CountInviterRewardedOnOrAfter(r.Context(), tenantID, inviterCode.InviterUserID, dayStart); countErr != nil {
				getErr = countErr
			} else if rewarded >= cfg.DailyRewardCap {
				status = "reserved"
				rewardStatus = "reserved"
			}
		}
		if needsNetworkClientReview {
			status = "reserved"
			rewardStatus = "reserved"
		}
		referral := &store.UserReferral{ID: llmservice.NewID("referral"), TenantID: tenantID, ReferralCodeID: inviterCode.ID, InviterUserID: inviterCode.InviterUserID, InviteeUserID: user.ID, Status: status, RegisteredAt: now, ServiceGroupID: cfg.ServiceGroupID, InviterCredits: cfg.InviterCredits, InviteeCredits: cfg.InviteeCredits, DurationDays: cfg.DurationDays, CreatedAt: now, UpdatedAt: now}
		if getErr == nil {
			getErr = repo.CreateReferral(r.Context(), referral)
		}
		if getErr == nil {
			createdReferralID = referral.ID
			recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricAttributionSucceeded)
			reason := "registration completed"
			if status == "reserved" && needsNetworkClientReview {
				reason = "network/client daily registration review threshold reached; awaiting review"
			} else if status == "reserved" {
				reason = "inviter daily reward cap reached; awaiting review"
			}
			_ = recordUserReferralStatusHistory(r.Context(), repo, tenantID, createdReferralID, "", status, reason, "system")
		}
		existing = referral
	}
	if getErr == nil && existing != nil && existing.Status == "attributed" {
		getErr = grantUserReferralRewards(r.Context(), repo, system, identity, existing)
	}
	userReferralMu.Unlock()
	if getErr != nil {
		// Registration is already durable at this point. Do not return a failed
		// registration response: callers could retry and be told the account is
		// already registered. Preserve the successful outcome and expose reward
		// processing as a separately retryable state instead.
		rewardStatus = "reward_failed"
		if createdReferralID != "" {
			// The referral row was just created and grantUserReferralRewards
			// recorded the durable reward_failed status.
			_ = recordUserReferralStatusHistory(r.Context(), repo, tenantID, createdReferralID, "attributed", "reward_failed", "reward processing failed", "system")
			recordUserReferralRewardFailure(r.Context(), failureLogs, tenantID, createdReferralID)
			recordUserReferralMetric(r.Context(), repo, tenantID, userReferralMetricRewardFailed)
		}
	}
	if getErr == nil && createdReferralID != "" && rewardStatus == "rewarded" {
		_ = recordUserReferralStatusHistory(r.Context(), repo, tenantID, createdReferralID, "attributed", "rewarded", "", "system")
	}
	// Link the one short-lived registration session to the completed referral so
	// callers can recover a dropped success response without testing account
	// existence or trying to register a second time. The persisted identifiers
	// are opaque internal IDs; no contact data or invitation secret is stored.
	if sessionHash, ok := userReferralRegistrationSessionHash(r, tenantID); ok {
		if completed, completedErr := repo.GetReferralForInvitee(r.Context(), tenantID, user.ID); completedErr == nil && completed != nil {
			_ = repo.MarkRegistrationSessionCompleted(r.Context(), tenantID, sessionHash, user.ID, completed.ID, time.Now().UTC())
		}
	}
	payload, _ := json.Marshal(map[string]any{"registered": true, "reward_status": rewardStatus, "invitee_credits": cfg.InviteeCredits, "duration_days": cfg.DurationDays, "downloads": cfg.Downloads})
	if err := finishPersistedUserReferralIdempotency(r.Context(), repo, tenantID, idempotencyKey, idempotencyFingerprint, http.StatusCreated, payload); err != nil {
		// The account and attribution are already durable; returning an error
		// would incorrectly imply registration failed. Keep the success response
		// and let a retry fall back to the normal existing-account guard if this
		// exceptional persistence failure survives.
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(payload)
}

func grantUserReferralRewards(ctx context.Context, repo store.UserReferralRepository, system store.SystemSettingsRepository, identity *auth.IdentityService, referral *store.UserReferral) error {
	if referral == nil {
		return errors.New("referral is required")
	}
	inviter, err := identity.UsersRepo().GetByID(ctx, referral.InviterUserID)
	if err != nil || inviter == nil || store.NormalizeTenantID(inviter.TenantID) != store.NormalizeTenantID(referral.TenantID) {
		if err == nil {
			err = errors.New("inviter unavailable")
		}
		_ = repo.UpdateRewardGrants(ctx, referral.TenantID, referral.ID, "reward_failed", referral.InviterGrantID, referral.InviteeGrantID, time.Now().UTC())
		return err
	}
	invitee, err := identity.UsersRepo().GetByID(ctx, referral.InviteeUserID)
	if err != nil || invitee == nil || store.NormalizeTenantID(invitee.TenantID) != store.NormalizeTenantID(referral.TenantID) {
		if err == nil {
			err = errors.New("invitee unavailable")
		}
		_ = repo.UpdateRewardGrants(ctx, referral.TenantID, referral.ID, "reward_failed", referral.InviterGrantID, referral.InviteeGrantID, time.Now().UTC())
		return err
	}
	tenantSystem := ScopedSystemSettingsForTenant(referral.TenantID, system)
	inviterGrant, inviteeGrant := referral.InviterGrantID, referral.InviteeGrantID
	if referral.InviterCredits > 0 && inviterGrant == "" {
		inviterGrant, err = llmservice.GrantUserReferralBenefitForUserID(ctx, tenantSystem, inviter.ID, inviter.Email, referral.ID, referral.ServiceGroupID, referral.DurationDays, referral.InviterCredits, referral.RegisteredAt)
		if err == nil && inviterGrant == "" {
			err = errors.New("inviter reward grant was not created")
		}
	}
	if err == nil && referral.InviteeCredits > 0 && inviteeGrant == "" {
		inviteeGrant, err = llmservice.GrantUserReferralBenefitForUserID(ctx, tenantSystem, invitee.ID, invitee.Email, referral.ID, referral.ServiceGroupID, referral.DurationDays, referral.InviteeCredits, referral.RegisteredAt)
		if err == nil && inviteeGrant == "" {
			err = errors.New("invitee reward grant was not created")
		}
	}
	status := "rewarded"
	if err != nil {
		status = "reward_failed"
	}
	updateErr := repo.UpdateRewardGrants(ctx, referral.TenantID, referral.ID, status, inviterGrant, inviteeGrant, time.Now().UTC())
	if err != nil {
		return err
	}
	return updateErr
}

// UserReferralRewardRecoveryResult describes one best-effort startup recovery
// pass. Failures are intentionally retained as reward_failed so a later start
// or the audited admin retry action can continue the idempotent compensation.
type UserReferralRewardRecoveryResult struct {
	Scanned             int
	Rewarded            int
	Failed              int
	RewardFailedBacklog int
}

// UserReferralExpiryCleanupResult records the work completed by one
// best-effort expiry pass. A reserved referral has already registered a user,
// but its reward is intentionally held for risk review; after the bounded
// review window it becomes terminally expired rather than remaining in the
// queue forever. Short-lived session artifacts are independent and are
// deleted only after their own expiry.
type UserReferralExpiryCleanupResult struct {
	ExpiredReserved      int
	IdempotencyRecords   int
	RegistrationSessions int
	IdentityReservations int
	Handoffs             int
}

// CleanupExpiredUserReferrals keeps the invitation review queue and its
// privacy-preserving temporary storage bounded. It covers every active tenant
// because public invitation endpoints do not have a single tenant context.
// It is safe to call repeatedly: status transitions use a compare-and-set and
// the repository appends history only for rows it actually transitioned.
func CleanupExpiredUserReferrals(ctx context.Context, repo store.UserReferralRepository, tenants store.TenantRepository, now time.Time) (UserReferralExpiryCleanupResult, error) {
	var result UserReferralExpiryCleanupResult
	if repo == nil {
		return result, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	if tenants != nil {
		allTenants, err := tenants.List(ctx)
		if err != nil {
			return result, err
		}
		for _, tenant := range allTenants {
			if tenant == nil || tenant.DeletedAt != nil || !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
				continue
			}
			expired, err := repo.ExpireReservedReferrals(ctx, tenant.ID, now.Add(-userReferralReservedReviewTTL), now)
			if err != nil {
				return result, err
			}
			result.ExpiredReserved += len(expired)
		}
	}
	cleaned, err := repo.CleanupExpiredRegistrationArtifacts(ctx, now)
	if err != nil {
		return result, err
	}
	result.IdempotencyRecords = cleaned.IdempotencyRecords
	result.RegistrationSessions = cleaned.Sessions
	result.IdentityReservations = cleaned.IdentityReservations
	result.Handoffs = cleaned.Handoffs
	return result, nil
}

// ReconcileUserReferralRewards recovers grants that were attributed before an
// interrupted/failed registry write. Each beneficiary grant is idempotent on
// referral ID and beneficiary, so replay never creates duplicate Credits. The
// scan deliberately includes disabled invitation campaigns: disabling stops
// future attribution, not an already registered user's pending reward.
func ReconcileUserReferralRewards(ctx context.Context, identity *auth.IdentityService, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository, failureLogs store.FailureEventLogRepository) (UserReferralRewardRecoveryResult, error) {
	var result UserReferralRewardRecoveryResult
	if identity == nil || repo == nil || system == nil || tenants == nil {
		return result, nil
	}
	allTenants, err := tenants.List(ctx)
	if err != nil {
		return result, err
	}
	for _, tenant := range allTenants {
		if tenant == nil || tenant.DeletedAt != nil || !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
			continue
		}
		candidates, err := repo.ListRewardRecoveryCandidates(ctx, tenant.ID, 0)
		if err != nil {
			return result, err
		}
		for _, candidate := range candidates {
			if candidate == nil {
				continue
			}
			userReferralMu.Lock()
			// Re-read while holding the same lock as initial registration. A prior
			// recovery pass may already have completed this row, and a terminal
			// moderation action must never be replayed into a new reward.
			referral, getErr := repo.GetReferralByID(ctx, tenant.ID, candidate.ID)
			if getErr != nil || referral == nil || (referral.Status != "attributed" && referral.Status != "reward_failed") {
				userReferralMu.Unlock()
				continue
			}
			result.Scanned++
			fromStatus := referral.Status
			grantErr := grantUserReferralRewards(ctx, repo, system, identity, referral)
			userReferralMu.Unlock()
			if grantErr != nil {
				result.Failed++
				_ = recordUserReferralStatusHistory(ctx, repo, tenant.ID, referral.ID, fromStatus, "reward_failed", "startup reward recovery failed", "system")
				recordUserReferralRewardFailure(ctx, []store.FailureEventLogRepository{failureLogs}, tenant.ID, referral.ID)
				recordUserReferralMetric(ctx, repo, tenant.ID, userReferralMetricRewardFailed)
				continue
			}
			result.Rewarded++
			_ = recordUserReferralStatusHistory(ctx, repo, tenant.ID, referral.ID, fromStatus, "rewarded", "startup reward recovery completed", "system")
		}
		// Re-read the durable state after recovery. This is intentionally not
		// inferred from Failed: a previously failed item may be recovered, and
		// the backlog is what operators need to act on.
		remaining, remainingErr := repo.ListRewardRecoveryCandidates(ctx, tenant.ID, 0)
		if remainingErr != nil {
			return result, remainingErr
		}
		for _, referral := range remaining {
			if referral != nil && strings.EqualFold(strings.TrimSpace(referral.Status), "reward_failed") {
				result.RewardFailedBacklog++
			}
		}
	}
	return result, nil
}

func resolvePublicReferral(ctx context.Context, code string, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository, users ...store.UserRepository) (string, string, UserReferralConfig, *store.UserReferralCode, error) {
	if code == "" || repo == nil || tenants == nil {
		return "", "", UserReferralConfig{}, nil, errors.New("invalid referral")
	}
	all, err := tenants.List(ctx)
	if err != nil {
		return "", "", UserReferralConfig{}, nil, err
	}
	for _, tenant := range all {
		if tenant == nil || tenant.DeletedAt != nil || !strings.EqualFold(tenant.Status, "active") {
			continue
		}
		found, lookupErr := repo.GetCodeByHash(ctx, tenant.ID, userReferralCodeHash(tenant.ID, code))
		if lookupErr != nil {
			return "", "", UserReferralConfig{}, nil, lookupErr
		}
		if found != nil {
			cfg, cfgErr := loadUserReferralConfig(ctx, system, tenant.ID)
			if cfgErr != nil || !cfg.Enabled {
				return "", "", UserReferralConfig{}, nil, errors.New("disabled")
			}
			if len(users) > 0 && users[0] != nil {
				inviter, inviterErr := users[0].GetByID(ctx, found.InviterUserID)
				if inviterErr != nil || inviter == nil || store.NormalizeTenantID(inviter.TenantID) != store.NormalizeTenantID(tenant.ID) || !strings.EqualFold(strings.TrimSpace(inviter.Status), "active") {
					return "", "", UserReferralConfig{}, nil, errors.New("inviter unavailable")
				}
			}
			return tenant.ID, code, cfg, found, nil
		}
	}
	return "", "", UserReferralConfig{}, nil, errors.New("not found")
}

// resolvePublicReferralRequest accepts either the public invitation-code path
// (browser flow) or a claimed desktop handoff. A desktop request carries no
// referral code: the short-lived session token and tenant header are resolved
// against the persisted HMAC-only session state before the referral code row
// is fetched. This prevents a deep link from becoming a client-controlled
// tenant or inviter selector.
func resolvePublicReferralRequest(ctx context.Context, r *http.Request, code string, repo store.UserReferralRepository, system store.SystemSettingsRepository, tenants store.TenantRepository, users ...store.UserRepository) (string, string, UserReferralConfig, *store.UserReferralCode, error) {
	if strings.TrimSpace(code) != "" {
		return resolvePublicReferral(ctx, code, repo, system, tenants, users...)
	}
	if repo == nil || r == nil {
		return "", "", UserReferralConfig{}, nil, errors.New("invalid referral")
	}
	tenantID := store.NormalizeTenantID(strings.TrimSpace(r.Header.Get(userReferralRegistrationTenantHeader)))
	token, ok := userReferralRegistrationSessionToken(r)
	if !ok || tenantID == "" || len(token) > 256 {
		return "", "", UserReferralConfig{}, nil, errors.New("missing desktop referral session")
	}
	session, err := repo.GetRegistrationSession(ctx, tenantID, userReferralRegistrationSessionTokenHash(tenantID, token), time.Now().UTC())
	if err != nil || session == nil {
		return "", "", UserReferralConfig{}, nil, errors.New("expired desktop referral session")
	}
	cfg, err := loadUserReferralConfig(ctx, system, tenantID)
	if err != nil || !cfg.Enabled || session.ConfigEpoch != cfg.SessionEpoch {
		return "", "", UserReferralConfig{}, nil, errors.New("desktop referral session unavailable")
	}
	found, err := repo.GetCodeByHash(ctx, tenantID, session.CodeHash)
	if err != nil || found == nil {
		return "", "", UserReferralConfig{}, nil, errors.New("desktop referral invitation unavailable")
	}
	if len(users) > 0 && users[0] != nil {
		inviter, inviterErr := users[0].GetByID(ctx, found.InviterUserID)
		if inviterErr != nil || inviter == nil || store.NormalizeTenantID(inviter.TenantID) != tenantID || !strings.EqualFold(strings.TrimSpace(inviter.Status), "active") {
			return "", "", UserReferralConfig{}, nil, errors.New("inviter unavailable")
		}
	}
	return tenantID, "", cfg, found, nil
}

func referralRequestReference(r *http.Request, code, tenantID string) string {
	if strings.TrimSpace(code) != "" {
		return code
	}
	if token, ok := userReferralRegistrationSessionToken(r); ok {
		return "desktop:" + userReferralRegistrationSessionTokenHash(tenantID, token)
	}
	return "desktop:missing"
}
func writeReferralRegistrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUserAlreadyRegistered):
		writeError(w, 409, "ALREADY_REGISTERED", "This invitation is only for new users")
	case errors.Is(err, auth.ErrRegistrationDisabled):
		writeError(w, 403, "REGISTRATION_DISABLED", "Registration is unavailable")
	case errors.Is(err, auth.ErrTenantInactive), errors.Is(err, auth.ErrTenantNotFound):
		writeError(w, 404, "INVITATION_UNAVAILABLE", "This invitation is unavailable")
	default:
		writeError(w, 409, "ALREADY_REGISTERED", "This invitation is only for new users")
	}
}
