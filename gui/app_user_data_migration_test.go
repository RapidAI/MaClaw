package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coreknowledge "github.com/RapidAI/CodeClaw/corelib/knowledge"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestUserDataMigrationMemoryRestoreReplacesExistingEntries(t *testing.T) {
	store, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	t.Cleanup(store.Stop)

	app := &App{memoryStore: store}
	if err := store.UpsertEntriesByID([]corememory.Entry{
		userDataMigrationTestEntry("old", "old migration memory"),
		userDataMigrationTestEntry("keep", "memory before migration"),
	}); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	if err := app.restoreUserDataMigrationMemory([]corememory.Entry{
		userDataMigrationTestEntry("keep", "memory after migration"),
		userDataMigrationTestEntry("new", "new migration memory"),
	}); err != nil {
		t.Fatalf("restore memory: %v", err)
	}

	got := map[string]string{}
	for _, entry := range store.List("", "") {
		got[entry.ID] = entry.Content
	}
	if _, ok := got["old"]; ok {
		t.Fatalf("old entry should have been removed: %#v", got)
	}
	if got["keep"] != "memory after migration" || got["new"] != "new migration memory" {
		t.Fatalf("unexpected restored memory: %#v", got)
	}
}

func TestUserDataMigrationKnowledgeAssetsRollbackRestoresExisting(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	oldPath := filepath.Join(app.GetDataDir(), "knowledge_assets", "source-old", "old.txt")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatalf("mkdir old assets: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("old asset"), 0o600); err != nil {
		t.Fatalf("write old asset: %v", err)
	}

	assetSrc := filepath.Join(t.TempDir(), "knowledge_assets")
	newPath := filepath.Join(assetSrc, "source-new", "new.txt")
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatalf("mkdir new assets: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new asset"), 0o600); err != nil {
		t.Fatalf("write new asset: %v", err)
	}

	_, rollback, commit, err := app.replaceUserDataMigrationKnowledgeAssets(assetSrc, t.TempDir())
	if err != nil {
		t.Fatalf("replace assets: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.GetDataDir(), "knowledge_assets", "source-new", "new.txt")); err != nil {
		t.Fatalf("new asset not installed: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old asset should be moved aside before rollback, err=%v", err)
	}

	if err := rollback(); err != nil {
		t.Fatalf("rollback assets: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old asset not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.GetDataDir(), "knowledge_assets", "source-new", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new asset should be removed by rollback, err=%v", err)
	}
	if commit != nil {
		commit()
	}
}

func TestUserDataMigrationReplaceDirectoryRejectsNestedBackup(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "pet-packs")
	if _, _, _, err := userDataMigrationReplaceDirectory(t.TempDir(), destination, filepath.Join(destination, "work"), "backup", "pet_packs"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected unsafe nested backup rejection, got %v", err)
	}
}

func TestUserDataMigrationReplaceDirectoryRejectsOverlappingSource(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "pet-packs")
	workDir := filepath.Join(root, "work")
	for _, source := range []string{destination, filepath.Join(destination, "payload"), root} {
		if _, _, _, err := userDataMigrationReplaceDirectory(source, destination, workDir, "backup", "pet_packs"); err == nil || !strings.Contains(err.Error(), "unsafe") {
			t.Fatalf("source %q: expected unsafe overlap rejection, got %v", source, err)
		}
	}
}

func TestUserDataMigrationDigestDirSortsFiles(t *testing.T) {
	root := t.TempDir()
	for name := range map[string]struct{}{"z.txt": {}, "nested/a.txt": {}, "a.txt": {}} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	digests, err := userDataMigrationDigestDir(root)
	if err != nil {
		t.Fatalf("digest directory: %v", err)
	}
	got := make([]string, 0, len(digests))
	for _, digest := range digests {
		got = append(got, digest.Path)
	}
	want := []string{"a.txt", "nested/a.txt", "z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("digest order = %v, want %v", got, want)
	}
}

func TestUserDataMigrationBuildRejectsInvalidKnowledgeAssetsPath(t *testing.T) {
	store, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	t.Cleanup(store.Stop)

	app := &App{testHomeDir: t.TempDir(), memoryStore: store}
	assetPath := filepath.Join(app.GetDataDir(), "knowledge_assets")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write invalid assets path: %v", err)
	}

	_, _, err = app.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID:    "tenant-a",
		UserID:      "user-a",
		MachineID:   "machine-a",
		MachineName: "Machine A",
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "knowledge_assets") {
		t.Fatalf("expected knowledge_assets error, got %v", err)
	}
}

func TestUserDataMigrationPackageKeepsAssetManifestFiles(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	assetPath := filepath.Join(source.GetDataDir(), "knowledge_assets", "source-a", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("mkdir asset path: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("asset manifest"), 0o600); err != nil {
		t.Fatalf("write asset manifest: %v", err)
	}

	zipPath, manifest, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID:    "tenant-a",
		UserID:      "user-a",
		MachineID:   "machine-a",
		MachineName: "Machine A",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	found := false
	for _, file := range manifest.Files {
		if file.Path == "knowledge_assets/source-a/manifest.json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("asset manifest file missing from package manifest: %#v", manifest.Files)
	}

	targetStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode target: %v", err)
	}
	t.Cleanup(targetStore.Stop)
	target := &App{testHomeDir: t.TempDir(), memoryStore: targetStore}
	if _, err := target.restoreUserDataMigrationPackage(context.Background(), zipPath, t.TempDir()); err != nil {
		t.Fatalf("restore package: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(target.GetDataDir(), "knowledge_assets", "source-a", "manifest.json"))
	if err != nil {
		t.Fatalf("read restored asset manifest: %v", err)
	}
	if string(restored) != "asset manifest" {
		t.Fatalf("restored asset manifest mismatch: %q", restored)
	}
}

func TestUserDataMigrationPackageMigratesPetPacks(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	packPath := filepath.Join(source.userDataMigrationPetPacksDir(), "custom-pet", "pet-pack.yaml")
	if err := os.MkdirAll(filepath.Dir(packPath), 0o755); err != nil {
		t.Fatalf("mkdir pet-pack path: %v", err)
	}
	if err := os.WriteFile(packPath, []byte("id: custom-pet\nname: Custom Pet\n"), 0o600); err != nil {
		t.Fatalf("write pet-pack: %v", err)
	}

	zipPath, manifest, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	if !manifest.PetPacksIncluded || manifest.PetPackBytes == 0 {
		t.Fatalf("pet-pack metadata missing: %#v", manifest)
	}
	found := false
	for _, file := range manifest.Files {
		if file.Path == "pet_packs/custom-pet/pet-pack.yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pet-pack file missing from package manifest: %#v", manifest.Files)
	}

	targetStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode target: %v", err)
	}
	t.Cleanup(targetStore.Stop)
	target := &App{testHomeDir: t.TempDir(), memoryStore: targetStore}
	oldPackPath := filepath.Join(target.userDataMigrationPetPacksDir(), "old-pet", "pet-pack.yaml")
	if err := os.MkdirAll(filepath.Dir(oldPackPath), 0o755); err != nil {
		t.Fatalf("mkdir target pet-pack path: %v", err)
	}
	if err := os.WriteFile(oldPackPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("write target pet-pack: %v", err)
	}
	result, err := target.restoreUserDataMigrationPackage(context.Background(), zipPath, t.TempDir())
	if err != nil {
		t.Fatalf("restore package: %v", err)
	}
	if _, err := os.Stat(oldPackPath); !os.IsNotExist(err) {
		t.Fatalf("old pet-pack should be replaced, err=%v", err)
	}
	restored, err := os.ReadFile(filepath.Join(target.userDataMigrationPetPacksDir(), "custom-pet", "pet-pack.yaml"))
	if err != nil {
		t.Fatalf("read restored pet-pack: %v", err)
	}
	if string(restored) != "id: custom-pet\nname: Custom Pet\n" {
		t.Fatalf("restored pet-pack mismatch: %q", restored)
	}
	petPacks, ok := result["pet_packs"].(map[string]interface{})
	if !ok || petPacks["included"] != true || petPacks["bytes"] != manifest.PetPackBytes {
		t.Fatalf("pet-pack restore result mismatch: %#v", result)
	}
}

