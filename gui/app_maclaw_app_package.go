package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	maclawappcontract "github.com/RapidAI/CodeClaw/internal/maclawappcontract"
)

func firstMaclawAppGovernanceIssueBlockingLocalInstall(issues []maclawAppReviewIssue) *maclawAppReviewIssue {
	for i := range issues {
		message := strings.ToLower(strings.TrimSpace(issues[i].Message))
		if strings.Contains(message, "does not match") {
			return &issues[i]
		}
	}
	return nil
}

func parseMaclawAppPackage(packageJSON string) (map[string]any, []string, []string, error) {
	var pkg map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &pkg); err != nil {
		return nil, nil, nil, fmt.Errorf("decode maclaw app package: %w", err)
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, true)
	if err != nil {
		return nil, nil, nil, err
	}
	appIDs := make([]string, 0, len(entries))
	appNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		appIDs = append(appIDs, entry.ID)
		appNames = append(appNames, entry.Name)
	}
	return pkg, appIDs, appNames, nil
}

func parseMaclawAppPackageEntriesFromMap(pkg map[string]any, requirePack bool) ([]parsedMaclawAppEntry, error) {
	if requirePack && stringMapValue(pkg, "schema") != "maclaw.app.pack.v1" {
		return nil, fmt.Errorf("maclaw app package schema must be maclaw.app.pack.v1")
	}
	if stringMapValue(pkg, "privateMarker") != "x_maclaw_apps" {
		return nil, fmt.Errorf("maclaw app package privateMarker must be x_maclaw_apps")
	}
	rawApps := anySlice(pkg["apps"])
	if len(rawApps) == 0 {
		return nil, fmt.Errorf("maclaw app package apps must be a non-empty array")
	}
	entries := make([]parsedMaclawAppEntry, 0, len(rawApps))
	seenIDs := make(map[string]struct{}, len(rawApps))
	for i, raw := range rawApps {
		entry := anyMap(raw)
		if entry == nil {
			return nil, fmt.Errorf("maclaw app package apps[%d] must be an object", i)
		}
		parsed, err := parseMaclawAppEntryFromMap(entry, fmt.Sprintf("maclaw app package apps[%d]", i), seenIDs)
		if err != nil {
			return nil, err
		}
		entries = append(entries, parsed)
	}
	return entries, nil
}

func maclawAppPackageForSelectedAppIDs(pkg map[string]any, selectedAppIDs []string) (map[string]any, []parsedMaclawAppEntry, error) {
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, true)
	if err != nil {
		return nil, nil, err
	}
	originalSubmissionPackageSHAs := maclawAppSubmissionPackageSHAsByAppID(entries)
	selected := maclawAppSelectionIDSet(selectedAppIDs)
	if len(selected) == 0 {
		return cloneMapAny(pkg), entries, nil
	}
	installPackage, err := maclawappcontract.SelectHubPackageApps(pkg, selectedAppIDs)
	if err != nil {
		return nil, nil, err
	}
	filteredEntries, err := parseMaclawAppPackageEntriesFromMap(installPackage, true)
	if err != nil {
		return nil, nil, err
	}
	maclawAppFilterBundledDependenciesForSelectedEntries(installPackage, filteredEntries)
	maclawAppRestoreSelectedSubmissionPackageSHAs(installPackage, originalSubmissionPackageSHAs)
	filteredEntries, err = parseMaclawAppPackageEntriesFromMap(installPackage, true)
	if err != nil {
		return nil, nil, err
	}
	return installPackage, filteredEntries, nil
}

// maclawAppFilterBundledDependenciesForSelectedEntries keeps selected Hub
// installs self-contained. The contract-layer selector intentionally knows
// nothing about bundled skill payloads, so scope both the pack-level fallback
// and each retained entry here before handing the package to the installer.
func maclawAppFilterBundledDependenciesForSelectedEntries(pkg map[string]any, entries []parsedMaclawAppEntry) {
	if pkg == nil || len(entries) == 0 {
		return
	}
	selected := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		for id := range maclawAppSelectionIDSet([]string{entry.ID}) {
			selected[id] = struct{}{}
		}
	}
	filter := func(raw any) maclawAppBundledDependencies {
		bundled := maclawAppBundledDependenciesFromDoc(map[string]any{"bundled_dependencies": raw})
		out := maclawAppBundledDependencies{Schema: bundled.Schema}
		for _, skill := range bundled.Skills {
			if len(skill.AppIDs) == 0 {
				out.Skills = append(out.Skills, skill)
				continue
			}
			matched := make([]string, 0, len(skill.AppIDs))
			for _, appID := range skill.AppIDs {
				if _, ok := selected[strings.ToLower(strings.TrimSpace(appID))]; ok {
					matched = append(matched, appID)
				}
			}
			if len(matched) > 0 {
				skill.AppIDs = matched
				out.Skills = append(out.Skills, skill)
			}
		}
		return out
	}
	if raw, ok := pkg["bundled_dependencies"]; ok {
		if bundled := filter(raw); len(bundled.Skills) > 0 {
			pkg["bundled_dependencies"] = bundled
		} else {
			delete(pkg, "bundled_dependencies")
		}
	}
	for _, raw := range anySlice(pkg["apps"]) {
		entry := anyMap(raw)
		if entry == nil {
			continue
		}
		if bundled, ok := entry["bundled_dependencies"]; ok {
			if filtered := filter(bundled); len(filtered.Skills) > 0 {
				entry["bundled_dependencies"] = filtered
			} else {
				delete(entry, "bundled_dependencies")
			}
		}
	}
}

func maclawAppRestoreSelectedSubmissionPackageSHAs(pkg map[string]any, packageSHAs map[string]string) {
	if len(pkg) == 0 || len(packageSHAs) == 0 {
		return
	}
	for _, raw := range anySlice(pkg["apps"]) {
		entry := anyMap(raw)
		app := anyMap(entry["app"])
		if len(app) == 0 {
			continue
		}
		appID := strings.ToLower(strings.TrimSpace(maclawAppStringValue(app, "id")))
		packageSHA := packageSHAs[appID]
		if packageSHA == "" {
			continue
		}
		governance := anyMap(app["governance"])
		submission := anyMap(governance["submission"])
		if len(submission) == 0 {
			continue
		}
		submission["package_sha256"] = packageSHA
	}
}

func maclawAppFilterEntryReviewEvidenceForSelectedEntries(entry map[string]any, entries []parsedMaclawAppEntry) {
	app := anyMap(entry["app"])
	governance := anyMap(app["governance"])
	submission := anyMap(governance["submission"])
	if len(submission) == 0 {
		return
	}
	if filtered := maclawAppReviewEvidenceForSelectedEntries(submission["review_evidence"], entries); filtered != nil {
		submission["review_evidence"] = filtered
	}
	if filtered := maclawAppReviewEvidenceForSelectedEntries(submission["maclaw_app_review_evidence"], entries); filtered != nil {
		submission["maclaw_app_review_evidence"] = filtered
	}
}

