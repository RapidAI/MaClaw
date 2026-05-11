package agent

import "strings"

// ConfirmationStatus is the persisted lifecycle state for a pending execution
// confirmation. It remains a string alias to preserve JSON compatibility.
type ConfirmationStatus string

const (
	ConfirmationStatusUnknown ConfirmationStatus = ""
	ConfirmationStatusPending ConfirmationStatus = "pending"
)

func NormalizeConfirmationStatus(status string) ConfirmationStatus {
	switch ConfirmationStatus(strings.ToLower(strings.TrimSpace(status))) {
	case ConfirmationStatusPending:
		return ConfirmationStatusPending
	default:
		return ConfirmationStatusUnknown
	}
}

func (status ConfirmationStatus) String() string {
	return string(status)
}
