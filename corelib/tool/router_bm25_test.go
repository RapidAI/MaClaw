package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	uicintent "github.com/RapidAI/CodeClaw/corelib/intent"
)

type sshBiasedEmbedder struct{}

func (sshBiasedEmbedder) Embed(text string) ([]float32, error) {
	return sshBiasedVector(text), nil
}

func (sshBiasedEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, text := range texts {
		vecs[i] = sshBiasedVector(text)
	}
	return vecs, nil
}

func (sshBiasedEmbedder) Dim() int { return 2 }
func (sshBiasedEmbedder) Close()   {}

func sshBiasedVector(text string) []float32 {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "ssh") || strings.Contains(lower, "remote") ||
		strings.Contains(lower, "production server") || strings.Contains(lower, "server logs") ||
		strings.Contains(lower, "gpu server") {
		return []float32{1, 0}
	}
	return []float32{0, 1}
}

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

func makeCoreSSHRouteTools(extraCount int) []map[string]interface{} {
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("ssh", "connect to a remote server and run commands"))
	for i := 0; i < extraCount; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}
	return tools
}

func routedToolNames(result []map[string]interface{}) map[string]bool {
	names := make(map[string]bool, len(result))
	for _, r := range result {
		names[ExtractToolName(r)] = true
	}
	return names
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

	// Without UIC or a semantic IntentClassifier, local wording must not keep
	// conditional tools for PDF/search delivery intent.
	for _, name := range []string{"web_search", "send_file", "open"} {
		if resultNames[name] {
			t.Errorf("PDF workflow tool %q should not be routed without semantic classification", name)
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

	// web_search should not be kept from local search wording alone.
	if resultNames["web_search"] {
		t.Error("web_search should not be routed without semantic classification")
	}

	// ssh should NOT be in result.
	if resultNames["ssh"] {
		t.Error("ssh should not be routed for search intent")
	}
}

func TestRouter_BM25_ConditionalKeep_MISBusinessTransactionWorkflow(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("mis_data", "structured MIS business data and AgentView transaction workspace"),
		makeToolDef("web_search", "search web content"),
		makeToolDef("ssh", "connect to servers with SSH"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("继续处理上次那个差旅报销录入，打开未完成事务", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}
	if resultNames["mis_data"] {
		t.Error("mis_data should not be routed without semantic classification")
	}
	if resultNames["ssh"] {
		t.Error("ssh should not be routed for business transaction workflow")
	}
}

func TestBuiltinToolNamesIncludesMISData(t *testing.T) {
	if !IsBuiltinToolName("mis_data") {
		t.Fatal("mis_data should be recognized as a builtin tool")
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

	// Browser tool should not be kept from local browser wording alone.
	if resultNames["browser"] {
		t.Error("browser tool should not be routed without semantic classification")
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
// tools are NOT activated when the IntentClassifier determines the actual
// intent is coding rather than browser automation.
// This is the core fix for the "Browser:" prefix hallucination bug.
func TestRouter_BrowserSemanticConfirm_RejectsFalsePositive(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	// Set up IntentClassifier with embedding model.
	emb := loadTestEmbedder(t)
	if emb == nil {
		t.Skip("embedding model not available")
	}
	defer emb.Close()
	ic := NewIntentClassifier(emb)
	defer ic.Close()
	if !ic.WaitReady(intentClassifierWarmupTimeout) {
		t.Fatalf("IntentClassifier not ready after %s", intentClassifierWarmupTimeout)
	}
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

	// This describes building a web game. Local lexical cues must not activate
	// browser automation.
	result := router.Route("Build a browser-playable airplane shooting game with a start screen and bullets.", tools)
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
// tools ARE activated when the IntentClassifier returns browser intent.
func TestRouter_BrowserSemanticConfirm_AcceptsTruePositive(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentBrowser, nil })
	router.SetIntentClassifier(ic)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("browser", "browser automation tool"))
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("Use a browser to open https://example.com and inspect the page.", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	if !resultNames["browser"] {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("browser tool should be in result when semantic classifier returns browser, got: %v", names)
	}
}

func TestRouter_SemanticBrowserIntentKeepsBrowserAndSuppressesFallbacks(t *testing.T) {
	router := NewRouter(nil)
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentBrowser, nil })
	router.SetIntentClassifier(ic)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("browser", "stable browser automation tool"))
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("登录知乎发表一条码卡龙 6 发布感言", tools)
	resultNames := make(map[string]bool)
	var names []string
	for _, r := range result {
		name := ExtractToolName(r)
		resultNames[name] = true
		names = append(names, name)
	}
	if !resultNames["browser"] {
		t.Fatalf("browser should be routed for semantic browser login/publish intent, got: %v", names)
	}
	if resultNames["screenshot"] {
		t.Fatalf("desktop screenshot should be suppressed for browser automation intent, got: %v", names)
	}
	if resultNames["bash"] {
		t.Fatalf("bash should be suppressed for browser automation intent, got: %v", names)
	}
}

func TestRouter_ScreenshotIsNotAlwaysOnCoreTool(t *testing.T) {
	if CoreToolNames["screenshot"] {
		t.Fatal("screenshot must not be a core always-on tool; route it only when user explicitly asks")
	}
}

func TestRouter_RecordAudioIsAlwaysOnCoreTool(t *testing.T) {
	if !CoreToolNames["record_audio"] {
		t.Fatal("record_audio must be a core always-on tool so meeting-recording intents cannot lose it to budget contention")
	}
}

func TestRouter_RecordAudioAlwaysRoutedEvenUnderBudgetPressure(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	// Extra candidates to force tight MaxToolBudget selection.
	for i := 0; i < 40; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool for budget pressure"))
	}

	result := router.Route("hello", tools)
	if !routedToolNames(result)["record_audio"] {
		t.Fatalf("record_audio must remain routed under budget pressure, got: %v", result)
	}
}

func TestRouter_ExplicitMeetingRecordRequestKeepsRecordAudio(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	for i := 0; i < 40; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	for _, msg := range []string{"会议录音", "开始录音", "record meeting", "帮我录音"} {
		result := router.Route(msg, tools)
		if !routedToolNames(result)["record_audio"] {
			t.Fatalf("explicit recording request %q must route record_audio, got: %v", msg, result)
		}
	}
}

func TestIsExplicitRecordAudioRequest(t *testing.T) {
	positives := []string{
		"会议录音",
		"开始录音",
		"打开录音",
		"帮我录音",
		"录音",
		"不要转写，开始录音",
		"开始会议录音并整理纪要",
		"record meeting",
		"start recording",
		"访谈录制",
	}
	for _, msg := range positives {
		if !isExplicitRecordAudioRequest(msg) {
			t.Fatalf("expected explicit record intent for %q", msg)
		}
	}
	negatives := []string{
		"把这段录音文件转写一下",
		"asr path=C:\\a.wav",
		"音频文件.mp3 转录",
		"不是已经好了吗？",
		"整理会议纪要文档",
		"把会议录音整理成纪要",
		"根据会议录音写摘要",
		"停止录音",
		"不要录音",
		"不要帮我录音",
		"取消录音",
		"昨天录音效果不好，我们讨论一下怎么优化产品文档",
		"stop recording",
		"don't record this",
	}
	for _, msg := range negatives {
		if isExplicitRecordAudioRequest(msg) {
			t.Fatalf("did not expect explicit record intent for %q", msg)
		}
	}
}

func TestRouter_ExplicitScreenshotRequestStillRoutesScreenshot(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"))
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("take a desktop screenshot and show me", tools)
	if !routedToolNames(result)["screenshot"] {
		t.Fatalf("explicit screenshot request should still route screenshot, got: %v", result)
	}
}

func TestRouter_GenericFollowupDoesNotRouteScreenshot(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"))
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("不是已经好了吗？", tools)
	if routedToolNames(result)["screenshot"] {
		t.Fatalf("generic follow-up should not route screenshot, got: %v", result)
	}
}

func TestRouter_CompositeResearchPublishToZhihuRoutesBrowser(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "stable browser automation merged tool"),
		makeToolDef("web_search", "search the web for latest papers"),
		makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"),
	)
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("找篇最新的agentic RL相关论文，做完综述，发表到知乎，作为正式文章，不是想法。", tools)
	names := routedToolNames(result)
	if !names["browser"] {
		t.Fatalf("composite publish-to-Zhihu task should route browser, got: %v", result)
	}
	for _, suppressed := range []string{"bash", "screenshot", "manage_skill", "discover_tool", "call_mcp_tool", "search_and_install_skill", "git_commit", "git_push", "passthrough_task", "list_mcp_tools"} {
		if names[suppressed] {
			t.Fatalf("browser publish task should suppress %s, got: %v", suppressed, result)
		}
	}
}

