package maclawpath

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	defaultBaseOnce sync.Once
	defaultBasePath string

	baseOnce     sync.Once
	baseMu       sync.RWMutex
	basePath     string
	baseExplicit bool
)

// DefaultBaseDir returns the fixed MaClaw config home (~/.maclaw).
func DefaultBaseDir() string {
	defaultBaseOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			defaultBasePath = filepath.Join(".", ".maclaw")
			return
		}
		defaultBasePath = filepath.Join(home, ".maclaw")
	})
	return defaultBasePath
}

// BaseDir returns the effective MaClaw base dir. config.json remains in
// DefaultBaseDir(), while data_dir can move logs/data/runtime payloads.
func BaseDir() string {
	baseOnce.Do(func() {
		baseMu.Lock()
		if !baseExplicit {
			basePath = resolveBaseDir(loadConfiguredDataDir())
		}
		baseMu.Unlock()
	})
	baseMu.RLock()
	defer baseMu.RUnlock()
	if strings.TrimSpace(basePath) == "" {
		return DefaultBaseDir()
	}
	return basePath
}

func SetBaseDirFromConfig(configuredDir string) {
	SetBaseDir(resolveBaseDir(configuredDir))
}

func SetBaseDir(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = DefaultBaseDir()
	}
	baseMu.Lock()
	basePath = dir
	baseExplicit = true
	baseMu.Unlock()
}

func DataDir() string {
	return filepath.Join(BaseDir(), "data")
}

func LogsDir() string {
	return filepath.Join(BaseDir(), "logs")
}

func SkillsDir() string {
	return filepath.Join(DataDir(), "skills")
}

func RuntimeDir() string {
	return filepath.Join(DataDir(), "runtimes")
}

func AppOutputsDir() string {
	return filepath.Join(DataDir(), "app-outputs")
}

func resolveBaseDir(configuredDir string) string {
	dir := strings.TrimSpace(configuredDir)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "[maclawpath] WARNING: configured data_dir %q not accessible (%v), falling back to default\n", dir, err)
			dir = ""
		}
	}
	if dir == "" {
		dir = DefaultBaseDir()
	}
	return dir
}

func loadConfiguredDataDir() string {
	data, err := os.ReadFile(filepath.Join(DefaultBaseDir(), "config.json"))
	if err != nil {
		return ""
	}
	var partial struct {
		DataDir string `json:"data_dir"`
	}
	if json.Unmarshal(data, &partial) != nil {
		return ""
	}
	return strings.TrimSpace(partial.DataDir)
}
