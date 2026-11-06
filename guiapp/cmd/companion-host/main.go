// Command companion-host is the native-messaging launcher Chrome and Edge start.
// It does not import the GUI package. Chrome mode inherits the browser's stdio
// and replaces this process with the GUI's one-shot host. Safari's listen mode
// stays in this binary; until a signed appex exists it exits without pretending
// the Mach connection succeeded.
package main

import (
	"os"
	"path/filepath"
	"strings"
)

const companionPaperHostArg = "companion-paper-host"

// companionHostLaunchHook replaces the GUI handoff in tests.
var companionHostLaunchHook func(gui string, argv []string) (int, error)

// companionGUIPathHook replaces executable lookup in tests.
var companionGUIPathHook func() (string, error)

func main() {
	os.Exit(runCompanionHost(os.Args))
}

func runCompanionHost(args []string) int {
	if chromeExtensionArg(args) {
		return launchChromeMode()
	}
	if hasArg(args, "--companion-host-listen") {
		return listenSafari()
	}
	return 1
}

func chromeExtensionArg(args []string) bool {
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "chrome-extension://") {
			return true
		}
	}
	return false
}

func hasArg(args []string, want string) bool {
	for _, arg := range args[1:] {
		if arg == want {
			return true
		}
	}
	return false
}

func launchChromeMode() int {
	gui, err := companionGUIPath()
	if err != nil || strings.TrimSpace(gui) == "" {
		return 1
	}
	argv := []string{gui, companionPaperHostArg}
	if companionHostLaunchHook != nil {
		code, hookErr := companionHostLaunchHook(gui, argv)
		if hookErr != nil {
			return 1
		}
		return code
	}
	return launchCompanionGUI(gui)
}

func companionGUIPath() (string, error) {
	if companionGUIPathHook != nil {
		return companionGUIPathHook()
	}
	if env := strings.TrimSpace(os.Getenv("MACLAW_COMPANION_GUI")); env != "" {
		return env, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	if gui := macOSBundleGUI(filepath.Dir(exe)); gui != "" {
		return gui, nil
	}
	if gui := siblingGUI(exe); gui != "" {
		return gui, nil
	}
	return "", os.ErrNotExist
}

// siblingGUI finds the product executable beside the helper. The install
// directory is named for the product, so MaClaw\maclaw-companion-host.exe
// pairs with MaClaw\MaClaw.exe.
func siblingGUI(helper string) string {
	dir := filepath.Dir(helper)
	name := filepath.Base(dir)
	if name == "MacOS" || name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	candidate := filepath.Join(dir, name)
	if !strings.EqualFold(filepath.Ext(candidate), ".exe") && fileExists(candidate+".exe") {
		candidate += ".exe"
	}
	if fileExists(candidate) && !sameFile(candidate, helper) {
		return candidate
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info != nil && !info.IsDir()
}

func sameFile(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(a, b)
	}
	return strings.EqualFold(aa, bb)
}
