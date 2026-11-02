package tinytex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// TeX Live compares the local critical packages (tlmgr and the infra
// packages) with the remote tlpdb. When the repository is newer, every
// action except `tlmgr update --self` prints the banner below and exits.
// On Windows, perl's die becomes exit status 255. The gpg line that often
// sits next to it ("not verified: gpg unavailable") is a warning: TinyTeX
// ships without gpg, and tlmgr keeps going.
const (
	tlmgrNeedsUpdateText = "tlmgr itself needs to be updated"
	tlmgrTerminatingText = "Terminating; please see warning above"
	tlmgrUpdaterMarker   = "update-tlmgr-latest"
)

// TlmgrRecovery is what to do after a tlmgr run that was supposed to make
// the local manager usable.
type TlmgrRecovery int

const (
	// TlmgrReady means install (or the retried install) may proceed.
	TlmgrReady TlmgrRecovery = iota
	// TlmgrNextMirror means this repository failed for a reason a tlmgr
	// updater cannot repair, such as a dead mirror.
	TlmgrNextMirror
	// TlmgrOfficialUpdater means the same-year guard or a broken infra
	// update needs update-tlmgr-latest from that repository.
	TlmgrOfficialUpdater
	// TlmgrOfficialUpgrade means the installed TeX Live year is older than
	// the repository. The updater has to be run with `-- --upgrade`.
	TlmgrOfficialUpgrade
)

// TlmgrOutputNeedsSelfUpdate reports the critical-package guard. A clipped
// tail can drop the first line and still keep the prescribed updater URL
// plus the terminating line.
func TlmgrOutputNeedsSelfUpdate(output string) bool {
	if strings.Contains(output, tlmgrNeedsUpdateText) {
		return true
	}
	return strings.Contains(output, tlmgrUpdaterMarker) && strings.Contains(output, tlmgrTerminatingText)
}

// TlmgrOutputCrossRelease reports a year mismatch. update --self cannot
// cross TeX Live releases; the official updater's --upgrade switch can.
func TlmgrOutputCrossRelease(output string) bool {
	return strings.Contains(output, "is older than remote repository") ||
		strings.Contains(output, "Cross release updates are only supported")
}

// TlmgrOutputInfraBroken reports a self-update that wrote a tree tlmgr can
// no longer read, which is the case the official updater archive is for.
func TlmgrOutputInfraBroken(output string) bool {
	return strings.Contains(output, "Unknown directive") ||
		strings.Contains(output, "Infrastructure update failed") ||
		strings.Contains(output, "failed self restore")
}

// ClassifyTlmgrSelfUpdate reads one `tlmgr update --self` result.
// A zero exit is ready: on Windows, tlmgr.bat runs temp\updater-w32.bat
// after perl exits, and that exit status is the status of the update.
// The script's own failures are exit /b 1 (rollback, missing tar.exe, or
// a directory change) and exit /b 666 (:panic). Those lines go to the
// console and to temp\update-self.log. Perl's own failures use F_WARNING
// 1 and F_ERROR 2 and do not leave the "Preparing" line without
// "Aborting infrastructure update".
func ClassifyTlmgrSelfUpdate(runErr error, output string) TlmgrRecovery {
	if runErr == nil {
		return TlmgrReady
	}
	recovery := classifyTlmgrFailure(output)
	if recovery == TlmgrNextMirror && windowsUpdaterScriptFailed(runErr, output) {
		return TlmgrOfficialUpdater
	}
	return recovery
}

// windowsUpdaterScriptFailed reports updater-w32.bat, not perl and not
// tlmgr.bat's own refusal to delete or rename the script. tlmgr.bat invokes
// the updater without call, so the script's exit code is the process code.
func windowsUpdaterScriptFailed(err error, output string) bool {
	code, ok := exitStatusCode(err)
	if !ok {
		return false
	}
	if code == 666 {
		return true
	}
	if code != 1 || TlmgrOutputStaleUpdater(output) {
		return false
	}
	if strings.Contains(output, "Aborting infrastructure update") {
		return false
	}
	return strings.Contains(output, "Preparing TeX Live infrastructure update")
}

func exitStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	const marker = "exit status "
	s := err.Error()
	i := strings.LastIndex(s, marker)
	if i < 0 {
		return 0, false
	}
	rest := s[i+len(marker):]
	n := 0
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		n = n*10 + int(rest[digits]-'0')
		digits++
		if n > 1_000_000 {
			return 0, false
		}
	}
	if digits == 0 {
		return 0, false
	}
	if digits < len(rest) {
		switch rest[digits] {
		case ' ', '\n', '\r', ':', '\t':
		default:
			return 0, false
		}
	}
	return n, true
}

// ClassifyTlmgrInstallFailure reads a failed install (or any action other
// than update --self). The guard exits before packages are unpacked.
func ClassifyTlmgrInstallFailure(output string) TlmgrRecovery {
	return classifyTlmgrFailure(output)
}

func classifyTlmgrFailure(output string) TlmgrRecovery {
	if TlmgrOutputCrossRelease(output) {
		return TlmgrOfficialUpgrade
	}
	if TlmgrOutputNeedsSelfUpdate(output) || TlmgrOutputInfraBroken(output) {
		return TlmgrOfficialUpdater
	}
	return TlmgrNextMirror
}

// TlmgrUpdaterName is the disaster-recovery archive published next to tlnet.
func TlmgrUpdaterName(goos string) string {
	if goos == "windows" {
		return "update-tlmgr-latest.exe"
	}
	return "update-tlmgr-latest.sh"
}

// TlmgrUpdaterURL joins a tlnet repository with the updater for goos.
func TlmgrUpdaterURL(repo, goos string) (string, error) {
	repo = strings.TrimRight(strings.TrimSpace(repo), "/")
	if !strings.HasPrefix(repo, "https://") || strings.ContainsAny(repo, " \t") {
		return "", fmt.Errorf("refusing TeX Live repository %q", repo)
	}
	return repo + "/" + TlmgrUpdaterName(goos), nil
}

// TlmgrUpdaterArgs is the extra argv for the official updater.
// A same-year repair takes none. A cross-release repair takes `-- --upgrade`.
func TlmgrUpdaterArgs(upgrade bool) []string {
	if !upgrade {
		return nil
	}
	return []string{"--", "--upgrade"}
}

// UpdaterOK accepts the official updater and rejects HTML error pages.
// The Windows file is a PE self-extractor. The Unix file is a shell script
// with an embedded payload, so the xz check used for TinyTeX archives does
// not apply.
func UpdaterOK(path string) error {
	if strings.EqualFold(extOf(path), ".exe") {
		return ArchiveOK(path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Size() < minArchiveBytes {
		return fmt.Errorf("updater too small (%d bytes)", fi.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 64)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	head := string(buf[:n])
	lower := strings.ToLower(head)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") {
		return fmt.Errorf("updater is an HTML page")
	}
	if !strings.HasPrefix(head, "#!") {
		return fmt.Errorf("updater is not a shell script")
	}
	return nil
}

func extOf(path string) string {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return ""
	}
	return path[i:]
}

// ErrStaleUpdater means a leftover Windows updater script is still in the way.
// Another mirror cannot repair that.
var ErrStaleUpdater = errors.New("stale tlmgr updater script")

// TlmgrOutputStaleUpdater reports tlmgr.bat's own refusal to start or to
// rename the updater. Those lines are not the critical-package guard, and
// they do not change on the next repository.
func TlmgrOutputStaleUpdater(output string) bool {
	if strings.Contains(output, "failed to remove previous updater script") {
		return true
	}
	return strings.Contains(output, "failed to rename") && strings.Contains(output, "updater-w32")
}

// ClearStaleTlmgrUpdater removes a leftover Windows updater script.
// tlmgr.bat deletes temp\updater-w32 before every command and exits 1 with
// "failed to remove previous updater script" when that delete fails. The
// batch del does not clear the read-only attribute. The renamed
// updater-w32.bat is not checked at startup; it only blocks the later
// move during update --self. Other systems have no updater-w32 step.
func ClearStaleTlmgrUpdater(dist string) error {
	if runtime.GOOS != "windows" || strings.TrimSpace(dist) == "" {
		return nil
	}
	for _, name := range []string{"updater-w32", "updater-w32.bat"} {
		if err := removeUpdaterFile(dist, name); err != nil {
			return err
		}
	}
	return nil
}

func removeUpdaterFile(dist, name string) error {
	path := filepath.Join(dist, "temp", name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%w (%s): %v", ErrStaleUpdater, path, err)
	}
	// Windows only honors the write bit. A read-only script makes del,
	// and Remove, fail until the attribute is cleared.
	_ = os.Chmod(path, 0o666)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w (%s): %v", ErrStaleUpdater, path, err)
	}
	return nil
}

