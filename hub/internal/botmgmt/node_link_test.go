package botmgmt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicAndNodeBotsUseDifferentSrvConnections(t *testing.T) {
	var publicHits, nodeHits []string
	publicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHits = append(publicHits, r.Method+" "+r.URL.Path)
		writeNodeStub(w, r, "public-token")
	}))
	defer publicSrv.Close()
	nodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeHits = append(nodeHits, r.Method+" "+r.URL.Path)
		writeNodeStub(w, r, "node-token")
	}))
	defer nodeSrv.Close()

	svc, settings := newStubService(publicSrv)
	useDirectNodeDial(svc)
	seedRecord(t, settings, "tenant-a", record{
		BaseURL:     publicSrv.URL,
		AccessToken: "public-access",
		AdminSecret: "public-admin",
		Grants:      []Grant{{ID: "g-a", Scope: ScopeUser, TargetID: "alice"}, {ID: "g-b", Scope: ScopeUser, TargetID: "bob"}},
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "t1", UserID: "u-alice", APIKey: "ak-alice", APISecret: "sk-alice",
		}},
		NodeLinks: []nodeLink{{
			NodeID: "node-1", BaseURL: nodeSrv.URL, AdminSecret: "node-admin",
			Principals: []ownerPrincipal{{
				HubUserID: "bob", TenantID: "t1", UserID: "u-bob", APIKey: "ak-bob", APISecret: "sk-bob",
			}},
		}},
		OwnerNode: map[string]string{"bob": "node-1"},
	})

	alice, err := svc.CreateBotForUser(context.Background(), "tenant-a", "alice", "public bot", "")
	if err != nil {
		t.Fatal(err)
	}
	if alice.NodeID != "" || alice.InstanceID != "inst-public-token" {
		t.Fatalf("alice = %+v", alice)
	}
	bob, err := svc.CreateBotForUser(context.Background(), "tenant-a", "bob", "intranet bot", "")
	if err != nil {
		t.Fatal(err)
	}
	if bob.NodeID != "node-1" || bob.InstanceID != "inst-node-token" {
		t.Fatalf("bob = %+v", bob)
	}
	if !containsHit(publicHits, "POST /api/v1/instances") {
		t.Fatalf("public srv did not create alice: %v", publicHits)
	}
	if !containsHit(nodeHits, "POST /api/v1/instances") {
		t.Fatalf("node srv did not create bob: %v", nodeHits)
	}
	if containsHit(publicHits, "POST /api/v1/instances") && hitCount(nodeHits, "POST /api/v1/instances") != 1 {
		t.Fatalf("node instance posts = %d %v", hitCount(nodeHits, "POST /api/v1/instances"), nodeHits)
	}
	if hitCount(publicHits, "POST /api/v1/instances") != 1 {
		t.Fatalf("public instance posts = %d %v", hitCount(publicHits, "POST /api/v1/instances"), publicHits)
	}

	stored := storedRecord(t, settings, "tenant-a")
	if stored.BaseURL != publicSrv.URL || stored.AccessToken != "public-access" {
		t.Fatalf("public connection changed: %+v", stored)
	}
	if len(stored.NodeLinks) != 1 || stored.NodeLinks[0].BaseURL != nodeSrv.URL {
		t.Fatalf("node link = %+v", stored.NodeLinks)
	}
	if stored.OwnerNode["bob"] != "node-1" || stored.OwnerNode["alice"] != "" {
		t.Fatalf("pins = %+v", stored.OwnerNode)
	}
}

