package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeGitHubReleaseSummariesAcceptsLargeCatalogue(t *testing.T) {
	asset := strings.Repeat("x", 512)
	var b bytes.Buffer
	b.WriteString("[{\"tag_name\":\"V1\",\"published_at\":\"2026-08-28T00:00:00Z\",\"assets\":[")
	for i := 0; i < 12000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "{\"name\":\"asset-%d\",\"browser_download_url\":\"https://example.invalid/%s\"}", i, asset)
	}
	b.WriteString("]}]")
	got, err := decodeGitHubReleaseSummaries(&b)
	if err != nil {
		t.Fatalf("decodeGitHubReleaseSummaries() error: %v", err)
	}
	if len(got) != 1 || len(got[0].Assets) != 12000 {
		t.Fatalf("decoded catalogue size = %d/%d", len(got), len(got[0].Assets))
	}
}

func TestDecodeGitHubReleaseSummariesRejectsOversizedResponse(t *testing.T) {
	data := bytes.Repeat([]byte("x"), githubReleaseListMaxBytes+1)
	if _, err := decodeGitHubReleaseSummaries(bytes.NewReader(data)); err == nil {
		t.Fatal("expected oversized response to be rejected")
	}
}

func TestCompareVersions_NumericOnly(t *testing.T) {
	cases := []struct {
		v1, v2 string
		want   int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.2.0", "1.1.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0", "1.0.0", 0},
		{"1.0.0.1", "1.0.0", 1},
	}
	for _, c := range cases {
		got := compareVersions(c.v1, c.v2)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.v1, c.v2, got, c.want)
		}
	}
}

func TestCompareVersions_PreRelease(t *testing.T) {
	cases := []struct {
		v1, v2 string
		want   int
	}{
		// beta < stable (same numeric)
		{"1.3.0-beta.1", "1.3.0", -1},
		{"1.3.0", "1.3.0-beta.1", 1},
		// beta < rc < stable
		{"1.3.0-beta.1", "1.3.0-rc.1", -1},
		{"1.3.0-rc.1", "1.3.0", -1},
		// alpha < beta
		{"1.3.0-alpha.1", "1.3.0-beta.1", -1},
		// same type, compare numbers
		{"1.3.0-beta.1", "1.3.0-beta.2", -1},
		{"1.3.0-beta.2", "1.3.0-beta.1", 1},
		{"1.3.0-beta.1", "1.3.0-beta.1", 0},
		// rc numbering
		{"1.3.0-rc.1", "1.3.0-rc.2", -1},
		// higher numeric always wins regardless of pre-release
		{"1.4.0-beta.1", "1.3.0", 1},
		{"1.3.0", "1.4.0-beta.1", -1},
		// beta with no number vs beta.1
		{"1.3.0-beta", "1.3.0-beta.1", -1},
		// "rc1" format (no dot separator)
		{"1.3.0-rc1", "1.3.0-rc2", -1},
	}
	for _, c := range cases {
		got := compareVersions(c.v1, c.v2)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.v1, c.v2, got, c.want)
		}
	}
}

func TestCompareVersions_WithVPrefix(t *testing.T) {
	cases := []struct {
		v1, v2 string
		want   int
	}{
		{"V1.3.0", "v1.3.0", 0},
		{"v1.3.0-beta.1", "V1.3.0", -1},
		{"V1.4.0", "v1.3.0-rc.1", 1},
	}
	for _, c := range cases {
		got := compareVersions(c.v1, c.v2)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.v1, c.v2, got, c.want)
		}
	}
}

func TestSplitVersionPreRelease(t *testing.T) {
	cases := []struct {
		input   string
		wantNum string
		wantPre string
	}{
		{"1.3.0", "1.3.0", ""},
		{"1.3.0-beta.1", "1.3.0", "beta.1"},
		{"1.3.0-rc1", "1.3.0", "rc1"},
		{"1.3.0-alpha", "1.3.0", "alpha"},
		{"1.3.0-beta.10", "1.3.0", "beta.10"},
		{"", "", ""},
	}
	for _, c := range cases {
		gotNum, gotPre := splitVersionPreRelease(c.input)
		if gotNum != c.wantNum || gotPre != c.wantPre {
			t.Errorf("splitVersionPreRelease(%q) = (%q, %q), want (%q, %q)",
				c.input, gotNum, gotPre, c.wantNum, c.wantPre)
		}
	}
}

