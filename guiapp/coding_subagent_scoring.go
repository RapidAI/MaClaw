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
	return scoreAndSelectTopKWithEmbedDocs(taskDescription, docs, nil, emb, maxResults, threshold)
}

// scoreAndSelectTopKWithEmbedDocs is scoreAndSelectTopK with an optional
// structured document text per candidate for the embedding signal only
// (embedDocs[i] for docs[i]; nil or a length mismatch means docs). BM25 and
// bigram always score docs. Only the instructed Qwen3 policy uses embedDocs.
func scoreAndSelectTopKWithEmbedDocs(taskDescription string, docs, embedDocs []string, emb embedding.Embedder, maxResults int, threshold float64) []scoredCandidate {
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
	pol := codingEmbeddingPolicyFor(emb)
	embBaseline := pol.baseline
	if emb != nil && !embedding.IsNoop(emb) {
		if vec, err := embedCodingQuery(emb, taskDescription); err == nil && len(vec) > 0 {
			queryEmbed = vec
		}
		if queryEmbed != nil {
			docTexts := docs
			if pol.instructed && len(embedDocs) == len(docs) {
				docTexts = embedDocs
			}
			if vectors, err := emb.EmbedBatch(docTexts); err == nil && len(vectors) == len(docs) {
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
		bm25Score := pol.bm25Weight * cskill.BM25ScoreSimple(queryTokens, docTokens)

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

// Qwen3 coding-subagent policy. Qwen3's cosine scale differs from Gemma's.
// Round 3 (out/replace_audit/int3/coding_r3.txt): RoleQuery task + plain doc,
// baseline 0.40. Round 4 (out/replace_audit/r4C/select_c.json), chosen on the
// TRAIN half (n=140) of the 305-task cross-family coding-skill set only:
// task side carries a coding-specific instruction (CT3), the embedding-side
// document is "Tool: <name>\nDescription: <description>", baseline 0.50 and
// BM25 weight 0.6 in the max-fusion (train cosine MRR 0.940 -> 0.956,
// calibrated F1 0.673 -> 0.731; flat for BM25 weight 0.4-0.7). Gemma and
// Qwen3 without prompt support are unchanged.
const (
	codingSubAgentEmbeddingBaselineQwen3      = 0.50
	codingSubAgentEmbeddingBaselineQwen3Plain = 0.48
	codingSubAgentBM25WeightQwen3             = 0.6
	codingSubAgentQwen3TaskInstruction        = "Given a programming task description, retrieve descriptions of the tools a developer would use for it"
)

type codingEmbedPolicy struct {
	instructed bool    // Qwen3 with prompt support: instructed task + structured docs
	baseline   float64 // cosine baseline subtracted before fusion
	bm25Weight float64 // BM25 multiplier in the max-fusion
}

func codingEmbeddingPolicyFor(emb embedding.Embedder) codingEmbedPolicy {
	if emb == nil || embedding.EmbedderFamily(emb) != embedding.FamilyQwen3 {
		return codingEmbedPolicy{baseline: codingSubAgentEmbeddingBaseline, bm25Weight: 1}
	}
	if _, ok := emb.(embedding.RoleEmbedder); ok {
		return codingEmbedPolicy{instructed: true, baseline: codingSubAgentEmbeddingBaselineQwen3, bm25Weight: codingSubAgentBM25WeightQwen3}
	}
	return codingEmbedPolicy{baseline: codingSubAgentEmbeddingBaselineQwen3Plain, bm25Weight: 1}
}

// codingEmbeddingPolicy returns whether the task side is instructed and the
// matching cosine baseline for emb.
func codingEmbeddingPolicy(emb embedding.Embedder) (instructed bool, baseline float64) {
	p := codingEmbeddingPolicyFor(emb)
	return p.instructed, p.baseline
}

// codingEmbeddingBaseline returns the cosine baseline for emb.
func codingEmbeddingBaseline(emb embedding.Embedder) float64 {
	return codingEmbeddingPolicyFor(emb).baseline
}

// codingQwen3EmbedDoc is the structured embedding-side document text.
func codingQwen3EmbedDoc(name, description string) string {
	return "Tool: " + strings.TrimSpace(name) + "\nDescription: " + strings.TrimSpace(description)
}

// embedCodingQuery embeds the task side (documents stay plain EmbedBatch).
func embedCodingQuery(emb embedding.Embedder, task string) ([]float32, error) {
	if codingEmbeddingPolicyFor(emb).instructed {
		return emb.Embed("Instruct: " + codingSubAgentQwen3TaskInstruction + "\nQuery: " + task)
	}
	return emb.Embed(task)
}
