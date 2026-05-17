package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests for VE Path Validator
// ---------------------------------------------------------------------------

func TestValidateVEFilePath_EmptyPath(t *testing.T) {
	_, err := ValidateVEFilePath("", []string{"C:\\allowed"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "path 参数不能为空") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateVEFilePath_WhitespacePath(t *testing.T) {
	_, err := ValidateVEFilePath("   ", []string{"C:\\allowed"})
	if err == nil {
		t.Fatal("expected error for whitespace-only path")
	}
	if !strings.Contains(err.Error(), "path 参数不能为空") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateVEFilePath_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDirs := []string{tmpDir}

	_, err := ValidateVEFilePath(filepath.Join(tmpDir, "nonexistent.txt"), allowedDirs)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "文件不存在") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateVEFilePath_DirectoryPath(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0755)
	allowedDirs := []string{tmpDir}

	_, err := ValidateVEFilePath(subDir, allowedDirs)
	if err == nil {
		t.Fatal("expected error for directory path")
	}
	if !strings.Contains(err.Error(), "路径是目录，请使用 list_directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateVEFilePath_FileWithinAllowedDir(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("hello"), 0644)
	allowedDirs := []string{tmpDir}

	canonical, err := ValidateVEFilePath(testFile, allowedDirs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical == "" {
		t.Fatal("expected non-empty canonical path")
	}
}

func TestValidateVEFilePath_FileInSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub", "deep")
	os.MkdirAll(subDir, 0755)
	testFile := filepath.Join(subDir, "doc.pdf")
	os.WriteFile(testFile, []byte("pdf content"), 0644)
	allowedDirs := []string{tmpDir}

	canonical, err := ValidateVEFilePath(testFile, allowedDirs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical == "" {
		t.Fatal("expected non-empty canonical path")
	}
}

func TestValidateVEFilePath_FileOutsideAllowedDir(t *testing.T) {
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()
	testFile := filepath.Join(outsideDir, "secret.txt")
	os.WriteFile(testFile, []byte("secret"), 0644)

	_, err := ValidateVEFilePath(testFile, []string{allowedDir})
	if err == nil {
		t.Fatal("expected error for file outside allowed dir")
	}
	if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateVEFilePath_DotDotTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	outsideDir := filepath.Join(tmpDir, "outside")
	os.MkdirAll(allowedDir, 0755)
	os.MkdirAll(outsideDir, 0755)

	// Create file outside allowed dir
	secretFile := filepath.Join(outsideDir, "secret.txt")
	os.WriteFile(secretFile, []byte("secret"), 0644)

	// Try to access via .. traversal
	traversalPath := filepath.Join(allowedDir, "..", "outside", "secret.txt")
	_, err := ValidateVEFilePath(traversalPath, []string{allowedDir})
	if err == nil {
		t.Fatal("expected error for path traversal via ..")
	}
	if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateVEFilePath_MultipleAllowedDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	testFile := filepath.Join(dir2, "file.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	// File is in dir2, both dirs are allowed
	canonical, err := ValidateVEFilePath(testFile, []string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical == "" {
		t.Fatal("expected non-empty canonical path")
	}
}

func TestValidateVEFilePath_EmptyAllowedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	_, err := ValidateVEFilePath(testFile, []string{})
	if err == nil {
		t.Fatal("expected error when allowed dirs is empty")
	}
	if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateVEFilePath_SymlinkWithinAllowedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	tmpDir := t.TempDir()
	realFile := filepath.Join(tmpDir, "real.txt")
	os.WriteFile(realFile, []byte("content"), 0644)
	linkFile := filepath.Join(tmpDir, "link.txt")
	os.Symlink(realFile, linkFile)

	canonical, err := ValidateVEFilePath(linkFile, []string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Canonical path should resolve to the real file
	if !strings.HasSuffix(canonical, "real.txt") {
		t.Errorf("expected canonical path to resolve to real.txt, got: %s", canonical)
	}
}

func TestValidateVEFilePath_SymlinkEscapingAllowedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	allowedDir := t.TempDir()
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.txt")
	os.WriteFile(secretFile, []byte("secret"), 0644)

	// Create symlink inside allowed dir pointing outside
	linkFile := filepath.Join(allowedDir, "escape.txt")
	os.Symlink(secretFile, linkFile)

	_, err := ValidateVEFilePath(linkFile, []string{allowedDir})
	if err == nil {
		t.Fatal("expected error for symlink escaping allowed dir")
	}
	if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// IsWithinAllowedDirs tests
// ---------------------------------------------------------------------------

func TestIsWithinAllowedDirs_EmptyPath(t *testing.T) {
	_, err := IsWithinAllowedDirs("", []string{"C:\\allowed"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "path 参数不能为空") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestIsWithinAllowedDirs_ExistingDirWithinAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)

	canonical, err := IsWithinAllowedDirs(subDir, []string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical == "" {
		t.Fatal("expected non-empty canonical path")
	}
}

func TestIsWithinAllowedDirs_NonExistentPathWithinAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	// Path doesn't exist but is within allowed dir
	nonExistent := filepath.Join(tmpDir, "does_not_exist")

	canonical, err := IsWithinAllowedDirs(nonExistent, []string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical == "" {
		t.Fatal("expected non-empty canonical path")
	}
}

func TestIsWithinAllowedDirs_PathOutsideAllowed(t *testing.T) {
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	_, err := IsWithinAllowedDirs(outsideDir, []string{allowedDir})
	if err == nil {
		t.Fatal("expected error for path outside allowed dir")
	}
	if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestIsWithinAllowedDirs_AllowedDirItself(t *testing.T) {
	tmpDir := t.TempDir()

	canonical, err := IsWithinAllowedDirs(tmpDir, []string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical == "" {
		t.Fatal("expected non-empty canonical path")
	}
}

// ---------------------------------------------------------------------------
// pathHasPrefix tests
// ---------------------------------------------------------------------------

func TestPathHasPrefix_ExactMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !pathHasPrefix(`C:\Users\test`, `C:\Users\test`) {
			t.Error("exact match should return true")
		}
	} else {
		if !pathHasPrefix("/home/user", "/home/user") {
			t.Error("exact match should return true")
		}
	}
}

func TestPathHasPrefix_SubdirectoryMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !pathHasPrefix(`C:\Users\test\file.txt`, `C:\Users\test`) {
			t.Error("subdirectory should match")
		}
	} else {
		if !pathHasPrefix("/home/user/file.txt", "/home/user") {
			t.Error("subdirectory should match")
		}
	}
}

func TestPathHasPrefix_SimilarPrefixNoMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		if pathHasPrefix(`C:\Users\testing`, `C:\Users\test`) {
			t.Error("similar prefix without separator should NOT match")
		}
	} else {
		if pathHasPrefix("/home/username", "/home/user") {
			t.Error("similar prefix without separator should NOT match")
		}
	}
}

func TestPathHasPrefix_WindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	if !pathHasPrefix(`C:\USERS\TEST\file.txt`, `c:\users\test`) {
		t.Error("Windows path comparison should be case-insensitive")
	}
}

// ---------------------------------------------------------------------------
// Windows-specific tests (run on all platforms but test Windows logic)
// ---------------------------------------------------------------------------

func TestValidateVEFilePath_WindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "Test.TXT")
	os.WriteFile(testFile, []byte("content"), 0644)

	// Use different casing for the allowed dir
	upperDir := strings.ToUpper(tmpDir)
	canonical, err := ValidateVEFilePath(testFile, []string{upperDir})
	if err != nil {
		t.Fatalf("Windows case-insensitive comparison should pass: %v", err)
	}
	if canonical == "" {
		t.Fatal("expected non-empty canonical path")
	}
}

