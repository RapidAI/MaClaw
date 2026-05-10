package main

import "strings"

type appLogLineKind int

const (
	appLogLineOther appLogLineKind = iota
	appLogLineError
)

func classifyAppLogLine(line string) appLogLineKind {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, " error"),
		strings.Contains(lower, "[error]"),
		strings.Contains(lower, "fatal"),
		strings.Contains(lower, "panic:"),
		strings.Contains(lower, "panic("):
		return appLogLineError
	default:
		return appLogLineOther
	}
}
