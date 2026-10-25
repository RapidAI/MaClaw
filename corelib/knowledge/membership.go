package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	MembershipKindUser    = "user"
	MembershipKindProject = "project"
	MembershipKindLibrary = "library"

	// desktopMembershipUser is the collapsed user key for desktop-user and
	// desktop-user:<task>. Source rows keep the original owner_id.
	desktopMembershipUser = "desktop-user"

	membershipBackfillMetaKey        = "membership_backfill"
	membershipBackfillOwners         = "owners"
	libraryMembershipBackfillMetaKey = "library_membership_backfill"
	libraryMembershipBackfillDone    = "done"
	membershipBackfillBatch          = 200
)

// MembershipUserKey is the user membership scope_key. desktop-user and
// desktop-user:<task> share one key. Every other owner stays exact.
func MembershipUserKey(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == desktopMembershipUser || strings.HasPrefix(ownerID, desktopMembershipUser+":") {
		return desktopMembershipUser
	}
	return ownerID
}

func membershipSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS knowledge_memberships (
			source_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			scope_key TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (source_id, kind, scope_key),
			FOREIGN KEY (source_id) REFERENCES knowledge_sources(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_memberships_kind_key ON knowledge_memberships(kind, scope_key)`,
	}
}

func ensureMembershipTable(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	for _, stmt := range membershipSchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("knowledge membership schema: %w", err)
		}
	}
	return nil
}

func ensureMembershipSchemaTx(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range membershipSchemaStatements() {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("knowledge membership schema: %w", err)
		}
	}
	return nil
}

// SetDesktopOwnerStamp makes later source writes on this store fill an empty
// owner_id with owner. Servers and the coding knowledge store leave it unset.
// Set it before the store is used concurrently.
func (s *SQLiteStore) SetDesktopOwnerStamp(owner string) {
	if s == nil {
		return
	}
	s.desktopOwnerStamp.Store(strings.TrimSpace(owner))
}

func (s *SQLiteStore) desktopOwnerStampValue() string {
	if s == nil {
		return ""
	}
	stamp, _ := s.desktopOwnerStamp.Load().(string)
	return strings.TrimSpace(stamp)
}

func (s *SQLiteStore) applyDesktopOwnerStamp(source Source) Source {
	if stamp := s.desktopOwnerStampValue(); stamp != "" && strings.TrimSpace(source.OwnerID) == "" {
		source.OwnerID = stamp
	}
	return source
}

// ownerForExistingSourceLookup is the owner a repeat save should search for.
// An empty request on a desktop store is stored as the stamp, so the lookup
// has to use that same value.
func (s *SQLiteStore) ownerForExistingSourceLookup(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		return ownerID
	}
	return s.desktopOwnerStampValue()
}

// preserveStoredOwner keeps a task-scoped owner already on the row. The lookup
// may arrive through the desktop stamp; the stored owner stays as it was written.
func preserveStoredOwner(source, existing Source) Source {
	if owner := strings.TrimSpace(existing.OwnerID); owner != "" {
		source.OwnerID = owner
	}
	return source
}

// preserveStoredProjectPath keeps a project path already stored on the source.
// A later save in another project adds a membership instead of replacing it.
func preserveStoredProjectPath(source, existing Source) Source {
	if path := strings.TrimSpace(existing.ProjectPath); path != "" {
		source.ProjectPath = path
	}
	return source
}

// alignCardOwners copies a source owner onto cards that were built before that
// owner was known. A card that already has an owner is left alone, and an
// empty source does not stamp its cards.
func (s *SQLiteStore) alignCardOwners(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	err := alignCardOwnersFromSources(ctx, s.db)
	if IsSQLiteLockedError(err) {
		return nil
	}
	return err
}

func alignCardOwnersFromSources(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	exists, err := tableExists(ctx, db, "knowledge_cards")
	if err != nil || !exists {
		return err
	}
	var one int
	err = db.QueryRowContext(ctx, `SELECT 1 FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		WHERE TRIM(COALESCE(c.owner_id, '')) = ''
		  AND TRIM(COALESCE(s.owner_id, '')) <> ''
		LIMIT 1`).Scan(&one)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	const query = `SELECT c.id, TRIM(s.owner_id)
		FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		WHERE TRIM(COALESCE(c.owner_id, '')) = ''
		  AND TRIM(COALESCE(s.owner_id, '')) <> ''
		LIMIT ?`
	var lastID string
	for {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, query, membershipBackfillBatch)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		type row struct{ id, owner string }
		batch := make([]row, 0, membershipBackfillBatch)
		for rows.Next() {
			var item row
			if err := rows.Scan(&item.id, &item.owner); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return err
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if len(batch) == 0 {
			_ = tx.Rollback()
			return nil
		}
		if batch[0].id == lastID {
			_ = tx.Rollback()
			return fmt.Errorf("card owner align made no progress at %s", lastID)
		}
		lastID = batch[0].id
		for _, item := range batch {
			if _, err := tx.ExecContext(ctx, `UPDATE knowledge_cards SET owner_id = ?
				WHERE id = ? AND TRIM(COALESCE(owner_id, '')) = ''`, item.owner, item.id); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(batch) < membershipBackfillBatch {
			return nil
		}
	}
}

func rejectSaveScopeSession(scope string) error {
	if strings.EqualFold(strings.TrimSpace(scope), SaveScopeSession) {
		return fmt.Errorf("save scope %q is not supported", SaveScopeSession)
	}
	return nil
}

// EnsureMembershipBackfill registers user and project memberships for sources
// that already have an owner or project path. When desktopStamp is non-empty,
// empty owner_id rows in this database are stamped to that user once. A server
// passes an empty stamp and never rewrites empty owners. The kb_meta value
// records which step finished so a later desktop stamp can still run after an
// owners-only pass. Batches are a few hundred rows and do not parse or call a
// model.
func (s *SQLiteStore) EnsureMembershipBackfill(ctx context.Context, desktopStamp string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	desktopStamp = strings.TrimSpace(desktopStamp)
	exists, err := tableExists(ctx, s.db, "knowledge_memberships")
	if err != nil {
		return err
	}
	if !exists {
		if err := ensureMembershipTable(ctx, s.db); err != nil {
			return err
		}
	}
	current, err := knowledgeMetaValue(ctx, s.db, membershipBackfillMetaKey)
	if err != nil {
		return err
	}
	if membershipBackfillSatisfied(current, desktopStamp) {
		if err := s.alignCardOwners(ctx); err != nil {
			return err
		}
		return s.EnsureCollectionProjectBackfill(ctx)
	}
	if err := s.backfillOwnerMemberships(ctx); err != nil {
		return err
	}
	if err := s.backfillProjectMemberships(ctx); err != nil {
		return err
	}
	if desktopStamp != "" {
		if err := s.stampEmptyOwners(ctx, desktopStamp); err != nil {
			return err
		}
	}
	value := membershipBackfillOwners
	if desktopStamp != "" {
		value = desktopStamp
	}
	if err := setKnowledgeMeta(ctx, s.db, membershipBackfillMetaKey, value); err != nil {
		return err
	}
	if err := s.alignCardOwners(ctx); err != nil {
		return err
	}
	return s.EnsureCollectionProjectBackfill(ctx)
}

func membershipBackfillSatisfied(current, stamp string) bool {
	current = strings.TrimSpace(current)
	stamp = strings.TrimSpace(stamp)
	if current == "" {
		return false
	}
	if stamp == "" {
		return true
	}
	return current == stamp
}

func (s *SQLiteStore) backfillOwnerMemberships(ctx context.Context) error {
	const query = `SELECT s.id, TRIM(s.owner_id)
		FROM knowledge_sources s
		WHERE TRIM(COALESCE(s.owner_id, '')) <> ''
		  AND NOT EXISTS (
			SELECT 1 FROM knowledge_memberships m
			WHERE m.source_id = s.id AND m.kind = ?
			  AND m.scope_key = CASE
				WHEN TRIM(s.owner_id) = ? OR TRIM(s.owner_id) LIKE ? THEN ?
				ELSE TRIM(s.owner_id)
			  END
		  )
		LIMIT ?`
	var lastID string
	for {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, query, MembershipKindUser, desktopMembershipUser, desktopMembershipUser+":%", desktopMembershipUser, membershipBackfillBatch)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		type row struct{ id, owner string }
		batch := make([]row, 0, membershipBackfillBatch)
		for rows.Next() {
			var item row
			if err := rows.Scan(&item.id, &item.owner); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return err
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if len(batch) == 0 {
			_ = tx.Rollback()
			return nil
		}
		if batch[0].id == lastID {
			_ = tx.Rollback()
			return fmt.Errorf("membership owner backfill made no progress at %s", lastID)
		}
		lastID = batch[0].id
		now := formatTime(time.Now().UTC())
		for _, item := range batch {
			if err := insertMembershipTx(ctx, tx, item.id, MembershipKindUser, MembershipUserKey(item.owner), now); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(batch) < membershipBackfillBatch {
			return nil
		}
	}
}

func (s *SQLiteStore) backfillProjectMemberships(ctx context.Context) error {
	const query = `SELECT s.id, TRIM(s.project_path)
		FROM knowledge_sources s
		WHERE TRIM(COALESCE(s.project_path, '')) <> ''
		  AND NOT EXISTS (
			SELECT 1 FROM knowledge_memberships m
			WHERE m.source_id = s.id AND m.kind = ? AND m.scope_key = TRIM(s.project_path)
		  )
		LIMIT ?`
	var lastID string
	for {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, query, MembershipKindProject, membershipBackfillBatch)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		type row struct{ id, path string }
		batch := make([]row, 0, membershipBackfillBatch)
		for rows.Next() {
			var item row
			if err := rows.Scan(&item.id, &item.path); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return err
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if len(batch) == 0 {
			_ = tx.Rollback()
			return nil
		}
		if batch[0].id == lastID {
			_ = tx.Rollback()
			return fmt.Errorf("membership project backfill made no progress at %s", lastID)
		}
		lastID = batch[0].id
		now := formatTime(time.Now().UTC())
		for _, item := range batch {
			if err := insertMembershipTx(ctx, tx, item.id, MembershipKindProject, item.path, now); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(batch) < membershipBackfillBatch {
			return nil
		}
	}
}

func (s *SQLiteStore) stampEmptyOwners(ctx context.Context, stamp string) error {
	stamp = strings.TrimSpace(stamp)
	if stamp == "" {
		return nil
	}
	const query = `SELECT id FROM knowledge_sources WHERE TRIM(COALESCE(owner_id, '')) = '' LIMIT ?`
	for {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, query, membershipBackfillBatch)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		ids := make([]string, 0, membershipBackfillBatch)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if len(ids) == 0 {
			_ = tx.Rollback()
			return nil
		}
		now := formatTime(time.Now().UTC())
		for _, id := range ids {
			result, err := tx.ExecContext(ctx, `UPDATE knowledge_sources SET owner_id = ? WHERE id = ? AND TRIM(COALESCE(owner_id, '')) = ''`, stamp, id)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			updated, err := result.RowsAffected()
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			if updated == 0 {
				continue
			}
			if err := insertMembershipTx(ctx, tx, id, MembershipKindUser, MembershipUserKey(stamp), now); err != nil {
				_ = tx.Rollback()
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE knowledge_cards SET owner_id = ?
				WHERE source_id = ? AND TRIM(COALESCE(owner_id, '')) = ''`, stamp, id); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(ids) < membershipBackfillBatch {
			return nil
		}
	}
}

