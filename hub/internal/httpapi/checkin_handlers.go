package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	checkinConfigKey = "checkin_config"
	// checkinLastKeyPrefix scopes the per-user last check-in marker under the
	// tenant settings namespace: "checkin:last:<userID>" -> "2006-01-02".
	checkinLastKeyPrefix = "checkin:last:"
	// checkinLogKeyPrefix scopes the per-day check-in audit log used by the
	// admin records view: "checkin:log:<YYYY-MM-DD>" (UTC day).
	checkinLogKeyPrefix        = "checkin:log:"
	checkinGrantSource         = "checkin"
	checkinMaxCredits          = 1000000
	checkinDefaultDurationDays = 30
	checkinMaxDurationDays     = 3650
	checkinLogDayFormat        = "2006-01-02"

	maxCheckinConfigBodyBytes = 64 << 10
)

// CheckinConfig is the tenant-scoped daily check-in policy configured from the
// admin console system settings. Credits are granted once per user per local
// day while Enabled is on. DurationDays is how long a new reward stays
// spendable. A stored value that is missing or non-positive loads as 30 days
// (one month). An update must send an integer from 1 to 3650; omitting the
// field keeps the stored duration.
type CheckinConfig struct {
	Enabled      bool    `json:"enabled"`
	Credits      float64 `json:"credits"`
	DurationDays int     `json:"duration_days"`
}

func defaultCheckinConfig() CheckinConfig {
	return CheckinConfig{Enabled: false, Credits: 10, DurationDays: checkinDefaultDurationDays}
}

func loadCheckinConfig(ctx context.Context, system store.SystemSettingsRepository) CheckinConfig {
	cfg := defaultCheckinConfig()
	if system == nil {
		return cfg
	}
	raw, err := system.Get(ctx, checkinConfigKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultCheckinConfig()
	}
	return normalizeCheckinConfig(cfg)
}

func normalizeCheckinConfig(cfg CheckinConfig) CheckinConfig {
	if cfg.Credits < 0 {
		cfg.Credits = 0
	}
	if cfg.Credits > checkinMaxCredits {
		cfg.Credits = checkinMaxCredits
	}
	if cfg.DurationDays <= 0 {
		cfg.DurationDays = checkinDefaultDurationDays
	} else if cfg.DurationDays > checkinMaxDurationDays {
		cfg.DurationDays = checkinMaxDurationDays
	}
	return cfg
}

// checkinConfigUpdate distinguishes an omitted duration from an explicit one.
// A missing duration keeps the stored value so an older admin page cannot
// reset a custom validity back to the 30-day default.
type checkinConfigUpdate struct {
	Enabled      bool     `json:"enabled"`
	Credits      float64  `json:"credits"`
	DurationDays *float64 `json:"duration_days"`
}

func checkinDurationFromUpdate(raw *float64) (int, bool, error) {
	if raw == nil {
		return 0, false, nil
	}
	if math.IsNaN(*raw) || math.IsInf(*raw, 0) || *raw != math.Trunc(*raw) || *raw < 1 || *raw > float64(checkinMaxDurationDays) {
		return 0, true, fmt.Errorf("duration_days must be an integer between 1 and %d", checkinMaxDurationDays)
	}
	return int(*raw), true, nil
}

func validateCheckinConfig(cfg CheckinConfig) error {
	if math.IsNaN(cfg.Credits) || math.IsInf(cfg.Credits, 0) || cfg.Credits < 0 || cfg.Credits > checkinMaxCredits {
		return fmt.Errorf("credits must be between 0 and %d", checkinMaxCredits)
	}
	if cfg.Enabled && cfg.Credits < 1 {
		return fmt.Errorf("credits must be at least 1 when check-in is enabled")
	}
	if cfg.DurationDays < 1 || cfg.DurationDays > checkinMaxDurationDays {
		return fmt.Errorf("duration_days must be an integer between 1 and %d", checkinMaxDurationDays)
	}
	return nil
}

func GetCheckinConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if system == nil {
			writeError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_UNAVAILABLE", "System settings are unavailable")
			return
		}
		writeJSON(w, http.StatusOK, loadCheckinConfig(r.Context(), scopedSystemSettingsForRequest(r, system)))
	}
}

func UpdateCheckinConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if system == nil {
			writeError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_UNAVAILABLE", "System settings are unavailable")
			return
		}
		var req checkinConfigUpdate
		data, err := io.ReadAll(io.LimitReader(r.Body, maxCheckinConfigBodyBytes+1))
		if err != nil || len(data) > maxCheckinConfigBodyBytes {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must contain one JSON object")
			return
		}
		days, present, err := checkinDurationFromUpdate(req.DurationDays)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_CHECKIN_CONFIG", err.Error())
			return
		}
		settings := scopedSystemSettingsForRequest(r, system)
		// Clamp credits the way this endpoint always has, then apply duration.
		// An omitted duration keeps the saved value; the 30-day default applies
		// only when nothing has been stored yet.
		cfg := normalizeCheckinConfig(CheckinConfig{Enabled: req.Enabled, Credits: req.Credits})
		if present {
			cfg.DurationDays = days
		} else {
			cfg.DurationDays = loadCheckinConfig(r.Context(), settings).DurationDays
		}
		if err := validateCheckinConfig(cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_CHECKIN_CONFIG", err.Error())
			return
		}
		encoded, err := json.Marshal(cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CHECKIN_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		if err := settings.Set(r.Context(), checkinConfigKey, string(encoded)); err != nil {
			writeError(w, http.StatusInternalServerError, "CHECKIN_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

// hubCheckinInfo is the viewer-facing check-in state embedded in the LLM
// service status responses so the desktop GUI can render the check-in button.
type hubCheckinInfo struct {
	Enabled        bool    `json:"enabled"`
	Credits        float64 `json:"credits"`
	DurationDays   int     `json:"duration_days,omitempty"`
	CheckedInToday bool    `json:"checked_in_today"`
}

// checkinGrantMu serializes check-in grants per process so two concurrent
// requests cannot both pass the last-check-in check.
var checkinGrantMu sync.Mutex

func checkinLastKey(userID string) string {
	return checkinLastKeyPrefix + strings.TrimSpace(userID)
}

func checkinDayKeyFor(reg *llmservice.Registry, email string, now time.Time) string {
	location := time.UTC
	if reg != nil {
		tz := strings.TrimSpace(reg.UserBillingTimezones[strings.ToLower(strings.TrimSpace(email))])
		if tz != "" {
			if parsed, err := time.LoadLocation(tz); err == nil {
				location = parsed
			}
		}
	}
	return now.In(location).Format("2006-01-02")
}

func checkinLastDay(ctx context.Context, system store.SystemSettingsRepository, userID string) string {
	if system == nil {
		return ""
	}
	raw, err := system.Get(ctx, checkinLastKey(userID))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

// checkinInfoForUser reports the tenant check-in policy and whether the user
// already checked in on their current local day.
func checkinInfoForUser(ctx context.Context, system store.SystemSettingsRepository, reg *llmservice.Registry, userID, email string) *hubCheckinInfo {
	cfg := loadCheckinConfig(ctx, system)
	checkedIn := cfg.Enabled && checkinLastDay(ctx, system, userID) == checkinDayKeyFor(reg, email, time.Now())
	return cfg.viewerCheckinInfo(checkedIn)
}

func (cfg CheckinConfig) viewerCheckinInfo(checkedInToday bool) *hubCheckinInfo {
	if !cfg.Enabled {
		return &hubCheckinInfo{Enabled: false}
	}
	return &hubCheckinInfo{
		Enabled:        true,
		Credits:        cfg.Credits,
		DurationDays:   cfg.DurationDays,
		CheckedInToday: checkedInToday,
	}
}

// splitCheckinCredits divides a reward across service groups at milli-credit
// precision. Earlier shares are rounded, and the last share keeps the
// remainder so the grants still add up to the awarded total.
func splitCheckinCredits(total float64, groups int) []float64 {
	if groups <= 0 {
		return nil
	}
	total = math.Round(total*1000) / 1000
	shares := make([]float64, groups)
	if groups == 1 {
		shares[0] = total
		return shares
	}
	each := math.Round((total/float64(groups))*1000) / 1000
	used := 0.0
	for i := 0; i < groups-1; i++ {
		share := each
		// Rounding every earlier share up can exceed the awarded total when
		// the reward is small relative to the group count. Cap that share
		// instead of letting the excess land in the wallet.
		if used+share > total {
			share = math.Round((total-used)*1000) / 1000
			if share < 0 {
				share = 0
			}
		}
		shares[i] = share
		used = math.Round((used+share)*1000) / 1000
	}
	last := math.Round((total-used)*1000) / 1000
	if last < 0 {
		last = 0
	}
	shares[groups-1] = last
	return shares
}

type hubCheckinResponse struct {
	Success          bool                      `json:"success"`
	AlreadyCheckedIn bool                      `json:"already_checked_in"`
	CreditsAwarded   float64                   `json:"credits_awarded"`
	Checkin          *hubCheckinInfo           `json:"checkin,omitempty"`
	ServiceStatus    *llmservice.ServiceStatus `json:"service_status,omitempty"`
}

// PostLLMServiceCheckinHandler grants the configured daily check-in credits to
// the authenticated viewer. The reward lands as a "checkin" grant in the LLM
// service registry, so it participates in the regular credit wallet.
func PostLLMServiceCheckinHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		system = scopedSystemSettingsForTenant(principal.TenantID, system)
		ctx := security.WithTenant(r.Context(), principal.TenantID)
		cfg := loadCheckinConfig(ctx, system)
		if !cfg.Enabled {
			writeError(w, http.StatusBadRequest, "CHECKIN_DISABLED", "Check-in is not enabled")
			return
		}
		checkinGrantMu.Lock()
		defer checkinGrantMu.Unlock()

		reg, err := llmservice.LoadRegistry(ctx, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_STATUS_FAILED", err.Error())
			return
		}
		today := checkinDayKeyFor(reg, principal.Email, time.Now())
		if checkinLastDay(ctx, system, principal.UserID) == today {
			writeJSON(w, http.StatusOK, hubCheckinResponse{Success: true, AlreadyCheckedIn: true, Checkin: cfg.viewerCheckinInfo(true)})
			return
		}
		awarded := 0.0
		if cfg.Credits > 0 {
			groupIDs := checkinGrantGroupIDs(ctx, reg, securitySvc, principal.UserID, principal.Email)
			if len(groupIDs) == 0 {
				writeError(w, http.StatusBadRequest, "CHECKIN_NO_SERVICE_GROUPS", "No metered LLM service groups are available for check-in rewards")
				return
			}
			shares := splitCheckinCredits(cfg.Credits, len(groupIDs))
			now := time.Now().UTC()
			expiresAt := now.AddDate(0, 0, cfg.DurationDays)
			grantedGroups := 0
			for i, groupID := range groupIDs {
				if shares[i] <= 0 {
					continue
				}
				grantedGroups++
				awarded += shares[i]
				reg.Grants = append(reg.Grants, llmservice.Grant{
					ID:             llmservice.NewID("grant"),
					UserID:         principal.UserID,
					Email:          principal.Email,
					ServiceGroupID: groupID,
					Source:         checkinGrantSource,
					StartsAt:       now,
					ExpiresAt:      expiresAt,
					CreatedAt:      now,
					CreditsTotal:   shares[i],
				})
			}
			awarded = math.Round(awarded*1000) / 1000
			if err := llmservice.SaveRegistry(ctx, system, reg); err != nil {
				writeError(w, http.StatusInternalServerError, "CHECKIN_SAVE_FAILED", err.Error())
				return
			}
			invalidateLLMRuntimeCaches(system)
			log.Printf("[checkin] granted credits user_id=%s email=%s tenant_id=%s credits=%.3f groups=%d duration_days=%d expires_at=%s",
				principal.UserID, principal.Email, principal.TenantID, awarded, grantedGroups, cfg.DurationDays, expiresAt.Format(time.RFC3339))
		}
		if err := system.Set(ctx, checkinLastKey(principal.UserID), today); err != nil {
			log.Printf("[checkin] last-checkin marker save failed user_id=%s err=%v", principal.UserID, err)
		}
		appendCheckinLog(ctx, system, checkinLogEntry{
			UserID:  principal.UserID,
			Email:   principal.Email,
			Credits: awarded,
			At:      time.Now().UTC(),
		})
		response := hubCheckinResponse{
			Success:        true,
			CreditsAwarded: awarded,
			Checkin:        cfg.viewerCheckinInfo(true),
		}
		if status, _, statusErr := llmservice.ResolveStatusFromRegistryForUser(ctx, reg, securitySvc, principal.UserID, principal.Email, externalLLMBaseURL(r)); statusErr == nil {
			response.ServiceStatus = status
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// checkinGrantGroupIDs picks the check-in reward targets: the account's
// currently effective service groups whose access policy is grant_required.
//
// Free-policy groups (including the reserved system-free group and the builtin
// no-permissions fallback, whose empty access policy normalizes to free) never
// debit credits — rewards granted there would sit unspendable while inflating
// the displayed wallet. Restricting to already-effective groups also keeps the
// grant from widening the account's model access, matching the referral
// reward flow which refuses non-metered service groups.
func checkinGrantGroupIDs(ctx context.Context, reg *llmservice.Registry, securitySvc *security.SecurityService, userID, email string) []string {
	effective, err := llmservice.EffectiveServiceGroupIDsForUser(ctx, reg, securitySvc, userID, email)
	if err != nil {
		log.Printf("[checkin] effective service group resolution failed user_id=%s err=%v", userID, err)
		return nil
	}
	ids := make([]string, 0, len(effective))
	for _, id := range effective {
		group := reg.FindModelServiceGroup(id)
		if group == nil || llmservice.NormalizeAccessPolicy(group.AccessPolicy) != llmservice.AccessPolicyGrantRequired {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// checkinLogEntry is one immutable check-in event in the tenant day log.
type checkinLogEntry struct {
	UserID  string    `json:"user_id,omitempty"`
	Email   string    `json:"email"`
	Credits float64   `json:"credits"`
	At      time.Time `json:"at"`
}

// appendCheckinLog records a check-in event under the UTC day key. Callers
// must hold checkinGrantMu: the log is a read-modify-write of one JSON array.
// Best-effort by design — a failed log write must not fail the check-in.
func appendCheckinLog(ctx context.Context, system store.SystemSettingsRepository, entry checkinLogEntry) {
	if system == nil {
		return
	}
	key := checkinLogKeyPrefix + entry.At.UTC().Format(checkinLogDayFormat)
	entries := loadCheckinLogDay(ctx, system, key)
	entries = append(entries, entry)
	data, err := json.Marshal(entries)
	if err != nil {
		log.Printf("[checkin] log marshal failed: %v", err)
		return
	}
	if err := system.Set(ctx, key, string(data)); err != nil {
		log.Printf("[checkin] log save failed key=%s: %v", key, err)
	}
}

func loadCheckinLogDay(ctx context.Context, system store.SystemSettingsRepository, key string) []checkinLogEntry {
	if system == nil {
		return nil
	}
	raw, err := system.Get(ctx, key)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var entries []checkinLogEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	return entries
}

// checkinPeriodRange returns the UTC [start, end) window for a stats period.
func checkinPeriodRange(period string, now time.Time) (time.Time, time.Time, bool) {
	now = now.UTC()
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "day", "":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1), true
	case "week":
		// Monday-based calendar week.
		offset := (int(now.Weekday()) + 6) % 7
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 7), true
	case "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), true
	default:
		return time.Time{}, time.Time{}, false
	}
}

const checkinRecordsPageSize = 50

// maxCheckinRecordsPage bounds the page offset so (page-1)*page_size cannot
// overflow int on hostile query strings; 100k pages covers 5M records.
const maxCheckinRecordsPage = 100000

// GetCheckinRecordsHandler serves the admin check-in users view: paged record
// cards (50 per page) plus per-period stats (distinct users, total credits).
func GetCheckinRecordsHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if system == nil {
			writeError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_UNAVAILABLE", "System settings are unavailable")
			return
		}
		period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
		start, end, ok := checkinPeriodRange(period, time.Now())
		if !ok {
			writeError(w, http.StatusBadRequest, "INVALID_CHECKIN_PERIOD", "period must be day, week, or month")
			return
		}
		page := 1
		if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				page = parsed
			} else {
				writeError(w, http.StatusBadRequest, "INVALID_CHECKIN_PAGE", "page must be a positive integer")
				return
			}
			if page > maxCheckinRecordsPage {
				page = maxCheckinRecordsPage
			}
		}
		system = scopedSystemSettingsForTenant(RequestTenantID(r), system)
		ctx := r.Context()
		var records []checkinLogEntry
		users := make(map[string]struct{})
		creditsTotal := 0.0
		for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
			for _, entry := range loadCheckinLogDay(ctx, system, checkinLogKeyPrefix+day.Format(checkinLogDayFormat)) {
				records = append(records, entry)
				if entry.UserID != "" {
					users[entry.UserID] = struct{}{}
				} else {
					users[strings.ToLower(entry.Email)] = struct{}{}
				}
				creditsTotal += entry.Credits
			}
		}
		// Newest first for the card list.
		sort.Slice(records, func(i, j int) bool { return records[i].At.After(records[j].At) })
		total := len(records)
		first := (page - 1) * checkinRecordsPageSize
		last := first + checkinRecordsPageSize
		if first > total {
			first = total
		}
		if last > total {
			last = total
		}
		pageRecords := records[first:last]
		if pageRecords == nil {
			pageRecords = []checkinLogEntry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"period":    period,
			"page":      page,
			"page_size": checkinRecordsPageSize,
			"total":     total,
			"stats": map[string]any{
				"users":   len(users),
				"credits": math.Round(creditsTotal*100) / 100,
			},
			"records": pageRecords,
		})
	}
}
