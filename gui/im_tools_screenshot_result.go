package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

const (
	screenshotIMInlineSizeLimit = 1_500_000
	screenshotIMDownsizeTarget  = 1_200_000
)

func formatScreenshotBase64Result(base64Data string) string {
	return fmt.Sprintf("[screenshot_base64]%s", downsizeScreenshotForIM(base64Data))
}

func downsizeScreenshotForIM(base64Data string) string {
	if len(base64Data) <= screenshotIMInlineSizeLimit {
		return base64Data
	}
	if downsized, err := remote.DownsizeScreenshotBase64(base64Data, screenshotIMDownsizeTarget); err == nil {
		return downsized
	}
	return base64Data
}
