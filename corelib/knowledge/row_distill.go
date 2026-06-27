package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// preparedCardFactStmts holds prepared statements for batch card/fact insertion.
// Reusing prepared statements across rows avoids re-parsing SQL for each row.
type preparedCardFactStmts struct {
	cardIns     *sql.Stmt
	cardFTSDel  *sql.Stmt
	cardFTSIns  *sql.Stmt
	factIns     *sql.Stmt
	factFTSDel  *sql.Stmt
	factFTSIns  *sql.Stmt
}

func prepareBatchCardFactStmts(ctx context.Context, tx *sql.Tx) (*preparedCardFactStmts, error) {
	cardIns, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO kb_cards
		(id, source_id, row_id, origin_type, title, claim, summary, entities_json, topics_json, tags_json,
		 project_path, owner_id, tenant_id, valid_at, invalid_at, confidence, importance, source_trust, embedding, created_at, updated_at)
		VALUES (?, ?, ?, 'table_row', ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, NULL, ?, ?)`)
	if err != nil {
		return nil, err
	}
	cardFTSDel, err := tx.PrepareContext(ctx, `DELETE FROM kb_cards_fts WHERE card_id = ?`)
	if err != nil {
		cardIns.Close()
		return nil, err
	}
	cardFTSIns, err := tx.PrepareContext(ctx, `INSERT INTO kb_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`)
	if err != nil {
		cardIns.Close()
		cardFTSDel.Close()
		return nil, err
	}
	factIns, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO kb_facts
		(id, card_id, source_id, row_id, subject, predicate, object, normalized_object, value_type,
		 number_value, date_value, bool_value, negated, valid_at, invalid_at, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', '', ?, ?)`)
	if err != nil {
		cardIns.Close()
		cardFTSDel.Close()
		cardFTSIns.Close()
		return nil, err
	}
	factFTSDel, err := tx.PrepareContext(ctx, `DELETE FROM kb_facts_fts WHERE fact_id = ?`)
	if err != nil {
		cardIns.Close()
		cardFTSDel.Close()
		cardFTSIns.Close()
		factIns.Close()
		return nil, err
	}
	factFTSIns, err := tx.PrepareContext(ctx, `INSERT INTO kb_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`)
	if err != nil {
		cardIns.Close()
		cardFTSDel.Close()
		cardFTSIns.Close()
		factIns.Close()
		factFTSDel.Close()
		return nil, err
	}
	return &preparedCardFactStmts{
		cardIns: cardIns, cardFTSDel: cardFTSDel, cardFTSIns: cardFTSIns,
		factIns: factIns, factFTSDel: factFTSDel, factFTSIns: factFTSIns,
	}, nil
}

func (s *preparedCardFactStmts) Close() {
	if s == nil {
		return
	}
	s.cardIns.Close()
	s.cardFTSDel.Close()
	s.cardFTSIns.Close()
	s.factIns.Close()
	s.factFTSDel.Close()
	s.factFTSIns.Close()
}

