package guiapp

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

const aiAssistantUIStateLegacyImportKey = "legacy_json_imported"

const (
	aiAssistantUIHotLocation     = 0
	aiAssistantUIArchiveLocation = 1
)

const aiAssistantUIStateSQLiteSchema = `
CREATE TABLE IF NOT EXISTS ai_assistant_ui_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS ai_assistant_ui_messages (
    location INTEGER NOT NULL,
    position INTEGER NOT NULL,
    message_id TEXT NOT NULL,
    signature TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL,
    content TEXT,
    session_key TEXT,
    timestamp_ms REAL,
    is_local INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(location, position)
);
CREATE INDEX IF NOT EXISTS idx_ai_assistant_ui_messages_id ON ai_assistant_ui_messages(message_id);
CREATE INDEX IF NOT EXISTS idx_ai_assistant_ui_archive_local ON ai_assistant_ui_messages(location, is_local);
CREATE TABLE IF NOT EXISTS ai_assistant_ui_message_fields (
    location INTEGER NOT NULL,
    message_position INTEGER NOT NULL,
    field_path TEXT NOT NULL,
    value_type TEXT NOT NULL,
    value_text TEXT,
    value_number REAL,
    value_bool INTEGER,
    PRIMARY KEY(location, message_position, field_path)
);
CREATE INDEX IF NOT EXISTS idx_ai_assistant_ui_message_fields_path ON ai_assistant_ui_message_fields(field_path);
CREATE TABLE IF NOT EXISTS ai_assistant_ui_prompts (
    position INTEGER PRIMARY KEY,
    prompt TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ai_assistant_ui_prompt_attachments (
    prompt_position INTEGER NOT NULL,
    attachment_position INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    extension TEXT NOT NULL DEFAULT '',
    is_image INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(prompt_position, attachment_position)
);
`

type aiAssistantUIStateDatabase struct {
	db       *sql.DB
	path     string
	imported bool
}

func (a *App) aiAssistantUIStateDBPath() string {
	return filepath.Join(a.GetDataDir(), "ai_assistant_ui_state.db")
}

func (a *App) closeAIAssistantUIStateDBLocked() {
	if a == nil || a.aiAssistantUIStateDB == nil {
		return
	}
	_ = a.aiAssistantUIStateDB.db.Close()
	a.aiAssistantUIStateDB = nil
}

func (a *App) aiAssistantUIStateDBLocked() (*sql.DB, error) {
	if a == nil {
		return nil, os.ErrInvalid
	}
	path := a.aiAssistantUIStateDBPath()
	if a.aiAssistantUIStateDB != nil && a.aiAssistantUIStateDB.path != path {
		a.closeAIAssistantUIStateDBLocked()
	}
	if a.aiAssistantUIStateDB == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		if testing.Testing() {
			db.SetMaxIdleConns(0)
		} else {
			db.SetMaxIdleConns(1)
		}
		if _, err := db.Exec(aiAssistantUIStateSQLiteSchema); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := ensureAIAssistantUIMessageSignatures(db); err != nil {
			_ = db.Close()
			return nil, err
		}
		a.aiAssistantUIStateDB = &aiAssistantUIStateDatabase{db: db, path: path}
	}
	store := a.aiAssistantUIStateDB
	if !store.imported {
		if err := a.importLegacyAIAssistantUIStateLocked(store.db); err != nil {
			return nil, err
		}
		store.imported = true
	}
	return store.db, nil
}

