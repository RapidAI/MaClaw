package memory

import (
	"strings"

	corebm25 "github.com/RapidAI/CodeClaw/corelib/bm25"
)

// strictRecallAnchors returns the spans a composite or unknown name must
// actually contain. A single dictionary word returns nil so synonym recall
// stays available.
func strictRecallAnchors(query string) []string {
	anchors, strict := corebm25.QueryAnchors(strings.TrimSpace(query))
	if !strict || len(anchors) == 0 {
		return nil
	}
	return anchors
}

// entryMentionsAnchors reports that every anchor appears in the entry's own
// text. Fields stay separated so the last character of one field cannot form
// a name with the first character of the next.
func entryMentionsAnchors(e Entry, anchors []string) bool {
	if len(anchors) == 0 {
		return true
	}
	hay := strings.ToLower(entryAnchorHaystack(e))
	for _, anchor := range anchors {
		anchor = strings.ToLower(strings.TrimSpace(anchor))
		if anchor == "" || !strings.Contains(hay, anchor) {
			return false
		}
	}
	return true
}

func entryAnchorHaystack(e Entry) string {
	parts := make([]string, 0, 3+len(e.Tags))
	parts = append(parts, e.Content, e.Title, e.CompactForm)
	parts = append(parts, e.Tags...)
	return strings.Join(parts, "\n")
}

func filterRecallByAnchors(candidates []recallScored, anchors []string) []recallScored {
	if len(anchors) == 0 || len(candidates) == 0 {
		return candidates
	}
	kept := make([]recallScored, 0, len(candidates))
	for _, candidate := range candidates {
		if entryMentionsAnchors(candidate.entry, anchors) {
			kept = append(kept, candidate)
		}
	}
	return kept
}
