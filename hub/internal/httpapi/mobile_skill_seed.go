package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"gopkg.in/yaml.v3"
)

// Seed Hub marketplace-style skill JSON (and real skill packages) into the
// mobile-agent user's skills directory so ListSkills/manage_skill see them.
// Idempotent per user via a marker file.

const mobileSkillSeedMarker = ".hub_seed_done"

var mobileSkillSeedMu sync.Mutex

type hubMarketSkillJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Steps       []struct {
		Action  string                 `json:"action"`
		Params  map[string]interface{} `json:"params"`
		OnError string                 `json:"on_error"`
	} `json:"steps"`
}

func mobileSeedUserSkills(svc *agentservice.Service, p agentservice.Principal) {
	if svc == nil {
		return
	}
	mobileSkillSeedMu.Lock()
	defer mobileSkillSeedMu.Unlock()

	root := svc.UserSkillsRoot(p.TenantID, p.UserID)
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Printf("[mobile-core-agent] skill seed mkdir: %v", err)
		return
	}
	marker := filepath.Join(root, mobileSkillSeedMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}

	seeded := 0
	for _, dir := range mobileHubSkillSeedDirs() {
		n, err := mobileSeedSkillsFromDir(dir, root)
		if err != nil {
			log.Printf("[mobile-core-agent] skill seed from %s: %v", dir, err)
			continue
		}
		seeded += n
	}
	// Marker even if zero so we do not re-scan every request.
	_ = os.WriteFile(marker, []byte(fmt.Sprintf("seeded=%d\n", seeded)), 0o644)
	if seeded > 0 {
		log.Printf("[mobile-core-agent] seeded %d skills for tenant=%s user=%s", seeded, p.TenantID, p.UserID)
	}
}

func mobileHubSkillSeedDirs() []string {
	var out []string
	if env := strings.TrimSpace(os.Getenv("MACLAW_HUB_SKILLS_SEED")); env != "" {
		out = append(out, env)
	}
	// Sibling of mobile-agent under Hub runtime data: {data}/skills
	parent := filepath.Dir(mobileCoreAgentDataRoot())
	out = append(out, filepath.Join(parent, "skills"))
	// Common hub layout when cwd is hub process root.
	out = append(out, filepath.Join("data", "skills"))
	out = append(out, filepath.Join("hub", "data", "skills"))
	return out
}

func mobileSeedSkillsFromDir(srcRoot, dstRoot string) (int, error) {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, ent := range entries {
		name := ent.Name()
		src := filepath.Join(srcRoot, name)
		if ent.IsDir() {
			if !mobileDirLooksLikeSkillPackage(src) {
				continue
			}
			dst := filepath.Join(dstRoot, name)
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			if err := mobileCopyDir(src, dst); err != nil {
				log.Printf("[mobile-core-agent] copy skill package %s: %v", name, err)
				continue
			}
			count++
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		n, err := mobileSeedMarketJSONSkill(src, dstRoot)
		if err != nil {
			log.Printf("[mobile-core-agent] seed json skill %s: %v", name, err)
			continue
		}
		count += n
	}
	return count, nil
}

func mobileDirLooksLikeSkillPackage(dir string) bool {
	for _, f := range []string{"skill.yaml", "skill.yml", "SKILL.md", "skill.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

func mobileSeedMarketJSONSkill(path, dstRoot string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var m hubMarketSkillJSON
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, err
	}
	id := strings.TrimSpace(m.ID)
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	id = sanitizePathSegment(id)
	dst := filepath.Join(dstRoot, id)
	if _, err := os.Stat(dst); err == nil {
		return 0, nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = id
	}
	type yamlStep struct {
		Action  string                 `yaml:"action"`
		Params  map[string]interface{} `yaml:"params,omitempty"`
		OnError string                 `yaml:"on_error,omitempty"`
	}
	type yamlSkill struct {
		Name        string     `yaml:"name"`
		Description string     `yaml:"description,omitempty"`
		Version     string     `yaml:"version,omitempty"`
		Author      string     `yaml:"author,omitempty"`
		Status      string     `yaml:"status"`
		Steps       []yamlStep `yaml:"steps,omitempty"`
	}
	doc := yamlSkill{
		Name:        name,
		Description: strings.TrimSpace(m.Description),
		Version:     strings.TrimSpace(m.Version),
		Author:      strings.TrimSpace(m.Author),
		Status:      "active",
	}
	for _, s := range m.Steps {
		doc.Steps = append(doc.Steps, yamlStep{
			Action:  strings.TrimSpace(s.Action),
			Params:  s.Params,
			OnError: strings.TrimSpace(s.OnError),
		})
	}
	if len(doc.Steps) == 0 {
		md := fmt.Sprintf("---\nname: %q\ndescription: %q\n---\n\n# %s\n\n%s\n",
			name, m.Description, name, m.Description)
		if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte(md), 0o644); err != nil {
			_ = os.RemoveAll(dst)
			return 0, err
		}
		return 1, nil
	}
	body, err := yaml.Marshal(&doc)
	if err != nil {
		_ = os.RemoveAll(dst)
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(dst, "skill.yaml"), body, 0o644); err != nil {
		_ = os.RemoveAll(dst)
		return 0, err
	}
	_ = os.WriteFile(filepath.Join(dst, "README.md"), []byte("# "+name+"\n\n"+m.Description+"\n"), 0o644)
	return 1, nil
}

func mobileCopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func removeFileIfExists(dir, name string) error {
	path := filepath.Join(dir, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
