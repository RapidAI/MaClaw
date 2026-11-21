package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	mcpHostAPIVersion       = 1
	mcpLogLimit             = 400
	mcpLogPageSize          = 100
	mcpLogTextRunes         = 2000
	mcpArtifactMaxFiles     = 200
	mcpArtifactMaxDepth     = 6
	mcpArtifactMaxRead      = 1 << 20
	mcpArtifactMaxDownload  = 16 << 20
	mcpArtifactMaxHash      = 32 << 20
	mcpArtifactHashBudget   = 32 << 20
	mcpClientRequestLimit   = 128
	mcpClientRequestIDRunes = 128
	mcpRequestKindCreate    = "create"
	mcpRequestKindCommand   = "command"
	mcpRequestKindRetry     = "retry"
	mcpConfirmIDPrefix      = "mcf_"
)

// mcpLogEvent is one cursor row. The task text stays a separate field;
// callers page through these instead of receiving the whole transcript.
type mcpLogEvent struct {
	Seq   int    `json:"seq"`
	At    string `json:"at"`
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text,omitempty"`
	OK    *bool  `json:"ok,omitempty"`
	RunID string `json:"run_id,omitempty"`
}

type mcpLogPage struct {
	ProjectPath   string        `json:"project_path"`
	RunID         string        `json:"run_id,omitempty"`
	StatusVersion int           `json:"status_version"`
	Events        []mcpLogEvent `json:"events"`
	NextCursor    int           `json:"next_cursor"`
	Gap           bool          `json:"gap,omitempty"`
	Empty         bool          `json:"empty,omitempty"`
}

type mcpArtifactFile struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256,omitempty"`
	HashSkipped bool   `json:"hash_skipped,omitempty"`
}

type mcpArtifactList struct {
	ProjectPath     string            `json:"project_path"`
	Files           []mcpArtifactFile `json:"files"`
	Truncated       bool              `json:"truncated,omitempty"`
	TruncatedReason string            `json:"truncated_reason,omitempty"`
	NextCursor      string            `json:"next_cursor,omitempty"`
	Scope           string            `json:"scope,omitempty"`
}

