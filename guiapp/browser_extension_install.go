package guiapp

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
)

const (
	browserExtStateAbsent       = "absent"
	browserExtStateNotInstalled = "not-installed"
	browserExtStateFailed       = "failed"
	browserExtStateInstalled    = "installed"
	browserExtStateNeedsRestart = "needs-restart"
	browserExtStateNeedsEnable  = "needs-enable"

	browserExtInstallUnpacked = "unpacked"
	browserExtInstallStore    = "store"

	browserExtDetailNotInstalled   = "not-installed"
	browserExtDetailInstalled      = "installed"
	browserExtDetailNeedsRestart   = "needs-restart"
	browserExtDetailVersion        = "version-mismatch"
	browserExtDetailEdgeSilent     = "edge-silent"
	browserExtDetailSafariCheck    = "safari-checkbox"
	browserExtDetailStoreEnable    = "store-enable"
	browserExtDetailNoStore        = "no-store-url"
	browserExtDetailLoadUnpacked   = "load-unpacked"
	browserExtDetailLinuxChrome    = "linux-chrome"
	browserExtDetailNoCRX          = "no-crx"
	browserExtDetailSafariUnsigned = "safari-unsigned"
	browserExtDetailLoginItem      = "login-item"
	browserExtDetailRegisterFailed = "register-failed"
)

// BrowserExtensionBrowserStatus is one row on the 浏览器插件 card.
type BrowserExtensionBrowserStatus struct {
	Browser string `json:"browser"`
	State   string `json:"state"`
	Detail  string `json:"detail,omitempty"`
}

// BrowserExtensionInstallResult is the card payload. Absent browsers are omitted.
// UnpackedPath is the folder a no-store build asks the user to load.
type BrowserExtensionInstallResult struct {
	OK           bool                            `json:"ok"`
	Browsers     []BrowserExtensionBrowserStatus `json:"browsers"`
	UnpackedPath string                          `json:"unpackedPath,omitempty"`
}

// companionBrowserLayoutHook replaces detection in tests so they do not read
// the machine's browsers or write the real user profile.
var companionBrowserLayoutHook func() (companionBrowserLayout, error)

// companionBrowserRunningHook replaces process detection in tests.
var companionBrowserRunningHook func(browser string) bool

// companionRegHook replaces HKCU writes. Tests set it so the suite does not
// touch the signed-in user's registry. HKLM stays with the installer.
var companionRegHook func(companionRegWrite) error

// companionBrowserLaunchHook replaces opening the extensions page in tests.
var companionBrowserLaunchHook func(exe, page string) error

type companionRegWrite struct {
	Delete bool
	View   string
	Path   string
	Name   string
	Value  string
}

type companionBrowserLayout struct {
	GOOS             string
	Home             string
	HostExe          string
	ChromeExe        string
	EdgeExe          string
	SafariApp        bool
	ChromeRunning    bool
	EdgeRunning      bool
	CRX              string
	SafariSigned     bool
	LoginItemAllowed bool
	SafariEnabled    bool
}

type companionBrowserSpec struct {
	Present       bool
	Failure       string
	SilentEdge    bool
	SafariEnabled bool
	LocalSideload bool
}

func (a *App) InstallBrowserExtensions() BrowserExtensionInstallResult {
	return runCompanionBrowserInstall(true)
}

func (a *App) GetBrowserExtensionStatus() BrowserExtensionInstallResult {
	return runCompanionBrowserInstall(false)
}

func (a *App) UninstallBrowserExtensions() BrowserExtensionInstallResult {
	layout, err := detectCompanionBrowserLayout()
	result := BrowserExtensionInstallResult{OK: err == nil}
	if err != nil {
		logBrowserExtension("*", "uninstall", "detect-failed")
		result.OK = false
		return result
	}
	if err := removeCompanionBrowserRegistration(layout); err != nil {
		logBrowserExtension("*", "uninstall", "failed")
		result.OK = false
		return result
	}
	logBrowserExtension("*", "uninstall", "ok")
	return runCompanionBrowserInstall(false)
}

