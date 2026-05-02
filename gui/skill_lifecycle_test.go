package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func writeLifecycleTestSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data := []byte("name: " + name + "\ndescription: A portable skill used by lifecycle tests.\ntriggers:\n  - lifecycle-test\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
}

func TestSkillDirHashIgnoresRuntimeStatusFiles(t *testing.T) {
	dir := t.TempDir()
	writeLifecycleTestSkill(t, dir, "hash-skill")
	baseHash := skillDirHash(dir)

	ignored := map[string]string{
		"upload_status.json":          `{"submission_id":"sub"}`,
		"quality_status.json":         `{"score":100}`,
		"skill_package_manifest.json": `{"files":[]}`,
		"skill.yaml.bak":              "old backup",
	}
	for name, content := range ignored {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
	if got := skillDirHash(dir); got != baseHash {
		t.Fatalf("runtime/status files changed hash: got %s want %s", got, baseHash)
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("real content"), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v", err)
	}
	if got := skillDirHash(dir); got == baseHash {
		t.Fatalf("real package content did not change hash")
	}
}

func TestEvaluateSkillPackageCompletenessRejectsJSONDefinition(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"name":"json-package-skill","steps":[{"run":"echo ok"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.json) error = %v", err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "json-package-skill",
		SkillDir: dir,
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo ok"}}},
	}
	summary, _, fatal, reasons := evaluateSkillPackageCompleteness(dir, entry)
	if summary.HasSkillDefinition || summary.HasSkillYAML {
		t.Fatalf("summary = %+v, want no accepted skill definition", summary)
	}
	if !fatal || len(reasons) == 0 || reasons[0] != "package lacks skill definition or skill documentation" {
		t.Fatalf("unexpected completeness result: fatal=%v reasons=%v", fatal, reasons)
	}
}

func TestEvaluateSkillPackageCompletenessAcceptsReadmeDocumentation(t *testing.T) {
	dir := t.TempDir()
	writeLifecycleTestSkill(t, dir, "readme-doc-skill")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# README docs\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v", err)
	}

	entry := &corelib.NLSkillEntry{
		Name:     "readme-doc-skill",
		SkillDir: dir,
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo ok"}}},
	}
	summary, penalty, fatal, reasons := evaluateSkillPackageCompleteness(dir, entry)
	if !summary.HasSkillYAML || !summary.HasSkillMD {
		t.Fatalf("summary = %+v, want skill yaml and docs", summary)
	}
	if fatal || penalty != 0 || len(reasons) != 0 {
		t.Fatalf("unexpected completeness result: penalty=%d fatal=%v reasons=%v", penalty, fatal, reasons)
	}
}

