package guiapp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyCompanionBrowserOrder(t *testing.T) {
	absent := classifyCompanionBrowser("safari", companionBrowserSpec{}, nil)
	if absent.State != browserExtStateAbsent {
		t.Fatalf("absent=%#v", absent)
	}
	missing := classifyCompanionBrowser("chrome", companionBrowserSpec{}, nil)
	if missing.State != browserExtStateNotInstalled || missing.Detail != browserExtDetailNotInstalled {
		t.Fatalf("missing=%#v", missing)
	}
	failed := classifyCompanionBrowser("chrome", companionBrowserSpec{Present: true, Failure: browserExtDetailNoStore}, &browserExtensionBrowserRecord{Version: browserExtensionVersion})
	if failed.State != browserExtStateFailed || failed.Detail != browserExtDetailNoStore {
		t.Fatalf("store failure did not win: %#v", failed)
	}
	installed := classifyCompanionBrowser("edge", companionBrowserSpec{Present: true}, &browserExtensionBrowserRecord{
		Version: browserExtensionVersion, HelloAt: "2020-01-01T00:00:00Z", RegisteredAt: "2026-01-01T00:00:00Z", BrowserRunningAtRegister: true,
	})
	if installed.State != browserExtStateInstalled {
		t.Fatalf("matching version was downgraded: %#v", installed)
	}
	restart := classifyCompanionBrowser("chrome", companionBrowserSpec{Present: true}, &browserExtensionBrowserRecord{
		Version: "9.9.9", RegisteredAt: "2026-01-01T00:00:00Z", BrowserRunningAtRegister: true,
	})
	if restart.State != browserExtStateNeedsRestart || restart.Detail != browserExtDetailNeedsRestart {
		t.Fatalf("restart=%#v", restart)
	}
	mismatch := classifyCompanionBrowser("chrome", companionBrowserSpec{Present: true}, &browserExtensionBrowserRecord{Version: "9.9.9"})
	if mismatch.State != browserExtStateNeedsEnable || mismatch.Detail != browserExtDetailVersion {
		t.Fatalf("mismatch=%#v", mismatch)
	}
	local := classifyCompanionBrowser("chrome", companionBrowserSpec{Present: true, LocalSideload: true}, nil)
	if local.State != browserExtStateNeedsEnable || local.Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("local=%#v", local)
	}
	localOld := classifyCompanionBrowser("edge", companionBrowserSpec{Present: true, LocalSideload: true}, &browserExtensionBrowserRecord{Version: "0.1.0", BrowserRunningAtRegister: true})
	if localOld.State != browserExtStateNeedsEnable || localOld.Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("stale packed hello=%#v", localOld)
	}
	packedHello := classifyCompanionBrowser("chrome", companionBrowserSpec{Present: true, LocalSideload: true}, &browserExtensionBrowserRecord{Version: browserExtensionVersion})
	if packedHello.State != browserExtStateNeedsEnable || packedHello.Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("packed hello hid the folder: %#v", packedHello)
	}
	unpackedHello := classifyCompanionBrowser("chrome", companionBrowserSpec{Present: true, LocalSideload: true}, &browserExtensionBrowserRecord{Version: browserExtensionVersion, InstallMode: browserExtInstallUnpacked})
	if unpackedHello.State != browserExtStateInstalled || unpackedHello.Detail != browserExtDetailInstalled {
		t.Fatalf("unpacked hello=%#v", unpackedHello)
	}
	silent := classifyCompanionBrowser("edge", companionBrowserSpec{Present: true, SilentEdge: true}, nil)
	if silent.Detail != browserExtDetailEdgeSilent {
		t.Fatalf("silent=%#v", silent)
	}
	safari := classifyCompanionBrowser("safari", companionBrowserSpec{Present: true, SafariEnabled: false}, &browserExtensionBrowserRecord{Version: "9.9.9", BrowserRunningAtRegister: true})
	if safari.State != browserExtStateNeedsEnable || safari.Detail != browserExtDetailSafariCheck {
		t.Fatalf("safari used hello: %#v", safari)
	}
	login := classifyCompanionBrowser("safari", companionBrowserSpec{Present: true, Failure: browserExtDetailLoginItem}, nil)
	if login.Detail != browserExtDetailLoginItem || login.State != browserExtStateFailed {
		t.Fatalf("login=%#v", login)
	}
}

