package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTokenBankNothingToWithdraw means the ledger says the user has nothing left
// that is not already withdrawn or frozen by an outstanding gift link.
var ErrTokenBankNothingToWithdraw = errors.New("token bank: nothing to withdraw")

// ErrTokenBankInsufficient means the requested amount exceeds what the caller is
// allowed to take right now. It is separate from ErrTokenBankNothingToWithdraw
// because the two need different HTTP answers and different client behaviour:
// "you have nothing" is final, "you asked for too much" is retryable with a
// smaller number.
var ErrTokenBankInsufficient = errors.New("token bank: insufficient available credits")

// ErrTokenBankWithdrawalMismatch means this request id was already used by a
// different account or hub. The stored amount stays hidden: another hub would
// otherwise mint a second grant for credits that were already pulled.
var ErrTokenBankWithdrawalMismatch = errors.New("token bank: withdrawal belongs to another account or hub")

// ErrTokenBankGiftWithdrawn means this gift link already produced a withdrawal.
// Settle is idempotent, so a second request id would debit whatever else is
// still available on the claimer's account and mint a second grant.
var ErrTokenBankGiftWithdrawn = errors.New("token bank: gift link already withdrawn")

// TokenBankWithdrawal is one grant issued to one hub. It doubles as the replay
// credential: a hub that lost its registry asks again with the same request_id
// and gets the first answer back instead of a second debit.
type TokenBankWithdrawal struct {
	ID          string
	RequestID   string
	UserID      string
	HubID       string
	AmountMicro int64
	GrantID     string
	// Kind is "self" for a withdrawal of the user's own earnings and "gift"
	// for credits claimed from somebody else's link (§14.7).
	Kind    string
	LinkID  string
	Status  string
	Created time.Time
}

const (
	TokenBankWithdrawStatusIssued   = "issued"   // debited, hub has not confirmed the grant yet
	TokenBankWithdrawStatusBound    = "bound"    // hub reported the grant id back
	TokenBankWithdrawStatusReissued = "reissued" // grant rebuilt after a hub reinstall
	// TokenBankWithdrawStatusPosted is a withdrawn ledger row whose withdrawal
	// record is not on this node. The ledger is replicated; the withdrawal
	// table is not. The balance already counts the debit, and the history
	// has to show it. There is no grant id on this copy.
	TokenBankWithdrawStatusPosted = "posted"
)

// TokenBankWithdrawRequest asks hubcenter to move credits into a hub grant.
type TokenBankWithdrawRequest struct {
	// RequestID is the idempotency key. It must be deterministic on the hub
	// side — "withdraw:<hub_id>:<seq>" or similar — because a hub that retries
	// after a crash has to produce the same value or it will be debited twice.
	RequestID string
	UserID    string
	HubID     string
	// AmountMicro is the amount the caller asked for. Zero means "as much as
	// allowed", which is what an automatic low-balance top-up wants; a positive
	// value is honoured exactly and is an error if it exceeds the cap below.
	//
	// Without this field the caller could only ever take the whole balance or
	// exactly 1/N, which makes a per-cycle budget impossible to express — a hub
	// asking to top up 2 credits out of 10 would have been handed all 10.
	AmountMicro int64
	// Manual withdrawals are an explicit user action and may take everything
	// available. Automatic ones are capped at 1/N so one machine cannot drain
	// a user who owns several hubs (E6).
	Manual bool
	Kind   string
	LinkID string
	Now    time.Time
}

// tokenBankWithdrawID and tokenBankWithdrawLedgerID derive both primary keys
// from the idempotency key. They must be pure functions of requestID, because
// three HA nodes (and a hub retrying after a crash) have to arrive at the same
// value or the same withdrawal gets recorded — and debited — more than once.
func tokenBankWithdrawID(requestID string) string {
	return "wd_" + hashTokenBankKey(requestID)
}

