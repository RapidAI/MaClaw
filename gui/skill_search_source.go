package main

import "strings"

type skillSearchSourceKind string

const (
	skillSearchSourceEnterpriseHub skillSearchSourceKind = "enterprise_hub"
	skillSearchSourceSkillMarket   skillSearchSourceKind = "skillmarket"
	skillSearchSourceSkillHub      skillSearchSourceKind = "skillhub"
	skillSearchSourceClawHub       skillSearchSourceKind = "clawhub"
	skillSearchSourceGitHub        skillSearchSourceKind = "github"
)

func skillSearchSourceFromStatus(status string) skillSearchSourceKind {
	switch skillSearchSourceKind(strings.TrimSpace(status)) {
	case skillSearchSourceEnterpriseHub:
		return skillSearchSourceEnterpriseHub
	case skillSearchSourceClawHub:
		return skillSearchSourceClawHub
	case skillSearchSourceGitHub:
		return skillSearchSourceGitHub
	case skillSearchSourceSkillHub:
		return skillSearchSourceSkillHub
	default:
		return skillSearchSourceSkillMarket
	}
}

func (s skillSearchSourceKind) String() string {
	return string(s)
}
