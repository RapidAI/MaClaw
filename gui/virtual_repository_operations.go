package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

type VirtualRepositoryOperationRequest struct {
	RootPath             string    `json:"root_path"`
	RepositoryID         string    `json:"repository_id,omitempty"`
	NodeID               string    `json:"node_id,omitempty"`
	Action               string    `json:"action"`
	Message              string    `json:"message,omitempty"`
	NodeIDs              []string  `json:"node_ids,omitempty"`
	ExpectedRepositoryID string    `json:"expected_repository_id,omitempty"`
	ExpectedUpdatedAt    time.Time `json:"expected_updated_at,omitempty"`
}

func (a *App) virtualRepositoryForOperation(req VirtualRepositoryOperationRequest) (*VirtualRepository, error) {
	if strings.TrimSpace(req.RepositoryID) != "" {
		indexItems, err := a.loadVirtualRepositoryIndexItems()
		if err != nil {
			return nil, err
		}
		for _, item := range indexItems {
			if item.ID == strings.TrimSpace(req.RepositoryID) && item.Remote != nil {
				return a.readRemoteVirtualRepository(item, "", false)
			}
		}
	}
	return readVirtualRepository(req.RootPath)
}

type VirtualRepositoryOperationPreview struct {
	Action       string                             `json:"action"`
	RepositoryID string                             `json:"repository_id"`
	UpdatedAt    time.Time                          `json:"updated_at"`
	Targets      []VirtualRepositoryOperationTarget `json:"targets"`
	SkippedLocal int                                `json:"skipped_local"`
	Blocked      bool                               `json:"blocked"`
	Warnings     []string                           `json:"warnings"`
}

type VirtualRepositoryOperationTarget struct {
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Status    string `json:"status,omitempty"`
	Changed   bool   `json:"changed"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

type VirtualRepositoryOperationResult struct {
	JobID        string                                 `json:"job_id"`
	Action       string                                 `json:"action"`
	Message      string                                 `json:"message,omitempty"`
	Status       string                                 `json:"status"`
	StartedAt    time.Time                              `json:"started_at"`
	FinishedAt   time.Time                              `json:"finished_at"`
	Items        []VirtualRepositoryOperationItemResult `json:"items"`
	SkippedLocal int                                    `json:"skipped_local"`
}

type virtualRepositoryOperationJob struct {
	mu                sync.RWMutex
	result            VirtualRepositoryOperationResult
	cancel            context.CancelFunc
	cancelRequested   bool
	targetCount       int
	retainedTextBytes int
}

var virtualRepositoryOperationJobs = struct {
	sync.Mutex
	items              map[string]*virtualRepositoryOperationJob
	order              []string
	activeRepositories map[string]string
}{items: make(map[string]*virtualRepositoryOperationJob), activeRepositories: make(map[string]string)}

const (
	virtualRepositoryOperationJobLimit              = 50
	virtualRepositoryOperationResultTextMaxBytes    = 64 * 1024
	virtualRepositoryOperationJobTextMaxBytes       = 4 * 1024 * 1024
	virtualRepositoryOperationResultTruncatedMarker = "\n[output truncated]"
)

var svnPasswordStdinCapability = struct {
	sync.Mutex
	executable string
	modTime    time.Time
	size       int64
	supported  bool
	known      bool
}{}

type VirtualRepositoryOperationItemResult struct {
	NodeID     string `json:"node_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type virtualRepositoryOperationError struct {
	Code string
	Err  error
}

func (e *virtualRepositoryOperationError) Error() string { return e.Err.Error() }
func (e *virtualRepositoryOperationError) Unwrap() error { return e.Err }

func classifyVirtualRepositoryOperationError(err error) string {
	if err == nil {
		return ""
	}
	var typed *virtualRepositoryOperationError
	if errors.As(err, &typed) {
		return typed.Code
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "nothing to commit"):
		return "nothing_to_commit"
	case strings.Contains(lower, "authentication failed") || strings.Contains(lower, "authorization failed") || strings.Contains(lower, "could not read username"):
		return "authentication_failed"
	case strings.Contains(lower, "non-fast-forward") || strings.Contains(lower, "rejected"):
		return "push_rejected"
	case strings.Contains(lower, "locked"):
		return "working_copy_locked"
	default:
		return "command_failed"
	}
}

// sanitizeVirtualRepositoryOperationResultText is the final boundary before
// command output or errors are retained in job memory and returned to the UI.
// Command runners also redact and limit their output, but not every error is
// produced by a command runner (for example SSH connection errors).
func sanitizeVirtualRepositoryOperationResultText(value string) string {
	return sanitizeVirtualRepositoryOperationResultTextLimit(value, virtualRepositoryOperationResultTextMaxBytes)
}

