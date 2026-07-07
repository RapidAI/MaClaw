package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Package Manifest — integrity verification for skill packages.
//
// Generated at upload time, verified at install time. Ensures that the package
// received by the installer is byte-for-byte identical to what the author
// uploaded.
// ────────────────────────────────────────────────────────────────────────────

const PackageManifestFileName = "skill_integrity_manifest.json"

// PackageManifest records the SHA256 hash of every file in a skill package.
// Written to the skill directory at upload time, checked at install time.
type PackageManifest struct {
	SkillID       string            `json:"skill_id"`
	Version       string            `json:"version"`
	GeneratedAt   string            `json:"generated_at"`
	PackageSHA256 string            `json:"package_sha256,omitempty"` // SHA256 of the zip file (set by Hub after upload)
	Files         map[string]string `json:"files"`                   // relative path → SHA256 hex
}

// GeneratePackageManifest creates a manifest by hashing all files in the skill directory.
// Excludes runtime/cache files (see IsSkillRuntimePackageFile/IsSkillRuntimePackageDir).
func GeneratePackageManifest(skillDir, skillID, version string) (*PackageManifest, error) {
	manifest := &PackageManifest{
		SkillID:     skillID,
		Version:     version,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Files:       make(map[string]string),
	}

	err := filepath.Walk(skillDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if info.IsDir() {
			if IsSkillRuntimePackageDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip runtime files and the manifest itself
		if IsSkillRuntimePackageFile(rel) || rel == PackageManifestFileName {
			return nil
		}

		hash, err := hashFileSHA256(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		manifest.Files[rel] = hash
		return nil
	})
	if err != nil {
		return nil, err
	}

	return manifest, nil
}

// WritePackageManifest writes the manifest to the skill directory.
func WritePackageManifest(skillDir string, manifest *PackageManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(skillDir, PackageManifestFileName), append(data, '\n'), 0o644)
}

// ReadPackageManifest reads the manifest from a skill directory.
// Returns nil, nil if the manifest file does not exist (legacy skill without manifest).
func ReadPackageManifest(skillDir string) (*PackageManifest, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, PackageManifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var manifest PackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &manifest, nil
}

// VerifyPackageIntegrity checks all files in the manifest against actual disk content.
// Returns nil if all hashes match. Returns a descriptive error listing mismatches.
func VerifyPackageIntegrity(skillDir string, manifest *PackageManifest) error {
	if manifest == nil || len(manifest.Files) == 0 {
		return nil // no manifest = nothing to verify (legacy skill)
	}

	var mismatches []string
	var missing []string

	for relPath, expectedHash := range manifest.Files {
		absPath := filepath.Join(skillDir, filepath.FromSlash(relPath))
		actualHash, err := hashFileSHA256(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, relPath)
			} else {
				mismatches = append(mismatches, fmt.Sprintf("%s: read error: %v", relPath, err))
			}
			continue
		}
		if !strings.EqualFold(actualHash, expectedHash) {
			expected := expectedHash
			actual := actualHash
			if len(expected) > 12 {
				expected = expected[:12] + "..."
			}
			if len(actual) > 12 {
				actual = actual[:12] + "..."
			}
			mismatches = append(mismatches, fmt.Sprintf("%s: expected %s, got %s", relPath, expected, actual))
		}
	}

	if len(mismatches) == 0 && len(missing) == 0 {
		return nil
	}

	sort.Strings(mismatches)
	sort.Strings(missing)

	var b strings.Builder
	b.WriteString("package integrity check failed:\n")
	for _, m := range mismatches {
		b.WriteString("  MISMATCH: " + m + "\n")
	}
	for _, m := range missing {
		b.WriteString("  MISSING: " + m + "\n")
	}
	return fmt.Errorf("%s", b.String())
}

// hashFileSHA256 returns the hex-encoded SHA256 hash of a file.
func hashFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
