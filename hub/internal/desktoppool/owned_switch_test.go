package desktoppool

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
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

func TestCloudBotKeepsTheCloudComputerAfterALocalNode(t *testing.T) {
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
	pool.Home = func(_ context.Context, _, _, botID string) (string, bool) {
		switch botID {
		case "bot_cloud":
			return "", true
		case "bot_local":
			return "dn_lab", true
		default:
			return "", false
		}
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", "alice"); got != owned.ID {
		t.Fatalf("user computer = %s, want local %s", got, owned.ID)
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", desktop.BotDesktopUser("alice", "bot_cloud")); got != cloud.ID {
		t.Fatalf("cloud bot = %s, want cloud %s", got, cloud.ID)
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", desktop.BotDesktopUser("alice", "bot_local")); got != owned.ID {
		t.Fatalf("local bot = %s, want local %s", got, owned.ID)
	}
	if got := pool.ResolvedServerID(WithComputer(ctx, ""), "tenant-a", "alice"); got != cloud.ID {
		t.Fatalf("released cloud bot = %s, want cloud %s", got, cloud.ID)
	}
	if got := pool.ResolvedServerID(WithComputer(ctx, "dn_lab"), "tenant-a", "alice"); got != owned.ID {
		t.Fatalf("released local bot = %s, want local %s", got, owned.ID)
	}
}

func TestCloudBotUsesTheGlobalComputerWhenAttachHasNoPrevious(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	cloud, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "cloud", BaseURL: "http://cloud.example", AccessToken: "cloud-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: cloud.ID}); err != nil {
		t.Fatal(err)
	}
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	pool.Home = func(_ context.Context, _, _, botID string) (string, bool) {
		if botID == "bot_cloud" {
			return "", true
		}
		return "", false
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", "alice"); got != owned.ID {
		t.Fatalf("user computer = %s, want local %s", got, owned.ID)
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", desktop.BotDesktopUser("alice", "bot_cloud")); got != cloud.ID {
		t.Fatalf("cloud bot = %s, want global cloud %s", got, cloud.ID)
	}
}

func TestLocalBotUsesTheCurrentComputerWhenAnOlderOneRemains(t *testing.T) {
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
	older, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab-old", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "old-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab-new", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "new-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	pool.Home = func(_ context.Context, _, _, botID string) (string, bool) {
		switch botID {
		case "bot_cloud":
			return "", true
		case "bot_local":
			return "dn_lab", true
		default:
			return "", false
		}
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", desktop.BotDesktopUser("alice", "bot_local")); got != current.ID || got == older.ID {
		t.Fatalf("local bot = %s, current %s, older %s", got, current.ID, older.ID)
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", desktop.BotDesktopUser("alice", "bot_cloud")); got != cloud.ID {
		t.Fatalf("cloud bot = %s, want cloud %s", got, cloud.ID)
	}
}

func TestLocalBotDoesNotOpenAnotherUsersComputer(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	if _, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "bob-lab", BaseURL: "http://bob.example", AccessToken: "bob-token",
		NodeID: "dn_lab", OwnerUserID: "bob",
	}); err != nil {
		t.Fatal(err)
	}
	pool.Home = func(_ context.Context, _, _, botID string) (string, bool) {
		return "dn_lab", botID == "bot_local"
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", desktop.BotDesktopUser("alice", "bot_local")); got != "" {
		t.Fatalf("alice opened %s", got)
	}
	shared, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "shared-lab", BaseURL: "http://shared.example", AccessToken: "shared-token",
		NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pool.ResolvedServerID(ctx, "tenant-a", desktop.BotDesktopUser("alice", "bot_local")); got != shared.ID {
		t.Fatalf("shared node = %s, want %s", got, shared.ID)
	}
}