func tokenBankWithdrawLedgerID(requestID string) string {
	return "wdl_" + hashTokenBankKey(requestID)
}

func hashTokenBankKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16])
}

// AutoWithdrawLimitMicro splits the available credits across the user's hubs.
//
// A hub is self-hosted, so one user can own several, and an automatic
// withdrawal triggered by a low-balance alarm on one machine must not take the
// pool that the others are about to ask for. N is advisory: it changes as hubs
// register and deregister, and the doc explicitly says it need not be exact —
// the rule only has to prevent one machine from monopolising the balance.
func AutoWithdrawLimitMicro(availableMicro int64, hubCount int) int64 {
	if availableMicro <= 0 {
		return 0
	}
	n := hubCount
	if n < 1 {
		n = 1
	}
	return availableMicro / int64(n)
}

// refuseSecondGiftWithdraw stops a new request id from debiting a gift that
// already has a withdrawal. The same request id is handled by the replay above.
func refuseSecondGiftWithdraw(ctx context.Context, tx *sql.Tx, req TokenBankWithdrawRequest) error {
	if req.Kind != "gift" || req.LinkID == "" {
		return nil
	}
	var existingUser string
	err := tx.QueryRowContext(ctx,
		`SELECT user_id FROM token_bank_withdrawals WHERE kind = 'gift' AND link_id = ?`,
		req.LinkID).Scan(&existingUser)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup gift withdrawal: %w", err)
	}
	if strings.TrimSpace(existingUser) == "" {
		return fmt.Errorf("gift withdrawal %q has no account", req.LinkID)
	}
	return ErrTokenBankGiftWithdrawn
}

// replayWithdrawal reads the row for this request id after a unique-index
// race. ok is false when the row is absent or belongs to someone else; the
// caller then decides whether the conflict was the gift link.
func replayWithdrawal(ctx context.Context, tx *sql.Tx, req TokenBankWithdrawRequest) (*TokenBankWithdrawal, bool) {
	var existing TokenBankWithdrawal
	var existingCreated string
	err := tx.QueryRowContext(ctx,
		`SELECT id, request_id, user_id, hub_id, amount_micro, grant_id, kind, link_id, status, created_at
		 FROM token_bank_withdrawals WHERE request_id = ?`, req.RequestID).
		Scan(&existing.ID, &existing.RequestID, &existing.UserID, &existing.HubID,
			&existing.AmountMicro, &existing.GrantID, &existing.Kind, &existing.LinkID,
			&existing.Status, &existingCreated)
	if err != nil {
		return nil, false
	}
	if existing.UserID != req.UserID || (req.HubID != "" && existing.HubID != "" && existing.HubID != req.HubID) {
		return nil, false
	}
	existing.Created, _ = time.Parse(time.RFC3339, existingCreated)
	return &existing, true
}

