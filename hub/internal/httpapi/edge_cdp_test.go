package httpapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

func TestCDPRelayKeepsTheGateTokenOffTheCaller(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	const gate = "gate-secret"
	session := desktoppool.DesktopSession{
		CDP:    "http://desktop:" + gate + "@desktopd.abc.node.invalid/v1/desktops/tunnel/userkey/cdp",
		NodeID: "abc",
	}
	if got, err := cdpURLForSession(context.Background(), "tenant-a", "alice", session); err == nil || strings.Contains(got, gate) {
		t.Fatalf("missing origin returned %q err %v", got, err)
	}
	ctx := context.WithValue(context.Background(), edgeOriginKey{}, "https://hub.example")
	got := edgeRelayCDP(ctx, session.CDP)
	if strings.Contains(got, gate) || strings.Contains(got, "@") || !strings.HasPrefix(got, "https://hub.example/api/v1/edge/desktop/abc/v1/desktops/tunnel/userkey/cdp") {
		t.Fatalf("relay %s", got)
	}
	public := edgeRelayCDP(ctx, "http://desktop:"+gate+"@dockerd.example:19020")
	if public != "http://desktop:"+gate+"@dockerd.example:19020" {
		t.Fatalf("public cdp changed: %s", public)
	}
}

func TestCDPTicketRelaysAnotherNodeAndRewritesTheDebuggerURL(t *testing.T) {
	t.Cleanup(holdEdgeState())
	t.Setenv("HUB_EDGE_NODES", "on")
	const gate = "gate-secret"
	key, err := desktop.UserKey("tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	var gotAuth, gotPath string
	desktopd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/page/abc"}]`)
	}))
	defer desktopd.Close()

	reg := edgenode.NewRegistry(&edgeMemSettings{})
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
	if reg.NodeIDForHubToken(enrolled.HubToken) != node.ID {
		t.Fatalf("hub token node %q", reg.NodeIDForHubToken(enrolled.HubToken))
	}
	prev := edgeRegistry
	edgeRegistry = reg
	t.Cleanup(func() {
		edgeRegistry = prev
		dropCDPTickets(node.ID)
	})

	left, right := net.Pipe()
	go func() {
		_ = edgelink.Serve(left, map[string]string{edgelink.TargetDesktopd: desktopd.Listener.Addr().String()})
	}()
	broker := edgenode.NewBroker(reg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Attach(ctx, node, right) }()
	deadline := time.Now().Add(2 * time.Second)
	for !broker.Online(node.ID) {
		if time.Now().After(deadline) {
			t.Fatal("node stayed offline")
		}
		time.Sleep(20 * time.Millisecond)
	}

	local := "http://desktop:" + gate + "@10.1.0.8/v1/desktops/tunnel/" + key + "/cdp"
	session := desktoppool.DesktopSession{
		CDP:      "http://desktop:" + gate + "@desktopd." + node.ID + ".node.invalid/v1/desktops/tunnel/" + key + "/cdp",
		LocalCDP: local,
		Display:  ":1",
		NodeID:   node.ID,
	}
	sameReq := httptest.NewRequest(http.MethodPost, "https://hub.example/api/v1/desktop-services/session", nil)
	sameReq.Host = "hub.example"
	sameReq.Header.Set("X-Forwarded-Proto", "https")
	sameReq.Header.Set("Authorization", "Bearer "+enrolled.HubToken)
	sameCtx := withEdgeCaller(withEdgeOrigin(sameReq.Context(), sameReq), sameReq)
	if got, err := cdpURLForSession(sameCtx, "tenant-a", "alice", session); err != nil || got != local {
		t.Fatalf("same-node cdp %q err %v", got, err)
	}

	otherReq := httptest.NewRequest(http.MethodPost, "https://hub.example/api/v1/desktop-services/session", nil)
	otherReq.Host = "hub.example"
	otherReq.Header.Set("X-Forwarded-Proto", "https")
	otherReq.Header.Set("Authorization", "Bearer global-token")
	otherCtx := withEdgeCaller(withEdgeOrigin(otherReq.Context(), otherReq), otherReq)
	plain := desktoppool.DesktopSession{CDP: "http://dockerd.example:19020", Display: ":1"}
	if got, err := cdpURLForSession(otherCtx, "tenant-a", "alice", plain); err != nil || got != plain.CDP {
		t.Fatalf("public cdp %q err %v", got, err)
	}
	ticketURL, err := cdpURLForSession(otherCtx, "tenant-a", "alice", session)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "https://hub.example" + edgeCDPPrefix
	if !strings.HasPrefix(ticketURL, prefix) || strings.Contains(ticketURL, gate) || strings.Contains(ticketURL, "@") {
		t.Fatalf("ticket url %s", ticketURL)
	}
	bad := session
	bad.LocalCDP = strings.Replace(local, key, "0123456789abcdef", 1)
	bad.CDP = strings.Replace(session.CDP, key, "0123456789abcdef", 1)
	if _, err := cdpURLForSession(otherCtx, "tenant-a", "alice", bad); err == nil {
		t.Fatal("mismatched user key was issued")
	}

	id := strings.TrimPrefix(ticketURL, prefix)
	rec := httptest.NewRecorder()
	relay := httptest.NewRequest(http.MethodGet, ticketURL+"/json", nil)
	relay.Host = "hub.example"
	relay.Header.Set("X-Forwarded-Proto", "https")
	relay.SetPathValue("ticket", id)
	relay.SetPathValue("rest", "json")
	EdgeCDPRelayHandler(reg, broker).ServeHTTP(rec, relay)
	if rec.Code != http.StatusOK {
		t.Fatalf("relay status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	wantSocket := "wss://hub.example" + edgeCDPPrefix + id + "/devtools/page/abc"
	if !strings.Contains(body, wantSocket) || strings.Contains(body, gate) || strings.Contains(body, "127.0.0.1:9222") {
		t.Fatalf("rewritten body %s", body)
	}
	if gotAuth != "Bearer "+gate {
		t.Fatalf("upstream auth %q", gotAuth)
	}
	if gotPath != "/v1/desktops/tunnel/"+key+"/cdp/json" {
		t.Fatalf("upstream path %s", gotPath)
	}

	cdpTickets.mu.Lock()
	item := cdpTickets.items[id]
	item.Expires = time.Now().Add(-time.Second)
	cdpTickets.items[id] = item
	cdpTickets.mu.Unlock()
	expired := httptest.NewRecorder()
	EdgeCDPRelayHandler(reg, broker).ServeHTTP(expired, relay)
	if expired.Code != http.StatusNotFound {
		t.Fatalf("expired status %d", expired.Code)
	}
}

func TestAnotherNodeGetsACDPTicketNotTheLocalAddress(t *testing.T) {
	t.Cleanup(holdEdgeState())
	t.Setenv("HUB_EDGE_NODES", "on")
	const gate = "gate-secret"
	key, err := desktop.UserKey("tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	enroll := func(name string) edgenode.EnrollResult {
		t.Helper()
		code, err := reg.IssueCode(context.Background(), "tenant-a", name)
		if err != nil {
			t.Fatal(err)
		}
		enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-"+name, "", "v1", true, false, "")
		if err != nil {
			t.Fatal(err)
		}
		return enrolled
	}
	home := enroll("home")
	other := enroll("other")
	prev := edgeRegistry
	edgeRegistry = reg
	t.Cleanup(func() { edgeRegistry = prev })

	local := "http://desktop:" + gate + "@10.1.0.8/v1/desktops/tunnel/" + key + "/cdp"
	session := desktoppool.DesktopSession{
		CDP:      "http://desktop:" + gate + "@desktopd." + home.NodeID + ".node.invalid/v1/desktops/tunnel/" + key + "/cdp",
		LocalCDP: local,
		Display:  ":1",
		NodeID:   home.NodeID,
	}
	req := httptest.NewRequest(http.MethodPost, "https://hub.example/api/v1/desktop-services/session", nil)
	req.Host = "hub.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Authorization", "Bearer "+other.HubToken)
	ctx := withEdgeCaller(withEdgeOrigin(req.Context(), req), req)
	got, err := cdpURLForSession(ctx, "tenant-a", "alice", session)
	if err != nil {
		t.Fatal(err)
	}
	if got == local || strings.Contains(got, gate) || strings.Contains(got, "10.1.0.8") || !strings.Contains(got, edgeCDPPrefix) {
		t.Fatalf("cross-node cdp %s", got)
	}
}
