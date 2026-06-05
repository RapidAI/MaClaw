package intent

import "strings"

// KeywordMatch represents a keyword hit during diagnostic recall.
type KeywordMatch struct {
	Entry    KeywordEntry
	Position int // byte offset in text
}

// KeywordRegistry holds keyword evidence organized by IntentLabel.
// Keyword evidence is not an execution-route authority.
type KeywordRegistry struct {
	entries      []KeywordEntry
	byLabel      map[IntentLabel][]KeywordEntry
	strongIndex  map[string]IntentLabel
	weakByLabel  map[IntentLabel][]string
	lowerEntries []string
}

// NewKeywordRegistry creates the registry from diagnostic keyword evidence.
func NewKeywordRegistry() *KeywordRegistry {
	return newKeywordRegistryFromEntries(defaultKeywords)
}

// newKeywordRegistryFromEntries builds a KeywordRegistry from an arbitrary
// keyword entry list. Shared by NewKeywordRegistry and definition-derived data.
func newKeywordRegistryFromEntries(keywords []KeywordEntry) *KeywordRegistry {
	r := &KeywordRegistry{
		entries:      make([]KeywordEntry, 0, len(keywords)),
		byLabel:      make(map[IntentLabel][]KeywordEntry),
		strongIndex:  make(map[string]IntentLabel),
		weakByLabel:  make(map[IntentLabel][]string),
		lowerEntries: make([]string, 0, len(keywords)),
	}

	priority := map[IntentLabel]int{
		LabelSSH:              0,
		LabelBrowser:          1,
		LabelCoding:           2,
		LabelNonCoding:        3,
		LabelAmbiguous:        4,
		LabelSearch:           5,
		LabelDocumentDelivery: 6,
		LabelBusinessData:     7,
		LabelBugFix:           8,
		LabelContinuation:     9,
		LabelMaintenance:      10,
		LabelOffice:           11,
		LabelUnknown:          12,
	}

	type entryKey struct {
		keyword  string
		label    IntentLabel
		strength KeywordStrength
	}
	seen := make(map[entryKey]bool)

	for _, e := range keywords {
		lowerKW := strings.ToLower(e.Keyword)
		key := entryKey{lowerKW, e.Label, e.Strength}
		if seen[key] {
			continue
		}
		seen[key] = true

		r.entries = append(r.entries, e)
		r.byLabel[e.Label] = append(r.byLabel[e.Label], e)
		r.lowerEntries = append(r.lowerEntries, lowerKW)
		if e.Strength == Strong {
			if existing, ok := r.strongIndex[lowerKW]; ok {
				if priority[e.Label] < priority[existing] {
					r.strongIndex[lowerKW] = e.Label
				}
				continue
			}
			r.strongIndex[lowerKW] = e.Label
		} else {
			r.weakByLabel[e.Label] = append(r.weakByLabel[e.Label], lowerKW)
		}
	}

	return r
}

// Match returns all diagnostic keyword evidence found in the text.
func (r *KeywordRegistry) Match(text string) []KeywordMatch {
	lower := strings.ToLower(text)
	var matches []KeywordMatch
	for i, e := range r.entries {
		pos := strings.Index(lower, r.lowerEntries[i])
		if pos >= 0 {
			matches = append(matches, KeywordMatch{Entry: e, Position: pos})
		}
	}
	return matches
}

// defaultKeywords is a compact diagnostic vocabulary. It feeds metadata,
// calibration, and prompts, but never directly authorizes workflow transitions
// or tool activation.
var defaultKeywords = []KeywordEntry{
	{Keyword: "ssh", Label: LabelSSH, Strength: Weak},
	{Keyword: "ssh into", Label: LabelSSH, Strength: Strong},
	{Keyword: "remote server", Label: LabelSSH, Strength: Strong},
	{Keyword: "server logs", Label: LabelSSH, Strength: Strong},
	{Keyword: "restart service", Label: LabelSSH, Strength: Strong},

	{Keyword: "browser", Label: LabelBrowser, Strength: Strong},
	{Keyword: "playwright", Label: LabelBrowser, Strength: Strong},
	{Keyword: "登录知乎", Label: LabelBrowser, Strength: Strong},
	{Keyword: "发表", Label: LabelBrowser, Strength: Weak},
	{Keyword: "发布", Label: LabelBrowser, Strength: Weak},
	{Keyword: "发帖", Label: LabelBrowser, Strength: Strong},
	{Keyword: "publish", Label: LabelBrowser, Strength: Weak},
	{Keyword: "sign in", Label: LabelBrowser, Strength: Strong},
	{Keyword: "web page", Label: LabelBrowser, Strength: Weak},
	{Keyword: "click", Label: LabelBrowser, Strength: Weak},

	{Keyword: "search", Label: LabelSearch, Strength: Strong},
	{Keyword: "paper", Label: LabelSearch, Strength: Strong},
	{Keyword: "news", Label: LabelSearch, Strength: Strong},

	{Keyword: "send file", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "export pdf", Label: LabelDocumentDelivery, Strength: Strong},
	{Keyword: "attachment", Label: LabelDocumentDelivery, Strength: Strong},

	{Keyword: "business transaction", Label: LabelBusinessData, Strength: Strong},
	{Keyword: "structured business data", Label: LabelBusinessData, Strength: Strong},
	{Keyword: "expense reimbursement", Label: LabelBusinessData, Strength: Weak},
	{Keyword: "invoice approval", Label: LabelBusinessData, Strength: Weak},

	{Keyword: "write code", Label: LabelCoding, Strength: Strong},
	{Keyword: "implement feature", Label: LabelCoding, Strength: Strong},
	{Keyword: "build app", Label: LabelCoding, Strength: Strong},

	{Keyword: "fix bug", Label: LabelBugFix, Strength: Strong},
	{Keyword: "debug crash", Label: LabelBugFix, Strength: Strong},

	{Keyword: "refactor", Label: LabelMaintenance, Strength: Strong},
	{Keyword: "optimize", Label: LabelMaintenance, Strength: Strong},

	{Keyword: "summarize", Label: LabelNonCoding, Strength: Strong},
	{Keyword: "translate", Label: LabelNonCoding, Strength: Strong},

	{Keyword: "ppt", Label: LabelOffice, Strength: Strong},
	{Keyword: "spreadsheet", Label: LabelOffice, Strength: Strong},

	{Keyword: "continue", Label: LabelContinuation, Strength: Weak},
	{Keyword: "go ahead", Label: LabelContinuation, Strength: Weak},
}
