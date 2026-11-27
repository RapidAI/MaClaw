package guiapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errInstallerElevationCancelled marks the one launch failure the user
// intentionally caused at the OS consent prompt. Deferred installs must not
// retry that failure automatically, or every retry would become another UAC
// prompt the user already declined.
var errInstallerElevationCancelled = errors.New("installer elevation was declined")

// Installer silent-install switches.
//
// Online update is the only caller: the app downloads the installer, quits,
// and lets the installer run unattended.
//
// The release installer is built by NSIS (build/windows/installer/multiarch.nsi),
// and NSIS has exactly one silent switch: /S. The Inno Setup family of switches
// (/VERYSILENT, /SUPPRESSMSGBOXES, /NOCANCEL, /NORESTART) is not recognised
// here — NSIS ignores them and runs the full interactive wizard, which is why
// an online update used to show the welcome/directory/finish pages. Do not
// reintroduce them.
//
// With /S the installer script declares /SD IDYES on every MessageBox it can
// reach (see multiarch.nsi), so those questions resolve to "yes" instead of
// blocking: stop the running app, and replace the previous build. The script
// also skips the uninstall-then-install dance for silent runs and installs in
// place, so an update never deletes the WebView2 profile that holds the user's
// session and UI state.
//
// These switches are only ever passed to a file this process downloaded and
// hash-verified in the same session (see authorizeInstallerLaunch), so an
// unknown installer silently ignoring them degrades to a normal install rather
// than to an unchecked elevated launch.
const windowsInstallerSilentArgs = "/S"

// Deliberately no /D= switch: the installer must keep the directory it reads
// from the existing uninstall key (custom install paths are preserved there).
// Pinning a directory here would install a second copy next to the running
// one on every machine that is not on the default path.
//
// windowsInstallerSilentArgs is split once for exec.Command-style callers that
// must not go through a shell.
var windowsInstallerSilentArgList = strings.Fields(windowsInstallerSilentArgs)

