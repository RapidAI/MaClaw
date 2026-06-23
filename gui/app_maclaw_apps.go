package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	contract "github.com/RapidAI/CodeClaw/corelib/structureddata"
)

type maclawAppSubmissionRecord struct {
	SubmissionID   string                           `json:"submission_id"`
	SubmittedAt    string                           `json:"submitted_at"`
	Status         string                           `json:"status"`
	Channel        string                           `json:"channel"`
	AppIDs         []string                         `json:"app_ids"`
	AppNames       []string                         `json:"app_names,omitempty"`
	PackageSHA     string                           `json:"package_sha256,omitempty"`
	PackageSize    int                              `json:"package_bytes,omitempty"`
	ReviewedAt     string                           `json:"reviewed_at,omitempty"`
	PublishedAt    string                           `json:"published_at,omitempty"`
	Reviewer       string                           `json:"reviewer,omitempty"`
	RiskLevel      string                           `json:"risk_level,omitempty"`
	ApprovedScopes []string                         `json:"approved_scopes,omitempty"`
	ReviewIssues   []maclawAppReviewIssue           `json:"review_issues,omitempty"`
	Dependencies   []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	Events         []maclawAppSubmissionEvent       `json:"events,omitempty"`
	Package        map[string]any                   `json:"package"`
	Message        string                           `json:"message"`
}

type maclawAppSubmissionQueue struct {
	Schema      string                      `json:"schema"`
	UpdatedAt   string                      `json:"updated_at"`
	Submissions []maclawAppSubmissionRecord `json:"submissions"`
}

type maclawAppSubmissionSummary struct {
	SubmissionID   string                           `json:"submission_id"`
	SubmittedAt    string                           `json:"submitted_at"`
	Status         string                           `json:"status"`
	Channel        string                           `json:"channel"`
	AppIDs         []string                         `json:"app_ids"`
	AppNames       []string                         `json:"app_names,omitempty"`
	PackageSHA     string                           `json:"package_sha256,omitempty"`
	PackageSize    int                              `json:"package_bytes,omitempty"`
	ReviewedAt     string                           `json:"reviewed_at,omitempty"`
	PublishedAt    string                           `json:"published_at,omitempty"`
	Reviewer       string                           `json:"reviewer,omitempty"`
	RiskLevel      string                           `json:"risk_level,omitempty"`
	ApprovedScopes []string                         `json:"approved_scopes,omitempty"`
	ReviewIssues   []maclawAppReviewIssue           `json:"review_issues,omitempty"`
	Dependencies   []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	EventCount     int                              `json:"event_count,omitempty"`
	LastEventAt    string                           `json:"last_event_at,omitempty"`
	Message        string                           `json:"message"`
}

type maclawAppReviewIssue struct {
	Path       string `json:"path,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
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
	Status         string                 `json:"status"`
	Channel        string                 `json:"channel"`
	Message        string                 `json:"message"`
	SubmissionID   string                 `json:"submission_id"`
	ReviewedAt     string                 `json:"reviewed_at"`
	PublishedAt    string                 `json:"published_at"`
	Reviewer       string                 `json:"reviewer"`
	RiskLevel      string                 `json:"risk_level"`
	ApprovedScopes []string               `json:"approved_scopes"`
	ReviewIssues   []maclawAppReviewIssue `json:"review_issues"`
}

type maclawAppInstallPlan struct {
	Schema                string                           `json:"schema"`
	Apps                  []maclawAppInstallPlanApp        `json:"apps"`
	Dependencies          []maclawAppInstallPlanDependency `json:"dependencies"`
	HasMissingRequired    bool                             `json:"has_missing_required"`
	HasBlockingDependency bool                             `json:"has_blocking_dependency,omitempty"`
}

type maclawAppInstallPlanApp struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Schema string `json:"schema,omitempty"`
}

type maclawAppInstallPlanDependency struct {
	ID              string   `json:"id"`
	Version         string   `json:"version,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Required        bool     `json:"required"`
	Source          string   `json:"source,omitempty"`
	AppIDs          []string `json:"app_ids,omitempty"`
	Installed       bool     `json:"installed"`
	InstalledName   string   `json:"installed_name,omitempty"`
	InstalledDir    string   `json:"installed_dir,omitempty"`
	InstalledStatus string   `json:"installed_status,omitempty"`
	Health          string   `json:"health,omitempty"`
	Action          string   `json:"action"`
	Message         string   `json:"message,omitempty"`
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
	AppID                 string                           `json:"app_id"`
	AppName               string                           `json:"app_name,omitempty"`
	Kind                  string                           `json:"kind,omitempty"`
	Source                string                           `json:"source,omitempty"`
	InstalledAt           string                           `json:"installed_at"`
	PackageSHA            string                           `json:"package_sha256,omitempty"`
	PackageSize           int                              `json:"package_bytes,omitempty"`
	Dependencies          []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	VersionSnapshot       maclawAppInstallVersionSnapshot  `json:"version_snapshot,omitempty"`
	HasMissingRequired    bool                             `json:"has_missing_required"`
	HasBlockingDependency bool                             `json:"has_blocking_dependency,omitempty"`
	Message               string                           `json:"message,omitempty"`
}

