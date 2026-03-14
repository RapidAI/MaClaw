package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndroidPWAShell_UsesMobileDirDefaults(t *testing.T) {
	app := NewApp()
	tempDir := t.TempDir()

	result, err := app.GenerateAndroidPWAShell(AndroidPWAShellRequest{
		OutputDir: filepath.Join(tempDir, "mobile", "android"),
		AppName:   "MaClaw APP",
	})
	if err != nil {
		t.Fatalf("GenerateAndroidPWAShell() error = %v", err)
	}

	if result.HubCenterURL != defaultRemoteHubCenterURL {
		t.Fatalf("HubCenterURL = %q, want %q", result.HubCenterURL, defaultRemoteHubCenterURL)
	}
	if !strings.Contains(result.StartURL, defaultAndroidShellStartURL) {
		t.Fatalf("StartURL = %q, want bootstrap asset url", result.StartURL)
	}

	buildGradlePath := filepath.Join(result.ProjectDir, "app", "build.gradle")
	buildGradle, err := os.ReadFile(buildGradlePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", buildGradlePath, err)
	}
	if !strings.Contains(string(buildGradle), defaultRemoteHubCenterURL) {
		t.Fatalf("app/build.gradle does not contain default hub center url")
	}
	if !strings.Contains(string(buildGradle), `maclaw-${variant.buildType.name}.apk`) {
		t.Fatalf("app/build.gradle does not configure maclaw APK naming")
	}
	if !strings.Contains(string(buildGradle), "RELEASE_STORE_FILE") {
		t.Fatalf("app/build.gradle does not support release signing configuration")
	}

	buildCmdPath := filepath.Join(result.ProjectDir, "build_android.cmd")
	buildCmd, err := os.ReadFile(buildCmdPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", buildCmdPath, err)
	}
	if !strings.Contains(string(buildCmd), "assembleDebug") {
		t.Fatalf("build_android.cmd does not contain assembleDebug task")
	}
	if !strings.Contains(string(buildCmd), `..\dist`) {
		t.Fatalf("build_android.cmd does not copy APKs into mobile/dist")
	}
	if !strings.Contains(string(buildCmd), `OUTPUT_KIND=release`) {
		t.Fatalf("build_android.cmd does not handle release outputs")
	}

	releaseCmdPath := filepath.Join(result.ProjectDir, "build_android_release.cmd")
	releaseCmd, err := os.ReadFile(releaseCmdPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", releaseCmdPath, err)
	}
	if !strings.Contains(string(releaseCmd), "assembleRelease") {
		t.Fatalf("build_android_release.cmd does not call assembleRelease")
	}

	aabCmdPath := filepath.Join(result.ProjectDir, "build_android_aab.cmd")
	aabCmd, err := os.ReadFile(aabCmdPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", aabCmdPath, err)
	}
	if !strings.Contains(string(aabCmd), "bundleRelease") || !strings.Contains(string(aabCmd), "maclaw-release.aab") {
		t.Fatalf("build_android_aab.cmd does not build or rename release AAB output")
	}

	signedReleaseCmdPath := filepath.Join(result.ProjectDir, "build_android_signed_release.cmd")
	signedReleaseCmd, err := os.ReadFile(signedReleaseCmdPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", signedReleaseCmdPath, err)
	}
	if !strings.Contains(string(signedReleaseCmd), "signing.env.cmd") {
		t.Fatalf("build_android_signed_release.cmd does not load signing.env.cmd")
	}

	signedAABCmdPath := filepath.Join(result.ProjectDir, "build_android_signed_aab.cmd")
	signedAABCmd, err := os.ReadFile(signedAABCmdPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", signedAABCmdPath, err)
	}
	if !strings.Contains(string(signedAABCmd), "build_android_aab.cmd") {
		t.Fatalf("build_android_signed_aab.cmd does not invoke the AAB build script")
	}

	signingExamplePath := filepath.Join(result.ProjectDir, "signing.example.cmd")
	signingExample, err := os.ReadFile(signingExamplePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", signingExamplePath, err)
	}
	if !strings.Contains(string(signingExample), "RELEASE_STORE_FILE") {
		t.Fatalf("signing.example.cmd does not contain signing placeholders")
	}

	createKeystoreCmdPath := filepath.Join(result.ProjectDir, "create_android_keystore.cmd")
	createKeystoreCmd, err := os.ReadFile(createKeystoreCmdPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", createKeystoreCmdPath, err)
	}
	if !strings.Contains(string(createKeystoreCmd), "keytool") {
		t.Fatalf("create_android_keystore.cmd does not invoke keytool")
	}

	bootstrapPath := filepath.Join(result.ProjectDir, "app", "src", "main", "assets", "bootstrap.html")
	bootstrap, err := os.ReadFile(bootstrapPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", bootstrapPath, err)
	}
	if !strings.Contains(string(bootstrap), "/api/entry/resolve") {
		t.Fatalf("bootstrap.html does not contain resolve endpoint")
	}
	if !strings.Contains(string(bootstrap), "/api/entry/probe") {
		t.Fatalf("bootstrap.html does not contain private probe endpoint")
	}
}

