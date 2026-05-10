package remote

import "strings"

type sudoPasswordMarkerKind int

const (
	sudoPasswordMarkerUnknown sudoPasswordMarkerKind = iota
	sudoPasswordMarkerRejected
)

func classifySudoPasswordMarker(line string) sudoPasswordMarkerKind {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "sorry"),
		strings.Contains(lower, "incorrect"),
		strings.Contains(lower, "authentication failure"),
		strings.Contains(lower, "try again"):
		return sudoPasswordMarkerRejected
	default:
		return sudoPasswordMarkerUnknown
	}
}
