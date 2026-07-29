package maclawappcontract

import (
	"fmt"
	"strings"
)

// NormalizeGUIInstallHubPackage repairs legacy published packages so GUI install
// can consume them without hard-failing on missing package-level
// resolved_dependencies. It synthesizes resolved_dependencies from:
//  1. existing package-level resolved_dependencies (pass-through)
//  2. entry-level resolved_dependencies
//  3. governance.dependencyVerification.dependencies / skills
//
// Mutates pkg in place. Returns whether synthesis ran and human-readable notes.
func NormalizeGUIInstallHubPackage(pkg map[string]any) (synthesized bool, notes []string) {
	if len(pkg) == 0 {
		return false, nil
	}
	existing := anySlice(pkg["resolved_dependencies"])
	if len(existing) > 0 {
		return false, nil
	}

	// Prefer entry-level resolved lists first (newer Hub storage model).
	collected := make([]any, 0)
	seen := map[string]struct{}{}
	addDep := func(raw any, appID string) {
		dep := anyMap(raw)
		if len(dep) == 0 {
			return
		}
		id := strings.TrimSpace(firstString(dep["id"], dep["skill_id"], dep["skillId"], dep["name"]))
		if id == "" {
			return
		}
		// Merge key: id + install_ref so distinct refs for same skill stay distinct.
		ref := strings.TrimSpace(firstString(dep["install_ref"], dep["installRef"], dep["runtime_skill_ref"], dep["runtimeSkillRef"]))
		key := strings.ToLower(id) + "\x00" + strings.ToLower(ref)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		// Clone so we do not mutate source verification maps unexpectedly.
		out := map[string]any{}
		for k, v := range dep {
			out[k] = v
		}
		if strings.TrimSpace(stringValue(out["id"])) == "" {
			out["id"] = id
		}
		if appID != "" {
			appIDs := stringSliceFromAny(firstAny(out["app_ids"], out["appIDs"]))
			if !containsStringFold(appIDs, appID) {
				appIDs = append(appIDs, appID)
			}
			out["app_ids"] = appIDs
		}
		// install_ref is preferred for deterministic installs; fall back to id.
		if strings.TrimSpace(firstString(out["install_ref"], out["installRef"])) == "" {
			if ref != "" {
				out["install_ref"] = ref
			} else {
				out["install_ref"] = id
			}
		}
		collected = append(collected, out)
	}

	for _, raw := range anySlice(pkg["apps"]) {
		entry := anyMap(raw)
		if len(entry) == 0 {
			continue
		}
		app := anyMap(entry["app"])
		appID := strings.TrimSpace(firstString(app["id"], app["name"]))
		for _, dep := range anySlice(entry["resolved_dependencies"]) {
			addDep(dep, appID)
		}
		governance := anyMap(app["governance"])
		verification := anyMap(firstAny(governance["dependencyVerification"], governance["dependency_verification"]))
		for _, dep := range append(anySlice(verification["dependencies"]), anySlice(verification["skills"])...) {
			addDep(dep, appID)
		}
		// Some older package exports declared the executable dependency directly
		// in binding.skill / appSkill (or dependencies.skill) but did not include
		// it in dependencyVerification. Preserve that declaration as resolved
		// metadata so the GUI planner can apply its trusted Hub compatibility
		// mapping instead of treating it as an unavailable local dependency.
		for _, holder := range []map[string]any{anyMap(app["binding"]), app} {
			if holder == nil {
				continue
			}
			addDep(holder["skill"], appID)
			addDep(holder["appSkill"], appID)
			addDep(holder["app_skill"], appID)
			dependencies := anyMap(holder["dependencies"])
			for _, dep := range anySlice(dependencies["skills"]) {
				addDep(dep, appID)
			}
			if singular := anyMap(dependencies["skill"]); singular != nil {
				addDep(singular, appID)
			}
			for _, dep := range anySlice(dependencies["skill"]) {
				addDep(dep, appID)
			}
		}
	}

	if len(collected) == 0 {
		// No skill deps declared anywhere — leave empty; Validate will allow it
		// when dependency_verification lists are also empty (no-app-dep packages).
		notes = append(notes, "package has no resolved_dependencies and no synthesizable dependency details")
		return false, notes
	}

	pkg["resolved_dependencies"] = collected
	compat := anyMap(pkg["compatibility"])
	if compat == nil {
		compat = map[string]any{}
	}
	compat["resolved_dependencies_synthesized"] = true
	compat["resolved_dependencies_source"] = "dependency_verification_or_entry_declaration"
	pkg["compatibility"] = compat
	notes = append(notes, fmt.Sprintf("synthesized %d resolved_dependencies from legacy package metadata", len(collected)))
	return true, notes
}

