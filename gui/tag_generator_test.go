package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── Task 32.3: TagGenerator 单元测试 ────────────────────────────────────

func createTestSkillDir(t *testing.T, yamlContent string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTagGenerator_PreserveExistingFields(t *testing.T) {
	gen := NewTagGenerator()
	dir := createTestSkillDir(t, `
name: my-tool
description: A great tool
tags:
  - custom-tag
  - another-tag
price: 20
`, nil)

	meta, err := gen.GenerateTags(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "my-tool" {
		t.Errorf("name=%s, want my-tool", meta.Name)
	}
	if meta.Description != "A great tool" {
		t.Errorf("description=%s, want A great tool", meta.Description)
	}
	if len(meta.Tags) != 2 || meta.Tags[0] != "custom-tag" {
		t.Errorf("tags=%v, want [custom-tag another-tag]", meta.Tags)
	}
	if meta.Price != 20 {
		t.Errorf("price=%d, want 20", meta.Price)
	}
}

func TestTagGenerator_FillMissingTags(t *testing.T) {
	gen := NewTagGenerator()
	dir := createTestSkillDir(t, `
name: my-tool
description: A tool
`, map[string]string{
		"main.py": "import requests\nresponse = requests.get('https://example.com')\n",
	})

	meta, err := gen.GenerateTags(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 应推断出 python 和 web-scraping tags
	if len(meta.Tags) == 0 {
		t.Error("expected inferred tags, got none")
	}
	hasPython := false
	for _, tag := range meta.Tags {
		if tag == "python" {
			hasPython = true
		}
	}
	if !hasPython {
		t.Errorf("expected 'python' tag, got %v", meta.Tags)
	}
}

func TestTagGenerator_InferPriceSimple(t *testing.T) {
	gen := NewTagGenerator()
	// 极简 Skill：1 个文件 < 30 行 → price=0
	dir := createTestSkillDir(t, `
name: simple
description: simple tool
`, map[string]string{
		"main.py": "print('hello')\n",
	})

	meta, err := gen.GenerateTags(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Price != 0 {
		t.Errorf("simple skill price=%d, want 0", meta.Price)
	}
}

func TestTagGenerator_InferPriceComplex(t *testing.T) {
	gen := NewTagGenerator()
	// 复杂 Skill：多文件多行
	files := make(map[string]string)
	for i := 0; i < 5; i++ {
		content := ""
		for j := 0; j < 120; j++ {
			content += "# line of code\n"
		}
		files["module"+string(rune('a'+i))+".py"] = content
	}
	dir := createTestSkillDir(t, `
name: complex
description: complex tool
`, files)

	meta, err := gen.GenerateTags(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Price < 20 {
		t.Errorf("complex skill price=%d, want >= 20", meta.Price)
	}
}

func TestTagGenerator_WriteBackOnlyMissing(t *testing.T) {
	gen := NewTagGenerator()
	dir := createTestSkillDir(t, `
name: my-tool
description: existing desc
`, map[string]string{
		"main.sh": "echo hello\n",
	})

	meta, err := gen.GenerateTags(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := gen.WriteBackToYAML(dir, meta); err != nil {
		t.Fatal(err)
	}

	// 读回验证
	data, _ := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	var result map[string]interface{}
	_ = yaml.Unmarshal(data, &result)

	// name 和 description 应保留
	if result["name"] != "my-tool" {
		t.Errorf("name=%v, want my-tool", result["name"])
	}
	if result["description"] != "existing desc" {
		t.Errorf("description=%v, want existing desc", result["description"])
	}
}

func TestTagGenerator_TagCategories(t *testing.T) {
	gen := NewTagGenerator()
	dir := createTestSkillDir(t, `
name: data-tool
description: data analysis
`, map[string]string{
		"analyze.py": "import pandas as pd\nimport numpy as np\ndf = pd.DataFrame()\n",
	})

	meta, err := gen.GenerateTags(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 应包含功能类 tag (python) 和领域类 tag (data-analysis)
	tagSet := make(map[string]bool)
	for _, tag := range meta.Tags {
		tagSet[tag] = true
	}
	if !tagSet["python"] {
		t.Error("missing functional tag: python")
	}
	if !tagSet["data-analysis"] {
		t.Error("missing domain tag: data-analysis")
	}
}
