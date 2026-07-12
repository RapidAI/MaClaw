package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// Versioner manages skill version history within a skill directory.
type Versioner struct{}

// BackupCurrent backs up the current structured skill definition to <name>.v{N}.
// Returns the version number assigned, or error if no structured definition exists.
func (v *Versioner) BackupCurrent(skillDir string) (int, error) {
	src, baseName, err := currentDefinitionFile(skillDir)
	if err != nil {
		return 0, err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", baseName, err)
	}

	nextVer := v.LatestVersion(skillDir) + 1
	dst := filepath.Join(skillDir, fmt.Sprintf("%s.v%d", baseName, nextVer))
	if err := fileutil.AtomicWriteFile(dst, data, 0o644); err != nil {
		return 0, fmt.Errorf("write backup %s: %w", dst, err)
	}

	return nextVer, nil
}

// CleanOldVersions keeps only the latest maxVersions backup files.
// Deletes oldest versions first.
func (v *Versioner) CleanOldVersions(skillDir string, maxVersions int) error {
	versions := v.listVersionFiles(skillDir)
	if len(versions) <= maxVersions {
		return nil
	}
	toRemove := versions[:len(versions)-maxVersions]
	for _, vf := range toRemove {
		path := filepath.Join(skillDir, vf.filename)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove old version %s: %w", path, err)
		}
	}
	return nil
}

// LatestVersion returns the highest version number found in skillDir.
// Returns 0 if no version backups exist.
func (v *Versioner) LatestVersion(skillDir string) int {
	versions := v.listVersionFiles(skillDir)
	if len(versions) == 0 {
		return 0
	}
	return versions[len(versions)-1].version
}

// ListVersions returns all backup version numbers ascending (oldest first).
func (v *Versioner) ListVersions(skillDir string) []int {
	files := v.listVersionFiles(skillDir)
	out := make([]int, 0, len(files))
	for _, f := range files {
		out = append(out, f.version)
	}
	return out
}

// BackupPath returns the path of skill.yaml.v{N} or skill.yml.v{N} if present.
func (v *Versioner) BackupPath(skillDir string, version int) (string, error) {
	if version <= 0 {
		return "", fmt.Errorf("invalid version %d", version)
	}
	for _, base := range []string{"skill.yaml", "skill.yml"} {
		p := filepath.Join(skillDir, fmt.Sprintf("%s.v%d", base, version))
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("backup v%d not found in %s", version, skillDir)
}

// RestoreVersion restores skill.yaml from backup v{N}.
// When backupCurrentFirst is true, the current definition is backed up before overwrite
// so the restore itself is reversible.
// Returns the path written and the pre-restore backup version (0 if skipped).
func (v *Versioner) RestoreVersion(skillDir string, version int, backupCurrentFirst bool) (writtenPath string, preBackupVer int, err error) {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return "", 0, fmt.Errorf("skillDir is empty")
	}
	src, err := v.BackupPath(skillDir, version)
	if err != nil {
		return "", 0, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", 0, fmt.Errorf("read backup: %w", err)
	}

	// Prefer restoring onto the existing definition basename; else use backup basename.
	dst, baseName, derr := currentDefinitionFile(skillDir)
	if derr != nil {
		// No current file — recreate skill.yaml from backup name.
		baseName = "skill.yaml"
		if strings.Contains(filepath.Base(src), "skill.yml.v") {
			baseName = "skill.yml"
		}
		dst = filepath.Join(skillDir, baseName)
	} else if backupCurrentFirst {
		if ver, berr := v.BackupCurrent(skillDir); berr == nil {
			preBackupVer = ver
		}
		// Refresh dst after potential no-op.
		if p, _, e := currentDefinitionFile(skillDir); e == nil {
			dst = p
		}
	}

	if err := fileutil.AtomicWriteFile(dst, data, 0o644); err != nil {
		return "", preBackupVer, fmt.Errorf("write restored definition: %w", err)
	}
	_ = v.CleanOldVersions(skillDir, 10)
	return dst, preBackupVer, nil
}

// RestoreLatest restores from the highest backup version.
func (v *Versioner) RestoreLatest(skillDir string, backupCurrentFirst bool) (writtenPath string, restoredVer, preBackupVer int, err error) {
	restoredVer = v.LatestVersion(skillDir)
	if restoredVer <= 0 {
		return "", 0, 0, fmt.Errorf("no backups available in %s", skillDir)
	}
	writtenPath, preBackupVer, err = v.RestoreVersion(skillDir, restoredVer, backupCurrentFirst)
	return writtenPath, restoredVer, preBackupVer, err
}

type versionFile struct {
	filename string
	version  int
}

func (v *Versioner) listVersionFiles(skillDir string) []versionFile {
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return nil
	}
	var versions []versionFile
	for _, e := range entries {
		name := e.Name()
		numStr := ""
		for _, prefix := range []string{"skill.yaml.v", "skill.yml.v"} {
			if strings.HasPrefix(name, prefix) {
				numStr = strings.TrimPrefix(name, prefix)
				break
			}
		}
		if numStr == "" {
			continue
		}
		num, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		versions = append(versions, versionFile{filename: name, version: num})
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].version < versions[j].version
	})
	return versions
}

func currentDefinitionFile(skillDir string) (string, string, error) {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		path := filepath.Join(skillDir, name)
		if _, err := os.Stat(path); err == nil {
			return path, name, nil
		}
	}
	return "", "", fmt.Errorf("skill definition not found in %s", skillDir)
}
