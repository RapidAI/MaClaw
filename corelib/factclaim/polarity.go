package factclaim

import "strings"

var unreachableMarkers = []string{
	"unreachable", "timed out", "no route to host",
	"network is unreachable", "host is down", "connection refused",
	"connection timed out", "100% packet loss", "100% loss",
	"destination host unreachable", "不通", "无法连接", "连不上",
	"连接失败", "连接超时", "主机不可达", "不可达", "当前不可达",
	"不能通", "不可以通", "not connected to", "not connected",
	"not reachable", "cannot connect", "can't connect",
	"failed to connect", "无法连通",
}

var reachableMarkers = []string{
	"0% packet loss", "0% loss", "bytes from", "reply from",
	"connected to", "connection established", "is alive",
	" is reachable", "currently reachable", "当前可达", "已连通", "能通", "可以通",
}

// DetectPolarity classifies reachability language. The rightmost match wins;
// at the same end index the longer marker wins.
func DetectPolarity(text string) Polarity {
	lower := strings.ToLower(text)
	_, unreachEnd := lastMarkerMatch(lower, unreachableMarkers)
	_, reachEnd := lastMarkerMatch(lower, reachableMarkers)
	switch {
	case unreachEnd >= 0 && reachEnd >= 0:
		if unreachEnd >= reachEnd {
			return PolarityUnreachable
		}
		return PolarityReachable
	case unreachEnd >= 0:
		return PolarityUnreachable
	case reachEnd >= 0:
		return PolarityReachable
	default:
		return PolarityUnknown
	}
}

func lastMarkerMatch(lower string, markers []string) (start, end int) {
	start, end = -1, -1
	bestLen := 0
	for _, marker := range markers {
		if marker == "" {
			continue
		}
		idx := strings.LastIndex(lower, marker)
		if idx < 0 {
			continue
		}
		markerEnd := idx + len(marker)
		if markerEnd > end || (markerEnd == end && len(marker) > bestLen) {
			start = idx
			end = markerEnd
			bestLen = len(marker)
		}
	}
	return start, end
}