func TestInstallBrowserExtensionsPreservesHelloAndSkipsStore(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	ops := hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS:          "windows",
		Home:          home,
		HostExe:       filepath.Join(dir, "MaClaw.exe"),
		ChromeExe:     `C:\Program Files\Google\Chrome\Application\chrome.exe`,
		EdgeExe:       `C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		SafariApp:     true,
		ChromeRunning: true,
	})
	helloAt := "2026-02-02T00:00:00Z"
	if err := writeBrowserExtensionState(dir, browserExtensionStateFile{
		Chrome: &browserExtensionBrowserRecord{Version: browserExtensionVersion, HelloAt: helloAt},
		Edge:   &browserExtensionBrowserRecord{Version: "0.1.0", HelloAt: helloAt},
	}); err != nil {
		t.Fatal(err)
	}
	prevChrome := browserExtensionChromeUpdateURL
	prevEdge := browserExtensionEdgeUpdateURL
	browserExtensionChromeUpdateURL = ""
	browserExtensionEdgeUpdateURL = "file:///tmp/ext.xml"
	t.Cleanup(func() {
		browserExtensionChromeUpdateURL = prevChrome
		browserExtensionEdgeUpdateURL = prevEdge
	})
	extDir := writeCompanionExtensionFixture(t)
	prevExt := companionExtensionDirHook
	companionExtensionDirHook = func() (string, error) { return extDir, nil }
	t.Cleanup(func() { companionExtensionDirHook = prevExt })
	got := (&App{}).InstallBrowserExtensions()
	if !got.OK {
		t.Fatalf("result=%#v", got)
	}
	byBrowser := map[string]BrowserExtensionBrowserStatus{}
	for _, row := range got.Browsers {
		byBrowser[row.Browser] = row
		if row.Browser == "safari" {
			t.Fatal("safari was drawn on windows")
		}
	}
	if byBrowser["chrome"].State != browserExtStateNeedsEnable || byBrowser["chrome"].Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("chrome=%#v", byBrowser["chrome"])
	}
	if byBrowser["edge"].State != browserExtStateNeedsEnable || byBrowser["edge"].Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("edge=%#v", byBrowser["edge"])
	}
	absUnpacked, err := filepath.Abs(filepath.Join(dir, companionUnpackedDirName))
	if err != nil {
		t.Fatal(err)
	}
	if got.UnpackedPath != absUnpacked {
		t.Fatalf("unpacked=%s", got.UnpackedPath)
	}
	devID := companionDevExtensionID()
	if devID == "" || devID == browserExtensionID {
		t.Fatalf("dev id=%s", devID)
	}
	deletes := 0
	for _, op := range ops.ops {
		if op.Name == "update_url" || strings.Contains(op.Value, "file://") {
			t.Fatalf("local install used a store url: %#v", op)
		}
		if op.Delete {
			if !strings.Contains(op.Path, `Extensions\`+devID) || strings.Contains(op.Path, `NativeMessagingHosts`) || strings.Contains(op.Path, `Policies`) {
				t.Fatalf("install deleted %#v", op)
			}
			deletes++
			continue
		}
		if strings.Contains(op.Path, `Extensions\`) {
			t.Fatalf("packed extension was registered: %#v", op)
		}
	}
	if deletes != 4 {
		t.Fatalf("deletes=%d ops=%#v", deletes, ops.ops)
	}
	if _, err := os.Stat(filepath.Join(dir, companionLocalCRXName)); !os.IsNotExist(err) {
		t.Fatal("packed crx was written")
	}
	manifestBody, err := os.ReadFile(filepath.Join(dir, companionUnpackedDirName, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var unpacked map[string]any
	if err := json.Unmarshal(manifestBody, &unpacked); err != nil {
		t.Fatal(err)
	}
	key, _ := unpacked["key"].(string)
	gotID, err := chromeExtensionIDFromManifestKey(key)
	if err != nil || gotID != devID || unpacked["version"] != browserExtensionVersion {
		t.Fatalf("unpacked id=%s version=%v err=%v", gotID, unpacked["version"], err)
	}
	if _, err := os.Stat(filepath.Join(dir, companionUnpackedDirName, "background.js")); err != nil {
		t.Fatal(err)
	}
	state := readBrowserExtensionState(dir)
	if state.Chrome == nil || state.Chrome.Version != "" || state.Chrome.HelloAt != "" || state.Chrome.InstallMode != browserExtInstallUnpacked || state.Chrome.RegisteredAt == "" || !state.Chrome.BrowserRunningAtRegister {
		t.Fatalf("chrome state=%#v", state.Chrome)
	}
	if state.Edge == nil || state.Edge.Version != "" || state.Edge.HelloAt != "" || state.Edge.InstallMode != browserExtInstallUnpacked || state.Edge.BrowserRunningAtRegister {
		t.Fatalf("edge state=%#v", state.Edge)
	}
	if err := recordBrowserExtensionHello("chrome", browserExtensionVersion); err != nil {
		t.Fatal(err)
	}
	again := statusByBrowser((&App{}).InstallBrowserExtensions())
	if again["chrome"].State != browserExtStateInstalled || again["edge"].Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("after hello chrome=%#v edge=%#v", again["chrome"], again["edge"])
	}
	state = readBrowserExtensionState(dir)
	if state.Chrome == nil || state.Chrome.Version != browserExtensionVersion || state.Chrome.HelloAt == "" || state.Chrome.InstallMode != browserExtInstallUnpacked {
		t.Fatalf("kept hello=%#v", state.Chrome)
	}
	if state.Edge == nil || state.Edge.Version != "" || state.Edge.HelloAt != "" || state.Edge.InstallMode != browserExtInstallUnpacked {
		t.Fatalf("edge hello returned=%#v", state.Edge)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, companionBrowserHostName+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytesContainsArguments(manifest) {
		t.Fatalf("manifest has arguments: %s", manifest)
	}
	var parsed companionHostManifest
	if err := json.Unmarshal(manifest, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Path != filepath.Join(dir, "MaClaw.exe") || parsed.Type != "stdio" || len(parsed.AllowedOrigins) != 2 || devID == "" || devID == browserExtensionID {
		t.Fatalf("manifest=%s dev=%s", manifest, devID)
	}
	if parsed.AllowedOrigins[0] != "chrome-extension://"+browserExtensionID+"/" || parsed.AllowedOrigins[1] != "chrome-extension://"+devID+"/" {
		t.Fatalf("origins=%v", parsed.AllowedOrigins)
	}
	lib := filepath.Join(dir, "library.json")
	if err := os.WriteFile(lib, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops.ops = nil
	removed := (&App{}).UninstallBrowserExtensions()
	if !removed.OK {
		t.Fatalf("uninstall=%#v", removed)
	}
	if _, err := os.Stat(lib); err != nil {
		t.Fatal("library.json was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, companionBrowserHostName+".json")); !os.IsNotExist(err) {
		t.Fatal("host manifest survived uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, companionUnpackedDirName)); !os.IsNotExist(err) {
		t.Fatal("unpacked folder survived uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, companionDevKeyName)); err != nil {
		t.Fatal("dev key was removed")
	}
	deleted := 0
	for _, op := range ops.ops {
		if !op.Delete {
			t.Fatalf("uninstall wrote %#v", op)
		}
		if strings.Contains(strings.ToUpper(op.Path), "HKLM") || strings.Contains(op.Path, `Policies`) {
			t.Fatalf("uninstall touched %s", op.Path)
		}
		deleted++
	}
	if deleted == 0 {
		t.Fatal("uninstall deleted no HKCU keys")
	}
}

func TestInstallBrowserExtensionsStoreURLAndLinuxChrome(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	ops := hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS:      "windows",
		Home:      home,
		HostExe:   filepath.Join(dir, "MaClaw.exe"),
		ChromeExe: "chrome",
		EdgeExe:   "msedge",
	})
	prevChrome := browserExtensionChromeUpdateURL
	prevEdge := browserExtensionEdgeUpdateURL
	browserExtensionChromeUpdateURL = companionChromeStoreUpdateURL
	browserExtensionEdgeUpdateURL = ""
	t.Cleanup(func() {
		browserExtensionChromeUpdateURL = prevChrome
		browserExtensionEdgeUpdateURL = prevEdge
	})
	got := (&App{}).InstallBrowserExtensions()
	if by := statusByBrowser(got); by["chrome"].State != browserExtStateNeedsEnable || by["chrome"].Detail != browserExtDetailStoreEnable {
		t.Fatalf("chrome=%#v", by["chrome"])
	}
	views := map[string]bool{}
	for _, op := range ops.ops {
		if strings.Contains(op.Path, `Extensions\`+browserExtensionID) {
			if op.Name != "update_url" || op.Value != companionChromeStoreUpdateURL {
				t.Fatalf("update_url name=%s value=%s", op.Name, op.Value)
			}
			views[op.View+" "+op.Path] = true
		}
	}
	if len(views) != 4 {
		t.Fatalf("extension views=%v", views)
	}

	linuxHome := t.TempDir()
	crx := filepath.Join(linuxHome, "maclaw-companion.crx")
	hookCompanionInstall(t, t.TempDir(), companionBrowserLayout{
		GOOS:      "linux",
		Home:      linuxHome,
		HostExe:   "/usr/bin/maclaw",
		ChromeExe: "/usr/bin/google-chrome",
		EdgeExe:   "/usr/bin/microsoft-edge",
		CRX:       crx,
	})
	if err := os.WriteFile(crx, []byte("crx"), 0o600); err != nil {
		t.Fatal(err)
	}
	linux := (&App{}).InstallBrowserExtensions()
	by := statusByBrowser(linux)
	if by["chrome"].State != browserExtStateNeedsEnable || by["chrome"].Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("linux chrome=%#v", by["chrome"])
	}
	if strings.TrimSpace(linux.UnpackedPath) == "" {
		t.Fatal("Linux Chrome with a store URL did not prepare the unpacked fallback")
	}
	if by["edge"].State != browserExtStateNeedsEnable || by["edge"].Detail != browserExtDetailEdgeSilent {
		t.Fatalf("linux edge=%#v", by["edge"])
	}
	chromeExternal := filepath.Join(linuxHome, ".config", "google-chrome", "External Extensions", browserExtensionID+".json")
	if _, err := os.Stat(chromeExternal); !os.IsNotExist(err) {
		t.Fatal("branded Chrome external extension was written")
	}
	edgeExternal := filepath.Join(linuxHome, ".config", "microsoft-edge", "External Extensions", browserExtensionID+".json")
	body, err := os.ReadFile(edgeExternal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"external_crx"`) || !strings.Contains(string(body), browserExtensionVersion) {
		t.Fatalf("edge json=%s", body)
	}
	info, err := os.Stat(edgeExternal)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(edgeExternal, old, old); err != nil {
		t.Fatal(err)
	}
	again := (&App{}).InstallBrowserExtensions()
	if !again.OK {
		t.Fatalf("second=%#v", again)
	}
	info2, err := os.Stat(edgeExternal)
	if err != nil {
		t.Fatal(err)
	}
	if info2.ModTime().After(old.Add(time.Minute)) {
		t.Fatalf("identical external json was rewritten: %s -> %s", info.ModTime(), info2.ModTime())
	}
	host := filepath.Join(linuxHome, ".config", "google-chrome", "NativeMessagingHosts", companionBrowserHostName+".json")
	if _, err := os.Stat(host); err != nil {
		t.Fatal(err)
	}
}

