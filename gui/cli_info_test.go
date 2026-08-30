package main

import (
	"strings"
	"testing"
)

func TestHandleCLIInfoRecognizesVersionAndHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: []string{"maclaw"}, want: false},
		{name: "desktop default", args: []string{"maclaw", "init"}, want: false},
		{name: "tui is not info", args: []string{"maclaw", "tui"}, want: false},
		{name: "remote-smoke is not info", args: []string{"maclaw", "remote-smoke"}, want: false},
		{name: "version long", args: []string{"maclaw", "--version"}, want: true},
		{name: "version bare", args: []string{"maclaw", "version"}, want: true},
		{name: "help long", args: []string{"maclaw", "--help"}, want: true},
		{name: "help short", args: []string{"maclaw", "-h"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := handleCLIInfo(tt.args); got != tt.want {
				t.Fatalf("handleCLIInfo(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestCLIHelpMentionsRemoteLinuxAppImage(t *testing.T) {
	if !strings.Contains(cliHelpText, "u2404") || !strings.Contains(cliHelpText, "WebKit") {
		t.Fatalf("cli help should tell remote Linux hosts which AppImage to download:\n%s", cliHelpText)
	}
}
