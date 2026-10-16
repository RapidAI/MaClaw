package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// tokenBankP2Store is the store surface for private shares, extra keys, and
// the sharer leaderboard. The production repository implements it. A narrower
// test double leaves these endpoints at 503 and leaves the older API alone.
type tokenBankP2Store interface {
	LoadShare(ctx context.Context, shareID, scopeOwner string) (*sqlite.TokenBankShare, error)
	ListModels(ctx context.Context, shareID string) ([]sqlite.TokenBankShareModel, error)
	UpdateShareAccess(ctx context.Context, shareID, scopeOwner, visibility, audienceJSON string, now time.Time) error
	UpdateShareExtraKeys(ctx context.Context, shareID, scopeOwner, extraJSON string, now time.Time) error
	UpdateShareExtraKeysCAS(ctx context.Context, shareID, scopeOwner, previousJSON, extraJSON string, now time.Time) error
	OwnerKeyFingerprintTaken(ctx context.Context, ownerID, fingerprint, exceptShare string) (bool, error)
	ExtraKeyFingerprintTaken(ctx context.Context, ownerID, fingerprint string) (bool, error)
	Leaderboard(ctx context.Context, limit int) ([]sqlite.TokenBankLeaderboardRow, error)
	SharerStanding(ctx context.Context, ownerUserID string) (earnedMicro int64, rank int, err error)
}

func (h *SkillMarketHandlers) tokenBankP2() tokenBankP2Store {
	if h == nil || h.tokenBank == nil {
		return nil
	}
	view, _ := h.tokenBank.(tokenBankP2Store)
	return view
}

func tokenBankAudiencePayload(raw string) []sqlite.TokenBankAudience {
	items := sqlite.ParseTokenBankAudiences(raw)
	if items == nil {
		return []sqlite.TokenBankAudience{}
	}
	return items
}

func tokenBankPublishSpecs(share sqlite.TokenBankShare, models []sqlite.TokenBankShareModel, primary string, extras []string) []llmservice.TokenBankPublishSpec {
	specs := make([]llmservice.TokenBankPublishSpec, 0, len(models))
	audiences := tokenBankPublishAudiences(sqlite.ParseTokenBankAudiences(share.AudienceJSON))
	for _, m := range models {
		if !m.Available || !m.Enabled {
			continue
		}
		specs = append(specs, llmservice.TokenBankPublishSpec{
			ShareID:                   share.ID,
			OwnerUserID:               share.OwnerUserID,
			DisplayName:               share.DisplayName,
			Model:                     m.ModelName,
			ArrayID:                   m.ArrayID,
			APIURL:                    share.APIURL,
			APIKey:                    primary,
			Protocol:                  share.Protocol,
			ServiceGroupID:            share.ServiceGroupID,
			MaxInputTokensPerRequest:  share.MaxInputTokensPerRequest,
			MaxOutputTokensPerRequest: share.MaxOutputTokensPerRequest,
			Tier:                      m.Tier,
			TierMultiplier:            m.TierMultiplier,
			Visibility:                share.Visibility,
			Audiences:                 audiences,
			CanaryUntil:               m.CanaryUntil,
			ExtraKeys:                 extras,
			SharePaused:               share.Status == sqlite.TokenBankShareStatusPaused,
			ShareWindow:               llmpool.ParseTokenBankShareWindow(m.ShareWindowJSON),
		})
	}
	return specs
}

func tokenBankPublishAudiences(in []sqlite.TokenBankAudience) []llmpool.TokenBankAudience {
	if len(in) == 0 {
		return nil
	}
	out := make([]llmpool.TokenBankAudience, 0, len(in))
	for _, item := range in {
		out = append(out, llmpool.TokenBankAudience{HubID: item.HubID, TenantID: item.TenantID})
	}
	return out
}

