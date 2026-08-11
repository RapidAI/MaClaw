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
	BotProfileID        string `json:"bot_profile_id,omitempty"`
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
	stateMu   sync.Mutex
	stateCond *sync.Cond
	accepted  uint64 // messages accepted by the queue
	processed uint64 // accepted messages attempted by the writer
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
	// A single connection keeps connection-scoped PRAGMAs deterministic and
	// matches the store's serialized write/maintenance model. Audit operations
	// are short; serial access also prevents connection-specific lock surprises.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("im audit store: set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("im audit store: set busy timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("im audit store: set synchronous mode: %w", err)
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
	store.stateCond = sync.NewCond(&store.stateMu)

	// Background writer goroutine — drains writeCh and batches INSERTs.
	go store.writeLoop()

	return store, nil
}

func createIMAuditSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS im_audit_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    bot_profile_id TEXT NOT NULL DEFAULT '',
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
CREATE INDEX IF NOT EXISTS idx_im_audit_platform_user ON im_audit_messages(platform, user_id);
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
		{"bot_profile_id", "TEXT NOT NULL DEFAULT ''"},
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
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_im_audit_bot_user_ts ON im_audit_messages(bot_profile_id, user_id, timestamp, id)"); err != nil {
		return err
	}
	return nil
}

// Write enqueues an audit message for async persistence.
// Non-blocking: if the channel is full, the message is dropped (logged).
// Platform is normalized at write time (e.g. "weixin_local" → "weixin").
// Third-party client suffixes are retained so records remain attributable to
// a particular gateway/device.
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
			s.markAccepted()
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
		s.markAccepted()
		return true
	default:
		log.Printf("[im-audit] write channel full, dropping message for user=%s", msg.UserID)
		return false
	}
}

func (s *IMAuditStore) markAccepted() {
	s.stateMu.Lock()
	s.accepted++
	s.stateMu.Unlock()
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

		s.processBatch(batch)
	}
}

func (s *IMAuditStore) processBatch(batch []IMAuditMessage) {
	// Every dequeued message must cross the flush barrier, even when SQLite
	// rejects the batch. Otherwise all later history reads can wait forever.
	defer s.markProcessed(len(batch))
	if err := s.persistBatch(batch); err != nil {
		log.Printf("[im-audit] persist batch (%d messages): %v", len(batch), err)
	}
}

func (s *IMAuditStore) markProcessed(count int) {
	s.stateMu.Lock()
	s.processed += uint64(count)
	s.stateCond.Broadcast()
	s.stateMu.Unlock()
}

