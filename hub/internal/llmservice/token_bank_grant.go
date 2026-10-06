package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Token Bank credits become spendable only after a hub writes a permanent
// grant. The ledger on HubCenter is not an agent balance. See the design
// doc, section 14.
const (
	TokenBankGrantSource = "token_bank"

	tokenBankGrantCardPrefix = "tbk:"
	// tokenBankMicroPerCredit is the single conversion between the integer
	// ledger and Grant.CreditsTotal. It is not roundCredits: rounding to
	// three decimals would drop up to 500 micro and break cross-end reconcile.
	tokenBankMicroPerCredit int64 = 1_000_000

	tokenBankAutoSettingsKey = "token_bank_auto_withdraw"
	tokenBankAutoSeqKey      = "token_bank_auto_seq"
	// TokenBankDefaultAutoThresholdMicro stays in stored settings so an older
	// document still loads. Auto pull does not top up to this balance. It runs
	// only when the user has no spendable credits left.
	TokenBankDefaultAutoThresholdMicro int64 = 1_000_000
)

// tokenBankGrantExpiresAt is the far-future sentinel other permanent grants
// use, so code that only reads ExpiresAt still treats the grant as live.
var tokenBankGrantExpiresAt = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

// PermanentGrantExpiresAt exposes the far-future expiry sentinel to transport
// handlers that mint their own never-expiring wallet grants (check-in rewards).
func PermanentGrantExpiresAt() time.Time {
	return tokenBankGrantExpiresAt
}

var (
	// ErrTokenBankServiceGroupMissing means the hub has no such service group.
	// Callers must return this before any HubCenter debit. A missing group is
	// an error, not a skipped grant.
	ErrTokenBankServiceGroupMissing = errors.New("token bank service group does not exist")
	// ErrTokenBankNothingToWithdraw means HubCenter refused because the ledger
	// has nothing left to move. Retrying the same request id is safe.
	ErrTokenBankNothingToWithdraw = errors.New("token bank: nothing to withdraw")
	// ErrTokenBankInsufficient means the request is larger than the automatic
	// 1/N cap. The hub secret must not lift that cap. A signed-in user can
	// authorize the full amount on the user-session endpoint, then this pull
	// replays the same request id and writes the grant.
	ErrTokenBankInsufficient = errors.New("token bank: withdrawal exceeds the automatic cap")
	// ErrTokenBankGrantPending means the ledger was debited but the grant was
	// not confirmed. The caller must retry the same request id.
	ErrTokenBankGrantPending = errors.New("token bank grant was not confirmed; retry the same request id")
	// ErrTokenBankGrantGroupMismatch means this request id already has a grant
	// on a different service group. A retry must not report success for the
	// group it asked for: the credits are on the original group, and a new
	// request id would debit the account again.
	ErrTokenBankGrantGroupMismatch = errors.New("token bank grant is already issued to a different service group")
	// ErrTokenBankGroupNotCharged means the configured auto-withdraw group is
	// not one this request bills. The bank was not contacted. Another model on
	// the same request may still charge that group.
	ErrTokenBankGroupNotCharged = errors.New("token bank group is not charged by this request")
)

// tokenBankGrantMu serializes grant writes and the auto-seq counter on this
// process. The registry document is one JSON blob, so two overlapping pulls
// would otherwise drop one of the grants.
var tokenBankGrantMu sync.Mutex

// TokenBankCenterWithdraw is what a hub sends when it pulls credits.
type TokenBankCenterWithdraw struct {
	Email          string
	RequestID      string
	ServiceGroupID string
	AmountMicro    int64
	Manual         bool
	Kind           string
	LinkID         string
}

// TokenBankCenterWithdrawal is the idempotent debit result.
type TokenBankCenterWithdrawal struct {
	Created     bool
	RequestID   string
	AmountMicro int64
	GrantID     string
	Status      string
	HubID       string
}

// TokenBankCenter is the HubCenter half of a pull. The hub validates its own
// service group before calling WithdrawTokenBank, then confirms the grant.
type TokenBankCenter interface {
	WithdrawTokenBank(ctx context.Context, req TokenBankCenterWithdraw) (TokenBankCenterWithdrawal, error)
	FinishTokenBankGrant(ctx context.Context, requestID, grantID string, reissue bool) error
}

// TokenBankPullResult is the grant a pull left on this hub.
type TokenBankPullResult struct {
	GrantID     string
	AmountMicro int64
	RequestID   string
	Created     bool
}

