package agent

// AgentDefinition and AgentRegistry: declarative sub-agent configuration.
// Inspired by OpenHuman's agent/harness/definition.rs — sub-agents are defined
// in YAML files rather than hardcoded, allowing users to create custom agents
// for specific tasks (code review, translation, testing, etc.).
//
// Definitions are loaded from:
// 1. Built-in defaults (embedded in binary)
// 2. ~/.maclaw/agents/*.yaml (user-level)
// 3. <project>/.maclaw/agents/*.yaml (project-level, overrides user-level)
//
// Each definition specifies:
// - System prompt (the agent's personality and instructions)
// - Tool whitelist (which tools the agent can use)
// - Model routing task type (which model to use)
// - Max iterations (safety limit)
// - Sandbox mode (full/readonly/none)

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// AgentDefinition describes a sub-agent archetype.
type AgentDefinition struct {
	Name         string   `yaml:"name" json:"name"`
	Description  string   `yaml:"description" json:"description"`
	SystemPrompt string   `yaml:"system_prompt" json:"system_prompt"`
	Tools        []string `yaml:"tools" json:"tools"`                 // tool whitelist (empty = all tools)
	MaxRounds    int      `yaml:"max_rounds" json:"max_rounds"`       // max iterations (0 = default 50)
	ModelTask    string   `yaml:"model" json:"model"`                 // model routing task type
	Sandbox      string   `yaml:"sandbox" json:"sandbox"`             // "full" | "readonly" | "none"
	Tags         []string `yaml:"tags,omitempty" json:"tags,omitempty"` // for discovery/search
	Source       string   `yaml:"-" json:"source,omitempty"`          // "builtin" | "user" | "project"
}

// EffectiveMaxRounds returns MaxRounds with a sensible default.
func (d AgentDefinition) EffectiveMaxRounds() int {
	if d.MaxRounds <= 0 {
		return 50
	}
	return d.MaxRounds
}

// IsReadOnly returns true if the agent should not modify files.
func (d AgentDefinition) IsReadOnly() bool {
	return d.Sandbox == "readonly"
}

// AgentRegistry manages agent definitions from multiple sources.
type AgentRegistry struct {
	mu          sync.RWMutex
	definitions map[string]*AgentDefinition // name → definition
	dirs        []string                    // directories to scan
}

// NewAgentRegistry creates a registry that scans the given directories.
// Directories are scanned in order; later directories override earlier ones.
func NewAgentRegistry(dirs ...string) *AgentRegistry {
	r := &AgentRegistry{
		definitions: make(map[string]*AgentDefinition),
		dirs:        dirs,
	}
	r.registerBuiltins()
	return r
}

// Load scans all configured directories and loads YAML definitions.
// Later directories override earlier ones (project > user > builtin).
func (r *AgentRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Keep builtins, clear user/project definitions
	for name, def := range r.definitions {
		if def.Source != "builtin" {
			delete(r.definitions, name)
		}
	}

	for _, dir := range r.dirs {
		if err := r.loadDirLocked(dir); err != nil {
			log.Printf("[agent-registry] warning: failed to scan %s: %v", dir, err)
		}
	}
	return nil
}

// Get returns a definition by name, or nil if not found.
func (r *AgentRegistry) Get(name string) *AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.definitions[name]
}

// List returns all registered definitions sorted by name.
func (r *AgentRegistry) List() []*AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*AgentDefinition, 0, len(r.definitions))
	for _, d := range r.definitions {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Names returns all registered definition names.
func (r *AgentRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.definitions))
	for name := range r.definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Register adds or replaces a definition programmatically.
func (r *AgentRegistry) Register(def *AgentDefinition) {
	if def == nil || def.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.definitions[def.Name] = def
}

// Search finds definitions matching a query (searches name, description, tags).
func (r *AgentRegistry) Search(query string) []*AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query = strings.ToLower(query)
	var results []*AgentDefinition
	for _, d := range r.definitions {
		if strings.Contains(strings.ToLower(d.Name), query) ||
			strings.Contains(strings.ToLower(d.Description), query) {
			results = append(results, d)
			continue
		}
		for _, tag := range d.Tags {
			if strings.Contains(strings.ToLower(tag), query) {
				results = append(results, d)
				break
			}
		}
	}
	return results
}

// --- Built-in definitions ---

func (r *AgentRegistry) registerBuiltins() {
	builtins := []*AgentDefinition{
		{
			Name:        "coding_workflow",
			Description: "编码工作流专家，引导需求→设计→任务拆分，使用 ask_user 确认每个阶段",
			SystemPrompt: "你是一个编码工作流专家。你的职责是引导用户完成编程任务的三个阶段：需求分析、技术设计、任务拆分。每个阶段完成后使用 ask_user 工具请求用户确认。",
			Tools:       []string{"ask_user", "task", "read_file", "list_directory", "web_search"},
			MaxRounds:   30,
			ModelTask:   "default",
			Sandbox:     "readonly",
			Tags:        []string{"coding", "workflow", "planning"},
			Source:      "builtin",
		},
		{
			Name:        "code_reviewer",
			Description: "代码审查专家，检查代码质量、安全性和最佳实践",
			SystemPrompt: "你是一个代码审查专家。审查时关注：1. 安全漏洞 2. 性能问题 3. 代码风格和可维护性 4. 测试覆盖率。输出结构化的审查报告。",
			Tools:       []string{"read_file", "list_directory", "bash"},
			MaxRounds:   20,
			ModelTask:   "reasoning",
			Sandbox:     "readonly",
			Tags:        []string{"review", "quality", "security"},
			Source:      "builtin",
		},
		{
			Name:        "researcher",
			Description: "研究助手，搜索和整理信息",
			SystemPrompt: "你是一个研究助手。你的职责是搜索、整理和总结信息。使用 web_search 和 web_fetch 获取最新信息，用结构化格式呈现结果。",
			Tools:       []string{"web_search", "web_fetch", "write_file", "read_file"},
			MaxRounds:   30,
			ModelTask:   "default",
			Sandbox:     "full",
			Tags:        []string{"research", "search", "information"},
			Source:      "builtin",
		},
		{
			Name:        "help",
			Description: "MaClaw 使用帮助专家，回答功能/配置/工具使用问题",
			SystemPrompt: "你是 MaClaw 的使用帮助专家。回答用户关于 MaClaw 功能、配置、工具使用的问题。",
			Tools:       []string{"read_file", "list_directory"},
			MaxRounds:   10,
			ModelTask:   "fast",
			Sandbox:     "readonly",
			Tags:        []string{"help", "documentation", "usage"},
			Source:      "builtin",
		},
	}
	for _, d := range builtins {
		r.definitions[d.Name] = d
	}
}

// --- File loading ---

func (r *AgentRegistry) loadDirLocked(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // directory doesn't exist yet, not an error
		}
		return err
	}

	source := "user"
	if strings.Contains(dir, ".maclaw/agents") && !strings.HasPrefix(dir, filepath.Join(os.Getenv("HOME"), ".maclaw")) {
		source = "project"
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		def, err := loadDefinitionFile(path)
		if err != nil {
			log.Printf("[agent-registry] failed to load %s: %v", path, err)
			continue
		}
		def.Source = source
		r.definitions[def.Name] = def
	}
	return nil
}

func loadDefinitionFile(path string) (*AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def AgentDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if def.Name == "" {
		// Use filename without extension as name
		base := filepath.Base(path)
		def.Name = strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	}
	return &def, nil
}