// Withdraw debits the ledger and records the withdrawal in one transaction, and
// is idempotent on RequestID: a replay returns the first result with
// created=false and does not touch the ledger again. A gift link is also
// idempotent on the link itself: a different request id does not debit again,
// and the debit is the link amount rather than the amount in the request.
func (r *TokenBankRepo) Withdraw(ctx context.Context, req TokenBankWithdrawRequest) (*TokenBankWithdrawal, bool, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.HubID = strings.TrimSpace(req.HubID)
	if req.RequestID == "" || req.UserID == "" {
		return nil, false, fmt.Errorf("token bank withdrawal requires request id and user id")
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.LinkID = strings.TrimSpace(req.LinkID)
	if req.Kind == "" {
		req.Kind = "self"
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Replay first: the same account and hub, retrying or rebuilt after a
	// reinstall, must see the original amount rather than a second debit.
	// A different account or hub must not receive that amount. The row's hub
	// id is what IssueTokenBankGrant would spend, so handing it to another
	// machine mints a second grant.
	var existing TokenBankWithdrawal
	var existingCreated string
	err = tx.QueryRowContext(ctx,
		`SELECT id, request_id, user_id, hub_id, amount_micro, grant_id, kind, link_id, status, created_at
		 FROM token_bank_withdrawals WHERE request_id = ?`, req.RequestID).
		Scan(&existing.ID, &existing.RequestID, &existing.UserID, &existing.HubID,
			&existing.AmountMicro, &existing.GrantID, &existing.Kind, &existing.LinkID,
			&existing.Status, &existingCreated)
	if err == nil {
		existing.Created, _ = time.Parse(time.RFC3339, existingCreated)
		if existing.UserID != req.UserID || (req.HubID != "" && existing.HubID != "" && existing.HubID != req.HubID) {
			return nil, false, ErrTokenBankWithdrawalMismatch
		}
		// An empty stored hub id is not yet promised to a machine. The first
		// caller that names a hub claims the row here, so a second linked hub
		// cannot replay the same request id into a second local grant.
		// IssueTokenBankGrant writes that grant before BindGrantID, and bind
		// matches on hub_id, so an unclaimed empty row lets every later hub
		// mint its own grant for the one debit. An owner retry that still
		// omits hub_id keeps reading the row and does not claim it.
		if req.HubID != "" && existing.HubID == "" {
			res, err := tx.ExecContext(ctx,
				`UPDATE token_bank_withdrawals SET hub_id = ?
				 WHERE request_id = ? AND user_id = ? AND hub_id = ''`,
				req.HubID, req.RequestID, req.UserID)
			if err != nil {
				return nil, false, fmt.Errorf("claim token bank withdrawal hub: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return nil, false, err
			}
			if n == 0 {
				var claimed string
				if err := tx.QueryRowContext(ctx,
					`SELECT hub_id FROM token_bank_withdrawals WHERE request_id = ?`, req.RequestID).
					Scan(&claimed); err != nil {
					return nil, false, fmt.Errorf("reread token bank withdrawal hub: %w", err)
				}
				if claimed != req.HubID {
					return nil, false, ErrTokenBankWithdrawalMismatch
				}
			}
			existing.HubID = req.HubID
			if err := tx.Commit(); err != nil {
				return nil, false, err
			}
			return &existing, false, nil
		}
		return &existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("lookup token bank withdrawal: %w", err)
	}
	if err := refuseSecondGiftWithdraw(ctx, tx, req); err != nil {
		return nil, false, err
	}
	// A gift debit is the link's own amount. The request amount is not a
	// second balance: zero means "everything available" on a self withdrawal,
	// and a larger figure would take the rest of this account in the same
	// gift. Settlement has already moved the link amount onto the claimer.
	if req.Kind == "gift" {
		link, err := r.giftLinkByID(ctx, tx, req.LinkID)
		if err != nil {
			return nil, false, err
		}
		if link.Status != TokenBankGiftStatusSettled || link.ClaimedByUser != req.UserID || link.CreditsMicro <= 0 {
			return nil, false, ErrGiftLinkNotClaimed
		}
		req.AmountMicro = link.CreditsMicro
	}

	// Available is always the ledger SUM, never the cached column: a stale
	// cache must not be able to authorise an overdraft. Frozen credits are
	// subtracted inside AvailableMicro (C1) — a link that was issued but not
	// yet claimed, or claimed but not yet withdrawn, has already been promised
	// to somebody else.
	balance, err := ledgerBalance(ctx, tx, req.UserID)
	if err != nil {
		return nil, false, err
	}
	available := balance.AvailableMicro()

	// The cap is the ceiling, and the requested amount is clamped to it rather
	// than trusted. Two separate rules:
	//   - Manual: the whole available balance (a person is watching).
	//   - Automatic: 1/N of it, so several hubs get a share (E6).
	// A caller that asks for more than the cap is refused, not silently
	// truncated: a hub that thinks it topped up 9 and actually got 3 would keep
	// retrying, and the retry would be deduplicated by request_id, so it would
	// never converge.
	capMicro := available
	if !req.Manual {
		hubs, err := countUserHubs(ctx, tx, req.UserID)
		if err != nil {
			return nil, false, err
		}
		capMicro = AutoWithdrawLimitMicro(available, hubs)
	}
	if req.AmountMicro < 0 {
		return nil, false, fmt.Errorf("token bank withdrawal amount must not be negative")
	}
	amount := capMicro
	if req.AmountMicro > 0 {
		if req.AmountMicro > capMicro {
			return nil, false, fmt.Errorf("%w: requested %d, allowed %d", ErrTokenBankInsufficient, req.AmountMicro, capMicro)
		}
		amount = req.AmountMicro
	}
	if amount <= 0 {
		return nil, false, ErrTokenBankNothingToWithdraw
	}

	// The row id and the ledger row id are both derived from request_id, so a
	// replay on any node rebuilds exactly the same keys. Hashing rather than
	// concatenating keeps the id bounded no matter how the hub formats its
	// request id, and keeps the two tables' ids from ever colliding.
	id := tokenBankWithdrawID(req.RequestID)
	bizKey := "withdraw:" + req.RequestID
	createdAt := now.UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO token_bank_withdrawals (id, request_id, user_id, hub_id, amount_micro, grant_id, kind, link_id, status, created_at)
		 VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
		id, req.RequestID, req.UserID, req.HubID, amount, req.Kind, req.LinkID,
		TokenBankWithdrawStatusIssued, createdAt); err != nil {
		// The request-id lookup above missed a row that committed between the
		// read and this insert. A gift link has the same race on its own key.
		if isTokenBankUniqueViolation(err) {
			if again, ok := replayWithdrawal(ctx, tx, req); ok {
				return again, false, nil
			}
			if req.Kind == "gift" && req.LinkID != "" {
				return nil, false, ErrTokenBankGiftWithdrawn
			}
		}
		return nil, false, fmt.Errorf("insert token bank withdrawal: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenBankWithdrawLedgerID(req.RequestID), req.UserID, TokenBankBucketWithdrawn, amount, bizKey,
		"withdrawal", req.RequestID, req.HubID, createdAt)
	if err != nil {
		return nil, false, fmt.Errorf("insert token bank withdrawal ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if rows == 0 {
		// The ledger already knew this withdrawal but the withdrawal row was
		// missing. That is drift, and the safe answer is to refuse rather than
		// to guess whether the money really moved.
		return nil, false, fmt.Errorf("token bank withdrawal %q already in ledger but missing from withdrawals", req.RequestID)
	}
	if err := bumpTokenBankAccount(ctx, tx, req.UserID, TokenBankBucketWithdrawn, amount, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	r.publishWithdraw(req.RequestID, req.UserID, req.HubID, amount, now)
	return &TokenBankWithdrawal{
		ID: id, RequestID: req.RequestID, UserID: req.UserID, HubID: req.HubID,
		AmountMicro: amount, Kind: req.Kind, LinkID: req.LinkID,
		Status: TokenBankWithdrawStatusIssued, Created: now.UTC(),
	}, true, nil
}

// BindGrantID records that the hub really created the grant. It is what turns a
// withdrawal from "debited" into "delivered", and it is what lets a reinstalled
// hub be recognised on replay. hubID must be the hub that was debited: another
// machine that knows the request id must not overwrite the grant.
func (r *TokenBankRepo) BindGrantID(ctx context.Context, requestID, hubID, grantID string) error {
	requestID = strings.TrimSpace(requestID)
	hubID = strings.TrimSpace(hubID)
	grantID = strings.TrimSpace(grantID)
	if requestID == "" || hubID == "" || grantID == "" {
		return fmt.Errorf("request id, hub id, and grant id required")
	}
	res, err := r.write.ExecContext(ctx,
		`UPDATE token_bank_withdrawals SET grant_id = ?, status = ?
		 WHERE request_id = ? AND hub_id = ? AND status <> ?`,
		grantID, TokenBankWithdrawStatusBound, requestID, hubID, TokenBankWithdrawStatusBound)
	if err != nil {
		return fmt.Errorf("bind token bank grant: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		// A retry after a successful bind must not look like a conflict when
		// the same hub is confirming the same grant id again.
		var current string
		err := r.write.QueryRowContext(ctx, `SELECT grant_id FROM token_bank_withdrawals WHERE request_id = ? AND hub_id = ?`, requestID, hubID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("token bank withdrawal %q not found", requestID)
		}
		if err != nil {
			return err
		}
		if current == grantID {
			return nil
		}
		return fmt.Errorf("token bank withdrawal %q not found or already bound", requestID)
	}
	return nil
}

// ReissueGrantID records a grant rebuilt after the hub lost its registry.
// It does not debit. The same hub may report the same grant id again.
// Another hub's request id is not found.
func (r *TokenBankRepo) ReissueGrantID(ctx context.Context, requestID, hubID, grantID string) error {
	requestID = strings.TrimSpace(requestID)
	hubID = strings.TrimSpace(hubID)
	grantID = strings.TrimSpace(grantID)
	if requestID == "" || hubID == "" || grantID == "" {
		return fmt.Errorf("request id, hub id, and grant id required")
	}
	res, err := r.write.ExecContext(ctx,
		`UPDATE token_bank_withdrawals SET grant_id = ?, status = ? WHERE request_id = ? AND hub_id = ?`,
		grantID, TokenBankWithdrawStatusReissued, requestID, hubID)
	if err != nil {
		return fmt.Errorf("reissue token bank grant: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}
	var current string
	err = r.write.QueryRowContext(ctx, `SELECT grant_id FROM token_bank_withdrawals WHERE request_id = ? AND hub_id = ?`, requestID, hubID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("token bank withdrawal %q not found", requestID)
	}
	if err != nil {
		return err
	}
	if current == grantID {
		return nil
	}
	return fmt.Errorf("token bank withdrawal %q was not reissued", requestID)
}

// SumWithdrawalMicro is the integer total pulled to one hub. An empty hubID
// sums every hub, which is the account-wide figure, not the per-hub reconcile.
func (r *TokenBankRepo) SumWithdrawalMicro(ctx context.Context, userID, hubID string) (int64, error) {
	userID = strings.TrimSpace(userID)
	hubID = strings.TrimSpace(hubID)
	query := `SELECT COALESCE(SUM(amount_micro), 0) FROM token_bank_withdrawals WHERE user_id = ?`
	args := []any{userID}
	if hubID != "" {
		query += ` AND hub_id = ?`
		args = append(args, hubID)
	}
	var sum int64
	if err := r.read.QueryRowContext(ctx, query, args...).Scan(&sum); err != nil {
		return 0, fmt.Errorf("sum token bank withdrawals: %w", err)
	}
	return sum, nil
}

// AppendAdjustment writes a correction as a ledger row. The account cache is
// updated by that insert. Callers must not UPDATE the balance columns instead.
// The id is derived from bizKey so a replay does not apply twice.
func (r *TokenBankRepo) AppendAdjustment(ctx context.Context, userID, bucket string, amountMicro int64, bizKey, note string) (bool, error) {
	bizKey = strings.TrimSpace(bizKey)
	if bizKey == "" {
		return false, fmt.Errorf("token bank adjustment requires a biz key")
	}
	return r.AppendLedger(ctx, TokenBankLedgerEntry{
		ID:          "adj_" + hashTokenBankKey(bizKey),
		UserID:      userID,
		Bucket:      bucket,
		AmountMicro: amountMicro,
		BizKey:      bizKey,
		RefType:     "adjustment",
		Note:        note,
	})
}

// MarkReissued flags a withdrawal whose grant had to be rebuilt on the hub after
// a reinstall. It never debits again — the ledger row is keyed by the same
// request_id and will not insert twice.
//
// It reports an error when nothing matched, because silently "succeeding" here
// is how an operator ends up believing a lost grant was re-issued when the
// request_id was simply wrong.
func (r *TokenBankRepo) MarkReissued(ctx context.Context, requestID string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("request id required")
	}
	res, err := r.write.ExecContext(ctx,
		`UPDATE token_bank_withdrawals SET status = ? WHERE request_id = ?`,
		TokenBankWithdrawStatusReissued, requestID)
	if err != nil {
		return fmt.Errorf("mark token bank withdrawal reissued: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("token bank withdrawal %q not found", requestID)
	}
	return nil
}

// ListWithdrawals serves both the user's own history ("where did my credits
// go") and the reconciliation job that compares hubcenter's withdrawn total
// against the grants each hub actually holds.
func (r *TokenBankRepo) ListWithdrawals(ctx context.Context, userID, hubID string, limit int) ([]TokenBankWithdrawal, error) {
	query := `SELECT id, request_id, user_id, hub_id, amount_micro, grant_id, kind, link_id, status, created_at
		 FROM token_bank_withdrawals WHERE user_id = ?`
	args := []any{strings.TrimSpace(userID)}
	if strings.TrimSpace(hubID) != "" {
		query += ` AND hub_id = ?`
		args = append(args, strings.TrimSpace(hubID))
	}
	query += ` ORDER BY created_at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list token bank withdrawals: %w", err)
	}
	return scanTokenBankWithdrawals(rows)
}

// ListUnboundGiftWithdrawals returns gift debits whose grant was never confirmed.
//
// The history list is newest-first and capped, and ordinary withdrawals fill
// that page. A gift debit that falls off it cannot be finished: a new request
// id is refused, and this row's request id is the only key that writes the
// grant without a second debit. The limit here counts unfinished gifts, not
// the mixed history page.
func (r *TokenBankRepo) ListUnboundGiftWithdrawals(ctx context.Context, userID, hubID string, limit int) ([]TokenBankWithdrawal, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("list unbound gift withdrawals requires a user id")
	}
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, request_id, user_id, hub_id, amount_micro, grant_id, kind, link_id, status, created_at
		 FROM token_bank_withdrawals
		 WHERE user_id = ? AND kind = 'gift' AND link_id <> '' AND grant_id = '' AND amount_micro > 0`
	args := []any{userID}
	if hub := strings.TrimSpace(hubID); hub != "" {
		query += ` AND hub_id = ?`
		args = append(args, hub)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list unbound gift withdrawals: %w", err)
	}
	return scanTokenBankWithdrawals(rows)
}

// tokenBankHubIDFromAutoRequest reads the hub id out of
// tbk-auto:<hub>:<email>:<group>:<seq>. A manual request id has no hub in it.
func tokenBankHubIDFromAutoRequest(requestID string) string {
	const prefix = "tbk-auto:"
	requestID = strings.TrimSpace(requestID)
	if !strings.HasPrefix(requestID, prefix) {
		return ""
	}
	rest := requestID[len(prefix):]
	hub, _, ok := strings.Cut(rest, ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(hub)
}

// ListOrphanWithdrawals returns withdrawn ledger rows that have no withdrawal
// record on this node.
//
// A pull inserts both, on the node that took the debit. HA copies the ledger
// line and leaves the withdrawal row behind. The balance on every node then
// includes the debit, and a history read here would otherwise skip it. The
// note on that line is the hub id. A blank note still matches an automatic
// request id of the form tbk-auto:<hub>:. Adjustments use another ref type
// and stay out of this list. A row this node already has is not returned again.
func (r *TokenBankRepo) ListOrphanWithdrawals(ctx context.Context, userID, hubID string, limit int) ([]TokenBankWithdrawal, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("list orphan token bank withdrawals requires a user id")
	}
	query := `SELECT id, user_id, amount_micro, ref_id, note, created_at
		 FROM token_bank_ledger
		 WHERE user_id = ? AND bucket = ? AND ref_type = 'withdrawal'
		   AND ref_id <> '' AND biz_key = 'withdraw:' || ref_id AND amount_micro > 0
		   AND NOT EXISTS (
		     SELECT 1 FROM token_bank_withdrawals w
		      WHERE w.user_id = token_bank_ledger.user_id AND w.request_id = token_bank_ledger.ref_id
		   )`
	args := []any{userID, TokenBankBucketWithdrawn}
	if hub := strings.TrimSpace(hubID); hub != "" {
		// The note is the hub id when the replica received one. Older replicas
		// stored a blank note; the automatic request id still starts with
		// tbk-auto:<hub>:. Match that same fallback, and keep the colon so
		// hub_bare does not also match hub_bareX.
		query += ` AND (trim(note) = ? OR (trim(note) = '' AND instr(ref_id, ?) = 1))`
		args = append(args, hub, "tbk-auto:"+hub+":")
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list orphan token bank withdrawals: %w", err)
	}
	defer rows.Close()
	out := []TokenBankWithdrawal{}
	for rows.Next() {
		var w TokenBankWithdrawal
		var created string
		if err := rows.Scan(&w.ID, &w.UserID, &w.AmountMicro, &w.RequestID, &w.HubID, &created); err != nil {
			return nil, err
		}
		w.HubID = strings.TrimSpace(w.HubID)
		w.RequestID = strings.TrimSpace(w.RequestID)
		if w.HubID == "" {
			// Older replicas published the ledger line without the hub id.
			// An automatic request id still carries it: tbk-auto:<hub>:<email>:<group>:<seq>.
			w.HubID = tokenBankHubIDFromAutoRequest(w.RequestID)
		}
		w.Kind = "self"
		w.Status = TokenBankWithdrawStatusPosted
		w.Created, _ = time.Parse(time.RFC3339, created)
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanTokenBankWithdrawals(rows *sql.Rows) ([]TokenBankWithdrawal, error) {
	defer rows.Close()
	out := []TokenBankWithdrawal{}
	for rows.Next() {
		var w TokenBankWithdrawal
		var created string
		if err := rows.Scan(&w.ID, &w.RequestID, &w.UserID, &w.HubID, &w.AmountMicro,
			&w.GrantID, &w.Kind, &w.LinkID, &w.Status, &created); err != nil {
			return nil, err
		}
		w.Created, _ = time.Parse(time.RFC3339, created)
		out = append(out, w)
	}
	return out, rows.Err()
}

// ledgerBalance sums the ledger inside an open transaction. The cache is not
// consulted: every debit decision has to be made against the source of truth.
//
// It delegates to the same fold Balance uses, so the read path and the debit
// path cannot disagree about what a bucket means. See scanTokenBankBalance.
func ledgerBalance(ctx context.Context, tx *sql.Tx, userID string) (TokenBankBalance, error) {
	return scanTokenBankBalance(ctx, tx.QueryContext, userID)
}

// countUserHubs is the N of the averaging rule (§14.5).
//
// The ledger is keyed by sm_users.id while the hub registry is keyed by email
// (that is what a hub proves when it links to an account), so the join goes
// through sm_users. Unknown degrades to 1 — "one hub, take it all" — rather
// than to 0, which would divide by zero and stall automatic withdrawals.
func countUserHubs(ctx context.Context, tx *sql.Tx, userID string) (int, error) {
	return countHubsForUser(ctx, tx.QueryRowContext, userID)
}

// HubBelongsToUser reports whether hubID is linked to the skillmarket user.
// A missing link table is not a match and not an error: settlement then
// continues, which pays the owner instead of dropping the credit.
func (r *TokenBankRepo) HubBelongsToUser(ctx context.Context, hubID, userID string) (bool, error) {
	hubID = strings.TrimSpace(hubID)
	userID = strings.TrimSpace(userID)
	if r == nil || hubID == "" || userID == "" {
		return false, nil
	}
	var n int
	err := r.read.QueryRowContext(ctx,
		`SELECT COUNT(1)
		   FROM hub_user_links l
		   JOIN sm_users u ON u.email = l.email
		  WHERE u.id = ? AND l.hub_id = ?`, userID, hubID).Scan(&n)
	if err != nil {
		if isMissingRelationError(err) {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}

// HubIsExclusiveToUser reports that this hub is linked to userID and to nobody
// else. A shared hub stays false: the owner having one account there does not
// make every caller that owner. A link whose email has no skillmarket user
// still counts as someone else. A missing link table is false, not an error,
// so settlement still records the call.
func (r *TokenBankRepo) HubIsExclusiveToUser(ctx context.Context, hubID, userID string) (bool, error) {
	hubID = strings.TrimSpace(hubID)
	userID = strings.TrimSpace(userID)
	if r == nil || hubID == "" || userID == "" {
		return false, nil
	}
	var ownerLinks, people int
	err := r.read.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT CASE WHEN u.id = ? THEN u.id END),
		        COUNT(DISTINCT l.email)
		   FROM hub_user_links l
		   LEFT JOIN sm_users u ON u.email = l.email
		  WHERE l.hub_id = ?`, userID, hubID).Scan(&ownerLinks, &people)
	if err != nil {
		if isMissingRelationError(err) {
			return false, nil
		}
		return false, err
	}
	return ownerLinks == 1 && people == 1, nil
}

// CountUserHubs is the read-side counterpart, for the summary endpoint that
// shows the user what their automatic-withdrawal share currently is. It runs on
// the read pool because nothing here decides whether to debit.
func (r *TokenBankRepo) CountUserHubs(ctx context.Context, userID string) (int, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 1, nil
	}
	return countHubsForUser(ctx, r.read.QueryRowContext, userID)
}

// countHubsForUser is shared by the write path (inside a transaction, where the
// count has to be consistent with the debit) and the read path. It takes a
// query function rather than a *sql.Tx so both callers can use it.
//
// The error handling is asymmetric on purpose. A deployment that never
// initialised SkillMarket has no sm_users/hub_user_links at all, and there N is
// genuinely unknown: degrading to 1 keeps the arithmetic defined. Any *other*
// error — a lock timeout, a schema change, a corrupt row — must not be
// swallowed. Falling back to 1 there is not a neutral default: N is the
// divisor of AutoWithdrawLimitMicro, so a user who really owns three hubs
// would suddenly be allowed to drain the whole balance in one automatic
// withdrawal (E6's anti-monopoly rule silently off).
func countHubsForUser(ctx context.Context, queryRow func(context.Context, string, ...any) *sql.Row, userID string) (int, error) {
	var n int
	err := queryRow(ctx,
		`SELECT COUNT(DISTINCT l.hub_id)
		   FROM hub_user_links l
		   JOIN sm_users u ON u.email = l.email
		  WHERE u.id = ?`, userID).Scan(&n)
	if err != nil {
		if isMissingRelationError(err) {
			return 1, nil
		}
		return 0, fmt.Errorf("count hubs for user %q: %w", userID, err)
	}
	if n < 1 {
		n = 1
	}
	return n, nil
}

// isMissingRelationError reports whether a query failed because a table it
// needs does not exist in this database.
//
// It matches on the driver's message rather than on a sentinel because
// modernc.org/sqlite does not export one for this — but it is deliberately
// narrow: only the exact "no such table" text qualifies, so a genuine error
// (a lock timeout, a syntax error, a corrupt index) is never mistaken for a
// missing table and silently downgraded.
func isMissingRelationError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}