func runCompanionBrowserInstall(write bool) BrowserExtensionInstallResult {
	layout, err := detectCompanionBrowserLayout()
	if err != nil {
		return BrowserExtensionInstallResult{OK: false}
	}
	ok := true
	local := companionLocalPackage{}
	var packErr error
	if write {
		local, packErr = prepareCompanionLocalPackage(layout)
		if packErr != nil {
			logBrowserExtension("*", "local-crx", "failed")
			ok = false
		}
	} else {
		local = existingCompanionLocalPackage()
	}
	var rows []BrowserExtensionBrowserStatus
	for _, browser := range []string{"chrome", "edge", "safari"} {
		spec := companionBrowserSpecFor(layout, browser, local)
		if packErr != nil && (spec.Failure == browserExtDetailNoStore || spec.Failure == browserExtDetailLinuxChrome || spec.Failure == browserExtDetailNoCRX) {
			spec.Failure = browserExtDetailRegisterFailed
		}
		if browser == "safari" && !spec.Present {
			continue
		}
		if !spec.Present {
			rows = append(rows, classifyCompanionBrowser(browser, spec, nil))
			continue
		}
		if write && browser != "safari" {
			if err := writeCompanionNativeHost(layout, browser, local.ID); err != nil {
				logBrowserExtension(browser, "native-host", "failed")
				if spec.Failure == "" {
					spec.Failure = browserExtDetailRegisterFailed
				}
				ok = false
			} else {
				logBrowserExtension(browser, "native-host", "ok")
			}
			if spec.Failure == "" {
				if err := writeCompanionExternal(layout, browser, local); err != nil {
					logBrowserExtension(browser, "external", "failed")
					spec.Failure = browserExtDetailRegisterFailed
					ok = false
				} else if companionExternalWritten(layout, browser, local) {
					logBrowserExtension(browser, "external", "ok")
				}
			}
			running := browser == "chrome" && layout.ChromeRunning || browser == "edge" && layout.EdgeRunning
			// A failed write did not finish this registration. Stamping unpacked
			// here would keep a later packed-CRX hello and hide the folder.
			mode := ""
			if spec.Failure == "" {
				if spec.LocalSideload {
					mode = browserExtInstallUnpacked
				} else if companionStoreURL(browser) != "" {
					mode = browserExtInstallStore
				}
			}
			if err := recordBrowserExtensionInstall(browser, running, mode); err != nil {
				logBrowserExtension(browser, "native-host", "state-failed")
				if spec.Failure == "" {
					spec.Failure = browserExtDetailRegisterFailed
				}
				ok = false
			}
		}
		var rec *browserExtensionBrowserRecord
		if dir, dirErr := browserExtensionStateDir(); dirErr == nil {
			state := readBrowserExtensionState(dir)
			rec = state.record(browser)
		}
		rows = append(rows, classifyCompanionBrowser(browser, spec, rec))
	}
	if rows == nil {
		rows = []BrowserExtensionBrowserStatus{}
	}
	result := BrowserExtensionInstallResult{OK: ok, Browsers: rows}
	if local.Ready {
		result.UnpackedPath = local.Dir
	}
	return result
}

