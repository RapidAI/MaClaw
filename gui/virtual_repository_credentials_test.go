package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestRepositoryCredentialSecretStaysOutOfMetadataFiles(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	secret := "test-super-secret-value"
	raw, err := app.SaveRepositoryCredential(`{"name":"GitHub","kind":"git","username":"alice","scope":"github.com","secret":"` + secret + `"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, secret) {
		t.Fatal("save response exposed secret")
	}
	var metadata RepositoryCredentialMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		t.Fatal(err)
	}
	stored, err := keyring.Get(virtualRepositoryKeyringService, metadata.ID)
	if err != nil || stored != secret {
		t.Fatalf("keyring secret=%q err=%v", stored, err)
	}
	contains, err := repositoryCredentialFilesContainSecret(app.getMaclawBaseDir(), secret)
	if err != nil {
		t.Fatal(err)
	}
	if contains {
		t.Fatal("metadata files contain secret")
	}
}

func TestRepositoryCredentialStateRejectsUnsupportedVersions(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := writeJSONFile(app.repositoryCredentialPath(), repositoryCredentialFile{Version: 2, Items: []RepositoryCredentialMetadata{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListRepositoryCredentials(""); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("credential version error = %v", err)
	}
	if err := writeJSONFile(app.repositoryCredentialBindingsPath(), repositoryCredentialBindingFile{Version: 2, Bindings: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListRepositoryCredentialBindings("repo"); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("binding version error = %v", err)
	}
}

func TestRepositoryCredentialStateRejectsInvalidTimestamps(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	now := time.Now().UTC()
	file := repositoryCredentialFile{Version: 1, Items: []RepositoryCredentialMetadata{{ID: "cred", Name: "Credential", Kind: "git", Username: "alice", CreatedAt: now, UpdatedAt: now.Add(-time.Second)}}}
	if err := writeJSONFile(app.repositoryCredentialPath(), file); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListRepositoryCredentials(""); err == nil || !strings.Contains(err.Error(), "timestamps") {
		t.Fatalf("credential timestamp error = %v", err)
	}
}

func TestDeleteVirtualRepositoryRejectsCorruptBindingsBeforeChangingIndex(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	indexPath := app.virtualRepositoryStatePath("virtual-repositories-index.json")
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{ID: "repo", Name: "Repo", RootPath: t.TempDir()}}}
	if err := writeJSONFile(indexPath, index); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(app.repositoryCredentialBindingsPath(), repositoryCredentialBindingFile{Version: 2, Bindings: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteVirtualRepository("repo"); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("delete with corrupt bindings error = %v", err)
	}
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("delete changed index despite invalid binding state")
	}
}

func TestDeleteRepositoryCredentialSortsAffectedBindings(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	raw, err := app.SaveRepositoryCredential(`{"name":"Git","kind":"git","username":"alice","secret":"secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	var credential RepositoryCredentialMetadata
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		t.Fatal(err)
	}
	bindings := repositoryCredentialBindingFile{Version: 1, Bindings: map[string]string{"repo:z": credential.ID, "repo:a": credential.ID}}
	if err := writeJSONFile(app.repositoryCredentialBindingsPath(), bindings); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := app.DeleteRepositoryCredential(credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	var result repositoryCredentialDeleteResult
	if err := json.Unmarshal([]byte(resultRaw), &result); err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.AffectedBindings, ",") != "repo:a,repo:z" {
		t.Fatalf("affected bindings = %v", result.AffectedBindings)
	}
}

func TestRepositoryCredentialBindingAndDelete(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "wc"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{ID: "repo", Name: "Test", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "node", Name: "SVN", Repository: &VirtualRepositoryBinding{Kind: "svn", RelativePath: "wc", RemoteURL: "https://example.com/svn", Enabled: true}}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	raw, err := app.SaveRepositoryCredential(`{"name":"SVN","kind":"svn","username":"bob","secret":"pw"}`)
	if err != nil {
		t.Fatal(err)
	}
	var metadata RepositoryCredentialMetadata
	_ = json.Unmarshal([]byte(raw), &metadata)
	if err := app.SetRepositoryCredentialBinding("repo", "node", metadata.ID); err != nil {
		t.Fatal(err)
	}
	bindings, err := app.ListRepositoryCredentialBindings("repo")
	if err != nil || !strings.Contains(bindings, metadata.ID) {
		t.Fatalf("bindings=%s err=%v", bindings, err)
	}
	result, err := app.DeleteRepositoryCredential(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "repo:node") {
		t.Fatalf("delete result=%s", result)
	}
	bindings, _ = app.ListRepositoryCredentialBindings("repo")
	if strings.Contains(bindings, metadata.ID) {
		t.Fatalf("binding survived delete: %s", bindings)
	}
}

func TestRepositoryCredentialBindingRejectsKindMismatch(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := &VirtualRepository{ID: "repo", Name: "Test", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "git", Name: "Git", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "repo", RemoteURL: "https://example.com/repo.git", Enabled: true}}}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	raw, err := app.SaveRepositoryCredential(`{"name":"SVN","kind":"svn","username":"bob","secret":"pw"}`)
	if err != nil {
		t.Fatal(err)
	}
	var metadata RepositoryCredentialMetadata
	_ = json.Unmarshal([]byte(raw), &metadata)
	if err := app.SetRepositoryCredentialBinding(repo.ID, "git", metadata.ID); err == nil {
		t.Fatal("SVN credential should not bind to a Git repository")
	}
}

