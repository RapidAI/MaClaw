package main

import (
	_ "embed"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultMobileShellDir            = "mobile"
	defaultMobileSharedDir           = "shared"
	defaultAndroidShellAppName       = "MaClaw APP"
	defaultAndroidShellApplicationID = "tech.rapidai.hubshell"
	defaultAndroidShellStartURL      = "file:///android_asset/bootstrap.html"
	defaultIOSBundleID               = "tech.rapidai.hubshell"
	defaultIOSDeploymentTarget       = "15.0"
)

//go:embed build/mobile/bootstrap.html.tmpl
var sharedBootstrapTemplate string

type AndroidPWAShellRequest struct {
	OutputDir     string `json:"output_dir"`
	AppName       string `json:"app_name"`
	ApplicationID string `json:"application_id"`
	HubCenterURL  string `json:"hubcenter_url"`
	StartURL      string `json:"start_url"`
}

type AndroidPWAShellResult struct {
	ProjectDir       string `json:"project_dir"`
	ReadmePath       string `json:"readme_path"`
	ManifestPath     string `json:"manifest_path"`
	MainActivityPath string `json:"main_activity_path"`
	StartURL         string `json:"start_url"`
	HubCenterURL     string `json:"hubcenter_url"`
}

type IOSPWAShellRequest struct {
	OutputDir    string `json:"output_dir"`
	AppName      string `json:"app_name"`
	BundleID     string `json:"bundle_id"`
	HubCenterURL string `json:"hubcenter_url"`
	StartURL     string `json:"start_url"`
}

type IOSPWAShellResult struct {
	ProjectDir         string `json:"project_dir"`
	XcodeProjectPath   string `json:"xcode_project_path"`
	ReadmePath         string `json:"readme_path"`
	InfoPlistPath      string `json:"info_plist_path"`
	ViewControllerPath string `json:"view_controller_path"`
	StartURL           string `json:"start_url"`
	HubCenterURL       string `json:"hubcenter_url"`
}

type MobilePWAShellRequest struct {
	OutputDir       string `json:"output_dir"`
	AppName         string `json:"app_name"`
	ApplicationID   string `json:"application_id"`
	IOSBundleID     string `json:"ios_bundle_id"`
	HubCenterURL    string `json:"hubcenter_url"`
	StartURL        string `json:"start_url"`
	GenerateAndroid bool   `json:"generate_android"`
	GenerateIOS     bool   `json:"generate_ios"`
}

type MobilePWAShellResult struct {
	RootDir string                 `json:"root_dir"`
	Android *AndroidPWAShellResult `json:"android,omitempty"`
	IOS     *IOSPWAShellResult     `json:"ios,omitempty"`
}

func (a *App) GenerateMobilePWAShell(req MobilePWAShellRequest) (MobilePWAShellResult, error) {
	rootDir := strings.TrimSpace(req.OutputDir)
	if rootDir == "" {
		rootDir = defaultMobileShellDir
	}
	rootDir = filepath.Clean(rootDir)

	generateAndroid := req.GenerateAndroid || (!req.GenerateAndroid && !req.GenerateIOS)
	generateIOS := req.GenerateIOS || (!req.GenerateAndroid && !req.GenerateIOS)

	appName := strings.TrimSpace(req.AppName)
	if appName == "" {
		appName = defaultAndroidShellAppName
	}

	hubCenterURL := strings.TrimRight(strings.TrimSpace(req.HubCenterURL), "/")
	if hubCenterURL == "" {
		hubCenterURL = defaultRemoteHubCenterURL
	}

	result := MobilePWAShellResult{RootDir: rootDir}
	sharedBootstrap := renderBootstrapHTML(appName)

	if err := writeGeneratedTextFile(filepath.Join(rootDir, defaultMobileSharedDir, "bootstrap.html"), sharedBootstrap); err != nil {
		return MobilePWAShellResult{}, err
	}
	if err := writeGeneratedTextFile(filepath.Join(rootDir, "dist", "README.md"), buildMobileDistReadme()); err != nil {
		return MobilePWAShellResult{}, err
	}

	if generateAndroid {
		androidResult, err := a.GenerateAndroidPWAShell(AndroidPWAShellRequest{
			OutputDir:     filepath.Join(rootDir, "android"),
			AppName:       appName,
			ApplicationID: req.ApplicationID,
			HubCenterURL:  hubCenterURL,
			StartURL:      req.StartURL,
		})
		if err != nil {
			return MobilePWAShellResult{}, err
		}
		result.Android = &androidResult
	}

	if generateIOS {
		iosResult, err := a.GenerateIOSPWAShell(IOSPWAShellRequest{
			OutputDir:    filepath.Join(rootDir, "ios"),
			AppName:      appName,
			BundleID:     req.IOSBundleID,
			HubCenterURL: hubCenterURL,
			StartURL:     req.StartURL,
		})
		if err != nil {
			return MobilePWAShellResult{}, err
		}
		result.IOS = &iosResult
	}

	if err := writeGeneratedTextFile(filepath.Join(rootDir, "README.md"), buildMobileShellRootReadme(result)); err != nil {
		return MobilePWAShellResult{}, err
	}
	return result, nil
}

func (a *App) GenerateAndroidPWAShell(req AndroidPWAShellRequest) (AndroidPWAShellResult, error) {
	appName := strings.TrimSpace(req.AppName)
	if appName == "" {
		appName = defaultAndroidShellAppName
	}

	applicationID, err := normalizeAndroidApplicationID(req.ApplicationID)
	if err != nil {
		return AndroidPWAShellResult{}, err
	}

	hubCenterURL := strings.TrimRight(strings.TrimSpace(req.HubCenterURL), "/")
	if hubCenterURL == "" {
		hubCenterURL = defaultRemoteHubCenterURL
	}

	startURL := strings.TrimSpace(req.StartURL)
	if startURL == "" {
		startURL = defaultAndroidShellStartURL + "?hubcenter=" + url.QueryEscape(hubCenterURL)
	}

	projectDir := strings.TrimSpace(req.OutputDir)
	if projectDir == "" {
		projectDir = filepath.Join(defaultMobileShellDir, "android")
	}
	projectDir = filepath.Clean(projectDir)

	packagePath := filepath.Join(strings.Split(applicationID, ".")...)
	mainActivityPath := filepath.Join(projectDir, "app", "src", "main", "java", packagePath, "MainActivity.kt")
	manifestPath := filepath.Join(projectDir, "app", "src", "main", "AndroidManifest.xml")
	readmePath := filepath.Join(projectDir, "README.md")

	files := map[string]string{
		filepath.Join(projectDir, "settings.gradle"):                                                         buildAndroidSettingsGradle(appName),
		filepath.Join(projectDir, "build.gradle"):                                                            buildAndroidRootGradle(),
		filepath.Join(projectDir, "gradle.properties"):                                                       buildAndroidGradleProperties(),
		filepath.Join(projectDir, "build_android.cmd"):                                                       buildAndroidBuildCMD(),
		filepath.Join(projectDir, "build_android_release.cmd"):                                               buildAndroidReleaseCMD(),
		filepath.Join(projectDir, "build_android_signed_release.cmd"):                                        buildAndroidSignedReleaseCMD(),
		filepath.Join(projectDir, "build_android_aab.cmd"):                                                   buildAndroidAABCMD(),
		filepath.Join(projectDir, "build_android_signed_aab.cmd"):                                            buildAndroidSignedAABCMD(),
		filepath.Join(projectDir, "create_android_keystore.cmd"):                                             buildAndroidCreateKeystoreCMD(),
		filepath.Join(projectDir, "signing.example.cmd"):                                                     buildAndroidSigningExampleCMD(),
		filepath.Join(projectDir, "README.md"):                                                               buildAndroidShellReadme(appName, applicationID, startURL, hubCenterURL),
		filepath.Join(projectDir, "app", "build.gradle"):                                                     buildAndroidAppGradle(applicationID, startURL, hubCenterURL),
		filepath.Join(projectDir, "app", "proguard-rules.pro"):                                               "",
		filepath.Join(projectDir, "app", "src", "main", "AndroidManifest.xml"):                               buildAndroidManifest(applicationID),
		filepath.Join(projectDir, "app", "src", "main", "assets", "bootstrap.html"):                          renderBootstrapHTML(appName),
		filepath.Join(projectDir, "app", "src", "main", "java", packagePath, "MainActivity.kt"):              buildAndroidMainActivity(applicationID),
		filepath.Join(projectDir, "app", "src", "main", "res", "xml", "network_security_config.xml"):         buildAndroidNetworkSecurityConfig(),
		filepath.Join(projectDir, "app", "src", "main", "res", "values", "strings.xml"):                      buildAndroidStrings(appName),
		filepath.Join(projectDir, "app", "src", "main", "res", "values", "colors.xml"):                       buildAndroidColors(),
		filepath.Join(projectDir, "app", "src", "main", "res", "values", "themes.xml"):                       buildAndroidThemes(),
		filepath.Join(projectDir, "app", "src", "main", "res", "drawable", "ic_launcher_background.xml"):     buildAndroidLauncherBackground(),
		filepath.Join(projectDir, "app", "src", "main", "res", "drawable", "ic_launcher_foreground.xml"):     buildAndroidLauncherForeground(),
		filepath.Join(projectDir, "app", "src", "main", "res", "mipmap-anydpi-v26", "ic_launcher.xml"):       buildAndroidLauncherIcon(),
		filepath.Join(projectDir, "app", "src", "main", "res", "mipmap-anydpi-v26", "ic_launcher_round.xml"): buildAndroidLauncherIcon(),
	}

	for path, content := range files {
		if err := writeGeneratedTextFile(path, content); err != nil {
			return AndroidPWAShellResult{}, err
		}
	}

	return AndroidPWAShellResult{
		ProjectDir:       projectDir,
		ReadmePath:       readmePath,
		ManifestPath:     manifestPath,
		MainActivityPath: mainActivityPath,
		StartURL:         startURL,
		HubCenterURL:     hubCenterURL,
	}, nil
}