// ===========================================================================
// Feature: ve-file-sharing-directories, Property 2: Path containment after canonical resolution
//
// For any file path and any list of allowed directories, if the canonical
// absolute form of the file path (resolved via filepath.EvalSymlinks +
// filepath.Abs) has the canonical absolute form of at least one allowed
// directory as a prefix, then ValidateVEFilePath SHALL return success. If the
// canonical path does not have any allowed directory's canonical path as a
// prefix, then ValidateVEFilePath SHALL return an error — regardless of
// whether the original path contained .. segments, symbolic links, or relative
// components.
//
// **Validates: Requirements 3.3, 3.4, 3.5, 3.6, 5.1, 5.2, 5.5**
// ===========================================================================

func TestProperty2_PathContainmentAfterCanonicalResolution(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- Setup: create a random directory tree ---
		baseDir := createTempDirForRapid(t)
		defer os.RemoveAll(baseDir)

		// Generate 1-3 allowed directories under baseDir
		numAllowed := rapid.IntRange(1, 3).Draw(t, "numAllowedDirs")
		allowedDirs := make([]string, numAllowed)
		for i := 0; i < numAllowed; i++ {
			dirName := rapid.StringMatching(`[a-z]{3,8}`).Draw(t, fmt.Sprintf("allowedDirName_%d", i))
			dir := filepath.Join(baseDir, "allowed", dirName)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("failed to create allowed dir: %v", err)
			}
			allowedDirs[i] = dir
		}

		// Create 1-3 subdirectories within each allowed dir (for depth)
		for _, allowedDir := range allowedDirs {
			numSub := rapid.IntRange(0, 2).Draw(t, "numSubDirs")
			for j := 0; j < numSub; j++ {
				subName := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, fmt.Sprintf("subDir_%d", j))
				subDir := filepath.Join(allowedDir, subName)
				os.MkdirAll(subDir, 0755)
			}
		}

		// Create an "outside" directory that is NOT in allowed dirs
		outsideDir := filepath.Join(baseDir, "outside")
		os.MkdirAll(outsideDir, 0755)

		// --- Property A: Files INSIDE allowed dirs are accepted ---
		// Pick a random allowed dir and create a file inside it
		chosenIdx := rapid.IntRange(0, len(allowedDirs)-1).Draw(t, "chosenAllowedIdx")
		chosenDir := allowedDirs[chosenIdx]

		// Optionally create file in a subdirectory
		useSubDir := rapid.Bool().Draw(t, "useSubDir")
		targetDir := chosenDir
		if useSubDir {
			subName := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "insideSubDir")
			targetDir = filepath.Join(chosenDir, subName)
			os.MkdirAll(targetDir, 0755)
		}

		insideFileName := rapid.StringMatching(`[a-z]{3,8}\.(txt|pdf|docx|md)`).Draw(t, "insideFileName")
		insideFilePath := filepath.Join(targetDir, insideFileName)
		if err := os.WriteFile(insideFilePath, []byte("test content"), 0644); err != nil {
			t.Fatalf("failed to create inside file: %v", err)
		}

		// Validate: file inside allowed dir should be accepted
		canonical, err := ValidateVEFilePath(insideFilePath, allowedDirs)
		if err != nil {
			t.Fatalf("file inside allowed dir should be accepted, got error: %v (path=%s, allowedDirs=%v)", err, insideFilePath, allowedDirs)
		}
		if canonical == "" {
			t.Fatal("canonical path should not be empty for accepted file")
		}

		// --- Property B: Files OUTSIDE allowed dirs are rejected ---
		outsideFileName := rapid.StringMatching(`[a-z]{3,8}\.(txt|pdf|docx)`).Draw(t, "outsideFileName")
		outsideFilePath := filepath.Join(outsideDir, outsideFileName)
		if err := os.WriteFile(outsideFilePath, []byte("secret content"), 0644); err != nil {
			t.Fatalf("failed to create outside file: %v", err)
		}

		_, err = ValidateVEFilePath(outsideFilePath, allowedDirs)
		if err == nil {
			t.Fatalf("file outside allowed dirs should be rejected (path=%s, allowedDirs=%v)", outsideFilePath, allowedDirs)
		}
		if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
			t.Fatalf("expected containment error, got: %v", err)
		}

		// --- Property C: .. segments that resolve INSIDE are accepted ---
		// Create path with .. that still resolves inside the allowed dir
		// e.g., allowedDir/sub/../file.txt → allowedDir/file.txt (still inside)
		dotDotInsideDir := filepath.Join(chosenDir, "tempSub")
		os.MkdirAll(dotDotInsideDir, 0755)
		dotDotInsideFile := filepath.Join(chosenDir, "dotdot_test.txt")
		os.WriteFile(dotDotInsideFile, []byte("dotdot inside"), 0644)

		// Path with .. that resolves back to chosenDir
		dotDotPath := filepath.Join(dotDotInsideDir, "..", "dotdot_test.txt")
		canonical, err = ValidateVEFilePath(dotDotPath, allowedDirs)
		if err != nil {
			t.Fatalf("path with .. resolving inside should be accepted, got error: %v (path=%s)", err, dotDotPath)
		}
		if canonical == "" {
			t.Fatal("canonical path should not be empty for .. path resolving inside")
		}

		// --- Property D: .. segments that resolve OUTSIDE are rejected ---
		// Create a file outside and try to access via .. from inside
		outsideViaTraversal := filepath.Join(outsideDir, "traversal_target.txt")
		os.WriteFile(outsideViaTraversal, []byte("traversal target"), 0644)

		// Build a path that uses .. to escape: allowedDir/../../outside/traversal_target.txt
		traversalPath := filepath.Join(chosenDir, "..", "..", "outside", "traversal_target.txt")
		_, err = ValidateVEFilePath(traversalPath, allowedDirs)
		if err == nil {
			t.Fatalf("path with .. resolving outside should be rejected (path=%s)", traversalPath)
		}
		if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
			t.Fatalf("expected containment error for traversal, got: %v", err)
		}

		// --- Property E: Relative path components resolve correctly ---
		// Use a relative-style path (with .) that resolves inside
		dotPath := filepath.Join(chosenDir, ".", insideFileName)
		// Only test if the file exists at this path (it should since . is identity)
		if _, statErr := os.Stat(dotPath); statErr == nil {
			canonical, err = ValidateVEFilePath(dotPath, allowedDirs)
			if err != nil {
				t.Fatalf("path with . component resolving inside should be accepted, got error: %v", err)
			}
			if canonical == "" {
				t.Fatal("canonical path should not be empty for . path")
			}
		}
	})
}

