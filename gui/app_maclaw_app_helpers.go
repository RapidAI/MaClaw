package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func maclawAppIntFromRegistration(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return n
		}
	}
	return 0
}

func appendMaclawAppUniqueStrings(values []string, extra ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values)+len(extra))
	for _, value := range append(values, extra...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func maclawAppSelectionIDSet(appIDs []string) map[string]struct{} {
	out := map[string]struct{}{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	for _, appID := range appIDs {
		add(appID)
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(appID)), "market-") {
			add(strings.TrimSpace(appID)[len("market-"):])
		} else if strings.TrimSpace(appID) != "" {
			add("market-" + strings.TrimSpace(appID))
		}
	}
	return out
}

func maclawAppSelectionMatches(selected map[string]struct{}, appID string) bool {
	for key := range maclawAppSelectionIDSet([]string{appID}) {
		if _, ok := selected[key]; ok {
			return true
		}
	}
	return false
}

func (a *App) installedMaclawAppSkillIndex() map[string]NLSkillDefinition {
	defs := a.ListNLSkills()
	index := make(map[string]NLSkillDefinition, len(defs)*4)
	for _, def := range defs {
		// Same identity key set as MatchesName / run resolution so authoring
		// hub ids, display names, and directory basenames resolve to one skill.
		for _, key := range def.SkillIdentityKeys() {
			if _, exists := index[key]; !exists {
				index[key] = def
			}
		}
	}
	return index
}

func maclawAppStripVersionSuffix(value string) string {
	return corelib.NormalizeSkillMatchQuery(value)
}

// maclawAppVersionLooksLikeContentHash reports whether v looks like a hub
// content digest (hex with at least one a-f letter), not a human version.
func maclawAppVersionLooksLikeContentHash(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) < 8 {
		return false
	}
	hasLetter := false
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'f':
			hasLetter = true
		case r >= '0' && r <= '9':
			// digit ok
		default:
			return false
		}
	}
	// Pure digit strings (e.g. "20240101") stay version-like; digests include a-f.
	return hasLetter
}

// maclawAppVersionIsSemverLike reports whether v can be compared with minimum
// semver semantics (numeric, not a content hash).
func maclawAppVersionIsSemverLike(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || maclawAppVersionLooksLikeContentHash(v) {
		return false
	}
	return maclawAppVersionIsNumeric(v)
}

// maclawAppRevisionTokensCompatible compares revision tokens after skill
// identity is already known to match (same hub target, or local dependency
// identity resolution). Tokens may be semver, content hashes, or codenames.
//
// Policy (shared by dual-key @suffix and cross-coordinate plain↔key paths):
//   - equal (case-insensitive) → match
//   - both semver-like → installed >= required
//   - required content-hash pin → only equal digest matches (pin must be proven)
//   - required human semver/codename + installed content-hash → match
//     (appSkill.version "1.0.0" vs hub_version …@hash)
//   - empty required → match (no pin); empty installed → mismatch
func maclawAppRevisionTokensCompatible(required, installed string) bool {
	required = strings.TrimSpace(required)
	installed = strings.TrimSpace(installed)
	if required == "" {
		return true
	}
	if installed == "" {
		return false
	}
	if strings.EqualFold(required, installed) {
		return true
	}
	reqSem := maclawAppVersionIsSemverLike(required)
	instSem := maclawAppVersionIsSemverLike(installed)
	if reqSem && instSem {
		return maclawAppPlainVersionSatisfied(required, installed)
	}
	// Content-hash pin on the required side: EqualFold already failed.
	if maclawAppVersionLooksLikeContentHash(required) {
		return false
	}
	// Required is human-facing; installed content digest cannot be ordered but
	// identity is already resolved — accept (PDF翻译工具 regression).
	if maclawAppVersionLooksLikeContentHash(installed) {
		return true
	}
	// Non-comparable tokens (codenames, etc.) require exact match (failed above).
	return false
}

// maclawAppCrossCoordinateVersionSatisfied handles one hub source-version key
// and one plain version (semver/codename/bare digest). Revision policy is
// shared with dual-key suffix compare via maclawAppRevisionTokensCompatible.
func maclawAppCrossCoordinateVersionSatisfied(required, installed string, reqIsKey bool) bool {
	keySide, plainSide := installed, required
	if reqIsKey {
		keySide, plainSide = required, installed
	}
	_, _, keyVer, ok := maclawAppParseSourceVersionKey(keySide)
	if !ok {
		// Defensive: caller thought this was a key. Do not invent a match for a
		// required pin; installed-side garbage is ignored as "unknown hub meta".
		return !reqIsKey
	}
	return maclawAppCrossCoordinateVersionSatisfiedParts(keyVer, plainSide, reqIsKey)
}

