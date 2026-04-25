package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// 鈹€鈹€ SubmissionRepository implementation 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

func (s *Store) CreateSubmission(ctx context.Context, sub *SkillSubmission) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_submissions (id, email, user_id, skill_id, fingerprint, status, zip_path, error_msg, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.Email, sub.UserID, sub.SkillID, sub.Fingerprint,
		sub.Status, sub.ZipPath, sub.ErrorMsg,
		fmtTime(sub.CreatedAt), fmtTime(sub.UpdatedAt),
	)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) GetSubmissionByID(ctx context.Context, id string) (*SkillSubmission, error) {
	row := s.readDB.QueryRowContext(ctx, `
		SELECT id, email, user_id, skill_id, fingerprint, status, zip_path, error_msg, created_at, updated_at
		FROM sm_submissions WHERE id = ?`, id)

	var sub SkillSubmission
	var createdAt, updatedAt string
	err := row.Scan(
		&sub.ID, &sub.Email, &sub.UserID, &sub.SkillID, &sub.Fingerprint,
		&sub.Status, &sub.ZipPath, &sub.ErrorMsg, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sub.CreatedAt = parseTime(createdAt)
	sub.UpdatedAt = parseTime(updatedAt)
	return &sub, nil
}

func (s *Store) UpdateSubmissionStatus(ctx context.Context, id, status, errorMsg, skillID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_submissions SET status = ?, error_msg = ?, skill_id = ?, updated_at = ?
		WHERE id = ?`, status, errorMsg, skillID, time.Now().Format(timeFmt), id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

// CountRecentSubmissions 缁熻鎸囧畾 email 鍦ㄦ渶杩?withinHours 灏忔椂鍐呯殑鏈夋晥鎻愪氦鏁帮紙鎺掗櫎 failed锛夈€?
func (s *Store) CountRecentSubmissions(ctx context.Context, email string, withinHours int) (int, error) {
	since := time.Now().Add(-time.Duration(withinHours) * time.Hour).Format(timeFmt)
	var count int
	err := s.readDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sm_submissions
		WHERE email = ? AND created_at >= ? AND status IN ('pending','processing','success')`,
		email, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count recent submissions: %w", err)
	}
	return count, nil
}

// CountRecentDailySubmissions 缁熻鎸囧畾 email 鍦ㄤ粖澶╋紙UTC锛夌殑鏈夋晥鎻愪氦鏁般€?
func (s *Store) CountRecentDailySubmissions(ctx context.Context, email string) (int, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour).Format(timeFmt)
	var count int
	err := s.readDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sm_submissions
		WHERE email = ? AND created_at >= ? AND status IN ('pending','processing','success')`,
		email, today,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count daily submissions: %w", err)
	}
	return count, nil
}

// GetLatestSuccessSubmissionByFingerprint 鏌ヨ鍚?fingerprint 鐨勬渶鏂版垚鍔熸彁浜ゃ€?
func (s *Store) GetLatestSuccessSubmissionByFingerprint(ctx context.Context, fingerprint string) (*SkillSubmission, error) {
	row := s.readDB.QueryRowContext(ctx, `
		SELECT id, email, user_id, skill_id, fingerprint, status, zip_path, error_msg, created_at, updated_at
		FROM sm_submissions
		WHERE fingerprint = ? AND status = 'success'
		ORDER BY created_at DESC LIMIT 1`, fingerprint)

	var sub SkillSubmission
	var createdAt, updatedAt string
	err := row.Scan(
		&sub.ID, &sub.Email, &sub.UserID, &sub.SkillID, &sub.Fingerprint,
		&sub.Status, &sub.ZipPath, &sub.ErrorMsg, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sub.CreatedAt = parseTime(createdAt)
	sub.UpdatedAt = parseTime(updatedAt)
	return &sub, nil
}

// CountSuccessSubmissionsByFingerprint 缁熻鍚?fingerprint 鐨勬垚鍔熸彁浜ゆ暟銆?
func (s *Store) CountSuccessSubmissionsByFingerprint(ctx context.Context, fingerprint string) (int, error) {
	var count int
	err := s.readDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sm_submissions
		WHERE fingerprint = ? AND status = 'success'`, fingerprint,
	).Scan(&count)
	return count, err
}

// UpdateSubmissionFingerprint 鏇存柊 submission 鐨?fingerprint銆?
func (s *Store) UpdateSubmissionFingerprint(ctx context.Context, id, fingerprint string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sm_submissions SET fingerprint = ?, updated_at = ? WHERE id = ?`,
		fingerprint, time.Now().Format(timeFmt), id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}
