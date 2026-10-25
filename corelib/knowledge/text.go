package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const maxSavedTextRunes = 20_000_000

func (s *SQLiteStore) SaveText(ctx context.Context, req TextSaveRequest) (Source, error) {
	if err := rejectSaveScopeSession(req.SaveScope); err != nil {
		return Source{}, err
	}
	seed := Source{}
	forceID := strings.TrimSpace(req.ForceID)
	if forceID != "" {
		seed.ID = forceID
		if !req.ForceCreatedAt.IsZero() {
			seed.CreatedAt = req.ForceCreatedAt.UTC()
		}
	}
	source, nodes, err := buildTextSourceAndNodes(req, seed)
	if err != nil {
		return Source{}, err
	}
	// Dedup has to look up the owner this save will store. The desktop stamp
	// fills an empty owner, and a repeat save must find that row.
	source = s.applyDesktopOwnerStamp(source)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	isDuplicate := false
	requestedProject := ""
	// Content-hash dedup remaps to another source id. Skip when ForceID is set so
	// callers (e.g. coding experience UpdateExperience) keep a stable identity.
	if forceID == "" {
		if existing, ok, err := findExistingTextSourceForSave(ctx, tx, source); err != nil {
			return Source{}, err
		} else if ok {
			isDuplicate = true
			requestedProject = strings.TrimSpace(source.ProjectPath)
			source.ID = existing.ID
			source.CreatedAt = existing.CreatedAt
			source = preserveStoredOwner(source, existing)
			source = preserveStoredProjectPath(source, existing)
			if err := deleteSourceDerivedRows(ctx, tx, existing.ID); err != nil {
				return Source{}, err
			}
			for i := range nodes {
				nodes[i].SourceID = source.ID
			}
		}
	}
	if forceID != "" {
		// Keep the source row and replace derived rows in this transaction so an
		// update cannot delete the experience before the replacement is durable.
		if err := deleteSourceDerivedRows(ctx, tx, forceID); err != nil {
			return Source{}, err
		}
	}
	if err := s.insertSource(ctx, tx, source); err != nil {
		return Source{}, err
	}
	if requestedProject != "" && requestedProject != strings.TrimSpace(source.ProjectPath) {
		if err := assignSourceMembershipsTx(ctx, tx, Source{ID: source.ID, OwnerID: source.OwnerID, ProjectPath: requestedProject}); err != nil {
			return Source{}, err
		}
	}
	labels := ingestLabelsForSource(source, req.Labels, req.AutoLabels)
	if forceID != "" {
		if err := replaceSourceLabelsTx(ctx, tx, source.ID, labels); err != nil {
			return Source{}, err
		}
	} else if err := addSourceLabelsTx(ctx, tx, source.ID, labels); err != nil {
		return Source{}, err
	}
	if err := insertDocumentNodes(ctx, tx, nodes); err != nil {
		return Source{}, err
	}
	source, err = s.DistillAndSaveCardsWithMode(ctx, tx, source, nodes, req.DistillMode)
	if err != nil {
		return Source{}, err
	}
	if err := insertSourceVersionTx(ctx, tx, source, "save_text"); err != nil {
		return Source{}, err
	}
	if err := tx.Commit(); err != nil {
		return Source{}, err
	}
	_ = s.BackfillNodeEmbeddingsForSources(ctx, []string{source.ID})
	_, _ = s.refreshSourceTopicLinksFast(ctx, source.ID, importTopicLinkLimit, nil)
	// The source is durable once Commit succeeds. Hydration only enriches the
	// returned view, so cancellation here must not trigger duplicate retries.
	source = s.finalizeCommittedSource(ctx, source, isDuplicate)
	if !isDuplicate && strings.TrimSpace(req.ForceID) == "" {
		s.syncVerifiedTextFact(ctx, source, req)
	}
	return source, nil
}

