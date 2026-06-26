package knowledge

import (
	"context"
	"fmt"
	"strings"
)

func (s *SQLiteStore) kbFactGraphEdges(ctx context.Context, opts SearchOptions) ([]FactGraphEdge, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	query := buildFTSQuery(opts.Query)
	where := []string{"f.row_id IS NOT NULL", "c.origin_type = 'table_row'"}
	args := make([]interface{}, 0)
	from := `kb_facts f`
	order := `f.confidence DESC, f.id DESC`
	if query != "" {
		from = `kb_facts_fts JOIN kb_facts f ON f.id = kb_facts_fts.fact_id`
		where = append([]string{"kb_facts_fts MATCH ?"}, where...)
		args = append(args, query)
		order = `bm25(kb_facts_fts), f.confidence DESC`
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
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, opts.SourceKinds, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.card_id, f.source_id, f.subject, f.predicate, f.object, f.confidence,
		c.title, c.claim,
		s.title, s.kind, s.uri, s.canonical_uri, s.relative_path,
		COALESCE(t.sheet_name, ''), COALESCE(r.row_index, 0)
		FROM `+from+`
		JOIN kb_cards c ON c.id = f.card_id
		LEFT JOIN kb_rows r ON r.id = f.row_id
		LEFT JOIN kb_tables t ON t.id = r.table_id
		JOIN kb_sources s ON s.id = f.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	edges := make([]FactGraphEdge, 0, limit)
	for rows.Next() {
		var factID, cardID, sourceID, subject, predicate, object string
		var confidence float64
		var cardTitle, claim, sourceTitle, sourceKind, uri, canonicalURI, relativePath string
		var sheetName string
		var rowIndex int
		if err := rows.Scan(&factID, &cardID, &sourceID, &subject, &predicate, &object, &confidence, &cardTitle, &claim, &sourceTitle, &sourceKind, &uri, &canonicalURI, &relativePath, &sheetName, &rowIndex); err != nil {
			return nil, err
		}
		citation := sourceCitationLabel(Source{ID: sourceID, Kind: sourceKind, URI: uri, CanonicalURI: canonicalURI, Title: sourceTitle, RelativePath: relativePath})
		if sheetName != "" {
			citation += " | sheet " + sheetName
		}
		if rowIndex > 0 {
			citation += fmt.Sprintf(" | row %d", rowIndex)
		}
		if cardTitle != "" && cardTitle != sourceTitle {
			citation += " | " + cardTitle
		}
		edges = append(edges, FactGraphEdge{
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
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return edges, nil
}