// TokenBankAutoSettings controls the background pull. A missing settings
// document means enabled. ThresholdMicro is kept so a previously stored
// document still parses; the pull does not use it as a top-up target.
// MaxPerWithdrawMicro is the local ceiling for one automatic pull, in
// microcredits. Zero means no local ceiling: HubCenter still applies the
// per-machine 1/N share.
type TokenBankAutoSettings struct {
	Enabled             bool   `json:"enabled"`
	ThresholdMicro      int64  `json:"threshold_micro"`
	ServiceGroupID      string `json:"service_group_id"`
	MaxPerWithdrawMicro int64  `json:"max_per_withdraw_micro,omitempty"`
}

// TokenBankGrantCardID is the registry card id for one pull. It is the
// request id with a fixed prefix, so a retry finds the grant the first
// call wrote.
func TokenBankGrantCardID(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	return tokenBankGrantCardPrefix + requestID
}

// TokenBankGrantIDForRequest returns the local grant already stored for this
// pull. Empty means this hub has not written it. HubCenter can still be
// waiting on the confirm call; a later credit balance does not mean that
// confirm happened.
func TokenBankGrantIDForRequest(reg *Registry, requestID string) string {
	if reg == nil {
		return ""
	}
	cardID := TokenBankGrantCardID(requestID)
	if cardID == "" {
		return ""
	}
	for i := range reg.Grants {
		grant := &reg.Grants[i]
		if strings.TrimSpace(grant.Source) != TokenBankGrantSource {
			continue
		}
		if strings.TrimSpace(grant.CardID) != cardID {
			continue
		}
		return strings.TrimSpace(grant.ID)
	}
	return ""
}

// IssueTokenBankGrant writes one permanent Source=token_bank grant.
//
// It is idempotent on requestID: a second call returns the same grant id and
// does not add credits again. A missing service group returns
// ErrTokenBankServiceGroupMissing instead of skipping the grant.
func IssueTokenBankGrant(ctx context.Context, system SystemSettingsRepository, userID, email, serviceGroupID, requestID string, amountMicro int64) (grantID string, created bool, err error) {
	tokenBankGrantMu.Lock()
	defer tokenBankGrantMu.Unlock()
	return issueTokenBankGrantLocked(ctx, system, userID, email, serviceGroupID, requestID, amountMicro)
}

func issueTokenBankGrantLocked(ctx context.Context, system SystemSettingsRepository, userID, email, serviceGroupID, requestID string, amountMicro int64) (string, bool, error) {
	if system == nil {
		return "", false, fmt.Errorf("token bank grant requires settings")
	}
	requestID = strings.TrimSpace(requestID)
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if requestID == "" || serviceGroupID == "" {
		return "", false, fmt.Errorf("token bank grant requires request id and service group")
	}
	if amountMicro <= 0 {
		return "", false, fmt.Errorf("token bank grant amount must be positive")
	}
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return "", false, fmt.Errorf("token bank grant requires a user")
	}
	var grantID string
	var created bool
	err := WithServiceRegistryMutation(func() error {
		reg, err := LoadRegistry(ctx, system)
		if err != nil {
			return err
		}
		if reg.FindModelServiceGroup(serviceGroupID) == nil {
			return fmt.Errorf("%w: %s", ErrTokenBankServiceGroupMissing, serviceGroupID)
		}
		cardID := TokenBankGrantCardID(requestID)
		for i := range reg.Grants {
			grant := reg.Grants[i]
			if strings.TrimSpace(grant.Source) != TokenBankGrantSource {
				continue
			}
			if strings.TrimSpace(grant.CardID) != cardID {
				continue
			}
			// The card id is the request id. A later call that names another
			// service group still finds this grant and must not describe that
			// other group as funded. FindModelServiceGroup matches case-insensitively,
			// so the comparison does too.
			if !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) {
				grantID = grant.ID
				return fmt.Errorf("%w: %s", ErrTokenBankGrantGroupMismatch, strings.TrimSpace(grant.ServiceGroupID))
			}
			grantID = grant.ID
			return nil
		}
		now := time.Now().UTC()
		id := NewID("grant")
		reg.Grants = append(reg.Grants, Grant{
			ID:             id,
			UserID:         owner.UserID,
			Email:          owner.Email,
			ServiceGroupID: serviceGroupID,
			Source:         TokenBankGrantSource,
			CardID:         cardID,
			StartsAt:       now,
			ExpiresAt:      tokenBankGrantExpiresAt,
			CreatedAt:      now,
			Permanent:      true,
			CreditsTotal:   float64(amountMicro) / float64(tokenBankMicroPerCredit),
		})
		if err := SaveRegistry(ctx, system, reg); err != nil {
			return err
		}
		grantID = id
		created = true
		return nil
	})
	return grantID, created, err
}

