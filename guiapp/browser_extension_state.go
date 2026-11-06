package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// browserExtensionStateDirHook redirects the hello/register file in tests.
var browserExtensionStateDirHook func() (string, error)

type browserExtensionBrowserRecord struct {
	Version                  string `json:"version,omitempty"`
	HelloAt                  string `json:"helloAt,omitempty"`
	RegisteredAt             string `json:"registeredAt,omitempty"`
	BrowserRunningAtRegister bool   `json:"browserRunningAtRegister,omitempty"`
	// InstallMode is "unpacked" after the tools card drops the packed CRX, or "store".
	InstallMode string `json:"installMode,omitempty"`
}

type browserExtensionStateFile struct {
	Chrome *browserExtensionBrowserRecord `json:"chrome,omitempty"`
	Edge   *browserExtensionBrowserRecord `json:"edge,omitempty"`
}

func (s *browserExtensionStateFile) record(browser string) *browserExtensionBrowserRecord {
	if s == nil {
		return nil
	}
	switch browser {
	case "chrome":
		return s.Chrome
	case "edge":
		return s.Edge
	default:
		return nil
	}
}

func (s *browserExtensionStateFile) setRecord(browser string, rec *browserExtensionBrowserRecord) {
	if s == nil {
		return
	}
	switch browser {
	case "chrome":
		s.Chrome = rec
	case "edge":
		s.Edge = rec
	}
}

func browserExtensionStateDir() (string, error) {
	if browserExtensionStateDirHook != nil {
		return browserExtensionStateDirHook()
	}
	dir := filepath.Join((*App)(nil).GetDataDir(), "file-companion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func withBrowserExtensionStateLock(dir string, fn func() error) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, "browser-extension-state.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := lockBrowserExtensionState(file); err != nil {
		return err
	}
	defer unlockBrowserExtensionState(file)
	return fn()
}

func readBrowserExtensionState(dir string) browserExtensionStateFile {
	if strings.TrimSpace(dir) == "" {
		return browserExtensionStateFile{}
	}
	var state browserExtensionStateFile
	// Status reads import the legacy file. That import replaces every row, so
	// it has to take the same lock as hello and install.
	_ = withBrowserExtensionStateLock(dir, func() error {
		state, _ = loadBrowserExtensionState(dir)
		return nil
	})
	return state
}

func writeBrowserExtensionState(dir string, state browserExtensionStateFile) error {
	if strings.TrimSpace(dir) == "" {
		return os.ErrInvalid
	}
	return withBrowserExtensionStateLock(dir, func() error {
		return saveBrowserExtensionState(dir, state, false)
	})
}

// recordBrowserExtensionHello updates version and helloAt for chrome or edge.
// registeredAt and browserRunningAtRegister are left as they were.
func recordBrowserExtensionHello(browser, version string) error {
	dir, err := browserExtensionStateDir()
	if err != nil {
		return err
	}
	return updateBrowserExtensionState(dir, func(state *browserExtensionStateFile) error {
		rec := state.record(browser)
		if rec == nil {
			rec = &browserExtensionBrowserRecord{}
			state.setRecord(browser, rec)
		}
		rec.Version = version
		rec.HelloAt = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}
