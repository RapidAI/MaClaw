package main

import "strings"

type incompleteTaskMarkerKind int

const (
	incompleteTaskMarkerNone incompleteTaskMarkerKind = iota
	incompleteTaskMarkerCodingStillRunning
	incompleteTaskMarkerMaxRoundsReached
	incompleteTaskMarkerMaxRoundsReachedMojibake
	incompleteTaskMarkerApproachingMaxRounds
)

func (k incompleteTaskMarkerKind) IsIncompleteTask() bool {
	return k != incompleteTaskMarkerNone
}

func (k incompleteTaskMarkerKind) IsReasoningRoundMarker() bool {
	switch k {
	case incompleteTaskMarkerMaxRoundsReached, incompleteTaskMarkerMaxRoundsReachedMojibake, incompleteTaskMarkerApproachingMaxRounds:
		return true
	default:
		return false
	}
}

func classifyIncompleteTaskMarker(text string) incompleteTaskMarkerKind {
	trimmed := strings.TrimSpace(text)
	switch {
	case strings.Contains(trimmed, "coding session is still running"):
		return incompleteTaskMarkerCodingStillRunning
	case strings.Contains(trimmed, "maximum reasoning rounds"):
		return incompleteTaskMarkerMaxRoundsReached
	case strings.Contains(trimmed, "已达到最大推理轮次，请继续发送消息以完成任务"):
		return incompleteTaskMarkerMaxRoundsReachedMojibake
	case strings.Contains(trimmed, "(瀹歌尪鎻崚鐗堟付婢堆勫腹閻炲棜鐤嗗▎鈽呯礉鐠囬鎴风紒顓炲絺闁焦绉烽幁顖欎簰鐎瑰本鍨氭禒璇插)"):
		return incompleteTaskMarkerMaxRoundsReachedMojibake
	case strings.Contains(trimmed, "approaching maximum reasoning rounds"):
		return incompleteTaskMarkerApproachingMaxRounds
	default:
		return incompleteTaskMarkerNone
	}
}
