package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestUserOwnsDesktopNodeRequiresARegisteredDesktop(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	settings := &edgeMemSettings{}
	reg := edgenode.NewRegistry(settings)
	pool := desktoppool.New(settings, nil)
	ctx := context.Background()

	code, err := reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "srv only")
	if err != nil {
		t.Fatal(err)
	}
	srvOnly, err := reg.Enroll(ctx, code.Code, "", "srv-admin", "v1", false, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := userOwnsDesktopNode(ctx, pool, reg, store.DefaultTenantID, "alice", srvOnly.NodeID); err == nil || !strings.Contains(err.Error(), "桌面服务") {
		t.Fatalf("srv-only node err = %v", err)
	}

	code, err = reg.IssueOwnedCode(ctx, store.DefaultTenantID, "alice", "desk")
	if err != nil {
		t.Fatal(err)
	}
	desk, err := reg.Enroll(ctx, code.Code, "desk-key", "srv-admin", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := userOwnsDesktopNode(ctx, pool, reg, store.DefaultTenantID, "alice", desk.NodeID); err == nil || !strings.Contains(err.Error(), "还没有登记") {
		t.Fatalf("unregistered desktop err = %v", err)
	}
	if _, err := pool.AttachOwnedServer(ctx, store.DefaultTenantID, "alice", desktoppool.Server{
		Name: "desk", BaseURL: desktoppool.NodeBaseURL(desk.NodeID), AccessToken: "desk-key", NodeID: desk.NodeID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := userOwnsDesktopNode(ctx, pool, reg, store.DefaultTenantID, "alice", desk.NodeID); err != nil {
		t.Fatalf("registered desktop err = %v", err)
	}
}