type maclawAppInstallRegistry struct {
	Schema    string                   `json:"schema"`
	UpdatedAt string                   `json:"updated_at"`
	Installs  []maclawAppInstallRecord `json:"installs"`
}

type maclawAppApprovalInstance struct {
	AppID              string                      `json:"app_id"`
	AppName            string                      `json:"app_name,omitempty"`
	BlueprintID        string                      `json:"blueprint_id,omitempty"`
	DatasetID          string                      `json:"dataset_id,omitempty"`
	ObjectRole         string                      `json:"object_role,omitempty"`
	ApprovalObjectRole string                      `json:"approval_object_role,omitempty"`
	ApprovalEvent      string                      `json:"approval_event,omitempty"`
	InstanceID         string                      `json:"instance_id"`
	Title              string                      `json:"title"`
	Lane               string                      `json:"lane"`
	Status             string                      `json:"status"`
	CurrentNode        string                      `json:"current_node"`
	Owner              string                      `json:"owner"`
	Applicant          string                      `json:"applicant,omitempty"`
	Approver           string                      `json:"approver"`
	CreatedAt          string                      `json:"created_at,omitempty"`
	UpdatedAt          string                      `json:"updated_at"`
	Result             string                      `json:"result"`
	WorkflowSkillID    string                      `json:"workflow_skill_id,omitempty"`
	WorkflowVersion    string                      `json:"workflow_version,omitempty"`
	BusinessStatus     string                      `json:"business_status,omitempty"`
	ResultStatus       string                      `json:"result_status,omitempty"`
	WorkflowDecisionID string                      `json:"workflow_decision_id,omitempty"`
	RecordID           string                      `json:"record_id,omitempty"`
	ApprovalID         string                      `json:"approval_id,omitempty"`
	RecordApprovalID   string                      `json:"record_approval_id,omitempty"`
	DetailURL          string                      `json:"detail_url,omitempty"`
	BusinessEntity     string                      `json:"business_entity,omitempty"`
	BusinessAction     string                      `json:"business_action,omitempty"`
	BusinessNote       string                      `json:"business_note,omitempty"`
	ResultPayload      map[string]any              `json:"result_payload,omitempty"`
	Outputs            []maclawAppApprovalOutput   `json:"outputs,omitempty"`
	Artifacts          []maclawAppApprovalArtifact `json:"artifacts,omitempty"`
	Events             []maclawAppApprovalEvent    `json:"events,omitempty"`
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

// SubmitMaclawAppPackage stores a maclaw.app.pack.v1 submission in the local
// durable queue. Enterprise Hub upload can later consume the same package JSON.
func (a *App) SubmitMaclawAppPackage(packageJSON string) (map[string]any, error) {
	pkg, appIDs, appNames, err := parseMaclawAppPackage(packageJSON)
	if err != nil {
		return nil, err
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(pkg)
	if err != nil {
		return nil, err
	}
	dependencies := maclawAppSubmissionDependenciesFromPackage(pkg)
	reviewIssues := maclawAppGovernanceReviewIssuesFromPackage(pkg)
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
		"submission_id":      submissionID,
		"submitted_at":       now,
		"status":             record.Status,
		"channel":            record.Channel,
		"app_ids":            append([]string(nil), record.AppIDs...),
		"app_names":          append([]string(nil), record.AppNames...),
		"package_sha256":     record.PackageSHA,
		"package_bytes":      record.PackageSize,
		"dependencies":       cloneMaclawAppPlanDependencies(record.Dependencies),
		"dependency_count":   len(record.Dependencies),
		"review_issues":      cloneMaclawAppReviewIssues(record.ReviewIssues),
		"review_issue_count": len(record.ReviewIssues),
		"message":            record.Message,
	}, nil
}

// PlanMaclawAppInstall returns the backend-authoritative install plan for a
// maclaw.app.v1 entry or maclaw.app.pack.v1 package.
func (a *App) PlanMaclawAppInstall(packageJSON string) (maclawAppInstallPlan, error) {
	entries, err := parseMaclawAppInstallEntries(packageJSON)
	if err != nil {
		return maclawAppInstallPlan{}, err
	}
	installed := a.installedMaclawAppSkillIndex()
	plan := maclawAppInstallPlan{
		Schema:       "maclaw.app.install_plan.v1",
		Apps:         make([]maclawAppInstallPlanApp, 0, len(entries)),
		Dependencies: []maclawAppInstallPlanDependency{},
	}
	depsByID := make(map[string]*maclawAppInstallPlanDependency)
	for _, entry := range entries {
		plan.Apps = append(plan.Apps, maclawAppInstallPlanApp{
			ID:     entry.ID,
			Name:   entry.Name,
			Kind:   normalizeMaclawAppKind(entry.Kind),
			Schema: entry.Schema,
		})
		for _, dep := range maclawAppDependenciesForEntry(entry) {
			key := strings.ToLower(dep.ID)
			existing := depsByID[key]
			if existing == nil {
				dep.AppIDs = []string{entry.ID}
				if match, ok := installed[key]; ok {
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
				depsByID[key] = &dep
				plan.Dependencies = append(plan.Dependencies, dep)
				continue
			}
			if !containsMaclawAppString(existing.AppIDs, entry.ID) {
				existing.AppIDs = append(existing.AppIDs, entry.ID)
			}
			if dep.Required && !existing.Required {
				existing.Required = true
				if !existing.Installed {
					existing.Health = "missing"
					existing.Action = "blocked"
					existing.Message = "required skill dependency is missing"
				} else if !maclawAppDependencyIsReady(*existing) {
					existing.Action = "blocked"
					existing.Message = maclawAppDependencyInactiveMessage(*existing, true)
				}
			}
		}
	}
	for i := range plan.Dependencies {
		if dep := depsByID[strings.ToLower(plan.Dependencies[i].ID)]; dep != nil {
			plan.Dependencies[i] = *dep
		}
	}
	plan.refreshMaclawAppDependencyFlags()
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
		if err := a.InstallMixedSkill(source, dep.ID, ""); err != nil {
			dep.Health = "missing"
			dep.Action = "failed"
			dep.Message = err.Error()
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
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		return nil, fmt.Errorf("cannot install MaClaw App: required Skill dependencies are missing or unavailable")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &doc); err != nil {
		return nil, fmt.Errorf("decode maclaw app package: %w", err)
	}
	packageSHA, packageSize, err := maclawAppPackageFingerprint(doc)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
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
		record := maclawAppInstallRecord{
			AppID:                 entry.ID,
			AppName:               entry.Name,
			Kind:                  normalizeMaclawAppKind(entry.Kind),
			Source:                strings.TrimSpace(source),
			InstalledAt:           now,
			PackageSHA:            packageSHA,
			PackageSize:           packageSize,
			Dependencies:          cloneMaclawAppPlanDependenciesForApp(plan.Dependencies, entry.ID),
			VersionSnapshot:       maclawAppInstallVersionSnapshotForEntry(entry),
			HasMissingRequired:    hasMissingMaclawAppRequiredDependencyForApp(plan.Dependencies, entry.ID),
			HasBlockingDependency: hasBlockingMaclawAppRequiredDependencyForApp(plan.Dependencies, entry.ID),
			Message:               "installed locally",
		}
		registry.upsert(record)
	}
	registry.UpdatedAt = now
	if err := a.writeMaclawAppInstallRegistry(registry); err != nil {
		return nil, err
	}
	dataSrvRegistration := a.registerMaclawAppInstallationsToDataSrv(entries, source, packageSHA, packageSize, plan.Dependencies)
	return map[string]any{
		"schema":                  registry.Schema,
		"installed_at":            now,
		"app_count":               len(entries),
		"package_sha":             packageSHA,
		"package_bytes":           packageSize,
		"dependencies":            cloneMaclawAppPlanDependencies(plan.Dependencies),
		"app_versions":            maclawAppInstallVersionSnapshotsByApp(entries),
		"has_missing_required":    plan.HasMissingRequired,
		"has_blocking_dependency": plan.HasBlockingDependency,
		"datasrv_registration":    dataSrvRegistration,
	}, nil
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
	return map[string]any{
		"synced":          true,
		"mode":            mode,
		"target":          target,
		"app_id":          strings.TrimSpace(input.AppID),
		"dataset_id":      strings.TrimSpace(input.DatasetID),
		"object_role":     strings.TrimSpace(input.ObjectRole),
		"business_action": strings.TrimSpace(input.BusinessAction),
		"result_status":   firstNonEmptyMaclawAppString(stringMapValue(response, "result_status"), stringMapValue(response, "status"), "done"),
		"response":        response,
	}, nil
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
	if instance.Status == "attention" {
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		if input.ApprovalID != "" {
			instance.ApprovalID = input.ApprovalID
			instance.RecordApprovalID = input.ApprovalID
			_, _ = a.RecordMaclawAppApprovalInstance(instance)
		}
		return map[string]any{"synced": true, "action": "attention_view_only", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "reason": "attention is view-only and does not review the DataSrv approval", "business_record_sync": businessRecordSync}, nil
	}
	if input.ApprovalID == "" && maclawAppApprovalStatusCanReview(instance.Status) {
		input.ApprovalID = a.findMaclawAppRecordApprovalID(input, instance)
	}
	if input.ApprovalID != "" && maclawAppApprovalStatusCanReview(instance.Status) {
		out := a.executeMISDataTool(map[string]interface{}{
			"action":               "review_record_approval",
			"approval_id":          input.ApprovalID,
			"decision":             instance.Status,
			"reason":               instance.Result,
			"workflow_node_id":     instance.CurrentNode,
			"workflow_decision_id": instance.WorkflowDecisionID,
			"workflow_version":     instance.WorkflowVersion,
			"business_status":      firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"result_status":        firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"result_payload":       cloneMapAny(instance.ResultPayload),
			"outputs":              cloneMaclawAppApprovalOutputs(instance.Outputs),
			"artifacts":            append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
		})
		instance.ApprovalID = input.ApprovalID
		instance.RecordApprovalID = input.ApprovalID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		return map[string]any{"synced": true, "action": "review_record_approval", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "response": out, "business_record_sync": businessRecordSync}, nil
	}
	businessRecordSync := a.syncMaclawAppApprovalBusinessRecordForApproval(input.DatasetID, input.RecordID, instance, true)
	out := a.executeMISDataTool(map[string]interface{}{
		"action":       "create_record_approval",
		"dataset_id":   input.DatasetID,
		"object_role":  input.ObjectRole,
		"app_id":       input.AppID,
		"blueprint_id": input.BlueprintID,
		"record_id":    input.RecordID,
		"kind":         "approval",
		"summary":      instance.Title,
		"assigned_to":  instance.Approver,
		"request": compactPayload(map[string]interface{}{
			"app_id":               input.AppID,
			"blueprint_id":         input.BlueprintID,
			"dataset_id":           input.DatasetID,
			"object_role":          input.ObjectRole,
			"approval_instance_id": instance.InstanceID,
			"owner":                instance.Owner,
			"applicant":            instance.Applicant,
			"business_entity":      instance.BusinessEntity,
			"business_action":      instance.BusinessAction,
			"business_note":        instance.BusinessNote,
			"result":               instance.Result,
		}),
		"workflow_skill_id":    instance.WorkflowSkillID,
		"workflow_version":     instance.WorkflowVersion,
		"workflow_instance_id": instance.InstanceID,
		"workflow_node_id":     instance.CurrentNode,
		"workflow_decision_id": instance.WorkflowDecisionID,
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

func (a *App) findMaclawAppRecordApprovalID(input maclawAppApprovalDataSrvSyncInput, instance maclawAppApprovalInstance) string {
	out := a.executeMISDataTool(map[string]interface{}{
		"action":               "list_record_approvals",
		"dataset_id":           input.DatasetID,
		"record_id":            input.RecordID,
		"app_id":               input.AppID,
		"blueprint_id":         input.BlueprintID,
		"object_role":          input.ObjectRole,
		"workflow_instance_id": instance.InstanceID,
		"status":               "pending",
		"limit":                1,
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
		"app_id":                instance.AppID,
		"app_name":              instance.AppName,
		"blueprint_id":          instance.BlueprintID,
		"object_role":           firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole),
		"approval_event":        instance.ApprovalEvent,
		"approval_instance_id":  instance.InstanceID,
		"approval_status":       instance.Status,
		"approval_lane":         maclawAppApprovalBusinessRecordLane(instance),
		"approval_current_node": instance.CurrentNode,
		"workflow_skill_id":     instance.WorkflowSkillID,
		"workflow_instance_id":  instance.InstanceID,
		"workflow_node_id":      instance.CurrentNode,
		"workflow_decision_id":  instance.WorkflowDecisionID,
		"business_entity":       instance.BusinessEntity,
		"business_action":       instance.BusinessAction,
		"business_note":         instance.BusinessNote,
		"status":                firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"business_status":       firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"result_status":         firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
		"owner":                 instance.Owner,
		"applicant":             instance.Applicant,
		"approver":              instance.Approver,
	})
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
		if lane == "all" || lane == "" || instance.Lane == lane {
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
		instance := maclawAppApprovalInstanceFromRecordApproval(item, lane)
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

func maclawAppApprovalInstanceFromRecordApproval(item contract.RecordApproval, requestedLane string) maclawAppApprovalInstance {
	request := cloneMapAny(item.Request)
	resultPayload := cloneMapAny(item.ResultPayload)
	instanceID := firstNonEmptyMaclawAppString(item.WorkflowInstanceID, stringMapValue(request, "approval_instance_id"), item.ID)
	status := normalizeMaclawAppApprovalStatusForRecordApproval(item)
	lane := normalizeMaclawAppApprovalLaneForRecordApproval(item, requestedLane, status)
	result := firstNonEmptyMaclawAppString(item.Reason, stringMapValue(resultPayload, "text"), stringMapValue(resultPayload, "summary"), item.ResultStatus, item.BusinessStatus, status)
	instance := maclawAppApprovalInstance{
		AppID:              firstNonEmptyMaclawAppString(item.AppID, stringMapValue(request, "app_id")),
		BlueprintID:        firstNonEmptyMaclawAppString(item.BlueprintID, stringMapValue(request, "blueprint_id")),
		DatasetID:          item.DatasetID,
		ObjectRole:         firstNonEmptyMaclawAppString(item.ObjectRole, stringMapValue(request, "object_role")),
		ApprovalObjectRole: firstNonEmptyMaclawAppString(item.ObjectRole, stringMapValue(request, "object_role")),
		ApprovalEvent:      stringMapValue(request, "approval_event"),
		InstanceID:         instanceID,
		Title:              firstNonEmptyMaclawAppString(item.Summary, stringMapValue(request, "title"), instanceID, item.ID),
		Lane:               lane,
		Status:             status,
		CurrentNode:        item.WorkflowNodeID,
		Owner:              firstNonEmptyMaclawAppString(item.CreatedBy, stringMapValue(request, "owner"), stringMapValue(request, "applicant")),
		Applicant:          firstNonEmptyMaclawAppString(stringMapValue(request, "applicant"), item.CreatedBy),
		Approver:           firstNonEmptyMaclawAppString(item.AssignedTo, item.ReviewedBy),
		CreatedAt:          maclawAppApprovalTimeString(item.CreatedAt),
		UpdatedAt:          maclawAppApprovalTimeString(item.UpdatedAt),
		Result:             result,
		WorkflowSkillID:    item.WorkflowSkillID,
		WorkflowVersion:    item.WorkflowVersion,
		BusinessStatus:     item.BusinessStatus,
		ResultStatus:       item.ResultStatus,
		WorkflowDecisionID: item.WorkflowDecisionID,
		RecordID:           item.RecordID,
		ApprovalID:         item.ID,
		RecordApprovalID:   item.ID,
		BusinessEntity:     stringMapValue(request, "business_entity"),
		BusinessAction:     stringMapValue(request, "business_action"),
		BusinessNote:       stringMapValue(request, "business_note"),
		ResultPayload:      resultPayload,
		Outputs:            maclawAppApprovalOutputsFromRecordApprovals(item.Outputs),
		Artifacts:          maclawAppApprovalArtifactsFromRecordApprovals(item.Artifacts),
	}
	if instance.UpdatedAt == "" {
		instance.UpdatedAt = instance.CreatedAt
	}
	return normalizeMaclawAppApprovalInstanceFields(instance)
}

func normalizeMaclawAppApprovalLaneForRecordApproval(item contract.RecordApproval, requestedLane string, status string) string {
	if lane := normalizeMaclawAppApprovalLaneFilter(requestedLane); lane != "" && lane != "all" {
		return lane
	}
	if strings.TrimSpace(item.Kind) == "attention" || strings.TrimSpace(item.BusinessStatus) == "attention" || strings.TrimSpace(item.ResultStatus) == "attention" || status == "attention" {
		return "attention"
	}
	switch status {
	case "approved", "rejected", "cancelled", "timeout":
		return "handled"
	default:
		return "pending_my_approval"
	}
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
		if instance.Lane == lane {
			out = append(out, instance)
		}
	}
	return out
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
		for _, id := range []string{def.Name, def.DirName, def.HubSkillID} {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			index[strings.ToLower(id)] = def
		}
	}
	return index
}

func applyMaclawAppInstalledSkillDependency(dep *maclawAppInstallPlanDependency, match NLSkillDefinition) {
	dep.Installed = true
	dep.InstalledName = match.Name
	dep.InstalledDir = match.SkillDir
	dep.InstalledStatus, dep.Health = maclawAppInstalledSkillStatus(match)
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

func maclawAppInstallVersionSnapshotForEntry(entry parsedMaclawAppEntry) maclawAppInstallVersionSnapshot {
	snapshot := maclawAppInstallVersionSnapshot{
		AppEntryVersion: maclawAppVersionString(firstNonEmptyMaclawAppAny(entry.App["version"], entry.Entry["version"])),
	}
	if appSkill := maclawAppAppSkillBlockForEntry(entry); appSkill != nil {
		if id := maclawAppStringValue(appSkill, "id"); id != "" {
			skill := maclawAppInstallSkillVersionSnapshot{
				ID:      id,
				Version: maclawAppVersionString(firstNonEmptyMaclawAppAny(appSkill["version"], appSkill["version_constraint"], appSkill["versionConstraint"])),
				Kind:    firstNonEmptyMaclawAppString(maclawAppStringValue(appSkill, "kind"), "runtime_skill"),
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
		if appSkill := maclawAppAppSkillBlockForEntry(entry); appSkill != nil {
			metadata["app_skill_id"] = maclawAppStringValue(appSkill, "id")
			metadata["app_skill_version"] = maclawAppStringValue(appSkill, "version")
		}
		if workflowSkillIDs := maclawAppWorkflowSkillIDsForEntry(entry); len(workflowSkillIDs) > 0 {
			metadata["workflow_skill_ids"] = workflowSkillIDs
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

func maclawAppWorkspaceLayoutMetadataForEntry(entry parsedMaclawAppEntry) map[string]interface{} {
	var ui map[string]any
	for _, holder := range maclawAppBindingHolders(entry) {
		if candidate := anyMap(holder["ui"]); candidate != nil {
			ui = candidate
			break
		}
	}
	if ui == nil {
		return nil
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
			out["primary_region"] = primary
		}
		if output := maclawAppStringValue(layout, "outputRegion", "output_region"); output != "" {
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
		"status":           maclawAppStringValue(governance, "status"),
		"risk_level":       maclawAppStringValue(governance, "riskLevel", "risk_level"),
		"required_scopes":  governance["requiredScopes"],
		"dependencies":     governance["dependencies"],
		"workspace_layout": governance["workspaceLayout"],
		"result_contract":  firstNonEmptyMaclawAppAny(governance["resultContract"], governance["result_contract"]),
		"test_evidence":    governance["testEvidence"],
		"submission":       governance["submission"],
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
		app["ui"] = defaultUI
		return nil
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
				ID:       stringMapValue(skill, "id"),
				Version:  stringMapValue(skill, "version"),
				Kind:     "runtime_skill",
				Required: true,
				Source:   stringMapValue(skill, "source"),
			})
		}
		for _, appSkill := range []map[string]any{anyMap(holder["appSkill"]), anyMap(holder["app_skill"])} {
			if appSkill == nil {
				continue
			}
			add(maclawAppInstallPlanDependency{
				ID:       stringMapValue(appSkill, "id"),
				Version:  stringMapValue(appSkill, "version"),
				Kind:     "runtime_skill",
				Required: true,
				Source:   stringMapValue(appSkill, "source"),
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
					ID:       firstNonEmptyMISAgentView(stringMapValue(bindingMap, "workflowSkillId"), stringMapValue(bindingMap, "workflow_skill_id"), stringMapValue(bindingMap, "workflowId"), stringMapValue(bindingMap, "workflow_id")),
					Version:  firstNonEmptyMISAgentView(stringMapValue(bindingMap, "workflowVersion"), stringMapValue(bindingMap, "workflow_version")),
					Kind:     "workflow_skill",
					Required: true,
					Source:   "hub",
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
					ID:       stringMapValue(depMap, "id"),
					Version:  stringMapValue(depMap, "version"),
					Kind:     stringMapValue(depMap, "kind"),
					Required: required,
					Source:   stringMapValue(depMap, "source"),
				})
			}
		}
	}
	return deps
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
		if !maclawAppHasPublishableWorkspaceLayout(entry.App, governance) {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".governance.workspaceLayout", Severity: "error", Message: "missing workspace layout evidence", Suggestion: "save the generated UI layout in the app manifest before publishing"})
		}
		if !maclawAppHasPublishableResultContract(governance) {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".governance.resultContract", Severity: "error", Message: "missing result contract", Suggestion: "declare the app output contract before submitting to the capability market"})
		}
		if issue := maclawAppDependencyVerificationReviewIssue(entry, governance, path); issue != nil {
			issues = append(issues, *issue)
		}
		if maclawAppHasPublishableTestEvidence(governance) && maclawAppHasPublishableResultContract(governance) {
			if issue := maclawAppResultCoverageReviewIssue(governance, path); issue != nil {
				issues = append(issues, *issue)
			}
		}
		if normalizeMaclawAppKind(entry.Kind) == "enterprise_approval_app" && maclawAppWorkflowMappingForEntry(entry) == nil {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".binding.workflow", Severity: "error", Message: "missing workflow node mapping", Suggestion: "save the approval workflow node mapping in App Studio before submitting to the capability market"})
		}
	}
	return normalizeMaclawAppReviewIssues(issues)
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
	if maclawAppBoolValue(verification, "hasMissingRequired", "has_missing_required", "hasBlockingDependency", "has_blocking_dependency") {
		return &maclawAppReviewIssue{Path: appPath + ".governance.dependencyVerification", Severity: "error", Message: "required dependency is missing or blocked", Suggestion: "install or enable required Skill dependencies before submitting"}
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
		if ok, _ := coverage["ok"].(bool); ok && maclawAppCoveredResultTypesContain(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(coverage["coveredTypes"], coverage["covered_types"])), primary) {
			return nil
		}
		missing := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(coverage["missingTypes"], coverage["missing_types"]))
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