func (h *SkillMarketHandlers) tokenBankDecryptShareKeys(share *sqlite.TokenBankShare) (string, []string, error) {
	if share == nil {
		return "", nil, errors.New("share is required")
	}
	primary, err := h.decryptTokenBankKey(share.EncryptedKey)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(primary) == "" {
		return "", nil, errors.New("stored key is empty")
	}
	stored := sqlite.ParseTokenBankExtraKeys(share.ExtraKeysJSON)
	extras := make([]string, 0, len(stored))
	for _, item := range stored {
		plain, decErr := h.decryptTokenBankKey(item.Encrypted)
		if decErr != nil || strings.TrimSpace(plain) == "" {
			return "", nil, errors.New("stored extra key could not be read")
		}
		extras = append(extras, plain)
	}
	return primary, extras, nil
}

func withTokenBankRouteLock(fn func() error) error {
	return llmservice.WithTokenBankRouteLock(fn)
}

// republishTokenBankShare re-publishes one share from the database.
//
// It takes an id rather than a loaded row on purpose. Callers here mutate a
// share struct before calling it — to render the response, or because they just
// wrote a new key — and a republish that consumed those edits instead of the
// stored row would publish a half-applied state: a rotated key would go out
// with the old credential still on the member. Loading inside means the
// registry always receives exactly what the database says, which is also what
// makes retrying a "saved but failed to publish" safe to repeat.
func (h *SkillMarketHandlers) republishTokenBankShare(ctx context.Context, shareID string) error {
	publisher := h.tokenBankPublisher()
	repo := h.tokenBankShareRepo()
	if publisher == nil || repo == nil || strings.TrimSpace(shareID) == "" {
		return errors.New("token bank routing is not available on this node")
	}
	return llmservice.WithTokenBankRouteLock(func() error {
		fresh, err := repo.LoadShare(ctx, shareID, "")
		if err != nil {
			return err
		}
		models, err := repo.ListModels(ctx, fresh.ID)
		if err != nil {
			return err
		}
		primary, extras, err := h.tokenBankDecryptShareKeys(fresh)
		if err != nil {
			return err
		}
		_, err = publisher.PublishTokenBankShare(ctx, tokenBankPublishSpecs(*fresh, models, primary, extras))
		return err
	})
}

// publishTokenBankSpecs is the first publish of a share that was just created.
// It takes the same lock as republish so a take-out cannot be overwritten by
// this write, and this write cannot land between another share's load and publish.
func (h *SkillMarketHandlers) publishTokenBankSpecs(ctx context.Context, specs []llmservice.TokenBankPublishSpec) error {
	publisher := h.tokenBankPublisher()
	if publisher == nil {
		return errors.New("token bank routing is not available on this node")
	}
	return llmservice.WithTokenBankRouteLock(func() error {
		_, err := publisher.PublishTokenBankShare(ctx, specs)
		return err
	})
}

func (h *SkillMarketHandlers) encryptTokenBankKey(apiKey, apiURL, protocol string) (string, error) {
	if h == nil || h.rsaPrivKey == nil {
		return "", errors.New("encryption key unavailable")
	}
	payload, err := json.Marshal(tokenBankKeyEnvelope{
		APIKey:   strings.TrimSpace(apiKey),
		APIURL:   strings.TrimSpace(apiURL),
		Protocol: strings.TrimSpace(protocol),
	})
	if err != nil {
		return "", err
	}
	pkg, err := skillmarket.EncryptForDownload(payload, h.tokenBankEnvelopeUser(), h.rsaPrivKey)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(pkg)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// tokenBankKeyFingerprint matches the desktop client's SHA-256 prefix of
// apiURL + NUL + apiKey. Automation uses it so a retried script hits the same
// idempotency key the GUI would.
func tokenBankKeyFingerprint(apiURL, apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiURL) + "\x00" + apiKey))
	return hex.EncodeToString(sum[:])[:32]
}

// --- PUT /api/v1/token-bank/shares/{id}/visibility ---

