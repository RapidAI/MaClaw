package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func newRepairTransactionFixture(t *testing.T, name string) (*App, *SkillRunner, string, []byte) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("name: " + name + "\ndescription: transaction fixture\nsteps:\n  - action: bash\n    params:\n      command: echo old\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{{
		Name: name, Source: "file", SkillDir: dir, Status: "active",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo old"}}},
	}}, nil)
	return app, NewSkillRunner(app.skillExecutor), dir, original
}

func TestPersistRepairResultRollsBackOnFinalAuditFailure(t *testing.T) {
	app, runner, dir, original := newRepairTransactionFixture(t, "audit-rollback")
	runner.auditFn = func(string, map[string]string) error { return errors.New("injected audit failure") }
	entry := &corelib.NLSkillEntry{
		Name: "audit-rollback", Source: "file", SkillDir: dir, Status: "active",
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo new"}}},
		RepairAttemptCount: 1, LastRepairAt: "2026-08-29T00:00:00Z",
	}
	err := runner.persistRepairResultWithContext(context.Background(), entry)
	if err == nil || !strings.Contains(err.Error(), "final repair audit") {
		t.Fatalf("persist error = %v, want final audit failure", err)
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != string(original) {
		t.Fatalf("YAML was not restored after audit failure: %q", data)
	}
	loaded, loadErr := app.LoadConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	for _, skill := range loaded.NLSkills {
		if skill.Name == entry.Name && skill.RepairAttemptCount != 0 {
			t.Fatalf("config metadata was not restored: %+v", skill)
		}
	}
}

func TestPersistRepairResultRollsBackOnIndexRefreshFailure(t *testing.T) {
	app, runner, dir, original := newRepairTransactionFixture(t, "index-rollback")
	runner.indexRefreshFn = func(string) error { return errors.New("injected index failure") }
	entry := &corelib.NLSkillEntry{
		Name: "index-rollback", Source: "file", SkillDir: dir, Status: "active",
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo new"}}},
		RepairAttemptCount: 1, LastRepairAt: "2026-08-29T00:00:00Z",
	}
	err := runner.persistRepairResultWithContext(context.Background(), entry)
	if err == nil || !strings.Contains(err.Error(), "refresh skill index") {
		t.Fatalf("persist error = %v, want index refresh failure", err)
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != string(original) {
		t.Fatalf("YAML was not restored after index failure: %q", data)
	}
	loaded, loadErr := app.LoadConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	for _, skill := range loaded.NLSkills {
		if skill.Name == entry.Name && skill.RepairAttemptCount != 0 {
			t.Fatalf("config metadata was not restored: %+v", skill)
		}
	}
}