func ensureAIAssistantUIMessageSignatures(db *sql.DB) error {
	var hasSignature int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('ai_assistant_ui_messages') WHERE name='signature'`).Scan(&hasSignature); err != nil {
		return err
	}
	if hasSignature > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`ALTER TABLE ai_assistant_ui_messages ADD COLUMN signature TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for _, location := range []int{aiAssistantUIHotLocation, aiAssistantUIArchiveLocation} {
		messages, err := loadAIAssistantUIMessageRows(tx, location)
		if err != nil {
			return err
		}
		for position, message := range messages {
			signature, err := aiAssistantUIMessageSignature(message)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE ai_assistant_ui_messages SET signature=? WHERE location=? AND position=?`, signature, location, position); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (a *App) importLegacyAIAssistantUIStateLocked(db *sql.DB) error {
	var imported int
	err := db.QueryRow(`SELECT COUNT(*) FROM ai_assistant_ui_meta WHERE key=?`, aiAssistantUIStateLegacyImportKey).Scan(&imported)
	if err != nil {
		return err
	}
	if imported > 0 {
		return nil
	}
	state := AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}}
	data, err := os.ReadFile(a.aiAssistantUIStatePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("decode legacy AI assistant UI state: %w", err)
		}
	}
	normalizeAIAssistantUIState(&state)
	archive := []interface{}{}
	if data, readErr := os.ReadFile(a.aiAssistantUIArchivePath()); readErr == nil && len(data) > 0 {
		var document conversationViewArchiveDocument
		if json.Unmarshal(data, &document) == nil {
			archive = document.Messages
		}
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeAIAssistantUIStateTx(tx, state); err != nil {
		return err
	}
	if err := writeAIAssistantUIArchiveTx(tx, archive); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_meta(key, value) VALUES(?, '1')`, aiAssistantUIStateLegacyImportKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) loadAIAssistantUIStateLocked() (AIAssistantUIState, error) {
	state := AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}, StoragePath: a.aiAssistantUIStateDBPath()}
	db, err := a.aiAssistantUIStateDBLocked()
	if err != nil {
		return state, err
	}
	var boundary, updated string
	if err := db.QueryRow(`SELECT
		COALESCE((SELECT value FROM ai_assistant_ui_meta WHERE key='context_boundary_message_id'), ''),
		COALESCE((SELECT value FROM ai_assistant_ui_meta WHERE key='updated_at'), '')`).Scan(&boundary, &updated); err != nil {
		return state, err
	}
	state.ContextBoundaryMessageID = boundary
	state.UpdatedAt = updated
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_assistant_ui_messages WHERE location=? AND is_local=1`, aiAssistantUIArchiveLocation).Scan(&state.ArchivedCount); err != nil {
		return state, err
	}
	state.Messages, err = loadAIAssistantUIMessageRows(db, aiAssistantUIHotLocation)
	if err != nil {
		return state, err
	}
	promptRows, err := db.Query(`SELECT position, prompt FROM ai_assistant_ui_prompts ORDER BY position`)
	if err != nil {
		return state, err
	}
	promptPositions := make([]int, 0)
	for promptRows.Next() {
		var position int
		var prompt string
		if err := promptRows.Scan(&position, &prompt); err != nil {
			_ = promptRows.Close()
			return state, err
		}
		promptPositions = append(promptPositions, position)
		state.Prompts = append(state.Prompts, prompt)
		state.PromptAttachments = append(state.PromptAttachments, []AIAssistantPromptAttachment{})
	}
	if err := promptRows.Err(); err != nil {
		_ = promptRows.Close()
		return state, err
	}
	_ = promptRows.Close()
	attachmentIndexes := make(map[int]int, len(promptPositions))
	for index, position := range promptPositions {
		attachmentIndexes[position] = index
	}
	attachmentRows, err := db.Query(`SELECT prompt_position, file_path, file_name, extension, is_image
		FROM ai_assistant_ui_prompt_attachments ORDER BY prompt_position, attachment_position`)
	if err != nil {
		return state, err
	}
	defer attachmentRows.Close()
	for attachmentRows.Next() {
		var promptPosition, isImage int
		var attachment AIAssistantPromptAttachment
		if err := attachmentRows.Scan(&promptPosition, &attachment.FilePath, &attachment.FileName, &attachment.Extension, &isImage); err != nil {
			return state, err
		}
		attachment.IsImage = isImage != 0
		if index, ok := attachmentIndexes[promptPosition]; ok {
			state.PromptAttachments[index] = append(state.PromptAttachments[index], attachment)
		}
	}
	if err := attachmentRows.Err(); err != nil {
		return state, err
	}
	normalizeAIAssistantUIState(&state)
	return state, nil
}

func (a *App) saveAIAssistantUIStateLocked(state AIAssistantUIState) error {
	db, err := a.aiAssistantUIStateDBLocked()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeAIAssistantUIStateTx(tx, state); err != nil {
		return err
	}
	return tx.Commit()
}

func aiAssistantPromptHistorySignature(state AIAssistantUIState) (string, error) {
	raw, err := json.Marshal(struct {
		Prompts     []string                        `json:"prompts"`
		Attachments [][]AIAssistantPromptAttachment `json:"attachments"`
	}{Prompts: state.Prompts, Attachments: state.PromptAttachments})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func writeAIAssistantUIStateTx(tx *sql.Tx, state AIAssistantUIState) error {
	if err := writeAIAssistantUIMessageRows(tx, aiAssistantUIHotLocation, state.Messages); err != nil {
		return err
	}
	promptSignature, err := aiAssistantPromptHistorySignature(state)
	if err != nil {
		return err
	}
	var storedPromptSignature string
	err = tx.QueryRow(`SELECT value FROM ai_assistant_ui_meta WHERE key='prompts_signature'`).Scan(&storedPromptSignature)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if storedPromptSignature != promptSignature {
		if _, err := tx.Exec(`DELETE FROM ai_assistant_ui_prompt_attachments`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM ai_assistant_ui_prompts`); err != nil {
			return err
		}
		for position, prompt := range state.Prompts {
			if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_prompts(position, prompt) VALUES(?,?)`, position, prompt); err != nil {
				return err
			}
			attachments := normalizeAIAssistantPromptAttachments(promptAttachmentRow(state.PromptAttachments, position))
			for attachmentPosition, attachment := range attachments {
				isImage := 0
				if attachment.IsImage {
					isImage = 1
				}
				if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_prompt_attachments(
					prompt_position, attachment_position, file_path, file_name, extension, is_image)
					VALUES(?,?,?,?,?,?)`, position, attachmentPosition, attachment.FilePath, attachment.FileName, attachment.Extension, isImage); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_meta(key, value) VALUES('prompts_signature', ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, promptSignature); err != nil {
			return err
		}
	}
	for key, value := range map[string]string{
		"context_boundary_message_id": state.ContextBoundaryMessageID,
		"updated_at":                  state.UpdatedAt,
	} {
		if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_meta(key, value) VALUES(?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) clearAIAssistantUIStateLocked() error {
	db, err := a.aiAssistantUIStateDBLocked()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"ai_assistant_ui_message_fields", "ai_assistant_ui_messages", "ai_assistant_ui_prompt_attachments", "ai_assistant_ui_prompts"} {
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM ai_assistant_ui_meta WHERE key<>?`, aiAssistantUIStateLegacyImportKey); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_meta(key, value) VALUES('updated_at', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func writeAIAssistantUIArchiveTx(tx *sql.Tx, messages []interface{}) error {
	return writeAIAssistantUIMessageRows(tx, aiAssistantUIArchiveLocation, anyToMaps(messages))
}

type aiAssistantUIStoredMessage struct {
	position  int
	signature string
}

type aiAssistantUIOutgoingMessage struct {
	message   map[string]interface{}
	signature string
}

func aiAssistantUIMessageSignature(message map[string]interface{}) (string, error) {
	raw, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func writeAIAssistantUIMessageRows(tx *sql.Tx, location int, messages []map[string]interface{}) error {
	rows, err := tx.Query(`SELECT position, message_id, signature FROM ai_assistant_ui_messages WHERE location=? ORDER BY position`, location)
	if err != nil {
		return err
	}
	existing := map[string]aiAssistantUIStoredMessage{}
	for rows.Next() {
		var position int
		var id, signature string
		if err := rows.Scan(&position, &id, &signature); err != nil {
			_ = rows.Close()
			return err
		}
		if id == "" || signature == "" {
			_ = rows.Close()
			return rewriteAIAssistantUIMessageRows(tx, location, messages)
		}
		if _, duplicate := existing[id]; duplicate {
			_ = rows.Close()
			return rewriteAIAssistantUIMessageRows(tx, location, messages)
		}
		existing[id] = aiAssistantUIStoredMessage{position: position, signature: signature}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	outgoing := make([]aiAssistantUIOutgoingMessage, len(messages))
	ids := make(map[string]struct{}, len(messages))
	stablePositions := true
	for position, message := range messages {
		id, _ := message["id"].(string)
		signature, err := aiAssistantUIMessageSignature(message)
		if err != nil {
			return err
		}
		if id == "" {
			return rewriteAIAssistantUIMessageRows(tx, location, messages)
		}
		if _, duplicate := ids[id]; duplicate {
			return rewriteAIAssistantUIMessageRows(tx, location, messages)
		}
		ids[id] = struct{}{}
		outgoing[position] = aiAssistantUIOutgoingMessage{message: message, signature: signature}
		if stored, ok := existing[id]; ok && stored.position != position {
			stablePositions = false
		}
	}
	if stablePositions {
		return writeAIAssistantUIStableRows(tx, location, existing, outgoing)
	}
	return writeAIAssistantUIReorderedRows(tx, location, existing, outgoing)
}

func writeAIAssistantUIStableRows(tx *sql.Tx, location int, existing map[string]aiAssistantUIStoredMessage, outgoing []aiAssistantUIOutgoingMessage) error {
	outgoingIndexes := make(map[string]int, len(outgoing))
	for index, item := range outgoing {
		id, _ := item.message["id"].(string)
		outgoingIndexes[id] = index
	}
	for id, stored := range existing {
		itemIndex, ok := outgoingIndexes[id]
		if ok && outgoing[itemIndex].signature == stored.signature {
			continue
		}
		if err := deleteAIAssistantUIMessageRow(tx, location, stored.position); err != nil {
			return err
		}
	}
	for position, item := range outgoing {
		id, _ := item.message["id"].(string)
		stored, ok := existing[id]
		if ok && stored.signature == item.signature {
			continue
		}
		if err := insertAIAssistantUIMessageRow(tx, location, position, item.message, item.signature); err != nil {
			return err
		}
	}
	return nil
}

func writeAIAssistantUIReorderedRows(tx *sql.Tx, location int, existing map[string]aiAssistantUIStoredMessage, outgoing []aiAssistantUIOutgoingMessage) error {
	if _, err := tx.Exec(`UPDATE ai_assistant_ui_messages SET position=-position-1 WHERE location=?`, location); err != nil {
		return err
	}
	outgoingIndexes := make(map[string]int, len(outgoing))
	for index, item := range outgoing {
		id, _ := item.message["id"].(string)
		outgoingIndexes[id] = index
	}
	for id, stored := range existing {
		itemIndex, ok := outgoingIndexes[id]
		if ok && outgoing[itemIndex].signature == stored.signature {
			continue
		}
		if err := deleteAIAssistantUIMessageRow(tx, location, -stored.position-1); err != nil {
			return err
		}
	}
	for position, item := range outgoing {
		id, _ := item.message["id"].(string)
		stored, ok := existing[id]
		if ok && stored.signature == item.signature {
			oldPosition := -stored.position - 1
			if _, err := tx.Exec(`UPDATE ai_assistant_ui_messages SET position=? WHERE location=? AND position=?`, position, location, oldPosition); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE ai_assistant_ui_message_fields SET message_position=? WHERE location=? AND message_position=?`, position, location, oldPosition); err != nil {
				return err
			}
			continue
		}
		if err := insertAIAssistantUIMessageRow(tx, location, position, item.message, item.signature); err != nil {
			return err
		}
	}
	return nil
}

