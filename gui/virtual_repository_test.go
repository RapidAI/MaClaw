package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadVirtualRepositoryRejectsOversizedManifest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, virtualRepositoryDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, virtualRepositoryManifestName)
	file, err := os.Create(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(virtualRepositoryManifestMaxBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readVirtualRepository(root); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("read error = %v, want manifest size rejection", err)
	}
}

func TestVirtualRepositoryInputRejectsOversizedJSON(t *testing.T) {
	var value map[string]any
	err := unmarshalVirtualRepositoryInput(strings.Repeat("x", virtualRepositoryManifestMaxBytes+1), "test input", &value)
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized input error = %v", err)
	}
}

func TestLocalSettingsRejectUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writeJSONFile(path, virtualRepositoryLocalSettings{Version: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadVirtualRepositoryLocalSettings(path); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("local settings version error = %v", err)
	}
}

func TestGitClientExecutableOverrideIsPersistedAndPreferred(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	app := &App{testHomeDir: t.TempDir()}
	raw, err := app.SetVCSClientExecutable("git", git)
	if err != nil {
		t.Fatal(err)
	}
	var set VCSClientStatus
	if err := json.Unmarshal([]byte(raw), &set); err != nil {
		t.Fatal(err)
	}
	if !set.Available || set.Source != "user" || set.Executable == "" {
		t.Fatalf("saved Git client status = %+v", set)
	}

	settings, err := loadVirtualRepositoryLocalSettings(app.virtualRepositoryStatePath("virtual-repository-local-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if settings.GitExecutable != set.Executable {
		t.Fatalf("saved Git executable = %q, want %q", settings.GitExecutable, set.Executable)
	}

	raw, err = app.GetVCSClientStatus("git")
	if err != nil {
		t.Fatal(err)
	}
	var current VCSClientStatus
	if err := json.Unmarshal([]byte(raw), &current); err != nil {
		t.Fatal(err)
	}
	if !current.Available || current.Source != "user" || current.Executable != set.Executable {
		t.Fatalf("current Git client status = %+v, want configured executable %q", current, set.Executable)
	}

	if _, err := app.ResetVCSClientExecutable("git"); err != nil {
		t.Fatal(err)
	}
	if hint, err := app.VCSClientExecutableHint("git"); err != nil || hint != "" {
		t.Fatalf("Git executable hint after reset = %q, %v; want empty", hint, err)
	}
	settings, err = loadVirtualRepositoryLocalSettings(app.virtualRepositoryStatePath("virtual-repository-local-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if settings.GitExecutable != "" {
		t.Fatalf("Git executable after reset = %q, want empty", settings.GitExecutable)
	}
}

func TestVirtualRepositoryVCSClientCachesUnavailableLookup(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	clients := virtualRepositoryVCSClients{
		"git": {Kind: "git", Error: "cached missing Git"},
	}
	status := app.virtualRepositoryVCSClient("git", clients)
	if status.Error != "cached missing Git" {
		t.Fatalf("cached client status = %+v", status)
	}
}

func TestJSONStateFilesRejectEmptyAndOversizedContent(t *testing.T) {
	for name, contents := range map[string][]byte{
		"empty":     nil,
		"oversized": bytes.Repeat([]byte{'x'}, virtualRepositoryManifestMaxBytes+1),
	} {
		path := filepath.Join(t.TempDir(), name+".json")
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := readJSONFile(path, &value); err == nil {
			t.Fatalf("%s JSON state should be rejected", name)
		}
	}
}

func TestWriteJSONStateRejectsOversizedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := writeJSONFile(path, map[string]string{"value": strings.Repeat("x", virtualRepositoryManifestMaxBytes)}); err == nil {
		t.Fatal("oversized JSON state should be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("oversized JSON state created a target file: %v", err)
	}
}

func TestWriteVirtualRepositoryRejectsOversizedManifestWithoutReplacingExisting(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Name: "small", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(virtualRepositoryManifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	repo.Nodes = make([]VirtualRepositoryNode, 0, virtualRepositoryNodeMaxCount)
	for i := 0; i < cap(repo.Nodes); i++ {
		repo.Nodes = append(repo.Nodes, VirtualRepositoryNode{
			ID:       fmt.Sprintf("node-%d", i),
			ParentID: strings.Repeat("p", virtualRepositoryNameMaxLength),
			Name:     strings.Repeat("x", virtualRepositoryNameMaxLength),
		})
	}
	if err := writeVirtualRepository(repo); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("write error = %v, want manifest size rejection", err)
	}
	after, err := os.ReadFile(virtualRepositoryManifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("oversized save replaced the existing manifest")
	}
}

func TestVirtualRepositoryRoundTripAndPortableManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "build", "release"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{
		Name:     "Product",
		RootPath: root,
		Nodes: []VirtualRepositoryNode{
			{ID: "group", Name: "Outputs", Order: 10},
			{ID: "local", ParentID: "group", Name: "Release", Order: 10, Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "build/release", Enabled: true}},
		},
	}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(virtualRepositoryManifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || containsJSONKey(data, "root_path") {
		t.Fatalf("manifest must be portable and omit root_path: %s", data)
	}
	loaded, err := readVirtualRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RootPath != root || loaded.ID == "" || len(loaded.Nodes) != 2 {
		t.Fatalf("unexpected round trip: %#v", loaded)
	}
}

func TestLocalVirtualRepositoryRootMigrationCopiesFilesAndKeepsSource(t *testing.T) {
	source, destinationParent := t.TempDir(), t.TempDir()
	destination := filepath.Join(destinationParent, "new-root")
	if err := os.MkdirAll(filepath.Join(source, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "workspace", "notes.txt"), []byte("migration proof"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{Name: "Migrated", RootPath: source, Nodes: []VirtualRepositoryNode{{ID: "workspace", Name: "workspace", Repository: &VirtualRepositoryBinding{Kind: "local", Enabled: true}}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	previewRaw, err := app.PreviewVirtualRepositoryRootMigration(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination))
	if err != nil {
		t.Fatal(err)
	}
	var preview VirtualRepositoryRootMigrationPreview
	if err := json.Unmarshal([]byte(previewRaw), &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.CanMigrate || preview.SourceFileCount < 2 { // manifest + notes
		t.Fatalf("unexpected migration preview: %+v", preview)
	}
	migratedRaw, err := app.MigrateVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination))
	if err != nil {
		t.Fatal(err)
	}
	var migrated VirtualRepository
	if err := json.Unmarshal([]byte(migratedRaw), &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.RootPath != destination || migrated.ID != repo.ID {
		t.Fatalf("migration result = %+v", migrated)
	}
	if content, err := os.ReadFile(filepath.Join(destination, "workspace", "notes.txt")); err != nil || string(content) != "migration proof" {
		t.Fatalf("migrated file = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(source, "workspace", "notes.txt")); err != nil {
		t.Fatalf("source should be kept after migration: %v", err)
	}
	itemsRaw, err := app.ListVirtualRepositories()
	if err != nil {
		t.Fatal(err)
	}
	var items []VirtualRepository
	if err := json.Unmarshal([]byte(itemsRaw), &items); err != nil || len(items) != 1 || items[0].RootPath != destination {
		t.Fatalf("recent index was not switched: %s (%v)", itemsRaw, err)
	}
}

func TestLocalVirtualRepositoryRootMigrationPreviewAcceptsNewDestinationDirectory(t *testing.T) {
	source, destinationParent := t.TempDir(), t.TempDir()
	destination := filepath.Join(destinationParent, "new-root")
	repo := &VirtualRepository{Name: "New directory", RootPath: source, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	previewRaw, err := app.PreviewVirtualRepositoryRootMigration(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination))
	if err != nil {
		t.Fatal(err)
	}
	var preview VirtualRepositoryRootMigrationPreview
	if err := json.Unmarshal([]byte(previewRaw), &preview); err != nil || !preview.CanMigrate || preview.DestinationExists {
		t.Fatalf("preview = %+v, error = %v", preview, err)
	}
	if _, err := app.MigrateVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(virtualRepositoryManifestPath(destination)); err != nil {
		t.Fatalf("migration did not create destination root: %v", err)
	}
}

func TestLocalVirtualRepositoryRootMigrationRejectsSymlinkDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating directory symlinks requires extra privileges on Windows")
	}
	source, destinationParent, linkedTarget := t.TempDir(), t.TempDir(), t.TempDir()
	destination := filepath.Join(destinationParent, "linked-root")
	if err := os.Symlink(linkedTarget, destination); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{Name: "Symlink destination", RootPath: source, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	_, err := app.PreviewVirtualRepositoryRootMigration(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination))
	if err == nil || !strings.Contains(err.Error(), "must not be a symbolic link") {
		t.Fatalf("preview error = %v, want symlink destination rejection", err)
	}
	_, err = app.MigrateVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination))
	if err == nil || !strings.Contains(err.Error(), "must not be a symbolic link") {
		t.Fatalf("migration error = %v, want symlink destination rejection", err)
	}
}

func TestLocalVirtualRepositoryMigrationRejectsSymlinkDirectoryInExistingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating directory symlinks requires extra privileges on Windows")
	}
	source, destination, linkedTarget := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedTarget, filepath.Join(destination, "workspace")); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{Name: "Symlink target child", RootPath: source, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	_, err := app.MigrateVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination))
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("migration error = %v, want symlink child rejection", err)
	}
}

