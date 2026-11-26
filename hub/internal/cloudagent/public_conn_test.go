package cloudagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The connection card accepts whatever the admin registered — a co-located
// loopback srv or a remote http(s) address — and carries the admin secret
// to it the same way a registered srv link does. Addresses botmgmt's save
// path would never accept fail closed here too.
func TestPublicConnAcceptsRegisteredAddress(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"loopback http", "http://127.0.0.1:18080", false},
		{"remote https", "https://maclawsrv.example", false},
		{"remote http (intranet)", "http://maclawsrv.intranet:18080", false},
		{"empty", "", true},
		{"not a url", "not a url", true},
		{"wrong scheme", "ftp://maclawsrv.example", true},
		{"user info", "https://user:pass@maclawsrv.example", true},
		{"tunnel host", "http://srv.node-7.node.invalid", true},
	}
	for _, tc := range cases {
		rec := srvRecord{baseURL: tc.baseURL, adminSecret: "sec"}
		conn, err := rec.publicConn()
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got conn %+v", tc.name, conn)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if conn.AdminSecret != "sec" || conn.BaseURL != tc.baseURL {
			t.Fatalf("%s: conn = %+v", tc.name, conn)
		}
	}
}

// An instance recorded on a registered srv link reports the link's
// reachability — nodeConn only knows edge-tunnel links, so resolution
// must go through linkConn or every registered-link instance reads
// "unreachable" while dispatch works fine.
func TestRuntimeStatusResolvesRegisteredSrvLink(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer runtime.Close()

	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{
			"base_url": "http://127.0.0.1:9", "admin_secret": "pub-sec",
			"srv_links": [{"id": "srv-a", "base_url": "` + runtime.URL + `", "admin_secret": "a-sec"}]
		}`,
	}})
	ctx := context.Background()

	if got := svc.runtimeStatus(ctx, Instance{ID: "cag_1", TenantID: "tenant-a", SrvNodeID: "srv-a"}); got != "ready" {
		t.Fatalf("srv-a status = %s", got)
	}
	if got := svc.runtimeStatus(ctx, Instance{ID: "cag_2", TenantID: "tenant-a", SrvNodeID: "srv-gone"}); got != "unreachable" {
		t.Fatalf("unknown link status = %s", got)
	}
	// The public connection is dialed directly: the dead port is unreachable.
	if got := svc.runtimeStatus(ctx, Instance{ID: "cag_3", TenantID: "tenant-a"}); got != "unreachable" {
		t.Fatalf("public status = %s", got)
	}
}
