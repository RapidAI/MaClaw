package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/skill"
)

type GeneratedMetadata struct {
	Name        string   `yaml:"name,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Triggers    []string `yaml:"triggers,omitempty"`
	Price       int      `yaml:"price,omitempty"`
}

type TagGenerator struct{}

func NewTagGenerator() *TagGenerator {
	return &TagGenerator{}
}

func readSkillYAMLFile(skillDir string) (*skill.SkillYAMLFile, string, error) {
	parsed, defPath, _, err := readSkillDefinitionForTags(skillDir)
	return parsed, defPath, err
}

func readSkillDefinitionForTags(skillDir string) (*skill.SkillYAMLFile, string, string, error) {
	defPath, defFormat := findSkillDefinitionFile(skillDir)
	if defPath == "" {
		return nil, "", "", os.ErrNotExist
	}
	data, err := os.ReadFile(defPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("read %s: %w", filepath.Base(defPath), err)
	}
	parsed, err := skill.ParseSkillDefinitionFile(data, defFormat)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse %s: %w", filepath.Base(defPath), err)
	}
	return parsed, defPath, defFormat, nil
}

func (g *TagGenerator) GenerateTags(skillDir string) (*GeneratedMetadata, error) {
	existing, _, err := readSkillYAMLFile(skillDir)
	if err != nil {
		return nil, err
	}

	result := &GeneratedMetadata{}

	if existing.Name != "" {
		result.Name = existing.Name
	}
	if existing.Description != "" {
		result.Description = existing.Description
	}
	if len(existing.Extra) > 0 {
		if tags, ok := existing.Extra["tags"].([]any); ok && len(tags) > 0 {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					result.Tags = append(result.Tags, s)
				}
			}
		}
		if price, ok := existing.Extra["price"].(int); ok && price > 0 {
			result.Price = price
		} else if priceNum, ok := existing.Extra["price"].(float64); ok && priceNum > 0 {
			result.Price = int(priceNum)
		}
	}

	if len(result.Tags) == 0 {
		result.Tags = g.inferTags(skillDir)
	}
	if result.Price == 0 {
		result.Price = g.inferPrice(skillDir)
	}

	return result, nil
}

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

func (g *TagGenerator) scanContentTags(content string, tagSet map[string]bool) {
	lower := strings.ToLower(content)
	patterns := map[string][]string{
		"web-scraping":    {"requests.get", "beautifulsoup", "scrapy", "selenium"},
		"data-analysis":   {"pandas", "numpy", "matplotlib", "seaborn"},
		"file-management": {"shutil", "os.path", "pathlib", "glob"},
		"automation":      {"subprocess", "os.system", "schedule"},
		"api":             {"flask", "fastapi", "django", "http.server"},
		"database":        {"sqlite3", "sqlalchemy", "pymongo"},
		"ai-ml":           {"torch", "tensorflow", "sklearn", "openai"},
		"network":         {"socket", "paramiko", "ftplib", "smtplib"},
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

func (g *TagGenerator) WriteBackToYAML(skillDir string, meta *GeneratedMetadata) error {
	existing, defPath, defFormat, err := readSkillDefinitionForTags(skillDir)
	if err != nil {
		return err
	}
	if existing.Extra == nil {
		existing.Extra = make(map[string]any)
	}

	if _, ok := existing.Extra["tags"]; !ok && len(meta.Tags) > 0 {
		existing.Extra["tags"] = meta.Tags
	}
	if _, ok := existing.Extra["price"]; !ok && meta.Price > 0 {
		existing.Extra["price"] = meta.Price
	}

	out, err := skill.FormatSkillDefinitionFile(existing, defFormat)
	if err != nil {
		return err
	}
	return os.WriteFile(defPath, out, 0o644)
}
