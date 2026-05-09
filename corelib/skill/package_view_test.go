package skill

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestPackageViewFromRuntimeEntryRewritesSkillDirRefs(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "scripts", "run.py")
	entry := &corelib.NLSkillEntry{
		Name:     "package-view",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":     "python " + script,
				"working_dir": dir,
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	command, _ := got.Steps[0].Params["command"].(string)
	if !strings.Contains(command, "{baseDir}/scripts/run.py") {
		t.Fatalf("command = %q, want baseDir script reference", command)
	}
	if workingDir, _ := got.Steps[0].Params["working_dir"].(string); workingDir != "" {
		t.Fatalf("working_dir = %q, want package root/empty", workingDir)
	}
	if entry.Steps[0].Params["working_dir"] != dir {
		t.Fatalf("runtime entry was mutated: %#v", entry.Steps[0].Params)
	}
}

func TestPackageViewFromRuntimeEntryRewritesCaseVariantWindowsPathRefs(t *testing.T) {
	dir := t.TempDir()
	caseVariantDir := strings.ToUpper(filepath.ToSlash(dir))
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-case",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python " + caseVariantDir + "/scripts/run.py",
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	command, _ := got.Steps[0].Params["command"].(string)
	if !strings.Contains(command, "{baseDir}/scripts/run.py") {
		t.Fatalf("command = %q, want case-insensitive baseDir script reference", command)
	}
}

func TestPackageViewFromRuntimeEntryDoesNotRewriteSiblingPrefixPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skill")
	for _, sibling := range []string{
		filepath.ToSlash(dir + "-other"),
		strings.ToUpper(filepath.ToSlash(dir + "-other")),
	} {
		entry := &corelib.NLSkillEntry{
			Name:     "package-view-sibling",
			SkillDir: dir,
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "python " + sibling + "/run.py",
				},
			}},
		}

		got := PackageViewFromRuntimeEntry(entry, dir)
		command, _ := got.Steps[0].Params["command"].(string)
		if strings.Contains(command, "{baseDir}") {
			t.Fatalf("command = %q, should not rewrite sibling path sharing skillDir prefix", command)
		}
	}
}

func TestPackageRelativePathFromRuntimePathRejectsOutsidePath(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "run.py")
	if rel, ok := PackageRelativePathFromRuntimePath(dir, outside); ok {
		t.Fatalf("PackageRelativePathFromRuntimePath() = %q, true; want false", rel)
	}
}

func TestPackageViewFromRuntimeEntryDoesNotRewritePlainTextParams(t *testing.T) {
	dir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-text",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{
				"instructions": "The local test directory was " + dir + ".",
				"summary":      "  keep surrounding whitespace  ",
				"notes":        []interface{}{"Observed at " + filepath.Join(dir, "scripts", "run.py")},
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	if got.Steps[0].Params["instructions"] != entry.Steps[0].Params["instructions"] {
		t.Fatalf("instructions = %#v, want unchanged plain text", got.Steps[0].Params["instructions"])
	}
	if got.Steps[0].Params["summary"] != entry.Steps[0].Params["summary"] {
		t.Fatalf("summary = %#v, want unchanged plain text whitespace", got.Steps[0].Params["summary"])
	}
	notes, _ := got.Steps[0].Params["notes"].([]interface{})
	if len(notes) != 1 || notes[0] != entry.Steps[0].Params["notes"].([]interface{})[0] {
		t.Fatalf("notes = %#v, want unchanged plain text array", notes)
	}
}

func TestPackageViewFromRuntimeEntryRewritesPathBearingParams(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "scripts", "run.py")
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-paths",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "mcp_call",
			Params: map[string]interface{}{
				"arguments": map[string]interface{}{
					"script_path": script,
					"items": []interface{}{
						map[string]interface{}{"path": filepath.Join(dir, "data", "input.txt")},
					},
				},
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	args, _ := got.Steps[0].Params["arguments"].(map[string]interface{})
	if args["script_path"] != "{baseDir}/scripts/run.py" {
		t.Fatalf("script_path = %#v, want package-relative path", args["script_path"])
	}
	items, _ := args["items"].([]interface{})
	item, _ := items[0].(map[string]interface{})
	if item["path"] != "{baseDir}/data/input.txt" {
		t.Fatalf("nested path = %#v, want package-relative path", item["path"])
	}
}

