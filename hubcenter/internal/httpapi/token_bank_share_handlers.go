package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// Token Bank share management (§6.1). These endpoints are the only way a share
// enters the system, and they are also where a share becomes *reachable*: the
// store row is a record, and PublishTokenBankShare turns it into registry
// members that dispatch can actually select.
//
// Two rules apply throughout:
//
//   - The acting user always comes from the session, never the body. These
//     endpoints decide who owns a credential; a body-supplied owner would let
//     one account publish another's key.
//   - The plaintext upstream key exists only inside this file's submit path.
//     It is decrypted, handed to the registry, and never written to the store,
//     never logged, and never returned by any handler.

// tokenBankShareRepoView is the share/model slice of the ledger repository.
type tokenBankShareRepoView interface {
	CreateShare(ctx context.Context, share sqlite.TokenBankShare, models []sqlite.TokenBankShareModel, maxShares int, now time.Time) (*sqlite.TokenBankShare, bool, error)
	ListShares(ctx context.Context, ownerUserID, status string, limit, offset int) ([]sqlite.TokenBankShare, error)
	LoadShare(ctx context.Context, shareID, scopeOwner string) (*sqlite.TokenBankShare, error)
	ListModels(ctx context.Context, shareID string) ([]sqlite.TokenBankShareModel, error)
	SetSharePaused(ctx context.Context, shareID, scopeOwner, reason string, paused bool, now time.Time) error
	TakeOutShare(ctx context.Context, shareID, scopeOwner string) (bool, error)
	RenameShareKey(ctx context.Context, shareID, scopeOwner, encryptedKey, fingerprint, apiURL, protocol string, now time.Time) error
	SyncShareModels(ctx context.Context, shareID, scopeOwner string, models []sqlite.TokenBankShareModel, now time.Time) (int, error)
	ListPriceRules(ctx context.Context) ([]sqlite.TokenBankPriceRule, error)
	ResolvePrice(ctx context.Context, modelName string) (sqlite.TokenBankPriceRule, bool, error)
	SumUsageBucketsByShare(ctx context.Context, ownerID string, now time.Time) (map[string]sqlite.TokenBankUsageBuckets, error)
	SumUsageBucketsByModel(ctx context.Context, ownerID, shareID string, now time.Time) (map[string]sqlite.TokenBankUsageBuckets, error)
}

// tokenBankPublishView is the registry half: publishing a share's models,
// pausing them, rotating their key and withdrawing them. Declared here (rather
// than taking *llmservice.Service) so the handler tests can drive it with a
// stub and so a deployment without the registry degrades to a clear 503.
type tokenBankPublishView interface {
	PublishTokenBankShare(ctx context.Context, specs []llmservice.TokenBankPublishSpec) (llmservice.TokenBankPublishResult, error)
	UnpublishTokenBankShare(ctx context.Context, shareID string) ([]string, error)
	SetTokenBankSharePaused(ctx context.Context, shareID string, paused bool) (int, error)
	SetTokenBankMemberKey(ctx context.Context, shareID, apiURL, apiKey, protocol string) (int, error)
	TokenBankShareMemberIDs(ctx context.Context, shareID string) ([]string, error)
}

func (h *SkillMarketHandlers) tokenBankShareRepo() tokenBankShareRepoView {
	if h == nil || h.tokenBank == nil {
		return nil
	}
	view, _ := h.tokenBank.(tokenBankShareRepoView)
	return view
}

// tokenBankProviderDenied matches the API host against the denylist. A listed
// host also blocks its subdomains.
func tokenBankProviderDenied(apiURL string, denylist []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	for _, item := range denylist {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if host == item || strings.HasSuffix(host, "."+item) {
			return true
		}
	}
	return false
}

func (h *SkillMarketHandlers) tokenBankPublisher() tokenBankPublishView {
	if h == nil || h.tokenBankLLM == nil {
		return nil
	}
	return h.tokenBankLLM
}

// --- POST /api/v1/token-bank/shares ---

