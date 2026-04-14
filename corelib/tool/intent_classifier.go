package tool

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Intent constants returned by IntentClassifier.
const (
	IntentCoding       = "coding"
	IntentSSH          = "ssh"
	IntentContent      = "content"
	IntentChat         = "chat"
	IntentBrowser      = "browser"
	IntentQuery        = "query"         // knowledge question, not an action
	IntentShortCommand = "short_command" // needs context to interpret
	IntentUnknown      = "unknown"
)

// IntentResult holds the classification output.
type IntentResult struct {
	Intent     string  // one of the Intent* constants
	Confidence float64 // 0-1, higher = more certain
	Gap        float64 // score gap between top-1 and top-2 intent
	Layer      int     // 1=rule, 2=embedding, 3=LLM
}

// LLMClassifyFunc is a callback that sends a prompt to an LLM and returns the
// raw text response. The caller (gui/tui) provides this based on their LLM config.
// The function should use a short timeout (~5s) and small max_tokens (~60).
type LLMClassifyFunc func(prompt string) (string, error)

// intentAnchor groups anchor texts and their pre-computed embeddings for one intent.
type intentAnchor struct {
	Name string
	Texts []string
	Vecs  [][]float32
}

// IntentClassifier performs three-layer intent classification:
//   Layer 1: regex/rule-based fast path
//   Layer 2: embedding cosine similarity against anchor texts
//   Layer 3: LLM-based classification for ambiguous cases
type IntentClassifier struct {
	embedder   embedding.Embedder
	anchors    []intentAnchor
	queryCache *QueryEmbeddingCache
	llmFunc    LLMClassifyFunc // optional, set via SetLLMFunc
	ready      bool
	mu         sync.RWMutex
}

// ── Question-pattern regexes (Layer 1) ──────────────────────────────────────

var questionPatternsCN = regexp.MustCompile(
	`(?i)^(什么是|是什么|怎么(?:用|写|安装|配置|部署|学|实现|使用)|如何|为什么|有哪些|介绍一下|解释一下|了解一下|讲讲|说说)`)

var questionSuffixCN = regexp.MustCompile(
	`(?i)(是什么|怎么用|怎么写|怎么安装|怎么配置|有什么区别|的区别|的优缺点|有哪些|是啥)$`)

var questionMidCN = regexp.MustCompile(
	`(?i)(我想了解|想了解|想知道|想学习|帮我介绍|给我介绍|给我讲讲|给我说说)`)

var questionPatternsEN = regexp.MustCompile(
	`(?i)^(what is|what are|how to|how do|how does|why |explain |tell me about |describe )`)

// shortCommandMaxRunes is the max rune length for a message to be considered a
// "short command" that needs conversational context to interpret.
// Catches "开工"(2), "好的"(2), "ok"(2), "开干"(2), "继续"(2), "嗯"(1), "go"(2)
// but lets "你好啊"(3), "你能做什么"(5) fall through to embedding for chat detection.
const shortCommandMaxRunes = 2

// ── Anchor definitions ──────────────────────────────────────────────────────