func TestWriteSkillPackageManifestExcludesRuntimeFiles(t *testing.T) {
	dir := t.TempDir()
	writeLifecycleTestSkill(t, dir, "manifest-skill")
	for _, name := range []string{"upload_status.json", "quality_status.json", "skill_package_manifest.json", "skill.yaml.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("runtime"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	entry := &corelib.NLSkillEntry{Name: "manifest-skill", SkillDir: dir, SuccessCount: 1}
	quality := skillQualityReport{Score: 100, MarketReady: true}
	if err := writeSkillPackageManifest(dir, entry, quality, "package", true); err != nil {
		t.Fatalf("writeSkillPackageManifest() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "skill_package_manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	var manifest skillPackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Unmarshal(manifest) error = %v", err)
	}
	if manifest.SkillName != "manifest-skill" {
		t.Fatalf("SkillName = %q", manifest.SkillName)
	}
	if manifest.Quality.VerificationStatus != "verified_success" {
		t.Fatalf("VerificationStatus = %q", manifest.Quality.VerificationStatus)
	}
	if manifest.Quality.MinMarketScore != skillMarketReadyMinScore {
		t.Fatalf("MinMarketScore = %d, want %d", manifest.Quality.MinMarketScore, skillMarketReadyMinScore)
	}
	seen := map[string]bool{}
	for _, f := range manifest.Files {
		seen[f.Path] = true
		if f.SHA256 == "" || f.Size <= 0 {
			t.Fatalf("manifest file lacks digest/size: %+v", f)
		}
	}
	if !seen["skill.yaml"] {
		t.Fatalf("manifest files = %+v, want skill.yaml", manifest.Files)
	}
	for _, ignored := range []string{"upload_status.json", "quality_status.json", "skill_package_manifest.json", "skill.yaml.bak"} {
		if seen[ignored] {
			t.Fatalf("manifest included runtime file %s: %+v", ignored, manifest.Files)
		}
	}
}

func TestSkillLifecycleRetryBlockedMovesItemsPending(t *testing.T) {
	queuePath := filepath.Join(t.TempDir(), "queue.json")
	m := &SkillLifecycleManager{queuePath: queuePath}
	m.recordBlocked("blocked-skill", "", "test", "hash", true, "needs verification", 42)

	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusBlocked {
		t.Fatalf("initial queue = %+v", items)
	}

	if err := m.RetryBlocked("blocked-skill"); err != nil {
		t.Fatalf("RetryBlocked() error = %v", err)
	}
	items, err = m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() after retry error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusPending || items[0].LastError != "" {
		t.Fatalf("queue after retry = %+v", items)
	}
}

func TestSkillLifecycleEnqueueBlocksWithoutRuntimeProof(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	m := NewSkillLifecycleManager(app)
	dir := filepath.Join(tempHome, "needs-proof")
	writeLifecycleTestSkill(t, dir, "needs-proof")

	item, err := m.EnqueueUpload(nil, "needs-proof", dir, "test", true, false)
	if err == nil {
		t.Fatal("EnqueueUpload() expected runtime proof error")
	}
	if item == nil || item.Status != skillUploadStatusBlocked {
		t.Fatalf("blocked item = %+v err=%v", item, err)
	}
	statusData, readErr := os.ReadFile(filepath.Join(dir, "quality_status.json"))
	if readErr != nil {
		t.Fatalf("ReadFile(quality_status.json) error = %v", readErr)
	}
	var status persistedSkillQualityStatus
	if err := json.Unmarshal(statusData, &status); err != nil {
		t.Fatalf("Unmarshal(quality_status) error = %v", err)
	}
	if status.VerificationStatus != "needs_runtime_proof" || status.MarketReady || status.MinMarketScore != skillMarketReadyMinScore {
		t.Fatalf("quality status = %+v", status)
	}
}

func TestAuditInstalledSkillQualityWritesStatus(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "audit-skill")
	writeLifecycleTestSkill(t, dir, "audit-skill")

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:         "audit-skill",
		SkillDir:     dir,
		Source:       "file",
		UsageCount:   1,
		SuccessCount: 1,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	statuses, err := app.AuditInstalledSkillQuality(false)
	if err != nil {
		t.Fatalf("AuditInstalledSkillQuality() error = %v", err)
	}
	var found *SkillQualityStatus
	for i := range statuses {
		if statuses[i].SkillName == "audit-skill" {
			found = &statuses[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("audit status for skill not found in %+v", statuses)
	}
	if !found.MarketReady || found.VerificationStatus != "verified_success" || found.LocalHash == "" {
		t.Fatalf("audit status = %+v", *found)
	}

	statusData, err := os.ReadFile(filepath.Join(dir, "quality_status.json"))
	if err != nil {
		t.Fatalf("ReadFile(quality_status.json) error = %v", err)
	}
	var persisted persistedSkillQualityStatus
	if err := json.Unmarshal(statusData, &persisted); err != nil {
		t.Fatalf("Unmarshal(quality_status) error = %v", err)
	}
	if persisted.SkillName != "audit-skill" || persisted.Stage != "audit" || !persisted.MarketReady {
		t.Fatalf("persisted status = %+v", persisted)
	}
}

func TestSkillLifecycleStaleUploadingItemIsRetried(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	m := &SkillLifecycleManager{queuePath: filepath.Join(t.TempDir(), "queue.json")}
	stale := SkillUploadQueueItem{
		ID:        "stale-upload",
		SkillName: "stale-skill",
		Status:    skillUploadStatusUploading,
		UpdatedAt: now.Add(-skillUploadLeaseTimeout - time.Minute).Format(time.RFC3339),
		CreatedAt: now.Add(-time.Hour).Format(time.RFC3339),
	}
	if err := m.saveQueueLocked(skillUploadQueueFile{Items: []SkillUploadQueueItem{stale}}); err != nil {
		t.Fatalf("saveQueueLocked() error = %v", err)
	}

	item, ok, err := m.nextPendingItem(now)
	if err != nil {
		t.Fatalf("nextPendingItem() error = %v", err)
	}
	if !ok || item.ID != stale.ID || item.Status != skillUploadStatusUploading {
		t.Fatalf("nextPendingItem() = %+v ok=%v", item, ok)
	}
	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusUploading || items[0].UpdatedAt != now.Format(time.RFC3339) {
		t.Fatalf("queue after stale retry = %+v", items)
	}
}

func TestSkillLifecycleFreshUploadingItemIsLeased(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	m := &SkillLifecycleManager{queuePath: filepath.Join(t.TempDir(), "queue.json")}
	fresh := SkillUploadQueueItem{
		ID:        "fresh-upload",
		SkillName: "fresh-skill",
		Status:    skillUploadStatusUploading,
		UpdatedAt: now.Add(-time.Minute).Format(time.RFC3339),
		CreatedAt: now.Add(-time.Minute).Format(time.RFC3339),
	}
	if err := m.saveQueueLocked(skillUploadQueueFile{Items: []SkillUploadQueueItem{fresh}}); err != nil {
		t.Fatalf("saveQueueLocked() error = %v", err)
	}

	item, ok, err := m.nextPendingItem(now)
	if err != nil {
		t.Fatalf("nextPendingItem() error = %v", err)
	}
	if ok {
		t.Fatalf("fresh lease should not be retried, got %+v", item)
	}
}

func TestSkillQualityBlocksMissingReferencedScript(t *testing.T) {
	dir := t.TempDir()
	data := []byte("name: missing-script\ndescription: A portable skill whose package should include referenced scripts.\ntriggers:\n  - missing-script\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: python scripts/missing.py\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	entry, err := loadImportedSkillEntry(dir)
	if err != nil {
		t.Fatalf("loadImportedSkillEntry() error = %v", err)
	}
	report, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	quality := evaluateSkillQualityForDir(entry, report, false, dir)
	if quality.MarketReady {
		t.Fatalf("quality should block missing referenced script: %+v", quality)
	}
	if len(quality.Package.ReferencedMissing) != 1 || quality.Package.ReferencedMissing[0] != "scripts/missing.py" {
		t.Fatalf("ReferencedMissing = %+v", quality.Package.ReferencedMissing)
	}
}

func TestSkillQualityAcceptsPackagedReferencedScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	data := []byte("name: packaged-script\ndescription: A portable skill whose package includes its referenced script.\ntriggers:\n  - packaged-script\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: python scripts/run.py\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Packaged script\n\nRuns the bundled script.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	entry, err := loadImportedSkillEntry(dir)
	if err != nil {
		t.Fatalf("loadImportedSkillEntry() error = %v", err)
	}
	report, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	quality := evaluateSkillQualityForDir(entry, report, false, dir)
	if !quality.MarketReady {
		t.Fatalf("quality should accept packaged script: %+v", quality)
	}
	if !quality.Package.HasSkillYAML || !quality.Package.HasSkillMD || len(quality.Package.ReferencedMissing) != 0 {
		t.Fatalf("Package summary = %+v", quality.Package)
	}
}

func TestSkillQualityAcceptsFlagStyleReferencedScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	data := []byte("name: flag-script\ndescription: A portable skill that passes a bundled script through a CLI flag.\ntriggers:\n  - flag-script\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: python launcher.py --script=scripts/run.py\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "launcher.py"), []byte("print('launch')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(launcher.py) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Flag script\n\nRuns a bundled script selected by flag.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	entry, err := loadImportedSkillEntry(dir)
	if err != nil {
		t.Fatalf("loadImportedSkillEntry() error = %v", err)
	}
	report, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	quality := evaluateSkillQualityForDir(entry, report, false, dir)
	if !quality.MarketReady {
		t.Fatalf("quality should accept flag-style script reference: %+v", quality)
	}
	if len(quality.Package.ReferencedMissing) != 0 {
		t.Fatalf("ReferencedMissing = %+v", quality.Package.ReferencedMissing)
	}
}

func TestSkillQualityDetectsMissingStructuredCommandScript(t *testing.T) {
	dir := t.TempDir()
	data := []byte("name: structured-missing-script\ndescription: A portable skill using structured command metadata.\ntriggers:\n  - structured-missing-script\nplatforms:\n  - universal\nsteps:\n  - action: run\n    params:\n      command:\n        program: python\n        args:\n          - scripts/missing.py\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Structured missing script\n\nRuns a bundled script.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	entry, err := loadImportedSkillEntry(dir)
	if err != nil {
		t.Fatalf("loadImportedSkillEntry() error = %v", err)
	}
	report, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	quality := evaluateSkillQualityForDir(entry, report, false, dir)
	if quality.MarketReady {
		t.Fatalf("quality should block missing structured script reference: %+v", quality)
	}
	if len(quality.Package.ReferencedMissing) != 1 || quality.Package.ReferencedMissing[0] != "scripts/missing.py" {
		t.Fatalf("ReferencedMissing = %+v, want scripts/missing.py", quality.Package.ReferencedMissing)
	}
}

func TestSkillQualityAcceptsPackagedStructuredCommandScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	data := []byte("name: structured-script\ndescription: A portable skill using structured command metadata.\ntriggers:\n  - structured-script\nplatforms:\n  - universal\nsteps:\n  - action: run\n    params:\n      command:\n        program: python\n        args:\n          - scripts/run.py\n          - --count\n          - 3\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Structured script\n\nRuns a bundled script.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	entry, err := loadImportedSkillEntry(dir)
	if err != nil {
		t.Fatalf("loadImportedSkillEntry() error = %v", err)
	}
	report, err := skill.ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	quality := evaluateSkillQualityForDir(entry, report, false, dir)
	if !quality.MarketReady {
		t.Fatalf("quality should accept packaged structured script reference: %+v", quality)
	}
	if len(quality.Package.ReferencedMissing) != 0 {
		t.Fatalf("ReferencedMissing = %+v", quality.Package.ReferencedMissing)
	}
}

func TestSkillLifecycleRetryBlockedKeepsUnreadySkillBlocked(t *testing.T) {
	dir := t.TempDir()
	data := []byte("name: blocked-missing-script\ndescription: A portable skill whose referenced script is not packaged yet.\ntriggers:\n  - blocked-missing-script\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: python scripts/missing.py\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	m := &SkillLifecycleManager{queuePath: filepath.Join(t.TempDir(), "queue.json")}
	localHash := skillDirHash(dir)
	m.recordBlocked("blocked-missing-script", dir, "test_retry", localHash, false, "missing script", 25)

	if err := m.RetryBlocked("blocked-missing-script"); err != nil {
		t.Fatalf("RetryBlocked() error = %v", err)
	}
	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusBlocked {
		t.Fatalf("queue after retry = %+v", items)
	}
	if !strings.Contains(items[0].LastError, "missing referenced local file") {
		t.Fatalf("LastError = %q", items[0].LastError)
	}
}

func TestSkillLifecycleRetryBlockedRequeuesRepairedSkill(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	data := []byte("name: repaired-skill\ndescription: A portable skill that becomes market ready after repair.\ntriggers:\n  - repaired-skill\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: python scripts/run.py\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Repaired skill\n\nRuns the bundled repair script.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	m := &SkillLifecycleManager{queuePath: filepath.Join(t.TempDir(), "queue.json")}
	m.recordBlocked("repaired-skill", dir, "test_retry", skillDirHash(dir), false, "missing script", 25)
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	if err := m.RetryBlocked("repaired-skill"); err != nil {
		t.Fatalf("RetryBlocked() error = %v", err)
	}
	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusPending || items[0].LastError != "" {
		t.Fatalf("queue after repair retry = %+v", items)
	}
	if items[0].QualityScore < 70 || items[0].LocalHash == "" {
		t.Fatalf("queue quality/hash not refreshed: %+v", items[0])
	}
}

func TestSkillLifecycleProcessMovesUploadTimeQualityFailureToBlocked(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "late-broken-skill")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	data := []byte("name: late-broken-skill\ndescription: A portable skill that may break after it is queued.\ntriggers:\n  - late-broken-skill\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: python scripts/run.py\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Late broken skill\n\nRuns a bundled script.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "late-broken-skill", SkillDir: dir, Source: "file", Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	m := NewSkillLifecycleManager(app)
	if _, err := m.EnqueueUpload(context.Background(), "late-broken-skill", dir, "test", false, false); err != nil {
		t.Fatalf("EnqueueUpload() error = %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "scripts", "run.py")); err != nil {
		t.Fatalf("Remove(script) error = %v", err)
	}
	if err := m.ProcessPendingUploads(context.Background(), 1); err != nil {
		t.Fatalf("ProcessPendingUploads() error = %v", err)
	}
	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusBlocked || items[0].Attempts != 0 || items[0].NextAttemptAt != "" {
		t.Fatalf("queue after upload-time quality failure = %+v", items)
	}
	if !strings.Contains(items[0].LastError, "quality gate") || !strings.Contains(items[0].LastError, "missing referenced local file") {
		t.Fatalf("LastError = %q", items[0].LastError)
	}
}

func TestSkillLifecycleEnqueueUsesRegisteredRuntimeProof(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "proof-skill")
	writeLifecycleTestSkill(t, dir, "proof-skill")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Proof skill\n\nRuns after a successful verification.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:         "proof-skill",
		SkillDir:     dir,
		Source:       "file",
		UsageCount:   1,
		SuccessCount: 1,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	m := NewSkillLifecycleManager(app)
	item, err := m.EnqueueUpload(context.Background(), "proof-skill", dir, "test", true, false)
	if err != nil {
		t.Fatalf("EnqueueUpload() unexpected error = %v", err)
	}
	if item == nil || item.Status != skillUploadStatusPending || item.QualityScore < 70 {
		t.Fatalf("queued item = %+v", item)
	}
	statusData, err := os.ReadFile(filepath.Join(dir, "quality_status.json"))
	if err != nil {
		t.Fatalf("ReadFile(quality_status.json) error = %v", err)
	}
	var status persistedSkillQualityStatus
	if err := json.Unmarshal(statusData, &status); err != nil {
		t.Fatalf("Unmarshal(quality_status) error = %v", err)
	}
	if status.VerificationStatus != "verified_success" || !status.MarketReady {
		t.Fatalf("quality status = %+v", status)
	}
}

func TestSkillLifecycleEnqueueCanonicalizesNameBeforeDedupe(t *testing.T) {
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "alias-dir")
	writeLifecycleTestSkill(t, dir, "canonical-upload-skill")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Canonical upload skill\n\nPortable skill for canonical queue dedupe.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	var submitCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{server.URL}})
		case "/api/v1/skills/submit":
			submitCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"submission_id": "sub-canonical"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.RemoteViewerToken = "test-token"
	cfg.RemoteHubCenterURL = server.URL
	cfg.RemoteHubCenterURLs = []string{server.URL}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillMarketClient = NewSkillMarketClient(app)
	m := NewSkillLifecycleManager(app)
	app.skillLifecycle = m

	if _, err := m.EnqueueUpload(context.Background(), "alias-dir", dir, "test", false, true); err != nil {
		t.Fatalf("EnqueueUpload(alias) error = %v", err)
	}
	if _, err := m.EnqueueUpload(context.Background(), "canonical-upload-skill", dir, "test", false, true); err != nil {
		t.Fatalf("EnqueueUpload(canonical) error = %v", err)
	}
	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].SkillName != "canonical-upload-skill" || items[0].Status != skillUploadStatusUploaded {
		t.Fatalf("queue = %+v", items)
	}
	if submitCount != 1 {
		t.Fatalf("submitCount = %d, want one upload for canonicalized skill", submitCount)
	}
}

func TestSkillLifecycleRetryBlockedAndProcessUploadsReadySkill(t *testing.T) {
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "ready-after-proof")
	writeLifecycleTestSkill(t, dir, "ready-after-proof")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Ready after proof\n\nA portable skill that uploads once runtime proof exists.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	var submitCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{server.URL}})
		case "/api/v1/skills/submit":
			submitCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"submission_id": "sub-ready"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.RemoteViewerToken = "test-token"
	cfg.RemoteHubCenterURL = server.URL
	cfg.RemoteHubCenterURLs = []string{server.URL}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "ready-after-proof", SkillDir: dir, Source: "file", Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillMarketClient = NewSkillMarketClient(app)
	m := NewSkillLifecycleManager(app)
	app.skillLifecycle = m

	if _, err := m.EnqueueUpload(context.Background(), "ready-after-proof", dir, "auto_upload", true, false); err == nil {
		t.Fatal("EnqueueUpload() expected runtime proof block")
	}
	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusBlocked {
		t.Fatalf("queue before proof = %+v", items)
	}

	cfg.NLSkills[0].UsageCount = 1
	cfg.NLSkills[0].SuccessCount = 1
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig(success proof) error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	if _, err := m.upsertPending("other-pending-skill", "", "", "test", false, 100); err != nil {
		t.Fatalf("upsertPending(other) error = %v", err)
	}

	if err := m.RetryBlockedAndProcess(context.Background(), "ready-after-proof", 1); err != nil {
		t.Fatalf("RetryBlockedAndProcess() error = %v", err)
	}
	items, err = m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	var target, other *SkillUploadQueueItem
	for i := range items {
		if items[i].SkillName == "ready-after-proof" {
			target = &items[i]
		}
		if items[i].SkillName == "other-pending-skill" {
			other = &items[i]
		}
	}
	if target == nil || target.Status != skillUploadStatusUploaded || target.SubmissionID != "sub-ready" {
		t.Fatalf("target queue item after proof = %+v all=%+v", target, items)
	}
	if other == nil || other.Status != skillUploadStatusPending {
		t.Fatalf("unrelated pending item should not be processed: other=%+v all=%+v", other, items)
	}
	if submitCount != 1 {
		t.Fatalf("submitCount = %d, want only target upload", submitCount)
	}
}

