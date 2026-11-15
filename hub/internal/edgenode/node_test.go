package edgenode

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (m *memSettings) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[key]
	if !ok {
		return "", io.EOF
	}
	return v, nil
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

func TestTunnelReachesOnlyTheNamedTarget(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	desktopd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer desktopd.Close()

	reg := NewRegistry(&memSettings{})
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret)
	if err != nil {
		t.Fatal(err)
	}
	if reg.TenantForHubToken(enrolled.HubToken) != "tenant-a" {
		t.Fatalf("token tenant %q", reg.TenantForHubToken(enrolled.HubToken))
	}

	left, right := net.Pipe()
	go func() {
		_ = edgelink.Serve(left, map[string]string{edgelink.TargetDesktopd: desktopd.Listener.Addr().String()})
	}()
	broker := NewBroker(reg)
	if broker.Client("tenant-a", node.ID, edgelink.TargetDesktopd) != broker.Client("tenant-a", node.ID, edgelink.TargetDesktopd) {
		t.Fatal("tunnel client was rebuilt on every call")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Attach(ctx, node, right) }()

	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for {
		client := broker.Client("tenant-a", node.ID, edgelink.TargetDesktopd)
		resp, err = client.Get("http://desktopd.node.invalid/v1/health")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != `{"ok":true}` {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
	if _, err := broker.Dial(context.Background(), "tenant-b", node.ID, edgelink.TargetDesktopd); err == nil {
		t.Fatal("cross-tenant dial succeeded")
	}
	if _, err := broker.Dial(context.Background(), "tenant-a", node.ID, "metadata"); err == nil {
		t.Fatal("unknown target dial succeeded")
	}
}

type failSettings struct {
	memSettings
	failSaves int
}

func (f *failSettings) Set(ctx context.Context, key, value string) error {
	if f.failSaves > 0 {
		f.failSaves--
		return errors.New("disk full")
	}
	return f.memSettings.Set(ctx, key, value)
}

func TestNodeAuthSurvivesRestart(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	store := &memSettings{}
	reg := NewRegistry(store)
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewRegistry(store)
	node, err := restarted.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret)
	if err != nil {
		t.Fatal(err)
	}
	if node.TenantID != "tenant-a" {
		t.Fatalf("tenant %q", node.TenantID)
	}
	if restarted.TenantForHubToken(enrolled.HubToken) != "tenant-a" {
		t.Fatal("hub token was not reloaded")
	}
	if restarted.TenantForHubToken("not-a-token") != "" {
		t.Fatal("unknown token matched")
	}
}

func TestEnrollRestoresCodeWhenSaveFails(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	store := &failSettings{failSaves: 1}
	reg := NewRegistry(store)
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", false, true, ""); err == nil {
		t.Fatal("save failure was ignored")
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", false, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveStaysGoneAfterRestart(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	store := &memSettings{}
	reg := NewRegistry(store)
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", false, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove(context.Background(), enrolled.NodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret); err == nil {
		t.Fatal("removed node still authenticated")
	}
	restarted := NewRegistry(store)
	if _, err := restarted.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret); err == nil {
		t.Fatal("removed node came back after restart")
	}
	if restarted.TenantForHubToken(enrolled.HubToken) != "" {
		t.Fatal("removed hub token came back")
	}
}

func TestExpiredEnrollmentCodeIsRejected(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := NewRegistry(&memSettings{})
	start := time.Now()
	reg.Now = func() time.Time { return start }
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	reg.Now = func() time.Time { return start.Add(11 * time.Minute) }
	if _, err := reg.Enroll(context.Background(), code.Code, "", "", "v1", false, true, ""); err != ErrCode {
		t.Fatalf("expired code: %v", err)
	}
}

func TestPutNodeDropsDuplicateIDs(t *testing.T) {
	nodes := []Node{
		{ID: "a", SecretHash: "old"},
		{ID: "b", SecretHash: "keep"},
		{ID: "a", SecretHash: "older"},
	}
	out, prev, replaced := putNode(nodes, Node{ID: "a", SecretHash: "new"})
	if !replaced || prev.SecretHash != "old" || len(out) != 2 {
		t.Fatalf("replaced=%v prev=%s len=%d", replaced, prev.SecretHash, len(out))
	}
	if out[0].ID != "b" || out[1].SecretHash != "new" {
		t.Fatalf("out = %+v", out)
	}
	back := undoPutNode(out, "a", prev, true)
	if len(back) != 2 || back[1].SecretHash != "old" {
		t.Fatalf("undo = %+v", back)
	}
}

func TestIssueCodeRejectsALongName(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := NewRegistry(&memSettings{})
	name := strings.Repeat("n", 81)
	if _, err := reg.IssueCode(context.Background(), "tenant-a", name); err == nil {
		t.Fatal("long name was accepted")
	}
}

