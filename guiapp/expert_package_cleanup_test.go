package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func setupImportedExpertPackageSkill(t *testing.T, name string) (*App, string) {
	t.Helper()
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: "+name+"\ndescription: imported dependency\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: name, SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	return app, dir
}

func TestCleanupImportedExpertPackageSkillsUsesDurableDelete(t *testing.T) {
	app, dir := setupImportedExpertPackageSkill(t, "expert-dependency")

	if err := app.cleanupImportedExpertPackageSkills([]string{"expert-dependency"}); err != nil {
		t.Fatalf("cleanupImportedExpertPackageSkills() error = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("imported skill directory still exists: %v", err)
	}
	for _, entry := range app.skillExecutor.loadSkills() {
		if entry.Name == "expert-dependency" {
			t.Fatalf("imported skill remains in registry: %#v", entry)
		}
	}
}

func TestCleanupImportedExpertPackageSkillsReportsRollbackFailure(t *testing.T) {
	app, dir := setupImportedExpertPackageSkill(t, "expert-dependency-failure")
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return os.ErrPermission
		}
		return nil
	}

	err := app.cleanupImportedExpertPackageSkills([]string{"expert-dependency-failure"})
	if err == nil || !strings.Contains(err.Error(), "rollback imported expert skills") {
		t.Fatalf("cleanupImportedExpertPackageSkills() error = %v, want durable rollback failure", err)
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want commit and rollback refresh", refreshCalls)
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("skill directory was not restored after failed durable delete: %v", statErr)
	}
	found := false
	for _, entry := range app.skillExecutor.loadSkills() {
		if entry.Name == "expert-dependency-failure" {
			found = true
		}
	}
	if !found {
		t.Fatal("skill registry was not restored after failed durable delete")
	}
}
