package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Gift link errors. They are sentinel values rather than strings because the
// HTTP layer has to map each one to a distinct status code, and a caller that
// cannot tell "you were too slow" from "your code is wrong" will show the user
// the wrong message.
var (
	ErrGiftLinkNotFound   = errors.New("token bank: gift link not found")
	ErrGiftLinkNotActive  = errors.New("token bank: gift link is no longer active")
	ErrGiftLinkExpired    = errors.New("token bank: gift link has expired")
	ErrGiftLinkOwnLink    = errors.New("token bank: cannot claim your own gift link")
	ErrGiftLinkOverCap    = errors.New("token bank: gift exceeds the 50% limit")
	ErrGiftLinkNoBalance  = errors.New("token bank: nothing available to gift")
	ErrGiftLinkNotClaimed = errors.New("token bank: gift link has not been claimed yet")
	// Anti-abuse limits from §5. Distinct sentinels because the two failures
	// need different advice: "below the floor" is a typo the user fixes by
	// typing more, "over the daily limit" is a wait-until-tomorrow.
	ErrGiftLinkBelowFloor  = errors.New("token bank: gift is below the minimum")
	ErrGiftLinkRateLimited = errors.New("token bank: daily share-link limit reached")
)

// TokenBankGiftLink is one "share my credits" link (§3.6). Only the first
// claimer wins; the claim itself binds the person and does not move money.
type TokenBankGiftLink struct {
	ID             string
	Code           string
	SenderUserID   string
	SenderEmail    string
	CreditsMicro   int64
	Status         string
	ClaimedByUser  string
	ClaimedByEmail string
	// OriginNodeID pins the link to the hubcenter node that created it. Claims
	// must be decided by that node: two nodes each running a local
	// "UPDATE ... WHERE status='active'" would both succeed and then overwrite
	// each other during HA sync, paying the same credits out twice (§8 item 9).
	OriginNodeID string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	ClaimedAt    time.Time
	RevokedAt    time.Time
}

const (
	TokenBankGiftStatusActive  = "active"
	TokenBankGiftStatusClaimed = "claimed"
	TokenBankGiftStatusRevoked = "revoked"
	TokenBankGiftStatusExpired = "expired"
	TokenBankGiftStatusSettled = "settled" // claimed, and the receiver has withdrawn
)

// TokenBankGiftSharePercent is the single-share cap from §3.6: half the
// available credits. It is expressed as a fraction of 100 so the arithmetic
// stays integer and the rounding rule is explicit.
const TokenBankGiftSharePercent = 50

// TokenBankGiftLinkTTL is the default life of an unclaimed link. It stays the
// fallback for a deployment whose settings blob predates
// `credit_share_link_ttl_hours`; a live one passes the configured TTL through
// GiftLinkPolicy instead.
const TokenBankGiftLinkTTL = 7 * 24 * time.Hour

// GiftLinkPolicy carries the admin-configured anti-abuse limits (§5) into the
// freeze transaction.
//
// It is a parameter rather than a read of the settings blob inside this
// function on purpose: the store layer does not own `system_settings` (the
// HTTP layer does), and a store that reached for it could not be tested without
// standing up the settings table.
type GiftLinkPolicy struct {
	// MaxRatio caps a single link as a fraction of available credits. §3.6 fixes
	// the shipped default at 0.5; zero means "use the built-in 50%".
	MaxRatio float64
	// MinMicro is the smallest single gift, in microcredits. Zero disables the
	// floor (which is what `credit_share_min_credits: 0` means).
	MinMicro int64
	// DailyLimit is how many links one user may create per UTC day. Zero means
	// unlimited — the admin setting documents `>= 0` with 0 as "no cap".
	DailyLimit int
	// TTL is the life of an unclaimed link. Zero falls back to
	// TokenBankGiftLinkTTL.
	TTL time.Duration
}

