package guiapp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testInstallerExt returns an extension the current platform accepts for
// unattended launches, so these tests run on every GOOS.
func testInstallerExt() string {
	switch runtime.GOOS {
	case "windows":
		return ".exe"
	case "darwin":
		return ".pkg"
	default:
		return ".deb"
	}
}

func writeInstallerFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+testInstallerExt())
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func testDigest(content string) string {
	return sha256Hex([]byte(content))
}

func TestWindowsInstallerSilentArgsUseNSISSwitches(t *testing.T) {
	// The release installer is built by NSIS, which understands exactly one
	// silent switch: /S. The Inno Setup family (/VERYSILENT, /SUPPRESSMSGBOXES,
	// /NOCANCEL, /NORESTART) is silently ignored by NSIS, so passing it ran the
	// full interactive wizard on every online update.
	for _, innoOnly := range []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NOCANCEL", "/NORESTART", "/SP-"} {
		if strings.Contains(windowsInstallerSilentArgs, innoOnly) {
			t.Fatalf("silent args %q contain the Inno Setup switch %q, which NSIS ignores", windowsInstallerSilentArgs, innoOnly)
		}
	}
	args := windowsInstallerSilentArgList
	if len(args) != 1 || args[0] != "/S" {
		t.Fatalf("silent args = %#v, want exactly [\"/S\"]", args)
	}
}

func TestAuthorizeInstallerLaunchRequiresVerifiedDownload(t *testing.T) {
	app := &App{}
	path := writeInstallerFixture(t, "setup", "payload")
	if _, err := app.authorizeInstallerLaunch(path); err == nil {
		t.Fatal("expected an unverified installer to be refused")
	}
}

func TestAuthorizeInstallerLaunchRejectsWrongExtension(t *testing.T) {
	app := &App{}
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := app.authorizeInstallerLaunch(path); err == nil {
		t.Fatal("expected a non-installer file to be refused")
	}
}

func TestAuthorizeInstallerLaunchAcceptsVerifiedDownload(t *testing.T) {
	app := &App{}
	path := writeInstallerFixture(t, "setup", "payload")
	app.rememberVerifiedInstaller(path, testDigest("payload"))

	got, err := app.authorizeInstallerLaunch(path)
	if err != nil {
		t.Fatalf("verified installer refused: %v", err)
	}
	if got != path {
		t.Fatalf("authorized path = %q, want %q", got, path)
	}
}

func TestAuthorizeInstallerLaunchDetectsPostDownloadTamper(t *testing.T) {
	app := &App{}
	path := writeInstallerFixture(t, "setup", "payload")
	app.rememberVerifiedInstaller(path, testDigest("payload"))

	// Swap the bytes after verification: the launch must not proceed even
	// though the path still matches the recorded download.
	if err := os.WriteFile(path, []byte("replaced"), 0o600); err != nil {
		t.Fatalf("overwrite fixture: %v", err)
	}
	if _, err := app.authorizeInstallerLaunch(path); err == nil {
		t.Fatal("expected a tampered installer to be refused")
	}
}

func TestAuthorizeInstallerLaunchRejectsOtherPath(t *testing.T) {
	app := &App{}
	verified := writeInstallerFixture(t, "setup", "payload")
	other := writeInstallerFixture(t, "other", "payload")
	app.rememberVerifiedInstaller(verified, testDigest("payload"))

	if _, err := app.authorizeInstallerLaunch(other); err == nil {
		t.Fatal("expected a different installer path to be refused")
	}
}

