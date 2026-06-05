package browser

import (
	"fmt"
	"strings"
	"time"
)

// ResolveRef resolves a ref from a snapshot or the latest observation.
func (s *BrowserAgentSession) ResolveRef(snapshotID, ref string) (*BrowserElementRef, error) {
	if s == nil {
		return nil, fmt.Errorf("browser session is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if snapshotID == "" {
		snapshotID = s.lastSnapshotID
	}
	snap, ok := s.snapshots[snapshotID]
	if !ok || snap == nil {
		return nil, fmt.Errorf("browser snapshot not found: %s", snapshotID)
	}
	for _, item := range snap.Refs {
		if strings.EqualFold(item.Ref, ref) {
			cp := item
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("browser ref not found: %s", ref)
}

func (s *BrowserAgentSession) selectorCandidatesForRef(snapshotID, ref string) ([]string, *BrowserElementRef, error) {
	resolved, err := s.ResolveRef(snapshotID, ref)
	if err != nil {
		return nil, nil, err
	}
	return selectorCandidatesFromResolvedRef(resolved)
}

func (s *BrowserAgentSession) selectorCandidatesForText(snapshotID, text string) ([]string, *BrowserElementRef, error) {
	text = normalizeElementText(text)
	if text == "" {
		return nil, nil, fmt.Errorf("missing text")
	}
	if s == nil {
		return nil, nil, fmt.Errorf("browser session is nil")
	}
	s.mu.RLock()
	if snapshotID == "" {
		snapshotID = s.lastSnapshotID
	}
	snap, ok := s.snapshots[snapshotID]
	s.mu.RUnlock()
	if !ok || snap == nil {
		return nil, nil, fmt.Errorf("browser snapshot not found: %s", snapshotID)
	}

	var exact *BrowserElementRef
	var contains *BrowserElementRef
	for _, item := range snap.Refs {
		name := normalizeElementText(item.Name)
		body := normalizeElementText(item.Text)
		if name == text || body == text {
			cp := item
			exact = &cp
			break
		}
		if contains == nil && (strings.Contains(name, text) || strings.Contains(body, text) || strings.Contains(text, name) && name != "") {
			cp := item
			contains = &cp
		}
	}
	resolved := exact
	if resolved == nil {
		resolved = contains
	}
	if resolved == nil {
		return nil, nil, fmt.Errorf("browser element text not found: %s", text)
	}
	return selectorCandidatesFromResolvedRef(resolved)
}

func selectorCandidatesFromResolvedRef(resolved *BrowserElementRef) ([]string, *BrowserElementRef, error) {
	if resolved == nil {
		return nil, nil, fmt.Errorf("browser ref is nil")
	}
	candidates := make([]string, 0, 1+len(resolved.SelectorCandidates))
	if strings.TrimSpace(resolved.Selector) != "" {
		candidates = append(candidates, strings.TrimSpace(resolved.Selector))
	}
	for _, candidate := range resolved.SelectorCandidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		duplicate := false
		for _, existing := range candidates {
			if existing == candidate {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return nil, resolved, fmt.Errorf("ref %s has no selector candidates; run observe again", resolved.Ref)
	}
	return candidates, resolved, nil
}

func normalizeElementText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(text)), " "))
}

func (s *BrowserAgentSession) selectorForAction(snapshotID, ref, selector string) (string, *BrowserElementRef, error) {
	selector = strings.TrimSpace(selector)
	ref = strings.TrimSpace(ref)
	if ref != "" {
		candidates, resolved, err := s.selectorCandidatesForRef(snapshotID, ref)
		if err != nil {
			return "", resolved, err
		}
		for _, candidate := range candidates {
			if err := s.session.WaitForSelector(candidate, 1); err == nil {
				return candidate, resolved, nil
			}
		}
		return "", resolved, fmt.Errorf("ref %s is stale; run observe again to get fresh refs", ref)
	}
	if selector == "" {
		return "", nil, fmt.Errorf("missing ref or selector")
	}
	return selector, nil, nil
}

func (s *BrowserAgentSession) appendActionTrace(kind, summary string) {
	if s == nil {
		return
	}
	now := time.Now().UnixMilli()
	s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: kind, Summary: summary, CreatedAt: now}, browserAgentConsoleLimit)
	s.activityLog = appendCapped(s.activityLog, summary, browserAgentConsoleLimit)
	s.UpdatedAt = time.Now()
}