func runMobilePWAShellGenerator(app *App, args []string) int {
	fs := flag.NewFlagSet("generate-mobile-pwa-shell", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	outputDir := fs.String("output-dir", defaultMobileShellDir, "target mobile shell root directory")
	appName := fs.String("app-name", defaultAndroidShellAppName, "mobile application display name")
	applicationID := fs.String("application-id", defaultAndroidShellApplicationID, "android application id")
	iosBundleID := fs.String("ios-bundle-id", defaultIOSBundleID, "ios bundle id")
	hubCenterURL := fs.String("hubcenter-url", defaultRemoteHubCenterURL, "default hub center url")
	startURL := fs.String("start-url", "", "default web start url, leave empty to use the built-in bootstrap page")
	platform := fs.String("platform", "all", "target platform: all, android, or ios")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	generateAndroid := true
	generateIOS := true
	switch strings.ToLower(strings.TrimSpace(*platform)) {
	case "", "all":
	case "android":
		generateIOS = false
	case "ios":
		generateAndroid = false
	default:
		fmt.Fprintln(os.Stderr, "invalid platform:", *platform)
		return 2
	}

	result, err := app.GenerateMobilePWAShell(MobilePWAShellRequest{
		OutputDir:       *outputDir,
		AppName:         *appName,
		ApplicationID:   *applicationID,
		IOSBundleID:     *iosBundleID,
		HubCenterURL:    *hubCenterURL,
		StartURL:        *startURL,
		GenerateAndroid: generateAndroid,
		GenerateIOS:     generateIOS,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate mobile pwa shell failed:", err)
		return 1
	}

	fmt.Println("Mobile PWA shell generated:")
	fmt.Println("  Root:", result.RootDir)
	if result.Android != nil {
		fmt.Println("  Android:", result.Android.ProjectDir)
	}
	if result.IOS != nil {
		fmt.Println("  iOS:", result.IOS.XcodeProjectPath)
	}
	fmt.Println("  Hub Center:", *hubCenterURL)
	return 0
}

func runAndroidPWAShellGenerator(app *App, args []string) int {
	fs := flag.NewFlagSet("generate-android-pwa-shell", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	outputDir := fs.String("output-dir", filepath.Join(defaultMobileShellDir, "android"), "target android project directory")
	appName := fs.String("app-name", defaultAndroidShellAppName, "android application display name")
	applicationID := fs.String("application-id", defaultAndroidShellApplicationID, "android application id")
	hubCenterURL := fs.String("hubcenter-url", defaultRemoteHubCenterURL, "default hub center url")
	startURL := fs.String("start-url", "", "default web start url, leave empty to use the built-in bootstrap page")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	result, err := app.GenerateAndroidPWAShell(AndroidPWAShellRequest{
		OutputDir:     *outputDir,
		AppName:       *appName,
		ApplicationID: *applicationID,
		HubCenterURL:  *hubCenterURL,
		StartURL:      *startURL,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate android pwa shell failed:", err)
		return 1
	}

	fmt.Println("Android PWA shell project generated:")
	fmt.Println("  Project:", result.ProjectDir)
	fmt.Println("  Entry:", result.StartURL)
	fmt.Println("  Hub Center:", result.HubCenterURL)
	fmt.Println("  README:", result.ReadmePath)
	return 0
}

func normalizeAndroidApplicationID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = defaultAndroidShellApplicationID
	}

	parts := strings.Split(strings.ToLower(value), ".")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizePackageSegment(part)
		if part == "" {
			continue
		}
		if part[0] >= '0' && part[0] <= '9' {
			part = "app" + part
		}
		cleaned = append(cleaned, part)
	}

	if len(cleaned) < 2 {
		return "", fmt.Errorf("application_id must contain at least two dot-separated segments")
	}
	return strings.Join(cleaned, "."), nil
}

func sanitizePackageSegment(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sanitizeIOSProjectName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "RapidAIHubShell"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "RapidAIHubShell"
	}
	return b.String()
}

func writeGeneratedTextFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func buildAndroidSettingsGradle(appName string) string {
	return fmt.Sprintf(`pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = %s
include(":app")
`, strconv.Quote(appName))
}

func buildAndroidRootGradle() string {
	return `plugins {
    id("com.android.application") version "8.5.2" apply false
    id("org.jetbrains.kotlin.android") version "1.9.24" apply false
}
`
}

func buildAndroidGradleProperties() string {
	return `org.gradle.jvmargs=-Xmx2048m -Dfile.encoding=UTF-8
android.useAndroidX=true
kotlin.code.style=official
android.nonTransitiveRClass=true
`
}

