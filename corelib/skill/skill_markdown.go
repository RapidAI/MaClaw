package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type MarkdownSkillOptions struct {
	NameFallback        string
	DescriptionFallback string
	Source              string
	SourceProject       string
	TrustLevel          string
	SkillDir            string
	Triggers            []string
}

type parsedSkillMarkdown struct {
	markdown      string
	body          string
	name          string
	description   string
	compatibility string
}

func ParseMarkdownSkill(content string, opts MarkdownSkillOptions) (*corelib.NLSkillEntry, error) {
	parsed, err := parseSkillMarkdownDocument(content, opts.NameFallback, opts.DescriptionFallback)
	if err != nil {
		return nil, err
	}
	triggers := append([]string(nil), opts.Triggers...)
	if len(triggers) == 0 && strings.TrimSpace(parsed.name) != "" {
		triggers = []string{parsed.name}
	}
	if parsed.compatibility != "" && !containsString(triggers, "agent-skill") {
		triggers = append(triggers, "agent-skill")
	}
	params := map[string]interface{}{
		"instructions": parsed.markdown,
	}
	if skillDir := strings.TrimSpace(opts.SkillDir); skillDir != "" {
		params["working_dir"] = skillDir
	}
	sourceProject := strings.TrimSpace(opts.SourceProject)
	if sourceProject == "" {
		sourceProject = parsed.compatibility
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = "file"
	}
	return &corelib.NLSkillEntry{
		Name:          parsed.name,
		Description:   parsed.description,
		Triggers:      triggers,
		Steps:         []corelib.NLSkillStep{{Action: "craft_tool", Params: params}},
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        source,
		SourceProject: sourceProject,
		TrustLevel:    strings.TrimSpace(opts.TrustLevel),
		SkillDir:      strings.TrimSpace(opts.SkillDir),
	}, nil
}

func ImportMarkdownSkillDir(skillDir string, opts MarkdownSkillOptions) (*corelib.NLSkillEntry, error) {
	mdPath, err := skillMarkdownPath(skillDir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, fmt.Errorf("无法读取 %s: %v", filepath.Base(mdPath), err)
	}
	parsed, err := parseSkillMarkdownDocument(string(data), opts.NameFallback, opts.DescriptionFallback)
	if err != nil {
		return nil, err
	}
	triggers := append([]string(nil), opts.Triggers...)
	if len(triggers) == 0 && strings.TrimSpace(parsed.name) != "" {
		triggers = []string{parsed.name}
	}
	if parsed.compatibility != "" && !containsString(triggers, "agent-skill") {
		triggers = append(triggers, "agent-skill")
	}
	steps := make([]corelib.NLSkillStep, 0)
	scriptsDir := filepath.Join(skillDir, "scripts")
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			scriptPath := filepath.Join(scriptsDir, e.Name())
			scriptData, err := os.ReadFile(scriptPath)
			if err != nil {
				continue
			}
			params := map[string]interface{}{"command": string(scriptData)}
			if strings.TrimSpace(skillDir) != "" {
				params["working_dir"] = skillDir
			}
			steps = append(steps, corelib.NLSkillStep{Action: "bash", Params: params, OnError: "stop"})
		}
	}
	if len(steps) == 0 {
		entry, err := ParseMarkdownSkill(parsed.markdown, MarkdownSkillOptions{
			NameFallback:        parsed.name,
			DescriptionFallback: parsed.description,
			Source:              opts.Source,
			SourceProject:       firstNonEmpty(strings.TrimSpace(opts.SourceProject), parsed.compatibility),
			TrustLevel:          opts.TrustLevel,
			SkillDir:            firstNonEmpty(strings.TrimSpace(opts.SkillDir), skillDir),
			Triggers:            triggers,
		})
		if err != nil {
			return nil, err
		}
		entry.CreatedAt = fileModTime(mdPath)
		return entry, nil
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = "file"
	}
	entry := &corelib.NLSkillEntry{
		Name:          parsed.name,
		Description:   parsed.description,
		Triggers:      triggers,
		Steps:         steps,
		Status:        "active",
		CreatedAt:     fileModTime(mdPath),
		Source:        source,
		SourceProject: firstNonEmpty(strings.TrimSpace(opts.SourceProject), parsed.compatibility),
		TrustLevel:    strings.TrimSpace(opts.TrustLevel),
		SkillDir:      firstNonEmpty(strings.TrimSpace(opts.SkillDir), skillDir),
	}
	return entry, nil
}

func skillMarkdownPath(skillDir string) (string, error) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		path := filepath.Join(skillDir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("无法读取 SKILL.md: file not found")
}

func parseSkillMarkdownDocument(content, nameFallback, descriptionFallback string) (*parsedSkillMarkdown, error) {
	skillMD := strings.TrimSpace(content)
	if skillMD == "" {
		return nil, fmt.Errorf("empty SKILL.md document")
	}
	frontmatter, body := ParseMarkdownFrontmatter(skillMD)
	name := strings.TrimSpace(frontmatter["name"])
	if name == "" {
		name = firstMarkdownHeading(body)
	}
	if name == "" {
		name = strings.TrimSpace(nameFallback)
	}
	if name == "" {
		name = "unnamed-skill"
	}
	description := strings.TrimSpace(frontmatter["description"])
	if description == "" {
		description = strings.TrimSpace(descriptionFallback)
	}
	if description == "" {
		description = firstMarkdownParagraph(body)
	}
	return &parsedSkillMarkdown{
		markdown:      skillMD,
		body:          body,
		name:          name,
		description:   description,
		compatibility: strings.TrimSpace(frontmatter["compatibility"]),
	}, nil
}

func ParseMarkdownFrontmatter(content string) (map[string]string, string) {
	fm := make(map[string]string)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return fm, content
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return fm, content
	}
	fmBlock := rest[:idx]
	body := strings.TrimSpace(rest[idx+4:])
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.Trim(strings.TrimSpace(line[colonIdx+1:]), `"'`)
		fm[key] = val
	}
	return fm, body
}

func firstMarkdownHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if heading != "" {
			return heading
		}
	}
	return ""
}

func firstMarkdownParagraph(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var triggerCleanupRe = regexp.MustCompile(`[^a-z0-9_-]+`)

func normalizeTrigger(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, " ", "-")
	value = triggerCleanupRe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	return value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
