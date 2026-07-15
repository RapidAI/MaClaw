package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Guards read-modify-write of app_install_records.json.
var maclawAppInstallRegistryMu sync.RWMutex

// PlanMaclawAppInstall returns the backend-authoritative install plan for a
// maclaw.app.v1 entry or maclaw.app.pack.v1 package.
func (a *App) PlanMaclawAppInstall(packageJSON string) (maclawAppInstallPlan, error) {
	entries, err := parseMaclawAppInstallEntries(packageJSON)
	if err != nil {
		return maclawAppInstallPlan{}, err
	}
	var installDoc map[string]any
	_ = json.Unmarshal([]byte(packageJSON), &installDoc)
	installed := a.installedMaclawAppSkillIndex()
	plan := maclawAppInstallPlan{
		Schema:       "maclaw.app.install_plan.v1",
		Apps:         make([]maclawAppInstallPlanApp, 0, len(entries)),
		Dependencies: []maclawAppInstallPlanDependency{},
	}
	depsByKey := make(map[string]*maclawAppInstallPlanDependency)
	for _, entry := range entries {
		plan.Apps = append(plan.Apps, maclawAppInstallPlanApp{
			ID:     entry.ID,
			Name:   entry.Name,
			Kind:   normalizeMaclawAppKind(entry.Kind),
			Schema: entry.Schema,
		})
		for _, dep := range maclawAppDependenciesForEntry(entry) {
			key := maclawAppInstallPlanDependencyMergeKey(dep)
			existing := depsByKey[key]
			if existing == nil {
				dep.AppIDs = []string{entry.ID}
				if match, ok := maclawAppInstalledSkillMatch(installed, dep); ok {
					applyMaclawAppInstalledSkillDependency(&dep, match)
				} else if dep.Required {
					dep.Health = "missing"
					dep.Action = "blocked"
					dep.Message = "required skill dependency is missing"
				} else {
					dep.Health = "missing"
					dep.Action = "optional_missing"
					dep.Message = "optional skill dependency is missing"
				}
				depsByKey[key] = &dep
				plan.Dependencies = append(plan.Dependencies, dep)
				continue
			}
			if !containsMaclawAppString(existing.AppIDs, entry.ID) {
				existing.AppIDs = append(existing.AppIDs, entry.ID)
			}
		}
	}
	plan.WorkflowContractIssues = maclawAppWorkflowContractIssuesForEntries(entries, installed)
	if installDoc != nil {
		plan.GovernanceReviewIssues = maclawAppBlockingInstallGovernanceReviewIssues(installDoc)
	}
	for i := range plan.Dependencies {
		if dep := depsByKey[maclawAppInstallPlanDependencyMergeKey(plan.Dependencies[i])]; dep != nil {
			plan.Dependencies[i] = *dep
		}
	}
	// Merge resolved_dependencies from enriched packages: apply InstallRef and
	// Source upgrades to plan dependencies so receivers use deterministic IDs.
	// Must run AFTER the depsByKey sync loop above, otherwise the loop would
	// overwrite the enriched values with stale originals.
	if installDoc != nil {
		applyResolvedDependenciesToPlan(plan.Dependencies, installDoc)
	}
	maclawAppApplySourceVersionKeyDependencyRefs(plan.Dependencies)
	maclawAppValidateDependencyInstallRefs(plan.Dependencies)
	maclawAppApplyDependencyPreflightDiagnostics(plan.Dependencies)
	a.maclawAppApplyRemoteDependencyPreflightDiagnostics(plan.Dependencies)
	maclawAppMarkInstallableMissingDependencies(plan.Dependencies)
	plan.refreshMaclawAppDependencyFlags()
	plan.HasWorkflowContractIssue = len(plan.WorkflowContractIssues) > 0
	plan.HasGovernanceReviewIssue = len(plan.GovernanceReviewIssues) > 0
	return plan, nil
}