// GiftShareCapMicro is the largest single share allowed from an available
// balance, under a given policy.
//
// This is the *only* implementation of the cap arithmetic. The HTTP layer calls
// it too, to render the same number the create path will enforce — two copies of
// a rounding rule is how a GUI ends up offering an amount the server refuses.
//
// Rounded down, never up: a gift that rounds up could exceed the cap and
// over-issue. A zero or negative ratio falls back to the built-in 50%.
func GiftShareCapMicro(availableMicro int64, policy GiftLinkPolicy) int64 {
	if availableMicro <= 0 {
		return 0
	}
	if policy.MaxRatio <= 0 {
		return availableMicro * TokenBankGiftSharePercent / 100
	}
	// Float ratio, but still floored: half of an odd number of microcredits must
	// not round up into a cap the balance cannot cover.
	return int64(float64(availableMicro) * policy.MaxRatio)
}

// CreateGiftLink freezes the gifted amount and returns the link. Freezing is a
// real ledger row in the "frozen" bucket, not a count over this table, because
// §3.6-C1 requires the withdrawal endpoint to see it: a link that was only
// recorded here would be invisible to the available-balance arithmetic, letting
// the sender withdraw the same credits and over-issue.
func (r *TokenBankRepo) CreateGiftLink(ctx context.Context, link TokenBankGiftLink, policy GiftLinkPolicy, now time.Time) (*TokenBankGiftLink, error) {
	link.SenderUserID = strings.TrimSpace(link.SenderUserID)
	link.Code = strings.TrimSpace(link.Code)
	link.ID = strings.TrimSpace(link.ID)
	if link.SenderUserID == "" || link.Code == "" || link.ID == "" {
		return nil, fmt.Errorf("gift link requires id, code and sender")
	}
	if link.CreditsMicro <= 0 {
		return nil, fmt.Errorf("gift link requires a positive amount")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expires := link.ExpiresAt
	if expires.IsZero() {
		ttl := policy.TTL
		if ttl <= 0 {
			ttl = TokenBankGiftLinkTTL
		}
		expires = now.Add(ttl)
	}

	// The floor is checked before opening the transaction: it depends on
	// nothing in the ledger, and a rejected amount should not have taken a
	// write lock. `credit_share_min_credits` exists to stop dust transfers; a
	// zero means the admin disabled it.
	if policy.MinMicro > 0 && link.CreditsMicro < policy.MinMicro {
		return nil, ErrGiftLinkBelowFloor
	}

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// The daily cap is counted inside the transaction, not before it. Two
	// concurrent creates would each read limit-1 and both insert, so the check
	// has to be serialised against the same snapshot the insert commits under.
	// This is the same reasoning as the balance check right below it.
	if policy.DailyLimit > 0 {
		createdToday, err := countGiftLinksSince(ctx, tx, link.SenderUserID, StartOfUTCDay(now))
		if err != nil {
			return nil, err
		}
		if createdToday >= policy.DailyLimit {
			return nil, ErrGiftLinkRateLimited
		}
	}

	balance, err := ledgerBalance(ctx, tx, link.SenderUserID)
	if err != nil {
		return nil, err
	}
	available := balance.AvailableMicro()
	// Two checks, and the second is the important one: the cap alone would let
	// a user with 0 available credits gift 50% of 0, and it also has to hold
	// when several links are open at once (each freeze shrinks available).
	if available <= 0 {
		return nil, ErrGiftLinkNoBalance
	}
	if link.CreditsMicro > GiftShareCapMicro(available, policy) {
		return nil, ErrGiftLinkOverCap
	}

	stamp := now.UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO credit_share_links (id, code, sender_user_id, sender_email, credits_micro, status,
			claimed_by_user_id, claimed_by_email, origin_node_id, expires_at, created_at, claimed_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, '', '')`,
		link.ID, link.Code, link.SenderUserID, strings.TrimSpace(link.SenderEmail), link.CreditsMicro,
		TokenBankGiftStatusActive, strings.TrimSpace(link.OriginNodeID),
		expires.UTC().Format(time.RFC3339), stamp); err != nil {
		return nil, fmt.Errorf("insert gift link: %w", err)
	}

	bizKey := "freeze:" + link.ID
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)`,
		giftLedgerID("frz", "freeze:"+link.ID), link.SenderUserID, TokenBankBucketFrozen,
		link.CreditsMicro, bizKey, "gift_link", link.ID, stamp)
	if err != nil {
		return nil, fmt.Errorf("insert gift freeze ledger: %w", err)
	}
	if rows, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if rows == 0 {
		return nil, fmt.Errorf("gift link %q already frozen", link.ID)
	}
	if err := bumpTokenBankAccount(ctx, tx, link.SenderUserID, TokenBankBucketFrozen, link.CreditsMicro, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	link.Status = TokenBankGiftStatusActive
	link.ExpiresAt = expires.UTC()
	link.CreatedAt = now.UTC()
	// Publish the freeze row so peers see the sender's reduced available
	// balance. Without it a peer would happily authorise a withdrawal of
	// credits this link has already promised away (C1, across nodes).
	r.publishGiftLedger("frz", link.SenderUserID, TokenBankBucketFrozen,
		"freeze:"+link.ID, link.ID, link.CreditsMicro, "", now)
	return &link, nil
}

// ClaimGiftLink binds the first claimer and moves no money. The single
// conditional UPDATE is what makes it atomic: whoever's UPDATE reports
// RowsAffected == 1 won, everyone else sees 0 and learns the link is gone.
//
// Callers must reach the origin node (§OriginNodeID). Doing this on a replica
// produces two local winners that overwrite each other on sync.
func (r *TokenBankRepo) ClaimGiftLink(ctx context.Context, code, claimerUserID, claimerEmail string, now time.Time) (*TokenBankGiftLink, error) {
	code = strings.TrimSpace(code)
	claimerUserID = strings.TrimSpace(claimerUserID)
	if code == "" || claimerUserID == "" {
		return nil, fmt.Errorf("gift claim requires code and claimer")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stamp := now.UTC().Format(time.RFC3339)

	link, err := r.giftLinkByCode(ctx, r.read, code)
	if err != nil {
		return nil, err
	}
	if link.SenderUserID == claimerUserID {
		return nil, ErrGiftLinkOwnLink
	}

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE credit_share_links
		    SET status = ?, claimed_by_user_id = ?, claimed_by_email = ?, claimed_at = ?
		  WHERE id = ? AND status = ? AND (expires_at = '' OR expires_at > ?)`,
		TokenBankGiftStatusClaimed, claimerUserID, strings.TrimSpace(claimerEmail), stamp,
		link.ID, TokenBankGiftStatusActive, stamp)
	if err != nil {
		return nil, fmt.Errorf("claim gift link: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		// Distinguish "someone beat you to it" from "it expired" from "it was
		// revoked": the GUI shows a different message for each, and a client
		// that retries on the wrong one spins forever.
		current, lookupErr := r.giftLinkByID(ctx, tx, link.ID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		switch current.Status {
		case TokenBankGiftStatusActive:
			return nil, ErrGiftLinkExpired
		case TokenBankGiftStatusClaimed, TokenBankGiftStatusSettled:
			return nil, ErrGiftLinkNotActive
		default:
			return nil, ErrGiftLinkNotActive
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	link.Status = TokenBankGiftStatusClaimed
	link.ClaimedByUser = claimerUserID
	link.ClaimedByEmail = strings.TrimSpace(claimerEmail)
	link.ClaimedAt = now.UTC()
	return link, nil
}

// SettleClaimedGift turns a claimed link into real credits for the receiver.
//
// This is the point where money moves, and it moves as two ledger rows in one
// transaction: the sender's frozen credits are released and simultaneously
// booked as "granted" (they really did leave), and the receiver gets "received".
// Doing it as one transaction is what keeps SUM(ledger) globally consistent —
// a crash between the two would otherwise create or destroy credits.
//
// It is idempotent on the link status. A second call finds the row already
// settled and returns applied=false. Ledger rows that arrived without that
// status flip are not a finished settle: this call still writes the missing
// rows and flips the status, in the same transaction.
func (r *TokenBankRepo) SettleClaimedGift(ctx context.Context, linkID string, now time.Time) (applied bool, err error) {
	linkID = strings.TrimSpace(linkID)
	if linkID == "" {
		return false, fmt.Errorf("link id required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stamp := now.UTC().Format(time.RFC3339)

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	link, err := r.giftLinkByID(ctx, tx, linkID)
	if err != nil {
		return false, err
	}
	switch link.Status {
	case TokenBankGiftStatusClaimed:
		// The only state that may move money. The return time by itself does
		// not: the sender is told the claimer can still withdraw until the
		// sweeper flips this row. That flip is what the status update below
		// loses, and the ledger writes roll back with it.
	case TokenBankGiftStatusSettled:
		// Already done — by an earlier call, a retry, or an HA replay. Report
		// it as "nothing applied" rather than as an error: the caller asked for
		// a settle and the settle is in place, so returning an error here would
		// make a healthy replay look like a failure and could tempt the caller
		// into a compensating write.
		return false, tx.Commit()
	default:
		return false, ErrGiftLinkNotClaimed
	}

	// A replicated receive row is not a finished settle. The link is finished
	// only when this transaction flips it to settled. Committing the unfreeze
	// and grant on "receive already exists" left the row claimed, so the
	// sweeper could still return the freeze after the receiver had the credit.
	type giftMovement struct {
		prefix string
		userID string
		bucket string
		bizKey string
		amount int64
		note   string
	}
	movements := []giftMovement{
		{"unfrz", link.SenderUserID, TokenBankBucketFrozen, "unfreeze:" + linkID, -link.CreditsMicro, ""},
		{"grnt", link.SenderUserID, TokenBankBucketGranted, "grant:" + linkID, link.CreditsMicro, ""},
		{"rcv", link.ClaimedByUser, TokenBankBucketReceived, "receive:" + linkID, link.CreditsMicro, link.SenderEmail},
	}
	inserted := make([]giftMovement, 0, len(movements))
	for _, m := range movements {
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			giftLedgerID(m.prefix, m.bizKey), m.userID, m.bucket, m.amount,
			m.bizKey, "gift_link", linkID, m.note, stamp)
		if err != nil {
			return false, fmt.Errorf("insert gift %s ledger: %w", m.prefix, err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if rows == 0 {
			continue
		}
		if err := bumpTokenBankAccount(ctx, tx, m.userID, m.bucket, m.amount, now); err != nil {
			return false, err
		}
		inserted = append(inserted, m)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE credit_share_links SET status = ? WHERE id = ? AND status = ?`,
		TokenBankGiftStatusSettled, linkID, TokenBankGiftStatusClaimed)
	if err != nil {
		return false, fmt.Errorf("mark gift link settled: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed != 1 {
		// The link left "claimed" after we read it. The ledger rows in this
		// transaction roll back with it, so a return and a payout cannot both commit.
		return false, ErrGiftLinkNotClaimed
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	// Only the rows this transaction inserted. A row that was already here
	// has already been published by the writer that inserted it.
	for _, m := range inserted {
		r.publishGiftLedger(m.prefix, m.userID, m.bucket, m.bizKey, linkID, m.amount, m.note, now)
	}
	return true, nil
}

// RevokeGiftLink cancels an unclaimed link and releases its freeze. A link that
// was already claimed cannot be revoked — the claimer owns it now, and the
// credits will move to them when they withdraw.
//
// senderUserID is optional: empty means an admin revocation with no ownership
// check (§6.2 freezes a suspicious link). The unfreeze always credits the
// sender recorded on the row, never the argument, so passing a different id can
// only ever be refused — it can never redirect the refund.
func (r *TokenBankRepo) RevokeGiftLink(ctx context.Context, linkID, senderUserID string, now time.Time) error {
	linkID = strings.TrimSpace(linkID)
	senderUserID = strings.TrimSpace(senderUserID)
	if linkID == "" {
		return fmt.Errorf("link id required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stamp := now.UTC().Format(time.RFC3339)

	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	link, err := r.giftLinkByID(ctx, tx, linkID)
	if err != nil {
		return err
	}
	// An empty sender skips the check; a non-empty one must match. Reporting
	// not-found rather than forbidden keeps the endpoint from enumerating ids.
	if senderUserID != "" && link.SenderUserID != senderUserID {
		return ErrGiftLinkNotFound
	}
	if link.Status != TokenBankGiftStatusActive {
		return ErrGiftLinkNotActive
	}

	released, note, inserted, err := releaseGiftFreeze(ctx, tx, link.SenderUserID, linkID, link.CreditsMicro, now)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE credit_share_links SET status = ?, revoked_at = ? WHERE id = ? AND status = ?`,
		TokenBankGiftStatusRevoked, stamp, linkID, TokenBankGiftStatusActive); err != nil {
		return fmt.Errorf("revoke gift link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if inserted {
		r.publishGiftLedger("unfrz", link.SenderUserID, TokenBankBucketFrozen,
			"unfreeze:"+linkID, linkID, -released, note, now)
	}
	return nil
}

// releaseGiftFreeze writes the unfreeze row for a link, capped at the sender's
// current frozen ledger sum. A shortfall is recorded on that row's note and the
// caller still marks the link revoked or expired. The frozen bucket is never
// driven negative, and no extra bucket is invented for the gap.
func releaseGiftFreeze(ctx context.Context, tx *sql.Tx, sender, linkID string, credits int64, now time.Time) (released int64, note string, inserted bool, err error) {
	var sum int64
	if err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_micro), 0) FROM token_bank_ledger WHERE user_id = ? AND bucket = ?`,
		sender, TokenBankBucketFrozen).Scan(&sum); err != nil {
		return 0, "", false, fmt.Errorf("sum frozen bucket: %w", err)
	}
	released = credits
	if sum < released {
		released = sum
	}
	if released < 0 {
		released = 0
	}
	if shortfall := credits - released; shortfall > 0 {
		note = fmt.Sprintf("shortfall_micro=%d", shortfall)
	}
	stamp := now.UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		giftLedgerID("unfrz", "unfreeze:"+linkID), sender, TokenBankBucketFrozen,
		-released, "unfreeze:"+linkID, "gift_link", linkID, note, stamp)
	if err != nil {
		return 0, "", false, fmt.Errorf("insert gift unfreeze ledger: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, "", false, err
	}
	if affected == 0 {
		return released, note, false, nil
	}
	// The cache follows the ledger sum. Bumping by a delta would go negative
	// when the cache and the ledger have already drifted; the sum cannot,
	// because the row just inserted was capped at that sum.
	if _, err := tx.ExecContext(ctx,
		`UPDATE token_bank_accounts SET frozen_micro = (
			SELECT COALESCE(SUM(amount_micro), 0) FROM token_bank_ledger WHERE user_id = ? AND bucket = ?
		), updated_at = ? WHERE user_id = ?`,
		sender, TokenBankBucketFrozen, stamp, sender); err != nil {
		return 0, "", false, fmt.Errorf("sync frozen cache: %w", err)
	}
	return released, note, true, nil
}

// ExpireGiftLinks releases every freeze whose link ran out of time, including a
// link that was claimed but never withdrawn. Claim already refuses an expired
// active link; this sweep is what returns the sender's credits, and it is the
// same return for a claim that nobody withdrew before the deadline (§14.7).
func (r *TokenBankRepo) ExpireGiftLinks(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stamp := now.UTC().Format(time.RFC3339)
	rows, err := r.write.QueryContext(ctx,
		`SELECT id, sender_user_id, credits_micro FROM credit_share_links
		  WHERE status IN (?, ?) AND expires_at <> '' AND expires_at <= ?`,
		TokenBankGiftStatusActive, TokenBankGiftStatusClaimed, stamp)
	if err != nil {
		return 0, fmt.Errorf("list expired gift links: %w", err)
	}
	type pending struct {
		id      string
		sender  string
		credits int64
	}
	var expired []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.sender, &p.credits); err != nil {
			_ = rows.Close()
			return 0, err
		}
		expired = append(expired, p)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	count := 0
	for _, p := range expired {
		tx, err := r.write.BeginTx(ctx, nil)
		if err != nil {
			return count, fmt.Errorf("begin tx: %w", err)
		}
		released, note, inserted, err := releaseGiftFreeze(ctx, tx, p.sender, p.id, p.credits, now)
		if err != nil {
			_ = tx.Rollback()
			return count, err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE credit_share_links SET status = ? WHERE id = ? AND status IN (?, ?)`,
			TokenBankGiftStatusExpired, p.id, TokenBankGiftStatusActive, TokenBankGiftStatusClaimed)
		if err != nil {
			_ = tx.Rollback()
			return count, err
		}
		changed, err := res.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return count, err
		}
		if changed == 0 {
			_ = tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			return count, err
		}
		if inserted {
			r.publishGiftLedger("unfrz", p.sender, TokenBankBucketFrozen,
				"unfreeze:"+p.id, p.id, -released, note, now)
		}
		count++
	}
	return count, nil
}

// ListGiftLinks backs the "links I sent" view and the admin audit.
//
// senderUserID is optional: empty means every sender, which is what the §6.2
// audit endpoint needs. It must be an omitted predicate rather than
// `sender_user_id = ”` — the latter silently returns no rows at all, which an
// admin would read as "nobody has ever sent credits" instead of seeing the
// audit they asked for. It mirrors ListShares' owner filter for the same reason.
func (r *TokenBankRepo) ListGiftLinks(ctx context.Context, senderUserID, status string, limit int) ([]TokenBankGiftLink, error) {
	where := ""
	args := []any{}
	if sender := strings.TrimSpace(senderUserID); sender != "" {
		where += ` AND sender_user_id = ?`
		args = append(args, sender)
	}
	if s := strings.TrimSpace(status); s != "" {
		where += ` AND status = ?`
		args = append(args, s)
	}
	return r.listGiftLinks(ctx, where, args, limit)
}

// ListClaimedGiftLinks returns gifts this person claimed and has not withdrawn.
// A passed return time stays in the list until the sweeper changes the status:
// that is the window the sender is told the claimer can still withdraw.
// An empty claimer is refused: omitting the predicate would list every claim.
func (r *TokenBankRepo) ListClaimedGiftLinks(ctx context.Context, claimerUserID string, now time.Time, limit int) ([]TokenBankGiftLink, error) {
	claimerUserID = strings.TrimSpace(claimerUserID)
	if claimerUserID == "" {
		return nil, fmt.Errorf("claimer required")
	}
	return r.listGiftLinks(ctx,
		` AND claimed_by_user_id = ? AND status = ?`,
		[]any{claimerUserID, TokenBankGiftStatusClaimed},
		limit)
}

func (r *TokenBankRepo) listGiftLinks(ctx context.Context, where string, args []any, limit int) ([]TokenBankGiftLink, error) {
	query := `SELECT id, code, sender_user_id, sender_email, credits_micro, status,
		claimed_by_user_id, claimed_by_email, origin_node_id, expires_at, created_at, claimed_at, revoked_at
		FROM credit_share_links WHERE 1 = 1` + where + ` ORDER BY created_at DESC`
	queryArgs := append([]any{}, args...)
	if limit > 0 {
		query += ` LIMIT ?`
		queryArgs = append(queryArgs, limit)
	}
	rows, err := r.read.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list gift links: %w", err)
	}
	defer rows.Close()
	out := []TokenBankGiftLink{}
	for rows.Next() {
		link, err := scanGiftLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *link)
	}
	return out, rows.Err()
}

// CountGiftLinksSince is the read-side counterpart of countGiftLinksSince, for
// the summary endpoint that shows the user how much of today's allowance is
// left. It runs on the read pool because nothing here decides whether to mint a
// link; the authoritative check happens inside CreateGiftLink's transaction.
func (r *TokenBankRepo) CountGiftLinksSince(ctx context.Context, senderUserID string, since time.Time) (int, error) {
	senderUserID = strings.TrimSpace(senderUserID)
	if senderUserID == "" {
		return 0, nil
	}
	var n int
	err := r.read.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM credit_share_links
		  WHERE sender_user_id = ? AND created_at >= ?`,
		senderUserID, since.UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("count gift links: %w", err)
	}
	return n, nil
}

type giftLinkScanner interface {
	Scan(dest ...any) error
}

// startOfUTCDay is the boundary the daily share-link cap counts from.
//
// UTC, not the server's local zone: the limit is an anti-abuse counter, and a
// local-zone boundary would let a user on a rolling clock get two days' worth
// by straddling midnight. The stored `created_at` is RFC3339 UTC, so this
// comparison is string-safe and index-friendly.
func StartOfUTCDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

// countGiftLinksSince counts links this user created at or after `since`.
//
// It counts every link regardless of status, including revoked and expired
// ones. Counting only live links would make the cap trivially escapable:
// create, revoke, create, revoke — the point of the limit is to bound how many
// links are minted, not how many happen to be outstanding.
func countGiftLinksSince(ctx context.Context, tx *sql.Tx, senderUserID string, since time.Time) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM credit_share_links
		  WHERE sender_user_id = ? AND created_at >= ?`,
		senderUserID, since.UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("count gift links: %w", err)
	}
	return n, nil
}

func scanGiftLink(row giftLinkScanner) (*TokenBankGiftLink, error) {
	var link TokenBankGiftLink
	var expiresAt, createdAt, claimedAt, revokedAt string
	if err := row.Scan(&link.ID, &link.Code, &link.SenderUserID, &link.SenderEmail, &link.CreditsMicro,
		&link.Status, &link.ClaimedByUser, &link.ClaimedByEmail, &link.OriginNodeID,
		&expiresAt, &createdAt, &claimedAt, &revokedAt); err != nil {
		return nil, err
	}
	link.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	link.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	link.ClaimedAt, _ = time.Parse(time.RFC3339, claimedAt)
	link.RevokedAt, _ = time.Parse(time.RFC3339, revokedAt)
	return &link, nil
}

const giftLinkColumns = `id, code, sender_user_id, sender_email, credits_micro, status,
	claimed_by_user_id, claimed_by_email, origin_node_id, expires_at, created_at, claimed_at, revoked_at`

// GiftLinkByCode loads one link for the public preview endpoint. It reads on
// the read pool and moves nothing: the preview must be cheap enough to rate
// limit rather than authenticate, because the claimer has no relationship with
// this server yet.
func (r *TokenBankRepo) GiftLinkByCode(ctx context.Context, code string) (*TokenBankGiftLink, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrGiftLinkNotFound
	}
	return r.giftLinkByCode(ctx, r.read, code)
}

func (r *TokenBankRepo) giftLinkByCode(ctx context.Context, db *sql.DB, code string) (*TokenBankGiftLink, error) {
	link, err := scanGiftLink(db.QueryRowContext(ctx,
		`SELECT `+giftLinkColumns+` FROM credit_share_links WHERE code = ?`, code))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGiftLinkNotFound
		}
		return nil, fmt.Errorf("load gift link by code: %w", err)
	}
	return link, nil
}

// GiftLinkByID loads one link from the read pool. Withdrawal settlement uses
// it to confirm the caller is the claimer before any money moves.
func (r *TokenBankRepo) GiftLinkByID(ctx context.Context, id string) (*TokenBankGiftLink, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrGiftLinkNotFound
	}
	return r.giftLinkByID(ctx, r.read, id)
}

type giftRowQuery interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *TokenBankRepo) giftLinkByID(ctx context.Context, db giftRowQuery, id string) (*TokenBankGiftLink, error) {
	link, err := scanGiftLink(db.QueryRowContext(ctx,
		`SELECT `+giftLinkColumns+` FROM credit_share_links WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGiftLinkNotFound
		}
		return nil, fmt.Errorf("load gift link: %w", err)
	}
	return link, nil
}
