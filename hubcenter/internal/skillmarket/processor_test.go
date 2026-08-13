package skillmarket

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hubskill "github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

// ── Task 4.5: Processor 单元测试 ────────────────────────────────────────

func TestSafeUnzip_ValidZip(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"skill.yaml": "name: test\ndescription: hello\n",
		"main.py":    "print('hello')\n",
	})
	destDir := t.TempDir()
	if err := SafeUnzip(zipPath, destDir); err != nil {
		t.Fatalf("SafeUnzip failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "skill.yaml")); err != nil {
		t.Error("skill.yaml not extracted")
	}
	if _, err := os.Stat(filepath.Join(destDir, "main.py")); err != nil {
		t.Error("main.py not extracted")
	}
}

func TestSafeUnzip_InvalidZip(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(tmpFile, []byte("not a zip file"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	if err := SafeUnzip(tmpFile, destDir); err == nil {
		t.Error("expected error for invalid zip")
	}
}

func TestSafeUnzip_TooManyFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("creating MaxSkillMarketZipEntries+1 zip entries is slow")
	}
	zipPath := createLargeFileCountZip(t, maxFileCount+1)
	destDir := t.TempDir()
	err := SafeUnzip(zipPath, destDir)
	if err == nil {
		t.Error("expected error for too many files")
	}
	if err != nil && !strings.Contains(err.Error(), "LIMIT_EXCEEDED") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSafeUnzip_AllowsManySmallNecessaryFiles(t *testing.T) {
	// Volume-first policy: multi-thousand tiny assets must pass (old cap was 1000).
	const n = 3500
	zipPath := createLargeFileCountZip(t, n)
	destDir := t.TempDir()
	if err := SafeUnzip(zipPath, destDir); err != nil {
		t.Fatalf("SafeUnzip many small files: %v", err)
	}
}

func TestSafeUnzip_ZipSlipPrevention(t *testing.T) {
	zipPath := createZipSlipZip(t)
	destDir := t.TempDir()
	err := SafeUnzip(zipPath, destDir)
	if err == nil {
		t.Error("expected error for zip slip attack")
	}
}

func TestSafeUnzip_RejectsParentDirEntry(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "dotdot.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	// Bare ".." must be rejected (filepath.Clean("..") has no "../" prefix).
	if _, err := w.Create(".."); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := SafeUnzip(zipPath, t.TempDir()); err == nil {
		t.Fatal("expected zip slip rejection for '..' entry")
	}
}

func TestSafeUnzip_SandboxCleanup(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"skill.yaml": "name: test\ndescription: hello\n",
	})
	sandboxDir := filepath.Join(t.TempDir(), "sandbox-test")
	if err := SafeUnzip(zipPath, sandboxDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Error("sandbox dir should exist after unzip")
	}
	os.RemoveAll(sandboxDir)
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Error("sandbox dir should be removed after cleanup")
	}
}

