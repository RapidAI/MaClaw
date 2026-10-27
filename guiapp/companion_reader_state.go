package guiapp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Companion reading state used to live in the WebView localStorage: the PDF
// highlights and the composer's typed-question history. That origin is shared
// with chat transcripts and can be cleared with site data, so the state now
// lives beside the rest of the app data in SQLite. The old browser documents
// are imported once by LoadCompanionReaderState and deleted by the caller only
// after the import returned no error.

const (
	companionReaderPDFMarksKind        = "pdf_marks"
	companionReaderPDFTranslationsKind = "pdf_translations"
	companionReaderChatMarksKind       = "chat_marks"
	companionReaderInputHistoryKind    = "input_history"
	// One document can hold many long translations (200 × 12k characters), so
	// the 1MB bound the old localStorage era used is too small for Chinese text.
	companionReaderValueMaxBytes = 8 << 20
	companionReaderKeyMaxBytes   = 4096
	companionReaderEntryMax      = 100
	companionReaderEntryRunesMax = 12_000
)

var (
	companionReaderStateMu  sync.Mutex
	companionReaderPathRe   = regexp.MustCompile(`^[a-zA-Z]:/`)
	companionReaderStateNow = func() string { return time.Now().UTC().Format(time.RFC3339) }
)

func (a *App) companionReaderStateDBPath() string {
	return filepath.Join(a.GetDataDir(), "companion_reader_state.db")
}

func openCompanionReaderStateDB(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("companion reader state database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create companion reader state directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open companion reader state database: %w", err)
	}
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000"} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure companion reader state database: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS reader_state (
		kind TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (kind, key)
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create companion reader state table: %w", err)
	}
	return db, nil
}

// companionReaderPathKey matches the reader's key: forward slashes, and drive
// letters or UNC paths lowercased so a Windows path compares consistently.
func companionReaderPathKey(path string) string {
	key := strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	if companionReaderPathRe.MatchString(key) || strings.HasPrefix(key, "//") {
		key = strings.ToLower(key)
	}
	return key
}

// companionReaderNormalizeHistory keeps the same shape the composer kept in
// localStorage: trimmed non-blank lines, newest last, at most 100.
func companionReaderNormalizeHistory(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		text := strings.TrimSpace(entry)
		if text == "" || len([]rune(text)) > companionReaderEntryRunesMax {
			continue
		}
		out = append(out, text)
	}
	if len(out) > companionReaderEntryMax {
		out = out[len(out)-companionReaderEntryMax:]
	}
	return out
}

// LoadCompanionReaderState returns every stored value for the companion reader
// as JSON: {"pdf_marks": {path: [...mark...]}, "chat_marks": {path: [...mark...]},
// "input_history": ["..."]}.
// legacyPDFMarksJSON and legacyInputHistoryJSON are the old WebView documents
// (maclaw.fileCompanionPdfMarks.v1 and maclaw.fileCompanionInputHistory.v1).
// A non-empty document inserts rows that are not stored yet, so a leftover
// browser document cannot replace state saved later. The caller deletes the
// browser keys only after this returns no error. Empty strings only read.
func (a *App) LoadCompanionReaderState(legacyPDFMarksJSON, legacyPDFTranslationsJSON, legacyInputHistoryJSON string) (string, error) {
	companionReaderStateMu.Lock()
	defer companionReaderStateMu.Unlock()
	db, err := openCompanionReaderStateDB(a.companionReaderStateDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := importCompanionReaderState(db, legacyPDFMarksJSON, legacyPDFTranslationsJSON, legacyInputHistoryJSON); err != nil {
		return "", err
	}
	payload, err := readCompanionReaderState(db)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode companion reader state: %w", err)
	}
	return string(body), nil
}

type companionReaderStatePayload struct {
	PDFMarks        map[string]json.RawMessage `json:"pdf_marks"`
	PDFTranslations map[string]json.RawMessage `json:"pdf_translations"`
	ChatMarks       map[string]json.RawMessage `json:"chat_marks"`
	InputHistory    []string                   `json:"input_history"`
}

