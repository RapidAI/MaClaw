package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	CollectionKindManual  = "manual"
	CollectionKindFolder  = "folder"
	CollectionKindProject = "project"

	CollectionAddedByUser    = "user"
	CollectionAddedByWatch   = "watch"
	CollectionAddedByImport  = "import"
	CollectionAddedByProject = "project"

	collectionProjectBackfillMetaKey = "collection_project_backfill"
	collectionProjectBackfillDone    = "done"
	collectionBackfillBatch          = 200
)

func collectionSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS knowledge_collections (
			id TEXT PRIMARY KEY,
			owner_user TEXT NOT NULL,
			title TEXT NOT NULL,
			kind TEXT NOT NULL,
			folder_path TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_collections_owner_kind_path
			ON knowledge_collections(owner_user, kind, folder_path)
			WHERE folder_path != ''`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_collections_owner
			ON knowledge_collections(owner_user, kind)`,
		`CREATE TABLE IF NOT EXISTS knowledge_collection_members (
			collection_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			added_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (collection_id, source_id),
			FOREIGN KEY (collection_id) REFERENCES knowledge_collections(id) ON DELETE CASCADE,
			FOREIGN KEY (source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_collection_members_source
			ON knowledge_collection_members(source_id)`,
	}
}

// ensureKnowledgeOrganizationSchema creates collection tables and the source-link
// status column when they are missing. A complete database only reads
// sqlite_master, so a folder import that already holds a write lock does not
// wait on this open. Existing link rows gain status suggested: unconfirmed
// overlap is not structure until someone confirms it.
func ensureKnowledgeOrganizationSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	ready, err := knowledgeOrganizationObjectsReady(ctx, db)
	if err != nil {
		return err
	}
	if !ready {
		for _, stmt := range collectionSchemaStatements() {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("knowledge collection schema: %w", err)
			}
		}
	}
	hasStatus, err := sourceLinkStatusColumnExists(ctx, db)
	if err != nil {
		return err
	}
	if hasStatus {
		return nil
	}
	exists, err := tableExists(ctx, db, "knowledge_source_links")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE knowledge_source_links ADD COLUMN status TEXT NOT NULL DEFAULT 'suggested'`); err != nil {
		return fmt.Errorf("knowledge source link status: %w", err)
	}
	return nil
}

func knowledgeOrganizationObjectsReady(ctx context.Context, db *sql.DB) (bool, error) {
	for _, name := range []string{
		"knowledge_collections",
		"knowledge_collection_members",
		"idx_knowledge_collections_owner_kind_path",
		"idx_knowledge_collections_owner",
		"idx_knowledge_collection_members_source",
	} {
		ok, err := schemaObjectExists(ctx, db, name)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func schemaObjectExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func sourceLinkStatusColumnExists(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('knowledge_source_links')`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == "status" {
			return true, nil
		}
	}
	return false, rows.Err()
}

// NormalizeFolderCollectionPath is the folder-watch identity. Windows paths
// are cleaned and case-folded so the same directory finds one collection.
func NormalizeFolderCollectionPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return ""
	}
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	return cleaned
}

func collectionTitleFromPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, `/\`)
	if path == "" {
		return ""
	}
	title := path
	if i := strings.LastIndexAny(path, `/\`); i >= 0 && i < len(path)-1 {
		title = path[i+1:]
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return path
	}
	return title
}

func collectionOwnerKey(owner string) string {
	return MembershipUserKey(strings.TrimSpace(owner))
}

func validCollectionAddedBy(addedBy string) bool {
	switch strings.TrimSpace(addedBy) {
	case CollectionAddedByUser, CollectionAddedByWatch, CollectionAddedByImport, CollectionAddedByProject:
		return true
	default:
		return false
	}
}

// CreateManualCollection adds a collection that belongs to a person. It is not
// tied to an import batch. owner is stored as the membership user key.
func (s *SQLiteStore) CreateManualCollection(ctx context.Context, owner, title string) (Collection, error) {
	if s == nil || s.db == nil {
		return Collection{}, fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	title = strings.TrimSpace(title)
	if owner == "" {
		return Collection{}, fmt.Errorf("collection owner is required")
	}
	if title == "" {
		return Collection{}, fmt.Errorf("collection title is required")
	}
	runes := []rune(title)
	if len(runes) > 200 {
		title = string(runes[:200])
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return Collection{}, err
	}
	now := time.Now().UTC()
	collection := Collection{
		ID:        NewID("kcol"),
		OwnerUser: owner,
		Title:     title,
		Kind:      CollectionKindManual,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO knowledge_collections (id, owner_user, title, kind, folder_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, ?)`,
		collection.ID, collection.OwnerUser, collection.Title, collection.Kind, formatTime(now), formatTime(now)); err != nil {
		return Collection{}, err
	}
	return collection, nil
}

