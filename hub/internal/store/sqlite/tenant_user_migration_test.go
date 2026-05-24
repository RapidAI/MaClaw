package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func newTenantMigrationProvider(t *testing.T) *Provider {
	t.Helper()
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hub-test.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return provider
}

func TestTenantUserMigrationDryRunDoesNotCommit(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO machines (id, tenant_id, user_id, name, platform, machine_token_hash, status, created_at, updated_at) VALUES ('machine-1', ?, 'user-1', 'pc', 'windows', 'hash', 'offline', ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID, DryRun: true})
	if err != nil {
		t.Fatalf("migrate dry-run: %v", err)
	}
	if result.UsersMoved != 1 || result.TableUpdates["users"] != 1 || result.TableUpdates["machines"] != 1 {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}

	var tenantID string
	if err := provider.Write.QueryRowContext(ctx, `SELECT tenant_id FROM users WHERE id = 'user-1'`).Scan(&tenantID); err != nil {
		t.Fatalf("query user tenant: %v", err)
	}
	if tenantID != store.DefaultTenantID {
		t.Fatalf("dry-run committed user tenant %q", tenantID)
	}
}

func TestTenantUserMigrationApplyMovesRelatedRows(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO sessions (id, tenant_id, machine_id, user_id, tool, title, project_path, status, started_at, updated_at) VALUES ('sess-1', ?, 'machine-1', 'user-1', 'claude', 'title', '', 'running', ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO login_tokens (id, tenant_id, email, token_hash, purpose, expires_at, created_at) VALUES ('login-1', ?, 'alice@example.com', 'hash-1', 'login', ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert login token: %v", err)
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID, DryRun: false})
	if err != nil {
		t.Fatalf("migrate apply: %v", err)
	}
	if result.UsersMoved != 1 || result.TableUpdates["sessions"] != 1 || result.TableUpdates["login_tokens"] != 1 {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	for _, table := range []string{"users", "sessions", "login_tokens"} {
		var count int
		if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = 'tenant_a'`).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s tenant_a count = %d, want 1", table, count)
		}
	}
}

func TestTenantUserMigrationCopiesTenantConfigAndRemapsSecurityGroup(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES ('llm_service_registry', '{"model_service_groups":[]}', ?), ('center_base_url', '"https://center"', ?)`, now, now); err != nil {
		t.Fatalf("insert settings: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `UPDATE tenant_digital_employee_authorizations SET enabled = 1, quota = 9, used = 4, status = 'active' WHERE tenant_id = ?`, store.DefaultTenantID); err != nil {
		t.Fatalf("update authz: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO security_groups (tenant_id, id, name, parent_id, created_at, updated_at) VALUES (?, 'root-default', 'root', '', ?, ?), (?, 'dept-default', 'dept', 'root-default', ?, ?)`, store.DefaultTenantID, now, now, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert groups: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO security_policies (tenant_id, group_id, policy_json, updated_at) VALUES (?, 'dept-default', '{"level":"dept"}', ?)`, store.DefaultTenantID, now); err != nil {
		t.Fatalf("insert policy: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO security_group_members (tenant_id, email, group_id, created_at) VALUES (?, 'alice@example.com', 'dept-default', ?)`, store.DefaultTenantID, now); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	registry := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "pro", Name: "Pro", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		GroupBindings:      []llmservice.GroupBinding{{GroupID: "dept-default", ServiceGroupIDs: []string{"pro"}}},
	}
	registryData, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `UPDATE system_settings SET value_json = ? WHERE key = 'llm_service_registry'`, string(registryData)); err != nil {
		t.Fatalf("update registry: %v", err)
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID, CopyTenantConfig: true})
	if err != nil {
		t.Fatalf("migrate apply: %v", err)
	}
	if result.TenantResourceCopies["tenant_a"]["llm_service_registry_base"] != 1 || result.TenantResourceCopies["tenant_a"]["tenant_digital_employee_authorizations"] != 1 {
		t.Fatalf("unexpected copied resources: %+v", result.TenantResourceCopies)
	}

	var copiedSetting string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = 'tenant:tenant_a:llm_service_registry'`).Scan(&copiedSetting); err != nil {
		t.Fatalf("query copied setting: %v", err)
	}
	var globalCopies int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_settings WHERE key = 'tenant:tenant_a:center_base_url'`).Scan(&globalCopies); err != nil {
		t.Fatalf("query global setting copy: %v", err)
	}
	if globalCopies != 0 {
		t.Fatalf("global setting copied into tenant scope")
	}
	var quota, used int
	if err := provider.Write.QueryRowContext(ctx, `SELECT quota, used FROM tenant_digital_employee_authorizations WHERE tenant_id = 'tenant_a'`).Scan(&quota, &used); err != nil {
		t.Fatalf("query authz: %v", err)
	}
	if quota != 9 || used != 0 {
		t.Fatalf("copied authz should keep quota and reset used, quota=%d used=%d", quota, used)
	}
	var memberGroupID string
	if err := provider.Write.QueryRowContext(ctx, `SELECT group_id FROM security_group_members WHERE tenant_id = 'tenant_a' AND email = 'alice@example.com'`).Scan(&memberGroupID); err != nil {
		t.Fatalf("query member: %v", err)
	}
	if memberGroupID == "dept-default" || memberGroupID == "" {
		t.Fatalf("security group was not remapped: %q", memberGroupID)
	}
	var policyJSON string
	if err := provider.Write.QueryRowContext(ctx, `SELECT policy_json FROM security_policies WHERE tenant_id = 'tenant_a' AND group_id = ?`, memberGroupID).Scan(&policyJSON); err != nil {
		t.Fatalf("query copied policy: %v", err)
	}
	if policyJSON != `{"level":"dept"}` {
		t.Fatalf("unexpected policy json: %s", policyJSON)
	}
	var targetRegistryRaw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = 'tenant:tenant_a:llm_service_registry'`).Scan(&targetRegistryRaw); err != nil {
		t.Fatalf("query target registry: %v", err)
	}
	targetRegistry, err := decodeLLMRegistry(targetRegistryRaw)
	if err != nil {
		t.Fatalf("decode target registry: %v", err)
	}
	if len(targetRegistry.GroupBindings) != 1 || targetRegistry.GroupBindings[0].GroupID != memberGroupID {
		t.Fatalf("expected LLM group binding remapped to %s, got %+v", memberGroupID, targetRegistry.GroupBindings)
	}
}

func TestTenantUserMigrationMovesLLMServiceAssignments(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, nowStr, nowStr); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, nowStr, nowStr); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	sourceReg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "pro", Name: "Pro", AccessPolicy: llmservice.AccessPolicyGrantRequired}},
		GroupBindings:      []llmservice.GroupBinding{{GroupID: "dept-default", ServiceGroupIDs: []string{"pro"}}},
		UserBindings:       []llmservice.UserBinding{{Email: "alice@example.com", ServiceGroupIDs: []string{"pro"}}, {Email: "other@example.com", ServiceGroupIDs: []string{"pro"}}},
		Grants:             []llmservice.Grant{{ID: "grant-alice", Email: "alice@example.com", ServiceGroupID: "pro", Source: "card", StartsAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now}, {ID: "grant-other", Email: "other@example.com", ServiceGroupID: "pro", Source: "card", StartsAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now}},
		Cards:              []llmservice.RechargeCard{{ID: "card-alice", RedeemedByEmail: "alice@example.com", CreatedAt: now}, {ID: "card-unused", CreatedAt: now}},
	}
	sourceData, err := json.Marshal(sourceReg)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)`, tenantRegistryKey(store.DefaultTenantID), string(sourceData), nowStr); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID})
	if err != nil {
		t.Fatalf("migrate apply: %v", err)
	}
	if result.TableUpdates["llm_service_user_bindings"] != 1 || result.TableUpdates["llm_service_grants"] != 1 || result.TableUpdates["llm_service_redeemed_cards"] != 1 {
		t.Fatalf("unexpected llm updates: %+v", result.TableUpdates)
	}
	var sourceRaw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = ?`, tenantRegistryKey(store.DefaultTenantID)).Scan(&sourceRaw); err != nil {
		t.Fatalf("get source registry: %v", err)
	}
	var targetRaw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = ?`, tenantRegistryKey("tenant_a")).Scan(&targetRaw); err != nil {
		t.Fatalf("get target registry: %v", err)
	}
	sourceAfter, err := decodeLLMRegistry(sourceRaw)
	if err != nil {
		t.Fatalf("decode source: %v", err)
	}
	targetAfter, err := decodeLLMRegistry(targetRaw)
	if err != nil {
		t.Fatalf("decode target: %v", err)
	}
	if len(sourceAfter.Grants) != 1 || sourceAfter.Grants[0].Email != "other@example.com" || len(sourceAfter.Cards) != 1 || sourceAfter.Cards[0].ID != "card-unused" {
		t.Fatalf("source registry did not remove alice entries: %+v", sourceAfter)
	}
	if len(targetAfter.ModelServiceGroups) == 0 || len(targetAfter.Grants) != 1 || targetAfter.Grants[0].ID != "grant-alice" || len(targetAfter.Cards) != 1 || targetAfter.Cards[0].ID != "card-alice" {
		t.Fatalf("target registry did not receive alice entries/base config: %+v", targetAfter)
	}
}

