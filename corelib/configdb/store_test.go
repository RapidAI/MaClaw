package configdb

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReadRawUsesOneRowAfterMigration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(JSONPath(dir), []byte(`{"language":"en","data_dir":"D:/json"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var dataDir string
	if !ReadValue(dir, "data_dir", &dataDir) || dataDir != "D:/json" {
		t.Fatalf("json fallback = %q", dataDir)
	}

	entries := map[string]json.RawMessage{
		"data_dir":   json.RawMessage(`"D:/sqlite"`),
		"permission": json.RawMessage(`{"rules":[{"tool":"bash","effect":"ask"}]}`),
	}
	if err := SaveEntries(DatabasePath(dir), entries); err != nil {
		t.Fatal(err)
	}
	dataDir = ""
	if !ReadValue(dir, "data_dir", &dataDir) || dataDir != "D:/sqlite" {
		t.Fatalf("sqlite value = %q", dataDir)
	}
	permission, ok, err := ReadRaw(dir, "permission")
	if err != nil || !ok || string(permission) != `{"rules":[{"tool":"bash","effect":"ask"}]}` {
		t.Fatalf("permission = %s ok=%v err=%v", permission, ok, err)
	}
	if _, ok, err := ReadRaw(dir, "language"); err != nil || ok {
		t.Fatalf("absent key ok=%v err=%v", ok, err)
	}
}
