package plugin

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// knownPluginTypes is the set of valid PluginType values.
var knownPluginTypes = map[PluginType]bool{
	PluginTypeMCP:      true,
	PluginTypeLocalMCP: true,
	PluginTypeNLSkill:  true,
	PluginTypeNative:   true,
	PluginTypeScript:   true,
}

// typeConfigKeys lists the YAML keys that hold type-specific configuration.
var typeConfigKeys = map[string]bool{
	"mcp":      true,
	"local_mcp": true,
	"nlskill":  true,
	"script":   true,
}

// rawManifest is the intermediate struct used for YAML round-tripping.
// It captures all known top-level fields. Type-specific config sections
// (mcp, local_mcp, nlskill) are extracted separately from the raw map.
type rawManifest struct {
	Name        string                 `yaml:"name"`
	Version     string                 `yaml:"version,omitempty"`
	Description string                 `yaml:"description,omitempty"`
	Type        PluginType             `yaml:"type"`
	Author      string                 `yaml:"author,omitempty"`
	Tags        []string               `yaml:"tags,omitempty"`
	Platforms   []string               `yaml:"platforms,omitempty"`
	Settings    map[string]interface{} `yaml:"settings,omitempty"`
}

// ParseManifestFile reads a plugin.yaml file from disk and parses it
// into a PluginManifest. Returns an error if the file cannot be read,
// the YAML is invalid, name is empty, or type is unknown.
func ParseManifestFile(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin manifest: %w", err)
	}
	return ParseManifestBytes(data)
}

// ParseManifestBytes parses raw YAML bytes into a PluginManifest.
// Validates that name is non-empty and type is a known PluginType.
func ParseManifestBytes(data []byte) (*PluginManifest, error) {
	// First pass: parse into raw map to capture type-specific config sections.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	// Second pass: parse known fields into the intermediate struct.
	var rm rawManifest
	if err := yaml.Unmarshal(data, &rm); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	// Validate required fields.
	if rm.Name == "" {
		return nil, fmt.Errorf("plugin manifest: name must be non-empty")
	}
	if !knownPluginTypes[rm.Type] {
		return nil, fmt.Errorf("plugin manifest: unknown type %q", rm.Type)
	}

	// Extract type-specific config sections from the raw map.
	rawTypeConfig := make(map[string]interface{})
	for key := range typeConfigKeys {
		if v, ok := raw[key]; ok {
			rawTypeConfig[key] = v
		}
	}
	if len(rawTypeConfig) == 0 {
		rawTypeConfig = nil
	}

	m := &PluginManifest{
		Name:          rm.Name,
		Version:       rm.Version,
		Description:   rm.Description,
		Type:          rm.Type,
		Author:        rm.Author,
		Tags:          rm.Tags,
		Platforms:     rm.Platforms,
		Settings:      rm.Settings,
		RawTypeConfig: rawTypeConfig,
	}
	return m, nil
}

// FormatManifestFile serializes a PluginManifest to valid YAML bytes.
// The RawTypeConfig entries are placed under their respective top-level
// keys (e.g., "mcp", "local_mcp", "nlskill") to match the plugin.yaml format.
func FormatManifestFile(m *PluginManifest) ([]byte, error) {
	// Marshal the known fields first.
	rm := rawManifest{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Type:        m.Type,
		Author:      m.Author,
		Tags:        m.Tags,
		Platforms:   m.Platforms,
		Settings:    m.Settings,
	}
	data, err := yaml.Marshal(&rm)
	if err != nil {
		return nil, fmt.Errorf("marshal plugin manifest: %w", err)
	}

	if len(m.RawTypeConfig) == 0 {
		return data, nil
	}

	// Merge type-specific config sections into the output.
	var base map[string]interface{}
	if err := yaml.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("marshal plugin manifest: %w", err)
	}
	if base == nil {
		base = make(map[string]interface{})
	}
	for k, v := range m.RawTypeConfig {
		base[k] = v
	}
	merged, err := yaml.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("marshal plugin manifest: %w", err)
	}
	return merged, nil
}
