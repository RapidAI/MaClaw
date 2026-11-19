package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	_ "modernc.org/sqlite"
)

func pendingReplyDBPath(jsonPath string) string {
	ext := filepath.Ext(jsonPath)
	if strings.EqualFold(ext, ".json") {
		return strings.TrimSuffix(jsonPath, ext) + ".db"
	}
	return jsonPath + ".db"
}

func openPendingReplyDatabase(jsonPath string) (*sql.DB, error) {
	dbPath := pendingReplyDBPath(jsonPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS pending_reply_sessions (
    user_id TEXT PRIMARY KEY,
    ask_present INTEGER NOT NULL DEFAULT 0,
    ask_question TEXT NOT NULL DEFAULT '',
    ask_input_type TEXT NOT NULL DEFAULT '',
    ask_context TEXT NOT NULL DEFAULT '',
    ask_timestamp_unix_nano INTEGER NOT NULL DEFAULT 0,
    ask_mailbox_draft_id TEXT NOT NULL DEFAULT '',
    user_reply_present INTEGER NOT NULL DEFAULT 0,
    user_reply_question TEXT NOT NULL DEFAULT '',
    user_reply_timestamp_unix_nano INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS pending_reply_options (
    user_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY(user_id, position),
    FOREIGN KEY(user_id) REFERENCES pending_reply_sessions(user_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS pending_reply_fields (
    user_id TEXT NOT NULL,
    field_path TEXT NOT NULL,
    value_type TEXT NOT NULL,
    value_text TEXT,
    value_number REAL,
    value_bool INTEGER,
    PRIMARY KEY(user_id, field_path),
    FOREIGN KEY(user_id) REFERENCES pending_reply_sessions(user_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_pending_reply_fields_path ON pending_reply_fields(field_path);
CREATE TABLE IF NOT EXISTS pending_reply_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := importLegacyPendingReplies(db, jsonPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func importLegacyPendingReplies(db *sql.DB, jsonPath string) error {
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_reply_meta WHERE key='legacy_json_imported'`).Scan(&imported); err != nil {
		return err
	}
	if imported > 0 {
		return nil
	}
	snapshot := pendingReplyDiskSnapshot{Version: pendingReplyStoreVersion, Sessions: map[string]pendingReplyDiskSession{}}
	data, err := os.ReadFile(jsonPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &snapshot); err != nil {
			log.Printf("[PendingReply] parse failed: %v", err)
			snapshot = pendingReplyDiskSnapshot{Version: pendingReplyStoreVersion, Sessions: map[string]pendingReplyDiskSession{}}
		}
		if snapshot.Sessions == nil {
			snapshot.Sessions = map[string]pendingReplyDiskSession{}
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for userID, session := range snapshot.Sessions {
		if err := writePendingReplySession(tx, userID, session); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO pending_reply_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func loadPendingReplySnapshot(db *sql.DB) (pendingReplyDiskSnapshot, error) {
	snapshot := pendingReplyDiskSnapshot{Version: pendingReplyStoreVersion, Sessions: map[string]pendingReplyDiskSession{}}
	rows, err := db.Query(`SELECT user_id, ask_present, ask_question, ask_input_type, ask_context,
		ask_timestamp_unix_nano, ask_mailbox_draft_id, user_reply_present, user_reply_question,
		user_reply_timestamp_unix_nano FROM pending_reply_sessions`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var userID string
		var session pendingReplyDiskSession
		var askPresent, userPresent int
		var askQuestion, inputType, context, mailboxDraftID, userQuestion string
		var askTimestamp, userTimestamp int64
		if err := rows.Scan(&userID, &askPresent, &askQuestion, &inputType, &context, &askTimestamp,
			&mailboxDraftID, &userPresent, &userQuestion, &userTimestamp); err != nil {
			_ = rows.Close()
			return snapshot, err
		}
		if askPresent != 0 {
			session.Ask = &pendingAskUserState{Question: askQuestion, InputType: inputType, Context: context, MailboxDraftID: mailboxDraftID}
			if askTimestamp != 0 {
				session.Ask.Timestamp = time.Unix(0, askTimestamp)
			}
		}
		if userPresent != 0 {
			session.UserReply = &pendingUserReplyState{Question: userQuestion}
			if userTimestamp != 0 {
				session.UserReply.Timestamp = time.Unix(0, userTimestamp)
			}
		}
		snapshot.Sessions[userID] = session
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return snapshot, err
	}
	_ = rows.Close()
	optionRows, err := db.Query(`SELECT user_id, value FROM pending_reply_options ORDER BY user_id, position`)
	if err != nil {
		return snapshot, err
	}
	for optionRows.Next() {
		var userID, value string
		if err := optionRows.Scan(&userID, &value); err != nil {
			_ = optionRows.Close()
			return snapshot, err
		}
		session := snapshot.Sessions[userID]
		if session.Ask != nil {
			session.Ask.Options = append(session.Ask.Options, value)
			snapshot.Sessions[userID] = session
		}
	}
	if err := optionRows.Err(); err != nil {
		_ = optionRows.Close()
		return snapshot, err
	}
	_ = optionRows.Close()
	fieldRows, err := db.Query(`SELECT user_id, field_path, value_type, value_text, value_number, value_bool
		FROM pending_reply_fields ORDER BY user_id, field_path`)
	if err != nil {
		return snapshot, err
	}
	defer fieldRows.Close()
	fieldsByUser := map[string][]sqliteValueField{}
	for fieldRows.Next() {
		var userID string
		var field sqliteValueField
		var text sql.NullString
		var number sql.NullFloat64
		var boolean sql.NullBool
		if err := fieldRows.Scan(&userID, &field.Path, &field.Kind, &text, &number, &boolean); err != nil {
			return snapshot, err
		}
		field.Text = text.String
		field.Number = number.Float64
		field.Bool = boolean.Valid && boolean.Bool
		fieldsByUser[userID] = append(fieldsByUser[userID], field)
	}
	if err := fieldRows.Err(); err != nil {
		return snapshot, err
	}
	for userID, fields := range fieldsByUser {
		values, err := unflattenSQLiteValueFields(fields)
		if err != nil {
			return snapshot, err
		}
		session := snapshot.Sessions[userID]
		if session.Ask != nil {
			if value, ok := values["History"]; ok {
				if err := decodePendingReplyValue(value, &session.Ask.History); err != nil {
					return snapshot, err
				}
			}
			if value, ok := values["WorkingState"]; ok {
				var working agent.WorkingState
				if err := decodePendingReplyValue(value, &working); err != nil {
					return snapshot, err
				}
				session.Ask.WorkingState = &working
			}
		}
		if session.UserReply != nil {
			if value, ok := values["UserReplyHistory"]; ok {
				if err := decodePendingReplyValue(value, &session.UserReply.History); err != nil {
					return snapshot, err
				}
			}
		}
		snapshot.Sessions[userID] = session
	}
	return snapshot, nil
}

func decodePendingReplyValue(value interface{}, target interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("rebuild pending reply value: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("rebuild pending reply value: %w", err)
	}
	return nil
}

func savePendingReplySession(db *sql.DB, userID string, session pendingReplyDiskSession) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writePendingReplySession(tx, userID, session); err != nil {
		return err
	}
	return tx.Commit()
}

func writePendingReplySession(tx *sql.Tx, userID string, session pendingReplyDiskSession) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("pending reply user id is required")
	}
	if session.Ask == nil && session.UserReply == nil {
		_, err := tx.Exec(`DELETE FROM pending_reply_sessions WHERE user_id=?`, userID)
		return err
	}
	askPresent, userPresent := 0, 0
	var askQuestion, inputType, context, mailboxDraftID, userQuestion string
	var askTimestamp, userTimestamp int64
	if session.Ask != nil {
		askPresent = 1
		askQuestion = session.Ask.Question
		inputType = session.Ask.InputType
		context = session.Ask.Context
		mailboxDraftID = session.Ask.MailboxDraftID
		if !session.Ask.Timestamp.IsZero() {
			askTimestamp = session.Ask.Timestamp.UnixNano()
		}
	}
	if session.UserReply != nil {
		userPresent = 1
		userQuestion = session.UserReply.Question
		if !session.UserReply.Timestamp.IsZero() {
			userTimestamp = session.UserReply.Timestamp.UnixNano()
		}
	}
	if _, err := tx.Exec(`INSERT INTO pending_reply_sessions(
		user_id, ask_present, ask_question, ask_input_type, ask_context, ask_timestamp_unix_nano,
		ask_mailbox_draft_id, user_reply_present, user_reply_question, user_reply_timestamp_unix_nano)
		VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET ask_present=excluded.ask_present,
		ask_question=excluded.ask_question, ask_input_type=excluded.ask_input_type, ask_context=excluded.ask_context,
		ask_timestamp_unix_nano=excluded.ask_timestamp_unix_nano, ask_mailbox_draft_id=excluded.ask_mailbox_draft_id,
		user_reply_present=excluded.user_reply_present, user_reply_question=excluded.user_reply_question,
		user_reply_timestamp_unix_nano=excluded.user_reply_timestamp_unix_nano`,
		userID, askPresent, askQuestion, inputType, context, askTimestamp, mailboxDraftID,
		userPresent, userQuestion, userTimestamp); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pending_reply_options WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pending_reply_fields WHERE user_id=?`, userID); err != nil {
		return err
	}
	if session.Ask != nil {
		for position, option := range session.Ask.Options {
			if _, err := tx.Exec(`INSERT INTO pending_reply_options(user_id, position, value) VALUES(?,?,?)`, userID, position, option); err != nil {
				return err
			}
		}
		if err := writePendingReplyFields(tx, userID, "History", session.Ask.History); err != nil {
			return err
		}
		if session.Ask.WorkingState != nil {
			if err := writePendingReplyFields(tx, userID, "WorkingState", session.Ask.WorkingState); err != nil {
				return err
			}
		}
	}
	if session.UserReply != nil {
		if err := writePendingReplyFields(tx, userID, "UserReplyHistory", session.UserReply.History); err != nil {
			return err
		}
	}
	return nil
}

func writePendingReplyFields(tx *sql.Tx, userID, name string, value interface{}) error {
	if value == nil {
		return nil
	}
	fields, err := flattenSQLiteValueFields(value)
	if err != nil {
		return err
	}
	for _, field := range fields {
		field.Path = sqliteMapFieldPath("", name) + field.Path
		if _, err := tx.Exec(`INSERT INTO pending_reply_fields(
			user_id, field_path, value_type, value_text, value_number, value_bool) VALUES(?,?,?,?,?,?)`,
			userID, field.Path, field.Kind, nullableSQLiteFieldText(field), nullableSQLiteFieldNumber(field), nullableSQLiteFieldBool(field)); err != nil {
			return err
		}
	}
	return nil
}

func closePendingReplyDiskStores() {
	pendingReplyDiskStores.Lock()
	defer pendingReplyDiskStores.Unlock()
	for path, store := range pendingReplyDiskStores.byPath {
		store.mu.Lock()
		if store.db != nil {
			_ = store.db.Close()
			store.db = nil
		}
		store.mu.Unlock()
		delete(pendingReplyDiskStores.byPath, path)
	}
}
