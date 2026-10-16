package agentruntime

import (
	"regexp"
	"strings"
)

// Shared experience-learning tag and trace vocabulary. The values are frozen
// here so every host (GUI, srv, TUI) composes failure/review memories with
// identical tags; hosts keep only alias constants for compatibility.

// ExperienceTraceSourceToolUsage is the experience trace source value for
// memories derived from tool usage.
const ExperienceTraceSourceToolUsage = "tool_usage"

const (
	// ExperienceReviewRequiredTag marks a memory entry that awaits review.
	ExperienceReviewRequiredTag = "review_required"
	// ExperienceReviewResolvedTag marks a memory entry whose review finished.
	ExperienceReviewResolvedTag = "review_resolved"
	// ExperienceReviewStatusTagPrefix prefixes the recorded review outcome.
	ExperienceReviewStatusTagPrefix = "review_status:"
	// ExperienceReviewedAtTagPrefix prefixes the review date stamp.
	ExperienceReviewedAtTagPrefix = "reviewed_at:"
)

// ExperienceReviewLifecycleTagKind classifies review-lifecycle state tags.
type ExperienceReviewLifecycleTagKind string

const (
	ExperienceReviewLifecycleTagUnknown              ExperienceReviewLifecycleTagKind = ""
	ExperienceReviewLifecycleTagDeferred             ExperienceReviewLifecycleTagKind = "review_deferred"
	ExperienceReviewLifecycleTagRollbackReviewed     ExperienceReviewLifecycleTagKind = "rollback_reviewed"
	ExperienceReviewLifecycleTagRollbackRejected     ExperienceReviewLifecycleTagKind = "rollback_rejected"
	ExperienceReviewLifecycleTagConflictReviewed     ExperienceReviewLifecycleTagKind = "conflict_reviewed"
	ExperienceReviewLifecycleTagConflictRejected     ExperienceReviewLifecycleTagKind = "conflict_rejected"
	ExperienceReviewLifecycleTagSkillNudgeReviewed   ExperienceReviewLifecycleTagKind = "skill_nudge_reviewed"
	ExperienceReviewLifecycleTagSkillNudgeRejected   ExperienceReviewLifecycleTagKind = "skill_nudge_rejected"
	ExperienceReviewLifecycleTagToolRecoveryReviewed ExperienceReviewLifecycleTagKind = "tool_recovery_reviewed"
	ExperienceReviewLifecycleTagToolRecoveryRejected ExperienceReviewLifecycleTagKind = "tool_recovery_rejected"
)

// NormalizeExperienceReviewLifecycleTagKind maps a tag onto the known
// review-lifecycle vocabulary; anything else is Unknown.
func NormalizeExperienceReviewLifecycleTagKind(tag string) ExperienceReviewLifecycleTagKind {
	switch ExperienceReviewLifecycleTagKind(strings.TrimSpace(tag)) {
	case ExperienceReviewLifecycleTagDeferred:
		return ExperienceReviewLifecycleTagDeferred
	case ExperienceReviewLifecycleTagRollbackReviewed:
		return ExperienceReviewLifecycleTagRollbackReviewed
	case ExperienceReviewLifecycleTagRollbackRejected:
		return ExperienceReviewLifecycleTagRollbackRejected
	case ExperienceReviewLifecycleTagConflictReviewed:
		return ExperienceReviewLifecycleTagConflictReviewed
	case ExperienceReviewLifecycleTagConflictRejected:
		return ExperienceReviewLifecycleTagConflictRejected
	case ExperienceReviewLifecycleTagSkillNudgeReviewed:
		return ExperienceReviewLifecycleTagSkillNudgeReviewed
	case ExperienceReviewLifecycleTagSkillNudgeRejected:
		return ExperienceReviewLifecycleTagSkillNudgeRejected
	case ExperienceReviewLifecycleTagToolRecoveryReviewed:
		return ExperienceReviewLifecycleTagToolRecoveryReviewed
	case ExperienceReviewLifecycleTagToolRecoveryRejected:
		return ExperienceReviewLifecycleTagToolRecoveryRejected
	default:
		return ExperienceReviewLifecycleTagUnknown
	}
}

func (kind ExperienceReviewLifecycleTagKind) String() string {
	return string(kind)
}

// IsStateTag reports whether the kind is a known lifecycle state tag.
func (kind ExperienceReviewLifecycleTagKind) IsStateTag() bool {
	return kind != ExperienceReviewLifecycleTagUnknown
}

// NormalizeExperienceBrowserToolName folds every browser_* spelling into the
// canonical "browser" experience bucket; other names pass through lowercased.
func NormalizeExperienceBrowserToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	if name == "browser" || strings.HasPrefix(name, "browser_") {
		return "browser"
	}
	return name
}

// NormalizeUsageMemoryTags canonicalizes and de-duplicates memory tags while
// preserving order; empty tags are dropped.
func NormalizeUsageMemoryTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = NormalizeUsageMemoryBrowserTag(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
	}
	return result
}

