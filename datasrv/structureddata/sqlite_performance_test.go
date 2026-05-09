package structureddata

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLitePerformanceIndexesExist(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	for _, indexName := range []string{
		"idx_records_scope",
		"idx_field_text",
		"idx_field_number",
		"idx_field_time",
		"idx_tags_lookup",
		"idx_audit_tenant_time",
		"idx_audit_action_time",
		"idx_import_jobs_dataset_time",
		"idx_import_jobs_tenant_time",
		"idx_import_jobs_status",
		"idx_export_jobs_dataset_time",
		"idx_export_jobs_tenant_time",
		"idx_export_jobs_status",
	} {
		var name string
		err := store.db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, indexName).Scan(&name)
		if err != nil {
			t.Fatalf("expected performance index %s to exist: %v", indexName, err)
		}
	}
	var ftsTable string
	if err := store.db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'record_fts'`).Scan(&ftsTable); err != nil {
		t.Fatalf("expected record_fts virtual table to exist: %v", err)
	}
}

func BenchmarkSQLiteQueryRecordsIndexedNumberFilter(b *testing.B) {
	svc, p, datasetID := newBenchmarkRecordStore(b, 1500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := svc.QueryRecords(context.Background(), p, datasetID, QueryRecordsInput{
			Filter: map[string]any{"field": "amount", "op": "gte", "value": 1200},
			Sort:   []SortSpec{{Field: "amount", Direction: "desc"}},
			Limit:  50,
		})
		if err != nil {
			b.Fatalf("QueryRecords indexed number filter: %v", err)
		}
		if len(items) == 0 {
			b.Fatal("expected indexed number filter to return records")
		}
	}
}

func BenchmarkSQLiteQueryRecordsFTSAndTag(b *testing.B) {
	svc, p, datasetID := newBenchmarkRecordStore(b, 1500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := svc.QueryRecords(context.Background(), p, datasetID, QueryRecordsInput{Q: "renewal", Tag: "finance", Limit: 50})
		if err != nil {
			b.Fatalf("QueryRecords fts/tag: %v", err)
		}
		if len(items) == 0 {
			b.Fatal("expected fts/tag query to return records")
		}
	}
}

func BenchmarkSQLiteQueryAuditLogsCursorPage(b *testing.B) {
	store, err := NewSQLiteStore(filepath.Join(b.TempDir(), "data.db"))
	if err != nil {
		b.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_perf", UserID: "bench_admin", Role: "data_admin"}
	base := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 1500; i++ {
		entry := AuditLog{
			ID:         fmt.Sprintf("audit_%04d", i),
			TenantID:   p.TenantID,
			UserID:     p.UserID,
			Action:     "record.update",
			DatasetID:  "perf.records",
			TargetType: "record",
			TargetID:   fmt.Sprintf("record_%04d", i),
			Summary:    "Updated benchmark record",
			Metadata:   map[string]any{"batch": i % 10},
			CreatedAt:  base.Add(time.Duration(i) * time.Second),
		}
		if _, err := store.AppendAuditLog(context.Background(), entry); err != nil {
			b.Fatalf("AppendAuditLog: %v", err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := svc.QueryAuditLogs(context.Background(), p, QueryAuditLogsInput{Action: "record.update", Limit: 100})
		if err != nil {
			b.Fatalf("QueryAuditLogs: %v", err)
		}
		if len(items) == 0 {
			b.Fatal("expected audit cursor page to return records")
		}
	}
}

func BenchmarkSQLiteFindAPIKeyPolicyBySecret(b *testing.B) {
	store, err := NewSQLiteStore(filepath.Join(b.TempDir(), "data.db"))
	if err != nil {
		b.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_perf", UserID: "bench_admin", Role: "data_admin"}
	created, err := svc.CreateAPIKeyPolicy(context.Background(), p, CreateAPIKeyPolicyInput{
		ID:              "bench_key",
		UserID:          "bench_agent",
		Role:            "data_user",
		AllowedDatasets: []string{"perf.records"},
		AllowedActions:  []string{"record.query"},
	})
	if err != nil {
		b.Fatalf("CreateAPIKeyPolicy: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policy, err := svc.FindAPIKeyPolicyBySecret(context.Background(), created.Key)
		if err != nil {
			b.Fatalf("FindAPIKeyPolicyBySecret: %v", err)
		}
		if policy.ID != created.Policy.ID {
			b.Fatalf("unexpected policy id %q", policy.ID)
		}
	}
}

func BenchmarkSQLiteFindAdminSessionBySecret(b *testing.B) {
	store, err := NewSQLiteStore(filepath.Join(b.TempDir(), "data.db"))
	if err != nil {
		b.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	login, err := svc.InitializeAdmin(context.Background(), InitializeAdminInput{
		TenantID:    "tenant_perf",
		Username:    "bench_admin",
		Password:    "StrongerPassword123!",
		DisplayName: "Benchmark Admin",
	})
	if err != nil {
		b.Fatalf("InitializeAdmin: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		principal, err := svc.FindAdminSessionBySecret(context.Background(), login.Token)
		if err != nil {
			b.Fatalf("FindAdminSessionBySecret: %v", err)
		}
		if principal.UserID == "" || principal.Role != "data_admin" {
			b.Fatalf("unexpected principal: %#v", principal)
		}
	}
}

func BenchmarkSQLiteGovernanceEvidencePackOperationalFixture(b *testing.B) {
	svc, p := newBenchmarkGovernanceStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pack, err := svc.GovernanceEvidencePack(context.Background(), p, GovernanceEvidencePackInput{MinSeverity: "medium", Lang: "en"})
		if err != nil {
			b.Fatalf("GovernanceEvidencePack: %v", err)
		}
		if pack.EvidenceID == "" || pack.Summary.SectionCount < 6 {
			b.Fatalf("unexpected governance evidence pack: %#v", pack)
		}
	}
}

func BenchmarkSQLiteListImportJobsStatusPage(b *testing.B) {
	svc, p, datasetID := newBenchmarkJobHistoryStore(b, 1500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := svc.ListImportJobs(context.Background(), p, QueryImportJobsInput{DatasetID: datasetID, Status: "completed", Limit: 100})
		if err != nil {
			b.Fatalf("ListImportJobs: %v", err)
		}
		if len(items) == 0 {
			b.Fatal("expected import job page to return items")
		}
	}
}

func BenchmarkSQLiteListExportJobsStatusPage(b *testing.B) {
	svc, p, datasetID := newBenchmarkJobHistoryStore(b, 1500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := svc.ListExportJobs(context.Background(), p, QueryExportJobsInput{DatasetID: datasetID, Status: "completed", Limit: 100})
		if err != nil {
			b.Fatalf("ListExportJobs: %v", err)
		}
		if len(items) == 0 {
			b.Fatal("expected export job page to return items")
		}
	}
}

func newBenchmarkRecordStore(tb testing.TB, count int) (*Service, Principal, string) {
	tb.Helper()
	store, err := NewSQLiteStore(filepath.Join(tb.TempDir(), "data.db"))
	if err != nil {
		tb.Fatalf("NewSQLiteStore: %v", err)
	}
	tb.Cleanup(func() { _ = store.Close() })
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_perf", UserID: "bench_admin", Role: "data_admin"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "perf", Name: "records", Title: "Performance Records"})
	if err != nil {
		tb.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.UpsertFields(context.Background(), p, ds.ID, UpsertFieldsInput{Fields: []FieldDefinition{
		{Key: "amount", Type: "number", Indexed: true},
		{Key: "department", Type: "string", Indexed: true},
		{Key: "order_date", Type: "date", Indexed: true},
	}}); err != nil {
		tb.Fatalf("UpsertFields: %v", err)
	}
	base := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	records := make([]Record, 0, count)
	for i := 0; i < count; i++ {
		department := "sales"
		if i%3 == 0 {
			department = "finance"
		}
		title := fmt.Sprintf("Benchmark order %04d renewal", i)
		if i%5 == 0 {
			title = strings.ReplaceAll(title, "renewal", "expansion")
		}
		records = append(records, Record{
			ID:        fmt.Sprintf("record_%04d", i),
			TenantID:  p.TenantID,
			DatasetID: ds.ID,
			Title:     title,
			Tags:      []string{department, "finance"},
			Data: map[string]any{
				"amount":     i % 2000,
				"department": department,
				"order_date": base.AddDate(0, 0, i%90).Format("2006-01-02"),
				"customer":   fmt.Sprintf("Customer %03d", i%120),
			},
			CreatedBy: p.UserID,
			UpdatedBy: p.UserID,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	if _, err := store.ImportRecords(context.Background(), records); err != nil {
		tb.Fatalf("ImportRecords: %v", err)
	}
	return svc, p, ds.ID
}

func newBenchmarkGovernanceStore(tb testing.TB) (*Service, Principal) {
	tb.Helper()
	store, err := NewSQLiteStore(filepath.Join(tb.TempDir(), "data.db"))
	if err != nil {
		tb.Fatalf("NewSQLiteStore: %v", err)
	}
	tb.Cleanup(func() { _ = store.Close() })
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_perf", UserID: "bench_admin", Role: "data_admin"}
	now := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	if _, err := svc.CreateBackup(context.Background(), p, CreateBackupInput{Name: "governance baseline"}); err != nil {
		tb.Fatalf("CreateBackup: %v", err)
	}
	for i := 0; i < 120; i++ {
		role := "data_user"
		allowRaw := false
		if i%10 == 0 {
			role = "data_admin"
			allowRaw = true
		}
		_, err := svc.CreateAPIKeyPolicy(context.Background(), p, CreateAPIKeyPolicyInput{
			ID:              fmt.Sprintf("bench_key_%03d", i),
			UserID:          fmt.Sprintf("bench_agent_%03d", i),
			Role:            role,
			AllowedDatasets: []string{"perf.records"},
			AllowedActions:  []string{"record.query"},
			AllowRawData:    allowRaw,
			AllowAdmin:      role == "data_admin",
		})
		if err != nil {
			tb.Fatalf("CreateAPIKeyPolicy %d: %v", i, err)
		}
	}
	for i := 0; i < 25; i++ {
		connector := ExternalConnector{
			ID:                fmt.Sprintf("connector_%03d", i),
			TenantID:          p.TenantID,
			Domain:            "perf",
			Name:              fmt.Sprintf("Connector %03d", i),
			Kind:              "crm",
			Enabled:           i%7 != 0,
			SubscribedActions: []string{"record.upsert"},
			CreatedBy:         p.UserID,
			CreatedAt:         now.Add(time.Duration(i) * time.Second),
			UpdatedAt:         now.Add(time.Duration(i) * time.Second),
		}
		if _, err := store.UpsertExternalConnector(context.Background(), connector); err != nil {
			tb.Fatalf("UpsertExternalConnector %d: %v", i, err)
		}
	}
	for i := 0; i < 1500; i++ {
		entry := AuditLog{
			ID:         fmt.Sprintf("audit_governance_%04d", i),
			TenantID:   p.TenantID,
			UserID:     p.UserID,
			Action:     "record.update",
			DatasetID:  "perf.records",
			TargetType: "record",
			TargetID:   fmt.Sprintf("record_%04d", i),
			Summary:    "Updated benchmark record",
			Metadata:   map[string]any{"batch": i % 20},
			CreatedAt:  now.Add(time.Duration(i) * time.Second),
		}
		if _, err := store.AppendAuditLog(context.Background(), entry); err != nil {
			tb.Fatalf("AppendAuditLog %d: %v", i, err)
		}
	}
	return svc, p
}

func newBenchmarkJobHistoryStore(tb testing.TB, count int) (*Service, Principal, string) {
	tb.Helper()
	store, err := NewSQLiteStore(filepath.Join(tb.TempDir(), "data.db"))
	if err != nil {
		tb.Fatalf("NewSQLiteStore: %v", err)
	}
	tb.Cleanup(func() { _ = store.Close() })
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_perf", UserID: "bench_admin", Role: "data_admin"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "perf", Name: "jobs", Title: "Job History"})
	if err != nil {
		tb.Fatalf("CreateDataset: %v", err)
	}
	base := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		status := "completed"
		if i%11 == 0 {
			status = "failed"
		}
		createdAt := base.Add(time.Duration(i) * time.Second)
		importJob := ImportJob{
			ID:         fmt.Sprintf("import_job_%04d", i),
			TenantID:   p.TenantID,
			DatasetID:  ds.ID,
			Kind:       "jsonl",
			Status:     status,
			Total:      100,
			Imported:   100,
			Valid:      status == "completed",
			CreatedBy:  p.UserID,
			CreatedAt:  createdAt,
			StartedAt:  createdAt,
			FinishedAt: createdAt.Add(time.Second),
		}
		if status == "failed" {
			importJob.Error = "benchmark failure"
		}
		if _, err := store.UpsertImportJob(context.Background(), importJob); err != nil {
			tb.Fatalf("UpsertImportJob %d: %v", i, err)
		}
		exportJob := ExportJob{
			ID:         fmt.Sprintf("export_job_%04d", i),
			TenantID:   p.TenantID,
			DatasetID:  ds.ID,
			Format:     "jsonl",
			Status:     status,
			Total:      100,
			Bytes:      4096,
			ResultText: strings.Repeat("x", 128),
			CreatedBy:  p.UserID,
			CreatedAt:  createdAt,
			StartedAt:  createdAt,
			FinishedAt: createdAt.Add(time.Second),
		}
		if status == "failed" {
			exportJob.Error = "benchmark failure"
		}
		if _, err := store.UpsertExportJob(context.Background(), exportJob); err != nil {
			tb.Fatalf("UpsertExportJob %d: %v", i, err)
		}
	}
	return svc, p, ds.ID
}
