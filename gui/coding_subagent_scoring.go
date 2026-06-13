package main

// coding_subagent_scoring.go provides shared scoring infrastructure for
// task-aware tool/skill selection in the CodingSubAgent.
//
// Both coding_subagent_skills.go and coding_subagent_mcp.go use three-signal
// fusion (BM25 + bigram Jaccard + embedding cosine) to match task descriptions
// against candidate tool/skill descriptions. This file extracts the common
// scoring logic to avoid duplication.

import (
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// scoredCandidate holds an index and its computed relevance score.
type scoredCandidate struct {
	Idx   int
	Score float64
}

// scoreAndSelectTopK runs three-signal fusion on a set of candidate documents
// against a task description, returning the top-K candidates above threshold.
//
// Parameters:
//   - taskDescription: the query text (task title + description)
//   - docs: candidate documents to score (one per candidate)
//   - emb: optional embedder (nil = skip embedding signal)
//   - maxResults: maximum number of results to return
//   - threshold: minimum score to include
//
// Returns indices and scores of selected candidates, sorted descending.
func scoreAndSelectTopK(taskDescription string, docs []string, emb embedding.Embedder, maxResults int, threshold float64) []scoredCandidate {
	if len(docs) == 0 || len(taskDescription) == 0 {
		return nil
	}

	// Signal 1: BM25 tokens
	queryTokens := cskill.TokenizeSimple(taskDescription)

	// Signal 2: Character bigrams
	queryBigrams := extractBigrams(strings.ToLower(taskDescription))

	// Signal 3: Embedding vectors (optional)
	var queryEmbed []float32
	var docEmbeddings map[int][]float32
	if emb != nil && !embedding.IsNoop(emb) {
		if vec, err := emb.Embed(taskDescription); err == nil && len(vec) > 0 {
			queryEmbed = vec
		}
		if queryEmbed != nil {
			if vectors, err := emb.EmbedBatch(docs); err == nil && len(vectors) == len(docs) {
				docEmbeddings = make(map[int][]float32, len(docs))
				for i, vec := range vectors {
					if len(vec) > 0 {
						docEmbeddings[i] = vec
					}
				}
			}
		}
	}

	// Score each candidate.
	var results []scoredCandidate
	for i, doc := range docs {
		docLower := strings.ToLower(doc)

		// BM25
		docTokens := cskill.TokenizeSimple(doc)
		bm25Score := cskill.BM25ScoreSimple(queryTokens, docTokens)

		// Bigram Jaccard
		docBigrams := extractBigrams(docLower)
		bigramScore := bigramJaccard(queryBigrams, docBigrams)

		// Embedding cosine (calibrated)
		var embScore float64
		if queryEmbed != nil {
			if docVec, ok := docEmbeddings[i]; ok {
				raw := float64(cosine32(queryEmbed, docVec))
				embScore = raw - codingSubAgentEmbeddingBaseline
				if embScore < 0 {
					embScore = 0
				}
			}
		}

		// Take max signal.
		score := bm25Score
		if bigramScore > score {
			score = bigramScore
		}
		if embScore > score {
			score = embScore
		}

		if score >= threshold {
			results = append(results, scoredCandidate{Idx: i, Score: score})
		}
	}

	if len(results) == 0 {
		return nil
	}

	// Sort descending, take top-K.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}