// InstallMaclawAppDependencies installs missing required Skill dependencies for a
// maclaw.app.v1 entry or maclaw.app.pack.v1 package, then returns the updated plan.
func (a *App) InstallMaclawAppDependencies(packageJSON string) (maclawAppInstallPlan, error) {
	plan, err := a.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		return maclawAppInstallPlan{}, err
	}
	for i := range plan.Dependencies {
		dep := &plan.Dependencies[i]
		if dep.Installed {
			if maclawAppDependencyIsReady(*dep) {
				dep.Action = "skip"
				if dep.Message == "" {
					dep.Message = "installed locally"
				}
				continue
			}
			if dep.VersionStatus == "mismatch" {
				if updated, updateErr := a.updateInstalledMaclawAppDependency(dep); updated {
					if updateErr != nil {
						dep.InstallErrorCode = "dependency_update_failed"
						dep.InstallErrorStage = "dependency_update"
						dep.InstallErrorDetail = updateErr.Error()
						dep.Health = "missing"
						dep.Action = "failed"
						dep.Message = updateErr.Error()
						continue
					}
					dep.Action = "updated"
					dep.Message = "updated dependency skill from remote"
					continue
				}
			}
		}
		if !dep.Required {
			dep.Health = "missing"
			dep.Action = "optional_missing"
			if dep.Message == "" {
				dep.Message = "optional skill dependency is missing"
			}
			continue
		}
		source, ok := maclawAppDependencyInstallerSource(*dep)
		if !ok {
			if installedFromBundle, bundleErr := a.installBundledMaclawAppDependency(packageJSON, *dep); installedFromBundle {
				if bundleErr != nil {
					dep.InstallErrorCode = "bundled_dependency_failed"
					dep.InstallErrorStage = "bundled_dependency_install"
					dep.InstallErrorDetail = bundleErr.Error()
					dep.Health = "missing"
					dep.Action = "failed"
					dep.Message = bundleErr.Error()
					continue
				}
				dep.Installed = true
				dep.Action = "installed_from_bundle"
				dep.Message = "installed bundled dependency skill"
				continue
			} else if bundleErr != nil {
				dep.InstallErrorCode = "bundled_dependency_failed"
				dep.InstallErrorStage = "bundled_dependency_install"
				dep.InstallErrorDetail = bundleErr.Error()
			}
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = firstNonEmpty(dep.InstallErrorDetail, fmt.Sprintf("required skill dependency source %q cannot be installed automatically", dep.Source))
			continue
		}
		var downloadTrace skillHubDownloadTrace
		installMixedSkill := func(source, id, installRef string) error {
			if a.maclawAppInstallMixedSkill != nil {
				return a.maclawAppInstallMixedSkill(source, id, installRef)
			}
			trace, err := a.installMixedSkillWithIntegrityAndLocatorTrace(source, id, installRef, dep.PackageDownloadURL, firstNonEmpty(dep.PackageSHA256, dep.PackageChecksum), dep.PackageSignature)
			downloadTrace = trace
			applySkillHubDownloadTraceToDependency(dep, trace)
			return err
		}
		installRef := maclawAppDependencyInstallerRef(*dep)
		if dep.InstallRefStatus == "invalid" || dep.InstallRefStatus == "missing" {
			dep.Health = "missing"
			dep.Action = "blocked"
			if dep.InstallRefMessage != "" {
				dep.Message = dep.InstallRefMessage
			}
			continue
		}
		if err := installMixedSkill(source, dep.ID, installRef); err != nil {
			applySkillHubDownloadTraceToDependency(dep, downloadTrace)
			if installedFromBundle, bundleErr := a.installBundledMaclawAppDependency(packageJSON, *dep); installedFromBundle {
				if bundleErr != nil {
					dep.InstallErrorCode = "bundled_dependency_failed"
					dep.InstallErrorStage = "bundled_dependency_install"
					dep.InstallErrorDetail = fmt.Sprintf("%v; bundled fallback failed: %v", err, bundleErr)
					dep.Health = "missing"
					dep.Action = "failed"
					dep.Message = dep.InstallErrorDetail
					continue
				}
				dep.Installed = true
				dep.Action = "installed_from_bundle"
				dep.Message = "remote dependency install failed; installed bundled dependency skill"
				continue
			} else if bundleErr != nil {
				dep.InstallErrorCode = "bundled_dependency_failed"
				dep.InstallErrorStage = "bundled_dependency_install"
				dep.InstallErrorDetail = fmt.Sprintf("%v; bundled fallback failed: %v", err, bundleErr)
				dep.Health = "missing"
				dep.Action = "failed"
				dep.Message = dep.InstallErrorDetail
				continue
			}
			dep.Health = "missing"
			dep.Action = "failed"
			dep.InstallErrorCode, dep.InstallErrorStage, dep.InstallErrorDetail = maclawAppClassifyDependencyInstallError(*dep, source, err)
			dep.Message = dep.InstallErrorDetail
			continue
		}
		applySkillHubDownloadTraceToDependency(dep, downloadTrace)
		dep.Installed = true
		dep.Action = "installed"
		if downloadTrace.UsedBase != "" {
			dep.Message = fmt.Sprintf("installed dependency skill (download_node=%s)", downloadTrace.UsedBase)
		} else {
			dep.Message = "installed dependency skill"
		}
	}
	installed := a.installedMaclawAppSkillIndex()
	for i := range plan.Dependencies {
		dep := &plan.Dependencies[i]
		previousAction := dep.Action
		previousMessage := dep.Message
		if match, ok := maclawAppInstalledSkillMatch(installed, *dep); ok {
			applyMaclawAppInstalledSkillDependency(dep, match)
			if maclawAppDependencyIsReady(*dep) && previousAction == "installed" {
				dep.Action = "installed"
				dep.Message = "installed dependency skill"
			}
			continue
		}
		dep.Installed = false
		dep.InstalledName = ""
		dep.InstalledVersion = ""
		dep.RuntimeSkillRef = ""
		dep.RequiredVersion = strings.TrimSpace(dep.Version)
		dep.VersionStatus = maclawAppDependencyVersionStatus(*dep)
		dep.InstalledDir = ""
		dep.InstalledStatus = ""
		dep.Health = "missing"
		if dep.Required {
			if previousAction == "installed" {
				dep.Action = "failed"
				dep.Message = "installed dependency skill was not found after install"
			} else if dep.Action == "" || dep.Action == "skip" {
				dep.Action = "blocked"
				dep.Message = "required skill dependency is missing"
			} else if previousMessage != "" {
				dep.Message = previousMessage
			}
		} else if dep.Action == "" || dep.Action == "skip" {
			dep.Action = "optional_missing"
			dep.Message = "optional skill dependency is missing"
		}
	}
	plan.refreshMaclawAppDependencyFlags()
	entries, err := parseMaclawAppInstallEntries(packageJSON)
	if err != nil {
		return maclawAppInstallPlan{}, err
	}
	plan.WorkflowContractIssues = maclawAppWorkflowContractIssuesForEntries(entries, installed)
	plan.HasWorkflowContractIssue = len(plan.WorkflowContractIssues) > 0
	plan.HasGovernanceReviewIssue = len(plan.GovernanceReviewIssues) > 0
	return plan, nil
}

