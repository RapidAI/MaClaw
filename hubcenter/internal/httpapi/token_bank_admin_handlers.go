package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// Token Bank admin API (§6.2). Everything here is behind RequireAdmin, so the
// handlers trust their caller completely and do NOT scope by owner — the store
// methods they call take an optional owner precisely so the same code serves
// the client API (scoped) and this one (unscoped).
//
// The one rule that survives is that money must never be silently lost: a
// revoke here always returns the freeze to the sender of record, and a tier
// change never touches the settlement counters.

// tokenBankAdminRepoView is the slice of the repository the admin surface
// needs. It is a separate interface from tokenBankRepoView so that a partially
// wired node reports the endpoints it really has rather than 503-ing the whole
// admin tab. The settings endpoints do not appear here because they live in the
// system-settings store (they replicate as settings, not as Token Bank rows).
type tokenBankAdminRepoView interface {
	Overview(ctx context.Context) (sqlite.TokenBankOverview, error)
	ListShareOwners(ctx context.Context, limit, offset int) ([]sqlite.TokenBankOwnerSummary, error)
	ListShares(ctx context.Context, ownerUserID, status string, limit, offset int) ([]sqlite.TokenBankShare, error)
	LoadShare(ctx context.Context, shareID, scopeOwner string) (*sqlite.TokenBankShare, error)
	ListModels(ctx context.Context, shareID string) ([]sqlite.TokenBankShareModel, error)
	SumSettledNetByShare(ctx context.Context, shareIDs []string) (map[string]int64, map[string]map[string]int64, error)
	SetSharePaused(ctx context.Context, shareID, scopeOwner, reason string, paused bool, now time.Time) error
	TakeOutShare(ctx context.Context, shareID, scopeOwner string) (bool, error)
	SetModelTier(ctx context.Context, shareID, modelName, tier string, tierMultiplier float64, arrayID string, now time.Time) error

	ListPriceRules(ctx context.Context) ([]sqlite.TokenBankPriceRule, error)
	UpsertPriceRule(ctx context.Context, rule sqlite.TokenBankPriceRule, now time.Time) (*sqlite.TokenBankPriceRule, error)
	DeletePriceRule(ctx context.Context, idOrPattern string) (bool, error)

	ListGiftLinks(ctx context.Context, senderUserID, status string, limit int) ([]sqlite.TokenBankGiftLink, error)
	RevokeGiftLink(ctx context.Context, linkID, senderUserID string, now time.Time) error

	UsageMargins(ctx context.Context, days int, group sqlite.TokenBankMarginGroup) ([]sqlite.TokenBankMarginRow, error)
}

// tokenBankAdminRepo returns the admin view of the repository, or nil when the
// LLM module has not been wired. Distinct from tokenBankRepo() so a node that
// only initialised the client side still answers 503 instead of panicking.
func (h *SkillMarketHandlers) tokenBankAdminRepo() tokenBankAdminRepoView {
	if h == nil || h.tokenBank == nil {
		return nil
	}
	view, _ := h.tokenBank.(tokenBankAdminRepoView)
	return view
}

// --- GET/PUT /api/admin/token-bank/settings ---

// TokenBankGetSettings serves the current settings, falling back to the
// documented defaults when nothing has ever been saved. Returning defaults
// rather than 404 lets the admin form render immediately.
func (h *SkillMarketHandlers) TokenBankGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"settings": settings,
	})
}

