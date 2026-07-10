package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBrandLabels(t *testing.T) {
	old := activeLanguage
	activeLanguage = langChinese
	defer func() { activeLanguage = old }()

	maclaw, err := resolveBrand("maclaw")
	if err != nil {
		t.Fatal(err)
	}
	if maclaw.ProductName != "MaClaw" || brandLabel(maclaw) != "MaClaw (\u539f\u5382\u54c1\u724c)" {
		t.Fatalf("maclaw brand = %+v label=%q", maclaw, brandLabel(maclaw))
	}
	tiger, err := resolveBrand("tigerclaw")
	if err != nil {
		t.Fatal(err)
	}
	if tiger.ProductName != "TigerClaw" || brandLabel(tiger) != "TigerClaw (\u5947\u5b89\u4fe1 OEM \u7248)" {
		t.Fatalf("tigerclaw brand = %+v label=%q", tiger, brandLabel(tiger))
	}
	meta, err := resolveBrand("\u667a\u5458")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ProductName != "MetaStaff" || brandLabel(meta) != "\u667a\u5458 MetaStaff (OEM \u7248)" {
		t.Fatalf("metastaff brand = %+v label=%q", meta, brandLabel(meta))
	}
}

func TestLanguageSwitchesBrandLabels(t *testing.T) {
	old := activeLanguage
	defer func() { activeLanguage = old }()
	activeLanguage = langEnglish
	if got := brandLabel(brandOptions[0]); got != "MaClaw (Original Brand)" {
		t.Fatalf("english maclaw label = %q", got)
	}
	if got := brandLabel(brandOptions[1]); got != "TigerClaw (QiAnXin OEM Edition)" {
		t.Fatalf("english tiger label = %q", got)
	}
	if got := brandLabel(brandOptions[2]); got != "MetaStaff (OEM Edition)" {
		t.Fatalf("english metastaff label = %q", got)
	}
	activeLanguage = langChinese
	if got := tr("language"); got != "\u8bed\u8a00\uff1a\u7b80\u4f53\u4e2d\u6587" {
		t.Fatalf("chinese language label = %q", got)
	}
}

func TestEnglishTranslationsAreASCII(t *testing.T) {
	for key, value := range translations[langEnglish] {
		for _, r := range value {
			if r > 127 {
				t.Fatalf("english translation %s contains non-ASCII rune %q in %q", key, r, value)
			}
		}
	}
}