func maclawAppCrossCoordinateVersionSatisfiedParts(keyVer, plainSide string, reqIsKey bool) bool {
	keyVer = strings.TrimSpace(keyVer)
	plainSide = strings.TrimSpace(plainSide)
	if keyVer == "" {
		// Identity-only key (no @suffix revision pin) on the key side.
		return true
	}
	if reqIsKey {
		return maclawAppRevisionTokensCompatible(keyVer, plainSide)
	}
	return maclawAppRevisionTokensCompatible(plainSide, keyVer)
}

// maclawAppPlainVersionSatisfied compares non-key version strings only
// (semver minimum, constraints, codenames). Callers must strip hub keys first.
func maclawAppPlainVersionSatisfied(required, installed string) bool {
	required = strings.TrimSpace(required)
	installed = strings.TrimSpace(installed)
	if required == "" || installed == "" {
		return required == installed
	}
	// Constraint operators: no full solver here — accept, matching the lenient
	// behavior of maclawAppWorkflowVersionMatches.
	if strings.ContainsAny(required, "<>=^~*") {
		return true
	}
	// Normalized exact match (handles v-prefix, whitespace, case).
	if maclawAppNormalizeVersion(required) == maclawAppNormalizeVersion(installed) {
		return true
	}
	// Both numeric-parseable → minimum-version satisfaction (installed >= required).
	if maclawAppVersionIsNumeric(required) && maclawAppVersionIsNumeric(installed) {
		return compareVersions(installed, required) >= 0
	}
	// Non-numeric versions that don't match exactly → conservative mismatch.
	return false
}

// maclawAppNormalizeVersion lowercases, trims, and strips a leading "v" prefix
// so that "V1.0.0", " 1.0.0 " and "1.0.0" compare equal.
func maclawAppNormalizeVersion(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "v")
	return strings.TrimSpace(v)
}

// maclawAppVersionIsNumeric reports whether the numeric portion of a version
// begins with a digit (e.g. "1.0.0", "10", "2.0-beta"), so that compareVersions
// can be applied meaningfully. Codename-style versions ("stable", "latest")
// return false and fall back to exact matching.
func maclawAppVersionIsNumeric(v string) bool {
	v = maclawAppNormalizeVersion(v)
	if v == "" {
		return false
	}
	// Split off any pre-release suffix, then inspect the first segment.
	numeric := v
	if idx := strings.IndexByte(numeric, '-'); idx >= 0 {
		numeric = numeric[:idx]
	}
	first := strings.SplitN(numeric, ".", 2)[0]
	if first == "" {
		return false
	}
	return first[0] >= '0' && first[0] <= '9'
}

func maclawAppMarkInstallableMissingDependencies(deps []maclawAppInstallPlanDependency) {
	for i := range deps {
		dep := &deps[i]
		if !maclawAppDependencyCanAttemptInstall(*dep) {
			continue
		}
		if action := strings.TrimSpace(dep.Action); action != "" && action != "blocked" {
			continue
		}
		dep.Health = "missing"
		dep.Action = "install"
		dep.Message = "required skill dependency is missing and can be installed automatically"
	}
}

