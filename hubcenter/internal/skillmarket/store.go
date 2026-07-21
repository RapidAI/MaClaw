package skillmarket

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Use sub-second precision so consecutive status changes and deletion
// tombstones retain their true order during HA snapshot conflict resolution.
// RFC3339Nano parsing remains backward-compatible with existing RFC3339 rows.
const timeFmt = time.RFC3339Nano

// Store 鏄?SkillMarket 鐨?SQLite 瀛樺偍灞傦紝瀹炵幇鎵€鏈?Repository 鎺ュ彛銆?
type Store struct {
	db          *sql.DB
	readDB      *sql.DB
	sync        SyncRecorder
	syncMu      sync.Mutex
	syncRunning bool
	syncPending bool
}

// NewStore 鍒涘缓 SkillMarket 瀛樺偍灞傚苟鎵ц杩佺Щ銆?
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
		`CREATE TABLE IF NOT EXISTS sm_problem_reports (
			id TEXT PRIMARY KEY, reporter_user_id TEXT NOT NULL, reporter_contact TEXT NOT NULL,
			os_version TEXT NOT NULL, description TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
			admin_note TEXT NOT NULL DEFAULT '', diagnostics_path TEXT NOT NULL DEFAULT '',
			screenshot_paths TEXT NOT NULL DEFAULT '[]', archived_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, origin_url TEXT NOT NULL DEFAULT '', gui_version TEXT NOT NULL DEFAULT ''
		);`,
		`ALTER TABLE sm_problem_reports ADD COLUMN origin_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sm_problem_reports ADD COLUMN gui_version TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_sm_problem_reports_user ON sm_problem_reports(reporter_user_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_problem_reports_status ON sm_problem_reports(status, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS sm_problem_report_tombstones (
			id TEXT PRIMARY KEY, deleted_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS sm_purchase_records (
			id                TEXT PRIMARY KEY,
			hub_id            TEXT NOT NULL DEFAULT '',
			tenant_id         TEXT NOT NULL DEFAULT '',
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
		`ALTER TABLE sm_purchase_records ADD COLUMN hub_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sm_purchase_records ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_sm_purchase_buyer_skill ON sm_purchase_records(buyer_id, skill_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_purchase_hub_tenant_buyer ON sm_purchase_records(hub_id, tenant_id, buyer_email);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_purchase_seller ON sm_purchase_records(seller_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_purchase_pending_key ON sm_purchase_records(key_status) WHERE key_status = 'pending_key';`,
		// 鈹€鈹€ Ratings 鈹€鈹€
		`CREATE TABLE IF NOT EXISTS sm_ratings (
			skill_id   TEXT NOT NULL,
			email      TEXT NOT NULL,
			score      INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (skill_id, email)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sm_ratings_skill ON sm_ratings(skill_id);`,
		// 鈹€鈹€ Admin Config 鈹€鈹€
		`CREATE TABLE IF NOT EXISTS sm_admin_config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);`,
		// 鈹€鈹€ Uploader Tiers 鈹€鈹€
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
			if isDuplicateColumnError(stmt, err) {
				continue
			}
			return fmt.Errorf("exec %q: %w", stmt[:min(len(stmt), 60)], err)
		}
	}
	// Auth tables (sessions, tokens, password_hash column)
	if err := s.migrateAuth(); err != nil {
		return fmt.Errorf("auth migrate: %w", err)
	}
	// Skill ID ownership tables (ownership binding + version history)
	if err := s.migrateSkillIDOwnership(); err != nil {
		return fmt.Errorf("skill_id_ownership migrate: %w", err)
	}
	return nil
}

// 鈹€鈹€ helpers 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

// DB 杩斿洖鍐欐暟鎹簱杩炴帴銆?
func (s *Store) DB() *sql.DB { return s.db }

// ReadDB 杩斿洖璇绘暟鎹簱杩炴帴銆?
func (s *Store) ReadDB() *sql.DB { return s.readDB }

func isDuplicateColumnError(stmt string, err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "ALTER TABLE") && strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

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

// generateID 鐢熸垚鍞竴 ID锛堟椂闂存埑 + 闅忔満鍚庣紑锛夈€?
func generateID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), hex.EncodeToString(buf[:]))
}
