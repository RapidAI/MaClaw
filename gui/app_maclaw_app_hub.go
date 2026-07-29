package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	maclawappcontract "github.com/RapidAI/CodeClaw/internal/maclawappcontract"
)

// Guards read-modify-write of app_market_submissions.json.
var maclawAppSubmissionQueueMu sync.RWMutex

// DownloadMaclawAppPackageFromHub downloads an approved MaClaw App package from
// the configured Enterprise Hub capability market.
func (a *App) DownloadMaclawAppPackageFromHub(capabilityID string) (map[string]any, error) {
	capabilityID = strings.TrimSpace(capabilityID)
	if capabilityID == "" {
		return nil, fmt.Errorf("capability_id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	base := capabilityMarketBaseURL(cfg)
	token := capabilityMarketAuthToken(cfg)
	if base == "" || token == "" {
		return nil, fmt.Errorf("enterprise Hub marketplace URL or auth token is not configured")
	}
	pkg, err := maclawappcontract.DownloadGUIInstallHubPackage(context.Background(), &http.Client{Timeout: 60 * time.Second}, base, token, capabilityID)
	if err != nil {
		return nil, err
	}
	// Verify signature against the published package first (package_sha256 field),
	// then soft-repair legacy missing resolved_dependencies for install planning.
	signedPackageSHA := strings.TrimSpace(maclawAppStringValue(pkg, "package_sha256", "packageSha256", "package_sha"))
	trustedFingerprints := []string{}
	if fingerprint, err := verifyMaclawAppHubPackageSignature(pkg); err != nil {
		return nil, err
	} else if fingerprint != "" {
		trustedFingerprints = append(trustedFingerprints, fingerprint)
		if err := a.mergeTrustedSkillPackageKeyFingerprint(fingerprint); err != nil {
			return nil, err
		}
	}
	resolvedSynthesized, normalizeNotes := maclawappcontract.NormalizeGUIInstallHubPackage(pkg)
	if resolvedSynthesized {
		log.Printf("[maclaw-app] downloaded package %s: %s", capabilityID, strings.Join(normalizeNotes, "; "))
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, true)
	if err != nil {
		return nil, err
	}
	if err := validateDownloadedMaclawAppHubPackageGovernance(pkg, entries, capabilityID); err != nil {
		return nil, err
	}
	packageJSON, err := maclawAppStableJSON(pkg)
	if err != nil {
		return nil, err
	}
	// Prefer Hub-declared package_sha256 (signed). Skip full re-hash when signed
	// so synthesis-mutated maps do not invent a new checksum; size from JSON.
	packageSHA := signedPackageSHA
	packageSize := len(packageJSON)
	if packageSHA == "" {
		packageSHA, packageSize, err = maclawAppPackageFingerprint(pkg)
		if err != nil {
			return nil, err
		}
	}
	appIDs := make([]string, 0, len(entries))
	appNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		appIDs = append(appIDs, entry.ID)
		appNames = append(appNames, entry.Name)
	}
	return map[string]any{
		"schema":                            "maclaw.app.hub_package_download.v1",
		"capability_id":                     capabilityID,
		"package":                           pkg,
		"package_json":                      packageJSON,
		"package_sha256":                    packageSHA,
		"package_bytes":                     packageSize,
		"app_count":                         len(entries),
		"app_ids":                           appIDs,
		"app_names":                         appNames,
		"trusted_package_key_fingerprints":  trustedFingerprints,
		"downloaded_from":                   "enterprise_hub",
		"resolved_dependencies_synthesized": resolvedSynthesized,
		"compatibility_notes":               normalizeNotes,
	}, nil
}

func validateDownloadedMaclawAppHubPackageGovernance(pkg map[string]any, entries []parsedMaclawAppEntry, capabilityID string) error {
	if err := maclawappcontract.ValidateGUIInstallHubPackage(pkg, capabilityID); err != nil {
		return err
	}
	if len(anyMap(pkg["package_signature"])) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing package_signature")
	}
	if strings.TrimSpace(maclawAppStringValue(pkg, "package_sha256", "packageSha256")) == "" {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing package_sha256")
	}
	packageCapabilityID := firstNonEmptyMaclawAppString(
		maclawAppStringValue(pkg, "capability_id", "capabilityId"),
		maclawAppStringValue(anyMap(pkg["capability"]), "id", "capability_id", "capabilityId"),
	)
	if packageCapabilityID != "" && capabilityID != "" && packageCapabilityID != capabilityID {
		return fmt.Errorf("downloaded maclaw app package capability_id %q does not match requested capability_id %q", packageCapabilityID, capabilityID)
	}
	if capabilityStatus := strings.TrimSpace(maclawAppStringValue(anyMap(pkg["capability"]), "status", "state")); capabilityStatus != "" && capabilityStatus != "published" {
		return fmt.Errorf("downloaded maclaw app package capability status must be published, got %q", capabilityStatus)
	}
	if len(entries) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub has no apps")
	}
	for _, entry := range entries {
		appID := firstNonEmptyMaclawAppString(entry.ID, entry.Name)
		submission := maclawAppSubmissionMetadataForEntry(entry)
		if len(submission) == 0 {
			return fmt.Errorf("downloaded maclaw app %q is missing Hub governance submission", appID)
		}
		status := firstNonEmptyMaclawAppString(
			maclawAppStringValue(submission, "status", "review_status", "reviewStatus", "state"),
			maclawAppStringValue(anyMap(submission["review"]), "status", "state"),
		)
		if status != "published" {
			return fmt.Errorf("downloaded maclaw app %q Hub submission status must be published, got %q", appID, status)
		}
		submissionCapabilityID := firstNonEmptyMaclawAppString(
			maclawAppStringValue(submission, "capability_id", "capabilityId"),
			packageCapabilityID,
		)
		if submissionCapabilityID == "" {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing capability_id", appID)
		}
		if capabilityID != "" && submissionCapabilityID != capabilityID {
			return fmt.Errorf("downloaded maclaw app %q Hub submission capability_id %q does not match requested capability_id %q", appID, submissionCapabilityID, capabilityID)
		}
		if strings.TrimSpace(maclawAppStringValue(submission, "version_key", "versionKey")) == "" {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing version_key", appID)
		}
		if len(maclawAppReviewEvidenceForEntry(entry)) == 0 {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing review_evidence", appID)
		}
	}
	packageReviewEvidence := maclawAppReviewEvidenceFromMetadata(pkg)
	if len(packageReviewEvidence) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing package review_evidence")
	}
	return nil
}

func verifyMaclawAppHubPackageSignature(pkg map[string]any) (string, error) {
	return maclawappcontract.VerifyHubPackageSignature(pkg)
}