func buildTextSourceAndNodes(req TextSaveRequest, existing Source) (Source, []DocumentNode, error) {
	text := normalizeKnowledgeText(req.Text)
	if text == "" {
		return Source{}, nil, fmt.Errorf("text is required")
	}
	runes := []rune(text)
	if len(runes) > maxSavedTextRunes {
		text = string(runes[:maxSavedTextRunes])
	}
	kind := normalizeTextSourceKind(req.Kind)
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = titleFromText(text)
	}
	now := time.Now().UTC()
	hash := sha256String(kind + "\x00" + text)
	source := Source{
		ID:           existing.ID,
		Kind:         kind,
		URI:          fmt.Sprintf("knowledge://%s/%s", kind, shortHash(hash)),
		Title:        title,
		FetchedAt:    now,
		ContentHash:  hash,
		OwnerID:      strings.TrimSpace(req.OwnerID),
		TenantID:     strings.TrimSpace(req.TenantID),
		ProjectPath:  strings.TrimSpace(req.ProjectPath),
		TopicHint:    strings.TrimSpace(req.TopicHint),
		SourceTrust:  0.75,
		BatchID:      strings.TrimSpace(req.BatchID),
		Status:       StatusParsed,
		CreatedAt:    now,
		UpdatedAt:    now,
		RelativePath: title,
	}
	if source.ID == "" {
		source.ID = NewID("ksrc")
	}
	if !existing.CreatedAt.IsZero() {
		source.CreatedAt = existing.CreatedAt
	}
	nodes := annotateMultilingualNodes(parsePlainTextNodes(source, text, kind))
	for i := range nodes {
		if nodes[i].Title == "" {
			nodes[i].Title = title
		}
		if nodes[i].Metadata == nil {
			nodes[i].Metadata = map[string]string{}
		}
		nodes[i].Metadata["source_kind"] = kind
	}
	return source, nodes, nil
}

func findExistingTextSourceForSave(ctx context.Context, tx *sql.Tx, source Source) (Source, bool, error) {
	if strings.TrimSpace(source.ContentHash) == "" {
		return Source{}, false, nil
	}
	// First try exact match including project_path (same scope update)
	ownerSQL, ownerArgs := sourceOwnerMatchSQL(source.OwnerID)
	q := `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources
		WHERE kind = ? AND content_hash = ? AND ` + ownerSQL + ` AND COALESCE(tenant_id, '') = ? AND COALESCE(project_path, '') = ?
		ORDER BY ` + sourceOwnerMatchOrder + ` LIMIT 1`
	args := []any{source.Kind, source.ContentHash}
	args = append(args, ownerArgs...)
	args = append(args, source.TenantID, source.ProjectPath, source.OwnerID)
	existing, err := scanSource(tx.QueryRowContext(ctx, q, args...))
	if err == nil {
		return existing, true, nil
	}
	if err != sql.ErrNoRows {
		return Source{}, false, err
	}
	// Cross-scope dedup: same content_hash + owner_id + tenant_id but different project_path.
	// This prevents the same text from being stored twice under different scopes.
	q2 := `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources
		WHERE kind = ? AND content_hash = ? AND ` + ownerSQL + ` AND COALESCE(tenant_id, '') = ?
		ORDER BY ` + sourceOwnerMatchOrder + ` LIMIT 1`
	args = []any{source.Kind, source.ContentHash}
	args = append(args, ownerArgs...)
	args = append(args, source.TenantID, source.OwnerID)
	existing, err = scanSource(tx.QueryRowContext(ctx, q2, args...))
	if err == nil {
		return existing, true, nil
	}
	if err == sql.ErrNoRows {
		return Source{}, false, nil
	}
	return Source{}, false, err
}

func normalizeTextSourceKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case SourceKindWorkflowArtifact:
		return SourceKindWorkflowArtifact
	case SourceKindText:
		return SourceKindText
	default:
		return SourceKindConversation
	}
}

func titleFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 80 {
			return string(runes[:80])
		}
		return line
	}
	return "Saved knowledge"
}

func shortHash(hash string) string {
	if len(hash) > 16 {
		return hash[:16]
	}
	return hash
}
