package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
