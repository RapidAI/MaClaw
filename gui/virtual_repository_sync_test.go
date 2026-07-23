package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestVirtualRepositorySyncTombstoneUpdatesDoNotLoseConcurrentDeletes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	const count = 32
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			app.recordVirtualRepositorySyncTombstone("repo", fmt.Sprintf("repo-%d", i))
		}(i)
	}
	group.Wait()
	state, err := app.loadVirtualRepositorySyncState()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("repo:repo-%d", i)
		if _, exists := state.Tombstones[key]; !exists {
			t.Fatalf("missing concurrent tombstone %q", key)
		}
	}
}

func TestMergeVirtualRepositorySyncDeletionConflictCanUseCloudDeletion(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 1, 0, 0, 0, time.UTC)
	deleted := updated.Add(time.Hour)
	base := virtualRepositorySyncRepo{Repository: VirtualRepository{ID: "repo-1", Name: "Workspace", Version: 1, UpdatedAt: updated}, Location: "local"}
	local := base
	local.Repository.Name = "Workspace changed locally"
	merged, conflicts := mergeVirtualRepositorySyncPackages(
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{"repo-1": local}},
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{}, Tombstones: map[string]time.Time{"repo:repo-1": deleted}},
		map[string]string{"repo:repo-1": virtualRepositorySyncHash(base)},
		map[string]string{"repo:repo-1": "cloud"},
	)
	if len(conflicts) != 0 {
		t.Fatalf("cloud deletion should resolve conflict, got %#v", conflicts)
	}
	if _, exists := merged.Repositories["repo-1"]; exists {
		t.Fatal("cloud deletion left the local repository in the merged package")
	}
	if _, exists := merged.Tombstones["repo:repo-1"]; !exists {
		t.Fatal("cloud deletion did not retain the repository tombstone")
	}
}

func TestMergeVirtualRepositorySyncDeletionConflictCanKeepLocal(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 1, 0, 0, 0, time.UTC)
	deleted := updated.Add(time.Hour)
	base := virtualRepositorySyncRepo{Repository: VirtualRepository{ID: "repo-1", Name: "Workspace", Version: 1, UpdatedAt: updated}, Location: "local"}
	local := base
	local.Repository.Name = "Workspace changed locally"
	merged, conflicts := mergeVirtualRepositorySyncPackages(
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{"repo-1": local}},
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{}, Tombstones: map[string]time.Time{"repo:repo-1": deleted}},
		map[string]string{"repo:repo-1": virtualRepositorySyncHash(base)},
		map[string]string{"repo:repo-1": "local"},
	)
	if len(conflicts) != 0 {
		t.Fatalf("local choice should resolve conflict, got %#v", conflicts)
	}
	if got := merged.Repositories["repo-1"].Repository.Name; got != "Workspace changed locally" {
		t.Fatalf("merged repository name = %q", got)
	}
	if _, exists := merged.Tombstones["repo:repo-1"]; exists {
		t.Fatal("keeping the local repository retained the deletion tombstone")
	}
}

func TestMergeVirtualRepositorySyncRepositoryDeletionRemovesSSHSecret(t *testing.T) {
	deleted := time.Date(2026, time.July, 23, 3, 0, 0, 0, time.UTC)
	merged, conflicts := mergeVirtualRepositorySyncPackages(
		virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"repo:repo-1": deleted}},
		virtualRepositorySyncPackage{Version: 1, SSHSecrets: map[string]string{"repo-1": "cloud-password"}, Bindings: map[string]string{"repo-1:node-1": "credential-1"}},
		map[string]string{"ssh:repo-1": virtualRepositorySyncHash("cloud-password"), "binding:repo-1:node-1": virtualRepositorySyncHash("credential-1")},
		nil,
	)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected deletion conflicts: %#v", conflicts)
	}
	if _, exists := merged.SSHSecrets["repo-1"]; exists {
		t.Fatal("deleted repository retained its SSH password in the sync package")
	}
	if _, exists := merged.Tombstones["ssh:repo-1"]; !exists {
		t.Fatal("SSH deletion tombstone was not retained")
	}
	if _, exists := merged.Bindings["repo-1:node-1"]; exists {
		t.Fatal("deleted repository retained a credential binding")
	}
	if _, exists := merged.Tombstones["binding:repo-1:node-1"]; !exists {
		t.Fatal("repository deletion did not retain the binding tombstone")
	}
}

func TestMergeVirtualRepositorySyncCredentialDeletionConflictChoices(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 4, 0, 0, 0, time.UTC)
	base := virtualRepositorySyncCred{Metadata: RepositoryCredentialMetadata{ID: "credential-1", Name: "Git", Kind: "git", Username: "old", CreatedAt: updated, UpdatedAt: updated}, Secret: "old-secret"}
	local := base
	local.Metadata.Username = "new"
	local.Metadata.UpdatedAt = updated.Add(time.Minute)

	for _, choice := range []struct {
		name       string
		resolution string
		wantExists bool
	}{
		{name: "keep local", resolution: "local", wantExists: true},
		{name: "keep cloud deletion", resolution: "cloud", wantExists: false},
	} {
		t.Run(choice.name, func(t *testing.T) {
			merged, conflicts := mergeVirtualRepositorySyncPackages(
				virtualRepositorySyncPackage{Version: 1, Credentials: map[string]virtualRepositorySyncCred{"credential-1": local}},
				virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"cred:credential-1": updated.Add(2 * time.Minute)}},
				map[string]string{"cred:credential-1": virtualRepositorySyncHash(base)},
				map[string]string{"cred:credential-1": choice.resolution},
			)
			if len(conflicts) != 0 {
				t.Fatalf("unexpected conflicts: %#v", conflicts)
			}
			_, exists := merged.Credentials["credential-1"]
			if exists != choice.wantExists {
				t.Fatalf("credential exists = %t, want %t", exists, choice.wantExists)
			}
			_, tombstoned := merged.Tombstones["cred:credential-1"]
			if tombstoned == choice.wantExists {
				t.Fatalf("credential tombstone = %t, want %t", tombstoned, !choice.wantExists)
			}
		})
	}
}

