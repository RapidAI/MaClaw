//go:build !windows

package guiapp

import (
	"os"
	"strings"
)

func detectOSAppLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return string(normalizeAppLanguageKind(v))
		}
	}
	return string(appLanguageEnglish)
}
