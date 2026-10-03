package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Token Bank shares (the "shared upstream" half of the feature). A share is one
// provider a user hands to the pool; its models are what consumers actually
// route to, and the price book is the platform-wide rate table that decides how
// many credits each call is worth.
//
// The encrypted API key arrives already encrypted by the client (RSA, envelope
// per §4). Nothing in this file can read it, and that is deliberate: the
// decryption key lives in the skillmarket handler set, so a bug here cannot
// leak a plaintext provider key.

// Share status values.
const (
	TokenBankShareStatusActive  = "active"
	TokenBankShareStatusPaused  = "paused"
	TokenBankShareStatusRevoked = "revoked"
)

// TokenBankShare is one shared provider.
type TokenBankShare struct {
	ID                        string
	OwnerUserID               string
	OwnerEmail                string
	DisplayName               string
	APIURL                    string
	Protocol                  string
	EncryptedKey              string
	KeyFingerprint            string
	Status                    string
	Visibility                string
	ServiceGroupID            string
	MaxInputTokensPerRequest  int64
	MaxOutputTokensPerRequest int64
	DailyTokenCap             int64
	MonthlyTokenCap           int64
	TotalEarnedMicro          int64
	LastError                 string
	PausedReason              string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	// AudienceJSON is a JSON array of {hub_id, tenant_id}. Empty and "[]" both
	// mean no audience. Only a private share consults it.
	AudienceJSON string
	// ExtraKeysJSON is a JSON array of {fingerprint, encrypted_api_key}. The
	// primary key stays in EncryptedKey. Plaintext never lands in either field.
	ExtraKeysJSON string
}

// TokenBankShareModel is one model inside a share. tier/tierMultiplier are the
// settlement rate; arrayID only says where the dispatch member sits, and its own
// multiplier is pinned at 1.0 so the tier rate cannot leak into what the
// consumer pays (§3.4-A1).
type TokenBankShareModel struct {
	ID               string
	ShareID          string
	ModelName        string
	MemberID         string
	ArrayID          string
	Tier             string
	TierMultiplier   float64
	Enabled          bool
	Available        bool
	LastProbeError   string
	UsedInputTokens  int64
	UsedOutputTokens int64
	EarnedMicro      int64
	// CanaryUntil is when the 5% traffic window ends. Zero means the model is
	// not in canary (shares created before the window, or already promoted).
	CanaryUntil time.Time
	// ShareWindowJSON is when dispatch may dial this model. Empty means
	// always available. It is not a billing schedule.
	ShareWindowJSON string
}

// TokenBankPriceRule is one row of the platform price book.
type TokenBankPriceRule struct {
	ID                   string
	ModelPattern         string
	UnitInputPer10K      float64
	UnitOutputPer10K     float64
	UnitCachedReadPer10K float64
	UnitCacheWritePer10K float64
	UpdatedAt            time.Time
}

// TokenBankSettings is the contents of the `token_bank_settings` system setting.
type TokenBankSettings struct {
	FeeRate                      float64 `json:"fee_rate"`
	FeeTarget                    string  `json:"fee_target"`
	DefaultUnitInputPer10K       float64 `json:"default_unit_input_credits_per_10k"`
	DefaultUnitOutputPer10K      float64 `json:"default_unit_output_credits_per_10k"`
	DefaultUnitCachedReadPer10K  float64 `json:"default_unit_cached_read_credits_per_10k"`
	DefaultUnitCacheWritePer10K  float64 `json:"default_unit_cache_write_credits_per_10k"`
	RequireReviewBeforeOnline    bool    `json:"require_review_before_online"`
	RequireVerifiedIdentity      bool    `json:"require_verified_identity"`
	AutoPauseConsecutiveFailures int     `json:"auto_pause_consecutive_failures"`
	MaxSharesPerUser             int     `json:"max_shares_per_user"`
	CreditShareMaxRatio          float64 `json:"credit_share_max_ratio"`
	CreditShareLinkTTLHours      int     `json:"credit_share_link_ttl_hours"`
	CreditShareDailyLimit        int     `json:"credit_share_daily_limit"`
	CreditShareMinCredits        float64 `json:"credit_share_min_credits"`
	// CanaryWindowHours is how long a newly published model stays on the 5%
	// path. Zero means that model is a full member immediately. A model that
	// already has a deadline keeps it; changing this does not move it.
	CanaryWindowHours int `json:"canary_window_hours"`
	// ClearingNodeID names the one hubcenter node that decides withdrawals.
	//
	// The ledger is replicated asynchronously (a batch every 200 rows or 15s),
	// so two nodes can each see a balance that the other has already spent.
	// Idempotency does not help here: two hubs pulling with two different
	// request ids are two different ledger rows, and both succeed — the credits
	// are over-issued. Nothing in this cluster can serialise them except
	// deciding them in one place, so every withdrawal is routed here.
	//
	// Empty disables the routing, which is the correct default: a single-node
	// deployment has nothing to serialise against, and an unconfigured cluster
	// keeps its current behaviour rather than failing every pull because an
	// operator never filled this in.
	ClearingNodeID string `json:"clearing_node_id"`
	// ProviderDenylist holds hostnames that must not be shared. A match is the
	// API URL host or a subdomain of a listed host.
	ProviderDenylist []string `json:"provider_denylist"`
}

