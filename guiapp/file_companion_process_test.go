package guiapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func TestDarwinLaunchBatchEmptyStillRunsEnvironmentCheck(t *testing.T) {
	// CheckEnvironment writes its default config on a goroutine after the
	// synchronous entry counter. t.TempDir cleanup races that write on Windows
	// and fails the test. This directory is removed after the write settles.
	home, err := os.MkdirTemp("", "maclaw-file-companion-envcheck-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if removeErr := os.RemoveAll(home); removeErr == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Logf("env-check home cleanup left %s", home)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	app := &App{testHomeDir: home}
	app.processHooks.runMainStartup = func(*App) {}
	var shown int
	app.processHooks.showWindow = func() { shown++ }
	var started [][]string
	app.processHooks.startProcess = func(argv []string) error {
		started = append(started, append([]string(nil), argv...))
		return nil
	}
	var exited []int
	app.processHooks.exitProcess = func(code int) { exited = append(exited, code) }

	app.armDarwinDocumentLatch()
	// An empty host queue is not a commit. didFinishLaunching only releases
	// the wait; the batch hook is what decides.
	if app.fileCompanion.startupRequested || shown != 0 || len(started) != 0 {
		t.Fatal("empty host queue committed the main window")
	}
	app.frontendHTMLReady.Store(true)
	before := environmentCheckEntries.Load()
	app.darwinOnLaunchFileBatch(nil)
	if !app.fileCompanion.startupRequested {
		t.Fatal("empty launch batch did not request main startup")
	}
	if shown != 1 {
		t.Fatalf("main window shown %d times, want 1", shown)
	}
	if got := environmentCheckEntries.Load() - before; got != 1 {
		t.Fatalf("CheckEnvironment entries = %d, want 1", got)
	}
	if len(started) != 0 || len(exited) != 0 {
		t.Fatalf("empty batch forwarded=%v exited=%v", started, exited)
	}
	// Replaying the hook after the latch closes must not check again.
	app.darwinOnLaunchFileBatch(nil)
	if got := environmentCheckEntries.Load() - before; got != 1 {
		t.Fatalf("second empty batch ran CheckEnvironment again: %d", got)
	}
	cfg := filepath.Join(home, ".maclaw", "config.json")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, statErr := os.Stat(cfg)
		if statErr == nil && info.Size() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDarwinLaunchBatchForwardsBufferedPathsTogether(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.processHooks.runMainStartup = func(*App) { t.Fatal("document batch started the main app") }
	app.processHooks.showWindow = func() { t.Fatal("document batch showed the main window") }
	var started [][]string
	app.processHooks.startProcess = func(argv []string) error {
		started = append(started, append([]string(nil), argv...))
		return nil
	}
	var exited []int
	app.processHooks.exitProcess = func(code int) { exited = append(exited, code) }

	dir := t.TempDir()
	first := filepath.Join(dir, "a.md")
	second := filepath.Join(dir, "b.md")
	app.armDarwinDocumentLatch()
	// OnFileOpen while the latch is open only records. It must not exit on
	// the first path, or the sibling would be dropped.
	app.darwinOnFileOpen(first)
	app.darwinOnFileOpen(second)
	if len(exited) != 0 || len(started) != 0 {
		t.Fatalf("OnFileOpen during latch exited=%v started=%v", exited, started)
	}
	notes := append([]string(nil), app.fileCompanion.latchNotes...)
	if len(notes) != 2 || notes[0] != first || notes[1] != second {
		t.Fatalf("latch notes = %#v", notes)
	}
	before := environmentCheckEntries.Load()
	app.darwinOnLaunchFileBatch([]string{first, second})
	if len(started) != 1 {
		t.Fatalf("exec count = %d, want 1: %#v", len(started), started)
	}
	argv := started[0]
	if len(argv) != 4 || argv[1] != "open-file" || argv[2] != first || argv[3] != second {
		t.Fatalf("forward argv = %#v", argv)
	}
	if len(exited) != 1 || exited[0] != 0 {
		t.Fatalf("exit = %#v", exited)
	}
	if app.fileCompanion.windowShown {
		t.Fatal("main window was shown for a document batch")
	}
	if environmentCheckEntries.Load() != before {
		t.Fatal("document batch ran CheckEnvironment")
	}
}

func TestFileCompanionBootQueuesOverlappingOpensBeforeUIReady(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	first := filepath.Join(dir, "one.txt")
	second := filepath.Join(dir, "two.txt")
	third := filepath.Join(dir, "three.txt")
	app.ctx = nil
	app.onFileCompanionSecondInstance(options.SecondInstanceData{Args: []string{"open-file", first}})
	app.onFileCompanionSecondInstance(options.SecondInstanceData{Args: []string{"open-file", second}})
	boot := app.GetFileCompanionBoot()
	if boot.Mode != desktopLaunchFileCompanion {
		t.Fatalf("mode = %q", boot.Mode)
	}
	if len(boot.Paths) != 2 || boot.Paths[0] != first || boot.Paths[1] != second {
		t.Fatalf("boot paths = %#v", boot.Paths)
	}
	app.FileCompanionUIReady()
	if len(app.fileCompanion.emitted) != 0 {
		t.Fatalf("boot paths were emitted again: %#v", app.fileCompanion.emitted)
	}
	app.onFileCompanionSecondInstance(options.SecondInstanceData{Args: []string{"open-file", third}})
	if len(app.fileCompanion.emitted) != 1 || len(app.fileCompanion.emitted[0]) != 1 || app.fileCompanion.emitted[0][0] != third {
		t.Fatalf("post-ready emit = %#v", app.fileCompanion.emitted)
	}
	again := app.GetFileCompanionBoot()
	if len(again.Paths) != 0 {
		t.Fatalf("second boot repeated paths: %#v", again.Paths)
	}
}

func TestFileCompanionUIReadyEmitsPathsQueuedAfterBoot(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	first := filepath.Join(dir, "one.txt")
	second := filepath.Join(dir, "two.txt")
	app.ctx = nil
	app.onFileCompanionSecondInstance(options.SecondInstanceData{Args: []string{"open-file", first}})
	boot := app.GetFileCompanionBoot()
	if len(boot.Paths) != 1 || boot.Paths[0] != first {
		t.Fatalf("boot paths = %#v", boot.Paths)
	}
	app.onFileCompanionSecondInstance(options.SecondInstanceData{Args: []string{"open-file", second}})
	if len(app.fileCompanion.emitted) != 0 {
		t.Fatalf("path queued before ready was emitted early: %#v", app.fileCompanion.emitted)
	}
	got := app.FileCompanionUIReady()
	if len(got) != 1 || got[0] != second {
		t.Fatalf("ready paths = %#v", got)
	}
	if len(app.fileCompanion.emitted) != 0 {
		t.Fatalf("ready paths were emitted: %#v", app.fileCompanion.emitted)
	}
	if len(app.fileCompanion.pending) != 0 {
		t.Fatalf("pending after ready = %#v", app.fileCompanion.pending)
	}
	if again := app.FileCompanionUIReady(); len(again) != 0 {
		t.Fatalf("second ready = %#v", again)
	}
}

func TestFileCompanionSecondInstanceRevealsAndReopens(t *testing.T) {
	app := &App{}
	var shown int
	app.processHooks.showWindow = func() { shown++ }
	dir := t.TempDir()
	first := filepath.Join(dir, "one.txt")
	app.seedFileCompanionPaths([]string{first})
	app.FileCompanionUIReady()
	app.onFileCompanionSecondInstance(options.SecondInstanceData{Args: []string{"open-file", first}})
	if shown != 1 {
		t.Fatalf("shown = %d", shown)
	}
	if len(app.fileCompanion.emitted) != 1 || len(app.fileCompanion.emitted[0]) != 1 || app.fileCompanion.emitted[0][0] != first {
		t.Fatalf("reopen emit = %#v", app.fileCompanion.emitted)
	}
	app.onFileCompanionSecondInstance(options.SecondInstanceData{Args: []string{"open-file"}})
	if shown != 2 {
		t.Fatalf("empty reveal shown = %d", shown)
	}
	if len(app.fileCompanion.emitted) != 1 {
		t.Fatalf("empty open-file emitted = %#v", app.fileCompanion.emitted)
	}
}

func TestFileCompanionCanonicalDedupeInBootQueue(t *testing.T) {
	if goruntime.GOOS != "windows" {
		t.Skip("full-path case fold is Windows-only")
	}
	app := &App{}
	app.seedFileCompanionPaths([]string{`D:\Notes\A.md`})
	app.enqueueFileCompanionPaths([]string{`d:\notes\a.md`})
	boot := app.GetFileCompanionBoot()
	if len(boot.Paths) != 1 || boot.Paths[0] != `D:\Notes\A.md` {
		t.Fatalf("boot paths = %#v", boot.Paths)
	}
}

func TestFileCompanionStartupDoesNotOpenMainTranscriptOrEnvCheck(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	before := environmentCheckEntries.Load()
	app.fileCompanionStartup(context.Background())
	if environmentCheckEntries.Load() != before {
		t.Fatal("companion startup called CheckEnvironment")
	}
	conversation := filepath.Join(app.GetDataDir(), "ai_assistant_conversation.json")
	if _, err := os.Stat(conversation); !os.IsNotExist(err) {
		t.Fatalf("main transcript stat err=%v", err)
	}
	if app.imHandler != nil {
		t.Fatal("companion startup constructed the main IM handler")
	}
	if app.mcpRegistry != nil {
		t.Fatal("companion startup started MCP")
	}
	if app.remoteSessions == nil || app.remoteSessions.GetHubClient() == nil {
		t.Fatal("companion startup did not prepare a Hub client")
	}
	if app.remoteSessions.GetHubClient().currentIMHandler() != nil {
		t.Fatal("Hub client constructed an IM handler during companion startup")
	}
	app.configSnap.Store(&corelib.AppConfig{Language: "zh-Hans"})
	app.noteFileCompanionConfigChanged()
	if app.configSnap.Load() != nil {
		t.Fatal("config watch did not drop the snapshot")
	}
}

func TestApplyDesktopLaunchOptionsHidesDarwinEmptyArgv(t *testing.T) {
	app := &App{}
	opts := &options.App{
		StartHidden: false,
		OnStartup:   app.startup,
		OnDomReady:  app.domReady,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceUniqueID(),
		},
		Mac:     &mac.Options{},
		Windows: &windows.Options{},
	}
	launch := classifyDesktopLaunchForOS([]string{"MaClaw"}, "darwin")
	applyDesktopLaunchOptions(app, opts, launch)
	if !opts.StartHidden {
		t.Fatal("darwin empty argv did not set StartHidden")
	}
	if opts.SingleInstanceLock.UniqueId != "maclaw-lock" {
		t.Fatalf("darwin empty argv left the main lock: %q", opts.SingleInstanceLock.UniqueId)
	}
	if opts.HideWindowOnClose {
		t.Fatal("main window close must still be allowed to exit")
	}
	if opts.Mac.OnLaunchFileBatch == nil || opts.Mac.OnFileOpen == nil {
		t.Fatal("darwin latch did not install the batch and file-open callbacks")
	}
	if opts.OnDomReady == nil {
		t.Fatal("OnDomReady was replaced")
	}
	// The latched startup must not be App.startup. Calling it records ctx only.
	opts.OnStartup(context.Background())
	if app.ctx == nil || !app.darwinDocumentLatchOpen() {
		t.Fatal("latched startup did not arm the latch")
	}
	if environmentCheckEntries.Load() != 0 && app.fileCompanion.startupRequested {
		t.Fatal("latched startup committed the main app")
	}
}

func TestApplyDesktopLaunchOptionsUsesCompanionLock(t *testing.T) {
	app := &App{}
	opts := &options.App{
		Width:  100,
		Height: 100,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceUniqueID(),
		},
		Windows: &windows.Options{WebviewUserDataPath: defaultWebviewUserDataPath()},
	}
	file := filepath.Join(t.TempDir(), "note.md")
	launch := classifyDesktopLaunchForOS([]string{"MaClaw", "open-file", file}, "windows")
	applyDesktopLaunchOptions(app, opts, launch)
	if opts.SingleInstanceLock.UniqueId != fileCompanionLockID() {
		t.Fatalf("lock = %q", opts.SingleInstanceLock.UniqueId)
	}
	if singleInstanceUniqueID() != "maclaw-lock" {
		t.Fatalf("historical lock changed: %q", singleInstanceUniqueID())
	}
	if opts.Width != 1280 || opts.Height != 800 {
		t.Fatalf("window = %dx%d", opts.Width, opts.Height)
	}
	if opts.StartHidden {
		t.Fatal("open-file used the Darwin hidden latch")
	}
	if !opts.HideWindowOnClose {
		t.Fatal("companion close must hide the window so open files stay in this process")
	}
	if goruntime.GOOS == "windows" {
		if !strings.HasSuffix(opts.Windows.WebviewUserDataPath, "MaClaw.file-companion") {
			t.Fatalf("webview profile = %q", opts.Windows.WebviewUserDataPath)
		}
	}
	boot := app.GetFileCompanionBoot()
	if boot.Mode != desktopLaunchFileCompanion || len(boot.Paths) != 1 || boot.Paths[0] != file {
		t.Fatalf("boot = %#v", boot)
	}
}