func TestTenantUserMigrationMovesIMBindings(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	settings := map[string]string{
		"dingtalk_bindings":    `{"staff-1":"alice@example.com","staff-2":"other@example.com"}`,
		"wecom_bindings":       `{"wecom-1":"{\"email\":\"alice@example.com\",\"tenant_id\":\"tenant_default\"}"}`,
		"qqbot_bindings":       `{"qq-1":"alice@example.com"}`,
		"im_telegram_bindings": `{"tg-1":"alice@example.com"}`,
		"feishu_openid_map":    `{"alice@example.com":"ou_alice","other@example.com":{"open_id":"ou_other","email":"other@example.com","tenant_id":"tenant_default"}}`,
	}
	for key, value := range settings {
		if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)`, key, value, now); err != nil {
			t.Fatalf("insert setting %s: %v", key, err)
		}
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID})
	if err != nil {
		t.Fatalf("migrate apply: %v", err)
	}
	for _, key := range []string{"im_bindings:dingtalk_bindings", "im_bindings:wecom_bindings", "im_bindings:qqbot_bindings", "im_bindings:im_telegram_bindings", "im_bindings:feishu_openid_map"} {
		if result.TableUpdates[key] != 1 {
			t.Fatalf("%s updates = %d, want 1; all=%+v", key, result.TableUpdates[key], result.TableUpdates)
		}
	}

	var raw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = 'dingtalk_bindings'`).Scan(&raw); err != nil {
		t.Fatalf("query dingtalk bindings: %v", err)
	}
	var dingtalk map[string]string
	if err := json.Unmarshal([]byte(raw), &dingtalk); err != nil {
		t.Fatalf("decode dingtalk bindings: %v", err)
	}
	info := decodeTenantMigrationBindingValue(dingtalk["staff-1"])
	if info.TenantID != "tenant_a" || info.Email != "alice@example.com" || dingtalk["staff-2"] != "other@example.com" {
		t.Fatalf("unexpected dingtalk bindings: %+v", dingtalk)
	}
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = 'im_telegram_bindings'`).Scan(&raw); err != nil {
		t.Fatalf("query remote bindings: %v", err)
	}
	var remote map[string]string
	if err := json.Unmarshal([]byte(raw), &remote); err != nil {
		t.Fatalf("decode remote bindings: %v", err)
	}
	remoteKey := tenantMigrationRemoteBindingKey("tenant_a", "tg-1")
	if _, ok := remote["tg-1"]; ok {
		t.Fatalf("legacy remote binding key should have been removed: %+v", remote)
	}
	remoteInfo := decodeTenantMigrationBindingValue(remote[remoteKey])
	if remoteInfo.TenantID != "tenant_a" || remoteInfo.Email != "alice@example.com" {
		t.Fatalf("unexpected remote bindings: %+v", remote)
	}
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = 'feishu_openid_map'`).Scan(&raw); err != nil {
		t.Fatalf("query feishu bindings: %v", err)
	}
	var feishu map[string]tenantMigrationFeishuBindingInfo
	if err := json.Unmarshal([]byte(raw), &feishu); err != nil {
		t.Fatalf("decode feishu bindings: %v", err)
	}
	moved := feishu[tenantMigrationFeishuKey("tenant_a", "alice@example.com")]
	if moved.OpenID != "ou_alice" || moved.TenantID != "tenant_a" || moved.Email != "alice@example.com" {
		t.Fatalf("unexpected moved feishu binding: %+v in %+v", moved, feishu)
	}
	if _, ok := feishu["alice@example.com"]; ok {
		t.Fatalf("legacy feishu binding key should have been removed: %+v", feishu)
	}
}