func (a *App) mergeTrustedSkillPackageKeyFingerprint(fingerprint string) error {
	fingerprint = normalizeDownloadedSkillPublicKeyFingerprint(fingerprint)
	if fingerprint == "" {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	for _, existing := range cfg.TrustedSkillPackageKeyFingerprints {
		if normalizeDownloadedSkillPublicKeyFingerprint(existing) == fingerprint {
			return nil
		}
	}
	cfg.TrustedSkillPackageKeyFingerprints = append(cfg.TrustedSkillPackageKeyFingerprints, fingerprint)
	_, err = a.PatchConfigFields(map[string]interface{}{
		"trusted_skill_package_key_fingerprints": cfg.TrustedSkillPackageKeyFingerprints,
	})
	return err
}

// InstallMaclawAppPackageFromHub downloads an approved MaClaw App from the
// Enterprise Hub, installs required Skill dependencies, and records install audit.
func (a *App) InstallMaclawAppPackageFromHub(capabilityID string) (map[string]any, error) {
	return a.InstallSelectedMaclawAppPackageFromHub(capabilityID, nil)
}

// InstallSelectedMaclawAppPackageFromHub installs only the selected apps from a
// Hub capability package. Empty appIDs preserves the legacy whole-package install.
func (a *App) InstallSelectedMaclawAppPackageFromHub(capabilityID string, appIDs []string) (map[string]any, error) {
	download, err := a.DownloadMaclawAppPackageFromHub(capabilityID)
	if err != nil {
		return nil, err
	}
	rawPackage, ok := download["package"].(map[string]any)
	if !ok || rawPackage == nil {
		return nil, fmt.Errorf("downloaded MaClaw App package is empty")
	}
	installPackage, entries, err := maclawAppPackageForSelectedAppIDs(rawPackage, appIDs)
	if err != nil {
		return nil, err
	}
	// Older published Hub packages can still declare a known public SkillMarket
	// dependency as source=local. The package signature and the Hub download
	// path have already been verified above, so upgrade only aliases from the
	// explicit compatibility registry. Unknown local dependencies deliberately
	// remain local: they must be restored locally or carried in the package.
	if upgraded := maclawAppUpgradeTrustedHubLocalDependenciesForSkillMarket(installPackage); upgraded > 0 {
		log.Printf("[maclaw-app] Hub package %s: upgraded %d known local dependency install target(s) to SkillMarket", capabilityID, upgraded)
	}
	packageJSON, err := maclawAppStableJSON(installPackage)
	if err != nil {
		return nil, err
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(installPackage)
	if err != nil {
		return nil, err
	}
	installPlan, err := a.InstallMaclawAppDependencies(packageJSON)
	if err != nil {
		return nil, err
	}
	if installPlan.HasWorkflowContractIssue && maclawAppWorkflowContractIssuesShouldPrecedeDependencyBlock(installPlan.WorkflowContractIssues, installPlan.HasMissingRequired || installPlan.HasBlockingDependency) {
		return nil, fmt.Errorf("cannot install MaClaw App from Hub: approval workflow contract is invalid: %s", firstMaclawAppReviewIssueMessage(installPlan.WorkflowContractIssues, "approval workflow contract issue"))
	}
	if installPlan.HasMissingRequired || installPlan.HasBlockingDependency {
		if detail := maclawAppInstallPlanBlockingDependencySummary(installPlan); detail != "" {
			return nil, fmt.Errorf("cannot install MaClaw App from Hub: required Skill dependencies are missing or unavailable: %s", detail)
		}
		return nil, fmt.Errorf("cannot install MaClaw App from Hub: required Skill dependencies are missing or unavailable")
	}
	// Governance review issues are informational at install time — the Hub
	// publish/approval endpoint is the authoritative enforcement point.
	// Blocking installation here would prevent users from installing apps that
	// passed Hub review under older governance requirements or whose governance
	// metadata was stripped during download.
	if installPlan.HasGovernanceReviewIssue {
		log.Printf("[maclaw-app] install governance review warning (non-blocking): %s", firstMaclawAppReviewIssueMessage(installPlan.GovernanceReviewIssues, "governance review issue"))
	}
	installRecord, err := a.recordMaclawAppInstall(packageJSON, "enterprise_hub", &installPlan)
	if err != nil {
		return nil, err
	}
	appIDsOut := make([]string, 0, len(entries))
	appNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		appIDsOut = append(appIDsOut, entry.ID)
		appNames = append(appNames, entry.Name)
	}
	return map[string]any{
		"schema":           "maclaw.app.hub_install.v1",
		"capability_id":    strings.TrimSpace(capabilityID),
		"package":          installPackage,
		"package_json":     packageJSON,
		"package_sha":      packageSHA,
		"package_sha256":   packageSHA,
		"package_bytes":    packageSize,
		"source_app_count": download["app_count"],
		"source_app_ids":   download["app_ids"],
		"app_count":        len(entries),
		"app_ids":          appIDsOut,
		"app_names":        appNames,
		"install_plan":     installPlan,
		"install_record":   installRecord,
	}, nil
}

// maclawAppUpgradeTrustedHubLocalDependenciesForSkillMarket repairs legacy Hub
// package metadata for a deliberately small compatibility set. It is called
// only after DownloadMaclawAppPackageFromHub has authenticated, signature-
// verified, and governance-validated the package. It keeps the declared skill
// identity and version, but normalizes its legacy local source and install
// metadata to the receiver-side SkillMarket target used by the install planner.
//
// A source=local dependency is not generally safe to download. We only upgrade
// it when its ID/alias and kind match a registry entry explicitly authorized for
// both local legacy declarations and SkillMarket installation. This lets older
// published apps recover without making private local dependencies network
// installable.
func maclawAppUpgradeTrustedHubLocalDependenciesForSkillMarket(pkg map[string]any) int {
	// Legacy Hub packages are permitted to omit source. This helper is only
	// reached from the authenticated, signed, and governance-validated Hub
	// download path, but still refuses a package that explicitly claims a
	// different source.
	source := strings.TrimSpace(maclawAppStringValue(pkg, "source"))
	if len(pkg) == 0 || (source != "" && !strings.EqualFold(source, "enterprise_hub")) {
		return 0
	}
	upgraded := 0
	for _, raw := range anySlice(pkg["resolved_dependencies"]) {
		if maclawAppUpgradeTrustedHubLocalDependencyMapForSkillMarket(anyMap(raw)) {
			upgraded++
		}
	}
	for _, rawEntry := range anySlice(pkg["apps"]) {
		entry := anyMap(rawEntry)
		if entry == nil {
			continue
		}
		for _, rawDep := range anySlice(entry["resolved_dependencies"]) {
			depMap := anyMap(rawDep)
			if maclawAppUpgradeTrustedHubLocalDependencyMapForSkillMarket(depMap) {
				upgraded++
			}
		}
		app := anyMap(entry["app"])
		for _, holder := range []map[string]any{anyMap(app["binding"]), app} {
			if holder == nil {
				continue
			}
			if maclawAppUpgradeTrustedHubLocalDependencyMapForSkillMarket(anyMap(holder["skill"])) {
				upgraded++
			}
			for _, key := range []string{"appSkill", "app_skill"} {
				if maclawAppUpgradeTrustedHubLocalDependencyMapForSkillMarket(anyMap(holder[key])) {
					upgraded++
				}
			}
			dependencies := anyMap(holder["dependencies"])
			for _, depMap := range append(maclawAppDependencyMaps(dependencies["skills"]), maclawAppDependencyMaps(dependencies["skill"])...) {
				if maclawAppUpgradeTrustedHubLocalDependencyMapForSkillMarket(depMap) {
					upgraded++
				}
			}
		}
	}
	return upgraded
}

func maclawAppUpgradeTrustedHubLocalDependencyMapForSkillMarket(depMap map[string]any) bool {
	if depMap == nil || !strings.EqualFold(strings.TrimSpace(maclawAppStringValue(depMap, "source")), "local") {
		return false
	}
	dep := maclawAppInstallPlanDependency{
		ID:      maclawAppStringValue(depMap, "id", "skill_id", "skillId", "name"),
		Version: maclawAppStringValue(depMap, "version"),
		Kind:    maclawAppStringValue(depMap, "kind"),
		Source:  "local",
		Aliases: maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(depMap["aliases"], depMap["install_aliases"], depMap["installAliases"])),
	}
	target, ok := maclawAppTrustedHubLocalDependencySkillMarketTarget(dep)
	if !ok {
		return false
	}
	depMap["source"] = "skillmarket"
	depMap["canonical_id"] = target
	depMap["install_ref_kind"] = "skillmarket"
	depMap["install_ref_target"] = target
	if version := strings.TrimSpace(dep.Version); version != "" {
		depMap["install_ref_version"] = version
		depMap["install_ref"] = "skillmarket://skills/" + target + "@" + version
	} else {
		depMap["install_ref"] = "skillmarket://skills/" + target
	}
	return true
}

func maclawAppTrustedHubLocalDependencySkillMarketTarget(dep maclawAppInstallPlanDependency) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(dep.Source), "local") || !maclawAppDependencyKindAllowsImplicitHubTarget(dep.Kind) {
		return "", false
	}
	values := append([]string{dep.ID}, dep.Aliases...)
	for _, entry := range maclawAppDependencyAliasRegistry {
		if !maclawAppDependencyAliasRegistryAllows(entry.Sources, "local") || !maclawAppDependencyAliasRegistryAllows(entry.Sources, "skillmarket") || !maclawAppDependencyAliasRegistryAllows(entry.Kinds, dep.Kind) {
			continue
		}
		for _, value := range values {
			if maclawAppDependencyAliasMatches(value, entry.Target, entry.Aliases, entry.LocalNames) {
				return entry.Target, true
			}
		}
	}
	return "", false
}

