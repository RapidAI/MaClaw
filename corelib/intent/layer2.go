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
				"切换到完整模式",
				"切换到完整agent模式",
				"用完整能力再做一次",
				"switch to full agent",
				"switch to full agent mode",
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
				"全网搜索这个人的资料",
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
				"帮我发送这份报告",
				"把结果导出为文件发送",
				// English
				"send me this file",
				"deliver the report to me",
				"export the results and send the file",
			},
		},
		{
			Label: LabelDocumentOpen,
			Texts: []string{
				"打开桌面上的PDF文件",
				"用默认程序打开这个文档",
				"open the PDF document on my desktop",
				"open this document with the default app",
			},
		},
		{
			Label: LabelDocumentGenerate,
			Texts: []string{
				"生成一份PDF文档并发给我",
				"生成pdf报告",
				"把这些内容生成PDF",
				"export pdf",
				"generate a PDF and send it over",
				"generate a PDF document",
				"render this as a PDF file",
				"make a PDF from these facts",
			},
		},
		{
			Label: LabelBrowser,
			Texts: []string{
				// Chinese
				"打开 Chrome 点购买",
				"open Chrome and click Buy",
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

const (
	EmbeddingConfidentMinScore = 0.78
	EmbeddingConfidentMinGap   = 0.10
	// The companion floor is calibrated against the installed production Gemma
	// model.  It is intentionally separate from a single-label grant: the
	// leading label must still meet EmbeddingCompositePrimaryMinScore and the
	// pair must be explicitly declared by the taxonomy.
	// The floor is calibrated with generic live-data-to-PDF requests against
	// PDF-only negative controls, and it is dimension-sensitive: absolute
	// cosines rise with the embedding width, so the value must be re-measured
	// whenever DefaultEmbeddingDim or the model changes. At 768 dimensions
	// (2026-08-24) the strongest observed PDF-only live_data score is 0.715
	// ("将附件里的内容排版并导出为PDF报告") and the weakest genuine weather+PDF
	// companion is 0.744 ("天津天气…"); 0.73 sits between them. (At 256 the
	// same negatives peaked at 0.643 and the floor was 0.67.)
	EmbeddingCompositeSecondaryMinScore = 0.73
	// EmbeddingCompositeLiveDataMinScore is the companion floor specific to a
	// live_data half.  PDF-only negatives ("将附件里的内容排版并导出为PDF报告"
	// family) embed close to the generic live_data anchors, so once the
	// composite anchors ("查询某城市天气并输出格式化PDF报告",
	// "查询比特币价格并输出格式化PDF报告") lifted genuine live-data+PDF
	// queries, the negatives rose with them and the generic 0.73 floor
	// stopped separating the two groups.  Measured at 768 dimensions with
	// those anchors installed (2026-08-24): genuine live-data+PDF queries
	// score live_data 0.841-0.957 across 19 phrasings (weather-first and
	// PDF-first, 10 city names, FX/stock/news/flight/crypto); PDF-only
	// negatives peak at 0.802.  0.82 leaves margin on both sides
	// (+0.021 / -0.018).  Re-measure whenever DefaultEmbeddingDim, the
	// model, or the live_data anchor set changes.
	EmbeddingCompositeLiveDataMinScore = 0.82
	// EmbeddingCompositePrimaryMinScore is the minimum score for the leading
	// half of a declared composite to stand on its own.  A compound result is
	// only locally decisive when both independently embedded meanings clear
	// their reviewed thresholds; it is not inferred from words in the request.
	EmbeddingCompositePrimaryMinScore = EmbeddingConfidentMinScore
	EmbeddingLookupMinScore           = 0.70
	EmbeddingLookupMinGap             = 0.05
	EmbeddingLookupCompositeFloor     = 0.60
	// TreeVerdictDistrustMaxScore bounds the tree verdicts allowed to override
	// a stronger L2 leader.  The tree prompt's own score guide bands 0.40-0.64
	// as "uncertain"; below 0.50 the model is effectively guessing, and a guess
	// must not strip capabilities a stronger local reading granted
	// (production: "pdf在哪？" L2=document_generate 0.87 was replaced by
	// tree=session_manage 0.41; a book-writing turn L2=workflow_task 0.74
	// was replaced by tree=task_track 0.38 and locked onto a managed
	// task-tracking surface).
	TreeVerdictDistrustMaxScore = 0.50
)

// retainEmbeddingOverWeakTree reports whether an escalated L2 leader should
// be kept instead of a guessing tree verdict. L2 only reaches the tree when
// it is *not* locally confident, so requiring EmbeddingConfidentMinScore here
// would make the guard vacuous for the 0.70–0.78 band that actually escalates.
// EmbeddingLookupMinScore is the usable-hint floor already used for lookup /
// office hints; below that both channels are noise and the tree may stand.
func retainEmbeddingOverWeakTree(l2 ClassificationResult, treeScore float64) bool {
	switch l2.Primary {
	case "", LabelUnknown, LabelAmbiguous:
		return false
	}
	if treeScore >= TreeVerdictDistrustMaxScore {
		return false
	}
	if l2.Confidence < EmbeddingLookupMinScore {
		return false
	}
	return l2.Confidence > treeScore
}

// classifyByEmbedding performs Layer 2 embedding-based classification using
// cosine similarity between the user message embedding and pre-computed anchor
// vectors for each intent label category.
//
// Returns (result, true) when confident (top1Score >= EmbeddingConfidentMinScore
// and gap >= EmbeddingConfidentMinGap, or a read-only search/live_data hit with
// score >= EmbeddingLookupMinScore and gap >= EmbeddingLookupMinGap),
// or (result, false) when the result is ambiguous and should escalate to Layer 3.
//
// Caching is handled one level up: UnifiedIntentClassifier memoizes results
// per message (full Classify cache and the ClassifyEmbeddingOnly cache).
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

	// 3. Find top-1 and top-2 categories, plus the strongest declared
	// composite companion.  The runner-up is not necessarily the companion:
	// a broad non-capability label can rank between the two halves of a real
	// lookup + artifact request.
	var top1, top2 labelScore
	for _, s := range scores {
		if s.score > top1.score {
			top2 = top1
			top1 = s
		} else if s.score > top2.score {
			top2 = s
		}
	}
	var composite labelScore
	for _, s := range scores {
		if s.label != top1.label && locallyVerifiedEmbeddingCompositePair(top1.label, s.label) && s.score > composite.score {
			composite = s
		}
	}
	// declaredCompanion tracks the strongest taxonomy-declared composite half
	// (lookup ↔ document_generate/live_data_visual) regardless of local
	// verifiability.  The locally-verified scan above governs fast-path
	// grants; this one only vetoes silent capability loss: a strong lookup
	// reading with a material artifact half must escalate rather than ship
	// as a plain lookup ("杭州天气，生成pdf报告" scored live_data 0.833 with
	// document_generate 0.72 and used to collapse to a confident plain
	// lookup, dropping the PDF capability without ever consulting the tree).
	var declaredCompanion labelScore
	for _, s := range scores {
		if s.label != top1.label && declaredCompositeIntentPair(top1.label, s.label) && s.score > declaredCompanion.score {
			declaredCompanion = s
		}
	}

	gap := top1.score - top2.score

	// 4. Decision thresholds.
	result := ClassificationResult{
		Primary:       top1.label,
		Confidence:    top1.score,
		Layer:         2,
		RunnerUp:      top2.label,
		RunnerUpScore: top2.score,
	}

	// Live external data and an artifact-generating candidate form a dependency
	// graph, not two interchangeable labels. The pair is a narrowly reviewed
	// local decision: both independently embedded meanings must clear their
	// thresholds. This avoids making a valid local semantic result depend on an
	// unrelated model's structured-output behavior.
	companionFloor := EmbeddingCompositeSecondaryMinScore
	if composite.label == LabelLiveData {
		companionFloor = EmbeddingCompositeLiveDataMinScore
	}
	if composite.label != "" && composite.score >= companionFloor {
		result.Secondary = []IntentLabel{composite.label}
		if top1.score >= EmbeddingCompositePrimaryMinScore {
			result.Reason = fmt.Sprintf("embedding declared composite: top=%s (%.3f), companion=%s (%.3f), gap=%.3f", top1.label, top1.score, composite.label, composite.score, gap)
			return result, true
		}
		result.Reason = fmt.Sprintf("embedding semantic composite requires tree: top=%s (%.3f), companion=%s (%.3f), gap=%.3f", top1.label, top1.score, composite.label, composite.score, gap)
		return result, false
	}

	// A document-generation candidate without a declared lookup companion
	// remains unresolved.  Layer 2 can recognize the requested artifact, but it
	// cannot prove whether its source facts are already supplied or must first
	// be acquired.
	if top1.label == LabelDocumentGenerate {
		result.Reason = fmt.Sprintf("embedding document generation requires tree: top=%s (%.3f), gap=%.3f", top1.label, top1.score, gap)
		return result, false
	}

	if top1.score >= EmbeddingConfidentMinScore && gap >= EmbeddingConfidentMinGap {
		// A confident lookup with a material declared artifact half is a
		// composite request, not a plain lookup: shipping it as confident
		// lookup-only silently drops the generate capability without ever
		// consulting the tree.  Escalate with the half as evidence.
		if isLookupIntentLabel(top1.label) && declaredCompanion.label != "" && declaredCompanion.score >= EmbeddingLookupCompositeFloor {
			result.Secondary = []IntentLabel{declaredCompanion.label}
			result.Reason = fmt.Sprintf("embedding ambiguous composite: top=%s (%.3f), companion=%s (%.3f), gap=%.3f", top1.label, top1.score, declaredCompanion.label, declaredCompanion.score, gap)
			return result, false
		}
		// Mirror guard for a confident office/deck request whose companion is a
		// lookup ("网上找几张照片做成生日PPT").  The deck half is settled, but
		// the search boundary is not locally separable: on the installed
		// 768-dim model (2026-08-25) office-only negatives score the lookup
		// labels up to 0.740 while the genuine find-images phrasings score
		// 0.65, so any local floor would mis-grant search on plain Excel/PPT
		// requests.  Attach the half and let the tree read the request;
		// shipping confident office-only dropped web_search in production and
		// the turn reported the search tool as unavailable.
		if top1.label == LabelOffice && isLookupIntentLabel(declaredCompanion.label) && declaredCompanion.score >= EmbeddingLookupCompositeFloor {
			result.Secondary = []IntentLabel{declaredCompanion.label}
			result.Reason = fmt.Sprintf("embedding ambiguous composite: top=%s (%.3f), companion=%s (%.3f), gap=%.3f", top1.label, top1.score, declaredCompanion.label, declaredCompanion.score, gap)
			return result, false
		}
		result.Reason = fmt.Sprintf("embedding: top=%s (%.3f), gap=%.3f", top1.label, top1.score, gap)
		return result, true
	}

	// Read-only lookup: a slightly weaker live_data/search hit is still a
	// lookup. Escalating to the chat model as Layer 3 makes the tree parser
	// fail closed (the model answers as an assistant instead of emitting
	// candidates) and HostRejects ordinary weather queries. A PDF/generate
	// composite must not take this shortcut: it is mutating and has to keep
	// the confident path or Layer 3 rather than collapsing to search-only.
	if (top1.label == LabelSearch || top1.label == LabelLiveData) && top1.score >= EmbeddingLookupMinScore && gap >= EmbeddingLookupMinGap {
		if composite.label != "" && composite.score >= EmbeddingLookupCompositeFloor {
			result.Secondary = []IntentLabel{composite.label}
			result.Reason = fmt.Sprintf("embedding ambiguous composite: top=%s (%.3f), companion=%s (%.3f), gap=%.3f", top1.label, top1.score, composite.label, composite.score, gap)
			return result, false
		}
		// A declared-but-not-locally-verified composite half (search +
		// document_generate / live_data_visual) must escalate too: the
		// locallyVerified scan above cannot see it, so without this check
		// the turn collapses to a confident plain lookup, the artifact
		// capability is never routed, and the loop later reports the
		// generate tool as unavailable ("本轮未授权").  Escalation lets the
		// tree verdict plus the L2 runner-up synthesize the composite.
		if declaredCompanion.label != "" && declaredCompanion.score >= EmbeddingLookupCompositeFloor {
			result.Secondary = []IntentLabel{declaredCompanion.label}
			result.Reason = fmt.Sprintf("embedding ambiguous composite: top=%s (%.3f), companion=%s (%.3f), gap=%.3f", top1.label, top1.score, declaredCompanion.label, declaredCompanion.score, gap)
			return result, false
		}
		result.Reason = fmt.Sprintf("embedding lookup: top=%s (%.3f), gap=%.3f", top1.label, top1.score, gap)
		return result, true
	}

	// Ambiguous — escalate to Layer 3. Keep a declared lookup companion of an
	// office primary as synthesis evidence: the runner-up slot may be held by
	// an unrelated label (document_read/workflow_task embed close to deck
	// requests), and without the half the tree merge cannot see the
	// find-images-online part of the request at all.
	if top1.label == LabelOffice && isLookupIntentLabel(declaredCompanion.label) && declaredCompanion.score >= EmbeddingLookupCompositeFloor {
		result.Secondary = []IntentLabel{declaredCompanion.label}
	}
	result.Reason = fmt.Sprintf("embedding ambiguous: top=%s (%.3f), gap=%.3f", top1.label, top1.score, gap)
	return result, false
}

// warmupAnchors pre-computes embeddings for all anchor texts using the
// provided embedder. This should be called in a background goroutine at
// startup to avoid blocking the main thread. It returns a fully populated
// replacement snapshot only when every anchor set received one vector per
// source text; a partial warmup must never make the classifier appear ready.
// It deliberately does not mutate the caller's anchors, because a reader may
// still hold a snapshot from the previous model while this warmup runs.
func warmupAnchors(embedder embedding.Embedder, anchors []intentAnchor) ([]intentAnchor, error) {
	if embedder == nil || embedding.IsNoop(embedder) {
		return nil, fmt.Errorf("embedding unavailable for anchor warmup")
	}
	warmed := make([]intentAnchor, len(anchors))
	for i, anchor := range anchors {
		warmed[i] = intentAnchor{Label: anchor.Label, Texts: append([]string(nil), anchor.Texts...)}
		if len(anchor.Texts) == 0 {
			continue
		}
		vecs, err := embedder.EmbedBatch(anchor.Texts)
		if err != nil {
			log.Printf("[UnifiedIntentClassifier] warmup failed for %s: %v", anchor.Label, err)
			return nil, fmt.Errorf("warm %s anchors: %w", anchor.Label, err)
		}
		if len(vecs) != len(anchor.Texts) {
			return nil, fmt.Errorf("warm %s anchors: got %d vectors for %d texts", anchor.Label, len(vecs), len(anchor.Texts))
		}
		for j, vec := range vecs {
			if len(vec) == 0 || (embedder.Dim() > 0 && len(vec) != embedder.Dim()) {
				return nil, fmt.Errorf("warm %s anchors: invalid vector %d (len=%d, dim=%d)", anchor.Label, j, len(vec), embedder.Dim())
			}
		}
		warmed[i].Vecs = vecs
	}
	log.Printf("[UnifiedIntentClassifier] anchor warmup complete (%d labels)", len(warmed))
	return warmed, nil
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
