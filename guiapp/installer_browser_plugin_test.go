package guiapp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerDetectsBrowsersBeforePluginInstall(t *testing.T) {
	nsi := readRepoFile(t, filepath.Join("build", "windows", "installer", "multiarch.nsi"))
	for _, needle := range []string{
		"Function CompanionInstallBrowserPlugins",
		"Function CompanionFindBrowser",
		"Call CompanionInstallBrowserPlugins",
		"Call un.CompanionRemoveBrowserPlugins",
		"未检测到 Google Chrome，跳过",
		"未检测到 Microsoft Edge，跳过",
		"未检测到 Chrome 或 Edge，不写消息通道。",
		"Windows 安装包不处理 Safari",
		browserExtensionID,
		companionBrowserHostName,
		companionChromeStoreUpdateURL,
		companionEdgeStoreUpdateURL,
		"com.maclaw.companion.json",
		`NativeMessagingHosts\${COMPANION_BROWSER_HOST_NAME}`,
	} {
		if !strings.Contains(nsi, needle) {
			t.Fatalf("installer missing %s", needle)
		}
	}
	if strings.Contains(nsi, `Software\Policies`) {
		t.Fatal("installer writes a browser policy hive")
	}
	findFn := nsiFunction(t, nsi, "CompanionFindBrowser")
	for _, needle := range []string{"$PROGRAMFILES32", "$PROGRAMFILES64", "$LOCALAPPDATA", "CompanionExistingExe"} {
		if !strings.Contains(findFn, needle) {
			t.Fatalf("browser finder missing %s", needle)
		}
	}
	regFn := nsiFunction(t, nsi, "CompanionRegisterDetectedBrowser")
	if strings.Contains(regFn, "WriteRegStr HKCU") || strings.Contains(regFn, "WriteRegDWORD HKCU") {
		t.Fatal("elevated installer writes HKCU")
	}
	installFn := nsiFunction(t, nsi, "CompanionInstallBrowserPlugins")
	if strings.Contains(installFn, "WriteRegStr HKCU") {
		t.Fatal("install function writes HKCU")
	}
	manifestFn := nsiFunction(t, nsi, "CompanionWriteHostManifest")
	for _, needle := range []string{"WideCharToMultiByte", "CreateFileW(w r6,", "WriteFile"} {
		if !strings.Contains(manifestFn, needle) {
			t.Fatalf("host manifest writer missing %s", needle)
		}
	}
	if strings.Contains(manifestFn, "FileWrite") || strings.Contains(manifestFn, "FileOpen") {
		t.Fatal("host manifest uses FileWrite, which writes the system ANSI code page instead of UTF-8")
	}
	if !strings.Contains(installFn, "StrCmp $0 \"\" companion_install_pop") {
		t.Fatal("install continues after the host manifest fails to write")
	}
	uninstallAt := strings.Index(nsi, `Section "uninstall"`)
	callAt := strings.Index(nsi, "Call CompanionInstallBrowserPlugins")
	if callAt < 0 || uninstallAt < 0 || callAt > uninstallAt {
		t.Fatal("browser plugin install is not in the install section")
	}
	removeAt := strings.LastIndex(nsi, "Call un.CompanionRemoveBrowserPlugins")
	if removeAt < uninstallAt {
		t.Fatal("browser plugin removal is not in the uninstall section")
	}
	uninstall := nsi[uninstallAt:]
	if strings.Count(uninstall, "Call un.CompanionRemoveBrowserPlugins") != 2 {
		t.Fatal("uninstall must remove the plugin from both registry views")
	}
	rmdirAt := strings.Index(uninstall, `RMDir /r "$INSTDIR"`)
	firstRemove := strings.Index(uninstall, "Call un.CompanionRemoveBrowserPlugins")
	if firstRemove < 0 || rmdirAt < 0 || firstRemove > rmdirAt {
		t.Fatal("plugin removal runs after the install directory is deleted")
	}

	canonical := readRepoFile(t, filepath.Join("build", "scripts", "install-companion-browser.sh"))
	for _, needle := range []string{
		"Google Chrome.app",
		"Microsoft Edge.app",
		"Safari.app",
		"is not installed, skipping",
		"does not exist yet, skipping registration",
		"does not include a Safari extension yet",
		"companion_profile_ready",
		"s/^NFSHomeDirectory: //",
		"json_escape",
		"chmod go-w",
		"Chrome store file was not written",
		"Edge store file was not written",
		"host manifest was not written",
		browserExtensionID,
		"NativeMessagingHosts",
	} {
		if !strings.Contains(canonical, needle) {
			t.Fatalf("mac browser script missing %s", needle)
		}
	}
	if strings.Contains(canonical, "enable the extension in Safari settings") {
		t.Fatal("mac script tells the user to enable a Safari extension this package does not include")
	}
	if strings.Contains(canonical, "awk '{print $2}'") {
		t.Fatal("mac script truncates a home directory that contains a space")
	}
	for _, rel := range []string{
		filepath.Join("build", "scripts_x64"),
		filepath.Join("build", "scripts_arm64"),
		filepath.Join("build", "scripts_universal"),
	} {
		post := readRepoFile(t, filepath.Join(rel, "postinstall"))
		if !strings.Contains(post, "install-companion-browser.sh") {
			t.Fatalf("%s does not source the browser script", rel)
		}
		copy := readRepoFile(t, filepath.Join(rel, "install-companion-browser.sh"))
		if copy != canonical {
			t.Fatalf("%s browser script drifted from build/scripts/install-companion-browser.sh", rel)
		}
	}

	linux := readRepoFile(t, filepath.Join("build", "linux", "install-companion-browser.sh"))
	for _, needle := range []string{
		"Google Chrome is not installed, skipping",
		"Microsoft Edge is not installed, skipping",
		"Safari is not installed on Linux, skipping",
		browserExtensionID,
		"NativeMessagingHosts",
		"json_escape",
	} {
		if !strings.Contains(linux, needle) {
			t.Fatalf("linux installer missing %s", needle)
		}
	}

	manifest := readRepoFile(t, filepath.Join("extensions", "companion-paper", "manifest.json"))
	gotID := chromeExtensionIDFromManifest(t, manifest)
	if gotID != browserExtensionID {
		t.Fatalf("manifest key hashes to %s, constant is %s", gotID, browserExtensionID)
	}
	if !strings.Contains(nsi, gotID) || !strings.Contains(canonical, gotID) || !strings.Contains(linux, gotID) {
		t.Fatal("installer extension id does not match the manifest key")
	}
	menu := readRepoFile(t, filepath.Join("extensions", "companion-paper", "_locales", "zh_CN", "messages.json"))
	if !strings.Contains(menu, "添加到伴读") {
		t.Fatal("menu label is not 添加到伴读")
	}
	background := strings.ReplaceAll(readRepoFile(t, filepath.Join("extensions", "companion-paper", "background.js")), "\r\n", "\n")
	for _, needle := range []string{
		`contexts: ["page", "frame", "link", "selection"]`,
		"file://*/*",
		"isPaperTarget",
		"isArxivPaper",
		"isAclPaper",
		`path.startsWith("/abs/")`,
		`path.startsWith("/pdf/")`,
		`path.startsWith("/html/")`,
		`path.startsWith("/src/")`,
		"arxiv.org",
		"\ninstallMenu();\n",
		"menuEpoch",
		`path === "/forum"`,
		"info.frameUrl",
	} {
		if !strings.Contains(background, needle) {
			t.Fatalf("extension missing %s", needle)
		}
	}
}

func nsiFunction(t *testing.T, text, name string) string {
	t.Helper()
	marker := "Function " + name
	start := strings.Index(text, marker)
	if start < 0 {
		t.Fatalf("missing %s", marker)
	}
	rest := text[start:]
	end := strings.Index(rest, "\nFunctionEnd")
	if end < 0 {
		t.Fatalf("%s is not closed", marker)
	}
	return rest[:end]
}

func chromeExtensionIDFromManifest(t *testing.T, manifest string) string {
	t.Helper()
	var doc struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(manifest), &doc); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(doc.Key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	var id [32]byte
	for i := 0; i < 16; i++ {
		id[i*2] = 'a' + (sum[i] >> 4)
		id[i*2+1] = 'a' + (sum[i] & 0x0f)
	}
	return string(id[:])
}
