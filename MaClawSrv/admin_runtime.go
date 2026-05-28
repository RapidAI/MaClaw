package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

var serviceStartedAt = time.Now().UTC()

type adminRuntimeStatus struct {
	GeneratedAt       time.Time              `json:"generated_at"`
	StartedAt         time.Time              `json:"started_at"`
	UptimeSeconds     int64                  `json:"uptime_seconds"`
	Version           map[string]string      `json:"version"`
	Process           adminProcessStatus     `json:"process"`
	Memory            adminMemoryStatus      `json:"memory"`
	Readiness         readinessReport        `json:"readiness"`
	Jobs              map[asyncJobStatus]int `json:"jobs"`
	Scheduler         adminSchedulerStatus   `json:"scheduler"`
	Sandbox           sandboxStatus          `json:"sandbox"`
	LastSandboxReport *sandboxDiagnoseReport `json:"last_sandbox_report,omitempty"`
	LogSources        []adminLogSource       `json:"log_sources"`
	DataRoot          string                 `json:"data_root"`
	RuntimeConfigDir  string                 `json:"runtime_config_dir"`
}

type adminProcessStatus struct {
	PID           int    `json:"pid"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	GoVersion     string `json:"go_version"`
	NumCPU        int    `json:"num_cpu"`
	NumGoroutine  int    `json:"num_goroutine"`
	GOMAXPROCS    int    `json:"gomaxprocs"`
	BuildMainPath string `json:"build_main_path,omitempty"`
}

type adminRuntimeGCResponse struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Before      adminMemoryStatus `json:"before"`
	After       adminMemoryStatus `json:"after"`
}
type adminMemoryStatus struct {
	AllocBytes      uint64 `json:"alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
	HeapAllocBytes  uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes  uint64 `json:"heap_inuse_bytes"`
	StackInuseBytes uint64 `json:"stack_inuse_bytes"`
	NumGC           uint32 `json:"num_gc"`
}

type adminSchedulerStatus struct {
	Enabled     bool                      `json:"enabled"`
	Path        string                    `json:"path"`
	Exists      bool                      `json:"exists"`
	TaskCount   int                       `json:"task_count"`
	ByStatus    map[string]int            `json:"by_status"`
	NextRunAt   *time.Time                `json:"next_run_at,omitempty"`
	LastErrorAt *time.Time                `json:"last_error_at,omitempty"`
	LastError   string                    `json:"last_error,omitempty"`
	RecentTasks []scheduler.ScheduledTask `json:"recent_tasks,omitempty"`
}

func (s *HTTPServer) handleAdminRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, redactAdminRuntimeStatusForAdminAPI(s.svc.DataRoot(), buildAdminRuntimeStatus(s)))
}

func (s *HTTPServer) handleAdminRuntimeGC(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	before := readAdminMemoryStatus()
	goruntime.GC()
	debug.FreeOSMemory()
	after := readAdminMemoryStatus()
	_ = s.recordAdminAudit(r.Context(), "admin.runtime_gc", "runtime", "process", map[string]string{"before_alloc_bytes": strconv.FormatUint(before.AllocBytes, 10), "after_alloc_bytes": strconv.FormatUint(after.AllocBytes, 10), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, adminRuntimeGCResponse{GeneratedAt: time.Now().UTC(), Before: before, After: after})
}
func (s *HTTPServer) handleAdminRuntimeGoroutines(w http.ResponseWriter, r *http.Request) {
	debugLevel := 2
	if raw := strings.TrimSpace(r.URL.Query().Get("debug")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 1 && parsed != 2) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "debug must be 1 or 2"})
			return
		}
		debugLevel = parsed
	}
	download := false
	if raw := strings.TrimSpace(r.URL.Query().Get("download")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid download"})
			return
		}
		download = parsed
	}
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, debugLevel); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	body := redactMultilineRuntimeDump(buf.String())
	_ = s.recordAdminAudit(r.Context(), "admin.runtime_goroutines", "runtime", "goroutines", map[string]string{"debug": strconv.Itoa(debugLevel), "download": strconv.FormatBool(download), "bytes": strconv.Itoa(len(body)), "remote_ip": requestClientIP(r)})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"maclawsrv-goroutines-%s.txt\"", time.Now().UTC().Format("20060102T150405Z")))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
