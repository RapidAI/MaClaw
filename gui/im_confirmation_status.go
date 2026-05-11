package main

import "strings"

type confirmationStatus string

const (
	confirmationStatusUnknown confirmationStatus = ""
	confirmationStatusPending confirmationStatus = "pending"
)

func normalizeConfirmationStatus(status string) confirmationStatus {
	switch confirmationStatus(strings.ToLower(strings.TrimSpace(status))) {
	case confirmationStatusPending:
		return confirmationStatusPending
	default:
		return confirmationStatusUnknown
	}
}

func (status confirmationStatus) String() string {
	return string(status)
}
