package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// loadTestEmbedder loads the Gemma embedding model for tests that need
// semantic classification. Returns nil if the model is not available.
func loadTestEmbedder(t *testing.T) embedding.Embedder {
	t.Helper()
	modelPath := ""
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		modelPath = p
	} else {
		home, _ := os.UserHomeDir()
		p := filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf")
		if _, err := os.Stat(p); err == nil {
			modelPath = p
		}
	}
	if modelPath == "" {
		return nil
	}
	emb, err := embedding.NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Logf("failed to load embedding model: %v", err)
		return nil
	}
	return emb
}

func makeToolDef(name, description string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
}

func TestRouter_BM25_ChineseQuery(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	// Build 35 tools (exceeds MaxToolBudget=28) to trigger routing.
	var tools []map[string]interface{}
	// Add all core tools first.
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core tool "+name))
	}
	// Add non-core candidates.
	tools = append(tools,
		makeToolDef("database_query", "执行数据库查询，支持 SQL 语句"),
		makeToolDef("git_commit", "提交代码到 Git 仓库"),
		makeToolDef("deploy_service", "部署服务到生产环境"),
		makeToolDef("network_scan", "扫描网络端口和服务"),
		makeToolDef("log_analyzer", "分析日志文件查找错误"),
		makeToolDef("image_resize", "调整图片大小和格式"),
		makeToolDef("email_send", "发送邮件通知"),
		makeToolDef("cache_clear", "清除缓存数据"),
		makeToolDef("backup_create", "创建数据备份"),
		makeToolDef("monitor_health", "监控服务健康状态"),
		makeToolDef("translate_text", "翻译文本内容"),
		makeToolDef("compress_file", "压缩文件和目录"),
		makeToolDef("schedule_task", "创建定时任务"),
		makeToolDef("search_code", "搜索代码库中的内容"),
		makeToolDef("format_code", "格式化代码文件"),
		makeToolDef("test_runner", "运行测试套件"),
		makeToolDef("doc_generator", "生成文档"),
		makeToolDef("api_tester", "测试 API 接口"),
		makeToolDef("perf_profiler", "性能分析工具"),
		makeToolDef("security_scan", "安全漏洞扫描"),
	)

	if len(tools) <= MaxToolBudget {
		t.Fatalf("need more than %d tools to test routing, got %d", MaxToolBudget, len(tools))
	}

	result := router.Route("我要查询数据库", tools)
	if len(result) > MaxToolBudget+2 { // +2 for possible recommendation hint
		t.Errorf("result should be within budget, got %d", len(result))
	}

	// database_query should be in the result since the query is about databases.
	found := false
	for _, r := range result {
		if ExtractToolName(r) == "database_query" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("database_query should be selected for '我要查询数据库', got: %v", names)
	}
}

func TestRouter_BM25_EmptyMessage(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("", tools)
	// Should still return results (BM25 returns nil scores, all get 0, still fills budget).
	if len(result) == 0 {
		t.Error("empty message should still return tools")
	}
}