func (s *HTTPServer) handleAdminRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	profileName := strings.TrimSpace(r.PathValue("profileName"))
	if !isAllowedRuntimeProfile(profileName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported runtime profile"})
		return
	}
	debugLevel := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("debug")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 1 && parsed != 2) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "debug must be 1 or 2"})
			return
		}
		debugLevel = parsed
	}
	forceGC := false
	if raw := strings.TrimSpace(r.URL.Query().Get("gc")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid gc"})
			return
		}
		forceGC = parsed
	}
	if forceGC {
		goruntime.GC()
	}
	download := false
	if raw := strings.TrimSpace(r.URL.Query().Get("download")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid download"})
			return
		}
		download = parsed
	}
	profile := pprof.Lookup(profileName)
	if profile == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "runtime profile not found"})
		return
	}
	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, debugLevel); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	body := redactMultilineRuntimeDump(buf.String())
	_ = s.recordAdminAudit(r.Context(), "admin.runtime_profile", "runtime_profile", profileName, map[string]string{"debug": strconv.Itoa(debugLevel), "download": strconv.FormatBool(download), "gc": strconv.FormatBool(forceGC), "bytes": strconv.Itoa(len(body)), "remote_ip": requestClientIP(r)})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"maclawsrv-%s-profile-%s.txt\"", profileName, time.Now().UTC().Format("20060102T150405Z")))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func isAllowedRuntimeProfile(name string) bool {
	switch name {
	case "allocs", "block", "heap", "mutex", "threadcreate":
		return true
	default:
		return false
	}
}
func (s *HTTPServer) handleAdminSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, redactAdminSchedulerStatusForAdminAPI(s.svc.DataRoot(), buildAdminSchedulerStatus(s.svc.DataRoot(), true)))
}

func (s *HTTPServer) handleAdminJobs(w http.ResponseWriter, r *http.Request) {
	status, ok := parseAsyncJobStatus(r.URL.Query().Get("status"), false)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	items := s.jobs.listAllJobs(adminJobListFilter{
		Kind:     strings.TrimSpace(r.URL.Query().Get("kind")),
		Status:   status,
		TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:   strings.TrimSpace(r.URL.Query().Get("user_id")),
		Limit:    limit,
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "counts": s.jobs.snapshotCounts()})
}

