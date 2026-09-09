package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
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
	Files         map[string]string `json:"files"`                    // relative path → SHA256 hex
}

// GeneratePackageManifest creates a manifest by hashing all files in the skill directory.
// Excludes runtime/cache files (see IsSkillRuntimePackageFile/IsSkillRuntimePackageDir).
func GeneratePackageManifest(skillDir, skillID, version string) (*PackageManifest, error) {
	if err := validatePackageManifestDir(skillDir); err != nil {
		return nil, err
	}
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
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("manifest refuses symlink entry: %s", rel)
		}

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
	if err := validatePackageManifestDir(skillDir); err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("manifest is nil")
	}
	target := filepath.Join(skillDir, PackageManifestFileName)
	// Do not let the atomic writer's Windows replacement fallback remove a
	// directory (or traverse a symlink) that merely collides with the manifest
	// name. Such a shape is a packaging error and must fail closed.
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("manifest target is not a regular file: %s", PackageManifestFileName)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect manifest target: %w", statErr)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	// Keep the integrity evidence crash-safe. A direct WriteFile can leave a
	// truncated manifest after power loss, while AtomicWriteFile replaces the
	// target only after the complete JSON has been flushed.
	return fileutil.AtomicWriteFile(target, append(data, '\n'), 0o644)
}

func validatePackageManifestDir(skillDir string) error {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return fmt.Errorf("skill directory is empty")
	}
	info, err := os.Lstat(skillDir)
	if err != nil {
		return fmt.Errorf("inspect skill directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("skill path is not a regular directory")
	}
	return nil
}

// ReadPackageManifest reads the manifest from a skill directory.
// Returns nil, nil if the manifest file does not exist (legacy skill without manifest).
func ReadPackageManifest(skillDir string) (*PackageManifest, error) {
	if err := validatePackageManifestDir(skillDir); err != nil {
		return nil, err
	}
	target := filepath.Join(skillDir, PackageManifestFileName)
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("manifest target is not a regular file: %s", PackageManifestFileName)
		}
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("inspect manifest target: %w", statErr)
	}
	data, err := os.ReadFile(target)
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
	if manifest == nil {
		return nil // no manifest = nothing to verify (legacy skill)
	}
	if err := validatePackageManifestDir(skillDir); err != nil {
		return err
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("package integrity manifest contains no files")
	}

	var mismatches []string
	var missing []string
	listed := make(map[string]struct{}, len(manifest.Files))

	for relPath, expectedHash := range manifest.Files {
		canonical, err := canonicalManifestRelativePath(relPath)
		if err != nil {
			return fmt.Errorf("package integrity manifest has invalid path %q: %w", relPath, err)
		}
		if _, duplicate := listed[canonical]; duplicate {
			return fmt.Errorf("package integrity manifest has duplicate path %q", relPath)
		}
		listed[canonical] = struct{}{}
		if len(expectedHash) != sha256.Size*2 {
			return fmt.Errorf("package integrity manifest has invalid hash for %q", relPath)
		}
		if _, err := hex.DecodeString(expectedHash); err != nil {
			return fmt.Errorf("package integrity manifest has invalid hash for %q: %w", relPath, err)
		}
		absPath := filepath.Join(skillDir, filepath.FromSlash(canonical))
		info, statErr := os.Lstat(absPath)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("package integrity check refuses symlink: %s", canonical)
			}
			if info.IsDir() {
				return fmt.Errorf("package integrity check expected file but found directory: %s", canonical)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("package integrity check refuses non-regular file: %s", canonical)
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("package integrity check cannot inspect %s: %w", canonical, statErr)
		}
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

	// A manifest is a complete package inventory, not merely a set of hashes
	// for selected files. Detect unlisted regular files as well; otherwise an
	// attacker could append an executable payload without invalidating the
	// advertised integrity evidence. Runtime/cache artifacts remain excluded by
	// the same policy used by GeneratePackageManifest.
	if err := filepath.Walk(skillDir, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(skillDir, filePath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("package integrity check refuses symlink: %s", rel)
		}
		if info.IsDir() {
			if IsSkillRuntimePackageDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if IsSkillRuntimePackageFile(rel) || rel == PackageManifestFileName {
			return nil
		}
		if _, ok := listed[rel]; !ok {
			mismatches = append(mismatches, fmt.Sprintf("%s: unexpected file", rel))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk package for integrity check: %w", err)
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

// canonicalManifestRelativePath validates the portable slash-separated path
// stored in a manifest and returns its canonical representation. Manifest
// entries are untrusted input; accepting an absolute path or ".." component
// would let integrity verification read files outside the Skill directory.
func canonicalManifestRelativePath(rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	local := filepath.FromSlash(rel)
	if rel == "" || rel == "." || strings.HasPrefix(rel, "/") || path.IsAbs(rel) || filepath.IsAbs(local) || filepath.VolumeName(local) != "" {
		return "", fmt.Errorf("path must be relative")
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes Skill directory")
	}
	return clean, nil
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
