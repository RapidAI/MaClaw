package maclawappcontract

import (
	"fmt"
	"strings"
)

// ValidateGUIInstallHubPackage verifies the cross-service fields that the GUI
// install entrypoint depends on when consuming a published Enterprise Hub
// MaClaw App download package.
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
	if deps := anySlice(pkg["resolved_dependencies"]); len(deps) == 0 {
		return fmt.Errorf("downloaded maclaw app package from enterprise Hub is missing resolved_dependencies")
	}
	return nil
}

func validateDependencyVerification(appID string, governance map[string]any) error {
	verification := anyMap(firstAny(governance["dependencyVerification"], governance["dependency_verification"]))
	if len(verification) == 0 {
		return fmt.Errorf("downloaded maclaw app %q is missing dependency_verification", appID)
	}
	if boolValue(firstAny(verification["blocked"], verification["has_blocking_dependency"], verification["hasBlockingDependency"], verification["has_missing_required"], verification["hasMissingRequired"])) {
		return fmt.Errorf("downloaded maclaw app %q dependency_verification has blocking dependencies", appID)
	}
	dependencies := anySlice(firstAny(verification["dependencies"], verification["skills"]))
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

func anySlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
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