func TestSkillLifecycleRetryBlockedAndProcessForcesNamedFailedBackoff(t *testing.T) {
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "backoff-ready")
	writeLifecycleTestSkill(t, dir, "backoff-ready")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Backoff ready\n\nA repaired skill can be retried immediately after validation.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	var submitCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{server.URL}})
		case "/api/v1/skills/submit":
			submitCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"submission_id": "sub-backoff-ready"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.RemoteViewerToken = "test-token"
	cfg.RemoteHubCenterURL = server.URL
	cfg.RemoteHubCenterURLs = []string{server.URL}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "backoff-ready", SkillDir: dir, Source: "file", Status: "active", UsageCount: 1, SuccessCount: 1}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillMarketClient = NewSkillMarketClient(app)
	m := NewSkillLifecycleManager(app)
	app.skillLifecycle = m

	now := time.Now()
	localHash := skillDirHash(dir)
	target := SkillUploadQueueItem{
		ID:             skillUploadQueueID("backoff-ready", localHash),
		SkillName:      "backoff-ready",
		SkillDir:       dir,
		LocalHash:      localHash,
		Reason:         "repair_success",
		Status:         skillUploadStatusFailed,
		Attempts:       2,
		LastError:      "temporary network failure before repair",
		NextAttemptAt:  now.Add(time.Hour).Format(time.RFC3339),
		QualityScore:   100,
		RequireRuntime: false,
		CreatedAt:      now.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt:      now.Add(-time.Minute).Format(time.RFC3339),
	}
	other := SkillUploadQueueItem{
		ID:             skillUploadQueueID("other-pending-skill", ""),
		SkillName:      "other-pending-skill",
		Reason:         "background",
		Status:         skillUploadStatusPending,
		QualityScore:   100,
		RequireRuntime: false,
		CreatedAt:      now.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt:      now.Add(-time.Minute).Format(time.RFC3339),
	}
	if err := m.saveQueueLocked(skillUploadQueueFile{Items: []SkillUploadQueueItem{other, target}}); err != nil {
		t.Fatalf("saveQueueLocked() error = %v", err)
	}

	if err := m.RetryBlockedAndProcess(context.Background(), "backoff-ready", 1); err != nil {
		t.Fatalf("RetryBlockedAndProcess() error = %v", err)
	}

	items, err := m.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	var gotTarget, gotOther *SkillUploadQueueItem
	for i := range items {
		if items[i].SkillName == "backoff-ready" {
			gotTarget = &items[i]
		}
		if items[i].SkillName == "other-pending-skill" {
			gotOther = &items[i]
		}
	}
	if gotTarget == nil || gotTarget.Status != skillUploadStatusUploaded || gotTarget.SubmissionID != "sub-backoff-ready" || gotTarget.NextAttemptAt != "" {
		t.Fatalf("target queue item after forced retry = %+v all=%+v", gotTarget, items)
	}
	if gotOther == nil || gotOther.Status != skillUploadStatusPending {
		t.Fatalf("unrelated pending item should not be processed: other=%+v all=%+v", gotOther, items)
	}
	if submitCount != 1 {
		t.Fatalf("submitCount = %d, want only target upload", submitCount)
	}
}
