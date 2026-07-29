package skill

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestIsSourceAllowedNormalizesAliases(t *testing.T) {
	allowed := []string{"hubcenter", "git_hub", "enterprise", "zip"}
	for _, source := range []string{"skillhub", "github", "enterprise_hub", "local", "local_upload"} {
		if !IsSourceAllowed(source, allowed) {
			t.Fatalf("source %q should be allowed by aliases %#v", source, allowed)
		}
	}
	if IsSourceAllowed("clawhub", allowed) {
		t.Fatal("clawhub should not be allowed")
	}
}

func TestEntryFromSkillHubDownload_ParsesSteps(t *testing.T) {
	entry, err := entryFromSkillHubDownload(skillHubDownloadResponse{
		skillHubItem: skillHubItem{
			ID:          "hub-1",
			Name:        "demo-skill",
			Description: "demo",
			Version:     "1",
		},
		Triggers: []string{"demo"},
		Steps: []skillHubDownloadStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}},
		},
	}, HubDownloadOptions{HubURL: "https://hub.example", SkillID: "hub-1", SkipExtract: true})
	if err != nil {
		t.Fatalf("entryFromSkillHubDownload: %v", err)
	}
	if entry.Name != "demo-skill" {
		t.Fatalf("name = %q", entry.Name)
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "bash" {
		t.Fatalf("steps = %#v", entry.Steps)
	}
	if entry.TrustLevel != "trusted" {
		t.Fatalf("trust = %q, want trusted", entry.TrustLevel)
	}
}

func TestEntryFromSkillHubDownload_SynthesizesCraftToolFromSKILLMD(t *testing.T) {
	tmp := t.TempDir()
	md := base64.StdEncoding.EncodeToString([]byte("# Hello\n\nDo the thing."))
	steps := craftToolStepsFromHubFiles(map[string]string{"SKILL.md": md}, tmp)
	if len(steps) != 1 || steps[0].Action != "craft_tool" {
		t.Fatalf("craft steps = %#v", steps)
	}
	if err := extractHubBundledFiles("demo-md", map[string]string{"SKILL.md": md}, tmp); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md not written: %v", err)
	}
}

func TestEntryFromSkillHubDownload_RejectsEmptyPayload(t *testing.T) {
	_, err := entryFromSkillHubDownload(skillHubDownloadResponse{
		skillHubItem: skillHubItem{Name: "empty"},
	}, HubDownloadOptions{HubURL: "https://hub.example", SkillID: "empty", SkipExtract: true})
	if err == nil {
		t.Fatal("expected error for empty steps/files")
	}
}

func TestEntryFromSkillHubDownload_AcceptsMaclawAppPackageWithoutSkillSteps(t *testing.T) {
	tmp := t.TempDir()
	appDefinition := base64.StdEncoding.EncodeToString([]byte(`{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"app":{"id":"pdf-translator","name":"PDF Translator"}
	}`))
	entry, err := entryFromSkillHubDownload(skillHubDownloadResponse{
		skillHubItem: skillHubItem{Name: "skill-app-pdf-translator"},
		Files:        map[string]string{"maclaw.app.json": appDefinition},
	}, HubDownloadOptions{HubURL: "https://hub.example", SkillID: "app-1", TargetDir: tmp})
	if err != nil {
		t.Fatalf("entryFromSkillHubDownload: %v", err)
	}
	if entry.Type != "instruction" {
		t.Fatalf("type = %q, want instruction", entry.Type)
	}
	if len(entry.Steps) != 0 {
		t.Fatalf("steps = %#v, want no executable steps", entry.Steps)
	}
	if _, err := os.Stat(filepath.Join(tmp, "maclaw.app.json")); err != nil {
		t.Fatalf("maclaw.app.json not written: %v", err)
	}
}

