package guiapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// mcpRunSnapshot is the durable copy of one round. Cancel funcs and SSH
// passwords are not fields on the run, and they are not written here.
type mcpRunSnapshot struct {
	ProjectPath        string         `json:"project_path"`
	Name               string         `json:"name,omitempty"`
	Mode               string         `json:"mode,omitempty"`
	Status             string         `json:"status,omitempty"`
	Text               string         `json:"text,omitempty"`
	Error              string         `json:"error,omitempty"`
	Provider           string         `json:"provider,omitempty"`
	Model              string         `json:"model,omitempty"`
	RequestID          string         `json:"request_id,omitempty"`
	ConfirmationKind   string         `json:"confirmation_kind,omitempty"`
	Commands           []string       `json:"commands,omitempty"`
	Workspace          string         `json:"workspace,omitempty"`
	Round              int            `json:"round,omitempty"`
	StartedAt          time.Time      `json:"started_at,omitempty"`
	FinishedAt         time.Time      `json:"finished_at,omitempty"`
	EndReason          string         `json:"end_reason,omitempty"`
	EndDetail          string         `json:"end_detail,omitempty"`
	ConfirmationID     string         `json:"confirmation_id,omitempty"`
	ConfirmFingerprint string         `json:"confirmation_fingerprint,omitempty"`
	LogSeq             int            `json:"log_seq,omitempty"`
	Logs               []mcpLogEvent  `json:"logs,omitempty"`
	HistoryCount       int            `json:"history_count,omitempty"`
	HistorySeal        string         `json:"history_seal,omitempty"`
	RemoteHost         string         `json:"ssh_host,omitempty"`
	RemoteUser         string         `json:"ssh_user,omitempty"`
	RemotePort         int            `json:"ssh_port,omitempty"`
	RemoteWorkDir      string         `json:"remote_work_dir,omitempty"`
	Steps              []mcpStepView  `json:"steps,omitempty"`
	Checks             []mcpCheckView `json:"checks,omitempty"`
	RunID              string         `json:"run_id,omitempty"`
	StatusVersion      int            `json:"status_version,omitempty"`
	ParentTaskID       string         `json:"parent_task_id,omitempty"`
	ChildTaskID        string         `json:"child_task_id,omitempty"`
	Children           []mcpChildView `json:"children,omitempty"`
	Stop               *mcpStopView   `json:"stop,omitempty"`
	PlanID             string         `json:"plan_id,omitempty"`
	PlanVersion        int            `json:"plan_version,omitempty"`
	ProviderRequested  string         `json:"provider_requested,omitempty"`
	ModelRequested     string         `json:"model_requested,omitempty"`
	ProviderEffective  string         `json:"provider_effective,omitempty"`
	ModelEffective     string         `json:"model_effective,omitempty"`
	WorkDirRequested   string         `json:"working_dir_requested,omitempty"`
	SessionPath        string         `json:"session_path,omitempty"`
}

type mcpDurableBundle struct {
	ProjectPath string            `json:"project_path"`
	SavedAt     time.Time         `json:"saved_at"`
	Current     *mcpRunSnapshot   `json:"current,omitempty"`
	Archive     []*mcpRunSnapshot `json:"archive,omitempty"`
}

type mcpRequestSnapshot struct {
	ID          string    `json:"id"`
	Hash        string    `json:"hash,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Command     string    `json:"command,omitempty"`
	Prompt      string    `json:"prompt,omitempty"`
	ProjectPath string    `json:"project_path,omitempty"`
	Error       string    `json:"error,omitempty"`
	Failed      bool      `json:"failed,omitempty"`
	Done        bool      `json:"done,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	State       string    `json:"state,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type mcpRequestTomb struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
}

type mcpRequestDisk struct {
	Requests []mcpRequestSnapshot `json:"requests,omitempty"`
	Tombs    []mcpRequestTomb     `json:"tombs,omitempty"`
}

type mcpExpiredDisk struct {
	Tasks []mcpRequestTomb `json:"tasks,omitempty"`
}

// mcpLogSidecarLine is one progress or tool row. It is appended beside the
// snapshot so a log write does not rename over the last flushed task file.
type mcpLogSidecarLine struct {
	mcpLogEvent
	StatusVersion int `json:"status_version,omitempty"`
}

