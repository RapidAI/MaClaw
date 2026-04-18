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

// BackupCurrent backs up the current skill.yaml to skill.yaml.v{N}.
// Returns the version number assigned, or error if skill.yaml doesn't exist.
func (v *Versioner) BackupCurrent(skillDir string) (int, error) {
	src := filepath.Join(skillDir, "skill.yaml")
	if _, err := os.Stat(src); err != nil {
		return 0, fmt.Errorf("skill.yaml not found in %s: %w", skillDir, err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return 0, fmt.Errorf("read skill.yaml: %w", err)
	}

	nextVer := v.LatestVersion(skillDir) + 1
	dst := filepath.Join(skillDir, fmt.Sprintf("skill.yaml.v%d", nextVer))
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
	// versions is sorted ascending; remove the oldest.
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
		if !strings.HasPrefix(name, "skill.yaml.v") {
			continue
		}
		numStr := strings.TrimPrefix(name, "skill.yaml.v")
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
