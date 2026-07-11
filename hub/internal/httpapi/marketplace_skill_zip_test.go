package httpapi

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestPrepareSkillZipForHubCenterMarket_StripsRuntimeAndKeepsSkill(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "skill.zip")
	if err := writeTestSkillZip(t, src, map[string]string{
		"skill.yaml":                         "name: ppt-master\ndescription: demo\n",
		"scripts/run.py":                     "print('ok')\n",
		"node_modules/left-pad/index.js":     "module.exports=1\n",
		"node_modules/left-pad/package.json": "{}\n",
		".git/HEAD":                          "ref: refs/heads/main\n",
		".venv/lib/site.py":                  "# venv\n",
		"__pycache__/run.cpython.pyc":        "x",
	}); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := prepareSkillZipForHubCenterMarket(src)
	if err != nil {
		t.Fatalf("prepareSkillZipForHubCenterMarket: %v", err)
	}
	defer cleanup()
	if out == src {
		t.Fatal("expected a filtered temp zip when runtime entries are present")
	}

	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := make(map[string]bool, len(r.File))
	for _, f := range r.File {
		names[filepath.ToSlash(f.Name)] = true
		if enterpriseSkillPathHasRuntimeArtifact(f.Name) {
			t.Fatalf("filtered zip still contains runtime path %q", f.Name)
		}
	}
	if !names["skill.yaml"] || !names["scripts/run.py"] {
		t.Fatalf("expected skill files kept, got %v", names)
	}
	if len(r.File) != 2 {
		t.Fatalf("kept entries = %d, want 2; names=%v", len(r.File), names)
	}
}

func TestPrepareSkillZipForHubCenterMarket_NoOpWhenAlreadyClean(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clean.zip")
	if err := writeTestSkillZip(t, src, map[string]string{
		"skill.yaml":     "name: clean\ndescription: ok\n",
		"scripts/run.py": "print(1)\n",
	}); err != nil {
		t.Fatal(err)
	}
	out, cleanup, err := prepareSkillZipForHubCenterMarket(src)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer cleanup()
	if out != src {
		t.Fatalf("clean package should reuse original path, got %q want %q", out, src)
	}
}

func TestPrepareSkillZipForHubCenterMarket_AllowsManyNecessaryAssets(t *testing.T) {
	// Product rule: thousands of small necessary resources are OK when size is fine.
	// Old HubCenter cap was 1000 entries; 3496-style packs must pass after cleanup.
	const assetCount = 3500
	dir := t.TempDir()
	src := filepath.Join(dir, "assets.zip")
	if err := writeManyEntrySkillZip(t, src, assetCount); err != nil {
		t.Fatal(err)
	}

	out, cleanup, err := prepareSkillZipForHubCenterMarket(src)
	if err != nil {
		t.Fatalf("many necessary assets should be allowed when size is small: %v", err)
	}
	defer cleanup()
	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if len(r.File) < assetCount {
		t.Fatalf("entries=%d, want at least %d skill.yaml + assets", len(r.File), assetCount)
	}
	if len(r.File) > coreskill.MaxSkillMarketZipEntries {
		t.Fatalf("entries=%d exceed DoS ceiling", len(r.File))
	}
}

func TestPrepareSkillZipForHubCenterMarket_RejectsEntryDoSCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("creating MaxSkillMarketZipEntries+1 zip entries is slow")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "dos.zip")
	// One past the DoS ceiling (tiny files — size is fine, count is not).
	if err := writeManyEntrySkillZip(t, src, coreskill.MaxSkillMarketZipEntries+1); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := prepareSkillZipForHubCenterMarket(src)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("expected entry-count DoS rejection")
	}
	if !strings.Contains(err.Error(), "条目过多") {
		t.Fatalf("error = %v, want Chinese entry-limit message", err)
	}
}

func TestPrepareSkillZipForHubCenterMarket_StripsRuntimeSoUnderLimit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mixed.zip")
	files := map[string]string{
		"skill.yaml": "name: ppt\ndescription: ok\n",
	}
	// Real assets under the old 1000 cap, plus node_modules that used to push raw zip over 1000.
	for i := 0; i < 500; i++ {
		files[filepath.ToSlash(filepath.Join("templates", "t"+strconv.Itoa(i)+".svg"))] = "<svg/>"
	}
	for i := 0; i < 1200; i++ {
		files[filepath.ToSlash(filepath.Join("node_modules", "pkg", "f"+strconv.Itoa(i)+".js"))] = "1"
	}
	if err := writeTestSkillZip(t, src, files); err != nil {
		t.Fatal(err)
	}

	total, kept, skipped, err := countSkillZipEntries(src)
	if err != nil {
		t.Fatal(err)
	}
	if total <= 1000 {
		t.Fatalf("setup: total=%d, want raw zip > old 1000 cap", total)
	}
	if kept > coreskill.MaxSkillMarketZipEntries {
		t.Fatalf("setup: kept=%d still over DoS ceiling", kept)
	}
	if skipped < 1000 {
		t.Fatalf("setup: skipped=%d, expected many node_modules", skipped)
	}

	out, cleanup, err := prepareSkillZipForHubCenterMarket(src)
	if err != nil {
		t.Fatalf("prepare should succeed after stripping node_modules: %v", err)
	}
	defer cleanup()
	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if len(r.File) > coreskill.MaxSkillMarketZipEntries {
		t.Fatalf("filtered entries=%d still over limit", len(r.File))
	}
}

func TestHumanizeHubCenterPackageError(t *testing.T) {
	msg := humanizeHubCenterPackageError("unzip failed: too many files: 3496 (max 1000)")
	if !strings.Contains(msg, "node_modules") && !strings.Contains(msg, "防滥用") {
		t.Fatalf("humanized message missing guidance: %s", msg)
	}
	sizeMsg := humanizeHubCenterPackageError("total uncompressed size exceeds 524288000 bytes")
	if !strings.Contains(sizeMsg, "总体积") && !strings.Contains(sizeMsg, "精简") {
		t.Fatalf("size humanize missing guidance: %s", sizeMsg)
	}
	if humanizeHubCenterPackageError("") != "" {
		t.Fatal("empty stays empty")
	}
}

func TestSkillPackagePathHasRuntimeArtifact_ViaEnterpriseHelper(t *testing.T) {
	cases := map[string]bool{
		"skill.yaml":          false,
		"scripts/run.py":      false,
		"node_modules/x.js":   true,
		"foo/.venv/lib/x.py":  true,
		"__pycache__/a.pyc":   true,
		"upload_status.json":  true,
		"quality_status.json": true,
		"docs/readme.md":      false,
		".git/config":         true,
	}
	for path, want := range cases {
		if got := enterpriseSkillPathHasRuntimeArtifact(path); got != want {
			t.Errorf("enterpriseSkillPathHasRuntimeArtifact(%q)=%v want %v", path, got, want)
		}
	}
}

// writeManyEntrySkillZip writes skill.yaml plus n tiny asset files without
// holding the full map in memory (needed for multi-thousand entry tests).
func writeManyEntrySkillZip(t *testing.T, zipPath string, assetCount int) error {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("skill.yaml")
	if err != nil {
		_ = zw.Close()
		return err
	}
	if _, err := w.Write([]byte("name: many-assets\ndescription: necessary resources\n")); err != nil {
		_ = zw.Close()
		return err
	}
	for i := 0; i < assetCount; i++ {
		name := filepath.ToSlash(filepath.Join("assets", fmt.Sprintf("f%d.bin", i)))
		aw, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := aw.Write([]byte("x")); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}
