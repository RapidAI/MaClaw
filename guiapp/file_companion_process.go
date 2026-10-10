package guiapp

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// environmentCheckEntries counts entries into CheckEnvironment. The increment
// happens before the install goroutine so a latch test can see the call
// without waiting for Visual C++ / Node / Git setup.
var environmentCheckEntries atomic.Uint64

func noteEnvironmentCheckEntry() {
	environmentCheckEntries.Add(1)
}

// staleSingleInstanceLockIDs are the Darwin lock files removed after a crash.
// The companion id is included so a dead companion cannot make the next
// open-file call os.Exit(0) while its lock file remains.
func staleSingleInstanceLockIDs() []string {
	return []string{singleInstanceUniqueID(), fileCompanionLockID()}
}

// desktopProcessHooks replaces process boundaries in tests. Nil fields use the
// real executable start, os.Exit, WindowShow, and App.startup.
type desktopProcessHooks struct {
	startProcess   func(argv []string) error
	exitProcess    func(code int)
	showWindow     func()
	runMainStartup func(*App)
}

type fileCompanionRuntime struct {
	mu sync.Mutex

	process bool

	latchOpen        bool
	latchNotes       []string
	startupRequested bool
	windowShown      bool
	domReadyDeferred bool
	domReadyReplayed bool

	uiReady   bool
	bootTaken bool
	pending   []string
	seen      map[string]bool
	emitted   [][]string
	grants    map[string]*fileCompanionGrant
	fileMu    map[string]*sync.Mutex

	// recordEvents keeps the final companion reply for tests. Production
	// leaves it false, so the slice stays empty.
	recordEvents bool
	events       []recordedCompanionEvent

	// chatMu guards the clear generation. A turn captures chatGen when it
	// starts. /clear bumps the generation and drops a later save from that
	// turn, so the reply cannot write the cleared transcript back.
	chatMu      sync.Mutex
	chatGen     map[string]uint64
	chatDiscard map[string]bool
}

type recordedCompanionEvent struct {
	Name string
	Data string
}

func (a *App) companionRuntime() *fileCompanionRuntime {
	if a == nil {
		return nil
	}
	if a.fileCompanion == nil {
		a.fileCompanion = &fileCompanionRuntime{seen: map[string]bool{}}
	}
	if a.fileCompanion.seen == nil {
		a.fileCompanion.seen = map[string]bool{}
	}
	if a.fileCompanion.grants == nil {
		a.fileCompanion.grants = map[string]*fileCompanionGrant{}
	}
	if a.fileCompanion.fileMu == nil {
		a.fileCompanion.fileMu = map[string]*sync.Mutex{}
	}
	return a.fileCompanion
}

// fileCompanionFileLock serializes reads and writes of one open file.
// The grant object is replaced on every open, so the lock cannot live on it.
func (a *App) fileCompanionFileLock(canonical string) *sync.Mutex {
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	mu := st.fileMu[canonical]
	if mu == nil {
		mu = &sync.Mutex{}
		st.fileMu[canonical] = mu
	}
	return mu
}

