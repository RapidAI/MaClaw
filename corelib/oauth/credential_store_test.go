package oauth

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileCredentialStore_ReadWriteDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	store := NewFileCredentialStore(path)

	// Read non-existent → nil
	cred, err := store.Read("openai")
	if err != nil {
		t.Fatalf("Read non-existent: %v", err)
	}
	if cred != nil {
		t.Fatalf("expected nil, got %+v", cred)
	}

	// Modify: create
	err = store.Modify("openai", func(old *StoredCredential) (*StoredCredential, error) {
		if old != nil {
			t.Fatalf("expected nil old, got %+v", old)
		}
		return &StoredCredential{
			Type:        "oauth",
			AccessToken: "sk-test-123",
			ExpiresAt:   time.Now().Unix() + 3600,
		}, nil
	})
	if err != nil {
		t.Fatalf("Modify create: %v", err)
	}

	// Read back
	cred, err = store.Read("openai")
	if err != nil {
		t.Fatalf("Read after create: %v", err)
	}
	if cred == nil || cred.AccessToken != "sk-test-123" {
		t.Fatalf("unexpected cred: %+v", cred)
	}

	// Modify: update
	err = store.Modify("openai", func(old *StoredCredential) (*StoredCredential, error) {
		if old == nil || old.AccessToken != "sk-test-123" {
			t.Fatalf("expected old with sk-test-123, got %+v", old)
		}
		old.AccessToken = "sk-test-456"
		return old, nil
	})
	if err != nil {
		t.Fatalf("Modify update: %v", err)
	}

	cred, _ = store.Read("openai")
	if cred.AccessToken != "sk-test-456" {
		t.Fatalf("expected sk-test-456, got %s", cred.AccessToken)
	}

	// Delete
	err = store.Delete("openai")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	cred, _ = store.Read("openai")
	if cred != nil {
		t.Fatalf("expected nil after delete, got %+v", cred)
	}

	// Delete non-existent → no-op
	err = store.Delete("nonexistent")
	if err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestFileCredentialStore_MultipleProviders(t *testing.T) {
	dir := t.TempDir()
	store := NewFileCredentialStore(filepath.Join(dir, "creds.json"))

	// Write two providers
	_ = store.Modify("openai", func(_ *StoredCredential) (*StoredCredential, error) {
		return &StoredCredential{Type: "oauth", AccessToken: "openai-token"}, nil
	})
	_ = store.Modify("anthropic", func(_ *StoredCredential) (*StoredCredential, error) {
		return &StoredCredential{Type: "oauth", AccessToken: "anthropic-token"}, nil
	})

	// Read independently
	c1, _ := store.Read("openai")
	c2, _ := store.Read("anthropic")
	if c1.AccessToken != "openai-token" || c2.AccessToken != "anthropic-token" {
		t.Fatalf("cross-contamination: openai=%s anthropic=%s", c1.AccessToken, c2.AccessToken)
	}

	// Delete one doesn't affect other
	_ = store.Delete("openai")
	c2, _ = store.Read("anthropic")
	if c2 == nil || c2.AccessToken != "anthropic-token" {
		t.Fatalf("deleting openai affected anthropic: %+v", c2)
	}
}

func TestFileCredentialStore_ModifyError_NoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	store := NewFileCredentialStore(path)

	// Write initial value
	_ = store.Modify("test", func(_ *StoredCredential) (*StoredCredential, error) {
		return &StoredCredential{Type: "oauth", AccessToken: "initial"}, nil
	})

	// Modify with error → should not change stored value
	err := store.Modify("test", func(_ *StoredCredential) (*StoredCredential, error) {
		return nil, fmt.Errorf("simulated error")
	})
	if err == nil {
		t.Fatal("expected error")
	}

	cred, _ := store.Read("test")
	if cred == nil || cred.AccessToken != "initial" {
		t.Fatalf("error in fn should not mutate store, got %+v", cred)
	}
}