func buildAndroidAppGradle(applicationID string, startURL string, hubCenterURL string) string {
	return fmt.Sprintf(`plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

def releaseStoreFile = providers.gradleProperty("RELEASE_STORE_FILE").orElse(providers.environmentVariable("RELEASE_STORE_FILE")).orNull
def releaseStorePassword = providers.gradleProperty("RELEASE_STORE_PASSWORD").orElse(providers.environmentVariable("RELEASE_STORE_PASSWORD")).orNull
def releaseKeyAlias = providers.gradleProperty("RELEASE_KEY_ALIAS").orElse(providers.environmentVariable("RELEASE_KEY_ALIAS")).orNull
def releaseKeyPassword = providers.gradleProperty("RELEASE_KEY_PASSWORD").orElse(providers.environmentVariable("RELEASE_KEY_PASSWORD")).orNull
def hasReleaseSigning = [releaseStoreFile, releaseStorePassword, releaseKeyAlias, releaseKeyPassword].every { it != null && !it.trim().isEmpty() }

android {
    namespace %s
    compileSdk 35

    defaultConfig {
        applicationId %s
        minSdk 24
        targetSdk 35
        versionCode 1
        versionName "1.0"

        buildConfigField "String", "DEFAULT_START_URL", %s
        buildConfigField "String", "DEFAULT_HUBCENTER_URL", %s
    }

    signingConfigs {
        if (hasReleaseSigning) {
            release {
                storeFile file(releaseStoreFile)
                storePassword releaseStorePassword
                keyAlias releaseKeyAlias
                keyPassword releaseKeyPassword
            }
        }
    }

    buildTypes {
        release {
            minifyEnabled false
            if (hasReleaseSigning) {
                signingConfig signingConfigs.release
            }
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    compileOptions {
        sourceCompatibility JavaVersion.VERSION_17
        targetCompatibility JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        buildConfig true
    }

    applicationVariants.configureEach { variant ->
        variant.outputs.configureEach {
            outputFileName = "maclaw-${variant.buildType.name}.apk"
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("androidx.activity:activity-ktx:1.9.2")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.webkit:webkit:1.11.0")
}
`, strconv.Quote(applicationID), strconv.Quote(applicationID), gradleBuildConfigStringLiteral(startURL), gradleBuildConfigStringLiteral(hubCenterURL))
}

func gradleBuildConfigStringLiteral(value string) string {
	return strconv.Quote(strconv.Quote(value))
}

func buildAndroidManifest(applicationID string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">

    <uses-permission android:name="android.permission.INTERNET" />
    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />

    <application
        android:allowBackup="true"
        android:icon="@mipmap/ic_launcher"
        android:label="@string/app_name"
        android:networkSecurityConfig="@xml/network_security_config"
        android:roundIcon="@mipmap/ic_launcher_round"
        android:supportsRtl="true"
        android:theme="@style/Theme.RapidAIHubShell"
        android:usesCleartextTraffic="true">
        <activity
            android:name="%s.MainActivity"
            android:configChanges="keyboard|keyboardHidden|navigation|orientation|screenLayout|screenSize|smallestScreenSize|uiMode"
            android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>

</manifest>
`, applicationID)
}

func buildAndroidMainActivity(applicationID string) string {
	return fmt.Sprintf(`package %s

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.webkit.CookieManager
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        webView = WebView(this)
        setContentView(webView)

        if (BuildConfig.DEBUG) {
            WebView.setWebContentsDebuggingEnabled(true)
        }

        val settings = webView.settings
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.databaseEnabled = true
        settings.allowFileAccess = true
        settings.allowContentAccess = true
        settings.loadsImagesAutomatically = true
        settings.cacheMode = WebSettings.LOAD_DEFAULT
        settings.mediaPlaybackRequiresUserGesture = false
        settings.mixedContentMode = WebSettings.MIXED_CONTENT_ALWAYS_ALLOW

        CookieManager.getInstance().setAcceptCookie(true)
        CookieManager.getInstance().setAcceptThirdPartyCookies(webView, true)

        webView.webChromeClient = WebChromeClient()
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
                val target = request?.url?.toString()?.trim().orEmpty()
                if (target.isEmpty()) {
                    return false
                }
                if (
                    target.startsWith("http://") ||
                    target.startsWith("https://") ||
                    target.startsWith("file:///android_asset/")
                ) {
                    return false
                }
                startActivity(Intent(Intent.ACTION_VIEW, request?.url))
                return true
            }
        }

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (webView.canGoBack()) {
                    webView.goBack()
                } else {
                    finish()
                }
            }
        })

        webView.loadUrl(resolveLaunchUrl(intent?.dataString))
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val deepLink = intent.dataString?.trim().orEmpty()
        if (deepLink.isNotEmpty()) {
            webView.loadUrl(deepLink)
        }
    }

    private fun resolveLaunchUrl(deepLink: String?): String {
        if (!deepLink.isNullOrBlank()) {
            return deepLink
        }
        val configuredStart = BuildConfig.DEFAULT_START_URL.trim()
        if (configuredStart.isNotEmpty()) {
            return configuredStart
        }
        val hubCenter = BuildConfig.DEFAULT_HUBCENTER_URL.trim()
        if (hubCenter.isEmpty()) {
            return "file:///android_asset/bootstrap.html"
        }
        return "file:///android_asset/bootstrap.html?hubcenter=" + Uri.encode(hubCenter)
    }
}
`, applicationID)
}

func renderBootstrapHTML(appName string) string {
	replacer := strings.NewReplacer(
		"__APP_NAME_HTML__", escapeXML(appName),
		"__APP_NAME_JS__", strconv.Quote(appName),
	)
	return replacer.Replace(sharedBootstrapTemplate)
}

func buildAndroidNetworkSecurityConfig() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
    <base-config cleartextTrafficPermitted="true" />
</network-security-config>
`
}

func buildAndroidStrings(appName string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<resources>
    <string name="app_name">%s</string>
</resources>
`, escapeXML(appName))
}

func buildAndroidColors() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<resources>
    <color name="rapidai_shell_primary">#0F6FFF</color>
    <color name="rapidai_shell_on_primary">#FFFFFF</color>
    <color name="rapidai_shell_background">#F4F7FB</color>
</resources>
`
}

func buildAndroidThemes() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<resources xmlns:tools="http://schemas.android.com/tools">
    <style name="Theme.RapidAIHubShell" parent="Theme.MaterialComponents.DayNight.NoActionBar">
        <item name="colorPrimary">@color/rapidai_shell_primary</item>
        <item name="colorOnPrimary">@color/rapidai_shell_on_primary</item>
        <item name="android:statusBarColor">@color/rapidai_shell_primary</item>
        <item name="android:navigationBarColor">@color/rapidai_shell_background</item>
        <item name="android:windowBackground">@color/rapidai_shell_background</item>
        <item name="android:windowLayoutInDisplayCutoutMode" tools:targetApi="p">shortEdges</item>
    </style>
</resources>
`
}

func buildAndroidLauncherBackground() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<shape xmlns:android="http://schemas.android.com/apk/res/android" android:shape="rectangle">
    <solid android:color="#0F6FFF" />
</shape>
`
}

func buildAndroidLauncherForeground() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp"
    android:height="108dp"
    android:viewportWidth="108"
    android:viewportHeight="108">
    <path android:fillColor="#FFFFFF" android:pathData="M24,30h60c5,0 9,4 9,9v30c0,5 -4,9 -9,9H24c-5,0 -9,-4 -9,-9V39c0,-5 4,-9 9,-9z" />
    <path android:fillColor="#0F6FFF" android:pathData="M32,38h44c3.3,0 6,2.7 6,6v20c0,3.3 -2.7,6 -6,6H32c-3.3,0 -6,-2.7 -6,-6V44c0,-3.3 2.7,-6 6,-6z" />
    <path android:fillColor="#FFFFFF" android:pathData="M39,47h30v4H39zM39,56h22v4H39z" />
</vector>
`
}

func buildAndroidLauncherIcon() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@drawable/ic_launcher_background" />
    <foreground android:drawable="@drawable/ic_launcher_foreground" />
</adaptive-icon>
`
}