func defaultAnchors() []intentAnchor {
	return []intentAnchor{
		{
			Name: IntentCoding,
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
			Name: IntentSSH,
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
			Name: IntentContent,
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
			Name: IntentChat,
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
			Name: IntentBrowser,
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

// ── Thresholds ──────────────────────────────────────────────────────────────

const (
	embeddingHighThreshold = 0.78 // above this + sufficient gap = confident
	embeddingLowThreshold  = 0.55 // below this = no match
	embeddingMinGap        = 0.10 // minimum gap between top-1 and top-2 for confidence
)

// ── Constructor & initialization ────────────────────────────────────────────

// NewIntentClassifier creates a classifier. If emb is nil or NoopEmbedder,
// Layer 2 (embedding) is disabled and only rule-based classification is used.
func NewIntentClassifier(emb embedding.Embedder) *IntentClassifier {
	ic := &IntentClassifier{
		embedder: emb,
		anchors:  defaultAnchors(),
	}
	if emb != nil && !embedding.IsNoop(emb) {
		ic.queryCache = NewQueryEmbeddingCache(emb, 64, 30*time.Second)
		go ic.warmAnchors()
	}
	return ic
}

// warmAnchors pre-computes anchor embeddings in the background.
func (ic *IntentClassifier) warmAnchors() {
	defer func() {
		if r := recover(); r != nil {
			// Embedder panicked — stay not-ready, Layer 2 will be skipped.
		}
	}()
	for i := range ic.anchors {
		vecs := make([][]float32, len(ic.anchors[i].Texts))
		for j, text := range ic.anchors[i].Texts {
			vec, err := ic.embedder.Embed(text)
			if err != nil {
				return // model not available, stay not-ready
			}
			vecs[j] = vec
		}
		ic.anchors[i].Vecs = vecs
	}
	ic.mu.Lock()
	ic.ready = true
	ic.mu.Unlock()
}

// Ready returns true when anchor embeddings have been computed.
func (ic *IntentClassifier) Ready() bool {
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	return ic.ready
}

// SetLLMFunc sets the optional Layer 3 LLM classification callback.
// When set, ambiguous Layer 2 results will be refined by the LLM.
func (ic *IntentClassifier) SetLLMFunc(fn LLMClassifyFunc) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.llmFunc = fn
}

func (ic *IntentClassifier) getLLMFunc() LLMClassifyFunc {
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	return ic.llmFunc
}

// ── Classification ──────────────────────────────────────────────────────────

// Classify determines the intent of a user message.
func (ic *IntentClassifier) Classify(text string) IntentResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return IntentResult{Intent: IntentUnknown, Layer: 1}
	}

	// Layer 1: rule-based fast path.
	if r := ic.classifyByRules(text); r.Intent != IntentUnknown {
		return r
	}

	// Layer 2: embedding similarity (if available).
	var embResult IntentResult
	embAvailable := ic.Ready() && ic.queryCache != nil
	embAmbiguous := false

	if embAvailable {
		embResult = ic.classifyByEmbedding(text)
		if embResult.Intent != IntentUnknown && embResult.Confidence >= embeddingHighThreshold {
			return embResult // high confidence, no need for LLM
		}
		if embResult.Intent != IntentUnknown {
			embAmbiguous = true // has a guess but low confidence
		}
	}

	// Layer 3: LLM classification for ambiguous or unresolved cases.
	if fn := ic.getLLMFunc(); fn != nil {
		if r := ic.classifyByLLM(text, fn); r.Intent != IntentUnknown {
			return r
		}
	}

	// Fall back to ambiguous embedding result if available.
	if embAmbiguous {
		return embResult
	}

	return IntentResult{Intent: IntentUnknown, Layer: 1}
}

// ── Layer 1: Rules ──────────────────────────────────────────────────────────

func (ic *IntentClassifier) classifyByRules(text string) IntentResult {
	lower := strings.ToLower(text)

	// 1a. Question pattern detection — must come BEFORE short command check,
	// because some questions are short (e.g. "什么是服务器" = 5 runes).
	if questionPatternsCN.MatchString(text) || questionSuffixCN.MatchString(text) ||
		questionMidCN.MatchString(text) || questionPatternsEN.MatchString(lower) {
		return IntentResult{Intent: IntentQuery, Confidence: 0.85, Layer: 1}
	}

	// 1b. Short command detection (≤ shortCommandMaxRunes runes).
	if utf8.RuneCountInString(text) <= shortCommandMaxRunes {
		return IntentResult{Intent: IntentShortCommand, Confidence: 0.5, Layer: 1}
	}

	// 1c. No high-confidence keyword match at Layer 1.
	// We intentionally do NOT replicate the full keyword lists here;
	// the existing keyword matching in router.go / im_tools_session.go
	// continues to work as before. This classifier is an additional signal.
	return IntentResult{Intent: IntentUnknown, Layer: 1}
}

