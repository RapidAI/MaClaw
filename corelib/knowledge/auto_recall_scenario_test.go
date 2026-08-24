package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

// TestKnowledgeSearchScenarioServerInfoQuery reproduces the reported miss: a
// task-style imperative message ("build on api2 server ...") must still
// FTS-match a stored server-info note so knowledge_search can retrieve it.
func TestKnowledgeSearchScenarioServerInfoQuery(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_, err = store.SaveText(ctx, TextSaveRequest{
		Title: "api2_server_info",
		Text: "api2 服务器信息\n域名：api2.maclaw.top\n用户名：root\n项目目录：/opt/omniroute-src/\n构建方式：webpack 模式（OMNIROUTE_USE_TURBOPACK=0）",
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}

	query := "在api2服务器上，使用最新的ominiroute代码构建镜像，使用webpack，成功后启用新的omniroute服务"
	results, err := store.Search(ctx, SearchOptions{Query: query, Limit: 8})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected FTS hits for server-info note, got 0 results")
	}
}
