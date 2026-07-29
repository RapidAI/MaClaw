package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Serializes read-modify-write of app_run_history.json across concurrent
// skill completions / UI clears. Readers take RLock so list is not torn mid-write.
var maclawAppRunHistoryMu sync.RWMutex

// maclawAppRunHistoryEntry is the durable run history / publish-evidence record
// shared by the App panel, skill governance testEvidence, and package gates.
// JSON field names intentionally match the frontend AppRunHistoryEntry shape
// so Wails can round-trip without a separate DTO layer.
type maclawAppRunHistoryEntry struct {
	RunID                      string         `json:"runID"`
	AppID                      string         `json:"appID"`
	Status                     string         `json:"status"`
	DefinitionHash             string         `json:"definitionHash,omitempty"`
	TestProtocolFingerprint    string         `json:"testProtocolFingerprint,omitempty"`
	WorkspaceLayoutFingerprint string         `json:"workspaceLayoutFingerprint,omitempty"`
	OutputMode                 string         `json:"outputMode,omitempty"`
	InputSummary               string         `json:"inputSummary,omitempty"`
	Message                    string         `json:"message,omitempty"`
	ArtifactID                 string         `json:"artifactID,omitempty"`
	ArtifactURI                string         `json:"artifactURI,omitempty"`
	ArtifactName               string         `json:"artifactName,omitempty"`
	ArtifactPath               string         `json:"artifactPath,omitempty"`
	ArtifactDownloadState      string         `json:"artifactDownloadState,omitempty"`
	Artifacts                  []any          `json:"artifacts,omitempty"`
	ResultPayload              map[string]any `json:"resultPayload,omitempty"`
	Outputs                    []any          `json:"outputs,omitempty"`
	ResultCoverage             map[string]any `json:"resultCoverage,omitempty"`
	DependencyVerification     map[string]any `json:"dependencyVerification,omitempty"`
	ApprovalInstance           map[string]any `json:"approvalInstance,omitempty"`
	SkillName                  string         `json:"skillName,omitempty"`
	GovernanceRecorded         bool           `json:"governanceRecorded,omitempty"`
	GovernanceError            string         `json:"governanceError,omitempty"`
	Source                     string         `json:"source,omitempty"` // skill_governance | runtime | imported
	At                         string         `json:"at"`
}

type maclawAppRunHistoryStore struct {
	Schema    string                                `json:"schema"`
	UpdatedAt string                                `json:"updated_at"`
	ByApp     map[string][]maclawAppRunHistoryEntry `json:"by_app"`
}

const (
	maclawAppRunHistorySchema      = "maclaw.app.run_history.v1"
	maclawAppRunHistoryPerAppLimit = 50
	maclawAppRunHistoryAppLimit    = 200

	// maclawAppSkillGovernanceRunMessage marks the lightweight durable entry
	// written by RecordMaclawAppRunEvidenceForSkill. It is a provenance note,
	// not a run summary, so merges prefer any richer existing message.
	maclawAppSkillGovernanceRunMessage = "skill governance testEvidence recorded"
)