// ── Layer 2: Embedding ──────────────────────────────────────────────────────

func (ic *IntentClassifier) classifyByEmbedding(text string) IntentResult {
	queryVec, err := ic.queryCache.Get(text)
	if err != nil || queryVec == nil {
		return IntentResult{Intent: IntentUnknown, Layer: 2}
	}

	type scored struct {
		name  string
		score float64
	}
	var results []scored

	for _, anchor := range ic.anchors {
		var maxSim float64
		for _, anchorVec := range anchor.Vecs {
			sim := CosineSimilarity(queryVec, anchorVec)
			if sim > maxSim {
				maxSim = sim
			}
		}
		results = append(results, scored{anchor.Name, maxSim})
	}

	// Sort descending by score.
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) == 0 {
		return IntentResult{Intent: IntentUnknown, Layer: 2}
	}

	best := results[0]
	gap := 0.0
	if len(results) >= 2 {
		gap = results[0].score - results[1].score
	}

	// High confidence: score above threshold AND sufficient gap.
	if best.score >= embeddingHighThreshold && gap >= embeddingMinGap {
		return IntentResult{
			Intent:     best.name,
			Confidence: best.score,
			Gap:        gap,
			Layer:      2,
		}
	}

	// Below low threshold: no match.
	if best.score < embeddingLowThreshold {
		return IntentResult{Intent: IntentUnknown, Confidence: best.score, Gap: gap, Layer: 2}
	}

	// Ambiguous zone: return the best guess with low confidence.
	return IntentResult{
		Intent:     best.name,
		Confidence: best.score * 0.6, // discount to signal ambiguity
		Gap:        gap,
		Layer:      2,
	}
}

// ── Layer 3: LLM ────────────────────────────────────────────────────────────

const llmClassifyPrompt = `你是一个意图分类器。根据用户消息判断意图类别，只返回一个单词，不要解释。

类别：
- coding: 写代码、开发应用、修bug、重构、设计架构等编程操作
- ssh: 登录服务器、查看日志、重启服务、上传文件等远程服务器操作
- content: 翻译、整理、总结、格式化文字、收集资料等内容处理
- chat: 打招呼、闲聊、感谢等日常对话
- browser: 打开浏览器、录制网页操作、点击网页元素等浏览器操作
- query: 概念解释、知识问答、工具使用方法等不涉及实际操作的提问
- unknown: 无法判断

用户消息: %s

类别:`

// validLLMIntents maps LLM response text to canonical intent constants.
var validLLMIntents = map[string]string{
	"coding":  IntentCoding,
	"ssh":     IntentSSH,
	"content": IntentContent,
	"chat":    IntentChat,
	"browser": IntentBrowser,
	"query":   IntentQuery,
	"unknown": IntentUnknown,
}

func (ic *IntentClassifier) classifyByLLM(text string, fn LLMClassifyFunc) IntentResult {
	prompt := fmt.Sprintf(llmClassifyPrompt, text)
	resp, err := fn(prompt)
	if err != nil {
		return IntentResult{Intent: IntentUnknown, Layer: 3}
	}

	// Parse the LLM response — expect a single word.
	resp = strings.TrimSpace(strings.ToLower(resp))
	// Strip any punctuation or extra text.
	resp = strings.TrimRight(resp, ".,;:!?。，；：！？")
	// Take only the first word/line.
	if idx := strings.IndexAny(resp, " \t\n\r"); idx > 0 {
		resp = resp[:idx]
	}

	if intent, ok := validLLMIntents[resp]; ok && intent != IntentUnknown {
		return IntentResult{
			Intent:     intent,
			Confidence: 0.90, // LLM classification is high confidence
			Layer:      3,
		}
	}

	return IntentResult{Intent: IntentUnknown, Layer: 3}
}

// ── Helpers ─────────────────────────────────────────────────────────────────
