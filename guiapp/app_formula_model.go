package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/formula"
	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

const formulaDownloadTimeout = 30 * time.Minute

// formulaIdleUnloadDelay releases the FormulaNet graphs after this much
// recognition idle time; the next call reloads them transparently.
// MACLAW_MODEL_IDLE_UNLOAD_SEC overrides it (0 disables idle unload).
const formulaIdleUnloadDelay = 5 * time.Minute

var formulaDownloadMu sync.Mutex

// errFormulaNotReady means the pinned files are absent and a download was started.
var errFormulaNotReady = errors.New("formula model files are not present yet")

// errFormulaRepairing means a size-matched file failed its pin and a repair
// download was started.
var errFormulaRepairing = errors.New("formula model files failed verification")

// errFormulaDownloadBusy means another download already holds the lock.
var errFormulaDownloadBusy = errors.New("formula download already running")

// formulaRuntime owns the shared FormulaNet engine through a modelmanager:
// the graphs load on first acquire and unload after formulaIdleUnloadDelay
// of no use. A new manager is built when the model dir changes.
type formulaRuntime struct {
	mu  sync.Mutex
	mm  *modelmanager.Manager[*formula.Engine]
	dir string
}

var sharedFormula formulaRuntime

// GetFormulaEnabled reports whether FormulaNet is enabled. The model is
// managed from the OCR section of AI model settings and used by
// ocr_recognize when mode is formula.
func (a *App) GetFormulaEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.FormulaEnabled
}

// SetFormulaEnabled persists the formula toggle. Enabling starts the same
// background download used for a missing OCR model.
func (a *App) SetFormulaEnabled(enabled bool) error {
	if _, err := a.PatchConfigFields(map[string]interface{}{"formula_enabled": enabled}); err != nil {
		return err
	}
	if enabled {
		info := a.CheckFormulaModel()
		if exists, _ := info["exists"].(bool); !exists {
			go a.downloadFormulaModelIfStillEnabled()
		}
	}
	return nil
}

// CheckFormulaModel reports whether the pinned FormulaNet files are present.
func (a *App) CheckFormulaModel() map[string]interface{} {
	dir, err := formula.DefaultModelDir()
	if err != nil {
		return map[string]interface{}{"exists": false, "size": int64(0)}
	}
	size, ok := formula.FilesPresent(dir)
	if !ok {
		return map[string]interface{}{"exists": false, "size": int64(0)}
	}
	return map[string]interface{}{"exists": true, "size": size, "model": "formulanet"}
}

// DownloadFormulaModel fetches FormulaNet (GitHub release, then the hub
// mirror) into <Maclaw base>/models/formula and loads it. A download that
// is already running returns nil so the settings panel keeps the progress
// events from that run.
func (a *App) DownloadFormulaModel() error {
	err := a.downloadFormulaModel(true)
	if errors.Is(err, errFormulaDownloadBusy) {
		return nil
	}
	return err
}

func (a *App) downloadFormulaModelIfStillEnabled() error {
	err := a.downloadFormulaModel(false)
	if errors.Is(err, errFormulaDownloadBusy) {
		return nil
	}
	return err
}

func (a *App) downloadFormulaModel(autoEnable bool) error {
	if !formulaDownloadMu.TryLock() {
		return errFormulaDownloadBusy
	}
	dir, err := a.fetchFormulaLocked(autoEnable)
	if err != nil || dir == "" {
		return err
	}
	// Load outside the download lock so a second request is not stuck behind
	// graph init, and a busy caller can tell "already downloading" from "done".
	if err := sharedFormula.warm(dir); err != nil {
		if a != nil {
			a.emitFormulaProgress(0, 0, 0, err.Error())
		}
		return err
	}
	if a != nil {
		a.emitFormulaProgress(100, 0, 0, "")
	}
	return nil
}

// fetchFormulaLocked releases the download lock even if fetch panics.
// Open stays outside the lock so graph init does not block another download.
func (a *App) fetchFormulaLocked(autoEnable bool) (dir string, err error) {
	defer formulaDownloadMu.Unlock()
	return a.fetchFormulaModels(autoEnable)
}

// fetchFormulaModels downloads any missing or pin-mismatched file. An empty
// dir with a nil error means the caller disabled formula while the download
// was in flight, so the engine must not be loaded.
func (a *App) fetchFormulaModels(autoEnable bool) (string, error) {
	dir, err := formula.DefaultModelDir()
	if err != nil {
		return "", fmt.Errorf("formula model dir: %w", err)
	}
	if err := formula.VerifyLoaded(dir); err != nil {
		hubURL := ""
		if a != nil {
			hubURL = a.PeekRemoteHubURLTrimmed()
		}
		ctx, cancel := context.WithTimeout(context.Background(), formulaDownloadTimeout)
		defer cancel()
		err := formula.EnsureModelsProgress(ctx, dir, hubURL, func(downloaded, total int64) {
			pct := 0
			if total > 0 {
				pct = int(downloaded * 100 / total)
			}
			if pct >= 100 {
				pct = 99
			}
			if a != nil {
				a.emitFormulaProgress(pct, downloaded, total, "")
			}
		})
		if err != nil {
			if a != nil {
				a.emitFormulaProgress(0, 0, 0, err.Error())
			}
			return "", err
		}
	}
	if autoEnable {
		if a != nil {
			a.autoEnableFormula()
		}
	} else if a != nil && !a.formulaStillConfiguredEnabled() {
		// Files match the pin. Leave the engine unloaded while formula is
		// off, and publish completion so the settings panel refreshes.
		// A later enable reads that refreshed status and does not keep the
		// spinner up for a download that already finished.
		a.emitFormulaProgress(100, 0, 0, "")
		return "", nil
	}
	return dir, nil
}