// TokenBankSettingsKey is the system-settings key holding TokenBankSettings.
// It mirrors llmservice.RegistrySettingKey: one JSON blob, read whole, written
// whole, replicated as an HA system-setting op for free.
const TokenBankSettingsKey = "token_bank_settings"

// DefaultTokenBankSettings returns the documented defaults (§5). It is exported
// so the HTTP layer can serve GET before anything has ever been saved, instead
// of returning an empty object the admin UI would have to special-case.
func DefaultTokenBankSettings() TokenBankSettings {
	return TokenBankSettings{
		FeeRate:                      0.10,
		FeeTarget:                    "provider",
		DefaultUnitInputPer10K:       3.0,
		DefaultUnitOutputPer10K:      6.0,
		DefaultUnitCachedReadPer10K:  0.3,
		DefaultUnitCacheWritePer10K:  3.75,
		RequireVerifiedIdentity:      true,
		AutoPauseConsecutiveFailures: 5,
		MaxSharesPerUser:             20,
		CreditShareMaxRatio:          0.5,
		CreditShareLinkTTLHours:      168,
		CreditShareDailyLimit:        10,
		CreditShareMinCredits:        1,
		CanaryWindowHours:            24,
	}
}

// CanaryWindow is how long a newly published model stays on the 5% path.
// Zero means the model is a full member immediately.
func (s TokenBankSettings) CanaryWindow() time.Duration {
	if s.CanaryWindowHours <= 0 {
		return 0
	}
	return time.Duration(s.CanaryWindowHours) * time.Hour
}

// ParseTokenBankSettings decodes a stored blob, filling in any field the stored
// JSON predates. Zero values are NOT treated as "unset" for the fields where
// zero is meaningful (fee_rate 0 is a legitimate "no fee"), so only the slices
// that cannot be zero — the defaults and caps — are repaired. That means a
// stored settings blob is always forward-compatible with new fields.
func ParseTokenBankSettings(raw string) TokenBankSettings {
	out := DefaultTokenBankSettings()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		// A corrupt blob must not strand the whole feature: the defaults are a
		// safe, documented configuration, and the admin can re-save.
		return DefaultTokenBankSettings()
	}
	if out.FeeTarget != "consumer" {
		out.FeeTarget = "provider"
	}
	if out.MaxSharesPerUser <= 0 {
		out.MaxSharesPerUser = DefaultTokenBankSettings().MaxSharesPerUser
	}
	if out.CreditShareMaxRatio <= 0 || out.CreditShareMaxRatio > 1 {
		out.CreditShareMaxRatio = DefaultTokenBankSettings().CreditShareMaxRatio
	}
	if out.CreditShareLinkTTLHours <= 0 {
		out.CreditShareLinkTTLHours = DefaultTokenBankSettings().CreditShareLinkTTLHours
	}
	// Zero hours is "skip the canary". Only a negative value is unread.
	if out.CanaryWindowHours < 0 {
		out.CanaryWindowHours = DefaultTokenBankSettings().CanaryWindowHours
	}
	return out
}

// ErrTokenBankShareNotFound is returned when a share id does not exist or does
// not belong to the caller.
var ErrTokenBankShareNotFound = errors.New("token bank: share not found")

// ErrTokenBankDuplicateKey means the same user already shared this API key.
// The dedup index is (owner_user_id, key_fingerprint), and the fingerprint is a
// hash — never the key itself — so the error never reveals anything.
var ErrTokenBankDuplicateKey = errors.New("token bank: this api key is already shared")

// ErrTokenBankShareLimitReached means the user hit settings.max_shares_per_user.
var ErrTokenBankShareLimitReached = errors.New("token bank: share limit reached")

// ErrTokenBankShareConflict means the row changed since it was read.
var ErrTokenBankShareConflict = errors.New("token bank: share changed")

const tokenBankShareColumns = `id, owner_user_id, owner_email, display_name, api_url, protocol,
	encrypted_api_key, key_fingerprint, status, visibility, service_group_id,
	max_input_tokens_per_request, max_output_tokens_per_request, daily_token_cap, monthly_token_cap,
	total_earned_micro, last_error, paused_reason, created_at, updated_at,
	audience_json, extra_keys_json`

func scanTokenBankShare(row giftLinkScanner) (*TokenBankShare, error) {
	var s TokenBankShare
	var created, updated string
	if err := row.Scan(&s.ID, &s.OwnerUserID, &s.OwnerEmail, &s.DisplayName, &s.APIURL, &s.Protocol,
		&s.EncryptedKey, &s.KeyFingerprint, &s.Status, &s.Visibility, &s.ServiceGroupID,
		&s.MaxInputTokensPerRequest, &s.MaxOutputTokensPerRequest, &s.DailyTokenCap, &s.MonthlyTokenCap,
		&s.TotalEarnedMicro, &s.LastError, &s.PausedReason, &created, &updated,
		&s.AudienceJSON, &s.ExtraKeysJSON); err != nil {
		return nil, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, created)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &s, nil
}