// tokenBankShareSubmit is the client payload. The API key is NOT here: the
// client encrypts an envelope with the HubCenter public key, and the plaintext
// key never appears in a field the server logs or echoes.
type tokenBankShareSubmit struct {
	ClientInstanceID string `json:"client_instance_id"`
	DisplayName      string `json:"display_name"`
	APIURL           string `json:"api_url"`
	Protocol         string `json:"protocol"`
	ServiceGroupID   string `json:"service_group_id"`
	// KeyFingerprint is the client's masked identifier for the key. It is part
	// of the idempotency key, so it must be stable across retries of the same
	// submission and different for a genuinely different key.
	KeyFingerprint string `json:"key_fingerprint"`
	// Models is the selected model list. Only the models the client marked
	// available are published; an unavailable model is still recorded so the
	// owner sees why it is missing.
	Models []tokenBankShareSubmitModel `json:"models"`
	// EncryptedPayload is the RSA-wrapped envelope carrying the plaintext key.
	EncryptedPayload string `json:"encrypted_payload"`
	MaxInputTokens   int64  `json:"max_input_tokens_per_request"`
	MaxOutputTokens  int64  `json:"max_output_tokens_per_request"`
	// Visibility is public or private. Private requires Audiences.
	Visibility string                     `json:"visibility"`
	Audiences  []sqlite.TokenBankAudience `json:"audiences"`
}

type tokenBankShareSubmitModel struct {
	Model        string `json:"model"`
	Available    bool   `json:"available"`
	ProbeError   string `json:"probe_error"`
	InputTokens  int64  `json:"used_input_tokens"`
	OutputTokens int64  `json:"used_output_tokens"`
	// ShareWindow is when this model may be dialed. Nil means always.
	ShareWindow *llmpool.TokenBankShareWindow `json:"share_window"`
}

// tokenBankKeyEnvelope is the decrypted payload. Field names match the client
// envelope so a change on either side is a compile-time mismatch on the
// struct, not a silent empty string that reads as "no key".
type tokenBankKeyEnvelope struct {
	APIKey   string `json:"api_key"`
	APIURL   string `json:"api_url"`
	Protocol string `json:"protocol"`
}