func buildAndroidShellReadme(appName string, applicationID string, startURL string, hubCenterURL string) string {
	return fmt.Sprintf(
		"# MaClaw Android Shell\n\n"+
			"This Android shell project was generated by MaClaw's Go-based mobile shell generator.\n\n"+
			"## Defaults\n\n"+
			"- App name: MaClaw\n"+
			"- Application ID: %s\n"+
			"- Start URL: %s\n"+
			"- Hub Center: %s\n\n"+
			"## Output\n\n"+
			"All Android packaging artifacts are copied into `../dist/`.\n\n"+
			"Current filenames:\n\n"+
			"- Debug APK: `maclaw-debug.apk`\n"+
			"- Release APK: `maclaw-release.apk`\n"+
			"- Release AAB: `maclaw-release.aab`\n\n"+
			"## Scripts\n\n"+
			"- `build_android.cmd`\n"+
			"  Builds the debug APK.\n"+
			"- `build_android_release.cmd`\n"+
			"  Builds an unsigned release APK.\n"+
			"- `build_android_signed_release.cmd`\n"+
			"  Builds a signed release APK using `signing.env.cmd`.\n"+
			"- `build_android_aab.cmd`\n"+
			"  Builds an unsigned release AAB.\n"+
			"- `build_android_signed_aab.cmd`\n"+
			"  Builds a signed release AAB using `signing.env.cmd`.\n"+
			"- `create_android_keystore.cmd`\n"+
			"  Creates a new local keystore, defaulting to `maclaw-release.jks`.\n"+
			"- `signing.example.cmd`\n"+
			"  Template for signing configuration.\n"+
			"- `signing.env.cmd`\n"+
			"  Local signing configuration used by the signed release scripts.\n\n"+
			"## Common Flows\n\n"+
			"Debug APK:\n\n"+
			"```cmd\n"+
			"build_android.cmd\n"+
			"```\n\n"+
			"Unsigned release APK:\n\n"+
			"```cmd\n"+
			"build_android_release.cmd\n"+
			"```\n\n"+
			"Signed release APK:\n\n"+
			"```cmd\n"+
			"build_android_signed_release.cmd\n"+
			"```\n\n"+
			"Signed release AAB:\n\n"+
			"```cmd\n"+
			"build_android_signed_aab.cmd\n"+
			"```\n\n"+
			"## Signing Setup\n\n"+
			"If you do not already have a keystore:\n\n"+
			"```cmd\n"+
			"create_android_keystore.cmd\n"+
			"```\n\n"+
			"Then update `signing.env.cmd` with:\n\n"+
			"- `RELEASE_STORE_FILE`\n"+
			"- `RELEASE_STORE_PASSWORD`\n"+
			"- `RELEASE_KEY_ALIAS`\n"+
			"- `RELEASE_KEY_PASSWORD`\n\n"+
			"## Android Studio\n\n"+
			"You can also open this folder in Android Studio and let Gradle sync normally. The command-line scripts are only wrappers around the same Gradle tasks and keep outputs collected under `mobile/dist/`.\n",
		applicationID,
		startURL,
		hubCenterURL,
	)
}

func buildAndroidBuildCMD() string {
	return `@echo off
setlocal

cd /d "%~dp0"

	set "TASK=assembleDebug"
	if not "%~1"=="" set "TASK=%~1"
	set "OUTPUT_KIND=debug"
	echo %TASK% | findstr /I "Release" >nul
	if not errorlevel 1 set "OUTPUT_KIND=release"

	if exist gradlew.bat (
	    call gradlew.bat %TASK%
    goto :after_build
)

where gradle >nul 2>nul
if %ERRORLEVEL%==0 (
    call gradle %TASK%
    goto :after_build
)

echo [ERROR] Neither gradlew.bat nor gradle was found.
echo Install Gradle or open this project in Android Studio first.
exit /b 1

:after_build
if not %ERRORLEVEL%==0 (
    echo [ERROR] Android build failed.
	    exit /b %ERRORLEVEL%
	)

	set "APK_DIR=%CD%\app\build\outputs\apk\%OUTPUT_KIND%"
	set "DIST_DIR=%CD%\..\dist"
	if exist "%APK_DIR%" (
    if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
    for %%F in ("%APK_DIR%\*.apk") do (
        copy /y "%%~fF" "%DIST_DIR%\%%~nxF" >nul
    )
    echo.
    echo Build finished.
    echo APK output directory:
    echo   %APK_DIR%
    echo Copied APKs to:
    echo   %DIST_DIR%
    dir /b "%APK_DIR%\*.apk" 2>nul
) else (
    echo.
    echo Build finished, but APK directory was not found yet:
    echo   %APK_DIR%
)

	exit /b 0
`
}

func buildAndroidReleaseCMD() string {
	return `@echo off
setlocal

cd /d "%~dp0"
call build_android.cmd assembleRelease
exit /b %ERRORLEVEL%
`
}

func buildAndroidSignedReleaseCMD() string {
	return `@echo off
setlocal

cd /d "%~dp0"

if exist signing.env.cmd (
    call signing.env.cmd
)

if "%RELEASE_STORE_FILE%"=="" (
    echo [ERROR] RELEASE_STORE_FILE is not set.
    echo Create signing.env.cmd from signing.example.cmd and fill in your keystore values.
    exit /b 1
)
if "%RELEASE_STORE_PASSWORD%"=="" (
    echo [ERROR] RELEASE_STORE_PASSWORD is not set.
    exit /b 1
)
if "%RELEASE_KEY_ALIAS%"=="" (
    echo [ERROR] RELEASE_KEY_ALIAS is not set.
    exit /b 1
)
if "%RELEASE_KEY_PASSWORD%"=="" (
    echo [ERROR] RELEASE_KEY_PASSWORD is not set.
    exit /b 1
)

call build_android.cmd assembleRelease
exit /b %ERRORLEVEL%
`
}

func buildAndroidAABCMD() string {
	return `@echo off
setlocal

cd /d "%~dp0"

if exist gradlew.bat (
    call gradlew.bat bundleRelease
    goto :after_build
)

where gradle >nul 2>nul
if %ERRORLEVEL%==0 (
    call gradle bundleRelease
    goto :after_build
)

echo [ERROR] Neither gradlew.bat nor gradle was found.
echo Install Gradle or open this project in Android Studio first.
exit /b 1

:after_build
if not %ERRORLEVEL%==0 (
    echo [ERROR] Android AAB build failed.
    exit /b %ERRORLEVEL%
)

set "AAB_DIR=%CD%\app\build\outputs\bundle\release"
set "DIST_DIR=%CD%\..\dist"
if exist "%AAB_DIR%" (
    if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"
    if exist "%AAB_DIR%\*.aab" (
        for %%F in ("%AAB_DIR%\*.aab") do (
            copy /y "%%~fF" "%DIST_DIR%\maclaw-release.aab" >nul
        )
    )
    echo.
    echo Build finished.
    echo AAB output directory:
    echo   %AAB_DIR%
    echo Copied AAB to:
    echo   %DIST_DIR%\maclaw-release.aab
    dir /b "%AAB_DIR%\*.aab" 2>nul
) else (
    echo.
    echo Build finished, but AAB directory was not found yet:
    echo   %AAB_DIR%
)

exit /b 0
`
}

func buildAndroidSignedAABCMD() string {
	return `@echo off
setlocal

cd /d "%~dp0"

if exist signing.env.cmd (
    call signing.env.cmd
)

if "%RELEASE_STORE_FILE%"=="" (
    echo [ERROR] RELEASE_STORE_FILE is not set.
    echo Create signing.env.cmd from signing.example.cmd and fill in your keystore values.
    exit /b 1
)
if "%RELEASE_STORE_PASSWORD%"=="" (
    echo [ERROR] RELEASE_STORE_PASSWORD is not set.
    exit /b 1
)
if "%RELEASE_KEY_ALIAS%"=="" (
    echo [ERROR] RELEASE_KEY_ALIAS is not set.
    exit /b 1
)
if "%RELEASE_KEY_PASSWORD%"=="" (
    echo [ERROR] RELEASE_KEY_PASSWORD is not set.
    exit /b 1
)

call build_android_aab.cmd
exit /b %ERRORLEVEL%
`
}

