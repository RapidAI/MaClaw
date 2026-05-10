package main

import "strings"

type toolBoolTokenKind string

const (
	toolBoolTokenUnknown  toolBoolTokenKind = ""
	toolBoolTokenTrue     toolBoolTokenKind = "true"
	toolBoolTokenFalse    toolBoolTokenKind = "false"
	toolBoolTokenOne      toolBoolTokenKind = "1"
	toolBoolTokenZero     toolBoolTokenKind = "0"
	toolBoolTokenYes      toolBoolTokenKind = "yes"
	toolBoolTokenNo       toolBoolTokenKind = "no"
	toolBoolTokenY        toolBoolTokenKind = "y"
	toolBoolTokenN        toolBoolTokenKind = "n"
	toolBoolTokenOn       toolBoolTokenKind = "on"
	toolBoolTokenOff      toolBoolTokenKind = "off"
	toolBoolTokenEnabled  toolBoolTokenKind = "enabled"
	toolBoolTokenDisabled toolBoolTokenKind = "disabled"
	toolBoolTokenOptional toolBoolTokenKind = "optional"
)

func normalizeToolBoolTokenKind(value string) toolBoolTokenKind {
	switch kind := toolBoolTokenKind(strings.ToLower(strings.TrimSpace(value))); kind {
	case toolBoolTokenTrue,
		toolBoolTokenFalse,
		toolBoolTokenOne,
		toolBoolTokenZero,
		toolBoolTokenYes,
		toolBoolTokenNo,
		toolBoolTokenY,
		toolBoolTokenN,
		toolBoolTokenOn,
		toolBoolTokenOff,
		toolBoolTokenEnabled,
		toolBoolTokenDisabled,
		toolBoolTokenOptional:
		return kind
	default:
		return toolBoolTokenUnknown
	}
}

func (kind toolBoolTokenKind) BoolValue() (bool, bool) {
	switch kind {
	case toolBoolTokenTrue,
		toolBoolTokenOne,
		toolBoolTokenYes,
		toolBoolTokenY,
		toolBoolTokenOn,
		toolBoolTokenEnabled:
		return true, true
	case toolBoolTokenFalse,
		toolBoolTokenZero,
		toolBoolTokenNo,
		toolBoolTokenN,
		toolBoolTokenOff,
		toolBoolTokenDisabled,
		toolBoolTokenOptional:
		return false, true
	default:
		return false, false
	}
}

func coerceToolBoolToken(value string) (bool, bool) {
	return normalizeToolBoolTokenKind(value).BoolValue()
}