func sanitizeVirtualRepositoryOperationResultTextLimit(value string, limit int) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	value = redactVCSOutput(value)
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	if limit <= len(virtualRepositoryOperationResultTruncatedMarker) {
		return virtualRepositoryOperationResultTruncatedMarker[:limit]
	}
	prefixBytes := limit - len(virtualRepositoryOperationResultTruncatedMarker)
	value = value[:prefixBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + virtualRepositoryOperationResultTruncatedMarker
}

func parseVirtualRepositoryOperationRequest(inputJSON string) (VirtualRepositoryOperationRequest, error) {
	var req VirtualRepositoryOperationRequest
	if err := unmarshalVirtualRepositoryInput(inputJSON, "virtual repository operation", &req); err != nil {
		return req, err
	}
	req.RootPath = strings.TrimSpace(req.RootPath)
	req.RepositoryID = strings.TrimSpace(req.RepositoryID)
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.ExpectedRepositoryID = strings.TrimSpace(req.ExpectedRepositoryID)
	for _, value := range []string{req.RepositoryID, req.NodeID, req.ExpectedRepositoryID} {
		if len(value) > virtualRepositoryNameMaxLength || containsControlCharacter(value) {
			return req, errors.New("virtual repository operation contains an invalid id")
		}
	}
	if len(req.RootPath) > virtualRepositoryFieldMaxLength || containsControlCharacter(req.RootPath) {
		return req, errors.New("virtual repository operation contains an invalid root path")
	}
	if len(req.NodeIDs) > virtualRepositoryNodeMaxCount {
		return req, errors.New("virtual repository operation contains too many node ids")
	}
	for i := range req.NodeIDs {
		req.NodeIDs[i] = strings.TrimSpace(req.NodeIDs[i])
		if req.NodeIDs[i] == "" || len(req.NodeIDs[i]) > virtualRepositoryNameMaxLength || containsControlCharacter(req.NodeIDs[i]) {
			return req, errors.New("virtual repository operation contains an invalid node id")
		}
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.Message = strings.TrimSpace(req.Message)
	if len(req.Message) > virtualRepositoryFieldMaxLength {
		return req, errors.New("commit message is too long")
	}
	if strings.ContainsRune(req.Message, '\x00') {
		return req, errors.New("commit message must not contain NUL characters")
	}
	switch req.Action {
	case "sync", "commit", "push", "commit_push", "revert":
	default:
		return req, errors.New("unsupported virtual repository operation")
	}
	if (req.Action == "commit" || req.Action == "commit_push") && req.Message == "" {
		return req, errors.New("commit message is required")
	}
	return req, nil
}

func collectVirtualRepositoryTargets(repo *VirtualRepository, req VirtualRepositoryOperationRequest) ([]VirtualRepositoryNode, int, error) {
	allowed := map[string]bool{}
	existing := make(map[string]bool, len(repo.Nodes))
	for _, node := range repo.Nodes {
		existing[node.ID] = true
	}
	if len(req.NodeIDs) > 0 {
		for _, id := range req.NodeIDs {
			id = strings.TrimSpace(id)
			if id == "" || !existing[id] {
				return nil, 0, fmt.Errorf("virtual repository node %q was not found", id)
			}
			allowed[id] = true
		}
	} else if strings.TrimSpace(req.NodeID) != "" {
		req.NodeID = strings.TrimSpace(req.NodeID)
		if !existing[req.NodeID] {
			return nil, 0, fmt.Errorf("virtual repository node %q was not found", req.NodeID)
		}
		children := make(map[string][]string, len(repo.Nodes))
		for _, node := range repo.Nodes {
			if node.ParentID != "" {
				children[node.ParentID] = append(children[node.ParentID], node.ID)
			}
		}
		allowed[req.NodeID] = true
		queue := []string{req.NodeID}
		for len(queue) > 0 {
			parent := queue[0]
			queue = queue[1:]
			for _, child := range children[parent] {
				if !allowed[child] {
					allowed[child] = true
					queue = append(queue, child)
				}
			}
		}
	}
	all := len(allowed) == 0
	var targets []VirtualRepositoryNode
	skippedLocal := 0
	for _, node := range repo.Nodes {
		if !all && !allowed[node.ID] {
			continue
		}
		if node.Repository == nil || !node.Repository.Enabled {
			continue
		}
		if node.Repository.Kind == "local" {
			skippedLocal++
			continue
		}
		targets = append(targets, node)
	}
	sortVirtualRepositoryNodes(targets)
	return targets, skippedLocal, nil
}

func sortVirtualRepositoryNodes(nodes []VirtualRepositoryNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Order != nodes[j].Order {
			return nodes[i].Order < nodes[j].Order
		}
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].ID < nodes[j].ID
	})
}

