package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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
