package embedding

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"
)

// intentAnchor defines a set of anchor texts for one intent category.
type intentAnchor struct {
	Name   string
	Texts  []string
	Vecs   [][]float32 // populated at test init
}

// testCase defines a user message with its expected intent.
type testCase struct {
	Input          string
	ExpectedIntent string // "coding", "ssh", "content", "chat", "browser", "none"
	Description    string // why this case matters
}

// buildAnchors returns the intent anchors for testing.
func buildAnchors() []intentAnchor {
	return []intentAnchor{
		{
			Name: "coding",
			Texts: []string{
				"帮我写一个程序",
				"开发一个应用",
				"修复这个bug",
				"重构代码",
				"实现一个功能",
				"写个Python脚本",
				"create a web application",
				"fix the login issue in the code",
				"开发一个贪吃蛇游戏",
				"帮我写一个爬虫",
				"设计一个数据库架构",
			},
		},
		{
			Name: "ssh",
			Texts: []string{
				"登录服务器查看GPU占用",
				"连接远程主机",
				"查看服务器日志",
				"重启nginx服务",
				"SSH到生产环境",
				"看一下线上机器的内存",
				"上传文件到服务器",
				"connect to the remote server",
			},
		},
		{
			Name: "content",
			Texts: []string{
				"翻译这篇文章",
				"整理这些资料",
				"总结这个文档",
				"帮我把这段英文翻译成中文",
				"把这个markdown转成更好的格式",
				"summarize this document",
				"translate this to Chinese",
				"帮我收集关于AI的资料",
			},
		},
		{
			Name: "chat",
			Texts: []string{
				"你好",
				"今天天气怎么样",
				"你是谁",
				"谢谢",
				"hello",
				"what can you do",
				"讲个笑话",
			},
		},
		{
			Name: "browser",
			Texts: []string{
				"打开浏览器访问百度",
				"用浏览器录制操作流程",
				"帮我在网页上点击登录按钮",
				"open chrome and navigate to google",
				"回放之前录制的浏览器操作",
			},
		},
	}
}

// buildTestCases returns all test cases grouped by category.
func buildTestCases() []testCase {
	return []testCase{
		// ============================================================
		// 1. 明确的编码意图 — 应该命中 coding
		// ============================================================
		{"帮我写一个贪吃蛇游戏", "coding", "明确编码请求"},
		{"开发一个自动抢票工具", "coding", "口语化编码请求，无关键词'代码'"},
		{"帮我搞个能自动抢票的东西", "coding", "非常口语化，测试语义理解"},
		{"实现一个用户注册登录功能", "coding", "功能开发"},
		{"写个脚本批量重命名文件", "coding", "脚本编写"},
		{"fix the authentication bug", "coding", "英文编码请求"},
		{"create a REST API for user management", "coding", "英文API开发"},
		{"帮我把这个函数重构一下", "coding", "代码重构"},
		{"给这个项目添加单元测试", "coding", "测试编写"},
		{"设计一个微服务架构", "coding", "架构设计"},

		// ============================================================
		// 2. 明确的 SSH/服务器意图 — 应该命中 ssh
		// ============================================================
		{"登录4090服务器查看GPU占用率", "ssh", "典型SSH场景"},
		{"连上生产环境看一下日志", "ssh", "查看日志"},
		{"重启一下nginx", "ssh", "服务管理"},
		{"看看线上机器的磁盘空间", "ssh", "服务器监控"},
		{"把这个文件上传到服务器", "ssh", "文件传输"},
		{"check the GPU usage on the server", "ssh", "英文SSH"},

		// ============================================================
		// 3. 明确的内容处理意图 — 应该命中 content
		// ============================================================
		{"翻译这篇论文摘要", "content", "翻译"},
		{"帮我整理一下会议纪要", "content", "内容整理"},
		{"总结这个PDF的要点", "content", "文档总结"},
		{"把这段英文翻译成中文", "content", "翻译"},
		{"收集一下最近的AI新闻", "content", "资料收集"},
		{"summarize this article for me", "content", "英文总结"},

		// ============================================================
		// 4. 闲聊 — 应该命中 chat
		// ============================================================
		{"你好啊", "chat", "打招呼"},
		{"你能做什么", "chat", "能力询问"},
		{"谢谢你的帮助", "chat", "感谢"},
		{"hello there", "chat", "英文打招呼"},
		{"讲个笑话吧", "chat", "闲聊"},

		// ============================================================
		// 5. 浏览器意图 — 应该命中 browser
		// ============================================================
		{"打开浏览器帮我填个表单", "browser", "浏览器操作"},
		{"录制一下这个网页的操作流程", "browser", "浏览器录制"},
		{"帮我在这个网站上点击购买按钮", "browser", "网页交互"},

		// ============================================================
		// 6. 容易误判的边界 case（重点关注！）
		// ============================================================

		// 6a. "格式化" — 应该是 content，不是 coding
		{"格式化一下这段文字", "content", "格式化=排版，不是代码格式化"},
		{"format this text nicely", "content", "英文格式化文字"},

		// 6b. "架构设计" 讨论 vs 编码 — 讨论应该是 chat/content
		{"介绍一下微服务架构的优缺点", "content", "架构知识问答，不是编码"},
		{"什么是MVC架构", "content", "概念解释，不是编码"},

		// 6c. "服务器" 但不是 SSH
		{"什么是服务器", "chat", "概念问答，不是SSH操作"},
		{"服务器和客户端的区别是什么", "chat", "知识问答"},

		// 6d. 短指令 — 这些本身不应该高置信命中任何意图
		{"开工", "none", "短指令，需要上下文才能判断"},
		{"好的", "none", "确认，需要上下文"},
		{"继续", "none", "延续指令"},
		{"ok", "none", "确认"},
		{"开干", "none", "短指令"},

		// 6e. 混合意图 — 看哪个更强
		{"帮我写个脚本然后上传到服务器", "coding", "混合coding+ssh，coding更主要"},
		{"翻译这段代码的注释", "content", "翻译为主，不是编码"},
		{"看看服务器上的代码有没有bug", "ssh", "SSH为主，虽然提到代码"},

		// 6f. 容易被关键词误触发的
		{"我想了解一下这个项目的架构设计", "content", "了解/学习，不是编码"},
		{"docker是什么", "chat", "概念问答，不应触发SSH"},
		{"nginx的配置文件怎么写", "content", "知识问答，不是SSH操作"},
		{"python怎么安装", "chat", "安装问题，不是编码"},
		{"git怎么用", "chat", "工具使用问题"},

		// 6g. 中英混合
		{"帮我fix一下这个bug", "coding", "中英混合编码"},
		{"deploy到production", "ssh", "中英混合部署"},
	}
}