// NormalizeUsageMemoryBrowserTag folds browser_* tag spellings into the
// canonical "browser" tag; other tags pass through trimmed.
func NormalizeUsageMemoryBrowserTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	lower := strings.ToLower(tag)
	if lower == "browser" || strings.HasPrefix(lower, "browser_") {
		return "browser"
	}
	return tag
}

// ExperienceTraceKindToolRecoveryPattern is the experience trace kind for
// tool-recovery patterns learned from adaptive retry outcomes.
const ExperienceTraceKindToolRecoveryPattern = "tool_recovery_pattern"

// Shared review outcome vocabulary (frozen; hosts keep aliases).
const (
	ExperienceReviewOutcomeApproved = "approved"
	ExperienceReviewOutcomeRejected = "rejected"
	ExperienceReviewOutcomeDeferred = "deferred"
)

// ExperienceReviewOutcomeKind is the normalized review decision kind.
type ExperienceReviewOutcomeKind string

// ExperienceReviewOutcomeUnknown marks an unrecognized review decision.
const ExperienceReviewOutcomeUnknown ExperienceReviewOutcomeKind = ""

// NormalizeExperienceReviewOutcomeKind maps free-form review text onto the
// shared outcome vocabulary.
func NormalizeExperienceReviewOutcomeKind(value string) ExperienceReviewOutcomeKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approve", "approved", "accept", "accepted", "ok":
		return ExperienceReviewOutcomeKind(ExperienceReviewOutcomeApproved)
	case "reject", "rejected", "deny", "denied":
		return ExperienceReviewOutcomeKind(ExperienceReviewOutcomeRejected)
	case "defer", "deferred", "later", "pending":
		return ExperienceReviewOutcomeKind(ExperienceReviewOutcomeDeferred)
	default:
		return ExperienceReviewOutcomeUnknown
	}
}

func (kind ExperienceReviewOutcomeKind) String() string {
	return string(kind)
}

// IsKnown reports whether the kind belongs to the shared outcome vocabulary.
func (kind ExperienceReviewOutcomeKind) IsKnown() bool {
	return kind != ExperienceReviewOutcomeUnknown
}

// ExperienceReviewStatus is the review lifecycle status recorded on tags.
type ExperienceReviewStatus string

const (
	ExperienceReviewStatusUnknown  ExperienceReviewStatus = ""
	ExperienceReviewStatusRequired ExperienceReviewStatus = "required"
	ExperienceReviewStatusApproved ExperienceReviewStatus = ExperienceReviewOutcomeApproved
	ExperienceReviewStatusRejected ExperienceReviewStatus = ExperienceReviewOutcomeRejected
	ExperienceReviewStatusDeferred ExperienceReviewStatus = ExperienceReviewOutcomeDeferred
)

// NormalizeExperienceReviewStatus maps a tag status value onto the shared
// review status vocabulary.
func NormalizeExperienceReviewStatus(status string) ExperienceReviewStatus {
	switch ExperienceReviewStatus(strings.ToLower(strings.TrimSpace(status))) {
	case ExperienceReviewStatusRequired:
		return ExperienceReviewStatusRequired
	case ExperienceReviewStatusApproved:
		return ExperienceReviewStatusApproved
	case ExperienceReviewStatusRejected:
		return ExperienceReviewStatusRejected
	case ExperienceReviewStatusDeferred:
		return ExperienceReviewStatusDeferred
	default:
		return ExperienceReviewStatusUnknown
	}
}

// IsResolved reports whether the status is a final approved/rejected outcome.
func (s ExperienceReviewStatus) IsResolved() bool {
	switch s {
	case ExperienceReviewStatusApproved, ExperienceReviewStatusRejected:
		return true
	default:
		return false
	}
}

// IsRecordedReviewOutcome reports whether the status is a recorded decision.
func (s ExperienceReviewStatus) IsRecordedReviewOutcome() bool {
	switch s {
	case ExperienceReviewStatusApproved, ExperienceReviewStatusRejected, ExperienceReviewStatusDeferred:
		return true
	default:
		return false
	}
}

func (s ExperienceReviewStatus) String() string {
	return string(s)
}

// HasTag reports whether target appears verbatim in tags.
func HasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

// ExperienceTraceReviewResolved reports whether the tag set records a
// resolved review (explicit resolved tag or a resolved status value).
func ExperienceTraceReviewResolved(tags []string) bool {
	if HasTag(tags, ExperienceReviewResolvedTag) {
		return true
	}
	for _, tag := range tags {
		if !strings.HasPrefix(tag, ExperienceReviewStatusTagPrefix) {
			continue
		}
		status := NormalizeExperienceReviewStatus(strings.TrimPrefix(tag, ExperienceReviewStatusTagPrefix))
		if status.IsResolved() {
			return true
		}
	}
	return false
}

// SafeFilenameRe matches characters that must be folded to '-' in safe
// filename/tag identifiers.
var SafeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)