func TestUserDataMigrationPackageMigratesExperts(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	sourceExperts := expertStoreFile{Experts: []ExpertDefinition{{
		ID: "expert-source", Name: "Source Expert", SystemPrompt: "source prompt", CreatedAt: "2026-08-03T00:00:00Z", UpdatedAt: "2026-08-03T00:00:00Z",
	}}}
	if err := userDataMigrationWriteJSONFile(filepath.Join(source.userDataMigrationExpertsDir(), "experts.json"), sourceExperts); err != nil {
		t.Fatalf("write source experts: %v", err)
	}

	zipPath, manifest, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	if !manifest.ExpertsIncluded || manifest.ExpertBytes == 0 {
		t.Fatalf("expert metadata missing: %#v", manifest)
	}
	found := false
	for _, file := range manifest.Files {
		if file.Path == "experts/experts.json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expert file missing from package manifest: %#v", manifest.Files)
	}

	targetStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode target: %v", err)
	}
	t.Cleanup(targetStore.Stop)
	target := &App{testHomeDir: t.TempDir(), memoryStore: targetStore}
	if err := userDataMigrationWriteJSONFile(filepath.Join(target.userDataMigrationExpertsDir(), "experts.json"), expertStoreFile{Experts: []ExpertDefinition{{ID: "expert-target", Name: "Target Expert"}}}); err != nil {
		t.Fatalf("write target experts: %v", err)
	}
	result, err := target.restoreUserDataMigrationPackage(context.Background(), zipPath, t.TempDir())
	if err != nil {
		t.Fatalf("restore package: %v", err)
	}
	var restored expertStoreFile
	if err := userDataMigrationReadJSONFileLimited(filepath.Join(target.userDataMigrationExpertsDir(), "experts.json"), &restored, userDataMigrationMaxConfigJSON); err != nil {
		t.Fatalf("read restored experts: %v", err)
	}
	if len(restored.Experts) != 1 || restored.Experts[0].ID != "expert-source" || restored.Experts[0].SystemPrompt != "source prompt" {
		t.Fatalf("restored experts mismatch: %#v", restored)
	}
	experts, ok := result["experts"].(map[string]interface{})
	if !ok || experts["included"] != true || experts["bytes"] != manifest.ExpertBytes {
		t.Fatalf("expert restore result mismatch: %#v", result)
	}
}

func TestUserDataMigrationManifestRejectsUndeclaredPetPackData(t *testing.T) {
	manifest := userDataMigrationManifest{
		Version:      userDataMigrationPackageVersion,
		AssetBytes:   0,
		PetPackBytes: 3,
		Files: []userDataMigrationFileDigest{
			{Path: "memory_entries.json"},
			{Path: "knowledge_snapshot.jsonl"},
			{Path: "config/app_config.json"},
			{Path: "config/migration_policy.json"},
			{Path: "config/secret_inventory.json"},
			{Path: "pet_packs/custom-pet/pet-pack.yaml", Bytes: 3},
		},
	}
	if err := validateUserDataMigrationManifestFileStats(manifest); err == nil || !strings.Contains(err.Error(), "without declaring") {
		t.Fatalf("expected undeclared pet-pack payload rejection, got %v", err)
	}
}

func TestUserDataMigrationManifestRejectsPetPackRootFile(t *testing.T) {
	manifest := userDataMigrationManifest{
		Version:          userDataMigrationPackageVersion,
		PetPacksIncluded: true,
		Files: []userDataMigrationFileDigest{
			{Path: "memory_entries.json"},
			{Path: "knowledge_snapshot.jsonl"},
			{Path: "config/app_config.json"},
			{Path: "config/migration_policy.json"},
			{Path: "config/secret_inventory.json"},
			{Path: "pet_packs"},
		},
	}
	if err := validateUserDataMigrationManifestFileStats(manifest); err == nil || !strings.Contains(err.Error(), "root must be a directory") {
		t.Fatalf("expected pet-pack root file rejection, got %v", err)
	}
}

func TestUserDataMigrationPackageRestoresFullConfigAndPreservesMachineIdentity(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	sourceConfig := corelib.AppConfig{
		Language:                    "zh-CN",
		DefaultProxyPassword:        "proxy-secret",
		MaclawLLMUrl:                "https://llm.example.com/v1",
		MaclawLLMKey:                "llm-secret-key",
		MaclawLLMModel:              "model-a",
		LLMTrajectoryLogging:        true,
		LogDetailEnabled:            true,
		BugReportEnabled:            true,
		BugReportPreviousTrajectory: false,
		BugReportPreviousLogDetail:  false,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "Custom Provider", URL: "https://provider.example.com/v1", Key: "provider-secret-key", Model: "provider-model",
			RefreshToken: "refresh-secret", OAuthAccessToken: "oauth-secret",
		}},
		RemoteHubURL:       "https://source-hub.example.com",
		RemoteEnabled:      false,
		RemoteMachineID:    "source-machine",
		RemoteMachineToken: "source-machine-token",
		RemoteViewerToken:  "source-viewer-token",
		WorkingDirectory:   `C:\source\workspace`,
	}
	if err := source.SaveConfig(sourceConfig); err != nil {
		t.Fatalf("save source config: %v", err)
	}
	zipPath, manifest, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID: "tenant-a", UserID: "user-a", MachineID: "source-machine",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	if manifest.ConfigSchema != userDataMigrationConfigSchema || manifest.SecretCount < 2 {
		t.Fatalf("config metadata missing: %#v", manifest)
	}

	targetStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode target: %v", err)
	}
	t.Cleanup(targetStore.Stop)
	target := &App{testHomeDir: t.TempDir(), memoryStore: targetStore}
	targetConfig := corelib.AppConfig{
		Language:             "en",
		LLMTrajectoryLogging: false,
		LogDetailEnabled:     false,
		RemoteHubURL:         "https://target-hub.example.com",
		RemoteEnabled:        true,
		RemoteMachineID:      "target-machine",
		RemoteMachineToken:   "target-machine-token",
		RemoteViewerToken:    "target-viewer-token",
		WorkingDirectory:     `D:\target\workspace`,
	}
	if err := target.SaveConfig(targetConfig); err != nil {
		t.Fatalf("save target config: %v", err)
	}
	result, err := target.restoreUserDataMigrationPackage(context.Background(), zipPath, t.TempDir())
	if err != nil {
		t.Fatalf("restore package: %v", err)
	}
	restored, err := target.LoadConfig()
	if err != nil {
		t.Fatalf("load restored config: %v", err)
	}
	if restored.MaclawLLMKey != "llm-secret-key" || restored.MaclawLLMUrl != sourceConfig.MaclawLLMUrl || restored.MaclawLLMModel != "model-a" {
		t.Fatalf("LLM configuration was not fully restored: key=%q url=%q model=%q", restored.MaclawLLMKey, restored.MaclawLLMUrl, restored.MaclawLLMModel)
	}
	if len(restored.MaclawLLMProviders) != 1 || restored.MaclawLLMProviders[0].Key != "provider-secret-key" || restored.MaclawLLMProviders[0].RefreshToken != "refresh-secret" || restored.MaclawLLMProviders[0].OAuthAccessToken != "oauth-secret" {
		t.Fatalf("LLM provider credentials were not fully restored: %#v", restored.MaclawLLMProviders)
	}
	if restored.DefaultProxyPassword != "proxy-secret" || restored.Language != "zh-CN" {
		t.Fatalf("system configuration was not restored: %#v", restored)
	}
	if !restored.BugReportEnabled || !restored.LLMTrajectoryLogging || !restored.LogDetailEnabled ||
		restored.BugReportPreviousTrajectory || restored.BugReportPreviousLogDetail {
		t.Fatalf("bug report collection state was not restored: %#v", restored)
	}
	stopped, err := target.SetBugReportEnabled(false)
	if err != nil {
		t.Fatalf("stop restored bug report collection: %v", err)
	}
	if stopped.BugReportEnabled || stopped.LLMTrajectoryLogging || stopped.LogDetailEnabled {
		t.Fatalf("restored bug report settings were not reversible: %#v", stopped)
	}
	if restored.RemoteMachineID != "target-machine" || restored.RemoteMachineToken != "target-machine-token" || restored.RemoteViewerToken != "target-viewer-token" || restored.RemoteHubURL != "https://target-hub.example.com" {
		t.Fatalf("target machine identity was overwritten: %#v", restored)
	}
	if !restored.RemoteEnabled {
		t.Fatalf("target Hub enablement was overwritten: %#v", restored)
	}
	if restored.WorkingDirectory != `D:\target\workspace` {
		t.Fatalf("target working directory was overwritten: %q", restored.WorkingDirectory)
	}
	configResult, _ := result["config"].(map[string]interface{})
	if configResult["secrets"].(int) < 2 {
		t.Fatalf("secret result missing: %#v", result)
	}
}

