package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"gopkg.in/yaml.v3"
)

// CraftedSkillsSubdir is the subdirectory under the skills root where
// auto-persisted craft_tool outputs are stored.
const CraftedSkillsSubdir = "crafted"

// CraftToSkillResult holds the outcome of persisting a craft_tool output.
type CraftToSkillResult struct {
	SkillName string `json:"skill_name"`
	SkillDir  string `json:"skill_dir"`
	IsUpdate  bool   `json:"is_update"` // true if an existing crafted skill was updated
}

// PersistCraftedSkill saves a successfully executed craft_tool script as a
// reusable skill in the crafted skills directory. If a semantically similar
// crafted skill already exists (BM25 similarity > dedupeThreshold), the
// existing skill is updated instead of creating a new one.
//
// Inspired by Memento-Skills' CreateOnMiss: when the skill library has no
// matching skill, the system creates one from scratch and persists it so
// future similar tasks can reuse it directly.
//
// Parameters:
//   - skillsRoot: the root skills directory (e.g. ~/.maclaw/skills/)
//   - taskDescription: the original task description from craft_tool
//   - scriptContent: the generated script that was successfully executed
//   - scriptLang: "python", "bash", "node", etc.
func PersistCraftedSkill(skillsRoot, taskDescription, scriptContent, scriptLang string) (*CraftToSkillResult, error) {
	if skillsRoot == "" || taskDescription == "" || scriptContent == "" {
		return nil, fmt.Errorf("skillsRoot, taskDescription, and scriptContent are required")
	}

	craftedDir := filepath.Join(skillsRoot, CraftedSkillsSubdir)
	if err := os.MkdirAll(craftedDir, 0755); err != nil {
		return nil, fmt.Errorf("create crafted dir: %w", err)
	}

	// Check for existing similar skill.
	existingDir, isUpdate := findSimilarCraftedSkill(craftedDir, taskDescription)

	skillName := craftedSkillName(taskDescription)
	var targetDir string
	if isUpdate && existingDir != "" {
		targetDir = existingDir
		// Read existing skill name from YAML if available.
		if existing := readExistingSkillName(targetDir); existing != "" {
			skillName = existing
		}
	} else {
		targetDir = filepath.Join(craftedDir, sanitizeDirName(skillName))
		// Ensure unique directory.
		if _, err := os.Stat(targetDir); err == nil {
			targetDir = fmt.Sprintf("%s_%d", targetDir, time.Now().Unix())
		}
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}

	// Write the script file.
	ext := craftedScriptExtension(scriptLang)
	scriptPath := filepath.Join(targetDir, "main"+ext)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0644); err != nil {
		return nil, fmt.Errorf("write script: %w", err)
	}

	// Build the command to execute the script, then append extracted
	// parameter placeholders so persisted crafted skills remain reusable
	// after app restart.
	skillParams := ExtractScriptParams(scriptContent, scriptLang)
	portableScriptPath := "{baseDir}/main" + ext
	command := AppendRunParamPlaceholders(buildCraftedScriptCommand(scriptLang, portableScriptPath), skillParams)

	// Write skill.yaml.
	sf := SkillYAMLFile{
		Name:        skillName,
		Description: taskDescription,
		Params:      skillYAMLParamsFromCore(skillParams),
		RequiredEnv: ExtractScriptRequiredEnv(scriptContent, scriptLang),
		Requires:    ExtractScriptRequires(scriptContent, scriptLang),
		Steps: []SkillYAMLStep{
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command": command,
				},
			},
		},
	}
	yamlData, err := FormatSkillYAMLFile(&sf)
	if err != nil {
		return nil, fmt.Errorf("format skill yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "skill.yaml"), yamlData, 0644); err != nil {
		return nil, fmt.Errorf("write skill.yaml: %w", err)
	}

	log.Printf("[craft-to-skill] persisted crafted skill %q in %s (update=%v)", skillName, targetDir, isUpdate)

	return &CraftToSkillResult{
		SkillName: skillName,
		SkillDir:  targetDir,
		IsUpdate:  isUpdate,
	}, nil
}

// dedupeThreshold is the minimum BM25 similarity score for two crafted skills
// to be considered duplicates. Tuned conservatively — only very similar tasks
// should be merged.
const dedupeThreshold = 3.0