func (h *SkillMarketHandlers) TokenBankCreateShare(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankShareRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	publisher := h.tokenBankPublisher()
	if publisher == nil {
		// Without the registry half the share would be recorded but never
		// reachable. Refusing is the honest answer: a share that silently does
		// nothing is worse than a share that fails loudly.
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank routing is not available on this node")
		return
	}

	var body tokenBankShareSubmit
	if err := decodeLimitedJSON(w, r, &body, largeJSONBodyLimit); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	displayName := strings.TrimSpace(body.DisplayName)
	apiURL := strings.TrimSpace(body.APIURL)
	fingerprint := strings.TrimSpace(body.KeyFingerprint)
	if displayName == "" || apiURL == "" || fingerprint == "" {
		tbError(w, http.StatusBadRequest, "invalid_body", "display_name, api_url and key_fingerprint are required")
		return
	}
	if len(body.Models) == 0 {
		tbError(w, http.StatusBadRequest, "invalid_body", "at least one model is required")
		return
	}

	// Identity gate (§10 P0-8): an unverified account may not publish a
	// credential. Checked before decryption so an unverified caller cannot even
	// exercise the key path.
	settings, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	if settings.RequireVerifiedIdentity && !strings.EqualFold(strings.TrimSpace(user.Status), "verified") {
		tbError(w, http.StatusForbidden, "identity_not_verified", "verify your account before sharing a provider")
		return
	}
	if tokenBankProviderDenied(apiURL, settings.ProviderDenylist) {
		tbError(w, http.StatusForbidden, "provider_denied", "this provider is not allowed in the Token Bank")
		return
	}

	plainKey, err := h.decryptTokenBankKey(body.EncryptedPayload)
	if err != nil {
		tbError(w, http.StatusBadRequest, "decrypt_failed", "the encrypted payload could not be read")
		return
	}
	if strings.TrimSpace(plainKey) == "" {
		tbError(w, http.StatusBadRequest, "invalid_body", "the encrypted payload contained no api key")
		return
	}

	audiences, err := sqlite.NormalizeTokenBankAudiences(body.Visibility, body.Audiences)
	if err != nil {
		tbError(w, http.StatusBadRequest, "invalid_audience", err.Error())
		return
	}
	visibility, err := sqlite.NormalizeTokenBankVisibility(body.Visibility)
	if err != nil {
		tbError(w, http.StatusBadRequest, "invalid_visibility", err.Error())
		return
	}
	audienceJSON, err := sqlite.MarshalTokenBankAudiences(audiences)
	if err != nil {
		tbError(w, http.StatusBadRequest, "invalid_audience", err.Error())
		return
	}

	now := time.Now().UTC()
	canaryUntil := llmservice.TokenBankCanaryDeadlineAfter(now, settings.CanaryWindow())
	shareID := newTokenBankShareID()
	models := make([]sqlite.TokenBankShareModel, 0, len(body.Models))
	for _, m := range body.Models {
		name := strings.TrimSpace(m.Model)
		if name == "" {
			continue
		}
		windowJSON, windowErr := llmpool.MarshalTokenBankShareWindow(m.ShareWindow)
		if windowErr != nil {
			tbError(w, http.StatusBadRequest, "invalid_window", windowErr.Error())
			return
		}
		// array_id/tier start at the mid tier. Grading is a separate admin
		// action (§3.2: an admin regrades after the share is live), so a
		// submit never chooses its own settlement rate.
		models = append(models, sqlite.TokenBankShareModel{
			ID:               newTokenBankModelID(),
			ModelName:        name,
			MemberID:         llmservice.TokenBankMemberID(shareID, name),
			ArrayID:          llmservice.TokenBankArrayMid,
			Tier:             llmservice.TokenBankTierMid,
			TierMultiplier:   1,
			Enabled:          true,
			Available:        m.Available,
			LastProbeError:   strings.TrimSpace(m.ProbeError),
			UsedInputTokens:  m.InputTokens,
			UsedOutputTokens: m.OutputTokens,
			CanaryUntil:      canaryUntil,
			ShareWindowJSON:  windowJSON,
		})
	}
	if len(models) == 0 {
		tbError(w, http.StatusBadRequest, "invalid_body", "at least one named model is required")
		return
	}
	// Extra keys are not in the primary unique index. Reject them here so a
	// second share cannot reuse a key that already rotates on another share.
	// A matching primary is left to CreateShare, which replays that share.
	if p2 := h.tokenBankP2(); p2 != nil {
		taken, takenErr := p2.ExtraKeyFingerprintTaken(r.Context(), user.ID, fingerprint)
		if takenErr != nil {
			tbError(w, http.StatusInternalServerError, "token_bank_unavailable", takenErr.Error())
			return
		}
		if taken {
			tbError(w, http.StatusConflict, "duplicate_key", "this api key is already shared")
			return
		}
	}
	// The encrypted key is stored, not the plaintext one. The client's
	// envelope is opaque to this layer by design.
	share := sqlite.TokenBankShare{
		ID:                        shareID,
		OwnerUserID:               user.ID,
		OwnerEmail:                user.Email,
		DisplayName:               displayName,
		APIURL:                    apiURL,
		Protocol:                  firstNonEmpty(strings.TrimSpace(body.Protocol), "openai"),
		EncryptedKey:              strings.TrimSpace(body.EncryptedPayload),
		KeyFingerprint:            fingerprint,
		Status:                    sqlite.TokenBankShareStatusActive,
		Visibility:                visibility,
		AudienceJSON:              audienceJSON,
		ServiceGroupID:            strings.TrimSpace(body.ServiceGroupID),
		MaxInputTokensPerRequest:  body.MaxInputTokens,
		MaxOutputTokensPerRequest: body.MaxOutputTokens,
	}
	stored, created, err := repo.CreateShare(r.Context(), share, models, settings.MaxSharesPerUser, now)
	if err != nil {
		switch {
		case errors.Is(err, sqlite.ErrTokenBankShareLimitReached):
			tbError(w, http.StatusConflict, "share_limit_reached", "you have reached the maximum number of shares")
		case errors.Is(err, sqlite.ErrTokenBankDuplicateKey):
			tbError(w, http.StatusConflict, "duplicate_key", "this api key is already shared")
		default:
			tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		}
		return
	}

	// Publish only the models the client found available. An unavailable model
	// is still recorded so the owner sees why it is absent, but publishing it
	// would put a member in the dispatch pool that is known to fail.
	//
	// An idempotent replay must publish the stored share, not the retry body.
	// The retry can name fewer models and has no extra keys; publishing that
	// would take live members offline and drop keys the owner already added.
	responseModels := models
	if !created {
		if err := h.republishTokenBankShare(r.Context(), stored.ID); err != nil {
			tbError(w, http.StatusInternalServerError, "publish_failed", fmt.Sprintf("share %s was saved but could not be published: %v", stored.ID, err))
			return
		}
		responseModels, err = repo.ListModels(r.Context(), stored.ID)
		if err != nil {
			tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
			return
		}
	} else if err := h.publishTokenBankSpecs(r.Context(), tokenBankPublishSpecs(*stored, models, plainKey, nil)); err != nil {
		// The share row exists but is unreachable. Report the failure with the
		// id so the owner can retry against the same share instead of creating
		// a duplicate key entry.
		tbError(w, http.StatusInternalServerError, "publish_failed", fmt.Sprintf("share %s was saved but could not be published: %v", stored.ID, err))
		return
	}

	status := http.StatusCreated
	if !created {
		// Idempotent replay: the same key was already shared. 200 tells the
		// client it did not create anything new.
		status = http.StatusOK
	}
	writeJSON(w, status, tokenBankClientSharePayload(*stored, responseModels, true))
}

