package skillmarket

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const timeFmt = time.RFC3339

// Store 是 SkillMarket 的 SQLite 存储层，实现所有 Repository 接口。
type Store struct {
	db     *sql.DB
	readDB *sql.DB
}

// NewStore 创建 SkillMarket 存储层并执行迁移。
func NewStore(writeDB, readDB *sql.DB) (*Store, error) {
	s := &Store{db: writeDB, readDB: readDB}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("skillmarket migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sm_users (
			id                  TEXT PRIMARY KEY,
			email               TEXT NOT NULL UNIQUE,
			status              TEXT NOT NULL DEFAULT 'unverified',
			verify_method       TEXT NOT NULL DEFAULT '',
			credits             INTEGER NOT NULL DEFAULT 0,
			settled_credits     INTEGER NOT NULL DEFAULT 0,
			pending_settlement  INTEGER NOT NULL DEFAULT 0,
			debt                INTEGER NOT NULL DEFAULT 0,
			voucher_count       INTEGER NOT NULL DEFAULT 0,
			voucher_expires_at  TEXT NOT NULL DEFAULT '',
			created_at          TEXT NOT NULL,
			updated_at          TEXT NOT NULL,
			verified_at         TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS sm_credits_transactions (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			type        TEXT NOT NULL,
			amount      INTEGER NOT NULL,
			balance     INTEGER NOT NULL,
			skill_id    TEXT NOT NULL DEFAULT '',
			purchase_id TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_credits_tx_user ON sm_credits_transactions(user_id, created_at);`,
		`CREATE TABLE IF NOT EXISTS sm_submissions (
			id          TEXT PRIMARY KEY,
			email       TEXT NOT NULL,
			user_id     TEXT NOT NULL DEFAULT '',
			skill_id    TEXT NOT NULL DEFAULT '',
			fingerprint TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT 'pending',
			zip_path    TEXT NOT NULL,
			error_msg   TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_submissions_email ON sm_submissions(email, created_at);`,
		`CREATE TABLE IF NOT EXISTS sm_purchase_records (
			id                TEXT PRIMARY KEY,
			buyer_email       TEXT NOT NULL,
			buyer_id          TEXT NOT NULL,
			skill_id          TEXT NOT NULL,
			purchased_version INTEGER NOT NULL DEFAULT 1,
			purchase_type     TEXT NOT NULL DEFAULT 'purchase',
			amount_paid       INTEGER NOT NULL DEFAULT 0,
			platform_fee      INTEGER NOT NULL DEFAULT 0,
			seller_earning    INTEGER NOT NULL DEFAULT 0,
			seller_id         TEXT NOT NULL,
			key_status        TEXT NOT NULL DEFAULT '',
			api_key_id        TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'active',
			created_at        TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_purchase_buyer_skill ON sm_purchase_records(buyer_id, skill_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_purchase_seller ON sm_purchase_records(seller_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_purchase_pending_key ON sm_purchase_records(key_status) WHERE key_status = 'pending_key';`,
		// ── Ratings ──
		`CREATE TABLE IF NOT EXISTS sm_ratings (
			skill_id   TEXT NOT NULL,
			email      TEXT NOT NULL,
			score      INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (skill_id, email)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_ratings_skill ON sm_ratings(skill_id);`,
		// ── Admin Config ──
		`CREATE TABLE IF NOT EXISTS sm_admin_config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);`,
		// ── Uploader Tiers ──
		`CREATE TABLE IF NOT EXISTS sm_uploader_tiers (
			user_id          TEXT PRIMARY KEY,
			tier             INTEGER NOT NULL DEFAULT 1,
			published_count  INTEGER NOT NULL DEFAULT 0,
			avg_rating       REAL NOT NULL DEFAULT 0,
			total_downloads  INTEGER NOT NULL DEFAULT 0,
			updated_at       TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(len(stmt), 60)], err)
		}
	}
	// Auth tables (sessions, tokens, password_hash column)
	if err := s.migrateAuth(); err != nil {
		return fmt.Errorf("auth migrate: %w", err)
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────

// DB 返回写数据库连接。
func (s *Store) DB() *sql.DB { return s.db }

// ReadDB 返回读数据库连接。
func (s *Store) ReadDB() *sql.DB { return s.readDB }

func parseTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	t, _ := time.Parse(timeFmt, v)
	return t
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(timeFmt)
}

// generateID 生成唯一 ID（时间戳 + 随机后缀）。
func generateID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), hex.EncodeToString(buf[:]))
}