// DistFromTlmgr is the TeX tree that contains bin/<platform>/tlmgr.
func DistFromTlmgr(tlmgr string) (string, bool) {
	platform := filepath.Dir(tlmgr)
	bin := filepath.Dir(platform)
	if !strings.EqualFold(filepath.Base(bin), "bin") {
		return "", false
	}
	dist := filepath.Dir(bin)
	if dist == "" || dist == "." {
		return "", false
	}
	return dist, true
}

// prepareTlmgrLaunch runs before a Windows tlmgr.bat. A killed self-update
// can leave updater-w32 behind, and the next bat will not start until that
// file is gone. updater-w32.bat is the rename target of update --self only;
// a locked copy must not block option repository or install.
func prepareTlmgrLaunch(bin string, args []string) error {
	if runtime.GOOS != "windows" || !strings.EqualFold(filepath.Base(bin), "tlmgr.bat") {
		return nil
	}
	dist, ok := DistFromTlmgr(bin)
	if !ok {
		return nil
	}
	if err := removeUpdaterFile(dist, "updater-w32"); err != nil {
		return err
	}
	err := removeUpdaterFile(dist, "updater-w32.bat")
	if err != nil && tlmgrUpdatingSelf(args) {
		return err
	}
	return nil
}

// SavedRepository reads the repository tlmgr last saved. It is the
// opt_location of 00texlive.installation, which sits at the front of the
// tlpdb. The rest of that file is the package catalogue and is not read.
func SavedRepository(dist string) (string, bool) {
	f, err := os.Open(filepath.Join(dist, "tlpkg", "texlive.tlpdb"))
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF && n == 0 {
		return "", false
	}
	const key = "depend opt_location:"
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if !strings.HasPrefix(line, key) {
			continue
		}
		repo := strings.TrimRight(strings.TrimSpace(line[len(key):]), "/")
		if repo == "" {
			return "", false
		}
		return repo, true
	}
	return "", false
}

// lookupTLNet resolves one repository URL. Tests replace it.
var lookupTLNet = ResolveTLNet

// pinInstalledRepository points a redirector such as mirror.ctan.org at one
// concrete tlnet before update --self. Otherwise the self-update and the
// retried install follow the redirector separately and can see two revisions.
// The returned URL is the repository tlmgr kept. A failed option command
// leaves the saved URL in place, and the official updater must use that
// same URL instead of a resolution tlmgr never stored.
func pinInstalledRepository(ctx context.Context, run CommandRunner, tlmgr string) (string, bool) {
	dist, ok := DistFromTlmgr(tlmgr)
	if !ok || run == nil {
		return "", false
	}
	repo, ok := SavedRepository(dist)
	if !ok {
		return "", false
	}
	pinned := strings.TrimRight(strings.TrimSpace(lookupTLNet(ctx, repo)), "/")
	if pinned == "" || pinned == repo {
		return repo, true
	}
	_, code, err := run(ctx, tlmgr, filepath.Dir(tlmgr), "option", "repository", pinned)
	if err != nil || code != 0 {
		return repo, true
	}
	return pinned, true
}

// applyOfficialUpdater runs update-tlmgr-latest for one saved repository.
// The scheme installer does this itself. 编译预览 and build_verify register
// the same runner so a later package install can repair a self-update that
// exited 1 or 666, or that reported success while install stays guarded.
// Nil leaves that failure as a tlmgr error and does not download anything.
var applyOfficialUpdater func(ctx context.Context, dist, tlmgr, repo string, upgrade bool) error