// RecordMaclawAppInstall persists a local install audit record for installed
// MaClaw App entries and their dependency state.
func (a *App) RecordMaclawAppInstall(packageJSON string, source string) (map[string]any, error) {
	return a.recordMaclawAppInstall(packageJSON, source, nil)
}

func (a *App) recordMaclawAppInstall(packageJSON string, source string, planOverride *maclawAppInstallPlan) (map[string]any, error) {
	entries, err := parseMaclawAppInstallEntries(packageJSON)
	if err != nil {
		return nil, err
	}
	var plan maclawAppInstallPlan
	if planOverride != nil {
		plan = *planOverride
		plan.Apps = append([]maclawAppInstallPlanApp(nil), plan.Apps...)
		plan.Dependencies = cloneMaclawAppPlanDependencies(plan.Dependencies)
		plan.WorkflowContractIssues = append([]maclawAppReviewIssue(nil), plan.WorkflowContractIssues...)
		plan.GovernanceReviewIssues = append([]maclawAppReviewIssue(nil), plan.GovernanceReviewIssues...)
	} else {
		plan, err = a.PlanMaclawAppInstall(packageJSON)
		if err != nil {
			return nil, err
		}
	}
	if plan.HasWorkflowContractIssue && maclawAppWorkflowContractIssuesShouldPrecedeDependencyBlock(plan.WorkflowContractIssues, plan.HasMissingRequired || plan.HasBlockingDependency) {
		return nil, fmt.Errorf("cannot install MaClaw App: approval workflow contract is invalid: %s", firstMaclawAppReviewIssueMessage(plan.WorkflowContractIssues, "approval workflow contract issue"))
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		if detail := maclawAppInstallPlanBlockingDependencySummary(plan); detail != "" {
			return nil, fmt.Errorf("cannot install MaClaw App: required Skill dependencies are missing or unavailable: %s", detail)
		}
		return nil, fmt.Errorf("cannot install MaClaw App: required Skill dependencies are missing or unavailable")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &doc); err != nil {
		return nil, fmt.Errorf("decode maclaw app package: %w", err)
	}
	if issues := maclawAppBlockingInstallGovernanceReviewIssues(doc); len(issues) > 0 {
		blockingIssue := firstMaclawAppGovernanceIssueBlockingLocalInstall(issues)
		if blockingIssue == nil || strings.EqualFold(strings.TrimSpace(source), "enterprise_hub") {
			log.Printf("[maclaw-app] install governance review warning (non-blocking): %s", issues[0].Message)
		} else {
			return nil, fmt.Errorf("cannot install MaClaw App: governance review failed: %s", blockingIssue.Message)
		}
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(doc)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	dataSrvRegistration := a.registerMaclawAppInstallationsToDataSrv(entries, source, packageSHA, packageSize, plan.Dependencies)
	if err := maclawAppBlockingDataSrvRegistrationError(entries, dataSrvRegistration); err != nil {
		return nil, err
	}
	maclawAppInstallRegistryMu.Lock()
	registry, err := a.readMaclawAppInstallRegistryUnlocked()
	if err != nil {
		maclawAppInstallRegistryMu.Unlock()
		return nil, err
	}
	if registry.Schema == "" {
		registry.Schema = "maclaw.app.installs.v1"
	}
	if registry.Installs == nil {
		registry.Installs = []maclawAppInstallRecord{}
	}
	for _, entry := range entries {
		governance := maclawAppGovernanceMetadataForEntry(entry)
		record := maclawAppInstallRecord{
			AppID:                  entry.ID,
			AppName:                entry.Name,
			Kind:                   normalizeMaclawAppKind(entry.Kind),
			Source:                 strings.TrimSpace(source),
			InstalledAt:            now,
			PackageSHA:             packageSHA,
			PackageSize:            packageSize,
			Package:                cloneMapAny(entry.Entry),
			Dependencies:           cloneMaclawAppPlanDependenciesForApp(plan.Dependencies, entry.ID),
			VersionSnapshot:        maclawAppInstallVersionSnapshotForEntry(entry),
			WorkflowContract:       cloneMapAny(maclawAppWorkflowContractForEntry(entry)),
			WorkspaceLayout:        cloneMapAny(maclawAppWorkspaceLayoutMetadataForEntry(entry)),
			ResultContract:         cloneMapAny(anyMap(governance["result_contract"])),
			ReviewEvidence:         cloneMapAny(maclawAppReviewEvidenceForEntry(entry)),
			Submission:             cloneMapAny(maclawAppSubmissionMetadataForEntry(entry)),
			TestEvidence:           cloneMapAny(anyMap(governance["test_evidence"])),
			DependencyVerification: cloneMapAny(maclawAppDependencyVerificationMetadataForEntry(entry, plan.Dependencies)),
			DataSrvRegistration:    cloneMapAny(maclawAppDataSrvRegistrationForApp(dataSrvRegistration, entry.ID)),
			HasMissingRequired:     hasMissingMaclawAppRequiredDependencyForApp(plan.Dependencies, entry.ID),
			HasBlockingDependency:  hasBlockingMaclawAppRequiredDependencyForApp(plan.Dependencies, entry.ID),
			Message:                "installed locally",
		}
		registry.upsert(record)
	}
	registry.UpdatedAt = now
	if err := a.writeMaclawAppInstallRegistryUnlocked(registry); err != nil {
		maclawAppInstallRegistryMu.Unlock()
		return nil, err
	}
	maclawAppInstallRegistryMu.Unlock()
	return map[string]any{
		"schema":                  registry.Schema,
		"installed_at":            now,
		"app_count":               len(entries),
		"apps":                    plan.Apps,
		"package_sha":             packageSHA,
		"package_sha256":          packageSHA,
		"package_bytes":           packageSize,
		"dependencies":            cloneMaclawAppPlanDependencies(plan.Dependencies),
		"app_versions":            maclawAppInstallVersionSnapshotsByApp(entries),
		"install_evidence":        maclawAppInstallEvidenceByApp(entries, plan.Dependencies, dataSrvRegistration),
		"has_missing_required":    plan.HasMissingRequired,
		"has_blocking_dependency": plan.HasBlockingDependency,
		"datasrv_registration":    dataSrvRegistration,
	}, nil
}

func maclawAppDataSrvRegistrationForApp(registration map[string]any, appID string) map[string]any {
	registration = cloneMapAny(registration)
	if registration == nil {
		return nil
	}
	appID = strings.TrimSpace(appID)
	items, _ := registration["items"].([]map[string]any)
	if len(items) == 0 {
		return registration
	}
	selected := make([]map[string]any, 0, 1)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(stringMapValue(item, "app_id")), appID) {
			selected = append(selected, cloneMapAny(item))
		}
	}
	if len(selected) == 0 {
		registration["items"] = []map[string]any{}
		registration["eligible_count"] = 0
		registration["synced_count"] = 0
		registration["failed_count"] = 0
		registration["synced"] = false
		registration["status"] = "skipped"
		registration["reason"] = "no datasrv role bindings"
		return registration
	}
	syncedCount := 0
	failedCount := 0
	for _, item := range selected {
		if synced, _ := item["synced"].(bool); synced {
			syncedCount++
		} else {
			failedCount++
		}
	}
	registration["items"] = selected
	registration["eligible_count"] = len(selected)
	registration["synced_count"] = syncedCount
	registration["failed_count"] = failedCount
	registration["synced"] = syncedCount == len(selected) && failedCount == 0
	registration["status"] = maclawAppDataSrvRegistrationStatus(len(selected), syncedCount, failedCount)
	if failedCount > 0 && strings.TrimSpace(stringMapValue(registration, "reason")) == "" {
		registration["reason"] = "app installation failed to register"
	}
	return registration
}

