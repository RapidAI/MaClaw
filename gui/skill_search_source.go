package main

import "strings"

type skillSearchSourceKind string

const (
	skillSearchSourceSkillMarket skillSearchSourceKind = "skillmarket"
	skillSearchSourceSkillHub    skillSearchSourceKind = "skillhub"
	skillSearchSourceClawHub     skillSearchSourceKind = "clawhub"
	skillSearchSourceGitHub      skillSearchSourceKind = "github"
)

func skillSearchSourceFromStatus(status string) skillSearchSourceKind {
	switch skillSearchSourceKind(strings.TrimSpace(status)) {
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