func TestProperty2_SymlinkContainment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	rapid.Check(t, func(t *rapid.T) {
		baseDir := createTempDirForRapid(t)
		defer os.RemoveAll(baseDir)

		// Create allowed directory
		allowedDir := filepath.Join(baseDir, "allowed")
		os.MkdirAll(allowedDir, 0755)

		// Create outside directory
		outsideDir := filepath.Join(baseDir, "outside")
		os.MkdirAll(outsideDir, 0755)

		allowedDirs := []string{allowedDir}

		// --- Property: Symlink inside allowed dir pointing to file inside → accepted ---
		insideName := rapid.StringMatching(`[a-z]{3,6}\.txt`).Draw(t, "insideTarget")
		insideTarget := filepath.Join(allowedDir, insideName)
		os.WriteFile(insideTarget, []byte("inside content"), 0644)

		linkInsideName := rapid.StringMatching(`link_[a-z]{3,5}\.txt`).Draw(t, "linkInsideName")
		linkInside := filepath.Join(allowedDir, linkInsideName)
		// Remove if exists from previous iteration
		os.Remove(linkInside)
		if err := os.Symlink(insideTarget, linkInside); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		canonical, err := ValidateVEFilePath(linkInside, allowedDirs)
		if err != nil {
			t.Fatalf("symlink to file inside allowed dir should be accepted: %v", err)
		}
		if canonical == "" {
			t.Fatal("canonical path should not be empty")
		}

		// --- Property: Symlink inside allowed dir pointing to file OUTSIDE → rejected ---
		outsideName := rapid.StringMatching(`[a-z]{3,6}\.txt`).Draw(t, "outsideTarget")
		outsideTarget := filepath.Join(outsideDir, outsideName)
		os.WriteFile(outsideTarget, []byte("outside content"), 0644)

		linkEscapeName := rapid.StringMatching(`escape_[a-z]{3,5}\.txt`).Draw(t, "linkEscapeName")
		linkEscape := filepath.Join(allowedDir, linkEscapeName)
		os.Remove(linkEscape)
		if err := os.Symlink(outsideTarget, linkEscape); err != nil {
			t.Fatalf("failed to create escape symlink: %v", err)
		}

		_, err = ValidateVEFilePath(linkEscape, allowedDirs)
		if err == nil {
			t.Fatal("symlink escaping allowed dir should be rejected")
		}
		if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
			t.Fatalf("expected containment error for symlink escape, got: %v", err)
		}
	})
}