func maclawAppReviewEvidenceForSelectedEntries(raw any, entries []parsedMaclawAppEntry) map[string]any {
	evidence := anyMap(raw)
	if len(evidence) == 0 || len(entries) == 0 {
		return nil
	}
	selected := map[string]struct{}{}
	for _, entry := range entries {
		for key := range maclawAppSelectionIDSet([]string{entry.ID}) {
			selected[key] = struct{}{}
		}
	}
	filtered := map[string]any{}
	for key, value := range evidence {
		if _, ok := selected[strings.ToLower(strings.TrimSpace(key))]; !ok {
			continue
		}
		if valueMap := anyMap(value); valueMap != nil {
			filtered[key] = cloneMapAny(valueMap)
		} else {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return cloneMapAny(evidence)
	}
	return filtered
}

func parseMaclawAppEntryFromMap(entry map[string]any, path string, seenIDs map[string]struct{}) (parsedMaclawAppEntry, error) {
	if stringMapValue(entry, "schema") != "maclaw.app.v1" {
		return parsedMaclawAppEntry{}, fmt.Errorf("%s.schema must be maclaw.app.v1", path)
	}
	if stringMapValue(entry, "privateMarker") != "x_maclaw_apps" {
		return parsedMaclawAppEntry{}, fmt.Errorf("%s.privateMarker must be x_maclaw_apps", path)
	}
	app, ok := entry["app"].(map[string]any)
	if !ok {
		return parsedMaclawAppEntry{}, fmt.Errorf("%s.app must be an object", path)
	}
	appID := strings.TrimSpace(stringMapValue(app, "id"))
	if appID == "" {
		return parsedMaclawAppEntry{}, fmt.Errorf("%s.app.id is required", path)
	}
	if seenIDs != nil {
		if _, ok := seenIDs[appID]; ok {
			return parsedMaclawAppEntry{}, fmt.Errorf("%s.app.id duplicates %q", path, appID)
		}
		seenIDs[appID] = struct{}{}
	}
	kind := normalizeMaclawAppKind(stringMapValue(app, "kind"))
	if err := normalizeMaclawAppWorkspaceLayout(app, kind, path+".app"); err != nil {
		return parsedMaclawAppEntry{}, err
	}
	if err := normalizeMaclawAppWorkflowMapping(app, kind, path+".app"); err != nil {
		return parsedMaclawAppEntry{}, err
	}
	parsed := parsedMaclawAppEntry{
		Schema: stringMapValue(entry, "schema"),
		Entry:  entry,
		App:    app,
		ID:     appID,
		Name:   stringMapValue(app, "name"),
		Kind:   kind,
	}
	if err := validateMaclawAppKindContract(parsed, path+".app"); err != nil {
		return parsedMaclawAppEntry{}, err
	}
	return parsed, nil
}

func validateMaclawAppKindContract(entry parsedMaclawAppEntry, path string) error {
	kind := normalizeMaclawAppKind(entry.Kind)
	switch kind {
	case "enterprise_approval_app":
		if !maclawAppHasWorkflowSkillForEntry(entry) {
			return fmt.Errorf("%s workflow_skill dependency is required for enterprise_approval_app", path)
		}
	case "enterprise_normal_app":
		if len(maclawAppApprovalBindingMapsForEntry(entry)) > 0 {
			return fmt.Errorf("%s.binding.mis.approvalBindings is only valid for enterprise_approval_app", path)
		}
		if maclawAppWorkflowMappingForEntry(entry) != nil {
			return fmt.Errorf("%s.binding.workflow is only valid for enterprise_approval_app", path)
		}
	case "tool_app":
		if maclawAppDataSrvBlockForEntry(entry) != nil {
			return fmt.Errorf("%s.binding.datasrv is not valid for tool_app", path)
		}
		if len(maclawAppApprovalBindingMapsForEntry(entry)) > 0 {
			return fmt.Errorf("%s.binding.mis.approvalBindings is not valid for tool_app", path)
		}
		if maclawAppWorkflowMappingForEntry(entry) != nil {
			return fmt.Errorf("%s.binding.workflow is not valid for tool_app", path)
		}
	case "automation_app", "":
		return nil
	default:
		return fmt.Errorf("%s.kind must be enterprise_approval_app, enterprise_normal_app, tool_app, or automation_app", path)
	}
	return nil
}

func maclawAppReviewEvidenceForEntry(entry parsedMaclawAppEntry) map[string]any {
	governance := anyMap(entry.App["governance"])
	if governance == nil {
		return nil
	}
	submission := anyMap(governance["submission"])
	if submission == nil {
		return nil
	}
	evidence := maclawAppReviewEvidenceFromMetadata(submission)
	if evidence == nil {
		return nil
	}
	for _, key := range []string{entry.ID, entry.Name} {
		if key == "" {
			continue
		}
		if appEvidence := anyMap(evidence[key]); appEvidence != nil {
			return cloneMapAny(appEvidence)
		}
	}
	if maclawAppStringValue(evidence, "run_id", "runId", "approval_status", "approvalStatus", "result_contract_primary", "resultContractPrimary") != "" {
		return cloneMapAny(evidence)
	}
	return nil
}

func maclawAppReviewEvidenceNumber(value any) any {
	if n, ok := maclawAppNumberFromAny(value); ok {
		if math.Trunc(n) == n {
			return int(n)
		}
		return n
	}
	return nil
}

func applyMaclawAppDataSrvHubPackageSignatureMetadata(metadata map[string]interface{}, submission map[string]any) {
	if metadata == nil || submission == nil {
		return
	}
	signature := anyMap(firstNonEmptyMaclawAppAny(submission["package_signature"], submission["packageSignature"]))
	if signature == nil {
		return
	}
	metadata["hub_package_signature"] = cloneMapAny(signature)
	if algorithm := maclawAppStringValue(signature, "algorithm"); algorithm != "" {
		metadata["hub_package_signature_algorithm"] = algorithm
	}
	if fingerprint := firstNonEmptyMaclawAppString(maclawAppStringValue(signature, "public_key_fingerprint"), maclawAppStringValue(signature, "key_fingerprint"), maclawAppStringValue(signature, "fingerprint")); fingerprint != "" {
		metadata["hub_package_signature_fingerprint"] = fingerprint
	}
	if signedAt := firstNonEmptyMaclawAppString(maclawAppStringValue(signature, "signed_at"), maclawAppStringValue(signature, "signedAt")); signedAt != "" {
		metadata["hub_package_signature_signed_at"] = signedAt
	}
	if signedBy := firstNonEmptyMaclawAppString(maclawAppStringValue(signature, "signed_by"), maclawAppStringValue(signature, "signedBy")); signedBy != "" {
		metadata["hub_package_signature_signed_by"] = signedBy
	}
}

func applyMaclawAppDataSrvReviewEvidenceMetadata(metadata map[string]interface{}, reviewEvidence map[string]any) {
	if metadata == nil || reviewEvidence == nil {
		return
	}
	record := maclawAppReviewEvidenceRecord(reviewEvidence)
	if record == nil {
		return
	}
	for _, pair := range []struct {
		keys []string
		meta string
	}{
		{[]string{"reviewStatus", "review_status", "status"}, "review_evidence_status"},
		{[]string{"runID", "runId", "run_id"}, "review_evidence_run_id"},
		{[]string{"testProtocolFingerprint", "test_protocol_fingerprint"}, "review_evidence_test_protocol_fingerprint"},
		{[]string{"resultContractPrimary", "result_contract_primary"}, "review_evidence_result_contract_primary"},
		{[]string{"resultCoveragePrimary", "result_coverage_primary"}, "review_evidence_result_coverage_primary"},
		{[]string{"approvalStatus", "approval_status"}, "review_evidence_approval_status"},
		{[]string{"currentNode", "current_node"}, "review_evidence_current_node"},
	} {
		if value := maclawAppStringValue(record, pair.keys...); value != "" {
			metadata[pair.meta] = value
		}
	}
	for _, pair := range []struct {
		keys []string
		meta string
	}{
		{[]string{"resultCoverageCoveredCount", "result_coverage_covered_count"}, "review_evidence_result_coverage_covered_count"},
		{[]string{"resultCoverageMissingCount", "result_coverage_missing_count"}, "review_evidence_result_coverage_missing_count"},
		{[]string{"outputCount", "output_count"}, "review_evidence_output_count"},
		{[]string{"artifactCount", "artifact_count"}, "review_evidence_artifact_count"},
	} {
		for _, key := range pair.keys {
			if value, ok := maclawAppNumberFromAny(record[key]); ok {
				metadata[pair.meta] = value
				break
			}
		}
	}
}

func maclawAppReviewEvidenceRecord(reviewEvidence map[string]any) map[string]any {
	if reviewEvidence == nil {
		return nil
	}
	if maclawAppStringValue(reviewEvidence, "run_id", "runId", "test_protocol_fingerprint", "testProtocolFingerprint", "approval_status", "approvalStatus", "current_node", "currentNode", "result_coverage_primary", "resultCoveragePrimary") != "" {
		return reviewEvidence
	}
	for _, value := range reviewEvidence {
		if record := maclawAppReviewEvidenceRecord(anyMap(value)); record != nil {
			return record
		}
	}
	return nil
}

func applyMaclawAppDataSrvTestEvidenceMetadata(metadata map[string]interface{}, testEvidence map[string]any) {
	if metadata == nil || testEvidence == nil {
		return
	}
	for _, pair := range []struct {
		keys []string
		meta string
	}{
		{[]string{"runId", "run_id"}, "test_evidence_run_id"},
		{[]string{"verifiedAt", "verified_at"}, "test_evidence_verified_at"},
		{[]string{"definitionFingerprint", "definition_fingerprint", "definitionHash", "definition_hash"}, "test_evidence_definition_fingerprint"},
		{[]string{"testProtocolFingerprint", "test_protocol_fingerprint", "testProtocolHash", "test_protocol_hash"}, "test_evidence_test_protocol_fingerprint"},
		{[]string{"workspaceLayoutFingerprint", "workspace_layout_fingerprint", "workspaceLayoutHash", "workspace_layout_hash", "layoutFingerprint", "layout_fingerprint"}, "test_evidence_workspace_layout_fingerprint"},
		{[]string{"artifactName", "artifact_name"}, "test_evidence_artifact_name"},
		{[]string{"artifactURI", "artifactUri", "artifact_uri"}, "test_evidence_artifact_uri"},
		{[]string{"artifactPath", "artifact_path"}, "test_evidence_artifact_path"},
		{[]string{"primaryResult", "primary_result"}, "test_evidence_primary_result"},
		{[]string{"resultType", "result_type"}, "test_evidence_result_type"},
		{[]string{"outputType", "output_type"}, "test_evidence_output_type"},
		{[]string{"resultContent", "result_content"}, "test_evidence_result_content"},
	} {
		if value := maclawAppStringValue(testEvidence, pair.keys...); value != "" {
			metadata[pair.meta] = value
		}
	}
	if _, ok := metadata["test_evidence_test_protocol_fingerprint"]; !ok {
		if protocol := anyMap(firstNonEmptyMaclawAppAny(testEvidence["testProtocol"], testEvidence["test_protocol"])); protocol != nil {
			if fingerprint := maclawAppStringValue(protocol, "fingerprint", "hash"); fingerprint != "" {
				metadata["test_evidence_test_protocol_fingerprint"] = fingerprint
			}
		}
	}
	approval := anyMap(firstNonEmptyMaclawAppAny(testEvidence["approvalInstance"], testEvidence["approval_instance"], testEvidence["approval"]))
	approvalOutputs := anySlice(firstNonEmptyMaclawAppAny(approval["outputs"], approval["output_blocks"], approval["outputBlocks"]))
	approvalArtifacts := anySlice(approval["artifacts"])
	if value, ok := firstNonEmptyMaclawAppAny(testEvidence["artifactPresent"], testEvidence["artifact_present"]).(bool); ok {
		metadata["test_evidence_artifact_present"] = value
	}
	if value, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(testEvidence["artifactCount"], testEvidence["artifact_count"])); ok {
		metadata["test_evidence_artifact_count"] = value
	} else if artifacts := anySlice(testEvidence["artifacts"]); len(artifacts) > 0 {
		metadata["test_evidence_artifact_count"] = len(artifacts)
	} else if len(approvalArtifacts) > 0 {
		metadata["test_evidence_artifact_count"] = len(approvalArtifacts)
	}
	if value, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(testEvidence["outputCount"], testEvidence["output_count"])); ok {
		metadata["test_evidence_output_count"] = value
	} else if outputs := maclawAppTestEvidenceOutputs(testEvidence); len(outputs) > 0 {
		metadata["test_evidence_output_count"] = len(outputs)
	} else if len(approvalOutputs) > 0 {
		metadata["test_evidence_output_count"] = len(approvalOutputs)
	}
	if payload := anyMap(firstNonEmptyMaclawAppAny(testEvidence["resultPayload"], testEvidence["result_payload"])); payload != nil {
		metadata["test_evidence_result_payload"] = payload
		applyMaclawAppDataSrvResultPayloadMetadata(metadata, payload)
	} else if payload := anyMap(firstNonEmptyMaclawAppAny(approval["resultPayload"], approval["result_payload"])); payload != nil {
		metadata["test_evidence_result_payload"] = payload
		applyMaclawAppDataSrvResultPayloadMetadata(metadata, payload)
	}
	if outputs := maclawAppTestEvidenceOutputs(testEvidence); len(outputs) > 0 {
		metadata["test_evidence_outputs"] = outputs
		applyMaclawAppDataSrvOutputMetadata(metadata, outputs)
	} else if len(approvalOutputs) > 0 {
		metadata["test_evidence_outputs"] = approvalOutputs
		applyMaclawAppDataSrvOutputMetadata(metadata, approvalOutputs)
	}
	if artifacts := anySlice(testEvidence["artifacts"]); len(artifacts) > 0 {
		metadata["test_evidence_artifacts"] = artifacts
		applyMaclawAppDataSrvArtifactMetadata(metadata, artifacts)
	} else if len(approvalArtifacts) > 0 {
		metadata["test_evidence_artifacts"] = approvalArtifacts
		applyMaclawAppDataSrvArtifactMetadata(metadata, approvalArtifacts)
	}
	if coverage := anyMap(firstNonEmptyMaclawAppAny(testEvidence["resultCoverage"], testEvidence["result_coverage"])); coverage != nil {
		metadata["test_evidence_result_coverage"] = coverage
		if value, ok := coverage["ok"].(bool); ok {
			metadata["test_evidence_result_coverage_ok"] = value
		}
		if primary := maclawAppStringValue(coverage, "primary"); primary != "" {
			metadata["test_evidence_result_coverage_primary"] = primary
		}
		if covered := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(coverage["coveredTypes"], coverage["covered_types"])); len(covered) > 0 {
			metadata["test_evidence_covered_types"] = covered
			metadata["test_evidence_result_coverage_covered_count"] = len(covered)
		}
		if missing := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(coverage["missingTypes"], coverage["missing_types"])); len(missing) > 0 {
			metadata["test_evidence_missing_types"] = missing
		}
	}
	if approval != nil {
		metadata["test_evidence_approval_instance"] = approval
		if instanceID := firstNonEmptyMaclawAppString(
			maclawAppStringValue(approval, "instanceId", "instance_id", "approvalInstanceId", "approval_instance_id", "workflowInstanceId", "workflow_instance_id"),
			maclawAppStringValue(approval, "approvalID", "approvalId", "approval_id"),
		); instanceID != "" {
			metadata["test_evidence_approval_instance_id"] = instanceID
		}
		if approvalID := maclawAppStringValue(approval, "approvalID", "approvalId", "approval_id", "recordApprovalID", "record_approval_id"); approvalID != "" {
			metadata["test_evidence_approval_id"] = approvalID
		}
		if recordID := maclawAppStringValue(approval, "recordID", "record_id"); recordID != "" {
			metadata["test_evidence_record_id"] = recordID
		}
		if status := maclawAppStringValue(approval, "status", "approvalStatus", "approval_status", "resultStatus", "result_status"); status != "" {
			metadata["test_evidence_approval_status"] = status
		}
		for _, pair := range []struct {
			keys []string
			meta string
		}{
			{[]string{"currentNode", "current_node"}, "test_evidence_approval_current_node"},
			{[]string{"workflowSkillId", "workflowSkillID", "workflow_skill_id"}, "test_evidence_workflow_skill_id"},
			{[]string{"workflowVersion", "workflow_version"}, "test_evidence_workflow_version"},
			{[]string{"businessStatus", "business_status"}, "test_evidence_business_status"},
			{[]string{"resultStatus", "result_status"}, "test_evidence_result_status"},
			{[]string{"datasetID", "datasetId", "dataset_id"}, "test_evidence_dataset_id"},
			{[]string{"blueprintID", "blueprintId", "blueprint_id"}, "test_evidence_blueprint_id"},
			{[]string{"objectRole", "object_role"}, "test_evidence_object_role"},
			{[]string{"approvalEvent", "approval_event"}, "test_evidence_approval_event"},
			{[]string{"approvalWorkflowID", "approvalWorkflowId", "approval_workflow_id"}, "test_evidence_approval_workflow_id"},
			{[]string{"detailURL", "detailUrl", "detail_url"}, "test_evidence_detail_url"},
		} {
			if value := maclawAppStringValue(approval, pair.keys...); value != "" {
				metadata[pair.meta] = value
			}
		}
		if verified, ok := firstNonEmptyMaclawAppAny(approval["approvalInstanceViewVerified"], approval["approval_instance_view_verified"], approval["approvalViewVerified"], approval["approval_view_verified"], approval["viewVerified"], approval["view_verified"]).(bool); ok {
			metadata["test_evidence_approval_view_verified"] = verified
		}
	}
}

