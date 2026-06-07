package intent

import (
	"fmt"
	"log"
	"math"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// intentAnchor groups anchor texts and their pre-computed embeddings for one
// intent label category. Used by Layer 2 embedding cosine classification.
type intentAnchor struct {
	Label IntentLabel
	Texts []string
	Vecs  [][]float32
}

// defaultAnchors returns anchor text sets for all 10 non-ambiguous/unknown
// intent labels. Each label has 6-14 representative sentences used as
// reference points for embedding cosine similarity scoring.
//
// Deprecated: Production code now uses BuildAnchorsFromDefinitions(DefaultDefinitions())
// via classifier.go. This function is retained for backward compatibility with
// tests in corelib/tool/intent_classifier.go that have their own copy.
func defaultAnchors() []intentAnchor {
	return []intentAnchor{
		{
			Label: LabelCoding,
			Texts: []string{
				// Chinese (creation-oriented) — from gateAnchors new_project
				"开发一个贪吃蛇游戏",
				"写一个爬虫程序",
				"帮我开发一个聊天应用",
				"实现一个REST API服务",
				"创建一个命令行工具",
				"写一个自动化脚本",
				"开发一个数据可视化面板",
				// English (creation-oriented) — from gateAnchors new_project
				"build a web application",
				"create a CLI tool",
				"develop a REST API",
				"write a Python script for data processing",
				"implement a chat server",
				"build a game in JavaScript",
				"create a file upload service",
			},
		},
		{
			Label: LabelBugFix,
			Texts: []string{
				// Chinese (fix/debug-oriented) — from gateAnchors bug_fix
				"有bug，一直显示加载中",
				"修复崩溃问题",
				"页面白屏了",
				"程序闪退",
				"调试一下这个问题",
				"排查报错原因",
				"修复登录失败的bug",
				// English (fix/debug-oriented) — from gateAnchors bug_fix
				"fix the loading issue",
				"debug this crash",
				"the app keeps crashing on startup",
				"fix the authentication error",
				"there's a bug in the payment flow",
				"troubleshoot the memory leak",
				"resolve the null pointer exception",
			},
		},
		{
			Label: LabelMaintenance,
			Texts: []string{
				// Chinese (refactor/optimize) — from gateAnchors maintenance
				"重构这个函数",
				"优化性能",
				"清理无用代码",
				"升级依赖版本",
				"改善代码结构",
				"优化数据库查询速度",
				// English (refactor/optimize) — from gateAnchors maintenance
				"refactor the auth module",
				"clean up dead code",
				"optimize the database queries",
				"upgrade the dependencies",
				"improve code readability",
				"reduce technical debt in the codebase",
			},
		},
		{
			Label: LabelNonCoding,
			Texts: []string{
				// Chinese (non-coding tasks) — from gateAnchors non_coding
				"翻译文档",
				"整理会议纪要",
				"总结这篇文章",
				"帮我整理资料",
				"生成PDF报告",
				"把这段话翻译成英文",
				// English (non-coding tasks) — from gateAnchors non_coding
				"summarize this article",
				"translate this document",
				"organize meeting notes into a concise summary",
				"organize these notes",
				"help me write a report",
				"draft a project proposal document",
			},
		},
		{
			Label: LabelContinuation,
			Texts: []string{
				// Chinese (short action phrases) — from gateAnchors continuation
				"继续",
				"开工",
				"开干",
				"动手",
				"搞起来",
				"开始吧",
				// English (short action phrases) — from gateAnchors continuation
				"let's go",
				"start working",
				"go ahead",
				"continue",
			},
		},
		{
			Label: LabelSSH,
			Texts: []string{
				// Chinese
				"登录服务器查看日志",
				"连接远程服务器",
				"SSH到生产环境检查状态",
				"帮我登录服务器重启服务",
				"查看服务器上的GPU占用率",
				"远程执行命令查看磁盘空间",
				// English
				"connect to the server via SSH",
				"log into the production server",
				"check the server logs remotely",
				"restart the service on the remote machine",
				"SSH into the GPU server and check usage",
				"run a command on the remote host",
			},
		},
		{
			Label: LabelSearch,
			Texts: []string{
				// Chinese
				"搜索一下最新的AI论文",
				"帮我在网上查找这个问题的解决方案",
				"搜索关于机器学习的资料",
				"上网查一下这个API的文档",
				"帮我搜索这个错误信息",
				"网上找一下这个库的用法",
				// English
				"search the web for this error message",
				"look up the latest documentation for React",
				"find information about this API online",
				"search for solutions to this problem",
				"google this error and find a fix",
				"look up best practices for Go concurrency",
			},
		},
		{
			Label: LabelDocumentDelivery,
			Texts: []string{
				// Chinese
				"把这个文件发送给我",
				"打开桌面上的PDF文件",
				"帮我发送这份报告",
				"生成一份PDF文档并发给我",
				"打开这个Excel文件看看内容",
				"把结果导出为文件发送",
				// English
				"send me this file",
				"open the PDF document on my desktop",
				"deliver the report to me",
				"generate a PDF and send it over",
				"open this spreadsheet file",
				"export the results and send the file",
			},
		},
		{
			Label: LabelBrowser,
			Texts: []string{
				// Chinese
				"打开浏览器访问这个网站",
				"帮我在网页上点击购买按钮",
				"用浏览器自动化填写表单",
				"录制浏览器操作步骤",
				"在Chrome中打开这个页面并截图",
				"自动化浏览器测试这个功能",
				"用playwright测试登录流程",
				// English
				"open the browser and navigate to this URL",
				"click the submit button on the web page",
				"automate the browser to fill in the form",
				"record browser actions for this workflow",
				"take a screenshot of this page in Chrome",
				"use playwright to test the login flow",
				"automate web testing with browser tools",
				"log into a website and publish a post",
				"sign in to a social website and submit content",
				"open Zhihu in a browser and publish a pin",
				"登录知乎并发表一条想法",
				"打开网页登录账号然后发布内容",
				"在网页上发帖并验证发布结果",
			},
		},
		{
			Label: LabelOffice,
			Texts: []string{
				// Chinese
				"帮我制作一个PPT演示文稿",
				"生成一份Excel报表",
				"创建一个Word文档",
				"把数据整理成Excel表格",
				"制作项目汇报PPT",
				"生成一份数据分析的电子表格",
				// English
				"create a PowerPoint presentation",
				"generate an Excel spreadsheet report",
				"make a Word document for the proposal",
				"organize the data into an Excel file",
				"build a slide deck for the meeting",
				"create a spreadsheet with the analysis results",
			},
		},
	}
}

// classifyByEmbedding performs Layer 2 embedding-based classification using
// cosine similarity between the user message embedding and pre-computed anchor
// vectors for each intent label category.
//
// Returns (result, true) when confident (top1Score >= 0.78 and gap >= 0.10),
// or (result, false) when the result is ambiguous and should escalate to Layer 3.
//
// The function does NOT depend on QueryEmbeddingCache — the caller handles caching.
func classifyByEmbedding(embedder embedding.Embedder, anchors []intentAnchor, text string) (ClassificationResult, bool) {
	// 1. Get query embedding.
	queryVec, err := embedder.Embed(text)
	if err != nil || queryVec == nil {
		return ClassificationResult{}, false
	}

	// 2. Compute max cosine similarity for each anchor set.
	type labelScore struct {
		label IntentLabel
		score float64
	}
	scores := make([]labelScore, 0, len(anchors))

	for _, anchor := range anchors {
		if len(anchor.Vecs) == 0 {
			continue // anchor not yet warmed up
		}
		maxSim := 0.0
		for _, vec := range anchor.Vecs {
			sim := cosineSimilarity(queryVec, vec)
			if sim > maxSim {
				maxSim = sim
			}
		}
		scores = append(scores, labelScore{label: anchor.Label, score: maxSim})
	}

	if len(scores) == 0 {
		return ClassificationResult{}, false
	}

	// 3. Find top-1 and top-2 categories.
	var top1, top2 labelScore
	for _, s := range scores {
		if s.score > top1.score {
			top2 = top1
			top1 = s
		} else if s.score > top2.score {
			top2 = s
		}
	}

	gap := top1.score - top2.score

	// 4. Decision thresholds.
	result := ClassificationResult{
		Primary:    top1.label,
		Confidence: top1.score,
		Layer:      2,
	}

	if top1.score >= 0.78 && gap >= 0.10 {
		result.Reason = fmt.Sprintf("embedding: top=%s (%.3f), gap=%.3f", top1.label, top1.score, gap)
		return result, true
	}

	// Ambiguous — escalate to Layer 3.
	result.Reason = fmt.Sprintf("embedding ambiguous: top=%s (%.3f), gap=%.3f", top1.label, top1.score, gap)
	return result, false
}

// warmupAnchors pre-computes embeddings for all anchor texts using the
// provided embedder. This should be called in a background goroutine at
// startup to avoid blocking the main thread.
func warmupAnchors(embedder embedding.Embedder, anchors []intentAnchor) {
	for i := range anchors {
		if len(anchors[i].Texts) == 0 {
			continue
		}
		vecs, err := embedder.EmbedBatch(anchors[i].Texts)
		if err != nil {
			log.Printf("[UnifiedIntentClassifier] warmup failed for %s: %v", anchors[i].Label, err)
			return
		}
		anchors[i].Vecs = vecs
	}
	log.Printf("[UnifiedIntentClassifier] anchor warmup complete (%d labels)", len(anchors))
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0.0 if either vector is empty or they have different lengths.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}

	var dot, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