// --- GET /api/v1/token-bank/shares ---

func (h *SkillMarketHandlers) TokenBankListShares(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankShareRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	rangeName, ok := sqlite.CanonicalTokenBankRange(r.URL.Query().Get("range"))
	if !ok {
		tbError(w, http.StatusBadRequest, "invalid_range", "range must be today, month, or all")
		return
	}
	shares, err := repo.ListShares(r.Context(), user.ID, strings.TrimSpace(r.URL.Query().Get("status")), tokenBankListLimit(r), 0)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	buckets, err := repo.SumUsageBucketsByShare(r.Context(), user.ID, time.Now())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(shares))
	for _, share := range shares {
		// The list view omits models: a card that shows status and totals is
		// what the GUI needs, and the per-model detail is its own endpoint.
		item := tokenBankClientSharePayload(share, nil, false)
		tokenBankApplyShareRange(item, buckets[share.ID], rangeName)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "range": rangeName, "shares": items})
}

// --- GET /api/v1/token-bank/shares/{id}/models ---

func (h *SkillMarketHandlers) TokenBankListShareModels(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankShareRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	rangeName, ok := sqlite.CanonicalTokenBankRange(r.URL.Query().Get("range"))
	if !ok {
		tbError(w, http.StatusBadRequest, "invalid_range", "range must be today, month, or all")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	// Scoped to the caller: a share id from another account must read as
	// "not found", not as somebody else's model list.
	share, err := repo.LoadShare(r.Context(), shareID, user.ID)
	if err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	models, err := repo.ListModels(r.Context(), share.ID)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	buckets, err := repo.SumUsageBucketsByModel(r.Context(), user.ID, share.ID, time.Now())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	payloads := tokenBankModelPayloads(models)
	known := make(map[string]bool, len(models))
	for i, model := range models {
		key := strings.ToLower(strings.TrimSpace(model.ModelName))
		known[key] = true
		payloads[i]["usage"] = tokenBankUsagePayload(buckets[key].Pick(rangeName))
	}
	// A model that was settled and then removed from the share still earned
	// credits. It is not part of `models`: the share chip treats that list as
	// "already shared" and would hide the name from a fresh submit.
	orphans := make([]map[string]any, 0)
	names := make([]string, 0, len(buckets))
	for name := range buckets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if known[name] {
			continue
		}
		selected := buckets[name].Pick(rangeName)
		if selected.Calls == 0 {
			continue
		}
		label := buckets[name].Label
		if label == "" {
			label = name
		}
		orphans = append(orphans, map[string]any{
			"model_name": label,
			"available":  false,
			"enabled":    false,
			"usage":      tokenBankUsagePayload(selected),
		})
	}
	sharePayload := tokenBankClientSharePayload(*share, nil, false)
	tokenBankApplyShareRange(sharePayload, sqlite.CombineUsageBuckets(buckets), rangeName)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"range":        rangeName,
		"share":        sharePayload,
		"models":       payloads,
		"usage_models": orphans,
		"pricing":      h.tokenBankPricingHints(r.Context(), models),
	})
}

// --- PUT /api/v1/token-bank/shares/{id}/models ---