// PullTokenBankGrant debits HubCenter and writes the local grant.
//
// The local service group is checked before the debit. If the debit succeeds
// and the grant write does not, the error is ErrTokenBankGrantPending and a
// retry with the same request id does not debit again.
func PullTokenBankGrant(ctx context.Context, system SystemSettingsRepository, client TokenBankCenter, userID, email, serviceGroupID, requestID string, amountMicro int64, manual bool, kind, linkID string) (TokenBankPullResult, error) {
	var zero TokenBankPullResult
	if client == nil {
		return zero, fmt.Errorf("token bank pull requires a hub center client")
	}
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	requestID = strings.TrimSpace(requestID)
	if serviceGroupID == "" || requestID == "" {
		return zero, fmt.Errorf("token bank pull requires request id and service group")
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return zero, err
	}
	if reg.FindModelServiceGroup(serviceGroupID) == nil {
		return zero, fmt.Errorf("%w: %s", ErrTokenBankServiceGroupMissing, serviceGroupID)
	}
	withdrawal, err := client.WithdrawTokenBank(ctx, TokenBankCenterWithdraw{
		Email:          email,
		RequestID:      requestID,
		ServiceGroupID: serviceGroupID,
		AmountMicro:    amountMicro,
		Manual:         manual,
		Kind:           kind,
		LinkID:         linkID,
	})
	if err != nil {
		return zero, err
	}
	if withdrawal.AmountMicro <= 0 {
		return zero, ErrTokenBankNothingToWithdraw
	}
	grantID, created, err := IssueTokenBankGrant(ctx, system, userID, email, serviceGroupID, requestID, withdrawal.AmountMicro)
	if err != nil {
		result := TokenBankPullResult{RequestID: requestID, AmountMicro: withdrawal.AmountMicro, GrantID: grantID}
		if errors.Is(err, ErrTokenBankGrantGroupMismatch) {
			// Bind the original grant before refusing, so a group change
			// during a pending retry does not leave the debit unconfirmed.
			// The refusal stays a mismatch: the requested group was not funded.
			if grantID != "" && withdrawal.GrantID != grantID {
				reissue := withdrawal.GrantID != "" && withdrawal.GrantID != grantID
				if finErr := client.FinishTokenBankGrant(ctx, requestID, grantID, reissue); finErr != nil {
					return result, fmt.Errorf("%w: %v", ErrTokenBankGrantPending, finErr)
				}
			}
			return result, err
		}
		return result, fmt.Errorf("%w: %v", ErrTokenBankGrantPending, err)
	}
	result := TokenBankPullResult{
		GrantID:     grantID,
		AmountMicro: withdrawal.AmountMicro,
		RequestID:   requestID,
		Created:     created,
	}
	if withdrawal.GrantID == grantID && !created {
		return result, nil
	}
	reissue := withdrawal.GrantID != "" && withdrawal.GrantID != grantID
	if err := client.FinishTokenBankGrant(ctx, requestID, grantID, reissue); err != nil {
		return result, fmt.Errorf("%w: %v", ErrTokenBankGrantPending, err)
	}
	return result, nil
}

// LoadTokenBankAutoSettings returns the background-pull policy. An empty
// document keeps the pull on. The stored threshold is not a top-up target.
func LoadTokenBankAutoSettings(ctx context.Context, system SystemSettingsRepository) (TokenBankAutoSettings, error) {
	cfg := TokenBankAutoSettings{Enabled: true, ThresholdMicro: TokenBankDefaultAutoThresholdMicro}
	if system == nil {
		return cfg, fmt.Errorf("token bank auto settings require a store")
	}
	raw, err := system.Get(ctx, tokenBankAutoSettingsKey)
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	var stored TokenBankAutoSettings
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return cfg, fmt.Errorf("parse token bank auto settings: %w", err)
	}
	if stored.ThresholdMicro < 0 {
		stored.ThresholdMicro = 0
	}
	if stored.MaxPerWithdrawMicro < 0 {
		stored.MaxPerWithdrawMicro = 0
	}
	return stored, nil
}

