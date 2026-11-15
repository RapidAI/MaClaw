package desktopd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// panelResources is the size an operator typed in the admin panel. Nil means
// containers use the size Hub sends, which was measured at install.
var panelResources atomic.Pointer[desktop.Resources]

func panelResourceChoice() *desktop.Resources {
	return panelResources.Load()
}

func panelResourcePath(stateDir string) string {
	return filepath.Join(strings.TrimSpace(stateDir), "desktop_resources.json")
}

// LoadPanelResources reads a size saved from the admin panel. A missing file
// leaves containers on the size Hub sends.
func LoadPanelResources(stateDir string) {
	if strings.TrimSpace(stateDir) == "" || strings.EqualFold(stateDir, "off") {
		panelResources.Store(nil)
		return
	}
	raw, err := os.ReadFile(panelResourcePath(stateDir))
	if err != nil {
		panelResources.Store(nil)
		return
	}
	var in struct {
		Memory  string `json:"memory"`
		CPUs    string `json:"cpus"`
		ShmSize string `json:"shm_size"`
	}
	if json.Unmarshal(raw, &in) != nil {
		panelResources.Store(nil)
		return
	}
	fitted, err := desktop.NormalizeResources("", in.Memory, in.CPUs, in.ShmSize)
	if err != nil {
		panelResources.Store(nil)
		return
	}
	panelResources.Store(&fitted)
}

// SavePanelResources keeps a size typed in the admin panel and uses it for
// the next desktop container on this machine.
func SavePanelResources(stateDir, memory, cpus, shm string) (desktop.Resources, error) {
	fitted, err := desktop.NormalizeResources("", memory, cpus, shm)
	if err != nil {
		return desktop.Resources{}, err
	}
	dir := strings.TrimSpace(stateDir)
	if dir == "" || strings.EqualFold(dir, "off") {
		return desktop.Resources{}, errors.New("admin state directory is not configured")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return desktop.Resources{}, err
	}
	raw, err := json.Marshal(map[string]string{
		"memory": fitted.Memory, "cpus": fitted.CPUs, "shm_size": fitted.ShmSize,
	})
	if err != nil {
		return desktop.Resources{}, err
	}
	if err := os.WriteFile(panelResourcePath(dir), raw, 0o600); err != nil {
		return desktop.Resources{}, err
	}
	panelResources.Store(&fitted)
	return fitted, nil
}

// shownDesktopResources is the size the admin panel displays. A saved choice
// wins. Otherwise it is the size measured from this machine.
func shownDesktopResources() (memory, cpus, shm, source string) {
	if choice := panelResourceChoice(); choice != nil {
		return choice.Memory, choice.CPUs, choice.ShmSize, "panel"
	}
	memory, cpus, shm = DefaultMemory, DefaultCPUs, DefaultShmSize
	ram, logical := HostCapacity()
	if ram == 0 {
		return memory, cpus, shm, "auto"
	}
	fitted, err := desktop.FitHostResources(ram, logical)
	if err != nil {
		return memory, cpus, shm, "auto"
	}
	return fitted.Memory, fitted.CPUs, fitted.ShmSize, "auto"
}

// ClearPanelResources returns this machine to the size measured at install.
// The in-memory choice stays until the file is actually gone. An empty state
// directory is not a place to delete a file.
func ClearPanelResources(stateDir string) error {
	dir := strings.TrimSpace(stateDir)
	if dir == "" || strings.EqualFold(dir, "off") {
		panelResources.Store(nil)
		return nil
	}
	err := os.Remove(panelResourcePath(dir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	panelResources.Store(nil)
	return nil
}
