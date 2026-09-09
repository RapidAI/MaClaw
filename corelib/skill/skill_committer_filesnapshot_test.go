package skill

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSkillCommitterRollbackRestoresDefinitionSidecarSnapshot(t *testing.T) {
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	patchPath := filepath.Join(dir, ".patches.json")
	oldYAML := []byte("name: sidecar-test\nsteps: []\n")
	oldPatch := []byte("[]\n")
	if err := os.WriteFile(yamlPath, oldYAML, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patchPath, oldPatch, 0644); err != nil {
		t.Fatal(err)
	}

	original := []corelib.NLSkillEntry{{Name: "sidecar-test", SkillDir: dir, Status: "active"}}
	candidate := corelib.NLSkillEntry{Name: "sidecar-test", SkillDir: dir, Status: "active"}
	newYAML := []byte("name: sidecar-test\nsteps:\n  - action: shell\n    params:\n      command: echo new\n")
	newPatch := []byte("[{\"find\":\"old\",\"replace\":\"new\"}]\n")
	committer := &SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry { return original },
		SkillSaver:  func([]corelib.NLSkillEntry) error { return nil },
		EntryMerger: func(dst, src *corelib.NLSkillEntry) { dst.Steps = src.Steps },
		CompensationMutator: func(record *EvolutionCompensationRecord) {
			record.SetFileSnapshots([]EvolutionFileSnapshot{{
				Path: patchPath, Exists: true,
				BackupB64: base64.StdEncoding.EncodeToString(oldPatch),
			}})
		},
		DefinitionWriterWithCompensation: func(_ *corelib.NLSkillEntry, record *EvolutionCompensationRecord) error {
			if err := os.WriteFile(yamlPath, newYAML, 0644); err != nil {
				return err
			}
			if err := os.WriteFile(patchPath, newPatch, 0644); err != nil {
				return err
			}
			return record.SetFileSnapshotPostImage(patchPath, newPatch, true)
		},
		IndexRefresher:         func() error { return errors.New("injected index failure") },
		RollbackIndexRefresher: func() error { return nil },
	}
	result := committer.Commit(context.Background(), "sidecar-test", &candidate, "skill:definition_patched", nil)
	if result.State != "rolled_back" || !result.RollbackComplete {
		t.Fatalf("unexpected commit result: %+v", result)
	}
	gotYAML, err := os.ReadFile(yamlPath)
	if err != nil || string(gotYAML) != string(oldYAML) {
		t.Fatalf("YAML was not restored: %q (err=%v)", gotYAML, err)
	}
	gotPatch, err := os.ReadFile(patchPath)
	if err != nil || string(gotPatch) != string(oldPatch) {
		t.Fatalf("sidecar was not restored: %q (err=%v)", gotPatch, err)
	}
}
