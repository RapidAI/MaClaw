package main

import "regexp"

var ansiEscapeForTest = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSIForTest(s string) string {
	return ansiEscapeForTest.ReplaceAllString(s, "")
}