func TestMergeVirtualRepositorySyncBindingAndSSHDeletionConflictChoices(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 5, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		name       string
		prefix     string
		local      virtualRepositorySyncPackage
		cloud      virtualRepositorySyncPackage
		baseHash   string
		resolution string
		present    func(virtualRepositorySyncPackage) bool
	}{
		{
			name: "binding keeps local", prefix: "binding:repo-1:node-1", resolution: "local", baseHash: virtualRepositorySyncHash("credential-old"),
			local:   virtualRepositorySyncPackage{Version: 1, Bindings: map[string]string{"repo-1:node-1": "credential-new"}},
			cloud:   virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"binding:repo-1:node-1": updated.Add(time.Minute)}},
			present: func(pkg virtualRepositorySyncPackage) bool { return pkg.Bindings["repo-1:node-1"] == "credential-new" },
		},
		{
			name: "SSH keeps cloud deletion", prefix: "ssh:repo-1", resolution: "cloud", baseHash: virtualRepositorySyncHash("old-password"),
			local:   virtualRepositorySyncPackage{Version: 1, SSHSecrets: map[string]string{"repo-1": "new-password"}},
			cloud:   virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"ssh:repo-1": updated.Add(time.Minute)}},
			present: func(pkg virtualRepositorySyncPackage) bool { _, exists := pkg.SSHSecrets["repo-1"]; return exists },
		},
	} {
		t.Run(item.name, func(t *testing.T) {
			merged, conflicts := mergeVirtualRepositorySyncPackages(item.local, item.cloud, map[string]string{item.prefix: item.baseHash}, map[string]string{item.prefix: item.resolution})
			if len(conflicts) != 0 {
				t.Fatalf("unexpected conflicts: %#v", conflicts)
			}
			if got := item.present(merged); got != (item.resolution == "local") {
				t.Fatalf("item exists = %t, want %t", got, item.resolution == "local")
			}
		})
	}
}

func TestMergeVirtualRepositorySyncPackageDoesNotMutateInputs(t *testing.T) {
	deleted := time.Date(2026, time.July, 23, 6, 0, 0, 0, time.UTC)
	local := virtualRepositorySyncPackage{
		Version:    1,
		Tombstones: map[string]time.Time{"repo:repo-1": deleted},
		Bindings:   map[string]string{"repo-1:node-1": "credential-1"},
	}
	cloud := virtualRepositorySyncPackage{
		Version:    1,
		SSHSecrets: map[string]string{"repo-1": "password"},
	}
	_, conflicts := mergeVirtualRepositorySyncPackages(local, cloud, nil, nil)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	if _, exists := local.Tombstones["ssh:repo-1"]; exists {
		t.Fatal("merge mutated local tombstones")
	}
	if _, exists := cloud.Tombstones["ssh:repo-1"]; exists {
		t.Fatal("merge mutated cloud tombstones")
	}
}

func TestMergeVirtualRepositorySyncRepositoryCopyKeepsScopedSecretsAndBindings(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 7, 0, 0, 0, time.UTC)
	base := virtualRepositorySyncRepo{Repository: VirtualRepository{ID: "repo-1", Name: "Workspace", Version: 1, UpdatedAt: updated}, Location: "remote"}
	localRepo := base
	localRepo.Repository.Name = "Workspace on this computer"
	cloudRepo := base
	cloudRepo.Repository.Name = "Workspace on the Hub"
	credential := virtualRepositorySyncCred{Metadata: RepositoryCredentialMetadata{ID: "credential-1", Name: "Git", Kind: "git", Username: "user", CreatedAt: updated, UpdatedAt: updated}, Secret: "token"}
	local := virtualRepositorySyncPackage{
		Version:      1,
		Repositories: map[string]virtualRepositorySyncRepo{"repo-1": localRepo},
		Credentials:  map[string]virtualRepositorySyncCred{"credential-1": credential},
		Bindings:     map[string]string{"repo-1:node-1": "credential-1"},
		SSHSecrets:   map[string]string{"repo-1": "ssh-password"},
	}
	cloud := virtualRepositorySyncPackage{
		Version:      1,
		Repositories: map[string]virtualRepositorySyncRepo{"repo-1": cloudRepo},
		Credentials:  map[string]virtualRepositorySyncCred{"credential-1": credential},
		Bindings:     map[string]string{"repo-1:node-1": "credential-1"},
		SSHSecrets:   map[string]string{"repo-1": "ssh-password"},
	}
	merged, conflicts := mergeVirtualRepositorySyncPackages(local, cloud, map[string]string{"repo:repo-1": virtualRepositorySyncHash(base)}, map[string]string{"repo:repo-1": "copy"})
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	var copyID string
	for id, repo := range merged.Repositories {
		if id != "repo-1" && repo.Repository.Name == "Workspace on this computer (local copy)" {
			copyID = id
		}
	}
	if copyID == "" {
		t.Fatalf("repository copy missing from %#v", merged.Repositories)
	}
	if merged.SSHSecrets[copyID] != "ssh-password" {
		t.Fatalf("copied repository SSH password = %q", merged.SSHSecrets[copyID])
	}
	if merged.Bindings[copyID+":node-1"] != "credential-1" {
		t.Fatalf("copied repository binding = %q", merged.Bindings[copyID+":node-1"])
	}
}