func TestUserDataMigrationSecretInventoryNeverContainsValues(t *testing.T) {
	out, paths, err := userDataMigrationExportableConfig(corelib.AppConfig{
		MaclawLLMKey:         "super-secret-api-key",
		DefaultProxyPassword: "proxy-password",
		RemoteMachineToken:   "must-not-migrate",
	})
	if err != nil {
		t.Fatalf("exportable config: %v", err)
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		t.Fatalf("marshal paths: %v", err)
	}
	if strings.Contains(string(encoded), "super-secret-api-key") || strings.Contains(string(encoded), "proxy-password") {
		t.Fatalf("secret inventory leaked a value: %s", encoded)
	}
	if _, ok := out["remote_machine_token"]; ok {
		t.Fatal("machine token must not be exported")
	}
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "maclaw_llm_key") || !strings.Contains(joined, "default_proxy_password") {
		t.Fatalf("expected secret paths missing: %#v", paths)
	}
}

func TestUserDataMigrationExportIncludesEmptyFieldsToClearTargetValues(t *testing.T) {
	out, _, err := userDataMigrationExportableConfig(corelib.AppConfig{})
	if err != nil {
		t.Fatalf("exportable config: %v", err)
	}
	for _, key := range []string{"maclaw_llm_providers", "mcp_servers", "ssh_hosts", "auxiliary_llm", "show_app_entry"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("empty portable field %q must be exported so it can clear the target", key)
		}
	}
	for _, key := range userDataMigrationExcludedConfigPaths() {
		if _, ok := out[key]; ok {
			t.Fatalf("excluded field %q must not be exported", key)
		}
	}
}

func TestUserDataMigrationEmptySourceConfigClearsTargetLLMProviders(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	target := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: "Target", Key: "target-key", Model: "target-model"}},
		MCPServers:         []corelib.MCPServerEntry{{ID: "target-mcp", Name: "Target MCP", AuthSecret: "target-secret"}},
		RemoteMachineID:    "target-machine",
	}
	if err := app.SaveConfig(target); err != nil {
		t.Fatalf("save target config: %v", err)
	}
	incoming, _, err := userDataMigrationExportableConfig(corelib.AppConfig{})
	if err != nil {
		t.Fatalf("build empty source config: %v", err)
	}
	if _, _, _, err := app.applyUserDataMigrationConfig(incoming); err != nil {
		t.Fatalf("apply empty source config: %v", err)
	}
	restored, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("load restored config: %v", err)
	}
	if len(restored.MaclawLLMProviders) != 0 || len(restored.MCPServers) != 0 {
		t.Fatalf("empty source collections did not clear target values: providers=%#v mcp=%#v", restored.MaclawLLMProviders, restored.MCPServers)
	}
	if restored.RemoteMachineID != "target-machine" {
		t.Fatalf("target machine identity was not preserved: %q", restored.RemoteMachineID)
	}
}

func TestUserDataMigrationRewriteTargetPathsArePreserved(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	target := corelib.AppConfig{
		DataDir:          `D:\target\data`,
		WorkingDirectory: `D:\target\workspace`,
		MaclawLLMKey:     "target-key",
	}
	if err := app.SaveConfig(target); err != nil {
		t.Fatalf("save target config: %v", err)
	}
	incoming, _, err := userDataMigrationExportableConfig(corelib.AppConfig{
		DataDir:          `/source/data`,
		WorkingDirectory: `/source/workspace`,
		MaclawLLMKey:     "source-key",
	})
	if err != nil {
		t.Fatalf("build source config: %v", err)
	}
	for _, key := range userDataMigrationRewriteTargetConfigPaths {
		if _, ok := incoming[key]; ok {
			t.Fatalf("rewrite-for-target field %q must not carry an unusable source path", key)
		}
	}
	if _, _, _, err := app.applyUserDataMigrationConfig(incoming); err != nil {
		t.Fatalf("apply source config: %v", err)
	}
	restored, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("load restored config: %v", err)
	}
	if restored.DataDir != target.DataDir || restored.WorkingDirectory != target.WorkingDirectory {
		t.Fatalf("target-local paths changed: data=%q work=%q", restored.DataDir, restored.WorkingDirectory)
	}
	if restored.MaclawLLMKey != "source-key" {
		t.Fatalf("portable LLM key was not restored: %q", restored.MaclawLLMKey)
	}
}

func TestUserDataMigrationRejectsUnknownConfigFields(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, _, _, err := app.applyUserDataMigrationConfig(map[string]interface{}{
		"maclaw_llm_key":          "source-key",
		"future_unhandled_secret": "must-not-be-persisted",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("expected unknown config field rejection, got %v", err)
	}
}

func TestUserDataMigrationSecretInventoryRecognizesCamelCaseAPIKey(t *testing.T) {
	paths := userDataMigrationSecretPaths(map[string]interface{}{
		"models": []interface{}{map[string]interface{}{"apiKey": "camel-case-secret"}},
	}, "")
	if strings.Join(paths, ",") != "models[0].apiKey" {
		t.Fatalf("camelCase API key missing from secret inventory: %#v", paths)
	}
}

func TestUserDataMigrationSecretInventoryRecognizesCustomAuthenticationFields(t *testing.T) {
	paths := userDataMigrationSecretPaths(map[string]interface{}{
		"mcp_servers": []interface{}{map[string]interface{}{
			"headers": map[string]interface{}{
				"Cookie":       "session=secret-cookie",
				"X-Credential": "credential-value",
				"X-Bearer":     "bearer-value",
				"privateKey":   "private-key-value",
				"refreshToken": "refresh-token-value",
				"tokenBudget":  4096,
				"X-Trace":      "not-a-secret",
			},
		}},
	}, "")
	want := []string{
		"mcp_servers[0].headers.Cookie",
		"mcp_servers[0].headers.X-Bearer",
		"mcp_servers[0].headers.X-Credential",
		"mcp_servers[0].headers.privateKey",
		"mcp_servers[0].headers.refreshToken",
	}
	if !userDataMigrationStringSlicesEqual(paths, want) {
		t.Fatalf("custom authentication secrets missing from inventory: got %#v want %#v", paths, want)
	}
}

func TestUserDataMigrationConfigRollbackRestoresTargetState(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	target := corelib.AppConfig{
		Language:           "en",
		MaclawLLMKey:       "target-llm-key",
		RemoteHubURL:       "https://target-hub.example.com",
		RemoteEnabled:      true,
		RemoteMachineID:    "target-machine",
		RemoteMachineToken: "target-machine-token",
		WorkingDirectory:   `D:\target\workspace`,
		Projects:           []corelib.ProjectConfig{{Id: "target-project", Name: "Target Project", Path: `D:\target\project`}},
		CurrentProject:     "target-project",
		AudioInputDeviceID: "target-microphone",
	}
	if err := app.SaveConfig(target); err != nil {
		t.Fatalf("save target config: %v", err)
	}
	incoming, _, err := userDataMigrationExportableConfig(corelib.AppConfig{
		Language:           "zh-CN",
		MaclawLLMKey:       "source-llm-key",
		RemoteHubURL:       "https://source-hub.example.com",
		RemoteEnabled:      false,
		RemoteMachineID:    "source-machine",
		RemoteMachineToken: "source-machine-token",
		WorkingDirectory:   `C:\source\workspace`,
		Projects:           []corelib.ProjectConfig{{Id: "source-project", Name: "Source Project", Path: `C:\source\project`}},
		CurrentProject:     "source-project",
		AudioInputDeviceID: "source-microphone",
	})
	if err != nil {
		t.Fatalf("build incoming config: %v", err)
	}
	_, _, rollback, err := app.applyUserDataMigrationConfig(incoming)
	if err != nil {
		t.Fatalf("apply migration config: %v", err)
	}
	migrated, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	if migrated.MaclawLLMKey != "source-llm-key" || migrated.RemoteMachineID != "target-machine" || !migrated.RemoteEnabled ||
		migrated.CurrentProject != "target-project" || len(migrated.Projects) != 1 || migrated.Projects[0].Path != `D:\target\project` ||
		migrated.AudioInputDeviceID != "target-microphone" {
		t.Fatalf("unexpected migrated config: %#v", migrated)
	}
	if rollback == nil {
		t.Fatal("expected config rollback")
	}
	if err := rollback(); err != nil {
		t.Fatalf("rollback config: %v", err)
	}
	restored, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("load rolled-back config: %v", err)
	}
	if restored.Language != target.Language || restored.MaclawLLMKey != target.MaclawLLMKey ||
		restored.RemoteHubURL != target.RemoteHubURL || restored.RemoteMachineID != target.RemoteMachineID ||
		restored.RemoteMachineToken != target.RemoteMachineToken || restored.RemoteEnabled != target.RemoteEnabled || restored.WorkingDirectory != target.WorkingDirectory ||
		restored.CurrentProject != target.CurrentProject || len(restored.Projects) != 1 || restored.Projects[0].Path != target.Projects[0].Path ||
		restored.AudioInputDeviceID != target.AudioInputDeviceID {
		t.Fatalf("target config was not fully restored: %#v", restored)
	}
}

