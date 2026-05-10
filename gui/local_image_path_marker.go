package main

import (
	"path/filepath"
	"strings"
)

type localImagePathLineKind int

const (
	localImagePathLineOther localImagePathLineKind = iota
	localImagePathLineInstruction
	localImagePathLineImagePath
)

func classifyLocalImagePathLine(line string) localImagePathLineKind {
	trimmed := strings.TrimSpace(strings.TrimPrefix(line, "-"))
	if trimmed == "" {
		return localImagePathLineOther
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "这是用户已经提供的本地图片文件") ||
		strings.HasPrefix(lower, "请直接使用这些路径") ||
		strings.HasPrefix(lower, "杩欐槸鐢ㄦ埛宸茬粡鎻愪緵鐨勬湰鍦板浘鐗囨枃浠?") ||
		strings.HasPrefix(lower, "璇风洿鎺ヤ娇鐢ㄨ繖浜涜矾寰?"):
		return localImagePathLineInstruction
	case isLocalImagePathExtension(filepath.Ext(lower)):
		return localImagePathLineImagePath
	default:
		return localImagePathLineOther
	}
}

func isLocalImagePathExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tif", ".tiff":
		return true
	default:
		return false
	}
}
