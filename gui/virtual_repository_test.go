package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

func TestValidateVirtualRepositoryRejectsCyclesAndDuplicatePaths(t *testing.T) {
	root := t.TempDir()
	repo := &VirtualRepository{Version: 1, Name: "x", RootPath: root, Nodes: []VirtualRepositoryNode{
		{ID: "a", ParentID: "b", Name: "A"},
		{ID: "b", ParentID: "a", Name: "B"},
	}}
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("cycle should fail")
	}
	repo.Nodes = []VirtualRepositoryNode{
		{ID: "a", Name: "A", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "out", Enabled: true}},
		{ID: "b", Name: "B", Repository: &VirtualRepositoryBinding{Kind: "local", RelativePath: "out", Enabled: true}},
	}
	if err := validateVirtualRepository(repo); err == nil {
		t.Fatal("duplicate mapped path should fail")
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

func TestValidateRemoteVirtualRepository(t *testing.T) {
	repo := &VirtualRepository{Version: 1, Name: "remote", RootPath: "/srv/workspace", Remote: &VirtualRepositoryRemote{Host: "example.com", User: "deploy"}, Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "services/api", RemoteURL: "https://example.com/api.git", Enabled: true}}}}
	if err := validateVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if repo.Remote.Port != 22 {
		t.Fatalf("default remote port=%d", repo.Remote.Port)
	}
	for _, invalid := range []string{"../escape", "/absolute", `.vrepo/secret`, `windows\path`} {
		if err := validateRemoteVirtualRepositoryRelativePath(invalid); err == nil {
			t.Fatalf("remote relative path %q should fail", invalid)
		}
	}
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