func TestInstallBrowserExtensionsSafariFailureDetails(t *testing.T) {
	dir := t.TempDir()
	hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS:      "darwin",
		Home:      t.TempDir(),
		HostExe:   "/Applications/MaClaw.app/Contents/MacOS/MaClaw",
		SafariApp: true,
	})
	unsigned := statusByBrowser((&App{}).InstallBrowserExtensions())
	if unsigned["safari"].State != browserExtStateFailed || unsigned["safari"].Detail != browserExtDetailSafariUnsigned {
		t.Fatalf("unsigned=%#v", unsigned["safari"])
	}
	hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS: "darwin", Home: t.TempDir(), HostExe: "/Applications/MaClaw.app/Contents/MacOS/MaClaw",
		SafariApp: true, SafariSigned: true, LoginItemAllowed: false,
	})
	login := statusByBrowser((&App{}).GetBrowserExtensionStatus())
	if login["safari"].Detail != browserExtDetailLoginItem {
		t.Fatalf("login=%#v", login["safari"])
	}
	hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS: "darwin", Home: t.TempDir(), HostExe: "/Applications/MaClaw.app/Contents/MacOS/MaClaw",
		SafariApp: true, SafariSigned: true, LoginItemAllowed: true,
	})
	check := statusByBrowser((&App{}).GetBrowserExtensionStatus())
	if check["safari"].State != browserExtStateNeedsEnable || check["safari"].Detail != browserExtDetailSafariCheck {
		t.Fatalf("checkbox=%#v", check["safari"])
	}
}