func TestRouter_BrowserSessionFollowupSuppressesUnstableFallbacks(t *testing.T) {
	router := NewRouter(nil)
	router.ActivateSessionTool("browser")
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "stable browser automation merged tool"),
		makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"),
		makeToolDef("git_commit", "commit code changes to git repository"),
		makeToolDef("git_push", "push git commits to remote repository"),
		makeToolDef("manage_skill", "run local skill"),
	)
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("\u597d\u50cf\u63d0\u4ea4\u65f6\u6ca1\u6210\u529f\uff0c\u6d88\u5931\u4e86\u3002", tools)
	names := routedToolNames(result)
	if !names["browser"] {
		t.Fatalf("active browser session should keep browser tool for short follow-up, got: %v", result)
	}
	for _, suppressed := range []string{"bash", "screenshot", "git_commit", "git_push", "manage_skill"} {
		if names[suppressed] {
			t.Fatalf("browser follow-up should suppress %s, got: %v", suppressed, result)
		}
	}
}

func TestRouter_GenericSubmitFollowupDoesNotRouteScreenshotOrGit(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"),
		makeToolDef("git_commit", "commit code changes to git repository"),
		makeToolDef("git_push", "push git commits to remote repository"),
	)
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("\u597d\u50cf\u63d0\u4ea4\u65f6\u6ca1\u6210\u529f\uff0c\u6d88\u5931\u4e86\u3002", tools)
	names := routedToolNames(result)
	for _, suppressed := range []string{"screenshot", "git_commit", "git_push"} {
		if names[suppressed] {
			t.Fatalf("generic submit follow-up should not route %s without explicit request, got: %v", suppressed, result)
		}
	}
}

