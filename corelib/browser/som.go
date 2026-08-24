package browser

import (
	"sort"
	"strings"
)

const compactRefLimit = 80
const visionEmptyRefThreshold = 3

func compactElementRef(ref BrowserElementRef) CompactElementRef {
	name := strings.TrimSpace(firstNonEmpty(ref.Name, ref.Text))
	role := strings.ToLower(strings.TrimSpace(ref.Role))
	tag := strings.ToLower(strings.TrimSpace(ref.Tag))
	if ref.Value != "" && (tag == "select" || role == "combobox") {
		if name == "" {
			name = ref.Value
		} else if !strings.Contains(strings.ToLower(name), strings.ToLower(ref.Value)) {
			name = name + " = " + ref.Value
		}
	}
	return CompactElementRef{
		Ref:     ref.Ref,
		Role:    ref.Role,
		Name:    name,
		Tag:     ref.Tag,
		Enabled: !ref.Disabled,
		Checked: compactChecked(role, tag, ref.InputType, ref.Checked),
	}
}

func compactChecked(role, tag, inputType string, checked bool) *bool {
	switch role {
	case "checkbox", "radio", "switch":
		v := checked
		return &v
	}
	if tag == "input" {
		switch strings.ToLower(strings.TrimSpace(inputType)) {
		case "checkbox", "radio":
			v := checked
			return &v
		}
	}
	return nil
}

func compactElementRefs(refs []BrowserElementRef) []CompactElementRef {
	out := make([]CompactElementRef, 0, len(refs))
	for _, ref := range refs {
		item := compactElementRef(ref)
		if strings.TrimSpace(item.Ref) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func rankRefsForCompact(refs []BrowserElementRef) []BrowserElementRef {
	ranked := append([]BrowserElementRef(nil), refs...)
	sort.SliceStable(ranked, func(i, j int) bool {
		si, sj := refCompactScore(ranked[i]), refCompactScore(ranked[j])
		if si != sj {
			return si > sj
		}
		return ranked[i].Ref < ranked[j].Ref
	})
	return ranked
}

func refCompactScore(ref BrowserElementRef) int {
	score := 0
	if ref.Visible {
		score += 8
	}
	if ref.InViewport {
		score += 8
	}
	if !ref.Disabled {
		score += 4
	}
	if strings.TrimSpace(firstNonEmpty(ref.Name, ref.Text)) != "" {
		score += 4
	}
	if ref.BackendNodeID != 0 {
		score += 1
	}
	return score
}

func truncateRefs(refs []BrowserElementRef, limit int) ([]BrowserElementRef, bool) {
	if limit <= 0 || len(refs) <= limit {
		return refs, false
	}
	ranked := rankRefsForCompact(refs)
	return ranked[:limit], true
}

func observeDataFromSnapshot(snapshot BrowserSnapshot) map[string]interface{} {
	compact, truncated := truncateRefs(snapshot.Refs, compactRefLimit)
	truncated = truncated || snapshot.RefsTruncated
	data := map[string]interface{}{
		"snapshot_id":        snapshot.SnapshotID,
		"url":                snapshot.URL,
		"title":              snapshot.Title,
		"refs":               compactElementRefs(compact),
		"refs_truncated":     truncated || snapshot.RefsTruncated,
		"console_summary":    snapshot.ConsoleSummary,
		"network_summary":    snapshot.NetworkSummary,
		"page_state":         map[string]interface{}{"ready_state": snapshot.ReadyState, "url": snapshot.URL, "title": snapshot.Title},
		"page_text_excerpt":  snapshot.PageTextExcerpt,
		"page_text_total":    snapshot.PageTextTotal,
		"page_text_offset":   snapshot.PageTextOffset,
		"page_text_has_more": snapshot.PageTextHasMore,
		"page_flags":         snapshot.PageFlags,
	}
	if snapshot.VisionExcerpt != "" {
		data["vision_excerpt"] = snapshot.VisionExcerpt
	}
	return data
}

func compactActionData(obs *BrowserObservation, extra map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{}
	if obs != nil {
		for k, v := range observeDataFromSnapshot(obs.Snapshot) {
			data[k] = v
		}
		if obs.Snapshot.SnapshotID != "" {
			data["snapshot_id"] = obs.Snapshot.SnapshotID
		}
	}
	for k, v := range extra {
		if llmHiddenExtraKey(k) {
			continue
		}
		data[k] = v
	}
	return data
}

func compactFailureData(obs *BrowserObservation, extra map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{}
	if obs != nil {
		data["snapshot_id"] = obs.Snapshot.SnapshotID
		data["url"] = obs.Snapshot.URL
		data["title"] = obs.Snapshot.Title
		data["page_state"] = map[string]interface{}{"ready_state": obs.Snapshot.ReadyState, "url": obs.Snapshot.URL, "title": obs.Snapshot.Title}
		data["page_flags"] = obs.Snapshot.PageFlags
	}
	for k, v := range extra {
		if llmHiddenExtraKey(k) || failureDroppedExtraKey(k) {
			continue
		}
		data[k] = v
	}
	return data
}

func compactFailureDataFromResult(result *BrowserActionResult) map[string]interface{} {
	if result == nil {
		return nil
	}
	extra := map[string]interface{}{}
	var obs *BrowserObservation
	if result.Data != nil {
		for k, v := range result.Data {
			extra[k] = v
		}
		snap := BrowserSnapshot{
			SnapshotID: result.SnapshotID,
		}
		if url, ok := result.Data["url"].(string); ok {
			snap.URL = url
		}
		if title, ok := result.Data["title"].(string); ok {
			snap.Title = title
		}
		if flags, ok := result.Data["page_flags"].(BrowserPageFlags); ok {
			snap.PageFlags = flags
		}
		if state, ok := result.Data["page_state"].(map[string]interface{}); ok {
			if ready, ok := state["ready_state"].(string); ok {
				snap.ReadyState = ready
			}
			if url, ok := state["url"].(string); ok && snap.URL == "" {
				snap.URL = url
			}
			if title, ok := state["title"].(string); ok && snap.Title == "" {
				snap.Title = title
			}
		}
		obs = &BrowserObservation{Snapshot: snap}
	} else if result.SnapshotID != "" {
		obs = &BrowserObservation{Snapshot: BrowserSnapshot{SnapshotID: result.SnapshotID}}
	}
	return compactFailureData(obs, extra)
}

func failureDroppedExtraKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "refs", "refs_truncated", "frame_tree", "console_summary", "network_summary", "page_text_excerpt", "page_text_total", "page_text_offset", "page_text_has_more", "vision_excerpt", "tab_id":
		return true
	default:
		return false
	}
}

func llmHiddenExtraKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "selector", "selector_candidates", "bounding_box", "backend_node_id", "backendnodeid", "frame_id", "frame_tree", "tab_id":
		return true
	default:
		return false
	}
}
