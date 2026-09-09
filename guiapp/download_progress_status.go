package guiapp

import (
	"encoding/json"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type downloadProgressStatus string

const (
	downloadProgressStatusDownloading downloadProgressStatus = "downloading"
	downloadProgressStatusVerifying   downloadProgressStatus = "verifying"
	downloadProgressStatusCompleted   downloadProgressStatus = "completed"
	downloadProgressStatusError       downloadProgressStatus = "error"
	downloadProgressStatusCancelled   downloadProgressStatus = "cancelled"
)

func (s downloadProgressStatus) RuntimeStatusValue() agentruntime.JobStatus {
	return agentruntime.ProjectLegacyJobStatus(s.String())
}

func (s downloadProgressStatus) String() string { return string(s) }

func downloadProgressNamedStatus(pct int, errMsg string) downloadProgressStatus {
	if errMsg != "" {
		return downloadProgressStatusError
	}
	if pct >= 100 {
		return downloadProgressStatusCompleted
	}
	return downloadProgressStatusDownloading
}

// MarshalJSON adds the canonical Runtime status to legacy download events
// without requiring every existing event producer to change its struct literal.
func (p DownloadProgress) MarshalJSON() ([]byte, error) {
	type downloadProgressAlias DownloadProgress
	copy := downloadProgressAlias(p)
	copy.RuntimeStatus = p.Status.RuntimeStatusValue()
	return json.Marshal(copy)
}
