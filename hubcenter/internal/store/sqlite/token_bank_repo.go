package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Token Bank ledger buckets. Every movement of a user's Token Bank credits is
// an append-only row in one of these buckets; a balance is always the SUM of
// them, never a value read-modify-written in place.
//
// That is deliberate. HubCenter replicates rows between HA nodes and resolves
// conflicts by comparing entity versions, so a balance column loses one of two
// concurrent updates. An append-only row has no such problem: replay is an
// INSERT OR REPLACE keyed by a deterministic id, which is idempotent.
// See the design doc, section 14 (E1).
const (
	TokenBankBucketEarned    = "earned"    // credited by settlement
	TokenBankBucketReceived  = "received"  // credited by a claimed gift
	TokenBankBucketWithdrawn = "withdrawn" // debited by a hub withdrawal
	TokenBankBucketGranted   = "granted"   // debited when a gift is settled
	TokenBankBucketFrozen    = "frozen"    // reserved by an unsettled gift link
)

// TokenBankLedgerEntry is one append-only movement.
type TokenBankLedgerEntry struct {
	ID          string // MUST be deterministic; see the id conventions below
	UserID      string
	Bucket      string
	AmountMicro int64 // signed, 1 credit = 1e6
	BizKey      string
	RefType     string
	RefID       string
	Note        string
	CreatedAt   time.Time
}

// TokenBankBalance is the SUM view of the ledger.
type TokenBankBalance struct {
	EarnedMicro    int64
	ReceivedMicro  int64
	WithdrawnMicro int64
	GrantedMicro   int64
	FrozenMicro    int64
}

// AvailableMicro is what the user may still withdraw. Frozen credits count
// against it: a link that has been created but not yet settled, and one that
// has been claimed but not yet withdrawn, both reserve credits that are no
// longer the sender's to spend. Leaving frozen out of this arithmetic lets a
// user hand out N links and withdraw the same credits N times.
// See the design doc, section 3.6 (C1).
func (b TokenBankBalance) AvailableMicro() int64 {
	return b.EarnedMicro + b.ReceivedMicro - b.WithdrawnMicro - b.GrantedMicro - b.FrozenMicro
}

type TokenBankRepo struct {
	write *sql.DB
	read  *sql.DB

	// syncSink, when set, receives every row this node appends so the HA layer
	// can publish it to peers. Without it the ledger only exists locally and
	// each node's balance drifts apart by whatever it settled itself — which is
	// precisely the failure E1 was written to prevent.
	//
	// It is called after the local transaction commits, so it can never see a
	// row that rolled back, and it must never block: see the buffer's Add.
	syncSink func(TokenBankLedgerEntry)

	// priceBookSink receives the price rules this node writes. It is separate
	// from syncSink because the price book is not append-only: an admin edit
	// replaces a row and a removal deletes one, so the two need different op
	// types rather than two flavours of insert.
	//
	// Without it an edit is invisible outside the node that happened to serve
	// the admin's request, and the cluster quietly runs several price lists at
	// once — the same model earning different amounts depending on which node
	// proxied a call, with no error anywhere to explain it.
	priceBookSink func(rule TokenBankPriceRule, deleted bool)
}

func NewTokenBankRepo(p *Provider) *TokenBankRepo {
	return &TokenBankRepo{write: p.Write, read: p.Read}
}

// SetSyncSink installs the replication hook. Passing nil disables replication,
// which is what a single-node deployment wants.
func (r *TokenBankRepo) SetSyncSink(sink func(TokenBankLedgerEntry)) {
	r.syncSink = sink
}

// SetPriceBookSyncSink installs the replication hook for admin price rules.
// Passing nil disables it, which is what a single-node deployment wants.
func (r *TokenBankRepo) SetPriceBookSyncSink(sink func(rule TokenBankPriceRule, deleted bool)) {
	r.priceBookSink = sink
}