func (a *App) PreviewVirtualRepositoryOperation(inputJSON string) (string, error) {
	req, err := parseVirtualRepositoryOperationRequest(inputJSON)
	if err != nil {
		return "", err
	}
	repo, err := a.virtualRepositoryForOperation(req)
	if err != nil {
		return "", err
	}
	targets, skipped, err := collectVirtualRepositoryTargets(repo, req)
	if err != nil {
		return "", err
	}
	preview := VirtualRepositoryOperationPreview{Action: req.Action, RepositoryID: repo.ID, UpdatedAt: repo.UpdatedAt, Targets: []VirtualRepositoryOperationTarget{}, SkippedLocal: skipped, Warnings: []string{}}
	if skipped > 0 {
		preview.Warnings = append(preview.Warnings, fmt.Sprintf("%d local directories will be skipped", skipped))
	}
	var remoteClient *ssh.Client
	if repo.Remote != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		remoteRepo, client, remoteErr := a.remoteVirtualRepositoryByIDContext(ctx, repo.ID)
		cancel()
		if remoteErr != nil {
			return "", remoteErr
		}
		if !remoteRepo.UpdatedAt.Equal(repo.UpdatedAt) {
			_ = client.Close()
			return "", errors.New("remote virtual repository changed during operation preview; preview again")
		}
		repo, remoteClient = remoteRepo, client
		defer remoteClient.Close()
	}
	previewTimeout := 2 * time.Minute
	if remoteClient != nil {
		previewTimeout = 5 * time.Minute
	}
	previewContext, previewCancel := context.WithTimeout(context.Background(), previewTimeout)
	defer previewCancel()
	clients := make(virtualRepositoryVCSClients, 2)
	for _, node := range targets {
		if previewContext.Err() != nil {
			return "", fmt.Errorf("virtual repository operation preview timed out: %w", previewContext.Err())
		}
		var status VirtualRepositoryNodeStatus
		if remoteClient != nil {
			// Remote inspection already executes its VCS commands over SSH. Avoid a
			// redundant local path/client probe whose result is immediately replaced.
			status = inspectRemoteVirtualRepositoryNode(previewContext, remoteClient, repo, node)
		} else {
			status = a.inspectVirtualRepositoryNodeContextWithClients(previewContext, repo.RootPath, node, clients)
		}
		item := VirtualRepositoryOperationTarget{NodeID: node.ID, Name: node.Name, Kind: node.Repository.Kind, Path: status.Path, Status: sanitizeVirtualRepositoryOperationResultText(status.Status), Changed: !status.Clean, ErrorCode: status.ErrorCode, Error: sanitizeVirtualRepositoryOperationResultText(status.Error)}
		if status.Error != "" {
			preview.Blocked = true
		}
		if refErr := validateVirtualRepositoryOperationForBinding(node.Repository, req.Action); refErr != nil {
			item.ErrorCode = classifyVirtualRepositoryOperationError(refErr)
			item.Error = refErr.Error()
			preview.Blocked = true
		}
		if req.Action == "revert" && hasUnsafeVCSState(status.Status, node.Repository.Kind) {
			item.ErrorCode = "conflict_detected"
			item.Error = "working copy has conflicts or an in-progress merge/rebase"
			preview.Blocked = true
		}
		preview.Targets = append(preview.Targets, item)
	}
	if len(preview.Targets) == 0 {
		preview.Blocked = true
		preview.Warnings = append(preview.Warnings, "no version-controlled repositories selected")
	}
	data, err := json.Marshal(preview)
	return string(data), err
}

func hasUnsafeVCSState(status, kind string) bool {
	lower := strings.ToLower(status)
	if kind == "svn" {
		return strings.Contains(lower, "conflict") || strings.Contains(status, "C ") || strings.HasPrefix(status, "C")
	}
	return strings.Contains(lower, "unmerged") || strings.Contains(status, "UU ") || strings.Contains(status, "AA ") || strings.Contains(status, "DD ")
}

