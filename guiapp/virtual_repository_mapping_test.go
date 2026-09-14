package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func writeTestVirtualRepository(t *testing.T, app *App, name, root string) *VirtualRepository {
	t.Helper()
	repo := &VirtualRepository{Name: name, RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestVirtualRepositoryMappingMigrationFromLegacyLocalRoot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())

	loaded, err := readVirtualRepository(repo.RootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Mappings) != 1 {
		t.Fatalf("mappings = %#v, want one migrated default mapping", loaded.Mappings)
	}
	mapping := loaded.Mappings[0]
	if mapping.ID != virtualRepositoryLegacyDefaultMappingID || mapping.Kind != virtualRepositoryMappingKindLocal || !mapping.IsDefault || mapping.RootPath != repo.RootPath {
		t.Fatalf("migrated mapping = %#v", mapping)
	}

	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	entryMappings := virtualRepositoryIndexEntryMappings(items[0])
	if len(entryMappings) != 1 || entryMappings[0].ID != virtualRepositoryLegacyDefaultMappingID || entryMappings[0].Kind != virtualRepositoryMappingKindLocal {
		t.Fatalf("index entry mappings = %#v", entryMappings)
	}
	if got := virtualRepositoryMappingSecretKey(repo.ID, mapping.ID); got != repo.ID {
		t.Fatalf("legacy default mapping secret key = %q, want repository id %q", got, repo.ID)
	}
}

func TestVirtualRepositoryMappingMigrationFromLegacyRemote(t *testing.T) {
	remote := &VirtualRepositoryRemote{Host: "dev.example.com", Port: 2222, User: "dev"}
	mappings := legacyVirtualRepositoryMappings("/srv/workspace", remote)
	if len(mappings) != 1 {
		t.Fatalf("mappings = %#v", mappings)
	}
	mapping := mappings[0]
	if mapping.Kind != virtualRepositoryMappingKindRemoteSSH || !mapping.IsDefault || mapping.Host != "dev.example.com" || mapping.Port != 2222 || mapping.User != "dev" || mapping.RootPath != "/srv/workspace" {
		t.Fatalf("migrated remote mapping = %#v", mapping)
	}
	if resolved := virtualRepositoryMappingRemote(&mapping); resolved == nil || *resolved != *remote {
		t.Fatalf("remote view = %#v", resolved)
	}
	if legacyVirtualRepositoryMappings("", nil) != nil {
		t.Fatal("unbound synchronized definition must not gain a mapping")
	}
}