func maclawAppDataSrvRegistrationStatus(eligibleCount, syncedCount, failedCount int) string {
	if eligibleCount <= 0 {
		return "skipped"
	}
	if syncedCount >= eligibleCount && failedCount == 0 {
		return "ready"
	}
	if syncedCount > 0 {
		return "partial"
	}
	if failedCount > 0 {
		return "failed"
	}
	return "skipped"
}

func (a *App) registerMaclawAppInstallationsToDataSrv(entries []parsedMaclawAppEntry, source, packageSHA string, packageSize int, dependencies []maclawAppInstallPlanDependency) map[string]any {
	payloads := maclawAppDataSrvInstallationPayloads(entries, source, packageSHA, packageSize, dependencies)
	result := map[string]any{
		"synced":         false,
		"eligible_count": len(payloads),
		"synced_count":   0,
		"failed_count":   0,
		"items":          []map[string]any{},
	}
	if len(payloads) == 0 {
		result["status"] = "skipped"
		result["reason"] = "no datasrv role bindings"
		return result
	}
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		result["reason"] = err.Error()
		return result
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
		result["status"] = "skipped"
		result["reason"] = "mis data service unavailable"
		return result
	}
	items := make([]map[string]any, 0, len(payloads))
	syncedCount := 0
	failedCount := 0
	for _, payload := range payloads {
		item := map[string]any{"app_id": payload.AppID, "role_binding_count": payload.RoleBindingCount}
		path := "/api/v1/data/app-installations/" + pathEscape(payload.AppID)
		data, err := a.callMISDataAPIBytes(cfg, http.MethodPut, path, compactPayload(payload.Body))
		if err != nil {
			failedCount++
			item["synced"] = false
			item["reason"] = err.Error()
			items = append(items, item)
			continue
		}
		syncedCount++
		item["synced"] = true
		var response map[string]any
		if err := json.Unmarshal(data, &response); err == nil {
			if status := strings.TrimSpace(stringMapValue(response, "status")); status != "" {
				item["status"] = status
			}
		}
		items = append(items, item)
	}
	result["items"] = items
	result["synced_count"] = syncedCount
	result["failed_count"] = failedCount
	result["synced"] = syncedCount == len(payloads) && failedCount == 0
	result["status"] = maclawAppDataSrvRegistrationStatus(len(payloads), syncedCount, failedCount)
	if failedCount > 0 {
		result["reason"] = "one or more app installations failed to register"
	}
	return result
}