func classifyCompanionBrowser(browser string, spec companionBrowserSpec, rec *browserExtensionBrowserRecord) BrowserExtensionBrowserStatus {
	row := BrowserExtensionBrowserStatus{Browser: browser}
	if !spec.Present {
		if browser == "safari" {
			row.State = browserExtStateAbsent
			return row
		}
		row.State = browserExtStateNotInstalled
		row.Detail = browserExtDetailNotInstalled
		return row
	}
	if spec.Failure != "" {
		row.State = browserExtStateFailed
		row.Detail = spec.Failure
		return row
	}
	if browser == "safari" {
		if spec.SafariEnabled {
			row.State = browserExtStateInstalled
			row.Detail = browserExtDetailInstalled
			return row
		}
		row.State = browserExtStateNeedsEnable
		row.Detail = browserExtDetailSafariCheck
		return row
	}
	version := ""
	running := false
	helloFresh := false
	if rec != nil {
		version = strings.TrimSpace(rec.Version)
		running = rec.BrowserRunningAtRegister
		hello, helloErr := time.Parse(time.RFC3339, rec.HelloAt)
		registered, regErr := time.Parse(time.RFC3339, rec.RegisteredAt)
		helloFresh = helloErr == nil && regErr == nil && hello.After(registered)
	}
	// A matching hello counts only for the registration that produced it.
	// Local sideload drops the packed CRX, so a hello from that CRX is not installed.
	if version != "" && version == browserExtensionVersion && unpackedHelloCounts(spec, rec) {
		row.State = browserExtStateInstalled
		row.Detail = browserExtDetailInstalled
		return row
	}
	// A packed off-store extension never reaches hello: the browser disables it
	// and shows no enable prompt. The folder instructions matter more than a
	// restart or a stale hello version.
	if spec.LocalSideload {
		row.State = browserExtStateNeedsEnable
		row.Detail = browserExtDetailLoadUnpacked
		return row
	}
	if running && !helloFresh {
		row.State = browserExtStateNeedsRestart
		row.Detail = browserExtDetailNeedsRestart
		return row
	}
	row.State = browserExtStateNeedsEnable
	switch {
	case version != "" && version != browserExtensionVersion:
		row.Detail = browserExtDetailVersion
	case spec.SilentEdge:
		row.Detail = browserExtDetailEdgeSilent
	default:
		row.Detail = browserExtDetailStoreEnable
	}
	return row
}

func companionBrowserSpecFor(layout companionBrowserLayout, browser string, local companionLocalPackage) companionBrowserSpec {
	var spec companionBrowserSpec
	switch browser {
	case "chrome":
		spec.Present = strings.TrimSpace(layout.ChromeExe) != ""
	case "edge":
		spec.Present = strings.TrimSpace(layout.EdgeExe) != ""
	case "safari":
		if layout.GOOS != "darwin" || !layout.SafariApp {
			return companionBrowserSpec{}
		}
		spec.Present = true
		if !layout.SafariSigned {
			spec.Failure = browserExtDetailSafariUnsigned
			return spec
		}
		if !layout.LoginItemAllowed {
			spec.Failure = browserExtDetailLoginItem
			return spec
		}
		spec.SafariEnabled = layout.SafariEnabled
		return spec
	}
	if !spec.Present {
		return spec
	}
	if layout.GOOS == "linux" {
		if browser == "chrome" {
			if local.Ready {
				spec.LocalSideload = true
			} else {
				spec.Failure = browserExtDetailLinuxChrome
			}
			return spec
		}
		crx := strings.TrimSpace(layout.CRX)
		if (crx == "" || !companionPathIsFile(crx)) && !local.Ready {
			spec.Failure = browserExtDetailNoCRX
			return spec
		}
		if local.Ready && (crx == "" || !companionPathIsFile(crx)) {
			spec.LocalSideload = true
		}
		spec.SilentEdge = !layout.EdgeRunning
		return spec
	}
	if companionStoreURL(browser) == "" {
		if local.Ready {
			spec.LocalSideload = true
			return spec
		}
		spec.Failure = browserExtDetailNoStore
	}
	return spec
}

func companionStoreURL(browser string) string {
	chrome := strings.TrimSpace(browserExtensionChromeUpdateURL)
	edge := strings.TrimSpace(browserExtensionEdgeUpdateURL)
	if browser == "edge" && edge == "" {
		edge = chrome
	}
	chosen := chrome
	if browser == "edge" {
		chosen = edge
	}
	if chosen == companionChromeStoreUpdateURL || chosen == companionEdgeStoreUpdateURL {
		return chosen
	}
	if chosen != "" {
		logBrowserExtension(browser, "external", "rejected-url")
	}
	return ""
}