func deleteAIAssistantUIMessageRow(tx *sql.Tx, location, position int) error {
	if _, err := tx.Exec(`DELETE FROM ai_assistant_ui_message_fields WHERE location=? AND message_position=?`, location, position); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM ai_assistant_ui_messages WHERE location=? AND position=?`, location, position)
	return err
}

func rewriteAIAssistantUIMessageRows(tx *sql.Tx, location int, messages []map[string]interface{}) error {
	if _, err := tx.Exec(`DELETE FROM ai_assistant_ui_message_fields WHERE location=?`, location); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM ai_assistant_ui_messages WHERE location=?`, location); err != nil {
		return err
	}
	for position, message := range messages {
		if err := insertAIAssistantUIMessageRow(tx, location, position, message, ""); err != nil {
			return err
		}
	}
	return nil
}

func insertAIAssistantUIMessageRow(tx *sql.Tx, location, position int, message map[string]interface{}, signature string) error {
	if signature == "" {
		var err error
		signature, err = aiAssistantUIMessageSignature(message)
		if err != nil {
			return err
		}
	}
	messageID, _ := message["id"].(string)
	role, _ := message["role"].(string)
	var content, sessionKey sql.NullString
	if value, ok := message["content"].(string); ok {
		content = sql.NullString{String: value, Valid: true}
	}
	if value, ok := message["sessionKey"].(string); ok {
		sessionKey = sql.NullString{String: value, Valid: true}
	}
	var timestamp sql.NullFloat64
	if value, ok := sqliteMessageNumber(message["timestamp"]); ok {
		timestamp = sql.NullFloat64{Float64: value, Valid: true}
	}
	isLocal := 0
	if conversationViewIsLocal(message) {
		isLocal = 1
	}
	if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_messages(
		location, position, message_id, signature, role, content, session_key, timestamp_ms, is_local)
		VALUES(?,?,?,?,?,?,?,?,?)`, location, position, messageID, signature, role, content, sessionKey, timestamp, isLocal); err != nil {
		return err
	}
	for key, value := range message {
		if key == "id" || key == "role" || (key == "content" && content.Valid) ||
			(key == "sessionKey" && sessionKey.Valid) || (key == "timestamp" && timestamp.Valid) {
			continue
		}
		fields, err := flattenSQLiteValueFields(value)
		if err != nil {
			return fmt.Errorf("encode AI assistant message field %q: %w", key, err)
		}
		for _, field := range fields {
			field.Path = sqliteMapFieldPath("", key) + field.Path
			if _, err := tx.Exec(`INSERT INTO ai_assistant_ui_message_fields(
				location, message_position, field_path, value_type, value_text, value_number, value_bool)
				VALUES(?,?,?,?,?,?,?)`, location, position, field.Path, field.Kind, nullableSQLiteFieldText(field), nullableSQLiteFieldNumber(field), nullableSQLiteFieldBool(field)); err != nil {
				return err
			}
		}
	}
	return nil
}

func sqliteMessageNumber(value interface{}) (float64, bool) {
	if value == nil {
		return 0, false
	}
	if number, ok := value.(json.Number); ok {
		parsed, err := number.Float64()
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	}
	number := reflect.ValueOf(value)
	var parsed float64
	switch number.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed = float64(number.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed = float64(number.Uint())
	case reflect.Float32, reflect.Float64:
		parsed = number.Float()
	default:
		return 0, false
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func nullableSQLiteFieldText(field sqliteValueField) interface{} {
	if field.Kind == "s" {
		return field.Text
	}
	return nil
}

func nullableSQLiteFieldNumber(field sqliteValueField) interface{} {
	if field.Kind == "n" {
		return field.Number
	}
	return nil
}

func nullableSQLiteFieldBool(field sqliteValueField) interface{} {
	if field.Kind != "b" {
		return nil
	}
	if field.Bool {
		return 1
	}
	return 0
}

type aiAssistantUIQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func loadAIAssistantUIMessageRows(db aiAssistantUIQueryer, location int) ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT position, message_id, role, content, session_key, timestamp_ms
		FROM ai_assistant_ui_messages WHERE location=? ORDER BY position`, location)
	if err != nil {
		return nil, err
	}
	positions := make([]int, 0)
	messages := make(map[int]map[string]interface{})
	for rows.Next() {
		var position int
		var messageID, role string
		var content, sessionKey sql.NullString
		var timestamp sql.NullFloat64
		if err := rows.Scan(&position, &messageID, &role, &content, &sessionKey, &timestamp); err != nil {
			_ = rows.Close()
			return nil, err
		}
		message := map[string]interface{}{"id": messageID, "role": role}
		if content.Valid {
			message["content"] = content.String
		}
		if sessionKey.Valid {
			message["sessionKey"] = sessionKey.String
		}
		if timestamp.Valid {
			message["timestamp"] = timestamp.Float64
		}
		positions = append(positions, position)
		messages[position] = message
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	fieldRows, err := db.Query(`SELECT message_position, field_path, value_type, value_text, value_number, value_bool
		FROM ai_assistant_ui_message_fields WHERE location=? ORDER BY message_position, field_path`, location)
	if err != nil {
		return nil, err
	}
	defer fieldRows.Close()
	fieldsByPosition := make(map[int][]sqliteValueField)
	for fieldRows.Next() {
		var position int
		var field sqliteValueField
		var text sql.NullString
		var number sql.NullFloat64
		var boolean sql.NullBool
		if err := fieldRows.Scan(&position, &field.Path, &field.Kind, &text, &number, &boolean); err != nil {
			return nil, err
		}
		field.Text = text.String
		field.Number = number.Float64
		field.Bool = boolean.Valid && boolean.Bool
		fieldsByPosition[position] = append(fieldsByPosition[position], field)
	}
	if err := fieldRows.Err(); err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(positions))
	for _, position := range positions {
		message := messages[position]
		if len(fieldsByPosition[position]) > 0 {
			fields, err := unflattenSQLiteValueFields(fieldsByPosition[position])
			if err != nil {
				return nil, err
			}
			for key, value := range fields {
				message[key] = value
			}
		}
		out = append(out, message)
	}
	return out, nil
}