// publishPriceRule hands a locally written rule to the HA layer. It is called
// only after the write succeeded, so a peer never receives a rule this node
// failed to save.
func (r *TokenBankRepo) publishPriceRule(rule TokenBankPriceRule, deleted bool) {
	if r == nil || r.priceBookSink == nil {
		return
	}
	r.priceBookSink(rule, deleted)
}

// AppendLedger adds one movement and keeps the cached balance in step, both in
// a single transaction.
//
// It is idempotent by primary key: replaying the same entry (HA sync, a hub
// retrying a withdrawal, a settlement that runs twice) reports applied=false
// and changes nothing. This is why TokenBankLedgerEntry.ID must be derived from
// the business key rather than randomly generated — three nodes each minting a
// random id for the same settlement would triplicate the credit.
func (r *TokenBankRepo) AppendLedger(ctx context.Context, entry TokenBankLedgerEntry) (applied bool, err error) {
	entry.UserID = strings.TrimSpace(entry.UserID)
	entry.ID = strings.TrimSpace(entry.ID)
	if entry.UserID == "" || entry.ID == "" {
		return false, fmt.Errorf("token bank ledger entry requires user id and id")
	}
	if !isTokenBankBucket(entry.Bucket) {
		return false, fmt.Errorf("unknown token bank bucket %q", entry.Bucket)
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.UserID, entry.Bucket, entry.AmountMicro,
		strings.TrimSpace(entry.BizKey), strings.TrimSpace(entry.RefType),
		strings.TrimSpace(entry.RefID), strings.TrimSpace(entry.Note),
		createdAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, fmt.Errorf("insert token bank ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 0 {
		// Already present: a replay or a retry. Nothing to apply, nothing to fix.
		return false, tx.Commit()
	}
	// The cache is a SUM mirror, so bump it with an atomic SQL increment rather
	// than reading it back. A stale cache is recoverable by RebuildAccount.
	if err := bumpTokenBankAccount(ctx, tx, entry.UserID, entry.Bucket, entry.AmountMicro, createdAt); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	// Publish only after the commit succeeded. Doing it before would let a
	// rolled-back row reach peers, and they would have no way to tell it apart
	// from a real one.
	r.publish(entry, createdAt)
	return true, nil
}

func (r *TokenBankRepo) publish(entry TokenBankLedgerEntry, createdAt time.Time) {
	if r == nil || r.syncSink == nil {
		return
	}
	entry.CreatedAt = createdAt
	r.syncSink(entry)
}

// publishWithdraw reuses the same hook for the ledger row a withdrawal writes.
// The caller has already committed; a nil sink is a single-node deployment.
func (r *TokenBankRepo) publishWithdraw(requestID, userID string, amount int64, now time.Time) {
	r.publish(TokenBankLedgerEntry{
		ID:          tokenBankWithdrawLedgerID(requestID),
		UserID:      userID,
		Bucket:      TokenBankBucketWithdrawn,
		AmountMicro: amount,
		BizKey:      "withdraw:" + requestID,
		RefType:     "withdrawal",
		RefID:       requestID,
	}, now)
}

// publishGiftLedger republishes one of the gift lifecycle's ledger rows (freeze,
// unfreeze, grant, receive). Each is a plain movement in a bucket, so they all
// share this one shape; the prefix keeps the derived ids from colliding across
// the four kinds written for the same link.
func (r *TokenBankRepo) publishGiftLedger(prefix, userID, bucket, bizKey, linkID string, amount int64, note string, now time.Time) {
	r.publish(TokenBankLedgerEntry{
		ID:          prefix + "_" + hashTokenBankKey(bizKey),
		UserID:      userID,
		Bucket:      bucket,
		AmountMicro: amount,
		BizKey:      bizKey,
		RefType:     "gift_link",
		RefID:       linkID,
		Note:        note,
	}, now)
}

// giftLedgerID is the single place that derives a gift ledger row id, so the
// write path and the publish path cannot drift apart. Two different ids for the
// same movement would make a peer apply it as a second, separate row.
func giftLedgerID(prefix, bizKey string) string {
	return prefix + "_" + hashTokenBankKey(bizKey)
}

func bumpTokenBankAccount(ctx context.Context, tx *sql.Tx, userID, bucket string, delta int64, now time.Time) error {
	column, ok := tokenBankBucketColumn(bucket)
	if !ok {
		return fmt.Errorf("unknown token bank bucket %q", bucket)
	}
	stamp := now.UTC().Format(time.RFC3339)
	// Upsert first so a brand new user has a row to increment.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO token_bank_accounts (user_id, updated_at) VALUES (?, ?)
		 ON CONFLICT(user_id) DO NOTHING`,
		userID, stamp); err != nil {
		return fmt.Errorf("ensure token bank account: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE token_bank_accounts SET %s = %s + ?, updated_at = ? WHERE user_id = ?`, column, column),
		delta, stamp, userID); err != nil {
		return fmt.Errorf("bump token bank account: %w", err)
	}
	return nil
}

