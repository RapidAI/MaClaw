package pyenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

const sharedPythonRuntimeSchema = "maclaw.python_runtime.v1"

type SharedPythonRuntimeSpec struct {
	Python       string   `json:"python,omitempty"`
	Manager      string   `json:"manager,omitempty"`
	Packages     []string `json:"packages,omitempty"`
	IndexURLs    []string `json:"index_urls,omitempty"`
	ExtraHashKey string   `json:"extra_hash_key,omitempty"`
}

type SharedPythonRuntimePlan struct {
	Schema        string   `json:"schema"`
	ID            string   `json:"id"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	Manager       string   `json:"manager"`
	Python        string   `json:"python"`
	PythonRequest string   `json:"python_request"`
	Packages      []string `json:"packages"`
	IndexURLs     []string `json:"index_urls,omitempty"`
	RootDir       string   `json:"root_dir"`
	EnvDir        string   `json:"env_dir"`
	PythonPath    string   `json:"python_path"`
	LockPath      string   `json:"lock_path"`
	CacheDir      string   `json:"cache_dir"`
}

type SharedPythonRuntimeStatus struct {
	SharedPythonRuntimePlan
	Status     string   `json:"status"`
	UsedBy     []string `json:"used_by,omitempty"`
	CreatedAt  string   `json:"created_at,omitempty"`
	UpdatedAt  string   `json:"updated_at,omitempty"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
	HasLock    bool     `json:"has_lock"`
	HasPython  bool     `json:"has_python"`
	Error      string   `json:"error,omitempty"`
}

type sharedPythonRuntimeLock struct {
	SharedPythonRuntimePlan
	Status     string   `json:"status"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	UsedBy     []string `json:"used_by,omitempty"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
}

var sharedRuntimeExecCommand = exec.Command

var (
	sharedRuntimeMu       sync.Mutex
	sharedRuntimeLocks    = map[string]*sync.Mutex{}
	sharedRuntimeLocksRef = map[string]int{}
)

func PlanSharedPythonRuntime(dataDir string, spec SharedPythonRuntimeSpec) (SharedPythonRuntimePlan, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return SharedPythonRuntimePlan{}, fmt.Errorf("data dir is required for shared python runtime")
	}
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return SharedPythonRuntimePlan{}, err
	}
	packages := normalizeRuntimeList(spec.Packages)
	indexURLs := normalizeRuntimeList(spec.IndexURLs)
	pythonRequest := strings.TrimSpace(spec.Python)
	python := selectSharedPythonVersion(pythonRequest)
	manager := strings.ToLower(strings.TrimSpace(spec.Manager))
	if manager == "" {
		manager = "uv"
	}
	if manager != "uv" {
		return SharedPythonRuntimePlan{}, fmt.Errorf("unsupported python runtime manager %q", spec.Manager)
	}
	hashInput := map[string]any{
		"schema":         sharedPythonRuntimeSchema,
		"os":             runtime.GOOS,
		"arch":           runtime.GOARCH,
		"manager":        manager,
		"python":         python,
		"python_request": pythonRequest,
		"packages":       packages,
		"index_urls":     indexURLs,
		"extra":          strings.TrimSpace(spec.ExtraHashKey),
	}
	hashData, _ := json.Marshal(hashInput)
	sum := sha256.Sum256(hashData)
	id := "sha256-" + hex.EncodeToString(sum[:])[:16]
	root := filepath.Join(absDataDir, "runtimes", "python")
	envDir := filepath.Join(root, "envs", id, ".venv")
	return SharedPythonRuntimePlan{
		Schema:        sharedPythonRuntimeSchema,
		ID:            id,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Manager:       manager,
		Python:        python,
		PythonRequest: pythonRequest,
		Packages:      packages,
		IndexURLs:     indexURLs,
		RootDir:       root,
		EnvDir:        envDir,
		PythonPath:    sharedRuntimePythonPath(envDir),
		LockPath:      filepath.Join(root, "envs", id, "runtime.lock.json"),
		CacheDir:      filepath.Join(root, "uv-cache"),
	}, nil
}

func EnsureSharedPythonRuntime(spec SharedPythonRuntimeSpec, usedBy string, emit ProgressFunc) (SharedPythonRuntimePlan, error) {
	return EnsureSharedPythonRuntimeWithDataDir(filepath.Join(corelib.MaclawBaseDir(), "data"), spec, usedBy, emit)
}

