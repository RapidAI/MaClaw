package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Token Bank settlement: the bridge from a proxied request to a credited owner.
//
// The money rules live here rather than in the proxy because this is the layer
// that can write the usage row and the ledger row in one transaction. A
// settlement that recorded the usage but failed to credit, or credited twice,
// would be invisible to every later reconciliation — the ledger is the only
// record of what an owner earned.
//
// The formula is §5 and it is applied exactly once per call:
//
//	base  = Σ ceil(tokens / 10000 × unit_per_10k × 1e6)
//	gross = round(base × tier_multiplier)
//	fee   = round(gross × fee_rate)          (default 10%)
//	net   = gross − fee
//	net   = max(0, min(net, charged))        (anti-inversion, §5 ⑥)

// TokenBankSettlement is one settled call.
type TokenBankSettlement struct {
	// RequestID is the caller's idempotency key. Together with ShareID and
	// ModelName it forms the usage row's unique key and the ledger row's id, so
	// a retry converges on the same movement instead of crediting twice.
	RequestID string
	ShareID   string
	OwnerID   string
	// ShareDisplayName is a snapshot: taking a share out deletes the row, and
	// without it the owner's history would show unnamed records.
	ShareDisplayName string
	ModelName        string

	ConsumerHubID    string
	ConsumerTenantID string

	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64

	// Tier and TierMultiplier come from token_bank_models at settlement time.
	// They are stored on the usage row as a snapshot because an admin regrade
	// must not retroactively change what an already-settled call earned.
	Tier           string
	TierMultiplier float64

	// FeeRate is the platform's cut of gross. It is snapshotted for the same
	// reason: changing the rate must not rewrite history.
	FeeRate float64

	// ChargedMicro is what the CONSUMER paid, in microcredits. It is the upper
	// bound of net (the anti-inversion rule). Zero means the consumer paid
	// nothing — a free route — and the owner's earning is clamped to zero, which
	// is recorded with net_clamped so it is visible rather than silent.
	ChargedMicro int64

	// SelfUse marks a call whose consumer hub is linked only to the owner.
	// The earning is still credited. ChargedMicro is the consumer line and
	// NetMicro is the sharer line.
	SelfUse bool

	// Unit prices used, recorded so the detail view can show the arithmetic.
	UnitInputPer10K      float64
	UnitOutputPer10K     float64
	UnitCachedReadPer10K float64
	UnitCacheWritePer10K float64
	PriceBookID          string

	CreatedAt time.Time
}

// TokenBankSettlementResult is what the caller needs to log or surface.
type TokenBankSettlementResult struct {
	UsageID    int64
	GrossMicro int64
	FeeMicro   int64
	NetMicro   int64
	// NetClamped reports that the anti-inversion rule reduced the payout. It is
	// the difference between "your model earned nothing" and "your model earned
	// nothing because the group sold it for free", which is a support question
	// the operator has to be able to answer.
	NetClamped bool
	// Applied reports whether this call settled the usage. False means the
	// request was already settled (an idempotent replay).
	Applied bool
	// CapHit is "daily", "monthly", or "anomaly" when this insert crossed a
	// share limit. A replay leaves it empty.
	CapHit string
}

// ComputeTokenBankSettlement applies §5 to the token counts. It is exported so
// the same arithmetic is reachable from tests and from the detail view that
// shows the owner the calculation, without a second implementation.
func ComputeTokenBankSettlement(inputTokens, outputTokens, cachedInputTokens, cacheWriteTokens int64, unitIn, unitOut, unitCachedRead, unitCacheWrite, tierMultiplier, feeRate float64) (gross, fee, net int64) {
	base := tokenBankTokenCostMicro(inputTokens, unitIn) +
		tokenBankTokenCostMicro(outputTokens, unitOut) +
		tokenBankTokenCostMicro(cachedInputTokens, unitCachedRead) +
		tokenBankTokenCostMicro(cacheWriteTokens, unitCacheWrite)
	if tierMultiplier <= 0 {
		tierMultiplier = 1
	}
	gross = roundTokenBankMicro(float64(base) * tierMultiplier)
	if feeRate < 0 {
		feeRate = 0
	}
	if feeRate > 1 {
		feeRate = 1
	}
	fee = roundTokenBankMicro(float64(gross) * feeRate)
	net = gross - fee
	if net < 0 {
		net = 0
	}
	return gross, fee, net
}