// ValidateGUIInstallHubPackage verifies the cross-service fields that the GUI
// install entrypoint depends on when consuming a published Enterprise Hub
// MaClaw App download package.
//
// Legacy packages that omit package-level resolved_dependencies are normalized
// in place from entry-level lists or dependency_verification before the
// resolved_dependencies presence check.
func ValidateGUIInstallHubPackage(pkg map[string]any, capabilityID string) error {
	if len(pkg) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is empty")
	}
	if strings.TrimSpace(stringValue(pkg["schema"])) != "maclaw.app.pack.v1" {
		return fmt.Errorf("downloaded maclaw app package schema must be maclaw.app.pack.v1")
	}
	if source := strings.TrimSpace(stringValue(pkg["source"])); source != "" && source != "enterprise_hub" {
		return fmt.Errorf("downloaded maclaw app package source must be enterprise_hub, got %q", source)
	}
	packageSHA := strings.TrimSpace(firstString(pkg["package_sha256"], pkg["packageSha256"], pkg["package_sha"]))
	if packageSHA == "" {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing package_sha256")
	}
	signature := anyMap(pkg["package_signature"])
	if len(signature) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing package_signature")
	}
	if algorithm := strings.ToLower(strings.TrimSpace(stringValue(signature["algorithm"]))); algorithm != "ed25519" {
		return fmt.Errorf("downloaded maclaw app package signature algorithm must be ed25519, got %q", algorithm)
	}
	for _, key := range []string{"payload", "signature_base64", "public_key_base64", "public_key_fingerprint"} {
		if strings.TrimSpace(stringValue(signature[key])) == "" {
			return fmt.Errorf("downloaded maclaw app package signature is missing %s", key)
		}
	}
	if signedSHA := strings.TrimSpace(stringValue(signature["package_sha256"])); signedSHA != "" && signedSHA != packageSHA {
		return fmt.Errorf("downloaded maclaw app package signature checksum %q does not match package_sha256 %q", signedSHA, packageSHA)
	}

	packageCapabilityID := firstString(pkg["capability_id"], pkg["capabilityId"], anyMap(pkg["capability"])["id"], anyMap(pkg["capability"])["capability_id"], anyMap(pkg["capability"])["capabilityId"])
	if packageCapabilityID != "" && capabilityID != "" && packageCapabilityID != capabilityID {
		return fmt.Errorf("downloaded maclaw app package capability_id %q does not match requested capability_id %q", packageCapabilityID, capabilityID)
	}
	capability := anyMap(pkg["capability"])
	if status := strings.TrimSpace(firstString(capability["status"], capability["state"])); status != "" && status != "published" {
		return fmt.Errorf("downloaded maclaw app package capability status must be published, got %q", status)
	}
	if len(capability) > 0 && strings.TrimSpace(firstString(capability["current_version_key"], capability["currentVersionKey"])) == "" {
		return fmt.Errorf("downloaded maclaw app package capability is missing current_version_key")
	}
	if len(reviewEvidenceFromPackage(pkg)) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing package review_evidence")
	}

	apps := anySlice(pkg["apps"])
	if len(apps) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub has no apps")
	}
	for index, raw := range apps {
		entry := anyMap(raw)
		if len(entry) == 0 {
			return fmt.Errorf("downloaded maclaw app package app entry %d is invalid", index)
		}
		if schema := strings.TrimSpace(stringValue(entry["schema"])); schema != "" && schema != "maclaw.app.v1" {
			return fmt.Errorf("downloaded maclaw app entry %d schema must be maclaw.app.v1, got %q", index, schema)
		}
		app := anyMap(entry["app"])
		appID := strings.TrimSpace(firstString(app["id"], app["name"]))
		governance := anyMap(app["governance"])
		submission := anyMap(governance["submission"])
		if len(submission) == 0 {
			return fmt.Errorf("downloaded maclaw app %q is missing Hub governance submission", appID)
		}
		status := firstString(submission["status"], submission["review_status"], submission["reviewStatus"], submission["state"], anyMap(submission["review"])["status"], anyMap(submission["review"])["state"])
		if status != "published" {
			return fmt.Errorf("downloaded maclaw app %q Hub submission status must be published, got %q", appID, status)
		}
		submissionCapabilityID := firstString(submission["capability_id"], submission["capabilityId"], packageCapabilityID)
		if submissionCapabilityID == "" {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing capability_id", appID)
		}
		if capabilityID != "" && submissionCapabilityID != capabilityID {
			return fmt.Errorf("downloaded maclaw app %q Hub submission capability_id %q does not match requested capability_id %q", appID, submissionCapabilityID, capabilityID)
		}
		if strings.TrimSpace(firstString(submission["version_key"], submission["versionKey"])) == "" {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing version_key", appID)
		}
		if strings.TrimSpace(firstString(submission["package_sha256"], submission["packageSHA256"])) == "" {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing package_sha256", appID)
		}
		entrySignature := anyMap(firstAny(submission["package_signature"], submission["packageSignature"]))
		if len(entrySignature) == 0 {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing package_signature", appID)
		}
		topFingerprint := strings.TrimSpace(firstString(signature["public_key_fingerprint"], signature["key_fingerprint"], signature["fingerprint"]))
		entryFingerprint := strings.TrimSpace(firstString(entrySignature["public_key_fingerprint"], entrySignature["key_fingerprint"], entrySignature["fingerprint"]))
		if topFingerprint != "" && entryFingerprint != "" && entryFingerprint != topFingerprint {
			return fmt.Errorf("downloaded maclaw app %q Hub submission package_signature fingerprint %q does not match package fingerprint %q", appID, entryFingerprint, topFingerprint)
		}
		if len(reviewEvidenceFromSubmission(submission)) == 0 {
			return fmt.Errorf("downloaded maclaw app %q Hub submission is missing review_evidence", appID)
		}
		if err := validateDependencyVerification(appID, governance); err != nil {
			return err
		}
	}
	// Soft-repair legacy packages, then require resolved_dependencies only when
	// the package actually declares skill/workflow dependencies.
	_, _ = NormalizeGUIInstallHubPackage(pkg)
	if deps := anySlice(pkg["resolved_dependencies"]); len(deps) == 0 && packageDeclaresSkillDependencies(pkg) {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing resolved_dependencies and dependency_verification could not synthesize them")
	}
	return nil
}

