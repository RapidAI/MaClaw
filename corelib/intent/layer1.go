package intent

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// classifyByKeywords performs Layer 1 keyword-based classification.
// Returns (result, true) when confident (confidence >= 0.90), or (result, false) to escalate.
func classifyByKeywords(registry *KeywordRegistry, affinity *ToolAffinityRegistry, msg MessageContext) (ClassificationResult, bool) {
	text := strings.TrimSpace(msg.Text)

	// Empty message → immediate return.
	if text == "" {
		return ClassificationResult{
			Primary:    LabelUnknown,
			Confidence: 0,
			Layer:      1,
			Reason:     "empty message",
		}, true
	}

	// Match all keywords.
	matches := registry.Match(text)

	// No matches → low confidence unknown.
	if len(matches) == 0 {
		// Check continuation for short messages with context.
		if isContinuationByContext(msg) {
			result := ClassificationResult{
				Primary:    LabelContinuation,
				Confidence: 0.85,
				Layer:      1,
				Reason:     "short message with coding context in history",
				ToolNames:  affinity.Resolve(LabelContinuation, nil),
			}
			return result, false // below 0.90, escalate
		}
		return ClassificationResult{
			Primary:    LabelUnknown,
			Confidence: 0,
			Layer:      1,
			Reason:     "no keyword matches",
		}, false
	}

	// Group matches by label, counting strong and weak hits.
	scores := make(map[IntentLabel]*keywordLabelScore)
	hasCreationKeyword := false
	for _, m := range matches {
		ls, ok := scores[m.Entry.Label]
		if !ok {
			ls = &keywordLabelScore{}
			scores[m.Entry.Label] = ls
		}
		if m.Entry.Strength == Strong {
			ls.strong++
		} else {
			ls.weak++
		}
		if m.Entry.Creation {
			hasCreationKeyword = true
		}
	}

	// Apply mixed-intent dominance rules (Requirement 5.3).
	applyDominanceRules(scores)

	// Browser two-tier detection (Requirement 5.4).
	if bs, hasBrowser := scores[LabelBrowser]; hasBrowser {
		if bs.strong > 0 {
			// Strong browser keywords → high confidence.
			result := buildResult(LabelBrowser, 0.92, scores, affinity, "strong browser keyword match", false)
			return result, true
		}
		// Weak browser keywords only → low confidence, needs Layer 2 confirmation.
		if bs.weak >= 2 {
			result := buildResult(LabelBrowser, 0.55, scores, affinity, "weak browser keyword combination (page + action words)", false)
			return result, false
		}
		// Single weak browser keyword → remove from contention.
		if bs.weak == 1 && bs.strong == 0 {
			delete(scores, LabelBrowser)
		}
	}

	// If no labels remain after filtering, escalate.
	if len(scores) == 0 {
		return ClassificationResult{
			Primary:    LabelUnknown,
			Confidence: 0,
			Layer:      1,
			Reason:     "no confident keyword matches after filtering",
		}, false
	}

	// Select winner using conflict resolution priority.
	winner := selectWinner(scores)

	// Compute confidence based on match strength.
	confidence := computeConfidence(scores[winner])

	// Continuation detection for short messages (≤10 runes) with conversation context.
	if winner == LabelContinuation && confidence < 0.90 {
		if isContinuationByContext(msg) {
			confidence = 0.92
		}
	}

	reason := fmt.Sprintf("keyword match: %s (strong=%d, weak=%d)",
		winner, scores[winner].strong, scores[winner].weak)

	result := buildResult(winner, confidence, scores, affinity, reason, hasCreationKeyword)
	return result, confidence >= 0.90
}

// keywordLabelScore tracks strong and weak keyword hits for a label.
type keywordLabelScore struct {
	strong int
	weak   int
}