// findSimilarCraftedSkill scans existing crafted skill directories and returns
// the path of the most similar one (by BM25 on description), or empty string
// if no match exceeds the threshold.
func findSimilarCraftedSkill(craftedDir, taskDescription string) (string, bool) {
	entries, err := os.ReadDir(craftedDir)
	if err != nil {
		return "", false
	}

	idx := bm25.New()
	var docs []bm25.Doc
	dirMap := make(map[string]string) // doc ID → dir path

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(craftedDir, entry.Name())
		yamlPath := filepath.Join(skillDir, "skill.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		sf, err := ParseSkillYAMLFile(data)
		if err != nil {
			continue
		}
		docID := entry.Name()
		docs = append(docs, bm25.Doc{ID: docID, Text: sf.Description})
		dirMap[docID] = skillDir
	}

	if len(docs) == 0 {
		return "", false
	}

	idx.RebuildIfChanged(docs)
	scores := idx.Score(taskDescription)

	var bestID string
	var bestScore float64
	for id, score := range scores {
		if score > bestScore {
			bestScore = score
			bestID = id
		}
	}

	if bestScore >= dedupeThreshold && bestID != "" {
		return dirMap[bestID], true
	}
	return "", false
}

var (
	whitespaceRe    = regexp.MustCompile(`\s+`)
	nonAlphanumRe   = regexp.MustCompile(`[^\p{L}\p{N}\-]`)
	unsafeDirCharRe = regexp.MustCompile(`[<>:"/\\|?*]`)
)

// craftedSkillName creates a short skill name from the task description
// for use as a directory name and skill identifier in the crafted skills library.
// Distinct from gui/tool_craft.go's generateSkillName which uses a different
// naming convention (craft_ prefix) for the in-memory skill registry.
func craftedSkillName(desc string) string {
	runes := []rune(desc)
	if len(runes) > 40 {
		runes = runes[:40]
	}
	name := strings.TrimSpace(string(runes))
	name = whitespaceRe.ReplaceAllString(name, "-")
	name = nonAlphanumRe.ReplaceAllString(name, "")
	if name == "" {
		name = fmt.Sprintf("crafted-%d", time.Now().Unix())
	}
	return name
}

// sanitizeDirName makes a string safe for use as a directory name.
func sanitizeDirName(name string) string {
	name = unsafeDirCharRe.ReplaceAllString(name, "_")
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

// SanitizeSkillName makes a user-provided name safe for use as both a skill
// identifier and a directory name. It strips path separators, filesystem-unsafe
// characters, leading/trailing dots and spaces, and truncates to 80 runes.
func SanitizeSkillName(name string) string {
	name = strings.TrimSpace(name)
	// Strip path separators to prevent path traversal.
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	// Strip filesystem-unsafe characters.
	name = unsafeDirCharRe.ReplaceAllString(name, "_")
	// Collapse consecutive underscores.
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	// Strip leading/trailing dots and underscores (avoid hidden dirs / trailing dots on Windows).
	name = strings.Trim(name, "._")
	// Truncate to 80 runes (not bytes) to avoid splitting multibyte characters.
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	return name
}

// craftedScriptExtension returns the file extension for a script language.
// Distinct from gui/tool_craft.go's scriptExtension which handles additional
// languages (powershell) and uses normalizeCraftLanguage.
func craftedScriptExtension(lang string) string {
	switch strings.ToLower(lang) {
	case "python", "py":
		return ".py"
	case "node", "javascript", "js":
		return ".js"
	case "bash", "sh":
		return ".sh"
	case "powershell", "pwsh", "ps1":
		return ".ps1"
	default:
		return ".py" // default to Python
	}
}

// buildCraftedScriptCommand returns the command to execute a crafted script file.
func buildCraftedScriptCommand(lang, scriptPath string) string {
	switch strings.ToLower(lang) {
	case "python", "py":
		return fmt.Sprintf("python %q", scriptPath)
	case "node", "javascript", "js":
		return fmt.Sprintf("node %q", scriptPath)
	case "bash", "sh":
		return fmt.Sprintf("bash %q", scriptPath)
	case "powershell", "pwsh", "ps1":
		return fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File %q", scriptPath)
	default:
		return fmt.Sprintf("python %q", scriptPath)
	}
}

func skillYAMLParamsFromCore(params []corelib.NLSkillParam) []SkillYAMLParam {
	if len(params) == 0 {
		return nil
	}
	result := make([]SkillYAMLParam, 0, len(params))
	for _, param := range params {
		result = append(result, SkillYAMLParam{
			Name:        param.Name,
			Description: param.Description,
			Type:        param.Type,
			Aliases:     append([]string(nil), param.Aliases...),
			CLIFlag:     param.CLIFlag,
			Default:     param.Default,
			Required:    param.Required,
		})
	}
	return result
}

// readExistingSkillName reads the skill name from an existing skill.yaml.
func readExistingSkillName(skillDir string) string {
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		return ""
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return ""
	}
	if name, ok := m["name"].(string); ok {
		return name
	}
	return ""
}