func buildAndroidSigningExampleCMD() string {
	return `@echo off
set "RELEASE_STORE_FILE=C:\path\to\maclaw-release.jks"
set "RELEASE_STORE_PASSWORD=change-me"
set "RELEASE_KEY_ALIAS=maclaw"
set "RELEASE_KEY_PASSWORD=change-me"
`
}

func buildAndroidCreateKeystoreCMD() string {
	return `@echo off
setlocal

cd /d "%~dp0"

set "KEYSTORE_PATH=%CD%\maclaw-release.jks"
if not "%~1"=="" set "KEYSTORE_PATH=%~1"
set "KEY_ALIAS=maclaw"
if not "%~2"=="" set "KEY_ALIAS=%~2"

set "KEYTOOL="
if exist "%JAVA_HOME%\bin\keytool.exe" set "KEYTOOL=%JAVA_HOME%\bin\keytool.exe"
if "%KEYTOOL%"=="" if exist "C:\Program Files\Android\Android Studio\jbr\bin\keytool.exe" set "KEYTOOL=C:\Program Files\Android\Android Studio\jbr\bin\keytool.exe"
if "%KEYTOOL%"=="" for %%I in (keytool.exe) do set "KEYTOOL=%%~$PATH:I"

if "%KEYTOOL%"=="" (
    echo [ERROR] keytool.exe was not found.
    echo Set JAVA_HOME or install a JDK / Android Studio JBR first.
    exit /b 1
)

echo Creating keystore:
echo   %KEYSTORE_PATH%
echo Alias:
echo   %KEY_ALIAS%
echo.
echo keytool will prompt you for the keystore password and certificate details.

"%KEYTOOL%" -genkeypair -v -storetype PKCS12 -keystore "%KEYSTORE_PATH%" -alias "%KEY_ALIAS%" -keyalg RSA -keysize 2048 -validity 3650
if not %ERRORLEVEL%==0 exit /b %ERRORLEVEL%

echo.
echo Keystore created successfully.
echo Next step:
echo   1. Copy signing.example.cmd to signing.env.cmd
echo   2. Set RELEASE_STORE_FILE=%KEYSTORE_PATH%
echo   3. Set RELEASE_KEY_ALIAS=%KEY_ALIAS%
echo   4. Fill in the passwords you entered above
exit /b 0
`
}

func (a *App) GenerateIOSPWAShell(req IOSPWAShellRequest) (IOSPWAShellResult, error) {
	appName := strings.TrimSpace(req.AppName)
	if appName == "" {
		appName = defaultAndroidShellAppName
	}

	bundleID, err := normalizeAndroidApplicationID(req.BundleID)
	if err != nil {
		return IOSPWAShellResult{}, err
	}

	hubCenterURL := strings.TrimRight(strings.TrimSpace(req.HubCenterURL), "/")
	if hubCenterURL == "" {
		hubCenterURL = defaultRemoteHubCenterURL
	}

	startURL := strings.TrimSpace(req.StartURL)
	if startURL == "" {
		startURL = "bootstrap"
	}

	projectDir := strings.TrimSpace(req.OutputDir)
	if projectDir == "" {
		projectDir = filepath.Join(defaultMobileShellDir, "ios")
	}
	projectDir = filepath.Clean(projectDir)

	projectName := sanitizeIOSProjectName(appName)
	appDir := filepath.Join(projectDir, projectName)
	infoPlist := filepath.Join(appDir, "Info.plist")
	viewController := filepath.Join(appDir, "ViewController.swift")

	files := map[string]string{
		filepath.Join(projectDir, "README.md"):                                          buildIOSShellReadme(appName, bundleID, hubCenterURL, startURL, projectName),
		filepath.Join(projectDir, "build_ios_simulator.sh"):                             buildIOSSimulatorBuildSH(projectName),
		filepath.Join(projectDir, "build_ios_simulator_release.sh"):                     buildIOSSimulatorReleaseBuildSH(projectName),
		filepath.Join(projectDir, projectName+".xcodeproj", "project.pbxproj"):          buildIOSProjectPBXProj(projectName, bundleID),
		filepath.Join(appDir, "AppDelegate.swift"):                                      buildIOSAppDelegateSwift(),
		filepath.Join(appDir, "SceneDelegate.swift"):                                    buildIOSSceneDelegateSwift(projectName),
		filepath.Join(appDir, "ViewController.swift"):                                   buildIOSViewControllerSwift(),
		filepath.Join(appDir, "Configuration.swift"):                                    buildIOSConfigurationSwift(hubCenterURL, startURL),
		filepath.Join(appDir, "Info.plist"):                                             buildIOSInfoPlist(projectName),
		filepath.Join(appDir, "Assets.xcassets", "Contents.json"):                       buildIOSAssetsCatalogContents(),
		filepath.Join(appDir, "Assets.xcassets", "AppIcon.appiconset", "Contents.json"): buildIOSAppIconContents(),
		filepath.Join(appDir, "Base.lproj", "LaunchScreen.storyboard"):                  buildIOSLaunchScreenStoryboard(projectName),
		filepath.Join(appDir, "Resources", "bootstrap.html"):                            renderBootstrapHTML(appName),
	}

	for path, content := range files {
		if err := writeGeneratedTextFile(path, content); err != nil {
			return IOSPWAShellResult{}, err
		}
	}

	return IOSPWAShellResult{
		ProjectDir:         projectDir,
		XcodeProjectPath:   filepath.Join(projectDir, projectName+".xcodeproj"),
		ReadmePath:         filepath.Join(projectDir, "README.md"),
		InfoPlistPath:      infoPlist,
		ViewControllerPath: viewController,
		StartURL:           startURL,
		HubCenterURL:       hubCenterURL,
	}, nil
}

func buildIOSAppDelegateSwift() string {
	return `import UIKit

@main
class AppDelegate: UIResponder, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        configurationForConnecting connectingSceneSession: UISceneSession,
        options: UIScene.ConnectionOptions
    ) -> UISceneConfiguration {
        UISceneConfiguration(name: "Default Configuration", sessionRole: connectingSceneSession.role)
    }
}
`
}

func buildIOSSceneDelegateSwift(projectName string) string {
	return fmt.Sprintf(`import UIKit

class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    func scene(
        _ scene: UIScene,
        willConnectTo session: UISceneSession,
        options connectionOptions: UIScene.ConnectionOptions
    ) {
        guard let windowScene = scene as? UIWindowScene else { return }

        let window = UIWindow(windowScene: windowScene)
        let navigationController = UINavigationController(rootViewController: ViewController())
        navigationController.navigationBar.prefersLargeTitles = true
        navigationController.topViewController?.title = %s
        window.rootViewController = navigationController
        self.window = window
        window.makeKeyAndVisible()
    }
}
`, strconv.Quote(projectName))
}

func buildIOSConfigurationSwift(hubCenterURL string, startURL string) string {
	return fmt.Sprintf(`import Foundation

enum AppConfiguration {
    static let hubCenterURL = %s
    static let startURL = %s
}
`, strconv.Quote(hubCenterURL), strconv.Quote(startURL))
}

