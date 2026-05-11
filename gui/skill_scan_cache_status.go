package main

import "strings"

type skillScanCacheStatus string

const (
	skillScanCacheStatusUnknown skillScanCacheStatus = ""
	skillScanCacheStatusAllowed skillScanCacheStatus = "allowed"
	skillScanCacheStatusBlocked skillScanCacheStatus = "blocked"
)

func normalizeSkillScanCacheStatus(status skillScanCacheStatus) skillScanCacheStatus {
	switch skillScanCacheStatus(strings.TrimSpace(status.String())) {
	case skillScanCacheStatusAllowed:
		return skillScanCacheStatusAllowed
	case skillScanCacheStatusBlocked:
		return skillScanCacheStatusBlocked
	default:
		return skillScanCacheStatusUnknown
	}
}

func (s skillScanCacheStatus) String() string {
	return string(s)
}

func (s skillScanCacheStatus) IsAllowed() bool {
	return s == skillScanCacheStatusAllowed
}

func (s skillScanCacheStatus) IsBlocked() bool {
	return s == skillScanCacheStatusBlocked
}
