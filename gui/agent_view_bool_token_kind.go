package main

import "strings"

type agentViewBoolTokenKind string

const (
	agentViewBoolTokenUnknown  agentViewBoolTokenKind = ""
	agentViewBoolTokenOne      agentViewBoolTokenKind = "1"
	agentViewBoolTokenZero     agentViewBoolTokenKind = "0"
	agentViewBoolTokenTrue     agentViewBoolTokenKind = "true"
	agentViewBoolTokenFalse    agentViewBoolTokenKind = "false"
	agentViewBoolTokenYes      agentViewBoolTokenKind = "yes"
	agentViewBoolTokenNo       agentViewBoolTokenKind = "no"
	agentViewBoolTokenY        agentViewBoolTokenKind = "y"
	agentViewBoolTokenN        agentViewBoolTokenKind = "n"
	agentViewBoolTokenOn       agentViewBoolTokenKind = "on"
	agentViewBoolTokenOff      agentViewBoolTokenKind = "off"
	agentViewBoolTokenEnabled  agentViewBoolTokenKind = "enabled"
	agentViewBoolTokenDisabled agentViewBoolTokenKind = "disabled"
)

func normalizeAgentViewBoolTokenKind(value string) agentViewBoolTokenKind {
	switch kind := agentViewBoolTokenKind(strings.ToLower(strings.TrimSpace(value))); kind {
	case agentViewBoolTokenOne,
		agentViewBoolTokenZero,
		agentViewBoolTokenTrue,
		agentViewBoolTokenFalse,
		agentViewBoolTokenYes,
		agentViewBoolTokenNo,
		agentViewBoolTokenY,
		agentViewBoolTokenN,
		agentViewBoolTokenOn,
		agentViewBoolTokenOff,
		agentViewBoolTokenEnabled,
		agentViewBoolTokenDisabled:
		return kind
	default:
		return agentViewBoolTokenUnknown
	}
}

func (kind agentViewBoolTokenKind) BoolValue() (bool, bool) {
	switch kind {
	case agentViewBoolTokenOne,
		agentViewBoolTokenTrue,
		agentViewBoolTokenYes,
		agentViewBoolTokenY,
		agentViewBoolTokenOn,
		agentViewBoolTokenEnabled:
		return true, true
	case agentViewBoolTokenZero,
		agentViewBoolTokenFalse,
		agentViewBoolTokenNo,
		agentViewBoolTokenN,
		agentViewBoolTokenOff,
		agentViewBoolTokenDisabled:
		return false, true
	default:
		return false, false
	}
}

func coerceAgentViewBoolToken(value string) (bool, bool) {
	return normalizeAgentViewBoolTokenKind(value).BoolValue()
}