func TestGenerateMobilePWAShell_CreatesAndroidAndIOSProjects(t *testing.T) {
	app := NewApp()
	tempDir := t.TempDir()

	result, err := app.GenerateMobilePWAShell(MobilePWAShellRequest{
		OutputDir: filepath.Join(tempDir, "mobile"),
		AppName:   "MaClaw APP",
	})
	if err != nil {
		t.Fatalf("GenerateMobilePWAShell() error = %v", err)
	}
	if result.Android == nil {
		t.Fatal("Android result is nil")
	}
	if result.IOS == nil {
		t.Fatal("iOS result is nil")
	}

	sharedBootstrapPath := filepath.Join(result.RootDir, defaultMobileSharedDir, "bootstrap.html")
	sharedBootstrap, err := os.ReadFile(sharedBootstrapPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", sharedBootstrapPath, err)
	}
	if !strings.Contains(string(sharedBootstrap), "/api/entry/resolve") || !strings.Contains(string(sharedBootstrap), "/api/entry/probe") {
		t.Fatalf("shared bootstrap is missing expected resolve/probe logic")
	}

	distReadmePath := filepath.Join(result.RootDir, "dist", "README.md")
	distReadme, err := os.ReadFile(distReadmePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", distReadmePath, err)
	}
	if !strings.Contains(string(distReadme), "maclaw-release.apk") || !strings.Contains(string(distReadme), "maclaw-ios-simulator.app") {
		t.Fatalf("dist README is missing expected mobile deliverable entries")
	}

	pbxprojPath := filepath.Join(result.IOS.ProjectDir, "RapidAIHubShell.xcodeproj", "project.pbxproj")
	pbxproj, err := os.ReadFile(pbxprojPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", pbxprojPath, err)
	}
	if !strings.Contains(string(pbxproj), "CODE_SIGNING_ALLOWED=NO") && !strings.Contains(string(pbxproj), "CODE_SIGN_STYLE = Automatic;") {
		t.Fatalf("project.pbxproj does not look like an iOS app project")
	}

	iosBootstrapPath := filepath.Join(result.IOS.ProjectDir, "RapidAIHubShell", "Resources", "bootstrap.html")
	iosBootstrap, err := os.ReadFile(iosBootstrapPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", iosBootstrapPath, err)
	}
	if !strings.Contains(string(iosBootstrap), "/api/entry/resolve") {
		t.Fatalf("iOS bootstrap.html does not contain resolve endpoint")
	}
	if !strings.Contains(string(iosBootstrap), "/api/entry/probe") {
		t.Fatalf("iOS bootstrap.html does not contain private probe endpoint")
	}

	iosBuildScriptPath := filepath.Join(result.IOS.ProjectDir, "build_ios_simulator.sh")
	iosBuildScript, err := os.ReadFile(iosBuildScriptPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", iosBuildScriptPath, err)
	}
	if !strings.Contains(string(iosBuildScript), "xcodebuild -project") {
		t.Fatalf("build_ios_simulator.sh does not contain xcodebuild command")
	}
	if !strings.Contains(string(iosBuildScript), "ios-deriveddata") {
		t.Fatalf("build_ios_simulator.sh does not write derived data under mobile/dist")
	}
	if !strings.Contains(string(iosBuildScript), "maclaw-ios-simulator.app") {
		t.Fatalf("build_ios_simulator.sh does not copy the simulator app into mobile/dist")
	}

	iosReleaseBuildScriptPath := filepath.Join(result.IOS.ProjectDir, "build_ios_simulator_release.sh")
	iosReleaseBuildScript, err := os.ReadFile(iosReleaseBuildScriptPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", iosReleaseBuildScriptPath, err)
	}
	if !strings.Contains(string(iosReleaseBuildScript), "CONFIGURATION=Release") || !strings.Contains(string(iosReleaseBuildScript), "maclaw-ios-simulator-release.app") {
		t.Fatalf("build_ios_simulator_release.sh does not configure release simulator output")
	}
}