// TokenBankSyncShareModels replaces the models a live share publishes.
//
// CreateShare is idempotent on the key fingerprint and will not change an
// existing model list. This is the owner's add/remove path: the client probes
// the provider, the user ticks a different set, and dispatch has to follow.
// Models left off the list are disabled, not deleted, so their earnings stay
// on the row. A model that was already graded keeps its tier and member id.
func (h *SkillMarketHandlers) TokenBankSyncShareModels(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankShareRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	publisher := h.tokenBankPublisher()
	if publisher == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank routing is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		Models []tokenBankShareSubmitModel `json:"models"`
	}
	if err := decodeLimitedJSON(w, r, &body, largeJSONBodyLimit); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	share, err := repo.LoadShare(r.Context(), shareID, user.ID)
	if err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	if share.Status == sqlite.TokenBankShareStatusRevoked {
		tbError(w, http.StatusConflict, "share_revoked", "this share has been taken out")
		return
	}
	existing, err := repo.ListModels(r.Context(), share.ID)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	settings, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	now := time.Now().UTC()
	models, err := buildTokenBankSyncModels(share.ID, existing, body.Models, now, settings.CanaryWindow())
	if err != nil {
		code := "invalid_body"
		if strings.Contains(err.Error(), "share window") {
			code = "invalid_window"
		}
		tbError(w, http.StatusBadRequest, code, err.Error())
		return
	}
	if _, err := repo.SyncShareModels(r.Context(), share.ID, user.ID, models, now); err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
		tbError(w, http.StatusInternalServerError, "publish_failed", fmt.Sprintf("share %s was saved but could not be published: %v", share.ID, err))
		return
	}
	fresh, err := repo.LoadShare(r.Context(), share.ID, user.ID)
	if err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	listed, err := repo.ListModels(r.Context(), share.ID)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tokenBankClientSharePayload(*fresh, listed, true))
}

// buildTokenBankSyncModels turns the client's probed selection into store rows.
//
// A name that already belongs to the share keeps that row's id, member, tier,
// and canary deadline. Re-submitting the list must not move a graded model
// back to mid, and must not mint a new member id that would drop the live one.
// A new name starts at mid, the same grade a first share uses.
func buildTokenBankSyncModels(shareID string, existing []sqlite.TokenBankShareModel, submitted []tokenBankShareSubmitModel, now time.Time, canaryWindow time.Duration) ([]sqlite.TokenBankShareModel, error) {
	byName := make(map[string]sqlite.TokenBankShareModel, len(existing))
	for _, model := range existing {
		key := strings.ToLower(strings.TrimSpace(model.ModelName))
		if key == "" {
			continue
		}
		byName[key] = model
	}
	seen := make(map[string]struct{}, len(submitted))
	out := make([]sqlite.TokenBankShareModel, 0, len(submitted))
	canary := llmservice.TokenBankCanaryDeadlineAfter(now, canaryWindow)
	for _, item := range submitted {
		name := strings.TrimSpace(item.Model)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("token bank model %q listed twice", name)
		}
		seen[key] = struct{}{}
		windowJSON, err := llmpool.MarshalTokenBankShareWindow(item.ShareWindow)
		if err != nil {
			return nil, err
		}
		row := sqlite.TokenBankShareModel{
			ModelName:       name,
			ArrayID:         llmservice.TokenBankArrayMid,
			Tier:            llmservice.TokenBankTierMid,
			TierMultiplier:  1,
			Enabled:         true,
			Available:       item.Available,
			LastProbeError:  strings.TrimSpace(item.ProbeError),
			CanaryUntil:     canary,
			ShareWindowJSON: windowJSON,
		}
		if prior, ok := byName[key]; ok {
			row.ID = prior.ID
			row.ModelName = prior.ModelName
			row.MemberID = prior.MemberID
			row.ArrayID = prior.ArrayID
			row.Tier = prior.Tier
			row.TierMultiplier = prior.TierMultiplier
			row.CanaryUntil = prior.CanaryUntil
		} else {
			row.ID = newTokenBankModelID()
			row.MemberID = llmservice.TokenBankMemberID(shareID, row.ModelName)
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one named model is required")
	}
	return out, nil
}

// --- PUT /api/v1/token-bank/shares/{id}/paused ---

