//go:build windows

package guiapp

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	registryVSInstallOnce   sync.Once
	registryVSInstallCached []string
)

// registryVSInstallPaths reads Visual Studio install locations from the
// SxS VS7 keys and from Uninstall entries whose display name is Visual Studio.
// vswhere is preferred; this covers a machine where vswhere is missing.
func registryVSInstallPaths() []string {
	registryVSInstallOnce.Do(func() {
		registryVSInstallCached = loadRegistryVSInstallPaths()
	})
	return registryVSInstallCached
}

func loadRegistryVSInstallPaths() []string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		path = strings.Trim(strings.TrimSpace(path), `"`)
		path = strings.TrimRight(path, `\/`)
		if path == "" {
			return
		}
		key := strings.ToLower(path)
		if seen[key] {
			return
		}
		fi, err := os.Stat(path)
		if err != nil || !fi.IsDir() {
			return
		}
		seen[key] = true
		out = append(out, path)
	}
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		readRegistryStringValues(root, `SOFTWARE\WOW6432Node\Microsoft\VisualStudio\SxS\VS7`, add)
		readRegistryStringValues(root, `SOFTWARE\Microsoft\VisualStudio\SxS\VS7`, add)
	}
	for _, path := range []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subs, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			continue
		}
		for _, sub := range subs {
			sk, err := registry.OpenKey(registry.LOCAL_MACHINE, path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			name, _, _ := sk.GetStringValue("DisplayName")
			loc, _, locErr := sk.GetStringValue("InstallLocation")
			sk.Close()
			lower := strings.ToLower(name)
			if locErr != nil || !strings.Contains(lower, "visual studio") || strings.Contains(lower, "visual studio code") {
				continue
			}
			add(loc)
		}
	}
	return out
}

func readRegistryStringValues(root registry.Key, path string, add func(string)) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer key.Close()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return
	}
	for _, name := range names {
		value, _, err := key.GetStringValue(name)
		if err != nil {
			continue
		}
		add(value)
	}
}

func windowsSDKRootFromRegistry() string {
	for _, path := range []string{
		`SOFTWARE\WOW6432Node\Microsoft\Windows Kits\Installed Roots`,
		`SOFTWARE\Microsoft\Windows Kits\Installed Roots`,
	} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		root, _, err := key.GetStringValue("KitsRoot10")
		key.Close()
		if err != nil {
			continue
		}
		root = strings.TrimRight(strings.TrimSpace(root), `\/`)
		if root != "" {
			return root
		}
	}
	return ""
}

func probeWindowsMSVCFromRegistry() *windowsMSVCToolchain {
	installs := make([]vsInstallRef, 0, len(registryVSInstallPaths()))
	for _, dir := range registryVSInstallPaths() {
		installs = append(installs, vsInstallRef{path: dir})
	}
	return newestWindowsMSVCToolchain(installs)
}

// windowsLogonPathDirs is the machine and user Path a new logon shell would
// search. It is read on each coding bash: a compiler installed after this
// process started is on the next command, not stuck behind the launch PATH.
func windowsLogonPathDirs() []string {
	return loadWindowsLogonPathDirs()
}

func loadWindowsLogonPathDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(block string) {
		block = os.ExpandEnv(block)
		for _, dir := range strings.Split(block, ";") {
			dir = strings.Trim(strings.TrimSpace(dir), `"`)
			if dir == "" {
				continue
			}
			dir = filepath.Clean(dir)
			key := strings.ToLower(dir)
			if seen[key] {
				continue
			}
			seen[key] = true
			dirs = append(dirs, dir)
		}
	}
	if machine, ok := registryPathValue(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`); ok {
		add(machine)
	}
	if user, ok := registryPathValue(registry.CURRENT_USER, `Environment`); ok {
		add(user)
	}
	return dirs
}

var windowsDriveLocal sync.Map

var procWideCharToMultiByte = windows.NewLazySystemDLL("kernel32.dll").NewProc("WideCharToMultiByte")

// errWindowsBatNotEncodable means the vcvars path contains a character the
// ANSI code page cannot store. Capture stops and the layout fallback runs.
var errWindowsBatNotEncodable = errors.New("vcvars path is not representable in the ANSI code page")

// writeWindowsCmdBat writes a batch file in the ANSI code page. cmd.exe reads
// batch files that way: on code page 936 a UTF-8 file makes call fail, and a
// UTF-16 file is executed as mojibake. /u on the command only changes how
// `set` writes stdout.
func writeWindowsCmdBat(w io.Writer, script string) error {
	encoded, err := encodeWindowsACP(script)
	if err != nil {
		return err
	}
	_, err = w.Write(encoded)
	return err
}

func encodeWindowsACP(script string) ([]byte, error) {
	if script == "" {
		return nil, nil
	}
	utf16s, err := windows.UTF16FromString(script)
	if err != nil {
		return nil, err
	}
	if len(utf16s) > 0 && utf16s[len(utf16s)-1] == 0 {
		utf16s = utf16s[:len(utf16s)-1]
	}
	if len(utf16s) == 0 {
		return nil, nil
	}
	const cpACP = 0
	const wcNoBestFitChars = 0x00000400
	size, used, err := wideCharToMultiByte(cpACP, wcNoBestFitChars, utf16s, nil)
	if err != nil {
		return nil, err
	}
	if used {
		return nil, errWindowsBatNotEncodable
	}
	buf := make([]byte, size)
	if _, used, err = wideCharToMultiByte(cpACP, wcNoBestFitChars, utf16s, buf); err != nil {
		return nil, err
	}
	if used {
		return nil, errWindowsBatNotEncodable
	}
	return buf, nil
}

func wideCharToMultiByte(codePage, flags uint32, src []uint16, dst []byte) (int, bool, error) {
	var written uintptr
	var callErr error
	var used int32
	var dstPtr uintptr
	var dstLen uintptr
	if len(dst) > 0 {
		dstPtr = uintptr(unsafe.Pointer(&dst[0]))
		dstLen = uintptr(len(dst))
	}
	written, _, callErr = procWideCharToMultiByte.Call(
		uintptr(codePage),
		uintptr(flags),
		uintptr(unsafe.Pointer(&src[0])),
		uintptr(len(src)),
		dstPtr,
		dstLen,
		0,
		uintptr(unsafe.Pointer(&used)),
	)
	if written == 0 {
		if callErr == nil || callErr == windows.ERROR_SUCCESS {
			return 0, false, errors.New("WideCharToMultiByte failed")
		}
		return 0, false, callErr
	}
	return int(written), used != 0, nil
}

// windowsToolPathIsLocal reports whether os.Stat on path stays on a local
// volume. UNC paths and remote or CD-ROM drives are not local: CreateFile
// on a disconnected redirector does not return. A missing or unknown drive
// is skipped for this call and is not remembered, so a volume that appears
// later can still be searched.
func windowsToolPathIsLocal(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`) {
		return false
	}
	vol := filepath.VolumeName(path)
	if vol == "" {
		return true
	}
	if !strings.HasSuffix(vol, ":") {
		return false
	}
	return windowsDriveLetterIsLocal(vol)
}

func windowsDriveLetterIsLocal(vol string) bool {
	key := strings.ToUpper(vol)
	if v, ok := windowsDriveLocal.Load(key); ok {
		return v.(bool)
	}
	root, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return false
	}
	local, remember := windowsDriveKindIsLocal(windows.GetDriveType(root))
	if remember {
		windowsDriveLocal.Store(key, local)
	}
	return local
}

// windowsDriveKindIsLocal classifies a GetDriveType result. Remote and
// CD-ROM stay non-local for the process lifetime. A missing or unknown
// volume is non-local only for this call: the letter can show up later.
func windowsDriveKindIsLocal(kind uint32) (local, remember bool) {
	switch kind {
	case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_RAMDISK:
		return true, true
	case windows.DRIVE_REMOTE, windows.DRIVE_CDROM:
		return false, true
	default:
		return false, false
	}
}

func registryPathValue(root registry.Key, path string) (string, bool) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Path")
	if err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}
