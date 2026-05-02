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