func TestPackageViewFromRuntimeEntryRewritesNestedExecutionParams(t *testing.T) {
	dir := t.TempDir()
	fallbackScript := filepath.Join(dir, "fallback.py")
	pipelineInput := filepath.Join(dir, "data", "input.json")
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-nested",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo promoted"},
			FallbackStep: &corelib.NLSkillStep{
				Action: "bash",
				Params: map[string]interface{}{"command": "python " + fallbackScript},
			},
		}},
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "child",
			Params: map[string]string{
				"input_path": pipelineInput,
				"note":       "Observed " + pipelineInput,
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	fallbackCommand, _ := got.Steps[0].FallbackStep.Params["command"].(string)
	if fallbackCommand != "python {baseDir}/fallback.py" {
		t.Fatalf("fallback command = %q, want package-relative command", fallbackCommand)
	}
	if got.Pipeline[0].Params["input_path"] != "{baseDir}/data/input.json" {
		t.Fatalf("pipeline input_path = %q, want package-relative path", got.Pipeline[0].Params["input_path"])
	}
	if got.Pipeline[0].Params["note"] != entry.Pipeline[0].Params["note"] {
		t.Fatalf("pipeline note = %q, want unchanged plain text", got.Pipeline[0].Params["note"])
	}
}

func TestPackageViewFromRuntimeEntryRewritesStructuredCommandArgs(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "scripts", "run.py")
	input := filepath.Join(dir, "data", "input.json")
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-structured-command",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": map[string]interface{}{
					"program": "python",
					"args": []interface{}{
						script,
						"--input=" + input,
						"literal",
					},
				},
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	command, _ := got.Steps[0].Params["command"].(map[string]interface{})
	args, _ := command["args"].([]interface{})
	if len(args) != 3 {
		t.Fatalf("args = %#v, want three command args", command["args"])
	}
	if args[0] != "{baseDir}/scripts/run.py" {
		t.Fatalf("script arg = %#v, want package-relative script", args[0])
	}
	if args[1] != "--input={baseDir}/data/input.json" {
		t.Fatalf("flag arg = %#v, want package-relative input", args[1])
	}
	if args[2] != "literal" {
		t.Fatalf("literal arg = %#v, want unchanged", args[2])
	}
}

func TestPackageViewFromRuntimeEntryRewritesStructuredCommandProgram(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "bin", "runner.exe")
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-structured-program",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": map[string]interface{}{
					"program": binary,
					"args":    []interface{}{"--version"},
				},
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	command, _ := got.Steps[0].Params["command"].(map[string]interface{})
	if command["program"] != "{baseDir}/bin/runner.exe" {
		t.Fatalf("program = %#v, want package-relative executable", command["program"])
	}
}

func TestPackageViewFromRuntimeEntryRewritesInterfaceKeyCommandMap(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "scripts", "run.py")
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-interface-map",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": map[interface{}]interface{}{
					"program": "python",
					"args": []interface{}{
						script,
					},
				},
			},
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	command, ok := got.Steps[0].Params["command"].(map[string]interface{})
	if !ok {
		t.Fatalf("command = %#v, want normalized string-key map", got.Steps[0].Params["command"])
	}
	args, _ := command["args"].([]interface{})
	if len(args) != 1 || args[0] != "{baseDir}/scripts/run.py" {
		t.Fatalf("args = %#v, want package-relative script arg", command["args"])
	}
}

func TestPackageViewFromRuntimeEntryRewritesParamDefaults(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "data", "input.json")
	entry := &corelib.NLSkillEntry{
		Name:     "package-view-param-default",
		SkillDir: dir,
		Params: []corelib.NLSkillParam{{
			Name:    "input",
			Aliases: []string{"i"},
			Default: input,
		}},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	if len(got.Params) != 1 {
		t.Fatalf("len(got.Params) = %d, want 1", len(got.Params))
	}
	if got.Params[0].Default != "{baseDir}/data/input.json" {
		t.Fatalf("default = %q, want package-relative default", got.Params[0].Default)
	}
	if entry.Params[0].Default != input {
		t.Fatalf("runtime entry was mutated: %#v", entry.Params[0])
	}
	if got.Params[0].Aliases[0] != "i" {
		t.Fatalf("aliases = %#v, want cloned alias", got.Params[0].Aliases)
	}
}

func TestPackageViewFromRuntimeEntryRewritesRequiredCredentialFiles(t *testing.T) {
	dir := t.TempDir()
	credential := filepath.Join(dir, "credentials", "api.json")
	entry := &corelib.NLSkillEntry{
		Name:                    "package-view-credentials",
		SkillDir:                dir,
		RequiredCredentialFiles: []string{credential},
	}

	got := PackageViewFromRuntimeEntry(entry, dir)
	if len(got.RequiredCredentialFiles) != 1 || got.RequiredCredentialFiles[0] != "{baseDir}/credentials/api.json" {
		t.Fatalf("RequiredCredentialFiles = %#v, want package-relative credential file", got.RequiredCredentialFiles)
	}
	if entry.RequiredCredentialFiles[0] != credential {
		t.Fatalf("runtime entry was mutated: %#v", entry.RequiredCredentialFiles)
	}
}