// mergeMaclawAppRunHistoryEntry folds an existing durable entry into the
// incoming one when both share a runID. The skill-governance write arrives
// after the rich runtime entry and carries only provenance fields; a plain
// replace would downgrade the stored evidence (loss of test protocol /
// workspace layout fingerprints and dependency verification), breaking the
// publish checks. Incoming non-empty fields win; gaps are filled from the
// existing entry.
func mergeMaclawAppRunHistoryEntry(incoming, existing maclawAppRunHistoryEntry) maclawAppRunHistoryEntry {
	merged := incoming
	if merged.DefinitionHash == "" {
		merged.DefinitionHash = existing.DefinitionHash
	}
	if merged.TestProtocolFingerprint == "" {
		merged.TestProtocolFingerprint = existing.TestProtocolFingerprint
	}
	if merged.WorkspaceLayoutFingerprint == "" {
		merged.WorkspaceLayoutFingerprint = existing.WorkspaceLayoutFingerprint
	}
	if merged.OutputMode == "" {
		merged.OutputMode = existing.OutputMode
	}
	if merged.InputSummary == "" {
		merged.InputSummary = existing.InputSummary
	}
	if merged.Message == "" || (merged.Message == maclawAppSkillGovernanceRunMessage && existing.Message != "") {
		merged.Message = existing.Message
	}
	if merged.ArtifactID == "" {
		merged.ArtifactID = existing.ArtifactID
	}
	if merged.ArtifactURI == "" {
		merged.ArtifactURI = existing.ArtifactURI
	}
	if merged.ArtifactName == "" {
		merged.ArtifactName = existing.ArtifactName
	}
	if merged.ArtifactPath == "" {
		merged.ArtifactPath = existing.ArtifactPath
	}
	if merged.ArtifactDownloadState == "" {
		merged.ArtifactDownloadState = existing.ArtifactDownloadState
	}
	if len(merged.Artifacts) == 0 {
		merged.Artifacts = existing.Artifacts
	}
	if merged.ResultPayload == nil {
		merged.ResultPayload = existing.ResultPayload
	}
	if len(merged.Outputs) == 0 {
		merged.Outputs = existing.Outputs
	}
	if merged.ResultCoverage == nil {
		merged.ResultCoverage = existing.ResultCoverage
	}
	if merged.DependencyVerification == nil {
		merged.DependencyVerification = existing.DependencyVerification
	}
	if merged.ApprovalInstance == nil {
		merged.ApprovalInstance = existing.ApprovalInstance
	}
	if merged.SkillName == "" {
		merged.SkillName = existing.SkillName
	}
	merged.GovernanceRecorded = merged.GovernanceRecorded || existing.GovernanceRecorded
	if merged.GovernanceError == "" {
		merged.GovernanceError = existing.GovernanceError
	}
	if merged.Source == "" {
		merged.Source = existing.Source
	}
	return merged
}

// RecordMaclawAppRunHistory persists one app run (success or failure) to the
// durable data directory. localStorage remains a UI cache only.
func (a *App) RecordMaclawAppRunHistory(entry maclawAppRunHistoryEntry) (maclawAppRunHistoryEntry, error) {
	entry = normalizeMaclawAppRunHistoryEntry(entry)
	if entry.AppID == "" {
		return maclawAppRunHistoryEntry{}, fmt.Errorf("appID is required")
	}
	if entry.RunID == "" {
		return maclawAppRunHistoryEntry{}, fmt.Errorf("runID is required")
	}
	if entry.Status == "" {
		entry.Status = "done"
	}
	if entry.At == "" {
		entry.At = time.Now().UTC().Format(time.RFC3339)
	}
	if entry.Source == "" {
		entry.Source = "runtime"
	}

	maclawAppRunHistoryMu.Lock()
	defer maclawAppRunHistoryMu.Unlock()

	store, err := a.readMaclawAppRunHistoryStore()
	if err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	if store.ByApp == nil {
		store.ByApp = map[string][]maclawAppRunHistoryEntry{}
	}
	list := store.ByApp[entry.AppID]
	next := make([]maclawAppRunHistoryEntry, 0, len(list)+1)
	for _, existing := range list {
		if strings.EqualFold(strings.TrimSpace(existing.RunID), entry.RunID) {
			// Same run recorded twice (runtime entry + skill-governance stamp):
			// merge so the later sparse write cannot erase rich evidence.
			entry = mergeMaclawAppRunHistoryEntry(entry, existing)
			continue
		}
		next = append(next, existing)
	}
	next = append([]maclawAppRunHistoryEntry{entry}, next...)
	if len(next) > maclawAppRunHistoryPerAppLimit {
		next = next[:maclawAppRunHistoryPerAppLimit]
	}
	store.ByApp[entry.AppID] = next
	store.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	store.Schema = maclawAppRunHistorySchema
	pruneMaclawAppRunHistoryStore(&store)
	if err := a.writeMaclawAppRunHistoryStore(store); err != nil {
		return maclawAppRunHistoryEntry{}, err
	}
	return entry, nil
}

