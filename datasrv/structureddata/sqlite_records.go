package structureddata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	maxFilterListItems = 20
	maxFilterValues    = 100
	maxRecordIndexKeys = 300
	maxSortFields      = 5
)

func (s *SQLiteStore) UpsertFields(ctx context.Context, tenantID, datasetID string, fields []FieldDefinition) ([]FieldDefinition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, field := range fields {
		_, err := tx.ExecContext(ctx, `INSERT INTO field_definitions(id, tenant_id, dataset_id, field_key, field_type, title, required, indexed, sensitive, config_json, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id, dataset_id, field_key) DO UPDATE SET field_type=excluded.field_type, title=excluded.title, required=excluded.required, indexed=excluded.indexed, sensitive=excluded.sensitive, config_json=excluded.config_json, updated_at=excluded.updated_at`,
			field.ID, tenantID, datasetID, field.Key, field.Type, field.Title, boolInt(field.Required), boolInt(field.Indexed), boolInt(field.Sensitive), jsonString(field.Config), formatTime(field.CreatedAt), formatTime(field.UpdatedAt))
		if err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE datasets SET schema_version = schema_version + 1, updated_at = ? WHERE tenant_id = ? AND id = ?`, formatTime(time.Now().UTC()), tenantID, datasetID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.ListFields(ctx, tenantID, datasetID)
}

func (s *SQLiteStore) ListFields(ctx context.Context, tenantID, datasetID string) ([]FieldDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, dataset_id, field_key, field_type, title, required, indexed, sensitive, config_json, created_at, updated_at FROM field_definitions WHERE tenant_id = ? AND dataset_id = ? ORDER BY field_key`, tenantID, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FieldDefinition{}
	for rows.Next() {
		field, err := scanField(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, field)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ImportRecords(ctx context.Context, records []Record) ([]Record, error) {
	if len(records) == 0 {
		return []Record{}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, record := range records {
		dataJSON := jsonString(record.Data)
		_, err := tx.ExecContext(ctx, `INSERT INTO records(id, tenant_id, dataset_id, title, data_json, source_id, created_by, updated_by, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id, dataset_id, id) DO UPDATE SET title=excluded.title, data_json=excluded.data_json, source_id=excluded.source_id, updated_by=excluded.updated_by, updated_at=excluded.updated_at`, record.ID, record.TenantID, record.DatasetID, record.Title, dataJSON, record.SourceID, record.CreatedBy, record.UpdatedBy, formatTime(record.CreatedAt), formatTime(record.UpdatedAt))
		if err != nil {
			return nil, err
		}
		if err := clearRecordIndexes(ctx, tx, record.TenantID, record.DatasetID, record.ID); err != nil {
			return nil, err
		}
		if err := writeRecordIndexes(ctx, tx, record, dataJSON); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	out := make([]Record, 0, len(records))
	for _, record := range records {
		stored, err := s.GetRecord(ctx, record.TenantID, record.DatasetID, record.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *stored)
	}
	return out, nil
}

func (s *SQLiteStore) CreateRecord(ctx context.Context, record Record) (*Record, error) {
	dataJSON := jsonString(record.Data)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx, `INSERT INTO records(id, tenant_id, dataset_id, title, data_json, source_id, created_by, updated_by, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ID, record.TenantID, record.DatasetID, record.Title, dataJSON, record.SourceID, record.CreatedBy, record.UpdatedBy, formatTime(record.CreatedAt), formatTime(record.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	if err := writeRecordIndexes(ctx, tx, record, dataJSON); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.GetRecord(ctx, record.TenantID, record.DatasetID, record.ID)
}

func (s *SQLiteStore) GetRecord(ctx context.Context, tenantID, datasetID, recordID string) (*Record, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, dataset_id, title, data_json, source_id, created_by, updated_by, created_at, updated_at FROM records WHERE tenant_id = ? AND dataset_id = ? AND id = ?`, tenantID, datasetID, recordID)
	record, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	record.Tags, err = s.recordTags(ctx, tenantID, datasetID, recordID)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLiteStore) UpdateRecord(ctx context.Context, tenantID, datasetID, recordID string, in UpdateRecordInput, actor string, now time.Time) (*Record, error) {
	record, err := s.GetRecord(ctx, tenantID, datasetID, recordID)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		record.Title = strings.TrimSpace(*in.Title)
	}
	if in.Tags != nil {
		record.Tags = normalizeTags(in.Tags)
	}
	if in.Data != nil {
		record.Data = cloneJSONMap(in.Data)
	}
	record.UpdatedBy = strings.TrimSpace(actor)
	record.UpdatedAt = now
	dataJSON := jsonString(record.Data)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx, `UPDATE records SET title = ?, data_json = ?, updated_by = ?, updated_at = ? WHERE tenant_id = ? AND dataset_id = ? AND id = ?`, record.Title, dataJSON, record.UpdatedBy, formatTime(record.UpdatedAt), tenantID, datasetID, recordID)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrRecordNotFound
	}
	if err := clearRecordIndexes(ctx, tx, tenantID, datasetID, recordID); err != nil {
		return nil, err
	}
	if err := writeRecordIndexes(ctx, tx, *record, dataJSON); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.GetRecord(ctx, tenantID, datasetID, recordID)
}

func (s *SQLiteStore) DeleteRecord(ctx context.Context, tenantID, datasetID, recordID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx, `DELETE FROM records WHERE tenant_id = ? AND dataset_id = ? AND id = ?`, tenantID, datasetID, recordID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrRecordNotFound
	}
	if err := clearRecordIndexes(ctx, tx, tenantID, datasetID, recordID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLiteStore) QueryRecords(ctx context.Context, tenantID, datasetID string, in QueryRecordsInput) ([]Record, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	clauses := []string{"r.tenant_id = ?", "r.dataset_id = ?"}
	whereArgs := []any{tenantID, datasetID}
	joinArgs := []any{}
	join := ""
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" && len(in.Sort) == 0 {
			clauses = append(clauses, "(r.created_at < ? OR (r.created_at = ? AND r.id < ?))")
			whereArgs = append(whereArgs, before, before, beforeID)
		} else {
			clauses = append(clauses, "r.created_at < ?")
			whereArgs = append(whereArgs, before)
		}
	}
	if tag := strings.ToLower(strings.TrimSpace(in.Tag)); tag != "" {
		clauses = append(clauses, `r.id IN (
			SELECT record_id FROM record_tags
			WHERE tenant_id = ? AND dataset_id = ? AND tag_norm = ?
		)`)
		whereArgs = append(whereArgs, tenantID, datasetID, tag)
	}
	if q := strings.TrimSpace(in.Q); q != "" {
		clauses = append(clauses, `r.id IN (
			SELECT record_id FROM record_fts
			WHERE tenant_id = ? AND dataset_id = ? AND record_fts MATCH ?
		)`)
		whereArgs = append(whereArgs, tenantID, datasetID, ftsQuery(q))
	}
	filterJoin, filterClauses, filterJoinArgs, filterWhereArgs, err := buildFilterSQL(in.Filter)
	if err != nil {
		return nil, err
	}
	join += filterJoin
	joinArgs = append(joinArgs, filterJoinArgs...)
	clauses = append(clauses, filterClauses...)
	whereArgs = append(whereArgs, filterWhereArgs...)
	sortJoin, sortArgs, orderBy, err := buildSortSQL(in.Sort)
	if err != nil {
		return nil, err
	}
	join += sortJoin
	joinArgs = append(joinArgs, sortArgs...)
	args := append(joinArgs, whereArgs...)
	args = append(args, limit)
	query := fmt.Sprintf(`SELECT r.id, r.tenant_id, r.dataset_id, r.title, r.data_json, r.source_id, r.created_by, r.updated_by, r.created_at, r.updated_at FROM records r%s WHERE %s GROUP BY r.tenant_id, r.dataset_id, r.id ORDER BY %s LIMIT ?`, join, strings.Join(clauses, " AND "), orderBy)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	records := []Record{}
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range records {
		records[i].Tags, err = s.recordTags(ctx, tenantID, datasetID, records[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}

func writeRecordIndexes(ctx context.Context, tx *sql.Tx, record Record, dataJSON string) error {
	if err := writeRecordFieldIndexes(ctx, tx, record); err != nil {
		return err
	}
	for _, tag := range record.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO record_tags(tenant_id, dataset_id, record_id, tag, tag_norm) VALUES(?, ?, ?, ?, ?)`, record.TenantID, record.DatasetID, record.ID, tag, strings.ToLower(strings.TrimSpace(tag))); err != nil {
			return err
		}
	}
	text := strings.Join(append([]string{record.Title, dataJSON}, record.Tags...), " ")
	_, err := tx.ExecContext(ctx, `INSERT INTO record_fts(tenant_id, dataset_id, record_id, text) VALUES(?, ?, ?, ?)`, record.TenantID, record.DatasetID, record.ID, text)
	return err
}

func writeRecordFieldIndexes(ctx context.Context, tx *sql.Tx, record Record) error {
	indexValues := recordIndexValues(record.Data)
	if len(indexValues) > maxRecordIndexKeys {
		return fmt.Errorf("%w: record has too many indexed data fields", ErrInvalidInput)
	}
	for _, indexed := range indexValues {
		valueText := indexValueText(indexed.Value)
		var valueNumber any
		if n, ok := numberValue(indexed.Value); ok {
			valueNumber = n
		}
		var valueTime any
		if t, ok := timeValue(indexed.Value); ok {
			valueTime = t
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text, value_number, value_time, value_hash) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, record.TenantID, record.DatasetID, record.ID, indexed.Key, valueText, valueNumber, valueTime, indexed.Hash); err != nil {
			return err
		}
	}
	return nil
}

type recordIndexValue struct {
	Key   string
	Value any
	Hash  string
}

func recordIndexValues(data map[string]any) []recordIndexValue {
	out := []recordIndexValue{}
	seen := map[string]struct{}{}
	add := func(key string, value any) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		hash := recordIndexValueHash(value)
		dedupeKey := key + "\x00" + hash
		if _, exists := seen[dedupeKey]; exists {
			return
		}
		seen[dedupeKey] = struct{}{}
		out = append(out, recordIndexValue{Key: key, Value: value, Hash: hash})
	}
	var walk func(prefix string, value any, depth int)
	walk = func(prefix string, value any, depth int) {
		if strings.TrimSpace(prefix) == "" {
			return
		}
		arrayValues := scalarArrayValues(value)
		if !isArrayValue(value) {
			add(prefix, value)
		}
		for _, item := range arrayValues {
			add(prefix, item)
		}
		if depth >= 4 {
			return
		}
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		for _, key := range sortedKeys(object) {
			child := strings.TrimSpace(key)
			if child == "" {
				continue
			}
			walk(prefix+"."+child, object[key], depth+1)
		}
	}
	for _, key := range sortedKeys(data) {
		walk(strings.TrimSpace(key), data[key], 0)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key == out[j].Key {
			return out[i].Hash < out[j].Hash
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func scalarArrayValues(value any) []any {
	switch v := value.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			if isScalarIndexValue(item) {
				out = append(out, item)
			}
		}
		return out
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []int:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []int64:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []float64:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []bool:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func isArrayValue(value any) bool {
	switch value.(type) {
	case []any, []string, []int, []int64, []float64, []bool:
		return true
	default:
		return false
	}
}

func isScalarIndexValue(value any) bool {
	switch value.(type) {
	case string, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number, time.Time:
		return true
	default:
		return false
	}
}

func indexValueText(value any) string {
	if value == nil {
		return ""
	}
	switch value.(type) {
	case map[string]any, []any:
		return jsonString(value)
	default:
		return fmt.Sprint(value)
	}
}

func recordIndexValueHash(value any) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%T\x00%s", value, indexValueText(value))))
	return hex.EncodeToString(sum[:])
}

func clearRecordIndexes(ctx context.Context, tx *sql.Tx, tenantID, datasetID, recordID string) error {
	stmts := []string{
		`DELETE FROM record_field_index WHERE tenant_id = ? AND dataset_id = ? AND record_id = ?`,
		`DELETE FROM record_tags WHERE tenant_id = ? AND dataset_id = ? AND record_id = ?`,
		`DELETE FROM record_fts WHERE tenant_id = ? AND dataset_id = ? AND record_id = ?`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt, tenantID, datasetID, recordID); err != nil {
			return err
		}
	}
	return nil
}

func scanField(scanner interface{ Scan(dest ...any) error }) (FieldDefinition, error) {
	var field FieldDefinition
	var required, indexed, sensitive int
	var configJSON string
	var createdAt, updatedAt string
	if err := scanner.Scan(&field.ID, &field.TenantID, &field.DatasetID, &field.Key, &field.Type, &field.Title, &required, &indexed, &sensitive, &configJSON, &createdAt, &updatedAt); err != nil {
		return FieldDefinition{}, err
	}
	field.Required = intBool(required)
	field.Indexed = intBool(indexed)
	field.Sensitive = intBool(sensitive)
	field.Config = map[string]any{}
	_ = json.Unmarshal([]byte(configJSON), &field.Config)
	field.CreatedAt = parseTime(createdAt)
	field.UpdatedAt = parseTime(updatedAt)
	return field, nil
}

func scanRecord(scanner interface{ Scan(dest ...any) error }) (Record, error) {
	var record Record
	var dataJSON string
	var createdAt, updatedAt string
	if err := scanner.Scan(&record.ID, &record.TenantID, &record.DatasetID, &record.Title, &dataJSON, &record.SourceID, &record.CreatedBy, &record.UpdatedBy, &createdAt, &updatedAt); err != nil {
		return Record{}, err
	}
	record.Data = map[string]any{}
	if err := json.Unmarshal([]byte(dataJSON), &record.Data); err != nil {
		return Record{}, err
	}
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

func (s *SQLiteStore) recordTags(ctx context.Context, tenantID, datasetID, recordID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tag FROM record_tags WHERE tenant_id = ? AND dataset_id = ? AND record_id = ? ORDER BY tag`, tenantID, datasetID, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func buildFilterSQL(filter map[string]any) (string, []string, []any, []any, error) {
	if len(filter) == 0 {
		return "", nil, nil, nil, nil
	}
	op := strings.ToLower(strings.TrimSpace(stringValue(filter["op"])))
	if _, ok := filter["and"]; ok && op == "" {
		op = "and"
	}
	if _, ok := filter["or"]; ok && op == "" {
		op = "or"
	}
	switch op {
	case "and":
		filters, err := compoundFilters(filter, "and")
		if err != nil {
			return "", nil, nil, nil, err
		}
		joins := []string{}
		clauses := []string{}
		joinArgs := []any{}
		whereArgs := []any{}
		for i, item := range filters {
			join, childClauses, childJoinArgs, childWhereArgs, err := buildFieldFilterSQL(item, fmt.Sprintf("fi%d", i))
			if err != nil {
				return "", nil, nil, nil, err
			}
			joins = append(joins, join)
			clauses = append(clauses, childClauses...)
			joinArgs = append(joinArgs, childJoinArgs...)
			whereArgs = append(whereArgs, childWhereArgs...)
		}
		return strings.Join(joins, ""), clauses, joinArgs, whereArgs, nil
	case "or":
		filters, err := compoundFilters(filter, "or")
		if err != nil {
			return "", nil, nil, nil, err
		}
		clauses := []string{}
		whereArgs := []any{}
		for i, item := range filters {
			clause, args, err := buildFieldFilterExistsSQL(item, fmt.Sprintf("fo%d", i))
			if err != nil {
				return "", nil, nil, nil, err
			}
			clauses = append(clauses, clause)
			whereArgs = append(whereArgs, args...)
		}
		return "", []string{"(" + strings.Join(clauses, " OR ") + ")"}, nil, whereArgs, nil
	}
	return buildFieldFilterSQL(filter, "fi")
}

func buildFieldFilterSQL(filter map[string]any, alias string) (string, []string, []any, []any, error) {
	switch strings.ToLower(strings.TrimSpace(stringValue(filter["op"]))) {
	case "neq", "not_in", "not_contains", "not_exists", "empty", "not_empty":
		clause, args, err := buildFieldFilterExistsSQL(filter, alias)
		if err != nil {
			return "", nil, nil, nil, err
		}
		return "", []string{clause}, nil, args, nil
	}
	field, _ := filter["field"].(string)
	op, _ := filter["op"].(string)
	if strings.TrimSpace(field) == "" || strings.TrimSpace(op) == "" {
		return "", nil, nil, nil, fmt.Errorf("%w: filter requires field and op", ErrInvalidInput)
	}
	join := fmt.Sprintf(" JOIN record_field_index %s ON %s.tenant_id = r.tenant_id AND %s.dataset_id = r.dataset_id AND %s.record_id = r.id AND %s.field_key = ?", alias, alias, alias, alias, alias)
	joinArgs := []any{field}
	clauses, whereArgs, err := buildFieldFilterClauses(filter, alias)
	if err != nil {
		return "", nil, nil, nil, err
	}
	return join, clauses, joinArgs, whereArgs, nil
}

func buildFieldFilterExistsSQL(filter map[string]any, alias string) (string, []any, error) {
	field, _ := filter["field"].(string)
	op := strings.ToLower(strings.TrimSpace(stringValue(filter["op"])))
	if strings.TrimSpace(field) == "" || op == "" {
		return "", nil, fmt.Errorf("%w: filter requires field and op", ErrInvalidInput)
	}
	switch op {
	case "not_exists":
		return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM record_field_index %s WHERE %s.tenant_id = r.tenant_id AND %s.dataset_id = r.dataset_id AND %s.record_id = r.id AND %s.field_key = ?)", alias, alias, alias, alias, alias), []any{field}, nil
	case "empty":
		return fmt.Sprintf("(NOT EXISTS (SELECT 1 FROM record_field_index %s_missing WHERE %s_missing.tenant_id = r.tenant_id AND %s_missing.dataset_id = r.dataset_id AND %s_missing.record_id = r.id AND %s_missing.field_key = ?) OR EXISTS (SELECT 1 FROM record_field_index %s WHERE %s.tenant_id = r.tenant_id AND %s.dataset_id = r.dataset_id AND %s.record_id = r.id AND %s.field_key = ? AND TRIM(%s.value_text) = ''))", alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias), []any{field, field}, nil
	case "not_empty":
		return fmt.Sprintf("EXISTS (SELECT 1 FROM record_field_index %s WHERE %s.tenant_id = r.tenant_id AND %s.dataset_id = r.dataset_id AND %s.record_id = r.id AND %s.field_key = ? AND TRIM(%s.value_text) != '')", alias, alias, alias, alias, alias, alias), []any{field}, nil
	case "neq", "not_in", "not_contains":
		positive := positiveFilterForNegation(filter, op)
		clauses, args, err := buildFieldFilterClauses(positive, alias)
		if err != nil {
			return "", nil, err
		}
		where := []string{
			alias + ".tenant_id = r.tenant_id",
			alias + ".dataset_id = r.dataset_id",
			alias + ".record_id = r.id",
			alias + ".field_key = ?",
		}
		where = append(where, clauses...)
		clause := fmt.Sprintf("(EXISTS (SELECT 1 FROM record_field_index %s_exists WHERE %s_exists.tenant_id = r.tenant_id AND %s_exists.dataset_id = r.dataset_id AND %s_exists.record_id = r.id AND %s_exists.field_key = ?) AND NOT EXISTS (SELECT 1 FROM record_field_index %s WHERE %s))", alias, alias, alias, alias, alias, alias, strings.Join(where, " AND "))
		allArgs := []any{field, field}
		allArgs = append(allArgs, args...)
		return clause, allArgs, nil
	}
	clauses, args, err := buildFieldFilterClauses(filter, alias)
	if err != nil {
		return "", nil, err
	}
	allArgs := append([]any{field}, args...)
	where := []string{
		alias + ".tenant_id = r.tenant_id",
		alias + ".dataset_id = r.dataset_id",
		alias + ".record_id = r.id",
		alias + ".field_key = ?",
	}
	where = append(where, clauses...)
	return fmt.Sprintf("EXISTS (SELECT 1 FROM record_field_index %s WHERE %s)", alias, strings.Join(where, " AND ")), allArgs, nil
}

func positiveFilterForNegation(filter map[string]any, op string) map[string]any {
	out := map[string]any{}
	for key, value := range filter {
		out[key] = value
	}
	switch op {
	case "neq":
		out["op"] = "eq"
	case "not_in":
		out["op"] = "in"
	case "not_contains":
		out["op"] = "contains"
	}
	return out
}

func buildFieldFilterClauses(filter map[string]any, alias string) ([]string, []any, error) {
	op, _ := filter["op"].(string)
	value := filter["value"]
	switch strings.ToLower(op) {
	case "exists":
		return nil, nil, nil
	case "empty", "not_empty":
		return nil, nil, fmt.Errorf("%w: %s filter must be used as a field presence filter", ErrInvalidInput, strings.ToLower(op))
	case "eq":
		return []string{alias + ".value_text = ? COLLATE NOCASE"}, []any{fmt.Sprint(value)}, nil
	case "neq":
		return []string{alias + ".value_text != ? COLLATE NOCASE"}, []any{fmt.Sprint(value)}, nil
	case "in", "not_in":
		values, ok := filterValues(value)
		if !ok || len(values) == 0 {
			return nil, nil, fmt.Errorf("%w: non-empty filter values required", ErrInvalidInput)
		}
		if len(values) > maxFilterValues {
			return nil, nil, fmt.Errorf("%w: too many filter values", ErrInvalidInput)
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(values)), ",")
		operator := "IN"
		if strings.EqualFold(op, "not_in") {
			operator = "NOT IN"
		}
		return []string{alias + ".value_text COLLATE NOCASE " + operator + " (" + placeholders + ")"}, values, nil
	case "contains":
		return []string{alias + ".value_text LIKE ? COLLATE NOCASE"}, []any{"%" + fmt.Sprint(value) + "%"}, nil
	case "not_contains":
		return []string{alias + ".value_text NOT LIKE ? COLLATE NOCASE"}, []any{"%" + fmt.Sprint(value) + "%"}, nil
	case "prefix":
		return []string{alias + ".value_text LIKE ? COLLATE NOCASE"}, []any{fmt.Sprint(value) + "%"}, nil
	case "between":
		values, ok := filterValues(value)
		if !ok || len(values) != 2 {
			return nil, nil, fmt.Errorf("%w: between filter requires exactly two values", ErrInvalidInput)
		}
		if start, ok := numberValue(values[0]); ok {
			end, ok := numberValue(values[1])
			if !ok {
				return nil, nil, fmt.Errorf("%w: between filter values must use same comparable type", ErrInvalidInput)
			}
			return []string{alias + ".value_number >= ?", alias + ".value_number <= ?"}, []any{start, end}, nil
		}
		if start, ok := timeValue(values[0]); ok {
			end, ok := timeValue(values[1])
			if !ok {
				return nil, nil, fmt.Errorf("%w: between filter values must use same comparable type", ErrInvalidInput)
			}
			return []string{alias + ".value_time >= ?", alias + ".value_time <= ?"}, []any{start, end}, nil
		}
		return []string{alias + ".value_text >= ? COLLATE NOCASE", alias + ".value_text <= ? COLLATE NOCASE"}, values, nil
	case "gt", "gte", "lt", "lte":
		operator := map[string]string{"gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[strings.ToLower(op)]
		if n, ok := numberValue(value); ok {
			return []string{alias + ".value_number " + operator + " ?"}, []any{n}, nil
		}
		if t, ok := timeValue(value); ok {
			return []string{alias + ".value_time " + operator + " ?"}, []any{t}, nil
		}
		return nil, nil, fmt.Errorf("%w: numeric or time filter value required", ErrInvalidInput)
	default:
		return nil, nil, fmt.Errorf("%w: unsupported filter op", ErrInvalidInput)
	}
}

func compoundFilters(filter map[string]any, op string) ([]map[string]any, error) {
	value := filter["filters"]
	if value == nil {
		value = filter[op]
	}
	filters, ok := filterList(value)
	if !ok || len(filters) == 0 {
		return nil, fmt.Errorf("%w: %s filter requires filters", ErrInvalidInput, op)
	}
	if len(filters) > maxFilterListItems {
		return nil, fmt.Errorf("%w: too many %s filters", ErrInvalidInput, op)
	}
	return filters, nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func filterList(value any) ([]map[string]any, bool) {
	switch items := value.(type) {
	case []map[string]any:
		return items, true
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			filter, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			out = append(out, filter)
		}
		return out, true
	default:
		return nil, false
	}
}

func filterValues(value any) ([]any, bool) {
	switch values := value.(type) {
	case []any:
		out := make([]any, 0, len(values))
		for _, item := range values {
			out = append(out, fmt.Sprint(item))
		}
		return out, true
	case []string:
		out := make([]any, 0, len(values))
		for _, item := range values {
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}

func buildSortSQL(sortSpecs []SortSpec) (string, []any, string, error) {
	if len(sortSpecs) == 0 {
		return "", nil, "r.created_at DESC, r.id DESC", nil
	}
	if len(sortSpecs) > maxSortFields {
		return "", nil, "", fmt.Errorf("%w: too many sort fields", ErrInvalidInput)
	}
	joins := []string{}
	args := []any{}
	order := []string{}
	for i, spec := range sortSpecs {
		field := strings.TrimSpace(spec.Field)
		if field == "" {
			return "", nil, "", fmt.Errorf("%w: sort field is required", ErrInvalidInput)
		}
		dir := "ASC"
		switch strings.ToLower(strings.TrimSpace(spec.Direction)) {
		case "", "asc":
			dir = "ASC"
		case "desc":
			dir = "DESC"
		default:
			return "", nil, "", fmt.Errorf("%w: unsupported sort direction", ErrInvalidInput)
		}
		switch field {
		case "id":
			order = append(order, "r.id "+dir)
		case "title":
			order = append(order, "r.title COLLATE NOCASE "+dir)
		case "created_at":
			order = append(order, "r.created_at "+dir)
		case "updated_at":
			order = append(order, "r.updated_at "+dir)
		default:
			alias := fmt.Sprintf("s%d", i)
			aggregate := "MIN"
			if dir == "DESC" {
				aggregate = "MAX"
			}
			joins = append(joins, fmt.Sprintf(" LEFT JOIN (SELECT tenant_id, dataset_id, record_id, %s(value_number) AS value_number, %s(value_time) AS value_time, %s(value_text COLLATE NOCASE) AS value_text FROM record_field_index WHERE field_key = ? GROUP BY tenant_id, dataset_id, record_id) %s ON %s.tenant_id = r.tenant_id AND %s.dataset_id = r.dataset_id AND %s.record_id = r.id", aggregate, aggregate, aggregate, alias, alias, alias, alias))
			args = append(args, field)
			order = append(order,
				alias+".value_number IS NULL",
				alias+".value_number "+dir,
				alias+".value_time IS NULL",
				alias+".value_time "+dir,
				alias+".value_text COLLATE NOCASE "+dir,
			)
		}
	}
	order = append(order, "r.created_at DESC", "r.id DESC")
	return strings.Join(joins, ""), args, strings.Join(order, ", "), nil
}