func maclawAppHasPublishableWorkspaceLayout(app map[string]any, governance map[string]any) bool {
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
			if entry != "" && regionCount > 0 {
				return true
			}
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
	return len(regions) > 0
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
func maclawAppInstallSkillSource(source string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "hub", "skillhub":
		return string(skillSearchSourceSkillHub), true
	case "market", "skillmarket":
		return string(skillSearchSourceSkillMarket), true
	case "enterprise", "enterprise_hub":
		return string(skillSearchSourceEnterpriseHub), true
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
		SubmissionID:   record.SubmissionID,
		SubmittedAt:    record.SubmittedAt,
		Status:         record.Status,
		Channel:        record.Channel,
		AppIDs:         append([]string(nil), record.AppIDs...),
		AppNames:       append([]string(nil), record.AppNames...),
		PackageSHA:     packageSHA,
		PackageSize:    packageSize,
		ReviewedAt:     record.ReviewedAt,
		PublishedAt:    record.PublishedAt,
		Reviewer:       record.Reviewer,
		RiskLevel:      record.RiskLevel,
		ApprovedScopes: append([]string(nil), record.ApprovedScopes...),
		ReviewIssues:   cloneMaclawAppReviewIssues(record.ReviewIssues),
		Dependencies:   cloneMaclawAppPlanDependencies(record.Dependencies),
		EventCount:     eventCount,
		LastEventAt:    lastEventAt,
		Message:        record.Message,
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
		if !containsMaclawAppString(dep.AppIDs, appID) {
			continue
		}
		dep.AppIDs = append([]string(nil), dep.AppIDs...)
		out = append(out, dep)
	}
	return out
}

