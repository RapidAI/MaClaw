package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// GeneratedMetadata 自动生成的元数据。
type GeneratedMetadata struct {
	Name        string   `yaml:"name,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Triggers    []string `yaml:"triggers,omitempty"`
	Price       int      `yaml:"price,omitempty"`
}

// TagGenerator 自动 Tag 生成器。
type TagGenerator struct{}

// NewTagGenerator 创建 TagGenerator。
func NewTagGenerator() *TagGenerator {
	return &TagGenerator{}
}

// GenerateTags 分析 Skill 目录，生成/补全元数据。
// 保留已有非空字段，仅补全缺失字段。
func (g *TagGenerator) GenerateTags(skillDir string) (*GeneratedMetadata, error) {
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("read skill.yaml: %w", err)
	}

	var existing map[string]interface{}
	if err := yaml.Unmarshal(data, &existing); err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w", err)
	}

	result := &GeneratedMetadata{}

	// 保留已有字段
	if name, ok := existing["name"].(string); ok && name != "" {
		result.Name = name
	}
	if desc, ok := existing["description"].(string); ok && desc != "" {
		result.Description = desc
	}
	if tags, ok := existing["tags"].([]interface{}); ok && len(tags) > 0 {
		for _, t := range tags {
			if s, ok := t.(string); ok {
				result.Tags = append(result.Tags, s)
			}
		}
	}

	// 扫描脚本文件推断缺失的 tags
	if len(result.Tags) == 0 {
		result.Tags = g.inferTags(skillDir)
	}

	// 推断 price（简单启发式）
	if price, ok := existing["price"].(int); ok && price > 0 {
		result.Price = price
	} else {
		result.Price = g.inferPrice(skillDir)
	}

	return result, nil
}

// inferTags 从脚本文件内容推断 tags。
func (g *TagGenerator) inferTags(skillDir string) []string {
	tagSet := make(map[string]bool)

	_ = filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".py":
			tagSet["python"] = true
			content, _ := os.ReadFile(path)
			g.scanContentTags(string(content), tagSet)
		case ".sh", ".bash":
			tagSet["shell"] = true
		case ".go":
			tagSet["golang"] = true
		case ".js", ".ts":
			tagSet["javascript"] = true
		}
		return nil
	})

	var tags []string
	for t := range tagSet {
		tags = append(tags, t)
	}
	return tags
}

// scanContentTags 从文件内容推断领域 tags。
func (g *TagGenerator) scanContentTags(content string, tagSet map[string]bool) {
	lower := strings.ToLower(content)
	patterns := map[string][]string{
		"web-scraping":  {"requests.get", "beautifulsoup", "scrapy", "selenium"},
		"data-analysis": {"pandas", "numpy", "matplotlib", "seaborn"},
		"file-management": {"shutil", "os.path", "pathlib", "glob"},
		"automation":    {"subprocess", "os.system", "schedule"},
		"api":           {"flask", "fastapi", "django", "http.server"},
		"database":      {"sqlite3", "sqlalchemy", "pymongo"},
		"ai-ml":         {"torch", "tensorflow", "sklearn", "openai"},
		"network":       {"socket", "paramiko", "ftplib", "smtplib"},
	}
	for tag, keywords := range patterns {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				tagSet[tag] = true
				break
			}
		}
	}
}

// inferPrice 根据 Skill 复杂度推断价格。
func (g *TagGenerator) inferPrice(skillDir string) int {
	fileCount := 0
	totalLines := 0
	_ = filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".py" || ext == ".sh" || ext == ".go" || ext == ".js" {
			fileCount++
			data, _ := os.ReadFile(path)
			totalLines += strings.Count(string(data), "\n")
		}
		return nil
	})

	// 极简 → 0, 普通 → 5~15, 复杂 → 20~50
	if fileCount <= 1 && totalLines < 30 {
		return 0
	}
	if fileCount <= 3 && totalLines < 200 {
		return 10
	}
	if totalLines < 500 {
		return 25
	}
	return 40
}

// WriteBackToYAML 将生成的元数据写回 skill.yaml（仅补全缺失字段）。
func (g *TagGenerator) WriteBackToYAML(skillDir string, meta *GeneratedMetadata) error {
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return err
	}

	var existing map[string]interface{}
	if err := yaml.Unmarshal(data, &existing); err != nil {
		return err
	}
	if existing == nil {
		existing = make(map[string]interface{})
	}

	// 仅补全缺失字段
	if _, ok := existing["tags"]; !ok && len(meta.Tags) > 0 {
		existing["tags"] = meta.Tags
	}
	if _, ok := existing["price"]; !ok && meta.Price > 0 {
		existing["price"] = meta.Price
	}

	out, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	return os.WriteFile(yamlPath, out, 0o644)
}