func TestRepositoryCredentialKindCannotChangeOnEdit(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	raw, err := app.SaveRepositoryCredential(`{"name":"Git","kind":"git","username":"alice","secret":"pw"}`)
	if err != nil {
		t.Fatal(err)
	}
	var metadata RepositoryCredentialMetadata
	_ = json.Unmarshal([]byte(raw), &metadata)
	_, err = app.SaveRepositoryCredential(`{"id":"` + metadata.ID + `","name":"Changed","kind":"svn","username":"alice"}`)
	if err == nil {
		t.Fatal("editing must not change credential kind and invalidate existing bindings")
	}
}

func TestRepositoryCredentialRejectsLineBreaks(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.SaveRepositoryCredential("{\"name\":\"Git\",\"kind\":\"git\",\"username\":\"alice\\nmalicious\",\"secret\":\"pw\"}"); err == nil {
		t.Fatal("credential line breaks should be rejected")
	}
}

func TestRepositoryCredentialScopeHostRestriction(t *testing.T) {
	for _, test := range []struct {
		scope, remote string
		allowed       bool
	}{
		{"github.com", "https://github.com/org/repo.git", true},
		{"https://github.com", "https://github.com/org/repo.git", true},
		{"github.com:443", "https://github.com/org/repo.git", true},
		{"github.com", "https://evil.example/repo.git", false},
		{"github.com", "git@github.com:org/repo.git", true},
		{"github.com", "git@evil.example:org/repo.git", false},
		{"127.0.0.1", "http://127.0.0.1/repo", true},
		{"VisualSVN Realm", "https://svn.example/repo", true},
	} {
		if got := repositoryCredentialScopeAllowsRemote(test.scope, test.remote); got != test.allowed {
			t.Fatalf("scope %q remote %q allowed=%v, want %v", test.scope, test.remote, got, test.allowed)
		}
	}
}

func TestRepositoryCredentialRejectsControlCharactersAndOversizedSecret(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	for _, input := range []saveRepositoryCredentialInput{
		{Name: "bad\tname", Kind: "git", Username: "alice", Secret: "pw"},
		{Name: "Git", Kind: "git", Username: "alice\x00admin", Secret: "pw"},
		{Name: "Git", Kind: "git", Username: "alice", Secret: strings.Repeat("x", virtualRepositoryFieldMaxLength+1)},
	} {
		raw, _ := json.Marshal(input)
		if _, err := app.SaveRepositoryCredential(string(raw)); err == nil {
			t.Fatalf("credential input %#v should be rejected", input)
		}
	}
}

func TestSaveVirtualRepositoryPrunesRemovedAndMismatchedCredentialBindings(t *testing.T) {
	keyring.MockInit()
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	repo := &VirtualRepository{ID: "repo", Name: "Test", RootPath: root, Nodes: []VirtualRepositoryNode{
		{ID: "keep", Name: "Keep", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "keep", RemoteURL: "https://example.com/keep.git", Enabled: true}},
		{ID: "remove", Name: "Remove", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "remove", RemoteURL: "https://example.com/remove.git", Enabled: true}},
	}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	raw, err := app.SaveRepositoryCredential(`{"name":"Git","kind":"git","username":"alice","secret":"pw"}`)
	if err != nil {
		t.Fatal(err)
	}
	var credential RepositoryCredentialMetadata
	_ = json.Unmarshal([]byte(raw), &credential)
	if err := app.SetRepositoryCredentialBinding(repo.ID, "keep", credential.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.SetRepositoryCredentialBinding(repo.ID, "remove", credential.ID); err != nil {
		t.Fatal(err)
	}

	repo.Nodes = []VirtualRepositoryNode{{ID: "keep", Name: "Keep", Repository: &VirtualRepositoryBinding{Kind: "svn", RelativePath: "keep", RemoteURL: "https://example.com/svn/keep", Enabled: true}}}
	input, _ := json.Marshal(repo)
	if _, err := app.SaveVirtualRepository(string(input)); err != nil {
		t.Fatal(err)
	}
	bindings, err := app.ListRepositoryCredentialBindings(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bindings != "{}" {
		t.Fatalf("stale bindings survived manifest save: %s", bindings)
	}
}
