package pyenv

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlanSharedPythonRuntimeStableForPackageOrder(t *testing.T) {
	dataDir := t.TempDir()
	a, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{
		Python:   ">=3.10,<3.14",
		Manager:  "uv",
		Packages: []string{"babeldoc==0.6.3", "requests"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{
		Python:   ">=3.10,<3.14",
		Manager:  "uv",
		Packages: []string{"requests", "babeldoc==0.6.3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || a.EnvDir != b.EnvDir {
		t.Fatalf("package ordering should not change runtime id/env: %#v vs %#v", a, b)
	}
	if a.Python != "3.12" {
		t.Fatalf("Python = %q, want 3.12", a.Python)
	}
	wantRoot := filepath.Join(dataDir, "runtimes", "python")
	if a.RootDir != wantRoot {
		t.Fatalf("RootDir = %q, want %q", a.RootDir, wantRoot)
	}
	if a.CacheDir != filepath.Join(wantRoot, "uv-cache") {
		t.Fatalf("CacheDir = %q", a.CacheDir)
	}
}

func TestPlanSharedPythonRuntimeDifferentPackagesDifferentEnv(t *testing.T) {
	dataDir := t.TempDir()
	a, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"babeldoc==0.6.3"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"babeldoc==0.6.4"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatalf("different package sets should not share runtime id %q", a.ID)
	}
}

func TestPlanSharedPythonRuntimeRejectsUnsupportedManager(t *testing.T) {
	_, err := PlanSharedPythonRuntime(t.TempDir(), SharedPythonRuntimeSpec{Manager: "pip", Packages: []string{"requests"}})
	if err == nil {
		t.Fatal("expected unsupported manager error")
	}
}

func TestPlanSharedPythonRuntimeRequiresDataDir(t *testing.T) {
	if _, err := PlanSharedPythonRuntime("", SharedPythonRuntimeSpec{Packages: []string{"requests"}}); err == nil {
		t.Fatal("expected data dir error")
	}
	if _, err := ListSharedPythonRuntimes(""); err == nil {
		t.Fatal("expected data dir error")
	}
}

func TestListSharedPythonRuntimesReportsReadyAndMissingPython(t *testing.T) {
	dataDir := t.TempDir()
	ready, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"requests"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ready.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRuntimeLock(ready, "ready-skill", "ready"); err != nil {
		t.Fatal(err)
	}
	missing, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"babeldoc==0.6.3"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(missing.LockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRuntimeLock(missing, "missing-skill", "ready"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", filepath.Join(t.TempDir(), "pip-list.log"))
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() { sharedRuntimeExecCommand = oldExec })

	items, err := ListSharedPythonRuntimes(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]SharedPythonRuntimeStatus{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if got := byID[ready.ID]; got.Status != "ready" || !got.HasPython || !got.HasPip || !got.HasLock || len(got.UsedBy) != 1 || got.UsedBy[0] != "ready-skill" {
		t.Fatalf("ready runtime status = %#v", got)
	}
	if got := byID[missing.ID]; got.Status != "missing_python" || got.HasPython || !got.HasLock {
		t.Fatalf("missing runtime status = %#v", got)
	}
}

func TestListSharedPythonRuntimesReportsMissingPip(t *testing.T) {
	dataDir := t.TempDir()
	plan, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRuntimeLock(plan, "pdf-word", "ready"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", filepath.Join(t.TempDir(), "pip-list-missing.log"))
	t.Setenv("MACLAW_FAKE_PIP_FAIL_CONTAINS", "-m pip --version")
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() { sharedRuntimeExecCommand = oldExec })

	items, err := ListSharedPythonRuntimes(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one runtime", items)
	}
	if got := items[0]; got.Status != "missing_pip" || !got.HasPython || got.HasPip || !strings.Contains(got.Error, "pip") {
		t.Fatalf("runtime status = %#v, want missing_pip", got)
	}
}

func TestListSharedPythonRuntimesMissingRootIsEmpty(t *testing.T) {
	items, err := ListSharedPythonRuntimes(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v, want empty", items)
	}
}

func TestListSharedPythonRuntimesReportsMismatchedLockID(t *testing.T) {
	dataDir := t.TempDir()
	plan, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"requests"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.LockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := sharedPythonRuntimeLock{
		SharedPythonRuntimePlan: plan,
		Status:                  "ready",
	}
	lock.ID = "sha256-other"
	if err := writeSharedRuntimeLock(lock.SharedPythonRuntimePlan, "demo", lock.Status); err != nil {
		t.Fatal(err)
	}

	items, err := ListSharedPythonRuntimes(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one", items)
	}
	if items[0].Status != "invalid_lock" || !strings.Contains(items[0].Error, "does not match") || items[0].PythonPath == "" {
		t.Fatalf("runtime status = %#v, want invalid_lock with fallback paths", items[0])
	}
}

func TestListSharedPythonRuntimesReportsFailedStageAndError(t *testing.T) {
	dataDir := t.TempDir()
	plan, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}})
	if err != nil {
		t.Fatal(err)
	}
	failure := fmt.Errorf("uv not found")
	if _, err := failSharedPythonRuntime(plan, "pdf-word", "resolve_uv", failure); err == nil {
		t.Fatal("expected failure to be returned")
	}

	items, err := ListSharedPythonRuntimes(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one failed runtime", items)
	}
	got := items[0]
	if got.Status != "failed" || got.Stage != "resolve_uv" || got.Error != failure.Error() || len(got.UsedBy) != 1 || got.UsedBy[0] != "pdf-word" {
		t.Fatalf("failed runtime status = %#v", got)
	}
}

func TestListSharedPythonRuntimesReportsPipCapabilityForFailedRuntime(t *testing.T) {
	dataDir := t.TempDir()
	plan, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	failure := fmt.Errorf("previous install failed")
	if _, err := failSharedPythonRuntime(plan, "pdf-word", "pip_install", failure); err == nil {
		t.Fatal("expected failure to be returned")
	}
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", filepath.Join(t.TempDir(), "failed-runtime-pip.log"))
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() { sharedRuntimeExecCommand = oldExec })

	items, err := ListSharedPythonRuntimes(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one failed runtime", items)
	}
	got := items[0]
	if got.Status != "failed" || !got.HasPython || !got.HasPip || got.Stage != "pip_install" {
		t.Fatalf("failed runtime status = %#v, want failed with pip capability", got)
	}
}