func TestShowBrowserExtensionLoadOpensExtensionsPage(t *testing.T) {
	dir := t.TempDir()
	hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS:      "windows",
		Home:      t.TempDir(),
		HostExe:   filepath.Join(dir, "MaClaw.exe"),
		ChromeExe: filepath.Join(dir, "chrome.exe"),
		EdgeExe:   filepath.Join(dir, "msedge.exe"),
	})
	if err := (&App{}).ShowBrowserExtensionLoad("safari"); err != os.ErrInvalid {
		t.Fatalf("safari=%v", err)
	}
	if err := (&App{}).ShowBrowserExtensionLoad("chrome"); !os.IsNotExist(err) {
		t.Fatalf("missing folder=%v", err)
	}
	extDir := writeCompanionExtensionFixture(t)
	prevExt := companionExtensionDirHook
	companionExtensionDirHook = func() (string, error) { return extDir, nil }
	prevChrome := browserExtensionChromeUpdateURL
	prevEdge := browserExtensionEdgeUpdateURL
	browserExtensionChromeUpdateURL = ""
	browserExtensionEdgeUpdateURL = ""
	t.Cleanup(func() {
		companionExtensionDirHook = prevExt
		browserExtensionChromeUpdateURL = prevChrome
		browserExtensionEdgeUpdateURL = prevEdge
	})
	if got := (&App{}).InstallBrowserExtensions(); !got.OK {
		t.Fatalf("install=%#v", got)
	}
	var launched []string
	prevLaunch := companionBrowserLaunchHook
	companionBrowserLaunchHook = func(exe, page string) error {
		launched = append(launched, exe+" "+page)
		return nil
	}
	prevStart := startShownItemCommand
	startShownItemCommand = func(cmd *exec.Cmd) error {
		t.Errorf("extensions page also opened explorer: %v", cmd.Args)
		return nil
	}
	t.Cleanup(func() {
		companionBrowserLaunchHook = prevLaunch
		startShownItemCommand = prevStart
	})
	if err := (&App{}).ShowBrowserExtensionLoad("chrome"); err != nil {
		t.Fatal(err)
	}
	if err := (&App{}).ShowBrowserExtensionLoad("edge"); err != nil {
		t.Fatal(err)
	}
	if len(launched) != 2 || !strings.HasSuffix(launched[0], " chrome://extensions/") || !strings.HasSuffix(launched[1], " edge://extensions/") {
		t.Fatalf("launched=%v", launched)
	}
}

