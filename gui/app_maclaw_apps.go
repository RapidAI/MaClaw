package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	contract "github.com/RapidAI/CodeClaw/corelib/structureddata"
)

type maclawAppSubmissionRecord struct {
	SubmissionID    string                           `json:"submission_id"`
	HubCapabilityID string                           `json:"hub_capability_id,omitempty"`
	SubmittedAt     string                           `json:"submitted_at"`
	Status          string                           `json:"status"`
	Channel         string                           `json:"channel"`
	AppIDs          []string                         `json:"app_ids"`
	AppNames        []string                         `json:"app_names,omitempty"`
	PackageSHA      string                           `json:"package_sha256,omitempty"`
	PackageSize     int                              `json:"package_bytes,omitempty"`
	ReviewedAt      string                           `json:"reviewed_at,omitempty"`
	PublishedAt     string                           `json:"published_at,omitempty"`
	Reviewer        string                           `json:"reviewer,omitempty"`
	RiskLevel       string                           `json:"risk_level,omitempty"`
	ApprovedScopes  []string                         `json:"approved_scopes,omitempty"`
	ReviewIssues    []maclawAppReviewIssue           `json:"review_issues,omitempty"`
	Dependencies    []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	Events          []maclawAppSubmissionEvent       `json:"events,omitempty"`
	Package         map[string]any                   `json:"package"`
	Message         string                           `json:"message"`
}

type maclawAppSubmissionQueue struct {
	Schema      string                      `json:"schema"`
	UpdatedAt   string                      `json:"updated_at"`
	Submissions []maclawAppSubmissionRecord `json:"submissions"`
}

type maclawAppSubmissionSummary struct {
	SubmissionID    string                           `json:"submission_id"`
	HubCapabilityID string                           `json:"hub_capability_id,omitempty"`
	SubmittedAt     string                           `json:"submitted_at"`
	Status          string                           `json:"status"`
	Channel         string                           `json:"channel"`
	AppIDs          []string                         `json:"app_ids"`
	AppNames        []string                         `json:"app_names,omitempty"`
	PackageSHA      string                           `json:"package_sha,omitempty"`
	PackageSHA256   string                           `json:"package_sha256,omitempty"`
	PackageSize     int                              `json:"package_bytes,omitempty"`
	ReviewedAt      string                           `json:"reviewed_at,omitempty"`
	PublishedAt     string                           `json:"published_at,omitempty"`
	Reviewer        string                           `json:"reviewer,omitempty"`
	RiskLevel       string                           `json:"risk_level,omitempty"`
	ApprovedScopes  []string                         `json:"approved_scopes,omitempty"`
	ReviewIssues    []maclawAppReviewIssue           `json:"review_issues,omitempty"`
	Dependencies    []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	Evidence        map[string]any                   `json:"submission_evidence,omitempty"`
	EventCount      int                              `json:"event_count,omitempty"`
	LastEventAt     string                           `json:"last_event_at,omitempty"`
	Message         string                           `json:"message"`
}

type maclawAppReviewIssue struct {
	Path       string         `json:"path,omitempty"`
	Severity   string         `json:"severity,omitempty"`
	Message    string         `json:"message"`
	Suggestion string         `json:"suggestion,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type maclawAppSubmissionEvent struct {
	At           string `json:"at"`
	Status       string `json:"status"`
	Channel      string `json:"channel"`
	SubmissionID string `json:"submission_id"`
	Message      string `json:"message,omitempty"`
	Reviewer     string `json:"reviewer,omitempty"`
}

type maclawAppSubmissionStatusUpdate struct {
	Status          string                 `json:"status"`
	HubCapabilityID string                 `json:"hub_capability_id"`
	Channel         string                 `json:"channel"`
	Message         string                 `json:"message"`
	SubmissionID    string                 `json:"submission_id"`
	ReviewedAt      string                 `json:"reviewed_at"`
	PublishedAt     string                 `json:"published_at"`
	Reviewer        string                 `json:"reviewer"`
	RiskLevel       string                 `json:"risk_level"`
	ApprovedScopes  []string               `json:"approved_scopes"`
	ReviewIssues    []maclawAppReviewIssue `json:"review_issues"`
}

type maclawAppHubSubmissionResponse struct {
	Schema        string                         `json:"schema"`
	Status        string                         `json:"status"`
	PackageSHA256 string                         `json:"package_sha256"`
	AppCount      int                            `json:"app_count"`
	Submissions   []maclawAppHubSubmissionResult `json:"submissions"`
}

type maclawAppHubSubmissionResult struct {
	SubmissionID string `json:"submission_id"`
	CapabilityID string `json:"capability_id"`
	AppID        string `json:"app_id"`
	AppName      string `json:"app_name"`
	Status       string `json:"status"`
	VersionKey   string `json:"version_key"`
}

type maclawAppHubCapabilityDetail struct {
	ID                string `json:"id"`
	CapabilityID      string `json:"capability_id"`
	Status            string `json:"status"`
	CurrentVersionKey string `json:"current_version_key"`
	MetadataJSON      string `json:"metadata_json"`
}

type maclawAppInstallPlan struct {
	Schema                   string                           `json:"schema"`
	Apps                     []maclawAppInstallPlanApp        `json:"apps"`
	Dependencies             []maclawAppInstallPlanDependency `json:"dependencies"`
	WorkflowContractIssues   []maclawAppReviewIssue           `json:"workflow_contract_issues,omitempty"`
	GovernanceReviewIssues   []maclawAppReviewIssue           `json:"governance_review_issues,omitempty"`
	HasMissingRequired       bool                             `json:"has_missing_required"`
	HasBlockingDependency    bool                             `json:"has_blocking_dependency,omitempty"`
	HasWorkflowContractIssue bool                             `json:"has_workflow_contract_issue,omitempty"`
	HasGovernanceReviewIssue bool                             `json:"has_governance_review_issue,omitempty"`
}

type maclawAppInstallPlanApp struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Schema string `json:"schema,omitempty"`
}

type maclawAppInstallPlanDependency struct {
	ID                 string   `json:"id"`
	Version            string   `json:"version,omitempty"`
	Kind               string   `json:"kind,omitempty"`
	Required           bool     `json:"required"`
	Source             string   `json:"source,omitempty"`
	InstallRef         string   `json:"install_ref,omitempty"`
	InstallRefKind     string   `json:"install_ref_kind,omitempty"`
	InstallRefTarget   string   `json:"install_ref_target,omitempty"`
	InstallRefVersion  string   `json:"install_ref_version,omitempty"`
	InstallRefStatus   string   `json:"install_ref_status,omitempty"`
	InstallRefMessage  string   `json:"install_ref_message,omitempty"`
	InstallErrorCode   string   `json:"install_error_code,omitempty"`
	InstallErrorStage  string   `json:"install_error_stage,omitempty"`
	InstallErrorDetail string   `json:"install_error_detail,omitempty"`
	PreflightStatus    string   `json:"preflight_status,omitempty"`
	PreflightCode      string   `json:"preflight_code,omitempty"`
	PreflightStage     string   `json:"preflight_stage,omitempty"`
	PreflightMessage   string   `json:"preflight_message,omitempty"`
	PackageSHA256      string   `json:"package_sha256,omitempty"`
	PackageChecksum    string   `json:"package_checksum,omitempty"`
	PackageSignature   string   `json:"package_signature,omitempty"`
	PackageDownloadURL string   `json:"package_download_url,omitempty"`
	IntegrityStatus    string   `json:"integrity_status,omitempty"`
	IntegrityCode      string   `json:"integrity_code,omitempty"`
	IntegrityStage     string   `json:"integrity_stage,omitempty"`
	IntegrityMessage   string   `json:"integrity_message,omitempty"`
	AppIDs             []string `json:"app_ids,omitempty"`
	Installed          bool     `json:"installed"`
	InstalledName      string   `json:"installed_name,omitempty"`
	InstalledVersion   string   `json:"installed_version,omitempty"`
	RequiredVersion    string   `json:"required_version,omitempty"`
	VersionStatus      string   `json:"version_status,omitempty"`
	InstalledDir       string   `json:"installed_dir,omitempty"`
	InstalledStatus    string   `json:"installed_status,omitempty"`
	Health             string   `json:"health,omitempty"`
	Action             string   `json:"action"`
	Message            string   `json:"message,omitempty"`
}

type maclawAppInstallSkillVersionSnapshot struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Source  string `json:"source,omitempty"`
}

type maclawAppInstallApprovalBindingSnapshot struct {
	Event           string `json:"event,omitempty"`
	ObjectRole      string `json:"object_role,omitempty"`
	WorkflowSkillID string `json:"workflow_skill_id,omitempty"`
	WorkflowVersion string `json:"workflow_version,omitempty"`
}

type maclawAppInstallVersionSnapshot struct {
	AppEntryVersion  string                                    `json:"app_entry_version,omitempty"`
	AppSkill         *maclawAppInstallSkillVersionSnapshot     `json:"app_skill,omitempty"`
	WorkflowSkills   []maclawAppInstallSkillVersionSnapshot    `json:"workflow_skills,omitempty"`
	ApprovalBindings []maclawAppInstallApprovalBindingSnapshot `json:"approval_bindings,omitempty"`
}

type parsedMaclawAppEntry struct {
	Schema string
	Entry  map[string]any
	App    map[string]any
	ID     string
	Name   string
	Kind   string
}

type maclawAppDataSrvInstallationPayload struct {
	AppID            string
	RoleBindingCount int
	Body             map[string]interface{}
}

type MaclawAppBusinessOperationInput struct {
	AppID              string         `json:"app_id"`
	AppName            string         `json:"app_name,omitempty"`
	DatasetID          string         `json:"dataset_id,omitempty"`
	ObjectRole         string         `json:"object_role,omitempty"`
	BlueprintID        string         `json:"blueprint_id,omitempty"`
	BusinessEntity     string         `json:"business_entity,omitempty"`
	BusinessAction     string         `json:"business_action,omitempty"`
	BusinessNote       string         `json:"business_note,omitempty"`
	PreferredAction    string         `json:"preferred_action,omitempty"`
	PreferredView      string         `json:"preferred_view,omitempty"`
	PreferredReport    string         `json:"preferred_report,omitempty"`
	PreferredDashboard string         `json:"preferred_dashboard,omitempty"`
	Data               map[string]any `json:"data,omitempty"`
	Filter             map[string]any `json:"filter,omitempty"`
	Limit              int            `json:"limit,omitempty"`
	DryRun             bool           `json:"dry_run,omitempty"`
}

type maclawAppBusinessOperationInput = MaclawAppBusinessOperationInput

type maclawAppInstallRecord struct {
	AppID                  string                           `json:"app_id"`
	AppName                string                           `json:"app_name,omitempty"`
	Kind                   string                           `json:"kind,omitempty"`
	Source                 string                           `json:"source,omitempty"`
	InstalledAt            string                           `json:"installed_at"`
	PackageSHA             string                           `json:"package_sha256,omitempty"`
	PackageSize            int                              `json:"package_bytes,omitempty"`
	Package                map[string]any                   `json:"package,omitempty"`
	Dependencies           []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	VersionSnapshot        maclawAppInstallVersionSnapshot  `json:"version_snapshot,omitempty"`
	WorkflowContract       map[string]any                   `json:"workflow_contract,omitempty"`
	WorkspaceLayout        map[string]any                   `json:"workspace_layout,omitempty"`
	ResultContract         map[string]any                   `json:"result_contract,omitempty"`
	TestEvidence           map[string]any                   `json:"test_evidence,omitempty"`
	DependencyVerification map[string]any                   `json:"dependency_verification,omitempty"`
	DataSrvRegistration    map[string]any                   `json:"datasrv_registration,omitempty"`
	HasMissingRequired     bool                             `json:"has_missing_required"`
	HasBlockingDependency  bool                             `json:"has_blocking_dependency,omitempty"`
	Message                string                           `json:"message,omitempty"`
}

type maclawAppInstallRegistry struct {
	Schema    string                   `json:"schema"`
	UpdatedAt string                   `json:"updated_at"`
	Installs  []maclawAppInstallRecord `json:"installs"`
}

type maclawAppApprovalInstance struct {
	AppID               string                      `json:"app_id"`
	AppName             string                      `json:"app_name,omitempty"`
	BlueprintID         string                      `json:"blueprint_id,omitempty"`
	DatasetID           string                      `json:"dataset_id,omitempty"`
	ObjectRole          string                      `json:"object_role,omitempty"`
	ApprovalObjectRole  string                      `json:"approval_object_role,omitempty"`
	ApprovalEvent       string                      `json:"approval_event,omitempty"`
	ApprovalWorkflowID  string                      `json:"approval_workflow_id,omitempty"`
	InstanceID          string                      `json:"instance_id"`
	Title               string                      `json:"title"`
	Lane                string                      `json:"lane"`
	Status              string                      `json:"status"`
	CurrentNode         string                      `json:"current_node"`
	CurrentNodeIDs      []string                    `json:"current_node_ids,omitempty"`
	WorkflowNodeIDs     []string                    `json:"workflow_node_ids,omitempty"`
	Owner               string                      `json:"owner"`
	Applicant           string                      `json:"applicant,omitempty"`
	Approver            string                      `json:"approver"`
	CurrentAssignee     string                      `json:"current_assignee,omitempty"`
	CurrentAssigneeType string                      `json:"current_assignee_type,omitempty"`
	CreatedAt           string                      `json:"created_at,omitempty"`
	UpdatedAt           string                      `json:"updated_at"`
	Result              string                      `json:"result"`
	WorkflowSkillID     string                      `json:"workflow_skill_id,omitempty"`
	WorkflowVersion     string                      `json:"workflow_version,omitempty"`
	BusinessStatus      string                      `json:"business_status,omitempty"`
	ResultStatus        string                      `json:"result_status,omitempty"`
	FromStatus          string                      `json:"from_status,omitempty"`
	ToStatus            string                      `json:"to_status,omitempty"`
	WorkflowDecisionID  string                      `json:"workflow_decision_id,omitempty"`
	RecordID            string                      `json:"record_id,omitempty"`
	ApprovalID          string                      `json:"approval_id,omitempty"`
	RecordApprovalID    string                      `json:"record_approval_id,omitempty"`
	DetailURL           string                      `json:"detail_url,omitempty"`
	BusinessEntity      string                      `json:"business_entity,omitempty"`
	BusinessAction      string                      `json:"business_action,omitempty"`
	BusinessNote        string                      `json:"business_note,omitempty"`
	ResultPayload       map[string]any              `json:"result_payload,omitempty"`
	Outputs             []maclawAppApprovalOutput   `json:"outputs,omitempty"`
	Artifacts           []maclawAppApprovalArtifact `json:"artifacts,omitempty"`
	Events              []maclawAppApprovalEvent    `json:"events,omitempty"`
}

type maclawAppApprovalEvent struct {
	At       string `json:"at"`
	Node     string `json:"node,omitempty"`
	Actor    string `json:"actor,omitempty"`
	Decision string `json:"decision,omitempty"`
	Message  string `json:"message,omitempty"`
}
type maclawAppApprovalArtifact struct {
	ID            string `json:"id,omitempty"`
	URI           string `json:"uri,omitempty"`
	Name          string `json:"name,omitempty"`
	Path          string `json:"path,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	Checksum      string `json:"checksum,omitempty"`
	DownloadState string `json:"download_state,omitempty"`
	Status        string `json:"status,omitempty"`
	Presentation  string `json:"presentation,omitempty"`
}

type maclawAppApprovalOutput struct {
	Type       string                     `json:"type,omitempty"`
	Kind       string                     `json:"kind,omitempty"`
	Title      string                     `json:"title,omitempty"`
	Text       string                     `json:"text,omitempty"`
	Status     string                     `json:"status,omitempty"`
	ArtifactID string                     `json:"artifact_id,omitempty"`
	Artifact   *maclawAppApprovalArtifact `json:"artifact,omitempty"`
	Data       map[string]any             `json:"data,omitempty"`
}

type maclawAppApprovalRegistry struct {
	Schema    string                      `json:"schema"`
	UpdatedAt string                      `json:"updated_at"`
	Instances []maclawAppApprovalInstance `json:"instances"`
}

type maclawAppApprovalDataSrvSyncInput struct {
	DatasetID   string                    `json:"dataset_id"`
	ObjectRole  string                    `json:"object_role,omitempty"`
	AppID       string                    `json:"app_id,omitempty"`
	BlueprintID string                    `json:"blueprint_id,omitempty"`
	RecordID    string                    `json:"record_id"`
	ApprovalID  string                    `json:"approval_id,omitempty"`
	Instance    maclawAppApprovalInstance `json:"instance"`
}

type MaclawAppApprovalWorkflowStartInput struct {
	AppID               string         `json:"app_id"`
	AppName             string         `json:"app_name,omitempty"`
	DatasetID           string         `json:"dataset_id,omitempty"`
	ObjectRole          string         `json:"object_role,omitempty"`
	BlueprintID         string         `json:"blueprint_id,omitempty"`
	RecordID            string         `json:"record_id"`
	Title               string         `json:"title,omitempty"`
	Applicant           string         `json:"applicant,omitempty"`
	Owner               string         `json:"owner,omitempty"`
	Approver            string         `json:"approver,omitempty"`
	CurrentAssignee     string         `json:"current_assignee,omitempty"`
	CurrentAssigneeType string         `json:"current_assignee_type,omitempty"`
	ApprovalEvent       string         `json:"approval_event,omitempty"`
	WorkflowSkillID     string         `json:"workflow_skill_id,omitempty"`
	WorkflowVersion     string         `json:"workflow_version,omitempty"`
	CurrentNode         string         `json:"current_node,omitempty"`
	CurrentNodeIDs      []string       `json:"current_node_ids,omitempty"`
	WorkflowNodeIDs     []string       `json:"workflow_node_ids,omitempty"`
	BusinessStatus      string         `json:"business_status,omitempty"`
	ResultStatus        string         `json:"result_status,omitempty"`
	FromStatus          string         `json:"from_status,omitempty"`
	ToStatus            string         `json:"to_status,omitempty"`
	BusinessEntity      string         `json:"business_entity,omitempty"`
	BusinessAction      string         `json:"business_action,omitempty"`
	BusinessNote        string         `json:"business_note,omitempty"`
	FormData            map[string]any `json:"form_data,omitempty"`
	BusinessPayload     map[string]any `json:"business_payload,omitempty"`
	ResultPayload       map[string]any `json:"result_payload,omitempty"`
	RunWorkflowSkill    bool           `json:"run_workflow_skill,omitempty"`
	WorkflowRunArgs     map[string]any `json:"workflow_run_args,omitempty"`
}

type maclawAppApprovalWorkflowStartInput = MaclawAppApprovalWorkflowStartInput

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
	req, err := http.NewRequest(http.MethodGet, base+"/api/capabilities/maclaw-apps/"+url.PathEscape(capabilityID)+"/package", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download maclaw app package from enterprise Hub failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var pkg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return nil, err
	}
	trustedFingerprints := []string{}
	if fingerprint, err := verifyMaclawAppHubPackageSignature(pkg); err != nil {
		return nil, err
	} else if fingerprint != "" {
		trustedFingerprints = append(trustedFingerprints, fingerprint)
		if err := a.mergeTrustedSkillPackageKeyFingerprint(fingerprint); err != nil {
			return nil, err
		}
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, true)
	if err != nil {
		return nil, err
	}
	packageJSON, err := maclawAppStableJSON(pkg)
	if err != nil {
		return nil, err
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(pkg)
	if err != nil {
		return nil, err
	}
	appIDs := make([]string, 0, len(entries))
	appNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		appIDs = append(appIDs, entry.ID)
		appNames = append(appNames, entry.Name)
	}
	return map[string]any{
		"schema":                           "maclaw.app.hub_package_download.v1",
		"capability_id":                    capabilityID,
		"package":                          pkg,
		"package_json":                     packageJSON,
		"package_sha256":                   packageSHA,
		"package_bytes":                    packageSize,
		"app_count":                        len(entries),
		"app_ids":                          appIDs,
		"app_names":                        appNames,
		"trusted_package_key_fingerprints": trustedFingerprints,
		"downloaded_from":                  "enterprise_hub",
	}, nil
}

