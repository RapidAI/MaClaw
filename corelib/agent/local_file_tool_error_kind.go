package agent

import "strings"

type localFileToolErrorKind int

const (
	localFileToolErrorUnknown localFileToolErrorKind = iota
	localFileToolErrorUnsupportedMode
	localFileToolErrorMissingOldString
	localFileToolErrorNoReplacementMatch
)

func classifyLocalFileToolError(err error) localFileToolErrorKind {
	if err == nil {
		return localFileToolErrorUnknown
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "不支持的 mode") || strings.Contains(msg, "涓嶆敮鎸佺殑 mode"):
		return localFileToolErrorUnsupportedMode
	case strings.Contains(msg, "缺少 old_string 参数") || strings.Contains(msg, "缂哄皯 old_string 鍙傛暟"):
		return localFileToolErrorMissingOldString
	case strings.Contains(msg, "未找到要替换的内容") || strings.Contains(msg, "鏈壘鍒拌鏇挎崲鐨勫唴瀹"):
		return localFileToolErrorNoReplacementMatch
	default:
		return localFileToolErrorUnknown
	}
}

func (k localFileToolErrorKind) ReturnRawError() bool {
	switch k {
	case localFileToolErrorUnsupportedMode, localFileToolErrorMissingOldString, localFileToolErrorNoReplacementMatch:
		return true
	default:
		return false
	}
}
