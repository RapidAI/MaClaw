package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func maclawAppResolvedDependenciesForSelectedEntries(raw any, entries []parsedMaclawAppEntry) []any {
	items := anySlice(raw)
	if len(items) == 0 || len(entries) == 0 {
		return nil
	}
	selectedAppIDs := map[string]struct{}{}
	selectedDependencyIDs := map[string]struct{}{}
	for _, entry := range entries {
		selectedAppIDs[strings.ToLower(strings.TrimSpace(entry.ID))] = struct{}{}
		for _, dep := range maclawAppDependenciesForEntry(entry) {
			if id := strings.ToLower(strings.TrimSpace(dep.ID)); id != "" {
				selectedDependencyIDs[id] = struct{}{}
			}
		}
	}
	filtered := make([]any, 0, len(items))
	for _, item := range items {
		m := anyMap(item)
		if m == nil {
			continue
		}
		appIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(m["app_ids"], m["appIDs"]))
		if len(appIDs) > 0 {
			for _, appID := range appIDs {
				if _, ok := selectedAppIDs[strings.ToLower(strings.TrimSpace(appID))]; ok {
					filtered = append(filtered, cloneMapAny(m))
					break
				}
			}
			continue
		}
		id := strings.ToLower(strings.TrimSpace(fmt.Sprint(m["id"])))
		if _, ok := selectedDependencyIDs[id]; ok {
			filtered = append(filtered, cloneMapAny(m))
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func maclawAppDependencyVersionStatus(dep maclawAppInstallPlanDependency) string {
	required := strings.TrimSpace(dep.RequiredVersion)
	if required == "" {
		required = strings.TrimSpace(dep.Version)
	}
	if required == "" {
		return ""
	}
	installed := strings.TrimSpace(dep.InstalledVersion)
	if installed == "" {
		return "unknown"
	}
	if maclawAppDependencyVersionSatisfied(required, installed) {
		return "matched"
	}
	return "mismatch"
}

// maclawAppLooksLikeSourceVersionKey reports whether v is a parseable hub
// source-version key (enterprise_hub|skillmarket|hubcenter:skill:target[@ver]).
// Uses the same parser as install-ref enrichment so detection and comparison agree.
func maclawAppLooksLikeSourceVersionKey(v string) bool {
	_, _, _, ok := maclawAppParseSourceVersionKey(v)
	return ok
}

func maclawAppNormalizeSourceVersionKey(v string) string {
	return strings.TrimSpace(strings.ToLower(v))
}

// maclawAppNormalizeSourceVersionKind collapses marketplace aliases so
// hubcenter/skillmarket keys for the same skill compare as one coordinate system.
func maclawAppNormalizeSourceVersionKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "hubcenter", "skillmarket", "market":
		return "skillmarket"
	case "enterprise", "enterprise_hub":
		return "enterprise_hub"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

// maclawAppSourceVersionKeysCompatible compares two hub source-version keys.
// Prefer the parsed form via maclawAppSourceVersionKeysCompatibleParts when
// both sides were already parsed by the caller.
func maclawAppSourceVersionKeysCompatible(required, installed string) bool {
	if maclawAppNormalizeSourceVersionKey(required) == maclawAppNormalizeSourceVersionKey(installed) {
		return true
	}
	reqKind, reqTarget, reqVer, reqOK := maclawAppParseSourceVersionKey(required)
	instKind, instTarget, instVer, instOK := maclawAppParseSourceVersionKey(installed)
	if !reqOK || !instOK {
		return false
	}
	return maclawAppSourceVersionKeysCompatibleParts(reqKind, reqTarget, reqVer, instKind, instTarget, instVer)
}

func maclawAppSourceVersionKeysCompatibleParts(reqKind, reqTarget, reqVer, instKind, instTarget, instVer string) bool {
	if maclawAppNormalizeSourceVersionKind(reqKind) != maclawAppNormalizeSourceVersionKind(instKind) ||
		!strings.EqualFold(strings.TrimSpace(reqTarget), strings.TrimSpace(instTarget)) {
		return false
	}
	return maclawAppRevisionTokensCompatible(reqVer, instVer)
}

// maclawAppDependencyVersionSatisfied reports whether an installed dependency
// version satisfies a required version. A plain required version (e.g. "1.0.0")
// is treated as a MINIMUM constraint following standard dependency semantics
// ("does not satisfy required version"): the installed version satisfies it when
// installed >= required. This avoids false mismatches from:
//   - cosmetic differences: "v1.0.0" vs "1.0.0", " 1.0.0 " vs "1.0.0"
//   - segment-count differences: "1.0" vs "1.0.0"
//   - newer compatible versions: installed "10" satisfies required "1.0.0"
//   - coordinate-system mix: app-declared semver "1.0.0" vs installed hub
//     content key "enterprise_hub:skill:id@hash" (identity already resolved)
//   - same hub key identity with newer semver @suffix (skillmarket-style keys)
//   - full content key vs bare digest of the same hash
//
// Constraint expressions (^, ~, >=, <, *, etc.) are not resolved here and are
// treated as satisfied, consistent with maclawAppWorkflowVersionMatches. When
// either version is not numeric-parseable, it falls back to a conservative
// normalized exact-match comparison.
//
// Each side is parsed at most once.
func maclawAppDependencyVersionSatisfied(required, installed string) bool {
	required = strings.TrimSpace(required)
	installed = strings.TrimSpace(installed)
	if required == "" || installed == "" {
		return required == installed
	}
	reqKind, reqTarget, reqVer, reqOK := maclawAppParseSourceVersionKey(required)
	instKind, instTarget, instVer, instOK := maclawAppParseSourceVersionKey(installed)
	switch {
	case reqOK && instOK:
		if maclawAppNormalizeSourceVersionKey(required) == maclawAppNormalizeSourceVersionKey(installed) {
			return true
		}
		return maclawAppSourceVersionKeysCompatibleParts(reqKind, reqTarget, reqVer, instKind, instTarget, instVer)
	case reqOK:
		return maclawAppCrossCoordinateVersionSatisfiedParts(reqVer, installed, true)
	case instOK:
		return maclawAppCrossCoordinateVersionSatisfiedParts(instVer, required, false)
	default:
		return maclawAppPlainVersionSatisfied(required, installed)
	}
}

func maclawAppDependencyVersionMismatch(dep maclawAppInstallPlanDependency) bool {
	return maclawAppDependencyVersionStatus(dep) == "mismatch"
}

func maclawAppDependencyIsReady(dep maclawAppInstallPlanDependency) bool {
	return dep.Installed && strings.TrimSpace(dep.Health) == "ready"
}

func maclawAppDependencyInactiveMessage(dep maclawAppInstallPlanDependency, required bool) string {
	status := strings.TrimSpace(dep.InstalledStatus)
	if status == "" {
		status = "unknown"
	}
	if required {
		return fmt.Sprintf("required skill dependency is installed but not active (status: %s)", status)
	}
	return fmt.Sprintf("optional skill dependency is installed but not active (status: %s)", status)
}

func maclawAppDependencyBlocksInstall(dep maclawAppInstallPlanDependency) bool {
	if !dep.Required {
		return false
	}
	action := strings.TrimSpace(dep.Action)
	if action == "blocked" || action == "failed" {
		return true
	}
	if !dep.Installed {
		return !maclawAppDependencyCanAttemptInstall(dep)
	}
	health := strings.TrimSpace(dep.Health)
	return health != "" && health != "ready"
}

func maclawAppDependencyRequiresGovernanceRepair(dep maclawAppInstallPlanDependency) bool {
	if !dep.Required {
		return false
	}
	if !dep.Installed {
		return true
	}
	health := strings.TrimSpace(dep.Health)
	return health != "" && health != "ready"
}

func maclawAppDependencyCanAttemptInstall(dep maclawAppInstallPlanDependency) bool {
	if !dep.Required || dep.Installed {
		return false
	}
	if dep.InstallRefStatus == "missing" || dep.InstallRefStatus == "invalid" || dep.PreflightStatus == "blocked" {
		return false
	}
	if _, ok := maclawAppDependencyInstallerSource(dep); !ok {
		return false
	}
	return true
}

func (plan *maclawAppInstallPlan) refreshMaclawAppDependencyFlags() {
	plan.HasMissingRequired = false
	plan.HasBlockingDependency = false
	for _, dep := range plan.Dependencies {
		if dep.Required && !dep.Installed {
			plan.HasMissingRequired = true
		}
		if maclawAppDependencyBlocksInstall(dep) {
			plan.HasBlockingDependency = true
		}
	}
}

func maclawAppDependencyVerificationMetadataForEntry(entry parsedMaclawAppEntry, dependencies []maclawAppInstallPlanDependency) map[string]interface{} {
	appDependencies := cloneMaclawAppPlanDependenciesForApp(dependencies, entry.ID)
	if governance := maclawAppGovernanceMetadataForEntry(entry); governance != nil {
		if verification := anyMap(governance["dependency_verification"]); verification != nil {
			out := cloneMapAny(verification)
			if out["verified_at"] == nil {
				if verifiedAt := firstNonEmptyMaclawAppAny(out["verifiedAt"], out["verified_at"]); verifiedAt != nil {
					out["verified_at"] = verifiedAt
				}
			}
			if out["verifiedAt"] == nil {
				if verifiedAt := firstNonEmptyMaclawAppAny(out["verified_at"], out["verifiedAt"]); verifiedAt != nil {
					out["verifiedAt"] = verifiedAt
				}
			}
			if len(appDependencies) > 0 {
				out["dependencies"] = maclawAppMergedDependencyVerificationItems(anySlice(verification["dependencies"]), appDependencies)
				out["dependency_count"] = len(appDependencies)
				out["dependencyCount"] = len(appDependencies)
				out["has_missing_required"] = hasMissingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
				out["hasMissingRequired"] = hasMissingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
				out["has_blocking_dependency"] = hasBlockingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
				out["hasBlockingDependency"] = hasBlockingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
				if trace := maclawAppDependencyInstallTraceSummary(appDependencies); trace != nil {
					out["install_trace"] = trace
					out["installTrace"] = trace
				}
			}
			return compactPayload(out)
		}
	}
	if len(appDependencies) == 0 {
		return nil
	}
	return compactPayload(map[string]interface{}{
		"schema":                  "maclaw.app.install_plan.v1",
		"verified_at":             time.Now().UTC().Format(time.RFC3339),
		"app_count":               1,
		"dependency_count":        len(appDependencies),
		"has_missing_required":    hasMissingMaclawAppRequiredDependencyForApp(dependencies, entry.ID),
		"has_blocking_dependency": hasBlockingMaclawAppRequiredDependencyForApp(dependencies, entry.ID),
		"install_trace":           maclawAppDependencyInstallTraceSummary(appDependencies),
		"dependencies":            appDependencies,
	})
}

func maclawAppDependencyInstallTraceSummary(dependencies []maclawAppInstallPlanDependency) map[string]any {
	if len(dependencies) == 0 {
		return nil
	}
	summary := map[string]any{
		"schema":                       "maclaw.app.dependency_install_trace.v1",
		"dependency_count":             len(dependencies),
		"dependencyCount":              len(dependencies),
		"preflight_checked_count":      0,
		"preflightCheckedCount":        0,
		"preflight_ready_count":        0,
		"preflightReadyCount":          0,
		"preflight_failed_count":       0,
		"preflightFailedCount":         0,
		"integrity_checked_count":      0,
		"integrityCheckedCount":        0,
		"integrity_ready_count":        0,
		"integrityReadyCount":          0,
		"integrity_failed_count":       0,
		"integrityFailedCount":         0,
		"download_available_count":     0,
		"downloadAvailableCount":       0,
		"signature_available_count":    0,
		"signatureAvailableCount":      0,
		"install_error_count":          0,
		"installErrorCount":            0,
		"required_install_error_count": 0,
		"requiredInstallErrorCount":    0,
		"ok":                           true,
		"traceOk":                      true,
	}
	increment := func(snake, camel string) {
		if current, ok := summary[snake].(int); ok {
			summary[snake] = current + 1
		}
		if current, ok := summary[camel].(int); ok {
			summary[camel] = current + 1
		}
	}
	markFailed := func() {
		summary["ok"] = false
		summary["traceOk"] = false
	}
	for _, dep := range dependencies {
		if strings.TrimSpace(dep.PreflightStatus) != "" || strings.TrimSpace(dep.PreflightCode) != "" || strings.TrimSpace(dep.PreflightStage) != "" {
			increment("preflight_checked_count", "preflightCheckedCount")
		}
		if maclawAppDependencyDiagnosticStatusReady(dep.PreflightStatus) {
			increment("preflight_ready_count", "preflightReadyCount")
		}
		if maclawAppDependencyDiagnosticStatusFailed(dep.PreflightStatus) || maclawAppDependencyDiagnosticCodeFailed(dep.PreflightCode) {
			increment("preflight_failed_count", "preflightFailedCount")
			markFailed()
		}
		if strings.TrimSpace(dep.IntegrityStatus) != "" || strings.TrimSpace(dep.IntegrityCode) != "" || strings.TrimSpace(dep.IntegrityStage) != "" {
			increment("integrity_checked_count", "integrityCheckedCount")
		}
		if maclawAppDependencyDiagnosticStatusReady(dep.IntegrityStatus) {
			increment("integrity_ready_count", "integrityReadyCount")
		}
		if maclawAppDependencyDiagnosticStatusFailed(dep.IntegrityStatus) || maclawAppDependencyDiagnosticCodeFailed(dep.IntegrityCode) {
			increment("integrity_failed_count", "integrityFailedCount")
			markFailed()
		}
		if strings.TrimSpace(dep.PackageDownloadURL) != "" {
			increment("download_available_count", "downloadAvailableCount")
		}
		if strings.TrimSpace(dep.PackageSignature) != "" {
			increment("signature_available_count", "signatureAvailableCount")
		}
		if strings.TrimSpace(dep.InstallErrorCode) != "" || strings.TrimSpace(dep.InstallErrorStage) != "" || strings.TrimSpace(dep.InstallErrorDetail) != "" {
			increment("install_error_count", "installErrorCount")
			if dep.Required {
				increment("required_install_error_count", "requiredInstallErrorCount")
			}
			markFailed()
		}
	}
	return compactPayload(summary)
}

func maclawAppDependencyDiagnosticStatusReady(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "passed", "ready", "success", "succeeded", "trusted", "verified":
		return true
	default:
		return false
	}
}

func maclawAppDependencyDiagnosticStatusFailed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "blocked", "denied", "disabled", "error", "failed", "failure", "invalid", "missing", "mismatch", "needs_review", "rejected", "tampered", "unhealthy", "untrusted":
		return true
	default:
		return false
	}
}

