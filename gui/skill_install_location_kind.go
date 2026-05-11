package main

import "strings"

type skillInstallLocationKind string

const (
	skillInstallLocationUnknown skillInstallLocationKind = ""
	skillInstallLocationUser    skillInstallLocationKind = "user"
	skillInstallLocationProject skillInstallLocationKind = "project"
)

func normalizeSkillInstallLocationKind(value string) skillInstallLocationKind {
	switch skillInstallLocationKind(strings.ToLower(strings.TrimSpace(value))) {
	case skillInstallLocationUser:
		return skillInstallLocationUser
	case skillInstallLocationProject:
		return skillInstallLocationProject
	default:
		return skillInstallLocationUnknown
	}
}

func (location skillInstallLocationKind) IsUser() bool {
	return location == skillInstallLocationUser
}

func (location skillInstallLocationKind) IsProject() bool {
	return location == skillInstallLocationProject
}
