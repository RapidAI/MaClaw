package memory

// StabilityDetector tracks knowledge stability — how confident we are that
// a piece of knowledge is correct and stable over time.
// Inspired by OpenHuman's learning/stability_detector.rs.
//
// Stability levels:
// - Unverified: newly written, never confirmed or contradicted
// - Stable: confirmed ≥3 times with 0 contradictions
// - Volatile: contradicted at least once
//
// Stable knowledge gets boosted in recall scoring (+2.0).
// Volatile knowledge gets penalized (-1.0) and annotated with a warning.

import (
	"strings"
	"time"
)

// StabilityLevel represents how stable/reliable a piece of knowledge is.
type StabilityLevel string

const (
	StabilityUnverified StabilityLevel = ""         // default for new entries
	StabilityStable     StabilityLevel = "stable"   // confirmed ≥3 times, 0 contradictions
	StabilityVolatile   StabilityLevel = "volatile" // contradicted at least once
)

// StabilityMeta holds stability tracking fields for a memory entry.
type StabilityMeta struct {
	Level          StabilityLevel `json:"stability,omitempty"`
	ConfirmCount   int            `json:"confirm_count,omitempty"`
	ContradictCount int           `json:"contradict_count,omitempty"`
	LastVerifiedAt time.Time      `json:"last_verified_at,omitempty"`
}

// StabilityBoost returns the recall score adjustment for this stability level.
// Stable: +2.0, Volatile: -1.0, Unverified/nil: 0.
func (m *StabilityMeta) StabilityBoost() float64 {
	if m == nil {
		return 0.0
	}
	switch m.Level {
	case StabilityStable:
		return 2.0
	case StabilityVolatile:
		return -1.0
	default:
		return 0.0
	}
}

// RecordConfirmation marks a knowledge entry as confirmed (consistent with new evidence).
func (m *StabilityMeta) RecordConfirmation() {
	if m == nil {
		return
	}
	m.ConfirmCount++
	m.LastVerifiedAt = time.Now()
	m.recomputeLevel()
}

// RecordContradiction marks a knowledge entry as contradicted by new evidence.
func (m *StabilityMeta) RecordContradiction() {
	if m == nil {
		return
	}
	m.ContradictCount++
	m.LastVerifiedAt = time.Now()
	m.recomputeLevel()
}

// Reset clears stability tracking (e.g. when content is updated).
func (m *StabilityMeta) Reset() {
	if m == nil {
		return
	}
	m.Level = StabilityUnverified
	m.ConfirmCount = 0
	m.ContradictCount = 0
	m.LastVerifiedAt = time.Time{}
}

func (m *StabilityMeta) recomputeLevel() {
	if m.ContradictCount > 0 {
		m.Level = StabilityVolatile
		return
	}
	if m.ConfirmCount >= 3 {
		m.Level = StabilityStable
		return
	}
	m.Level = StabilityUnverified
}

// DetectContradiction checks if newContent contradicts existingContent.
// Uses simple heuristic: if both are about the same entity but have
// conflicting values (negation, different numbers, opposite adjectives).
// Returns true if contradiction is detected.
func DetectContradiction(existingContent, newContent string) bool {
	existing := strings.ToLower(existingContent)
	new_ := strings.ToLower(newContent)

	// Same topic detection: at least 30% word overlap
	existingWords := uniqueWords(existing)
	newWords := uniqueWords(new_)
	overlap := wordOverlap(existingWords, newWords)
	minLen := min(len(existingWords), len(newWords))
	if minLen == 0 || float64(overlap)/float64(minLen) < 0.3 {
		return false // different topics, not a contradiction
	}

	// Negation patterns
	negationPairs := [][2]string{
		{"不是", "是"},
		{"没有", "有"},
		{"不能", "能"},
		{"不支持", "支持"},
		{"禁止", "允许"},
		{"false", "true"},
		{"disabled", "enabled"},
		{"不需要", "需要"},
	}
	for _, pair := range negationPairs {
		if (strings.Contains(existing, pair[0]) && strings.Contains(new_, pair[1]) && !strings.Contains(new_, pair[0])) ||
			(strings.Contains(existing, pair[1]) && strings.Contains(new_, pair[0]) && !strings.Contains(existing, pair[0])) {
			return true
		}
	}

	// Explicit correction signals in new content
	correctionSignals := []string{
		"不对", "错了", "应该是", "实际上是", "纠正", "更正",
		"actually", "correction", "wrong", "incorrect",
	}
	for _, signal := range correctionSignals {
		if strings.Contains(new_, signal) {
			return true
		}
	}

	return false
}

func uniqueWords(s string) map[string]bool {
	words := make(map[string]bool)
	// For space-separated languages (English), split on whitespace
	for _, w := range strings.Fields(s) {
		if len(w) >= 2 {
			words[w] = true
		}
	}
	// For Chinese: extract 2-gram character pairs as "words"
	runes := []rune(s)
	for i := 0; i+1 < len(runes); i++ {
		r1, r2 := runes[i], runes[i+1]
		if isCJK(r1) && isCJK(r2) {
			words[string(runes[i:i+2])] = true
		}
	}
	return words
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0xF900 && r <= 0xFAFF) // CJK Compatibility Ideographs
}

func wordOverlap(a, b map[string]bool) int {
	count := 0
	for w := range a {
		if b[w] {
			count++
		}
	}
	return count
}
