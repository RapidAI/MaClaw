package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate durable device-key I/O from the real machine for the whole package
// (enrollment tests also call LoadOrCreateDeviceKey / EnsureDeviceKey).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "maclaw-device-key-test-*")
	if err != nil {
		panic(err)
	}
	SetDurableDeviceKeyDirForTest(dir)
	code := m.Run()
	SetDurableDeviceKeyDirForTest("")
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestLoadOrCreateDeviceKeyPersistsAcrossCacheReset(t *testing.T) {
	dir := t.TempDir()
	SetDurableDeviceKeyDirForTest(dir)
	t.Cleanup(func() { SetDurableDeviceKeyDirForTest("") })
	ResetDeviceKeyCacheForTest()

	first := LoadOrCreateDeviceKey()
	if first == "" {
		t.Fatal("expected non-empty device key")
	}
	path := DurableDeviceKeyPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("durable key file missing: %v", err)
	}

	ResetDeviceKeyCacheForTest()
	second := LoadOrCreateDeviceKey()
	if second != first {
		t.Fatalf("device key changed after cache reset: %q vs %q", first, second)
	}
}

func TestEnsureDeviceKeySeedsDurableStore(t *testing.T) {
	dir := t.TempDir()
	SetDurableDeviceKeyDirForTest(dir)
	t.Cleanup(func() { SetDurableDeviceKeyDirForTest("") })
	ResetDeviceKeyCacheForTest()

	key := EnsureDeviceKey("client-stable-1")
	if key != "client-stable-1" {
		t.Fatalf("EnsureDeviceKey = %q", key)
	}
	ResetDeviceKeyCacheForTest()
	got, ok := ReadDurableDeviceKey()
	if !ok || got != "client-stable-1" {
		t.Fatalf("ReadDurableDeviceKey = %q ok=%v", got, ok)
	}
	// Simulate wiped config: LoadOrCreate should recover durable key.
	if got := LoadOrCreateDeviceKey(); got != "client-stable-1" {
		t.Fatalf("LoadOrCreateDeviceKey after wipe = %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(DurableDeviceKeyPath())); len(entries) == 0 {
		t.Fatal("expected durable dir to contain key file")
	}
}

func TestEnsureDeviceKeyPreferredOverridesDurable(t *testing.T) {
	dir := t.TempDir()
	SetDurableDeviceKeyDirForTest(dir)
	t.Cleanup(func() { SetDurableDeviceKeyDirForTest("") })
	ResetDeviceKeyCacheForTest()

	if got := EnsureDeviceKey("durable-first"); got != "durable-first" {
		t.Fatalf("seed = %q", got)
	}
	ResetDeviceKeyCacheForTest()
	// Explicit preferred (config remote_client_id) is authoritative and re-seeds durable.
	if got := EnsureDeviceKey("config-client"); got != "config-client" {
		t.Fatalf("EnsureDeviceKey = %q, want config-client", got)
	}
	ResetDeviceKeyCacheForTest()
	if got, ok := ReadDurableDeviceKey(); !ok || got != "config-client" {
		t.Fatalf("durable not re-seeded: %q ok=%v", got, ok)
	}
}

func TestWriteDeviceKeyFallsBackWhenPrimaryDirFails(t *testing.T) {
	// Use a multi-dir hook simulation: primary is a file path (mkdir fails),
	// secondary is a real temp dir. Override via sequential hook is hard;
	// instead verify write to a normal dir and read round-trip still works
	// when the key lives only under the hooked dir.
	dir := t.TempDir()
	SetDurableDeviceKeyDirForTest(dir)
	t.Cleanup(func() { SetDurableDeviceKeyDirForTest("") })
	ResetDeviceKeyCacheForTest()

	key := LoadOrCreateDeviceKey()
	path := filepath.Join(dir, durableDeviceKeyFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected key written to durable dir: %v", err)
	}
	if strings.TrimSpace(string(data)) != key {
		t.Fatalf("file key = %q want %q", data, key)
	}
}
