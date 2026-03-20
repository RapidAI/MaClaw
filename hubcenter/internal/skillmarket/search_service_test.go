package skillmarket

import (
	"context"
	"testing"
)

// ── Task 31.5: 搜索功能测试 ─────────────────────────────────────────────

func setupSearchTest(t *testing.T) (*Store, *SearchService) {
	t.Helper()
	store := newTestStore(t)
	// SearchService 需要 skill.SkillStore, 但 FTS 索引可以直接通过 IndexSkill 测试
	svc := &SearchService{store: store}
	if err := svc.migrate(); err != nil {
		t.Fatal(err)
	}
	return store, svc
}

func TestSearch_FTS5Index(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	// 索引几个 Skill
	_ = svc.IndexSkill(ctx, "s1", "Python Formatter", "Format Python code", []string{"python", "format"}, 4.5, 100, 10, "published", "2025-01-01T00:00:00Z")
	_ = svc.IndexSkill(ctx, "s2", "Go Linter", "Lint Go code", []string{"go", "lint"}, 3.0, 50, 0, "published", "2025-01-02T00:00:00Z")
	_ = svc.IndexSkill(ctx, "s3", "Python Debugger", "Debug Python apps", []string{"python", "debug"}, 4.0, 200, 5, "trial", "2025-01-03T00:00:00Z")

	// 搜索 "python"
	results, err := svc.Search(ctx, "python", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'python', got %d", len(results))
	}
}

func TestSearch_TagsFilter(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Tool A", "desc", []string{"python", "format"}, 4.0, 100, 0, "published", "2025-01-01T00:00:00Z")
	_ = svc.IndexSkill(ctx, "s2", "Tool B", "desc", []string{"go", "lint"}, 3.0, 50, 0, "published", "2025-01-01T00:00:00Z")

	// 仅 tags 过滤
	results, err := svc.Search(ctx, "", []string{"python"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for tag 'python', got %d", len(results))
	}
	if len(results) > 0 && results[0].ID != "s1" {
		t.Errorf("expected s1, got %s", results[0].ID)
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	results, err := svc.Search(ctx, "nonexistent", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_SortByScore(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	// s1: 高评分高下载, s2: 低评分低下载
	_ = svc.IndexSkill(ctx, "s1", "Alpha Tool", "great tool", []string{"tool"}, 4.5, 500, 0, "published", "2025-01-01T00:00:00Z")
	_ = svc.IndexSkill(ctx, "s2", "Beta Tool", "okay tool", []string{"tool"}, 1.0, 5, 0, "published", "2025-01-01T00:00:00Z")

	results, err := svc.Search(ctx, "tool", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// s1 应排在前面（更高的综合得分）
	if results[0].Score < results[1].Score {
		t.Errorf("results not sorted by score: first=%.2f, second=%.2f", results[0].Score, results[1].Score)
	}
}

func TestSearch_ExcludesNonVisibleStatus(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Visible Tool", "desc", nil, 4.0, 100, 0, "published", "2025-01-01T00:00:00Z")
	_ = svc.IndexSkill(ctx, "s2", "Hidden Tool", "desc", nil, 4.0, 100, 0, "withdrawn", "2025-01-01T00:00:00Z")
	_ = svc.IndexSkill(ctx, "s3", "Rejected Tool", "desc", nil, 4.0, 100, 0, "rejected", "2025-01-01T00:00:00Z")

	results, _ := svc.Search(ctx, "", nil, 10)
	if len(results) != 1 {
		t.Errorf("expected 1 visible result, got %d", len(results))
	}
}

func TestSearch_RemoveSkill(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Tool", "desc", nil, 4.0, 100, 0, "published", "2025-01-01T00:00:00Z")
	_ = svc.RemoveSkill(ctx, "s1")

	results, _ := svc.Search(ctx, "", nil, 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results after removal, got %d", len(results))
	}
}
