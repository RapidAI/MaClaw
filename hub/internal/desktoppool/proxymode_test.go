package desktoppool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type proxyModeHost struct {
	mu    sync.Mutex
	calls []map[string]string
	paths []string
}

func newProxyModeHost(t *testing.T) (*proxyModeHost, *httptest.Server) {
	host := &proxyModeHost{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		host.mu.Lock()
		host.calls = append(host.calls, body)
		host.paths = append(host.paths, r.URL.Path)
		host.mu.Unlock()
		switch r.URL.Path {
		case "/v1/desktops":
			_, _ = w.Write([]byte(`{"container":"c","status":"running"}`))
		default:
			_, _ = w.Write([]byte(`{"mode":"` + body["mode"] + `","changed":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	return host, srv
}

func assignedPool(t *testing.T, url string) *Pool {
	t.Helper()
	pool := New(&memSettings{}, memDir{})
	view, err := pool.CreateServer(context.Background(), "tenant-a", Server{Name: "h", BaseURL: url, AccessToken: "tok", Memory: "3g", CPUs: "1.5", ShmSize: "1g"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestProxyModeDefaultsToSmart(t *testing.T) {
	_, srv := newProxyModeHost(t)
	pool := assignedPool(t, srv.URL)
	view, err := pool.ProxyMode(context.Background(), "tenant-a", "alice")
	if err != nil || view.Mode != ProxyModeSmart || view.UserMode != "" || view.Locked {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

func TestSetUserProxyModePushesToDesktopd(t *testing.T) {
	host, srv := newProxyModeHost(t)
	pool := assignedPool(t, srv.URL)
	view, err := pool.SetUserProxyMode(context.Background(), "tenant-a", "alice", "direct")
	if err != nil || view.Mode != ProxyModeDirect || !view.Applied {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if len(host.paths) != 1 || host.paths[0] != "/v1/desktops/proxy-mode" || host.calls[0]["mode"] != "direct" || host.calls[0]["user_id"] != "alice" {
		t.Fatalf("push = %v %v", host.paths, host.calls)
	}
	// Create carries the effective mode so a restarted host restores it.
	if _, err := pool.CreateDesktop(context.Background(), "tenant-a", "alice"); err != nil {
		t.Fatal(err)
	}
	if got := host.calls[len(host.calls)-1]["proxy_mode"]; got != "direct" {
		t.Fatalf("create proxy_mode = %q", got)
	}
	if _, err := pool.SetUserProxyMode(context.Background(), "tenant-a", "alice", "vpn"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid mode err = %v", err)
	}
	// "" falls back to the tenant default.
	view, _ = pool.SetUserProxyMode(context.Background(), "tenant-a", "alice", "")
	if view.Mode != ProxyModeSmart || view.UserMode != "" {
		t.Fatalf("inherit view=%+v", view)
	}
}

func TestTenantLockOverridesUserMode(t *testing.T) {
	_, srv := newProxyModeHost(t)
	pool := assignedPool(t, srv.URL)
	ctx := context.Background()
	if _, err := pool.SetUserProxyMode(ctx, "tenant-a", "alice", "direct"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.SetTenantProxyModePolicy(ctx, "tenant-a", ProxyModePolicy{Default: "always", Locked: true}); err != nil {
		t.Fatal(err)
	}
	view, err := pool.ProxyMode(ctx, "tenant-a", "alice")
	if err != nil || view.Mode != ProxyModeProxy || !view.Locked {
		t.Fatalf("locked view=%+v err=%v", view, err)
	}
	if _, err := pool.SetUserProxyMode(ctx, "tenant-a", "alice", "smart"); !errors.Is(err, ErrProxyModeLocked) {
		t.Fatalf("locked set err = %v", err)
	}
	// Unlocked: the tenant default applies to users without their own mode,
	// and alice's stored choice comes back.
	if _, err := pool.SetTenantProxyModePolicy(ctx, "tenant-a", ProxyModePolicy{Default: "proxy"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := pool.ProxyMode(ctx, "tenant-a", "bob"); v.Mode != ProxyModeProxy {
		t.Fatalf("bob view=%+v", v)
	}
	if v, _ := pool.ProxyMode(ctx, "tenant-a", "alice"); v.Mode != ProxyModeDirect {
		t.Fatalf("alice view=%+v", v)
	}
}

func TestSetUserProxyModeWithoutAssignmentStillSaves(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	view, err := pool.SetUserProxyMode(context.Background(), "tenant-a", "alice", "proxy")
	if err != nil || view.Mode != ProxyModeProxy || !view.Applied {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}