func EnsureSharedPythonRuntimeWithDataDir(dataDir string, spec SharedPythonRuntimeSpec, usedBy string, emit ProgressFunc) (SharedPythonRuntimePlan, error) {
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		return SharedPythonRuntimePlan{}, err
	}
	if len(plan.Packages) == 0 {
		return plan, nil
	}
	unlock := lockSharedPythonRuntime(plan.ID)
	defer unlock()
	uvPath, err := resolveUVExecutable()
	if err != nil {
		return plan, err
	}
	if sharedPythonRuntimeReady(plan) {
		if err := writeSharedRuntimeLock(plan, usedBy, "ready"); err != nil {
			return plan, err
		}
		return plan, nil
	}
	if emit != nil {
		emit("python_runtime", 10, "preparing shared Python "+plan.Python)
	}
	if err := os.MkdirAll(filepath.Dir(plan.EnvDir), 0o755); err != nil {
		return plan, err
	}
	if err := os.MkdirAll(plan.CacheDir, 0o755); err != nil {
		return plan, err
	}
	if err := writeSharedRuntimeLock(plan, usedBy, "installing"); err != nil {
		return plan, err
	}
	if err := runUV(plan, uvPath, "python", "install", plan.Python); err != nil {
		return plan, err
	}
	if _, err := os.Stat(plan.PythonPath); os.IsNotExist(err) {
		if emit != nil {
			emit("python_runtime", 35, "creating shared virtual environment")
		}
		if err := runUV(plan, uvPath, "venv", plan.EnvDir, "--python", plan.Python); err != nil {
			return plan, err
		}
	} else if err != nil {
		return plan, err
	}
	if emit != nil {
		emit("python_runtime", 60, "installing Python dependencies")
	}
	args := []string{"pip", "install", "--python", plan.PythonPath}
	for _, indexURL := range plan.IndexURLs {
		args = append(args, "--index-url", indexURL)
	}
	args = append(args, plan.Packages...)
	if err := runUV(plan, uvPath, args...); err != nil {
		return plan, err
	}
	if err := writeSharedRuntimeLock(plan, usedBy, "ready"); err != nil {
		return plan, err
	}
	if emit != nil {
		emit("python_runtime", 100, "shared Python runtime ready")
	}
	return plan, nil
}

func lockSharedPythonRuntime(id string) func() {
	id = strings.TrimSpace(id)
	if id == "" {
		return func() {}
	}
	sharedRuntimeMu.Lock()
	mu := sharedRuntimeLocks[id]
	if mu == nil {
		mu = &sync.Mutex{}
		sharedRuntimeLocks[id] = mu
	}
	sharedRuntimeLocksRef[id]++
	sharedRuntimeMu.Unlock()

	mu.Lock()
	return func() {
		mu.Unlock()
		sharedRuntimeMu.Lock()
		sharedRuntimeLocksRef[id]--
		if sharedRuntimeLocksRef[id] <= 0 {
			delete(sharedRuntimeLocksRef, id)
			delete(sharedRuntimeLocks, id)
		}
		sharedRuntimeMu.Unlock()
	}
}

func sharedPythonRuntimeReady(plan SharedPythonRuntimePlan) bool {
	if _, err := os.Stat(plan.PythonPath); err != nil {
		return false
	}
	data, err := os.ReadFile(plan.LockPath)
	if err != nil {
		return false
	}
	var lock sharedPythonRuntimeLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return false
	}
	return lock.ID == plan.ID && strings.EqualFold(strings.TrimSpace(lock.Status), "ready")
}

func normalizeRuntimeList(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func selectSharedPythonVersion(constraint string) string {
	c := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(constraint)), " ", "")
	switch {
	case strings.Contains(c, ">=3.13") && !strings.Contains(c, "<3.13"):
		return "3.13"
	case strings.Contains(c, "<3.12"):
		return "3.11"
	default:
		return "3.12"
	}
}

func sharedRuntimePythonPath(envDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(envDir, "Scripts", "python.exe")
	}
	return filepath.Join(envDir, "bin", "python")
}

func resolveUVExecutable() (string, error) {
	if path, err := exec.LookPath("uv"); err == nil {
		return path, nil
	}
	private, err := privateUVPath()
	if err == nil {
		if _, statErr := os.Stat(private); statErr == nil {
			return private, nil
		}
	}
	return "", fmt.Errorf("uv not found")
}