func (a *App) mcpHostStateDir() string {
	if a == nil {
		return ""
	}
	if dir := strings.TrimSpace(a.mcpStateDir); dir != "" {
		return dir
	}
	if testing.Testing() {
		return ""
	}
	base := strings.TrimSpace(corelib.MaclawDataDir())
	if base == "" {
		return ""
	}
	return filepath.Join(base, "mcp-host")
}

func (a *App) mcpLoadDurable() {
	if a == nil {
		return
	}
	a.mcpDurableOnce.Do(func() {
		dir := a.mcpHostStateDir()
		if dir == "" {
			return
		}
		a.mcpLoadExpired(dir)
		a.mcpLoadRunDir(dir)
		a.mcpLoadRequestDisk(dir)
	})
}

func (a *App) mcpLoadExpired(dir string) {
	var disk mcpExpiredDisk
	if err := mcpReadJSON(filepath.Join(dir, "expired.json"), &disk); err != nil {
		return
	}
	a.mcpExpiredMu.Lock()
	defer a.mcpExpiredMu.Unlock()
	if a.mcpExpired == nil {
		a.mcpExpired = map[string]time.Time{}
	}
	now := time.Now()
	for _, item := range disk.Tasks {
		path := normalizeProjectSessionPath(item.ID)
		if path == "" || !mcpExpiredAtFresh(item.At, now) {
			continue
		}
		if prev, ok := a.mcpExpired[path]; ok && !item.At.After(prev) {
			continue
		}
		a.mcpExpired[path] = item.At
	}
}

func (a *App) mcpLoadRunDir(dir string) {
	runs := filepath.Join(dir, "runs")
	entries, err := os.ReadDir(runs)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		full := filepath.Join(runs, entry.Name())
		var bundle mcpDurableBundle
		if err := mcpReadJSON(full, &bundle); err != nil {
			continue
		}
		logPath := mcpRunSidecarPath(full)
		mcpMergeLogSidecar(&bundle, logPath)
		path := normalizeProjectSessionPath(bundle.ProjectPath)
		if path == "" && bundle.Current != nil {
			path = normalizeProjectSessionPath(bundle.Current.ProjectPath)
		}
		if path == "" {
			continue
		}
		if !mcpBundleFresh(bundle) {
			// Stamp this moment. SavedAt is already past retention, so using
			// it would drop the tombstone in the same save.
			a.mcpMarkTaskExpired(path, time.Now())
			_ = os.Remove(full)
			_ = os.Remove(logPath)
			continue
		}
		a.mcpInstallBundle(path, bundle)
	}
	a.mcpSaveExpired()
}

func mcpBundleFresh(bundle mcpDurableBundle) bool {
	latest := bundle.SavedAt
	note := func(snap *mcpRunSnapshot) {
		if snap == nil {
			return
		}
		if snap.FinishedAt.After(latest) {
			latest = snap.FinishedAt
		}
		if snap.StartedAt.After(latest) {
			latest = snap.StartedAt
		}
	}
	note(bundle.Current)
	for _, snap := range bundle.Archive {
		note(snap)
	}
	if latest.IsZero() {
		return true
	}
	return time.Since(latest) <= mcpStateRetention
}