func maclawAppTestEvidenceOutputs(testEvidence map[string]any) []any {
	if testEvidence == nil {
		return nil
	}
	if outputs := anySlice(testEvidence["outputs"]); len(outputs) > 0 {
		return outputs
	}
	return anySlice(testEvidence["output_blocks"])
}

func maclawAppWorkspaceLayoutStudioMetadata(layout map[string]any) map[string]any {
	studio := anyMap(layout["studio"])
	if studio == nil {
		return nil
	}
	out := map[string]any{}
	if saved, ok := firstNonEmptyMaclawAppAny(studio["savedInManifest"], studio["saved_in_manifest"]).(bool); ok {
		out["savedInManifest"] = saved
		out["saved_in_manifest"] = saved
	}
	if editable, ok := studio["editable"].(bool); ok {
		out["editable"] = editable
	}
	if imported, ok := firstNonEmptyMaclawAppAny(studio["importedFromDataSrv"], studio["imported_from_datasrv"]).(bool); ok {
		out["importedFromDataSrv"] = imported
		out["imported_from_datasrv"] = imported
	}
	if updatedBy := firstNonEmptyMaclawAppString(maclawAppStringValue(studio, "updatedBy"), maclawAppStringValue(studio, "updated_by")); updatedBy != "" {
		out["updatedBy"] = updatedBy
		out["updated_by"] = updatedBy
	}
	return compactPayload(out)
}

func maclawAppWorkspaceLayoutMetadataForEntry(entry parsedMaclawAppEntry) map[string]interface{} {
	var ui map[string]any
	if entry.App != nil {
		ui = anyMap(entry.App["ui"])
	}
	if ui == nil && entry.App != nil {
		if binding := anyMap(entry.App["binding"]); binding != nil {
			ui = anyMap(binding["ui"])
		}
	}
	governance := maclawAppGovernanceMetadataForEntry(entry)
	governanceWorkspaceLayout := anyMap(governance["workspace_layout"])
	useGovernanceWorkspaceLayout := governanceWorkspaceLayout != nil && (maclawAppStringValue(governanceWorkspaceLayout, "fingerprint") != "" || len(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(governanceWorkspaceLayout["regionIds"], governanceWorkspaceLayout["region_ids"]))) > 0 || firstNonEmptyMaclawAppAny(governanceWorkspaceLayout["visibleRegionCount"], governanceWorkspaceLayout["visible_region_count"]) != nil || anyMap(governanceWorkspaceLayout["studio"]) != nil)
	if ui == nil || useGovernanceWorkspaceLayout {
		workspaceLayout := governanceWorkspaceLayout
		if workspaceLayout == nil {
			return nil
		}
		out := cloneMapAny(workspaceLayout)
		if ui != nil {
			entryName := strings.TrimSpace(stringMapValue(ui, "entry"))
			if entryName == "" {
				entryName = maclawAppStringValue(out, "entry")
			}
			if entryName != "" {
				out["entry"] = entryName
			}
			layouts := anyMap(ui["layouts"])
			uiLayout := anyMap(layouts[entryName])
			if uiLayout != nil {
				if schema := stringMapValue(ui, "schema"); schema != "" && maclawAppStringValue(out, "schema") == "" {
					out["schema"] = schema
				}
				if template := maclawAppStringValue(uiLayout, "template"); template != "" {
					out["template"] = template
				}
				if density := maclawAppStringValue(uiLayout, "density"); density != "" {
					out["density"] = density
				}
				if primary := maclawAppStringValue(uiLayout, "primaryRegion", "primary_region"); primary != "" {
					out["primaryRegion"] = primary
					out["primary_region"] = primary
				}
				if output := maclawAppStringValue(uiLayout, "outputRegion", "output_region"); output != "" {
					out["outputRegion"] = output
					out["output_region"] = output
				}
				if generated, ok := ui["generated"].(bool); ok {
					if _, exists := out["generated"]; !exists {
						out["generated"] = generated
					}
				}
				if _, exists := out["regions"]; !exists {
					if regions := anySlice(uiLayout["regions"]); len(regions) > 0 {
						out["regions"] = regions
					}
				}
				if len(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(out["regionIds"], out["region_ids"]))) == 0 {
					if regionIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(uiLayout["regionIds"], uiLayout["region_ids"])); len(regionIDs) > 0 {
						out["regionIds"] = regionIDs
						out["region_ids"] = regionIDs
					}
				}
				if _, exists := out["regionCount"]; !exists {
					if count, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(uiLayout["regionCount"], uiLayout["region_count"])); ok && count > 0 {
						regionCount := int(math.Floor(count))
						out["regionCount"] = regionCount
						out["region_count"] = regionCount
					}
				}
				if _, exists := out["visibleRegionCount"]; !exists {
					if visibleCount, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(uiLayout["visibleRegionCount"], uiLayout["visible_region_count"])); ok && visibleCount >= 0 {
						visibleRegionCount := int(math.Floor(visibleCount))
						out["visibleRegionCount"] = visibleRegionCount
						out["visible_region_count"] = visibleRegionCount
					}
				}
				if anyMap(out["studio"]) == nil {
					if studio := maclawAppWorkspaceLayoutStudioMetadata(uiLayout); studio != nil {
						out["studio"] = studio
					}
				}
			}
		}
		if primary := maclawAppStringValue(out, "primaryRegion", "primary_region"); primary != "" {
			out["primaryRegion"] = primary
			out["primary_region"] = primary
		}
		if output := maclawAppStringValue(out, "outputRegion", "output_region"); output != "" {
			out["outputRegion"] = output
			out["output_region"] = output
		}
		if fingerprint := maclawAppStringValue(out, "fingerprint"); fingerprint != "" {
			out["fingerprint"] = fingerprint
		}
		if regionIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(out["regionIds"], out["region_ids"])); len(regionIDs) > 0 {
			out["regionIds"] = regionIDs
			out["region_ids"] = regionIDs
		}
		if visibleCount, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(out["visibleRegionCount"], out["visible_region_count"])); ok && visibleCount >= 0 {
			visibleRegionCount := int(math.Floor(visibleCount))
			out["visibleRegionCount"] = visibleRegionCount
			out["visible_region_count"] = visibleRegionCount
		}
		if count, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(out["regionCount"], out["region_count"])); ok && count > 0 {
			regionCount := int(math.Floor(count))
			out["regionCount"] = regionCount
			out["region_count"] = regionCount
		} else if regions := anySlice(out["regions"]); len(regions) > 0 {
			out["regionCount"] = len(regions)
			out["region_count"] = len(regions)
		}
		if regions := anySlice(out["regions"]); len(regions) > 0 {
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
			if len(regionIDs) > 0 && len(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(out["regionIds"], out["region_ids"]))) == 0 {
				out["regionIds"] = regionIDs
				out["region_ids"] = regionIDs
			}
			if _, exists := out["visibleRegionCount"]; !exists {
				out["visibleRegionCount"] = visibleRegionCount
				out["visible_region_count"] = visibleRegionCount
			}
		}
		if studio := maclawAppWorkspaceLayoutStudioMetadata(out); studio != nil {
			out["studio"] = studio
			if saved, ok := studio["savedInManifest"].(bool); ok {
				out["studio_saved_in_manifest"] = saved
			}
			if editable, ok := studio["editable"].(bool); ok {
				out["studio_editable"] = editable
			}
			if updatedBy := maclawAppStringValue(studio, "updatedBy", "updated_by"); updatedBy != "" {
				out["studio_updated_by"] = updatedBy
			}
		}
		return compactPayload(out)
	}
	entryName := strings.TrimSpace(stringMapValue(ui, "entry"))
	layouts := anyMap(ui["layouts"])
	layout := anyMap(layouts[entryName])
	out := map[string]interface{}{
		"schema": stringMapValue(ui, "schema"),
		"entry":  entryName,
	}
	if generated, ok := ui["generated"].(bool); ok {
		out["generated"] = generated
	}
	if layout != nil {
		if template := maclawAppStringValue(layout, "template"); template != "" {
			out["template"] = template
		}
		if density := maclawAppStringValue(layout, "density"); density != "" {
			out["density"] = density
		}
		if primary := maclawAppStringValue(layout, "primaryRegion", "primary_region"); primary != "" {
			out["primaryRegion"] = primary
			out["primary_region"] = primary
		}
		if output := maclawAppStringValue(layout, "outputRegion", "output_region"); output != "" {
			out["outputRegion"] = output
			out["output_region"] = output
		}
		if fingerprint := maclawAppStringValue(layout, "fingerprint"); fingerprint != "" {
			out["fingerprint"] = fingerprint
		}
		if regionIDs := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(layout["regionIds"], layout["region_ids"])); len(regionIDs) > 0 {
			out["regionIds"] = regionIDs
			out["region_ids"] = regionIDs
		}
		if visibleCount, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(layout["visibleRegionCount"], layout["visible_region_count"])); ok && visibleCount >= 0 {
			visibleRegionCount := int(math.Floor(visibleCount))
			out["visibleRegionCount"] = visibleRegionCount
			out["visible_region_count"] = visibleRegionCount
		}
		if navigation := maclawAppStringListFromAny(layout["navigation"]); len(navigation) > 0 {
			out["navigation"] = navigation
		}
		if list := anyMap(layout["list"]); list != nil {
			listOut := map[string]interface{}{}
			if columns := maclawAppStringListFromAny(list["columns"]); len(columns) > 0 {
				listOut["columns"] = columns
			}
			if len(listOut) > 0 {
				out["list"] = listOut
			}
		}
		if studio := maclawAppWorkspaceLayoutStudioMetadata(layout); studio != nil {
			out["studio"] = studio
			if saved, ok := studio["savedInManifest"].(bool); ok {
				out["studio_saved_in_manifest"] = saved
			}
			if editable, ok := studio["editable"].(bool); ok {
				out["studio_editable"] = editable
			}
			if updatedBy := maclawAppStringValue(studio, "updatedBy", "updated_by"); updatedBy != "" {
				out["studio_updated_by"] = updatedBy
			}
		}
		if regions := anySlice(layout["regions"]); len(regions) > 0 {
			out["regionCount"] = len(regions)
			out["region_count"] = len(regions)
			out["regions"] = regions
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
			if len(regionIDs) > 0 && len(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(out["regionIds"], out["region_ids"]))) == 0 {
				out["regionIds"] = regionIDs
				out["region_ids"] = regionIDs
			}
			if _, exists := out["visibleRegionCount"]; !exists {
				out["visibleRegionCount"] = visibleRegionCount
				out["visible_region_count"] = visibleRegionCount
			}
		}
	}
	return compactPayload(out)
}