func TestUserDataMigrationKnowledgeRollbackRestoresTargetSnapshot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	original, err := app.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title: "Target knowledge", Text: "knowledge that must survive rollback",
		SaveScope: coreknowledge.SaveScopePersonal, DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed target knowledge: %v", err)
	}
	workDir := t.TempDir()
	rollback, err := app.prepareUserDataMigrationKnowledgeRollback(workDir)
	if err != nil {
		t.Fatalf("prepare rollback: %v", err)
	}
	replacementPath := filepath.Join(workDir, "replacement.jsonl")
	other := &App{testHomeDir: t.TempDir()}
	if _, err := other.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title: "Migrated knowledge", Text: "temporary imported knowledge",
		SaveScope: coreknowledge.SaveScopePersonal, DistillMode: coreknowledge.DistillModeRules,
	}); err != nil {
		t.Fatalf("seed replacement knowledge: %v", err)
	}
	if _, err := other.KnowledgeExportSnapshotWithOptions(coreknowledge.ExportOptions{OutputPath: replacementPath}); err != nil {
		t.Fatalf("export replacement: %v", err)
	}
	if _, err := app.importUserDataMigrationKnowledgeSnapshot(replacementPath, workDir); err != nil {
		t.Fatalf("import replacement: %v", err)
	}
	if err := rollback(); err != nil {
		t.Fatalf("rollback knowledge: %v", err)
	}
	verifyPath := filepath.Join(workDir, "verified.jsonl")
	if _, err := app.KnowledgeExportSnapshotWithOptions(coreknowledge.ExportOptions{OutputPath: verifyPath}); err != nil {
		t.Fatalf("export restored knowledge: %v", err)
	}
	data, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(original.ID)) || bytes.Contains(data, []byte("temporary imported knowledge")) {
		t.Fatalf("knowledge rollback did not restore the target snapshot: %s", data)
	}
}

func TestUserDataMigrationPlaintextDirectoriesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}

	jsonPath := filepath.Join(t.TempDir(), "payload", "config", "app_config.json")
	if err := userDataMigrationWriteJSONFile(jsonPath, map[string]interface{}{"api_key": "secret"}); err != nil {
		t.Fatalf("write migration JSON: %v", err)
	}
	assertUserDataMigrationPrivateDir(t, filepath.Dir(jsonPath))

	zipPath := filepath.Join(t.TempDir(), "migration.zip")
	out, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create("config/app_config.json")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(`{"api_key":"secret"}`)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "unzipped")
	if err := userDataMigrationUnzipToDir(zipPath, dest, 1024); err != nil {
		t.Fatalf("unzip migration package: %v", err)
	}
	assertUserDataMigrationPrivateDir(t, filepath.Join(dest, "config"))
}

func TestUserDataMigrationConfigMetadataRejectsIncompatiblePolicy(t *testing.T) {
	payloadDir := t.TempDir()
	configDir := filepath.Join(payloadDir, "config")
	policy := userDataMigrationConfigPolicy{
		SchemaVersion:  userDataMigrationConfigSchema,
		Restore:        "all_fields_except_explicit_exclusions",
		PreserveTarget: append([]string(nil), userDataMigrationPreserveTargetConfigPaths...),
		RewriteTarget:  append([]string(nil), userDataMigrationRewriteTargetConfigPaths...),
		SkipRuntime:    append([]string(nil), userDataMigrationSkipRuntimeConfigPaths...),
	}
	policy.PreserveTarget = policy.PreserveTarget[1:]
	if err := userDataMigrationWriteJSONFile(filepath.Join(configDir, "migration_policy.json"), policy); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	if err := userDataMigrationWriteJSONFile(filepath.Join(configDir, "secret_inventory.json"), userDataMigrationSecretInventory{
		SchemaVersion: userDataMigrationConfigSchema,
	}); err != nil {
		t.Fatalf("write inventory: %v", err)
	}
	err := validateUserDataMigrationConfigMetadata(payloadDir, userDataMigrationManifest{
		SecretCount:    0,
		ExcludedConfig: userDataMigrationExcludedConfigPaths(),
	}, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "policy is incompatible") {
		t.Fatalf("expected incompatible policy error, got %v", err)
	}
}

func TestUserDataMigrationConfigMetadataRejectsExcludedPathMismatch(t *testing.T) {
	payloadDir := t.TempDir()
	configDir := filepath.Join(payloadDir, "config")
	if err := userDataMigrationWriteJSONFile(filepath.Join(configDir, "migration_policy.json"), userDataMigrationConfigPolicy{
		SchemaVersion:  userDataMigrationConfigSchema,
		Restore:        "all_fields_except_explicit_exclusions",
		PreserveTarget: append([]string(nil), userDataMigrationPreserveTargetConfigPaths...),
		RewriteTarget:  append([]string(nil), userDataMigrationRewriteTargetConfigPaths...),
		SkipRuntime:    append([]string(nil), userDataMigrationSkipRuntimeConfigPaths...),
	}); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	if err := userDataMigrationWriteJSONFile(filepath.Join(configDir, "secret_inventory.json"), userDataMigrationSecretInventory{
		SchemaVersion: userDataMigrationConfigSchema,
	}); err != nil {
		t.Fatalf("write inventory: %v", err)
	}
	err := validateUserDataMigrationConfigMetadata(payloadDir, userDataMigrationManifest{
		ExcludedConfig: []string{"remote_machine_id"},
	}, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "excluded configuration metadata") {
		t.Fatalf("expected excluded metadata mismatch, got %v", err)
	}
}

func TestUserDataMigrationEncryptionRequiresPasswordAndPlainHash(t *testing.T) {
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "migration.zip")
	plain := []byte("migration package bytes")
	if err := os.WriteFile(plainPath, plain, 0o600); err != nil {
		t.Fatalf("write plain package: %v", err)
	}
	plainHash, _, err := userDataMigrationFileSHA256(plainPath)
	if err != nil {
		t.Fatalf("hash plain package: %v", err)
	}

	encryptedPath := filepath.Join(dir, "migration.mlawenc")
	if err := encryptUserDataMigrationFile(plainPath, encryptedPath, "correct-password", plainHash); err != nil {
		t.Fatalf("encrypt package: %v", err)
	}

	if err := decryptUserDataMigrationFile(encryptedPath, filepath.Join(dir, "wrong-password.zip"), "wrong-password", plainHash); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("expected wrong password error, got %v", err)
	}
	if err := decryptUserDataMigrationFile(encryptedPath, filepath.Join(dir, "wrong-hash.zip"), "correct-password", strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}

	restoredPath := filepath.Join(dir, "restored.zip")
	if err := decryptUserDataMigrationFile(encryptedPath, restoredPath, "correct-password", plainHash); err != nil {
		t.Fatalf("decrypt package: %v", err)
	}
	restored, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("read restored package: %v", err)
	}
	if string(restored) != string(plain) {
		t.Fatalf("restored package mismatch: %q", restored)
	}
}

func TestUserDataMigrationEncryptedHeaderAcceptsLegacyV1(t *testing.T) {
	header := userDataMigrationEncryptedHeader{
		Version: userDataMigrationLegacyVersion, KDF: "argon2id", Time: 3,
		MemoryKB: 64 * 1024, Threads: 4, Salt: make([]byte, 16), Nonce: make([]byte, 12),
	}
	if err := validateUserDataMigrationEncryptedHeader(header); err != nil {
		t.Fatalf("legacy v1 encrypted package should remain importable: %v", err)
	}
}

