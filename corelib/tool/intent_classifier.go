package tool

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

const (
	IntentCoding       = "coding"
	IntentSSH          = "ssh"
	IntentContent      = "content"
	IntentChat         = "chat"
	IntentBrowser      = "browser"
	IntentQuery        = "query"
	IntentShortCommand = "short_command"
	IntentUnknown      = "unknown"
)

type IntentResult struct {
	Intent     string
	Confidence float64
	Gap        float64
	Layer      int
}

type LLMClassifyFunc func(prompt string) (string, error)

type intentAnchor struct {
	Name  string
	Texts []string
	Vecs  [][]float32
}

// IntentClassifier classifies with semantic layers only:
// Layer 1 is limited to structural signals that do not identify a domain.
// Layer 2 uses embeddings over representative anchor utterances.
// Layer 3 delegates unresolved cases to an LLM.
type IntentClassifier struct {
	embedder   embedding.Embedder
	anchors    []intentAnchor
	queryCache *QueryEmbeddingCache
	llmFunc    LLMClassifyFunc
	ready      bool
	warmupDone chan struct{}
	mu         sync.RWMutex
}

const shortCommandMaxRunes = 2

func defaultAnchors() []intentAnchor {
	return []intentAnchor{
		{
			Name: IntentCoding,
			Texts: []string{
				"create a software application",
				"fix a bug in source code",
				"refactor an existing codebase",
				"implement a backend API",
				"write automated tests for a project",
				"design a software architecture",
				"modify frontend components",
				"debug a failing build",
			},
		},
		{
			Name: IntentSSH,
			Texts: []string{
				"connect to a remote server",
				"inspect server logs over ssh",
				"restart a remote service",
				"upload a file to a server",
				"download a file from a host",
				"check resource usage on a remote machine",
				"run a command on production infrastructure",
			},
		},
		{
			Name: IntentContent,
			Texts: []string{
				"translate a document",
				"summarize an article",
				"organize notes into a report",
				"format text into a presentation",
				"collect source material",
				"rewrite prose for clarity",
			},
		},
		{
			Name: IntentChat,
			Texts: []string{
				"say hello",
				"casual conversation",
				"ask what you can do",
				"thank you",
				"tell a joke",
			},
		},
		{
			Name: IntentBrowser,
			Texts: []string{
				"open a website in the browser",
				"click a button on a web page",
				"record a browser workflow",
				"replay browser automation",
				"verify a web page visually",
			},
		},
		{
			Name: IntentQuery,
			Texts: []string{
				"explain a concept",
				"answer a knowledge question",
				"describe how a tool works",
				"compare two technologies",
				"teach me about a topic",
			},
		},
	}
}

const (
	embeddingHighThreshold = 0.78
	embeddingLowThreshold  = 0.55
	embeddingMinGap        = 0.10
)

func NewIntentClassifier(emb embedding.Embedder) *IntentClassifier {
	ic := &IntentClassifier{
		embedder:   emb,
		anchors:    defaultAnchors(),
		warmupDone: make(chan struct{}),
	}
	if emb != nil && !embedding.IsNoop(emb) {
		ic.queryCache = NewQueryEmbeddingCache(emb, 64, 30*time.Second)
		go ic.warmAnchors()
	} else {
		close(ic.warmupDone)
	}
	return ic
}

func (ic *IntentClassifier) warmAnchors() {
	defer close(ic.warmupDone)
	defer func() {
		if recover() != nil {
			// Stay not-ready; callers will skip Layer 2.
		}
	}()
	for i := range ic.anchors {
		vecs := make([][]float32, len(ic.anchors[i].Texts))
		for j, text := range ic.anchors[i].Texts {
			vec, err := ic.embedder.Embed(text)
			if err != nil {
				return
			}
			vecs[j] = vec
		}
		ic.anchors[i].Vecs = vecs
	}
	ic.mu.Lock()
	ic.ready = true
	ic.mu.Unlock()
}

func (ic *IntentClassifier) Ready() bool {
	if ic == nil {
		return false
	}
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	return ic.ready
}

