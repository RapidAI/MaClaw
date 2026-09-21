// Package surfaceeval is the offline surface-selection evaluator for the
// tool-routing improvement plan (docs/design/tool-routing-improvement-plan-zh.md
// §6 Phase 0 item 2, docs/design/tool-routing-phase0-baseline-zh.md §3). It is
// the deterministic half of the Phase-0 runtime evaluation capability: per
// sample it runs the legacy text router (corelib/tool.Router.RouteWithOptions)
// headlessly and measures whether the selected tool surface contains the tools
// a task needs (recall), how large the surface is (context-cost proxy), and
// how that compares against the "render everything" full-catalog baseline.
//
// This harness makes NO LLM calls and NO network calls: no embedder, no
// reranker, no skill provider, no recommender and no live Unified Intent
// Classifier (UIC) is attached. LLM-in-the-loop task-success evaluation
// (whether the model actually completes the task with the routed surface)
// is a separate, future component and is intentionally out of scope here.
//
// The one exception is the deterministic UIC cache stub: a sample may carry
// an optional "simulated_intent" (see below). When present, the harness
// attaches a real *intent.UnifiedIntentClassifier built on a NoopEmbedder
// with no LLM callbacks, with the simulated classification pre-seeded into
// its per-message cache via StoreRecoveredClassification — exactly the
// cache surface the live router reads through ClassifyCached. RouteWithOptions
// then activates conditional tools from that cached result. Samples without
// simulated_intent run with no UIC attached (or a UIC with nothing cached),
// so fail-closed conditional tools stay closed. No classification is ever
// computed: nothing is embedded and no tree/LLM call is made.
//
// Limitations that follow from the headless, stub-only UIC construction,
// and from the closed legacy adapter catalog:
//
//   - UIC-activated conditional tools fire ONLY when the sample declares a
//     simulated_intent. Without one, fail-closed conditional tools (ssh,
//     browser, screenshot, record_audio, craft_tool, mis_data, IM delivery,
//     knowledge writers) are filtered out of the candidate pool regardless
//     of wording, so such samples report recall 0 by construction. That is
//     an honest measurement of the no-UIC legacy fallback path, not a bug.
//   - Candidate selection additionally requires a live reviewed provision in
//     corelib/tool/legacy_adapter_catalog.go. The reviewed lowercase aliases
//     "glob"→Glob and "grep"→ripgrep resolve at the provision lookup
//     boundary; any other catalog name without a provision can never be
//     selected by RouteWithOptions, and "manage_skill" is a legacy dynamic
//     gateway that is stripped before routing.
//   - Retrieval text is name+description+tags plus the curated bilingual
//     BuiltinEnrichments aliases (corelib/tool/enrichment.go). Well-known
//     lookup tools (web_search, web_fetch, knowledge_search, glob, grep)
//     carry alias terms so cross-language queries (English query vs Chinese
//     description and vice versa) still retrieve them.
//   - The bootstrap tools (task, async_wait, compress_context) and the
//     compatibility fallback surface (bash, read_file, ripgrep, edit_file,
//     discover_tool) are always part of the selected surface whenever they
//     are present in the catalog.
//
// Dataset format (one JSON file, see data/baseline-v1.json):
//
//	{
//	  "version": 1,                            // schema version, must be 1
//	  "tools": [                               // optional; built-in default
//	    {"name": "ssh", "description": "...", "tags": ["remote"]}
//	  ],
//	  "samples": [
//	    {
//	      "id": "ssh-connect-1",               // stable identity
//	      "user_text": "帮我连上服务器 10.0.0.1",
//	      "expect_tools": ["ssh"],             // must name catalog tools;
//	                                             // empty = pure cost sample
//	      "categories": ["conditional_activation"] // free-form metadata,
//	                                             // echoed into results and
//	                                             // the per-category recall
//	      "simulated_intent": {                  // optional; when present the
//	        "label": "ssh",                      // harness attaches a UIC cache
//	        "confidence": 0.9,                   // stub (see package doc) that
//	        "tool_names": ["ssh"]                // returns this classification.
//	      }                                      // label must be a known intent
//	                                             // label, confidence in [0,1],
//	                                             // tool_names in the catalog
//	    }
//	  ]
//	}
//
// Metrics per sample:
//
//	recall             |expect ∩ selected| / |expect|; 1.0 when expect is
//	                   empty. Empty-expect samples are cost-measurement only
//	                   and are excluded from all recall averages.
//	surface_size       len(selected)
//	est_surface_tokens sum over selected tools of
//	                   round((len(name)+len(description))/4) — a cheap context
//	                   proxy only. String length is measured in bytes, so
//	                   Chinese descriptions count ~3 bytes per character;
//	                   treat absolute numbers as rough estimates, not tokenizer
//	                   truth.
//	baseline_*         the same cost metrics computed over the FULL catalog
//	                   surface (no routing); cost_savings = 1 - est/baseline.
package surfaceeval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// DatasetVersion is the only supported dataset schema version.
const DatasetVersion = 1

