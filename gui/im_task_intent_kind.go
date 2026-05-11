package main

import "strings"

type taskIntent string

const (
	intentCoding    taskIntent = "coding"
	intentSSH       taskIntent = "ssh"
	intentNonCoding taskIntent = "non_coding"
	intentAmbiguous taskIntent = "ambiguous"
	intentUnknown   taskIntent = "unknown"
)

func (intent taskIntent) String() string {
	return string(intent)
}

func (intent taskIntent) IsKnown() bool {
	return intent != "" && intent != intentUnknown
}

func normalizeTaskIntent(raw taskIntent) taskIntent {
	switch taskIntent(strings.TrimSpace(strings.ToLower(raw.String()))) {
	case intentCoding:
		return intentCoding
	case intentSSH:
		return intentSSH
	case intentNonCoding:
		return intentNonCoding
	case intentAmbiguous:
		return intentAmbiguous
	default:
		return intentUnknown
	}
}
