package edgenode

import (
	"context"
	"testing"
)

func TestRotateCodeReplacesTheSecretWithoutASecondNode(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := NewRegistry(&memSettings{})
	ctx := context.Background()
	if err := reg.SetPolicy(ctx, "tenant-a", Policy{AllowSelf: true, MaxNodes: 1}); err != nil {
		t.Fatal(err)
	}
	code, err := reg.IssueCode(ctx, "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	first, err := reg.Enroll(ctx, code.Code, "desk-key", "srv-admin", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDirectBase(ctx, first.NodeID, "http://10.0.0.8:18081"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.IssueCode(ctx, "tenant-a", "another"); err == nil {
		t.Fatal("a tenant at its node cap issued a second node")
	}
	rotate, err := reg.IssueRotateCode(ctx, "tenant-a", first.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if rotate.NodeID != first.NodeID || !rotate.Rotate {
		t.Fatalf("code = %+v", rotate)
	}
	if _, err := reg.IssueRotateCode(ctx, "tenant-b", first.NodeID); err == nil {
		t.Fatal("another tenant issued a rotation code")
	}
	second, err := reg.Enroll(ctx, rotate.Code, "desk-key-2", "srv-admin", "v2", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Rotated || second.NodeID != first.NodeID || second.NodeSecret == first.NodeSecret || second.HubToken == first.HubToken {
		t.Fatalf("rotated = %+v", second)
	}
	if _, err := reg.Authenticate(ctx, first.NodeID, first.NodeSecret); err == nil {
		t.Fatal("the old secret still opens the tunnel")
	}
	if _, err := reg.Authenticate(ctx, first.NodeID, second.NodeSecret); err != nil {
		t.Fatal(err)
	}
	if reg.TenantForHubToken(first.HubToken) != "" {
		t.Fatal("the old hub token still locks a tenant")
	}
	if reg.TenantForHubToken(second.HubToken) != "tenant-a" {
		t.Fatal("the new hub token was not stored")
	}
	nodes, err := reg.List(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Version != "v2" || nodes[0].DirectBase != "http://10.0.0.8:18081" {
		t.Fatalf("nodes = %+v", nodes)
	}
}
