package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestListSkillAppManifestsReadsPrivateExtension(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "doc-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [{
			"id": "redact",
			"name": "Document Redaction",
			"description": "Upload a document and return a redacted copy.",
			"category": "Document",
			"icon": "shield",
			"input_mode": "file",
			"multiple_files": true,
			"output_modes": ["docx", "pdf"]
		}]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "doc-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items := app.ListSkillAppManifests()
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1: %#v", len(items), items)
	}
	if items[0].ID != "redact" || items[0].SkillID != "doc-tools" || items[0].Name != "Document Redaction" || items[0].InputMode != "file" {
		t.Fatalf("unexpected manifest item: %#v", items[0])
	}
	if !items[0].MultipleFiles {
		t.Fatalf("expected multiple_files=true: %#v", items[0])
	}
	if len(items[0].OutputModes) != 2 || items[0].OutputModes[0] != "docx" || items[0].OutputModes[1] != "pdf" {
		t.Fatalf("unexpected output modes: %#v", items[0].OutputModes)
	}
}

func TestStageSkillAppInputFileWritesTempFile(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}

	ref, err := app.StageSkillAppInputFile("../demo.pdf", "application/pdf", 1234, base64.StdEncoding.EncodeToString([]byte("pdf data")))
	if err != nil {
		t.Fatalf("StageSkillAppInputFile() error = %v", err)
	}
	if ref.Name != "demo.pdf" || ref.Type != "application/pdf" || ref.Size != int64(len("pdf data")) || ref.LastModified != 1234 || ref.Transfer != "staged_file" {
		t.Fatalf("unexpected file ref: %#v", ref)
	}
	if !strings.Contains(filepath.Clean(ref.StagedPath), filepath.Clean(filepath.Join(tmpHome, ".maclaw", "temp", "app-inputs"))) {
		t.Fatalf("staged path outside app temp: %s", ref.StagedPath)
	}
	got, err := os.ReadFile(ref.StagedPath)
	if err != nil {
		t.Fatalf("ReadFile staged path: %v", err)
	}
	if string(got) != "pdf data" {
		t.Fatalf("staged content = %q, want pdf data", string(got))
	}
}

func TestStageSkillAppInputFileRejectsInvalidPayload(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.StageSkillAppInputFile("demo.pdf", "application/pdf", 0, "not base64"); err == nil {
		t.Fatal("expected invalid base64 error")
	}
	tooLarge := base64.StdEncoding.EncodeToString(make([]byte, maxSkillAppInputFileBytes+1))
	if _, err := app.StageSkillAppInputFile("big.bin", "application/octet-stream", 0, tooLarge); err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestStageSkillAppInputFileCleansStaleInputDirs(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	root := filepath.Join(app.GetTempDir(), "app-inputs")
	oldDir := filepath.Join(root, "input-old")
	freshDir := filepath.Join(root, "input-fresh")
	foreignDir := filepath.Join(root, "keep-old")
	for _, dir := range []string{oldDir, freshDir, foreignDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "payload.txt"), []byte("data"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", dir, err)
		}
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	for _, path := range []string{oldDir, filepath.Join(oldDir, "payload.txt"), foreignDir, filepath.Join(foreignDir, "payload.txt")} {
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes %s: %v", path, err)
		}
	}

	if _, err := app.StageSkillAppInputFile("demo.txt", "text/plain", 0, base64.StdEncoding.EncodeToString([]byte("demo"))); err != nil {
		t.Fatalf("StageSkillAppInputFile() error = %v", err)
	}

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("expected stale input dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatalf("fresh input dir should remain: %v", err)
	}
	if _, err := os.Stat(foreignDir); err != nil {
		t.Fatalf("non input-* dir should remain: %v", err)
	}
}

func TestRunNLSkillAsyncCleansStagedInputWhenStartFails(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	ref, err := app.StageSkillAppInputFile("demo.txt", "text/plain", 0, base64.StdEncoding.EncodeToString([]byte("demo")))
	if err != nil {
		t.Fatalf("StageSkillAppInputFile() error = %v", err)
	}

	_, err = app.RunNLSkillAsync("missing-skill", map[string]interface{}{
		"file": map[string]interface{}{"staged_path": ref.StagedPath},
	})

	if err == nil {
		t.Fatal("expected missing skill error")
	}
	if _, statErr := os.Stat(ref.StagedPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected staged input cleanup after start failure, stat err=%v", statErr)
	}
}

