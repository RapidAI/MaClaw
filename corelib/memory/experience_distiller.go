package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ExperienceSource identifies where an entry originally came from. The values
// intentionally align with Entry.SourceType, while accepting legacy empty source
// values as conversation-derived memories.
type ExperienceSource string

const (
	ExperienceSourceConversation ExperienceSource = "conversation"
	ExperienceSourceWorkflow     ExperienceSource = "workflow"
	ExperienceSourceSwarm        ExperienceSource = "swarm"
	ExperienceSourceA2A          ExperienceSource = "a2a_discussion"
	ExperienceSourceToolUsage    ExperienceSource = "tool_usage"
	ExperienceSourceManual       ExperienceSource = "manual"
	ExperienceSourceUnknown      ExperienceSource = "unknown"
)

// ExperienceDistillResult is a non-mutating summary emitted by the maintenance
// pipeline before compression/promotion/reflection. Phase 1 uses it as an
// observability and gating surface; later phases can feed protected candidates
// into LLM-backed distillation prompts.
type ExperienceDistillResult struct {
	ScannedEntries        int                            `json:"scanned_entries"`
	ActiveEntries         int                            `json:"active_entries"`
	SourceCounts          map[string]int                 `json:"source_counts,omitempty"`
	ProtectedCandidates   int                            `json:"protected_candidates"`
	ProtectedReasonCounts map[string]int                 `json:"protected_reason_counts,omitempty"`
	ProtectedSourceCounts map[string]int                 `json:"protected_source_counts,omitempty"`
	ProtectedSamples      []ProtectedExperienceCandidate `json:"protected_samples,omitempty"`
	LayeredRecommended    bool                           `json:"layered_recommended"`
	Reason                string                         `json:"reason,omitempty"`
}