func maclawAppVersionString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		f := float64(v)
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func maclawAppDataSrvInstallationPayloads(entries []parsedMaclawAppEntry, source, packageSHA string, packageSize int, dependencies []maclawAppInstallPlanDependency) []maclawAppDataSrvInstallationPayload {
	payloads := make([]maclawAppDataSrvInstallationPayload, 0, len(entries))
	installEvidenceByApp := maclawAppInstallEvidenceByApp(entries, dependencies, nil)
	for _, entry := range entries {
		roleBindings := maclawAppDataSrvRoleBindingsForEntry(entry)
		if len(roleBindings) == 0 {
			continue
		}
		metadata := map[string]interface{}{
			"package_sha256": packageSHA,
			"package_bytes":  packageSize,
			"schema":         entry.Schema,
		}
		versionSnapshot := maclawAppInstallVersionSnapshotForEntry(entry)
		metadata["version_snapshot"] = versionSnapshot
		if versionSnapshot.AppEntryVersion != "" {
			metadata["app_entry_version"] = versionSnapshot.AppEntryVersion
		}
		if installEvidence := anyMap(installEvidenceByApp[entry.ID]); installEvidence != nil {
			metadata["install_evidence"] = installEvidence
		}
		if reviewEvidence := maclawAppReviewEvidenceForEntry(entry); reviewEvidence != nil {
			metadata["review_evidence"] = reviewEvidence
			applyMaclawAppDataSrvReviewEvidenceMetadata(metadata, reviewEvidence)
		}
		if submission := maclawAppSubmissionMetadataForEntry(entry); submission != nil {
			metadata["submission"] = submission
			if capabilityID := maclawAppStringValue(submission, "capability_id"); capabilityID != "" {
				metadata["hub_capability_id"] = capabilityID
			}
			if marketCapabilityID := maclawAppStringValue(submission, "market_capability_id"); marketCapabilityID != "" {
				metadata["hub_market_capability_id"] = marketCapabilityID
			}
			if submissionID := maclawAppStringValue(submission, "submission_id"); submissionID != "" {
				metadata["hub_submission_id"] = submissionID
			}
			if versionKey := maclawAppStringValue(submission, "version_key"); versionKey != "" {
				metadata["hub_version_key"] = versionKey
			}
			if status := maclawAppStringValue(submission, "status"); status != "" {
				metadata["hub_review_status"] = status
			}
			if packageSHA := maclawAppStringValue(submission, "package_sha256"); packageSHA != "" {
				metadata["hub_package_sha256"] = packageSHA
			}
			applyMaclawAppDataSrvHubPackageSignatureMetadata(metadata, submission)
		}
		if appSkill := maclawAppAppSkillBlockForEntry(entry); appSkill != nil {
			metadata["app_skill_id"] = maclawAppStringValue(appSkill, "id")
			metadata["app_skill_version"] = maclawAppStringValue(appSkill, "version")
			metadata["app_skill_source"] = maclawAppStringValue(appSkill, "source")
		}
		if workflowSkillIDs := maclawAppWorkflowSkillIDsForEntry(entry); len(workflowSkillIDs) > 0 {
			metadata["workflow_skill_ids"] = workflowSkillIDs
		}
		if workflowContract := maclawAppWorkflowContractForEntry(entry); workflowContract != nil {
			metadata["workflow_contract"] = workflowContract
			if schema := maclawAppStringValue(workflowContract, "schema"); schema != "" {
				metadata["workflow_contract_schema"] = schema
			}
			if workflowSkillID := maclawAppStringValue(workflowContract, "workflowSkillId", "workflow_skill_id"); workflowSkillID != "" {
				metadata["workflow_contract_skill_id"] = workflowSkillID
			}
			if objectRole := maclawAppStringValue(workflowContract, "objectRole", "object_role"); objectRole != "" {
				metadata["workflow_contract_object_role"] = objectRole
			}
		}
		if workflowMapping := maclawAppWorkflowMappingForEntry(entry); workflowMapping != nil {
			metadata["workflow_mapping"] = workflowMapping
			metadata["workflow_mapping_schema"] = maclawAppStringValue(workflowMapping, "schema")
			if submitNode := maclawAppStringValue(workflowMapping, "submitNode", "submit_node"); submitNode != "" {
				metadata["workflow_submit_node"] = submitNode
			}
			if approvalNode := maclawAppStringValue(workflowMapping, "approvalNode", "approval_node"); approvalNode != "" {
				metadata["workflow_approval_node"] = approvalNode
			}
			if resultNode := maclawAppStringValue(workflowMapping, "resultNode", "result_node"); resultNode != "" {
				metadata["workflow_result_node"] = resultNode
			}
		}
		if workspaceLayout := maclawAppWorkspaceLayoutMetadataForEntry(entry); workspaceLayout != nil {
			metadata["workspace_layout"] = workspaceLayout
			if entryName := maclawAppStringValue(workspaceLayout, "entry"); entryName != "" {
				metadata["workspace_layout_entry"] = entryName
			}
			if template := maclawAppStringValue(workspaceLayout, "template"); template != "" {
				metadata["workspace_layout_template"] = template
			}
			if density := maclawAppStringValue(workspaceLayout, "density"); density != "" {
				metadata["workspace_layout_density"] = density
			}
			if primary := maclawAppStringValue(workspaceLayout, "primaryRegion", "primary_region"); primary != "" {
				metadata["workspace_layout_primary_region"] = primary
			}
			if output := maclawAppStringValue(workspaceLayout, "outputRegion", "output_region"); output != "" {
				metadata["workspace_layout_output_region"] = output
			}
			if fingerprint := maclawAppStringValue(workspaceLayout, "fingerprint"); fingerprint != "" {
				metadata["workspace_layout_fingerprint"] = fingerprint
			}
			if visibleRegionCount, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(workspaceLayout["visibleRegionCount"], workspaceLayout["visible_region_count"])); ok && visibleRegionCount >= 0 {
				metadata["workspace_layout_visible_region_count"] = int(math.Floor(visibleRegionCount))
			}
			if regionCount, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(workspaceLayout["regionCount"], workspaceLayout["region_count"])); ok && regionCount > 0 {
				metadata["workspace_layout_region_count"] = int(regionCount)
			}
			if regionIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(workspaceLayout["regionIds"], workspaceLayout["region_ids"])); len(regionIDs) > 0 {
				metadata["workspace_layout_region_ids"] = regionIDs
			}
			if regions := anySlice(workspaceLayout["regions"]); len(regions) > 0 {
				regionIDs := make([]string, 0, len(regions))
				visibleRegionCount := 0
				for _, rawRegion := range regions {
					region := anyMap(rawRegion)
					if id := maclawAppStringValue(region, "id"); id != "" {
						regionIDs = append(regionIDs, id)
					}
					if visible, ok := region["visible"].(bool); !ok || visible {
						visibleRegionCount++
					}
				}
				if len(regionIDs) > 0 {
					metadata["workspace_layout_region_ids"] = regionIDs
				}
				if _, exists := metadata["workspace_layout_region_count"]; !exists {
					metadata["workspace_layout_region_count"] = len(regions)
				}
				if _, exists := metadata["workspace_layout_visible_region_count"]; !exists {
					metadata["workspace_layout_visible_region_count"] = visibleRegionCount
				}
			}
			if navigation := maclawAppStringListFromAny(workspaceLayout["navigation"]); len(navigation) > 0 {
				metadata["workspace_layout_navigation"] = navigation
			}
			if list := anyMap(workspaceLayout["list"]); list != nil {
				if columns := maclawAppStringListFromAny(list["columns"]); len(columns) > 0 {
					metadata["workspace_layout_list_columns"] = columns
				}
			}
			workspaceStudio := anyMap(workspaceLayout["studio"])
			if saved, ok := firstNonEmptyMaclawAppAny(workspaceLayout["studio_saved_in_manifest"], workspaceStudio["savedInManifest"], workspaceStudio["saved_in_manifest"]).(bool); ok {
				metadata["workspace_layout_studio_saved_in_manifest"] = saved
			}
			if editable, ok := firstNonEmptyMaclawAppAny(workspaceLayout["studio_editable"], workspaceStudio["editable"]).(bool); ok {
				metadata["workspace_layout_studio_editable"] = editable
			}
			if updatedBy := firstNonEmptyMaclawAppString(maclawAppStringValue(workspaceLayout, "studio_updated_by"), maclawAppStringValue(workspaceStudio, "updatedBy", "updated_by")); updatedBy != "" {
				metadata["workspace_layout_studio_updated_by"] = updatedBy
			}
		}
		if governance := maclawAppGovernanceMetadataForEntry(entry); governance != nil {
			metadata["governance"] = governance
			if status := maclawAppStringValue(governance, "status"); status != "" {
				metadata["governance_status"] = status
			}
			if riskLevel := maclawAppStringValue(governance, "riskLevel", "risk_level"); riskLevel != "" {
				metadata["governance_risk_level"] = riskLevel
			}
			if resultContract := anyMap(governance["result_contract"]); resultContract != nil {
				metadata["result_contract"] = resultContract
				if schema := maclawAppStringValue(resultContract, "schema"); schema != "" {
					metadata["result_contract_schema"] = schema
				}
				if primary := maclawAppStringValue(resultContract, "primary"); primary != "" {
					metadata["result_contract_primary"] = primary
				}
				if types := maclawAppStringListFromAny(resultContract["types"]); len(types) > 0 {
					metadata["result_contract_types"] = types
				}
			}
			if testEvidence := anyMap(governance["test_evidence"]); testEvidence != nil {
				metadata["test_evidence"] = testEvidence
				applyMaclawAppDataSrvTestEvidenceMetadata(metadata, testEvidence)
			}
			applyMaclawAppDataSrvDesignConsistencyMetadata(metadata, entry, governance)
		}
		if dependencyVerification := maclawAppDependencyVerificationMetadataForEntry(entry, dependencies); dependencyVerification != nil {
			metadata["dependency_verification"] = dependencyVerification
			if verifiedAt := maclawAppStringValue(dependencyVerification, "verifiedAt", "verified_at"); verifiedAt != "" {
				metadata["test_evidence_dependency_verified_at"] = verifiedAt
			}
			if count, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(dependencyVerification["dependencyCount"], dependencyVerification["dependency_count"])); ok {
				metadata["test_evidence_dependency_count"] = int(math.Floor(count))
			}
			if missing, ok := firstNonEmptyMaclawAppAny(dependencyVerification["hasMissingRequired"], dependencyVerification["has_missing_required"]).(bool); ok {
				metadata["test_evidence_dependency_missing_required"] = missing
			}
			if blocked, ok := firstNonEmptyMaclawAppAny(dependencyVerification["hasBlockingDependency"], dependencyVerification["has_blocking_dependency"]).(bool); ok {
				metadata["test_evidence_dependency_blocking"] = blocked
			}
			if installTrace := anyMap(firstNonEmptyMaclawAppAny(dependencyVerification["install_trace"], dependencyVerification["installTrace"])); installTrace != nil {
				metadata["dependency_install_trace"] = installTrace
				for _, pair := range []struct {
					keys []string
					meta string
				}{
					{[]string{"preflight_checked_count", "preflightCheckedCount"}, "dependency_preflight_checked_count"},
					{[]string{"preflight_ready_count", "preflightReadyCount"}, "dependency_preflight_ready_count"},
					{[]string{"preflight_failed_count", "preflightFailedCount"}, "dependency_preflight_failed_count"},
					{[]string{"integrity_checked_count", "integrityCheckedCount"}, "dependency_integrity_checked_count"},
					{[]string{"integrity_ready_count", "integrityReadyCount"}, "dependency_integrity_ready_count"},
					{[]string{"integrity_failed_count", "integrityFailedCount"}, "dependency_integrity_failed_count"},
					{[]string{"download_available_count", "downloadAvailableCount"}, "dependency_download_available_count"},
					{[]string{"signature_available_count", "signatureAvailableCount"}, "dependency_signature_available_count"},
					{[]string{"install_error_count", "installErrorCount"}, "dependency_install_error_count"},
				} {
					for _, key := range pair.keys {
						if value, ok := maclawAppNumberFromAny(installTrace[key]); ok {
							metadata[pair.meta] = int(math.Floor(value))
							break
						}
					}
				}
				if ok, exists := firstNonEmptyMaclawAppAny(installTrace["ok"], installTrace["traceOk"]).(bool); exists {
					metadata["dependency_install_trace_ok"] = ok
				}
			}
		}
		appDependencies := cloneMaclawAppPlanDependenciesForApp(dependencies, entry.ID)
		if len(appDependencies) > 0 {
			metadata["dependencies"] = appDependencies
			metadata["dependency_count"] = len(appDependencies)
			metadata["has_missing_required_dependency"] = hasMissingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
			metadata["has_blocking_dependency"] = hasBlockingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
		}
		body := map[string]interface{}{
			"app_id":        entry.ID,
			"blueprint_id":  maclawAppBlueprintIDForEntry(entry),
			"name":          entry.Name,
			"version":       maclawAppStringValue(entry.App, "version"),
			"kind":          normalizeMaclawAppKind(entry.Kind),
			"status":        "installed",
			"source":        firstNonEmptyMaclawAppString(source, maclawAppStringValue(entry.App, "source")),
			"role_bindings": roleBindings,
			"metadata":      compactPayload(metadata),
		}
		payloads = append(payloads, maclawAppDataSrvInstallationPayload{AppID: entry.ID, RoleBindingCount: len(roleBindings), Body: compactPayload(body)})
	}
	return payloads
}