func TestPreReleaseWeight(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 100},
		{"alpha.1", 10},
		{"beta.1", 20},
		{"rc.1", 30},
		{"Beta.2", 20},
		{"RC1", 30},
		{"dev.1", 15},
	}
	for _, c := range cases {
		got := preReleaseWeight(c.input)
		if got != c.want {
			t.Errorf("preReleaseWeight(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestPreReleaseNumber(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"beta", 0},
		{"beta.1", 1},
		{"beta.10", 10},
		{"rc1", 1},
		{"rc12", 12},
		{"alpha-3", 3},
	}
	for _, c := range cases {
		got := preReleaseNumber(c.input)
		if got != c.want {
			t.Errorf("preReleaseNumber(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestVerifySHA256File_EmptyExpected(t *testing.T) {
	// Empty expected = no verification = always passes
	if err := verifySHA256File("/nonexistent/path", ""); err != nil {
		t.Errorf("verifySHA256File with empty expected should return nil, got %v", err)
	}
}

func TestVerifySHA256File_InvalidLength(t *testing.T) {
	err := verifySHA256File("/some/path", "tooshort")
	if err == nil {
		t.Error("verifySHA256File with short digest should return error")
	}
}

func TestVerifySHA256File_CorrectHash(t *testing.T) {
	// Write a temp file and verify its known SHA256
	content := []byte("hello world\n")
	tmpFile := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	// sha256("hello world\n") = a948904f2f0f479b8f8564e9d7d4e6fc80d4f85e3d5fda5b7f6c5fc4b2cb4f2b ... nope
	// Compute expected: sha256sum of "hello world\n"
	h := sha256.New()
	h.Write(content)
	expected := fmt.Sprintf("%x", h.Sum(nil))

	if err := verifySHA256File(tmpFile, expected); err != nil {
		t.Errorf("verifySHA256File with correct hash should pass, got: %v", err)
	}
}

func TestVerifySHA256File_WrongHash(t *testing.T) {
	content := []byte("hello world\n")
	tmpFile := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	err := verifySHA256File(tmpFile, wrongHash)
	if err == nil {
		t.Error("verifySHA256File with wrong hash should return error")
	}
	if !strings.Contains(err.Error(), "integrity verification failed") {
		t.Errorf("error should mention integrity verification, got: %v", err)
	}
}

func TestVerifySHA256File_UppercaseExpected(t *testing.T) {
	content := []byte("test data")
	tmpFile := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	h.Write(content)
	expected := strings.ToUpper(fmt.Sprintf("%x", h.Sum(nil)))

	// Should still pass — function lowercases the expected hash internally
	if err := verifySHA256File(tmpFile, expected); err != nil {
		t.Errorf("verifySHA256File with uppercase expected should pass, got: %v", err)
	}
}

func TestPickPreferredUpdateResult(t *testing.T) {
	betaOnly := UpdateResult{HasUpdate: true, LatestVersion: "V1.4.0-beta.1", TagName: "1.4.0-beta.1", Channel: "beta"}
	stableOnly := UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0", TagName: "1.3.0", Channel: "stable"}
	olderBeta := UpdateResult{HasUpdate: false, LatestVersion: "V1.3.0-beta.2", TagName: "1.3.0-beta.2", Channel: "beta"}
	newerStable := UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0", TagName: "1.3.0", Channel: "stable"}
	sameBeta := UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0-beta.1", TagName: "1.3.0-beta.1", Channel: "beta"}
	sameStable := UpdateResult{HasUpdate: false, LatestVersion: "V1.2.0", TagName: "1.2.0", Channel: "stable"}
	// Same numeric tag on both channels should prefer beta (user opted in).
	equalBeta := UpdateResult{HasUpdate: true, LatestVersion: "V1.5.0", TagName: "1.5.0", Channel: "beta"}
	equalStable := UpdateResult{HasUpdate: true, LatestVersion: "V1.5.0", TagName: "1.5.0", Channel: "stable"}
	// TagName should win over display-only LatestVersion for comparison.
	tagWinsBeta := UpdateResult{HasUpdate: true, LatestVersion: "V1.0.0", TagName: "2.0.0-beta.1", Channel: "beta"}
	tagWinsStable := UpdateResult{HasUpdate: true, LatestVersion: "V9.0.0", TagName: "1.9.0", Channel: "stable"}

	t.Run("both fail", func(t *testing.T) {
		_, err := pickPreferredUpdateResult(UpdateResult{}, fmt.Errorf("beta down"), UpdateResult{}, fmt.Errorf("stable down"))
		if err == nil {
			t.Fatal("expected error when both channels fail")
		}
	})
	t.Run("beta fails falls back to stable", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(UpdateResult{}, fmt.Errorf("beta down"), stableOnly, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "stable" || !got.HasUpdate {
			t.Fatalf("got %+v, want stable update", got)
		}
	})
	t.Run("stable fails falls back to beta", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(betaOnly, nil, UpdateResult{}, fmt.Errorf("stable down"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" || !got.HasUpdate {
			t.Fatalf("got %+v, want beta update", got)
		}
	})
	t.Run("newer beta preferred over older stable", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(betaOnly, nil, stableOnly, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" || got.LatestVersion != "V1.4.0-beta.1" {
			t.Fatalf("got %+v, want beta V1.4.0-beta.1", got)
		}
	})
	t.Run("newer stable not masked by older beta", func(t *testing.T) {
		// User on a beta build; formal 1.3.0 is out but beta.json still points at 1.3.0-beta.2.
		got, err := pickPreferredUpdateResult(olderBeta, nil, newerStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "stable" || !got.HasUpdate || got.LatestVersion != "V1.3.0" {
			t.Fatalf("got %+v, want stable V1.3.0 with has_update", got)
		}
	})
	t.Run("newer beta preferred when stable has no update path", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(sameBeta, nil, sameStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" {
			t.Fatalf("got %+v, want beta", got)
		}
	})
	t.Run("equal versions prefer beta", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(equalBeta, nil, equalStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" {
			t.Fatalf("got %+v, want beta on version tie", got)
		}
	})
	t.Run("compare TagName over display LatestVersion", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(tagWinsBeta, nil, tagWinsStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Tag 2.0.0-beta.1 > 1.9.0 even though display LatestVersion on stable is V9.0.0.
		if got.Channel != "beta" || got.TagName != "2.0.0-beta.1" {
			t.Fatalf("got %+v, want beta by TagName", got)
		}
	})
}

func TestRollbackReleasesFromManifestUsesOnlyStableMirrorURLs(t *testing.T) {
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "11970", PublishedAt: "2026-08-28T10:00:00Z", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URLs: []string{
				"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/11970/MaClaw-Setup.exe",
				"https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/releases/11970/MaClaw-Setup.exe",
			}, SHA256: strings.Repeat("a", 64)},
		}},
		{Build: "11969", PublishedAt: "2026-08-27T10:00:00Z", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URL: "https://untrusted.example/installer.exe"},
		}},
	}}

	releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe")
	if len(releases) != 1 {
		t.Fatalf("got %d releases, want 1", len(releases))
	}
	if releases[0].Build != "11970" || releases[0].PublishedAt != "2026-08-28T10:00:00Z" || releases[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("unexpected rollback release: %+v", releases[0])
	}
	urls := splitDownloadURLs(releases[0].DownloadUrl)
	if len(urls) != 3 || urls[0] != "https://github.com/RapidAI/MaClaw/releases/download/11970/MaClaw-Setup.exe" || !strings.Contains(urls[1], "/releases/11970/MaClaw-Setup.exe") {
		t.Fatalf("rollback URLs = %#v, want GitHub then immutable R2/COS paths", urls)
	}
}