// ListCollections returns this user's collections. Manual, folder, and project
// collections share one list.
func (s *SQLiteStore) ListCollections(ctx context.Context, owner string) ([]Collection, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	if owner == "" {
		return nil, fmt.Errorf("collection owner is required")
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.owner_user, c.title, c.kind, c.folder_path, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM knowledge_collection_members m WHERE m.collection_id = c.id)
		FROM knowledge_collections c
		WHERE c.owner_user = ?
		ORDER BY c.kind, LOWER(c.title), c.id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCollections(rows)
}

// ListCollectionMembers returns the sources in one collection. The collection
// must belong to owner.
func (s *SQLiteStore) ListCollectionMembers(ctx context.Context, owner, collectionID string) ([]CollectionMember, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	collectionID = strings.TrimSpace(collectionID)
	if owner == "" || collectionID == "" {
		return nil, fmt.Errorf("collection owner and id are required")
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return nil, err
	}
	if _, err := s.collectionForOwner(ctx, owner, collectionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.collection_id, m.source_id, m.added_by, m.created_at,
			s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
			s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM knowledge_collection_members m
		JOIN knowledge_sources s ON s.id = m.source_id
		WHERE m.collection_id = ?
		ORDER BY s.title, m.source_id`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]CollectionMember, 0)
	for rows.Next() {
		var member CollectionMember
		var createdAt string
		var source Source
		var publishedAt, fetchedAt, sourceCreatedAt, sourceUpdatedAt string
		if err := rows.Scan(&member.CollectionID, &member.SourceID, &member.AddedBy, &createdAt,
			&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt, &source.ContentHash,
			&source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
			&source.Status, &source.ErrorMessage, &sourceCreatedAt, &sourceUpdatedAt); err != nil {
			return nil, err
		}
		member.CreatedAt = parseTime(createdAt)
		source.PublishedAt = parseTime(publishedAt)
		source.FetchedAt = parseTime(fetchedAt)
		source.CreatedAt = parseTime(sourceCreatedAt)
		source.UpdatedAt = parseTime(sourceUpdatedAt)
		member.Source = source
		members = append(members, member)
	}
	return members, rows.Err()
}

// AddCollectionMember files a source into a collection. The source row stays.
func (s *SQLiteStore) AddCollectionMember(ctx context.Context, owner, collectionID, sourceID, addedBy string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	collectionID = strings.TrimSpace(collectionID)
	sourceID = strings.TrimSpace(sourceID)
	addedBy = strings.TrimSpace(addedBy)
	if addedBy == "" {
		addedBy = CollectionAddedByUser
	}
	if !validCollectionAddedBy(addedBy) {
		return fmt.Errorf("collection added_by %q is not supported", addedBy)
	}
	if owner == "" || collectionID == "" || sourceID == "" {
		return fmt.Errorf("collection owner, collection id, and source id are required")
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := collectionForOwnerTx(ctx, tx, owner, collectionID); err != nil {
		return err
	}
	var found int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources WHERE id = ?`, sourceID).Scan(&found); err != nil {
		return err
	}
	if found == 0 {
		return fmt.Errorf("source %s was not found", sourceID)
	}
	if err := addCollectionMemberTx(ctx, tx, collectionID, sourceID, addedBy); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveCollectionMember drops the filing. The source and its memberships stay.
func (s *SQLiteStore) RemoveCollectionMember(ctx context.Context, owner, collectionID, sourceID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	collectionID = strings.TrimSpace(collectionID)
	sourceID = strings.TrimSpace(sourceID)
	if owner == "" || collectionID == "" || sourceID == "" {
		return fmt.Errorf("collection owner, collection id, and source id are required")
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := collectionForOwnerTx(ctx, tx, owner, collectionID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM knowledge_collection_members WHERE collection_id = ? AND source_id = ?`, collectionID, sourceID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("collection member was not found")
	}
	return tx.Commit()
}

// ListUnassignedSources returns sources that are not in any of this user's collections.
func (s *SQLiteStore) ListUnassignedSources(ctx context.Context, owner string, limit int) ([]Source, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	if owner == "" {
		return nil, fmt.Errorf("collection owner is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 5000 {
		limit = 5000
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
			s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM knowledge_sources s
		WHERE NOT EXISTS (
			SELECT 1 FROM knowledge_collection_members m
			JOIN knowledge_collections c ON c.id = m.collection_id
			WHERE m.source_id = s.id AND c.owner_user = ?
		)
		ORDER BY s.updated_at DESC, s.id DESC
		LIMIT ?`, owner, limit)
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
	return sources, rows.Err()
}

// ListSourceCollectionRefs lists collection membership for export grouping.
// A source in two collections appears twice. A source in none is absent.
func (s *SQLiteStore) ListSourceCollectionRefs(ctx context.Context, owner string) ([]SourceCollectionRef, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	if owner == "" {
		return nil, fmt.Errorf("collection owner is required")
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.title, c.kind, c.folder_path, m.source_id
		FROM knowledge_collections c
		JOIN knowledge_collection_members m ON m.collection_id = c.id
		WHERE c.owner_user = ?
		ORDER BY c.kind, LOWER(c.title), c.id, m.source_id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make([]SourceCollectionRef, 0)
	for rows.Next() {
		var ref SourceCollectionRef
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.Kind, &ref.FolderPath, &ref.SourceID); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// AttachFolderCollection finds or creates the folder collection for path and
// files the imported sources into it. Calling it again keeps the same
// collection. It does not remove members when the watch stops.
func (s *SQLiteStore) AttachFolderCollection(ctx context.Context, owner, folderPath string, sourceIDs []string) (Collection, error) {
	if s == nil || s.db == nil {
		return Collection{}, fmt.Errorf("knowledge store is nil")
	}
	owner = collectionOwnerKey(owner)
	folderPath = NormalizeFolderCollectionPath(folderPath)
	if owner == "" || folderPath == "" {
		return Collection{}, fmt.Errorf("collection owner and folder path are required")
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return Collection{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Collection{}, err
	}
	defer tx.Rollback()
	id, err := ensurePathCollectionTx(ctx, tx, owner, CollectionKindFolder, folderPath, collectionTitleFromPath(folderPath))
	if err != nil {
		return Collection{}, err
	}
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		if _, ok := seen[sourceID]; ok {
			continue
		}
		seen[sourceID] = struct{}{}
		if err := addCollectionMemberTx(ctx, tx, id, sourceID, CollectionAddedByWatch); err != nil {
			return Collection{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Collection{}, err
	}
	return s.collectionForOwner(ctx, owner, id)
}

func ensureProjectCollectionMemberTx(ctx context.Context, tx *sql.Tx, owner, projectPath, sourceID string) error {
	owner = collectionOwnerKey(owner)
	projectPath = strings.TrimSpace(projectPath)
	sourceID = strings.TrimSpace(sourceID)
	if owner == "" || projectPath == "" || sourceID == "" {
		return nil
	}
	id, err := ensurePathCollectionTx(ctx, tx, owner, CollectionKindProject, projectPath, collectionTitleFromPath(projectPath))
	if err != nil || id == "" {
		return err
	}
	return addCollectionMemberTx(ctx, tx, id, sourceID, CollectionAddedByProject)
}

func ensurePathCollectionTx(ctx context.Context, tx *sql.Tx, owner, kind, folderPath, title string) (string, error) {
	owner = strings.TrimSpace(owner)
	folderPath = strings.TrimSpace(folderPath)
	if owner == "" || folderPath == "" {
		return "", nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM knowledge_collections WHERE owner_user = ? AND kind = ? AND folder_path = ?`, owner, kind, folderPath).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = collectionTitleFromPath(folderPath)
	}
	id = NewID("kcol")
	now := formatTime(time.Now().UTC())
	if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_collections (id, owner_user, title, kind, folder_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, owner, title, kind, folderPath, now, now); err != nil {
		var existing string
		if scanErr := tx.QueryRowContext(ctx, `SELECT id FROM knowledge_collections WHERE owner_user = ? AND kind = ? AND folder_path = ?`, owner, kind, folderPath).Scan(&existing); scanErr == nil {
			return existing, nil
		}
		return "", err
	}
	return id, nil
}

func addCollectionMemberTx(ctx context.Context, tx *sql.Tx, collectionID, sourceID, addedBy string) error {
	collectionID = strings.TrimSpace(collectionID)
	sourceID = strings.TrimSpace(sourceID)
	if collectionID == "" || sourceID == "" {
		return nil
	}
	if !validCollectionAddedBy(addedBy) {
		return fmt.Errorf("collection added_by %q is not supported", addedBy)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO knowledge_collection_members (collection_id, source_id, added_by, created_at)
		SELECT ?, id, ?, ? FROM knowledge_sources WHERE id = ?`,
		collectionID, addedBy, formatTime(time.Now().UTC()), sourceID); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) collectionForOwner(ctx context.Context, owner, collectionID string) (Collection, error) {
	row := s.db.QueryRowContext(ctx, `SELECT c.id, c.owner_user, c.title, c.kind, c.folder_path, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM knowledge_collection_members m WHERE m.collection_id = c.id)
		FROM knowledge_collections c
		WHERE c.id = ? AND c.owner_user = ?`, collectionID, owner)
	return scanCollectionRow(row)
}

func collectionForOwnerTx(ctx context.Context, tx *sql.Tx, owner, collectionID string) (Collection, error) {
	row := tx.QueryRowContext(ctx, `SELECT c.id, c.owner_user, c.title, c.kind, c.folder_path, c.created_at, c.updated_at, 0
		FROM knowledge_collections c
		WHERE c.id = ? AND c.owner_user = ?`, collectionID, owner)
	return scanCollectionRow(row)
}

func scanCollectionRow(row interface{ Scan(...interface{}) error }) (Collection, error) {
	var collection Collection
	var createdAt, updatedAt string
	if err := row.Scan(&collection.ID, &collection.OwnerUser, &collection.Title, &collection.Kind, &collection.FolderPath, &createdAt, &updatedAt, &collection.MemberCount); err != nil {
		if err == sql.ErrNoRows {
			return Collection{}, fmt.Errorf("collection was not found")
		}
		return Collection{}, err
	}
	collection.CreatedAt = parseTime(createdAt)
	collection.UpdatedAt = parseTime(updatedAt)
	return collection, nil
}

func scanCollections(rows *sql.Rows) ([]Collection, error) {
	collections := make([]Collection, 0)
	for rows.Next() {
		collection, err := scanCollectionRow(rows)
		if err != nil {
			return nil, err
		}
		collections = append(collections, collection)
	}
	return collections, rows.Err()
}

// EnsureCollectionProjectBackfill copies existing project memberships into
// project collections. The kb_meta flag is set only after every non-empty
// owner has a member row, so a desktop stamp that lands later can still file
// sources whose owner was empty on the first pass.
func (s *SQLiteStore) EnsureCollectionProjectBackfill(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return err
	}
	current, err := knowledgeMetaValue(ctx, s.db, collectionProjectBackfillMetaKey)
	if err != nil {
		return err
	}
	if current == collectionProjectBackfillDone {
		return nil
	}
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT m.source_id, m.scope_key, COALESCE(s.owner_id, '')
			FROM knowledge_memberships m
			JOIN knowledge_sources s ON s.id = m.source_id
			WHERE m.kind = ? AND TRIM(m.scope_key) != '' AND TRIM(COALESCE(s.owner_id, '')) != ''
			AND NOT EXISTS (
				SELECT 1 FROM knowledge_collection_members cm
				JOIN knowledge_collections c ON c.id = cm.collection_id
				WHERE cm.source_id = m.source_id AND c.kind = ? AND c.folder_path = m.scope_key
			)
			LIMIT ?`, MembershipKindProject, CollectionKindProject, collectionBackfillBatch)
		if err != nil {
			return err
		}
		type projectMember struct {
			sourceID string
			path     string
			ownerID  string
		}
		batch := make([]projectMember, 0, collectionBackfillBatch)
		for rows.Next() {
			var item projectMember
			if err := rows.Scan(&item.sourceID, &item.path, &item.ownerID); err != nil {
				_ = rows.Close()
				return err
			}
			batch = append(batch, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, item := range batch {
			if err := ensureProjectCollectionMemberTx(ctx, tx, item.ownerID, item.path, item.sourceID); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(batch) < collectionBackfillBatch {
			break
		}
	}
	var pendingEmpty int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM knowledge_memberships m
		JOIN knowledge_sources s ON s.id = m.source_id
		WHERE m.kind = ? AND TRIM(m.scope_key) != '' AND TRIM(COALESCE(s.owner_id, '')) = ''`, MembershipKindProject).Scan(&pendingEmpty); err != nil {
		return err
	}
	if pendingEmpty > 0 {
		return nil
	}
	return setKnowledgeMeta(ctx, s.db, collectionProjectBackfillMetaKey, collectionProjectBackfillDone)
}

func (s *SQLiteStore) collectionFacetBucketsFromHits(ctx context.Context, opts SearchOptions, hits []SearchResult) ([]SearchFacetBucket, error) {
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.Source.ID)
	}
	bySource, err := s.sourceCollectionLabels(ctx, collectionFacetOwner(opts), ids)
	if err != nil {
		return nil, err
	}
	collections := make(map[string]*SearchFacetBucket)
	for _, hit := range hits {
		example := facetExample(hit)
		for _, bucket := range bySource[hit.Source.ID] {
			addSearchFacetBucket(collections, bucket.CollectionID, bucket, example)
		}
	}
	if len(collections) == 0 {
		return nil, nil
	}
	return searchFacetBuckets(collections, 20), nil
}

func collectionFacetOwner(opts SearchOptions) string {
	owner := strings.TrimSpace(opts.OwnerID)
	if owner == "" {
		return ""
	}
	return MembershipUserKey(owner)
}

func (s *SQLiteStore) sourceCollectionLabels(ctx context.Context, owner string, sourceIDs []string) (map[string][]SearchFacetBucket, error) {
	ids := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	ownerSQL := ""
	if owner != "" {
		ownerSQL = " AND c.owner_user = ?"
		args = append(args, owner)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.source_id, c.id, c.title
		FROM knowledge_collection_members m
		JOIN knowledge_collections c ON c.id = m.collection_id
		WHERE m.source_id IN (`+placeholders+`)`+ownerSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]SearchFacetBucket)
	for rows.Next() {
		var sourceID, id, title string
		if err := rows.Scan(&sourceID, &id, &title); err != nil {
			return nil, err
		}
		title = strings.TrimSpace(title)
		if title == "" {
			title = id
		}
		out[sourceID] = append(out[sourceID], SearchFacetBucket{
			Label:        title,
			Kind:         "collection",
			CollectionID: id,
		})
	}
	return out, rows.Err()
}

func (s *SQLiteStore) browseSourceCollectionFacets(ctx context.Context, owner string, sourceWhere []string, sourceArgs []interface{}) ([]SearchFacetBucket, error) {
	where := append([]string{}, sourceWhere...)
	args := append([]interface{}{}, sourceArgs...)
	if owner != "" {
		where = append(where, "c.owner_user = ?")
		args = append(args, owner)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.title, COUNT(DISTINCT s.id)
		FROM knowledge_sources s
		JOIN knowledge_collection_members m ON m.source_id = s.id
		JOIN knowledge_collections c ON c.id = m.collection_id
		WHERE `+strings.Join(where, " AND ")+`
		GROUP BY c.id, c.title
		ORDER BY COUNT(DISTINCT s.id) DESC, LOWER(c.title), c.id
		LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := make([]SearchFacetBucket, 0)
	for rows.Next() {
		var bucket SearchFacetBucket
		if err := rows.Scan(&bucket.CollectionID, &bucket.Label, &bucket.Count); err != nil {
			return nil, err
		}
		bucket.Label = strings.TrimSpace(bucket.Label)
		if bucket.Label == "" {
			bucket.Label = bucket.CollectionID
		}
		if bucket.CollectionID == "" || bucket.Count <= 0 {
			continue
		}
		bucket.Kind = "collection"
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}