func verifyMaclawAppHubPackageSignature(pkg map[string]any) (string, error) {
	signatureMap := anyMap(pkg["package_signature"])
	if signatureMap == nil {
		return "", nil
	}
	algorithm := strings.ToLower(strings.TrimSpace(stringFromAny(signatureMap["algorithm"])))
	if algorithm == "" || algorithm != "ed25519" {
		return "", nil
	}
	payload := strings.TrimSpace(stringFromAny(signatureMap["payload"]))
	if payload == "" {
		return "", fmt.Errorf("maclaw app package signature payload is missing")
	}
	publicKey, err := decodeDownloadedSkillSignatureBytes(firstNonEmpty(stringFromAny(signatureMap["public_key_base64"]), stringFromAny(signatureMap["public_key"])))
	if err != nil {
		return "", fmt.Errorf("maclaw app package signature invalid public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("maclaw app package signature invalid public key length: got %d", len(publicKey))
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	declared := normalizeDownloadedSkillPublicKeyFingerprint(firstNonEmpty(stringFromAny(signatureMap["public_key_fingerprint"]), stringFromAny(signatureMap["key_fingerprint"]), stringFromAny(signatureMap["fingerprint"])))
	if declared != "" && declared != fingerprint {
		return "", fmt.Errorf("maclaw app package signature public key fingerprint mismatch: expected %s, got %s", declared, fingerprint)
	}
	if signedSHA := normalizeExpectedPackageSHA256(stringFromAny(signatureMap["package_sha256"])); signedSHA != "" {
		if packageSHA := normalizeExpectedPackageSHA256(stringFromAny(pkg["package_sha256"])); packageSHA != "" && packageSHA != signedSHA {
			return "", fmt.Errorf("maclaw app package signature checksum mismatch: expected sha256 %s, got %s", signedSHA, packageSHA)
		}
	}
	signature, err := decodeDownloadedSkillSignatureBytes(firstNonEmpty(stringFromAny(signatureMap["signature_base64"]), stringFromAny(signatureMap["signature"])))
	if err != nil {
		return "", fmt.Errorf("maclaw app package signature invalid signature bytes: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return "", fmt.Errorf("maclaw app package signature invalid signature length: got %d", len(signature))
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(payload), signature) {
		return "", fmt.Errorf("maclaw app package signature verification failed")
	}
	return fingerprint, nil
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
		return nil, fmt.Errorf("cannot install MaClaw App from Hub: required Skill dependencies are missing or unavailable")
	}
	if installPlan.HasGovernanceReviewIssue {
		return nil, fmt.Errorf("cannot install MaClaw App from Hub: package governance review failed: %s", firstMaclawAppReviewIssueMessage(installPlan.GovernanceReviewIssues, "governance review issue"))
	}
	installRecord, err := a.RecordMaclawAppInstall(packageJSON, "enterprise_hub")
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
	record.Events = append(record.Events, record.maclawAppSubmissionEvent(now))
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
		"review_issues":       cloneMaclawAppReviewIssues(record.ReviewIssues),
		"review_issue_count":  len(record.ReviewIssues),
		"message":             record.Message,
	}, nil
}

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
				if match, ok := installed[strings.ToLower(dep.ID)]; ok {
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
	maclawAppValidateDependencyInstallRefs(plan.Dependencies)
	maclawAppApplyDependencyPreflightDiagnostics(plan.Dependencies)
	a.maclawAppApplyRemoteDependencyPreflightDiagnostics(plan.Dependencies)
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
			}
			continue
		}
		if !dep.Required {
			dep.Health = "missing"
			dep.Action = "optional_missing"
			if dep.Message == "" {
				dep.Message = "optional skill dependency is missing"
			}
			continue
		}
		source, ok := maclawAppInstallSkillSource(dep.Source)
		if !ok {
			dep.Health = "missing"
			dep.Action = "blocked"
			dep.Message = fmt.Sprintf("required skill dependency source %q cannot be installed automatically", dep.Source)
			continue
		}
		installMixedSkill := func(source, id, installRef string) error {
			return a.installMixedSkillWithIntegrity(source, id, installRef, firstNonEmpty(dep.PackageSHA256, dep.PackageChecksum), dep.PackageSignature)
		}
		if a.maclawAppInstallMixedSkill != nil {
			installMixedSkill = a.maclawAppInstallMixedSkill
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
			dep.Health = "missing"
			dep.Action = "failed"
			dep.InstallErrorCode, dep.InstallErrorStage, dep.InstallErrorDetail = maclawAppClassifyDependencyInstallError(*dep, source, err)
			dep.Message = dep.InstallErrorDetail
			continue
		}
		dep.Installed = true
		dep.Action = "installed"
		dep.Message = "installed dependency skill"
	}
	installed := a.installedMaclawAppSkillIndex()
	for i := range plan.Dependencies {
		dep := &plan.Dependencies[i]
		previousAction := dep.Action
		previousMessage := dep.Message
		if match, ok := installed[strings.ToLower(dep.ID)]; ok {
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
	entries, err := parseMaclawAppInstallEntries(packageJSON)
	if err != nil {
		return nil, err
	}
	plan, err := a.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		return nil, err
	}
	if plan.HasWorkflowContractIssue && maclawAppWorkflowContractIssuesShouldPrecedeDependencyBlock(plan.WorkflowContractIssues, plan.HasMissingRequired || plan.HasBlockingDependency) {
		return nil, fmt.Errorf("cannot install MaClaw App: approval workflow contract is invalid: %s", firstMaclawAppReviewIssueMessage(plan.WorkflowContractIssues, "approval workflow contract issue"))
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		return nil, fmt.Errorf("cannot install MaClaw App: required Skill dependencies are missing or unavailable")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &doc); err != nil {
		return nil, fmt.Errorf("decode maclaw app package: %w", err)
	}
	if issues := maclawAppBlockingInstallGovernanceReviewIssues(doc); len(issues) > 0 {
		return nil, fmt.Errorf("cannot install MaClaw App: package governance review failed: %s", issues[0].Message)
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(doc)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	dataSrvRegistration := a.registerMaclawAppInstallationsToDataSrv(entries, source, packageSHA, packageSize, plan.Dependencies)
	registry, err := a.readMaclawAppInstallRegistry()
	if err != nil {
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
	if err := a.writeMaclawAppInstallRegistry(registry); err != nil {
		return nil, err
	}
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
		"install_evidence":        maclawAppInstallEvidenceByApp(entries, plan.Dependencies),
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
	if failedCount > 0 && strings.TrimSpace(stringMapValue(registration, "reason")) == "" {
		registration["reason"] = "app installation failed to register"
	}
	return registration
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
		result["reason"] = "no datasrv role bindings"
		return result
	}
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		result["reason"] = err.Error()
		return result
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
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
	if failedCount > 0 {
		result["reason"] = "one or more app installations failed to register"
	}
	return result
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

// ExecuteMaclawAppBusinessOperation runs the DataSrv/MIS binding for an
// enterprise_normal_app that has no dedicated appSkill runtime. It keeps
// credentials in the Go backend while the GUI remains a visual operation shell.
func (a *App) ExecuteMaclawAppBusinessOperation(input MaclawAppBusinessOperationInput) (map[string]any, error) {
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return nil, fmt.Errorf("load MIS data config failed: %w", err)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("MIS data service is disabled. Enable it in Settings > MIS data")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("MIS data service token is empty. Configure it in Settings > MIS data")
	}
	limit := input.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	base := map[string]any{
		"_maclaw_app":         true,
		"app_id":              strings.TrimSpace(input.AppID),
		"app_name":            strings.TrimSpace(input.AppName),
		"dataset_id":          strings.TrimSpace(input.DatasetID),
		"object_role":         strings.TrimSpace(input.ObjectRole),
		"blueprint_id":        strings.TrimSpace(input.BlueprintID),
		"business_entity":     strings.TrimSpace(input.BusinessEntity),
		"business_action":     strings.TrimSpace(input.BusinessAction),
		"business_note":       strings.TrimSpace(input.BusinessNote),
		"preferred_action":    strings.TrimSpace(input.PreferredAction),
		"preferred_view":      strings.TrimSpace(input.PreferredView),
		"preferred_report":    strings.TrimSpace(input.PreferredReport),
		"preferred_dashboard": strings.TrimSpace(input.PreferredDashboard),
	}
	for key, value := range input.Data {
		base[key] = value
	}
	actionID := strings.TrimSpace(input.PreferredAction)
	var raw []byte
	mode := ""
	target := ""
	switch {
	case actionID != "":
		mode = "business_action"
		target = actionID
		body := map[string]interface{}{"data": mapAnyToInterfaceMap(base), "dry_run": input.DryRun}
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/business-actions/"+pathEscape(actionID)+"/execute", compactPayload(body))
	case strings.TrimSpace(input.PreferredView) != "":
		mode = "business_view"
		target = strings.TrimSpace(input.PreferredView)
		body := map[string]interface{}{"q": strings.TrimSpace(input.BusinessNote), "filter": mapAnyToInterfaceMap(input.Filter), "limit": limit}
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/views/"+pathEscape(target)+"/query", compactPayload(body))
	case strings.TrimSpace(input.PreferredReport) != "":
		mode = "business_report"
		target = strings.TrimSpace(input.PreferredReport)
		body := map[string]interface{}{"filter": mapAnyToInterfaceMap(input.Filter), "limit": limit}
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/reports/"+pathEscape(target)+"/run", compactPayload(body))
	case strings.TrimSpace(input.PreferredDashboard) != "":
		mode = "business_dashboard"
		target = strings.TrimSpace(input.PreferredDashboard)
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/dashboards/"+pathEscape(target)+"/run", nil)
	default:
		return nil, fmt.Errorf("enterprise normal app has no DataSrv operation binding")
	}
	if err != nil {
		return nil, err
	}
	response := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &response); err != nil {
			response["raw"] = strings.TrimSpace(string(raw))
		}
	}
	resultStatus := firstNonEmptyMaclawAppString(stringMapValue(response, "result_status"), stringMapValue(response, "status"), "done")
	resultPayload, outputs, artifacts, primaryResult := maclawAppBusinessOperationResultPackage(input, mode, target, resultStatus, response)
	return map[string]any{
		"synced":          true,
		"mode":            mode,
		"target":          target,
		"app_id":          strings.TrimSpace(input.AppID),
		"dataset_id":      strings.TrimSpace(input.DatasetID),
		"object_role":     strings.TrimSpace(input.ObjectRole),
		"business_action": strings.TrimSpace(input.BusinessAction),
		"result_status":   resultStatus,
		"business_status": firstNonEmptyMaclawAppString(stringMapValue(response, "business_status"), resultStatus),
		"primary_result":  primaryResult,
		"result_payload":  resultPayload,
		"outputs":         outputs,
		"artifacts":       artifacts,
		"response":        response,
	}, nil
}

func maclawAppBusinessOperationResultPackage(input MaclawAppBusinessOperationInput, mode string, target string, resultStatus string, response map[string]any) (map[string]any, []map[string]any, []map[string]any, string) {
	resultPayload := cloneMapAny(anyMap(response["result_payload"]))
	if resultPayload == nil {
		resultPayload = cloneMapAny(response)
	}
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	if strings.TrimSpace(input.AppID) != "" {
		resultPayload["app_id"] = strings.TrimSpace(input.AppID)
	}
	if strings.TrimSpace(input.DatasetID) != "" {
		resultPayload["dataset_id"] = strings.TrimSpace(input.DatasetID)
	}
	if strings.TrimSpace(input.ObjectRole) != "" {
		resultPayload["object_role"] = strings.TrimSpace(input.ObjectRole)
	}
	if strings.TrimSpace(input.BusinessAction) != "" {
		resultPayload["business_action"] = strings.TrimSpace(input.BusinessAction)
	}
	if resultStatus != "" {
		resultPayload["result_status"] = resultStatus
	}

	outputs := maclawAppBusinessOperationOutputs(response, mode, target, resultStatus)
	artifacts := maclawAppBusinessOperationArtifacts(response)
	primaryResult := firstNonEmptyMaclawAppString(stringMapValue(response, "primary_result"), stringMapValue(response, "primaryResult"))
	if primaryResult == "" {
		switch mode {
		case "business_action":
			if response["record"] != nil || stringMapValue(response, "record_id") != "" {
				primaryResult = "business_record"
			} else {
				primaryResult = "business_status"
			}
		case "business_view":
			primaryResult = "records"
		case "business_report":
			primaryResult = "report"
		case "business_dashboard":
			primaryResult = "dashboard"
		default:
			primaryResult = "content"
		}
	}
	return resultPayload, outputs, artifacts, primaryResult
}

func maclawAppBusinessOperationOutputs(response map[string]any, mode string, target string, resultStatus string) []map[string]any {
	if raw := anySlice(response["outputs"]); len(raw) > 0 {
		outputs := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if output := cloneMapAny(anyMap(item)); output != nil {
				outputs = append(outputs, output)
			}
		}
		if len(outputs) > 0 {
			return outputs
		}
	}
	kind := "content"
	switch mode {
	case "business_action":
		kind = "business_record"
	case "business_view":
		kind = "table"
	case "business_report":
		kind = "report"
	case "business_dashboard":
		kind = "dashboard"
	}
	output := map[string]any{
		"kind":   kind,
		"title":  target,
		"status": resultStatus,
		"data":   cloneMapAny(response),
	}
	if text := firstNonEmptyMaclawAppString(stringMapValue(response, "text"), stringMapValue(response, "summary"), stringMapValue(response, "message")); text != "" {
		output["text"] = text
	}
	return []map[string]any{output}
}

func maclawAppBusinessOperationArtifacts(response map[string]any) []map[string]any {
	raw := anySlice(response["artifacts"])
	if len(raw) == 0 {
		return []map[string]any{}
	}
	artifacts := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if artifact := cloneMapAny(anyMap(item)); artifact != nil {
			artifacts = append(artifacts, artifact)
		}
	}
	return artifacts
}

// RecordMaclawAppApprovalInstance persists one approval instance snapshot for a
// MaClaw approval app. It is the GUI-facing approval instance cache; DataSrv or
// workflow sync can later write the same shape.
func (a *App) RecordMaclawAppApprovalInstance(instance maclawAppApprovalInstance) (maclawAppApprovalInstance, error) {
	instance = normalizeMaclawAppApprovalInstanceFields(instance)
	if instance.AppID == "" {
		return maclawAppApprovalInstance{}, fmt.Errorf("app_id is required")
	}
	if instance.InstanceID == "" {
		instance.InstanceID = "appr-" + firstMaclawAppID([]string{instance.AppID}) + "-" + shortRandomHex()
	}
	if instance.Title == "" {
		instance.Title = instance.AppID
	}
	instance.Lane = normalizeMaclawAppApprovalLane(instance.Lane)
	instance.Status = normalizeMaclawAppApprovalStatus(instance.Status)
	if instance.CurrentNode == "" {
		instance.CurrentNode = "submit"
	}
	var err error
	instance, err = a.applyMaclawAppApprovalRuntimeContract(instance)
	if err != nil {
		return maclawAppApprovalInstance{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if instance.CreatedAt == "" {
		instance.CreatedAt = now
	}
	if instance.UpdatedAt == "" {
		instance.UpdatedAt = now
	}
	if len(instance.Events) == 0 {
		instance.Events = []maclawAppApprovalEvent{{At: instance.UpdatedAt, Node: instance.CurrentNode, Actor: firstNonEmptyMaclawAppString(instance.Owner, instance.Applicant), Decision: instance.Status, Message: instance.Result}}
	}
	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		return maclawAppApprovalInstance{}, err
	}
	if registry.Schema == "" {
		registry.Schema = "maclaw.app.approvals.v1"
	}
	stored := registry.upsert(instance)
	registry.UpdatedAt = now
	if err := a.writeMaclawAppApprovalRegistry(registry); err != nil {
		return maclawAppApprovalInstance{}, err
	}
	return cloneMaclawAppApprovalInstance(stored), nil
}

func (a *App) StartMaclawAppApprovalWorkflow(input MaclawAppApprovalWorkflowStartInput) (map[string]any, error) {
	input.AppID = strings.TrimSpace(input.AppID)
	input.RecordID = strings.TrimSpace(input.RecordID)
	if input.AppID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
	if input.RecordID == "" {
		return nil, fmt.Errorf("record_id is required")
	}
	install, err := a.findMaclawAppInstallRecord(input.AppID)
	if err != nil {
		return nil, err
	}
	if install == nil {
		return nil, fmt.Errorf("installed MaClaw App %s was not found", input.AppID)
	}
	if strings.TrimSpace(install.Kind) != "" && !strings.EqualFold(strings.TrimSpace(install.Kind), "enterprise_approval_app") {
		return nil, fmt.Errorf("installed MaClaw App %s is not an enterprise approval app", input.AppID)
	}
	currentNode := firstNonEmptyMaclawAppString(input.CurrentNode, maclawAppWorkflowMappingNodeFromInstall(install, "approvalNode", "approval_node"), maclawAppWorkflowMappingNodeFromInstall(install, "submitNode", "submit_node"), "submit")
	resultPayload := cloneMapAny(input.ResultPayload)
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	if input.FormData != nil {
		resultPayload["form_data"] = cloneMapAny(input.FormData)
	}
	if input.BusinessPayload != nil {
		resultPayload["business_payload"] = cloneMapAny(input.BusinessPayload)
	}
	if _, ok := resultPayload["business_record"]; !ok {
		resultPayload["business_record"] = map[string]any{"id": input.RecordID}
	}
	if _, ok := resultPayload["text"]; !ok {
		resultPayload["text"] = firstNonEmptyMaclawAppString(input.BusinessNote, "workflow submitted")
	}
	instance := maclawAppApprovalInstance{
		AppID:               input.AppID,
		AppName:             firstNonEmptyMaclawAppString(input.AppName, install.AppName),
		BlueprintID:         strings.TrimSpace(input.BlueprintID),
		DatasetID:           strings.TrimSpace(input.DatasetID),
		ObjectRole:          strings.TrimSpace(input.ObjectRole),
		ApprovalObjectRole:  strings.TrimSpace(input.ObjectRole),
		ApprovalEvent:       strings.TrimSpace(input.ApprovalEvent),
		Title:               firstNonEmptyMaclawAppString(input.Title, install.AppName, input.AppID),
		Lane:                "my_requests",
		Status:              "pending",
		CurrentNode:         currentNode,
		CurrentNodeIDs:      append([]string(nil), firstNonEmptyMaclawAppStringList(input.CurrentNodeIDs, input.WorkflowNodeIDs)...),
		WorkflowNodeIDs:     append([]string(nil), firstNonEmptyMaclawAppStringList(input.WorkflowNodeIDs, input.CurrentNodeIDs)...),
		Owner:               firstNonEmptyMaclawAppString(input.Owner, input.Applicant),
		Applicant:           firstNonEmptyMaclawAppString(input.Applicant, input.Owner),
		Approver:            strings.TrimSpace(input.Approver),
		CurrentAssignee:     firstNonEmptyMaclawAppString(input.CurrentAssignee, input.Approver),
		CurrentAssigneeType: firstNonEmptyMaclawAppString(input.CurrentAssigneeType, "user"),
		WorkflowSkillID:     strings.TrimSpace(input.WorkflowSkillID),
		WorkflowVersion:     strings.TrimSpace(input.WorkflowVersion),
		BusinessStatus:      firstNonEmptyMaclawAppString(input.BusinessStatus, "pending"),
		ResultStatus:        firstNonEmptyMaclawAppString(input.ResultStatus, "pending"),
		FromStatus:          strings.TrimSpace(input.FromStatus),
		ToStatus:            firstNonEmptyMaclawAppString(input.ToStatus, input.BusinessStatus, "pending"),
		RecordID:            input.RecordID,
		BusinessEntity:      strings.TrimSpace(input.BusinessEntity),
		BusinessAction:      strings.TrimSpace(input.BusinessAction),
		BusinessNote:        strings.TrimSpace(input.BusinessNote),
		Result:              firstNonEmptyMaclawAppString(input.BusinessNote, "workflow submitted"),
		ResultPayload:       resultPayload,
	}
	if len(instance.CurrentNodeIDs) == 0 && currentNode != "" {
		instance.CurrentNodeIDs = []string{currentNode}
	}
	if len(instance.WorkflowNodeIDs) == 0 {
		instance.WorkflowNodeIDs = append([]string(nil), instance.CurrentNodeIDs...)
	}
	stored, err := a.RecordMaclawAppApprovalInstance(instance)
	if err != nil {
		return nil, err
	}
	syncResult, err := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: stored.DatasetID, ObjectRole: stored.ObjectRole, AppID: stored.AppID, BlueprintID: stored.BlueprintID, RecordID: stored.RecordID, Instance: stored})
	if err != nil {
		return nil, err
	}
	if approvalID := firstNonEmptyMaclawAppString(stringMapValue(syncResult, "approval_id"), stringMapValue(syncResult, "record_approval_id")); approvalID != "" {
		stored.ApprovalID = approvalID
		stored.RecordApprovalID = approvalID
		stored, _ = a.RecordMaclawAppApprovalInstance(stored)
	}
	result := map[string]any{"started": true, "instance": stored, "sync": syncResult, "workflow_skill_id": stored.WorkflowSkillID, "workflow_version": stored.WorkflowVersion, "approval_id": stored.ApprovalID}
	if input.RunWorkflowSkill {
		workflowRun, err := a.runMaclawAppApprovalWorkflowSkill(stored, input)
		if err != nil {
			return nil, err
		}
		result["workflow_run"] = workflowRun
		if instance, ok := workflowRun["instance"].(maclawAppApprovalInstance); ok {
			result["instance"] = instance
			result["approval_id"] = instance.ApprovalID
		}
	}
	return result, nil
}

