package main

import "strings"

type CodingSubAgentGuardrailCategory string

const (
	codingSubAgentGuardrailCategoryPolicy     CodingSubAgentGuardrailCategory = "policy"
	codingSubAgentGuardrailCategoryGit        CodingSubAgentGuardrailCategory = "git"
	codingSubAgentGuardrailCategoryDelete     CodingSubAgentGuardrailCategory = "delete"
	codingSubAgentGuardrailCategoryShellWrite CodingSubAgentGuardrailCategory = "shell_write"
	codingSubAgentGuardrailCategoryCommand    CodingSubAgentGuardrailCategory = "command"
	codingSubAgentGuardrailCategoryHost       CodingSubAgentGuardrailCategory = "host"
	codingSubAgentGuardrailCategoryScope      CodingSubAgentGuardrailCategory = "scope"
)

func (c CodingSubAgentGuardrailCategory) String() string {
	return string(c)
}

type codingGuardrailResultMarker int

const (
	codingGuardrailResultMarkerNone codingGuardrailResultMarker = iota
	codingGuardrailResultMarkerHostUnavailable
	codingGuardrailResultMarkerProjectScope
)

func classifyCodingGuardrailResultMarker(result string) codingGuardrailResultMarker {
	lower := strings.ToLower(result)
	switch {
	case strings.Contains(lower, "host tool handler is unavailable"):
		return codingGuardrailResultMarkerHostUnavailable
	case strings.Contains(lower, "outside project") || strings.Contains(lower, "project"):
		return codingGuardrailResultMarkerProjectScope
	default:
		return codingGuardrailResultMarkerNone
	}
}