func (a *App) StartVirtualRepositoryOperation(inputJSON string) (string, error) {
	req, err := parseVirtualRepositoryOperationRequest(inputJSON)
	if err != nil {
		return "", err
	}
	previewRaw, err := a.PreviewVirtualRepositoryOperation(inputJSON)
	if err != nil {
		return "", err
	}
	var preview VirtualRepositoryOperationPreview
	if err := json.Unmarshal([]byte(previewRaw), &preview); err != nil {
		return "", err
	}
	if preview.Blocked {
		return "", errors.New("operation preview is blocked; resolve repository errors first")
	}
	repo, err := a.virtualRepositoryForOperation(req)
	if err != nil {
		return "", err
	}
	if req.ExpectedRepositoryID != "" && (repo.ID != req.ExpectedRepositoryID || req.ExpectedUpdatedAt.IsZero() || !repo.UpdatedAt.Equal(req.ExpectedUpdatedAt)) {
		return "", errors.New("virtual repository changed after the displayed preview; preview the operation again")
	}
	if repo.ID != preview.RepositoryID || !repo.UpdatedAt.Equal(preview.UpdatedAt) {
		return "", errors.New("virtual repository changed during operation validation; preview the operation again")
	}
	targetNodes, skipped, err := collectVirtualRepositoryTargets(repo, req)
	if err != nil {
		return "", err
	}
	// Re-check the exact target snapshot immediately before enqueueing. The
	// manifest may have changed between preview and start.
	if len(targetNodes) != len(preview.Targets) {
		return "", errors.New("virtual repository changed after preview; preview the operation again")
	}
	for i, node := range targetNodes {
		target := preview.Targets[i]
		if target.NodeID != node.ID || target.Name != node.Name || target.Kind != node.Repository.Kind {
			return "", errors.New("virtual repository targets changed after preview; preview the operation again")
		}
	}
	jobID := "vrepo-" + uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	job := &virtualRepositoryOperationJob{cancel: cancel, targetCount: len(targetNodes), result: VirtualRepositoryOperationResult{JobID: jobID, Action: req.Action, Message: req.Message, Status: "running", StartedAt: time.Now().UTC(), Items: []VirtualRepositoryOperationItemResult{}, SkippedLocal: skipped}}
	if err := registerVirtualRepositoryOperationJob(repo.ID, jobID, job); err != nil {
		cancel()
		return "", err
	}
	log.Printf("[vrepo] operation_queued repo=%q job=%q action=%q targets=%d skipped_local=%d remote=%t", repo.ID, jobID, req.Action, len(targetNodes), skipped, repo.Remote != nil)
	go a.runVirtualRepositoryOperationJob(ctx, job, repo, targetNodes, req)
	return marshalVirtualRepositoryOperationJob(job)
}

func registerVirtualRepositoryOperationJob(repositoryID, jobID string, job *virtualRepositoryOperationJob) error {
	virtualRepositoryOperationJobs.Lock()
	defer virtualRepositoryOperationJobs.Unlock()
	if activeJobID := virtualRepositoryOperationJobs.activeRepositories[repositoryID]; activeJobID != "" {
		return fmt.Errorf("virtual repository already has a running operation (%s)", activeJobID)
	}
	for len(virtualRepositoryOperationJobs.order) >= virtualRepositoryOperationJobLimit {
		removeAt := -1
		for i, candidateID := range virtualRepositoryOperationJobs.order {
			candidate := virtualRepositoryOperationJobs.items[candidateID]
			candidate.mu.RLock()
			isRunning := candidate.result.Status == "running"
			candidate.mu.RUnlock()
			if !isRunning {
				removeAt = i
				break
			}
		}
		if removeAt < 0 {
			break
		}
		removeID := virtualRepositoryOperationJobs.order[removeAt]
		virtualRepositoryOperationJobs.order = append(virtualRepositoryOperationJobs.order[:removeAt], virtualRepositoryOperationJobs.order[removeAt+1:]...)
		delete(virtualRepositoryOperationJobs.items, removeID)
	}
	if len(virtualRepositoryOperationJobs.order) >= virtualRepositoryOperationJobLimit {
		return errors.New("too many virtual repository operations are still running")
	}
	virtualRepositoryOperationJobs.items[jobID] = job
	virtualRepositoryOperationJobs.order = append(virtualRepositoryOperationJobs.order, jobID)
	virtualRepositoryOperationJobs.activeRepositories[repositoryID] = jobID
	return nil
}