func TestCreateBotUsesTheServerTheUserPicked(t *testing.T) {
	var publicHits, nodeHits []string
	publicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHits = append(publicHits, r.Method+" "+r.URL.Path)
		writeNodeStub(w, r, "public-token")
	}))
	defer publicSrv.Close()
	nodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeHits = append(nodeHits, r.Method+" "+r.URL.Path)
		writeNodeStub(w, r, "node-token")
	}))
	defer nodeSrv.Close()

	svc, settings := newStubService(publicSrv)
	useDirectNodeDial(svc)
	alice := ownerPrincipal{HubUserID: "alice", TenantID: "t1", UserID: "u-alice", APIKey: "ak-alice", APISecret: "sk-alice"}
	seedRecord(t, settings, "tenant-a", record{
		BaseURL: publicSrv.URL, AccessToken: "public-access", AdminSecret: "public-admin",
		Grants:     []Grant{{ID: "g-a", Scope: ScopeUser, TargetID: "alice"}},
		Principals: []ownerPrincipal{alice},
		NodeLinks: []nodeLink{{
			NodeID: "node-1", BaseURL: nodeSrv.URL, AdminSecret: "node-admin",
			Principals: []ownerPrincipal{alice},
		}},
		OwnerNode: map[string]string{"alice": "node-1"},
	})
	ctx := context.Background()
	yes := true
	cloud, err := svc.CreateBotForUserOnNode(ctx, "tenant-a", "alice", "cloud bot", "", &yes, "")
	if err != nil {
		t.Fatal(err)
	}
	if cloud.NodeID != "" || cloud.InstanceID != "inst-public-token" {
		t.Fatalf("cloud = %+v", cloud)
	}
	local, err := svc.CreateBotForUserOnNode(ctx, "tenant-a", "alice", "local bot", "", &yes, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if local.NodeID != "node-1" || local.InstanceID != "inst-node-token" {
		t.Fatalf("local = %+v", local)
	}
	pinned, err := svc.CreateBotForUser(ctx, "tenant-a", "alice", "pinned bot", "")
	if err != nil {
		t.Fatal(err)
	}
	if pinned.NodeID != "node-1" || pinned.InstanceID != "inst-node-token" {
		t.Fatalf("pinned = %+v", pinned)
	}
	if hitCount(publicHits, "POST /api/v1/instances") != 1 || hitCount(nodeHits, "POST /api/v1/instances") != 2 {
		t.Fatalf("public %v node %v", publicHits, nodeHits)
	}
}

func TestPinOwnerNodeRefusesAServerWithoutSrv(t *testing.T) {
	settings := &memSettings{}
	svc := NewService(settings)
	seedRecord(t, settings, "tenant-a", record{
		OwnerNode: map[string]string{"alice": "node-old"},
		NodeLinks: []nodeLink{{NodeID: "node-old", BaseURL: "http://srv.node-old.node.invalid", AdminSecret: "secret"}},
	})
	err := svc.PinOwnerNode(context.Background(), "tenant-a", "alice", "node-new")
	if !errors.Is(err, ErrSrv) {
		t.Fatalf("err = %v", err)
	}
	stored := storedRecord(t, settings, "tenant-a")
	if stored.OwnerNode["alice"] != "node-old" {
		t.Fatalf("pin = %+v", stored.OwnerNode)
	}
}

func TestNodeOnlyTenantCreatesOnTheNodeSrv(t *testing.T) {
	var hits []string
	nodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		writeNodeStub(w, r, "node-token")
	}))
	defer nodeSrv.Close()
	svc, settings := newStubService(nodeSrv)
	useDirectNodeDial(svc)
	seedRecord(t, settings, "tenant-a", record{
		Grants: []Grant{{ID: "g-a", Scope: ScopeUser, TargetID: "alice"}, {ID: "g-b", Scope: ScopeUser, TargetID: "bob"}},
		NodeLinks: []nodeLink{{
			NodeID: "node-1", BaseURL: nodeSrv.URL, AdminSecret: "node-admin",
			Principals: []ownerPrincipal{{
				HubUserID: "bob", TenantID: "t1", UserID: "u-bob", APIKey: "ak-bob", APISecret: "sk-bob",
			}},
		}},
		OwnerNode: map[string]string{"bob": "node-1"},
	})
	if _, err := svc.CreateBotForUser(context.Background(), "tenant-a", "alice", "public bot", ""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unpinned user err = %v", err)
	}
	bob, err := svc.CreateBotForUser(context.Background(), "tenant-a", "bob", "intranet bot", "")
	if err != nil {
		t.Fatal(err)
	}
	if bob.NodeID != "node-1" || bob.InstanceID != "inst-node-token" {
		t.Fatalf("bob = %+v", bob)
	}
	if !containsHit(hits, "POST /api/v1/instances") {
		t.Fatalf("node srv was not used: %v", hits)
	}
	stored := storedRecord(t, settings, "tenant-a")
	if stored.BaseURL != "" || stored.AccessToken != "" {
		t.Fatalf("public connection was invented: %+v", stored)
	}
}

