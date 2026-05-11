package knowledge

type URLDiscoveryStatus string

const (
	URLDiscoveryStatusCandidate        URLDiscoveryStatus = "candidate"
	URLDiscoveryStatusRejected         URLDiscoveryStatus = "rejected"
	URLDiscoveryStatusSkippedDuplicate URLDiscoveryStatus = "skipped_duplicate"
)

func (s URLDiscoveryStatus) IsCandidate() bool {
	return s == URLDiscoveryStatusCandidate
}

type URLBatchSaveStatus string

const (
	URLBatchSaveStatusSkippedDuplicate URLBatchSaveStatus = "skipped_duplicate"
	URLBatchSaveStatusFailed           URLBatchSaveStatus = "failed"
)

func URLBatchSaveStatusFromSource(status string) URLBatchSaveStatus {
	return URLBatchSaveStatus(status)
}