func maclawAppGovernanceMetadataForEntry(entry parsedMaclawAppEntry) map[string]interface{} {
	governance := anyMap(entry.App["governance"])
	if governance == nil {
		return nil
	}
	return compactPayload(map[string]interface{}{
		"status":                  maclawAppStringValue(governance, "status"),
		"risk_level":              maclawAppStringValue(governance, "riskLevel", "risk_level"),
		"required_scopes":         governance["requiredScopes"],
		"dependencies":            governance["dependencies"],
		"dependency_verification": firstNonEmptyMaclawAppAny(governance["dependencyVerification"], governance["dependency_verification"]),
		"workspace_layout":        governance["workspaceLayout"],
		"result_contract":         firstNonEmptyMaclawAppAny(governance["resultContract"], governance["result_contract"]),
		"workflow_contract":       firstNonEmptyMaclawAppAny(governance["workflowContract"], governance["workflow_contract"]),
		"test_evidence":           firstNonEmptyMaclawAppAny(governance["testEvidence"], governance["test_evidence"]),
		"submission":              governance["submission"],
	})
}

func maclawAppDataSrvRoleBindingsForEntry(entry parsedMaclawAppEntry) []map[string]interface{} {
	datasrv := maclawAppDataSrvBlockForEntry(entry)
	if datasrv == nil {
		return nil
	}
	datasetID := maclawAppStringValue(datasrv, "datasetID", "dataset_id", "dataset")
	if datasetID == "" {
		return nil
	}
	domain := firstNonEmptyMaclawAppString(maclawAppStringValue(datasrv, "domain"), maclawAppDomainFromDatasetID(datasetID))
	templateID := firstNonEmptyMaclawAppString(maclawAppStringValue(datasrv, "templateID", "template_id"), datasetID)
	roleBindings := []map[string]interface{}{}
	seen := map[string]struct{}{}
	add := func(objectRole string, required bool, binding map[string]any) {
		objectRole = firstNonEmptyMaclawAppString(objectRole, maclawAppStringValue(datasrv, "objectRole", "object_role", "businessObjectRole", "business_object_role"))
		objectRole = strings.TrimSpace(objectRole)
		if objectRole == "" {
			return
		}
		key := strings.ToLower(objectRole)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		roleBindings = append(roleBindings, compactPayload(map[string]interface{}{
			"object_role": objectRole,
			"domain":      domain,
			"dataset_id":  datasetID,
			"template_id": firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "templateID", "template_id"), templateID),
			"required":    required,
		}))
	}
	switch normalizeMaclawAppKind(entry.Kind) {
	case "enterprise_approval_app":
		for _, binding := range maclawAppApprovalBindingMapsForEntry(entry) {
			add(firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "objectRole", "object_role"), maclawAppStringValue(binding, "businessObjectRole", "business_object_role"), maclawAppStringValue(binding, "role")), true, binding)
		}
		if len(roleBindings) == 0 {
			add("", true, nil)
		}
	case "enterprise_normal_app":
		add("", true, nil)
	}
	return roleBindings
}

func maclawAppDataSrvBlockForEntry(entry parsedMaclawAppEntry) map[string]any {
	for _, holder := range maclawAppBindingHolders(entry) {
		if datasrv := anyMap(holder["datasrv"]); datasrv != nil {
			return datasrv
		}
	}
	return nil
}

func maclawAppAppSkillBlockForEntry(entry parsedMaclawAppEntry) map[string]any {
	for _, holder := range maclawAppBindingHolders(entry) {
		if appSkill := anyMap(holder["appSkill"]); appSkill != nil {
			return appSkill
		}
		if appSkill := anyMap(holder["app_skill"]); appSkill != nil {
			return appSkill
		}
		if skill := anyMap(holder["skill"]); skill != nil {
			return skill
		}
	}
	return nil
}

func maclawAppBlueprintIDForEntry(entry parsedMaclawAppEntry) string {
	for _, holder := range maclawAppBindingHolders(entry) {
		if value := maclawAppStringValue(holder, "blueprintID", "blueprint_id"); value != "" {
			return value
		}
	}
	if datasrv := maclawAppDataSrvBlockForEntry(entry); datasrv != nil {
		return maclawAppStringValue(datasrv, "blueprintID", "blueprint_id")
	}
	return ""
}

func normalizeMaclawAppWorkspaceLayout(app map[string]any, kind, path string) error {
	if app == nil {
		return nil
	}
	entry := maclawAppWorkspaceEntryForKind(kind)
	defaultUI := defaultMaclawAppWorkspaceLayout(kind)
	rawUI, exists := app["ui"]
	if !exists || rawUI == nil {
		if binding := anyMap(app["binding"]); binding != nil {
			if bindingUI := anyMap(binding["ui"]); bindingUI != nil {
				rawUI = cloneMapAny(bindingUI)
				exists = true
			} else if bindingWorkspaceLayout := anyMap(binding["workspaceLayout"]); bindingWorkspaceLayout != nil {
				rawUI = cloneMapAny(bindingWorkspaceLayout)
				exists = true
			} else if bindingWorkspaceLayout := anyMap(binding["workspace_layout"]); bindingWorkspaceLayout != nil {
				rawUI = cloneMapAny(bindingWorkspaceLayout)
				exists = true
			}
		}
		if !exists || rawUI == nil {
			app["ui"] = defaultUI
			return nil
		}
	}
	ui := anyMap(rawUI)
	if ui == nil {
		return fmt.Errorf("%s.ui must be an object", path)
	}
	if schemaRaw, ok := ui["schema"]; ok && schemaRaw != nil {
		if schema, ok := schemaRaw.(string); !ok || strings.TrimSpace(schema) == "" {
			return fmt.Errorf("%s.ui.schema must be a non-empty string", path)
		} else {
			schema = strings.TrimSpace(schema)
			if schema != "maclaw.app.ui.v1" {
				return fmt.Errorf("%s.ui.schema must be maclaw.app.ui.v1", path)
			}
			ui["schema"] = schema
		}
	} else {
		ui["schema"] = "maclaw.app.ui.v1"
	}
	if _, exists := ui["generated"]; !exists {
		if generated, ok := defaultUI["generated"]; ok {
			ui["generated"] = generated
		}
	}
	if rawEntry, ok := ui["entry"]; ok && rawEntry != nil {
		value, ok := rawEntry.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s.ui.entry must be a non-empty string", path)
		}
		entry = strings.TrimSpace(value)
	}
	ui["entry"] = entry
	layoutsRaw, exists := ui["layouts"]
	if !exists || layoutsRaw == nil {
		ui["layouts"] = defaultUI["layouts"]
		app["ui"] = ui
		return nil
	}
	layouts := anyMap(layoutsRaw)
	if layouts == nil {
		return fmt.Errorf("%s.ui.layouts must be an object", path)
	}
	if len(layouts) == 0 {
		return fmt.Errorf("%s.ui.layouts must not be empty", path)
	}
	layout := anyMap(layouts[entry])
	if layout == nil {
		return fmt.Errorf("%s.ui.layouts.%s must be an object", path, entry)
	}
	defaults := anyMap(anyMap(defaultUI["layouts"])[entry])
	for key, value := range defaults {
		if _, exists := layout[key]; !exists {
			layout[key] = value
		}
	}
	if err := normalizeMaclawAppWorkspaceLayoutDetails(layout, path+".ui.layouts."+entry); err != nil {
		return err
	}
	layouts[entry] = layout
	ui["layouts"] = layouts
	app["ui"] = ui
	return nil
}

func normalizeMaclawAppWorkspaceLayoutDetails(layout map[string]any, path string) error {
	if value, ok := layout["template"]; ok && value != nil {
		template, ok := value.(string)
		if !ok || !validMaclawAppWorkspaceTemplate(template) {
			return fmt.Errorf("%s.template must be classic_split, left_nav, document_workspace, or dashboard", path)
		}
		layout["template"] = strings.TrimSpace(template)
	}
	if value, ok := layout["density"]; ok && value != nil {
		density, ok := value.(string)
		if !ok || !validMaclawAppWorkspaceDensity(density) {
			return fmt.Errorf("%s.density must be compact, comfortable, or spacious", path)
		}
		layout["density"] = strings.TrimSpace(density)
	}
	for _, key := range []string{"primaryRegion", "outputRegion"} {
		if value, ok := layout[key]; ok && value != nil {
			placement, ok := value.(string)
			if !ok || !validMaclawAppWorkspacePlacement(placement) {
				return fmt.Errorf("%s.%s must be left, center, right, bottom, or modal", path, key)
			}
			layout[key] = strings.TrimSpace(placement)
		}
	}
	if value, ok := layout["regions"]; ok && value != nil {
		regions, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s.regions must be an array", path)
		}
		seen := map[string]struct{}{}
		for i, raw := range regions {
			region := anyMap(raw)
			if region == nil {
				return fmt.Errorf("%s.regions[%d] must be an object", path, i)
			}
			id, ok := region["id"].(string)
			id = strings.TrimSpace(id)
			if !ok || id == "" {
				return fmt.Errorf("%s.regions[%d].id is required", path, i)
			}
			key := strings.ToLower(id)
			if _, exists := seen[key]; exists {
				return fmt.Errorf("%s.regions[%d].id duplicates %q", path, i, id)
			}
			seen[key] = struct{}{}
			region["id"] = id
			if role, ok := region["role"].(string); ok {
				region["role"] = strings.TrimSpace(role)
			}
			placement, ok := region["placement"].(string)
			placement = strings.TrimSpace(placement)
			if !ok || !validMaclawAppWorkspacePlacement(placement) {
				return fmt.Errorf("%s.regions[%d].placement must be left, center, right, bottom, or modal", path, i)
			}
			region["placement"] = placement
			regions[i] = region
		}
		layout["regions"] = regions
	}
	return nil
}

