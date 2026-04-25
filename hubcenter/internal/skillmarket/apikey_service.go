package skillmarket

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"time"
)

type APIKey struct {
	ID           string    `json:"id"`
	SkillID      string    `json:"skill_id"`
	EnvName      string    `json:"env_name"`
	EncryptedKey string    `json:"-"`
	Status       string    `json:"status"`
	BuyerEmail   string    `json:"buyer_email,omitempty"`
	AssignedAt   time.Time `json:"assigned_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type PendingKeyOrder struct {
	ID               string    `json:"id"`
	PurchaseRecordID string    `json:"purchase_record_id"`
	SkillID          string    `json:"skill_id"`
	BuyerEmail       string    `json:"buyer_email"`
	EnvName          string    `json:"env_name"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type APIKeyPoolService struct {
	store     *Store
	encSecret []byte
}

func NewAPIKeyPoolService(store *Store, encSecret []byte) (*APIKeyPoolService, error) {
	svc := &APIKeyPoolService{store: store, encSecret: encSecret}
	if err := svc.migrate(); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *APIKeyPoolService) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sm_api_keys (
			id            TEXT PRIMARY KEY,
			skill_id      TEXT NOT NULL,
			env_name      TEXT NOT NULL DEFAULT '',
			encrypted_key TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'available',
			buyer_email   TEXT NOT NULL DEFAULT '',
			assigned_at   TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_api_keys_skill_status ON sm_api_keys(skill_id, status);`,
		`CREATE TABLE IF NOT EXISTS sm_pending_key_orders (
			id                 TEXT PRIMARY KEY,
			purchase_record_id TEXT NOT NULL,
			skill_id           TEXT NOT NULL,
			buyer_email        TEXT NOT NULL,
			env_name           TEXT NOT NULL DEFAULT '',
			status             TEXT NOT NULL DEFAULT 'pending_key',
			created_at         TEXT NOT NULL,
			updated_at         TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_pending_key_skill ON sm_pending_key_orders(skill_id, status, created_at);`,
	}
	for _, stmt := range stmts {
		if _, err := s.store.db.Exec(stmt); err != nil {
			return fmt.Errorf("apikey migrate: %w", err)
		}
	}
	return nil
}

func (s *APIKeyPoolService) UploadKeys(ctx context.Context, skillID, envName string, keys []string) (int, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count := 0
	now := fmtTime(time.Now())
	for _, key := range keys {
		if key == "" {
			continue
		}
		enc, err := s.encryptKey(key)
		if err != nil {
			return 0, fmt.Errorf("encrypt key: %w", err)
		}
		id := generateID()
		if _, err = tx.ExecContext(ctx, `INSERT INTO sm_api_keys (id, skill_id, env_name, encrypted_key, status, created_at) VALUES (?, ?, ?, ?, 'available', ?)`, id, skillID, envName, enc, now); err != nil {
			return 0, err
		}
		count++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.store.emitSync(ctx)
	return count, nil
}

func (s *APIKeyPoolService) AssignKey(ctx context.Context, skillID, buyerEmail, envName string) (*APIKey, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var k APIKey
	var encKey, createdAt string
	err = tx.QueryRowContext(ctx, `SELECT id, skill_id, env_name, encrypted_key, created_at FROM sm_api_keys WHERE skill_id = ? AND status = 'available' AND env_name = ? LIMIT 1`, skillID, envName).Scan(&k.ID, &k.SkillID, &k.EnvName, &encKey, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("no available key for skill %s env %s", skillID, envName)
	}
	now := fmtTime(time.Now())
	if _, err = tx.ExecContext(ctx, `UPDATE sm_api_keys SET status = 'assigned', buyer_email = ?, assigned_at = ? WHERE id = ?`, buyerEmail, now, k.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.store.emitSync(ctx)
	k.Status = "assigned"
	k.BuyerEmail = buyerEmail
	k.AssignedAt = parseTime(now)
	k.EncryptedKey = encKey
	k.CreatedAt = parseTime(createdAt)
	return &k, nil
}

func (s *APIKeyPoolService) DecryptAssignedKey(k *APIKey) (string, error) {
	return s.decryptKey(k.EncryptedKey)
}

func (s *APIKeyPoolService) CreatePendingOrder(ctx context.Context, purchaseRecordID, skillID, buyerEmail, envName string) error {
	now := fmtTime(time.Now())
	id := generateID()
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO sm_pending_key_orders (id, purchase_record_id, skill_id, buyer_email, env_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'pending_key', ?, ?)`, id, purchaseRecordID, skillID, buyerEmail, envName, now, now)
	if err == nil {
		s.store.emitSync(ctx)
	}
	return err
}

func (s *APIKeyPoolService) FulfillPendingOrders(ctx context.Context, skillID string) (int, error) {
	rows, err := s.store.readDB.QueryContext(ctx, `SELECT id, purchase_record_id, buyer_email, env_name FROM sm_pending_key_orders WHERE skill_id = ? AND status = 'pending_key' ORDER BY created_at ASC`, skillID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type order struct{ id, purchaseRecordID, buyerEmail, envName string }
	var orders []order
	for rows.Next() {
		var o order
		if err := rows.Scan(&o.id, &o.purchaseRecordID, &o.buyerEmail, &o.envName); err != nil {
			return 0, err
		}
		orders = append(orders, o)
	}
	fulfilled := 0
	now := fmtTime(time.Now())
	for _, o := range orders {
		key, err := s.AssignKey(ctx, skillID, o.buyerEmail, o.envName)
		if err != nil {
			break
		}
		_, _ = s.store.db.ExecContext(ctx, `UPDATE sm_pending_key_orders SET status = 'key_delivered', updated_at = ? WHERE id = ?`, now, o.id)
		_, _ = s.store.db.ExecContext(ctx, `UPDATE sm_purchase_records SET key_status = 'key_delivered', api_key_id = ? WHERE id = ?`, key.ID, o.purchaseRecordID)
		fulfilled++
	}
	if fulfilled > 0 {
		s.store.emitSync(ctx)
	}
	return fulfilled, nil
}

func (s *APIKeyPoolService) GetStockStatus(ctx context.Context, skillID string) string {
	var available, total int
	_ = s.store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_api_keys WHERE skill_id = ? AND status = 'available'`, skillID).Scan(&available)
	_ = s.store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_api_keys WHERE skill_id = ?`, skillID).Scan(&total)
	if available == 0 {
		return "缺货"
	}
	if total > 0 && (float64(available)/float64(total) >= 0.2) && available >= 5 {
		return "充足"
	}
	return "紧张"
}

func (s *APIKeyPoolService) GetPendingOrderCount(ctx context.Context, skillID string) int {
	var count int
	_ = s.store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_pending_key_orders WHERE skill_id = ? AND status = 'pending_key'`, skillID).Scan(&count)
	return count
}

func (s *APIKeyPoolService) encryptKey(plaintext string) (string, error) {
	key := sha256.Sum256(s.encSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *APIKeyPoolService) decryptKey(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256(s.encSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