func readCompanionReaderState(db *sql.DB) (companionReaderStatePayload, error) {
	payload := companionReaderStatePayload{
		PDFMarks:        map[string]json.RawMessage{},
		PDFTranslations: map[string]json.RawMessage{},
		ChatMarks:       map[string]json.RawMessage{},
		InputHistory:    []string{},
	}
	rows, err := db.Query(`SELECT kind, key, value FROM reader_state`)
	if err != nil {
		return payload, fmt.Errorf("read companion reader state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, key, value string
		if scanErr := rows.Scan(&kind, &key, &value); scanErr != nil {
			return payload, fmt.Errorf("scan companion reader state: %w", scanErr)
		}
		switch kind {
		case companionReaderPDFMarksKind:
			if json.Valid([]byte(value)) {
				payload.PDFMarks[key] = json.RawMessage(value)
			}
		case companionReaderPDFTranslationsKind:
			if json.Valid([]byte(value)) {
				payload.PDFTranslations[key] = json.RawMessage(value)
			}
		case companionReaderChatMarksKind:
			if json.Valid([]byte(value)) {
				payload.ChatMarks[key] = json.RawMessage(value)
			}
		case companionReaderInputHistoryKind:
			var rawEntries []json.RawMessage
			if json.Unmarshal([]byte(value), &rawEntries) == nil {
				entries := make([]string, 0, len(rawEntries))
				for _, raw := range rawEntries {
					var text string
					if json.Unmarshal(raw, &text) == nil {
						entries = append(entries, text)
					}
				}
				payload.InputHistory = companionReaderNormalizeHistory(entries)
			}
		}
	}
	return payload, rows.Err()
}

func importCompanionReaderState(db *sql.DB, legacyPDFMarksJSON, legacyPDFTranslationsJSON, legacyInputHistoryJSON string) error {
	legacyPDFMarksJSON = strings.TrimSpace(legacyPDFMarksJSON)
	legacyPDFTranslationsJSON = strings.TrimSpace(legacyPDFTranslationsJSON)
	legacyInputHistoryJSON = strings.TrimSpace(legacyInputHistoryJSON)
	if legacyPDFMarksJSON == "" && legacyPDFTranslationsJSON == "" && legacyInputHistoryJSON == "" {
		return nil
	}
	if len(legacyPDFMarksJSON) > companionReaderValueMaxBytes ||
		len(legacyPDFTranslationsJSON) > companionReaderValueMaxBytes ||
		len(legacyInputHistoryJSON) > companionReaderValueMaxBytes {
		return fmt.Errorf("companion reader state import exceeds %d bytes", companionReaderValueMaxBytes)
	}
	now := companionReaderStateNow()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin companion reader state import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if legacyPDFMarksJSON != "" {
		var doc map[string]json.RawMessage
		if json.Unmarshal([]byte(legacyPDFMarksJSON), &doc) != nil {
			// A broken document imports nothing; the state it would replace is
			// still readable, and the caller keeps the browser key.
			return fmt.Errorf("companion pdf marks import must be a JSON object")
		}
		for path, rawMarks := range doc {
			key := companionReaderPathKey(path)
			if key == "" || len(key) > companionReaderKeyMaxBytes {
				continue
			}
			var marks []json.RawMessage
			if json.Unmarshal(rawMarks, &marks) != nil || len(marks) == 0 {
				continue
			}
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO reader_state (kind, key, value, updated_at) VALUES (?, ?, ?, ?)`,
				companionReaderPDFMarksKind, key, string(rawMarks), now,
			); err != nil {
				return fmt.Errorf("import companion pdf marks: %w", err)
			}
		}
	}
	if legacyPDFTranslationsJSON != "" {
		var doc map[string]json.RawMessage
		if json.Unmarshal([]byte(legacyPDFTranslationsJSON), &doc) != nil {
			return fmt.Errorf("companion pdf translations import must be a JSON object")
		}
		for path, rawList := range doc {
			key := companionReaderPathKey(path)
			if key == "" || len(key) > companionReaderKeyMaxBytes {
				continue
			}
			var list []json.RawMessage
			if json.Unmarshal(rawList, &list) != nil || len(list) == 0 {
				continue
			}
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO reader_state (kind, key, value, updated_at) VALUES (?, ?, ?, ?)`,
				companionReaderPDFTranslationsKind, key, string(rawList), now,
			); err != nil {
				return fmt.Errorf("import companion pdf translations: %w", err)
			}
		}
	}
	if legacyInputHistoryJSON != "" {
		// A browser document can hold anything a previous build wrote. Keep the
		// string entries instead of failing the whole state read over one of
		// them being a number or null.
		var rawEntries []json.RawMessage
		if json.Unmarshal([]byte(legacyInputHistoryJSON), &rawEntries) != nil {
			return fmt.Errorf("companion input history import must be a JSON array")
		}
		entries := make([]string, 0, len(rawEntries))
		for _, raw := range rawEntries {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				entries = append(entries, text)
			}
		}
		entries = companionReaderNormalizeHistory(entries)
		if len(entries) > 0 {
			body, marshalErr := json.Marshal(entries)
			if marshalErr != nil {
				return fmt.Errorf("encode imported input history: %w", marshalErr)
			}
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO reader_state (kind, key, value, updated_at) VALUES (?, ?, ?, ?)`,
				companionReaderInputHistoryKind, "default", string(body), now,
			); err != nil {
				return fmt.Errorf("import companion input history: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit companion reader state import: %w", err)
	}
	return nil
}

// SaveCompanionPdfMarks stores one document's highlights. An empty list removes
// the row, which is what clearing every highlight means.
func (a *App) SaveCompanionPdfMarks(path, marksJSON string) error {
	key := companionReaderPathKey(path)
	if key == "" || len(key) > companionReaderKeyMaxBytes {
		return fmt.Errorf("invalid companion document path")
	}
	marksJSON = strings.TrimSpace(marksJSON)
	if len(marksJSON) > companionReaderValueMaxBytes {
		return fmt.Errorf("companion pdf marks exceed %d bytes", companionReaderValueMaxBytes)
	}
	var marks []json.RawMessage
	if err := json.Unmarshal([]byte(marksJSON), &marks); err != nil {
		return fmt.Errorf("companion pdf marks must be a JSON array")
	}
	if len(marks) == 0 {
		return a.deleteCompanionReaderState(companionReaderPDFMarksKind, key)
	}
	return a.writeCompanionReaderState(companionReaderPDFMarksKind, key, marksJSON)
}

// SaveCompanionPdfTranslations stores one document's translations. An empty
// list removes the row, which is what clearing every translation means.
func (a *App) SaveCompanionPdfTranslations(path, translationsJSON string) error {
	key := companionReaderPathKey(path)
	if key == "" || len(key) > companionReaderKeyMaxBytes {
		return fmt.Errorf("invalid companion document path")
	}
	translationsJSON = strings.TrimSpace(translationsJSON)
	if len(translationsJSON) > companionReaderValueMaxBytes {
		return fmt.Errorf("companion pdf translations exceed %d bytes", companionReaderValueMaxBytes)
	}
	var list []json.RawMessage
	if err := json.Unmarshal([]byte(translationsJSON), &list); err != nil {
		return fmt.Errorf("companion pdf translations must be a JSON array")
	}
	if len(list) == 0 {
		return a.deleteCompanionReaderState(companionReaderPDFTranslationsKind, key)
	}
	return a.writeCompanionReaderState(companionReaderPDFTranslationsKind, key, translationsJSON)
}

// SaveCompanionChatMarks stores the highlights a reader placed on the agent's
// answers for one document. The answer text itself is transcript data and is
// not touched. An empty list removes the row.
func (a *App) SaveCompanionChatMarks(path, marksJSON string) error {
	key := companionReaderPathKey(path)
	if key == "" || len(key) > companionReaderKeyMaxBytes {
		return fmt.Errorf("invalid companion document path")
	}
	marksJSON = strings.TrimSpace(marksJSON)
	if len(marksJSON) > companionReaderValueMaxBytes {
		return fmt.Errorf("companion chat marks exceed %d bytes", companionReaderValueMaxBytes)
	}
	var marks []json.RawMessage
	if err := json.Unmarshal([]byte(marksJSON), &marks); err != nil {
		return fmt.Errorf("companion chat marks must be a JSON array")
	}
	if len(marks) == 0 {
		return a.deleteCompanionReaderState(companionReaderChatMarksKind, key)
	}
	return a.writeCompanionReaderState(companionReaderChatMarksKind, key, marksJSON)
}

// SaveCompanionInputHistory stores the composer's typed-question history. The
// caller sends the full list, newest last.
func (a *App) SaveCompanionInputHistory(historyJSON string) error {
	historyJSON = strings.TrimSpace(historyJSON)
	if len(historyJSON) > companionReaderValueMaxBytes {
		return fmt.Errorf("companion input history exceeds %d bytes", companionReaderValueMaxBytes)
	}
	var entries []string
	if err := json.Unmarshal([]byte(historyJSON), &entries); err != nil {
		return fmt.Errorf("companion input history must be a JSON array of strings")
	}
	entries = companionReaderNormalizeHistory(entries)
	if len(entries) == 0 {
		return a.deleteCompanionReaderState(companionReaderInputHistoryKind, "default")
	}
	body, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode companion input history: %w", err)
	}
	return a.writeCompanionReaderState(companionReaderInputHistoryKind, "default", string(body))
}

func (a *App) writeCompanionReaderState(kind, key, value string) error {
	companionReaderStateMu.Lock()
	defer companionReaderStateMu.Unlock()
	db, err := openCompanionReaderStateDB(a.companionReaderStateDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO reader_state (kind, key, value, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(kind, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		kind, key, value, companionReaderStateNow(),
	)
	if err != nil {
		return fmt.Errorf("save companion reader state: %w", err)
	}
	return nil
}

func (a *App) deleteCompanionReaderState(kind, key string) error {
	companionReaderStateMu.Lock()
	defer companionReaderStateMu.Unlock()
	db, err := openCompanionReaderStateDB(a.companionReaderStateDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM reader_state WHERE kind = ? AND key = ?`, kind, key); err != nil {
		return fmt.Errorf("clear companion reader state: %w", err)
	}
	return nil
}