func (a *App) runVirtualRepositoryOperationJob(ctx context.Context, job *virtualRepositoryOperationJob, repo *VirtualRepository, targetNodes []VirtualRepositoryNode, req VirtualRepositoryOperationRequest) {
	jobStarted := time.Now()
	job.mu.RLock()
	jobID := job.result.JobID
	job.mu.RUnlock()
	log.Printf("[vrepo] operation_start repo=%q job=%q action=%q targets=%d remote=%t", repo.ID, jobID, req.Action, len(targetNodes), repo.Remote != nil)
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[vrepo] operation_panic repo=%q job=%q action=%q panic_type=%q duration_ms=%d", repo.ID, jobID, req.Action, fmt.Sprintf("%T", recovered), time.Since(jobStarted).Milliseconds())
			job.mu.Lock()
			// Preserve cancellation if it won the race with a panic; otherwise the
			// job would appear failed after the user explicitly cancelled it.
			if job.cancelRequested || ctx.Err() != nil {
				job.result.Status = "cancelled"
			} else {
				job.result.Status = "failed"
				job.result.Items = append(job.result.Items, VirtualRepositoryOperationItemResult{Status: "failed", ErrorCode: "internal_error", Error: "operation runner stopped unexpectedly"})
			}
			job.result.FinishedAt = time.Now().UTC()
			job.mu.Unlock()
			a.emitVirtualRepositoryJob(job)
		}
		virtualRepositoryOperationJobs.Lock()
		if virtualRepositoryOperationJobs.activeRepositories[repo.ID] == jobID {
			delete(virtualRepositoryOperationJobs.activeRepositories, repo.ID)
		}
		virtualRepositoryOperationJobs.Unlock()
	}()
	var remoteClient *ssh.Client
	var remoteConnectErr error
	if repo.Remote != nil {
		var current *VirtualRepository
		current, remoteClient, remoteConnectErr = a.remoteVirtualRepositoryByIDContext(ctx, repo.ID)
		if remoteConnectErr == nil {
			if !current.UpdatedAt.Equal(repo.UpdatedAt) {
				remoteConnectErr = errors.New("remote virtual repository changed after the operation was queued; preview and start it again")
				_ = remoteClient.Close()
				remoteClient = nil
			} else {
				repo = current
				defer remoteClient.Close()
			}
		}
	}
	if remoteConnectErr == nil && repo.Remote == nil {
		current, err := readVirtualRepository(repo.RootPath)
		if err != nil {
			remoteConnectErr = fmt.Errorf("reopen virtual repository before executing operation: %w", err)
		} else if current.ID != repo.ID || !current.UpdatedAt.Equal(repo.UpdatedAt) {
			remoteConnectErr = errors.New("virtual repository changed after the operation was queued; preview and start it again")
		} else {
			repo = current
		}
	}
	clients := make(virtualRepositoryVCSClients, 2)
	succeeded, failed := 0, 0
	for _, node := range targetNodes {
		if ctx.Err() != nil {
			break
		}
		started := time.Now()
		item := VirtualRepositoryOperationItemResult{NodeID: node.ID, Name: node.Name, Kind: node.Repository.Kind}
		var output string
		var execErr error
		if remoteConnectErr != nil {
			execErr = remoteConnectErr
		} else if remoteClient != nil {
			output, execErr = a.executeRemoteVirtualRepositoryOperationWithClient(ctx, remoteClient, repo, node, req)
		} else {
			output, execErr = a.executeVirtualRepositoryOperationWithClients(ctx, repo, node, req, clients)
		}
		item.DurationMS = time.Since(started).Milliseconds()
		if execErr != nil {
			item.Error = sanitizeVirtualRepositoryOperationResultText(execErr.Error())
			item.ErrorCode = classifyVirtualRepositoryOperationError(execErr)
			if item.ErrorCode == "cancelled" || jobCancellationRequested(job) {
				item.Status = "cancelled"
				item.ErrorCode = "cancelled"
			} else {
				item.Status = "failed"
				failed++
			}
		} else {
			item.Status = "success"
			item.Output = sanitizeVirtualRepositoryOperationResultText(output)
			succeeded++
		}
		log.Printf("[vrepo] operation_node repo=%q job=%q node=%q kind=%q action=%q status=%s error_code=%q duration_ms=%d error=%q", repo.ID, jobID, node.ID, node.Repository.Kind, req.Action, item.Status, item.ErrorCode, item.DurationMS, virtualRepositoryLogError(execErr))
		job.mu.Lock()
		remainingTextBytes := virtualRepositoryOperationJobTextMaxBytes - job.retainedTextBytes
		if item.Error != "" {
			item.Error = sanitizeVirtualRepositoryOperationResultTextLimit(item.Error, min(remainingTextBytes, virtualRepositoryOperationResultTextMaxBytes))
			job.retainedTextBytes += len(item.Error)
		} else if item.Output != "" {
			item.Output = sanitizeVirtualRepositoryOperationResultTextLimit(item.Output, min(remainingTextBytes, virtualRepositoryOperationResultTextMaxBytes))
			job.retainedTextBytes += len(item.Output)
		}
		job.result.Items = append(job.result.Items, item)
		completed := len(job.result.Items)
		targetCount := job.targetCount
		job.mu.Unlock()
		if shouldEmitVirtualRepositoryJobProgress(completed, targetCount) {
			a.emitVirtualRepositoryJob(job)
		}
	}
	job.mu.Lock()
	job.result.FinishedAt = time.Now().UTC()
	if job.cancelRequested || ctx.Err() != nil {
		job.result.Status = "cancelled"
	} else if failed > 0 && succeeded > 0 {
		job.result.Status = "partial_success"
	} else if failed > 0 {
		job.result.Status = "failed"
	} else {
		job.result.Status = "success"
	}
	finalStatus := job.result.Status
	completed := len(job.result.Items)
	job.mu.Unlock()
	log.Printf("[vrepo] operation_finish repo=%q job=%q action=%q status=%s succeeded=%d failed=%d completed=%d duration_ms=%d", repo.ID, jobID, req.Action, finalStatus, succeeded, failed, completed, time.Since(jobStarted).Milliseconds())
	a.emitVirtualRepositoryJob(job)
}