func (h *SkillMarketHandlers) TokenBankSetShareVisibility(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	p2 := h.tokenBankP2()
	if p2 == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	var body struct {
		Visibility string                     `json:"visibility"`
		Audiences  []sqlite.TokenBankAudience `json:"audiences"`
	}
	if err := decodeLimitedJSON(w, r, &body, defaultJSONBodyLimit); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
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
	shareID := strings.TrimSpace(r.PathValue("id"))
	share, err := p2.LoadShare(r.Context(), shareID, user.ID)
	if err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	if err := p2.UpdateShareAccess(r.Context(), share.ID, user.ID, visibility, audienceJSON, time.Now().UTC()); err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	share.Visibility = visibility
	share.AudienceJSON = audienceJSON
	if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
		tbError(w, http.StatusInternalServerError, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tokenBankClientSharePayload(*share, nil, false))
}

// --- POST /api/v1/token-bank/shares/{id}/keys ---

func (h *SkillMarketHandlers) TokenBankAddShareKey(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	h.tokenBankWriteExtraKey(w, r, user.ID)
}

func (h *SkillMarketHandlers) tokenBankWriteExtraKey(w http.ResponseWriter, r *http.Request, ownerID string) {
	p2 := h.tokenBankP2()
	if p2 == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	var body struct {
		EncryptedPayload string `json:"encrypted_payload"`
		KeyFingerprint   string `json:"key_fingerprint"`
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
	plain, err := h.decryptTokenBankKey(body.EncryptedPayload)
	if err != nil || strings.TrimSpace(plain) == "" {
		tbError(w, http.StatusBadRequest, "decrypt_failed", "the encrypted payload could not be read")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	share, err := p2.LoadShare(r.Context(), shareID, ownerID)
	if err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	if share.KeyFingerprint == fingerprint {
		tbError(w, http.StatusConflict, "duplicate_key", "this api key is already the primary key")
		return
	}
	keys := sqlite.ParseTokenBankExtraKeys(share.ExtraKeysJSON)
	for _, key := range keys {
		if key.Fingerprint != fingerprint {
			continue
		}
		// The row is already stored. A retry after a failed publish must put
		// that key back on the member instead of stopping at duplicate_key.
		if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
			tbError(w, http.StatusInternalServerError, "publish_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tokenBankClientSharePayload(*share, nil, false))
		return
	}
	taken, err := p2.OwnerKeyFingerprintTaken(r.Context(), share.OwnerUserID, fingerprint, "")
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	if taken {
		tbError(w, http.StatusConflict, "duplicate_key", "this api key is already shared")
		return
	}
	if len(keys) >= sqlite.TokenBankMaxExtraKeys {
		tbError(w, http.StatusConflict, "key_limit_reached", "this share already has the maximum number of keys")
		return
	}
	keys = append(keys, sqlite.TokenBankStoredKey{Fingerprint: fingerprint, Encrypted: strings.TrimSpace(body.EncryptedPayload)})
	encoded, err := sqlite.MarshalTokenBankExtraKeys(keys)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	if err := p2.UpdateShareExtraKeysCAS(r.Context(), share.ID, ownerID, share.ExtraKeysJSON, encoded, time.Now().UTC()); err != nil {
		if errors.Is(err, sqlite.ErrTokenBankShareConflict) {
			tbError(w, http.StatusConflict, "conflict", "this share changed while the key was being saved, try again")
			return
		}
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	share.ExtraKeysJSON = encoded
	if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
		tbError(w, http.StatusInternalServerError, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tokenBankClientSharePayload(*share, nil, false))
}

// --- DELETE /api/v1/token-bank/shares/{id}/keys/{fingerprint} ---

func (h *SkillMarketHandlers) TokenBankRemoveShareKey(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	p2 := h.tokenBankP2()
	if p2 == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	shareID := strings.TrimSpace(r.PathValue("id"))
	fingerprint := strings.TrimSpace(r.PathValue("fingerprint"))
	share, err := p2.LoadShare(r.Context(), shareID, user.ID)
	if err != nil {
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	keys := sqlite.ParseTokenBankExtraKeys(share.ExtraKeysJSON)
	next := make([]sqlite.TokenBankStoredKey, 0, len(keys))
	found := false
	for _, key := range keys {
		if key.Fingerprint == fingerprint {
			found = true
			continue
		}
		next = append(next, key)
	}
	if !found {
		// The database is already without this key. Republish so a delete that
		// saved and then failed to publish does not keep dialing the old key.
		if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
			tbError(w, http.StatusInternalServerError, "publish_failed", err.Error())
			return
		}
		tbError(w, http.StatusNotFound, "key_not_found", "extra key not found")
		return
	}
	encoded, err := sqlite.MarshalTokenBankExtraKeys(next)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	if err := p2.UpdateShareExtraKeysCAS(r.Context(), share.ID, user.ID, share.ExtraKeysJSON, encoded, time.Now().UTC()); err != nil {
		if errors.Is(err, sqlite.ErrTokenBankShareConflict) {
			tbError(w, http.StatusConflict, "conflict", "this share changed while the key was being saved, try again")
			return
		}
		h.writeTokenBankShareLookupError(w, err)
		return
	}
	share.ExtraKeysJSON = encoded
	if err := h.republishTokenBankShare(r.Context(), share.ID); err != nil {
		tbError(w, http.StatusInternalServerError, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tokenBankClientSharePayload(*share, nil, false))
}

// --- GET /api/admin/token-bank/leaderboard ---

func (h *SkillMarketHandlers) TokenBankAdminLeaderboard(w http.ResponseWriter, r *http.Request) {
	p2 := h.tokenBankP2()
	if p2 == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	limit := tokenBankListLimit(r)
	if limit > 100 {
		limit = 100
	}
	rows, err := p2.Leaderboard(r.Context(), limit)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "token_bank_unavailable", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(rows))
	// Same rule as SharerStanding: rank is 1 plus the number of owners with a
	// strictly higher net. A tie shares the medal. The page is the top of the
	// board, so the row index is that count when the net changes.
	rank := 1
	for i, row := range rows {
		if i > 0 && row.EarnedMicro != rows[i-1].EarnedMicro {
			rank = i + 1
		}
		rankBadge, lifetime := sqlite.TokenBankBadges(rank, row.EarnedMicro)
		items = append(items, map[string]any{
			"rank":           rank,
			"owner_user_id":  row.OwnerUserID,
			"owner_email":    row.OwnerEmail,
			"earned_micro":   row.EarnedMicro,
			"calls":          row.Calls,
			"tokens":         row.Tokens,
			"rank_badge":     rankBadge,
			"lifetime_badge": lifetime,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "leaders": items})
}

// tokenBankAutomationShare is one scripted share. The plaintext key is accepted
// here because the rest of the hck_ provider API already takes upstream keys
// in the body. It is encrypted before it is stored.
type tokenBankAutomationShare struct {
	OwnerUserID     string                     `json:"owner_user_id"`
	DisplayName     string                     `json:"display_name"`
	APIURL          string                     `json:"api_url"`
	Protocol        string                     `json:"protocol"`
	APIKey          string                     `json:"api_key"`
	Models          []string                   `json:"models"`
	Visibility      string                     `json:"visibility"`
	Audiences       []sqlite.TokenBankAudience `json:"audiences"`
	MaxInputTokens  int64                      `json:"max_input_tokens_per_request"`
	MaxOutputTokens int64                      `json:"max_output_tokens_per_request"`
}

// --- POST /api/admin/llm/token-bank/shares ---

func (h *SkillMarketHandlers) TokenBankAutomationCreateShare(w http.ResponseWriter, r *http.Request) {
	var body tokenBankAutomationShare
	if err := decodeLimitedJSON(w, r, &body, largeJSONBodyLimit); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	status, payload := h.tokenBankAutomationCreateOne(r, body)
	if status >= 400 {
		code, _ := payload["code"].(string)
		message, _ := payload["message"].(string)
		if code == "" {
			code = "invalid_body"
		}
		tbError(w, status, code, message)
		return
	}
	writeJSON(w, status, payload)
}

// --- POST /api/admin/llm/token-bank/shares/batch ---

func (h *SkillMarketHandlers) TokenBankAutomationBatchShares(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Shares []tokenBankAutomationShare `json:"shares"`
	}
	if err := decodeLimitedJSON(w, r, &body, largeJSONBodyLimit); err != nil {
		tbError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if len(body.Shares) == 0 {
		tbError(w, http.StatusBadRequest, "invalid_body", "at least one share is required")
		return
	}
	if len(body.Shares) > 20 {
		tbError(w, http.StatusBadRequest, "invalid_body", "a batch can contain at most 20 shares")
		return
	}
	results := make([]map[string]any, 0, len(body.Shares))
	for i, item := range body.Shares {
		status, payload := h.tokenBankAutomationCreateOne(r, item)
		row := map[string]any{"index": i, "status": status}
		for k, v := range payload {
			row[k] = v
		}
		row["ok"] = status < 400
		results = append(results, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

func (h *SkillMarketHandlers) tokenBankAutomationCreateOne(r *http.Request, body tokenBankAutomationShare) (int, map[string]any) {
	repo := h.tokenBankShareRepo()
	publisher := h.tokenBankPublisher()
	if repo == nil || publisher == nil || h.store == nil {
		return http.StatusServiceUnavailable, map[string]any{"code": "token_bank_unavailable", "message": "token bank is not available on this node"}
	}
	ownerID := strings.TrimSpace(body.OwnerUserID)
	displayName := strings.TrimSpace(body.DisplayName)
	apiURL := strings.TrimSpace(body.APIURL)
	apiKey := strings.TrimSpace(body.APIKey)
	if ownerID == "" || displayName == "" || apiURL == "" || apiKey == "" || len(body.Models) == 0 {
		return http.StatusBadRequest, map[string]any{"code": "invalid_body", "message": "owner_user_id, display_name, api_url, api_key and models are required"}
	}
	user, err := h.store.GetUserByID(r.Context(), ownerID)
	if err != nil || user == nil || strings.TrimSpace(user.ID) == "" {
		return http.StatusNotFound, map[string]any{"code": "owner_not_found", "message": "owner user was not found"}
	}
	settings, err := h.loadTokenBankSettings(r.Context())
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"code": "token_bank_unavailable", "message": err.Error()}
	}
	if settings.RequireVerifiedIdentity && !strings.EqualFold(strings.TrimSpace(user.Status), "verified") {
		return http.StatusForbidden, map[string]any{"code": "identity_not_verified", "message": "verify the account before sharing a provider"}
	}
	if tokenBankProviderDenied(apiURL, settings.ProviderDenylist) {
		return http.StatusForbidden, map[string]any{"code": "provider_denied", "message": "this provider is not allowed in the Token Bank"}
	}
	audiences, err := sqlite.NormalizeTokenBankAudiences(body.Visibility, body.Audiences)
	if err != nil {
		return http.StatusBadRequest, map[string]any{"code": "invalid_audience", "message": err.Error()}
	}
	visibility, err := sqlite.NormalizeTokenBankVisibility(body.Visibility)
	if err != nil {
		return http.StatusBadRequest, map[string]any{"code": "invalid_visibility", "message": err.Error()}
	}
	audienceJSON, err := sqlite.MarshalTokenBankAudiences(audiences)
	if err != nil {
		return http.StatusBadRequest, map[string]any{"code": "invalid_audience", "message": err.Error()}
	}
	protocol := strings.TrimSpace(body.Protocol)
	if protocol == "" {
		protocol = "openai"
	}
	envelope, err := h.encryptTokenBankKey(apiKey, apiURL, protocol)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"code": "token_bank_unavailable", "message": "the key could not be stored"}
	}
	now := time.Now().UTC()
	canaryUntil := llmservice.TokenBankCanaryDeadlineAfter(now, settings.CanaryWindow())
	shareID := newTokenBankShareID()
	models := make([]sqlite.TokenBankShareModel, 0, len(body.Models))
	for _, name := range body.Models {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		models = append(models, sqlite.TokenBankShareModel{
			ID:             newTokenBankModelID(),
			ModelName:      name,
			MemberID:       llmservice.TokenBankMemberID(shareID, name),
			ArrayID:        llmservice.TokenBankArrayMid,
			Tier:           llmservice.TokenBankTierMid,
			TierMultiplier: 1,
			Enabled:        true,
			Available:      true,
			CanaryUntil:    canaryUntil,
		})
	}
	if len(models) == 0 {
		return http.StatusBadRequest, map[string]any{"code": "invalid_body", "message": "at least one named model is required"}
	}
	share := sqlite.TokenBankShare{
		ID:                        shareID,
		OwnerUserID:               user.ID,
		OwnerEmail:                user.Email,
		DisplayName:               displayName,
		APIURL:                    apiURL,
		Protocol:                  protocol,
		EncryptedKey:              envelope,
		KeyFingerprint:            tokenBankKeyFingerprint(apiURL, apiKey),
		Status:                    sqlite.TokenBankShareStatusActive,
		Visibility:                visibility,
		AudienceJSON:              audienceJSON,
		MaxInputTokensPerRequest:  body.MaxInputTokens,
		MaxOutputTokensPerRequest: body.MaxOutputTokens,
	}
	if p2 := h.tokenBankP2(); p2 != nil {
		taken, takenErr := p2.ExtraKeyFingerprintTaken(r.Context(), user.ID, share.KeyFingerprint)
		if takenErr != nil {
			return http.StatusInternalServerError, map[string]any{"code": "token_bank_unavailable", "message": takenErr.Error()}
		}
		if taken {
			return http.StatusConflict, map[string]any{"code": "duplicate_key", "message": "this api key is already shared"}
		}
	}
	stored, created, err := repo.CreateShare(r.Context(), share, models, settings.MaxSharesPerUser, now)
	if err != nil {
		switch {
		case errors.Is(err, sqlite.ErrTokenBankShareLimitReached):
			return http.StatusConflict, map[string]any{"code": "share_limit_reached", "message": "share limit reached"}
		case errors.Is(err, sqlite.ErrTokenBankDuplicateKey):
			return http.StatusConflict, map[string]any{"code": "duplicate_key", "message": "this api key is already shared"}
		default:
			return http.StatusInternalServerError, map[string]any{"code": "token_bank_unavailable", "message": err.Error()}
		}
	}
	if !created {
		if err := h.republishTokenBankShare(r.Context(), stored.ID); err != nil {
			return http.StatusInternalServerError, map[string]any{"code": "publish_failed", "message": err.Error(), "id": stored.ID}
		}
		storedModels, listErr := repo.ListModels(r.Context(), stored.ID)
		if listErr != nil {
			return http.StatusInternalServerError, map[string]any{"code": "token_bank_unavailable", "message": listErr.Error(), "id": stored.ID}
		}
		models = storedModels
	} else if err := h.publishTokenBankSpecs(r.Context(), tokenBankPublishSpecs(*stored, models, apiKey, nil)); err != nil {
		return http.StatusInternalServerError, map[string]any{"code": "publish_failed", "message": err.Error(), "id": stored.ID}
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	payload := tokenBankClientSharePayload(*stored, models, true)
	payload["ok"] = true
	payload["created"] = created
	return status, payload
}