func (a *App) runMaclawAppApprovalWorkflowSkill(base maclawAppApprovalInstance, input MaclawAppApprovalWorkflowStartInput) (map[string]any, error) {
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("workflow skill runner is not initialized")
	}
	workflowSkillID := strings.TrimSpace(base.WorkflowSkillID)
	if workflowSkillID == "" {
		return nil, fmt.Errorf("workflow_skill_id is required to run approval workflow skill")
	}
	runArgs := cloneMapAny(input.WorkflowRunArgs)
	if runArgs == nil {
		runArgs = map[string]any{}
	}
	runArgs["app_id"] = base.AppID
	runArgs["app_name"] = base.AppName
	runArgs["dataset_id"] = base.DatasetID
	runArgs["object_role"] = base.ObjectRole
	runArgs["blueprint_id"] = base.BlueprintID
	runArgs["record_id"] = base.RecordID
	runArgs["approval_id"] = base.ApprovalID
	runArgs["approval_instance_id"] = base.InstanceID
	runArgs["workflow_skill_id"] = base.WorkflowSkillID
	runArgs["workflow_version"] = base.WorkflowVersion
	runArgs["current_node"] = base.CurrentNode
	runArgs["current_node_ids"] = append([]string(nil), base.CurrentNodeIDs...)
	runArgs["workflow_node_ids"] = append([]string(nil), firstNonEmptyMaclawAppStringList(base.WorkflowNodeIDs, base.CurrentNodeIDs)...)
	runArgs["applicant"] = base.Applicant
	runArgs["approver"] = base.Approver
	runArgs["business_status"] = base.BusinessStatus
	runArgs["result_status"] = base.ResultStatus
	runArgs["business_payload"] = cloneMapAny(input.BusinessPayload)
	runArgs["form_data"] = cloneMapAny(input.FormData)
	runArgs["result_payload"] = cloneMapAny(base.ResultPayload)
	runArgs["instance"] = cloneMaclawAppApprovalInstance(base)
	runArgs["_skill_owner_id"] = "maclaw_app:" + base.AppID
	execResult := a.skillExecutor.executeSkillByNameDetailed(workflowSkillID, runArgs)
	if execResult.Err != nil {
		return map[string]any{"ran": false, "workflow_skill_id": workflowSkillID, "output": execResult.Output, "captured": execResult.Captured}, execResult.Err
	}
	payload, err := maclawAppWorkflowSkillPayloadFromOutput(execResult.Output, execResult.Captured)
	if err != nil {
		return nil, err
	}
	progressInstances := maclawAppApprovalProgressInstancesFromWorkflowPayload(payload, base)
	progressSyncs := make([]map[string]any, 0, len(progressInstances))
	for _, progress := range progressInstances {
		syncResult, err := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: progress.DatasetID, ObjectRole: progress.ObjectRole, AppID: progress.AppID, BlueprintID: progress.BlueprintID, RecordID: progress.RecordID, ApprovalID: firstNonEmptyMaclawAppString(progress.ApprovalID, base.ApprovalID), Instance: progress})
		if err != nil {
			return nil, err
		}
		progressSyncs = append(progressSyncs, syncResult)
	}
	instance := maclawAppApprovalInstanceFromWorkflowPayload(payload, base)
	syncResult, err := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: instance.DatasetID, ObjectRole: instance.ObjectRole, AppID: instance.AppID, BlueprintID: instance.BlueprintID, RecordID: instance.RecordID, ApprovalID: firstNonEmptyMaclawAppString(instance.ApprovalID, base.ApprovalID), Instance: instance})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ran": true, "workflow_skill_id": workflowSkillID, "output": execResult.Output, "captured": execResult.Captured, "payload": payload, "progress_instances": progressInstances, "progress_sync": progressSyncs, "instance": instance, "sync": syncResult}, nil
}

func maclawAppWorkflowSkillPayloadFromOutput(output string, captured map[string]string) (map[string]any, error) {
	if captured != nil {
		for _, key := range []string{"maclaw_app_workflow_result", "workflow_result", "approval_result", "result"} {
			if value := strings.TrimSpace(captured[key]); value != "" {
				if payload, err := decodeMaclawAppWorkflowSkillJSONPayload(value); err == nil {
					return payload, nil
				}
			}
		}
	}
	return decodeMaclawAppWorkflowSkillJSONPayload(output)
}

func decodeMaclawAppWorkflowSkillJSONPayload(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("workflow skill returned empty output")
	}
	candidates := []string{trimmed}
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			candidates = append(candidates, trimmed[start:end+1])
		}
	}
	var lastErr error
	for _, candidate := range candidates {
		var payload map[string]any
		if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
			lastErr = err
			continue
		}
		if len(payload) == 0 {
			lastErr = fmt.Errorf("workflow skill returned empty JSON object")
			continue
		}
		return payload, nil
	}
	return nil, fmt.Errorf("workflow skill output is not a JSON object: %v", lastErr)
}

func maclawAppApprovalInstanceFromWorkflowPayload(payload map[string]any, base maclawAppApprovalInstance) maclawAppApprovalInstance {
	instance := cloneMaclawAppApprovalInstance(base)
	source := payload
	if nested := anyMap(firstNonEmptyMaclawAppAny(payload["approval_instance"], payload["approvalInstance"], payload["instance"])); nested != nil {
		source = nested
	}
	instance.AppID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "app_id", "appID"), instance.AppID)
	instance.AppName = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "app_name", "appName"), instance.AppName)
	instance.BlueprintID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "blueprint_id", "blueprintID"), instance.BlueprintID)
	instance.DatasetID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "dataset_id", "datasetID"), instance.DatasetID)
	instance.ObjectRole = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "object_role", "objectRole"), instance.ObjectRole)
	instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole)
	instance.ApprovalEvent = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "approval_event", "approvalEvent", "trigger_event", "triggerEvent"), instance.ApprovalEvent)
	instance.RecordID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "record_id", "recordID"), instance.RecordID)
	instance.ApprovalID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "approval_id", "approvalID", "record_approval_id", "recordApprovalID"), instance.ApprovalID)
	instance.RecordApprovalID = firstNonEmptyMaclawAppString(instance.ApprovalID, instance.RecordApprovalID)
	instance.InstanceID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "instance_id", "instanceID", "workflow_instance_id", "workflowInstanceID", "approval_instance_id", "approvalInstanceID"), instance.InstanceID)
	instance.Status = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "status", "decision"), instance.Status)
	instance.Lane = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "lane"), instance.Lane)
	instance.CurrentNode = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "current_node", "currentNode", "workflow_node_id", "workflowNodeId", "node"), instance.CurrentNode)
	if nodes := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(source["current_node_ids"], source["currentNodeIDs"], source["workflow_node_ids"], source["workflowNodeIds"])); len(nodes) > 0 {
		instance.CurrentNodeIDs = nodes
	}
	if nodes := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(source["workflow_node_ids"], source["workflowNodeIds"], source["current_node_ids"], source["currentNodeIDs"])); len(nodes) > 0 {
		instance.WorkflowNodeIDs = nodes
	}
	if instance.CurrentNode != "" && len(instance.CurrentNodeIDs) == 0 {
		instance.CurrentNodeIDs = []string{instance.CurrentNode}
	}
	if len(instance.WorkflowNodeIDs) == 0 {
		instance.WorkflowNodeIDs = append([]string(nil), instance.CurrentNodeIDs...)
	}
	instance.CurrentAssignee = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "current_assignee", "currentAssignee", "assigned_to", "assignedTo"), instance.CurrentAssignee)
	instance.CurrentAssigneeType = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "current_assignee_type", "currentAssigneeType"), instance.CurrentAssigneeType)
	instance.WorkflowSkillID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "workflow_skill_id", "workflowSkillId"), instance.WorkflowSkillID)
	instance.WorkflowVersion = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "workflow_version", "workflowVersion"), instance.WorkflowVersion)
	instance.WorkflowDecisionID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "workflow_decision_id", "workflowDecisionId", "decision_id", "decisionID"), instance.WorkflowDecisionID)
	instance.DetailURL = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "detail_url", "detailURL"), instance.DetailURL)
	instance.BusinessStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "business_status", "businessStatus"), instance.BusinessStatus)
	instance.ResultStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "result_status", "resultStatus"), instance.ResultStatus)
	instance.FromStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "from_status", "fromStatus"), instance.FromStatus)
	instance.ToStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "to_status", "toStatus"), instance.ToStatus)
	instance.Result = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "result", "reason", "summary", "text", "progress"), instance.Result)
	if resultPayload := anyMap(firstNonEmptyMaclawAppAny(source["result_payload"], source["resultPayload"], payload["result_payload"], payload["resultPayload"])); resultPayload != nil {
		instance.ResultPayload = cloneMapAny(resultPayload)
	}
	if outputs := decodeMaclawAppApprovalOutputsFromAny(firstNonEmptyMaclawAppAny(source["outputs"], payload["outputs"])); len(outputs) > 0 {
		instance.Outputs = outputs
	}
	if artifacts := decodeMaclawAppApprovalArtifactsFromAny(firstNonEmptyMaclawAppAny(source["artifacts"], payload["artifacts"])); len(artifacts) > 0 {
		instance.Artifacts = artifacts
	}
	instance.Status = normalizeMaclawAppApprovalStatus(instance.Status)
	if instance.Status == "approved" || instance.Status == "rejected" {
		instance.Lane = "handled"
	}
	if instance.Status == "attention" {
		instance.Lane = "attention"
	}
	if instance.Lane == "" {
		instance.Lane = "my_requests"
	}
	return instance
}

func maclawAppApprovalProgressInstancesFromWorkflowPayload(payload map[string]any, base maclawAppApprovalInstance) []maclawAppApprovalInstance {
	raw := firstNonEmptyMaclawAppAny(payload["progress_instances"], payload["progressInstances"], payload["workflow_progress"], payload["workflowProgress"], payload["approval_progress"], payload["approvalProgress"])
	items := anySlice(raw)
	if len(items) == 0 {
		if item := anyMap(raw); item != nil {
			items = []any{item}
		}
	}
	if len(items) == 0 {
		return nil
	}
	instances := make([]maclawAppApprovalInstance, 0, len(items))
	current := cloneMaclawAppApprovalInstance(base)
	for _, item := range items {
		progressPayload := anyMap(item)
		if progressPayload == nil {
			continue
		}
		progress := maclawAppApprovalInstanceFromWorkflowPayload(progressPayload, current)
		progress.Status = normalizeMaclawAppApprovalStatus(firstNonEmptyMaclawAppString(progress.Status, "pending"))
		if progress.ApprovalID == "" {
			progress.ApprovalID = current.ApprovalID
			progress.RecordApprovalID = firstNonEmptyMaclawAppString(progress.RecordApprovalID, progress.ApprovalID)
		}
		instances = append(instances, progress)
		current = progress
	}
	return instances
}

func decodeMaclawAppApprovalOutputsFromAny(value any) []maclawAppApprovalOutput {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func decodeMaclawAppApprovalArtifactsFromAny(value any) []maclawAppApprovalArtifact {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalArtifact
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// SyncMaclawAppApprovalInstanceToDataSrv writes the app approval instance state
// into DataSrv's RecordApproval link. It is intentionally app-level so the GUI
// never handles DataSrv credentials directly.
func (a *App) SyncMaclawAppApprovalInstanceToDataSrv(input maclawAppApprovalDataSrvSyncInput) (map[string]any, error) {
	input.DatasetID = strings.TrimSpace(input.DatasetID)
	input.ObjectRole = strings.TrimSpace(input.ObjectRole)
	input.AppID = strings.TrimSpace(input.AppID)
	input.BlueprintID = strings.TrimSpace(input.BlueprintID)
	input.RecordID = strings.TrimSpace(input.RecordID)
	input.ApprovalID = strings.TrimSpace(input.ApprovalID)
	instance := normalizeMaclawAppApprovalInstanceFields(cloneMaclawAppApprovalInstance(input.Instance))
	if instance.AppID == "" || instance.InstanceID == "" {
		return nil, fmt.Errorf("instance app_id and instance_id are required")
	}
	instance.Status = normalizeMaclawAppApprovalStatus(instance.Status)
	input.AppID = firstNonEmptyMaclawAppString(input.AppID, instance.AppID)
	input.BlueprintID = firstNonEmptyMaclawAppString(input.BlueprintID, instance.BlueprintID)
	input.DatasetID = firstNonEmptyMaclawAppString(input.DatasetID, instance.DatasetID)
	input.ObjectRole = firstNonEmptyMaclawAppString(input.ObjectRole, instance.ObjectRole, instance.ApprovalObjectRole)
	input.RecordID = firstNonEmptyMaclawAppString(input.RecordID, instance.RecordID)
	input.ApprovalID = firstNonEmptyMaclawAppString(input.ApprovalID, instance.ApprovalID, instance.RecordApprovalID)
	instance.AppID = firstNonEmptyMaclawAppString(instance.AppID, input.AppID)
	instance.BlueprintID = firstNonEmptyMaclawAppString(instance.BlueprintID, input.BlueprintID)
	instance.DatasetID = firstNonEmptyMaclawAppString(instance.DatasetID, input.DatasetID)
	instance.ObjectRole = firstNonEmptyMaclawAppString(instance.ObjectRole, input.ObjectRole)
	instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ApprovalObjectRole, instance.ObjectRole)
	instance.RecordID = firstNonEmptyMaclawAppString(instance.RecordID, input.RecordID)
	instance.ApprovalID = firstNonEmptyMaclawAppString(instance.ApprovalID, input.ApprovalID)
	instance.RecordApprovalID = firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID)
	var runtimeErr error
	instance, runtimeErr = a.applyMaclawAppApprovalRuntimeContract(instance)
	if runtimeErr != nil {
		return nil, runtimeErr
	}
	input.AppID = firstNonEmptyMaclawAppString(input.AppID, instance.AppID)
	input.BlueprintID = firstNonEmptyMaclawAppString(input.BlueprintID, instance.BlueprintID)
	input.DatasetID = firstNonEmptyMaclawAppString(input.DatasetID, instance.DatasetID)
	input.ObjectRole = firstNonEmptyMaclawAppString(input.ObjectRole, instance.ObjectRole, instance.ApprovalObjectRole)
	input.RecordID = firstNonEmptyMaclawAppString(input.RecordID, instance.RecordID)
	input.ApprovalID = firstNonEmptyMaclawAppString(input.ApprovalID, instance.ApprovalID, instance.RecordApprovalID)
	if input.DatasetID == "" && input.ObjectRole != "" {
		cfg, err := a.GetMISDataConfig()
		if err != nil {
			return map[string]any{"synced": false, "reason": err.Error(), "object_role": input.ObjectRole}, nil
		}
		if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
			return map[string]any{"synced": false, "reason": "mis data service unavailable", "object_role": input.ObjectRole}, nil
		}
		resolvedDatasetID, err := a.resolveMISDatasetIDArg(cfg, map[string]interface{}{
			"app_id":       input.AppID,
			"blueprint_id": input.BlueprintID,
			"object_role":  input.ObjectRole,
		}, true)
		if err != nil {
			return map[string]any{"synced": false, "reason": err.Error(), "object_role": input.ObjectRole}, nil
		}
		input.DatasetID = resolvedDatasetID
	}
	if input.DatasetID == "" || input.RecordID == "" {
		return map[string]any{"synced": false, "reason": "missing dataset_id/object_role or record_id"}, nil
	}
	if instance.WorkflowSkillID == "" {
		return map[string]any{"synced": false, "reason": "missing workflow_skill_id"}, nil
	}
	if instance.Status == "attention" && input.ApprovalID != "" {
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		instance.ApprovalID = input.ApprovalID
		instance.RecordApprovalID = input.ApprovalID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
		return map[string]any{"synced": true, "action": "attention_view_only", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "reason": "attention is view-only and does not review the DataSrv approval", "business_record_sync": businessRecordSync}, nil
	}
	if input.ApprovalID != "" && instance.Status == "pending" {
		out := a.executeMISDataTool(map[string]interface{}{
			"action":                "update_record_approval_progress",
			"approval_id":           input.ApprovalID,
			"workflow_instance_id":  instance.InstanceID,
			"workflow_node_id":      instance.CurrentNode,
			"workflow_node_ids":     append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"current_assignee":      instance.CurrentAssignee,
			"current_assignee_type": instance.CurrentAssigneeType,
			"from_status":           instance.FromStatus,
			"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"workflow_decision_id":  instance.WorkflowDecisionID,
			"detail_url":            instance.DetailURL,
			"workflow_version":      instance.WorkflowVersion,
			"business_status":       firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"result_status":         firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"result_payload":        cloneMapAny(instance.ResultPayload),
			"outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
			"artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
			"progress":              instance.Result,
		})
		instance.ApprovalID = input.ApprovalID
		instance.RecordApprovalID = input.ApprovalID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		return map[string]any{"synced": true, "action": "update_record_approval_progress", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "response": out, "business_record_sync": businessRecordSync}, nil
	}
	if input.ApprovalID == "" && maclawAppApprovalStatusCanReview(instance.Status) {
		input.ApprovalID = a.findMaclawAppRecordApprovalID(input, instance)
	}
	if input.ApprovalID != "" && maclawAppApprovalStatusCanReview(instance.Status) {
		out := a.executeMISDataTool(map[string]interface{}{
			"action":                "review_record_approval",
			"approval_id":           input.ApprovalID,
			"workflow_instance_id":  instance.InstanceID,
			"decision":              instance.Status,
			"reason":                instance.Result,
			"workflow_node_id":      instance.CurrentNode,
			"workflow_node_ids":     append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"current_assignee":      instance.CurrentAssignee,
			"current_assignee_type": instance.CurrentAssigneeType,
			"from_status":           instance.FromStatus,
			"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"workflow_decision_id":  instance.WorkflowDecisionID,
			"detail_url":            instance.DetailURL,
			"workflow_version":      instance.WorkflowVersion,
			"business_status":       firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"result_status":         firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"result_payload":        cloneMapAny(instance.ResultPayload),
			"outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
			"artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
		})
		instance.ApprovalID = input.ApprovalID
		instance.RecordApprovalID = input.ApprovalID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		return map[string]any{"synced": true, "action": "review_record_approval", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "response": out, "business_record_sync": businessRecordSync}, nil
	}
	businessRecordSync := a.syncMaclawAppApprovalBusinessRecordForApproval(input.DatasetID, input.RecordID, instance, true)
	approvalKind := "approval"
	if instance.Status == "attention" {
		approvalKind = "attention"
	}
	out := a.executeMISDataTool(map[string]interface{}{
		"action":                "create_record_approval",
		"dataset_id":            input.DatasetID,
		"object_role":           input.ObjectRole,
		"app_id":                input.AppID,
		"blueprint_id":          input.BlueprintID,
		"approval_workflow_id":  firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
		"trigger_event":         firstNonEmptyMaclawAppString(instance.ApprovalEvent, input.AppID),
		"submitted_by":          firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner),
		"record_id":             input.RecordID,
		"kind":                  approvalKind,
		"summary":               instance.Title,
		"assigned_to":           instance.Approver,
		"current_assignee":      instance.CurrentAssignee,
		"current_assignee_type": instance.CurrentAssigneeType,
		"from_status":           instance.FromStatus,
		"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
		"request": compactPayload(map[string]interface{}{
			"app_id":                input.AppID,
			"blueprint_id":          input.BlueprintID,
			"dataset_id":            input.DatasetID,
			"object_role":           input.ObjectRole,
			"approval_instance_id":  instance.InstanceID,
			"approval_workflow_id":  firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
			"trigger_event":         firstNonEmptyMaclawAppString(instance.ApprovalEvent, input.AppID),
			"owner":                 instance.Owner,
			"applicant":             instance.Applicant,
			"submitted_by":          firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner),
			"assigned_to":           instance.Approver,
			"current_assignee":      instance.CurrentAssignee,
			"currentAssignee":       instance.CurrentAssignee,
			"current_assignee_type": instance.CurrentAssigneeType,
			"currentAssigneeType":   instance.CurrentAssigneeType,
			"business_entity":       instance.BusinessEntity,
			"business_action":       instance.BusinessAction,
			"business_note":         instance.BusinessNote,
			"workflow_skill_id":     instance.WorkflowSkillID,
			"workflowSkillId":       instance.WorkflowSkillID,
			"workflow_version":      instance.WorkflowVersion,
			"workflowVersion":       instance.WorkflowVersion,
			"workflow_node_id":      instance.CurrentNode,
			"workflowNodeId":        instance.CurrentNode,
			"workflow_node_ids":     append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"workflowNodeIds":       append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"workflow_decision_id":  instance.WorkflowDecisionID,
			"workflowDecisionId":    instance.WorkflowDecisionID,
			"from_status":           instance.FromStatus,
			"fromStatus":            instance.FromStatus,
			"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"toStatus":              firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"business_status":       firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"businessStatus":        firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"result_status":         firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"resultStatus":          firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"result_payload":        cloneMapAny(instance.ResultPayload),
			"resultPayload":         cloneMapAny(instance.ResultPayload),
			"outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
			"artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
			"result":                instance.Result,
			"detail_url":            instance.DetailURL,
			"detailURL":             instance.DetailURL,
		}),
		"workflow_skill_id":    instance.WorkflowSkillID,
		"workflow_version":     instance.WorkflowVersion,
		"workflow_instance_id": instance.InstanceID,
		"workflow_node_id":     instance.CurrentNode,
		"workflow_node_ids":    append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
		"workflow_decision_id": instance.WorkflowDecisionID,
		"detail_url":           instance.DetailURL,
		"business_status":      firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"result_status":        firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
		"result_payload":       cloneMapAny(instance.ResultPayload),
		"outputs":              cloneMaclawAppApprovalOutputs(instance.Outputs),
		"artifacts":            append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
	})
	approvalID := maclawAppApprovalIDFromToolResult(out)
	if approvalID != "" {
		instance.ApprovalID = approvalID
		instance.RecordApprovalID = approvalID
		instance.DatasetID = input.DatasetID
		instance.ObjectRole = input.ObjectRole
		instance.ApprovalObjectRole = input.ObjectRole
		instance.BlueprintID = input.BlueprintID
		instance.RecordID = input.RecordID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
	}
	return map[string]any{"synced": true, "action": "create_record_approval", "dataset_id": input.DatasetID, "approval_id": approvalID, "response": out, "business_record_sync": businessRecordSync}, nil
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

