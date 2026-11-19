package edgenode

import (
	"context"
	"testing"
)

func TestTenantPolicyBlocksASecondNodeAndHidesSecrets(t *testing.T) {
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
	enrolled, err := reg.Enroll(ctx, code.Code, "desk-key-secret", "admin-secret", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.IssueCode(ctx, "tenant-a", "second"); err != ErrNodeLimit {
		t.Fatalf("second code = %v", err)
	}
	if err := reg.SetPolicy(ctx, "tenant-b", Policy{AllowSelf: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.IssueCode(ctx, "tenant-b", "nope"); err != ErrSelfNodes {
		t.Fatalf("blocked tenant = %v", err)
	}
	all, err := reg.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != enrolled.NodeID || all[0].TenantID != "tenant-a" {
		t.Fatalf("platform list = %+v", all)
	}
	if all[0].DesktopdKey != "" || all[0].SrvAdmin != "" || all[0].SecretHash != "" || all[0].HubTokenHash != "" {
		t.Fatalf("platform list kept a secret: %+v", all[0])
	}
	b := NewBroker(reg)
	if b.acquireStream("tenant-a", 1) != true || b.acquireStream("tenant-a", 1) != false {
		t.Fatal("stream cap did not stop the second connection")
	}
	b.releaseStream("tenant-a")
	if !b.acquireStream("tenant-a", 1) {
		t.Fatal("a closed stream did not free its slot")
	}
}