func packageDeclaresSkillDependencies(pkg map[string]any) bool {
	for _, raw := range anySlice(pkg["apps"]) {
		entry := anyMap(raw)
		if len(entry) == 0 {
			continue
		}
		if len(anySlice(entry["resolved_dependencies"])) > 0 {
			return true
		}
		app := anyMap(entry["app"])
		governance := anyMap(app["governance"])
		verification := anyMap(firstAny(governance["dependencyVerification"], governance["dependency_verification"]))
		if len(anySlice(verification["dependencies"])) > 0 || len(anySlice(verification["skills"])) > 0 {
			return true
		}
		// Binding-level skill refs also count as declared dependencies.
		for _, holder := range []map[string]any{anyMap(app["binding"]), app} {
			if holder == nil {
				continue
			}
			if skill := anyMap(holder["skill"]); len(skill) > 0 && strings.TrimSpace(firstString(skill["id"], skill["name"])) != "" {
				return true
			}
			for _, key := range []string{"appSkill", "app_skill"} {
				if skill := anyMap(holder[key]); len(skill) > 0 && strings.TrimSpace(firstString(skill["id"], skill["name"])) != "" {
					return true
				}
			}
			deps := anyMap(holder["dependencies"])
			if len(anySlice(deps["skills"])) > 0 || len(anySlice(deps["skill"])) > 0 || len(anyMap(deps["skill"])) > 0 {
				return true
			}
		}
	}
	return false
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringValue(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		if text := strings.TrimSpace(stringValue(value)); text != "" {
			return []string{text}
		}
		return nil
	}
}

func containsStringFold(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

func validateDependencyVerification(appID string, governance map[string]any) error {
	verification := anyMap(firstAny(governance["dependencyVerification"], governance["dependency_verification"]))
	if len(verification) == 0 {
		return fmt.Errorf("downloaded maclaw app %q is missing dependency_verification", appID)
	}
	if boolValue(firstAny(verification["blocked"], verification["has_blocking_dependency"], verification["hasBlockingDependency"], verification["has_missing_required"], verification["hasMissingRequired"])) {
		return fmt.Errorf("downloaded maclaw app %q dependency_verification has blocking dependencies", appID)
	}
	dependencies := append(anySlice(verification["dependencies"]), anySlice(verification["skills"])...)
	if len(dependencies) == 0 {
		return fmt.Errorf("downloaded maclaw app %q dependency_verification is missing dependency details", appID)
	}
	return nil
}

func reviewEvidenceFromPackage(pkg map[string]any) map[string]any {
	if evidence := anyMap(pkg["review_evidence"]); len(evidence) > 0 {
		return evidence
	}
	return anyMap(pkg["maclaw_app_review_evidence"])
}

func reviewEvidenceFromSubmission(submission map[string]any) map[string]any {
	if evidence := anyMap(submission["review_evidence"]); len(evidence) > 0 {
		return evidence
	}
	return anyMap(submission["maclaw_app_review_evidence"])
}

func anyMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return nil
}

// anySlice normalizes common slice shapes produced by JSON decode ([]any) and
// in-memory builders ([]map[string]any) into a single []any for iteration.
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
	default:
		return nil
	}
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
				continue
			}
			return value
		}
	}
	return nil
}

func firstString(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(stringValue(value)); text != "" {
			return text
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "y":
			return true
		}
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	}
	return false
}
