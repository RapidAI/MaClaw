package guiapp

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func installCloudWorkspaceTestKeyring(t *testing.T, keys map[string][]byte) {
	t.Helper()
	oldGet, oldSet, oldDelete := cloudWorkspaceCacheKeyringGet, cloudWorkspaceCacheKeyringSet, cloudWorkspaceCacheKeyringDelete
	cloudWorkspaceCacheKeyringGet = func(service, item string) (string, error) {
		key, ok := keys[item]
		if !ok {
			return "", keyring.ErrNotFound
		}
		return base64.RawStdEncoding.EncodeToString(key), nil
	}
	cloudWorkspaceCacheKeyringSet = func(service, item, value string) error {
		key, err := base64.RawStdEncoding.DecodeString(value)
		if err != nil {
			return err
		}
		keys[item] = append([]byte(nil), key...)
		return nil
	}
	cloudWorkspaceCacheKeyringDelete = func(service, item string) error {
		if _, ok := keys[item]; !ok {
			return keyring.ErrNotFound
		}
		delete(keys, item)
		return nil
	}
	t.Cleanup(func() {
		cloudWorkspaceCacheKeyringGet, cloudWorkspaceCacheKeyringSet, cloudWorkspaceCacheKeyringDelete = oldGet, oldSet, oldDelete
	})
}

func TestCloudWorkspaceCacheSealRoundTrip(t *testing.T) {
	keys := map[string][]byte{}
	installCloudWorkspaceTestKeyring(t, keys)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeCloudWorkspaceState(root, cloudWorkspaceLocalState{LastPushedRevision: "rev-1"}); err != nil {
		t.Fatal(err)
	}
	if err := sealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "nested", "main.go")); !os.IsNotExist(err) {
		t.Fatalf("plaintext file still exists after seal: %v", err)
	}
	if _, err := os.Stat(cloudWorkspaceCacheSealMarkerPath(root)); err != nil {
		t.Fatalf("seal marker missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceCacheStateFile)); !os.IsNotExist(err) {
		t.Fatalf("state sidecar remains plaintext after seal: %v", err)
	}
	sealed, err := os.ReadFile(filepath.Join(cloudWorkspaceCacheSealDirPath(root), "does-not-leak-paths"))
	if err == nil || len(sealed) != 0 {
		t.Fatalf("unexpected sealed path probe: %v", err)
	}
	if err := unsealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err != nil {
		t.Fatalf("unseal: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "src", "nested", "main.go"))
	if err != nil || string(got) != "package main\n" {
		t.Fatalf("restored content=%q err=%v", got, err)
	}
	if _, err := os.Stat(cloudWorkspaceCacheSealMarkerPath(root)); !os.IsNotExist(err) {
		t.Fatalf("seal marker remains after unseal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceCacheStateFile)); err != nil {
		t.Fatalf("state sidecar was not restored: %v", err)
	}
}

func TestCloudWorkspaceCacheUnsealFailsWithDestroyedKey(t *testing.T) {
	keys := map[string][]byte{}
	installCloudWorkspaceTestKeyring(t, keys)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("do not expose"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err != nil {
		t.Fatal(err)
	}
	delete(keys, cloudWorkspaceCacheKeyringID("tenant_a", "cws_crypto"))
	if err := unsealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err == nil {
		t.Fatal("unseal unexpectedly succeeded after key destruction")
	}
	if _, err := os.Stat(filepath.Join(root, "secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("failed unseal should not expose plaintext, stat err=%v", err)
	}
}

func TestCloudWorkspaceCacheUnsealRejectsUnexpectedPlaintext(t *testing.T) {
	keys := map[string][]byte{}
	installCloudWorkspaceTestKeyring(t, keys)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "known.txt"), []byte("known"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unexpected.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unsealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err == nil {
		t.Fatal("unseal unexpectedly discarded an unexpected plaintext file")
	}
	got, err := os.ReadFile(filepath.Join(root, "unexpected.txt"))
	if err != nil || string(got) != "dirty" {
		t.Fatalf("unexpected plaintext changed: %q err=%v", got, err)
	}
}

func TestCloudWorkspaceCacheUnsealDoesNotPartiallyRestoreOnTamper(t *testing.T) {
	keys := map[string][]byte{}
	installCloudWorkspaceTestKeyring(t, keys)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"a.txt": "first", "b.txt": "second"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := sealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadDir(cloudWorkspaceCacheSealDirPath(root))
	if err != nil || len(sealed) != 2 {
		t.Fatalf("sealed files=%v err=%v", sealed, err)
	}
	if err := os.WriteFile(filepath.Join(cloudWorkspaceCacheSealDirPath(root), sealed[1].Name()), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unsealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err == nil {
		t.Fatal("tampered cache unexpectedly unsealed")
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("tampered unseal partially restored %s: %v", name, err)
		}
	}
}

func TestPurgeCloudWorkspaceLocalCachesRetiresSealedKey(t *testing.T) {
	keys := map[string][]byte{}
	installCloudWorkspaceTestKeyring(t, keys)
	app := &App{testHomeDir: t.TempDir()}
	root := app.cloudWorkspaceCachePath("tenant_a", "cws_crypto")
	if err := os.MkdirAll(filepath.Join(root, cloudWorkspaceCacheStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sealCloudWorkspaceCache(nil, root, "tenant_a", "cws_crypto"); err != nil {
		t.Fatal(err)
	}
	item := cloudWorkspaceCacheKeyringID("tenant_a", "cws_crypto")
	if _, ok := keys[item]; !ok {
		t.Fatal("seal did not create a per-workspace key")
	}
	if err := app.purgeCloudWorkspaceLocalCaches("cws_crypto"); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, ok := keys[item]; ok {
		t.Fatal("purge left the sealed cache key in the keyring")
	}
}

func TestCloudWorkspaceReleaseAndPrepareRoundTripWithCacheEncryption(t *testing.T) {
	keys := map[string][]byte{}
	installCloudWorkspaceTestKeyring(t, keys)
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.CloudWorkspaceCacheEncryption = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	prepared, err := app.PrepareCloudWorkspace("cws_crypto_lifecycle")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "notes.txt"), []byte("continue on another machine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.ReleaseCloudWorkspace(prepared.WorkspaceID); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("release left plaintext cache file: %v", err)
	}
	if !cloudWorkspaceCacheSealMarkerExists(prepared.LocalPath) {
		t.Fatal("release did not write cache seal marker")
	}
	reopened, err := app.PrepareCloudWorkspace(prepared.WorkspaceID)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(reopened.LocalPath, "notes.txt"))
	if err != nil || string(got) != "continue on another machine" {
		t.Fatalf("reopened content=%q err=%v", got, err)
	}
	_ = app.ReleaseCloudWorkspace(reopened.WorkspaceID)
}
