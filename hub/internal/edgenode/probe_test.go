package edgenode

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

func TestTunnelProbeRecordsHealthAndSrvVersion(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	const deskKey = "desk-key-not-logged"
	desktopd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" || r.Header.Get("Authorization") != "Bearer "+deskKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer desktopd.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"version":"9.9.9"}`))
	}))
	defer srv.Close()

	reg := NewRegistry(&memSettings{})
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, deskKey, "admin-secret", "enroll-ver", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := reg.Authenticate(context.Background(), enrolled.NodeID, enrolled.NodeSecret)
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	go func() {
		_ = edgelink.Serve(left, map[string]string{
			edgelink.TargetDesktopd: desktopd.Listener.Addr().String(),
			edgelink.TargetSrv:      srv.Listener.Addr().String(),
		})
	}()
	broker := NewBroker(reg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Attach(ctx, node, right) }()

	deadline := time.Now().Add(3 * time.Second)
	var shown string
	for {
		live := Node{ID: node.ID, Version: "enroll-ver"}
		broker.ApplyLive(&live)
		shown = live.Version
		if strings.Contains(shown, "desktopd") && strings.Contains(shown, "srv 9.9.9") && live.Online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live version %q online %v", shown, live.Online)
		}
		time.Sleep(20 * time.Millisecond)
	}
	listed, err := reg.List(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Version != "enroll-ver" {
		t.Fatalf("stored version changed: %+v", listed)
	}
	if strings.Contains(shown, deskKey) {
		t.Fatal("the desktopd key was copied into the version")
	}
}
