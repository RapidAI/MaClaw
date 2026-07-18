package main

import (
	"strings"
	"testing"
)

func TestAcpProgrammingUserText(t *testing.T) {
	got := acpProgrammingUserText(`D:\work\demo`, "add README")
	if got == "add README" {
		t.Fatal("expected workspace wrapper")
	}
	for _, part := range []string{`D:\work\demo`, "add README", "VS Code", "disk"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
	if acpProgrammingUserText("", "hi") != "hi" {
		t.Fatal("empty cwd should pass through")
	}
}

func TestCollectACPResultPaths(t *testing.T) {
	resp := &IMAgentResponse{
		LocalFilePath:  `D:\a\x.go`,
		LocalFilePaths: []string{`D:\a\x.go`, `D:\a\y.go`, ""},
		FileName:       "ignored-when-local-set",
	}
	paths := collectACPResultPaths(resp)
	if len(paths) != 2 {
		t.Fatalf("paths=%v", paths)
	}
}