func applyMaclawAppDataSrvDesignConsistencyMetadata(metadata map[string]interface{}, entry parsedMaclawAppEntry, governance map[string]any) {
	if metadata == nil || governance == nil {
		return
	}
	testEvidence := maclawAppTestEvidenceMap(governance)
	if testEvidence == nil {
		return
	}
	type consistencyItem struct {
		Current  string
		Evidence string
		Matches  bool
		Present  bool
	}
	definition := consistencyItem{
		Current:  maclawAppDefinitionFingerprintForEntry(entry),
		Evidence: strings.TrimSpace(maclawAppStringValue(testEvidence, "definitionHash", "definition_hash", "definitionFingerprint", "definition_fingerprint")),
	}
	protocol := maclawAppTestProtocolMap(governance, testEvidence)
	protocolFingerprint := strings.TrimSpace(maclawAppStringValue(protocol, "fingerprint", "hash", "testProtocolFingerprint", "test_protocol_fingerprint", "protocolFingerprint", "protocol_fingerprint"))
	testProtocolEvidenceFingerprint := strings.TrimSpace(maclawAppStringValue(testEvidence, "testProtocolFingerprint", "test_protocol_fingerprint", "testProtocolHash", "test_protocol_hash", "protocolFingerprint", "protocol_fingerprint", "protocolHash", "protocol_hash"))
	testProtocol := consistencyItem{
		Current:  firstNonEmptyMaclawAppString(protocolFingerprint, maclawAppTestProtocolFingerprint(protocol), testProtocolEvidenceFingerprint),
		Evidence: testProtocolEvidenceFingerprint,
	}
	workspaceLayout := consistencyItem{
		Current:  maclawAppCurrentWorkspaceLayoutFingerprint(entry, governance),
		Evidence: strings.TrimSpace(maclawAppStringValue(testEvidence, "workspaceLayoutFingerprint", "workspace_layout_fingerprint", "workspaceLayoutHash", "workspace_layout_hash", "layoutFingerprint", "layout_fingerprint", "layoutHash", "layout_hash")),
	}
	apply := func(prefix string, item consistencyItem) consistencyItem {
		item.Present = item.Current != "" && item.Evidence != ""
		item.Matches = item.Present && item.Current == item.Evidence
		if item.Current != "" {
			metadata["current_"+prefix+"_fingerprint"] = item.Current
		}
		if item.Evidence != "" {
			metadata["test_evidence_"+prefix+"_fingerprint"] = item.Evidence
		}
		if item.Present {
			metadata["test_evidence_"+prefix+"_matches_current"] = item.Matches
		}
		return item
	}
	definition = apply("definition", definition)
	testProtocol = apply("test_protocol", testProtocol)
	workspaceLayout = apply("workspace_layout", workspaceLayout)
	checked := 0
	matched := 0
	for _, item := range []consistencyItem{definition, testProtocol, workspaceLayout} {
		if item.Present {
			checked++
			if item.Matches {
				matched++
			}
		}
	}
	if checked > 0 {
		metadata["design_consistency_checked_count"] = checked
		metadata["design_consistency_matched_count"] = matched
		metadata["design_consistency_ok"] = checked == 3 && matched == 3
		metadata["design_consistency"] = map[string]any{
			"definition": map[string]any{
				"current_fingerprint":  definition.Current,
				"evidence_fingerprint": definition.Evidence,
				"matches_current":      definition.Matches,
			},
			"test_protocol": map[string]any{
				"current_fingerprint":  testProtocol.Current,
				"evidence_fingerprint": testProtocol.Evidence,
				"matches_current":      testProtocol.Matches,
			},
			"workspace_layout": map[string]any{
				"current_fingerprint":  workspaceLayout.Current,
				"evidence_fingerprint": workspaceLayout.Evidence,
				"matches_current":      workspaceLayout.Matches,
			},
		}
	}
}

