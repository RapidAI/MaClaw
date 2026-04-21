package intent

import "sort"

// ---------------------------------------------------------------------------
// Dual-channel fusion algorithm — ported from intent-fusion's router.py.
//
// Formula: final_score = α × emb_score + (1-α) × tree_score
//
// Candidates appearing in both channels naturally score higher (cross-channel
// agreement) without any special bonus term.
// ---------------------------------------------------------------------------

// MergeAndScore unions candidates from both channels and computes fused scores.
//
// embTop: top-k candidates from embedding channel (label → score).
// treeTop: top candidates from tree channel (label → score).
// alpha: embedding channel weight [0, 1].
func MergeAndScore(embTop []labelScore, treeTop []labelScore, alpha float64) []FusedCandidate {
	cands := make(map[IntentLabel]*FusedCandidate)

	for _, ls := range embTop {
		cands[ls.label] = &FusedCandidate{
			Label:    ls.label,
			EmbScore: ls.score,
			InEmb:    true,
		}
	}

	for _, ls := range treeTop {
		if c, ok := cands[ls.label]; ok {
			c.TreeScore = ls.score
			c.InTree = true
		} else {
			cands[ls.label] = &FusedCandidate{
				Label:     ls.label,
				TreeScore: ls.score,
				InTree:    true,
			}
		}
	}

	result := make([]FusedCandidate, 0, len(cands))
	for _, c := range cands {
		c.FinalScore = alpha*c.EmbScore + (1-alpha)*c.TreeScore
		result = append(result, *c)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].FinalScore > result[j].FinalScore
	})

	return result
}

// Decide produces a three-state verdict from fused candidates.
//
// CLEAR:     top score >= lowThreshold and gap to runner-up >= delta
// AMBIGUOUS: top score >= lowThreshold but gap < delta
// LOW:       top score < lowThreshold (no confident match)
func Decide(candidates []FusedCandidate, cfg FusionConfig) FusionResult {
	if len(candidates) == 0 {
		return FusionResult{Verdict: VerdictLow}
	}

	top := candidates[0]

	if top.FinalScore < cfg.LowThreshold {
		return FusionResult{
			Verdict:    VerdictLow,
			Top:        top,
			Candidates: candidates,
		}
	}

	if len(candidates) < 2 {
		return FusionResult{
			Verdict:    VerdictClear,
			Top:        top,
			Candidates: candidates,
		}
	}

	runnerUp := candidates[1]
	gap := top.FinalScore - runnerUp.FinalScore

	verdict := VerdictClear
	if gap < cfg.Delta {
		verdict = VerdictAmbiguous
	}

	result := FusionResult{
		Verdict:    verdict,
		Top:        top,
		Candidates: candidates,
	}
	if verdict == VerdictAmbiguous {
		result.RunnerUp = &runnerUp
	}

	return result
}

// labelScore is a (label, score) pair used by MergeAndScore.
// Exported as a type alias for use by the embedding and tree channels.
type labelScore struct {
	label IntentLabel
	score float64
}

// LabelScore creates a labelScore pair. Used by channel implementations.
func LabelScore(label IntentLabel, score float64) labelScore {
	return labelScore{label: label, score: score}
}