func TestTenantUserMigrationMovesWorkflowRuntime(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO workflow_definitions (id, tenant_id, owner_id, name, description, created_at, updated_at) VALUES ('wf-1', ?, 'user-1', 'Approval', '', ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert workflow definition: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, created_at, updated_at) VALUES ('ver-1', 'wf-1', '1', 'published', '{}', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert workflow version: %v", err)
	}
	instanceData := `{"initiator_id":"user-1","workflow_name":"Approval"}`
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO workflow_instances (id, tenant_id, workflow_id, version_id, status, instance_data, trigger_data, created_at) VALUES ('inst-1', ?, 'wf-1', 'ver-1', 'running', ?, '{}', ?)`, store.DefaultTenantID, instanceData, now); err != nil {
		t.Fatalf("insert workflow instance: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO confirmations (id, tenant_id, instance_id, recipient_id, type, status, notes, timeout_hours, reminders_sent, created_at) VALUES ('conf-1', ?, 'inst-1', 'user-1', 'executor', 'pending', '', 24, 0, ?)`, store.DefaultTenantID, now); err != nil {
		t.Fatalf("insert confirmation: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO approval_audit_trail (id, tenant_id, instance_id, event_type, actor_id, timestamp) VALUES ('audit-1', ?, 'inst-1', 'created', 'user-1', ?)`, store.DefaultTenantID, now); err != nil {
		t.Fatalf("insert audit trail: %v", err)
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID})
	if err != nil {
		t.Fatalf("migrate apply: %v", err)
	}
	for _, table := range []string{"workflow_instances", "confirmations", "approval_audit_trail"} {
		if result.TableUpdates[table] != 1 {
			t.Fatalf("%s updates = %d, want 1; all=%+v", table, result.TableUpdates[table], result.TableUpdates)
		}
		var count int
		if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = 'tenant_a'`).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s tenant_a count = %d, want 1", table, count)
		}
	}
	if _, err := provider.Write.ExecContext(ctx, `UPDATE approval_audit_trail SET event_type = 'changed' WHERE id = 'audit-1'`); err == nil {
		t.Fatalf("approval audit immutability trigger was not restored")
	}
}

func TestTenantUserMigrationMovesUserSystemSettings(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES ('shortcuts_user-1', '[{"name":"Open","cmd":"open"}]', ?)`, now); err != nil {
		t.Fatalf("insert shortcut setting: %v", err)
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID})
	if err != nil {
		t.Fatalf("migrate apply: %v", err)
	}
	if result.TableUpdates["system_settings:shortcuts_user-1"] != 1 {
		t.Fatalf("shortcut setting update = %d, all=%+v", result.TableUpdates["system_settings:shortcuts_user-1"], result.TableUpdates)
	}
	var targetRaw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = 'tenant:tenant_a:shortcuts_user-1'`).Scan(&targetRaw); err != nil {
		t.Fatalf("query target shortcut setting: %v", err)
	}
	if targetRaw == "" {
		t.Fatalf("target shortcut setting empty")
	}
	var sourceCount int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_settings WHERE key = 'shortcuts_user-1'`).Scan(&sourceCount); err != nil {
		t.Fatalf("query source shortcut setting: %v", err)
	}
	if sourceCount != 0 {
		t.Fatalf("source shortcut setting should be removed, count=%d", sourceCount)
	}
}