// ListMaclawAppRunHistory returns durable run history for one app (newest first).
func (a *App) ListMaclawAppRunHistory(appID string, limit int) ([]maclawAppRunHistoryEntry, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, fmt.Errorf("appID is required")
	}
	if limit <= 0 || limit > maclawAppRunHistoryPerAppLimit {
		limit = 8
	}
	maclawAppRunHistoryMu.RLock()
	defer maclawAppRunHistoryMu.RUnlock()
	store, err := a.readMaclawAppRunHistoryStore()
	if err != nil {
		return nil, err
	}
	list := store.ByApp[appID]
	if len(list) == 0 {
		return []maclawAppRunHistoryEntry{}, nil
	}
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]maclawAppRunHistoryEntry, len(list))
	copy(out, list)
	return out, nil
}

// ListAllMaclawAppRunHistory returns newest durable runs across all apps.
func (a *App) ListAllMaclawAppRunHistory(limit int) ([]maclawAppRunHistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	maclawAppRunHistoryMu.RLock()
	defer maclawAppRunHistoryMu.RUnlock()
	store, err := a.readMaclawAppRunHistoryStore()
	if err != nil {
		return nil, err
	}
	all := make([]maclawAppRunHistoryEntry, 0, 32)
	for appID, list := range store.ByApp {
		for _, entry := range list {
			if strings.TrimSpace(entry.AppID) == "" {
				entry.AppID = appID
			}
			all = append(all, entry)
		}
	}
	sortMaclawAppRunHistoryNewestFirst(all)
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// ClearMaclawAppRunHistory removes durable history for one app.
func (a *App) ClearMaclawAppRunHistory(appID string) (bool, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return false, fmt.Errorf("appID is required")
	}
	maclawAppRunHistoryMu.Lock()
	defer maclawAppRunHistoryMu.Unlock()

	store, err := a.readMaclawAppRunHistoryStore()
	if err != nil {
		return false, err
	}
	if store.ByApp == nil {
		return false, nil
	}
	if _, ok := store.ByApp[appID]; !ok {
		return false, nil
	}
	delete(store.ByApp, appID)
	store.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.writeMaclawAppRunHistoryStore(store); err != nil {
		return false, err
	}
	return true, nil
}