// scanTokenBankBalance runs the ledger SUM query and folds the rows into a
// balance. It is the only implementation of that fold.
//
// It is shared by the read path (Balance, on the read pool) and the write path
// (ledgerBalance, inside a transaction) because the two had to be identical: a
// bucket added to one copy and not the other would still sum correctly on
// whichever path the test happened to exercise, and would quietly vanish from
// the other — which, for a bucket, means money disappearing from a balance
// with no error anywhere. The query function parameter is what lets one
// implementation serve both a *sql.DB and a *sql.Tx.
func scanTokenBankBalance(ctx context.Context, query func(context.Context, string, ...any) (*sql.Rows, error), userID string) (TokenBankBalance, error) {
	var out TokenBankBalance
	rows, err := query(ctx,
		`SELECT bucket, COALESCE(SUM(amount_micro), 0) FROM token_bank_ledger WHERE user_id = ? GROUP BY bucket`,
		strings.TrimSpace(userID))
	if err != nil {
		return out, fmt.Errorf("sum token bank ledger: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket string
		var amount int64
		if err := rows.Scan(&bucket, &amount); err != nil {
			return out, err
		}
		switch bucket {
		case TokenBankBucketEarned:
			out.EarnedMicro = amount
		case TokenBankBucketReceived:
			out.ReceivedMicro = amount
		case TokenBankBucketWithdrawn:
			out.WithdrawnMicro = amount
		case TokenBankBucketGranted:
			out.GrantedMicro = amount
		case TokenBankBucketFrozen:
			out.FrozenMicro = amount
		default:
			// An unrecognised bucket means a row this version cannot
			// interpret, which in turn means the balance below is not the
			// whole truth. Every balance is used to authorise a debit, so a
			// silent partial SUM is worse than a refusal.
			return out, fmt.Errorf("unknown token bank bucket %q in ledger for user %q", bucket, userID)
		}
	}
	return out, rows.Err()
}

// Balance sums the ledger. This is the authoritative read; the accounts table
// is only a cache and must never be trusted for a debit decision.
func (r *TokenBankRepo) Balance(ctx context.Context, userID string) (TokenBankBalance, error) {
	return scanTokenBankBalance(ctx, r.read.QueryContext, userID)
}

// AvailableMicro is the amount a withdrawal may take. It reads the ledger,
// never the cache, so a stale cache cannot let a withdrawal overdraw.
func (r *TokenBankRepo) AvailableMicro(ctx context.Context, userID string) (int64, error) {
	balance, err := r.Balance(ctx, userID)
	if err != nil {
		return 0, err
	}
	return balance.AvailableMicro(), nil
}

// RebuildAccount recomputes the cached balance from the ledger. It exists so a
// cache that drifted (a replayed row, a partial write) is a recoverable
// inconsistency instead of permanent corruption.
func (r *TokenBankRepo) RebuildAccount(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("user id required")
	}
	balance, err := r.Balance(ctx, userID)
	if err != nil {
		return err
	}
	_, err = r.write.ExecContext(ctx,
		`INSERT INTO token_bank_accounts (user_id, earned_micro, received_micro, withdrawn_micro, granted_micro, frozen_micro, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   earned_micro = excluded.earned_micro,
		   received_micro = excluded.received_micro,
		   withdrawn_micro = excluded.withdrawn_micro,
		   granted_micro = excluded.granted_micro,
		   frozen_micro = excluded.frozen_micro,
		   updated_at = excluded.updated_at`,
		userID, balance.EarnedMicro, balance.ReceivedMicro, balance.WithdrawnMicro,
		balance.GrantedMicro, balance.FrozenMicro, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("rebuild token bank account: %w", err)
	}
	return nil
}

