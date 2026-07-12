package skill

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCloneNLSkillEntry_DeepCopiesStepsAndParams(t *testing.T) {
	src := &corelib.NLSkillEntry{
		Name:         "demo",
		Triggers:     []string{"a", "b"},
		RequiredArgs: []string{"input"},
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo {{input}}",
					"env":     map[string]interface{}{"X": "1"},
				},
				Capture: map[string]string{"out": "(.*)"},
			},
		},
		Params: []corelib.NLSkillParam{
			{Name: "input", Aliases: []string{"text", "q"}},
		},
		RepairHistory: []corelib.SkillRepairRecord{
			{ErrorClass: "missing_param", Explanation: "x"},
		},
	}
	cp := CloneNLSkillEntry(src)
	if cp == nil || cp == src {
		t.Fatal("expected distinct clone")
	}
	cp.Triggers[0] = "mutated"
	if src.Triggers[0] != "a" {
		t.Fatal("triggers not deep-copied")
	}
	cp.Steps[0].Params["command"] = "rm -rf /"
	if src.Steps[0].Params["command"] != "echo {{input}}" {
		t.Fatal("step params not deep-copied")
	}
	env := cp.Steps[0].Params["env"].(map[string]interface{})
	env["X"] = "2"
	srcEnv := src.Steps[0].Params["env"].(map[string]interface{})
	if srcEnv["X"] != "1" {
		t.Fatal("nested param map not deep-copied")
	}
	cp.Params[0].Aliases[0] = "changed"
	if src.Params[0].Aliases[0] != "text" {
		t.Fatal("param aliases not deep-copied")
	}
	cp.RepairHistory[0].ErrorClass = "other"
	if src.RepairHistory[0].ErrorClass != "missing_param" {
		t.Fatal("repair history not deep-copied")
	}
}

func TestExtractSkillPackageFiles_RejectsUnsafeAndPartialWrite(t *testing.T) {
	tmp := t.TempDir()
	err := ExtractSkillPackageFiles("partial", map[string]string{
		"safe/ok.txt": base64.StdEncoding.EncodeToString([]byte("ok")),
		"../bad.txt":  base64.StdEncoding.EncodeToString([]byte("bad")),
	}, tmp)
	if err == nil {
		t.Fatal("expected unsafe path error")
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "safe", "ok.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("safe file was written before invalid package failed; stat=%v", statErr)
	}
}
