package knowledge

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

const (
	knowledgeRetrievalAuto   = "auto"
	knowledgeRetrievalFTS    = "fts"
	knowledgeRetrievalVector = "vector"
	knowledgeRetrievalHybrid = "hybrid"
)

// canonicalKnowledgeRetrieval accepts the empty value as auto. Any other
// unknown token is an error so a typo cannot silently take the heuristic path.
func canonicalKnowledgeRetrieval(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", knowledgeRetrievalAuto:
		return knowledgeRetrievalAuto, nil
	case knowledgeRetrievalFTS, knowledgeRetrievalVector, knowledgeRetrievalHybrid:
		return strings.ToLower(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("unknown knowledge retrieval %q", strings.TrimSpace(raw))
	}
}

// QuerySuppressesEmbedding reports that the query is a composite or unknown
// name. Semantic neighbors of that name are a different record, so every
// retrieval route stays on the lexical hits.
func QuerySuppressesEmbedding(query string) bool {
	_, strict := bm25.QueryAnchors(normalizeKnowledgeLexicalText(query))
	return strict
}

type knowledgeRetrievalDecision struct {
	runEmbedding bool
	fuse         bool
}

// knowledgeRetrievalPlan decides the vector half of Search after lexical
// retrieval has already run. auto keeps the historical heuristic: embed when
// nothing matched, the caller asked for it, the query has no word boundaries,
// or the best non-node lexical score is below its type-adjusted threshold.
// A strict name suppresses embedding on every route, including vector.
func knowledgeRetrievalPlan(opts SearchOptions, results []SearchResult, hasEmbedder bool) knowledgeRetrievalDecision {
	if !hasEmbedder || QuerySuppressesEmbedding(opts.Query) {
		return knowledgeRetrievalDecision{}
	}
	switch opts.Retrieval {
	case knowledgeRetrievalFTS:
		return knowledgeRetrievalDecision{}
	case knowledgeRetrievalVector:
		return knowledgeRetrievalDecision{runEmbedding: true, fuse: false}
	case knowledgeRetrievalHybrid:
		return knowledgeRetrievalDecision{runEmbedding: true, fuse: true}
	default:
		bestFTSScore := 0.0
		bestResultType := ""
		for _, result := range results {
			if result.Score > bestFTSScore && result.ResultType != "node" {
				bestFTSScore = result.Score
				bestResultType = result.ResultType
			}
		}
		needsEmbedding := len(results) == 0 || opts.PreferEmbedding || containsNoSpaceScriptRunes(opts.Query)
		threshold := 2.0 - searchResultTypeBonus(bestResultType)
		if !needsEmbedding && bestFTSScore < threshold {
			needsEmbedding = true
		}
		return knowledgeRetrievalDecision{runEmbedding: needsEmbedding, fuse: needsEmbedding}
	}
}