// Dataset is the top-level document of one evaluation file.
type Dataset struct {
	Version int           `json:"version"`
	Tools   []CatalogTool `json:"tools,omitempty"`
	Samples []Sample      `json:"samples"`
}

// CatalogTool is one catalog entry. When the dataset file omits "tools",
// DefaultCatalog is used instead.
type CatalogTool struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

// Sample is one evaluation case. See the package doc for the format.
type Sample struct {
	ID          string   `json:"id"`
	UserText    string   `json:"user_text"`
	ExpectTools []string `json:"expect_tools"`
	Categories  []string `json:"categories,omitempty"`
	// SimulatedIntent, when non-nil, makes the harness attach a UIC cache
	// stub that returns this classification for the sample's user_text.
	// Absent means no UIC: conditional tools stay fail-closed.
	SimulatedIntent *SimulatedIntent `json:"simulated_intent,omitempty"`
}

// SimulatedIntent is the dataset-declared classification a sample's UIC
// stub returns. It mirrors the fields of intent.ClassificationResult that
// the router consumes for conditional-tool activation.
type SimulatedIntent struct {
	Label      string   `json:"label"`
	Confidence float64  `json:"confidence"`
	ToolNames  []string `json:"tool_names,omitempty"`
}

// SampleResult holds every per-sample metric plus the selected surface.
type SampleResult struct {
	ID          string   `json:"id"`
	UserText    string   `json:"user_text"`
	Categories  []string `json:"categories,omitempty"`
	ExpectTools []string `json:"expect_tools,omitempty"`

	SelectedTools []string `json:"selected_tools"`
	// MissingTools is expect_tools minus the selected surface.
	MissingTools []string `json:"missing_tools,omitempty"`

	// RecallEligible is false when expect_tools is empty; such samples are
	// excluded from every recall average.
	RecallEligible bool    `json:"recall_eligible"`
	Recall         float64 `json:"recall"`

	SurfaceSize      int `json:"surface_size"`
	EstSurfaceTokens int `json:"est_surface_tokens"`

	// Baseline metrics for the full-catalog surface (no routing).
	BaselineSurfaceSize int `json:"baseline_surface_size"`
	BaselineEstTokens   int `json:"baseline_est_tokens"`
	// CostSavings is 1 - EstSurfaceTokens/BaselineEstTokens; 0 when the
	// baseline has no estimated tokens.
	CostSavings float64 `json:"cost_savings"`
}

// CategoryRecallStat is the per-category recall breakdown over the samples
// that carry the category and have a non-empty expect_tools list.
type CategoryRecallStat struct {
	Category   string  `json:"category"`
	Samples    int     `json:"samples"`
	MeanRecall float64 `json:"mean_recall"`
}