// SubmitMaclawAppPackage stores a maclaw.app.pack.v1 submission in the local
// durable queue. Enterprise Hub upload can later consume the same package JSON.
func (a *App) SubmitMaclawAppPackage(packageJSON string) (map[string]any, error) {
	pkg, appIDs, appNames, err := parseMaclawAppPackage(packageJSON)
	if err != nil {
		return nil, err
	}
	plan, err := a.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		return nil, err
	}

	// Enrich dependencies with HubSkillID from locally installed skills and
	// stamp into the package so receivers get deterministic install refs.
	dependencies := cloneMaclawAppPlanDependencies(plan.Dependencies)
	a.enrichDependenciesWithHubSkillID(dependencies)
	if enrichedDeps := maclawAppSerializableResolvedDeps(dependencies); len(enrichedDeps) > 0 {
		// Package-level (used for local queue, pre-Hub local install).
		pkg["resolved_dependencies"] = enrichedDeps
		// Entry-level (survives Hub storage which only persists per-entry ManifestJSON).
		injectResolvedDepsIntoAppEntries(pkg, enrichedDeps)
	}
	if bundledDeps := a.maclawAppBundledDependenciesForPlan(dependencies); len(bundledDeps.Skills) > 0 {
		pkg["bundled_dependencies"] = bundledDeps
		injectBundledDepsIntoAppEntries(pkg, bundledDeps)
	}

	// Compute fingerprint AFTER enrichment so the receiver's integrity check
	// matches what was actually uploaded (including resolved_dependencies).
	packageSHA, packageSize, err := maclawAppPackageFingerprint(pkg)
	if err != nil {
		return nil, err
	}

	reviewIssues := maclawAppReadyReviewIssuesForPackage(pkg, plan)
	if issue := firstBlockingMaclawAppReviewIssue(reviewIssues); issue != nil {
		return nil, fmt.Errorf("maclaw app package is not ready for submission: %s: %s", issue.Path, issue.Message)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	submissionID := "local-review-" + firstMaclawAppID(appIDs) + "-" + shortRandomHex()
	record := maclawAppSubmissionRecord{
		SubmissionID: submissionID,
		SubmittedAt:  now,
		Status:       "submitted",
		Channel:      "local",
		AppIDs:       appIDs,
		AppNames:     appNames,
		PackageSHA:   packageSHA,
		PackageSize:  packageSize,
		Dependencies: cloneMaclawAppPlanDependencies(dependencies),
		ReviewIssues: cloneMaclawAppReviewIssues(reviewIssues),
		Package:      pkg,
		Message:      "queued locally for enterprise market sync",
	}
	record.Events = appendMaclawAppSubmissionEvent(record.Events, record.maclawAppSubmissionEvent(now))
	if err := a.appendMaclawAppSubmission(record); err != nil {
		return nil, err
	}
	return map[string]any{
		"submission_id":       submissionID,
		"submitted_at":        now,
		"status":              record.Status,
		"channel":             record.Channel,
		"app_ids":             append([]string(nil), record.AppIDs...),
		"app_names":           append([]string(nil), record.AppNames...),
		"package_sha":         record.PackageSHA,
		"package_sha256":      record.PackageSHA,
		"package_bytes":       record.PackageSize,
		"dependencies":        cloneMaclawAppPlanDependencies(record.Dependencies),
		"dependency_count":    len(record.Dependencies),
		"submission_evidence": maclawAppSubmissionEvidenceForRecord(record),
		"review_evidence":     maclawAppSubmissionReviewEvidenceForRecord(record),
		"review_issues":       cloneMaclawAppReviewIssues(record.ReviewIssues),
		"review_issue_count":  len(record.ReviewIssues),
		"message":             record.Message,
	}, nil
}

// ListMaclawAppPackageSubmissions returns newest-first submission summaries
// without exposing full package payloads.
func (a *App) ListMaclawAppPackageSubmissions(limit int) ([]maclawAppSubmissionSummary, error) {
	queue, err := a.readMaclawAppSubmissionQueue()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if limit > len(queue.Submissions) {
		limit = len(queue.Submissions)
	}
	summaries := make([]maclawAppSubmissionSummary, 0, limit)
	for _, record := range queue.Submissions[:limit] {
		summaries = append(summaries, record.maclawAppSubmissionSummary())
	}
	return summaries, nil
}

// GetMaclawAppPackageSubmission returns a full queued submission, including the
// maclaw.app.pack.v1 package payload, for sync workers and audit diagnostics.
func (a *App) GetMaclawAppPackageSubmission(submissionID string) (*maclawAppSubmissionRecord, error) {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return nil, fmt.Errorf("submission_id is required")
	}
	queue, err := a.readMaclawAppSubmissionQueue()
	if err != nil {
		return nil, err
	}
	for _, record := range queue.Submissions {
		if record.SubmissionID != submissionID {
			continue
		}
		cloned := record
		cloned.AppIDs = append([]string(nil), record.AppIDs...)
		cloned.AppNames = append([]string(nil), record.AppNames...)
		cloned.ApprovedScopes = append([]string(nil), record.ApprovedScopes...)
		cloned.ReviewIssues = cloneMaclawAppReviewIssues(record.ReviewIssues)
		cloned.Dependencies = cloneMaclawAppPlanDependencies(record.Dependencies)
		cloned.Events = append([]maclawAppSubmissionEvent(nil), record.Events...)
		cloned.Package = cloneMapAny(record.Package)
		if cloned.PackageSHA == "" || cloned.PackageSize == 0 {
			packageSHA, packageSize, _ := maclawAppPackageFingerprint(cloned.Package)
			cloned.PackageSHA = packageSHA
			cloned.PackageSize = packageSize
		}
		return &cloned, nil
	}
	return nil, nil
}

// SyncMaclawAppPackageSubmissionToHub uploads one local maclaw.app.pack.v1
// submission to the configured Enterprise Hub and records the Hub review state.
func (a *App) SyncMaclawAppPackageSubmissionToHub(submissionID string) (map[string]any, error) {
	return a.syncMaclawAppPackageSubmissionToHub(context.Background(), submissionID)
}

