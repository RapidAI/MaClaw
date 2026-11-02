package guiapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func setRememberedToolPathsForTest(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool_paths.json")
	prev := rememberedToolPathsPathFn
	rememberedToolPathsPathFn = func() string {
		return path
	}
	t.Cleanup(func() { rememberedToolPathsPathFn = prev })
}

func TestRememberedToolPathsRoundTrip(t *testing.T) {
	setRememberedToolPathsForTest(t)

	if dir := loadRememberedToolDir("go.exe"); dir != "" {
		t.Fatalf("empty registry returned %q", dir)
	}

	home := t.TempDir()
	found := filepath.Join(home, "Go", "bin")
	if err := os.MkdirAll(found, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(found, "go.exe"), []byte("go binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	rememberToolDir("go.exe", found)
	if dir := loadRememberedToolDir("go.exe"); dir != found {
		t.Fatalf("remembered = %q, want %q", dir, found)
	}

	// A corrupt registry file behaves like an empty one: the entry is
	// rewritten and the tool — restored here — validates again.
	if err := os.WriteFile(filepath.Join(found, "go.exe"), []byte("go binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rememberedToolPathsPathFn(), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	rememberToolDir("go.exe", found)
	if dir := loadRememberedToolDir("go.exe"); dir != found {
		t.Fatalf("entry after corrupt registry = %q, want %q", dir, found)
	}
}

// A tool found once stays found: a later process start reads the registry
// even when PATH says nothing, and an uninstalled tool is dropped on read.
// The probe name is unique to this test so the machine's real logon Path can
// never answer before the registry does.
func TestDetectRememberedWindowsToolDirRemembersAcrossRestart(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	setRememberedToolPathsForTest(t)

	const probeName = "zcode-remembered-probe.exe"
	installed := t.TempDir()
	if err := os.WriteFile(filepath.Join(installed, probeName), []byte("probe binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The install is not on PATH: only the well-known-location probe finds it,
	// which is the case the registry exists for.
	t.Setenv("PATH", t.TempDir())
	slow := func() []string { return []string{filepath.Join(installed, probeName)} }

	slot, once := "", sync.Once{}
	if dir := detectRememberedWindowsToolDir(&slot, &once, probeName, "[probe-detect-test]", slow); dir != installed {
		t.Fatalf("first detect = %q, want %q", dir, installed)
	}

	// A new process: fresh slot and once, still off PATH, and the slow probe
	// would not answer. The remembered registry is what keeps it found.
	t.Setenv("PATH", t.TempDir())
	slowRan := false
	slowNext := func() []string {
		slowRan = true
		return nil
	}
	slot2, once2 := "", sync.Once{}
	if dir := detectRememberedWindowsToolDir(&slot2, &once2, probeName, "[probe-detect-test]", slowNext); dir != installed {
		t.Fatalf("restart detect = %q, want the remembered %q", dir, installed)
	}
	if slowRan {
		t.Fatal("slow probe ran although the registry already answered")
	}

	// The tool is gone: the remembered directory must not resurrect it.
	if err := os.Remove(filepath.Join(installed, probeName)); err != nil {
		t.Fatal(err)
	}
	slot3, once3 := "", sync.Once{}
	if dir := detectRememberedWindowsToolDir(&slot3, &once3, probeName, "[probe-detect-test]", func() []string { return nil }); dir != "" {
		t.Fatalf("uninstalled tool detect = %q, want empty", dir)
	}
	if dir := loadRememberedToolDir(probeName); dir != "" {
		t.Fatalf("uninstalled tool stayed remembered at %q", dir)
	}
}

// A PATH hit is situational and must not be remembered: activating a venv in
// one process would otherwise inject that venv into every later process.
func TestDetectRememberedWindowsToolDirDoesNotRememberPathHits(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	setRememberedToolPathsForTest(t)

	const probeName = "zcode-path-hit-probe.exe"
	onPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(onPath, probeName), []byte("probe binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", onPath)

	slot, once := "", sync.Once{}
	if dir := detectRememberedWindowsToolDir(&slot, &once, probeName, "[probe-detect-test]", func() []string { return nil }); dir != onPath {
		t.Fatalf("path hit detect = %q, want %q", dir, onPath)
	}
	if dir := loadRememberedToolDir(probeName); dir != "" {
		t.Fatalf("PATH hit was remembered at %q", dir)
	}
}

func TestWindowsGoCandidatesCoverWellKnownInstalls(t *testing.T) {
	t.Setenv("GOROOT", `D:\Tools\Go`)
	matches := windowsGoCandidates()
	joined := strings.Join(matches, ";")
	for _, want := range []string{
		`D:\Tools\Go\bin\go.exe`,
		`C:\Program Files\Go\bin\go.exe`,
		`C:\Go\bin\go.exe`,
	} {
		if !strings.Contains(strings.ToLower(joined), strings.ToLower(want)) {
			t.Fatalf("go candidates missing %s:\n%s", want, joined)
		}
	}
}

// The command environment puts the probed toolchain directories in front of
// PATH, so a bare `go build` runs on a machine whose PATH lacks the install.
func TestCodingCommandEnvPrependsProbedGo(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	setRememberedToolPathsForTest(t)

	goBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(goBin, "go.exe"), []byte("go binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	windowsToolDirMu.Lock()
	origGoBin, origGoOnce := windowsGoBin, windowsGoBinOnce
	origNodeBin, origNodeOnce := windowsNodeBin, windowsNodeBinOnce
	origPythonBin, origPythonOnce := windowsPythonBin, windowsPythonBinOnce
	windowsGoBin, windowsGoBinOnce = goBin, sync.Once{}
	windowsNodeBin, windowsNodeBinOnce = "", sync.Once{}
	windowsPythonBin, windowsPythonBinOnce = "", sync.Once{}
	windowsToolDirMu.Unlock()
	t.Cleanup(func() {
		windowsToolDirMu.Lock()
		windowsGoBin, windowsGoBinOnce = origGoBin, origGoOnce
		windowsNodeBin, windowsNodeBinOnce = origNodeBin, origNodeOnce
		windowsPythonBin, windowsPythonBinOnce = origPythonBin, origPythonOnce
		windowsToolDirMu.Unlock()
	})

	env := codingCommandEnv()
	pathValue := ""
	for _, item := range env {
		if strings.HasPrefix(strings.ToLower(item), "path=") {
			pathValue = item[len("Path="):]
			break
		}
	}
	if pathValue == "" {
		t.Fatal("command env has no PATH")
	}
	// The MSVC merge legitimately puts its own entries at the very front, so
	// assert membership: the seeded go dir must be in PATH even though the
	// process PATH (C:\Windows and below) does not contain it.
	parts := strings.Split(pathValue, string(os.PathListSeparator))
	found := false
	for _, part := range parts {
		if strings.EqualFold(strings.TrimSpace(part), goBin) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("probed go dir missing from command PATH:\n%q", parts)
	}
}

// End-to-end: the reported machine had go.exe in Program Files and no go dir
// in PATH, so `where go` and `go build` failed inside the coding shell. With
// the probed directory injected, a shell built from codingCommandEnv resolves
// the bare command even though the PATH it started from never contained it.
func TestCodingCommandEnvResolvesGoWithStrippedPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	setRememberedToolPathsForTest(t)

	// Probe the way the first bash call does, then keep the result in the slot.
	windowsToolDirMu.Lock()
	origGoBin, origGoOnce := windowsGoBin, windowsGoBinOnce
	windowsGoBin, windowsGoBinOnce = "", sync.Once{}
	windowsToolDirMu.Unlock()
	t.Cleanup(func() {
		windowsToolDirMu.Lock()
		windowsGoBin, windowsGoBinOnce = origGoBin, origGoOnce
		windowsToolDirMu.Unlock()
	})
	goDir := detectWindowsGoBin()
	if goDir == "" {
		t.Skip("no go install detected on this host")
	}

	// The reported failure state: a PATH with no go directory at all.
	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	t.Setenv("PATH", filepath.Join(systemRoot, "System32"))

	env := codingCommandEnv()
	cmd := exec.Command(passthroughRuntimeProgram("cmd.exe"), "/d", "/c", "where go.exe")
	cmd.Env = env
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("where go.exe with the command env failed: %v\n%s", err, out)
	}
	first := strings.TrimSpace(strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")[0])
	if !strings.EqualFold(filepath.Dir(first), goDir) {
		t.Fatalf("where go.exe resolved %q, want a file under %q", first, goDir)
	}
}

// probedWindowsCommandDirs keeps the codingCommandEnv prepend order —
// compilers first, then the language toolchains — because cl.exe must stay
// ahead of any MinGW link.exe.
func TestProbedWindowsCommandDirsOrder(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	setRememberedToolPathsForTest(t)

	mingw, goBin := t.TempDir(), t.TempDir()
	for name, body := range map[string][]byte{"g++.exe": []byte("g++"), "go.exe": []byte("go")} {
		if err := os.WriteFile(filepath.Join(mingw, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(goBin, "go.exe"), []byte("go binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	windowsToolDirMu.Lock()
	origMinGW, origMinGWOnce := windowsMinGWBin, windowsMinGWBinOnce
	origGoBin, origGoOnce := windowsGoBin, windowsGoBinOnce
	origNodeBin, origNodeOnce := windowsNodeBin, windowsNodeBinOnce
	origPythonBin, origPythonOnce := windowsPythonBin, windowsPythonBinOnce
	windowsMinGWBin, windowsMinGWBinOnce = mingw, sync.Once{}
	windowsGoBin, windowsGoBinOnce = goBin, sync.Once{}
	windowsNodeBin, windowsNodeBinOnce = "", sync.Once{}
	windowsPythonBin, windowsPythonBinOnce = "", sync.Once{}
	windowsToolDirMu.Unlock()
	t.Cleanup(func() {
		windowsToolDirMu.Lock()
		windowsMinGWBin, windowsMinGWBinOnce = origMinGW, origMinGWOnce
		windowsGoBin, windowsGoBinOnce = origGoBin, origGoOnce
		windowsNodeBin, windowsNodeBinOnce = origNodeBin, origNodeOnce
		windowsPythonBin, windowsPythonBinOnce = origPythonBin, origPythonOnce
		windowsToolDirMu.Unlock()
	})

	dirs := probedWindowsCommandDirs()
	minGWAt, goAt := -1, -1
	for i, dir := range dirs {
		if strings.EqualFold(dir, mingw) {
			minGWAt = i
		}
		if strings.EqualFold(dir, goBin) {
			goAt = i
		}
	}
	if minGWAt < 0 || goAt < 0 {
		t.Fatalf("probed dirs missing seeded entries: %q", dirs)
	}
	if minGWAt > goAt {
		t.Fatalf("compiler dir %q is behind the toolchain dir %q in %q", mingw, goBin, dirs)
	}
}

// The startup registration routes the same repair into every corelib shell:
// a background task on a machine whose PATH lacks the Go install resolves it.
func TestInstallCorelibCommandEnvAugmenter(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	setRememberedToolPathsForTest(t)

	goBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(goBin, "go.exe"), []byte("go binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	windowsToolDirMu.Lock()
	origGoBin, origGoOnce := windowsGoBin, windowsGoBinOnce
	windowsGoBin, windowsGoBinOnce = goBin, sync.Once{}
	windowsToolDirMu.Unlock()
	t.Cleanup(func() {
		windowsToolDirMu.Lock()
		windowsGoBin, windowsGoBinOnce = origGoBin, origGoOnce
		windowsToolDirMu.Unlock()
		coretool.SetCommandEnvAugmenter(nil)
	})

	installCorelibCommandEnvAugmenter()
	env := coretool.AugmentCommandEnv([]string{`Path=C:\Windows\System32`})
	pathValue := ""
	for _, item := range env {
		if strings.HasPrefix(strings.ToLower(item), "path=") {
			pathValue = item[len("Path="):]
			break
		}
	}
	if pathValue == "" {
		t.Fatal("augmented env has no PATH")
	}
	if !strings.Contains(strings.ToLower(pathValue), strings.ToLower(goBin)) {
		t.Fatalf("background-task env does not contain the probed go dir: %q", pathValue)
	}
}