// Report aggregates the per-sample results of one Run.
type Report struct {
	CatalogSize             int     `json:"catalog_size"`
	SamplesRun              int     `json:"samples_run"`
	RecallEligible          int     `json:"recall_eligible"`
	MeanRecall              float64 `json:"mean_recall"`
	MinRecall               float64 `json:"min_recall"`
	MeanSurfaceSize         float64 `json:"mean_surface_size"`
	MeanBaselineSurfaceSize float64 `json:"mean_baseline_surface_size"`
	TotalEstTokens          int     `json:"total_est_tokens"`
	TotalBaselineEstTokens  int     `json:"total_baseline_est_tokens"`
	// TokenSavings is 1 - TotalEstTokens/TotalBaselineEstTokens.
	TokenSavings   float64                       `json:"token_savings"`
	CategoryRecall map[string]CategoryRecallStat `json:"category_recall"`
	Results        []SampleResult                `json:"results"`
}

// Failures returns the results of all recall-eligible samples whose recall
// is below 1.0, i.e. the samples whose expected tools were not fully covered
// by the routed surface.
func (r *Report) Failures() []SampleResult {
	var out []SampleResult
	for _, res := range r.Results {
		if res.RecallEligible && res.Recall < 1.0 {
			out = append(out, res)
		}
	}
	return out
}

// RunFile loads a dataset from path, validates it strictly and runs the
// evaluation. Any parse or validation problem is returned as an error; a
// successfully returned Report always covers every sample.
func RunFile(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("surfaceeval: read dataset %q: %w", path, err)
	}
	var ds Dataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("surfaceeval: parse dataset %q: %w", path, err)
	}
	if err := validate(&ds); err != nil {
		return nil, fmt.Errorf("surfaceeval: invalid dataset %q: %w", path, err)
	}
	return Run(&ds), nil
}

// validate rejects datasets that would produce misleading reports: wrong
// schema version, duplicate or empty sample ids, empty user text,
// expect_tools names that do not exist in the effective catalog, or a
// malformed simulated_intent (unknown label, confidence outside [0,1], or
// tool_names that do not exist in the effective catalog).
func validate(ds *Dataset) error {
	if ds == nil {
		return fmt.Errorf("dataset is nil")
	}
	if ds.Version != DatasetVersion {
		return fmt.Errorf("unsupported version %d (want %d)", ds.Version, DatasetVersion)
	}
	catalog := ds.Tools
	if len(catalog) == 0 {
		catalog = DefaultCatalog()
	}
	names := make(map[string]bool, len(catalog))
	for _, t := range catalog {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return fmt.Errorf("catalog entry with empty name")
		}
		if names[name] {
			return fmt.Errorf("duplicate catalog tool %q", name)
		}
		names[name] = true
	}
	seen := make(map[string]bool, len(ds.Samples))
	for i, s := range ds.Samples {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("sample %d has empty id", i)
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate sample id %q", s.ID)
		}
		seen[s.ID] = true
		if strings.TrimSpace(s.UserText) == "" {
			return fmt.Errorf("sample %q has empty user_text", s.ID)
		}
		for _, want := range s.ExpectTools {
			if !names[want] {
				return fmt.Errorf("sample %q expects unknown tool %q (not in catalog)", s.ID, want)
			}
		}
		if sim := s.SimulatedIntent; sim != nil {
			if !knownIntentLabels[sim.Label] {
				return fmt.Errorf("sample %q simulated_intent has unknown label %q", s.ID, sim.Label)
			}
			if math.IsNaN(sim.Confidence) || sim.Confidence < 0 || sim.Confidence > 1 {
				return fmt.Errorf("sample %q simulated_intent confidence %.3f outside [0,1]", s.ID, sim.Confidence)
			}
			for _, toolName := range sim.ToolNames {
				if !names[toolName] {
					return fmt.Errorf("sample %q simulated_intent names unknown tool %q (not in catalog)", s.ID, toolName)
				}
			}
		}
	}
	return nil
}

// knownIntentLabels is the set of valid simulated_intent labels: every label
// the intent package declares (corelib/intent.AllLabels).
var knownIntentLabels = func() map[string]bool {
	labels := intent.AllLabels()
	m := make(map[string]bool, len(labels))
	for _, l := range labels {
		m[string(l)] = true
	}
	return m
}()

