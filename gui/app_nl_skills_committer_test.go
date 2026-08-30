package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCreateNLSkillUsesSharedCommitterAndRollsBackIndexFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return errors.New("injected index provider failure")
		}
		return nil
	}
	err := app.CreateNLSkill(corelib.NLSkillEntry{
		Name: "committer-create", Description: "test", Source: "manual", Status: "active",
		Triggers: []string{"test"}, Steps: []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}}},
	})
	if err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("CreateNLSkill() error = %v, want commit failure", err)
	}
	if app.skillNameAlreadyRegistered("committer-create") {
		t.Fatal("failed create remained registered after index rollback")
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want forward failure and rollback refresh", refreshCalls)
	}
}
