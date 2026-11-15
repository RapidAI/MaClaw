package desktoppool

import (
	"context"
	"testing"
)

func TestReleaseOwnedDesktopRestoresThePreviousComputer(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	cloud, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "cloud", BaseURL: "http://cloud.example", AccessToken: "cloud-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeUser, TargetID: "alice", ServerID: cloud.ID}); err != nil {
		t.Fatal(err)
	}
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pool.ResolvedServerID(ctx, "tenant-a", "alice") != owned.ID {
		t.Fatal("attach did not make the local desktop current")
	}
	left, err := pool.ReleaseOwnedDesktop(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if left != owned.ID {
		t.Fatalf("left = %s", left)
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", "alice"); got != cloud.ID {
		t.Fatalf("current = %s, want cloud %s", got, cloud.ID)
	}
	servers, active, err := pool.OwnedServers(ctx, "tenant-a", "alice")
	if err != nil || len(servers) != 1 || servers[0].ID != owned.ID || active != "" {
		t.Fatalf("owned = %+v active=%s err=%v", servers, active, err)
	}
	if again, err := pool.ReleaseOwnedDesktop(ctx, "tenant-a", "alice"); err != nil || again != "" {
		t.Fatalf("second release = %q %v", again, err)
	}
}
