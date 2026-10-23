package guiapp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopBotAppearancePersistsInDataSQLite(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveDesktopBotLook("alice", "bot_1", "#2563eb", "cloud"); err != nil {
		t.Fatalf("SaveDesktopBotLook() error = %v", err)
	}
	if err := app.SaveDesktopBotLook("bob", "bot_9", "#16a34a", "leaf"); err != nil {
		t.Fatalf("SaveDesktopBotLook(bob) error = %v", err)
	}
	got, err := app.LoadDesktopBotLooks("alice", "", "")
	if err != nil {
		t.Fatalf("LoadDesktopBotLooks() error = %v", err)
	}
	if len(got.Looks) != 1 || got.Looks["bot_1"].Color != "#2563eb" || got.Looks["bot_1"].Shape != "cloud" {
		t.Fatalf("alice looks = %+v", got.Looks)
	}
	if len(got.Migrated) != 0 {
		t.Fatalf("migrated = %+v, want empty", got.Migrated)
	}
	path := app.desktopBotLooksDBPath()
	if !strings.HasSuffix(path, filepath.Join("data", "desktop_bot_appearance.db")) {
		t.Fatalf("database path = %q, want data/desktop_bot_appearance.db", path)
	}
}

func TestDesktopBotAppearanceImportsLegacyWebViewDocument(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	legacy := `{"alice":{"bot_1":{"color":"#2563eb","shape":"cloud"},"bad":{"color":"red","shape":"cloud"}},"bob":{"bot_2":{"color":"#db2777","shape":"pill"}}}`
	migrated := `{"alice":["bot_1","bot_1"]}`
	got, err := app.LoadDesktopBotLooks("alice", legacy, migrated)
	if err != nil {
		t.Fatalf("import load error = %v", err)
	}
	if got.Looks["bot_1"].Shape != "cloud" || got.Looks["bad"].Color != "" {
		t.Fatalf("imported alice looks = %+v", got.Looks)
	}
	if len(got.Migrated) != 1 || got.Migrated[0] != "bot_1" {
		t.Fatalf("imported migrated = %+v", got.Migrated)
	}
	again, err := app.LoadDesktopBotLooks("bob", "", "")
	if err != nil {
		t.Fatalf("second load error = %v", err)
	}
	if again.Looks["bot_2"].Color != "#db2777" || again.Looks["bot_2"].Shape != "pill" {
		t.Fatalf("bob looks = %+v", again.Looks)
	}
}

func TestDesktopBotAppearanceClearAndMark(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveDesktopBotLook("alice", "bot_1", "#2563eb", "cloud"); err != nil {
		t.Fatalf("save error = %v", err)
	}
	if err := app.MarkDesktopBotLookMigrated("alice", "bot_1"); err != nil {
		t.Fatalf("mark error = %v", err)
	}
	if err := app.MarkDesktopBotLookMigrated("alice", "bot_1"); err != nil {
		t.Fatalf("second mark error = %v", err)
	}
	if err := app.ClearDesktopBotLook("alice", "bot_1"); err != nil {
		t.Fatalf("clear error = %v", err)
	}
	got, err := app.LoadDesktopBotLooks("alice", "", "")
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	if len(got.Looks) != 0 {
		t.Fatalf("looks after clear = %+v", got.Looks)
	}
	if len(got.Migrated) != 1 || got.Migrated[0] != "bot_1" {
		t.Fatalf("migrated after clear = %+v", got.Migrated)
	}
	// Saving another color replaces the look only. The copied mark stays until Unmark.
	if err := app.SaveDesktopBotLook("alice", "bot_1", "#16a34a", "leaf"); err != nil {
		t.Fatalf("save replacement error = %v", err)
	}
	if err := app.MarkDesktopBotLookMigrated("bob", "bot_9"); err != nil {
		t.Fatalf("mark bob error = %v", err)
	}
	replaced, err := app.LoadDesktopBotLooks("alice", "", "")
	if err != nil {
		t.Fatalf("load replacement error = %v", err)
	}
	if replaced.Looks["bot_1"].Color != "#16a34a" || replaced.Looks["bot_1"].Shape != "leaf" {
		t.Fatalf("replacement look = %+v", replaced.Looks["bot_1"])
	}
	if len(replaced.Migrated) != 1 || replaced.Migrated[0] != "bot_1" {
		t.Fatalf("save must keep migrated mark: %+v", replaced.Migrated)
	}
	if err := app.UnmarkDesktopBotLookMigrated("alice", "bot_1"); err != nil {
		t.Fatalf("unmark error = %v", err)
	}
	if err := app.UnmarkDesktopBotLookMigrated("alice", "bot_1"); err != nil {
		t.Fatalf("second unmark error = %v", err)
	}
	if err := app.UnmarkDesktopBotLookMigrated("alice", ""); err == nil {
		t.Fatal("empty bot id was accepted")
	}
	unmarked, err := app.LoadDesktopBotLooks("alice", "", "")
	if err != nil {
		t.Fatalf("load unmarked error = %v", err)
	}
	if len(unmarked.Migrated) != 0 {
		t.Fatalf("migrated after unmark = %+v", unmarked.Migrated)
	}
	if unmarked.Looks["bot_1"].Color != "#16a34a" || unmarked.Looks["bot_1"].Shape != "leaf" {
		t.Fatalf("unmark removed the look: %+v", unmarked.Looks["bot_1"])
	}
	bob, err := app.LoadDesktopBotLooks("bob", "", "")
	if err != nil {
		t.Fatalf("load bob error = %v", err)
	}
	if len(bob.Migrated) != 1 || bob.Migrated[0] != "bot_9" {
		t.Fatalf("bob migrated = %+v", bob.Migrated)
	}
}

