package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// ---------------------------------------------------------------------------
// AutoFixPortability — main entry point
// ---------------------------------------------------------------------------

// AutoFixPortability applies safe, reversible fixes to a skill directory.
// It reads skill.yaml (and optionally SKILL.md), detects fixable issues
// internally (does not require a pre-computed report), applies fixes for
// hardcoded paths, missing metadata, and path separators, then writes
// the modified files back. Creates .bak backups before modifying.
// Returns the list of changes made.
func AutoFixPortability(skillDir string) ([]PortabilityChange, error) {
	// Check directory exists.
	info, err := os.Stat(skillDir)
	if err != nil {
		return nil, fmt.Errorf("skill directory does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", skillDir)
	}

	var changes []PortabilityChange

	// --- Fix skill.yaml ---
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	yamlData, err := os.ReadFile(yamlPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot read skill.yaml: %w", err)
	}

	if err == nil {
		sf, parseErr := ParseSkillYAMLFile(yamlData)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse skill.yaml: %w", parseErr)
		}

		yamlChanged := false

		// Fix missing_basedir: replace absolute paths inside skill dir with {baseDir}/relative.
		if fixBaseDirChanges := fixMissingBaseDir(sf, skillDir); len(fixBaseDirChanges) > 0 {
			changes = append(changes, fixBaseDirChanges...)
			yamlChanged = true
		}

		// Fix hardcoded_path (home dir): replace home dir paths with $HOME.
		if fixHomeChanges := fixHardcodedHomePaths(sf); len(fixHomeChanges) > 0 {
			changes = append(changes, fixHomeChanges...)
			yamlChanged = true
		}

		// Fix missing_platforms: set platforms to ["universal"] when empty.
		if fixPlatformChanges := fixMissingPlatforms(sf); len(fixPlatformChanges) > 0 {
			changes = append(changes, fixPlatformChanges...)
			yamlChanged = true
		}

		// Fix path_separator: replace backslashes with forward slashes.
		if fixSepChanges := fixPathSeparators(sf); len(fixSepChanges) > 0 {
			changes = append(changes, fixSepChanges...)
			yamlChanged = true
		}

		// Write back if any changes were made.
		if yamlChanged {
			// Create backup first.
			bakPath := yamlPath + ".bak"
			if writeErr := fileutil.AtomicWriteFile(bakPath, yamlData, 0644); writeErr != nil {
				return nil, fmt.Errorf("cannot create backup %s: %w", bakPath, writeErr)
			}

			// Format and write modified skill.yaml.
			newData, fmtErr := FormatSkillYAMLFile(sf)
			if fmtErr != nil {
				return nil, fmt.Errorf("cannot format skill.yaml: %w", fmtErr)
			}
			if writeErr := fileutil.AtomicWriteFile(yamlPath, newData, 0644); writeErr != nil {
				return nil, fmt.Errorf("cannot write skill.yaml: %w", writeErr)
			}
		}
	}

	// --- Fix SKILL.md ---
	mdPath, mdErr := findSkillMD(skillDir)
	if mdErr == nil {
		mdChanges, mdFixErr := fixSkillMD(mdPath, skillDir)
		if mdFixErr != nil {
			return changes, fmt.Errorf("cannot fix SKILL.md: %w", mdFixErr)
		}
		changes = append(changes, mdChanges...)
	}

	return changes, nil
}

// ---------------------------------------------------------------------------
// fixMissingBaseDir — replace absolute paths inside skill dir with {baseDir}
// ---------------------------------------------------------------------------

func fixMissingBaseDir(sf *SkillYAMLFile, skillDir string) []PortabilityChange {
	var changes []PortabilityChange
	normalizedDir := filepath.ToSlash(filepath.Clean(skillDir))
	normalizedDir = strings.TrimRight(normalizedDir, "/")

	for i := range sf.Steps {
		if sf.Steps[i].Action != "bash" {
			continue
		}
		cmd, ok := sf.Steps[i].Params["command"].(string)
		if !ok || cmd == "" {
			continue
		}

		newCmd := cmd
		paths := extractAbsolutePaths(cmd)
		for _, p := range paths {
			normalizedPath := filepath.ToSlash(p)
			if strings.HasPrefix(normalizedPath, normalizedDir+"/") {
				relPath := strings.TrimPrefix(normalizedPath, normalizedDir+"/")
				replacement := "{baseDir}/" + relPath
				newCmd = strings.ReplaceAll(newCmd, p, replacement)
				changes = append(changes, PortabilityChange{
					File:        "skill.yaml",
					Field:       fmt.Sprintf("steps[%d].params.command", i),
					Original:    p,
					Replacement: replacement,
				})
			} else if normalizedPath == normalizedDir {
				replacement := "{baseDir}"
				newCmd = strings.ReplaceAll(newCmd, p, replacement)
				changes = append(changes, PortabilityChange{
					File:        "skill.yaml",
					Field:       fmt.Sprintf("steps[%d].params.command", i),
					Original:    p,
					Replacement: replacement,
				})
			}
		}
		if newCmd != cmd {
			sf.Steps[i].Params["command"] = newCmd
		}
	}
	return changes
}