func maclawAppDependencyDiagnosticCodeFailed(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return false
	}
	for _, marker := range []string{"blocked", "denied", "error", "fail", "invalid", "missing", "mismatch", "needs_review", "rejected", "review_required", "tampered", "untrusted"} {
		if strings.Contains(code, marker) {
			return true
		}
	}
	return false
}

func maclawAppMergedDependencyVerificationItems(existing []any, appDependencies []maclawAppInstallPlanDependency) []any {
	existingByKey := map[string]map[string]any{}
	for _, raw := range existing {
		item := anyMap(raw)
		if item == nil {
			continue
		}
		id := strings.ToLower(maclawAppStringValue(item, "id"))
		kind := strings.ToLower(maclawAppStringValue(item, "kind"))
		if id == "" {
			continue
		}
		existingByKey[kind+":"+id] = item
		existingByKey[id] = item
	}
	out := make([]any, 0, len(appDependencies))
	for _, dep := range appDependencies {
		id := strings.ToLower(strings.TrimSpace(dep.ID))
		kind := strings.ToLower(strings.TrimSpace(dep.Kind))
		item := cloneMapAny(existingByKey[kind+":"+id])
		if item == nil {
			item = cloneMapAny(existingByKey[id])
		}
		if item == nil {
			item = map[string]any{}
		}
		item["id"] = dep.ID
		if dep.Version != "" {
			item["version"] = dep.Version
		}
		if dep.Kind != "" {
			item["kind"] = dep.Kind
		}
		item["required"] = dep.Required
		if dep.Source != "" {
			item["source"] = dep.Source
		}
		if dep.InstallRef != "" {
			item["install_ref"] = dep.InstallRef
		}
		if dep.CanonicalID != "" {
			item["canonical_id"] = dep.CanonicalID
		}
		if len(dep.Aliases) > 0 {
			item["aliases"] = append([]string(nil), dep.Aliases...)
		}
		if dep.InstallRefKind != "" {
			item["install_ref_kind"] = dep.InstallRefKind
		}
		if dep.InstallRefTarget != "" {
			item["install_ref_target"] = dep.InstallRefTarget
		}
		if dep.InstallRefVersion != "" {
			item["install_ref_version"] = dep.InstallRefVersion
		}
		if dep.InstallRefStatus != "" {
			item["install_ref_status"] = dep.InstallRefStatus
		}
		if dep.InstallRefMessage != "" {
			item["install_ref_message"] = dep.InstallRefMessage
		}
		if dep.PreflightStatus != "" {
			item["preflight_status"] = dep.PreflightStatus
		}
		if dep.PreflightCode != "" {
			item["preflight_code"] = dep.PreflightCode
		}
		if dep.PreflightStage != "" {
			item["preflight_stage"] = dep.PreflightStage
		}
		if dep.PreflightMessage != "" {
			item["preflight_message"] = dep.PreflightMessage
		}
		if dep.PackageSHA256 != "" {
			item["package_sha256"] = dep.PackageSHA256
		}
		if dep.PackageChecksum != "" {
			item["package_checksum"] = dep.PackageChecksum
		}
		if dep.PackageSignature != "" {
			item["package_signature"] = dep.PackageSignature
		}
		if dep.PackageDownloadURL != "" {
			item["package_download_url"] = dep.PackageDownloadURL
		}
		if dep.DownloadNode != "" {
			item["download_node"] = dep.DownloadNode
		}
		if dep.ResolvedDownloadURL != "" {
			item["resolved_download_url"] = dep.ResolvedDownloadURL
		}
		if dep.IntegrityStatus != "" {
			item["integrity_status"] = dep.IntegrityStatus
		}
		if dep.IntegrityCode != "" {
			item["integrity_code"] = dep.IntegrityCode
		}
		if dep.IntegrityStage != "" {
			item["integrity_stage"] = dep.IntegrityStage
		}
		if dep.IntegrityMessage != "" {
			item["integrity_message"] = dep.IntegrityMessage
		}
		if dep.InstallErrorCode != "" {
			item["install_error_code"] = dep.InstallErrorCode
		}
		if dep.InstallErrorStage != "" {
			item["install_error_stage"] = dep.InstallErrorStage
		}
		if dep.InstallErrorDetail != "" {
			item["install_error_detail"] = dep.InstallErrorDetail
		}
		if len(dep.AppIDs) > 0 {
			item["app_ids"] = append([]string(nil), dep.AppIDs...)
		}
		item["installed"] = dep.Installed
		if dep.InstalledName != "" {
			item["installed_name"] = dep.InstalledName
		}
		if dep.InstalledVersion != "" {
			item["installed_version"] = dep.InstalledVersion
		}
		if dep.RequiredVersion != "" {
			item["required_version"] = dep.RequiredVersion
		}
		if dep.VersionStatus != "" {
			item["version_status"] = dep.VersionStatus
		}
		if dep.InstalledDir != "" {
			item["installed_dir"] = dep.InstalledDir
		}
		if dep.InstalledStatus != "" {
			item["installed_status"] = dep.InstalledStatus
		}
		if dep.Health != "" {
			item["health"] = dep.Health
		}
		if dep.Action != "" {
			item["action"] = dep.Action
		}
		if dep.Message != "" {
			item["message"] = dep.Message
		}
		out = append(out, compactPayload(item))
	}
	return out
}

func maclawAppDefaultDependencySourceForEntry(entry parsedMaclawAppEntry) string {
	for _, holder := range []map[string]any{entry.App, anyMap(entry.App["binding"]), entry.Entry} {
		if holder == nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(firstNonEmpty(
			stringMapValue(holder, "dependency_source"),
			stringMapValue(holder, "dependencySource"),
			stringMapValue(holder, "install_source"),
			stringMapValue(holder, "installSource"),
			stringMapValue(holder, "marketInstallSource"),
			stringMapValue(holder, "market_install_source"),
			stringMapValue(holder, "source"),
		)))
		switch source {
		case "market", "skillmarket", "hubcenter":
			return "skillmarket"
		case "enterprise", "enterprise_hub":
			return "enterprise_hub"
		}
	}
	return "hub"
}

func maclawAppNormalizeDependencySourceForEntry(dep maclawAppInstallPlanDependency, defaultSource string) string {
	source := strings.ToLower(strings.TrimSpace(dep.Source))
	defaultSource = strings.ToLower(strings.TrimSpace(defaultSource))
	if defaultSource == "" {
		defaultSource = "hub"
	}
	switch source {
	case "":
		return defaultSource
	case "local":
		return "local"
	case "hub", "skillhub":
		if defaultSource == "enterprise_hub" {
			return defaultSource
		}
		if defaultSource == "skillmarket" && maclawAppHubDependencyShouldUseMarketDefault(dep) {
			return "skillmarket"
		}
		return "hub"
	case "market", "skillmarket", "hubcenter":
		return "skillmarket"
	case "enterprise", "enterprise_hub":
		return "enterprise_hub"
	default:
		return source
	}
}

func maclawAppHubDependencyShouldUseMarketDefault(dep maclawAppInstallPlanDependency) bool {
	ref := strings.ToLower(strings.TrimSpace(dep.InstallRef))
	if strings.HasPrefix(ref, "skillmarket://") || strings.HasPrefix(ref, "hubcenter://") {
		return true
	}
	if _, ok := maclawAppImplicitHubSkillResolution(dep); ok {
		return true
	}
	return false
}

func maclawAppDependencyInstallRef(values map[string]any) string {
	if values == nil {
		return ""
	}
	for _, key := range []string{"install_ref", "installRef", "capability_id", "capabilityID", "hub_skill_id", "hubSkillID", "skill_id", "skillID", "raw_url", "rawURL", "repo_url", "repoURL"} {
		if value := stringMapValue(values, key); value != "" {
			return value
		}
	}
	return ""
}

func maclawAppDependencyCanonicalID(values map[string]any) string {
	if values == nil {
		return ""
	}
	for _, key := range []string{"canonical_id", "canonicalID", "install_target", "installTarget", "target_id", "targetID"} {
		if value := stringMapValue(values, key); value != "" {
			return value
		}
	}
	return ""
}

func maclawAppDependencyAliases(values map[string]any) []string {
	if values == nil {
		return nil
	}
	aliases := append([]string{}, maclawAppStringSliceFromAny(values["aliases"])...)
	aliases = append(aliases, maclawAppStringSliceFromAny(values["install_aliases"])...)
	aliases = append(aliases, maclawAppStringSliceFromAny(values["installAliases"])...)
	return appendMaclawAppUniqueStrings(nil, aliases...)
}

func maclawAppDependencyVerificationReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	declared := maclawAppDependenciesForEntry(entry)
	if len(declared) == 0 {
		return nil
	}
	if governance == nil {
		return &maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification", Severity: "error", Message: "missing dependency verification", Suggestion: "run dependency verification before submitting to the capability market"}
	}
	verification := anyMap(governance["dependencyVerification"])
	if verification == nil {
		verification = anyMap(governance["dependency_verification"])
	}
	if verification == nil {
		return &maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification", Severity: "error", Message: "missing dependency verification", Suggestion: "run dependency verification before submitting to the capability market"}
	}
	if schema := strings.TrimSpace(maclawAppStringValue(verification, "schema")); schema != "" && schema != "maclaw.app.install_plan.v1" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification", Severity: "error", Message: "invalid dependency verification schema", Suggestion: "attach the backend PlanMaclawAppInstall result to governance.dependencyVerification"}
	}
	if maclawAppDependencyVerificationBlocksEntry(verification, entry.ID) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification", Severity: "error", Message: "required dependency is missing or blocked", Suggestion: "install or enable required Skill dependencies before submitting"}
	}
	if issue := maclawAppDependencyVerificationIssueFromEvidence(verification, appPath, "workflow", "approval workflow contract verification failed", "refresh dependency verification and align the approval workflow Skill contract before submitting"); issue != nil {
		return issue
	}
	if issue := maclawAppDependencyVerificationIssueFromEvidence(verification, appPath, "governance", "dependency governance review failed", "resolve app governance review issues and refresh dependency verification before submitting"); issue != nil {
		return issue
	}
	verified := map[string]map[string]any{}
	for _, item := range anySlice(verification["dependencies"]) {
		dep := anyMap(item)
		id := strings.ToLower(strings.TrimSpace(maclawAppStringValue(dep, "id")))
		if id != "" {
			verified[id] = dep
		}
	}
	for _, dep := range declared {
		id := strings.ToLower(strings.TrimSpace(dep.ID))
		if id == "" {
			continue
		}
		verifiedDep := verified[id]
		if verifiedDep == nil {
			return &maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification", Severity: "error", Message: "dependency verification is missing declared dependency: " + dep.ID, Suggestion: "refresh dependency verification before submitting"}
		}
		if dep.Required && maclawAppVerifiedDependencyBlocked(verifiedDep) {
			return &maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification", Severity: "error", Message: "required dependency is not ready: " + dep.ID, Suggestion: "install or enable the required Skill dependency before submitting"}
		}
	}
	return nil
}

