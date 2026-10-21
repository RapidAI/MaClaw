package guiapp

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func silentUpdateNSI(t *testing.T) string {
	t.Helper()
	return readRepoFile(t, filepath.Join("build", "windows", "installer", "multiarch.nsi"))
}

// shippedInstallerScripts are the NSIS scripts CI publishes. They share the
// uninstall-then-install upgrade pattern, so a fix applied to one must be
// applied to all of them or the remaining ones keep the defect.
func shippedInstallerScripts(t *testing.T) map[string]string {
	t.Helper()
	names := []string{"multiarch.nsi", "maclawsrv.nsi", "tigerproxy.nsi", "datasrv.nsi"}
	texts := make(map[string]string, len(names))
	for _, name := range names {
		texts[name] = readRepoFile(t, filepath.Join("build", "windows", "installer", name))
	}
	return texts
}

// The registry UninstallString already carries quotes, so wrapping it in another
// pair produced ""path"" - an empty executable name - and the uninstaller never
// ran. Every script that launches the previous uninstaller must strip them.
func TestEveryInstallerStripsUninstallerQuotes(t *testing.T) {
	for name, text := range shippedInstallerScripts(t) {
		// datasrv.nsi runs the previous QuietUninstallString (already complete
		// with its own /S and quotes) and never re-quotes it, so it is exempt.
		// The other scripts read UninstallString and must strip its quotes.
		if name == "datasrv.nsi" {
			if strings.Contains(text, `ExecWait '"$R0"`) {
				t.Fatalf("%s: re-quotes an UninstallString it does not use", name)
			}
			continue
		}
		execAt := strings.Index(text, `ExecWait '"$R0" /S`)
		if execAt < 0 {
			t.Fatalf("%s: uninstaller is not invoked with a single-quoted path", name)
		}
		start := execAt - 600
		if start < 0 {
			start = 0
		}
		strip := text[start:execAt]
		if !strings.Contains(strip, "StrCpy $R1 $R0 1") || !strings.Contains(strip, `StrCmp $R1 '$\"' 0`) {
			t.Fatalf("%s: uninstaller path is not stripped of the quotes the registry value carries", name)
		}
		if strings.Count(text, `ExecWait '"$R0"`) != 1 {
			t.Fatalf("%s: uninstaller must be invoked from exactly one place", name)
		}
	}
}

// A MessageBox without /SD blocks a silent install on a question nobody can
// see. Every script that ships must answer its own prompts.
func TestEveryInstallerMessageHasSilentDefault(t *testing.T) {
	// The script is CRLF, so allow the trailing \r; anchoring on \r\n would
	// match nothing and pass vacuously.
	messageBox := regexp.MustCompile(`(?m)^[ \t]*MessageBox[ \t]+([^\r\n]+)`)
	for name, text := range shippedInstallerScripts(t) {
		matches := messageBox.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			t.Fatalf("%s: found no MessageBox calls; the check would pass vacuously", name)
		}
		for _, match := range matches {
			if !strings.Contains(match[1], "/SD") {
				t.Fatalf("%s: MessageBox without /SD blocks a silent install: %s", name, strings.TrimSpace(match[1]))
			}
		}
	}
}

// A console program started with ExecWait paints a black window over a silent
// install. Every shipped script must hide its taskkill calls.
func TestEveryInstallerHidesConsoleWindows(t *testing.T) {
	for name, text := range shippedInstallerScripts(t) {
		if regexp.MustCompile(`(?m)^\s*ExecWait\s+['"]taskkill`).MatchString(text) {
			t.Fatalf("%s: ExecWait taskkill flashes a console window during a silent install", name)
		}
	}
}

// A silent uninstall must not wipe the WebView2 profile: it holds the signed-in
// session and local UI state, so removing it turns unattended maintenance into
// a forced sign-in. Scripts that remove it must gate it on an interactive run.
func TestEveryInstallerKeepsProfileOnSilentUninstall(t *testing.T) {
	for name, text := range shippedInstallerScripts(t) {
		uninstallAt := strings.Index(text, `Section "uninstall"`)
		if uninstallAt < 0 {
			t.Fatalf("%s: missing uninstall section", name)
		}
		uninstall := text[uninstallAt:]
		profileAt := strings.Index(uninstall, `RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"`)
		if profileAt < 0 {
			continue
		}
		if !strings.Contains(uninstall[:profileAt], "IfSilent skipWebviewProfile") {
			t.Fatalf("%s: WebView2 profile removal is not gated on an interactive uninstall", name)
		}
	}
}