// tokenBankTokenCostMicro is ceil(tokens / 10000 × unitPer10K) in microcredits.
//
// Each leg rounds UP independently (§5 ②). Rounding the sum instead would let a
// request that consumed a fractional credit on every leg settle to zero, and
// rounding down would systematically under-pay the owner.
func tokenBankTokenCostMicro(tokens int64, unitPer10K float64) int64 {
	if tokens <= 0 || unitPer10K <= 0 {
		return 0
	}
	value := float64(tokens) / 10000.0 * unitPer10K * float64(TokenBankMicrocreditsPerCredit)
	if value <= 0 {
		return 0
	}
	if value >= math.MaxInt64 {
		return math.MaxInt64
	}
	// A tiny epsilon guards the float boundary: 3.7020000000000004 must not
	// round up to a whole extra microcredit.
	return int64(math.Ceil(value - 1e-9))
}

func roundTokenBankMicro(value float64) int64 {
	if value <= 0 {
		return 0
	}
	if value >= math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(math.Round(value))
}

// TokenBankMicrocreditsPerCredit mirrors llmpool's ledger precision. It is
// repeated rather than imported so this package does not depend on llmpool for
// a constant; the two must agree, and a test pins that.
const TokenBankMicrocreditsPerCredit int64 = 1_000_000

