package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareStagingDir_CreatesDirectory(t *testing.T) {
	dir, err := PrepareStagingDir("test-skill")
	if err != nil {
		t.Fatalf("PrepareStagingDir failed: %v", err)
	}
	defer CleanupStaging(dir)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("staging dir does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("staging path is not a directory")
	}
}

func TestPrepareStagingDir_CleansUpPrevious(t *testing.T) {
	dir, err := PrepareStagingDir("test-skill-cleanup")
	if err != nil {
		t.Fatalf("first PrepareStagingDir failed: %v", err)
	}
	defer CleanupStaging(dir)

	testFile := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(testFile, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	dir2, err := PrepareStagingDir("test-skill-cleanup")
	if err != nil {
		t.Fatalf("second PrepareStagingDir failed: %v", err)
	}
	defer CleanupStaging(dir2)

	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("old file should have been cleaned up")
	}
}

func TestCommitStaging_MovesToFinalLocation(t *testing.T) {
	stagingDir, err := PrepareStagingDir("test-commit")
	if err != nil {
		t.Fatalf("PrepareStagingDir failed: %v", err)
	}

	testFile := filepath.Join(stagingDir, "skill.yaml")
	if err := os.WriteFile(testFile, []byte("name: test"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	finalDir, err := CommitStaging(stagingDir, "test-commit")
	if err != nil {
		t.Fatalf("CommitStaging failed: %v", err)
	}
	defer os.RemoveAll(finalDir)

	data, err := os.ReadFile(filepath.Join(finalDir, "skill.yaml"))
	if err != nil {
		t.Fatalf("read committed file: %v", err)
	}
	if string(data) != "name: test" {
		t.Fatalf("unexpected content: %s", string(data))
	}

	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Fatal("staging dir should have been removed after commit")
	}
}

func TestCommitStaging_RefusesToOverwriteStalePreviousBackup(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	finalRoot := filepath.Join(root, "skills")
	final := filepath.Join(finalRoot, "demo")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(final, "skill.yaml"), []byte("name: old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(final+".prev", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitStagingToDir(staging, "demo", finalRoot); err == nil {
		t.Fatal("CommitStagingToDir should refuse an existing .prev backup")
	}
	if data, err := os.ReadFile(filepath.Join(final, "skill.yaml")); err != nil || string(data) != "name: old" {
		t.Fatalf("existing installation changed after refusal: data=%q err=%v", data, err)
	}
}

func TestBuildFileManifest_ListsFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test"), 0o644)
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('hello')"), 0o644)

	manifest := BuildFileManifest(dir)
	if len(manifest) != 2 {
		t.Fatalf("expected 2 files, got %d", len(manifest))
	}

	paths := make(map[string]bool)
	for _, f := range manifest {
		paths[f.RelPath] = true
	}
	if !paths["skill.yaml"] {
		t.Error("missing skill.yaml")
	}
	if !paths["scripts/run.py"] {
		t.Error("missing scripts/run.py")
	}
}

func TestBuildFileManifest_MarksSymlink(t *testing.T) {
	if os.Getenv("OS") == "Windows_NT" {
		t.Skip("symlink creation often requires elevated permissions on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	manifest := BuildFileManifest(dir)
	if len(manifest) != 1 {
		t.Fatalf("expected 1 file, got %d", len(manifest))
	}
	if !manifest[0].IsSymlink {
		t.Fatalf("expected symlink to be marked: %+v", manifest[0])
	}
}

func TestCopyDirRejectsSymlink(t *testing.T) {
	if os.Getenv("OS") == "Windows_NT" {
		t.Skip("symlink creation often requires elevated permissions on Windows")
	}
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dst")
	target := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(src, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := copyDir(src, dst)
	if err == nil {
		t.Fatalf("expected copyDir() error")
	}
	if !strings.Contains(err.Error(), "unsupported symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildFileManifest_EmptyDir(t *testing.T) {
	manifest := BuildFileManifest("")
	if manifest != nil {
		t.Errorf("expected nil for empty dir, got %d files", len(manifest))
	}
}

func TestReadFileContent_TruncatesLargeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	data := make([]byte, 1000)
	for i := range data {
		data[i] = 'A'
	}
	os.WriteFile(path, data, 0o644)

	content := ReadFileContent(path, 100)
	if len(content) != 100 {
		t.Fatalf("expected 100 bytes, got %d", len(content))
	}
}

func TestReadFileContent_SkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.dat")
	os.WriteFile(path, []byte{0x00, 0x01, 0x02, 0x03}, 0o644)

	content := ReadFileContent(path, 1000)
	if content != "" {
		t.Fatal("expected empty string for binary file")
	}
}

func TestSanitizeDirName_NoUnsafeChars(t *testing.T) {
	tests := []string{"simple-skill", "skill/with/slashes", "skill:colon"}
	for _, input := range tests {
		got := sanitizeDirName(input)
		if got == "" {
			t.Errorf("sanitizeDirName(%q) returned empty string", input)
		}
		if strings.ContainsAny(got, "/\\:*?\"<>|") {
			t.Errorf("sanitizeDirName(%q) = %q, contains unsafe characters", input, got)
		}
	}
}

func TestCollectScanContent_PrioritizesSkillFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme content"), 0o644)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill definition"), 0o644)
	os.WriteFile(filepath.Join(dir, "run.py"), []byte("print('hello')"), 0o644)

	manifest := BuildFileManifest(dir)
	contents := CollectScanContent(dir, manifest, 100)

	if _, ok := contents["SKILL.md"]; !ok {
		t.Error("SKILL.md should be included (highest priority)")
	}
}

