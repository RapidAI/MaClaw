package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// FormatMemorySummary generates a structured overview of what the memory system
// knows about the user. Groups entries by semantic category, shows top entries
// per group by strength, and provides health statistics.
//
// This implements the Dreaming V3 "Memory Summary" concept: a high-level view
// that lets users (and the LLM) quickly understand the memory state.
//
// ownerID filters entries in multi-tenant mode; empty means show all.
func (s *Store) FormatMemorySummary(ownerID string) string {
	return s.FormatMemorySummaryForOwner(ownerID, false)
}

// FormatMemorySummaryForOwner optionally applies an exact owner boundary.
func (s *Store) FormatMemorySummaryForOwner(ownerID string, strictOwner bool) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()

	groups := map[string][]Entry{
		"user_info":   {},
		"projects":    {},
		"preferences": {},
		"knowledge":   {},
		"other":       {},
	}

	var totalActive, staleCount int

	for i := range s.entries {
		e := &s.entries[i]
		if !e.IsActive() {
			continue
		}
		if strictOwner && (e.OwnerID != ownerID || (e.Boundary != nil && e.Boundary.OwnerID != "" && e.Boundary.OwnerID != ownerID)) {
			continue
		}
		if !strictOwner && ownerID != "" && e.OwnerID != "" && e.OwnerID != ownerID {
			continue
		}
		totalActive++
		if e.Stale {
			staleCount++
		}

		canonical := MapToCanonical(e.Category)
		var key string
		switch {
		case e.Category == CategoryUserFact || canonical == CategoryUserFact ||
			e.Category == CategorySelfIdentity || e.Category == "user":
			key = "user_info"
		case e.Category == CategoryProjectKnowledge || e.Category == "project" ||
			e.Category == CategoryTaskArtifact:
			key = "projects"
		case e.Category == "preference" || e.Category == "instruction" ||
			e.Category == "feedback":
			key = "preferences"
		case e.Category == "profile" || e.Category == "reference":
			key = "knowledge"
		default:
			key = "other"
		}
		groups[key] = append(groups[key], *e)
	}

	// Sort each group by strength descending, keep top 5.
	const maxPerGroup = 5
	groupTotals := make(map[string]int, len(groups))
	for k, g := range groups {
		groupTotals[k] = len(g)
		sort.Slice(g, func(i, j int) bool {
			return g[i].Strength > g[j].Strength
		})
		if len(g) > maxPerGroup {
			g = g[:maxPerGroup]
		}
		groups[k] = g
	}

	// Build output.
	var b strings.Builder
	fmt.Fprintf(&b, "记忆概览（共 %d 条活跃记忆", totalActive)
	if staleCount > 0 {
		fmt.Fprintf(&b, "，%d 条可能过时", staleCount)
	}
	b.WriteString("）\n\n")

	writeGroup := func(prefix, title, key string) {
		entries := groups[key]
		if len(entries) == 0 {
			return
		}
		if prefix != "" {
			fmt.Fprintf(&b, "%s %s（%d 条）：\n", prefix, title, groupTotals[key])
		} else {
			fmt.Fprintf(&b, "%s（%d 条）：\n", title, groupTotals[key])
		}
		for _, e := range entries {
			content := e.CompactForm
			if content == "" {
				content = e.Content
			}
			runes := []rune(content)
			if len(runes) > 80 {
				content = string(runes[:77]) + "..."
			}
			content = strings.ReplaceAll(content, "\n", " ")

			var marks string
			if e.Stale {
				marks += " 过时"
			}
			if e.InvalidAt != nil && e.InvalidAt.Before(now) {
				marks += " 已过期"
			}
			fmt.Fprintf(&b, "  • %s%s [%s]\n", content, marks, truncateID(e.ID, 8))
		}
		b.WriteByte('\n')
	}

	writeGroup("", "关于你", "user_info")
	writeGroup("", "项目知识", "projects")
	writeGroup("", "偏好与指令", "preferences")
	writeGroup("", "通用知识", "knowledge")
	writeGroup("", "其他", "other")

	b.WriteString("使用 memory(action=recall, query=\"...\") 查询具体记忆\n")
	b.WriteString("使用 memory(action=delete, id=\"...\") 删除错误记忆\n")

	return b.String()
}

// truncateID returns at most maxLen characters of an ID for display.
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}