type mcpArtifactBody struct {
	ProjectPath string `json:"project_path"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	Text        string `json:"text,omitempty"`
	Content     string `json:"content,omitempty"`
	Snapshot    bool   `json:"snapshot,omitempty"`
}

type mcpRequestScope struct {
	TaskID string `json:"task_id,omitempty"`
	Action string `json:"action,omitempty"`
}

type mcpRequestView struct {
	Accepted        bool             `json:"accepted"`
	Pending         bool             `json:"pending,omitempty"`
	ClientRequestID string           `json:"client_request_id"`
	Kind            string           `json:"kind,omitempty"`
	ProjectPath     string           `json:"project_path,omitempty"`
	Error           string           `json:"error,omitempty"`
	Task            *mcpTaskView     `json:"task,omitempty"`
	State           string           `json:"state"`
	PayloadHash     string           `json:"payload_hash,omitempty"`
	Scope           *mcpRequestScope `json:"scope,omitempty"`
	RunID           string           `json:"run_id,omitempty"`
}

type mcpBrowserMethod struct {
	Name      string   `json:"name"`
	Tool      string   `json:"tool"`
	Action    string   `json:"action"`
	Available bool     `json:"available"`
	Arguments []string `json:"arguments,omitempty"`
}

type mcpCapabilityView struct {
	APIVersion      int                  `json:"api_version"`
	ProtocolVersion string               `json:"protocol_version"`
	Features        []string             `json:"features"`
	ModelConnection string               `json:"model_connection"`
	Provider        string               `json:"provider,omitempty"`
	Model           string               `json:"model,omitempty"`
	BrowserTest     bool                 `json:"browser_test"`
	Screenshot      bool                 `json:"screenshot"`
	Shell           bool                 `json:"shell"`
	BrowserMethods  []mcpBrowserMethod   `json:"browser_methods"`
	SchemaVersion   string               `json:"schema_version,omitempty"`
	ServerBuild     string               `json:"server_build,omitempty"`
	Commit          string               `json:"commit"`
	Persistence     *mcpPersistenceView  `json:"persistence,omitempty"`
	CurrentModel    *mcpCurrentModelView `json:"current_model,omitempty"`
	FeatureFlags    *mcpFeatureFlagView  `json:"feature_flags,omitempty"`
}

type mcpPersistenceView struct {
	Durable   bool   `json:"durable"`
	Retention string `json:"retention"`
}

type mcpCurrentModelView struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Configured bool   `json:"configured"`
	ProbeOK    *bool  `json:"probe_ok"`
}

type mcpFeatureFlagView struct {
	DurableRuns           bool `json:"durable_runs"`
	ExpectedStatusVersion bool `json:"expected_status_version"`
	DryRun                bool `json:"dry_run"`
	ArtifactPaging        bool `json:"artifact_paging"`
	ExpectedSHA256        bool `json:"expected_sha256"`
}

// mcpClientRequest remembers a caller-supplied id so a timed-out retry does
// not start a second turn. Accepted requests are written with the run snapshot.
type mcpClientRequest struct {
	Kind        string
	Command     string
	Prompt      string
	ProjectPath string
	errText     string
	failed      bool
	done        bool
	ready       chan struct{}
	closeOnce   sync.Once
	Hash        string
	RunID       string
	State       string
	updated     time.Time
}

func (a *App) mcpPublishTaskView(view mcpTaskView) mcpTaskView {
	if a == nil {
		return view
	}
	if strings.TrimSpace(view.WorkingDir) == "" {
		view.WorkingDir = strings.TrimSpace(view.workspace)
	}
	view = a.mcpFillProviderModel(view)
	view = mcpSealTaskIdentity(view)
	// current_run_id is the live round. The viewed run_id stays on this session.
	if live := a.mcpLiveRunID(view.ProjectPath); live != "" {
		view.CurrentRunID = live
	}
	view.Text = mcpRelabelSessionWorkspace(view.Text, view.SessionPath, firstNonEmpty(view.WorkingDirEffective, view.WorkingDir, view.workspace))
	view = mcpGuardCompletion(view)
	// Mint the session before the confirmation id. The id is stored on the
	// live record, and a record created below has to exist first or the next
	// read would mint a second confirmation id.
	view = a.mcpAssignPlanSession(view)
	view = a.mcpMergeHolderRunningGates(view)
	view = a.mcpEnsureConfirmationID(view)
	if mcpConfirmationBindsPlan(view.ConfirmationKind, view.Commands) {
		view.Text = mcpAttachSessionLine(view.Text, mcpSessionDecisionLine(firstNonEmpty(view.RunID, view.CurrentRunID), view.ConfirmationID))
	}
	view = mcpPublishChildren(view)
	view = a.mcpAnnotateStepFreshness(view)
	view.Outcome = mcpCommandOutcome(view)
	return view
}

// mcpAssignPlanSession gives a live coding-plan card a run id when the round
// never received one. A desktop task can show /plan approve with both run
// ids empty, and the caller then has no session to approve. The id is stored
// on the live record and status_version is left alone: a read must not make
// the next send_command look like a conflict. A task with no record gets an
// idle session holder, not a copy of the plan card. A card that is not a
// plan, and a legacy row that is not waiting on a plan, keep an empty run id.
func (a *App) mcpAssignPlanSession(view mcpTaskView) mcpTaskView {
	if a == nil || !mcpConfirmationBindsPlan(view.ConfirmationKind, view.Commands) {
		return view
	}
	if strings.TrimSpace(view.RunID) != "" {
		return view
	}
	if !a.mcpViewIsLiveRound(view) {
		return view
	}
	if id := strings.TrimSpace(view.CurrentRunID); id != "" {
		view.RunID = id
		return view
	}
	path := normalizeProjectSessionPath(view.ProjectPath)
	if path == "" {
		return view
	}
	id := ""
	persist := false
	a.mcpRunsMu.Lock()
	rec := a.mcpRuns[path]
	switch {
	case rec != nil && strings.TrimSpace(rec.RunID) != "":
		id = strings.TrimSpace(rec.RunID)
	case rec != nil:
		rec.RunID = mcpNewRunID()
		id = rec.RunID
		persist = true
	case rec == nil:
		id = mcpNewRunID()
		if a.mcpRuns == nil {
			a.mcpRuns = map[string]*mcpRunRecord{}
		}
		// The record only holds the session. The live plan is applied on read,
		// so a cleared plan does not keep offering approve, and a later
		// desktop turn is not stuck on needs_confirmation. mcpCommitRun
		// would force status_version to at least 1.
		a.mcpRuns[path] = &mcpRunRecord{
			ProjectPath:   path,
			Name:          view.Name,
			Mode:          view.Mode,
			Status:        mcpStatusIdle,
			Workspace:     firstNonEmpty(view.workspace, view.WorkingDir),
			RunID:         id,
			StatusVersion: view.StatusVersion,
		}
		persist = true
	}
	a.mcpRunsMu.Unlock()
	if persist {
		a.mcpPersistRun(path)
	}
	if id == "" {
		return view
	}
	view.RunID = id
	view.CurrentRunID = id
	return view
}

// mcpUnsealedSessionHolder is the idle record a read mints so a desktop plan
// has a session id. It is not a prompt and not a confirmation round: its
// confirmation id must not gate retry, and the record must not hide the
// desktop task.
func mcpUnsealedSessionHolder(view mcpTaskView) bool {
	return view.Status == mcpStatusIdle &&
		strings.TrimSpace(view.Text) == "" &&
		strings.TrimSpace(view.RequestID) == "" &&
		strings.TrimSpace(view.ConfirmationKind) == "" &&
		len(view.Commands) == 0 &&
		len(view.Steps) == 0 &&
		view.historyCount == 0 &&
		strings.TrimSpace(view.historySeal) == ""
}

// mcpProjectSessionHolder keeps the holder's session id and status_version.
// A live confirmation cannot use a running, failed, or cancelled snapshot:
// those statuses skip the overlay. The transcript and the error still belong
// on the card, and the status stays idle so the overlay still runs. Every
// other snapshot is the task, with this session id attached.
func mcpProjectSessionHolder(holder, store mcpTaskView, confirmationPending bool) mcpTaskView {
	if confirmationPending {
		switch store.Status {
		case mcpStatusRunning, mcpStatusFailed, mcpStatusCancelled:
			kept := holder
			if text := strings.TrimSpace(store.Text); text != "" {
				kept.Text = store.Text
			}
			if errText := strings.TrimSpace(store.Error); errText != "" {
				kept.Error = errText
			}
			if strings.TrimSpace(kept.Name) == "" {
				kept.Name = store.Name
			}
			if strings.TrimSpace(kept.Mode) == "" {
				kept.Mode = store.Mode
			}
			if strings.TrimSpace(kept.workspace) == "" {
				kept.workspace = firstNonEmpty(store.workspace, store.WorkingDir)
			}
			if strings.TrimSpace(kept.WorkingDir) == "" {
				kept.WorkingDir = firstNonEmpty(store.WorkingDir, store.workspace)
			}
			return kept
		}
	}
	merged := mcpFillTaskView(store, holder)
	if id := strings.TrimSpace(holder.RunID); id != "" {
		merged.RunID = id
		merged.CurrentRunID = id
	}
	merged.StatusVersion = holder.StatusVersion
	return merged
}

// mcpMergeHolderRunningGates keeps a desktop scope or critical gate on the
// live session holder's confirmation card. Keeping that card idle lets the
// plan, review, dialogue, confirm, or ask overlay run, and the same commands
// are part of the confirmation id that send_command checks.
func (a *App) mcpMergeHolderRunningGates(view mcpTaskView) mcpTaskView {
	if a == nil || view.Status == mcpStatusRunning {
		return view
	}
	switch strings.TrimSpace(view.ConfirmationKind) {
	case "", "scope", "critical":
		return view
	}
	if !a.mcpViewIsLiveRound(view) {
		return view
	}
	path := normalizeProjectSessionPath(view.ProjectPath)
	reg, ok := a.mcpViewFromRegistry(path)
	if !ok || !mcpUnsealedSessionHolder(reg) {
		return view
	}
	return mcpApplyRunningGateOverlay(view, a.mcpCollectRunningGates(view), false)
}

// mcpLiveConfirmationPending is true when a read of this idle session would
// attach a confirmation card. The check is that overlay, so a plan, a review,
// a dialogue, a confirm, and an ask all keep the card in front of a running,
// failed, or cancelled desktop snapshot.
func (a *App) mcpLiveConfirmationPending(projectPath string) bool {
	if a == nil || strings.TrimSpace(projectPath) == "" {
		return false
	}
	probed := a.mcpApplyLiveConfirmation(mcpTaskView{
		ProjectPath: normalizeProjectSessionPath(projectPath),
		Status:      mcpStatusIdle,
	})
	return strings.TrimSpace(probed.ConfirmationKind) != "" || len(probed.Commands) > 0
}

func (a *App) mcpReturnTask(view mcpTaskView) mcpTaskView {
	if a == nil {
		return view
	}
	if view.Status == mcpStatusRunning {
		view = a.mcpAnnotateRunningGates(view)
		view.EndReason = ""
		view.EndDetail = ""
		view.FinishedAt = ""
		view.Stop = nil
	} else {
		// The desktop plan, ask, and confirm card belong to the live round.
		// An older command session keeps the result stored for that run.
		if a.mcpViewIsLiveRound(view) {
			view = a.mcpApplyLiveConfirmation(view)
		}
		if view.Status == mcpStatusNeedsConfirmation {
			view.EndReason = mcpStatusNeedsConfirmation
			if strings.TrimSpace(view.EndDetail) == "" {
				view.EndDetail = view.ConfirmationKind
			}
		}
		if view.Status == mcpStatusWaitingReview {
			if strings.TrimSpace(view.EndReason) == "" {
				view.EndReason = mcpStatusWaitingReview
			}
			if strings.TrimSpace(view.EndDetail) == "" && view.Stop != nil {
				view.EndDetail = view.Stop.Reason
			}
		}
	}
	return a.mcpPublishTaskView(view)
}

func (a *App) mcpLiveRunID(path string) string {
	path = normalizeProjectSessionPath(path)
	if a == nil || path == "" {
		return ""
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil {
		return ""
	}
	return strings.TrimSpace(rec.RunID)
}

// mcpViewIsLiveRound is false for a stored command session that is no longer
// the registry's current run. An empty run id is the task-level read.
func (a *App) mcpViewIsLiveRound(view mcpTaskView) bool {
	runID := strings.TrimSpace(view.RunID)
	if runID == "" {
		return true
	}
	live := a.mcpLiveRunID(view.ProjectPath)
	if live == "" {
		return false
	}
	return live == runID
}

func (a *App) mcpFillProviderModel(view mcpTaskView) mcpTaskView {
	provider := strings.TrimSpace(view.Provider)
	model := strings.TrimSpace(view.Model)
	if provider != "" && model != "" {
		view.Provider = provider
		view.Model = model
		if strings.TrimSpace(view.ProviderEffective) == "" {
			view.ProviderEffective = provider
		}
		if strings.TrimSpace(view.ModelEffective) == "" {
			view.ModelEffective = model
		}
		return view
	}
	// A half-filled pair is the record's own value. Filling the blank from
	// another source would pair one provider with a different model's name.
	if provider != "" || model != "" {
		view.Provider = provider
		view.Model = model
		return view
	}
	// Only a live run may take the pin captured for this session. A finished
	// or historical row keeps an empty pair instead of today's configuration.
	if view.Status == mcpStatusRunning {
		if pin, ok := a.mcpPinNames(view.ProjectPath); ok {
			view.Provider = pin.ProviderName
			view.Model = pin.Model
			if strings.TrimSpace(view.ProviderEffective) == "" {
				view.ProviderEffective = pin.ProviderName
			}
			if strings.TrimSpace(view.ModelEffective) == "" {
				view.ModelEffective = pin.Model
			}
		}
	}
	return view
}

func (a *App) mcpPinNames(projectPath string) (mcpModelPin, bool) {
	if a == nil {
		return mcpModelPin{}, false
	}
	userID := desktopAIAssistantUserIDForProjectPath(projectPath)
	if userID == "" || userID == desktopUserID {
		return mcpModelPin{}, false
	}
	a.mcpPinsMu.Lock()
	defer a.mcpPinsMu.Unlock()
	pin, ok := a.mcpPins[userID]
	if !ok || strings.TrimSpace(pin.ProviderName) == "" || strings.TrimSpace(pin.Model) == "" {
		return mcpModelPin{}, false
	}
	return pin, true
}

func (a *App) mcpNextRound(path string) int {
	if prev, ok := a.mcpViewFromRegistry(path); ok && prev.Round > 0 {
		return prev.Round + 1
	}
	return 1
}

func mcpFormatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339)
}

func mcpConfirmationFingerprint(kind string, commands []string) string {
	return strings.TrimSpace(kind) + "\n" + strings.Join(commands, "\n")
}

// mcpConfirmationBindsPlan reports whether this card approves or replaces
// the desktop's pending coding plan. The command list stays the same when
// that plan is edited, so the fingerprint has to include the plan itself.
func mcpConfirmationBindsPlan(kind string, commands []string) bool {
	switch strings.TrimSpace(kind) {
	case mcpConfirmKindPlan, mcpConfirmKindPlanReview, mcpConfirmKindPlanDialogue:
		return true
	}
	for _, command := range commands {
		switch strings.TrimSpace(command) {
		case "/plan approve", "/plan skip", "/plan reject":
			return true
		}
	}
	return false
}

// mcpPendingPlanStamp is the pending plan a plan confirmation would act on.
// An empty stamp leaves the fingerprint as kind plus commands.
func (a *App) mcpPendingPlanStamp(projectPath string) string {
	handler := a.mcpReadyIMHandler()
	if handler == nil {
		return ""
	}
	userID := desktopAIAssistantUserIDForProjectPath(projectPath)
	if userID == "" || userID == desktopUserID {
		return ""
	}
	mem := handler.getStickyCodingWorkbenchMemory(userID)
	parts := []string{
		strings.TrimSpace(mem.PendingPlanJSON),
		strings.TrimSpace(mem.PendingPlanMarkdown),
		strings.TrimSpace(mem.PlanDialogueNotes),
	}
	if parts[0] == "" && parts[1] == "" && parts[2] == "" {
		return ""
	}
	return strings.Join(parts, "\n")
}

func (a *App) mcpEnsureConfirmationID(view mcpTaskView) mcpTaskView {
	if strings.TrimSpace(view.ConfirmationKind) == "" && len(view.Commands) == 0 {
		view.ConfirmationID = ""
		return view
	}
	fp := mcpConfirmationFingerprint(view.ConfirmationKind, view.Commands)
	path := normalizeProjectSessionPath(view.ProjectPath)
	if a == nil || path == "" {
		view.ConfirmationID = ""
		return view
	}
	// Read the plan before taking the run lock. The workbench load must not
	// sit behind that lock, and a plan edit has to change this fingerprint.
	if mcpConfirmationBindsPlan(view.ConfirmationKind, view.Commands) {
		if stamp := a.mcpPendingPlanStamp(path); stamp != "" {
			fp += "\n" + stamp
		}
	}
	// Persist only after this lock is released. mcpPersistRun takes the disk
	// lock and then the run lock again.
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		a.mcpRunsMu.Unlock()
		if persist {
			a.mcpPersistRun(path)
		}
	}()
	// Only the live round owns the id stored on the record. Reading an older
	// command session must not replace the confirmation the current round shows.
	viewRun := strings.TrimSpace(firstNonEmpty(view.RunID, view.CurrentRunID))
	if rec := a.mcpRuns[path]; rec != nil && (viewRun == "" || strings.TrimSpace(rec.RunID) == viewRun) {
		// The run record is the only live id. Drop ids minted before it existed.
		a.mcpDropConfirmIDsLocked(path)
		// A stored fingerprint is the card that was shown, including the pending
		// plan. An older snapshot has none: keep the id when the loaded kind
		// and commands still match. A different card mints a new id.
		cardFP := rec.confirmFingerprint
		if cardFP == "" {
			cardFP = mcpConfirmationFingerprint(rec.ConfirmationKind, rec.Commands)
		}
		if strings.HasPrefix(rec.ConfirmationID, mcpConfirmIDPrefix) && cardFP == fp {
			rec.confirmFingerprint = fp
			view.ConfirmationID = rec.ConfirmationID
			return view
		}
		rec.ConfirmationID = mcpConfirmIDPrefix + randomHexID(12)
		rec.confirmFingerprint = fp
		view.ConfirmationID = rec.ConfirmationID
		persist = true
		return view
	}
	// This round is not the live record. Keep its id beside the path and the
	// run, and do not drop the id the current round is showing.
	key := path + "\x00" + viewRun + "\x00" + fp
	if a.mcpConfirmIDs == nil {
		a.mcpConfirmIDs = map[string]string{}
	}
	// The id minted while this round was current stays on the archived record.
	// A later read must keep that same id, not mint another one.
	if stored := strings.TrimSpace(view.ConfirmationID); strings.HasPrefix(stored, mcpConfirmIDPrefix) {
		a.mcpConfirmIDs[key] = stored
		return view
	}
	if id := a.mcpConfirmIDs[key]; strings.HasPrefix(id, mcpConfirmIDPrefix) {
		view.ConfirmationID = id
		if a.mcpStoreArchivedConfirmationLocked(path, viewRun, id) {
			persist = true
		}
		return view
	}
	id := mcpConfirmIDPrefix + randomHexID(12)
	a.mcpConfirmIDs[key] = id
	view.ConfirmationID = id
	if a.mcpStoreArchivedConfirmationLocked(path, viewRun, id) {
		persist = true
	}
	return view
}

func (a *App) mcpStoreArchivedConfirmationLocked(path, runID, id string) bool {
	runID = strings.TrimSpace(runID)
	if a == nil || path == "" || runID == "" || !strings.HasPrefix(id, mcpConfirmIDPrefix) {
		return false
	}
	for _, old := range a.mcpRunArchive[path] {
		if old == nil || old.RunID != runID {
			continue
		}
		if old.ConfirmationID == id {
			return false
		}
		old.ConfirmationID = id
		return true
	}
	return false
}

func (a *App) mcpDropConfirmIDsLocked(path string) {
	if a == nil || len(a.mcpConfirmIDs) == 0 {
		return
	}
	prefix := path + "\x00"
	for key := range a.mcpConfirmIDs {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		// path, run, fingerprint is one command session. Refreshing the live
		// round drops only ids that were minted before a run existed.
		if strings.Contains(key[len(prefix):], "\x00") {
			continue
		}
		delete(a.mcpConfirmIDs, key)
	}
}

func (a *App) mcpConsumeConfirmation(path, id string) error {
	id = strings.TrimSpace(id)
	path = normalizeProjectSessionPath(path)
	if a == nil || path == "" || !strings.HasPrefix(id, mcpConfirmIDPrefix) {
		return errors.New("确认编号不匹配或已失效")
	}
	// Persist only after this lock is released. mcpPersistRun takes the disk
	// lock and then the run lock again.
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		a.mcpRunsMu.Unlock()
		if persist {
			a.mcpPersistRun(path)
		}
	}()
	if rec := a.mcpRuns[path]; rec != nil && rec.ConfirmationID == id {
		rec.ConfirmationID = ""
		rec.confirmFingerprint = ""
		// A minted id is on disk. Clearing it only in memory lets the next
		// process accept a confirmation this process already used.
		persist = true
		return nil
	}
	prefix := path + "\x00"
	for key, have := range a.mcpConfirmIDs {
		if have == id && strings.HasPrefix(key, prefix) {
			delete(a.mcpConfirmIDs, key)
			return nil
		}
	}
	return errors.New("确认编号不匹配或已失效")
}

func (a *App) mcpCurrentConfirmation(path string, reg mcpTaskView, inRegistry bool) mcpTaskView {
	view := reg
	if !inRegistry {
		view = mcpTaskView{ProjectPath: normalizeProjectSessionPath(path), workspace: reg.workspace}
	}
	if strings.TrimSpace(view.ProjectPath) == "" {
		view.ProjectPath = normalizeProjectSessionPath(path)
	}
	if view.Status == mcpStatusRunning {
		view = a.mcpAnnotateRunningGates(view)
	} else {
		view = a.mcpApplyLiveConfirmation(view)
	}
	return a.mcpPublishTaskView(view)
}

func mcpIsStructuralDecision(command string) bool {
	switch mcpCommandHead(command) {
	case "__scope_approval__", "__resolve_critical_confirm__",
		"__confirm_execution__", "__cancel_execution__",
		"__coding_recovery_confirm__", "__coding_recovery_decline__":
		return true
	}
	switch strings.TrimSpace(command) {
	case "/plan approve", "/plan skip", "/plan reject":
		return true
	}
	return false
}

func mcpCommandNeedsConfirmationID(command string, commands []string) bool {
	if mcpIsStructuralDecision(command) {
		return true
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	for _, item := range commands {
		if item == command {
			return true
		}
	}
	return false
}

func (a *App) mcpRequireListedConfirmation(path string, reg mcpTaskView, inRegistry bool, command, confirmationID string) error {
	published := a.mcpCurrentConfirmation(path, reg, inRegistry)
	if strings.TrimSpace(published.ConfirmationID) == "" || !mcpCommandNeedsConfirmationID(command, published.Commands) {
		return nil
	}
	if published.ConfirmationID != strings.TrimSpace(confirmationID) {
		return errors.New("确认编号不匹配或已失效")
	}
	return a.mcpConsumeConfirmation(published.ProjectPath, confirmationID)
}

func (a *App) mcpRequireLiveConfirmation(path string, reg mcpTaskView, inRegistry bool, confirmationID string) error {
	published := a.mcpCurrentConfirmation(path, reg, inRegistry)
	if strings.TrimSpace(published.ConfirmationID) == "" || published.ConfirmationID != strings.TrimSpace(confirmationID) {
		return errors.New("确认编号不匹配或已失效")
	}
	return a.mcpConsumeConfirmation(published.ProjectPath, confirmationID)
}

func (a *App) mcpPrepareLiveGate(projectPath string, reg mcpTaskView, inRegistry bool, command string) (bool, error) {
	switch mcpCommandHead(command) {
	case "__scope_approval__":
		return true, a.mcpValidateScopeCommand(projectPath, reg, inRegistry, command)
	case "__resolve_critical_confirm__":
		return true, a.mcpValidateCriticalCommand(projectPath, reg, inRegistry, command)
	default:
		return false, nil
	}
}

var (
	mcpRedactMCK    = regexp.MustCompile(`mck_[A-Za-z0-9]+`)
	mcpRedactSK     = regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`)
	mcpRedactGitHub = regexp.MustCompile(`\b(?:ghp|gho|github_pat)_[A-Za-z0-9_]{16,}`)
	mcpRedactAWS    = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	mcpRedactPEM    = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	mcpRedactBearer = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-+/=]{8,}`)
	mcpRedactAssign = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|access[_-]?key|token|password|secret)["']?)\s*[=:]\s*["']?[^"'\s,;}&]{4,}["']?`)
)

