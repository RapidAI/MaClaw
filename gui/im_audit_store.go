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
	ID                  int64  `json:"id"`
	Timestamp           string `json:"timestamp"`
	UserID              string `json:"user_id"`
	Platform            string `json:"platform"`
	Role                string `json:"role"` // "user" | "assistant"
	Content             string `json:"content"`
	AttachmentPath      string `json:"attachment_path,omitempty"`
	AttachmentName      string `json:"attachment_name,omitempty"`
	AttachmentMediaType string `json:"attachment_media_type,omitempty"`
	AttachmentSize      int64  `json:"attachment_size,omitempty"`
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
	imAuditCriticalWait   = 5 * time.Second
)

// IMAuditStore manages SQLite-backed IM message audit storage.
type IMAuditStore struct {
	mu        sync.Mutex
	enqueueMu sync.RWMutex // prevents sends while Close closes writeCh
	db        *sql.DB
	writeCh   chan IMAuditMessage // async write channel
	done      chan struct{}       // signals writer goroutine exit
	closing   chan struct{}
	cleanupWG sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
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
		closing: make(chan struct{}),
	}

	// Background writer goroutine — drains writeCh and batches INSERTs.
	go store.writeLoop()

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
    content TEXT NOT NULL,
    attachment_path TEXT NOT NULL DEFAULT '',
    attachment_name TEXT NOT NULL DEFAULT '',
    attachment_media_type TEXT NOT NULL DEFAULT '',
    attachment_size INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_im_audit_ts ON im_audit_messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_im_audit_platform ON im_audit_messages(platform);