func TestWriteVirtualRepositoryNeverPersistsMappingsInManifest(t *testing.T) {
	repo := &VirtualRepository{Name: "Workspace", RootPath: t.TempDir(), Nodes: []VirtualRepositoryNode{}}
	ensureVirtualRepositoryMappings(repo)
	if len(repo.Mappings) != 1 {
		t.Fatalf("in-memory mappings = %#v", repo.Mappings)
	}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(virtualRepositoryManifestPath(repo.RootPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "mappings") {
		t.Fatalf("manifest must not contain machine mappings: %s", data)
	}
}

func TestVirtualRepositoryMappingCRUD(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())

	addRemote := func(label, host string, isDefault bool) VirtualRepositoryMapping {
		t.Helper()
		req := virtualRepositoryMappingRequest{
			RepositoryID: repo.ID,
			Mapping: VirtualRepositoryMapping{
				Label:     label,
				Kind:      virtualRepositoryMappingKindRemoteSSH,
				RootPath:  "/srv/workspace",
				Host:      host,
				Port:      22,
				User:      "dev",
				IsDefault: isDefault,
			},
			Password: "secret-" + label,
		}
		raw, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		var mappings []VirtualRepositoryMapping
		if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
			t.Fatal(err)
		}
		for _, mapping := range mappings {
			if mapping.Label == label {
				return mapping
			}
		}
		t.Fatalf("added mapping %q missing from result %s", label, resultJSON)
		return VirtualRepositoryMapping{}
	}

	devServer := addRemote("dev-server", "dev.example.com", false)
	addRemote("backup-server", "backup.example.com", false)

	// Passwords land in the keyring under the mapping-scoped key, never in the index.
	if secret, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID+"/"+devServer.ID); err != nil || secret != "secret-dev-server" {
		t.Fatalf("scoped secret = %q, %v", secret, err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items[0].Mappings) != 3 {
		t.Fatalf("stored mappings = %#v", items[0].Mappings)
	}

	listJSON, err := app.ListVirtualRepositoryMappings(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	var listed []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(listJSON), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("listed mappings = %s", listJSON)
	}
	for _, mapping := range listed {
		if mapping.Kind == virtualRepositoryMappingKindLocal {
			if !mapping.IsDefault || mapping.LastStatus != "ok" {
				t.Fatalf("default local mapping = %#v", mapping)
			}
		}
	}

	// Promoting a remote mapping re-mirrors the legacy entry location.
	if _, err := app.SetDefaultVirtualRepositoryMapping(repo.ID, devServer.ID); err != nil {
		t.Fatal(err)
	}
	items, err = app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Remote == nil || items[0].Remote.Host != "dev.example.com" || items[0].RootPath != "/srv/workspace" {
		t.Fatalf("entry location after SetDefault = %#v", items[0])
	}
	if secret, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID); err != nil || secret != "secret-dev-server" {
		t.Fatalf("default mapping secret not mirrored to repository key: %q, %v", secret, err)
	}

	// The default mapping's location cannot change through an edit.
	updateReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		ID: devServer.ID, Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/elsewhere", Host: "dev.example.com", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(updateReq)
	if _, err := app.UpdateVirtualRepositoryMapping(string(raw)); err == nil || !strings.Contains(err.Error(), "MigrateVirtualRepositoryRoot") {
		t.Fatalf("default mapping location change error = %v", err)
	}

	// A label-only edit of the default mapping works and keeps it default.
	updateReq.Mapping.RootPath = "/srv/workspace"
	updateReq.Mapping.Label = "dev server renamed"
	raw, _ = json.Marshal(updateReq)
	if _, err := app.UpdateVirtualRepositoryMapping(string(raw)); err != nil {
		t.Fatal(err)
	}
	items, _ = app.loadVirtualRepositoryIndexItems()
	renamed := virtualRepositoryMappingByID(items[0].Mappings, devServer.ID)
	if renamed == nil || renamed.Label != "dev server renamed" || !renamed.IsDefault {
		t.Fatalf("updated mapping = %#v", renamed)
	}

	// Removing the default mapping promotes another one and tombstones the
	// remote mapping plus its scoped secret.
	if err := app.RemoveVirtualRepositoryMapping(repo.ID, devServer.ID); err != nil {
		t.Fatal(err)
	}
	items, _ = app.loadVirtualRepositoryIndexItems()
	if len(items[0].Mappings) != 2 || virtualRepositoryMappingByID(items[0].Mappings, devServer.ID) != nil {
		t.Fatalf("mappings after removal = %#v", items[0].Mappings)
	}
	if virtualRepositoryDefaultMapping(items[0].Mappings) == nil {
		t.Fatal("removal must promote another mapping to default")
	}
	if _, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID+"/"+devServer.ID); err == nil {
		t.Fatal("scoped secret must be deleted with its mapping")
	}
	state, err := app.loadVirtualRepositorySyncState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tombstones["vmap:"+repo.ID+"/"+devServer.ID].IsZero() {
		t.Fatalf("missing mapping tombstone: %#v", state.Tombstones)
	}
	if state.Tombstones["ssh:"+repo.ID+"/"+devServer.ID].IsZero() {
		t.Fatalf("missing scoped secret tombstone: %#v", state.Tombstones)
	}

	// Reduce to a single mapping: removing the last one is rejected.
	remainingID := items[0].Mappings[0].ID
	otherID := items[0].Mappings[1].ID
	if err := app.RemoveVirtualRepositoryMapping(repo.ID, remainingID); err != nil {
		t.Fatal(err)
	}
	if err := app.RemoveVirtualRepositoryMapping(repo.ID, otherID); err == nil || !strings.Contains(err.Error(), "only mapping") {
		t.Fatalf("removing the only mapping error = %v", err)
	}
}

func TestAddVirtualRepositoryLocalMappingInitializesEmptyRoot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())
	secondRoot := t.TempDir()

	req := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "second disk", Kind: virtualRepositoryMappingKindLocal, RootPath: secondRoot,
	}}
	raw, _ := json.Marshal(req)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 2 {
		t.Fatalf("mappings = %s", resultJSON)
	}
	initialized, err := readVirtualRepository(secondRoot)
	if err != nil {
		t.Fatalf("second root was not initialized with the manifest: %v", err)
	}
	if initialized.ID != repo.ID {
		t.Fatalf("initialized manifest id = %q, want %q", initialized.ID, repo.ID)
	}

	junkRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(junkRoot, "desktop.ini"), []byte("[.ShellClassInfo]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req.Mapping = VirtualRepositoryMapping{Label: "windows junk", Kind: virtualRepositoryMappingKindLocal, RootPath: junkRoot}
	raw, _ = json.Marshal(req)
	if _, err := app.AddVirtualRepositoryMapping(string(raw)); err != nil {
		t.Fatalf("directory with desktop.ini should still initialize: %v", err)
	}

	missingRoot := filepath.Join(t.TempDir(), "new-root")
	req.Mapping = VirtualRepositoryMapping{Label: "missing disk", Kind: virtualRepositoryMappingKindLocal, RootPath: missingRoot}
	raw, _ = json.Marshal(req)
	if _, err := app.AddVirtualRepositoryMapping(string(raw)); err != nil {
		t.Fatalf("missing local directory should be created: %v", err)
	}
	if _, err := readVirtualRepository(missingRoot); err != nil {
		t.Fatalf("missing local directory was not initialized: %v", err)
	}

	// A directory containing a different repository is rejected.
	other := writeTestVirtualRepository(t, app, "Other", t.TempDir())
	req.Mapping = VirtualRepositoryMapping{Label: "conflict", Kind: virtualRepositoryMappingKindLocal, RootPath: other.RootPath}
	raw, _ = json.Marshal(req)
	if _, err := app.AddVirtualRepositoryMapping(string(raw)); err == nil {
		t.Fatal("local mapping over a foreign repository was accepted")
	}
}

