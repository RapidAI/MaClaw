package computeruse

import "strings"

// AdapterKind is a coarse app family. When observe matches, the playbook
// steers the model toward existing specialist tools instead of pixel-clicking.
type AdapterKind string

const (
	AdapterGeneric AdapterKind = ""
	AdapterOffice  AdapterKind = "office"
	AdapterShell   AdapterKind = "shell"
	AdapterIM      AdapterKind = "im"
	AdapterBrowser AdapterKind = "browser"
	AdapterEditor  AdapterKind = "editor"
)

// AdapterHint is injected into observe text when the focused window matches.
type AdapterHint struct {
	Kind   AdapterKind
	Advice string
}

// MatchAdapter inspects the focused window for a known app family.
// A non-empty crop title wins: background windows must not relabel the app
// the user is actually operating. With no crop, mixed families produce no
// hint rather than letting whichever app was enumerated first steer the model.
func MatchAdapter(windows []string, cropTitle string) AdapterHint {
	if strings.TrimSpace(cropTitle) != "" {
		return adapterAdvice(kindOfTitle(cropTitle))
	}
	var found AdapterHint
	seen := false
	for _, title := range windows {
		hint := adapterAdvice(kindOfTitle(title))
		if hint.Kind == "" {
			continue
		}
		if !seen {
			found = hint
			seen = true
			continue
		}
		if found.Kind != hint.Kind {
			return AdapterHint{}
		}
	}
	return found
}

func adapterAdvice(kind AdapterKind) AdapterHint {
	switch kind {
	case AdapterOffice:
		return AdapterHint{Kind: AdapterOffice, Advice: "Office window: prefer office_read / document tools for content; Computer Use only for ribbon, dialogs, and Save."}
	case AdapterShell:
		return AdapterHint{Kind: AdapterShell, Advice: "File manager: prefer shell / file tools to open paths; Computer Use only for picker dialogs."}
	case AdapterBrowser:
		return AdapterHint{Kind: AdapterBrowser, Advice: "Browser window: prefer browser_* tools. Computer Use is for native chrome only (download bar, OS dialogs)."}
	case AdapterIM:
		return AdapterHint{Kind: AdapterIM, Advice: "IM window: use the search box first (computer_find 搜索/Search), type the name, re-observe. Do not scroll a long contact list blindly."}
	case AdapterEditor:
		return AdapterHint{Kind: AdapterEditor, Advice: "Editor window: type into the document via computer_type after focusing the text area; use computer_key for shortcuts."}
	default:
		return AdapterHint{}
	}
}