func hasMissingMaclawAppRequiredDependencyForApp(deps []maclawAppInstallPlanDependency, appID string) bool {
	for _, dep := range deps {
		if dep.Required && !dep.Installed && containsMaclawAppString(dep.AppIDs, appID) {
			return true
		}
	}
	return false
}

func hasBlockingMaclawAppRequiredDependencyForApp(deps []maclawAppInstallPlanDependency, appID string) bool {
	for _, dep := range deps {
		if containsMaclawAppString(dep.AppIDs, appID) && maclawAppDependencyBlocksInstall(dep) {
			return true
		}
	}
	return false
}
func cloneMaclawAppReviewIssues(issues []maclawAppReviewIssue) []maclawAppReviewIssue {
	if len(issues) == 0 {
		return nil
	}
	return append([]maclawAppReviewIssue(nil), issues...)
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
	instance.InstanceID = strings.TrimSpace(instance.InstanceID)
	instance.Title = strings.TrimSpace(instance.Title)
	instance.Lane = strings.TrimSpace(instance.Lane)
	instance.Status = strings.TrimSpace(instance.Status)
	instance.CurrentNode = strings.TrimSpace(instance.CurrentNode)
	instance.Owner = strings.TrimSpace(instance.Owner)
	instance.Applicant = strings.TrimSpace(instance.Applicant)
	instance.Owner = firstNonEmptyMaclawAppString(instance.Owner, instance.Applicant)
	instance.Applicant = firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner)
	instance.Approver = strings.TrimSpace(instance.Approver)
	instance.CreatedAt = strings.TrimSpace(instance.CreatedAt)
	instance.UpdatedAt = strings.TrimSpace(instance.UpdatedAt)
	instance.Result = strings.TrimSpace(instance.Result)
	instance.WorkflowSkillID = strings.TrimSpace(instance.WorkflowSkillID)
	instance.WorkflowVersion = strings.TrimSpace(instance.WorkflowVersion)
	instance.BusinessStatus = strings.TrimSpace(instance.BusinessStatus)
	instance.ResultStatus = strings.TrimSpace(instance.ResultStatus)
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
	incoming.AppName = firstNonEmptyMaclawAppString(incoming.AppName, existing.AppName)
	incoming.BlueprintID = firstNonEmptyMaclawAppString(incoming.BlueprintID, existing.BlueprintID)
	incoming.DatasetID = firstNonEmptyMaclawAppString(incoming.DatasetID, existing.DatasetID)
	incoming.ObjectRole = firstNonEmptyMaclawAppString(incoming.ObjectRole, existing.ObjectRole, existing.ApprovalObjectRole)
	incoming.ApprovalObjectRole = firstNonEmptyMaclawAppString(incoming.ApprovalObjectRole, incoming.ObjectRole, existing.ApprovalObjectRole)
	incoming.ApprovalEvent = firstNonEmptyMaclawAppString(incoming.ApprovalEvent, existing.ApprovalEvent)
	incoming.Owner = firstNonEmptyMaclawAppString(incoming.Owner, existing.Owner, existing.Applicant)
	incoming.Applicant = firstNonEmptyMaclawAppString(incoming.Applicant, incoming.Owner, existing.Applicant)
	incoming.Approver = firstNonEmptyMaclawAppString(incoming.Approver, existing.Approver)
	incoming.CreatedAt = firstNonEmptyMaclawAppString(incoming.CreatedAt, existing.CreatedAt)
	incoming.WorkflowSkillID = firstNonEmptyMaclawAppString(incoming.WorkflowSkillID, existing.WorkflowSkillID)
	incoming.WorkflowVersion = firstNonEmptyMaclawAppString(incoming.WorkflowVersion, existing.WorkflowVersion)
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
	switch strings.TrimSpace(lane) {
	case "pending_my_approval", "handled", "attention":
		return strings.TrimSpace(lane)
	default:
		return "my_requests"
	}
}

func normalizeMaclawAppApprovalLaneFilter(lane string) string {
	switch strings.TrimSpace(lane) {
	case "my_requests", "pending_my_approval", "handled", "attention", "all":
		return strings.TrimSpace(lane)
	default:
		return "all"
	}
}

func normalizeMaclawAppApprovalStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "draft", "pending", "approved", "rejected", "attention", "cancelled", "timeout":
		return strings.TrimSpace(status)
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
	case "submitted", "review_failed", "approved", "published", "deprecated", "revoked":
		return strings.TrimSpace(status)
	default:
		return ""
	}
}
