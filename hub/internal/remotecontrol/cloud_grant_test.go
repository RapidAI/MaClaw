package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
)

type cloudCapture struct {
	ops    []string
	grants []string
}

func (c *cloudCapture) DispatchCloud(_ context.Context, grant Grant, op string, _ json.RawMessage) (json.RawMessage, error) {
	c.ops = append(c.ops, op)
	c.grants = append(c.grants, grant.ID)
	return json.RawMessage(`{"ok":true}`), nil
}

func TestCloudGrantsSurviveExecAndSnapshot(t *testing.T) {
	svc, machines, sender, _ := testService(t)
	for id := range machines.online {
		machines.online[id] = false
	}
	ctx := context.Background()
	g1, err := svc.EnsureCloudGrant(ctx, "tenant-1", "user-a", "mac-a", "cag_one", "user-a")
	if err != nil {
		t.Fatal(err)
	}
	gSame, err := svc.EnsureCloudGrant(ctx, "tenant-1", "user-a", "mac-a", "cag_two", "user-a")
	if err != nil {
		t.Fatal(err)
	}
	g2, err := svc.EnsureCloudGrant(ctx, "tenant-2", "user-b", "mac-c", "cag_other", "user-b")
	if err != nil {
		t.Fatal(err)
	}
	desk, err := svc.GrantSameAccount(ctx, "tenant-1", "user-a", "mac-a", "mac-b", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	capture := &cloudCapture{}
	svc.Cloud = capture
	for _, grant := range []Grant{g1, gSame, g2} {
		if _, err := svc.Exec(ctx, grant.TenantID, grant.ControllerUserID, grant.ControllerMachineID, grant.ID, OpCloudListTasks, json.RawMessage(`{"instance_id":"`+grant.TargetAgentID+`"}`)); err != nil {
			t.Fatalf("exec %s: %v", grant.ID, err)
		}
	}
	if len(capture.ops) != 3 {
		t.Fatalf("dispatch calls = %d, want 3", len(capture.ops))
	}
	for _, item := range []struct {
		tenant, machine, agent string
	}{
		{"tenant-1", "mac-a", "cag_one"},
		{"tenant-1", "mac-a", "cag_two"},
		{"tenant-2", "mac-c", "cag_other"},
	} {
		got, ok := svc.findActiveCloud(ctx, item.tenant, item.machine, item.agent)
		if !ok || got.Status != StatusActive || got.TargetKind != TargetKindCloudAgent || got.TargetMachineID != "" {
			t.Fatalf("cloud grant %s missing after exec: %+v ok=%v", item.agent, got, ok)
		}
	}
	if sender.first() != nil {
		t.Fatal("cloud exec was delivered to a desktop socket")
	}
	state, err := svc.Snapshot(ctx, "tenant-1", "user-a", "mac-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Grants) != 1 || state.Grants[0].ID != desk.ID {
		t.Fatalf("desktop grants = %+v", state.Grants)
	}
	assertNoCloud(t, "controller", state, g1.ID, gSame.ID)
	phone, err := svc.Snapshot(ctx, "tenant-1", "user-a", "")
	if err != nil {
		t.Fatal(err)
	}
	assertNoCloud(t, "phone", phone, g1.ID, gSame.ID)
	other, err := svc.Snapshot(ctx, "tenant-2", "user-b", "mac-c")
	if err != nil {
		t.Fatal(err)
	}
	assertNoCloud(t, "other-tenant", other, g2.ID)
}

func assertNoCloud(t *testing.T, label string, state State, ids ...string) {
	t.Helper()
	var seen []string
	for _, list := range [][]GrantView{state.Grants, state.Pending, state.Invites, state.Controlled} {
		for _, item := range list {
			seen = append(seen, item.ID)
		}
	}
	for _, id := range ids {
		for _, got := range seen {
			if got == id {
				t.Fatalf("%s snapshot contains cloud grant %s in %+v", label, id, state)
			}
		}
	}
}