// TokenBankPutSettings validates and stores the settings blob.
//
// The body is a patch on the stored blob, not a replacement. A field the
// caller leaves out keeps its stored value (or the documented default, when
// nothing is stored yet). The admin form edits the rates and the caps; it does
// not edit clearing_node_id or provider_denylist, and a replace would blank
// both on every save. An explicit empty string or empty list still clears,
// because that key is present in the patch.
func (h *SkillMarketHandlers) TokenBankPutSettings(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.tokenBankSettingsRepo() == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "settings store is not available on this node")
		return
	}
	var body struct {
		Settings json.RawMessage `json:"settings"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", "settings payload is not valid JSON")
		return
	}
	current, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", err.Error())
		return
	}
	merged, err := mergeTokenBankSettings(current, body.Settings)
	if err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	settings, err := validateTokenBankSettings(merged)
	if err != nil {
		tbError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	if err := h.tokenBankSettingsRepo().Set(r.Context(), sqlite.TokenBankSettingsKey, string(encoded)); err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": settings})
}

// mergeTokenBankSettings overlays a JSON object onto the current settings.
// Keys absent from the patch are left as they are. A patch that is not a JSON
// object is rejected, so a string or array cannot wipe the blob.
func mergeTokenBankSettings(current sqlite.TokenBankSettings, patch json.RawMessage) (sqlite.TokenBankSettings, error) {
	trimmed := strings.TrimSpace(string(patch))
	if trimmed == "" || trimmed == "null" {
		return current, errors.New("settings object is required")
	}
	var overlay map[string]json.RawMessage
	if err := json.Unmarshal(patch, &overlay); err != nil {
		return current, errors.New("settings payload is not a JSON object")
	}
	base, err := json.Marshal(current)
	if err != nil {
		return current, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return current, err
	}
	for key, value := range overlay {
		merged[key] = value
	}
	blob, err := json.Marshal(merged)
	if err != nil {
		return current, err
	}
	// Unmarshal into a zero value. merged already contains every field Marshal
	// wrote for current, so a key the patch omitted cannot fall back to zero.
	var out sqlite.TokenBankSettings
	if err := json.Unmarshal(blob, &out); err != nil {
		return current, errors.New("settings payload is not valid JSON")
	}
	return out, nil
}

// tokenBankSettingsRepo returns the system-settings repository the handlers
// already hold. Using it rather than the LLM service keeps this file free of an
// llmservice dependency and, more importantly, gets HA replication for free:
// system settings are replicated as settings, not as Token Bank rows.
func (h *SkillMarketHandlers) tokenBankSettingsRepo() store.SystemSettingsRepository {
	if h == nil {
		return nil
	}
	return h.settings
}

// loadTokenBankSettings reads the blob. A missing key yields the documented
// defaults, and a nil settings repository degrades the same way: the admin tab
// should still open on a node whose settings store is not wired.
func (h *SkillMarketHandlers) loadTokenBankSettings(ctx context.Context) (sqlite.TokenBankSettings, error) {
	repo := h.tokenBankSettingsRepo()
	if repo == nil {
		return sqlite.DefaultTokenBankSettings(), nil
	}
	raw, err := repo.Get(ctx, sqlite.TokenBankSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return sqlite.DefaultTokenBankSettings(), nil
	}
	return sqlite.ParseTokenBankSettings(raw), nil
}

// validateTokenBankSettings clamps a submitted blob into a legal configuration
// and returns an error only for values that are structurally meaningless. It
// repairs rather than rejects wherever a sane interpretation exists, because an
// admin who types 1.5 into a ratio field wants it bounded, not a 400.
func validateTokenBankSettings(in sqlite.TokenBankSettings) (sqlite.TokenBankSettings, error) {
	defaults := sqlite.DefaultTokenBankSettings()
	out := in

	// A negative rate is not a refund, it is a typo. Above 1.0 the platform
	// would take more than the entire earning.
	if out.FeeRate < 0 || out.FeeRate > 1 {
		return out, errors.New("fee_rate must be between 0 and 1")
	}
	// §5 fixes the fee target to the sharer's side. Accepting "consumer" here
	// would silently change who pays, which is a money decision, not a setting.
	out.FeeTarget = strings.TrimSpace(out.FeeTarget)
	if out.FeeTarget == "" {
		out.FeeTarget = defaults.FeeTarget
	}
	if out.FeeTarget != "provider" && out.FeeTarget != "consumer" {
		return out, errors.New("fee_target must be provider or consumer")
	}
	if out.FeeTarget != defaults.FeeTarget {
		return out, errors.New("fee_target must be provider: §5 takes the fee from the sharer's earnings")
	}

	for _, unit := range []struct {
		name  string
		value float64
	}{
		{"default_unit_input_credits_per_10k", out.DefaultUnitInputPer10K},
		{"default_unit_output_credits_per_10k", out.DefaultUnitOutputPer10K},
		{"default_unit_cached_read_credits_per_10k", out.DefaultUnitCachedReadPer10K},
		{"default_unit_cache_write_credits_per_10k", out.DefaultUnitCacheWritePer10K},
	} {
		if unit.value < 0 || math.IsNaN(unit.value) || math.IsInf(unit.value, 0) {
			return out, errors.New(unit.name + " must be a finite, non-negative number")
		}
	}

	if out.CreditShareMaxRatio <= 0 || out.CreditShareMaxRatio > 1 {
		return out, errors.New("credit_share_max_ratio must be between 0 and 1")
	}
	if out.CreditShareLinkTTLHours <= 0 {
		return out, errors.New("credit_share_link_ttl_hours must be positive")
	}
	if out.CreditShareDailyLimit < 0 {
		return out, errors.New("credit_share_daily_limit must not be negative")
	}
	if out.CreditShareMinCredits < 0 {
		return out, errors.New("credit_share_min_credits must not be negative")
	}
	if out.AutoPauseConsecutiveFailures < 0 {
		return out, errors.New("auto_pause_consecutive_failures must not be negative")
	}
	if out.MaxSharesPerUser <= 0 {
		return out, errors.New("max_shares_per_user must be positive: zero would close the feature")
	}
	if out.CanaryWindowHours < 0 {
		return out, errors.New("canary_window_hours must not be negative: zero skips the canary for a newly published model")
	}
	cleaned := make([]string, 0, len(out.ProviderDenylist))
	for _, item := range out.ProviderDenylist {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		cleaned = append(cleaned, item)
	}
	out.ProviderDenylist = cleaned
	return out, nil
}

// --- GET /api/admin/token-bank/overview ---

func (h *SkillMarketHandlers) TokenBankAdminOverview(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	overview, err := repo.Overview(r.Context())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	settings, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		settings = sqlite.DefaultTokenBankSettings()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                     true,
		"overview":               overview,
		"fee_rate":               settings.FeeRate,
		"fee_target":             settings.FeeTarget,
		"credit_share_max_ratio": settings.CreditShareMaxRatio,
	})
}

// --- GET /api/admin/token-bank/margins ---
//
// §9 #20. The platform's gross margin: what consumers paid, what sharers
// received, and the difference. §13 names this the ongoing control against
// billing inversion, which is why it reports inversions rather than only the
// margin — the clamp hides inversion from the margin figure by design.

func (h *SkillMarketHandlers) TokenBankAdminMargins(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	rows, err := repo.UsageMargins(r.Context(), marginDays(r), marginGroup(r))
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"key":             row.Key,
			"charged_micro":   row.ChargedMicro,
			"gross_micro":     row.GrossMicro,
			"fee_micro":       row.FeeMicro,
			"net_micro":       row.NetMicro,
			"margin_micro":    row.MarginMicro,
			"margin_rate":     row.MarginRate(),
			"calls":           row.Calls,
			"inverted_count":  row.InvertedCount,
			"clamped_count":   row.ClampedCount,
			"shortfall_micro": row.ShortfallMicro,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "margins": out})
}

// marginDays reads the window, defaulting to 30. An unparseable value falls
// back rather than 400: this is a dashboard query, and an admin who typed
// "30d" wants numbers, not an error.
func marginDays(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("days"))
	if raw == "" {
		return 30
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 30
	}
	return n
}

// marginGroup reads the grouping, defaulting to model. Unknown values fall
// through to the store's whitelist, which maps them to the grand total.
func marginGroup(r *http.Request) sqlite.TokenBankMarginGroup {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("group_by"))) {
	case "share":
		return sqlite.TokenBankMarginByShare
	case "day":
		return sqlite.TokenBankMarginByDay
	default:
		return sqlite.TokenBankMarginByModel
	}
}

// --- GET /api/admin/token-bank/users ---

func (h *SkillMarketHandlers) TokenBankAdminUsers(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	limit, offset := tokenBankAdminPage(r)
	owners, err := repo.ListShareOwners(r.Context(), limit, offset)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"users":  owners,
		"limit":  limit,
		"offset": offset,
	})
}

// --- GET /api/admin/token-bank/shares ---

// tokenBankSharePayload is the admin view of a share. It includes the models
// (so the tier editor has something to edit) and the masked owner email, but
// never the encrypted key: an admin has no need for it, and an endpoint that
// returns it is an endpoint that can leak it.
type tokenBankSharePayload struct {
	sqlite.TokenBankShare
	OwnerEmailMasked string                       `json:"owner_email_masked"`
	HasKey           bool                         `json:"has_key"`
	Models           []sqlite.TokenBankShareModel `json:"models"`
}

func (h *SkillMarketHandlers) TokenBankAdminShares(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	limit, offset := tokenBankAdminPage(r)
	owner := strings.TrimSpace(r.URL.Query().Get("user_id"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	shares, err := repo.ListShares(r.Context(), owner, status, limit, offset)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	modelsRaw := strings.TrimSpace(r.URL.Query().Get("include_models"))
	includeModels := modelsRaw == "" || modelsRaw == "1" || strings.EqualFold(modelsRaw, "true")

	shareIDs := make([]string, len(shares))
	for i, share := range shares {
		shareIDs[i] = share.ID
	}
	// Usage is the figure that includes calls settled before the denormalized
	// counters existed. The counters are still written for new calls; this read
	// is what the share list shows.
	shareNet, modelNet, err := repo.SumSettledNetByShare(r.Context(), shareIDs)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	out := make([]tokenBankSharePayload, 0, len(shares))
	for _, share := range shares {
		payload := tokenBankSharePayload{
			TokenBankShare:   share,
			OwnerEmailMasked: maskEmail(share.OwnerEmail),
			HasKey:           share.EncryptedKey != "",
		}
		payload.EncryptedKey = "" // never leave the process, even to an admin
		payload.TotalEarnedMicro = shareNet[share.ID]
		if includeModels {
			models, err := repo.ListModels(r.Context(), share.ID)
			if err != nil {
				tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
				return
			}
			for i := range models {
				key := strings.ToLower(strings.TrimSpace(models[i].ModelName))
				models[i].EarnedMicro = modelNet[share.ID][key]
			}
			payload.Models = models
		}
		out = append(out, payload)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"shares": out,
		"limit":  limit,
		"offset": offset,
	})
}

// --- PUT /api/admin/token-bank/shares/{id}/paused ---

type tokenBankPausedRequest struct {
	Paused bool   `json:"paused"`
	Reason string `json:"reason"`
}

func (h *SkillMarketHandlers) TokenBankAdminSetSharePaused(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	if shareID == "" {
		tbError(w, http.StatusBadRequest, "invalid_share", "share id is required")
		return
	}
	// Default to pausing when the body is empty: the endpoint is a pause
	// switch, and an accidental empty PUT must not silently resume a share the
	// admin paused because it was misbehaving.
	var req tokenBankPausedRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
			tbError(w, http.StatusBadRequest, "invalid_body", "paused payload is not valid JSON")
			return
		}
	} else {
		req.Paused = true
	}
	if _, err := repo.LoadShare(r.Context(), shareID, ""); err != nil {
		if errors.Is(err, sqlite.ErrTokenBankShareNotFound) {
			tbError(w, http.StatusNotFound, "share_not_found", "share not found")
			return
		}
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	var publishErr, storeErr error
	_ = withTokenBankRouteLock(func() error {
		publishErr, storeErr = h.tokenBankCommitSharePaused(r.Context(), repo, shareID, "", req.Reason, req.Paused, time.Now().UTC())
		if publishErr != nil {
			return publishErr
		}
		return storeErr
	})
	if publishErr != nil {
		tbError(w, http.StatusInternalServerError, "publish_failed", publishErr.Error())
		return
	}
	if storeErr != nil {
		if errors.Is(storeErr, sqlite.ErrTokenBankShareNotFound) {
			tbError(w, http.StatusNotFound, "share_not_found", "share not found")
			return
		}
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", storeErr.Error())
		return
	}
	share, err := repo.LoadShare(r.Context(), shareID, "")
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	share.EncryptedKey = ""
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "share": share})
}

// --- DELETE /api/admin/token-bank/shares/{id} ---

func (h *SkillMarketHandlers) TokenBankAdminTakeOutShare(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	if shareID == "" {
		tbError(w, http.StatusBadRequest, "invalid_share", "share id is required")
		return
	}
	var publishErr error
	var removed bool
	storeErr := withTokenBankRouteLock(func() error {
		if publisher := h.tokenBankPublisher(); publisher != nil {
			if _, err := publisher.UnpublishTokenBankShare(r.Context(), shareID); err != nil {
				publishErr = err
				return err
			}
		}
		var err error
		removed, err = repo.TakeOutShare(r.Context(), shareID, "")
		return err
	})
	if publishErr != nil {
		tbError(w, http.StatusInternalServerError, "publish_failed", publishErr.Error())
		return
	}
	if storeErr != nil {
		if errors.Is(storeErr, sqlite.ErrTokenBankShareNotFound) {
			tbError(w, http.StatusNotFound, "share_not_found", "share not found")
			return
		}
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", storeErr.Error())
		return
	}
	if !removed {
		tbError(w, http.StatusNotFound, "share_not_found", "share not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": shareID})
}

// --- PUT /api/admin/token-bank/shares/{id}/models/{model}/tier ---

type tokenBankTierRequest struct {
	Tier string `json:"tier"`
	// TierMultiplier and ArrayID are optional overrides. When absent the tier's
	// canonical pair is used, so a caller cannot set tier=high with a 1.0
	// multiplier and create a share that pays high rates for mid work.
	TierMultiplier *float64 `json:"tier_multiplier"`
	ArrayID        string   `json:"array_id"`
}

// tokenBankTierDefaults is the canonical (multiplier, array) pair per tier
// (§3.4). `custom` is not listed: it requires an explicit pair, because a
// "custom" tier with no numbers is meaningless.
var tokenBankTierDefaults = map[string]struct {
	Multiplier float64
	ArrayID    string
}{
	"low":  {0.5, "token_bank_low"},
	"mid":  {1.0, "token_bank_mid"},
	"high": {2.0, "token_bank_high"},
}

func (h *SkillMarketHandlers) TokenBankAdminSetModelTier(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	modelName := strings.TrimSpace(r.PathValue("model"))
	if shareID == "" || modelName == "" {
		tbError(w, http.StatusBadRequest, "invalid_share", "share id and model name are required")
		return
	}
	var req tokenBankTierRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", "tier payload is not valid JSON")
		return
	}
	tier := strings.ToLower(strings.TrimSpace(req.Tier))

	multiplier := 0.0
	arrayID := strings.TrimSpace(req.ArrayID)
	if defaults, ok := tokenBankTierDefaults[tier]; ok {
		multiplier, arrayID = defaults.Multiplier, defaults.ArrayID
	} else if tier == "custom" {
		if req.TierMultiplier == nil || *req.TierMultiplier <= 0 {
			tbError(w, http.StatusBadRequest, "invalid_tier", "custom tier requires a positive tier_multiplier")
			return
		}
		multiplier = *req.TierMultiplier
		if arrayID == "" {
			arrayID = "token_bank_mid"
		}
	} else {
		tbError(w, http.StatusBadRequest, "invalid_tier", "tier must be low, mid, high or custom")
		return
	}
	// An explicit multiplier/array may override the default, but only with a
	// sanity-checked value.
	if req.TierMultiplier != nil {
		if *req.TierMultiplier <= 0 || math.IsNaN(*req.TierMultiplier) || math.IsInf(*req.TierMultiplier, 0) {
			tbError(w, http.StatusBadRequest, "invalid_tier", "tier_multiplier must be a finite positive number")
			return
		}
		multiplier = *req.TierMultiplier
	}
	if arrayID == "" {
		tbError(w, http.StatusBadRequest, "invalid_tier", "array_id must not be empty")
		return
	}

	if err := repo.SetModelTier(r.Context(), shareID, modelName, tier, multiplier, arrayID, time.Now().UTC()); err != nil {
		if errors.Is(err, sqlite.ErrTokenBankShareNotFound) {
			tbError(w, http.StatusNotFound, "model_not_found", "share or model not found")
			return
		}
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	// Settlement reads TokenBankTierMultiplier from the registry member. The
	// database row alone does not change what this share is paid.
	if h.tokenBankPublisher() != nil {
		share, err := repo.LoadShare(r.Context(), shareID, "")
		if err != nil {
			tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
			return
		}
		if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
			tbError(w, http.StatusInternalServerError, "publish_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"share_id":        shareID,
		"model_name":      modelName,
		"tier":            tier,
		"tier_multiplier": multiplier,
		"array_id":        arrayID,
	})
}

// --- price book ---

func (h *SkillMarketHandlers) TokenBankAdminListPriceBook(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	rules, err := repo.ListPriceRules(r.Context())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": rules})
}

// tokenBankPriceRuleRequest is the wire form. Pointers distinguish "leave it
// alone" from "set it to zero" on update: zero is a legitimate price (a free
// model), so its absence cannot mean "clear it".
type tokenBankPriceRuleRequest struct {
	ModelPattern         string   `json:"model_pattern"`
	UnitInputPer10K      *float64 `json:"unit_input_credits_per_10k"`
	UnitOutputPer10K     *float64 `json:"unit_output_credits_per_10k"`
	UnitCachedReadPer10K *float64 `json:"unit_cached_read_credits_per_10k"`
	UnitCacheWritePer10K *float64 `json:"unit_cache_write_credits_per_10k"`
}

func (h *SkillMarketHandlers) TokenBankAdminUpsertPriceRule(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	var req tokenBankPriceRuleRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", "price rule payload is not valid JSON")
		return
	}
	pattern := strings.TrimSpace(req.ModelPattern)
	if pattern == "" {
		tbError(w, http.StatusBadRequest, "invalid_price_rule", "model_pattern is required")
		return
	}
	// A mid-pattern wildcard is not a matcher we have: resolution only honours a
	// trailing `*`. Rejecting it here is better than accepting `a*b` and quietly
	// pricing nothing. `?` is rejected for the same reason — tokenBankWildcardPrefix
	// refuses it, so a stored `gpt-4o?` would never match any model.
	if strings.ContainsAny(pattern, "?") {
		tbError(w, http.StatusBadRequest, "invalid_price_rule", "the ? wildcard is not supported; use a trailing *")
		return
	}
	if strings.Count(pattern, "*") > 1 || (strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, "*")) {
		tbError(w, http.StatusBadRequest, "invalid_price_rule", "only a single trailing * wildcard is supported")
		return
	}
	// A missing unit is not a zero. Zero is a real price (a free model), so the
	// only way to say "leave the stored unit alone" is to omit the field. A new
	// pattern has nothing stored, and a missing unit there is 0.
	rule := sqlite.TokenBankPriceRule{ModelPattern: pattern}
	if req.UnitInputPer10K == nil || req.UnitOutputPer10K == nil ||
		req.UnitCachedReadPer10K == nil || req.UnitCacheWritePer10K == nil {
		rules, err := repo.ListPriceRules(r.Context())
		if err != nil {
			tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
			return
		}
		for i := range rules {
			if rules[i].ModelPattern == pattern {
				rule = rules[i]
				rule.ModelPattern = pattern
				break
			}
		}
	}
	rule.UnitInputPer10K = tokenBankFloatOrDefault(req.UnitInputPer10K, rule.UnitInputPer10K)
	rule.UnitOutputPer10K = tokenBankFloatOrDefault(req.UnitOutputPer10K, rule.UnitOutputPer10K)
	rule.UnitCachedReadPer10K = tokenBankFloatOrDefault(req.UnitCachedReadPer10K, rule.UnitCachedReadPer10K)
	rule.UnitCacheWritePer10K = tokenBankFloatOrDefault(req.UnitCacheWritePer10K, rule.UnitCacheWritePer10K)
	for _, v := range []float64{rule.UnitInputPer10K, rule.UnitOutputPer10K, rule.UnitCachedReadPer10K, rule.UnitCacheWritePer10K} {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			tbError(w, http.StatusBadRequest, "invalid_price_rule", "price units must be finite and non-negative")
			return
		}
	}
	saved, err := repo.UpsertPriceRule(r.Context(), rule, time.Now().UTC())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rule": saved})
}

func (h *SkillMarketHandlers) TokenBankAdminDeletePriceRule(w http.ResponseWriter, r *http.Request) {
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	// The pattern may be a path segment, a query parameter, or the rule id;
	// accept all three so the GUI and a curl script both work.
	key := strings.TrimSpace(r.PathValue("id"))
	if key == "" {
		key = strings.TrimSpace(r.URL.Query().Get("pattern"))
	}
	if key == "" {
		tbError(w, http.StatusBadRequest, "invalid_price_rule", "rule id or pattern is required")
		return
	}
	ok, err := repo.DeletePriceRule(r.Context(), key)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	if !ok {
		tbError(w, http.StatusNotFound, "price_rule_not_found", "price rule not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": key})
}

// --- GET /api/admin/token-bank/credit-shares ---

// tokenBankCreditSharePayload is the audit row. Both sides of the transfer are
// shown masked: the admin investigating abuse needs to correlate, not to
// harvest two addresses at once.
type tokenBankCreditSharePayload struct {
	ID               string `json:"id"`
	Code             string `json:"code"`
	SenderEmail      string `json:"sender_email_masked"`
	ClaimedByEmail   string `json:"claimed_by_email_masked"`
	CreditsMicro     int64  `json:"credits_micro"`
	Status           string `json:"status"`
	OriginNodeID     string `json:"origin_node_id"`
	CreatedAt        string `json:"created_at"`
	ExpiresAt        string `json:"expires_at"`
	ClaimedAt        string `json:"claimed_at"`
	RevokedAt        string `json:"revoked_at"`
	Revocable        bool   `json:"revocable"`
	RemainingSeconds int64  `json:"remaining_seconds"`
}

func (h *SkillMarketHandlers) TokenBankAdminListCreditShares(w http.ResponseWriter, r *http.Request) {
	// Admin sees the same local-only rows as the user; listing elsewhere shows
	// an audit page with nothing in it.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	sender := strings.TrimSpace(r.URL.Query().Get("sender_user_id"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	links, err := repo.ListGiftLinks(r.Context(), sender, status, tokenBankListLimit(r))
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	now := time.Now().UTC()
	out := make([]tokenBankCreditSharePayload, 0, len(links))
	for _, link := range links {
		out = append(out, tokenBankCreditSharePayload{
			ID:               link.ID,
			Code:             link.Code,
			SenderEmail:      maskEmail(link.SenderEmail),
			ClaimedByEmail:   maskEmail(link.ClaimedByEmail),
			CreditsMicro:     link.CreditsMicro,
			Status:           link.Status,
			OriginNodeID:     link.OriginNodeID,
			CreatedAt:        formatOptionalTime(link.CreatedAt),
			ExpiresAt:        formatOptionalTime(link.ExpiresAt),
			ClaimedAt:        formatOptionalTime(link.ClaimedAt),
			RevokedAt:        formatOptionalTime(link.RevokedAt),
			Revocable:        link.Status == sqlite.TokenBankGiftStatusActive,
			RemainingSeconds: remainingSeconds(link.ExpiresAt, now),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "credit_shares": out})
}

// --- POST /api/admin/token-bank/credit-shares/{id}/revoke ---

func (h *SkillMarketHandlers) TokenBankAdminRevokeCreditShare(w http.ResponseWriter, r *http.Request) {
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	repo := h.tokenBankAdminRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	linkID := strings.TrimSpace(r.PathValue("id"))
	if linkID == "" {
		tbError(w, http.StatusBadRequest, "invalid_link", "link id is required")
		return
	}
	// Empty sender = admin revocation, no ownership check. The unfreeze always
	// returns to the sender recorded on the row (see RevokeGiftLink), so this
	// cannot be used to move credits anywhere.
	if err := repo.RevokeGiftLink(r.Context(), linkID, "", time.Now().UTC()); err != nil {
		switch {
		case errors.Is(err, sqlite.ErrGiftLinkNotFound):
			tbError(w, http.StatusNotFound, "link_not_found", "credit share link not found")
		case errors.Is(err, sqlite.ErrGiftLinkNotActive):
			tbError(w, http.StatusConflict, "link_not_active", "link is no longer active; a claimed link belongs to the receiver")
		default:
			tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": linkID})
}

// --- helpers ---

// tokenBankAdminPage reads limit/offset. The page size is capped at 200 so a
// single admin request cannot scan an unbounded number of shares and models.
func tokenBankAdminPage(r *http.Request) (limit, offset int) {
	const defaultLimit = 20
	const maxLimit = 200
	limit = defaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

func tokenBankFloatOrDefault(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}
