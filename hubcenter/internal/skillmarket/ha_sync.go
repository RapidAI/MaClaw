package skillmarket

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

type SyncRecorder interface {
	AppendSkillMarketSnapshot(ctx context.Context, snapshot *Snapshot)
}

const (
	syncEmissionMaxDumpRetries = 3
	syncEmissionRetryDelay     = 100 * time.Millisecond
)

type SnapshotUser struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Status            string `json:"status"`
	VerifyMethod      string `json:"verify_method"`
	Credits           int64  `json:"credits"`
	SettledCredits    int64  `json:"settled_credits"`
	PendingSettlement int64  `json:"pending_settlement"`
	Debt              int64  `json:"debt"`
	VoucherCount      int    `json:"voucher_count"`
	VoucherExpiresAt  string `json:"voucher_expires_at"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	VerifiedAt        string `json:"verified_at"`
	PasswordHash      string `json:"password_hash"`
}

type Snapshot struct {
	Users                 []SnapshotUser         `json:"users"`
	Transactions          []CreditsTransaction   `json:"transactions"`
	Submissions           []SkillSubmission      `json:"submissions"`
	Purchases             []PurchaseRecord       `json:"purchases"`
	Ratings               []Rating               `json:"ratings"`
	Configs               []AdminConfig          `json:"configs"`
	Tiers                 []UploaderTier         `json:"tiers"`
	AuthTokens            []AuthToken            `json:"auth_tokens"`
	Sessions              []Session              `json:"sessions"`
	SessionRevocations    []SessionRevocation    `json:"session_revocations"`
	APIKeys               []APIKey               `json:"api_keys"`
	PendingKeyOrders      []PendingKeyOrder      `json:"pending_key_orders"`
	NotificationSequences []NotificationSequence `json:"notification_sequences"`
}

func (s *Store) SetSyncRecorder(rec SyncRecorder) {
	if s == nil {
		return
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.sync = rec
}

func (s *Store) emitSync(ctx context.Context) {
	if s == nil || s.currentSyncRecorder() == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	if !s.beginSyncEmission() {
		return
	}
	go s.runSyncEmission(ctx)
}

func (s *Store) runSyncEmission(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[hubcenter][skillmarket] sync emission recovered: %v", r)
			s.recoverSyncEmission(ctx)
		}
	}()
	for {
		snapshot, err := s.dumpSnapshotWithRetry(ctx)
		if err == nil {
			if rec := s.currentSyncRecorder(); rec != nil {
				rec.AppendSkillMarketSnapshot(ctx, snapshot)
			}
		}
		if !s.finishOrContinueSyncEmission() {
			return
		}
	}
}

func (s *Store) dumpSnapshotWithRetry(ctx context.Context) (*Snapshot, error) {
	var lastErr error
	for attempt := 0; attempt <= syncEmissionMaxDumpRetries; attempt++ {
		snapshot, err := s.DumpSnapshot(ctx)
		if err == nil {
			if attempt > 0 {
				log.Printf("[hubcenter][skillmarket] sync snapshot dump recovered after %d retries", attempt)
			}
			return snapshot, nil
		}
		lastErr = err
		if attempt < syncEmissionMaxDumpRetries {
			time.Sleep(syncEmissionRetryDelay)
		}
	}
	log.Printf("[hubcenter][skillmarket] sync snapshot dump failed after %d retries: %v", syncEmissionMaxDumpRetries, lastErr)
	return nil, lastErr
}

func (s *Store) recoverSyncEmission(ctx context.Context) {
	if s == nil {
		return
	}
	restart := false
	s.syncMu.Lock()
	if s.syncPending && s.sync != nil {
		s.syncPending = false
		s.syncRunning = true
		restart = true
	} else {
		s.syncRunning = false
		s.syncPending = false
	}
	s.syncMu.Unlock()
	if restart {
		go s.runSyncEmission(ctx)
	}
}

func (s *Store) currentSyncRecorder() SyncRecorder {
	if s == nil {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	return s.sync
}

func (s *Store) beginSyncEmission() bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncRunning {
		s.syncPending = true
		return false
	}
	s.syncRunning = true
	return true
}

func (s *Store) finishOrContinueSyncEmission() bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncPending {
		s.syncPending = false
		return true
	}
	s.syncRunning = false
	return false
}

// CountSnapshotRecords returns the same aggregate record count as DumpSnapshot,
// but uses cheap COUNT(*) queries so HA status rendering does not need to load
// the full skillmarket state.
func (s *Store) CountSnapshotRecords(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	tables := []string{
		"sm_users",
		"sm_credits_transactions",
		"sm_submissions",
		"sm_purchase_records",
		"sm_ratings",
		"sm_admin_config",
		"sm_uploader_tiers",
		"sm_auth_tokens",
		"sm_sessions",
		"sm_session_revocations",
		"sm_api_keys",
		"sm_pending_key_orders",
		"sm_notification_sequences",
	}
	var total int64
	for _, table := range tables {
		var count int64
		if err := s.readDB.QueryRowContext(ctx, "SELECT COUNT(1) FROM "+table).Scan(&count); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return 0, fmt.Errorf("count %s: %w", table, err)
		}
		total += count
	}
	return total, nil
}

func (s *Store) DumpSnapshot(ctx context.Context) (*Snapshot, error) {
	if s == nil {
		return &Snapshot{}, nil
	}
	snap := &Snapshot{}
	var err error
	if snap.Users, err = s.dumpUsers(ctx); err != nil {
		return nil, err
	}
	if snap.Transactions, err = s.dumpTransactions(ctx); err != nil {
		return nil, err
	}
	if snap.Submissions, err = s.dumpSubmissions(ctx); err != nil {
		return nil, err
	}
	if snap.Purchases, err = s.dumpPurchases(ctx); err != nil {
		return nil, err
	}
	if snap.Ratings, err = s.dumpRatings(ctx); err != nil {
		return nil, err
	}
	if snap.Configs, err = s.dumpConfigs(ctx); err != nil {
		return nil, err
	}
	if snap.Tiers, err = s.dumpTiers(ctx); err != nil {
		return nil, err
	}
	if snap.AuthTokens, err = s.dumpAuthTokens(ctx); err != nil {
		return nil, err
	}
	if snap.Sessions, err = s.dumpSessions(ctx); err != nil {
		return nil, err
	}
	if snap.SessionRevocations, err = s.dumpSessionRevocations(ctx); err != nil {
		return nil, err
	}
	if snap.APIKeys, err = s.dumpAPIKeys(ctx); err != nil {
		return nil, err
	}
	if snap.PendingKeyOrders, err = s.dumpPendingKeyOrders(ctx); err != nil {
		return nil, err
	}
	if snap.NotificationSequences, err = s.dumpNotificationSequences(ctx); err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *Store) LoadSnapshot(ctx context.Context, snap *Snapshot) error {
	if s == nil || snap == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	userIDAliases := map[string]string{}
	for _, item := range snap.Users {
		canonicalID, err := upsertSnapshotUser(ctx, tx, item)
		if err != nil {
			return fmt.Errorf("insert sm_users: %w", err)
		}
		if canonicalID != "" && canonicalID != item.ID {
			userIDAliases[item.ID] = canonicalID
		}
	}
	for _, item := range snap.Transactions {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID, canonicalSnapshotUserID(userIDAliases, item.UserID), item.Type, item.Amount, item.Balance, item.SkillID, item.PurchaseID, item.Description, fmtTime(item.CreatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_credits_transactions: %w", err)
		}
	}
	for _, item := range snap.Submissions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_submissions (id, email, user_id, skill_id, fingerprint, status, zip_path, error_msg, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				email = excluded.email,
				user_id = excluded.user_id,
				skill_id = excluded.skill_id,
				fingerprint = excluded.fingerprint,
				status = excluded.status,
				zip_path = excluded.zip_path,
				error_msg = excluded.error_msg,
				created_at = excluded.created_at,
				updated_at = excluded.updated_at
			WHERE excluded.updated_at >= sm_submissions.updated_at`,
			item.ID, item.Email, canonicalSnapshotUserID(userIDAliases, item.UserID), item.SkillID, item.Fingerprint, item.Status, item.ZipPath, item.ErrorMsg, fmtTime(item.CreatedAt), fmtTime(item.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_submissions: %w", err)
		}
	}
	for _, item := range snap.Purchases {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_purchase_records (id, buyer_email, buyer_id, skill_id, purchased_version, purchase_type, amount_paid, platform_fee, seller_earning, seller_id, key_status, api_key_id, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				buyer_email = excluded.buyer_email,
				buyer_id = excluded.buyer_id,
				skill_id = excluded.skill_id,
				purchased_version = excluded.purchased_version,
				purchase_type = excluded.purchase_type,
				amount_paid = excluded.amount_paid,
				platform_fee = excluded.platform_fee,
				seller_earning = excluded.seller_earning,
				seller_id = excluded.seller_id,
				key_status = CASE
					WHEN sm_purchase_records.key_status = 'refunded' AND excluded.key_status != 'refunded' THEN sm_purchase_records.key_status
					WHEN sm_purchase_records.key_status = 'key_delivered' AND excluded.key_status IN ('', 'pending_key') THEN sm_purchase_records.key_status
					WHEN sm_purchase_records.key_status = 'cancelled' AND excluded.key_status = 'pending_key' THEN sm_purchase_records.key_status
					ELSE excluded.key_status
				END,
				api_key_id = CASE
					WHEN sm_purchase_records.key_status = 'key_delivered' AND excluded.api_key_id = '' THEN sm_purchase_records.api_key_id
					ELSE excluded.api_key_id
				END,
				status = CASE
					WHEN sm_purchase_records.status = 'refunded' AND excluded.status != 'refunded' THEN sm_purchase_records.status
					ELSE excluded.status
				END,
				created_at = excluded.created_at`,
			item.ID, item.BuyerEmail, canonicalSnapshotUserID(userIDAliases, item.BuyerID), item.SkillID, item.PurchasedVersion, item.PurchaseType, item.AmountPaid, item.PlatformFee, item.SellerEarning, canonicalSnapshotUserID(userIDAliases, item.SellerID), item.KeyStatus, item.APIKeyID, item.Status, fmtTime(item.CreatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_purchase_records: %w", err)
		}
	}
	for _, item := range snap.Ratings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_ratings (skill_id, email, score, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(skill_id, email) DO UPDATE SET
				score = excluded.score,
				created_at = excluded.created_at,
				updated_at = excluded.updated_at
			WHERE excluded.updated_at >= sm_ratings.updated_at`,
			item.SkillID, item.Email, item.Score, fmtTime(item.CreatedAt), fmtTime(item.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_ratings: %w", err)
		}
	}
	for _, item := range snap.Configs {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO sm_admin_config (key, value) VALUES (?, ?)`, item.Key, item.Value); err != nil {
			return fmt.Errorf("insert sm_admin_config: %w", err)
		}
	}
	for _, item := range snap.Tiers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_uploader_tiers (user_id, tier, published_count, avg_rating, total_downloads, updated_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id) DO UPDATE SET
				tier = excluded.tier,
				published_count = excluded.published_count,
				avg_rating = excluded.avg_rating,
				total_downloads = excluded.total_downloads,
				updated_at = excluded.updated_at
			WHERE excluded.updated_at >= sm_uploader_tiers.updated_at`,
			canonicalSnapshotUserID(userIDAliases, item.UserID), item.Tier, item.PublishedCount, item.AvgRating, item.TotalDownloads, fmtTime(item.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_uploader_tiers: %w", err)
		}
	}
	for _, item := range snap.AuthTokens {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO sm_auth_tokens (token, user_id, token_type, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
			item.Token, canonicalSnapshotUserID(userIDAliases, item.UserID), item.TokenType, fmtTime(item.ExpiresAt), fmtTime(item.CreatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_auth_tokens: %w", err)
		}
	}
	for _, item := range snap.Sessions {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO sm_sessions (token, user_id, email, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
			item.Token, canonicalSnapshotUserID(userIDAliases, item.UserID), item.Email, fmtTime(item.ExpiresAt), fmtTime(item.CreatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_sessions: %w", err)
		}
	}
	for _, item := range snap.SessionRevocations {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO sm_session_revocations (token, expires_at, revoked_at) VALUES (?, ?, ?)`,
			item.Token, fmtTime(item.ExpiresAt), fmtTime(item.RevokedAt),
		); err != nil {
			return fmt.Errorf("insert sm_session_revocations: %w", err)
		}
	}
	for _, item := range snap.APIKeys {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_api_keys (id, skill_id, env_name, encrypted_key, status, buyer_email, assigned_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				skill_id = excluded.skill_id,
				env_name = excluded.env_name,
				encrypted_key = excluded.encrypted_key,
				status = CASE
					WHEN sm_api_keys.status = 'refunded' AND excluded.status != 'refunded' THEN sm_api_keys.status
					WHEN sm_api_keys.status = 'assigned' AND excluded.status = 'available' THEN sm_api_keys.status
					ELSE excluded.status
				END,
				buyer_email = CASE
					WHEN sm_api_keys.status IN ('assigned', 'refunded') AND excluded.status = 'available' THEN sm_api_keys.buyer_email
					ELSE excluded.buyer_email
				END,
				assigned_at = CASE
					WHEN sm_api_keys.status IN ('assigned', 'refunded') AND excluded.status = 'available' THEN sm_api_keys.assigned_at
					ELSE excluded.assigned_at
				END,
				created_at = excluded.created_at`,
			item.ID, item.SkillID, item.EnvName, item.EncryptedKey, item.Status, item.BuyerEmail, fmtTime(item.AssignedAt), fmtTime(item.CreatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_api_keys: %w", err)
		}
	}
	for _, item := range snap.PendingKeyOrders {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_pending_key_orders (id, purchase_record_id, skill_id, buyer_email, env_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				purchase_record_id = excluded.purchase_record_id,
				skill_id = excluded.skill_id,
				buyer_email = excluded.buyer_email,
				env_name = excluded.env_name,
				status = excluded.status,
				created_at = excluded.created_at,
				updated_at = excluded.updated_at
			WHERE excluded.updated_at >= sm_pending_key_orders.updated_at`,
			item.ID, item.PurchaseRecordID, item.SkillID, item.BuyerEmail, item.EnvName, item.Status, fmtTime(item.CreatedAt), fmtTime(item.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_pending_key_orders: %w", err)
		}
	}
	for _, item := range snap.NotificationSequences {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_notification_sequences (id, notification_type, target_email, trigger_context, subject, body, sent_count, next_send_at, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				notification_type = excluded.notification_type,
				target_email = excluded.target_email,
				trigger_context = excluded.trigger_context,
				subject = excluded.subject,
				body = excluded.body,
				sent_count = excluded.sent_count,
				next_send_at = excluded.next_send_at,
				is_active = excluded.is_active,
				created_at = excluded.created_at,
				updated_at = excluded.updated_at
			WHERE excluded.updated_at >= sm_notification_sequences.updated_at`,
			item.ID, item.NotificationType, item.TargetEmail, item.TriggerContext, item.Subject, item.Body, item.SentCount, fmtTime(item.NextSendAt), boolToInt(item.IsActive), fmtTime(item.CreatedAt), fmtTime(item.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert sm_notification_sequences: %w", err)
		}
	}

	return tx.Commit()
}

func canonicalSnapshotUserID(aliases map[string]string, userID string) string {
	if aliases == nil {
		return userID
	}
	if canonicalID, ok := aliases[userID]; ok {
		return canonicalID
	}
	return userID
}

func upsertSnapshotUser(ctx context.Context, tx *sql.Tx, item SnapshotUser) (string, error) {
	item.Email = normalizeEmail(item.Email)
	var existingID, existingUpdatedAt string
	err := tx.QueryRowContext(ctx, `SELECT id, updated_at FROM sm_users WHERE email = ? COLLATE NOCASE LIMIT 1`, item.Email).Scan(&existingID, &existingUpdatedAt)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if err == nil && existingID != "" && existingID != item.ID {
		if parseTime(item.UpdatedAt).Before(parseTime(existingUpdatedAt)) {
			return existingID, nil
		}
		if err := migrateSnapshotUserReferences(ctx, tx, existingID, item.ID); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sm_users WHERE id = ?`, existingID); err != nil {
			return "", err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sm_users (id, email, status, verify_method, credits, settled_credits, pending_settlement, debt, voucher_count, voucher_expires_at, created_at, updated_at, verified_at, password_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			email = excluded.email,
			status = excluded.status,
			verify_method = excluded.verify_method,
			credits = excluded.credits,
			settled_credits = excluded.settled_credits,
			pending_settlement = excluded.pending_settlement,
			debt = excluded.debt,
			voucher_count = excluded.voucher_count,
			voucher_expires_at = excluded.voucher_expires_at,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			verified_at = excluded.verified_at,
			password_hash = excluded.password_hash
		WHERE excluded.updated_at >= sm_users.updated_at`,
		item.ID, item.Email, item.Status, item.VerifyMethod, item.Credits, item.SettledCredits, item.PendingSettlement, item.Debt, item.VoucherCount, item.VoucherExpiresAt, item.CreatedAt, item.UpdatedAt, item.VerifiedAt, item.PasswordHash,
	)
	return item.ID, err
}

func migrateSnapshotUserReferences(ctx context.Context, tx *sql.Tx, fromID, toID string) error {
	if fromID == "" || toID == "" || fromID == toID {
		return nil
	}
	updates := []struct {
		table string
		col   string
	}{
		{"sm_credits_transactions", "user_id"},
		{"sm_submissions", "user_id"},
		{"sm_purchase_records", "buyer_id"},
		{"sm_purchase_records", "seller_id"},
		{"sm_uploader_tiers", "user_id"},
		{"sm_auth_tokens", "user_id"},
		{"sm_sessions", "user_id"},
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE `+update.table+` SET `+update.col+` = ? WHERE `+update.col+` = ?`, toID, fromID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) dumpUsers(ctx context.Context) ([]SnapshotUser, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, email, status, verify_method, credits, settled_credits, pending_settlement, debt, voucher_count, voucher_expires_at, created_at, updated_at, verified_at, password_hash FROM sm_users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SnapshotUser
	for rows.Next() {
		var item SnapshotUser
		if err := rows.Scan(&item.ID, &item.Email, &item.Status, &item.VerifyMethod, &item.Credits, &item.SettledCredits, &item.PendingSettlement, &item.Debt, &item.VoucherCount, &item.VoucherExpiresAt, &item.CreatedAt, &item.UpdatedAt, &item.VerifiedAt, &item.PasswordHash); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpTransactions(ctx context.Context) ([]CreditsTransaction, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at FROM sm_credits_transactions ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CreditsTransaction
	for rows.Next() {
		var item CreditsTransaction
		var createdAt string
		if err := rows.Scan(&item.ID, &item.UserID, &item.Type, &item.Amount, &item.Balance, &item.SkillID, &item.PurchaseID, &item.Description, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpSubmissions(ctx context.Context) ([]SkillSubmission, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, email, user_id, skill_id, fingerprint, status, zip_path, error_msg, created_at, updated_at FROM sm_submissions ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SkillSubmission
	for rows.Next() {
		var item SkillSubmission
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Email, &item.UserID, &item.SkillID, &item.Fingerprint, &item.Status, &item.ZipPath, &item.ErrorMsg, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpPurchases(ctx context.Context) ([]PurchaseRecord, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, buyer_email, buyer_id, skill_id, purchased_version, purchase_type, amount_paid, platform_fee, seller_earning, seller_id, key_status, api_key_id, status, created_at FROM sm_purchase_records ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PurchaseRecord
	for rows.Next() {
		item, err := s.scanPurchaseRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Store) dumpRatings(ctx context.Context) ([]Rating, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT skill_id, email, score, created_at, updated_at FROM sm_ratings ORDER BY skill_id, email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rating
	for rows.Next() {
		var item Rating
		var createdAt, updatedAt string
		if err := rows.Scan(&item.SkillID, &item.Email, &item.Score, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpConfigs(ctx context.Context) ([]AdminConfig, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT key, value FROM sm_admin_config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminConfig
	for rows.Next() {
		var item AdminConfig
		if err := rows.Scan(&item.Key, &item.Value); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpTiers(ctx context.Context) ([]UploaderTier, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT user_id, tier, published_count, avg_rating, total_downloads, updated_at FROM sm_uploader_tiers ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UploaderTier
	for rows.Next() {
		var item UploaderTier
		var updatedAt string
		if err := rows.Scan(&item.UserID, &item.Tier, &item.PublishedCount, &item.AvgRating, &item.TotalDownloads, &updatedAt); err != nil {
			return nil, err
		}
		item.UpdatedAt = parseTime(updatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpAuthTokens(ctx context.Context) ([]AuthToken, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT token, user_id, token_type, expires_at, created_at FROM sm_auth_tokens ORDER BY token`)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []AuthToken
	for rows.Next() {
		var item AuthToken
		var expiresAt, createdAt string
		if err := rows.Scan(&item.Token, &item.UserID, &item.TokenType, &expiresAt, &createdAt); err != nil {
			return nil, err
		}
		item.ExpiresAt = parseTime(expiresAt)
		item.CreatedAt = parseTime(createdAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT token, user_id, email, expires_at, created_at FROM sm_sessions ORDER BY token`)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var item Session
		var expiresAt, createdAt string
		if err := rows.Scan(&item.Token, &item.UserID, &item.Email, &expiresAt, &createdAt); err != nil {
			return nil, err
		}
		item.ExpiresAt = parseTime(expiresAt)
		item.CreatedAt = parseTime(createdAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpSessionRevocations(ctx context.Context) ([]SessionRevocation, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT token, expires_at, revoked_at FROM sm_session_revocations ORDER BY token`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRevocation
	for rows.Next() {
		var item SessionRevocation
		var expiresAt, revokedAt string
		if err := rows.Scan(&item.Token, &expiresAt, &revokedAt); err != nil {
			return nil, err
		}
		item.ExpiresAt = parseTime(expiresAt)
		item.RevokedAt = parseTime(revokedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, skill_id, env_name, encrypted_key, status, buyer_email, assigned_at, created_at FROM sm_api_keys ORDER BY created_at, id`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var item APIKey
		var assignedAt, createdAt string
		if err := rows.Scan(&item.ID, &item.SkillID, &item.EnvName, &item.EncryptedKey, &item.Status, &item.BuyerEmail, &assignedAt, &createdAt); err != nil {
			return nil, err
		}
		item.AssignedAt = parseTime(assignedAt)
		item.CreatedAt = parseTime(createdAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpPendingKeyOrders(ctx context.Context) ([]PendingKeyOrder, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, purchase_record_id, skill_id, buyer_email, env_name, status, created_at, updated_at FROM sm_pending_key_orders ORDER BY created_at, id`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var out []PendingKeyOrder
	for rows.Next() {
		var item PendingKeyOrder
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.PurchaseRecordID, &item.SkillID, &item.BuyerEmail, &item.EnvName, &item.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dumpNotificationSequences(ctx context.Context) ([]NotificationSequence, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, notification_type, target_email, trigger_context, subject, body, sent_count, next_send_at, is_active, created_at, updated_at FROM sm_notification_sequences ORDER BY created_at, id`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var out []NotificationSequence
	for rows.Next() {
		var item NotificationSequence
		var nextSendAt, createdAt, updatedAt string
		var isActive int
		if err := rows.Scan(&item.ID, &item.NotificationType, &item.TargetEmail, &item.TriggerContext, &item.Subject, &item.Body, &item.SentCount, &nextSendAt, &isActive, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.NextSendAt = parseTime(nextSendAt)
		item.IsActive = isActive != 0
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