func defaultMaclawAppWorkspaceLayout(kind string) map[string]any {
	entry := maclawAppWorkspaceEntryForKind(kind)
	layout := map[string]any{}
	switch entry {
	case "approval_workspace":
		layout = map[string]any{
			"type":       "split_view",
			"template":   "classic_split",
			"density":    "comfortable",
			"toolbar":    []any{"create_request", "refresh", "export", "filter"},
			"navigation": []any{"my_requests", "pending_my_approval", "handled", "attention", "all"},
			"list":       map[string]any{"columns": []any{"title", "applicant", "current_node", "status", "updated_at"}},
			"detail":     map[string]any{"sections": []any{"summary", "form_data", "attachments", "timeline", "approval_actions", "result"}},
			"regions": []any{
				map[string]any{"id": "request_form", "role": "input", "placement": "left"},
				map[string]any{"id": "approval_inbox", "role": "instance_list", "placement": "center"},
				map[string]any{"id": "approval_detail", "role": "detail", "placement": "center"},
				map[string]any{"id": "result_panel", "role": "output", "placement": "bottom"},
			},
		}
	case "business_workspace":
		layout = map[string]any{
			"type":       "split_view",
			"template":   "classic_split",
			"density":    "comfortable",
			"toolbar":    []any{"new_record", "query", "refresh", "export"},
			"navigation": []any{"records", "recent", "needs_attention"},
			"list":       map[string]any{"columns": []any{"title", "status", "owner", "updated_at"}},
			"detail":     map[string]any{"sections": []any{"form_panel", "business_record", "operation_history", "output_panel"}},
			"regions": []any{
				map[string]any{"id": "operation_form", "role": "input", "placement": "left"},
				map[string]any{"id": "record_list", "role": "record_list", "placement": "center"},
				map[string]any{"id": "record_detail", "role": "detail", "placement": "center"},
				map[string]any{"id": "output_panel", "role": "output", "placement": "bottom"},
			},
		}
	default:
		layout = map[string]any{
			"type":     "tool_workspace",
			"template": "document_workspace",
			"density":  "comfortable",
			"toolbar":  []any{"add_file", "run", "cancel", "open_output"},
			"regions": []any{
				map[string]any{"id": "file_queue", "role": "input", "placement": "left"},
				map[string]any{"id": "settings_panel", "role": "parameters", "placement": "right"},
				map[string]any{"id": "preview_panel", "role": "preview", "placement": "center"},
				map[string]any{"id": "output_panel", "role": "output", "placement": "right"},
			},
		}
	}
	return map[string]any{
		"schema":    "maclaw.app.ui.v1",
		"generated": true,
		"entry":     entry,
		"layouts":   map[string]any{entry: layout},
	}
}

func maclawAppWorkspaceEntryForKind(kind string) string {
	switch normalizeMaclawAppKind(kind) {
	case "enterprise_approval_app":
		return "approval_workspace"
	case "enterprise_normal_app":
		return "business_workspace"
	default:
		return "tool_workspace"
	}
}

func validMaclawAppWorkspaceTemplate(value string) bool {
	switch strings.TrimSpace(value) {
	case "classic_split", "left_nav", "document_workspace", "dashboard":
		return true
	default:
		return false
	}
}

func validMaclawAppWorkspaceDensity(value string) bool {
	switch strings.TrimSpace(value) {
	case "compact", "comfortable", "spacious":
		return true
	default:
		return false
	}
}

func validMaclawAppWorkspacePlacement(value string) bool {
	switch strings.TrimSpace(value) {
	case "left", "center", "right", "bottom", "modal":
		return true
	default:
		return false
	}
}

func maclawAppDependenciesForEntry(entry parsedMaclawAppEntry) []maclawAppInstallPlanDependency {
	deps := []maclawAppInstallPlanDependency{}
	seen := map[string]int{}
	defaultSource := maclawAppDefaultDependencySourceForEntry(entry)
	add := func(dep maclawAppInstallPlanDependency) {
		dep.ID = strings.TrimSpace(dep.ID)
		if dep.ID == "" {
			return
		}
		dep.Version = strings.TrimSpace(dep.Version)
		dep.RequiredVersion = dep.Version
		dep.VersionStatus = maclawAppDependencyVersionStatus(dep)
		if dep.Kind == "" {
			dep.Kind = "skill"
		}
		dep.Source = maclawAppNormalizeDependencySourceForEntry(dep, defaultSource)
		if dep.InstallRef == "" && strings.EqualFold(dep.Source, "skillmarket") {
			if resolved, ok := maclawAppImplicitHubSkillResolution(dep); ok {
				dep.InstallRef = resolved.Target
				if dep.CanonicalID == "" {
					dep.CanonicalID = resolved.Target
				}
			}
		}
		key := strings.ToLower(dep.ID)
		if idx, ok := seen[key]; ok {
			if dep.Required && !deps[idx].Required {
				deps[idx].Required = true
			}
			if deps[idx].Version == "" {
				deps[idx].Version = dep.Version
			}
			if deps[idx].Kind == "skill" && dep.Kind != "" {
				deps[idx].Kind = dep.Kind
			}
			if dep.Source != "" && (deps[idx].Source == "" || deps[idx].Source == "hub") {
				deps[idx].Source = dep.Source
			}
			if deps[idx].InstallRef == "" {
				deps[idx].InstallRef = dep.InstallRef
			}
			if deps[idx].CanonicalID == "" {
				deps[idx].CanonicalID = dep.CanonicalID
			}
			deps[idx].Aliases = appendMaclawAppUniqueStrings(deps[idx].Aliases, dep.Aliases...)
			return
		}
		seen[key] = len(deps)
		deps = append(deps, dep)
	}
	for _, holder := range []map[string]any{anyMap(entry.App["binding"]), entry.App} {
		if holder == nil {
			continue
		}
		if skill := anyMap(holder["skill"]); skill != nil {
			add(maclawAppInstallPlanDependency{
				ID:          stringMapValue(skill, "id"),
				Version:     stringMapValue(skill, "version"),
				Kind:        "runtime_skill",
				Required:    true,
				Source:      stringMapValue(skill, "source"),
				InstallRef:  maclawAppDependencyInstallRef(skill),
				CanonicalID: maclawAppDependencyCanonicalID(skill),
				Aliases:     maclawAppDependencyAliases(skill),
			})
		}
		for _, appSkill := range []map[string]any{anyMap(holder["appSkill"]), anyMap(holder["app_skill"])} {
			if appSkill == nil {
				continue
			}
			add(maclawAppInstallPlanDependency{
				ID:          stringMapValue(appSkill, "id"),
				Version:     stringMapValue(appSkill, "version"),
				Kind:        "app_skill",
				Required:    true,
				Source:      stringMapValue(appSkill, "source"),
				InstallRef:  maclawAppDependencyInstallRef(appSkill),
				CanonicalID: maclawAppDependencyCanonicalID(appSkill),
				Aliases:     maclawAppDependencyAliases(appSkill),
			})
		}
		if misBlock := anyMap(holder["mis"]); misBlock != nil {
			bindings := anySlice(misBlock["approvalBindings"])
			if len(bindings) == 0 {
				bindings = anySlice(misBlock["approval_bindings"])
			}
			for _, item := range bindings {
				bindingMap := anyMap(item)
				if bindingMap == nil {
					continue
				}
				add(maclawAppInstallPlanDependency{
					ID:          firstNonEmptyMISAgentView(stringMapValue(bindingMap, "workflowSkillId"), stringMapValue(bindingMap, "workflow_skill_id"), stringMapValue(bindingMap, "workflowId"), stringMapValue(bindingMap, "workflow_id")),
					Version:     firstNonEmptyMISAgentView(stringMapValue(bindingMap, "workflowVersion"), stringMapValue(bindingMap, "workflow_version")),
					Kind:        "workflow_skill",
					Required:    true,
					Source:      "hub",
					InstallRef:  maclawAppDependencyInstallRef(bindingMap),
					CanonicalID: maclawAppDependencyCanonicalID(bindingMap),
					Aliases:     maclawAppDependencyAliases(bindingMap),
				})
			}
		}
		if depsBlock := anyMap(holder["dependencies"]); depsBlock != nil {
			for _, depMap := range append(maclawAppDependencyMaps(depsBlock["skills"]), maclawAppDependencyMaps(depsBlock["skill"])...) {
				required := true
				if rawRequired, ok := depMap["required"].(bool); ok {
					required = rawRequired
				}
				add(maclawAppInstallPlanDependency{
					ID:          stringMapValue(depMap, "id"),
					Version:     stringMapValue(depMap, "version"),
					Kind:        stringMapValue(depMap, "kind"),
					Required:    required,
					Source:      stringMapValue(depMap, "source"),
					InstallRef:  maclawAppDependencyInstallRef(depMap),
					CanonicalID: maclawAppDependencyCanonicalID(depMap),
					Aliases:     maclawAppDependencyAliases(depMap),
				})
			}
		}
	}
	return deps
}

func maclawAppGovernanceReviewIssuesFromPackage(pkg map[string]any) []maclawAppReviewIssue {
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, true)
	if err != nil {
		return nil
	}
	issues := []maclawAppReviewIssue{}
	for i, entry := range entries {
		path := fmt.Sprintf("apps[%d].app", i)
		governance := anyMap(entry.App["governance"])
		if governance == nil {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".governance", Severity: "warning", Message: "missing governance metadata", Suggestion: "include dependency, workspace layout, and test evidence metadata before publishing"})
		}
		if !maclawAppHasPublishableTestEvidence(governance) {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".governance.testEvidence", Severity: "error", Message: "missing successful local run evidence", Suggestion: "run the app once in App Studio before submitting to the capability market"})
		}
		if !maclawAppHasPublishableWorkspaceLayout(entry.App, governance, normalizeMaclawAppKind(entry.Kind)) {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".governance.workspaceLayout", Severity: "error", Message: "missing workspace layout evidence", Suggestion: "save the generated UI layout in the app manifest before publishing"})
		}
		if issue := maclawAppWorkspaceLayoutReviewIssue(entry, governance, path); issue != nil {
			issues = append(issues, *issue)
		}
		if !maclawAppHasPublishableResultContract(governance) {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".governance.resultContract", Severity: "error", Message: "missing result contract", Suggestion: "declare the app output contract before submitting to the capability market"})
		}
		if issue := maclawAppTestProtocolReviewIssue(governance, path); issue != nil {
			issues = append(issues, *issue)
		}
		if issue := maclawAppApprovalInstanceTestEvidenceReviewIssue(entry, governance, path); issue != nil {
			issues = append(issues, *issue)
		}
		if issue := maclawAppDependencyVerificationReviewIssue(entry, governance, path); issue != nil {
			issues = append(issues, *issue)
		}
		if maclawAppHasPublishableTestEvidence(governance) {
			if issue := maclawAppDefinitionHashReviewIssue(entry, governance, path); issue != nil {
				issues = append(issues, *issue)
			}
			if issue := maclawAppWorkspaceLayoutEvidenceReviewIssue(entry, governance, path); issue != nil {
				issues = append(issues, *issue)
			}
		}
		if maclawAppHasPublishableTestEvidence(governance) && maclawAppHasPublishableResultContract(governance) {
			if issue := maclawAppResultCoverageReviewIssue(governance, path); issue != nil {
				issues = append(issues, *issue)
			}
		}
		if normalizeMaclawAppKind(entry.Kind) == "enterprise_approval_app" && maclawAppWorkflowMappingForEntry(entry) == nil {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".binding.workflow", Severity: "error", Message: "missing workflow node mapping", Suggestion: "save the approval workflow node mapping in App Studio before submitting to the capability market"})
		}
		if issue := maclawAppWorkflowContractReviewIssue(entry, governance, path); issue != nil {
			issues = append(issues, *issue)
		}
	}
	return normalizeMaclawAppReviewIssues(issues)
}