func TestUserDataMigrationPasswordPolicy(t *testing.T) {
	for _, password := range []string{"short1", "abcdefghijkl", "123456789012"} {
		if err := validateUserDataMigrationPassword(password); err == nil {
			t.Fatalf("password %q should be rejected", password)
		}
	}
	if err := validateUserDataMigrationPassword("migration-2026"); err != nil {
		t.Fatalf("strong password rejected: %v", err)
	}
}

func TestUserDataMigrationKnowledgeImportReplacesExistingSnapshot(t *testing.T) {
	target := &App{testHomeDir: t.TempDir()}
	oldSource, err := target.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Old local note",
		Text:        "old local migration note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed old knowledge: %v", err)
	}

	source := &App{testHomeDir: t.TempDir()}
	newSource, err := source.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Migrated note",
		Text:        "new migrated knowledge note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed migrated knowledge: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "knowledge_snapshot.jsonl")
	if _, err := source.KnowledgeExportSnapshotWithOptions(coreknowledge.ExportOptions{OutputPath: snapshotPath}); err != nil {
		t.Fatalf("export migrated snapshot: %v", err)
	}

	if _, err := target.importUserDataMigrationKnowledgeSnapshot(snapshotPath, t.TempDir()); err != nil {
		t.Fatalf("import migrated snapshot: %v", err)
	}
	sources := userDataMigrationKnowledgeSourceIDs(t, target)
	if _, ok := sources[oldSource.ID]; ok {
		t.Fatalf("old knowledge source should be replaced, sources=%#v", sources)
	}
	if _, ok := sources[newSource.ID]; !ok {
		t.Fatalf("migrated knowledge source missing, sources=%#v", sources)
	}
}

func TestUserDataMigrationKnowledgeImportDoesNotDeleteOpenDatabaseFile(t *testing.T) {
	target := &App{testHomeDir: t.TempDir()}
	oldSource, err := target.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Old local note",
		Text:        "old local migration note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed old knowledge: %v", err)
	}
	pinnedStore, err := target.openKnowledgeStore()
	if err != nil {
		t.Fatalf("open pinned knowledge store: %v", err)
	}
	defer pinnedStore.Close()

	source := &App{testHomeDir: t.TempDir()}
	newSource, err := source.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Migrated note",
		Text:        "new migrated knowledge note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed migrated knowledge: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "knowledge_snapshot.jsonl")
	if _, err := source.KnowledgeExportSnapshotWithOptions(coreknowledge.ExportOptions{OutputPath: snapshotPath}); err != nil {
		t.Fatalf("export migrated snapshot: %v", err)
	}

	if _, err := target.importUserDataMigrationKnowledgeSnapshot(snapshotPath, t.TempDir()); err != nil {
		t.Fatalf("import migrated snapshot with open database handle: %v", err)
	}
	sources := userDataMigrationKnowledgeSourceIDs(t, target)
	if _, ok := sources[oldSource.ID]; ok {
		t.Fatalf("old knowledge source should be replaced, sources=%#v", sources)
	}
	if _, ok := sources[newSource.ID]; !ok {
		t.Fatalf("migrated knowledge source missing, sources=%#v", sources)
	}
}

func TestUserDataMigrationKnowledgeImportRollsBackOnFailure(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	oldSource, err := app.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Old local note",
		Text:        "old local note survives failed migration",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed old knowledge: %v", err)
	}
	brokenPath := filepath.Join(t.TempDir(), "broken_snapshot.jsonl")
	if err := os.WriteFile(brokenPath, []byte(`{"type":"node","data":{"id":"node_missing_source","source_id":"missing_source","text":"broken"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write broken snapshot: %v", err)
	}

	if _, err := app.importUserDataMigrationKnowledgeSnapshot(brokenPath, t.TempDir()); err == nil {
		t.Fatal("expected broken snapshot import to fail")
	}
	sources := userDataMigrationKnowledgeSourceIDs(t, app)
	if _, ok := sources[oldSource.ID]; !ok {
		t.Fatalf("old knowledge source should be restored after failed import, sources=%#v", sources)
	}
}

func TestUserDataMigrationKnowledgeValidationUsesIsolatedStore(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	existingSource, err := app.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Existing local note",
		Text:        "local note should not satisfy migration snapshot references",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed existing knowledge: %v", err)
	}
	brokenPath := filepath.Join(t.TempDir(), "broken_snapshot.jsonl")
	if err := os.WriteFile(brokenPath, []byte(`{"type":"node","data":{"id":"node_missing_package_source","source_id":"`+existingSource.ID+`","text":"broken"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write broken snapshot: %v", err)
	}

	if err := app.validateUserDataMigrationKnowledgeSnapshot(brokenPath); err == nil {
		t.Fatal("expected isolated validation to reject missing package source")
	}
}

func TestUserDataMigrationUnzipRejectsExpandedSizeLimit(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "migration.zip")
	out, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create("payload.txt")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte("too large")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	if err := userDataMigrationUnzipToDir(zipPath, filepath.Join(t.TempDir(), "out"), 4); err == nil {
		t.Fatal("expected expanded size limit error")
	}
}

func TestUserDataMigrationUnzipRejectsDuplicateAndNonCanonicalEntries(t *testing.T) {
	for _, names := range [][]string{{"same.txt", "same.txt"}, {"dir\\file.txt"}} {
		zipPath := filepath.Join(t.TempDir(), "migration.zip")
		out, err := os.Create(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(out)
		for _, name := range names {
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte("x"))
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
		if err := userDataMigrationUnzipToDir(zipPath, filepath.Join(t.TempDir(), "out"), 1024); err == nil {
			t.Fatalf("expected entry names %v to be rejected", names)
		}
	}
}

func TestUserDataMigrationVerifyFileDigestsRejectsAliases(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.json")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, size, err := userDataMigrationFileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	base := userDataMigrationFileDigest{Path: "data.json", Bytes: size, SHA256: hash}
	for _, files := range [][]userDataMigrationFileDigest{
		{base, base},
		{{Path: "data\\json", Bytes: size, SHA256: hash}},
		{{Path: "manifest.json", Bytes: size, SHA256: hash}},
	} {
		if err := userDataMigrationVerifyFileDigests(root, files); err == nil {
			t.Fatalf("expected invalid digest paths to be rejected: %#v", files)
		}
	}
}

func TestUserDataMigrationCanonicalPathsArePortableAcrossCaseSensitiveHosts(t *testing.T) {
	_, first, err := userDataMigrationCanonicalRelativePath("Config/app.json")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := userDataMigrationCanonicalRelativePath("config/app.json")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("portable identities differ: %q != %q", first, second)
	}
}

func TestUserDataMigrationCanonicalPathsRejectWindowsAliases(t *testing.T) {
	for _, name := range []string{"C:/config.json", "config./app.json", "config /app.json", "NUL", "assets/COM1.txt", "assets/bad\x01.txt"} {
		if _, _, err := userDataMigrationCanonicalRelativePath(name); err == nil {
			t.Fatalf("expected non-portable path to be rejected: %q", name)
		}
	}
}

func TestUserDataMigrationDownloadMetadataIsStrict(t *testing.T) {
	valid := map[string]interface{}{
		"chunk_count": float64(1), "chunk_size": float64(userDataMigrationChunkSize),
		"encrypted_size": float64(42), "compressed_size": float64(21), "encrypted_sha256": strings.Repeat("a", 64), "plain_sha256": strings.Repeat("b", 64),
	}
	if _, _, _, _, _, _, err := userDataMigrationDownloadMetadata(valid); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	invalid := []map[string]interface{}{
		{"chunk_count": 1.5, "chunk_size": float64(userDataMigrationChunkSize), "encrypted_size": float64(42), "compressed_size": float64(21), "encrypted_sha256": strings.Repeat("a", 64), "plain_sha256": strings.Repeat("b", 64)},
		{"chunk_count": float64(2), "chunk_size": float64(userDataMigrationChunkSize), "encrypted_size": float64(42), "compressed_size": float64(21), "encrypted_sha256": strings.Repeat("a", 64), "plain_sha256": strings.Repeat("b", 64)},
		{"chunk_count": float64(1), "chunk_size": float64(userDataMigrationChunkSize), "encrypted_size": float64(42), "compressed_size": float64(21), "encrypted_sha256": "bad", "plain_sha256": strings.Repeat("b", 64)},
		{"chunk_count": float64(1), "chunk_size": float64(userDataMigrationChunkSize), "encrypted_size": float64(42), "compressed_size": float64(43), "encrypted_sha256": strings.Repeat("a", 64), "plain_sha256": strings.Repeat("b", 64)},
		{"chunk_count": float64(1), "chunk_size": float64(userDataMigrationChunkSize), "encrypted_size": float64((1 << 20) + 66), "compressed_size": float64(1), "encrypted_sha256": strings.Repeat("a", 64), "plain_sha256": strings.Repeat("b", 64)},
	}
	for _, metadata := range invalid {
		if _, _, _, _, _, _, err := userDataMigrationDownloadMetadata(metadata); err == nil {
			t.Fatalf("invalid metadata accepted: %#v", metadata)
		}
	}
}

func TestUserDataMigrationDecryptRejectsDeclaredPlainSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "plain.zip")
	if err := os.WriteFile(plainPath, []byte("plain migration package"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, size, err := userDataMigrationFileSHA256(plainPath)
	if err != nil {
		t.Fatal(err)
	}
	encryptedPath := filepath.Join(dir, "package.enc")
	if err := encryptUserDataMigrationFile(plainPath, encryptedPath, "correct-password", hash); err != nil {
		t.Fatal(err)
	}
	if err := decryptUserDataMigrationFile(encryptedPath, filepath.Join(dir, "out.zip"), "correct-password", hash, size+1); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("expected declared plain size mismatch, got %v", err)
	}
}