// ---------------------------------------------------------------------------
// fixHardcodedHomePaths — replace home dir paths with $HOME
// ---------------------------------------------------------------------------

func fixHardcodedHomePaths(sf *SkillYAMLFile) []PortabilityChange {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var changes []PortabilityChange
	// Normalize home dir to forward slashes for comparison.
	homeSlash := filepath.ToSlash(homeDir)
	homeSlash = strings.TrimRight(homeSlash, "/")

	// Also keep the native form for Windows matching.
	homeNative := filepath.Clean(homeDir)

	for i := range sf.Steps {
		if sf.Steps[i].Action != "bash" {
			continue
		}
		cmd, ok := sf.Steps[i].Params["command"].(string)
		if !ok || cmd == "" {
			continue
		}

		newCmd := cmd
		paths := extractAbsolutePaths(cmd)
		for _, p := range paths {
			normalizedPath := filepath.ToSlash(p)
			// Check if this path starts with the user's home directory.
			if strings.HasPrefix(normalizedPath, homeSlash+"/") || normalizedPath == homeSlash {
				// Skip paths already replaced by fixMissingBaseDir (no longer in the command).
				if !strings.Contains(newCmd, p) {
					continue
				}
				// Skip system binary paths.
				if isSystemBinaryPath(p) {
					continue
				}
				rest := strings.TrimPrefix(normalizedPath, homeSlash)
				replacement := "$HOME" + rest
				newCmd = strings.ReplaceAll(newCmd, p, replacement)
				changes = append(changes, PortabilityChange{
					File:        "skill.yaml",
					Field:       fmt.Sprintf("steps[%d].params.command", i),
					Original:    p,
					Replacement: replacement,
				})
			} else if strings.Contains(p, `\`) {
				// Check Windows-style home path.
				nativePath := filepath.FromSlash(p)
				homeNativeSlash := strings.ReplaceAll(homeNative, `/`, `\`)
				if strings.HasPrefix(nativePath, homeNativeSlash+`\`) || nativePath == homeNativeSlash {
					rest := filepath.ToSlash(strings.TrimPrefix(nativePath, homeNativeSlash))
					replacement := "$HOME" + rest
					newCmd = strings.ReplaceAll(newCmd, p, replacement)
					changes = append(changes, PortabilityChange{
						File:        "skill.yaml",
						Field:       fmt.Sprintf("steps[%d].params.command", i),
						Original:    p,
						Replacement: replacement,
					})
				}
			}
		}
		if newCmd != cmd {
			sf.Steps[i].Params["command"] = newCmd
		}
	}
	return changes
}

// ---------------------------------------------------------------------------
// fixMissingPlatforms — set platforms to ["universal"] when empty
// ---------------------------------------------------------------------------

func fixMissingPlatforms(sf *SkillYAMLFile) []PortabilityChange {
	if len(sf.Platforms) > 0 {
		return nil
	}
	sf.Platforms = []string{"universal"}
	return []PortabilityChange{
		{
			File:        "skill.yaml",
			Field:       "platforms",
			Original:    "[]",
			Replacement: `["universal"]`,
		},
	}
}

// ---------------------------------------------------------------------------
// fixPathSeparators — replace backslashes with forward slashes in commands
// ---------------------------------------------------------------------------

func fixPathSeparators(sf *SkillYAMLFile) []PortabilityChange {
	var changes []PortabilityChange

	for i := range sf.Steps {
		if sf.Steps[i].Action != "bash" {
			continue
		}
		cmd, ok := sf.Steps[i].Params["command"].(string)
		if !ok || cmd == "" {
			continue
		}

		if !containsBackslashPath(cmd) {
			continue
		}

		newCmd := replaceBackslashPaths(cmd)
		if newCmd != cmd {
			sf.Steps[i].Params["command"] = newCmd
			changes = append(changes, PortabilityChange{
				File:        "skill.yaml",
				Field:       fmt.Sprintf("steps[%d].params.command", i),
				Original:    cmd,
				Replacement: newCmd,
			})
		}
	}
	return changes
}

// replaceBackslashPaths replaces backslash path separators with forward
// slashes, while preserving shell escape sequences.
func replaceBackslashPaths(command string) string {
	var result strings.Builder
	lines := strings.Split(command, "\n")
	for li, line := range lines {
		if li > 0 {
			result.WriteByte('\n')
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			result.WriteString(line)
			continue
		}

		// Process character by character, replacing backslashes that are
		// part of path patterns but not shell escape sequences.
		newLine := replaceBackslashesInLine(line)
		result.WriteString(newLine)
	}
	return result.String()
}

// replaceBackslashesInLine replaces backslash path separators in a single
// line while preserving shell escape sequences.
func replaceBackslashesInLine(line string) string {
	// Find all backslash path matches using the same regex as the validator.
	locs := backslashPathRe.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return line
	}

	// Build a set of backslash positions that are path separators (not escapes).
	replacePositions := make(map[int]bool)
	for _, loc := range locs {
		// Find the actual backslash position within the match.
		for i := loc[0]; i < loc[1]; i++ {
			if line[i] == '\\' {
				// Check if this is a shell escape sequence.
				if i+1 < len(line) {
					twoChar := line[i : i+2]
					isEscape := false
					for _, esc := range shellEscapes {
						if twoChar == esc {
							isEscape = true
							break
						}
					}
					if !isEscape {
						replacePositions[i] = true
					}
				}
				break
			}
		}
	}

	if len(replacePositions) == 0 {
		return line
	}

	// Replace backslashes at identified positions.
	var buf strings.Builder
	buf.Grow(len(line))
	for i := 0; i < len(line); i++ {
		if replacePositions[i] {
			buf.WriteByte('/')
		} else {
			buf.WriteByte(line[i])
		}
	}
	return buf.String()
}

// ---------------------------------------------------------------------------
// fixSkillMD — fix absolute paths in SKILL.md bash blocks
// ---------------------------------------------------------------------------

func fixSkillMD(mdPath, skillDir string) ([]PortabilityChange, error) {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read SKILL.md: %w", err)
	}

	content := string(data)
	normalizedDir := filepath.ToSlash(filepath.Clean(skillDir))
	normalizedDir = strings.TrimRight(normalizedDir, "/")

	homeDir, _ := os.UserHomeDir()
	homeSlash := ""
	if homeDir != "" {
		homeSlash = filepath.ToSlash(homeDir)
		homeSlash = strings.TrimRight(homeSlash, "/")
	}

	mdFileName := filepath.Base(mdPath)
	var changes []PortabilityChange
	newContent := content

	// Find bash blocks and replace absolute paths within them.
	// We use the same regex as extractAllBashBlocksFromMarkdown but operate
	// on the raw content to preserve formatting.
	matches := bashBlockRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	// Process matches in reverse order so index positions remain valid.
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		// m[2],m[3] = .norun capture group; m[4],m[5] = block content
		if len(m) < 6 {
			continue
		}
		// Skip .norun blocks.
		if m[2] != -1 && m[3] != -1 {
			continue
		}

		blockStart := m[4]
		blockEnd := m[5]
		block := content[blockStart:blockEnd]

		newBlock := block
		paths := extractAbsolutePaths(block)
		for _, p := range paths {
			if isSystemBinaryPath(p) {
				continue
			}
			normalizedPath := filepath.ToSlash(p)

			// Check if path points inside skill dir.
			if strings.HasPrefix(normalizedPath, normalizedDir+"/") {
				relPath := strings.TrimPrefix(normalizedPath, normalizedDir+"/")
				replacement := "{baseDir}/" + relPath
				newBlock = strings.ReplaceAll(newBlock, p, replacement)
				changes = append(changes, PortabilityChange{
					File:        mdFileName,
					Field:       "bash block",
					Original:    p,
					Replacement: replacement,
				})
			} else if normalizedPath == normalizedDir {
				replacement := "{baseDir}"
				newBlock = strings.ReplaceAll(newBlock, p, replacement)
				changes = append(changes, PortabilityChange{
					File:        mdFileName,
					Field:       "bash block",
					Original:    p,
					Replacement: replacement,
				})
			} else if homeSlash != "" && (strings.HasPrefix(normalizedPath, homeSlash+"/") || normalizedPath == homeSlash) {
				// Home dir path.
				rest := strings.TrimPrefix(normalizedPath, homeSlash)
				replacement := "$HOME" + rest
				newBlock = strings.ReplaceAll(newBlock, p, replacement)
				changes = append(changes, PortabilityChange{
					File:        mdFileName,
					Field:       "bash block",
					Original:    p,
					Replacement: replacement,
				})
			}
		}

		if newBlock != block {
			newContent = newContent[:blockStart] + newBlock + newContent[blockEnd:]
		}
	}

	if len(changes) == 0 {
		return nil, nil
	}

	// Create backup before modifying.
	bakPath := mdPath + ".bak"
	if writeErr := fileutil.AtomicWriteFile(bakPath, data, 0644); writeErr != nil {
		return nil, fmt.Errorf("cannot create backup %s: %w", bakPath, writeErr)
	}

	// Write modified SKILL.md.
	if writeErr := fileutil.AtomicWriteFile(mdPath, []byte(newContent), 0644); writeErr != nil {
		return nil, fmt.Errorf("cannot write %s: %w", mdPath, writeErr)
	}

	return changes, nil
}