type regCapture struct {
	ops []companionRegWrite
}

func hookCompanionInstall(t *testing.T, dir string, layout companionBrowserLayout) *regCapture {
	t.Helper()
	prevDir := browserExtensionStateDirHook
	prevLayout := companionBrowserLayoutHook
	prevReg := companionRegHook
	prevRun := companionBrowserRunningHook
	capture := &regCapture{}
	browserExtensionStateDirHook = func() (string, error) { return dir, nil }
	companionBrowserLayoutHook = func() (companionBrowserLayout, error) { return layout, nil }
	companionBrowserRunningHook = func(string) bool { return false }
	companionRegHook = func(op companionRegWrite) error {
		capture.ops = append(capture.ops, op)
		return nil
	}
	t.Cleanup(func() {
		browserExtensionStateDirHook = prevDir
		companionBrowserLayoutHook = prevLayout
		companionRegHook = prevReg
		companionBrowserRunningHook = prevRun
	})
	return capture
}

func statusByBrowser(result BrowserExtensionInstallResult) map[string]BrowserExtensionBrowserStatus {
	out := map[string]BrowserExtensionBrowserStatus{}
	for _, row := range result.Browsers {
		out[row.Browser] = row
	}
	return out
}

func writeCompanionExtensionFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	manifest := "{\n  \"manifest_version\": 3,\n  \"name\": \"Companion\",\n  \"version\": \"1.0.0\",\n  \"background\": {\"service_worker\": \"background.js\"}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "background.js"), []byte("void 0;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locale := filepath.Join(dir, "_locales", "zh_CN")
	if err := os.MkdirAll(locale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locale, "messages.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func bytesContainsArguments(body []byte) bool {
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		return true
	}
	_, ok := generic["arguments"]
	return ok
}

