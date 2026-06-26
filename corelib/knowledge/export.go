package knowledge

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type exportRecord struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type snapshotRecord struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type snapshotImportState struct {
	sources map[string]struct{}
	nodes   map[string]struct{}
	cards   map[string]struct{}
}

func (s *SQLiteStore) ExportSnapshot(ctx context.Context, opts ExportOptions) (ExportResult, error) {
	outputPath := strings.TrimSpace(opts.OutputPath)
	if outputPath == "" {
		return ExportResult{}, fmt.Errorf("output path is required")
	}
	tenantID := strings.TrimSpace(opts.TenantID)
	ownerID := strings.TrimSpace(opts.OwnerID)
	sourceIDs := uniqueTrimmed(opts.SourceIDs)
	scopeActive := len(sourceIDs) > 0 || tenantID != "" || ownerID != ""
	if len(sourceIDs) == 0 && (tenantID != "" || ownerID != "") {
		var err error
		sourceIDs, err = s.snapshotSourceIDs(ctx, tenantID, ownerID)
		if err != nil {
			return ExportResult{}, err
		}
	}
	sourceSet := stringSet(sourceIDs)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return ExportResult{}, err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return ExportResult{}, err
	}
	defer file.Close()

	startedAt := time.Now().UTC()
	writer := bufio.NewWriterSize(file, 1024*1024)
	result := ExportResult{
		OutputPath:      outputPath,
		Format:          "jsonl",
		RedactSensitive: opts.RedactSensitive,
		Scoped:          scopeActive,
		SourceIDs:       sourceIDs,
		GeneratedAt:     startedAt,
	}
	if err := writeExportRecord(writer, "manifest", map[string]interface{}{
		"format":             result.Format,
		"generated_at":       startedAt,
		"redact_sensitive":   opts.RedactSensitive,
		"query_requires_llm": false,
		"scoped":             result.Scoped,
		"source_ids":         sourceIDs,
	}); err != nil {
		return result, err
	}
	if !result.Scoped {
		if err := s.exportURLDomainPolicies(ctx, writer, opts.RedactSensitive, &result); err != nil {
			return result, err
		}
	}
	if err := s.exportSources(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := s.exportSourceLabels(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := s.exportSourceVersions(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := s.exportSourceLinks(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := s.exportSourceLinkEvents(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := s.exportNodes(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := s.exportCards(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := s.exportFacts(ctx, writer, opts.RedactSensitive, sourceSet, scopeActive, &result); err != nil {
		return result, err
	}
	if err := writeExportRecord(writer, "summary", result); err != nil {
		return result, err
	}
	if err := writer.Flush(); err != nil {
		return result, err
	}
	if info, err := file.Stat(); err == nil {
		result.Bytes = info.Size()
	}
	return result, nil
}

func (s *SQLiteStore) ImportSnapshot(ctx context.Context, opts SnapshotImportOptions) (SnapshotImportResult, error) {
	inputPath := strings.TrimSpace(opts.InputPath)
	if inputPath == "" {
		return SnapshotImportResult{}, fmt.Errorf("input path is required")
	}
	result := SnapshotImportResult{
		InputPath: inputPath,
		DryRun:    opts.DryRun,
		Overwrite: opts.Overwrite,
		StartedAt: time.Now().UTC(),
	}
	if !opts.DryRun && !opts.SkipSafetyBackup {
		backupPath, err := snapshotSafetyBackupPath(inputPath, opts.SafetyBackupPath, result.StartedAt)
		if err != nil {
			return result, err
		}
		if snapshotPathEqual(inputPath, backupPath) {
			return result, fmt.Errorf("safety backup path must differ from input snapshot path")
		}
		backup, err := s.ExportSnapshot(ctx, ExportOptions{
			OutputPath:      backupPath,
			RedactSensitive: opts.SafetyBackupRedact,
		})
		if err != nil {
			return result, fmt.Errorf("create safety backup before restore: %w", err)
		}
		result.SafetyBackupPath = backup.OutputPath
		result.SafetyBackup = &backup
	}
	file, err := os.Open(inputPath)
	if err != nil {
		return result, err
	}
	defer file.Close()

	var tx *sql.Tx
	if !opts.DryRun {
		tx, err = s.db.BeginTx(ctx, nil)
		if err != nil {
			return result, err
		}
		defer tx.Rollback()
		if opts.ReplaceAll {
			if err := s.clearSnapshotImportTargetTx(ctx, tx); err != nil {
				result.CompletedAt = time.Now().UTC()
				return result, err
			}
		}
	}
	state := snapshotImportState{
		sources: map[string]struct{}{},
		nodes:   map[string]struct{}{},
		cards:   map[string]struct{}{},
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 128*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record snapshotRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SnapshotImportFailure{Line: lineNo, Error: err.Error()})
			continue
		}
		if record.Type == "manifest" || record.Type == "summary" {
			continue
		}
		result.Records++
		if err := s.importSnapshotRecord(ctx, tx, record, lineNo, opts, &state, &result); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, SnapshotImportFailure{Line: lineNo, Type: record.Type, Error: err.Error()})
		}
	}
	if err := scanner.Err(); err != nil {
		result.CompletedAt = time.Now().UTC()
		return result, err
	}
	if opts.AbortOnError {
		if err := snapshotImportResultError(result); err != nil {
			result.CompletedAt = time.Now().UTC()
			return result, err
		}
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			result.CompletedAt = time.Now().UTC()
			return result, err
		}
	}
	result.CompletedAt = time.Now().UTC()
	return result, nil
}

func (s *SQLiteStore) clearSnapshotImportTargetTx(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`DELETE FROM document_nodes_fts`,
		`DELETE FROM knowledge_cards_fts`,
		`DELETE FROM knowledge_facts_fts`,
		`DELETE FROM knowledge_card_suppressions`,
		`DELETE FROM knowledge_facts`,
		`DELETE FROM knowledge_cards`,
		`DELETE FROM document_nodes`,
		`DELETE FROM knowledge_source_link_events`,
		`DELETE FROM knowledge_source_links`,
		`DELETE FROM knowledge_source_labels`,
		`DELETE FROM knowledge_source_versions`,
		`DELETE FROM knowledge_import_items`,
		`DELETE FROM knowledge_import_batches`,
		`DELETE FROM knowledge_sources`,
		`DELETE FROM knowledge_url_domain_policies`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func snapshotImportResultError(result SnapshotImportResult) error {
	if result.Failed == 0 && result.UnknownRecords == 0 && result.MissingReferences == 0 && result.Conflicts == 0 {
		return nil
	}
	parts := []string{}
	if result.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed records", result.Failed))
	}
	if result.UnknownRecords > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown records", result.UnknownRecords))
	}
	if result.MissingReferences > 0 {
		parts = append(parts, fmt.Sprintf("%d missing references", result.MissingReferences))
	}
	if result.Conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", result.Conflicts))
	}
	if len(result.Failures) > 0 {
		parts = append(parts, "first error: "+result.Failures[0].Error)
	}
	return fmt.Errorf("knowledge snapshot import failed: %s", strings.Join(parts, ", "))
}