// syncMaclawAppPackageSubmissionToHub is the context-aware Hub pack submit path
// used by one-click publish so the overall timeout can cancel the HTTP round-trip.
func (a *App) syncMaclawAppPackageSubmissionToHub(ctx context.Context, submissionID string) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := a.GetMaclawAppPackageSubmission(submissionID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("submission %s not found", strings.TrimSpace(submissionID))
	}
	if record.Channel != "" && record.Channel != "local" {
		return nil, fmt.Errorf("submission %s is already %s-backed", record.SubmissionID, record.Channel)
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	base := capabilityMarketBaseURL(cfg)
	token := capabilityMarketAuthToken(cfg)
	if base == "" || token == "" {
		return nil, fmt.Errorf("enterprise Hub marketplace URL or auth token is not configured")
	}

	if record.Package == nil {
		return nil, fmt.Errorf("submission %s has no package payload", record.SubmissionID)
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(record.Package)
	if err != nil {
		return nil, err
	}
	// Package payload is the upload source of truth for local-queue sync (this
	// function already rejects non-local channels). If the recorded SHA is stale
	// (enqueue-time enrichment, later queue edit, or re-fingerprint), refresh it
	// and continue instead of blocking Hub sync with a fingerprint mismatch.
	if record.PackageSHA != "" && !strings.EqualFold(strings.TrimSpace(record.PackageSHA), packageSHA) {
		oldSHA := strings.TrimSpace(record.PackageSHA)
		if err := a.refreshMaclawAppSubmissionPackageFingerprint(record.SubmissionID, packageSHA, packageSize); err != nil {
			return nil, fmt.Errorf("submission %s package fingerprint mismatch (recorded %s, current %s) and refresh failed: %w", record.SubmissionID, oldSHA, packageSHA, err)
		}
		record.PackageSHA = packageSHA
		if packageSize > 0 {
			record.PackageSize = packageSize
		}
		log.Printf("[maclaw-app-hub] submission %s package fingerprint refreshed: %s -> %s", record.SubmissionID, oldSHA, packageSHA)
	}
	packageJSON, err := maclawAppStableJSON(record.Package)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	plan, err := a.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		return nil, err
	}
	reviewIssues := maclawAppReadyReviewIssuesForPackage(record.Package, plan)
	if issue := firstBlockingMaclawAppReviewIssue(reviewIssues); issue != nil {
		return nil, fmt.Errorf("maclaw app package is not ready for Hub sync: %s: %s", issue.Path, issue.Message)
	}

	// Pre-upload gate: required skills must be bundled in the pack and/or
	// published (HubSkillID / remote install_ref) so receivers can install them.
	if gateErr := a.validateAppDependenciesPublished(plan, record.Package); gateErr != nil {
		return nil, gateErr
	}

	body, err := json.Marshal(map[string]any{
		"package":              record.Package,
		"source_submission_id": record.SubmissionID,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/capabilities/maclaw-apps/submit", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	timeout, err := maclawAppHubSubmitHTTPTimeout(ctx, 60*time.Second)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("submit maclaw app package to enterprise Hub failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var hubResp maclawAppHubSubmissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&hubResp); err != nil {
		return nil, err
	}
	if hubResp.Schema != "maclaw.app.hub_submission.v1" {
		return nil, fmt.Errorf("unexpected maclaw app hub submission response schema %q", hubResp.Schema)
	}
	hubSubmissionID, hubCapabilityID, hubPackageSHA, err := resolveMaclawAppHubSubmissionIdentity(hubResp, record.PackageSHA)
	if err != nil {
		return nil, err
	}
	status := normalizeMaclawAppSubmissionStatus(hubResp.Status)
	if status == "" {
		status = "pending_review"
	}
	message := fmt.Sprintf("submitted to enterprise Hub for review (%d app)", hubResp.AppCount)
	if hubResp.AppCount != 1 {
		message = fmt.Sprintf("submitted to enterprise Hub for review (%d apps)", hubResp.AppCount)
	}
	ok, err := a.UpdateMaclawAppPackageSubmissionStatus(record.SubmissionID, maclawAppSubmissionStatusUpdate{
		Status:          status,
		Channel:         "hub",
		SubmissionID:    hubSubmissionID,
		HubCapabilityID: hubCapabilityID,
		Message:         message,
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("submission %s disappeared before Hub sync status update", record.SubmissionID)
	}
	return map[string]any{
		"submission_id":        hubSubmissionID,
		"source_submission_id": record.SubmissionID,
		"hub_capability_id":    hubCapabilityID,
		"status":               status,
		"channel":              "hub",
		"package_sha":          hubPackageSHA,
		"package_sha256":       hubPackageSHA,
		"app_count":            hubResp.AppCount,
		"submissions":          hubResp.Submissions,
		"message":              message,
	}, nil
}

// RefreshMaclawAppPackageSubmissionFromHub pulls the latest Hub review state for
// a hub-backed MaClaw App package submission and updates the local durable queue.
func (a *App) RefreshMaclawAppPackageSubmissionFromHub(submissionID string) (map[string]any, error) {
	record, err := a.GetMaclawAppPackageSubmission(submissionID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("submission %s not found", strings.TrimSpace(submissionID))
	}
	if record.Channel != "hub" {
		return nil, fmt.Errorf("submission %s is not hub-backed", record.SubmissionID)
	}
	capabilityID := strings.TrimSpace(record.HubCapabilityID)
	if capabilityID == "" {
		capabilityID = firstNonEmptyMaclawAppString(firstString(record.AppIDs), record.SubmissionID)
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	base := capabilityMarketBaseURL(cfg)
	token := capabilityMarketAuthToken(cfg)
	if base == "" || token == "" {
		return nil, fmt.Errorf("enterprise Hub marketplace URL or auth token is not configured")
	}
	req, err := http.NewRequest(http.MethodGet, base+"/api/capabilities/"+url.PathEscape(capabilityID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("refresh maclaw app package submission from enterprise Hub failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var detail maclawAppHubCapabilityDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	metadata := map[string]any{}
	if strings.TrimSpace(detail.MetadataJSON) != "" {
		_ = json.Unmarshal([]byte(detail.MetadataJSON), &metadata)
	}
	status := normalizeMaclawAppSubmissionStatus(firstNonEmptyMaclawAppString(maclawAppStringFromAny(metadata["review_state"]), detail.Status))
	if status == "" {
		status = "pending_review"
	}
	message := "enterprise Hub review state refreshed"
	if status == "approved" || status == "published" {
		message = "enterprise Hub approved this MaClaw App"
	} else if status == "review_failed" {
		message = "enterprise Hub rejected this MaClaw App"
	}
	ok, err := a.UpdateMaclawAppPackageSubmissionStatus(record.SubmissionID, maclawAppSubmissionStatusUpdate{
		Status:          status,
		Channel:         "hub",
		HubCapabilityID: firstNonEmptyMaclawAppString(detail.ID, detail.CapabilityID, record.HubCapabilityID),
		ReviewedAt:      firstNonEmptyMaclawAppString(maclawAppStringFromAny(metadata["reviewed_at"]), maclawAppStringFromAny(metadata["approved_at"])),
		PublishedAt:     maclawAppStringFromAny(metadata["published_at"]),
		Reviewer:        maclawAppStringFromAny(metadata["reviewer"]),
		RiskLevel:       maclawAppStringFromAny(metadata["risk_level"]),
		ApprovedScopes:  maclawAppStringSliceFromAny(metadata["approved_scopes"]),
		ReviewIssues:    maclawAppReviewIssuesFromAny(metadata["review_issues"]),
		Message:         message,
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("submission %s disappeared before Hub refresh status update", record.SubmissionID)
	}
	return map[string]any{
		"submission_id":     record.SubmissionID,
		"hub_capability_id": firstNonEmptyMaclawAppString(detail.ID, detail.CapabilityID, record.HubCapabilityID),
		"status":            status,
		"channel":           "hub",
		"message":           message,
	}, nil
}

// WithdrawMaclawAppPackageSubmission removes a local pending submission from the
// durable queue. Hub-backed submissions must be withdrawn through the market.
func (a *App) WithdrawMaclawAppPackageSubmission(submissionID string) (bool, error) {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return false, fmt.Errorf("submission_id is required")
	}
	maclawAppSubmissionQueueMu.Lock()
	defer maclawAppSubmissionQueueMu.Unlock()
	queue, err := a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		return false, err
	}
	next := queue.Submissions[:0]
	removed := false
	for _, record := range queue.Submissions {
		if record.SubmissionID == submissionID {
			if record.Channel != "" && record.Channel != "local" {
				return false, fmt.Errorf("submission %s is %s-backed and cannot be removed from the local queue", submissionID, record.Channel)
			}
			removed = true
			continue
		}
		next = append(next, record)
	}
	if !removed {
		return false, nil
	}
	queue.Submissions = next
	queue.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return true, a.writeMaclawAppSubmissionQueueUnlocked(queue)
}

// UpdateMaclawAppPackageSubmissionStatus updates a queued submission after a
// sync worker or enterprise market review callback reports a new state.
func (a *App) UpdateMaclawAppPackageSubmissionStatus(submissionID string, update maclawAppSubmissionStatusUpdate) (bool, error) {
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return false, fmt.Errorf("submission_id is required")
	}
	status := normalizeMaclawAppSubmissionStatus(update.Status)
	if status == "" {
		return false, fmt.Errorf("invalid maclaw app submission status")
	}
	channel := strings.TrimSpace(update.Channel)
	if channel == "" {
		channel = "hub"
	}
	if channel != "local" && channel != "hub" {
		return false, fmt.Errorf("invalid maclaw app submission channel")
	}
	maclawAppSubmissionQueueMu.Lock()
	defer maclawAppSubmissionQueueMu.Unlock()
	queue, err := a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		return false, err
	}
	for i := range queue.Submissions {
		if queue.Submissions[i].SubmissionID != submissionID {
			continue
		}
		if nextID := strings.TrimSpace(update.SubmissionID); nextID != "" {
			for j := range queue.Submissions {
				if j != i && queue.Submissions[j].SubmissionID == nextID {
					return false, fmt.Errorf("submission_id %s already exists", nextID)
				}
			}
			queue.Submissions[i].SubmissionID = nextID
		}
		now := time.Now().UTC().Format(time.RFC3339)
		queue.Submissions[i].Status = status
		queue.Submissions[i].Channel = channel
		if hubCapabilityID := strings.TrimSpace(update.HubCapabilityID); hubCapabilityID != "" {
			queue.Submissions[i].HubCapabilityID = hubCapabilityID
		}
		queue.Submissions[i].Message = strings.TrimSpace(update.Message)
		queue.Submissions[i].Reviewer = strings.TrimSpace(update.Reviewer)
		queue.Submissions[i].RiskLevel = normalizeMaclawAppRiskLevel(update.RiskLevel)
		queue.Submissions[i].ApprovedScopes = normalizeMaclawAppScopes(update.ApprovedScopes)
		queue.Submissions[i].ReviewIssues = normalizeMaclawAppReviewIssues(update.ReviewIssues)
		if reviewedAt := strings.TrimSpace(update.ReviewedAt); reviewedAt != "" {
			queue.Submissions[i].ReviewedAt = reviewedAt
		} else if status != "submitted" {
			queue.Submissions[i].ReviewedAt = now
		}
		if publishedAt := strings.TrimSpace(update.PublishedAt); publishedAt != "" {
			queue.Submissions[i].PublishedAt = publishedAt
		} else if status == "published" {
			queue.Submissions[i].PublishedAt = now
		}
		queue.Submissions[i].Events = appendMaclawAppSubmissionEvent(queue.Submissions[i].Events, queue.Submissions[i].maclawAppSubmissionEvent(now))
		queue.UpdatedAt = now
		return true, a.writeMaclawAppSubmissionQueueUnlocked(queue)
	}
	return false, nil
}

func maclawAppSubmissionPackageSHAsByAppID(entries []parsedMaclawAppEntry) map[string]string {
	out := map[string]string{}
	for _, entry := range entries {
		submission := maclawAppSubmissionMetadataForEntry(entry)
		if packageSHA := maclawAppStringValue(submission, "package_sha256", "packageSha256"); packageSHA != "" {
			out[strings.ToLower(strings.TrimSpace(entry.ID))] = packageSHA
		}
	}
	return out
}

func maclawAppSubmissionMetadataForEntry(entry parsedMaclawAppEntry) map[string]any {

	governance := anyMap(entry.App["governance"])

	if governance == nil {

		return nil

	}

	submission := anyMap(governance["submission"])

	if len(submission) == 0 {

		return nil

	}

	return cloneMapAny(submission)
}

func maclawAppSubmissionEvidenceForRecord(record maclawAppSubmissionRecord) map[string]any {
	if len(record.Package) == 0 {
		return nil
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(record.Package, false)
	if err != nil || len(entries) == 0 {
		return nil
	}
	return maclawAppInstallEvidenceByApp(entries, record.Dependencies, nil)
}

func maclawAppSubmissionReviewEvidenceForRecord(record maclawAppSubmissionRecord) map[string]any {
	if len(record.Package) == 0 {
		return nil
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(record.Package, false)
	if err != nil || len(entries) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		governance := maclawAppGovernanceMetadataForEntry(entry)
		rawGovernance := anyMap(entry.App["governance"])
		testEvidence := anyMap(governance["test_evidence"])
		approval := anyMap(firstNonEmptyMaclawAppAny(testEvidence["approvalInstance"], testEvidence["approval_instance"]))
		progress := maclawAppEvidenceMapSlice(firstNonEmptyMaclawAppAny(
			testEvidence["progressInstances"],
			testEvidence["progress_instances"],
			testEvidence["workflowProgress"],
			testEvidence["workflow_progress"],
			testEvidence["approvalProgress"],
			testEvidence["approval_progress"],
			firstNonEmptyMaclawAppAny(approval["progressInstances"], approval["progress_instances"], approval["workflowProgress"], approval["workflow_progress"]),
		))
		dependencyVerification := maclawAppDependencyVerificationMetadataForEntry(entry, record.Dependencies)
		workspaceLayout := maclawAppWorkspaceLayoutMetadataForEntry(entry)
		workspaceStudio := anyMap(workspaceLayout["studio"])
		workflowContract := maclawAppWorkflowContractForEntry(entry)
		resultContract := anyMap(firstNonEmptyMaclawAppAny(governance["result_contract"], rawGovernance["resultContract"], rawGovernance["result_contract"], entry.App["resultContract"], entry.App["result_contract"], anyMap(entry.App["binding"])["resultContract"], anyMap(entry.App["binding"])["result_contract"]))
		testProtocol := anyMap(firstNonEmptyMaclawAppAny(testEvidence["testProtocol"], testEvidence["test_protocol"], governance["test_protocol"], rawGovernance["testProtocol"], rawGovernance["test_protocol"]))
		resultCoverage := anyMap(firstNonEmptyMaclawAppAny(testEvidence["resultCoverage"], testEvidence["result_coverage"]))
		dataSrvRegistration := anyMap(firstNonEmptyMaclawAppAny(governance["datasrv_registration"], governance["datasrvRegistration"], governance["dataSrvRegistration"], rawGovernance["datasrv_registration"], rawGovernance["datasrvRegistration"], rawGovernance["dataSrvRegistration"], testEvidence["datasrv_registration"], testEvidence["datasrvRegistration"], testEvidence["dataSrvRegistration"]))
		approvalViews := maclawAppEvidenceStringSlice(firstNonEmptyMaclawAppAny(testEvidence["approvalViews"], testEvidence["approval_views"], testEvidence["viewVerified"], testEvidence["approvalInstanceViewVerified"]))
		out[entry.ID] = compactPayload(map[string]any{
			"app_id":                        entry.ID,
			"app_name":                      entry.Name,
			"app_kind":                      entry.Kind,
			"has_test_evidence":             len(testEvidence) > 0,
			"run_id":                        firstNonEmptyMaclawAppString(maclawAppStringValue(testEvidence, "runId"), maclawAppStringValue(testEvidence, "run_id")),
			"verified_at":                   firstNonEmptyMaclawAppString(maclawAppStringValue(testEvidence, "verifiedAt"), maclawAppStringValue(testEvidence, "verified_at")),
			"has_approval_instance":         len(approval) > 0,
			"approval_id":                   firstNonEmptyMaclawAppString(maclawAppStringValue(approval, "approval_id"), maclawAppStringValue(approval, "approvalId")),
			"approval_status":               firstNonEmptyMaclawAppString(maclawAppStringValue(approval, "status"), maclawAppStringValue(approval, "result_status"), maclawAppStringValue(approval, "resultStatus")),
			"current_node":                  firstNonEmptyMaclawAppString(maclawAppStringValue(approval, "workflow_node_id"), maclawAppStringValue(approval, "workflowNodeId"), maclawAppStringValue(approval, "current_node"), maclawAppStringValue(approval, "currentNode")),
			"progress_count":                len(progress),
			"approval_views":                approvalViews,
			"has_dependency_verification":   len(dependencyVerification) > 0,
			"dependency_count":              maclawAppReviewEvidenceNumber(firstNonEmptyMaclawAppAny(dependencyVerification["dependency_count"], dependencyVerification["dependencyCount"])),
			"has_blocking_dependency":       firstNonEmptyMaclawAppAny(dependencyVerification["has_blocking_dependency"], dependencyVerification["hasBlockingDependency"]),
			"has_workspace_layout":          len(workspaceLayout) > 0,
			"workspace_template":            firstNonEmptyMaclawAppString(maclawAppStringValue(workspaceLayout, "template"), maclawAppStringValue(workspaceLayout, "layout")),
			"workspace_saved_in_manifest":   firstNonEmptyMaclawAppAny(workspaceLayout["studio_saved_in_manifest"], workspaceStudio["savedInManifest"], workspaceStudio["saved_in_manifest"]),
			"workspace_studio_editable":     firstNonEmptyMaclawAppAny(workspaceLayout["studio_editable"], workspaceStudio["editable"]),
			"workspace_updated_by":          firstNonEmptyMaclawAppString(maclawAppStringValue(workspaceLayout, "studio_updated_by"), maclawAppStringValue(workspaceStudio, "updatedBy", "updated_by")),
			"datasrv_registration_status":   firstNonEmptyMaclawAppString(maclawAppStringValue(dataSrvRegistration, "status"), maclawAppStringValue(dataSrvRegistration, "state")),
			"has_workflow_contract":         len(workflowContract) > 0,
			"workflow_contract_version":     firstNonEmptyMaclawAppString(maclawAppStringValue(workflowContract, "version"), maclawAppStringValue(workflowContract, "schema")),
			"has_result_contract":           len(resultContract) > 0,
			"result_contract_primary":       firstNonEmptyMaclawAppString(maclawAppStringValue(resultContract, "primary"), maclawAppStringValue(resultContract, "primary_result")),
			"result_contract_type_count":    len(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(resultContract["types"], resultContract["result_types"]))),
			"has_test_protocol":             len(testProtocol) > 0,
			"test_protocol_fingerprint":     firstNonEmptyMaclawAppString(maclawAppStringValue(testProtocol, "fingerprint"), maclawAppStringValue(testEvidence, "testProtocolFingerprint", "test_protocol_fingerprint")),
			"result_coverage_ok":            firstNonEmptyMaclawAppAny(resultCoverage["ok"], resultCoverage["covered"]),
			"result_coverage_primary":       firstNonEmptyMaclawAppString(maclawAppStringValue(resultCoverage, "primary"), maclawAppStringValue(resultCoverage, "primary_result")),
			"result_coverage_covered_count": len(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(resultCoverage["coveredTypes"], resultCoverage["covered_types"]))),
			"result_coverage_missing_count": len(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(resultCoverage["missingTypes"], resultCoverage["missing_types"]))),
			"output_count":                  len(maclawAppTestEvidenceOutputs(testEvidence)),
			"artifact_count":                len(anySlice(testEvidence["artifacts"])),
		})
	}
	return compactPayload(out)
}

func maclawAppSubmissionDependenciesFromPackage(pkg map[string]any) []maclawAppInstallPlanDependency {
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, true)
	if err != nil {
		return nil
	}
	deps := []maclawAppInstallPlanDependency{}
	seen := map[string]int{}
	for _, entry := range entries {
		for _, dep := range maclawAppDependenciesForEntry(entry) {
			dep.AppIDs = append([]string(nil), dep.AppIDs...)
			if !containsMaclawAppString(dep.AppIDs, entry.ID) {
				dep.AppIDs = append(dep.AppIDs, entry.ID)
			}
			key := strings.ToLower(strings.TrimSpace(dep.ID))
			if key == "" {
				continue
			}
			if idx, ok := seen[key]; ok {
				if dep.Required && !deps[idx].Required {
					deps[idx].Required = true
				}
				if deps[idx].Version == "" {
					deps[idx].Version = dep.Version
					deps[idx].RequiredVersion = dep.RequiredVersion
					deps[idx].VersionStatus = maclawAppDependencyVersionStatus(deps[idx])
				}
				if deps[idx].Kind == "skill" && dep.Kind != "" {
					deps[idx].Kind = dep.Kind
				}
				if deps[idx].Source == "" {
					deps[idx].Source = dep.Source
				}
				for _, appID := range dep.AppIDs {
					if !containsMaclawAppString(deps[idx].AppIDs, appID) {
						deps[idx].AppIDs = append(deps[idx].AppIDs, appID)
					}
				}
				continue
			}
			seen[key] = len(deps)
			deps = append(deps, dep)
		}
	}
	return deps
}

