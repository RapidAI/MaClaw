package browser

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// AmbiguousElementError is returned when visible text matches more than one
// element at the best rank. The model must pick a ref instead of guessing.
type AmbiguousElementError struct {
	Query string
	Refs  []BrowserElementRef
}

func (e *AmbiguousElementError) Error() string {
	if e == nil {
		return "ambiguous browser element"
	}
	parts := make([]string, 0, len(e.Refs))
	for _, ref := range e.Refs {
		parts = append(parts, ref.Ref)
	}
	return fmt.Sprintf("ambiguous element text %q matches %s; run observe and click by ref", e.Query, strings.Join(parts, ", "))
}

func rejectDisabledRef(resolved *BrowserElementRef) error {
	if resolved == nil || !resolved.Disabled {
		return nil
	}
	label := strings.TrimSpace(resolved.Ref)
	if label == "" {
		label = firstNonEmpty(resolved.Name, resolved.Text)
	}
	return fmt.Errorf("ref %s is disabled; observe again and pick an enabled control", label)
}

func (s *BrowserAgentSession) selectorCandidatesForText(snapshotID, text string) ([]string, *BrowserElementRef, error) {
	resolved, err := s.resolveUniqueText(snapshotID, text)
	if err != nil {
		return nil, nil, err
	}
	return selectorCandidatesFromResolvedRef(resolved)
}

func (s *BrowserAgentSession) resolveUniqueText(snapshotID, text string) (*BrowserElementRef, error) {
	text = normalizeElementText(text)
	if text == "" {
		return nil, fmt.Errorf("missing text")
	}
	if s == nil {
		return nil, fmt.Errorf("browser session is nil")
	}
	s.mu.RLock()
	if snapshotID == "" {
		snapshotID = s.lastSnapshotID
	}
	snap, ok := s.snapshots[snapshotID]
	s.mu.RUnlock()
	if !ok || snap == nil {
		return nil, fmt.Errorf("browser snapshot not found: %s", snapshotID)
	}

	var exact, prefix, contains, reverse []BrowserElementRef
	for _, item := range snap.Refs {
		name := normalizeElementText(item.Name)
		body := normalizeElementText(item.Text)
		cp := item
		switch {
		case name == text || body == text:
			exact = append(exact, cp)
		case strings.HasPrefix(name, text) || strings.HasPrefix(body, text):
			prefix = append(prefix, cp)
		case strings.Contains(name, text) || strings.Contains(body, text):
			contains = append(contains, cp)
		case utf8.RuneCountInString(name) >= 4 && strings.Contains(text, name):
			reverse = append(reverse, cp)
		}
	}
	for _, group := range [][]BrowserElementRef{exact, prefix, contains, reverse} {
		if len(group) == 1 {
			cp := group[0]
			return &cp, nil
		}
		if len(group) > 1 {
			return nil, &AmbiguousElementError{Query: text, Refs: group}
		}
	}
	return nil, fmt.Errorf("browser element text not found: %s", text)
}

func uniqueSelectorCandidates(candidates []string) []string {
	out := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func (s *BrowserAgentSession) locatorCandidates(snapshotID, ref, selector string) ([]string, *BrowserElementRef, error) {
	selector = strings.TrimSpace(selector)
	ref = strings.TrimSpace(ref)
	if ref != "" {
		resolved, err := s.ResolveRef(snapshotID, ref)
		if err != nil {
			return nil, nil, err
		}
		candidates := uniqueSelectorCandidates(append([]string{resolved.Selector}, resolved.SelectorCandidates...))
		if len(candidates) == 0 && resolved.BackendNodeID == 0 {
			return nil, resolved, fmt.Errorf("ref %s has no selector candidates; run observe again", resolved.Ref)
		}
		return candidates, resolved, nil
	}
	if selector == "" {
		return nil, nil, fmt.Errorf("missing ref or selector")
	}
	if err := s.rejectNonUniqueSelector(selector); err != nil {
		return nil, nil, err
	}
	return []string{selector}, nil, nil
}

func (s *BrowserAgentSession) rejectNonUniqueSelector(selector string) error {
	if s == nil || s.session == nil || strings.TrimSpace(selector) == "" {
		return nil
	}
	n, err := s.session.countSelector(selector)
	if err != nil || n < 0 {
		return nil
	}
	if n > 1 {
		return fmt.Errorf("selector matches %d elements; run observe and click by ref", n)
	}
	return nil
}
