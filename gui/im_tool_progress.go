package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func userFacingToolProgressText(toolName string) string {
	switch toolName {
	case "craft_tool":
		return "馃洜锔?姝ｅ湪鐢熸垚骞舵墽琛岃剼鏈紝鍑嗗缁х画瀹屾垚浜や粯..."
	case "bash":
		return "馃枼锔?姝ｅ湪鎵ц鍛戒护澶勭悊鏂囦欢锛岃绋嶅€?.."
	case "run_skill":
		return "馃殌 姝ｅ湪鍚姩 Skill 骞剁瓑寰呯姸鎬佸揩鐓?.."
	case "send_file":
		return "馃摛 姝ｅ湪鏁寸悊骞跺彂閫佺敓鎴愮殑鏂囦欢..."
	case "generate_pdf":
		return "馃搫 姝ｅ湪鐢熸垚 PDF 鏂囦欢..."
	default:
		return "鈿欙笍 姝ｅ湪鎵ц宸ュ叿锛岃绋嶅€?.."
	}
}

func shouldExposeToolInternalProgress(toolName string) bool {
	switch toolName {
	case "craft_tool", "bash", "run_skill":
		return true
	default:
		return false
	}
}

func filterUserFacingToolProgress(toolName, msg string) string {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return ""
	}
	if shouldExposeToolInternalProgress(toolName) {
		switch toolName {
		case "craft_tool":
			allowedPrefixes := []string{"馃 ", "馃捑 ", "馃殌 ", "馃摝 ", "鈴?"}
			for _, prefix := range allowedPrefixes {
				if strings.HasPrefix(trimmed, prefix) {
					return trimmed
				}
			}
			return ""
		case "bash":
			if strings.HasPrefix(trimmed, "鈴?") {
				return trimmed
			}
			return ""
		case "run_skill":
			allowedPrefixes := []string{"馃殌 ", "鈴?", "鉁?", "鉂?"}
			for _, prefix := range allowedPrefixes {
				if strings.HasPrefix(trimmed, prefix) {
					return trimmed
				}
			}
			return ""
		}
	}
	return ""
}

func filteredToolProgressCallback(toolName string, onProgress tool.ProgressCallback, debug bool) tool.ProgressCallback {
	if debug {
		return onProgress
	}
	if onProgress == nil || !shouldExposeToolInternalProgress(toolName) {
		return nil
	}
	return func(msg string) {
		if filtered := filterUserFacingToolProgress(toolName, msg); filtered != "" {
			onProgress(filtered)
		}
	}
}
