package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	knowledgeFolderWatchMaxFolders = 16
	knowledgeFolderWatchFileName   = "knowledge-folder-watches.json"

	knowledgeFolderWatchStatusWatching  = "watching"
	knowledgeFolderWatchStatusImporting = "importing"
	knowledgeFolderWatchStatusError     = "error"
	knowledgeFolderWatchStatusMissing   = "missing"
)

// knowledgeFolderWatchDebounce coalesces editor save bursts before import.
var knowledgeFolderWatchDebounce = 800 * time.Millisecond

// knowledgeFolderWatchMaxDirs is the process-wide cap on watched directories.
var knowledgeFolderWatchMaxDirs = 2000

var (
	errKnowledgeFolderWatchDirLimit = errors.New("knowledge folder watch directory limit")
	errKnowledgeFolderWatchGone     = errors.New("knowledge folder watch removed")
)

func knowledgeFolderWatchDirLimitMessage() string {
	return fmt.Sprintf("stopped watching new directories after %d", knowledgeFolderWatchMaxDirs)
}

// KnowledgeFolderWatch is one user-selected folder whose document changes are
// imported into the local knowledge base. Removing it stops watching only.
type KnowledgeFolderWatch struct {
	ID            string    `json:"id"`
	Path          string    `json:"path"`
	Recursive     bool      `json:"recursive"`
	AddedAt       time.Time `json:"added_at"`
	LastEventAt   time.Time `json:"last_event_at,omitempty"`
	LastImportAt  time.Time `json:"last_import_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	LastFile      string    `json:"last_file,omitempty"`
	ImportedFiles int       `json:"imported_files"`
	SkippedFiles  int       `json:"skipped_files"`
	FailedFiles   int       `json:"failed_files"`
	Status        string    `json:"status"`
}

type knowledgeFolderWatchFile struct {
	Watches []KnowledgeFolderWatch `json:"watches"`
}

// knowledgeFolderWatchPending coalesces file changes for one watched folder.
type knowledgeFolderWatchPending struct {
	gen   int
	timer *time.Timer
	paths map[string]struct{}
}

// knowledgeFolderWatchDir is one directory registered with fsnotify.
// The map key is the case-folded path. Path is the directory fsnotify stores,
// which Remove must use again. On Windows a clean branch sets recursive and is
// added as one subtree watch, so descendant directories are not opened.
type knowledgeFolderWatchDir struct {
	id        string
	path      string
	recursive bool
}

// knowledgeFolderWatchDirRequest is one directory attach or forget. The
// fsnotify reader only enqueues these. Add and Remove run on the directory
// worker: on Windows that backend applies them on the same thread that sends
// events, and the events channel holds 50. Remove from the reader fills the
// channel, blocks that thread, and drops later file changes.
type knowledgeFolderWatchDirRequest struct {
	path   string
	forget bool
}

type knowledgeFolderWatchState struct {
	persistMu sync.Mutex
	importMu  sync.Mutex
	// watchMu covers watcher Add/Remove together with the dirs map update.
	// Lock order is watchMu, then mu. Do not take persistMu while holding it.
	watchMu  sync.Mutex
	mu       sync.Mutex
	started  bool
	closed   bool
	cancel   context.CancelFunc
	watcher  *fsnotify.Watcher
	items    []KnowledgeFolderWatch
	dirs     map[string]knowledgeFolderWatchDir
	pending  map[string]*knowledgeFolderWatchPending
	dirQueue []knowledgeFolderWatchDirRequest
	dirWake  chan struct{}
	// overflowRunning is set while a rescan for a dropped event batch is in
	// progress. overflowAgain asks that rescan to repeat once, so a burst of
	// overflow errors does not stack one full import per error.
	overflowRunning bool
	overflowAgain   bool
}

func newKnowledgeFolderWatchState() *knowledgeFolderWatchState {
	return &knowledgeFolderWatchState{
		dirs:    map[string]knowledgeFolderWatchDir{},
		pending: map[string]*knowledgeFolderWatchPending{},
		dirWake: make(chan struct{}, 1),
	}
}

func (a *App) folderWatch() *knowledgeFolderWatchState {
	if a == nil {
		return newKnowledgeFolderWatchState()
	}
	a.knowledgeFolderWatchOnce.Do(func() {
		a.knowledgeFolderWatch = newKnowledgeFolderWatchState()
	})
	return a.knowledgeFolderWatch
}

func (a *App) knowledgeFolderWatchStorePath() string {
	return filepath.Join(a.GetDataDir(), knowledgeFolderWatchFileName)
}

// KnowledgeListFolderWatches returns the saved folder watches.
func (a *App) KnowledgeListFolderWatches() ([]KnowledgeFolderWatch, error) {
	if a == nil {
		return []KnowledgeFolderWatch{}, nil
	}
	st := a.folderWatch()
	st.mu.Lock()
	started := st.started
	items := copyKnowledgeFolderWatches(st.items)
	st.mu.Unlock()
	if started {
		if items == nil {
			items = []KnowledgeFolderWatch{}
		}
		return items, nil
	}
	return a.loadKnowledgeFolderWatches()
}

// KnowledgeBrowseAddFolderWatch opens a folder picker and starts watching the selection.
// Cancelling the picker returns an empty watch and no error.
func (a *App) KnowledgeBrowseAddFolderWatch() (KnowledgeFolderWatch, error) {
	if a == nil || a.ctx == nil {
		return KnowledgeFolderWatch{}, fmt.Errorf("folder picker is unavailable")
	}
	title := "Select Folder to Watch"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a.CurrentLanguage)), "zh") {
		title = "选择要监控的文件夹"
	}
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil {
		return KnowledgeFolderWatch{}, err
	}
	if strings.TrimSpace(selection) == "" {
		return KnowledgeFolderWatch{}, nil
	}
	return a.KnowledgeAddFolderWatch(selection)
}

// KnowledgeAddFolderWatch starts watching a folder and imports its documents.
// Adding the same folder again returns the existing watch.
func (a *App) KnowledgeAddFolderWatch(rawPath string) (KnowledgeFolderWatch, error) {
	if a == nil {
		return KnowledgeFolderWatch{}, fmt.Errorf("app is unavailable")
	}
	abs, err := canonicalKnowledgeFolderWatchPath(rawPath)
	if err != nil {
		return KnowledgeFolderWatch{}, err
	}
	dataDir := a.GetDataDir()
	if pathEqual(abs, dataDir) || knowledgeWatchPathInside(dataDir, abs) {
		return KnowledgeFolderWatch{}, fmt.Errorf("folder is inside the application data directory")
	}
	if err := a.ensureKnowledgeFolderWatch(); err != nil {
		return KnowledgeFolderWatch{}, err
	}
	st := a.folderWatch()
	st.persistMu.Lock()
	unlock := true
	defer func() {
		if unlock {
			st.persistMu.Unlock()
		}
	}()
	st.mu.Lock()
	for _, item := range st.items {
		if pathEqual(item.Path, abs) {
			existing := item
			attached := knowledgeFolderWatchDirAttachedLocked(st, existing.ID, abs)
			st.mu.Unlock()
			if attached && (existing.Status == knowledgeFolderWatchStatusWatching || existing.Status == knowledgeFolderWatchStatusImporting) {
				return existing, nil
			}
			unlock = false
			st.persistMu.Unlock()
			return a.resumeKnowledgeFolderWatch(existing)
		}
		if knowledgeWatchPathInside(item.Path, abs) || knowledgeWatchPathInside(abs, item.Path) {
			st.mu.Unlock()
			return KnowledgeFolderWatch{}, fmt.Errorf("folder is already covered by %s", item.Path)
		}
	}
	if len(st.items) >= knowledgeFolderWatchMaxFolders {
		st.mu.Unlock()
		return KnowledgeFolderWatch{}, fmt.Errorf("too many watched folders")
	}
	item := KnowledgeFolderWatch{
		ID:        knowledge.NewID("kfw"),
		Path:      abs,
		Recursive: true,
		AddedAt:   time.Now().UTC(),
		Status:    knowledgeFolderWatchStatusWatching,
	}
	st.mu.Unlock()

	warn, _, err := a.attachKnowledgeFolderWatchDirs(item.ID, abs, true)
	if err != nil {
		return KnowledgeFolderWatch{}, err
	}
	if warn != "" {
		item.LastError = warn
	}
	st.mu.Lock()
	for _, existing := range st.items {
		if pathEqual(existing.Path, abs) || knowledgeWatchPathInside(existing.Path, abs) || knowledgeWatchPathInside(abs, existing.Path) {
			same := pathEqual(existing.Path, abs)
			kept := existing
			st.mu.Unlock()
			a.detachKnowledgeFolderWatchDirs(item.ID)
			if !same {
				return KnowledgeFolderWatch{}, fmt.Errorf("folder is already covered by %s", kept.Path)
			}
			if kept.Status == knowledgeFolderWatchStatusWatching || kept.Status == knowledgeFolderWatchStatusImporting {
				return kept, nil
			}
			unlock = false
			st.persistMu.Unlock()
			return a.resumeKnowledgeFolderWatch(kept)
		}
	}
	st.items = append(st.items, item)
	snapshot := copyKnowledgeFolderWatches(st.items)
	st.mu.Unlock()
	if err := a.saveKnowledgeFolderWatches(snapshot); err != nil {
		a.detachKnowledgeFolderWatchDirs(item.ID)
		st.mu.Lock()
		st.items = removeKnowledgeFolderWatchID(st.items, item.ID)
		st.mu.Unlock()
		return KnowledgeFolderWatch{}, err
	}
	unlock = false
	st.persistMu.Unlock()
	a.emitKnowledgeFolderWatches(snapshot)
	go a.importKnowledgeFolderWatchDirectory(item.ID)
	return item, nil
}

// KnowledgeRemoveFolderWatch stops watching a folder. Imported knowledge stays.
func (a *App) KnowledgeRemoveFolderWatch(id string) error {
	if a == nil {
		return fmt.Errorf("app is unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("watched folder id is required")
	}
	if err := a.ensureKnowledgeFolderWatch(); err != nil {
		return err
	}
	st := a.folderWatch()
	st.persistMu.Lock()
	st.mu.Lock()
	var removed KnowledgeFolderWatch
	found := false
	for _, item := range st.items {
		if item.ID == id {
			removed = item
			found = true
			break
		}
	}
	if !found {
		st.mu.Unlock()
		st.persistMu.Unlock()
		return fmt.Errorf("watched folder was not found")
	}
	st.items = removeKnowledgeFolderWatchID(st.items, id)
	a.stopPendingKnowledgeFolderWatchLocked(st, removed.ID)
	snapshot := copyKnowledgeFolderWatches(st.items)
	st.mu.Unlock()
	a.detachKnowledgeFolderWatchDirs(id)
	if err := a.saveKnowledgeFolderWatches(snapshot); err != nil {
		st.mu.Lock()
		st.items = append(st.items, removed)
		st.mu.Unlock()
		if _, _, attachErr := a.attachKnowledgeFolderWatchDirs(removed.ID, removed.Path, false); attachErr != nil {
			log.Printf("[knowledge-watch] restore %s: %v", removed.Path, attachErr)
		}
		st.persistMu.Unlock()
		return err
	}
	st.persistMu.Unlock()
	a.emitKnowledgeFolderWatches(snapshot)
	return nil
}

func (a *App) ensureKnowledgeFolderWatch() error {
	if a == nil {
		return fmt.Errorf("app is unavailable")
	}
	st := a.folderWatch()
	st.persistMu.Lock()
	st.mu.Lock()
	if st.started && st.watcher != nil {
		st.mu.Unlock()
		st.persistMu.Unlock()
		return nil
	}
	st.mu.Unlock()

	items, err := a.loadKnowledgeFolderWatches()
	if err != nil {
		st.persistMu.Unlock()
		return err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		st.persistMu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	ids := make([]string, 0, len(items))
	for i := range items {
		if strings.TrimSpace(items[i].Status) == "" {
			items[i].Status = knowledgeFolderWatchStatusWatching
		}
		if _, statErr := os.Stat(items[i].Path); statErr != nil {
			items[i].Status = knowledgeFolderWatchStatusMissing
			items[i].LastError = "folder does not exist"
			continue
		}
		ids = append(ids, items[i].ID)
	}
	st.mu.Lock()
	st.watcher = watcher
	st.cancel = cancel
	st.items = items
	st.closed = false
	st.started = true
	if st.dirs == nil {
		st.dirs = map[string]knowledgeFolderWatchDir{}
	}
	if st.pending == nil {
		st.pending = map[string]*knowledgeFolderWatchPending{}
	}
	if st.dirWake == nil {
		st.dirWake = make(chan struct{}, 1)
	}
	st.mu.Unlock()
	st.persistMu.Unlock()

	go a.runKnowledgeFolderWatchDirs(ctx)
	go a.runKnowledgeFolderWatchLoop(ctx, watcher)
	for _, item := range items {
		if item.Status == knowledgeFolderWatchStatusMissing {
			continue
		}
		warn, _, attachErr := a.attachKnowledgeFolderWatchDirs(item.ID, item.Path, false)
		if attachErr != nil {
			log.Printf("[knowledge-watch] attach %s: %v", item.Path, attachErr)
			a.updateKnowledgeFolderWatch(item.ID, func(watch *KnowledgeFolderWatch) {
				watch.Status = knowledgeFolderWatchStatusError
				watch.LastError = attachErr.Error()
			})
			continue
		}
		if warn != "" {
			a.updateKnowledgeFolderWatch(item.ID, func(watch *KnowledgeFolderWatch) {
				if watch.LastError == "" {
					watch.LastError = warn
				}
			})
		}
	}
	go a.reconcileKnowledgeFolderWatches(ids)
	return nil
}

func (a *App) stopKnowledgeFolderWatch() {
	if a == nil || a.knowledgeFolderWatch == nil {
		return
	}
	st := a.knowledgeFolderWatch
	st.persistMu.Lock()
	st.mu.Lock()
	cancel := st.cancel
	watcher := st.watcher
	pending := st.pending
	st.pending = map[string]*knowledgeFolderWatchPending{}
	st.started = false
	st.closed = true
	st.watcher = nil
	st.cancel = nil
	st.dirs = map[string]knowledgeFolderWatchDir{}
	st.mu.Unlock()
	st.persistMu.Unlock()
	for _, bucket := range pending {
		if bucket != nil && bucket.timer != nil {
			bucket.timer.Stop()
		}
	}
	if cancel != nil {
		cancel()
	}
	if watcher != nil {
		_ = watcher.Close()
	}
	st.importMu.Lock()
	st.importMu.Unlock()
}

func (a *App) runKnowledgeFolderWatchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	if watcher == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			a.handleKnowledgeFolderWatchEvent(event)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			a.handleKnowledgeFolderWatchError(err)
		}
	}
}

// handleKnowledgeFolderWatchError records a watcher failure. An overflow means
// the kernel buffer dropped events. The rescan runs off this loop: Add on the
// reader can fill the event channel and block the backend thread.
func (a *App) handleKnowledgeFolderWatchError(err error) {
	if err == nil {
		return
	}
	log.Printf("[knowledge-watch] watcher: %v", err)
	if knowledgeFolderWatchEventOverflow(err) {
		a.requestKnowledgeFolderWatchOverflowReconcile()
	}
}

func knowledgeFolderWatchEventOverflow(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "events have likely been missed")
}

func (a *App) requestKnowledgeFolderWatchOverflowReconcile() {
	if a == nil || a.knowledgeFolderWatchClosed() {
		return
	}
	st := a.folderWatch()
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return
	}
	if st.overflowRunning {
		st.overflowAgain = true
		st.mu.Unlock()
		return
	}
	st.overflowRunning = true
	st.mu.Unlock()
	go a.runKnowledgeFolderWatchOverflowReconcile()
}

func (a *App) runKnowledgeFolderWatchOverflowReconcile() {
	for {
		if a.knowledgeFolderWatchClosed() {
			break
		}
		st := a.folderWatch()
		st.mu.Lock()
		items := copyKnowledgeFolderWatches(st.items)
		st.mu.Unlock()
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if a.knowledgeFolderWatchClosed() {
				break
			}
			if item.Status == knowledgeFolderWatchStatusMissing {
				continue
			}
			if _, err := os.Stat(item.Path); err != nil {
				continue
			}
			// A subtree watch cannot exclude a skipped directory that appeared
			// later. Split that branch before putting kernel watches back.
			a.separateKnowledgeFolderWatchSkipped(item.ID, item.Path)
			// The directory map can still list a folder after fsnotify dropped
			// its inode. Revive adds those directories again, then the import
			// below reads the files whose events were lost.
			_, _, _ = a.reviveKnowledgeFolderWatchDirs(item.ID, item.Path)
			ids = append(ids, item.ID)
		}
		a.reconcileKnowledgeFolderWatches(ids)
		st.mu.Lock()
		again := st.overflowAgain && !st.closed
		st.overflowAgain = false
		if !again {
			st.overflowRunning = false
			st.mu.Unlock()
			return
		}
		st.mu.Unlock()
	}
	st := a.folderWatch()
	st.mu.Lock()
	st.overflowRunning = false
	st.overflowAgain = false
	st.mu.Unlock()
}

func (a *App) handleKnowledgeFolderWatchEvent(event fsnotify.Event) {
	path := strings.TrimSpace(event.Name)
	if path == "" || a.knowledgeFolderWatchClosed() {
		return
	}
	removed := event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
	changed := event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename)
	if !removed && !changed {
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		if removed {
			a.enqueueKnowledgeFolderWatchForget(path)
		}
		return
	}
	if info.IsDir() {
		// The parent reports a recreated directory as Create. A Remove that is
		// handled only after that directory exists again is the same case:
		// fsnotify has dropped the old inode, and the map can still list it.
		if event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
			a.enqueueKnowledgeFolderWatchDir(path)
		}
		return
	}
	if !changed || !info.Mode().IsRegular() || knowledgeFolderWatchIgnoredFile(path) {
		return
	}
	item, ok := a.knowledgeFolderWatchForPath(path)
	if !ok || knowledgeFolderWatchPathSkipped(item.Path, path, a.GetDataDir()) {
		return
	}
	a.scheduleKnowledgeFolderWatchImport(path)
}

func (a *App) enqueueKnowledgeFolderWatchDir(path string) {
	a.enqueueKnowledgeFolderWatchRequest(knowledgeFolderWatchDirRequest{path: path})
}

func (a *App) enqueueKnowledgeFolderWatchForget(path string) {
	a.enqueueKnowledgeFolderWatchRequest(knowledgeFolderWatchDirRequest{path: path, forget: true})
}

func (a *App) enqueueKnowledgeFolderWatchRequest(req knowledgeFolderWatchDirRequest) {
	if a == nil || strings.TrimSpace(req.path) == "" || a.knowledgeFolderWatchClosed() {
		return
	}
	st := a.folderWatch()
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return
	}
	st.dirQueue = append(st.dirQueue, req)
	wake := st.dirWake
	st.mu.Unlock()
	if wake == nil {
		a.runKnowledgeFolderWatchDirRequest(req)
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (a *App) runKnowledgeFolderWatchDirs(ctx context.Context) {
	st := a.folderWatch()
	wake := st.dirWake
	if wake == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			for {
				if ctx.Err() != nil || a.knowledgeFolderWatchClosed() {
					return
				}
				st.mu.Lock()
				if len(st.dirQueue) == 0 {
					st.mu.Unlock()
					break
				}
				req := st.dirQueue[0]
				st.dirQueue[0] = knowledgeFolderWatchDirRequest{}
				st.dirQueue = st.dirQueue[1:]
				st.mu.Unlock()
				a.runKnowledgeFolderWatchDirRequest(req)
			}
		}
	}
}

func (a *App) runKnowledgeFolderWatchDirRequest(req knowledgeFolderWatchDirRequest) {
	if req.forget {
		a.forgetKnowledgeFolderWatchPath(req.path)
		return
	}
	a.watchNewKnowledgeFolder(req.path)
}

func (a *App) watchNewKnowledgeFolder(path string) {
	item, ok := a.knowledgeFolderWatchForPath(path)
	if !ok {
		return
	}
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	if knowledgeFolderWatchPathSkipped(item.Path, path, dataDir) {
		// The directory is inside a subtree watch, which reports every file
		// under it. Replace that watch so the skipped directory is not opened.
		if cover, covered := a.knowledgeFolderWatchRecursiveCover(item.ID, path); covered {
			if a.separateKnowledgeFolderWatchSkipped(item.ID, cover.path) {
				go a.importKnowledgeFolderWatchSubtree(item.ID, cover.path)
			}
		}
		return
	}
	if cover, ok := a.knowledgeFolderWatchRecursiveCover(item.ID, path); ok {
		kernel := a.knowledgeFolderWatchKernelKeys()
		if _, live := kernel[knowledgeFolderWatchDirKey(cover.path)]; !live {
			warn, _, err := a.attachKnowledgeFolderWatchDirsRestoring(item.ID, cover.path, false, kernel)
			if err != nil {
				log.Printf("[knowledge-watch] attach new dir %s: %v", cover.path, err)
				return
			}
			if warn != "" {
				a.noteKnowledgeFolderWatchDirLimit(item.ID, warn)
			}
		}
		// The parent subtree watch already reports files here. Import this
		// directory so files that arrived with it are not left until the next
		// edit. A descendant handle is not opened, so the directory can be renamed.
		go a.importKnowledgeFolderWatchSubtree(item.ID, path)
		return
	}
	restore := false
	var kernel map[string]struct{}
	if a.knowledgeFolderWatchRootAttached(item.ID, path) {
		kernel = a.knowledgeFolderWatchKernelKeys()
		if _, live := kernel[knowledgeFolderWatchDirKey(path)]; live {
			return
		}
		// This directory has its own map entry, but ReadDirectoryChanges is gone.
		// Add it again and import files written while the watch was down.
		restore = true
	}
	var warn string
	var added int
	var err error
	if restore {
		warn, added, err = a.attachKnowledgeFolderWatchDirsRestoring(item.ID, path, false, kernel)
	} else {
		warn, added, err = a.attachKnowledgeFolderWatchDirs(item.ID, path, false)
	}
	if err != nil {
		log.Printf("[knowledge-watch] attach new dir %s: %v", path, err)
		return
	}
	if warn != "" {
		a.noteKnowledgeFolderWatchDirLimit(item.ID, warn)
	}
	if added > 0 || restore {
		go a.importKnowledgeFolderWatchSubtree(item.ID, path)
	}
}

func (a *App) scheduleKnowledgeFolderWatchImport(path string) {
	if a.knowledgeFolderWatchClosed() {
		return
	}
	st := a.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return
	}
	item, ok := knowledgeFolderWatchForPathLocked(st.items, path)
	if !ok {
		return
	}
	if st.pending == nil {
		st.pending = map[string]*knowledgeFolderWatchPending{}
	}
	bucket := st.pending[item.ID]
	if bucket == nil {
		bucket = &knowledgeFolderWatchPending{paths: map[string]struct{}{}}
		st.pending[item.ID] = bucket
	}
	if bucket.paths == nil {
		bucket.paths = map[string]struct{}{}
	}
	bucket.paths[path] = struct{}{}
	if bucket.timer != nil {
		bucket.timer.Stop()
	}
	bucket.gen++
	gen := bucket.gen
	id := item.ID
	bucket.timer = time.AfterFunc(knowledgeFolderWatchDebounce, func() {
		paths := a.takeKnowledgeFolderWatchPending(id, gen)
		if len(paths) == 0 {
			return
		}
		a.importKnowledgeFolderWatchFiles(id, paths)
	})
}

func (a *App) takeKnowledgeFolderWatchPending(id string, gen int) []string {
	st := a.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	bucket := st.pending[id]
	if bucket == nil || bucket.gen != gen {
		return nil
	}
	paths := make([]string, 0, len(bucket.paths))
	for path := range bucket.paths {
		paths = append(paths, path)
	}
	delete(st.pending, id)
	return paths
}

func (a *App) reconcileKnowledgeFolderWatches(ids []string) {
	for _, id := range ids {
		if a.knowledgeFolderWatchClosed() {
			return
		}
		a.importKnowledgeFolderWatchDirectory(id)
	}
}

func (a *App) importKnowledgeFolderWatchDirectory(id string) {
	if a == nil || a.knowledgeFolderWatchClosed() {
		return
	}
	st := a.folderWatch()
	st.importMu.Lock()
	defer st.importMu.Unlock()
	if a.knowledgeFolderWatchClosed() {
		return
	}
	item, ok := a.knowledgeFolderWatchByID(id)
	if !ok {
		return
	}
	if _, err := os.Stat(item.Path); err != nil {
		a.updateKnowledgeFolderWatch(id, func(watch *KnowledgeFolderWatch) {
			watch.Status = knowledgeFolderWatchStatusMissing
			watch.LastError = "folder does not exist"
		})
		return
	}
	a.updateKnowledgeFolderWatch(id, func(watch *KnowledgeFolderWatch) {
		watch.Status = knowledgeFolderWatchStatusImporting
	})
	store, err := a.openKnowledgeStore()
	if err != nil {
		a.finishKnowledgeFolderWatchImport(id, item.Path, knowledge.DirectoryImportResult{}, err)
		return
	}
	result, err := store.ImportDirectory(a.knowledgeContext(), a.knowledgeFolderWatchImportRequest(item.Path))
	_ = store.Close()
	if !a.knowledgeFolderWatchHas(id) || a.knowledgeFolderWatchClosed() {
		return
	}
	a.finishKnowledgeFolderWatchImport(id, item.Path, result, err)
}

func (a *App) importKnowledgeFolderWatchFile(path string) {
	item, ok := a.knowledgeFolderWatchForPath(path)
	if !ok {
		return
	}
	a.importKnowledgeFolderWatchFiles(item.ID, []string{path})
}

func (a *App) importKnowledgeFolderWatchSubtree(id, dir string) {
	item, ok := a.knowledgeFolderWatchByID(id)
	if !ok || item.Status == knowledgeFolderWatchStatusMissing {
		return
	}
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	files := make([]string, 0)
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if knowledgeFolderWatchPathSkipped(item.Path, path, dataDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if knowledgeFolderWatchIgnoredFile(path) || knowledgeFolderWatchPathSkipped(item.Path, path, dataDir) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if len(files) == 0 {
		return
	}
	a.importKnowledgeFolderWatchFiles(id, files)
}

func (a *App) importKnowledgeFolderWatchFiles(id string, paths []string) {
	if a == nil || a.knowledgeFolderWatchClosed() || len(paths) == 0 {
		return
	}
	st := a.folderWatch()
	st.importMu.Lock()
	defer st.importMu.Unlock()
	if a.knowledgeFolderWatchClosed() {
		return
	}
	item, ok := a.knowledgeFolderWatchByID(id)
	if !ok || item.Status == knowledgeFolderWatchStatusMissing {
		return
	}
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	cleaned := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		key := cleanKnowledgeWatchPath(path)
		if _, dup := seen[key]; dup {
			continue
		}
		if knowledgeFolderWatchIgnoredFile(path) || knowledgeFolderWatchPathSkipped(item.Path, path, dataDir) {
			continue
		}
		if !pathEqual(item.Path, path) && !knowledgeWatchPathInside(item.Path, path) {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, path)
	}
	if len(cleaned) == 0 {
		return
	}
	sort.Strings(cleaned)
	a.updateKnowledgeFolderWatch(id, func(watch *KnowledgeFolderWatch) {
		watch.Status = knowledgeFolderWatchStatusImporting
	})
	store, err := a.openKnowledgeStore()
	if err != nil {
		a.finishKnowledgeFolderWatchImport(id, cleaned[len(cleaned)-1], knowledge.DirectoryImportResult{}, err)
		return
	}
	result, err := store.ImportFiles(a.knowledgeContext(), a.knowledgeFolderWatchImportRequest(item.Path), cleaned)
	_ = store.Close()
	if !a.knowledgeFolderWatchHas(id) || a.knowledgeFolderWatchClosed() {
		return
	}
	a.finishKnowledgeFolderWatchImport(id, cleaned[len(cleaned)-1], result, err)
}

func (a *App) knowledgeFolderWatchImportRequest(root string) knowledge.DirectoryImportRequest {
	globs := knowledgeFolderWatchExcludeGlobs()
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	if extra := knowledgeFolderWatchDataDirExcludeGlob(root, dataDir); extra != "" {
		globs = append(globs, extra)
	}
	return a.normalizeKnowledgeImportRequest(knowledge.DirectoryImportRequest{
		RootPath:     root,
		SaveScope:    knowledge.SaveScopePersonal,
		TopicHint:    filepath.Base(root),
		Labels:       []string{"folder-watch", filepath.Base(root)},
		AutoLabels:   true,
		Recursive:    true,
		ExcludeGlobs: globs,
	})
}

func (a *App) finishKnowledgeFolderWatchImport(id, path string, result knowledge.DirectoryImportResult, importErr error) {
	rootMissing := false
	var reattachErr error
	if current, ok := a.knowledgeFolderWatchByID(id); ok {
		if _, err := os.Stat(current.Path); err != nil {
			rootMissing = true
		} else if !a.knowledgeFolderWatchRootAttached(id, current.Path) {
			_, _, reattachErr = a.attachKnowledgeFolderWatchDirs(id, current.Path, false)
		}
	}
	limitMessage := knowledgeFolderWatchDirLimitMessage()
	a.updateKnowledgeFolderWatch(id, func(watch *KnowledgeFolderWatch) {
		watch.LastImportAt = time.Now().UTC()
		watch.LastEventAt = watch.LastImportAt
		if strings.TrimSpace(path) != "" {
			watch.LastFile = path
		}
		watch.ImportedFiles = result.ImportedFiles
		watch.SkippedFiles = result.SkippedFiles
		watch.FailedFiles = result.FailedFiles
		if rootMissing {
			watch.Status = knowledgeFolderWatchStatusMissing
			watch.LastError = "folder does not exist"
			return
		}
		keepDirLimit := watch.LastError == limitMessage
		if importErr != nil {
			watch.Status = knowledgeFolderWatchStatusError
			watch.LastError = importErr.Error()
			return
		}
		if reattachErr != nil {
			watch.Status = knowledgeFolderWatchStatusError
			watch.LastError = reattachErr.Error()
			return
		}
		watch.Status = knowledgeFolderWatchStatusWatching
		if result.FailedFiles > 0 {
			watch.LastError = fmt.Sprintf("failed %d file(s)", result.FailedFiles)
			return
		}
		if keepDirLimit {
			watch.LastError = limitMessage
			return
		}
		watch.LastError = ""
	})
}

// attachKnowledgeFolderWatchDirs watches root and its child directories.
// allowUnlisted is set while Add is still inserting the watch into the list.
// The returned count is the number of directories newly registered for id.
func (a *App) attachKnowledgeFolderWatchDirs(id, root string, allowUnlisted bool) (string, int, error) {
	return a.attachKnowledgeFolderWatchDirsRestoring(id, root, allowUnlisted, nil)
}

// reviveKnowledgeFolderWatchDirs adds directories whose kernel watch was
// dropped while the directory map still lists them.
func (a *App) reviveKnowledgeFolderWatchDirs(id, root string) (string, int, error) {
	return a.attachKnowledgeFolderWatchDirsRestoring(id, root, false, a.knowledgeFolderWatchKernelKeys())
}

// attachKnowledgeFolderWatchDirsRestoring walks root. kernel is nil for a
// normal attach: a directory already in the map is left alone. A non-nil
// kernel is the WatchList snapshot, and a mapped directory missing from it is
// added again without taking another directory slot.
func (a *App) attachKnowledgeFolderWatchDirsRestoring(id, root string, allowUnlisted bool, kernel map[string]struct{}) (string, int, error) {
	st := a.folderWatch()
	st.mu.Lock()
	watcher := st.watcher
	closed := st.closed
	st.mu.Unlock()
	if closed {
		return "", 0, fmt.Errorf("folder watcher is stopped")
	}
	if watcher == nil {
		return "", 0, fmt.Errorf("folder watcher is not running")
	}
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	blocked := knowledgeFolderWatchBlockedRecursive(root, dataDir)
	addedDirs := make([]string, 0)
	limitHit := false
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if pathEqual(path, root) {
				return walkErr
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if knowledgeFolderWatchPathSkipped(root, path, dataDir) {
			return filepath.SkipDir
		}
		if !allowUnlisted && !a.knowledgeFolderWatchHas(id) {
			return errKnowledgeFolderWatchGone
		}
		clean := filepath.Clean(path)
		key := knowledgeFolderWatchDirKey(clean)
		_, blockedRecursive := blocked[key]
		recurse := knowledgeFolderWatchSupportsRecurse() && !blockedRecursive
		st.mu.Lock()
		owner := ""
		watched := false
		recursiveExisting := false
		if st.dirs != nil {
			if entry, ok := st.dirs[key]; ok {
				owner = entry.id
				watched = true
				recursiveExisting = entry.recursive
			}
		}
		atLimit := !watched && len(st.dirs) >= knowledgeFolderWatchMaxDirs
		st.mu.Unlock()
		if watched && owner != id {
			if pathEqual(clean, root) {
				return fmt.Errorf("folder is already covered by another watch")
			}
			return filepath.SkipDir
		}
		if watched && !knowledgeFolderWatchKernelMissing(kernel, key) {
			// A subtree watch already reports every descendant. Opening those
			// directories would make Windows refuse to rename this folder.
			if recursiveExisting {
				return filepath.SkipDir
			}
			return nil
		}
		if !watched && atLimit {
			limitHit = true
			return errKnowledgeFolderWatchDirLimit
		}
		st.watchMu.Lock()
		addErr := watcher.Add(knowledgeFolderWatchKernelPath(clean, recurse))
		if addErr != nil {
			st.watchMu.Unlock()
			if pathEqual(clean, root) {
				return addErr
			}
			log.Printf("[knowledge-watch] watch %s: %v", clean, addErr)
			return nil
		}
		st.mu.Lock()
		if st.dirs != nil {
			if existing, ok := st.dirs[key]; ok && existing.id != id {
				st.mu.Unlock()
				st.watchMu.Unlock()
				if pathEqual(clean, root) {
					return fmt.Errorf("folder is already covered by another watch")
				}
				return filepath.SkipDir
			}
		}
		rollback := false
		if st.closed || st.watcher == nil || (!allowUnlisted && !knowledgeFolderWatchContainsID(st.items, id)) {
			rollback = true
		} else {
			if st.dirs == nil {
				st.dirs = map[string]knowledgeFolderWatchDir{}
			}
			if _, ok := st.dirs[key]; !ok && len(st.dirs) >= knowledgeFolderWatchMaxDirs {
				rollback = true
				limitHit = true
			} else {
				if _, ok := st.dirs[key]; !ok {
					addedDirs = append(addedDirs, key)
				}
				st.dirs[key] = knowledgeFolderWatchDir{id: id, path: clean, recursive: recurse}
			}
		}
		st.mu.Unlock()
		if rollback {
			_ = watcher.Remove(clean)
		}
		st.watchMu.Unlock()
		if rollback && limitHit {
			return errKnowledgeFolderWatchDirLimit
		}
		if rollback {
			return errKnowledgeFolderWatchGone
		}
		if recurse {
			return filepath.SkipDir
		}
		return nil
	})
	if errors.Is(walkErr, errKnowledgeFolderWatchGone) {
		a.removeWatchedDirs(id, addedDirs)
		return "", 0, nil
	}
	if limitHit || errors.Is(walkErr, errKnowledgeFolderWatchDirLimit) {
		return knowledgeFolderWatchDirLimitMessage(), len(addedDirs), nil
	}
	if walkErr != nil {
		a.removeWatchedDirs(id, addedDirs)
		return "", 0, fmt.Errorf("watch folder: %w", walkErr)
	}
	return "", len(addedDirs), nil
}

func (a *App) detachKnowledgeFolderWatchDirs(id string) {
	st := a.folderWatch()
	st.watchMu.Lock()
	st.mu.Lock()
	watcher := st.watcher
	paths := make([]string, 0)
	for key, entry := range st.dirs {
		if entry.id == id {
			paths = append(paths, entry.path)
			delete(st.dirs, key)
		}
	}
	st.mu.Unlock()
	for _, dir := range paths {
		if watcher != nil {
			_ = watcher.Remove(dir)
		}
	}
	st.watchMu.Unlock()
}

func (a *App) updateKnowledgeFolderWatch(id string, mutate func(*KnowledgeFolderWatch)) {
	if a == nil || mutate == nil || a.knowledgeFolderWatchClosed() {
		return
	}
	st := a.folderWatch()
	st.persistMu.Lock()
	st.mu.Lock()
	found := false
	for i := range st.items {
		if st.items[i].ID == id {
			mutate(&st.items[i])
			found = true
			break
		}
	}
	snapshot := copyKnowledgeFolderWatches(st.items)
	st.mu.Unlock()
	if !found {
		st.persistMu.Unlock()
		return
	}
	if err := a.saveKnowledgeFolderWatches(snapshot); err != nil {
		log.Printf("[knowledge-watch] save: %v", err)
	}
	st.persistMu.Unlock()
	a.emitKnowledgeFolderWatches(snapshot)
}

func (a *App) emitKnowledgeFolderWatches(items []KnowledgeFolderWatch) {
	if items == nil {
		items = []KnowledgeFolderWatch{}
	}
	a.emitEvent("knowledge:folder-watch", items)
}

func (a *App) knowledgeFolderWatchClosed() bool {
	if a == nil || a.knowledgeFolderWatch == nil {
		return false
	}
	st := a.knowledgeFolderWatch
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.closed
}

func (a *App) knowledgeFolderWatchHas(id string) bool {
	_, ok := a.knowledgeFolderWatchByID(id)
	return ok
}

func (a *App) knowledgeFolderWatchByID(id string) (KnowledgeFolderWatch, bool) {
	st := a.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, item := range st.items {
		if item.ID == id {
			return item, true
		}
	}
	return KnowledgeFolderWatch{}, false
}

func (a *App) knowledgeFolderWatchForPath(path string) (KnowledgeFolderWatch, bool) {
	st := a.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	return knowledgeFolderWatchForPathLocked(st.items, path)
}

func knowledgeFolderWatchForPathLocked(items []KnowledgeFolderWatch, path string) (KnowledgeFolderWatch, bool) {
	var best KnowledgeFolderWatch
	found := false
	for _, item := range items {
		if item.Status == knowledgeFolderWatchStatusMissing {
			continue
		}
		if pathEqual(item.Path, path) || knowledgeWatchPathInside(item.Path, path) {
			if !found || len(cleanKnowledgeWatchPath(item.Path)) > len(cleanKnowledgeWatchPath(best.Path)) {
				best = item
				found = true
			}
		}
	}
	return best, found
}

func (a *App) stopPendingKnowledgeFolderWatchLocked(st *knowledgeFolderWatchState, id string) {
	bucket := st.pending[id]
	if bucket == nil {
		return
	}
	if bucket.timer != nil {
		bucket.timer.Stop()
	}
	delete(st.pending, id)
}

func (a *App) resumeKnowledgeFolderWatch(item KnowledgeFolderWatch) (KnowledgeFolderWatch, error) {
	if _, err := os.Stat(item.Path); err != nil {
		return item, nil
	}
	warn, _, err := a.attachKnowledgeFolderWatchDirs(item.ID, item.Path, false)
	if err != nil {
		a.updateKnowledgeFolderWatch(item.ID, func(watch *KnowledgeFolderWatch) {
			watch.Status = knowledgeFolderWatchStatusError
			watch.LastError = err.Error()
		})
		updated, ok := a.knowledgeFolderWatchByID(item.ID)
		if !ok {
			return KnowledgeFolderWatch{}, err
		}
		return updated, err
	}
	a.updateKnowledgeFolderWatch(item.ID, func(watch *KnowledgeFolderWatch) {
		watch.Status = knowledgeFolderWatchStatusWatching
		watch.LastError = warn
	})
	go a.importKnowledgeFolderWatchDirectory(item.ID)
	updated, ok := a.knowledgeFolderWatchByID(item.ID)
	if !ok {
		return item, nil
	}
	return updated, nil
}

func (a *App) noteKnowledgeFolderWatchDirLimit(id, warn string) {
	if warn == "" {
		return
	}
	a.updateKnowledgeFolderWatch(id, func(watch *KnowledgeFolderWatch) {
		if watch.Status == knowledgeFolderWatchStatusError {
			return
		}
		if watch.LastError == "" || watch.LastError == knowledgeFolderWatchDirLimitMessage() {
			watch.LastError = warn
		}
	})
}

// separateKnowledgeFolderWatchSkipped rebuilds recursive watches under root
// that now contain a skipped directory. The subtree handle is removed and the
// branch is attached again, leaving the skipped directory unopened.
func (a *App) separateKnowledgeFolderWatchSkipped(id, root string) bool {
	if a == nil || id == "" || root == "" || !knowledgeFolderWatchSupportsRecurse() || a.knowledgeFolderWatchClosed() {
		return false
	}
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	st := a.folderWatch()
	st.mu.Lock()
	covers := make([]string, 0)
	for _, entry := range st.dirs {
		if entry.id != id || !entry.recursive {
			continue
		}
		if pathEqual(entry.path, root) || knowledgeWatchPathInside(root, entry.path) {
			covers = append(covers, entry.path)
		}
	}
	st.mu.Unlock()
	if len(covers) == 0 {
		return false
	}
	sort.Slice(covers, func(i, j int) bool {
		return len(cleanKnowledgeWatchPath(covers[i])) < len(cleanKnowledgeWatchPath(covers[j]))
	})
	changed := false
	for _, cover := range covers {
		if !a.knowledgeFolderWatchRecursiveAttached(id, cover) {
			continue
		}
		blocked := knowledgeFolderWatchBlockedRecursive(cover, dataDir)
		if _, ok := blocked[knowledgeFolderWatchDirKey(cover)]; !ok {
			continue
		}
		a.dropKnowledgeFolderWatchDirs(id, cover)
		warn, _, err := a.attachKnowledgeFolderWatchDirs(id, cover, false)
		if err != nil {
			log.Printf("[knowledge-watch] separate %s: %v", cover, err)
			continue
		}
		if warn != "" {
			a.noteKnowledgeFolderWatchDirLimit(id, warn)
		}
		changed = true
	}
	return changed
}

func (a *App) knowledgeFolderWatchRecursiveAttached(id, path string) bool {
	st := a.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	entry, ok := st.dirs[knowledgeFolderWatchDirKey(path)]
	return ok && entry.id == id && entry.recursive
}

func (a *App) dropKnowledgeFolderWatchDirs(id, root string) {
	if a == nil || id == "" || a.knowledgeFolderWatchClosed() {
		return
	}
	st := a.folderWatch()
	st.watchMu.Lock()
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		st.watchMu.Unlock()
		return
	}
	watcher := st.watcher
	paths := make([]string, 0)
	for key, entry := range st.dirs {
		if entry.id != id {
			continue
		}
		if pathEqual(entry.path, root) || knowledgeWatchPathInside(root, entry.path) {
			paths = append(paths, entry.path)
			delete(st.dirs, key)
		}
	}
	st.mu.Unlock()
	for _, dir := range paths {
		if watcher != nil {
			_ = watcher.Remove(dir)
		}
	}
	st.watchMu.Unlock()
}

func (a *App) forgetKnowledgeFolderWatchPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" || a == nil || a.knowledgeFolderWatchClosed() {
		return
	}
	st := a.folderWatch()
	st.watchMu.Lock()
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		st.watchMu.Unlock()
		return
	}
	watcher := st.watcher
	dirs := make([]string, 0)
	for key, entry := range st.dirs {
		if pathEqual(entry.path, path) || knowledgeWatchPathInside(path, entry.path) {
			dirs = append(dirs, entry.path)
			delete(st.dirs, key)
		}
	}
	missing := make([]string, 0)
	for _, item := range st.items {
		if pathEqual(item.Path, path) || knowledgeWatchPathInside(path, item.Path) {
			missing = append(missing, item.ID)
			if bucket := st.pending[item.ID]; bucket != nil {
				if bucket.timer != nil {
					bucket.timer.Stop()
				}
				delete(st.pending, item.ID)
			}
		}
	}
	for id, bucket := range st.pending {
		if bucket == nil || bucket.paths == nil {
			continue
		}
		for file := range bucket.paths {
			if pathEqual(file, path) || knowledgeWatchPathInside(path, file) {
				// Forget is queued when the path is missing, then runs on the
				// directory worker. An editor delete-and-replace schedules the
				// same path before this runs. Drop the import only if the file
				// is still gone.
				if _, statErr := os.Lstat(file); statErr != nil {
					delete(bucket.paths, file)
				}
			}
		}
		if len(bucket.paths) == 0 {
			if bucket.timer != nil {
				bucket.timer.Stop()
			}
			delete(st.pending, id)
		}
	}
	st.mu.Unlock()
	for _, dir := range dirs {
		if watcher == nil {
			break
		}
		_ = watcher.Remove(dir)
	}
	st.watchMu.Unlock()
	for _, id := range missing {
		a.updateKnowledgeFolderWatch(id, func(watch *KnowledgeFolderWatch) {
			watch.Status = knowledgeFolderWatchStatusMissing
			watch.LastError = "folder does not exist"
		})
	}
}

func (a *App) removeWatchedDirs(id string, dirs []string) {
	if len(dirs) == 0 {
		return
	}
	st := a.folderWatch()
	st.watchMu.Lock()
	st.mu.Lock()
	watcher := st.watcher
	kept := make([]string, 0, len(dirs))
	for _, key := range dirs {
		entry, ok := st.dirs[key]
		if ok && entry.id == id {
			delete(st.dirs, key)
			kept = append(kept, entry.path)
		}
	}
	st.mu.Unlock()
	for _, dir := range kept {
		if watcher == nil {
			break
		}
		_ = watcher.Remove(dir)
	}
	st.watchMu.Unlock()
}

func knowledgeFolderWatchDirKey(path string) string {
	return cleanKnowledgeWatchPath(path)
}

func knowledgeFolderWatchContainsID(items []KnowledgeFolderWatch, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func knowledgeFolderWatchDirAttachedLocked(st *knowledgeFolderWatchState, id, root string) bool {
	if st == nil {
		return false
	}
	for _, entry := range st.dirs {
		if entry.id == id && pathEqual(entry.path, root) {
			return true
		}
	}
	return false
}

func (a *App) knowledgeFolderWatchRecursiveCover(id, path string) (knowledgeFolderWatchDir, bool) {
	st := a.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	var best knowledgeFolderWatchDir
	found := false
	for _, entry := range st.dirs {
		if entry.id != id || !entry.recursive {
			continue
		}
		if pathEqual(entry.path, path) || knowledgeWatchPathInside(entry.path, path) {
			if !found || len(cleanKnowledgeWatchPath(entry.path)) > len(cleanKnowledgeWatchPath(best.path)) {
				best = entry
				found = true
			}
		}
	}
	return best, found
}

func (a *App) knowledgeFolderWatchRootAttached(id, root string) bool {
	st := a.folderWatch()
	st.mu.Lock()
	defer st.mu.Unlock()
	return knowledgeFolderWatchDirAttachedLocked(st, id, root)
}

func (a *App) knowledgeFolderWatchKernelKeys() map[string]struct{} {
	keys := map[string]struct{}{}
	if a == nil {
		return keys
	}
	st := a.folderWatch()
	st.mu.Lock()
	watcher := st.watcher
	closed := st.closed
	st.mu.Unlock()
	if closed || watcher == nil {
		return keys
	}
	for _, path := range watcher.WatchList() {
		keys[knowledgeFolderWatchDirKey(path)] = struct{}{}
	}
	return keys
}

func knowledgeFolderWatchKernelMissing(kernel map[string]struct{}, key string) bool {
	if kernel == nil {
		return false
	}
	_, live := kernel[key]
	return !live
}

func knowledgeFolderWatchKernelPath(path string, recurse bool) string {
	if !recurse {
		return path
	}
	return path + string(os.PathSeparator) + "..."
}

// knowledgeFolderWatchBlockedRecursive marks directories that must not use a
// subtree watch. ReadDirectoryChangesW cannot exclude a child, and a subtree
// watch on a parent of the application data directory would observe the
// knowledge database writing itself.
func knowledgeFolderWatchBlockedRecursive(root, dataDir string) map[string]struct{} {
	blocked := map[string]struct{}{}
	if !knowledgeFolderWatchSupportsRecurse() {
		return blocked
	}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil || !entry.IsDir() {
			return nil
		}
		if pathEqual(path, root) || !knowledgeFolderWatchSkipNested(root, path, dataDir) {
			return nil
		}
		parent := filepath.Dir(path)
		for {
			blocked[knowledgeFolderWatchDirKey(parent)] = struct{}{}
			if pathEqual(parent, root) || parent == filepath.Dir(parent) {
				break
			}
			parent = filepath.Dir(parent)
		}
		return filepath.SkipDir
	})
	return blocked
}

func (a *App) loadKnowledgeFolderWatches() ([]KnowledgeFolderWatch, error) {
	data, err := os.ReadFile(a.knowledgeFolderWatchStorePath())
	if err != nil {
		if os.IsNotExist(err) {
			return []KnowledgeFolderWatch{}, nil
		}
		return nil, err
	}
	var file knowledgeFolderWatchFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("read folder watches: %w", err)
	}
	if file.Watches == nil {
		file.Watches = []KnowledgeFolderWatch{}
	}
	return file.Watches, nil
}

func (a *App) saveKnowledgeFolderWatches(items []KnowledgeFolderWatch) error {
	if items == nil {
		items = []KnowledgeFolderWatch{}
	}
	payload, err := json.MarshalIndent(knowledgeFolderWatchFile{Watches: items}, "", "  ")
	if err != nil {
		return err
	}
	path := a.knowledgeFolderWatchStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		if renameErr := os.Rename(tmp, path); renameErr != nil {
			_ = os.Remove(tmp)
			return renameErr
		}
	}
	return nil
}

func canonicalKnowledgeFolderWatchPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("folder path is required")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("folder does not exist")
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a folder")
	}
	return abs, nil
}

func cleanKnowledgeWatchPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if os.PathSeparator == '\\' {
		path = strings.ToLower(path)
	}
	return path
}

func pathEqual(left, right string) bool {
	return cleanKnowledgeWatchPath(left) == cleanKnowledgeWatchPath(right)
}

func knowledgeWatchPathInside(parent, child string) bool {
	parent = cleanKnowledgeWatchPath(parent)
	child = cleanKnowledgeWatchPath(child)
	if parent == "" || child == "" || parent == child {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func knowledgeFolderWatchPathSkipped(root, path, dataDir string) bool {
	cur := filepath.Clean(strings.TrimSpace(path))
	root = filepath.Clean(strings.TrimSpace(root))
	for cur != "" {
		if pathEqual(cur, root) {
			return false
		}
		if knowledgeFolderWatchSkipNested(root, cur, dataDir) {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
	return false
}

func knowledgeFolderWatchSkipNested(root, dir, dataDir string) bool {
	if pathEqual(dir, root) {
		return false
	}
	if dataDir != "" && (pathEqual(dir, dataDir) || knowledgeWatchPathInside(dataDir, dir)) {
		return true
	}
	switch strings.ToLower(filepath.Base(dir)) {
	case ".git", ".hg", ".svn", ".maclaw", "node_modules", "vendor", "__pycache__", ".idea", ".cache":
		return true
	default:
		return false
	}
}

func knowledgeFolderWatchExcludeGlobs() []string {
	return []string{
		".git/**",
		".hg/**",
		".svn/**",
		".maclaw/**",
		"node_modules/**",
		"vendor/**",
		"__pycache__/**",
		".idea/**",
		".cache/**",
	}
}

// knowledgeFolderWatchDataDirExcludeGlob is the import-walk form of the watch
// skip. A parent folder may contain the application data directory. The watch
// walk already steps over it; ImportDirectory only sees exclude globs.
func knowledgeFolderWatchDataDirExcludeGlob(root, dataDir string) string {
	root = filepath.Clean(strings.TrimSpace(root))
	dataDir = filepath.Clean(strings.TrimSpace(dataDir))
	if root == "" || dataDir == "" || pathEqual(root, dataDir) || !knowledgeWatchPathInside(root, dataDir) {
		return ""
	}
	rel, err := filepath.Rel(root, dataDir)
	if err != nil {
		return ""
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ""
	}
	return filepath.ToSlash(rel) + "/**"
}

func knowledgeFolderWatchIgnoredFile(path string) bool {
	base := filepath.Base(path)
	if base == "" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "~$") {
		return true
	}
	lower := strings.ToLower(base)
	switch filepath.Ext(lower) {
	case ".tmp", ".temp", ".swp", ".swo", ".partial", ".crdownload", ".download", ".bak":
		return true
	}
	if strings.HasSuffix(lower, "~") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(base))
	for _, want := range knowledge.DefaultIncludeExts {
		if ext == strings.ToLower(want) {
			return false
		}
	}
	return true
}

func copyKnowledgeFolderWatches(items []KnowledgeFolderWatch) []KnowledgeFolderWatch {
	if len(items) == 0 {
		return []KnowledgeFolderWatch{}
	}
	out := make([]KnowledgeFolderWatch, len(items))
	copy(out, items)
	return out
}

func removeKnowledgeFolderWatchID(items []KnowledgeFolderWatch, id string) []KnowledgeFolderWatch {
	out := make([]KnowledgeFolderWatch, 0, len(items))
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}
