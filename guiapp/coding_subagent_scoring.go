package guiapp

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
	embBaseline := codingEmbeddingBaseline(emb)
	if emb != nil && !embedding.IsNoop(emb) {
		if vec, err := embedCodingQuery(emb, taskDescription); err == nil && len(vec) > 0 {
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
				embScore = raw - embBaseline
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

// Qwen3 cosine baselines. Qwen3's cosine scale differs from Gemma's; the
// query side carries the RoleQuery instruction when the embedder supports
// prompts. Calibrated on the TRAIN half of the 305-task cross-family
// coding-skill set (2026-10-07 replacement audit, out/replace_audit/int3/
// coding_r3.txt): RoleQuery task + plain doc, train F1 0.673 at 0.40 (plain
// Embed both sides: best 0.594 at 0.48); test F1 0.664 vs Gemma 0.640.
const (
	codingSubAgentEmbeddingBaselineQwen3      = 0.40
	codingSubAgentEmbeddingBaselineQwen3Plain = 0.48
)

// codingEmbeddingPolicy returns whether the task side is embedded with the
// RoleQuery instruction and the matching cosine baseline for emb.
func codingEmbeddingPolicy(emb embedding.Embedder) (queryRole bool, baseline float64) {
	if embedding.EmbedderFamily(emb) != embedding.FamilyQwen3 {
		return false, codingSubAgentEmbeddingBaseline
	}
	if _, ok := emb.(embedding.RoleEmbedder); ok {
		return true, codingSubAgentEmbeddingBaselineQwen3
	}
	return false, codingSubAgentEmbeddingBaselineQwen3Plain
}

// codingEmbeddingBaseline returns the cosine baseline for emb.
func codingEmbeddingBaseline(emb embedding.Embedder) float64 {
	_, b := codingEmbeddingPolicy(emb)
	return b
}

// embedCodingQuery embeds the task side (documents stay plain EmbedBatch).
func embedCodingQuery(emb embedding.Embedder, task string) ([]float32, error) {
	if role, _ := codingEmbeddingPolicy(emb); role {
		return embedding.EmbedAs(emb, task, embedding.RoleQuery)
	}
	return emb.Embed(task)
}