func mcpRedactSecrets(text string) string {
	if text == "" {
		return ""
	}
	text = mcpRedactPEM.ReplaceAllString(text, "[redacted private key]")
	text = mcpRedactMCK.ReplaceAllString(text, "mck_[redacted]")
	text = mcpRedactSK.ReplaceAllString(text, "sk-[redacted]")
	text = mcpRedactGitHub.ReplaceAllString(text, "[redacted]")
	text = mcpRedactAWS.ReplaceAllString(text, "[redacted]")
	text = mcpRedactBearer.ReplaceAllString(text, "Bearer [redacted]")
	text = mcpRedactAssign.ReplaceAllString(text, "$1=[redacted]")
	return text
}

func mcpClipForLog(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) <= 8000 {
		return text
	}
	return string([]rune(text)[:8000])
}

func mcpTrimLog(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <= mcpLogTextRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:mcpLogTextRunes]) + "…"
}

func mcpPushLogLocked(rec *mcpRunRecord, ev mcpLogEvent) {
	if rec == nil {
		return
	}
	ev.Text = mcpTrimLog(mcpRedactSecrets(ev.Text))
	ev.Name = mcpTrimLog(mcpRedactSecrets(ev.Name))
	if ev.Text == "" && ev.Name == "" {
		return
	}
	if strings.TrimSpace(ev.RunID) == "" {
		ev.RunID = rec.RunID
	}
	rec.logSeq++
	ev.Seq = rec.logSeq
	if ev.At == "" {
		ev.At = time.Now().Format(time.RFC3339)
	}
	rec.logs = append(rec.logs, ev)
	if len(rec.logs) > mcpLogLimit {
		rec.logs = append([]mcpLogEvent(nil), rec.logs[len(rec.logs)-mcpLogLimit:]...)
	}
	rec.progressAt = time.Now()
	mcpTouchLocked(rec)
}