func maclawAppDependencyVerificationBlocksEntry(verification map[string]any, appID string) bool {
	if verification == nil {
		return false
	}
	dependencies := anySlice(verification["dependencies"])
	if len(dependencies) == 0 {
		return maclawAppBoolValue(verification, "hasMissingRequired", "has_missing_required", "hasBlockingDependency", "has_blocking_dependency")
	}
	for _, item := range dependencies {
		dep := anyMap(item)
		if dep == nil || !maclawAppDependencyEvidenceMatchesAppID(dep, appID) {
			continue
		}
		if maclawAppVerifiedDependencyBlocked(dep) {
			return true
		}
	}
	return false
}

func maclawAppDependencyEvidenceMatchesAppID(dep map[string]any, appID string) bool {
	appIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(dep["app_ids"], dep["appIDs"], dep["AppIDs"]))
	if len(appIDs) == 0 {
		return true
	}
	for _, candidate := range appIDs {
		if maclawAppIDsMatch(candidate, appID) {
			return true
		}
	}
	return false
}

func maclawAppIDAliases(id string) map[string]bool {
	id = strings.TrimSpace(id)
	aliases := map[string]bool{}
	if id == "" {
		return aliases
	}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			aliases[strings.ToLower(value)] = true
		}
	}
	add(id)
	if strings.HasPrefix(id, "market-") {
		add(strings.TrimPrefix(id, "market-"))
	} else {
		add("market-" + id)
	}
	if strings.HasPrefix(id, "datasrv-installed-") {
		add(strings.TrimPrefix(id, "datasrv-installed-"))
	} else {
		add("datasrv-installed-" + id)
	}
	return aliases
}

func maclawAppDependencyVerificationIssueFromEvidence(verification map[string]any, appPath, kind, fallbackMessage, fallbackSuggestion string) *maclawAppReviewIssue {
	if verification == nil {
		return nil
	}
	var flagged bool
	var rawIssues any
	pathSuffix := ".workflowContractIssues"
	if kind == "workflow" {
		flagged = maclawAppBoolValue(verification, "hasWorkflowContractIssue", "has_workflow_contract_issue")
		rawIssues = firstNonEmptyMaclawAppAny(verification["workflowContractIssues"], verification["workflow_contract_issues"])
	} else {
		flagged = maclawAppBoolValue(verification, "hasGovernanceReviewIssue", "has_governance_review_issue")
		rawIssues = firstNonEmptyMaclawAppAny(verification["governanceReviewIssues"], verification["governance_review_issues"])
		pathSuffix = ".governanceReviewIssues"
	}
	issues := maclawAppReviewIssuesFromAny(rawIssues)
	if len(issues) > 0 {
		filtered := make([]maclawAppReviewIssue, 0, len(issues))
		for _, issue := range issues {
			issuePath := strings.TrimSpace(issue.Path)
			if issuePath == "" || strings.HasPrefix(issuePath, appPath) || !strings.HasPrefix(issuePath, "apps[") {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
		if len(issues) == 0 {
			return nil
		}
	}
	if !flagged && len(issues) == 0 {
		return nil
	}
	issue := maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification" + pathSuffix, Severity: "error", Message: fallbackMessage, Suggestion: fallbackSuggestion}
	if len(issues) > 0 {
		first := issues[0]
		if strings.TrimSpace(first.Message) != "" {
			issue.Message = fallbackMessage + ": " + strings.TrimSpace(first.Message)
		}
		if strings.TrimSpace(first.Suggestion) != "" {
			issue.Suggestion = strings.TrimSpace(first.Suggestion)
		}
		issue.Metadata = cloneMapAny(first.Metadata)
	}
	return &issue
}

func maclawAppVerifiedDependencyBlocked(dep map[string]any) bool {
	if dep == nil {
		return true
	}
	if installed, ok := dep["installed"].(bool); ok && !installed {
		return true
	}
	health := strings.ToLower(strings.TrimSpace(maclawAppStringValue(dep, "health")))
	action := strings.ToLower(strings.TrimSpace(maclawAppStringValue(dep, "action")))
	status := strings.ToLower(strings.TrimSpace(maclawAppStringValue(dep, "installed_status", "installedStatus")))
	preflightStatus := maclawAppStringValue(dep, "preflight_status", "preflightStatus")
	preflightCode := maclawAppStringValue(dep, "preflight_code", "preflightCode")
	integrityStatus := maclawAppStringValue(dep, "integrity_status", "integrityStatus")
	integrityCode := maclawAppStringValue(dep, "integrity_code", "integrityCode")
	if health == "missing" || health == "disabled" || health == "needs_setup" || health == "needs_review" || health == "unhealthy" {
		return true
	}
	if action == "blocked" || action == "failed" || action == "needs_review" || action == "optional_unhealthy" {
		return true
	}
	if status == "disabled" || status == "error" || status == "failed" || status == "needs_review" {
		return true
	}
	if strings.TrimSpace(maclawAppStringValue(dep, "install_error_code", "installErrorCode", "install_error_stage", "installErrorStage", "install_error_detail", "installErrorDetail")) != "" {
		return true
	}
	if maclawAppDependencyDiagnosticStatusFailed(preflightStatus) || maclawAppDependencyDiagnosticCodeFailed(preflightCode) {
		return true
	}
	if maclawAppDependencyDiagnosticStatusFailed(integrityStatus) || maclawAppDependencyDiagnosticCodeFailed(integrityCode) {
		return true
	}
	return false
}

func maclawAppAuthoritativeDependencyReviewIssues(plan maclawAppInstallPlan, existing []maclawAppReviewIssue) []maclawAppReviewIssue {
	if len(plan.Dependencies) == 0 {
		return nil
	}
	appIndexByID := make(map[string]int, len(plan.Apps))
	for i, app := range plan.Apps {
		appIndexByID[strings.ToLower(strings.TrimSpace(app.ID))] = i
	}
	hasExistingDependencyIssue := func(path string) bool {
		for _, issue := range existing {
			if strings.TrimSpace(issue.Path) == path {
				return true
			}
		}
		return false
	}
	issues := []maclawAppReviewIssue{}
	for _, dep := range plan.Dependencies {
		if !maclawAppDependencyRequiresGovernanceRepair(dep) {
			continue
		}
		appIDs := dep.AppIDs
		if len(appIDs) == 0 && len(plan.Apps) == 1 {
			appIDs = []string{plan.Apps[0].ID}
		}
		for _, appID := range appIDs {
			idx, ok := appIndexByID[strings.ToLower(strings.TrimSpace(appID))]
			if !ok {
				continue
			}
			path := fmt.Sprintf("apps[%d].app.governance.dependencyVerification", idx)
			if hasExistingDependencyIssue(path) {
				continue
			}
			issues = append(issues, maclawAppReviewIssue{
				Path:       path,
				Severity:   "error",
				Message:    "authoritative dependency plan found required dependency not ready: " + dep.ID,
				Suggestion: "run dependency verification again and install or enable required Skill dependencies before submitting",
			})
		}
	}
	return issues
}

// enrichDependenciesWithHubSkillID stamps each dependency with the HubSkillID
// and source metadata from the locally installed skill. This enrichment runs at
// package submit time so the persisted package carries deterministic remote
// identifiers — receivers install by ID, not by keyword search.
func (a *App) enrichDependenciesWithHubSkillID(deps []maclawAppInstallPlanDependency) {
	if len(deps) == 0 {
		return
	}
	installed := a.installedMaclawAppSkillIndex()
	for i := range deps {
		dep := &deps[i]
		// Match by full candidate set (id / alias / canonical / install_ref target),
		// not only dep.ID — otherwise late stamps miss HubSkillID after MarkUploaded.
		match, found := maclawAppInstalledSkillMatch(installed, *dep)
		if !found {
			continue
		}
		hubID := strings.TrimSpace(match.HubSkillID)
		if hubID == "" {
			continue
		}
		ref := strings.TrimSpace(dep.InstallRef)
		refKind := strings.ToLower(strings.TrimSpace(dep.InstallRefKind))
		refTarget := strings.TrimSpace(dep.InstallRefTarget)
		shouldStampHubID := ref == "" ||
			refKind == "" ||
			refKind == "hub" ||
			refKind == "skillhub" ||
			strings.EqualFold(ref, dep.ID) ||
			(refTarget != "" && strings.EqualFold(ref, refTarget))
		// Stamp InstallRef with HubSkillID for precision remote install. This
		// also upgrades implicit alias refs like rapidocr to the concrete UUID.
		if shouldStampHubID {
			dep.InstallRef = hubID
			dep.InstallRefTarget = hubID
			dep.InstallRefStatus = "ok"
		}
		// Stamp SkillID (publisher.skill-name) for deterministic dependency resolution.
		if dep.SkillID == "" && strings.TrimSpace(match.SkillID) != "" {
			dep.SkillID = match.SkillID
		}
		matchSource := strings.ToLower(strings.TrimSpace(match.Source))
		// Upgrade source from "local"/empty to a resolvable remote source.
		src := strings.ToLower(strings.TrimSpace(dep.Source))
		if src == "" || src == "local" {
			dep.Source = "hub"
		}
		switch matchSource {
		case "skillmarket", "market", "hubcenter":
			if src == "" || src == "local" || src == "hub" || src == "skillhub" {
				dep.Source = "skillmarket"
			}
			if shouldStampHubID || refKind == "" || refKind == "hub" || refKind == "skillhub" {
				dep.InstallRefKind = "skillmarket"
			}
		case "hub", "skillhub":
			if shouldStampHubID && refKind == "" {
				dep.InstallRefKind = "hub"
			}
		case "enterprise", "enterprise_hub":
			if src == "" || src == "local" {
				dep.Source = "enterprise_hub"
			}
			if shouldStampHubID && refKind == "" {
				dep.InstallRefKind = "enterprise_hub"
			}
		}
		// Record installed version for the receiver's compatibility check.
		if dep.Version == "" && strings.TrimSpace(match.HubVersion) != "" {
			dep.Version = strings.TrimSpace(match.HubVersion)
		}
	}
}

// maclawAppSerializableResolvedDeps converts enriched dependencies into a
// JSON-serializable slice for embedding in the package as "resolved_dependencies".
// Only entries that have a non-empty InstallRef (i.e., were enriched) are included.
func maclawAppSerializableResolvedDeps(deps []maclawAppInstallPlanDependency) []map[string]any {
	var out []map[string]any
	for _, dep := range deps {
		ref := strings.TrimSpace(dep.InstallRef)
		if ref == "" {
			continue
		}
		entry := map[string]any{
			"id":          dep.ID,
			"install_ref": ref,
			"source":      dep.Source,
			"kind":        dep.Kind,
			"required":    dep.Required,
		}
		if len(dep.AppIDs) > 0 {
			entry["app_ids"] = append([]string(nil), dep.AppIDs...)
		}
		if dep.Version != "" {
			entry["version"] = dep.Version
		}
		if dep.CanonicalID != "" {
			entry["canonical_id"] = dep.CanonicalID
		}
		if len(dep.Aliases) > 0 {
			entry["aliases"] = append([]string(nil), dep.Aliases...)
		}
		if dep.InstallRefKind != "" {
			entry["install_ref_kind"] = dep.InstallRefKind
		}
		if dep.InstallRefTarget != "" {
			entry["install_ref_target"] = dep.InstallRefTarget
		}
		if dep.InstallRefVersion != "" {
			entry["install_ref_version"] = dep.InstallRefVersion
		}
		if dep.PackageSHA256 != "" {
			entry["package_sha256"] = dep.PackageSHA256
		}
		if dep.PackageChecksum != "" {
			entry["package_checksum"] = dep.PackageChecksum
		}
		if dep.PackageSignature != "" {
			entry["package_signature"] = dep.PackageSignature
		}
		if dep.PackageDownloadURL != "" {
			entry["package_download_url"] = dep.PackageDownloadURL
		}
		if dep.DownloadNode != "" {
			entry["download_node"] = dep.DownloadNode
		}
		if dep.ResolvedDownloadURL != "" {
			entry["resolved_download_url"] = dep.ResolvedDownloadURL
		}
		out = append(out, entry)
	}
	return out
}

// applyResolvedDependenciesToPlan merges the "resolved_dependencies" field from
// an enriched package into the install plan. This gives receivers deterministic
// InstallRef and Source values without re-running the publisher's local enrichment.
//
// Reads from two locations (both written by enrichDependenciesWithHubSkillID):
//   - Package-level: installDoc["resolved_dependencies"] (local queue / direct install)
//   - Entry-level: installDoc["apps"][N]["resolved_dependencies"] (survives Hub round-trip)
func applySkillHubDownloadTraceToDependency(dep *maclawAppInstallPlanDependency, trace skillHubDownloadTrace) {
	if dep == nil {
		return
	}
	// download_node is only the node that actually served package bytes.
	// Preferred/sticky locators stay on package_download_url (and error preferred_node=).
	if node := strings.TrimSpace(trace.UsedBase); node != "" {
		dep.DownloadNode = node
	}
	if resolved := strings.TrimSpace(trace.ResolvedDownloadURL); resolved != "" {
		dep.ResolvedDownloadURL = resolved
	}
}

func maclawAppClassifyDependencyInstallError(dep maclawAppInstallPlanDependency, source string, err error) (code, stage, detail string) {
	detail = strings.TrimSpace(fmt.Sprint(err))
	if detail == "" {
		detail = "dependency install failed"
	}
	stage = maclawAppDependencyInstallStage(source)
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "policy") || strings.Contains(lower, "enterprise-only") || strings.Contains(lower, "non-enterprise"):
		code = "policy_rejected"
	case strings.Contains(lower, "checksum") || strings.Contains(lower, "signature") || strings.Contains(lower, "sha256") || strings.Contains(lower, "integrity"):
		code = "package_integrity_failed"
	case strings.Contains(lower, "not found") || strings.Contains(lower, "404") || strings.Contains(lower, "no such"):
		code = "not_found"
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "permission") || strings.Contains(lower, "denied") || strings.Contains(lower, "401") || strings.Contains(lower, "403"):
		code = "access_denied"
	case strings.Contains(lower, "version") && (strings.Contains(lower, "mismatch") || strings.Contains(lower, "constraint") || strings.Contains(lower, "satisf") || strings.Contains(lower, "required")):
		code = "version_mismatch"
	case strings.Contains(lower, "download") || strings.Contains(lower, "timeout") || strings.Contains(lower, "connection") || strings.Contains(lower, "network"):
		code = "download_failed"
	case strings.Contains(lower, "scan") || strings.Contains(lower, "admit") || strings.Contains(lower, "security"):
		code = "security_scan_failed"
	default:
		code = "install_failed"
	}
	if dep.InstallRefStatus == "invalid" || dep.InstallRefStatus == "missing" {
		stage = "install_ref"
		code = dep.InstallRefStatus + "_install_ref"
	}
	return code, stage, detail
}