func applyMaclawAppDataSrvResultPayloadMetadata(metadata map[string]interface{}, payload map[string]any) {
	if metadata == nil || payload == nil {
		return
	}
	if resultType := maclawAppStringValue(payload, "resultType", "result_type", "type", "kind"); resultType != "" {
		metadata["test_evidence_result_type"] = resultType
	}
	if outputType := maclawAppStringValue(payload, "outputType", "output_type"); outputType != "" {
		metadata["test_evidence_output_type"] = outputType
	}
	if content := maclawAppStringValue(payload, "content", "text", "message", "summary"); content != "" {
		metadata["test_evidence_result_content"] = content
	}
}

func applyMaclawAppDataSrvOutputMetadata(metadata map[string]interface{}, outputs []any) {
	if metadata == nil || len(outputs) == 0 {
		return
	}
	if kinds := maclawAppItemStrings(outputs, "kind", "type", "result_type", "resultType", "output_type", "outputType"); len(kinds) > 0 {
		metadata["test_evidence_output_kinds"] = kinds
		metadata["test_evidence_output_types"] = kinds
		if maclawAppStringValue(metadata, "test_evidence_output_type") == "" {
			metadata["test_evidence_output_type"] = kinds[0]
		}
	}
}

func applyMaclawAppDataSrvArtifactMetadata(metadata map[string]interface{}, artifacts []any) {
	if metadata == nil || len(artifacts) == 0 {
		return
	}
	if names := maclawAppItemStrings(artifacts, "name", "artifactName", "artifact_name", "filename", "fileName", "file_name"); len(names) > 0 {
		metadata["test_evidence_artifact_names"] = names
		if maclawAppStringValue(metadata, "test_evidence_artifact_name") == "" {
			metadata["test_evidence_artifact_name"] = names[0]
		}
	}
	if uris := maclawAppItemStrings(artifacts, "uri", "URI", "artifactURI", "artifactUri", "artifact_uri", "downloadURI", "downloadUri", "download_uri"); len(uris) > 0 {
		metadata["test_evidence_artifact_uris"] = uris
		if maclawAppStringValue(metadata, "test_evidence_artifact_uri") == "" {
			metadata["test_evidence_artifact_uri"] = uris[0]
		}
	}
	if paths := maclawAppItemStrings(artifacts, "path", "artifactPath", "artifact_path", "filePath", "file_path"); len(paths) > 0 && maclawAppStringValue(metadata, "test_evidence_artifact_path") == "" {
		metadata["test_evidence_artifact_path"] = paths[0]
	}
	if types := maclawAppItemStrings(artifacts, "kind", "type", "result_type", "resultType"); len(types) > 0 {
		metadata["test_evidence_artifact_types"] = types
	}
}

