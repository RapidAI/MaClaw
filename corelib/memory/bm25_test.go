package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBM25_BasicScoring(t *testing.T) {
	idx := newBM25Index()
	entries := []Entry{
		{ID: "1", Content: "deploy_all.cmd 部署脚本执行失败，需要检查路径配置"},
		{ID: "2", Content: "用户偏好使用 VS Code 作为编辑器"},
		{ID: "3", Content: "项目使用 Go 1.25，依赖 gse 做中文分词"},
		{ID: "4", Content: "上次部署时 deploy 脚本的权限问题已修复"},
		{ID: "5", Content: "数据库连接池大小设置为 20"},
	}
	idx.rebuild(entries)

	// Query about deployment should rank deploy-related entries higher.
	scores := idx.score("部署脚本的问题")
	if scores == nil {
		t.Fatal("expected non-nil scores")
	}

	// Entry 1 and 4 are about deployment, should have scores.
	if scores["1"] <= 0 {
		t.Errorf("entry 1 (deploy failure) should have positive score, got %f", scores["1"])
	}
	if scores["4"] <= 0 {
		t.Errorf("entry 4 (deploy permission) should have positive score, got %f", scores["4"])
	}
	// Entry 5 (database) should not match.
	if scores["5"] > 0 {
		t.Errorf("entry 5 (database) should not match deploy query, got %f", scores["5"])
	}
}

func TestBM25_EmptyQuery(t *testing.T) {
	idx := newBM25Index()
	idx.rebuild([]Entry{{ID: "1", Content: "hello world"}})
	scores := idx.score("")
	if scores != nil {
		t.Errorf("empty query should return nil scores, got %v", scores)
	}
}

func TestBM25_EmptyIndex(t *testing.T) {
	idx := newBM25Index()
	scores := idx.score("hello")
	if scores != nil {
		t.Errorf("empty index should return nil scores, got %v", scores)
	}
}

func TestBM25_AddRemoveUpdate(t *testing.T) {
	idx := newBM25Index()
	e1 := Entry{ID: "1", Content: "Go 语言编程"}
	e2 := Entry{ID: "2", Content: "Python 数据分析"}

	idx.addEntry(e1)
	idx.addEntry(e2)

	scores := idx.score("Go 编程")
	if scores["1"] <= 0 {
		t.Errorf("entry 1 should match 'Go 编程'")
	}

	idx.removeEntry("1")
	scores = idx.score("Go 编程")
	if scores["1"] > 0 {
		t.Errorf("entry 1 should be removed")
	}

	// Update entry 2 to mention Go.
	e2.Content = "Go 和 Python 混合编程"
	idx.updateEntry(e2)
	scores = idx.score("Go 编程")
	if scores["2"] <= 0 {
		t.Errorf("updated entry 2 should match 'Go 编程'")
	}
}

func TestBM25_TagsIndexed(t *testing.T) {
	idx := newBM25Index()
	e := Entry{ID: "1", Content: "配置文件说明", Tags: []string{"deployment", "config"}}
	idx.addEntry(e)

	scores := idx.score("deployment")
	if scores["1"] <= 0 {
		t.Errorf("tags should be indexed, entry should match 'deployment'")
	}
}

func TestStore_RecallWithBM25(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entries := []Entry{
		{Content: "deploy_all.cmd 部署脚本执行失败", Category: CategoryProjectKnowledge, Tags: []string{"deploy"}},
		{Content: "用户喜欢深色主题", Category: CategoryPreference},
		{Content: "Go 项目使用 gse 分词库", Category: CategoryProjectKnowledge, Tags: []string{"nlp"}},
		{Content: "上次部署权限问题已修复", Category: CategoryProjectKnowledge, Tags: []string{"deploy"}},
		{Content: "数据库连接池设置为 20", Category: CategoryProjectKnowledge},
	}
	for _, e := range entries {
		if err := store.Save(e); err != nil {
			t.Fatal(err)
		}
	}

	results := store.Recall("部署脚本出了什么问题")
	if len(results) == 0 {
		t.Fatal("expected recall results")
	}

	// The top results should be deploy-related.
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(r.Content, "部署") || strings.Contains(r.Content, "deploy") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("top recall results should include deploy-related entries, got: %v", results)
	}
}
