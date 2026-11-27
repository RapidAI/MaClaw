package cloudagent

import (
	"context"
	"testing"
)

// srvConn pin chain: user-scope row → owner's own node pin → department
// chain → global row → public connection.
func TestSrvConnScopePinChain(t *testing.T) {
	record := `{
		"base_url": "http://127.0.0.1:9",
		"admin_secret": "pub-sec",
		"owner_cloud_node": {"user-2": "node-1"},
		"node_links": [{"node_id": "node-1", "base_url": "http://n1.internal", "admin_secret": "n1-sec"}],
		"srv_links": [
			{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"},
			{"id": "srv-b", "base_url": "https://b.example.com", "admin_secret": "b-sec"}
		]
	}`
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): record,
	}})
	svc.Directory = &fakeDirectory{
		email:  map[string]string{"user-1": "a@example.com", "user-2": "b@example.com"},
		group:  map[string]string{"a@example.com": "leaf", "b@example.com": "leaf"},
		parent: map[string]string{"leaf": ""},
	}
	ctx := context.Background()

	connFor := func(user string) srvConn {
		t.Helper()
		conn, err := svc.srvConn(ctx, "tenant-a", user)
		if err != nil {
			t.Fatalf("srvConn(%s): %v", user, err)
		}
		return conn
	}
	setSrv := func(scope, target, srv string) {
		t.Helper()
		view := AccessView{Scope: scope, TargetID: target, Enabled: AccessOn, SrvID: &srv}
		if scope == ScopeGlobal {
			three, one := 3, 1
			view.MaxInstances, view.MaxConcurrentTurns = &three, &one
		}
		if _, _, err := svc.SaveAccess(ctx, "tenant-a", view); err != nil {
			t.Fatalf("SaveAccess %s/%s: %v", scope, target, err)
		}
	}

	// No pins anywhere → public connection.
	if conn := connFor("user-1"); conn.LinkID != "" || conn.BaseURL != "http://127.0.0.1:9" || conn.AdminSecret != "pub-sec" {
		t.Fatalf("public conn = %+v", conn)
	}

	// Global pin picks the registered srv.
	setSrv(ScopeGlobal, "", "srv-a")
	if conn := connFor("user-1"); conn.LinkID != "srv-a" || conn.BaseURL != "https://a.example.com" || conn.AdminSecret != "a-sec" {
		t.Fatalf("global srv-a conn = %+v", conn)
	}

	// Department pin beats global.
	setSrv(ScopeDepartment, "leaf", "srv-b")
	if conn := connFor("user-1"); conn.LinkID != "srv-b" {
		t.Fatalf("dept srv-b conn = %+v", conn)
	}

	// The owner's own node pin beats department+global for user-2.
	if conn := connFor("user-2"); conn.LinkID != "node-1" || conn.NodeID != "node-1" || conn.AdminSecret != "n1-sec" {
		t.Fatalf("self-pin node conn = %+v", conn)
	}

	// A user-scope row is the narrowest: it beats the owner's own pin.
	setSrv(ScopeUser, "user-2", "srv-a")
	if conn := connFor("user-2"); conn.LinkID != "srv-a" || conn.NodeID != "" {
		t.Fatalf("user-scope conn = %+v", conn)
	}

	// An explicit empty pin on the user row is the public default — it
	// still wins the chain over the owner's own node pin.
	empty := ""
	if _, _, err := svc.SaveAccess(ctx, "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-2", Enabled: AccessOn, SrvID: &empty,
	}); err != nil {
		t.Fatal(err)
	}
	if conn := connFor("user-2"); conn.LinkID != "" || conn.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("explicit-default conn = %+v", conn)
	}
}

// SaveAccess refuses a pin that names no registered srv — a typo cannot
// silently land agents on the public connection.
func TestSaveAccessRejectsUnregisteredSrvPin(t *testing.T) {
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec"}`,
	}})
	pin := "srv-gone"
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-1", Enabled: AccessOn, SrvID: &pin,
	}); err == nil {
		t.Fatal("unregistered pin should be rejected")
	}
}

// A pin saved while the link existed fails closed once the link is gone —
// it never silently falls back to the public connection. Same for a link
// whose admin secret was never saved.
func TestSrvConnMissingLinkFailsClosed(t *testing.T) {
	settings := memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{
			"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec",
			"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"}]
		}`,
	}}
	svc := New(openTestDB(t), settings)
	pin := "srv-a"
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-1", Enabled: AccessOn, SrvID: &pin,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.srvConn(context.Background(), "tenant-a", "user-1"); err != nil {
		t.Fatalf("registered link should resolve: %v", err)
	}

	settings.kv[SrvSettingsKey("tenant-a")] = `{
		"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec",
		"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com"}]
	}`
	if _, err := svc.srvConn(context.Background(), "tenant-a", "user-1"); err == nil {
		t.Fatal("link without an admin secret should fail closed")
	}
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec"}`
	if _, err := svc.srvConn(context.Background(), "tenant-a", "user-1"); err == nil {
		t.Fatal("deleted link should fail closed")
	}
}

