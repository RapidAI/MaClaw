package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestListShareAudiencesUsesTheCallerLinksAndHubTenants(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-audiences.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := provider.Write.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	mustExec(`CREATE TABLE IF NOT EXISTS sm_users (
		id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	mustExec(`INSERT INTO sm_users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"user-1", "owner@example.test", now, now)
	mustExec(`INSERT INTO sm_users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"user-2", "other@example.test", now, now)
	mustExec(`INSERT INTO hub_instances (
		id, owner_email, name, base_url, visibility, enrollment_mode, hub_secret_hash,
		registration_policy_json, capabilities_json, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"hub-b", "owner@example.test", "Beta", "https://beta.example", "private", "invite", "hash",
		`{"tenants":{"ten-market":{"tenant_name":"市场"}}}`,
		`{"tenant_names":{"ten-dev":"研发"}}`,
		now, now)
	mustExec(`INSERT INTO hub_instances (
		id, owner_email, name, base_url, visibility, enrollment_mode, hub_secret_hash,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"hub-hidden", "other@example.test", "Hidden", "https://hidden.example", "private", "invite", "hash", now, now)
	mustExec(`INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)`,
		"link-1", "hub-b", "", "owner@example.test", now, now)
	// Same hub, different id casing, and no joined display name. The later
	// row that does join hub_instances must still supply the name.
	mustExec(`INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
		"link-case", "Hub-B", "", "owner@example.test", now, now)
	mustExec(`INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
		"link-2", "hub-a", "ten-solo", "Owner@Example.Test", now, now)
	mustExec(`INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
		"link-other", "hub-hidden", "ten-secret", "other@example.test", now, now)

	got, err := repo.ListShareAudiences(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListShareAudiences: %v", err)
	}
	if len(got.Hubs) != 2 || got.Hubs[0].ID != "hub-b" || got.Hubs[0].Name != "Beta" || got.Hubs[1].ID != "hub-a" {
		t.Fatalf("hubs = %+v, want the joined hub id Beta then the unnamed hub", got.Hubs)
	}
	byID := map[string]TokenBankAudienceChoice{}
	for _, item := range got.Tenants {
		byID[item.ID] = item
	}
	if byID["ten-market"].Name != "市场" || byID["ten-market"].HubID != "hub-b" {
		t.Fatalf("policy tenant = %+v", byID["ten-market"])
	}
	if byID["ten-dev"].Name != "研发" || byID["ten-dev"].HubName != "Beta" {
		t.Fatalf("capability tenant = %+v", byID["ten-dev"])
	}
	if byID["ten-solo"].ID != "ten-solo" || byID["ten-solo"].HubID != "hub-a" {
		t.Fatalf("link tenant = %+v", byID["ten-solo"])
	}
	if _, ok := byID["ten-secret"]; ok {
		t.Fatalf("another account's tenant was listed: %+v", got.Tenants)
	}
}

func TestListShareAudiencesDropsHubLabelWhenTenantSpansHubs(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-audiences-span.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := provider.Write.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	mustExec(`CREATE TABLE IF NOT EXISTS sm_users (
		id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	mustExec(`INSERT INTO sm_users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"user-1", "owner@example.test", now, now)
	for _, hub := range []struct{ id, name string }{{"hub-b", "Beta"}, {"hub-c", "Gamma"}} {
		mustExec(`INSERT INTO hub_instances (
			id, owner_email, name, base_url, visibility, enrollment_mode, hub_secret_hash,
			registration_policy_json, capabilities_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			hub.id, "owner@example.test", hub.name, "https://"+hub.id+".example", "private", "invite", "hash",
			`{"tenants":{"ten-market":{"tenant_name":"市场"}}}`, `{}`, now, now)
		mustExec(`INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
			"link-"+hub.id, hub.id, "", "owner@example.test", now, now)
	}

	got, err := repo.ListShareAudiences(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListShareAudiences: %v", err)
	}
	if len(got.Hubs) != 2 {
		t.Fatalf("hubs = %+v, want both hubs", got.Hubs)
	}
	if len(got.Tenants) != 1 {
		t.Fatalf("tenants = %+v, want one shared tenant", got.Tenants)
	}
	tenant := got.Tenants[0]
	if tenant.ID != "ten-market" || tenant.Name != "市场" || tenant.HubID != "" || tenant.HubName != "" {
		t.Fatalf("spanned tenant = %+v, want the name kept and the single-hub label cleared", tenant)
	}
}
