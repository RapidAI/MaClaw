package guiapp

import (
	"os"
	"strings"
)

// uiScreenshotsEnvKey opts into startup diagnostic screenshots and DOM dumps.
// They are off by default: a full-window PrintWindow plus PNG encode is a
// multi-hundred-millisecond stall on a large display, and the output is only
// useful to someone debugging a rendering failure. The [perf] timeline marks
// replace them as the always-on diagnostic.
const uiScreenshotsEnvKey = "MACLAW_UI_SCREENSHOTS"

func uiScreenshotsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(uiScreenshotsEnvKey))) {
	case "1", "true", "yes", "on", "verbose":
		return true
	}
	return false
}