func TestProcessorPublishesMaclawAppJSONFile(t *testing.T) {
	store := newTestStore(t)
	skillStore := hubskill.NewSkillStore(t.TempDir())
	processor := NewProcessor("", t.TempDir(), store, skillStore, nil, nil, nil)
	ctx := context.Background()

	zipPath := createTestZip(t, map[string]string{
		"skill.yaml": `name: invoice-app
description: Invoice review app
triggers:
  - invoice
steps:
  - action: craft_tool
    params:
      instructions: review invoice
`,
		"skill_package_manifest.json": `{
  "package_kind": "maclaw-skill-market",
  "product_kind": "maclaw_app_skill",
  "is_maclaw_app": true,
  "maclaw_app_count": 1,
  "maclaw_app_entry": "maclaw.app.json",
  "maclaw_app_id": "invoice-review",
  "maclaw_app_name": "Invoice Review",
  "maclaw_app_description": "Review invoices with a guided panel",
  "maclaw_app_category": "finance",
  "maclaw_app_icon": "receipt",
  "maclaw_app_input_mode": "file",
  "maclaw_app_output_modes": ["pdf", "docx"],
  "maclaw_app_definition_sha256": "abc123",
  "maclaw_app_test_evidence": {"run_id":"run-ok-1","verified_at":"2026-06-17T10:00:00Z","definition_fingerprint":"feedbeef","artifact_present":true,"artifact_name":"invoice.pdf","output_count":1,"primary_result":"invoice_ready","result_payload":{"business_status":"invoice_ready","business_record":{"id":"INV-1","status":"invoice_ready"}}},
  "artifact_contract_required": true,
  "artifact_contract_output_modes": ["pdf", "docx"],
  "artifact_contract_presentation": "preview_or_file",
  "declared_permissions": ["gui", "env:INVOICE_API_KEY", "tool:browser"],
  "declared_required_env": ["INVOICE_API_KEY"],
  "declared_requires_gui": true
}`,
		"maclaw.app.json": `{
  "schema": "maclaw.app.v1",
  "privateMarker": "x_maclaw_apps",
  "app": {"id": "invoice-review", "name": "Invoice Review", "description": "Review invoices with a guided panel", "category": "finance", "icon": "receipt"}
}`,
	})
	sub := &SkillSubmission{
		ID:        "sub-app-json",
		Email:     "seller@test.com",
		UserID:    "seller-1",
		Status:    "pending",
		ZipPath:   zipPath,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateSubmission(ctx, sub); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}

	if err := processor.processOne(ctx, sub.ID); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	gotSub, err := store.GetSubmissionByID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubmissionByID() error = %v", err)
	}
	published, err := skillStore.Get(gotSub.SkillID)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", gotSub.SkillID, err)
	}
	if _, ok := published.Files["maclaw.app.json"]; !ok {
		t.Fatalf("published files missing maclaw.app.json: %#v", published.Files)
	}
	if published.ProductKind != "maclaw_app_skill" || !published.IsMaclawApp || published.MaclawAppEntry != "maclaw.app.json" {
		t.Fatalf("unexpected app product metadata: %#v", published.HubSkillMeta)
	}
	if published.MaclawAppID != "invoice-review" || published.MaclawAppName != "Invoice Review" || published.MaclawAppDescription != "Review invoices with a guided panel" || published.MaclawAppCategory != "finance" || published.MaclawAppIcon != "receipt" {
		t.Fatalf("unexpected app preview metadata: %#v", published.HubSkillMeta)
	}
	if published.MaclawAppInputMode != "file" || strings.Join(published.MaclawAppOutputModes, ",") != "pdf,docx" {
		t.Fatalf("unexpected app IO metadata: %#v", published.HubSkillMeta)
	}
	if published.MaclawAppDefinitionSHA256 != "abc123" {
		t.Fatalf("unexpected app definition hash: %#v", published.HubSkillMeta)
	}
	if published.MaclawAppTestEvidence == nil || published.MaclawAppTestEvidence.RunID != "run-ok-1" || published.MaclawAppTestEvidence.DefinitionFingerprint != "feedbeef" || !published.MaclawAppTestEvidence.ArtifactPresent || published.MaclawAppTestEvidence.ArtifactName != "invoice.pdf" {
		t.Fatalf("unexpected app test evidence: %#v", published.HubSkillMeta)
	}
	if published.MaclawAppTestEvidence.OutputCount != 1 || published.MaclawAppTestEvidence.PrimaryResult != "invoice_ready" {
		t.Fatalf("unexpected structured app test evidence: %#v", published.HubSkillMeta)
	}
	if record, ok := published.MaclawAppTestEvidence.ResultPayload["business_record"].(map[string]any); !ok || record["id"] != "INV-1" {
		t.Fatalf("unexpected app test result payload: %#v", published.HubSkillMeta)
	}
	if !published.ArtifactContractRequired || strings.Join(published.ArtifactContractOutputModes, ",") != "pdf,docx" || published.ArtifactContractPresentation != "preview_or_file" {
		t.Fatalf("unexpected artifact contract: %#v", published.HubSkillMeta)
	}
	if !published.RequiresGUI || strings.Join(published.RequiredEnv, ",") != "INVOICE_API_KEY" || strings.Join(published.Permissions, ",") != "gui,env:INVOICE_API_KEY,tool:browser" {
		t.Fatalf("unexpected declared permissions: %#v", published.HubSkillMeta)
	}
}

