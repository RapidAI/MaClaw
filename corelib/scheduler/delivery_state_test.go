package scheduler

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestDeliveryStateStoreRememberAndResolve(t *testing.T) {
	dir := t.TempDir()
	s := NewDeliveryStateStore(dir)
	if s.GetLastPeer("telegram") != "" {
		t.Fatal("expected empty")
	}
	s.RememberPeer("telegram", "42")
	if got := s.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("got %q", got)
	}
	// Reload
	s2 := NewDeliveryStateStore(dir)
	if got := s2.ResolveSelfPeer("telegram", "self"); got != "42" {
		t.Fatalf("resolve self=%q", got)
	}
	if got := s2.ResolveSelfPeer("telegram", "99"); got != "99" {
		t.Fatalf("explicit=%q", got)
	}
	// self must not overwrite
	s2.RememberPeer("telegram", "self")
	if got := s2.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("self clobber=%q", got)
	}
	if !IsSelfPeerID("SELF") || IsSelfPeerID("42") {
		t.Fatal("IsSelfPeerID")
	}
	if (*DeliveryStateStore)(nil).ResolveSelfPeer("telegram", "self") != "" {
		t.Fatal("nil store resolve self")
	}
	if (*DeliveryStateStore)(nil).ResolveSelfPeer("telegram", "9") != "9" {
		t.Fatal("nil store resolve explicit")
	}
	// No-op rewrite should not error
	s2.RememberPeer("telegram", "42")
	if got := s2.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("after noop write: %q", got)
	}
	if _, err := filepath.Glob(filepath.Join(dir, DeliveryStateFileName)); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryStateImportsLegacyJSONAndIgnoresCorruptFile(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, DeliveryStateFileName)
	if err := os.WriteFile(jsonPath, []byte(`{"last_peer":{"Telegram":"42"},"updated_at":"2026-05-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewDeliveryStateStore(dir)
	if got := store.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("imported peer = %q", got)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("legacy json still present: %v", err)
	}
	reloaded := NewDeliveryStateStore(dir)
	if got := reloaded.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("reloaded peer = %q", got)
	}

	db, err := sql.Open("sqlite", deliveryStateDBPath(jsonPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM delivery_meta WHERE key = 'legacy_import'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := NewDeliveryStateStore(dir)
	if got := kept.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("corrupt json replaced rows: %q", got)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("corrupt json should stay until a valid import: %v", err)
	}
	kept.RememberPeer("weixin", "7")
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("later save left corrupt json: %v", err)
	}
	if got := kept.GetLastPeer("telegram"); got != "42" {
		t.Fatalf("peer after save = %q", got)
	}
	if got := kept.GetLastPeer("weixin"); got != "7" {
		t.Fatalf("new peer = %q", got)
	}
}

func TestDeliverySaveLeavesUnreadLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, DeliveryStateFileName)
	body := []byte(`{"last_peer":{"telegram":"42"},"updated_at":"2026-05-01T00:00:00Z"}`)
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewDeliveryStateStore(dir)
	store.mu.Lock()
	store.cache = &DeliveryState{LastPeer: map[string]string{"telegram": "99"}}
	err := store.saveLocked(*store.cache)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(jsonPath)
	if err != nil || string(got) != string(body) {
		t.Fatalf("legacy file changed: %v %s", err, got)
	}
	reloaded := NewDeliveryStateStore(dir)
	if peer := reloaded.GetLastPeer("telegram"); peer != "42" {
		t.Fatalf("import after unread save = %q", peer)
	}
}

func TestDeliveryWrongShapeLegacyDoesNotBlockRemember(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, DeliveryStateFileName)
	if err := os.WriteFile(jsonPath, []byte(`{"last_peer":"nope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewDeliveryStateStore(dir)
	if got := store.GetLastPeer("telegram"); got != "" {
		t.Fatalf("wrong-shape import = %q", got)
	}
	store.RememberPeer("telegram", "7")
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("wrong-shape json still present: %v", err)
	}
	if got := store.GetLastPeer("telegram"); got != "7" {
		t.Fatalf("peer = %q", got)
	}
	reloaded := NewDeliveryStateStore(dir)
	if got := reloaded.GetLastPeer("telegram"); got != "7" {
		t.Fatalf("reloaded = %q", got)
	}
}

func TestDeliveryDirectoryLegacyDoesNotBlockRemember(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, DeliveryStateFileName)
	if err := os.Mkdir(jsonPath, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewDeliveryStateStore(dir)
	store.RememberPeer("telegram", "7")
	if got := store.GetLastPeer("telegram"); got != "7" {
		t.Fatalf("peer = %q", got)
	}
	reloaded := NewDeliveryStateStore(dir)
	if got := reloaded.GetLastPeer("telegram"); got != "7" {
		t.Fatalf("reloaded = %q", got)
	}
}

func TestDeliveryRememberPeerRetriesAfterSaveFailure(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, DeliveryStateFileName)
	store := NewDeliveryStateStore(dir)
	store.RememberPeer("telegram", "1")
	if got := store.GetLastPeer("telegram"); got != "1" {
		t.Fatalf("stored peer = %q", got)
	}
	dbPath := deliveryStateDBPath(jsonPath)
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(dbPath, 0o755); err != nil {
		t.Fatal(err)
	}
	store.RememberPeer("telegram", "2")
	if got := store.GetLastPeer("telegram"); got != "1" {
		t.Fatalf("failed save kept peer = %q", got)
	}
	if err := os.RemoveAll(dbPath); err != nil {
		t.Fatal(err)
	}
	store.RememberPeer("telegram", "2")
	reloaded := NewDeliveryStateStore(dir)
	if got := reloaded.GetLastPeer("telegram"); got != "2" {
		t.Fatalf("retry wrote peer = %q", got)
	}
}

func TestDeliveryLoadRetriesWhenDatabaseIsBlocked(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, DeliveryStateFileName)
	body := []byte(`{"last_peer":{"Telegram":"42"},"updated_at":"2026-05-01T00:00:00Z"}`)
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(deliveryStateDBPath(jsonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewDeliveryStateStore(dir)
	store.RememberPeer("telegram", "99")
	got, err := os.ReadFile(jsonPath)
	if err != nil || string(got) != string(body) {
		t.Fatalf("blocked load changed legacy file: %v %s", err, got)
	}
	if err := os.RemoveAll(deliveryStateDBPath(jsonPath)); err != nil {
		t.Fatal(err)
	}
	if peer := store.GetLastPeer("telegram"); peer != "42" {
		t.Fatalf("retry import = %q", peer)
	}
}

func TestPeerIDFromTarget(t *testing.T) {
	t.Parallel()
	if PeerIDFromTarget(DeliveryTarget{UserID: "u", GroupID: "g"}) != "u" {
		t.Fatal("prefer user")
	}
	if PeerIDFromTarget(DeliveryTarget{GroupID: "g"}) != "g" {
		t.Fatal("group")
	}
}