func TestImmutableReleaseAssetURLUsesPathEscapedOpaqueBuild(t *testing.T) {
	got := immutableReleaseAssetURL(r2PublicBaseURL, "V7+candidate:1@host/unsafe", "MaClaw Setup.exe")
	want := r2PublicBaseURL + "/releases/V7+candidate:1@host%2Funsafe/MaClaw%20Setup.exe"
	if got != want {
		t.Fatalf("immutableReleaseAssetURL() = %q, want %q", got, want)
	}
}

func TestGitHubReleaseAssetURLUsesPathEscapedOpaqueBuild(t *testing.T) {
	got := githubReleaseAssetURL("V7+candidate:1@host/unsafe", "MaClaw Setup.exe")
	want := "https://github.com/RapidAI/MaClaw/releases/download/V7+candidate:1@host%2Funsafe/MaClaw%20Setup.exe"
	if got != want {
		t.Fatalf("githubReleaseAssetURL() = %q, want %q", got, want)
	}
}

func TestBuildUpdateResultPathEscapesGitHubTag(t *testing.T) {
	app := &App{}
	result, err := app.buildUpdateResult("V7.0.0.1", latestReleaseInfo{TagName: "V7.1.0/unsafe"}, false)
	if err != nil {
		t.Fatalf("buildUpdateResult() error = %v", err)
	}
	if got := splitDownloadURLs(result.DownloadUrl)[1]; got != "https://github.com/RapidAI/MaClaw/releases/download/V7.1.0%2Funsafe/MaClaw-Setup.exe" {
		t.Fatalf("GitHub update URL = %q, want escaped tag segment", got)
	}
}
func TestIsReleaseMirrorURLRejectsLookalikeHost(t *testing.T) {
	lookalike := "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev.evil.example/latest/MaClaw-Setup.exe"
	if isReleaseMirrorURL(lookalike, "MaClaw-Setup.exe", false) {
		t.Fatalf("isReleaseMirrorURL accepted lookalike host %q", lookalike)
	}
}

