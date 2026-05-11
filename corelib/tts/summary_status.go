package tts

import "strings"

type voiceSummaryStatus string

const (
	voiceSummaryStatusSuccess           voiceSummaryStatus = "success"
	voiceSummaryStatusError             voiceSummaryStatus = "error"
	voiceSummaryStatusPaused            voiceSummaryStatus = "paused"
	voiceSummaryStatusNeedsConfirmation voiceSummaryStatus = "needs_confirmation"
)

func normalizeVoiceSummaryStatus(status string) voiceSummaryStatus {
	switch voiceSummaryStatus(strings.TrimSpace(status)) {
	case voiceSummaryStatusError:
		return voiceSummaryStatusError
	case voiceSummaryStatusPaused:
		return voiceSummaryStatusPaused
	case voiceSummaryStatusNeedsConfirmation:
		return voiceSummaryStatusNeedsConfirmation
	default:
		return voiceSummaryStatusSuccess
	}
}

func (s voiceSummaryStatus) String() string {
	return string(s)
}

func (s voiceSummaryStatus) Phrase() string {
	switch s {
	case voiceSummaryStatusError:
		return "任务处理失败"
	case voiceSummaryStatusPaused:
		return "任务已暂停"
	case voiceSummaryStatusNeedsConfirmation:
		return "需要任务确认"
	default:
		return "任务已完成"
	}
}