func TestSanitizeInstallerFileNameRejectsPathComponents(t *testing.T) {
	// The download path is later launched silently and elevated, so a crafted
	// manifest must not be able to aim it outside the Downloads folder.
	for _, bad := range []string{
		"",
		"   ",
		".",
		"..",
		"../evil.exe",
		`..\..\Windows\System32\evil.exe`,
		"sub/dir/setup.exe",
		`C:\Windows\Temp\setup.exe`,
		"/etc/cron.d/evil.exe",
	} {
		if _, err := sanitizeInstallerFileName(bad); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}

func TestSanitizeInstallerFileNameAcceptsBareName(t *testing.T) {
	got, err := sanitizeInstallerFileName("  MaClaw-Setup.exe  ")
	if err != nil {
		t.Fatalf("bare file name rejected: %v", err)
	}
	if got != "MaClaw-Setup.exe" {
		t.Fatalf("sanitized name = %q", got)
	}
}

func TestAuthorizeInstallerLaunchRejectsEmptyPath(t *testing.T) {
	app := &App{}
	path := writeInstallerFixture(t, "setup", "payload")
	app.rememberVerifiedInstaller(path, testDigest("payload"))
	if _, err := app.authorizeInstallerLaunch("   "); err == nil {
		t.Fatal("expected an empty installer path to be refused")
	}
}

func TestRememberVerifiedInstallerIgrowsUnusablePath(t *testing.T) {
	app := &App{}
	missing := filepath.Join(t.TempDir(), "gone"+testInstallerExt())
	app.rememberVerifiedInstaller(missing, "")
	// Nothing was recorded, so the launch must stay refused rather than fall
	// back to a path-only check.
	if _, err := app.authorizeInstallerLaunch(missing); err == nil {
		t.Fatal("expected an installer that could not be stat'ed to stay unauthorized")
	}
}

func TestSilentArgsListIsStable(t *testing.T) {
	first := append([]string(nil), windowsInstallerSilentArgList...)
	if len(windowsInstallerSilentArgList) != len(first) {
		t.Fatal("silent arg list changed between calls")
	}
}

func TestAuthorizeInstallerLaunchWithoutManifestHashUsesFileIdentity(t *testing.T) {
	app := &App{}
	path := writeInstallerFixture(t, "setup", "payload")
	// Older manifests can omit sha256; identity still guards the launch.
	app.rememberVerifiedInstaller(path, "")

	if _, err := app.authorizeInstallerLaunch(path); err != nil {
		t.Fatalf("verified installer refused: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte("replaced"), 0o600); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}
	if _, err := app.authorizeInstallerLaunch(path); err == nil {
		t.Fatal("expected a re-created installer file to be refused")
	}
}

// goFuncBody returns the source of the function starting at signature, up to
// the next top-level func. Source-level checks are the only way to guard a
// syscall path that cannot run on the test machine.
func goFuncBody(t *testing.T, text, signature string) string {
	t.Helper()
	for _, fn := range goFuncBodies(text) {
		if strings.HasPrefix(fn, signature) {
			return fn
		}
	}
	t.Fatalf("missing %s", signature)
	return ""
}

// goFuncBodies splits a Go source file into per-function chunks, each
// starting with its "func" keyword.
func goFuncBodies(text string) []string {
	chunks := strings.Split(text, "\nfunc ")
	for i := 1; i < len(chunks); i++ {
		chunks[i] = "func " + chunks[i]
	}
	return chunks[1:]
}

// A "runas" launch whose nShow is SW_HIDE fails with SE_ERR_ACCESSDENIED (5)
// before the UAC prompt is ever drawn: the online updater reported exactly
// that ("ShellExecute error code 5") and the update never installed. The
// launch path must keep SW_SHOWNORMAL (NSIS /S hides the installer UI
// itself) and must go through ShellExecuteExW, because plain ShellExecuteW
// reports a declined prompt and a genuine failure as the same access-denied
// code.
func TestWindowsInstallerLaunchNeverHidesElevatedWindow(t *testing.T) {
	text := readRepoFile(t, filepath.Join("guiapp", "platform_windows.go"))

	helper := goFuncBody(t, text, "func shellExecuteFile")
	if strings.Contains(helper, "SW_HIDE") {
		t.Fatal("shellExecuteFile must not pass SW_HIDE: the runas verb rejects it with access denied")
	}
	if !strings.Contains(helper, "swShowNormal") {
		t.Fatal("shellExecuteFile must launch with a normal window state")
	}
	if !strings.Contains(helper, "ShellExecuteExW") {
		t.Fatal("shellExecuteFile must use ShellExecuteExW so ERROR_CANCELLED is reported")
	}

	launch := goFuncBody(t, text, "func (a *App) LaunchInstallerAndExit")
	if strings.Contains(launch, "ShellExecuteW") || strings.Contains(launch, "SW_HIDE") {
		t.Fatal("installer launch must not call ShellExecuteW or hide the elevated window")
	}
	if !strings.Contains(launch, "shellExecuteFile") {
		t.Fatal("installer launch must go through the ShellExecuteExW helper")
	}
	// In the non-elevated branch runas must be tried before open: only runas
	// can raise the UAC prompt the installer needs.
	notElevated := strings.Index(launch, "if !elevated")
	if notElevated < 0 {
		t.Fatal("installer launch must branch on the process token")
	}
	branch := launch[notElevated:]
	if strings.Index(branch, `"runas"`) > strings.Index(branch, `"open"`) {
		t.Fatal("runas must be tried before open when the process is not elevated")
	}
}

// Every elevated launch in the platform file must go through the
// ShellExecuteExW helper: a "runas" paired with SW_HIDE fails with access
// denied before the UAC prompt is drawn, and plain ShellExecuteW cannot tell
// a declined prompt apart from a real failure. The installer updater and the
// Node/Git dependency installers all hit that defect.
func TestWindowsElevatedLaunchesShareCheckedHelper(t *testing.T) {
	text := readRepoFile(t, filepath.Join("guiapp", "platform_windows.go"))

	for _, fn := range goFuncBodies(text) {
		if !strings.Contains(fn, `"runas"`) {
			continue
		}
		sig := fn
		if nl := strings.Index(fn, "\n"); nl >= 0 {
			sig = fn[:nl]
		}
		if strings.Contains(fn, "syscall.SW_HIDE") {
			t.Fatalf("runas launch hides its window: %s", sig)
		}
		if strings.Contains(fn, `NewProc("ShellExecuteW")`) {
			t.Fatalf("runas launch bypasses shellExecuteFile: %s", sig)
		}
	}
	if strings.Contains(text, `StringToUTF16Ptr("runas")`) {
		t.Fatal("runas launches must go through shellExecuteFile")
	}
}