func TestDesktopBotAppearanceRejectsUnknownLook(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	for _, call := range []struct{ color, shape string }{
		{"red", "cloud"},
		{"#2563eb", "hexagon"},
		{"", "circle"},
	} {
		if err := app.SaveDesktopBotLook("alice", "bot_1", call.color, call.shape); err == nil {
			t.Fatalf("SaveDesktopBotLook(%q, %q) error = nil", call.color, call.shape)
		}
	}
	if err := app.SaveDesktopBotLook("alice", "", "#2563eb", "circle"); err == nil {
		t.Fatal("empty bot id was accepted")
	}
	if _, err := app.LoadDesktopBotLooks("alice", "{", ""); err == nil {
		t.Fatal("broken legacy JSON was accepted")
	}
}

func TestDesktopBotAppearanceImportKeepsSavedLook(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveDesktopBotLook("alice", "bot_1", "#2563eb", "cloud"); err != nil {
		t.Fatalf("save error = %v", err)
	}
	legacy := `{"alice":{"bot_1":{"color":"#db2777","shape":"pill"},"bot_2":{"color":"#16a34a","shape":"leaf"}}}`
	got, err := app.LoadDesktopBotLooks("alice", legacy, "")
	if err != nil {
		t.Fatalf("import error = %v", err)
	}
	if got.Looks["bot_1"].Color != "#2563eb" || got.Looks["bot_1"].Shape != "cloud" {
		t.Fatalf("saved look replaced = %+v", got.Looks["bot_1"])
	}
	if got.Looks["bot_2"].Color != "#16a34a" || got.Looks["bot_2"].Shape != "leaf" {
		t.Fatalf("new legacy look = %+v", got.Looks["bot_2"])
	}
	if _, err := app.LoadDesktopBotLooks("alice", "{", ""); err == nil {
		t.Fatal("broken legacy JSON was accepted")
	}
	again, err := app.LoadDesktopBotLooks("alice", "", "")
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if again.Looks["bot_1"].Color != "#2563eb" || again.Looks["bot_2"].Shape != "leaf" {
		t.Fatalf("rows after failed import = %+v", again.Looks)
	}
}

func TestDesktopBotAppearanceImportSkipsBadEntries(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	legacy := `{"alice":{"bot_ok":{"color":"#2563eb","shape":"cloud"},"bot_bad":"nope","bot_num":{"color":1,"shape":"blob"}},"not-bots":["x"],"":{"bot_x":{"color":"#111111","shape":"circle"}}}`
	migrated := `{"alice":["bot_ok",7,{"id":"z"}],"bob":{"bot_9":true}}`
	got, err := app.LoadDesktopBotLooks("alice", legacy, migrated)
	if err != nil {
		t.Fatalf("import error = %v", err)
	}
	if len(got.Looks) != 1 || got.Looks["bot_ok"].Color != "#2563eb" || got.Looks["bot_ok"].Shape != "cloud" {
		t.Fatalf("looks = %+v", got.Looks)
	}
	if len(got.Migrated) != 1 || got.Migrated[0] != "bot_ok" {
		t.Fatalf("migrated = %+v", got.Migrated)
	}
	again, err := app.LoadDesktopBotLooks("alice", `[{"color":"#000000"}]`, `[]`)
	if err != nil {
		t.Fatalf("non-object import error = %v", err)
	}
	if len(again.Looks) != 1 || again.Looks["bot_ok"].Color != "#2563eb" {
		t.Fatalf("non-object import changed looks = %+v", again.Looks)
	}
}

func TestDesktopBotAppearanceEmptyAccount(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	got, err := app.LoadDesktopBotLooks("", "", "")
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	if got.Looks == nil || len(got.Looks) != 0 || got.Migrated == nil || len(got.Migrated) != 0 {
		t.Fatalf("empty file = %+v", got)
	}
}