func jobCancellationRequested(job *virtualRepositoryOperationJob) bool {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.cancelRequested
}

func marshalVirtualRepositoryOperationJob(job *virtualRepositoryOperationJob) (string, error) {
	job.mu.RLock()
	defer job.mu.RUnlock()
	data, err := json.Marshal(job.result)
	return string(data), err
}

func (a *App) emitVirtualRepositoryJob(job *virtualRepositoryOperationJob) {
	if raw, err := marshalVirtualRepositoryOperationJob(job); err == nil {
		a.emitEvent("virtual-repository:job-updated", raw)
	}
}

// Bound full-result events to roughly one hundred updates for large virtual
// repositories. Clients can still poll, and the terminal result is always
// emitted by the caller after the loop.
func shouldEmitVirtualRepositoryJobProgress(completed, total int) bool {
	if completed <= 10 || completed >= total || total <= 100 {
		return true
	}
	stride := (total + 99) / 100
	return completed%stride == 0
}

func (a *App) GetVirtualRepositoryOperation(jobID string) (string, error) {
	virtualRepositoryOperationJobs.Lock()
	job := virtualRepositoryOperationJobs.items[strings.TrimSpace(jobID)]
	virtualRepositoryOperationJobs.Unlock()
	if job == nil {
		return "", errors.New("virtual repository operation not found")
	}
	return marshalVirtualRepositoryOperationJob(job)
}

func (a *App) CancelVirtualRepositoryOperation(jobID string) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.New("virtual repository operation job id is required")
	}
	virtualRepositoryOperationJobs.Lock()
	job := virtualRepositoryOperationJobs.items[jobID]
	virtualRepositoryOperationJobs.Unlock()
	if job == nil {
		return errors.New("virtual repository operation not found")
	}
	job.mu.Lock()
	status := job.result.Status
	cancel := job.cancel
	if status != "running" {
		job.mu.Unlock()
		return fmt.Errorf("virtual repository operation is not running (status=%s)", status)
	}
	if cancel == nil {
		job.mu.Unlock()
		return errors.New("virtual repository operation cannot be cancelled")
	}
	if job.cancelRequested {
		job.mu.Unlock()
		return nil
	}
	job.cancelRequested = true
	job.mu.Unlock()
	cancel()
	return nil
}

func (a *App) executeVirtualRepositoryOperation(parent context.Context, repo *VirtualRepository, node VirtualRepositoryNode, req VirtualRepositoryOperationRequest) (string, error) {
	return a.executeVirtualRepositoryOperationWithClients(parent, repo, node, req, nil)
}