// Run evaluates every sample of ds with a freshly built headless router and
// returns the aggregated report. ds must have been validated (RunFile does
// this); Run on an unvalidated dataset may produce empty metrics. Samples
// with a simulated_intent run with a UIC cache stub attached (see
// buildStubUIC); all other samples run with no UIC.
//
// Run is an offline measurement: it disables corelib/tool's global routing
// telemetry for its duration (restored on return) so replayed samples never
// mutate the live counters or schedule durable writes against the real
// routing.json.
func Run(ds *Dataset) *Report {
	prev := tool.RecordRoutingStats
	tool.RecordRoutingStats = false
	defer func() { tool.RecordRoutingStats = prev }()

	catalog := ds.Tools
	if len(catalog) == 0 {
		catalog = DefaultCatalog()
	}
	registry := buildRegistry(catalog)
	defs := buildDefinitions(registry)
	router := tool.NewRouter(nil)
	router.SetRegistry(registry)

	baselineSize, baselineTokens := surfaceCost(catalog, catalog)

	report := &Report{
		CatalogSize:    len(catalog),
		CategoryRecall: map[string]CategoryRecallStat{},
	}
	report.Results = make([]SampleResult, 0, len(ds.Samples))
	for _, sample := range ds.Samples {
		router.SetUnifiedClassifier(buildStubUIC(sample))
		report.Results = append(report.Results, runSample(router, defs, catalog, sample, baselineSize, baselineTokens))
	}
	aggregate(report)
	return report
}

// buildStubUIC returns the UIC the router consults for one sample, or nil
// when the sample declares no simulated_intent — the fail-closed default.
func buildStubUIC(sample Sample) *intent.UnifiedIntentClassifier {
	return BuildStubUIC(sample.UserText, sample.SimulatedIntent)
}

// BuildStubUIC returns the UIC the router consults for one unit of replay
// input, or nil when sim is nil — the fail-closed default. The stub is a
// real *intent.UnifiedIntentClassifier on a NoopEmbedder with no LLM
// callbacks; the simulated classification is pre-seeded into its per-message
// cache (StoreRecoveredClassification), which is exactly the cache
// RouteWithOptions reads via ClassifyCached. Nothing is embedded and no
// tree/LLM call is ever made, so the stub is deterministic and offline.
// Exported for sibling offline harnesses (corelib/tool/taskeval).
func BuildStubUIC(userText string, sim *SimulatedIntent) *intent.UnifiedIntentClassifier {
	if sim == nil {
		return nil
	}
	uic := intent.New(intent.Config{Embedder: embedding.NewNoopEmbedder()})
	uic.StoreRecoveredClassification(intent.MessageContext{Text: userText}, intent.ClassificationResult{
		Primary:    intent.IntentLabel(sim.Label),
		Confidence: sim.Confidence,
		Layer:      3, // tree-grade provenance, mirroring a real UIC verdict
		Reason:     "surfaceeval simulated_intent",
		ToolNames:  sim.ToolNames,
	})
	return uic
}

// SurfaceCost returns the size and estimated token count of the given tool
// surface; see the package doc for the estimation rule. Exported for
// sibling offline harnesses (corelib/tool/taskeval).
func SurfaceCost(surface []CatalogTool) (int, int) {
	return surfaceCost(surface, nil)
}

