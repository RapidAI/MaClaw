package main

import "strings"

type imResponseSourceKind string

const (
	imResponseSourceUnknown          imResponseSourceKind = ""
	imResponseSourceFileDelivery     imResponseSourceKind = "file_delivery"
	imResponseSourceScreenshot       imResponseSourceKind = "screenshot"
	imResponseSourceAskUser          imResponseSourceKind = "ask_user"
	imResponseSourceRecordAudio      imResponseSourceKind = "record_audio"
	imResponseSourceCancel           imResponseSourceKind = "cancel"
	imResponseSourceAgentLoop        imResponseSourceKind = "agent_loop"
	imResponseSourceAgentViewSubmit  imResponseSourceKind = "agent_view_submit"
	imResponseSourceAgentViewDismiss imResponseSourceKind = "agent_view_dismiss"
)

func (k imResponseSourceKind) String() string {
	return string(k)
}

func (k imResponseSourceKind) IsArtifactDelivery() bool {
	return k == imResponseSourceFileDelivery
}

func (k imResponseSourceKind) IsKnownPreservedSource() bool {
	switch k {
	case imResponseSourceFileDelivery,
		imResponseSourceScreenshot,
		imResponseSourceAskUser,
		imResponseSourceRecordAudio,
		imResponseSourceCancel,
		imResponseSourceAgentViewSubmit,
		imResponseSourceAgentViewDismiss:
		return true
	default:
		return false
	}
}

func canonicalIMResponseSourceKind(source string) imResponseSourceKind {
	source = strings.TrimSpace(source)
	if source == "" {
		return imResponseSourceUnknown
	}
	token := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(source))
	switch token {
	case "filedelivery":
		return imResponseSourceFileDelivery
	case "screenshot", "screenshotcapture":
		return imResponseSourceScreenshot
	case "askuser":
		return imResponseSourceAskUser
	case "recordaudio", "recordingsession":
		return imResponseSourceRecordAudio
	case "cancel", "cancelled", "canceled":
		return imResponseSourceCancel
	case "agentloop":
		return imResponseSourceAgentLoop
	case "agentviewsubmit":
		return imResponseSourceAgentViewSubmit
	case "agentviewdismiss":
		return imResponseSourceAgentViewDismiss
	default:
		return imResponseSourceKind(source)
	}
}