func TestUserDataMigrationDecryptRejectsAmbiguousEncryptedHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ambiguous.enc")
	header := `{"version":"` + userDataMigrationPackageVersion + `","Version":"shadow","kdf":"argon2id","time":3,"memory_kb":65536,"threads":4,"salt":"AAAAAAAAAAAAAAAAAAAAAA==","nonce":"AAAAAAAAAAAAAAAA","plain_sha256":"` + strings.Repeat("a", 64) + `"}`
	var raw bytes.Buffer
	raw.WriteString(userDataMigrationMagic)
	if err := binary.Write(&raw, binary.BigEndian, uint32(len(header))); err != nil {
		t.Fatal(err)
	}
	raw.WriteString(header)
	if err := os.WriteFile(path, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	err := decryptUserDataMigrationFile(path, filepath.Join(dir, "out.zip"), "password", "")
	if err == nil || !strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("expected ambiguous encrypted header rejection, got %v", err)
	}
}

func TestUserDataMigrationHubBytesLimitedRejectsOversizedChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))
	defer srv.Close()
	app := &App{}
	_, err := app.userDataMigrationHubBytesLimited(context.Background(), userDataMigrationClientConfig{
		HubURL: srv.URL, MachineID: "machine", MachineToken: "token",
	}, http.MethodGet, "/chunk", nil, 4, "")
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("expected oversized response rejection, got %v", err)
	}
}

func TestUserDataMigrationRetryableLocalRestoreError(t *testing.T) {
	for _, message := range []string{
		"database is locked",
		"database table is locked: knowledge",
		"The process cannot access the file because it is being used by another process",
		"resource temporarily unavailable",
	} {
		if !userDataMigrationRetryableLocalRestoreError(errors.New(message)) {
			t.Fatalf("expected retryable local restore error for %q", message)
		}
	}
	for _, message := range []string{
		"migration package hash mismatch",
		"no space left on device",
		"migration password is incorrect",
	} {
		if userDataMigrationRetryableLocalRestoreError(errors.New(message)) {
			t.Fatalf("unexpected retryable local restore error for %q", message)
		}
	}
}

func TestUserDataMigrationRetryableTransferError(t *testing.T) {
	for _, message := range []string{
		"connection reset by peer",
		"unexpected EOF",
		"Hub migration API GET /chunk returned 503: request failed",
		"Hub migration API GET /chunk returned 429: request failed",
	} {
		if !userDataMigrationRetryableTransferError(errors.New(message)) {
			t.Fatalf("expected retryable transfer error for %q", message)
		}
	}
	for _, message := range []string{
		"Hub migration API GET /chunk returned 401: request failed",
		"downloaded migration chunk 0 size mismatch",
		"migration password is incorrect",
	} {
		if userDataMigrationRetryableTransferError(errors.New(message)) {
			t.Fatalf("unexpected retryable transfer error for %q", message)
		}
	}
}

func TestUserDataMigrationRepairKnowledgeSnapshotDropsOnlyOrphans(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "knowledge_snapshot.jsonl")
	outputPath := filepath.Join(dir, "knowledge_snapshot.repaired.jsonl")
	lines := []string{
		`{"type":"source","data":{"id":"ksrc_repair","kind":"text","uri":"knowledge://text/repair","content_hash":"repair","status":"distilled"}}`,
		`{"type":"node","data":{"id":"kdn_repair_valid","source_id":"ksrc_repair","type":"document","text":"valid node"}}`,
		`{"type":"card","data":{"id":"kcard_repair_valid","source_id":"ksrc_repair","node_id":"kdn_repair_valid","claim":"valid card"}}`,
		`{"type":"card","data":{"id":"kcard_repair_orphan","source_id":"ksrc_repair","node_id":"kdn_repair_missing","claim":"orphan card"}}`,
		`{"type":"fact","data":{"id":"kfact_repair_valid","card_id":"kcard_repair_valid","source_id":"ksrc_repair","subject":"valid","predicate":"is","object":"kept"}}`,
		`{"type":"fact","data":{"id":"kfact_repair_orphan","card_id":"kcard_repair_orphan","source_id":"ksrc_repair","subject":"orphan","predicate":"is","object":"dropped"}}`,
	}
	if err := os.WriteFile(inputPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	gotPath, repair, err := userDataMigrationRepairKnowledgeSnapshot(inputPath, outputPath)
	if err != nil {
		t.Fatalf("userDataMigrationRepairKnowledgeSnapshot: %v", err)
	}
	if gotPath != outputPath || repair["orphaned_cards"] != 1 || repair["orphaned_facts"] != 1 {
		t.Fatalf("unexpected repair result: path=%q repair=%#v", gotPath, repair)
	}
	repaired, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"kcard_repair_valid", "kfact_repair_valid"} {
		if !strings.Contains(string(repaired), id) {
			t.Fatalf("repaired snapshot lost valid record %q: %s", id, repaired)
		}
	}
	for _, id := range []string{"kcard_repair_orphan", "kfact_repair_orphan"} {
		if strings.Contains(string(repaired), id) {
			t.Fatalf("repaired snapshot retained orphan record %q: %s", id, repaired)
		}
	}
	if err := (&App{testHomeDir: dir}).validateUserDataMigrationKnowledgeSnapshot(outputPath); err != nil {
		t.Fatalf("repaired snapshot should validate: %v", err)
	}
}

