// Package botcatalog is a browsable Skills/MCP directory with per-bot install
// state. It sits on top of the repo mcps/ tree and skills-lock.json; one-click
// install/uninstall records an assignment for a bot without replacing the
// existing global skill/MCP lifecycle.
package botcatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const assignmentsFileName = "bot_catalog_assignments.json"

// Kind distinguishes catalog rows.
type Kind string

const (
	KindSkill Kind = "skill"
	KindMCP   Kind = "mcp"
)

// Item is one browsable catalog entry.
type Item struct {
	ID          string   `json:"id"`
	Kind        Kind     `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Source      string   `json:"source,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Installed   bool     `json:"installed"`
}

// AssignmentStore keeps which catalog items each bot has installed.
type AssignmentStore struct {
	mu   sync.Mutex
	path string
	data map[string]map[string]bool // botID -> itemKey -> true
}

type assignmentFile struct {
	Version int                          `json:"version"`
	Bots    map[string]map[string]bool   `json:"bots"`
}

func itemKey(kind Kind, id string) string {
	return string(kind) + ":" + strings.TrimSpace(id)
}

// OpenAssignments loads or creates the assignment store under dataDir.
func OpenAssignments(dataDir string) (*AssignmentStore, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &AssignmentStore{path: filepath.Join(dataDir, assignmentsFileName), data: map[string]map[string]bool{}}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var file assignmentFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Bots != nil {
		s.data = file.Bots
	}
	return s, nil
}

func (s *AssignmentStore) persistLocked() error {
	file := assignmentFile{Version: 1, Bots: s.data}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// IsInstalled reports whether botID has kind/id assigned.
func (s *AssignmentStore) IsInstalled(botID string, kind Kind, id string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bot := s.data[strings.TrimSpace(botID)]
	if bot == nil {
		return false
	}
	return bot[itemKey(kind, id)]
}

// Install assigns kind/id to botID.
func (s *AssignmentStore) Install(botID string, kind Kind, id string) error {
	if s == nil {
		return fmt.Errorf("assignment store is nil")
	}
	botID = strings.TrimSpace(botID)
	id = strings.TrimSpace(id)
	if botID == "" || id == "" {
		return fmt.Errorf("bot id and item id are required")
	}
	if kind != KindSkill && kind != KindMCP {
		return fmt.Errorf("unknown kind %q", kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[botID] == nil {
		s.data[botID] = map[string]bool{}
	}
	s.data[botID][itemKey(kind, id)] = true
	return s.persistLocked()
}

// Uninstall removes the assignment for botID.
func (s *AssignmentStore) Uninstall(botID string, kind Kind, id string) error {
	if s == nil {
		return fmt.Errorf("assignment store is nil")
	}
	botID = strings.TrimSpace(botID)
	id = strings.TrimSpace(id)
	if botID == "" || id == "" {
		return fmt.Errorf("bot id and item id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bot := s.data[botID]
	if bot == nil {
		return nil
	}
	delete(bot, itemKey(kind, id))
	if len(bot) == 0 {
		delete(s.data, botID)
	}
	return s.persistLocked()
}

// skillsLockFile matches the repo skills-lock.json shape.
type skillsLockFile struct {
	Version int `json:"version"`
	Skills  map[string]struct {
		Source     string `json:"source"`
		SourceType string `json:"sourceType"`
		SkillPath  string `json:"skillPath"`
	} `json:"skills"`
}

// LoadCatalog reads bundled MCP packages under mcpsDir and skills from
// skills-lock.json. query filters by id/name/description/source (case-insensitive).
func LoadCatalog(mcpsDir, skillsLockPath, query string, store *AssignmentStore, botID string) ([]Item, error) {
	items := make([]Item, 0, 16)
	if strings.TrimSpace(mcpsDir) != "" {
		entries, err := os.ReadDir(mcpsDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, ent := range entries {
			if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
				continue
			}
			name := ent.Name()
			desc := "Bundled MCP package"
			toolsDir := filepath.Join(mcpsDir, name, "tools")
			if toolEntries, err := os.ReadDir(toolsDir); err == nil {
				names := make([]string, 0, len(toolEntries))
				for _, te := range toolEntries {
					if te.IsDir() {
						continue
					}
					n := strings.TrimSuffix(te.Name(), filepath.Ext(te.Name()))
					if n != "" {
						names = append(names, n)
					}
				}
				sort.Strings(names)
				if len(names) > 0 {
					desc = "Tools: " + strings.Join(names, ", ")
				}
			}
			items = append(items, Item{
				ID:          name,
				Kind:        KindMCP,
				Name:        name,
				Description: desc,
				Source:      "mcps/" + name,
				Tags:        []string{"mcp", "bundled"},
				Installed:   store != nil && store.IsInstalled(botID, KindMCP, name),
			})
		}
	}
	if strings.TrimSpace(skillsLockPath) != "" {
		raw, err := os.ReadFile(skillsLockPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			var lock skillsLockFile
			if err := json.Unmarshal(raw, &lock); err != nil {
				return nil, err
			}
			names := make([]string, 0, len(lock.Skills))
			for name := range lock.Skills {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				meta := lock.Skills[name]
				items = append(items, Item{
					ID:          name,
					Kind:        KindSkill,
					Name:        name,
					Description: meta.SkillPath,
					Source:      meta.Source,
					Tags:        []string{"skill", meta.SourceType},
					Installed:   store != nil && store.IsInstalled(botID, KindSkill, name),
				})
			}
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Kind != items[j].Kind {
				return items[i].Kind < items[j].Kind
			}
			return items[i].Name < items[j].Name
		})
		return items, nil
	}
	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		hay := strings.ToLower(strings.Join([]string{item.ID, item.Name, item.Description, item.Source, string(item.Kind)}, " "))
		if strings.Contains(hay, q) {
			filtered = append(filtered, item)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Kind != filtered[j].Kind {
			return filtered[i].Kind < filtered[j].Kind
		}
		return filtered[i].Name < filtered[j].Name
	})
	return filtered, nil
}
