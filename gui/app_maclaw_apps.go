package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	Schema             string                           `json:"schema"`
	Apps               []maclawAppInstallPlanApp        `json:"apps"`
	Dependencies       []maclawAppInstallPlanDependency `json:"dependencies"`
	HasMissingRequired bool                             `json:"has_missing_required"`
}

type maclawAppInstallPlanApp struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Schema string `json:"schema,omitempty"`
}

type maclawAppInstallPlanDependency struct {
	ID            string   `json:"id"`
	Version       string   `json:"version,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Required      bool     `json:"required"`
	Source        string   `json:"source,omitempty"`
	AppIDs        []string `json:"app_ids,omitempty"`
	Installed     bool     `json:"installed"`
	InstalledName string   `json:"installed_name,omitempty"`
	InstalledDir  string   `json:"installed_dir,omitempty"`
	Action        string   `json:"action"`
	Message       string   `json:"message,omitempty"`
}

type parsedMaclawAppEntry struct {
	Schema string
	Entry  map[string]any
	App    map[string]any
	ID     string
	Name   string
	Kind   string
}

type maclawAppInstallRecord struct {
	AppID              string                           `json:"app_id"`
	AppName            string                           `json:"app_name,omitempty"`
	Kind               string                           `json:"kind,omitempty"`
	Source             string                           `json:"source,omitempty"`
	InstalledAt        string                           `json:"installed_at"`
	PackageSHA         string                           `json:"package_sha256,omitempty"`
	PackageSize        int                              `json:"package_bytes,omitempty"`
	Dependencies       []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	HasMissingRequired bool                             `json:"has_missing_required"`
	Message            string                           `json:"message,omitempty"`
}

type maclawAppInstallRegistry struct {
	Schema    string                   `json:"schema"`
	UpdatedAt string                   `json:"updated_at"`
	Installs  []maclawAppInstallRecord `json:"installs"`
}

type maclawAppApprovalInstance struct {
	AppID              string                      `json:"app_id"`
	InstanceID         string                      `json:"instance_id"`
	Title              string                      `json:"title"`
	Lane               string                      `json:"lane"`
	Status             string                      `json:"status"`
	CurrentNode        string                      `json:"current_node"`
	Owner              string                      `json:"owner"`
	Approver           string                      `json:"approver"`
	UpdatedAt          string                      `json:"updated_at"`
	Result             string                      `json:"result"`
	WorkflowSkillID    string                      `json:"workflow_skill_id,omitempty"`
	BusinessStatus     string                      `json:"business_status,omitempty"`
	ResultStatus       string                      `json:"result_status,omitempty"`
	WorkflowDecisionID string                      `json:"workflow_decision_id,omitempty"`
	RecordID           string                      `json:"record_id,omitempty"`
	DetailURL          string                      `json:"detail_url,omitempty"`
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
	DatasetID  string                    `json:"dataset_id"`
	RecordID   string                    `json:"record_id"`
	ApprovalID string                    `json:"approval_id,omitempty"`
	Instance   maclawAppApprovalInstance `json:"instance"`
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
					dep.Installed = true
					dep.InstalledName = match.Name
					dep.InstalledDir = match.SkillDir
					dep.Action = "skip"
					dep.Message = "installed locally"
				} else if dep.Required {
					dep.Action = "blocked"
					dep.Message = "required skill dependency is missing"
					plan.HasMissingRequired = true
				} else {
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
					existing.Action = "blocked"
					existing.Message = "required skill dependency is missing"
					plan.HasMissingRequired = true
				}
			}
		}
	}
	for i := range plan.Dependencies {
		if dep := depsByID[strings.ToLower(plan.Dependencies[i].ID)]; dep != nil {
			plan.Dependencies[i] = *dep
		}
	}
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
			dep.Action = "skip"
			if dep.Message == "" {
				dep.Message = "installed locally"
			}
			continue
		}
		if !dep.Required {
			dep.Action = "optional_missing"
			if dep.Message == "" {
				dep.Message = "optional skill dependency is missing"
			}
			continue
		}
		source, ok := maclawAppInstallSkillSource(dep.Source)
		if !ok {
			dep.Action = "blocked"
			dep.Message = fmt.Sprintf("required skill dependency source %q cannot be installed automatically", dep.Source)
			plan.HasMissingRequired = true
			continue
		}
		if err := a.InstallMixedSkill(source, dep.ID, ""); err != nil {
			dep.Action = "failed"
			dep.Message = err.Error()
			plan.HasMissingRequired = true
			continue
		}
		dep.Installed = true
		dep.Action = "installed"
		dep.Message = "installed dependency skill"
	}
	installed := a.installedMaclawAppSkillIndex()
	plan.HasMissingRequired = false
	for i := range plan.Dependencies {
		dep := &plan.Dependencies[i]
		if match, ok := installed[strings.ToLower(dep.ID)]; ok {
			dep.Installed = true
			dep.InstalledName = match.Name
			dep.InstalledDir = match.SkillDir
			if dep.Action == "failed" || dep.Action == "blocked" {
				dep.Action = "skip"
				dep.Message = "installed locally"
			}
		}
		if dep.Required && !dep.Installed {
			plan.HasMissingRequired = true
		}
	}
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
			AppID:              entry.ID,
			AppName:            entry.Name,
			Kind:               normalizeMaclawAppKind(entry.Kind),
			Source:             strings.TrimSpace(source),
			InstalledAt:        now,
			PackageSHA:         packageSHA,
			PackageSize:        packageSize,
			Dependencies:       cloneMaclawAppPlanDependenciesForApp(plan.Dependencies, entry.ID),
			HasMissingRequired: hasMissingMaclawAppRequiredDependencyForApp(plan.Dependencies, entry.ID),
			Message:            "installed locally",
		}
		registry.upsert(record)
	}
	registry.UpdatedAt = now
	if err := a.writeMaclawAppInstallRegistry(registry); err != nil {
		return nil, err
	}
	return map[string]any{
		"schema":        registry.Schema,
		"installed_at":  now,
		"app_count":     len(entries),
		"package_sha":   packageSHA,
		"package_bytes": packageSize,
	}, nil
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
	instance.AppID = strings.TrimSpace(instance.AppID)
	if instance.AppID == "" {
		return maclawAppApprovalInstance{}, fmt.Errorf("app_id is required")
	}
	instance.InstanceID = strings.TrimSpace(instance.InstanceID)
	if instance.InstanceID == "" {
		instance.InstanceID = "appr-" + firstMaclawAppID([]string{instance.AppID}) + "-" + shortRandomHex()
	}
	instance.Title = strings.TrimSpace(instance.Title)
	if instance.Title == "" {
		instance.Title = instance.AppID
	}
	instance.Lane = normalizeMaclawAppApprovalLane(instance.Lane)
	instance.Status = normalizeMaclawAppApprovalStatus(instance.Status)
	if instance.CurrentNode = strings.TrimSpace(instance.CurrentNode); instance.CurrentNode == "" {
		instance.CurrentNode = "submit"
	}
	instance.Owner = strings.TrimSpace(instance.Owner)
	instance.Approver = strings.TrimSpace(instance.Approver)
	instance.Result = strings.TrimSpace(instance.Result)
	instance.WorkflowSkillID = strings.TrimSpace(instance.WorkflowSkillID)
	instance.BusinessStatus = strings.TrimSpace(instance.BusinessStatus)
	instance.ResultStatus = strings.TrimSpace(instance.ResultStatus)
	instance.WorkflowDecisionID = strings.TrimSpace(instance.WorkflowDecisionID)
	instance.RecordID = strings.TrimSpace(instance.RecordID)
	instance.DetailURL = strings.TrimSpace(instance.DetailURL)
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(instance.UpdatedAt) == "" {
		instance.UpdatedAt = now
	}
	if len(instance.Events) == 0 {
		instance.Events = []maclawAppApprovalEvent{{At: instance.UpdatedAt, Node: instance.CurrentNode, Actor: instance.Owner, Decision: instance.Status, Message: instance.Result}}
	}
	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		return maclawAppApprovalInstance{}, err
	}
	if registry.Schema == "" {
		registry.Schema = "maclaw.app.approvals.v1"
	}
	registry.upsert(instance)
	registry.UpdatedAt = now
	if err := a.writeMaclawAppApprovalRegistry(registry); err != nil {
		return maclawAppApprovalInstance{}, err
	}
	return cloneMaclawAppApprovalInstance(instance), nil
}

// SyncMaclawAppApprovalInstanceToDataSrv writes the app approval instance state
// into DataSrv's RecordApproval link. It is intentionally app-level so the GUI
// never handles DataSrv credentials directly.
func (a *App) SyncMaclawAppApprovalInstanceToDataSrv(input maclawAppApprovalDataSrvSyncInput) (map[string]any, error) {
	input.DatasetID = strings.TrimSpace(input.DatasetID)
	input.RecordID = strings.TrimSpace(input.RecordID)
	input.ApprovalID = strings.TrimSpace(input.ApprovalID)
	instance := cloneMaclawAppApprovalInstance(input.Instance)
	instance.AppID = strings.TrimSpace(instance.AppID)
	instance.InstanceID = strings.TrimSpace(instance.InstanceID)
	if instance.AppID == "" || instance.InstanceID == "" {
		return nil, fmt.Errorf("instance app_id and instance_id are required")
	}
	if input.DatasetID == "" || input.RecordID == "" {
		return map[string]any{"synced": false, "reason": "missing dataset_id or record_id"}, nil
	}
	if instance.WorkflowSkillID == "" {
		return map[string]any{"synced": false, "reason": "missing workflow_skill_id"}, nil
	}
	if input.ApprovalID != "" && (instance.Status == "approved" || instance.Status == "rejected" || instance.Status == "attention") {
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
		return map[string]any{"synced": true, "action": "review_record_approval", "response": out}, nil
	}
	out := a.executeMISDataTool(map[string]interface{}{
		"action":               "create_record_approval",
		"dataset_id":           input.DatasetID,
		"record_id":            input.RecordID,
		"kind":                 "approval",
		"summary":              instance.Title,
		"assigned_to":          instance.Approver,
		"request":              map[string]interface{}{"app_id": instance.AppID, "approval_instance_id": instance.InstanceID, "owner": instance.Owner, "result": instance.Result},
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
	return map[string]any{"synced": true, "action": "create_record_approval", "response": out}, nil
}

// ListMaclawAppApprovalInstances returns newest-first approval instances for a
// MaClaw approval app. lane can be my_requests, pending_my_approval, handled,
// attention, or all/empty.
func (a *App) ListMaclawAppApprovalInstances(appID string, lane string, limit int) ([]maclawAppApprovalInstance, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
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
		if instance.AppID != appID {
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
				Source:   "hub",
			})
		}
		if appSkill := anyMap(holder["appSkill"]); appSkill != nil {
			add(maclawAppInstallPlanDependency{
				ID:       stringMapValue(appSkill, "id"),
				Version:  stringMapValue(appSkill, "version"),
				Kind:     "runtime_skill",
				Required: true,
				Source:   "hub",
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

func (registry *maclawAppApprovalRegistry) upsert(instance maclawAppApprovalInstance) {
	next := make([]maclawAppApprovalInstance, 0, len(registry.Instances)+1)
	next = append(next, cloneMaclawAppApprovalInstance(instance))
	for _, existing := range registry.Instances {
		if existing.AppID == instance.AppID && existing.InstanceID == instance.InstanceID {
			continue
		}
		next = append(next, existing)
	}
	if len(next) > 500 {
		next = next[:500]
	}
	registry.Instances = next
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