// CheckMaclawAppRuntimeHealth re-plans dependencies for a package/app and
// returns a structured pre-run health snapshot. Use this before launch so
// install-time and run-time dependency checks share one authoritative path.
func (a *App) CheckMaclawAppRuntimeHealth(packageJSON string, appID string) (map[string]any, error) {
	packageJSON = strings.TrimSpace(packageJSON)
	if packageJSON == "" {
		return nil, fmt.Errorf("packageJSON is required")
	}
	appID = strings.TrimSpace(appID)

	plan, err := a.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		return nil, err
	}

	// Optionally narrow dependency flags to one app while still returning full plan.
	deps := plan.Dependencies
	if appID != "" {
		filtered := make([]maclawAppInstallPlanDependency, 0, len(deps))
		for _, dep := range deps {
			if len(dep.AppIDs) == 0 || containsMaclawAppString(dep.AppIDs, appID) {
				filtered = append(filtered, dep)
			}
		}
		deps = filtered
	}

	hasMissingRequired := false
	hasBlocking := false
	readyCount := 0
	missingRequired := make([]string, 0)
	blockingDetails := make([]string, 0)
	for _, dep := range deps {
		if maclawAppDependencyIsReady(dep) {
			readyCount++
			continue
		}
		if !dep.Required {
			continue
		}
		hasMissingRequired = true
		label := firstNonEmpty(dep.ID, dep.SkillID, dep.InstalledName, dep.InstallRef, dep.RuntimeSkillRef)
		if label != "" {
			missingRequired = append(missingRequired, label)
		}
		// Align with frontend isBlockingBackendDependency: action=install is
		// auto-installable and must not hard-block open/run health.
		if maclawAppDependencyIsHardBlocked(dep) {
			hasBlocking = true
			detail := firstNonEmpty(dep.Message, dep.InstallErrorDetail, dep.InstallRefMessage)
			if detail != "" {
				blockingDetails = append(blockingDetails, fmt.Sprintf("%s: %s", label, detail))
			} else if label != "" {
				blockingDetails = append(blockingDetails, label)
			}
		}
	}
	if plan.HasMissingRequired && appID == "" {
		hasMissingRequired = true
	}
	if plan.HasBlockingDependency && appID == "" {
		hasBlocking = true
	}

	installPresent := false
	installBlocking := false
	installSource := ""
	if appID != "" {
		if rec, findErr := a.findMaclawAppInstallRecord(appID); findErr == nil && rec != nil {
			installPresent = true
			installSource = strings.TrimSpace(rec.Source)
			// Snapshot only — may lag after InstallMaclawAppDependencies.
			installBlocking = rec.HasMissingRequired || rec.HasBlockingDependency
		}
	}

	// Live plan is authoritative. has_missing_required may still be true when
	// deps are action=install (auto-installable); only hard blocks set blocked.
	blocked := hasBlocking || plan.HasWorkflowContractIssue
	message := "runtime dependencies ready"
	if plan.HasWorkflowContractIssue {
		message = firstMaclawAppReviewIssueMessage(plan.WorkflowContractIssues, "approval workflow contract is invalid")
	} else if hasBlocking {
		if summary := strings.Join(blockingDetails, "; "); summary != "" {
			message = "required skill dependencies are missing or unavailable: " + summary
		} else if len(missingRequired) > 0 {
			message = "required skill dependencies are missing or unavailable: " + strings.Join(missingRequired, ", ")
		} else {
			message = "required skill dependencies are missing or unavailable"
		}
	} else if hasMissingRequired {
		// Installable missing deps — run path can auto-install.
		if len(missingRequired) > 0 {
			message = "required skill dependencies can be installed automatically: " + strings.Join(missingRequired, ", ")
		} else {
			message = "required skill dependencies can be installed automatically"
		}
	} else if installBlocking {
		// Soft note only: registry still says blocked but live plan is clean.
		message = "runtime dependencies ready (install record snapshot is stale; re-record install to refresh)"
	}

	return map[string]any{
		"schema":                      "maclaw.app.runtime_health.v1",
		"ok":                          !blocked,
		"blocked":                     blocked,
		"message":                     message,
		"app_id":                      appID,
		"checked_at":                  time.Now().UTC().Format(time.RFC3339),
		"dependency_count":            len(deps),
		"ready_dependency_count":      readyCount,
		"has_missing_required":        hasMissingRequired,
		"has_blocking_dependency":     hasBlocking,
		"has_workflow_contract_issue": plan.HasWorkflowContractIssue,
		"has_governance_review_issue": plan.HasGovernanceReviewIssue,
		"missing_required":            missingRequired,
		"blocking_details":            blockingDetails,
		"install_record_present":      installPresent,
		"install_record_source":       installSource,
		"install_record_blocking":     installBlocking,
		"plan":                        plan,
	}, nil
}

func normalizeMaclawAppRunHistoryEntry(entry maclawAppRunHistoryEntry) maclawAppRunHistoryEntry {
	entry.RunID = strings.TrimSpace(entry.RunID)
	entry.AppID = strings.TrimSpace(entry.AppID)
	entry.Status = strings.ToLower(strings.TrimSpace(entry.Status))
	entry.DefinitionHash = strings.TrimSpace(entry.DefinitionHash)
	entry.TestProtocolFingerprint = strings.TrimSpace(entry.TestProtocolFingerprint)
	entry.WorkspaceLayoutFingerprint = strings.TrimSpace(entry.WorkspaceLayoutFingerprint)
	entry.OutputMode = strings.TrimSpace(entry.OutputMode)
	entry.InputSummary = strings.TrimSpace(entry.InputSummary)
	entry.Message = strings.TrimSpace(entry.Message)
	entry.ArtifactID = strings.TrimSpace(entry.ArtifactID)
	entry.ArtifactURI = strings.TrimSpace(entry.ArtifactURI)
	entry.ArtifactName = strings.TrimSpace(entry.ArtifactName)
	entry.ArtifactPath = strings.TrimSpace(entry.ArtifactPath)
	entry.ArtifactDownloadState = strings.TrimSpace(entry.ArtifactDownloadState)
	entry.SkillName = strings.TrimSpace(entry.SkillName)
	entry.GovernanceError = strings.TrimSpace(entry.GovernanceError)
	entry.Source = strings.TrimSpace(entry.Source)
	entry.At = strings.TrimSpace(entry.At)
	switch entry.Status {
	case "done", "error", "cancelled":
	case "success", "ok", "completed":
		entry.Status = "done"
	case "fail", "failed":
		entry.Status = "error"
	case "cancel", "canceled":
		entry.Status = "cancelled"
	default:
		if entry.Status == "" {
			entry.Status = "done"
		}
	}
	return entry
}