func (a *App) mcpInstallBundle(path string, bundle mcpDurableBundle) {
	if a == nil {
		return
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	if a.mcpRuns == nil {
		a.mcpRuns = map[string]*mcpRunRecord{}
	}
	if a.mcpRuns[path] == nil && bundle.Current != nil {
		if rec := mcpSettleLoadedRun(mcpRunFromSnapshot(bundle.Current)); rec != nil {
			rec.ProjectPath = path
			a.mcpRuns[path] = rec
		}
	}
	if a.mcpRunArchive == nil {
		a.mcpRunArchive = map[string][]*mcpRunRecord{}
	}
	have := map[string]struct{}{}
	if rec := a.mcpRuns[path]; rec != nil && rec.RunID != "" {
		have[rec.RunID] = struct{}{}
	}
	for _, old := range a.mcpRunArchive[path] {
		if old != nil && old.RunID != "" {
			have[old.RunID] = struct{}{}
		}
	}
	list := append([]*mcpRunRecord(nil), a.mcpRunArchive[path]...)
	for _, snap := range bundle.Archive {
		rec := mcpSettleLoadedRun(mcpRunFromSnapshot(snap))
		if rec == nil || rec.RunID == "" {
			continue
		}
		if _, ok := have[rec.RunID]; ok {
			continue
		}
		have[rec.RunID] = struct{}{}
		rec.ProjectPath = path
		list = append(list, rec)
	}
	if len(list) > mcpRunArchiveLimit {
		list = append([]*mcpRunRecord(nil), list[len(list)-mcpRunArchiveLimit:]...)
	}
	if len(list) > 0 {
		a.mcpRunArchive[path] = list
	}
}

func mcpSettleLoadedRun(rec *mcpRunRecord) *mcpRunRecord {
	if rec == nil || rec.Status != mcpStatusRunning {
		return rec
	}
	now := time.Now()
	rec.Status = mcpStatusCancelled
	rec.FinishedAt = now
	rec.EndReason = mcpStatusCancelled
	rec.Error = errMCPHostWaitEnded.Error()
	rec.Stop = mcpStopFor(mcpStatusCancelled, rec.Error, rec.Steps, errMCPHostWaitEnded, now)
	rec.cancel = nil
	rec.hostWaitEnded = true
	return rec
}

func (a *App) mcpLoadRequestDisk(dir string) {
	var disk mcpRequestDisk
	if err := mcpReadJSON(filepath.Join(dir, "requests.json"), &disk); err != nil {
		return
	}
	a.mcpReqMu.Lock()
	pruned := a.mcpInstallRequestsLocked(disk)
	a.mcpReqMu.Unlock()
	if pruned {
		// Drop aged rows from the file. The lock is already released.
		a.mcpPersistRequests()
	}
}

func (a *App) mcpInstallRequestsLocked(disk mcpRequestDisk) bool {
	if a.mcpReqs == nil {
		a.mcpReqs = map[string]*mcpClientRequest{}
	}
	if a.mcpReqTombs == nil {
		a.mcpReqTombs = map[string]mcpRequestTomb{}
	}
	now := time.Now()
	pruned := false
	for _, snap := range disk.Requests {
		id := strings.TrimSpace(snap.ID)
		if id == "" {
			continue
		}
		// Past retention the id is free again. Tombstoning it here would
		// keep the id reserved forever, because the next load would copy
		// that tomb with no age check.
		if !snap.UpdatedAt.IsZero() && !mcpExpiredAtFresh(snap.UpdatedAt, now) {
			pruned = true
			continue
		}
		if _, ok := a.mcpReqs[id]; ok {
			continue
		}
		ready := make(chan struct{})
		close(ready)
		a.mcpReqs[id] = &mcpClientRequest{
			Kind:        snap.Kind,
			Command:     snap.Command,
			Prompt:      snap.Prompt,
			ProjectPath: normalizeProjectSessionPath(snap.ProjectPath),
			errText:     snap.Error,
			failed:      snap.Failed,
			done:        true,
			ready:       ready,
			Hash:        snap.Hash,
			RunID:       snap.RunID,
			State:       snap.State,
			updated:     snap.UpdatedAt,
		}
		a.mcpReqOrder = append(a.mcpReqOrder, id)
	}
	for _, tomb := range disk.Tombs {
		id := strings.TrimSpace(tomb.ID)
		if id == "" {
			continue
		}
		if !mcpRequestTombFresh(tomb.At, now) {
			pruned = true
			continue
		}
		if _, ok := a.mcpReqTombs[id]; ok {
			continue
		}
		a.mcpReqTombs[id] = tomb
	}
	return pruned
}

func mcpExpiredAtFresh(at, now time.Time) bool {
	return !at.IsZero() && !at.Before(now.Add(-mcpStateRetention))
}

func mcpRequestTombFresh(at, now time.Time) bool {
	// A missing stamp is kept. Eviction always records the time, so an
	// absent stamp is not the retention leak.
	if at.IsZero() {
		return true
	}
	return mcpExpiredAtFresh(at, now)
}

// mcpRequestStillKept reports whether a remembered request may still reserve
// its id. A live request stays, even when the turn is older than retention.
// A finished request past retention does not, so the id can be claimed again.
func mcpRequestStillKept(req *mcpClientRequest, now time.Time) bool {
	if req == nil || !req.done || req.updated.IsZero() {
		return true
	}
	return mcpExpiredAtFresh(req.updated, now)
}

func (a *App) mcpMarkTaskExpired(path string, at time.Time) {
	path = normalizeProjectSessionPath(path)
	if a == nil || path == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	a.mcpExpiredMu.Lock()
	defer a.mcpExpiredMu.Unlock()
	if a.mcpExpired == nil {
		a.mcpExpired = map[string]time.Time{}
	}
	if prev, ok := a.mcpExpired[path]; ok && !prev.IsZero() && !at.After(prev) {
		return
	}
	a.mcpExpired[path] = at
}

func (a *App) mcpSaveExpired() {
	dir := a.mcpHostStateDir()
	if a == nil || dir == "" {
		return
	}
	a.mcpDiskMu.Lock()
	defer a.mcpDiskMu.Unlock()
	a.mcpExpiredMu.Lock()
	a.mcpPruneExpiredLocked(time.Now())
	disk := mcpExpiredDisk{}
	for path, at := range a.mcpExpired {
		disk.Tasks = append(disk.Tasks, mcpRequestTomb{ID: path, At: at})
	}
	a.mcpExpiredMu.Unlock()
	target := filepath.Join(dir, "expired.json")
	if len(disk.Tasks) == 0 {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			log.Printf("[mcp-host] persist expired: %v", err)
		}
		return
	}
	if err := mcpWriteJSON(target, disk); err != nil {
		log.Printf("[mcp-host] persist expired: %v", err)
	}
}