// classifyByEmbedding computes cosine similarity against all anchors and returns
// the best matching intent + score, and the full score breakdown.
func classifyByEmbedding(queryVec []float32, anchors []intentAnchor) (bestIntent string, bestScore float64, allScores map[string]float64) {
	allScores = make(map[string]float64)
	for _, anchor := range anchors {
		var maxSim float64
		for _, anchorVec := range anchor.Vecs {
			sim := cosine(queryVec, anchorVec)
			if sim > maxSim {
				maxSim = sim
			}
		}
		allScores[anchor.Name] = maxSim
		if maxSim > bestScore {
			bestScore = maxSim
			bestIntent = anchor.Name
		}
	}
	return
}

// TestIntentEmbeddingAccuracy is the main accuracy test.
// It computes embeddings for all test cases and checks classification accuracy.
func TestIntentEmbeddingAccuracy(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	// Build and embed anchors.
	anchors := buildAnchors()
	for i := range anchors {
		anchors[i].Vecs = make([][]float32, len(anchors[i].Texts))
		for j, text := range anchors[i].Texts {
			vec, err := emb.Embed(text)
			if err != nil {
				t.Fatalf("embed anchor %q failed: %v", text, err)
			}
			anchors[i].Vecs[j] = vec
		}
	}

	cases := buildTestCases()

	// Track results.
	var correct, incorrect, ambiguous int
	type failInfo struct {
		input       string
		expected    string
		got         string
		score       float64
		gap         float64 // gap between top-1 and top-2
		description string
		allScores   map[string]float64
	}
	var failures []failInfo

	// Thresholds to test.
	highThreshold := 0.75  // above this = confident match
	lowThreshold := 0.60   // below this = no match ("none")
	// Between low and high = ambiguous zone

	t.Logf("\n%-50s | %-10s | %-10s | %-6s | %-6s | %s", "Input", "Expected", "Got", "Score", "Gap", "Result")
	t.Logf("%s", strings.Repeat("-", 120))

	for _, tc := range cases {
		queryVec, err := emb.Embed(tc.Input)
		if err != nil {
			t.Fatalf("embed %q failed: %v", tc.Input, err)
		}

		bestIntent, bestScore, allScores := classifyByEmbedding(queryVec, anchors)

		// Find second-best score for gap analysis.
		type intentScore struct {
			name  string
			score float64
		}
		sorted := make([]intentScore, 0, len(allScores))
		for name, score := range allScores {
			sorted = append(sorted, intentScore{name, score})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].score > sorted[j].score })
		gap := 0.0
		if len(sorted) >= 2 {
			gap = sorted[0].score - sorted[1].score
		}

		// Determine classification result.
		var predicted string
		if bestScore < lowThreshold {
			predicted = "none"
		} else if bestScore < highThreshold {
			predicted = bestIntent + "?" // ambiguous
		} else {
			predicted = bestIntent
		}

		// Check correctness.
		var result string
		isCorrect := false
		if tc.ExpectedIntent == "none" {
			// For "none" cases, we want the score to be below highThreshold
			// (ideally below lowThreshold, but ambiguous is also acceptable).
			if bestScore < highThreshold {
				isCorrect = true
				result = ""
				correct++
			} else {
				result = "FALSE_POSITIVE"
				incorrect++
			}
		} else {
			// For specific intent cases.
			if bestIntent == tc.ExpectedIntent && bestScore >= lowThreshold {
				isCorrect = true
				result = ""
				correct++
			} else if bestScore < lowThreshold {
				result = "MISSED"
				incorrect++
			} else if bestIntent != tc.ExpectedIntent {
				result = fmt.Sprintf("WRONG(%s)", bestIntent)
				incorrect++
			} else {
				result = "AMBIGUOUS"
				ambiguous++
			}
		}

		// Truncate input for display.
		displayInput := tc.Input
		if len(displayInput) > 45 {
			displayInput = displayInput[:42] + "..."
		}

		t.Logf("%-50s | %-10s | %-10s | %.4f | %.4f | %s",
			displayInput, tc.ExpectedIntent, predicted, bestScore, gap, result)

		if !isCorrect {
			failures = append(failures, failInfo{
				input:       tc.Input,
				expected:    tc.ExpectedIntent,
				got:         bestIntent,
				score:       bestScore,
				gap:         gap,
				description: tc.Description,
				allScores:   allScores,
			})
		}
	}

	// Summary.
	total := correct + incorrect + ambiguous
	t.Logf("\n%s", strings.Repeat("=", 80))
	t.Logf("SUMMARY: %d/%d correct (%.1f%%), %d incorrect, %d ambiguous",
		correct, total, float64(correct)/float64(total)*100, incorrect, ambiguous)
	t.Logf("Thresholds: high=%.2f, low=%.2f", highThreshold, lowThreshold)

	if len(failures) > 0 {
		t.Logf("\nFAILURE DETAILS:")
		for _, f := range failures {
			t.Logf("  Input: %s", f.input)
			t.Logf("  Expected: %s, Got: %s (score=%.4f, gap=%.4f)", f.expected, f.got, f.score, f.gap)
			t.Logf("  Reason: %s", f.description)
			t.Logf("  All scores: %v", f.allScores)
			t.Logf("")
		}
	}
}