func buildIOSViewControllerSwift() string {
	return `import UIKit
import WebKit

final class ViewController: UIViewController, UITextFieldDelegate {
    private let textField = UITextField(frame: .zero)
    private let button = UIButton(type: .system)
    private let stackView = UIStackView(frame: .zero)
    private let webView = WKWebView(frame: .zero)

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = UIColor(red: 244.0 / 255.0, green: 247.0 / 255.0, blue: 251.0 / 255.0, alpha: 1)
        configureControls()
        configureWebView()
        loadInitialPage(prefillEmail: nil)
    }

    private func configureControls() {
        [textField, button, stackView, webView].forEach { $0.translatesAutoresizingMaskIntoConstraints = false }

        textField.borderStyle = .roundedRect
        textField.placeholder = "name@example.com"
        textField.keyboardType = .emailAddress
        textField.autocapitalizationType = .none
        textField.autocorrectionType = .no
        textField.returnKeyType = .go
        textField.delegate = self

        button.configuration = .filled()
        button.configuration?.title = "Open PWA"
        button.addTarget(self, action: #selector(openUsingEmail), for: .touchUpInside)

        stackView.axis = .horizontal
        stackView.spacing = 12
        stackView.addArrangedSubview(textField)
        stackView.addArrangedSubview(button)
        button.widthAnchor.constraint(equalToConstant: 120).isActive = true

        view.addSubview(stackView)
        view.addSubview(webView)

        NSLayoutConstraint.activate([
            stackView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 12),
            stackView.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            stackView.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            webView.topAnchor.constraint(equalTo: stackView.bottomAnchor, constant: 12),
            webView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            webView.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
    }

    private func configureWebView() {
        webView.allowsBackForwardNavigationGestures = true
        if #available(iOS 16.4, *) {
            webView.isInspectable = true
        }
    }

    private func loadInitialPage(prefillEmail: String?) {
        let start = AppConfiguration.startURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if !start.isEmpty && start.lowercased() != "bootstrap", let url = URL(string: start) {
            webView.load(URLRequest(url: url))
            return
        }

        guard let bootstrapURL = Bundle.main.url(forResource: "bootstrap", withExtension: "html", subdirectory: "Resources"),
              var components = URLComponents(url: bootstrapURL, resolvingAgainstBaseURL: false)
        else {
            return
        }

        var queryItems = [URLQueryItem(name: "hubcenter", value: AppConfiguration.hubCenterURL)]
        if let prefillEmail, !prefillEmail.isEmpty {
            queryItems.append(URLQueryItem(name: "prefill_email", value: prefillEmail))
        }
        components.queryItems = queryItems
        if let url = components.url {
            webView.loadFileURL(url, allowingReadAccessTo: bootstrapURL.deletingLastPathComponent())
        }
    }

    @objc private func openUsingEmail() {
        let email = textField.text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !email.isEmpty else {
            textField.becomeFirstResponder()
            return
        }

        let start = AppConfiguration.startURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if !start.isEmpty && start.lowercased() != "bootstrap", var components = URLComponents(string: start) {
            var queryItems = components.queryItems ?? []
            if !queryItems.contains(where: { $0.name == "email" }) {
                queryItems.append(URLQueryItem(name: "email", value: email))
            }
            if !queryItems.contains(where: { $0.name == "entry" }) {
                queryItems.append(URLQueryItem(name: "entry", value: "app"))
            }
            components.queryItems = queryItems
            if let url = components.url {
                webView.load(URLRequest(url: url))
                return
            }
        }

        loadInitialPage(prefillEmail: email)
    }

    func textFieldShouldReturn(_ textField: UITextField) -> Bool {
        openUsingEmail()
        return true
    }
}
`
}

func buildIOSInfoPlist(projectName string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>$(DEVELOPMENT_LANGUAGE)</string>
    <key>CFBundleDisplayName</key>
    <string>%s</string>
    <key>CFBundleExecutable</key>
    <string>$(EXECUTABLE_NAME)</string>
    <key>CFBundleIdentifier</key>
    <string>$(PRODUCT_BUNDLE_IDENTIFIER)</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>$(PRODUCT_NAME)</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0</string>
    <key>CFBundleVersion</key>
    <string>1</string>
    <key>LSRequiresIPhoneOS</key>
    <true/>
    <key>UIApplicationSceneManifest</key>
    <dict>
        <key>UIApplicationSupportsMultipleScenes</key>
        <false/>
        <key>UISceneConfigurations</key>
        <dict>
            <key>UIWindowSceneSessionRoleApplication</key>
            <array>
                <dict>
                    <key>UISceneConfigurationName</key>
                    <string>Default Configuration</string>
                    <key>UISceneDelegateClassName</key>
                    <string>$(PRODUCT_MODULE_NAME).SceneDelegate</string>
                </dict>
            </array>
        </dict>
    </dict>
    <key>UILaunchStoryboardName</key>
    <string>LaunchScreen</string>
    <key>UISupportedInterfaceOrientations</key>
    <array>
        <string>UIInterfaceOrientationPortrait</string>
    </array>
    <key>UISupportedInterfaceOrientations~ipad</key>
    <array>
        <string>UIInterfaceOrientationPortrait</string>
        <string>UIInterfaceOrientationLandscapeLeft</string>
        <string>UIInterfaceOrientationLandscapeRight</string>
    </array>
    <key>NSAppTransportSecurity</key>
    <dict>
        <key>NSAllowsArbitraryLoads</key>
        <true/>
    </dict>
</dict>
</plist>
`, escapeXML(projectName))
}

func buildIOSAssetsCatalogContents() string {
	return "{\n  \"info\" : {\n    \"author\" : \"xcode\",\n    \"version\" : 1\n  }\n}\n"
}

func buildIOSAppIconContents() string {
	return "{\n  \"images\" : [\n  ],\n  \"info\" : {\n    \"author\" : \"xcode\",\n    \"version\" : 1\n  }\n}\n"
}

func buildIOSLaunchScreenStoryboard(projectName string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<document type="com.apple.InterfaceBuilder3.CocoaTouch.Storyboard.XIB" version="3.0" toolsVersion="22154" targetRuntime="iOS.CocoaTouch" propertyAccessControl="none" useAutolayout="YES" launchScreen="YES" useTraitCollections="YES" useSafeAreas="YES" colorMatched="YES" initialViewController="LaunchScreen">
    <device id="retina6_12" orientation="portrait" appearance="light"/>
    <scenes>
        <scene sceneID="Scene">
            <objects>
                <viewController id="LaunchScreen" sceneMemberID="viewController">
                    <view key="view" contentMode="scaleToFill" id="RootView">
                        <rect key="frame" x="0.0" y="0.0" width="393" height="852"/>
                        <subviews>
                            <label opaque="NO" userInteractionEnabled="NO" contentMode="left" text=%s textAlignment="center" translatesAutoresizingMaskIntoConstraints="NO" id="TitleLabel">
                                <fontDescription key="fontDescription" type="boldSystem" pointSize="28"/>
                                <color key="textColor" red="0.05882352941" green="0.4352941176" blue="1" alpha="1" colorSpace="custom" customColorSpace="sRGB"/>
                            </label>
                        </subviews>
                        <color key="backgroundColor" red="0.9568627451" green="0.968627451" blue="0.9843137255" alpha="1" colorSpace="custom" customColorSpace="sRGB"/>
                        <constraints>
                            <constraint firstItem="TitleLabel" firstAttribute="centerX" secondItem="RootView" secondAttribute="centerX" id="centerX"/>
                            <constraint firstItem="TitleLabel" firstAttribute="centerY" secondItem="RootView" secondAttribute="centerY" id="centerY"/>
                        </constraints>
                    </view>
                </viewController>
                <placeholder placeholderIdentifier="IBFirstResponder" id="FirstResponder" sceneMemberID="firstResponder"/>
            </objects>
        </scene>
    </scenes>
</document>
`, strconv.Quote(projectName))
}