// insertKBTableRowCardAndFactsBatch uses pre-prepared statements for better throughput.
// nowStr and invariant JSON fields are passed in to avoid re-computing per row.
// skipFTSDelete should be true for fresh imports where IDs are newly generated (no prior FTS entries exist).
func insertKBTableRowCardAndFactsBatch(ctx context.Context, stmts *preparedCardFactStmts, source Source, row KnowledgeTableRow, cells []KnowledgeTableCell, precomputedFTS *rowCardFTSData, nowStr string, tagsJSON string, topicsJSON string, skipFTSDelete bool) error {
	if row.ID == "" || row.SourceID == "" || strings.TrimSpace(row.RowText) == "" {
		return nil
	}
	subject := chooseTableRowSubject(source, row, cells)
	title := tableRowCardTitle(source, row, subject)
	cardID := NewID("kcard")
	entitiesJSON, _ := json.Marshal(extractEntities(title + " " + row.RowText))
	if _, err := stmts.cardIns.ExecContext(ctx,
		cardID, source.ID, row.ID, title, row.RowText, row.RowText, string(entitiesJSON), topicsJSON, tagsJSON,
		source.ProjectPath, source.OwnerID, source.TenantID, 0.78, 1.15, source.SourceTrust, nowStr, nowStr); err != nil {
		return fmt.Errorf("knowledge sqlite insert kb row card: %w", err)
	}
	// FTS for card — use pre-computed tokens
	ftsTitle := precomputedFTS.ftsTitle
	if ftsTitle == "" {
		ftsTitle = segmentTextForFTS(title)
	}
	ftsRowText := precomputedFTS.ftsRowText
	if ftsRowText == "" {
		ftsRowText = segmentTextForFTS(row.RowText)
	}
	if !skipFTSDelete {
		_, _ = stmts.cardFTSDel.ExecContext(ctx, cardID)
	}
	_, _ = stmts.cardFTSIns.ExecContext(ctx, cardID, ftsTitle, ftsRowText, ftsRowText)

	// Use pre-computed subject FTS
	ftsSubject := precomputedFTS.ftsSubject
	if ftsSubject == "" {
		ftsSubject = segmentTextForFTS(subject)
	}
	for idx, cell := range cells {
		if strings.TrimSpace(cell.NormalizedValue) == "" || strings.TrimSpace(cell.ColumnName) == "" {
			continue
		}
		object := cleanFactPart(cell.NormalizedValue)
		predicate := cleanFactPart(cell.ColumnName)
		if subject == "" || predicate == "" || object == "" {
			continue
		}
		factID := NewID("kfact")
		var numberValue interface{}
		if cell.NumberValue != nil {
			numberValue = *cell.NumberValue
		}
		var boolValue interface{}
		if cell.BoolValue != nil {
			if *cell.BoolValue {
				boolValue = 1
			} else {
				boolValue = 0
			}
		}
		if _, err := stmts.factIns.ExecContext(ctx,
			factID, cardID, source.ID, row.ID, subject, predicate, object, cell.NormalizedValue, cell.ValueType,
			numberValue, cell.DateValue, boolValue, 0.82, nowStr); err != nil {
			return fmt.Errorf("knowledge sqlite insert kb row fact: %w", err)
		}
		// Use pre-computed FTS predicates and objects when available
		ftsPred := ""
		ftsObj := ""
		if idx < len(precomputedFTS.ftsPredicates) {
			ftsPred = precomputedFTS.ftsPredicates[idx]
		}
		if idx < len(precomputedFTS.ftsObjects) {
			ftsObj = precomputedFTS.ftsObjects[idx]
		}
		if ftsPred == "" {
			ftsPred = segmentTextForFTS(predicate)
		}
		if ftsObj == "" {
			ftsObj = segmentTextForFTS(object)
		}
		if !skipFTSDelete {
			_, _ = stmts.factFTSDel.ExecContext(ctx, factID)
		}
		_, _ = stmts.factFTSIns.ExecContext(ctx, factID, ftsSubject, ftsPred, ftsObj)
	}
	return nil
}

// rowCardFTSData holds pre-computed FTS tokens that can be shared between row insert and card/fact insert.
type rowCardFTSData struct {
	ftsRowText    string   // segmentTextForFTS(rowText)
	ftsTitle      string   // segmentTextForFTS(title)
	ftsSubject    string   // segmentTextForFTS(subject)
	ftsPredicates []string // segmentTextForFTS(predicate) per cell, indexed by cell position
	ftsObjects    []string // segmentTextForFTS(object) per cell, indexed by cell position
}