func TestRollbackReleasesFromManifestRequiresSHA256(t *testing.T) {
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "11970", PublishedAt: "2026-08-28T10:00:00Z", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URLs: []string{"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/11970/MaClaw-Setup.exe"}, SHA256: ""},
		}},
	}}
	if releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe"); len(releases) != 0 {
		t.Fatalf("got %+v, want no release without a checksum", releases)
	}
}

func TestRollbackReleasesFromManifestRequiresRFC3339PublicationDate(t *testing.T) {
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "11970", PublishedAt: "not-a-date", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URLs: []string{"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/11970/MaClaw-Setup.exe"}, SHA256: strings.Repeat("a", 64)},
		}},
	}}
	if releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe"); len(releases) != 0 {
		t.Fatalf("got %+v, want no release with a malformed publication date", releases)
	}
}

func TestFetchStableHistoryFallsBackWhenFirstMirrorHasNoValidRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(stableHistoryManifest{Releases: []stableHistoryRelease{{
			Build:       "11970",
			PublishedAt: "2026-08-28T10:00:00Z",
			Assets: map[string]updateManifestAsset{
				"MaClaw-Setup.exe": {URLs: []string{"https://untrusted.example/installer.exe"}, SHA256: strings.Repeat("a", 64)},
			},
		}}})
	}))
	defer server.Close()

	if _, err := fetchStableHistory("test", server.URL, time.Second); err == nil || !strings.Contains(err.Error(), "no valid rollback releases") {
		t.Fatalf("fetchStableHistory() error = %v, want invalid-history failure", err)
	}
}