func (a *App) mcpAppendLog(path, requestID string, ev mcpLogEvent) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	var saved mcpLogEvent
	version := 0
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		if persist {
			// One synced line beside the snapshot. Folding that file back
			// into the snapshot waits until it reaches the size bound, and
			// that rewrite is itself synced.
			a.mcpPersistLogLine(path, saved, version)
		}
	}()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	before := rec.logSeq
	mcpPushLogLocked(rec, ev)
	if rec.logSeq == before || len(rec.logs) == 0 {
		return
	}
	saved = rec.logs[len(rec.logs)-1]
	version = rec.StatusVersion
	persist = true
}

func (a *App) mcpRecordToolEvent(path, requestID string, ev ACPToolEvent) {
	text := mcpToolLogText(ev)
	event := mcpLogEvent{Kind: "tool", Name: strings.TrimSpace(ev.Name), Text: text}
	if ev.Phase == "end" {
		ok := ev.OK
		event.OK = &ok
	}
	a.mcpAppendLog(path, requestID, event)
	if ev.Phase == "end" {
		a.mcpNoteAdmittedChildren(path, requestID, ev.Result)
	}
}

func mcpToolLogText(ev ACPToolEvent) string {
	name := strings.TrimSpace(ev.Name)
	if name == "" {
		name = "tool"
	}
	phase := strings.TrimSpace(ev.Phase)
	if phase == "" {
		phase = "call"
	}
	parts := []string{phase, name}
	if title := strings.TrimSpace(ev.Title); title != "" && title != name {
		parts = append(parts, title)
	}
	if args := mcpClipForLog(ev.ArgsJSON); args != "" && args != "{}" {
		parts = append(parts, args)
	}
	if phase == "end" {
		if ev.OK {
			parts = append(parts, "ok")
		} else {
			parts = append(parts, "failed")
		}
		if result := mcpClipForLog(ev.Result); result != "" {
			parts = append(parts, result)
		}
	}
	return strings.Join(parts, " ")
}