// NSIS reads a script without a BOM as ANSI. A script containing non-ASCII text
// (localized LangStrings) therefore builds only on a machine whose code page
// happens to match, and fails everywhere else. Pure-ASCII scripts are exempt.
func TestInstallerScriptsWithNonASCIIDeclareUTF8BOM(t *testing.T) {
	for name := range shippedInstallerScripts(t) {
		path := filepath.Join("..", "build", "windows", "installer", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		hasBOM := bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
		body := raw
		if hasBOM {
			body = raw[3:]
		}
		if hasBOM {
			continue
		}
		for _, b := range body {
			if b >= 0x80 {
				t.Fatalf("%s: contains non-ASCII text but has no UTF-8 BOM; NSIS will read it as ANSI and fail on non-Chinese runners", name)
			}
		}
	}
}

// A silent upgrade must not uninstall the previous build first: the uninstall
// section empties $INSTDIR, so a failure part-way through the new install would
// leave the user with no application at all. Silent runs install in place.
func TestSilentUpgradeInstallsInPlace(t *testing.T) {
	text := silentUpdateNSI(t)
	initAt := strings.Index(text, "Function .onInit")
	if initAt < 0 {
		t.Fatal("missing .onInit")
	}
	initBody := text[initAt:]
	if end := strings.Index(initBody, "\nFunctionEnd"); end >= 0 {
		initBody = initBody[:end]
	}

	// The already-installed question must route silent runs past the uninstall
	// branch, not merely default its answer to yes.
	installedAt := strings.Index(initBody, "installed:")
	if installedAt < 0 {
		t.Fatal("missing installed: label")
	}
	installedBlock := initBody[installedAt:]
	if !strings.Contains(installedBlock, "IfSilent notInstalled") {
		t.Fatal("silent upgrades must skip the uninstall-then-install branch")
	}
	uninstallAt := strings.Index(installedBlock, "uninstall:")
	if uninstallAt < 0 {
		t.Fatal("missing uninstall: label")
	}
	if !strings.Contains(installedBlock[:uninstallAt], "IfSilent notInstalled") {
		t.Fatal("the silent skip must run before the uninstall branch is reached")
	}
}

// The registry UninstallString already carries quotes, so wrapping it in another
// pair produced ""path"" — an empty executable name — and the uninstaller never
// ran. The script must strip them before quoting exactly once.
func TestUninstallerInvocationStripsExistingQuotes(t *testing.T) {
	text := silentUpdateNSI(t)
	execAt := strings.Index(text, `ExecWait '"$R0" /S`)
	if execAt < 0 {
		t.Fatal("uninstaller is not invoked with a single-quoted path")
	}
	// The strip must run immediately before the launch, in the same block.
	start := execAt - 600
	if start < 0 {
		start = 0
	}
	strip := text[start:execAt]
	if !strings.Contains(strip, "StrCpy $R1 $R0 1") || !strings.Contains(strip, `StrCmp $R1 '$\"' 0`) {
		t.Fatal("uninstaller path is not stripped of the quotes the registry value carries")
	}
	if !strings.Contains(strip, "StrCpy $R0 $R1 -1") {
		t.Fatal("uninstaller path keeps the trailing quote")
	}
	// Guard against a silent install re-running the uninstaller by another path.
	if strings.Count(text, `ExecWait '"$R0"`) != 1 {
		t.Fatal("uninstaller must be invoked from exactly one place")
	}
}

// A silent run has no UI, so the console programs the installer starts must not
// paint a console window over it.
func TestSilentUpdateHidesConsoleWindows(t *testing.T) {
	text := silentUpdateNSI(t)
	initAt := strings.Index(text, "Function .onInit")
	if initAt < 0 {
		t.Fatal("missing .onInit")
	}
	initBody := text[initAt:]
	if end := strings.Index(initBody, "\nFunctionEnd"); end >= 0 {
		initBody = initBody[:end]
	}
	if regexp.MustCompile(`(?m)^\s*ExecWait\s+'taskkill`).MatchString(initBody) {
		t.Fatal(".onInit must not ExecWait taskkill: it flashes a console window during a silent install")
	}
	for _, exe := range []string{"${PRODUCT_EXECUTABLE}", "${CLI_EXECUTABLE}", "${ACP_BRIDGE_EXECUTABLE}"} {
		if !strings.Contains(initBody, "Push \""+exe+"\"") {
			t.Fatalf("silent taskkill does not cover %s", exe)
		}
	}
	if !strings.Contains(text, "nsExec::Exec 'taskkill") {
		t.Fatal("missing the hidden taskkill helper")
	}
}

// Every MessageBox the installer can reach during a silent run needs a /SD
// default, or the silent install blocks on a question nobody can see.
func TestSilentUpdateMessagesHaveDefaults(t *testing.T) {
	text := silentUpdateNSI(t)
	// The script is CRLF, so allow the trailing \r before the line end; a
	// pattern anchored to \r\n would silently match nothing and pass vacuously.
	messageBox := regexp.MustCompile(`(?m)^[ \t]*MessageBox[ \t]+([^\r\n]+)`)
	matches := messageBox.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		t.Fatal("found no MessageBox calls; the check would pass vacuously")
	}
	for _, match := range matches {
		if !strings.Contains(match[1], "/SD") {
			t.Fatalf("MessageBox without /SD blocks a silent install: %s", strings.TrimSpace(match[1]))
		}
	}
}