func maclawAppItemStrings(items []any, keys ...string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, raw := range items {
		item := anyMap(raw)
		if item == nil {
			continue
		}
		for _, key := range keys {
			value := maclawAppStringValue(item, key)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func maclawAppBindingHolders(entry parsedMaclawAppEntry) []map[string]any {
	holders := []map[string]any{}
	if binding := anyMap(entry.App["binding"]); binding != nil {
		holders = append(holders, binding)
	}
	if entry.App != nil {
		holders = append(holders, entry.App)
	}
	return holders
}

func maclawAppStringValue(values map[string]any, keys ...string) string {
	if values == nil {
		return ""
	}
	for _, key := range keys {
		if value := stringMapValue(values, key); value != "" {
			return value
		}
	}
	return ""
}

func maclawAppDomainFromDatasetID(datasetID string) string {
	datasetID = strings.TrimSpace(datasetID)
	if idx := strings.Index(datasetID, "."); idx > 0 {
		return datasetID[:idx]
	}
	return ""
}

func maclawAppNumberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func maclawAppStringListContains(values []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == want {
			return true
		}
	}
	return false
}

func maclawAppTestProtocolMap(governance map[string]any, testEvidence map[string]any) map[string]any {
	if governance != nil {
		if protocol := anyMap(firstNonEmptyMaclawAppAny(governance["testProtocol"], governance["test_protocol"])); protocol != nil {
			return protocol
		}
	}
	if testEvidence != nil {
		return anyMap(firstNonEmptyMaclawAppAny(testEvidence["testProtocol"], testEvidence["test_protocol"]))
	}
	return nil
}

func maclawAppTestProtocolHasExpectedOutput(protocol map[string]any) bool {
	if protocol == nil {
		return false
	}
	for _, key := range []string{"expectedOutput", "expected_output", "expectedResult", "expected_result"} {
		if value, ok := protocol[key]; ok && value != nil {
			return true
		}
	}
	return false
}

func maclawAppTestProtocolFingerprint(protocol map[string]any) string {
	if protocol == nil {
		return ""
	}
	// Strip declared fingerprint keys before hashing — mirrors the frontend
	// appTestProtocolFingerprint, which hashes the protocol minus its stamp.
	// Hashing the declared fingerprint into itself would make the recompute
	// useless for cross-side consistency checks.
	stable := make(map[string]any, len(protocol))
	for key, value := range protocol {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "fingerprint", "hash", "testprotocolfingerprint", "test_protocol_fingerprint", "protocolfingerprint", "protocol_fingerprint", "protocolhash", "protocol_hash":
			continue
		}
		stable[key] = value
	}
	encoded, err := maclawAppStableJSON(compactPayload(stable))
	if err != nil {
		return ""
	}
	return maclawAppFNV1aTextHash(encoded)
}

func maclawAppIDsMatch(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	leftAliases := maclawAppIDAliases(left)
	for alias := range maclawAppIDAliases(right) {
		if leftAliases[strings.ToLower(alias)] {
			return true
		}
	}
	return false
}

func maclawAppNormalizedVersionAny(value any) int {
	if number, ok := maclawAppNumberFromAny(value); ok && number > 0 {
		return int(math.Floor(number))
	}
	return 1
}

func maclawAppFNV1aTextHash(value string) string {
	var hash uint32 = 2166136261
	for _, char := range value {
		hash ^= uint32(char)
		hash *= 16777619
	}
	return fmt.Sprintf("%08x", hash)
}

func maclawAppCoveredResultTypesContain(covered []string, primary string) bool {
	seen := map[string]bool{}
	for _, value := range covered {
		seen[strings.TrimSpace(value)] = true
	}
	return seen[primary] || (primary == "document" && seen["artifact"]) || (primary == "artifact" && seen["document"]) || (primary == "text" && seen["content"]) || (primary == "content" && seen["text"])
}

func maclawAppBoolValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := values[key].(bool); ok && value {
			return true
		}
	}
	return false
}