func TestInstallerLogoMatchesGUIAsset(t *testing.T) {
	installerLogoPath := filepath.Join("assets", "appicon.png")
	guiLogoPath := filepath.Join("..", "gui", "build", "appicon.png")
	installerLogo, err := os.ReadFile(installerLogoPath)
	if err != nil {
		t.Fatalf("read installer logo: %v", err)
	}
	guiLogo, err := os.ReadFile(guiLogoPath)
	if err != nil {
		t.Fatalf("read GUI logo: %v", err)
	}
	if !bytes.Equal(installerLogo, guiLogo) {
		t.Fatalf("installer logo %s is not synced with %s", installerLogoPath, guiLogoPath)
	}
	img, err := png.Decode(bytes.NewReader(installerLogo))
	if err != nil {
		t.Fatalf("installer logo is not a valid PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() < 128 || bounds.Dy() < 128 {
		t.Fatalf("installer logo is too small: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestTUISelectPromptUsesDefaultIndex(t *testing.T) {
	old := activeLanguage
	defer func() { activeLanguage = old }()

	activeLanguage = langEnglish
	if got := fmt.Sprintf(tr("tui.select"), brandSelectionIndex(brandOptions[2])); got != "Select [3]: " {
		t.Fatalf("english metastaff default prompt = %q", got)
	}

	activeLanguage = langChinese
	if got := fmt.Sprintf(tr("tui.select"), brandSelectionIndex(brandOptions[2])); got != "\u8bf7\u9009\u62e9 [3]\uff1a" {
		t.Fatalf("chinese metastaff default prompt = %q", got)
	}
}

func TestBrandSelectionIndex(t *testing.T) {
	if got := brandSelectionIndex(brandOptions[0]); got != 1 {
		t.Fatalf("maclaw default index = %d", got)
	}
	if got := brandSelectionIndex(brandOptions[1]); got != 2 {
		t.Fatalf("tigerclaw default index = %d", got)
	}
	if got := brandSelectionIndex(brandOptions[2]); got != 3 {
		t.Fatalf("metastaff default index = %d", got)
	}
}

func TestSetLanguage(t *testing.T) {
	old := activeLanguage
	defer func() { activeLanguage = old }()
	if err := setLanguage("zh"); err != nil {
		t.Fatal(err)
	}
	if activeLanguage != langChinese {
		t.Fatalf("activeLanguage = %q, want zh", activeLanguage)
	}
	if err := setLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if activeLanguage != langEnglish {
		t.Fatalf("activeLanguage = %q, want en", activeLanguage)
	}
	if err := setLanguage("klingon"); err == nil {
		t.Fatal("setLanguage accepted invalid language")
	}
}

func TestTargetAssetNameForPlatforms(t *testing.T) {
	tests := []struct {
		name        string
		productName string
		goos        string
		goarch      string
		ubuntuLabel string
		want        string
		wantErr     bool
	}{
		{name: "windows", productName: "MaClaw", goos: "windows", goarch: "amd64", want: "MaClaw-Setup.exe"},
		{name: "darwin", productName: "TigerClaw", goos: "darwin", goarch: "arm64", want: "TigerClaw-Universal.pkg"},
		{name: "metastaff windows", productName: "MetaStaff", goos: "windows", goarch: "amd64", want: "MetaStaff-Setup.exe"},
		{name: "metastaff darwin", productName: "MetaStaff", goos: "darwin", goarch: "arm64", want: "MetaStaff-Universal.pkg"},
		{name: "linux amd64", productName: "MaClaw", goos: "linux", goarch: "amd64", ubuntuLabel: "u2404", want: "MaClaw-x86_64-u2404.AppImage"},
		{name: "linux arm64", productName: "TigerClaw", goos: "linux", goarch: "arm64", ubuntuLabel: "u2204", want: "TigerClaw-aarch64-u2204.AppImage"},
		{name: "metastaff linux amd64", productName: "MetaStaff", goos: "linux", goarch: "amd64", ubuntuLabel: "u2404", want: "MetaStaff-x86_64-u2404.AppImage"},
		{name: "metastaff linux arm64", productName: "MetaStaff", goos: "linux", goarch: "arm64", ubuntuLabel: "u2204", want: "MetaStaff-aarch64-u2204.AppImage"},
		{name: "linux unsupported arch", productName: "MaClaw", goos: "linux", goarch: "386", ubuntuLabel: "u2404", wantErr: true},
		{name: "unsupported os", productName: "MaClaw", goos: "freebsd", goarch: "amd64", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := targetAssetNameFor(tt.productName, tt.goos, tt.goarch, tt.ubuntuLabel)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("targetAssetNameFor succeeded with %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("targetAssetNameFor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDownloadStartMessageShowsSelectedNodeAndArchitecture(t *testing.T) {
	old := activeLanguage
	defer func() { activeLanguage = old }()

	activeLanguage = langEnglish
	got := downloadStartMessage("r2", "linux", "arm64", "MaClaw-aarch64-u2404.AppImage")
	for _, want := range []string{"Selected download node: r2", "Current system architecture: linux/arm64", "MaClaw-aarch64-u2404.AppImage"} {
		if !strings.Contains(got, want) {
			t.Fatalf("download start message %q does not contain %q", got, want)
		}
	}
}

func TestDownloadNodeName(t *testing.T) {
	if got := downloadNodeName("https://mirror.example:8443/releases/MaClaw-Setup.exe"); got != "mirror.example:8443" {
		t.Fatalf("download node = %q", got)
	}
	if got := downloadNodeName("not a URL"); got != "not a URL" {
		t.Fatalf("invalid download node = %q", got)
	}
}

func TestSplitDownloadURLsDedupes(t *testing.T) {
	got := splitDownloadURLs(" https://a.example/file\nhttps://b.example/file\nhttps://a.example/file\t")
	want := []string{"https://a.example/file", "https://b.example/file"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDownloadURLOrderUsesResponsiveManifestSourceFirst(t *testing.T) {
	manifest := updateManifest{
		Tag: "V1.2.3",
		Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {
				URL: "https://mirror.example/MaClaw-Setup.exe",
			},
		},
	}

	githubReleaseURL := "https://github.com/RapidAI/MaClaw/releases/download/V1.2.3/MaClaw-Setup.exe"
	mirrorURLs := manifestAssetDownloadURLs(manifest, "MaClaw-Setup.exe", "V1.2.3")
	githubFirst := combineDownloadURLList(append([]string{githubReleaseURL}, mirrorURLs...)...)
	if got := splitDownloadURLs(githubFirst)[0]; got != githubReleaseURL {
		t.Fatalf("github source first URL = %q, want %q", got, githubReleaseURL)
	}

	mirrorFirst := combineDownloadURLList(append(mirrorURLs, githubReleaseURL)...)
	if got := splitDownloadURLs(mirrorFirst)[0]; got != "https://mirror.example/MaClaw-Setup.exe" {
		t.Fatalf("mirror source first URL = %q", got)
	}
}

func TestNormalizeDownloadURLTrimsWhitespace(t *testing.T) {
	got, err := normalizeDownloadURL("  https://example.com/installer.exe  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/installer.exe" {
		t.Fatalf("normalized URL = %q", got)
	}
}

func TestValidateDownloadURL(t *testing.T) {
	valid := []string{
		"https://github.com/RapidAI/MaClaw/releases/download/V1/MaClaw-Setup.exe",
		" https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe ",
	}
	for _, candidate := range valid {
		if err := validateDownloadURL(candidate); err != nil {
			t.Fatalf("validateDownloadURL(%q) failed: %v", candidate, err)
		}
	}
	invalid := []string{"", "http://example.com/file.exe", "file:///C:/Temp/file.exe", "https:///missing-host"}
	for _, candidate := range invalid {
		if err := validateDownloadURL(candidate); err == nil {
			t.Fatalf("validateDownloadURL(%q) succeeded", candidate)
		}
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asset.bin")
	data := []byte("maclaw installer asset")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := verifySHA256(path, fmt.Sprintf("%x", sum[:])); err != nil {
		t.Fatalf("verifySHA256 valid digest: %v", err)
	}
	if err := verifySHA256(path, ""); err != nil {
		t.Fatalf("verifySHA256 empty digest: %v", err)
	}
	if err := verifySHA256(path, "0000"); err == nil || !strings.Contains(err.Error(), "invalid sha256 digest") {
		t.Fatalf("verifySHA256 invalid digest error = %v", err)
	}
	if err := verifySHA256(path, strings.Repeat("z", 64)); err == nil || !strings.Contains(err.Error(), "invalid sha256 digest") {
		t.Fatalf("verifySHA256 non-hex digest error = %v", err)
	}
	if err := verifySHA256(path, strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("verifySHA256 mismatch error = %v", err)
	}
}

func TestGUIResultMessage(t *testing.T) {
	old := activeLanguage
	activeLanguage = langEnglish
	defer func() { activeLanguage = old }()

	result := installResult{
		Release:        latestReleaseInfo{TagName: "V1.2.3", Source: "github"},
		TargetFileName: "MaClaw-Setup.exe",
		DownloadedPath: `C:\Temp\MaClaw-Setup.exe`,
	}
	if got := guiResultMessage(result, true, false); !strings.Contains(got, "Latest release found") || !strings.Contains(got, "MaClaw-Setup.exe") {
		t.Fatalf("check message = %q", got)
	}
	result.Skipped = true
	if got := guiResultMessage(result, false, false); !strings.Contains(got, "Already up to date") {
		t.Fatalf("skipped message = %q", got)
	}
}