func maclawAppBlockingDataSrvRegistrationError(entries []parsedMaclawAppEntry, registration map[string]any) error {
	if len(entries) == 0 || registration == nil {
		return nil
	}
	eligibleCount := maclawAppIntFromRegistration(registration["eligible_count"])
	if eligibleCount <= 0 {
		return nil
	}
	hasEnterpriseDataApp := false
	for _, entry := range entries {
		switch normalizeMaclawAppKind(entry.Kind) {
		case "enterprise_approval_app", "enterprise_normal_app":
			if len(maclawAppDataSrvRoleBindingsForEntry(entry)) > 0 {
				hasEnterpriseDataApp = true
			}
		}
	}
	if !hasEnterpriseDataApp {
		return nil
	}
	status := strings.ToLower(strings.TrimSpace(stringMapValue(registration, "status")))
	failedCount := maclawAppIntFromRegistration(registration["failed_count"])
	if status == "ready" && failedCount == 0 {
		return nil
	}
	if status == "" {
		status = maclawAppDataSrvRegistrationStatus(eligibleCount, maclawAppIntFromRegistration(registration["synced_count"]), failedCount)
	}
	detail := strings.TrimSpace(stringMapValue(registration, "reason"))
	if detail == "" {
		detail = "DataSrv app installation registration did not complete"
	}
	if itemDetail := maclawAppDataSrvRegistrationFailureItemDetail(registration); itemDetail != "" {
		detail += ": " + itemDetail
	}
	return fmt.Errorf("cannot install MaClaw App: DataSrv app installation registration failed: status=%s: %s", status, detail)
}

func maclawAppDataSrvRegistrationFailureItemDetail(registration map[string]any) string {
	items := []map[string]any{}
	switch typed := registration["items"].(type) {
	case []map[string]any:
		items = typed
	case []any:
		for _, raw := range typed {
			if item := anyMap(raw); item != nil {
				items = append(items, item)
			}
		}
	}
	for _, item := range items {
		if synced, _ := item["synced"].(bool); synced {
			continue
		}
		appID := strings.TrimSpace(stringMapValue(item, "app_id"))
		reason := strings.TrimSpace(stringMapValue(item, "reason"))
		switch {
		case appID != "" && reason != "":
			return appID + ": " + reason
		case appID != "":
			return appID
		case reason != "":
			return reason
		}
	}
	return ""
}

// ListMaclawAppInstalls returns newest-first local install audit records.
func (a *App) ListMaclawAppInstalls(limit int) ([]maclawAppInstallRecord, error) {
	registry, err := a.readMaclawAppInstallRegistry()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if limit > len(registry.Installs) {
		limit = len(registry.Installs)
	}
	out := make([]maclawAppInstallRecord, 0, limit)
	for _, record := range registry.Installs[:limit] {
		record.Dependencies = cloneMaclawAppPlanDependencies(record.Dependencies)
		record.Package = cloneMapAny(record.Package)
		record.WorkflowContract = cloneMapAny(record.WorkflowContract)
		record.WorkspaceLayout = cloneMapAny(record.WorkspaceLayout)
		record.ResultContract = cloneMapAny(record.ResultContract)
		record.TestEvidence = cloneMapAny(record.TestEvidence)
		record.DependencyVerification = cloneMapAny(record.DependencyVerification)
		record.DataSrvRegistration = cloneMapAny(record.DataSrvRegistration)
		out = append(out, record)
	}
	return out, nil
}

func (a *App) findMaclawAppInstallRecord(appID string) (*maclawAppInstallRecord, error) {
	registry, err := a.readMaclawAppInstallRegistry()
	if err != nil {
		return nil, err
	}
	appID = strings.TrimSpace(appID)
	for i := range registry.Installs {
		if strings.EqualFold(strings.TrimSpace(registry.Installs[i].AppID), appID) {
			record := registry.Installs[i]
			return &record, nil
		}
	}
	return nil, nil
}

