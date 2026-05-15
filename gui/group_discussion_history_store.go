package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	_ "modernc.org/sqlite"
)

type GroupDiscussionAttachmentRecord struct {
	AttachmentID  string
	DiscussionID  string
	MessageID     string
	Kind          string
	Filename      string
	MimeType      string
	HubURL        string
	LocalPath     string
	SizeBytes     int64
	Checksum      string
	DownloadState string
}

type GroupDiscussionHistoryStore struct {
	mu sync.Mutex
	db *sql.DB
}

func NewGroupDiscussionHistoryStore(dbPath string) (*GroupDiscussionHistoryStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("group discussion history store: db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("group discussion history store: create dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("group discussion history store: open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("group discussion history store: set WAL: %w", err)
	}
	if err := createGroupDiscussionHistorySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &GroupDiscussionHistoryStore{db: db}, nil
}

func (s *GroupDiscussionHistoryStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func createGroupDiscussionHistorySchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS group_discussion_summaries (
    discussion_id TEXT PRIMARY KEY,
    local_relation TEXT NOT NULL DEFAULT '',
    readonly INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT '',
    topic TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL DEFAULT '',
    result_summary TEXT NOT NULL DEFAULT '',
    participant_ids_json TEXT NOT NULL DEFAULT '[]',
    message_count INTEGER NOT NULL DEFAULT 0,
    answer_count INTEGER NOT NULL DEFAULT 0,
    expected_answer_count INTEGER NOT NULL DEFAULT 0,
    ready_to_summarize INTEGER NOT NULL DEFAULT 0,
    readiness_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    hub_updated_at TEXT NOT NULL DEFAULT '',
    last_synced_at TEXT NOT NULL DEFAULT '',
    local_visibility TEXT NOT NULL DEFAULT 'visible',
    hidden_at TEXT NOT NULL DEFAULT '',
    sync_state TEXT NOT NULL DEFAULT 'synced',
    last_error TEXT NOT NULL DEFAULT '',
    attachment_local_root TEXT NOT NULL DEFAULT '',
    summary_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS group_discussion_details (
    discussion_id TEXT PRIMARY KEY,
    detail_json TEXT NOT NULL,
    last_synced_at TEXT NOT NULL DEFAULT '',
    sync_state TEXT NOT NULL DEFAULT 'synced',
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS group_discussion_attachments (
    discussion_id TEXT NOT NULL,
    attachment_id TEXT NOT NULL,
    message_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    hub_url TEXT NOT NULL DEFAULT '',
    local_path TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    checksum TEXT NOT NULL DEFAULT '',
    download_state TEXT NOT NULL DEFAULT 'remote',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (discussion_id, attachment_id)
);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("group discussion history store: create schema: %w", err)
	}
	if err := migrateGroupDiscussionHistoryColumns(db); err != nil {
		return err
	}
	if err := migrateGroupDiscussionAttachmentSchema(db); err != nil {
		return err
	}
	if err := createGroupDiscussionHistoryIndexes(db); err != nil {
		return err
	}
	return nil
}

func createGroupDiscussionHistoryIndexes(db *sql.DB) error {
	indexes := `
CREATE INDEX IF NOT EXISTS idx_group_discussion_summaries_visibility ON group_discussion_summaries(local_visibility);
CREATE INDEX IF NOT EXISTS idx_group_discussion_summaries_updated ON group_discussion_summaries(updated_at);
CREATE INDEX IF NOT EXISTS idx_group_discussion_attachments_discussion ON group_discussion_attachments(discussion_id);
`
	if _, err := db.Exec(indexes); err != nil {
		return fmt.Errorf("group discussion history store: create indexes: %w", err)
	}
	return nil
}

func migrateGroupDiscussionHistoryColumns(db *sql.DB) error {
	summaryColumns := map[string]string{
		"local_relation":        "TEXT NOT NULL DEFAULT ''",
		"readonly":              "INTEGER NOT NULL DEFAULT 1",
		"status":                "TEXT NOT NULL DEFAULT ''",
		"topic":                 "TEXT NOT NULL DEFAULT ''",
		"question":              "TEXT NOT NULL DEFAULT ''",
		"result_summary":        "TEXT NOT NULL DEFAULT ''",
		"participant_ids_json":  "TEXT NOT NULL DEFAULT '[]'",
		"message_count":         "INTEGER NOT NULL DEFAULT 0",
		"answer_count":          "INTEGER NOT NULL DEFAULT 0",
		"expected_answer_count": "INTEGER NOT NULL DEFAULT 0",
		"ready_to_summarize":    "INTEGER NOT NULL DEFAULT 0",
		"readiness_reason":      "TEXT NOT NULL DEFAULT ''",
		"created_at":            "TEXT NOT NULL DEFAULT ''",
		"updated_at":            "TEXT NOT NULL DEFAULT ''",
		"hub_updated_at":        "TEXT NOT NULL DEFAULT ''",
		"last_synced_at":        "TEXT NOT NULL DEFAULT ''",
		"local_visibility":      "TEXT NOT NULL DEFAULT 'visible'",
		"hidden_at":             "TEXT NOT NULL DEFAULT ''",
		"sync_state":            "TEXT NOT NULL DEFAULT 'synced'",
		"last_error":            "TEXT NOT NULL DEFAULT ''",
		"attachment_local_root": "TEXT NOT NULL DEFAULT ''",
		"summary_json":          "TEXT NOT NULL DEFAULT '{}'",
	}
	if err := ensureSQLiteColumns(db, "group_discussion_summaries", summaryColumns); err != nil {
		return err
	}
	detailColumns := map[string]string{
		"detail_json":    "TEXT NOT NULL DEFAULT '{}'",
		"last_synced_at": "TEXT NOT NULL DEFAULT ''",
		"sync_state":     "TEXT NOT NULL DEFAULT 'synced'",
		"last_error":     "TEXT NOT NULL DEFAULT ''",
	}
	if err := ensureSQLiteColumns(db, "group_discussion_details", detailColumns); err != nil {
		return err
	}
	return nil
}

func ensureSQLiteColumns(db *sql.DB, table string, columns map[string]string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("group discussion history store: inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	existing := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("group discussion history store: scan %s schema: %w", table, err)
		}
		existing[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("group discussion history store: inspect %s schema: %w", table, err)
	}
	for name, definition := range columns {
		if _, ok := existing[name]; ok {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + definition); err != nil {
			return fmt.Errorf("group discussion history store: add %s.%s: %w", table, name, err)
		}
	}
	return nil
}

func migrateGroupDiscussionAttachmentSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(group_discussion_attachments)`)
	if err != nil {
		return fmt.Errorf("group discussion history store: inspect attachment schema: %w", err)
	}
	defer rows.Close()
	attachmentPKOnly := false
	discussionPK := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("group discussion history store: scan attachment schema: %w", err)
		}
		if name == "attachment_id" && pk == 1 {
			attachmentPKOnly = true
		}
		if name == "discussion_id" && pk > 0 {
			discussionPK = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("group discussion history store: inspect attachment schema: %w", err)
	}
	if !attachmentPKOnly || discussionPK {
		return nil
	}
	migration := `
BEGIN;
ALTER TABLE group_discussion_attachments RENAME TO group_discussion_attachments_old;
CREATE TABLE group_discussion_attachments (
    discussion_id TEXT NOT NULL,
    attachment_id TEXT NOT NULL,
    message_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    hub_url TEXT NOT NULL DEFAULT '',
    local_path TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    checksum TEXT NOT NULL DEFAULT '',
    download_state TEXT NOT NULL DEFAULT 'remote',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (discussion_id, attachment_id)
);
INSERT OR REPLACE INTO group_discussion_attachments (discussion_id, attachment_id, message_id, kind, filename, mime_type, hub_url, local_path, size_bytes, checksum, download_state, created_at, updated_at)
SELECT discussion_id, attachment_id, message_id, kind, filename, mime_type, hub_url, local_path, size_bytes, checksum, download_state, created_at, updated_at
FROM group_discussion_attachments_old;
DROP TABLE group_discussion_attachments_old;
CREATE INDEX IF NOT EXISTS idx_group_discussion_attachments_discussion ON group_discussion_attachments(discussion_id);
COMMIT;`
	if _, err := db.Exec(migration); err != nil {
		_, _ = db.Exec(`ROLLBACK`)
		return fmt.Errorf("group discussion history store: migrate attachment schema: %w", err)
	}
	return nil
}

func (s *GroupDiscussionHistoryStore) CacheSummaries(ctx context.Context, summaries []a2a.HubDiscussionSummary, attachmentRoot func(string) string) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO group_discussion_summaries (discussion_id, local_relation, readonly, status, topic, question, result_summary, participant_ids_json, message_count, answer_count, expected_answer_count, ready_to_summarize, readiness_reason, created_at, updated_at, hub_updated_at, last_synced_at, sync_state, last_error, attachment_local_root, summary_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'synced', '', ?, ?)
ON CONFLICT(discussion_id) DO UPDATE SET
    local_relation=excluded.local_relation, readonly=excluded.readonly, status=excluded.status, topic=excluded.topic, question=excluded.question, result_summary=excluded.result_summary, participant_ids_json=excluded.participant_ids_json, message_count=excluded.message_count, answer_count=excluded.answer_count, expected_answer_count=excluded.expected_answer_count, ready_to_summarize=excluded.ready_to_summarize, readiness_reason=excluded.readiness_reason, created_at=excluded.created_at, updated_at=excluded.updated_at, hub_updated_at=excluded.hub_updated_at, last_synced_at=excluded.last_synced_at, sync_state='synced', last_error='', attachment_local_root=excluded.attachment_local_root, summary_json=excluded.summary_json`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, summary := range summaries {
		summary = normalizeHistorySummaryForCache(summary)
		id := strings.TrimSpace(summary.ID)
		if id == "" {
			continue
		}
		participantsJSON, _ := json.Marshal(summary.ParticipantIDs)
		summaryJSON, _ := json.Marshal(summary)
		root := ""
		if attachmentRoot != nil {
			root = attachmentRoot(id)
		}
		if _, err := stmt.ExecContext(ctx, id, summary.LocalRelation, boolToInt(summary.Readonly), summary.Status, summary.Topic, summary.Question, summary.ResultSummary, string(participantsJSON), summary.MessageCount, summary.AnswerCount, summary.ExpectedAnswerCount, boolToInt(summary.ReadyToSummarize), summary.ReadinessReason, formatOptionalTime(summary.CreatedAt), formatOptionalTime(summary.UpdatedAt), formatOptionalTime(summary.UpdatedAt), now, root, string(summaryJSON)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *GroupDiscussionHistoryStore) CachedSummaries(ctx context.Context, includeHidden bool) ([]a2a.HubDiscussionSummary, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	where := ""
	if !includeHidden {
		where = " WHERE local_visibility <> 'hidden'"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT summary_json FROM group_discussion_summaries`+where+` ORDER BY updated_at DESC, last_synced_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []a2a.HubDiscussionSummary{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var summary a2a.HubDiscussionSummary
		if err := json.Unmarshal([]byte(raw), &summary); err == nil && strings.TrimSpace(summary.ID) != "" {
			out = append(out, normalizeHistorySummaryForCache(summary))
		}
	}
	return out, rows.Err()
}

func (s *GroupDiscussionHistoryStore) HiddenSummaries(ctx context.Context) ([]a2a.HubDiscussionSummary, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT summary_json FROM group_discussion_summaries WHERE local_visibility = 'hidden' ORDER BY updated_at DESC, last_synced_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []a2a.HubDiscussionSummary{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var summary a2a.HubDiscussionSummary
		if err := json.Unmarshal([]byte(raw), &summary); err == nil && strings.TrimSpace(summary.ID) != "" {
			out = append(out, normalizeHistorySummaryForCache(summary))
		}
	}
	return out, rows.Err()
}

func (s *GroupDiscussionHistoryStore) VisibleSummaries(ctx context.Context, summaries []a2a.HubDiscussionSummary) ([]a2a.HubDiscussionSummary, error) {
	if s == nil || s.db == nil || len(summaries) == 0 {
		return summaries, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT discussion_id FROM group_discussion_summaries WHERE local_visibility = 'hidden'`)
	if err != nil {
		return summaries, nil
	}
	defer rows.Close()
	hidden := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			hidden[id] = struct{}{}
		}
	}
	out := summaries[:0]
	for _, summary := range summaries {
		if _, ok := hidden[summary.ID]; ok {
			continue
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *GroupDiscussionHistoryStore) CacheDetail(ctx context.Context, detail a2a.HubDiscussionDetail, attachmentRoot func(string) string) error {
	if s == nil || s.db == nil {
		return nil
	}
	id := strings.TrimSpace(detail.Discussion.ID)
	if id == "" && detail.Session != nil {
		id = strings.TrimSpace(detail.Session.ID)
	}
	if id == "" {
		return nil
	}
	detail.Discussion = s.mergeCachedSummaryRelation(ctx, detail.Discussion)
	detail = s.materializeInlineTextAttachments(ctx, id, detail, attachmentRoot)
	if err := s.CacheSummaries(ctx, []a2a.HubDiscussionSummary{detail.Discussion}, attachmentRoot); err != nil {
		return err
	}
	raw, _ := json.Marshal(detail)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `INSERT INTO group_discussion_details (discussion_id, detail_json, last_synced_at, sync_state, last_error) VALUES (?, ?, ?, 'synced', '') ON CONFLICT(discussion_id) DO UPDATE SET detail_json=excluded.detail_json, last_synced_at=excluded.last_synced_at, sync_state='synced', last_error=''`, id, string(raw), now)
	return err
}

func (s *GroupDiscussionHistoryStore) materializeInlineTextAttachments(ctx context.Context, discussionID string, detail a2a.HubDiscussionDetail, attachmentRoot func(string) string) a2a.HubDiscussionDetail {
	if s == nil || attachmentRoot == nil || strings.TrimSpace(discussionID) == "" {
		return detail
	}
	root := strings.TrimSpace(attachmentRoot(discussionID))
	if root == "" {
		return detail
	}
	writeMessages := func(messages []a2a.Message) []a2a.Message {
		for i := range messages {
			messageID := strings.TrimSpace(messages[i].ID)
			if messageID == "" {
				messageID = fmt.Sprintf("message-%d", i+1)
			}
			for j := range messages[i].TextAttachments {
				att := &messages[i].TextAttachments[j]
				if strings.TrimSpace(att.LocalPath) != "" || strings.TrimSpace(att.Content) == "" {
					continue
				}
				decoded, err := decodeGroupDiscussionTextAttachment(att.Content)
				if err != nil || len(decoded) == 0 {
					continue
				}
				filename := safeGroupDiscussionFilename(att.Filename)
				if filename == "" {
					filename = fmt.Sprintf("text-%d.txt", j+1)
				}
				attachmentID := safeGroupDiscussionPathSegment(messageID + "-text-" + fmt.Sprint(j+1))
				localName := safeGroupDiscussionFilename(attachmentID + "-" + filename)
				if localName == "" {
					continue
				}
				if err := os.MkdirAll(root, 0o755); err != nil {
					continue
				}
				localPath := filepath.Join(root, localName)
				if _, statErr := os.Stat(localPath); statErr != nil {
					if writeErr := os.WriteFile(localPath, decoded, 0o644); writeErr != nil {
						continue
					}
				}
				att.LocalPath = localPath
				_ = s.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: attachmentID, DiscussionID: discussionID, MessageID: messageID, Kind: "text", Filename: filename, MimeType: att.MimeType, LocalPath: localPath, SizeBytes: int64(len(decoded)), DownloadState: "downloaded"})
			}
		}
		return messages
	}
	detail.Messages = writeMessages(detail.Messages)
	if detail.Session != nil {
		detail.Session.Messages = writeMessages(detail.Session.Messages)
	}
	return detail
}

func decodeGroupDiscussionTextAttachment(content string) ([]byte, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty text attachment")
	}
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(content)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (s *GroupDiscussionHistoryStore) mergeCachedSummaryRelation(ctx context.Context, incoming a2a.HubDiscussionSummary) a2a.HubDiscussionSummary {
	if s == nil || s.db == nil || strings.TrimSpace(incoming.ID) == "" {
		return normalizeHistorySummaryReadonly(incoming)
	}
	if strings.TrimSpace(incoming.LocalRelation) != "" {
		return normalizeHistorySummaryReadonly(incoming)
	}
	if relation := localRelationFromHistoryRole(incoming.Role); relation != "" {
		incoming.LocalRelation = relation
		return normalizeHistorySummaryReadonly(incoming)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT summary_json FROM group_discussion_summaries WHERE discussion_id = ?`, incoming.ID).Scan(&raw); err != nil {
		return normalizeHistorySummaryReadonly(incoming)
	}
	var cached a2a.HubDiscussionSummary
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return normalizeHistorySummaryReadonly(incoming)
	}
	incoming.Role = firstNonEmptyGroupString(incoming.Role, cached.Role)
	incoming.LocalRelation = firstNonEmptyGroupString(localRelationFromHistoryRole(incoming.Role), cached.LocalRelation)
	if incoming.Readonly || cached.Readonly {
		incoming.Readonly = true
	}
	return normalizeHistorySummaryReadonly(incoming)
}

func localRelationFromHistoryRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "initiator":
		return "initiated_by_me"
	case "review", "speak", "speaker", "observe", "observer", "participant":
		return "owned_ve_invited"
	default:
		return ""
	}
}
func normalizeHistorySummaryForCache(summary a2a.HubDiscussionSummary) a2a.HubDiscussionSummary {
	if strings.TrimSpace(summary.LocalRelation) == "" {
		summary.LocalRelation = localRelationFromHistoryRole(summary.Role)
	}
	return normalizeHistorySummaryReadonly(summary)
}

func normalizeHistorySummaryReadonly(summary a2a.HubDiscussionSummary) a2a.HubDiscussionSummary {
	if !normalizeGroupDiscussionSessionStatus(summary.Status).IsOpen() {
		summary.Readonly = true
		return summary
	}
	if strings.EqualFold(strings.TrimSpace(summary.LocalRelation), "owned_ve_invited") {
		summary.Readonly = true
	}
	return summary
}

func (s *GroupDiscussionHistoryStore) CachedDetail(ctx context.Context, discussionID string) (a2a.HubDiscussionDetail, bool, error) {
	var detail a2a.HubDiscussionDetail
	if s == nil || s.db == nil {
		return detail, false, nil
	}
	discussionID = strings.TrimSpace(discussionID)
	if discussionID == "" {
		return detail, false, nil
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT detail_json FROM group_discussion_details WHERE discussion_id = ?`, discussionID).Scan(&raw)
	if err == sql.ErrNoRows {
		return detail, false, nil
	}
	if err != nil {
		return detail, false, err
	}
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		return detail, false, err
	}
	detail.Discussion = normalizeHistorySummaryForCache(detail.Discussion)
	detail = s.EnrichDetailAttachments(ctx, detail)
	return detail, true, nil
}

func (s *GroupDiscussionHistoryStore) DownloadedAttachments(ctx context.Context, discussionID string) ([]GroupDiscussionAttachmentRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	discussionID = strings.TrimSpace(discussionID)
	if discussionID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT attachment_id, discussion_id, message_id, kind, filename, mime_type, hub_url, local_path, size_bytes, checksum, download_state FROM group_discussion_attachments WHERE discussion_id = ? AND download_state = 'downloaded' AND local_path <> ''`, discussionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []GroupDiscussionAttachmentRecord{}
	for rows.Next() {
		var record GroupDiscussionAttachmentRecord
		if err := rows.Scan(&record.AttachmentID, &record.DiscussionID, &record.MessageID, &record.Kind, &record.Filename, &record.MimeType, &record.HubURL, &record.LocalPath, &record.SizeBytes, &record.Checksum, &record.DownloadState); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *GroupDiscussionHistoryStore) EnrichDetailAttachments(ctx context.Context, detail a2a.HubDiscussionDetail) a2a.HubDiscussionDetail {
	if s == nil || s.db == nil {
		return detail
	}
	discussionID := strings.TrimSpace(detail.Discussion.ID)
	if discussionID == "" && detail.Session != nil {
		discussionID = strings.TrimSpace(detail.Session.ID)
	}
	records, err := s.DownloadedAttachments(ctx, discussionID)
	if err != nil || len(records) == 0 {
		return detail
	}
	byURL := map[string]string{}
	byAttachmentID := map[string]string{}
	byFilename := map[string]string{}
	for _, record := range records {
		localPath := strings.TrimSpace(record.LocalPath)
		if localPath == "" {
			continue
		}
		if attachmentID := strings.TrimSpace(record.AttachmentID); attachmentID != "" {
			byAttachmentID[attachmentID] = localPath
		}
		if hubURL := strings.TrimSpace(record.HubURL); hubURL != "" {
			byURL[hubURL] = localPath
			if attachmentID := groupDiscussionAttachmentIDFromURL(hubURL); attachmentID != "" {
				byAttachmentID[attachmentID] = localPath
			}
		}
		if filename := strings.TrimSpace(record.Filename); filename != "" {
			byFilename[filename] = localPath
		}
	}
	localPathForFileURL := func(fileURL, filename string) string {
		fileURL = strings.TrimSpace(fileURL)
		if fileURL != "" {
			if localPath := byURL[fileURL]; localPath != "" {
				return localPath
			}
			if attachmentID := groupDiscussionAttachmentIDFromURL(fileURL); attachmentID != "" {
				if localPath := byAttachmentID[attachmentID]; localPath != "" {
					return localPath
				}
			}
		}
		return byFilename[strings.TrimSpace(filename)]
	}
	enrichMessages := func(messages []a2a.Message) []a2a.Message {
		for i := range messages {
			for j := range messages[i].TextAttachments {
				if messages[i].TextAttachments[j].LocalPath == "" {
					messages[i].TextAttachments[j].LocalPath = byFilename[messages[i].TextAttachments[j].Filename]
				}
			}
			for j := range messages[i].ImageAttachments {
				if messages[i].ImageAttachments[j].LocalPath == "" {
					messages[i].ImageAttachments[j].LocalPath = localPathForFileURL(messages[i].ImageAttachments[j].FileURL, messages[i].ImageAttachments[j].Filename)
				}
			}
			for j := range messages[i].FileAttachments {
				if messages[i].FileAttachments[j].LocalPath == "" {
					messages[i].FileAttachments[j].LocalPath = localPathForFileURL(messages[i].FileAttachments[j].FileURL, messages[i].FileAttachments[j].Filename)
				}
			}
		}
		return messages
	}
	detail.Messages = enrichMessages(detail.Messages)
	if detail.Session != nil {
		detail.Session.Messages = enrichMessages(detail.Session.Messages)
	}
	return detail
}

func groupDiscussionAttachmentIDFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if idx := strings.IndexAny(rawURL, "?#"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	return pathBaseNoQuery(rawURL)
}

func (s *GroupDiscussionHistoryStore) UpsertDownloadedAttachment(ctx context.Context, record GroupDiscussionAttachmentRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	record.AttachmentID = strings.TrimSpace(record.AttachmentID)
	record.DiscussionID = strings.TrimSpace(record.DiscussionID)
	if record.AttachmentID == "" || record.DiscussionID == "" {
		return fmt.Errorf("attachment id and discussion id are required")
	}
	if strings.TrimSpace(record.DownloadState) == "" {
		record.DownloadState = "downloaded"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `INSERT INTO group_discussion_attachments (discussion_id, attachment_id, message_id, kind, filename, mime_type, hub_url, local_path, size_bytes, checksum, download_state, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(discussion_id, attachment_id) DO UPDATE SET message_id=excluded.message_id, kind=excluded.kind, filename=excluded.filename, mime_type=excluded.mime_type, hub_url=excluded.hub_url, local_path=excluded.local_path, size_bytes=excluded.size_bytes, checksum=excluded.checksum, download_state=excluded.download_state, updated_at=excluded.updated_at`, record.DiscussionID, record.AttachmentID, record.MessageID, record.Kind, record.Filename, record.MimeType, record.HubURL, record.LocalPath, record.SizeBytes, record.Checksum, record.DownloadState, now, now)
	return err
}

func (s *GroupDiscussionHistoryStore) SetHidden(ctx context.Context, discussionID string, hidden bool) error {
	if s == nil || s.db == nil {
		return nil
	}
	discussionID = strings.TrimSpace(discussionID)
	if discussionID == "" {
		return fmt.Errorf("discussion id is required")
	}
	visibility := "visible"
	hiddenAt := ""
	if hidden {
		visibility = "hidden"
		hiddenAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE group_discussion_summaries SET local_visibility = ?, hidden_at = ? WHERE discussion_id = ?`, visibility, hiddenAt, discussionID)
	return err
}

func filterGroupDiscussionSummariesByRole(summaries []a2a.HubDiscussionSummary, role string) []a2a.HubDiscussionSummary {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" || role == "all" {
		return summaries
	}
	targetRelation := localRelationFromHistoryRole(role)
	out := summaries[:0]
	for _, summary := range summaries {
		summaryRole := strings.ToLower(strings.TrimSpace(summary.Role))
		if summaryRole == role {
			out = append(out, summary)
			continue
		}
		if summaryRole == "" && targetRelation != "" && strings.EqualFold(strings.TrimSpace(summary.LocalRelation), targetRelation) {
			out = append(out, summary)
		}
	}
	return out
}

func mergeGroupDiscussionSummaries(live, cached []a2a.HubDiscussionSummary, role string) []a2a.HubDiscussionSummary {
	cached = filterGroupDiscussionSummariesByRole(cached, role)
	if len(cached) == 0 {
		return sortGroupDiscussionSummariesByUpdated(live)
	}
	seen := make(map[string]struct{}, len(live)+len(cached))
	out := make([]a2a.HubDiscussionSummary, 0, len(live)+len(cached))
	for _, summary := range live {
		id := strings.TrimSpace(summary.ID)
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, normalizeHistorySummaryForCache(summary))
	}
	for _, summary := range cached {
		id := strings.TrimSpace(summary.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, normalizeHistorySummaryForCache(summary))
	}
	return sortGroupDiscussionSummariesByUpdated(out)
}

func sortGroupDiscussionSummariesByUpdated(summaries []a2a.HubDiscussionSummary) []a2a.HubDiscussionSummary {
	sort.SliceStable(summaries, func(i, j int) bool {
		left := groupDiscussionSummarySortTime(summaries[i])
		right := groupDiscussionSummarySortTime(summaries[j])
		if left.IsZero() && right.IsZero() {
			return false
		}
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		return left.After(right)
	})
	return summaries
}

func groupDiscussionSummarySortTime(summary a2a.HubDiscussionSummary) time.Time {
	if !summary.UpdatedAt.IsZero() {
		return summary.UpdatedAt
	}
	return summary.CreatedAt
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func formatOptionalTime(v time.Time) string {
	if v.IsZero() {
		return ""
	}
	return v.UTC().Format(time.RFC3339Nano)
}

func (a *App) groupDiscussionHistoryDBPath() string {
	return filepath.Join(a.GetDataDir(), "group_discussion_history.db")
}

func (a *App) groupDiscussionAttachmentRoot(discussionID string) string {
	return filepath.Join(a.GetDataDir(), "group-discussions", safeGroupDiscussionPathSegment(discussionID), "attachments")
}

func safeGroupDiscussionPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "." || out == ".." {
		return "_" + out
	}
	return out
}

func (a *App) openGroupDiscussionHistoryStore() (*GroupDiscussionHistoryStore, error) {
	return NewGroupDiscussionHistoryStore(a.groupDiscussionHistoryDBPath())
}