// SetApplyOfficialUpdater registers the official updater used by InstallPackage.
func SetApplyOfficialUpdater(fn func(ctx context.Context, dist, tlmgr, repo string, upgrade bool) error) {
	applyOfficialUpdater = fn
}

var errOfficialUpdaterUnset = errors.New("official tlmgr updater is not registered")

// InstallPackage runs `tlmgr install`. When the critical-package guard (or a
// broken infra tree) refuses the install, it runs `tlmgr update --self` once
// and retries. A zero exit from that self-update is what Windows records
// after tlmgr.bat has applied updater-w32. When that exit is the updater
// script's failure, or the retried install is still guarded, the registered
// official updater runs once. The working directory is the directory of
// tlmgr so a relative path inside the batch file stays in this tree.
func InstallPackage(ctx context.Context, run CommandRunner, tlmgr, pkg string) error {
	if run == nil {
		return fmt.Errorf("tlmgr runner is missing")
	}
	dir := filepath.Dir(tlmgr)
	out, code, err := run(ctx, tlmgr, dir, "install", pkg)
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	if classifyTlmgrFailure(out) == TlmgrNextMirror {
		return fmt.Errorf("tlmgr install %s exited %d: %s", pkg, code, clip(out, 400))
	}
	repo, haveRepo := pinInstalledRepository(ctx, run, tlmgr)
	selfOut, selfCode, selfErr := run(ctx, tlmgr, dir, "update", "--self")
	if selfErr != nil {
		return selfErr
	}
	official := false
	if selfCode != 0 {
		switch ClassifyTlmgrSelfUpdate(fmt.Errorf("exit status %d", selfCode), selfOut) {
		case TlmgrReady:
		case TlmgrNextMirror:
			return fmt.Errorf("tlmgr update --self exited %d: %s", selfCode, clip(selfOut, 400))
		case TlmgrOfficialUpgrade:
			if err := installPackageOfficialUpdater(ctx, tlmgr, repo, haveRepo, true); err != nil {
				if errors.Is(err, errOfficialUpdaterUnset) {
					return fmt.Errorf("tlmgr update --self exited %d: %s", selfCode, clip(selfOut, 400))
				}
				return err
			}
			official = true
		default:
			if err := installPackageOfficialUpdater(ctx, tlmgr, repo, haveRepo, false); err != nil {
				if errors.Is(err, errOfficialUpdaterUnset) {
					return fmt.Errorf("tlmgr update --self exited %d: %s", selfCode, clip(selfOut, 400))
				}
				return err
			}
			official = true
		}
	}
	out, code, err = run(ctx, tlmgr, dir, "install", pkg)
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	recovery := classifyTlmgrFailure(out)
	if !official && recovery != TlmgrNextMirror {
		if err := installPackageOfficialUpdater(ctx, tlmgr, repo, haveRepo, recovery == TlmgrOfficialUpgrade); err != nil {
			if errors.Is(err, errOfficialUpdaterUnset) {
				return fmt.Errorf("tlmgr install %s exited %d: %s", pkg, code, clip(out, 400))
			}
			return err
		}
		out, code, err = run(ctx, tlmgr, dir, "install", pkg)
		if err != nil {
			return err
		}
		if code == 0 {
			return nil
		}
	}
	return fmt.Errorf("tlmgr install %s exited %d: %s", pkg, code, clip(out, 400))
}

func installPackageOfficialUpdater(ctx context.Context, tlmgr, repo string, haveRepo, upgrade bool) error {
	if applyOfficialUpdater == nil {
		return errOfficialUpdaterUnset
	}
	if !haveRepo || strings.TrimSpace(repo) == "" {
		return fmt.Errorf("tlmgr updater: saved repository is unknown")
	}
	dist, ok := DistFromTlmgr(tlmgr)
	if !ok {
		return fmt.Errorf("tlmgr updater: installation root is unknown")
	}
	return applyOfficialUpdater(ctx, dist, tlmgr, repo, upgrade)
}