func TestRemoteVirtualRepositoryMigrationCopyCommandStagesOnlyNewDestinations(t *testing.T) {
	newCommand, newCleanup := remoteVirtualRepositoryMigrationCopyCommand("/srv/source", "/srv/destination", "vrepo_test", false)
	// Publication is race-guarded with mv -T (see
	// TestRemoteVirtualRepositoryMigrationCopyCommandGuardsDestinationRace).
	if !strings.Contains(newCommand, ".vrepo-migration-vrepo_test-") || !strings.Contains(newCommand, "mv -T --") || !strings.Contains(newCleanup, "rm -rf --") {
		t.Fatalf("new-destination copy command = %q; cleanup = %q", newCommand, newCleanup)
	}
	existingCommand, existingCleanup := remoteVirtualRepositoryMigrationCopyCommand("/srv/source", "/srv/destination", "vrepo_test", true)
	if strings.Contains(existingCommand, ".vrepo-migration-") || strings.Contains(existingCommand, "mv --") || existingCleanup != "" {
		t.Fatalf("existing-destination copy command = %q; cleanup = %q", existingCommand, existingCleanup)
	}
	if !strings.Contains(existingCommand, "cp -a -n") {
		t.Fatalf("existing-destination copy must never overwrite a concurrently-created file: %q", existingCommand)
	}
}

func TestRemoteVirtualRepositoryMigrationCopyCommandGuardsDestinationRace(t *testing.T) {
	command, _ := remoteVirtualRepositoryMigrationCopyCommand("/srv/source", "/srv/destination", "vrepo_test", false)
	for _, guard := range []string{"test ! -e '/srv/destination'", "test ! -L '/srv/destination'", "mv -T --"} {
		if !strings.Contains(command, guard) {
			t.Fatalf("new-destination copy command must guard against publication races (%q): %q", guard, command)
		}
	}
}

