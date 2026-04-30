package workermemory

import (
	"context"
	"path/filepath"
	"testing"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestSeedBootstrapMemoriesWritesThreeScopes(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	h := NewHandler(store)

	seeded, err := h.SeedBootstrapMemories(context.Background(), "tenant-a", BootstrapSeedInput{
		CompanyName:        "Acme",
		BusinessSummary:    "Makes industrial parts.",
		Priority:           "Stabilize delivery.",
		VirtualDepartments: []string{"Operations"},
		InitialWorkers:     []BootstrapWorkerSeed{{ID: "worker-ops", Name: "Ops iWorker", Role: "operations"}},
		RecurringTasks:     []string{"Daily operating brief"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(seeded) != 3 {
		t.Fatalf("seeded count = %d, want 3: %+v", len(seeded), seeded)
	}

	owners := map[string]bool{}
	for _, entry := range store.Search(corememory.CategoryProjectKnowledge, "", 0) {
		owners[entry.OwnerID] = true
	}
	for _, ownerID := range []string{
		companyOwnerID("tenant-a"),
		departmentOwnerID("tenant-a", "Operations"),
		personalOwnerID("tenant-a", "worker-ops"),
	} {
		if !owners[ownerID] {
			t.Fatalf("missing owner %s in %+v", ownerID, owners)
		}
	}
}