func insertKBTableRowCardAndFacts(ctx context.Context, tx *sql.Tx, source Source, row KnowledgeTableRow, cells []KnowledgeTableCell) error {
	if row.ID == "" || row.SourceID == "" || strings.TrimSpace(row.RowText) == "" {
		return nil
	}
	now := time.Now().UTC()
	subject := chooseTableRowSubject(source, row, cells)
	title := tableRowCardTitle(source, row, subject)
	cardID := NewID("kcard")
	tagsJSON, _ := json.Marshal([]string{source.Kind, "table_row", "structured"})
	topicsJSON, _ := json.Marshal(topicsForSource(source))
	entitiesJSON, _ := json.Marshal(extractEntities(title + " " + row.RowText))
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_cards
		(id, source_id, row_id, origin_type, title, claim, summary, entities_json, topics_json, tags_json,
		 project_path, owner_id, tenant_id, valid_at, invalid_at, confidence, importance, source_trust, embedding, created_at, updated_at)
		VALUES (?, ?, ?, 'table_row', ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, NULL, ?, ?)`,
		cardID, source.ID, row.ID, title, row.RowText, row.RowText, string(entitiesJSON), string(topicsJSON), string(tagsJSON),
		source.ProjectPath, source.OwnerID, source.TenantID, 0.78, 1.15, source.SourceTrust, formatTime(now), formatTime(now)); err != nil {
		return fmt.Errorf("knowledge sqlite insert kb row card: %w", err)
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM kb_cards_fts WHERE card_id = ?`, cardID)
	_, _ = tx.ExecContext(ctx, `INSERT INTO kb_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`,
		cardID, segmentTextForFTS(title), segmentTextForFTS(row.RowText), segmentTextForFTS(row.RowText))
	for _, cell := range cells {
		if strings.TrimSpace(cell.NormalizedValue) == "" || strings.TrimSpace(cell.ColumnName) == "" {
			continue
		}
		object := cleanFactPart(cell.NormalizedValue)
		predicate := cleanFactPart(cell.ColumnName)
		if subject == "" || predicate == "" || object == "" {
			continue
		}
		factID := NewID("kfact")
		var numberValue interface{}
		if cell.NumberValue != nil {
			numberValue = *cell.NumberValue
		}
		var boolValue interface{}
		if cell.BoolValue != nil {
			if *cell.BoolValue {
				boolValue = 1
			} else {
				boolValue = 0
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_facts
			(id, card_id, source_id, row_id, subject, predicate, object, normalized_object, value_type,
			 number_value, date_value, bool_value, negated, valid_at, invalid_at, confidence, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', '', ?, ?)`,
			factID, cardID, source.ID, row.ID, subject, predicate, object, cell.NormalizedValue, cell.ValueType,
			numberValue, cell.DateValue, boolValue, 0.82, formatTime(now)); err != nil {
			return fmt.Errorf("knowledge sqlite insert kb row fact: %w", err)
		}
		_, _ = tx.ExecContext(ctx, `DELETE FROM kb_facts_fts WHERE fact_id = ?`, factID)
		_, _ = tx.ExecContext(ctx, `INSERT INTO kb_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`,
			factID, segmentTextForFTS(subject), segmentTextForFTS(predicate), segmentTextForFTS(object))
	}
	return nil
}

func chooseTableRowSubject(source Source, row KnowledgeTableRow, cells []KnowledgeTableCell) string {
	if subject := cleanFactPart(row.PrimaryKeyText); subject != "" {
		return subject
	}
	for _, cell := range cells {
		name := normalizeSpreadsheetColumnName(cell.ColumnName)
		if strings.Contains(name, "name") || strings.Contains(name, "姓名") || strings.Contains(name, "title") || strings.Contains(name, "标题") || strings.Contains(name, "id") || strings.Contains(name, "编号") {
			if subject := cleanFactPart(cell.NormalizedValue); subject != "" {
				return subject
			}
		}
	}
	if source.Title != "" && row.RowIndex > 0 {
		return fmt.Sprintf("%s row %d", source.Title, row.RowIndex)
	}
	return source.ID
}

func tableRowCardTitle(source Source, row KnowledgeTableRow, subject string) string {
	parts := nonEmptyStrings(source.Title, subject)
	if row.RowIndex > 0 {
		parts = append(parts, fmt.Sprintf("row %d", row.RowIndex))
	}
	return strings.Join(parts, " / ")
}
