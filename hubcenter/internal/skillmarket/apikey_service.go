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

// APIKey 代表一个 API Key 记录。
type APIKey struct {
	ID           string    `json:"id"`
	SkillID      string    `json:"skill_id"`
	EnvName      string    `json:"env_name"`
	EncryptedKey string    `json:"-"`
	Status       string    `json:"status"` // available, assigned, refunded
	BuyerEmail   string    `json:"buyer_email,omitempty"`
	AssignedAt   time.Time `json:"assigned_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// PendingKeyOrder 代表一个待分配 Key 的订单。
type PendingKeyOrder struct {
	ID               string    `json:"id"`
	PurchaseRecordID string    `json:"purchase_record_id"`
	SkillID          string    `json:"skill_id"`
	BuyerEmail       string    `json:"buyer_email"`
	EnvName          string    `json:"env_name"`
	Status           string    `json:"status"` // pending_key, key_delivered, cancelled
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// APIKeyPoolService 管理 API Key 池。
type APIKeyPoolService struct {
	store     *Store
	encSecret []byte // 用于 AES 加密 Key 的密钥
}

// NewAPIKeyPoolService 创建 APIKeyPoolService。
// encSecret 应为 RSA 私钥的 SHA256 哈希（32 字节）。
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

// UploadKeys 批量上传 API Key（加密存储）。
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
		_, err = tx.ExecContext(ctx, `INSERT INTO sm_api_keys (id, skill_id, env_name, encrypted_key, status, created_at) VALUES (?, ?, ?, ?, 'available', ?)`,
			id, skillID, envName, enc, now)
		if err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit()
}

// AssignKey 从 available 池中分配一个 Key 给买家。
func (s *APIKeyPoolService) AssignKey(ctx context.Context, skillID, buyerEmail, envName string) (*APIKey, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var k APIKey
	var encKey, assignedAt string
	err = tx.QueryRowContext(ctx, `SELECT id, skill_id, env_name, encrypted_key, created_at FROM sm_api_keys WHERE skill_id = ? AND status = 'available' AND env_name = ? LIMIT 1`,
		skillID, envName).Scan(&k.ID, &k.SkillID, &k.EnvName, &encKey, &assignedAt)
	if err != nil {
		return nil, fmt.Errorf("no available key for skill %s env %s", skillID, envName)
	}

	now := fmtTime(time.Now())
	_, err = tx.ExecContext(ctx, `UPDATE sm_api_keys SET status = 'assigned', buyer_email = ?, assigned_at = ? WHERE id = ?`,
		buyerEmail, now, k.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	k.Status = "assigned"
	k.BuyerEmail = buyerEmail
	k.AssignedAt = parseTime(now)
	k.EncryptedKey = encKey
	k.CreatedAt = parseTime(assignedAt)
	return &k, nil
}

// DecryptAssignedKey 解密已分配的 Key 明文。
func (s *APIKeyPoolService) DecryptAssignedKey(k *APIKey) (string, error) {
	return s.decryptKey(k.EncryptedKey)
}

// CreatePendingOrder 池耗尽时创建 pending_key 订单。
func (s *APIKeyPoolService) CreatePendingOrder(ctx context.Context, purchaseRecordID, skillID, buyerEmail, envName string) error {
	now := fmtTime(time.Now())
	id := generateID()
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO sm_pending_key_orders (id, purchase_record_id, skill_id, buyer_email, env_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'pending_key', ?, ?)`,
		id, purchaseRecordID, skillID, buyerEmail, envName, now, now)
	return err
}

// FulfillPendingOrders 补货后按购买时间顺序自动分配 Key。
func (s *APIKeyPoolService) FulfillPendingOrders(ctx context.Context, skillID string) (int, error) {
	rows, err := s.store.readDB.QueryContext(ctx, `SELECT id, purchase_record_id, buyer_email, env_name FROM sm_pending_key_orders WHERE skill_id = ? AND status = 'pending_key' ORDER BY created_at ASC`, skillID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type order struct {
		id, purchaseRecordID, buyerEmail, envName string
	}
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
			break // 没有更多可用 Key
		}
		// 更新 pending order 状态
		_, _ = s.store.db.ExecContext(ctx, `UPDATE sm_pending_key_orders SET status = 'key_delivered', updated_at = ? WHERE id = ?`, now, o.id)
		// 更新 purchase record 的 key_status
		_, _ = s.store.db.ExecContext(ctx, `UPDATE sm_purchase_records SET key_status = 'key_delivered', api_key_id = ? WHERE id = ?`, key.ID, o.purchaseRecordID)
		fulfilled++
	}
	return fulfilled, nil
}

// GetStockStatus 返回库存状态："充足"/"紧张"/"缺货"。
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

// GetPendingOrderCount 返回待分配订单数。
func (s *APIKeyPoolService) GetPendingOrderCount(ctx context.Context, skillID string) int {
	var count int
	_ = s.store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_pending_key_orders WHERE skill_id = ? AND status = 'pending_key'`, skillID).Scan(&count)
	return count
}

// ── encryption helpers ──────────────────────────────────────────────────

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
