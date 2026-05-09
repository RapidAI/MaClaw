package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// UsagePatternBridge periodically extracts usage patterns from UsageTracker
// and writes them to the Memory Store as project_knowledge entries.
type UsagePatternBridge struct {
	tracker *tool.UsageTracker
	memory  *memory.Store
	stopCh  chan struct{}
	once    sync.Once
}

// NewUsagePatternBridge creates a new bridge between UsageTracker and Memory Store.
func NewUsagePatternBridge(tracker *tool.UsageTracker, mem *memory.Store) *UsagePatternBridge {
	return &UsagePatternBridge{
		tracker: tracker,
		memory:  mem,
		stopCh:  make(chan struct{}),
	}
}

// Start begins the 24-hour periodic extraction loop.
func (b *UsagePatternBridge) Start() {
	go b.loop()
}

// Stop halts the periodic loop.
func (b *UsagePatternBridge) Stop() {
	b.once.Do(func() {
		close(b.stopCh)
	})
}

func (b *UsagePatternBridge) loop() {
	// Run once on startup after a short delay.
	select {
	case <-b.stopCh:
		return
	case <-time.After(30 * time.Second):
	}
	b.RunOnce()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.RunOnce()
		}
	}
}

// RunOnce executes a single extraction + write cycle.
func (b *UsagePatternBridge) RunOnce() {
	if b.tracker == nil || b.memory == nil {
		log.Printf("[usage-to-memory] tracker or memory store not available, skipping")
		return
	}

	patterns := b.tracker.ExtractPatterns(7)
	routingHints := b.tracker.DistillRoutingHints(14, 3)
	skillNudges := b.tracker.DistillSkillNudgeCandidates(30, 3)
	recoveryPatterns := b.tracker.DistillRecoveryPatterns(30, 3)
	if len(patterns) == 0 && len(routingHints) == 0 && len(skillNudges) == 0 && len(recoveryPatterns) == 0 {
		return
	}

	written := 0
	updated := 0
	for _, p := range patterns {
		created, changed := b.upsertUsageMemory(p.Description, []string{"usage_pattern", p.ToolName})
		if created {
			written++
		} else if changed {
			updated++
		}
	}
	for _, hint := range routingHints {
		content := formatToolRoutingHintMemory(hint)
		tags := []string{"usage_routing_hint", hint.ContextKey}
		tags = append(tags, hint.PreferTools...)
		tags = append(tags, hint.AvoidTools...)
		tags = append(tags, hint.RecoveryTools...)
		created, changed := b.upsertUsageMemory(content, tags)
		if created {
			written++
		} else if changed {
			updated++
		}
	}
	for _, nudge := range skillNudges {
		content := formatToolSkillNudgeMemory(nudge)
		tags := []string{"skill_nudge_candidate", nudge.ContextKey}
		if nudge.SuggestedName != "" {
			tags = append(tags, nudge.SuggestedName)
		}
		tags = append(tags, nudge.ToolSequence...)
		tags = append(tags, experienceReviewRequiredTag)
		created, changed := b.upsertUsageMemory(content, tags)
		if created {
			written++
		} else if changed {
			updated++
		}
	}
	for _, pattern := range recoveryPatterns {
		content := formatToolRecoveryPatternMemory(pattern)
		tags := []string{"tool_recovery_pattern", pattern.ContextKey, pattern.FailedTool, pattern.RecoveryTool}
		if pattern.ErrorClass != "" {
			tags = append(tags, pattern.ErrorClass)
		}
		tags = append(tags, pattern.ToolSequence...)
		created, changed := b.upsertUsageMemory(content, tags)
		if created {
			written++
		} else if changed {
			updated++
		}
	}

	if written > 0 || updated > 0 {
		log.Printf("[usage-to-memory] extracted %d patterns, %d routing hints, %d skill nudges, %d recovery patterns: %d new, %d updated",
			len(patterns), len(routingHints), len(skillNudges), len(recoveryPatterns), written, updated)
	}
}

func (b *UsagePatternBridge) upsertUsageMemory(content string, tags []string) (created bool, updated bool) {
	content = strings.TrimSpace(content)
	tags = normalizeUsageMemoryTags(tags)
	if content == "" || len(tags) == 0 || b.memory == nil {
		return false, false
	}

	allEntries := b.memory.List(memory.CategoryProjectKnowledge, "")
	for _, e := range allEntries {
		if !hasAllTags(e.Tags, tags[:usageMemoryIdentityTagCount(tags)]...) {
			continue
		}
		if strings.TrimSpace(e.Content) == content {
			b.memory.TouchAccess([]string{e.ID})
			return false, false
		}
		_ = b.memory.Update(e.ID, content, memory.CategoryProjectKnowledge, resetExperienceReviewTagsForChangedContent(e.Tags, tags))
		return false, true
	}

	entry := memory.Entry{
		Content:    content,
		Category:   memory.CategoryProjectKnowledge,
		Tags:       tags,
		SourceType: "tool_usage",
	}
	if err := b.memory.Save(entry); err != nil {
		return false, false
	}
	return true, false
}

