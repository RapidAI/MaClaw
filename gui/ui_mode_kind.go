package main

import "strings"

type uiModeKind string

const (
	uiModeUnknown uiModeKind = ""
	uiModeLite    uiModeKind = "lite"
	uiModePro     uiModeKind = "pro"
)

func normalizeUIModeKind(value string) uiModeKind {
	switch uiModeKind(strings.ToLower(strings.TrimSpace(value))) {
	case uiModeLite:
		return uiModeLite
	case uiModePro:
		return uiModePro
	default:
		return uiModeUnknown
	}
}

func (kind uiModeKind) IsProDefault() bool {
	return kind == uiModeUnknown || kind == uiModePro
}

func (kind uiModeKind) IsProExplicit() bool {
	return kind == uiModePro
}