func (ic *IntentClassifier) WaitReady(timeout time.Duration) bool {
	if ic == nil {
		return false
	}
	if ic.warmupDone == nil {
		return ic.Ready()
	}
	if ic.Ready() {
		return true
	}
	if timeout <= 0 {
		select {
		case <-ic.warmupDone:
		default:
		}
		return ic.Ready()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ic.warmupDone:
	case <-timer.C:
	}
	return ic.Ready()
}

func (ic *IntentClassifier) Close() {
	if ic == nil || ic.warmupDone == nil {
		return
	}
	<-ic.warmupDone
}

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

func (ic *IntentClassifier) Classify(text string) IntentResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return IntentResult{Intent: IntentUnknown, Layer: 1}
	}
	if r := ic.classifyByRules(text); r.Intent != IntentUnknown {
		return r
	}

	var embResult IntentResult
	embAvailable := ic.Ready() && ic.queryCache != nil
	embAmbiguous := false
	if embAvailable {
		embResult = ic.classifyByEmbedding(text)
		if embResult.Intent != IntentUnknown && embResult.Confidence >= embeddingHighThreshold {
			return embResult
		}
		if embResult.Intent != IntentUnknown {
			embAmbiguous = true
		}
	}

	if fn := ic.getLLMFunc(); fn != nil {
		if r := ic.classifyByLLM(text, fn); r.Intent != IntentUnknown {
			return r
		}
	}
	if embAmbiguous {
		return embResult
	}
	return IntentResult{Intent: IntentUnknown, Layer: 1}
}

func (ic *IntentClassifier) classifyByRules(text string) IntentResult {
	if utf8.RuneCountInString(strings.TrimSpace(text)) <= shortCommandMaxRunes {
		return IntentResult{Intent: IntentShortCommand, Confidence: 0.5, Layer: 1}
	}
	return IntentResult{Intent: IntentUnknown, Layer: 1}
}

func (ic *IntentClassifier) classifyByEmbedding(text string) IntentResult {
	queryVec, err := ic.queryCache.Get(text)
	if err != nil || queryVec == nil {
		return IntentResult{Intent: IntentUnknown, Layer: 2}
	}

	type scored struct {
		name  string
		score float64
	}
	results := make([]scored, 0, len(ic.anchors))
	for _, anchor := range ic.anchors {
		var maxSim float64
		for _, anchorVec := range anchor.Vecs {
			if sim := CosineSimilarity(queryVec, anchorVec); sim > maxSim {
				maxSim = sim
			}
		}
		results = append(results, scored{name: anchor.Name, score: maxSim})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
	if len(results) == 0 {
		return IntentResult{Intent: IntentUnknown, Layer: 2}
	}

	best := results[0]
	gap := 0.0
	if len(results) >= 2 {
		gap = results[0].score - results[1].score
	}
	if best.score >= embeddingHighThreshold && gap >= embeddingMinGap {
		return IntentResult{Intent: best.name, Confidence: best.score, Gap: gap, Layer: 2}
	}
	if best.score < embeddingLowThreshold {
		return IntentResult{Intent: IntentUnknown, Confidence: best.score, Gap: gap, Layer: 2}
	}
	return IntentResult{Intent: best.name, Confidence: best.score * 0.6, Gap: gap, Layer: 2}
}

const llmClassifyPrompt = `Classify the user's execution intent.

Return exactly one label from:
coding, ssh, content, chat, browser, query, unknown.

Classify by the action required, not by isolated words.

User message: %s

Label:`

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
	resp, err := fn(fmt.Sprintf(llmClassifyPrompt, text))
	if err != nil {
		return IntentResult{Intent: IntentUnknown, Layer: 3}
	}
	resp = strings.TrimSpace(strings.ToLower(resp))
	resp = strings.TrimRight(resp, ".,;:!?")
	if idx := strings.IndexAny(resp, " \t\n\r"); idx > 0 {
		resp = resp[:idx]
	}
	if intent, ok := validLLMIntents[resp]; ok && intent != IntentUnknown {
		return IntentResult{Intent: intent, Confidence: 0.90, Layer: 3}
	}
	return IntentResult{Intent: IntentUnknown, Layer: 3}
}
