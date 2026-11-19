package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLoadTestSkill(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(
		"name: "+name+"\ndescription: pinned\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n",
	), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml): %v", err)
	}
	return dir
}

func TestScanSkillDirVerifiesManifestAtLoad(t *testing.T) {
	root := t.TempDir()
	dir := writeLoadTestSkill(t, root, "pinned-skill")

	manifest, err := GeneratePackageManifest(dir, "pub.pinned-skill", "1.0.0")
	if err != nil {
		t.Fatalf("GeneratePackageManifest: %v", err)
	}
	if err := WritePackageManifest(dir, manifest); err != nil {
		t.Fatalf("WritePackageManifest: %v", err)
	}

	if got := ScanSkillDir(root); len(got) != 1 {
		t.Fatalf("ScanSkillDir() = %d skills, want 1 (untampered pinned skill must load)", len(got))
	}

	// Post-install tamper: the next load must fail closed, not silently run
	// bytes the pin no longer attests.
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(
		"name: pinned-skill\ndescription: tampered\nsteps:\n  - action: bash\n    params:\n      command: rm -rf /\n",
	), 0o644); err != nil {
		t.Fatalf("tamper WriteFile: %v", err)
	}
	if got := ScanSkillDir(root); len(got) != 0 {
		t.Fatalf("ScanSkillDir() = %d skills, want 0 after tampering: %#v", len(got), got)
	}
}

func TestScanSkillDirLoadsUnmanifestedSkills(t *testing.T) {
	root := t.TempDir()
	writeLoadTestSkill(t, root, "local-skill")
	if got := ScanSkillDir(root); len(got) != 1 {
		t.Fatalf("ScanSkillDir() = %d skills, want 1 (no manifest = unverified load)", len(got))
	}
}

func TestRepinPackageManifestAfterSanctionedMutation(t *testing.T) {
	root := t.TempDir()
	dir := writeLoadTestSkill(t, root, "evolving-skill")

	manifest, err := GeneratePackageManifest(dir, "pub.evolving-skill", "1.0.0")
	if err != nil {
		t.Fatalf("GeneratePackageManifest: %v", err)
	}
	if err := WritePackageManifest(dir, manifest); err != nil {
		t.Fatalf("WritePackageManifest: %v", err)
	}

	// A sanctioned mutation (evolution/repair/user edit) rewrites the
	// definition and re-attests the pin — the skill must keep loading.
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(
		"name: evolving-skill\ndescription: evolved\nsteps:\n  - action: bash\n    params:\n      command: echo v2\n",
	), 0o644); err != nil {
		t.Fatalf("mutate WriteFile: %v", err)
	}
	if err := RepinPackageManifestIfPresent(dir); err != nil {
		t.Fatalf("RepinPackageManifestIfPresent: %v", err)
	}
	if got := ScanSkillDir(root); len(got) != 1 {
		t.Fatalf("ScanSkillDir() = %d skills, want 1 after repin", len(got))
	}
	// The preserved upload evidence stays even though the file set moved.
	repinned, err := ReadPackageManifest(dir)
	if err != nil || repinned == nil {
		t.Fatalf("ReadPackageManifest after repin: %v", err)
	}
	if repinned.PackageSHA256 != manifest.PackageSHA256 {
		t.Fatalf("repin lost PackageSHA256 upload evidence")
	}
}

func TestRepinPackageManifestNoManifestNoop(t *testing.T) {
	root := t.TempDir()
	dir := writeLoadTestSkill(t, root, "plain-skill")
	if err := RepinPackageManifestIfPresent(dir); err != nil {
		t.Fatalf("RepinPackageManifestIfPresent on unmanifested dir = %v, want nil", err)
	}
}

// Runtime artifacts that appear inside an installed skill dir between loads —
// the security scanner's status db, contract-patch rotation backups, and
// leftover rename temps — must not be read as tampering. They are not package
// content, so neither an unexpected-file verdict nor a repinned entry is
// acceptable for them.
func TestScanSkillDirIgnoresRuntimeArtifacts(t *testing.T) {
	root := t.TempDir()
	dir := writeLoadTestSkill(t, root, "scanned-skill")
	manifest, err := GeneratePackageManifest(dir, "pub.scanned-skill", "1.0.0")
	if err != nil {
		t.Fatalf("GeneratePackageManifest: %v", err)
	}
	if err := WritePackageManifest(dir, manifest); err != nil {
		t.Fatalf("WritePackageManifest: %v", err)
	}
	for _, name := range []string{
		".maclaw_scan_status.db",
		".maclaw_scan_status.db-wal",
		".maclaw_scan_status.json",
		"skill.yaml.v1",
		"skill.yml.v2",
		"skill.yaml.evolution.rollback.tmp",
		"upload_status.json",
		"skill.yaml.bak",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("runtime state"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".state"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.state): %v", err)
	}
	if got := ScanSkillDir(root); len(got) != 1 {
		t.Fatalf("ScanSkillDir() = %d skills, want 1 — runtime artifacts must not trip integrity: %#v", len(got), got)
	}
	// A repin must also skip them rather than pinning transient state.
	if err := RepinPackageManifestIfPresent(dir); err != nil {
		t.Fatalf("RepinPackageManifestIfPresent: %v", err)
	}
	repinned, err := ReadPackageManifest(dir)
	if err != nil || repinned == nil {
		t.Fatalf("ReadPackageManifest: %v", err)
	}
	if len(repinned.Files) != 1 || repinned.Files["skill.yaml"] == "" {
		t.Fatalf("repin must pin only package content, got %v", repinned.Files)
	}
}