func (a *App) appendMaclawAppSubmission(record maclawAppSubmissionRecord) error {
	maclawAppSubmissionQueueMu.Lock()
	defer maclawAppSubmissionQueueMu.Unlock()
	queue, err := a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		return err
	}
	if queue.Schema == "" {
		queue.Schema = "maclaw.app.submissions.v1"
	}
	queue.UpdatedAt = record.SubmittedAt
	queue.Submissions = append([]maclawAppSubmissionRecord{record}, queue.Submissions...)
	if len(queue.Submissions) > 200 {
		queue.Submissions = queue.Submissions[:200]
	}
	return a.writeMaclawAppSubmissionQueueUnlocked(queue)
}

// refreshMaclawAppSubmissionPublishStamps re-enriches a local-queue package with
// HubSkillID / install_ref stamps from currently installed skills. Used after
// skill-market upload in one-click publish so the Hub pack gate and receivers
// see resolved_dependencies without requiring a re-queue.
//
// Returns the number of newly stamped install_ref entries (0 when nothing changed).
// Plan/enrich runs outside the queue mutex so long install planning does not
// block concurrent submission status / fingerprint updates.
func (a *App) refreshMaclawAppSubmissionPublishStamps(submissionID string) (int, error) {
	if a == nil {
		return 0, fmt.Errorf("app is not initialized")
	}
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return 0, fmt.Errorf("submission_id is required")
	}

	// Snapshot package under lock, then release before Plan/enrich.
	maclawAppSubmissionQueueMu.RLock()
	queueSnap, err := a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		maclawAppSubmissionQueueMu.RUnlock()
		return 0, err
	}
	var pkgSnap map[string]any
	channel := ""
	found := false
	for i := range queueSnap.Submissions {
		if queueSnap.Submissions[i].SubmissionID != submissionID {
			continue
		}
		found = true
		channel = queueSnap.Submissions[i].Channel
		if queueSnap.Submissions[i].Package != nil {
			// Deep clone so unlock is safe for concurrent queue writers.
			pkgSnap = cloneMapAny(queueSnap.Submissions[i].Package)
		}
		break
	}
	maclawAppSubmissionQueueMu.RUnlock()
	if !found {
		return 0, fmt.Errorf("submission %s not found", submissionID)
	}
	if pkgSnap == nil {
		return 0, fmt.Errorf("submission %s has no package payload", submissionID)
	}
	// Only local-queue drafts are rewriteable before Hub rename.
	if ch := strings.TrimSpace(strings.ToLower(channel)); ch != "" && ch != "local" {
		return 0, nil
	}

	packageJSON, err := maclawAppStableJSON(pkgSnap)
	if err != nil {
		return 0, err
	}
	plan, err := a.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		return 0, err
	}
	dependencies := cloneMaclawAppPlanDependencies(plan.Dependencies)
	a.enrichDependenciesWithHubSkillID(dependencies)
	enrichedDeps := maclawAppSerializableResolvedDeps(dependencies)
	if len(enrichedDeps) == 0 {
		return 0, nil
	}

	// Phase 1 write lock: merge onto *current* package, then release for fingerprint.
	maclawAppSubmissionQueueMu.Lock()
	queue, err := a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		maclawAppSubmissionQueueMu.Unlock()
		return 0, err
	}
	idx := -1
	for i := range queue.Submissions {
		if queue.Submissions[i].SubmissionID == submissionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		maclawAppSubmissionQueueMu.Unlock()
		return 0, fmt.Errorf("submission %s not found", submissionID)
	}
	rec := &queue.Submissions[idx]
	if rec.Package == nil {
		maclawAppSubmissionQueueMu.Unlock()
		return 0, fmt.Errorf("submission %s has no package payload", submissionID)
	}
	if ch := strings.TrimSpace(strings.ToLower(rec.Channel)); ch != "" && ch != "local" {
		maclawAppSubmissionQueueMu.Unlock()
		return 0, nil
	}
	stamped := countNewMaclawAppResolvedStamps(maclawAppResolvedInstallRefSet(rec.Package), enrichedDeps)
	baseSHA := strings.TrimSpace(rec.PackageSHA)
	pkg := applyMaclawAppResolvedDepsToPackage(rec.Package, enrichedDeps)
	changed, changeErr := maclawAppPackagePayloadChanged(rec.Package, pkg)
	if changeErr != nil {
		maclawAppSubmissionQueueMu.Unlock()
		return 0, changeErr
	}
	if !changed {
		maclawAppSubmissionQueueMu.Unlock()
		return 0, nil
	}
	maclawAppSubmissionQueueMu.Unlock()

	// Fingerprint outside the exclusive lock (can be non-trivial for large packs).
	packageSHA, packageSize, fpErr := maclawAppPackageFingerprint(pkg)
	if fpErr != nil {
		return 0, fpErr
	}

	// Phase 2 write lock: re-check + commit (or re-merge if concurrent edit).
	maclawAppSubmissionQueueMu.Lock()
	defer maclawAppSubmissionQueueMu.Unlock()
	queue, err = a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		return 0, err
	}
	idx = -1
	for i := range queue.Submissions {
		if queue.Submissions[i].SubmissionID == submissionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, fmt.Errorf("submission %s not found", submissionID)
	}
	rec = &queue.Submissions[idx]
	if rec.Package == nil {
		return 0, fmt.Errorf("submission %s has no package payload", submissionID)
	}
	if ch := strings.TrimSpace(strings.ToLower(rec.Channel)); ch != "" && ch != "local" {
		return 0, nil
	}
	// If the package moved under us (or had no SHA yet when we snapshotted),
	// re-merge onto the latest payload and re-hash so concurrent queue edits
	// are not clobbered by a stale phase-1 merge.
	currentSHA := strings.TrimSpace(rec.PackageSHA)
	if baseSHA == "" || !strings.EqualFold(currentSHA, baseSHA) {
		stamped = countNewMaclawAppResolvedStamps(maclawAppResolvedInstallRefSet(rec.Package), enrichedDeps)
		pkg = applyMaclawAppResolvedDepsToPackage(rec.Package, enrichedDeps)
		changed, changeErr = maclawAppPackagePayloadChanged(rec.Package, pkg)
		if changeErr != nil {
			return 0, changeErr
		}
		if !changed {
			return 0, nil
		}
		packageSHA, packageSize, fpErr = maclawAppPackageFingerprint(pkg)
		if fpErr != nil {
			return 0, fpErr
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rec.Package = pkg
	rec.PackageSHA = packageSHA
	if packageSize > 0 {
		rec.PackageSize = packageSize
	}
	// Do not overwrite rec.Message — one-click / user-facing summaries live there.
	// Audit the stamp via Events only.
	note := fmt.Sprintf("dependency install references refreshed after skill publish (%d new resolved stamp)", stamped)
	if stamped != 1 {
		note = fmt.Sprintf("dependency install references refreshed after skill publish (%d new resolved stamps)", stamped)
	}
	evt := rec.maclawAppSubmissionEvent(now)
	evt.Message = note
	rec.Events = appendMaclawAppSubmissionEvent(rec.Events, evt)
	queue.UpdatedAt = now
	if err := a.writeMaclawAppSubmissionQueueUnlocked(queue); err != nil {
		return 0, err
	}
	return stamped, nil
}

// maxMaclawAppSubmissionEvents keeps durable queue rows bounded under repeated
// one-click / status stamp churn.
const maxMaclawAppSubmissionEvents = 40

func appendMaclawAppSubmissionEvent(events []maclawAppSubmissionEvent, evt maclawAppSubmissionEvent) []maclawAppSubmissionEvent {
	events = append(events, evt)
	if len(events) <= maxMaclawAppSubmissionEvents {
		return events
	}
	return append([]maclawAppSubmissionEvent(nil), events[len(events)-maxMaclawAppSubmissionEvents:]...)
}

// maclawAppHubSubmitHTTPTimeout picks the HTTP client timeout for Hub pack
// submit: min(cap, ctx remaining). Returns ctx.Err() when the deadline has
// already elapsed so callers do not start a doomed request.
func maclawAppHubSubmitHTTPTimeout(ctx context.Context, cap time.Duration) (time.Duration, error) {
	if cap <= 0 {
		cap = 60 * time.Second
	}
	if ctx == nil {
		return cap, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return cap, nil
	}
	remain := time.Until(deadline)
	if remain <= 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 0, context.DeadlineExceeded
	}
	if remain < cap {
		return remain, nil
	}
	return cap, nil
}

