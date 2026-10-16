package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// Hard-issue 8 probe. P5 writes a preference through the Service, which opens
// its own short-lived store on every call (service_user_memory.go
// openUserMemoryStore + `defer store.Stop()`). The worker instead reads through
// CoreAgentExecutor.resourcesForUser, which memoizes one store per
// tenant:user for the lifetime of the process (core_agent_executor.go
// resourcesForUser). Two different Store objects over one directory means the
// question is not "is the file written" but "does the cached one ever notice".
//
// Both orders matter. Write-then-open can only fail on a path bug, whereas
// open-then-write is the real production order: the first task of a session
// warms the cache, the user states a preference afterwards, and every later
// task in that process reuses the already-warm store.
//
// Each case runs under both backends because resolveMode picks JSON unless
// memory.db exists or MACLAW_MEMORY_BACKEND=sqlite, so a fresh deployment
// silently gets the no-sync backend.
func TestPreferenceWriteReachesWarmExecutorStore(t *testing.T) {
	for _, mode := range []struct {
		name   string
		sqlite bool
	}{
		{name: "json", sqlite: false},
		{name: "sqlite", sqlite: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			if mode.sqlite {
				t.Setenv("MACLAW_MEMORY_BACKEND", "sqlite")
			} else {
				t.Setenv("MACLAW_MEMORY_BACKEND", "")
			}
			probePreferenceReachesWarmStore(t)
		})
	}
}

func probePreferenceReachesWarmStore(t *testing.T) {
	ctx := context.Background()
	executor := &CoreAgentExecutor{}
	svc, err := NewService(
		Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"},
		NewMemoryStore(),
		executor,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(ctx, CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	p := Principal{TenantID: tenant.ID, UserID: user.ID}
	dataDir := svc.UserDataRoot(p)

	// Production order: the first task of the session warms the cache before
	// the user has said anything worth remembering.
	warm, err := executor.resourcesForUser(p.TenantID, p.UserID, dataDir)
	if err != nil {
		t.Fatalf("warm resourcesForUser: %v", err)
	}
	if got := warm.List(memory.CategoryPreference, ""); len(got) != 0 {
		t.Fatalf("preference store should start empty, got %d", len(got))
	}

	if _, err := svc.SaveUserMemory(ctx, p, UserMemorySaveInput{
		Content:  "User prefers more images in reports",
		Category: "preference",
	}); err != nil {
		t.Fatalf("SaveUserMemory: %v", err)
	}

	cached, err := executor.resourcesForUser(p.TenantID, p.UserID, dataDir)
	if err != nil {
		t.Fatalf("cached resourcesForUser: %v", err)
	}
	if cached != warm {
		t.Fatalf("expected the executor to reuse its cached store, got a different instance")
	}

	// The cached store is what the next task's cb.memory points at, and it must
	// see the write with no waiting at all. Before the fix this was the exact
	// production failure: under JSON the entry never arrived, and under SQLite
	// it arrived only after a cross-instance sync tick, so a task started right
	// after the user stated a preference ran without it.
	if _, statErr := os.Stat(filepath.Join(memory.DataDirStoreDir(dataDir), "memory.db")); statErr != nil {
		t.Logf("json backend selected: no cross-instance sync exists, so immediacy is the only correct outcome")
	} else {
		t.Logf("sqlite backend selected: cross-instance sync would paper over a regression after up to %s, so assert immediacy anyway", memory.DefaultSyncInterval)
	}
	if got := cached.List(memory.CategoryPreference, "prefers more images"); len(got) != 1 {
		t.Fatalf("warm executor store cannot see the preference written afterwards: %d entries; only a freshly opened store on the same directory does", len(got))
	}

	// A brand new store does see it, which isolates the failure to the cache
	// rather than to the write.
	fresh, err := memory.OpenDataDirStore(dataDir, memory.StoreModeAuto)
	if err != nil {
		t.Fatalf("OpenDataDirStore: %v", err)
	}
	defer fresh.Stop()
	if got := fresh.List(memory.CategoryPreference, "prefers more images"); len(got) != 1 {
		t.Fatalf("fresh store should see the preference, got %d", len(got))
	}
}