// mcpLogSeqGap reports a missing sequence in the retained log up through the
// last event on this page. A later hole waits for the next page. A row that
// belongs to another round still occupies its seq, so it is not a gap.
func mcpLogSeqGap(logs []mcpLogEvent, cursor, last int) bool {
	if last <= cursor {
		return false
	}
	seqs := make([]int, 0, len(logs))
	for _, ev := range logs {
		if ev.Seq > cursor && ev.Seq <= last {
			seqs = append(seqs, ev.Seq)
		}
	}
	sort.Ints(seqs)
	prev := cursor
	for _, seq := range seqs {
		if seq <= prev {
			continue
		}
		if seq > prev+1 {
			return true
		}
		prev = seq
	}
	return false
}

func (a *App) mcpReadLog(ctx context.Context, projectPath, runID string, cursor int) (mcpLogPage, error) {
	if cursor < 0 {
		cursor = 0
	}
	path := normalizeProjectSessionPath(projectPath)
	runID = strings.TrimSpace(runID)
	if a == nil || path == "" {
		return mcpLogPage{}, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, runID)
	}
	a.mcpLoadDurable()
	a.mcpRunsMu.Lock()
	rec, err := a.mcpRunForReadLocked(path, runID)
	var page mcpLogPage
	if err == nil && rec != nil {
		events := make([]mcpLogEvent, 0)
		for _, ev := range rec.logs {
			if ev.Seq <= cursor || !mcpLogMatchesRun(ev, rec, runID) {
				continue
			}
			events = append(events, ev)
			if len(events) >= mcpLogPageSize {
				break
			}
		}
		next := cursor
		gap := false
		if n := len(events); n > 0 {
			next = events[n-1].Seq
			// Another round's row is not a lost row. A hole after this page
			// is reported when the caller asks for the next page.
			gap = mcpLogSeqGap(rec.logs, cursor, next)
		}
		pageRun := runID
		if pageRun == "" {
			pageRun = rec.RunID
		}
		page = mcpLogPage{ProjectPath: path, RunID: pageRun, StatusVersion: rec.StatusVersion, Events: events, NextCursor: next, Gap: gap, Empty: len(events) == 0}
	}
	a.mcpRunsMu.Unlock()
	if err == nil {
		return page, nil
	}
	var coded *mcpCallError
	if errors.As(err, &coded) && coded != nil && coded.Code == mcpCodeRunNotFound {
		return mcpLogPage{}, err
	}
	if a.mcpTaskKnown(path) {
		if runID == "" {
			return mcpLogPage{ProjectPath: path, Events: []mcpLogEvent{}, NextCursor: cursor, Empty: true}, nil
		}
		return mcpLogPage{}, mcpErr(mcpCodeRunNotFound, "没有这一轮执行", path, runID)
	}
	if a.mcpTaskExpiredOnly(path) {
		return mcpLogPage{}, mcpErr(mcpCodeTaskExpired, mcpTaskExpiredMessage, path, runID)
	}
	return mcpLogPage{}, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, runID)
}

func (a *App) mcpArtifactRoot(projectPath string) (string, mcpTaskView, error) {
	path := normalizeProjectSessionPath(projectPath)
	if a == nil || path == "" {
		return "", mcpTaskView{}, errors.New("没有找到这个任务")
	}
	view, ok := a.mcpViewFromRegistry(path)
	if !ok {
		view = mcpTaskView{ProjectPath: path}
		if a != nil {
			view = mcpViewWithTaskTags(view, a.remoteControlTaskSnapshot(path).Tags)
		}
	}
	if err := mcpRemoteArtifactError(view); err != nil {
		return "", view, err
	}
	if root := mcpExistingDir(view.workspace); root != "" {
		view.ProjectPath = path
		return root, view, nil
	}
	if root := mcpExistingDir(view.ProjectPath); root != "" {
		return root, view, nil
	}
	if root := mcpExistingDir(path); root != "" {
		view.ProjectPath = path
		return root, view, nil
	}
	return "", view, errors.New("任务还没有工作目录")
}

func mcpRemoteArtifactError(view mcpTaskView) error {
	if view.Mode == taskRemoteCodingDevTag || strings.TrimSpace(view.RemoteHost) != "" {
		return errors.New("远程工作目录在 SSH 主机上，不从本机读取")
	}
	return nil
}

func mcpExistingDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

func (a *App) mcpListArtifacts(ctx context.Context, projectPath string) (mcpArtifactList, error) {
	root, view, err := a.mcpArtifactRoot(projectPath)
	if err != nil {
		return mcpArtifactList{}, err
	}
	list := mcpWalkArtifacts(root, mcpArtifactWalkFrom(ctx))
	list.ProjectPath = view.ProjectPath
	if list.Files == nil {
		list.Files = []mcpArtifactFile{}
	}
	return list, nil
}