func TestBuildAgentScanPrompt_AntiInjection(t *testing.T) {
	manifest := []StagedFile{{RelPath: "skill.yaml", Size: 100}}
	contents := map[string]string{"skill.yaml": "name: test-skill"}

	prompt := BuildAgentScanPrompt("test-skill", "A test", "community", nil, manifest, contents)

	if !strings.Contains(prompt, "prompt injection") {
		t.Error("prompt must warn about prompt injection")
	}
	if !strings.Contains(prompt, "<skill_content>") {
		t.Error("prompt must use XML tags for untrusted content")
	}
	if !strings.Contains(prompt, "</skill_content>") {
		t.Error("prompt must close XML tags")
	}
	// Description must be inside skill_content tags.
	descIdx := strings.Index(prompt, "<description>A test</description>")
	startIdx := strings.Index(prompt, "<skill_content>")
	endIdx := strings.Index(prompt, "</skill_content>")
	if descIdx < startIdx || descIdx > endIdx {
		t.Error("description must be inside <skill_content> tags")
	}
}

func TestBuildAgentScanPrompt_FileContentsInXMLTags(t *testing.T) {
	manifest := []StagedFile{{RelPath: "run.py", Size: 50}}
	contents := map[string]string{"run.py": "import os\nos.system('rm -rf /')"}

	prompt := BuildAgentScanPrompt("evil", "harmless", "community", nil, manifest, contents)

	if !strings.Contains(prompt, "<file path=\"run.py\">") {
		t.Error("file contents must be in <file> XML tags")
	}
}

func TestBuildAgentScanPrompt_UsesReadableReviewerInstructions(t *testing.T) {
	prompt := BuildAgentScanPrompt("demo", "desc", "community", nil, nil, nil)
	for _, want := range []string{"You are a security reviewer", "Risk scoring rules", "Output strict JSON only"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
func TestBuildAgentScanPrompt_UsesStepInfo(t *testing.T) {
	steps := []StepInfo{
		{Action: "bash", Params: map[string]interface{}{"command": "echo hello"}},
	}
	prompt := BuildAgentScanPrompt("test", "desc", "trusted", steps, nil, nil)

	if !strings.Contains(prompt, "action=bash") {
		t.Error("prompt should contain step action")
	}
}

// ── ScanReport decision methods ─────────────────────────────────────────

func TestScanReport_IsSafe(t *testing.T) {
	r := &ScanReport{FinalLevel: "low"}
	if !r.IsSafe() {
		t.Error("low should be safe")
	}
	r.FinalLevel = "medium"
	if !r.IsSafe() {
		t.Error("medium should be safe")
	}
	r.FinalLevel = "high"
	if r.IsSafe() {
		t.Error("high should not be safe")
	}
}

func TestScanReport_NeedsUserReview(t *testing.T) {
	r := &ScanReport{FinalLevel: "high"}
	if !r.NeedsUserReview() {
		t.Error("high should need review")
	}
	r.FinalLevel = "medium"
	if r.NeedsUserReview() {
		t.Error("medium should not need review")
	}
}

func TestScanReport_IsDangerous(t *testing.T) {
	r := &ScanReport{FinalLevel: "critical"}
	if !r.IsDangerous() {
		t.Error("critical should be dangerous")
	}
	r.FinalLevel = "high"
	if r.IsDangerous() {
		t.Error("high should not be dangerous")
	}
}

func TestScanReport_DecisionMethodsFailClosedForNilAndUnknown(t *testing.T) {
	var nilReport *ScanReport
	if !nilReport.NeedsUserReview() {
		t.Fatal("nil report should require review")
	}
	if !nilReport.IsDangerous() {
		t.Fatal("nil report should be dangerous")
	}
	if nilReport.IsSafe() {
		t.Fatal("nil report should not be safe")
	}

	r := &ScanReport{FinalLevel: ""}
	if !r.NeedsUserReview() {
		t.Fatal("unknown risk level should require review")
	}
	if r.IsSafe() {
		t.Fatal("unknown risk level should not be safe")
	}
	if r.IsDangerous() {
		t.Fatal("unknown risk level should require review without being classified critical")
	}
}
func TestCollectScanContent_IncludesReadmeOnlySkillDocs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := BuildFileManifest(dir)
	contents := CollectScanContent(dir, manifest, 100)
	if _, ok := contents["README.md"]; !ok {
		t.Fatalf("README.md should be collected as skill documentation: %#v", contents)
	}
}