func usageMemoryIdentityTagCount(tags []string) int {
	if len(tags) < 2 {
		return len(tags)
	}
	switch tags[0] {
	case "tool_recovery_pattern":
		if len(tags) < 4 {
			return len(tags)
		}
		return 4
	case "skill_nudge_candidate":
		if len(tags) < 3 {
			return len(tags)
		}
		return 3
	default:
		return 2
	}
}

func formatToolRoutingHintMemory(hint tool.ToolRoutingHint) string {
	parts := []string{fmt.Sprintf("Tool routing hint for %s", firstNonEmptyUsageString(hint.ContextKey, "unknown context"))}
	if len(hint.QueryTokens) > 0 {
		parts = append(parts, "tokens ["+strings.Join(hint.QueryTokens, ", ")+"]")
	}
	if len(hint.PreferTools) > 0 {
		parts = append(parts, "prefer "+strings.Join(hint.PreferTools, ", "))
	}
	if len(hint.AvoidTools) > 0 {
		parts = append(parts, "avoid "+strings.Join(hint.AvoidTools, ", "))
	}
	if len(hint.RecoveryTools) > 0 {
		parts = append(parts, "recovery tools "+strings.Join(hint.RecoveryTools, ", "))
	}
	parts = append(parts, fmt.Sprintf("evidence %d, confidence %.2f", hint.Evidence, hint.Confidence))
	if strings.TrimSpace(hint.Description) != "" {
		parts = append(parts, strings.TrimSpace(hint.Description))
	}
	return strings.Join(parts, "; ")
}

func formatToolSkillNudgeMemory(nudge tool.ToolSkillNudgeCandidate) string {
	name := firstNonEmptyUsageString(nudge.SuggestedName, "unnamed skill candidate")
	parts := []string{fmt.Sprintf("Skill nudge candidate %s", name)}
	if len(nudge.ToolSequence) > 0 {
		parts = append(parts, "sequence "+strings.Join(nudge.ToolSequence, " -> "))
	}
	if len(nudge.QueryTokens) > 0 {
		parts = append(parts, "tokens ["+strings.Join(nudge.QueryTokens, ", ")+"]")
	}
	parts = append(parts, fmt.Sprintf("evidence %d, success %.0f%%, confidence %.2f", nudge.Evidence, nudge.SuccessRate*100, nudge.Confidence))
	if strings.TrimSpace(nudge.Description) != "" {
		parts = append(parts, strings.TrimSpace(nudge.Description))
	}
	return strings.Join(parts, "; ")
}

func formatToolRecoveryPatternMemory(pattern tool.ToolRecoveryPattern) string {
	parts := []string{fmt.Sprintf("Tool recovery pattern for %s", firstNonEmptyUsageString(pattern.ContextKey, "unknown context"))}
	if pattern.FailedTool != "" && pattern.RecoveryTool != "" {
		parts = append(parts, fmt.Sprintf("recover %s with %s", pattern.FailedTool, pattern.RecoveryTool))
	}
	if pattern.ErrorClass != "" {
		parts = append(parts, "error class "+pattern.ErrorClass)
	}
	if len(pattern.ToolSequence) > 0 {
		parts = append(parts, "sequence "+strings.Join(pattern.ToolSequence, " -> "))
	}
	if len(pattern.QueryTokens) > 0 {
		parts = append(parts, "tokens ["+strings.Join(pattern.QueryTokens, ", ")+"]")
	}
	parts = append(parts, fmt.Sprintf("evidence %d, recovered %.0f%%, confidence %.2f", pattern.Evidence, pattern.SuccessRate*100, pattern.Confidence))
	if strings.TrimSpace(pattern.Description) != "" {
		parts = append(parts, strings.TrimSpace(pattern.Description))
	}
	return strings.Join(parts, "; ")
}
func firstNonEmptyUsageString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

func hasAllTags(tags []string, targets ...string) bool {
	for _, target := range targets {
		if !hasTag(tags, target) {
			return false
		}
	}
	return true
}

func normalizeUsageMemoryTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
	}
	return result
}
func mergeTags(existing, additional []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, t := range existing {
		seen[t] = true
	}
	result := make([]string, len(existing))
	copy(result, existing)
	for _, t := range additional {
		if !seen[t] {
			result = append(result, t)
			seen[t] = true
		}
	}
	return result
}
