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
		return nil, resolved, fmt.Errorf("ref %s 没有关联 selector，请重新 observe", ref)
	}
	return candidates, resolved, nil
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
		return "", resolved, fmt.Errorf("ref %s 已失效，请重新 observe 获取新的 refs", ref)
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