func buildIOSProjectPBXProj(projectName string, bundleID string) string {
	return fmt.Sprintf(`// !$*UTF8*$!
{
	archiveVersion = 1;
	classes = {};
	objectVersion = 56;
	objects = {
		A1001 = {isa = PBXBuildFile; fileRef = A2001; };
		A1002 = {isa = PBXBuildFile; fileRef = A2002; };
		A1003 = {isa = PBXBuildFile; fileRef = A2003; };
		A1004 = {isa = PBXBuildFile; fileRef = A2004; };
		A1005 = {isa = PBXBuildFile; fileRef = A2005; };
		A1006 = {isa = PBXBuildFile; fileRef = A2006; };
		A1007 = {isa = PBXBuildFile; fileRef = A2007; };
		A1008 = {isa = PBXBuildFile; fileRef = A2008; };
		A2001 = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = AppDelegate.swift; sourceTree = "<group>"; };
		A2002 = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = SceneDelegate.swift; sourceTree = "<group>"; };
		A2003 = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = ViewController.swift; sourceTree = "<group>"; };
		A2004 = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = Configuration.swift; sourceTree = "<group>"; };
		A2005 = {isa = PBXFileReference; lastKnownFileType = text.plist.xml; path = Info.plist; sourceTree = "<group>"; };
		A2006 = {isa = PBXFileReference; lastKnownFileType = folder.assetcatalog; path = Assets.xcassets; sourceTree = "<group>"; };
		A2007 = {isa = PBXFileReference; lastKnownFileType = file.storyboard; path = Base.lproj/LaunchScreen.storyboard; sourceTree = "<group>"; };
		A2008 = {isa = PBXFileReference; lastKnownFileType = text.html; path = Resources/bootstrap.html; sourceTree = "<group>"; };
		A2009 = {isa = PBXFileReference; lastKnownFileType = wrapper.framework; name = WebKit.framework; path = System/Library/Frameworks/WebKit.framework; sourceTree = SDKROOT; };
		A2010 = {isa = PBXFileReference; explicitFileType = wrapper.application; path = %s.app; sourceTree = BUILT_PRODUCTS_DIR; };
		A3001 = {isa = PBXFrameworksBuildPhase; buildActionMask = 2147483647; files = (A1008, ); runOnlyForDeploymentPostprocessing = 0; };
		A3002 = {isa = PBXResourcesBuildPhase; buildActionMask = 2147483647; files = (A1005, A1006, A1007, ); runOnlyForDeploymentPostprocessing = 0; };
		A3003 = {isa = PBXSourcesBuildPhase; buildActionMask = 2147483647; files = (A1001, A1002, A1003, A1004, ); runOnlyForDeploymentPostprocessing = 0; };
		A4001 = {isa = PBXGroup; children = (A4002, A4003, A4004, ); sourceTree = "<group>"; };
		A4002 = {isa = PBXGroup; path = %s; sourceTree = "<group>"; children = (A2001, A2002, A2003, A2004, A2005, A2006, A2007, A2008, ); };
		A4003 = {isa = PBXGroup; name = Frameworks; sourceTree = "<group>"; children = (A2009, ); };
		A4004 = {isa = PBXGroup; name = Products; sourceTree = "<group>"; children = (A2010, ); };
		A5001 = {isa = PBXNativeTarget; buildConfigurationList = A7001; buildPhases = (A3003, A3001, A3002, ); buildRules = (); dependencies = (); name = %s; productName = %s; productReference = A2010; productType = "com.apple.product-type.application"; };
		A6001 = {isa = PBXProject; attributes = {BuildIndependentTargetsInParallel = 1; LastSwiftUpdateCheck = 1600; LastUpgradeCheck = 1600;}; buildConfigurationList = A7002; compatibilityVersion = "Xcode 14.0"; developmentRegion = en; hasScannedForEncodings = 0; knownRegions = (en, Base, ); mainGroup = A4001; productRefGroup = A4004; projectDirPath = ""; projectRoot = ""; targets = (A5001, ); };
		A7001 = {isa = XCConfigurationList; buildConfigurations = (A7101, A7102, ); defaultConfigurationIsVisible = 0; defaultConfigurationName = Release; };
		A7002 = {isa = XCConfigurationList; buildConfigurations = (A7201, A7202, ); defaultConfigurationIsVisible = 0; defaultConfigurationName = Release; };
		A7101 = {isa = XCBuildConfiguration; name = Debug; buildSettings = {ASSETCATALOG_COMPILER_APPICON_NAME = AppIcon; CODE_SIGN_STYLE = Automatic; CURRENT_PROJECT_VERSION = 1; DEVELOPMENT_TEAM = ""; GENERATE_INFOPLIST_FILE = NO; INFOPLIST_FILE = %s/Info.plist; IPHONEOS_DEPLOYMENT_TARGET = %s; LD_RUNPATH_SEARCH_PATHS = ("$(inherited)", "@executable_path/Frameworks", ); MARKETING_VERSION = 1.0; PRODUCT_BUNDLE_IDENTIFIER = %s; PRODUCT_NAME = "$(TARGET_NAME)"; SDKROOT = iphoneos; SUPPORTED_PLATFORMS = "iphoneos iphonesimulator"; SWIFT_VERSION = 5.0; TARGETED_DEVICE_FAMILY = "1,2"; };
		};
		A7102 = {isa = XCBuildConfiguration; name = Release; buildSettings = {ASSETCATALOG_COMPILER_APPICON_NAME = AppIcon; CODE_SIGN_STYLE = Automatic; CURRENT_PROJECT_VERSION = 1; DEVELOPMENT_TEAM = ""; GENERATE_INFOPLIST_FILE = NO; INFOPLIST_FILE = %s/Info.plist; IPHONEOS_DEPLOYMENT_TARGET = %s; LD_RUNPATH_SEARCH_PATHS = ("$(inherited)", "@executable_path/Frameworks", ); MARKETING_VERSION = 1.0; PRODUCT_BUNDLE_IDENTIFIER = %s; PRODUCT_NAME = "$(TARGET_NAME)"; SDKROOT = iphoneos; SUPPORTED_PLATFORMS = "iphoneos iphonesimulator"; SWIFT_VERSION = 5.0; TARGETED_DEVICE_FAMILY = "1,2"; };
		};
		A7201 = {isa = XCBuildConfiguration; name = Debug; buildSettings = {CLANG_ENABLE_MODULES = YES; CLANG_ENABLE_OBJC_ARC = YES; IPHONEOS_DEPLOYMENT_TARGET = %s; SDKROOT = iphoneos; SWIFT_VERSION = 5.0; TARGETED_DEVICE_FAMILY = "1,2"; };
		};
		A7202 = {isa = XCBuildConfiguration; name = Release; buildSettings = {CLANG_ENABLE_MODULES = YES; CLANG_ENABLE_OBJC_ARC = YES; IPHONEOS_DEPLOYMENT_TARGET = %s; SDKROOT = iphoneos; SWIFT_VERSION = 5.0; TARGETED_DEVICE_FAMILY = "1,2"; };
		};
	};
	rootObject = A6001;
}
`, projectName, projectName, projectName, projectName, projectName, defaultIOSDeploymentTarget, bundleID, projectName, defaultIOSDeploymentTarget, bundleID, defaultIOSDeploymentTarget, defaultIOSDeploymentTarget)
}