func snapshotSafetyBackupPath(inputPath, requestedPath string, now time.Time) (string, error) {
	if requestedPath = strings.TrimSpace(requestedPath); requestedPath != "" {
		return requestedPath, nil
	}
	dir := filepath.Dir(strings.TrimSpace(inputPath))
	if dir == "." || dir == "" {
		abs, err := filepath.Abs(inputPath)
		if err != nil {
			return "", err
		}
		dir = filepath.Dir(abs)
	}
	return filepath.Join(dir, fmt.Sprintf("knowledge-pre-restore-backup-%s.jsonl", now.UTC().Format("20060102-150405"))), nil
}

func snapshotPathEqual(a, b string) bool {
	absA, errA := filepath.Abs(strings.TrimSpace(a))
	absB, errB := filepath.Abs(strings.TrimSpace(b))
	if errA == nil && errB == nil {
		return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
	}
	return strings.EqualFold(filepath.Clean(strings.TrimSpace(a)), filepath.Clean(strings.TrimSpace(b)))
}

func (s *SQLiteStore) importSnapshotRecord(ctx context.Context, tx *sql.Tx, record snapshotRecord, lineNo int, opts SnapshotImportOptions, state *snapshotImportState, result *SnapshotImportResult) error {
	switch record.Type {
	case "url_domain_policy":
		var policy URLDomainPolicy
		if err := json.Unmarshal(record.Data, &policy); err != nil {
			return err
		}
		policy.Domain = normalizeURLPolicyDomain(policy.Domain)
		if policy.Domain == "" {
			return fmt.Errorf("url domain policy domain is required")
		}
		if skip, err := s.shouldSkipSnapshotID(ctx, tx, "knowledge_url_domain_policies", policy.Domain, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, policy.Domain)
			return nil
		}
		result.URLPolicies++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		now := time.Now().UTC()
		if policy.CreatedAt.IsZero() {
			policy.CreatedAt = now
		}
		if policy.UpdatedAt.IsZero() {
			policy.UpdatedAt = now
		}
		normalized, ok := normalizeURLDomainPolicy(policy.Domain, policy.Action, policy.Reason, policy.CreatedAt)
		if !ok {
			return fmt.Errorf("invalid url domain policy %q/%q", policy.Domain, policy.Action)
		}
		normalized.UpdatedAt = policy.UpdatedAt
		if err := upsertURLDomainPolicy(ctx, tx, normalized); err != nil {
			return err
		}
	case "source":
		var source Source
		if err := json.Unmarshal(record.Data, &source); err != nil {
			return err
		}
		if source.ID == "" {
			return fmt.Errorf("source id is required")
		}
		if skip, err := s.shouldSkipSnapshotID(ctx, tx, "knowledge_sources", source.ID, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, source.ID)
			return nil
		}
		state.sources[source.ID] = struct{}{}
		result.Sources++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if err := insertSource(ctx, tx, source); err != nil {
			return err
		}
	case "source_label":
		var label SourceLabel
		if err := json.Unmarshal(record.Data, &label); err != nil {
			return err
		}
		label.SourceID = strings.TrimSpace(label.SourceID)
		normalized := normalizeSourceLabels([]string{label.Label})
		if label.SourceID == "" || len(normalized) == 0 {
			return fmt.Errorf("source label requires source_id and label")
		}
		label.Label = normalized[0]
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", label.SourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing source reference %q for source label %q", label.SourceID, label.Label)
		}
		if skip, err := s.shouldSkipSnapshotSourceLabel(ctx, tx, label.SourceID, label.Label, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, label.SourceID+":"+label.Label)
			return nil
		}
		result.SourceLabels++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if label.CreatedAt.IsZero() {
			label.CreatedAt = time.Now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO knowledge_source_labels(source_id, label, created_at) VALUES (?, ?, ?)`, label.SourceID, label.Label, formatTime(label.CreatedAt)); err != nil {
			return err
		}
	case "source_version":
		var version SourceVersion
		if err := json.Unmarshal(record.Data, &version); err != nil {
			return err
		}
		if version.ID == "" {
			return fmt.Errorf("source version id is required")
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", version.SourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing source reference %q for source version %q", version.SourceID, version.ID)
		}
		if skip, err := s.shouldSkipSnapshotID(ctx, tx, "knowledge_source_versions", version.ID, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, version.ID)
			return nil
		}
		result.SourceVersions++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if err := insertSourceVersionRecordTx(ctx, tx, version); err != nil {
			return err
		}
	case "source_link":
		var link SourceLink
		if err := json.Unmarshal(record.Data, &link); err != nil {
			return err
		}
		link.SourceID = strings.TrimSpace(link.SourceID)
		link.RelatedSourceID = strings.TrimSpace(link.RelatedSourceID)
		link.Relation = strings.TrimSpace(link.Relation)
		if link.Relation == "" {
			link.Relation = SourceRelationTopicRelated
		}
		if link.SourceID == "" || link.RelatedSourceID == "" || link.SourceID == link.RelatedSourceID {
			return fmt.Errorf("source link requires distinct source_id and related_source_id")
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", link.SourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing source reference %q for source link", link.SourceID)
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", link.RelatedSourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing related source reference %q for source link", link.RelatedSourceID)
		}
		if skip, err := s.shouldSkipSnapshotSourceLink(ctx, tx, link.SourceID, link.RelatedSourceID, link.Relation, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, sourceLinkSnapshotID(link.SourceID, link.RelatedSourceID, link.Relation))
			return nil
		}
		link.Terms = uniqueTrimmed(link.Terms)
		link.Evidence = uniqueTrimmed(link.Evidence)
		link.RelatedSource = Source{}
		result.SourceLinks++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if err := insertSourceLinkTx(ctx, tx, link); err != nil {
			return err
		}
	case "source_link_event":
		var event SourceLinkEvent
		if err := json.Unmarshal(record.Data, &event); err != nil {
			return err
		}
		event.ID = strings.TrimSpace(event.ID)
		event.SourceID = strings.TrimSpace(event.SourceID)
		event.RelatedSourceID = strings.TrimSpace(event.RelatedSourceID)
		event.Relation = strings.TrimSpace(event.Relation)
		event.Action = strings.TrimSpace(event.Action)
		if event.Relation == "" {
			event.Relation = SourceRelationTopicRelated
		}
		if event.ID == "" {
			return fmt.Errorf("source link event id is required")
		}
		if event.SourceID == "" || event.RelatedSourceID == "" || event.SourceID == event.RelatedSourceID {
			return fmt.Errorf("source link event requires distinct source_id and related_source_id")
		}
		if event.Action == "" {
			return fmt.Errorf("source link event action is required")
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", event.SourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing source reference %q for source link event %q", event.SourceID, event.ID)
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", event.RelatedSourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing related source reference %q for source link event %q", event.RelatedSourceID, event.ID)
		}
		if skip, err := s.shouldSkipSnapshotID(ctx, tx, "knowledge_source_link_events", event.ID, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, event.ID)
			return nil
		}
		event.Terms = uniqueTrimmed(event.Terms)
		event.Evidence = uniqueTrimmed(event.Evidence)
		result.SourceLinkEvents++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if err := insertSourceLinkEventRecordTx(ctx, tx, event); err != nil {
			return err
		}
	case "node":
		var node DocumentNode
		if err := json.Unmarshal(record.Data, &node); err != nil {
			return err
		}
		if node.ID == "" {
			return fmt.Errorf("node id is required")
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", node.SourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing source reference %q for node %q", node.SourceID, node.ID)
		}
		if skip, err := s.shouldSkipSnapshotID(ctx, tx, "document_nodes", node.ID, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, node.ID)
			return nil
		}
		state.nodes[node.ID] = struct{}{}
		result.Nodes++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if err := insertDocumentNode(ctx, tx, node); err != nil {
			return err
		}
	case "card":
		var card Card
		if err := json.Unmarshal(record.Data, &card); err != nil {
			return err
		}
		if card.ID == "" {
			return fmt.Errorf("card id is required")
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", card.SourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing source reference %q for card %q", card.SourceID, card.ID)
		}
		if strings.TrimSpace(card.NodeID) != "" {
			if ok, err := s.snapshotReferenceExists(ctx, tx, "document_nodes", card.NodeID, state.nodes); err != nil {
				return err
			} else if !ok {
				result.MissingReferences++
				return fmt.Errorf("missing node reference %q for card %q", card.NodeID, card.ID)
			}
		}
		if skip, err := s.shouldSkipSnapshotID(ctx, tx, "knowledge_cards", card.ID, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, card.ID)
			return nil
		}
		state.cards[card.ID] = struct{}{}
		result.Cards++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if err := insertCard(ctx, tx, card); err != nil {
			return err
		}
	case "fact":
		var fact Fact
		if err := json.Unmarshal(record.Data, &fact); err != nil {
			return err
		}
		if fact.ID == "" {
			return fmt.Errorf("fact id is required")
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_sources", fact.SourceID, state.sources); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing source reference %q for fact %q", fact.SourceID, fact.ID)
		}
		if ok, err := s.snapshotReferenceExists(ctx, tx, "knowledge_cards", fact.CardID, state.cards); err != nil {
			return err
		} else if !ok {
			result.MissingReferences++
			return fmt.Errorf("missing card reference %q for fact %q", fact.CardID, fact.ID)
		}
		if skip, err := s.shouldSkipSnapshotID(ctx, tx, "knowledge_facts", fact.ID, opts.Overwrite); err != nil {
			return err
		} else if skip {
			addSnapshotImportConflict(result, lineNo, record.Type, fact.ID)
			return nil
		}
		result.Facts++
		if opts.DryRun {
			result.WouldImport++
			return nil
		}
		if err := insertFact(ctx, tx, fact); err != nil {
			return err
		}
	default:
		result.Skipped++
		result.UnknownRecords++
		return nil
	}
	result.Imported++
	return nil
}

func addSnapshotImportConflict(result *SnapshotImportResult, lineNo int, typ, id string) {
	if result == nil {
		return
	}
	result.Skipped++
	result.Conflicts++
	if len(result.ConflictItems) >= 20 {
		return
	}
	result.ConflictItems = append(result.ConflictItems, SnapshotImportConflict{Line: lineNo, Type: typ, ID: id})
}

func (s *SQLiteStore) shouldSkipSnapshotID(ctx context.Context, tx *sql.Tx, table, id string, overwrite bool) (bool, error) {
	if overwrite || strings.TrimSpace(id) == "" {
		return false, nil
	}
	exists, err := s.snapshotIDExists(ctx, tx, table, id)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *SQLiteStore) shouldSkipSnapshotSourceLabel(ctx context.Context, tx *sql.Tx, sourceID, label string, overwrite bool) (bool, error) {
	if overwrite || strings.TrimSpace(sourceID) == "" || strings.TrimSpace(label) == "" {
		return false, nil
	}
	var one int
	var err error
	if tx != nil {
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM knowledge_source_labels WHERE source_id = ? AND label = ? LIMIT 1`, sourceID, label).Scan(&one)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT 1 FROM knowledge_source_labels WHERE source_id = ? AND label = ? LIMIT 1`, sourceID, label).Scan(&one)
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) shouldSkipSnapshotSourceLink(ctx context.Context, tx *sql.Tx, sourceID, relatedSourceID, relation string, overwrite bool) (bool, error) {
	sourceID = strings.TrimSpace(sourceID)
	relatedSourceID = strings.TrimSpace(relatedSourceID)
	relation = strings.TrimSpace(relation)
	if relation == "" {
		relation = SourceRelationTopicRelated
	}
	if overwrite || sourceID == "" || relatedSourceID == "" || relation == "" {
		return false, nil
	}
	var one int
	var err error
	if tx != nil {
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM knowledge_source_links WHERE source_id = ? AND related_source_id = ? AND relation = ? LIMIT 1`, sourceID, relatedSourceID, relation).Scan(&one)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT 1 FROM knowledge_source_links WHERE source_id = ? AND related_source_id = ? AND relation = ? LIMIT 1`, sourceID, relatedSourceID, relation).Scan(&one)
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) snapshotIDExists(ctx context.Context, tx *sql.Tx, table, id string) (bool, error) {
	switch table {
	case "knowledge_sources", "knowledge_source_versions", "knowledge_source_link_events", "document_nodes", "knowledge_cards", "knowledge_facts", "knowledge_url_domain_policies":
	default:
		return false, fmt.Errorf("unsupported snapshot table %q", table)
	}
	var one int
	var err error
	column := "id"
	if table == "knowledge_url_domain_policies" {
		column = "domain"
		id = normalizeURLPolicyDomain(id)
	}
	if tx != nil {
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE `+column+` = ? LIMIT 1`, id).Scan(&one)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE `+column+` = ? LIMIT 1`, id).Scan(&one)
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) snapshotReferenceExists(ctx context.Context, tx *sql.Tx, table, id string, planned map[string]struct{}) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	if _, ok := planned[id]; ok {
		return true, nil
	}
	return s.snapshotIDExists(ctx, tx, table, id)
}

func (s *SQLiteStore) exportURLDomainPolicies(ctx context.Context, writer *bufio.Writer, redact bool, result *ExportResult) error {
	policies, err := s.ListURLDomainPolicies(ctx)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if redact {
			policy.Reason = redactSensitiveText(policy.Reason)
		}
		if err := writeExportRecord(writer, "url_domain_policy", policy); err != nil {
			return err
		}
		result.URLPolicies++
	}
	return nil
}

func (s *SQLiteStore) exportSources(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at,
		content_hash, owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path,
		status, error_message, created_at, updated_at
		FROM knowledge_sources ORDER BY updated_at DESC, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, source.ID, scoped) {
			continue
		}
		if redact {
			source.Title = redactSensitiveText(source.Title)
			source.ErrorMessage = redactSensitiveText(source.ErrorMessage)
			source.TopicHint = redactSensitiveText(source.TopicHint)
		}
		if err := writeExportRecord(writer, "source", source); err != nil {
			return err
		}
		result.Sources++
	}
	return rows.Err()
}

func (s *SQLiteStore) exportSourceVersions(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, COALESCE(kind, ''), COALESCE(uri, ''), COALESCE(canonical_uri, ''), COALESCE(title, ''),
		COALESCE(content_hash, ''), COALESCE(status, ''), COALESCE(reason, ''), COALESCE(fetched_at, ''),
		node_count, card_count, fact_count, created_at
		FROM knowledge_source_versions ORDER BY source_id, created_at ASC, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		version, err := scanSourceVersion(rows)
		if err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, version.SourceID, scoped) {
			continue
		}
		if redact {
			version.Title = redactSensitiveText(version.Title)
			version.Reason = redactSensitiveText(version.Reason)
		}
		if err := writeExportRecord(writer, "source_version", version); err != nil {
			return err
		}
		result.SourceVersions++
	}
	return rows.Err()
}

func (s *SQLiteStore) exportSourceLabels(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, label, created_at FROM knowledge_source_labels ORDER BY source_id, label`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var label SourceLabel
		var createdAt string
		if err := rows.Scan(&label.SourceID, &label.Label, &createdAt); err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, label.SourceID, scoped) {
			continue
		}
		label.CreatedAt = parseTime(createdAt)
		if redact {
			label.Label = redactSensitiveText(label.Label)
		}
		if err := writeExportRecord(writer, "source_label", label); err != nil {
			return err
		}
		result.SourceLabels++
	}
	return rows.Err()
}

