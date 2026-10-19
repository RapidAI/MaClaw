package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// errCloudProgressStaysInContainer refuses a cloud-desktop progress file in
// the local GUI data directory. That file lives in the container home.
var errCloudProgressStaysInContainer = fmt.Errorf("cloud desktop progress stays in the container home")

// writeLocalTaskProgress replaces the three sections of one local background
// task. A cloud desktop bot does not get a copy under root.
func writeLocalTaskProgress(root, taskID string, cloud bool, sections desktopbot.Sections) (string, desktopbot.Sections, error) {
	if cloud {
		return "", desktopbot.Sections{}, errCloudProgressStaysInContainer
	}
	if strings.TrimSpace(root) == "" {
		return "", desktopbot.Sections{}, fmt.Errorf("local progress directory is empty")
	}
	return desktopbot.WriteProgressFile(root, desktopbot.ProgressKindTask, taskID, sections)
}
