package main

import (
	"sort"
	"strings"
)

const promptSkillIndexLimit = 40

func promptSkillIndexEntries(skills []NLSkillDefinition, limit int) []NLSkillDefinition {
	if limit <= 0 || len(skills) == 0 {
		return nil
	}

	active := make([]NLSkillDefinition, 0, len(skills))
	for _, s := range skills {
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
			continue
		}
		if strings.TrimSpace(s.Name) == "" {
			continue
		}
		if isShellBrowserAutomationSkill(s) {
			continue
		}
		active = append(active, s)
	}

	sort.SliceStable(active, func(i, j int) bool {
		left := active[i]
		right := active[j]
		if left.UsageCount != right.UsageCount {
			return left.UsageCount > right.UsageCount
		}
		if left.SuccessRate != right.SuccessRate {
			return left.SuccessRate > right.SuccessRate
		}
		if left.LastUsedAt != nil && right.LastUsedAt != nil && !left.LastUsedAt.Equal(*right.LastUsedAt) {
			return left.LastUsedAt.After(*right.LastUsedAt)
		}
		if left.LastUsedAt != nil && right.LastUsedAt == nil {
			return true
		}
		if left.LastUsedAt == nil && right.LastUsedAt != nil {
			return false
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})

	if len(active) > limit {
		active = active[:limit]
	}
	return active
}