func (a *App) mcpReadArtifact(ctx context.Context, projectPath, rel string, download bool) (mcpArtifactBody, error) {
	root, view, err := a.mcpArtifactRoot(projectPath)
	if err != nil {
		return mcpArtifactBody{}, err
	}
	full, err := mcpResolveInside(root, rel)
	if err != nil {
		return mcpArtifactBody{}, err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return mcpArtifactBody{}, errors.New("工作目录里没有这个文件")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return mcpArtifactBody{}, errors.New("不能读取链接")
	}
	if info.IsDir() {
		return mcpArtifactBody{}, errors.New("这是目录")
	}
	if !info.Mode().IsRegular() {
		return mcpArtifactBody{}, errors.New("不能读取链接")
	}
	sensitive := mcpSensitiveInfos()
	if mcpArtifactSecretName(filepath.Base(full)) || mcpInfoIsSensitive(info, sensitive) {
		return mcpArtifactBody{}, errors.New("不能读取密钥文件")
	}
	limit := int64(mcpArtifactMaxRead)
	if download {
		limit = mcpArtifactMaxDownload
	} else if info.Size() > int64(mcpArtifactMaxRead) && info.Size() <= mcpArtifactMaxDownload {
		return mcpArtifactBody{}, errors.New("文件较大，请用 download 读取")
	}
	if info.Size() > limit {
		return mcpArtifactBody{}, errors.New("文件超过读取上限")
	}
	f, err := os.Open(full)
	if err != nil {
		return mcpArtifactBody{}, errors.New("读不到这个文件")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return mcpArtifactBody{}, errors.New("读不到这个文件")
	}
	if mcpInfoIsSensitive(opened, sensitive) {
		return mcpArtifactBody{}, errors.New("不能读取密钥文件")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return mcpArtifactBody{}, errors.New("读不到这个文件")
	}
	if int64(len(raw)) > limit {
		return mcpArtifactBody{}, errors.New("文件超过读取上限")
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if opts := mcpCallOptsFrom(ctx); opts.HasExpectedSHA && !mcpHashMatches(digest, opts.ExpectedSHA) {
		return mcpArtifactBody{}, mcpErr(mcpCodeArtifactStale, mcpArtifactStaleMessage, view.ProjectPath, "")
	}
	body := mcpArtifactBody{
		ProjectPath: view.ProjectPath,
		Path:        filepath.ToSlash(mcpRelativeInside(root, full)),
		Size:        info.Size(),
		SHA256:      digest,
		Snapshot:    true,
	}
	if download {
		body.Encoding = "base64"
		body.Content = base64.StdEncoding.EncodeToString(raw)
		return body, nil
	}
	if !utf8.Valid(raw) {
		return mcpArtifactBody{}, errors.New("这是二进制文件，请用 download 读取")
	}
	body.Encoding = "utf-8"
	body.Text = string(raw)
	return body, nil
}

type mcpArtifactWalk struct {
	Cursor        string
	Limit         int
	IncludeHidden bool
	Exclude       map[string]struct{}
}

func mcpArtifactWalkFrom(ctx context.Context) mcpArtifactWalk {
	opts := mcpCallOptsFrom(ctx)
	limit := mcpArtifactMaxFiles
	if opts.HasLimit && opts.Limit > 0 && opts.Limit < limit {
		limit = opts.Limit
	}
	exclude := map[string]struct{}{}
	for _, name := range opts.ExcludeDirs {
		name = strings.ToLower(strings.TrimSpace(filepath.ToSlash(name)))
		if name != "" {
			exclude[name] = struct{}{}
		}
	}
	return mcpArtifactWalk{Cursor: strings.TrimSpace(opts.ArtifactCursor), Limit: limit, IncludeHidden: opts.IncludeHidden, Exclude: exclude}
}

type mcpArtifactCandidate struct {
	rel  string
	size int64
	full string
}

func mcpWalkArtifacts(root string, opt mcpArtifactWalk) mcpArtifactList {
	var found []mcpArtifactCandidate
	sensitive := mcpSensitiveInfos()
	if opt.Limit <= 0 {
		opt.Limit = mcpArtifactMaxFiles
	}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}
		name := entry.Name()
		if mcpReservedDevice(name) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		slash := filepath.ToSlash(rel)
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if entry.IsDir() {
			if mcpArtifactSkipDir(name) || depth > mcpArtifactMaxDepth || mcpArtifactExcluded(name, slash, opt.Exclude) {
				return filepath.SkipDir
			}
			if !opt.IncludeHidden && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > mcpArtifactMaxDepth || mcpArtifactSecretName(name) || mcpArtifactExcluded(name, slash, opt.Exclude) {
			return nil
		}
		if !opt.IncludeHidden && strings.HasPrefix(name, ".") {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || mcpInfoIsSensitive(info, sensitive) {
			return nil
		}
		found = append(found, mcpArtifactCandidate{rel: slash, size: info.Size(), full: path})
		return nil
	})
	sort.Slice(found, func(i, j int) bool { return found[i].rel < found[j].rel })
	start := 0
	if cursor := filepath.ToSlash(strings.TrimSpace(opt.Cursor)); cursor != "" {
		start = sort.Search(len(found), func(i int) bool { return found[i].rel > cursor })
	}
	end := start + opt.Limit
	truncated := end < len(found)
	if end > len(found) {
		end = len(found)
	}
	var hashed int64
	files := make([]mcpArtifactFile, 0, end-start)
	for _, item := range found[start:end] {
		file := mcpArtifactFile{Path: item.rel, Size: item.size}
		if item.size <= mcpArtifactMaxHash && hashed+item.size <= mcpArtifactHashBudget {
			if sum, hashErr := mcpHashFile(item.full); hashErr == nil {
				file.SHA256 = sum
				hashed += item.size
			} else {
				file.HashSkipped = true
			}
		} else {
			file.HashSkipped = true
		}
		files = append(files, file)
	}
	list := mcpArtifactList{Files: files, Scope: "working_dir", Truncated: truncated}
	if truncated && len(files) > 0 {
		list.NextCursor = files[len(files)-1].Path
		list.TruncatedReason = "page_limit"
	}
	return list
}

func mcpArtifactExcluded(name, rel string, exclude map[string]struct{}) bool {
	if len(exclude) == 0 {
		return false
	}
	if _, ok := exclude[strings.ToLower(name)]; ok {
		return true
	}
	_, ok := exclude[strings.ToLower(rel)]
	return ok
}

func mcpArtifactSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case "node_modules", ".git", "dist", "vendor":
		return true
	default:
		return false
	}
}

func mcpArtifactSecretName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	switch name {
	case "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "credentials.json", "secrets.json", "apikey":
		return true
	}
	return strings.Contains(name, "apikey") || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key")
}

func mcpSensitiveInfos() []os.FileInfo {
	paths := []string{filepath.Join(mcpDiscoveryDir(), "apikey")}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		for _, name := range []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"} {
			paths = append(paths, filepath.Join(home, ".ssh", name))
		}
	}
	out := make([]os.FileInfo, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, info)
	}
	return out
}

