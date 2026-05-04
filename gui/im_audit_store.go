package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// IMAuditMessage represents a single IM message record for audit.
type IMAuditMessage struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	UserID    string `json:"user_id"`
	Platform  string `json:"platform"`
	Role      string `json:"role"` // "user" | "assistant"
	Content   string `json:"content"`
}

// IMAuditQueryResult is the paginated query response.
type IMAuditQueryResult struct {
	Messages []IMAuditMessage `json:"messages"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// IMAuditStats holds per-platform message counts.
type IMAuditStats struct {
	QQ         int `json:"qq"`
	Telegram   int `json:"telegram"`
	Weixin     int `json:"weixin"`
	Lansenger  int `json:"lansenger"`
	ThirdParty int `json:"thirdparty"`
	Total      int `json:"total"`
}

const (
	imAuditPageSize       = 50
	imAuditMaxContentRune = 10000
	imAuditRetentionDays  = 30
)

// IMAuditStore manages SQLite-backed IM message audit storage.
type IMAuditStore struct {
	mu      sync.Mutex
	db      *sql.DB
	writeCh chan IMAuditMessage // async write channel
	done    chan struct{}       // signals writer goroutine exit
}

// NewIMAuditStore opens or creates the audit database at dbPath.
func NewIMAuditStore(dbPath string) (*IMAuditStore, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("im audit store: create dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("im audit store: open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("im audit store: set WAL: %w", err)
	}

	if err := createIMAuditSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("im audit store: create schema: %w", err)
	}

	store := &IMAuditStore{
		db:      db,
		writeCh: make(chan IMAuditMessage, 256),
		done:    make(chan struct{}),
	}

	// Background writer goroutine — drains writeCh and batches INSERTs.
	go store.writeLoop()

	// Auto-cleanup old records on startup.
	go store.autoCleanup()

	return store, nil
}

func createIMAuditSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS im_audit_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_id TEXT NOT NULL,
    platform TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_im_audit_ts ON im_audit_messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_im_audit_platform ON im_audit_messages(platform);
CREATE INDEX IF NOT EXISTS idx_im_audit_user ON im_audit_messages(user_id);
`
	_, err := db.Exec(schema)
	return err
}

// Write enqueues an audit message for async persistence.
// Non-blocking: if the channel is full, the message is dropped (logged).
// Platform is normalized at write time (e.g. "weixin_local" → "weixin")
// so all read paths use simple equality matching with zero mapping logic.
func (s *IMAuditStore) Write(msg IMAuditMessage) {
	if msg.Timestamp == "" {
		msg.Timestamp = time.Now().Format(time.RFC3339)
	}
	msg.Content = truncateIMAuditContent(msg.Content)
	msg.Platform = normalizeIMAuditPlatform(msg.Platform)

	select {
	case s.writeCh <- msg:
	default:
		log.Printf("[im-audit] write channel full, dropping message for user=%s", msg.UserID)
	}
}

// writeLoop is the background goroutine that drains writeCh and writes to SQLite.
// It batches pending messages into a single transaction for efficiency.
func (s *IMAuditStore) writeLoop() {
	defer close(s.done)

	batch := make([]IMAuditMessage, 0, 32)
	for msg := range s.writeCh {
		batch = append(batch[:0], msg)

		// Drain any additional pending messages without blocking.
	drain:
		for {
			select {
			case m, ok := <-s.writeCh:
				if !ok {
					break drain
				}
				batch = append(batch, m)
				if len(batch) >= 64 {
					break drain
				}
			default:
				break drain
			}
		}

		s.mu.Lock()
		if len(batch) == 1 {
			_, err := s.db.Exec(
				`INSERT INTO im_audit_messages (timestamp, user_id, platform, role, content) VALUES (?, ?, ?, ?, ?)`,
				batch[0].Timestamp, batch[0].UserID, batch[0].Platform, batch[0].Role, batch[0].Content,
			)
			if err != nil {
				log.Printf("[im-audit] write error: %v", err)
			}
		} else {
			tx, err := s.db.Begin()
			if err != nil {
				log.Printf("[im-audit] begin tx error: %v", err)
				s.mu.Unlock()
				continue
			}
			stmt, err := tx.Prepare(`INSERT INTO im_audit_messages (timestamp, user_id, platform, role, content) VALUES (?, ?, ?, ?, ?)`)
			if err != nil {
				log.Printf("[im-audit] prepare error: %v", err)
				tx.Rollback()
				s.mu.Unlock()
				continue
			}
			for _, m := range batch {
				if _, err := stmt.Exec(m.Timestamp, m.UserID, m.Platform, m.Role, m.Content); err != nil {
					log.Printf("[im-audit] batch insert error: %v", err)
				}
			}
			stmt.Close()
			if err := tx.Commit(); err != nil {
				log.Printf("[im-audit] commit error: %v", err)
			}
		}
		s.mu.Unlock()
	}
}

