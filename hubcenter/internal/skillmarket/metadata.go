package skillmarket

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// SkillMetadata 是 skill.yaml 的结构化表示。
type SkillMetadata struct {
	ID          string   `yaml:"id,omitempty" json:"id,omitempty"` // UUID，首次创建时生成，重新上传时复用
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Triggers    []string `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Version     string   `yaml:"version,omitempty" json:"version,omitempty"`
	Author      string   `yaml:"author,omitempty" json:"author,omitempty"`
	Price       int      `yaml:"price,omitempty" json:"price,omitempty"`

	// 安全相关字段 (Req 37)
	Permissions []string `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	RequiredEnv []string `yaml:"required_env,omitempty" json:"required_env,omitempty"`

	// PricingMode 定价模式 (Req 36): auto|free|fixed
	PricingMode string `yaml:"pricing_mode,omitempty" json:"pricing_mode,omitempty"`

	// 平台兼容性
	Platforms   []string `yaml:"platforms,omitempty" json:"platforms,omitempty"`     // "windows","linux","macos"; empty = universal
	RequiresGUI bool     `yaml:"requires_gui,omitempty" json:"requires_gui,omitempty"` // Linux 下是否需要 GUI 环境

	// Extra 保留未识别字段，确保 round-trip 安全。
	Extra map[string]any `yaml:"-" json:"-"`
}

// ParseSkillYAML 解析 skill.yaml 为 SkillMetadata。
// 未识别字段保留在 Extra 中，确保 round-trip 安全。
func ParseSkillYAML(data []byte) (*SkillMetadata, error) {
	// 先解析到 map 获取所有字段
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("parse skill.yaml: empty document")
	}

	// 解析已知字段
	var meta SkillMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w", err)
	}

	// 收集未识别字段到 Extra
	knownKeys := map[string]bool{
		"id": true, "name": true, "description": true, "tags": true,
		"triggers": true, "version": true, "author": true,
		"price": true, "permissions": true, "required_env": true,
		"pricing_mode": true, "platforms": true, "requires_gui": true,
	}
	extra := make(map[string]any)
	for k, v := range raw {
		if !knownKeys[k] {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		meta.Extra = extra
	}

	return &meta, nil
}

// FormatSkillYAML 将 SkillMetadata 格式化为 YAML 文本。
// Extra 中的未识别字段会被保留输出。
func FormatSkillYAML(meta *SkillMetadata) ([]byte, error) {
	// 先序列化已知字段到 map
	data, err := yaml.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("format skill.yaml: %w", err)
	}

	// 如果没有 Extra 字段，直接返回
	if len(meta.Extra) == 0 {
		return data, nil
	}

	// 有 Extra 字段时，合并到输出
	var known map[string]any
	if err := yaml.Unmarshal(data, &known); err != nil {
		return nil, fmt.Errorf("format skill.yaml: re-parse: %w", err)
	}
	if known == nil {
		known = make(map[string]any)
	}
	for k, v := range meta.Extra {
		known[k] = v
	}
	merged, err := yaml.Marshal(known)
	if err != nil {
		return nil, fmt.Errorf("format skill.yaml: merge: %w", err)
	}
	return merged, nil
}

// ValidateMetadata 检查 SkillMetadata 的必填字段。
func ValidateMetadata(meta *SkillMetadata) []string {
	var errs []string
	if meta.Name == "" {
		errs = append(errs, "name is required")
	}
	if meta.Description == "" {
		errs = append(errs, "description is required")
	}
	if meta.Price < 0 {
		errs = append(errs, "price must be non-negative")
	}
	return errs
}
