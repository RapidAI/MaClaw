package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Task 10.2: 为信任确认编写单元测试

func TestTrustStore_FirstLoadPromptsConfirm(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "trusted_plugins.json")

	confirmed := false
	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: fp,
		confirm: func(name, pluginDir string) bool {
			confirmed = true
			return true
		},
	}

	result := ts.CheckTrust("new-plugin", "/some/dir")
	if !confirmed {
		t.Error("confirm callback was not called")
	}
	if !result {
		t.Error("expected trust to be granted")
	}
	if !ts.IsTrusted("new-plugin") {
		t.Error("plugin should be trusted after confirmation")
	}

	// Verify persisted to disk
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read trust file: %v", err)
	}
	var payload trustFilePayload
	json.Unmarshal(data, &payload)
	found := false
	for _, name := range payload.Trusted {
		if name == "new-plugin" {
			found = true
		}
	}
	if !found {
		t.Error("plugin not found in persisted trust file")
	}
}

func TestTrustStore_AlreadyTrustedSkipsConfirm(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "trusted_plugins.json")

	// Pre-populate trust file
	payload := trustFilePayload{Trusted: []string{"existing-plugin"}}
	data, _ := json.Marshal(payload)
	os.MkdirAll(filepath.Dir(fp), 0755)
	os.WriteFile(fp, data, 0644)

	confirmCalled := false
	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: fp,
		confirm: func(name, pluginDir string) bool {
			confirmCalled = true
			return true
		},
	}
	ts.load()

	result := ts.CheckTrust("existing-plugin", "/dir")
	if confirmCalled {
		t.Error("confirm should not be called for already-trusted plugin")
	}
	if !result {
		t.Error("expected trust to be granted for existing plugin")
	}
}

func TestTrustStore_DeniedNotTrusted(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "trusted_plugins.json")

	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: fp,
		confirm: func(name, pluginDir string) bool {
			return false // user denies
		},
	}

	result := ts.CheckTrust("untrusted", "/dir")
	if result {
		t.Error("expected trust to be denied")
	}
	if ts.IsTrusted("untrusted") {
		t.Error("plugin should not be trusted after denial")
	}
}

func TestTrustStore_NilConfirmFunc(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "trusted_plugins.json")

	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: fp,
		confirm:  nil,
	}

	result := ts.CheckTrust("no-confirm", "/dir")
	if result {
		t.Error("expected false when confirm is nil")
	}
}

func TestTrustStore_LoadFromDisk(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "trusted_plugins.json")

	payload := trustFilePayload{Trusted: []string{"a", "b", "c"}}
	data, _ := json.Marshal(payload)
	os.WriteFile(fp, data, 0644)

	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: fp,
	}
	ts.load()

	for _, name := range []string{"a", "b", "c"} {
		if !ts.IsTrusted(name) {
			t.Errorf("%q should be trusted", name)
		}
	}
	if ts.IsTrusted("d") {
		t.Error("d should not be trusted")
	}
}

func TestTrustStore_LoadMissingFile(t *testing.T) {
	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: "/nonexistent/path/trusted.json",
	}
	ts.load() // should not panic
	if len(ts.trusted) != 0 {
		t.Error("expected empty trusted map for missing file")
	}
}

func TestTrustStore_LoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "trusted_plugins.json")
	os.WriteFile(fp, []byte("{invalid json"), 0644)

	ts := &TrustStore{
		trusted:  make(map[string]bool),
		filePath: fp,
	}
	ts.load() // should not panic
	if len(ts.trusted) != 0 {
		t.Error("expected empty trusted map for invalid JSON")
	}
}