func countNewMaclawAppResolvedStamps(prev map[string]string, enrichedDeps []map[string]any) int {
	stamped := 0
	for _, entry := range enrichedDeps {
		id := strings.ToLower(strings.TrimSpace(stringFromAny(entry["id"])))
		ref := strings.TrimSpace(stringFromAny(entry["install_ref"]))
		if id == "" || ref == "" {
			continue
		}
		if prev[id] != ref {
			stamped++
		}
	}
	return stamped
}

func applyMaclawAppResolvedDepsToPackage(src map[string]any, enrichedDeps []map[string]any) map[string]any {
	pkg := cloneMapAny(src)
	if pkg == nil {
		pkg = map[string]any{}
	}
	pkg["resolved_dependencies"] = enrichedDeps
	injectResolvedDepsIntoAppEntries(pkg, enrichedDeps)
	maclawAppRefreshDependencyVerificationInstallRefs(pkg, enrichedDeps)
	return pkg
}

// maclawAppRefreshDependencyVerificationInstallRefs keeps the durable
// governance proof aligned with resolved_dependencies. Hub validates the
// dependencyVerification skills/dependencies lists directly, so refreshing only
// the package-level resolved_dependencies leaves an otherwise published Skill
// without the required install_ref proof.
func maclawAppRefreshDependencyVerificationInstallRefs(pkg map[string]any, enrichedDeps []map[string]any) {
	if pkg == nil || len(enrichedDeps) == 0 {
		return
	}
	for _, rawEntry := range anySlice(pkg["apps"]) {
		entry := anyMap(rawEntry)
		if entry == nil {
			continue
		}
		appID := maclawAppPackageEntryID(entry)
		app := anyMap(entry["app"])
		if app == nil {
			continue
		}
		governance := anyMap(app["governance"])
		if governance == nil {
			continue
		}
		verification := anyMap(firstNonEmptyMaclawAppAny(governance["dependencyVerification"], governance["dependency_verification"]))
		if verification == nil {
			continue
		}
		for _, key := range []string{"skills", "dependencies"} {
			items := anySlice(verification[key])
			if len(items) == 0 {
				continue
			}
			verification[key] = maclawAppRefreshDependencyVerificationItems(items, enrichedDeps, appID)
		}
		// Preserve the package's original key style while making the in-memory
		// map explicit for JSON serialization.
		if _, ok := governance["dependencyVerification"]; ok {
			governance["dependencyVerification"] = verification
		} else {
			governance["dependency_verification"] = verification
		}
	}
}

