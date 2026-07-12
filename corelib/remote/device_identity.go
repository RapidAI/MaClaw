package remote

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const durableDeviceKeyFileName = "device_key"

var (
	deviceKeyMu       sync.Mutex
	cachedDeviceKey   string
	deviceKeyPathHook func() string // tests may override
)

// DurableDeviceKeyPath returns the preferred OS-level path used to persist the
// desktop device key across app reinstalls and user data directory wipes.
//
// Windows:  %ProgramData%\MaClaw\device_key  (survives uninstall of per-user app data)
//
//	fallback: %LOCALAPPDATA%\MaClawMachine\device_key when ProgramData is not writable
//
// macOS:    ~/Library/Application Support/MaClawMachine/device_key
// Linux:    $XDG_DATA_HOME/maclaw-machine/device_key or ~/.local/share/maclaw-machine/device_key
func DurableDeviceKeyPath() string {
	dirs := durableDeviceKeyDirs()
	if len(dirs) == 0 {
		return durableDeviceKeyFileName
	}
	return filepath.Join(dirs[0], durableDeviceKeyFileName)
}

func durableDeviceKeyDir() string {
	dirs := durableDeviceKeyDirs()
	if len(dirs) == 0 {
		return "."
	}
	return dirs[0]
}

// durableDeviceKeyDirs returns candidate directories in priority order.
// Read checks every dir; write tries each until one succeeds.
func durableDeviceKeyDirs() []string {
	if deviceKeyPathHook != nil {
		return []string{deviceKeyPathHook()}
	}
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		base := strings.TrimSpace(os.Getenv("PROGRAMDATA"))
		if base == "" {
			base = `C:\ProgramData`
		}
		dirs = append(dirs, filepath.Join(base, "MaClaw"))
		// User-writable fallback when ProgramData is locked down by policy.
		if la := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); la != "" {
			fb := filepath.Join(la, "MaClawMachine")
			if !samePathFold(fb, dirs[0]) {
				dirs = append(dirs, fb)
			}
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		if home == "" {
			dirs = append(dirs, filepath.Join(".", "MaClawMachine"))
		} else {
			// Keep outside the app's normal data tree so wiping app data
			// does not destroy machine identity.
			dirs = append(dirs, filepath.Join(home, "Library", "Application Support", "MaClawMachine"))
		}
	default:
		if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
			dirs = append(dirs, filepath.Join(xdg, "maclaw-machine"))
		} else {
			home, _ := os.UserHomeDir()
			if home == "" {
				dirs = append(dirs, filepath.Join(".", "maclaw-machine"))
			} else {
				dirs = append(dirs, filepath.Join(home, ".local", "share", "maclaw-machine"))
			}
		}
	}
	return dirs
}

func samePathFold(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// LoadOrCreateDeviceKey returns a stable per-machine client/device key.
// Preference order:
//  1. in-process cache
//  2. durable OS-level store (any candidate path)
//  3. generate UUID, persist to durable store, return
func LoadOrCreateDeviceKey() string {
	deviceKeyMu.Lock()
	defer deviceKeyMu.Unlock()
	if key := strings.TrimSpace(cachedDeviceKey); key != "" {
		return key
	}
	if key, ok := readDurableDeviceKeyLocked(); ok {
		cachedDeviceKey = key
		return key
	}
	key := GenerateClientID()
	_ = writeDurableDeviceKeyLocked(key)
	cachedDeviceKey = key
	return key
}

// EnsureDeviceKey returns preferred when non-empty (and seeds the durable
// store so a later data wipe can recover), otherwise LoadOrCreateDeviceKey.
// Config/explicit preferred wins so restored settings and enroll ClientID stay
// authoritative; reinstall recovery uses LoadOrCreateDeviceKey when preferred
// is empty.
func EnsureDeviceKey(preferred string) string {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return LoadOrCreateDeviceKey()
	}
	deviceKeyMu.Lock()
	defer deviceKeyMu.Unlock()
	cachedDeviceKey = preferred
	// Best-effort upgrade: seed durable store so a later data wipe can recover.
	if existing, ok := readDurableDeviceKeyLocked(); !ok || existing != preferred {
		_ = writeDurableDeviceKeyLocked(preferred)
	}
	return preferred
}

// ReadDurableDeviceKey returns the durable key without creating one.
func ReadDurableDeviceKey() (string, bool) {
	deviceKeyMu.Lock()
	defer deviceKeyMu.Unlock()
	if key := strings.TrimSpace(cachedDeviceKey); key != "" {
		return key, true
	}
	key, ok := readDurableDeviceKeyLocked()
	if ok {
		cachedDeviceKey = key
	}
	return key, ok
}

func readDurableDeviceKeyLocked() (string, bool) {
	dirs := durableDeviceKeyDirs()
	for i, dir := range dirs {
		path := filepath.Join(dir, durableDeviceKeyFileName)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			continue
		}
		// Promote fallback-path keys into higher-priority dirs when possible
		// (e.g. ProgramData becomes writable again after a policy change).
		if i > 0 {
			for _, primary := range dirs[:i] {
				_ = writeDeviceKeyToDir(primary, key)
			}
		}
		return key, true
	}
	return "", false
}

func writeDurableDeviceKeyLocked(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return os.ErrInvalid
	}
	var last error
	for _, dir := range durableDeviceKeyDirs() {
		if err := writeDeviceKeyToDir(dir, key); err != nil {
			last = err
			continue
		}
		return nil
	}
	if last != nil {
		return last
	}
	return os.ErrInvalid
}

func writeDeviceKeyToDir(dir, key string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, durableDeviceKeyFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		if err2 := os.Rename(tmp, path); err2 != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return nil
}

// ResetDeviceKeyCacheForTest clears the in-process cache (tests only).
func ResetDeviceKeyCacheForTest() {
	deviceKeyMu.Lock()
	cachedDeviceKey = ""
	deviceKeyMu.Unlock()
}

// SetDurableDeviceKeyDirForTest overrides the durable directory (tests only).
func SetDurableDeviceKeyDirForTest(dir string) {
	deviceKeyMu.Lock()
	if dir == "" {
		deviceKeyPathHook = nil
	} else {
		deviceKeyPathHook = func() string { return dir }
	}
	cachedDeviceKey = ""
	deviceKeyMu.Unlock()
}