CREATE INDEX IF NOT EXISTS idx_im_audit_user ON im_audit_messages(user_id);
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	existing := make(map[string]bool)
	rows, err := db.Query(`PRAGMA table_info(im_audit_messages)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"attachment_path", "TEXT NOT NULL DEFAULT ''"},
		{"attachment_name", "TEXT NOT NULL DEFAULT ''"},
		{"attachment_media_type", "TEXT NOT NULL DEFAULT ''"},
		{"attachment_size", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if existing[column.name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE im_audit_messages ADD COLUMN " + column.name + " " + column.definition); err != nil {
			return err
		}
	}
	return nil
}

// Write enqueues an audit message for async persistence.
// Non-blocking: if the channel is full, the message is dropped (logged).
// Platform is normalized at write time (e.g. "weixin_local" → "weixin")
// so all read paths use simple equality matching with zero mapping logic.
func (s *IMAuditStore) Write(msg IMAuditMessage) {
	s.enqueue(msg, false)
}

// WriteCritical preserves attachment records under bursts by waiting for the
// bounded writer queue instead of silently dropping them.
func (s *IMAuditStore) WriteCritical(msg IMAuditMessage) bool {
	return s.enqueue(msg, true)
}

func (s *IMAuditStore) enqueue(msg IMAuditMessage, critical bool) bool {
	s.enqueueMu.RLock()
	defer s.enqueueMu.RUnlock()
	if msg.Timestamp == "" {
		msg.Timestamp = time.Now().Format(time.RFC3339)
	}
	msg.Content = truncateIMAuditContent(msg.Content)
	msg.Platform = normalizeIMAuditPlatform(msg.Platform)

	select {
	case <-s.closing:
		return false
	default:
	}

	if critical {
		timer := time.NewTimer(imAuditCriticalWait)
		defer timer.Stop()
		select {
		case <-s.closing:
			return false
		case s.writeCh <- msg:
			return true
		case <-timer.C:
			log.Printf("[im-audit] critical write queue timeout for user=%s", msg.UserID)
			return false
		}
	}
	select {
	case <-s.closing:
		return false
	case s.writeCh <- msg:
		return true
	default:
		log.Printf("[im-audit] write channel full, dropping message for user=%s", msg.UserID)
		return false
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
				`INSERT INTO im_audit_messages (timestamp, user_id, platform, role, content, attachment_path, attachment_name, attachment_media_type, attachment_size) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				batch[0].Timestamp, batch[0].UserID, batch[0].Platform, batch[0].Role, batch[0].Content,
				batch[0].AttachmentPath, batch[0].AttachmentName, batch[0].AttachmentMediaType, batch[0].AttachmentSize,
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
			stmt, err := tx.Prepare(`INSERT INTO im_audit_messages (timestamp, user_id, platform, role, content, attachment_path, attachment_name, attachment_media_type, attachment_size) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
			if err != nil {
				log.Printf("[im-audit] prepare error: %v", err)
				tx.Rollback()
				s.mu.Unlock()
				continue
			}
			for _, m := range batch {
				if _, err := stmt.Exec(m.Timestamp, m.UserID, m.Platform, m.Role, m.Content, m.AttachmentPath, m.AttachmentName, m.AttachmentMediaType, m.AttachmentSize); err != nil {
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
	querySQL := "SELECT id, timestamp, user_id, platform, role, content, attachment_path, attachment_name, attachment_media_type, attachment_size FROM im_audit_messages" + where +
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
		if err := rows.Scan(&m.ID, &m.Timestamp, &m.UserID, &m.Platform, &m.Role, &m.Content, &m.AttachmentPath, &m.AttachmentName, &m.AttachmentMediaType, &m.AttachmentSize); err != nil {
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
	deleted, _, err := s.DeleteBeforeWithAttachmentPaths(days)
	return deleted, err
}

// DeleteBeforeWithAttachmentPaths atomically selects attachment paths and
// deletes their rows under the same store lock, avoiding cleanup/query races.
func (s *IMAuditStore) DeleteBeforeWithAttachmentPaths(days int) (int64, []string, error) {
	if days <= 0 {
		return 0, nil, fmt.Errorf("im audit: retention days must be positive")
	}
	threshold := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, nil, fmt.Errorf("im audit: begin delete: %w", err)
	}
	rows, err := tx.Query(`SELECT attachment_path FROM im_audit_messages WHERE timestamp < ? AND attachment_path <> ''`, threshold)
	if err != nil {
		_ = tx.Rollback()
		return 0, nil, err
	}
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			_ = tx.Rollback()
			return 0, nil, err
		}
		paths = append(paths, path)
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		return 0, nil, err
	}
	result, err := tx.Exec(`DELETE FROM im_audit_messages WHERE timestamp < ?`, threshold)
	if err != nil {
		_ = tx.Rollback()
		return 0, nil, fmt.Errorf("im audit: delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	deleted, err := result.RowsAffected()
	return deleted, paths, err
}

func (s *IMAuditStore) AllAttachmentPaths() ([]string, error) {
	rows, err := s.db.Query(`SELECT attachment_path FROM im_audit_messages WHERE attachment_path <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

// ListUsers returns distinct user IDs for the given platform.
func (s *IMAuditStore) ListUsers(platform string) ([]string, error) {
	var rows *sql.Rows
	var err error
	platformKind := normalizeIMAuditPlatformKind(platform)
	if platformKind.IsThirdParty() {
		rows, err = s.db.Query(
			`SELECT DISTINCT user_id FROM im_audit_messages WHERE platform LIKE ? ORDER BY user_id`, imAuditPlatformThirdParty.String()+":%")
	} else if platformKind != imAuditPlatformUnknown {
		rows, err = s.db.Query(
			`SELECT DISTINCT user_id FROM im_audit_messages WHERE platform = ? ORDER BY user_id`, platformKind.String())
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
		switch normalizeIMAuditPlatformKind(platform) {
		case imAuditPlatformQQ:
			stats.QQ += count
		case imAuditPlatformTelegram:
			stats.Telegram += count
		case imAuditPlatformWeixin:
			stats.Weixin += count
		case imAuditPlatformLansenger:
			stats.Lansenger += count
		case imAuditPlatformThirdParty:
			stats.ThirdParty += count
		default:
		}
		stats.Total += count
	}
	return stats, rows.Err()
}

// ExportCSV exports matching messages to a CSV file and returns the file path.
func (s *IMAuditStore) ExportCSV(platform, userID, keyword, outputDir string) (string, error) {
	where, args := buildIMAuditWhere(platform, userID, keyword)
	querySQL := "SELECT timestamp, user_id, platform, role, content, attachment_name, attachment_media_type, attachment_size, attachment_path FROM im_audit_messages" + where +
		" ORDER BY timestamp ASC, id ASC"

	rows, err := s.db.Query(querySQL, args...)
	if err != nil {
		return "", fmt.Errorf("im audit: export query: %w", err)
	}
	defer rows.Close()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("im audit: create export dir: %w", err)
	}

	f, filePath, err := createIMAuditCSVFile(outputDir, platform, time.Now())
	if err != nil {
		return "", fmt.Errorf("im audit: create csv: %w", err)
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(filePath)
		}
	}()

	// Write UTF-8 BOM for Excel compatibility.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", fmt.Errorf("im audit: write csv BOM: %w", err)
	}

	w := csv.NewWriter(f)

	// Header
	if err := w.Write([]string{"时间", "用户ID", "平台", "角色", "内容", "附件名称", "附件类型", "附件大小(字节)", "本地路径"}); err != nil {
		return "", fmt.Errorf("im audit: write csv header: %w", err)
	}

	for rows.Next() {
		var ts, uid, plat, role, content, attachmentName, attachmentType, attachmentPath string
		var attachmentSize int64
		if err := rows.Scan(&ts, &uid, &plat, &role, &content, &attachmentName, &attachmentType, &attachmentSize, &attachmentPath); err != nil {
			return "", fmt.Errorf("im audit: scan csv row: %w", err)
		}
		if err := w.Write(safeIMAuditCSVRow([]string{ts, uid, plat, role, content, attachmentName, attachmentType, fmt.Sprint(attachmentSize), attachmentPath})); err != nil {
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
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("im audit: close csv: %w", err)
	}

	complete = true
	return filePath, nil
}