func (a *App) mcpPruneExpiredLocked(now time.Time) {
	for path, at := range a.mcpExpired {
		if !mcpExpiredAtFresh(at, now) {
			delete(a.mcpExpired, path)
		}
	}
}

func (a *App) mcpTaskExpiredOnly(path string) bool {
	path = normalizeProjectSessionPath(path)
	if a == nil || path == "" || !a.mcpExpiredHas(path) {
		return false
	}
	if _, ok := a.mcpViewFromRegistry(path); ok {
		return false
	}
	if a.memoryStore != nil {
		if _, err := a.remoteControlKnownTask(path); err == nil {
			return false
		}
	}
	return true
}

func (a *App) mcpExpiredHas(path string) bool {
	if a == nil {
		return false
	}
	a.mcpExpiredMu.Lock()
	defer a.mcpExpiredMu.Unlock()
	a.mcpPruneExpiredLocked(time.Now())
	_, ok := a.mcpExpired[normalizeProjectSessionPath(path)]
	return ok
}

func (a *App) mcpTaskKnown(path string) bool {
	path = normalizeProjectSessionPath(path)
	if a == nil || path == "" {
		return false
	}
	if _, ok := a.mcpViewFromRegistry(path); ok {
		return true
	}
	if a.memoryStore == nil {
		return false
	}
	_, err := a.remoteControlKnownTask(path)
	return err == nil
}

func (a *App) mcpPersistRun(path string) {
	if a == nil {
		return
	}
	dir := a.mcpHostStateDir()
	if dir == "" {
		return
	}
	path = normalizeProjectSessionPath(path)
	if path == "" {
		return
	}
	a.mcpDiskMu.Lock()
	defer a.mcpDiskMu.Unlock()
	a.mcpRunsMu.Lock()
	bundle := a.mcpCaptureRunLocked(path)
	a.mcpRunsMu.Unlock()
	if bundle == nil {
		return
	}
	snapshot := mcpRunFilePath(dir, path)
	// Always sync. Renaming an unsynced file over the previous snapshot can
	// destroy the last durable copy when power fails.
	if err := mcpWriteJSON(snapshot, bundle); err != nil {
		log.Printf("[mcp-host] persist run: %v", err)
		return
	}
	if err := mcpDiscardLogSidecar(mcpRunSidecarPath(snapshot)); err != nil {
		log.Printf("[mcp-host] persist log: %v", err)
	}
}

// mcpLogSidecarCompactBytes is the size at which a progress sidecar is folded
// back into the synced snapshot. The memory ring is 400 lines. The sidecar is
// not, so a long run would keep fsyncing a file that only grows. One short
// progress line stays under this bound and does not rename the snapshot.
const mcpLogSidecarCompactBytes int64 = 1 << 20

