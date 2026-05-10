package main

import "strings"

type skillTypeKind string

const (
	skillTypeUnknown   skillTypeKind = ""
	skillTypeZip       skillTypeKind = "zip"
	skillTypeAddress   skillTypeKind = "address"
	skillTypeKnowledge skillTypeKind = "knowledge"
)

func normalizeSkillTypeKind(value string) skillTypeKind {
	switch skillTypeKind(strings.TrimSpace(value)) {
	case skillTypeZip:
		return skillTypeZip
	case skillTypeAddress:
		return skillTypeAddress
	case skillTypeKnowledge:
		return skillTypeKnowledge
	default:
		return skillTypeUnknown
	}
}

func (kind skillTypeKind) String() string {
	return string(kind)
}

func (kind skillTypeKind) IsZip() bool {
	return kind == skillTypeZip
}

func (kind skillTypeKind) IsAddress() bool {
	return kind == skillTypeAddress
}

func (kind skillTypeKind) IsKnowledge() bool {
	return kind == skillTypeKnowledge
}
