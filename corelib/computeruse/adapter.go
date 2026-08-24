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

// MatchAdapter inspects window titles (and optional crop title) for a known app family.
func MatchAdapter(windows []string, cropTitle string) AdapterHint {
	blob := strings.ToLower(strings.TrimSpace(strings.Join(append(append([]string{}, windows...), cropTitle), " ")))
	if blob == "" {
		return AdapterHint{}
	}
	switch {
	case containsAny(blob, "microsoft word", "winword", "excel", "powerpoint", "wps", "libreoffice", "pages", "numbers", "keynote", "docx", "xlsx", "pptx", " - word", "文字", "表格", "演示"):
		return AdapterHint{Kind: AdapterOffice, Advice: "Office window: prefer office_read / document tools for content; Computer Use only for ribbon, dialogs, and Save."}
	case containsAny(blob, "file explorer", "explorer", "finder", "nautilus", "dolphin"):
		return AdapterHint{Kind: AdapterShell, Advice: "File manager: prefer shell / file tools to open paths; Computer Use only for picker dialogs."}
	case containsAny(blob, "google chrome", "microsoft edge", "firefox", "safari", "chromium"):
		return AdapterHint{Kind: AdapterBrowser, Advice: "Browser window: prefer browser_* tools. Computer Use is for native chrome only (download bar, OS dialogs)."}
	case containsAny(blob, "weixin", "wechat", "微信", "slack", "telegram", "discord", "qq", "钉钉", "feishu", "lark", "teams"):
		return AdapterHint{Kind: AdapterIM, Advice: "IM window: use the search box first (computer_find 搜索/Search), type the name, re-observe. Do not scroll a long contact list blindly."}
	case containsAny(blob, "visual studio code", "vscode", "notepad", "textedit", "gedit", "sublime", "notepad++"):
		return AdapterHint{Kind: AdapterEditor, Advice: "Editor window: type into the document via computer_type after focusing the text area; use computer_key for shortcuts."}
	default:
		return AdapterHint{}
	}
}

func containsAny(blob string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(blob, n) {
			return true
		}
	}
	return false
}