// mcpPersistLogLine appends one synced row. The snapshot stays in place until
// this sidecar reaches mcpLogSidecarCompactBytes, or until commit, finish,
// cancel, or another structural write.
func (a *App) mcpPersistLogLine(path string, ev mcpLogEvent, statusVersion int) {
	if a == nil || ev.Seq <= 0 {
		return
	}
	dir := a.mcpHostStateDir()
	if dir == "" {
		return
	}
	path = normalizeProjectSessionPath(path)
	if path == "" {
		return
	}
	a.mcpDiskMu.Lock()
	defer a.mcpDiskMu.Unlock()
	if err := mcpAppendLogSidecar(mcpRunLogPath(dir, path), ev, statusVersion); err != nil {
		log.Printf("[mcp-host] persist log: %v", err)
		return
	}
	a.mcpFoldLogSidecarLocked(dir, path, ev, statusVersion, mcpLogSidecarCompactBytes)
}

// mcpFoldLogSidecarLocked rewrites the snapshot from memory and removes the
// sidecar once the sidecar is at least minBytes. The caller holds mcpDiskMu
// and must not hold mcpRunsMu, and must not call mcpPersistRun. The sidecar
// stays when the live record does not yet cover this row: a crash then merges
// the row, and a snapshot write is not left without it. Discard runs only
// after the synced snapshot write succeeds.
func (a *App) mcpFoldLogSidecarLocked(dir, path string, ev mcpLogEvent, statusVersion int, minBytes int64) {
	if a == nil || minBytes <= 0 || ev.Seq <= 0 {
		return
	}
	side := mcpRunLogPath(dir, path)
	info, err := os.Stat(side)
	if err != nil || info.Size() < minBytes {
		return
	}
	a.mcpRunsMu.Lock()
	bundle := a.mcpCaptureRunLocked(path)
	a.mcpRunsMu.Unlock()
	if bundle == nil || bundle.Current == nil || bundle.Current.LogSeq < ev.Seq || bundle.Current.StatusVersion < statusVersion {
		return
	}
	if err := mcpWriteJSON(mcpRunFilePath(dir, path), bundle); err != nil {
		log.Printf("[mcp-host] persist log: %v", err)
		return
	}
	if err := mcpDiscardLogSidecar(side); err != nil {
		log.Printf("[mcp-host] persist log: %v", err)
	}
}

func (a *App) mcpCaptureRunLocked(path string) *mcpDurableBundle {
	if a == nil {
		return nil
	}
	bundle := &mcpDurableBundle{ProjectPath: path, SavedAt: time.Now()}
	if rec := a.mcpRuns[path]; rec != nil {
		bundle.Current = mcpSnapshotFromRun(rec)
	}
	for _, old := range a.mcpRunArchive[path] {
		if snap := mcpSnapshotFromRun(old); snap != nil {
			bundle.Archive = append(bundle.Archive, snap)
		}
	}
	if bundle.Current == nil && len(bundle.Archive) == 0 {
		return nil
	}
	return bundle
}

func (a *App) mcpPersistRequests() {
	if a == nil {
		return
	}
	dir := a.mcpHostStateDir()
	if dir == "" {
		return
	}
	a.mcpDiskMu.Lock()
	defer a.mcpDiskMu.Unlock()
	a.mcpReqMu.Lock()
	disk := a.mcpCaptureRequestsLocked()
	a.mcpReqMu.Unlock()
	if disk == nil {
		return
	}
	if err := mcpWriteJSON(filepath.Join(dir, "requests.json"), disk); err != nil {
		log.Printf("[mcp-host] persist requests: %v", err)
	}
}

func (a *App) mcpCaptureRequestsLocked() *mcpRequestDisk {
	if a == nil {
		return nil
	}
	disk := &mcpRequestDisk{}
	a.mcpPruneRequestTombsLocked(time.Now())
	for id, req := range a.mcpReqs {
		if req == nil {
			continue
		}
		if strings.TrimSpace(req.ProjectPath) == "" && !req.done {
			continue
		}
		if req.failed && strings.TrimSpace(req.ProjectPath) == "" {
			continue
		}
		updated := req.updated
		if updated.IsZero() {
			updated = time.Now()
		}
		disk.Requests = append(disk.Requests, mcpRequestSnapshot{
			ID:          id,
			Hash:        req.Hash,
			Kind:        req.Kind,
			Command:     req.Command,
			Prompt:      req.Prompt,
			ProjectPath: req.ProjectPath,
			Error:       req.errText,
			Failed:      req.failed,
			Done:        req.done,
			RunID:       req.RunID,
			State:       req.State,
			UpdatedAt:   updated,
		})
	}
	for _, tomb := range a.mcpReqTombs {
		if strings.TrimSpace(tomb.ID) == "" {
			continue
		}
		disk.Tombs = append(disk.Tombs, tomb)
	}
	return disk
}