func TestProcessorKeepsPublisherSkillIDRevisionsForHistory(t *testing.T) {
	store := newTestStore(t)
	skillStore := hubskill.NewSkillStore(t.TempDir())
	processor := NewProcessor("", t.TempDir(), store, skillStore, nil, nil, NewVersionManager(store))
	ctx := context.Background()

	createSubmission := func(id, version string) *SkillSubmission {
		zipPath := createTestZip(t, map[string]string{
			"skill.yaml": "id: paper.pdf-translator\nname: PDF Translator\nversion: " + version + "\ndescription: Translate PDFs\n",
		})
		sub := &SkillSubmission{ID: id, Email: "seller@test.com", UserID: "seller-1", Status: "pending", ZipPath: zipPath, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := store.CreateSubmission(ctx, sub); err != nil {
			t.Fatalf("CreateSubmission(%s): %v", id, err)
		}
		if err := processor.processOne(ctx, id); err != nil {
			t.Fatalf("processOne(%s): %v", id, err)
		}
		return sub
	}

	first := createSubmission("sub-pdf-v1", "1.0.0")
	second := createSubmission("sub-pdf-v2", "2.0.0")
	firstStored, err := store.GetSubmissionByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondStored, err := store.GetSubmissionByID(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstStored.SkillID == "" || secondStored.SkillID == "" || firstStored.SkillID == secondStored.SkillID {
		t.Fatalf("submission IDs = %q, %q; want two immutable revision IDs", firstStored.SkillID, secondStored.SkillID)
	}
	if _, err := skillStore.Get(firstStored.SkillID); err != nil {
		t.Fatalf("first revision missing: %v", err)
	}
	listed := skillStore.ListAllPaged(1, 20)
	if listed.Total != 1 || len(listed.Skills) != 1 || listed.Skills[0].ID != secondStored.SkillID || listed.Skills[0].VersionCount != 2 {
		t.Fatalf("catalog result = %#v, want latest revision with two-version history", listed)
	}
}

func createTestZip(t *testing.T, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func createLargeFileCountZip(t *testing.T, count int) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "many_files.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for i := 0; i < count; i++ {
		name := filepath.Join("files", strings.Replace(generateID(), "-", "", -1))
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte("x"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func createZipSlipZip(t *testing.T) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "slip.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	fw, err := w.Create("../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("malicious"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func TestResolvePackageRoot_FlatLayout(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0o644)
	got := resolvePackageRoot(dir)
	if got != dir {
		t.Errorf("expected %s, got %s", dir, got)
	}
}

func TestResolvePackageRoot_WrappedLayout(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: test\n"), 0o644)
	got := resolvePackageRoot(dir)
	if got != sub {
		t.Errorf("expected %s, got %s", sub, got)
	}
}

func TestResolvePackageRoot_WrappedWithMacOSX(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: test\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "__MACOSX"), 0o755)
	got := resolvePackageRoot(dir)
	if got != sub {
		t.Errorf("expected %s, got %s", sub, got)
	}
}

func TestResolvePackageRoot_MultipleDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.MkdirAll(filepath.Join(dir, "b"), 0o755)
	got := resolvePackageRoot(dir)
	if got != dir {
		t.Errorf("expected fallback to %s, got %s", dir, got)
	}
}

func TestResolvePackageRoot_SubdirWithoutYaml(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "empty-skill"), 0o755)
	got := resolvePackageRoot(dir)
	if got != dir {
		t.Errorf("expected fallback to %s, got %s", dir, got)
	}
}

func TestResolvePackageRoot_WrappedWithLooseFiles(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: test\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte{}, 0o644)
	got := resolvePackageRoot(dir)
	if got != sub {
		t.Errorf("expected %s, got %s", sub, got)
	}
}