// TestIntentEmbeddingThresholdSweep sweeps thresholds to find optimal values.
func TestIntentEmbeddingThresholdSweep(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	anchors := buildAnchors()
	for i := range anchors {
		anchors[i].Vecs = make([][]float32, len(anchors[i].Texts))
		for j, text := range anchors[i].Texts {
			vec, err := emb.Embed(text)
			if err != nil {
				t.Fatalf("embed anchor failed: %v", err)
			}
			anchors[i].Vecs[j] = vec
		}
	}

	cases := buildTestCases()

	// Pre-compute all query embeddings.
	type precomputed struct {
		tc       testCase
		queryVec []float32
		best     string
		bestSc   float64
	}
	var data []precomputed
	for _, tc := range cases {
		vec, err := emb.Embed(tc.Input)
		if err != nil {
			t.Fatal(err)
		}
		best, bestSc, _ := classifyByEmbedding(vec, anchors)
		data = append(data, precomputed{tc, vec, best, bestSc})
	}

	// Sweep high threshold from 0.60 to 0.90.
	t.Logf("\n%-12s | %-12s | %-8s | %-8s | %-8s | %-10s", "HighThresh", "LowThresh", "Correct", "Wrong", "FP", "Accuracy")
	t.Logf("%s", strings.Repeat("-", 80))

	bestAccuracy := 0.0
	bestHigh := 0.0
	bestLow := 0.0

	for high := 0.60; high <= 0.90; high += 0.02 {
		for low := 0.45; low <= high-0.05; low += 0.02 {
			correct := 0
			wrong := 0
			falsePositive := 0
			total := len(data)

			for _, d := range data {
				if d.tc.ExpectedIntent == "none" {
					if d.bestSc < high {
						correct++
					} else {
						falsePositive++
					}
				} else {
					if d.best == d.tc.ExpectedIntent && d.bestSc >= low {
						correct++
					} else {
						wrong++
					}
				}
			}

			accuracy := float64(correct) / float64(total)
			if accuracy > bestAccuracy {
				bestAccuracy = accuracy
				bestHigh = high
				bestLow = low
			}

			// Only print interesting rows.
			if accuracy >= 0.70 {
				t.Logf("%-12.2f | %-12.2f | %-8d | %-8d | %-8d | %.1f%%",
					high, low, correct, wrong, falsePositive, accuracy*100)
			}
		}
	}

	t.Logf("\nBEST: high=%.2f, low=%.2f, accuracy=%.1f%%", bestHigh, bestLow, bestAccuracy*100)
}

