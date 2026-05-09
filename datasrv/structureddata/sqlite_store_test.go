package structureddata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStoreDatasetFieldRecordFlow(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}

	ready, err := svc.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if ready.SchemaVersion != currentSchemaVersion || ready.Engine != "sqlite" {
		t.Fatalf("unexpected readiness: %#v", ready)
	}

	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "finance", Name: "expenses", Title: "Expenses"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if ds.ID != "finance.expenses" {
		t.Fatalf("dataset id = %q", ds.ID)
	}

	fields, err := svc.UpsertFields(context.Background(), p, ds.ID, UpsertFieldsInput{Fields: []FieldDefinition{{Key: "amount", Type: "number", Indexed: true}, {Key: "department", Type: "string", Indexed: true}}})
	if err != nil {
		t.Fatalf("UpsertFields: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("fields = %#v", fields)
	}

	rec, err := svc.CreateRecord(context.Background(), p, ds.ID, CreateRecordInput{Title: "March travel", Tags: []string{"travel", "finance"}, Data: map[string]any{"amount": 1200, "department": "Sales", "employee": "Alice", "expense_date": "2026-03-10"}})
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	items, err := svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Q: "March", Tag: "travel", Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords fts/tag: %v", err)
	}
	if len(items) != 1 || items[0].ID != rec.ID {
		t.Fatalf("unexpected q/tag items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "amount", "op": "gte", "value": 1000}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords filter: %v", err)
	}
	if len(items) != 1 || items[0].Data["department"] != "Sales" {
		t.Fatalf("unexpected filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "department", "op": "eq", "value": "sales"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords case-insensitive eq filter: %v", err)
	}
	if len(items) != 1 || items[0].Data["department"] != "Sales" {
		t.Fatalf("unexpected case-insensitive eq filter items: %#v", items)
	}

	if _, err := svc.CreateRecord(context.Background(), p, ds.ID, CreateRecordInput{Title: "April office", Tags: []string{"finance"}, Data: map[string]any{"amount": 300, "department": "Ops", "employee": "Bob", "expense_date": "2026-04-01", "approved_by": "", "reviewers": []any{}, "nullable_note": nil, "nullable_reviewers": []any{nil}}}); err != nil {
		t.Fatalf("CreateRecord sort fixture: %v", err)
	}
	if _, err := svc.CreateRecord(context.Background(), p, ds.ID, CreateRecordInput{Title: "May software", Tags: []string{"finance"}, Data: map[string]any{"amount": 800, "department": "Engineering", "employee": "Cora", "expense_date": "2026-05-01", "approved_by": "Dana", "approvers": []any{"Dana", "Manager"}, "reviewers": []any{"Dana"}, "nullable_reviewers": []any{"Dana", nil}, "approval": map[string]any{"assigned_to": "Dana", "status": "approved", "score": 7}}}); err != nil {
		t.Fatalf("CreateRecord second sort fixture: %v", err)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Sort: []SortSpec{{Field: "amount", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords numeric sort: %v", err)
	}
	if len(items) != 3 || items[0].Title != "April office" || items[2].Title != "March travel" {
		t.Fatalf("unexpected numeric sort items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Sort: []SortSpec{{Field: "department", Direction: "desc"}, {Field: "title", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords text sort: %v", err)
	}
	if len(items) != 3 || items[0].Data["department"] != "Sales" || items[2].Data["department"] != "Engineering" {
		t.Fatalf("unexpected text sort items: %#v", items)
	}

	if _, err := svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Sort: []SortSpec{{Field: "amount", Direction: "sideways"}}, Limit: 10}); err == nil {
		t.Fatalf("expected invalid sort direction to fail")
	}
	if _, err := svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Sort: []SortSpec{{Field: "amount"}, {Field: "department"}, {Field: "employee"}, {Field: "expense_date"}, {Field: "title"}, {Field: "id"}}, Limit: 10}); err == nil {
		t.Fatalf("expected too many sort fields to fail")
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "expense_date", "op": "gte", "value": "2026-04-01"}, Sort: []SortSpec{{Field: "expense_date", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords time filter: %v", err)
	}
	if len(items) != 2 || items[0].Title != "April office" || items[1].Title != "May software" {
		t.Fatalf("unexpected time filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "department", "op": "in", "value": []any{"Sales", "Engineering"}}, Sort: []SortSpec{{Field: "department", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords in filter: %v", err)
	}
	if len(items) != 2 || items[0].Data["department"] != "Engineering" || items[1].Data["department"] != "Sales" {
		t.Fatalf("unexpected in filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "department", "op": "prefix", "value": "eng"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords prefix filter: %v", err)
	}
	if len(items) != 1 || items[0].Data["department"] != "Engineering" {
		t.Fatalf("unexpected prefix filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "department", "op": "not_contains", "value": "eng"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords not_contains filter: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected not_contains filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "department", "op": "not_in", "value": []any{"Ops", "Engineering"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords not_in filter: %v", err)
	}
	if len(items) != 1 || items[0].Data["department"] != "Sales" {
		t.Fatalf("unexpected not_in filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "department", "op": "neq", "value": "Ops"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords neq filter: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected neq filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{
		Filter: map[string]any{
			"op": "and",
			"filters": []any{
				map[string]any{"field": "department", "op": "in", "value": []any{"Sales", "Engineering"}},
				map[string]any{"field": "expense_date", "op": "gte", "value": "2026-04-01"},
			},
		},
		Sort:  []SortSpec{{Field: "expense_date", Direction: "asc"}},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("QueryRecords and filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("unexpected and filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{
		Filter: map[string]any{
			"or": []any{
				map[string]any{"field": "department", "op": "eq", "value": "Sales"},
				map[string]any{"field": "amount", "op": "lt", "value": 500},
			},
		},
		Sort:  []SortSpec{{Field: "amount", Direction: "asc"}},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("QueryRecords or filter: %v", err)
	}
	if len(items) != 2 || items[0].Title != "April office" || items[1].Title != "March travel" {
		t.Fatalf("unexpected or filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "expense_date", "op": "between", "value": []any{"2026-03-01", "2026-04-30"}}, Sort: []SortSpec{{Field: "expense_date", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords between filter: %v", err)
	}
	if len(items) != 2 || items[0].Title != "March travel" || items[1].Title != "April office" {
		t.Fatalf("unexpected between filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "employee", "op": "exists"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords exists filter: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("unexpected exists filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approved_by", "op": "not_exists"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords not_exists filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "March travel" {
		t.Fatalf("unexpected not_exists filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approved_by", "op": "empty"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords empty filter: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected empty filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approved_by", "op": "not_empty"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords not_empty filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("unexpected not_empty filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approval.assigned_to", "op": "eq", "value": "dana"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords nested field filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("unexpected nested field filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approval.status", "op": "prefix", "value": "app"}, Sort: []SortSpec{{Field: "approval.assigned_to", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords nested field prefix/sort: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("unexpected nested field prefix/sort items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approvers", "op": "eq", "value": "manager"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array eq filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("unexpected scalar array eq filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approvers", "op": "contains", "value": "dan"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array contains filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("unexpected scalar array contains filter items: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approvers", "op": "neq", "value": "Dana"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array neq filter: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("scalar array neq should reject records with any matching element: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "approvers", "op": "not_in", "value": []any{"Dana"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array not_in filter: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("scalar array not_in should reject records with any matching element: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "reviewers", "op": "exists"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array exists filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("empty scalar arrays should not count as existing field indexes: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "reviewers", "op": "empty"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array empty filter: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("empty scalar array should be treated like an empty/missing field: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "nullable_reviewers", "op": "exists"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords null array exists filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "May software" {
		t.Fatalf("array null elements should not count as existing field indexes: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "nullable_reviewers", "op": "empty"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords null array empty filter: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("array with only null elements should be treated like an empty/missing field: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "nullable_note", "op": "exists"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords null field exists filter: %v", err)
	}
	if len(items) != 1 || items[0].Title != "April office" {
		t.Fatalf("null field should remain detectable as present: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Filter: map[string]any{"field": "nullable_note", "op": "not_empty"}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords null field not_empty filter: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("null field should not count as not_empty: %#v", items)
	}

	aggregate, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}, Limit: 1})
	if err != nil {
		t.Fatalf("AggregateRecords sum: %v", err)
	}
	if aggregate.Scanned != 3 || len(aggregate.Rows) != 1 || aggregate.Rows[0]["amount"] != float64(2300) {
		t.Fatalf("unexpected aggregate sum: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}, Limit: 1, ScanLimit: 2})
	if err != nil {
		t.Fatalf("AggregateRecords scan limit: %v", err)
	}
	if aggregate.Scanned != 2 || !aggregate.Truncated || aggregate.ScanLimit != 2 {
		t.Fatalf("expected aggregate scan limit to report truncation: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "expenses", Op: "count"}}, Limit: 1})
	if err != nil {
		t.Fatalf("AggregateRecords grouped limit: %v", err)
	}
	if aggregate.Scanned != 3 || len(aggregate.Rows) != 1 || aggregate.Limit != 1 {
		t.Fatalf("aggregate limit should only cap returned rows: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}, Sort: []SortSpec{{Field: "amount", Direction: "desc"}}, Limit: 2})
	if err != nil {
		t.Fatalf("AggregateRecords sorted groups: %v", err)
	}
	if len(aggregate.Rows) != 2 || aggregate.Rows[0]["department"] != "Sales" || aggregate.Rows[1]["department"] != "Engineering" {
		t.Fatalf("unexpected sorted aggregate groups: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{As: "total_amount", Op: "sum", Field: "amount"}}, Sort: []SortSpec{{Field: "total_amount", Direction: "desc"}}, Limit: 1})
	if err != nil {
		t.Fatalf("AggregateRecords metric alias: %v", err)
	}
	if len(aggregate.Rows) != 1 || aggregate.Rows[0]["total_amount"] != float64(1200) {
		t.Fatalf("unexpected aggregate metric alias rows: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Filter: map[string]any{"field": "approval.status", "op": "exists"}, GroupBy: []string{"approval.status"}, Metrics: []AggregateMetric{{Name: "expenses", Op: "count"}, {Name: "score", Op: "sum", Field: "approval.score"}}, Limit: 10})
	if err != nil {
		t.Fatalf("AggregateRecords nested group/metric: %v", err)
	}
	if len(aggregate.Rows) != 1 || aggregate.Rows[0]["approval.status"] != "approved" || aggregate.Rows[0]["expenses"] != 1 || aggregate.Rows[0]["score"] != float64(7) {
		t.Fatalf("unexpected nested aggregate rows: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Filter: map[string]any{"field": "approvers", "op": "exists"}, GroupBy: []string{"approvers"}, Metrics: []AggregateMetric{{Name: "expenses", Op: "count"}}, Sort: []SortSpec{{Field: "approvers", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("AggregateRecords scalar array group: %v", err)
	}
	if len(aggregate.Rows) != 2 || aggregate.Rows[0]["approvers"] != "Dana" || aggregate.Rows[0]["expenses"] != 1 || aggregate.Rows[1]["approvers"] != "Manager" || aggregate.Rows[1]["expenses"] != 1 {
		t.Fatalf("unexpected scalar array aggregate rows: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "distinct_departments", Op: "count_distinct", Field: "department"}, {Name: "distinct_approvers", Op: "count_distinct", Field: "approvers"}}})
	if err != nil {
		t.Fatalf("AggregateRecords count_distinct: %v", err)
	}
	if len(aggregate.Rows) != 1 || aggregate.Rows[0]["distinct_departments"] != 3 || aggregate.Rows[0]["distinct_approvers"] != 2 {
		t.Fatalf("unexpected count_distinct aggregate rows: %#v", aggregate)
	}

	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "bad", Op: "median", Field: "amount"}}}); err == nil {
		t.Fatalf("expected invalid aggregate metric op to fail")
	}
	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "bad", Op: "sum"}}}); err == nil {
		t.Fatalf("expected aggregate metric without field to fail")
	}
	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"a", "b", "c", "d", "e", "f"}, Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}}); err == nil {
		t.Fatalf("expected too many aggregate group_by fields to fail")
	}
	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}, Sort: []SortSpec{{Field: "amount", Direction: "sideways"}}}); err == nil {
		t.Fatalf("expected invalid aggregate sort direction to fail")
	}
	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}, Sort: []SortSpec{{Field: "missing", Direction: "asc"}}}); err == nil {
		t.Fatalf("expected invalid aggregate sort field to fail")
	}
	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}, Limit: maxAggregateOutputLimit + 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected oversized aggregate limit to fail with ErrInvalidInput, got %v", err)
	}
	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "amount", Op: "sum", Field: "amount"}}, ScanLimit: maxAggregateScanLimit + 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected oversized aggregate scan limit to fail with ErrInvalidInput, got %v", err)
	}

	report, err := svc.RunReport(context.Background(), p, "finance.expense_by_department", AggregateInput{Sort: []SortSpec{{Field: "amount", Direction: "desc"}}, Limit: 1, ScanLimit: 2})
	if err != nil {
		t.Fatalf("RunReport override sort and scan limit: %v", err)
	}
	if report.Result.Limit != 1 || report.Result.ScanLimit != 2 || !report.Result.Truncated || len(report.Result.Rows) != 1 {
		t.Fatalf("unexpected report override result: %#v", report)
	}
	if _, err := svc.RunReport(context.Background(), p, "finance.expense_by_department", AggregateInput{Limit: maxAggregateOutputLimit + 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected oversized report limit to fail with ErrInvalidInput, got %v", err)
	}
	if _, err := svc.RunReport(context.Background(), p, "finance.expense_by_department", AggregateInput{ScanLimit: maxAggregateScanLimit + 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected oversized report scan limit to fail with ErrInvalidInput, got %v", err)
	}
}

func TestSQLiteStoreRecordCursorKeepsCreatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "ops", Name: "cursor", Title: "Cursor"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	createdAt := time.Date(2026, 5, 6, 8, 30, 0, 0, time.UTC)
	records := make([]Record, 0, 505)
	for i := 0; i < 505; i++ {
		records = append(records, Record{
			ID:        fmt.Sprintf("rec_%03d", i),
			TenantID:  p.TenantID,
			DatasetID: ds.ID,
			Title:     fmt.Sprintf("Record %03d", i),
			Data:      map[string]any{"status": "open", "seq": i},
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		})
	}
	if _, err := store.ImportRecords(context.Background(), records); err != nil {
		t.Fatalf("ImportRecords: %v", err)
	}

	firstPage, err := svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Limit: 2})
	if err != nil {
		t.Fatalf("QueryRecords first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "rec_504" || firstPage[1].ID != "rec_503" {
		t.Fatalf("unexpected first page order: %#v", firstPage)
	}
	secondPage, err := svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Limit: 2, Before: firstPage[1].CreatedAt.Format(time.RFC3339Nano), BeforeID: firstPage[1].ID})
	if err != nil {
		t.Fatalf("QueryRecords second page: %v", err)
	}
	if len(secondPage) != 2 || secondPage[0].ID != "rec_502" || secondPage[1].ID != "rec_501" {
		t.Fatalf("cursor with before_id should continue through created_at ties: %#v", secondPage)
	}

	aggregate, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Metrics: []AggregateMetric{{Name: "records", Op: "count"}}, ScanLimit: 505})
	if err != nil {
		t.Fatalf("AggregateRecords cursor scan: %v", err)
	}
	if aggregate.Scanned != 505 || len(aggregate.Rows) != 1 || aggregate.Rows[0]["records"] != 505 {
		t.Fatalf("aggregate scan should keep same-created_at records across pages: %#v", aggregate)
	}

	exported, err := svc.queryRecordsForExport(context.Background(), p, ds.ID, QueryRecordsInput{Limit: 505}, 5000)
	if err != nil {
		t.Fatalf("queryRecordsForExport: %v", err)
	}
	if len(exported) != 505 || exported[0].ID != "rec_504" || exported[len(exported)-1].ID != "rec_000" {
		t.Fatalf("export pagination should include all same-created_at records in order: %#v", exported)
	}

	quality, err := svc.RunQualityCheck(context.Background(), p, ds.ID, RunQualityCheckInput{Limit: 505})
	if err != nil {
		t.Fatalf("RunQualityCheck cursor scan: %v", err)
	}
	if quality.Scanned != 505 {
		t.Fatalf("quality scan should keep same-created_at records across pages: %#v", quality)
	}
}
func TestRecordTimelineCursorKeepsCreatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "ops", Name: "timeline", Title: "Timeline"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	createdAt := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	recordID := "rec_1"

	if _, err := store.AppendRecordRevision(context.Background(), RecordRevision{ID: "tl_001", TenantID: p.TenantID, DatasetID: ds.ID, RecordID: recordID, Action: "update", Data: map[string]any{"id": "tl_001"}, CreatedBy: p.UserID, CreatedAt: createdAt}); err != nil {
		t.Fatalf("AppendRecordRevision: %v", err)
	}
	if _, err := store.AppendAuditLog(context.Background(), AuditLog{ID: "tl_002", TenantID: p.TenantID, UserID: p.UserID, Action: "record.update", DatasetID: ds.ID, TargetType: "record", TargetID: recordID, Summary: "Updated record", Metadata: map[string]any{"id": "tl_002"}, CreatedAt: createdAt}); err != nil {
		t.Fatalf("AppendAuditLog: %v", err)
	}
	if _, err := store.AppendDataEventLog(context.Background(), DataEventLog{ID: "tl_003", TenantID: p.TenantID, Source: "crm", EventType: "record.updated", Operation: "update", DatasetID: ds.ID, RecordID: recordID, IdempotencyKey: "tl_003", ResultStatus: "applied", CreatedBy: p.UserID, AppliedAt: createdAt}); err != nil {
		t.Fatalf("AppendDataEventLog: %v", err)
	}
	if _, err := store.CreateRecordApproval(context.Background(), RecordApproval{ID: "tl_004", TenantID: p.TenantID, DatasetID: ds.ID, RecordID: recordID, Status: "pending", Kind: "timeline", Request: map[string]any{"id": "tl_004"}, CreatedBy: p.UserID, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
		t.Fatalf("CreateRecordApproval: %v", err)
	}

	firstPage, err := svc.GetRecordTimeline(context.Background(), p, ds.ID, recordID, QueryRecordTimelineInput{Limit: 2})
	if err != nil {
		t.Fatalf("GetRecordTimeline first page: %v", err)
	}
	if !firstPage.HasMore || firstPage.NextBefore != createdAt.Format(time.RFC3339Nano) || firstPage.NextBeforeID != "tl_003" || len(firstPage.Items) != 2 || firstPage.Items[0].ID != "tl_004" || firstPage.Items[1].ID != "tl_003" {
		t.Fatalf("unexpected timeline first page: %#v", firstPage)
	}
	secondPage, err := svc.GetRecordTimeline(context.Background(), p, ds.ID, recordID, QueryRecordTimelineInput{Limit: 2, Before: firstPage.NextBefore, BeforeID: firstPage.NextBeforeID})
	if err != nil {
		t.Fatalf("GetRecordTimeline second page: %v", err)
	}
	if secondPage.HasMore || secondPage.NextBefore != "" || secondPage.NextBeforeID != "" || len(secondPage.Items) != 2 || secondPage.Items[0].ID != "tl_002" || secondPage.Items[1].ID != "tl_001" {
		t.Fatalf("timeline cursor should continue through created_at ties: %#v", secondPage)
	}
}
func TestSQLiteStoreRevisionCursorKeepsCreatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "ops", Name: "revisions", Title: "Revisions"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	createdAt := time.Date(2026, 5, 6, 9, 5, 0, 0, time.UTC)

	for _, id := range []string{"revision_001", "revision_002", "revision_003"} {
		if _, err := store.AppendRecordRevision(context.Background(), RecordRevision{ID: id, TenantID: p.TenantID, DatasetID: ds.ID, RecordID: "rec_1", Action: "update", Title: id, Data: map[string]any{"id": id}, CreatedBy: p.UserID, CreatedAt: createdAt}); err != nil {
			t.Fatalf("AppendRecordRevision %s: %v", id, err)
		}
	}

	firstPage, err := svc.QueryRecordRevisions(context.Background(), p, ds.ID, "rec_1", QueryRecordRevisionsInput{Limit: 2})
	if err != nil {
		t.Fatalf("QueryRecordRevisions first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "revision_003" || firstPage[1].ID != "revision_002" {
		t.Fatalf("unexpected revision first page: %#v", firstPage)
	}
	cursor := recordRevisionListResponse(firstPage, 2)
	if cursor.NextBefore != createdAt.Format(time.RFC3339Nano) || cursor.NextBeforeID == "" || cursor.NextBeforeID == "revision_002" {
		t.Fatalf("unexpected revision cursor: %#v", cursor)
	}

	secondPage, err := svc.QueryRecordRevisions(context.Background(), p, ds.ID, "rec_1", QueryRecordRevisionsInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("QueryRecordRevisions second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "revision_001" {
		t.Fatalf("revision cursor should continue through created_at ties: %#v", secondPage)
	}
}
func TestSQLiteStoreAuditCursorKeepsCreatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	createdAt := time.Date(2026, 5, 6, 9, 15, 0, 0, time.UTC)

	for _, id := range []string{"audit_001", "audit_002", "audit_003"} {
		if _, err := store.AppendAuditLog(context.Background(), AuditLog{ID: id, TenantID: p.TenantID, UserID: p.UserID, Action: "record.create", DatasetID: "ops.cursor", TargetType: "record", TargetID: id, Summary: "Created record", Metadata: map[string]any{"id": id}, CreatedAt: createdAt}); err != nil {
			t.Fatalf("AppendAuditLog %s: %v", id, err)
		}
	}

	firstPage, err := svc.QueryAuditLogs(context.Background(), p, QueryAuditLogsInput{Limit: 2})
	if err != nil {
		t.Fatalf("QueryAuditLogs first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "audit_003" || firstPage[1].ID != "audit_002" {
		t.Fatalf("unexpected audit first page: %#v", firstPage)
	}
	cursor := auditLogListResponse(firstPage, 2)
	if cursor.NextBefore != createdAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "audit_002" {
		t.Fatalf("unexpected audit cursor: %#v", cursor)
	}

	secondPage, err := svc.QueryAuditLogs(context.Background(), p, QueryAuditLogsInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("QueryAuditLogs second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "audit_001" {
		t.Fatalf("audit cursor should continue through created_at ties: %#v", secondPage)
	}
}
func TestSQLiteStoreEventCursorsKeepTimestampTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	occurredAt := time.Date(2026, 5, 6, 9, 45, 0, 0, time.UTC)

	for _, id := range []string{"event_001", "event_002", "event_003"} {
		if _, err := store.AppendDataEventLog(context.Background(), DataEventLog{ID: id, TenantID: p.TenantID, Source: "crm", EventType: "sales.order.updated", Operation: "upsert", BusinessAction: "sales.order_upsert", DatasetID: "sales.orders", RecordID: id, IdempotencyKey: id, ResultStatus: "applied", CreatedBy: p.UserID, AppliedAt: occurredAt}); err != nil {
			t.Fatalf("AppendDataEventLog %s: %v", id, err)
		}
		if _, err := store.CreateDataEventDeadLetter(context.Background(), DataEventDeadLetter{ID: "dead_" + id[len("event_"):], TenantID: p.TenantID, Status: "open", Source: "crm", EventType: "sales.order.updated", BusinessAction: "sales.order_upsert", DatasetID: "sales.orders", RecordID: id, IdempotencyKey: "dead_" + id, Error: "validation failed", Payload: map[string]any{"id": id}, CreatedBy: p.UserID, CreatedAt: occurredAt, UpdatedAt: occurredAt}); err != nil {
			t.Fatalf("CreateDataEventDeadLetter %s: %v", id, err)
		}
	}

	firstEvents, err := svc.QueryDataEvents(context.Background(), p, QueryDataEventsInput{Limit: 2})
	if err != nil {
		t.Fatalf("QueryDataEvents first page: %v", err)
	}
	if len(firstEvents) != 2 || firstEvents[0].ID != "event_003" || firstEvents[1].ID != "event_002" {
		t.Fatalf("unexpected event first page: %#v", firstEvents)
	}
	eventCursor := dataEventListResponse(firstEvents, 2)
	if eventCursor.NextBefore != occurredAt.Format(time.RFC3339Nano) || eventCursor.NextBeforeID != "event_002" {
		t.Fatalf("unexpected event cursor: %#v", eventCursor)
	}
	secondEvents, err := svc.QueryDataEvents(context.Background(), p, QueryDataEventsInput{Limit: 2, Before: eventCursor.NextBefore, BeforeID: eventCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("QueryDataEvents second page: %v", err)
	}
	if len(secondEvents) != 1 || secondEvents[0].ID != "event_001" {
		t.Fatalf("event cursor should continue through timestamp ties: %#v", secondEvents)
	}

	firstDeadLetters, err := svc.QueryDataEventDeadLetters(context.Background(), p, QueryDataEventDeadLettersInput{Limit: 2})
	if err != nil {
		t.Fatalf("QueryDataEventDeadLetters first page: %v", err)
	}
	if len(firstDeadLetters) != 2 || firstDeadLetters[0].ID != "dead_003" || firstDeadLetters[1].ID != "dead_002" {
		t.Fatalf("unexpected dead-letter first page: %#v", firstDeadLetters)
	}
	deadLetterCursor := dataEventDeadLetterListResponse(firstDeadLetters, 2)
	if deadLetterCursor.NextBefore != occurredAt.Format(time.RFC3339Nano) || deadLetterCursor.NextBeforeID != "dead_002" {
		t.Fatalf("unexpected dead-letter cursor: %#v", deadLetterCursor)
	}
	secondDeadLetters, err := svc.QueryDataEventDeadLetters(context.Background(), p, QueryDataEventDeadLettersInput{Limit: 2, Before: deadLetterCursor.NextBefore, BeforeID: deadLetterCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("QueryDataEventDeadLetters second page: %v", err)
	}
	if len(secondDeadLetters) != 1 || secondDeadLetters[0].ID != "dead_001" {
		t.Fatalf("dead-letter cursor should continue through timestamp ties: %#v", secondDeadLetters)
	}
}
func TestSQLiteStoreWorkQueueCursorsKeepCreatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	createdAt := time.Date(2026, 5, 6, 10, 5, 0, 0, time.UTC)
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "sales", Name: "orders", Title: "Orders"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	for _, suffix := range []string{"001", "002", "003"} {
		if _, err := store.UpsertImportJob(context.Background(), ImportJob{ID: "import_" + suffix, TenantID: p.TenantID, DatasetID: "sales.orders", Kind: "csv", Status: "completed", Total: 1, Imported: 1, Valid: true, CreatedBy: p.UserID, CreatedAt: createdAt}); err != nil {
			t.Fatalf("UpsertImportJob %s: %v", suffix, err)
		}
		if _, err := store.UpsertExportJob(context.Background(), ExportJob{ID: "export_" + suffix, TenantID: p.TenantID, DatasetID: "sales.orders", Format: "csv", Status: "completed", Total: 1, Bytes: 12, CreatedBy: p.UserID, CreatedAt: createdAt}); err != nil {
			t.Fatalf("UpsertExportJob %s: %v", suffix, err)
		}
		if _, err := store.CreateOperationPlan(context.Background(), OperationPlan{ID: "plan_" + suffix, TenantID: p.TenantID, DatasetID: "sales.orders", Operation: "bulk_update_records", Status: "pending", Summary: "Plan", RiskLevel: "medium", Request: map[string]any{"id": suffix}, Preview: map[string]any{"matched": 1}, CreatedBy: p.UserID, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
			t.Fatalf("CreateOperationPlan %s: %v", suffix, err)
		}
		if _, err := store.CreateRecordApproval(context.Background(), RecordApproval{ID: "approval_" + suffix, TenantID: p.TenantID, DatasetID: ds.ID, RecordID: "SO-" + suffix, Status: "pending", Kind: "sales_order", Priority: "normal", Summary: "Approve", Request: map[string]any{"id": suffix}, CreatedBy: p.UserID, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
			t.Fatalf("CreateRecordApproval %s: %v", suffix, err)
		}
		if _, err := store.AppendQualityRun(context.Background(), QualityCheckResult{ID: "quality_" + suffix, TenantID: p.TenantID, DatasetID: ds.ID, Checks: []string{"schema_validation"}, Scanned: 1, Valid: suffix != "003", Limit: 1, CreatedBy: p.UserID, CreatedAt: createdAt}); err != nil {
			t.Fatalf("AppendQualityRun %s: %v", suffix, err)
		}
	}

	imports, err := svc.ListImportJobs(context.Background(), p, QueryImportJobsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListImportJobs first page: %v", err)
	}
	if len(imports) != 2 || imports[0].ID != "import_003" || imports[1].ID != "import_002" {
		t.Fatalf("unexpected import jobs first page: %#v", imports)
	}
	importCursor := importJobListResponse(imports, 2)
	imports, err = svc.ListImportJobs(context.Background(), p, QueryImportJobsInput{Limit: 2, Before: importCursor.NextBefore, BeforeID: importCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListImportJobs second page: %v", err)
	}
	if len(imports) != 1 || imports[0].ID != "import_001" {
		t.Fatalf("import job cursor should continue through created_at ties: %#v", imports)
	}

	exports, err := svc.ListExportJobs(context.Background(), p, QueryExportJobsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListExportJobs first page: %v", err)
	}
	if len(exports) != 2 || exports[0].ID != "export_003" || exports[1].ID != "export_002" {
		t.Fatalf("unexpected export jobs first page: %#v", exports)
	}
	exportCursor := exportJobListResponse(exports, 2)
	exports, err = svc.ListExportJobs(context.Background(), p, QueryExportJobsInput{Limit: 2, Before: exportCursor.NextBefore, BeforeID: exportCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListExportJobs second page: %v", err)
	}
	if len(exports) != 1 || exports[0].ID != "export_001" {
		t.Fatalf("export job cursor should continue through created_at ties: %#v", exports)
	}

	plans, err := svc.ListOperationPlans(context.Background(), p, QueryOperationPlansInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListOperationPlans first page: %v", err)
	}
	if len(plans) != 2 || plans[0].ID != "plan_003" || plans[1].ID != "plan_002" {
		t.Fatalf("unexpected operation plans first page: %#v", plans)
	}
	planCursor := operationPlanListResponse(plans, 2)
	plans, err = svc.ListOperationPlans(context.Background(), p, QueryOperationPlansInput{Limit: 2, Before: planCursor.NextBefore, BeforeID: planCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListOperationPlans second page: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != "plan_001" {
		t.Fatalf("operation plan cursor should continue through created_at ties: %#v", plans)
	}

	approvals, err := svc.ListRecordApprovals(context.Background(), p, QueryRecordApprovalsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListRecordApprovals first page: %v", err)
	}
	if len(approvals) != 2 || approvals[0].ID != "approval_003" || approvals[1].ID != "approval_002" {
		t.Fatalf("unexpected approvals first page: %#v", approvals)
	}
	approvalCursor := recordApprovalListResponse(approvals, 2)
	approvals, err = svc.ListRecordApprovals(context.Background(), p, QueryRecordApprovalsInput{Limit: 2, Before: approvalCursor.NextBefore, BeforeID: approvalCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListRecordApprovals second page: %v", err)
	}
	if len(approvals) != 1 || approvals[0].ID != "approval_001" {
		t.Fatalf("approval cursor should continue through created_at ties: %#v", approvals)
	}

	inbox, err := svc.MISInbox(context.Background(), p, QueryMISInboxInput{Type: "approval", Limit: 2})
	if err != nil {
		t.Fatalf("MISInbox first page: %v", err)
	}
	if !inbox.HasMore || inbox.NextBefore != createdAt.Format(time.RFC3339Nano) || inbox.NextBeforeID != "approval_002" || len(inbox.Items) != 2 || inbox.Items[0].ID != "approval_003" || inbox.Items[1].ID != "approval_002" {
		t.Fatalf("unexpected inbox first page: %#v", inbox)
	}
	inbox, err = svc.MISInbox(context.Background(), p, QueryMISInboxInput{Type: "approval", Limit: 2, Before: inbox.NextBefore, BeforeID: inbox.NextBeforeID})
	if err != nil {
		t.Fatalf("MISInbox second page: %v", err)
	}
	if inbox.HasMore || inbox.NextBefore != "" || inbox.NextBeforeID != "" || len(inbox.Items) != 1 || inbox.Items[0].ID != "approval_001" {
		t.Fatalf("inbox cursor should continue through created_at ties: %#v", inbox)
	}

	qualityRuns, err := svc.ListQualityRuns(context.Background(), p, ds.ID, QueryQualityRunsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListQualityRuns first page: %v", err)
	}
	if len(qualityRuns) != 2 || qualityRuns[0].ID != "quality_003" || qualityRuns[1].ID != "quality_002" {
		t.Fatalf("unexpected quality runs first page: %#v", qualityRuns)
	}
	qualityCursor := qualityRunListResponse(qualityRuns, 2)
	qualityRuns, err = svc.ListQualityRuns(context.Background(), p, ds.ID, QueryQualityRunsInput{Limit: 2, Before: qualityCursor.NextBefore, BeforeID: qualityCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListQualityRuns second page: %v", err)
	}
	if len(qualityRuns) != 1 || qualityRuns[0].ID != "quality_001" {
		t.Fatalf("quality run cursor should continue through created_at ties: %#v", qualityRuns)
	}
}
func TestSQLiteStoreSchemaProposalCursorKeepsCreatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	createdAt := time.Date(2026, 5, 6, 10, 10, 0, 0, time.UTC)
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "sales", Name: "proposal_orders", Title: "Proposal Orders"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	for _, suffix := range []string{"001", "002", "003"} {
		if _, err := store.CreateSchemaProposal(context.Background(), SchemaProposal{ID: "schema_proposal_" + suffix, TenantID: p.TenantID, DatasetID: ds.ID, Reason: "cursor fixture", Suggested: []FieldDefinition{{Key: "field_" + suffix, Type: "string"}}, Ignored: []string{}, Impact: map[string]interface{}{"suggested_count": 1}, Status: "pending", CreatedBy: p.UserID, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
			t.Fatalf("CreateSchemaProposal %s: %v", suffix, err)
		}
	}

	firstPage, err := svc.ListSchemaProposals(context.Background(), p, ds.ID, ListSchemaProposalsInput{Status: "pending", Limit: 2})
	if err != nil {
		t.Fatalf("ListSchemaProposals first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "schema_proposal_003" || firstPage[1].ID != "schema_proposal_002" {
		t.Fatalf("unexpected schema proposal first page: %#v", firstPage)
	}
	cursor := schemaProposalListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBefore != createdAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "schema_proposal_002" {
		t.Fatalf("unexpected schema proposal cursor: %#v", cursor)
	}

	secondPage, err := svc.ListSchemaProposals(context.Background(), p, ds.ID, ListSchemaProposalsInput{Status: "pending", Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListSchemaProposals second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "schema_proposal_001" {
		t.Fatalf("schema proposal cursor should continue through created_at ties: %#v", secondPage)
	}
}
func TestDashboardAndReportCursorsUseIDTiebreaker(t *testing.T) {
	dashboards := []DashboardDefinition{{ID: "dashboard.alpha"}, {ID: "dashboard.gamma"}, {ID: "dashboard.beta"}}
	dashboardPage := paginateDashboards(dashboards, QueryDashboardsInput{Limit: 3})
	dashboardCursor := dashboardListResponse(dashboardPage, 2)
	if len(dashboardCursor.Items) != 2 || dashboardCursor.Items[0].ID != "dashboard.gamma" || dashboardCursor.Items[1].ID != "dashboard.beta" || !dashboardCursor.HasMore {
		t.Fatalf("unexpected dashboard first page: %#v", dashboardPage)
	}
	dashboardPage = paginateDashboards(dashboards, QueryDashboardsInput{Limit: 2, BeforeID: dashboardCursor.NextBeforeID})
	if len(dashboardPage) != 1 || dashboardPage[0].ID != "dashboard.alpha" {
		t.Fatalf("dashboard cursor should continue by descending id: %#v", dashboardPage)
	}

	reports := []ReportDefinition{{ID: "report.alpha"}, {ID: "report.gamma"}, {ID: "report.beta"}}
	reportPage := paginateReports(reports, QueryReportsInput{Limit: 3})
	reportCursor := reportListResponse(reportPage, 2)
	if len(reportCursor.Items) != 2 || reportCursor.Items[0].ID != "report.gamma" || reportCursor.Items[1].ID != "report.beta" || !reportCursor.HasMore {
		t.Fatalf("unexpected report first page: %#v", reportPage)
	}
	reportPage = paginateReports(reports, QueryReportsInput{Limit: 2, BeforeID: reportCursor.NextBeforeID})
	if len(reportPage) != 1 || reportPage[0].ID != "report.alpha" {
		t.Fatalf("report cursor should continue by descending id: %#v", reportPage)
	}
}
func TestCatalogAndAccessPresetCursorsUseIDTiebreaker(t *testing.T) {
	domains := []BusinessDomainCatalog{{Domain: "alpha"}, {Domain: "gamma"}, {Domain: "beta"}}
	domainPage := paginateBusinessDomains(domains, QueryBusinessDomainsInput{Limit: 3})
	domainCursor := businessDomainListResponse(domainPage, 2)
	if len(domainCursor.Items) != 2 || domainCursor.Items[0].Domain != "gamma" || domainCursor.Items[1].Domain != "beta" || !domainCursor.HasMore {
		t.Fatalf("unexpected domain first page: %#v", domainPage)
	}
	domainPage = paginateBusinessDomains(domains, QueryBusinessDomainsInput{Limit: 2, BeforeID: domainCursor.NextBeforeID})
	if len(domainPage) != 1 || domainPage[0].Domain != "alpha" {
		t.Fatalf("domain cursor should continue by descending id: %#v", domainPage)
	}

	presets := []AccessPolicyPreset{{ID: "preset.alpha"}, {ID: "preset.gamma"}, {ID: "preset.beta"}}
	presetPage := paginateAccessPolicyPresets(presets, QueryAccessPolicyPresetsInput{Limit: 2})
	if len(presetPage) != 2 || presetPage[0].ID != "preset.gamma" || presetPage[1].ID != "preset.beta" {
		t.Fatalf("unexpected access preset first page: %#v", presetPage)
	}
	presetCursor := accessPolicyPresetListResponse(presetPage, 2)
	presetPage = paginateAccessPolicyPresets(presets, QueryAccessPolicyPresetsInput{Limit: 2, BeforeID: presetCursor.NextBeforeID})
	if len(presetPage) != 1 || presetPage[0].ID != "preset.alpha" {
		t.Fatalf("access preset cursor should continue by descending id: %#v", presetPage)
	}
}

func TestStaticDefinitionCursorsUseIDTiebreaker(t *testing.T) {
	templates := []DatasetTemplate{{ID: "template.alpha"}, {ID: "template.gamma"}, {ID: "template.beta"}}
	templatePage := paginateTemplates(templates, QueryTemplatesInput{Limit: 3})
	templateCursor := templateListResponse(templatePage, 2)
	if len(templateCursor.Items) != 2 || templateCursor.Items[0].ID != "template.gamma" || templateCursor.Items[1].ID != "template.beta" || !templateCursor.HasMore {
		t.Fatalf("unexpected template first page: %#v", templatePage)
	}
	templatePage = paginateTemplates(templates, QueryTemplatesInput{Limit: 2, BeforeID: templateCursor.NextBeforeID})
	if len(templatePage) != 1 || templatePage[0].ID != "template.alpha" {
		t.Fatalf("template cursor should continue by descending id: %#v", templatePage)
	}

	views := []BusinessViewDefinition{{ID: "view.alpha"}, {ID: "view.gamma"}, {ID: "view.beta"}}
	viewPage := paginateBusinessViews(views, QueryBusinessViewsInput{Limit: 3})
	viewCursor := businessViewListResponse(viewPage, 2)
	if len(viewCursor.Items) != 2 || viewCursor.Items[0].ID != "view.gamma" || viewCursor.Items[1].ID != "view.beta" || !viewCursor.HasMore {
		t.Fatalf("unexpected business view first page: %#v", viewPage)
	}
	viewPage = paginateBusinessViews(views, QueryBusinessViewsInput{Limit: 2, BeforeID: viewCursor.NextBeforeID})
	if len(viewPage) != 1 || viewPage[0].ID != "view.alpha" {
		t.Fatalf("business view cursor should continue by descending id: %#v", viewPage)
	}

	checks := []QualityCheckDefinition{{ID: "check.alpha"}, {ID: "check.gamma"}, {ID: "check.beta"}}
	checkPage := paginateQualityChecks(checks, QueryQualityChecksInput{Limit: 2})
	if len(checkPage) != 2 || checkPage[0].ID != "check.gamma" || checkPage[1].ID != "check.beta" {
		t.Fatalf("unexpected quality check first page: %#v", checkPage)
	}
	checkCursor := qualityCheckListResponse(checkPage, 2)
	checkPage = paginateQualityChecks(checks, QueryQualityChecksInput{Limit: 2, BeforeID: checkCursor.NextBeforeID})
	if len(checkPage) != 1 || checkPage[0].ID != "check.alpha" {
		t.Fatalf("quality check cursor should continue by descending id: %#v", checkPage)
	}
}
func TestEventContractCursorUsesIDTiebreaker(t *testing.T) {
	items := []EventContract{{ID: "contract.alpha"}, {ID: "contract.gamma"}, {ID: "contract.beta"}}

	firstPage := paginateEventContracts(items, QueryEventContractsInput{Limit: 2})
	if len(firstPage) != 2 || firstPage[0].ID != "contract.gamma" || firstPage[1].ID != "contract.beta" {
		t.Fatalf("unexpected event contract first page: %#v", firstPage)
	}
	cursor := eventContractListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBeforeID != "contract.beta" {
		t.Fatalf("unexpected event contract cursor: %#v", cursor)
	}

	secondPage := paginateEventContracts(items, QueryEventContractsInput{Limit: 2, BeforeID: cursor.NextBeforeID})
	if len(secondPage) != 1 || secondPage[0].ID != "contract.alpha" {
		t.Fatalf("event contract cursor should continue by descending id: %#v", secondPage)
	}
}
func TestBusinessActionCursorUsesIDTiebreaker(t *testing.T) {
	items := []BusinessAction{{ID: "action.alpha"}, {ID: "action.gamma"}, {ID: "action.beta"}}

	firstPage := paginateBusinessActions(items, QueryBusinessActionsInput{Limit: 3})
	cursor := businessActionListResponse(firstPage, 2)
	if len(cursor.Items) != 2 || cursor.Items[0].ID != "action.gamma" || cursor.Items[1].ID != "action.beta" || !cursor.HasMore {
		t.Fatalf("unexpected business action first page: %#v", firstPage)
	}
	if !cursor.HasMore || cursor.NextBeforeID != "action.beta" {
		t.Fatalf("unexpected business action cursor: %#v", cursor)
	}

	secondPage := paginateBusinessActions(items, QueryBusinessActionsInput{Limit: 2, BeforeID: cursor.NextBeforeID})
	if len(secondPage) != 1 || secondPage[0].ID != "action.alpha" {
		t.Fatalf("business action cursor should continue by descending id: %#v", secondPage)
	}
}
func TestFieldCursorKeepsUpdatedAtTies(t *testing.T) {
	updatedAt := time.Date(2026, 5, 6, 10, 40, 0, 0, time.UTC)
	items := []FieldDefinition{
		{ID: "field_001", Key: "a", UpdatedAt: updatedAt},
		{ID: "field_003", Key: "c", UpdatedAt: updatedAt},
		{ID: "field_002", Key: "b", UpdatedAt: updatedAt},
	}

	firstPage := paginateFields(items, QueryFieldsInput{Limit: 2})
	if len(firstPage) != 2 || firstPage[0].ID != "field_003" || firstPage[1].ID != "field_002" {
		t.Fatalf("unexpected field first page: %#v", firstPage)
	}
	cursor := fieldListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBefore != updatedAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "field_002" {
		t.Fatalf("unexpected field cursor: %#v", cursor)
	}

	secondPage := paginateFields(items, QueryFieldsInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if len(secondPage) != 1 || secondPage[0].ID != "field_001" {
		t.Fatalf("field cursor should continue through updated_at ties: %#v", secondPage)
	}
}
func TestRelationshipCursorUsesCompositeKey(t *testing.T) {
	items := []DatasetRelationship{
		{SourceDatasetID: "sales.orders", SourceField: "customer_ref", TargetDatasetID: "sales.customers", FieldType: "record_ref"},
		{SourceDatasetID: "sales.orders", SourceField: "owner_ref", TargetDatasetID: "hr.employees", FieldType: "person_ref"},
		{SourceDatasetID: "sales.opportunities", SourceField: "account_ref", TargetDatasetID: "sales.accounts", FieldType: "org_ref"},
	}

	firstPage := paginateRelationships(items, QueryRelationshipsInput{Limit: 2})
	if len(firstPage) != 2 || relationshipCursorKey(firstPage[0]) <= relationshipCursorKey(firstPage[1]) {
		t.Fatalf("unexpected relationship first page ordering: %#v", firstPage)
	}
	cursor := relationshipListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBeforeID != relationshipCursorKey(firstPage[1]) {
		t.Fatalf("unexpected relationship cursor: %#v", cursor)
	}

	secondPage := paginateRelationships(items, QueryRelationshipsInput{Limit: 2, BeforeID: cursor.NextBeforeID})
	if len(secondPage) != 1 || relationshipCursorKey(secondPage[0]) >= cursor.NextBeforeID {
		t.Fatalf("relationship cursor should continue by descending composite key: cursor=%q page=%#v", cursor.NextBeforeID, secondPage)
	}
}
func TestBusinessRuleCursorUsesIDTiebreaker(t *testing.T) {
	svc := NewService(nil, "test")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}

	firstPage, err := svc.ListBusinessRules(context.Background(), p, QueryBusinessRulesInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListBusinessRules first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID <= firstPage[1].ID {
		t.Fatalf("unexpected business rules first page ordering: %#v", firstPage)
	}
	cursor := businessRuleListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBeforeID != firstPage[1].ID {
		t.Fatalf("unexpected business rule cursor: %#v", cursor)
	}

	secondPage, err := svc.ListBusinessRules(context.Background(), p, QueryBusinessRulesInput{Limit: 2, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListBusinessRules second page: %v", err)
	}
	if len(secondPage) == 0 || secondPage[0].ID >= cursor.NextBeforeID {
		t.Fatalf("business rule cursor should continue by descending id: cursor=%q page=%#v", cursor.NextBeforeID, secondPage)
	}
}
func TestServiceDatasetCursorKeepsUpdatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	updatedAt := time.Date(2026, 5, 6, 10, 35, 0, 0, time.UTC)

	for _, suffix := range []string{"001", "002", "003"} {
		if _, err := store.CreateDataset(context.Background(), Dataset{ID: "ops.dataset_" + suffix, TenantID: p.TenantID, Domain: "ops", Name: "dataset_" + suffix, Title: "Dataset " + suffix, SchemaVersion: 1, CreatedAt: updatedAt, UpdatedAt: updatedAt}); err != nil {
			t.Fatalf("CreateDataset %s: %v", suffix, err)
		}
	}

	firstPage, err := svc.ListDatasets(context.Background(), p, QueryDatasetsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListDatasets first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "ops.dataset_003" || firstPage[1].ID != "ops.dataset_002" {
		t.Fatalf("unexpected datasets first page: %#v", firstPage)
	}
	cursor := datasetListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBefore != updatedAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "ops.dataset_002" {
		t.Fatalf("unexpected dataset cursor: %#v", cursor)
	}

	secondPage, err := svc.ListDatasets(context.Background(), p, QueryDatasetsInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListDatasets second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "ops.dataset_001" {
		t.Fatalf("dataset cursor should continue through updated_at ties: %#v", secondPage)
	}
}
func TestSQLiteStoreAPIKeyPolicyCursorKeepsUpdatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "admin_1", Role: "data_admin"}
	updatedAt := time.Date(2026, 5, 6, 10, 30, 0, 0, time.UTC)

	for _, suffix := range []string{"001", "002", "003"} {
		record := APIKeyPolicyRecord{ID: "key_" + suffix, TenantID: p.TenantID, UserID: "user_" + suffix, Role: "data_user", KeyPrefix: "mcds_" + suffix, Enabled: true, CreatedBy: p.UserID, CreatedAt: updatedAt, UpdatedAt: updatedAt}
		if _, err := store.CreateAPIKeyPolicy(context.Background(), record, apiKeyHash("secret-"+suffix+"-012345678901234567890123")); err != nil {
			t.Fatalf("CreateAPIKeyPolicy %s: %v", suffix, err)
		}
	}

	firstPage, err := svc.ListAPIKeyPolicies(context.Background(), p, QueryAPIKeyPoliciesInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListAPIKeyPolicies first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "key_003" || firstPage[1].ID != "key_002" {
		t.Fatalf("unexpected api key policy first page: %#v", firstPage)
	}
	cursor := apiKeyPolicyListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBefore != updatedAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "key_002" {
		t.Fatalf("unexpected api key policy cursor: %#v", cursor)
	}

	secondPage, err := svc.ListAPIKeyPolicies(context.Background(), p, QueryAPIKeyPoliciesInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListAPIKeyPolicies second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "key_001" {
		t.Fatalf("api key policy cursor should continue through updated_at ties: %#v", secondPage)
	}
}
func TestSQLiteStoreExternalConnectorCursorKeepsUpdatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	updatedAt := time.Date(2026, 5, 6, 10, 25, 0, 0, time.UTC)

	for _, suffix := range []string{"001", "002", "003"} {
		if _, err := store.UpsertExternalConnector(context.Background(), ExternalConnector{ID: "connector_" + suffix, TenantID: p.TenantID, Domain: "sales", Name: "Connector " + suffix, Kind: "crm", Enabled: true, CreatedBy: p.UserID, CreatedAt: updatedAt, UpdatedAt: updatedAt}); err != nil {
			t.Fatalf("UpsertExternalConnector %s: %v", suffix, err)
		}
	}

	firstPage, err := svc.ListExternalConnectors(context.Background(), p, QueryExternalConnectorsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListExternalConnectors first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "connector_003" || firstPage[1].ID != "connector_002" {
		t.Fatalf("unexpected connector first page: %#v", firstPage)
	}
	cursor := externalConnectorListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBefore != updatedAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "connector_002" {
		t.Fatalf("unexpected connector cursor: %#v", cursor)
	}

	secondPage, err := svc.ListExternalConnectors(context.Background(), p, QueryExternalConnectorsInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListExternalConnectors second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "connector_001" {
		t.Fatalf("connector cursor should continue through updated_at ties: %#v", secondPage)
	}

	health, err := svc.ListConnectorHealth(context.Background(), p, QueryExternalConnectorsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListConnectorHealth first page: %v", err)
	}
	healthCursor := connectorHealthListResponse(health, 2)
	health, err = svc.ListConnectorHealth(context.Background(), p, QueryExternalConnectorsInput{Limit: 2, Before: healthCursor.NextBefore, BeforeID: healthCursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListConnectorHealth second page: %v", err)
	}
	if len(health) != 1 || health[0].Connector.ID != "connector_001" {
		t.Fatalf("connector health cursor should continue through updated_at ties: %#v", health)
	}
}
func TestConnectorSyncRunCursorKeepsFinishedAtTies(t *testing.T) {
	finishedAt := time.Date(2026, 5, 6, 10, 20, 0, 0, time.UTC)
	runs := []ConnectorSyncRun{
		{ID: "sync_run_001", Status: "success", FinishedAt: finishedAt},
		{ID: "sync_run_003", Status: "success", FinishedAt: finishedAt},
		{ID: "sync_run_002", Status: "failed", FinishedAt: finishedAt},
	}

	firstPage := paginateConnectorSyncRuns(runs, QueryConnectorSyncRunsInput{Limit: 2})
	if len(firstPage) != 2 || firstPage[0].ID != "sync_run_003" || firstPage[1].ID != "sync_run_002" {
		t.Fatalf("unexpected connector sync run first page: %#v", firstPage)
	}
	cursor := connectorSyncRunListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBefore != finishedAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "sync_run_002" {
		t.Fatalf("unexpected connector sync run cursor: %#v", cursor)
	}

	secondPage := paginateConnectorSyncRuns(runs, QueryConnectorSyncRunsInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if len(secondPage) != 1 || secondPage[0].ID != "sync_run_001" {
		t.Fatalf("connector sync run cursor should continue through finished_at ties: %#v", secondPage)
	}
}
func TestRecordPageCursorIncludesIDOnlyForDefaultSort(t *testing.T) {
	createdAt := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	records := []Record{{ID: "rec_1", CreatedAt: createdAt}, {ID: "rec_2", CreatedAt: createdAt.Add(time.Minute)}}

	nextBefore, nextBeforeID := recordPageCursor(records, nil)
	if nextBefore != records[1].CreatedAt.Format(time.RFC3339Nano) || nextBeforeID != "rec_2" {
		t.Fatalf("default cursor should include timestamp and id: before=%q before_id=%q", nextBefore, nextBeforeID)
	}

	nextBefore, nextBeforeID = recordPageCursor(records, []SortSpec{{Field: "amount", Direction: "desc"}})
	if nextBefore != records[1].CreatedAt.Format(time.RFC3339Nano) || nextBeforeID != "" {
		t.Fatalf("custom sort cursor should omit before_id: before=%q before_id=%q", nextBefore, nextBeforeID)
	}
}
func TestBusinessViewResultIncludesPaginationCursor(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	if _, err := svc.CreateDatasetFromTemplate(context.Background(), p, "sales.orders", CreateFromTemplateInput{}); err != nil {
		t.Fatalf("CreateDatasetFromTemplate: %v", err)
	}

	base := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	records := []Record{
		{ID: "SO-VIEW-1", TenantID: p.TenantID, DatasetID: "sales.orders", Title: "View 1", Data: map[string]any{"order_no": "SO-VIEW-1", "customer": "CursorCo", "order_date": "2026-05-06", "amount": 100}, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)},
		{ID: "SO-VIEW-2", TenantID: p.TenantID, DatasetID: "sales.orders", Title: "View 2", Data: map[string]any{"order_no": "SO-VIEW-2", "customer": "CursorCo", "order_date": "2026-05-06", "amount": 200}, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "SO-VIEW-3", TenantID: p.TenantID, DatasetID: "sales.orders", Title: "View 3", Data: map[string]any{"order_no": "SO-VIEW-3", "customer": "CursorCo", "order_date": "2026-05-06", "amount": 300}, CreatedAt: base.Add(3 * time.Minute), UpdatedAt: base.Add(3 * time.Minute)},
	}
	if _, err := store.ImportRecords(context.Background(), records); err != nil {
		t.Fatalf("ImportRecords: %v", err)
	}

	first, err := svc.QueryBusinessView(context.Background(), p, "sales.order_overview", QueryBusinessViewInput{Filter: map[string]any{"field": "customer", "op": "eq", "value": "CursorCo"}, Limit: 2})
	if err != nil {
		t.Fatalf("QueryBusinessView first page: %v", err)
	}
	if !first.HasMore || first.NextBefore == "" || first.NextBeforeID != "SO-VIEW-2" || len(first.Records) != 2 || first.Records[0].ID != "SO-VIEW-3" || first.Records[1].ID != "SO-VIEW-2" {
		t.Fatalf("business view should return a stable next cursor: %#v", first)
	}
	if _, ok := first.Records[0].Data["amount"]; !ok {
		t.Fatalf("business view should project declared fields: %#v", first.Records[0].Data)
	}
	if _, ok := first.Records[0].Data["gross_margin"]; ok {
		t.Fatalf("business view should omit undeclared fields: %#v", first.Records[0].Data)
	}

	second, err := svc.QueryBusinessView(context.Background(), p, "sales.order_overview", QueryBusinessViewInput{Filter: map[string]any{"field": "customer", "op": "eq", "value": "CursorCo"}, Limit: 2, Before: first.NextBefore, BeforeID: first.NextBeforeID})
	if err != nil {
		t.Fatalf("QueryBusinessView second page: %v", err)
	}
	if second.HasMore || len(second.Records) != 1 || second.Records[0].ID != "SO-VIEW-1" || second.NextBefore != "" || second.NextBeforeID != "" {
		t.Fatalf("business view cursor should continue to the remaining records: %#v", second)
	}
}
func TestSQLiteStoreScalarArraySortIsStable(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "ops", Name: "tasks", Title: "Tasks"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	fixtures := []CreateRecordInput{
		{ID: "task_a", Title: "Task A", Data: map[string]any{"watchers": []any{"Cora", "Dana"}}},
		{ID: "task_b", Title: "Task B", Data: map[string]any{"watchers": []any{"Alice", "Eve"}}},
		{ID: "task_c", Title: "Task C", Data: map[string]any{"watchers": []any{"Bob"}}},
	}
	for _, fixture := range fixtures {
		if _, err := svc.CreateRecord(context.Background(), p, ds.ID, fixture); err != nil {
			t.Fatalf("CreateRecord %s: %v", fixture.ID, err)
		}
	}

	items, err := svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Sort: []SortSpec{{Field: "watchers", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array asc sort: %v", err)
	}
	if len(items) != 3 || items[0].ID != "task_b" || items[1].ID != "task_c" || items[2].ID != "task_a" {
		t.Fatalf("unexpected scalar array asc sort: %#v", items)
	}

	items, err = svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Sort: []SortSpec{{Field: "watchers", Direction: "desc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords scalar array desc sort: %v", err)
	}
	if len(items) != 3 || items[0].ID != "task_b" || items[1].ID != "task_a" || items[2].ID != "task_c" {
		t.Fatalf("unexpected scalar array desc sort: %#v", items)
	}
}

func TestSQLiteStoreAggregateScalarArrayGroupExpansion(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "ops", Name: "incidents", Title: "Incidents"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.CreateRecord(context.Background(), p, ds.ID, CreateRecordInput{ID: "incident_1", Title: "Incident 1", Data: map[string]any{"watchers": []any{"Alice", "Bob"}, "labels": []any{"sev1", "network"}, "scores": []any{2, 5}}}); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	aggregate, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"watchers", "labels"}, Metrics: []AggregateMetric{{Name: "incidents", Op: "count"}}, Limit: 10})
	if err != nil {
		t.Fatalf("AggregateRecords scalar array combination group: %v", err)
	}
	if aggregate.Scanned != 1 || len(aggregate.Rows) != 4 {
		t.Fatalf("expected one scanned record expanded into four groups: %#v", aggregate)
	}
	expected := []map[string]any{
		{"watchers": "Alice", "labels": "network"},
		{"watchers": "Alice", "labels": "sev1"},
		{"watchers": "Bob", "labels": "network"},
		{"watchers": "Bob", "labels": "sev1"},
	}
	for i, want := range expected {
		if aggregate.Rows[i]["watchers"] != want["watchers"] || aggregate.Rows[i]["labels"] != want["labels"] || aggregate.Rows[i]["incidents"] != 1 {
			t.Fatalf("unexpected expanded group at %d: %#v", i, aggregate.Rows)
		}
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"watchers"}, Metrics: []AggregateMetric{{Name: "watcher_count", Op: "count_distinct", Field: "watchers"}}, Sort: []SortSpec{{Field: "watchers", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("AggregateRecords grouped count_distinct over same array field: %v", err)
	}
	if len(aggregate.Rows) != 2 || aggregate.Rows[0]["watchers"] != "Alice" || aggregate.Rows[0]["watcher_count"] != 1 || aggregate.Rows[1]["watchers"] != "Bob" || aggregate.Rows[1]["watcher_count"] != 1 {
		t.Fatalf("grouped count_distinct should use the current group label: %#v", aggregate)
	}

	aggregate, err = svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{GroupBy: []string{"scores"}, Metrics: []AggregateMetric{{Name: "score_sum", Op: "sum", Field: "scores"}, {Name: "score_avg", Op: "avg", Field: "scores"}, {Name: "score_min", Op: "min", Field: "scores"}, {Name: "score_max", Op: "max", Field: "scores"}}, Sort: []SortSpec{{Field: "scores", Direction: "asc"}}, Limit: 10})
	if err != nil {
		t.Fatalf("AggregateRecords grouped numeric metric over same array field: %v", err)
	}
	if len(aggregate.Rows) != 2 ||
		aggregate.Rows[0]["scores"] != float64(2) || aggregate.Rows[0]["score_sum"] != float64(2) || aggregate.Rows[0]["score_avg"] != float64(2) || aggregate.Rows[0]["score_min"] != float64(2) || aggregate.Rows[0]["score_max"] != float64(2) ||
		aggregate.Rows[1]["scores"] != float64(5) || aggregate.Rows[1]["score_sum"] != float64(5) || aggregate.Rows[1]["score_avg"] != float64(5) || aggregate.Rows[1]["score_min"] != float64(5) || aggregate.Rows[1]["score_max"] != float64(5) {
		t.Fatalf("grouped numeric metrics should use the current group label: %#v", aggregate)
	}

	largeA := []any{}
	largeB := []any{}
	for i := 0; i < 8; i++ {
		largeA = append(largeA, fmt.Sprintf("a%d", i))
		largeB = append(largeB, fmt.Sprintf("b%d", i))
	}
	if _, err := svc.CreateRecord(context.Background(), p, ds.ID, CreateRecordInput{ID: "incident_2", Title: "Incident 2", Data: map[string]any{"large_a": largeA, "large_b": largeB}}); err != nil {
		t.Fatalf("CreateRecord large arrays: %v", err)
	}
	if _, err := svc.AggregateRecords(context.Background(), p, ds.ID, AggregateInput{Filter: map[string]any{"field": "large_a", "op": "exists"}, GroupBy: []string{"large_a", "large_b"}, Metrics: []AggregateMetric{{Name: "incidents", Op: "count"}}, Limit: 10}); err == nil {
		t.Fatalf("expected too many aggregate group values to fail")
	}
}

func TestSQLiteStoreMigratesRecordFieldIndexMultiValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at) VALUES(17, '2026-05-06T00:00:00Z')`,
		`CREATE TABLE record_field_index(
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			field_key TEXT NOT NULL,
			value_text TEXT,
			value_number REAL,
			value_time TEXT,
			PRIMARY KEY(tenant_id, dataset_id, record_id, field_key)
		)`,
		`INSERT INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text) VALUES('tenant_1', 'sales.orders', 'order_1', 'watchers', 'Dana')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatalf("seed old schema: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore migrated old db: %v", err)
	}
	defer store.Close()
	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text, value_hash) VALUES(?, ?, ?, ?, ?, ?)`, "tenant_1", "sales.orders", "order_1", "watchers", "Ops", recordIndexValueHash("Ops")); err != nil {
		t.Fatalf("insert second value for migrated index key: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM record_field_index WHERE tenant_id = ? AND dataset_id = ? AND record_id = ? AND field_key = ?`, "tenant_1", "sales.orders", "order_1", "watchers").Scan(&count); err != nil {
		t.Fatalf("count migrated index rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("migrated index row count = %d, want 2", count)
	}
}

func TestSQLiteStoreMigrationV18RebuildsArrayFieldIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at) VALUES(17, '2026-05-06T00:00:00Z')`,
		`CREATE TABLE records(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			data_json TEXT NOT NULL,
			source_id TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, dataset_id, id)
		)`,
		`CREATE TABLE record_field_index(
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			field_key TEXT NOT NULL,
			value_text TEXT,
			value_number REAL,
			value_time TEXT,
			PRIMARY KEY(tenant_id, dataset_id, record_id, field_key)
		)`,
		`INSERT INTO records(id, tenant_id, dataset_id, title, data_json, created_at, updated_at) VALUES('order_1', 'tenant_1', 'sales.orders', 'Order 1', '{"watchers":["Dana","Ops"],"approval":{"status":"pending"}}', '2026-05-06T00:00:00Z', '2026-05-06T00:00:00Z')`,
		`INSERT INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text) VALUES('tenant_1', 'sales.orders', 'order_1', 'watchers', '["Dana","Ops"]')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatalf("seed old schema: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore migrated old db: %v", err)
	}
	defer store.Close()
	var watcherCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM record_field_index WHERE tenant_id = ? AND dataset_id = ? AND record_id = ? AND field_key = ? AND value_text IN ('Dana', 'Ops')`, "tenant_1", "sales.orders", "order_1", "watchers").Scan(&watcherCount); err != nil {
		t.Fatalf("count rebuilt watcher indexes: %v", err)
	}
	if watcherCount != 2 {
		t.Fatalf("rebuilt watcher index count = %d, want 2", watcherCount)
	}
	var nestedCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM record_field_index WHERE tenant_id = ? AND dataset_id = ? AND record_id = ? AND field_key = ? AND value_text = ?`, "tenant_1", "sales.orders", "order_1", "approval.status", "pending").Scan(&nestedCount); err != nil {
		t.Fatalf("count rebuilt nested indexes: %v", err)
	}
	if nestedCount != 1 {
		t.Fatalf("rebuilt nested index count = %d, want 1", nestedCount)
	}
}

func TestSQLiteStoreMigrationV18SkipsOversizedLegacyIndexRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at) VALUES(17, '2026-05-06T00:00:00Z')`,
		`CREATE TABLE records(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			data_json TEXT NOT NULL,
			source_id TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, dataset_id, id)
		)`,
		`CREATE TABLE record_field_index(
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			field_key TEXT NOT NULL,
			value_text TEXT,
			value_number REAL,
			value_time TEXT,
			PRIMARY KEY(tenant_id, dataset_id, record_id, field_key)
		)`,
		`INSERT INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text) VALUES('tenant_1', 'sales.orders', 'order_large', 'legacy_key', 'legacy_value')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatalf("seed old schema: %v", err)
		}
	}
	largeData := map[string]any{}
	for i := 0; i <= maxRecordIndexKeys; i++ {
		largeData[fmt.Sprintf("field_%03d", i)] = i
	}
	if _, err := db.Exec(`INSERT INTO records(id, tenant_id, dataset_id, title, data_json, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, "order_large", "tenant_1", "sales.orders", "Large Order", jsonString(largeData), "2026-05-06T00:00:00Z", "2026-05-06T00:00:00Z"); err != nil {
		_ = db.Close()
		t.Fatalf("seed oversized record: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore should skip oversized legacy index rebuild: %v", err)
	}
	defer store.Close()
	var legacyCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM record_field_index WHERE tenant_id = ? AND dataset_id = ? AND record_id = ? AND field_key = ? AND value_text = ?`, "tenant_1", "sales.orders", "order_large", "legacy_key", "legacy_value").Scan(&legacyCount); err != nil {
		t.Fatalf("count preserved legacy index: %v", err)
	}
	if legacyCount != 1 {
		t.Fatalf("legacy index count = %d, want 1", legacyCount)
	}
	var rebuiltCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM record_field_index WHERE tenant_id = ? AND dataset_id = ? AND record_id = ?`, "tenant_1", "sales.orders", "order_large").Scan(&rebuiltCount); err != nil {
		t.Fatalf("count oversized record indexes: %v", err)
	}
	if rebuiltCount != 1 {
		t.Fatalf("oversized legacy record should keep only copied indexes, got %d", rebuiltCount)
	}
}

func TestSQLiteStoreMigrationV18SkipsInvalidLegacyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at) VALUES(17, '2026-05-06T00:00:00Z')`,
		`CREATE TABLE records(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			data_json TEXT NOT NULL,
			source_id TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, dataset_id, id)
		)`,
		`CREATE TABLE record_field_index(
			tenant_id TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			field_key TEXT NOT NULL,
			value_text TEXT,
			value_number REAL,
			value_time TEXT,
			PRIMARY KEY(tenant_id, dataset_id, record_id, field_key)
		)`,
		`INSERT INTO records(id, tenant_id, dataset_id, title, data_json, created_at, updated_at) VALUES('order_bad', 'tenant_1', 'sales.orders', 'Bad Order', '{"watchers":["Dana"', '2026-05-06T00:00:00Z', '2026-05-06T00:00:00Z')`,
		`INSERT INTO records(id, tenant_id, dataset_id, title, data_json, created_at, updated_at) VALUES('order_good', 'tenant_1', 'sales.orders', 'Good Order', '{"watchers":["Ops"]}', '2026-05-06T00:00:00Z', '2026-05-06T00:00:00Z')`,
		`INSERT INTO record_field_index(tenant_id, dataset_id, record_id, field_key, value_text) VALUES('tenant_1', 'sales.orders', 'order_bad', 'legacy_key', 'legacy_value')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatalf("seed old schema: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore should skip invalid legacy JSON: %v", err)
	}
	defer store.Close()
	var legacyCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM record_field_index WHERE record_id = ? AND field_key = ? AND value_text = ?`, "order_bad", "legacy_key", "legacy_value").Scan(&legacyCount); err != nil {
		t.Fatalf("count preserved invalid JSON legacy index: %v", err)
	}
	if legacyCount != 1 {
		t.Fatalf("invalid JSON legacy index count = %d, want 1", legacyCount)
	}
	var rebuiltGoodCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM record_field_index WHERE record_id = ? AND field_key = ? AND value_text = ?`, "order_good", "watchers", "Ops").Scan(&rebuiltGoodCount); err != nil {
		t.Fatalf("count rebuilt good JSON index: %v", err)
	}
	if rebuiltGoodCount != 1 {
		t.Fatalf("good JSON rebuilt index count = %d, want 1", rebuiltGoodCount)
	}
}

func TestSQLiteStoreMigrationV20PersistsAdminLoginLockoutColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at) VALUES(19, '2026-05-07T09:00:00Z')`,
		`CREATE TABLE admin_users(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			username TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_login_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, username),
			UNIQUE(id)
		)`,
		`CREATE TABLE admin_sessions(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			username TEXT NOT NULL,
			role TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatalf("seed v19 schema: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO admin_users(
		id, tenant_id, username, display_name, role, password_hash, enabled, created_at, updated_at, login_failure_count, login_locked_until
	) VALUES('admin_1', 'default', 'admin', 'Admin', 'data_admin', 'hash', 1, '2026-05-07T09:00:00Z', '2026-05-07T09:00:00Z', 2, '2026-05-07T09:10:00Z')`); err != nil {
		t.Fatalf("admin_users v20 columns should be writable after migration: %v", err)
	}
	user, err := store.FindAdminUser(context.Background(), "default", "admin")
	if err != nil {
		t.Fatalf("FindAdminUser: %v", err)
	}
	if user.LoginFailureCount != 2 || user.LoginLockedUntil.IsZero() {
		t.Fatalf("login lockout columns not scanned after v20 migration: %#v", user)
	}
}

