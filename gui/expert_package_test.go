package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExportableExpertDefinitionClearsLocalIdentity(t *testing.T) {
	def := testExpert("expert-local", "2026-08-01T00:00:00Z")
	def.Builtin = true
	def.Tools = []string{"tool-a", "tool-a"}
	def.Skills = []string{"skill-a", "skill-a"}

	got := exportableExpertDefinition(def)
	if got.ID != "" || got.CreatedAt != "" || got.UpdatedAt != "" || got.Builtin {
		t.Fatalf("export should clear local identity, got %+v", got)
	}
	if len(got.Tools) != 1 || got.Tools[0] != "tool-a" || len(got.Skills) != 1 || got.Skills[0] != "skill-a" {
		t.Fatalf("export should normalize allow-lists, got %+v", got)
	}
}

func TestExpertPackageIdentityIsStableAndDistinct(t *testing.T) {
	expert := testExpert("expert-origin", "2026-08-01T00:00:00Z")
	first := expertPackageIdentity(expert)
	second := expertPackageIdentity(expert)
	if first != second || first == expert.ID || len(first) == 0 {
		t.Fatalf("package identity must be stable and distinct: first=%q second=%q", first, second)
	}
	if !strings.HasPrefix(first, expertPackageIDPrefix) || !expertIDPattern.MatchString(first) {
		t.Fatalf("invalid package identity %q", first)
	}
}

func TestExpertPackageIdentitySurvivesReExport(t *testing.T) {
	origin := testExpert("expert-origin", "2026-08-01T00:00:00Z")
	packageID := expertPackageIdentity(origin)
	imported := testExpert(packageID, "2026-08-01T00:00:00Z")
	if got := expertPackageIdentity(imported); got != packageID {
		t.Fatalf("re-export must preserve package identity: got %q want %q", got, packageID)
	}
}

func TestExpertPackageDefinitionsEqualIgnoresLocalIdentity(t *testing.T) {
	local := testExpert("pkgexp-local", "2026-08-01T00:00:00Z")
	incoming := local
	incoming.ID = ""
	incoming.CreatedAt = ""
	incoming.UpdatedAt = ""
	if !expertPackageDefinitionsEqual(local, incoming) {
		t.Fatal("same portable expert content should compare equal despite local identity")
	}
	incoming.SystemPrompt = "changed prompt"
	if expertPackageDefinitionsEqual(local, incoming) {
		t.Fatal("changed package content must not be treated as an already-imported duplicate")
	}
}

func TestExpertPackageDependenciesInstalled(t *testing.T) {
	installed := map[string]bool{"skill-a": true, "skill-b": true}
	if !expertPackageDependenciesInstalled(installed, []string{"skill-a", "skill-b"}) {
		t.Fatal("all installed expert dependencies should allow an idempotent import")
	}
	if expertPackageDependenciesInstalled(installed, []string{"skill-a", "skill-missing"}) {
		t.Fatal("a missing expert dependency must force package import to continue")
	}
	if !expertPackageDependenciesInstalled(installed, nil) {
		t.Fatal("an expert without skill dependencies should be satisfied")
	}
}

func TestExpertPackageHasMissingSkills(t *testing.T) {
	installed := map[string]corelib.NLSkillEntry{"skill-a": {Name: "skill-a"}}
	if expertPackageHasMissingSkills(installed, []expertPackageSkill{{Name: "skill-a"}}) {
		t.Fatal("an already-installed bundled skill should not require skill-import permission")
	}
	if !expertPackageHasMissingSkills(installed, []expertPackageSkill{{Name: "skill-a"}, {Name: "skill-b"}}) {
		t.Fatal("a missing bundled skill must require the skill-import path")
	}
}

