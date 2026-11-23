//go:build windows

package accessibility

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

// errNoInstalledApp means the hint is not a desktop app, or this machine has
// no Start Menu shortcut for it. Focus then keeps the original not-found error.
var errNoInstalledApp = errors.New("no installed application")

// appLaunchWait is how long focus waits for a window after opening the app.
var appLaunchWait = 8 * time.Second

var (
	shell32Focus      = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32Focus.NewProc("ShellExecuteW")
)

const swShowNormal = 1

// launchDesktopApp opens the installed application named by hint.
// The shortcut display name is the match. The executable path is not searched
// and is not hardcoded. Tests never start a program.
func launchDesktopApp(hint string) error {
	if testing.Testing() {
		return errNoInstalledApp
	}
	app, ok := computeruse.DesktopAppFromHint(hint)
	if !ok {
		return errNoInstalledApp
	}
	path, ok := installedShortcut(app)
	if !ok {
		return errNoInstalledApp
	}
	log.Printf("[computer-use] focus %q: opening installed application %s", hint, path)
	return shellOpen(path)
}

func waitForAppFrame(hint string) (visibleWindow, bool) {
	return waitForAppFrameMatching(hint, func(visibleWindow) bool { return true })
}

func waitForVisibleAppFrame(hint string) (visibleWindow, bool) {
	return waitForAppFrameMatching(hint, func(frame visibleWindow) bool { return frame.visible })
}

func waitForAppFrameMatching(hint string, accept func(visibleWindow) bool) (visibleWindow, bool) {
	deadline := time.Now().Add(appLaunchWait)
	for {
		if picked, ok := pickBestVisible(hint); ok && accept(picked) {
			return picked, true
		}
		if appLaunchWait <= 0 || time.Now().After(deadline) {
			return visibleWindow{}, false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func installedShortcut(app string) (string, bool) {
	var names []string
	byName := map[string]string{}
	for _, dir := range shortcutDirs() {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".lnk") {
				return nil
			}
			name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			if _, seen := byName[name]; seen {
				return nil
			}
			byName[name] = path
			names = append(names, name)
			return nil
		})
	}
	chosen, ok := computeruse.ChooseAppLaunchName(app, names)
	if !ok {
		return "", false
	}
	path, ok := byName[chosen]
	return path, ok && path != ""
}

func shortcutDirs() []string {
	var dirs []string
	add := func(root, rel string) {
		if root == "" {
			return
		}
		dir := filepath.Join(root, rel)
		st, err := os.Stat(dir)
		if err == nil && st.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	add(os.Getenv("ProgramData"), filepath.Join("Microsoft", "Windows", "Start Menu", "Programs"))
	add(os.Getenv("APPDATA"), filepath.Join("Microsoft", "Windows", "Start Menu", "Programs"))
	add(os.Getenv("PUBLIC"), "Desktop")
	add(os.Getenv("USERPROFILE"), "Desktop")
	return dirs
}

func shellOpen(path string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, callErr := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, swShowNormal)
	if r > 32 {
		return nil
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return callErr
	}
	return errors.New("could not open the installed application")
}