func maclawAppWorkflowMappingNodeFromInstall(install *maclawAppInstallRecord, keys ...string) string {
	if install == nil {
		return ""
	}
	for _, source := range []map[string]any{install.TestEvidence, install.WorkflowContract} {
		if node := maclawAppStringValue(source, keys...); node != "" {
			return node
		}
		if mapping := anyMap(source["workflow_mapping"]); mapping != nil {
			if node := maclawAppStringValue(mapping, keys...); node != "" {
				return node
			}
		}
	}
	if mapping := anyMap(install.Package["workflow_mapping"]); mapping != nil {
		if node := maclawAppStringValue(mapping, keys...); node != "" {
			return node
		}
	}
	for _, entry := range anySlice(install.Package["apps"]) {
		entryMap := anyMap(entry)
		app := anyMap(entryMap["app"])
		for _, source := range []map[string]any{anyMap(app["binding"]), anyMap(app["governance"]), app} {
			if mapping := anyMap(source["workflow"]); mapping != nil {
				if node := maclawAppStringValue(mapping, keys...); node != "" {
					return node
				}
			}
			if mapping := anyMap(source["workflow_mapping"]); mapping != nil {
				if node := maclawAppStringValue(mapping, keys...); node != "" {
					return node
				}
			}
		}
	}
	return ""
}
func (a *App) applyMaclawAppApprovalRuntimeContract(instance maclawAppApprovalInstance) (maclawAppApprovalInstance, error) {
	registry, err := a.readMaclawAppInstallRegistry()
	if err != nil {
		return instance, err
	}
	var install *maclawAppInstallRecord
	for i := range registry.Installs {
		if strings.EqualFold(strings.TrimSpace(registry.Installs[i].AppID), strings.TrimSpace(instance.AppID)) {
			install = &registry.Installs[i]
			break
		}
	}
	if install == nil {
		return instance, nil
	}
	contract := install.WorkflowContract
	snapshot := install.VersionSnapshot
	if instance.WorkflowSkillID == "" {
		instance.WorkflowSkillID = maclawAppRuntimeDefaultWorkflowSkillID(contract, snapshot)
	}
	if instance.WorkflowSkillID == "" {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: missing workflow_skill_id for installed app %s", instance.AppID)
	}
	contractWorkflowID := maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id")
	if contractWorkflowID != "" && !strings.EqualFold(contractWorkflowID, instance.WorkflowSkillID) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: workflow_skill_id %s does not match installed contract %s", instance.WorkflowSkillID, contractWorkflowID)
	}
	binding := maclawAppRuntimeApprovalBindingSnapshot(snapshot, instance)
	if binding.WorkflowSkillID != "" && !strings.EqualFold(binding.WorkflowSkillID, instance.WorkflowSkillID) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: workflow_skill_id %s does not match installed binding %s", instance.WorkflowSkillID, binding.WorkflowSkillID)
	}
	if instance.ApprovalEvent == "" {
		instance.ApprovalEvent = binding.Event
	}
	expectedObjectRole := firstNonEmptyMaclawAppString(binding.ObjectRole, maclawAppStringValue(contract, "objectRole", "object_role", "businessObjectRole", "business_object_role"))
	if instance.ObjectRole == "" {
		instance.ObjectRole = expectedObjectRole
		instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ApprovalObjectRole, instance.ObjectRole)
	}
	if instance.ApprovalObjectRole == "" {
		instance.ApprovalObjectRole = instance.ObjectRole
	}
	if expectedObjectRole != "" && !strings.EqualFold(instance.ObjectRole, expectedObjectRole) && !strings.EqualFold(instance.ApprovalObjectRole, expectedObjectRole) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: object_role %s does not match installed contract %s", firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole), expectedObjectRole)
	}
	expectedVersion := firstNonEmptyMaclawAppString(binding.WorkflowVersion, maclawAppStringValue(contract, "workflowVersion", "workflow_version"), maclawAppRuntimeWorkflowVersion(snapshot, instance.WorkflowSkillID))
	if instance.WorkflowVersion == "" {
		instance.WorkflowVersion = expectedVersion
	}
	if expectedVersion != "" && instance.WorkflowVersion != "" && instance.WorkflowVersion != expectedVersion {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: workflow_version %s does not match installed version %s", instance.WorkflowVersion, expectedVersion)
	}
	return normalizeMaclawAppApprovalInstanceFields(instance), nil
}

func maclawAppRuntimeDefaultWorkflowSkillID(contract map[string]any, snapshot maclawAppInstallVersionSnapshot) string {
	if id := maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id"); id != "" {
		return id
	}
	if len(snapshot.ApprovalBindings) == 1 {
		return snapshot.ApprovalBindings[0].WorkflowSkillID
	}
	if len(snapshot.WorkflowSkills) == 1 {
		return snapshot.WorkflowSkills[0].ID
	}
	return ""
}

func maclawAppRuntimeApprovalBindingSnapshot(snapshot maclawAppInstallVersionSnapshot, instance maclawAppApprovalInstance) maclawAppInstallApprovalBindingSnapshot {
	for _, binding := range snapshot.ApprovalBindings {
		if instance.WorkflowSkillID != "" && strings.EqualFold(binding.WorkflowSkillID, instance.WorkflowSkillID) {
			return binding
		}
	}
	for _, binding := range snapshot.ApprovalBindings {
		if instance.ApprovalEvent != "" && strings.EqualFold(binding.Event, instance.ApprovalEvent) {
			return binding
		}
	}
	if len(snapshot.ApprovalBindings) == 1 {
		return snapshot.ApprovalBindings[0]
	}
	return maclawAppInstallApprovalBindingSnapshot{}
}

func maclawAppRuntimeWorkflowVersion(snapshot maclawAppInstallVersionSnapshot, workflowSkillID string) string {
	for _, skill := range snapshot.WorkflowSkills {
		if strings.EqualFold(skill.ID, workflowSkillID) {
			return skill.Version
		}
	}
	return ""
}
func (a *App) findMaclawAppRecordApprovalID(input maclawAppApprovalDataSrvSyncInput, instance maclawAppApprovalInstance) string {
	out := a.executeMISDataTool(map[string]interface{}{
		"action":                "list_record_approvals",
		"dataset_id":            input.DatasetID,
		"record_id":             input.RecordID,
		"app_id":                input.AppID,
		"blueprint_id":          input.BlueprintID,
		"object_role":           input.ObjectRole,
		"workflow_instance_id":  instance.InstanceID,
		"approval_workflow_id":  firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
		"trigger_event":         instance.ApprovalEvent,
		"current_assignee":      instance.CurrentAssignee,
		"current_assignee_type": instance.CurrentAssigneeType,
		"from_status":           instance.FromStatus,
		"to_status":             instance.ToStatus,
		"status":                "pending",
		"limit":                 1,
	})
	return maclawAppApprovalIDFromToolResult(out)
}

func maclawAppApprovalIDFromToolResult(out string) string {
	var body map[string]any
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		return ""
	}
	if id := firstNonEmptyMaclawAppString(stringMapValue(body, "id"), stringMapValue(body, "approval_id"), stringMapValue(body, "record_approval_id")); id != "" {
		return id
	}
	if approval := anyMap(body["approval"]); approval != nil {
		if id := firstNonEmptyMaclawAppString(stringMapValue(approval, "id"), stringMapValue(approval, "approval_id"), stringMapValue(approval, "record_approval_id")); id != "" {
			return id
		}
	}
	for _, item := range anySlice(body["items"]) {
		approval := anyMap(item)
		if approval == nil {
			continue
		}
		if id := firstNonEmptyMaclawAppString(stringMapValue(approval, "id"), stringMapValue(approval, "approval_id"), stringMapValue(approval, "record_approval_id")); id != "" {
			return id
		}
	}
	return ""
}

func maclawAppApprovalStatusCanReview(status string) bool {
	switch strings.TrimSpace(status) {
	case "approved", "rejected":
		return true
	default:
		return false
	}
}

func (a *App) syncMaclawAppApprovalBusinessRecord(datasetID, recordID string, instance maclawAppApprovalInstance) map[string]any {
	return a.syncMaclawAppApprovalBusinessRecordForApproval(datasetID, recordID, instance, false)
}

func (a *App) syncMaclawAppApprovalBusinessRecordForApproval(datasetID, recordID string, instance maclawAppApprovalInstance, createIfMissing bool) map[string]any {
	patch := maclawAppApprovalBusinessRecordPatch(instance)
	if len(patch) == 0 && createIfMissing {
		patch = maclawAppApprovalFallbackBusinessRecordPatch(instance)
	}
	if len(patch) == 0 {
		return map[string]any{"synced": false, "reason": "missing business record patch"}
	}
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return map[string]any{"synced": false, "reason": err.Error()}
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
		return map[string]any{"synced": false, "reason": "mis data service unavailable"}
	}
	path := "/api/v1/data/datasets/" + pathEscape(datasetID) + "/records/" + pathEscape(recordID)
	data, err := a.callMISDataAPIBytes(cfg, http.MethodGet, path, nil)
	if err != nil {
		if createIfMissing && strings.Contains(err.Error(), "HTTP 404") {
			createBody := compactPayload(map[string]interface{}{
				"id":        recordID,
				"title":     instance.Title,
				"source_id": instance.InstanceID,
				"data":      cloneMapAny(patch),
			})
			response, createErr := a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records", createBody)
			if createErr != nil {
				return map[string]any{"synced": false, "reason": createErr.Error(), "patch": cloneMapAny(patch), "action": "create_business_record"}
			}
			result := map[string]any{}
			_ = json.Unmarshal(response, &result)
			return map[string]any{"synced": true, "action": "create_business_record", "record_id": recordID, "patch": cloneMapAny(patch), "response": result}
		}
		return map[string]any{"synced": false, "reason": err.Error(), "patch": cloneMapAny(patch)}
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		return map[string]any{"synced": false, "reason": err.Error(), "patch": cloneMapAny(patch)}
	}
	merged := cloneMapAny(anyMap(record["data"]))
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range patch {
		merged[key] = value
	}
	response, err := a.callMISDataAPIBytes(cfg, http.MethodPatch, path, compactPayload(map[string]interface{}{"data": merged}))
	if err != nil {
		return map[string]any{"synced": false, "reason": err.Error(), "patch": cloneMapAny(patch)}
	}
	result := map[string]any{}
	_ = json.Unmarshal(response, &result)
	return map[string]any{"synced": true, "action": "update_business_record", "record_id": recordID, "patch": cloneMapAny(patch), "response": result}
}

func maclawAppApprovalBusinessRecordPatch(instance maclawAppApprovalInstance) map[string]any {
	for _, key := range []string{"business_record_patch", "record_patch", "business_record"} {
		if patch := maclawAppApprovalPatchMap(instance.ResultPayload[key]); len(patch) > 0 {
			return mergeMaclawAppApprovalBusinessRecordPatch(patch, maclawAppApprovalSemanticBusinessRecordPatch(instance))
		}
	}
	for _, output := range instance.Outputs {
		outputKind := strings.ToLower(strings.TrimSpace(firstNonEmptyMaclawAppString(output.Kind, output.Type)))
		if outputKind != "business_record" && outputKind != "record_patch" {
			continue
		}
		if patch := maclawAppApprovalPatchMap(output.Data); len(patch) > 0 {
			return mergeMaclawAppApprovalBusinessRecordPatch(patch, maclawAppApprovalSemanticBusinessRecordPatch(instance))
		}
	}
	return nil
}
func maclawAppApprovalFallbackBusinessRecordPatch(instance maclawAppApprovalInstance) map[string]any {
	patch := mergeMaclawAppApprovalBusinessRecordPatch(nil, maclawAppApprovalSemanticBusinessRecordPatch(instance))
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func maclawAppApprovalBusinessRecordLane(instance maclawAppApprovalInstance) string {
	lane := normalizeMaclawAppApprovalLane(instance.Lane)
	switch strings.TrimSpace(instance.Status) {
	case "approved", "rejected":
		return "handled"
	case "attention":
		return "attention"
	}
	return lane
}
func maclawAppApprovalSemanticBusinessRecordPatch(instance maclawAppApprovalInstance) map[string]any {
	return compactPayload(map[string]interface{}{
		"app_id":                         instance.AppID,
		"app_name":                       instance.AppName,
		"blueprint_id":                   instance.BlueprintID,
		"object_role":                    firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole),
		"approval_event":                 instance.ApprovalEvent,
		"approval_workflow_id":           firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
		"approval_trigger_event":         firstNonEmptyMaclawAppString(instance.ApprovalEvent, instance.AppID),
		"approval_submitted_by":          firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner),
		"approval_instance_id":           instance.InstanceID,
		"approval_id":                    firstNonEmptyMaclawAppString(instance.ApprovalID, instance.RecordApprovalID),
		"record_approval_id":             firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID),
		"approval_status":                instance.Status,
		"approval_lane":                  maclawAppApprovalBusinessRecordLane(instance),
		"approval_current_node":          instance.CurrentNode,
		"approval_current_nodes":         append([]string(nil), instance.CurrentNodeIDs...),
		"approval_current_assignee":      instance.CurrentAssignee,
		"approval_current_assignee_type": instance.CurrentAssigneeType,
		"approval_from_status":           instance.FromStatus,
		"approval_to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
		"workflow_skill_id":              instance.WorkflowSkillID,
		"workflow_version":               instance.WorkflowVersion,
		"workflow_instance_id":           instance.InstanceID,
		"workflow_node_id":               instance.CurrentNode,
		"workflow_node_ids":              append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
		"workflow_decision_id":           instance.WorkflowDecisionID,
		"approval_detail_url":            instance.DetailURL,
		"approval_result_summary":        maclawAppApprovalResultSummary(instance),
		"approval_primary_artifact":      maclawAppApprovalPrimaryArtifactName(instance),
		"approval_output_count":          maclawAppApprovalCountValue(len(instance.Outputs)),
		"approval_artifact_count":        maclawAppApprovalCountValue(len(instance.Artifacts)),
		"approval_result_payload":        cloneMapAny(instance.ResultPayload),
		"approval_outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
		"approval_artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
		"business_entity":                instance.BusinessEntity,
		"business_action":                instance.BusinessAction,
		"business_note":                  instance.BusinessNote,
		"status":                         firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"business_status":                firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"result_status":                  firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
		"owner":                          instance.Owner,
		"applicant":                      instance.Applicant,
		"approver":                       instance.Approver,
	})
}

func maclawAppApprovalCountValue(count int) any {
	if count <= 0 {
		return nil
	}
	return count
}

func maclawAppApprovalResultSummary(instance maclawAppApprovalInstance) string {
	for _, key := range []string{"summary", "text", "content", "message", "result"} {
		if value, ok := instance.ResultPayload[key].(string); ok && strings.TrimSpace(value) != "" {
			return truncateMaclawAppApprovalSummary(value)
		}
	}
	businessRecordSummary := ""
	if businessRecord := anyMap(instance.ResultPayload["business_record"]); len(businessRecord) > 0 {
		for _, key := range []string{"summary", "title", "name", "status"} {
			if value, ok := businessRecord[key].(string); ok && strings.TrimSpace(value) != "" {
				businessRecordSummary = value
				break
			}
		}
	}
	if (instance.Status == "approved" || instance.Status == "rejected") && businessRecordSummary != "" {
		return truncateMaclawAppApprovalSummary(businessRecordSummary)
	}
	for _, output := range instance.Outputs {
		for _, value := range []string{output.Text, output.Title, output.Status, output.Kind, output.Type} {
			if strings.TrimSpace(value) != "" {
				return truncateMaclawAppApprovalSummary(value)
			}
		}
	}
	if businessRecordSummary != "" {
		return truncateMaclawAppApprovalSummary(businessRecordSummary)
	}
	for _, value := range []string{instance.Result, instance.ResultStatus, instance.BusinessStatus, instance.Status, instance.Title} {
		if strings.TrimSpace(value) != "" {
			return truncateMaclawAppApprovalSummary(value)
		}
	}
	return ""
}

