//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"
)

// ============================================================================
// Feature: ve-file-sharing-directories, Property 3: Windows path format and case-insensitivity
//
// For any valid file path within an allowed directory on Windows, changing the
// casing of any path component (drive letter, directory names, file name) SHALL
// NOT affect the validation result. Additionally, for any valid Windows path
// format (drive letters like C:\, mixed forward/backward slashes), the path
// validator SHALL correctly resolve and validate the path.
//
// **Validates: Requirements 5.3, 5.4**
// ============================================================================

// randomizeCasing randomly changes the casing of each character in a string.
func randomizeCasing(s string, t *rapid.T) string {
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsLetter(r) {
			if rapid.Bool().Draw(t, "upper") {
				runes[i] = unicode.ToUpper(r)
			} else {
				runes[i] = unicode.ToLower(r)
			}
		}
	}
	return string(runes)
}

// randomizeSlashes randomly replaces path separators with forward or backward slashes.
func randomizeSlashes(path string, t *rapid.T) string {
	var result strings.Builder
	for _, ch := range path {
		if ch == '\\' || ch == '/' {
			if rapid.Bool().Draw(t, "slash_dir") {
				result.WriteRune('/')
			} else {
				result.WriteRune('\\')
			}
		} else {
			result.WriteRune(ch)
		}
	}
	return result.String()
}

// genDirName generates a valid directory name component (ASCII letters + digits).
func genDirName() *rapid.Generator[string] {
	return rapid.Custom[string](func(t *rapid.T) string {
		// Generate a name with 1-8 ASCII letters/digits
		length := rapid.IntRange(1, 8).Draw(t, "dir_name_len")
		chars := make([]byte, length)
		for i := range chars {
			// Use alphanumeric characters only (safe for Windows filenames)
			idx := rapid.IntRange(0, 35).Draw(t, "char_idx")
			if idx < 26 {
				chars[i] = byte('a' + idx)
			} else {
				chars[i] = byte('0' + (idx - 26))
			}
		}
		return string(chars)
	})
}

// genFileName generates a valid file name with extension.
func genFileName() *rapid.Generator[string] {
	return rapid.Custom[string](func(t *rapid.T) string {
		name := genDirName().Draw(t, "file_base")
		ext := rapid.SampledFrom([]string{".txt", ".pdf", ".docx", ".md", ".csv"}).Draw(t, "file_ext")
		return name + ext
	})
}

// genDriveLetter generates a Windows drive letter (C-Z).
func genDriveLetter() *rapid.Generator[byte] {
	return rapid.Custom[byte](func(t *rapid.T) byte {
		// Use the actual temp dir's drive letter to ensure it exists
		return byte(rapid.IntRange(int('C'), int('Z')).Draw(t, "drive"))
	})
}