func maclawAppDependencyInstallStage(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "enterprise", "enterprise_hub":
		return "enterprise_hub_install"
	case "market", "skillmarket", "hubcenter":
		return "skillmarket_download"
	case "hub", "skillhub", "local", "":
		return "skillhub_download"
	case "github":
		return "github_import"
	default:
		return "dependency_install"
	}
}

func maclawAppApplyDependencyPreflightDiagnostics(deps []maclawAppInstallPlanDependency) {
	for i := range deps {
		dep := &deps[i]
		dep.PreflightStage = "dependency_preflight"
		if dep.InstallRefStatus == "missing" || dep.InstallRefStatus == "invalid" {
			dep.PreflightStatus = "blocked"
			dep.PreflightCode = dep.InstallRefStatus + "_install_ref"
			dep.PreflightStage = "install_ref"
			dep.PreflightMessage = firstNonEmpty(dep.InstallRefMessage, dep.Message)
			continue
		}
		if dep.Installed && maclawAppDependencyVersionMismatch(*dep) {
			dep.PreflightStatus = "blocked"
			dep.PreflightCode = "version_mismatch"
			dep.PreflightStage = "local_dependency_scan"
			dep.PreflightMessage = fmt.Sprintf("installed version %s does not satisfy required version %s", dep.InstalledVersion, firstNonEmpty(dep.RequiredVersion, dep.Version))
			continue
		}
		if strings.TrimSpace(dep.InstallRefVersion) != "" && strings.TrimSpace(dep.Version) != "" && !maclawAppInstallRefVersionSatisfiesDependency(*dep) {
			dep.PreflightStatus = "blocked"
			dep.PreflightCode = "version_mismatch"
			dep.PreflightStage = "install_ref"
			dep.PreflightMessage = fmt.Sprintf("install_ref version %s does not satisfy required version %s", dep.InstallRefVersion, dep.Version)
			if dep.Required {
				dep.Health = "missing"
				dep.Action = "blocked"
				dep.Message = dep.PreflightMessage
			}
			continue
		}
		if dep.Installed && maclawAppDependencyIsReady(*dep) {
			dep.PreflightStatus = "ready"
			dep.PreflightCode = "installed_ready"
			dep.PreflightStage = "local_dependency_scan"
			dep.PreflightMessage = "installed dependency is ready"
			continue
		}
		if dep.InstallRefStatus == "ok" {
			dep.PreflightStatus = "pending"
			dep.PreflightCode = "target_resolved"
			dep.PreflightStage = "install_ref"
			dep.PreflightMessage = "install_ref target resolved; remote availability will be checked during install"
			continue
		}
		if dep.InstallRefStatus == "not_required" {
			dep.PreflightStatus = "pending"
			dep.PreflightCode = "name_based_lookup"
			dep.PreflightMessage = "name-based dependency lookup will be checked during install"
			continue
		}
		dep.PreflightStatus = "pending"
		dep.PreflightCode = "not_checked"
		dep.PreflightMessage = "dependency preflight has not checked remote availability yet"
	}
}