func (s *SQLiteStore) exportSourceLinks(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, related_source_id, relation, score, COALESCE(terms_json, '[]'), COALESCE(evidence_json, '[]'), created_at, updated_at
		FROM knowledge_source_links ORDER BY source_id, score DESC, related_source_id, relation`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var link SourceLink
		var termsJSON, evidenceJSON string
		var createdAt, updatedAt string
		if err := rows.Scan(&link.SourceID, &link.RelatedSourceID, &link.Relation, &link.Score, &termsJSON, &evidenceJSON, &createdAt, &updatedAt); err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, link.SourceID, scoped) || !exportSourceSelected(sourceSet, link.RelatedSourceID, scoped) {
			continue
		}
		_ = json.Unmarshal([]byte(termsJSON), &link.Terms)
		_ = json.Unmarshal([]byte(evidenceJSON), &link.Evidence)
		link.Terms = uniqueTrimmed(link.Terms)
		link.Evidence = uniqueTrimmed(link.Evidence)
		link.CreatedAt = parseTime(createdAt)
		link.UpdatedAt = parseTime(updatedAt)
		if redact {
			link.Terms = redactSensitiveStrings(link.Terms)
			link.Evidence = redactSensitiveStrings(link.Evidence)
		}
		if err := writeExportRecord(writer, "source_link", link); err != nil {
			return err
		}
		result.SourceLinks++
	}
	return rows.Err()
}

func (s *SQLiteStore) exportSourceLinkEvents(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, related_source_id, relation, action, score, COALESCE(terms_json, '[]'), COALESCE(evidence_json, '[]'), COALESCE(note, ''), created_at
		FROM knowledge_source_link_events ORDER BY created_at ASC, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		event, err := scanSourceLinkEvent(rows)
		if err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, event.SourceID, scoped) || !exportSourceSelected(sourceSet, event.RelatedSourceID, scoped) {
			continue
		}
		if redact {
			event.Terms = redactSensitiveStrings(event.Terms)
			event.Evidence = redactSensitiveStrings(event.Evidence)
			event.Note = redactSensitiveText(event.Note)
		}
		if err := writeExportRecord(writer, "source_link_event", event); err != nil {
			return err
		}
		result.SourceLinkEvents++
	}
	return rows.Err()
}

func (s *SQLiteStore) exportNodes(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, COALESCE(parent_id, ''), type, COALESCE(title, ''), COALESCE(text, ''), level, page,
		COALESCE(sheet_name, ''), COALESCE(row_range, ''), COALESCE(col_range, ''), COALESCE(xpath, ''),
		offset, COALESCE(metadata_json, '{}'), token_count
		FROM document_nodes ORDER BY source_id, offset ASC, level ASC, id ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		node, err := scanDocumentNode(rows)
		if err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, node.SourceID, scoped) {
			continue
		}
		if redact {
			node.Title = redactSensitiveText(node.Title)
			node.Text = redactSensitiveText(node.Text)
			node.Metadata = redactSensitiveStringMap(node.Metadata)
		}
		if err := writeExportRecord(writer, "node", node); err != nil {
			return err
		}
		result.Nodes++
	}
	return rows.Err()
}

func (s *SQLiteStore) exportCards(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, COALESCE(node_id, ''), COALESCE(title, ''), claim, COALESCE(summary, ''),
		COALESCE(entities_json, '[]'), COALESCE(topics_json, '[]'), COALESCE(tags_json, '[]'),
		COALESCE(project_path, ''), COALESCE(owner_id, ''), COALESCE(tenant_id, ''), COALESCE(valid_at, ''), COALESCE(invalid_at, ''),
		confidence, importance, source_trust, created_at, updated_at
		FROM knowledge_cards ORDER BY source_id, importance DESC, updated_at DESC, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		card, err := scanCard(rows)
		if err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, card.SourceID, scoped) {
			continue
		}
		if redact {
			card.Title = redactSensitiveText(card.Title)
			card.Claim = redactSensitiveText(card.Claim)
			card.Summary = redactSensitiveText(card.Summary)
			card.Entities = redactSensitiveStrings(card.Entities)
			card.Topics = redactSensitiveStrings(card.Topics)
			card.Tags = redactSensitiveStrings(card.Tags)
		}
		if err := writeExportRecord(writer, "card", card); err != nil {
			return err
		}
		result.Cards++
	}
	return rows.Err()
}

func (s *SQLiteStore) exportFacts(ctx context.Context, writer *bufio.Writer, redact bool, sourceSet map[string]struct{}, scoped bool, result *ExportResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, card_id, source_id, subject, predicate, object, negated,
		COALESCE(valid_at, ''), COALESCE(invalid_at, ''), confidence
		FROM knowledge_facts ORDER BY source_id, subject, predicate, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		fact, err := scanFact(rows)
		if err != nil {
			return err
		}
		if !exportSourceSelected(sourceSet, fact.SourceID, scoped) {
			continue
		}
		if redact {
			fact.Subject = redactSensitiveText(fact.Subject)
			fact.Predicate = redactSensitiveText(fact.Predicate)
			fact.Object = redactSensitiveText(fact.Object)
		}
		if err := writeExportRecord(writer, "fact", fact); err != nil {
			return err
		}
		result.Facts++
	}
	return rows.Err()
}

func writeExportRecord(writer *bufio.Writer, typ string, data interface{}) error {
	line, err := json.Marshal(exportRecord{Type: typ, Data: data})
	if err != nil {
		return err
	}
	if _, err := writer.Write(line); err != nil {
		return err
	}
	return writer.WriteByte('\n')
}

func stringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func (s *SQLiteStore) snapshotSourceIDs(ctx context.Context, tenantID, ownerID string) ([]string, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0, 2)
	if tenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, tenantID)
	}
	if ownerID != "" {
		where = append(where, "owner_id = ?")
		args = append(args, ownerID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM knowledge_sources WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func exportSourceSelected(sourceSet map[string]struct{}, sourceID string, scoped bool) bool {
	if !scoped {
		return true
	}
	_, ok := sourceSet[sourceID]
	return ok
}

func sourceLinkSnapshotID(sourceID, relatedSourceID, relation string) string {
	return strings.TrimSpace(sourceID) + ":" + strings.TrimSpace(relatedSourceID) + ":" + strings.TrimSpace(relation)
}

func redactSensitiveText(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	result := text
	for _, pattern := range sensitivePatterns {
		result = pattern.re.ReplaceAllStringFunc(result, redactSensitive)
	}
	return result
}

// RedactSensitiveText applies the same text redaction used by snapshot export.
func RedactSensitiveText(text string) string {
	return redactSensitiveText(text)
}

func redactSensitiveStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = redactSensitiveText(value)
	}
	return out
}

func redactSensitiveStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if isSensitiveMetadataKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redactSensitiveText(value)
	}
	return out
}

func isSensitiveMetadataKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	sensitiveFragments := []string{
		"api_key",
		"apikey",
		"access_key",
		"authorization",
		"bearer",
		"credential",
		"password",
		"passwd",
		"private_key",
		"secret",
		"token",
	}
	for _, fragment := range sensitiveFragments {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func redactSensitiveMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return values
	}
	out := make(map[string]interface{}, len(values))
	for key, value := range values {
		out[key] = redactSensitiveValue(value)
	}
	return out
}

func redactSensitiveValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case string:
		return redactSensitiveText(typed)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = redactSensitiveValue(item)
		}
		return out
	case map[string]interface{}:
		return redactSensitiveMap(typed)
	default:
		return value
	}
}
