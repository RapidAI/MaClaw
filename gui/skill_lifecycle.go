package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

const skillMarketReadyMinScore = 70

type skillQualityReport struct {
	Score       int
	MarketReady bool
	Reasons     []string
	Portability *cskill.PortabilityReport
	Package     skillPackageQualitySummary
}

type skillPackageQualitySummary struct {
	Files              int      `json:"files"`
	HasSkillYAML       bool     `json:"has_skill_yaml"`
	HasSkillDefinition bool     `json:"has_skill_definition"`
	HasSkillMD         bool     `json:"has_skill_md"`
	ReferencedMissing  []string `json:"referenced_missing,omitempty"`
}

// SkillQualityStatus is the exported view returned by lifecycle quality audits.
type SkillQualityStatus = persistedSkillQualityStatus

type persistedSkillQualityStatus struct {
	SkillName           string                      `json:"skill_name"`
	Stage               string                      `json:"stage"`
	Score               int                         `json:"score"`
	MarketReady         bool                        `json:"market_ready"`
	MinMarketScore      int                         `json:"min_market_score"`
	Reasons             []string                    `json:"reasons,omitempty"`
	PortabilitySummary  cskill.IssueSummary         `json:"portability_summary"`
	PackageSummary      skillPackageQualitySummary  `json:"package_summary"`
	RequireRuntimeProof bool                        `json:"require_runtime_proof"`
	UsageCount          int                         `json:"usage_count"`
	SuccessCount        int                         `json:"success_count"`
	FailureCount        int                         `json:"failure_count"`
	VerificationStatus  skillVerificationStatusKind `json:"verification_status"`
	VerificationSummary string                      `json:"verification_summary,omitempty"`
	LocalHash           string                      `json:"local_hash,omitempty"`
	UpdatedAt           string                      `json:"updated_at"`
}