func (a *App) maclawAppApplyRemoteDependencyPreflightDiagnostics(deps []maclawAppInstallPlanDependency) {
	if a == nil || len(deps) == 0 {
		return
	}
	cfg, cfgErr := a.LoadConfig()
	var enterpriseClient *capabilityMarketClient
	var enterpriseClientErr error
	var publicSearcher *SkillSearcher
	var publicSearcherErr error
	for i := range deps {
		dep := &deps[i]
		if dep.Installed && maclawAppDependencyIsReady(*dep) {
			continue
		}
		if dep.PreflightStatus == "blocked" {
			continue
		}
		if dep.Installed {
			continue
		}
		if strings.EqualFold(dep.InstallRefKind, "enterprise_hub") && strings.TrimSpace(dep.InstallRefTarget) != "" {
			if enterpriseClient == nil && enterpriseClientErr == nil {
				if cfgErr != nil {
					enterpriseClientErr = cfgErr
				} else {
					enterpriseClient, enterpriseClientErr = newCapabilityMarketClient(cfg)
				}
			}
			if enterpriseClientErr != nil || enterpriseClient == nil {
				dep.PreflightStatus = "pending"
				dep.PreflightCode = "remote_preflight_unavailable"
				dep.PreflightStage = "enterprise_hub_preflight"
				dep.PreflightMessage = firstNonEmpty(strings.TrimSpace(fmt.Sprint(enterpriseClientErr)), "enterprise Hub marketplace is not configured for dependency preflight")
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			item, err := enterpriseClient.getCapability(ctx, dep.InstallRefTarget)
			cancel()
			if err != nil {
				code := maclawAppClassifyDependencyPreflightError(err)
				dep.PreflightStatus = "blocked"
				dep.PreflightCode = code
				dep.PreflightStage = "enterprise_hub_preflight"
				dep.PreflightMessage = fmt.Sprintf("enterprise Hub dependency %s preflight failed: %v", dep.InstallRefTarget, err)
				if dep.Required {
					dep.Health = "missing"
					dep.Action = "blocked"
					dep.Message = dep.PreflightMessage
				}
				continue
			}
			maclawAppApplyEnterpriseHubCapabilityPreflight(dep, item)
			continue
		}
		opportunisticHubCenterLookup := false
		if !maclawAppDependencySupportsPublicMarketPreflight(*dep) {
			if !maclawAppDependencySupportsHubCenterLookup(*dep) || cfgErr != nil || !maclawAppConfigHasExplicitHubCenter(cfg) {
				continue
			}
			// A custom installer may resolve aliases itself; do not let remote
			// preflight rewrite its install_ref contract.
			if a.maclawAppInstallMixedSkill != nil {
				continue
			}
			opportunisticHubCenterLookup = true
		}
		if cfgErr != nil || !maclawAppConfigHasExplicitHubCenter(cfg) {
			dep.PreflightStatus = "pending"
			dep.PreflightCode = "remote_preflight_unavailable"
			dep.PreflightStage = "skillmarket_preflight"
			dep.PreflightMessage = firstNonEmpty(strings.TrimSpace(fmt.Sprint(cfgErr)), "HubCenter is not explicitly configured for SkillMarket dependency preflight")
			continue
		}
		if publicSearcher == nil && publicSearcherErr == nil {
			client := NewSkillMarketClient(a)
			publicSearcher = NewSkillSearcher(client)
		}
		if publicSearcherErr != nil || publicSearcher == nil {
			dep.PreflightStatus = "pending"
			dep.PreflightCode = "remote_preflight_unavailable"
			dep.PreflightStage = "skillmarket_preflight"
			dep.PreflightMessage = firstNonEmpty(strings.TrimSpace(fmt.Sprint(publicSearcherErr)), "SkillMarket searcher is not configured for dependency preflight")
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		query, results, searched, searchErr := maclawAppSearchSkillMarketDependencyPreflight(ctx, publicSearcher, *dep)
		cancel()
		if !searched {
			dep.PreflightStatus = "pending"
			dep.PreflightCode = "remote_preflight_unavailable"
			dep.PreflightStage = "skillmarket_preflight"
			dep.PreflightMessage = fmt.Sprintf("SkillMarket dependency %s preflight unavailable: %v", firstNonEmpty(query, dep.InstallRefTarget, dep.InstallRef, dep.ID), searchErr)
			continue
		}
		if opportunisticHubCenterLookup {
			maclawAppApplyHubCenterLookupPreflight(dep, results)
			continue
		}
		maclawAppApplyPublicSkillMarketPreflight(dep, results)
	}
}

func maclawAppSearchSkillMarketDependencyPreflight(ctx context.Context, searcher *SkillSearcher, dep maclawAppInstallPlanDependency) (string, []SkillSearchResult, bool, error) {
	var lastQuery string
	var lastResults []SkillSearchResult
	var lastErr error
	searched := false
	hadErr := false
	for _, query := range maclawAppDependencySkillMarketSearchQueries(dep) {
		lastQuery = query
		results, err := searcher.Search(ctx, query, nil, 10)
		if err != nil {
			lastErr = err
			hadErr = true
			continue
		}
		searched = true
		lastResults = results
		if maclawAppFindSkillMarketPreflightMatch(dep, results) != nil {
			return query, results, true, nil
		}
	}
	if hadErr {
		return lastQuery, nil, false, lastErr
	}
	return lastQuery, lastResults, searched, nil
}

func maclawAppDependencySkillMarketSearchQueries(dep maclawAppInstallPlanDependency) []string {
	values := []string{dep.InstallRefTarget, dep.InstallRef, dep.CanonicalID}
	values = append(values, dep.Aliases...)
	values = append(values, dep.ID)
	queries := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, "{") {
			continue
		}
		if normalized := maclawAppDependencyLookupValue(value); normalized != "" {
			value = normalized
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		queries = append(queries, value)
	}
	return queries
}

func maclawAppDependencySupportsPublicMarketPreflight(dep maclawAppInstallPlanDependency) bool {
	if strings.TrimSpace(dep.ID) == "" || strings.EqualFold(dep.InstallRefKind, "github") {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(dep.Source))
	kind := strings.ToLower(strings.TrimSpace(dep.InstallRefKind))
	switch source {
	case "market", "skillmarket", "hubcenter":
		return true
	}
	switch kind {
	case "skillmarket", "market", "hubcenter":
		return true
	}
	return false
}

func maclawAppDependencySupportsHubCenterLookup(dep maclawAppInstallPlanDependency) bool {
	if strings.TrimSpace(dep.ID) == "" || strings.EqualFold(dep.InstallRefKind, "github") || strings.EqualFold(dep.InstallRefKind, "enterprise_hub") {
		return false
	}
	// Only hub-type sources may be rewritten via HubCenter name lookup.
	// Explicit local / market / enterprise packages must keep their declared source
	// (local deps especially must not be "promoted" into a remote install path).
	switch strings.ToLower(strings.TrimSpace(dep.Source)) {
	case "", "hub", "skillhub":
		// allowed
	default:
		return false
	}
	status := strings.ToLower(strings.TrimSpace(dep.InstallRefStatus))
	return status == "ok" || status == "not_required"
}

func maclawAppApplyHubCenterLookupPreflight(dep *maclawAppInstallPlanDependency, results []SkillSearchResult) bool {
	if dep == nil {
		return false
	}
	match := maclawAppFindSkillMarketPreflightMatch(*dep, results)
	if match == nil {
		return false
	}
	maclawAppApplyPublicSkillMarketPreflight(dep, []SkillSearchResult{*match})
	return true
}

func maclawAppDependencyIntegrityFromSkillMarketResult(result SkillSearchResult) maclawAppDependencyIntegrityMetadata {
	return maclawAppDependencyIntegrityMetadata{
		PackageSHA256:      firstNonEmpty(result.PackageSHA256, result.SHA256),
		PackageChecksum:    firstNonEmpty(result.PackageChecksum, result.Checksum),
		PackageSignature:   firstNonEmpty(result.PackageSignature, result.Signature),
		PackageDownloadURL: firstNonEmpty(result.PackageDownloadURL, result.DownloadURL),
	}
}

func maclawAppDependencyIntegrityFromCapability(item *HubCapabilitySummary) maclawAppDependencyIntegrityMetadata {
	if item == nil {
		return maclawAppDependencyIntegrityMetadata{}
	}
	metadata := map[string]any{}
	if strings.TrimSpace(item.MetadataJSON) != "" {
		_ = json.Unmarshal([]byte(item.MetadataJSON), &metadata)
	}
	return maclawAppDependencyIntegrityMetadata{
		PackageSHA256: firstNonEmpty(
			item.PackageSHA256,
			stringFromAny(metadata["package_sha256"]),
			stringFromAny(metadata["sha256"]),
		),
		PackageChecksum: firstNonEmpty(
			item.PackageChecksum,
			stringFromAny(metadata["package_checksum"]),
			stringFromAny(metadata["checksum"]),
		),
		PackageSignature: firstNonEmpty(
			item.PackageSignature,
			stringFromAny(metadata["package_signature"]),
			stringFromAny(metadata["signature"]),
		),
		PackageDownloadURL: firstNonEmpty(
			item.PackageDownloadURL,
			stringFromAny(metadata["package_download_url"]),
			stringFromAny(metadata["download_url"]),
			stringFromAny(metadata["package_url"]),
		),
	}
}

func maclawAppApplyDependencyIntegrityMetadata(dep *maclawAppInstallPlanDependency, metadata maclawAppDependencyIntegrityMetadata, stage string) {
	if dep == nil {
		return
	}
	checksum := firstNonEmpty(metadata.PackageSHA256, metadata.PackageChecksum)
	signature := strings.TrimSpace(metadata.PackageSignature)
	downloadURL := strings.TrimSpace(metadata.PackageDownloadURL)
	dep.PackageSHA256 = strings.TrimSpace(metadata.PackageSHA256)
	if dep.PackageSHA256 == "" {
		dep.PackageSHA256 = checksum
	}
	dep.PackageChecksum = strings.TrimSpace(metadata.PackageChecksum)
	if dep.PackageChecksum == "" {
		dep.PackageChecksum = checksum
	}
	dep.PackageSignature = signature
	dep.PackageDownloadURL = downloadURL
	dep.IntegrityStage = strings.TrimSpace(stage)
	if checksum == "" {
		dep.IntegrityStatus = "missing"
		dep.IntegrityCode = "checksum_unavailable"
		dep.IntegrityMessage = "package checksum metadata is not available before install"
		return
	}
	if signature == "" {
		dep.IntegrityStatus = "partial"
		dep.IntegrityCode = "signature_unavailable"
		dep.IntegrityMessage = "package checksum is available but package signature metadata is missing"
		return
	}
	dep.IntegrityStatus = "ready"
	dep.IntegrityCode = "package_integrity_metadata_ready"
	if downloadURL != "" {
		dep.IntegrityMessage = "package checksum, signature and download metadata are available"
	} else {
		dep.IntegrityMessage = "package checksum and signature metadata are available"
	}
}

func maclawAppApplyPublicSkillMarketPreflight(dep *maclawAppInstallPlanDependency, results []SkillSearchResult) {
	if dep == nil {
		return
	}
	match := maclawAppFindSkillMarketPreflightMatch(*dep, results)
	if match == nil {
		dep.PreflightStatus = "blocked"
		dep.PreflightCode = "not_found"
		dep.PreflightStage = "skillmarket_preflight"
		dep.PreflightMessage = fmt.Sprintf("SkillMarket dependency %s was not found", firstNonEmpty(dep.InstallRefTarget, dep.InstallRef, dep.ID))
		if dep.Required {
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = dep.PreflightMessage
		}
		return
	}
	dep.Source = "skillmarket"
	maclawAppApplyDependencyIntegrityMetadata(dep, maclawAppDependencyIntegrityFromSkillMarketResult(*match), "skillmarket_preflight")
	if ref := strings.TrimSpace(firstNonEmpty(match.InstallRef, match.ID)); ref != "" {
		dep.InstallRef = ref
		dep.InstallRefKind = "skillmarket"
		dep.InstallRefTarget = ref
		dep.InstallRefStatus = "ok"
		if dep.CanonicalID == "" {
			dep.CanonicalID = ref
		}
	}
	requiredVersion := strings.TrimSpace(firstNonEmpty(dep.InstallRefVersion, dep.Version, dep.RequiredVersion))
	if requiredVersion != "" && strings.TrimSpace(match.Version) != "" && !maclawAppDependencyVersionSatisfied(requiredVersion, match.Version) {
		dep.PreflightStatus = "blocked"
		dep.PreflightCode = "version_mismatch"
		dep.PreflightStage = "skillmarket_preflight"
		dep.PreflightMessage = fmt.Sprintf("SkillMarket dependency %s version %s does not satisfy required version %s", match.ID, match.Version, requiredVersion)
		if dep.Required {
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = dep.PreflightMessage
		}
		return
	}
	dep.PreflightStatus = "ready"
	dep.PreflightCode = "skillmarket_target_ready"
	dep.PreflightStage = "skillmarket_preflight"
	dep.PreflightMessage = fmt.Sprintf("SkillMarket dependency %s is available", match.ID)
}

func maclawAppFindSkillMarketPreflightMatch(dep maclawAppInstallPlanDependency, results []SkillSearchResult) *SkillSearchResult {
	keys := map[string]struct{}{}
	values := []string{dep.InstallRefTarget, dep.InstallRef, dep.ID, dep.CanonicalID}
	values = append(values, dep.Aliases...)
	for _, value := range values {
		maclawAppAddDependencyLookupKey(keys, value)
	}
	for i := range results {
		for _, value := range []string{results[i].ID, results[i].Name, results[i].InstallRef} {
			if _, ok := keys[strings.ToLower(strings.TrimSpace(value))]; ok {
				return &results[i]
			}
			if normalized := maclawAppDependencyLookupValue(value); normalized != "" {
				if _, ok := keys[strings.ToLower(normalized)]; ok {
					return &results[i]
				}
			}
		}
	}
	return nil
}

func maclawAppAddDependencyLookupKey(keys map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	keys[strings.ToLower(value)] = struct{}{}
	if normalized := maclawAppDependencyLookupValue(value); normalized != "" {
		keys[strings.ToLower(normalized)] = struct{}{}
	}
}

func maclawAppDependencyLookupValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.Contains(value, "://") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" {
		return value
	}
	target := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if target == "" && strings.TrimSpace(parsed.Host) != "" {
		target = strings.TrimSpace(parsed.Host)
	} else if strings.TrimSpace(parsed.Host) != "" && !strings.EqualFold(parsed.Host, "skills") && !strings.EqualFold(parsed.Host, "capabilities") {
		target = strings.Trim(strings.TrimSpace(parsed.Host+"/"+target), "/")
	}
	if strings.Contains(target, "@") {
		parts := strings.SplitN(target, "@", 2)
		target = strings.TrimSpace(parts[0])
	}
	if target == "" {
		return value
	}
	return target
}

func maclawAppApplyEnterpriseHubCapabilityPreflight(dep *maclawAppInstallPlanDependency, item *HubCapabilitySummary) {
	if dep == nil || item == nil {
		return
	}
	capType := strings.TrimSpace(item.CapabilityType)
	if capType != "" && !strings.EqualFold(capType, "skill") {
		dep.PreflightStatus = "blocked"
		dep.PreflightCode = "capability_type_mismatch"
		dep.PreflightStage = "enterprise_hub_preflight"
		dep.PreflightMessage = fmt.Sprintf("enterprise Hub target %s is capability type %s, not skill", dep.InstallRefTarget, capType)
		if dep.Required {
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = dep.PreflightMessage
		}
		return
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status != "" && status != "published" && status != "approved" && status != "active" && status != "available" {
		dep.PreflightStatus = "blocked"
		dep.PreflightCode = "policy_rejected"
		dep.PreflightStage = "enterprise_hub_preflight"
		dep.PreflightMessage = fmt.Sprintf("enterprise Hub target %s is not published or available (status: %s)", dep.InstallRefTarget, item.Status)
		if dep.Required {
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = dep.PreflightMessage
		}
		return
	}
	maclawAppApplyDependencyIntegrityMetadata(dep, maclawAppDependencyIntegrityFromCapability(item), "enterprise_hub_preflight")
	availableVersion := strings.TrimSpace(item.CurrentVersionKey)
	requiredVersion := strings.TrimSpace(firstNonEmpty(dep.InstallRefVersion, dep.Version, dep.RequiredVersion))
	if requiredVersion != "" && availableVersion != "" && !maclawAppDependencyVersionSatisfied(requiredVersion, availableVersion) {
		dep.PreflightStatus = "blocked"
		dep.PreflightCode = "version_mismatch"
		dep.PreflightStage = "enterprise_hub_preflight"
		dep.PreflightMessage = fmt.Sprintf("enterprise Hub target %s version %s does not satisfy required version %s", dep.InstallRefTarget, availableVersion, requiredVersion)
		if dep.Required {
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = dep.PreflightMessage
		}
		return
	}
	dep.PreflightStatus = "ready"
	dep.PreflightCode = "enterprise_hub_target_ready"
	dep.PreflightStage = "enterprise_hub_preflight"
	dep.PreflightMessage = fmt.Sprintf("enterprise Hub target %s is available", firstNonEmpty(item.ID, item.CapabilityID, dep.InstallRefTarget))
}

func maclawAppClassifyDependencyPreflightError(err error) string {
	lower := strings.ToLower(strings.TrimSpace(fmt.Sprint(err)))
	switch {
	case strings.Contains(lower, "404") || strings.Contains(lower, "not found"):
		return "not_found"
	case strings.Contains(lower, "401") || strings.Contains(lower, "403") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "permission") || strings.Contains(lower, "denied"):
		return "access_denied"
	case strings.Contains(lower, "policy") || strings.Contains(lower, "rejected"):
		return "policy_rejected"
	case strings.Contains(lower, "version"):
		return "version_mismatch"
	default:
		return "remote_preflight_failed"
	}
}

func maclawAppDependencyInstallerRef(dep maclawAppInstallPlanDependency) string {
	if strings.EqualFold(dep.InstallRefKind, "github") {
		return strings.TrimSpace(dep.InstallRef)
	}
	return firstNonEmpty(dep.InstallRefTarget, dep.InstallRef)
}

func maclawAppDependencyInstallerSource(dep maclawAppInstallPlanDependency) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(dep.InstallRefKind)) {
	case "skillmarket", "market", "hubcenter":
		return string(skillSearchSourceSkillMarket), true
	case "enterprise", "enterprise_hub":
		return string(skillSearchSourceEnterpriseHub), true
	case "github":
		return string(skillSearchSourceGitHub), true
	}
	return maclawAppInstallSkillSource(dep.Source)
}

func maclawAppRegisteredDependencySource(installerSource string) string {
	switch strings.ToLower(strings.TrimSpace(installerSource)) {
	case string(skillSearchSourceSkillMarket):
		return string(skillSearchSourceSkillMarket)
	case string(skillSearchSourceGitHub):
		return skillEntrySourceGitHub.String()
	case string(skillSearchSourceClawHub):
		return skillEntrySourceClawHub.String()
	default:
		return skillEntrySourceHub.String()
	}
}

func maclawAppValidateDependencyInstallRefs(deps []maclawAppInstallPlanDependency) {
	for i := range deps {
		dep := &deps[i]
		kind, target, version, status, message := maclawAppParseDependencyInstallRef(*dep)
		dep.InstallRefKind = kind
		dep.InstallRefTarget = target
		dep.InstallRefVersion = version
		dep.InstallRefStatus = status
		dep.InstallRefMessage = message
		if dep.Required && (status == "invalid" || status == "missing") {
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = message
		}
	}
}

