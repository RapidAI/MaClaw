package browser

import (
	"strings"
	"time"
)

const (
	// parkFreshFor is how long a new window is polled quickly. Chrome creates
	// the HWND after the process starts and first clamps it onto a monitor.
	parkFreshFor = 4 * time.Second
	// parkProcessGoneAfter drops a watched profile once its processes have
	// been gone this long. Otherwise the loop snapshots every chrome.exe for
	// the life of the app.
	parkProcessGoneAfter = 30 * time.Second
)

// parkPIDCacheAge is how long a process-list snapshot stays usable. A window
// that just appeared, or one still on a monitor, needs a fresh list. A parked
// window does not.
func parkPIDCacheAge(fresh, busy bool) time.Duration {
	if fresh || busy {
		return 80 * time.Millisecond
	}
	return 2 * time.Second
}

// parkPollWait is the pause between HWND checks. No watched profile waits a
// full second and does not snapshot processes. A parked window is checked
// less often than one that is still on a monitor.
func parkPollWait(watching int, fresh, busy bool) time.Duration {
	if watching == 0 {
		return time.Second
	}
	if fresh || busy {
		return 15 * time.Millisecond
	}
	return 400 * time.Millisecond
}

// parkWatchExpired reports that a profile's processes have been missing long
// enough to stop watching it. The first miss only starts the timer, so a slow
// startup is not dropped.
func parkWatchExpired(missSince, now time.Time) bool {
	if missSince.IsZero() {
		return false
	}
	return !now.Before(missSince.Add(parkProcessGoneAfter))
}

// shownParkWidth is the width the window will occupy once it is shown.
// A minimized rectangle is the icon, and a maximized one is the monitor.
// Neither is the size that comes back onto the desktop.
func shownParkWidth(shownWidth, normalWidth int, iconicOrZoomed bool) int {
	if iconicOrZoomed {
		if normalWidth >= 400 {
			return normalWidth
		}
		return 1280
	}
	if shownWidth > 0 {
		return shownWidth
	}
	return 1280
}

// ensureManagedBrowserParked keeps a headed window this process owns off the
// desktop while the preview is on. Minimize and SW_HIDE are not used: both
// make document.visibilityState hidden, and the next click then calls
// Page.bringToFront. A missing preview, or the person's own Chrome, is left
// where it is.
func ensureManagedBrowserParked(userDataDir string) {
	if !managedWindowShouldPark(userDataDir) {
		return
	}
	startManagedBrowserParkLoop(userDataDir)
}

// parkRestoreTarget is the window that should take the keyboard back when
// the parked browser is in front. remembered was captured before the browser
// started. behind is the next real window in Z-order, used when that capture
// missed. own is this process's window, used when nothing else is left.
func parkRestoreTarget(foreground, remembered, behind, own uintptr, foregroundIsOurs bool) uintptr {
	if foreground == 0 || !foregroundIsOurs {
		return 0
	}
	if remembered != 0 && remembered != foreground {
		return remembered
	}
	if behind != 0 && behind != foreground {
		return behind
	}
	if own != 0 && own != foreground {
		return own
	}
	return 0
}

// parkFocusCandidate reports a window that can take the keyboard. Tool
// windows and no-activate windows sit in the Z-order without being what the
// person was using. A minimized window is not a target: activating it
// restores that window onto a monitor. The parked browser is rejected by
// the caller as ours.
func parkFocusCandidate(exStyle uintptr, visible, ours, hasArea, iconic bool) bool {
	if ours || !visible || !hasArea || iconic {
		return false
	}
	const tool = 0x00000080
	const noActivate = 0x08000000
	return exStyle&tool == 0 && exStyle&noActivate == 0
}

// commandLineHasProfile reports a process command line that opened this
// profile directory. A longer directory that only shares the prefix does not
// match.
func commandLineHasProfile(commandLine, userDataDir string) bool {
	needle := strings.ToLower(strings.TrimSpace(userDataDir))
	if needle == "" {
		return false
	}
	cmd := strings.ToLower(commandLine)
	from := 0
	for from < len(cmd) {
		i := strings.Index(cmd[from:], needle)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(needle)
		if end >= len(cmd) {
			return true
		}
		switch cmd[end] {
		case '\\', '/', '"', '\'', ' ', '\t':
			return true
		default:
			from = end
		}
	}
	return false
}
