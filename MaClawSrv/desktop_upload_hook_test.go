package main

import (
	"os"
	"strings"
	"testing"
)

// The upload hook must not go through the model page-script guard, which
// refuses prototype patches.
func TestUploadHookBypassesThePageScriptGuard(t *testing.T) {
	src, err := os.ReadFile("desktop_upload.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "RunPageScriptBatch(session, \"return \"+desktopUploadHookJS") {
		t.Fatal("upload hook runs through the guarded page-script path")
	}
	if !strings.Contains(string(src), "session.EvalValue(desktopUploadHookJS)") {
		t.Fatal("upload hook not evaluated directly")
	}
}