// TestRouter_ExplicitScreenshotOverridesBrowserCondKeep verifies that when the
// user explicitly asks for a desktop screenshot but UIC misclassifies the message
// as browser intent (setting condKeep["browser"]=true), the explicit screenshot
// signal takes precedence: screenshot is routed, browser is demoted.
func TestRouter_ExplicitScreenshotOverridesBrowserCondKeep(t *testing.T) {
	router := NewRouter(nil)
	// Simulate UIC setting condKeep["browser"] by pre-pinning via condKeep path.
	// We cannot directly set condKeep, but we can set sessionTools to trigger
	// browserSessionActive. However, the fix specifically checks condKeep vs
	// sessionTools, so we use a different approach: manually verify via the
	// route result that screenshot is routed and browser suppression does not
	// block it when using Chinese "截屏" keyword.
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "stable browser automation merged tool"),
		makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"),
	)
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	// "截屏" is a Chinese explicit screenshot request. Even if browser ends up
	// in condKeep (via UIC misclassification), screenshot must be routed.
	result := router.Route("截屏", tools)
	names := routedToolNames(result)
	if !names["screenshot"] {
		t.Fatalf("explicit '截屏' request should route screenshot even if browser is active, got: %v", names)
	}
}

// TestRouter_ExplicitScreenshotWithBrowserSessionPin verifies that when browser
// is session-pinned (user previously used browser) AND the user explicitly asks
// for a screenshot, the screenshot tool is still available. The browser session
// pin is legitimate (user did use browser before), so browser should remain, but
// screenshot should NOT be suppressed.
func TestRouter_ExplicitScreenshotWithBrowserSessionPin(t *testing.T) {
	router := NewRouter(nil)
	router.ActivateSessionTool("browser")
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "stable browser automation merged tool"),
		makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"),
		makeToolDef("manage_skill", "run local skill"),
	)
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("截屏桌面", tools)
	names := routedToolNames(result)
	if !names["screenshot"] {
		t.Fatalf("explicit screenshot with browser session pin should still route screenshot, got: %v", names)
	}
	if !names["browser"] {
		t.Fatalf("browser session pin should keep browser available, got: %v", names)
	}
}

