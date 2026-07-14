package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"gopkg.in/yaml.v3"
)

// buildMaclawAppSkillZipForHubCenter materializes a HubCenter skill-market package
// from an enterprise MaClaw App capability stored as DB JSON (no on-disk skill zip).
//
// Layout:
//
//	skill.yaml
//	skill_package_manifest.json
//	maclaw.app.json
func buildMaclawAppSkillZipForHubCenter(item *capability.CapabilitySummary, version *capability.VersionSummary, metadata map[string]any) (zipPath string, cleanup func(), err error) {
	noop := func() {}
	if item == nil {
		return "", noop, &maclawAppHubCenterUploadError{Code: "MACLAW_APP_PACKAGE_BUILD_FAILED", Message: "capability is nil"}
	}
	if version == nil {
		return "", noop, &maclawAppHubCenterUploadError{Code: "MACLAW_APP_MANIFEST_MISSING", Message: "capability version is nil"}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	manifestJSON := strings.TrimSpace(version.ManifestJSON)
	if manifestJSON == "" {
		return "", noop, &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_MANIFEST_MISSING",
			Message: "maclaw app capability is missing manifest_json",
		}
	}
	var appEntry map[string]any
	if err := json.Unmarshal([]byte(manifestJSON), &appEntry); err != nil {
		return "", noop, &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_MANIFEST_INVALID",
			Message: "maclaw app manifest_json is not valid JSON: " + err.Error(),
		}
	}
	if err := validateMaclawAppHubCenterEntry(appEntry); err != nil {
		return "", noop, err
	}
	// Compact JSON is enough for HubCenter storage and keeps packages smaller.
	appEntryBytes, err := json.Marshal(appEntry)
	if err != nil {
		return "", noop, fmt.Errorf("encode maclaw.app.json: %w", err)
	}

	skillName := maclawAppHubCenterSkillName(item, metadata)
	description := firstNonEmpty(
		strings.TrimSpace(item.Description),
		stringFromAny(metadata["maclaw_app_description"]),
		strings.TrimSpace(item.DisplayName),
		skillName,
	)
	appID := firstNonEmpty(stringFromAny(metadata["maclaw_app_id"]), strings.TrimSpace(item.CapabilityID), skillName)
	appName := firstNonEmpty(stringFromAny(metadata["maclaw_app_name"]), strings.TrimSpace(item.DisplayName), appID)

	skillYAML, err := buildMaclawAppHubCenterSkillYAML(skillName, description)
	if err != nil {
		return "", noop, err
	}
	packageManifest := buildMaclawAppHubCenterPackageManifest(item, version, metadata, skillName, appID, appName, description)
	manifestBytes, err := json.Marshal(packageManifest)
	if err != nil {
		return "", noop, fmt.Errorf("encode skill_package_manifest.json: %w", err)
	}

	tmp, err := os.CreateTemp("", "maclaw-app-hubcenter-*.zip")
	if err != nil {
		return "", noop, fmt.Errorf("create temp zip: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup = func() { _ = os.Remove(tmpPath) }
	fail := func(err error) (string, func(), error) {
		_ = tmp.Close()
		cleanup()
		return "", noop, err
	}

	zw := zip.NewWriter(tmp)
	write := func(name string, body []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(body)
		return err
	}
	for _, file := range []struct {
		name string
		body []byte
	}{
		{"skill.yaml", skillYAML},
		{"skill_package_manifest.json", manifestBytes},
		{"maclaw.app.json", appEntryBytes},
	} {
		if err := write(file.name, file.body); err != nil {
			_ = zw.Close()
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		return fail(fmt.Errorf("close zip: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return fail(fmt.Errorf("sync zip: %w", err))
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close temp zip: %w", err)
	}
	return tmpPath, cleanup, nil
}

func buildMaclawAppHubCenterSkillYAML(name, description string) ([]byte, error) {
	doc := map[string]any{
		"name":        name,
		"description": description,
		"triggers":    []string{name},
		"steps": []map[string]any{
			{
				"action": "craft_tool",
				"params": map[string]any{
					"instructions": "MaClaw App skill package published from enterprise Hub",
				},
			},
		},
		"maclaw_app": map[string]any{
			"entry": "maclaw.app.json",
		},
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode skill.yaml: %w", err)
	}
	return body, nil
}

func buildMaclawAppHubCenterPackageManifest(item *capability.CapabilitySummary, version *capability.VersionSummary, metadata map[string]any, skillName, appID, appName, description string) map[string]any {
	manifest := map[string]any{
		"package_kind":   "maclaw-skill-market",
		"product_kind":   "maclaw_app_skill",
		"is_maclaw_app":  true,
		"maclaw_app_count": 1,
		"maclaw_app_entry": "maclaw.app.json",
		"maclaw_app_id":    appID,
		"maclaw_app_name":  appName,
		"maclaw_app_description": description,
		"maclaw_app_definition_sha256": firstNonEmpty(
			stringFromAny(metadata["maclaw_app_definition_sha256"]),
			stringFromAny(metadata["package_sha256"]),
			strings.TrimSpace(version.PackageChecksum),
		),
		"source":                    "enterprise_hub",
		"enterprise_capability_id":  strings.TrimSpace(item.ID),
		"enterprise_capability_key": strings.TrimSpace(item.GlobalKey),
		"enterprise_version_key":    strings.TrimSpace(version.VersionKey),
		"skill_name":                skillName,
	}
	if category := stringFromAny(metadata["maclaw_app_category"]); category != "" {
		manifest["maclaw_app_category"] = category
	}
	if icon := stringFromAny(metadata["maclaw_app_icon"]); icon != "" {
		manifest["maclaw_app_icon"] = icon
	}
	if inputMode := stringFromAny(metadata["maclaw_app_input_mode"]); inputMode != "" {
		manifest["maclaw_app_input_mode"] = inputMode
	}
	if modes := stringSliceFromAny(metadata["maclaw_app_output_modes"]); len(modes) > 0 {
		manifest["maclaw_app_output_modes"] = modes
	}
	if kind := stringFromAny(metadata["maclaw_app_kind"]); kind != "" {
		manifest["maclaw_app_kind"] = kind
	}
	if evidence := anyMapFromAny(metadata["maclaw_app_test_evidence"]); evidence != nil {
		if subset := hubCenterMaclawAppTestEvidenceSubset(evidence); subset != nil {
			manifest["maclaw_app_test_evidence"] = subset
		}
	} else if evidence := anyMapFromAny(metadata["test_evidence"]); evidence != nil {
		if subset := hubCenterMaclawAppTestEvidenceSubset(evidence); subset != nil {
			manifest["maclaw_app_test_evidence"] = subset
		}
	}
	return compactMetadata(manifest)
}

func hubCenterMaclawAppTestEvidenceSubset(evidence map[string]any) map[string]any {
	if evidence == nil {
		return nil
	}
	// Emit only HubCenter-canonical snake_case keys (drop camelCase aliases).
	out := map[string]any{}
	if v := firstNonEmpty(stringFromAny(evidence["run_id"]), stringFromAny(evidence["runId"])); v != "" {
		out["run_id"] = v
	}
	if v := firstNonEmpty(stringFromAny(evidence["verified_at"]), stringFromAny(evidence["verifiedAt"])); v != "" {
		out["verified_at"] = v
	}
	if v := firstNonEmpty(
		stringFromAny(evidence["definition_fingerprint"]),
		stringFromAny(evidence["definitionFingerprint"]),
		stringFromAny(evidence["definition_hash"]),
		stringFromAny(evidence["definitionHash"]),
	); v != "" {
		out["definition_fingerprint"] = v
	}
	if v, ok := evidence["artifact_present"]; ok && v != nil {
		out["artifact_present"] = v
	} else if v, ok := evidence["artifactPresent"]; ok && v != nil {
		out["artifact_present"] = v
	}
	if v := firstNonEmpty(stringFromAny(evidence["artifact_name"]), stringFromAny(evidence["artifactName"])); v != "" {
		out["artifact_name"] = v
	}
	if v, ok := evidence["output_count"]; ok && v != nil {
		out["output_count"] = v
	} else if v, ok := evidence["outputCount"]; ok && v != nil {
		out["output_count"] = v
	}
	if v := firstNonEmpty(stringFromAny(evidence["primary_result"]), stringFromAny(evidence["primaryResult"])); v != "" {
		out["primary_result"] = v
	}
	if v, ok := evidence["result_payload"]; ok && v != nil {
		out["result_payload"] = v
	} else if v, ok := evidence["resultPayload"]; ok && v != nil {
		out["result_payload"] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var maclawAppHubCenterSkillNameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func maclawAppHubCenterSkillName(item *capability.CapabilitySummary, metadata map[string]any) string {
	raw := firstNonEmpty(
		stringFromAny(metadata["maclaw_app_id"]),
		strings.TrimSpace(item.CapabilityID),
		strings.TrimSpace(item.DisplayName),
		"maclaw-app",
	)
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, " ", "-")
	raw = maclawAppHubCenterSkillNameRe.ReplaceAllString(raw, "-")
	raw = strings.Trim(raw, "-._")
	if raw == "" {
		return "maclaw-app"
	}
	if len(raw) > 80 {
		raw = raw[:80]
		raw = strings.Trim(raw, "-._")
	}
	return strings.ToLower(raw)
}

func anyMapFromAny(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func stringSliceFromAny(v any) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(stringFromAny(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(stringFromAny(v)); s != "" {
			return []string{s}
		}
		return nil
	}
}

// prepareMaclawAppZipForHubCenterUpload loads the published app's current version
// and materializes a HubCenter skill-market zip. It does not run
// prepareSkillZipForHubCenterMarket, which would strip skill_package_manifest.json.
func prepareMaclawAppZipForHubCenterUpload(ctx context.Context, svc *capability.Service, item *capability.CapabilitySummary) (uploadZip string, cleanup func(), metadata map[string]any, err error) {
	noop := func() {}
	if item == nil {
		return "", noop, nil, &maclawAppHubCenterUploadError{Code: "MACLAW_APP_PACKAGE_BUILD_FAILED", Message: "capability is nil"}
	}
	if svc == nil {
		return "", noop, nil, &maclawAppHubCenterUploadError{Code: "MACLAW_APP_PACKAGE_BUILD_FAILED", Message: "capability service is nil"}
	}
	status := strings.TrimSpace(strings.ToLower(item.Status))
	if status != "published" {
		return "", noop, nil, &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_NOT_PUBLISHED",
			Message: "MaClaw App must be published on enterprise Hub before upload to HubCenter",
		}
	}
	metadata = mapFromRawJSON(json.RawMessage(item.MetadataJSON))
	if metadata == nil {
		metadata = map[string]any{}
	}
	versions, err := svc.ListVersions(ctx, item.ID)
	if err != nil {
		return "", noop, nil, fmt.Errorf("list capability versions: %w", err)
	}
	version := currentCapabilityVersion(versions, item.CurrentVersionKey)
	if version == nil {
		return "", noop, nil, &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_MANIFEST_MISSING",
			Message: "MaClaw App package version not found",
		}
	}
	if strings.TrimSpace(version.ManifestJSON) == "" {
		return "", noop, nil, &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_MANIFEST_MISSING",
			Message: "MaClaw App package is missing manifest_json",
		}
	}
	// Do NOT run prepareSkillZipForHubCenterMarket here: that filter intentionally
	// drops skill_package_manifest.json as a "runtime" artifact, but HubCenter
	// needs that manifest to classify the package as maclaw_app_skill.
	// The materializer already emits a clean three-file zip.
	uploadZip, cleanup, buildErr := buildMaclawAppSkillZipForHubCenter(item, version, metadata)
	if buildErr != nil {
		return "", noop, nil, buildErr
	}
	return uploadZip, cleanup, metadata, nil
}

type maclawAppHubCenterUploadError struct {
	Code    string
	Message string
}

func (e *maclawAppHubCenterUploadError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func validateMaclawAppHubCenterEntry(entry map[string]any) error {
	if entry == nil {
		return &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_MANIFEST_MISSING",
			Message: "MaClaw App package manifest is empty",
		}
	}
	// Accept either a bare app object or maclaw.app.v1 entry wrapper.
	app := anyMapFromAny(entry["app"])
	if app == nil {
		// Bare app body: id at top level.
		if strings.TrimSpace(stringFromAny(entry["id"])) != "" {
			return nil
		}
		return &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_MANIFEST_INVALID",
			Message: "MaClaw App package manifest is missing app.id",
		}
	}
	if strings.TrimSpace(stringFromAny(app["id"])) == "" {
		return &maclawAppHubCenterUploadError{
			Code:    "MACLAW_APP_MANIFEST_INVALID",
			Message: "MaClaw App package manifest is missing app.id",
		}
	}
	return nil
}

func stampCapabilityHubCenterUpload(ctx context.Context, svc *capability.Service, item *capability.CapabilitySummary, submissionID, skillID, localStatus string) {
	if svc == nil || item == nil {
		return
	}
	metadata := mapFromRawJSON(json.RawMessage(item.MetadataJSON))
	if metadata == nil {
		metadata = map[string]any{}
	}
	if strings.TrimSpace(submissionID) != "" {
		metadata["hubcenter_submission_id"] = strings.TrimSpace(submissionID)
	}
	if strings.TrimSpace(skillID) != "" {
		metadata["hubcenter_skill_id"] = strings.TrimSpace(skillID)
	}
	if strings.TrimSpace(localStatus) != "" {
		metadata["hubcenter_upload_status"] = strings.TrimSpace(localStatus)
	}
	metadata["hubcenter_uploaded_at"] = time.Now().UTC().Format(time.RFC3339)
	// Never downgrade capability status while stamping market linkage.
	status := firstNonEmpty(strings.TrimSpace(item.Status), "published")
	if _, err := svc.ReviewCapabilityVersion(ctx, item.ID, status, jsonObjectString(compactMetadata(metadata))); err != nil {
		// Best-effort stamp only; upload already succeeded at HubCenter.
		return
	}
}
