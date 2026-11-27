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
	onFileCompanionSecondInstance3(app, []string{"open-file", first}, "")
	onFileCompanionSecondInstance3(app, []string{"open-file", second}, "")
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
	onFileCompanionSecondInstance3(app, []string{"open-file", third}, "")
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
	onFileCompanionSecondInstance3(app, []string{"open-file", first}, "")
	boot := app.GetFileCompanionBoot()
	if len(boot.Paths) != 1 || boot.Paths[0] != first {
		t.Fatalf("boot paths = %#v", boot.Paths)
	}
	onFileCompanionSecondInstance3(app, []string{"open-file", second}, "")
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
	onFileCompanionSecondInstance3(app, []string{"open-file", first}, "")
	if shown != 1 {
		t.Fatalf("shown = %d", shown)
	}
	if len(app.fileCompanion.emitted) != 1 || len(app.fileCompanion.emitted[0]) != 1 || app.fileCompanion.emitted[0][0] != first {
		t.Fatalf("reopen emit = %#v", app.fileCompanion.emitted)
	}
	onFileCompanionSecondInstance3(app, []string{"open-file"}, "")
	if shown != 2 {
		t.Fatalf("empty reveal shown = %d", shown)
	}
	if len(app.fileCompanion.emitted) != 1 {
		t.Fatalf("empty open-file emitted = %#v", app.fileCompanion.emitted)
	}
}

