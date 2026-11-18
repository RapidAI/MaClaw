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
	StatusVersion int           `json:"status_version,omitempty"`
	Events        []mcpLogEvent `json:"events"`
	NextCursor    int           `json:"next_cursor"`
	Gap           bool          `json:"gap,omitempty"`
}

type mcpArtifactFile struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256,omitempty"`
	HashSkipped bool   `json:"hash_skipped,omitempty"`
}

type mcpArtifactList struct {
	ProjectPath string            `json:"project_path"`
	Files       []mcpArtifactFile `json:"files"`
	Truncated   bool              `json:"truncated,omitempty"`
}

type mcpArtifactBody struct {
	ProjectPath string `json:"project_path"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	Text        string `json:"text,omitempty"`
	Content     string `json:"content,omitempty"`
}

type mcpRequestView struct {
	Accepted        bool         `json:"accepted"`
	Pending         bool         `json:"pending,omitempty"`
	ClientRequestID string       `json:"client_request_id"`
	Kind            string       `json:"kind,omitempty"`
	ProjectPath     string       `json:"project_path,omitempty"`
	Error           string       `json:"error,omitempty"`
	Task            *mcpTaskView `json:"task,omitempty"`
}

type mcpBrowserMethod struct {
	Name      string   `json:"name"`
	Tool      string   `json:"tool"`
	Action    string   `json:"action"`
	Available bool     `json:"available"`
	Arguments []string `json:"arguments,omitempty"`
}

type mcpCapabilityView struct {
	APIVersion      int                `json:"api_version"`
	ProtocolVersion string             `json:"protocol_version"`
	Features        []string           `json:"features"`
	ModelConnection string             `json:"model_connection"`
	Provider        string             `json:"provider,omitempty"`
	Model           string             `json:"model,omitempty"`
	BrowserTest     bool               `json:"browser_test"`
	Screenshot      bool               `json:"screenshot"`
	Shell           bool               `json:"shell"`
	BrowserMethods  []mcpBrowserMethod `json:"browser_methods"`
}

// mcpClientRequest remembers a caller-supplied id so a timed-out retry does
// not start a second turn. The record is in memory only.
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
}

func (a *App) mcpPublishTaskView(view mcpTaskView) mcpTaskView {
	if a == nil {
		return view
	}
	if strings.TrimSpace(view.WorkingDir) == "" {
		view.WorkingDir = strings.TrimSpace(view.workspace)
	}
	view = a.mcpFillProviderModel(view)
	view = a.mcpEnsureConfirmationID(view)
	view = mcpPublishChildren(view)
	return a.mcpAnnotateStepFreshness(view)
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
		view = a.mcpApplyLiveConfirmation(view)
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

func (a *App) mcpFillProviderModel(view mcpTaskView) mcpTaskView {
	provider := strings.TrimSpace(view.Provider)
	model := strings.TrimSpace(view.Model)
	if provider != "" && model != "" {
		view.Provider = provider
		view.Model = model
		return view
	}
	// A half-filled pair is the record's own value. Filling the blank from
	// another source would pair one provider with a different model's name.
	if provider != "" || model != "" {
		view.Provider = provider
		view.Model = model
		return view
	}
	if pin, ok := a.mcpPinNames(view.ProjectPath); ok {
		view.Provider = pin.ProviderName
		view.Model = pin.Model
		return view
	}
	cfg := a.peekConfigOrEmpty()
	view.Provider = strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	view.Model = strings.TrimSpace(cfg.MaclawLLMModel)
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
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	if rec := a.mcpRuns[path]; rec != nil {
		// The run record is the only live id. Drop ids minted before it existed.
		a.mcpDropConfirmIDsLocked(path)
		if rec.confirmFingerprint == fp && strings.HasPrefix(rec.ConfirmationID, mcpConfirmIDPrefix) {
			view.ConfirmationID = rec.ConfirmationID
			return view
		}
		rec.ConfirmationID = mcpConfirmIDPrefix + randomHexID(12)
		rec.confirmFingerprint = fp
		view.ConfirmationID = rec.ConfirmationID
		return view
	}
	key := path + "\x00" + fp
	if a.mcpConfirmIDs == nil {
		a.mcpConfirmIDs = map[string]string{}
	}
	if id := a.mcpConfirmIDs[key]; strings.HasPrefix(id, mcpConfirmIDPrefix) {
		a.mcpDropConfirmIDsLocked(path)
		a.mcpConfirmIDs[key] = id
		view.ConfirmationID = id
		return view
	}
	a.mcpDropConfirmIDsLocked(path)
	id := mcpConfirmIDPrefix + randomHexID(12)
	a.mcpConfirmIDs[key] = id
	view.ConfirmationID = id
	return view
}

func (a *App) mcpDropConfirmIDsLocked(path string) {
	if a == nil || len(a.mcpConfirmIDs) == 0 {
		return
	}
	prefix := path + "\x00"
	for key := range a.mcpConfirmIDs {
		if strings.HasPrefix(key, prefix) {
			delete(a.mcpConfirmIDs, key)
		}
	}
}

func (a *App) mcpConsumeConfirmation(path, id string) error {
	id = strings.TrimSpace(id)
	path = normalizeProjectSessionPath(path)
	if a == nil || path == "" || !strings.HasPrefix(id, mcpConfirmIDPrefix) {
		return errors.New("确认编号不匹配或已失效")
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	if rec := a.mcpRuns[path]; rec != nil && rec.ConfirmationID == id {
		rec.ConfirmationID = ""
		rec.confirmFingerprint = ""
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
	mcpTouchLocked(rec)
}

func (a *App) mcpAppendLog(path, requestID string, ev mcpLogEvent) {
	if a == nil {
		return
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	mcpPushLogLocked(rec, ev)
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

func (a *App) mcpReadLog(ctx context.Context, projectPath, runID string, cursor int) (mcpLogPage, error) {
	if cursor < 0 {
		cursor = 0
	}
	path := normalizeProjectSessionPath(projectPath)
	runID = strings.TrimSpace(runID)
	if a == nil || path == "" {
		return mcpLogPage{}, errors.New("没有找到这个任务")
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	rec, err := a.mcpRunForReadLocked(path, runID)
	if err != nil {
		return mcpLogPage{}, err
	}
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
		gap = events[0].Seq > cursor+1
	}
	pageRun := runID
	if pageRun == "" {
		pageRun = rec.RunID
	}
	return mcpLogPage{ProjectPath: path, RunID: pageRun, StatusVersion: rec.StatusVersion, Events: events, NextCursor: next, Gap: gap}, nil
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
	files, truncated := mcpWalkArtifacts(root)
	if files == nil {
		files = []mcpArtifactFile{}
	}
	return mcpArtifactList{ProjectPath: view.ProjectPath, Files: files, Truncated: truncated}, nil
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
	body := mcpArtifactBody{
		ProjectPath: view.ProjectPath,
		Path:        filepath.ToSlash(mcpRelativeInside(root, full)),
		Size:        info.Size(),
		SHA256:      hex.EncodeToString(sum[:]),
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

func mcpWalkArtifacts(root string) ([]mcpArtifactFile, bool) {
	var files []mcpArtifactFile
	truncated := false
	var hashed int64
	sensitive := mcpSensitiveInfos()
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		// Reserved names such as NUL and COM1 are devices. Opening them can
		// block the GUI, so they are skipped before Stat or Read.
		if mcpReservedDevice(name) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip the link itself. WalkDir lists a directory only after the
		// callback returns, so SkipDir stops a junction or symlink from
		// being followed outside the work directory.
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
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if entry.IsDir() {
			if mcpArtifactSkipDir(name) || depth > mcpArtifactMaxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > mcpArtifactMaxDepth || mcpArtifactSecretName(name) {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || mcpInfoIsSensitive(info, sensitive) {
			return nil
		}
		if len(files) >= mcpArtifactMaxFiles {
			truncated = true
			return filepath.SkipAll
		}
		file := mcpArtifactFile{Path: filepath.ToSlash(rel), Size: info.Size()}
		if info.Size() <= mcpArtifactMaxHash && hashed+info.Size() <= mcpArtifactHashBudget {
			if sum, hashErr := mcpHashFile(path); hashErr == nil {
				file.SHA256 = sum
				hashed += info.Size()
			} else {
				file.HashSkipped = true
			}
		} else {
			file.HashSkipped = true
		}
		files = append(files, file)
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, truncated
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
	a.mcpReqMu.Lock()
	defer a.mcpReqMu.Unlock()
	if a.mcpReqs == nil {
		a.mcpReqs = map[string]*mcpClientRequest{}
	}
	if prev, ok := a.mcpReqs[id]; ok && prev != nil {
		if prev.Kind != kind || prev.Command != command || prev.Prompt != prompt {
			return nil, false, errors.New("这个请求编号已经用于另一条调用")
		}
		return prev, false, nil
	}
	req := &mcpClientRequest{Kind: kind, Command: command, Prompt: prompt, ready: make(chan struct{})}
	a.mcpReqs[id] = req
	a.mcpReqOrder = append(a.mcpReqOrder, id)
	if err := a.mcpEvictRequestsLocked(id); err != nil {
		return nil, false, err
	}
	return req, true, nil
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
	a.mcpReqMu.Unlock()
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
	a.mcpReqMu.Unlock()
	req.finish()
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
	if view, ok := a.mcpViewFromRegistry(path); ok {
		return a.mcpReturnTask(view), nil
	}
	return mcpTaskView{ProjectPath: path, Status: mcpStatusRunning, Text: "请求已受理"}, nil
}

func (a *App) mcpGetRequest(ctx context.Context, clientRequestID string) (mcpRequestView, error) {
	id := strings.TrimSpace(clientRequestID)
	if id == "" {
		return mcpRequestView{}, errors.New("请求编号不能为空")
	}
	out := mcpRequestView{ClientRequestID: id}
	if a == nil {
		return out, nil
	}
	a.mcpReqMu.Lock()
	req := a.mcpReqs[id]
	var path, kind, errText string
	done := false
	if req != nil {
		path = req.ProjectPath
		kind = req.Kind
		errText = req.errText
		done = req.done
	}
	a.mcpReqMu.Unlock()
	if req == nil {
		return out, nil
	}
	out.Accepted = true
	out.Kind = kind
	out.ProjectPath = path
	out.Error = errText
	out.Pending = !done
	if path != "" {
		if view, ok := a.mcpViewFromRegistry(path); ok {
			published := a.mcpReturnTask(view)
			out.Task = &published
		}
	}
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
