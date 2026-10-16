// A stable per-install machine identity for the Qoder device login. The
// official CLI binds approvals to its own machine code; MaClaw keeps a
// locally generated UUID in its config directory instead.
package qoder

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	machineMu       sync.Mutex
	machineCachedID string
)

// MachineID returns the stable per-install device id, creating one on first use.
func MachineID() string {
	machineMu.Lock()
	defer machineMu.Unlock()
	if machineCachedID != "" {
		return machineCachedID
	}
	path := machineIDPath()
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if id := sanitizeMachineID(string(data)); id != "" {
				machineCachedID = id
				return id
			}
		}
	}
	id := newUUID()
	machineCachedID = id
	if path == "" {
		return id
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		_ = os.WriteFile(path, []byte(id), 0o600)
	}
	return id
}

func sanitizeMachineID(raw string) string {
	id := strings.TrimSpace(raw)
	if len(id) > 128 {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return ""
		}
	}
	return id
}

func machineIDPath() string {
	if path := strings.TrimSpace(os.Getenv("QODER_MACHINE_ID_FILE")); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".maclaw", "qoder-machine-id")
	}
	return filepath.Join(home, ".maclaw", "qoder-machine-id")
}

func newUUID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "maclaw-device"
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return uuidFormat(raw)
}