func TestProperty3_WindowsPathFormatAndCaseInsensitivity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a temporary directory structure for testing
		baseDir := os.TempDir()

		// Create a unique test directory under temp
		testDirName := genDirName().Draw(t, "test_dir")
		subDirName := genDirName().Draw(t, "sub_dir")
		fileName := genFileName().Draw(t, "file_name")

		testDir := filepath.Join(baseDir, "ve_prop3_"+testDirName)
		subDir := filepath.Join(testDir, subDirName)
		testFile := filepath.Join(subDir, fileName)

		// Create the directory structure and file
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Skip("cannot create test directory")
			return
		}
		defer os.RemoveAll(testDir)

		if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
			t.Skip("cannot create test file")
			return
		}

		// Get the canonical form of the allowed directory
		canonicalTestDir, err := filepath.Abs(testDir)
		if err != nil {
			t.Skip("cannot get absolute path")
			return
		}

		// --- Test 1: Validation with canonical path should succeed ---
		allowedDirs := []string{canonicalTestDir}
		canonicalResult, err := ValidateVEFilePath(testFile, allowedDirs)
		if err != nil {
			t.Fatalf("validation with canonical path should succeed: %v", err)
		}
		if canonicalResult == "" {
			t.Fatal("canonical result should not be empty")
		}

		// --- Test 2: Random casing of the file path should produce same result ---
		casedPath := randomizeCasing(testFile, t)
		casedResult, casedErr := ValidateVEFilePath(casedPath, allowedDirs)
		if casedErr != nil {
			t.Fatalf("validation with random casing should succeed (path=%q): %v", casedPath, casedErr)
		}
		// Both should resolve to the same canonical path (case-insensitive comparison)
		if !strings.EqualFold(canonicalResult, casedResult) {
			t.Errorf("canonical paths should match (case-insensitive):\n  original: %s\n  cased:    %s", canonicalResult, casedResult)
		}

		// --- Test 3: Random casing of the allowed directory should still validate ---
		casedAllowedDir := randomizeCasing(canonicalTestDir, t)
		casedDirResult, casedDirErr := ValidateVEFilePath(testFile, []string{casedAllowedDir})
		if casedDirErr != nil {
			t.Fatalf("validation with random-cased allowed dir should succeed (dir=%q): %v", casedAllowedDir, casedDirErr)
		}
		if !strings.EqualFold(canonicalResult, casedDirResult) {
			t.Errorf("canonical paths should match with cased allowed dir:\n  original: %s\n  cased:    %s", canonicalResult, casedDirResult)
		}

		// --- Test 4: Mixed forward/backward slashes should produce same result ---
		slashedPath := randomizeSlashes(testFile, t)
		slashedResult, slashedErr := ValidateVEFilePath(slashedPath, allowedDirs)
		if slashedErr != nil {
			t.Fatalf("validation with mixed slashes should succeed (path=%q): %v", slashedPath, slashedErr)
		}
		if !strings.EqualFold(canonicalResult, slashedResult) {
			t.Errorf("canonical paths should match with mixed slashes:\n  original: %s\n  slashed:  %s", canonicalResult, slashedResult)
		}

		// --- Test 5: Combined random casing + mixed slashes ---
		combinedPath := randomizeSlashes(randomizeCasing(testFile, t), t)
		combinedResult, combinedErr := ValidateVEFilePath(combinedPath, allowedDirs)
		if combinedErr != nil {
			t.Fatalf("validation with combined casing+slashes should succeed (path=%q): %v", combinedPath, combinedErr)
		}
		if !strings.EqualFold(canonicalResult, combinedResult) {
			t.Errorf("canonical paths should match with combined transforms:\n  original: %s\n  combined: %s", canonicalResult, combinedResult)
		}

		// --- Test 6: Random casing of both path AND allowed dir + mixed slashes ---
		fullRandomPath := randomizeSlashes(randomizeCasing(testFile, t), t)
		fullRandomDir := randomizeSlashes(randomizeCasing(canonicalTestDir, t), t)
		fullRandomResult, fullRandomErr := ValidateVEFilePath(fullRandomPath, []string{fullRandomDir})
		if fullRandomErr != nil {
			t.Fatalf("validation with fully randomized path+dir should succeed (path=%q, dir=%q): %v",
				fullRandomPath, fullRandomDir, fullRandomErr)
		}
		if !strings.EqualFold(canonicalResult, fullRandomResult) {
			t.Errorf("canonical paths should match with fully randomized transforms:\n  original:    %s\n  randomized: %s", canonicalResult, fullRandomResult)
		}
	})
}