func TestAddVirtualRepositoryLocalMappingInitializesEmptyRootFromRemoteDefinition(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{
		Version:  1,
		ID:       "vrepo_remote_init",
		Name:     "Remote workspace",
		RootPath: "/home/vrepo",
		Remote:   &VirtualRepositoryRemote{Host: "www.example.com", Port: 22, User: "root"},
		Nodes:    []VirtualRepositoryNode{{ID: "src", Name: "src"}},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	app.rememberVirtualRepositoryDefinition(repo)

	localRoot := t.TempDir()
	req := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "this computer", Kind: virtualRepositoryMappingKindLocal, RootPath: localRoot,
	}}
	raw, _ := json.Marshal(req)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatalf("initialize local mapping from remote definition: %v", err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var localCount int
	for _, mapping := range mappings {
		if mapping.Kind == virtualRepositoryMappingKindLocal {
			localCount++
			if !sameVirtualRepositoryPath(mapping.RootPath, localRoot) {
				t.Fatalf("local mapping root = %q, want %q", mapping.RootPath, localRoot)
			}
		}
	}
	if localCount != 1 {
		t.Fatalf("mappings = %s", resultJSON)
	}
	initialized, err := readVirtualRepository(localRoot)
	if err != nil {
		t.Fatalf("empty local root was not initialized: %v", err)
	}
	if initialized.ID != repo.ID || initialized.Name != repo.Name {
		t.Fatalf("initialized = %#v", initialized)
	}
	if initialized.Remote != nil {
		t.Fatalf("local mapping root must not keep remote coordinates: %#v", initialized.Remote)
	}
	if len(initialized.Nodes) != 1 || initialized.Nodes[0].Name != "src" {
		t.Fatalf("initialized nodes = %#v", initialized.Nodes)
	}
}

func TestAddVirtualRepositoryLocalMappingWithoutRemoteDefinitionReportsCopyError(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{
		Version:  1,
		ID:       "vrepo_remote_missing_def",
		Name:     "Remote workspace",
		RootPath: "/home/vrepo",
		Remote:   &VirtualRepositoryRemote{Host: "www.example.com", Port: 22, User: "root"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	req := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "this computer", Kind: virtualRepositoryMappingKindLocal, RootPath: t.TempDir(),
	}}
	raw, _ := json.Marshal(req)
	_, err := app.AddVirtualRepositoryMapping(string(raw))
	if err == nil || !strings.Contains(err.Error(), "cannot copy the remote virtual repository definition") {
		t.Fatalf("error = %v, want remote definition copy failure", err)
	}
}

func TestVirtualRepositoryOperationMappingDispatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())
	secondRoot := t.TempDir()
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "second disk", Kind: virtualRepositoryMappingKindLocal, RootPath: secondRoot,
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var secondID string
	for _, mapping := range mappings {
		if mapping.Kind == virtualRepositoryMappingKindLocal && !mapping.IsDefault {
			secondID = mapping.ID
		}
	}
	if secondID == "" {
		t.Fatalf("second local mapping missing in %s", resultJSON)
	}

	// The default mapping resolves to the repository root, an explicit mapping
	// id resolves to that mapping's root.
	opReq := VirtualRepositoryOperationRequest{RepositoryID: repo.ID, Action: "sync"}
	resolved, err := app.virtualRepositoryForOperation(opReq)
	if err != nil {
		t.Fatal(err)
	}
	if !sameVirtualRepositoryPath(resolved.RootPath, repo.RootPath) {
		t.Fatalf("default mapping root = %q, want %q", resolved.RootPath, repo.RootPath)
	}
	opReq.MappingID = secondID
	resolved, err = app.virtualRepositoryForOperation(opReq)
	if err != nil {
		t.Fatal(err)
	}
	if !sameVirtualRepositoryPath(resolved.RootPath, secondRoot) {
		t.Fatalf("explicit mapping root = %q, want %q", resolved.RootPath, secondRoot)
	}
	opReq.MappingID = "map_missing"
	if _, err := app.virtualRepositoryForOperation(opReq); err == nil {
		t.Fatal("unknown mapping id must fail")
	}
	if _, err := parseVirtualRepositoryOperationRequest(`{"repository_id":"` + repo.ID + `","mapping_id":"bad/id","action":"sync"}`); err == nil {
		t.Fatal("invalid mapping id was accepted")
	}
}