func TestEntryFromSkillHubDownload_UsesStandaloneAppMetadataWhenSkillFieldsMissing(t *testing.T) {
	appDefinition := base64.StdEncoding.EncodeToString([]byte(`{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"app":{"id":"pdf-translator","name":"PDF Translator","description":"Translate a PDF"}
	}`))
	entry, err := entryFromSkillHubDownload(skillHubDownloadResponse{
		skillHubItem: skillHubItem{Name: "skill-app-pdf-translator"},
		Files:        map[string]string{"maclaw.app.json": appDefinition},
	}, HubDownloadOptions{HubURL: "https://hub.example", SkillID: "app-1", SkipExtract: true})
	if err != nil {
		t.Fatalf("entryFromSkillHubDownload: %v", err)
	}
	if entry.Description != "Translate a PDF" {
		t.Fatalf("description = %q", entry.Description)
	}
	if len(entry.Triggers) != 1 || entry.Triggers[0] != "PDF Translator" {
		t.Fatalf("triggers = %#v", entry.Triggers)
	}
}

func TestEntryFromSkillHubDownload_KeepsExecutableAppWrapperExecutable(t *testing.T) {
	appDefinition := base64.StdEncoding.EncodeToString([]byte(`{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"app":{"id":"invoice-review","name":"Invoice Review"}
	}`))
	entry, err := entryFromSkillHubDownload(skillHubDownloadResponse{
		skillHubItem: skillHubItem{Name: "invoice-review-skill"},
		Steps: []skillHubDownloadStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo invoice"},
		}},
		Files: map[string]string{"maclaw.app.json": appDefinition},
	}, HubDownloadOptions{HubURL: "https://hub.example", SkillID: "invoice-app", SkipExtract: true})
	if err != nil {
		t.Fatalf("entryFromSkillHubDownload: %v", err)
	}
	if entry.Type == "instruction" {
		t.Fatalf("type = %q, executable app wrapper must not become instruction-only", entry.Type)
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "bash" {
		t.Fatalf("steps = %#v, want preserved executable step", entry.Steps)
	}
}

func TestParseSkillHubDownloadJSON_WithFiles(t *testing.T) {
	tmp := t.TempDir()
	md := base64.StdEncoding.EncodeToString([]byte("# Demo\n\nRun the demo."))
	payload := []byte(`{
		"id":"id-1",
		"name":"file-skill",
		"description":"from files",
		"files":{"SKILL.md":"` + md + `","scripts/run.sh":"` + base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\necho ok")) + `"}
	}`)
	entry, err := ParseSkillHubDownloadJSON(payload, HubDownloadOptions{
		HubURL:    "https://hub.example",
		SkillID:   "id-1",
		Source:    "skillhub",
		TargetDir: tmp,
	})
	if err != nil {
		t.Fatalf("ParseSkillHubDownloadJSON: %v", err)
	}
	if entry.Source != "skillhub" {
		t.Fatalf("source = %q", entry.Source)
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "craft_tool" {
		t.Fatalf("steps = %#v", entry.Steps)
	}
	if _, err := os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "scripts", "run.sh")); err != nil {
		t.Fatalf("script missing: %v", err)
	}
}

func TestPatternStagingValidator_SafeSkill(t *testing.T) {
	tmp := t.TempDir()
	// bash steps escalate community trust to high — install validator blocks review,
	// auto-promotion validator only blocks critical.
	yaml := []byte("name: safe-skill\ndescription: hello\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n")
	if err := os.WriteFile(filepath.Join(tmp, "skill.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewPatternStagingValidator().ScanSkillDir(tmp); err == nil {
		t.Fatal("install validator should block community bash (high)")
	}
	if err := NewAutoPromotionStagingValidator().ScanSkillDir(tmp); err != nil {
		t.Fatalf("auto-promotion validator should allow non-critical bash skill, got %v", err)
	}
}

func TestDefaultSandboxSkipsNonBash(t *testing.T) {
	sk := &corelib.NLSkillEntry{Name: "x"}
	ok, out, err := defaultBashSandboxStepRunner(context.Background(), sk, []corelib.NLSkillStep{
		{Action: "craft_tool"},
	}, nil, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("expected soft-pass for non-bash steps")
	}
	if out == "" {
		t.Fatal("expected skip reason in output")
	}
}