func parseMaclawAppInstallEntries(packageJSON string) ([]parsedMaclawAppEntry, error) {
	if strings.TrimSpace(packageJSON) == "" {
		return nil, fmt.Errorf("maclaw app package is empty")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &doc); err != nil {
		return nil, fmt.Errorf("decode maclaw app package: %w", err)
	}
	schema := stringMapValue(doc, "schema")
	switch schema {
	case "maclaw.app.pack.v1":
		return parseMaclawAppPackageEntriesFromMap(doc, true)
	case "maclaw.app.v1":
		entry, err := parseMaclawAppEntryFromMap(doc, "maclaw app", map[string]struct{}{})
		if err != nil {
			return nil, err
		}
		return []parsedMaclawAppEntry{entry}, nil
	default:
		return nil, fmt.Errorf("maclaw app package schema must be maclaw.app.v1 or maclaw.app.pack.v1")
	}
}

func maclawAppInstalledSkillMatch(index map[string]NLSkillDefinition, dep maclawAppInstallPlanDependency) (NLSkillDefinition, bool) {
	// Candidate IDs are already CollectSkillIdentityKeys-normalized map keys.
	for _, key := range maclawAppInstalledSkillCandidateIDs(dep) {
		if match, ok := index[key]; ok {
			return match, true
		}
	}
	return NLSkillDefinition{}, false
}

func maclawAppInstalledSkillCandidateIDs(dep maclawAppInstallPlanDependency) []string {
	candidates := []string{dep.ID, dep.SkillID, dep.InstallRefTarget, dep.RuntimeSkillRef, dep.InstalledName}
	if strings.TrimSpace(dep.InstallRefTarget) == "" {
		_, target, _, status, _ := maclawAppParseDependencyInstallRef(dep)
		if status == "ok" && target != "" {
			candidates = append(candidates, target)
		}
	}
	candidates = append(candidates, dep.CanonicalID)
	candidates = append(candidates, dep.Aliases...)
	if resolved, ok := maclawAppImplicitHubSkillResolution(dep); ok {
		candidates = append(candidates, resolved.Target)
		candidates = append(candidates, resolved.LocalNames...)
		candidates = append(candidates, resolved.Aliases...)
	}
	if ref := strings.TrimSpace(dep.InstallRef); ref != "" && !strings.Contains(ref, "://") && !strings.HasPrefix(ref, "{") {
		candidates = append(candidates, ref)
	}
	// CollectSkillIdentityKeys strips @version and adds underscore-normalized forms.
	return corelib.CollectSkillIdentityKeys(candidates...)
}

func applyMaclawAppInstalledSkillDependency(dep *maclawAppInstallPlanDependency, match NLSkillDefinition) {
	dep.Installed = true
	dep.InstalledName = match.Name
	// Bind declared app dependency id to stable package identity for run.
	if hub := strings.TrimSpace(match.HubSkillID); hub != "" {
		if strings.TrimSpace(dep.CanonicalID) == "" {
			dep.CanonicalID = hub
		}
		if strings.TrimSpace(dep.SkillID) == "" {
			dep.SkillID = firstNonEmpty(match.SkillID, hub)
		}
	} else if sid := strings.TrimSpace(match.SkillID); sid != "" && strings.TrimSpace(dep.SkillID) == "" {
		dep.SkillID = sid
	}
	dep.RuntimeSkillRef = match.PreferredRuntimeSkillRef()
	dep.RequiredVersion = strings.TrimSpace(dep.Version)
	dep.InstalledVersion = strings.TrimSpace(match.HubVersion)
	dep.InstalledDir = match.SkillDir
	dep.InstalledStatus, dep.Health = maclawAppInstalledSkillStatus(match)
	if maclawAppDependencyVersionMismatch(*dep) {
		dep.VersionStatus = "mismatch"
		dep.Health = "version_mismatch"
		if dep.Required {
			dep.Action = "blocked"
			dep.Message = fmt.Sprintf("required skill dependency version %s is installed, but %s is required", dep.InstalledVersion, dep.RequiredVersion)
			return
		}
		dep.Action = "optional_unhealthy"
		dep.Message = fmt.Sprintf("optional skill dependency version %s is installed, but %s is required", dep.InstalledVersion, dep.RequiredVersion)
		return
	}
	dep.VersionStatus = maclawAppDependencyVersionStatus(*dep)
	if maclawAppDependencyIsReady(*dep) {
		dep.Action = "skip"
		dep.Message = "installed locally"
		return
	}
	if dep.Required {
		dep.Action = "blocked"
		dep.Message = maclawAppDependencyInactiveMessage(*dep, true)
		return
	}
	dep.Action = "optional_unhealthy"
	dep.Message = maclawAppDependencyInactiveMessage(*dep, false)
}