func TestExpertPackageRequiredSkillsIgnoresUnreferencedBundledSkills(t *testing.T) {
	installed := map[string]corelib.NLSkillEntry{
		"required": {Name: "required"},
	}
	selected, err := (&App{}).expertPackageRequiredSkills(
		[]string{"required"},
		installed,
		[]expertPackageSkill{
			{Name: "required", Archive: "skills/required.zip"},
			{Name: "unrelated", Archive: "skills/unrelated.zip"},
		},
		map[string][]byte{},
	)
	if err != nil {
		t.Fatalf("resolve required skills: %v", err)
	}
	if len(selected) != 0 {
		t.Fatalf("unreferenced bundled skills must not be selected for installation: %+v", selected)
	}
}

func TestExpertPackageRequiredSkillsInstallsMissingChildOfInstalledPipeline(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	installed := map[string]corelib.NLSkillEntry{
		"parent": {
			Name:     "parent",
			Pipeline: []corelib.SkillPipelineStep{{Skill: "child"}},
		},
	}
	child := expertPackageTestSkillArchive(t, "skill.yaml", []byte("name: child\ndescription: Child dependency\nsteps: []\n"))

	selected, err := app.expertPackageRequiredSkills(
		[]string{"parent"},
		installed,
		[]expertPackageSkill{
			{Name: "child", Archive: "skills/child.zip"},
			{Name: "unrelated", Archive: "skills/unrelated.zip"},
		},
		map[string][]byte{"skills/child.zip": child},
	)
	if err != nil {
		t.Fatalf("resolve installed pipeline dependency: %v", err)
	}
	if len(selected) != 1 || selected[0].Name != "child" {
		t.Fatalf("selected skills = %+v, want only missing child", selected)
	}
}

func TestExpertPackageRequiredSkillsTraversesBundledPipelineDependencies(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	parent := expertPackageTestSkillArchive(t, "skill.yaml", []byte("name: parent\ndescription: Parent pipeline\npipeline:\n  - skill: child\n"))
	child := expertPackageTestSkillArchive(t, "skill.yaml", []byte("name: child\ndescription: Child dependency\nsteps: []\n"))

	selected, err := app.expertPackageRequiredSkills(
		[]string{"parent"},
		nil,
		[]expertPackageSkill{
			{Name: "parent", Archive: "skills/parent.zip"},
			{Name: "child", Archive: "skills/child.zip"},
			{Name: "unrelated", Archive: "skills/unrelated.zip"},
		},
		map[string][]byte{
			"skills/parent.zip": parent,
			"skills/child.zip":  child,
		},
	)
	if err != nil {
		t.Fatalf("resolve bundled pipeline dependency: %v", err)
	}
	if len(selected) != 2 || selected[0].Name != "child" || selected[1].Name != "parent" {
		t.Fatalf("selected skills = %+v, want parent dependency closure only", selected)
	}
}

func TestExpertPackageRequiredSkillsRejectsMissingPipelineChildBeforeImport(t *testing.T) {
	installed := map[string]corelib.NLSkillEntry{
		"parent": {
			Name:     "parent",
			Pipeline: []corelib.SkillPipelineStep{{Skill: "child"}},
		},
	}

	_, err := (&App{}).expertPackageRequiredSkills(
		[]string{"parent"},
		installed,
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), `skill "child"`) {
		t.Fatalf("resolve missing pipeline dependency error = %v, want child dependency error", err)
	}
}

func TestExpertPackageRequiredSkillsBoundsInstalledPipelineClosure(t *testing.T) {
	installed := make(map[string]corelib.NLSkillEntry, maxExpertPackageSkills+1)
	for i := 0; i <= maxExpertPackageSkills; i++ {
		name := fmt.Sprintf("pipeline-%03d", i)
		entry := corelib.NLSkillEntry{Name: name}
		if i < maxExpertPackageSkills {
			entry.Pipeline = []corelib.SkillPipelineStep{{Skill: fmt.Sprintf("pipeline-%03d", i+1)}}
		}
		installed[name] = entry
	}

	_, err := (&App{}).expertPackageRequiredSkills(
		[]string{"pipeline-000"},
		installed,
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "too many dependent skills") {
		t.Fatalf("resolve oversized installed pipeline closure error = %v, want size limit", err)
	}
}