func TestProperty2_IsWithinAllowedDirs_Containment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		baseDir := createTempDirForRapid(t)
		defer os.RemoveAll(baseDir)

		// Create allowed directory with subdirectories
		allowedDir := filepath.Join(baseDir, "allowed")
		os.MkdirAll(allowedDir, 0755)

		subName := rapid.StringMatching(`[a-z]{2,5}`).Draw(t, "subDirName")
		subDir := filepath.Join(allowedDir, subName)
		os.MkdirAll(subDir, 0755)

		// Create outside directory
		outsideDir := filepath.Join(baseDir, "outside")
		os.MkdirAll(outsideDir, 0755)

		allowedDirs := []string{allowedDir}

		// --- Property: Existing path inside → accepted ---
		canonical, err := IsWithinAllowedDirs(subDir, allowedDirs)
		if err != nil {
			t.Fatalf("existing path inside allowed dir should be accepted: %v", err)
		}
		if canonical == "" {
			t.Fatal("canonical path should not be empty")
		}

		// --- Property: Non-existing path inside → accepted (for list_directory) ---
		nonExistName := rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "nonExistDir")
		nonExistPath := filepath.Join(allowedDir, nonExistName)
		canonical, err = IsWithinAllowedDirs(nonExistPath, allowedDirs)
		if err != nil {
			t.Fatalf("non-existing path inside allowed dir should be accepted: %v", err)
		}
		if canonical == "" {
			t.Fatal("canonical path should not be empty for non-existing inside path")
		}

		// --- Property: Path outside → rejected ---
		_, err = IsWithinAllowedDirs(outsideDir, allowedDirs)
		if err == nil {
			t.Fatal("path outside allowed dirs should be rejected")
		}
		if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
			t.Fatalf("expected containment error, got: %v", err)
		}

		// --- Property: .. traversal outside → rejected ---
		traversalPath := filepath.Join(allowedDir, "..", "outside")
		_, err = IsWithinAllowedDirs(traversalPath, allowedDirs)
		if err == nil {
			t.Fatal(".. traversal outside should be rejected")
		}
		if !strings.Contains(err.Error(), "文件不在允许访问的目录中") {
			t.Fatalf("expected containment error for traversal, got: %v", err)
		}

		// --- Property: .. traversal that stays inside → accepted ---
		staysInsidePath := filepath.Join(subDir, "..", subName)
		canonical, err = IsWithinAllowedDirs(staysInsidePath, allowedDirs)
		if err != nil {
			t.Fatalf(".. traversal staying inside should be accepted: %v", err)
		}
		if canonical == "" {
			t.Fatal("canonical path should not be empty for .. staying inside")
		}
	})
}

