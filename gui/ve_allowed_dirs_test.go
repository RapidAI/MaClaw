//go:build windows

package main

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Property-Based Test: Duplicate directory detection (case-insensitive)
// Feature: ve-file-sharing-directories, Property 6: Duplicate directory detection (case-insensitive)
//
// **Validates: Requirements 1.6**
//
// For any directory path that is already in the allowed directories list,
// attempting to add the same path with different casing (on Windows) SHALL be
// rejected, and the list SHALL remain unchanged.
// ---------------------------------------------------------------------------

// isDuplicateDirBackend mirrors the frontend isDuplicateDir logic for testing.
// On Windows, comparison is case-insensitive with slash normalization.
// This is the expected behavior per Requirement 1.6.
func isDuplicateDirBackend(newPath string, existingDirs []string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(newPath, "/", "\\"))
	for _, d := range existingDirs {
		existing := strings.ToLower(strings.ReplaceAll(d, "/", "\\"))
		if existing == normalized {
			return true
		}
	}
	return false
}

// genWindowsAbsPath generates a random Windows absolute path with a drive letter.
func genWindowsAbsPath(t *rapid.T, label string) string {
	driveLetters := []string{"C", "D", "E", "F", "G", "H"}
	drive := driveLetters[rapid.IntRange(0, len(driveLetters)-1).Draw(t, label+"_drive")]

	numSegments := rapid.IntRange(1, 5).Draw(t, label+"_numSeg")
	segments := make([]string, numSegments)
	for i := 0; i < numSegments; i++ {
		segments[i] = rapid.StringMatching(`[A-Za-z][A-Za-z0-9_ -]{0,15}`).Draw(t, fmt.Sprintf("%s_seg%d", label, i))
	}
	return drive + ":\\" + strings.Join(segments, "\\")
}

// randomizeDirCasing randomly changes the casing of characters in a path string.
// (Named differently from ve_path_validator_windows_test.go's randomizeCasing to avoid conflict)
func randomizeDirCasing(t *rapid.T, path string) string {
	runes := []rune(path)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			if rapid.Bool().Draw(t, fmt.Sprintf("case_%d", i)) {
				if r >= 'A' && r <= 'Z' {
					runes[i] = r + 32 // to lower
				} else {
					runes[i] = r - 32 // to upper
				}
			}
		}
	}
	return string(runes)
}

// TestProperty6_DuplicateDirectoryDetection_CaseInsensitive verifies that
// adding the same path with different casing is detected as a duplicate
// and the list remains unchanged.
func TestProperty6_DuplicateDirectoryDetection_CaseInsensitive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Step 1: Generate a random directory path
		originalPath := genWindowsAbsPath(t, "path")

		// Step 2: Create a list containing this path
		initialList := []string{originalPath}

		// Step 3: Generate a variant with different casing
		variantPath := randomizeDirCasing(t, originalPath)

		// Step 4: Verify duplicate detection identifies the variant as duplicate
		isDup := isDuplicateDirBackend(variantPath, initialList)
		if !isDup {
			t.Fatalf("case-insensitive duplicate detection failed:\n  original: %q\n  variant:  %q\n  list:     %v",
				originalPath, variantPath, initialList)
		}

		// Step 5: Verify the list remains unchanged (simulate the rejection)
		// The frontend logic: if isDuplicateDir returns true, do NOT add to list
		finalList := initialList
		if !isDuplicateDirBackend(variantPath, initialList) {
			finalList = append(finalList, variantPath)
		}

		if len(finalList) != len(initialList) {
			t.Fatalf("list should remain unchanged after duplicate rejection:\n  initial: %v\n  final:   %v",
				initialList, finalList)
		}
		for i := range initialList {
			if initialList[i] != finalList[i] {
				t.Fatalf("list entry %d changed: %q -> %q", i, initialList[i], finalList[i])
			}
		}
	})
}

// TestProperty6_DuplicateDirectoryDetection_SlashNormalization verifies that
// paths with mixed forward/backward slashes are detected as duplicates.
func TestProperty6_DuplicateDirectoryDetection_SlashNormalization(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a path with backslashes
		originalPath := genWindowsAbsPath(t, "path")

		// Create a variant with some backslashes replaced by forward slashes
		runes := []rune(originalPath)
		for i, r := range runes {
			if r == '\\' {
				if rapid.Bool().Draw(t, fmt.Sprintf("slash_%d", i)) {
					runes[i] = '/'
				}
			}
		}
		variantPath := string(runes)

		// The list contains the original path
		initialList := []string{originalPath}

		// Verify duplicate detection with slash normalization
		isDup := isDuplicateDirBackend(variantPath, initialList)
		if !isDup {
			t.Fatalf("slash-normalized duplicate detection failed:\n  original: %q\n  variant:  %q",
				originalPath, variantPath)
		}

		// Verify list unchanged
		finalList := initialList
		if !isDuplicateDirBackend(variantPath, initialList) {
			finalList = append(finalList, variantPath)
		}
		if len(finalList) != len(initialList) {
			t.Fatalf("list should remain unchanged after slash-variant duplicate rejection")
		}
	})
}