func TestValidatePackage_RejectsLegacyPackage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Legacy Skill\n\nUse it."), 0o644)
	os.WriteFile(filepath.Join(dir, "_meta.json"), []byte(`{"description":"legacy"}`), 0o644)

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected invalid result")
	}
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "no longer supported") {
		t.Fatalf("unexpected errors: %#v", result.Errors)
	}
}

func TestValidatePackage_EnrichesDescriptionFromSkillMD(t *testing.T) {
	// Enterprise Hub packages often put human title/description only in skill.md.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: ppt-master\ntriggers:\n  - ppt\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\nname: PPT 演示文稿大师\ndescription: AI multi-format presentation skill\n---\n\n# PPT Master\n"), 0o644)

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid package, got errors: %v", result.Errors)
	}
	if result.Metadata == nil || result.Metadata.Description != "AI multi-format presentation skill" {
		t.Fatalf("expected description enriched from skill.md, got %#v", result.Metadata)
	}
	// Prefer human markdown title over technical yaml slug for market display.
	if result.Metadata.Name != "PPT 演示文稿大师" {
		t.Fatalf("expected human name from skill.md, got %q", result.Metadata.Name)
	}
}

func TestIsLikelySkillSlug(t *testing.T) {
	if !isLikelySkillSlug("ppt-master") {
		t.Fatal("expected slug")
	}
	if isLikelySkillSlug("PPT 演示文稿大师") {
		t.Fatal("CJK title should not be slug")
	}
	if isLikelySkillSlug("Hello World") {
		t.Fatal("spaced title should not be slug")
	}
}

func TestValidatePackage_SkillMDFrontmatterCRLF(t *testing.T) {
	dir := t.TempDir()
	// Windows-style CRLF frontmatter must still enrich description.
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: crlf-skill\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\r\nname: CRLF Title\r\ndescription: from crlf frontmatter\r\n---\r\n\r\n# body\r\n"), 0o644)

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, errors=%v", result.Errors)
	}
	if result.Metadata.Description != "from crlf frontmatter" {
		t.Fatalf("description=%q", result.Metadata.Description)
	}
	if result.Metadata.Name != "CRLF Title" {
		t.Fatalf("name=%q", result.Metadata.Name)
	}
}

func TestValidatePackage_AcceptsSkillYML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yml"), []byte("name: yml-skill\ndescription: uses yml extension\n"), 0o644)

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	if result.Metadata.Name != "yml-skill" {
		t.Fatalf("name = %q", result.Metadata.Name)
	}
}

