package skillmarket

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrRedeemCardUnavailable = errors.New("redeem card is unavailable")
	ErrRedeemCardNotActive   = errors.New("redeem card is no longer active")
)

func newCreditRedeemCode() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "CRD-" + strings.ToUpper(hex.EncodeToString(buf)), nil
}

var creditRedeemCodeGenerator = newCreditRedeemCode
var creditRedeemIDGenerator = generateID

func (s *CreditsService) IssueRedeemCards(ctx context.Context, credits int64, count int, issuedBy string) ([]CreditRedeemCard, error) {
	if s == nil || s.store == nil || credits <= 0 || count < 1 || count > 1000 {
		return nil, fmt.Errorf("invalid redeem card issue request")
	}
	now := time.Now()
	cards := make([]CreditRedeemCard, 0, count)
	for i := 0; i < count; i++ {
		code, err := creditRedeemCodeGenerator()
		if err != nil {
			return nil, err
		}
		cards = append(cards, CreditRedeemCard{ID: creditRedeemIDGenerator(), Code: code, Credits: credits, Status: "active", IssuedBy: strings.TrimSpace(issuedBy), IssuedAt: now})
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i := range cards {
		card := &cards[i]
		for attempts := 0; ; attempts++ {
			_, err := tx.ExecContext(ctx, `INSERT INTO sm_credit_redeem_cards (id, code, credits, status, issued_by, issued_at) VALUES (?, ?, ?, 'active', ?, ?)`, card.ID, card.Code, card.Credits, card.IssuedBy, card.IssuedAt.Format(timeFmt))
			if err == nil {
				break
			}
			if !isRedeemCardCodeConflict(err) || attempts == 4 {
				return nil, err
			}
			code, codeErr := creditRedeemCodeGenerator()
			if codeErr != nil {
				return nil, codeErr
			}
			card.Code = code
			// The database reports duplicate code and duplicate ID through the same
			// constraint family. Refresh both values so either collision can recover.
			card.ID = creditRedeemIDGenerator()
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.store.emitSync(ctx)
	return cards, nil
}

func isRedeemCardCodeConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed: sm_credit_redeem_cards.code") ||
		strings.Contains(message, "unique constraint failed: sm_credit_redeem_cards.id")
}

func (s *CreditsService) RedeemCard(ctx context.Context, userID, email, code string) (int64, error) {
	if s == nil || s.store == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(code) == "" {
		return 0, ErrRedeemCardUnavailable
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id, status string
	var credits int64
	if err := tx.QueryRowContext(ctx, `SELECT id, credits, status FROM sm_credit_redeem_cards WHERE code=?`, strings.ToUpper(strings.TrimSpace(code))).Scan(&id, &credits, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrRedeemCardUnavailable
		}
		return 0, err
	}
	if status != "active" {
		return 0, ErrRedeemCardNotActive
	}
	u, err := s.store.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return 0, err
	}
	if u.Status != "verified" {
		return 0, ErrUnverifiedAccount
	}
	if credits > math.MaxInt64-u.Credits {
		return 0, fmt.Errorf("credits balance exceeds maximum value")
	}
	now := time.Now().Format(timeFmt)
	newBalance := u.Credits + credits
	result, err := tx.ExecContext(ctx, `UPDATE sm_credit_redeem_cards SET status='redeemed', redeemed_by_user_id=?, redeemed_by_email=?, redeemed_at=? WHERE id=? AND status='active'`, userID, normalizeEmail(email), now, id)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected != 1 {
		return 0, ErrRedeemCardNotActive
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET credits=?, updated_at=? WHERE id=?`, newBalance, now, userID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'topup', ?, ?, '', ?, 'Redeem card top up', ?)`, generateID(), userID, credits, newBalance, id, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.store.emitSync(ctx)
	return newBalance, nil
}

func (s *CreditsService) RevokeRedeemCard(ctx context.Context, id, revokedBy string) error {
	if s == nil || s.store == nil || strings.TrimSpace(id) == "" {
		return ErrRedeemCardUnavailable
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE sm_credit_redeem_cards SET status='revoked', revoked_by=?, revoked_at=? WHERE id=? AND status='active'`, strings.TrimSpace(revokedBy), time.Now().Format(timeFmt), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRedeemCardNotActive
	}
	s.store.emitSync(ctx)
	return nil
}

func (s *CreditsService) ExportUnusedRedeemCards(ctx context.Context, onlyUnexported bool, exportedBy string) ([]CreditRedeemCard, error) {
	if s == nil || s.store == nil {
		return nil, ErrRedeemCardUnavailable
	}
	condition := `status='active'`
	if onlyUnexported {
		condition += ` AND exported_at=''`
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+creditRedeemCardColumns+` FROM sm_credit_redeem_cards WHERE `+condition+` ORDER BY issued_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	cards := make([]CreditRedeemCard, 0)
	for rows.Next() {
		card, err := s.store.scanCreditRedeemCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *card)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(cards) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE sm_credit_redeem_cards SET exported_at=?, exported_by=? WHERE `+condition, time.Now().Format(timeFmt), strings.TrimSpace(exportedBy)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if len(cards) > 0 {
		s.store.emitSync(ctx)
	}
	return cards, nil
}
