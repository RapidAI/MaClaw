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
)

const formulaDownloadTimeout = 30 * time.Minute

var formulaDownloadMu sync.Mutex

// errFormulaNotReady means the pinned files are absent and a download was started.
var errFormulaNotReady = errors.New("formula model files are not present yet")

// errFormulaRepairing means a size-matched file failed its pin and a repair
// download was started.
var errFormulaRepairing = errors.New("formula model files failed verification")

// errFormulaDownloadBusy means another download already holds the lock.
var errFormulaDownloadBusy = errors.New("formula download already running")

type formulaRuntime struct {
	mu  sync.Mutex
	eng *formula.Engine
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
	if _, err := sharedFormula.open(dir); err != nil {
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

// ensureFormulaReady returns the loaded FormulaNet engine. Disk pins are
// checked on every call. A missing file or a failed pin starts a background
// repair. An engine already loaded from a good copy is still returned, so a
// deleted file does not blank out recognition while the files are fetched
// again. With nothing loaded, the caller gets errFormulaNotReady or
// errFormulaRepairing.
func (a *App) ensureFormulaReady() (*formula.Engine, error) {
	dir, err := formula.DefaultModelDir()
	if err != nil {
		return nil, err
	}
	if err := formula.VerifyLoaded(dir); err != nil {
		if !errors.Is(err, formula.ErrCorrupt) && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		go a.backgroundPreloadFormulaModel()
		if eng := sharedFormula.current(dir); eng != nil {
			return eng, nil
		}
		if errors.Is(err, formula.ErrCorrupt) {
			return nil, errFormulaRepairing
		}
		return nil, errFormulaNotReady
	}
	return sharedFormula.open(dir)
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
	if _, err := sharedFormula.open(dir); err != nil {
		log.Printf("[formula] background preload: %v", err)
	}
}

func (r *formulaRuntime) current(dir string) *formula.Engine {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.eng != nil && r.dir == dir {
		return r.eng
	}
	return nil
}

func (r *formulaRuntime) open(dir string) (*formula.Engine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.eng != nil && r.dir == dir {
		return r.eng, nil
	}
	eng, err := formula.Open(dir)
	if err != nil {
		return nil, err
	}
	r.eng = eng
	r.dir = dir
	return eng, nil
}
