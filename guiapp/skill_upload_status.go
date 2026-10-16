package guiapp

import (
	"encoding/json"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type skillUploadStatus string

const (
	skillUploadStatusPending   skillUploadStatus = "pending"
	skillUploadStatusUploading skillUploadStatus = "uploading"
	// remote_submitted means all remote targets accepted the package, but the
	// local receipt is still pending. Retrying this state must never resubmit
	// the package; it only retries local persistence.
	skillUploadStatusRemoteSubmitted skillUploadStatus = "remote_submitted"
	skillUploadStatusUploaded        skillUploadStatus = "uploaded"
	skillUploadStatusBlocked         skillUploadStatus = "blocked"
	skillUploadStatusFailed          skillUploadStatus = "failed"
)

func (s skillUploadStatus) String() string {
	return string(s)
}

func (s skillUploadStatus) RuntimeStatusValue() agentruntime.JobStatus {
	switch s {
	case skillUploadStatusPending:
		return agentruntime.JobStatusPending
	case skillUploadStatusUploading:
		return agentruntime.JobStatusRunning
	case skillUploadStatusUploaded:
		return agentruntime.JobStatusSucceeded
	case skillUploadStatusFailed, skillUploadStatusBlocked:
		return agentruntime.JobStatusFailed
	default:
		return agentruntime.JobStatusUnknown
	}
}

func (item SkillUploadQueueItem) MarshalJSON() ([]byte, error) {
	type alias SkillUploadQueueItem
	copy := alias(item)
	copy.RuntimeStatus = item.Status.RuntimeStatusValue()
	return json.Marshal(copy)
}

func (s skillUploadStatus) IsPending() bool {
	return s == skillUploadStatusPending
}

func (s skillUploadStatus) IsUploading() bool {
	return s == skillUploadStatusUploading
}

func (s skillUploadStatus) IsRemoteSubmitted() bool {
	return s == skillUploadStatusRemoteSubmitted
}

func (s skillUploadStatus) IsUploaded() bool {
	return s == skillUploadStatusUploaded
}

func (s skillUploadStatus) IsFailed() bool {
	return s == skillUploadStatusFailed
}

func (s skillUploadStatus) IsBlocked() bool {
	return s == skillUploadStatusBlocked
}