func TestTenantUserMigrationMovesA2AGroupState(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO machines (id, tenant_id, user_id, name, platform, machine_token_hash, status, created_at, updated_at) VALUES ('machine-1', ?, 'user-1', 'pc', 'windows', 'hash', 'offline', ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
	profileJSON := `{"agent_id":"machine-1","display_name":"Agent","tenant_id":"tenant_default"}`
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO a2a_group_profiles (tenant_id, agent_id, display_name, discoverable, available, updated_at, profile_json) VALUES (?, 'machine-1', 'Agent', 1, 1, ?, ?)`, store.DefaultTenantID, now, profileJSON); err != nil {
		t.Fatalf("insert a2a profile: %v", err)
	}
	sessionJSON := `{"id":"a2a-1","tenant_id":"tenant_default","participants":[{"id":"machine-1","role_code":"initiator"}],"messages":[{"from_id":"machine-1","to_ids":["machine-1"]}]}`
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO a2a_group_sessions (tenant_id, session_id, status, topic, created_at, updated_at, session_json) VALUES (?, 'a2a-1', 'open', 'topic', ?, ?, ?)`, store.DefaultTenantID, now, now, sessionJSON); err != nil {
		t.Fatalf("insert a2a session: %v", err)
	}
	inviteJSON := `{"id":"a2ainv-1","tenant_id":"tenant_default","session_id":"a2a-1","invite":{"from_id":"machine-1","to_id":"machine-2"},"status":"pending"}`
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO a2a_group_invites (tenant_id, invite_id, session_id, to_id, from_id, role, status, created_at, invite_json) VALUES (?, 'a2ainv-1', 'a2a-1', 'machine-2', 'machine-1', 'review', 'pending', ?, ?)`, store.DefaultTenantID, now, inviteJSON); err != nil {
		t.Fatalf("insert a2a invite: %v", err)
	}
	mapping := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(mapping, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	result, err := MigrateTenantUsers(ctx, provider.Write, TenantUserMigrationOptions{MappingPath: mapping, FromTenant: store.DefaultTenantID})
	if err != nil {
		t.Fatalf("migrate apply: %v", err)
	}
	for _, table := range []string{"a2a_group_profiles", "a2a_group_sessions", "a2a_group_invites"} {
		if result.TableUpdates[table] != 1 {
			t.Fatalf("%s updates = %d, want 1; all=%+v", table, result.TableUpdates[table], result.TableUpdates)
		}
		var targetCount, sourceCount int
		if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = 'tenant_a'`).Scan(&targetCount); err != nil {
			t.Fatalf("count target %s: %v", table, err)
		}
		if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = ?`, store.DefaultTenantID).Scan(&sourceCount); err != nil {
			t.Fatalf("count source %s: %v", table, err)
		}
		if targetCount != 1 || sourceCount != 0 {
			t.Fatalf("%s target/source counts = %d/%d, want 1/0", table, targetCount, sourceCount)
		}
	}
	var raw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT session_json FROM a2a_group_sessions WHERE tenant_id = 'tenant_a' AND session_id = 'a2a-1'`).Scan(&raw); err != nil {
		t.Fatalf("query moved session json: %v", err)
	}
	var moved map[string]any
	if err := json.Unmarshal([]byte(raw), &moved); err != nil {
		t.Fatalf("decode moved session json: %v", err)
	}
	if moved["tenant_id"] != "tenant_a" {
		t.Fatalf("moved session tenant_id = %v, want tenant_a; raw=%s", moved["tenant_id"], raw)
	}
}