func createIMAuditCSVFile(outputDir, platform string, now time.Time) (*os.File, string, error) {
	label := safeIMAuditExportLabel(platform)
	if label == "" {
		label = "all"
	}
	base := fmt.Sprintf("im_audit_%s_%s", label, now.Format("20060102_150405.000"))
	for i := 0; i < 1000; i++ {
		name := base + ".csv"
		if i > 0 {
			name = fmt.Sprintf("%s_%d.csv", base, i)
		}
		path := filepath.Join(outputDir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if os.IsExist(err) {
			continue
		}
		return f, path, err
	}
	return nil, "", fmt.Errorf("too many export filename collisions")
}

func safeIMAuditExportLabel(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
	value = strings.Trim(value, "._-")
	if len(value) > 48 {
		value = value[:48]
	}
	return value
}

// safeIMAuditCSVCell prevents spreadsheet applications from interpreting
// untrusted chat text, user IDs, filenames, or paths as formulas on open.
func safeIMAuditCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func safeIMAuditCSVRow(row []string) []string {
	for i := range row {
		row[i] = safeIMAuditCSVCell(row[i])
	}
	return row
}

// Close drains pending writes and closes the database.
func (s *IMAuditStore) Close() error {
	s.closeOnce.Do(func() {
		s.enqueueMu.Lock()
		if s.closing != nil {
			close(s.closing)
		}
		if s.writeCh != nil {
			close(s.writeCh) // signal writeLoop to drain and exit
		}
		s.enqueueMu.Unlock()
		if s.done != nil {
			<-s.done
		}
		s.cleanupWG.Wait()
		if s.db != nil {
			s.closeErr = s.db.Close()
		}
	})
	return s.closeErr
}

// buildIMAuditWhere constructs the WHERE clause and args for query/export.
// Platform values are already normalized at write time, so simple equality works.
func buildIMAuditWhere(platform, userID, keyword string) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	platformKind := normalizeIMAuditPlatformKind(platform)
	if platformKind.IsThirdParty() {
		conditions = append(conditions, "platform LIKE ?")
		args = append(args, imAuditPlatformThirdParty.String()+":%")
	} else if platformKind != imAuditPlatformUnknown {
		conditions = append(conditions, "platform = ?")
		args = append(args, platformKind.String())
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
	if kind := normalizeIMAuditPlatformKind(platform); kind != imAuditPlatformUnknown {
		return kind.String()
	}
	if platform != "" {
		// Already canonical, or unknown — store as-is.
		return platform
	}
	return ""
}

// truncateIMAuditContent truncates content to imAuditMaxContentRune runes.
func truncateIMAuditContent(s string) string {
	runes := []rune(s)
	if len(runes) <= imAuditMaxContentRune {
		return s
	}
	return string(runes[:imAuditMaxContentRune]) + "\n...(已截断)"
}