func TestSavingANodeCallDoesNotReplaceThePublicConnection(t *testing.T) {
	idle := httptest.NewServer(http.NotFoundHandler())
	defer idle.Close()
	svc, settings := newStubService(idle)
	seedRecord(t, settings, "tenant-a", record{
		BaseURL: "http://public.example", AccessToken: "public-access", AdminSecret: "public-admin",
		NodeLinks: []nodeLink{{NodeID: "node-1", BaseURL: "http://node.example", AdminSecret: "node-admin"}},
	})
	rec, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	view, ok := nodeView(rec, "node-1")
	if !ok {
		t.Fatal("node view missing")
	}
	view.Principals = []ownerPrincipal{{HubUserID: "bob", UserID: "u-bob", APIKey: "ak", APISecret: "sk"}}
	if err := svc.save(context.Background(), "tenant-a", view); err != nil {
		t.Fatal(err)
	}
	stored := storedRecord(t, settings, "tenant-a")
	if stored.BaseURL != "http://public.example" || stored.AccessToken != "public-access" || stored.AdminSecret != "public-admin" {
		t.Fatalf("public connection = %+v", stored)
	}
	if stored.DialNode != "" || len(stored.NodeLinks) != 1 || len(stored.NodeLinks[0].Principals) != 1 {
		t.Fatalf("node link was not folded: %+v", stored.NodeLinks)
	}
}

func TestNodeSrvGetsItsOwnConnectionCredential(t *testing.T) {
	var hits []string
	var admin string
	nodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		admin = r.Header.Get("X-MaClaw-Admin-Secret")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/tenants":
			_, _ = io.WriteString(w, `{"items":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/tenants":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"maclaw-tenant"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"user-hub"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users/user-hub/credentials":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"cred-1","api_key":"node-key","api_secret":"node-secret"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer nodeSrv.Close()
	svc, settings := newStubService(nodeSrv)
	seedRecord(t, settings, "tenant-a", record{
		BaseURL: "http://public.example", AccessToken: "public-access", AdminSecret: "public-admin",
		NodeLinks: []nodeLink{{NodeID: "node-1", BaseURL: nodeSrv.URL, AdminSecret: "node-admin"}},
	})
	if err := svc.ProvisionNodeConnection(context.Background(), "tenant-a", "node-1"); err != nil {
		t.Fatal(err)
	}
	if admin != "node-admin" {
		t.Fatalf("admin header = %q", admin)
	}
	stored := storedRecord(t, settings, "tenant-a")
	if stored.BaseURL != "http://public.example" || stored.AccessToken != "public-access" || stored.AdminSecret != "public-admin" || stored.Connection != nil {
		t.Fatalf("public connection changed: %+v", stored)
	}
	link := stored.NodeLinks[0]
	if link.MaClawTenantID != "maclaw-tenant" || link.Connection == nil || link.Connection.APIKey != "node-key" || link.Connection.APISecret != "node-secret" {
		t.Fatalf("node credential = %+v", link)
	}
	before := len(hits)
	if err := svc.ProvisionNodeConnection(context.Background(), "tenant-a", "node-1"); err != nil {
		t.Fatal(err)
	}
	if len(hits) != before {
		t.Fatalf("second provision called the node again: %v", hits[before:])
	}
}

// useDirectNodeDial lets a unit test reach an httptest srv by its real
// address. Production node calls go through the tunnel client instead.
func useDirectNodeDial(svc *Service) {
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Timeout: 15 * time.Second}
	}
}

func writeNodeStub(w http.ResponseWriter, r *http.Request, token string) {
	switch r.URL.Path {
	case "/api/v1/auth/token":
		_, _ = io.WriteString(w, `{"access_token":"`+token+`"}`)
	case "/api/v1/instances":
		if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != token {
			http.Error(w, "wrong srv", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"inst-`+token+`"}`)
	default:
		http.NotFound(w, r)
	}
}

func containsHit(hits []string, want string) bool {
	return hitCount(hits, want) > 0
}

func hitCount(hits []string, want string) int {
	n := 0
	for _, hit := range hits {
		if hit == want {
			n++
		}
	}
	return n
}
