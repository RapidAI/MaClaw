package browser

import "strings"

type browserLaunchErrorKind int

const (
	browserLaunchErrorUnknown browserLaunchErrorKind = iota
	browserLaunchErrorExited
)

func classifyBrowserLaunchError(err error) browserLaunchErrorKind {
	if err == nil {
		return browserLaunchErrorUnknown
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "立即退出") || strings.Contains(msg, "绔嬪嵆閫€鍑") || strings.Contains(msg, "exited"):
		return browserLaunchErrorExited
	default:
		return browserLaunchErrorUnknown
	}
}

func (k browserLaunchErrorKind) IsProfileLockLikely() bool {
	return k == browserLaunchErrorExited
}
