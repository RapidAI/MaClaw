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

	items, err := ListSharedPythonRuntimes(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]SharedPythonRuntimeStatus{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if got := byID[ready.ID]; got.Status != "ready" || !got.HasPython || !got.HasLock || len(got.UsedBy) != 1 || got.UsedBy[0] != "ready-skill" {
		t.Fatalf("ready runtime status = %#v", got)
	}
	if got := byID[missing.ID]; got.Status != "missing_python" || got.HasPython || !got.HasLock {
		t.Fatalf("missing runtime status = %#v", got)
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

func TestEnsureSharedPythonRuntimeLeavesInstallingLockOnFailure(t *testing.T) {
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
	if lock.Status != "installing" || len(lock.UsedBy) != 1 || lock.UsedBy[0] != "ocr-app" {
		t.Fatalf("failed install should leave installing lock with used_by: %#v", lock)
	}
	if sharedPythonRuntimeReady(plan) {
		t.Fatal("installing lock must not be treated as ready")
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
