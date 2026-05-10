package main

import "strings"

type browserEventKind string

const (
	browserEventKindUnknown  browserEventKind = ""
	browserEventKindObserve  browserEventKind = "observe"
	browserEventKindNavigate browserEventKind = "navigate"
	browserEventKindClick    browserEventKind = "click"
	browserEventKindType     browserEventKind = "type"
	browserEventKindWait     browserEventKind = "wait"
	browserEventKindExtract  browserEventKind = "extract"
	browserEventKindConsole  browserEventKind = "console"
	browserEventKindNetwork  browserEventKind = "network"
	browserEventKindError    browserEventKind = "error"
)

func normalizeBrowserEventKind(kind string) browserEventKind {
	switch browserEventKind(strings.ToLower(strings.TrimSpace(kind))) {
	case browserEventKindObserve:
		return browserEventKindObserve
	case browserEventKindNavigate:
		return browserEventKindNavigate
	case browserEventKindClick:
		return browserEventKindClick
	case browserEventKindType:
		return browserEventKindType
	case browserEventKindWait:
		return browserEventKindWait
	case browserEventKindExtract:
		return browserEventKindExtract
	case browserEventKindConsole:
		return browserEventKindConsole
	case browserEventKindNetwork:
		return browserEventKindNetwork
	case browserEventKindError:
		return browserEventKindError
	default:
		return browserEventKindUnknown
	}
}

func (k browserEventKind) Title(fallback string) string {
	switch k {
	case browserEventKindObserve:
		return "Observe"
	case browserEventKindNavigate:
		return "Navigate"
	case browserEventKindClick:
		return "Click"
	case browserEventKindType:
		return "Type"
	case browserEventKindWait:
		return "Wait"
	case browserEventKindExtract:
		return "Extract"
	case browserEventKindConsole:
		return "Console"
	case browserEventKindNetwork:
		return "Network"
	case browserEventKindError:
		return "Error"
	default:
		return strings.Title(fallback)
	}
}