func maclawAppInstalledSkillStatus(match NLSkillDefinition) (string, string) {
	status := strings.TrimSpace(match.Status)
	switch normalizeSkillEntryStatus(status) {
	case skillEntryStatusActive:
		return string(skillEntryStatusActive), "ready"
	case skillEntryStatusDisabled:
		return string(skillEntryStatusDisabled), string(skillEntryStatusDisabled)
	case skillEntryStatusNeedsSetup:
		return string(skillEntryStatusNeedsSetup), string(skillEntryStatusNeedsSetup)
	default:
		if status == "" {
			status = "unknown"
		}
		return status, "unknown"
	}
}

func maclawAppInstallPlanDependencyMergeKey(dep maclawAppInstallPlanDependency) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(dep.ID)),
		strings.ToLower(strings.TrimSpace(dep.Version)),
		strings.ToLower(strings.TrimSpace(dep.Source)),
		strings.ToLower(strings.TrimSpace(dep.InstallRef)),
		strings.ToLower(strings.TrimSpace(dep.CanonicalID)),
		strconv.FormatBool(dep.Required),
	}, "\x1f")
}

func maclawAppInstallPlanBlockingDependencySummary(plan maclawAppInstallPlan) string {
	parts := make([]string, 0, 3)
	for _, dep := range plan.Dependencies {
		if !maclawAppDependencyBlocksInstall(dep) {
			continue
		}
		id := strings.TrimSpace(dep.ID)
		if id == "" {
			id = "dependency"
		}
		stage := firstNonEmptyMaclawAppString(dep.InstallErrorStage, dep.PreflightStage, dep.IntegrityStage, dep.InstallRefKind, dep.Source)
		code := firstNonEmptyMaclawAppString(dep.InstallErrorCode, dep.PreflightCode, dep.IntegrityCode, dep.InstallRefStatus, dep.Action, dep.Health)
		detail := firstNonEmptyMaclawAppString(dep.InstallErrorDetail, dep.PreflightMessage, dep.IntegrityMessage, dep.InstallRefMessage, dep.Message)
		fields := []string{id}
		if code != "" {
			fields = append(fields, code)
		}
		if stage != "" {
			fields = append(fields, "at "+stage)
		}
		if detail != "" {
			fields = append(fields, detail)
		}
		parts = append(parts, strings.Join(fields, ": "))
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, "; ")
}

func maclawAppInstallVersionSnapshotsByApp(entries []parsedMaclawAppEntry) map[string]maclawAppInstallVersionSnapshot {
	out := map[string]maclawAppInstallVersionSnapshot{}
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		out[entry.ID] = maclawAppInstallVersionSnapshotForEntry(entry)
	}
	return out
}

func maclawAppInstallEvidenceByApp(entries []parsedMaclawAppEntry, dependencies []maclawAppInstallPlanDependency, dataSrvRegistration map[string]any) map[string]any {
	out := map[string]any{}
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		governance := maclawAppGovernanceMetadataForEntry(entry)
		out[entry.ID] = compactPayload(map[string]interface{}{
			"version_snapshot":        maclawAppInstallVersionSnapshotForEntry(entry),
			"dependencies":            cloneMaclawAppPlanDependenciesForApp(dependencies, entry.ID),
			"has_missing_required":    hasMissingMaclawAppRequiredDependencyForApp(dependencies, entry.ID),
			"has_blocking_dependency": hasBlockingMaclawAppRequiredDependencyForApp(dependencies, entry.ID),
			"workspace_layout":        maclawAppWorkspaceLayoutMetadataForEntry(entry),
			"result_contract":         anyMap(governance["result_contract"]),
			"review_evidence":         maclawAppReviewEvidenceForEntry(entry),
			"submission":              maclawAppSubmissionMetadataForEntry(entry),
			"workflow_mapping":        maclawAppWorkflowMappingForEntry(entry),
			"workflow_contract":       maclawAppWorkflowContractForEntry(entry),
			"test_evidence":           anyMap(governance["test_evidence"]),
			"dependency_verification": maclawAppDependencyVerificationMetadataForEntry(entry, dependencies),
			"datasrv_registration":    maclawAppDataSrvRegistrationForApp(dataSrvRegistration, entry.ID),
		})
	}
	return out
}