func TestSQLiteStoreMigrationV20RecoversPartialAdminLockoutMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at) VALUES(19, '2026-05-07T09:00:00Z')`,
		`CREATE TABLE admin_users(
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			username TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_login_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			login_failure_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(tenant_id, username),
			UNIQUE(id)
		)`,
		`CREATE TABLE admin_sessions(
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			username TEXT NOT NULL,
			role TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatalf("seed partial v20 schema: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore should recover partial v20 migration: %v", err)
	}
	defer store.Close()
	var lockedUntilDefault string
	if err := store.db.QueryRowContext(context.Background(), `SELECT dflt_value FROM pragma_table_info('admin_users') WHERE name = 'login_locked_until'`).Scan(&lockedUntilDefault); err != nil {
		t.Fatalf("login_locked_until column should exist after partial v20 migration recovery: %v", err)
	}
	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
}

func TestSQLiteColumnExistsRejectsNonSQLiteIdentifiers(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	tx, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	for _, tc := range []struct {
		table  string
		column string
	}{
		{table: "admin-users", column: "login_failure_count"},
		{table: "admin_users", column: "login-locked-until"},
		{table: "1admin_users", column: "login_locked_until"},
		{table: "admin_users; DROP TABLE admin_users", column: "login_locked_until"},
	} {
		if _, err := sqliteColumnExists(context.Background(), tx, tc.table, tc.column); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("sqliteColumnExists(%q, %q) error=%v, want ErrInvalidInput", tc.table, tc.column, err)
		}
	}
	ok, err := sqliteColumnExists(context.Background(), tx, "admin_users", "login_locked_until")
	if err != nil {
		t.Fatalf("sqliteColumnExists valid identifier: %v", err)
	}
	if !ok {
		t.Fatal("login_locked_until should exist in migrated admin_users table")
	}
}

