package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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

type maclawAppInstallRecord struct {
	AppID                 string                           `json:"app_id"`
	AppName               string                           `json:"app_name,omitempty"`
	Kind                  string                           `json:"kind,omitempty"`
	Source                string                           `json:"source,omitempty"`
	InstalledAt           string                           `json:"installed_at"`
	PackageSHA            string                           `json:"package_sha256,omitempty"`
	PackageSize           int                              `json:"package_bytes,omitempty"`
	Dependencies          []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
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
		Package:      pkg,
		Message:      "queued locally for enterprise market sync",
	}
	record.Events = append(record.Events, record.maclawAppSubmissionEvent(now))
	if err := a.appendMaclawAppSubmission(record); err != nil {
		return nil, err
	}
	return map[string]any{
		"submission_id":    submissionID,
		"submitted_at":     now,
		"status":           record.Status,
		"channel":          record.Channel,
		"app_ids":          append([]string(nil), record.AppIDs...),
		"app_names":        append([]string(nil), record.AppNames...),
		"package_sha256":   record.PackageSHA,
		"package_bytes":    record.PackageSize,
		"dependencies":     cloneMaclawAppPlanDependencies(record.Dependencies),
		"dependency_count": len(record.Dependencies),
		"message":          record.Message,
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
		"schema":               registry.Schema,
		"installed_at":         now,
		"app_count":            len(entries),
		"package_sha":          packageSHA,
		"package_bytes":        packageSize,
		"datasrv_registration": dataSrvRegistration,
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
	businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
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
	patch := maclawAppApprovalBusinessRecordPatch(instance)
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
			return patch
		}
	}
	for _, output := range instance.Outputs {
		outputKind := strings.ToLower(strings.TrimSpace(firstNonEmptyMaclawAppString(output.Kind, output.Type)))
		if outputKind != "business_record" && outputKind != "record_patch" {
			continue
		}
		if patch := maclawAppApprovalPatchMap(output.Data); len(patch) > 0 {
			return patch
		}
	}
	return nil
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
	out := make([]maclawAppApprovalInstance, 0, limit)
	for _, instance := range registry.Instances {
		if appID != "" && instance.AppID != appID {
			continue
		}
		if lane != "all" && lane != "" && instance.Lane != lane {
			continue
		}
		out = append(out, cloneMaclawAppApprovalInstance(instance))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
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
	return parsedMaclawAppEntry{
		Schema: stringMapValue(entry, "schema"),
		Entry:  entry,
		App:    app,
		ID:     appID,
		Name:   stringMapValue(app, "name"),
		Kind:   stringMapValue(app, "kind"),
	}, nil
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
		if appSkill := maclawAppAppSkillBlockForEntry(entry); appSkill != nil {
			metadata["app_skill_id"] = maclawAppStringValue(appSkill, "id")
			metadata["app_skill_version"] = maclawAppStringValue(appSkill, "version")
		}
		if workflowSkillIDs := maclawAppWorkflowSkillIDsForEntry(entry); len(workflowSkillIDs) > 0 {
			metadata["workflow_skill_ids"] = workflowSkillIDs
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

func anyMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
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
