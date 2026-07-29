package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaclawAppsPanelStatePersistsInDataSQLite(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	state := `{"orderedIds":["pdf-translate"],"customApps":[{"id":"pdf-translate","name":"PDF翻译工具"}]}`
	if err := app.SaveMaclawAppsPanelState(state); err != nil {
		t.Fatalf("SaveMaclawAppsPanelState() error = %v", err)
	}
	if got, err := app.LoadMaclawAppsPanelState(); err != nil || !strings.Contains(got, `"PDF翻译工具"`) || !strings.Contains(got, `"pdf-translate"`) {
		t.Fatalf("LoadMaclawAppsPanelState() = %q, %v; want persisted JSON", got, err)
	}
	path := app.maclawAppsPanelStateDBPath()
	if !strings.HasSuffix(path, filepath.Join("data", "apps_panel.db")) {
		t.Fatalf("database path = %q, want data/apps_panel.db", path)
	}
}

func TestMaclawAppsPanelStateReturnsEmptyForNewDatabase(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	got, err := app.LoadMaclawAppsPanelState()
	if err != nil {
		t.Fatalf("LoadMaclawAppsPanelState() error = %v", err)
	}
	if got != "" {
		t.Fatalf("LoadMaclawAppsPanelState() = %q, want empty state", got)
	}
}

func TestMaclawAppsPanelStateCanonicalizesJSONObject(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMaclawAppsPanelState(` { "z" : 1, "a" : { "enabled" : true } } `); err != nil {
		t.Fatalf("SaveMaclawAppsPanelState() error = %v", err)
	}
	got, err := app.LoadMaclawAppsPanelState()
	if err != nil {
		t.Fatalf("LoadMaclawAppsPanelState() error = %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("persisted state %q is not JSON: %v", got, err)
	}
	if string(decoded["z"]) != "1" || string(decoded["a"]) != `{"enabled":true}` {
		t.Fatalf("persisted state = %q, want canonical object fields", got)
	}
}

func TestMaclawAppsPanelStateRejectsNonObjectJSON(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	for _, state := range []string{"", "[]", `"value"`, "not-json"} {
		if err := app.SaveMaclawAppsPanelState(state); err == nil {
			t.Fatalf("SaveMaclawAppsPanelState(%q) error = nil, want validation error", state)
		}
	}
}