func runUV(plan SharedPythonRuntimePlan, uvPath string, args ...string) error {
	cmd := sharedRuntimeExecCommand(uvPath, args...)
	cmd.Env = append(os.Environ(),
		"UV_CACHE_DIR="+plan.CacheDir,
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uv %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeSharedRuntimeLock(plan SharedPythonRuntimePlan, usedBy, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := sharedPythonRuntimeLock{}
	if data, err := os.ReadFile(plan.LockPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing.ID != "" && existing.ID != plan.ID {
		existing = sharedPythonRuntimeLock{}
	}
	lock := sharedPythonRuntimeLock{
		SharedPythonRuntimePlan: plan,
		Status:                  status,
		CreatedAt:               now,
		UpdatedAt:               now,
		LastUsedAt:              now,
	}
	if existing.CreatedAt != "" {
		lock.CreatedAt = existing.CreatedAt
	}
	lock.UsedBy = append([]string(nil), existing.UsedBy...)
	usedBy = strings.TrimSpace(usedBy)
	if usedBy != "" {
		found := false
		for _, item := range lock.UsedBy {
			if strings.EqualFold(strings.TrimSpace(item), usedBy) {
				found = true
				break
			}
		}
		if !found {
			lock.UsedBy = append(lock.UsedBy, usedBy)
		}
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(plan.LockPath, append(data, '\n'), 0o644)
}

func ListSharedPythonRuntimes(dataDir string) ([]SharedPythonRuntimeStatus, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("data dir is required for shared python runtime")
	}
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(absDataDir, "runtimes", "python")
	envsDir := filepath.Join(root, "envs")
	entries, err := os.ReadDir(envsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	statuses := make([]SharedPythonRuntimeStatus, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := strings.TrimSpace(entry.Name())
		envRoot := filepath.Join(envsDir, id)
		envDir := filepath.Join(envRoot, ".venv")
		plan := SharedPythonRuntimePlan{
			Schema:     sharedPythonRuntimeSchema,
			ID:         id,
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			Manager:    "uv",
			RootDir:    root,
			EnvDir:     envDir,
			PythonPath: sharedRuntimePythonPath(envDir),
			LockPath:   filepath.Join(envRoot, "runtime.lock.json"),
			CacheDir:   filepath.Join(root, "uv-cache"),
		}
		status := SharedPythonRuntimeStatus{
			SharedPythonRuntimePlan: plan,
			Status:                  "unknown",
		}
		if _, err := os.Stat(status.PythonPath); err == nil {
			status.HasPython = true
		} else if err != nil && !os.IsNotExist(err) {
			status.Error = err.Error()
		}
		if data, err := os.ReadFile(status.LockPath); err == nil {
			status.HasLock = true
			var lock sharedPythonRuntimeLock
			if err := json.Unmarshal(data, &lock); err != nil {
				status.Status = "invalid_lock"
				status.Error = err.Error()
			} else {
				status.SharedPythonRuntimePlan = mergeSharedPythonRuntimePlanDefaults(lock.SharedPythonRuntimePlan, plan)
				status.Status = firstNonEmptyRuntimeString(lock.Status, "unknown")
				status.UsedBy = append([]string(nil), lock.UsedBy...)
				status.CreatedAt = lock.CreatedAt
				status.UpdatedAt = lock.UpdatedAt
				status.LastUsedAt = lock.LastUsedAt
				if lock.ID != "" && lock.ID != id {
					status.Status = "invalid_lock"
					status.Error = fmt.Sprintf("lock id %q does not match directory %q", lock.ID, id)
				}
				if _, err := os.Stat(status.PythonPath); err == nil {
					status.HasPython = true
				} else if err != nil && !os.IsNotExist(err) {
					status.Error = err.Error()
				}
			}
		} else if err != nil && !os.IsNotExist(err) {
			status.Error = err.Error()
		}
		if status.Status == "ready" && !status.HasPython {
			status.Status = "missing_python"
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return strings.ToLower(statuses[i].ID) < strings.ToLower(statuses[j].ID)
	})
	return statuses, nil
}

func mergeSharedPythonRuntimePlanDefaults(plan, fallback SharedPythonRuntimePlan) SharedPythonRuntimePlan {
	if plan.Schema == "" {
		plan.Schema = fallback.Schema
	}
	if plan.ID == "" {
		plan.ID = fallback.ID
	}
	if plan.OS == "" {
		plan.OS = fallback.OS
	}
	if plan.Arch == "" {
		plan.Arch = fallback.Arch
	}
	if plan.Manager == "" {
		plan.Manager = fallback.Manager
	}
	if plan.RootDir == "" {
		plan.RootDir = fallback.RootDir
	}
	if plan.EnvDir == "" {
		plan.EnvDir = fallback.EnvDir
	}
	if plan.PythonPath == "" {
		plan.PythonPath = fallback.PythonPath
	}
	if plan.LockPath == "" {
		plan.LockPath = fallback.LockPath
	}
	if plan.CacheDir == "" {
		plan.CacheDir = fallback.CacheDir
	}
	return plan
}

func firstNonEmptyRuntimeString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