func detectCompanionBrowserLayout() (companionBrowserLayout, error) {
	if companionBrowserLayoutHook != nil {
		return companionBrowserLayoutHook()
	}
	home, _ := os.UserHomeDir()
	host, _ := companionHostExecutable()
	layout := companionBrowserLayout{
		GOOS:          runtime.GOOS,
		Home:          home,
		HostExe:       host,
		ChromeExe:     findCompanionBrowser("chrome", home),
		EdgeExe:       findCompanionBrowser("edge", home),
		SafariApp:     runtime.GOOS == "darwin" && companionPathIsDir("/Applications/Safari.app"),
		ChromeRunning: browserProcessRunning("chrome"),
		EdgeRunning:   browserProcessRunning("edge"),
	}
	if runtime.GOOS == "linux" {
		layout.CRX = defaultCompanionCRXPath(home)
	}
	return layout, nil
}

func companionHostExecutable() (string, error) {
	exe, err := companionExecutable()
	if err != nil {
		return "", err
	}
	name := "maclaw-companion-host"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	helper := filepath.Join(filepath.Dir(exe), name)
	if companionPathIsFile(helper) {
		return helper, nil
	}
	return exe, nil
}

func findCompanionBrowser(browser, home string) string {
	switch runtime.GOOS {
	case "windows":
		return firstCompanionFile(windowsBrowserCandidates(browser))
	case "darwin":
		return firstCompanionFile(darwinBrowserCandidates(browser, home))
	default:
		return lookPathCompanionBrowser(browser)
	}
}

func windowsBrowserCandidates(browser string) []string {
	exe := "chrome.exe"
	dir := filepath.Join("Google", "Chrome", "Application")
	if browser == "edge" {
		exe = "msedge.exe"
		dir = filepath.Join("Microsoft", "Edge", "Application")
	}
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if root := os.Getenv(env); root != "" {
			roots = append(roots, filepath.Join(root, dir, exe))
		}
	}
	if fromAppPath := companionAppPath(exe); fromAppPath != "" {
		roots = append(roots, fromAppPath)
	}
	return roots
}

func darwinBrowserCandidates(browser, home string) []string {
	name := "Google Chrome.app"
	if browser == "edge" {
		name = "Microsoft Edge.app"
	}
	rel := filepath.Join(name, "Contents", "MacOS", strings.TrimSuffix(name, ".app"))
	return []string{
		filepath.Join("/Applications", rel),
		filepath.Join(home, "Applications", rel),
	}
}

func lookPathCompanionBrowser(browser string) string {
	names := []string{"google-chrome", "google-chrome-stable"}
	if browser == "edge" {
		names = []string{"microsoft-edge", "microsoft-edge-stable"}
	}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil && path != "" {
			return path
		}
	}
	return ""
}

func defaultCompanionCRXPath(home string) string {
	data := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	id := strings.TrimSpace(brand.Current().ID)
	if id == "" {
		id = "maclaw"
	}
	return filepath.Join(data, id, "browser-extension", "maclaw-companion.crx")
}

func browserProcessRunning(browser string) bool {
	if companionBrowserRunningHook != nil {
		return companionBrowserRunningHook(browser)
	}
	return browserProcessRunningOS(companionBrowserImage(runtime.GOOS, browser))
}

func companionBrowserImage(goos, browser string) string {
	switch goos {
	case "windows":
		if browser == "edge" {
			return "msedge.exe"
		}
		return "chrome.exe"
	case "darwin":
		if browser == "edge" {
			return "Microsoft Edge"
		}
		return "Google Chrome"
	default:
		if browser == "edge" {
			return "microsoft-edge"
		}
		return "google-chrome"
	}
}

func firstCompanionFile(paths []string) string {
	for _, path := range paths {
		if companionPathIsFile(path) {
			return path
		}
	}
	return ""
}

func companionPathIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info != nil && !info.IsDir()
}

func companionPathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info != nil && info.IsDir()
}

func unpackedHelloCounts(spec companionBrowserSpec, rec *browserExtensionBrowserRecord) bool {
	if !spec.LocalSideload {
		return true
	}
	return rec != nil && rec.InstallMode == browserExtInstallUnpacked
}

