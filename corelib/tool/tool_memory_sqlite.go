package tool

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func toolMemoryDBPath(path string) string {
	ext := filepath.Ext(path)
	if strings.EqualFold(ext, ".json") {
		return strings.TrimSuffix(path, ext) + ".db"
	}
	return path + ".db"
}

const toolMemorySchema = `
CREATE TABLE IF NOT EXISTS tool_rules (
    key TEXT PRIMARY KEY,
    tool_name TEXT NOT NULL DEFAULT '',
    context TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    learned_at TEXT NOT NULL DEFAULT '',
    last_used_at TEXT NOT NULL DEFAULT '',
    used_count INTEGER NOT NULL DEFAULT 0,
    fail_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tool_rules_tool ON tool_rules(tool_name);
CREATE INDEX IF NOT EXISTS idx_tool_rules_context ON tool_rules(tool_name, context);
CREATE VIRTUAL TABLE IF NOT EXISTS tool_rules_fts USING fts5(
    key UNINDEXED,
    tool_name,
    context,
    content,
    tokenize='trigram'
);
CREATE TABLE IF NOT EXISTS tool_rule_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func (s *ToolMemoryStore) openDB() error {
	if s.path == "" {
		return os.ErrInvalid
	}
	dbPath := toolMemoryDBPath(s.path)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(toolMemorySchema); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}

func (s *ToolMemoryStore) loadDB() error {
	rows, err := s.db.Query(`SELECT key, tool_name, context, content, confidence, learned_at, last_used_at, used_count, fail_count FROM tool_rules`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var rules []ToolRule
	for rows.Next() {
		var rule ToolRule
		var learned, used string
		if err := rows.Scan(&rule.Key, &rule.ToolName, &rule.Context, &rule.Content, &rule.Confidence, &learned, &used, &rule.UsedCount, &rule.FailCount); err != nil {
			return err
		}
		rule.LearnedAt = parseToolRuleTime(learned)
		rule.LastUsedAt = parseToolRuleTime(used)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.rules = rules
	s.rulesLoaded = true
	return nil
}

func (s *ToolMemoryStore) importLegacyJSON() {
	var marker string
	err := s.db.QueryRow(`SELECT value FROM tool_rule_meta WHERE key='legacy_import'`).Scan(&marker)
	if err == nil && marker == "1" {
		return
	}
	if err != nil && err != sql.ErrNoRows {
		log.Printf("[tool-memory] legacy marker: %v", err)
		return
	}
	if len(s.rules) == 0 {
		if !s.loadLegacyFile() {
			return
		}
		if len(s.rules) > 0 {
			if err := s.saveDB(); err != nil {
				log.Printf("[tool-memory] legacy import: %v", err)
				return
			}
		}
	}
	_, _ = s.db.Exec(`INSERT INTO tool_rule_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
}

func (s *ToolMemoryStore) saveDB() error {
	if !s.rulesLoaded {
		// Memory is not the table. A full replace would delete rows the read
		// never returned.
		return fmt.Errorf("tool rules were not loaded")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tool_rules`); err != nil {
		return err
	}
	for _, rule := range s.rules {
		if _, err := tx.Exec(`INSERT INTO tool_rules(key, tool_name, context, content, confidence, learned_at, last_used_at, used_count, fail_count)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			rule.Key, rule.ToolName, rule.Context, rule.Content, rule.Confidence,
			formatToolRuleTime(rule.LearnedAt), formatToolRuleTime(rule.LastUsedAt), rule.UsedCount, rule.FailCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Search returns rules whose tool, context, text, or key matches query.
// The rule set is resident and capped, so the scan is the complete result.
func (s *ToolMemoryStore) Search(query string) []ToolRule {
	if s == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	needle := strings.ToLower(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ToolRule
	for _, rule := range s.rules {
		blob := strings.ToLower(rule.ToolName + "\n" + rule.Context + "\n" + rule.Content + "\n" + rule.Key)
		if strings.Contains(blob, needle) {
			out = append(out, rule)
		}
	}
	return out
}

func formatToolRuleTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseToolRuleTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}