func (a *App) autoEnableFormula() {
	_, _ = a.PatchConfigFields(map[string]interface{}{"formula_enabled": true})
}

// formulaProgressSink observes progress in tests. emitEvent is a no-op
// without a Wails lifecycle context.
var formulaProgressSink func(pct int, downloaded, total int64, errMsg string)

func (a *App) emitFormulaProgress(pct int, downloaded, total int64, errMsg string) {
	if formulaProgressSink != nil {
		formulaProgressSink(pct, downloaded, total, errMsg)
	}
	a.emitEvent("formula-download-progress", map[string]interface{}{
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
	})
}

func (a *App) formulaStillConfiguredEnabled() bool {
	cfg, err := a.LoadConfig()
	return err == nil && cfg.FormulaEnabled
}

// ensureFormulaReady returns the loaded FormulaNet engine plus a release
// function the caller must run when done using it (done() schedules the idle
// unload; it never invalidates the returned engine). Disk pins are checked on
// every call. A missing file or a failed pin starts a background repair. An
// engine already loaded from a good copy is still returned, so a deleted file
// does not blank out recognition while the files are fetched again. With
// nothing loaded, the caller gets errFormulaNotReady or errFormulaRepairing.
func (a *App) ensureFormulaReady() (*formula.Engine, func(), error) {
	dir, err := formula.DefaultModelDir()
	if err != nil {
		return nil, nil, err
	}
	if err := formula.VerifyLoaded(dir); err != nil {
		if !errors.Is(err, formula.ErrCorrupt) && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		go a.backgroundPreloadFormulaModel()
		// Acquire rather than peek: the caller holds a lease so the idle
		// timer cannot unload the graphs mid-recognition. When nothing is
		// resident, Open re-fails on the bad pins and we fall through to
		// the not-ready errors below.
		if eng, done, aerr := sharedFormula.acquire(dir); aerr == nil {
			return eng, done, nil
		}
		if errors.Is(err, formula.ErrCorrupt) {
			return nil, nil, errFormulaRepairing
		}
		return nil, nil, errFormulaNotReady
	}
	return sharedFormula.acquire(dir)
}

func (a *App) backgroundPreloadFormulaModel() {
	if !a.formulaStillConfiguredEnabled() {
		return
	}
	dir, err := formula.DefaultModelDir()
	if err != nil {
		return
	}
	// A cached engine is not proof the files are still on disk. open would
	// return that engine and skip the download.
	if err := formula.VerifyLoaded(dir); err != nil {
		if !errors.Is(err, formula.ErrCorrupt) && !errors.Is(err, os.ErrNotExist) {
			log.Printf("[formula] background preload: %v", err)
			return
		}
		log.Printf("[formula] background preload: starting download")
		if err := a.downloadFormulaModel(false); err != nil {
			if errors.Is(err, errFormulaDownloadBusy) {
				return
			}
			log.Printf("[formula] background preload: %v", err)
			return
		}
		log.Printf("[formula] background preload: download complete")
		return
	}
	if err := sharedFormula.warm(dir); err != nil {
		log.Printf("[formula] background preload: %v", err)
	}
}

// acquire returns the loaded engine and a release function (done must run
// when the caller stops using the engine — it schedules the idle unload).
// A dir change swaps in a fresh manager so a stale dir's graphs are dropped.
func (r *formulaRuntime) acquire(dir string) (*formula.Engine, func(), error) {
	r.mu.Lock()
	if r.mm == nil || r.dir != dir {
		if r.mm != nil {
			r.mm.Shutdown()
		}
		d := dir
		r.mm = modelmanager.New(modelmanager.Config[*formula.Engine]{
			Name: "formula",
			Load: func() (*formula.Engine, error) { return formula.Open(d) },
			// Graph memory is Go heap plus onnxrt scratch; the post-unload
			// ReleaseMemory pass returns both to the OS.
			Close:       nil,
			UnloadDelay: formulaIdleUnloadDelay,
		})
		r.dir = dir
	}
	mm := r.mm
	r.mu.Unlock()
	return mm.Acquire()
}

// warm loads the engine now (download/verify callers prove the files open)
// and releases the acquisition; the idle timer unloads it when unused.
func (r *formulaRuntime) warm(dir string) error {
	_, done, err := r.acquire(dir)
	if err != nil {
		return err
	}
	done()
	return nil
}

// current returns the engine already resident for dir, or nil — a peek used
// to keep serving a deleted file's cached engine while it re-downloads.
func (r *formulaRuntime) current(dir string) *formula.Engine {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mm == nil || r.dir != dir {
		return nil
	}
	eng, ok := r.mm.Peek()
	if !ok {
		return nil
	}
	return eng
}