func TestReadExpertPackageAssignsLegacyPackageIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-expert.zip")
	manifest := expertPackageManifest{
		Format:  expertPackageFormat,
		Version: expertPackageVersion,
		Expert:  exportableExpertDefinition(testExpert("expert-local", "2026-08-01T00:00:00Z")),
	}
	if err := (&App{}).writeExpertPackageAtomic(path, manifest, nil); err != nil {
		t.Fatalf("write legacy package: %v", err)
	}
	got, _, err := readExpertPackage(path)
	if err != nil {
		t.Fatalf("read legacy package: %v", err)
	}
	if !strings.HasPrefix(got.ExpertPackageID, expertPackageIDPrefix) {
		t.Fatalf("legacy package was not assigned an identity: %+v", got)
	}
}

func TestWriteAndReadExpertPackage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "expert.zip")
	manifest := expertPackageManifest{
		Format:  expertPackageFormat,
		Version: expertPackageVersion,
		Expert:  exportableExpertDefinition(testExpert("expert-local", "2026-08-01T00:00:00Z")),
	}
	if err := (&App{}).writeExpertPackageAtomic(path, manifest, nil); err != nil {
		t.Fatalf("write package: %v", err)
	}
	got, archives, err := readExpertPackage(path)
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	if got.Format != expertPackageFormat || got.Version != expertPackageVersion || got.Expert.Name == "" {
		t.Fatalf("unexpected manifest: %+v", got)
	}
	if len(archives) != 0 {
		t.Fatalf("unexpected archives: %v", archives)
	}
}

func TestReadExpertPackageRejectsUnexpectedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	manifest, err := json.Marshal(expertPackageManifest{
		Format:  expertPackageFormat,
		Version: expertPackageVersion,
		Expert:  exportableExpertDefinition(testExpert("expert-local", "2026-08-01T00:00:00Z")),
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create(expertPackageManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(manifest); err != nil {
		t.Fatal(err)
	}
	w, err = zw.Create("unexpected.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("not allowed")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readExpertPackage(path); err == nil {
		t.Fatal("expected unexpected package file to be rejected")
	}
}

func TestReadExpertPackageRejectsSkillArchiveReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate-archive.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	manifest, err := json.Marshal(expertPackageManifest{
		Format:  expertPackageFormat,
		Version: expertPackageVersion,
		Expert:  exportableExpertDefinition(testExpert("expert-local", "2026-08-01T00:00:00Z")),
		Skills: []expertPackageSkill{
			{Name: "one", Archive: "skills/shared.zip"},
			{Name: "two", Archive: "skills/shared.zip"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create(expertPackageManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(manifest); err != nil {
		t.Fatal(err)
	}
	w, err = zw.Create("skills/shared.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("not evaluated: manifest must fail first")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readExpertPackage(path); err == nil {
		t.Fatal("expected duplicate skill archive to be rejected")
	}
}

func TestExpertPackageNestedArchiveExpandedSizeRejectsInvalidZip(t *testing.T) {
	if _, err := expertPackageNestedArchiveExpandedSize([]byte("not a zip")); err == nil {
		t.Fatal("expected malformed nested skill archive to be rejected")
	}
}

func TestExpertPackageNestedArchiveExpandedSizeCountsContents(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("skill.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("name: sample\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := expertPackageNestedArchiveExpandedSize(buf.Bytes())
	if err != nil {
		t.Fatalf("size nested archive: %v", err)
	}
	if want := uint64(len("name: sample\n")); got != want {
		t.Fatalf("expanded bytes = %d, want %d", got, want)
	}
}

func TestValidateExpertPackageNestedArchivesValidatesEveryArchive(t *testing.T) {
	archive := expertPackageTestSkillArchive(t, "sample", []byte("name: sample\n"))
	items := []expertPackageSkill{{Name: "sample", Archive: "skills/sample.zip"}}
	archives := map[string][]byte{"skills/sample.zip": archive}
	if err := validateExpertPackageNestedArchives(items, archives); err != nil {
		t.Fatalf("validate nested archives: %v", err)
	}
}

func expertPackageTestSkillArchive(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