func TestStartVirtualRepositoryCodingTaskDispatchesByMapping(t *testing.T) {
	app := newProjectSearchTestApp(t)
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())
	secondRoot := t.TempDir()
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "second disk", Kind: virtualRepositoryMappingKindLocal, RootPath: secondRoot,
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var secondID string
	for _, mapping := range mappings {
		if mapping.Kind == virtualRepositoryMappingKindLocal && !mapping.IsDefault {
			secondID = mapping.ID
		}
	}

	launch, err := app.StartVirtualRepositoryCodingTask(repo.ID, secondID)
	if err != nil {
		t.Fatal(err)
	}
	if launch.AgentMode != "coding_dev" || launch.ProjectPath == "" {
		t.Fatalf("launch = %#v", launch)
	}
	status, err := app.EnsureCodingWorkbenchArmed(launch.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != "local" || !status.Armed {
		t.Fatalf("coding workbench status = %#v", status)
	}
}

func TestStartVirtualRepositoryCodingTaskLocalMappingStaysLocalWhenRemoteExists(t *testing.T) {
	keyring.MockInit()
	app := newProjectSearchTestApp(t)
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var localID, remoteID string
	for _, mapping := range mappings {
		if mapping.Kind == virtualRepositoryMappingKindLocal {
			localID = mapping.ID
		}
		if mapping.Kind == virtualRepositoryMappingKindRemoteSSH {
			remoteID = mapping.ID
		}
	}
	if localID == "" || remoteID == "" {
		t.Fatalf("mappings = %#v", mappings)
	}

	launch, err := app.StartVirtualRepositoryCodingTask(repo.ID, localID)
	if err != nil {
		t.Fatal(err)
	}
	if launch.AgentMode != "coding_dev" {
		t.Fatalf("local mapping launch = %#v, want coding_dev", launch)
	}

	if _, err := app.StartVirtualRepositoryCodingTask(repo.ID, remoteID); err == nil || !strings.Contains(err.Error(), "SSH password is unavailable") {
		t.Fatalf("remote mapping launch error = %v, want SSH preflight", err)
	}
}

func TestStartVirtualRepositoryCodingTaskUsesMappingScopedSecret(t *testing.T) {
	keyring.MockInit()
	app := newProjectSearchTestApp(t)
	repo := &VirtualRepository{
		ID:       "vrepo_mapping_remote",
		Name:     "Remote workspace",
		RootPath: "/srv/workspace",
		Remote:   &VirtualRepositoryRemote{Host: "primary.example.com", Port: 22, User: "dev"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var devServerID string
	for _, mapping := range mappings {
		if mapping.Label == "dev-server" {
			devServerID = mapping.ID
		}
	}

	// Without the mapping-scoped password the launch fails before any dial.
	if _, err := app.StartVirtualRepositoryCodingTask(repo.ID, devServerID); err == nil || !strings.Contains(err.Error(), "SSH password is unavailable") {
		t.Fatalf("launch error = %v, want missing scoped password", err)
	}
	if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID+"/"+devServerID, "secret"); err != nil {
		t.Fatal(err)
	}
	// With the scoped password present, preflight advances to the host-key
	// check, proving the launch targets the requested mapping.
	if _, err := app.StartVirtualRepositoryCodingTask(repo.ID, devServerID); err == nil || !strings.Contains(err.Error(), "host key is not trusted") {
		t.Fatalf("launch error = %v, want host-key preflight failure", err)
	}
	if tasks := app.ListTasks(10); len(tasks) != 0 {
		t.Fatalf("failed remote preflight created task records: %#v", tasks)
	}
}

func TestSnapshotVirtualRepositorySyncPackageCarriesOnlyRemoteMappings(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var devServerID string
	for _, mapping := range mappings {
		if mapping.Label == "dev-server" {
			devServerID = mapping.ID
		}
	}
	if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID+"/"+devServerID, "secret"); err != nil {
		t.Fatal(err)
	}

	pkg, err := app.snapshotVirtualRepositorySyncPackage()
	if err != nil {
		t.Fatal(err)
	}
	synced := pkg.Repositories[repo.ID]
	if synced.Repository.RootPath != "" || synced.Repository.Remote != nil || len(synced.Repository.Mappings) != 0 {
		t.Fatalf("portable repository leaks machine coordinates: %#v", synced.Repository)
	}
	key := repo.ID + "/" + devServerID
	mapping, ok := pkg.Mappings[key]
	if !ok {
		t.Fatalf("remote mapping missing from sync package: %#v", pkg.Mappings)
	}
	if mapping.RepositoryID != repo.ID || mapping.MappingID != devServerID || mapping.Label != "dev-server" || mapping.Host != "dev.example.com" || mapping.RootPath != "/srv/workspace" {
		t.Fatalf("synced mapping = %#v", mapping)
	}
	for key, mapping := range pkg.Mappings {
		if strings.HasPrefix(key, repo.ID+"/") && mapping.MappingID == virtualRepositoryLegacyDefaultMappingID {
			t.Fatalf("local mapping leaked into sync package: %#v", pkg.Mappings)
		}
	}
	if pkg.SSHSecrets[key] != "secret" {
		t.Fatalf("mapping-scoped SSH secret missing from sync package: %#v", pkg.SSHSecrets)
	}
}

