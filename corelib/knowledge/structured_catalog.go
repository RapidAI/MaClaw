package knowledge

import (
	"context"
	"fmt"
	"strings"
)

func (s *SQLiteStore) StructuredCatalog(ctx context.Context, opts StructuredCatalogOptions) (StructuredCatalogResult, error) {
	if s == nil || s.db == nil {
		return StructuredCatalogResult{}, fmt.Errorf("knowledge store is nil")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	where, args = appendKBSourceFilters(where, args, "s", opts.OwnerID, opts.TenantID, opts.ProjectPath, opts.SearchScope, nil, append(append([]string{}, opts.SourceIDs...), opts.SourceID), opts.IncludeDisabled)
	if len(opts.SheetNames) > 0 {
		names := normalizeSearchStrings(opts.SheetNames)
		if len(names) == 1 {
			where = append(where, "LOWER(t.sheet_name) = ?")
			args = append(args, names[0])
		} else if len(names) > 1 {
			where = append(where, "LOWER(t.sheet_name) IN ("+placeholders(len(names))+")")
			for _, name := range names {
				args = append(args, name)
			}
		}
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.source_id, COALESCE(s.title, ''), s.kind, t.sheet_name,
			COALESCE(t.table_title, ''), t.row_count, t.column_count
		FROM kb_tables t
		JOIN kb_sources s ON s.id = t.source_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY s.updated_at DESC, t.sheet_name ASC, t.id ASC
		LIMIT ?`, args...)
	if err != nil {
		return StructuredCatalogResult{}, err
	}
	defer rows.Close()
	result := StructuredCatalogResult{Tables: make([]StructuredTableCatalog, 0, limit)}
	tableIDs := make([]string, 0)
	for rows.Next() {
		var table StructuredTableCatalog
		if err := rows.Scan(&table.ID, &table.SourceID, &table.SourceTitle, &table.SourceKind, &table.SheetName, &table.TableTitle, &table.RowCount, &table.ColumnCount); err != nil {
			return StructuredCatalogResult{}, err
		}
		result.Tables = append(result.Tables, table)
		tableIDs = append(tableIDs, table.ID)
	}
	if err := rows.Err(); err != nil {
		return StructuredCatalogResult{}, err
	}
	if len(tableIDs) > 0 {
		columns, err := s.structuredColumnsByTable(ctx, tableIDs)
		if err != nil {
			return StructuredCatalogResult{}, err
		}
		for i := range result.Tables {
			result.Tables[i].Columns = columns[result.Tables[i].ID]
		}
	}
	result.Count = len(result.Tables)
	return result, nil
}

func (s *SQLiteStore) structuredColumnsByTable(ctx context.Context, tableIDs []string) (map[string][]KnowledgeTableColumn, error) {
	out := make(map[string][]KnowledgeTableColumn, len(tableIDs))
	if len(tableIDs) == 0 {
		return out, nil
	}
	args := make([]interface{}, 0, len(tableIDs))
	for _, id := range tableIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, table_id, column_index, column_name, normalized_name, value_type,
			COALESCE(aliases_json, ''), COALESCE(stats_json, '')
		FROM kb_columns
		WHERE table_id IN (`+placeholders(len(tableIDs))+`)
		ORDER BY table_id, column_index ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var column KnowledgeTableColumn
		if err := rows.Scan(&column.ID, &column.TableID, &column.ColumnIndex, &column.ColumnName, &column.NormalizedName, &column.ValueType, &column.AliasesJSON, &column.StatsJSON); err != nil {
			return nil, err
		}
		out[column.TableID] = append(out[column.TableID], column)
	}
	return out, rows.Err()
}