// CreateShare inserts a share plus its models in one transaction and is
// idempotent on (owner, key fingerprint): a double-click, or a client retrying
// after a dropped response, returns the existing row instead of creating a
// second share for the same key.
//
// created reports whether a new row was written, so the caller can tell "your
// share is live" from "you already shared this".
func (r *TokenBankRepo) CreateShare(ctx context.Context, share TokenBankShare, models []TokenBankShareModel, maxShares int, now time.Time) (*TokenBankShare, bool, error) {
	share.OwnerUserID = strings.TrimSpace(share.OwnerUserID)
	share.ID = strings.TrimSpace(share.ID)
	share.KeyFingerprint = strings.TrimSpace(share.KeyFingerprint)
	if share.OwnerUserID == "" || share.ID == "" || share.KeyFingerprint == "" {
		return nil, false, fmt.Errorf("token bank share requires id, owner and key fingerprint")
	}
	if strings.TrimSpace(share.EncryptedKey) == "" {
		return nil, false, fmt.Errorf("token bank share requires an encrypted key")
	}
	if strings.TrimSpace(share.DisplayName) == "" {
		return nil, false, fmt.Errorf("token bank share requires a display name")
	}
	if strings.TrimSpace(share.APIURL) == "" {
		return nil, false, fmt.Errorf("token bank share requires an api url")
	}
	if share.Status == "" {
		share.Status = TokenBankShareStatusActive
	}
	visibility, visErr := NormalizeTokenBankVisibility(share.Visibility)
	if visErr != nil {
		return nil, false, visErr
	}
	share.Visibility = visibility
	if strings.TrimSpace(share.AudienceJSON) == "" {
		share.AudienceJSON = "[]"
	}
	if strings.TrimSpace(share.ExtraKeysJSON) == "" {
		share.ExtraKeysJSON = "[]"
	}
	if share.Protocol == "" {
		share.Protocol = "openai"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stamp := now.UTC().Format(time.RFC3339)

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Idempotency first, and inside the transaction: two concurrent submissions
	// of the same key would both pass a pre-transaction check and then one would
	// fail the unique index with a raw SQL error.
	existing, lookupErr := scanTokenBankShare(tx.QueryRowContext(ctx,
		`SELECT `+tokenBankShareColumns+` FROM token_bank_shares WHERE owner_user_id = ? AND key_fingerprint = ?`,
		share.OwnerUserID, share.KeyFingerprint))
	if lookupErr == nil {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	if !errors.Is(lookupErr, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("lookup token bank share: %w", lookupErr)
	}

	// The cap is enforced here rather than in the handler so a future caller
	// cannot bypass it, and it counts only live shares: a user who withdrew a
	// provider should get that slot back.
	if maxShares > 0 {
		var live int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM token_bank_shares WHERE owner_user_id = ? AND status <> ?`,
			share.OwnerUserID, TokenBankShareStatusRevoked).Scan(&live); err != nil {
			return nil, false, fmt.Errorf("count token bank shares: %w", err)
		}
		if live >= maxShares {
			return nil, false, ErrTokenBankShareLimitReached
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO token_bank_shares (`+tokenBankShareColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		share.ID, share.OwnerUserID, strings.TrimSpace(share.OwnerEmail), strings.TrimSpace(share.DisplayName),
		strings.TrimSpace(share.APIURL), share.Protocol, share.EncryptedKey, share.KeyFingerprint,
		share.Status, share.Visibility, strings.TrimSpace(share.ServiceGroupID),
		share.MaxInputTokensPerRequest, share.MaxOutputTokensPerRequest, share.DailyTokenCap, share.MonthlyTokenCap,
		0, "", "", stamp, stamp, share.AudienceJSON, share.ExtraKeysJSON); err != nil {
		if isTokenBankUniqueViolation(err) {
			return nil, false, ErrTokenBankDuplicateKey
		}
		return nil, false, fmt.Errorf("insert token bank share: %w", err)
	}

	for _, model := range models {
		if err := upsertTokenBankModelTx(ctx, tx, share.ID, model, now); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	share.CreatedAt = now.UTC()
	share.UpdatedAt = now.UTC()
	share.Status = TokenBankShareStatusActive
	return &share, true, nil
}

// ListShares returns shares filtered by owner (empty = every owner) and status
// (empty = every status), newest first. The limit is applied by the caller so
// the admin pagination and the per-user list can share this query.
func (r *TokenBankRepo) ListShares(ctx context.Context, ownerUserID, status string, limit, offset int) ([]TokenBankShare, error) {
	query := `SELECT ` + tokenBankShareColumns + ` FROM token_bank_shares WHERE 1 = 1`
	args := []any{}
	if v := strings.TrimSpace(ownerUserID); v != "" {
		query += ` AND owner_user_id = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(status); v != "" {
		query += ` AND status = ?`
		args = append(args, v)
	}
	query += ` ORDER BY created_at DESC, id ASC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
		if offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list token bank shares: %w", err)
	}
	defer rows.Close()
	out := []TokenBankShare{}
	for rows.Next() {
		share, err := scanTokenBankShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *share)
	}
	return out, rows.Err()
}

// ListShareOwners returns one row per owner of a live share: share and model
// counts, token counters, and credits earned. It backs the admin user grid
// without N+1 queries.
//
// Earned comes from the account cache. Settlement always credits that cache.
// token_bank_models.earned_micro is a per-model counter: taking a share out
// deletes the model rows, and calls settled before the counter was maintained
// never wrote it. Summing the column would show zero for both.
func (r *TokenBankRepo) ListShareOwners(ctx context.Context, limit, offset int) ([]TokenBankOwnerSummary, error) {
	query := `SELECT s.owner_user_id, s.owner_email,
	                 COUNT(DISTINCT s.id) AS share_count,
	                 COUNT(DISTINCT m.model_name) AS model_count,
	                 COALESCE(SUM(m.used_input_tokens + m.used_output_tokens), 0) AS used_tokens,
	                 COALESCE((SELECT a.earned_micro FROM token_bank_accounts a
	                            WHERE a.user_id = s.owner_user_id), 0) AS earned_micro
	            FROM token_bank_shares s
	            LEFT JOIN token_bank_models m ON m.share_id = s.id
	           WHERE s.status <> ?
	           GROUP BY s.owner_user_id, s.owner_email
	           ORDER BY earned_micro DESC, s.owner_user_id ASC`
	args := []any{TokenBankShareStatusRevoked}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
		if offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list token bank owners: %w", err)
	}
	defer rows.Close()
	out := []TokenBankOwnerSummary{}
	for rows.Next() {
		var item TokenBankOwnerSummary
		if err := rows.Scan(&item.OwnerUserID, &item.OwnerEmail, &item.ShareCount,
			&item.ModelCount, &item.UsedTokens, &item.EarnedMicro); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// TokenBankOwnerSummary is one row of the admin user grid.
type TokenBankOwnerSummary struct {
	OwnerUserID string
	OwnerEmail  string
	ShareCount  int
	ModelCount  int
	UsedTokens  int64
	EarnedMicro int64
}

// TokenBankOverview is the platform-wide statistics card.
type TokenBankOverview struct {
	ShareOwners    int
	ShareCount     int
	ModelCount     int
	UsedTokens     int64
	EarnedMicro    int64
	GrantedMicro   int64 // total credits gifted away
	WithdrawnMicro int64
	FrozenMicro    int64
}

// Overview aggregates the whole Token Bank. It reads the materialized account
// cache for the credit totals, including earned (that is exactly what the cache
// is for) but takes the share/model counts and token counters from their own
// tables, so a stale cache cannot invent providers that do not exist.
func (r *TokenBankRepo) Overview(ctx context.Context) (TokenBankOverview, error) {
	var out TokenBankOverview
	if err := r.read.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT owner_user_id), COUNT(*)
		   FROM token_bank_shares WHERE status <> ?`, TokenBankShareStatusRevoked).
		Scan(&out.ShareOwners, &out.ShareCount); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, fmt.Errorf("overview shares: %w", err)
	}
	if err := r.read.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(used_input_tokens + used_output_tokens), 0)
		   FROM token_bank_models m
		   JOIN token_bank_shares s ON s.id = m.share_id AND s.status <> ?`,
		TokenBankShareStatusRevoked).Scan(&out.ModelCount, &out.UsedTokens); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return out, fmt.Errorf("overview models: %w", err)
	}
	if err := r.read.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(earned_micro), 0), COALESCE(SUM(granted_micro), 0),
		        COALESCE(SUM(withdrawn_micro), 0), COALESCE(SUM(frozen_micro), 0)
		   FROM token_bank_accounts`).
		Scan(&out.EarnedMicro, &out.GrantedMicro, &out.WithdrawnMicro, &out.FrozenMicro); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return out, fmt.Errorf("overview accounts: %w", err)
	}
	return out, nil
}

// SetSharePaused flips a share between active and paused. scopeOwner, when not
// empty, restricts the update to that owner — a user may pause their own share,
// an admin omits it. A cross-owner update reports not-found rather than
// forbidden so the endpoint cannot be used to enumerate share ids.
func (r *TokenBankRepo) SetSharePaused(ctx context.Context, shareID, scopeOwner, reason string, paused bool, now time.Time) error {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return ErrTokenBankShareNotFound
	}
	status := TokenBankShareStatusActive
	pausedAt := ""
	if paused {
		status = TokenBankShareStatusPaused
		pausedAt = strings.TrimSpace(reason)
	}
	query := `UPDATE token_bank_shares SET status = ?, paused_reason = ?, updated_at = ? WHERE id = ?`
	args := []any{status, pausedAt, now.UTC().Format(time.RFC3339), shareID}
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		query += ` AND owner_user_id = ?`
		args = append(args, owner)
	}
	res, err := r.write.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("set token bank share paused: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTokenBankShareNotFound
	}
	return nil
}

// TakeOutShare removes a share: it deletes the models and the share row itself
// (§3.5 "取出"). The usage history is NOT touched — token_bank_usage carries a
// snapshot of owner and display name for exactly this reason, and the earned
// credits stay in the ledger. A user who withdraws a provider keeps everything
// it ever earned.
//
// It is a hard delete, so the unique (owner, key fingerprint) row disappears and
// the same key can be shared again later.
func (r *TokenBankRepo) TakeOutShare(ctx context.Context, shareID, scopeOwner string) (bool, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return false, ErrTokenBankShareNotFound
	}
	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	ownerQuery := `SELECT owner_user_id FROM token_bank_shares WHERE id = ?`
	ownerArgs := []any{shareID}
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		ownerQuery += ` AND owner_user_id = ?`
		ownerArgs = append(ownerArgs, owner)
	}
	var ownerUserID string
	if err := tx.QueryRowContext(ctx, ownerQuery, ownerArgs...).Scan(&ownerUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrTokenBankShareNotFound
		}
		return false, fmt.Errorf("load token bank share owner: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM token_bank_models WHERE share_id = ?`, shareID); err != nil {
		return false, fmt.Errorf("delete token bank models: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_bank_shares WHERE id = ?`, shareID); err != nil {
		return false, fmt.Errorf("delete token bank share: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// RenameShareKey replaces the encrypted key in place, keeping the share id and
// its accumulated credits (§6.1 "原地换 Key"). A rotation that changed the id
// would orphan the usage history and the earned total. A non-empty apiURL or
// protocol is written in the same update so a later republish cannot put the
// previous endpoint back.
func (r *TokenBankRepo) RenameShareKey(ctx context.Context, shareID, scopeOwner, encryptedKey, fingerprint, apiURL, protocol string, now time.Time) error {
	shareID = strings.TrimSpace(shareID)
	encryptedKey = strings.TrimSpace(encryptedKey)
	fingerprint = strings.TrimSpace(fingerprint)
	if shareID == "" {
		return ErrTokenBankShareNotFound
	}
	if encryptedKey == "" || fingerprint == "" {
		return fmt.Errorf("token bank key rotation requires an encrypted key and its fingerprint")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	query := `UPDATE token_bank_shares SET encrypted_api_key = ?, key_fingerprint = ?, updated_at = ?`
	args := []any{encryptedKey, fingerprint, now.UTC().Format(time.RFC3339)}
	if url := strings.TrimSpace(apiURL); url != "" {
		query += `, api_url = ?`
		args = append(args, url)
	}
	if proto := strings.TrimSpace(protocol); proto != "" {
		query += `, protocol = ?`
		args = append(args, proto)
	}
	query += ` WHERE id = ?`
	args = append(args, shareID)
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		query += ` AND owner_user_id = ?`
		args = append(args, owner)
	}
	res, err := r.write.ExecContext(ctx, query, args...)
	if err != nil {
		if isTokenBankUniqueViolation(err) {
			return ErrTokenBankDuplicateKey
		}
		return fmt.Errorf("rotate token bank key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTokenBankShareNotFound
	}
	return nil
}

// LoadShare returns one share, optionally scoped to an owner.
func (r *TokenBankRepo) LoadShare(ctx context.Context, shareID, scopeOwner string) (*TokenBankShare, error) {
	query := `SELECT ` + tokenBankShareColumns + ` FROM token_bank_shares WHERE id = ?`
	args := []any{strings.TrimSpace(shareID)}
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		query += ` AND owner_user_id = ?`
		args = append(args, owner)
	}
	share, err := scanTokenBankShare(r.read.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenBankShareNotFound
		}
		return nil, fmt.Errorf("load token bank share: %w", err)
	}
	return share, nil
}

const tokenBankModelColumns = `id, share_id, model_name, member_id, array_id, tier, tier_multiplier,
	enabled, available, last_probe_error, used_input_tokens, used_output_tokens, earned_micro, canary_until,
	share_window`

func scanTokenBankModel(row giftLinkScanner) (*TokenBankShareModel, error) {
	var m TokenBankShareModel
	var enabled, available int
	var canaryUntil string
	if err := row.Scan(&m.ID, &m.ShareID, &m.ModelName, &m.MemberID, &m.ArrayID, &m.Tier, &m.TierMultiplier,
		&enabled, &available, &m.LastProbeError, &m.UsedInputTokens, &m.UsedOutputTokens, &m.EarnedMicro, &canaryUntil,
		&m.ShareWindowJSON); err != nil {
		return nil, err
	}
	m.Enabled = enabled != 0
	m.Available = available != 0
	if strings.TrimSpace(canaryUntil) != "" {
		m.CanaryUntil, _ = time.Parse(time.RFC3339, canaryUntil)
	}
	return &m, nil
}

// ListModels returns every model of a share, ordered by name so the GUI and the
// admin table do not reshuffle between requests.
func (r *TokenBankRepo) ListModels(ctx context.Context, shareID string) ([]TokenBankShareModel, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT `+tokenBankModelColumns+` FROM token_bank_models WHERE share_id = ? ORDER BY model_name ASC`,
		strings.TrimSpace(shareID))
	if err != nil {
		return nil, fmt.Errorf("list token bank models: %w", err)
	}
	defer rows.Close()
	out := []TokenBankShareModel{}
	for rows.Next() {
		m, err := scanTokenBankModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// SetModelTier re-tiers one model by name within a share. The row write itself
// does not touch routing. Callers that label a live member also update the
// registry multiplier and the service-group route.
func (r *TokenBankRepo) SetModelTier(ctx context.Context, shareID, modelName, tier string, tierMultiplier float64, arrayID string, now time.Time) error {
	shareID = strings.TrimSpace(shareID)
	modelName = strings.TrimSpace(modelName)
	if shareID == "" || modelName == "" {
		return fmt.Errorf("token bank tier update requires share id and model name")
	}
	res, err := r.write.ExecContext(ctx,
		`UPDATE token_bank_models SET tier = ?, tier_multiplier = ?, array_id = ? WHERE share_id = ? AND model_name = ?`,
		tier, tierMultiplier, strings.TrimSpace(arrayID), shareID, modelName)
	if err != nil {
		return fmt.Errorf("set token bank model tier: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTokenBankShareNotFound
	}
	return nil
}

// CountShareModels returns how many models a share currently has. Used to reject
// a tier change whose share id is wrong before doing any work.
func (r *TokenBankRepo) CountShareModels(ctx context.Context, shareID string) (int, error) {
	var n int
	err := r.read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM token_bank_models WHERE share_id = ?`, strings.TrimSpace(shareID)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count token bank models: %w", err)
	}
	return n, nil
}

// SyncShareModels reconciles a share's model list with a freshly probed one.
//
// This is the operation §3.1 actually needs and CreateShare alone cannot
// provide: the client probes the provider, the user ticks a different set of
// models, and the result has to reach the pool. CreateShare is idempotent on
// (owner, fingerprint) and therefore returns early for an already-known key —
// models are never re-synced — so without this method a user who enabled a new
// model on the provider side would have no way to share it, and the
// ON CONFLICT branch of the model upsert would be unreachable code.
//
// Semantics:
//   - models already present are UPDATED in place (tier/member/enabled), so
//     their accumulated used_input_tokens/earned_micro survive. Overwriting
//     the row would silently erase the share's earnings history.
//   - models absent from the new list are DISABLED, not deleted, for the same
//     reason: the row is what carries the usage counters, and a settlement may
//     still be in flight against it.
//   - the whole thing is one transaction so the pool never sees a half-updated
//     share.
func (r *TokenBankRepo) SyncShareModels(ctx context.Context, shareID, scopeOwner string, models []TokenBankShareModel, now time.Time) (int, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return 0, ErrTokenBankShareNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// The owner scope is checked inside the transaction so a caller cannot race
	// a takeout and sync models onto a share that no longer exists.
	ownerQuery := `SELECT owner_user_id FROM token_bank_shares WHERE id = ?`
	ownerArgs := []any{shareID}
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		ownerQuery += ` AND owner_user_id = ?`
		ownerArgs = append(ownerArgs, owner)
	}
	var ownerUserID string
	if err := tx.QueryRowContext(ctx, ownerQuery, ownerArgs...).Scan(&ownerUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrTokenBankShareNotFound
		}
		return 0, fmt.Errorf("load token bank share owner: %w", err)
	}

	keep := make(map[string]struct{}, len(models))
	for _, model := range models {
		name := strings.TrimSpace(model.ModelName)
		if name == "" {
			return 0, fmt.Errorf("token bank model requires a model name")
		}
		if _, dup := keep[name]; dup {
			return 0, fmt.Errorf("token bank model %q listed twice", name)
		}
		keep[name] = struct{}{}
	}

	for _, model := range models {
		if err := upsertTokenBankModelTx(ctx, tx, shareID, model, now); err != nil {
			return 0, err
		}
	}

	// Retire whatever is no longer in the list. Disabling rather than deleting
	// keeps the usage counters attached to the model they describe.
	existing, err := listTokenBankModelNamesTx(ctx, tx, shareID)
	if err != nil {
		return 0, err
	}
	retired := 0
	for _, name := range existing {
		if _, ok := keep[name]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE token_bank_models SET enabled = 0 WHERE share_id = ? AND model_name = ?`,
			shareID, name); err != nil {
			return 0, fmt.Errorf("disable retired token bank model %q: %w", name, err)
		}
		retired++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return retired, nil
}

// listTokenBankModelNamesTx reads the model names of one share inside an open
// transaction.
func listTokenBankModelNamesTx(ctx context.Context, tx *sql.Tx, shareID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT model_name FROM token_bank_models WHERE share_id = ?`, shareID)
	if err != nil {
		return nil, fmt.Errorf("list token bank model names: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// upsertTokenBankModelTx writes one model row inside an open transaction.
//
// The ON CONFLICT branch intentionally updates only the routing/rate/probe
// fields and never the counters: re-syncing a model list must not zero out what
// the model has already earned.
//
// `array_id` IS refreshed, because §3.4 defines 标档位 = 移阵列: the array the
// member sits in *is* the tier, so changing the tier without moving the array
// would leave dispatch routing the member to its old group.
func upsertTokenBankModelTx(ctx context.Context, tx *sql.Tx, shareID string, model TokenBankShareModel, now time.Time) error {
	model.ModelName = strings.TrimSpace(model.ModelName)
	if model.ModelName == "" {
		return fmt.Errorf("token bank model requires a model name")
	}
	if model.ID == "" {
		model.ID = "tbkm_" + hashTokenBankKey(shareID+"/"+model.ModelName)
	}
	if model.Tier == "" {
		model.Tier = "mid"
	}
	if model.ArrayID == "" {
		model.ArrayID = "token_bank_mid"
	}
	if model.TierMultiplier == 0 {
		model.TierMultiplier = 1
	}
	enabled := 0
	if model.Enabled {
		enabled = 1
	}
	available := 0
	if model.Available {
		available = 1
	}
	// ON CONFLICT rather than INSERT OR IGNORE: a re-submitted share whose model
	// list changed must update the existing rows, not silently keep the old ones.
	//
	// `array_id`, `available` and `last_probe_error` ARE refreshed: §3.4 makes the
	// array the tier (标档位 = 移阵列), and §3.1 says the server's periodic
	// re-probe overwrites the client's initial one, so a model that has since
	// gone down must be marked unavailable instead of staying green forever.
	// The token counters are deliberately NOT in this list — see the doc comment
	// above.
	canaryUntil := ""
	if !model.CanaryUntil.IsZero() {
		canaryUntil = model.CanaryUntil.UTC().Format(time.RFC3339)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO token_bank_models (`+tokenBankModelColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(share_id, model_name) DO UPDATE SET
		     member_id = excluded.member_id,
		     array_id = excluded.array_id,
		     tier = excluded.tier,
		     tier_multiplier = excluded.tier_multiplier,
		     enabled = excluded.enabled,
		     available = excluded.available,
		     last_probe_error = excluded.last_probe_error,
		     share_window = excluded.share_window,
		     canary_until = CASE
		         WHEN token_bank_models.canary_until <> '' THEN token_bank_models.canary_until
		         ELSE excluded.canary_until
		     END`,
		model.ID, shareID, model.ModelName, strings.TrimSpace(model.MemberID), model.ArrayID, model.Tier,
		model.TierMultiplier, enabled, available, strings.TrimSpace(model.LastProbeError),
		model.UsedInputTokens, model.UsedOutputTokens, model.EarnedMicro, canaryUntil,
		strings.TrimSpace(model.ShareWindowJSON)); err != nil {
		return fmt.Errorf("upsert token bank model: %w", err)
	}
	return nil
}

// NoteShareModelError records a proxy refusal against the share. The model name
// is part of the text so the owner can see which model tripped the cap. A
// missing share is not an error: the member may already have been taken out.
func (r *TokenBankRepo) NoteShareModelError(ctx context.Context, shareID, model, message string) error {
	shareID = strings.TrimSpace(shareID)
	message = strings.TrimSpace(message)
	if r == nil || shareID == "" || message == "" {
		return nil
	}
	if model = strings.TrimSpace(model); model != "" {
		message = model + ": " + message
	}
	_, err := r.write.ExecContext(ctx,
		`UPDATE token_bank_shares SET last_error = ?, updated_at = ? WHERE id = ?`,
		message, time.Now().UTC().Format(time.RFC3339), shareID)
	if err != nil {
		return fmt.Errorf("note token bank share error: %w", err)
	}
	return nil
}

// --- price book ---

// ListPriceRules returns the whole price book, exact patterns first and then by
// descending prefix length, which is the order resolution walks it.
func (r *TokenBankRepo) ListPriceRules(ctx context.Context) ([]TokenBankPriceRule, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT id, model_pattern, unit_input_credits_per_10k, unit_output_credits_per_10k,
		        unit_cached_read_credits_per_10k, unit_cache_write_credits_per_10k, updated_at
		   FROM token_bank_price_book ORDER BY model_pattern ASC`)
	if err != nil {
		return nil, fmt.Errorf("list token bank price rules: %w", err)
	}
	defer rows.Close()
	out := []TokenBankPriceRule{}
	for rows.Next() {
		var rule TokenBankPriceRule
		var updated string
		if err := rows.Scan(&rule.ID, &rule.ModelPattern, &rule.UnitInputPer10K, &rule.UnitOutputPer10K,
			&rule.UnitCachedReadPer10K, &rule.UnitCacheWritePer10K, &updated); err != nil {
			return nil, err
		}
		rule.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, rule)
	}
	return out, rows.Err()
}

// UpsertPriceRule writes one price-book row. The id is deterministic on the
// pattern so an admin editing a pattern twice updates in place, and so the row
// is safe to replicate by id.
func (r *TokenBankRepo) UpsertPriceRule(ctx context.Context, rule TokenBankPriceRule, now time.Time) (*TokenBankPriceRule, error) {
	rule.ModelPattern = strings.TrimSpace(rule.ModelPattern)
	if rule.ModelPattern == "" {
		return nil, fmt.Errorf("token bank price rule requires a model pattern")
	}
	if rule.UnitInputPer10K < 0 || rule.UnitOutputPer10K < 0 ||
		rule.UnitCachedReadPer10K < 0 || rule.UnitCacheWritePer10K < 0 {
		return nil, fmt.Errorf("token bank price rule units must not be negative")
	}
	if rule.ID == "" {
		rule.ID = "tbkp_" + hashTokenBankKey(rule.ModelPattern)
	}
	stamp := now.UTC().Format(time.RFC3339)
	if _, err := r.write.ExecContext(ctx,
		`INSERT INTO token_bank_price_book (id, model_pattern, unit_input_credits_per_10k, unit_output_credits_per_10k,
			unit_cached_read_credits_per_10k, unit_cache_write_credits_per_10k, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     model_pattern = excluded.model_pattern,
		     unit_input_credits_per_10k = excluded.unit_input_credits_per_10k,
		     unit_output_credits_per_10k = excluded.unit_output_credits_per_10k,
		     unit_cached_read_credits_per_10k = excluded.unit_cached_read_credits_per_10k,
		     unit_cache_write_credits_per_10k = excluded.unit_cache_write_credits_per_10k,
		     updated_at = excluded.updated_at`,
		rule.ID, rule.ModelPattern, rule.UnitInputPer10K, rule.UnitOutputPer10K,
		rule.UnitCachedReadPer10K, rule.UnitCacheWritePer10K, stamp); err != nil {
		return nil, fmt.Errorf("upsert token bank price rule: %w", err)
	}
	rule.UpdatedAt = now.UTC()
	// Publish only after the write landed. The id is deterministic on the
	// pattern, so a peer applying this twice converges instead of duplicating.
	r.publishPriceRule(rule, false)
	return &rule, nil
}

// DeletePriceRule removes one rule by id or by pattern.
func (r *TokenBankRepo) DeletePriceRule(ctx context.Context, idOrPattern string) (bool, error) {
	key := strings.TrimSpace(idOrPattern)
	if key == "" {
		return false, nil
	}
	// Resolve to the canonical id first. Replication keys a deletion by id, and
	// a caller is allowed to pass either the id or the pattern — publishing
	// whichever string arrived would make peers delete by a value that only
	// ever matched on this node.
	var ruleID string
	err := r.read.QueryRowContext(ctx,
		`SELECT id FROM token_bank_price_book WHERE id = ? OR model_pattern = ?
		  ORDER BY (id = ?) DESC LIMIT 1`, key, key, key).Scan(&ruleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("lookup token bank price rule: %w", err)
	}
	res, err := r.write.ExecContext(ctx,
		`DELETE FROM token_bank_price_book WHERE id = ?`, ruleID)
	if err != nil {
		return false, fmt.Errorf("delete token bank price rule: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows > 0 {
		r.publishPriceRule(TokenBankPriceRule{ID: ruleID}, true)
	}
	return rows > 0, nil
}

// ResolvePrice picks the rule that applies to a model name.
//
// Order: an exact match wins outright; otherwise the longest matching prefix
// wins. Longest-prefix is the rule that lets an admin write `gpt-4o-*` and have
// it beaten by `gpt-4o-mini` without either pattern needing to know about the
// other. The second result is false when nothing matched, which tells the
// caller to fall back to the settings defaults.
func (r *TokenBankRepo) ResolvePrice(ctx context.Context, modelName string) (TokenBankPriceRule, bool, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return TokenBankPriceRule{}, false, nil
	}
	rules, err := r.ListPriceRules(ctx)
	if err != nil {
		return TokenBankPriceRule{}, false, err
	}
	best := -1
	bestLen := -1
	for i, rule := range rules {
		pattern := strings.TrimSpace(rule.ModelPattern)
		if pattern == "" {
			continue
		}
		if pattern == modelName {
			return rule, true, nil
		}
		if prefix, ok := tokenBankWildcardPrefix(pattern); ok && strings.HasPrefix(modelName, prefix) {
			if len(prefix) > bestLen {
				best, bestLen = i, len(prefix)
			}
		}
	}
	if best >= 0 {
		return rules[best], true, nil
	}
	return TokenBankPriceRule{}, false, nil
}

// tokenBankWildcardPrefix extracts the literal prefix of a glob pattern. Only a
// trailing `*` is honoured: a pattern with a wildcard in the middle would need a
// real matcher, and quietly treating `a*b` as prefix `a` would price models the
// admin never meant to include.
func tokenBankWildcardPrefix(pattern string) (string, bool) {
	if !strings.HasSuffix(pattern, "*") {
		return "", false
	}
	prefix := strings.TrimSuffix(pattern, "*")
	if strings.ContainsAny(prefix, "*?") {
		return "", false
	}
	return prefix, prefix != ""
}

// SortedPricePatterns is a small helper for the admin UI and tests: the price
// book in the order resolution considers it.
func SortedPricePatterns(rules []TokenBankPriceRule) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rule.ModelPattern)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// isTokenBankUniqueViolation reports whether an error is a UNIQUE constraint
// failure. modernc's sqlite driver does not export a typed error for this, so
// the message is the only signal available; matching on it is ugly but it is
// what keeps a duplicate share from surfacing as a 500.
func isTokenBankUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "UNIQUE CONSTRAINT FAILED") || strings.Contains(msg, "CONSTRAINT_UNIQUE")
}