// executeVirtualRepositoryOperationWithClients shares resolved VCS clients
// across one job. A multi-repository operation therefore validates each client
// at most once instead of spawning a version process for every target.
func (a *App) executeVirtualRepositoryOperationWithClients(parent context.Context, repo *VirtualRepository, node VirtualRepositoryNode, req VirtualRepositoryOperationRequest, clients virtualRepositoryVCSClients) (string, error) {
	if err := validateVirtualRepositoryOperationForBinding(node.Repository, req.Action); err != nil {
		return "", err
	}
	if repo.Remote != nil {
		return a.executeRemoteVirtualRepositoryOperation(parent, repo, node, req)
	}
	path, err := resolveVirtualRepositoryPath(repo.RootPath, node.Repository.RelativePath, true)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	credential, secret, err := a.repositoryCredentialForNode(repo.ID, node.ID, node.Repository.Kind, node.Repository.RemoteURL)
	if err != nil {
		return "", err
	}
	if node.Repository.Kind == "git" {
		client := a.virtualRepositoryVCSClient("git", clients)
		if !client.Available {
			return "", errors.New(client.Error)
		}
		return executeGitVirtualRepositoryOperation(ctx, client.Executable, path, req, credential, secret, node.Repository.RemoteURL)
	}
	client := a.virtualRepositoryVCSClient("svn", clients)
	if !client.Available {
		return "", errors.New(client.Error)
	}
	return executeSVNVirtualRepositoryOperation(ctx, client.Executable, path, req, credential, secret)
}

func executeGitVirtualRepositoryOperation(ctx context.Context, executable, path string, req VirtualRepositoryOperationRequest, credential *RepositoryCredentialMetadata, secret, expectedRemote string) (string, error) {
	var outputs []string
	extraEnv := []string{"GIT_TERMINAL_PROMPT=0"}
	// Push the configured URL directly. Inspection has already verified that it
	// matches the working copy, and this also supports repositories whose remote
	// is not named "origin".
	remoteArg := sanitizeRepositoryRemoteURL(expectedRemote)
	if (req.Action == "push" || req.Action == "commit_push") && remoteArg == "" {
		return "", &virtualRepositoryOperationError{Code: "remote_unavailable", Err: errors.New("configured Git repository URL is required for push")}
	}
	cleanup := func() {}
	if credential != nil {
		if strings.ContainsAny(credential.Username, "\r\n") || strings.ContainsAny(secret, "\r\n") {
			return "", &virtualRepositoryOperationError{Code: "credential_unavailable", Err: errors.New("Git credential contains unsupported line breaks")}
		}
		askPass, err := createGitAskPassScript()
		if err != nil {
			return "", err
		}
		cleanup = askPass.cleanup
		extraEnv = append(extraEnv,
			"GIT_ASKPASS="+askPass.path,
			"GIT_ASKPASS_REQUIRE=force",
			"MACLAW_VREPO_GIT_USERNAME="+credential.Username,
			"MACLAW_VREPO_GIT_SECRET="+secret,
		)
	}
	defer cleanup()
	run := func(args ...string) error {
		out, err := runVCSCommandEnv(ctx, executable, path, extraEnv, args...)
		if out != "" {
			outputs = append(outputs, out)
		}
		return err
	}
	switch req.Action {
	case "sync":
		// Keep an existing checkout current without creating a merge commit. This
		// makes the in-app action safe to use for both Git worktrees and the
		// already-checked-out virtual-repository mappings shown in the UI.
		if err := run("pull", "--ff-only"); err != nil {
			return strings.Join(outputs, "\n"), err
		}
	case "commit", "commit_push":
		if err := run("add", "-A"); err != nil {
			return strings.Join(outputs, "\n"), err
		}
		staged, err := runVCSCommand(ctx, executable, path, "diff", "--cached", "--name-only")
		if err != nil {
			return strings.Join(outputs, "\n"), err
		}
		if strings.TrimSpace(staged) == "" {
			return strings.Join(outputs, "\n"), errors.New("nothing to commit")
		}
		if err := run("commit", "-m", req.Message); err != nil {
			return strings.Join(outputs, "\n"), err
		}
		if req.Action == "commit_push" {
			if err := run("push", remoteArg); err != nil {
				return strings.Join(outputs, "\n"), &virtualRepositoryOperationError{Code: "push_failed_after_commit", Err: fmt.Errorf("commit succeeded but push failed: %w", err)}
			}
		}
	case "push":
		if err := run("push", remoteArg); err != nil {
			return strings.Join(outputs, "\n"), err
		}
	case "revert":
		if err := run("restore", "--staged", "--worktree", "--", "."); err != nil {
			if fallbackErr := run("reset", "--mixed", "HEAD", "--"); fallbackErr != nil {
				return strings.Join(outputs, "\n"), fmt.Errorf("git restore failed: %v; reset fallback failed: %w", err, fallbackErr)
			}
			if fallbackErr := run("checkout", "--", "."); fallbackErr != nil {
				return strings.Join(outputs, "\n"), fallbackErr
			}
		}
	}
	return strings.Join(outputs, "\n"), nil
}

