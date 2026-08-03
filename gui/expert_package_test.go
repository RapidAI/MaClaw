package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	installed := map[string]bool{"skill-a": true}
	if expertPackageHasMissingSkills(installed, []expertPackageSkill{{Name: "skill-a"}}) {
		t.Fatal("an already-installed bundled skill should not require skill-import permission")
	}
	if !expertPackageHasMissingSkills(installed, []expertPackageSkill{{Name: "skill-a"}, {Name: "skill-b"}}) {
		t.Fatal("a missing bundled skill must require the skill-import path")
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