func TestWithSkillAppInputFileAliasesAddsTemplateFriendlyPaths(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"file": map[string]interface{}{
			"name":        "demo.pdf",
			"staged_path": "/tmp/maclaw/demo.pdf",
		},
		"files": []map[string]interface{}{
			{"staged_path": "/tmp/maclaw/demo.pdf"},
			{"staged_path": "/tmp/maclaw/extra.pdf"},
		},
	})

	for _, key := range []string{"file_path", "input_file_path", "local_file_path", "uploaded_file_path"} {
		if got[key] != "/tmp/maclaw/demo.pdf" {
			t.Fatalf("%s = %#v, want staged path in %#v", key, got[key], got)
		}
	}
	if got["file_name"] != "demo.pdf" {
		t.Fatalf("file_name = %#v, want demo.pdf", got["file_name"])
	}
	paths, ok := got["file_paths"].([]string)
	if !ok || len(paths) != 2 || paths[0] != "/tmp/maclaw/demo.pdf" || paths[1] != "/tmp/maclaw/extra.pdf" {
		t.Fatalf("file_paths = %#v, want both staged paths", got["file_paths"])
	}
}

func TestWithSkillAppInputFileAliasesDoesNotOverrideExplicitValues(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"file_path": "explicit.txt",
		"file_name": "explicit-name.txt",
		"file": map[string]interface{}{
			"name":        "demo.pdf",
			"staged_path": "/tmp/maclaw/demo.pdf",
		},
	})

	if got["file_path"] != "explicit.txt" || got["file_name"] != "explicit-name.txt" {
		t.Fatalf("explicit aliases should win: %#v", got)
	}
	if got["input_file_path"] != "/tmp/maclaw/demo.pdf" {
		t.Fatalf("missing non-conflicting alias: %#v", got)
	}
}

func TestListSkillAppManifestsNormalizesPrivateExtension(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "sheet-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [{
			"id": "clean",
			"name": "Sheet Clean",
			"category": "",
			"icon": "unknown",
			"input_mode": "weird",
			"output_modes": ["xlsx", "bad", "xlsx", "JSON"]
		}]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "sheet-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items := app.ListSkillAppManifests()
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1: %#v", len(items), items)
	}
	if items[0].Category != "Skill" || items[0].Icon != "contract" || items[0].InputMode != "file" {
		t.Fatalf("unexpected normalized fields: %#v", items[0])
	}
	if len(items[0].OutputModes) != 2 || items[0].OutputModes[0] != "xlsx" || items[0].OutputModes[1] != "json" {
		t.Fatalf("unexpected normalized output modes: %#v", items[0].OutputModes)
	}
}

func TestListSkillAppManifestsNormalizesFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "field-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [{
			"id": "fields",
			"name": "Field Tool",
			"input_mode": "form",
			"fields": [
				{ "name": "", "label": "Skip me" },
				{ "name": " title ", "type": "unknown", "required": true, "default": 42 },
				{ "name": "format", "type": "SELECT", "default": "Summary", "options": ["Detailed", "Summary", "Detailed", 2] },
				{ "name": "include_refs", "type": "boolean", "required": true, "default": "true", "options": ["ignored"] }
			]
		}]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "field-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items := app.ListSkillAppManifests()
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1: %#v", len(items), items)
	}
	fields := items[0].Fields
	if len(fields) != 3 {
		t.Fatalf("fields len=%d want 3: %#v", len(fields), fields)
	}
	if fields[0].Name != "title" || fields[0].Label != "title" || fields[0].Type != "text" || !fields[0].Required || fields[0].Default != "42" {
		t.Fatalf("unexpected text field: %#v", fields[0])
	}
	if fields[1].Type != "select" || len(fields[1].Options) != 3 || fields[1].Options[0] != "Summary" || fields[1].Options[1] != "Detailed" || fields[1].Options[2] != "2" {
		t.Fatalf("unexpected select field: %#v", fields[1])
	}
	if fields[2].Type != "boolean" || fields[2].Default != true || len(fields[2].Options) != 0 {
		t.Fatalf("unexpected boolean field: %#v", fields[2])
	}
}

func TestNormalizeSkillAppIconAndInputModeLowercase(t *testing.T) {
	if got := normalizeSkillAppIconName(" PDF "); got != "pdf" {
		t.Fatalf("normalizeSkillAppIconName()=%q want pdf", got)
	}
	if got := normalizeSkillAppInputMode(" Mixed "); got != "mixed" {
		t.Fatalf("normalizeSkillAppInputMode()=%q want mixed", got)
	}
}

func TestListSkillAppManifestsRequiresV1PrivateMarker(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "bad-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "not-v1",
		"apps": [{ "id": "bad", "name": "Bad App" }]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "bad-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if items := app.ListSkillAppManifests(); len(items) != 0 {
		t.Fatalf("items len=%d want 0: %#v", len(items), items)
	}
}