func TestRouter_BM25_ConditionalKeep_PDFWorkflow(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	// Add workflow-specific tools that are not core.
	tools = append(tools,
		makeToolDef("web_search", "搜索网页和在线内容"),
		makeToolDef("send_file", "发送文件给用户"),
		makeToolDef("open", "打开文件"),
		makeToolDef("generate_pdf", "生成PDF文档"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("搜索 huggingface daily papers，生成每日论文综述，生成pdf发我", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	// web_search and document delivery tools should be kept for PDF intent.
	for _, name := range []string{"web_search", "send_file", "open"} {
		if !resultNames[name] {
			names := make([]string, len(result))
			for i, r := range result {
				names[i] = ExtractToolName(r)
			}
			t.Errorf("PDF workflow tool %q should be in result, got: %v", name, names)
		}
	}

	// SSH should NOT be in result (no SSH intent).
	if resultNames["ssh"] {
		t.Error("ssh should not be routed for PDF workflow")
	}
}

func TestRouter_BM25_ConditionalKeep_SearchWorkflow(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("web_search", "搜索网页和在线内容"),
		makeToolDef("ssh", "通过 SSH 连接服务器"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("搜索最新的 AI 新闻", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	// web_search should be kept for search intent.
	if !resultNames["web_search"] {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("web_search should be in result for search intent, got: %v", names)
	}

	// ssh should NOT be in result.
	if resultNames["ssh"] {
		t.Error("ssh should not be routed for search intent")
	}
}

func TestRouter_BM25_ConditionalKeep_BrowserWorkflow(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "浏览器自动化工具"),
		makeToolDef("ssh", "通过 SSH 连接服务器"),
		makeToolDef("web_search", "搜索网页"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("打开浏览器访问 example.com 网站并观察页面内容", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	// Browser tool should be kept for browser intent.
	if !resultNames["browser"] {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("browser tool should be in result, got: %v", names)
	}

	// ssh and web_search should NOT be in result.
	if resultNames["ssh"] {
		t.Error("ssh should not be routed for browser workflow")
	}
	if resultNames["web_search"] {
		t.Error("web_search should not be routed for browser workflow")
	}
}

func TestRouter_BM25_ConditionalKeep_NoFalseTriggers(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("ssh", "通过 SSH 连接服务器"),
		makeToolDef("web_search", "搜索网页"),
		makeToolDef("send_file", "发送文件"),
		makeToolDef("open", "打开文件"),
		makeToolDef("craft_tool", "生成内容"),
		makeToolDef("browser", "浏览器自动化工具"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	// Database query — none of the conditional keep intents should match.
	result := router.Route("我要查询数据库", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	// All conditional tools should be absent.
	for _, name := range []string{"ssh", "web_search", "send_file", "open", "craft_tool", "browser"} {
		if resultNames[name] {
			names := make([]string, len(result))
			for i, r := range result {
				names[i] = ExtractToolName(r)
			}
			t.Errorf("conditional keep tool %q should NOT be in result for '我要查询数据库', got: %v", name, names)
		}
	}
}

// TestRouter_BrowserSemanticConfirm_RejectsFalsePositive verifies that browser
// tools are NOT activated when keywords like "打开"+"页面" match but the
// IntentClassifier determines the actual intent is coding (not browser).
// This is the core fix for the "Browser:" prefix hallucination bug.
func TestRouter_BrowserSemanticConfirm_RejectsFalsePositive(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	// Set up IntentClassifier with embedding model.
	emb := loadTestEmbedder(t)
	if emb == nil {
		t.Skip("embedding model not available")
	}
	ic := NewIntentClassifier(emb)
	router.SetIntentClassifier(ic)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "浏览器自动化工具"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	// "开发一个打飞机游戏，页面上直接打开即玩" — weak keywords "打开"+"页面"
	// match the browser rule, but the intent is clearly coding, not browser.
	// Note: no strong keyword like "浏览器" — only the weak page+action combo.
	result := router.Route("开发一个打飞机游戏，页面上直接打开即玩，有飞机和子弹", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	if resultNames["browser"] {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("browser tool should NOT be in result for coding intent (game dev), got: %v", names)
	}
}

// TestRouter_BrowserSemanticConfirm_AcceptsTruePositive verifies that browser
// tools ARE activated when both keywords and IntentClassifier agree on browser intent.
func TestRouter_BrowserSemanticConfirm_AcceptsTruePositive(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	emb := loadTestEmbedder(t)
	if emb == nil {
		t.Skip("embedding model not available")
	}
	ic := NewIntentClassifier(emb)
	router.SetIntentClassifier(ic)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "浏览器自动化工具"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	// Genuine browser intent — strong keyword "浏览器" directly matches.
	result := router.Route("打开浏览器帮我在网页上点击购买按钮", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	if !resultNames["browser"] {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("browser tool should be in result for genuine browser intent, got: %v", names)
	}
}

// TestRouter_BrowserSemanticConfirm_FallbackWithoutClassifier verifies that
// when no IntentClassifier is available, browser tools fall back to keyword
// matching (backward compatible behavior).
func TestRouter_BrowserSemanticConfirm_FallbackWithoutClassifier(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen) // No IntentClassifier set.

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "浏览器自动化工具"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	// Without IntentClassifier, keyword match should still work (fallback).
	result := router.Route("打开浏览器访问百度网站", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	if !resultNames["browser"] {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("browser should be in result when no classifier (fallback), got: %v", names)
	}
}

func TestRouter_Route_ConditionallyKeepsSSHForSSHIntent(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("ssh", "通过 SSH 连接服务器并执行命令"))
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	if len(tools) <= MaxToolBudget {
		t.Fatalf("need more than %d tools to test routing, got %d", MaxToolBudget, len(tools))
	}

	runCase := func(message string, wantSSH bool) {
		result := router.Route(message, tools)
		found := false
		for _, r := range result {
			if ExtractToolName(r) == "ssh" {
				found = true
				break
			}
		}
		if found != wantSSH {
			names := make([]string, len(result))
			for i, r := range result {
				names[i] = ExtractToolName(r)
			}
			t.Fatalf("ssh presence for %q = %v, want %v; got: %v", message, found, wantSSH, names)
		}
	}

	runCase("登录 4090 服务器，host home.rapidai.tech 端口 33", true)
	router.ResetSession() // Clear session-pinned tools between independent test cases.
	runCase("我要查询数据库", false)
}

func TestDynamicToolBuilder_BM25(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "bash", Description: "run shell", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "read_file", Description: "read a file", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "db_query", Description: "执行数据库 SQL 查询", Category: CategoryNonCode, Tags: []string{"database", "sql"}})
	reg.Register(RegisteredTool{Name: "git_push", Description: "推送代码到远程仓库", Category: CategoryNonCode, Tags: []string{"git", "vcs"}})
	reg.Register(RegisteredTool{Name: "deploy", Description: "部署服务", Category: CategoryNonCode, Tags: []string{"deploy"}})
	// Add enough tools to trigger filtering.
	for i := 0; i < 20; i++ {
		reg.Register(RegisteredTool{
			Name:        fmt.Sprintf("filler_%d", i),
			Description: fmt.Sprintf("filler tool number %d", i),
			Category:    CategoryNonCode,
		})
	}

	builder := NewDynamicToolBuilder(reg)
	result := builder.Build("数据库查询")

	// db_query should be in the result.
	found := false
	for _, def := range result {
		if ExtractToolName(def) == "db_query" {
			found = true
			break
		}
	}
	if !found {
		t.Error("db_query should be selected for '数据库查询'")
	}
}