func TestTenantUserMigrationRemoteBindingCollisionFails(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	remoteKey := tenantMigrationRemoteBindingKey("tenant_a", "tg-1")
	bindings := map[string]string{
		"tg-1":    "alice@example.com",
		remoteKey: `{"email":"bob@example.com","tenant_id":"tenant_a"}`,
	}
	raw, err := json.Marshal(bindings)
	if err != nil {
		t.Fatalf("marshal bindings: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES ('im_telegram_bindings', ?, ?)`, string(raw), now); err != nil {
		t.Fatalf("insert remote bindings: %v", err)
	}
	tx, err := provider.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	if _, err := moveRemoteGatewayBindingSetting(ctx, tx, "im_telegram_bindings", store.DefaultTenantID, "tenant_a", "alice@example.com"); err == nil {
		t.Fatal("expected remote binding collision error")
	}
}

func TestTenantUserMigrationRemotePlainKeyValueTenantIsHonored(t *testing.T) {
	provider := newTenantMigrationProvider(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	bindings := map[string]string{
		"tg-1": `{"email":"alice@example.com","tenant_id":"tenant_b"}`,
	}
	raw, err := json.Marshal(bindings)
	if err != nil {
		t.Fatalf("marshal bindings: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES ('im_telegram_bindings', ?, ?)`, string(raw), now); err != nil {
		t.Fatalf("insert remote bindings: %v", err)
	}
	tx, err := provider.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	if moved, err := moveRemoteGatewayBindingSetting(ctx, tx, "im_telegram_bindings", store.DefaultTenantID, "tenant_a", "alice@example.com"); err != nil || moved != 0 {
		t.Fatalf("moved = %d err=%v, want 0 nil", moved, err)
	}
}

func TestMergeTenantsMovesScopedRowsAndDeletesSource(t *testing.T) {
	ctx := context.Background()
	provider := newTenantMigrationProvider(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES (?, ?, ?, 'active', '{}', 'test', ?, ?)`, "tenant_a", "a", "Tenant A", now, now); err != nil {
		t.Fatalf("insert source tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES (?, ?, ?, 'active', '{}', 'test', ?, ?)`, "tenant_b", "b", "Tenant B", now, now); err != nil {
		t.Fatalf("insert target tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', 'approved', 0, ?, ?)`, "u-a", "tenant_a", "alice@example.com", "sn-a", now, now); err != nil {
		t.Fatalf("insert source user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', 'approved', 0, ?, ?)`, "u-b", "tenant_b", "bob@example.com", "sn-b", now, now); err != nil {
		t.Fatalf("insert target user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)`, "tenant:tenant_a:security_settings", `{"org_structure_enabled":true}`, now); err != nil {
		t.Fatalf("insert tenant setting: %v", err)
	}
	sourceRegistry := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "source-group", Name: "Source Group"}}, GlobalServiceGroupIDs: []string{"source-group"}}
	targetRegistry := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "target-group", Name: "Target Group"}}, GlobalServiceGroupIDs: []string{"target-group"}}
	sourceRegistryRaw, _ := json.Marshal(sourceRegistry)
	targetRegistryRaw, _ := json.Marshal(targetRegistry)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?), (?, ?, ?)`, tenantRegistryKey("tenant_a"), string(sourceRegistryRaw), now, tenantRegistryKey("tenant_b"), string(targetRegistryRaw), now); err != nil {
		t.Fatalf("insert llm registries: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO security_groups (tenant_id, id, name, parent_id, created_at, updated_at) VALUES (?, ?, ?, '', ?, ?), (?, ?, ?, '', ?, ?), (?, ?, ?, ?, ?, ?)`, "tenant_a", "root-a", "Root A", now, now, "tenant_b", "root-b", "Root B", now, now, "tenant_a", "child-a", "Child A", "root-a", now, now); err != nil {
		t.Fatalf("insert security groups: %v", err)
	}

	dry, err := MergeTenants(ctx, provider.Write, TenantMergeOptions{FromTenant: "tenant_a", ToTenant: "tenant_b", DryRun: true, DeleteSource: true})
	if err != nil {
		t.Fatalf("dry merge: %v", err)
	}
	if dry.Tables["users"].MovedRows != 1 || dry.SystemSettings.MovedKeys != 1 {
		t.Fatalf("unexpected dry result: %#v", dry)
	}
	var tenantAfterDry string
	if err := provider.Write.QueryRowContext(ctx, `SELECT tenant_id FROM users WHERE id = 'u-a'`).Scan(&tenantAfterDry); err != nil || tenantAfterDry != "tenant_a" {
		t.Fatalf("dry run changed source user tenant=%q err=%v", tenantAfterDry, err)
	}

	result, err := MergeTenants(ctx, provider.Write, TenantMergeOptions{FromTenant: "tenant_a", ToTenant: "tenant_b", DeleteSource: true})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Tables["users"].MovedRows != 1 || result.SystemSettings.MovedKeys != 1 || result.SystemSettings.MergedKeys != 1 {
		t.Fatalf("unexpected merge result: %#v", result)
	}
	var tenantID string
	if err := provider.Write.QueryRowContext(ctx, `SELECT tenant_id FROM users WHERE id = 'u-a'`).Scan(&tenantID); err != nil || tenantID != "tenant_b" {
		t.Fatalf("source user tenant=%q err=%v", tenantID, err)
	}
	var status string
	if err := provider.Write.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id = 'tenant_a'`).Scan(&status); err != nil || status != "deleted" {
		t.Fatalf("source tenant status=%q err=%v", status, err)
	}
	var targetSetting string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = 'tenant:tenant_b:security_settings'`).Scan(&targetSetting); err != nil || !strings.Contains(targetSetting, "org_structure_enabled") {
		t.Fatalf("target setting=%q err=%v", targetSetting, err)
	}
	var registryRaw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = ?`, tenantRegistryKey("tenant_b")).Scan(&registryRaw); err != nil {
		t.Fatalf("target registry missing: %v", err)
	}
	if !strings.Contains(registryRaw, "source-group") || !strings.Contains(registryRaw, "target-group") {
		t.Fatalf("target registry did not merge source and target groups: %s", registryRaw)
	}
	var sourceRegistryCount int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_settings WHERE key = ?`, tenantRegistryKey("tenant_a")).Scan(&sourceRegistryCount); err != nil || sourceRegistryCount != 0 {
		t.Fatalf("source registry count=%d err=%v", sourceRegistryCount, err)
	}
	var targetRootCount int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_groups WHERE tenant_id = 'tenant_b' AND parent_id = ''`).Scan(&targetRootCount); err != nil || targetRootCount != 1 {
		t.Fatalf("target root count=%d err=%v", targetRootCount, err)
	}
	var childParent string
	if err := provider.Write.QueryRowContext(ctx, `SELECT parent_id FROM security_groups WHERE id = 'child-a'`).Scan(&childParent); err != nil || childParent != "root-b" {
		t.Fatalf("child parent=%q err=%v", childParent, err)
	}
}

func TestMergeTenantsRejectsTenantScopedRowConflicts(t *testing.T) {
	ctx := context.Background()
	provider := newTenantMigrationProvider(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES (?, ?, ?, 'active', '{}', 'test', ?, ?)`, "tenant_a", "a", "Tenant A", now, now); err != nil {
		t.Fatalf("insert source tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES (?, ?, ?, 'active', '{}', 'test', ?, ?)`, "tenant_b", "b", "Tenant B", now, now); err != nil {
		t.Fatalf("insert target tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE tenant_merge_conflict_probe (tenant_id TEXT NOT NULL, item_key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY (tenant_id, item_key))`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenant_merge_conflict_probe (tenant_id, item_key, value) VALUES ('tenant_a', 'same-key', 'source'), ('tenant_b', 'same-key', 'target')`); err != nil {
		t.Fatalf("insert probe rows: %v", err)
	}

	_, err := MergeTenants(ctx, provider.Write, TenantMergeOptions{FromTenant: "tenant_a", ToTenant: "tenant_b", DeleteSource: true})
	if !errors.Is(err, ErrTenantMergeConflict) || !strings.Contains(err.Error(), "tenant_merge_conflict_probe") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	var sourceCount, targetCount int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenant_merge_conflict_probe WHERE tenant_id = 'tenant_a'`).Scan(&sourceCount); err != nil {
		t.Fatalf("count source rows: %v", err)
	}
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenant_merge_conflict_probe WHERE tenant_id = 'tenant_b'`).Scan(&targetCount); err != nil {
		t.Fatalf("count target rows: %v", err)
	}
	if sourceCount != 1 || targetCount != 1 {
		t.Fatalf("merge should not delete conflicting rows, source=%d target=%d", sourceCount, targetCount)
	}
}