// srvConnForNode resolves the recorded link on an existing instance —
// registered srv ids and node ids both land there.
func TestSrvConnForNodeResolvesRegisteredLink(t *testing.T) {
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{
			"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec",
			"node_links": [{"node_id": "node-1", "base_url": "http://n1.internal", "admin_secret": "n1-sec"}],
			"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"}]
		}`,
	}})
	conn, err := svc.srvConnForNode(context.Background(), "tenant-a", "srv-a")
	if err != nil || conn.LinkID != "srv-a" || conn.NodeID != "" || conn.AdminSecret != "a-sec" {
		t.Fatalf("srv-a conn = %+v err=%v", conn, err)
	}
	conn, err = svc.srvConnForNode(context.Background(), "tenant-a", "node-1")
	if err != nil || conn.NodeID != "node-1" {
		t.Fatalf("node conn = %+v err=%v", conn, err)
	}
	conn, err = svc.srvConnForNode(context.Background(), "tenant-a", "")
	if err != nil || conn.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("public conn = %+v err=%v", conn, err)
	}
}

// With exactly one usable endpoint — a registered link and no usable
// public connection — that link is the implicit default for every
// unpinned scope and for an explicit ” pin. Two usable endpoints keep
// the public connection: ambiguity needs an explicit pin.
func TestSrvConnSingleUsableSrvIsTheDefault(t *testing.T) {
	settings := memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{
			"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"}]
		}`,
	}}
	svc := New(openTestDB(t), settings)
	conn, err := svc.srvConn(context.Background(), "tenant-a", "user-1")
	if err != nil || conn.LinkID != "srv-a" || conn.AdminSecret != "a-sec" {
		t.Fatalf("single-srv default = %+v err=%v", conn, err)
	}

	// An explicit '' pin asks for the effective default: the one srv.
	empty := ""
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-1", Enabled: AccessOn, SrvID: &empty,
	}); err != nil {
		t.Fatal(err)
	}
	conn, err = svc.srvConn(context.Background(), "tenant-a", "user-1")
	if err != nil || conn.LinkID != "srv-a" {
		t.Fatalf("'' pin conn = %+v err=%v", conn, err)
	}

	// A configured-but-unusable public connection does not count: the one
	// usable link is still the default.
	settings.kv[SrvSettingsKey("tenant-a")] = `{
		"base_url": "http://10.0.0.9",
		"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"}]
	}`
	conn, err = svc.srvConn(context.Background(), "tenant-a", "user-1")
	if err != nil || conn.LinkID != "srv-a" {
		t.Fatalf("unusable-public default = %+v err=%v", conn, err)
	}

	// A second usable endpoint reinstates the public default: the admin
	// has to pin explicitly. The registered address counts the same way
	// loopback does — a remote https card is a full default candidate.
	settings.kv[SrvSettingsKey("tenant-a")] = `{
		"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec",
		"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"}]
	}`
	conn, err = svc.srvConn(context.Background(), "tenant-a", "user-1")
	if err != nil || conn.LinkID != "" || conn.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("two-endpoint default = %+v err=%v", conn, err)
	}
	settings.kv[SrvSettingsKey("tenant-a")] = `{
		"base_url": "https://maclawsrv.example", "admin_secret": "pub-sec",
		"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"}]
	}`
	conn, err = svc.srvConn(context.Background(), "tenant-a", "user-1")
	if err != nil || conn.LinkID != "" || conn.BaseURL != "https://maclawsrv.example" {
		t.Fatalf("remote-card default = %+v err=%v", conn, err)
	}
}

// An instance recorded on the public connection stays there even when the
// effective default later becomes a registered link — pins resolve at
// provision time, they do not move live instances.
func TestSrvConnForNodeKeepsRecordedDefault(t *testing.T) {
	settings := memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{
			"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec",
			"srv_links": [{"id": "srv-a", "base_url": "https://a.example.com", "admin_secret": "a-sec"}]
		}`,
	}}
	svc := New(openTestDB(t), settings)
	conn, err := svc.srvConnForNode(context.Background(), "tenant-a", "")
	if err != nil || conn.LinkID != "" || conn.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("recorded-default conn = %+v err=%v", conn, err)
	}
}