// ---------------------------------------------------------------------------
// Property-Based Tests
// ---------------------------------------------------------------------------

// TestProperty4_SensitiveFileBlockingWithinAllowedDirs verifies that sensitive
// files are always rejected by vePathIsSensitive regardless of casing.
//
// Feature: ve-file-sharing-directories, Property 4: Sensitive file blocking within allowed directories
//
// **Validates: Requirements 8.1, 8.2, 8.3, 8.4**
func TestProperty4_SensitiveFileBlockingWithinAllowedDirs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Create a temporary directory as the allowed dir
		dir := createTempDirForRapid(t)

		// Generate a sensitive file name with random casing
		sensitiveBase := rapid.SampledFrom([]string{
			".env", ".pem", ".key", "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa",
			".env.local", ".env.production", ".env.staging", ".env.development",
			".env.test", ".env.backup",
			"credentials", "credentials.json",
			".npmrc", ".pypirc", ".netrc",
			"keystore.jks", "keystore.p12",
		}).Draw(t, "sensitiveBase")

		// Apply random casing to the sensitive file name
		randomCased := applyRandomCasing(t, sensitiveBase)

		// Also test sensitive extensions with random file names
		sensitiveExt := rapid.SampledFrom([]string{
			".pem", ".key", ".p12", ".pfx", ".jks",
		}).Draw(t, "sensitiveExt")

		// Generate a random prefix for extension-based files
		prefix := rapid.StringMatching(`[a-zA-Z0-9_]{1,10}`).Draw(t, "prefix")
		extBasedFile := prefix + applyRandomCasing(t, sensitiveExt)

		// Test 1: Exact name sensitive files with random casing
		filePath := filepath.Join(dir, randomCased)
		if !vePathIsSensitive(filePath) {
			t.Fatalf("vePathIsSensitive should return true for sensitive file %q (original: %q), path: %s",
				randomCased, sensitiveBase, filePath)
		}

		// Test 2: Extension-based sensitive files with random casing
		extFilePath := filepath.Join(dir, extBasedFile)
		if !vePathIsSensitive(extFilePath) {
			t.Fatalf("vePathIsSensitive should return true for extension-based sensitive file %q (ext: %q), path: %s",
				extBasedFile, sensitiveExt, extFilePath)
		}
	})
}