func maclawAppInstallVersionSnapshotForEntry(entry parsedMaclawAppEntry) maclawAppInstallVersionSnapshot {
	snapshot := maclawAppInstallVersionSnapshot{
		AppEntryVersion: maclawAppVersionString(firstNonEmptyMaclawAppAny(entry.App["version"], entry.Entry["version"])),
	}
	if appSkill := maclawAppAppSkillBlockForEntry(entry); appSkill != nil {
		if id := maclawAppStringValue(appSkill, "id"); id != "" {
			skill := maclawAppInstallSkillVersionSnapshot{
				ID:      id,
				Version: maclawAppVersionString(firstNonEmptyMaclawAppAny(appSkill["version"], appSkill["version_constraint"], appSkill["versionConstraint"])),
				Kind:    firstNonEmptyMaclawAppString(maclawAppStringValue(appSkill, "kind"), "app_skill"),
				Source:  maclawAppStringValue(appSkill, "source"),
			}
			snapshot.AppSkill = &skill
		}
	}
	workflowSeen := map[string]struct{}{}
	addWorkflow := func(id, version, kind, source string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		key := strings.ToLower(id)
		if _, ok := workflowSeen[key]; ok {
			return
		}
		workflowSeen[key] = struct{}{}
		snapshot.WorkflowSkills = append(snapshot.WorkflowSkills, maclawAppInstallSkillVersionSnapshot{ID: id, Version: version, Kind: firstNonEmptyMaclawAppString(kind, "workflow_skill"), Source: source})
	}
	for _, binding := range maclawAppApprovalBindingMapsForEntry(entry) {
		workflowID := firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "workflowSkillId", "workflow_skill_id"), maclawAppStringValue(binding, "workflowId", "workflow_id"))
		workflowVersion := maclawAppVersionString(firstNonEmptyMaclawAppAny(binding["workflowVersion"], binding["workflow_version"]))
		datasrv := maclawAppDataSrvBlockForEntry(entry)
		snapshot.ApprovalBindings = append(snapshot.ApprovalBindings, maclawAppInstallApprovalBindingSnapshot{
			Event:           maclawAppStringValue(binding, "event"),
			DatasetID:       maclawAppStringValue(datasrv, "datasetID", "dataset_id", "dataset"),
			BlueprintID:     firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "blueprintID", "blueprint_id"), maclawAppBlueprintIDForEntry(entry)),
			ObjectRole:      maclawAppStringValue(binding, "objectRole", "object_role"),
			WorkflowSkillID: workflowID,
			WorkflowVersion: workflowVersion,
		})
		addWorkflow(workflowID, workflowVersion, "workflow_skill", maclawAppStringValue(binding, "source"))
	}
	for _, dep := range maclawAppDependenciesForEntry(entry) {
		if strings.TrimSpace(dep.Kind) == "workflow_skill" {
			addWorkflow(dep.ID, dep.Version, dep.Kind, dep.Source)
		}
	}
	return snapshot
}

func maclawAppInstallRefVersionSatisfiesDependency(dep maclawAppInstallPlanDependency) bool {
	required := strings.TrimSpace(dep.Version)
	installRefVersion := strings.TrimSpace(dep.InstallRefVersion)
	if required == "" || installRefVersion == "" {
		return required == installRefVersion
	}
	if _, target, version, ok := maclawAppParseSourceVersionKey(required); ok {
		if version == "" {
			return true
		}
		refTarget := strings.TrimSpace(dep.InstallRefTarget)
		if refTarget != "" && target != "" && !strings.EqualFold(refTarget, target) {
			return false
		}
		return maclawAppDependencyVersionSatisfied(version, installRefVersion)
	}
	return maclawAppDependencyVersionSatisfied(required, installRefVersion)
}

func maclawAppInstallRefSourceMatches(source, kind string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch source {
	case "", "local", "hub", "skillhub":
		return kind == "hub" || kind == "skillhub" || kind == "skill" || kind == "skills"
	case "market", "skillmarket", "hubcenter":
		return kind == "market" || kind == "skillmarket" || kind == "hubcenter"
	case "enterprise", "enterprise_hub":
		return kind == "enterprise" || kind == "enterprise_hub" || kind == "hub"
	case "github":
		return kind == "github"
	default:
		return false
	}
}

func maclawAppInstallSkillSource(source string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "local", "hub", "skillhub":
		return string(skillSearchSourceSkillHub), true
	case "market", "skillmarket", "hubcenter":
		return string(skillSearchSourceSkillMarket), true
	case "enterprise", "enterprise_hub":
		return string(skillSearchSourceEnterpriseHub), true
	case "github":
		return string(skillSearchSourceGitHub), true
	default:
		return "", false
	}
}

func (a *App) maclawAppInstallRegistryPath() string {
	return filepath.Join(a.GetDataDir(), "app_install_records.json")
}

func (a *App) readMaclawAppInstallRegistry() (maclawAppInstallRegistry, error) {
	maclawAppInstallRegistryMu.RLock()
	defer maclawAppInstallRegistryMu.RUnlock()
	return a.readMaclawAppInstallRegistryUnlocked()
}

func (a *App) readMaclawAppInstallRegistryUnlocked() (maclawAppInstallRegistry, error) {
	path := a.maclawAppInstallRegistryPath()
	registry := maclawAppInstallRegistry{Schema: "maclaw.app.installs.v1"}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return registry, nil
	}
	if err != nil {
		return registry, err
	}
	if len(data) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return registry, fmt.Errorf("decode maclaw app install registry: %w", err)
	}
	if registry.Schema == "" {
		registry.Schema = "maclaw.app.installs.v1"
	}
	return registry, nil
}

func (a *App) writeMaclawAppInstallRegistry(registry maclawAppInstallRegistry) error {
	maclawAppInstallRegistryMu.Lock()
	defer maclawAppInstallRegistryMu.Unlock()
	return a.writeMaclawAppInstallRegistryUnlocked(registry)
}

func (a *App) writeMaclawAppInstallRegistryUnlocked(registry maclawAppInstallRegistry) error {
	path := a.maclawAppInstallRegistryPath()
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}
