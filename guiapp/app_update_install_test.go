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

func TestWindowsInstallerSilentArgsAnswerEveryPrompt(t *testing.T) {
	for _, want := range []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"} {
		if !strings.Contains(windowsInstallerSilentArgs, want) {
			t.Fatalf("silent args %q missing %q", windowsInstallerSilentArgs, want)
		}
	}
	args := windowsInstallerSilentArgList
	if len(args) == 0 || args[0] != "/VERYSILENT" {
		t.Fatalf("unexpected arg split: %#v", args)
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
