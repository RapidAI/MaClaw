package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// EnsureSkillIDBeforeUpload guarantees that a skill has a valid SkillID
// before uploading to Hub/SkillMarket. If the skill already has an id, it
// is validated. If not, one is auto-generated from the uploader's email
// and the skill name, then written back to skill.yaml on disk.
//
// Returns the (possibly newly-assigned) skill ID, or an error if generation
// or persistence fails.
func EnsureSkillIDBeforeUpload(entry *corelib.NLSkillEntry, uploaderEmail string) (string, error) {
	if entry == nil {
		return "", fmt.Errorf("nil skill entry")
	}

	// Already has a valid ID — just validate format.
	if sid := strings.TrimSpace(entry.SkillID); sid != "" {
		if !IsValidSkillID(sid) {
			return "", fmt.Errorf("skill id %q 格式无效（要求: publisher.skill-name，仅小写字母、数字、连字符）", sid)
		}
		return sid, nil
	}

	// No ID declared — auto-generate from email + name.
	uploaderEmail = strings.TrimSpace(uploaderEmail)
	if uploaderEmail == "" {
		return "", fmt.Errorf("无法生成 skill id：缺少上传者邮箱（请先登录/注册）")
	}

	skillID := DeriveSkillID(uploaderEmail, entry.Name)
	if skillID == "" {
		return "", fmt.Errorf("无法从邮箱 %q 和名称 %q 生成有效的 skill id", uploaderEmail, entry.Name)
	}

	// Validate the generated ID
	if !IsValidSkillID(skillID) {
		return "", fmt.Errorf("auto-generated skill id %q is invalid (this is a bug)", skillID)
	}

	// Assign to entry, but retain the pre-image so a persistence failure does
	// not leave callers with an identity that was never written to disk.
	originalSkillID, originalPublisher := entry.SkillID, entry.Publisher
	entry.SkillID = skillID
	if dot := strings.IndexByte(skillID, '.'); dot > 0 {
		entry.Publisher = skillID[:dot]
	}

	// Persist to skill.yaml on disk (if directory-backed skill)
	if entry.SkillDir != "" {
		if err := insertSkillIDIntoYAML(entry.SkillDir, skillID); err != nil {
			entry.SkillID = originalSkillID
			entry.Publisher = originalPublisher
			// The generated identity is part of the durable upload contract. Do
			// not continue with an in-memory-only id: a retry or another process
			// would observe a different identity and could publish a second skill.
			return "", fmt.Errorf("persist skill id %q: %w", skillID, err)
		}
	}

	log.Printf("[skill-id] auto-generated id %q for skill %q (email: %s)", skillID, entry.Name, maskEmail(uploaderEmail))
	return skillID, nil
}

// insertSkillIDIntoYAML adds the id field at the top of skill.yaml.
// If skill.yaml already contains a top-level "id:" line, it is updated in place.
func insertSkillIDIntoYAML(skillDir, skillID string) error {
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		// Try skill.yml
		yamlPath = filepath.Join(skillDir, "skill.yml")
		data, err = os.ReadFile(yamlPath)
		if err != nil {
			return fmt.Errorf("read skill definition: %w", err)
		}
	}

	content := string(data)
	idLine := "id: " + skillID + "\n"

	// Check if top-level id field already exists (no leading whitespace, not a comment)
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t' || line[0] == '#') {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "id:") {
			lines[i] = "id: " + skillID
			found = true
			break
		}
	}

	var newContent string
	if found {
		newContent = strings.Join(lines, "\n")
	} else {
		// Insert id after document start marker (---) if present, otherwise as first line.
		if strings.HasPrefix(strings.TrimSpace(content), "---") {
			// Find the end of the --- line and insert after it
			idx := strings.Index(content, "\n")
			if idx >= 0 {
				newContent = content[:idx+1] + idLine + content[idx+1:]
			} else {
				newContent = content + "\n" + idLine
			}
		} else {
			newContent = idLine + content
		}
	}

	return fileutil.AtomicWriteFile(yamlPath, []byte(newContent), 0o644)
}

// maskEmail masks an email for logging: "zhangsan@gmail.com" → "zha***@gmail.com"
func maskEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 3 {
		return email // too short to mask
	}
	return email[:3] + "***" + email[at:]
}