func TestSQLiteAdminLoginFailurePreservesActiveLockout(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	lockedUntil := now.Add(10 * time.Minute)
	record := adminUserRecord{
		ID:                "admin_1",
		TenantID:          "default",
		Username:          "admin",
		DisplayName:       "Admin",
		Role:              "data_admin",
		PasswordHash:      "hash",
		Enabled:           true,
		CreatedAt:         now.Add(-time.Hour),
		UpdatedAt:         now,
		LoginFailureCount: 2,
		LoginLockedUntil:  lockedUntil,
	}
	if _, err := store.CreateAdminUser(context.Background(), record); err != nil {
		t.Fatalf("CreateAdminUser: %v", err)
	}
	out, err := store.RecordAdminLoginFailure(context.Background(), "default", "admin", now.Add(time.Minute), 2, 15*time.Minute)
	if err != nil {
		t.Fatalf("RecordAdminLoginFailure: %v", err)
	}
	if out.LoginFailureCount != 2 || !out.LoginLockedUntil.Equal(lockedUntil) || !out.UpdatedAt.Equal(now) {
		t.Fatalf("active lockout should be preserved without extending lockout: count=%d locked_until=%s updated_at=%s", out.LoginFailureCount, out.LoginLockedUntil, out.UpdatedAt)
	}
}

