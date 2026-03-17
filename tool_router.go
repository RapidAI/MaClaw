package main

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	builtinToolCount = 14 // first 14 tools are always builtins (12 original + parallel_execute + recommend_tool)
	routeThreshold   = 20 // only filter when total tools exceed this
	maxDynamicRouted = 15 // max dynamic tools to keep after filtering
)

// ToolRouter selects the most relevant tools for a given user message.
// When the total tool count exceeds routeThreshold, it keeps all builtin
// tools and selects the top dynamic tools ranked by TF-IDF similarity.
type ToolRouter struct {
	generator *ToolDefinitionGenerator
}

// NewToolRouter creates a new ToolRouter.
func NewToolRouter(generator *ToolDefinitionGenerator) *ToolRouter {
	return &ToolRouter{generator: generator}
}

// Route filters allTools based on relevance to userMessage.
// - If len(allTools) <= 20, returns allTools unchanged.
// - Otherwise, keeps the first 12 builtins + top 15 dynamic tools ranked
//   by keyword match + TF-IDF similarity to the user message.
func (r *ToolRouter) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	if len(allTools) <= routeThreshold {
		return allTools
	}

	// Split into builtins and dynamic tools.
	builtinCount := builtinToolCount
	if builtinCount > len(allTools) {
		builtinCount = len(allTools)
	}
	builtins := allTools[:builtinCount]
	dynamicTools := allTools[builtinCount:]

	if len(dynamicTools) == 0 {
		return builtins
	}

	// Tokenize user message.
	msgTokens := tokenize(userMessage)
	if len(msgTokens) == 0 {
		// No meaningful tokens — return builtins + first N dynamic tools.
		limit := maxDynamicRouted
		if limit > len(dynamicTools) {
			limit = len(dynamicTools)
		}
		result := make([]map[string]interface{}, len(builtins), len(builtins)+limit)
		copy(result, builtins)
		return append(result, dynamicTools[:limit]...)
	}

	// Build IDF from all dynamic tool documents.
	allDocs := make([][]string, len(dynamicTools))
	for i, tool := range dynamicTools {
		allDocs[i] = toolTokens(tool)
	}
	idf := computeIDF(allDocs)

	// Score each dynamic tool.
	type scored struct {
		index int
		score float64
	}
	scores := make([]scored, len(dynamicTools))
	for i, doc := range allDocs {
		scores[i] = scored{
			index: i,
			score: tfidfSimilarity(msgTokens, doc, idf),
		}
	}

	// Sort by score descending; stable sort preserves original order for ties.
	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Take top N dynamic tools.
	limit := maxDynamicRouted
	if limit > len(scores) {
		limit = len(scores)
	}

	result := make([]map[string]interface{}, len(builtins), len(builtins)+limit)
	copy(result, builtins)
	for i := 0; i < limit; i++ {
		result = append(result, dynamicTools[scores[i].index])
	}
	return result
}


// ---------------------------------------------------------------------------
// Tokenization
// ---------------------------------------------------------------------------

// tokenize splits text into lowercase tokens by whitespace and punctuation.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || r == '_' || r == '-'
	})
	// Filter out very short tokens (single char) that add noise.
	result := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if len(t) > 1 {
			result = append(result, t)
		}
	}
	return result
}

// toolTokens extracts tokens from a tool definition's name and description.
func toolTokens(def map[string]interface{}) []string {
	name := extractToolName(def)
	desc := extractToolDescription(def)
	combined := name + " " + desc
	return tokenize(combined)
}

// extractToolDescription extracts the description from an OpenAI function
// calling tool definition.
func extractToolDescription(def map[string]interface{}) string {
	fn, ok := def["function"]
	if !ok {
		return ""
	}
	fnMap, ok := fn.(map[string]interface{})
	if !ok {
		return ""
	}
	desc, _ := fnMap["description"].(string)
	return desc
}

// ---------------------------------------------------------------------------
// TF-IDF Computation
// ---------------------------------------------------------------------------

// termFrequency computes the term frequency of each token in a document.
// TF(t, d) = count(t in d) / len(d)
func termFrequency(tokens []string) map[string]float64 {
	counts := make(map[string]int, len(tokens))
	for _, t := range tokens {
		counts[t]++
	}
	tf := make(map[string]float64, len(counts))
	n := float64(len(tokens))
	if n == 0 {
		return tf
	}
	for t, c := range counts {
		tf[t] = float64(c) / n
	}
	return tf
}

// computeIDF computes inverse document frequency across all documents.
// IDF(t) = log(N / (1 + df(t)))  where df(t) = number of docs containing t.
func computeIDF(docs [][]string) map[string]float64 {
	df := make(map[string]int)
	for _, doc := range docs {
		seen := make(map[string]bool, len(doc))
		for _, t := range doc {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
	}
	n := float64(len(docs))
	idf := make(map[string]float64, len(df))
	for t, count := range df {
		idf[t] = math.Log(n / (1.0 + float64(count)))
	}
	return idf
}

// tfidfVector computes the TF-IDF vector for a document given precomputed IDF.
func tfidfVector(tokens []string, idf map[string]float64) map[string]float64 {
	tf := termFrequency(tokens)
	vec := make(map[string]float64, len(tf))
	for t, tfVal := range tf {
		idfVal, ok := idf[t]
		if !ok {
			// Term not in any document corpus — use a default IDF.
			idfVal = math.Log(float64(len(idf)) + 1.0)
		}
		vec[t] = tfVal * idfVal
	}
	return vec
}

// tfidfSimilarity computes the cosine similarity between the user message
// tokens and a tool document's tokens using TF-IDF weighting.
func tfidfSimilarity(queryTokens, docTokens []string, idf map[string]float64) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}

	qVec := tfidfVector(queryTokens, idf)
	dVec := tfidfVector(docTokens, idf)

	// Cosine similarity = dot(q, d) / (|q| * |d|)
	var dot, qNorm, dNorm float64
	for t, qVal := range qVec {
		if dVal, ok := dVec[t]; ok {
			dot += qVal * dVal
		}
		qNorm += qVal * qVal
	}
	for _, dVal := range dVec {
		dNorm += dVal * dVal
	}

	denom := math.Sqrt(qNorm) * math.Sqrt(dNorm)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