func TestApplyVirtualRepositorySyncMergesRemoteMappingsKeepsLocal(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())

	portable := VirtualRepository{Version: 1, ID: repo.ID, Name: repo.Name, Nodes: []VirtualRepositoryNode{}}
	pkg := virtualRepositorySyncPackage{
		Version:      virtualRepositorySyncVersion,
		Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: portable, Location: "local"}},
		Mappings: map[string]virtualRepositorySyncMapping{
			repo.ID + "/map_dev": {RepositoryID: repo.ID, MappingID: "map_dev", Label: "dev-server", RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev", IsDefault: true},
		},
	}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("index items = %#v", items)
	}
	item := items[0]
	// The bound local root and its device-private default stay untouched; the
	// synchronized default flag must not override a local default.
	if item.Unbound || item.Remote != nil || !sameVirtualRepositoryPath(item.RootPath, repo.RootPath) {
		t.Fatalf("local binding was overwritten by sync: %#v", item)
	}
	mappings := virtualRepositoryIndexEntryMappings(item)
	if len(mappings) != 2 {
		t.Fatalf("merged mappings = %#v", mappings)
	}
	remote := virtualRepositoryMappingByID(mappings, "map_dev")
	if remote == nil || remote.Kind != virtualRepositoryMappingKindRemoteSSH || remote.IsDefault {
		t.Fatalf("merged remote mapping = %#v", remote)
	}
	local := virtualRepositoryMappingByID(mappings, virtualRepositoryLegacyDefaultMappingID)
	if local == nil || !local.IsDefault {
		t.Fatalf("local default mapping = %#v", local)
	}

	// A mapping tombstone removes the synchronized mapping again.
	pkg.Tombstones = map[string]time.Time{"vmap:" + repo.ID + "/map_dev": time.Now().UTC()}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	items, _ = app.loadVirtualRepositoryIndexItems()
	if mappings := virtualRepositoryIndexEntryMappings(items[0]); len(mappings) != 1 || mappings[0].ID != virtualRepositoryLegacyDefaultMappingID {
		t.Fatalf("mappings after tombstone = %#v", mappings)
	}
}

func TestApplyVirtualRepositorySyncDefaultRemoteMappingOpensUnboundRepository(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	definition := VirtualRepository{Version: 1, ID: "vrepo_synced_remote", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	pkg := virtualRepositorySyncPackage{
		Version:      virtualRepositorySyncVersion,
		Repositories: map[string]virtualRepositorySyncRepo{definition.ID: {Repository: definition, Location: "local"}},
		Mappings: map[string]virtualRepositorySyncMapping{
			definition.ID + "/map_dev": {RepositoryID: definition.ID, MappingID: "map_dev", Label: "dev-server", RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev", IsDefault: true},
		},
	}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("index items = %#v", items)
	}
	item := items[0]
	// The synchronized default remote mapping makes the previously unbound
	// definition directly openable through that SSH endpoint.
	if item.Unbound || item.Remote == nil || item.Remote.Host != "dev.example.com" || item.RootPath != "/srv/workspace" {
		t.Fatalf("index item after mapping apply = %#v", item)
	}
	mapping := virtualRepositoryMappingByID(item.Mappings, "map_dev")
	if mapping == nil || !mapping.IsDefault {
		t.Fatalf("stored mapping = %#v", item.Mappings)
	}
}

func TestValidateVirtualRepositorySyncPackageRejectsMalformedMappings(t *testing.T) {
	repo := VirtualRepository{Version: 1, ID: "vrepo_map", Name: "Workspace", Nodes: []VirtualRepositoryNode{}}
	valid := virtualRepositorySyncMapping{RepositoryID: repo.ID, MappingID: "map_dev", Label: "dev", RootPath: "/srv/ws", Host: "dev.example.com", Port: 22, User: "dev"}
	for name, pkg := range map[string]virtualRepositorySyncPackage{
		"key mismatch": {Version: virtualRepositorySyncVersion,
			Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}},
			Mappings:     map[string]virtualRepositorySyncMapping{"other/map_dev": valid}},
		"missing repository": {Version: virtualRepositorySyncVersion,
			Mappings: map[string]virtualRepositorySyncMapping{repo.ID + "/map_dev": valid}},
		"relative remote root": {Version: virtualRepositorySyncVersion,
			Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}},
			Mappings: map[string]virtualRepositorySyncMapping{repo.ID + "/map_dev": {
				RepositoryID: repo.ID, MappingID: "map_dev", Label: "dev", RootPath: "srv/ws", Host: "dev.example.com", Port: 22, User: "dev"}}},
		"bad host": {Version: virtualRepositorySyncVersion,
			Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}},
			Mappings: map[string]virtualRepositorySyncMapping{repo.ID + "/map_dev": {
				RepositoryID: repo.ID, MappingID: "map_dev", Label: "dev", RootPath: "/srv/ws", Host: "bad host", Port: 22, User: "dev"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateVirtualRepositorySyncPackage(pkg); err == nil {
				t.Fatal("malformed mapping section was accepted")
			}
		})
	}
	// The vmap tombstone kind round-trips through validation.
	pkg := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Tombstones: map[string]time.Time{
		"vmap:" + repo.ID + "/map_dev": time.Now().UTC(),
	}}
	if err := validateVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatalf("vmap tombstone rejected: %v", err)
	}
}