func validateVirtualRepositoryOperationForBinding(binding *VirtualRepositoryBinding, action string) error {
	if binding == nil {
		return &virtualRepositoryOperationError{Code: "invalid_target", Err: errors.New("repository mapping is missing")}
	}
	if binding.RefType == "tag" && binding.RefName != "" && action != "revert" {
		return &virtualRepositoryOperationError{Code: "tag_read_only", Err: errors.New("tag checkouts are read-only; commit and push require a branch checkout")}
	}
	return nil
}

func isHTTPSRepositoryURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

func executeSVNVirtualRepositoryOperation(ctx context.Context, executable, path string, req VirtualRepositoryOperationRequest, credential *RepositoryCredentialMetadata, secret string) (string, error) {
	auth, stdin, err := svnVirtualRepositoryAuth(ctx, executable, credential, secret)
	if err != nil {
		return "", err
	}
	switch req.Action {
	case "sync":
		return runVCSCommandInputEnv(ctx, executable, path, nil, stdin, append([]string{"update"}, auth...)...)
	case "push":
		return "SVN commit already uploads changes; push skipped", nil
	case "commit", "commit_push":
		return runVCSCommandInputEnv(ctx, executable, path, nil, stdin, append([]string{"commit", "-m", req.Message}, auth...)...)
	case "revert":
		return runVCSCommand(ctx, executable, path, "revert", "-R", ".")
	default:
		return "", errors.New("unsupported SVN operation")
	}
}

func svnVirtualRepositoryAuth(ctx context.Context, executable string, credential *RepositoryCredentialMetadata, secret string) ([]string, string, error) {
	auth := []string{"--non-interactive", "--no-auth-cache"}
	if credential == nil {
		return auth, "", nil
	}
	if strings.ContainsAny(credential.Username, "\r\n") || strings.ContainsAny(secret, "\r\n") {
		return nil, "", &virtualRepositoryOperationError{Code: "credential_unavailable", Err: errors.New("SVN credential contains unsupported line breaks")}
	}
	if !svnSupportsPasswordFromStdin(ctx, executable) {
		return nil, "", &virtualRepositoryOperationError{Code: "credential_unavailable", Err: errors.New("installed SVN client does not support secure password input; upgrade SVN or use its external credential cache")}
	}
	return append(auth, "--username", credential.Username, "--password-from-stdin"), secret + "\n", nil
}

func svnSupportsPasswordFromStdin(ctx context.Context, executable string) bool {
	cleanExecutable := filepath.Clean(strings.TrimSpace(executable))
	info, statErr := os.Stat(cleanExecutable)
	if statErr == nil {
		svnPasswordStdinCapability.Lock()
		if svnPasswordStdinCapability.known && svnPasswordStdinCapability.executable == cleanExecutable && svnPasswordStdinCapability.modTime.Equal(info.ModTime()) && svnPasswordStdinCapability.size == info.Size() {
			supported := svnPasswordStdinCapability.supported
			svnPasswordStdinCapability.Unlock()
			return supported
		}
		svnPasswordStdinCapability.Unlock()
	}
	help, err := runVCSCommand(ctx, executable, "", "help", "commit")
	supported := err == nil && strings.Contains(help, "--password-from-stdin")
	if statErr == nil && ctx.Err() == nil {
		svnPasswordStdinCapability.Lock()
		svnPasswordStdinCapability.executable = cleanExecutable
		svnPasswordStdinCapability.modTime = info.ModTime()
		svnPasswordStdinCapability.size = info.Size()
		svnPasswordStdinCapability.supported = supported
		svnPasswordStdinCapability.known = true
		svnPasswordStdinCapability.Unlock()
	}
	return supported
}

type temporaryAskPass struct {
	path    string
	cleanup func()
}

func createGitAskPassScript() (temporaryAskPass, error) {
	dir, err := os.MkdirTemp("", "maclaw-vrepo-auth-*")
	if err != nil {
		return temporaryAskPass{}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "askpass.sh")
	contents := "#!/bin/sh\ncase \"$1\" in\n  *sername*) printf '%s\\n' \"$MACLAW_VREPO_GIT_USERNAME\" ;;\n  *) printf '%s\\n' \"$MACLAW_VREPO_GIT_SECRET\" ;;\nesac\n"
	if goruntime.GOOS == "windows" {
		path = filepath.Join(dir, "askpass.cmd")
		contents = "@echo off\r\necho %1 | findstr /I username >nul\r\nif %errorlevel%==0 (echo %MACLAW_VREPO_GIT_USERNAME%) else (echo %MACLAW_VREPO_GIT_SECRET%)\r\n"
	}
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		cleanup()
		return temporaryAskPass{}, err
	}
	return temporaryAskPass{path: path, cleanup: cleanup}, nil
}