// FindDriftedAccounts returns the users whose cached balance has stopped
// matching the ledger SUM.
//
// RebuildAccount repairs one user, but nothing calls it on its own — and a
// cache that drifted stays wrong until somebody thinks to look, which is the
// difference between "the cache is a recoverable mirror" and "the cache is a
// second, wrong source of truth". This is the detector that lets a caller
// schedule that repair instead of waiting for a support ticket.
//
// The comparison is one query rather than a per-user loop: the cache and the
// ledger are joined and compared in SQL, so a deployment with a hundred
// thousand accounts costs one scan and not a hundred thousand round trips.
func (r *TokenBankRepo) FindDriftedAccounts(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.read.QueryContext(ctx,
		`SELECT a.user_id
		   FROM token_bank_accounts a
		   LEFT JOIN token_bank_ledger l ON l.user_id = a.user_id
		  GROUP BY a.user_id
		 HAVING MAX(a.earned_micro)    <> COALESCE(SUM(CASE WHEN l.bucket = ? THEN l.amount_micro ELSE 0 END), 0)
		     OR MAX(a.received_micro)  <> COALESCE(SUM(CASE WHEN l.bucket = ? THEN l.amount_micro ELSE 0 END), 0)
		     OR MAX(a.withdrawn_micro) <> COALESCE(SUM(CASE WHEN l.bucket = ? THEN l.amount_micro ELSE 0 END), 0)
		     OR MAX(a.granted_micro)   <> COALESCE(SUM(CASE WHEN l.bucket = ? THEN l.amount_micro ELSE 0 END), 0)
		     OR MAX(a.frozen_micro)    <> COALESCE(SUM(CASE WHEN l.bucket = ? THEN l.amount_micro ELSE 0 END), 0)
		  LIMIT ?`,
		TokenBankBucketEarned, TokenBankBucketReceived, TokenBankBucketWithdrawn,
		TokenBankBucketGranted, TokenBankBucketFrozen, limit)
	if err != nil {
		return nil, fmt.Errorf("find drifted token bank accounts: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		out = append(out, userID)
	}
	return out, rows.Err()
}

func isTokenBankBucket(bucket string) bool {
	_, ok := tokenBankBucketColumn(bucket)
	return ok
}

// tokenBankBucketColumn maps a bucket to the cached column that mirrors it.
//
// This switch and the one in scanTokenBankBalance are the two halves of a
// bucket's definition — how it sums into a balance, and which cache column it
// bumps — and they must list the same buckets. They cannot share one table
// because one yields a column name and the other assigns a struct field, so the
// agreement is documented here instead: adding a bucket means editing both.
// TestTokenBankBucketsAreKnown is the guard that catches a half-edit.
func tokenBankBucketColumn(bucket string) (string, bool) {
	switch bucket {
	case TokenBankBucketEarned:
		return "earned_micro", true
	case TokenBankBucketReceived:
		return "received_micro", true
	case TokenBankBucketWithdrawn:
		return "withdrawn_micro", true
	case TokenBankBucketGranted:
		return "granted_micro", true
	case TokenBankBucketFrozen:
		return "frozen_micro", true
	default:
		return "", false
	}
}
