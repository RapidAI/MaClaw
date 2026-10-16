package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopDeliverSizeHint: deliver of a file over the chat attachment cap
// came back as a bare "file is too large" (HTTP 400), and the model spent a
// round guessing the limit (seen live 10-15 05:39, a 165 KB docx). The cap
// is ours; the error now says it and how to get under it.
func desktopDeliverSizeHint(args map[string]any, err error) error {
	if err == nil || strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))) != "deliver" ||
		!strings.Contains(err.Error(), "file is too large") {
		return err
	}
	return fmt.Errorf("%w (deliver limit is %d bytes per file; shrink it, e.g. recompress pictures as JPEG at about 1000 px wide, then check the size with ls -l and deliver again)", err, desktop.FileBytesMax)
}
