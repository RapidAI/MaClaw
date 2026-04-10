package memory

import (
	"fmt"
	"strings"
)

// FindBySubstring returns entries whose Content contains the given substring.
// The search is case-insensitive. Returns an error if no matches are found.
func (s *Store) FindBySubstring(text string) ([]Entry, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("memory_store: search text must not be empty")
	}

	needle := strings.ToLower(strings.TrimSpace(text))

	s.mu.RLock()
	defer s.mu.RUnlock()

	var matches []Entry
	for _, e := range s.entries {
		if strings.Contains(strings.ToLower(e.Content), needle) {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("memory_store: no entry contains %q", text)
	}
	return matches, nil
}

// DeleteBySubstring deletes the entry uniquely identified by the given
// substring. Returns an error if zero or multiple entries match.
func (s *Store) DeleteBySubstring(text string) error {
	matches, err := s.FindBySubstring(text)
	if err != nil {
		return err
	}
	if len(matches) > 1 {
		return fmt.Errorf("memory_store: %d entries match %q, provide a more specific substring", len(matches), text)
	}
	return s.Delete(matches[0].ID)
}

// ReplaceBySubstring finds the entry uniquely identified by oldText and
// replaces its content with newContent. Returns an error if zero or
// multiple entries match.
func (s *Store) ReplaceBySubstring(oldText, newContent string, category Category, tags []string) error {
	matches, err := s.FindBySubstring(oldText)
	if err != nil {
		return err
	}
	if len(matches) > 1 {
		return fmt.Errorf("memory_store: %d entries match %q, provide a more specific substring", len(matches), oldText)
	}
	return s.Update(matches[0].ID, newContent, category, tags)
}
