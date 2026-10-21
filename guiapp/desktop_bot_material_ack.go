package guiapp

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// desktopBotMaterialNames are the file names of a trailing [素材] list.
func desktopBotMaterialNames(message string) []string {
	if !desktopBotHasMaterials(message) {
		return nil
	}
	at := strings.LastIndex(message, "[素材]\n")
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(message[at+len("[素材]\n"):]), "\n") {
		p := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if name := path.Base(p); name != "" && name != "." && name != "/" {
			names = append(names, name)
		}
	}
	return names
}

// desktopBotWordsBeforeMaterials is the typed text without the [素材] list.
func desktopBotWordsBeforeMaterials(message string) string {
	if !desktopBotHasMaterials(message) {
		return strings.TrimSpace(message)
	}
	return strings.TrimSpace(message[:strings.LastIndex(message, "[素材]\n")])
}

// desktopBotLooksEnglish: no Han characters in the typed words.
func desktopBotLooksEnglish(words string) bool {
	for _, r := range words {
		if unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return strings.TrimSpace(words) != ""
}

// desktopBotFileList names up to three files: 《a》、《b》, or 《a》、《b》等 5 个文件.
func desktopBotFileList(names []string, en bool) string {
	shown := names
	if len(shown) > 3 {
		shown = shown[:2]
	}
	if en {
		quoted := make([]string, len(shown))
		for i, n := range shown {
			quoted[i] = "\"" + n + "\""
		}
		out := strings.Join(quoted, ", ")
		if len(names) > len(shown) {
			out += fmt.Sprintf(" and %d more", len(names)-len(shown))
		}
		return out
	}
	quoted := make([]string, len(shown))
	for i, n := range shown {
		quoted[i] = "《" + n + "》"
	}
	out := strings.Join(quoted, "、")
	if len(names) > len(shown) {
		out += fmt.Sprintf("等 %d 个文件", len(names))
	}
	return out
}

func desktopBotMentionsAny(told string, names []string) bool {
	for _, n := range names {
		stem := strings.TrimSuffix(n, path.Ext(n))
		if strings.Contains(told, n) || (utf8.RuneCountInString(stem) >= 4 && strings.Contains(told, stem)) {
			return true
		}
	}
	return false
}