func (s *IMAuditStore) persistBatch(batch []IMAuditMessage) error {
	if len(batch) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const insertSQL = `INSERT INTO im_audit_messages (timestamp, bot_profile_id, user_id, platform, role, content, attachment_path, attachment_name, attachment_media_type, attachment_size) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if len(batch) == 1 {
		m := batch[0]
		_, err := s.db.Exec(insertSQL, m.Timestamp, m.BotProfileID, m.UserID, m.Platform, m.Role, m.Content, m.AttachmentPath, m.AttachmentName, m.AttachmentMediaType, m.AttachmentSize)
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	for _, m := range batch {
		if _, err := stmt.Exec(m.Timestamp, m.BotProfileID, m.UserID, m.Platform, m.Role, m.Content, m.AttachmentPath, m.AttachmentName, m.AttachmentMediaType, m.AttachmentSize); err != nil {
			_ = stmt.Close()
			return fmt.Errorf("insert message: %w", err)
		}
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("close insert statement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// flush waits until all messages accepted before this call have been written.
// Query-like operations use the barrier so opening/refreshing chat history
// immediately after a reply cannot temporarily omit the latest exchange.
func (s *IMAuditStore) flush() {
	if s == nil {
		return
	}
	// Exclude enqueues just long enough to capture a stable target. The writer
	// continues draining concurrently, so this does not serialize DB I/O.
	s.enqueueMu.Lock()
	s.stateMu.Lock()
	target := s.accepted
	s.enqueueMu.Unlock()
	for s.processed < target {
		s.stateCond.Wait()
	}
	s.stateMu.Unlock()
}

// Query returns paginated messages matching the filters.
func (s *IMAuditStore) Query(platform, userID, keyword string, page int) (*IMAuditQueryResult, error) {
	return s.QueryForBot(platform, "", userID, keyword, page)
}

// QueryForBot returns paginated history restricted to a Lansenger bot profile.
// Empty botProfileID preserves the existing cross-bot API behavior.
func (s *IMAuditStore) QueryForBot(platform, botProfileID, userID, keyword string, page int) (*IMAuditQueryResult, error) {
	s.flush()
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * imAuditPageSize

	where, args := buildIMAuditWhereForBot(platform, botProfileID, userID, keyword)

	// Count and fetch from one snapshot so concurrent audit writes cannot make
	// the reported total disagree with the returned page.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("im audit: begin query: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	countSQL := "SELECT COUNT(*) FROM im_audit_messages" + where
	if err := tx.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("im audit: count: %w", err)
	}

	// Fetch page — copy args to avoid mutating the original slice.
	querySQL := "SELECT id, timestamp, bot_profile_id, user_id, platform, role, content, attachment_path, attachment_name, attachment_media_type, attachment_size FROM im_audit_messages" + where +
		" ORDER BY timestamp ASC, id ASC LIMIT ? OFFSET ?"
	queryArgs := make([]interface{}, len(args), len(args)+2)
	copy(queryArgs, args)
	queryArgs = append(queryArgs, imAuditPageSize, offset)

	rows, err := tx.Query(querySQL, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("im audit: query: %w", err)
	}
	var messages []IMAuditMessage
	for rows.Next() {
		var m IMAuditMessage
		if err := rows.Scan(&m.ID, &m.Timestamp, &m.BotProfileID, &m.UserID, &m.Platform, &m.Role, &m.Content, &m.AttachmentPath, &m.AttachmentName, &m.AttachmentMediaType, &m.AttachmentSize); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("im audit: scan: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("im audit: rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("im audit: close rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("im audit: commit query: %w", err)
	}
	if messages == nil {
		messages = []IMAuditMessage{}
	}

	return &IMAuditQueryResult{
		Messages: messages,
		Total:    total,
		Page:     page,
		PageSize: imAuditPageSize,
	}, nil
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
	s.flush()
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
	s.flush()
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
	return s.ListUsersForBot(platform, "")
}

// ListUsersForBot returns distinct user IDs restricted to one bot profile.
// An empty botProfileID keeps the existing cross-bot behavior.
func (s *IMAuditStore) ListUsersForBot(platform, botProfileID string) ([]string, error) {
	s.flush()
	if strings.TrimSpace(botProfileID) != "" {
		where, args := buildIMAuditWhereForBot(platform, botProfileID, "", "")
		rows, err := s.db.Query(`SELECT DISTINCT user_id FROM im_audit_messages`+where+` ORDER BY user_id`, args...)
		if err != nil {
			return nil, fmt.Errorf("im audit: list bot users: %w", err)
		}
		defer rows.Close()
		return scanIMAuditUsers(rows)
	}
	var rows *sql.Rows
	var err error
	platformKind := normalizeIMAuditPlatformKind(platform)
	if platformKind.IsThirdParty() {
		rows, err = s.db.Query(
			`SELECT DISTINCT user_id FROM im_audit_messages WHERE platform = ? OR platform GLOB ? ORDER BY user_id`,
			imAuditPlatformThirdParty.String(), imAuditPlatformThirdParty.String()+":*")
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

	return scanIMAuditUsers(rows)
}

func scanIMAuditUsers(rows *sql.Rows) ([]string, error) {
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
	s.flush()
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
	return s.ExportCSVForBot(platform, "", userID, keyword, outputDir)
}

// ExportCSVForBot exports only records belonging to botProfileID. Empty keeps
// backwards-compatible all-bot export semantics.
func (s *IMAuditStore) ExportCSVForBot(platform, botProfileID, userID, keyword, outputDir string) (string, error) {
	s.flush()
	where, args := buildIMAuditWhereForBot(platform, botProfileID, userID, keyword)
	querySQL := "SELECT timestamp, bot_profile_id, user_id, platform, role, content, attachment_name, attachment_media_type, attachment_size, attachment_path FROM im_audit_messages" + where +
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
	if err := w.Write([]string{"时间", "机器人ID", "用户ID", "平台", "角色", "内容", "附件名称", "附件类型", "附件大小(字节)", "本地路径"}); err != nil {
		return "", fmt.Errorf("im audit: write csv header: %w", err)
	}

	for rows.Next() {
		var ts, botID, uid, plat, role, content, attachmentName, attachmentType, attachmentPath string
		var attachmentSize int64
		if err := rows.Scan(&ts, &botID, &uid, &plat, &role, &content, &attachmentName, &attachmentType, &attachmentSize, &attachmentPath); err != nil {
			return "", fmt.Errorf("im audit: scan csv row: %w", err)
		}
		if err := w.Write(safeIMAuditCSVRow([]string{ts, botID, uid, plat, role, content, attachmentName, attachmentType, fmt.Sprint(attachmentSize), attachmentPath})); err != nil {
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
// Platform values are normalized at write time. Third-party records may be the
// bare Hub platform or a client-specific local gateway value.
func buildIMAuditWhere(platform, userID, keyword string) (string, []interface{}) {
	return buildIMAuditWhereForBot(platform, "", userID, keyword)
}

func buildIMAuditWhereForBot(platform, botProfileID, userID, keyword string) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if botProfileID = strings.TrimSpace(botProfileID); botProfileID != "" {
		conditions = append(conditions, "bot_profile_id = ?")
		args = append(args, botProfileID)
	}
	platformKind := normalizeIMAuditPlatformKind(platform)
	if platformKind.IsThirdParty() {
		// GLOB's literal prefix is indexable by SQLite. LIKE is case-insensitive
		// by default and caused a full table scan despite idx_im_audit_platform.
		// Stored third-party platforms are already normalized to lower case.
		conditions = append(conditions, "(platform = ? OR platform GLOB ?)")
		args = append(args, imAuditPlatformThirdParty.String(), imAuditPlatformThirdParty.String()+":*")
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
// names used in the audit database. This is the SINGLE write normalization
// point. Third-party suffixes are significant provenance and must not be
// collapsed into the family name.
//
// Adding a new IM gateway only requires adding one line here.
func normalizeIMAuditPlatform(platform string) string {
	trimmed := strings.ToLower(strings.TrimSpace(platform))
	if kind := normalizeIMAuditPlatformKind(trimmed); kind.IsThirdParty() {
		return trimmed
	} else if kind != imAuditPlatformUnknown {
		return kind.String()
	}
	if strings.TrimSpace(platform) != "" {
		// Already canonical, or unknown — store as-is.
		return strings.TrimSpace(platform)
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