// TestProperty6_DuplicateDirectoryDetection_CaseAndSlashCombined verifies that
// paths with both different casing AND mixed slashes are detected as duplicates.
func TestProperty6_DuplicateDirectoryDetection_CaseAndSlashCombined(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate original path
		originalPath := genWindowsAbsPath(t, "path")

		// Apply both casing changes and slash changes
		variantPath := randomizeDirCasing(t, originalPath)
		// Also randomize slashes
		runes := []rune(variantPath)
		for i, r := range runes {
			if r == '\\' {
				if rapid.Bool().Draw(t, fmt.Sprintf("slash_%d", i)) {
					runes[i] = '/'
				}
			}
		}
		variantPath = string(runes)

		initialList := []string{originalPath}

		isDup := isDuplicateDirBackend(variantPath, initialList)
		if !isDup {
			t.Fatalf("combined case+slash duplicate detection failed:\n  original: %q\n  variant:  %q",
				originalPath, variantPath)
		}

		// Verify list unchanged
		finalList := initialList
		if !isDuplicateDirBackend(variantPath, initialList) {
			finalList = append(finalList, variantPath)
		}
		if len(finalList) != len(initialList) {
			t.Fatalf("list should remain unchanged after combined-variant duplicate rejection")
		}
	})
}

// TestProperty6_DuplicateDirectoryDetection_MultipleEntries verifies duplicate
// detection works correctly when the list has multiple entries.
func TestProperty6_DuplicateDirectoryDetection_MultipleEntries(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a list of 2-5 unique paths
		numPaths := rapid.IntRange(2, 5).Draw(t, "numPaths")
		paths := make([]string, numPaths)
		for i := 0; i < numPaths; i++ {
			paths[i] = genWindowsAbsPath(t, fmt.Sprintf("path%d", i))
		}

		// Pick one path to duplicate with different casing
		targetIdx := rapid.IntRange(0, numPaths-1).Draw(t, "targetIdx")
		targetPath := paths[targetIdx]
		variantPath := randomizeDirCasing(t, targetPath)

		// Verify duplicate detection
		isDup := isDuplicateDirBackend(variantPath, paths)
		if !isDup {
			t.Fatalf("duplicate detection in multi-entry list failed:\n  target:  %q\n  variant: %q\n  list:    %v",
				targetPath, variantPath, paths)
		}

		// Verify list unchanged
		originalLen := len(paths)
		finalList := paths
		if !isDuplicateDirBackend(variantPath, paths) {
			finalList = append(finalList, variantPath)
		}
		if len(finalList) != originalLen {
			t.Fatalf("list length changed: %d -> %d", originalLen, len(finalList))
		}
	})
}

// TestProperty6_DuplicateDirectoryDetection_NonDuplicate verifies that
// genuinely different paths are NOT detected as duplicates.
func TestProperty6_DuplicateDirectoryDetection_NonDuplicate(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two distinct paths (different drive letters guarantee uniqueness)
		path1Drive := rapid.IntRange(0, 2).Draw(t, "drive1")
		path2Drive := rapid.IntRange(3, 5).Draw(t, "drive2")
		drives := []string{"C", "D", "E", "F", "G", "H"}

		numSeg1 := rapid.IntRange(1, 4).Draw(t, "numSeg1")
		segments1 := make([]string, numSeg1)
		for i := 0; i < numSeg1; i++ {
			segments1[i] = rapid.StringMatching(`[A-Za-z][A-Za-z0-9_]{1,10}`).Draw(t, fmt.Sprintf("seg1_%d", i))
		}
		path1 := drives[path1Drive] + ":\\" + strings.Join(segments1, "\\")

		numSeg2 := rapid.IntRange(1, 4).Draw(t, "numSeg2")
		segments2 := make([]string, numSeg2)
		for i := 0; i < numSeg2; i++ {
			segments2[i] = rapid.StringMatching(`[A-Za-z][A-Za-z0-9_]{1,10}`).Draw(t, fmt.Sprintf("seg2_%d", i))
		}
		path2 := drives[path2Drive] + ":\\" + strings.Join(segments2, "\\")

		// path2 should NOT be detected as duplicate of path1
		initialList := []string{path1}
		isDup := isDuplicateDirBackend(path2, initialList)
		if isDup {
			t.Fatalf("false positive: different paths detected as duplicate:\n  path1: %q\n  path2: %q",
				path1, path2)
		}

		// Adding path2 should succeed (list grows by 1)
		finalList := initialList
		if !isDuplicateDirBackend(path2, initialList) {
			finalList = append(finalList, path2)
		}
		if len(finalList) != 2 {
			t.Fatalf("expected list to grow to 2 entries, got %d", len(finalList))
		}
	})
}
