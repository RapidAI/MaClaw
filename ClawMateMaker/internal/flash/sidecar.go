package flash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const maxSidecarManifestBytes = 256 * 1024

var ErrSidecarInvalid = errors.New("packaged esptool sidecar is unavailable or invalid")

type sidecarConfig struct {
	executable string
	production bool
}

var configuredSidecar struct {
	sync.RWMutex
	sidecarConfig
}

type sidecarManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Tools         []sidecarRecord `json:"tools"`
}

type sidecarRecord struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// ConfigureSidecar is called by the desktop bootstrap. Official builds set
// production=true and therefore accept only the sidecar shipped beside the
// application. Development builds retain the explicit CLAWMATE_ESPTOOL/PATH
// fallback so contributors can work with an ESP-IDF installation.
func ConfigureSidecar(executable string, production bool) {
	configuredSidecar.Lock()
	defer configuredSidecar.Unlock()
	configuredSidecar.executable = executable
	configuredSidecar.production = production
}

func currentSidecarConfig() sidecarConfig {
	configuredSidecar.RLock()
	defer configuredSidecar.RUnlock()
	return configuredSidecar.sidecarConfig
}

func managedTool(executable string) (Tool, error) {
	if executable == "" {
		return Tool{}, fmt.Errorf("%w: application executable path is unavailable", ErrSidecarInvalid)
	}
	root := filepath.Join(filepath.Dir(executable), "tools")
	manifestPath := filepath.Join(root, "sidecar-manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Tool{}, fmt.Errorf("%w: read manifest: %v", ErrSidecarInvalid, err)
	}
	if len(raw) == 0 || len(raw) > maxSidecarManifestBytes {
		return Tool{}, fmt.Errorf("%w: manifest has invalid size", ErrSidecarInvalid)
	}
	var manifest sidecarManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Tool{}, fmt.Errorf("%w: decode manifest: %v", ErrSidecarInvalid, err)
	}
	if manifest.SchemaVersion != 1 {
		return Tool{}, fmt.Errorf("%w: unsupported manifest schema", ErrSidecarInvalid)
	}
	expected := "esptool"
	if runtime.GOOS == "windows" {
		expected += ".exe"
	}
	for _, record := range manifest.Tools {
		if record.Name != "esptool" || record.Path != expected || !validSidecarHash(record.SHA256) {
			continue
		}
		candidate := filepath.Join(root, record.Path)
		if filepath.Clean(candidate) != candidate {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || (info.Mode()&0111 == 0 && runtime.GOOS != "windows") {
			return Tool{}, fmt.Errorf("%w: binary is missing or not executable", ErrSidecarInvalid)
		}
		sum, err := fileSHA256(candidate)
		if err != nil || !strings.EqualFold(record.SHA256, "sha256:"+sum) {
			return Tool{}, fmt.Errorf("%w: binary hash does not match manifest", ErrSidecarInvalid)
		}
		return Tool{Path: candidate, Version: record.Version}, nil
	}
	return Tool{}, fmt.Errorf("%w: no matching esptool record", ErrSidecarInvalid)
}

func validSidecarHash(value string) bool {
	if !strings.HasPrefix(strings.ToLower(value), "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