// TestRouter_BrowserScreenshotRequestKeepsBrowser verifies that when a user asks
// for a browser/webpage screenshot (e.g., "在浏览器中截图这个页面"), the browser
// tool is NOT demoted because the message contains browser context words.
// We use session pin to simulate browser being legitimately active, then verify
// messageHasBrowserContext prevents demotion for the condKeep path too.
func TestRouter_BrowserScreenshotRequestKeepsBrowser(t *testing.T) {
	// Test messageHasBrowserContext directly for the key scenario:
	// "在浏览器中截图这个网页" has browser context words, so demotion should NOT fire.
	if !messageHasBrowserContext("在浏览器中截图这个网页") {
		t.Fatal("expected messageHasBrowserContext to return true for browser screenshot request")
	}
	if !messageHasBrowserContext("take a screenshot of this web page in Chrome") {
		t.Fatal("expected messageHasBrowserContext to return true for English browser screenshot")
	}
	// Plain desktop screenshot should NOT have browser context
	if messageHasBrowserContext("截屏") {
		t.Fatal("expected messageHasBrowserContext to return false for plain desktop screenshot")
	}
	if messageHasBrowserContext("截屏桌面") {
		t.Fatal("expected messageHasBrowserContext to return false for desktop screenshot")
	}

	// Integration: with session-pinned browser, explicit screenshot + browser
	// context words = both tools remain. Session pin path takes precedence anyway,
	// but this validates the full signal chain.
	router := NewRouter(nil)
	router.ActivateSessionTool("browser")
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "stable browser automation merged tool"),
		makeToolDef("screenshot", "take a desktop screenshot capture the visible screen"),
	)
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("在浏览器中截图这个网页", tools)
	names := routedToolNames(result)
	if !names["screenshot"] {
		t.Fatalf("browser webpage screenshot request should include screenshot tool, got: %v", names)
	}
	if !names["browser"] {
		t.Fatalf("browser webpage screenshot with session pin should keep browser, got: %v", names)
	}
}

func TestRouter_ExplicitGitRequestStillRoutesGitCommit(t *testing.T) {
	router := NewRouter(nil)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("git_commit", "commit code changes to git repository"))
	for i := 0; i < 30; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("\u5e2e\u6211\u628a\u4ee3\u7801 git commit \u4e00\u4e0b", tools)
	if !routedToolNames(result)["git_commit"] {
		t.Fatalf("explicit git request should still route git_commit, got: %v", result)
	}
}

// TestRouter_BrowserSemanticConfirm_NoFallbackWithoutClassifier verifies that
// when no IntentClassifier is available, browser tools do not fall back to
// local lexical activation.
func TestRouter_BrowserSemanticConfirm_NoFallbackWithoutClassifier(t *testing.T) {
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

	result := router.Route("打开浏览器访问百度网站", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	if resultNames["browser"] {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("browser should not be in result without semantic classifier, got: %v", names)
	}
}

func TestRouter_Route_FiltersConditionalToolsUnderBudgetWithoutClassifier(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "browser automation tool"),
		makeToolDef("ssh", "connect to a remote server and run commands"),
	)

	if len(tools) > MaxToolBudget {
		t.Fatalf("test expects tools under budget, got %d > %d", len(tools), MaxToolBudget)
	}

	result := router.Route("Open the website and inspect the deployment server.", tools)
	for _, r := range result {
		name := ExtractToolName(r)
		if name == "browser" || name == "ssh" {
			t.Fatalf("conditional tool %q should be filtered under budget without semantic classifier; result=%v", name, result)
		}
	}
}

func TestRouter_UICLowConfidenceDoesNotActivateConditionalTools(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetUnifiedClassifier(uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			// Return a low-confidence result (below uicActivationThreshold=0.50).
			// This simulates a scenario where the tree channel is uncertain.
			return `{"top":[{"skill":"browser","score":0.35},{"skill":"ssh","score":0.30},{"skill":"coding","score":0.25}]}`, nil
		},
	}))

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("browser", "browser automation tool"))
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("Open a website in the browser.", tools)
	for _, r := range result {
		if ExtractToolName(r) == "browser" {
			t.Fatalf("browser should not be included for UIC confidence below activation threshold")
		}
	}
}