func (h *SkillMarketHandlers) TokenBankSetSharePaused(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankShareRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		Paused bool   `json:"paused"`
		Reason string `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &body, defaultJSONBodyLimit); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	// The store scopes by owner, so a foreign share id reports not-found rather
	// than pausing somebody else's provider.
	if _, err := repo.LoadShare(r.Context(), shareID, user.ID); err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	// Registry first, under the same lock as republish. A republish that
	// already read the old status cannot put the member back after this
	// returns. A registry failure leaves the stored status unchanged, and a
	// store failure puts the registry back, so the two cannot stay split.
	var publishErr, storeErr error
	_ = withTokenBankRouteLock(func() error {
		publishErr, storeErr = h.tokenBankCommitSharePaused(r.Context(), repo, shareID, user.ID, strings.TrimSpace(body.Reason), body.Paused, time.Now().UTC())
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
		h.writeTokenBankShareLookupError(w, storeErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": shareID, "paused": body.Paused})
}

// tokenBankPauseStore is the share row half of a pause or resume. Both the
// user repository and the admin repository satisfy it.
type tokenBankPauseStore interface {
	LoadShare(ctx context.Context, shareID, scopeOwner string) (*sqlite.TokenBankShare, error)
	SetSharePaused(ctx context.Context, shareID, scopeOwner, reason string, paused bool, now time.Time) error
}

// tokenBankCommitSharePaused applies a pause or resume. The caller already
// holds the route lock. SetTokenBankSharePaused does not take that lock.
//
// The status used for a revert is read inside this call. A status read before
// the lock can be stale: another pause may have committed while this request
// waited. The registry is updated first. If the store write then fails, the
// registry goes back to that status. Republish of an active share keeps an
// existing registry pause, so leaving the member paused while the row stays
// active would stop traffic until a later pause succeeded.
func (h *SkillMarketHandlers) tokenBankCommitSharePaused(ctx context.Context, repo tokenBankPauseStore, shareID, scopeOwner, reason string, paused bool, now time.Time) (publishErr, storeErr error) {
	fresh, err := repo.LoadShare(ctx, shareID, scopeOwner)
	if err != nil {
		return nil, err
	}
	oldPaused := fresh.Status == sqlite.TokenBankShareStatusPaused
	publisher := h.tokenBankPublisher()
	if publisher != nil {
		if _, err := publisher.SetTokenBankSharePaused(ctx, shareID, paused); err != nil {
			return err, nil
		}
	}
	if err := repo.SetSharePaused(ctx, shareID, scopeOwner, reason, paused, now); err != nil {
		if publisher != nil {
			if _, revErr := publisher.SetTokenBankSharePaused(ctx, shareID, oldPaused); revErr != nil {
				err = fmt.Errorf("%w (registry revert: %v)", err, revErr)
			}
		}
		return nil, err
	}
	return nil, nil
}

// --- DELETE /api/v1/token-bank/shares/{id} ---

func (h *SkillMarketHandlers) TokenBankTakeOutShare(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankShareRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))

	// Confirm ownership before touching the registry. Doing the store delete
	// first would leave the members published if the registry call then failed,
	// and the reverse order would unpublish a share that the store rejects.
	if _, err := repo.LoadShare(r.Context(), shareID, user.ID); err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	// Hold the route lock across unpublish and delete. A visibility or key
	// republish that already read the row would otherwise publish the members
	// back after they were removed.
	var publishErr error
	var storeErr error
	var removed bool
	// The lock's return value is dropped on purpose: it carries whichever of
	// the two steps failed, and the two are reported differently below. Keeping
	// them in separate variables is what lets the caller tell an unpublish
	// failure from a delete failure — a single variable would hold the publish
	// error while being named as the store's.
	_ = withTokenBankRouteLock(func() error {
		if publisher := h.tokenBankPublisher(); publisher != nil {
			if _, err := publisher.UnpublishTokenBankShare(r.Context(), shareID); err != nil {
				publishErr = err
				return err
			}
		}
		var err error
		removed, err = repo.TakeOutShare(r.Context(), shareID, user.ID)
		storeErr = err
		return err
	})
	if publishErr != nil {
		tbError(w, http.StatusInternalServerError, "publish_failed", publishErr.Error())
		return
	}
	if storeErr != nil {
		h.writeTokenBankShareLookupError(w, storeErr)
		return
	}
	if !removed {
		tbError(w, http.StatusNotFound, "share_not_found", "share not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": shareID, "taken_out": true})
}

// --- PUT /api/v1/token-bank/shares/{id}/key ---

func (h *SkillMarketHandlers) TokenBankRotateShareKey(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankShareRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		EncryptedPayload string `json:"encrypted_payload"`
		KeyFingerprint   string `json:"key_fingerprint"`
		APIURL           string `json:"api_url"`
		Protocol         string `json:"protocol"`
	}
	if err := decodeLimitedJSON(w, r, &body, largeJSONBodyLimit); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	fingerprint := strings.TrimSpace(body.KeyFingerprint)
	if fingerprint == "" {
		tbError(w, http.StatusBadRequest, "invalid_body", "key_fingerprint is required")
		return
	}
	plainKey, err := h.decryptTokenBankKey(body.EncryptedPayload)
	if err != nil || strings.TrimSpace(plainKey) == "" {
		tbError(w, http.StatusBadRequest, "decrypt_failed", "the encrypted payload could not be read")
		return
	}
	// The share must exist and belong to the caller before anything is written.
	share, err := repo.LoadShare(r.Context(), shareID, user.ID)
	if err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	apiURL := strings.TrimSpace(body.APIURL)
	protocol := strings.TrimSpace(body.Protocol)
	// exceptShare skips this share's current primary, so submitting the same
	// primary again still works. An extra key, here or on another share, does not.
	if p2 := h.tokenBankP2(); p2 != nil {
		taken, takenErr := p2.OwnerKeyFingerprintTaken(r.Context(), share.OwnerUserID, fingerprint, share.ID)
		if takenErr != nil {
			tbError(w, http.StatusInternalServerError, "token_bank_unavailable", takenErr.Error())
			return
		}
		if taken {
			tbError(w, http.StatusConflict, "duplicate_key", "this api key is already shared")
			return
		}
	}
	if err := repo.RenameShareKey(r.Context(), share.ID, user.ID, strings.TrimSpace(body.EncryptedPayload), fingerprint, apiURL, protocol, time.Now().UTC()); err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	// Republish the saved row, including extra keys. Updating only the primary
	// on the registry races a republish that loaded the previous key and can
	// put that key back.
	if h.tokenBankPublisher() != nil {
		if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
			tbError(w, http.StatusInternalServerError, "publish_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": share.ID, "key_fingerprint": fingerprint})
}

// --- helpers ---

// tokenBankClientSharePayload renders a share for its owner. The encrypted key never
// appears: the owner already has the plaintext, and sending the envelope back
// would put a credential on the wire for no reason. has_key is the boolean the
// UI actually needs.
func tokenBankClientSharePayload(share sqlite.TokenBankShare, models []sqlite.TokenBankShareModel, includeModels bool) map[string]any {
	out := map[string]any{
		"id":                 share.ID,
		"display_name":       share.DisplayName,
		"api_url":            share.APIURL,
		"protocol":           share.Protocol,
		"status":             share.Status,
		"visibility":         share.Visibility,
		"service_group_id":   share.ServiceGroupID,
		"key_fingerprint":    share.KeyFingerprint,
		"has_key":            strings.TrimSpace(share.EncryptedKey) != "",
		"total_earned_micro": share.TotalEarnedMicro,
		"max_input_tokens":   share.MaxInputTokensPerRequest,
		"max_output_tokens":  share.MaxOutputTokensPerRequest,
		"last_error":         share.LastError,
		"paused_reason":      share.PausedReason,
		"created_at":         formatOptionalTime(share.CreatedAt),
		"updated_at":         formatOptionalTime(share.UpdatedAt),
		"audiences":          tokenBankAudiencePayload(share.AudienceJSON),
		"extra_key_count":    len(sqlite.ParseTokenBankExtraKeys(share.ExtraKeysJSON)),
	}
	if includeModels && models != nil {
		out["models"] = tokenBankModelPayloads(models)
	}
	return out
}

// tokenBankApplyShareRange attaches the three card windows and the figures for
// the selected one. total_earned_micro is the share-row lifetime counter
// settlement maintains. all_earned_micro is the usage sum, which is what the
// desktop cards show, including calls settled before that counter was written.
func tokenBankApplyShareRange(payload map[string]any, buckets sqlite.TokenBankUsageBuckets, rangeName string) {
	selected := buckets.Pick(rangeName)
	payload["stats_ready"] = true
	payload["today_earned_micro"] = buckets.Today.NetMicro
	payload["month_earned_micro"] = buckets.Month.NetMicro
	payload["all_earned_micro"] = buckets.All.NetMicro
	payload["range_earned_micro"] = selected.NetMicro
	payload["range_gross_micro"] = selected.GrossMicro
	payload["range_fee_micro"] = selected.FeeMicro
	payload["range_charged_micro"] = selected.ChargedMicro
	payload["range_tokens"] = selected.Tokens()
	payload["range_calls"] = selected.Calls
	payload["range_clamped_calls"] = selected.ClampedCalls
}

func tokenBankUsagePayload(totals sqlite.TokenBankUsageTotals) map[string]any {
	return map[string]any{
		"calls":               totals.Calls,
		"input_tokens":        totals.InputTokens,
		"output_tokens":       totals.OutputTokens,
		"cached_input_tokens": totals.CachedInputTokens,
		"cache_write_tokens":  totals.CacheWriteTokens,
		"gross_micro":         totals.GrossMicro,
		"fee_micro":           totals.FeeMicro,
		"net_micro":           totals.NetMicro,
		"charged_micro":       totals.ChargedMicro,
		"clamped_calls":       totals.ClampedCalls,
		"tokens":              totals.Tokens(),
	}
}

func tokenBankModelPayloads(models []sqlite.TokenBankShareModel) []map[string]any {
	out := make([]map[string]any, 0, len(models))
	for _, m := range models {
		item := map[string]any{
			"id":                 m.ID,
			"model_name":         m.ModelName,
			"member_id":          m.MemberID,
			"array_id":           m.ArrayID,
			"tier":               m.Tier,
			"tier_multiplier":    m.TierMultiplier,
			"enabled":            m.Enabled,
			"available":          m.Available,
			"last_probe_error":   m.LastProbeError,
			"used_input_tokens":  m.UsedInputTokens,
			"used_output_tokens": m.UsedOutputTokens,
			"earned_micro":       m.EarnedMicro,
		}
		if window := strings.TrimSpace(m.ShareWindowJSON); window != "" {
			item["share_window"] = json.RawMessage(window)
		}
		out = append(out, item)
	}
	return out
}

// tokenBankPricingHints resolves the platform unit price for each model, so the
// owner can see the rate their models settle at. A model with no price-book
// entry is reported with an empty rule rather than omitted: the absence is a
// fact the UI should be able to show.
func (h *SkillMarketHandlers) tokenBankPricingHints(ctx context.Context, models []sqlite.TokenBankShareModel) []map[string]any {
	repo := h.tokenBankShareRepo()
	out := make([]map[string]any, 0, len(models))
	for _, m := range models {
		item := map[string]any{"model_name": m.ModelName}
		if repo != nil {
			if rule, ok, err := repo.ResolvePrice(ctx, m.ModelName); err == nil && ok {
				item["price_book_id"] = rule.ID
				item["model_pattern"] = rule.ModelPattern
				item["unit_input_per_10k"] = rule.UnitInputPer10K
				item["unit_output_per_10k"] = rule.UnitOutputPer10K
				item["unit_cached_read_per_10k"] = rule.UnitCachedReadPer10K
				item["unit_cache_write_per_10k"] = rule.UnitCacheWritePer10K
			}
		}
		out = append(out, item)
	}
	return out
}

func (h *SkillMarketHandlers) writeTokenBankShareLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, sqlite.ErrTokenBankShareNotFound) {
		tbError(w, http.StatusNotFound, "share_not_found", "share not found")
		return
	}
	tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
}

// decryptTokenBankKey unwraps the client envelope. The RSA private key lives on
// the handler set and nowhere else, so a bug in the store layer cannot expose a
// plaintext upstream credential.
//
// The envelope is a JSON document (see tokenBankKeyEnvelope) rather than a bare
// ciphertext so the client can carry the api_url and protocol alongside the key
// without a second round trip; the whole document is what gets encrypted.
func (h *SkillMarketHandlers) decryptTokenBankKey(encrypted string) (string, error) {
	encrypted = strings.TrimSpace(encrypted)
	if encrypted == "" {
		return "", errors.New("no encrypted payload")
	}
	pkg := &skillmarket.EncryptedPackage{}
	if err := json.Unmarshal([]byte(encrypted), pkg); err != nil {
		return "", fmt.Errorf("decode envelope: %w", err)
	}
	if h == nil || h.rsaPrivKey == nil {
		return "", errors.New("decryption key unavailable")
	}
	plaintext, err := skillmarket.DecryptDownload(pkg, h.tokenBankEnvelopeUser(), h.rsaPrivKey)
	if err != nil {
		return "", err
	}
	// The payload is the JSON envelope. Falling back to treating the plaintext
	// as the key itself would accept a bare key, which is the shape that skips
	// the api_url binding — so a document that does not parse is an error.
	var envelope tokenBankKeyEnvelope
	if err := json.Unmarshal(plaintext, &envelope); err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}
	return strings.TrimSpace(envelope.APIKey), nil
}

// tokenBankEnvelopeUser is the PBKDF2 salt owner for Token Bank envelopes. It
// is a fixed domain separator, not a per-user secret: the confidentiality comes
// from the RSA layer, and a user-derived salt would make an envelope
// undecryptable after the account id changed for any reason.
const tokenBankEnvelopeSaltOwner = "token-bank-share"

func (h *SkillMarketHandlers) tokenBankEnvelopeUser() string {
	return tokenBankEnvelopeSaltOwner
}

func newTokenBankShareID() string {
	return "tbkshare_" + randomTokenBankID(12)
}

func newTokenBankModelID() string {
	return "tbkmodel_" + randomTokenBankID(12)
}

func randomTokenBankID(n int) string {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(sum[:n])
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