func maclawAppConfigHasExplicitHubCenter(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.RemoteHubCenterURL) != ""
}

func maclawAppStringListsOverlap(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		key := strings.ToLower(strings.TrimSpace(value))
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, value := range right {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			return true
		}
	}
	return false
}

func containsMaclawAppStringFold(values []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == needle {
			return true
		}
	}
	return false
}

const (
	maxMaclawAppBundledSkillFiles = 256
	maxMaclawAppBundledSkillBytes = 8 << 20
)

func maclawAppBundleInstalledSkill(def NLSkillDefinition, dep maclawAppInstallPlanDependency) (maclawAppBundledSkillEntry, error) {
	root := strings.TrimSpace(def.SkillDir)
	if root == "" {
		return maclawAppBundledSkillEntry{}, fmt.Errorf("skill directory is empty")
	}
	type bundledFile struct {
		rel  string
		data []byte
	}
	var collected []bundledFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() && maclawAppBundledSkillSkipDir(name) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !maclawAppBundledSkillFilePathOK(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		collected = append(collected, bundledFile{rel: rel, data: data})
		return nil
	})
	if err != nil {
		return maclawAppBundledSkillEntry{}, err
	}
	if len(collected) == 0 {
		return maclawAppBundledSkillEntry{}, fmt.Errorf("skill directory has no packageable files")
	}
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].rel < collected[j].rel
	})
	files := map[string]string{}
	total := int64(0)
	hasher := sha256.New()
	for _, file := range collected {
		total += int64(len(file.data))
		if len(files)+1 > maxMaclawAppBundledSkillFiles {
			return maclawAppBundledSkillEntry{}, fmt.Errorf("too many files: %d > %d", len(files)+1, maxMaclawAppBundledSkillFiles)
		}
		if total > maxMaclawAppBundledSkillBytes {
			return maclawAppBundledSkillEntry{}, fmt.Errorf("too much data: %d > %d bytes", total, maxMaclawAppBundledSkillBytes)
		}
		files[file.rel] = base64.StdEncoding.EncodeToString(file.data)
		hasher.Write([]byte(file.rel))
		hasher.Write([]byte{0})
		hasher.Write(file.data)
		hasher.Write([]byte{0})
	}
	stableID := maclawAppStableSkillID(def)
	return maclawAppBundledSkillEntry{
		StableID:    stableID,
		ID:          firstNonEmpty(dep.CanonicalID, dep.InstallRefTarget, dep.ID, def.HubSkillID, def.Name),
		Name:        def.Name,
		Version:     firstNonEmpty(dep.InstalledVersion, def.HubVersion, dep.Version),
		Source:      def.Source,
		HubSkillID:  def.HubSkillID,
		HubVersion:  def.HubVersion,
		CanonicalID: firstNonEmpty(dep.CanonicalID, dep.InstallRefTarget, def.HubSkillID, def.Name),
		SHA256:      hex.EncodeToString(hasher.Sum(nil)),
		Files:       files,
		AppIDs:      append([]string(nil), dep.AppIDs...),
	}, nil
}