func TestRemoteVirtualRepositoryMigrationAllowsExistingDirectoriesOnly(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(destination, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := os.Lstat(filepath.Join(source, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	destinationDirectory, err := os.Lstat(filepath.Join(destination, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if remoteVirtualRepositoryMigrationPathsConflict(sourceDirectory, destinationDirectory) {
		t.Fatal("matching directories should be merge points, not migration conflicts")
	}
	if err := os.WriteFile(filepath.Join(source, "notes.txt"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "notes.txt"), []byte("destination"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceFile, err := os.Lstat(filepath.Join(source, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	destinationFile, err := os.Lstat(filepath.Join(destination, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !remoteVirtualRepositoryMigrationPathsConflict(sourceFile, destinationFile) {
		t.Fatal("existing file must remain a migration conflict")
	}
}

func TestLocalVirtualRepositoryRootMigrationRejectsExistingDestinationConflict(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "same.txt"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "same.txt"), []byte("destination"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{Name: "Conflict", RootPath: source, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	_, err := app.MigrateVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"destination_root":%q}`, repo.ID, destination))
	if err == nil || !strings.Contains(err.Error(), "migration conflict") {
		t.Fatalf("migration error = %v, want destination conflict", err)
	}
	if content, readErr := os.ReadFile(filepath.Join(destination, "same.txt")); readErr != nil || string(content) != "destination" {
		t.Fatalf("destination file was changed: %q, %v", content, readErr)
	}
}

func TestSaveVirtualRepositoryRejectsDirectRootChange(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{Name: "Protected root", RootPath: source, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	// A copied manifest makes the destination look valid to the ordinary save
	// path. It must still be rejected so callers cannot bypass the migration
	// copy/verification transaction by simply changing root_path.
	if err := copyVirtualRepositoryTree(source, destination); err != nil {
		t.Fatal(err)
	}
	candidate, err := readVirtualRepository(destination)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveVirtualRepository(string(raw)); err == nil || !strings.Contains(err.Error(), "requires MigrateVirtualRepositoryRoot") {
		t.Fatalf("direct root change error = %v", err)
	}
}

func containsJSONKey(data []byte, key string) bool {
	var value map[string]any
	_ = json.Unmarshal(data, &value)
	_, ok := value[key]
	return ok
}

func TestResolveVirtualRepositoryPathRejectsEscapeAndMetadata(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../outside", ".vrepo", ".vrepo/cache", filepath.Join("..", "outside")} {
		if _, err := resolveVirtualRepositoryPath(root, rel, false); err == nil {
			t.Fatalf("resolveVirtualRepositoryPath(%q) should fail", rel)
		}
	}
	got, err := resolveVirtualRepositoryPath(root, "build/release", false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "build", "release")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestVirtualRepositoryPathsRejectControlCharactersAndOversizedValues(t *testing.T) {
	root := t.TempDir()
	if _, err := cleanVirtualRepositoryRoot(root + "\x00"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("root control-character error = %v", err)
	}
	for _, relative := range []string{"build\nrelease", strings.Repeat("x", virtualRepositoryFieldMaxLength+1)} {
		if _, err := resolveVirtualRepositoryPath(root, relative, false); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("local relative path %q error = %v", truncateVirtualRepositoryDiagnostic(relative, 32), err)
		}
		if err := validateRemoteVirtualRepositoryRelativePath(filepath.ToSlash(relative)); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("remote relative path %q error = %v", truncateVirtualRepositoryDiagnostic(relative, 32), err)
		}
	}
}

func TestVirtualRepositoryRootUnavailableRecognizesWindowsDriveFailures(t *testing.T) {
	if !isVirtualRepositoryRootUnavailable(os.ErrNotExist) {
		t.Fatal("ordinary missing roots must be repairable")
	}
	pathMissing := &os.PathError{Op: "open", Path: `Z:\workspace\.vrepo\manifest.json`, Err: syscall.Errno(3)}
	if got := isVirtualRepositoryRootUnavailable(pathMissing); got != (runtime.GOOS == "windows") {
		t.Fatalf("Windows path-not-found unavailable = %t, want %t", got, runtime.GOOS == "windows")
	}
	invalidDrive := &os.PathError{Op: "open", Path: `Z:\workspace\.vrepo\manifest.json`, Err: syscall.Errno(15)}
	if got := isVirtualRepositoryRootUnavailable(invalidDrive); got != (runtime.GOOS == "windows") {
		t.Fatalf("Windows invalid-drive unavailable = %t, want %t", got, runtime.GOOS == "windows")
	}
}

func TestResolveVirtualRepositoryPathRejectsExistingSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := resolveVirtualRepositoryPath(root, "linked/new-output", false); err == nil {
		t.Fatal("create path through escaping symlink should fail")
	}
}

func TestValidateVirtualRepositoryRejectsCyclesAndDuplicateTreePaths(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{
		{ID: "a", ParentID: "b", Name: "A"},
		{ID: "b", ParentID: "a", Name: "B"},
	}}
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("cycle should fail")
	}
	repo.Nodes = []VirtualRepositoryNode{
		{ID: "a", Name: "out", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "first", Enabled: true}},
		{ID: "b", Name: "out", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "second", Enabled: true}},
	}
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("duplicate mapped tree path should fail")
	}
}

func TestValidateVirtualRepositoryDerivesMappingPathFromTree(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Version: 1, Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{
		{ID: "services", Name: "services"},
		{ID: "api", ParentID: "services", Name: "api", Repository: &VirtualRepositoryBinding{Kind: "git", RemoteURL: "https://example.com/api.git", Enabled: true}},
	}}
	if err := validateVirtualRepository(repo); err != nil {
		t.Fatalf("validateVirtualRepository() error = %v", err)
	}
	if got := repo.Nodes[1].Repository.RelativePath; got != "services/api" {
		t.Fatalf("derived mapping path = %q, want %q", got, "services/api")
	}
}

func TestValidateVirtualRepositoryRejectsChildrenOfMappings(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{
		{ID: "repo", Name: "Repo", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "out", Enabled: true}},
		{ID: "child", ParentID: "repo", Name: "Child"},
	}}
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("mapped nodes must be leaves")
	}
}

func TestValidateVirtualRepositoryHandlesDeepParentChains(t *testing.T) {
	root := t.TempDir()
	nodes := make([]VirtualRepositoryNode, 2000)
	for i := range nodes {
		nodes[i] = VirtualRepositoryNode{ID: "node-" + strconv.Itoa(i), Name: "Node " + strconv.Itoa(i)}
		if i > 0 {
			nodes[i].ParentID = nodes[i-1].ID
		}
	}
	repo := &VirtualRepository{Version: 1, Name: "deep", RootPath: root, Nodes: nodes}
	if err := validateVirtualRepository(repo); err != nil {
		t.Fatalf("deep valid tree rejected: %v", err)
	}
	nodes[0].ParentID = nodes[len(nodes)-1].ID
	if err := validateVirtualRepository(repo); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("deep cycle error = %v", err)
	}
}

func TestValidateVirtualRepositoryRejectsExcessiveNodeCount(t *testing.T) {
	nodes := make([]VirtualRepositoryNode, virtualRepositoryNodeMaxCount+1)
	for i := range nodes {
		nodes[i] = VirtualRepositoryNode{ID: fmt.Sprintf("node-%d", i), Name: fmt.Sprintf("Node %d", i)}
	}
	repo := &VirtualRepository{Name: "too-many", RootPath: t.TempDir(), Nodes: nodes}
	if err := validateVirtualRepository(repo); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("node count error = %v", err)
	}
}

func TestVirtualRepositoryIndexDoesNotDeleteManifest(t *testing.T) {
	root := t.TempDir()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteVirtualRepository(repo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(virtualRepositoryManifestPath(root)); err != nil {
		t.Fatalf("deleting index entry must preserve .vrepo: %v", err)
	}
}

func TestSaveVirtualRepositoryRejectsStaleRevision(t *testing.T) {
	root := t.TempDir()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{Name: "first", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	stale := *repo
	repo.Name = "newer"
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	stale.Name = "stale overwrite"
	raw, _ := json.Marshal(stale)
	if _, err := app.SaveVirtualRepository(string(raw)); err == nil {
		t.Fatal("stale save should be rejected")
	}
}

func TestSaveVirtualRepositoryRejectsMismatchedIdentityAndMissingRevision(t *testing.T) {
	root := t.TempDir()
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{Name: "original", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}

	wrongID := *repo
	wrongID.ID = "vrepo_different"
	raw, _ := json.Marshal(wrongID)
	if _, err := app.SaveVirtualRepository(string(raw)); err == nil || !strings.Contains(err.Error(), "id does not match") {
		t.Fatalf("mismatched id error = %v", err)
	}

	missingRevision := *repo
	missingRevision.UpdatedAt = time.Time{}
	raw, _ = json.Marshal(missingRevision)
	if _, err := app.SaveVirtualRepository(string(raw)); err == nil || !strings.Contains(err.Error(), "revision is required") {
		t.Fatalf("missing revision error = %v", err)
	}

	current, err := readVirtualRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != repo.ID || current.Name != "original" {
		t.Fatalf("rejected save changed manifest: %#v", current)
	}
}

func TestSaveVirtualRepositoryDoesNotOverwriteExistingManifestAsNew(t *testing.T) {
	root := t.TempDir()
	app := &App{testHomeDir: t.TempDir()}
	existing := &VirtualRepository{Name: "existing", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(existing); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(virtualRepositoryManifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	input := VirtualRepository{Name: "replacement", RootPath: root, Nodes: []VirtualRepositoryNode{}}
	raw, _ := json.Marshal(input)
	if _, err := app.SaveVirtualRepository(string(raw)); err == nil || !strings.Contains(err.Error(), "already contains") {
		t.Fatalf("create over existing manifest error = %v", err)
	}
	after, err := os.ReadFile(virtualRepositoryManifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("new repository request replaced the existing manifest")
	}
}

func TestSaveVirtualRepositoryRejectsInvalidIndexBeforeWritingManifest(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), virtualRepositoryIndex{Version: 2}); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(VirtualRepository{Name: "new", RootPath: root, Nodes: []VirtualRepositoryNode{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveVirtualRepository(string(input)); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("invalid index save error = %v", err)
	}
	if _, err := os.Stat(virtualRepositoryManifestPath(root)); !os.IsNotExist(err) {
		t.Fatalf("manifest was created despite invalid index: %v", err)
	}
}

func TestDeleteVirtualRepositoryRejectsEmptyID(t *testing.T) {
	if err := (&App{testHomeDir: t.TempDir()}).DeleteVirtualRepository("  "); err == nil {
		t.Fatal("empty repository id should be rejected")
	}
}

func TestDeleteVirtualRepositoryRejectsUnknownIDWithoutRewritingIndex(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	indexPath := app.virtualRepositoryStatePath("virtual-repositories-index.json")
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{ID: "keep", Name: "Keep", RootPath: t.TempDir(), LastOpened: time.Now().UTC()}}}
	if err := writeJSONFile(indexPath, index); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteVirtualRepository("missing"); err == nil {
		t.Fatal("unknown repository id should be rejected")
	}
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unknown-id deletion rewrote the index")
	}
}

func TestDeleteVirtualRepositoryWaitsForRemoteSaveTransaction(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{ID: "repo", Name: "Repo", RootPath: t.TempDir()}}}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), index); err != nil {
		t.Fatal(err)
	}
	virtualRepositoryRemoteSaveMu.Lock()
	done := make(chan error, 1)
	go func() { done <- app.DeleteVirtualRepository("repo") }()
	select {
	case err := <-done:
		virtualRepositoryRemoteSaveMu.Unlock()
		t.Fatalf("delete completed during remote save transaction: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	virtualRepositoryRemoteSaveMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete did not resume after remote save transaction")
	}
}

func TestListVirtualRepositoriesRejectsInvalidIndex(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{
		{ID: "duplicate", Name: "One", RootPath: t.TempDir()},
		{ID: "duplicate", Name: "Two", RootPath: t.TempDir()},
	}}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), index); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListVirtualRepositories(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("invalid index error = %v", err)
	}
}

func TestVirtualRepositoryIndexLookupRejectsInvalidIndex(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{
		{ID: "duplicate", Name: "One", RootPath: t.TempDir()},
		{ID: "duplicate", Name: "Two", RootPath: t.TempDir()},
	}}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), index); err != nil {
		t.Fatal(err)
	}
	if _, err := app.virtualRepositoryIndexEntryByID("duplicate"); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("index lookup error = %v", err)
	}
}

func TestCommonSVNPathsAreBoundedAndUnique(t *testing.T) {
	paths := commonSVNExecutablePaths()
	seen := map[string]bool{}
	for _, path := range paths {
		key := path
		if runtime.GOOS == "windows" {
			key = filepath.Clean(path)
		}
		if seen[key] {
			t.Fatalf("duplicate path %q", path)
		}
		seen[key] = true
	}
	if len(paths) > 20 {
		t.Fatalf("discovery must remain bounded, got %d paths", len(paths))
	}
}

func TestCommonGitPathsAreBoundedAndUnique(t *testing.T) {
	paths := commonGitExecutablePaths()
	seen := map[string]bool{}
	for _, path := range paths {
		key := path
		if runtime.GOOS == "windows" {
			key = filepath.Clean(path)
		}
		if seen[key] {
			t.Fatalf("duplicate path %q", path)
		}
		seen[key] = true
	}
	if len(paths) > 20 {
		t.Fatalf("discovery must remain bounded, got %d paths", len(paths))
	}
}

func TestNormalizeRepositoryRemoteURLStripsCredentialsAndNoise(t *testing.T) {
	got := normalizeRepositoryRemoteURL("https://user:secret@example.com/repo.git/?token=secret#fragment")
	if got != "https://example.com/repo.git" {
		t.Fatalf("unexpected normalized URL %q", got)
	}
}

func TestValidateVirtualRepositoryRejectsRemoteURLCredentials(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "repo", RemoteURL: "https://user:secret@example.com/repo.git", Enabled: true}}}}
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("repository URLs containing credentials should be rejected")
	}
}

func TestValidateVirtualRepositoryRejectsRemoteURLQueryAndMalformedEscape(t *testing.T) {
	root := t.TempDir()
	for _, remote := range []string{
		"https://example.com/repo.git?token=secret",
		"https://example.com/repo.git#credentials",
		"https://example.com/%zz",
	} {
		repo := &VirtualRepository{Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "repo", RemoteURL: remote, Enabled: true}}}}
		if err := validateVirtualRepository(repo); err == nil {
			t.Fatalf("repository URL %q should be rejected", remote)
		}
	}
}

func TestValidateVirtualRepositoryRejectsUnsupportedOrIncompleteRemoteURLs(t *testing.T) {
	root := t.TempDir()
	for _, remote := range []string{
		"example.com/repo.git",
		"ftp://example.com/repo.git",
		"https:///repo.git",
		"https://example.com",
		"https://example.com/repo.git\n--upload-pack=evil",
	} {
		repo := &VirtualRepository{Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "repo", RemoteURL: remote, Enabled: true}}}}
		if err := validateVirtualRepository(repo); err == nil {
			t.Fatalf("repository URL %q should be rejected", remote)
		}
	}
}

func TestValidateVirtualRepositoryAcceptsSupportedRemoteURLs(t *testing.T) {
	root := t.TempDir()
	for _, remote := range []string{
		"https://github.com/example/repo.git",
		"ssh://github.com/example/repo.git",
		"git@github.com:example/repo.git",
		"file:///srv/git/repo.git",
	} {
		repo := &VirtualRepository{Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "repo", RemoteURL: remote, Enabled: true}}}}
		if err := validateVirtualRepository(repo); err != nil {
			t.Fatalf("repository URL %q rejected: %v", remote, err)
		}
	}
}

func TestValidateVirtualRepositoryRejectsControlCharactersAndOversizedFields(t *testing.T) {
	root := t.TempDir()
	for name, repo := range map[string]*VirtualRepository{
		"repository name control": {Version: 1, Name: "bad\nname", RootPath: root},
		"node name control":       {Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "node", Name: "bad\tname"}}},
		"oversized name":          {Version: 1, Name: strings.Repeat("x", virtualRepositoryNameMaxLength+1), RootPath: root},
	} {
		if err := validateVirtualRepository(repo); err == nil {
			t.Fatalf("%s should be rejected", name)
		}
	}
}

func TestValidateVirtualRepositoryRejectsBindingDelimiterInIDs(t *testing.T) {
	root := t.TempDir()
	for name, repo := range map[string]*VirtualRepository{
		"repository id": {Version: 1, ID: "repo:other", Name: "Repo", RootPath: root},
		"node id":       {Version: 1, Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "node:other", Name: "Node"}}},
	} {
		if err := validateVirtualRepository(repo); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestValidateVirtualRepositoryRejectsReversedTimestamps(t *testing.T) {
	now := time.Now().UTC()
	repo := &VirtualRepository{Version: 1, Name: "Repo", RootPath: t.TempDir(), CreatedAt: now, UpdatedAt: now.Add(-time.Second)}
	if err := validateVirtualRepository(repo); err == nil || !strings.Contains(err.Error(), "updated_at") {
		t.Fatalf("timestamp error = %v", err)
	}
}

func TestVirtualRepositoryBindingByRelativePath(t *testing.T) {
	repo := &VirtualRepository{Nodes: []VirtualRepositoryNode{{Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "build/output"}}}}
	if got := virtualRepositoryBindingByRelativePath(repo, " build/output "); got == nil || got.RelativePath != "build/output" {
		t.Fatalf("binding lookup=%#v", got)
	}
	if got := virtualRepositoryBindingByRelativePath(repo, "other"); got != nil {
		t.Fatalf("unexpected binding=%#v", got)
	}
}

func TestValidateRemoteVirtualRepository(t *testing.T) {
	repo := &VirtualRepository{Version: 1, Name: "remote", RootPath: "/srv/workspace", Remote: &VirtualRepositoryRemote{Host: "example.com", User: "deploy"}, Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "services/api", RemoteURL: "https://example.com/api.git", Enabled: true}}}}
	if err := validateVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if repo.Remote.Port != 22 {
		t.Fatalf("default remote port=%d", repo.Remote.Port)
	}
	// Mapping paths are derived from the node tree (name + parent chain); a
	// persisted relative_path is an implementation detail that validation
	// recomputes, so a hostile persisted value cannot smuggle an absolute or
	// escaping path into the checkout layout (deriveVirtualRepositoryMappingPaths).
	repo.Nodes[0].Repository.RelativePath = "/maclaw2"
	if err := validateVirtualRepository(repo); err != nil {
		t.Fatalf("persisted relative_path must be re-derived, not rejected: %v", err)
	}
	if repo.Nodes[0].Repository.RelativePath != "Git" {
		t.Fatalf("relative path = %q, want derived from node name", repo.Nodes[0].Repository.RelativePath)
	}
	repo.Nodes[0].Repository.RelativePath = "services/api"
	for _, invalid := range []string{"../escape", "folder/../escape", "folder//child", "./child", "/absolute", "//server/share", `.vrepo/secret`, `windows\path`} {
		if err := validateRemoteVirtualRepositoryRelativePath(invalid); err == nil {
			t.Fatalf("remote relative path %q should fail", invalid)
		}
	}
	repo.Nodes[0].Repository.RelativePath = "services/api"
	repo.RootPath = `C:\workspace`
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("remote Windows root should fail for POSIX SSH runtime")
	}
	for _, host := range []string{"https://example.com", "user@example.com", "example.com:22", "example.com/path"} {
		repo.RootPath = "/srv/workspace"
		repo.Remote.Host = host
		if err := validateVirtualRepository(repo); err == nil {
			t.Fatalf("remote host %q should fail", host)
		}
	}
	repo.Remote.Host = "example.com"
	repo.RootPath = "/"
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("remote filesystem root should fail")
	}
}

func TestValidateVirtualRepositoryBranchAndTag(t *testing.T) {
	root := t.TempDir()
	for _, ref := range []VirtualRepositoryBinding{
		{Kind: "git", RelativePath: "git", RemoteURL: "https://example.com/repo.git", RefType: "branch", RefName: "feature/api", Enabled: true},
		{Kind: "git", RelativePath: "tag", RemoteURL: "https://example.com/repo.git", RefType: "tag", RefName: "v1.2.3", Enabled: true},
		{Kind: "svn", RelativePath: "svn", RemoteURL: "https://example.com/repo", RefType: "branch", RefName: "release/2026", Enabled: true},
	} {
		repo := &VirtualRepository{Version: 1, Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: ref.RelativePath, Name: ref.RelativePath, Repository: &ref}}}
		if err := validateVirtualRepository(repo); err != nil {
			t.Fatalf("ref %#v: %v", ref, err)
		}
	}
	for _, ref := range []VirtualRepositoryBinding{
		{Kind: "git", RelativePath: "git", RemoteURL: "https://example.com/repo.git", RefType: "branch", RefName: "../escape", Enabled: true},
		{Kind: "git", RelativePath: "git", RemoteURL: "https://example.com/repo.git", RefName: "main", Enabled: true},
		{Kind: "git", RelativePath: "git", RemoteURL: "https://example.com/repo.git", RefType: "branch", RefName: "feature..broken", Enabled: true},
		{Kind: "git", RelativePath: "git", RemoteURL: "https://example.com/repo.git", RefType: "tag", RefName: "release.lock", Enabled: true},
		{Kind: "svn", RelativePath: "svn", RemoteURL: "https://example.com/repo", RefType: "branch", RefName: "%2e%2e/escape", Enabled: true},
		{Kind: "svn", RelativePath: "svn", RemoteURL: "https://example.com/repo", RefType: "branch", RefName: "release/%2e%2e/escape", Enabled: true},
	} {
		repo := &VirtualRepository{Version: 1, Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "node", Name: "Node", Repository: &ref}}}
		if err := validateVirtualRepository(repo); err == nil {
			t.Fatalf("invalid ref %#v was accepted", ref)
		}
	}
	if got := svnRepositoryURLForBinding(&VirtualRepositoryBinding{RemoteURL: "https://example.com/repo/", RefType: "tag", RefName: "v1"}); got != "https://example.com/repo/tags/v1" {
		t.Fatalf("SVN tag URL=%q", got)
	}
	repo := &VirtualRepository{Version: 1, Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "default", Name: "Default", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "default", RemoteURL: "https://example.com/repo.git", RefType: "branch", Enabled: true}}}}
	if err := validateVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if repo.Nodes[0].Repository.RefType != "" {
		t.Fatalf("empty ref name should canonicalize ref type, got %q", repo.Nodes[0].Repository.RefType)
	}
}

func TestVirtualRepositoryTagWritesAreBlockedAtExecutionBoundary(t *testing.T) {
	binding := &VirtualRepositoryBinding{Kind: "svn", RefType: "tag", RefName: "v1", Enabled: true}
	for _, action := range []string{"commit", "push", "commit_push"} {
		err := validateVirtualRepositoryOperationForBinding(binding, action)
		if err == nil || classifyVirtualRepositoryOperationError(err) != "tag_read_only" {
			t.Fatalf("action %q error=%v", action, err)
		}
	}
	if err := validateVirtualRepositoryOperationForBinding(binding, "revert"); err != nil {
		t.Fatalf("tag revert should remain available: %v", err)
	}
}

func TestInspectVirtualRepositoryReportsMissingCheckoutSeparately(t *testing.T) {
	root := t.TempDir()
	status := (&App{}).inspectVirtualRepositoryNodeContext(context.Background(), root, VirtualRepositoryNode{
		ID: "missing", Name: "Missing",
		Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "not-created", RemoteURL: "https://example.com/repo.git", Enabled: true},
	})
	if status.ErrorCode != "not_checked_out" {
		t.Fatalf("missing checkout error code=%q, want not_checked_out (error=%q)", status.ErrorCode, status.Error)
	}
}

func TestInspectVirtualRepositoryDoesNotTreatBrokenSymlinkAsMissingCheckout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks is not generally available to unprivileged Windows tests")
	}
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "mapped")); err != nil {
		t.Fatal(err)
	}
	status := (&App{}).inspectVirtualRepositoryNodeContext(context.Background(), root, VirtualRepositoryNode{
		ID: "broken", Name: "Broken",
		Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "mapped", RemoteURL: "https://example.com/repo.git", Enabled: true},
	})
	if status.ErrorCode != "path_invalid" {
		t.Fatalf("broken symlink error code=%q, want path_invalid (error=%q)", status.ErrorCode, status.Error)
	}
}

func TestCollectVirtualRepositoryGitChangesIncludesFilesCommitsAndDiff(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	work := t.TempDir()
	runGitForTest(t, "", "init", "--bare", remote)
	remoteURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(remote), "/")
	runGitForTest(t, "", "clone", remoteURL, work)
	runGitForTest(t, work, "config", "user.name", "Test User")
	runGitForTest(t, work, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(work, "tracked.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, work, "add", "tracked.txt")
	runGitForTest(t, work, "commit", "-m", "initial commit")
	if err := os.WriteFile(filepath.Join(work, "tracked.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const untrackedName = "token=reviewable.txt"
	if err := os.WriteFile(filepath.Join(work, untrackedName), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	changes, err := collectVirtualRepositoryGitChanges(context.Background(), func(args ...string) (string, error) {
		return runVCSCommand(context.Background(), git, work, args...)
	}, func(args ...string) (string, error) {
		return runVCSCommandRaw(context.Background(), git, work, args...)
	}, "node", "main", "tracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	if changes.NodeID != "node" || len(changes.Commits) != 1 || changes.Commits[0].Subject != "initial commit" {
		t.Fatalf("changes commits = %+v", changes)
	}
	if len(changes.Files) != 2 || !virtualRepositoryChangeFileExists(changes.Files, untrackedName) {
		t.Fatalf("changed files = %+v, want tracked file and literal untracked path", changes.Files)
	}
	if !strings.Contains(changes.Diff, "-first") || !strings.Contains(changes.Diff, "+second") {
		t.Fatalf("diff = %q", changes.Diff)
	}
	untracked, err := collectVirtualRepositoryGitChanges(context.Background(), func(args ...string) (string, error) {
		return runVCSCommand(context.Background(), git, work, args...)
	}, func(args ...string) (string, error) {
		return runVCSCommandRaw(context.Background(), git, work, args...)
	}, "node", "main", untrackedName)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(untracked.Diff, "+new") {
		t.Fatalf("untracked diff = %q", untracked.Diff)
	}
}

func TestGetVirtualRepositoryChangesPreservesWhitespaceInSelectedPath(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "mapped")
	runGitForTest(t, "", "init", work)
	runGitForTest(t, work, "config", "user.name", "Test User")
	runGitForTest(t, work, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(work, "tracked.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, work, "add", "tracked.txt")
	runGitForTest(t, work, "commit", "-m", "initial commit")
	const selectedPath = " leading space.txt"
	if err := os.WriteFile(filepath.Join(work, selectedPath), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{Version: 1, ID: "repo", Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{{
		ID: "node", Name: "mapped", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "mapped", RemoteURL: "https://example.com/repo.git", Enabled: true},
	}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(virtualRepositoryChangesRequest{RootPath: root, NodeID: "node", FilePath: selectedPath})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := app.GetVirtualRepositoryChanges(string(request))
	if err != nil {
		t.Fatal(err)
	}
	var changes VirtualRepositoryChanges
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		t.Fatal(err)
	}
	if !virtualRepositoryChangeFileExists(changes.Files, selectedPath) || !strings.Contains(changes.Diff, "+new") {
		t.Fatalf("changes did not retain selected whitespace path: %+v", changes)
	}
}

func TestCollectVirtualRepositoryGitChangesSupportsUnbornHead(t *testing.T) {
	work := t.TempDir()
	runGitForTest(t, "", "init", work)
	const filePath = "first.txt"
	if err := os.WriteFile(filepath.Join(work, filePath), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	changes, err := collectVirtualRepositoryGitChanges(context.Background(), func(args ...string) (string, error) {
		return runVCSCommand(context.Background(), git, work, args...)
	}, func(args ...string) (string, error) {
		return runVCSCommandRaw(context.Background(), git, work, args...)
	}, "node", "", filePath)
	if err != nil {
		t.Fatal(err)
	}
	if changes.Head != "" || !virtualRepositoryChangeFileExists(changes.Files, filePath) || !strings.Contains(changes.Diff, "+first") {
		t.Fatalf("unborn-head changes = %+v", changes)
	}
}

func TestIsVirtualRepositoryGitUnbornHeadError(t *testing.T) {
	for _, message := range []string{"fatal: ambiguous argument 'HEAD': unknown revision", "fatal: Needed a single revision", "fatal: your current branch 'master' does not have any commits yet"} {
		if !isVirtualRepositoryGitUnbornHeadError(errors.New(message)) {
			t.Fatalf("%q was not recognized as an unborn HEAD error", message)
		}
	}
	if isVirtualRepositoryGitUnbornHeadError(errors.New("fatal: not a git repository")) {
		t.Fatal("non-HEAD failure was incorrectly accepted")
	}
}

func TestParseVirtualRepositoryGitStatusHandlesRename(t *testing.T) {
	files, truncated, err := parseVirtualRepositoryGitStatus("R  renamed.txt\x00original.txt\x00")
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(files) != 1 || files[0].Path != "renamed.txt" || files[0].OriginalPath != "original.txt" {
		t.Fatalf("files = %+v", files)
	}
}

func TestParseVirtualRepositoryGitStatusHandlesConflict(t *testing.T) {
	files, truncated, err := parseVirtualRepositoryGitStatus("UU conflict.txt\x00")
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(files) != 1 || files[0].IndexStatus != "U" || files[0].WorktreeStatus != "U" || files[0].Path != "conflict.txt" {
		t.Fatalf("files = %+v", files)
	}
}

func TestParseVirtualRepositoryGitStatusPreservesQuotedAndUnicodeFilenames(t *testing.T) {
	files, truncated, err := parseVirtualRepositoryGitStatus(" M 报告 \"final\".txt\x00")
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(files) != 1 || files[0].Path != "报告 \"final\".txt" {
		t.Fatalf("files = %+v", files)
	}
}

func TestParseVirtualRepositoryGitStatusRejectsControlCharacters(t *testing.T) {
	if _, _, err := parseVirtualRepositoryGitStatus(" M unsafe\nname\x00"); err == nil {
		t.Fatal("expected control character path to be rejected")
	}
}

func TestParseVirtualRepositoryGitStatusTruncatesLargeWorkingTree(t *testing.T) {
	var status strings.Builder
	status.WriteString("## main\x00")
	for i := 0; i < virtualRepositoryChangesMaxFiles+1; i++ {
		fmt.Fprintf(&status, " M file-%d.txt\x00", i)
	}
	files, truncated, err := parseVirtualRepositoryGitStatus(status.String())
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(files) != virtualRepositoryChangesMaxFiles {
		t.Fatalf("files=%d truncated=%v, want %d true", len(files), truncated, virtualRepositoryChangesMaxFiles)
	}
}

func TestPreviewVirtualRepositoryOperationBlocksGitTagWrites(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	remoteURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(remote), "/")
	runGitForTest(t, "", "init", "--bare", remote)
	work := filepath.Join(root, "tagged")
	runGitForTest(t, "", "clone", remoteURL, work)
	runGitForTest(t, work, "config", "user.name", "Test User")
	runGitForTest(t, work, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("tagged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, work, "add", "README.md")
	runGitForTest(t, work, "commit", "-m", "tagged")
	runGitForTest(t, work, "tag", "v1")
	runGitForTest(t, work, "checkout", "v1")
	repo := &VirtualRepository{Version: 1, ID: "repo", Name: "Repo", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "tag", Name: "Tag", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "tagged", RemoteURL: remoteURL, RefType: "tag", RefName: "v1", Enabled: true}}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir()}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(VirtualRepositoryOperationRequest{RootPath: root, Action: "commit", Message: "must fail"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := app.PreviewVirtualRepositoryOperation(string(request))
	if err != nil {
		t.Fatal(err)
	}
	var preview VirtualRepositoryOperationPreview
	if err := json.Unmarshal([]byte(raw), &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.Blocked || len(preview.Targets) != 1 || preview.Targets[0].ErrorCode != "tag_read_only" {
		t.Fatalf("unexpected tag preview: %+v", preview)
	}
}

func TestValidateRemoteVirtualRepositoryPathsRemainCaseSensitive(t *testing.T) {
	repo := &VirtualRepository{Version: 1, Name: "remote", RootPath: "/srv/workspace", Remote: &VirtualRepositoryRemote{Host: "example.com", User: "deploy"}, Nodes: []VirtualRepositoryNode{
		{ID: "upper", Name: "Upper", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "Build/Output", Enabled: true}},
		{ID: "lower", Name: "Lower", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "build/output", Enabled: true}},
	}}
	if err := validateVirtualRepository(repo); err != nil {
		t.Fatalf("POSIX remote paths with different case should be distinct: %v", err)
	}
}

func TestSanitizeRepositoryRemoteURL(t *testing.T) {
	got := sanitizeRepositoryRemoteURL("https://user:secret@example.com/repo.git?token=secret#fragment")
	if got != "https://example.com/repo.git" {
		t.Fatalf("unexpected sanitized URL %q", got)
	}
}

func TestVirtualRepositoryLogErrorRedactsSecretsAndRepositoryLocations(t *testing.T) {
	got := virtualRepositoryLogError(errors.New(`fatal: unable to access 'https://alice:secret@example.com/private/repo.git?token=hidden': password=hunter2; local C:\Users\alice\repo`))
	for _, secret := range []string{"alice", "secret", "private", "hidden", "hunter2", `C:\Users\alice\repo`} {
		if strings.Contains(got, secret) {
			t.Fatalf("log error leaked %q: %q", secret, got)
		}
	}
	if !strings.Contains(got, "[REPOSITORY_URL]") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("log error did not preserve useful redaction markers: %q", got)
	}
}

func TestVirtualRepositoryDirectoryStats(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "build")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "artifact.bin"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := (&App{}).GetVirtualRepositoryDirectoryStats(root, "build")
	if err != nil {
		t.Fatal(err)
	}
	var stats VirtualRepositoryDirectoryStats
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.FileCount != 1 || stats.SizeBytes != 5 {
		t.Fatalf("unexpected stats %#v", stats)
	}
}

func TestRunVCSOutputRedactionAndLimit(t *testing.T) {
	redacted := redactVCSOutput("svn+ssh://alice:secret@example.com/repo?access_token=query-secret password=hunter2 token=abc authorization: bearer-secret")
	for _, secret := range []string{"alice:secret", "query-secret", "hunter2", "abc", "bearer-secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q leaked in %q", secret, redacted)
		}
	}
	buffer := &limitedVCSBuffer{limit: 4}
	if n, err := buffer.Write([]byte("123456")); err != nil || n != 6 || buffer.String() != "1234\n[output truncated]" {
		t.Fatalf("limited buffer n=%d value=%q err=%v", n, buffer.String(), err)
	}
	if buffer.RawString() != "1234" {
		t.Fatalf("raw limited buffer = %q", buffer.RawString())
	}
}

func TestInspectVirtualRepositoryNodeContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	work := filepath.Join(root, "git")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	status := (&App{}).inspectVirtualRepositoryNodeContext(ctx, root, VirtualRepositoryNode{
		ID: "git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "git", Enabled: true},
	})
	if status.Error == "" || !errors.Is(errors.New(status.Error), context.Canceled) && !strings.Contains(strings.ToLower(status.Error), "canceled") {
		t.Fatalf("cancelled inspection status = %#v", status)
	}
}