func (a *App) mcpPruneRequestTombsLocked(now time.Time) {
	for id, tomb := range a.mcpReqTombs {
		if !mcpRequestTombFresh(tomb.At, now) {
			delete(a.mcpReqTombs, id)
		}
	}
}

func (a *App) mcpTombRequestLocked(id string) {
	id = strings.TrimSpace(id)
	if a == nil || id == "" {
		return
	}
	if a.mcpReqTombs == nil {
		a.mcpReqTombs = map[string]mcpRequestTomb{}
	}
	if _, ok := a.mcpReqTombs[id]; ok {
		return
	}
	a.mcpReqTombs[id] = mcpRequestTomb{ID: id, At: time.Now()}
}

func mcpSnapshotFromRun(rec *mcpRunRecord) *mcpRunSnapshot {
	if rec == nil {
		return nil
	}
	clone := mcpCloneRun(rec)
	return &mcpRunSnapshot{
		ProjectPath:        clone.ProjectPath,
		Name:               clone.Name,
		Mode:               clone.Mode,
		Status:             clone.Status,
		Text:               clone.Text,
		Error:              clone.Error,
		Provider:           clone.Provider,
		Model:              clone.Model,
		RequestID:          clone.RequestID,
		ConfirmationKind:   clone.ConfirmationKind,
		Commands:           clone.Commands,
		Workspace:          clone.Workspace,
		Round:              clone.Round,
		StartedAt:          clone.StartedAt,
		FinishedAt:         clone.FinishedAt,
		EndReason:          clone.EndReason,
		EndDetail:          clone.EndDetail,
		ConfirmationID:     clone.ConfirmationID,
		ConfirmFingerprint: clone.confirmFingerprint,
		LogSeq:             clone.logSeq,
		Logs:               clone.logs,
		HistoryCount:       clone.historyCount,
		HistorySeal:        clone.historySeal,
		RemoteHost:         clone.RemoteHost,
		RemoteUser:         clone.RemoteUser,
		RemotePort:         clone.RemotePort,
		RemoteWorkDir:      clone.RemoteWorkDir,
		Steps:              clone.Steps,
		Checks:             clone.Checks,
		RunID:              clone.RunID,
		StatusVersion:      clone.StatusVersion,
		ParentTaskID:       clone.ParentTaskID,
		ChildTaskID:        clone.ChildTaskID,
		Children:           clone.Children,
		Stop:               clone.Stop,
		PlanID:             clone.PlanID,
		PlanVersion:        clone.PlanVersion,
		ProviderRequested:  clone.ProviderRequested,
		ModelRequested:     clone.ModelRequested,
		ProviderEffective:  clone.ProviderEffective,
		ModelEffective:     clone.ModelEffective,
		WorkDirRequested:   clone.WorkDirRequested,
		SessionPath:        clone.SessionPath,
	}
}

func mcpRunFromSnapshot(snap *mcpRunSnapshot) *mcpRunRecord {
	if snap == nil {
		return nil
	}
	return &mcpRunRecord{
		ProjectPath:        snap.ProjectPath,
		Name:               snap.Name,
		Mode:               snap.Mode,
		Status:             snap.Status,
		Text:               snap.Text,
		Error:              snap.Error,
		Provider:           snap.Provider,
		Model:              snap.Model,
		RequestID:          snap.RequestID,
		ConfirmationKind:   snap.ConfirmationKind,
		Commands:           append([]string(nil), snap.Commands...),
		Workspace:          snap.Workspace,
		Round:              snap.Round,
		StartedAt:          snap.StartedAt,
		FinishedAt:         snap.FinishedAt,
		EndReason:          snap.EndReason,
		EndDetail:          snap.EndDetail,
		ConfirmationID:     snap.ConfirmationID,
		confirmFingerprint: snap.ConfirmFingerprint,
		logSeq:             snap.LogSeq,
		logs:               append([]mcpLogEvent(nil), snap.Logs...),
		historyCount:       snap.HistoryCount,
		historySeal:        snap.HistorySeal,
		RemoteHost:         snap.RemoteHost,
		RemoteUser:         snap.RemoteUser,
		RemotePort:         snap.RemotePort,
		RemoteWorkDir:      snap.RemoteWorkDir,
		Steps:              mcpCloneSteps(snap.Steps),
		Checks:             mcpCloneChecks(snap.Checks),
		RunID:              snap.RunID,
		StatusVersion:      snap.StatusVersion,
		ParentTaskID:       snap.ParentTaskID,
		ChildTaskID:        snap.ChildTaskID,
		Children:           mcpCloneChildren(snap.Children),
		Stop:               mcpCloneStop(snap.Stop),
		PlanID:             snap.PlanID,
		PlanVersion:        snap.PlanVersion,
		ProviderRequested:  snap.ProviderRequested,
		ModelRequested:     snap.ModelRequested,
		ProviderEffective:  snap.ProviderEffective,
		ModelEffective:     snap.ModelEffective,
		WorkDirRequested:   snap.WorkDirRequested,
		SessionPath:        snap.SessionPath,
	}
}