func TestProperty3_WindowsDriveLetterCaseInsensitivity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a test file in temp directory
		baseDir := os.TempDir()
		testDirName := genDirName().Draw(t, "test_dir")
		fileName := genFileName().Draw(t, "file_name")

		testDir := filepath.Join(baseDir, "ve_prop3_drv_"+testDirName)
		testFile := filepath.Join(testDir, fileName)

		if err := os.MkdirAll(testDir, 0755); err != nil {
			t.Skip("cannot create test directory")
			return
		}
		defer os.RemoveAll(testDir)

		if err := os.WriteFile(testFile, []byte("drive test"), 0644); err != nil {
			t.Skip("cannot create test file")
			return
		}

		canonicalTestDir, err := filepath.Abs(testDir)
		if err != nil {
			t.Skip("cannot get absolute path")
			return
		}

		// Ensure path has a drive letter
		if len(canonicalTestDir) < 2 || canonicalTestDir[1] != ':' {
			t.Skip("path does not have a drive letter")
			return
		}

		allowedDirs := []string{canonicalTestDir}

		// Validate with canonical path first
		canonicalResult, err := ValidateVEFilePath(testFile, allowedDirs)
		if err != nil {
			t.Fatalf("canonical validation should succeed: %v", err)
		}

		// --- Test: Toggle drive letter case ---
		absFile, _ := filepath.Abs(testFile)
		driveLetter := absFile[0]

		var altCasePath string
		if driveLetter >= 'A' && driveLetter <= 'Z' {
			// Make lowercase
			altCasePath = string(driveLetter+32) + absFile[1:]
		} else {
			// Make uppercase
			altCasePath = string(driveLetter-32) + absFile[1:]
		}

		altResult, altErr := ValidateVEFilePath(altCasePath, allowedDirs)
		if altErr != nil {
			t.Fatalf("validation with toggled drive letter case should succeed (path=%q): %v", altCasePath, altErr)
		}
		if !strings.EqualFold(canonicalResult, altResult) {
			t.Errorf("canonical paths should match with toggled drive letter:\n  original: %s\n  toggled:  %s", canonicalResult, altResult)
		}

		// --- Test: Toggle drive letter case in allowed dir ---
		var altCaseDir string
		dirDrive := canonicalTestDir[0]
		if dirDrive >= 'A' && dirDrive <= 'Z' {
			altCaseDir = string(dirDrive+32) + canonicalTestDir[1:]
		} else {
			altCaseDir = string(dirDrive-32) + canonicalTestDir[1:]
		}

		altDirResult, altDirErr := ValidateVEFilePath(testFile, []string{altCaseDir})
		if altDirErr != nil {
			t.Fatalf("validation with toggled drive letter in allowed dir should succeed (dir=%q): %v", altCaseDir, altDirErr)
		}
		if !strings.EqualFold(canonicalResult, altDirResult) {
			t.Errorf("canonical paths should match with toggled drive letter in dir:\n  original: %s\n  toggled:  %s", canonicalResult, altDirResult)
		}
	})
}

func TestProperty3_WindowsForwardSlashesEquivalent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a test file with nested directories
		baseDir := os.TempDir()
		dir1 := genDirName().Draw(t, "dir1")
		dir2 := genDirName().Draw(t, "dir2")
		fileName := genFileName().Draw(t, "file_name")

		testDir := filepath.Join(baseDir, "ve_prop3_fwd_"+dir1)
		subDir := filepath.Join(testDir, dir2)
		testFile := filepath.Join(subDir, fileName)

		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Skip("cannot create test directory")
			return
		}
		defer os.RemoveAll(testDir)

		if err := os.WriteFile(testFile, []byte("slash test"), 0644); err != nil {
			t.Skip("cannot create test file")
			return
		}

		canonicalTestDir, err := filepath.Abs(testDir)
		if err != nil {
			t.Skip("cannot get absolute path")
			return
		}

		allowedDirs := []string{canonicalTestDir}

		// Validate with canonical (backslash) path
		canonicalResult, err := ValidateVEFilePath(testFile, allowedDirs)
		if err != nil {
			t.Fatalf("canonical validation should succeed: %v", err)
		}

		// Convert all backslashes to forward slashes
		forwardSlashPath := strings.ReplaceAll(testFile, "\\", "/")
		fwdResult, fwdErr := ValidateVEFilePath(forwardSlashPath, allowedDirs)
		if fwdErr != nil {
			t.Fatalf("validation with forward slashes should succeed (path=%q): %v", forwardSlashPath, fwdErr)
		}
		if !strings.EqualFold(canonicalResult, fwdResult) {
			t.Errorf("canonical paths should match with forward slashes:\n  backslash: %s\n  forward:   %s", canonicalResult, fwdResult)
		}

		// Convert allowed dir to forward slashes
		forwardSlashDir := strings.ReplaceAll(canonicalTestDir, "\\", "/")
		fwdDirResult, fwdDirErr := ValidateVEFilePath(testFile, []string{forwardSlashDir})
		if fwdDirErr != nil {
			t.Fatalf("validation with forward-slash allowed dir should succeed (dir=%q): %v", forwardSlashDir, fwdDirErr)
		}
		if !strings.EqualFold(canonicalResult, fwdDirResult) {
			t.Errorf("canonical paths should match with forward-slash dir:\n  backslash: %s\n  forward:   %s", canonicalResult, fwdDirResult)
		}

		// Random mix of slashes in both path and dir
		mixedPath := randomizeSlashes(testFile, t)
		mixedDir := randomizeSlashes(canonicalTestDir, t)
		mixedResult, mixedErr := ValidateVEFilePath(mixedPath, []string{mixedDir})
		if mixedErr != nil {
			t.Fatalf("validation with mixed slashes in both should succeed (path=%q, dir=%q): %v",
				mixedPath, mixedDir, mixedErr)
		}
		if !strings.EqualFold(canonicalResult, mixedResult) {
			t.Errorf("canonical paths should match with mixed slashes:\n  canonical: %s\n  mixed:     %s", canonicalResult, mixedResult)
		}
	})
}