func maclawAppParseDependencyInstallRef(dep maclawAppInstallPlanDependency) (kind, target, version, status, message string) {
	ref := strings.TrimSpace(dep.InstallRef)
	source := strings.ToLower(strings.TrimSpace(dep.Source))
	if ref == "" {
		if resolved, ok := maclawAppImplicitHubSkillResolution(dep); ok {
			return "hub", resolved.Target, strings.TrimSpace(dep.Version), "ok", resolved.Message
		}
		switch source {
		case "enterprise", "enterprise_hub", "github":
			return "", "", "", "missing", fmt.Sprintf("required skill dependency %q from %s must include install_ref", dep.ID, dep.Source)
		default:
			return "", "", "", "not_required", "install_ref not required for name-based SkillHub/SkillMarket install"
		}
	}
	if strings.HasPrefix(ref, "{") {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(ref), &candidate); err != nil {
			return "github", "", "", "invalid", fmt.Sprintf("invalid github install_ref for %s: %v", dep.ID, err)
		}
		target = firstNonEmpty(stringFromAny(candidate["repo_full_name"]), stringFromAny(candidate["repoFullName"]), stringFromAny(candidate["raw_url"]), stringFromAny(candidate["rawURL"]))
		if firstNonEmpty(stringFromAny(candidate["raw_url"]), stringFromAny(candidate["rawURL"])) == "" {
			return "github", target, "", "invalid", fmt.Sprintf("invalid github install_ref for %s: missing raw_url", dep.ID)
		}
		if source != "" && source != "github" {
			return "github", target, "", "invalid", fmt.Sprintf("install_ref source github does not match dependency source %q", dep.Source)
		}
		return "github", target, "", "ok", "github install_ref resolved"
	}
	if strings.HasPrefix(strings.ToLower(ref), "enterprise_hub://") {
		kind = "enterprise_hub"
		target = strings.TrimPrefix(ref, ref[:len("enterprise_hub://")])
		target = strings.Trim(strings.TrimSpace(target), "/")
		if strings.HasPrefix(strings.ToLower(target), "capabilities/") {
			target = strings.TrimPrefix(target, target[:len("capabilities/")])
		}
		if strings.Contains(target, "@") {
			parts := strings.SplitN(target, "@", 2)
			target = strings.TrimSpace(parts[0])
			version = strings.TrimSpace(parts[1])
		}
		if target == "" {
			return kind, "", version, "invalid", fmt.Sprintf("install_ref %q does not include a target capability", ref)
		}
		if !maclawAppInstallRefSourceMatches(source, kind) {
			return kind, target, version, "invalid", fmt.Sprintf("install_ref source %q does not match dependency source %q", kind, dep.Source)
		}
		return kind, target, version, "ok", "install_ref resolved"
	}
	parsed, err := url.Parse(ref)
	if err == nil && strings.TrimSpace(parsed.Scheme) != "" {
		kind = strings.ToLower(strings.TrimSpace(parsed.Scheme))
		target = strings.Trim(strings.TrimSpace(parsed.Path), "/")
		if target == "" && strings.TrimSpace(parsed.Host) != "" {
			target = strings.TrimSpace(parsed.Host)
		} else if strings.TrimSpace(parsed.Host) != "" && !strings.EqualFold(parsed.Host, "skills") && !strings.EqualFold(parsed.Host, "capabilities") {
			target = strings.Trim(strings.TrimSpace(parsed.Host+"/"+target), "/")
		}
		if strings.Contains(target, "@") {
			parts := strings.SplitN(target, "@", 2)
			target = strings.TrimSpace(parts[0])
			version = strings.TrimSpace(parts[1])
		}
		if target == "" {
			return kind, "", version, "invalid", fmt.Sprintf("install_ref %q does not include a target skill or capability", ref)
		}
		if !maclawAppInstallRefSourceMatches(source, kind) {
			return kind, target, version, "invalid", fmt.Sprintf("install_ref source %q does not match dependency source %q", kind, dep.Source)
		}
		return kind, target, version, "ok", "install_ref resolved"
	}
	if source == "github" {
		return "github", ref, "", "invalid", fmt.Sprintf("github dependency %q must use a JSON install_ref with raw_url", dep.ID)
	}
	if kind := maclawAppBareDependencyInstallRefKind(dep); kind != "" {
		return kind, ref, "", "ok", "install_ref resolved"
	}
	return "id", ref, "", "ok", "install_ref resolved"
}

var maclawAppDependencyAliasRegistry = []maclawAppDependencyAliasRegistryEntry{
	{
		Target:     "rapidocr",
		Aliases:    []string{"RapidOCR", "rapidocr-runtime"},
		LocalNames: []string{"rapidocr-runtime"},
		Sources:    []string{"", "hub", "skillhub", "local"},
		Kinds:      []string{"", "runtime_skill", "app_skill", "skill"},
	},
	{
		Target:     "paper_pdf_translator",
		Aliases:    []string{"paper-pdf-translator", "pdf-paper-translator", "pdf_paper_translator"},
		LocalNames: []string{"paper_pdf_translator", "paper-pdf-translator", "pdf-paper-translator"},
		Sources:    []string{"", "hub", "skillhub", "market", "skillmarket", "hubcenter", "local"},
		Kinds:      []string{"", "runtime_skill", "app_skill", "skill", "tool_skill"},
	},
}

func maclawAppImplicitHubSkillResolution(dep maclawAppInstallPlanDependency) (maclawAppDependencyImplicitResolution, bool) {
	source := strings.ToLower(strings.TrimSpace(dep.Source))
	if source != "" && !maclawAppSourceAllowsImplicitHubResolution(source) {
		return maclawAppDependencyImplicitResolution{}, false
	}
	kind := strings.ToLower(strings.TrimSpace(dep.Kind))
	if !maclawAppDependencyKindAllowsImplicitHubTarget(kind) {
		return maclawAppDependencyImplicitResolution{}, false
	}
	if target := strings.TrimSpace(dep.CanonicalID); target != "" {
		return maclawAppDependencyImplicitResolution{
			Target:     target,
			Aliases:    appendMaclawAppUniqueStrings([]string{dep.ID}, dep.Aliases...),
			LocalNames: maclawAppImplicitHubLocalNames(target, kind),
			Message:    "declared dependency canonical target resolved",
		}, true
	}
	values := append([]string{dep.ID}, dep.Aliases...)
	for _, entry := range maclawAppDependencyAliasRegistry {
		if !maclawAppDependencyAliasRegistryAllows(entry.Sources, source) || !maclawAppDependencyAliasRegistryAllows(entry.Kinds, kind) {
			continue
		}
		for _, value := range values {
			if maclawAppDependencyAliasMatches(value, entry.Target, entry.Aliases, entry.LocalNames) {
				return maclawAppDependencyImplicitResolution{
					Target:     entry.Target,
					Aliases:    appendMaclawAppUniqueStrings(append([]string{}, entry.Aliases...), dep.Aliases...),
					LocalNames: append([]string{}, entry.LocalNames...),
					Message:    "known dependency alias target resolved",
				}, true
			}
		}
	}
	return maclawAppDependencyImplicitResolution{}, false
}

func maclawAppDependencyKindAllowsImplicitHubTarget(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "runtime_skill", "app_skill", "skill", "workflow_skill", "connector_skill", "data_skill", "tool_skill":
		return true
	default:
		return false
	}
}

func maclawAppImplicitHubLocalNames(target, kind string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	names := []string{target}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "runtime_skill":
		names = append(names, target+"-runtime")
	}
	return appendMaclawAppUniqueStrings(nil, names...)
}

func maclawAppDependencyAliasRegistryAllows(allowed []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, item := range allowed {
		if strings.ToLower(strings.TrimSpace(item)) == value {
			return true
		}
	}
	return false
}

func maclawAppDependencyAliasMatches(value, target string, aliases, localNames []string) bool {
	key := maclawAppNormalizeDependencyAlias(value)
	if key == "" {
		return false
	}
	for _, candidate := range append(append([]string{target}, aliases...), localNames...) {
		if key == maclawAppNormalizeDependencyAlias(candidate) {
			return true
		}
	}
	return false
}

func maclawAppNormalizeDependencyAlias(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func maclawAppBareDependencyInstallRefKind(dep maclawAppInstallPlanDependency) string {
	source := strings.ToLower(strings.TrimSpace(dep.Source))
	explicit := strings.ToLower(strings.TrimSpace(dep.InstallRefKind))
	if explicit != "" && explicit != "id" {
		if source == "" || maclawAppInstallRefSourceMatches(source, explicit) {
			return maclawAppNormalizeInstallRefKind(explicit)
		}
	}
	switch source {
	case "market", "skillmarket", "hubcenter":
		return "skillmarket"
	case "enterprise", "enterprise_hub":
		return "enterprise_hub"
	case "hub", "skillhub":
		return "hub"
	default:
		return ""
	}
}

func maclawAppNormalizeInstallRefKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "market", "skillmarket", "hubcenter":
		return "skillmarket"
	case "enterprise", "enterprise_hub":
		return "enterprise_hub"
	case "hub", "skillhub", "skill", "skills":
		return "hub"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func applyResolvedDependenciesToPlan(deps []maclawAppInstallPlanDependency, installDoc map[string]any) {
	// Collect resolved entries from all available locations.
	// Use anySlice so both []interface{} (JSON decode) and []map[string]any
	// (in-memory stamp path before re-encode) are accepted.
	var allResolved []any
	// Source 1: package-level (local queue path).
	allResolved = append(allResolved, anySlice(installDoc["resolved_dependencies"])...)
	// Source 2: entry-level (Hub download path — each entry may carry its own).
	for _, appRaw := range anySlice(installDoc["apps"]) {
		entryMap := anyMap(appRaw)
		if entryMap == nil {
			continue
		}
		allResolved = append(allResolved, anySlice(entryMap["resolved_dependencies"])...)
	}
	if len(allResolved) == 0 {
		return
	}
	// Build a lookup from resolved entries: id → scoped resolved metadata.
	lookup := make(map[string][]maclawAppResolvedDependencyEntry, len(allResolved))
	for _, item := range allResolved {
		resMap := anyMap(item)
		if resMap == nil {
			continue
		}
		id := stringFromMapSafe(resMap, "id")
		ref := stringFromMapSafe(resMap, "install_ref")
		if id == "" || ref == "" {
			continue
		}
		key := strings.ToLower(id)
		entry := maclawAppResolvedDependencyEntry{
			ID:                 id,
			InstallRef:         ref,
			Source:             stringFromMapSafe(resMap, "source"),
			Version:            stringFromMapSafe(resMap, "version"),
			CanonicalID:        firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "canonical_id"), stringFromMapSafe(resMap, "canonicalID")),
			Aliases:            maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(resMap["aliases"], resMap["install_aliases"], resMap["installAliases"])),
			AppIDs:             maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(resMap["app_ids"], resMap["appIDs"])),
			InstallRefKind:     firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "install_ref_kind"), stringFromMapSafe(resMap, "installRefKind")),
			InstallRefTarget:   firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "install_ref_target"), stringFromMapSafe(resMap, "installRefTarget")),
			InstallRefVersion:  firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "install_ref_version"), stringFromMapSafe(resMap, "installRefVersion")),
			PackageSHA256:      firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "package_sha256"), stringFromMapSafe(resMap, "packageSHA256")),
			PackageChecksum:    firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "package_checksum"), stringFromMapSafe(resMap, "packageChecksum")),
			PackageSignature:   firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "package_signature"), stringFromMapSafe(resMap, "packageSignature")),
			PackageDownloadURL: firstNonEmptyMaclawAppString(stringFromMapSafe(resMap, "package_download_url"), stringFromMapSafe(resMap, "packageDownloadURL")),
		}
		lookup[key] = append(lookup[key], entry)
	}
	if len(lookup) == 0 {
		return
	}
	// Apply to plan dependencies.
	for i := range deps {
		entry, ok := maclawAppResolvedDependencyEntryForPlanDep(deps[i], lookup[strings.ToLower(deps[i].ID)])
		if !ok {
			continue
		}
		deps[i].InstallRef = entry.InstallRef
		if entry.Source != "" {
			deps[i].Source = entry.Source
		}
		if entry.Version != "" && deps[i].Version == "" {
			deps[i].Version = entry.Version
		}
		if entry.CanonicalID != "" && strings.TrimSpace(deps[i].CanonicalID) == "" {
			deps[i].CanonicalID = entry.CanonicalID
		}
		if len(entry.Aliases) > 0 {
			deps[i].Aliases = appendMaclawAppUniqueStrings(deps[i].Aliases, entry.Aliases...)
		}
		if entry.InstallRefKind != "" {
			deps[i].InstallRefKind = entry.InstallRefKind
		}
		if entry.InstallRefTarget != "" {
			deps[i].InstallRefTarget = entry.InstallRefTarget
		}
		if entry.InstallRefVersion != "" {
			deps[i].InstallRefVersion = entry.InstallRefVersion
		}
		if entry.PackageSHA256 != "" && strings.TrimSpace(deps[i].PackageSHA256) == "" {
			deps[i].PackageSHA256 = entry.PackageSHA256
		}
		if entry.PackageChecksum != "" && strings.TrimSpace(deps[i].PackageChecksum) == "" {
			deps[i].PackageChecksum = entry.PackageChecksum
		}
		if entry.PackageSignature != "" && strings.TrimSpace(deps[i].PackageSignature) == "" {
			deps[i].PackageSignature = entry.PackageSignature
		}
		if entry.PackageDownloadURL != "" && strings.TrimSpace(deps[i].PackageDownloadURL) == "" {
			deps[i].PackageDownloadURL = entry.PackageDownloadURL
		}
	}
}