func maclawAppStableSkillID(def NLSkillDefinition) string {
	if def.Capability != nil {
		if value := strings.TrimSpace(def.Capability.GlobalKey); value != "" {
			return value
		}
		if value := strings.TrimSpace(def.Capability.CapabilityID); value != "" {
			return "capability:" + value
		}
	}
	if value := strings.TrimSpace(def.HubSkillID); value != "" {
		return "hub_skill:" + value
	}
	if value := strings.TrimSpace(def.QualifiedID()); value != "" {
		return "skill:" + strings.ToLower(value)
	}
	return "skill:" + strings.ToLower(strings.TrimSpace(def.Name))
}

func normalizeMaclawAppKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "enterprise_app" {
		return "enterprise_normal_app"
	}
	return kind
}

func firstNonEmptyMaclawAppAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func anyMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

// anySlice normalizes common slice shapes produced by JSON decode ([]any) and
// in-memory builders ([]map[string]any, []string) into a single []any for
// iteration.
func anySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case []string:
		// Go-built maps (no JSON round-trip) carry typed string slices.
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func maclawAppStringListFromAny(value any) []string {
	out := []string{}
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text != "" {
			out = append(out, text)
		}
	}
	switch items := value.(type) {
	case string:
		add(items)
	case []string:
		for _, item := range items {
			add(item)
		}
	case []any:
		for _, item := range items {
			text, _ := item.(string)
			add(text)
		}
	}
	return out
}

func firstNonEmptyMaclawAppString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonEmptyMaclawAppStringList(values ...[]string) []string {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		out := make([]string, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func containsMaclawAppString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (registry *maclawAppApprovalRegistry) upsert(instance maclawAppApprovalInstance) maclawAppApprovalInstance {
	incoming := cloneMaclawAppApprovalInstance(instance)
	next := make([]maclawAppApprovalInstance, 0, len(registry.Instances)+1)
	for _, existing := range registry.Instances {
		if existing.AppID == instance.AppID && existing.InstanceID == instance.InstanceID {
			incoming = mergeMaclawAppApprovalInstance(existing, incoming)
			continue
		}
		next = append(next, existing)
	}
	next = append([]maclawAppApprovalInstance{incoming}, next...)
	if len(next) > 500 {
		next = next[:500]
	}
	registry.Instances = next
	return cloneMaclawAppApprovalInstance(incoming)
}

func (registry *maclawAppInstallRegistry) upsert(record maclawAppInstallRecord) {
	next := make([]maclawAppInstallRecord, 0, len(registry.Installs)+1)
	next = append(next, record)
	for _, existing := range registry.Installs {
		if existing.AppID == record.AppID {
			continue
		}
		next = append(next, existing)
	}
	if len(next) > 200 {
		next = next[:200]
	}
	registry.Installs = next
}

func firstMaclawAppID(ids []string) string {
	if len(ids) == 0 {
		return "app"
	}
	id := strings.ToLower(ids[0])
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func shortRandomHex() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maclawAppStringFromAny(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

func maclawAppStringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return normalizeMaclawAppScopes(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return normalizeMaclawAppScopes(out)
	default:
		return nil
	}
}

func normalizeMaclawAppRiskLevel(value string) string {
	switch strings.TrimSpace(value) {
	case "low", "medium", "high", "critical":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func normalizeMaclawAppScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	return normalized
}

func cloneMaclawAppPlanDependencies(deps []maclawAppInstallPlanDependency) []maclawAppInstallPlanDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]maclawAppInstallPlanDependency, 0, len(deps))
	for _, dep := range deps {
		dep.AppIDs = append([]string(nil), dep.AppIDs...)
		out = append(out, dep)
	}
	return out
}

func cloneMaclawAppPlanDependenciesForApp(deps []maclawAppInstallPlanDependency, appID string) []maclawAppInstallPlanDependency {
	out := []maclawAppInstallPlanDependency{}
	for _, dep := range deps {
		if !maclawAppPlanDependencyMatchesAppID(dep, appID) {
			continue
		}
		dep.AppIDs = append([]string(nil), dep.AppIDs...)
		out = append(out, dep)
	}
	return out
}

func mapAnyToInterfaceMap(value map[string]any) map[string]interface{} {
	if value == nil {
		return nil
	}
	out := make(map[string]interface{}, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func cloneMapAny(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}

func cloneMaclawAppMapSlice(items []map[string]any) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		cloned = append(cloned, cloneMapAny(item))
	}
	return cloned
}
