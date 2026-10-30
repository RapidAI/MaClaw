package main

import (
	"os"
	"testing"
)

func TestMultibotSaveLeavesUnreadLegacyJSON(t *testing.T) {
	path := useTempMultibotState(t)
	body := []byte(`{"runs":{"run":{"n":3,"at":"2026-10-30T00:00:00Z"}},"seen":{"box|1":"2026-10-30T00:00:00Z"}}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeMultibotState(multibotState{
		Runs: map[string]multibotRunCount{"other": {N: 1, At: "2026-10-30T00:00:00Z"}},
		Seen: map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("save removed unread legacy json: %v", err)
	}
	loaded := readMultibotState()
	if loaded.Runs["run"].N != 3 || loaded.Seen["box|1"] == "" {
		t.Fatalf("import after unread save = %+v", loaded)
	}
	if _, ok := loaded.Runs["other"]; ok {
		t.Fatal("unread save outranked the legacy file")
	}
}