func TestProcessorRecoverPendingEnqueuesUnfinished(t *testing.T) {
	store := newTestStore(t)
	skillStore := hubskill.NewSkillStore(t.TempDir())
	processor := NewProcessor("", t.TempDir(), store, skillStore, nil, nil, nil)
	ctx := context.Background()

	zipPath := createTestZip(t, map[string]string{
		"skill.yaml": "name: recover-me\ndescription: should be requeued\n",
	})
	sub := &SkillSubmission{
		ID:        "sub-recover-1",
		Email:     "seller@test.com",
		UserID:    "seller-1",
		Status:    "pending",
		ZipPath:   zipPath,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateSubmission(ctx, sub); err != nil {
		t.Fatalf("CreateSubmission: %v", err)
	}

	// Recover into the queue, then drain one item via processOne path used by Run.
	processor.RecoverPending(ctx)
	select {
	case id := <-processor.queue:
		if id != sub.ID {
			t.Fatalf("recovered id = %q, want %q", id, sub.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected unfinished submission to be enqueued")
	}
}

func TestValidatePackage_WrappedLayout(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: wrapped\ndescription: test wrapped layout\n"), 0o644)

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	if result.Metadata.Name != "wrapped" {
		t.Errorf("expected name 'wrapped', got %q", result.Metadata.Name)
	}
	if result.PackageRoot != sub {
		t.Errorf("expected PackageRoot %s, got %s", sub, result.PackageRoot)
	}
}

func TestValidatePackageReadsMaclawAppProductManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: invoice-app\ndescription: Invoice review app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill_package_manifest.json"), []byte(`{
		"package_kind": "maclaw-skill-market",
		"product_kind": "maclaw_app_skill",
		"is_maclaw_app": true,
		"maclaw_app_count": 1,
		"maclaw_app_entry": "maclaw.app.json",
		"maclaw_app_id": "invoice-review",
		"maclaw_app_name": "Invoice Review",
		"maclaw_app_description": "Review invoices with a guided panel",
		"maclaw_app_category": "finance",
		"maclaw_app_icon": "receipt",
		"maclaw_app_input_mode": "file",
		"maclaw_app_output_modes": ["pdf", "docx"],
		"maclaw_app_definition_sha256": "abc123",
		"maclaw_app_test_evidence": {"run_id":"run-ok-1","verified_at":"2026-06-17T10:00:00Z","definition_fingerprint":"feedbeef","artifact_present":true,"artifact_name":"invoice.pdf","output_count":1,"primary_result":"invoice_ready","result_payload":{"business_status":"invoice_ready","business_record":{"id":"INV-1","status":"invoice_ready"}}},
		"artifact_contract_required": true,
		"artifact_contract_output_modes": ["pdf", "docx"],
		"artifact_contract_presentation": "preview_or_file",
		"declared_permissions": ["gui", "env:INVOICE_API_KEY", "tool:browser"],
		"declared_required_env": ["INVOICE_API_KEY"],
		"declared_requires_gui": true
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	meta := result.Metadata
	if meta.ProductKind != "maclaw_app_skill" || !meta.IsMaclawApp || meta.MaclawAppCount != 1 || meta.MaclawAppEntry != "maclaw.app.json" {
		t.Fatalf("unexpected app product metadata: %#v", meta)
	}
	if meta.MaclawAppID != "invoice-review" || meta.MaclawAppName != "Invoice Review" || meta.MaclawAppDescription != "Review invoices with a guided panel" || meta.MaclawAppCategory != "finance" || meta.MaclawAppIcon != "receipt" {
		t.Fatalf("unexpected app preview metadata: %#v", meta)
	}
	if meta.MaclawAppInputMode != "file" || strings.Join(meta.MaclawAppOutputModes, ",") != "pdf,docx" {
		t.Fatalf("unexpected app IO metadata: %#v", meta)
	}
	if meta.MaclawAppDefinitionSHA256 != "abc123" {
		t.Fatalf("unexpected app definition hash: %#v", meta)
	}
	if meta.MaclawAppTestEvidence == nil || meta.MaclawAppTestEvidence.RunID != "run-ok-1" || meta.MaclawAppTestEvidence.DefinitionFingerprint != "feedbeef" || !meta.MaclawAppTestEvidence.ArtifactPresent || meta.MaclawAppTestEvidence.ArtifactName != "invoice.pdf" {
		t.Fatalf("unexpected app test evidence: %#v", meta)
	}
	if meta.MaclawAppTestEvidence.OutputCount != 1 || meta.MaclawAppTestEvidence.PrimaryResult != "invoice_ready" {
		t.Fatalf("unexpected structured app test evidence: %#v", meta)
	}
	if record, ok := meta.MaclawAppTestEvidence.ResultPayload["business_record"].(map[string]any); !ok || record["id"] != "INV-1" {
		t.Fatalf("unexpected app test result payload: %#v", meta)
	}
	if !meta.ArtifactContractRequired || strings.Join(meta.ArtifactContractOutputModes, ",") != "pdf,docx" || meta.ArtifactContractPresentation != "preview_or_file" {
		t.Fatalf("unexpected artifact contract: %#v", meta)
	}
	if !meta.RequiresGUI || strings.Join(meta.RequiredEnv, ",") != "INVOICE_API_KEY" || strings.Join(meta.Permissions, ",") != "gui,env:INVOICE_API_KEY,tool:browser" {
		t.Fatalf("unexpected declared permissions: %#v", meta)
	}
}