// TestProperty4_SensitiveFileBlockingInSubdirectories verifies that sensitive
// files are blocked even when nested in subdirectories within allowed dirs.
//
// **Validates: Requirements 8.1, 8.2, 8.3, 8.4**
func TestProperty4_SensitiveFileBlockingInSubdirectories(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := createTempDirForRapid(t)

		// Generate random subdirectory depth (1-4 levels)
		depth := rapid.IntRange(1, 4).Draw(t, "depth")
		subPath := dir
		for i := 0; i < depth; i++ {
			segment := rapid.StringMatching(`[a-zA-Z0-9]{1,8}`).Draw(t, fmt.Sprintf("seg%d", i))
			subPath = filepath.Join(subPath, segment)
		}

		// Generate a sensitive file name with random casing
		sensitiveBase := rapid.SampledFrom([]string{
			".env", ".key", ".pem", "id_rsa",
			".env.local", ".env.production",
		}).Draw(t, "sensitiveBase")

		randomCased := applyRandomCasing(t, sensitiveBase)
		filePath := filepath.Join(subPath, randomCased)

		if !vePathIsSensitive(filePath) {
			t.Fatalf("vePathIsSensitive should return true for sensitive file %q at depth %d, path: %s",
				randomCased, depth, filePath)
		}
	})
}

// TestProperty4_SensitiveDirectoryBlocking verifies that files within
// sensitive directories (.ssh, secrets) are always blocked.
//
// **Validates: Requirements 8.1, 8.2**
func TestProperty4_SensitiveDirectoryBlocking(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := createTempDirForRapid(t)

		// Generate a sensitive directory name
		sensitiveDir := rapid.SampledFrom([]string{
			".ssh", "secrets",
		}).Draw(t, "sensitiveDir")

		// Generate a random file name (not necessarily sensitive by name)
		fileName := rapid.StringMatching(`[a-zA-Z0-9_]{1,10}\.[a-z]{1,4}`).Draw(t, "fileName")

		filePath := filepath.Join(dir, sensitiveDir, fileName)

		if !vePathIsSensitive(filePath) {
			t.Fatalf("vePathIsSensitive should return true for file %q in sensitive directory %q, path: %s",
				fileName, sensitiveDir, filePath)
		}
	})
}

// ===========================================================================
// Feature: ve-file-sharing-directories, Property 9: File size limit enforcement
//
// For any file within an allowed directory with size exceeding 50 MB, the
// send_file operation SHALL be rejected with a size-exceeded error message.
// For any file at or below 50 MB, the size check SHALL pass.
//
// **Validates: Requirements 4.3**
// ===========================================================================

// veMaxFileSizeForTest mirrors the constant defined in app_ve_handler.go.
// This is the 50 MB limit enforced in ExecuteTool for send_file.
const veMaxFileSizeForTest = 50 * 1024 * 1024 // 50 MB

