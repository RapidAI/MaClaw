package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	_ "modernc.org/sqlite"
)

type ImportProgressFunc func(DirectoryImportResult)

type SQLiteStore struct {
	db             *sql.DB
	distiller      CardDistiller
	importProgress ImportProgressFunc
	embedder       embedding.Embedder
	imageAssets    *ImageAssetManager
	imageDescriber ImageDescriber
	imageDescSem   chan struct{} // semaphore for concurrent image description calls
}

// SetEmbedder sets the embedding model for vector search.
// When set, card embeddings are generated on insert and used for semantic search.
func (s *SQLiteStore) SetEmbedder(emb embedding.Embedder) {
	s.embedder = emb
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("knowledge sqlite: db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("knowledge sqlite: create parent: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge sqlite open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := applyPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := createTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) SetCardDistiller(distiller CardDistiller) {
	if s != nil {
		s.distiller = distiller
	}
}

func (s *SQLiteStore) SetImportProgressCallback(callback ImportProgressFunc) {
	if s != nil {
		s.importProgress = callback
	}
}

// SetImageAssetManager sets the image asset manager for storing extracted images.
func (s *SQLiteStore) SetImageAssetManager(mgr *ImageAssetManager) {
	if s != nil {
		s.imageAssets = mgr
	}
}

// SetImageDescriber sets the image description provider (Vision LLM + OCR).
func (s *SQLiteStore) SetImageDescriber(describer ImageDescriber) {
	if s != nil {
		s.imageDescriber = describer
		// Initialize concurrency semaphore for image description (max 2 concurrent).
		if s.imageDescSem == nil {
			s.imageDescSem = make(chan struct{}, 2)
		}
	}
}

// ImageAssets returns the configured image asset manager (may be nil).
func (s *SQLiteStore) ImageAssets() *ImageAssetManager {
	if s == nil {
		return nil
	}
	return s.imageAssets
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func applyPragmas(db *sql.DB) error {
	stmts := []string{
		`PRAGMA busy_timeout=5000`,
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA foreign_keys=ON`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			if stmt == `PRAGMA journal_mode=WAL` && IsSQLiteLockedError(err) {
				continue
			}
			return fmt.Errorf("knowledge sqlite pragma %q: %w", stmt, err)
		}
	}
	return nil
}

func IsSQLiteLockedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") || strings.Contains(message, "sqlite_busy")
}

func isSQLiteLockedError(err error) bool {
	return IsSQLiteLockedError(err)
}

func createTables(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS knowledge_sources (
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
		`CREATE TABLE IF NOT EXISTS knowledge_source_versions (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			kind TEXT,
			uri TEXT,
			canonical_uri TEXT,
			title TEXT,
			content_hash TEXT,
			status TEXT,
			reason TEXT,
			fetched_at TEXT,
			node_count INTEGER DEFAULT 0,
			card_count INTEGER DEFAULT 0,
			fact_count INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_source_labels (
			source_id TEXT NOT NULL,
			label TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(source_id, label),
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_source_links (
			source_id TEXT NOT NULL,
			related_source_id TEXT NOT NULL,
			relation TEXT NOT NULL,
			score REAL DEFAULT 0,
			terms_json TEXT,
			evidence_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(source_id, related_source_id, relation),
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(related_source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_source_link_events (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			related_source_id TEXT NOT NULL,
			relation TEXT NOT NULL,
			action TEXT NOT NULL,
			score REAL DEFAULT 0,
			terms_json TEXT,
			evidence_json TEXT,
			note TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(related_source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS document_nodes (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			parent_id TEXT,
			type TEXT NOT NULL,
			title TEXT,
			text TEXT,
			level INTEGER DEFAULT 0,
			page INTEGER DEFAULT 0,
			sheet_name TEXT,
			row_range TEXT,
			col_range TEXT,
			xpath TEXT,
			offset INTEGER DEFAULT 0,
			metadata_json TEXT,
			token_count INTEGER DEFAULT 0,
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS document_nodes_fts USING fts5(node_id UNINDEXED, title, text)`,
		`CREATE TABLE IF NOT EXISTS knowledge_cards (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			node_id TEXT,
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
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_cards_fts USING fts5(card_id UNINDEXED, title, claim, summary)`,
		`CREATE TABLE IF NOT EXISTS knowledge_facts (
			id TEXT PRIMARY KEY,
			card_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			negated INTEGER DEFAULT 0,
			valid_at TEXT,
			invalid_at TEXT,
			confidence REAL DEFAULT 0.5,
			FOREIGN KEY(card_id) REFERENCES knowledge_cards(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_facts_fts USING fts5(fact_id UNINDEXED, subject, predicate, object)`,
		`CREATE TABLE IF NOT EXISTS knowledge_card_suppressions (
			card_id TEXT PRIMARY KEY,
			reason TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY(card_id) REFERENCES knowledge_cards(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_url_domain_policies (
			domain TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			reason TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object)
			SELECT f.id, f.subject, f.predicate, f.object FROM knowledge_facts f
			WHERE NOT EXISTS (SELECT 1 FROM knowledge_facts_fts WHERE fact_id = f.id)`,
		`CREATE TABLE IF NOT EXISTS knowledge_import_batches (
			id TEXT PRIMARY KEY,
			root_path TEXT NOT NULL,
			owner_id TEXT,
			tenant_id TEXT,
			project_path TEXT,
			topic_hint TEXT,
			recursive INTEGER DEFAULT 1,
			include_exts_json TEXT,
			exclude_globs_json TEXT,
			max_file_bytes INTEGER DEFAULT 0,
			status TEXT NOT NULL,
			total_files INTEGER DEFAULT 0,
			queued_files INTEGER DEFAULT 0,
			imported_files INTEGER DEFAULT 0,
			skipped_files INTEGER DEFAULT 0,
			failed_files INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS knowledge_import_items (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL,
			source_id TEXT,
			file_path TEXT NOT NULL,
			relative_path TEXT,
			file_hash TEXT,
			file_size INTEGER DEFAULT 0,
			kind TEXT,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(batch_id) REFERENCES knowledge_import_batches(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES knowledge_sources(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sources_scope ON knowledge_sources(tenant_id, owner_id, project_path, kind, status)`,
		`CREATE INDEX IF NOT EXISTS idx_sources_hash ON knowledge_sources(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_sources_batch ON knowledge_sources(batch_id, relative_path)`,
		`CREATE INDEX IF NOT EXISTS idx_source_versions_source ON knowledge_source_versions(source_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_source_labels_label ON knowledge_source_labels(label, source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_source_links_related ON knowledge_source_links(related_source_id, relation, score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_source_link_events_source ON knowledge_source_link_events(source_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_source_link_events_related ON knowledge_source_link_events(related_source_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cards_scope ON knowledge_cards(tenant_id, owner_id, project_path, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_facts_subject ON knowledge_facts(subject)`,
		`CREATE INDEX IF NOT EXISTS idx_facts_object ON knowledge_facts(object)`,
		`CREATE INDEX IF NOT EXISTS idx_import_items_batch ON knowledge_import_items(batch_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_import_items_hash ON knowledge_import_items(file_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_url_domain_policies_action ON knowledge_url_domain_policies(action)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("knowledge sqlite schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) SaveSource(ctx context.Context, source Source) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	source = normalizeSource(source)
	_, err := s.db.ExecContext(ctx, insertSourceSQL,
		source.ID, source.Kind, source.URI, source.CanonicalURI, source.Title, source.Author, source.SiteName,
		formatTime(source.PublishedAt), formatTime(source.FetchedAt), source.ContentHash, source.OwnerID, source.TenantID,
		source.ProjectPath, source.TopicHint, source.SourceTrust, source.BatchID, source.RelativePath, source.Status,
		source.ErrorMessage, formatTime(source.CreatedAt), formatTime(source.UpdatedAt),
	)
	return err
}

func (s *SQLiteStore) SaveDocumentNode(ctx context.Context, node DocumentNode) error {
	if node.ID == "" {
		node.ID = NewID("kdn")
	}
	meta, _ := json.Marshal(node.Metadata)
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO document_nodes
		(id, source_id, parent_id, type, title, text, level, page, sheet_name, row_range, col_range, xpath, offset, metadata_json, token_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.SourceID, node.ParentID, node.Type, node.Title, node.Text, node.Level, node.Page, node.SheetName,
		node.RowRange, node.ColRange, node.XPath, node.Offset, string(meta), node.TokenCount,
	)
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM document_nodes_fts WHERE node_id = ?`, node.ID)
	_, _ = s.db.ExecContext(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`, node.ID, segmentTextForFTS(node.Title), segmentTextForFTS(node.Text))
	return nil
}

func (s *SQLiteStore) SaveCard(ctx context.Context, card Card) error {
	if card.ID == "" {
		card.ID = NewID("kcard")
	}
	now := time.Now().UTC()
	if card.CreatedAt.IsZero() {
		card.CreatedAt = now
	}
	if card.UpdatedAt.IsZero() {
		card.UpdatedAt = now
	}
	if card.Confidence <= 0 {
		card.Confidence = 0.5
	}
	if card.Importance <= 0 {
		card.Importance = 1
	}
	if card.SourceTrust <= 0 {
		card.SourceTrust = 0.5
	}
	entitiesJSON, _ := json.Marshal(card.Entities)
	topicsJSON, _ := json.Marshal(card.Topics)
	tagsJSON, _ := json.Marshal(card.Tags)
	var embBlob interface{}
	if len(card.Embedding) > 0 {
		embBlob = float32SliceToBytes(card.Embedding)
	}
	_, err := s.db.ExecContext(ctx, insertCardSQL,
		card.ID, card.SourceID, nullableString(card.NodeID), card.Title, card.Claim, card.Summary, string(entitiesJSON), string(topicsJSON), string(tagsJSON),
		card.ProjectPath, card.OwnerID, card.TenantID, formatTime(card.ValidAt), formatTime(card.InvalidAt), card.Confidence, card.Importance,
		card.SourceTrust, embBlob, formatTime(card.CreatedAt), formatTime(card.UpdatedAt),
	)
	if err != nil {
		return err
	}
	ftsSummary := cardFTSSummary(card)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM knowledge_cards_fts WHERE card_id = ?`, card.ID)
	_, _ = s.db.ExecContext(ctx, `INSERT INTO knowledge_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`, card.ID, segmentTextForFTS(card.Title), segmentTextForFTS(card.Claim), segmentTextForFTS(ftsSummary))
	return nil
}

func (s *SQLiteStore) DistillAndSaveCards(ctx context.Context, tx *sql.Tx, source Source, nodes []DocumentNode) (Source, error) {
	return s.DistillAndSaveCardsWithMode(ctx, tx, source, nodes, DistillModeAuto)
}

func (s *SQLiteStore) DistillAndSaveCardsWithMode(ctx context.Context, tx *sql.Tx, source Source, nodes []DocumentNode, mode string) (Source, error) {
	cards := BuildCardsForNodes(source, nodes)
	if s != nil && s.distiller != nil && shouldUseLLMForMode(mode, source, nodes) {
		if llmCards, err := s.distiller.DistillCards(ctx, source, nodes); err == nil && len(llmCards) > 0 {
			cards = NormalizeDistilledCards(source, llmCards)
		}
	}
	if len(cards) == 0 {
		return source, nil
	}
	// Generate embeddings for cards if embedder is available
	if s != nil && s.embedder != nil && !embedding.IsNoop(s.embedder) {
		texts := make([]string, len(cards))
		for i, card := range cards {
			texts[i] = cardEmbeddingText(card)
		}
		if vectors, err := s.embedder.EmbedBatch(texts); err == nil && len(vectors) == len(cards) {
			for i := range cards {
				cards[i].Embedding = vectors[i]
			}
		}
	}
	for _, card := range cards {
		card = enrichCardStructure(source, card)
		if err := insertCard(ctx, tx, card); err != nil {
			return source, err
		}
		for _, fact := range BuildFactsForCard(source, card) {
			if err := insertFact(ctx, tx, fact); err != nil {
				return source, err
			}
		}
	}
	source.Status = StatusDistilled
	source.UpdatedAt = time.Now().UTC()
	return source, insertSource(ctx, tx, source)
}

func (s *SQLiteStore) ListSources(ctx context.Context, opts ListSourcesOptions) ([]Source, error) {
	opts.OwnerID = strings.TrimSpace(opts.OwnerID)
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	opts.ProjectPath = strings.TrimSpace(opts.ProjectPath)
	opts.SearchScope = strings.ToLower(strings.TrimSpace(opts.SearchScope))
	opts.Status = strings.ToLower(strings.TrimSpace(opts.Status))
	opts.Kind = strings.ToLower(strings.TrimSpace(opts.Kind))
	opts.Domain = strings.TrimSpace(opts.Domain)
	opts.Label = strings.TrimSpace(opts.Label)
	opts.Query = strings.TrimSpace(opts.Query)
	opts.CoverageFilter = strings.TrimSpace(opts.CoverageFilter)
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 5000 {
		opts.Limit = 5000
	}
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	if opts.TenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, opts.TenantID)
	}
	if opts.OwnerID != "" {
		where = append(where, "owner_id = ?")
		args = append(args, opts.OwnerID)
	}
	switch opts.SearchScope {
	case SaveScopePersonal, SaveScopeLocalOnly, "local":
		where = append(where, "COALESCE(project_path, '') = ''")
	case SaveScopeProject:
		if opts.ProjectPath != "" {
			where = append(where, "project_path = ?")
			args = append(args, opts.ProjectPath)
		}
	default:
		if opts.ProjectPath != "" {
			where = append(where, "project_path = ?")
			args = append(args, opts.ProjectPath)
		}
	}
	sourceIDs := normalizeSearchStrings(append(append([]string{}, opts.SourceIDs...), opts.SourceID))
	if len(sourceIDs) == 1 {
		where = append(where, "id = ?")
		args = append(args, sourceIDs[0])
	} else if len(sourceIDs) > 1 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(sourceIDs)), ",")
		where = append(where, "id IN ("+placeholders+")")
		for _, id := range sourceIDs {
			args = append(args, id)
		}
	}
	if opts.Status != "" {
		where, args = appendSourceStatusFilter(where, args, opts.Status)
	}
	labels := normalizeSourceLabels(append(append([]string{}, opts.Labels...), opts.Label))
	for _, label := range labels {
		where = append(where, "EXISTS (SELECT 1 FROM knowledge_source_labels sl WHERE sl.source_id = knowledge_sources.id AND sl.label = ?)")
		args = append(args, label)
	}
	where, args = appendSourceDomainFilter(where, args, "", opts.Domain)
	switch normalizeCoverageFilter(opts.CoverageFilter) {
	case "missing_nodes":
		where = append(where, "status IN ('parsed', 'distilled', 'stale') AND NOT EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = knowledge_sources.id)")
	case "missing_cards":
		where = append(where, "status IN ('parsed', 'distilled', 'stale') AND EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = knowledge_sources.id) AND NOT EXISTS (SELECT 1 FROM knowledge_cards c WHERE c.source_id = knowledge_sources.id)")
	case "missing_facts":
		where = append(where, "status IN ('distilled', 'stale') AND EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = knowledge_sources.id) AND NOT EXISTS (SELECT 1 FROM knowledge_facts f WHERE f.source_id = knowledge_sources.id)")
	case "missing_links":
		where = append(where, "status IN ('parsed', 'distilled', 'stale') AND NOT EXISTS (SELECT 1 FROM knowledge_source_links l WHERE l.source_id = knowledge_sources.id)")
	case "complete":
		where = append(where, "EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = knowledge_sources.id)")
		where = append(where, "EXISTS (SELECT 1 FROM knowledge_cards c WHERE c.source_id = knowledge_sources.id)")
		where = append(where, "EXISTS (SELECT 1 FROM knowledge_facts f WHERE f.source_id = knowledge_sources.id)")
	case "has_nodes":
		where = append(where, "EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = knowledge_sources.id)")
	case "has_cards":
		where = append(where, "EXISTS (SELECT 1 FROM knowledge_cards c WHERE c.source_id = knowledge_sources.id)")
	case "has_facts":
		where = append(where, "EXISTS (SELECT 1 FROM knowledge_facts f WHERE f.source_id = knowledge_sources.id)")
	case "has_links":
		where = append(where, "EXISTS (SELECT 1 FROM knowledge_source_links l WHERE l.source_id = knowledge_sources.id)")
	case "pdf_ocr_needed":
		where = append(where, "kind = 'pdf' AND status IN ('parsed', 'distilled', 'stale') AND NOT EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = knowledge_sources.id AND length(trim(COALESCE(n.text, ''))) >= 40)")
	case "missing_labels":
		where = append(where, "status <> 'disabled' AND NOT EXISTS (SELECT 1 FROM knowledge_source_labels sl WHERE sl.source_id = knowledge_sources.id)")
	}
	kinds := normalizeSearchStrings(append(append([]string{}, opts.SourceKinds...), opts.Kind))
	if len(kinds) == 1 {
		where = append(where, "kind = ?")
		args = append(args, kinds[0])
	} else if len(kinds) > 1 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
		where = append(where, "kind IN ("+placeholders+")")
		for _, kind := range kinds {
			args = append(args, kind)
		}
	}
	if query := opts.Query; query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		where = append(where, "(LOWER(COALESCE(title, '')) LIKE ? OR LOWER(COALESCE(uri, '')) LIKE ? OR LOWER(COALESCE(canonical_uri, '')) LIKE ? OR LOWER(COALESCE(relative_path, '')) LIKE ? OR LOWER(COALESCE(topic_hint, '')) LIKE ? OR LOWER(COALESCE(error_message, '')) LIKE ? OR LOWER(COALESCE(batch_id, '')) LIKE ? OR EXISTS (SELECT 1 FROM knowledge_source_labels slq WHERE slq.source_id = knowledge_sources.id AND slq.label LIKE ?))")
		for i := 0; i < 8; i++ {
			args = append(args, pattern)
		}
	}
	args = append(args, opts.Limit)
	q := `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources WHERE ` + strings.Join(where, " AND ") + ` ORDER BY updated_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := make([]Source, 0)
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.hydrateSourceCounts(ctx, sources); err != nil {
		return nil, err
	}
	if err := s.hydrateSourceLabels(ctx, sources); err != nil {
		return nil, err
	}
	return sources, nil
}

func (s *SQLiteStore) GetSource(ctx context.Context, id string) (Source, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Source{}, fmt.Errorf("source id is required")
	}
	q := `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources WHERE id = ?`
	source, err := scanSource(s.db.QueryRowContext(ctx, q, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return Source{}, fmt.Errorf("source %s not found", id)
		}
		return Source{}, err
	}
	sources := []Source{source}
	if err := s.hydrateSourceCounts(ctx, sources); err != nil {
		return Source{}, err
	}
	if err := s.hydrateSourceLabels(ctx, sources); err != nil {
		return Source{}, err
	}
	return sources[0], nil
}

func (s *SQLiteStore) UpdateSourceMetadata(ctx context.Context, req SourceUpdateRequest) (Source, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return Source{}, fmt.Errorf("source id is required")
	}
	existing, err := s.GetSource(ctx, id)
	if err != nil {
		return Source{}, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = existing.Title
	}
	topicHint := strings.TrimSpace(req.TopicHint)
	if topicHint == "" {
		topicHint = existing.TopicHint
	}
	trust := req.SourceTrust
	if trust < 0 {
		trust = existing.SourceTrust
	}
	if trust > 1 {
		trust = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE knowledge_sources
		SET title = ?, topic_hint = ?, source_trust = ?, updated_at = ?
		WHERE id = ?`,
		title, topicHint, trust, formatTime(time.Now().UTC()), id)
	if err != nil {
		return Source{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Source{}, err
	}
	if affected == 0 {
		return Source{}, fmt.Errorf("source %s not found", id)
	}
	if req.Labels != nil {
		if err := s.ReplaceSourceLabels(ctx, id, req.Labels); err != nil {
			return Source{}, err
		}
	}
	return s.GetSource(ctx, id)
}

func (s *SQLiteStore) ReplaceSourceLabels(ctx context.Context, sourceID string, labels []string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("source id is required")
	}
	labels = normalizeSourceLabels(labels)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceSourceLabelsTx(ctx, tx, sourceID, labels); err != nil {
		return err
	}
	return tx.Commit()
}

func addSourceLabelsTx(ctx context.Context, tx *sql.Tx, sourceID string, labels []string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("source id is required")
	}
	labels = normalizeSourceLabels(labels)
	if len(labels) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, label := range labels {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO knowledge_source_labels(source_id, label, created_at) VALUES (?, ?, ?)`, sourceID, label, formatTime(now)); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE knowledge_sources SET updated_at = ? WHERE id = ?`, formatTime(now), sourceID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("source %s not found", sourceID)
	}
	return nil
}

func replaceSourceLabelsTx(ctx context.Context, tx *sql.Tx, sourceID string, labels []string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("source id is required")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_source_labels WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	if len(normalizeSourceLabels(labels)) > 0 {
		return addSourceLabelsTx(ctx, tx, sourceID, labels)
	}
	res, err := tx.ExecContext(ctx, `UPDATE knowledge_sources SET updated_at = ? WHERE id = ?`, formatTime(time.Now().UTC()), sourceID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("source %s not found", sourceID)
	}
	return nil
}

func (s *SQLiteStore) ListSourceLabels(ctx context.Context, opts ListSourcesOptions) ([]SourceLabelSummary, error) {
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}
	if opts.Limit > 5000 {
		opts.Limit = 5000
	}
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return nil, err
	}
	return summarizeSourceLabels(sources), nil
}

func (s *SQLiteStore) UpdateSourceLabels(ctx context.Context, req SourceLabelUpdateRequest) (SourceLabelUpdateResult, error) {
	if req.Limit <= 0 {
		req.Limit = 1000
	}
	if req.Limit > 5000 {
		req.Limit = 5000
	}
	sources, err := s.sourcesForLabelUpdate(ctx, req)
	if err != nil {
		return SourceLabelUpdateResult{}, err
	}
	result := SourceLabelUpdateResult{
		Requested: len(sources),
		DryRun:    req.DryRun,
		Mode:      sourceLabelUpdateMode(req),
	}
	addLabels := normalizeSourceLabels(req.AddLabels)
	removeLabels := normalizeSourceLabels(req.RemoveLabels)
	replaceLabels := normalizeSourceLabels(req.ReplaceLabels)
	renameFrom := firstNormalizedSourceLabel(req.RenameFrom)
	renameTo := firstNormalizedSourceLabel(req.RenameTo)
	for _, source := range sources {
		if source.ID == "" {
			continue
		}
		before := normalizeSourceLabels(source.Labels)
		after := nextSourceLabels(before, addLabels, removeLabels, replaceLabels, renameFrom, renameTo, req)
		result.SourceIDs = append(result.SourceIDs, source.ID)
		result.LabelChanges = append(result.LabelChanges, SourceLabelChange{
			SourceID: source.ID,
			Before:   append([]string{}, before...),
			After:    append([]string{}, after...),
		})
		if req.DryRun {
			result.Updated++
			continue
		}
		if err := s.ReplaceSourceLabels(ctx, source.ID, after); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SourceLabelUpdateFail{SourceID: source.ID, Error: err.Error()})
			continue
		}
		updated, err := s.GetSource(ctx, source.ID)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SourceLabelUpdateFail{SourceID: source.ID, Error: err.Error()})
			continue
		}
		result.Updated++
		if len(result.Sources) < 100 {
			result.Sources = append(result.Sources, updated)
		}
	}
	return result, nil
}

func (s *SQLiteStore) BackfillSourceAutoLabels(ctx context.Context, req SourceAutoLabelBackfillRequest) (SourceLabelUpdateResult, error) {
	if req.Limit <= 0 {
		req.Limit = 1000
	}
	if req.Limit > 5000 {
		req.Limit = 5000
	}
	sources, err := s.sourcesForLabelUpdate(ctx, SourceLabelUpdateRequest{
		SourceIDs: req.SourceIDs,
		Filter:    req.Filter,
		Limit:     req.Limit,
	})
	if err != nil {
		return SourceLabelUpdateResult{}, err
	}
	result := SourceLabelUpdateResult{
		Requested: len(sources),
		DryRun:    req.DryRun,
		Mode:      "backfill_auto_labels",
	}
	for _, source := range sources {
		if source.ID == "" {
			continue
		}
		before := normalizeSourceLabels(source.Labels)
		autoLabels := ingestLabelsForSource(source, nil, true)
		after := nextSourceLabels(before, autoLabels, nil, nil, "", "", SourceLabelUpdateRequest{})
		result.SourceIDs = append(result.SourceIDs, source.ID)
		result.LabelChanges = append(result.LabelChanges, SourceLabelChange{
			SourceID: source.ID,
			Before:   append([]string{}, before...),
			After:    append([]string{}, after...),
		})
		if stringSlicesEqual(before, after) {
			continue
		}
		if req.DryRun {
			result.Updated++
			continue
		}
		if err := s.ReplaceSourceLabels(ctx, source.ID, after); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SourceLabelUpdateFail{SourceID: source.ID, Error: err.Error()})
			continue
		}
		updated, err := s.GetSource(ctx, source.ID)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SourceLabelUpdateFail{SourceID: source.ID, Error: err.Error()})
			continue
		}
		result.Updated++
		if len(result.Sources) < 100 {
			result.Sources = append(result.Sources, updated)
		}
	}
	return result, nil
}

func (s *SQLiteStore) sourcesForLabelUpdate(ctx context.Context, req SourceLabelUpdateRequest) ([]Source, error) {
	if len(req.SourceIDs) > 0 {
		ids := uniqueTrimmed(req.SourceIDs)
		if len(ids) > req.Limit {
			ids = ids[:req.Limit]
		}
		sources := make([]Source, 0, len(ids))
		for _, id := range ids {
			source, err := s.GetSource(ctx, id)
			if err != nil {
				return nil, err
			}
			sources = append(sources, source)
		}
		return sources, nil
	}
	opts := req.Filter
	if !hasListSourceFilters(opts) {
		if renameFrom := firstNormalizedSourceLabel(req.RenameFrom); renameFrom != "" {
			opts.Labels = []string{renameFrom}
		} else if labels := normalizeSourceLabels(req.RemoveLabels); len(labels) > 0 {
			opts.Labels = labels
		}
	}
	opts.Limit = req.Limit
	return s.ListSources(ctx, opts)
}

func (s *SQLiteStore) hydrateSourceCounts(ctx context.Context, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	index := make(map[string]int, len(sources))
	ids := make([]string, 0, len(sources))
	for i, source := range sources {
		if source.ID == "" {
			continue
		}
		index[source.ID] = i
		ids = append(ids, source.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	if err := s.hydrateSourceCountColumn(ctx, ids, "document_nodes", func(source *Source, count int) {
		source.NodeCount = count
	}, sources, index); err != nil {
		return err
	}
	if err := s.hydrateSourceCountColumn(ctx, ids, "knowledge_cards", func(source *Source, count int) {
		source.CardCount = count
	}, sources, index); err != nil {
		return err
	}
	return s.hydrateSourceCountColumn(ctx, ids, "knowledge_facts", func(source *Source, count int) {
		source.FactCount = count
	}, sources, index)
}

func (s *SQLiteStore) hydrateSourceLabels(ctx context.Context, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	index := make(map[string]int, len(sources))
	ids := make([]string, 0, len(sources))
	for i, source := range sources {
		if source.ID == "" {
			continue
		}
		index[source.ID] = i
		ids = append(ids, source.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, label FROM knowledge_source_labels WHERE source_id IN (`+placeholders+`) ORDER BY label ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID, label string
		if err := rows.Scan(&sourceID, &label); err != nil {
			return err
		}
		if i, ok := index[sourceID]; ok {
			sources[i].Labels = append(sources[i].Labels, label)
		}
	}
	return rows.Err()
}

func (s *SQLiteStore) hydrateSourceCountColumn(ctx context.Context, ids []string, table string, set func(*Source, int), sources []Source, index map[string]int) error {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, COUNT(*) FROM `+table+` WHERE source_id IN (`+placeholders+`) GROUP BY source_id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID string
		var count int
		if err := rows.Scan(&sourceID, &count); err != nil {
			return err
		}
		if i, ok := index[sourceID]; ok {
			set(&sources[i], count)
		}
	}
	return rows.Err()
}

var canonicalCoverageFilters = []string{"missing_nodes", "missing_cards", "missing_facts", "missing_links", "complete", "has_nodes", "has_cards", "has_facts", "has_links", "pdf_ocr_needed", "missing_labels"}

var coverageFilterAliases = map[string]string{
	"without_nodes":            "missing_nodes",
	"no_nodes":                 "missing_nodes",
	"missingnodes":             "missing_nodes",
	"without_cards":            "missing_cards",
	"no_cards":                 "missing_cards",
	"missingcards":             "missing_cards",
	"rebuild_cards":            "missing_cards",
	"rebuildcards":             "missing_cards",
	"cards_rebuildable":        "missing_cards",
	"cardsrebuildable":         "missing_cards",
	"missing_cards_with_nodes": "missing_cards",
	"without_facts":            "missing_facts",
	"no_facts":                 "missing_facts",
	"missingfacts":             "missing_facts",
	"rebuild_facts":            "missing_facts",
	"rebuildfacts":             "missing_facts",
	"facts_rebuildable":        "missing_facts",
	"factsrebuildable":         "missing_facts",
	"missing_facts_with_nodes": "missing_facts",
	"without_links":            "missing_links",
	"no_links":                 "missing_links",
	"missinglinks":             "missing_links",
	"unlinked":                 "missing_links",
	"full":                     "complete",
	"with_nodes":               "has_nodes",
	"hasnodes":                 "has_nodes",
	"with_cards":               "has_cards",
	"hascards":                 "has_cards",
	"with_facts":               "has_facts",
	"hasfacts":                 "has_facts",
	"with_links":               "has_links",
	"haslinks":                 "has_links",
	"linked":                   "has_links",
	"pdfocrneeded":             "pdf_ocr_needed",
	"ocr_needed":               "pdf_ocr_needed",
	"needsocr":                 "pdf_ocr_needed",
	"needs_ocr":                "pdf_ocr_needed",
	"scanned_pdf":              "pdf_ocr_needed",
	"without_labels":           "missing_labels",
	"no_labels":                "missing_labels",
	"missinglabels":            "missing_labels",
	"unlabeled":                "missing_labels",
	"unlabelled":               "missing_labels",
}

func coverageFilters() []string {
	return append([]string(nil), canonicalCoverageFilters...)
}

func coverageAliases() map[string]string {
	out := make(map[string]string, len(coverageFilterAliases))
	for alias, canonical := range coverageFilterAliases {
		out[alias] = canonical
	}
	return out
}

func normalizeCoverageFilter(value string) string {
	normalized := normalizeCoverageFilterKey(value)
	for _, filter := range canonicalCoverageFilters {
		if normalized == filter {
			return filter
		}
	}
	return coverageFilterAliases[normalized]
}

func normalizeCoverageFilterKey(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("-", " ", "_", " ", ".", " ", "/", " ")
	normalized = replacer.Replace(normalized)
	return strings.Join(strings.Fields(normalized), "_")
}

func (s *SQLiteStore) ListNodesBySource(ctx context.Context, sourceID string, limit int) ([]DocumentNode, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, COALESCE(parent_id, ''), type, COALESCE(title, ''), COALESCE(text, ''),
		level, page, COALESCE(sheet_name, ''), COALESCE(row_range, ''), COALESCE(col_range, ''), COALESCE(xpath, ''),
		offset, COALESCE(metadata_json, '{}'), token_count
		FROM document_nodes WHERE source_id = ? ORDER BY offset ASC, level ASC, id ASC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]DocumentNode, 0)
	for rows.Next() {
		node, err := scanDocumentNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *SQLiteStore) ListSourceVersions(ctx context.Context, sourceID string, limit int) ([]SourceVersion, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, COALESCE(kind, ''), COALESCE(uri, ''), COALESCE(canonical_uri, ''), COALESCE(title, ''),
		COALESCE(content_hash, ''), COALESCE(status, ''), COALESCE(reason, ''), COALESCE(fetched_at, ''),
		node_count, card_count, fact_count, created_at
		FROM knowledge_source_versions WHERE source_id = ? ORDER BY created_at DESC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := make([]SourceVersion, 0)
	for rows.Next() {
		version, err := scanSourceVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (s *SQLiteStore) ListCardsBySource(ctx context.Context, sourceID string, limit int) ([]Card, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, COALESCE(node_id, ''), title, claim, summary, entities_json, topics_json, tags_json,
		project_path, owner_id, tenant_id, valid_at, invalid_at, confidence, importance, source_trust, created_at, updated_at
		FROM knowledge_cards WHERE source_id = ? ORDER BY importance DESC, updated_at DESC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := make([]Card, 0)
	for rows.Next() {
		card, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

func (s *SQLiteStore) ListFactsBySource(ctx context.Context, sourceID string, limit int) ([]Fact, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source id is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, card_id, source_id, subject, predicate, object, negated, valid_at, invalid_at, confidence
		FROM knowledge_facts WHERE source_id = ? ORDER BY confidence DESC, subject, predicate LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := make([]Fact, 0)
	for rows.Next() {
		fact, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}

func (s *SQLiteStore) DeleteSource(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("source id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteSourceDerivedRows(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_sources WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSourcesByFilter deletes all sources matching the given owner/tenant filter
// and their derived data (nodes, cards, facts, FTS entries) in a single transaction.
// Returns the number of sources deleted.
func (s *SQLiteStore) DeleteSourcesByFilter(ctx context.Context, opts ListSourcesOptions) (int, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	if opts.TenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, opts.TenantID)
	}
	if opts.OwnerID != "" {
		where = append(where, "owner_id = ?")
		args = append(args, opts.OwnerID)
	}
	if opts.TenantID == "" && opts.OwnerID == "" {
		return 0, fmt.Errorf("at least one of tenant_id or owner_id is required for bulk delete")
	}

	// Collect source IDs matching the filter
	query := `SELECT id FROM knowledge_sources WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	// Delete all in a single transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Batch delete derived data using IN clause instead of per-ID loop.
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	deleteArgs := make([]interface{}, len(ids))
	for i, id := range ids {
		deleteArgs[i] = id
	}

	derivedDeletes := []string{
		`DELETE FROM document_nodes_fts WHERE node_id IN (SELECT id FROM document_nodes WHERE source_id IN (` + placeholders + `))`,
		`DELETE FROM knowledge_cards_fts WHERE card_id IN (SELECT id FROM knowledge_cards WHERE source_id IN (` + placeholders + `))`,
		`DELETE FROM knowledge_facts_fts WHERE fact_id IN (SELECT id FROM knowledge_facts WHERE source_id IN (` + placeholders + `))`,
		`DELETE FROM knowledge_facts WHERE source_id IN (` + placeholders + `)`,
		`DELETE FROM knowledge_cards WHERE source_id IN (` + placeholders + `)`,
		`DELETE FROM knowledge_card_suppressions WHERE card_id IN (SELECT id FROM knowledge_cards WHERE source_id IN (` + placeholders + `))`,
		`DELETE FROM document_nodes WHERE source_id IN (` + placeholders + `)`,
	}
	for _, stmt := range derivedDeletes {
		if _, err := tx.ExecContext(ctx, stmt, deleteArgs...); err != nil {
			return 0, err
		}
	}

	// Delete sources themselves
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_sources WHERE id IN (`+placeholders+`)`, deleteArgs...); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (s *SQLiteStore) DisableSource(ctx context.Context, id string) (Source, error) {
	return s.updateSourceStatus(ctx, id, StatusDisabled)
}

func (s *SQLiteStore) DisableSources(ctx context.Context, ids []string) SourceStatusUpdateResult {
	return s.updateSourcesStatus(ctx, ids, StatusDisabled)
}

func (s *SQLiteStore) DisableSourcesByFilter(ctx context.Context, opts ListSourcesOptions) (SourceStatusUpdateResult, error) {
	return s.updateSourcesStatusByFilter(ctx, opts, StatusDisabled)
}

func (s *SQLiteStore) EnableSource(ctx context.Context, id string) (Source, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Source{}, fmt.Errorf("source id is required")
	}
	var cardCount, nodeCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE source_id = ?`, id).Scan(&cardCount); err != nil {
		return Source{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_nodes WHERE source_id = ?`, id).Scan(&nodeCount); err != nil {
		return Source{}, err
	}
	status := StatusPending
	if cardCount > 0 {
		status = StatusDistilled
	} else if nodeCount > 0 {
		status = StatusParsed
	}
	return s.updateSourceStatus(ctx, id, status)
}

func (s *SQLiteStore) EnableSources(ctx context.Context, ids []string) SourceStatusUpdateResult {
	ids = uniqueTrimmed(ids)
	result := SourceStatusUpdateResult{Requested: len(ids)}
	for _, id := range ids {
		source, err := s.EnableSource(ctx, id)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SourceStatusUpdateFailure{SourceID: id, Error: err.Error()})
			continue
		}
		result.Updated++
		result.Sources = append(result.Sources, source)
	}
	return result
}

func (s *SQLiteStore) EnableSourcesByFilter(ctx context.Context, opts ListSourcesOptions) (SourceStatusUpdateResult, error) {
	opts = normalizeSourceStatusFilterOptions(opts)
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceStatusUpdateResult{}, err
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	return s.EnableSources(ctx, ids), nil
}

func (s *SQLiteStore) updateSourceStatus(ctx context.Context, id string, status string) (Source, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Source{}, fmt.Errorf("source id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return Source{}, fmt.Errorf("source status is required")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE knowledge_sources SET status = ?, error_message = '', updated_at = ? WHERE id = ?`, status, formatTime(time.Now().UTC()), id)
	if err != nil {
		return Source{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Source{}, err
	}
	if affected == 0 {
		return Source{}, fmt.Errorf("source %s not found", id)
	}
	return s.GetSource(ctx, id)
}

func (s *SQLiteStore) updateSourcesStatus(ctx context.Context, ids []string, status string) SourceStatusUpdateResult {
	ids = uniqueTrimmed(ids)
	result := SourceStatusUpdateResult{Requested: len(ids), Status: strings.TrimSpace(status)}
	for _, id := range ids {
		source, err := s.updateSourceStatus(ctx, id, status)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SourceStatusUpdateFailure{SourceID: id, Error: err.Error()})
			continue
		}
		result.Updated++
		result.Sources = append(result.Sources, source)
	}
	return result
}

func (s *SQLiteStore) updateSourcesStatusByFilter(ctx context.Context, opts ListSourcesOptions, status string) (SourceStatusUpdateResult, error) {
	opts = normalizeSourceStatusFilterOptions(opts)
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceStatusUpdateResult{}, err
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	return s.updateSourcesStatus(ctx, ids, status), nil
}

func normalizeSourceStatusFilterOptions(opts ListSourcesOptions) ListSourcesOptions {
	opts.Limit = sourceFilterLimit(opts, 100, 1000, 5000)
	return opts
}

// appendSourceStatusFilter translates semantic status aliases into SQL conditions.
// "active" = all non-disabled sources; "error" = alias for "failed"; others = exact match.
func appendSourceStatusFilter(where []string, args []interface{}, status string) ([]string, []interface{}) {
	switch status {
	case "active":
		where = append(where, "status != 'disabled'")
	case "error":
		where = append(where, "status = 'failed'")
	default:
		where = append(where, "status = ?")
		args = append(args, status)
	}
	return where, args
}

func (s *SQLiteStore) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	query := buildFTSQuerySegmented(opts.Query)
	if query == "" {
		return []SearchResult{}, nil
	}
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 20
	}
	candidateLimit := opts.Limit * 3
	if candidateLimit < opts.Limit {
		candidateLimit = opts.Limit
	}
	if candidateLimit > 100 {
		candidateLimit = 100
	}
	resultTypes := normalizeSearchResultTypes(opts.ResultTypes)

	results := make([]SearchResult, 0, opts.Limit)
	seenCards := make(map[string]struct{})
	seenNodes := make(map[string]struct{})

	if wantsSearchType(resultTypes, "card") {
		cardWhere := []string{"knowledge_cards_fts MATCH ?", "NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)"}
		cardArgs := []interface{}{query}
		cardWhere, cardArgs = appendSearchFilters(cardWhere, cardArgs, "s", opts)
		cardArgs = append(cardArgs, candidateLimit)
		cardQuery := `SELECT c.id, COALESCE(c.node_id, ''), c.title, c.claim, c.summary,
		COALESCE(n.title, ''), COALESCE(n.type, ''), COALESCE(n.page, 0), COALESCE(n.sheet_name, ''), COALESCE(n.row_range, ''), COALESCE(n.col_range, ''),
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		snippet(knowledge_cards_fts, 2, '', '', '...', 32), bm25(knowledge_cards_fts)
		FROM knowledge_cards_fts
		JOIN knowledge_cards c ON c.id = knowledge_cards_fts.card_id
		LEFT JOIN document_nodes n ON n.id = c.node_id
		JOIN knowledge_sources s ON s.id = c.source_id
		WHERE ` + strings.Join(cardWhere, " AND ") + ` ORDER BY bm25(knowledge_cards_fts) LIMIT ?`
		cardRows, err := s.db.QueryContext(ctx, cardQuery, cardArgs...)
		if err != nil {
			return nil, err
		}
		for cardRows.Next() {
			var result SearchResult
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			var snippet string
			var rank float64
			if err := cardRows.Scan(&result.CardID, &result.NodeID, &result.CardTitle, &result.Claim, &result.Summary,
				&result.NodeTitle, &result.NodeType, &result.Page, &result.SheetName, &result.RowRange, &result.ColRange,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &snippet, &rank); err != nil {
				_ = cardRows.Close()
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			result.Source = source
			result.ResultType = "card"
			result.Snippet = strings.TrimSpace(snippet)
			result.Score = scoreSearchResult(result, opts, -rank)
			result.Citation = formatResultCitation(result)
			seenCards[result.CardID] = struct{}{}
			if result.NodeID != "" {
				seenNodes[source.ID+"\x00"+result.NodeID] = struct{}{}
			}
			results = append(results, result)
		}
		if err := cardRows.Close(); err != nil {
			return nil, err
		}
		if err := cardRows.Err(); err != nil {
			return nil, err
		}
	}

	if wantsSearchType(resultTypes, "fact") {
		factWhere := []string{"knowledge_facts_fts MATCH ?", "NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)"}
		factArgs := []interface{}{query}
		factWhere, factArgs = appendSearchFilters(factWhere, factArgs, "s", opts)
		factArgs = append(factArgs, candidateLimit)
		factQuery := `SELECT f.id, f.card_id, f.subject, f.predicate, f.object, c.title, COALESCE(c.node_id, ''), c.claim, c.summary,
		COALESCE(n.title, ''), COALESCE(n.type, ''), COALESCE(n.page, 0), COALESCE(n.sheet_name, ''), COALESCE(n.row_range, ''), COALESCE(n.col_range, ''),
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		bm25(knowledge_facts_fts)
		FROM knowledge_facts_fts
		JOIN knowledge_facts f ON f.id = knowledge_facts_fts.fact_id
		JOIN knowledge_cards c ON c.id = f.card_id
		LEFT JOIN document_nodes n ON n.id = c.node_id
		JOIN knowledge_sources s ON s.id = f.source_id
		WHERE ` + strings.Join(factWhere, " AND ") + ` ORDER BY bm25(knowledge_facts_fts) LIMIT ?`
		factRows, err := s.db.QueryContext(ctx, factQuery, factArgs...)
		if err != nil {
			return nil, err
		}
		for factRows.Next() {
			var result SearchResult
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			var rank float64
			if err := factRows.Scan(&result.FactID, &result.CardID, &result.Subject, &result.Predicate, &result.Object, &result.CardTitle, &result.NodeID, &result.Claim, &result.Summary,
				&result.NodeTitle, &result.NodeType, &result.Page, &result.SheetName, &result.RowRange, &result.ColRange,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &rank); err != nil {
				_ = factRows.Close()
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			result.Source = source
			result.ResultType = "fact"
			result.Snippet = strings.TrimSpace(result.Subject + " " + result.Predicate + " " + result.Object)
			result.Score = scoreSearchResult(result, opts, -rank)
			result.Citation = formatResultCitation(result)
			seenCards[result.CardID] = struct{}{}
			if result.NodeID != "" {
				seenNodes[source.ID+"\x00"+result.NodeID] = struct{}{}
			}
			results = append(results, result)
		}
		if err := factRows.Close(); err != nil {
			return nil, err
		}
		if err := factRows.Err(); err != nil {
			return nil, err
		}
	}

	if wantsSearchType(resultTypes, "node") {
		nodeWhere := []string{"document_nodes_fts MATCH ?"}
		nodeArgs := []interface{}{query}
		nodeWhere, nodeArgs = appendSearchFilters(nodeWhere, nodeArgs, "s", opts)
		nodeArgs = append(nodeArgs, candidateLimit)
		nodeQuery := `SELECT n.id, n.title, n.type, n.page, n.sheet_name, n.row_range, n.col_range,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at,
		snippet(document_nodes_fts, 2, '', '', '...', 32), bm25(document_nodes_fts)
		FROM document_nodes_fts
		JOIN document_nodes n ON n.id = document_nodes_fts.node_id
		JOIN knowledge_sources s ON s.id = n.source_id
		WHERE ` + strings.Join(nodeWhere, " AND ") + ` ORDER BY bm25(document_nodes_fts) LIMIT ?`
		nodeRows, err := s.db.QueryContext(ctx, nodeQuery, nodeArgs...)
		if err != nil {
			return nil, err
		}
		defer nodeRows.Close()
		for nodeRows.Next() {
			var result SearchResult
			var snippet string
			var rank float64
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			if err := nodeRows.Scan(&result.NodeID, &result.NodeTitle, &result.NodeType, &result.Page, &result.SheetName, &result.RowRange, &result.ColRange,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt, &snippet, &rank); err != nil {
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			if _, ok := seenNodes[source.ID+"\x00"+result.NodeID]; ok {
				continue
			}
			result.Source = source
			result.ResultType = "node"
			result.Snippet = strings.TrimSpace(snippet)
			result.Score = scoreSearchResult(result, opts, -rank)
			result.Citation = formatResultCitation(result)
			results = append(results, result)
		}
		if err := nodeRows.Err(); err != nil {
			return nil, err
		}
	}

	// FTS fallback: if FTS returned no results or only very low-scoring results
	// and the query contains CJK characters, fall back to LIKE-based search.
	// This handles three cases:
	// 1. FTS index not yet rebuilt with segmentation (rebuild pending/failed)
	// 2. FTS tokenization mismatch (new words not in gse dictionary)
	// 3. FTS found card/fact results but missed relevant document_nodes content
	//    (distillation loss: cards/facts may not cover all original document text)
	if containsCJKRunes(opts.Query) {
		// For CJK queries, always run LIKE fallback to search document_nodes original
		// text. FTS tokenization mismatch means FTS may find some nodes (via "马勇"
		// matching page 1) but miss others (page 2 has "书籍" which doesn't match
		// query token "书"). LIKE handles arbitrary substrings correctly.
		// Performance: O(nodes × terms) string matching. For typical knowledge bases
		// (<2000 nodes, <12 terms), this is <50ms — acceptable tradeoff for recall.
		likeResults, likeErr := s.searchCJKLikeFallback(ctx, opts)
		if likeErr == nil && len(likeResults) > 0 {
			// Merge LIKE results into FTS results, deduplicating by card/fact/node ID
			seen := make(map[string]struct{})
			for _, r := range results {
				if r.CardID != "" {
					seen[r.CardID] = struct{}{}
				}
				if r.FactID != "" {
					seen[r.FactID] = struct{}{}
				}
				if r.NodeID != "" {
					seen[r.NodeID] = struct{}{}
				}
			}
			for _, lr := range likeResults {
				isDup := false
				if lr.CardID != "" {
					if _, ok := seen[lr.CardID]; ok {
						isDup = true
					}
				}
				if lr.FactID != "" {
					if _, ok := seen[lr.FactID]; ok {
						isDup = true
					}
				}
				if lr.NodeID != "" && lr.CardID == "" && lr.FactID == "" {
					if _, ok := seen[lr.NodeID]; ok {
						isDup = true
					}
				}
				if !isDup {
					results = append(results, lr)
				}
			}
		}
	}

	// Embedding vector search: fuse with FTS/LIKE results using RRF.
	// This provides semantic matching (e.g., "学历" matches "博士") that
	// neither FTS nor LIKE can achieve.
	// Triggered when FTS+LIKE didn't find high-confidence results from the
	// ORIGINAL FTS path (ignoring LIKE-injected scores). LIKE may produce
	// high scores from term-count heuristics that don't reflect true semantic
	// relevance, so we check the best FTS-originated score separately.
	if s.embedder != nil && !embedding.IsNoop(s.embedder) {
		// Find the best score from FTS results only (exclude LIKE-injected nodes
		// which use term-count scoring that can be artificially high).
		bestFTSScore := 0.0
		for _, r := range results {
			if r.Score > bestFTSScore && r.ResultType != "node" {
				bestFTSScore = r.Score
			}
		}
		needsEmbedding := len(results) == 0
		if !needsEmbedding && bestFTSScore < 2.0 {
			// FTS found nothing high-confidence — embedding may find semantic matches
			needsEmbedding = true
		}
		if needsEmbedding {
			embResults, embErr := s.searchByEmbedding(ctx, opts)
			if embErr == nil && len(embResults) > 0 {
				results = rrfFuse(results, embResults, opts.Limit)
			}
		}
	}

	sortSearchResults(results)
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	if err := s.hydrateSearchResultSourceLabels(ctx, results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SQLiteStore) hydrateSearchResultSourceLabels(ctx context.Context, results []SearchResult) error {
	if len(results) == 0 {
		return nil
	}
	sourceByID := make(map[string]Source)
	sources := make([]Source, 0)
	for _, result := range results {
		if result.Source.ID == "" {
			continue
		}
		if _, ok := sourceByID[result.Source.ID]; ok {
			continue
		}
		sourceByID[result.Source.ID] = result.Source
		sources = append(sources, result.Source)
	}
	if len(sources) == 0 {
		return nil
	}
	if err := s.hydrateSourceLabels(ctx, sources); err != nil {
		return err
	}
	for _, source := range sources {
		sourceByID[source.ID] = source
	}
	for i := range results {
		if source, ok := sourceByID[results[i].Source.ID]; ok {
			results[i].Source.Labels = source.Labels
		}
	}
	return nil
}

func (s *SQLiteStore) Explain(ctx context.Context, opts SearchOptions) (ExplainResult, error) {
	query := strings.TrimSpace(opts.Query)
	if opts.Limit <= 0 || opts.Limit > 50 {
		opts.Limit = 8
	}
	results, err := s.Search(ctx, opts)
	if err != nil {
		return ExplainResult{}, err
	}
	citations := make([]Citation, 0, len(results))
	seen := make(map[string]struct{})
	for _, result := range results {
		citation := citationFromResult(result)
		key := citationKey(citation)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		citations = append(citations, citation)
	}
	return ExplainResult{
		Query:     query,
		Count:     len(results),
		Results:   results,
		Citations: citations,
		Notes:     []string{"local_fts_no_llm", "topic_context_rerank", "card_fact_node_candidates"},
	}, nil
}

func (s *SQLiteStore) SearchFacets(ctx context.Context, opts SearchOptions) (SearchFacetsResult, error) {
	query := strings.TrimSpace(opts.Query)
	result := SearchFacetsResult{
		Query: query,
		Notes: []string{"local_search_facets_no_llm", "derived_from_search_results", "source_filtered"},
	}
	if buildFTSQuery(query) == "" {
		return s.SearchFacetsBrowse(ctx, opts)
	}
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 100
	}
	results, err := s.Search(ctx, opts)
	if err != nil {
		return SearchFacetsResult{}, err
	}
	result.Count = len(results)

	resultTypes := make(map[string]*SearchFacetBucket)
	sourceKinds := make(map[string]*SearchFacetBucket)
	domains := make(map[string]*SearchFacetBucket)
	labels := make(map[string]*SearchFacetBucket)
	sources := make(map[string]*SearchFacetBucket)
	entities := make(map[string]*SearchFacetBucket)
	predicates := make(map[string]*SearchFacetBucket)

	for _, hit := range results {
		example := facetExample(hit)
		if hit.ResultType != "" {
			addSearchFacetBucket(resultTypes, hit.ResultType, SearchFacetBucket{Label: hit.ResultType, Kind: "result_type"}, example)
		}
		if hit.Source.Kind != "" {
			addSearchFacetBucket(sourceKinds, hit.Source.Kind, SearchFacetBucket{Label: hit.Source.Kind, Kind: "source_kind"}, example)
		}
		domain := sourceFacetDomain(hit.Source)
		addSearchFacetBucket(domains, domain, SearchFacetBucket{Label: domain, Kind: "domain", Domain: domain}, example)
		for _, label := range hit.Source.Labels {
			addSearchFacetBucket(labels, label, SearchFacetBucket{Label: label, Kind: "label"}, example)
		}
		sourceLabel := sourceCitationLabel(hit.Source)
		addSearchFacetBucket(sources, hit.Source.ID, SearchFacetBucket{
			Label:      sourceLabel,
			Kind:       "source",
			SourceID:   hit.Source.ID,
			SourceKind: hit.Source.Kind,
			Domain:     domain,
		}, example)
	}

	factOpts := opts
	factOpts.ResultTypes = []string{"fact"}
	factOpts.Limit = 100
	factResults, err := s.Search(ctx, factOpts)
	if err != nil {
		return SearchFacetsResult{}, err
	}
	for _, hit := range factResults {
		example := facetExample(hit)
		subject := cleanFactPart(hit.Subject)
		object := cleanFactPart(hit.Object)
		predicate := cleanFactPart(hit.Predicate)
		if subject != "" {
			addSearchFacetBucket(entities, subject, SearchFacetBucket{Label: subject, Kind: "entity"}, example)
		}
		if object != "" {
			addSearchFacetBucket(entities, object, SearchFacetBucket{Label: object, Kind: "entity"}, example)
		}
		if predicate != "" {
			addSearchFacetBucket(predicates, predicate, SearchFacetBucket{Label: predicate, Kind: "predicate"}, example)
		}
	}

	result.ResultTypes = searchFacetBuckets(resultTypes, 8)
	result.SourceKinds = searchFacetBuckets(sourceKinds, 12)
	result.Domains = searchFacetBuckets(domains, 12)
	result.Labels = searchFacetBuckets(labels, 20)
	result.Sources = searchFacetBuckets(sources, 20)
	result.Entities = searchFacetBuckets(entities, 20)
	result.Predicates = searchFacetBuckets(predicates, 20)
	return result, nil
}

func (s *SQLiteStore) SearchFacetsBrowse(ctx context.Context, opts SearchOptions) (SearchFacetsResult, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 100
	}
	result := SearchFacetsResult{
		Query: strings.TrimSpace(opts.Query),
		Notes: []string{"local_search_facets_no_llm", "browse_mode_no_query", "source_filtered"},
	}
	sourceKinds := make(map[string]*SearchFacetBucket)
	domains := make(map[string]*SearchFacetBucket)
	labels := make(map[string]*SearchFacetBucket)
	sources := make(map[string]*SearchFacetBucket)
	sourceWhere, sourceArgs := appendSearchFilters([]string{"1=1"}, nil, "s", opts)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources s WHERE `+strings.Join(sourceWhere, " AND "), sourceArgs...).Scan(&result.Count); err != nil {
		return SearchFacetsResult{}, err
	}
	sourceKindRows, err := s.db.QueryContext(ctx, `SELECT COALESCE(s.kind, ''), COUNT(*)
		FROM knowledge_sources s
		WHERE `+strings.Join(sourceWhere, " AND ")+`
		GROUP BY COALESCE(s.kind, '')
		ORDER BY COUNT(*) DESC, LOWER(COALESCE(s.kind, ''))
		LIMIT 12`, sourceArgs...)
	if err != nil {
		return SearchFacetsResult{}, err
	}
	for sourceKindRows.Next() {
		var kind string
		var count int
		if err := sourceKindRows.Scan(&kind, &count); err != nil {
			_ = sourceKindRows.Close()
			return SearchFacetsResult{}, err
		}
		kind = strings.TrimSpace(kind)
		if kind == "" || count <= 0 {
			continue
		}
		sourceKinds[strings.ToLower(kind)] = &SearchFacetBucket{Label: kind, Kind: "source_kind", Count: count}
	}
	if err := sourceKindRows.Close(); err != nil {
		return SearchFacetsResult{}, err
	}
	if err := sourceKindRows.Err(); err != nil {
		return SearchFacetsResult{}, err
	}
	if err := s.browseSourceDomainFacets(ctx, sourceWhere, sourceArgs, domains); err != nil {
		return SearchFacetsResult{}, err
	}
	if err := s.browseSourceLabelFacets(ctx, sourceWhere, sourceArgs, labels); err != nil {
		return SearchFacetsResult{}, err
	}
	sourceListArgs := append([]interface{}{}, sourceArgs...)
	sourceListArgs = append(sourceListArgs, opts.Limit)
	sourceRows, err := s.db.QueryContext(ctx, `SELECT s.id, s.kind, s.uri, s.canonical_uri, s.title, s.site_name, s.relative_path
		FROM knowledge_sources s
		WHERE `+strings.Join(sourceWhere, " AND ")+`
		ORDER BY s.updated_at DESC, s.id DESC
		LIMIT ?`, sourceListArgs...)
	if err != nil {
		return SearchFacetsResult{}, err
	}
	for sourceRows.Next() {
		var source Source
		if err := sourceRows.Scan(&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.SiteName, &source.RelativePath); err != nil {
			_ = sourceRows.Close()
			return SearchFacetsResult{}, err
		}
		example := sourceCitationLabel(source)
		domain := sourceFacetDomain(source)
		addSearchFacetBucket(sources, source.ID, SearchFacetBucket{
			Label:      example,
			Kind:       "source",
			SourceID:   source.ID,
			SourceKind: source.Kind,
			Domain:     domain,
		}, example)
	}
	if err := sourceRows.Close(); err != nil {
		return SearchFacetsResult{}, err
	}
	if err := sourceRows.Err(); err != nil {
		return SearchFacetsResult{}, err
	}
	result.SourceKinds = searchFacetBuckets(sourceKinds, 12)
	result.Domains = searchFacetBuckets(domains, 12)
	result.Labels = searchFacetBuckets(labels, 20)
	result.Sources = searchFacetBuckets(sources, 20)

	resultTypes, err := s.browseResultTypeFacets(ctx, opts)
	if err != nil {
		return SearchFacetsResult{}, err
	}
	result.ResultTypes = resultTypes
	entityIndex, err := s.FactIndex(ctx, FactIndexOptions{SearchOptions: opts, Kind: "entity"})
	if err != nil {
		return SearchFacetsResult{}, err
	}
	predicateIndex, err := s.FactIndex(ctx, FactIndexOptions{SearchOptions: opts, Kind: "predicate"})
	if err != nil {
		return SearchFacetsResult{}, err
	}
	result.Entities = factIndexItemsToSearchFacetBuckets(entityIndex.Items, 20)
	result.Predicates = factIndexItemsToSearchFacetBuckets(predicateIndex.Items, 20)
	return result, nil
}

func (s *SQLiteStore) browseSourceDomainFacets(ctx context.Context, sourceWhere []string, sourceArgs []interface{}, domains map[string]*SearchFacetBucket) error {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.kind, s.uri, s.canonical_uri, s.title, s.site_name, s.relative_path
		FROM knowledge_sources s
		WHERE `+strings.Join(sourceWhere, " AND ")+`
		ORDER BY s.updated_at DESC, s.id DESC
		LIMIT 5000`, sourceArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var source Source
		if err := rows.Scan(&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.SiteName, &source.RelativePath); err != nil {
			return err
		}
		domain := sourceFacetDomain(source)
		addSearchFacetBucket(domains, domain, SearchFacetBucket{Label: domain, Kind: "domain", Domain: domain}, sourceCitationLabel(source))
	}
	return rows.Err()
}

func (s *SQLiteStore) browseSourceLabelFacets(ctx context.Context, sourceWhere []string, sourceArgs []interface{}, labels map[string]*SearchFacetBucket) error {
	rows, err := s.db.QueryContext(ctx, `SELECT sl.label, COUNT(DISTINCT s.id)
		FROM knowledge_sources s
		JOIN knowledge_source_labels sl ON sl.source_id = s.id
		WHERE `+strings.Join(sourceWhere, " AND ")+`
		GROUP BY sl.label
		ORDER BY COUNT(DISTINCT s.id) DESC, sl.label ASC
		LIMIT 20`, sourceArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var label string
		var count int
		if err := rows.Scan(&label, &count); err != nil {
			return err
		}
		label = strings.TrimSpace(label)
		if label == "" || count <= 0 {
			continue
		}
		labels[strings.ToLower(label)] = &SearchFacetBucket{Label: label, Kind: "label", Count: count}
	}
	return rows.Err()
}

func (s *SQLiteStore) browseResultTypeFacets(ctx context.Context, opts SearchOptions) ([]SearchFacetBucket, error) {
	resultTypes := make(map[string]*SearchFacetBucket)
	for _, spec := range []struct {
		label string
		query string
	}{
		{
			label: "card",
			query: `SELECT COUNT(*) FROM knowledge_cards c
				JOIN knowledge_sources s ON s.id = c.source_id
				WHERE NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id) AND `,
		},
		{
			label: "fact",
			query: `SELECT COUNT(*) FROM knowledge_facts f
				JOIN knowledge_cards c ON c.id = f.card_id
				JOIN knowledge_sources s ON s.id = f.source_id
				WHERE NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id) AND `,
		},
		{
			label: "node",
			query: `SELECT COUNT(*) FROM document_nodes n
				JOIN knowledge_sources s ON s.id = n.source_id
				WHERE `,
		},
	} {
		where, args := appendSearchFilters([]string{"1=1"}, nil, "s", opts)
		var count int
		if err := s.db.QueryRowContext(ctx, spec.query+strings.Join(where, " AND "), args...).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			addSearchFacetBucket(resultTypes, spec.label, SearchFacetBucket{Label: spec.label, Kind: "result_type"}, "")
			resultTypes[strings.ToLower(spec.label)].Count = count
		}
	}
	return searchFacetBuckets(resultTypes, 8), nil
}

func factIndexItemsToSearchFacetBuckets(items []FactIndexItem, limit int) []SearchFacetBucket {
	buckets := make([]SearchFacetBucket, 0, len(items))
	for _, item := range items {
		buckets = append(buckets, SearchFacetBucket{
			Label:    item.Label,
			Kind:     item.Kind,
			Count:    item.Count,
			Examples: item.Examples,
		})
	}
	if limit > 0 && len(buckets) > limit {
		return buckets[:limit]
	}
	return buckets
}

func addSearchFacetBucket(buckets map[string]*SearchFacetBucket, key string, value SearchFacetBucket, example string) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return
	}
	bucket, ok := buckets[key]
	if !ok {
		value.Label = strings.TrimSpace(value.Label)
		if value.Label == "" {
			value.Label = key
		}
		bucket = &value
		buckets[key] = bucket
	}
	bucket.Count++
	example = strings.TrimSpace(example)
	if example == "" || len(bucket.Examples) >= 4 {
		return
	}
	for _, existing := range bucket.Examples {
		if strings.EqualFold(existing, example) {
			return
		}
	}
	bucket.Examples = append(bucket.Examples, example)
}

func searchFacetBuckets(values map[string]*SearchFacetBucket, limit int) []SearchFacetBucket {
	if limit <= 0 {
		limit = len(values)
	}
	buckets := make([]SearchFacetBucket, 0, len(values))
	for _, bucket := range values {
		buckets = append(buckets, *bucket)
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count != buckets[j].Count {
			return buckets[i].Count > buckets[j].Count
		}
		return strings.ToLower(buckets[i].Label) < strings.ToLower(buckets[j].Label)
	})
	if len(buckets) > limit {
		buckets = buckets[:limit]
	}
	return buckets
}

func sourceFacetDomain(source Source) string {
	if domain := normalizeURLPolicyDomain(source.SiteName); domain != "" {
		return domain
	}
	if strings.EqualFold(source.Kind, SourceKindURL) {
		for _, candidate := range []string{source.CanonicalURI, source.URI} {
			if domain := normalizeURLPolicyDomain(candidate); domain != "" {
				return domain
			}
		}
	}
	for _, candidate := range []string{source.CanonicalURI, source.URI} {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if !strings.HasPrefix(candidate, "http://") && !strings.HasPrefix(candidate, "https://") {
			continue
		}
		if domain := normalizeURLPolicyDomain(candidate); domain != "" {
			return domain
		}
	}
	return "local"
}

func facetExample(hit SearchResult) string {
	for _, candidate := range []string{hit.Citation, hit.Snippet, hit.Claim, hit.Summary, hit.NodeTitle, hit.CardTitle} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			if len(candidate) > 160 {
				candidate = strings.TrimSpace(candidate[:160]) + "..."
			}
			return candidate
		}
	}
	return sourceCitationLabel(hit.Source)
}

func (s *SQLiteStore) FactGraph(ctx context.Context, opts SearchOptions) (FactGraphResult, error) {
	query := buildFTSQuery(opts.Query)
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 40
	}
	where := []string{"NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)"}
	args := make([]interface{}, 0)
	from := `knowledge_facts f`
	order := `f.confidence DESC, f.id DESC`
	if query != "" {
		from = `knowledge_facts_fts JOIN knowledge_facts f ON f.id = knowledge_facts_fts.fact_id`
		where = append([]string{"knowledge_facts_fts MATCH ?"}, where...)
		args = append(args, query)
		order = `bm25(knowledge_facts_fts), f.confidence DESC`
	}
	entity := cleanFactPart(opts.Entity)
	if entity != "" {
		pattern := "%" + strings.ToLower(entity) + "%"
		where = append(where, "(LOWER(f.subject) = ? OR LOWER(f.object) = ? OR LOWER(f.subject) LIKE ? OR LOWER(f.object) LIKE ?)")
		args = append(args, strings.ToLower(entity), strings.ToLower(entity), pattern, pattern)
	}
	predicate := cleanFactPart(opts.Predicate)
	if predicate != "" {
		where = append(where, "LOWER(f.predicate) = ?")
		args = append(args, strings.ToLower(predicate))
	}
	where, args = appendSearchFilters(where, args, "s", opts)
	args = append(args, opts.Limit)
	q := `SELECT f.id, f.card_id, f.source_id, f.subject, f.predicate, f.object, f.confidence,
		c.title, c.claim,
		s.title, s.kind, s.uri, s.canonical_uri, s.relative_path
		FROM ` + from + `
		JOIN knowledge_cards c ON c.id = f.card_id
		JOIN knowledge_sources s ON s.id = f.source_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + ` LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return FactGraphResult{}, err
	}
	defer rows.Close()
	result := FactGraphResult{
		Query:     strings.TrimSpace(opts.Query),
		Entity:    entity,
		Predicate: predicate,
		Notes:     []string{"local_fact_graph_no_llm", "facts_fts_or_recent", "source_filtered", "entity_predicate_drilldown"},
	}
	nodeIndex := make(map[string]int)
	entityCounts := make(map[string]int)
	predicateCounts := make(map[string]int)
	addNode := func(label, kind string) {
		label = cleanFactPart(label)
		if label == "" {
			return
		}
		key := kind + "\x00" + strings.ToLower(label)
		if idx, ok := nodeIndex[key]; ok {
			result.Nodes[idx].Count++
			return
		}
		nodeIndex[key] = len(result.Nodes)
		result.Nodes = append(result.Nodes, FactGraphNode{ID: NewID("kgnode"), Label: label, Kind: kind, Count: 1})
	}
	addCount := func(counts map[string]int, value string) {
		value = cleanFactPart(value)
		if value != "" {
			counts[value]++
		}
	}
	for rows.Next() {
		var factID, cardID, sourceID, subject, predicate, object string
		var confidence float64
		var cardTitle, claim, sourceTitle, sourceKind, uri, canonicalURI, relativePath string
		if err := rows.Scan(&factID, &cardID, &sourceID, &subject, &predicate, &object, &confidence, &cardTitle, &claim, &sourceTitle, &sourceKind, &uri, &canonicalURI, &relativePath); err != nil {
			return FactGraphResult{}, err
		}
		citation := sourceCitationLabel(Source{ID: sourceID, Kind: sourceKind, URI: uri, CanonicalURI: canonicalURI, Title: sourceTitle, RelativePath: relativePath})
		if cardTitle != "" && cardTitle != sourceTitle {
			citation += " | " + cardTitle
		}
		edge := FactGraphEdge{
			ID:          NewID("kgedge"),
			FactID:      factID,
			CardID:      cardID,
			SourceID:    sourceID,
			Subject:     subject,
			Predicate:   predicate,
			Object:      object,
			SourceTitle: sourceTitle,
			Citation:    citation,
			Confidence:  confidence,
		}
		result.Edges = append(result.Edges, edge)
		addNode(subject, "entity")
		addNode(object, "entity")
		addNode(predicate, "predicate")
		addCount(entityCounts, subject)
		addCount(entityCounts, object)
		addCount(predicateCounts, predicate)
	}
	if err := rows.Err(); err != nil {
		return FactGraphResult{}, err
	}
	result.Count = len(result.Edges)
	result.TopEntities = factGraphTopCounts(entityCounts, "entity", 12)
	result.TopPredicates = factGraphTopCounts(predicateCounts, "predicate", 12)
	return result, nil
}

func factGraphTopCounts(counts map[string]int, kind string, limit int) []FactGraphNode {
	if limit <= 0 {
		limit = len(counts)
	}
	nodes := make([]FactGraphNode, 0, len(counts))
	for label, count := range counts {
		nodes = append(nodes, FactGraphNode{ID: NewID("kgnode"), Label: label, Kind: kind, Count: count})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Count != nodes[j].Count {
			return nodes[i].Count > nodes[j].Count
		}
		return strings.ToLower(nodes[i].Label) < strings.ToLower(nodes[j].Label)
	})
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes
}

func (s *SQLiteStore) FactIndex(ctx context.Context, opts FactIndexOptions) (FactIndexResult, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 60
	}
	kind := normalizeFactIndexKind(opts.Kind)
	where := []string{"NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)"}
	args := make([]interface{}, 0)
	where, args = appendSearchFilters(where, args, "s", opts.SearchOptions)
	valuesSQL := factIndexValuesSQL(kind)
	valueWhere := []string{"label <> ''"}
	query := cleanFactPart(opts.Query)
	if query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		valueWhere = append(valueWhere, "(LOWER(label) LIKE ? OR LOWER(predicate) LIKE ? OR LOWER(related) LIKE ?)")
		args = append(args, pattern, pattern, pattern)
	}
	args = append(args, opts.Limit)
	q := `WITH filtered_facts AS (
		SELECT f.id AS fact_id, f.card_id, f.source_id, f.subject, f.predicate, f.object
		FROM knowledge_facts f
		JOIN knowledge_cards c ON c.id = f.card_id
		JOIN knowledge_sources s ON s.id = f.source_id
		WHERE ` + strings.Join(where, " AND ") + `
	), fact_values AS (` + valuesSQL + `)
	SELECT label, kind, COUNT(*) AS count, COUNT(DISTINCT source_id) AS source_count, COUNT(DISTINCT card_id) AS card_count,
		COALESCE(GROUP_CONCAT(DISTINCT predicate), ''), COALESCE(GROUP_CONCAT(DISTINCT related), '')
	FROM fact_values
	WHERE ` + strings.Join(valueWhere, " AND ") + `
	GROUP BY LOWER(label), kind
	ORDER BY count DESC, LOWER(label)
	LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return FactIndexResult{}, err
	}
	defer rows.Close()
	result := FactIndexResult{
		Query: strings.TrimSpace(opts.Query),
		Kind:  kind,
		Notes: []string{"local_fact_index_no_llm", "derived_from_structured_facts", "source_filtered"},
	}
	for rows.Next() {
		var item FactIndexItem
		var predicates, examples string
		if err := rows.Scan(&item.Label, &item.Kind, &item.Count, &item.SourceCount, &item.CardCount, &predicates, &examples); err != nil {
			return FactIndexResult{}, err
		}
		item.Label = cleanFactPart(item.Label)
		item.Predicates = limitedCSVValues(predicates, 8)
		item.Examples = limitedCSVValues(examples, 8)
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return FactIndexResult{}, err
	}
	result.Count = len(result.Items)
	return result, nil
}

func normalizeFactIndexKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "predicate", "predicates", "relation", "relations", "edge", "edges":
		return "predicate"
	case "subject", "subjects":
		return "subject"
	case "object", "objects":
		return "object"
	default:
		return "entity"
	}
}

func factIndexValuesSQL(kind string) string {
	switch kind {
	case "predicate":
		return `SELECT predicate AS label, 'predicate' AS kind, source_id, card_id, predicate, subject || ' -> ' || object AS related FROM filtered_facts`
	case "subject":
		return `SELECT subject AS label, 'subject' AS kind, source_id, card_id, predicate, object AS related FROM filtered_facts`
	case "object":
		return `SELECT object AS label, 'object' AS kind, source_id, card_id, predicate, subject AS related FROM filtered_facts`
	default:
		return `SELECT subject AS label, 'entity' AS kind, source_id, card_id, predicate, object AS related FROM filtered_facts
			UNION ALL
			SELECT object AS label, 'entity' AS kind, source_id, card_id, predicate, subject AS related FROM filtered_facts`
	}
}

func limitedCSVValues(values string, limit int) []string {
	if limit <= 0 {
		limit = 8
	}
	parts := strings.Split(values, ",")
	capacity := len(parts)
	if capacity > limit {
		capacity = limit
	}
	result := make([]string, 0, capacity)
	seen := make(map[string]struct{})
	for _, part := range parts {
		part = cleanFactPart(part)
		if part == "" {
			continue
		}
		key := strings.ToLower(part)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, part)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func (s *SQLiteStore) EntityProfile(ctx context.Context, opts SearchOptions) (EntityProfileResult, error) {
	entity := cleanFactPart(firstNonEmpty(opts.Entity, opts.Query))
	if entity == "" {
		return EntityProfileResult{}, fmt.Errorf("missing entity")
	}
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 60
	}
	opts.Entity = entity
	graph, err := s.FactGraph(ctx, opts)
	if err != nil {
		return EntityProfileResult{}, err
	}
	result := EntityProfileResult{
		Entity: entity,
		Facts:  graph.Edges,
		Notes:  []string{"local_entity_profile_no_llm", "derived_from_fact_graph", "source_filtered"},
	}
	relatedCounts := make(map[string]int)
	relatedPredicates := make(map[string]map[string]struct{})
	relatedExamples := make(map[string]map[string]struct{})
	predicateCounts := make(map[string]int)
	predicateExamples := make(map[string]map[string]struct{})
	citationIndex := make(map[string]struct{})
	for _, edge := range graph.Edges {
		subject := cleanFactPart(edge.Subject)
		object := cleanFactPart(edge.Object)
		predicate := cleanFactPart(edge.Predicate)
		if predicate != "" {
			predicateCounts[predicate]++
			addProfileSet(predicateExamples, predicate, relatedExample(subject, object))
		}
		if entityMatches(subject, entity) && object != "" && !entityMatches(object, entity) {
			addRelatedProfileFact(relatedCounts, relatedPredicates, relatedExamples, object, predicate, subject)
		}
		if entityMatches(object, entity) && subject != "" && !entityMatches(subject, entity) {
			addRelatedProfileFact(relatedCounts, relatedPredicates, relatedExamples, subject, predicate, object)
		}
		if edge.Citation != "" {
			key := strings.Join([]string{edge.SourceID, edge.CardID, edge.FactID, edge.Citation}, "\x00")
			if _, ok := citationIndex[key]; !ok {
				citationIndex[key] = struct{}{}
				result.Citations = append(result.Citations, Citation{
					Label:       edge.Citation,
					SourceID:    edge.SourceID,
					SourceTitle: edge.SourceTitle,
					ResultType:  "fact",
					CardID:      edge.CardID,
					FactID:      edge.FactID,
				})
			}
		}
	}
	result.Count = len(result.Facts)
	result.RelatedEntities = profileItemsFromCounts(relatedCounts, relatedPredicates, relatedExamples, "entity", 16)
	result.Predicates = profileItemsFromCounts(predicateCounts, nil, predicateExamples, "predicate", 16)
	return result, nil
}

func (s *SQLiteStore) Suggest(ctx context.Context, opts KnowledgeSuggestOptions) (KnowledgeSuggestResult, error) {
	if opts.Limit <= 0 || opts.Limit > 80 {
		opts.Limit = 30
	}
	query := strings.TrimSpace(opts.Query)
	result := KnowledgeSuggestResult{
		Query: query,
		Notes: []string{"local_knowledge_suggest_no_llm", "source_filtered"},
	}
	kinds := normalizeSuggestKinds(opts.Kinds)
	if hasSuggestKind(kinds, "entity") {
		index, err := s.FactIndex(ctx, FactIndexOptions{SearchOptions: opts.SearchOptions, Kind: "entity"})
		if err != nil {
			return KnowledgeSuggestResult{}, err
		}
		for _, item := range index.Items {
			result.Items = append(result.Items, KnowledgeSuggestion{
				Label:    item.Label,
				Kind:     "entity",
				Count:    item.Count,
				Examples: item.Examples,
			})
			if len(result.Items) >= opts.Limit {
				break
			}
		}
	}
	if hasSuggestKind(kinds, "predicate") && len(result.Items) < opts.Limit {
		index, err := s.FactIndex(ctx, FactIndexOptions{SearchOptions: opts.SearchOptions, Kind: "predicate"})
		if err != nil {
			return KnowledgeSuggestResult{}, err
		}
		for _, item := range index.Items {
			result.Items = append(result.Items, KnowledgeSuggestion{
				Label:    item.Label,
				Kind:     "predicate",
				Count:    item.Count,
				Examples: item.Examples,
			})
			if len(result.Items) >= opts.Limit {
				break
			}
		}
	}
	if len(result.Items) < opts.Limit && (hasSuggestKind(kinds, "source") || hasSuggestKind(kinds, "domain") || hasSuggestKind(kinds, "source_kind") || hasSuggestKind(kinds, "label")) {
		sources, err := s.suggestionSources(ctx, opts.SearchOptions, 1000)
		if err != nil {
			return KnowledgeSuggestResult{}, err
		}
		if hasSuggestKind(kinds, "source") {
			for _, source := range sources {
				label := sourceSuggestionLabel(source)
				if !suggestionMatchesQuery(label, query) && !suggestionMatchesQuery(source.URI, query) && !suggestionMatchesQuery(source.CanonicalURI, query) && !suggestionMatchesQuery(source.RelativePath, query) && !suggestionMatchesQuery(source.TopicHint, query) {
					continue
				}
				result.Items = append(result.Items, KnowledgeSuggestion{
					Label:      label,
					Kind:       "source",
					Count:      source.NodeCount + source.CardCount + source.FactCount,
					SourceID:   source.ID,
					SourceKind: source.Kind,
					Domain:     sourceFacetDomain(source),
					URI:        fallbackText(source.CanonicalURI, source.URI),
					Examples:   compactSuggestionExamples(source.RelativePath, source.TopicHint, source.Status),
				})
				if len(result.Items) >= opts.Limit {
					break
				}
			}
		}
		if hasSuggestKind(kinds, "domain") && len(result.Items) < opts.Limit {
			for _, suggestion := range domainSuggestionsFromSources(sources, query, opts.Limit-len(result.Items)) {
				result.Items = append(result.Items, suggestion)
			}
		}
		if hasSuggestKind(kinds, "source_kind") && len(result.Items) < opts.Limit {
			for _, suggestion := range sourceKindSuggestionsFromSources(sources, query, opts.Limit-len(result.Items)) {
				result.Items = append(result.Items, suggestion)
			}
		}
		if hasSuggestKind(kinds, "label") && len(result.Items) < opts.Limit {
			for _, suggestion := range labelSuggestionsFromSources(sources, query, opts.Limit-len(result.Items)) {
				result.Items = append(result.Items, suggestion)
			}
		}
	}
	result.Count = len(result.Items)
	return result, nil
}

func normalizeSuggestKinds(values []string) map[string]struct{} {
	kinds := make(map[string]struct{})
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "entity", "entities":
			kinds["entity"] = struct{}{}
		case "predicate", "predicates", "relation", "relations":
			kinds["predicate"] = struct{}{}
		case "source", "sources":
			kinds["source"] = struct{}{}
		case "domain", "domains":
			kinds["domain"] = struct{}{}
		case "source_kind", "source_kinds", "kind", "kinds":
			kinds["source_kind"] = struct{}{}
		case "label", "labels", "source_label", "source_labels":
			kinds["label"] = struct{}{}
		}
	}
	if len(kinds) == 0 {
		for _, value := range []string{"entity", "predicate", "source", "domain", "source_kind", "label"} {
			kinds[value] = struct{}{}
		}
	}
	return kinds
}

func hasSuggestKind(kinds map[string]struct{}, kind string) bool {
	_, ok := kinds[kind]
	return ok
}

func (s *SQLiteStore) suggestionSources(ctx context.Context, opts SearchOptions, limit int) ([]Source, error) {
	if limit <= 0 {
		limit = 1000
	}
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	where, args = appendSearchFilters(where, args, "s", opts)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources s
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY updated_at DESC, title ASC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := make([]Source, 0)
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.hydrateSourceCounts(ctx, sources); err != nil {
		return nil, err
	}
	if err := s.hydrateSourceLabels(ctx, sources); err != nil {
		return nil, err
	}
	return sources, nil
}

func sourceSuggestionLabel(source Source) string {
	return fallbackText(source.Title, fallbackText(source.RelativePath, fallbackText(source.CanonicalURI, source.URI)))
}

func suggestionMatchesQuery(value, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), query)
}

func compactSuggestionExamples(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func domainSuggestionsFromSources(sources []Source, query string, limit int) []KnowledgeSuggestion {
	counts := make(map[string]int)
	examples := make(map[string][]string)
	for _, source := range sources {
		domain := sourceFacetDomain(source)
		if domain == "" || domain == "local" || !suggestionMatchesQuery(domain, query) {
			continue
		}
		counts[domain]++
		if len(examples[domain]) < 3 {
			examples[domain] = append(examples[domain], sourceSuggestionLabel(source))
		}
	}
	items := make([]KnowledgeSuggestion, 0, len(counts))
	for domain, count := range counts {
		items = append(items, KnowledgeSuggestion{Label: domain, Kind: "domain", Count: count, Domain: domain, Examples: examples[domain]})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Label < items[j].Label
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func sourceKindSuggestionsFromSources(sources []Source, query string, limit int) []KnowledgeSuggestion {
	counts := make(map[string]int)
	for _, source := range sources {
		kind := strings.TrimSpace(source.Kind)
		if kind == "" || !suggestionMatchesQuery(kind, query) {
			continue
		}
		counts[kind]++
	}
	items := make([]KnowledgeSuggestion, 0, len(counts))
	for kind, count := range counts {
		items = append(items, KnowledgeSuggestion{Label: kind, Kind: "source_kind", Count: count, SourceKind: kind})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Label < items[j].Label
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func labelSuggestionsFromSources(sources []Source, query string, limit int) []KnowledgeSuggestion {
	counts := make(map[string]int)
	examples := make(map[string][]string)
	for _, source := range sources {
		for _, label := range source.Labels {
			label = strings.TrimSpace(label)
			if label == "" || !suggestionMatchesQuery(label, query) {
				continue
			}
			counts[label]++
			if len(examples[label]) < 3 {
				examples[label] = append(examples[label], sourceSuggestionLabel(source))
			}
		}
	}
	items := make([]KnowledgeSuggestion, 0, len(counts))
	for label, count := range counts {
		items = append(items, KnowledgeSuggestion{Label: label, Kind: "label", Count: count, SourceLabel: label, Examples: examples[label]})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Label < items[j].Label
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func entityMatches(value, entity string) bool {
	value = strings.ToLower(cleanFactPart(value))
	entity = strings.ToLower(cleanFactPart(entity))
	return value != "" && entity != "" && (value == entity || strings.Contains(value, entity))
}

func addRelatedProfileFact(counts map[string]int, predicates map[string]map[string]struct{}, examples map[string]map[string]struct{}, label, predicate, example string) {
	label = cleanFactPart(label)
	if label == "" {
		return
	}
	counts[label]++
	addProfileSet(predicates, label, predicate)
	addProfileSet(examples, label, example)
}

func addProfileSet(values map[string]map[string]struct{}, label, value string) {
	if values == nil {
		return
	}
	label = cleanFactPart(label)
	value = cleanFactPart(value)
	if label == "" || value == "" {
		return
	}
	if values[label] == nil {
		values[label] = make(map[string]struct{})
	}
	values[label][value] = struct{}{}
}

func relatedExample(subject, object string) string {
	subject = cleanFactPart(subject)
	object = cleanFactPart(object)
	if subject == "" {
		return object
	}
	if object == "" {
		return subject
	}
	return subject + " -> " + object
}

func profileItemsFromCounts(counts map[string]int, predicates map[string]map[string]struct{}, examples map[string]map[string]struct{}, kind string, limit int) []FactIndexItem {
	items := make([]FactIndexItem, 0, len(counts))
	for label, count := range counts {
		item := FactIndexItem{
			Label: label,
			Kind:  kind,
			Count: count,
		}
		item.Predicates = sortedSetValues(predicates[label], 8)
		item.Examples = sortedSetValues(examples[label], 8)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func sortedSetValues(values map[string]struct{}, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for value := range values {
		value = cleanFactPart(value)
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

func scoreSearchResult(result SearchResult, opts SearchOptions, base float64) float64 {
	score := base
	if score < 0 {
		score = 0
	}
	switch result.ResultType {
	case "card":
		score += 0.30
	case "fact":
		score += 0.18
	case "node":
		score += 0.08
	}
	if result.Source.SourceTrust > 0 {
		score += result.Source.SourceTrust * 0.12
	}
	if opts.ProjectPath != "" && result.Source.ProjectPath == opts.ProjectPath {
		score += 0.15
	}
	terms := searchContextTerms(opts)
	if len(terms) > 0 {
		haystack := strings.ToLower(strings.Join([]string{
			result.Source.Title,
			result.Source.TopicHint,
			result.Source.RelativePath,
			result.CardTitle,
			result.NodeTitle,
			result.Claim,
			result.Summary,
			result.Subject,
			result.Predicate,
			result.Object,
			result.Snippet,
		}, "\n"))
		matches := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				matches++
			}
		}
		if matches > 0 {
			score += 0.22 * float64(matches)
		}
	}
	return score
}

func searchContextTerms(opts SearchOptions) []string {
	values := make([]string, 0, 8+len(opts.ContextTerms))
	values = append(values, opts.TopicHint)
	values = append(values, opts.ContextTerms...)
	terms := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, term := range topicSplitter.Split(value, -1) {
			term = strings.ToLower(strings.TrimSpace(term))
			if len([]rune(term)) < 2 {
				continue
			}
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			terms = append(terms, term)
			if len(terms) >= 12 {
				return terms
			}
		}
	}
	return terms
}

func sortSearchResults(results []SearchResult) {
	typePriority := map[string]int{"card": 0, "fact": 1, "node": 2}
	sort.SliceStable(results, func(i, j int) bool {
		pi, ok := typePriority[results[i].ResultType]
		if !ok {
			pi = 9
		}
		pj, ok := typePriority[results[j].ResultType]
		if !ok {
			pj = 9
		}
		if scoreDelta := results[i].Score - results[j].Score; scoreDelta > 0.35 || scoreDelta < -0.35 {
			return results[i].Score > results[j].Score
		}
		if pi != pj {
			return pi < pj
		}
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Citation < results[j].Citation
	})
}

func citationFromResult(result SearchResult) Citation {
	return Citation{
		Label:        formatResultCitation(result),
		SourceID:     result.Source.ID,
		SourceTitle:  result.Source.Title,
		SourceKind:   result.Source.Kind,
		URI:          fallbackText(result.Source.CanonicalURI, result.Source.URI),
		RelativePath: result.Source.RelativePath,
		ResultType:   result.ResultType,
		NodeID:       result.NodeID,
		CardID:       result.CardID,
		FactID:       result.FactID,
		Page:         result.Page,
		SheetName:    result.SheetName,
		RowRange:     result.RowRange,
		ColRange:     result.ColRange,
		Snippet:      result.Snippet,
		Score:        result.Score,
	}
}

func citationKey(citation Citation) string {
	return strings.Join([]string{citation.SourceID, citation.ResultType, citation.NodeID, citation.CardID, citation.FactID, citation.Label}, "\x00")
}

func formatResultCitation(result SearchResult) string {
	parts := []string{sourceCitationLabel(result.Source)}
	if result.Page > 0 {
		parts = append(parts, fmt.Sprintf("page %d", result.Page))
	}
	if result.SheetName != "" {
		parts = append(parts, "sheet "+result.SheetName)
	}
	if result.RowRange != "" {
		parts = append(parts, "rows "+result.RowRange)
	}
	if result.ColRange != "" {
		parts = append(parts, "cols "+result.ColRange)
	}
	if len(parts) == 1 && result.NodeTitle != "" && result.NodeTitle != result.Source.Title {
		parts = append(parts, result.NodeTitle)
	}
	switch result.ResultType {
	case "fact":
		fact := strings.TrimSpace(strings.Join(nonEmptyStrings(result.Subject, result.Predicate, result.Object), " "))
		if fact != "" {
			parts = append(parts, "fact: "+fact)
		}
	case "card":
		if result.CardTitle != "" && result.CardTitle != result.Source.Title && result.CardTitle != result.NodeTitle {
			parts = append(parts, "card: "+result.CardTitle)
		}
	}
	return strings.Join(parts, " | ")
}

func sourceCitationLabel(source Source) string {
	for _, candidate := range []string{source.Title, source.RelativePath, source.CanonicalURI, source.URI, source.ID} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return "unknown source"
}
func appendSearchFilters(where []string, args []interface{}, sourceAlias string, opts SearchOptions) ([]string, []interface{}) {
	prefix := ""
	if sourceAlias != "" {
		prefix = sourceAlias + "."
	}
	tenantID := strings.TrimSpace(opts.TenantID)
	ownerID := strings.TrimSpace(opts.OwnerID)
	projectPath := strings.TrimSpace(opts.ProjectPath)
	if tenantID != "" {
		where = append(where, prefix+"tenant_id = ?")
		args = append(args, tenantID)
	}
	if ownerID != "" {
		where = append(where, prefix+"owner_id = ?")
		args = append(args, ownerID)
	}
	scope := strings.ToLower(strings.TrimSpace(opts.SearchScope))
	switch scope {
	case SaveScopePersonal, SaveScopeLocalOnly, "local":
		where = append(where, "COALESCE("+prefix+"project_path, '') = ''")
	case SaveScopeProject:
		if projectPath != "" {
			where = append(where, prefix+"project_path = ?")
			args = append(args, projectPath)
		}
	default:
		if projectPath != "" {
			where = append(where, prefix+"project_path = ?")
			args = append(args, projectPath)
		}
	}
	kinds := normalizeSearchStrings(opts.SourceKinds)
	if len(kinds) == 1 {
		where = append(where, prefix+"kind = ?")
		args = append(args, kinds[0])
	} else if len(kinds) > 1 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
		where = append(where, prefix+"kind IN ("+placeholders+")")
		for _, kind := range kinds {
			args = append(args, kind)
		}
	}
	sourceIDs := normalizeSearchStrings(append(append([]string{}, opts.SourceIDs...), opts.SourceID))
	if len(sourceIDs) == 1 {
		where = append(where, prefix+"id = ?")
		args = append(args, sourceIDs[0])
	} else if len(sourceIDs) > 1 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(sourceIDs)), ",")
		where = append(where, prefix+"id IN ("+placeholders+")")
		for _, sourceID := range sourceIDs {
			args = append(args, sourceID)
		}
	}
	labels := normalizeSourceLabels(opts.Labels)
	sourceIDExpr := prefix + "id"
	if sourceAlias == "" {
		sourceIDExpr = "knowledge_sources.id"
	}
	for _, label := range labels {
		where = append(where, "EXISTS (SELECT 1 FROM knowledge_source_labels sl WHERE sl.source_id = "+sourceIDExpr+" AND sl.label = ?)")
		args = append(args, label)
	}
	where, args = appendSourceDomainFilter(where, args, sourceAlias, opts.Domain)
	if !opts.IncludeDisabled {
		where = append(where, prefix+"status <> ?")
		args = append(args, StatusDisabled)
	}
	return where, args
}

func appendSourceDomainFilter(where []string, args []interface{}, sourceAlias string, rawDomain string) ([]string, []interface{}) {
	domain := normalizeURLPolicyDomain(rawDomain)
	if domain == "" {
		return where, args
	}
	prefix := ""
	if sourceAlias != "" {
		prefix = sourceAlias + "."
	}
	where = append(where, "(LOWER(COALESCE("+prefix+"site_name, '')) = ? OR LOWER(COALESCE("+prefix+"site_name, '')) LIKE ? OR LOWER(COALESCE("+prefix+"canonical_uri, '')) IN (?, ?) OR LOWER(COALESCE("+prefix+"canonical_uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"canonical_uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"canonical_uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"canonical_uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"canonical_uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"canonical_uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"uri, '')) IN (?, ?) OR LOWER(COALESCE("+prefix+"uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"uri, '')) LIKE ? OR LOWER(COALESCE("+prefix+"uri, '')) LIKE ?)")
	args = append(args,
		domain, "%."+domain,
		"http://"+domain, "https://"+domain,
		"http://"+domain+"/%", "https://"+domain+"/%",
		"http://%."+domain, "https://%."+domain,
		"http://%."+domain+"/%", "https://%."+domain+"/%",
		"http://"+domain, "https://"+domain,
		"http://"+domain+"/%", "https://"+domain+"/%",
		"http://%."+domain, "https://%."+domain,
		"http://%."+domain+"/%", "https://%."+domain+"/%",
	)
	return where, args
}

func normalizeSearchResultTypes(input []string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range input {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "card", "cards", "knowledge_card":
			result["card"] = struct{}{}
		case "fact", "facts", "knowledge_fact":
			result["fact"] = struct{}{}
		case "node", "nodes", "document_node", "source_node":
			result["node"] = struct{}{}
		}
	}
	return result
}

func wantsSearchType(types map[string]struct{}, typ string) bool {
	if len(types) == 0 {
		return true
	}
	_, ok := types[typ]
	return ok
}

func normalizeSearchStrings(input []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(input))
	for _, value := range input {
		for _, part := range strings.FieldsFunc(value, isKnowledgeListSeparator) {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			result = append(result, part)
		}
	}
	return result
}

func isKnowledgeListSeparator(r rune) bool {
	return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == '\uFF0C' || r == '\uFF1B' || r == '\u3001'
}

func (s *SQLiteStore) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	queries := []struct {
		q   string
		dst *int
	}{
		{`SELECT COUNT(*) FROM knowledge_sources`, &stats.Sources},
		{`SELECT COUNT(*) FROM document_nodes`, &stats.DocumentNodes},
		{`SELECT COUNT(*) FROM knowledge_cards`, &stats.Cards},
		{`SELECT COUNT(*) FROM knowledge_facts`, &stats.Facts},
		{`SELECT COUNT(*) FROM knowledge_source_links`, &stats.SourceLinks},
		{`SELECT COUNT(*) FROM knowledge_source_link_events`, &stats.SourceLinkEvents},
		{`SELECT COUNT(*) FROM knowledge_import_batches`, &stats.Batches},
		{`SELECT COUNT(*) FROM knowledge_sources s WHERE s.status IN ('parsed', 'distilled', 'stale') AND NOT EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = s.id)`, &stats.SourcesWithoutNodes},
		{`SELECT COUNT(*) FROM knowledge_sources s WHERE s.status IN ('parsed', 'distilled', 'stale') AND NOT EXISTS (SELECT 1 FROM knowledge_cards c WHERE c.source_id = s.id)`, &stats.SourcesWithoutCards},
		{`SELECT COUNT(*) FROM knowledge_sources s WHERE s.status IN ('distilled', 'stale') AND NOT EXISTS (SELECT 1 FROM knowledge_facts f WHERE f.source_id = s.id)`, &stats.SourcesWithoutFacts},
		{`SELECT COUNT(*) FROM knowledge_sources s WHERE s.status IN ('parsed', 'distilled', 'stale') AND EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = s.id) AND NOT EXISTS (SELECT 1 FROM knowledge_cards c WHERE c.source_id = s.id)`, &stats.SourcesRebuildCards},
		{`SELECT COUNT(*) FROM knowledge_sources s WHERE s.status IN ('distilled', 'stale') AND EXISTS (SELECT 1 FROM document_nodes n WHERE n.source_id = s.id) AND NOT EXISTS (SELECT 1 FROM knowledge_facts f WHERE f.source_id = s.id)`, &stats.SourcesRebuildFacts},
		{`SELECT COUNT(*) FROM knowledge_sources s WHERE s.status IN ('parsed', 'distilled', 'stale') AND NOT EXISTS (SELECT 1 FROM knowledge_source_links l WHERE l.source_id = s.id)`, &stats.SourcesWithoutLinks},
	}
	for _, item := range queries {
		if err := s.db.QueryRowContext(ctx, item.q).Scan(item.dst); err != nil {
			return Stats{}, err
		}
	}
	var err error
	if stats.SourcesByKind, err = s.countBy(ctx, `SELECT COALESCE(kind, ''), COUNT(*) FROM knowledge_sources GROUP BY kind`); err != nil {
		return Stats{}, err
	}
	if stats.SourcesByStatus, err = s.countBy(ctx, `SELECT COALESCE(status, ''), COUNT(*) FROM knowledge_sources GROUP BY status`); err != nil {
		return Stats{}, err
	}
	if stats.SourcesByDomain, err = s.countBy(ctx, `SELECT LOWER(COALESCE(site_name, '')), COUNT(*) FROM knowledge_sources WHERE COALESCE(site_name, '') <> '' GROUP BY LOWER(site_name)`); err != nil {
		return Stats{}, err
	}
	if stats.SourcesByLabel, err = s.countBy(ctx, `SELECT label, COUNT(DISTINCT source_id) FROM knowledge_source_labels GROUP BY label`); err != nil {
		return Stats{}, err
	}
	if stats.LinkEventsByAction, err = s.countBy(ctx, `SELECT COALESCE(action, ''), COUNT(*) FROM knowledge_source_link_events GROUP BY action`); err != nil {
		return Stats{}, err
	}
	if stats.BatchesByStatus, err = s.countBy(ctx, `SELECT COALESCE(status, ''), COUNT(*) FROM knowledge_import_batches GROUP BY status`); err != nil {
		return Stats{}, err
	}
	if stats.ImportItemsByStatus, err = s.countBy(ctx, `SELECT COALESCE(status, ''), COUNT(*) FROM knowledge_import_items GROUP BY status`); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func (s *SQLiteStore) countBy(ctx context.Context, q string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		key = strings.TrimSpace(key)
		if key == "" {
			key = "unknown"
		}
		result[key] = count
	}
	return result, rows.Err()
}

func (s *SQLiteStore) KnownContentHashes(ctx context.Context, req DirectoryImportRequest) (map[string]struct{}, error) {
	where := []string{"content_hash <> ''"}
	args := make([]interface{}, 0)
	if req.TenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, req.TenantID)
	}
	if req.OwnerID != "" {
		where = append(where, "owner_id = ?")
		args = append(args, req.OwnerID)
	}
	if req.ProjectPath != "" && req.SaveScope == SaveScopeProject {
		where = append(where, "project_path = ?")
		args = append(args, req.ProjectPath)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT content_hash FROM knowledge_sources WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		if hash != "" {
			result[hash] = struct{}{}
		}
	}
	return result, rows.Err()
}

func (s *SQLiteStore) ScanDirectory(ctx context.Context, req DirectoryImportRequest) (DirectoryImportResult, error) {
	req = NormalizeDirectoryImportRequest(req)
	existing, err := s.KnownContentHashes(ctx, req)
	if err != nil {
		return DirectoryImportResult{}, err
	}
	result, _, err := ScanDirectory(ctx, req, existing)
	return result, err
}

func (s *SQLiteStore) ListImportBatches(ctx context.Context, limit int) ([]ImportBatch, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, root_path, owner_id, tenant_id, project_path, topic_hint, recursive, include_exts_json, exclude_globs_json, max_file_bytes,
		status, total_files, queued_files, imported_files, skipped_files, failed_files, created_at, updated_at
		FROM knowledge_import_batches ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batches := make([]ImportBatch, 0)
	for rows.Next() {
		batch, err := scanImportBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (s *SQLiteStore) GetImportBatch(ctx context.Context, batchID string) (ImportBatch, error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return ImportBatch{}, fmt.Errorf("batch id is required")
	}
	return scanImportBatch(s.db.QueryRowContext(ctx, `SELECT id, root_path, owner_id, tenant_id, project_path, topic_hint, recursive, include_exts_json, exclude_globs_json, max_file_bytes,
		status, total_files, queued_files, imported_files, skipped_files, failed_files, created_at, updated_at
		FROM knowledge_import_batches WHERE id = ?`, batchID))
}

func (s *SQLiteStore) ListImportItems(ctx context.Context, batchID string, limit int) ([]ImportItem, error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return nil, fmt.Errorf("batch id is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, batch_id, COALESCE(source_id, ''), file_path, relative_path, file_hash, file_size, kind, status, error_message, created_at, updated_at
		FROM knowledge_import_items WHERE batch_id = ? ORDER BY updated_at DESC LIMIT ?`, batchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ImportItem, 0)
	for rows.Next() {
		item, err := scanImportItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) RetryImportBatch(ctx context.Context, req ImportRetryRequest) (DirectoryImportResult, error) {
	req.BatchID = strings.TrimSpace(req.BatchID)
	if req.BatchID == "" {
		return DirectoryImportResult{}, fmt.Errorf("batch id is required")
	}
	batch, err := s.GetImportBatch(ctx, req.BatchID)
	if err != nil {
		return DirectoryImportResult{}, err
	}
	items, err := s.listImportItemsUnbounded(ctx, batch.ID)
	if err != nil {
		return DirectoryImportResult{}, err
	}
	selected := retryImportFilePaths(items, req)
	if len(selected) == 0 {
		return DirectoryImportResult{
			Status:   ImportStatusCompleted,
			RootPath: batch.RootPath,
			Warnings: []string{
				"no retryable import items matched the request",
			},
		}, nil
	}
	importReq := DirectoryImportRequest{
		RootPath:     batch.RootPath,
		OwnerID:      batch.OwnerID,
		TenantID:     batch.TenantID,
		ProjectPath:  batch.ProjectPath,
		TopicHint:    firstNonEmpty(strings.TrimSpace(req.TopicHint), batch.TopicHint),
		DistillMode:  req.DistillMode,
		SaveScope:    SaveScopeProject,
		Recursive:    batch.Recursive,
		IncludeExts:  batch.IncludeExts,
		ExcludeGlobs: batch.ExcludeGlobs,
		MaxFileBytes: batch.MaxFileBytes,
	}
	if len(req.IncludeExts) > 0 {
		importReq.IncludeExts = req.IncludeExts
	}
	if req.MaxFileBytes > 0 {
		importReq.MaxFileBytes = req.MaxFileBytes
	}
	importReq = NormalizeDirectoryImportRequest(importReq)
	result, scanned, err := ScanFiles(ctx, importReq, selected, map[string]struct{}{})
	if err != nil {
		return result, err
	}
	result.Warnings = append(result.Warnings, "retry_from_batch:"+batch.ID)
	return s.importScannedItems(ctx, importReq, result, scanned)
}

func (s *SQLiteStore) listImportItemsUnbounded(ctx context.Context, batchID string) ([]ImportItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, batch_id, COALESCE(source_id, ''), file_path, relative_path, file_hash, file_size, kind, status, error_message, created_at, updated_at
		FROM knowledge_import_items WHERE batch_id = ? ORDER BY updated_at DESC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ImportItem, 0)
	for rows.Next() {
		item, err := scanImportItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func retryImportFilePaths(items []ImportItem, req ImportRetryRequest) []string {
	itemIDs := make(map[string]struct{})
	for _, id := range req.ItemIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			itemIDs[id] = struct{}{}
		}
	}
	statuses := make(map[string]struct{})
	for _, status := range req.Statuses {
		status = strings.TrimSpace(status)
		if status != "" {
			statuses[status] = struct{}{}
		}
	}
	if len(statuses) == 0 {
		statuses[ItemStatusFailed] = struct{}{}
		if req.IncludeSkipped {
			statuses[ItemStatusSkippedType] = struct{}{}
			statuses[ItemStatusSkippedTooLarge] = struct{}{}
			statuses[ItemStatusSkippedDuplicate] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	filePaths := make([]string, 0)
	for _, item := range items {
		if len(itemIDs) > 0 {
			if _, ok := itemIDs[item.ID]; !ok {
				continue
			}
		} else if _, ok := statuses[item.Status]; !ok {
			continue
		}
		path := strings.TrimSpace(item.FilePath)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		filePaths = append(filePaths, path)
	}
	return filePaths
}

func (s *SQLiteStore) ScanFiles(ctx context.Context, req DirectoryImportRequest, filePaths []string) (DirectoryImportResult, error) {
	req = NormalizeDirectoryImportRequest(req)
	existing, err := s.KnownContentHashes(ctx, req)
	if err != nil {
		return DirectoryImportResult{}, err
	}
	result, _, err := ScanFiles(ctx, req, filePaths, existing)
	return result, err
}

func (s *SQLiteStore) ImportDirectory(ctx context.Context, req DirectoryImportRequest) (DirectoryImportResult, error) {
	req = NormalizeDirectoryImportRequest(req)
	existing, err := s.KnownContentHashes(ctx, req)
	if err != nil {
		return DirectoryImportResult{}, err
	}
	result, items, err := ScanDirectory(ctx, req, existing)
	if err != nil {
		return result, err
	}
	return s.importScannedItems(ctx, req, result, items)
}

func (s *SQLiteStore) ImportFiles(ctx context.Context, req DirectoryImportRequest, filePaths []string) (DirectoryImportResult, error) {
	req = NormalizeDirectoryImportRequest(req)
	existing, err := s.KnownContentHashes(ctx, req)
	if err != nil {
		return DirectoryImportResult{}, err
	}
	result, items, err := ScanFiles(ctx, req, filePaths, existing)
	if err != nil {
		return result, err
	}
	return s.importScannedItems(ctx, req, result, items)
}

func (s *SQLiteStore) importScannedItems(ctx context.Context, req DirectoryImportRequest, result DirectoryImportResult, items []ImportItem) (DirectoryImportResult, error) {
	batchID := NewID("kbatch")
	result.BatchID = batchID
	result.Status = ImportStatusRunning

	now := time.Now().UTC()
	batch := ImportBatch{
		ID:           batchID,
		RootPath:     result.RootPath,
		OwnerID:      req.OwnerID,
		TenantID:     req.TenantID,
		ProjectPath:  req.ProjectPath,
		TopicHint:    req.TopicHint,
		Recursive:    req.Recursive,
		IncludeExts:  req.IncludeExts,
		ExcludeGlobs: req.ExcludeGlobs,
		MaxFileBytes: req.MaxFileBytes,
		Status:       ImportStatusRunning,
		TotalFiles:   result.TotalFiles,
		QueuedFiles:  result.QueuedFiles,
		Skipped:      result.SkippedFiles,
		Failed:       result.FailedFiles,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := insertBatch(ctx, tx, batch); err != nil {
		return result, err
	}

	imported := 0
	failed := result.FailedFiles
	processed := 0
	importedSourceIDs := make([]string, 0)
	emitImportProgress := func(current ImportItem) {
		if s == nil || s.importProgress == nil {
			return
		}
		snapshot := result
		snapshot.Status = ImportStatusRunning
		snapshot.ImportedFiles = imported
		snapshot.FailedFiles = failed
		snapshot.ProcessedFiles = processed
		snapshot.CurrentFile = current.RelativePath
		if snapshot.CurrentFile == "" {
			snapshot.CurrentFile = current.FilePath
		}
		// Clear step fields when file is fully processed
		snapshot.CurrentStep = ""
		snapshot.StepProgress = 0
		snapshot.TotalSteps = 0
		snapshot.CurrentStepNum = 0
		snapshot.Items = nil
		s.importProgress(snapshot)
	}
	emitStepProgress := func(current ImportItem, stepName string, stepNum, totalSteps int) {
		if s == nil || s.importProgress == nil {
			return
		}
		snapshot := result
		snapshot.Status = ImportStatusRunning
		snapshot.ImportedFiles = imported
		snapshot.FailedFiles = failed
		snapshot.ProcessedFiles = processed
		snapshot.CurrentFile = current.RelativePath
		if snapshot.CurrentFile == "" {
			snapshot.CurrentFile = current.FilePath
		}
		snapshot.CurrentStep = stepName
		snapshot.CurrentStepNum = stepNum
		snapshot.TotalSteps = totalSteps
		snapshot.StepProgress = (stepNum - 1) * 100 / totalSteps // progress at start of step N = (N-1)/total
		snapshot.Items = nil
		s.importProgress(snapshot)
	}
	markImportItemProcessed := func(index int, item ImportItem) {
		items[index] = item
		processed++
		emitImportProgress(item)
	}
	emitImportProgress(ImportItem{})

	for i := range items {
		item := items[i]
		item.BatchID = batchID
		if item.Status != ItemStatusQueued {
			if err := insertImportItem(ctx, tx, item); err != nil {
				return result, err
			}
			markImportItemProcessed(i, item)
			continue
		}

		source := BuildSourceFromImport(req, batchID, item)
		emitStepProgress(item, "preparing", 1, 5)
		if existingSource, ok, err := findExistingSourceForImport(ctx, tx, req, item); err != nil {
			return result, err
		} else if ok {
			source.ID = existingSource.ID
			source.CreatedAt = existingSource.CreatedAt
			if source.Title == "" {
				source.Title = existingSource.Title
			}
			if source.SourceTrust == 0 {
				source.SourceTrust = existingSource.SourceTrust
			}
			if err := deleteSourceDerivedRows(ctx, tx, existingSource.ID); err != nil {
				return result, err
			}
		}
		item.SourceID = source.ID
		item.Status = ItemStatusImported
		item.UpdatedAt = time.Now().UTC()
		emitStepProgress(item, "saving", 2, 5)
		if err := insertSource(ctx, tx, source); err != nil {
			item.Status = ItemStatusFailed
			item.ErrorMessage = err.Error()
			failed++
			if err := insertImportItem(ctx, tx, item); err != nil {
				return result, err
			}
			markImportItemProcessed(i, item)
			continue
		}
		if err := addSourceLabelsTx(ctx, tx, source.ID, ingestLabelsForSource(source, req.Labels, req.AutoLabels)); err != nil {
			item.Status = ItemStatusFailed
			item.ErrorMessage = err.Error()
			failed++
			if err := insertImportItem(ctx, tx, item); err != nil {
				return result, err
			}
			markImportItemProcessed(i, item)
			continue
		}

		if isImmediatelyParsedKind(item.Kind) {
			emitStepProgress(item, "parsing", 3, 5)
			var nodes []DocumentNode
			nodes, parseErr := ParseDocumentNodes(source, item.FilePath, item.Kind)
			if parseErr != nil {
				if IsUnsupportedParserError(parseErr) {
					source.Status = StatusPending
					if err := insertSource(ctx, tx, source); err != nil {
						return result, err
					}
				} else {
					source.Status = StatusFailed
					source.ErrorMessage = parseErr.Error()
					if err := insertSource(ctx, tx, source); err != nil {
						return result, err
					}
					item.Status = ItemStatusFailed
					item.ErrorMessage = parseErr.Error()
					failed++
					if err := insertImportItem(ctx, tx, item); err != nil {
						return result, err
					}
					markImportItemProcessed(i, item)
					continue
				}
			}
			if len(nodes) > 0 {
				// Extract embedded images from the document (DOCX/PPTX/PDF).
				if imageNodes := s.ExtractAndProcessDocumentImages(ctx, source, item.FilePath, item.Kind, nodes); len(imageNodes) > 0 {
					nodes = append(nodes, imageNodes...)
					source.NodeCount = len(nodes)
				}
				emitStepProgress(item, "indexing", 4, 5)
				if err := insertDocumentNodes(ctx, tx, nodes); err != nil {
					source.Status = StatusFailed
					source.ErrorMessage = err.Error()
					if saveErr := insertSource(ctx, tx, source); saveErr != nil {
						return result, saveErr
					}
					item.Status = ItemStatusFailed
					item.ErrorMessage = err.Error()
					failed++
					if err := insertImportItem(ctx, tx, item); err != nil {
						return result, err
					}
					markImportItemProcessed(i, item)
					continue
				}
				emitStepProgress(item, "distilling", 5, 5)
				nextSource, err := s.DistillAndSaveCardsWithMode(ctx, tx, source, nodes, req.DistillMode)
				if err != nil {
					source.Status = StatusFailed
					source.ErrorMessage = err.Error()
					if saveErr := insertSource(ctx, tx, source); saveErr != nil {
						return result, saveErr
					}
					item.Status = ItemStatusFailed
					item.ErrorMessage = err.Error()
					failed++
					if err := insertImportItem(ctx, tx, item); err != nil {
						return result, err
					}
					markImportItemProcessed(i, item)
					continue
				}
				source = nextSource
			} else if item.Kind == SourceKindImage && parseErr == nil {
				// Standalone image: no text nodes from parsing, process as image.
				emitStepProgress(item, "processing image", 4, 5)
				imageNodes := s.ProcessStandaloneImage(ctx, source, item.FilePath, nil)
				if len(imageNodes) > 0 {
					if err := insertDocumentNodes(ctx, tx, imageNodes); err != nil {
						source.Status = StatusFailed
						source.ErrorMessage = err.Error()
						if saveErr := insertSource(ctx, tx, source); saveErr != nil {
							return result, saveErr
						}
						item.Status = ItemStatusFailed
						item.ErrorMessage = err.Error()
						failed++
						if err := insertImportItem(ctx, tx, item); err != nil {
							return result, err
						}
						markImportItemProcessed(i, item)
						continue
					}
					source.NodeCount = len(imageNodes)
					emitStepProgress(item, "distilling", 5, 5)
					nextSource, err := s.DistillAndSaveCardsWithMode(ctx, tx, source, imageNodes, req.DistillMode)
					if err != nil {
						source.Status = StatusFailed
						source.ErrorMessage = err.Error()
						if saveErr := insertSource(ctx, tx, source); saveErr != nil {
							return result, saveErr
						}
						item.Status = ItemStatusFailed
						item.ErrorMessage = err.Error()
						failed++
						if err := insertImportItem(ctx, tx, item); err != nil {
							return result, err
						}
						markImportItemProcessed(i, item)
						continue
					}
					source = nextSource
				}
			}
		}

		if err := insertSourceVersionTx(ctx, tx, source, "import"); err != nil {
			return result, err
		}
		imported++
		importedSourceIDs = append(importedSourceIDs, source.ID)
		if err := insertImportItem(ctx, tx, item); err != nil {
			return result, err
		}
		markImportItemProcessed(i, item)
	}

	result.ImportedFiles = imported
	result.FailedFiles = failed
	result.ProcessedFiles = processed
	result.CurrentFile = ""
	if failed > 0 {
		result.Status = ImportStatusFailed
		batch.Status = ImportStatusFailed
	} else {
		result.Status = ImportStatusCompleted
		batch.Status = ImportStatusCompleted
	}
	batch.Imported = imported
	batch.Failed = failed
	batch.UpdatedAt = time.Now().UTC()
	if err := updateBatchCompletion(ctx, tx, batch); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	for _, sourceID := range importedSourceIDs {
		_, _ = s.RefreshSourceTopicLinks(ctx, sourceID, 8)
	}
	result.Items = items
	return result, nil
}

const insertSourceSQL = `INSERT INTO knowledge_sources
	(id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash, owner_id, tenant_id,
	 project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		kind = excluded.kind,
		uri = excluded.uri,
		canonical_uri = excluded.canonical_uri,
		title = excluded.title,
		author = excluded.author,
		site_name = excluded.site_name,
		published_at = excluded.published_at,
		fetched_at = excluded.fetched_at,
		content_hash = excluded.content_hash,
		owner_id = excluded.owner_id,
		tenant_id = excluded.tenant_id,
		project_path = excluded.project_path,
		topic_hint = excluded.topic_hint,
		source_trust = excluded.source_trust,
		batch_id = excluded.batch_id,
		relative_path = excluded.relative_path,
		status = excluded.status,
		error_message = excluded.error_message,
		created_at = knowledge_sources.created_at,
		updated_at = excluded.updated_at`

const insertFactSQL = `INSERT OR REPLACE INTO knowledge_facts
	(id, card_id, source_id, subject, predicate, object, negated, valid_at, invalid_at, confidence)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const insertCardSQL = `INSERT INTO knowledge_cards
	(id, source_id, node_id, title, claim, summary, entities_json, topics_json, tags_json, project_path, owner_id, tenant_id,
	 valid_at, invalid_at, confidence, importance, source_trust, embedding, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		source_id = excluded.source_id,
		node_id = excluded.node_id,
		title = excluded.title,
		claim = excluded.claim,
		summary = excluded.summary,
		entities_json = excluded.entities_json,
		topics_json = excluded.topics_json,
		tags_json = excluded.tags_json,
		project_path = excluded.project_path,
		owner_id = excluded.owner_id,
		tenant_id = excluded.tenant_id,
		valid_at = excluded.valid_at,
		invalid_at = excluded.invalid_at,
		confidence = excluded.confidence,
		importance = excluded.importance,
		source_trust = excluded.source_trust,
		embedding = excluded.embedding,
		created_at = knowledge_cards.created_at,
		updated_at = excluded.updated_at`

func insertSource(ctx context.Context, tx *sql.Tx, source Source) error {
	source = normalizeSource(source)
	_, err := tx.ExecContext(ctx, insertSourceSQL,
		source.ID, source.Kind, source.URI, source.CanonicalURI, source.Title, source.Author, source.SiteName,
		formatTime(source.PublishedAt), formatTime(source.FetchedAt), source.ContentHash, source.OwnerID, source.TenantID,
		source.ProjectPath, source.TopicHint, source.SourceTrust, source.BatchID, source.RelativePath, source.Status,
		source.ErrorMessage, formatTime(source.CreatedAt), formatTime(source.UpdatedAt),
	)
	return err
}

func insertDocumentNode(ctx context.Context, tx *sql.Tx, node DocumentNode) error {
	if node.ID == "" {
		node.ID = NewID("kdn")
	}
	meta, _ := json.Marshal(node.Metadata)
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO document_nodes
		(id, source_id, parent_id, type, title, text, level, page, sheet_name, row_range, col_range, xpath, offset, metadata_json, token_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.SourceID, node.ParentID, node.Type, node.Title, node.Text, node.Level, node.Page, node.SheetName,
		node.RowRange, node.ColRange, node.XPath, node.Offset, string(meta), node.TokenCount,
	)
	if err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM document_nodes_fts WHERE node_id = ?`, node.ID)
	_, _ = tx.ExecContext(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`, node.ID, segmentTextForFTS(node.Title), segmentTextForFTS(node.Text))
	return nil
}

func insertDocumentNodes(ctx context.Context, tx *sql.Tx, nodes []DocumentNode) error {
	for _, node := range nodes {
		if err := insertDocumentNode(ctx, tx, node); err != nil {
			return err
		}
	}
	return nil
}

func insertCard(ctx context.Context, tx *sql.Tx, card Card) error {
	if card.ID == "" {
		card.ID = NewID("kcard")
	}
	now := time.Now().UTC()
	if card.CreatedAt.IsZero() {
		card.CreatedAt = now
	}
	if card.UpdatedAt.IsZero() {
		card.UpdatedAt = now
	}
	if card.Confidence <= 0 {
		card.Confidence = 0.5
	}
	if card.Importance <= 0 {
		card.Importance = 1
	}
	if card.SourceTrust <= 0 {
		card.SourceTrust = 0.5
	}
	entitiesJSON, _ := json.Marshal(card.Entities)
	topicsJSON, _ := json.Marshal(card.Topics)
	tagsJSON, _ := json.Marshal(card.Tags)
	var embBlob interface{}
	if len(card.Embedding) > 0 {
		embBlob = float32SliceToBytes(card.Embedding)
	}
	_, err := tx.ExecContext(ctx, insertCardSQL,
		card.ID, card.SourceID, nullableString(card.NodeID), card.Title, card.Claim, card.Summary, string(entitiesJSON), string(topicsJSON), string(tagsJSON),
		card.ProjectPath, card.OwnerID, card.TenantID, formatTime(card.ValidAt), formatTime(card.InvalidAt), card.Confidence, card.Importance,
		card.SourceTrust, embBlob, formatTime(card.CreatedAt), formatTime(card.UpdatedAt),
	)
	if err != nil {
		return err
	}
	ftsSummary := cardFTSSummary(card)
	_, _ = tx.ExecContext(ctx, `DELETE FROM knowledge_cards_fts WHERE card_id = ?`, card.ID)
	_, _ = tx.ExecContext(ctx, `INSERT INTO knowledge_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`, card.ID, segmentTextForFTS(card.Title), segmentTextForFTS(card.Claim), segmentTextForFTS(ftsSummary))
	return nil
}

func insertFact(ctx context.Context, tx *sql.Tx, fact Fact) error {
	if fact.ID == "" {
		fact.ID = NewID("kfact")
	}
	if fact.Confidence <= 0 {
		fact.Confidence = 0.5
	}
	_, err := tx.ExecContext(ctx, insertFactSQL,
		fact.ID, fact.CardID, fact.SourceID, fact.Subject, fact.Predicate, fact.Object, boolInt(fact.Negated),
		formatTime(fact.ValidAt), formatTime(fact.InvalidAt), fact.Confidence,
	)
	if err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM knowledge_facts_fts WHERE fact_id = ?`, fact.ID)
	_, _ = tx.ExecContext(ctx, `INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`, fact.ID, segmentTextForFTS(fact.Subject), segmentTextForFTS(fact.Predicate), segmentTextForFTS(fact.Object))
	return nil
}

func cardFTSSummary(card Card) string {
	parts := []string{card.Summary}
	if len(card.Entities) > 0 {
		parts = append(parts, strings.Join(card.Entities, " "))
	}
	if len(card.Topics) > 0 {
		parts = append(parts, strings.Join(card.Topics, " "))
	}
	if len(card.Tags) > 0 {
		parts = append(parts, strings.Join(card.Tags, " "))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func deleteSourceDerivedRows(ctx context.Context, tx *sql.Tx, sourceID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_nodes_fts WHERE node_id IN (SELECT id FROM document_nodes WHERE source_id = ?)`, sourceID); err != nil {
		return err
	}
	if err := deleteSourceCardsAndFacts(ctx, tx, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_nodes WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	return nil
}

func deleteSourceCardsAndFacts(ctx context.Context, tx *sql.Tx, sourceID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_cards_fts WHERE card_id IN (SELECT id FROM knowledge_cards WHERE source_id = ?)`, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_facts_fts WHERE fact_id IN (SELECT id FROM knowledge_facts WHERE source_id = ?)`, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_facts WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_cards WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	return nil
}

func findExistingSourceForImport(ctx context.Context, tx *sql.Tx, req DirectoryImportRequest, item ImportItem) (Source, bool, error) {
	path := strings.TrimSpace(item.FilePath)
	if path == "" {
		return Source{}, false, nil
	}
	q := `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources
		WHERE uri = ? AND kind = ? AND COALESCE(owner_id, '') = ? AND COALESCE(tenant_id, '') = ? AND COALESCE(project_path, '') = ?
		ORDER BY updated_at DESC LIMIT 1`
	source, err := scanSource(tx.QueryRowContext(ctx, q, path, item.Kind, req.OwnerID, req.TenantID, req.ProjectPath))
	if err != nil {
		if err == sql.ErrNoRows {
			return Source{}, false, nil
		}
		return Source{}, false, err
	}
	return source, true, nil
}

func insertBatch(ctx context.Context, tx *sql.Tx, batch ImportBatch) error {
	includeJSON, _ := json.Marshal(batch.IncludeExts)
	excludeJSON, _ := json.Marshal(batch.ExcludeGlobs)
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO knowledge_import_batches
		(id, root_path, owner_id, tenant_id, project_path, topic_hint, recursive, include_exts_json, exclude_globs_json, max_file_bytes,
		 status, total_files, queued_files, imported_files, skipped_files, failed_files, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		batch.ID, batch.RootPath, batch.OwnerID, batch.TenantID, batch.ProjectPath, batch.TopicHint, boolInt(batch.Recursive), string(includeJSON), string(excludeJSON), batch.MaxFileBytes,
		batch.Status, batch.TotalFiles, batch.QueuedFiles, batch.Imported, batch.Skipped, batch.Failed, formatTime(batch.CreatedAt), formatTime(batch.UpdatedAt),
	)
	return err
}

func updateBatchCompletion(ctx context.Context, tx *sql.Tx, batch ImportBatch) error {
	_, err := tx.ExecContext(ctx, `UPDATE knowledge_import_batches
		SET status = ?, imported_files = ?, failed_files = ?, updated_at = ? WHERE id = ?`,
		batch.Status, batch.Imported, batch.Failed, formatTime(batch.UpdatedAt), batch.ID,
	)
	return err
}

func insertImportItem(ctx context.Context, tx *sql.Tx, item ImportItem) error {
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO knowledge_import_items
		(id, batch_id, source_id, file_path, relative_path, file_hash, file_size, kind, status, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.BatchID, nullableString(item.SourceID), item.FilePath, item.RelativePath, item.FileHash, item.FileSize, item.Kind, item.Status, item.ErrorMessage,
		formatTime(item.CreatedAt), formatTime(item.UpdatedAt),
	)
	return err
}

func insertSourceVersionTx(ctx context.Context, tx *sql.Tx, source Source, reason string) error {
	source = normalizeSource(source)
	now := time.Now().UTC()
	nodeCount, err := countSourceRowsTx(ctx, tx, "document_nodes", source.ID)
	if err != nil {
		return err
	}
	cardCount, err := countSourceRowsTx(ctx, tx, "knowledge_cards", source.ID)
	if err != nil {
		return err
	}
	factCount, err := countSourceRowsTx(ctx, tx, "knowledge_facts", source.ID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_source_versions
		(id, source_id, kind, uri, canonical_uri, title, content_hash, status, reason, fetched_at, node_count, card_count, fact_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NewID("ksver"), source.ID, source.Kind, source.URI, source.CanonicalURI, source.Title, source.ContentHash,
		source.Status, strings.TrimSpace(reason), formatTime(source.FetchedAt), nodeCount, cardCount, factCount, formatTime(now),
	)
	return err
}

func insertSourceVersionRecordTx(ctx context.Context, tx *sql.Tx, version SourceVersion) error {
	version.ID = strings.TrimSpace(version.ID)
	version.SourceID = strings.TrimSpace(version.SourceID)
	if version.ID == "" {
		version.ID = NewID("ksver")
	}
	if version.SourceID == "" {
		return fmt.Errorf("source version source id is required")
	}
	if version.CreatedAt.IsZero() {
		version.CreatedAt = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_source_versions
		(id, source_id, kind, uri, canonical_uri, title, content_hash, status, reason, fetched_at, node_count, card_count, fact_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			source_id = excluded.source_id,
			kind = excluded.kind,
			uri = excluded.uri,
			canonical_uri = excluded.canonical_uri,
			title = excluded.title,
			content_hash = excluded.content_hash,
			status = excluded.status,
			reason = excluded.reason,
			fetched_at = excluded.fetched_at,
			node_count = excluded.node_count,
			card_count = excluded.card_count,
			fact_count = excluded.fact_count,
			created_at = excluded.created_at`,
		version.ID, version.SourceID, version.Kind, version.URI, version.CanonicalURI, version.Title, version.ContentHash,
		version.Status, version.Reason, formatTime(version.FetchedAt), version.NodeCount, version.CardCount, version.FactCount, formatTime(version.CreatedAt),
	)
	return err
}

func countSourceRowsTx(ctx context.Context, tx *sql.Tx, table string, sourceID string) (int, error) {
	switch table {
	case "document_nodes", "knowledge_cards", "knowledge_facts":
	default:
		return 0, fmt.Errorf("unsupported source count table %q", table)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE source_id = ?`, sourceID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func normalizeSourceLabels(values []string) []string {
	seen := map[string]struct{}{}
	labels := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, isKnowledgeListSeparator) {
			label := strings.ToLower(strings.TrimSpace(part))
			if label == "" {
				continue
			}
			label = normalizeWhitespace(label)
			runes := []rune(label)
			if len(runes) > 64 {
				label = string(runes[:64])
			}
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
	}
	return labels
}

func sourceLabelUpdateMode(req SourceLabelUpdateRequest) string {
	if req.ClearLabels {
		return "clear"
	}
	if firstNormalizedSourceLabel(req.RenameFrom) != "" && firstNormalizedSourceLabel(req.RenameTo) != "" {
		return "rename"
	}
	if len(req.ReplaceLabels) > 0 {
		return "replace"
	}
	if len(req.AddLabels) > 0 && len(req.RemoveLabels) > 0 {
		return "add_remove"
	}
	if len(req.AddLabels) > 0 {
		return "add"
	}
	if len(req.RemoveLabels) > 0 {
		return "remove"
	}
	return "noop"
}

func nextSourceLabels(before []string, addLabels []string, removeLabels []string, replaceLabels []string, renameFrom string, renameTo string, req SourceLabelUpdateRequest) []string {
	if req.ClearLabels {
		return []string{}
	}
	if len(req.ReplaceLabels) > 0 {
		return replaceLabels
	}
	if renameFrom != "" && renameTo != "" {
		removeLabels = append(removeLabels, renameFrom)
		addLabels = append(addLabels, renameTo)
	}
	remove := make(map[string]struct{}, len(removeLabels))
	for _, label := range removeLabels {
		remove[label] = struct{}{}
	}
	next := make([]string, 0, len(before)+len(addLabels))
	seen := map[string]struct{}{}
	for _, label := range before {
		if _, drop := remove[label]; drop {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		next = append(next, label)
	}
	for _, label := range addLabels {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		next = append(next, label)
	}
	return next
}

func firstNormalizedSourceLabel(value string) string {
	labels := normalizeSourceLabels([]string{value})
	if len(labels) == 0 {
		return ""
	}
	return labels[0]
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasListSourceFilters(opts ListSourcesOptions) bool {
	return strings.TrimSpace(opts.OwnerID) != "" ||
		strings.TrimSpace(opts.TenantID) != "" ||
		strings.TrimSpace(opts.SearchScope) != "" ||
		strings.TrimSpace(opts.ProjectPath) != "" ||
		strings.TrimSpace(opts.Status) != "" ||
		strings.TrimSpace(opts.Kind) != "" ||
		len(opts.SourceKinds) > 0 ||
		strings.TrimSpace(opts.Domain) != "" ||
		strings.TrimSpace(opts.Label) != "" ||
		len(opts.Labels) > 0 ||
		strings.TrimSpace(opts.Query) != "" ||
		strings.TrimSpace(opts.CoverageFilter) != ""
}

func summarizeSourceLabels(sources []Source) []SourceLabelSummary {
	index := map[string]int{}
	summaries := make([]SourceLabelSummary, 0)
	for _, source := range sources {
		for _, label := range normalizeSourceLabels(source.Labels) {
			i, ok := index[label]
			if !ok {
				index[label] = len(summaries)
				summaries = append(summaries, SourceLabelSummary{Label: label})
				i = len(summaries) - 1
			}
			summaries[i].Count++
			if len(summaries[i].SourceIDs) < 5 {
				summaries[i].SourceIDs = append(summaries[i].SourceIDs, source.ID)
			}
			name := strings.TrimSpace(source.Title)
			if name == "" {
				name = strings.TrimSpace(source.RelativePath)
			}
			if name == "" {
				name = strings.TrimSpace(source.CanonicalURI)
			}
			if name == "" {
				name = strings.TrimSpace(source.URI)
			}
			if name != "" && len(summaries[i].SourceNames) < 5 {
				summaries[i].SourceNames = append(summaries[i].SourceNames, name)
			}
		}
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Count != summaries[j].Count {
			return summaries[i].Count > summaries[j].Count
		}
		return summaries[i].Label < summaries[j].Label
	})
	return summaries
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

type sourceScanner interface {
	Scan(dest ...interface{}) error
}

func scanImportBatch(row sourceScanner) (ImportBatch, error) {
	var batch ImportBatch
	var recursive int
	var includeJSON, excludeJSON string
	var createdAt, updatedAt string
	err := row.Scan(&batch.ID, &batch.RootPath, &batch.OwnerID, &batch.TenantID, &batch.ProjectPath, &batch.TopicHint, &recursive, &includeJSON, &excludeJSON, &batch.MaxFileBytes,
		&batch.Status, &batch.TotalFiles, &batch.QueuedFiles, &batch.Imported, &batch.Skipped, &batch.Failed, &createdAt, &updatedAt)
	if err != nil {
		return ImportBatch{}, err
	}
	batch.Recursive = recursive != 0
	_ = json.Unmarshal([]byte(includeJSON), &batch.IncludeExts)
	_ = json.Unmarshal([]byte(excludeJSON), &batch.ExcludeGlobs)
	batch.CreatedAt = parseTime(createdAt)
	batch.UpdatedAt = parseTime(updatedAt)
	return batch, nil
}

func scanImportItem(row sourceScanner) (ImportItem, error) {
	var item ImportItem
	var createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.BatchID, &item.SourceID, &item.FilePath, &item.RelativePath, &item.FileHash, &item.FileSize, &item.Kind, &item.Status, &item.ErrorMessage, &createdAt, &updatedAt)
	if err != nil {
		return ImportItem{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanDocumentNode(row sourceScanner) (DocumentNode, error) {
	var node DocumentNode
	var metadataJSON string
	err := row.Scan(&node.ID, &node.SourceID, &node.ParentID, &node.Type, &node.Title, &node.Text, &node.Level, &node.Page,
		&node.SheetName, &node.RowRange, &node.ColRange, &node.XPath, &node.Offset, &metadataJSON, &node.TokenCount)
	if err != nil {
		return DocumentNode{}, err
	}
	metadataJSON = strings.TrimSpace(metadataJSON)
	if metadataJSON != "" && metadataJSON != "null" {
		_ = json.Unmarshal([]byte(metadataJSON), &node.Metadata)
	}
	return node, nil
}

func scanCard(row sourceScanner) (Card, error) {
	var card Card
	var entitiesJSON, topicsJSON, tagsJSON string
	var validAt, invalidAt, createdAt, updatedAt string
	err := row.Scan(&card.ID, &card.SourceID, &card.NodeID, &card.Title, &card.Claim, &card.Summary, &entitiesJSON, &topicsJSON, &tagsJSON,
		&card.ProjectPath, &card.OwnerID, &card.TenantID, &validAt, &invalidAt, &card.Confidence, &card.Importance, &card.SourceTrust, &createdAt, &updatedAt)
	if err != nil {
		return Card{}, err
	}
	_ = json.Unmarshal([]byte(entitiesJSON), &card.Entities)
	_ = json.Unmarshal([]byte(topicsJSON), &card.Topics)
	_ = json.Unmarshal([]byte(tagsJSON), &card.Tags)
	card.ValidAt = parseTime(validAt)
	card.InvalidAt = parseTime(invalidAt)
	card.CreatedAt = parseTime(createdAt)
	card.UpdatedAt = parseTime(updatedAt)
	return card, nil
}

func scanFact(row sourceScanner) (Fact, error) {
	var fact Fact
	var negated int
	var validAt, invalidAt string
	err := row.Scan(&fact.ID, &fact.CardID, &fact.SourceID, &fact.Subject, &fact.Predicate, &fact.Object, &negated, &validAt, &invalidAt, &fact.Confidence)
	if err != nil {
		return Fact{}, err
	}
	fact.Negated = negated != 0
	fact.ValidAt = parseTime(validAt)
	fact.InvalidAt = parseTime(invalidAt)
	return fact, nil
}

func scanSource(row sourceScanner) (Source, error) {
	var s Source
	var publishedAt, fetchedAt, createdAt, updatedAt string
	err := row.Scan(&s.ID, &s.Kind, &s.URI, &s.CanonicalURI, &s.Title, &s.Author, &s.SiteName, &publishedAt, &fetchedAt,
		&s.ContentHash, &s.OwnerID, &s.TenantID, &s.ProjectPath, &s.TopicHint, &s.SourceTrust, &s.BatchID, &s.RelativePath,
		&s.Status, &s.ErrorMessage, &createdAt, &updatedAt)
	if err != nil {
		return Source{}, err
	}
	s.PublishedAt = parseTime(publishedAt)
	s.FetchedAt = parseTime(fetchedAt)
	s.CreatedAt = parseTime(createdAt)
	s.UpdatedAt = parseTime(updatedAt)
	return s, nil
}

func scanSourceVersion(row sourceScanner) (SourceVersion, error) {
	var version SourceVersion
	var fetchedAt, createdAt string
	err := row.Scan(&version.ID, &version.SourceID, &version.Kind, &version.URI, &version.CanonicalURI, &version.Title,
		&version.ContentHash, &version.Status, &version.Reason, &fetchedAt,
		&version.NodeCount, &version.CardCount, &version.FactCount, &createdAt)
	if err != nil {
		return SourceVersion{}, err
	}
	version.FetchedAt = parseTime(fetchedAt)
	version.CreatedAt = parseTime(createdAt)
	return version, nil
}

func scanSourceWithPrefix(row sourceScanner, nodeID, nodeTitle, nodeType, snippet *string, rank *float64) (Source, error) {
	var s Source
	var publishedAt, fetchedAt, createdAt, updatedAt string
	err := row.Scan(nodeID, nodeTitle, nodeType,
		&s.ID, &s.Kind, &s.URI, &s.CanonicalURI, &s.Title, &s.Author, &s.SiteName, &publishedAt, &fetchedAt,
		&s.ContentHash, &s.OwnerID, &s.TenantID, &s.ProjectPath, &s.TopicHint, &s.SourceTrust, &s.BatchID, &s.RelativePath,
		&s.Status, &s.ErrorMessage, &createdAt, &updatedAt, snippet, rank)
	if err != nil {
		return Source{}, err
	}
	s.PublishedAt = parseTime(publishedAt)
	s.FetchedAt = parseTime(fetchedAt)
	s.CreatedAt = parseTime(createdAt)
	s.UpdatedAt = parseTime(updatedAt)
	return s, nil
}

func buildFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	terms := strings.Fields(query)
	if len(terms) <= 1 {
		return quoteFTSTerm(query)
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term != "" {
			parts = append(parts, quoteFTSTerm(term))
		}
	}
	return strings.Join(parts, " AND ")
}

func quoteFTSTerm(term string) string {
	term = strings.ReplaceAll(term, `"`, `""`)
	return `"` + term + `"`
}

// searchCJKLikeFallback performs a LIKE-based search when FTS fails for CJK text.
// It tokenizes the query using gse and searches for each meaningful term individually,
// then scores results by how many terms matched.
func (s *SQLiteStore) searchCJKLikeFallback(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, nil
	}

	// Extract meaningful search terms from the query using gse tokenization.
	// For LIKE search, we use two sources of terms:
	// 1. gse tokenization (multi-char words: "马勇", "博士", "著译")
	// 2. Individual CJK characters from the original query ("书", "译")
	// Source 2 is critical: LIKE '%书%' matches "书籍" in the original text,
	// which gse may not produce as a standalone token. This is the mechanism
	// that bridges the gap between user's word choice and document's phrasing.
	//
	// Term priority: gse compound words first (most specific), then single CJK
	// chars that are NOT already covered by a compound word. This ensures the
	// cap doesn't cut high-value single chars like "书" which may be the only
	// term that matches the target text.
	var terms []string
	if containsCJKRunes(query) {
		seen := make(map[string]struct{})

		// Source 1 (high priority): individual CJK characters from the raw query.
		// LIKE '%书%' matches "书籍", '%译%' matches "一译" — single chars have
		// the broadest recall and must not be truncated by the cap.
		for _, r := range query {
			if isCJK(r) && !isCJKStopChar(r) {
				ch := string(r)
				if _, ok := seen[ch]; ok {
					continue
				}
				seen[ch] = struct{}{}
				terms = append(terms, ch)
			}
		}

		// Source 2 (supplementary): gse multi-char tokens (compound words).
		// These provide more specific matching ("马勇" vs just "马"+"勇").
		// Only keep tokens ≥2 chars that are substrings of the original query
		// (filters out ngram noise like "勇博" that spans word boundaries).
		tokens := bm25.Tokenize(query)
		queryLower := strings.ToLower(query)
		for _, t := range tokens {
			t = strings.TrimSpace(t)
			if t == "" || len([]rune(t)) < 2 {
				continue
			}
			if !strings.Contains(queryLower, strings.ToLower(t)) {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		terms = []string{query}
	}
	// Limit total LIKE conditions to prevent query explosion.
	// Strategy: keep all single-char terms (high recall, max ~10 CJK chars in a query)
	// and cap multi-char terms (lower priority since single chars subsume their matching).
	// Final cap ensures total doesn't exceed database performance budget.
	const maxTotalTerms = 12
	if len(terms) > maxTotalTerms {
		terms = terms[:maxTotalTerms]
	}

	// Split terms by length for precision control:
	// - multiCharTerms (≥2 chars): higher precision, used for cards/facts LIKE
	// - allTerms (including single CJK chars): higher recall, used for nodes LIKE
	// Single-char LIKE on cards produces too much noise (e.g., '%士%' matches every
	// card mentioning "博士", "硕士", "战士"). Node full-text search benefits from
	// single chars because the target info may only share one character with the query.
	var multiCharTerms []string
	for _, t := range terms {
		if len([]rune(t)) >= 2 {
			multiCharTerms = append(multiCharTerms, t)
		}
	}
	if len(multiCharTerms) == 0 {
		// All terms are single-char — use them for cards too (no alternative)
		multiCharTerms = terms
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}

	results := make([]SearchResult, 0, limit)

	// Build OR-based LIKE conditions for given terms and columns
	buildLikeWhereWith := func(columns []string, termsToUse []string) (string, []interface{}) {
		var conditions []string
		var args []interface{}
		for _, col := range columns {
			for _, term := range termsToUse {
				conditions = append(conditions, col+" LIKE ?")
				args = append(args, "%"+term+"%")
			}
		}
		return "(" + strings.Join(conditions, " OR ") + ")", args
	}

	// Search cards by claim/title LIKE — use multi-char terms only for precision
	{
		likeExpr, likeArgs := buildLikeWhereWith([]string{"c.claim", "c.title", "c.summary"}, multiCharTerms)
		cardWhere := []string{likeExpr, "NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)"}
		cardArgs := append([]interface{}{}, likeArgs...)
		cardWhere, cardArgs = appendSearchFilters(cardWhere, cardArgs, "s", opts)
		cardArgs = append(cardArgs, limit)
		cardQuery := `SELECT c.id, COALESCE(c.node_id, ''), c.title, c.claim, c.summary,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		WHERE ` + strings.Join(cardWhere, " AND ") + ` ORDER BY c.importance DESC, c.updated_at DESC LIMIT ?`
		rows, err := s.db.QueryContext(ctx, cardQuery, cardArgs...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var result SearchResult
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			if err := rows.Scan(&result.CardID, &result.NodeID, &result.CardTitle, &result.Claim, &result.Summary,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			result.Source = source
			result.ResultType = "card"
			result.Snippet = result.Claim
			result.Score = 2.0 // LIKE fallback gets a fixed moderate score
			result.Citation = formatResultCitation(result)
			results = append(results, result)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Search facts by subject/object LIKE — use multi-char terms only for precision
	if len(results) < limit {
		likeExpr, likeArgs := buildLikeWhereWith([]string{"f.subject", "f.object"}, multiCharTerms)
		factWhere := []string{likeExpr, "NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)"}
		factArgs := append([]interface{}{}, likeArgs...)
		factWhere, factArgs = appendSearchFilters(factWhere, factArgs, "s", opts)
		factArgs = append(factArgs, limit-len(results))
		factQuery := `SELECT f.id, f.card_id, f.subject, f.predicate, f.object, c.title, COALESCE(c.node_id, ''), c.claim, c.summary,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM knowledge_facts f
		JOIN knowledge_cards c ON c.id = f.card_id
		JOIN knowledge_sources s ON s.id = f.source_id
		WHERE ` + strings.Join(factWhere, " AND ") + ` ORDER BY f.confidence DESC LIMIT ?`
		rows, err := s.db.QueryContext(ctx, factQuery, factArgs...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var result SearchResult
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			if err := rows.Scan(&result.FactID, &result.CardID, &result.Subject, &result.Predicate, &result.Object, &result.CardTitle, &result.NodeID, &result.Claim, &result.Summary,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			result.Source = source
			result.ResultType = "fact"
			result.Snippet = strings.TrimSpace(result.Subject + " " + result.Predicate + " " + result.Object)
			result.Score = 2.0
			result.Citation = formatResultCitation(result)
			results = append(results, result)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Search nodes by text LIKE — always search document_nodes regardless of
	// card/fact results above. Uses ALL terms (including single CJK chars) for
	// maximum recall against the original document full text. This is the
	// root-cause fix for distillation loss: LIKE '%书%' matches "书籍" in the
	// original text even when FTS and card LIKE cannot.
	{
		nodeLimit := limit
		if nodeLimit < 3 {
			nodeLimit = 3
		}
		likeExpr, likeArgs := buildLikeWhereWith([]string{"n.text", "n.title"}, terms)
		nodeWhere := []string{likeExpr}
		nodeArgs := append([]interface{}{}, likeArgs...)
		nodeWhere, nodeArgs = appendSearchFilters(nodeWhere, nodeArgs, "s", opts)
		nodeArgs = append(nodeArgs, nodeLimit)
		nodeQuery := `SELECT n.id, n.title, n.type, n.text, n.page,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM document_nodes n
		JOIN knowledge_sources s ON s.id = n.source_id
		WHERE ` + strings.Join(nodeWhere, " AND ") + ` ORDER BY n.page ASC LIMIT ?`
		rows, err := s.db.QueryContext(ctx, nodeQuery, nodeArgs...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var result SearchResult
			var source Source
			var publishedAt, fetchedAt, createdAt, updatedAt string
			var nodeText string
			if err := rows.Scan(&result.NodeID, &result.NodeTitle, &result.NodeType, &nodeText, &result.Page,
				&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt,
				&source.ContentHash, &source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
				&source.Status, &source.ErrorMessage, &createdAt, &updatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			source.PublishedAt = parseTime(publishedAt)
			source.FetchedAt = parseTime(fetchedAt)
			source.CreatedAt = parseTime(createdAt)
			source.UpdatedAt = parseTime(updatedAt)
			result.Source = source
			result.ResultType = "node"
			// Return the FULL node text when LIKE matches. Unlike FTS which may
			// return many low-relevance nodes, LIKE with multi-term matching has
			// already proven this node contains the query terms. Truncating to a
			// fixed-size snippet loses information that the LLM needs to answer
			// count/list questions accurately (e.g., "how many books").
			// Average node is ~1300 chars (~325 tokens) — acceptable for LLM context.
			result.Snippet = nodeText
			// Score by number of distinct terms matched — nodes with more term hits
			// are more likely to be relevant. This prevents single-char matches on
			// common characters from ranking as high as multi-term matches.
			textLower := strings.ToLower(nodeText)
			matchCount := 0
			for _, term := range terms {
				if strings.Contains(textLower, strings.ToLower(term)) {
					matchCount++
				}
			}
			result.Score = 1.5 + float64(matchCount)*0.3 // range: 1.8 (1 match) to ~5.0 (12 matches)
			result.Citation = formatResultCitation(result)
			results = append(results, result)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return results, nil
}

// extractLikeSnippet extracts a context window around the first occurrence of any
// search term in the full text. Returns up to windowRunes characters centered on
// the match. This provides the LLM with relevant evidence from the original
// document text that may have been lost during distillation into cards/facts.
func extractLikeSnippet(text string, terms []string, windowRunes int) string {
	if text == "" || len(terms) == 0 {
		return ""
	}
	textLower := strings.ToLower(text)
	textRunes := []rune(text)
	bestPos := -1
	for _, term := range terms {
		pos := strings.Index(textLower, strings.ToLower(term))
		if pos >= 0 {
			// Convert byte position to rune position
			runePos := len([]rune(text[:pos]))
			if bestPos < 0 || runePos < bestPos {
				bestPos = runePos
			}
		}
	}
	if bestPos < 0 {
		// No term found in text — return tail as fallback (often has summary info)
		if len(textRunes) <= windowRunes {
			return text
		}
		start := len(textRunes) - windowRunes
		return "..." + string(textRunes[start:])
	}
	// Center the window around the match position
	half := windowRunes / 2
	start := bestPos - half
	if start < 0 {
		start = 0
	}
	end := start + windowRunes
	if end > len(textRunes) {
		end = len(textRunes)
		start = end - windowRunes
		if start < 0 {
			start = 0
		}
	}
	snippet := string(textRunes[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(textRunes) {
		snippet = snippet + "..."
	}
	return snippet
}

func normalizeSource(source Source) Source {
	now := time.Now().UTC()
	if source.ID == "" {
		source.ID = NewID("ksrc")
	}
	if source.Kind == "" {
		source.Kind = SourceKindText
	}
	if source.Status == "" {
		source.Status = StatusPending
	}
	if source.ContentHash == "" {
		source.ContentHash = sha256String(source.URI + "\x00" + source.Kind)
	}
	if source.FetchedAt.IsZero() {
		source.FetchedAt = now
	}
	if source.CreatedAt.IsZero() {
		source.CreatedAt = now
	}
	if source.UpdatedAt.IsZero() {
		source.UpdatedAt = now
	}
	if source.SourceTrust <= 0 {
		source.SourceTrust = 0.5
	}
	return source
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
