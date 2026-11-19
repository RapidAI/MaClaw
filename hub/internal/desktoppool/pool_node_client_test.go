package desktoppool

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmptyNodeIDKeepsItsOwnClient(t *testing.T) {
	shared := &http.Client{Timeout: 2 * time.Minute}
	p := &Pool{HTTP: shared}
	if p.clientFor(context.Background(), Server{}) != shared {
		t.Fatal("direct server changed clients")
	}
	plain := &http.Client{Timeout: time.Minute}
	got, err := p.withNodeTransport(context.Background(), Server{}, plain)
	if err != nil || got != plain {
		t.Fatal("direct client was replaced")
	}
}

func TestNodeServerUsesTheTunnel(t *testing.T) {
	var dialed atomic.Int32
	pool := New(&memSettings{}, memDir{})
	pool.Nodes = func(context.Context, string, string, string) *http.Client {
		return &http.Client{Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				dialed.Add(1)
				return nil, errors.New("tunnel-dial")
			},
		}}
	}
	ctx := context.Background()
	view, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "node", BaseURL: "http://desktopd.abc.node.invalid", AccessToken: "tok",
		NodeID: "abc", Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = pool.RunBash(ctx, "tenant-a", "alice", "true")
	if dialed.Load() == 0 || err == nil || !strings.Contains(err.Error(), "tunnel-dial") {
		t.Fatalf("dialed=%d err=%v", dialed.Load(), err)
	}
}

func TestUpdateKeepsTheNodeID(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	view, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "node", BaseURL: "http://desktopd.abc.node.invalid", AccessToken: "tok",
		NodeID: "abc", Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.UpdateServer(ctx, "tenant-a", view.ID, Server{
		Name: "node", BaseURL: "http://desktopd.abc.node.invalid",
		Memory: "4g", CPUs: "1.5", ShmSize: "1g",
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = pool.RunBash(ctx, "tenant-a", "alice", "true")
	if err == nil || !strings.Contains(err.Error(), "node is offline") {
		t.Fatal(err)
	}
}

func TestNodeServerWithoutTunnelDoesNotDial(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	view, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "node", BaseURL: "http://desktopd.abc.node.invalid", AccessToken: "tok",
		NodeID: "abc", Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = pool.RunBash(ctx, "tenant-a", "alice", "true")
	if err == nil || !strings.Contains(err.Error(), "node is offline") {
		t.Fatal(err)
	}
}
