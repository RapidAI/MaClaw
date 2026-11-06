package guiapp

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserExtensionStateMigratesInstallMode(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "browser-extension-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE browser_extension_browsers (
  browser TEXT PRIMARY KEY,
  version TEXT NOT NULL DEFAULT '',
  hello_at TEXT NOT NULL DEFAULT '',
  registered_at TEXT NOT NULL DEFAULT '',
  running_at_register INTEGER NOT NULL DEFAULT 0
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO browser_extension_browsers(browser, version, hello_at, registered_at, running_at_register) VALUES('chrome','1.0.0','2026-01-02T00:00:00Z','',0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome == nil || got.Chrome.Version != "1.0.0" || got.Chrome.HelloAt != "2026-01-02T00:00:00Z" || got.Chrome.InstallMode != "" {
		t.Fatalf("migrated=%#v", got.Chrome)
	}
}

func TestBrowserExtensionStateImportsLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "browser-extension-state.json")
	body := []byte("{\n  \"edge\": {\n    \"version\": \"0.9.0\",\n    \"helloAt\": \"2026-01-02T03:04:05Z\",\n    \"registeredAt\": \"2026-01-01T00:00:00Z\",\n    \"browserRunningAtRegister\": true\n  }\n}\n")
	if err := os.WriteFile(legacy, body, 0o600); err != nil {
		t.Fatal(err)
	}
	state := readBrowserExtensionState(dir)
	if state.Chrome != nil || state.Edge == nil || state.Edge.Version != "0.9.0" || state.Edge.HelloAt != "2026-01-02T03:04:05Z" || state.Edge.RegisteredAt != "2026-01-01T00:00:00Z" || !state.Edge.BrowserRunningAtRegister {
		t.Fatalf("imported chrome=%#v edge=%#v", state.Chrome, state.Edge)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy json remains after import")
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-extension-state.db")); err != nil {
		t.Fatal(err)
	}
	state.Chrome = &browserExtensionBrowserRecord{Version: "1.0.0", HelloAt: "2026-02-02T00:00:00Z"}
	if err := writeBrowserExtensionState(dir, state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("later write recreated the json file")
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome == nil || got.Chrome.Version != "1.0.0" || got.Chrome.RegisteredAt != "" || got.Edge == nil || got.Edge.Version != "0.9.0" || !got.Edge.BrowserRunningAtRegister {
		t.Fatalf("chrome=%#v edge=%#v", got.Chrome, got.Edge)
	}
}

func TestBrowserExtensionStateSealsRejectedLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "browser-extension-state.json")
	if err := os.WriteFile(legacy, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeBrowserExtensionState(dir, browserExtensionStateFile{
		Chrome: &browserExtensionBrowserRecord{Version: "1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("rejected legacy json remains")
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome == nil || got.Chrome.Version != "1" || got.Edge != nil {
		t.Fatalf("chrome=%#v edge=%#v", got.Chrome, got.Edge)
	}
}

func TestBrowserExtensionStateKeepsUnreadLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "browser-extension-state.json")
	body := []byte(`{"edge":{"version":"0.9.0","helloAt":"2026-01-02T03:04:05Z"}}`)
	if err := os.WriteFile(legacy, body, 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeBrowserExtensionState(dir, browserExtensionStateFile{
		Chrome: &browserExtensionBrowserRecord{Version: "1.0.0"},
	})
	if !errors.Is(err, errBrowserExtensionLegacyPending) {
		t.Fatalf("write=%v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("unread legacy json was deleted")
	}
	db, err := openBrowserExtensionDB(filepath.Join(dir, "browser-extension-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	scanErr := db.QueryRow(`SELECT COUNT(*) FROM browser_extension_browsers`).Scan(&n)
	_ = db.Close()
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if n != 0 {
		t.Fatalf("pending write stored %d rows", n)
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome != nil || got.Edge == nil || got.Edge.Version != "0.9.0" {
		t.Fatalf("import did not keep the legacy document chrome=%#v edge=%#v", got.Chrome, got.Edge)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy json remains after the importing read")
	}
}

func TestBrowserExtensionHelloImportsLegacyAndKeepsOtherBrowser(t *testing.T) {
	dir := t.TempDir()
	prev := browserExtensionStateDirHook
	browserExtensionStateDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { browserExtensionStateDirHook = prev })
	legacy := filepath.Join(dir, "browser-extension-state.json")
	body := []byte(`{"edge":{"version":"0.9.0","helloAt":"2026-01-02T03:04:05Z","registeredAt":"2026-01-01T00:00:00Z","browserRunningAtRegister":true}}`)
	if err := os.WriteFile(legacy, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recordBrowserExtensionHello("chrome", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("hello left the legacy json")
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome == nil || got.Chrome.Version != "1.0.0" || got.Chrome.HelloAt == "" || got.Chrome.RegisteredAt != "" {
		t.Fatalf("chrome=%#v", got.Chrome)
	}
	if got.Edge == nil || got.Edge.Version != "0.9.0" || got.Edge.HelloAt != "2026-01-02T03:04:05Z" || !got.Edge.BrowserRunningAtRegister {
		t.Fatalf("edge=%#v", got.Edge)
	}
}

func TestBrowserExtensionHelloDoesNotReplaceUnreadableDatabase(t *testing.T) {
	dir := t.TempDir()
	prev := browserExtensionStateDirHook
	browserExtensionStateDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { browserExtensionStateDirHook = prev })
	if err := writeBrowserExtensionState(dir, browserExtensionStateFile{
		Edge: &browserExtensionBrowserRecord{Version: "0.9.0", HelloAt: "2026-01-02T03:04:05Z"},
	}); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "browser-extension-state.db")
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	if err := os.WriteFile(dbPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recordBrowserExtensionHello("chrome", "1.0.0"); err == nil {
		t.Fatal("hello wrote over an unreadable database")
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-extension-state.json")); !os.IsNotExist(err) {
		t.Fatal("failed hello created a json file")
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome != nil || got.Edge != nil {
		t.Fatalf("unreadable database was replaced chrome=%#v edge=%#v", got.Chrome, got.Edge)
	}
}

func TestBrowserExtensionReadWaitsForStateLock(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = withBrowserExtensionStateLock(dir, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	done := make(chan struct{})
	go func() {
		_ = readBrowserExtensionState(dir)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("read ran while the state lock was held")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not resume after the state lock was released")
	}
}

func TestBrowserExtensionHelloIgnoresLegacyAfterImport(t *testing.T) {
	dir := t.TempDir()
	prev := browserExtensionStateDirHook
	browserExtensionStateDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { browserExtensionStateDirHook = prev })
	legacy := filepath.Join(dir, "browser-extension-state.json")
	body := []byte(`{"edge":{"version":"0.9.0","helloAt":"2026-01-02T03:04:05Z","registeredAt":"2026-01-01T00:00:00Z"}}`)
	if err := os.WriteFile(legacy, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if state := readBrowserExtensionState(dir); state.Edge == nil || state.Edge.Version != "0.9.0" {
		t.Fatalf("edge=%#v", state.Edge)
	}
	if err := os.WriteFile(legacy, []byte(`{"chrome":{"version":"9.9.9","helloAt":"2026-03-03T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recordBrowserExtensionHello("chrome", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("stale json remained after hello")
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome == nil || got.Chrome.Version != "1.0.0" || got.Chrome.HelloAt == "" {
		t.Fatalf("chrome=%#v", got.Chrome)
	}
	if got.Edge == nil || got.Edge.Version != "0.9.0" || got.Edge.RegisteredAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("edge=%#v", got.Edge)
	}
}

func TestBrowserExtensionImportRemovesReadOnlyLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "browser-extension-state.json")
	body := []byte(`{"edge":{"version":"0.9.0","helloAt":"2026-01-02T03:04:05Z","browserRunningAtRegister":true}}`)
	if err := os.WriteFile(legacy, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacy, 0o400); err != nil {
		t.Fatal(err)
	}
	state := readBrowserExtensionState(dir)
	if state.Edge == nil || state.Edge.Version != "0.9.0" || !state.Edge.BrowserRunningAtRegister {
		t.Fatalf("edge=%#v", state.Edge)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("read-only legacy json remains")
	}
}

func TestClearBrowserExtensionRegistrationDropsLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	prev := browserExtensionStateDirHook
	browserExtensionStateDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { browserExtensionStateDirHook = prev })
	legacy := filepath.Join(dir, "browser-extension-state.json")
	if err := os.WriteFile(legacy, []byte(`{"chrome":{"version":"1.0.0","registeredAt":"2026-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearBrowserExtensionRegistration(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("clear left the legacy json")
	}
	got := readBrowserExtensionState(dir)
	if got.Chrome != nil || got.Edge != nil {
		t.Fatalf("cleared state chrome=%#v edge=%#v", got.Chrome, got.Edge)
	}
}
