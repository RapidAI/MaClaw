package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 28

type SQLiteStore struct {
	db        *sql.DB
	path      string
	backupDir string
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db, path: path, backupDir: filepath.Join(filepath.Dir(path), "backups")}
	if err := store.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if version < 1 {
		if err := s.applyMigrationV1(ctx); err != nil {
			return err
		}
	}
	if version < 2 {
		if err := s.applyMigrationV2(ctx); err != nil {
			return err
		}
	}
	if version < 3 {
		if err := s.applyMigrationV3(ctx); err != nil {
			return err
		}
	}
	if version < 4 {
		if err := s.applyMigrationV4(ctx); err != nil {
			return err
		}
	}
	if version < 5 {
		if err := s.applyMigrationV5(ctx); err != nil {
			return err
		}
	}
	if version < 6 {
		if err := s.applyMigrationV6(ctx); err != nil {
			return err
		}
	}
	if version < 7 {
		if err := s.applyMigrationV7(ctx); err != nil {
			return err
		}
	}
	if version < 8 {
		if err := s.applyMigrationV8(ctx); err != nil {
			return err
		}
	}
	if version < 9 {
		if err := s.applyMigrationV9(ctx); err != nil {
			return err
		}
	}
	if version < 10 {
		if err := s.applyMigrationV10(ctx); err != nil {
			return err
		}
	}
	if version < 11 {
		if err := s.applyMigrationV11(ctx); err != nil {
			return err
		}
	}
	if version < 12 {
		if err := s.applyMigrationV12(ctx); err != nil {
			return err
		}
	}
	if version < 13 {
		if err := s.applyMigrationV13(ctx); err != nil {
			return err
		}
	}
	if version < 14 {
		if err := s.applyMigrationV14(ctx); err != nil {
			return err
		}
	}
	if version < 15 {
		if err := s.applyMigrationV15(ctx); err != nil {
			return err
		}
	}
	if version < 16 {
		if err := s.applyMigrationV16(ctx); err != nil {
			return err
		}
	}
	if version < 17 {
		if err := s.applyMigrationV17(ctx); err != nil {
			return err
		}
	}
	if version < 18 {
		if err := s.applyMigrationV18(ctx); err != nil {
			return err
		}
	}
	if version < 19 {
		if err := s.applyMigrationV19(ctx); err != nil {
			return err
		}
	}
	if version < 20 {
		if err := s.applyMigrationV20(ctx); err != nil {
			return err
		}
	}
	if version < 21 {
		if err := s.applyMigrationV21(ctx); err != nil {
			return err
		}
	}
	if version < 22 {
		if err := s.applyMigrationV22(ctx); err != nil {
			return err
		}
	}
	if version < 23 {
		if err := s.applyMigrationV23(ctx); err != nil {
			return err
		}
	}
	if version < 24 {
		if err := s.applyMigrationV24(ctx); err != nil {
			return err
		}
	}
	if version < 25 {
		if err := s.applyMigrationV25(ctx); err != nil {
			return err
		}
	}
	if version < 26 {
		if err := s.applyMigrationV26(ctx); err != nil {
			return err
		}
	}
	if version < 27 {
		if err := s.applyMigrationV27(ctx); err != nil {
			return err
		}
	}
	if version < 28 {
		if err := s.applyMigrationV28(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) applyMigrationV1(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS datasets(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			domain TEXT NOT NULL,
			name TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			schema_version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, id),
			UNIQUE(tenant_id, domain, name)
		)`,
		`CREATE TABLE IF NOT EXISTS field_definitions(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			field_key TEXT NOT NULL,
			field_type TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			required INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			sensitive INTEGER NOT NULL DEFAULT 0,
			config_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant_id, dataset_id, field_key),
			FOREIGN KEY(tenant_id, dataset_id) REFERENCES datasets(tenant_id, id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS records(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			data_json TEXT NOT NULL,
			source_id TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, dataset_id, id),
			FOREIGN KEY(tenant_id, dataset_id) REFERENCES datasets(tenant_id, id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS record_field_index(
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			field_key TEXT NOT NULL,
			value_text TEXT,
			value_number REAL,
			value_time TEXT,
			value_hash TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(tenant_id, dataset_id, record_id, field_key, value_hash)
		)`,
		`CREATE TABLE IF NOT EXISTS record_tags(
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			tag_norm TEXT NOT NULL,
			PRIMARY KEY(tenant_id, dataset_id, record_id, tag_norm)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS record_fts USING fts5(tenant_id UNINDEXED, dataset_id UNINDEXED, record_id UNINDEXED, text)`,
		`CREATE INDEX IF NOT EXISTS idx_datasets_tenant_domain ON datasets(tenant_id, domain, name)`,
		`CREATE INDEX IF NOT EXISTS idx_records_scope ON records(tenant_id, dataset_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_field_text ON record_field_index(tenant_id, dataset_id, field_key, value_text)`,
		`CREATE INDEX IF NOT EXISTS idx_field_number ON record_field_index(tenant_id, dataset_id, field_key, value_number)`,
		`CREATE INDEX IF NOT EXISTS idx_field_time ON record_field_index(tenant_id, dataset_id, field_key, value_time)`,
		`CREATE INDEX IF NOT EXISTS idx_tags_lookup ON record_tags(tenant_id, dataset_id, tag_norm, record_id)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 1, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV2(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_proposals(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			status TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			suggested_json TEXT NOT NULL DEFAULT '[]',
			ignored_json TEXT NOT NULL DEFAULT '[]',
			impact_json TEXT NOT NULL DEFAULT '{}',
			created_by TEXT NOT NULL DEFAULT '',
			applied_by TEXT NOT NULL DEFAULT '',
			applied_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, dataset_id, id),
			FOREIGN KEY(tenant_id, dataset_id) REFERENCES datasets(tenant_id, id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_schema_proposals_scope ON schema_proposals(tenant_id, dataset_id, status, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 2, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV3(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS audit_logs(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			dataset_id TEXT NOT NULL DEFAULT '',
			target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_logs(tenant_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_dataset_time ON audit_logs(tenant_id, dataset_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_action_time ON audit_logs(tenant_id, action, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 3, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV4(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS data_events(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			source TEXT NOT NULL,
			event_type TEXT NOT NULL,
			operation TEXT NOT NULL,
			business_action_id TEXT NOT NULL DEFAULT '',
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL,
			result_status TEXT NOT NULL,
			created_by TEXT NOT NULL DEFAULT '',
			applied_at TEXT NOT NULL,
			UNIQUE(tenant_id, idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_data_events_tenant_time ON data_events(tenant_id, applied_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_data_events_dataset_time ON data_events(tenant_id, dataset_id, applied_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 4, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV5(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS record_revisions(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			action TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			tags_json TEXT NOT NULL DEFAULT '[]',
			data_json TEXT NOT NULL DEFAULT '{}',
			source_id TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_record_revisions_record_time ON record_revisions(tenant_id, dataset_id, record_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_record_revisions_dataset_time ON record_revisions(tenant_id, dataset_id, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 5, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV6(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS quality_runs(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			checks_json TEXT NOT NULL DEFAULT '[]',
			scanned INTEGER NOT NULL DEFAULT 0,
			valid INTEGER NOT NULL DEFAULT 1,
			issue_count INTEGER NOT NULL DEFAULT 0,
			issues_json TEXT NOT NULL DEFAULT '[]',
			limit_value INTEGER NOT NULL DEFAULT 0,
			include_warnings INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_quality_runs_dataset_time ON quality_runs(tenant_id, dataset_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_quality_runs_tenant_time ON quality_runs(tenant_id, created_at, id)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 6, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV7(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS import_jobs(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			status TEXT NOT NULL,
			dry_run INTEGER NOT NULL DEFAULT 0,
			total INTEGER NOT NULL DEFAULT 0,
			imported INTEGER NOT NULL DEFAULT 0,
			valid INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			result_json TEXT NOT NULL DEFAULT '{}',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_import_jobs_dataset_time ON import_jobs(tenant_id, dataset_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_import_jobs_tenant_time ON import_jobs(tenant_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_import_jobs_status ON import_jobs(tenant_id, status, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 7, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) SchemaVersion(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	var version int
	if err := row.Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *SQLiteStore) applyMigrationV8(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS export_jobs(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			format TEXT NOT NULL,
			status TEXT NOT NULL,
			total INTEGER NOT NULL DEFAULT 0,
			bytes INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			result_text TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_export_jobs_dataset_time ON export_jobs(tenant_id, dataset_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_export_jobs_tenant_time ON export_jobs(tenant_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_export_jobs_status ON export_jobs(tenant_id, status, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 8, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV9(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS operation_plans(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL DEFAULT '',
			operation TEXT NOT NULL,
			status TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			risk_level TEXT NOT NULL DEFAULT '',
			request_json TEXT NOT NULL DEFAULT '{}',
			preview_json TEXT NOT NULL DEFAULT '{}',
			created_by TEXT NOT NULL DEFAULT '',
			reviewed_by TEXT NOT NULL DEFAULT '',
			applied_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			reviewed_at TEXT NOT NULL DEFAULT '',
			applied_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operation_plans_tenant_time ON operation_plans(tenant_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_operation_plans_status ON operation_plans(tenant_id, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_operation_plans_dataset ON operation_plans(tenant_id, dataset_id, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 9, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV10(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS record_approvals(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			status TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			request_json TEXT NOT NULL DEFAULT '{}',
			decision TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			reviewed_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			reviewed_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_record_time ON record_approvals(tenant_id, dataset_id, record_id, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_status ON record_approvals(tenant_id, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_kind ON record_approvals(tenant_id, kind, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 10, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV11(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`ALTER TABLE record_approvals ADD COLUMN assigned_to TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE record_approvals ADD COLUMN due_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE record_approvals ADD COLUMN priority TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_assignee ON record_approvals(tenant_id, assigned_to, status, due_at)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_due ON record_approvals(tenant_id, status, due_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil && !isDuplicateColumnError(err) {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 11, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV12(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`ALTER TABLE data_events ADD COLUMN business_action_id TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_data_events_business_action ON data_events(tenant_id, business_action_id, applied_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil && !isDuplicateColumnError(err) {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 12, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV13(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS external_connectors(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			domain TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			auth_type TEXT NOT NULL DEFAULT '',
			token_ref TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			subscribed_actions_json TEXT NOT NULL DEFAULT '[]',
			config_json TEXT NOT NULL DEFAULT '{}',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_external_connectors_domain ON external_connectors(tenant_id, domain, enabled, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_external_connectors_kind ON external_connectors(tenant_id, kind, enabled, updated_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 13, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV14(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS data_event_dead_letters(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			status TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL DEFAULT '',
			business_action_id TEXT NOT NULL DEFAULT '',
			dataset_id TEXT NOT NULL DEFAULT '',
			record_id TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_by TEXT NOT NULL DEFAULT '',
			resolved_by TEXT NOT NULL DEFAULT '',
			resolution TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			resolved_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dead_letters_tenant_status ON data_event_dead_letters(tenant_id, status, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_dead_letters_business_action ON data_event_dead_letters(tenant_id, business_action_id, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_dead_letters_source ON data_event_dead_letters(tenant_id, source, status, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 14, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV15(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS api_key_policies(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'data_user',
			key_hash TEXT NOT NULL,
			key_prefix TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			allowed_domains_json TEXT NOT NULL DEFAULT '[]',
			allowed_datasets_json TEXT NOT NULL DEFAULT '[]',
			allowed_actions_json TEXT NOT NULL DEFAULT '[]',
			allowed_views_json TEXT NOT NULL DEFAULT '[]',
			allowed_reports_json TEXT NOT NULL DEFAULT '[]',
			allowed_dashboards_json TEXT NOT NULL DEFAULT '[]',
			allow_raw_data INTEGER NOT NULL DEFAULT 0,
			allow_sensitive INTEGER NOT NULL DEFAULT 0,
			allow_admin INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL DEFAULT '',
			last_used_at TEXT NOT NULL DEFAULT '',
			last_used_ip TEXT NOT NULL DEFAULT '',
			last_used_user_agent TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, id),
			UNIQUE(key_hash)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_policies_tenant_enabled ON api_key_policies(tenant_id, enabled, updated_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 15, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV16(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`ALTER TABLE api_key_policies ADD COLUMN last_used_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE api_key_policies ADD COLUMN last_used_ip TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE api_key_policies ADD COLUMN last_used_user_agent TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_policies_last_used ON api_key_policies(tenant_id, last_used_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 16, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV17(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`ALTER TABLE api_key_policies ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_policies_expires ON api_key_policies(tenant_id, expires_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 17, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV18(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(record_field_index)`)
	if err != nil {
		return err
	}
	hasValueHash := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "value_hash" {
			hasValueHash = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`DROP INDEX IF EXISTS idx_field_text`,
		`DROP INDEX IF EXISTS idx_field_number`,
		`DROP INDEX IF EXISTS idx_field_time`,
		`DROP TABLE IF EXISTS record_field_index_old`,
		`ALTER TABLE record_field_index RENAME TO record_field_index_old`,
		`CREATE TABLE record_field_index(
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			field_key TEXT NOT NULL,
			value_text TEXT,
			value_number REAL,
			value_time TEXT,
			value_hash TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(tenant_id, dataset_id, record_id, field_key, value_hash)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	copyStmt := `INSERT OR IGNORE INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text, value_number, value_time, value_hash)
		SELECT tenant_id, dataset_id, record_id, field_key, value_text, value_number, value_time,
			COALESCE(value_text, '') || char(31) || COALESCE(CAST(value_number AS TEXT), '') || char(31) || COALESCE(value_time, '')
		FROM record_field_index_old`
	if hasValueHash {
		copyStmt = `INSERT OR IGNORE INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text, value_number, value_time, value_hash)
			SELECT tenant_id, dataset_id, record_id, field_key, value_text, value_number, value_time, COALESCE(value_hash, '')
			FROM record_field_index_old`
	}
	if _, err := tx.ExecContext(ctx, copyStmt); err != nil {
		return err
	}
	hasRecords, err := sqliteTableExists(ctx, tx, "records")
	if err != nil {
		return err
	}
	if hasRecords {
		if err := rebuildRecordFieldIndexesFromRecords(ctx, tx); err != nil {
			return err
		}
	}
	stmts = []string{
		`DROP TABLE record_field_index_old`,
		`CREATE INDEX IF NOT EXISTS idx_field_text ON record_field_index(tenant_id, dataset_id, field_key, value_text)`,
		`CREATE INDEX IF NOT EXISTS idx_field_number ON record_field_index(tenant_id, dataset_id, field_key, value_number)`,
		`CREATE INDEX IF NOT EXISTS idx_field_time ON record_field_index(tenant_id, dataset_id, field_key, value_time)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 18, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV19(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS admin_users(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			username TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_login_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, username),
			UNIQUE(id)
		)`,
		`CREATE TABLE IF NOT EXISTS admin_sessions(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			username TEXT NOT NULL,
			role TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_users_enabled ON admin_users(enabled, tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires ON admin_sessions(expires_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 19, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV20(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	hasFailureCount, err := sqliteColumnExists(ctx, tx, "admin_users", "login_failure_count")
	if err != nil {
		return err
	}
	if !hasFailureCount {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN login_failure_count INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	hasLockedUntil, err := sqliteColumnExists(ctx, tx, "admin_users", "login_locked_until")
	if err != nil {
		return err
	}
	if !hasLockedUntil {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN login_locked_until TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_admin_users_lockout ON admin_users(login_locked_until, tenant_id, username)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 20, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV21(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, column := range []struct{ name, ddl string }{
		{"admin_scope", `ALTER TABLE admin_users ADD COLUMN admin_scope TEXT NOT NULL DEFAULT 'tenant'`},
	} {
		exists, err := sqliteColumnExists(ctx, tx, "admin_users", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
		}
	}
	exists, err := sqliteColumnExists(ctx, tx, "admin_sessions", "admin_scope")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE admin_sessions ADD COLUMN admin_scope TEXT NOT NULL DEFAULT 'tenant'`); err != nil {
			return err
		}
	}
	var globalAdminCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users WHERE admin_scope = 'global' AND enabled = 1 AND lower(role) = 'data_admin'`).Scan(&globalAdminCount); err != nil {
		return err
	}
	if globalAdminCount == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE admin_users
			SET admin_scope = 'global'
			WHERE id = (
				SELECT id FROM admin_users
				WHERE enabled = 1 AND lower(role) = 'data_admin'
				ORDER BY created_at, tenant_id, username
				LIMIT 1
			)`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_sessions
		SET admin_scope = 'global'
		WHERE user_id IN (SELECT id FROM admin_users WHERE admin_scope = 'global')`); err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS data_tenants(
			id TEXT PRIMARY KEY,
			hub_tenant_id TEXT NOT NULL DEFAULT '',
			slug TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			primary_domain TEXT NOT NULL DEFAULT '',
			domains_json TEXT NOT NULL DEFAULT '[]',
			virtual_mail_domain TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'hub',
			synced_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_users_scope_tenant ON admin_users(admin_scope, tenant_id, username)`,
		`CREATE INDEX IF NOT EXISTS idx_data_tenants_status ON data_tenants(status, id)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO data_tenants(id, hub_tenant_id, slug, name, status, source, synced_at, updated_at) VALUES('default', 'default', 'default', 'Default', 'active', 'local', ?, ?)`, formatTime(time.Now().UTC()), formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 21, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV22(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hub_registration(
			id TEXT PRIMARY KEY,
			hub_base_url TEXT NOT NULL DEFAULT '',
			platform_id TEXT NOT NULL DEFAULT '',
			platform_name TEXT NOT NULL DEFAULT '',
			callback_base_url TEXT NOT NULL DEFAULT '',
			virtual_mail_domain TEXT NOT NULL DEFAULT '',
			public_key_pem TEXT NOT NULL DEFAULT '',
			private_key_pem TEXT NOT NULL DEFAULT '',
			callback_secret TEXT NOT NULL DEFAULT '',
			registered INTEGER NOT NULL DEFAULT 0,
			last_registered_at TEXT NOT NULL DEFAULT '',
			last_synced_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 22, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV23(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	exists, err := sqliteColumnExists(ctx, tx, "data_tenants", "virtual_mail_domain")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE data_tenants ADD COLUMN virtual_mail_domain TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 23, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV24(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	hasRecordApprovals, err := sqliteTableExists(ctx, tx, "record_approvals")
	if err != nil {
		return err
	}
	if !hasRecordApprovals {
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS record_approvals(
				id TEXT PRIMARY KEY,
				tenant_id TEXT NOT NULL,
				dataset_id TEXT NOT NULL,
				record_id TEXT NOT NULL,
				status TEXT NOT NULL,
				kind TEXT NOT NULL DEFAULT '',
				summary TEXT NOT NULL DEFAULT '',
				request_json TEXT NOT NULL DEFAULT '{}',
				workflow_skill_id TEXT NOT NULL DEFAULT '',
				workflow_version TEXT NOT NULL DEFAULT '',
				workflow_instance_id TEXT NOT NULL DEFAULT '',
				workflow_node_id TEXT NOT NULL DEFAULT '',
				workflow_decision_id TEXT NOT NULL DEFAULT '',
				business_status TEXT NOT NULL DEFAULT '',
				result_status TEXT NOT NULL DEFAULT '',
				decision TEXT NOT NULL DEFAULT '',
				reason TEXT NOT NULL DEFAULT '',
				created_by TEXT NOT NULL DEFAULT '',
				reviewed_by TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				reviewed_at TEXT NOT NULL DEFAULT '',
				updated_at TEXT NOT NULL,
				assigned_to TEXT NOT NULL DEFAULT '',
				due_at TEXT NOT NULL DEFAULT '',
				priority TEXT NOT NULL DEFAULT '',
				result_payload_json TEXT NOT NULL DEFAULT '{}',
				outputs_json TEXT NOT NULL DEFAULT '[]',
				artifacts_json TEXT NOT NULL DEFAULT '[]'
			)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_record_time ON record_approvals(tenant_id, dataset_id, record_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_status ON record_approvals(tenant_id, status, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_kind ON record_approvals(tenant_id, kind, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_assignee ON record_approvals(tenant_id, assigned_to, status, due_at)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_due ON record_approvals(tenant_id, status, due_at)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_workflow_instance ON record_approvals(tenant_id, workflow_instance_id, status, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_workflow_skill ON record_approvals(tenant_id, workflow_skill_id, status, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_record_approvals_business_status ON record_approvals(tenant_id, business_status, result_status, created_at)`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 24, formatTime(time.Now().UTC())); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	}
	for _, column := range []struct{ name, ddl string }{
		{"workflow_skill_id", `ALTER TABLE record_approvals ADD COLUMN workflow_skill_id TEXT NOT NULL DEFAULT ''`},
		{"workflow_version", `ALTER TABLE record_approvals ADD COLUMN workflow_version TEXT NOT NULL DEFAULT ''`},
		{"workflow_instance_id", `ALTER TABLE record_approvals ADD COLUMN workflow_instance_id TEXT NOT NULL DEFAULT ''`},
		{"workflow_node_id", `ALTER TABLE record_approvals ADD COLUMN workflow_node_id TEXT NOT NULL DEFAULT ''`},
		{"workflow_decision_id", `ALTER TABLE record_approvals ADD COLUMN workflow_decision_id TEXT NOT NULL DEFAULT ''`},
		{"business_status", `ALTER TABLE record_approvals ADD COLUMN business_status TEXT NOT NULL DEFAULT ''`},
		{"result_status", `ALTER TABLE record_approvals ADD COLUMN result_status TEXT NOT NULL DEFAULT ''`},
		{"result_payload_json", `ALTER TABLE record_approvals ADD COLUMN result_payload_json TEXT NOT NULL DEFAULT '{}'`},
		{"outputs_json", `ALTER TABLE record_approvals ADD COLUMN outputs_json TEXT NOT NULL DEFAULT '[]'`},
		{"artifacts_json", `ALTER TABLE record_approvals ADD COLUMN artifacts_json TEXT NOT NULL DEFAULT '[]'`},
	} {
		exists, err := sqliteColumnExists(ctx, tx, "record_approvals", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
		}
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_workflow_instance ON record_approvals(tenant_id, workflow_instance_id, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_workflow_skill ON record_approvals(tenant_id, workflow_skill_id, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_business_status ON record_approvals(tenant_id, business_status, result_status, created_at)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 24, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV25(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS app_installations(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			app_id TEXT NOT NULL,
			blueprint_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'installed',
			source TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			installed_by TEXT NOT NULL DEFAULT '',
			installed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, id),
			UNIQUE(tenant_id, app_id)
		)`,
		`CREATE TABLE IF NOT EXISTS app_role_bindings(
			tenant_id TEXT NOT NULL,
			app_installation_id TEXT NOT NULL,
			app_id TEXT NOT NULL,
			blueprint_id TEXT NOT NULL DEFAULT '',
			object_role TEXT NOT NULL,
			domain TEXT NOT NULL DEFAULT '',
			dataset_id TEXT NOT NULL DEFAULT '',
			template_id TEXT NOT NULL DEFAULT '',
			required INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(tenant_id, app_installation_id, object_role),
			FOREIGN KEY(tenant_id, app_installation_id) REFERENCES app_installations(tenant_id, id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_app_installations_tenant_status ON app_installations(tenant_id, status, updated_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_installations_blueprint ON app_installations(tenant_id, blueprint_id, updated_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_role_bindings_role ON app_role_bindings(tenant_id, app_id, object_role)`,
		`CREATE INDEX IF NOT EXISTS idx_app_role_bindings_dataset ON app_role_bindings(tenant_id, dataset_id)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 25, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV26(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, column := range []struct{ name, ddl string }{
		{"app_id", `ALTER TABLE record_approvals ADD COLUMN app_id TEXT NOT NULL DEFAULT ''`},
		{"blueprint_id", `ALTER TABLE record_approvals ADD COLUMN blueprint_id TEXT NOT NULL DEFAULT ''`},
		{"object_role", `ALTER TABLE record_approvals ADD COLUMN object_role TEXT NOT NULL DEFAULT ''`},
	} {
		exists, err := sqliteColumnExists(ctx, tx, "record_approvals", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
		}
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_app_role ON record_approvals(tenant_id, app_id, object_role, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_blueprint_role ON record_approvals(tenant_id, blueprint_id, object_role, status, created_at)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 26, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) applyMigrationV27(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	exists, err := sqliteColumnExists(ctx, tx, "record_approvals", "detail_url")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE record_approvals ADD COLUMN detail_url TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 27, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
func (s *SQLiteStore) applyMigrationV28(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, column := range []struct{ name, ddl string }{
		{"approval_workflow_id", `ALTER TABLE record_approvals ADD COLUMN approval_workflow_id TEXT NOT NULL DEFAULT ''`},
		{"trigger_event", `ALTER TABLE record_approvals ADD COLUMN trigger_event TEXT NOT NULL DEFAULT ''`},
		{"submitted_by", `ALTER TABLE record_approvals ADD COLUMN submitted_by TEXT NOT NULL DEFAULT ''`},
		{"current_assignee", `ALTER TABLE record_approvals ADD COLUMN current_assignee TEXT NOT NULL DEFAULT ''`},
		{"current_assignee_type", `ALTER TABLE record_approvals ADD COLUMN current_assignee_type TEXT NOT NULL DEFAULT ''`},
		{"from_status", `ALTER TABLE record_approvals ADD COLUMN from_status TEXT NOT NULL DEFAULT ''`},
		{"to_status", `ALTER TABLE record_approvals ADD COLUMN to_status TEXT NOT NULL DEFAULT ''`},
	} {
		exists, err := sqliteColumnExists(ctx, tx, "record_approvals", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
		}
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_workflow_event ON record_approvals(tenant_id, approval_workflow_id, trigger_event, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_current_assignee ON record_approvals(tenant_id, current_assignee, status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_record_approvals_status_transition ON record_approvals(tenant_id, from_status, to_status, status, created_at)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, 28, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
func sqliteTableExists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func sqliteColumnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	if !validSQLiteIdentifier(table) || !validSQLiteIdentifier(column) {
		return false, fmt.Errorf("%w: invalid sqlite identifier", ErrInvalidInput)
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func validSQLiteIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func rebuildRecordFieldIndexesFromRecords(ctx context.Context, tx *sql.Tx) error {
	type indexedRecord struct {
		ID        string
		TenantID  string
		DatasetID string
		DataJSON  string
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, tenant_id, dataset_id, data_json FROM records`)
	if err != nil {
		return err
	}
	records := []indexedRecord{}
	for rows.Next() {
		var record indexedRecord
		if err := rows.Scan(&record.ID, &record.TenantID, &record.DatasetID, &record.DataJSON); err != nil {
			_ = rows.Close()
			return err
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, record := range records {
		var data map[string]any
		if err := json.Unmarshal([]byte(record.DataJSON), &data); err != nil {
			continue
		}
		if data == nil {
			data = map[string]any{}
		}
		if len(recordIndexValues(data)) > maxRecordIndexKeys {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM record_field_index WHERE tenant_id = ? AND dataset_id = ? AND record_id = ?`, record.TenantID, record.DatasetID, record.ID); err != nil {
			return err
		}
		if err := writeRecordFieldIndexes(ctx, tx, Record{ID: record.ID, TenantID: record.TenantID, DatasetID: record.DatasetID, Data: data}); err != nil {
			if errors.Is(err, ErrInvalidInput) {
				continue
			}
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) CreateDataset(ctx context.Context, dataset Dataset) (*Dataset, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO datasets(id, tenant_id, domain, name, title, description, schema_version, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, dataset.ID, dataset.TenantID, dataset.Domain, dataset.Name, dataset.Title, dataset.Description, dataset.SchemaVersion, formatTime(dataset.CreatedAt), formatTime(dataset.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return &dataset, nil
}

func (s *SQLiteStore) ListDatasets(ctx context.Context, tenantID string) ([]Dataset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, domain, name, title, description, schema_version, created_at, updated_at FROM datasets WHERE tenant_id = ? ORDER BY domain, name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dataset{}
	for rows.Next() {
		dataset, err := scanDataset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, dataset)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetDataset(ctx context.Context, tenantID, datasetID string) (*Dataset, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, domain, name, title, description, schema_version, created_at, updated_at FROM datasets WHERE tenant_id = ? AND id = ?`, tenantID, datasetID)
	dataset, err := scanDataset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDatasetNotFound
	}
	if err != nil {
		return nil, err
	}
	return &dataset, nil
}

func (s *SQLiteStore) UpdateDataset(ctx context.Context, tenantID, datasetID string, in UpdateDatasetInput, now time.Time) (*Dataset, error) {
	dataset, err := s.GetDataset(ctx, tenantID, datasetID)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		dataset.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		dataset.Description = strings.TrimSpace(*in.Description)
	}
	dataset.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `UPDATE datasets SET title = ?, description = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`, dataset.Title, dataset.Description, formatTime(dataset.UpdatedAt), tenantID, datasetID)
	if err != nil {
		return nil, err
	}
	return dataset, nil
}

func (s *SQLiteStore) DeleteDataset(ctx context.Context, tenantID, datasetID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM datasets WHERE tenant_id = ? AND id = ?`, tenantID, datasetID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrDatasetNotFound
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM record_fts WHERE tenant_id = ? AND dataset_id = ?`, tenantID, datasetID)
	return nil
}

func scanDataset(scanner interface{ Scan(dest ...any) error }) (Dataset, error) {
	var dataset Dataset
	var createdAt, updatedAt string
	if err := scanner.Scan(&dataset.ID, &dataset.TenantID, &dataset.Domain, &dataset.Name, &dataset.Title, &dataset.Description, &dataset.SchemaVersion, &createdAt, &updatedAt); err != nil {
		return Dataset{}, err
	}
	dataset.CreatedAt = parseTime(createdAt)
	dataset.UpdatedAt = parseTime(updatedAt)
	return dataset, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	return parsed
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func intBool(v int) bool { return v != 0 }

func jsonString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func ftsQuery(q string) string {
	fields := strings.Fields(q)
	for i, field := range fields {
		fields[i] = strings.ReplaceAll(field, `"`, `""`) + "*"
	}
	return strings.Join(fields, " AND ")
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func timeValue(v any) (string, bool) {
	text, ok := v.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.Format(time.RFC3339Nano), true
		}
	}
	return "", false
}