// checkVEFileSize mirrors the size-checking logic in veAgentCallbacks.ExecuteTool
// for the send_file operation. Returns nil if the file is within the size limit,
// or an error message string if it exceeds the limit.
func checkVEFileSize(filePath string) error {
	info, statErr := os.Stat(filePath)
	if statErr != nil {
		return fmt.Errorf("[error] 无法读取文件: %v", statErr)
	}
	if info.Size() > veMaxFileSizeForTest {
		return fmt.Errorf("[error] 文件过大（%d bytes），VE 模式最大支持 50 MB", info.Size())
	}
	return nil
}

func TestProperty9_FileSizeLimitEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		baseDir := createTempDirForRapid(t)
		defer os.RemoveAll(baseDir)

		// Generate a random file size around the 50 MB boundary.
		// Strategy: generate sizes in several ranges to thoroughly test the boundary:
		// - Well below limit (0 to 40 MB)
		// - Near boundary below (49 MB to 50 MB)
		// - Exactly at boundary (50 MB)
		// - Near boundary above (50 MB + 1 byte to 51 MB)
		// - Well above limit (51 MB to 60 MB)
		//
		// Since creating actual 50 MB files is slow, we use two approaches:
		// 1. For sizes <= 1 MB: create actual files with real content
		// 2. For sizes > 1 MB: create sparse files (seek to size, write 1 byte)
		//    This is fast and os.Stat still reports the correct size.

		sizeCategory := rapid.IntRange(0, 4).Draw(t, "sizeCategory")
		var fileSize int64

		switch sizeCategory {
		case 0: // Well below limit: 0 to 40 MB
			fileSize = rapid.Int64Range(0, 40*1024*1024).Draw(t, "size_below")
		case 1: // Near boundary below: 49 MB to 50 MB (inclusive)
			fileSize = rapid.Int64Range(49*1024*1024, veMaxFileSizeForTest).Draw(t, "size_near_below")
		case 2: // Exactly at boundary: 50 MB
			fileSize = veMaxFileSizeForTest
		case 3: // Near boundary above: 50 MB + 1 to 51 MB
			fileSize = rapid.Int64Range(veMaxFileSizeForTest+1, 51*1024*1024).Draw(t, "size_near_above")
		case 4: // Well above limit: 51 MB to 60 MB
			fileSize = rapid.Int64Range(51*1024*1024, 60*1024*1024).Draw(t, "size_above")
		}

		// Create a file with the target size using sparse file technique
		fileName := rapid.StringMatching(`[a-z]{3,8}\.(txt|pdf|docx|zip)`).Draw(t, "fileName")
		filePath := filepath.Join(baseDir, fileName)

		f, err := os.Create(filePath)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		if fileSize > 0 {
			// Seek to size-1 and write one byte to create a sparse file
			if _, err := f.Seek(fileSize-1, 0); err != nil {
				f.Close()
				t.Fatalf("failed to seek: %v", err)
			}
			if _, err := f.Write([]byte{0}); err != nil {
				f.Close()
				t.Fatalf("failed to write: %v", err)
			}
		}
		f.Close()

		// Verify the file has the expected size
		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("failed to stat file: %v", err)
		}
		if info.Size() != fileSize {
			t.Fatalf("file size mismatch: expected %d, got %d", fileSize, info.Size())
		}

		// Check the size limit
		sizeErr := checkVEFileSize(filePath)

		// Property: files at or below 50 MB are accepted, files above are rejected
		if fileSize <= veMaxFileSizeForTest {
			// Should be accepted (no error)
			if sizeErr != nil {
				t.Fatalf("file of size %d bytes (%.2f MB) should be accepted (≤ 50 MB), got error: %v",
					fileSize, float64(fileSize)/(1024*1024), sizeErr)
			}
		} else {
			// Should be rejected (error)
			if sizeErr == nil {
				t.Fatalf("file of size %d bytes (%.2f MB) should be rejected (> 50 MB), but was accepted",
					fileSize, float64(fileSize)/(1024*1024))
			}
			if !strings.Contains(sizeErr.Error(), "文件过大") {
				t.Fatalf("expected size-exceeded error message, got: %v", sizeErr)
			}
			if !strings.Contains(sizeErr.Error(), "50 MB") {
				t.Fatalf("error message should mention 50 MB limit, got: %v", sizeErr)
			}
		}

		// Cleanup: remove the potentially large sparse file
		os.Remove(filePath)
	})
}