// adoptUnownedHashSources gives an empty-owner content match the owner this
// import will store. A hash skip would otherwise leave that row with no user
// membership, and the desktop read path would not show it. A source that
// already has an owner is left unchanged.
func (s *SQLiteStore) adoptUnownedHashSources(ctx context.Context, ownerID string, existing map[string]string) error {
	ownerID = strings.TrimSpace(ownerID)
	if s == nil || s.db == nil || ownerID == "" || len(existing) == 0 {
		return nil
	}
	ids := make([]string, 0, len(existing))
	seen := make(map[string]struct{}, len(existing))
	for _, id := range existing {
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
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const batch = 80
	type unownedSource struct {
		id, projectPath string
	}
	pending := make([]unownedSource, 0)
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, TRIM(COALESCE(project_path, ''))
			FROM knowledge_sources
			WHERE TRIM(COALESCE(owner_id, '')) = '' AND id IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item unownedSource
			if err := rows.Scan(&item.id, &item.projectPath); err != nil {
				_ = rows.Close()
				return err
			}
			pending = append(pending, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	for _, item := range pending {
		result, err := tx.ExecContext(ctx, `UPDATE knowledge_sources SET owner_id = ?
			WHERE id = ? AND TRIM(COALESCE(owner_id, '')) = ''`, ownerID, item.id)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE knowledge_cards SET owner_id = ?
			WHERE source_id = ? AND TRIM(COALESCE(owner_id, '')) = ''`, ownerID, item.id); err != nil {
			return err
		}
		if err := assignSourceMembershipsTx(ctx, tx, Source{ID: item.id, OwnerID: ownerID, ProjectPath: item.projectPath}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// addCrossProjectHashMembership files an existing body into the project this
// import asked for. The stored project_path stays when it is already set.
func (s *SQLiteStore) addCrossProjectHashMembership(ctx context.Context, ownerID string, req DirectoryImportRequest, existing map[string]string) error {
	ownerID = strings.TrimSpace(ownerID)
	projectPath := strings.TrimSpace(req.ProjectPath)
	if s == nil || s.db == nil || ownerID == "" || projectPath == "" || req.SaveScope != SaveScopeProject || len(existing) == 0 {
		return nil
	}
	ids := make([]string, 0, len(existing))
	seen := make(map[string]struct{}, len(existing))
	for _, id := range existing {
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
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const batch = 80
	type hashProjectSource struct {
		id, owner, project string
	}
	pending := make([]hashProjectSource, 0)
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, TRIM(COALESCE(owner_id, '')), TRIM(COALESCE(project_path, ''))
			FROM knowledge_sources WHERE id IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item hashProjectSource
			if err := rows.Scan(&item.id, &item.owner, &item.project); err != nil {
				_ = rows.Close()
				return err
			}
			if item.project == projectPath {
				continue
			}
			pending = append(pending, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	for _, item := range pending {
		rowOwner := item.owner
		if rowOwner == "" {
			rowOwner = ownerID
		}
		if item.project == "" {
			if _, err := tx.ExecContext(ctx, `UPDATE knowledge_sources SET project_path = ?
				WHERE id = ? AND TRIM(COALESCE(project_path, '')) = ''`, projectPath, item.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE knowledge_cards SET project_path = ?
				WHERE source_id = ? AND TRIM(COALESCE(project_path, '')) = ''`, projectPath, item.id); err != nil {
				return err
			}
		}
		if err := assignSourceMembershipsTx(ctx, tx, Source{ID: item.id, OwnerID: rowOwner, ProjectPath: projectPath}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func assignSourceMembershipsTx(ctx context.Context, tx *sql.Tx, source Source) error {
	sourceID := strings.TrimSpace(source.ID)
	if sourceID == "" {
		return nil
	}
	now := formatTime(time.Now().UTC())
	if owner := strings.TrimSpace(source.OwnerID); owner != "" {
		if err := insertMembershipTx(ctx, tx, sourceID, MembershipKindUser, MembershipUserKey(owner), now); err != nil {
			return err
		}
	}
	if path := strings.TrimSpace(source.ProjectPath); path != "" {
		if err := insertMembershipTx(ctx, tx, sourceID, MembershipKindProject, path, now); err != nil {
			return err
		}
		if err := ensureProjectCollectionMemberTx(ctx, tx, MembershipUserKey(source.OwnerID), path, sourceID); err != nil {
			return err
		}
	}
	return nil
}

func insertMembershipTx(ctx context.Context, tx *sql.Tx, sourceID, kind, scopeKey, createdAt string) error {
	sourceID = strings.TrimSpace(sourceID)
	scopeKey = strings.TrimSpace(scopeKey)
	if sourceID == "" || scopeKey == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO knowledge_memberships (source_id, kind, scope_key, created_at) VALUES (?, ?, ?, ?)`,
		sourceID, kind, scopeKey, createdAt); err != nil {
		return fmt.Errorf("assign %s membership: %w", kind, err)
	}
	return nil
}

// AddMembership registers one membership without removing the source's other
// memberships. A second project is added here. Repeating the same key keeps
// the original created_at.
func (s *SQLiteStore) AddMembership(ctx context.Context, sourceID, kind, scopeKey string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	sourceID = strings.TrimSpace(sourceID)
	kind = strings.ToLower(strings.TrimSpace(kind))
	scopeKey = strings.TrimSpace(scopeKey)
	if sourceID == "" || scopeKey == "" {
		return fmt.Errorf("membership source_id and scope_key are required")
	}
	switch kind {
	case MembershipKindUser:
		scopeKey = MembershipUserKey(scopeKey)
	case MembershipKindProject, MembershipKindLibrary:
	default:
		return fmt.Errorf("membership kind %q is not supported", kind)
	}
	if err := ensureMembershipTable(ctx, s.db); err != nil {
		return err
	}
	if err := ensureKnowledgeOrganizationSchema(ctx, s.db); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertMembershipTx(ctx, tx, sourceID, kind, scopeKey, formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if kind == MembershipKindProject {
		var ownerID string
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(owner_id, '') FROM knowledge_sources WHERE id = ?`, sourceID).Scan(&ownerID); err != nil {
			return err
		}
		if err := ensureProjectCollectionMemberTx(ctx, tx, ownerID, scopeKey, sourceID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AssignLibraryMemberships adds library memberships for sources that exist.
// Existing rows keep their created_at.
func (s *SQLiteStore) AssignLibraryMemberships(ctx context.Context, libraryID string, sourceIDs []string) error {
	return s.writeLibraryMemberships(ctx, libraryID, sourceIDs, false)
}

// ReplaceLibraryMemberships replaces the membership set of one library. Sources
// that are not in this store are skipped so the foreign key is not violated.
func (s *SQLiteStore) ReplaceLibraryMemberships(ctx context.Context, libraryID string, sourceIDs []string) error {
	return s.writeLibraryMemberships(ctx, libraryID, sourceIDs, true)
}

func (s *SQLiteStore) writeLibraryMemberships(ctx context.Context, libraryID string, sourceIDs []string, replace bool) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library_id is required")
	}
	if err := ensureMembershipTable(ctx, s.db); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_memberships WHERE kind = ? AND scope_key = ?`, MembershipKindLibrary, libraryID); err != nil {
			return err
		}
	}
	now := formatTime(time.Now().UTC())
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
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO knowledge_memberships (source_id, kind, scope_key, created_at)
			SELECT id, ?, ?, ? FROM knowledge_sources WHERE id = ?`, MembershipKindLibrary, libraryID, now, sourceID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LibraryMembershipBackfillCompleted reports that this store already copied
// enterprise_source_map rows into library memberships.
func (s *SQLiteStore) LibraryMembershipBackfillCompleted(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("knowledge store is nil")
	}
	value, err := knowledgeMetaValue(ctx, s.db, libraryMembershipBackfillMetaKey)
	if err != nil {
		return false, err
	}
	return value == libraryMembershipBackfillDone, nil
}

// FinishLibraryMembershipBackfill records the one-shot library membership copy.
func (s *SQLiteStore) FinishLibraryMembershipBackfill(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store is nil")
	}
	return setKnowledgeMeta(ctx, s.db, libraryMembershipBackfillMetaKey, libraryMembershipBackfillDone)
}

func knowledgeMetaValue(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM kb_meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read kb_meta %s: %w", key, err)
	}
	return value, nil
}