// A silent run that Aborts would end with no window at all: the in-app updater
// already quit, and the installer shows nothing. The installed build must come
// back so the failure does not look like a crash.
func TestSilentAbortRelaunchesInstalledBuild(t *testing.T) {
	text := silentUpdateNSI(t)
	helper := nsiMacroBodyOrFunction(t, text, "SilentRelaunchApp")
	if !strings.Contains(helper, "explorer.exe") {
		t.Fatal("relaunch must go through explorer so the app is not elevated")
	}
	abortHelper := nsiMacroBodyOrFunction(t, text, "AbortSilentOrExit")
	if !strings.Contains(abortHelper, "Call SilentRelaunchApp") || !strings.Contains(abortHelper, "Abort") {
		t.Fatal("AbortSilentOrExit must relaunch before aborting")
	}
	// A custom install directory is only known from the registry, so the
	// relaunch helper would look in the default folder without this.
	initAt := strings.Index(text, "Function .onInit")
	if initAt < 0 {
		t.Fatal("missing .onInit")
	}
	initBody := text[initAt:]
	resolveCall := strings.Index(initBody, "Call ResolveExistingInstallDir")
	if resolveCall < 0 {
		t.Fatal(".onInit must resolve an existing install directory before it can abort")
	}
	// Every abort path must come after that resolution.
	for _, marker := range []string{"Call AbortSilentOrExit", "appRunning:"} {
		at := strings.Index(initBody, marker)
		if at < 0 {
			continue
		}
		if at < resolveCall {
			t.Fatalf("%s can abort before the install directory is resolved", marker)
		}
	}
	resolveHelper := nsiMacroBodyOrFunction(t, text, "ResolveExistingInstallDir")
	if !strings.Contains(resolveHelper, `"InstallLocation"`) || !strings.Contains(resolveHelper, "StrCpy $INSTDIR $R2") {
		t.Fatal("ResolveExistingInstallDir does not copy InstallLocation into $INSTDIR")
	}
}

func nsiMacroBodyOrFunction(t *testing.T, text, name string) string {
	t.Helper()
	for _, marker := range []string{"Function " + name, "!macro " + name} {
		at := strings.Index(text, marker)
		if at < 0 {
			continue
		}
		body := text[at+len(marker):]
		if end := strings.Index(body, "FunctionEnd"); end >= 0 {
			return body[:end]
		}
		if end := strings.Index(body, "!macroend"); end >= 0 {
			return body[:end]
		}
	}
	t.Fatalf("missing %s", name)
	return ""
}

// A silent uninstall (QuietUninstallString, fleet scripts) must keep the
// WebView2 profile: it holds the signed-in session and local UI state, so
// wiping it turns unattended maintenance into a forced sign-in.
func TestSilentUninstallKeepsWebviewProfile(t *testing.T) {
	text := silentUpdateNSI(t)
	uninstallAt := strings.Index(text, `Section "uninstall"`)
	if uninstallAt < 0 {
		t.Fatal("missing uninstall section")
	}
	uninstall := text[uninstallAt:]
	profileAt := strings.Index(uninstall, `RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"`)
	if profileAt < 0 {
		t.Fatal("interactive uninstall must still be able to drop the WebView2 profile")
	}
	if !strings.Contains(uninstall[:profileAt], "IfSilent skipWebviewProfile") {
		t.Fatal("WebView2 profile removal is not gated on an interactive uninstall")
	}
	if !strings.Contains(uninstall[profileAt:profileAt+200], "skipWebviewProfile:") {
		t.Fatal("missing skipWebviewProfile label after the profile removal")
	}
	// The uninstall section must not flash consoles either, and it cannot Call
	// installer functions at all: NSIS puts the uninstaller in a separate un.*
	// code block, so a Call to an installer-only function fails to compile.
	if regexp.MustCompile(`(?m)^\s*ExecWait\s+"taskkill`).MatchString(uninstall) {
		t.Fatal("uninstall must not ExecWait taskkill: it flashes a console window")
	}
	if strings.Contains(uninstall, "Call SilentTaskKill") {
		t.Fatal("uninstall cannot Call the installer's SilentTaskKill helper")
	}
	if !strings.Contains(uninstall, "nsExec::Exec 'taskkill") {
		t.Fatal("uninstall must hide its taskkill calls with nsExec")
	}
}