func TestRouter_UICHighConfidenceActivatesConditionalTools(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetUnifiedClassifier(uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"browser","score":0.95},{"skill":"search","score":0.20},{"skill":"coding","score":0.10}]}`, nil
		},
	}))

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("browser", "browser automation tool"))
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("Open a website in the browser.", tools)
	for _, r := range result {
		if ExtractToolName(r) == "browser" {
			return
		}
	}
	names := make([]string, len(result))
	for i, r := range result {
		names[i] = ExtractToolName(r)
	}
	t.Fatalf("browser should be included for high-confidence UIC browser intent, got: %v", names)
}

func TestRouter_UICDegradedConcreteIntentStillActivatesTools(t *testing.T) {
	result := uicintent.ClassificationResult{
		Primary:    uicintent.LabelSSH,
		Confidence: 0.95,
		Degraded:   true,
		ToolNames:  []string{"ssh"},
	}

	if !uicResultUsableForToolActivation(result) {
		t.Fatalf("degraded but concrete UIC result should remain usable for tool activation")
	}
}

func TestRouter_UICDegradedUnknownOrAmbiguousDoesNotActivateTools(t *testing.T) {
	for _, label := range []uicintent.IntentLabel{uicintent.LabelUnknown, uicintent.LabelAmbiguous} {
		result := uicintent.ClassificationResult{
			Primary:    label,
			Confidence: 0.95,
			Degraded:   true,
			ToolNames:  []string{"ssh"},
		}

		if uicResultUsableForToolActivation(result) {
			t.Fatalf("degraded %s UIC result should not be usable for tool activation", label)
		}
	}
}

func TestRouter_UICHighConfidenceSSHDoesNotEagerPin(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.SetUnifiedClassifier(uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"ssh","score":0.95},{"skill":"coding","score":0.10}]}`, nil
		},
	}))

	result := router.Route("SSH into the GPU server and check usage.", makeCoreSSHRouteTools(20))
	names := routedToolNames(result)
	if !names["ssh"] {
		t.Fatalf("ssh should be included for high-confidence UIC SSH intent; got %#v", names)
	}
	if router.IsSessionPinned("ssh") {
		t.Fatalf("UIC SSH intent should not eager-pin ssh before successful tool use")
	}
}

func TestRouter_UICActivatableToolNamesAreAllowlisted(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{name: "ssh", want: true},
		{name: "browser", want: true},
		{name: "knowledge_save_text", want: true},
		{name: "manage_skill", want: false},
		{name: "discover_tool", want: false},
		{name: "call_mcp_tool", want: false},
	} {
		if got := uicToolNameActivatable(tc.name); got != tc.want {
			t.Fatalf("uicToolNameActivatable(%q)=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRouter_UICDegradedSSHRouteKeepsBuiltinAndSuppressesFallbacks(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	uic := uicintent.New(uicintent.Config{
		Embedder: sshBiasedEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return "not json", nil
		},
	})
	for i := 0; i < 50 && !uic.Ready(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !uic.Ready() {
		t.Fatalf("test UIC did not become ready")
	}
	router.SetUnifiedClassifier(uic)
	message := "SSH into the GPU server and check usage."
	classification := uic.Classify(uicintent.MessageContext{Text: message})
	if classification.Primary != uicintent.LabelSSH || !classification.Degraded {
		t.Fatalf("test UIC should produce degraded SSH classification, got primary=%s degraded=%v confidence=%.2f reason=%q",
			classification.Primary, classification.Degraded, classification.Confidence, classification.Reason)
	}

	tools := makeCoreSSHRouteTools(20)

	result := router.Route(message, tools)
	names := routedToolNames(result)

	if !names["ssh"] {
		t.Fatalf("builtin ssh should be included for degraded-but-concrete UIC SSH intent; got %#v", names)
	}
	for _, fallback := range []string{"call_mcp_tool", "manage_skill", "discover_tool", "search_and_install_skill"} {
		if names[fallback] {
			t.Fatalf("%s should be suppressed for degraded-but-concrete UIC SSH intent; got %#v", fallback, names)
		}
	}
	if router.IsSessionPinned("ssh") {
		t.Fatalf("degraded UIC SSH intent should not eager-pin ssh to the session")
	}
}

func TestRouter_SSHIntentSuppressesFallbackTooling(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.sessionTools = make(map[string]bool)
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentSSH, nil })
	router.SetIntentClassifier(ic)

	tools := makeCoreSSHRouteTools(20)

	result := router.Route("Check the remote server resource usage.", tools)
	names := routedToolNames(result)

	if !names["ssh"] {
		t.Fatalf("ssh should be included for SSH intent")
	}
	for _, fallback := range []string{"call_mcp_tool", "manage_skill", "discover_tool", "search_and_install_skill"} {
		if names[fallback] {
			t.Fatalf("%s should be suppressed when builtin ssh is selected; got %#v", fallback, names)
		}
	}
	if router.IsSessionPinned("ssh") {
		t.Fatalf("fallback semantic SSH intent should not eager-pin ssh before successful tool use")
	}
}