// SaveTokenBankAutoMaxPerWithdraw stores the local ceiling for one automatic
// pull. Zero clears it. Other fields already in the document stay as they
// were. A hub that has no document yet keeps the pull enabled: an absent
// enabled field would otherwise load as off.
func SaveTokenBankAutoMaxPerWithdraw(ctx context.Context, system SystemSettingsRepository, maxMicro int64) error {
	if system == nil {
		return fmt.Errorf("token bank auto settings require a store")
	}
	if maxMicro < 0 {
		return fmt.Errorf("token bank auto withdraw cap must not be negative")
	}
	raw, err := system.Get(ctx, tokenBankAutoSettingsKey)
	if err != nil {
		return err
	}
	doc := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return fmt.Errorf("parse token bank auto settings: %w", err)
		}
		// JSON null unmarshals as a nil map. Writing a key on it panics.
		if doc == nil {
			doc = map[string]any{"enabled": true}
		}
	} else {
		doc["enabled"] = true
	}
	if maxMicro == 0 {
		delete(doc, "max_per_withdraw_micro")
	} else {
		doc["max_per_withdraw_micro"] = maxMicro
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return system.Set(ctx, tokenBankAutoSettingsKey, string(encoded))
}

// ResolveTokenBankServiceGroup picks the group a pull will fund.
//
// A configured id must exist. An empty id resolves only when the hub has
// exactly one group that is not a built-in fallback, so a multi-group hub
// cannot guess.
func ResolveTokenBankServiceGroup(ctx context.Context, system SystemSettingsRepository, configured string) (string, error) {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return "", err
	}
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if reg.FindModelServiceGroup(configured) == nil {
			return "", fmt.Errorf("%w: %s", ErrTokenBankServiceGroupMissing, configured)
		}
		return configured, nil
	}
	ids := make([]string, 0, 1)
	for _, group := range reg.ModelServiceGroups {
		id := strings.TrimSpace(group.ID)
		if id == "" || IsBuiltinModelServiceGroupID(id) || strings.EqualFold(id, SystemFreeServiceGroupID) {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return "", nil
}

// UserHasAvailableCredits reports whether this user can still be served
// without another token-bank withdrawal. A new-user period window counts.
// So does a real free route or an unlimited grant on a group that still exists.
// Credits on a deleted group do not. The builtin fallback group has no model
// permissions, so it is not a route. A queued unlimited grant counts when
// billing would start it now. In-flight holds are not spent credits.
func UserHasAvailableCredits(ctx context.Context, reg *Registry, userID, email string, now time.Time) bool {
	if reg == nil {
		return false
	}
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return false
	}
	// nil resolver: user bindings, grants, and defaults are in the registry.
	// A group-chain lookup is not available on the auto-pull loop. Missing
	// those bindings can only miss a free route, and the grant path still runs.
	ids, _, err := effectiveServiceGroupIDsForOwner(ctx, reg, nil, owner, now)
	if err != nil {
		// Do not move credits when the route set cannot be read.
		return true
	}
	// Each real group is judged on its own. A combined set lets a period limit
	// on one group early-start a future grant on another, and it also counts
	// a balance on the builtin fallback, which cannot authorize a model.
	routable := serviceGroupIDsWithoutBuiltinFallback(ids)
	if len(routable) == 0 {
		return false
	}
	total := 0.0
	for _, id := range routable {
		serviceGroupSet := map[string]struct{}{strings.ToLower(strings.TrimSpace(id)): {}}
		total += spendableGrantCredits(reg, owner, serviceGroupSet, now)
	}
	if roundCredits(total) > 0 {
		return true
	}
	if hasUnrestrictedFreeServiceGroup(reg, owner, routable, now) {
		return true
	}
	for _, id := range routable {
		if hasPeriodLimitedNewUserLimitCardForServiceGroup(reg, owner, id, now) {
			continue
		}
		one := []string{id}
		// Same admission paths as billing, without subtracting in-flight holds.
		// A queued unlimited grant adds no spendable balance, but billing starts
		// it immediately once the current point card on this group is exhausted.
		if hasUnmeteredUnlimitedActiveGrantForServiceGroups(reg, owner, one, now) ||
			hasEarlyStartableUnmeteredUnlimitedGrant(reg, owner, one, now) {
			return true
		}
	}
	return false
}