func setKnowledgeMeta(ctx context.Context, db *sql.DB, key, value string) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO kb_meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		return fmt.Errorf("write kb_meta %s: %w", key, err)
	}
	return nil
}

func (s *SQLiteStore) hydrateSearchResultLibraryIDs(ctx context.Context, results []SearchResult) error {
	if s == nil || s.db == nil || len(results) == 0 {
		return nil
	}
	exists, err := tableExists(ctx, s.db, "knowledge_memberships")
	if err != nil || !exists {
		return err
	}
	index := make(map[string][]int)
	ids := make([]string, 0)
	for i := range results {
		if strings.TrimSpace(results[i].LibraryID) != "" {
			continue
		}
		id := strings.TrimSpace(results[i].Source.ID)
		if id == "" {
			continue
		}
		if _, ok := index[id]; !ok {
			ids = append(ids, id)
		}
		index[id] = append(index[id], i)
	}
	if len(ids) == 0 {
		return nil
	}
	const chunk = 100
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		args := make([]any, 0, len(part)+1)
		args = append(args, MembershipKindLibrary)
		for _, id := range part {
			args = append(args, id)
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(part)), ",")
		rows, err := s.db.QueryContext(ctx, `SELECT source_id, MIN(scope_key) FROM knowledge_memberships
			WHERE kind = ? AND source_id IN (`+placeholders+`) GROUP BY source_id`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var sourceID, libraryID string
			if err := rows.Scan(&sourceID, &libraryID); err != nil {
				_ = rows.Close()
				return err
			}
			for _, i := range index[sourceID] {
				if strings.TrimSpace(results[i].LibraryID) == "" {
					results[i].LibraryID = libraryID
				}
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