// Query returns paginated messages matching the filters.
func (s *IMAuditStore) Query(platform, userID, keyword string, page int) (*IMAuditQueryResult, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * imAuditPageSize

	where, args := buildIMAuditWhere(platform, userID, keyword)

	// Count total.
	var total int
	countSQL := "SELECT COUNT(*) FROM im_audit_messages" + where
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("im audit: count: %w", err)
	}

	// Fetch page — copy args to avoid mutating the original slice.
	querySQL := "SELECT id, timestamp, user_id, platform, role, content FROM im_audit_messages" + where +
		" ORDER BY timestamp ASC, id ASC LIMIT ? OFFSET ?"
	queryArgs := make([]interface{}, len(args), len(args)+2)
	copy(queryArgs, args)
	queryArgs = append(queryArgs, imAuditPageSize, offset)

	rows, err := s.db.Query(querySQL, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("im audit: query: %w", err)
	}
	defer rows.Close()

	var messages []IMAuditMessage
	for rows.Next() {
		var m IMAuditMessage
		if err := rows.Scan(&m.ID, &m.Timestamp, &m.UserID, &m.Platform, &m.Role, &m.Content); err != nil {
			return nil, fmt.Errorf("im audit: scan: %w", err)
		}
		messages = append(messages, m)
	}
	if messages == nil {
		messages = []IMAuditMessage{}
	}

	return &IMAuditQueryResult{
		Messages: messages,
		Total:    total,
		Page:     page,
		PageSize: imAuditPageSize,
	}, rows.Err()
}