func mcpInfoIsSensitive(info os.FileInfo, sensitive []os.FileInfo) bool {
	if info == nil {
		return false
	}
	for _, other := range sensitive {
		if other != nil && os.SameFile(info, other) {
			return true
		}
	}
	return false
}

func mcpResolveInside(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errors.New("要读取的路径不能为空")
	}
	rel = filepath.Clean(rel)
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("只能读取任务工作目录里的文件")
	}
	cur := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Contains(part, ":") || strings.IndexByte(part, 0) >= 0 || mcpReservedDevice(part) {
			return "", errors.New("只能读取任务工作目录里的文件")
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return "", errors.New("工作目录里没有这个文件")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("不能读取链接")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", errors.New("工作目录里没有这个文件")
		}
	}
	back, err := filepath.Rel(root, cur)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", errors.New("只能读取任务工作目录里的文件")
	}
	return cur, nil
}

func mcpRelativeInside(root, full string) string {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return filepath.Base(full)
	}
	return rel
}

func mcpReservedDevice(name string) bool {
	name = strings.TrimSpace(name)
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return false
	}
	// Windows treats the stem as the device: NUL.txt and NUL. are both NUL.
	if cut := strings.IndexByte(name, '.'); cut >= 0 {
		name = name[:cut]
	}
	switch strings.ToUpper(name) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
		"CONIN$", "CONOUT$":
		return true
	default:
		return false
	}
}

func mcpClaimIdentity(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(strconv.Itoa(len(part)))
		b.WriteByte(':')
		b.WriteString(part)
		b.WriteByte('\n')
	}
	return b.String()
}

func mcpHashFile(path string) (string, error) {
	if mcpReservedDevice(filepath.Base(path)) {
		return "", errors.New("reserved device")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, mcpArtifactMaxHash+1))
	if err != nil {
		return "", err
	}
	if n > mcpArtifactMaxHash {
		return "", errors.New("file too large")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) mcpClaimRequest(id, kind, command, prompt string) (*mcpClientRequest, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, true, nil
	}
	if utf8.RuneCountInString(id) > mcpClientRequestIDRunes {
		return nil, false, errors.New("请求编号过长")
	}
	kind = strings.TrimSpace(kind)
	command = strings.TrimSpace(command)
	prompt = strings.TrimSpace(prompt)
	if a == nil {
		return nil, false, errors.New("MCP 任务服务还没有准备好")
	}
	a.mcpLoadDurable()
	hash := mcpRequestHash(kind, command, prompt)
	a.mcpReqMu.Lock()
	evicted := false
	defer func() {
		if evicted {
			a.mcpPersistRequests()
		}
	}()
	defer a.mcpReqMu.Unlock()
	now := time.Now()
	a.mcpPruneRequestTombsLocked(now)
	if a.mcpReqs == nil {
		a.mcpReqs = map[string]*mcpClientRequest{}
	}
	if prev, ok := a.mcpReqs[id]; ok && prev != nil {
		if !mcpRequestStillKept(prev, now) {
			a.mcpForgetRequestLocked(id)
		} else if prev.Hash != "" && prev.Hash != hash || prev.Kind != kind || prev.Command != command || prev.Prompt != prompt {
			return nil, false, mcpErr(mcpCodeRequestConflict, "这个请求编号已经用于另一条调用", "", "")
		} else {
			return prev, false, nil
		}
	}
	if _, tomb := a.mcpReqTombs[id]; tomb {
		return nil, false, mcpErr(mcpCodeRequestConflict, "这个请求编号已经用于另一条调用", "", "")
	}
	req := &mcpClientRequest{Kind: kind, Command: command, Prompt: prompt, Hash: hash, State: mcpRequestStateAccepted, updated: time.Now(), ready: make(chan struct{})}
	a.mcpReqs[id] = req
	a.mcpReqOrder = append(a.mcpReqOrder, id)
	before := len(a.mcpReqTombs)
	if err := a.mcpEvictRequestsLocked(id); err != nil {
		return nil, false, err
	}
	evicted = len(a.mcpReqTombs) != before
	return req, true, nil
}

func mcpRequestHash(kind, command, prompt string) string {
	sum := sha256.Sum256([]byte(kind + "\n" + command + "\n" + prompt))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (a *App) mcpEvictRequestsLocked(keep string) error {
	if len(a.mcpReqs) <= mcpClientRequestLimit {
		return nil
	}
	kept := make([]string, 0, len(a.mcpReqOrder))
	for _, id := range a.mcpReqOrder {
		item := a.mcpReqs[id]
		if item == nil {
			continue
		}
		if id != keep && len(a.mcpReqs) > mcpClientRequestLimit && item.done {
			a.mcpTombRequestLocked(id)
			delete(a.mcpReqs, id)
			continue
		}
		kept = append(kept, id)
	}
	a.mcpReqOrder = kept
	if len(a.mcpReqs) <= mcpClientRequestLimit {
		return nil
	}
	a.mcpForgetRequestLocked(keep)
	return errors.New("请求编号太多，请稍后再试")
}

func (a *App) mcpForgetRequestLocked(id string) {
	delete(a.mcpReqs, id)
	if len(a.mcpReqOrder) == 0 {
		return
	}
	kept := a.mcpReqOrder[:0]
	for _, item := range a.mcpReqOrder {
		if item != id {
			kept = append(kept, item)
		}
	}
	a.mcpReqOrder = kept
}

func (a *App) mcpBindRequest(req *mcpClientRequest, path string) {
	if a == nil || req == nil {
		return
	}
	a.mcpReqMu.Lock()
	req.ProjectPath = normalizeProjectSessionPath(path)
	if req.State == "" || req.State == mcpRequestStateAccepted {
		req.State = mcpRequestStateRunning
	}
	req.updated = time.Now()
	a.mcpReqMu.Unlock()
	a.mcpPersistRequests()
}

// mcpNoteRequestRun binds this command to the round that was just installed.
// A finished request keeps the session it already has, so a later command
// cannot make get_request return that later round.
func (a *App) mcpNoteRequestRun(path, runID string) {
	path = normalizeProjectSessionPath(path)
	runID = strings.TrimSpace(runID)
	if a == nil || path == "" || runID == "" {
		return
	}
	a.mcpReqMu.Lock()
	changed := false
	for _, req := range a.mcpReqs {
		if req == nil || req.done || req.failed {
			continue
		}
		if normalizeProjectSessionPath(req.ProjectPath) != path {
			continue
		}
		if strings.TrimSpace(req.RunID) != "" {
			continue
		}
		req.RunID = runID
		req.updated = time.Now()
		changed = true
	}
	a.mcpReqMu.Unlock()
	if changed {
		a.mcpPersistRequests()
	}
}

func (a *App) mcpCompleteRequest(req *mcpClientRequest, callErr error) {
	if a == nil || req == nil {
		return
	}
	a.mcpReqMu.Lock()
	if callErr != nil && req.errText == "" {
		req.errText = callErr.Error()
	}
	req.done = true
	req.updated = time.Now()
	if strings.TrimSpace(req.errText) != "" {
		req.State = mcpRequestStateFailed
	} else if req.State == "" {
		req.State = mcpRequestStateAccepted
	}
	a.mcpReqMu.Unlock()
	req.finish()
	a.mcpPersistRequests()
}

func (a *App) mcpSucceedRequest(req *mcpClientRequest, path string) {
	a.mcpBindRequest(req, path)
	a.mcpCompleteRequest(req, nil)
}

func (a *App) mcpFailRequest(req *mcpClientRequest) {
	a.mcpAbandonRequest(req, nil)
}

func (a *App) mcpAbandonRequest(req *mcpClientRequest, reason error) {
	if a == nil || req == nil {
		return
	}
	a.mcpReqMu.Lock()
	if reason != nil && req.errText == "" {
		req.errText = reason.Error()
	}
	if req.errText == "" {
		req.errText = "这次调用没有完成，可以用同一编号再试一次"
	}
	req.failed = true
	req.done = true
	for id, item := range a.mcpReqs {
		if item == req {
			a.mcpForgetRequestLocked(id)
			break
		}
	}
	a.mcpReqMu.Unlock()
	req.finish()
	a.mcpPersistRequests()
}

func (req *mcpClientRequest) finish() {
	if req == nil || req.ready == nil {
		return
	}
	req.closeOnce.Do(func() { close(req.ready) })
}

func (a *App) mcpReplayRequest(ctx context.Context, req *mcpClientRequest) (mcpTaskView, error) {
	if req == nil || req.ready == nil {
		return mcpTaskView{}, errors.New("请求已受理，用 get_request 查询")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-req.ready:
	default:
		select {
		case <-req.ready:
		case <-ctx.Done():
			return mcpTaskView{}, errors.New("请求还在处理，用 get_request 查询")
		}
	}
	a.mcpReqMu.Lock()
	path := req.ProjectPath
	errText := req.errText
	failed := req.failed
	done := req.done
	runID := req.RunID
	a.mcpReqMu.Unlock()
	if failed || errText != "" {
		if errText == "" {
			errText = "这次调用没有完成，可以用同一编号再试一次"
		}
		return mcpTaskView{}, errors.New(errText)
	}
	if path == "" {
		if done {
			return mcpTaskView{}, errors.New("这次调用没有完成，可以用同一编号再试一次")
		}
		return mcpTaskView{}, errors.New("请求还在处理，用 get_request 查询")
	}
	if view, ok := a.mcpRequestSessionView(path, runID); ok {
		return a.mcpReturnTask(view), nil
	}
	if strings.TrimSpace(runID) != "" {
		return mcpTaskView{}, mcpErr(mcpCodeRunNotFound, "没有这一轮执行", path, runID)
	}
	return mcpTaskView{ProjectPath: path, Status: mcpStatusRunning, Text: "请求已受理"}, nil
}

// mcpRequestSessionView is the round that belongs to one accepted command.
// A stored run id stays on that command after a later round becomes current.
func (a *App) mcpRequestSessionView(path, runID string) (mcpTaskView, bool) {
	path = normalizeProjectSessionPath(path)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return a.mcpViewForRun(path, runID)
	}
	return a.mcpViewFromRegistry(path)
}

