package desktoppool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (m *memSettings) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.m[key]
	if !ok {
		return "", io.EOF
	}
	return value, nil
}

func (m *memSettings) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = map[string]string{}
	}
	m.m[key] = value
	return nil
}

type memDir struct {
	email   map[string]string
	group   map[string]string
	parents map[string]string
}

func (d memDir) Email(_ context.Context, userID string) (string, error) { return d.email[userID], nil }
func (d memDir) GroupID(_ context.Context, email string) (string, error) {
	return d.group[email], nil
}
func (d memDir) ParentID(_ context.Context, groupID string) (string, error) {
	return d.parents[groupID], nil
}

func TestUserDepartmentAndGlobalPickDifferentDockerHosts(t *testing.T) {
	seen := map[string]string{}
	newServer := func(name, memory string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+name+"-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var body struct {
				UserID string `json:"user_id"`
				Memory string `json:"memory"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			seen[body.UserID] = name + ":" + body.Memory
			if r.URL.Path == "/v1/desktops/session" {
				_, _ = w.Write([]byte(`{"cdp_url":"http://` + name + `.example:19020","display":":20"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"running","memory":"` + body.Memory + `"}`))
		}))
	}
	globalSrv := newServer("global", "2500m")
	deptSrv := newServer("dept", "4g")
	userSrv := newServer("user", "8g")
	defer globalSrv.Close()
	defer deptSrv.Close()
	defer userSrv.Close()

	pool := New(&memSettings{}, memDir{
		email:   map[string]string{"bob": "bob@example.com", "carol": "carol@example.com"},
		group:   map[string]string{"bob@example.com": "eng", "carol@example.com": "eng-child"},
		parents: map[string]string{"eng-child": "eng"},
	})
	ctx := context.Background()
	add := func(name, rawURL, memory string) string {
		view, err := pool.CreateServer(ctx, "tenant-a", Server{
			Name: name, BaseURL: rawURL, AccessToken: name + "-token", Memory: memory, CPUs: "1.5", ShmSize: "512m",
		})
		if err != nil {
			t.Fatal(err)
		}
		return view.ID
	}
	globalID := add("global", globalSrv.URL, "2500m")
	deptID := add("dept", deptSrv.URL, "4g")
	userID := add("user", userSrv.URL, "8g")
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: globalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeDepartment, TargetID: "eng", ServerID: deptID}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeUser, TargetID: "alice", ServerID: userID}); err != nil {
		t.Fatal(err)
	}
	view, err := pool.View(ctx, "tenant-a")
	if err != nil || strings.Contains(mustJSON(view), "token") && strings.Contains(mustJSON(view), "global-token") {
		t.Fatalf("token leaked or view failed: %s %v", mustJSON(view), err)
	}

	cdp, _, err := pool.OpenSession(ctx, "tenant-a", "alice")
	if err != nil || cdp != "http://user.example:19020" || seen["alice"] != "user:8g" {
		t.Fatalf("alice cdp=%s seen=%v err=%v", cdp, seen, err)
	}
	if _, _, err := pool.OpenSession(ctx, "tenant-a", "carol"); err != nil || seen["carol"] != "dept:4g" {
		t.Fatalf("carol seen=%v err=%v", seen, err)
	}
	if _, _, err := pool.OpenSession(ctx, "tenant-a", "dave"); err != nil || seen["dave"] != "global:2500m" {
		t.Fatalf("dave seen=%v err=%v", seen, err)
	}
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestAuthorizedUsersListsOnlyAssignedUsers(t *testing.T) {
	candidates := testPoolCandidates()
	assignments := func(global bool) []Assignment {
		if global {
			return []Assignment{{Scope: ScopeGlobal}}
		}
		return []Assignment{
			{Scope: ScopeDepartment, TargetID: "eng"},
			{Scope: ScopeUser, TargetID: "alice"},
		}
	}
	// Without a global assignment the list keeps to the named user and the
	// users the department chain (child department included) reaches.
	pool := newCandidatePool(t, assignments(false))
	got := pool.AuthorizedUsers(context.Background(), "tenant-a", candidates)
	want := []AuthorizedUser{
		{ID: "alice", Label: "alice@example.com"},
		{ID: "bob", Label: "bob@example.com"},
		{ID: "carol", Label: "carol@example.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("department+user assignments authorized=%s want=%s", mustJSON(got), mustJSON(want))
	}
	// A global assignment covers everyone, whoever their department is.
	pool = newCandidatePool(t, assignments(true))
	got = pool.AuthorizedUsers(context.Background(), "tenant-a", candidates)
	want = append([]AuthorizedUser{}, candidates...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global assignment authorized=%s want=%s", mustJSON(got), mustJSON(want))
	}
}

// TestViewAuthorizedCarriesUsers checks the one-read path the GET handler
// uses: servers and assignments ride along with the same user filter that
// AuthorizedUsers reports for the same candidates.
func TestViewAuthorizedCarriesUsers(t *testing.T) {
	pool := newCandidatePool(t, []Assignment{
		{Scope: ScopeDepartment, TargetID: "eng"},
		{Scope: ScopeUser, TargetID: "alice"},
	})
	view, err := pool.ViewAuthorized(context.Background(), "tenant-a", testPoolCandidates())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Servers) != 1 || len(view.Assignments) != 2 {
		t.Fatalf("view=%s missing servers or assignments", mustJSON(view))
	}
	users := pool.AuthorizedUsers(context.Background(), "tenant-a", testPoolCandidates())
	if !reflect.DeepEqual(view.Users, users) {
		t.Fatalf("view users=%s want=%s", mustJSON(view.Users), mustJSON(users))
	}
}

// testPoolCandidates is one candidate per user the shared directory knows:
// alice (user assignment), bob and carol (the eng department chain), dave
// (no email, no department), eve (unrelated department).
func testPoolCandidates() []AuthorizedUser {
	return []AuthorizedUser{
		{ID: "alice", Label: "alice@example.com"},
		{ID: "bob", Label: "bob@example.com"},
		{ID: "carol", Label: "carol@example.com"},
		{ID: "dave", Label: "dave"},
		{ID: "eve", Label: "eve@example.com"},
	}
}

func newCandidatePool(t *testing.T, assignments []Assignment) *Pool {
	t.Helper()
	pool := New(&memSettings{}, memDir{
		email: map[string]string{
			"alice": "alice@example.com", "bob": "bob@example.com", "carol": "carol@example.com",
			"dave": "", "eve": "eve@example.com",
		},
		group:   map[string]string{"alice@example.com": "eng", "bob@example.com": "eng", "carol@example.com": "eng-child", "eve@example.com": "sales"},
		parents: map[string]string{"eng-child": "eng"},
	})
	ctx := context.Background()
	view, err := pool.CreateServer(ctx, "tenant-a", Server{Name: "srv", BaseURL: "http://srv.example:18081", AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range assignments {
		item.ServerID = view.ID
		if _, err := pool.CreateAssignment(ctx, "tenant-a", item); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

// TestDepartmentAssignmentResolvesInSettingsTenant starts a desktop from a
// department assignment when the caller did not already set a security tenant.
// The directory answers only for the assignment's tenant.
func TestDepartmentAssignmentResolvesInSettingsTenant(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/desktops" {
			http.NotFound(w, r)
			return
		}
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"container":"desk","status":"running","image":"img","memory":"2g","cpus":"1","shm_size":"1g"}`))
	}))
	defer srv.Close()

	pool := New(&memSettings{}, tenantPoolDir{
		want:  "tenant-a",
		email: map[string]string{"carol": "carol@example.com", "sam": "sam@example.com"},
		group: map[string]string{"carol@example.com": "eng", "sam@example.com": "sales"},
	})
	ctx := context.Background()
	view, err := pool.CreateServer(ctx, "tenant-a", Server{Name: "srv", BaseURL: srv.URL, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeDepartment, TargetID: "eng", ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	got := pool.AuthorizedUsers(ctx, "tenant-a", []AuthorizedUser{
		{ID: "carol", Label: "carol@example.com"},
		{ID: "sam", Label: "sam@example.com"},
	})
	if len(got) != 1 || got[0].ID != "carol" {
		t.Fatalf("authorized=%s, want carol", mustJSON(got))
	}
	desktop, err := pool.CreateDesktop(ctx, "tenant-a", "carol")
	if err != nil || desktop.ServerID != view.ID || hits != 1 {
		t.Fatalf("desktop=%s hits=%d err=%v", mustJSON(desktop), hits, err)
	}
	if _, err := pool.CreateDesktop(ctx, "tenant-a", "sam"); err != ErrNotAssigned || hits != 1 {
		t.Fatalf("sam err=%v hits=%d, want no assignment and no extra start", err, hits)
	}
}

// tenantPoolDir answers with a department only when the context carries want.
type tenantPoolDir struct {
	want  string
	email map[string]string
	group map[string]string
}

func (d tenantPoolDir) Email(ctx context.Context, userID string) (string, error) {
	if security.TenantIDFromContext(ctx) != d.want {
		return "", nil
	}
	return d.email[userID], nil
}

func (d tenantPoolDir) GroupID(ctx context.Context, email string) (string, error) {
	if security.TenantIDFromContext(ctx) != d.want {
		return "", nil
	}
	return d.group[email], nil
}

func (d tenantPoolDir) ParentID(ctx context.Context, groupID string) (string, error) {
	if security.TenantIDFromContext(ctx) != d.want {
		return "", nil
	}
	return "", nil
}

func TestAuthorizedUsersEmptyWithoutAssignments(t *testing.T) {
	pool := New(&memSettings{}, memDir{
		email: map[string]string{"alice": "alice@example.com"},
		group: map[string]string{"alice@example.com": "eng"},
	})
	ctx := context.Background()
	if _, err := pool.CreateServer(ctx, "tenant-a", Server{Name: "srv", BaseURL: "http://srv.example:18081", AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	got := pool.AuthorizedUsers(ctx, "tenant-a", []AuthorizedUser{{ID: "alice", Label: "alice@example.com"}})
	if len(got) != 0 {
		t.Fatalf("no assignments must authorize nobody, got=%s", mustJSON(got))
	}
}

func TestDeletingUserAssignmentStopsTheOldServerDesktop(t *testing.T) {
	var stopMu sync.Mutex
	stops := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/desktops/stop" {
			var body struct {
				UserID string `json:"user_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			stopMu.Lock()
			stops[body.UserID]++
			stopMu.Unlock()
			_, _ = w.Write([]byte(`{"status":"stopped"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	defer srv.Close()

	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	view, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "old", BaseURL: srv.URL, AccessToken: "old-token", Memory: "2500m", CPUs: "1.5", ShmSize: "512m",
	})
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeUser, TargetID: "alice", ServerID: view.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.DeleteAssignment(ctx, "tenant-a", assignment.ID); err != nil {
		t.Fatal(err)
	}
	stopMu.Lock()
	defer stopMu.Unlock()
	if stops["alice"] != 1 {
		t.Fatalf("old server desktop was not stopped on assignment removal: %v", stops)
	}
}