func (a *App) maclawAppRunHistoryPath() string {
	return filepath.Join(a.GetDataDir(), "app_run_history.json")
}

func (a *App) readMaclawAppRunHistoryStore() (maclawAppRunHistoryStore, error) {
	path := a.maclawAppRunHistoryPath()
	store := maclawAppRunHistoryStore{
		Schema: maclawAppRunHistorySchema,
		ByApp:  map[string][]maclawAppRunHistoryEntry{},
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return store, fmt.Errorf("decode maclaw app run history: %w", err)
	}
	if store.Schema == "" {
		store.Schema = maclawAppRunHistorySchema
	}
	if store.ByApp == nil {
		store.ByApp = map[string][]maclawAppRunHistoryEntry{}
	}
	return store, nil
}

func (a *App) writeMaclawAppRunHistoryStore(store maclawAppRunHistoryStore) error {
	path := a.maclawAppRunHistoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if store.Schema == "" {
		store.Schema = maclawAppRunHistorySchema
	}
	if store.ByApp == nil {
		store.ByApp = map[string][]maclawAppRunHistoryEntry{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

// maclawAppDependencyIsHardBlocked mirrors frontend isBlockingBackendDependency:
// missing required deps with action=install are auto-installable, not hard blocks.
func maclawAppDependencyIsHardBlocked(dep maclawAppInstallPlanDependency) bool {
	if !dep.Required {
		return false
	}
	if maclawAppDependencyIsReady(dep) {
		return false
	}
	action := strings.TrimSpace(dep.Action)
	health := strings.TrimSpace(dep.Health)
	if action == "blocked" || action == "failed" {
		return true
	}
	if !dep.Installed {
		return action != "install"
	}
	return health != "" && !strings.EqualFold(health, "ready")
}

func pruneMaclawAppRunHistoryStore(store *maclawAppRunHistoryStore) {
	if store == nil || len(store.ByApp) <= maclawAppRunHistoryAppLimit {
		return
	}
	type appNewest struct {
		appID string
		at    string
	}
	items := make([]appNewest, 0, len(store.ByApp))
	for appID, list := range store.ByApp {
		at := ""
		if len(list) > 0 {
			at = list[0].At
		}
		items = append(items, appNewest{appID: appID, at: at})
	}
	// Sort oldest-newest-at first, drop excess apps.
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j].at < items[j-1].at {
			items[j-1], items[j] = items[j], items[j-1]
			j--
		}
	}
	for len(store.ByApp) > maclawAppRunHistoryAppLimit && len(items) > 0 {
		delete(store.ByApp, items[0].appID)
		items = items[1:]
	}
}

func sortMaclawAppRunHistoryNewestFirst(items []maclawAppRunHistoryEntry) {
	// Small N (UI caps); keep allocation-free in-place sort.
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j-1].At < items[j].At {
			items[j-1], items[j] = items[j], items[j-1]
			j--
		}
	}
}

// recordMaclawAppRunHistoryBestEffort stores a lightweight durable run marker
// after skill governance evidence writes. Failures are returned so callers can
// surface them; they do not roll back the skill definition write.
func (a *App) recordMaclawAppRunHistoryBestEffort(entry maclawAppRunHistoryEntry) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	_, err := a.RecordMaclawAppRunHistory(entry)
	return err
}
