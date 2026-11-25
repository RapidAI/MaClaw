package desktoppool

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// A scope=global assignment is every user's fallback. A user with no user or
// department assignment must resolve to it on every path POST /desktop uses:
// the bare owner, an isolated bot whose Home is the cloud computer, a bot the
// Home hook does not know yet, and the computer list.
func TestGlobalAssignmentIsTheFallbackForIsolatedBots(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	global, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "global", BaseURL: "http://global.example", AccessToken: "global-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: global.ID}); err != nil {
		t.Fatal(err)
	}
	pool.Home = func(_ context.Context, _, _, botID string) (string, bool) {
		if botID == "bot_iso" {
			return "", true
		}
		return "", false
	}
	for name, user := range map[string]string{
		"owner":         "carol",
		"isolated bot":  desktop.BotDesktopUser("carol", "bot_iso"),
		"unknown bot":   desktop.BotDesktopUser("carol", "bot_new"),
	} {
		server, err := pool.resolve(ctx, "tenant-a", user)
		if err != nil || server.ID != global.ID {
			t.Fatalf("%s: server=%s err=%v, want global %s", name, server.ID, err, global.ID)
		}
	}
	if got := pool.ResolvedServerID(WithComputer(ctx, ""), "tenant-a", "carol"); got != global.ID {
		t.Fatalf("pinned cloud computer = %q, want %s", got, global.ID)
	}
	servers, err := pool.computersOf(ctx, "tenant-a", "carol")
	if err != nil || len(servers) != 1 || servers[0].ID != global.ID {
		t.Fatalf("computers = %+v err=%v", servers, err)
	}
	if _, err := pool.CloudServerFor(ctx, "tenant-a", "carol", nil); err != nil {
		t.Fatalf("CloudServerFor: %v", err)
	}
}

// A Home hook that names a node no desktop server is attached to — the real
// DesktopComputerNode answer for a bot whose computer string is a stale or
// unregistered node id — must not erase the global fallback. Resolving the
// node fails, so resolve continues to the user's assignments instead of
// reporting ErrNotAssigned for a user a scope=global assignment covers.
func TestGlobalAssignmentSurvivesAnUnknownHomeNode(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	global, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "global", BaseURL: "http://global.example", AccessToken: "global-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: global.ID}); err != nil {
		t.Fatal(err)
	}
	// A bot that exists but whose computer is a node Hub has no desktop
	// server for. DesktopComputerNode answers (node, true) here; an admin
	// pin can also reach resolveComputer through WithComputer.
	for name, pin := range map[string]context.Context{
		"Home reports the node":      context.Background(),
		"WithComputer pins the node": WithComputer(context.Background(), "node-gone"),
	} {
		_ = pin
		_ = name
	}
	pool.Home = func(_ context.Context, _, _, botID string) (string, bool) {
		if botID == "bot_stale" {
			return "node-gone", true
		}
		return "", false
	}
	botUser := desktop.BotDesktopUser("carol", "bot_stale")
	server, err := pool.resolve(ctx, "tenant-a", botUser)
	if err != nil || server.ID != global.ID {
		t.Fatalf("unknown Home node: server=%s err=%v, want global %s", server.ID, err, global.ID)
	}
	server, err = pool.resolve(WithComputer(ctx, "node-gone"), "tenant-a", botUser)
	if err != nil || server.ID != global.ID {
		t.Fatalf("pinned unknown node: server=%s err=%v, want global %s", server.ID, err, global.ID)
	}
}