func runSample(router *tool.Router, defs []map[string]interface{}, catalog []CatalogTool, sample Sample, baselineSize, baselineTokens int) SampleResult {
	selectedDefs := router.RouteWithOptions(sample.UserText, defs, tool.RouteOptions{})
	selected := make([]string, 0, len(selectedDefs))
	for _, def := range selectedDefs {
		selected = append(selected, tool.ExtractToolName(def))
	}
	sort.Strings(selected)

	selectedSet := make(map[string]bool, len(selected))
	for _, name := range selected {
		selectedSet[name] = true
	}
	var missing []string
	hits := 0
	for _, want := range sample.ExpectTools {
		if selectedSet[want] {
			hits++
		} else {
			missing = append(missing, want)
		}
	}
	eligible := len(sample.ExpectTools) > 0
	recall := 1.0
	if eligible {
		recall = float64(hits) / float64(len(sample.ExpectTools))
	}

	selectedCatalog := make([]CatalogTool, 0, len(selected))
	byName := make(map[string]CatalogTool, len(catalog))
	for _, t := range catalog {
		byName[t.Name] = t
	}
	for _, name := range selected {
		if t, ok := byName[name]; ok {
			selectedCatalog = append(selectedCatalog, t)
		}
	}
	_, estTokens := surfaceCost(selectedCatalog, catalog)

	costSavings := 0.0
	if baselineTokens > 0 {
		costSavings = 1.0 - float64(estTokens)/float64(baselineTokens)
	}

	return SampleResult{
		ID:                  sample.ID,
		UserText:            sample.UserText,
		Categories:          sample.Categories,
		ExpectTools:         sample.ExpectTools,
		SelectedTools:       selected,
		MissingTools:        missing,
		RecallEligible:      eligible,
		Recall:              recall,
		SurfaceSize:         len(selected),
		EstSurfaceTokens:    estTokens,
		BaselineSurfaceSize: baselineSize,
		BaselineEstTokens:   baselineTokens,
		CostSavings:         costSavings,
	}
}

// aggregate folds the per-sample results into the report-level means and the
// per-category recall breakdown.
func aggregate(report *Report) {
	n := len(report.Results)
	if n == 0 {
		return
	}
	var recallSum, surfaceSum, baselineSum float64
	minRecall := 0.0
	catSum := map[string]float64{}
	catCount := map[string]int{}
	for _, res := range report.Results {
		surfaceSum += float64(res.SurfaceSize)
		baselineSum += float64(res.BaselineSurfaceSize)
		if !res.RecallEligible {
			continue
		}
		report.RecallEligible++
		recallSum += res.Recall
		if report.RecallEligible == 1 || res.Recall < minRecall {
			minRecall = res.Recall
		}
		for _, cat := range res.Categories {
			catSum[cat] += res.Recall
			catCount[cat]++
		}
	}
	report.SamplesRun = n
	report.MeanSurfaceSize = surfaceSum / float64(n)
	report.MeanBaselineSurfaceSize = baselineSum / float64(n)
	if report.RecallEligible > 0 {
		report.MeanRecall = recallSum / float64(report.RecallEligible)
		report.MinRecall = minRecall
	}
	for cat, count := range catCount {
		report.CategoryRecall[cat] = CategoryRecallStat{
			Category:   cat,
			Samples:    count,
			MeanRecall: catSum[cat] / float64(count),
		}
	}
	for _, res := range report.Results {
		report.TotalEstTokens += res.EstSurfaceTokens
		report.TotalBaselineEstTokens += res.BaselineEstTokens
	}
	if report.TotalBaselineEstTokens > 0 {
		report.TokenSavings = 1.0 - float64(report.TotalEstTokens)/float64(report.TotalBaselineEstTokens)
	}
}

// surfaceCost returns the size and estimated token count of the given tool
// surface. The baseline argument is unused today but keeps the call sites
// symmetric; the estimate is documented as a cheap proxy in the package doc.
func surfaceCost(surface, _ []CatalogTool) (int, int) {
	tokens := 0
	for _, t := range surface {
		tokens += int(math.Round(float64(len(t.Name)+len(t.Description)) / 4.0))
	}
	return len(surface), tokens
}

// buildRegistry registers every catalog entry through the standard
// corelib/tool registration path, exactly like a host registry would.
func buildRegistry(catalog []CatalogTool) *tool.Registry {
	registry := tool.NewRegistry()
	for _, entry := range catalog {
		_ = registry.Register(tool.RegisteredTool{
			Name:        entry.Name,
			Description: entry.Description,
			Tags:        entry.Tags,
			Category:    tool.CategoryBuiltin,
			Status:      tool.StatusAvailable,
		})
	}
	return registry
}