func mcpRunFilePath(dir, projectPath string) string {
	sum := sha256.Sum256([]byte(normalizeProjectSessionPath(projectPath)))
	return filepath.Join(dir, "runs", hex.EncodeToString(sum[:])+".json")
}

func mcpRunLogPath(dir, projectPath string) string {
	return mcpRunSidecarPath(mcpRunFilePath(dir, projectPath))
}

func mcpRunSidecarPath(snapshotPath string) string {
	return strings.TrimSuffix(snapshotPath, ".json") + ".log.jsonl"
}

func mcpAppendLogSidecar(path string, ev mcpLogEvent, statusVersion int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(mcpLogSidecarLine{mcpLogEvent: ev, StatusVersion: statusVersion})
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	offset := info.Size()
	if offset > 0 {
		last := []byte{0}
		if _, readErr := file.ReadAt(last, offset-1); readErr != nil {
			_ = file.Close()
			return readErr
		}
		// A crash can leave a partial row with no newline. Closing it keeps
		// the next row from being glued onto that fragment.
		if last[0] != '\n' {
			raw = append([]byte{'\n'}, raw...)
		}
	}
	_, writeErr := file.WriteAt(raw, offset)
	var syncErr error
	if writeErr == nil {
		syncErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func mcpDiscardLogSidecar(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

// mcpMergeLogSidecar applies rows whose seq is past the snapshot. Lines are
// sorted by seq: writers can reach disk out of order. A torn final line is
// skipped. SavedAt moves to the newest applied row so a long run is not
// expired just because the snapshot itself was not rewritten.
func mcpMergeLogSidecar(bundle *mcpDurableBundle, logPath string) {
	if bundle == nil || bundle.Current == nil {
		return
	}
	raw, err := os.ReadFile(logPath)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	high := bundle.Current.LogSeq
	for _, ev := range bundle.Current.Logs {
		if ev.Seq > high {
			high = ev.Seq
		}
	}
	var rows []mcpLogSidecarLine
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var row mcpLogSidecarLine
		if json.Unmarshal(line, &row) != nil || row.Seq <= high {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	last := high
	for _, row := range rows {
		if row.Seq <= last {
			continue
		}
		bundle.Current.Logs = append(bundle.Current.Logs, row.mcpLogEvent)
		if row.Seq > bundle.Current.LogSeq {
			bundle.Current.LogSeq = row.Seq
		}
		if row.StatusVersion > bundle.Current.StatusVersion {
			bundle.Current.StatusVersion = row.StatusVersion
		}
		if ts, parseErr := time.Parse(time.RFC3339, row.At); parseErr == nil && ts.After(bundle.SavedAt) {
			bundle.SavedAt = ts
		}
		last = row.Seq
	}
	if len(bundle.Current.Logs) > mcpLogLimit {
		bundle.Current.Logs = append([]mcpLogEvent(nil), bundle.Current.Logs[len(bundle.Current.Logs)-mcpLogLimit:]...)
	}
}

func mcpReadJSON(path string, dest any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}

func mcpWriteJSON(path string, value any) error {
	return mcpWriteJSONMode(path, value, true)
}

func mcpWriteJSONMode(path string, value any, flush bool) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return mcpAtomicWrite(path, raw, flush)
}

func mcpAtomicWrite(path string, raw []byte, flush bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(raw)
	if writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return writeErr
	}
	if flush {
		if syncErr := tmp.Sync(); syncErr != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return syncErr
		}
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpName)
		return closeErr
	}
	// A failed replace must keep the previous snapshot. Deleting it first
	// would drop the last good copy when the new name cannot be published.
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