func TestWriteSharedRuntimeLockPreservesCreatedAtAndMergesUsedBy(t *testing.T) {
	dataDir := t.TempDir()
	plan, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"requests"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.LockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRuntimeLock(plan, "skill-a", "ready"); err != nil {
		t.Fatal(err)
	}
	firstData, err := os.ReadFile(plan.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	var first sharedPythonRuntimeLock
	if err := json.Unmarshal(firstData, &first); err != nil {
		t.Fatal(err)
	}
	first.UpdatedAt = "2000-01-01T00:00:00Z"
	first.LastUsedAt = "2000-01-01T00:00:00Z"
	staleData, err := json.MarshalIndent(first, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.LockPath, append(staleData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRuntimeLock(plan, "skill-b", "ready"); err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(plan.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	var second sharedPythonRuntimeLock
	if err := json.Unmarshal(secondData, &second); err != nil {
		t.Fatal(err)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("CreatedAt = %q, want preserved %q", second.CreatedAt, first.CreatedAt)
	}
	if second.UpdatedAt == first.UpdatedAt {
		t.Fatalf("UpdatedAt was not refreshed: %q", second.UpdatedAt)
	}
	if got := strings.Join(second.UsedBy, ","); got != "skill-a,skill-b" {
		t.Fatalf("UsedBy = %q, want merged skill-a,skill-b", got)
	}
}

func TestEnsureSharedPythonRuntimeReinstallsPackagesWhenLockNotReady(t *testing.T) {
	dataDir := t.TempDir()
	spec := SharedPythonRuntimeSpec{Packages: []string{"rapidocr-onnxruntime==1.4.4"}}
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.LockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := sharedPythonRuntimeLock{
		SharedPythonRuntimePlan: plan,
		Status:                  "installing",
	}
	staleData, err := json.MarshalIndent(stale, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.LockPath, append(staleData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeBin := t.TempDir()
	fakeUV := filepath.Join(fakeBin, "uv")
	if runtime.GOOS == "windows" {
		fakeUV += ".exe"
	}
	if err := os.WriteFile(fakeUV, []byte("fake uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	logPath := filepath.Join(t.TempDir(), "uv.log")
	t.Setenv("MACLAW_FAKE_UV_LOG", logPath)
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(_ string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakeUV", "--"}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() { sharedRuntimeExecCommand = oldExec })

	ensured, err := EnsureSharedPythonRuntimeWithDataDir(dataDir, spec, "ocr-app", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ensured.ID != plan.ID {
		t.Fatalf("runtime ID = %q, want %q", ensured.ID, plan.ID)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "python install "+plan.Python) {
		t.Fatalf("expected python install command, got:\n%s", logText)
	}
	if strings.Contains(logText, "venv ") {
		t.Fatalf("existing venv should be reused, got:\n%s", logText)
	}
	if !strings.Contains(logText, "pip install --python "+plan.PythonPath+" rapidocr-onnxruntime==1.4.4") {
		t.Fatalf("expected pip install retry, got:\n%s", logText)
	}
	lockData, err := os.ReadFile(plan.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	var ready sharedPythonRuntimeLock
	if err := json.Unmarshal(lockData, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Status != "ready" || len(ready.UsedBy) != 1 || ready.UsedBy[0] != "ocr-app" {
		t.Fatalf("lock should be refreshed as ready with used_by: %#v", ready)
	}
}

func TestEnsureSharedPythonRuntimeWritesFailedLockOnFailure(t *testing.T) {
	dataDir := t.TempDir()
	spec := SharedPythonRuntimeSpec{Packages: []string{"rapidocr-onnxruntime==1.4.4"}}
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakeUV := filepath.Join(fakeBin, "uv")
	if runtime.GOOS == "windows" {
		fakeUV += ".exe"
	}
	if err := os.WriteFile(fakeUV, []byte("fake uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MACLAW_FAKE_UV_LOG", filepath.Join(t.TempDir(), "uv.log"))
	t.Setenv("MACLAW_FAKE_UV_FAIL_CONTAINS", "pip install")
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(_ string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakeUV", "--"}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() { sharedRuntimeExecCommand = oldExec })

	if _, err := EnsureSharedPythonRuntimeWithDataDir(dataDir, spec, "ocr-app", nil); err == nil {
		t.Fatal("expected fake pip install failure")
	}
	lockData, err := os.ReadFile(plan.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock sharedPythonRuntimeLock
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Status != "failed" || lock.Stage != "uv_pip_install" || lock.Error == "" || len(lock.UsedBy) != 1 || lock.UsedBy[0] != "ocr-app" {
		t.Fatalf("failed install should write failed lock with used_by/stage/error: %#v", lock)
	}
	if sharedPythonRuntimeReady(plan) {
		t.Fatal("failed lock must not be treated as ready")
	}
}

func TestEnsureSharedPythonRuntimeFallsBackToPipWhenUVMissing(t *testing.T) {
	dataDir := t.TempDir()
	spec := SharedPythonRuntimeSpec{Packages: []string{"pymupdf", "python-docx"}}
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "pip-fallback.log")
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", logPath)
	oldResolveUV := sharedRuntimeResolveUVExecutable
	sharedRuntimeResolveUVExecutable = func() (string, error) {
		return "", fmt.Errorf("uv not found")
	}
	oldResolveSeed := sharedRuntimeResolveSeedPython
	sharedRuntimeResolveSeedPython = func() (string, error) {
		return "python", nil
	}
	oldCheckPython := sharedRuntimeCheckPython
	sharedRuntimeCheckPython = func(string) (string, bool) {
		return "Python " + plan.Python + ".8", true
	}
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() {
		sharedRuntimeResolveUVExecutable = oldResolveUV
		sharedRuntimeResolveSeedPython = oldResolveSeed
		sharedRuntimeCheckPython = oldCheckPython
		sharedRuntimeExecCommand = oldExec
	})

	ensured, err := EnsureSharedPythonRuntimeWithDataDir(dataDir, spec, "pdf-word", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ensured.ID != plan.ID {
		t.Fatalf("runtime ID = %q, want %q", ensured.ID, plan.ID)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "-m venv "+plan.EnvDir) {
		t.Fatalf("expected venv creation, got:\n%s", logText)
	}
	if !strings.Contains(logText, plan.PythonPath+" -m pip install --quiet pymupdf python-docx") {
		t.Fatalf("expected venv pip install, got:\n%s", logText)
	}
	lockData, err := os.ReadFile(plan.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	var ready sharedPythonRuntimeLock
	if err := json.Unmarshal(lockData, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Status != "ready" || len(ready.UsedBy) != 1 || ready.UsedBy[0] != "pdf-word" {
		t.Fatalf("lock should be ready after pip fallback: %#v", ready)
	}
}

func TestEnsureSharedPythonRuntimePipFallbackRepairsExistingVenvWithoutSeedPython(t *testing.T) {
	dataDir := t.TempDir()
	spec := SharedPythonRuntimeSpec{Packages: []string{"pymupdf", "python-docx"}}
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "pip-existing-venv.log")
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", logPath)
	t.Setenv("MACLAW_FAKE_PIP_ENSUREPIP_MARKER", filepath.Join(t.TempDir(), "ensurepip.done"))
	oldResolveUV := sharedRuntimeResolveUVExecutable
	sharedRuntimeResolveUVExecutable = func() (string, error) {
		return "", fmt.Errorf("uv not found")
	}
	oldResolveSeed := sharedRuntimeResolveSeedPython
	sharedRuntimeResolveSeedPython = func() (string, error) {
		return "", fmt.Errorf("seed python unavailable")
	}
	oldCheckPython := sharedRuntimeCheckPython
	sharedRuntimeCheckPython = func(string) (string, bool) {
		t.Fatal("existing venv repair should not validate seed Python")
		return "", false
	}
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() {
		sharedRuntimeResolveUVExecutable = oldResolveUV
		sharedRuntimeResolveSeedPython = oldResolveSeed
		sharedRuntimeCheckPython = oldCheckPython
		sharedRuntimeExecCommand = oldExec
	})

	ensured, err := EnsureSharedPythonRuntimeWithDataDir(dataDir, spec, "pdf-word", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ensured.ID != plan.ID {
		t.Fatalf("runtime ID = %q, want %q", ensured.ID, plan.ID)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if strings.Contains(logText, "-m venv "+plan.EnvDir) {
		t.Fatalf("existing venv should not be recreated, got:\n%s", logText)
	}
	if !strings.Contains(logText, "-m ensurepip --upgrade") {
		t.Fatalf("expected existing venv pip repair, got:\n%s", logText)
	}
	if !strings.Contains(logText, plan.PythonPath+" -m pip install --quiet pymupdf python-docx") {
		t.Fatalf("expected package install in existing venv, got:\n%s", logText)
	}
}

func TestEnsureSharedPythonRuntimePipFallbackReportsPackageInstallFailure(t *testing.T) {
	dataDir := t.TempDir()
	spec := SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}}
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", filepath.Join(t.TempDir(), "pip-install-failure.log"))
	t.Setenv("MACLAW_FAKE_PIP_FAIL_CONTAINS", "-m pip install")
	oldResolveUV := sharedRuntimeResolveUVExecutable
	sharedRuntimeResolveUVExecutable = func() (string, error) {
		return "", fmt.Errorf("uv not found")
	}
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() {
		sharedRuntimeResolveUVExecutable = oldResolveUV
		sharedRuntimeExecCommand = oldExec
	})

	if _, err := EnsureSharedPythonRuntimeWithDataDir(dataDir, spec, "pdf-word", nil); err == nil {
		t.Fatal("expected package install failure")
	}
	lockData, err := os.ReadFile(plan.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock sharedPythonRuntimeLock
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Status != "failed" || lock.Stage != "pip_install" || !strings.Contains(lock.Error, "fake pip failure") {
		t.Fatalf("lock = %#v, want pip_install failure", lock)
	}
}

func TestEnsureSharedPythonRuntimePipFallbackRejectsWrongPythonMinor(t *testing.T) {
	dataDir := t.TempDir()
	spec := SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}}
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		t.Fatal(err)
	}
	oldResolveUV := sharedRuntimeResolveUVExecutable
	sharedRuntimeResolveUVExecutable = func() (string, error) {
		return "", fmt.Errorf("uv not found")
	}
	oldResolveSeed := sharedRuntimeResolveSeedPython
	sharedRuntimeResolveSeedPython = func() (string, error) {
		return "python", nil
	}
	oldCheckPython := sharedRuntimeCheckPython
	sharedRuntimeCheckPython = func(string) (string, bool) {
		return "Python 3.11.9", true
	}
	t.Cleanup(func() {
		sharedRuntimeResolveUVExecutable = oldResolveUV
		sharedRuntimeResolveSeedPython = oldResolveSeed
		sharedRuntimeCheckPython = oldCheckPython
	})

	_, err = EnsureSharedPythonRuntimeWithDataDir(dataDir, spec, "pdf-word", nil)
	if err == nil || !strings.Contains(err.Error(), "requires Python "+plan.Python) {
		t.Fatalf("error = %v, want Python minor mismatch", err)
	}
	lockData, readErr := os.ReadFile(plan.LockPath)
	if readErr != nil {
		t.Fatalf("failed lock should be written for mismatched Python: %v", readErr)
	}
	var lock sharedPythonRuntimeLock
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Status != "failed" || lock.Stage != "validate_seed_python" || len(lock.UsedBy) != 1 || lock.UsedBy[0] != "pdf-word" || !strings.Contains(lock.Error, "requires Python "+plan.Python) {
		t.Fatalf("lock = %#v, want failed validate_seed_python with mismatch error", lock)
	}
	if sharedPythonRuntimeReady(plan) {
		t.Fatal("failed mismatch lock must not be treated as ready")
	}
}

func TestSharedPythonRuntimeReadyTrustsLockStatus(t *testing.T) {
	dataDir := t.TempDir()
	plan, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRuntimeLock(plan, "pdf-word", "ready"); err != nil {
		t.Fatal(err)
	}

	// When lock status="ready" and PythonPath exists, sharedPythonRuntimeReady
	// trusts the lock without spawning a subprocess to verify pip. This avoids
	// 200-500ms latency per skill run. Pip availability was ensured during the
	// original EnsureSharedPythonRuntime call.
	if !sharedPythonRuntimeReady(plan) {
		t.Fatal("ready lock with existing PythonPath must be treated as ready (trust lock)")
	}
}

func TestEnsureSharedPythonRuntimeSkipsReInstallWhenLockReady(t *testing.T) {
	dataDir := t.TempDir()
	spec := SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}}
	plan, err := PlanSharedPythonRuntime(dataDir, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRuntimeLock(plan, "pdf-word", "ready"); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakeUV := filepath.Join(fakeBin, "uv")
	if runtime.GOOS == "windows" {
		fakeUV += ".exe"
	}
	if err := os.WriteFile(fakeUV, []byte("fake uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	logPath := filepath.Join(t.TempDir(), "repair-pip.log")
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", logPath)
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() { sharedRuntimeExecCommand = oldExec })

	// With lock status="ready" and PythonPath existing, EnsureSharedPythonRuntime
	// trusts the lock and short-circuits — no ensurepip or pip install is called.
	if _, err := EnsureSharedPythonRuntimeWithDataDir(dataDir, spec, "pdf-word", nil); err != nil {
		t.Fatal(err)
	}
	// Verify no subprocess commands were run (log file should not exist or be empty)
	if data, err := os.ReadFile(logPath); err == nil && len(data) > 0 {
		t.Fatalf("expected no commands when lock is ready, got:\n%s", string(data))
	}
}

func TestEnsureSharedPythonRuntimePipErrorAvoidsNilUVFailure(t *testing.T) {
	dataDir := t.TempDir()
	plan, err := PlanSharedPythonRuntime(dataDir, SharedPythonRuntimeSpec{Packages: []string{"pymupdf"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.PythonPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.PythonPath, []byte("python"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_FAKE_PIP_RUNTIME_LOG", filepath.Join(t.TempDir(), "pip-error.log"))
	t.Setenv("MACLAW_FAKE_PIP_FAIL_CONTAINS", "-m pip --version|-m ensurepip --upgrade")
	oldExec := sharedRuntimeExecCommand
	sharedRuntimeExecCommand = func(name string, args ...string) *exec.Cmd {
		helperArgs := append([]string{"-test.run=TestSharedPythonRuntimeFakePip", "--", name}, args...)
		cmd := exec.Command(os.Args[0], helperArgs...)
		cmd.Env = os.Environ()
		return cmd
	}
	t.Cleanup(func() { sharedRuntimeExecCommand = oldExec })

	err = ensureSharedPythonRuntimePip(plan, "uv")
	if err == nil {
		t.Fatal("expected pip repair failure")
	}
	if strings.Contains(err.Error(), "<nil>") || !strings.Contains(err.Error(), "uv pip install") {
		t.Fatalf("error = %q, want clear uv repair failure without nil", err)
	}
}

func TestLockSharedPythonRuntimeSerializesSameRuntimeID(t *testing.T) {
	var active int32
	var maxActive int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			unlock := lockSharedPythonRuntime("same-runtime")
			defer unlock()
			current := atomic.AddInt32(&active, 1)
			for {
				previous := atomic.LoadInt32(&maxActive)
				if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&active, -1)
		}()
	}
	close(start)
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("same runtime lock allowed %d concurrent holders, want 1", maxActive)
	}
}

func TestShouldFallbackToNextSourceForHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "http 400", err: fmt.Errorf("下载失败: HTTP 400 (https://cnb.cool/example.zip)"), want: true},
		{name: "http 429", err: fmt.Errorf("download failed: HTTP 429 (https://example.test/uv.zip)"), want: true},
		{name: "http 500", err: fmt.Errorf("download failed: HTTP 500 (https://example.test/uv.zip)"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFallbackToNextSource(tc.err); got != tc.want {
				t.Fatalf("shouldFallbackToNextSource() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSharedPythonRuntimeFakeUV(t *testing.T) {
	if os.Getenv("MACLAW_FAKE_UV_LOG") == "" {
		return
	}
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		fmt.Fprintln(os.Stderr, "missing fake uv separator")
		os.Exit(2)
	}
	line := strings.Join(os.Args[sep+1:], " ") + "\n"
	if failNeedle := os.Getenv("MACLAW_FAKE_UV_FAIL_CONTAINS"); failNeedle != "" && strings.Contains(line, failNeedle) {
		fmt.Fprintf(os.Stderr, "fake uv failure for %s\n", failNeedle)
		os.Exit(1)
	}
	file, err := os.OpenFile(os.Getenv("MACLAW_FAKE_UV_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := file.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestSharedPythonRuntimeFakePip(t *testing.T) {
	if os.Getenv("MACLAW_FAKE_PIP_RUNTIME_LOG") == "" {
		return
	}
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		fmt.Fprintln(os.Stderr, "missing fake pip separator")
		os.Exit(2)
	}
	line := strings.Join(os.Args[sep+1:], " ") + "\n"
	if failNeedles := os.Getenv("MACLAW_FAKE_PIP_FAIL_CONTAINS"); failNeedles != "" {
		for _, failNeedle := range strings.Split(failNeedles, "|") {
			failNeedle = strings.TrimSpace(failNeedle)
			if failNeedle != "" && strings.Contains(line, failNeedle) {
				fmt.Fprintf(os.Stderr, "fake pip failure for %s\n", failNeedle)
				os.Exit(1)
			}
		}
	}
	if marker := os.Getenv("MACLAW_FAKE_PIP_ENSUREPIP_MARKER"); marker != "" {
		if strings.Contains(line, "-m pip ") {
			if _, err := os.Stat(marker); os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, "fake pip missing until ensurepip")
				os.Exit(1)
			}
		}
		if strings.Contains(line, "-m ensurepip --upgrade") {
			if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
	}
	file, err := os.OpenFile(os.Getenv("MACLAW_FAKE_PIP_RUNTIME_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := file.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}
