package guiapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func prepareRecordedSkillForCommit(t *testing.T, app *App) {
	t.Helper()
	recorder := NewSkillOperationRecorder()
	if err := recorder.Start(t.TempDir(), desktopUserID); err != nil {
		t.Fatal(err)
	}
	recorder.Record("bash", map[string]interface{}{"command": "echo recorded"}, "ok", true)
	app.skillRecorder = recorder
}

func TestResolveSkillRecordingCommitsStagedDefinition(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatal(err)
	}
	prepareRecordedSkillForCommit(t, app)

	out := app.ResolveSkillRecording("save", "recorded-transaction", "record a repeatable command")
	if out["status"] != "saved" {
		t.Fatalf("ResolveSkillRecording result=%#v", out)
	}
	loaded, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.NLSkills) != 1 {
		t.Fatalf("recorded skill config=%#v, want one committed entry", loaded.NLSkills)
	}
	entry := loaded.NLSkills[0]
	if entry.Name != "recorded-transaction" || entry.Source != "learned" {
		t.Fatalf("recorded skill provenance=%#v", entry)
	}
	root, err := app.primarySkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Clean(entry.SkillDir)) != filepath.Clean(root) {
		t.Fatalf("recorded skill dir=%q, want direct child of %q", entry.SkillDir, root)
	}
	if _, err := os.Stat(filepath.Join(entry.SkillDir, "skill.yaml")); err != nil {
		t.Fatalf("recorded skill definition missing: %v", err)
	}
	stagingRoot, err := app.skillStagingDir()
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(stagingRoot); err == nil && len(entries) != 0 {
		t.Fatalf("recording commit left staging entries: %#v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read staging root: %v", err)
	}
}

func TestResolveSkillRecordingRollsBackStagingOnIndexFailure(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatal(err)
	}
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return os.ErrPermission
		}
		return nil
	}
	prepareRecordedSkillForCommit(t, app)

	out := app.ResolveSkillRecording("save", "recording-index-failure", "record a repeatable command")
	if _, ok := out["error"].(string); !ok {
		t.Fatalf("ResolveSkillRecording result=%#v, want commit failure", out)
	}
	if refreshCalls < 2 {
		t.Fatalf("index refresh calls=%d, want forward failure plus rollback refresh", refreshCalls)
	}
	loaded, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.NLSkills) != 0 {
		t.Fatalf("failed recorded skill remained registered: %#v", loaded.NLSkills)
	}
	root, err := app.primarySkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) != 0 {
		t.Fatalf("failed recorded skill left durable entries: %#v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read primary skills root: %v", err)
	}
}