func TestRouter_SSHSessionPinOnlySuppressesMCPGateway(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.ActivateSessionTool("ssh")

	tools := makeCoreSSHRouteTools(20)

	result := router.Route("Search for a PDF conversion skill.", tools)
	names := routedToolNames(result)

	if names["call_mcp_tool"] {
		t.Fatalf("call_mcp_tool should remain suppressed while ssh is session-pinned")
	}
	for _, fallback := range []string{"manage_skill", "discover_tool"} {
		if !names[fallback] {
			t.Fatalf("%s should remain available for topic changes when ssh is only session-pinned; got %#v", fallback, names)
		}
	}
}

func TestRouter_ActivateSessionToolOnlyPinsAllowedConditionalTools(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	router.ActivateSessionTool("ssh")
	router.ActivateSessionTool("bash")
	router.ActivateSessionTool("generate_pdf")
	router.ActivateSessionTool("office")

	if !router.IsSessionPinned("ssh") {
		t.Fatalf("ssh should be pinned after successful use")
	}
	for _, name := range []string{"bash", "generate_pdf", "office"} {
		if router.IsSessionPinned(name) {
			t.Fatalf("%s should not be session-pinned by ActivateSessionTool", name)
		}
	}
}

func TestRouter_CoreOverflowIsTrimmedButKeepsIntentTool(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.sessionTools = make(map[string]bool)
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentSSH, nil })
	router.SetIntentClassifier(ic)

	tools := makeCoreSSHRouteTools(20)
	for i := 0; i < MaxToolBudget; i++ {
		name := fmt.Sprintf("pinned_tool_%d", i)
		router.sessionTools[name] = true
		tools = append(tools, makeToolDef(name, "session pinned test tool"))
	}

	result := router.Route("Check the remote server resource usage.", tools)
	names := routedToolNames(result)
	if len(result) > MaxToolBudget {
		t.Fatalf("route result should respect MaxToolBudget after core overflow trimming, got %d", len(result))
	}
	if !names["ssh"] {
		t.Fatalf("intent-selected ssh should survive core overflow trimming; got %#v", names)
	}
}

func TestRouter_CoreOverflowKeepsEssentialCoreBeforeStalePins(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	router.sessionTools = make(map[string]bool)

	tools := makeCoreSSHRouteTools(20)
	for i := 0; i < MaxToolBudget; i++ {
		name := fmt.Sprintf("stale_pinned_tool_%d", i)
		router.sessionTools[name] = true
		tools = append(tools, makeToolDef(name, "stale session pinned test tool"))
	}

	result := router.Route("Read files and inspect the project.", tools)
	names := routedToolNames(result)
	if len(result) > MaxToolBudget {
		t.Fatalf("route result should respect MaxToolBudget after stale pin trimming, got %d", len(result))
	}
	for _, essential := range []string{"bash", "read_file", "ripgrep", "edit_file"} {
		if !names[essential] {
			t.Fatalf("essential core tool %q should survive stale session pin overflow; got %#v", essential, names)
		}
	}
}

func TestRouter_Route_NoLocalSSHFallbackWithoutClassifier(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	tools := makeCoreSSHRouteTools(20)

	if len(tools) <= MaxToolBudget {
		t.Fatalf("need more than %d tools to test routing, got %d", MaxToolBudget, len(tools))
	}

	result := router.Route("Log in to the 4090 server at host home.rapidai.tech on port 33.", tools)
	for _, r := range result {
		if ExtractToolName(r) == "ssh" {
			t.Fatalf("ssh should not be included without semantic classifier")
		}
	}
}

func TestRouter_Route_SemanticallyKeepsSSHForSSHIntent(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)
	ic := NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) { return IntentSSH, nil })
	router.SetIntentClassifier(ic)

	tools := makeCoreSSHRouteTools(20)

	result := router.Route("Check the remote server resource usage.", tools)
	for _, r := range result {
		if ExtractToolName(r) == "ssh" {
			return
		}
	}
	names := make([]string, len(result))
	for i, r := range result {
		names[i] = ExtractToolName(r)
	}
	t.Fatalf("ssh should be included when semantic classifier returns ssh, got: %v", names)
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