func maclawAppRefreshDependencyVerificationItems(items []any, enrichedDeps []map[string]any, appID string) []any {
	out := make([]any, 0, len(items))
	for _, rawItem := range items {
		item := anyMap(rawItem)
		if item == nil {
			out = append(out, rawItem)
			continue
		}
		updated := cloneMapAny(item)
		id := strings.TrimSpace(maclawAppStringValue(updated, "id", "skill_id", "skillId", "canonical_id", "canonicalID"))
		for _, dep := range enrichedDeps {
			depID := strings.TrimSpace(stringFromAny(dep["id"]))
			if id == "" || !strings.EqualFold(id, depID) || !maclawAppResolvedDependencyAppliesToApp(dep, appID) {
				continue
			}
			for _, key := range []string{"install_ref", "source", "install_ref_kind", "install_ref_target", "install_ref_version"} {
				if value := dep[key]; value != nil && strings.TrimSpace(stringFromAny(value)) != "" {
					updated[key] = value
				}
			}
			break
		}
		out = append(out, updated)
	}
	return out
}

func maclawAppResolvedDependencyAppliesToApp(dep map[string]any, appID string) bool {
	appIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(dep["app_ids"], dep["appIDs"]))
	if len(appIDs) == 0 || strings.TrimSpace(appID) == "" {
		return true
	}
	for _, candidate := range appIDs {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(appID)) {
			return true
		}
	}
	return false
}

func maclawAppPackagePayloadChanged(before, after map[string]any) (bool, error) {
	beforeJSON, err := maclawAppStableJSON(before)
	if err != nil {
		return false, err
	}
	afterJSON, err := maclawAppStableJSON(after)
	if err != nil {
		return false, err
	}
	return beforeJSON != afterJSON, nil
}

