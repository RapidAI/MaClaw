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
	source, nodes, err := buildTextSourceAndNodes(req, Source{})
	if err != nil {
		return Source{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	isDuplicate := false
	if existing, ok, err := findExistingTextSourceForSave(ctx, tx, source); err != nil {
		return Source{}, err
	} else if ok {
		isDuplicate = true
		source.ID = existing.ID
		source.CreatedAt = existing.CreatedAt
		if err := deleteSourceDerivedRows(ctx, tx, existing.ID); err != nil {
			return Source{}, err
		}
		for i := range nodes {
			nodes[i].SourceID = source.ID
		}
	}
	if err := insertSource(ctx, tx, source); err != nil {
		return Source{}, err
	}
	if err := addSourceLabelsTx(ctx, tx, source.ID, ingestLabelsForSource(source, req.Labels, req.AutoLabels)); err != nil {
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
	_, _ = s.RefreshSourceTopicLinks(ctx, source.ID, 8)
	sources := []Source{source}
	if err := s.hydrateSourceCounts(ctx, sources); err != nil {
		return Source{}, err
	}
	if err := s.hydrateSourceLabels(ctx, sources); err != nil {
		return Source{}, err
	}
	if isDuplicate {
		sources[0].SaveStatus = SaveStatusDuplicate
	} else {
		sources[0].SaveStatus = SaveStatusCreated
	}
	return sources[0], nil
}

func buildTextSourceAndNodes(req TextSaveRequest, existing Source) (Source, []DocumentNode, error) {
	text := strings.TrimSpace(req.Text)
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
	nodes := parsePlainTextNodes(source, text, kind)
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
	q := `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources
		WHERE kind = ? AND content_hash = ? AND COALESCE(owner_id, '') = ? AND COALESCE(tenant_id, '') = ? AND COALESCE(project_path, '') = ?
		ORDER BY updated_at DESC LIMIT 1`
	existing, err := scanSource(tx.QueryRowContext(ctx, q,
		source.Kind, source.ContentHash, source.OwnerID, source.TenantID, source.ProjectPath,
	))
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
		WHERE kind = ? AND content_hash = ? AND COALESCE(owner_id, '') = ? AND COALESCE(tenant_id, '') = ?
		ORDER BY updated_at DESC LIMIT 1`
	existing, err = scanSource(tx.QueryRowContext(ctx, q2,
		source.Kind, source.ContentHash, source.OwnerID, source.TenantID,
	))
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