// applyDominanceRules applies mixed-intent dominance rules (Requirement 5.3):
// - Creation keywords (coding) dominate bug-fix keywords
// - Bug-fix keywords dominate maintenance keywords
// - Non-coding primary action dominates coding context words
func applyDominanceRules(scores map[IntentLabel]*keywordLabelScore) {
	codingScore := scores[LabelCoding]
	bugFixScore := scores[LabelBugFix]
	maintenanceScore := scores[LabelMaintenance]
	nonCodingScore := scores[LabelNonCoding]

	// Creation (coding strong) dominates bug-fix.
	if codingScore != nil && codingScore.strong > 0 && bugFixScore != nil {
		delete(scores, LabelBugFix)
	}

	// Bug-fix dominates maintenance.
	if bugFixScore != nil && bugFixScore.strong > 0 && maintenanceScore != nil {
		delete(scores, LabelMaintenance)
	}

	// Non-coding primary action (strong) dominates coding context words (weak only).
	if nonCodingScore != nil && nonCodingScore.strong > 0 {
		if codingScore != nil && codingScore.strong == 0 && codingScore.weak > 0 {
			delete(scores, LabelCoding)
		}
	}
}

// labelPriority defines the conflict resolution priority order.
// Lower index = higher priority: ssh > browser > coding > non_coding > ambiguous.
var labelPriority = map[IntentLabel]int{
	LabelSSH:              0,
	LabelBrowser:          1,
	LabelCoding:           2,
	LabelNonCoding:        3,
	LabelAmbiguous:        4,
	LabelSearch:           5,
	LabelDocumentDelivery: 6,
	LabelBugFix:           7,
	LabelContinuation:     8,
	LabelMaintenance:      9,
	LabelOffice:           10,
	LabelUnknown:          11,
}

// selectWinner picks the winning label using conflict resolution priority:
// ssh > browser(strong) > coding > non_coding > ambiguous
// For labels not in the priority list, we use strong count then weak count.
func selectWinner(scores map[IntentLabel]*keywordLabelScore) IntentLabel {
	// Find the label with the most strong hits.
	var bestLabel IntentLabel
	bestStrong := -1
	bestPriority := 999

	for label, ls := range scores {
		p := labelPriority[label]
		if ls.strong > bestStrong || (ls.strong == bestStrong && p < bestPriority) {
			bestStrong = ls.strong
			bestPriority = p
			bestLabel = label
		}
	}

	return bestLabel
}

// computeConfidence calculates confidence based on match strength.
func computeConfidence(ls *keywordLabelScore) float64 {
	if ls == nil {
		return 0
	}
	if ls.strong >= 2 {
		return 0.95
	}
	if ls.strong == 1 {
		return 0.92
	}
	// Weak only.
	if ls.weak >= 3 {
		return 0.88
	}
	if ls.weak == 2 {
		return 0.70
	}
	// Single weak keyword → low confidence.
	return 0.50
}

// isContinuationByContext checks if a short message (≤10 runes) with conversation
// history containing coding context should be treated as continuation.
func isContinuationByContext(msg MessageContext) bool {
	runeCount := utf8.RuneCountInString(strings.TrimSpace(msg.Text))
	if runeCount > 10 {
		return false
	}
	if len(msg.RecentHistory) == 0 {
		return false
	}
	// Check if recent history contains coding-related context.
	codingSignals := []string{
		"代码", "开发", "编程", "实现", "功能", "模块",
		"code", "develop", "implement", "function", "module",
		"bug", "修复", "重构", "refactor",
	}
	for _, hist := range msg.RecentHistory {
		lower := strings.ToLower(hist)
		for _, signal := range codingSignals {
			if strings.Contains(lower, signal) {
				return true
			}
		}
	}
	return false
}

// buildResult constructs a ClassificationResult with secondary labels and tool names.
func buildResult(primary IntentLabel, confidence float64, scores map[IntentLabel]*keywordLabelScore, affinity *ToolAffinityRegistry, reason string, creationOriented bool) ClassificationResult {
	var secondary []IntentLabel
	for label := range scores {
		if label != primary {
			secondary = append(secondary, label)
		}
	}

	toolNames := affinity.Resolve(primary, secondary)

	return ClassificationResult{
		Primary:          primary,
		Confidence:       confidence,
		Secondary:        secondary,
		ToolNames:        toolNames,
		Layer:            1,
		Reason:           reason,
		CreationOriented: creationOriented && primary == LabelCoding,
	}
}