func TestPersonalDesktopKeepsAHubTokenOnlyWithSrv(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := NewRegistry(&memSettings{})
	desktop, err := reg.IssueOwnedCode(context.Background(), "tenant-a", "alice", "desk")
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Enroll(context.Background(), desktop.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.HubToken != "" {
		t.Fatal("a desktop-only personal node must not receive a hub token")
	}
	bare, err := reg.IssueOwnedCode(context.Background(), "tenant-a", "alice", "srv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Enroll(context.Background(), bare.Code, "desk-key", "", "v1", true, true, ""); err == nil {
		t.Fatal("a personal srv enroll without an admin secret was accepted")
	}
	withSrv, err := reg.IssueOwnedCode(context.Background(), "tenant-a", "alice", "both")
	if err != nil {
		t.Fatal(err)
	}
	out, err = reg.Enroll(context.Background(), withSrv.Code, "desk-key", "admin-secret", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.HubToken == "" {
		t.Fatal("the same-machine srv needs the hub token")
	}
}

func TestEnrollRequiresAService(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := NewRegistry(&memSettings{})
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Enroll(context.Background(), code.Code, "", "", "v1", false, false, ""); err == nil {
		t.Fatal("empty node enrolled")
	}
	if _, err := reg.Enroll(context.Background(), code.Code, strings.Repeat("k", 4097), "", "v1", true, false, ""); err == nil {
		t.Fatal("oversized key enrolled")
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "", "", "v1", false, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret); err != nil {
		t.Fatal(err)
	}
}

func TestCommitReplacesTheSameNode(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	store := &memSettings{}
	reg := NewRegistry(store)
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret)
	if err != nil {
		t.Fatal(err)
	}
	node.SecretHash = hashSecret("rotated")
	node.HubTokenHash = hashSecret("rotated-token")
	if err := reg.commit(context.Background(), node); err != nil {
		t.Fatal(err)
	}
	f, err := reg.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Nodes) != 1 {
		t.Fatalf("nodes = %d", len(f.Nodes))
	}
	if reg.TenantForHubToken(enrolled.HubToken) != "" {
		t.Fatal("old hub token still mapped")
	}
	if _, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret); err == nil {
		t.Fatal("old secret still worked in this process")
	}
	if _, err := reg.Authenticate(context.Background(), enrolled.NodeID, "rotated"); err != nil {
		t.Fatal(err)
	}
	restarted := NewRegistry(store)
	if _, err := restarted.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret); err == nil {
		t.Fatal("old secret still worked")
	}
	if _, err := restarted.Authenticate(context.Background(), enrolled.NodeID, "rotated"); err != nil {
		t.Fatal(err)
	}
	if restarted.TenantForHubToken("rotated-token") != "tenant-a" {
		t.Fatal("new hub token was not loaded")
	}
}

func TestEdgeDisabledByDefault(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "")
	reg := NewRegistry(&memSettings{})
	if _, err := reg.IssueCode(context.Background(), "tenant-a", "lab"); err != ErrDisabled {
		t.Fatalf("got %v", err)
	}
}

func TestStreamCapStopsTheNextDial(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	desktopd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer desktopd.Close()
	reg := NewRegistry(&memSettings{})
	if err := reg.SetPolicy(context.Background(), "tenant-a", Policy{AllowSelf: true, MaxStreams: 1}); err != nil {
		t.Fatal(err)
	}
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret)
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	go func() {
		_ = edgelink.Serve(left, map[string]string{edgelink.TargetDesktopd: desktopd.Listener.Addr().String()})
	}()
	broker := NewBroker(reg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Attach(ctx, node, right) }()

	deadline := time.Now().Add(2 * time.Second)
	var first net.Conn
	for {
		first, err = broker.Dial(context.Background(), "tenant-a", node.ID, edgelink.TargetDesktopd)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := broker.Dial(context.Background(), "tenant-a", node.ID, edgelink.TargetDesktopd); !errors.Is(err, ErrStreamLimit) {
		t.Fatalf("second stream err = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := broker.Dial(context.Background(), "tenant-a", node.ID, edgelink.TargetDesktopd)
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Close()
	client := broker.Client("tenant-a", node.ID, edgelink.TargetDesktopd)
	for i := 0; i < 2; i++ {
		reqCtx, reqCancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://desktopd.node.invalid/v1/health", nil)
		if reqErr != nil {
			reqCancel()
			t.Fatal(reqErr)
		}
		resp, reqErr := client.Do(req)
		reqCancel()
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("health %d", resp.StatusCode)
		}
	}
	if err := reg.SetPolicy(context.Background(), "tenant-a", Policy{AllowSelf: true}); err != nil {
		t.Fatal(err)
	}
	held, err := broker.Dial(context.Background(), "tenant-a", node.ID, edgelink.TargetDesktopd)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := broker.Dial(context.Background(), "tenant-a", node.ID, edgelink.TargetDesktopd)
	if err != nil {
		t.Fatal(err)
	}
	_ = held.Close()
	_ = extra.Close()
}