func TestStaleSingleInstanceLockIDsIncludeCompanion(t *testing.T) {
	ids := staleSingleInstanceLockIDs()
	if len(ids) != 2 || ids[0] != "maclaw-lock" || ids[1] != "maclaw-lock-file-companion" {
		t.Fatalf("stale lock ids = %#v", ids)
	}
}

func TestInjectFileCompanionBootFlag(t *testing.T) {
	got := string(injectFileCompanionBootFlag([]byte("<head><title>MaClaw</title>")))
	if !strings.Contains(got, "window.__MACLAW_FILE_COMPANION__=true") {
		t.Fatalf("injected = %s", got)
	}
	again := string(injectFileCompanionBootFlag([]byte(got)))
	if strings.Count(again, "__MACLAW_FILE_COMPANION__") != 1 {
		t.Fatalf("flag repeated: %s", again)
	}

	app := &App{}
	app.seedFileCompanionPaths(nil)
	handler := fileCompanionBootMiddleware(app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<head><title>x</title>"))
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "__MACLAW_FILE_COMPANION__") {
		t.Fatalf("middleware body = %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if strings.Contains(rec.Body.String(), "__MACLAW_FILE_COMPANION__") {
		t.Fatal("middleware rewrote a non-shell response")
	}
}

func TestCompanionRevealKeepsAMaximisedWindow(t *testing.T) {
	if companionShowCommand(false) != 5 {
		t.Fatalf("hidden show command = %d, want SW_SHOW", companionShowCommand(false))
	}
	if companionShowCommand(true) != 9 {
		t.Fatalf("minimised show command = %d, want SW_RESTORE", companionShowCommand(true))
	}
	if companionRaiseFlags()&0x4000 == 0 || companionRaiseFlags()&0x0040 == 0 {
		t.Fatalf("raise flags = %#x, want posted show", companionRaiseFlags())
	}
	if goruntime.GOOS == "windows" {
		if companionRevealDeminiaturizes() {
			t.Fatal("windows reveal must show a hidden window without SC_RESTORE")
		}
		return
	}
	if !companionRevealDeminiaturizes() {
		t.Fatal("reveal must deminiaturize before show")
	}
}

func TestShowFileCompanionLaunchesWithoutAPath(t *testing.T) {
	app := &App{}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	if err := app.ShowFileCompanion(); err != nil {
		t.Fatal(err)
	}
	if len(argv) != 2 || argv[1] != "open-file" {
		t.Fatalf("argv = %#v", argv)
	}
}

func TestLaunchFileCompanionDoesNotTouchAssistantSession(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	file := filepath.Join(t.TempDir(), "doc.md")
	if err := app.LaunchFileCompanion([]string{file}); err != nil {
		t.Fatal(err)
	}
	if len(argv) != 3 || argv[1] != "open-file" || argv[2] != file {
		t.Fatalf("argv = %#v", argv)
	}
	if app.imHandler != nil {
		t.Fatal("launch constructed an assistant handler")
	}
	if _, err := os.Stat(filepath.Join(app.GetDataDir(), "ai_assistant_conversation.json")); !os.IsNotExist(err) {
		t.Fatalf("transcript err=%v", err)
	}
}