func TestProperty3_IsWithinAllowedDirs_CaseInsensitive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a test directory
		baseDir := os.TempDir()
		testDirName := genDirName().Draw(t, "test_dir")
		subDirName := genDirName().Draw(t, "sub_dir")

		testDir := filepath.Join(baseDir, "ve_prop3_within_"+testDirName)
		subDir := filepath.Join(testDir, subDirName)

		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Skip("cannot create test directory")
			return
		}
		defer os.RemoveAll(testDir)

		canonicalTestDir, err := filepath.Abs(testDir)
		if err != nil {
			t.Skip("cannot get absolute path")
			return
		}

		allowedDirs := []string{canonicalTestDir}

		// Validate with canonical path
		canonicalResult, err := IsWithinAllowedDirs(subDir, allowedDirs)
		if err != nil {
			t.Fatalf("canonical validation should succeed: %v", err)
		}

		// Random casing of the subdirectory path
		casedSubDir := randomizeCasing(subDir, t)
		casedResult, casedErr := IsWithinAllowedDirs(casedSubDir, allowedDirs)
		if casedErr != nil {
			t.Fatalf("IsWithinAllowedDirs with random casing should succeed (path=%q): %v", casedSubDir, casedErr)
		}
		if !strings.EqualFold(canonicalResult, casedResult) {
			t.Errorf("canonical paths should match:\n  original: %s\n  cased:    %s", canonicalResult, casedResult)
		}

		// Random casing of allowed dir
		casedAllowedDir := randomizeCasing(canonicalTestDir, t)
		casedDirResult, casedDirErr := IsWithinAllowedDirs(subDir, []string{casedAllowedDir})
		if casedDirErr != nil {
			t.Fatalf("IsWithinAllowedDirs with random-cased allowed dir should succeed (dir=%q): %v", casedAllowedDir, casedDirErr)
		}
		if !strings.EqualFold(canonicalResult, casedDirResult) {
			t.Errorf("canonical paths should match with cased dir:\n  original: %s\n  cased:    %s", canonicalResult, casedDirResult)
		}

		// Mixed slashes
		slashedSubDir := randomizeSlashes(subDir, t)
		slashedResult, slashedErr := IsWithinAllowedDirs(slashedSubDir, allowedDirs)
		if slashedErr != nil {
			t.Fatalf("IsWithinAllowedDirs with mixed slashes should succeed (path=%q): %v", slashedSubDir, slashedErr)
		}
		if !strings.EqualFold(canonicalResult, slashedResult) {
			t.Errorf("canonical paths should match with mixed slashes:\n  original: %s\n  slashed:  %s", canonicalResult, slashedResult)
		}
	})
}