func TestVirtualRepositorySyncHashCoversMappings(t *testing.T) {
	repo := VirtualRepository{Version: 1, ID: "vrepo_map", Name: "Workspace"}
	base := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}}}
	withMapping := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion,
		Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}},
		Mappings: map[string]virtualRepositorySyncMapping{repo.ID + "/map_dev": {
			RepositoryID: repo.ID, MappingID: "map_dev", Label: "dev", RootPath: "/srv/ws", Host: "dev.example.com", Port: 22, User: "dev"}}}
	if virtualRepositorySyncPackagesEqual(base, withMapping) {
		t.Fatal("packages differing only in mappings compared equal")
	}
	if !virtualRepositorySyncPackagesEqual(withMapping, withMapping) {
		t.Fatal("identical packages compared unequal")
	}
}

func TestBindVirtualRepositoryRootRecordsLocalMapping(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	definition := &VirtualRepository{Version: 1, ID: "vrepo_bind_mapping", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	pkg := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{
		definition.ID: {Repository: *definition, Location: "local"},
	}}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	req := virtualRepositoryRootBindingRequest{RepositoryID: definition.ID, RootPath: root}
	raw, _ := json.Marshal(req)
	if _, err := app.BindVirtualRepositoryRoot(string(raw)); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	mappings := virtualRepositoryIndexEntryMappings(items[0])
	if len(mappings) != 1 || mappings[0].Kind != virtualRepositoryMappingKindLocal || !mappings[0].IsDefault || !sameVirtualRepositoryPath(mappings[0].RootPath, root) {
		t.Fatalf("mappings after bind = %#v", mappings)
	}
	if _, err := os.Stat(filepath.Join(root, virtualRepositoryDirName, virtualRepositoryManifestName)); err != nil {
		t.Fatalf("bind did not initialize the manifest: %v", err)
	}
}

func TestRemoveDefaultRemoteMappingMirrorsPromotedRemoteSecret(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{
		ID:       "vrepo_remove_default",
		Name:     "Remote workspace",
		RootPath: "/srv/workspace",
		Remote:   &VirtualRepositoryRemote{Host: "primary.example.com", Port: 22, User: "dev"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID, "old-default-secret"); err != nil {
		t.Fatal(err)
	}
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var devServerID string
	for _, mapping := range mappings {
		if mapping.Label == "dev-server" {
			devServerID = mapping.ID
		}
	}
	if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID+"/"+devServerID, "dev-server-secret"); err != nil {
		t.Fatal(err)
	}

	if err := app.RemoveVirtualRepositoryMapping(repo.ID, virtualRepositoryLegacyDefaultMappingID); err != nil {
		t.Fatal(err)
	}
	// The promoted remote mapping's scoped secret takes over the bare key; the
	// detached endpoint's password must not survive there.
	if secret, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID); err != nil || secret != "dev-server-secret" {
		t.Fatalf("bare repository key after promotion = %q, %v", secret, err)
	}
}

func TestRemoveDefaultRemoteMappingToLocalDropsBareSecret(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{
		ID:       "vrepo_remove_to_local",
		Name:     "Remote workspace",
		RootPath: "/srv/workspace",
		Remote:   &VirtualRepositoryRemote{Host: "primary.example.com", Port: 22, User: "dev"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID, "old-default-secret"); err != nil {
		t.Fatal(err)
	}
	// A local mapping for a remote-primary repository needs a manifest with the
	// same id at the local root.
	localRoot := t.TempDir()
	localCopy := &VirtualRepository{ID: repo.ID, Name: repo.Name, RootPath: localRoot, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(localCopy); err != nil {
		t.Fatal(err)
	}
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "local copy", Kind: virtualRepositoryMappingKindLocal, RootPath: localRoot,
	}}
	raw, _ := json.Marshal(addReq)
	if _, err := app.AddVirtualRepositoryMapping(string(raw)); err != nil {
		t.Fatal(err)
	}

	if err := app.RemoveVirtualRepositoryMapping(repo.ID, virtualRepositoryLegacyDefaultMappingID); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID); err == nil {
		t.Fatal("bare repository key must be deleted when the promoted default is local")
	}
	state, err := app.loadVirtualRepositorySyncState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tombstones["ssh:"+repo.ID].IsZero() {
		t.Fatalf("missing bare-key tombstone: %#v", state.Tombstones)
	}
}