func TestShareNodeServersOpensTheComputerToAssignedUsers(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	pin := WithComputer(ctx, "dn_lab")
	if got := pool.ResolvedServerID(pin, "tenant-a", "bob"); got != "" {
		t.Fatalf("bob resolved %s before the share", got)
	}
	if err := pool.SetNodeServersOwner(ctx, "tenant-a", "dn_lab", ""); err != nil {
		t.Fatal(err)
	}
	if got := pool.ResolvedServerID(pin, "tenant-a", "bob"); got != owned.ID {
		t.Fatalf("bob resolved %s, want %s", got, owned.ID)
	}
	if got := pool.ResolvedServerID(pin, "tenant-a", "alice"); got != owned.ID {
		t.Fatalf("alice resolved %s, want %s", got, owned.ID)
	}
	if err := pool.SetNodeServersOwner(ctx, "tenant-a", "dn_lab", ""); err != nil {
		t.Fatal("a second share is not idempotent")
	}
	servers, _, err := pool.OwnedServers(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("alice still owns %d services", len(servers))
	}
}

func TestSetNodeServersOwnerMakesItExclusive(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A shared service on the same node accepts broad assignments; making
	// the node exclusive must drop the ones no one could resolve.
	shared, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "lab-shared", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "t", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeDepartment, TargetID: "ops", ServerID: shared.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeUser, TargetID: "carol", ServerID: shared.ID}); err != nil {
		t.Fatal(err)
	}
	if err := pool.SetNodeServersOwner(ctx, "tenant-a", "dn_lab", "bob"); err != nil {
		t.Fatal(err)
	}
	pin := WithComputer(ctx, "dn_lab")
	if got := pool.ResolvedServerID(pin, "tenant-a", "bob"); got != owned.ID {
		t.Fatalf("bob resolved %s, want the rotation-refreshed record %s", got, owned.ID)
	}
	if got := pool.ResolvedServerID(pin, "tenant-a", "alice"); got != "" {
		t.Fatalf("alice still resolves %s", got)
	}
	view, err := pool.View(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	nodeServers := map[string]bool{owned.ID: true, shared.ID: true}
	bobAssign := 0
	for _, item := range view.Assignments {
		if nodeServers[item.ServerID] && !(item.Scope == ScopeUser && item.TargetID == "bob") {
			t.Fatalf("stale binding to an exclusive computer: %+v", item)
		}
		if item.Scope == ScopeUser && item.TargetID == "bob" {
			bobAssign++
		}
	}
	if bobAssign != 1 {
		t.Fatalf("bob has %d user assignments", bobAssign)
	}
	if err := pool.SetNodeServersOwner(ctx, "tenant-a", "dn_lab", ""); err != nil {
		t.Fatal(err)
	}
	if got := pool.ResolvedServerID(pin, "tenant-a", "alice"); got == "" {
		t.Fatal("alice cannot resolve the shared node")
	}
}

func TestExclusiveComputerRejectsOtherScopes(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	owned, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "t",
		NodeID: "dn_lab", OwnerUserID: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []Assignment{
		{Scope: ScopeGlobal, ServerID: owned.ID},
		{Scope: ScopeDepartment, TargetID: "ops", ServerID: owned.ID},
		{Scope: ScopeUser, TargetID: "bob", ServerID: owned.ID},
	} {
		if _, err := pool.CreateAssignment(ctx, "tenant-a", in); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("scope %q err=%v, want ErrInvalidInput", in.Scope, err)
		}
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeUser, TargetID: "alice", ServerID: owned.ID}); err != nil {
		t.Fatalf("the owner's own assignment must still work: %v", err)
	}
	shared, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "lab-shared", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "t", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []Assignment{
		{Scope: ScopeGlobal, ServerID: shared.ID},
		{Scope: ScopeDepartment, TargetID: "ops", ServerID: shared.ID},
		{Scope: ScopeUser, TargetID: "bob", ServerID: shared.ID},
	} {
		if _, err := pool.CreateAssignment(ctx, "tenant-a", in); err != nil {
			t.Fatalf("shared scope %q err=%v", in.Scope, err)
		}
	}
}