func (s *HTTPServer) handleAdminJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.jobs.getAnyJob(r.PathValue("jobId"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *HTTPServer) handleAdminCancelJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	job, ok := s.jobs.cancelAnyJob(r.PathValue("jobId"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.job_cancel", "job", job.ID, map[string]string{"tenant_id": job.TenantID, "user_id": job.UserID, "kind": job.Kind, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, job)
}

func redactAdminRuntimeStatusForAdminAPI(dataRoot string, status adminRuntimeStatus) adminRuntimeStatus {
	status.DataRoot = filepath.Base(status.DataRoot)
	status.RuntimeConfigDir = filepath.Base(status.RuntimeConfigDir)
	status.Readiness = redactReadinessReport(status.Readiness)
	status.Scheduler = redactAdminSchedulerStatusForAdminAPI(dataRoot, status.Scheduler)
	status.Sandbox = redactSandboxStatusForSupportBundle(dataRoot, status.Sandbox)
	status.LogSources = redactAdminLogSourcesForAdminAPI(dataRoot, status.LogSources)
	if status.LastSandboxReport != nil {
		reports := redactSandboxReportsForAdminAPI(dataRoot, []sandboxDiagnoseReport{*status.LastSandboxReport})
		status.LastSandboxReport = &reports[0]
	}
	return status
}

func redactAdminSchedulerStatusForAdminAPI(dataRoot string, status adminSchedulerStatus) adminSchedulerStatus {
	status.Path = redactSupportBundleValue(dataRoot, status.Path)
	status.LastError = redactSupportBundleText(dataRoot, status.LastError)
	for i := range status.RecentTasks {
		status.RecentTasks[i].Name = redactSupportBundleText(dataRoot, status.RecentTasks[i].Name)
		status.RecentTasks[i].Action = redactSupportBundleText(dataRoot, status.RecentTasks[i].Action)
		status.RecentTasks[i].LastResult = redactSupportBundleText(dataRoot, status.RecentTasks[i].LastResult)
		status.RecentTasks[i].LastError = redactSupportBundleText(dataRoot, status.RecentTasks[i].LastError)
	}
	return status
}
func buildAdminRuntimeStatus(s *HTTPServer) adminRuntimeStatus {

	buildMainPath := ""
	if info, ok := debug.ReadBuildInfo(); ok && info != nil {
		buildMainPath = info.Main.Path
	}
	lastSandboxReport, _ := latestSandboxReport(s.svc.DataRoot())
	return adminRuntimeStatus{
		GeneratedAt:   time.Now().UTC(),
		StartedAt:     serviceStartedAt,
		UptimeSeconds: int64(time.Since(serviceStartedAt).Seconds()),
		Version:       map[string]string{"version": serviceVersion, "commit": serviceCommit, "built_at": serviceBuiltAt},
		Process: adminProcessStatus{
			PID:           os.Getpid(),
			OS:            goruntime.GOOS,
			Arch:          goruntime.GOARCH,
			GoVersion:     goruntime.Version(),
			NumCPU:        goruntime.NumCPU(),
			NumGoroutine:  goruntime.NumGoroutine(),
			GOMAXPROCS:    goruntime.GOMAXPROCS(0),
			BuildMainPath: buildMainPath,
		},
		Memory: readAdminMemoryStatus(),

		Readiness:         buildReadinessReport(s.svc.DataRoot(), s.jobs.filePath),
		Jobs:              s.jobs.snapshotCounts(),
		Scheduler:         buildAdminSchedulerStatus(s.svc.DataRoot(), false),
		Sandbox:           buildSandboxStatus(s.svc.DataRoot(), false),
		LastSandboxReport: lastSandboxReport,
		LogSources:        adminLogSources(s.svc.DataRoot()),
		DataRoot:          s.svc.DataRoot(),
		RuntimeConfigDir:  adminStateDir(s.svc.DataRoot()),
	}
}

func redactMultilineRuntimeDump(in string) string {
	lines := strings.Split(in, "\n")
	for i, line := range lines {
		lines[i] = redactLogLine(line)
	}
	return strings.Join(lines, "\n")
}
func readAdminMemoryStatus() adminMemoryStatus {
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)
	return adminMemoryStatus{
		AllocBytes:      mem.Alloc,
		SysBytes:        mem.Sys,
		HeapAllocBytes:  mem.HeapAlloc,
		HeapInuseBytes:  mem.HeapInuse,
		StackInuseBytes: mem.StackInuse,
		NumGC:           mem.NumGC,
	}
}
func buildAdminSchedulerStatus(dataRoot string, includeTasks bool) adminSchedulerStatus {
	path := filepath.Join(dataRoot, "scheduled_tasks.json")
	enabled, _ := getenvBoolStrict("MACLAW_ENABLE_SCHEDULER", false)
	status := adminSchedulerStatus{Enabled: enabled, Path: path, ByStatus: map[string]int{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return status
	}
	status.Exists = true
	var tasks []scheduler.ScheduledTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		status.LastError = err.Error()
		return status
	}
	status.TaskCount = len(tasks)
	for _, task := range tasks {
		key := strings.TrimSpace(task.Status)
		if key == "" {
			key = "unknown"
		}
		status.ByStatus[key]++
		if task.NextRunAt != nil && (status.NextRunAt == nil || task.NextRunAt.Before(*status.NextRunAt)) {
			next := task.NextRunAt.UTC()
			status.NextRunAt = &next
		}
		if task.LastError != "" && task.LastRunAt != nil && (status.LastErrorAt == nil || task.LastRunAt.After(*status.LastErrorAt)) {
			last := task.LastRunAt.UTC()
			status.LastErrorAt = &last
			status.LastError = task.LastError
		}
	}
	if includeTasks {
		sort.Slice(tasks, func(i, j int) bool {
			if tasks[i].NextRunAt == nil && tasks[j].NextRunAt == nil {
				return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
			}
			if tasks[i].NextRunAt == nil {
				return false
			}
			if tasks[j].NextRunAt == nil {
				return true
			}
			return tasks[i].NextRunAt.Before(*tasks[j].NextRunAt)
		})
		if len(tasks) > 50 {
			tasks = tasks[:50]
		}
		status.RecentTasks = tasks
	}
	return status
}
