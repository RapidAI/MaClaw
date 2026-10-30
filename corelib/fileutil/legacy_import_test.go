package fileutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyImportPending(t *testing.T) {
	dir := t.TempDir()
	validJSON := func(data []byte) bool { return json.Valid(data) }
	type peerDoc struct {
		LastPeer map[string]string `json:"last_peer"`
	}
	asPeerDoc := func(data []byte) bool {
		var doc peerDoc
		return json.Unmarshal(data, &doc) == nil
	}
	asList := func(data []byte) bool {
		var doc []struct{}
		return json.Unmarshal(data, &doc) == nil
	}

	if LegacyImportPending("", validJSON) || LegacyImportPending(filepath.Join(dir, "missing.json"), validJSON) {
		t.Fatal("blank or missing path is not a pending import")
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LegacyImportPending(empty, validJSON) {
		t.Fatal("empty file is not a pending import")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LegacyImportPending(bad, validJSON) || LegacyImportPending(bad, asPeerDoc) {
		t.Fatal("invalid json is not a pending import")
	}
	if !LegacyImportPending(bad, nil) {
		t.Fatal("untyped non-empty file stays pending")
	}
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LegacyImportPending(good, validJSON) || !LegacyImportPending(good, asPeerDoc) || !LegacyImportPending(good, nil) {
		t.Fatal("document this store can read stays pending")
	}
	shape := filepath.Join(dir, "shape.json")
	if err := os.WriteFile(shape, []byte(`{"last_peer":"nope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LegacyImportPending(shape, validJSON) {
		t.Fatal("wrong field type is still valid json")
	}
	if LegacyImportPending(shape, asPeerDoc) {
		t.Fatal("wrong field type is not a pending import")
	}
	list := filepath.Join(dir, "list.json")
	if err := os.WriteFile(list, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if LegacyImportPending(list, asList) {
		t.Fatal("object is not a list document")
	}
	if err := os.WriteFile(list, []byte(`null`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LegacyImportPending(list, asList) {
		t.Fatal("null list stays pending")
	}
	asDir := filepath.Join(dir, "dir.json")
	if err := os.Mkdir(asDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if LegacyImportPending(asDir, asPeerDoc) || LegacyImportPending(asDir, nil) {
		t.Fatal("directory is not a pending import")
	}
}
