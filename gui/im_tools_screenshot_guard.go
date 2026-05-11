package main

import (
	"fmt"
	"strings"
	"time"
)

const screenshotCooldown = 30 * time.Second

func screenshotLocalImagePathGuardMessage(userText string) string {
	if !hasSelectedLocalImagePath(userText) {
		return ""
	}
	return "用户消息里已经提供了本地图片文件路径。不要调用 screenshot 或重新截图；请直接使用这些路径，并优先用 read_file 或 open 查看图片内容。"
}

func screenshotCooldownMessage(lastScreenshotAt time.Time, now time.Time) string {
	if lastScreenshotAt.IsZero() {
		return ""
	}
	elapsed := now.Sub(lastScreenshotAt)
	if elapsed >= screenshotCooldown {
		return ""
	}
	remaining := screenshotCooldown - elapsed
	return fmt.Sprintf("截屏冷却中，请等待 %d 秒后再试", int(remaining.Seconds())+1)
}

func hasSelectedLocalImagePath(userText string) bool {
	lower := strings.ToLower(userText)
	idx := strings.Index(lower, strings.ToLower(filePathPromptPrefix))
	if idx < 0 {
		return false
	}
	block := userText[idx+len(filePathPromptPrefix):]
	for _, line := range strings.Split(block, "\n") {
		if classifyLocalImagePathLine(line) == localImagePathLineImagePath {
			return true
		}
	}
	return false
}