func TestMergeTenantsFromDefaultPreservesDefaultTenantAndSettings(t *testing.T) {
	ctx := context.Background()
	provider := newTenantMigrationProvider(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES (?, ?, ?, 'active', '{}', 'test', ?, ?)`, "tenant_b", "b", "Tenant B", now, now); err != nil {
		t.Fatalf("insert target tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', 'approved', 0, ?, ?)`, "u-default", store.DefaultTenantID, "default@example.com", "sn-default", now, now); err != nil {
		t.Fatalf("insert default user: %v", err)
	}
	sourceRegistry := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "default-source", Name: "Default Source"}}, GlobalServiceGroupIDs: []string{"default-source"}}
	sourceRegistryRaw, _ := json.Marshal(sourceRegistry)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?), ('security_settings', ?, ?)`, llmservice.RegistryKey, string(sourceRegistryRaw), now, `{"org_structure_enabled":true}`, now); err != nil {
		t.Fatalf("insert default settings: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO security_groups (tenant_id, id, name, parent_id, created_at, updated_at) VALUES (?, ?, ?, '', ?, ?), (?, ?, ?, '', ?, ?), (?, ?, ?, ?, ?, ?)`, store.DefaultTenantID, "default-root", "Default Root", now, now, "tenant_b", "root-b", "Root B", now, now, store.DefaultTenantID, "default-child", "Default Child", "default-root", now, now); err != nil {
		t.Fatalf("insert security groups: %v", err)
	}

	result, err := MergeTenants(ctx, provider.Write, TenantMergeOptions{FromTenant: store.DefaultTenantID, ToTenant: "tenant_b", DeleteSource: true})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected default tenant delete warning, got %#v", result)
	}
	var defaultStatus string
	if err := provider.Write.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id = ?`, store.DefaultTenantID).Scan(&defaultStatus); err != nil || defaultStatus != "active" {
		t.Fatalf("default status=%q err=%v", defaultStatus, err)
	}
	var defaultRegistryRaw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = ?`, llmservice.RegistryKey).Scan(&defaultRegistryRaw); err != nil || !strings.Contains(defaultRegistryRaw, "default-source") {
		t.Fatalf("default registry=%q err=%v", defaultRegistryRaw, err)
	}
	var targetRegistryRaw string
	if err := provider.Write.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = ?`, tenantRegistryKey("tenant_b")).Scan(&targetRegistryRaw); err != nil || !strings.Contains(targetRegistryRaw, "default-source") {
		t.Fatalf("target registry=%q err=%v", targetRegistryRaw, err)
	}
	var defaultRootCount int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_groups WHERE tenant_id = ? AND parent_id = ''`, store.DefaultTenantID).Scan(&defaultRootCount); err != nil || defaultRootCount != 1 {
		t.Fatalf("default root count=%d err=%v", defaultRootCount, err)
	}
	var childParent string
	if err := provider.Write.QueryRowContext(ctx, `SELECT parent_id FROM security_groups WHERE id = 'default-child'`).Scan(&childParent); err != nil || childParent != "root-b" {
		t.Fatalf("child parent=%q err=%v", childParent, err)
	}
}
