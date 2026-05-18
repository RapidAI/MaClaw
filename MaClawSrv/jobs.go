package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

type asyncJobStatus string

const (
	asyncJobStatusPending   asyncJobStatus = "pending"
	asyncJobStatusRunning   asyncJobStatus = "running"
	asyncJobStatusSucceeded asyncJobStatus = "succeeded"
	asyncJobStatusFailed    asyncJobStatus = "failed"
	asyncJobStatusCanceled  asyncJobStatus = "canceled"
)

const (
	asyncJobRetention = 24 * time.Hour
	asyncJobMaxCount  = 2000
)

type asyncJobView struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Status      asyncJobStatus  `json:"status"`
	TenantID    string          `json:"tenant_id"`
	UserID      string          `json:"user_id"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type asyncJobRecord struct {
	asyncJobView
	cancel context.CancelFunc
}

type asyncJobSnapshot struct {
	Items []asyncJobView `json:"items"`
}

type asyncJobManager struct {
	mu       sync.RWMutex
	jobs     map[string]*asyncJobRecord
	dataRoot string
	filePath string
}

func newAsyncJobManager(dataRoot string) *asyncJobManager {
	m := &asyncJobManager{jobs: map[string]*asyncJobRecord{}, dataRoot: dataRoot}
	root := filepath.Clean(filepath.Join(dataRoot, "state"))
	if stringsTrim(dataRoot) == "" {
		return m
	}
	m.filePath = filepath.Join(root, "jobs.json")
	m.loadFromDisk()
	return m
}

func (m *asyncJobManager) createUserJob(kind string, p agentservice.Principal, run func(context.Context) (any, error)) *asyncJobRecord {
	now := time.Now().UTC()
	ctx, cancel := context.WithCancel(context.Background())
	job := &asyncJobRecord{asyncJobView: asyncJobView{
		ID:        agentservice.NewID("job"),
		Kind:      kind,
		Status:    asyncJobStatusPending,
		TenantID:  p.TenantID,
		UserID:    p.UserID,
		CreatedAt: now,
	}, cancel: cancel}
	m.mu.Lock()
	m.pruneLocked(now)
	m.jobs[job.ID] = job
	m.persistLocked()
	m.mu.Unlock()
	go m.execute(ctx, job.ID, run)
	return m.snapshot(job)
}

func (m *asyncJobManager) execute(ctx context.Context, jobID string, run func(context.Context) (any, error)) {
	started := time.Now().UTC()
	m.mu.Lock()
	job := m.jobs[jobID]
	if job != nil {
		job.Status = asyncJobStatusRunning
		job.StartedAt = &started
		m.persistLocked()
	}
	m.mu.Unlock()
	if job == nil {
		return
	}

	result, err := run(ctx)
	completed := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()
	job = m.jobs[jobID]
	if job == nil {
		return
	}
	job.CompletedAt = &completed
	job.cancel = nil
	if err != nil {
		if ctx.Err() == context.Canceled {
			job.Status = asyncJobStatusCanceled
			job.Error = "job canceled"
			m.persistLocked()
			return
		}
		job.Status = asyncJobStatusFailed
		job.Error = redactAsyncJobText(m.dataRoot, err.Error())
		m.persistLocked()
		return
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		job.Status = asyncJobStatusFailed
		job.Error = redactAsyncJobText(m.dataRoot, marshalErr.Error())
		m.persistLocked()
		return
	}
	job.Status = asyncJobStatusSucceeded
	job.Result = redactAsyncJobRawMessage(m.dataRoot, payload)
	job.Error = ""
	m.persistLocked()
}

func (m *asyncJobManager) getUserJob(jobID string, p agentservice.Principal) (*asyncJobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != p.TenantID || job.UserID != p.UserID {
		return nil, false
	}
	return m.snapshot(job), true
}

func (m *asyncJobManager) listUserJobs(p agentservice.Principal, kind string, status asyncJobStatus) []asyncJobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	items := make([]asyncJobRecord, 0, len(m.jobs))
	kind = stringsTrim(kind)
	for _, job := range m.jobs {
		if job.TenantID != p.TenantID || job.UserID != p.UserID {
			continue
		}
		if kind != "" && job.Kind != kind {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		items = append(items, *m.snapshot(job))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (m *asyncJobManager) cancelUserJob(jobID string, p agentservice.Principal) (*asyncJobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != p.TenantID || job.UserID != p.UserID {
		return nil, false
	}
	if (job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning) && job.cancel != nil {
		job.cancel()
	}
	return m.snapshot(job), true
}

func (m *asyncJobManager) deleteUserJob(jobID string, p agentservice.Principal) (*asyncJobRecord, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok || job.TenantID != p.TenantID || job.UserID != p.UserID {
		return nil, false, false
	}
	if job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning {
		return m.snapshot(job), true, false
	}
	out := m.snapshot(job)
	delete(m.jobs, jobID)
	m.persistLocked()
	return out, true, true
}

func (m *asyncJobManager) deleteUserJobs(p agentservice.Principal, kind string, status asyncJobStatus, before *time.Time) []asyncJobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	kind = stringsTrim(kind)
	deleted := make([]asyncJobRecord, 0)
	for id, job := range m.jobs {
		if job.TenantID != p.TenantID || job.UserID != p.UserID {
			continue
		}
		if job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning {
			continue
		}
		if kind != "" && job.Kind != kind {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		if before != nil && !job.CreatedAt.Before(*before) {
			continue
		}
		deleted = append(deleted, *m.snapshot(job))
		delete(m.jobs, id)
	}
	if len(deleted) > 0 {
		sort.Slice(deleted, func(i, j int) bool {
			return deleted[i].CreatedAt.Before(deleted[j].CreatedAt)
		})
		m.persistLocked()
	}
	return deleted
}

func (m *asyncJobManager) pruneLocked(now time.Time) {
	if len(m.jobs) == 0 {
		return
	}
	changed := false
	for id, job := range m.jobs {
		if job.CompletedAt != nil && now.Sub(*job.CompletedAt) > asyncJobRetention {
			delete(m.jobs, id)
			changed = true
		}
	}
	if len(m.jobs) > asyncJobMaxCount {
		terminal := make([]*asyncJobRecord, 0, len(m.jobs))
		for _, job := range m.jobs {
			if job.CompletedAt != nil {
				terminal = append(terminal, job)
			}
		}
		sort.Slice(terminal, func(i, j int) bool {
			if terminal[i].CompletedAt == nil || terminal[j].CompletedAt == nil {
				return false
			}
			return terminal[i].CompletedAt.Before(*terminal[j].CompletedAt)
		})
		for len(m.jobs) > asyncJobMaxCount && len(terminal) > 0 {
			delete(m.jobs, terminal[0].ID)
			terminal = terminal[1:]
			changed = true
		}
	}
	if changed {
		m.persistLocked()
	}
}

func (m *asyncJobManager) snapshotCounts() map[asyncJobStatus]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	counts := map[asyncJobStatus]int{}
	for _, job := range m.jobs {
		counts[job.Status]++
	}
	return counts
}
func (m *asyncJobManager) loadFromDisk() {
	if stringsTrim(m.filePath) == "" {
		return
	}
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return
	}
	var snap asyncJobSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	now := time.Now().UTC()
	for _, item := range snap.Items {
		record := &asyncJobRecord{asyncJobView: item}
		if record.Status == asyncJobStatusPending || record.Status == asyncJobStatusRunning {
			record.Status = asyncJobStatusFailed
			record.Error = "service restarted before job completed"
			record.CompletedAt = &now
		}
		m.jobs[record.ID] = record
	}
	m.mu.Lock()
	m.pruneLocked(now)
	m.persistLocked()
	m.mu.Unlock()
}

func (m *asyncJobManager) persistLocked() {
	if stringsTrim(m.filePath) == "" {
		return
	}
	items := make([]asyncJobView, 0, len(m.jobs))
	for _, job := range m.jobs {
		items = append(items, m.snapshot(job).asyncJobView)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	payload, err := json.MarshalIndent(asyncJobSnapshot{Items: items}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.filePath), 0o700); err != nil {
		return
	}
	tmp := m.filePath + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, m.filePath)
}

func (m *asyncJobManager) snapshot(job *asyncJobRecord) *asyncJobRecord {
	if job == nil {
		return nil
	}
	copy := *job
	copy.cancel = nil
	copy.Error = redactAsyncJobText(m.dataRoot, copy.Error)
	if job.Result != nil {
		copy.Result = redactAsyncJobRawMessage(m.dataRoot, append(json.RawMessage(nil), job.Result...))
	}
	return &copy
}

func redactAsyncJobText(dataRoot, text string) string {
	return redactSupportBundleText(dataRoot, stringsTrim(text))
}

func redactAsyncJobRawMessage(dataRoot string, payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		redacted := []byte(redactSupportBundleText(dataRoot, string(payload)))
		if !json.Valid(redacted) {
			return json.RawMessage(`{"redacted":true}`)
		}
		return json.RawMessage(redacted)
	}
	redacted, err := json.Marshal(redactAsyncJobValue(dataRoot, "", value))
	if err != nil || !json.Valid(redacted) {
		return json.RawMessage(`{"redacted":true}`)
	}
	return json.RawMessage(redacted)
}

func redactAsyncJobValue(dataRoot, key string, value any) any {
	switch v := value.(type) {
	case string:
		if asyncJobPathLikeKey(key) || supportBundleLooksAbsolutePath(v) {
			return redactSupportBundleValue(dataRoot, v)
		}
		if asyncJobEndpointLikeKey(key) || strings.Contains(v, "://") {
			return redactEndpointForAPI(dataRoot, v)
		}
		return redactSupportBundleText(dataRoot, v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactAsyncJobValue(dataRoot, key, v[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for childKey, childValue := range v {
			out[childKey] = redactAsyncJobValue(dataRoot, childKey, childValue)
		}
		return out
	default:
		return value
	}
}

func asyncJobPathLikeKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	switch key {
	case "path", "root_path", "file_path", "current_file", "relative_path", "data_dir", "runtime_dir", "workspace", "workspace_dir", "skill_dir", "project_path":
		return true
	}
	return strings.HasSuffix(key, "_path") || strings.HasSuffix(key, "_dir")
}

func asyncJobEndpointLikeKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	switch key {
	case "url", "uri", "endpoint", "endpoint_url", "base_url", "raw_url", "repo_url", "canonical_uri":
		return true
	}
	return strings.HasSuffix(key, "_url") || strings.HasSuffix(key, "_uri") || strings.HasSuffix(key, "_endpoint")
}

func stringsTrim(v string) string {
	for len(v) > 0 && (v[0] == ' ' || v[0] == '\t' || v[0] == '\n' || v[0] == '\r') {
		v = v[1:]
	}
	for len(v) > 0 {
		last := v[len(v)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		v = v[:len(v)-1]
	}
	return v
}

// adminJobListFilter scopes admin-wide async job listing.
type adminJobListFilter struct {
	Kind     string
	Status   asyncJobStatus
	TenantID string
	UserID   string
	Limit    int
}

func (m *asyncJobManager) listAllJobs(filter adminJobListFilter) []asyncJobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	kind := stringsTrim(filter.Kind)
	tenantID := stringsTrim(filter.TenantID)
	userID := stringsTrim(filter.UserID)
	items := make([]asyncJobRecord, 0, len(m.jobs))
	for _, job := range m.jobs {
		if kind != "" && job.Kind != kind {
			continue
		}
		if filter.Status != "" && job.Status != filter.Status {
			continue
		}
		if tenantID != "" && job.TenantID != tenantID {
			continue
		}
		if userID != "" && job.UserID != userID {
			continue
		}
		items = append(items, *m.snapshot(job))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items
}

func (m *asyncJobManager) cancelAnyJob(jobID string) (*asyncJobRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now().UTC())
	job, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}
	if (job.Status == asyncJobStatusPending || job.Status == asyncJobStatusRunning) && job.cancel != nil {
		job.cancel()
	}
	return m.snapshot(job), true
}