// TestInstallBrowserExtensionsFromEmbeddedAsset covers the installed build:
// there is no extensions/ folder next to the exe, so before the asset was
// embedded the card button wrote nothing and stayed on no-store-url. The
// install must now produce the folder, allow the unpacked extension's own id
// to reach the native host, and keep the folder on a later status read.
func TestInstallBrowserExtensionsFromEmbeddedAsset(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS:      "windows",
		Home:      home,
		HostExe:   filepath.Join(dir, "MaClaw.exe"),
		ChromeExe: `C:\Program Files\Google\Chrome\Application\chrome.exe`,
		EdgeExe:   `C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	})
	prevExt := companionExtensionDirHook
	companionExtensionDirHook = func() (string, error) { return "", nil }
	prevChrome := browserExtensionChromeUpdateURL
	prevEdge := browserExtensionEdgeUpdateURL
	browserExtensionChromeUpdateURL = ""
	browserExtensionEdgeUpdateURL = ""
	t.Cleanup(func() {
		companionExtensionDirHook = prevExt
		browserExtensionChromeUpdateURL = prevChrome
		browserExtensionEdgeUpdateURL = prevEdge
	})

	got := (&App{}).InstallBrowserExtensions()
	if !got.OK {
		t.Fatalf("install=%#v", got)
	}
	if strings.TrimSpace(got.UnpackedPath) == "" {
		t.Fatal("install did not report the unpacked folder")
	}
	rows := statusByBrowser(got)
	for _, browser := range []string{"chrome", "edge"} {
		if rows[browser].State != browserExtStateNeedsEnable || rows[browser].Detail != browserExtDetailLoadUnpacked {
			t.Fatalf("%s=%#v", browser, rows[browser])
		}
	}
	for _, rel := range []string{"manifest.json", "background.js", filepath.Join("_locales", "zh_CN", "messages.json")} {
		if !companionPathIsFile(filepath.Join(got.UnpackedPath, rel)) {
			t.Fatalf("unpacked folder is missing %s", rel)
		}
	}
	// The loaded copy has its own id from the dev key. Without that origin the
	// extension cannot reach the native host and Add to Companion does nothing.
	body, err := os.ReadFile(filepath.Join(dir, companionBrowserHostName+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var host companionHostManifest
	if err := json.Unmarshal(body, &host); err != nil {
		t.Fatal(err)
	}
	devID := companionDevExtensionID()
	if devID == "" {
		t.Fatal("no dev extension id")
	}
	found := false
	for _, origin := range host.AllowedOrigins {
		if origin == "chrome-extension://"+devID+"/" {
			found = true
		}
	}
	if !found {
		t.Fatalf("host manifest does not allow the unpacked id: %v", host.AllowedOrigins)
	}

	// A status read (what the card polls) has to keep the folder, so the
	// user can still copy it after reopening the page.
	status := (&App{}).GetBrowserExtensionStatus()
	if strings.TrimSpace(status.UnpackedPath) == "" {
		t.Fatalf("status lost the folder: %#v", status)
	}
	if row := statusByBrowser(status)["chrome"]; row.Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("status row=%#v", row)
	}
}

func TestInstallBrowserExtensionsLinuxChromeUsesUnpacked(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	hookCompanionInstall(t, dir, companionBrowserLayout{
		GOOS:      "linux",
		Home:      home,
		HostExe:   "/usr/bin/maclaw",
		ChromeExe: "/usr/bin/google-chrome",
	})
	prevExt := companionExtensionDirHook
	companionExtensionDirHook = func() (string, error) { return "", nil }
	prevChrome := browserExtensionChromeUpdateURL
	prevEdge := browserExtensionEdgeUpdateURL
	browserExtensionChromeUpdateURL = ""
	browserExtensionEdgeUpdateURL = ""
	t.Cleanup(func() {
		companionExtensionDirHook = prevExt
		browserExtensionChromeUpdateURL = prevChrome
		browserExtensionEdgeUpdateURL = prevEdge
	})

	got := (&App{}).InstallBrowserExtensions()
	if !got.OK || strings.TrimSpace(got.UnpackedPath) == "" {
		t.Fatalf("install=%#v", got)
	}
	row := statusByBrowser(got)["chrome"]
	if row.State != browserExtStateNeedsEnable || row.Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("chrome=%#v", row)
	}
	host := filepath.Join(home, ".config", "google-chrome", "NativeMessagingHosts", companionBrowserHostName+".json")
	if !companionPathIsFile(host) {
		t.Fatal("Linux Chrome native host was not written")
	}
	external := filepath.Join(home, ".config", "google-chrome", "External Extensions", browserExtensionID+".json")
	if _, err := os.Stat(external); !os.IsNotExist(err) {
		t.Fatal("Linux Chrome used an external extension registration")
	}
	status := (&App{}).GetBrowserExtensionStatus()
	if strings.TrimSpace(status.UnpackedPath) == "" || statusByBrowser(status)["chrome"].Detail != browserExtDetailLoadUnpacked {
		t.Fatalf("status=%#v", status)
	}

	var launched []string
	prevLaunch := companionBrowserLaunchHook
	companionBrowserLaunchHook = func(exe, page string) error {
		launched = append(launched, exe+" "+page)
		return nil
	}
	t.Cleanup(func() { companionBrowserLaunchHook = prevLaunch })
	if err := (&App{}).ShowBrowserExtensionLoad("chrome"); err != nil {
		t.Fatal(err)
	}
	if len(launched) != 1 || launched[0] != "/usr/bin/google-chrome chrome://extensions/" {
		t.Fatalf("launched=%v", launched)
	}
}

func TestInstallBrowserExtensionsLinuxPackageFailureIsRetryable(t *testing.T) {
	for _, browser := range []string{"chrome", "edge"} {
		t.Run(browser, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(statePath, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			layout := companionBrowserLayout{
				GOOS:    "linux",
				Home:    t.TempDir(),
				HostExe: "/usr/bin/maclaw",
			}
			if browser == "chrome" {
				layout.ChromeExe = "/usr/bin/google-chrome"
			} else {
				layout.EdgeExe = "/usr/bin/microsoft-edge"
			}
			hookCompanionInstall(t, statePath, layout)
			prevChrome := browserExtensionChromeUpdateURL
			prevEdge := browserExtensionEdgeUpdateURL
			browserExtensionChromeUpdateURL = ""
			browserExtensionEdgeUpdateURL = ""
			t.Cleanup(func() {
				browserExtensionChromeUpdateURL = prevChrome
				browserExtensionEdgeUpdateURL = prevEdge
			})

			got := (&App{}).InstallBrowserExtensions()
			if got.OK {
				t.Fatalf("install unexpectedly succeeded: %#v", got)
			}
			row := statusByBrowser(got)[browser]
			if row.State != browserExtStateFailed || row.Detail != browserExtDetailRegisterFailed {
				t.Fatalf("%s=%#v", browser, row)
			}
		})
	}
}