func TestMirrorDefaultRemoteMappingPassword(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{
		ID:       "vrepo_mirror",
		Name:     "Remote workspace",
		RootPath: "/srv/workspace",
		Remote:   &VirtualRepositoryRemote{Host: "primary.example.com", Port: 22, User: "dev"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	// Legacy default mapping (id "default") shares the bare key: no second write.
	app.mirrorDefaultRemoteMappingPassword(repo.ID, "pw-one")
	if secret, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID); err == nil && secret == "pw-one" {
		t.Fatal("mirror must not invent the bare key; it only propagates to scoped keys")
	}

	// Give the repository a second remote mapping and make it the default: its
	// scoped key must receive the password saved through the legacy paths.
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var devServerID string
	for _, mapping := range mappings {
		if mapping.Label == "dev-server" {
			devServerID = mapping.ID
		}
	}
	// SetDefault requires a reachable verified endpoint only for unbound entries.
	if _, err := app.SetDefaultVirtualRepositoryMapping(repo.ID, devServerID); err != nil {
		t.Fatal(err)
	}
	app.mirrorDefaultRemoteMappingPassword(repo.ID, "pw-two")
	if secret, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID+"/"+devServerID); err != nil || secret != "pw-two" {
		t.Fatalf("scoped default mapping key = %q, %v", secret, err)
	}
}