func TestSQLiteClearAdminLoginFailureRequiresExistingAdmin(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	err = store.ClearAdminLoginFailure(context.Background(), "default", "missing", time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrAdminNotFound) {
		t.Fatalf("ClearAdminLoginFailure error=%v, want ErrAdminNotFound", err)
	}
}

func TestSQLiteStoreBackupCursorKeepsCreatedAtTies(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}
	createdAt := time.Date(2026, 5, 6, 10, 15, 0, 0, time.UTC)

	for _, suffix := range []string{"001", "002", "003"} {
		id := "backup_cursor_" + suffix
		backupPath := filepath.Join(store.backupDir, id+".db")
		if err := os.MkdirAll(store.backupDir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(backupPath, []byte("backup "+suffix), 0o600); err != nil {
			t.Fatalf("WriteFile backup %s: %v", suffix, err)
		}
		if err := writeBackupMeta(store.backupMetaPath(id), BackupInfo{ID: id, Name: "fixture " + suffix, Engine: "sqlite", Path: backupPath, SizeBytes: int64(len("backup " + suffix)), CreatedBy: p.UserID, CreatedAt: createdAt}); err != nil {
			t.Fatalf("writeBackupMeta %s: %v", suffix, err)
		}
	}

	firstPage, err := svc.ListBackups(context.Background(), p, QueryBackupsInput{Limit: 2})
	if err != nil {
		t.Fatalf("ListBackups first page: %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "backup_cursor_003" || firstPage[1].ID != "backup_cursor_002" {
		t.Fatalf("unexpected backups first page: %#v", firstPage)
	}
	cursor := backupListResponse(firstPage, 2)
	if !cursor.HasMore || cursor.NextBefore != createdAt.Format(time.RFC3339Nano) || cursor.NextBeforeID != "backup_cursor_002" {
		t.Fatalf("unexpected backup cursor: %#v", cursor)
	}

	secondPage, err := svc.ListBackups(context.Background(), p, QueryBackupsInput{Limit: 2, Before: cursor.NextBefore, BeforeID: cursor.NextBeforeID})
	if err != nil {
		t.Fatalf("ListBackups second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != "backup_cursor_001" {
		t.Fatalf("backup cursor should continue through created_at ties: %#v", secondPage)
	}
}
func TestSQLiteStoreBackupAndRestoreFlow(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_1", UserID: "user_1"}

	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "sales", Name: "orders", Title: "Orders"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.CreateRecord(context.Background(), p, ds.ID, CreateRecordInput{Title: "Order A", Data: map[string]any{"customer": "Acme", "amount": 8800}}); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	backup, err := svc.CreateBackup(context.Background(), p, CreateBackupInput{Name: "before cleanup", Note: "test backup"})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if backup.ID == "" || backup.SizeBytes <= 0 {
		t.Fatalf("unexpected backup: %#v", backup)
	}
	backups, err := svc.ListBackups(context.Background(), p, QueryBackupsInput{})
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 1 || backups[0].ID != backup.ID {
		t.Fatalf("unexpected backups: %#v", backups)
	}
	if err := svc.DeleteDataset(context.Background(), p, ds.ID); err != nil {
		t.Fatalf("DeleteDataset: %v", err)
	}
	if _, err := svc.GetDataset(context.Background(), p, ds.ID); err != ErrDatasetNotFound {
		t.Fatalf("expected dataset deletion before restore, got %v", err)
	}
	result, err := svc.RestoreBackup(context.Background(), p, backup.ID, RestoreBackupInput{Confirm: true, Reason: "test restore"})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if result.Status != "restored" || result.Backup.ID != backup.ID {
		t.Fatalf("unexpected restore result: %#v", result)
	}
	rollbackFiles, err := filepath.Glob(store.path + ".before-restore-*")
	if err != nil {
		t.Fatalf("glob rollback files: %v", err)
	}
	if len(rollbackFiles) != 1 {
		t.Fatalf("expected one pre-restore rollback snapshot, got %v", rollbackFiles)
	}
	if stat, err := os.Stat(rollbackFiles[0]); err != nil || stat.Size() == 0 {
		t.Fatalf("expected non-empty rollback snapshot %q stat=%#v err=%v", rollbackFiles[0], stat, err)
	}
	items, err := svc.QueryRecords(context.Background(), p, ds.ID, QueryRecordsInput{Q: "Acme", Limit: 10})
	if err != nil {
		t.Fatalf("QueryRecords after restore: %v", err)
	}
	if len(items) != 1 || items[0].Data["customer"] != "Acme" {
		t.Fatalf("unexpected restored records: %#v", items)
	}
}