// sanitizeInstallerFileName accepts only a bare file name for the downloaded
// installer.
//
// The name is joined with the Downloads folder, and that exact path is what
// LaunchInstallerAndExit later runs silently (and on Windows, elevated). A
// name carrying a path component would let a crafted manifest point both the
// download destination and the elevated launch at a directory of its choosing,
// so anything but a plain file name is rejected rather than quietly cleaned.
func sanitizeInstallerFileName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("installer file name is empty")
	}
	base := filepath.Base(filepath.FromSlash(trimmed))
	if base != trimmed || base == "." || base == ".." {
		return "", fmt.Errorf("installer file name must be a plain file name: %q", name)
	}
	// Second belt for platforms where Base keeps the whole string.
	if strings.ContainsAny(base, `/\`) {
		return "", fmt.Errorf("installer file name must not contain a path separator: %q", name)
	}
	return base, nil
}

// verifiedInstaller is the installer this process downloaded and checked.
type verifiedInstaller struct {
	// Path is the absolute destination the download wrote to.
	Path string
	// SHA256 is the manifest hash when the release provided one.
	SHA256 string
	// Info is the stat captured right after the download. It is the fallback
	// identity check when the manifest carried no hash.
	Info os.FileInfo
}

// verifiedInstallerKey canonicalizes an absolute installer path for map
// lookup. Windows paths compare case-insensitively; POSIX paths do not.
func verifiedInstallerKey(absPath string) string {
	key := filepath.Clean(absPath)
	if pathCaseInsensitive() {
		key = strings.ToLower(key)
	}
	return key
}

// rememberVerifiedInstaller records the installer produced by a successful
// download. LaunchInstallerAndExit runs silent (and on Windows possibly
// elevated) installs, so it must never be pointed at a file this process did
// not fetch and verify itself. Entries are keyed by canonical path: a later
// download of a different target must not revoke the launch authorization of
// an installer the user is still deciding on.
func (a *App) rememberVerifiedInstaller(path, sha256 string) {
	if a == nil || strings.TrimSpace(path) == "" {
		return
	}
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		a.log(fmt.Sprintf("[update-install] cannot resolve downloaded installer %q: %v", path, err))
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		// Without a stat we have no fallback identity, and a launch that
		// cannot be authorized is safer than one authorized by path alone.
		a.log(fmt.Sprintf("[update-install] cannot stat downloaded installer %q: %v", abs, err))
		return
	}
	a.verifiedInstallerMu.Lock()
	if a.verifiedInstallers == nil {
		a.verifiedInstallers = make(map[string]*verifiedInstaller)
	}
	a.verifiedInstallers[verifiedInstallerKey(abs)] = &verifiedInstaller{
		Path:   abs,
		SHA256: strings.TrimSpace(strings.ToLower(sha256)),
		Info:   info,
	}
	a.verifiedInstallerMu.Unlock()
}

// forgetVerifiedInstaller drops the launch authorization for one installer
// path, for example when a fresh download is about to truncate that file.
func (a *App) forgetVerifiedInstaller(path string) {
	if a == nil || strings.TrimSpace(path) == "" {
		return
	}
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return
	}
	a.verifiedInstallerMu.Lock()
	delete(a.verifiedInstallers, verifiedInstallerKey(abs))
	a.verifiedInstallerMu.Unlock()
}

// authorizeInstallerLaunch re-checks an installer path before it is executed
// silently. It returns the absolute path to run.
//
// The check is deliberately strict: a silent, elevated install of an
// arbitrary local file would be a privilege escalation, so the path must be
// the exact file this process downloaded, its extension must be a real
// installer type for the platform, and its content must still match what was
// verified at download time (or still be the same file when the manifest had
// no hash). This narrows the download-to-launch TOCTOU window: a swap after
// this check still races the launch itself, which no user-mode check can
// close, but the common case (installer replaced while the user decides) is
// caught.
func (a *App) authorizeInstallerLaunch(installerPath string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("application is not initialized")
	}
	trimmed := strings.TrimSpace(installerPath)
	if trimmed == "" {
		return "", fmt.Errorf("installer path is empty")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid installer path: %w", err)
	}
	if !installerExtensionAllowed(abs) {
		return "", fmt.Errorf("refusing to run a non-installer file: %s", abs)
	}

	a.verifiedInstallerMu.Lock()
	record := a.verifiedInstallers[verifiedInstallerKey(abs)]
	a.verifiedInstallerMu.Unlock()
	if record == nil || record.Path == "" {
		return "", fmt.Errorf("refusing to run an installer that was not downloaded and verified in this session")
	}
	if !installerPathsEqual(abs, record.Path) {
		return "", fmt.Errorf("installer path does not match the verified download: %s", abs)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("installer file not accessible: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("installer path is a directory: %s", abs)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("installer file is empty: %s", abs)
	}

	if record.SHA256 != "" {
		if err := verifySHA256File(abs, record.SHA256); err != nil {
			return "", fmt.Errorf("installer changed after verification: %w", err)
		}
		return abs, nil
	}
	// No manifest hash to compare against: fall back to file identity plus the
	// size/mtime captured at verification so an in-place rewrite is rejected
	// too. This still cannot close the final launch TOCTOU, but it is strictly
	// stronger than inode identity alone when no content hash exists.
	if record.Info == nil {
		return "", fmt.Errorf("verified installer has no file identity: %s", abs)
	}
	if !os.SameFile(record.Info, info) {
		return "", fmt.Errorf("installer changed after verification: %s", abs)
	}
	if info.Size() != record.Info.Size() || !info.ModTime().Equal(record.Info.ModTime()) {
		return "", fmt.Errorf("installer changed after verification: %s", abs)
	}
	a.log("[update-install] manifest carried no sha256; launching verified download by path/file identity")
	return abs, nil
}

// installerPathsEqual compares two absolute installer paths. Windows paths are
// case-insensitive; POSIX paths are not.
func installerPathsEqual(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if pathCaseInsensitive() && strings.EqualFold(left, right) {
		return true
	}
	return left == right
}

// cleanupStaleVersionedInstallers removes version-qualified installer files
// left in the downloads folder by earlier update attempts. Version-qualified
// names are only ever created by this update flow (server assets themselves
// are unversioned), so they are safe to garbage-collect once no live decision
// can still use them; the fixed unversioned name is deliberately left alone
// because it is also what a manual website download produces.
//
// Protected from deletion:
//   - the installer referenced by a deferred/next-startup install record
//   - the installer referenced by a pending-update (did-it-land) record
//   - installers verified in this session and still awaiting a user decision
//   - files with a download currently in flight
func (a *App) cleanupStaleVersionedInstallers() {
	if a == nil {
		return
	}
	downloadsDir, err := a.GetDownloadsFolder()
	if err != nil || strings.TrimSpace(downloadsDir) == "" {
		return
	}
	target := updateTargetFileName()
	ext := filepath.Ext(target)
	stem := strings.TrimSuffix(target, ext)
	if stem == "" || ext == "" {
		return
	}

	protected := make(map[string]bool)
	protect := func(path string) {
		abs, err := filepath.Abs(strings.TrimSpace(path))
		if err != nil || strings.TrimSpace(path) == "" {
			return
		}
		protected[verifiedInstallerKey(abs)] = true
	}
	if record, ok := a.readDeferredUpdateInstall(); ok {
		protect(record.InstallerPath)
	}
	if record, ok := a.readPendingUpdate(); ok {
		protect(record.InstallerPath)
	}
	a.verifiedInstallerMu.Lock()
	for _, verified := range a.verifiedInstallers {
		if verified != nil {
			protect(verified.Path)
		}
	}
	a.verifiedInstallerMu.Unlock()

	inFlight := make(map[string]bool)
	a.downloadMutex.Lock()
	for name := range a.downloadCancelers {
		inFlight[name] = true
	}
	a.downloadMutex.Unlock()

	entries, err := os.ReadDir(downloadsDir)
	if err != nil {
		return
	}
	prefix := stem + "-"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) || inFlight[name] {
			continue
		}
		full := filepath.Join(downloadsDir, name)
		if protected[verifiedInstallerKey(full)] {
			continue
		}
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			a.log(fmt.Sprintf("[update-install] cannot remove stale installer %s: %v", full, err))
			continue
		}
		a.log(fmt.Sprintf("[update-install] removed stale installer %s", full))
	}
}