func buildIOSShellReadme(appName string, bundleID string, hubCenterURL string, startURL string, projectName string) string {
	return fmt.Sprintf(
		"# MaClaw iOS Shell\n\n"+
			"This iOS shell project was generated by MaClaw's Go-based mobile shell generator.\n\n"+
			"## Defaults\n\n"+
			"- App name: MaClaw\n"+
			"- Bundle ID: %s\n"+
			"- Start URL: %s\n"+
			"- Hub Center: %s\n\n"+
			"## Output\n\n"+
			"All iOS simulator artifacts are written into `../dist/`.\n\n"+
			"Current filenames:\n\n"+
			"- Debug simulator app: `maclaw-ios-simulator.app`\n"+
			"- Release simulator app: `maclaw-ios-simulator-release.app`\n"+
			"- DerivedData root: `ios-deriveddata/`\n\n"+
			"## Scripts\n\n"+
			"- `build_ios_simulator.sh`\n"+
			"  Builds the debug simulator app and copies the `.app` bundle into `../dist/`.\n"+
			"- `build_ios_simulator_release.sh`\n"+
			"  Builds the release simulator app and copies the `.app` bundle into `../dist/`.\n\n"+
			"## Common Flows\n\n"+
			"Debug simulator app:\n\n"+
			"```bash\n"+
			"sh ./build_ios_simulator.sh\n"+
			"```\n\n"+
			"Release simulator app:\n\n"+
			"```bash\n"+
			"sh ./build_ios_simulator_release.sh\n"+
			"```\n\n"+
			"## Xcode\n\n"+
			"You can also open `%s.xcodeproj` in Xcode. The shell scripts simply wrap `xcodebuild` and keep simulator outputs collected under `mobile/dist/`.\n",
		bundleID,
		startURL,
		hubCenterURL,
		projectName,
	)
}

func buildMobileShellRootReadme(result MobilePWAShellResult) string {
	lines := []string{
		"# Mobile Shells",
		"",
		"This folder contains all generated mobile shell projects and mobile-only artifacts.",
		"",
		"## Layout",
		"",
	}
	if result.Android != nil {
		lines = append(lines, "- `android/`", "  Android shell project, packaging scripts, keystore helpers, and signing config templates.")
	}
	if result.IOS != nil {
		lines = append(lines, "- `ios/`", "  iOS shell project and simulator build script.")
	}
	lines = append(lines,
		"- `shared/bootstrap.html`",
		"  Shared mobile entry page used by both Android and iOS shells.",
		"- `dist/`",
		"  Consolidated mobile packaging outputs.",
		"  See `dist/README.md` for the handoff-oriented deliverable list.",
		"",
		"## Defaults",
		"",
		"Both Android and iOS default to Hub Center `http://hubs.mypapers.top:9388`.",
		"",
		"## Entry Points",
		"",
		"- Android debug APK: `android/build_android.cmd`",
		"- Android unsigned release APK: `android/build_android_release.cmd`",
		"- Android signed release APK: `android/build_android_signed_release.cmd`",
		"- Android unsigned release AAB: `android/build_android_aab.cmd`",
		"- Android signed release AAB: `android/build_android_signed_aab.cmd`",
		"- iOS simulator build: `ios/build_ios_simulator.sh`",
		"- iOS simulator release build: `ios/build_ios_simulator_release.sh`",
	)
	return strings.Join(lines, "\n") + "\n"
}

func buildMobileDistReadme() string {
	return "# Mobile Deliverables\n\n" +
		"This directory is the consolidated handoff location for mobile build outputs.\n\n" +
		"## Android\n\n" +
		"- `maclaw-release.apk`\n" +
		"  Signed Android release APK, suitable for direct installation and distribution.\n" +
		"- `maclaw-release.aab`\n" +
		"  Signed Android App Bundle, suitable for Play Console style distribution.\n" +
		"- `maclaw-debug.apk`\n" +
		"  Debug Android APK, generated when the debug packaging script is run.\n\n" +
		"Source scripts:\n\n" +
		"- `../android/build_android.cmd`\n" +
		"- `../android/build_android_release.cmd`\n" +
		"- `../android/build_android_signed_release.cmd`\n" +
		"- `../android/build_android_aab.cmd`\n" +
		"- `../android/build_android_signed_aab.cmd`\n\n" +
		"Signing inputs used by this project live under:\n\n" +
		"- `../android/maclaw-release.jks`\n" +
		"- `../android/signing.env.cmd`\n\n" +
		"## iOS\n\n" +
		"- `maclaw-ios-simulator.app`\n" +
		"  Debug iOS simulator app bundle, generated on macOS.\n" +
		"- `maclaw-ios-simulator-release.app`\n" +
		"  Release iOS simulator app bundle, generated on macOS.\n" +
		"- `ios-deriveddata/`\n" +
		"  Shared DerivedData root used by the iOS simulator scripts.\n\n" +
		"Source scripts:\n\n" +
		"- `../ios/build_ios_simulator.sh`\n" +
		"- `../ios/build_ios_simulator_release.sh`\n\n" +
		"## Notes\n\n" +
		"- All files in this directory are mobile-only artifacts.\n" +
		"- Android artifacts have been generated on this machine.\n" +
		"- iOS artifacts require running the iOS scripts on macOS with Xcode installed.\n"
}

func buildIOSSimulatorBuildSH(projectName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu

SCHEME=%s
CONFIGURATION=Debug
SDK=iphonesimulator
DIST_DIR="$(CDPATH= cd -- "$(dirname "$0")/../dist" && pwd)"
DERIVED_DATA_DIR="$DIST_DIR/ios-deriveddata"
APP_BUNDLE="$DERIVED_DATA_DIR/Build/Products/${CONFIGURATION}-iphonesimulator/%s.app"
COPIED_APP="$DIST_DIR/maclaw-ios-simulator.app"

if ! command -v xcodebuild >/dev/null 2>&1; then
  echo "[ERROR] xcodebuild was not found."
  echo "Run this on macOS with Xcode command line tools installed."
  exit 1
fi

mkdir -p "$DERIVED_DATA_DIR"
xcodebuild -project "%s.xcodeproj" -scheme "$SCHEME" -sdk "$SDK" -configuration "$CONFIGURATION" -derivedDataPath "$DERIVED_DATA_DIR" CODE_SIGNING_ALLOWED=NO build

if [ -d "$APP_BUNDLE" ]; then
  rm -rf "$COPIED_APP"
  cp -R "$APP_BUNDLE" "$COPIED_APP"
fi

echo
echo "Build finished."
echo "DerivedData path:"
echo "  $DERIVED_DATA_DIR"
if [ -d "$COPIED_APP" ]; then
  echo "Copied simulator app:"
  echo "  $COPIED_APP"
fi
`, projectName, projectName, projectName)
}

func buildIOSSimulatorReleaseBuildSH(projectName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu

SCHEME=%s
CONFIGURATION=Release
SDK=iphonesimulator
DIST_DIR="$(CDPATH= cd -- "$(dirname "$0")/../dist" && pwd)"
DERIVED_DATA_DIR="$DIST_DIR/ios-deriveddata"
APP_BUNDLE="$DERIVED_DATA_DIR/Build/Products/${CONFIGURATION}-iphonesimulator/%s.app"
COPIED_APP="$DIST_DIR/maclaw-ios-simulator-release.app"

if ! command -v xcodebuild >/dev/null 2>&1; then
  echo "[ERROR] xcodebuild was not found."
  echo "Run this on macOS with Xcode command line tools installed."
  exit 1
fi

mkdir -p "$DERIVED_DATA_DIR"
xcodebuild -project "%s.xcodeproj" -scheme "$SCHEME" -sdk "$SDK" -configuration "$CONFIGURATION" -derivedDataPath "$DERIVED_DATA_DIR" CODE_SIGNING_ALLOWED=NO build

if [ -d "$APP_BUNDLE" ]; then
  rm -rf "$COPIED_APP"
  cp -R "$APP_BUNDLE" "$COPIED_APP"
fi

echo
echo "Build finished."
echo "DerivedData path:"
echo "  $DERIVED_DATA_DIR"
if [ -d "$COPIED_APP" ]; then
  echo "Copied simulator app:"
  echo "  $COPIED_APP"
fi
`, projectName, projectName, projectName)
}

func escapeXML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}