// TestLiveUserDataMigrationDryRun is intentionally run only by name while
// diagnosing a user's migration package. It downloads and validates the
// current Hub package but never writes local config, memory, knowledge, or
// assets. The claim made to read encrypted chunks is released by the dry-run.
func TestLiveUserDataMigrationDryRun(t *testing.T) {
	password := os.Getenv("MACLAW_MIGRATION_DRY_RUN_PASSWORD")
	if password == "" {
		t.Skip("set MACLAW_MIGRATION_DRY_RUN_PASSWORD to validate the current Hub migration package")
	}
	app := &App{}
	cfg, err := app.userDataMigrationConfig()
	if err != nil {
		t.Fatal(err)
	}
	export, _, err := app.userDataMigrationHubGetCurrent(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	exportMap, ok := export.(map[string]interface{})
	if !ok {
		t.Fatalf("current migration export is unavailable: %#v", export)
	}
	exportID := strings.TrimSpace(fmt.Sprint(exportMap["export_id"]))
	if exportID == "" {
		t.Fatalf("current migration export has no export_id: %#v", exportMap)
	}
	result, err := app.dryRunUserDataMigrationImport(context.Background(), cfg, exportID, password, func(progress float64, stage string) {
		t.Logf("dry-run %.0f%%: %s", progress*100, stage)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("dry-run result: %#v", result)
}

func TestUserDataMigrationHubJSONRejectsAmbiguousResponse(t *testing.T) {
	for _, response := range []string{
		`{"export":{"status":"ready","Status":"deleted"}}`,
		`{"export":{}}{"shadow":true}`,
		`[["not-an-object"]]`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		app := &App{}
		_, err := app.userDataMigrationHubJSON(context.Background(), userDataMigrationClientConfig{
			HubURL: srv.URL, MachineID: "machine", MachineToken: "token",
		}, http.MethodGet, "/metadata", nil)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "invalid Hub migration JSON response") {
			t.Fatalf("expected ambiguous Hub response rejection for %s, got %v", response, err)
		}
	}
}

func TestUserDataMigrationHubErrorsDoNotEchoArbitraryResponseBodies(t *testing.T) {
	secret := "sk-must-not-be-echoed"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"safe migration error","debug":{"api_key":"` + secret + `"}}`))
	}))
	defer srv.Close()
	app := &App{}
	_, err := app.userDataMigrationHubJSON(context.Background(), userDataMigrationClientConfig{
		HubURL: srv.URL, MachineID: "machine", MachineToken: "token",
	}, http.MethodGet, "/metadata", nil)
	if err == nil || !strings.Contains(err.Error(), "safe migration error") {
		t.Fatalf("expected sanitized Hub error, got %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "api_key") {
		t.Fatalf("Hub response secret leaked through error: %v", err)
	}

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream dumped secret=" + secret))
	})
	_, err = app.userDataMigrationHubJSON(context.Background(), userDataMigrationClientConfig{
		HubURL: srv.URL, MachineID: "machine", MachineToken: "token",
	}, http.MethodGet, "/metadata", nil)
	if err == nil || !strings.Contains(err.Error(), "request failed") || strings.Contains(err.Error(), secret) {
		t.Fatalf("non-JSON Hub body was not sanitized: %v", err)
	}
}

func TestUserDataMigrationLLMValidationPanicIsNonFatalAndRedacted(t *testing.T) {
	result := userDataMigrationSafeLLMValidation(func() map[string]interface{} {
		panic("provider panic contained api_key=sk-must-not-leak")
	})
	if result["status"] != "validation_unavailable" {
		t.Fatalf("unexpected validation fallback: %#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("sk-must-not-leak")) || bytes.Contains(encoded, []byte("api_key")) {
		t.Fatalf("validation panic details leaked: %s", encoded)
	}
}

func TestUserDataMigrationManifestReadHasSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 65), 0o600); err != nil {
		t.Fatalf("write oversized manifest: %v", err)
	}
	var manifest userDataMigrationManifest
	err := userDataMigrationReadJSONFileLimited(path, &manifest, 64)
	if err == nil || !strings.Contains(err.Error(), "JSON file exceeds") {
		t.Fatalf("expected manifest size limit error, got %v", err)
	}
}

func TestUserDataMigrationRestoreJSONPayloadsHaveIndependentLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 33), 0o600); err != nil {
		t.Fatal(err)
	}
	var value interface{}
	if err := userDataMigrationReadJSONFileLimited(path, &value, 32); err == nil {
		t.Fatal("expected oversized payload JSON to be rejected before decoding")
	}
}

func TestUserDataMigrationJSONRejectsDuplicateFieldsAndTrailingValues(t *testing.T) {
	for _, data := range []string{
		`{"maclaw_llm_key":"first","maclaw_llm_key":"second"}`,
		`{"maclaw_llm_key":"first","Maclaw_LLM_Key":"second"}`,
		`{"providers":[{"key":"first","key":"second"}]}`,
		`{"ok":true}{"extra":true}`,
	} {
		var value interface{}
		if err := userDataMigrationDecodeStrictJSON([]byte(data), &value); err == nil {
			t.Fatalf("expected ambiguous JSON to be rejected: %s", data)
		}
	}
	var valid map[string]interface{}
	if err := userDataMigrationDecodeStrictJSON([]byte(`{"maclaw_llm_key":"complete-api-key"}`), &valid); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	if valid["maclaw_llm_key"] != "complete-api-key" {
		t.Fatalf("API key changed during strict decode: %#v", valid)
	}
}

func TestUserDataMigrationJSONRejectsExcessiveNesting(t *testing.T) {
	data := strings.Repeat("[", userDataMigrationMaxJSONDepth+2) + "null" + strings.Repeat("]", userDataMigrationMaxJSONDepth+2)
	var value interface{}
	if err := userDataMigrationDecodeStrictJSON([]byte(data), &value); err == nil || !strings.Contains(err.Error(), "nesting exceeds") {
		t.Fatalf("expected nesting limit error, got %v", err)
	}
	valid := strings.Repeat("[", userDataMigrationMaxJSONDepth) + "null" + strings.Repeat("]", userDataMigrationMaxJSONDepth)
	if err := userDataMigrationDecodeStrictJSON([]byte(valid), &value); err != nil {
		t.Fatalf("JSON at nesting limit rejected: %v", err)
	}
}

func TestUserDataMigrationRestoreRejectsManifestMemoryCountMismatch(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	if err := sourceStore.UpsertEntriesByID([]corememory.Entry{userDataMigrationTestEntry("memory-a", "migration memory")}); err != nil {
		t.Fatalf("seed source memory: %v", err)
	}
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	zipPath, _, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{}, t.TempDir())
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	payloadDir := filepath.Join(t.TempDir(), "payload")
	if err := userDataMigrationUnzipToDir(zipPath, payloadDir, userDataMigrationMaxExpanded); err != nil {
		t.Fatalf("unzip package: %v", err)
	}
	var manifest userDataMigrationManifest
	manifestPath := filepath.Join(payloadDir, "manifest.json")
	if err := userDataMigrationReadJSONFile(manifestPath, &manifest); err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest.MemoryEntries++
	if err := userDataMigrationWriteJSONFile(manifestPath, manifest); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
	tamperedZip := filepath.Join(t.TempDir(), "tampered.zip")
	if err := userDataMigrationZipDir(payloadDir, tamperedZip); err != nil {
		t.Fatalf("zip tampered package: %v", err)
	}
	target := &App{testHomeDir: t.TempDir()}
	_, err = target.restoreUserDataMigrationPackage(context.Background(), tamperedZip, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "memory entry count mismatch") {
		t.Fatalf("expected memory count mismatch, got %v", err)
	}
}

func TestUserDataMigrationSafeJoinRejectsTraversal(t *testing.T) {
	if _, err := userDataMigrationSafeJoin(filepath.Join(t.TempDir(), "root"), "../outside.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestUserDataMigrationCopyDirRejectsSymlink(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("target"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink("target.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if _, err := userDataMigrationCopyDirInto(t.TempDir(), src, "assets"); err == nil {
		t.Fatal("expected symlink copy to be rejected")
	}
}

func TestUserDataMigrationCleanupRetryUsesCurrentExport(t *testing.T) {
	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/exports/current":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":             "mig-current",
					"status":                "imported",
					"claimed_by_machine_id": "machine-current",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-current/complete":
			completeCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "status": "deleted"})
		case r.URL.Path == "/api/v1/migration/instances":
			t.Fatalf("cleanup retry should not depend on instances list")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := &App{}
	result, err := app.runUserDataMigrationCleanup(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-current", func(float64, string) {})
	if err != nil {
		t.Fatalf("cleanup retry: %v", err)
	}
	if completeCalls != 1 {
		t.Fatalf("complete calls = %d, want 1", completeCalls)
	}
	if result["cleanup_retried"] != true {
		t.Fatalf("cleanup result missing retry marker: %#v", result)
	}
}

func TestUserDataMigrationCleanupRetryRequiresCurrentMachineClaim(t *testing.T) {
	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/exports/current":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":             "mig-current",
					"status":                "imported",
					"claimed_by_machine_id": "machine-other",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-current/complete":
			completeCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "status": "deleted"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := &App{}
	_, err := app.runUserDataMigrationCleanup(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-current", func(float64, string) {})
	if err == nil || !strings.Contains(err.Error(), "not claimed by this machine") {
		t.Fatalf("expected current machine claim error, got %v", err)
	}
	if completeCalls != 0 {
		t.Fatalf("complete calls = %d, want 0", completeCalls)
	}
}

func TestUserDataMigrationCleanupRetryAllowsCurrentMachineImportingClaim(t *testing.T) {
	clearUserDataMigrationJobsForTest()
	t.Cleanup(clearUserDataMigrationJobsForTest)

	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/exports/current":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":             "mig-current",
					"status":                "importing",
					"claimed_by_machine_id": "machine-current",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-current/complete":
			completeCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "status": "deleted"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := &App{}
	_, err := app.runUserDataMigrationCleanup(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-current", func(float64, string) {})
	if err == nil || !strings.Contains(err.Error(), "not ready for cleanup retry") {
		t.Fatalf("expected importing cleanup retry to require local pending marker, got %v", err)
	}
	if completeCalls != 0 {
		t.Fatalf("complete calls before pending marker = %d, want 0", completeCalls)
	}

	cfg := userDataMigrationClientConfig{
		HubURL:       srv.URL,
		TenantID:     "tenant-a",
		UserID:       "user-a",
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}
	key := userDataMigrationCleanupPendingKey(cfg, "mig-current")
	userDataMigrationCleanupPendingExports.Store(key, time.Now().UTC())
	result, err := app.runUserDataMigrationCleanup(context.Background(), cfg, "mig-current", func(float64, string) {})
	if err != nil {
		t.Fatalf("cleanup retry for importing claim: %v", err)
	}
	if completeCalls != 1 {
		t.Fatalf("complete calls = %d, want 1", completeCalls)
	}
	if result["cleanup_retried"] != true {
		t.Fatalf("cleanup result missing retry marker: %#v", result)
	}
	if _, ok := userDataMigrationCleanupPendingExports.Load(key); ok {
		t.Fatalf("cleanup pending marker should be cleared after cleanup succeeds")
	}
}

func TestUserDataMigrationResumeDoesNotAbortAlreadyRestoredPackageOnCleanupFailure(t *testing.T) {
	abortCalls := 0
	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-resume/claim":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"export": map[string]interface{}{
				"export_id": "mig-resume", "status": "imported",
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-resume/complete":
			completeCalls++
			http.Error(w, "cleanup unavailable", http.StatusServiceUnavailable)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-resume/abort":
			abortCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := userDataMigrationClientConfig{HubURL: srv.URL, TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-current", MachineToken: "token-current"}
	_, err := (&App{}).runUserDataMigrationImport(context.Background(), cfg, "mig-resume", "unused", func(float64, string) {})
	if err == nil || !strings.Contains(err.Error(), "Hub cleanup retry failed") {
		t.Fatalf("expected cleanup retry error, got %v", err)
	}
	if completeCalls != 3 {
		t.Fatalf("complete calls = %d, want 3", completeCalls)
	}
	if abortCalls != 0 {
		t.Fatalf("already restored package must not be aborted, abort calls = %d", abortCalls)
	}
	if _, ok := userDataMigrationCleanupPendingExports.Load(userDataMigrationCleanupPendingKey(cfg, "mig-resume")); !ok {
		t.Fatal("cleanup failure should remain retryable")
	}
	userDataMigrationCleanupPendingExports.Delete(userDataMigrationCleanupPendingKey(cfg, "mig-resume"))
}

func TestUserDataMigrationImportSucceedsWhenHubCleanupFails(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	if err := sourceStore.UpsertEntriesByID([]corememory.Entry{
		userDataMigrationTestEntry("memory-new", "new migration memory"),
	}); err != nil {
		t.Fatalf("seed source memory: %v", err)
	}
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	if _, err := source.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Migrated note",
		Text:        "new migrated knowledge note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	}); err != nil {
		t.Fatalf("seed source knowledge: %v", err)
	}

	workDir := t.TempDir()
	plainPath, _, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID:    "tenant-a",
		UserID:      "user-a",
		MachineID:   "machine-source",
		MachineName: "Source Machine",
	}, workDir)
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	plainHash, plainSize, err := userDataMigrationFileSHA256(plainPath)
	if err != nil {
		t.Fatalf("hash plain package: %v", err)
	}
	encryptedPath := filepath.Join(workDir, "migration.mlawenc")
	if err := encryptUserDataMigrationFile(plainPath, encryptedPath, "secret-pass", plainHash); err != nil {
		t.Fatalf("encrypt package: %v", err)
	}
	encryptedHash, encryptedSize, err := userDataMigrationFileSHA256(encryptedPath)
	if err != nil {
		t.Fatalf("hash encrypted package: %v", err)
	}
	encryptedBytes, err := os.ReadFile(encryptedPath)
	if err != nil {
		t.Fatalf("read encrypted package: %v", err)
	}

	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-source/claim":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":        "mig-source",
					"status":           "ready",
					"chunk_count":      1,
					"chunk_size":       userDataMigrationChunkSize,
					"encrypted_size":   encryptedSize,
					"compressed_size":  plainSize,
					"encrypted_sha256": encryptedHash,
					"plain_sha256":     plainHash,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/imports/mig-source/chunks/0":
			_, _ = w.Write(encryptedBytes)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-source/complete":
			completeCalls++
			http.Error(w, "cleanup unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	targetStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode target: %v", err)
	}
	t.Cleanup(targetStore.Stop)
	target := &App{testHomeDir: t.TempDir(), memoryStore: targetStore}
	var lastProgress string
	result, err := target.runUserDataMigrationImport(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-source", "secret-pass", func(_ float64, text string) {
		lastProgress = text
	})
	if err != nil {
		t.Fatalf("import should succeed with cleanup pending: %v", err)
	}
	if result["cleanup_pending"] != true {
		t.Fatalf("expected cleanup_pending result, got %#v", result)
	}
	if strings.TrimSpace(result["cleanup_error"].(string)) == "" {
		t.Fatalf("expected cleanup_error detail, got %#v", result)
	}
	if lastProgress != "local import completed; Hub cleanup can be retried" {
		t.Fatalf("last progress = %q", lastProgress)
	}
	if completeCalls != 3 {
		t.Fatalf("complete calls = %d, want 3", completeCalls)
	}
	if got := targetStore.List("", ""); len(got) != 1 || got[0].ID != "memory-new" {
		t.Fatalf("memory not restored: %#v", got)
	}
}

func TestUserDataMigrationRejectsConcurrentJobs(t *testing.T) {
	clearUserDataMigrationJobsForTest()
	t.Cleanup(clearUserDataMigrationJobsForTest)

	app := &App{}
	release := make(chan struct{})
	started := make(chan struct{})
	job, err := app.startUserDataMigrationJob("migration.test", func(context.Context, func(float64, string)) (map[string]interface{}, error) {
		close(started)
		<-release
		return map[string]interface{}{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("start first job: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first job did not start")
	}

	if _, err := app.startUserDataMigrationJob("migration.test", func(context.Context, func(float64, string)) (map[string]interface{}, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("expected concurrent migration job to be rejected")
	}

	close(release)
	deadline := time.After(2 * time.Second)
	for {
		got, err := app.GetUserDataMigrationJob(job.ID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if got.Status == "succeeded" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("job did not finish, status=%s", got.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestUserDataMigrationJobRecoversPanic(t *testing.T) {
	clearUserDataMigrationJobsForTest()
	t.Cleanup(clearUserDataMigrationJobsForTest)

	app := &App{}
	job, err := app.startUserDataMigrationJob("migration.test", func(context.Context, func(float64, string)) (map[string]interface{}, error) {
		panic("boom")
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		got, err := app.GetUserDataMigrationJob(job.ID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if got.Status == "failed" {
			if !strings.Contains(got.Error, "boom") {
				t.Fatalf("panic error not reported: %q", got.Error)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("job did not fail, status=%s", got.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func userDataMigrationTestEntry(id, content string) corememory.Entry {
	return corememory.Entry{
		ID:       id,
		Content:  content,
		Category: corememory.CategoryProjectKnowledge,
	}
}

func userDataMigrationKnowledgeSourceIDs(t *testing.T, app *App) map[string]struct{} {
	t.Helper()
	items, err := app.KnowledgeListSources(coreknowledge.ListSourcesOptions{Limit: 100})
	if err != nil {
		t.Fatalf("list knowledge sources: %v", err)
	}
	out := map[string]struct{}{}
	for _, item := range items {
		out[item.ID] = struct{}{}
	}
	return out
}

func clearUserDataMigrationJobsForTest() {
	userDataMigrationJobs.Range(func(key, _ interface{}) bool {
		userDataMigrationJobs.Delete(key)
		return true
	})
	userDataMigrationCleanupPendingExports.Range(func(key, _ interface{}) bool {
		userDataMigrationCleanupPendingExports.Delete(key)
		return true
	})
}

func assertUserDataMigrationPrivateDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat private migration directory %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("migration directory %s permissions = %o, want no group/other access", path, got)
	}
}