func (a *App) mcpGetRequest(ctx context.Context, clientRequestID string) (mcpRequestView, error) {
	id := strings.TrimSpace(clientRequestID)
	if id == "" {
		return mcpRequestView{}, errors.New("请求编号不能为空")
	}
	out := mcpRequestView{ClientRequestID: id, State: mcpRequestStateNotFound}
	if a == nil {
		return out, nil
	}
	a.mcpLoadDurable()
	a.mcpReqMu.Lock()
	now := time.Now()
	a.mcpPruneRequestTombsLocked(now)
	req := a.mcpReqs[id]
	if req != nil && !mcpRequestStillKept(req, now) {
		a.mcpForgetRequestLocked(id)
		req = nil
	}
	_, tomb := a.mcpReqTombs[id]
	var path, kind, errText, hash, runID string
	done := false
	failed := false
	if req != nil {
		path = req.ProjectPath
		kind = req.Kind
		errText = req.errText
		done = req.done
		failed = req.failed
		hash = req.Hash
		runID = req.RunID
	}
	a.mcpReqMu.Unlock()
	if req == nil {
		if tomb {
			out.State = mcpRequestStateExpired
		}
		return out, nil
	}
	out.Accepted = true
	out.Kind = kind
	out.ProjectPath = path
	out.Error = errText
	out.Pending = !done
	out.PayloadHash = hash
	out.RunID = runID
	if path != "" || kind != "" {
		out.Scope = &mcpRequestScope{TaskID: path, Action: kind}
	}
	var published *mcpTaskView
	if path != "" {
		if view, ok := a.mcpRequestSessionView(path, runID); ok {
			next := a.mcpReturnTask(view)
			published = &next
			out.Task = published
			if out.RunID == "" {
				out.RunID = firstNonEmpty(next.RunID, next.CurrentRunID)
			}
		} else if strings.TrimSpace(runID) != "" && !failed && strings.TrimSpace(errText) == "" {
			// The run id is written only after that round is stored. A miss
			// means the session left the archive. The empty-id path below is
			// the short window before a row exists; reporting running here
			// makes a caller wait on a session that will not come back.
			// A request that already failed keeps that failure.
			return mcpRequestView{}, mcpErr(mcpCodeRunNotFound, "没有这一轮执行", path, runID)
		}
	}
	out.State = mcpDeriveRequestState(&mcpClientRequest{ProjectPath: path, errText: errText, failed: failed, done: done}, published)
	return out, nil
}

func (a *App) mcpCapabilities(ctx context.Context) (mcpCapabilityView, error) {
	cfg := corelib.AppConfig{}
	if a != nil {
		cfg = a.peekConfigOrEmpty()
	}
	provider := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	model := strings.TrimSpace(cfg.MaclawLLMModel)
	connection := "unconfigured"
	if provider != "" && model != "" {
		connection = "configured"
	}
	enabled := computerUseEnabledFromConfig(&cfg)
	build := strings.TrimSpace(versionVar())
	if build == "" {
		build = "dev"
	}
	return mcpCapabilityView{
		APIVersion:      mcpHostAPIVersion,
		ProtocolVersion: mcpProtocolVersionLatest,
		Features:        mcpFeatureNames(),
		ModelConnection: connection,
		Provider:        provider,
		Model:           model,
		BrowserTest:     enabled,
		Screenshot:      enabled,
		Shell:           false,
		BrowserMethods:  mcpBrowserMethods(enabled),
		SchemaVersion:   mcpSchemaVersion,
		ServerBuild:     build,
		Commit:          "",
		Persistence:     &mcpPersistenceView{Durable: true, Retention: "7d"},
		CurrentModel: &mcpCurrentModelView{
			Provider:   provider,
			Model:      model,
			Configured: connection == "configured",
			ProbeOK:    nil,
		},
		FeatureFlags: &mcpFeatureFlagView{
			DurableRuns:           true,
			ExpectedStatusVersion: true,
			DryRun:                true,
			ArtifactPaging:        true,
			ExpectedSHA256:        true,
		},
	}, nil
}

func mcpFeatureNames() []string {
	defs := mcpToolDefinitions()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		name, _ := def["name"].(string)
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
