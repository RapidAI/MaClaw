package main

import "strings"

type skillAgentViewFieldKind int

const (
	skillAgentViewFieldText skillAgentViewFieldKind = iota
	skillAgentViewFieldSelect
	skillAgentViewFieldBoolean
	skillAgentViewFieldTextarea
	skillAgentViewFieldNumber
	skillAgentViewFieldDate
	skillAgentViewFieldFile
	skillAgentViewFieldDirectory
)

func inferSkillAgentViewFieldKind(name, description string) skillAgentViewFieldKind {
	text := strings.ToLower(name + " " + description)
	trimmedName := strings.ToLower(strings.TrimSpace(name))
	switch {
	case len(skillAgentViewEnumOptions(description)) > 0:
		return skillAgentViewFieldSelect
	case strings.Contains(text, "enabled") || strings.Contains(text, "disabled") || strings.Contains(text, "boolean") || strings.Contains(text, "true/false") || strings.Contains(text, "yes/no") || strings.HasPrefix(trimmedName, "is_") || strings.HasPrefix(trimmedName, "has_"):
		return skillAgentViewFieldBoolean
	case strings.Contains(text, "count") || strings.Contains(text, "num") || strings.Contains(text, "number") || strings.Contains(text, "seconds") || strings.Contains(text, "limit") || strings.Contains(text, "amount") || strings.Contains(text, "price") || strings.Contains(text, "total"):
		return skillAgentViewFieldNumber
	case strings.Contains(text, "date") && !strings.Contains(text, "update"):
		return skillAgentViewFieldDate
	case strings.Contains(text, "workdir") || strings.Contains(text, "working dir") || strings.Contains(text, "working directory") || agentViewTextHasWord(text, "directory") || agentViewTextHasWord(text, "folder") || agentViewTextHasWord(text, "cwd") || agentViewTextHasWord(text, "dir"):
		return skillAgentViewFieldDirectory
	case strings.Contains(text, "filepath") || strings.Contains(text, "file path") || agentViewTextHasWord(text, "file") || agentViewTextHasWord(text, "path"):
		return skillAgentViewFieldFile
	case strings.Contains(text, "prompt") || strings.Contains(text, "content") || strings.Contains(text, "text") || strings.Contains(text, "markdown"):
		return skillAgentViewFieldTextarea
	default:
		return skillAgentViewFieldText
	}
}

func agentViewTextHasWord(text, word string) bool {
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == '/' || r == '\\' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if part == word {
			return true
		}
	}
	return false
}

func (k skillAgentViewFieldKind) FieldType() agentViewFieldType {
	switch k {
	case skillAgentViewFieldSelect:
		return agentViewFieldTypeSelect
	case skillAgentViewFieldBoolean:
		return agentViewFieldTypeBoolean
	case skillAgentViewFieldTextarea:
		return agentViewFieldTypeTextarea
	case skillAgentViewFieldNumber:
		return agentViewFieldTypeNumber
	case skillAgentViewFieldDate:
		return agentViewFieldTypeDate
	case skillAgentViewFieldFile:
		return agentViewFieldTypeFile
	case skillAgentViewFieldDirectory:
		return agentViewFieldTypeDirectory
	default:
		return agentViewFieldTypeText
	}
}
