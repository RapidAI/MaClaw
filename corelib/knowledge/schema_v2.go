package knowledge

import (
	"context"
	"database/sql"
	"fmt"
)

const knowledgeSchemaVersionV2 = 2

func createSchemaV2(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kb_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kb_sources (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			uri TEXT NOT NULL,
			canonical_uri TEXT,
			title TEXT,
			author TEXT,
			site_name TEXT,
			published_at TEXT,
			fetched_at TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			owner_id TEXT,
			tenant_id TEXT,
			project_path TEXT,
			topic_hint TEXT,
			source_trust REAL DEFAULT 0.5,
			batch_id TEXT,
			relative_path TEXT,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kb_tables (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			sheet_name TEXT NOT NULL,
			table_title TEXT,
			header_row_index INTEGER DEFAULT 0,
			row_count INTEGER DEFAULT 0,
			column_count INTEGER DEFAULT 0,
			schema_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS kb_columns (
			id TEXT PRIMARY KEY,
			table_id TEXT NOT NULL,
			column_index INTEGER NOT NULL,
			column_name TEXT NOT NULL,
			normalized_name TEXT NOT NULL,
			value_type TEXT NOT NULL,
			aliases_json TEXT,
			stats_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(table_id) REFERENCES kb_tables(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS kb_rows (
			id TEXT PRIMARY KEY,
			table_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			row_index INTEGER NOT NULL,
			primary_key_text TEXT,
			row_text TEXT,
			row_json TEXT,
			embedding BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(table_id) REFERENCES kb_tables(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS kb_rows_fts USING fts5(row_id UNINDEXED, primary_key_text, row_text)`,
		`CREATE TABLE IF NOT EXISTS kb_cells (
			id TEXT PRIMARY KEY,
			row_id TEXT NOT NULL,
			table_id TEXT NOT NULL,
			column_id TEXT NOT NULL,
			column_name TEXT NOT NULL,
			normalized_column_name TEXT NOT NULL,
			raw_value TEXT,
			normalized_value TEXT,
			value_type TEXT NOT NULL,
			number_value REAL,
			date_value TEXT,
			bool_value INTEGER,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(row_id) REFERENCES kb_rows(id) ON DELETE CASCADE,
			FOREIGN KEY(table_id) REFERENCES kb_tables(id) ON DELETE CASCADE,
			FOREIGN KEY(column_id) REFERENCES kb_columns(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS kb_cards (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			row_id TEXT,
			origin_type TEXT NOT NULL DEFAULT 'document',
			title TEXT,
			claim TEXT NOT NULL,
			summary TEXT,
			entities_json TEXT,
			topics_json TEXT,
			tags_json TEXT,
			project_path TEXT,
			owner_id TEXT,
			tenant_id TEXT,
			valid_at TEXT,
			invalid_at TEXT,
			confidence REAL DEFAULT 0.5,
			importance REAL DEFAULT 1.0,
			source_trust REAL DEFAULT 0.5,
			embedding BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(row_id) REFERENCES kb_rows(id) ON DELETE SET NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS kb_cards_fts USING fts5(card_id UNINDEXED, title, claim, summary)`,
		`CREATE TABLE IF NOT EXISTS kb_facts (
			id TEXT PRIMARY KEY,
			card_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			row_id TEXT,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			normalized_object TEXT,
			value_type TEXT,
			number_value REAL,
			date_value TEXT,
			bool_value INTEGER,
			negated INTEGER DEFAULT 0,
			valid_at TEXT,
			invalid_at TEXT,
			confidence REAL DEFAULT 0.5,
			created_at TEXT NOT NULL,
			FOREIGN KEY(card_id) REFERENCES kb_cards(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(row_id) REFERENCES kb_rows(id) ON DELETE SET NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS kb_facts_fts USING fts5(fact_id UNINDEXED, subject, predicate, object)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_sources_scope ON kb_sources(tenant_id, owner_id, project_path, kind, status)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_sources_hash ON kb_sources(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_tables_source_sheet ON kb_tables(source_id, sheet_name)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_tables_source_sheet_lower ON kb_tables(source_id, LOWER(sheet_name))`,
		`CREATE INDEX IF NOT EXISTS idx_kb_columns_table_index ON kb_columns(table_id, column_index)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_columns_table_name ON kb_columns(table_id, normalized_name)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_rows_table_row ON kb_rows(table_id, row_index)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_rows_source_row ON kb_rows(source_id, row_index)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_exact ON kb_cells(table_id, normalized_column_name, normalized_value)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_row_col ON kb_cells(row_id, normalized_column_name)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_col_value_row ON kb_cells(normalized_column_name, normalized_value, row_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_col_value_lower_row ON kb_cells(normalized_column_name, LOWER(normalized_value), row_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_col_raw_lower_row ON kb_cells(normalized_column_name, LOWER(raw_value), row_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_col_number_row ON kb_cells(normalized_column_name, number_value, row_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_col_date_row ON kb_cells(normalized_column_name, date_value, row_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_col_bool_row ON kb_cells(normalized_column_name, bool_value, row_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_number ON kb_cells(table_id, column_id, number_value)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cells_date ON kb_cells(table_id, column_id, date_value)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cards_scope ON kb_cards(tenant_id, owner_id, project_path, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_facts_subject ON kb_facts(subject)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_facts_object ON kb_facts(object)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("knowledge sqlite schema v2: %w", err)
		}
	}
	return nil
}