type skillPackageManifest struct {
	SkillName                    string                      `json:"skill_name"`
	PackageKind                  string                      `json:"package_kind"`
	ProductKind                  string                      `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                        `json:"is_maclaw_app,omitempty"`
	MaclawAppCount               int                         `json:"maclaw_app_count,omitempty"`
	MaclawAppEntry               string                      `json:"maclaw_app_entry,omitempty"`
	MaclawAppID                  string                      `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                      `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                      `json:"maclaw_app_description,omitempty"`
	MaclawAppCategory            string                      `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                      `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                      `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string                    `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                      `json:"maclaw_app_definition_sha256,omitempty"`
	ArtifactContractRequired     bool                        `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string                    `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                      `json:"artifact_contract_presentation,omitempty"`
	MaclawAppTestEvidence        *maclawAppTestEvidence      `json:"maclaw_app_test_evidence,omitempty"`
	DeclaredPermissions          []string                    `json:"declared_permissions,omitempty"`
	DeclaredRequiredEnv          []string                    `json:"declared_required_env,omitempty"`
	DeclaredRequiresGUI          bool                        `json:"declared_requires_gui,omitempty"`
	GeneratedAt                  string                      `json:"generated_at"`
	Quality                      persistedSkillQualityStatus `json:"quality"`
	Files                        []skillPackageManifestFile  `json:"files"`
}

type maclawAppTestEvidence struct {
	RunID                  string           `json:"run_id,omitempty"`
	VerifiedAt             string           `json:"verified_at,omitempty"`
	DefinitionFingerprint  string           `json:"definition_fingerprint,omitempty"`
	AppKind                string           `json:"app_kind,omitempty"`
	ArtifactPresent        bool             `json:"artifact_present,omitempty"`
	ArtifactName           string           `json:"artifact_name,omitempty"`
	OutputCount            int              `json:"output_count,omitempty"`
	PrimaryResult          string           `json:"primary_result,omitempty"`
	ResultPayload          map[string]any   `json:"result_payload,omitempty"`
	ApprovalInstance       map[string]any   `json:"approval_instance,omitempty"`
	ProgressInstances      []map[string]any `json:"progress_instances,omitempty"`
	ApprovalViews          []string         `json:"approval_views,omitempty"`
	DependencyVerification map[string]any   `json:"dependency_verification,omitempty"`
	WorkspaceLayout        map[string]any   `json:"workspace_layout,omitempty"`
	DataSrvRegistration    map[string]any   `json:"datasrv_registration,omitempty"`
	WorkflowContract       map[string]any   `json:"workflow_contract,omitempty"`
}

type skillPackageManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func skillVerificationStatus(entry *corelib.NLSkillEntry, requireRuntimeProof bool) skillVerificationStatusKind {
	if entry == nil {
		return skillVerificationStatusMissingEntry
	}
	if entry.SuccessCount > 0 {
		return skillVerificationStatusVerifiedSuccess
	}
	if entry.FailureCount > 0 {
		return skillVerificationStatusFailed
	}
	if requireRuntimeProof {
		return skillVerificationStatusNeedsRuntimeProof
	}
	return skillVerificationStatusStaticChecked
}

func skillVerificationSummary(entry *corelib.NLSkillEntry, requireRuntimeProof bool) string {
	if entry == nil {
		return "skill entry is missing"
	}
	if entry.SuccessCount > 0 {
		return fmt.Sprintf("%d successful run(s), %d failed run(s)", entry.SuccessCount, entry.FailureCount)
	}
	if entry.FailureCount > 0 {
		return fmt.Sprintf("no successful run; %d failed run(s)", entry.FailureCount)
	}
	if requireRuntimeProof {
		return "upload requires at least one successful verification run"
	}
	return "static portability and package checks completed"
}

func buildSkillQualityStatus(skillDir string, entry *corelib.NLSkillEntry, quality skillQualityReport, stage string, requireRuntimeProof bool) SkillQualityStatus {
	status := persistedSkillQualityStatus{
		Stage:               stage,
		Score:               quality.Score,
		MarketReady:         quality.MarketReady,
		MinMarketScore:      skillMarketReadyMinScore,
		Reasons:             append([]string(nil), quality.Reasons...),
		RequireRuntimeProof: requireRuntimeProof,
		VerificationStatus:  skillVerificationStatus(entry, requireRuntimeProof),
		VerificationSummary: skillVerificationSummary(entry, requireRuntimeProof),
		UpdatedAt:           time.Now().Format(time.RFC3339),
	}
	if entry != nil {
		status.SkillName = entry.Name
		status.UsageCount = entry.UsageCount
		status.SuccessCount = entry.SuccessCount
		status.FailureCount = entry.FailureCount
	}
	if strings.TrimSpace(skillDir) != "" {
		status.LocalHash = skillDirHash(skillDir)
	}
	if quality.Portability != nil {
		status.PortabilitySummary = quality.Portability.Summary
	}
	status.PackageSummary = quality.Package
	return status
}

func writeSkillPackageManifest(skillDir string, entry *corelib.NLSkillEntry, quality skillQualityReport, stage string, requireRuntimeProof bool) error {
	if strings.TrimSpace(skillDir) == "" || entry == nil {
		return nil
	}
	status := buildSkillQualityStatus(skillDir, entry, quality, stage, requireRuntimeProof)
	manifest := skillPackageManifest{
		SkillName:   entry.Name,
		PackageKind: "maclaw-skill-market",
		GeneratedAt: time.Now().Format(time.RFC3339),
		Quality:     status,
	}
	manifest.DeclaredPermissions = skillPackageDeclaredPermissions(entry)
	manifest.DeclaredRequiredEnv = cloneStringSlice(entry.RequiredEnv)
	manifest.DeclaredRequiresGUI = entry.RequiresGUI
	if isApp, count, appEntry := inspectMaclawAppSkillMetadata(skillDir); isApp {
		manifest.ProductKind = "maclaw_app_skill"
		manifest.IsMaclawApp = true
		manifest.MaclawAppCount = count
		manifest.MaclawAppEntry = appEntry
		appPath := filepath.Join(skillDir, appEntry)
		if data, err := os.ReadFile(appPath); err == nil {
			sum := sha256.Sum256(data)
			manifest.MaclawAppDefinitionSHA256 = hex.EncodeToString(sum[:])
			manifest.MaclawAppTestEvidence = maclawAppTestEvidenceFromDefinition(data)
		}
		if app, ok := readMaclawAppDefinitionAsSkillApp(appPath, entry.Name); ok {
			manifest.MaclawAppID = app.ID
			manifest.MaclawAppName = app.Name
			manifest.MaclawAppDescription = app.Description
			manifest.MaclawAppCategory = app.Category
			manifest.MaclawAppIcon = app.Icon
			manifest.MaclawAppInputMode = app.InputMode
			manifest.MaclawAppOutputModes = append([]string(nil), app.OutputModes...)
			if len(app.OutputModes) > 0 {
				manifest.ArtifactContractRequired = true
				manifest.ArtifactContractOutputModes = append([]string(nil), app.OutputModes...)
				manifest.ArtifactContractPresentation = "preview_or_file"
			}
		}
	}
	if err := filepath.WalkDir(skillDir, func(path string, dirEntry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if dirEntry.IsDir() {
			if isSkillRuntimePackageDir(dirEntry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		base := dirEntry.Name()
		if isSkillRuntimePackageFile(base) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := dirEntry.Info()
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, skillPackageManifestFile{
			Path:   filepath.ToSlash(rel),
			Size:   info.Size(),
			SHA256: hex.EncodeToString(sum[:]),
		})
		return nil
	}); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(skillDir, "skill_package_manifest.json"), data, 0o644)
}

func skillPackageDeclaredPermissions(entry *corelib.NLSkillEntry) []string {
	if entry == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	if entry.RequiresGUI {
		add("gui")
	}
	for _, env := range entry.RequiredEnv {
		add("env:" + env)
	}
	for _, file := range entry.RequiredCredentialFiles {
		if strings.TrimSpace(file) != "" {
			add("credential_file")
			break
		}
	}
	for _, tool := range entry.RequiresTools {
		add("tool:" + tool)
	}
	for _, toolset := range entry.RequiresToolsets {
		add("toolset:" + toolset)
	}
	for _, capability := range entry.Capabilities {
		add("capability:" + capability)
	}
	return out
}

func maclawAppTestEvidenceFromDefinition(data []byte) *maclawAppTestEvidence {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	app, _ := doc["app"].(map[string]any)
	governance, _ := app["governance"].(map[string]any)
	binding, _ := app["binding"].(map[string]any)
	testEvidence, _ := governance["testEvidence"].(map[string]any)
	if len(testEvidence) == 0 {
		testEvidence, _ = governance["test_evidence"].(map[string]any)
	}
	if len(testEvidence) == 0 {
		return nil
	}
	out := &maclawAppTestEvidence{
		RunID:                 strings.TrimSpace(firstNonEmptySkillAppString(stringMapValue(testEvidence, "runId"), stringMapValue(testEvidence, "run_id"))),
		VerifiedAt:            strings.TrimSpace(firstNonEmptySkillAppString(stringMapValue(testEvidence, "verifiedAt"), stringMapValue(testEvidence, "verified_at"))),
		DefinitionFingerprint: strings.TrimSpace(firstNonEmptySkillAppString(stringMapValue(testEvidence, "definitionHash"), stringMapValue(testEvidence, "definition_hash"), stringMapValue(testEvidence, "definitionFingerprint"), stringMapValue(testEvidence, "definition_fingerprint"))),
		AppKind:               strings.TrimSpace(firstNonEmptySkillAppString(stringMapValue(testEvidence, "appKind"), stringMapValue(testEvidence, "app_kind"), stringMapValue(app, "kind"))),
		ArtifactName:          strings.TrimSpace(firstNonEmptySkillAppString(stringMapValue(testEvidence, "artifactName"), stringMapValue(testEvidence, "artifact_name"))),
		PrimaryResult:         strings.TrimSpace(firstNonEmptySkillAppString(stringMapValue(testEvidence, "primaryResult"), stringMapValue(testEvidence, "primary_result"))),
	}
	if count, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(testEvidence["outputCount"], testEvidence["output_count"])); ok && count > 0 {
		out.OutputCount = int(count)
	}
	if payload := anyMap(firstNonEmptyMaclawAppAny(testEvidence["resultPayload"], testEvidence["result_payload"])); len(payload) > 0 {
		out.ResultPayload = cloneMapAny(payload)
	}
	if approval := anyMap(firstNonEmptyMaclawAppAny(testEvidence["approvalInstance"], testEvidence["approval_instance"])); len(approval) > 0 {
		out.ApprovalInstance = cloneMapAny(approval)
	}
	out.ProgressInstances = maclawAppEvidenceMapSlice(firstNonEmptyMaclawAppAny(
		testEvidence["progressInstances"],
		testEvidence["progress_instances"],
		testEvidence["workflowProgress"],
		testEvidence["workflow_progress"],
		testEvidence["approvalProgress"],
		testEvidence["approval_progress"],
		firstNonEmptyMaclawAppAny(out.ApprovalInstance["progressInstances"], out.ApprovalInstance["progress_instances"], out.ApprovalInstance["workflowProgress"], out.ApprovalInstance["workflow_progress"]),
	))
	out.ApprovalViews = maclawAppEvidenceStringSlice(firstNonEmptyMaclawAppAny(testEvidence["approvalViews"], testEvidence["approval_views"], testEvidence["viewVerified"], testEvidence["approvalInstanceViewVerified"]))
	out.DependencyVerification = cloneMapAny(anyMap(firstNonEmptyMaclawAppAny(testEvidence["dependencyVerification"], testEvidence["dependency_verification"], governance["dependencyVerification"], governance["dependency_verification"])))
	out.WorkspaceLayout = cloneMapAny(anyMap(firstNonEmptyMaclawAppAny(testEvidence["workspaceLayout"], testEvidence["workspace_layout"], governance["workspaceLayout"], governance["workspace_layout"])))
	out.DataSrvRegistration = cloneMapAny(anyMap(firstNonEmptyMaclawAppAny(testEvidence["datasrvRegistration"], testEvidence["datasrv_registration"], testEvidence["dataSrvRegistration"], governance["datasrvRegistration"], governance["datasrv_registration"], governance["dataSrvRegistration"])))
	out.WorkflowContract = cloneMapAny(anyMap(firstNonEmptyMaclawAppAny(testEvidence["workflowContract"], testEvidence["workflow_contract"], governance["workflowContract"], governance["workflow_contract"], binding["workflowContract"], binding["workflow_contract"])))
	if boolMapValue(testEvidence, "artifactPresent") || boolMapValue(testEvidence, "artifact_present") {
		out.ArtifactPresent = true
	}
	if out.ArtifactName == "" {
		if path := strings.TrimSpace(firstNonEmptySkillAppString(stringMapValue(testEvidence, "artifactPath"), stringMapValue(testEvidence, "artifact_path"))); path != "" {
			out.ArtifactName = filepath.Base(path)
			out.ArtifactPresent = true
		}
	}
	if out.RunID == "" && out.VerifiedAt == "" && out.DefinitionFingerprint == "" && out.AppKind == "" && out.ArtifactName == "" && !out.ArtifactPresent && out.OutputCount == 0 && out.PrimaryResult == "" && len(out.ResultPayload) == 0 && len(out.ApprovalInstance) == 0 && len(out.ProgressInstances) == 0 && len(out.ApprovalViews) == 0 && len(out.DependencyVerification) == 0 && len(out.WorkspaceLayout) == 0 && len(out.DataSrvRegistration) == 0 && len(out.WorkflowContract) == 0 {
		return nil
	}
	return out
}

func maclawAppEvidenceMapSlice(raw any) []map[string]any {
	items := anySlice(raw)
	if len(items) == 0 {
		if item := anyMap(raw); len(item) > 0 {
			items = []any{item}
		}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m := anyMap(item)
		if len(m) == 0 {
			continue
		}
		if nested := anyMap(firstNonEmptyMaclawAppAny(m["approvalInstance"], m["approval_instance"], m["instance"])); len(nested) > 0 {
			m = nested
		}
		out = append(out, cloneMapAny(m))
	}
	return out
}

func maclawAppEvidenceStringSlice(raw any) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	switch v := raw.(type) {
	case []string:
		for _, item := range v {
			add(item)
		}
	case []any:
		for _, item := range v {
			add(fmt.Sprint(item))
		}
	case map[string]any:
		for key, value := range v {
			if ok, _ := value.(bool); ok {
				add(key)
			}
		}
	case string:
		for _, item := range strings.Split(v, ",") {
			add(item)
		}
	case bool:
		if v {
			add("verified")
		}
	}
	return out
}
func skillYAMLFromEntry(entry *corelib.NLSkillEntry) *cskill.SkillYAMLFile {
	return buildSkillYAMLFileFromPackageEntry(entry)
}

func writeSkillYAMLForEntry(skillDir string, entry *corelib.NLSkillEntry) error {
	if strings.TrimSpace(skillDir) == "" || entry == nil {
		return nil
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := validateSkillYAMLWritePath(yamlPath); err != nil {
		return err
	}
	if data, err := os.ReadFile(yamlPath); err == nil {
		if err := fileutil.AtomicWriteFile(yamlPath+".bak", data, 0o644); err != nil {
			return fmt.Errorf("backup skill.yaml: %w", err)
		}
	}
	data, err := cskill.FormatSkillYAMLFile(skillYAMLFromEntry(entry))
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(yamlPath, data, 0o644)
}

func validateSkillYAMLWritePath(yamlPath string) error {
	for _, path := range []string{yamlPath, yamlPath + ".bak"} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write skill definition through symlink: %s", path)
		}
		if info.IsDir() {
			return fmt.Errorf("refusing to write skill definition over directory: %s", path)
		}
	}
	return nil
}

func prepareSkillDirForMarket(skillDir string, autoFix bool, appOpt ...*App) ([]cskill.PortabilityChange, *cskill.PortabilityReport, error) {
	if strings.TrimSpace(skillDir) == "" {
		return nil, nil, fmt.Errorf("skill directory is empty")
	}
	preflight, err := cskill.PrepareSkillForUploadWithOptions(skillDir, cskill.UploadPreflightOptions{AutoFix: autoFix})
	if err != nil {
		return nil, nil, err
	}
	changes := append([]cskill.PortabilityChange(nil), preflight.AutoFixed...)
	report := preflight.Report
	if !preflight.Portable() {
		if preflightHasOnlyQualityGatePathRefs(preflight) {
			return changes, report, nil
		}
		return changes, report, &skillUploadBlockedError{Message: cskill.FormatUploadPreflight(preflight), Score: 0}
	}
	app := firstSkillLifecycleApp(appOpt...)
	if app != nil && app.isRiskGuardrailOffMode() {
		return changes, report, nil
	}
	if scanReport, scanErr := scanSkillDirForWriteback(skillDir); scanErr != nil {
		if app != nil && !app.skillInstallMissingScanShouldBlock() {
			return changes, report, nil
		}
		return changes, nil, scanErr
	} else if app != nil && app.skillInstallScanShouldBlock(scanReport) {
		return changes, nil, fmt.Errorf("skill package blocked by security scan: level=%s summary=%s", scanReport.FinalLevel, scanReport.Summary)
	}
	return changes, report, nil
}

func preflightHasOnlyQualityGatePathRefs(preflight *cskill.UploadPreflightResult) bool {
	if preflight == nil || len(preflight.BlockingPaths) > 0 || len(preflight.MissingFiles) == 0 {
		return false
	}
	for _, ref := range preflight.MissingFiles {
		ref = strings.TrimSpace(filepath.ToSlash(ref))
		if ref == "" {
			return false
		}
		if strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "//") {
			continue
		}
		if len(ref) >= 2 && ref[1] == ':' {
			continue
		}
		return false
	}
	return true
}

func firstSkillLifecycleApp(appOpt ...*App) *App {
	for _, app := range appOpt {
		if app != nil {
			return app
		}
	}
	return nil
}

func scanSkillDirForOutboundPackage(skillDir string, appOpt ...*App) error {
	if app := firstSkillLifecycleApp(appOpt...); app != nil && app.isRiskGuardrailOffMode() {
		return nil
	}
	entry, err := loadImportedSkillEntry(skillDir)
	if err != nil {
		return fmt.Errorf("reload skill for outbound security scan: %w", err)
	}
	entry.SkillDir = skillDir
	report := cskill.NewSecurityScanner(nil).ScanInstallStaged(context.Background(), entry, skillDir, nil)
	if report == nil {
		if app := firstSkillLifecycleApp(appOpt...); app != nil && !app.skillInstallMissingScanShouldBlock() {
			return nil
		}
		return fmt.Errorf("skill package outbound security scan produced no report")
	}
	if app := firstSkillLifecycleApp(appOpt...); app != nil {
		if app.skillInstallScanShouldBlock(report) {
			return fmt.Errorf("skill package blocked by outbound security scan: level=%s summary=%s", report.FinalLevel, report.Summary)
		}
		return nil
	}
	return nil
}

func scanSkillDirForWriteback(skillDir string) (*cskill.ScanReport, error) {
	entry, err := loadImportedSkillEntry(skillDir)
	if err != nil {
		return nil, fmt.Errorf("reload skill for security scan: %w", err)
	}
	entry.SkillDir = skillDir
	report := cskill.NewSecurityScanner(nil).ScanInstallStaged(context.Background(), entry, skillDir, nil)
	if report == nil {
		return nil, fmt.Errorf("security scan produced no report")
	}
	return report, nil
}

func removeSkillPackagingBackups(skillDir string) error {
	return filepath.WalkDir(skillDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".bak") || name == "upload_status.json" {
			return os.Remove(path)
		}
		return nil
	})
}

func evaluateSkillQuality(entry *corelib.NLSkillEntry, report *cskill.PortabilityReport, requireRuntimeSuccess bool) skillQualityReport {
	skillDir := ""
	if entry != nil {
		skillDir = entry.SkillDir
	}
	return evaluateSkillQualityForDir(entry, report, requireRuntimeSuccess, skillDir)
}

func evaluateSkillQualityForDir(entry *corelib.NLSkillEntry, report *cskill.PortabilityReport, requireRuntimeSuccess bool, skillDir string) skillQualityReport {
	q := skillQualityReport{Score: 100, MarketReady: true, Portability: report}
	if entry == nil {
		q.Score = 0
		q.MarketReady = false
		q.Reasons = append(q.Reasons, "missing skill entry")
		return q
	}
	if report == nil {
		q.Score -= 30
		q.Reasons = append(q.Reasons, "missing portability report")
	} else {
		q.Score -= report.Summary.Errors * 35
		q.Score -= report.Summary.Warnings * 8
		if report.Summary.Errors > 0 {
			q.Reasons = append(q.Reasons, fmt.Sprintf("%d portability error(s)", report.Summary.Errors))
		}
		if report.Summary.Warnings > 0 {
			q.Reasons = append(q.Reasons, fmt.Sprintf("%d portability warning(s)", report.Summary.Warnings))
		}
	}
	if strings.TrimSpace(entry.Description) == "" || len([]rune(entry.Description)) < 10 {
		q.Score -= 15
		q.Reasons = append(q.Reasons, "description is missing or too short")
	}
	if len(entry.Triggers) == 0 {
		q.Score -= 10
		q.Reasons = append(q.Reasons, "no trigger keywords")
	}
	if !normalizeSkillTypeKind(entry.Type).IsKnowledge() && len(entry.Steps) == 0 && len(entry.Pipeline) == 0 {
		q.Score -= 35
		q.Reasons = append(q.Reasons, "no executable steps or pipeline")
	}
	if strings.TrimSpace(entry.LastError) != "" {
		q.Score -= 20
		q.Reasons = append(q.Reasons, "latest run has an error")
	}
	if requireRuntimeSuccess && entry.SuccessCount == 0 {
		q.Score -= 35
		q.Reasons = append(q.Reasons, "no successful verification run yet")
	}
	if entry.FailureCount > entry.SuccessCount && entry.FailureCount > 0 {
		q.Score -= 20
		q.Reasons = append(q.Reasons, "failure count exceeds success count")
	}
	if q.Score < 0 {
		q.Score = 0
	}
	packageSummary, packagePenalty, packageFatal, packageReasons := evaluateSkillPackageCompleteness(skillDir, entry)
	q.Package = packageSummary
	if packagePenalty > 0 {
		q.Score -= packagePenalty
		q.Reasons = append(q.Reasons, packageReasons...)
	}
	if q.Score < 0 {
		q.Score = 0
	}
	q.MarketReady = q.Score >= skillMarketReadyMinScore && !packageFatal && (report == nil || report.Summary.Errors == 0)
	return q
}

func evaluateSkillPackageCompleteness(skillDir string, entry *corelib.NLSkillEntry) (skillPackageQualitySummary, int, bool, []string) {
	var summary skillPackageQualitySummary
	if strings.TrimSpace(skillDir) == "" {
		return summary, 0, false, nil
	}
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return summary, 35, true, []string{"skill directory cannot be read"}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if isSkillRuntimePackageFile(name) {
			continue
		}
		summary.Files++
		switch strings.ToLower(name) {
		case "skill.yaml", "skill.yml":
			summary.HasSkillYAML = true
			summary.HasSkillDefinition = true
		case "skill.md", "readme.md":
			summary.HasSkillMD = true
		}
	}

	penalty := 0
	fatal := false
	var reasons []string
	if !summary.HasSkillDefinition && !summary.HasSkillMD {
		penalty += 45
		fatal = true
		reasons = append(reasons, "package lacks skill definition or skill documentation")
	}
	if !summary.HasSkillMD {
		penalty += 8
		reasons = append(reasons, "missing skill documentation")
	}
	if entry != nil {
		summary.ReferencedMissing = missingReferencedSkillFiles(skillDir, entry)
		if len(summary.ReferencedMissing) > 0 {
			penalty += 35
			fatal = true
			reasons = append(reasons, "package is missing referenced local file(s): "+strings.Join(summary.ReferencedMissing, ", "))
		}
	}
	return summary, penalty, fatal, reasons
}

func isSkillRuntimePackageFile(name string) bool {
	return cskill.IsSkillRuntimePackageFile(name)
}

func isSkillRuntimePackageDir(name string) bool {
	return cskill.IsSkillRuntimePackageDir(name)
}

func missingReferencedSkillFiles(skillDir string, entry *corelib.NLSkillEntry) []string {
	if entry == nil {
		return nil
	}
	copy := *entry
	copy.SkillDir = skillDir
	return cskill.CollectMissingPackageFileReferences(&copy)
}

func writeSkillQualityStatus(skillDir string, entry *corelib.NLSkillEntry, quality skillQualityReport, stage string, requireRuntimeProof bool) {
	if strings.TrimSpace(skillDir) == "" || entry == nil {
		return
	}
	status := buildSkillQualityStatus(skillDir, entry, quality, stage, requireRuntimeProof)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		log.Printf("[skill-lifecycle] marshal quality status for %s failed: %v", entry.Name, err)
		return
	}
	if err := os.WriteFile(filepath.Join(skillDir, "quality_status.json"), data, 0o644); err != nil {
		log.Printf("[skill-lifecycle] write quality status for %s failed: %v", entry.Name, err)
	}
}

func normalizeInstalledSkillEntry(entry *corelib.NLSkillEntry, appOpt ...*App) *corelib.NLSkillEntry {
	if entry == nil || strings.TrimSpace(entry.SkillDir) == "" {
		return entry
	}
	changes, report, err := prepareSkillDirForMarket(entry.SkillDir, true, appOpt...)
	if err != nil {
		log.Printf("[skill-lifecycle] normalize %s failed: %v", entry.Name, err)
		return entry
	}
	if len(changes) > 0 {
		log.Printf("[skill-lifecycle] normalized %s with %d portability fix(es)", entry.Name, len(changes))
	}
	qualityEntry := entry
	if reloaded, err := loadMarketPackageSkillEntry(entry.SkillDir, entry); err == nil {
		qualityEntry = reloaded
	}
	quality := evaluateSkillQuality(qualityEntry, report, false)
	writeSkillQualityStatus(entry.SkillDir, qualityEntry, quality, "normalize", false)
	if report != nil && report.Summary.Errors > 0 {
		log.Printf("[skill-lifecycle] %s still has %d portability error(s) after normalization", entry.Name, report.Summary.Errors)
	}
	if reloaded, err := loadImportedSkillEntry(entry.SkillDir); err == nil {
		reloaded.Source = entry.Source
		reloaded.SourceProject = entry.SourceProject
		reloaded.HubSkillID = entry.HubSkillID
		reloaded.HubVersion = entry.HubVersion
		reloaded.TrustLevel = entry.TrustLevel
		return reloaded
	}
	return entry
}

func (a *App) normalizeInstalledSkill(entry *corelib.NLSkillEntry) *corelib.NLSkillEntry {
	if a != nil {
		a.ensureSkillLifecycleManager()
		if a.skillLifecycle != nil {
			return a.skillLifecycle.NormalizeInstalled(entry)
		}
	}
	return normalizeInstalledSkillEntry(entry, a)
}
