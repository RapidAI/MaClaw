package database

import (
	"context"
	"testing"
)

func TestMutationNeedsHostApproval(t *testing.T) {
	if MutationNeedsHostApproval(context.Background(), nil) {
		t.Fatal("nil args must not require approval")
	}
	if MutationNeedsHostApproval(context.Background(), map[string]interface{}{"action": "query", "sql": "DELETE FROM t WHERE id=1", "dry_run": false}) {
		t.Fatal("query must not require host approval")
	}
	if MutationNeedsHostApproval(context.Background(), map[string]interface{}{"action": "execute", "sql": "DELETE FROM t WHERE id=1"}) {
		t.Fatal("dry-run default must not require host approval")
	}
	if MutationNeedsHostApproval(context.Background(), map[string]interface{}{"action": "execute", "sql": "DELETE FROM t WHERE id=1", "dry_run": true}) {
		t.Fatal("explicit dry-run must not require host approval")
	}
	if !MutationNeedsHostApproval(context.Background(), map[string]interface{}{"action": "execute", "sql": "DELETE FROM t WHERE id=1", "dry_run": false}) {
		t.Fatal("committing execute must require host approval")
	}
	if !MutationNeedsHostApproval(context.Background(), map[string]interface{}{"action": "write_table", "file_path": "a.xlsx", "dry_run": false}) {
		t.Fatal("committing write_table must require host approval")
	}
	if MutationNeedsHostApproval(context.Background(), map[string]interface{}{"action": "execute", "sql": "", "dry_run": false}) {
		t.Fatal("empty SQL must not open an approval panel")
	}
	if MutationNeedsHostApproval(context.Background(), map[string]interface{}{"action": "Execute", "sql": "DELETE FROM t WHERE id=1", "dry_run": false}) {
		t.Fatal("action names must match HandleTool exactly")
	}
	ctx := WithApprovalToken(context.Background(), "host-token")
	if MutationNeedsHostApproval(ctx, map[string]interface{}{"action": "execute", "sql": "DELETE FROM t WHERE id=1", "dry_run": false}) {
		t.Fatal("host-injected approval context must satisfy the gate")
	}
}