// SettleTokenBankUsage records the usage row and credits the owner, in one
// transaction.
//
// The two writes are one unit of work on purpose. A usage row without a ledger
// entry is an owner who was not paid for work that provably happened; a ledger
// entry without a usage row cannot be explained to the owner. Committing one
// without the other trades a visible bug for an invisible one.
func (r *TokenBankRepo) SettleTokenBankUsage(ctx context.Context, in TokenBankSettlement) (TokenBankSettlementResult, error) {
	var out TokenBankSettlementResult
	in.RequestID = strings.TrimSpace(in.RequestID)
	in.ShareID = strings.TrimSpace(in.ShareID)
	in.OwnerID = strings.TrimSpace(in.OwnerID)
	in.ModelName = strings.TrimSpace(in.ModelName)
	if in.RequestID == "" || in.ShareID == "" || in.ModelName == "" {
		return out, fmt.Errorf("token bank settlement requires request id, share id and model name")
	}
	// A settlement with no owner cannot be credited. It is not an error at this
	// level — the share may have been withdrawn between dispatch and settle —
	// but there is nothing to do, so report "not applied" and let the caller
	// decide whether to log.
	if in.OwnerID == "" {
		return out, nil
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	if in.Tier == "" {
		in.Tier = "mid"
	}
	if in.TierMultiplier <= 0 {
		in.TierMultiplier = 1
	}

	gross, fee, net := ComputeTokenBankSettlement(
		in.InputTokens, in.OutputTokens, in.CachedInputTokens, in.CacheWriteTokens,
		in.UnitInputPer10K, in.UnitOutputPer10K, in.UnitCachedReadPer10K, in.UnitCacheWritePer10K,
		in.TierMultiplier, in.FeeRate)

	// Anti-inversion (§5 ⑥): the platform never pays out more than it charged.
	// Zero charged (a free route) clamps the payout to zero.
	clamped := false
	if net > in.ChargedMicro {
		net = in.ChargedMicro
		clamped = true
	}
	if net < 0 {
		net = 0
		clamped = true
	}
	out.GrossMicro = gross
	out.FeeMicro = fee
	out.NetMicro = net
	out.NetClamped = clamped

	now := in.CreatedAt.UTC()
	stamp := now.Format(time.RFC3339)
	formula, _ := json.Marshal(map[string]any{
		"input_tokens":             in.InputTokens,
		"output_tokens":            in.OutputTokens,
		"cached_input_tokens":      in.CachedInputTokens,
		"cache_write_tokens":       in.CacheWriteTokens,
		"unit_in_per_10k":          in.UnitInputPer10K,
		"unit_out_per_10k":         in.UnitOutputPer10K,
		"unit_cached_read_per_10k": in.UnitCachedReadPer10K,
		"unit_cache_write_per_10k": in.UnitCacheWritePer10K,
		"tier":                     in.Tier,
		"tier_multiplier":          in.TierMultiplier,
		"fee_rate":                 in.FeeRate,
		"gross_micro":              gross,
		"fee_micro":                fee,
		"net_micro":                net,
		"charged_micro":            in.ChargedMicro,
		"net_clamped":              clamped,
		"self_use":                 in.SelfUse,
	})

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("begin token bank settlement: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO token_bank_usage (
			request_id, share_id, owner_user_id, share_display_name, model_name,
			consumer_hub_id, consumer_tenant_id,
			input_tokens, output_tokens, cached_input_tokens, cache_write_tokens,
			unit_in_credits_per_10k, unit_out_credits_per_10k,
			price_book_id, tier, tier_multiplier, fee_rate,
			gross_micro, fee_micro, net_micro, charged_micro, net_clamped, self_use, formula_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.RequestID, in.ShareID, in.OwnerID, strings.TrimSpace(in.ShareDisplayName), in.ModelName,
		strings.TrimSpace(in.ConsumerHubID), strings.TrimSpace(in.ConsumerTenantID),
		in.InputTokens, in.OutputTokens, in.CachedInputTokens, in.CacheWriteTokens,
		in.UnitInputPer10K, in.UnitOutputPer10K,
		strings.TrimSpace(in.PriceBookID), in.Tier, in.TierMultiplier, in.FeeRate,
		gross, fee, net, in.ChargedMicro, boolToInt(clamped), boolToInt(in.SelfUse), string(formula), stamp)
	if err != nil {
		return out, fmt.Errorf("insert token bank usage: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return out, err
	}
	if rows == 0 {
		// Already settled. Read the existing row back so the caller gets the
		// canonical amounts rather than the ones it just computed — a retry
		// after a rate change must not report a figure that was never applied.
		if err := tx.Commit(); err != nil {
			return out, err
		}
		return r.loadSettledUsage(ctx, in.RequestID, in.ShareID, in.ModelName)
	}

	usageID, err := res.LastInsertId()
	if err != nil {
		// LastInsertId is not supported everywhere; the row is still written,
		// so a missing id is cosmetic and must not roll back a credit.
		usageID = 0
	}
	out.UsageID = usageID
	out.Applied = true

	if net > 0 {
		// The ledger row's id is derived from the business key, so the HA
		// replication of this movement is idempotent by primary key (E1). A
		// random id would let three nodes each mint a distinct row for the same
		// call and triple the credit.
		ledgerID := tokenBankUsageLedgerID(in.RequestID, in.ShareID, in.ModelName)
		bizKey := tokenBankUsageBizKey(in.RequestID, in.ShareID, in.ModelName)
		ledgerRes, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ledgerID, in.OwnerID, TokenBankBucketEarned, net,
			bizKey, "usage", in.RequestID,
			in.ModelName, stamp)
		if err != nil {
			return out, fmt.Errorf("insert token bank settlement ledger: %w", err)
		}
		ledgerRows, err := ledgerRes.RowsAffected()
		if err != nil {
			return out, err
		}
		if ledgerRows > 0 {
			if err := bumpTokenBankAccount(ctx, tx, in.OwnerID, TokenBankBucketEarned, net, now); err != nil {
				return out, err
			}
		}
	}

	// The account ledger is the balance. These two columns are the per-share and
	// per-model figures the admin share list and the owner payload read. A
	// replay returned above, so this cannot double-count. A taken-out share has
	// no row left; zero updated rows is success and the usage row still stands.
	if _, err := tx.ExecContext(ctx,
		`UPDATE token_bank_models
		    SET used_input_tokens = used_input_tokens + ?,
		        used_output_tokens = used_output_tokens + ?,
		        earned_micro = earned_micro + ?
		  WHERE share_id = ? AND lower(model_name) = lower(?)`,
		in.InputTokens, in.OutputTokens, net, in.ShareID, in.ModelName); err != nil {
		return out, fmt.Errorf("update token bank model usage: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE token_bank_shares
		    SET total_earned_micro = total_earned_micro + ?
		  WHERE id = ?`,
		net, in.ShareID); err != nil {
		return out, fmt.Errorf("update token bank share earnings: %w", err)
	}
	hit, err := tokenBankCapHit(ctx, tx, in.ShareID, now)
	if err != nil {
		return out, err
	}
	out.CapHit = hit

	if err := tx.Commit(); err != nil {
		return out, err
	}

	// Publish after the commit, for the same reason AppendLedger does: a row
	// handed to a peer before the commit could reach it and then roll back
	// locally, and the peer cannot tell that from a real movement.
	if net > 0 {
		r.publish(TokenBankLedgerEntry{
			ID:          tokenBankUsageLedgerID(in.RequestID, in.ShareID, in.ModelName),
			UserID:      in.OwnerID,
			Bucket:      TokenBankBucketEarned,
			AmountMicro: net,
			BizKey:      tokenBankUsageBizKey(in.RequestID, in.ShareID, in.ModelName),
			RefType:     "usage",
			RefID:       in.RequestID,
			Note:        in.ModelName,
		}, now)
	}
	return out, nil
}

// loadSettledUsage returns the amounts an earlier settlement actually applied.
func (r *TokenBankRepo) loadSettledUsage(ctx context.Context, requestID, shareID, modelName string) (TokenBankSettlementResult, error) {
	var out TokenBankSettlementResult
	var clamped int
	err := r.read.QueryRowContext(ctx,
		`SELECT id, gross_micro, fee_micro, net_micro, net_clamped
		   FROM token_bank_usage
		  WHERE request_id = ? AND share_id = ? AND model_name = ?`,
		requestID, shareID, modelName).Scan(&out.UsageID, &out.GrossMicro, &out.FeeMicro, &out.NetMicro, &clamped)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		return out, err
	}
	out.NetClamped = clamped != 0
	out.Applied = false
	return out, nil
}

// tokenBankUsageBizKey is the business key of one settled call. It is written
// to the ledger's biz_key column, which carries a UNIQUE index — so it is the
// second half of the settlement's idempotency guarantee, alongside the derived
// primary key.
//
// It includes the model for the same reason the primary key does: the usage
// row's unique key is (request_id, share_id, model_name), so one request can
// legitimately settle against several models of one share and each of those is
// a separate earning. Omitting the model would let the unique biz_key index
// swallow the second model's credit as a duplicate.
//
// The shape is pinned by TestTokenBankEveryLedgerWriteIsPublished, which reads
// the ledger back and re-derives this key to check the movement is explainable.
func tokenBankUsageBizKey(requestID, shareID, modelName string) string {
	return "usage:" + strings.TrimSpace(requestID) + ":" + strings.TrimSpace(shareID) + ":" + strings.TrimSpace(modelName)
}

// tokenBankUsageLedgerID derives the ledger row id for one settled call. It
// must be a pure function of the business key: the HA replay path rebuilds it
// from the ledger row alone, and an id that depended on anything else would
// make a replay create a second credit.
func tokenBankUsageLedgerID(requestID, shareID, modelName string) string {
	return "usage_" + hashTokenBankKey(tokenBankUsageBizKey(requestID, shareID, modelName))
}

// TokenBankUsageRow is one settled call as read back for the detail view.
type TokenBankUsageRow struct {
	ID                int64
	RequestID         string
	ShareID           string
	OwnerUserID       string
	ShareDisplayName  string
	ModelName         string
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	ConsumerHubID     string
	ConsumerTenantID  string
	Tier              string
	TierMultiplier    float64
	FeeRate           float64
	GrossMicro        int64
	FeeMicro          int64
	NetMicro          int64
	ChargedMicro      int64
	NetClamped        bool
	SelfUse           bool
	FormulaJSON       string
	CreatedAt         time.Time
}

// ListUsage returns an owner's settled calls, newest first. Taking a share out
// deletes its row but not its history, which is why owner_user_id is stored on
// the usage row rather than joined.
func (r *TokenBankRepo) ListUsage(ctx context.Context, ownerUserID string, limit int, shareID string) ([]TokenBankUsageRow, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, request_id, share_id, owner_user_id, share_display_name, model_name,
	                 consumer_hub_id, consumer_tenant_id,
	                 input_tokens, output_tokens, cached_input_tokens, cache_write_tokens,
	                 tier, tier_multiplier, fee_rate, gross_micro, fee_micro, net_micro,
	                 charged_micro, net_clamped, self_use, formula_json, created_at
	            FROM token_bank_usage WHERE owner_user_id = ?`
	args := []any{ownerUserID}
	if v := strings.TrimSpace(shareID); v != "" {
		query += ` AND share_id = ?`
		args = append(args, v)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list token bank usage: %w", err)
	}
	defer rows.Close()
	out := []TokenBankUsageRow{}
	for rows.Next() {
		var row TokenBankUsageRow
		var clamped, selfUse int
		var created string
		if err := rows.Scan(&row.ID, &row.RequestID, &row.ShareID, &row.OwnerUserID, &row.ShareDisplayName,
			&row.ModelName, &row.ConsumerHubID, &row.ConsumerTenantID,
			&row.InputTokens, &row.OutputTokens, &row.CachedInputTokens, &row.CacheWriteTokens,
			&row.Tier, &row.TierMultiplier, &row.FeeRate, &row.GrossMicro, &row.FeeMicro, &row.NetMicro,
			&row.ChargedMicro, &clamped, &selfUse, &row.FormulaJSON, &created); err != nil {
			return nil, err
		}
		row.NetClamped = clamped != 0
		row.SelfUse = selfUse != 0
		row.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, row)
	}
	return out, rows.Err()
}

// SumUsageMicro totals an owner's settled earnings over the usage ledger. It is
// a read-side helper for the owner card; the authoritative balance is still
// Balance, which sums the ledger.
func (r *TokenBankRepo) SumUsageMicro(ctx context.Context, ownerUserID string, since time.Time) (gross, fee, net int64, err error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return 0, 0, 0, nil
	}
	query := `SELECT COALESCE(SUM(gross_micro),0), COALESCE(SUM(fee_micro),0), COALESCE(SUM(net_micro),0)
	            FROM token_bank_usage WHERE owner_user_id = ?`
	args := []any{ownerUserID}
	if !since.IsZero() {
		query += ` AND created_at >= ?`
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	if err := r.read.QueryRowContext(ctx, query, args...).Scan(&gross, &fee, &net); err != nil {
		return 0, 0, 0, fmt.Errorf("sum token bank usage: %w", err)
	}
	return gross, fee, net, nil
}

// SumSettledNetByShare totals settled net credits for the given shares.
// The share map is the whole share. The model map is keyed by share id, then
// by the lower-cased model name, so a casing difference between the usage row
// and the share row still lands on the model the admin list is showing.
//
// The admin share list reads this instead of the denormalized counters.
// Settlement maintains token_bank_shares.total_earned_micro and
// token_bank_models.earned_micro, but calls settled before that, and calls
// whose model row was removed, exist only in token_bank_usage.
func (r *TokenBankRepo) SumSettledNetByShare(ctx context.Context, shareIDs []string) (map[string]int64, map[string]map[string]int64, error) {
	shareNet := map[string]int64{}
	modelNet := map[string]map[string]int64{}
	if r == nil {
		return shareNet, modelNet, nil
	}
	ids := make([]any, 0, len(shareIDs))
	seen := make(map[string]bool, len(shareIDs))
	for _, id := range shareIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return shareNet, modelNet, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	rows, err := r.read.QueryContext(ctx,
		`SELECT share_id, lower(model_name), COALESCE(SUM(net_micro), 0)
		   FROM token_bank_usage
		  WHERE share_id IN (`+placeholders+`)
		  GROUP BY share_id, lower(model_name)`, ids...)
	if err != nil {
		return nil, nil, fmt.Errorf("sum token bank earnings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var shareID, model string
		var net int64
		if err := rows.Scan(&shareID, &model, &net); err != nil {
			return nil, nil, err
		}
		shareNet[shareID] += net
		byModel := modelNet[shareID]
		if byModel == nil {
			byModel = map[string]int64{}
			modelNet[shareID] = byModel
		}
		byModel[model] += net
	}
	return shareNet, modelNet, rows.Err()
}