// FileCompanionBoot is the first payload the companion window reads.
type FileCompanionBoot struct {
	Mode  string   `json:"mode,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

func (a *App) isFileCompanionProcess() bool {
	if a == nil || a.fileCompanion == nil {
		return false
	}
	a.fileCompanion.mu.Lock()
	defer a.fileCompanion.mu.Unlock()
	return a.fileCompanion.process
}

func (a *App) darwinDocumentLatchOpen() bool {
	if a == nil || a.fileCompanion == nil {
		return false
	}
	a.fileCompanion.mu.Lock()
	defer a.fileCompanion.mu.Unlock()
	return a.fileCompanion.latchOpen
}

func (a *App) armDarwinDocumentLatch() {
	st := a.companionRuntime()
	st.mu.Lock()
	st.latchOpen = true
	st.mu.Unlock()
}

// darwinLatchedStartup stores the Wails context and returns. It does not call
// App.startup or WindowShow: those wait for OnLaunchFileBatch, which runs
// only after applicationDidFinishLaunching.
func (a *App) darwinLatchedStartup(ctx context.Context) {
	if a == nil {
		return
	}
	a.ctx = ctx
	a.armDarwinDocumentLatch()
	logFileCompanion("darwin latch armed; main startup deferred")
}

// darwinOnFileOpen is mac.Options.OnFileOpen. While the latch is open it only
// records the path. It does not exec and it does not os.Exit, so one callback
// cannot drop the rest of the launch batch.
func (a *App) darwinOnFileOpen(path string) {
	path = strings.TrimSpace(path)
	if path == "" || a == nil {
		return
	}
	if a.darwinDocumentLatchOpen() {
		st := a.companionRuntime()
		st.mu.Lock()
		st.latchNotes = append(st.latchNotes, path)
		st.mu.Unlock()
		logFileCompanion("latch recorded open path")
		return
	}
	if a.isFileCompanionProcess() {
		a.enqueueFileCompanionPaths([]string{path})
		return
	}
	if err := a.forwardFilesToCompanion([]string{path}); err != nil {
		logFileCompanion("forward open failed: %v", err)
	}
}

// darwinOnLaunchFileBatch is mac.Options.OnLaunchFileBatch. paths is the
// drained Wails buffer. An empty slice is the no-document commit. An empty
// host-side note list is not a commit and must not be treated as one.
func (a *App) darwinOnLaunchFileBatch(paths []string) {
	if a == nil || !a.darwinDocumentLatchOpen() {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	st.latchOpen = false
	st.mu.Unlock()
	forward := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			forward = append(forward, path)
		}
	}
	if len(forward) > 0 {
		logFileCompanion("launch batch forwarding %d path(s)", len(forward))
		if err := a.forwardFilesToCompanion(forward); err != nil {
			logFileCompanion("launch batch forward failed: %v", err)
		}
		a.invokeExit(0)
		return
	}
	logFileCompanion("launch batch empty; showing main window")
	a.commitDarwinMainWindow()
}

func (a *App) commitDarwinMainWindow() {
	st := a.companionRuntime()
	st.mu.Lock()
	st.startupRequested = true
	st.mu.Unlock()
	a.invokeMainStartup()
	a.invokeShowMainWindow()
	if a.shouldReplayDarwinDomReady() {
		a.replayDeferredDomReady()
	}
}

func (a *App) deferDarwinDomReady() {
	st := a.companionRuntime()
	st.mu.Lock()
	st.domReadyDeferred = true
	st.mu.Unlock()
}

func (a *App) shouldReplayDarwinDomReady() bool {
	if a == nil {
		return false
	}
	if a.frontendHTMLReady.Load() {
		return true
	}
	st := a.companionRuntime()
	st.mu.Lock()
	deferred := st.domReadyDeferred
	st.mu.Unlock()
	return deferred
}

func (a *App) replayDeferredDomReady() {
	st := a.companionRuntime()
	st.mu.Lock()
	if st.domReadyReplayed {
		st.mu.Unlock()
		return
	}
	st.domReadyReplayed = true
	st.mu.Unlock()
	a.finishDomReady(a.ctx)
}

func (a *App) invokeMainStartup() {
	if a != nil && a.processHooks.runMainStartup != nil {
		a.processHooks.runMainStartup(a)
		return
	}
	a.startup(a.ctx)
}

func (a *App) invokeShowMainWindow() {
	st := a.companionRuntime()
	st.mu.Lock()
	st.windowShown = true
	st.mu.Unlock()
	if a.processHooks.showWindow != nil {
		a.processHooks.showWindow()
		return
	}
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowShow(a.ctx)
}

func (a *App) invokeExit(code int) {
	if a != nil && a.processHooks.exitProcess != nil {
		a.processHooks.exitProcess(code)
		return
	}
	os.Exit(code)
}

func (a *App) forwardFilesToCompanion(paths []string) error {
	if len(paths) == 0 {
		return errors.New("no file to open")
	}
	exe, err := companionExecutable()
	if err != nil {
		return err
	}
	argv := make([]string, 0, len(paths)+2)
	argv = append(argv, exe, fileCompanionOpenArg)
	argv = append(argv, paths...)
	return a.startCompanionProcess(argv)
}

func companionExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exe, nil
}

func (a *App) startCompanionProcess(argv []string) error {
	if a != nil && a.processHooks.startProcess != nil {
		return a.processHooks.startProcess(argv)
	}
	// The click is in this process. The companion process is the one that
	// shows its window, and Windows will not let it take the foreground
	// unless this foreground process allows it.
	allowCompanionForeground()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = filepath.Dir(argv[0])
	return cmd.Start()
}

// ShowFileCompanion brings the companion window forward. It does not open the
// file dialog. The window asks for a file only when it has no tab open.
func (a *App) ShowFileCompanion() error {
	if a == nil {
		return errors.New("app unavailable")
	}
	if a.isFileCompanionProcess() {
		a.revealFileCompanionWindow()
		a.notifyFileCompanionShow()
		return nil
	}
	exe, err := companionExecutable()
	if err != nil {
		return err
	}
	return a.startCompanionProcess([]string{exe, fileCompanionOpenArg})
}

// LaunchFileCompanion starts this binary in open-file mode. It does not insert
// the paths into the current assistant session.
func (a *App) LaunchFileCompanion(paths []string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || strings.ContainsRune(path, 0) {
			continue
		}
		cleaned = append(cleaned, path)
	}
	if len(cleaned) == 0 {
		return errors.New("no file selected")
	}
	return a.forwardFilesToCompanion(cleaned)
}

func (a *App) seedFileCompanionPaths(paths []string) {
	st := a.companionRuntime()
	st.mu.Lock()
	st.process = true
	st.mu.Unlock()
	a.enqueueFileCompanionPaths(paths)
}

func (a *App) enqueueFileCompanionPaths(paths []string) {
	if a == nil || len(paths) == 0 {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	var fresh []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || strings.ContainsRune(path, 0) {
			continue
		}
		key := fileCompanionDedupeKey(path)
		// Before the window subscribes, one canonical path is queued once.
		// After that, a later open of the same path is a user request to show
		// it again, so it is emitted instead of dropped.
		if !st.uiReady {
			if st.seen[key] {
				continue
			}
			st.seen[key] = true
			st.pending = append(st.pending, path)
			continue
		}
		st.seen[key] = true
		fresh = append(fresh, path)
	}
	if len(fresh) > 0 {
		copied := append([]string(nil), fresh...)
		st.emitted = append(st.emitted, copied)
		st.mu.Unlock()
		a.emitEvent("file-companion:open", copied)
		return
	}
	st.mu.Unlock()
}

func fileCompanionDedupeKey(path string) string {
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil || canonical == "" {
		return path
	}
	return canonical
}

// GetFileCompanionBoot returns argv paths plus paths queued before the
// companion UI subscribed, and removes them from the queue. Those paths are
// not emitted again. The window reads this once; a later call only sees paths
// that arrived after the previous read and have not been delivered yet.
func (a *App) GetFileCompanionBoot() FileCompanionBoot {
	boot := FileCompanionBoot{}
	if a == nil {
		return boot
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.process {
		boot.Mode = desktopLaunchFileCompanion
	}
	st.bootTaken = true
	boot.Paths = append([]string(nil), st.pending...)
	st.pending = nil
	return boot
}

// FileCompanionUIReady marks the window as subscribed and returns paths queued
// after GetFileCompanionBoot. Those paths are not emitted: the caller applies
// them before deciding the window is empty, so a later event cannot lose the
// race to the file dialog. Paths already returned by the boot read are not
// included. Pending that the window has not read yet stays queued for
// GetFileCompanionBoot. Paths that arrive after this call emit
// file-companion:open.
func (a *App) FileCompanionUIReady() []string {
	if a == nil {
		return []string{}
	}
	st := a.companionRuntime()
	st.mu.Lock()
	st.uiReady = true
	var pending []string
	if st.bootTaken {
		pending = append([]string(nil), st.pending...)
		st.pending = nil
	}
	st.mu.Unlock()
	logFileCompanion("ui ready")
	if pending == nil {
		pending = []string{}
	}
	return pending
}

func (a *App) onFileCompanionSecondInstance(data options.SecondInstanceData) {
	// Second-instance args omit argv[0]. Classify with a placeholder so
	// open-file stays the first user arg. Return immediately. The window is
	// revealed asynchronously and does not wait on ctx.
	launch := classifyDesktopLaunch(append([]string{"maclaw"}, data.Args...))
	if launch.Mode != desktopLaunchFileCompanion {
		return
	}
	logFileCompanion("second instance paths=%d", len(launch.Paths))
	a.enqueueFileCompanionPaths(launch.Paths)
	a.revealFileCompanionWindow()
	if len(launch.Paths) == 0 {
		a.notifyFileCompanionShow()
	}
}

// notifyFileCompanionShow tells the open window to ask for a file when it has
// no tab. A launch that has not subscribed yet asks from its own empty boot.
func (a *App) notifyFileCompanionShow() {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	ready := st.uiReady
	st.mu.Unlock()
	if !ready {
		return
	}
	a.emitEvent("file-companion:show", nil)
}

// revealFileCompanionWindow brings the already-running companion forward.
// A second open-file used to exit on the single-instance lock without showing
// this window, so the file dialog looked like it did nothing.
func (a *App) revealFileCompanionWindow() {
	if a == nil {
		return
	}
	if a.processHooks.showWindow != nil {
		a.processHooks.showWindow()
		return
	}
	if a.ctx == nil {
		return
	}
	ctx := a.ctx
	go func() {
		// Windows posts the show. A synchronous ShowWindow here can deadlock
		// the single-instance procedure, and WindowUnminimise sends SC_RESTORE,
		// which shrinks a maximized window.
		if !companionRevealDeminiaturizes() {
			// Posted z-order puts a hidden window above the main window.
			// WindowShow then runs on the UI thread and focuses the webview.
			// It uses SW_SHOW unless the window is minimized, so a maximized
			// window stays maximized.
			revealHiddenCompanionWindow()
			wailsruntime.WindowShow(ctx)
			return
		}
		wailsruntime.WindowUnminimise(ctx)
		wailsruntime.WindowShow(ctx)
		wailsruntime.WindowSetAlwaysOnTop(ctx, true)
		wailsruntime.WindowSetAlwaysOnTop(ctx, false)
	}()
}

// companionShowCommand is SW_SHOW for a hidden window and SW_RESTORE only when
// the window is minimized. SW_RESTORE on a maximized window returns it to normal.
func companionShowCommand(iconic bool) int {
	if iconic {
		return 9
	}
	return 5
}

// companionRaiseFlags posts the z-order change. The bits are SWP_NOSIZE,
// SWP_NOMOVE, SWP_SHOWWINDOW, and SWP_ASYNCWINDOWPOS.
func companionRaiseFlags() uintptr {
	return 0x0001 | 0x0002 | 0x0040 | 0x4000
}

// companionRevealDeminiaturizes is false on Windows. See revealFileCompanionWindow.
func companionRevealDeminiaturizes() bool {
	return goruntime.GOOS != "windows"
}

func applyDesktopLaunchOptions(app *App, opts *options.App, launch desktopLaunch) {
	if app == nil || opts == nil {
		return
	}
	if launch.Mode == desktopLaunchFileCompanion {
		app.seedFileCompanionPaths(launch.Paths)
		opts.Width = 1280
		opts.Height = 800
		opts.WindowStartState = options.Normal
		opts.StartHidden = false
		// Alt+F4 and the native close box hide this window. The title-bar
		// button calls runtime WindowHide itself; Quit would drop the open tabs.
		opts.HideWindowOnClose = true
		opts.OnStartup = app.fileCompanionStartup
		opts.OnDomReady = app.fileCompanionDomReady
		if opts.SingleInstanceLock != nil {
			opts.SingleInstanceLock.UniqueId = launch.LockID
			opts.SingleInstanceLock.OnSecondInstanceLaunch = app.onFileCompanionSecondInstance
		}
		if opts.Windows != nil {
			profile := fileCompanionWebviewUserDataPath()
			opts.Windows.WebviewUserDataPath = profile
			clearWebviewAssetCacheIfNeeded(profile)
		}
		if opts.Mac == nil {
			opts.Mac = &mac.Options{}
		}
		opts.Mac.OnFileOpen = app.darwinOnFileOpen
		logFileCompanion("process lock=%s paths=%d", launch.LockID, len(launch.Paths))
		return
	}
	if opts.Mac == nil {
		opts.Mac = &mac.Options{}
	}
	opts.Mac.OnFileOpen = app.darwinOnFileOpen
	if launch.StartHidden {
		opts.StartHidden = true
		app.armDarwinDocumentLatch()
		opts.OnStartup = app.darwinLatchedStartup
		opts.Mac.OnLaunchFileBatch = app.darwinOnLaunchFileBatch
		logFileCompanion("darwin main latch StartHidden lock=%s", launch.LockID)
	}
}

func fileCompanionWebviewUserDataPath() string {
	if goruntime.GOOS != "windows" {
		return ""
	}
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, webviewProfileFolder()+".file-companion")
}

// fileCompanionStartup is the companion OnStartup. It loads config, watches
// config.json only to drop the in-memory snapshot, and prepares a Hub client.
// It does not build an IM handler, open the main transcript, or start tray,
// environment check, updates, ACP, MCP, pet, TinyTeX, Computer Use, workflow,
// or a workspace-directory write.
func (a *App) fileCompanionStartup(ctx context.Context) {
	if a == nil {
		return
	}
	a.ctx = ctx
	st := a.companionRuntime()
	st.mu.Lock()
	st.process = true
	st.mu.Unlock()
	if _, err := a.LoadConfig(); err != nil {
		logFileCompanion("config load: %v", err)
	}
	if err := registerFileCompanionLinuxDesktop(); err != nil {
		logFileCompanion("linux desktop register: %v", err)
	}
	a.startFileCompanionConfigWatcher()
	a.prepareFileCompanionHub()
	logFileCompanion("startup complete")
}

func (a *App) fileCompanionDomReady(ctx context.Context) {
	if a == nil {
		return
	}
	if ctx != nil {
		a.ctx = ctx
	}
	logFileCompanion("dom ready")
}

func (a *App) startFileCompanionConfigWatcher() {
	if a == nil {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logFileCompanion("config watch: %v", err)
		return
	}
	a.watcher = watcher
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					a.noteFileCompanionConfigChanged()
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	configPath, err := a.getConfigPath()
	if err != nil {
		return
	}
	if err := watcher.Add(configPath); err != nil {
		logFileCompanion("watch %s: %v", configPath, err)
	}
}

// noteFileCompanionConfigChanged drops the published snapshot. It does not
// refresh power settings or sync IM gateways.
func (a *App) noteFileCompanionConfigChanged() {
	if a == nil {
		return
	}
	a.configMu.Lock()
	a.invalidateConfigCacheLocked()
	a.configMu.Unlock()
}

// prepareFileCompanionHub builds the Hub client only. The IM handler is
// attached later with a private transcript and is not constructed here.
func (a *App) prepareFileCompanionHub() {
	if a == nil {
		return
	}
	if a.remoteSessions == nil {
		a.remoteSessions = NewRemoteSessionManager(a)
	}
	if a.remoteSessions.GetHubClient() != nil {
		return
	}
	a.remoteSessions.SetHubClient(NewRemoteHubClient(a, a.remoteSessions))
}

func logFileCompanion(format string, args ...any) {
	log.Printf("[file-companion] "+format, args...)
}

// injectFileCompanionBootFlag marks index.html so the shell can choose the
// companion window before any main-app effect runs.
func injectFileCompanionBootFlag(html []byte) []byte {
	const marker = `<head>`
	const injected = "<head><script>window.__MACLAW_FILE_COMPANION__=true</script>"
	if strings.Contains(string(html), "__MACLAW_FILE_COMPANION__") {
		return html
	}
	if !strings.Contains(string(html), marker) {
		return html
	}
	return []byte(strings.Replace(string(html), marker, injected, 1))
}