// DeleteBefore removes messages older than the given number of days.
func (s *IMAuditStore) DeleteBefore(days int) (int64, error) {
	threshold := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM im_audit_messages WHERE timestamp < ?`, threshold)
	if err != nil {
		return 0, fmt.Errorf("im audit: delete: %w", err)
	}
	return result.RowsAffected()
}

// ListUsers returns distinct user IDs for the given platform.
func (s *IMAuditStore) ListUsers(platform string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if platform == "thirdparty" {
		rows, err = s.db.Query(
			`SELECT DISTINCT user_id FROM im_audit_messages WHERE platform LIKE ? ORDER BY user_id`, "thirdparty:%")
	} else if platform != "" {
		rows, err = s.db.Query(
			`SELECT DISTINCT user_id FROM im_audit_messages WHERE platform = ? ORDER BY user_id`, platform)
	} else {
		rows, err = s.db.Query(
			`SELECT DISTINCT user_id FROM im_audit_messages ORDER BY user_id`)
	}
	if err != nil {
		return nil, fmt.Errorf("im audit: list users: %w", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("im audit: scan user: %w", err)
		}
		users = append(users, u)
	}
	if users == nil {
		users = []string{}
	}
	return users, rows.Err()
}

// Stats returns per-platform message counts.
func (s *IMAuditStore) Stats() (*IMAuditStats, error) {
	rows, err := s.db.Query(
		`SELECT platform, COUNT(*) FROM im_audit_messages GROUP BY platform`)
	if err != nil {
		return nil, fmt.Errorf("im audit: stats: %w", err)
	}
	defer rows.Close()

	stats := &IMAuditStats{}
	for rows.Next() {
		var platform string
		var count int
		if err := rows.Scan(&platform, &count); err != nil {
			return nil, fmt.Errorf("im audit: scan stats: %w", err)
		}
		switch platform {
		case "qq":
			stats.QQ += count
		case "telegram":
			stats.Telegram += count
		case "weixin":
			stats.Weixin += count
		case "lansenger":
			stats.Lansenger += count
		default:
			if strings.HasPrefix(platform, "thirdparty:") {
				stats.ThirdParty += count
			}
		}
		stats.Total += count
	}
	return stats, rows.Err()
}

// ExportCSV exports matching messages to a CSV file and returns the file path.
func (s *IMAuditStore) ExportCSV(platform, userID, keyword, outputDir string) (string, error) {
	where, args := buildIMAuditWhere(platform, userID, keyword)
	querySQL := "SELECT timestamp, user_id, platform, role, content FROM im_audit_messages" + where +
		" ORDER BY timestamp ASC, id ASC"

	rows, err := s.db.Query(querySQL, args...)
	if err != nil {
		return "", fmt.Errorf("im audit: export query: %w", err)
	}
	defer rows.Close()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("im audit: create export dir: %w", err)
	}

	fileName := fmt.Sprintf("im_audit_%s_%s.csv", platform, time.Now().Format("20060102_150405"))
	if platform == "" {
		fileName = fmt.Sprintf("im_audit_all_%s.csv", time.Now().Format("20060102_150405"))
	}
	filePath := filepath.Join(outputDir, fileName)

	f, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("im audit: create csv: %w", err)
	}
	defer f.Close()

	// Write UTF-8 BOM for Excel compatibility.
	f.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(f)

	// Header
	if err := w.Write([]string{"时间", "用户ID", "平台", "角色", "内容"}); err != nil {
		return "", fmt.Errorf("im audit: write csv header: %w", err)
	}

	for rows.Next() {
		var ts, uid, plat, role, content string
		if err := rows.Scan(&ts, &uid, &plat, &role, &content); err != nil {
			return "", fmt.Errorf("im audit: scan csv row: %w", err)
		}
		if err := w.Write([]string{ts, uid, plat, role, content}); err != nil {
			return "", fmt.Errorf("im audit: write csv row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("im audit: csv rows: %w", err)
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("im audit: csv flush: %w", err)
	}

	return filePath, nil
}

// Close drains pending writes and closes the database.
func (s *IMAuditStore) Close() error {
	if s.writeCh != nil {
		close(s.writeCh) // signal writeLoop to exit
		<-s.done         // wait for it to drain
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// autoCleanup removes records older than imAuditRetentionDays.
// Runs with a short delay to avoid competing with writeLoop at startup.
func (s *IMAuditStore) autoCleanup() {
	time.Sleep(2 * time.Second)
	n, err := s.DeleteBefore(imAuditRetentionDays)
	if err != nil {
		log.Printf("[im-audit] auto cleanup error: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[im-audit] auto cleanup: removed %d records older than %d days", n, imAuditRetentionDays)
	}
}

// buildIMAuditWhere constructs the WHERE clause and args for query/export.
// Platform values are already normalized at write time, so simple equality works.
func buildIMAuditWhere(platform, userID, keyword string) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if platform == "thirdparty" {
		conditions = append(conditions, "platform LIKE ?")
		args = append(args, "thirdparty:%")
	} else if platform != "" {
		conditions = append(conditions, "platform = ?")
		args = append(args, platform)
	}
	if userID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, userID)
	}
	if keyword != "" {
		conditions = append(conditions, "content LIKE ?")
		args = append(args, "%"+keyword+"%")
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// normalizeIMAuditPlatform maps gateway-specific platform strings to canonical
// names used in the audit database. This is the SINGLE normalization point —
// all read paths (Query, ListUsers, Stats, ExportCSV) use simple equality
// matching against these canonical names.
//
// Adding a new IM gateway only requires adding one line here.
func normalizeIMAuditPlatform(platform string) string {
	switch platform {
	case "qqbot_local", "qqbot":
		return "qq"
	case "telegram_local":
		return "telegram"
	case "weixin_local":
		return "weixin"
	case "lansenger_local":
		return "lansenger"
	default:
		// Already canonical, or unknown — store as-is.
		return platform
	}
}

// truncateIMAuditContent truncates content to imAuditMaxContentRune runes.
func truncateIMAuditContent(s string) string {
	runes := []rune(s)
	if len(runes) <= imAuditMaxContentRune {
		return s
	}
	return string(runes[:imAuditMaxContentRune]) + "\n...(已截断)"
}