func maclawAppResolvedDependencyEntryForPlanDep(dep maclawAppInstallPlanDependency, entries []maclawAppResolvedDependencyEntry) (maclawAppResolvedDependencyEntry, bool) {
	var fallback maclawAppResolvedDependencyEntry
	hasFallback := false
	for _, entry := range entries {
		if len(entry.AppIDs) == 0 {
			if !hasFallback {
				fallback = entry
				hasFallback = true
			}
			continue
		}
		if maclawAppStringListsOverlap(dep.AppIDs, entry.AppIDs) {
			return entry, true
		}
	}
	return fallback, hasFallback
}

func maclawAppApplySourceVersionKeyDependencyRefs(deps []maclawAppInstallPlanDependency) {
	for i := range deps {
		dep := &deps[i]
		if strings.TrimSpace(dep.InstallRef) != "" {
			continue
		}
		kind, target, version, ok := maclawAppParseSourceVersionKey(dep.Version)
		if !ok || target == "" {
			continue
		}
		switch strings.ToLower(kind) {
		case "enterprise_hub":
			dep.InstallRef = "enterprise_hub://capabilities/" + target
			if version != "" {
				dep.InstallRef += "@" + version
			}
			source := strings.ToLower(strings.TrimSpace(dep.Source))
			if source == "" || source == "local" || source == "hub" || source == "skillhub" {
				dep.Source = "enterprise_hub"
			}
			if strings.TrimSpace(dep.CanonicalID) == "" {
				dep.CanonicalID = target
			}
		case "hubcenter", "skillmarket":
			dep.InstallRef = "skillmarket://skills/" + target
			if version != "" {
				dep.InstallRef += "@" + version
			}
			source := strings.ToLower(strings.TrimSpace(dep.Source))
			if source == "" || source == "local" || source == "hub" || source == "skillhub" || source == "hubcenter" {
				dep.Source = "skillmarket"
			}
			if strings.TrimSpace(dep.CanonicalID) == "" {
				dep.CanonicalID = target
			}
		}
	}
}

func maclawAppParseSourceVersionKey(value string) (kind, target, version string, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", false
	}
	lower := strings.ToLower(value)
	prefix := ""
	switch {
	case strings.HasPrefix(lower, "enterprise_hub:skill:"):
		kind = "enterprise_hub"
		prefix = value[:len("enterprise_hub:skill:")]
	case strings.HasPrefix(lower, "skillmarket:skill:"):
		kind = "skillmarket"
		prefix = value[:len("skillmarket:skill:")]
	case strings.HasPrefix(lower, "hubcenter:skill:"):
		kind = "hubcenter"
		prefix = value[:len("hubcenter:skill:")]
	default:
		return "", "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	if rest == "" {
		return "", "", "", false
	}
	if strings.Contains(rest, "@") {
		parts := strings.SplitN(rest, "@", 2)
		target = strings.TrimSpace(parts[0])
		version = strings.TrimSpace(parts[1])
	} else {
		target = rest
	}
	return kind, target, version, target != ""
}

// injectResolvedDepsIntoAppEntries writes resolved_dependencies into each app
// entry inside the package. This ensures the data survives Hub storage (which
// only persists per-entry ManifestJSON, not the top-level package structure).
func injectResolvedDepsIntoAppEntries(pkg map[string]any, enrichedDeps []map[string]any) {
	apps := anySlice(pkg["apps"])
	if len(apps) == 0 {
		return
	}
	for _, appRaw := range apps {
		entryMap := anyMap(appRaw)
		if entryMap == nil {
			continue
		}
		appID := maclawAppPackageEntryID(entryMap)
		scoped := maclawAppResolvedDependencyMapsForApp(enrichedDeps, appID)
		if len(scoped) > 0 {
			entryMap["resolved_dependencies"] = scoped
		}
	}
}

func injectBundledDepsIntoAppEntries(pkg map[string]any, bundled maclawAppBundledDependencies) {
	if len(bundled.Skills) == 0 {
		return
	}
	apps := anySlice(pkg["apps"])
	if len(apps) == 0 {
		return
	}
	for _, appRaw := range apps {
		entryMap := anyMap(appRaw)
		if entryMap == nil {
			continue
		}
		appID := maclawAppPackageEntryID(entryMap)
		scoped := maclawAppBundledDependenciesForApp(bundled, appID)
		if len(scoped.Skills) > 0 {
			entryMap["bundled_dependencies"] = scoped
		}
	}
}

func maclawAppResolvedDependencyMapsForApp(deps []map[string]any, appID string) []map[string]any {
	if len(deps) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(deps))
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		appIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(dep["app_ids"], dep["appIDs"]))
		if len(appIDs) > 0 && !containsMaclawAppStringFold(appIDs, appID) {
			continue
		}
		out = append(out, cloneMapAny(dep))
	}
	return out
}

func maclawAppBundledDependenciesForApp(bundled maclawAppBundledDependencies, appID string) maclawAppBundledDependencies {
	scoped := maclawAppBundledDependencies{Schema: bundled.Schema}
	for _, skill := range bundled.Skills {
		if len(skill.AppIDs) > 0 && !containsMaclawAppStringFold(skill.AppIDs, appID) {
			continue
		}
		skill.AppIDs = append([]string(nil), skill.AppIDs...)
		skill.Files = cloneStringMap(skill.Files)
		scoped.Skills = append(scoped.Skills, skill)
	}
	return scoped
}

func (a *App) maclawAppBundledDependenciesForPlan(deps []maclawAppInstallPlanDependency) maclawAppBundledDependencies {
	out := maclawAppBundledDependencies{Schema: "maclaw.app.bundled_dependencies.v1"}
	if a == nil || len(deps) == 0 {
		return out
	}
	defs := a.ListNLSkills()
	byName := map[string]NLSkillDefinition{}
	for _, def := range defs {
		for _, id := range []string{def.Name, def.DirName, def.HubSkillID} {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			key := strings.ToLower(id)
			if _, exists := byName[key]; !exists {
				byName[key] = def
			}
		}
	}
	seen := map[string]bool{}
	for _, dep := range deps {
		if !dep.Installed {
			continue
		}
		def, ok := byName[strings.ToLower(strings.TrimSpace(firstNonEmpty(dep.InstalledName, dep.CanonicalID, dep.ID)))]
		if !ok && strings.TrimSpace(dep.InstalledDir) != "" {
			for _, candidate := range defs {
				if skillDirIdentityKey(candidate.SkillDir) == skillDirIdentityKey(dep.InstalledDir) {
					def = candidate
					ok = true
					break
				}
			}
		}
		if !ok || strings.TrimSpace(def.SkillDir) == "" {
			continue
		}
		bundled, err := maclawAppBundleInstalledSkill(def, dep)
		if err != nil {
			log.Printf("[maclaw-app] skip bundled dependency %q: %v", dep.ID, err)
			continue
		}
		key := strings.ToLower(strings.TrimSpace(firstNonEmpty(bundled.StableID, bundled.Name)))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out.Skills = append(out.Skills, bundled)
	}
	return out
}

func maclawAppBundledSkillSkipDir(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case ".git", ".hg", ".svn", ".venv", "venv", "node_modules", "__pycache__", ".pytest_cache", ".mypy_cache", "dist", "build", ".maclaw", ".cache":
		return true
	default:
		return false
	}
}

func maclawAppBundledSkillFilePathOK(rel string) bool {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\x00") || strings.Contains(rel, "\\") {
		return false
	}
	if strings.Contains(rel, "../") || strings.HasPrefix(rel, "../") || rel == ".." {
		return false
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Contains(part, ":") {
			return false
		}
		if maclawAppBundledSkillSkipFileName(part) {
			return false
		}
	}
	return true
}

func maclawAppBundledSkillSkipFileName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return true
	}
	switch lower {
	case ".env", ".env.local", ".env.production", ".env.development", ".npmrc", ".pypirc", ".netrc", ".ds_store", "thumbs.db", "skill_scan_cache.json":
		return true
	default:
		return strings.HasSuffix(lower, ".pyc") || strings.HasSuffix(lower, ".pyo") || strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, ".tmp")
	}
}

func (a *App) installBundledMaclawAppDependency(packageJSON string, dep maclawAppInstallPlanDependency) (bool, error) {
	// Primary: check bundled_dependencies in the provided packageJSON.
	bundled := maclawAppBundledDependenciesForPackageJSON(packageJSON)
	if candidate := maclawAppFindBundledSkillForDep(bundled, dep); candidate != nil {
		if err := a.installBundledMaclawAppSkill(*candidate); err != nil {
			return true, err
		}
		return true, nil
	}

	// Fallback: check bundled_dependencies in persisted install records.
	// This covers the case where the caller passes a fresh manifest (e.g.,
	// appToManifest from the frontend) that doesn't include bundled_dependencies,
	// but the original install package (stored in app_install_records.json) does.
	if candidate := a.findBundledSkillInInstallRecords(dep); candidate != nil {
		if err := a.installBundledMaclawAppSkill(*candidate); err != nil {
			return true, err
		}
		return true, nil
	}

	return false, nil
}

// findBundledSkillInInstallRecords searches all install records for a bundled
// skill matching the given dependency. Returns nil if not found.
func (a *App) findBundledSkillInInstallRecords(dep maclawAppInstallPlanDependency) *maclawAppBundledSkillEntry {
	if a == nil {
		return nil
	}
	registry, err := a.readMaclawAppInstallRegistry()
	if err != nil || len(registry.Installs) == 0 {
		return nil
	}
	for _, record := range registry.Installs {
		if len(record.Package) == 0 {
			continue
		}
		bundled := maclawAppBundledDependenciesFromDoc(record.Package)
		if candidate := maclawAppFindBundledSkillForDep(bundled, dep); candidate != nil {
			return candidate
		}
	}
	return nil
}

// maclawAppFindBundledSkillForDep searches a bundled dependencies set for a
// skill matching the given dependency.
func maclawAppFindBundledSkillForDep(bundled maclawAppBundledDependencies, dep maclawAppInstallPlanDependency) *maclawAppBundledSkillEntry {
	for i := range bundled.Skills {
		if maclawAppBundledSkillMatchesDependency(bundled.Skills[i], dep) {
			return &bundled.Skills[i]
		}
	}
	return nil
}