func TestSetDefaultRemoteMappingWithoutScopedSecretDropsStaleBareKey(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{
		ID:       "vrepo_stale_bare",
		Name:     "Remote workspace",
		RootPath: "/srv/workspace",
		Remote:   &VirtualRepositoryRemote{Host: "primary.example.com", Port: 22, User: "dev"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID, "old-default-secret"); err != nil {
		t.Fatal(err)
	}
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var devServerID string
	for _, mapping := range mappings {
		if mapping.Label == "dev-server" {
			devServerID = mapping.ID
		}
	}
	if _, err := app.SetDefaultVirtualRepositoryMapping(repo.ID, devServerID); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(virtualRepositorySSHKeyringService, repo.ID); err == nil {
		t.Fatal("stale bare key survived a default switch to an endpoint without a stored password")
	}
}

func TestAddRemoteMappingToUnboundRepositoryRequiresVerifiedManifest(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	definition := VirtualRepository{Version: 1, ID: "vrepo_unbound_guard", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	pkg := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{
		definition.ID: {Repository: definition, Location: "local"},
	}}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	addReq := virtualRepositoryMappingRequest{RepositoryID: definition.ID, Mapping: VirtualRepositoryMapping{
		Label: "dev-server", Kind: virtualRepositoryMappingKindRemoteSSH,
		RootPath: "/srv/workspace", Host: "example.invalid", Port: 22, User: "dev",
	}}
	raw, _ := json.Marshal(addReq)
	if _, err := app.AddVirtualRepositoryMapping(string(raw)); err == nil || !strings.Contains(err.Error(), "verify the remote manifest") {
		t.Fatalf("add error = %v, want remote manifest verification failure", err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Unbound || items[0].Definition == nil || len(items[0].Mappings) != 0 {
		t.Fatalf("unbound entry was converted without verification: %#v", items)
	}
}

func TestSetDefaultRemoteMappingOnUnboundRepositoryRequiresVerifiedManifest(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	definition := VirtualRepository{Version: 1, ID: "vrepo_unbound_setdefault", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	pkg := virtualRepositorySyncPackage{
		Version:      virtualRepositorySyncVersion,
		Repositories: map[string]virtualRepositorySyncRepo{definition.ID: {Repository: definition, Location: "local"}},
		Mappings: map[string]virtualRepositorySyncMapping{
			definition.ID + "/map_dev": {RepositoryID: definition.ID, MappingID: "map_dev", Label: "dev-server", RootPath: "/srv/workspace", Host: "example.invalid", Port: 22, User: "dev"},
		},
	}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Unbound {
		t.Fatalf("a non-default synchronized mapping must not convert the entry: %#v", items)
	}
	if _, err := app.SetDefaultVirtualRepositoryMapping(definition.ID, "map_dev"); err == nil || !strings.Contains(err.Error(), "verify the remote manifest") {
		t.Fatalf("set default error = %v, want remote manifest verification failure", err)
	}
	items, _ = app.loadVirtualRepositoryIndexItems()
	if !items[0].Unbound || items[0].Definition == nil {
		t.Fatalf("unbound entry was converted without verification: %#v", items)
	}
}

func TestApplyVirtualRepositorySyncUpsertPreservesConnectionState(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())
	portable := VirtualRepository{Version: 1, ID: repo.ID, Name: repo.Name, Nodes: []VirtualRepositoryNode{}}
	mappingPkg := func(label string) virtualRepositorySyncPackage {
		return virtualRepositorySyncPackage{
			Version:      virtualRepositorySyncVersion,
			Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: portable, Location: "local"}},
			Mappings: map[string]virtualRepositorySyncMapping{
				repo.ID + "/map_dev": {RepositoryID: repo.ID, MappingID: "map_dev", Label: label, RootPath: "/srv/workspace", Host: "dev.example.com", Port: 22, User: "dev"},
			},
		}
	}
	if err := app.applyVirtualRepositorySyncPackage(mappingPkg("dev-server")); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	lastOpened := items[0].LastOpened
	// Give a regression a chance to produce a visibly newer timestamp.
	time.Sleep(30 * time.Millisecond)
	app.recordVirtualRepositoryMappingStatus(repo.ID, "map_dev", RemoteVirtualRepositoryConnectionStatus{Connected: true, RootExists: true})
	items, err = app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	stored := virtualRepositoryMappingByID(items[0].Mappings, "map_dev")
	if stored == nil || stored.LastStatus != "ok" || stored.LastCheckedAt == nil {
		t.Fatalf("recorded status = %#v", stored)
	}
	if !items[0].LastOpened.Equal(lastOpened) {
		t.Fatal("status write-back must not touch LastOpened")
	}

	if err := app.applyVirtualRepositorySyncPackage(mappingPkg("dev-server-renamed")); err != nil {
		t.Fatal(err)
	}
	items, _ = app.loadVirtualRepositoryIndexItems()
	updated := virtualRepositoryMappingByID(items[0].Mappings, "map_dev")
	if updated == nil || updated.Label != "dev-server-renamed" {
		t.Fatalf("upsert did not apply the new label: %#v", updated)
	}
	if updated.LastStatus != "ok" || updated.LastCheckedAt == nil {
		t.Fatalf("upsert dropped connection state: %#v", updated)
	}
}

func TestVirtualRepositoryOperationMappingRejectsMismatchedManifest(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := writeTestVirtualRepository(t, app, "Workspace", t.TempDir())
	secondRoot := t.TempDir()
	addReq := virtualRepositoryMappingRequest{RepositoryID: repo.ID, Mapping: VirtualRepositoryMapping{
		Label: "second disk", Kind: virtualRepositoryMappingKindLocal, RootPath: secondRoot,
	}}
	raw, _ := json.Marshal(addReq)
	resultJSON, err := app.AddVirtualRepositoryMapping(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var mappings []VirtualRepositoryMapping
	if err := json.Unmarshal([]byte(resultJSON), &mappings); err != nil {
		t.Fatal(err)
	}
	var secondID string
	for _, mapping := range mappings {
		if mapping.Label == "second disk" {
			secondID = mapping.ID
		}
	}
	// Replace the second root's manifest with a different repository id.
	foreign := &VirtualRepository{ID: "vrepo_foreign", Name: "Foreign", RootPath: secondRoot, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(foreign); err != nil {
		t.Fatal(err)
	}
	_, err = app.virtualRepositoryForOperation(VirtualRepositoryOperationRequest{RepositoryID: repo.ID, MappingID: secondID, Action: "sync"})
	if err == nil || !strings.Contains(err.Error(), "no longer matches its manifest") {
		t.Fatalf("operation resolution error = %v, want manifest id mismatch", err)
	}
}

func TestGetVirtualRepositoryChangesDispatchesLocalMappingByKind(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{Name: "Workspace", RootPath: t.TempDir(), Nodes: []VirtualRepositoryNode{{
		ID: "src", Name: "src", Repository: &VirtualRepositoryBinding{Kind: "git", RemoteURL: "https://example.com/src.git", Enabled: true},
	}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	request := virtualRepositoryChangesRequest{RepositoryID: repo.ID, MappingID: virtualRepositoryLegacyDefaultMappingID, NodeID: "src"}
	raw, _ := json.Marshal(request)
	_, err := app.GetVirtualRepositoryChanges(string(raw))
	// The local mapping must reach the local collector (failing on the missing
	// checkout), never the remote dial path.
	if err == nil || !strings.Contains(err.Error(), "has not been checked out") {
		t.Fatalf("changes error = %v, want local not-checked-out failure", err)
	}
}

func TestTestVirtualRepositoryMappingConnectionWithoutRepositoryIDSkipsKeyring(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	request := virtualRepositoryMappingConnectionRequest{Mapping: VirtualRepositoryMapping{
		Label: "scratch", Kind: virtualRepositoryMappingKindLocal, RootPath: t.TempDir(),
	}}
	raw, _ := json.Marshal(request)
	statusJSON, err := app.TestVirtualRepositoryMappingConnection(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var status RemoteVirtualRepositoryConnectionStatus
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Connected || !status.RootExists {
		t.Fatalf("local mapping probe = %#v", status)
	}
}