func recordBrowserExtensionInstall(browser string, running bool, mode string) error {
	if browser != "chrome" && browser != "edge" {
		return nil
	}
	dir, err := browserExtensionStateDir()
	if err != nil {
		return err
	}
	return updateBrowserExtensionState(dir, func(state *browserExtensionStateFile) error {
		rec := state.record(browser)
		if rec == nil {
			rec = &browserExtensionBrowserRecord{}
			state.setRecord(browser, rec)
		}
		// The first unpacked registration invalidates a hello from the packed CRX.
		// A later install of the same folder keeps the hello the unpacked worker sent.
		if mode == browserExtInstallUnpacked && rec.InstallMode != browserExtInstallUnpacked {
			rec.Version = ""
			rec.HelloAt = ""
		}
		if mode != "" {
			rec.InstallMode = mode
		}
		rec.RegisteredAt = time.Now().UTC().Format(time.RFC3339)
		rec.BrowserRunningAtRegister = running
		return nil
	})
}

func clearBrowserExtensionRegistration() error {
	dir, err := browserExtensionStateDir()
	if err != nil {
		return err
	}
	return withBrowserExtensionStateLock(dir, func() error {
		return clearBrowserExtensionState(dir)
	})
}

type companionHostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

func companionHostManifestJSON(exe, extraID string) ([]byte, error) {
	origins := []string{"chrome-extension://" + browserExtensionID + "/"}
	extraID = strings.TrimSpace(extraID)
	if extraID != "" && extraID != browserExtensionID {
		origins = append(origins, "chrome-extension://"+extraID+"/")
	}
	body, err := json.MarshalIndent(companionHostManifest{
		Name:           companionBrowserHostName,
		Description:    "Companion paper",
		Path:           exe,
		Type:           "stdio",
		AllowedOrigins: origins,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func writeCompanionNativeHost(layout companionBrowserLayout, browser, extraID string) error {
	exe := strings.TrimSpace(layout.HostExe)
	if exe == "" {
		var err error
		exe, err = companionHostExecutable()
		if err != nil || exe == "" {
			return os.ErrNotExist
		}
	}
	body, err := companionHostManifestJSON(exe, extraID)
	if err != nil {
		return err
	}
	if !json.Valid(body) || bytes.Contains(body, []byte(`"arguments"`)) {
		return os.ErrInvalid
	}
	switch layout.GOOS {
	case "windows":
		dir, err := browserExtensionStateDir()
		if err != nil {
			return err
		}
		path := filepath.Join(dir, companionBrowserHostName+".json")
		if err := writeCompanionFile(path, body); err != nil {
			return err
		}
		return writeCompanionHKCU(browser, "native", "", path, "")
	default:
		path := companionNativeHostPath(layout, browser)
		return writeCompanionFile(path, body)
	}
}

func writeCompanionExternal(layout companionBrowserLayout, browser string, local companionLocalPackage) error {
	if layout.GOOS == "linux" {
		if browser != "edge" {
			return nil
		}
		// A CRX shipped beside the Linux package still uses external_crx.
		// A dev build has no such file: do not register the packed copy Edge disables.
		crx := strings.TrimSpace(layout.CRX)
		if crx != "" && companionPathIsFile(crx) {
			return writeCompanionExternalCRX(layout, browser, browserExtensionID, crx, browserExtensionVersion)
		}
		if !local.Ready {
			return nil
		}
		return deleteCompanionDevExtension(layout, browser, local.ID)
	}
	if updateURL := companionStoreURL(browser); updateURL != "" {
		if layout.GOOS == "windows" {
			if err := writeCompanionHKCU(browser, "external", "update_url", updateURL, browserExtensionID); err != nil {
				return err
			}
		} else {
			payload, err := json.MarshalIndent(map[string]string{"external_update_url": updateURL}, "", "  ")
			if err != nil {
				return err
			}
			if err := writeCompanionFile(companionExternalFile(layout, browser, browserExtensionID), append(payload, '\n')); err != nil {
				return err
			}
		}
		return deleteCompanionDevExtension(layout, browser, local.ID)
	}
	if !local.Ready {
		return nil
	}
	// path+version and external_crx set may_be_untrusted. Edge and Chrome then
	// disable the extension and do not let the user turn it on. Drop that
	// registration. The unpacked folder is loaded with developer mode.
	return deleteCompanionDevExtension(layout, browser, local.ID)
}

func writeCompanionExternalCRX(layout companionBrowserLayout, browser, id, crx, version string) error {
	payload, err := json.MarshalIndent(map[string]string{
		"external_crx":     crx,
		"external_version": version,
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeCompanionFile(companionExternalFile(layout, browser, id), append(payload, '\n'))
}

func companionExternalWritten(layout companionBrowserLayout, browser string, local companionLocalPackage) bool {
	if layout.GOOS == "linux" {
		if browser != "edge" {
			return false
		}
		return companionPathIsFile(strings.TrimSpace(layout.CRX)) || local.Ready
	}
	if companionStoreURL(browser) != "" {
		return true
	}
	return local.Ready
}

func companionBrowserConfigDir(goos, home, browser string) string {
	switch goos {
	case "windows":
		return ""
	case "darwin":
		if browser == "edge" {
			return filepath.Join(home, "Library", "Application Support", "Microsoft Edge")
		}
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	default:
		if browser == "edge" {
			return filepath.Join(home, ".config", "microsoft-edge")
		}
		return filepath.Join(home, ".config", "google-chrome")
	}
}

func companionNativeHostPath(layout companionBrowserLayout, browser string) string {
	base := companionBrowserConfigDir(layout.GOOS, layout.Home, browser)
	return filepath.Join(base, "NativeMessagingHosts", companionBrowserHostName+".json")
}

func companionExternalPath(layout companionBrowserLayout, browser string) string {
	return companionExternalFile(layout, browser, browserExtensionID)
}

func companionExternalFile(layout companionBrowserLayout, browser, id string) string {
	base := companionBrowserConfigDir(layout.GOOS, layout.Home, browser)
	if strings.TrimSpace(id) == "" {
		id = browserExtensionID
	}
	return filepath.Join(base, "External Extensions", id+".json")
}

func writeCompanionHKCU(browser, kind, name, value, extensionID string) error {
	root := `Software\Google\Chrome`
	if browser == "edge" {
		root = `Software\Microsoft\Edge`
	}
	path := root + `\NativeMessagingHosts\` + companionBrowserHostName
	if kind == "external" {
		if strings.TrimSpace(extensionID) == "" {
			extensionID = browserExtensionID
		}
		path = root + `\Extensions\` + extensionID
	}
	for _, view := range []string{"64", "32"} {
		if err := writeHKCUString(view, path, name, value); err != nil {
			return err
		}
	}
	return nil
}

func removeCompanionBrowserRegistration(layout companionBrowserLayout) error {
	if layout.GOOS == "windows" {
		for _, browser := range []string{"chrome", "edge"} {
			if err := deleteCompanionHKCU(browser); err != nil {
				return err
			}
		}
	}
	for _, path := range companionRegistrationFiles(layout) {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if dir, err := browserExtensionStateDir(); err == nil && dir != "" {
		unpacked := filepath.Join(dir, companionUnpackedDirName)
		if err := os.RemoveAll(unpacked); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return clearBrowserExtensionRegistration()
}

// RevealBrowserExtensionFolder opens the unpacked extension directory.
func (a *App) RevealBrowserExtensionFolder() error {
	local := existingCompanionLocalPackage()
	if !local.Ready || strings.TrimSpace(local.Dir) == "" {
		return os.ErrNotExist
	}
	return revealCompanionFolder(local.Dir)
}

func revealCompanionFolder(path string) error {
	switch runtime.GOOS {
	case "windows":
		// Highlight the folder in its parent. Load unpacked needs that folder.
		// Explorer treats /select,path as one switch, so the path is quoted
		// by applyExplorerSelect rather than as a separate argument.
		exe := windowsSystemExecutable("explorer.exe")
		cmd := exec.Command(exe)
		applyExplorerSelect(cmd, exe, path)
		hideCommandWindow(cmd)
		return startShownItemCommand(cmd)
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// ShowBrowserExtensionLoad opens one browser's extensions page. A registry
// path does not load this folder, and a packed CRX is disabled before its
// service worker can create the context menu. Explorer is not opened here:
// its window is created on its own schedule and covers Developer mode and
// Load unpacked. The folder path stays on the card, and Show folder selects it.
// This does not install the extension.
func (a *App) ShowBrowserExtensionLoad(browser string) error {
	if browser != "chrome" && browser != "edge" {
		return os.ErrInvalid
	}
	local := existingCompanionLocalPackage()
	if !local.Ready || strings.TrimSpace(local.Dir) == "" {
		return os.ErrNotExist
	}
	layout, err := detectCompanionBrowserLayout()
	if err != nil {
		return err
	}
	return openCompanionExtensionsPage(layout, browser)
}

func openCompanionExtensionsPage(layout companionBrowserLayout, browser string) error {
	exe := layout.ChromeExe
	page := "chrome://extensions/"
	if browser == "edge" {
		exe = layout.EdgeExe
		page = "edge://extensions/"
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return os.ErrNotExist
	}
	if companionBrowserLaunchHook != nil {
		return companionBrowserLaunchHook(exe, page)
	}
	return openCompanionExtensionsPageOS(exe, page)
}

func deleteCompanionHKCU(browser string) error {
	root := `Software\Google\Chrome`
	if browser == "edge" {
		root = `Software\Microsoft\Edge`
	}
	paths := []string{
		root + `\NativeMessagingHosts\` + companionBrowserHostName,
		root + `\Extensions\` + browserExtensionID,
	}
	if devID := companionDevExtensionID(); devID != "" && devID != browserExtensionID {
		paths = append(paths, root+`\Extensions\`+devID)
	}
	for _, view := range []string{"64", "32"} {
		for _, path := range paths {
			if err := deleteHKCUKey(view, path); err != nil {
				return err
			}
		}
	}
	return nil
}

// deleteCompanionDevExtension removes a previous packed sideload under the dev
// id. It does not remove the native host or the store id.
func deleteCompanionDevExtension(layout companionBrowserLayout, browser, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		id = companionDevExtensionID()
	}
	if id == "" || id == browserExtensionID {
		return nil
	}
	if layout.GOOS == "windows" {
		root := `Software\Google\Chrome`
		if browser == "edge" {
			root = `Software\Microsoft\Edge`
		}
		path := root + `\Extensions\` + id
		for _, view := range []string{"64", "32"} {
			if err := deleteHKCUKey(view, path); err != nil {
				return err
			}
		}
		return nil
	}
	path := companionExternalFile(layout, browser, id)
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func companionRegistrationFiles(layout companionBrowserLayout) []string {
	var paths []string
	if dir, err := browserExtensionStateDir(); err == nil && dir != "" {
		paths = append(paths, filepath.Join(dir, companionBrowserHostName+".json"))
	}
	ids := []string{browserExtensionID}
	if devID := companionDevExtensionID(); devID != "" && devID != browserExtensionID {
		ids = append(ids, devID)
	}
	for _, browser := range []string{"chrome", "edge"} {
		if companionBrowserConfigDir(layout.GOOS, layout.Home, browser) == "" {
			continue
		}
		paths = append(paths, companionNativeHostPath(layout, browser))
		for _, id := range ids {
			paths = append(paths, companionExternalFile(layout, browser, id))
		}
	}
	if dir, err := browserExtensionStateDir(); err == nil && dir != "" {
		paths = append(paths, filepath.Join(dir, companionLocalCRXName))
	}
	return paths
}

func writeCompanionFile(path string, body []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, body) {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o755)
	tmp, err := os.CreateTemp(dir, ".companion-reg-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(body)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	_ = os.Chmod(tmpName, 0o600)
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Chmod(path, 0o600)
}

func logBrowserExtension(browser, action, result string) {
	log.Printf("[browser-ext] browser=%s action=%s result=%s", browser, action, result)
}