func (a *App) updateInstalledMaclawAppDependency(dep *maclawAppInstallPlanDependency) (bool, error) {
	if a == nil || dep == nil {
		return false, nil
	}
	name := strings.TrimSpace(dep.InstalledName)
	if name == "" {
		return false, nil
	}
	installerSource, ok := maclawAppDependencyInstallerSource(*dep)
	if !ok {
		return false, nil
	}
	switch installerSource {
	case string(skillSearchSourceSkillHub), string(skillSearchSourceSkillMarket):
	default:
		return false, nil
	}
	a.ensureSkillHubClient()
	if a.skillExecutor == nil {
		return true, fmt.Errorf("skill executor not initialized")
	}
	if a.skillHubClient == nil {
		return true, fmt.Errorf("skill hub client not initialized")
	}
	downloadID := strings.TrimSpace(firstNonEmpty(dep.InstallRefTarget, dep.CanonicalID, dep.ID))
	if downloadID == "" {
		return false, nil
	}
	targetDir := strings.TrimSpace(dep.InstalledDir)
	if targetDir == "" {
		primaryDir, err := a.primarySkillsDir()
		if err != nil {
			return true, fmt.Errorf("resolve primary skills directory: %w", err)
		}
		targetDir = filepath.Join(primaryDir, name)
	}
	targetParent := filepath.Dir(targetDir)
	if err := os.MkdirAll(targetParent, 0o755); err != nil {
		return true, fmt.Errorf("create dependency update parent dir: %w", err)
	}
	stagingDir, err := os.MkdirTemp(targetParent, ".maclaw-app-dep-update-*")
	if err != nil {
		return true, fmt.Errorf("create dependency update staging dir: %w", err)
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	updated, downloadTrace, err := downloadSkillJSONFromHubCenterLocatorToDirWithIntegrityTrace(context.Background(), a, dep.PackageDownloadURL, "/api/v1/skills/"+url.PathEscape(downloadID)+"/download", stagingDir, firstNonEmpty(dep.PackageSHA256, dep.PackageChecksum), dep.PackageSignature)
	applySkillHubDownloadTraceToDependency(dep, downloadTrace)
	if err != nil {
		return true, annotateSkillHubDownloadError(err, downloadTrace)
	}
	if !strings.EqualFold(strings.TrimSpace(updated.Name), name) {
		return true, fmt.Errorf("downloaded dependency name %q does not match installed skill %q", updated.Name, name)
	}
	updated.Name = name
	updated.Source = maclawAppRegisteredDependencySource(installerSource)
	updated.HubSkillID = firstNonEmpty(downloadID, updated.HubSkillID)
	updated.SkillDir = stagingDir
	report, err := a.scanAndAdmitSkillBeforeRegister(context.Background(), updated, installerSource)
	if err != nil {
		return true, err
	}
	if err := writeSkillScanCacheForInstalledEntry(updated, report); err != nil {
		return true, fmt.Errorf("write skill scan cache: %w", err)
	}
	backupDir := ""
	if _, err := os.Stat(targetDir); err == nil {
		backupDir = targetDir + ".bak-" + shortRandomHex()
		if err := os.Rename(targetDir, backupDir); err != nil {
			return true, fmt.Errorf("backup existing dependency skill dir: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return true, fmt.Errorf("check existing dependency skill dir: %w", err)
	}
	if err := os.Rename(stagingDir, targetDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, targetDir)
		}
		return true, fmt.Errorf("replace dependency skill dir: %w", err)
	}
	cleanupStaging = false
	updated.SkillDir = targetDir
	if err := a.updateRegisteredMaclawAppDependencySkill(*updated); err != nil {
		_ = os.RemoveAll(targetDir)
		if backupDir != "" {
			_ = os.Rename(backupDir, targetDir)
		}
		return true, err
	}
	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	if a.hubUpdCache != nil {
		a.hubUpdCache.invalidate()
	}
	dep.InstalledVersion = ""
	dep.VersionStatus = ""
	return true, nil
}

func (a *App) updateRegisteredMaclawAppDependencySkill(updated corelib.NLSkillEntry) error {
	if a == nil || a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	a.skillExecutor.mu.Lock()
	defer a.skillExecutor.mu.Unlock()
	skills := a.skillExecutor.loadSkills()
	for i, existing := range skills {
		if existing.Name != updated.Name {
			continue
		}
		updated.Status = firstNonEmpty(updated.Status, existing.Status)
		updated.CreatedAt = firstNonEmpty(existing.CreatedAt, updated.CreatedAt)
		updated.UsageCount = existing.UsageCount
		updated.SuccessCount = existing.SuccessCount
		updated.FailureCount = existing.FailureCount
		updated.LastUsedAt = existing.LastUsedAt
		updated.LastError = existing.LastError
		if isShellBrowserAutomationSkillEntry(updated) {
			return browserAutomationSkillRejectedError(updated.Name)
		}
		skills[i] = updated
		return a.skillExecutor.saveSkills(skills)
	}
	return fmt.Errorf("skill %q not found", updated.Name)
}

func maclawAppBundledDependenciesForPackageJSON(packageJSON string) maclawAppBundledDependencies {
	var doc map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &doc); err != nil {
		return maclawAppBundledDependencies{}
	}
	return maclawAppBundledDependenciesFromDoc(doc)
}

func maclawAppBundledDependenciesFromDoc(doc map[string]any) maclawAppBundledDependencies {
	out := maclawAppBundledDependencies{Schema: "maclaw.app.bundled_dependencies.v1"}
	seen := map[string]struct{}{}
	add := func(raw any) {
		block := anyMap(raw)
		if block == nil {
			return
		}
		for _, item := range anySlice(block["skills"]) {
			itemMap := anyMap(item)
			if itemMap == nil {
				continue
			}
			files := map[string]string{}
			if fileMap := anyMap(itemMap["files"]); fileMap != nil {
				for path, value := range fileMap {
					if s := strings.TrimSpace(stringFromAny(value)); s != "" {
						files[path] = s
					}
				}
			}
			if len(files) == 0 {
				continue
			}
			entry := maclawAppBundledSkillEntry{
				StableID:    stringMapValue(itemMap, "stable_id"),
				ID:          stringMapValue(itemMap, "id"),
				Name:        stringMapValue(itemMap, "name"),
				Version:     stringMapValue(itemMap, "version"),
				Source:      stringMapValue(itemMap, "source"),
				HubSkillID:  stringMapValue(itemMap, "hub_skill_id"),
				HubVersion:  stringMapValue(itemMap, "hub_version"),
				CanonicalID: stringMapValue(itemMap, "canonical_id"),
				SHA256:      stringMapValue(itemMap, "sha256"),
				Files:       files,
				AppIDs:      stringSliceFromAny(itemMap["app_ids"]),
			}
			key := strings.ToLower(strings.TrimSpace(firstNonEmpty(entry.StableID, entry.HubSkillID, entry.CanonicalID, entry.ID, entry.Name)))
			if key == "" {
				key = fmt.Sprintf("sha256:%s", strings.ToLower(strings.TrimSpace(entry.SHA256)))
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out.Skills = append(out.Skills, entry)
		}
	}
	add(doc["bundled_dependencies"])
	for _, rawEntry := range anySlice(doc["apps"]) {
		if entry := anyMap(rawEntry); entry != nil {
			add(entry["bundled_dependencies"])
		}
	}
	return out
}

func maclawAppBundledSkillMatchesDependency(skill maclawAppBundledSkillEntry, dep maclawAppInstallPlanDependency) bool {
	needles := []string{dep.ID, dep.CanonicalID, dep.InstallRefTarget, dep.InstalledName}
	if resolved, ok := maclawAppImplicitHubSkillResolution(dep); ok {
		needles = append(needles, resolved.Target)
		needles = append(needles, resolved.LocalNames...)
		needles = append(needles, resolved.Aliases...)
	}
	haystack := []string{skill.ID, skill.Name, skill.CanonicalID, skill.HubSkillID, strings.TrimPrefix(skill.StableID, "hub_skill:"), strings.TrimPrefix(skill.StableID, "skill:"), strings.TrimPrefix(skill.StableID, "capability:")}
	for _, needle := range needles {
		needle = strings.TrimSpace(needle)
		if needle == "" {
			continue
		}
		for _, candidate := range haystack {
			if strings.EqualFold(strings.TrimSpace(candidate), needle) {
				return true
			}
		}
	}
	return false
}

func (a *App) installBundledMaclawAppSkill(bundle maclawAppBundledSkillEntry) error {
	if a == nil {
		return fmt.Errorf("app is not initialized")
	}
	a.ensureInteractionInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	if strings.TrimSpace(bundle.Name) == "" {
		return fmt.Errorf("bundled skill name is required")
	}
	if len(bundle.Files) == 0 {
		return fmt.Errorf("bundled skill %q has no files", bundle.Name)
	}
	if a.skillNameAlreadyRegistered(bundle.Name) {
		return fmt.Errorf("skill %q already exists", bundle.Name)
	}
	tmpDir, err := os.MkdirTemp("", "maclaw-app-bundled-skill-*")
	if err != nil {
		return fmt.Errorf("create bundled skill temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := maclawAppExtractBundledSkillFiles(bundle, tmpDir); err != nil {
		return err
	}
	entry, err := loadImportedSkillEntry(tmpDir)
	if err != nil {
		return fmt.Errorf("load bundled skill definition: %w", err)
	}
	if strings.TrimSpace(entry.Name) == "" {
		entry.Name = bundle.Name
	}
	if !strings.EqualFold(strings.TrimSpace(entry.Name), strings.TrimSpace(bundle.Name)) {
		return fmt.Errorf("bundled skill name mismatch: package=%q definition=%q", bundle.Name, entry.Name)
	}
	entry.HubSkillID = firstNonEmpty(entry.HubSkillID, bundle.HubSkillID, bundle.CanonicalID, bundle.ID)
	entry.HubVersion = firstNonEmpty(entry.HubVersion, bundle.HubVersion, bundle.Version)
	if strings.TrimSpace(entry.HubSkillID) != "" {
		entry.Source = skillEntrySourceHub.String()
	} else {
		entry.Source = "maclaw_app_bundle"
	}
	entry.SkillDir = tmpDir
	report, err := a.scanAndAdmitSkillBeforeRegister(context.Background(), entry, "maclaw app bundled dependency")
	if err != nil {
		return err
	}
	primaryDir, err := a.primarySkillsDir()
	if err != nil {
		return fmt.Errorf("resolve primary skills directory: %w", err)
	}
	if err := os.MkdirAll(primaryDir, 0o755); err != nil {
		return fmt.Errorf("create primary skills directory: %w", err)
	}
	destDir := filepath.Join(primaryDir, entry.Name)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("skill %q already exists", entry.Name)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check bundled skill destination: %w", err)
	}
	if err := copySkillPackageRootAtomically(tmpDir, destDir, primaryDir); err != nil {
		return err
	}
	installedEntry := *entry
	installedEntry.SkillDir = destDir
	if err := writeSkillScanCacheForInstalledEntry(&installedEntry, report); err != nil {
		_ = os.RemoveAll(destDir)
		return fmt.Errorf("write skill scan cache: %w", err)
	}
	if err := a.skillExecutor.Register(installedEntry); err != nil {
		_ = os.RemoveAll(destDir)
		return err
	}
	a.emitSkillInstallProgress(installedEntry.Name, "done", "Bundled dependency skill installed successfully.", report)
	return nil
}

func maclawAppExtractBundledSkillFiles(bundle maclawAppBundledSkillEntry, destDir string) error {
	if strings.TrimSpace(destDir) == "" {
		return fmt.Errorf("bundled skill destination is empty")
	}
	total := int64(0)
	hasher := sha256.New()
	paths := make([]string, 0, len(bundle.Files))
	for path := range bundle.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if !maclawAppBundledSkillFilePathOK(rel) {
			return fmt.Errorf("bundled skill contains unsafe path %q", rel)
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(bundle.Files[rel]))
		if err != nil {
			return fmt.Errorf("decode bundled skill file %q: %w", rel, err)
		}
		total += int64(len(data))
		if total > maxMaclawAppBundledSkillBytes {
			return fmt.Errorf("bundled skill expands to too much data: %d > %d bytes", total, maxMaclawAppBundledSkillBytes)
		}
		target := filepath.Join(destDir, filepath.FromSlash(rel))
		if !strings.HasPrefix(skillDirIdentityKey(target), skillDirIdentityKey(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("bundled skill path escapes destination: %q", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create bundled skill directory: %w", err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write bundled skill file %q: %w", rel, err)
		}
		hasher.Write([]byte(filepath.ToSlash(rel)))
		hasher.Write([]byte{0})
		hasher.Write(data)
		hasher.Write([]byte{0})
	}
	if expected := strings.TrimSpace(bundle.SHA256); expected != "" && !strings.EqualFold(expected, hex.EncodeToString(hasher.Sum(nil))) {
		return fmt.Errorf("bundled skill %q checksum mismatch", bundle.Name)
	}
	return nil
}

// maclawAppSourceAllowsImplicitHubResolution reports whether a dependency source
// value allows the alias registry and implicit hub target resolution to resolve
// the dependency name to an installable target. This is the single source of
// truth for the source whitelist used by maclawAppImplicitHubSkillResolution,
// maclawAppDependencySupportsHubCenterLookup, and the alias registry Sources.
func maclawAppSourceAllowsImplicitHubResolution(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "local", "hub", "skillhub", "market", "skillmarket", "hubcenter":
		return true
	default:
		return false
	}
}

func hasMissingMaclawAppRequiredDependencyForApp(deps []maclawAppInstallPlanDependency, appID string) bool {
	for _, dep := range deps {
		if dep.Required && !dep.Installed && maclawAppPlanDependencyMatchesAppID(dep, appID) {
			return true
		}
	}
	return false
}

func hasBlockingMaclawAppRequiredDependencyForApp(deps []maclawAppInstallPlanDependency, appID string) bool {
	for _, dep := range deps {
		if maclawAppPlanDependencyMatchesAppID(dep, appID) && maclawAppDependencyBlocksInstall(dep) {
			return true
		}
	}
	return false
}

func maclawAppPlanDependencyMatchesAppID(dep maclawAppInstallPlanDependency, appID string) bool {
	if len(dep.AppIDs) == 0 {
		return false
	}
	for _, candidate := range dep.AppIDs {
		if maclawAppIDsMatch(candidate, appID) {
			return true
		}
	}
	return false
}
