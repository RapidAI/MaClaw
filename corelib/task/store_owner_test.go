package task

import "testing"

// The store is shared by every principal a host serves, so ownership is what
// keeps one caller's list from being another's. Before it existed, any
// authenticated principal reached the same list.

func TestListOnlyReturnsTheCallersOwnTasks(t *testing.T) {
	s := NewStore()
	s.CreateOwned("alice", "alice task", "", nil)
	s.CreateOwned("bob", "bob task", "", nil)

	alice := s.ListOwned("alice")
	if len(alice) != 1 || alice[0].Title != "alice task" {
		t.Fatalf("alice sees %d tasks: %+v", len(alice), alice)
	}
	bob := s.ListOwned("bob")
	if len(bob) != 1 || bob[0].Title != "bob task" {
		t.Fatalf("bob sees %d tasks: %+v", len(bob), bob)
	}
}

func TestAnUnknownOwnerSeesNothingRatherThanEverything(t *testing.T) {
	s := NewStore()
	s.CreateOwned("alice", "alice task", "", nil)
	if got := s.ListOwned("carol"); len(got) != 0 {
		t.Fatalf("an owner with no tasks received %d: %+v", len(got), got)
	}
	// The unowned view is the single-user surfaces, and must not be a way to
	// read owned tasks either.
	if got := s.List(); len(got) != 0 {
		t.Fatalf("the unowned list exposed %d owned tasks: %+v", len(got), got)
	}
}

func TestAnotherOwnersTaskCannotBeReadUpdatedOrDeleted(t *testing.T) {
	s := NewStore()
	id := s.CreateOwned("alice", "alice task", "", nil)

	if _, ok := s.GetOwned("bob", id); ok {
		t.Fatal("bob read alice's task")
	}
	if err := s.UpdateOwned("bob", id, StatusCompleted, ""); err == nil {
		t.Fatal("bob updated alice's task")
	}
	if err := s.DeleteOwned("bob", id); err == nil {
		t.Fatal("bob deleted alice's task")
	}
	if _, ok := s.GetOwned("alice", id); !ok {
		t.Fatal("alice's task did not survive bob's attempts")
	}
}

// A dependency on another owner's task would both cross the scope and confirm
// that the ID exists, since an accepted dependency can block the new task
// while a rejected one is dropped.
func TestDependenciesCannotReachAcrossOwners(t *testing.T) {
	s := NewStore()
	aliceID := s.CreateOwned("alice", "alice task", "", nil)

	bobID := s.CreateOwned("bob", "bob task", "", []string{aliceID})
	bob, ok := s.GetOwned("bob", bobID)
	if !ok {
		t.Fatal("bob's task was not created")
	}
	if len(bob.DependsOn) != 0 {
		t.Fatalf("bob's task depends on another owner's task: %+v", bob.DependsOn)
	}
	if bob.Status != StatusPending {
		t.Fatalf("status=%q, want pending; another owner's task must not block it", bob.Status)
	}
}

func TestCompletingATaskOnlyUnblocksTheSameOwnersDependents(t *testing.T) {
	s := NewStore()
	first := s.CreateOwned("alice", "first", "", nil)
	second := s.CreateOwned("alice", "second", "", []string{first})
	if got, _ := s.GetOwned("alice", second); got.Status != StatusBlocked {
		t.Fatalf("status=%q, want blocked while its dependency is open", got.Status)
	}
	if err := s.UpdateOwned("alice", first, StatusCompleted, ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got, _ := s.GetOwned("alice", second); got.Status != StatusPending {
		t.Fatalf("status=%q, want pending once the dependency completed", got.Status)
	}
}

// The single-user surfaces (TUI, the un-migrated GUI task tool) have no
// principal and keep the behavior they always had.
func TestTheUnownedSurfaceStillWorksAsOneSharedList(t *testing.T) {
	s := NewStore()
	id := s.Create("shared", "", nil)
	if got := s.List(); len(got) != 1 || got[0].Title != "shared" {
		t.Fatalf("unowned list=%+v", got)
	}
	if _, ok := s.Get(id); !ok {
		t.Fatal("unowned get failed")
	}
	if err := s.Update(id, StatusCompleted, "done"); err != nil {
		t.Fatalf("unowned update: %v", err)
	}
	if err := s.Delegate(id, "worker"); err != nil {
		t.Fatalf("unowned delegate: %v", err)
	}
	if err := s.Delete(id); err != nil {
		t.Fatalf("unowned delete: %v", err)
	}
}

// Delegation has no owned form, so it must not become a way for the
// un-migrated tool to reach an owned task.
func TestTheUnownedToolCannotDelegateAnOwnedTask(t *testing.T) {
	s := NewStore()
	id := s.CreateOwned("alice", "alice task", "", nil)
	if err := s.Delegate(id, "worker"); err == nil {
		t.Fatal("the unowned surface delegated an owned task")
	}
	if err := s.Update(id, StatusCompleted, ""); err == nil {
		t.Fatal("the unowned surface updated an owned task")
	}
	if err := s.Delete(id); err == nil {
		t.Fatal("the unowned surface deleted an owned task")
	}
}
