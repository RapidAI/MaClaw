package scheduler

import (
	"fmt"
	"strings"
	"unicode"
)

// GroupRef is a minimal group record for name→id resolution (channel-agnostic).
type GroupRef struct {
	ID   string
	Name string
}

// GroupMatchStatus classifies a name lookup.
type GroupMatchStatus string

const (
	GroupMatchNone     GroupMatchStatus = "none"
	GroupMatchUnique   GroupMatchStatus = "unique"
	GroupMatchAmbiguous GroupMatchStatus = "ambiguous"
)

// GroupMatchResult is the outcome of matching a query against joined groups.
type GroupMatchResult struct {
	Status  GroupMatchStatus
	Query   string
	Matches []GroupRef
}

// MatchGroupQuery finds groups by id (exact) or name (exact → contains, case-insensitive).
// Prefer unique exact name; then unique substring; otherwise report ambiguity / none.
func MatchGroupQuery(query string, groups []GroupRef) GroupMatchResult {
	q := normalizeGroupQuery(query)
	out := GroupMatchResult{Query: strings.TrimSpace(query), Status: GroupMatchNone}
	if q == "" {
		return out
	}

	// 1) Exact id match wins.
	for _, g := range groups {
		if normalizeGroupQuery(g.ID) == q {
			out.Status = GroupMatchUnique
			out.Matches = []GroupRef{g}
			return out
		}
	}

	var exact, partial []GroupRef
	for _, g := range groups {
		name := normalizeGroupQuery(g.Name)
		if name == "" {
			continue
		}
		if name == q {
			exact = append(exact, g)
			continue
		}
		if strings.Contains(name, q) || strings.Contains(q, name) {
			partial = append(partial, g)
		}
	}

	switch {
	case len(exact) == 1:
		out.Status = GroupMatchUnique
		out.Matches = exact
	case len(exact) > 1:
		out.Status = GroupMatchAmbiguous
		out.Matches = exact
	case len(partial) == 1:
		out.Status = GroupMatchUnique
		out.Matches = partial
	case len(partial) > 1:
		out.Status = GroupMatchAmbiguous
		out.Matches = partial
	default:
		out.Status = GroupMatchNone
	}
	return out
}

// FormatGroupMatchError builds a user/agent-facing message for non-unique matches.
func FormatGroupMatchError(r GroupMatchResult) string {
	q := r.Query
	if q == "" {
		q = "(empty)"
	}
	switch r.Status {
	case GroupMatchNone:
		return fmt.Sprintf("未找到匹配的群 %q。请先 manage_schedule action=list_targets 查看可用目标，或提供准确的 group_id。", q)
	case GroupMatchAmbiguous:
		var b strings.Builder
		b.WriteString(fmt.Sprintf("群名 %q 匹配到多个群，请改用准确名称或 group_id：\n", q))
		limit := len(r.Matches)
		if limit > 12 {
			limit = 12
		}
		for i := 0; i < limit; i++ {
			g := r.Matches[i]
			name := strings.TrimSpace(g.Name)
			if name == "" {
				name = "(无名称)"
			}
			b.WriteString(fmt.Sprintf("  - %s  group_id=%s\n", name, g.ID))
		}
		if len(r.Matches) > limit {
			b.WriteString(fmt.Sprintf("  …共 %d 个\n", len(r.Matches)))
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

// ResolveDeliveryGroupNames fills empty group_id from group_name using the catalog.
// Returns error on none/ambiguous matches. Mutates d in place.
func ResolveDeliveryGroupNames(d *TaskDelivery, groups []GroupRef) error {
	if d == nil || !d.Enabled {
		return nil
	}
	d.Normalize()
	byID := make(map[string]string, len(groups))
	for _, g := range groups {
		id := strings.TrimSpace(g.ID)
		if id == "" {
			continue
		}
		if _, ok := byID[id]; !ok {
			byID[id] = strings.TrimSpace(g.Name)
		}
	}
	for i := range d.Targets {
		t := &d.Targets[i]
		if t.Kind != DeliveryKindGroup {
			continue
		}
		if t.GroupID != "" {
			// Optionally fill display name from catalog.
			if t.GroupName == "" {
				if name := byID[t.GroupID]; name != "" {
					t.GroupName = name
				}
			}
			continue
		}
		query := t.GroupName
		if query == "" {
			return fmt.Errorf("delivery targets[%d]: group 需要 group_id 或 group_name", i)
		}
		m := MatchGroupQuery(query, groups)
		switch m.Status {
		case GroupMatchUnique:
			t.GroupID = strings.TrimSpace(m.Matches[0].ID)
			if name := strings.TrimSpace(m.Matches[0].Name); name != "" {
				t.GroupName = name
			}
		default:
			return fmt.Errorf("%s", FormatGroupMatchError(m))
		}
	}
	return nil
}

func normalizeGroupQuery(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Collapse whitespace; lower-case for Latin; keep CJK as-is (strings.EqualFold is enough for mixed).
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