// buildDefinitions renders the registry into OpenAI function-calling
// definition maps, the same projection hosts feed into RouteWithOptions.
func buildDefinitions(registry *tool.Registry) []map[string]interface{} {
	tools := registry.ListAvailable()
	defs := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, tool.RegisteredToolToDef(t))
	}
	return defs
}

// DefaultCatalog is the built-in fallback catalog of plausible MaClaw tool
// surfaces, used when a dataset file does not declare its own "tools" array.
// Descriptions are deliberately realistic: BM25 retrieval sees exactly this
// text. Lowercase glob/grep resolve through the reviewed provision aliases
// (Glob/ripgrep), and the manage_skill gateway is stripped before routing
// (see package doc limitations).
func DefaultCatalog() []CatalogTool {
	return []CatalogTool{
		{Name: "ask_user", Description: "向用户提问以澄清需求或确认关键操作", Tags: []string{"interaction"}},
		{Name: "bash", Description: "在本地工作区执行 shell 命令，例如运行构建、测试或脚本", Tags: []string{"shell", "workspace"}},
		{Name: "browser", Description: "控制浏览器完成网页访问、点击、填表与抓取等自动化操作", Tags: []string{"browser", "web"}},
		{Name: "craft_tool", Description: "创建或定制一个新的工具定义并注册到工具目录", Tags: []string{"meta"}},
		{Name: "current_datetime", Description: "获取当前的日期和时间信息", Tags: []string{"time"}},
		{Name: "database", Description: "连接业务数据库执行查询并返回结构化结果", Tags: []string{"data"}},
		{Name: "delegate_task", Description: "把子任务委派给一个专门的子代理执行并收集结果", Tags: []string{"task"}},
		{Name: "discover_tool", Description: "在工具目录中搜索并发现当前未暴露的可用工具", Tags: []string{"catalog"}},
		{Name: "download_file", Description: "从指定的 URL 下载文件到本地目录", Tags: []string{"web", "file"}},
		{Name: "edit_file", Description: "对指定文件做精确的局部编辑修改", Tags: []string{"file", "edit"}},
		{Name: "generate_pdf", Description: "把文档或报告渲染生成 PDF 文件", Tags: []string{"document"}},
		{Name: "glob", Description: "按照文件名模式在工作区内查找匹配的文件", Tags: []string{"file", "search"}},
		{Name: "grep", Description: "在文件内容中按正则模式搜索匹配的文本行", Tags: []string{"search"}},
		{Name: "knowledge_search", Description: "在本地知识库中检索用户保存过的资料、笔记与记录", Tags: []string{"knowledge"}},
		{Name: "list_mcp_tools", Description: "列出当前已配置的 MCP 服务器及其提供的工具", Tags: []string{"catalog"}},
		{Name: "manage_skill", Description: "管理已安装的技能：查看、运行或配置技能", Tags: []string{"skill"}},
		{Name: "memory", Description: "读写长期记忆，保存用户偏好与会话要点", Tags: []string{"memory"}},
		{Name: "read_file", Description: "读取本地文件的内容，支持指定行范围", Tags: []string{"file", "read"}},
		{Name: "record_audio", Description: "录制麦克风音频并返回音频数据", Tags: []string{"audio"}},
		{Name: "screenshot", Description: "截取当前桌面的屏幕图像", Tags: []string{"desktop", "capture"}},
		{Name: "ssh", Description: "通过 SSH 连接到远程服务器并执行命令", Tags: []string{"remote", "shell"}},
		{Name: "task", Description: "创建并跟踪一个异步后台任务，等待其完成或获取输出", Tags: []string{"task"}},
		{Name: "web_fetch", Description: "抓取指定 URL 的网页内容并返回正文", Tags: []string{"web"}},
		{Name: "web_search", Description: "在互联网上搜索信息并返回检索结果摘要", Tags: []string{"web", "search"}},
		{Name: "write_file", Description: "把内容写入一个新文件或覆盖已有文件", Tags: []string{"file", "write"}},
	}
}
