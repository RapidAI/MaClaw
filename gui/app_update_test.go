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