func maclawAppBlockingInstallGovernanceReviewIssues(doc map[string]any) []maclawAppReviewIssue {
	if doc == nil {
		return nil
	}
	var reviewDoc map[string]any
	switch strings.TrimSpace(stringMapValue(doc, "schema")) {
	case "maclaw.app.pack.v1":
		reviewDoc = doc
	case "maclaw.app.v1":
		app := anyMap(doc["app"])
		if anyMap(app["governance"]) == nil {
			return nil
		}
		reviewDoc = map[string]any{
			"schema":        "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"apps":          []any{doc},
		}
	default:
		return nil
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(reviewDoc, true)
	if err != nil {
		return nil
	}
	hasGovernance := false
	for _, entry := range entries {
		if anyMap(entry.App["governance"]) != nil {
			hasGovernance = true
			break
		}
	}
	if !hasGovernance {
		return nil
	}
	issues := maclawAppGovernanceReviewIssuesFromPackage(reviewDoc)
	blocking := make([]maclawAppReviewIssue, 0, len(issues))
	for _, issue := range issues {
		if strings.EqualFold(strings.TrimSpace(issue.Severity), "error") {
			blocking = append(blocking, issue)
		}
	}
	return blocking
}

func maclawAppHasPublishableResultContract(governance map[string]any) bool {
	if governance == nil {
		return false
	}
	contract := anyMap(governance["resultContract"])
	if contract == nil {
		contract = anyMap(governance["result_contract"])
	}
	if contract == nil {
		return false
	}
	if strings.TrimSpace(maclawAppStringValue(contract, "schema")) != "maclaw.app.result.v1" {
		return false
	}
	if strings.TrimSpace(maclawAppStringValue(contract, "primary")) == "" {
		return false
	}
	return len(maclawAppStringListFromAny(contract["types"])) > 0
}

func maclawAppHasPublishableTestEvidence(governance map[string]any) bool {
	if governance == nil {
		return false
	}
	testEvidence := anyMap(governance["testEvidence"])
	if testEvidence == nil {
		testEvidence = anyMap(governance["test_evidence"])
	}
	if testEvidence == nil {
		return false
	}
	if value, ok := testEvidence["artifactPresent"].(bool); ok && value {
		return true
	}
	if value, ok := testEvidence["artifact_present"].(bool); ok && value {
		return true
	}
	if count, ok := maclawAppNumberFromAny(testEvidence["artifactCount"]); ok && count > 0 {
		return true
	}
	if count, ok := maclawAppNumberFromAny(testEvidence["artifact_count"]); ok && count > 0 {
		return true
	}
	return strings.TrimSpace(maclawAppStringValue(testEvidence, "runId", "run_id", "definitionHash", "definition_hash", "verifiedAt", "verified_at")) != ""
}

func maclawAppTestProtocolReviewIssue(governance map[string]any, appPath string) *maclawAppReviewIssue {
	if governance == nil || !maclawAppHasPublishableTestEvidence(governance) {
		return nil
	}
	testEvidence := maclawAppTestEvidenceMap(governance)
	protocol := maclawAppTestProtocolMap(governance, testEvidence)
	if protocol == nil {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testProtocol", Severity: "error", Message: "missing test protocol", Suggestion: "save the App Studio test protocol with sample input, expected output, roles, scopes, and risk before submitting"}
	}
	if schema := strings.TrimSpace(maclawAppStringValue(protocol, "schema")); schema != "" && schema != "maclaw.app.test_protocol.v1" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testProtocol", Severity: "error", Message: "invalid test protocol schema", Suggestion: "set testProtocol.schema to maclaw.app.test_protocol.v1"}
	}
	if _, ok := protocol["sampleInput"]; !ok {
		if _, ok := protocol["sample_input"]; !ok {
			return &maclawAppReviewIssue{Path: appPath + ".governance.testProtocol.sampleInput", Severity: "error", Message: "test protocol is missing sample input", Suggestion: "include the App Studio test sample input used by the local run"}
		}
	}
	if !maclawAppTestProtocolHasExpectedOutput(protocol) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testProtocol.expectedOutput", Severity: "error", Message: "test protocol is missing expected output", Suggestion: "include expected_output or expectedOutput so the local run can be reproduced"}
	}
	fingerprint := strings.TrimSpace(maclawAppStringValue(testEvidence, "testProtocolFingerprint", "test_protocol_fingerprint", "testProtocolHash", "test_protocol_hash", "protocolFingerprint", "protocol_fingerprint", "protocolHash", "protocol_hash"))
	if fingerprint == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.testProtocolFingerprint", Severity: "error", Message: "run evidence is not linked to a test protocol fingerprint", Suggestion: "store the test protocol fingerprint produced by the App Studio test run"}
	}
	protocolFingerprint := strings.TrimSpace(maclawAppStringValue(protocol, "fingerprint", "hash", "testProtocolFingerprint", "test_protocol_fingerprint", "protocolFingerprint", "protocol_fingerprint"))
	// The evidence fingerprint must match either the recomputed protocol
	// fingerprint (proof the evidence ran this exact protocol) or the declared
	// stamp carried inside the protocol (opaque stamps from external tooling
	// that this recompute cannot reproduce). maclawAppTestProtocolFingerprint
	// strips the stamp keys before hashing, so a deleted stamp can no longer
	// short-circuit the check, and a stamp that matches neither value means
	// the protocol was edited after the run.
	recomputed := maclawAppTestProtocolFingerprint(protocol)
	if recomputed != "" && fingerprint != recomputed && fingerprint != protocolFingerprint {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.testProtocolFingerprint", Severity: "error", Message: "run evidence test protocol fingerprint does not match the current test protocol", Suggestion: "rerun the app test after editing the test protocol"}
	}
	return nil
}

func maclawAppTestEvidenceMap(governance map[string]any) map[string]any {
	if governance == nil {
		return nil
	}
	testEvidence := anyMap(governance["testEvidence"])
	if testEvidence == nil {
		testEvidence = anyMap(governance["test_evidence"])
	}
	return testEvidence
}

func firstMaclawAppReviewIssueMessage(issues []maclawAppReviewIssue, fallback string) string {
	for _, issue := range issues {
		if msg := strings.TrimSpace(issue.Message); msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(fallback)
}

func maclawAppDefinitionHashReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	if governance == nil || !maclawAppHasPublishableTestEvidence(governance) {
		return nil
	}
	testEvidence := anyMap(governance["testEvidence"])
	if testEvidence == nil {
		testEvidence = anyMap(governance["test_evidence"])
	}
	if testEvidence == nil {
		return nil
	}
	declared := strings.TrimSpace(maclawAppStringValue(testEvidence, "definitionHash", "definition_hash", "definitionFingerprint", "definition_fingerprint"))
	if declared == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.definitionHash", Severity: "error", Message: "run evidence is missing the current app definition hash", Suggestion: "run the current app definition again before submitting to the capability market"}
	}
	computed := maclawAppDefinitionFingerprintForEntry(entry)
	if computed == "" || declared == computed {
		return nil
	}
	return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.definitionHash", Severity: "error", Message: "run evidence definition hash does not match current app definition", Suggestion: "run the current app definition again before submitting to the capability market"}
}

func maclawAppDefinitionFingerprintForEntry(entry parsedMaclawAppEntry) string {
	payload := maclawAppDefinitionFingerprintPayloadForEntry(entry)
	if payload == nil {
		return ""
	}
	encoded, err := maclawAppStableJSON(payload)
	if err != nil {
		return ""
	}
	return maclawAppFNV1aTextHash(encoded)
}

func maclawAppDefinitionFingerprintPayloadForEntry(entry parsedMaclawAppEntry) map[string]any {
	app := entry.App
	if app == nil {
		return nil
	}
	binding := anyMap(app["binding"])
	runtimeManifest := map[string]any{
		"schema":        entry.Schema,
		"installUnit":   stringMapValue(entry.Entry, "installUnit"),
		"privateMarker": stringMapValue(entry.Entry, "privateMarker"),
		"entryKind":     stringMapValue(entry.Entry, "entryKind"),
		"launchMode":    firstNonEmptyMaclawAppString(maclawAppStringValue(app, "launchMode", "launch_mode"), stringMapValue(entry.Entry, "launchMode")),
	}
	if binding != nil {
		for _, pair := range []struct {
			out  string
			keys []string
		}{
			{"datasrv", []string{"datasrv"}},
			{"mis", []string{"mis"}},
			{"skill", []string{"skill"}},
			{"appSkill", []string{"appSkill", "app_skill"}},
			{"dependencies", []string{"dependencies"}},
			{"ui", []string{"ui"}},
			{"resultContract", []string{"resultContract", "result_contract"}},
			{"testProtocol", []string{"testProtocol", "test_protocol"}},
			{"workflow", []string{"workflow"}},
		} {
			if pair.out == "ui" {
				if value := entry.App["ui"]; value != nil {
					runtimeManifest[pair.out] = value
					continue
				}
			}
			for _, key := range pair.keys {
				if value := binding[key]; value != nil {
					runtimeManifest[pair.out] = value
					break
				}
			}
		}
	}
	payload := map[string]any{
		"name":        maclawAppStringValue(app, "name"),
		"description": maclawAppStringValue(app, "description"),
		"category":    maclawAppStringValue(app, "category"),
		"kind":        normalizeMaclawAppKind(maclawAppStringValue(app, "kind")),
		"icon":        maclawAppStringValue(app, "icon"),
		"version":     maclawAppNormalizedVersionAny(app["version"]),
		"manifest":    compactPayload(runtimeManifest),
	}
	if icon := strings.TrimSpace(maclawAppStringValue(app, "customIconDataUrl", "custom_icon_data_url")); icon != "" {
		payload["customIconDataUrl"] = icon
	}
	return payload
}

func maclawAppWorkspaceLayoutEvidenceReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	if governance == nil || !maclawAppHasPublishableTestEvidence(governance) {
		return nil
	}
	testEvidence := maclawAppTestEvidenceMap(governance)
	if testEvidence == nil {
		return nil
	}
	declared := strings.TrimSpace(maclawAppStringValue(testEvidence, "workspaceLayoutFingerprint", "workspace_layout_fingerprint", "workspaceLayoutHash", "workspace_layout_hash", "layoutFingerprint", "layout_fingerprint", "layoutHash", "layout_hash"))
	if declared == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.workspaceLayoutFingerprint", Severity: "error", Message: "run evidence is missing the current workspace layout fingerprint", Suggestion: "rerun the app test after saving the App Studio workspace layout"}
	}
	computed := maclawAppCurrentWorkspaceLayoutFingerprint(entry, governance)
	if computed == "" || declared == computed {
		return nil
	}
	return &maclawAppReviewIssue{
		Path:       appPath + ".governance.testEvidence.workspaceLayoutFingerprint",
		Severity:   "error",
		Message:    "run evidence workspace layout fingerprint does not match the current workspace layout",
		Suggestion: "rerun the app test after editing or saving the workspace layout",
		Metadata: map[string]any{
			"declared": declared,
			"computed": computed,
		},
	}
}

func maclawAppCurrentWorkspaceLayoutFingerprint(entry parsedMaclawAppEntry, governance map[string]any) string {
	var entryName string
	if governanceLayout := anyMap(firstNonEmptyMaclawAppAny(governance["workspaceLayout"], governance["workspace_layout"])); governanceLayout != nil {
		entryName = strings.TrimSpace(maclawAppStringValue(governanceLayout, "entry"))
		if entryName == "" {
			entryName = maclawAppWorkspaceLayoutEntryName(entry.App)
		}
		if entryName != "" {
			return firstNonEmptyMaclawAppString(maclawAppWorkspaceLayoutFingerprint(entryName, governanceLayout), maclawAppStringValue(governanceLayout, "fingerprint"))
		}
	}
	entryName = maclawAppWorkspaceLayoutEntryName(entry.App)
	if entryName == "" {
		return ""
	}
	for _, source := range maclawAppWorkspaceUILayoutSources(entry.App, entryName, "") {
		if source.layout == nil {
			continue
		}
		if fingerprint := firstNonEmptyMaclawAppString(maclawAppWorkspaceLayoutFingerprint(entryName, source.layout), maclawAppStringValue(source.layout, "fingerprint")); fingerprint != "" {
			return fingerprint
		}
	}
	return ""
}

func maclawAppStableJSON(value any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func maclawAppResultCoverageReviewIssue(governance map[string]any, appPath string) *maclawAppReviewIssue {
	contract := anyMap(governance["resultContract"])
	if contract == nil {
		contract = anyMap(governance["result_contract"])
	}
	testEvidence := anyMap(governance["testEvidence"])
	if testEvidence == nil {
		testEvidence = anyMap(governance["test_evidence"])
	}
	if contract == nil || testEvidence == nil {
		return nil
	}
	primary := strings.TrimSpace(maclawAppStringValue(contract, "primary"))
	if primary == "" {
		return nil
	}
	coverage := anyMap(testEvidence["resultCoverage"])
	if coverage == nil {
		coverage = anyMap(testEvidence["result_coverage"])
	}
	if coverage != nil {
		missing := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(coverage["missingTypes"], coverage["missing_types"]))
		if len(missing) > 0 {
			return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.resultCoverage", Severity: "error", Message: "run evidence does not cover result contract: " + strings.Join(missing, ", "), Suggestion: "run the app again and verify every declared required result type is present in the result payload or outputs"}
		}
		if ok, _ := coverage["ok"].(bool); ok && maclawAppCoveredResultTypesContain(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(coverage["coveredTypes"], coverage["covered_types"])), primary) {
			return nil
		}
		if len(missing) == 0 {
			missing = []string{primary}
		}
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.resultCoverage", Severity: "error", Message: "run evidence does not cover result contract: " + strings.Join(missing, ", "), Suggestion: "run the app again and verify the declared primary result is present in the result payload or outputs"}
	}
	covered := maclawAppCoveredResultTypesFromTestEvidence(testEvidence)
	if maclawAppCoveredResultTypesContain(covered, primary) {
		return nil
	}
	return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.resultCoverage", Severity: "error", Message: "run evidence does not cover result contract: " + primary, Suggestion: "run the app again and verify the declared primary result is present in the result payload or outputs"}
}

func maclawAppCoveredResultTypesFromTestEvidence(testEvidence map[string]any) []string {
	covered := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				covered[value] = true
			}
		}
	}
	if maclawAppBoolValue(testEvidence, "artifactPresent", "artifact_present") {
		add("artifact", "document")
	}
	if count, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(testEvidence["artifactCount"], testEvidence["artifact_count"])); ok && count > 0 {
		add("artifact", "document")
	}
	if strings.TrimSpace(maclawAppStringValue(testEvidence, "artifactName", "artifact_name", "artifactPath", "artifact_path")) != "" {
		add("artifact", "document")
	}
	payload := anyMap(firstNonEmptyMaclawAppAny(testEvidence["resultPayload"], testEvidence["result_payload"]))
	if payload != nil {
		if strings.TrimSpace(maclawAppStringValue(payload, "approval_result", "approvalResult", "approval_status", "approvalStatus", "approval_decision", "approvalDecision", "decision")) != "" {
			add("approval_result")
		}
		if strings.TrimSpace(maclawAppStringValue(payload, "business_status", "businessStatus", "result_status", "resultStatus", "status")) != "" {
			add("business_status")
		}
		if firstNonEmptyMaclawAppAny(payload["business_record"], payload["businessRecord"], payload["record"], payload["record_id"], payload["recordID"], payload["business_record_id"], payload["businessRecordID"]) != nil {
			add("business_record")
		}
		if strings.TrimSpace(maclawAppStringValue(payload, "text", "content", "message", "result", "summary")) != "" {
			add("content", "text")
		}
		if _, ok := firstNonEmptyMaclawAppAny(payload["rows"], payload["records"], payload["items"]).([]any); ok {
			add("table")
		}
		if _, ok := firstNonEmptyMaclawAppAny(payload["cards"], payload["widgets"], payload["charts"]).([]any); ok {
			add("dashboard")
		}
	}
	for _, item := range anySlice(firstNonEmptyMaclawAppAny(testEvidence["outputs"], testEvidence["output_blocks"])) {
		output := anyMap(item)
		kind := strings.ToLower(strings.TrimSpace(maclawAppStringValue(output, "kind", "type")))
		switch {
		case strings.Contains(kind, "artifact") || strings.Contains(kind, "document") || strings.Contains(kind, "file"):
			add("artifact", "document")
		case strings.Contains(kind, "approval") || strings.Contains(kind, "decision"):
			add("approval_result")
		case strings.Contains(kind, "business_status") || kind == "status":
			add("business_status")
		case strings.Contains(kind, "business_record") || strings.Contains(kind, "record"):
			add("business_record")
		case strings.Contains(kind, "table"):
			add("table")
		case strings.Contains(kind, "dashboard"):
			add("dashboard")
		case strings.Contains(kind, "notification"):
			add("notification")
		case strings.Contains(kind, "receipt"):
			add("external_receipt")
		case strings.Contains(kind, "action"):
			add("action")
		case strings.Contains(kind, "requires_input"):
			add("requires_input")
		case strings.Contains(kind, "error"):
			add("error")
		}
		if strings.TrimSpace(maclawAppStringValue(output, "text", "title", "status")) != "" || output["data"] != nil {
			add("content", "text")
		}
	}
	out := make([]string, 0, len(covered))
	for value := range covered {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func maclawAppRequiredWorkspaceRegionRoles(kind string) []string {
	switch normalizeMaclawAppKind(kind) {
	case "enterprise_approval_app":
		return []string{"input", "instance_list", "output"}
	case "enterprise_normal_app":
		return []string{"input", "record_list", "output"}
	default:
		return []string{"input", "output"}
	}
}

func maclawAppWorkspaceLayoutHasRequiredRoles(layout map[string]any, kind string) bool {
	regions := anySlice(layout["regions"])
	if len(regions) == 0 {
		return true
	}
	roles := map[string]bool{}
	for _, raw := range regions {
		region := anyMap(raw)
		if region == nil || maclawAppBoolValue(region, "hidden") || maclawAppBoolValue(region, "disabled") {
			continue
		}
		if visible, ok := region["visible"].(bool); ok && !visible {
			continue
		}
		role := strings.TrimSpace(maclawAppStringValue(region, "role"))
		if role != "" {
			roles[role] = true
		}
	}
	for _, required := range maclawAppRequiredWorkspaceRegionRoles(kind) {
		if !roles[required] {
			return false
		}
	}
	return true
}

func maclawAppHasPublishableWorkspaceLayout(app map[string]any, governance map[string]any, kind string) bool {
	if governance != nil {
		workspaceLayout := anyMap(governance["workspaceLayout"])
		if workspaceLayout == nil {
			workspaceLayout = anyMap(governance["workspace_layout"])
		}
		if workspaceLayout != nil {
			entry := strings.TrimSpace(maclawAppStringValue(workspaceLayout, "entry"))
			regionCount, _ := maclawAppNumberFromAny(workspaceLayout["regionCount"])
			if regionCount <= 0 {
				regionCount, _ = maclawAppNumberFromAny(workspaceLayout["region_count"])
			}
			return entry != "" && regionCount > 0 && maclawAppWorkspaceLayoutHasRequiredRoles(workspaceLayout, kind)
		}
	}
	ui := anyMap(app["ui"])
	if ui == nil || stringMapValue(ui, "schema") != "maclaw.app.ui.v1" {
		return false
	}
	entry := strings.TrimSpace(stringMapValue(ui, "entry"))
	layouts := anyMap(ui["layouts"])
	if entry == "" || layouts == nil {
		return false
	}
	layout := anyMap(layouts[entry])
	if layout == nil {
		return false
	}
	regions := anySlice(layout["regions"])
	return len(regions) > 0 && maclawAppWorkspaceLayoutHasRequiredRoles(layout, kind)
}

func maclawAppWorkspaceLayoutReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	governanceLayout := anyMap(firstNonEmptyMaclawAppAny(governance["workspaceLayout"], governance["workspace_layout"]))
	if governanceLayout == nil {
		return nil
	}
	entryName := strings.TrimSpace(maclawAppStringValue(governanceLayout, "entry"))
	if entryName == "" {
		entryName = maclawAppWorkspaceLayoutEntryName(entry.App)
	}
	if entryName == "" {
		return nil
	}
	if issue := maclawAppWorkspaceLayoutFingerprintIssue(governanceLayout, entryName, appPath+".governance.workspaceLayout"); issue != nil {
		return issue
	}
	governanceFingerprint := strings.TrimSpace(maclawAppStringValue(governanceLayout, "fingerprint"))
	for _, source := range maclawAppWorkspaceUILayoutSources(entry.App, entryName, appPath) {
		if source.layout == nil {
			continue
		}
		if issue := maclawAppWorkspaceLayoutFingerprintIssue(source.layout, entryName, source.path); issue != nil {
			return issue
		}
		layoutFingerprint := strings.TrimSpace(maclawAppStringValue(source.layout, "fingerprint"))
		if governanceFingerprint != "" && layoutFingerprint != "" && governanceFingerprint != layoutFingerprint {
			return &maclawAppReviewIssue{
				Path:       appPath + ".governance.workspaceLayout.fingerprint",
				Severity:   "error",
				Message:    "workspace layout fingerprint does not match the saved manifest UI layout",
				Suggestion: "save the App Studio layout again so app.ui, binding.ui, and governance.workspaceLayout share the same fingerprint",
			}
		}
	}
	return nil
}