// serviceGroupIDsWithoutBuiltinFallback drops the empty builtin group.
// SaveRegistry inserts it and points unbound users at it. It cannot authorize
// a model, so it must not keep a token-bank withdrawal from running.
func serviceGroupIDsWithoutBuiltinFallback(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if IsBuiltinModelServiceGroupID(id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// TokenBankRemainingMicro is the still-spendable token-bank grant balance for
// one user and service group, in microcredits. It does not use roundCredits.
func TokenBankRemainingMicro(reg *Registry, email, serviceGroupID string, now time.Time) int64 {
	if reg == nil {
		return 0
	}
	owner := newUserAccountRef("", email)
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	var sum int64
	for _, grant := range reg.Grants {
		if strings.TrimSpace(grant.Source) != TokenBankGrantSource || grant.Frozen {
			continue
		}
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) {
			continue
		}
		if !grantIsValidAt(grant, now) {
			continue
		}
		remaining := grant.CreditsTotal - grant.CreditsUsed
		if remaining <= 0 {
			continue
		}
		sum += int64(math.Round(remaining * float64(tokenBankMicroPerCredit)))
	}
	return sum
}

// SumTokenBankGrantMicro converts every token-bank grant for the email back
// to microcredits. The reverse of the single float conversion, so reconcile
// can compare it with the integer ledger.
func SumTokenBankGrantMicro(grants []Grant, email string) int64 {
	owner := newUserAccountRef("", email)
	var sum int64
	for _, grant := range grants {
		if strings.TrimSpace(grant.Source) != TokenBankGrantSource || grant.Frozen {
			continue
		}
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.CreditsTotal <= 0 {
			continue
		}
		sum += int64(math.Round(grant.CreditsTotal * float64(tokenBankMicroPerCredit)))
	}
	return sum
}

// TokenBankReconcile reports grant micro minus withdrawn micro.
// within is true when the two sides differ by at most one micro.
func TokenBankReconcile(grantMicro, withdrawnMicro int64) (delta int64, within bool) {
	delta = grantMicro - withdrawnMicro
	return delta, delta >= -1 && delta <= 1
}

// TokenBankAutoRequestID is stable for one user, group, and sequence. The
// stored sequence advances only after that id's grant is confirmed, so a crash
// retries the same id and does not debit twice. A later id can be debited
// while an earlier confirm is still open; that later id is just as stable.
func TokenBankAutoRequestID(hubID, email, serviceGroupID string, seq int64) string {
	if seq < 0 {
		seq = 0
	}
	return fmt.Sprintf("tbk-auto:%s:%s:%s:%d", strings.TrimSpace(hubID), normalizeEmail(email), strings.TrimSpace(serviceGroupID), seq)
}

// TokenBankAutoSeq is the next automatic request sequence for this user and group.
func TokenBankAutoSeq(ctx context.Context, system SystemSettingsRepository, email, serviceGroupID string) (int64, error) {
	tokenBankGrantMu.Lock()
	defer tokenBankGrantMu.Unlock()
	doc, err := loadTokenBankAutoSeq(ctx, system)
	if err != nil {
		return 0, err
	}
	return doc[tokenBankAutoSeqMapKey(email, serviceGroupID)], nil
}

// AdvanceTokenBankAutoSeq moves the sequence forward only from the value the
// caller just used. A stale caller does not skip an in-flight id.
func AdvanceTokenBankAutoSeq(ctx context.Context, system SystemSettingsRepository, email, serviceGroupID string, from int64) error {
	tokenBankGrantMu.Lock()
	defer tokenBankGrantMu.Unlock()
	doc, err := loadTokenBankAutoSeq(ctx, system)
	if err != nil {
		return err
	}
	key := tokenBankAutoSeqMapKey(email, serviceGroupID)
	if doc[key] != from {
		return nil
	}
	doc[key] = from + 1
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return system.Set(ctx, tokenBankAutoSeqKey, string(raw))
}

func tokenBankAutoSeqMapKey(email, serviceGroupID string) string {
	return normalizeEmail(email) + "\n" + strings.TrimSpace(serviceGroupID)
}

func loadTokenBankAutoSeq(ctx context.Context, system SystemSettingsRepository) (map[string]int64, error) {
	if system == nil {
		return nil, fmt.Errorf("token bank auto sequence requires settings")
	}
	raw, err := system.Get(ctx, tokenBankAutoSeqKey)
	if err != nil {
		return nil, err
	}
	doc := map[string]int64{}
	if strings.TrimSpace(raw) == "" {
		return doc, nil
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("parse token bank auto sequence: %w", err)
	}
	return doc, nil
}