func TestRollbackReleasesFromManifestLimitsAndDeduplicates(t *testing.T) {
	assets := func(build string) map[string]updateManifestAsset {
		return map[string]updateManifestAsset{"MaClaw-Setup.exe": {URLs: []string{
			"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/" + build + "/MaClaw-Setup.exe",
		}, SHA256: strings.Repeat("b", 64)}}
	}
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "6", PublishedAt: "2026-08-06T00:00:00Z", Assets: assets("6")},
		{Build: "6", PublishedAt: "2026-08-05T00:00:00Z", Assets: assets("6")},
		{Build: "5", PublishedAt: "2026-08-05T00:00:00Z", Assets: assets("5")},
		{Build: "4", PublishedAt: "2026-08-04T00:00:00Z", Assets: assets("4")},
		{Build: "3", PublishedAt: "2026-08-03T00:00:00Z", Assets: assets("3")},
		{Build: "2", PublishedAt: "2026-08-02T00:00:00Z", Assets: assets("2")},
		{Build: "1", PublishedAt: "2026-08-01T00:00:00Z", Assets: assets("1")},
	}}
	releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe")
	if len(releases) != 5 {
		t.Fatalf("got %d releases, want 5", len(releases))
	}
	if releases[0].Build != "6" || releases[4].Build != "2" {
		t.Fatalf("unexpected retained builds: %+v", releases)
	}
}
func TestUpdateResultVersionKey(t *testing.T) {
	if got := updateResultVersionKey(UpdateResult{TagName: "1.2.3", LatestVersion: "V9.9.9"}); got != "1.2.3" {
		t.Fatalf("prefer TagName, got %q", got)
	}
	if got := updateResultVersionKey(UpdateResult{LatestVersion: "V1.0.0"}); got != "V1.0.0" {
		t.Fatalf("fallback LatestVersion, got %q", got)
	}
	if got := updateResultVersionKey(UpdateResult{TagName: "  v1.0.0  "}); got != "v1.0.0" {
		t.Fatalf("trim TagName, got %q", got)
	}
}

func TestUpdateTargetFileNameFor_BrandPackages(t *testing.T) {
	cases := []struct {
		name      string
		brandName string
		goos      string
		want      string
	}{
		{name: "windows default", brandName: "MaClaw", goos: "windows", want: "MaClaw-Setup.exe"},
		{name: "darwin default", brandName: "MaClaw", goos: "darwin", want: "MaClaw-Universal.pkg"},
		{name: "windows metastaff", brandName: "MetaStaff", goos: "windows", want: "MetaStaff-Setup.exe"},
		{name: "darwin metastaff", brandName: "MetaStaff", goos: "darwin", want: "MetaStaff-Universal.pkg"},
		{name: "fallback", brandName: "", goos: "linux", want: "MaClaw-Setup.exe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateTargetFileNameFor(tc.brandName, tc.goos); got != tc.want {
				t.Fatalf("updateTargetFileNameFor(%q, %q) = %q, want %q", tc.brandName, tc.goos, got, tc.want)
			}
		})
	}
}

func TestManifestAssetDownloadURLsPreservesPublishedMirrors(t *testing.T) {
	manifest := updateManifest{
		Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {
				URLs: []string{
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe",
					"https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest/MaClaw-Setup.exe",
				},
			},
		},
	}
	urls := manifestAssetDownloadURLs(manifest, "MaClaw-Setup.exe", "V7.1.0.11864", false)
	if len(urls) != 2 || urls[0] != "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe" || !strings.Contains(urls[1], "myqcloud.com") {
		t.Fatalf("manifestAssetDownloadURLs() = %#v, want the published R2 and COS URLs", urls)
	}
}

func TestManifestAssetDownloadURLsRejectsUntrustedOrWrongPathURLs(t *testing.T) {
	manifest := updateManifest{
		Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {
				URLs: []string{
					"https://evil.example/latest/MaClaw-Setup.exe",
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/other.exe",
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe?token=untrusted",
				},
			},
		},
	}
	urls := manifestAssetDownloadURLs(manifest, "MaClaw-Setup.exe", "", false)
	if len(urls) != 0 {
		t.Fatalf("manifestAssetDownloadURLs() = %#v, want no untrusted URLs", urls)
	}
}

func TestValidGitHubReleaseAssetURL(t *testing.T) {
	build, asset := "V7.1.0.11876", "MaClaw-Setup.exe"
	want := githubReleaseAssetURL(build, asset)
	if !validGitHubReleaseAssetURL(want, build, asset) {
		t.Fatalf("expected canonical GitHub URL to validate: %s", want)
	}
	for _, bad := range []string{
		"https://evil.example/RapidAI/MaClaw/releases/download/V7.1.0.11876/MaClaw-Setup.exe",
		want + "?download=1",
		"https://github.com/RapidAI/MaClaw/releases/tag/" + build,
	} {
		if validGitHubReleaseAssetURL(bad, build, asset) {
			t.Fatalf("unexpectedly accepted untrusted GitHub URL: %s", bad)
		}
	}
}