func maclawAppWorkspaceLayoutFingerprintIssue(layout map[string]any, entryName, path string) *maclawAppReviewIssue {
	declared := strings.TrimSpace(maclawAppStringValue(layout, "fingerprint"))
	if declared == "" {
		return nil
	}
	computed := maclawAppWorkspaceLayoutFingerprint(entryName, layout)
	if computed == "" || computed == declared {
		return nil
	}
	return &maclawAppReviewIssue{
		Path:       path + ".fingerprint",
		Severity:   "error",
		Message:    "workspace layout fingerprint does not match the saved layout regions",
		Suggestion: "save the App Studio layout again after moving, hiding, or reordering workspace regions",
		Metadata: map[string]any{
			"declared": declared,
			"computed": computed,
		},
	}
}

func maclawAppWorkspaceUILayoutSources(app map[string]any, entryName, appPath string) []maclawAppWorkspaceUILayoutSource {
	sources := []maclawAppWorkspaceUILayoutSource{}
	if layout := maclawAppUILayoutForEntry(anyMap(app["ui"]), entryName); layout != nil {
		sources = append(sources, maclawAppWorkspaceUILayoutSource{path: appPath + ".ui.layouts." + entryName, layout: layout})
	}
	if binding := anyMap(app["binding"]); binding != nil {
		if layout := maclawAppUILayoutForEntry(anyMap(binding["ui"]), entryName); layout != nil {
			sources = append(sources, maclawAppWorkspaceUILayoutSource{path: appPath + ".binding.ui.layouts." + entryName, layout: layout})
		}
	}
	return sources
}

func maclawAppWorkspaceLayoutEntryName(app map[string]any) string {
	if entry := strings.TrimSpace(maclawAppStringValue(anyMap(app["ui"]), "entry")); entry != "" {
		return entry
	}
	if binding := anyMap(app["binding"]); binding != nil {
		if entry := strings.TrimSpace(maclawAppStringValue(anyMap(binding["ui"]), "entry")); entry != "" {
			return entry
		}
	}
	return ""
}

func maclawAppUILayoutForEntry(ui map[string]any, entryName string) map[string]any {
	if ui == nil || entryName == "" {
		return nil
	}
	layouts := anyMap(ui["layouts"])
	return anyMap(layouts[entryName])
}

func maclawAppWorkspaceLayoutFingerprint(entryName string, layout map[string]any) string {
	return maclawappcontract.WorkspaceLayoutFingerprint(entryName, layout)
}

func maclawAppCanonicalWorkspaceLayoutRegions(rawRegions []any) []map[string]any {
	type indexedRegion struct {
		index  int
		order  int
		region map[string]any
	}
	regions := make([]indexedRegion, 0, len(rawRegions))
	for i, raw := range rawRegions {
		region := anyMap(raw)
		if region == nil {
			continue
		}
		order := i + 1
		if value, ok := maclawAppNumberFromAny(region["order"]); ok && value > 0 {
			order = int(math.Floor(value))
		}
		regions = append(regions, indexedRegion{index: i, order: order, region: region})
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].order == regions[j].order {
			return regions[i].index < regions[j].index
		}
		return regions[i].order < regions[j].order
	})
	out := make([]map[string]any, 0, len(regions))
	for i, item := range regions {
		visible := true
		if value, ok := item.region["visible"].(bool); ok {
			visible = value
		}
		order := item.order
		if order <= 0 {
			order = i + 1
		}
		out = append(out, map[string]any{
			"id":        maclawAppStringValue(item.region, "id"),
			"role":      maclawAppStringValue(item.region, "role"),
			"placement": maclawAppStringValue(item.region, "placement"),
			"visible":   visible,
			"order":     order,
		})
	}
	return out
}

// validateAppDependenciesPublished checks that all required skill dependencies
// are resolvable by receivers. A dependency is OK if ANY of:
//
//  1. package includes it in bundled_dependencies (self-contained MiniApp pack)
//  2. plan has a remote install_ref (enterprise_hub / hubcenter / etc.)
//  3. plan has a valid skill_id for by-id download
//  4. local install has HubSkillID (skill was published to Hub/SkillMarket)
//
// Called at Hub upload time (SyncMaclawAppPackageSubmissionToHub), NOT at local
// submit time.
func (a *App) validateAppDependenciesPublished(plan maclawAppInstallPlan, pkg map[string]any) error {
	bundled := maclawAppBundledDependenciesFromDoc(pkg)
	var unpublished []string
	var installed map[string]NLSkillDefinition
	installedLoaded := false
	for _, dep := range plan.Dependencies {
		if !dep.Required {
			continue
		}
		// Self-contained MiniApp pack: receivers install from embedded skill files.
		if maclawAppDependencyIsBundled(bundled, dep) {
			continue
		}
		// Remote install_ref → receiver resolves from market independently.
		// Accept when source is non-local OR install_ref_kind is a remote kind
		// (legacy packs may still have source=local after a late stamp).
		if maclawAppDependencyHasRemoteInstallRef(dep) {
			continue
		}
		if dep.SkillID != "" && cskill.IsValidSkillID(dep.SkillID) {
			continue
		}
		// Lazy-load installed index only when a dep needs HubSkillID proof.
		if !installedLoaded {
			if a != nil {
				installed = a.installedMaclawAppSkillIndex()
			}
			installedLoaded = true
		}
		// Local skill was published (HubSkillID stamped on install / MarkUploaded).
		if maclawAppDependencyHasPublishedHubSkillID(installed, dep) {
			continue
		}
		unpublished = append(unpublished, dep.ID)
	}
	if len(unpublished) == 0 {
		return nil
	}
	if len(unpublished) == 1 {
		return fmt.Errorf("cannot upload App to Hub: skill dependency %q is neither bundled in the package nor published to Hub/SkillMarket. Bundle it (installed local skill) or upload the skill first (manage_skill action=upload name=%s)", unpublished[0], unpublished[0])
	}
	return fmt.Errorf("cannot upload App to Hub: %d skill dependencies are neither bundled nor published: %s", len(unpublished), strings.Join(unpublished, ", "))
}

func maclawAppDependencyHasPublishedHubSkillID(installed map[string]NLSkillDefinition, dep maclawAppInstallPlanDependency) bool {
	if installed == nil {
		return false
	}
	for _, key := range []string{dep.ID, dep.InstalledName, dep.CanonicalID, dep.InstallRefTarget} {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if m, ok := installed[key]; ok && strings.TrimSpace(m.HubSkillID) != "" {
			return true
		}
	}
	return false
}

// maclawAppDependencyHasRemoteInstallRef reports whether the plan dependency
// already carries a receiver-resolvable remote install coordinate.
func maclawAppDependencyHasRemoteInstallRef(dep maclawAppInstallPlanDependency) bool {
	ref := strings.TrimSpace(dep.InstallRef)
	if ref == "" {
		return false
	}
	src := strings.ToLower(strings.TrimSpace(dep.Source))
	if src != "" && src != "local" {
		return true
	}
	kind := strings.ToLower(strings.TrimSpace(dep.InstallRefKind))
	switch kind {
	case "skillmarket", "market", "hub", "skillhub", "hubcenter", "enterprise_hub", "enterprise":
		return true
	}
	// install_ref that looks like a market/hub URL or scheme is remote even when
	// source/kind were left empty by an older pack.
	lower := strings.ToLower(ref)
	if strings.Contains(lower, "://") || strings.Contains(lower, "enterprise_hub:") || strings.HasPrefix(lower, "hub:") {
		return true
	}
	return false
}

// maclawAppDependencyIsBundled reports whether pkg bundled_dependencies includes
// a non-empty skill payload matching dep (MiniApp self-contained publish).
func maclawAppDependencyIsBundled(bundled maclawAppBundledDependencies, dep maclawAppInstallPlanDependency) bool {
	for _, skill := range bundled.Skills {
		if maclawAppBundledSkillCanSatisfyDependency(skill, dep) {
			return true
		}
	}
	return false
}

func maclawAppPackageEntryID(entryMap map[string]any) string {
	if entryMap == nil {
		return ""
	}
	if id := maclawAppStringValue(anyMap(entryMap["app"]), "id", "app_id", "appID"); id != "" {
		return id
	}
	return maclawAppStringValue(entryMap, "id", "app_id", "appID")
}

// stringFromMapSafe extracts a string from a map[string]interface{} entry,
// returning "" for nil, missing keys, and the literal "<nil>" from fmt.Sprint.
func stringFromMapSafe(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "<nil>" {
		return ""
	}
	return s
}

func maclawAppReviewEvidenceFromMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	for _, key := range []string{"review_evidence", "reviewEvidence", "maclaw_app_review_evidence"} {
		if evidence := anyMap(metadata[key]); len(evidence) > 0 {
			return cloneMapAny(evidence)
		}
	}
	return nil
}

func maclawAppReviewIssuesFromAny(value any) []maclawAppReviewIssue {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var issues []maclawAppReviewIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return nil
	}
	return normalizeMaclawAppReviewIssues(issues)
}

func maclawAppReadyReviewIssuesForPackage(pkg map[string]any, plan maclawAppInstallPlan) []maclawAppReviewIssue {
	reviewIssues := maclawAppGovernanceReviewIssuesFromPackage(pkg)
	reviewIssues = append(reviewIssues, maclawAppAuthoritativeDependencyReviewIssues(plan, reviewIssues)...)
	return normalizeMaclawAppReviewIssues(reviewIssues)
}

func firstBlockingMaclawAppReviewIssue(issues []maclawAppReviewIssue) *maclawAppReviewIssue {
	for i := range issues {
		severity := strings.ToLower(strings.TrimSpace(issues[i].Severity))
		if severity == "error" || severity == "critical" {
			return &issues[i]
		}
	}
	return nil
}

func normalizeMaclawAppReviewIssues(issues []maclawAppReviewIssue) []maclawAppReviewIssue {
	if len(issues) == 0 {
		return nil
	}
	normalized := make([]maclawAppReviewIssue, 0, len(issues))
	for _, issue := range issues {
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			continue
		}
		severity := strings.TrimSpace(issue.Severity)
		switch severity {
		case "", "info", "warning", "error", "critical":
		default:
			severity = "warning"
		}
		normalized = append(normalized, maclawAppReviewIssue{
			Path:       strings.TrimSpace(issue.Path),
			Severity:   severity,
			Message:    message,
			Suggestion: strings.TrimSpace(issue.Suggestion),
			Metadata:   cloneMapAny(issue.Metadata),
		})
	}
	return normalized
}

func cloneMaclawAppReviewIssues(issues []maclawAppReviewIssue) []maclawAppReviewIssue {
	if len(issues) == 0 {
		return nil
	}
	out := append([]maclawAppReviewIssue(nil), issues...)
	for i := range out {
		out[i].Metadata = cloneMapAny(out[i].Metadata)
	}
	return out
}

func maclawAppPackageFingerprint(pkg map[string]any) (string, int, error) {
	if pkg == nil {
		return "", 0, nil
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), len(data), nil
}