// TestProperty9_FileSizeLimitBoundaryPrecise tests the exact boundary at 50*1024*1024 bytes.
// This is a focused test that verifies the boundary is exactly at 50 MB (not off-by-one).
func TestProperty9_FileSizeLimitBoundaryPrecise(t *testing.T) {
	baseDir := t.TempDir()

	// Test exact boundary: 50 MB should be accepted
	exactFile := filepath.Join(baseDir, "exact_50mb.bin")
	f, err := os.Create(exactFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if veMaxFileSizeForTest > 0 {
		f.Seek(veMaxFileSizeForTest-1, 0)
		f.Write([]byte{0})
	}
	f.Close()

	info, _ := os.Stat(exactFile)
	if info.Size() != veMaxFileSizeForTest {
		t.Fatalf("expected file size %d, got %d", veMaxFileSizeForTest, info.Size())
	}
	if err := checkVEFileSize(exactFile); err != nil {
		t.Fatalf("file of exactly 50 MB (%d bytes) should be accepted: %v", veMaxFileSizeForTest, err)
	}

	// Test boundary + 1: 50 MB + 1 byte should be rejected
	overFile := filepath.Join(baseDir, "over_50mb.bin")
	f, err = os.Create(overFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	f.Seek(veMaxFileSizeForTest, 0) // seek to 50 MB (one past the limit)
	f.Write([]byte{0})
	f.Close()

	info, _ = os.Stat(overFile)
	if info.Size() != veMaxFileSizeForTest+1 {
		t.Fatalf("expected file size %d, got %d", veMaxFileSizeForTest+1, info.Size())
	}
	if err := checkVEFileSize(overFile); err == nil {
		t.Fatalf("file of 50 MB + 1 byte (%d bytes) should be rejected", veMaxFileSizeForTest+1)
	}

	// Test boundary - 1: 50 MB - 1 byte should be accepted
	underFile := filepath.Join(baseDir, "under_50mb.bin")
	f, err = os.Create(underFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if veMaxFileSizeForTest > 1 {
		f.Seek(veMaxFileSizeForTest-2, 0)
		f.Write([]byte{0})
	}
	f.Close()

	info, _ = os.Stat(underFile)
	if info.Size() != veMaxFileSizeForTest-1 {
		t.Fatalf("expected file size %d, got %d", veMaxFileSizeForTest-1, info.Size())
	}
	if err := checkVEFileSize(underFile); err != nil {
		t.Fatalf("file of 50 MB - 1 byte (%d bytes) should be accepted: %v", veMaxFileSizeForTest-1, err)
	}
}

// ---------------------------------------------------------------------------
// Helpers for property tests
// ---------------------------------------------------------------------------

// createTempDirForRapid creates a temporary directory for use in rapid tests.
// It uses os.MkdirTemp since rapid.T doesn't have TempDir().
// The caller should defer os.RemoveAll(dir) if the test creates files that
// should not persist. For property tests that run 100+ iterations, cleanup
// prevents accumulating hundreds of temp directories.
func createTempDirForRapid(t *rapid.T) string {
	dir, err := os.MkdirTemp("", "ve_pbt_*")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp dir: %v", err))
	}
	return dir
}

// applyRandomCasing applies random upper/lower casing to each character of a string.
func applyRandomCasing(t *rapid.T, s string) string {
	var result strings.Builder
	for i, ch := range s {
		if ch >= 'a' && ch <= 'z' {
			// Randomly decide to uppercase this character
			if rapid.Bool().Draw(t, fmt.Sprintf("upper_%d", i)) {
				result.WriteRune(ch - 32) // to uppercase
			} else {
				result.WriteRune(ch)
			}
		} else if ch >= 'A' && ch <= 'Z' {
			// Randomly decide to lowercase this character
			if rapid.Bool().Draw(t, fmt.Sprintf("lower_%d", i)) {
				result.WriteRune(ch + 32) // to lowercase
			} else {
				result.WriteRune(ch)
			}
		} else {
			result.WriteRune(ch)
		}
	}
	return result.String()
}