// maclawAppResolvedInstallRefSet maps dependency id → install_ref from a package doc.
func maclawAppResolvedInstallRefSet(pkg map[string]any) map[string]string {
	out := map[string]string{}
	if pkg == nil {
		return out
	}
	add := func(raw any) {
		for _, item := range anySlice(raw) {
			m := anyMap(item)
			if m == nil {
				continue
			}
			id := strings.ToLower(strings.TrimSpace(stringFromAny(m["id"])))
			ref := strings.TrimSpace(stringFromAny(m["install_ref"]))
			if id == "" || ref == "" {
				continue
			}
			out[id] = ref
		}
	}
	add(pkg["resolved_dependencies"])
	for _, entry := range anySlice(pkg["apps"]) {
		if em := anyMap(entry); em != nil {
			add(em["resolved_dependencies"])
		}
	}
	return out
}

// refreshMaclawAppSubmissionPackageFingerprint updates PackageSHA/Size for a
// local-queue submission so Hub sync can proceed when the payload is current
// but the stored fingerprint is stale.
func (a *App) refreshMaclawAppSubmissionPackageFingerprint(submissionID, packageSHA string, packageSize int) error {
	submissionID = strings.TrimSpace(submissionID)
	packageSHA = strings.TrimSpace(packageSHA)
	if submissionID == "" || packageSHA == "" {
		return fmt.Errorf("submission_id and package_sha are required")
	}
	maclawAppSubmissionQueueMu.Lock()
	defer maclawAppSubmissionQueueMu.Unlock()
	queue, err := a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		return err
	}
	found := false
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range queue.Submissions {
		if queue.Submissions[i].SubmissionID != submissionID {
			continue
		}
		queue.Submissions[i].PackageSHA = packageSHA
		if packageSize > 0 {
			queue.Submissions[i].PackageSize = packageSize
		}
		// Keep durable Message (one-click summary); audit via Events only.
		evt := queue.Submissions[i].maclawAppSubmissionEvent(now)
		evt.Message = "package fingerprint refreshed for hub sync"
		queue.Submissions[i].Events = appendMaclawAppSubmissionEvent(queue.Submissions[i].Events, evt)
		found = true
		break
	}
	if !found {
		return fmt.Errorf("submission %s not found", submissionID)
	}
	queue.UpdatedAt = now
	return a.writeMaclawAppSubmissionQueueUnlocked(queue)
}

func (a *App) writeMaclawAppSubmissionQueue(queue maclawAppSubmissionQueue) error {
	maclawAppSubmissionQueueMu.Lock()
	defer maclawAppSubmissionQueueMu.Unlock()
	return a.writeMaclawAppSubmissionQueueUnlocked(queue)
}

func (a *App) writeMaclawAppSubmissionQueueUnlocked(queue maclawAppSubmissionQueue) error {
	path := a.maclawAppSubmissionQueuePath()
	data, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

func (a *App) readMaclawAppSubmissionQueue() (maclawAppSubmissionQueue, error) {
	maclawAppSubmissionQueueMu.RLock()
	defer maclawAppSubmissionQueueMu.RUnlock()
	return a.readMaclawAppSubmissionQueueUnlocked()
}

func (a *App) readMaclawAppSubmissionQueueUnlocked() (maclawAppSubmissionQueue, error) {
	path := a.maclawAppSubmissionQueuePath()
	queue := maclawAppSubmissionQueue{Schema: "maclaw.app.submissions.v1"}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return queue, nil
	}
	if err != nil {
		return queue, err
	}
	if len(data) == 0 {
		return queue, nil
	}
	if err := json.Unmarshal(data, &queue); err != nil {
		return queue, fmt.Errorf("decode maclaw app submission queue: %w", err)
	}
	if queue.Schema == "" {
		queue.Schema = "maclaw.app.submissions.v1"
	}
	return queue, nil
}

func (a *App) maclawAppSubmissionQueuePath() string {
	return filepath.Join(a.GetDataDir(), "app_market_submissions.json")
}

func (record maclawAppSubmissionRecord) maclawAppSubmissionSummary() maclawAppSubmissionSummary {
	packageSHA := record.PackageSHA
	packageSize := record.PackageSize
	if packageSHA == "" || packageSize == 0 {
		computedSHA, computedSize, _ := maclawAppPackageFingerprint(record.Package)
		if packageSHA == "" {
			packageSHA = computedSHA
		}
		if packageSize == 0 {
			packageSize = computedSize
		}
	}
	eventCount := len(record.Events)
	lastEventAt := ""
	if eventCount > 0 {
		lastEventAt = record.Events[eventCount-1].At
	} else if record.SubmittedAt != "" {
		eventCount = 1
		lastEventAt = record.SubmittedAt
	}
	reviewEvidence := cloneMapAny(record.ReviewEvidence)
	if len(reviewEvidence) == 0 {
		reviewEvidence = maclawAppSubmissionReviewEvidenceForRecord(record)
	}
	return maclawAppSubmissionSummary{
		SubmissionID:    record.SubmissionID,
		HubCapabilityID: record.HubCapabilityID,
		SubmittedAt:     record.SubmittedAt,
		Status:          record.Status,
		Channel:         record.Channel,
		AppIDs:          append([]string(nil), record.AppIDs...),
		AppNames:        append([]string(nil), record.AppNames...),
		PackageSHA:      packageSHA,
		PackageSHA256:   packageSHA,
		PackageSize:     packageSize,
		ReviewedAt:      record.ReviewedAt,
		PublishedAt:     record.PublishedAt,
		Reviewer:        record.Reviewer,
		RiskLevel:       record.RiskLevel,
		ApprovedScopes:  append([]string(nil), record.ApprovedScopes...),
		ReviewIssues:    cloneMaclawAppReviewIssues(record.ReviewIssues),
		Dependencies:    cloneMaclawAppPlanDependencies(record.Dependencies),
		Evidence:        maclawAppSubmissionEvidenceForRecord(record),
		ReviewEvidence:  reviewEvidence,
		EventCount:      eventCount,
		LastEventAt:     lastEventAt,
		Message:         record.Message,
	}
}

func (record maclawAppSubmissionRecord) maclawAppSubmissionEvent(at string) maclawAppSubmissionEvent {
	return maclawAppSubmissionEvent{
		At:           at,
		Status:       record.Status,
		Channel:      record.Channel,
		SubmissionID: record.SubmissionID,
		Message:      record.Message,
		Reviewer:     record.Reviewer,
	}
}

func firstMaclawAppHubCapabilityID(submissions []maclawAppHubSubmissionResult) string {
	if len(submissions) == 0 {
		return ""
	}
	return strings.TrimSpace(submissions[0].CapabilityID)
}

// resolveMaclawAppHubSubmissionIdentity extracts the Hub submission identity
// from a submit response. submission_id is required and must never fall back to
// package_sha256 (checksums are integrity fields, not queue identities).
func resolveMaclawAppHubSubmissionIdentity(hubResp maclawAppHubSubmissionResponse, fallbackPackageSHA string) (submissionID, capabilityID, packageSHA string, err error) {
	packageSHA = firstNonEmptyMaclawAppString(hubResp.PackageSHA256, fallbackPackageSHA)
	submissionID = strings.TrimSpace(hubResp.SubmissionID)
	capabilityID = strings.TrimSpace(hubResp.CapabilityID)

	if len(hubResp.Submissions) > 0 {
		first := hubResp.Submissions[0]
		if submissionID == "" {
			submissionID = strings.TrimSpace(first.SubmissionID)
		}
		if capabilityID == "" {
			capabilityID = strings.TrimSpace(first.CapabilityID)
		}
	}

	if submissionID == "" {
		return "", "", "", fmt.Errorf("enterprise Hub submit response missing submissions[0].submission_id (package_sha256 is not a valid submission identity)")
	}
	// Guard against legacy/broken Hub responses that reused checksum as ID.
	if packageSHA != "" && strings.EqualFold(submissionID, packageSHA) {
		return "", "", "", fmt.Errorf("enterprise Hub submit response submission_id must not equal package_sha256")
	}
	if fallbackPackageSHA != "" && strings.EqualFold(submissionID, strings.TrimSpace(fallbackPackageSHA)) {
		return "", "", "", fmt.Errorf("enterprise Hub submit response submission_id must not equal local package fingerprint")
	}
	return submissionID, capabilityID, packageSHA, nil
}

func normalizeMaclawAppSubmissionStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "submitted", "pending_review", "review_failed", "approved", "published", "deprecated", "revoked":
		return strings.TrimSpace(status)
	default:
		return ""
	}
}