// TestIntentEmbeddingScoreDistribution prints the raw score distribution
// to help understand the embedding space.
func TestIntentEmbeddingScoreDistribution(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	anchors := buildAnchors()
	for i := range anchors {
		anchors[i].Vecs = make([][]float32, len(anchors[i].Texts))
		for j, text := range anchors[i].Texts {
			vec, err := emb.Embed(text)
			if err != nil {
				t.Fatal(err)
			}
			anchors[i].Vecs[j] = vec
		}
	}

	cases := buildTestCases()

	// Collect score distributions per expected intent.
	type scoreEntry struct {
		input     string
		expected  string
		bestMatch string
		bestScore float64
		gap       float64
	}
	var entries []scoreEntry

	for _, tc := range cases {
		vec, err := emb.Embed(tc.Input)
		if err != nil {
			t.Fatal(err)
		}
		_, bestScore, allScores := classifyByEmbedding(vec, anchors)

		// Find best and second-best.
		type kv struct {
			k string
			v float64
		}
		var sorted []kv
		for k, v := range allScores {
			sorted = append(sorted, kv{k, v})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })

		gap := 0.0
		if len(sorted) >= 2 {
			gap = sorted[0].v - sorted[1].v
		}

		entries = append(entries, scoreEntry{
			input:     tc.Input,
			expected:  tc.ExpectedIntent,
			bestMatch: sorted[0].k,
			bestScore: bestScore,
			gap:       gap,
		})
	}

	// Print by expected intent.
	t.Logf("\n=== Score Distribution by Expected Intent ===\n")
	groups := map[string][]scoreEntry{}
	for _, e := range entries {
		groups[e.expected] = append(groups[e.expected], e)
	}

	for _, intent := range []string{"coding", "ssh", "content", "chat", "browser", "none"} {
		group := groups[intent]
		if len(group) == 0 {
			continue
		}
		t.Logf("--- %s (%d cases) ---", intent, len(group))

		var scores []float64
		var gaps []float64
		correctCount := 0
		for _, e := range group {
			scores = append(scores, e.bestScore)
			gaps = append(gaps, e.gap)
			if intent == "none" {
				// For "none", correct means NOT high confidence.
			} else if e.bestMatch == intent {
				correctCount++
			}
		}

		sort.Float64s(scores)
		sort.Float64s(gaps)

		t.Logf("  Score range: [%.4f, %.4f]", scores[0], scores[len(scores)-1])
		t.Logf("  Score median: %.4f", scores[len(scores)/2])
		t.Logf("  Gap range:   [%.4f, %.4f]", gaps[0], gaps[len(gaps)-1])
		if intent != "none" {
			t.Logf("  Correct match: %d/%d (%.0f%%)", correctCount, len(group),
				float64(correctCount)/float64(len(group))*100)
		}
		t.Logf("")
	}
}

// TestIntentEmbeddingLatency measures per-query classification latency.
func TestIntentEmbeddingLatency(t *testing.T) {
	modelPath := findModel(t)
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	anchors := buildAnchors()
	totalAnchors := 0
	for i := range anchors {
		anchors[i].Vecs = make([][]float32, len(anchors[i].Texts))
		for j, text := range anchors[i].Texts {
			vec, err := emb.Embed(text)
			if err != nil {
				t.Fatal(err)
			}
			anchors[i].Vecs[j] = vec
		}
		totalAnchors += len(anchors[i].Texts)
	}

	queries := []string{
		"帮我写一个贪吃蛇游戏",
		"登录服务器看GPU",
		"翻译这篇文章",
		"你好",
		"开工",
	}

	t.Logf("Total anchor vectors: %d", totalAnchors)
	for _, q := range queries {
		start := time.Now()
		vec, _ := emb.Embed(q)
		embedTime := time.Since(start)

		start = time.Now()
		_, _, _ = classifyByEmbedding(vec, anchors)
		classifyTime := time.Since(start)

		t.Logf("Query %q: embed=%v, classify=%v, total=%v",
			q, embedTime, classifyTime, embedTime+classifyTime)
	}
}

// Suppress unused import warning.
var _ = math.Sqrt
