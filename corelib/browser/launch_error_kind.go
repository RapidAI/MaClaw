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
	case strings.Contains(msg, "\u7acb\u5373\u9000\u51fa") || strings.Contains(msg, "exited"):
		return browserLaunchErrorExited
	default:
		return browserLaunchErrorUnknown
	}
}

func (k browserLaunchErrorKind) IsProfileLockLikely() bool {
	return k == browserLaunchErrorExited
}
