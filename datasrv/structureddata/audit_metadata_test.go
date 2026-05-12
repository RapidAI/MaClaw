package structureddata

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAPIKeyRotationAuditMetadataIsActionable(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_audit", UserID: "admin_1", Role: "data_admin"}
	created, err := svc.CreateAPIKeyPolicy(context.Background(), p, CreateAPIKeyPolicyInput{
		ID:              "agent_finance",
		UserID:          "agent_finance",
		Role:            "data_user",
		AllowedDatasets: []string{"finance.invoices"},
		AllowedActions:  []string{"record.query", "record.create"},
		AllowRawData:    true,
	})
	if err != nil {
		t.Fatalf("CreateAPIKeyPolicy: %v", err)
	}
	rotated, err := svc.RotateAPIKeyPolicySecret(context.Background(), p, created.Policy.ID)
	if err != nil {
		t.Fatalf("RotateAPIKeyPolicySecret: %v", err)
	}
	if rotated.Key == created.Key {
		t.Fatal("expected rotation to return a different secret")
	}
	log := latestAuditLog(t, svc, p, "access.api_key_rotate")
	requireAuditString(t, log.Metadata, "key_prefix", rotated.Policy.KeyPrefix)
	requireAuditString(t, log.Metadata, "role", "data_user")
	requireAuditBool(t, log.Metadata, "enabled", true)
	requireAuditBool(t, log.Metadata, "allow_raw_data", true)
	requireAuditNumber(t, log.Metadata, "allowed_dataset_count", 1)
	requireAuditNumber(t, log.Metadata, "allowed_action_count", 2)
}

func TestBackupRestoreAuditMetadataIncludesRecoveryEvidence(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_audit", UserID: "admin_1", Role: "data_admin"}
	ds, err := svc.CreateDataset(context.Background(), p, CreateDatasetInput{Domain: "audit", Name: "restore", Title: "Restore Audit"})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := svc.CreateRecord(context.Background(), p, ds.ID, CreateRecordInput{Title: "Acme", Data: map[string]any{"customer": "Acme"}}); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	backup, err := svc.CreateBackup(context.Background(), p, CreateBackupInput{Name: "before restore", Note: "audit metadata"})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if err := svc.DeleteDataset(context.Background(), p, ds.ID); err != nil {
		t.Fatalf("DeleteDataset: %v", err)
	}
	result, err := svc.RestoreBackup(context.Background(), p, backup.ID, RestoreBackupInput{Confirm: true, Reason: "operator recovery drill"})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	log := latestAuditLog(t, svc, p, "backup.restore")
	requireAuditString(t, log.Metadata, "backup_id", backup.ID)
	requireAuditString(t, log.Metadata, "sha256", result.Backup.SHA256)
	requireAuditString(t, log.Metadata, "download_url", result.Backup.DownloadURL)
	requireAuditString(t, log.Metadata, "reason", "operator recovery drill")
	requireAuditString(t, log.Metadata, "status", "restored")
	requireAuditNumber(t, log.Metadata, "size_bytes", result.Backup.SizeBytes)
}

func TestGovernanceEvidenceAuditMetadataSummarizesPack(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_audit", UserID: "admin_1", Role: "data_admin"}
	pack, err := svc.GovernanceEvidencePack(context.Background(), p, GovernanceEvidencePackInput{MinSeverity: "medium", Lang: "zh-CN"})
	if err != nil {
		t.Fatalf("GovernanceEvidencePack: %v", err)
	}
	log := latestAuditLog(t, svc, p, "governance.evidence_pack_export")
	requireAuditString(t, log.Metadata, "evidence_id", pack.EvidenceID)
	requireAuditString(t, log.Metadata, "evidence_sha256", pack.EvidenceSHA256)
	requireAuditString(t, log.Metadata, "lang", "zh")
	requireAuditString(t, log.Metadata, "status", pack.Summary.Status)
	requireAuditString(t, log.Metadata, "risk_level", pack.Summary.RiskLevel)
	requireAuditNumber(t, log.Metadata, "section_count", len(pack.Sections))
	requireAuditNumber(t, log.Metadata, "failed_sections", pack.Summary.FailedSections)
	requireAuditNumber(t, log.Metadata, "recommendation_count", len(pack.Summary.Recommendations))
}

func TestGovernanceEvidenceChineseSummaryIsReadable(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_audit", UserID: "admin_1", Role: "data_admin"}
	pack, err := svc.GovernanceEvidencePack(context.Background(), p, GovernanceEvidencePackInput{Lang: "zh-CN"})
	if err != nil {
		t.Fatalf("GovernanceEvidencePack: %v", err)
	}
	for _, want := range []string{"治理证据摘要", "证据 ID:", "证据 SHA256:", "状态:", "风险等级:", "控制项:", "建议:"} {
		if !strings.Contains(pack.SummaryText, want) {
			t.Fatalf("Chinese governance summary missing %q: %s", want, pack.SummaryText)
		}
	}
	for _, forbidden := range []string{string(utf8.RuneError), "涓", "鎶", "閲", "鐘", "灏", "闇"} {
		if strings.Contains(pack.SummaryText, forbidden) {
			t.Fatalf("Chinese governance summary contains mojibake marker %q: %s", forbidden, pack.SummaryText)
		}
	}
}

func latestAuditLog(t *testing.T, svc *Service, p Principal, action string) AuditLog {
	t.Helper()
	items, err := svc.QueryAuditLogs(context.Background(), p, QueryAuditLogsInput{Action: action, Limit: 1})
	if err != nil {
		t.Fatalf("QueryAuditLogs %s: %v", action, err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one audit log for %s, got %d", action, len(items))
	}
	if items[0].Metadata == nil {
		t.Fatalf("expected audit metadata for %s", action)
	}
	return items[0]
}

func requireAuditString(t *testing.T, metadata map[string]any, key string, want string) {
	t.Helper()
	got, ok := metadata[key].(string)
	if !ok || got != want {
		t.Fatalf("metadata[%s]=%#v, want %q", key, metadata[key], want)
	}
}

func requireAuditBool(t *testing.T, metadata map[string]any, key string, want bool) {
	t.Helper()
	got, ok := metadata[key].(bool)
	if !ok || got != want {
		t.Fatalf("metadata[%s]=%#v, want %v", key, metadata[key], want)
	}
}

func requireAuditNumber(t *testing.T, metadata map[string]any, key string, want any) {
	t.Helper()
	switch v := metadata[key].(type) {
	case int:
		if wantInt64(want) == int64(v) {
			return
		}
	case int64:
		if wantInt64(want) == v {
			return
		}
	case float64:
		if wantInt64(want) == int64(v) {
			return
		}
	}
	t.Fatalf("metadata[%s]=%#v, want %v", key, metadata[key], want)
}

func wantInt64(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}