func TestFileCompanionSecondInstanceOpensDocumentsOnly(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	file := filepath.Join(dir, "paper.pdf")
	named := filepath.Join(dir, "open-file")
	if err := os.WriteFile(named, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.seedFileCompanionPaths([]string{file, named})
	boot := app.GetFileCompanionBoot()
	if len(boot.Paths) != 2 || boot.Paths[0] != file || boot.Paths[1] != named {
		t.Fatalf("boot paths = %#v", boot.Paths)
	}
	app.FileCompanionUIReady()
	pdf := filepath.Join(dir, "next.pdf")
	onFileCompanionSecondInstance3(app, []string{"open-file", pdf, named}, "")
	got := app.fileCompanion.emitted
	if len(got) != 1 || len(got[0]) != 2 || got[0][0] != pdf || got[0][1] != named {
		t.Fatalf("emit = %#v", got)
	}
}

func TestFileCompanionSecondInstanceResolvesRelativeDocumentsInTheLaunchDirectory(t *testing.T) {
	app := &App{}
	app.FileCompanionUIReady()
	dir := t.TempDir()
	named := filepath.Join(dir, "open-file")
	onFileCompanionSecondInstance3(app, []string{"open-file", "notes.md", named}, dir)
	want, err := filepath.Abs(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := app.fileCompanion.emitted
	if len(got) != 1 || len(got[0]) != 2 || got[0][0] != want || got[0][1] != named {
		t.Fatalf("emit = %#v, notes %q", got, want)
	}
}

func TestFileCompanionDocumentArgFreezesPlainRelativesOnly(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.Abs(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := fileCompanionDocumentArg("notes.md", dir)
	if !ok || got != want {
		t.Fatalf("plain relative = %q ok %v, want %q", got, ok, want)
	}
	parent, err := filepath.Abs(filepath.Join(dir, "..", "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok = fileCompanionDocumentArg(filepath.Join("..", "notes.md"), dir)
	if !ok || got != parent {
		t.Fatalf("parent relative = %q ok %v, want %q", got, ok, parent)
	}
	absPath := filepath.Join(dir, "open-file")
	slash := filepath.ToSlash(absPath)
	got, ok = fileCompanionDocumentArg(slash, filepath.Join(dir, "other"))
	if !ok || got != slash {
		t.Fatalf("absolute = %q ok %v", got, ok)
	}
	got, ok = fileCompanionDocumentArg("notes.md", "relative-dir")
	if !ok || got != "notes.md" {
		t.Fatalf("non-absolute dir = %q ok %v", got, ok)
	}
	if _, ok = fileCompanionDocumentArg("   ", dir); ok {
		t.Fatal("blank path was a document")
	}
	if _, ok = fileCompanionDocumentArg("notes\x00.md", dir); ok {
		t.Fatal("NUL path was a document")
	}
	if goruntime.GOOS != "windows" {
		return
	}
	vol := filepath.VolumeName(dir)
	got, ok = fileCompanionDocumentArg(vol+"notes.md", dir)
	if !ok || got != want {
		t.Fatalf("same-volume drive relative = %q ok %v, want %q", got, ok, want)
	}
	other := "D:"
	if strings.EqualFold(vol, other) {
		other = "C:"
	}
	driveRel := other + "notes.md"
	got, ok = fileCompanionDocumentArg(driveRel, dir)
	if !ok || got != driveRel {
		t.Fatalf("other-volume drive relative = %q ok %v", got, ok)
	}
	got, ok = fileCompanionDocumentArg(`\notes.md`, dir)
	if !ok || got != `\notes.md` {
		t.Fatalf("rooted path = %q ok %v", got, ok)
	}
	unchanged := vol + `/Papers/open-file`
	got, ok = fileCompanionDocumentArg(unchanged, dir)
	if !ok || got != unchanged {
		t.Fatalf("absolute slash path = %q ok %v", got, ok)
	}
}

func TestForwardFilesToCompanionResolvesDocumentsBeforeInstallCwd(t *testing.T) {
	app := &App{}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	named := filepath.Join(dir, "open-file")
	if err := os.WriteFile(named, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := app.LaunchFileCompanion([]string{"notes.md", named}); err != nil {
		t.Fatal(err)
	}
	absNotes, err := filepath.Abs("notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 4 || argv[1] != fileCompanionOpenArg || argv[2] != absNotes || argv[3] != named {
		t.Fatalf("argv = %#v, notes %q", argv, absNotes)
	}
	launch := classifyDesktopLaunchForOS(argv, "windows")
	if launch.Mode != desktopLaunchFileCompanion || len(launch.Paths) != 2 || launch.Paths[0] != absNotes || launch.Paths[1] != named {
		t.Fatalf("child launch = %#v", launch)
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
	t.Cleanup(app.stopFileCompanionLanguageWatcher)
	t.Cleanup(app.stopFileCompanionNotificationWatch)
	before := environmentCheckEntries.Load()
	app.fileCompanionStartup(context.Background())
	if !fileCompanionNotificationWatchRunning() {
		t.Fatal("companion startup did not start the notification watch")
	}
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
	app := &App{testHomeDir: t.TempDir()}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	// Nothing is remembered. The companion still starts, and the window shows
	// its own open panel. open-file stays the mode token, not a document.
	asked := false
	app.processHooks.chooseFiles = func() ([]string, error) {
		asked = true
		return nil, nil
	}
	if err := app.ShowFileCompanion(); err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Fatal("empty history opened the system file dialog")
	}
	if len(argv) != 2 || argv[1] != "open-file" {
		t.Fatalf("argv = %#v", argv)
	}
}

func TestLaunchFileCompanionFromCompanionProcessDoesNotSpawn(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	app.FileCompanionUIReady()
	spawned := false
	app.processHooks.startProcess = func(got []string) error {
		spawned = true
		return nil
	}
	shown := 0
	app.processHooks.showWindow = func() { shown++ }
	file := filepath.Join(t.TempDir(), "shared.pdf")
	if err := app.LaunchFileCompanion([]string{file}); err != nil {
		t.Fatal(err)
	}
	if spawned {
		t.Fatal("companion process started another companion")
	}
	if shown != 1 {
		t.Fatalf("shown = %d", shown)
	}
	emitted := app.fileCompanion.emitted
	if len(emitted) != 1 || len(emitted[0]) != 1 || emitted[0][0] != file {
		t.Fatalf("emitted = %#v", emitted)
	}
}

func TestFileCompanionSecondInstanceQuitRequestsQuit(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	app.FileCompanionUIReady()
	var quits int
	app.processHooks.requestQuit = func(context.Context) { quits++ }
	var shown int
	app.processHooks.showWindow = func() { shown++ }

	onFileCompanionSecondInstance3(app, []string{"open-file", "--quit"}, "")

	if quits != 1 {
		t.Fatalf("quit requests = %d, want 1", quits)
	}
	if shown != 0 {
		t.Fatal("quit relay revealed the companion window")
	}
	if len(app.fileCompanion.emitted) != 0 {
		t.Fatalf("quit relay emitted document paths: %#v", app.fileCompanion.emitted)
	}
}

func TestSignalFileCompanionQuitSpawnsOpenFileQuitOnce(t *testing.T) {
	app := &App{}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	app.signalFileCompanionQuit()
	if len(argv) != 3 || argv[1] != "open-file" || argv[2] != "--quit" {
		t.Fatalf("relay argv = %#v", argv)
	}
	// The relay fires once per process even when OnShutdown repeats it.
	argv = nil
	app.signalFileCompanionQuit()
	if argv != nil {
		t.Fatal("quit relay spawned a second process")
	}
}

func TestSignalFileCompanionQuitSkipsTheCompanionItself(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	spawned := false
	app.processHooks.startProcess = func([]string) error {
		spawned = true
		return nil
	}
	app.signalFileCompanionQuit()
	if spawned {
		t.Fatal("companion relayed a quit to itself")
	}
}

func TestFileCompanionStartupQuitRelayExitsWithoutWindow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionQuit()
	var quits int
	app.processHooks.requestQuit = func(context.Context) { quits++ }
	app.fileCompanionStartup(context.Background())
	if quits != 1 {
		t.Fatalf("quit requests = %d, want 1", quits)
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

func TestFileCompanionLibraryLaunchBootsToLibraryView(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionLibrary()
	boot := app.GetFileCompanionBoot()
	if !boot.Library {
		t.Fatalf("--library boot = %#v", boot)
	}
	if len(boot.Paths) != 0 {
		t.Fatalf("--library boot carried documents %#v", boot.Paths)
	}
	if app.FileCompanionTakeLibraryView() {
		t.Fatal("take drained a flag the boot read already consumed")
	}
}

func TestFileCompanionSecondInstanceLibraryEmitsViewEvent(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	app.FileCompanionUIReady()
	var events []string
	app.testEmit = func(name string, data ...interface{}) { events = append(events, name) }
	var shown int
	app.processHooks.showWindow = func() { shown++ }

	onFileCompanionSecondInstance3(app, []string{"open-file", "--library"}, "")

	found := false
	for _, name := range events {
		if name == "file-companion:view" {
			found = true
		}
	}
	if !found {
		t.Fatalf("events %v, want file-companion:view", events)
	}
	if shown != 1 {
		t.Fatalf("shown = %d, want 1", shown)
	}
	if len(app.fileCompanion.emitted) != 0 {
		t.Fatalf("library launch emitted document paths: %#v", app.fileCompanion.emitted)
	}
}

func TestFileCompanionTakeLibraryViewDrainsPostBootRequest(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	boot := app.GetFileCompanionBoot()
	if boot.Library {
		t.Fatal("a plain open-file boot asked for the library view")
	}
	// A --library that lands after the boot read but before the window is
	// ready must not emit an event the window has not subscribed to.
	onFileCompanionSecondInstance3(app, []string{"open-file", "--library"}, "")
	if !app.FileCompanionTakeLibraryView() {
		t.Fatal("take did not see the post-boot library request")
	}
	if app.FileCompanionTakeLibraryView() {
		t.Fatal("take returned a drained library request again")
	}
}

func TestShowFileCompanionLibrarySpawnsOpenFileLibrary(t *testing.T) {
	app := &App{}
	var argv []string
	app.processHooks.startProcess = func(got []string) error {
		argv = append([]string(nil), got...)
		return nil
	}
	if err := app.ShowFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	if len(argv) != 3 || argv[1] != "open-file" || argv[2] != "--library" {
		t.Fatalf("library argv = %#v", argv)
	}
}

func TestShowFileCompanionLibraryInsideCompanionEmitsView(t *testing.T) {
	app := &App{}
	app.seedFileCompanionPaths(nil)
	app.FileCompanionUIReady()
	spawned := false
	app.processHooks.startProcess = func([]string) error {
		spawned = true
		return nil
	}
	var events []string
	app.testEmit = func(name string, data ...interface{}) { events = append(events, name) }
	var shown int
	app.processHooks.showWindow = func() { shown++ }

	if err := app.ShowFileCompanionLibrary(); err != nil {
		t.Fatal(err)
	}
	if spawned {
		t.Fatal("companion process started another companion")
	}
	found := false
	for _, name := range events {
		if name == "file-companion:view" {
			found = true
		}
	}
	if !found {
		t.Fatalf("events %v, want file-companion:view", events)
	}
	if shown != 1 {
		t.Fatalf("shown = %d, want 1", shown)
	}
}