func maclawAppApprovalPrimaryArtifactName(instance maclawAppApprovalInstance) string {
	for _, artifact := range instance.Artifacts {
		for _, value := range []string{artifact.Name, artifact.URI, artifact.ID, artifact.Path, artifact.RemoteURL} {
			if strings.TrimSpace(value) != "" {
				return truncateMaclawAppApprovalSummary(value)
			}
		}
	}
	for _, output := range instance.Outputs {
		if output.Artifact != nil {
			for _, value := range []string{output.Artifact.Name, output.Artifact.URI, output.Artifact.ID, output.Artifact.Path, output.Artifact.RemoteURL} {
				if strings.TrimSpace(value) != "" {
					return truncateMaclawAppApprovalSummary(value)
				}
			}
		}
	}
	return ""
}

func truncateMaclawAppApprovalSummary(value string) string {
	value = strings.TrimSpace(value)
	const maxRunes = 240
	if len([]rune(value)) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func mergeMaclawAppApprovalBusinessRecordPatch(primary, semantic map[string]any) map[string]any {
	merged := cloneMapAny(primary)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range semantic {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func maclawAppApprovalPatchMap(value any) map[string]any {
	data := anyMap(value)
	for _, key := range []string{"data", "fields", "set"} {
		if nested := anyMap(data[key]); len(nested) > 0 {
			data = nested
			break
		}
	}
	patch := map[string]any{}
	for key, item := range data {
		field := strings.TrimSpace(key)
		switch strings.ToLower(field) {
		case "", "id", "record_id", "recordid", "dataset_id", "datasetid":
			continue
		}
		patch[field] = item
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

// ListMaclawAppApprovalInstances returns newest-first approval instances for a
// MaClaw approval app. lane can be my_requests, pending_my_approval, handled,
// attention, or all/empty.
func (a *App) ListMaclawAppApprovalInstances(appID string, lane string, limit int) ([]maclawAppApprovalInstance, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
	return a.listMaclawAppApprovalInstances(appID, lane, limit)
}

// ListMaclawAppApprovalInstancesAll returns newest-first approval instances
// across all MaClaw approval apps. lane can be my_requests,
// pending_my_approval, handled, attention, or all/empty.
func (a *App) ListMaclawAppApprovalInstancesAll(lane string, limit int) ([]maclawAppApprovalInstance, error) {
	return a.listMaclawAppApprovalInstances("", lane, limit)
}

func (a *App) listMaclawAppApprovalInstances(appID string, lane string, limit int) ([]maclawAppApprovalInstance, error) {
	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	localContext := make([]maclawAppApprovalInstance, 0, len(registry.Instances))
	localVisible := make([]maclawAppApprovalInstance, 0, len(registry.Instances))
	for _, instance := range registry.Instances {
		if appID != "" && instance.AppID != appID {
			continue
		}
		cloned := cloneMaclawAppApprovalInstance(instance)
		localContext = append(localContext, cloned)
		if maclawAppApprovalInstanceMatchesLane(instance, lane) {
			localVisible = append(localVisible, cloned)
		}
	}
	remote, _ := a.listMaclawAppApprovalInstancesFromDataSrv(appID, lane, limit)
	out := localVisible
	if len(remote) > 0 {
		out = filterMaclawAppApprovalInstancesByLane(mergeMaclawAppApprovalInstanceLists(localContext, remote), lane)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (a *App) listMaclawAppApprovalInstancesFromDataSrv(appID string, lane string, limit int) ([]maclawAppApprovalInstance, error) {
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
		return nil, nil
	}
	values := url.Values{}
	if appID = strings.TrimSpace(appID); appID != "" {
		values.Set("app_id", appID)
	}
	if lane = normalizeMaclawAppApprovalLaneFilter(lane); lane != "" && lane != "all" {
		values.Set("lane", lane)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	values.Set("limit", fmt.Sprintf("%d", limit))
	path := "/api/v1/data/approvals"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	data, err := a.callMISDataAPIBytes(cfg, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Items []contract.RecordApproval `json:"items"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	out := make([]maclawAppApprovalInstance, 0, len(body.Items))
	for _, item := range body.Items {
		instance := maclawAppApprovalInstanceFromRecordApproval(item, lane, cfg.UserID)
		if instance.AppID == "" && appID != "" {
			instance.AppID = appID
		}
		if instance.AppID == "" && appID != "" {
			continue
		}
		out = append(out, instance)
	}
	return out, nil
}

func maclawAppApprovalInstanceFromRecordApproval(item contract.RecordApproval, requestedLane string, currentUserID string) maclawAppApprovalInstance {
	request := cloneMapAny(item.Request)
	resultPayload := cloneMapAny(item.ResultPayload)
	if resultPayload == nil {
		resultPayload = cloneMapAny(anyMap(firstNonEmptyMaclawAppAny(request["result_payload"], request["resultPayload"])))
	}
	instanceID := firstNonEmptyMaclawAppString(item.WorkflowInstanceID, stringMapValue(request, "approval_instance_id"), item.ID)
	status := normalizeMaclawAppApprovalStatusForRecordApproval(item)
	lane := normalizeMaclawAppApprovalLaneForRecordApproval(item, requestedLane, status, currentUserID)
	result := firstNonEmptyMaclawAppString(item.Reason, stringMapValue(resultPayload, "text"), stringMapValue(resultPayload, "summary"), item.ResultStatus, item.BusinessStatus, status)
	currentNodeIDs := append([]string(nil), item.WorkflowNodeIDs...)
	for _, key := range []string{"current_node_ids", "currentNodeIDs", "workflow_node_ids", "workflowNodeIDs", "workflowNodeIds", "current_node", "currentNode", "workflow_node", "workflowNode", "workflow_node_id", "workflowNodeId"} {
		currentNodeIDs = append(currentNodeIDs, maclawAppStringListFromAny(request[key])...)
	}
	outputs := maclawAppApprovalOutputsFromRecordApprovals(item.Outputs)
	if len(outputs) == 0 {
		outputs = maclawAppApprovalOutputsFromAny(firstNonEmptyMaclawAppAny(request["outputs"], request["approval_outputs"], request["approvalOutputs"]))
	}
	artifacts := maclawAppApprovalArtifactsFromRecordApprovals(item.Artifacts)
	if len(artifacts) == 0 {
		artifacts = maclawAppApprovalArtifactsFromAny(firstNonEmptyMaclawAppAny(request["artifacts"], request["approval_artifacts"], request["approvalArtifacts"]))
	}
	instance := maclawAppApprovalInstance{
		AppID:               firstNonEmptyMaclawAppString(item.AppID, stringMapValue(request, "app_id"), stringMapValue(request, "appID"), stringMapValue(request, "maclaw_app_id"), stringMapValue(request, "maclawAppID")),
		BlueprintID:         firstNonEmptyMaclawAppString(item.BlueprintID, stringMapValue(request, "blueprint_id"), stringMapValue(request, "blueprintID")),
		DatasetID:           item.DatasetID,
		ObjectRole:          firstNonEmptyMaclawAppString(item.ObjectRole, stringMapValue(request, "object_role"), stringMapValue(request, "objectRole"), stringMapValue(request, "business_object_role"), stringMapValue(request, "businessObjectRole")),
		ApprovalObjectRole:  firstNonEmptyMaclawAppString(item.ObjectRole, stringMapValue(request, "object_role"), stringMapValue(request, "objectRole"), stringMapValue(request, "business_object_role"), stringMapValue(request, "businessObjectRole")),
		ApprovalEvent:       firstNonEmptyMaclawAppString(item.TriggerEvent, stringMapValue(request, "approval_event"), stringMapValue(request, "approvalEvent"), stringMapValue(request, "trigger_event"), stringMapValue(request, "triggerEvent")),
		InstanceID:          instanceID,
		Title:               firstNonEmptyMaclawAppString(item.Summary, stringMapValue(request, "title"), instanceID, item.ID),
		Lane:                lane,
		Status:              status,
		CurrentNode:         firstNonEmptyMaclawAppString(item.WorkflowNodeID, stringMapValue(request, "current_node"), stringMapValue(request, "currentNode"), stringMapValue(request, "workflow_node"), stringMapValue(request, "workflowNode"), stringMapValue(request, "workflow_node_id"), stringMapValue(request, "workflowNodeId")),
		CurrentNodeIDs:      currentNodeIDs,
		WorkflowNodeIDs:     append([]string(nil), currentNodeIDs...),
		Owner:               firstNonEmptyMaclawAppString(item.SubmittedBy, item.CreatedBy, stringMapValue(request, "submitted_by"), stringMapValue(request, "submittedBy"), stringMapValue(request, "owner"), stringMapValue(request, "applicant")),
		Applicant:           firstNonEmptyMaclawAppString(stringMapValue(request, "applicant"), item.SubmittedBy, item.CreatedBy),
		Approver:            firstNonEmptyMaclawAppString(item.AssignedTo, item.CurrentAssignee, item.ReviewedBy, stringMapValue(request, "assigned_to"), stringMapValue(request, "assignedTo"), stringMapValue(request, "current_assignee"), stringMapValue(request, "currentAssignee"), stringMapValue(request, "reviewed_by"), stringMapValue(request, "reviewedBy")),
		CurrentAssignee:     firstNonEmptyMaclawAppString(item.CurrentAssignee, item.AssignedTo, stringMapValue(request, "current_assignee"), stringMapValue(request, "currentAssignee"), stringMapValue(request, "assigned_to"), stringMapValue(request, "assignedTo")),
		CurrentAssigneeType: firstNonEmptyMaclawAppString(item.CurrentAssigneeType, stringMapValue(request, "current_assignee_type"), stringMapValue(request, "currentAssigneeType"), stringMapValue(request, "assigned_to_type"), stringMapValue(request, "assignedToType")),
		CreatedAt:           maclawAppApprovalTimeString(item.CreatedAt),
		UpdatedAt:           maclawAppApprovalTimeString(item.UpdatedAt),
		Result:              result,
		ApprovalWorkflowID:  firstNonEmptyMaclawAppString(item.ApprovalWorkflowID, stringMapValue(request, "approval_workflow_id"), stringMapValue(request, "approvalWorkflowID"), stringMapValue(request, "workflow_id"), stringMapValue(request, "workflowID")),
		WorkflowSkillID:     firstNonEmptyMaclawAppString(item.WorkflowSkillID, stringMapValue(request, "workflow_skill_id"), stringMapValue(request, "workflowSkillID"), stringMapValue(request, "workflowSkillId")),
		WorkflowVersion:     firstNonEmptyMaclawAppString(item.WorkflowVersion, stringMapValue(request, "workflow_version"), stringMapValue(request, "workflowVersion")),
		BusinessStatus:      firstNonEmptyMaclawAppString(item.BusinessStatus, stringMapValue(request, "business_status"), stringMapValue(request, "businessStatus")),
		ResultStatus:        firstNonEmptyMaclawAppString(item.ResultStatus, stringMapValue(request, "result_status"), stringMapValue(request, "resultStatus")),
		FromStatus:          firstNonEmptyMaclawAppString(item.FromStatus, stringMapValue(request, "from_status"), stringMapValue(request, "fromStatus")),
		ToStatus:            firstNonEmptyMaclawAppString(item.ToStatus, stringMapValue(request, "to_status"), stringMapValue(request, "toStatus")),
		WorkflowDecisionID:  firstNonEmptyMaclawAppString(item.WorkflowDecisionID, stringMapValue(request, "workflow_decision_id"), stringMapValue(request, "workflowDecisionId")),
		RecordID:            item.RecordID,
		ApprovalID:          item.ID,
		RecordApprovalID:    item.ID,
		DetailURL:           firstNonEmptyMaclawAppString(item.DetailURL, stringMapValue(request, "detail_url"), stringMapValue(request, "detailURL"), stringMapValue(request, "detailUrl")),
		BusinessEntity:      firstNonEmptyMaclawAppString(stringMapValue(request, "business_entity"), stringMapValue(request, "businessEntity")),
		BusinessAction:      firstNonEmptyMaclawAppString(stringMapValue(request, "business_action"), stringMapValue(request, "businessAction")),
		BusinessNote:        firstNonEmptyMaclawAppString(stringMapValue(request, "business_note"), stringMapValue(request, "businessNote")),
		ResultPayload:       resultPayload,
		Outputs:             outputs,
		Artifacts:           artifacts,
	}
	if instance.UpdatedAt == "" {
		instance.UpdatedAt = instance.CreatedAt
	}
	return normalizeMaclawAppApprovalInstanceFields(instance)
}

func normalizeMaclawAppApprovalLaneForRecordApproval(item contract.RecordApproval, requestedLane string, status string, currentUserID string) string {
	request := cloneMapAny(item.Request)
	if lane := normalizeMaclawAppApprovalLaneFilter(firstNonEmptyMaclawAppString(stringMapValue(request, "lane"), stringMapValue(request, "approval_lane"), stringMapValue(request, "approvalLane"))); lane != "" && lane != "all" {
		return lane
	}
	if strings.EqualFold(strings.TrimSpace(item.Kind), "attention") || strings.EqualFold(strings.TrimSpace(item.BusinessStatus), "attention") || strings.EqualFold(strings.TrimSpace(item.ResultStatus), "attention") || status == "attention" {
		return "attention"
	}
	currentUserID = strings.TrimSpace(currentUserID)
	if currentUserID != "" {
		submitter := firstNonEmptyMaclawAppString(item.SubmittedBy, item.CreatedBy, stringMapValue(request, "submitted_by"), stringMapValue(request, "submittedBy"), stringMapValue(request, "owner"), stringMapValue(request, "applicant"))
		assignee := firstNonEmptyMaclawAppString(item.CurrentAssignee, item.AssignedTo, stringMapValue(request, "current_assignee"), stringMapValue(request, "currentAssignee"), stringMapValue(request, "assigned_to"), stringMapValue(request, "assignedTo"))
		reviewer := firstNonEmptyMaclawAppString(item.ReviewedBy, stringMapValue(request, "reviewed_by"), stringMapValue(request, "reviewedBy"))
		switch normalizeMaclawAppApprovalLaneFilter(requestedLane) {
		case "pending_my_approval":
			if status == "pending" && maclawAppApprovalActorMatches(currentUserID, assignee) {
				return "pending_my_approval"
			}
		case "handled":
			if status == "approved" || status == "rejected" || status == "cancelled" || status == "timeout" || maclawAppApprovalActorMatches(currentUserID, reviewer) {
				return "handled"
			}
		case "my_requests":
			if maclawAppApprovalActorMatches(currentUserID, submitter) {
				return "my_requests"
			}
		}
		if maclawAppApprovalActorMatches(currentUserID, submitter) {
			return "my_requests"
		}
		if status == "pending" && maclawAppApprovalActorMatches(currentUserID, assignee) {
			return "pending_my_approval"
		}
		if maclawAppApprovalActorMatches(currentUserID, reviewer) {
			return "handled"
		}
	}
	switch status {
	case "approved", "rejected", "cancelled", "timeout":
		return "handled"
	default:
		return "pending"
	}
}

func maclawAppApprovalActorMatches(currentUserID string, actor string) bool {
	currentUserID = strings.ToLower(strings.TrimSpace(currentUserID))
	if currentUserID == "" {
		return false
	}
	for _, candidate := range strings.FieldsFunc(strings.ToLower(actor), func(r rune) bool {
		switch r {
		case ',', ';', '|', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	}) {
		if strings.TrimSpace(candidate) == currentUserID {
			return true
		}
	}
	return false
}

func maclawAppApprovalOutputsFromRecordApprovals(outputs []contract.RecordApprovalOutput) []maclawAppApprovalOutput {
	if len(outputs) == 0 {
		return nil
	}
	out := make([]maclawAppApprovalOutput, 0, len(outputs))
	for _, output := range outputs {
		item := maclawAppApprovalOutput{
			Type:       output.Type,
			Kind:       output.Kind,
			Title:      output.Title,
			Text:       output.Text,
			Status:     output.Status,
			ArtifactID: output.ArtifactID,
			Data:       cloneMapAny(output.Data),
		}
		if output.Artifact != nil {
			artifact := maclawAppApprovalArtifactFromRecordApproval(*output.Artifact)
			item.Artifact = &artifact
		}
		out = append(out, item)
	}
	return out
}

func maclawAppApprovalOutputsFromAny(value any) []maclawAppApprovalOutput {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalOutput
	if err := json.Unmarshal(data, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func maclawAppApprovalArtifactsFromRecordApprovals(artifacts []contract.RecordApprovalArtifact) []maclawAppApprovalArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]maclawAppApprovalArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, maclawAppApprovalArtifactFromRecordApproval(artifact))
	}
	return out
}

func maclawAppApprovalArtifactsFromAny(value any) []maclawAppApprovalArtifact {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalArtifact
	if err := json.Unmarshal(data, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func maclawAppApprovalArtifactFromRecordApproval(artifact contract.RecordApprovalArtifact) maclawAppApprovalArtifact {
	return maclawAppApprovalArtifact{
		ID:            artifact.ID,
		URI:           artifact.URI,
		Name:          artifact.Name,
		Path:          artifact.Path,
		MimeType:      artifact.MimeType,
		SizeBytes:     artifact.SizeBytes,
		RemoteURL:     artifact.RemoteURL,
		Checksum:      artifact.Checksum,
		DownloadState: artifact.DownloadState,
		Status:        artifact.Status,
		Presentation:  artifact.Presentation,
	}
}

func mergeMaclawAppApprovalInstanceLists(local, remote []maclawAppApprovalInstance) []maclawAppApprovalInstance {
	merged := make([]maclawAppApprovalInstance, 0, len(local)+len(remote))
	index := map[string]int{}
	add := func(instance maclawAppApprovalInstance, preferIncoming bool) {
		instance = normalizeMaclawAppApprovalInstanceFields(instance)
		keys := maclawAppApprovalInstanceMergeKeys(instance)
		for _, key := range keys {
			if pos, ok := index[key]; ok {
				if preferIncoming {
					merged[pos] = mergeMaclawAppApprovalInstance(merged[pos], instance)
				} else {
					merged[pos] = mergeMaclawAppApprovalInstance(instance, merged[pos])
				}
				for _, nextKey := range maclawAppApprovalInstanceMergeKeys(merged[pos]) {
					index[nextKey] = pos
				}
				return
			}
		}
		pos := len(merged)
		merged = append(merged, instance)
		for _, key := range keys {
			index[key] = pos
		}
	}
	for _, instance := range local {
		add(instance, false)
	}
	for _, instance := range remote {
		add(instance, true)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return maclawAppApprovalSortTime(merged[i]).After(maclawAppApprovalSortTime(merged[j]))
	})
	out := make([]maclawAppApprovalInstance, len(merged))
	for i, instance := range merged {
		out[i] = cloneMaclawAppApprovalInstance(instance)
	}
	return out
}

func filterMaclawAppApprovalInstancesByLane(instances []maclawAppApprovalInstance, lane string) []maclawAppApprovalInstance {
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	if lane == "" || lane == "all" {
		return instances
	}
	out := make([]maclawAppApprovalInstance, 0, len(instances))
	for _, instance := range instances {
		if maclawAppApprovalInstanceMatchesLane(instance, lane) {
			out = append(out, instance)
		}
	}
	return out
}

func maclawAppApprovalInstanceMatchesLane(instance maclawAppApprovalInstance, lane string) bool {
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	if lane == "" || lane == "all" {
		return true
	}
	status := normalizeMaclawAppApprovalStatus(instance.Status)
	switch lane {
	case "handled":
		return status == "approved" || status == "rejected" || status == "cancelled" || status == "timeout" || normalizeMaclawAppApprovalLane(instance.Lane) == "handled"
	case "attention":
		return status == "attention" || normalizeMaclawAppApprovalLane(instance.Lane) == "attention"
	case "pending_my_approval":
		isLocalUnlinked := strings.TrimSpace(firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID)) == ""
		return status == "pending" && (normalizeMaclawAppApprovalLane(instance.Lane) == "pending_my_approval" || (isLocalUnlinked && strings.TrimSpace(firstNonEmptyMaclawAppString(instance.CurrentAssignee, instance.Approver)) != ""))
	case "my_requests":
		return normalizeMaclawAppApprovalLane(instance.Lane) == "my_requests"
	default:
		return normalizeMaclawAppApprovalLane(instance.Lane) == lane
	}
}
func maclawAppApprovalInstanceMergeKeys(instance maclawAppApprovalInstance) []string {
	keys := []string{}
	add := func(prefix string, parts ...string) {
		cleaned := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return
			}
			cleaned = append(cleaned, part)
		}
		keys = append(keys, prefix+":"+strings.Join(cleaned, "|"))
	}
	add("approval", firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID))
	add("workflow", instance.WorkflowSkillID, instance.InstanceID)
	add("instance", instance.AppID, instance.InstanceID)
	add("record", instance.AppID, instance.DatasetID, instance.RecordID)
	return keys
}

func maclawAppApprovalTimeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func maclawAppApprovalSortTime(instance maclawAppApprovalInstance) time.Time {
	for _, value := range []string{instance.UpdatedAt, instance.CreatedAt} {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return time.Time{}
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
	packageSHA, _, err := maclawAppPackageFingerprint(record.Package)
	if err != nil {
		return nil, err
	}
	if record.PackageSHA != "" && !strings.EqualFold(strings.TrimSpace(record.PackageSHA), packageSHA) {
		return nil, fmt.Errorf("submission %s package fingerprint mismatch: recorded %s, current %s", record.SubmissionID, record.PackageSHA, packageSHA)
	}
	packageJSON, err := maclawAppStableJSON(record.Package)
	if err != nil {
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

	// Pre-upload gate: verify all required skill dependencies are resolvable
	// from Hub. A dependency is resolvable if the locally installed skill has
	// a HubSkillID (assigned when the skill was published to Hub/SkillMarket).
	// This prevents uploading an App whose dependencies cannot be installed by
	// receivers who download from the market.
	if gateErr := a.validateAppDependenciesPublished(plan); gateErr != nil {
		return nil, gateErr
	}

	body, err := json.Marshal(map[string]any{
		"package":              record.Package,
		"source_submission_id": record.SubmissionID,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/capabilities/maclaw-apps/submit", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 60 * time.Second}
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
	hubSubmissionID := firstNonEmptyMaclawAppString(hubResp.PackageSHA256, record.PackageSHA)
	if len(hubResp.Submissions) > 0 {
		hubSubmissionID = firstNonEmptyMaclawAppString(hubResp.Submissions[0].SubmissionID, hubResp.Submissions[0].VersionKey, hubSubmissionID)
	}
	if hubSubmissionID == "" {
		return nil, fmt.Errorf("enterprise Hub submit response missing submission identifier")
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
		HubCapabilityID: firstMaclawAppHubCapabilityID(hubResp.Submissions),
		Message:         message,
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("submission %s disappeared before Hub sync status update", record.SubmissionID)
	}
	hubPackageSHA := firstNonEmptyMaclawAppString(hubResp.PackageSHA256, record.PackageSHA)
	return map[string]any{
		"submission_id":        hubSubmissionID,
		"source_submission_id": record.SubmissionID,
		"hub_capability_id":    firstMaclawAppHubCapabilityID(hubResp.Submissions),
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
	queue, err := a.readMaclawAppSubmissionQueue()
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
	return true, a.writeMaclawAppSubmissionQueue(queue)
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
	queue, err := a.readMaclawAppSubmissionQueue()
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
		queue.Submissions[i].Events = append(queue.Submissions[i].Events, queue.Submissions[i].maclawAppSubmissionEvent(now))
		queue.UpdatedAt = now
		return true, a.writeMaclawAppSubmissionQueue(queue)
	}
	return false, nil
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

func parseMaclawAppPackageEntriesFromMap(pkg map[string]any, requirePack bool) ([]parsedMaclawAppEntry, error) {
	if requirePack && stringMapValue(pkg, "schema") != "maclaw.app.pack.v1" {
		return nil, fmt.Errorf("maclaw app package schema must be maclaw.app.pack.v1")
	}
	if stringMapValue(pkg, "privateMarker") != "x_maclaw_apps" {
		return nil, fmt.Errorf("maclaw app package privateMarker must be x_maclaw_apps")
	}
	rawApps, ok := pkg["apps"].([]any)
	if !ok || len(rawApps) == 0 {
		return nil, fmt.Errorf("maclaw app package apps must be a non-empty array")
	}
	entries := make([]parsedMaclawAppEntry, 0, len(rawApps))
	seenIDs := make(map[string]struct{}, len(rawApps))
	for i, raw := range rawApps {
		entry, ok := raw.(map[string]any)
		if !ok {
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
	selected := maclawAppSelectionIDSet(selectedAppIDs)
	if len(selected) == 0 {
		return cloneMapAny(pkg), entries, nil
	}
	filteredEntries := make([]parsedMaclawAppEntry, 0, len(entries))
	filteredApps := make([]any, 0, len(entries))
	for _, entry := range entries {
		if !maclawAppSelectionMatches(selected, entry.ID) {
			continue
		}
		filteredEntries = append(filteredEntries, entry)
		entryClone := cloneMapAny(entry.Entry)
		if filtered := maclawAppResolvedDependenciesForSelectedEntries(entryClone["resolved_dependencies"], []parsedMaclawAppEntry{entry}); filtered != nil {
			entryClone["resolved_dependencies"] = filtered
		} else {
			delete(entryClone, "resolved_dependencies")
		}
		filteredApps = append(filteredApps, entryClone)
	}
	if len(filteredEntries) == 0 {
		return nil, nil, fmt.Errorf("selected MaClaw App package has no matching apps")
	}
	installPackage := cloneMapAny(pkg)
	installPackage["apps"] = filteredApps
	if filtered := maclawAppResolvedDependenciesForSelectedEntries(pkg["resolved_dependencies"], filteredEntries); filtered != nil {
		installPackage["resolved_dependencies"] = filtered
	} else {
		delete(installPackage, "resolved_dependencies")
	}
	return installPackage, filteredEntries, nil
}

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

func (a *App) installedMaclawAppSkillIndex() map[string]NLSkillDefinition {
	defs := a.ListNLSkills()
	index := make(map[string]NLSkillDefinition, len(defs))
	for _, def := range defs {
		// Primary key: QualifiedID (publisher:name or bare name)
		if qid := def.QualifiedID(); qid != "" {
			index[strings.ToLower(qid)] = def
		}
		// Secondary keys for backward compatibility: Name, DirName, HubSkillID
		for _, id := range []string{def.Name, def.DirName, def.HubSkillID} {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			key := strings.ToLower(id)
			if _, exists := index[key]; !exists {
				index[key] = def
			}
		}
	}
	return index
}

func applyMaclawAppInstalledSkillDependency(dep *maclawAppInstallPlanDependency, match NLSkillDefinition) {
	dep.Installed = true
	dep.InstalledName = match.Name
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
	if installed == required {
		return "matched"
	}
	return "mismatch"
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
		return true
	}
	health := strings.TrimSpace(dep.Health)
	return health != "" && health != "ready"
}

func maclawAppInstallPlanDependencyMergeKey(dep maclawAppInstallPlanDependency) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(dep.ID)),
		strings.ToLower(strings.TrimSpace(dep.Version)),
		strings.ToLower(strings.TrimSpace(dep.Source)),
		strings.ToLower(strings.TrimSpace(dep.InstallRef)),
		strconv.FormatBool(dep.Required),
	}, "\x1f")
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

func maclawAppInstallEvidenceByApp(entries []parsedMaclawAppEntry, dependencies []maclawAppInstallPlanDependency) map[string]any {
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
			"workflow_mapping":        maclawAppWorkflowMappingForEntry(entry),
			"workflow_contract":       maclawAppWorkflowContractForEntry(entry),
			"test_evidence":           anyMap(governance["test_evidence"]),
			"dependency_verification": maclawAppDependencyVerificationMetadataForEntry(entry, dependencies),
		})
	}
	return out
}

func maclawAppSubmissionEvidenceForRecord(record maclawAppSubmissionRecord) map[string]any {
	if len(record.Package) == 0 {
		return nil
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(record.Package, false)
	if err != nil || len(entries) == 0 {
		return nil
	}
	return maclawAppInstallEvidenceByApp(entries, record.Dependencies)
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
		snapshot.ApprovalBindings = append(snapshot.ApprovalBindings, maclawAppInstallApprovalBindingSnapshot{
			Event:           maclawAppStringValue(binding, "event"),
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
	installEvidenceByApp := maclawAppInstallEvidenceByApp(entries, dependencies)
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
			if regionCount, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(workspaceLayout["regionCount"], workspaceLayout["region_count"])); ok && regionCount > 0 {
				metadata["workspace_layout_region_count"] = int(regionCount)
			}
			if regions := anySlice(workspaceLayout["regions"]); len(regions) > 0 {
				regionIDs := make([]string, 0, len(regions))
				for _, rawRegion := range regions {
					region := anyMap(rawRegion)
					if id := maclawAppStringValue(region, "id"); id != "" {
						regionIDs = append(regionIDs, id)
					}
				}
				if len(regionIDs) > 0 {
					metadata["workspace_layout_region_ids"] = regionIDs
				}
				if _, exists := metadata["workspace_layout_region_count"]; !exists {
					metadata["workspace_layout_region_count"] = len(regions)
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
func maclawAppTestEvidenceOutputs(testEvidence map[string]any) []any {
	if testEvidence == nil {
		return nil
	}
	if outputs := anySlice(testEvidence["outputs"]); len(outputs) > 0 {
		return outputs
	}
	return anySlice(testEvidence["output_blocks"])
}

func maclawAppDependencyVerificationMetadataForEntry(entry parsedMaclawAppEntry, dependencies []maclawAppInstallPlanDependency) map[string]interface{} {
	appDependencies := cloneMaclawAppPlanDependenciesForApp(dependencies, entry.ID)
	if governance := maclawAppGovernanceMetadataForEntry(entry); governance != nil {
		if verification := anyMap(governance["dependency_verification"]); verification != nil {
			out := cloneMapAny(verification)
			if len(appDependencies) > 0 {
				out["dependencies"] = maclawAppMergedDependencyVerificationItems(anySlice(verification["dependencies"]), appDependencies)
				out["dependency_count"] = len(appDependencies)
				out["dependencyCount"] = len(appDependencies)
				out["has_missing_required"] = hasMissingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
				out["hasMissingRequired"] = hasMissingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
				out["has_blocking_dependency"] = hasBlockingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
				out["hasBlockingDependency"] = hasBlockingMaclawAppRequiredDependencyForApp(dependencies, entry.ID)
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
		"dependencies":            appDependencies,
	})
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
	if ui == nil {
		governance := maclawAppGovernanceMetadataForEntry(entry)
		workspaceLayout := anyMap(governance["workspace_layout"])
		if workspaceLayout == nil {
			return nil
		}
		out := cloneMapAny(workspaceLayout)
		if primary := maclawAppStringValue(out, "primaryRegion", "primary_region"); primary != "" {
			out["primaryRegion"] = primary
			out["primary_region"] = primary
		}
		if output := maclawAppStringValue(out, "outputRegion", "output_region"); output != "" {
			out["outputRegion"] = output
			out["output_region"] = output
		}
		if count, ok := maclawAppNumberFromAny(firstNonEmptyMaclawAppAny(out["regionCount"], out["region_count"])); ok && count > 0 {
			regionCount := int(math.Floor(count))
			out["regionCount"] = regionCount
			out["region_count"] = regionCount
		} else if regions := anySlice(out["regions"]); len(regions) > 0 {
			out["regionCount"] = len(regions)
			out["region_count"] = len(regions)
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
		if regions := anySlice(layout["regions"]); len(regions) > 0 {
			out["regionCount"] = len(regions)
			out["region_count"] = len(regions)
			out["regions"] = regions
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

func maclawAppWorkflowContractForEntry(entry parsedMaclawAppEntry) map[string]any {
	if governance := anyMap(entry.App["governance"]); governance != nil {
		if contract := anyMap(firstNonEmptyMaclawAppAny(governance["workflowContract"], governance["workflow_contract"])); contract != nil {
			return contract
		}
	}
	if binding := anyMap(entry.App["binding"]); binding != nil {
		if contract := anyMap(firstNonEmptyMaclawAppAny(binding["workflowContract"], binding["workflow_contract"])); contract != nil {
			return contract
		}
	}
	return nil
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

func maclawAppApprovalBindingMapsForEntry(entry parsedMaclawAppEntry) []map[string]any {
	out := []map[string]any{}
	for _, holder := range maclawAppBindingHolders(entry) {
		misBlock := anyMap(holder["mis"])
		if misBlock == nil {
			continue
		}
		bindings := anySlice(misBlock["approvalBindings"])
		if len(bindings) == 0 {
			bindings = anySlice(misBlock["approval_bindings"])
		}
		for _, item := range bindings {
			if binding := anyMap(item); binding != nil {
				out = append(out, binding)
			}
		}
	}
	return out
}
func maclawAppWorkflowMappingForEntry(entry parsedMaclawAppEntry) map[string]any {
	for _, holder := range maclawAppBindingHolders(entry) {
		if workflow := anyMap(holder["workflow"]); workflow != nil {
			return workflow
		}
	}
	if governance := anyMap(entry.App["governance"]); governance != nil {
		if workflow := anyMap(governance["workflow"]); workflow != nil {
			return workflow
		}
	}
	return nil
}

func normalizeMaclawAppWorkflowMapping(app map[string]any, kind, path string) error {
	kind = normalizeMaclawAppKind(kind)
	workflow, owner := maclawAppWorkflowMappingBlock(app)
	if kind != "enterprise_approval_app" {
		if workflow != nil {
			return fmt.Errorf("%s.binding.workflow is only valid for enterprise_approval_app", path)
		}
		return nil
	}
	objectRole := "record"
	if binding := anyMap(app["binding"]); binding != nil {
		if datasrv := anyMap(binding["datasrv"]); datasrv != nil {
			objectRole = firstNonEmptyMaclawAppString(maclawAppStringValue(datasrv, "objectRole", "object_role", "domain"), objectRole)
		}
	}
	for _, binding := range maclawAppApprovalBindingMapsForApp(app) {
		objectRole = firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "objectRole", "object_role", "businessObjectRole", "business_object_role", "role"), objectRole)
	}
	if workflow == nil {
		binding := anyMap(app["binding"])
		if binding == nil {
			binding = map[string]any{}
			app["binding"] = binding
		}
		binding["workflow"] = defaultMaclawAppWorkflowMapping(objectRole)
		return nil
	}
	if err := normalizeMaclawAppWorkflowMappingDetails(workflow, path+".binding.workflow", objectRole); err != nil {
		return err
	}
	if owner != nil {
		owner["workflow"] = workflow
	}
	return nil
}

func maclawAppWorkflowMappingBlock(app map[string]any) (map[string]any, map[string]any) {
	if binding := anyMap(app["binding"]); binding != nil {
		if workflow := anyMap(binding["workflow"]); workflow != nil {
			return workflow, binding
		}
	}
	if workflow := anyMap(app["workflow"]); workflow != nil {
		return workflow, app
	}
	return nil, nil
}

func maclawAppApprovalBindingMapsForApp(app map[string]any) []map[string]any {
	return maclawAppApprovalBindingMapsForEntry(parsedMaclawAppEntry{App: app})
}

func defaultMaclawAppWorkflowMapping(objectRole string) map[string]any {
	role := strings.TrimSpace(objectRole)
	if role == "" {
		role = "record"
	}
	return map[string]any{
		"schema":        "maclaw.app.workflow.v1",
		"submitNode":    role + ".submit",
		"approvalNode":  role + ".manager_approval",
		"resultNode":    role + ".result_feedback",
		"attentionNode": role + ".attention_review",
		"statusMapping": map[string]any{
			"pending":       "approval_pending",
			"approved":      "approved",
			"rejected":      "rejected",
			"attention":     "attention",
			"requiresInput": "requires_input",
		},
	}
}

func normalizeMaclawAppWorkflowMappingDetails(workflow map[string]any, path string, objectRole string) error {
	if workflow == nil {
		return fmt.Errorf("%s must be an object", path)
	}
	if schema := firstNonEmptyMaclawAppString(maclawAppStringValue(workflow, "schema"), "maclaw.app.workflow.v1"); schema != "maclaw.app.workflow.v1" {
		return fmt.Errorf("%s.schema must be maclaw.app.workflow.v1", path)
	}
	workflow["schema"] = "maclaw.app.workflow.v1"
	defaults := defaultMaclawAppWorkflowMapping(objectRole)
	for _, pair := range []struct{ camel, snake string }{{"submitNode", "submit_node"}, {"approvalNode", "approval_node"}, {"resultNode", "result_node"}, {"attentionNode", "attention_node"}} {
		value := firstNonEmptyMaclawAppString(maclawAppStringValue(workflow, pair.camel), maclawAppStringValue(workflow, pair.snake), maclawAppStringValue(defaults, pair.camel))
		if value == "" {
			return fmt.Errorf("%s.%s is required", path, pair.camel)
		}
		workflow[pair.camel] = value
		delete(workflow, pair.snake)
	}
	statusMapping := anyMap(workflow["statusMapping"])
	if statusMapping == nil {
		statusMapping = anyMap(workflow["status_mapping"])
	}
	if statusMapping == nil {
		statusMapping = map[string]any{}
	}
	defaultStatus := anyMap(defaults["statusMapping"])
	for _, pair := range []struct{ camel, snake string }{{"pending", "pending"}, {"approved", "approved"}, {"rejected", "rejected"}, {"attention", "attention"}, {"requiresInput", "requires_input"}} {
		value := firstNonEmptyMaclawAppString(maclawAppStringValue(statusMapping, pair.camel), maclawAppStringValue(statusMapping, pair.snake), maclawAppStringValue(defaultStatus, pair.camel))
		if value == "" {
			return fmt.Errorf("%s.statusMapping.%s is required", path, pair.camel)
		}
		statusMapping[pair.camel] = value
		if pair.snake != pair.camel {
			delete(statusMapping, pair.snake)
		}
	}
	workflow["statusMapping"] = statusMapping
	delete(workflow, "status_mapping")
	return nil
}

func maclawAppHasWorkflowSkillForEntry(entry parsedMaclawAppEntry) bool {
	if len(maclawAppWorkflowSkillIDsForEntry(entry)) > 0 {
		return true
	}
	for _, holder := range maclawAppBindingHolders(entry) {
		depsBlock := anyMap(holder["dependencies"])
		if depsBlock == nil {
			continue
		}
		for _, item := range anySlice(depsBlock["skills"]) {
			depMap := anyMap(item)
			if depMap == nil {
				continue
			}
			if required, ok := depMap["required"].(bool); ok && !required {
				continue
			}
			if strings.TrimSpace(maclawAppStringValue(depMap, "id")) != "" && strings.TrimSpace(maclawAppStringValue(depMap, "kind")) == "workflow_skill" {
				return true
			}
		}
	}
	return false
}
func maclawAppWorkflowSkillIDsForEntry(entry parsedMaclawAppEntry) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, binding := range maclawAppApprovalBindingMapsForEntry(entry) {
		workflowSkillID := firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "workflowSkillId", "workflow_skill_id"), maclawAppStringValue(binding, "workflowId", "workflow_id"))
		if workflowSkillID == "" {
			continue
		}
		key := strings.ToLower(workflowSkillID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, workflowSkillID)
	}
	return out
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
		if dep.Source == "" {
			dep.Source = "hub"
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
				ID:         stringMapValue(skill, "id"),
				Version:    stringMapValue(skill, "version"),
				Kind:       "runtime_skill",
				Required:   true,
				Source:     stringMapValue(skill, "source"),
				InstallRef: maclawAppDependencyInstallRef(skill),
			})
		}
		for _, appSkill := range []map[string]any{anyMap(holder["appSkill"]), anyMap(holder["app_skill"])} {
			if appSkill == nil {
				continue
			}
			add(maclawAppInstallPlanDependency{
				ID:         stringMapValue(appSkill, "id"),
				Version:    stringMapValue(appSkill, "version"),
				Kind:       "app_skill",
				Required:   true,
				Source:     stringMapValue(appSkill, "source"),
				InstallRef: maclawAppDependencyInstallRef(appSkill),
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
					ID:         firstNonEmptyMISAgentView(stringMapValue(bindingMap, "workflowSkillId"), stringMapValue(bindingMap, "workflow_skill_id"), stringMapValue(bindingMap, "workflowId"), stringMapValue(bindingMap, "workflow_id")),
					Version:    firstNonEmptyMISAgentView(stringMapValue(bindingMap, "workflowVersion"), stringMapValue(bindingMap, "workflow_version")),
					Kind:       "workflow_skill",
					Required:   true,
					Source:     "hub",
					InstallRef: maclawAppDependencyInstallRef(bindingMap),
				})
			}
		}
		if depsBlock := anyMap(holder["dependencies"]); depsBlock != nil {
			for _, item := range anySlice(depsBlock["skills"]) {
				depMap := anyMap(item)
				if depMap == nil {
					continue
				}
				required := true
				if rawRequired, ok := depMap["required"].(bool); ok {
					required = rawRequired
				}
				add(maclawAppInstallPlanDependency{
					ID:         stringMapValue(depMap, "id"),
					Version:    stringMapValue(depMap, "version"),
					Kind:       stringMapValue(depMap, "kind"),
					Required:   required,
					Source:     stringMapValue(depMap, "source"),
					InstallRef: maclawAppDependencyInstallRef(depMap),
				})
			}
		}
	}
	return deps
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
func maclawAppWorkflowContractIssuesForEntries(entries []parsedMaclawAppEntry, installed map[string]NLSkillDefinition) []maclawAppReviewIssue {
	issues := []maclawAppReviewIssue{}
	for idx, entry := range entries {
		governance := anyMap(entry.App["governance"])
		appPath := fmt.Sprintf("apps[%d].app", idx)
		if issue := maclawAppWorkflowContractReviewIssue(entry, governance, appPath); issue != nil {
			issues = append(issues, *issue)
		}
		issues = append(issues, maclawAppWorkflowRuntimeContractReviewIssues(entry, installed, appPath)...)
	}
	return normalizeMaclawAppReviewIssues(issues)
}

func maclawAppWorkflowContractIssuesShouldPrecedeDependencyBlock(issues []maclawAppReviewIssue, hasDependencyBlock bool) bool {
	if len(issues) == 0 {
		return false
	}
	if !hasDependencyBlock {
		return true
	}
	first := strings.ToLower(strings.TrimSpace(firstMaclawAppReviewIssueMessage(issues, "")))
	return first != "" && !strings.Contains(first, "missing approval workflow contract")
}

func maclawAppWorkflowRuntimeContractReviewIssues(entry parsedMaclawAppEntry, installed map[string]NLSkillDefinition, appPath string) []maclawAppReviewIssue {
	if normalizeMaclawAppKind(entry.Kind) != "enterprise_approval_app" {
		return nil
	}
	snapshot := maclawAppInstallVersionSnapshotForEntry(entry)
	if len(snapshot.ApprovalBindings) == 0 {
		return []maclawAppReviewIssue{{Path: appPath + ".binding.mis.approvalBindings", Severity: "error", Message: "approval app has no approval workflow binding", Suggestion: "declare binding.mis.approvalBindings with event, objectRole, and workflowSkillId"}}
	}
	contract := maclawAppWorkflowContractMapForEntry(entry)
	contractWorkflowID := maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id")
	contractVersion := maclawAppWorkflowContractVersion(contract)
	issues := []maclawAppReviewIssue{}
	for idx, binding := range snapshot.ApprovalBindings {
		path := fmt.Sprintf("%s.binding.mis.approvalBindings[%d]", appPath, idx)
		workflowID := firstNonEmptyMaclawAppString(binding.WorkflowSkillID, contractWorkflowID)
		if workflowID == "" {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".workflowSkillId", Severity: "error", Message: "approval workflow binding is missing workflowSkillId", Suggestion: "bind this approval event to an installed workflow Skill"})
			continue
		}
		match, ok := installed[strings.ToLower(workflowID)]
		if !ok {
			continue
		}
		installedStatus, health := maclawAppInstalledSkillStatus(match)
		if health != "ready" {
			issue := maclawAppReviewIssue{Path: path + ".workflowSkillId", Severity: "error", Message: fmt.Sprintf("approval workflow Skill %s is installed but not active", workflowID), Suggestion: "enable or finish setup for the workflow Skill before running approval instances", Metadata: maclawAppWorkflowRuntimeContractIssueMetadata(workflowID, "", "", binding.Event, binding.ObjectRole, installedStatus, health)}
			if installedStatus != "" {
				issue.Message += fmt.Sprintf(" (status: %s)", installedStatus)
			}
			issues = append(issues, issue)
			continue
		}
		expectedVersion := firstNonEmptyMaclawAppString(binding.WorkflowVersion, contractVersion)
		installedVersion := firstNonEmptyMaclawAppString(match.HubVersion)
		if !maclawAppWorkflowVersionMatches(expectedVersion, installedVersion) {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".workflowVersion", Severity: "error", Message: fmt.Sprintf("approval workflow Skill %s version %s does not match required %s", workflowID, installedVersion, expectedVersion), Suggestion: "install the workflow Skill version declared by the app approval binding or workflow contract", Metadata: maclawAppWorkflowRuntimeContractIssueMetadata(workflowID, expectedVersion, installedVersion, binding.Event, binding.ObjectRole, installedStatus, health)})
		}
	}
	return normalizeMaclawAppReviewIssues(issues)
}

func maclawAppWorkflowRuntimeContractIssueMetadata(workflowID, requiredVersion, installedVersion, event, objectRole, installedStatus, health string) map[string]any {
	metadata := map[string]any{}
	for key, value := range map[string]string{
		"workflow_skill_id": workflowID,
		"required_version":  requiredVersion,
		"installed_version": installedVersion,
		"binding_event":     event,
		"object_role":       objectRole,
		"installed_status":  installedStatus,
		"health":            health,
	} {
		if value := strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}
func maclawAppWorkflowContractMapForEntry(entry parsedMaclawAppEntry) map[string]any {
	governance := anyMap(entry.App["governance"])
	contract := anyMap(firstNonEmptyMaclawAppAny(governance["workflowContract"], governance["workflow_contract"]))
	if contract != nil {
		return contract
	}
	if binding := anyMap(entry.App["binding"]); binding != nil {
		return anyMap(firstNonEmptyMaclawAppAny(binding["workflowContract"], binding["workflow_contract"]))
	}
	return nil
}

func maclawAppWorkflowContractVersion(contract map[string]any) string {
	if contract == nil {
		return ""
	}
	return maclawAppVersionString(firstNonEmptyMaclawAppAny(contract["workflowVersion"], contract["workflow_version"], contract["version"], contract["versionConstraint"], contract["version_constraint"]))
}

func maclawAppWorkflowVersionMatches(required, installed string) bool {
	required = strings.TrimSpace(required)
	installed = strings.TrimSpace(installed)
	if required == "" || installed == "" {
		return true
	}
	if strings.EqualFold(required, installed) {
		return true
	}
	if strings.ContainsAny(required, "<>=^~*") {
		return true
	}
	return false
}
func maclawAppWorkflowContractReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	if normalizeMaclawAppKind(entry.Kind) != "enterprise_approval_app" {
		return nil
	}
	contract := anyMap(firstNonEmptyMaclawAppAny(governance["workflowContract"], governance["workflow_contract"]))
	if contract == nil {
		if binding := anyMap(entry.App["binding"]); binding != nil {
			contract = anyMap(firstNonEmptyMaclawAppAny(binding["workflowContract"], binding["workflow_contract"]))
		}
	}
	if contract == nil {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract", Severity: "error", Message: "missing approval workflow contract", Suggestion: "declare the workflow skill input, decision output, and status mapping contract before submitting"}
	}
	if strings.TrimSpace(maclawAppStringValue(contract, "schema")) != "maclaw.app.workflow_contract.v1" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract", Severity: "error", Message: "invalid approval workflow contract schema", Suggestion: "set workflowContract.schema to maclaw.app.workflow_contract.v1"}
	}
	workflowSkillID := strings.TrimSpace(maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id"))
	if workflowSkillID == "" || !maclawAppWorkflowContractMatchesWorkflowSkill(entry, workflowSkillID) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.workflowSkillId", Severity: "error", Message: "approval workflow contract does not match approval binding", Suggestion: "use a workflowSkillId declared by approvalBindings or workflow_skill dependencies"}
	}
	objectRole := strings.TrimSpace(maclawAppStringValue(contract, "objectRole", "object_role", "businessObjectRole", "business_object_role"))
	if objectRole == "" || !maclawAppWorkflowContractMatchesObjectRole(entry, objectRole) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.objectRole", Severity: "error", Message: "approval workflow contract object role does not match app binding", Suggestion: "align workflowContract.objectRole with approvalBindings.objectRole or binding.datasrv.objectRole"}
	}
	for _, required := range []string{"record_ref", "applicant", "business_payload"} {
		if !maclawAppStringListContains(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(contract["requiredInputs"], contract["required_inputs"])), required) {
			return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.requiredInputs", Severity: "error", Message: "approval workflow contract is missing required input: " + required, Suggestion: "include record_ref, applicant, and business_payload in workflowContract.requiredInputs"}
		}
	}
	for _, required := range []string{"approved", "rejected", "attention"} {
		if !maclawAppStringListContains(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(contract["decisionOutputs"], contract["decision_outputs"])), required) {
			return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.decisionOutputs", Severity: "error", Message: "approval workflow contract is missing decision output: " + required, Suggestion: "include approved, rejected, and attention in workflowContract.decisionOutputs"}
		}
	}
	statusMapping := anyMap(firstNonEmptyMaclawAppAny(contract["statusMapping"], contract["status_mapping"]))
	for _, required := range []string{"pending", "approved", "rejected", "attention"} {
		if strings.TrimSpace(maclawAppStringValue(statusMapping, required)) == "" {
			return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.statusMapping", Severity: "error", Message: "approval workflow contract is missing status mapping: " + required, Suggestion: "map pending, approved, rejected, and attention workflow states to app statuses"}
		}
	}
	return nil
}

func maclawAppWorkflowContractMatchesWorkflowSkill(entry parsedMaclawAppEntry, workflowSkillID string) bool {
	needle := strings.ToLower(strings.TrimSpace(workflowSkillID))
	if needle == "" {
		return false
	}
	for _, id := range maclawAppWorkflowSkillIDsForEntry(entry) {
		if strings.ToLower(strings.TrimSpace(id)) == needle {
			return true
		}
	}
	for _, dep := range maclawAppDependenciesForEntry(entry) {
		if strings.TrimSpace(dep.Kind) == "workflow_skill" && strings.ToLower(strings.TrimSpace(dep.ID)) == needle {
			return true
		}
	}
	return false
}

func maclawAppWorkflowContractMatchesObjectRole(entry parsedMaclawAppEntry, objectRole string) bool {
	needle := strings.ToLower(strings.TrimSpace(objectRole))
	if needle == "" {
		return false
	}
	for _, binding := range maclawAppApprovalBindingMapsForEntry(entry) {
		if strings.ToLower(strings.TrimSpace(firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "objectRole", "object_role"), maclawAppStringValue(binding, "businessObjectRole", "business_object_role"), maclawAppStringValue(binding, "role")))) == needle {
			return true
		}
	}
	if datasrv := maclawAppDataSrvBlockForEntry(entry); datasrv != nil {
		if strings.ToLower(strings.TrimSpace(firstNonEmptyMaclawAppString(maclawAppStringValue(datasrv, "objectRole", "object_role"), maclawAppStringValue(datasrv, "businessObjectRole", "business_object_role"), maclawAppStringValue(datasrv, "domain")))) == needle {
			return true
		}
	}
	return false
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
	computed := firstNonEmptyMaclawAppString(protocolFingerprint, maclawAppTestProtocolFingerprint(protocol))
	if computed != "" && fingerprint != computed {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.testProtocolFingerprint", Severity: "error", Message: "run evidence test protocol fingerprint does not match the current test protocol", Suggestion: "rerun the app test after editing the test protocol"}
	}
	return nil
}

func maclawAppApprovalInstanceTestEvidenceReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	if normalizeMaclawAppKind(entry.Kind) != "enterprise_approval_app" || governance == nil || !maclawAppHasPublishableTestEvidence(governance) {
		return nil
	}
	testEvidence := maclawAppTestEvidenceMap(governance)
	if testEvidence == nil {
		return nil
	}
	instance := anyMap(firstNonEmptyMaclawAppAny(testEvidence["approvalInstance"], testEvidence["approval_instance"], testEvidence["approval"]))
	instanceID := firstNonEmptyMaclawAppString(
		maclawAppStringValue(instance, "instanceId", "instance_id", "approvalInstanceId", "approval_instance_id", "workflowInstanceId", "workflow_instance_id"),
		maclawAppStringValue(testEvidence, "approvalInstanceId", "approval_instance_id", "workflowInstanceId", "workflow_instance_id"),
	)
	if instanceID == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance", Severity: "error", Message: "approval app test evidence is missing a created approval instance", Suggestion: "run the approval app in App Studio, create a test approval instance, and save its approval_instance_id in test evidence"}
	}
	status := firstNonEmptyMaclawAppString(
		maclawAppStringValue(instance, "status", "approvalStatus", "approval_status", "resultStatus", "result_status"),
		maclawAppStringValue(testEvidence, "approvalStatus", "approval_status", "resultStatus", "result_status"),
	)
	if status == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.status", Severity: "error", Message: "approval app test evidence is missing approval instance status", Suggestion: "save the status observed from the approval instance view after the App Studio test run"}
	}
	if currentNode := maclawAppStringValue(instance, "currentNode", "current_node"); currentNode == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.currentNode", Severity: "error", Message: "approval app test evidence is missing the current workflow node", Suggestion: "save the final current_node observed from the approval workflow instance after the App Studio test run"}
	}
	if workflowSkillID := maclawAppStringValue(instance, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id"); workflowSkillID == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.workflowSkillId", Severity: "error", Message: "approval app test evidence is missing the workflow Skill id", Suggestion: "save workflowSkillId/workflow_skill_id in approvalInstance evidence so the market can verify the approval workflow dependency"}
	}
	if resultStatus := firstNonEmptyMaclawAppString(maclawAppStringValue(instance, "resultStatus", "result_status"), maclawAppStringValue(instance, "businessStatus", "business_status")); resultStatus == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.resultStatus", Severity: "error", Message: "approval app test evidence is missing business/result status", Suggestion: "save businessStatus/resultStatus from the completed approval workflow instance"}
	}
	if !maclawAppApprovalInstanceHasResultPackage(instance) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.resultPayload", Severity: "error", Message: "approval app test evidence is missing the approval result package", Suggestion: "save resultPayload or outputs on approvalInstance after the approval workflow completes and DataSrv sync has been attempted"}
	}
	if !maclawAppApprovalInstanceViewVerified(testEvidence, instance) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalViews", Severity: "error", Message: "approval app test evidence has not verified the approval instance view", Suggestion: "open the generated approval app instance list and save view verification for my_requests, pending_my_approval, handled, or attention"}
	}
	return nil
}

func maclawAppApprovalInstanceHasResultPackage(instance map[string]any) bool {
	if instance == nil {
		return false
	}
	if payload := anyMap(firstNonEmptyMaclawAppAny(instance["resultPayload"], instance["result_payload"])); len(payload) > 0 {
		return true
	}
	if outputs := anySlice(instance["outputs"]); len(outputs) > 0 {
		return true
	}
	if artifacts := anySlice(instance["artifacts"]); len(artifacts) > 0 {
		return true
	}
	return false
}

func maclawAppApprovalInstanceViewVerified(testEvidence map[string]any, instance map[string]any) bool {
	if maclawAppBoolValue(testEvidence, "approvalInstanceViewVerified", "approval_instance_view_verified", "approvalViewVerified", "approval_view_verified") || maclawAppBoolValue(instance, "viewVerified", "view_verified") {
		return true
	}
	views := anyMap(firstNonEmptyMaclawAppAny(testEvidence["approvalViews"], testEvidence["approval_views"], testEvidence["instanceViews"], testEvidence["instance_views"], instance["views"]))
	if views == nil {
		return false
	}
	if maclawAppBoolValue(views, "verified", "ok") {
		return true
	}
	for _, lane := range []string{"my_requests", "pending_my_approval", "handled", "attention"} {
		if value, ok := views[lane]; ok && value != nil {
			return true
		}
	}
	return false
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
	encoded, err := maclawAppStableJSON(compactPayload(protocol))
	if err != nil {
		return ""
	}
	return maclawAppFNV1aTextHash(encoded)
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

func firstMaclawAppReviewIssueMessage(issues []maclawAppReviewIssue, fallback string) string {
	for _, issue := range issues {
		if msg := strings.TrimSpace(issue.Message); msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(fallback)
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
	if health == "missing" || health == "disabled" || health == "needs_setup" || health == "unhealthy" {
		return true
	}
	if action == "blocked" || action == "failed" || action == "optional_unhealthy" {
		return true
	}
	if status == "disabled" || status == "error" || status == "failed" {
		return true
	}
	return false
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
	app := entry.App
	if app == nil {
		return ""
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
	encoded, err := maclawAppStableJSON(payload)
	if err != nil {
		return ""
	}
	return maclawAppFNV1aTextHash(encoded)
}

func maclawAppNormalizedVersionAny(value any) int {
	if number, ok := maclawAppNumberFromAny(value); ok && number > 0 {
		return int(math.Floor(number))
	}
	return 1
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

func maclawAppFNV1aTextHash(value string) string {
	var hash uint32 = 2166136261
	for _, char := range value {
		hash ^= uint32(char)
		hash *= 16777619
	}
	return fmt.Sprintf("%08x", hash)
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
		if !dep.Required || !maclawAppDependencyBlocksInstall(dep) {
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

// validateAppDependenciesPublished checks that all required skill dependencies
// in the install plan are resolvable from Hub/SkillMarket by verifying the
// corresponding locally-installed skill has a HubSkillID. A HubSkillID is
// assigned by the server when the skill is first uploaded — its presence means
// the skill exists in the remote registry and can be found by other machines.
//
// Called at Hub upload time (SyncMaclawAppPackageSubmissionToHub), NOT at local
// submit time — the developer may submit locally first, upload the skill, then
// sync the app to Hub.
func (a *App) validateAppDependenciesPublished(plan maclawAppInstallPlan) error {
	installed := a.installedMaclawAppSkillIndex()
	var unpublished []string
	for _, dep := range plan.Dependencies {
		if !dep.Required {
			continue
		}
		// If the dependency already declares a remote source with install_ref,
		// trust it — the receiver can resolve it independently.
		src := strings.ToLower(strings.TrimSpace(dep.Source))
		if dep.InstallRef != "" && src != "" && src != "local" {
			continue
		}
		// Check if the locally installed skill has a HubSkillID (was published).
		match, found := installed[strings.ToLower(dep.ID)]
		if found && strings.TrimSpace(match.HubSkillID) != "" {
			continue // skill was published → receivers can find it by HubSkillID
		}
		unpublished = append(unpublished, dep.ID)
	}
	if len(unpublished) == 0 {
		return nil
	}
	if len(unpublished) == 1 {
		return fmt.Errorf("cannot upload App to Hub: skill dependency %q has not been published to Hub/SkillMarket. Please upload the skill first (manage_skill action=upload name=%s)", unpublished[0], unpublished[0])
	}
	return fmt.Errorf("cannot upload App to Hub: %d skill dependencies have not been published: %s. Please upload these skills first", len(unpublished), strings.Join(unpublished, ", "))
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
		match, found := installed[strings.ToLower(dep.ID)]
		if !found {
			continue
		}
		hubID := strings.TrimSpace(match.HubSkillID)
		if hubID == "" {
			continue
		}
		// Stamp InstallRef with HubSkillID for precision remote install.
		if strings.TrimSpace(dep.InstallRef) == "" {
			dep.InstallRef = hubID
		}
		// Upgrade source from "local"/empty to a resolvable remote source.
		src := strings.ToLower(strings.TrimSpace(dep.Source))
		if src == "" || src == "local" {
			dep.Source = "hub"
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
	case strings.Contains(lower, "not found") || strings.Contains(lower, "404") || strings.Contains(lower, "no such"):
		code = "not_found"
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "permission") || strings.Contains(lower, "denied") || strings.Contains(lower, "401") || strings.Contains(lower, "403"):
		code = "access_denied"
	case strings.Contains(lower, "version") && (strings.Contains(lower, "mismatch") || strings.Contains(lower, "constraint") || strings.Contains(lower, "satisf") || strings.Contains(lower, "required")):
		code = "version_mismatch"
	case strings.Contains(lower, "checksum") || strings.Contains(lower, "signature") || strings.Contains(lower, "sha256") || strings.Contains(lower, "integrity"):
		code = "package_integrity_failed"
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
	case "market", "skillmarket":
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
		if strings.TrimSpace(dep.InstallRefVersion) != "" && strings.TrimSpace(dep.Version) != "" && dep.InstallRefVersion != dep.Version {
			dep.PreflightStatus = "blocked"
			dep.PreflightCode = "version_mismatch"
			dep.PreflightStage = "install_ref"
			dep.PreflightMessage = fmt.Sprintf("install_ref version %s does not match required version %s", dep.InstallRefVersion, dep.Version)
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
		if !maclawAppDependencySupportsPublicMarketPreflight(*dep) {
			continue
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
		query := firstNonEmpty(dep.InstallRefTarget, dep.InstallRef, dep.ID)
		results, err := publicSearcher.Search(ctx, query, nil, 10)
		cancel()
		if err != nil {
			dep.PreflightStatus = "pending"
			dep.PreflightCode = "remote_preflight_unavailable"
			dep.PreflightStage = "skillmarket_preflight"
			dep.PreflightMessage = fmt.Sprintf("SkillMarket dependency %s preflight unavailable: %v", query, err)
			continue
		}
		maclawAppApplyPublicSkillMarketPreflight(dep, results)
	}
}

func maclawAppConfigHasExplicitHubCenter(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.RemoteHubCenterURL) != ""
}

func maclawAppDependencySupportsPublicMarketPreflight(dep maclawAppInstallPlanDependency) bool {
	if strings.TrimSpace(dep.ID) == "" || strings.EqualFold(dep.InstallRefKind, "github") {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(dep.Source))
	kind := strings.ToLower(strings.TrimSpace(dep.InstallRefKind))
	switch source {
	case "market", "skillmarket":
		return true
	}
	switch kind {
	case "skillmarket", "market":
		return true
	}
	return false
}

type maclawAppDependencyIntegrityMetadata struct {
	PackageSHA256      string
	PackageChecksum    string
	PackageSignature   string
	PackageDownloadURL string
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
	maclawAppApplyDependencyIntegrityMetadata(dep, maclawAppDependencyIntegrityFromSkillMarketResult(*match), "skillmarket_preflight")
	requiredVersion := strings.TrimSpace(firstNonEmpty(dep.InstallRefVersion, dep.Version, dep.RequiredVersion))
	if requiredVersion != "" && strings.TrimSpace(match.Version) != "" && strings.TrimSpace(match.Version) != requiredVersion {
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
	for _, value := range []string{dep.InstallRefTarget, dep.InstallRef, dep.ID} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			keys[value] = struct{}{}
		}
	}
	for i := range results {
		for _, value := range []string{results[i].ID, results[i].Name, results[i].InstallRef} {
			if _, ok := keys[strings.ToLower(strings.TrimSpace(value))]; ok {
				return &results[i]
			}
		}
	}
	return nil
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
	if requiredVersion != "" && availableVersion != "" && requiredVersion != availableVersion {
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
	return "id", ref, "", "ok", "install_ref resolved"
}

func maclawAppInstallRefSourceMatches(source, kind string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch source {
	case "", "local", "hub", "skillhub":
		return kind == "hub" || kind == "skillhub" || kind == "skill" || kind == "skills"
	case "market", "skillmarket":
		return kind == "market" || kind == "skillmarket"
	case "enterprise", "enterprise_hub":
		return kind == "enterprise" || kind == "enterprise_hub" || kind == "hub"
	case "github":
		return kind == "github"
	default:
		return false
	}
}
func applyResolvedDependenciesToPlan(deps []maclawAppInstallPlanDependency, installDoc map[string]any) {
	// Collect resolved entries from all available locations.
	var allResolved []interface{}
	// Source 1: package-level (local queue path).
	if topLevel, ok := installDoc["resolved_dependencies"].([]interface{}); ok {
		allResolved = append(allResolved, topLevel...)
	}
	// Source 2: entry-level (Hub download path — each entry may carry its own).
	for _, appRaw := range anySlice(installDoc["apps"]) {
		entryMap := anyMap(appRaw)
		if entryMap == nil {
			continue
		}
		if entryResolved, ok := entryMap["resolved_dependencies"].([]interface{}); ok {
			allResolved = append(allResolved, entryResolved...)
		}
	}
	if len(allResolved) == 0 {
		return
	}
	// Build a lookup from resolved entries: id → {install_ref, source, version}
	type resolvedEntry struct {
		InstallRef string
		Source     string
		Version    string
	}
	lookup := make(map[string]resolvedEntry, len(allResolved))
	for _, item := range allResolved {
		resMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id := stringFromMapSafe(resMap, "id")
		ref := stringFromMapSafe(resMap, "install_ref")
		if id == "" || ref == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, exists := lookup[key]; !exists {
			lookup[key] = resolvedEntry{
				InstallRef: ref,
				Source:     stringFromMapSafe(resMap, "source"),
				Version:    stringFromMapSafe(resMap, "version"),
			}
		}
	}
	if len(lookup) == 0 {
		return
	}
	// Apply to plan dependencies.
	for i := range deps {
		entry, ok := lookup[strings.ToLower(deps[i].ID)]
		if !ok {
			continue
		}
		if strings.TrimSpace(deps[i].InstallRef) == "" {
			deps[i].InstallRef = entry.InstallRef
		}
		if entry.Source != "" {
			src := strings.ToLower(strings.TrimSpace(deps[i].Source))
			if src == "" || src == "local" {
				deps[i].Source = entry.Source
			}
		}
		if entry.Version != "" && deps[i].Version == "" {
			deps[i].Version = entry.Version
		}
	}
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
		entryMap["resolved_dependencies"] = enrichedDeps
	}
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

func maclawAppInstallSkillSource(source string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "local", "hub", "skillhub":
		return string(skillSearchSourceSkillHub), true
	case "market", "skillmarket":
		return string(skillSearchSourceSkillMarket), true
	case "enterprise", "enterprise_hub":
		return string(skillSearchSourceEnterpriseHub), true
	case "github":
		return string(skillSearchSourceGitHub), true
	default:
		return "", false
	}
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

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
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

func normalizeMaclawAppApprovalCurrentNodes(currentNode string, currentNodeIDs []string) (string, []string) {
	primary := strings.TrimSpace(currentNode)
	seen := map[string]struct{}{}
	nodes := make([]string, 0, len(currentNodeIDs)+1)
	for _, node := range currentNodeIDs {
		node = strings.TrimSpace(node)
		if node == "" {
			continue
		}
		key := strings.ToLower(node)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		nodes = append(nodes, node)
	}
	if primary != "" {
		key := strings.ToLower(primary)
		if _, ok := seen[key]; !ok {
			nodes = append([]string{primary}, nodes...)
		}
	}
	if primary == "" && len(nodes) > 0 {
		primary = nodes[0]
	}
	if len(nodes) == 0 && primary != "" {
		nodes = []string{primary}
	}
	return primary, nodes
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
func (a *App) appendMaclawAppSubmission(record maclawAppSubmissionRecord) error {
	queue, err := a.readMaclawAppSubmissionQueue()
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
	return a.writeMaclawAppSubmissionQueue(queue)
}

func (a *App) writeMaclawAppSubmissionQueue(queue maclawAppSubmissionQueue) error {
	path := a.maclawAppSubmissionQueuePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *App) readMaclawAppSubmissionQueue() (maclawAppSubmissionQueue, error) {
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

func (a *App) maclawAppInstallRegistryPath() string {
	return filepath.Join(a.GetDataDir(), "app_install_records.json")
}

func (a *App) maclawAppApprovalRegistryPath() string {
	return filepath.Join(a.GetDataDir(), "app_approval_instances.json")
}

func (a *App) readMaclawAppApprovalRegistry() (maclawAppApprovalRegistry, error) {
	path := a.maclawAppApprovalRegistryPath()
	registry := maclawAppApprovalRegistry{Schema: "maclaw.app.approvals.v1"}
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
		return registry, fmt.Errorf("decode maclaw app approval registry: %w", err)
	}
	if registry.Schema == "" {
		registry.Schema = "maclaw.app.approvals.v1"
	}
	return registry, nil
}

func (a *App) writeMaclawAppApprovalRegistry(registry maclawAppApprovalRegistry) error {
	path := a.maclawAppApprovalRegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (a *App) readMaclawAppInstallRegistry() (maclawAppInstallRegistry, error) {
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
	path := a.maclawAppInstallRegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

func normalizeMaclawAppApprovalInstanceFields(instance maclawAppApprovalInstance) maclawAppApprovalInstance {
	instance.AppID = strings.TrimSpace(instance.AppID)
	instance.AppName = strings.TrimSpace(instance.AppName)
	instance.BlueprintID = strings.TrimSpace(instance.BlueprintID)
	instance.DatasetID = strings.TrimSpace(instance.DatasetID)
	instance.ObjectRole = strings.TrimSpace(instance.ObjectRole)
	instance.ApprovalObjectRole = strings.TrimSpace(instance.ApprovalObjectRole)
	instance.ObjectRole = firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole)
	instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ApprovalObjectRole, instance.ObjectRole)
	instance.ApprovalEvent = strings.TrimSpace(instance.ApprovalEvent)
	instance.ApprovalWorkflowID = strings.TrimSpace(instance.ApprovalWorkflowID)
	instance.ApprovalWorkflowID = firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID)
	instance.InstanceID = strings.TrimSpace(instance.InstanceID)
	instance.Title = strings.TrimSpace(instance.Title)
	instance.Lane = strings.TrimSpace(instance.Lane)
	instance.Status = strings.TrimSpace(instance.Status)
	instance.CurrentNode, instance.CurrentNodeIDs = normalizeMaclawAppApprovalCurrentNodes(instance.CurrentNode, firstNonEmptyMaclawAppStringList(instance.CurrentNodeIDs, instance.WorkflowNodeIDs))
	_, instance.WorkflowNodeIDs = normalizeMaclawAppApprovalCurrentNodes(instance.CurrentNode, firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs))
	instance.Owner = strings.TrimSpace(instance.Owner)
	instance.Applicant = strings.TrimSpace(instance.Applicant)
	instance.Owner = firstNonEmptyMaclawAppString(instance.Owner, instance.Applicant)
	instance.Applicant = firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner)
	instance.Approver = strings.TrimSpace(instance.Approver)
	instance.CurrentAssignee = strings.TrimSpace(instance.CurrentAssignee)
	instance.CurrentAssignee = firstNonEmptyMaclawAppString(instance.CurrentAssignee, instance.Approver)
	instance.CurrentAssigneeType = strings.TrimSpace(instance.CurrentAssigneeType)
	if instance.CurrentAssigneeType == "" && instance.CurrentAssignee != "" {
		instance.CurrentAssigneeType = "user"
	}
	instance.CreatedAt = strings.TrimSpace(instance.CreatedAt)
	instance.UpdatedAt = strings.TrimSpace(instance.UpdatedAt)
	instance.Result = strings.TrimSpace(instance.Result)
	instance.WorkflowSkillID = strings.TrimSpace(instance.WorkflowSkillID)
	instance.ApprovalWorkflowID = firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID)
	instance.WorkflowVersion = strings.TrimSpace(instance.WorkflowVersion)
	instance.BusinessStatus = strings.TrimSpace(instance.BusinessStatus)
	instance.ResultStatus = strings.TrimSpace(instance.ResultStatus)
	instance.FromStatus = strings.TrimSpace(instance.FromStatus)
	instance.ToStatus = strings.TrimSpace(instance.ToStatus)
	instance.ToStatus = firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status)
	instance.WorkflowDecisionID = strings.TrimSpace(instance.WorkflowDecisionID)
	instance.RecordID = strings.TrimSpace(instance.RecordID)
	instance.ApprovalID = strings.TrimSpace(instance.ApprovalID)
	instance.RecordApprovalID = strings.TrimSpace(instance.RecordApprovalID)
	instance.ApprovalID = firstNonEmptyMaclawAppString(instance.ApprovalID, instance.RecordApprovalID)
	instance.RecordApprovalID = firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID)
	instance.DetailURL = strings.TrimSpace(instance.DetailURL)
	instance.BusinessEntity = strings.TrimSpace(instance.BusinessEntity)
	instance.BusinessAction = strings.TrimSpace(instance.BusinessAction)
	instance.BusinessNote = strings.TrimSpace(instance.BusinessNote)
	return instance
}

func mergeMaclawAppApprovalInstance(existing, incoming maclawAppApprovalInstance) maclawAppApprovalInstance {
	if normalizeMaclawAppApprovalStatus(incoming.Status) == "pending" && normalizeMaclawAppApprovalLane(existing.Lane) == "pending_my_approval" && normalizeMaclawAppApprovalLane(incoming.Lane) == "my_requests" {
		incoming.Lane = existing.Lane
	}
	incoming.AppName = firstNonEmptyMaclawAppString(incoming.AppName, existing.AppName)
	incoming.BlueprintID = firstNonEmptyMaclawAppString(incoming.BlueprintID, existing.BlueprintID)
	incoming.DatasetID = firstNonEmptyMaclawAppString(incoming.DatasetID, existing.DatasetID)
	incoming.ObjectRole = firstNonEmptyMaclawAppString(incoming.ObjectRole, existing.ObjectRole, existing.ApprovalObjectRole)
	incoming.ApprovalObjectRole = firstNonEmptyMaclawAppString(incoming.ApprovalObjectRole, incoming.ObjectRole, existing.ApprovalObjectRole)
	incoming.ApprovalEvent = firstNonEmptyMaclawAppString(incoming.ApprovalEvent, existing.ApprovalEvent)
	incoming.ApprovalWorkflowID = firstNonEmptyMaclawAppString(incoming.ApprovalWorkflowID, existing.ApprovalWorkflowID, incoming.WorkflowSkillID, existing.WorkflowSkillID)
	incoming.Owner = firstNonEmptyMaclawAppString(incoming.Owner, existing.Owner, existing.Applicant)
	incoming.Applicant = firstNonEmptyMaclawAppString(incoming.Applicant, incoming.Owner, existing.Applicant)
	incoming.Approver = firstNonEmptyMaclawAppString(incoming.Approver, existing.Approver)
	incoming.CurrentAssignee = firstNonEmptyMaclawAppString(incoming.CurrentAssignee, existing.CurrentAssignee, incoming.Approver, existing.Approver)
	incoming.CurrentAssigneeType = firstNonEmptyMaclawAppString(incoming.CurrentAssigneeType, existing.CurrentAssigneeType)
	incoming.CreatedAt = firstNonEmptyMaclawAppString(incoming.CreatedAt, existing.CreatedAt)
	incoming.WorkflowSkillID = firstNonEmptyMaclawAppString(incoming.WorkflowSkillID, existing.WorkflowSkillID)
	incoming.WorkflowVersion = firstNonEmptyMaclawAppString(incoming.WorkflowVersion, existing.WorkflowVersion)
	incoming.FromStatus = firstNonEmptyMaclawAppString(incoming.FromStatus, existing.FromStatus)
	incoming.ToStatus = firstNonEmptyMaclawAppString(incoming.ToStatus, existing.ToStatus, incoming.BusinessStatus, incoming.Status)
	incoming.WorkflowDecisionID = firstNonEmptyMaclawAppString(incoming.WorkflowDecisionID, existing.WorkflowDecisionID)
	incoming.RecordID = firstNonEmptyMaclawAppString(incoming.RecordID, existing.RecordID)
	incoming.ApprovalID = firstNonEmptyMaclawAppString(incoming.ApprovalID, existing.ApprovalID, existing.RecordApprovalID)
	incoming.RecordApprovalID = firstNonEmptyMaclawAppString(incoming.RecordApprovalID, incoming.ApprovalID, existing.RecordApprovalID)
	incoming.DetailURL = firstNonEmptyMaclawAppString(incoming.DetailURL, existing.DetailURL)
	incoming.BusinessEntity = firstNonEmptyMaclawAppString(incoming.BusinessEntity, existing.BusinessEntity)
	incoming.BusinessAction = firstNonEmptyMaclawAppString(incoming.BusinessAction, existing.BusinessAction)
	incoming.BusinessNote = firstNonEmptyMaclawAppString(incoming.BusinessNote, existing.BusinessNote)
	if incoming.ResultPayload == nil {
		incoming.ResultPayload = cloneMapAny(existing.ResultPayload)
	}
	if len(incoming.Outputs) == 0 {
		incoming.Outputs = cloneMaclawAppApprovalOutputs(existing.Outputs)
	}
	if len(incoming.Artifacts) == 0 {
		incoming.Artifacts = append([]maclawAppApprovalArtifact(nil), existing.Artifacts...)
	}
	if len(incoming.Events) == 0 {
		incoming.Events = append([]maclawAppApprovalEvent(nil), existing.Events...)
	}
	return normalizeMaclawAppApprovalInstanceFields(incoming)
}

func normalizeMaclawAppApprovalLane(lane string) string {
	lane = strings.ToLower(strings.TrimSpace(lane))
	switch lane {
	case "pending", "pending_my_approval", "handled", "attention":
		return lane
	default:
		return "my_requests"
	}
}

func normalizeMaclawAppApprovalLaneFilter(lane string) string {
	lane = strings.ToLower(strings.TrimSpace(lane))
	switch lane {
	case "my_requests", "pending_my_approval", "handled", "attention", "all":
		return lane
	default:
		return "all"
	}
}

func normalizeMaclawAppApprovalStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "draft", "pending", "approved", "rejected", "attention", "cancelled", "timeout":
		return status
	default:
		return "pending"
	}
}

func normalizeMaclawAppApprovalStatusForRecordApproval(item contract.RecordApproval) string {
	if strings.TrimSpace(item.Kind) == "attention" || strings.TrimSpace(item.BusinessStatus) == "attention" || strings.TrimSpace(item.ResultStatus) == "attention" {
		return "attention"
	}
	return normalizeMaclawAppApprovalStatus(firstNonEmptyMaclawAppString(item.Decision, item.Status, item.ResultStatus, item.BusinessStatus))
}

func cloneMaclawAppApprovalInstance(instance maclawAppApprovalInstance) maclawAppApprovalInstance {
	instance.CurrentNodeIDs = append([]string(nil), instance.CurrentNodeIDs...)
	instance.WorkflowNodeIDs = append([]string(nil), instance.WorkflowNodeIDs...)
	instance.Events = append([]maclawAppApprovalEvent(nil), instance.Events...)
	instance.ResultPayload = cloneMapAny(instance.ResultPayload)
	instance.Outputs = cloneMaclawAppApprovalOutputs(instance.Outputs)
	instance.Artifacts = append([]maclawAppApprovalArtifact(nil), instance.Artifacts...)
	return instance
}

func cloneMaclawAppApprovalOutputs(outputs []maclawAppApprovalOutput) []maclawAppApprovalOutput {
	if len(outputs) == 0 {
		return nil
	}
	cloned := make([]maclawAppApprovalOutput, len(outputs))
	for i, output := range outputs {
		cloned[i] = output
		cloned[i].Data = cloneMapAny(output.Data)
		if output.Artifact != nil {
			artifact := *output.Artifact
			cloned[i].Artifact = &artifact
		}
	}
	return cloned
}

func normalizeMaclawAppSubmissionStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "submitted", "pending_review", "review_failed", "approved", "published", "deprecated", "revoked":
		return strings.TrimSpace(status)
	default:
		return ""
	}
}
