package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolSendToIMPreservesExactTargetInArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	raw := h.toolSendToIM(map[string]interface{}{
		"path": path, "channel": "lansenger", "group_id": "group-7",
	})
	if !strings.Contains(raw, "|target64:") {
		t.Fatalf("payload missing target metadata: %q", raw)
	}
	obs := parseToolPayloadResult(raw)
	if obs.File == nil || obs.File.target.Channel != "lansenger" || obs.File.target.GroupID != "group-7" {
		t.Fatalf("parsed file target = %#v", obs.File)
	}
}
