package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TokenBankVisibilityPublic  = "public"
	TokenBankVisibilityPrivate = "private"
	// TokenBankMaxAudiences bounds a private share's allow-list.
	TokenBankMaxAudiences = 32
	// TokenBankMaxExtraKeys bounds how many extra upstream keys one share rotates.
	TokenBankMaxExtraKeys = 8
)

// TokenBankAudience is one hub and/or tenant allowed to call a private share.
type TokenBankAudience struct {
	HubID    string `json:"hub_id,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// TokenBankStoredKey is one extra upstream key. Encrypted is the same envelope
// shape as the share's primary key. Fingerprint is the dedup id, never the key.
type TokenBankStoredKey struct {
	Fingerprint string `json:"fingerprint"`
	Encrypted   string `json:"encrypted_api_key"`
}

// TokenBankLeaderboardRow is one sharer ordered by settled net credits.
type TokenBankLeaderboardRow struct {
	OwnerUserID string
	OwnerEmail  string
	EarnedMicro int64
	Calls       int64
	Tokens      int64
}

// NormalizeTokenBankVisibility accepts empty as public. Anything else is an error.
func NormalizeTokenBankVisibility(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", TokenBankVisibilityPublic:
		return TokenBankVisibilityPublic, nil
	case TokenBankVisibilityPrivate:
		return TokenBankVisibilityPrivate, nil
	default:
		return "", fmt.Errorf("token bank visibility must be public or private")
	}
}

// NormalizeTokenBankAudiences drops blank rows, trims ids, and rejects a
// private share that names nobody. Public shares keep an empty list.
func NormalizeTokenBankAudiences(visibility string, in []TokenBankAudience) ([]TokenBankAudience, error) {
	visibility, err := NormalizeTokenBankVisibility(visibility)
	if err != nil {
		return nil, err
	}
	out := make([]TokenBankAudience, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item.HubID = strings.TrimSpace(item.HubID)
		item.TenantID = strings.TrimSpace(item.TenantID)
		if item.HubID == "" && item.TenantID == "" {
			continue
		}
		if len(item.HubID) > 128 || len(item.TenantID) > 128 {
			return nil, fmt.Errorf("token bank audience id is too long")
		}
		key := strings.ToLower(item.HubID) + "\x00" + strings.ToLower(item.TenantID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	if len(out) > TokenBankMaxAudiences {
		return nil, fmt.Errorf("token bank audience list is too long")
	}
	if visibility == TokenBankVisibilityPrivate && len(out) == 0 {
		return nil, fmt.Errorf("a private share needs at least one hub or tenant")
	}
	if visibility == TokenBankVisibilityPublic {
		return nil, nil
	}
	return out, nil
}

// MarshalTokenBankAudiences encodes an allow-list. Nil becomes "[]".
func MarshalTokenBankAudiences(items []TokenBankAudience) (string, error) {
	if items == nil {
		items = []TokenBankAudience{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ParseTokenBankAudiences decodes a stored allow-list. Blank and invalid JSON
// are an empty list: a corrupt blob must not widen a private share to public.
func ParseTokenBankAudiences(raw string) []TokenBankAudience {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var items []TokenBankAudience
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	out := make([]TokenBankAudience, 0, len(items))
	for _, item := range items {
		item.HubID = strings.TrimSpace(item.HubID)
		item.TenantID = strings.TrimSpace(item.TenantID)
		if item.HubID == "" && item.TenantID == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// ParseTokenBankExtraKeys decodes stored extra keys. Invalid JSON is empty so
// a corrupt blob cannot be published as a key.
func ParseTokenBankExtraKeys(raw string) []TokenBankStoredKey {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var items []TokenBankStoredKey
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	out := make([]TokenBankStoredKey, 0, len(items))
	for _, item := range items {
		item.Fingerprint = strings.TrimSpace(item.Fingerprint)
		item.Encrypted = strings.TrimSpace(item.Encrypted)
		if item.Fingerprint == "" || item.Encrypted == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// MarshalTokenBankExtraKeys encodes extra keys. Nil becomes "[]".
func MarshalTokenBankExtraKeys(items []TokenBankStoredKey) (string, error) {
	if items == nil {
		items = []TokenBankStoredKey{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// TokenBankBadges reports the top-three medal and the lifetime tier.
// rank 0 means the sharer has no settled usage and gets no medal.
func TokenBankBadges(rank int, earnedMicro int64) (rankBadge, lifetimeBadge string) {
	switch rank {
	case 1:
		rankBadge = "gold"
	case 2:
		rankBadge = "silver"
	case 3:
		rankBadge = "bronze"
	}
	credits := earnedMicro / 1_000_000
	switch {
	case credits >= 10000:
		lifetimeBadge = "pillar"
	case credits >= 1000:
		lifetimeBadge = "steady"
	case credits >= 100:
		lifetimeBadge = "contributor"
	case earnedMicro > 0:
		lifetimeBadge = "sprout"
	}
	return rankBadge, lifetimeBadge
}

// UpdateShareAccess replaces visibility and the allow-list. scopeOwner, when
// set, confines the write to that owner.
func (r *TokenBankRepo) UpdateShareAccess(ctx context.Context, shareID, scopeOwner, visibility, audienceJSON string, now time.Time) error {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return ErrTokenBankShareNotFound
	}
	normalized, err := NormalizeTokenBankVisibility(visibility)
	if err != nil {
		return err
	}
	if strings.TrimSpace(audienceJSON) == "" {
		audienceJSON = "[]"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	query := `UPDATE token_bank_shares SET visibility = ?, audience_json = ?, updated_at = ? WHERE id = ?`
	args := []any{normalized, audienceJSON, now.UTC().Format(time.RFC3339), shareID}
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		query += ` AND owner_user_id = ?`
		args = append(args, owner)
	}
	res, err := r.write.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update token bank share access: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTokenBankShareNotFound
	}
	return nil
}

// UpdateShareExtraKeys replaces the extra-key list. The primary key is untouched.
func (r *TokenBankRepo) UpdateShareExtraKeys(ctx context.Context, shareID, scopeOwner, extraJSON string, now time.Time) error {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return ErrTokenBankShareNotFound
	}
	if strings.TrimSpace(extraJSON) == "" {
		extraJSON = "[]"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	query := `UPDATE token_bank_shares SET extra_keys_json = ?, updated_at = ? WHERE id = ?`
	args := []any{extraJSON, now.UTC().Format(time.RFC3339), shareID}
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		query += ` AND owner_user_id = ?`
		args = append(args, owner)
	}
	res, err := r.write.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update token bank extra keys: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTokenBankShareNotFound
	}
	return nil
}

// UpdateShareExtraKeysCAS writes the extra-key list only when it still equals
// previousJSON. A concurrent add or delete returns ErrTokenBankShareConflict
// instead of dropping the other change.
func (r *TokenBankRepo) UpdateShareExtraKeysCAS(ctx context.Context, shareID, scopeOwner, previousJSON, extraJSON string, now time.Time) error {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return ErrTokenBankShareNotFound
	}
	if strings.TrimSpace(extraJSON) == "" {
		extraJSON = "[]"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	query := `UPDATE token_bank_shares SET extra_keys_json = ?, updated_at = ? WHERE id = ? AND extra_keys_json = ?`
	args := []any{extraJSON, now.UTC().Format(time.RFC3339), shareID, previousJSON}
	if owner := strings.TrimSpace(scopeOwner); owner != "" {
		query += ` AND owner_user_id = ?`
		args = append(args, owner)
	}
	res, err := r.write.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update token bank extra keys: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := r.LoadShare(ctx, shareID, scopeOwner); err != nil {
		return err
	}
	return ErrTokenBankShareConflict
}

// OwnerKeyFingerprintTaken reports whether this owner already uses the
// fingerprint as a primary key or an extra key. exceptShare skips that share's
// primary fingerprint only; extra keys on the same share still count.
func (r *TokenBankRepo) OwnerKeyFingerprintTaken(ctx context.Context, ownerID, fingerprint, exceptShare string) (bool, error) {
	ownerID = strings.TrimSpace(ownerID)
	fingerprint = strings.TrimSpace(fingerprint)
	exceptShare = strings.TrimSpace(exceptShare)
	if ownerID == "" || fingerprint == "" {
		return false, nil
	}
	shares, err := r.ListShares(ctx, ownerID, "", 0, 0)
	if err != nil {
		return false, err
	}
	for _, share := range shares {
		if exceptShare != "" && share.ID == exceptShare && share.KeyFingerprint == fingerprint {
			continue
		}
		if share.KeyFingerprint == fingerprint {
			return true, nil
		}
		for _, key := range ParseTokenBankExtraKeys(share.ExtraKeysJSON) {
			if key.Fingerprint == fingerprint {
				return true, nil
			}
		}
	}
	return false, nil
}

// ExtraKeyFingerprintTaken reports whether this owner already stored the
// fingerprint on any share as an extra key. Primary fingerprints are ignored
// so creating the same primary key again stays an idempotent replay.
func (r *TokenBankRepo) ExtraKeyFingerprintTaken(ctx context.Context, ownerID, fingerprint string) (bool, error) {
	ownerID = strings.TrimSpace(ownerID)
	fingerprint = strings.TrimSpace(fingerprint)
	if ownerID == "" || fingerprint == "" {
		return false, nil
	}
	shares, err := r.ListShares(ctx, ownerID, "", 0, 0)
	if err != nil {
		return false, err
	}
	for _, share := range shares {
		for _, key := range ParseTokenBankExtraKeys(share.ExtraKeysJSON) {
			if key.Fingerprint == fingerprint {
				return true, nil
			}
		}
	}
	return false, nil
}

// Leaderboard ranks sharers by settled net credits. The email is the latest
// share row for that owner; a sharer whose shares were all taken out still
// ranks, with an empty email.
func (r *TokenBankRepo) Leaderboard(ctx context.Context, limit int) ([]TokenBankLeaderboardRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.read.QueryContext(ctx, `
		SELECT u.owner_user_id,
		       COALESCE((SELECT s.owner_email FROM token_bank_shares s
		                  WHERE s.owner_user_id = u.owner_user_id
		                  ORDER BY s.updated_at DESC, s.id ASC LIMIT 1), ''),
		       COALESCE(SUM(u.net_micro), 0),
		       COUNT(*),
		       COALESCE(SUM(u.input_tokens + u.output_tokens), 0)
		  FROM token_bank_usage u
		 WHERE u.owner_user_id <> ''
		 GROUP BY u.owner_user_id
		HAVING SUM(u.net_micro) > 0
		 ORDER BY SUM(u.net_micro) DESC, u.owner_user_id ASC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("token bank leaderboard: %w", err)
	}
	defer rows.Close()
	out := []TokenBankLeaderboardRow{}
	for rows.Next() {
		var row TokenBankLeaderboardRow
		if err := rows.Scan(&row.OwnerUserID, &row.OwnerEmail, &row.EarnedMicro, &row.Calls, &row.Tokens); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SharerStanding is one owner's settled net and 1-based rank. Rank 0 means
// they have no settled usage yet.
func (r *TokenBankRepo) SharerStanding(ctx context.Context, ownerUserID string) (earnedMicro int64, rank int, err error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return 0, 0, nil
	}
	err = r.read.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(net_micro), 0) FROM token_bank_usage WHERE owner_user_id = ?`,
		ownerUserID).Scan(&earnedMicro)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("token bank standing: %w", err)
	}
	if earnedMicro <= 0 {
		return earnedMicro, 0, nil
	}
	var ahead int
	err = r.read.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT owner_user_id FROM token_bank_usage
			 WHERE owner_user_id <> ''
			 GROUP BY owner_user_id
			HAVING SUM(net_micro) > ?
		)`, earnedMicro).Scan(&ahead)
	if err != nil {
		return 0, 0, fmt.Errorf("token bank rank: %w", err)
	}
	return earnedMicro, ahead + 1, nil
}
