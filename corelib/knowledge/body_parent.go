package knowledge

import (
	"context"
	"database/sql"
	"fmt"
)

// ensureKnowledgeBodyParent copies source rows that exist only in kb_sources
// into knowledge_sources, then points spreadsheet child tables at
// knowledge_sources. SQLite cannot change a foreign key in place, so an old
// library is rewritten inside one transaction and the same table names remain.
// A library that already uses knowledge_sources only reads sqlite_master and
// the foreign-key list.
func ensureKnowledgeBodyParent(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	hasLegacy, err := tableExists(ctx, db, "kb_sources")
	if err != nil || !hasLegacy {
		return err
	}
	hasSources, err := tableExists(ctx, db, "knowledge_sources")
	if err != nil || !hasSources {
		return err
	}
	needCopy, err := kbSourcesMissingFromKnowledge(ctx, db)
	if err != nil {
		return err
	}
	needRetarget, err := bodyParentsNeedRetarget(ctx, db)
	if err != nil {
		return err
	}
	if !needCopy && !needRetarget {
		return nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if needRetarget {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return fmt.Errorf("knowledge sqlite disable foreign keys for body parent: %w", err)
		}
		defer conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if needCopy {
		if err := copyKBSourcesIntoKnowledgeSources(ctx, tx); err != nil {
			return err
		}
	}
	if needRetarget {
		for _, spec := range bodyParentTables() {
			parent, ok, err := kbSourceForeignKeyParent(ctx, tx, spec.name)
			if err != nil {
				return err
			}
			if !ok || parent != "kb_sources" {
				continue
			}
			if err := rebuildBodyParentTable(ctx, tx, spec); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func kbSourcesMissingFromKnowledge(ctx context.Context, db *sql.DB) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM kb_sources k
		WHERE NOT EXISTS (SELECT 1 FROM knowledge_sources s WHERE s.id = k.id)
		LIMIT 1`).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("knowledge sqlite probe kb_sources backfill: %w", err)
	}
	return true, nil
}

func bodyParentsNeedRetarget(ctx context.Context, db *sql.DB) (bool, error) {
	for _, spec := range bodyParentTables() {
		exists, err := tableExists(ctx, db, spec.name)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		parent, ok, err := kbSourceForeignKeyParent(ctx, db, spec.name)
		if err != nil {
			return false, err
		}
		if ok && parent == "kb_sources" {
			return true, nil
		}
	}
	return false, nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func kbSourceForeignKeyParent(ctx context.Context, q queryRower, table string) (string, bool, error) {
	var parent string
	query := `SELECT "table" FROM pragma_foreign_key_list('` + table + `') WHERE "from" = 'source_id'`
	err := q.QueryRowContext(ctx, query).Scan(&parent)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("knowledge sqlite probe %s foreign key: %w", table, err)
	}
	return parent, true, nil
}

func copyKBSourcesIntoKnowledgeSources(ctx context.Context, tx *sql.Tx) error {
	// Read the missing ids before the insert. A finished membership backfill
	// does not scan again, so these rows must receive the same user and project
	// memberships a normal save would. owner_id is copied as stored.
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(owner_id, ''), COALESCE(project_path, '')
		FROM kb_sources k
		WHERE NOT EXISTS (SELECT 1 FROM knowledge_sources s WHERE s.id = k.id)`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite list kb_sources backfill: %w", err)
	}
	type copiedSource struct {
		id, owner, project string
	}
	copied := make([]copiedSource, 0)
	for rows.Next() {
		var item copiedSource
		if err := rows.Scan(&item.id, &item.owner, &item.project); err != nil {
			rows.Close()
			return fmt.Errorf("knowledge sqlite scan kb_sources backfill: %w", err)
		}
		copied = append(copied, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("knowledge sqlite iterate kb_sources backfill: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_sources (
		id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at)
		SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		 owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM kb_sources k
		WHERE NOT EXISTS (SELECT 1 FROM knowledge_sources s WHERE s.id = k.id)`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite copy kb_sources into knowledge_sources: %w", err)
	}
	if len(copied) == 0 {
		return nil
	}
	if err := ensureMembershipSchemaTx(ctx, tx); err != nil {
		return err
	}
	for _, item := range copied {
		if err := assignSourceMembershipsTx(ctx, tx, Source{ID: item.id, OwnerID: item.owner, ProjectPath: item.project}); err != nil {
			return err
		}
	}
	return nil
}

type bodyParentSpec struct {
	name    string
	create  string
	columns string
	indexes []string
}

func bodyParentTables() []bodyParentSpec {
	return []bodyParentSpec{
		{
			name: "kb_tables",
			create: `CREATE TABLE kb_tables_body_parent (
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
				FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
			)`,
			columns: "id, source_id, sheet_name, table_title, header_row_index, row_count, column_count, schema_json, created_at, updated_at",
			indexes: []string{
				`CREATE INDEX IF NOT EXISTS idx_kb_tables_source_sheet ON kb_tables(source_id, sheet_name)`,
				`CREATE INDEX IF NOT EXISTS idx_kb_tables_source_sheet_lower ON kb_tables(source_id, LOWER(sheet_name))`,
			},
		},
		{
			name: "kb_rows",
			create: `CREATE TABLE kb_rows_body_parent (
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
				FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
			)`,
			columns: "id, table_id, source_id, row_index, primary_key_text, row_text, row_json, embedding, created_at, updated_at",
			indexes: []string{
				`CREATE INDEX IF NOT EXISTS idx_kb_rows_table_row ON kb_rows(table_id, row_index)`,
				`CREATE INDEX IF NOT EXISTS idx_kb_rows_source_row ON kb_rows(source_id, row_index)`,
			},
		},
		{
			name: "kb_cards",
			create: `CREATE TABLE kb_cards_body_parent (
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
				FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE,
				FOREIGN KEY(row_id) REFERENCES kb_rows(id) ON DELETE SET NULL
			)`,
			columns: `id, source_id, row_id, origin_type, title, claim, summary, entities_json, topics_json, tags_json,
				project_path, owner_id, tenant_id, valid_at, invalid_at, confidence, importance, source_trust, embedding, created_at, updated_at`,
			indexes: []string{
				`CREATE INDEX IF NOT EXISTS idx_kb_cards_scope ON kb_cards(tenant_id, owner_id, project_path, updated_at)`,
			},
		},
		{
			name: "kb_facts",
			create: `CREATE TABLE kb_facts_body_parent (
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
				FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE,
				FOREIGN KEY(row_id) REFERENCES kb_rows(id) ON DELETE SET NULL
			)`,
			columns: `id, card_id, source_id, row_id, subject, predicate, object, normalized_object, value_type,
				number_value, date_value, bool_value, negated, valid_at, invalid_at, confidence, created_at`,
			indexes: []string{
				`CREATE INDEX IF NOT EXISTS idx_kb_facts_subject ON kb_facts(subject)`,
				`CREATE INDEX IF NOT EXISTS idx_kb_facts_object ON kb_facts(object)`,
			},
		},
	}
}

func rebuildBodyParentTable(ctx context.Context, tx *sql.Tx, spec bodyParentSpec) error {
	temp := spec.name + "_body_parent"
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+temp); err != nil {
		return fmt.Errorf("knowledge sqlite drop %s: %w", temp, err)
	}
	if _, err := tx.ExecContext(ctx, spec.create); err != nil {
		return fmt.Errorf("knowledge sqlite create %s: %w", temp, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+temp+` (`+spec.columns+`) SELECT `+spec.columns+` FROM `+spec.name); err != nil {
		return fmt.Errorf("knowledge sqlite copy %s: %w", spec.name, err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE `+spec.name); err != nil {
		return fmt.Errorf("knowledge sqlite replace %s: %w", spec.name, err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+temp+` RENAME TO `+spec.name); err != nil {
		return fmt.Errorf("knowledge sqlite rename %s: %w", spec.name, err)
	}
	for _, stmt := range spec.indexes {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("knowledge sqlite index %s: %w", spec.name, err)
		}
	}
	return nil
}