func TestManifestDownloadOrderUsesR2ThenGitHubThenCOS(t *testing.T) {
	manifest := updateManifest{
		Tag: "V7.1.0.11864",
		Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {
				URLs: []string{
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe",
					"https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest/MaClaw-Setup.exe",
				},
			},
		},
	}
	urls := manifestAssetDownloadURLs(manifest, "MaClaw-Setup.exe", manifest.Tag, false)
	downloads := combineDownloadURLList(append([]string{r2ReleaseAssetURL("MaClaw-Setup.exe", false), "https://github.com/RapidAI/MaClaw/releases/download/V7.1.0.11864/MaClaw-Setup.exe"}, urls...)...)
	got := splitDownloadURLs(downloads)
	if len(got) != 3 || got[0] != r2ReleaseAssetURL("MaClaw-Setup.exe", false) || !strings.Contains(got[1], "github.com/") || !strings.Contains(got[2], "myqcloud.com") {
		t.Fatalf("download candidate order = %#v, want R2, GitHub, COS", got)
	}
}

func TestBuildUpdateResultUsesR2AndGitHubWhenNoManifestURLsExist(t *testing.T) {
	app := &App{}
	result, err := app.buildUpdateResult("V7.0.0.1", latestReleaseInfo{TagName: "V7.1.0.11864"}, false)
	if err != nil {
		t.Fatalf("buildUpdateResult() error = %v", err)
	}
	urls := splitDownloadURLs(result.DownloadUrl)
	if len(urls) != 3 || urls[0] != r2ReleaseAssetURL("MaClaw-Setup.exe", false) || !strings.Contains(urls[1], "github.com/") || !strings.Contains(urls[2], "myqcloud.com") {
		t.Fatalf("fallback download candidate order = %#v, want R2, GitHub, COS", urls)
	}
}

func TestBuildUpdateResultDoesNotOfferOlderRelease(t *testing.T) {
	app := &App{}
	result, err := app.buildUpdateResult("7.5.0.11973", latestReleaseInfo{TagName: "V7.1.0.11876"}, false)
	if err != nil {
		t.Fatalf("buildUpdateResult() error = %v", err)
	}
	if result.HasUpdate {
		t.Fatalf("older release was offered as update: current=7.5.0.11973 latest=%s", result.LatestVersion)
	}
}

func TestDownloadUpdateRemovesPartialInstallerAndReportsAllFailedSources(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "6")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bad"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusForbidden)
	}))
	defer second.Close()

	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatalf("create Downloads directory: %v", err)
	}
	app := &App{testHomeDir: home, downloadCancelers: make(map[string]context.CancelFunc)}
	_, err := app.DownloadUpdate(first.URL+"\n"+second.URL, "MaClaw-Setup.exe")
	if err == nil || !strings.Contains(err.Error(), "all download sources failed") || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("DownloadUpdate() error = %v, want aggregated source failures", err)
	}
	downloads := filepath.Join(home, "Downloads")
	if _, err := os.Stat(filepath.Join(downloads, "MaClaw-Setup.exe")); !os.IsNotExist(err) {
		t.Fatalf("partial installer remains after failed fallbacks: %v", err)
	}
}

func TestDownloadUpdateReportsChecksumFailureBeforeTryingFallback(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "5242880")
		_, _ = w.Write(make([]byte, 5*1024*1024))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusForbidden)
	}))
	defer second.Close()

	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatalf("create Downloads directory: %v", err)
	}
	app := &App{testHomeDir: home, downloadCancelers: make(map[string]context.CancelFunc)}
	_, err := app.DownloadUpdateWithSHA256(first.URL+"\n"+second.URL, "MaClaw-Setup.exe", strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "integrity verification failed") || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("DownloadUpdateWithSHA256() error = %v, want checksum and fallback failures", err)
	}
	if _, err := os.Stat(filepath.Join(home, "Downloads", "MaClaw-Setup.exe")); !os.IsNotExist(err) {
		t.Fatalf("installer remains after checksum failure and failed fallback: %v", err)
	}
}
