package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileKVStoreSQLiteRoundTripAndLegacyImport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge_access.json")
	if err := os.WriteFile(path, []byte(`{"tenant/a":"allow","tenant/b":"deny","other":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFileKVStore(path)
	got, err := store.Get(context.Background(), "tenant/a")
	if err != nil || got != "allow" {
		t.Fatalf("get tenant/a = %q err=%v", got, err)
	}
	keys := store.Keys("tenant/")
	if len(keys) != 2 || keys[0] != "tenant/a" || keys[1] != "tenant/b" {
		t.Fatalf("prefix keys = %#v", keys)
	}
	if err := store.Set(context.Background(), "tenant/a", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(context.Background(), "tenant/a"); got != "" {
		t.Fatalf("deleted value = %q", got)
	}
	if err := store.Set(context.Background(), "tenant/c", "ask"); err != nil {
		t.Fatal(err)
	}
	reloaded := newFileKVStore(path)
	if got, _ := reloaded.Get(context.Background(), "tenant/c"); got != "ask" {
		t.Fatalf("reloaded tenant/c = %q", got)
	}
	if got, _ := reloaded.Get(context.Background(), "tenant/a"); got != "" {
		t.Fatalf("reloaded deleted tenant/a = %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"tenant/a":"stale"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	again := newFileKVStore(path)
	if got, _ := again.Get(context.Background(), "tenant/c"); got != "ask" {
		t.Fatalf("stale json clobbered sqlite: %q", got)
	}
	if _, err := os.Stat(fileKVDBPath(path)); err != nil {
		t.Fatal(err)
	}
}

func TestFileKVDropsLegacyFileAfterImport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge_access.json")
	if err := os.WriteFile(path, []byte(`{"tenant/a":"allow"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFileKVStore(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy file remains: %v", err)
	}
	if err := store.Set(context.Background(), "tenant/c", "ask"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DELETE FROM kv_meta WHERE key='legacy_import'`); err != nil {
		t.Fatal(err)
	}
	again := newFileKVStore(path)
	if got, _ := again.Get(context.Background(), "tenant/a"); got != "allow" {
		t.Fatalf("imported key = %q", got)
	}
	if got, _ := again.Get(context.Background(), "tenant/c"); got != "ask" {
		t.Fatalf("later key = %q", got)
	}
}

func TestFileKVKeepsSQLiteWhenLegacyFileIsMissingOrBroken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge_access.json")
	store := newFileKVStore(path)
	if err := store.Set(context.Background(), "tenant/a", "allow"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DELETE FROM kv_meta WHERE key='legacy_import'`); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	kept := newFileKVStore(path)
	if got, _ := kept.Get(context.Background(), "tenant/a"); got != "allow" {
		t.Fatalf("missing json wiped sqlite: %q", got)
	}

	if _, err := kept.db.Exec(`DELETE FROM kv_meta WHERE key='legacy_import'`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := newFileKVStore(path)
	if got, _ := broken.Get(context.Background(), "tenant/a"); got != "allow" {
		t.Fatalf("broken json hid sqlite: %q", got)
	}

	if _, err := broken.db.Exec(`DELETE FROM kv_meta WHERE key='legacy_import'`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	nullDoc := newFileKVStore(path)
	if got, _ := nullDoc.Get(context.Background(), "tenant/a"); got != "allow" {
		t.Fatalf("null json hid sqlite: %q", got)
	}
}