func TestFileCredentialStore_ConcurrentModify_Serialized(t *testing.T) {
	dir := t.TempDir()
	store := NewFileCredentialStore(filepath.Join(dir, "creds.json"))

	// Write initial
	_ = store.Modify("test", func(_ *StoredCredential) (*StoredCredential, error) {
		return &StoredCredential{Type: "oauth", AccessToken: "v0", ExpiresAt: 1}, nil
	})

	// Simulate concurrent token refresh: 10 goroutines all try to "refresh"
	// but only the first should actually change the value (others see updated old)
	var refreshCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.Modify("test", func(old *StoredCredential) (*StoredCredential, error) {
				// Simulate: only refresh if still expired
				if old != nil && old.ExpiresAt > time.Now().Unix() {
					return old, nil // already refreshed by another goroutine
				}
				refreshCount.Add(1)
				return &StoredCredential{
					Type:        "oauth",
					AccessToken: "refreshed",
					ExpiresAt:   time.Now().Unix() + 3600,
				}, nil
			})
		}()
	}
	wg.Wait()

	// Due to serialization, only 1 goroutine should have done the actual refresh
	count := refreshCount.Load()
	if count != 1 {
		t.Fatalf("expected exactly 1 refresh, got %d", count)
	}

	cred, _ := store.Read("test")
	if cred.AccessToken != "refreshed" {
		t.Fatalf("expected 'refreshed', got %s", cred.AccessToken)
	}
}

func TestFileCredentialStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	store := NewFileCredentialStore(path)

	_ = store.Modify("test", func(_ *StoredCredential) (*StoredCredential, error) {
		return &StoredCredential{Type: "oauth", AccessToken: "data"}, nil
	})

	// Verify no .tmp file left behind
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("tmp file should not remain: %s", e.Name())
		}
	}

	// Verify file permissions (Unix only)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	// On Windows mode check is less meaningful, skip strict check
	if mode&0077 != 0 && os.Getenv("OS") != "Windows_NT" {
		t.Fatalf("credentials file should be owner-only, got %o", mode)
	}
}

func TestStoredCredential_IsExpired(t *testing.T) {
	cases := []struct {
		name    string
		cred    *StoredCredential
		expired bool
	}{
		{"nil", nil, false},
		{"no expiry", &StoredCredential{ExpiresAt: 0}, false},
		{"future", &StoredCredential{ExpiresAt: time.Now().Unix() + 3600}, false},
		{"past", &StoredCredential{ExpiresAt: time.Now().Unix() - 100}, true},
		{"within margin", &StoredCredential{ExpiresAt: time.Now().Unix() + 60}, true}, // 60s < 5min margin
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cred.IsExpired()
			if got != tc.expired {
				t.Errorf("IsExpired()=%v, want %v", got, tc.expired)
			}
		})
	}
}

func TestMigrateFromConfig_OnlyMigratesNew(t *testing.T) {
	dir := t.TempDir()
	store := NewFileCredentialStore(filepath.Join(dir, "creds.json"))

	// Pre-populate one provider
	_ = store.Modify("openai", func(_ *StoredCredential) (*StoredCredential, error) {
		return &StoredCredential{Type: "oauth", AccessToken: "existing"}, nil
	})

	// Migrate two providers
	sources := []MigrationSource{
		{ProviderID: "openai", Type: "oauth", AccessToken: "from-config-openai"},
		{ProviderID: "codegen", Type: "sso", AccessToken: "from-config-codegen", BaseURL: "https://codegen.example.com"},
	}
	migrated := MigrateFromConfig(store, sources)
	if migrated != 1 {
		t.Fatalf("expected 1 migrated, got %d", migrated)
	}

	// openai should NOT be overwritten
	c, _ := store.Read("openai")
	if c.AccessToken != "existing" {
		t.Fatalf("openai should not be overwritten, got %s", c.AccessToken)
	}

	// codegen should be migrated
	c, _ = store.Read("codegen")
	if c == nil || c.AccessToken != "from-config-codegen" || c.BaseURL != "https://codegen.example.com" {
		t.Fatalf("codegen not migrated correctly: %+v", c)
	}
}

func TestMigrateFromConfig_SkipsEmptyToken(t *testing.T) {
	dir := t.TempDir()
	store := NewFileCredentialStore(filepath.Join(dir, "creds.json"))

	sources := []MigrationSource{
		{ProviderID: "empty", Type: "oauth", AccessToken: ""},
	}
	migrated := MigrateFromConfig(store, sources)
	if migrated != 0 {
		t.Fatalf("expected 0 migrated, got %d", migrated)
	}
}