// ProtectedExperienceCandidate is a small, prompt-safe pointer to a memory that
// should retain concrete details during future distillation or compression.
type ProtectedExperienceCandidate struct {
	ID        string   `json:"id"`
	Title     string   `json:"title,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Category  string   `json:"category,omitempty"`
	Source    string   `json:"source,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Strength  float64  `json:"strength,omitempty"`
	Pinned    bool     `json:"pinned,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// ExperienceDistiller classifies memory entries before heavy maintenance work.
// It deliberately avoids mutating entries so it can run safely inside the normal
// background pipeline.
type ExperienceDistiller struct {
	LayeredThreshold int
}

// NewExperienceDistiller returns a distiller with conservative defaults.
func NewExperienceDistiller() *ExperienceDistiller {
	return &ExperienceDistiller{LayeredThreshold: 40}
}

// Analyze summarizes the active memory mix and flags whether a Combee-style
// layered pass would be appropriate for a future LLM-backed distillation step.
func (d *ExperienceDistiller) Analyze(entries []Entry) ExperienceDistillResult {
	return d.AnalyzeWithSampleLimit(entries, 12)
}

// AnalyzeWithSampleLimit is Analyze with an explicit protected sample cap.
// It is useful for read-only inspection tools that need more than the default
// compact observability sample while still avoiding unbounded payloads.
func (d *ExperienceDistiller) AnalyzeWithSampleLimit(entries []Entry, sampleLimit int) ExperienceDistillResult {
	threshold := d.LayeredThreshold
	if threshold <= 0 {
		threshold = 40
	}
	result := ExperienceDistillResult{SourceCounts: map[string]int{}, ProtectedReasonCounts: map[string]int{}, ProtectedSourceCounts: map[string]int{}}
	protectedSamples := []ProtectedExperienceCandidate{}
	for _, entry := range entries {
		result.ScannedEntries++
		if !entry.IsActive() {
			continue
		}
		result.ActiveEntries++
		source := ClassifyExperienceSource(entry)
		result.SourceCounts[string(source)]++
		if reason := experienceProtectionReason(entry, source); reason != "" {
			result.ProtectedCandidates++
			result.ProtectedReasonCounts[reason]++
			result.ProtectedSourceCounts[string(source)]++
			if candidate, ok := ProtectedExperienceCandidateForEntry(entry); ok {
				protectedSamples = append(protectedSamples, candidate)
			}
		}
	}
	sort.SliceStable(protectedSamples, func(i, j int) bool {
		return protectedExperienceCandidateLess(protectedSamples[i], protectedSamples[j])
	})
	if sampleLimit < 0 {
		sampleLimit = 0
	}
	if sampleLimit > 0 && len(protectedSamples) > sampleLimit {
		protectedSamples = protectedSamples[:sampleLimit]
	}
	if sampleLimit != 0 {
		result.ProtectedSamples = protectedSamples
	}
	if result.ActiveEntries >= threshold {
		result.LayeredRecommended = true
		result.Reason = "active memory volume exceeds layered distillation threshold"
	} else if result.ProtectedCandidates >= 8 && result.ActiveEntries >= 16 {
		result.LayeredRecommended = true
		result.Reason = "many protected experience candidates should be preserved during distillation"
	}
	if len(result.SourceCounts) == 0 {
		result.SourceCounts = nil
	}
	if len(result.ProtectedReasonCounts) == 0 {
		result.ProtectedReasonCounts = nil
	}
	if len(result.ProtectedSourceCounts) == 0 {
		result.ProtectedSourceCounts = nil
	}
	return result
}

// ProtectedExperienceCandidateForEntry returns a bounded, inspectable pointer
// for an entry that should retain concrete details during maintenance.
func ProtectedExperienceCandidateForEntry(entry Entry) (ProtectedExperienceCandidate, bool) {
	source := ClassifyExperienceSource(entry)
	reason := experienceProtectionReason(entry, source)
	if reason == "" {
		return ProtectedExperienceCandidate{}, false
	}
	candidate := ProtectedExperienceCandidate{
		ID:       strings.TrimSpace(entry.ID),
		Title:    truncateExperienceCandidateText(firstExperienceCandidateString(entry.Title, entry.Content), 120),
		Summary:  truncateExperienceCandidateText(entry.Content, 220),
		Category: string(entry.Category),
		Source:   string(source),
		Reason:   reason,
		Tags:     normalizeExperienceCandidateTags(entry.Tags, 12),
		Strength: entry.Strength,
		Pinned:   entry.Pinned,
	}
	if !entry.UpdatedAt.IsZero() {
		candidate.UpdatedAt = entry.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return candidate, true
}

func protectedExperienceCandidateLess(a, b ProtectedExperienceCandidate) bool {
	if a.Pinned != b.Pinned {
		return a.Pinned
	}
	if ar, br := protectedExperienceReasonRank(a.Reason), protectedExperienceReasonRank(b.Reason); ar != br {
		return ar > br
	}
	if a.Strength != b.Strength {
		return a.Strength > b.Strength
	}
	if a.UpdatedAt != b.UpdatedAt {
		return a.UpdatedAt > b.UpdatedAt
	}
	return a.ID < b.ID
}

func protectedExperienceReasonRank(reason string) int {
	switch strings.TrimSpace(reason) {
	case "pinned":
		return 80
	case "instruction":
		return 70
	case "self_identity":
		return 65
	case "high_strength":
		return 60
	case "a2a_discussion":
		return 50
	case "tool_usage":
		return 40
	case "swarm_trace":
		return 30
	default:
		return 0
	}
}

func normalizeExperienceCandidateTags(tags []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, truncateExperienceCandidateText(tag, 80))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func truncateExperienceCandidateText(value string, maxLen int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || maxLen <= 0 {
		return value
	}
	return truncStr(value, maxLen)
}

func firstExperienceCandidateString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ClassifyExperienceSource normalizes Entry.SourceType into the small set used
// by the experience learning layer.
func ClassifyExperienceSource(entry Entry) ExperienceSource {
	source := strings.ToLower(strings.TrimSpace(entry.SourceType))
	switch source {
	case "", "conversation", "chat", "session", "im":
		return ExperienceSourceConversation
	case "workflow", "task_artifact", "artifact", "document":
		return ExperienceSourceWorkflow
	case "swarm", "subagent", "agent":
		return ExperienceSourceSwarm
	case "a2a", "a2a_discussion", "group_discussion", "discussion":
		return ExperienceSourceA2A
	case "tool", "tool_usage", "usage":
		return ExperienceSourceToolUsage
	case "manual", "user", "import":
		return ExperienceSourceManual
	default:
		return ExperienceSourceUnknown
	}
}

func shouldProtectExperienceEntry(entry Entry, source ExperienceSource) bool {
	return experienceProtectionReason(entry, source) != ""
}

func experienceProtectionReason(entry Entry, source ExperienceSource) string {
	if entry.Pinned {
		return "pinned"
	}
	if entry.Strength >= 3.0 {
		return "high_strength"
	}
	switch MapToCanonical(entry.Category) {
	case CategoryInstruction:
		return "instruction"
	case CategorySelfIdentity:
		return "self_identity"
	}
	switch source {
	case ExperienceSourceA2A:
		return "a2a_discussion"
	case ExperienceSourceToolUsage:
		return "tool_usage"
	case ExperienceSourceSwarm:
		return "swarm_trace"
	default:
		return ""
	}
}

// ExperienceProtectionHint returns source-specific guidance for LLM-backed
// memory maintenance prompts. Empty means the entry can use the normal prompt.
func ExperienceProtectionHint(entry Entry) string {
	source := ClassifyExperienceSource(entry)
	reason := experienceProtectionReason(entry, source)
	if reason == "" {
		return ""
	}
	base := "Preserve this protected memory's concrete details: names, numbers, paths, commands, errors, user corrections, decisions, and rollback constraints."
	switch source {
	case ExperienceSourceA2A:
		return base + " This came from A2A/group discussion; keep objections, evidence, risks, minority views, and final recommendation distinct."
	case ExperienceSourceToolUsage:
		return base + " This came from tool usage; keep tool names, sequence, failure class, retry/recovery path, and final outcome."
	case ExperienceSourceSwarm:
		return base + " This came from Swarm/SubAgent work; keep task ownership, changed areas, test results, merge risks, and verifier findings."
	default:
		return base
	}
}

// CompressionProtectionHint returns guidance for LLM-backed memory
// compression. It currently shares the same protection policy as broader
// experience maintenance so compression, promotion, and reflection preserve the
// same concrete details.
func CompressionProtectionHint(entry Entry) string {
	return ExperienceProtectionHint(entry)
}

func FormatExperienceProtectionPrompt(samples []ProtectedExperienceCandidate) string {
	if len(samples) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Protected experience candidates to preserve during memory maintenance:\n")
	limit := len(samples)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		sample := samples[i]
		fmt.Fprintf(&b, "- id=%s reason=%s source=%s category=%s", sample.ID, sample.Reason, sample.Source, sample.Category)
		if sample.Title != "" {
			fmt.Fprintf(&b, " title=%q", sample.Title)
		}
		if len(sample.Tags) > 0 {
			fmt.Fprintf(&b, " tags=%s", strings.Join(sample.Tags, ","))
		}
		if sample.Summary != "" {
			fmt.Fprintf(&b, " summary=%q", sample.Summary)
		}
		b.WriteByte('\n')
	}
	b.WriteString("Use these as retention anchors. Do not merge away, over-promote, or flatten conflicting concrete evidence from pinned, instruction, A2A, tool, or swarm-derived memories.")
	return b.String()
}

func formatExperiencePromptEntry(index int, entry Entry, contentLimit int) string {
	if contentLimit <= 0 {
		contentLimit = 200
	}
	hint := ExperienceProtectionHint(entry)
	if hint != "" {
		contentLimit *= 2
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] %s\n", index, truncStr(entry.Content, contentLimit))
	if hint != "" {
		fmt.Fprintf(&b, "    experience_protection: %s\n", hint)
	}
	return b.String()
}
